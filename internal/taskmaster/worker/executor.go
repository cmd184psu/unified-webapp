package worker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"cmd184psu/unified-webapp/internal/taskmaster/models"
)

// waitDelay bounds cmd.Wait after ctx cancel even when (a) a backgrounded
// grandchild inherits the stdout/stderr pipes and never EOFs them, or
// (b) a sudo child is root-owned and our unprivileged kill gets EPERM.
// After cancel+waitDelay, Wait force-closes the pipes and returns.
const waitDelay = 10 * time.Second

// Executor executes a task and writes output to the provided captures.
// onStart, if non-nil, is called once the process has started with the PID
// of the process-group leader (before Execute blocks on completion) so the
// caller can register it (e.g. worker.ProcessRegistry) for pause/resume/kill.
type Executor interface {
	Execute(ctx context.Context, task *models.Task, stdout, stderr io.Writer, onStart func(pid int)) error
}

type TaskExecutor struct {
	// Sudo gates whether tasks may run with sudo. Shared with the
	// coordinator so a live toggle affects both create-time gating and
	// exec-time refusal. A nil gate denies sudo.
	Sudo *SudoGate
}

// Execute runs task.Command through a shell. This is the single execution
// path — there is no task "type" and no JSON args; the command line is the
// whole task (FRD §6/§9).
func (te *TaskExecutor) Execute(ctx context.Context, task *models.Task, stdout, stderr io.Writer, onStart func(pid int)) error {
	if strings.TrimSpace(task.Command) == "" {
		return errors.New("task has no command")
	}
	cmd, err := te.buildCmd(ctx, task.Sudo, task.Command)
	if err != nil {
		return err
	}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	if onStart != nil {
		onStart(cmd.Process.Pid)
	}
	return cmd.Wait()
}

func (te *TaskExecutor) buildCmd(ctx context.Context, sudo bool, command string) (*exec.Cmd, error) {
	if sudo && !te.Sudo.Allowed() {
		return nil, errors.New("task requests sudo but allow_sudo is disabled")
	}
	var cmd *exec.Cmd
	if sudo {
		cmd = exec.CommandContext(ctx, "sudo", "sh", "-c", command)
	} else {
		cmd = exec.CommandContext(ctx, "/bin/sh", "-c", command)
	}
	cmd.WaitDelay = waitDelay
	// Setpgid puts the command (and any children `sh -c` spawns) in their own
	// process group, so pause/resume/kill can signal the whole group as a
	// unit via the negative PID rather than just the shell itself.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if sudo {
		// CommandContext's default cancel (os.Process.Kill, i.e. SIGKILL on
		// just the shell) does two things wrong for a sudo child: it doesn't
		// reach the process group, and it's an unprivileged kill against a
		// root-owned process — EPERM. Escalate via `sudo kill` instead, same
		// as ProcessRegistry's Suspend/Resume. Homelab-only workaround
		// (allow_sudo-gated); the future curated setuid helper replaces this
		// shell-out.
		cmd.Cancel = func() error {
			return sendGroupSignal(cmd.Process.Pid, true, syscall.SIGKILL)
		}
	}
	return cmd, nil
}

// sendGroupSignal signals the process group led by pid (i.e. syscall.Kill
// with the negated pid). A sudo-launched task's group is root-owned, so an
// unprivileged syscall.Kill fails with EPERM — escalate via `sudo kill`
// instead. Shelling out per-signal is acceptable for a homelab; the curated
// setuid helper (future work, see taskmaster-future-ssh notes) replaces this.
func sendGroupSignal(pid int, sudo bool, sig syscall.Signal) error {
	if !sudo {
		return syscall.Kill(-pid, sig)
	}
	name := sigName(sig)
	out, err := exec.Command("sudo", "kill", "-"+name, "--", fmt.Sprintf("-%d", pid)).CombinedOutput()
	if err != nil {
		return fmt.Errorf("sudo kill -%s: %w: %s", name, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func sigName(sig syscall.Signal) string {
	switch sig {
	case syscall.SIGSTOP:
		return "STOP"
	case syscall.SIGCONT:
		return "CONT"
	case syscall.SIGKILL:
		return "KILL"
	default:
		return sig.String()
	}
}
