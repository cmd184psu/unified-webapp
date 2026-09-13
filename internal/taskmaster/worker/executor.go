package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
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
	AllowSudo bool
}

type execArgs struct {
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Workdir string            `json:"workdir"`
	Env     map[string]string `json:"env"`
}

type shellArgs struct {
	Shell string `json:"shell"`
}

type scriptArgs struct {
	Path string   `json:"path"`
	Args []string `json:"args"`
}

type migrationArgs struct {
	Name    string `json:"name"`
	Command string `json:"command"`
}

func (te *TaskExecutor) Execute(ctx context.Context, task *models.Task, stdout, stderr io.Writer) error {
	switch task.TaskType {
	case "exec":
		return te.executeExec(ctx, task, stdout, stderr)
	case "shell":
		return te.executeShell(ctx, task, stdout, stderr)
	case "script":
		return te.executeScript(ctx, task, stdout, stderr)
	case "migration":
		return te.executeMigration(ctx, task, stdout, stderr)
	default:
		return fmt.Errorf("unknown task_type: %q", task.TaskType)
	}
}

func (te *TaskExecutor) executeExec(ctx context.Context, task *models.Task, stdout, stderr io.Writer) error {
	var a execArgs
	if err := json.Unmarshal([]byte(task.Args), &a); err != nil {
		return fmt.Errorf("parse exec args: %w", err)
	}
	args := a.Args
	cmd, err := te.buildCmd(ctx, task.Sudo, a.Command, args...)
	if err != nil {
		return err
	}
	if a.Workdir != "" {
		cmd.Dir = a.Workdir
	}
	for k, v := range a.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}

func (te *TaskExecutor) executeShell(ctx context.Context, task *models.Task, stdout, stderr io.Writer) error {
	var a shellArgs
	if err := json.Unmarshal([]byte(task.Args), &a); err != nil {
		return fmt.Errorf(`parse shell args: %w — expected {"shell":"command"}`, err)
	}
	if a.Shell == "" {
		return fmt.Errorf(`shell args missing "shell" field — expected {"shell":"command"}`)
	}
	cmd, err := te.buildCmd(ctx, task.Sudo, "/bin/sh", "-c", a.Shell)
	if err != nil {
		return err
	}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}

func (te *TaskExecutor) executeScript(ctx context.Context, task *models.Task, stdout, stderr io.Writer) error {
	var a scriptArgs
	if err := json.Unmarshal([]byte(task.Args), &a); err != nil {
		return fmt.Errorf("parse script args: %w", err)
	}
	cmd, err := te.buildCmd(ctx, task.Sudo, a.Path, a.Args...)
	if err != nil {
		return err
	}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}

func (te *TaskExecutor) executeMigration(ctx context.Context, task *models.Task, stdout, stderr io.Writer) error {
	var a migrationArgs
	if err := json.Unmarshal([]byte(task.Args), &a); err != nil {
		return fmt.Errorf("parse migration args: %w", err)
	}
	migrateCmd := "migrate"
	if a.Command != "" {
		migrateCmd = a.Command
	}
	cmd, err := te.buildCmd(ctx, task.Sudo, migrateCmd, "--name", a.Name)
	if err != nil {
		return err
	}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}

func (te *TaskExecutor) buildCmd(ctx context.Context, sudo bool, name string, args ...string) (*exec.Cmd, error) {
	if sudo && !te.AllowSudo {
		return nil, errors.New("task requests sudo but allow_sudo is disabled in taskmaster config")
	}
	var cmd *exec.Cmd
	if sudo {
		cmd = exec.CommandContext(ctx, "sudo", append([]string{name}, args...)...)
	} else {
		cmd = exec.CommandContext(ctx, name, args...)
	}
	cmd.WaitDelay = waitDelay
	return cmd, nil
}
