// trust.go implements the server side of the "Trust this CA on this device"
// button (POST /api/ca/trust, gated by certmachine.trust_device_enabled):
// detect which of macOS, an RHEL-family Linux, or a Debian-family Linux this
// process is running on, and run that platform's native "add a root CA to
// the system trust store" procedure via sudo.
//
// This is deliberately scoped to the machine the unified-webapp process runs
// on, not the browser's machine -- there is no way to run a trust-store
// command on the client from an HTTP handler, and the intended use (per the
// feature request) is a lab box where certmachine and the services that
// consume its certs are the same host. Every command runs `sudo -n` (never
// prompt): a web handler has no TTY to answer a password prompt on, so
// without -n a misconfigured host would hang the request until the client
// gives up rather than fail immediately with a clear "sudo needs a NOPASSWD
// entry" error. See the README's "Automatic device trust" section for the
// sudoers line each platform needs and the security tradeoff of adding it.
package certmachine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// TrustPlatform identifies which device-trust procedure InstallTrust runs.
type TrustPlatform string

const (
	TrustPlatformDarwin     TrustPlatform = "darwin"
	TrustPlatformRHELFamily TrustPlatform = "rhel"
	TrustPlatformDebian     TrustPlatform = "debian"
)

// ErrTrustPlatformUnsupported means this host is not one of the three
// platforms InstallTrust knows how to automate (e.g. Windows, or a Linux
// distro whose /etc/os-release this package does not recognize). Trusting
// the CA there stays the manual, per-OS procedure the README documents.
var ErrTrustPlatformUnsupported = errors.New("certmachine: automatic device trust is not supported on this platform")

// DetectTrustPlatform reports which trust-install procedure applies to the
// host this process is running on. It is a var, not a plain function, so
// tests can stub it without depending on the actual machine running them.
var DetectTrustPlatform = detectTrustPlatform

func detectTrustPlatform() (TrustPlatform, error) {
	switch runtime.GOOS {
	case "darwin":
		return TrustPlatformDarwin, nil
	case "linux":
		return detectLinuxTrustPlatform()
	default:
		return "", ErrTrustPlatformUnsupported
	}
}

// detectLinuxTrustPlatform reads /etc/os-release (the standard, systemd-owned
// location every mainstream distro ships) and classifies ID (falling back to
// ID_LIKE for derivatives that don't set ID to a value listed here directly,
// e.g. some Rocky/Alma point-releases) into the two families this package
// automates. update-ca-trust and update-ca-certificates are not
// interchangeable -- running the wrong one either does nothing or fails --
// so an unrecognized ID is refused rather than guessed at.
func detectLinuxTrustPlatform() (TrustPlatform, error) {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return "", fmt.Errorf("%w: reading /etc/os-release: %v", ErrTrustPlatformUnsupported, err)
	}
	fields := parseOSRelease(string(data))
	ids := append([]string{fields["ID"]}, strings.Fields(fields["ID_LIKE"])...)
	for _, id := range ids {
		switch id {
		case "rhel", "rocky", "centos", "fedora", "almalinux":
			return TrustPlatformRHELFamily, nil
		case "debian", "ubuntu":
			return TrustPlatformDebian, nil
		}
	}
	return "", fmt.Errorf("%w: unrecognized /etc/os-release ID %q", ErrTrustPlatformUnsupported, fields["ID"])
}

// parseOSRelease parses the shell-variable-assignment lines /etc/os-release
// is specified to contain into a plain map, stripping the double quotes
// distros conventionally (but are not required to) wrap values in. It does
// not handle shell escaping beyond that -- os-release values in practice
// never need it.
func parseOSRelease(s string) map[string]string {
	out := make(map[string]string)
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		out[k] = strings.Trim(v, `"`)
	}
	return out
}

// trustCommandTimeout bounds every external command InstallTrust runs. sudo
// waiting on a password it can never receive is the main thing this guards
// against -- combined with -n it should never actually fire, but a hung
// update-ca-trust/update-ca-certificates run must not wedge the HTTP request
// forever either.
const trustCommandTimeout = 30 * time.Second

// trustDestFilename is the name InstallTrust gives the CA certificate under
// the two Linux anchor directories. Fixed rather than derived from the CA's
// subject: there is exactly one root CA per certmachine instance (FR-6), so
// a stable name lets a second click cleanly overwrite the first rather than
// accumulating a new anchor file per attempt.
const trustDestFilename = "certmachine-rootCA"

// InstallTrust writes rootPEM to a private temp file and runs the detected
// platform's native "trust this root CA system-wide" procedure against it.
// The combined stdout+stderr of every command attempted is returned
// regardless of outcome -- including on error -- so a sudo permission
// refusal or a missing update-ca-trust binary is visible to whoever clicked
// the button rather than only in the server log.
func InstallTrust(ctx context.Context, rootPEM []byte) (output string, err error) {
	platform, err := DetectTrustPlatform()
	if err != nil {
		return "", err
	}

	tmp, err := os.CreateTemp("", "certmachine-rootCA-*.crt")
	if err != nil {
		return "", fmt.Errorf("certmachine: writing temp cert for trust install: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(rootPEM); err != nil {
		tmp.Close()
		return "", fmt.Errorf("certmachine: writing temp cert for trust install: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("certmachine: writing temp cert for trust install: %w", err)
	}

	var cmds [][]string
	switch platform {
	case TrustPlatformDarwin:
		cmds = [][]string{
			{"sudo", "-n", "security", "add-trusted-cert", "-d", "-r", "trustRoot",
				"-k", "/Library/Keychains/System.keychain", tmpPath},
		}
	case TrustPlatformRHELFamily:
		dest := "/etc/pki/ca-trust/source/anchors/" + trustDestFilename + ".pem"
		cmds = [][]string{
			{"sudo", "-n", "cp", tmpPath, dest},
			{"sudo", "-n", "update-ca-trust", "extract"},
		}
	case TrustPlatformDebian:
		dest := "/usr/local/share/ca-certificates/" + trustDestFilename + ".crt"
		cmds = [][]string{
			{"sudo", "-n", "cp", tmpPath, dest},
			{"sudo", "-n", "update-ca-certificates"},
		}
	default:
		return "", ErrTrustPlatformUnsupported
	}

	var out bytes.Buffer
	for _, args := range cmds {
		fmt.Fprintf(&out, "$ %s\n", strings.Join(args, " "))

		runCtx, cancel := context.WithTimeout(ctx, trustCommandTimeout)
		cmd := exec.CommandContext(runCtx, args[0], args[1:]...)
		cmd.Stdout = &out
		cmd.Stderr = &out
		runErr := cmd.Run()
		cancel()

		if runErr != nil {
			fmt.Fprintf(&out, "\nfailed: %v\n", runErr)
			return out.String(), fmt.Errorf("certmachine: %s: %w", strings.Join(args, " "), runErr)
		}
		out.WriteByte('\n')
	}
	return out.String(), nil
}
