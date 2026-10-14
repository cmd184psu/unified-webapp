package opencode

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"cmd184psu/unified-webapp/internal/agentflow/runner"
	"cmd184psu/unified-webapp/internal/agentflow/sandbox"
)

// CommandFactory builds the process for one run. argv[0] is the opencode
// binary, env the child environment (KEY=VALUE). The production factory is
// SandboxCommand; a nil Adapter.Command runs argv directly in req.Workdir.
type CommandFactory func(ctx context.Context, req runner.Request, argv, env []string) (*exec.Cmd, error)

// Adapter is the opencode runner.Runner.
type Adapter struct {
	// Path is the opencode binary: an absolute path or a name on PATH (OC1).
	// It must be the real binary, not a HOME-relative shim (spike findings).
	Path string
	// Command builds the process; nil runs it directly without a sandbox.
	Command CommandFactory
	// ParentEnv is where PATH, LANG and key variables are taken from; nil means
	// os.Environ.
	ParentEnv []string
	// KeyEnvs names the AGENTFLOW_KEY_* variables to forward from ParentEnv.
	KeyEnvs []string
}

var _ runner.Runner = (*Adapter)(nil)

const stderrTail = 4 << 10

func (a *Adapter) path() (string, error) {
	p := a.Path
	if p == "" {
		p = "opencode"
	}
	resolved, err := exec.LookPath(p)
	if err != nil {
		return "", fmt.Errorf("opencode: binary not found: %w", err)
	}
	return resolved, nil
}

func (a *Adapter) parentEnv() []string {
	if a.ParentEnv != nil {
		return a.ParentEnv
	}
	return os.Environ()
}

func (a *Adapter) versionEnv() []string {
	var env []string
	for _, kv := range a.parentEnv() {
		if strings.HasPrefix(kv, "PATH=") || strings.HasPrefix(kv, "LANG=") {
			env = append(env, kv)
		}
	}
	return env
}

// Argv returns the opencode command line for req. The prompt is never part of
// it (OC2, S1): it goes on stdin, and no message argument is passed.
func Argv(path string, req runner.Request) []string {
	argv := []string{path, "run", "--pure", "--format", "json", "--auto",
		"--agent", AgentName(req.Role),
		"--model", ModelRef(req.Backend.Name, req.Model)}
	if req.SessionID != "" {
		argv = append(argv, "--session", req.SessionID)
	}
	return argv
}

// Run executes one model call. It reads req.PromptPath onto the child's stdin,
// appends the raw events to <StageDir>/events.jsonl, streams parsed events to
// req.OnEvent and classifies failures into *runner.Error (S5).
func (a *Adapter) Run(ctx context.Context, req runner.Request) (runner.Result, error) {
	var res runner.Result
	if req.PromptPath == "" || req.StageDir == "" || req.Workdir == "" || req.Role == "" {
		return res, errors.New("opencode: Role, Workdir, StageDir and PromptPath are required")
	}
	path, err := a.path()
	if err != nil {
		return res, &runner.Error{Class: runner.ErrCrash, Msg: err.Error(), Cause: err}
	}
	prompt, err := os.Open(req.PromptPath)
	if err != nil {
		return res, fmt.Errorf("opencode: open prompt: %w", err)
	}
	defer prompt.Close()
	if err := os.MkdirAll(HomeDir(req.StageDir), 0o755); err != nil {
		return res, fmt.Errorf("opencode: stage home: %w", err)
	}
	events, err := os.OpenFile(filepath.Join(req.StageDir, "events.jsonl"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return res, fmt.Errorf("opencode: events log: %w", err)
	}
	defer events.Close()

	argv := Argv(path, req)
	env := append(IsolationEnv(req.StageDir, a.parentEnv(), a.KeyEnvs), req.Env...)
	var cmd *exec.Cmd
	if a.Command != nil {
		cmd, err = a.Command(ctx, req, argv, env)
		if err != nil {
			return res, fmt.Errorf("opencode: build command: %w", err)
		}
	} else {
		cmd = exec.CommandContext(ctx, argv[0], argv[1:]...)
		cmd.Dir = req.Workdir
		cmd.Env = env
	}
	// Always set stdin explicitly: an inherited open pipe hangs opencode (S1).
	cmd.Stdin = prompt
	tail := &tailBuffer{max: stderrTail}
	cmd.Stderr = tail
	if cmd.WaitDelay == 0 {
		cmd.WaitDelay = 5 * time.Second
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return res, err
	}
	if err := cmd.Start(); err != nil {
		return res, &runner.Error{Class: runner.ErrCrash, Msg: "start: " + err.Error(), Cause: err}
	}
	out, parseErr := parseStream(stdout, events, req.OnEvent)
	if parseErr != nil {
		// Reading failed: stop the child so Wait cannot block on a full pipe.
		_, _ = io.Copy(io.Discard, stdout)
	}
	waitErr := cmd.Wait()

	res = runner.Result{Final: out.final, Events: out.events, Usage: out.usagePtr(), SessionID: out.sessionID}
	fail := func(class runner.ErrClass, msg string, cause error) (runner.Result, error) {
		res.ErrClass = class
		return res, &runner.Error{Class: class, Msg: msg, Cause: cause}
	}
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return fail(runner.ErrTimeout, "deadline exceeded", ctx.Err())
	case ctx.Err() != nil:
		return res, ctx.Err()
	case parseErr != nil:
		return fail(runner.ErrCrash, "read events: "+parseErr.Error(), parseErr)
	case out.hasError:
		return fail(out.errorClass(), out.errMessage, waitErr)
	case waitErr != nil:
		return fail(runner.ErrCrash, exitMsg(waitErr, tail.String()), waitErr)
	case len(out.events) == 0:
		// Exit 0 with no event at all: the model never answered (S5).
		return fail(runner.ErrTimeout, "no events received", nil)
	case out.final == "":
		// Nothing failed but there is no answer, e.g. an ask permission was
		// rejected and the loop stopped with reason tool-calls (S2, S5).
		res.Incomplete = true
	}
	return res, nil
}

func exitMsg(err error, stderr string) string {
	msg := err.Error()
	if s := strings.TrimSpace(stderr); s != "" {
		msg += ": " + s
	}
	return msg
}

// tailBuffer keeps the last max bytes written to it.
type tailBuffer struct {
	max int
	buf bytes.Buffer
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.buf.Write(p)
	if over := t.buf.Len() - t.max; over > 0 {
		t.buf.Next(over)
	}
	return len(p), nil
}

func (t *tailBuffer) String() string { return t.buf.String() }

// GitDirs returns the main repository's .git directory and the task
// worktree's admin directory (<repo>/.git/worktrees/<id>) for a request.
type GitDirs func(req runner.Request) (repoGitDir, worktreeGitDir string, err error)

// sandboxBin is where the real opencode binary is mounted read-only inside the
// sandbox.
const sandboxBin = "/sandbox/bin/opencode"

// installSandboxConfig copies the stage's frozen config to where opencode finds
// it inside the sandbox: <stage>/home/.config/opencode, which the sandbox maps
// to $HOME/.config. opencode writes next to its config at start-up, so it
// cannot be bound read-only. The copy is refreshed on every run.
func installSandboxConfig(stageDir string) error {
	data, err := os.ReadFile(ConfigPath(stageDir))
	if err != nil {
		return fmt.Errorf("opencode: frozen stage config: %w", err)
	}
	dir := filepath.Join(HomeDir(stageDir), ".config", "opencode")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "opencode.json"), data, 0o644)
}

// SandboxCommand returns the production CommandFactory: it runs opencode in a
// bwrap sandbox (B8) with the real binary bound read-only, the stage home bound
// as HOME (holding a copy of the frozen config) and the host network shared so
// the model backend is reachable. Only the worker role gets a writable
// worktree.
func SandboxCommand(gitDirs GitDirs) CommandFactory {
	return func(ctx context.Context, req runner.Request, argv, env []string) (*exec.Cmd, error) {
		repo, wt, err := gitDirs(req)
		if err != nil {
			return nil, err
		}
		if err := installSandboxConfig(req.StageDir); err != nil {
			return nil, err
		}
		bin, rest := argv[0], argv[1:]
		sbEnv := map[string]string{}
		for _, kv := range env {
			k, v, _ := strings.Cut(kv, "=")
			// HOME, PATH and the XDG dirs are the sandbox's own; the stage
			// home is bound at the sandbox HOME.
			if k == "HOME" || k == "PATH" || strings.HasPrefix(k, "XDG_") {
				continue
			}
			sbEnv[k] = v
		}
		return sandbox.Command(ctx, sandbox.Spec{
			Worktree:         req.Workdir,
			ReadOnlyWorktree: !writesWorktree(req.Role),
			RepoGitDir:       repo,
			WorktreeGitDir:   wt,
			StageHome:        HomeDir(req.StageDir),
			Net:              true,
			ExtraROBinds:     []sandbox.Bind{{Src: bin, Dst: sandboxBin}},
			Env:              sbEnv,
			Argv:             append([]string{sandboxBin}, rest...),
		})
	}
}
