package worker

import (
	"context"
	"errors"
	"io"
	"os/exec"
	"strings"
	"time"

	"cmd184psu/unified-webapp/internal/taskmaster/models"
)

// waitDelay bounds cmd.Wait after ctx cancel even when (a) a backgrounded
// grandchild inherits the stdout/stderr pipes and never EOFs them, or
// (b) a sudo child is root-owned and our unprivileged kill gets EPERM.
// After cancel+waitDelay, Wait force-closes the pipes and returns.
const waitDelay = 10 * time.Second

// Executor executes a task and writes output to the provided captures.
type Executor interface {
	Execute(ctx context.Context, task *models.Task, stdout, stderr io.Writer) error
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
func (te *TaskExecutor) Execute(ctx context.Context, task *models.Task, stdout, stderr io.Writer) error {
	if strings.TrimSpace(task.Command) == "" {
		return errors.New("task has no command")
	}
	cmd, err := te.buildCmd(ctx, task.Sudo, task.Command)
	if err != nil {
		return err
	}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
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
	return cmd, nil
}
