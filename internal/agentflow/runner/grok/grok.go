// Package grok is the runner.Runner for the Grok CLI (`grok`), which runs on the
// user's own Grok plan. Stages run headless with `--prompt-file` and
// `--output-format streaming-json`. Grok keeps its own login and brings its own
// sandbox (Landlock); agentflow never copies a credential. The stage runs in
// the task worktree under the "agentflow" profile (workspace access, other
// secrets denied) for roles that write, and "agentflow-ro" (read-only) for the
// rest. Both profiles must exist in ~/.grok/sandbox.toml (SandboxProfiles is the text; agentflow never writes it).
package grok

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
	"strings"
	"time"

	"cmd184psu/unified-webapp/internal/agentflow/runner"
)

// APIType is the backend api type this adapter serves.
const APIType = "grok"

// Profile names in ~/.grok/sandbox.toml.
const (
	ProfileWrite = "agentflow"
	ProfileRead  = "agentflow-ro"
)

// SandboxProfiles is the text of the sandbox profiles the adapter relies on.
// Grok's own login (auth.json) has to stay readable inside its sandbox, so it
// is not denied; other secrets are.
const SandboxProfiles = `# Written for agentflow: its Grok stages run with --sandbox agentflow (or agentflow-ro).
# Workspace access, but the model's tools cannot read other secrets. grok's own
# login (auth.json) must stay readable: grok reads it inside the sandbox.
[profiles.agentflow]
extends = "workspace"
deny = ["~/.ssh", "~/.local/share/opencode/auth.json", "~/.unified_common.pin", "**/.env"]

[profiles.agentflow-ro]
extends = "read-only"
deny = ["~/.ssh", "~/.local/share/opencode/auth.json", "~/.unified_common.pin", "**/.env"]
`

// Adapter is the Grok CLI runner.Runner.
type Adapter struct {
	// Path is the grok binary: an absolute path or a name on PATH.
	Path string
	// PathFunc, when set, is asked on every run and replaces Path.
	PathFunc func() string
	// ParentEnv is where PATH, HOME and LANG are taken from; nil means os.Environ.
	ParentEnv []string
	// MaxTurns bounds one stage; 0 means 40.
	MaxTurns int
}

var _ runner.Runner = (*Adapter)(nil)

func (a *Adapter) path() (string, error) {
	p := a.Path
	if a.PathFunc != nil {
		p = a.PathFunc()
	}
	if p == "" {
		p = "grok"
	}
	resolved, err := exec.LookPath(p)
	if err != nil {
		// The service's PATH often lacks the user's tool dirs: try where grok installs itself.
		if home, herr := os.UserHomeDir(); herr == nil && p == "grok" {
			if alt := filepath.Join(home, ".grok", "bin", "grok"); fileExists(alt) {
				return alt, nil
			}
		}
		return "", fmt.Errorf("grok: binary not found: %w", err)
	}
	return resolved, nil
}

func (a *Adapter) parentEnv() []string {
	if a.ParentEnv != nil {
		return a.ParentEnv
	}
	return os.Environ()
}

// env is the child environment: no agentflow keys, nothing but what grok needs.
func (a *Adapter) env(req runner.Request) []string {
	var env []string
	for _, kv := range a.parentEnv() {
		for _, p := range []string{"PATH=", "HOME=", "LANG=", "LC_ALL=", "TERM=", "XDG_RUNTIME_DIR="} {
			if strings.HasPrefix(kv, p) {
				env = append(env, kv)
			}
		}
	}
	// Grok's sandbox refuses a symlinked grok home (version managers make ~/.grok one),
	// so point it at the real directory.
	for _, kv := range env {
		if strings.HasPrefix(kv, "HOME=") {
			dir := filepath.Join(strings.TrimPrefix(kv, "HOME="), ".grok")
			if real, err := filepath.EvalSymlinks(dir); err == nil && real != dir {
				env = append(env, "GROK_HOME="+real)
			}
		}
	}
	return append(env, req.Env...)
}

func fileExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir()
}

func writesWorktree(role string) bool { return role == "worker" || role == "cleanup" }

// Argv is the grok command line for req.
func (a *Adapter) Argv(path string, req runner.Request) []string {
	turns := a.MaxTurns
	if turns <= 0 {
		turns = 40
	}
	profile := ProfileRead
	if writesWorktree(req.Role) {
		profile = ProfileWrite
	}
	argv := []string{path,
		"--prompt-file", req.PromptPath,
		"--output-format", "streaming-json",
		"--cwd", req.Workdir,
		"-m", req.Model,
		"--max-turns", fmt.Sprint(turns),
		"--permission-mode", "bypassPermissions",
		"--no-subagents",
		"--disable-web-search",
		"--sandbox", profile,
	}
	if req.SessionID != "" {
		argv = append(argv, "--resume", req.SessionID)
	}
	return argv
}

// Run executes one model call and classifies its failures.
func (a *Adapter) Run(ctx context.Context, req runner.Request) (runner.Result, error) {
	var res runner.Result
	if req.PromptPath == "" || req.StageDir == "" || req.Workdir == "" || req.Role == "" || req.Model == "" {
		return res, errors.New("grok: Role, Model, Workdir, StageDir and PromptPath are required")
	}
	path, err := a.path()
	if err != nil {
		return res, &runner.Error{Class: runner.ErrCrash, Msg: err.Error(), Cause: err}
	}
	events, err := os.OpenFile(filepath.Join(req.StageDir, "events.jsonl"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return res, fmt.Errorf("grok: events log: %w", err)
	}
	defer events.Close()

	argv := a.Argv(path, req)
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = req.Workdir
	cmd.Env = a.env(req)
	cmd.Stdin = nil
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
	case out.errMsg != "":
		return fail(classifyMessage(out.errMsg), out.errMsg, waitErr)
	case waitErr != nil:
		msg := waitErr.Error()
		if s := strings.TrimSpace(tail.String()); s != "" {
			msg += ": " + s
			if c := classifyMessage(s); c != runner.ErrCrash {
				return fail(c, msg, waitErr)
			}
		}
		return fail(runner.ErrCrash, msg, waitErr)
	case len(out.events) == 0:
		return fail(runner.ErrTimeout, "no events received", nil)
	case out.stop != "" && out.stop != "end_turn":
		// Max turns or a refusal: whatever was said last is not a finished answer.
		res.Incomplete = true
	case out.final() == "":
		res.Incomplete = true
	}
	return res, nil
}

// Version runs `grok --version`.
func (a *Adapter) Version(ctx context.Context) (string, error) {
	path, err := a.path()
	if err != nil {
		return "", err
	}
	cmd := exec.CommandContext(ctx, path, "--version")
	cmd.Env = a.env(runner.Request{})
	b, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("grok: --version: %w", err)
	}
	return parseVersion(string(b))
}

var versionRe = regexp.MustCompile(`\d+\.\d+\.\d+\S*`)

func parseVersion(out string) (string, error) {
	if v := versionRe.FindString(out); v != "" {
		return v, nil
	}
	return "", fmt.Errorf("grok: cannot parse version from %q", strings.TrimSpace(out))
}

// BuiltinModels lists the models the login can use (`grok models`).
func (a *Adapter) BuiltinModels(ctx context.Context) ([]string, error) {
	path, err := a.path()
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, path, "models")
	cmd.Env = a.env(runner.Request{})
	b, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("grok: models: %w", err)
	}
	return parseModels(string(b)), nil
}

var modelRe = regexp.MustCompile(`^\s*(?:[*>-]\s*)?(grok-[A-Za-z0-9._-]+)`)

func parseModels(out string) []string {
	var models []string
	seen := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		if m := modelRe.FindStringSubmatch(line); m != nil && !seen[m[1]] {
			seen[m[1]] = true
			models = append(models, m[1])
		}
	}
	return models
}

var (
	rateRe  = regexp.MustCompile(`(?i)rate.?limit|usage limit|quota|too many requests|\b429\b|limit reached|out of (credits|usage)|try again (later|in)`)
	netRe   = regexp.MustCompile(`(?i)connection (refused|reset)|dns|no such host|timed? ?out|network|unreachable|ECONN|\b50[234]\b`)
	loginRe = regexp.MustCompile(`(?i)not signed in|log ?in|unauthori[sz]ed|\b401\b`)
)

func classifyMessage(msg string) runner.ErrClass {
	switch {
	case loginRe.MatchString(msg):
		return runner.ErrCrash
	case rateRe.MatchString(msg):
		return runner.ErrRateLimit
	case netRe.MatchString(msg):
		return runner.ErrUnreachable
	}
	return runner.ErrCrash
}

type streamOut struct {
	events    []runner.Event
	text      strings.Builder // text since the last tool call
	sessionID string
	stop      string
	errMsg    string
	usage     *runner.Usage
}

func (o *streamOut) final() string { return strings.TrimSpace(o.text.String()) }

type rawEvent struct {
	Type       string `json:"type"`
	Data       string `json:"data"`
	Message    string `json:"message"`
	StopReason string `json:"stopReason"`
	SessionID  string `json:"sessionId"`
	ToolName   string `json:"toolName"`
	Usage      *struct {
		In    int64 `json:"input_tokens"`
		Out   int64 `json:"output_tokens"`
		Total int64 `json:"total_tokens"`
	} `json:"usage"`
}

// parseStream reads grok's streaming-json: one JSON object per line. Text
// deltas after the last tool call make the final answer. The end event's usage
// is the stage total.
func parseStream(r io.Reader, log io.Writer, on func(runner.Event)) (*streamOut, error) {
	out := &streamOut{}
	br := bufio.NewReaderSize(r, 1<<20)
	for {
		line, err := br.ReadBytes('\n')
		if t := bytes.TrimSpace(line); len(t) > 0 {
			_, _ = log.Write(append(append([]byte{}, t...), '\n'))
			var ev rawEvent
			if json.Unmarshal(t, &ev) == nil {
				e := runner.Event{Type: ev.Type, Raw: append([]byte{}, t...)}
				switch ev.Type {
				case "text":
					out.text.WriteString(ev.Data)
					e.Text = ev.Data
				case "tool_call":
					out.text.Reset()
					e.Text = ev.ToolName
				case "error":
					out.errMsg = ev.Message
					e.Text = ev.Message
				case "end":
					out.stop = ev.StopReason
					out.sessionID = ev.SessionID
					if ev.Usage != nil {
						out.usage = &runner.Usage{InputTokens: ev.Usage.In, OutputTokens: ev.Usage.Out, TotalTokens: ev.Usage.Total}
					}
				}
				// available_commands repeats the whole tool list; keep it out of the stream.
				if ev.Type != "available_commands" {
					out.events = append(out.events, e)
					if on != nil {
						on(e)
					}
				}
			}
		}
		if err != nil {
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
