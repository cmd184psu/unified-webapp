// Package claude is the runner.Runner for the Claude Code CLI (`claude -p`), which runs on
// the user's own Claude login. Stages run headless with the prompt on stdin and
// `--output-format stream-json` events on stdout. Claude Code keeps its own login and
// brings its own sandbox; agentflow never copies a credential. Roles that write (worker,
// cleanup) get the edit tools and a sandbox that can write the worktree, every other role
// read-only tools and a sandbox that cannot, both with the user's secrets denied.
package claude

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
const APIType = "claude"

// Adapter is the Claude Code CLI runner.Runner.
type Adapter struct {
	// Path is the claude binary: an absolute path or a name on PATH.
	Path string
	// PathFunc, when set, is asked on every run and replaces Path.
	PathFunc func() string
	// ParentEnv is where PATH, HOME and LANG are taken from; nil means os.Environ.
	ParentEnv []string
}

var _ runner.Runner = (*Adapter)(nil)

func (a *Adapter) path() (string, error) {
	p := a.Path
	if a.PathFunc != nil {
		p = a.PathFunc()
	}
	if p == "" {
		p = "claude"
	}
	if home, herr := os.UserHomeDir(); herr == nil && p == "claude" {
		if alt := filepath.Join(home, ".local", "bin", "claude"); fileExists(alt) {
			return alt, nil
		}
	}
	resolved, err := exec.LookPath(p)
	if err != nil {
		return "", fmt.Errorf("claude: binary not found: %w", err)
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

// env passes only what the CLI needs to find its login and its binaries.
func (a *Adapter) env(req runner.Request) []string {
	var env []string
	for _, kv := range a.parentEnv() {
		for _, p := range []string{"PATH=", "HOME=", "LANG=", "LC_ALL=", "TERM=", "XDG_RUNTIME_DIR=", "CLAUDE_CONFIG_DIR="} {
			if strings.HasPrefix(kv, p) {
				env = append(env, kv)
			}
		}
	}
	return append(env, req.Env...)
}

func writesWorktree(role string) bool { return role == "worker" || role == "cleanup" }

// secretPaths are the files and folders no model call may read: the user's SSH keys,
// other tools' logins, the service PIN and the Grok, Codex and Claude logins themselves.
// Only paths that exist are listed, and a symlink is resolved to its real path first.
func secretPaths() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	var out []string
	for _, p := range []string{
		filepath.Join(home, ".ssh"),
		filepath.Join(home, ".local", "share", "opencode", "auth.json"),
		filepath.Join(home, ".unified_common.pin"),
		filepath.Join(home, ".grok", "auth.json"),
		filepath.Join(home, ".codex", "auth.json"),
		filepath.Join(home, ".claude", ".credentials.json"),
	} {
		if real, err := filepath.EvalSymlinks(p); err == nil {
			out = append(out, real)
		}
	}
	return out
}

// settingsJSON is the --settings value for one call: the sandbox on and not escapable, the
// secrets unreadable both to commands (sandbox) and to the Read tool (permission rules),
// and for roles that do not write the worktree unwritable.
func settingsJSON(role, workdir string) string {
	deny := []string{}
	rules := []string{"Read(**/.env)"}
	for _, p := range secretPaths() {
		deny = append(deny, p)
		rules = append(rules, "Read(/"+p+")", "Read(/"+p+"/**)")
	}
	fs := map[string]any{"denyRead": deny}
	if !writesWorktree(role) && workdir != "" {
		fs["denyWrite"] = []string{workdir}
	}
	b, _ := json.Marshal(map[string]any{
		"sandbox": map[string]any{
			"enabled": true, "autoAllowBashIfSandboxed": true, "allowUnsandboxedCommands": false, "failIfUnavailable": true,
			"filesystem": fs,
		},
		"permissions": map[string]any{"deny": rules},
	})
	return string(b)
}

// Argv is the claude command line for req. The prompt goes on stdin.
func (a *Adapter) Argv(path string, req runner.Request) []string {
	tools, mode := "Bash,Read,Glob,Grep", "dontAsk"
	if writesWorktree(req.Role) {
		tools, mode = "Bash,Read,Edit,Write,Glob,Grep", "acceptEdits"
	}
	argv := []string{path, "-p", "--output-format", "stream-json", "--verbose",
		"--model", req.Model,
		"--setting-sources", "local", "--strict-mcp-config",
		"--settings", settingsJSON(req.Role, req.Workdir),
		"--tools", tools, "--permission-mode", mode,
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
		return res, errors.New("claude: Role, Model, Workdir, StageDir and PromptPath are required")
	}
	path, err := a.path()
	if err != nil {
		return res, &runner.Error{Class: runner.ErrCrash, Msg: err.Error(), Cause: err}
	}
	prompt, err := os.Open(req.PromptPath)
	if err != nil {
		return res, fmt.Errorf("claude: open prompt: %w", err)
	}
	defer prompt.Close()
	events, err := os.OpenFile(filepath.Join(req.StageDir, "events.jsonl"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return res, fmt.Errorf("claude: events log: %w", err)
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
	case out.errMsg != "":
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

// Version runs `claude --version`.
func (a *Adapter) Version(ctx context.Context) (string, error) {
	path, err := a.path()
	if err != nil {
		return "", err
	}
	cmd := exec.CommandContext(ctx, path, "--version")
	cmd.Env = a.env(runner.Request{})
	b, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("claude: --version: %w", err)
	}
	return parseVersion(string(b))
}

var versionRe = regexp.MustCompile(`\d+\.\d+\.\d+\S*`)

func parseVersion(out string) (string, error) {
	if v := versionRe.FindString(out); v != "" {
		return v, nil
	}
	return "", fmt.Errorf("claude: cannot parse version from %q", strings.TrimSpace(out))
}

// BuiltinModels lists the models agentflow offers on the Claude login. The CLI has no
// model listing, so the list is fixed; pick the ones to use when adding the backend.
func (a *Adapter) BuiltinModels(ctx context.Context) ([]string, error) {
	return []string{"claude-haiku-5-5", "claude-sonnet-5-5", "claude-opus-5-5"}, nil
}

type streamOut struct {
	events    []runner.Event
	text      string // last assistant text since the last tool call
	result    string
	sessionID string
	errMsg    string
	completed bool
	usage     *runner.Usage
}

func (o *streamOut) final() string {
	if o.result != "" {
		return strings.TrimSpace(o.result)
	}
	return strings.TrimSpace(o.text)
}

type rawEvent struct {
	Type      string `json:"type"`
	Subtype   string `json:"subtype"`
	SessionID string `json:"session_id"`
	IsError   bool   `json:"is_error"`
	Result    string `json:"result"`
	Message   *struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"message"`
	Usage *struct {
		In      int64 `json:"input_tokens"`
		Created int64 `json:"cache_creation_input_tokens"`
		Read    int64 `json:"cache_read_input_tokens"`
		Out     int64 `json:"output_tokens"`
	} `json:"usage"`
}

// parseStream reads claude's stream-json events, one object per line. The final answer is the
// result event's text; a result flagged as an error is the failure.
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
				if ev.SessionID != "" {
					out.sessionID = ev.SessionID
				}
				switch ev.Type {
				case "assistant":
					if ev.Message != nil {
						text, tool := "", false
						for _, c := range ev.Message.Content {
							switch c.Type {
							case "text":
								text += c.Text
							case "tool_use":
								tool = true
							}
						}
						if tool {
							out.text = ""
						}
						if text != "" {
							out.text = text
							e.Text = text
						}
					}
				case "result":
					out.completed = true
					out.result = ev.Result
					if ev.IsError || strings.HasPrefix(ev.Subtype, "error") {
						out.completed = false
						out.errMsg = strings.TrimSpace(ev.Result)
						if out.errMsg == "" {
							out.errMsg = ev.Subtype
						}
						e.Text = out.errMsg
					}
					if ev.Usage != nil {
						in := ev.Usage.In + ev.Usage.Created + ev.Usage.Read
						out.usage = &runner.Usage{InputTokens: in, OutputTokens: ev.Usage.Out, TotalTokens: in + ev.Usage.Out}
					}
				}
				out.events = append(out.events, e)
				if on != nil {
					on(e)
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
