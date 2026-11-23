// Package codex is the runner.Runner for the Codex CLI (`codex exec`), which runs
// on the user's own ChatGPT login. Stages run headless with the prompt on stdin
// and `--json` events on stdout. Codex keeps its own login and brings its own
// sandbox; agentflow never copies a credential. Roles that write (worker,
// cleanup) run under Codex's workspace permissions profile, every other role under
// read-only, both with the user's secrets (SSH keys, other logins, .env files) denied.
package codex

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"cmd184psu/unified-webapp/internal/agentflow/runner"
)

// APIType is the backend api type this adapter serves.
const APIType = "codex"

// Adapter is the Codex CLI runner.Runner.
type Adapter struct {
	// Path is the codex binary: an absolute path or a name on PATH.
	Path string
	// PathFunc, when set, is asked on every run and replaces Path.
	PathFunc func() string
	// ParentEnv is where PATH, HOME and LANG are taken from; nil means os.Environ.
	ParentEnv []string
	// Effort is the reasoning effort passed to every call; empty means "high" (owner 11-24: sol 6.2 high is an opus-level model).
	Effort string
}

func (a *Adapter) effort() string {
	if a.Effort == "" {
		return "high"
	}
	return a.Effort
}

var _ runner.Runner = (*Adapter)(nil)

func (a *Adapter) path() (string, error) {
	p := a.Path
	if a.PathFunc != nil {
		p = a.PathFunc()
	}
	if p == "" {
		p = "codex"
	}
	// The service's PATH holds an older system codex; the user's own install is the one logged in
	// and kept current, so it comes first.
	if home, herr := os.UserHomeDir(); herr == nil && p == "codex" {
		if alt := filepath.Join(home, ".local", "bin", "codex"); fileExists(alt) {
			return alt, nil
		}
	}
	resolved, err := exec.LookPath(p)
	if err != nil {
		return "", fmt.Errorf("codex: binary not found: %w", err)
	}
	return resolved, nil
}

func fileExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir()
}

func (a *Adapter) parentEnv() []string {
	if a.ParentEnv != nil {
		return a.ParentEnv
	}
	return os.Environ()
}

// env is the child environment: no agentflow keys, nothing but what codex needs.
func (a *Adapter) env(req runner.Request) []string {
	var env []string
	for _, kv := range a.parentEnv() {
		for _, p := range []string{"PATH=", "HOME=", "LANG=", "LC_ALL=", "TERM=", "XDG_RUNTIME_DIR=", "CODEX_HOME="} {
			if strings.HasPrefix(kv, p) {
				env = append(env, kv)
			}
		}
	}
	return append(env, req.Env...)
}

func writesWorktree(role string) bool { return role == "worker" || role == "cleanup" }

// profileName is the permissions profile a role runs under.
func profileName(role string) string {
	if writesWorktree(role) {
		return "agentflow"
	}
	return "agentflow-ro"
}

// secretPaths are the files and folders no model call may read: the user's SSH keys,
// other tools' logins, the service PIN and the Grok and Codex logins themselves. Codex
// reads its own login outside the sandbox, so denying it costs nothing. Only paths that
// exist are listed (the sandbox cannot mount a deny over a missing path), and a symlink is
// resolved first (it cannot mount over one).
func secretPaths() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	codexHome := os.Getenv("CODEX_HOME")
	if codexHome == "" {
		codexHome = filepath.Join(home, ".codex")
	}
	var out []string
	for _, p := range []string{
		filepath.Join(home, ".ssh"),
		filepath.Join(home, ".local", "share", "opencode", "auth.json"),
		filepath.Join(home, ".unified_common.pin"),
		filepath.Join(home, ".grok", "auth.json"),
		filepath.Join(codexHome, "auth.json"),
	} {
		if real, err := filepath.EvalSymlinks(p); err == nil {
			out = append(out, real)
		}
	}
	return out
}

// permissionsConfig is the -c value that defines the role's profile: Codex's workspace
// (workers) or read-only (everyone else) profile, with the secrets denied.
func permissionsConfig(role string) string {
	parent := ":read-only"
	if writesWorktree(role) {
		parent = ":workspace"
	}
	var fs []string
	for _, p := range secretPaths() {
		fs = append(fs, strconv.Quote(p)+`="deny"`)
	}
	fs = append(fs, `":workspace_roots"={ "**/.env"="deny" }`, `glob_scan_max_depth=4`)
	return "permissions." + profileName(role) + `={ extends="` + parent + `", filesystem={ ` + strings.Join(fs, ", ") + ` } }`
}

// Argv is the codex command line for req. The prompt goes on stdin ("-").
func (a *Adapter) Argv(path string, req runner.Request) []string {
	argv := []string{path, "exec"}
	if req.SessionID != "" {
		argv = append(argv, "resume")
	}
	argv = append(argv,
		"--json", "--skip-git-repo-check", "--ignore-rules",
		"-m", req.Model,
		"-c", permissionsConfig(req.Role),
		"-c", `default_permissions="`+profileName(req.Role)+`"`,
		"-c", `approval_policy="never"`,
		"-c", `model_reasoning_effort="`+a.effort()+`"`,
	)
	if req.SessionID != "" {
		argv = append(argv, req.SessionID)
	} else {
		argv = append(argv, "-C", req.Workdir)
	}
	return append(argv, "-")
}

// Run executes one model call and classifies its failures.
func (a *Adapter) Run(ctx context.Context, req runner.Request) (runner.Result, error) {
	var res runner.Result
	if req.PromptPath == "" || req.StageDir == "" || req.Workdir == "" || req.Role == "" || req.Model == "" {
		return res, errors.New("codex: Role, Model, Workdir, StageDir and PromptPath are required")
	}
	path, err := a.path()
	if err != nil {
		return res, &runner.Error{Class: runner.ErrCrash, Msg: err.Error(), Cause: err}
	}
	prompt, err := os.Open(req.PromptPath)
	if err != nil {
		return res, fmt.Errorf("codex: open prompt: %w", err)
	}
	defer prompt.Close()
	events, err := os.OpenFile(filepath.Join(req.StageDir, "events.jsonl"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return res, fmt.Errorf("codex: events log: %w", err)
	}
	defer events.Close()

	argv := a.Argv(path, req)
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = req.Workdir
	cmd.Env = a.env(req)
	cmd.Stdin = prompt
	tail := &tailBuffer{max: 4 << 10}
	cmd.Stderr = tail
	cmd.WaitDelay = 5 * time.Second
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return res, err
	}
	if err := cmd.Start(); err != nil {
		return res, &runner.Error{Class: runner.ErrCrash, Msg: "start: " + err.Error(), Cause: err}
	}
	out, parseErr := parseStream(stdout, events, req.OnEvent)
	if parseErr != nil {
		_, _ = io.Copy(io.Discard, stdout)
	}
	waitErr := cmd.Wait()

	res = runner.Result{Final: out.final(), Events: out.events, Usage: out.usage, SessionID: out.sessionID}
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
	case out.errMsg != "" && !out.completed: // an error event before a finished turn is a failure; a retry notice in a finished one is not
		return fail(runner.ClassifyMessage(out.errMsg), out.errMsg, waitErr)
	case waitErr != nil:
		msg := waitErr.Error()
		if s := strings.TrimSpace(tail.String()); s != "" {
			msg += ": " + s
			if c := runner.ClassifyMessage(s); c != runner.ErrCrash {
				return fail(c, msg, waitErr)
			}
		}
		return fail(runner.ErrCrash, msg, waitErr)
	case len(out.events) == 0:
		return fail(runner.ErrTimeout, "no events received", nil)
	case !out.completed, out.final() == "":
		res.Incomplete = true
	}
	return res, nil
}

// Version runs `codex --version`.
func (a *Adapter) Version(ctx context.Context) (string, error) {
	path, err := a.path()
	if err != nil {
		return "", err
	}
	cmd := exec.CommandContext(ctx, path, "--version")
	cmd.Env = a.env(runner.Request{})
	b, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("codex: --version: %w", err)
	}
	return parseVersion(string(b))
}

var versionRe = regexp.MustCompile(`\d+\.\d+\.\d+\S*`)

func parseVersion(out string) (string, error) {
	if v := versionRe.FindString(out); v != "" {
		return v, nil
	}
	return "", fmt.Errorf("codex: cannot parse version from %q", strings.TrimSpace(out))
}

// BuiltinModels lists the models the login offers (`codex debug models`).
func (a *Adapter) BuiltinModels(ctx context.Context) ([]string, error) {
	path, err := a.path()
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, path, "debug", "models")
	cmd.Env = a.env(runner.Request{})
	b, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("codex: debug models: %w", err)
	}
	return parseModels(b)
}

func parseModels(b []byte) ([]string, error) {
	var doc struct {
		Models []struct {
			Slug       string `json:"slug"`
			Visibility string `json:"visibility"`
		} `json:"models"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil, fmt.Errorf("codex: models: %w", err)
	}
	var out []string
	for _, m := range doc.Models {
		if m.Slug != "" && m.Visibility == "list" {
			out = append(out, m.Slug)
		}
	}
	return out, nil
}

type streamOut struct {
	events    []runner.Event
	text      string // last agent message since the last command
	sessionID string
	errMsg    string
	completed bool
	usage     *runner.Usage
}

func (o *streamOut) final() string { return strings.TrimSpace(o.text) }

type rawEvent struct {
	Type     string `json:"type"`
	ThreadID string `json:"thread_id"`
	Message  string `json:"message"`
	Error    *struct {
		Message string `json:"message"`
	} `json:"error"`
	Item *struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"item"`
	Usage *struct {
		In  int64 `json:"input_tokens"`
		Out int64 `json:"output_tokens"`
	} `json:"usage"`
}

// parseStream reads codex's --json events, one object per line. The final
// answer is the last agent message after the last command. A failed turn or
// an error event is the failure; an "error" item (a warning) is not.
func parseStream(r io.Reader, log io.Writer, on func(runner.Event)) (*streamOut, error) {
	out := &streamOut{}
	br := bufio.NewReaderSize(r, 1<<20)
	var in, outTok int64
	for {
		line, err := br.ReadBytes('\n')
		if t := bytes.TrimSpace(line); len(t) > 0 {
			_, _ = log.Write(append(append([]byte{}, t...), '\n'))
			var ev rawEvent
			if json.Unmarshal(t, &ev) == nil {
				e := runner.Event{Type: ev.Type, Raw: append([]byte{}, t...)}
				switch ev.Type {
				case "thread.started":
					out.sessionID = ev.ThreadID
				case "item.started":
					if ev.Item != nil && ev.Item.Type != "agent_message" {
						out.text = ""
					}
				case "item.completed":
					if ev.Item != nil && ev.Item.Type == "agent_message" {
						out.text = ev.Item.Text
						e.Text = ev.Item.Text
					}
				case "turn.completed":
					out.completed = true
					if ev.Usage != nil {
						in += ev.Usage.In
						outTok += ev.Usage.Out
					}
				case "turn.failed":
					out.errMsg = "turn failed"
					if ev.Error != nil && ev.Error.Message != "" {
						out.errMsg = ev.Error.Message
					}
					e.Text = out.errMsg
				case "error":
					if ev.Message != "" {
						out.errMsg = ev.Message
						e.Text = ev.Message
					}
				}
				out.events = append(out.events, e)
				if on != nil {
					on(e)
				}
			}
		}
		if err != nil {
			if in+outTok > 0 {
				out.usage = &runner.Usage{InputTokens: in, OutputTokens: outTok, TotalTokens: in + outTok}
			}
			if errors.Is(err, io.EOF) {
				return out, nil
			}
			return out, err
		}
	}
}

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
