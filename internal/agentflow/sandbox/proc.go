package sandbox

import (
	"os/exec"
	"syscall"
	"time"
)

// killGrace is how long a cancelled group gets between SIGTERM and SIGKILL.
const killGrace = 5 * time.Second

// setupProcessGroup puts the command in its own process group and makes
// context cancellation (including timeout) signal that group: SIGTERM now,
// SIGKILL after killGrace. The group is only best effort: bwrap starts the
// sandbox in a new session, so the guarantee that no descendant survives is
// --unshare-pid plus --die-with-parent (bwrap dying kills the whole PID
// namespace). The outcome tests depend on that.
func setupProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = killGrace + time.Second
	cmd.Cancel = func() error {
		pid := cmd.Process.Pid
		err := syscall.Kill(-pid, syscall.SIGTERM)
		time.AfterFunc(killGrace, func() {
			// Process.Signal fails once Wait has reaped the process, so a
			// recycled pid is never signalled.
			if cmd.Process.Signal(syscall.Signal(0)) == nil {
				_ = syscall.Kill(-pid, syscall.SIGKILL)
			}
		})
		return err
	}
}
