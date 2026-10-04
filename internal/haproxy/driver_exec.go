package haproxy

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
)

// linuxRealExec is the production Exec: it runs real commands via os/exec.
// sudo is made non-interactive (-n) so a missing sudoers rule fails instead of
// hanging on a password prompt.
type linuxRealExec struct{}

// NewExec returns the real command layer.
func NewExec() Exec { return linuxRealExec{} }

func (linuxRealExec) Run(ctx context.Context, stdin []byte, name string, args ...string) ([]byte, []byte, error) {
	if name == "sudo" {
		args = append([]string{"-n"}, args...)
	}
	discard := discardsStdout(name, args...)
	cmd := exec.CommandContext(ctx, name, args...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	// Homebrew binaries (haproxy on Apple silicon) live outside the default PATH.
	cmd.Env = append(os.Environ(), "PATH="+os.Getenv("PATH")+":/opt/homebrew/bin:/opt/homebrew/sbin")
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	if discard {
		cmd.Stdout = io.Discard
	}
	err := cmd.Run()
	return out.Bytes(), errOut.Bytes(), err
}

// discardsStdout reports whether the command is tee (directly or via sudo [-n]).
// tee echoes its stdin, which carries private-key bytes, so its stdout is never
// captured.
func discardsStdout(name string, args ...string) bool {
	if name == "sudo" {
		if len(args) > 0 && args[0] == "-n" {
			args = args[1:]
		}
		if len(args) == 0 {
			return false
		}
		name = args[0]
	}
	return name == "tee"
}
