//go:build unix

package smbedit

import (
	"os/exec"
	"syscall"
	"time"
)

// configureProcessGroup arranges full teardown of a streaming command and its
// descendants (D-6). "sudo tail -F" is two processes: cancelling the context
// with the default Cancel kills only sudo, orphaning tail against the log
// file forever. Setpgid puts both in a fresh process group; Cancel signals
// the negative PID, which addresses the whole group; WaitDelay bounds the
// reap so a survivor that ignores the signal is force-killed on a timer.
func configureProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.WaitDelay = 5 * time.Second
}
