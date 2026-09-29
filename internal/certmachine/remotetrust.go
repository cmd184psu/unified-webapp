// remotetrust.go installs certmachine's root CA into another machine's system
// trust store over SSH (POST /api/ca/trust/remote). It detects the remote OS
// and, for one it knows (macOS, an RHEL-family or Debian-family Linux, or
// Windows), runs that platform's trust procedure; for anything else it does
// nothing.
//
// Safety properties:
//   - The CA's PEM reaches the remote machine as the SSH session's input
//     (`cat > <mktemp file>`), never spliced into a command line.
//   - Only fixed commands run, built from trustCommands (shared with local
//     trust) and a mktemp path that is validated before use.
//   - Privileged steps run through `sudo -n`, which never prompts: the SSH
//     user needs passwordless sudo, or the login must be root. No sudo
//     password is ever collected or sent.
//   - SSH itself (credentials, key directory, host-key policy) comes from the
//     shared sshclient package, the same layer multissh uses.
package certmachine

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"cmd184psu/unified-webapp/internal/platform/sshclient"

	"golang.org/x/crypto/ssh"
)

// remoteTrustTimeout bounds a whole remote trust attempt: connect, detect,
// upload and install.
const remoteTrustTimeout = 90 * time.Second

// remoteTempPattern is the only temp path shape accepted back from mktemp.
var remoteTempPattern = regexp.MustCompile(`^/tmp/certmachine-rootCA\.[A-Za-z0-9]+$`)

// ErrRemoteSudo means a privileged step was refused because sudo wanted a
// password.
var ErrRemoteSudo = errors.New("certmachine: the SSH user needs passwordless sudo (or connect as root)")

// windowsTrustCommand writes the PEM it reads from stdin to a temp file,
// adds it to the machine's Trusted Root store, removes the file, and exits
// with certutil's status. It runs under Windows OpenSSH's default shell
// (cmd.exe), which hands the quoted script to PowerShell.
const windowsTrustCommand = `powershell -NoProfile -NonInteractive -Command "` +
	`$p = Join-Path $env:TEMP 'certmachine-rootCA.crt'; ` +
	`[IO.File]::WriteAllText($p, [Console]::In.ReadToEnd()); ` +
	`certutil -addstore -f Root $p; $c = $LASTEXITCODE; ` +
	`Remove-Item $p -Force; exit $c"`

// transcript records each remote command and its output, for the dialog.
type transcript struct{ strings.Builder }

func (t *transcript) run(ctx context.Context, client *ssh.Client, cmd string, stdin []byte) (string, error) {
	fmt.Fprintf(&t.Builder, "$ %s\n", cmd)
	out, err := sshclient.Run(ctx, client, cmd, stdin)
	t.WriteString(out)
	if out != "" && !strings.HasSuffix(out, "\n") {
		t.WriteByte('\n')
	}
	if err != nil {
		fmt.Fprintf(&t.Builder, "failed: %v\n", err)
	}
	return out, err
}

// detectRemotePlatform works out which trust procedure applies to the remote
// machine. Unix-likes answer `uname -s`; Windows (OpenSSH) doesn't have
// uname, so it's recognized from `cmd /c ver`.
func detectRemotePlatform(ctx context.Context, client *ssh.Client, t *transcript) (TrustPlatform, error) {
	out, err := t.run(ctx, client, "uname -s", nil)
	if err == nil {
		switch strings.TrimSpace(out) {
		case "Darwin":
			return TrustPlatformDarwin, nil
		case "Linux":
			rel, err := t.run(ctx, client, "cat /etc/os-release", nil)
			if err != nil {
				return "", fmt.Errorf("%w: reading /etc/os-release: %v", ErrTrustPlatformUnsupported, err)
			}
			return classifyOSRelease(rel)
		default:
			return "", fmt.Errorf("%w: uname reports %q", ErrTrustPlatformUnsupported, strings.TrimSpace(out))
		}
	}
	if ver, err := t.run(ctx, client, "cmd /c ver", nil); err == nil && strings.Contains(ver, "Windows") {
		return TrustPlatformWindows, nil
	}
	return "", fmt.Errorf("%w: couldn't identify the remote operating system", ErrTrustPlatformUnsupported)
}

// InstallTrustRemote connects with creds, detects the remote OS, and installs
// rootPEM into its system trust store. It returns the detected platform
// (empty if detection failed) and a transcript of every command run, both
// also on error. An unsupported OS returns ErrTrustPlatformUnsupported and
// installs nothing.
func InstallTrustRemote(ctx context.Context, creds sshclient.Credentials, hostKeyCB ssh.HostKeyCallback, rootPEM []byte, anchor string) (TrustPlatform, string, error) {
	ctx, cancel := context.WithTimeout(ctx, remoteTrustTimeout)
	defer cancel()

	client, err := sshclient.Dial(ctx, creds, hostKeyCB)
	if err != nil {
		return "", "", err
	}
	defer client.Close()

	var t transcript
	platform, err := detectRemotePlatform(ctx, client, &t)
	if err != nil {
		return "", t.String(), err
	}

	if platform == TrustPlatformWindows {
		if _, err := t.run(ctx, client, windowsTrustCommand, rootPEM); err != nil {
			return platform, t.String(), fmt.Errorf("certmachine: installing on Windows (the SSH user must be an administrator): %w", err)
		}
		return platform, t.String(), nil
	}

	// Root needs no sudo (and may not have it installed).
	uid, err := t.run(ctx, client, "id -u", nil)
	if err != nil {
		return platform, t.String(), fmt.Errorf("certmachine: checking the remote user: %w", err)
	}
	useSudo := strings.TrimSpace(uid) != "0"

	tmpOut, err := t.run(ctx, client, "mktemp /tmp/certmachine-rootCA.XXXXXX", nil)
	if err != nil {
		return platform, t.String(), fmt.Errorf("certmachine: creating a temp file: %w", err)
	}
	tmp := strings.TrimSpace(tmpOut)
	if !remoteTempPattern.MatchString(tmp) {
		return platform, t.String(), fmt.Errorf("certmachine: unexpected temp path from mktemp: %q", tmp)
	}
	defer func() {
		// Best effort, on a fresh context so a timed-out install still cleans up.
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _ = sshclient.Run(cleanup, client, "rm -f "+tmp, nil)
	}()

	if _, err := t.run(ctx, client, "cat > "+tmp, rootPEM); err != nil {
		return platform, t.String(), fmt.Errorf("certmachine: uploading the CA: %w", err)
	}

	cmds, err := trustCommands(platform, tmp, anchor, useSudo)
	if err != nil {
		return platform, t.String(), err
	}
	for _, args := range cmds {
		out, err := t.run(ctx, client, strings.Join(args, " "), nil)
		if err != nil {
			if useSudo && strings.Contains(out, "password is required") {
				return platform, t.String(), ErrRemoteSudo
			}
			return platform, t.String(), fmt.Errorf("certmachine: %s: %w", strings.Join(args, " "), err)
		}
	}
	return platform, t.String(), nil
}
