// Package agy is the runner.Runner for Google's Antigravity CLI (`agy`), which runs on the
// user's own Google login. Stages run headless in print mode with `--output-format
// stream-json` events on stdout. agy keeps its own login and brings its own terminal
// sandbox; agentflow never copies a credential. Each call gets a per-session settings
// object (`--settings`, never saved) that turns the sandbox on, denies reads of the user's
// secrets and of every .env file in the worktree, and for roles that do not write also
// denies writes to the worktree.
package agy

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
const APIType = "agy"

// Adapter is the Antigravity CLI runner.Runner.
type Adapter struct {
	// Path is the agy binary: an absolute path or a name on PATH.
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
		p = "agy"
	}
	if home, herr := os.UserHomeDir(); herr == nil && p == "agy" {
		if alt := filepath.Join(home, ".local", "bin", "agy"); fileExists(alt) {
			return alt, nil
		}
	}
	resolved, err := exec.LookPath(p)
	if err != nil {
		return "", fmt.Errorf("agy: binary not found: %w", err)
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
		for _, p := range []string{"PATH=", "HOME=", "LANG=", "LC_ALL=", "TERM=", "XDG_RUNTIME_DIR=", "XDG_CONFIG_HOME=", "XDG_DATA_HOME="} {
			if strings.HasPrefix(kv, p) {
				env = append(env, kv)
			}
		}
	}
	return append(env, req.Env...)
}

func writesWorktree(role string) bool { return role == "worker" || role == "cleanup" }

// maxInlinePrompt is the largest prompt passed on the command line; a longer one is read by the
// model from its file instead (one argument may not exceed 128 KB).
const maxInlinePrompt = 100_000

// secretPaths are the files and folders no model call may read: the user's SSH keys, other
// tools' logins, the service PIN and the Grok, Codex, Claude and agy logins themselves. Only
// paths that exist are listed, and a symlink is resolved to its real path first.
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
		filepath.Join(home, ".gemini"),
	} {
		if real, err := filepath.EvalSymlinks(p); err == nil {
			out = append(out, real)
		}
	}
	return out
}

// envFiles lists the .env and .env.* files under dir (skipping .git and node_modules, and going
// no deeper than six levels), because the permission rules take paths, not globs.
func envFiles(dir string) []string {
	var out []string
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if n := d.Name(); n == ".git" || n == "node_modules" {
				return filepath.SkipDir
			}
			if rel, rerr := filepath.Rel(dir, p); rerr == nil && strings.Count(rel, string(filepath.Separator)) >= 6 {
				return filepath.SkipDir
			}
			return nil
		}
		if n := d.Name(); n == ".env" || strings.HasPrefix(n, ".env.") {
			out = append(out, p)
		}
		return nil
	})
	return out
}

// settingsJSON is the --settings value for one call.
func settingsJSON(role, workdir, readable string) string {
	deny := []string{}
	for _, p := range secretPaths() {
		deny = append(deny, "read_file("+p+")")
	}
	for _, p := range envFiles(workdir) {
		deny = append(deny, "read_file("+p+")")
	}
	if !writesWorktree(role) && workdir != "" {
		deny = append(deny, "write_file("+workdir+")")
	}
	perms := map[string]any{"deny": deny}
	if readable != "" {
		perms["allow"] = []string{"read_file(" + readable + ")"}
	}
	b, _ := json.Marshal(map[string]any{
		"enableTerminalSandbox": true,
		"toolPermission":        "proceed-in-sandbox",
		"permissions":           perms,
	})
	return string(b)
}

// prompt is what goes after -p=: the prompt itself, or when it is too long for an argument a
// request to read it from its file (readable is then that file, allowed in the settings).
func prompt(path string) (text, readable string, err error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", "", err
	}
	if len(b) <= maxInlinePrompt {
		return string(b), "", nil
	}
	return "Your full instructions are in the file " + path + ". Read it completely, then carry them out exactly and answer in the format it asks for.", path, nil
}

// Argv is the agy command line for req, given the prompt text and the settings.
func (a *Adapter) Argv(path string, req runner.Request, promptText, settings string) []string {
	argv := []string{path, "--output-format", "stream-json",
		"--model", req.Model,
		"--mode", "accept-edits", "--sandbox",
		"--settings", settings,
		"--add-dir", req.Workdir,
	}
	if req.SessionID != "" {
		argv = append(argv, "--conversation", req.SessionID)
	}
	return append(argv, "-p="+promptText)
}

// Run executes one model call and classifies its failures.
func (a *Adapter) Run(ctx context.Context, req runner.Request) (runner.Result, error) {
	var res runner.Result
	if req.PromptPath == "" || req.StageDir == "" || req.Workdir == "" || req.Role == "" || req.Model == "" {
		return res, errors.New("agy: Role, Model, Workdir, StageDir and PromptPath are required")
	}
	path, err := a.path()
	if err != nil {
		return res, &runner.Error{Class: runner.ErrCrash, Msg: err.Error(), Cause: err}
	}
	text, readable, err := prompt(req.PromptPath)
	if err != nil {
		return res, fmt.Errorf("agy: read prompt: %w", err)
	}
	events, err := os.OpenFile(filepath.Join(req.StageDir, "events.jsonl"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return res, fmt.Errorf("agy: events log: %w", err)
	}
	defer events.Close()

	argv := a.Argv(path, req, text, settingsJSON(req.Role, req.Workdir, readable))
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = req.Workdir
	cmd.Env = a.env(req)
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

// Version runs `agy --version`.
func (a *Adapter) Version(ctx context.Context) (string, error) {
	path, err := a.path()
	if err != nil {
		return "", err
	}
	cmd := exec.CommandContext(ctx, path, "--version")
	cmd.Env = a.env(runner.Request{})
	b, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("agy: --version: %w", err)
	}
	return parseVersion(string(b))
}

var versionRe = regexp.MustCompile(`\d+\.\d+\.\d+\S*`)

func parseVersion(out string) (string, error) {
	if v := versionRe.FindString(out); v != "" {
		return v, nil
	}
	return "", fmt.Errorf("agy: cannot parse version from %q", strings.TrimSpace(out))
}

// BuiltinModels lists the models the login offers (`agy models`: an id, a tab, a label per line).
func (a *Adapter) BuiltinModels(ctx context.Context) ([]string, error) {
	path, err := a.path()
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, path, "models")
	cmd.Env = a.env(runner.Request{})
	b, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("agy: models: %w", err)
	}
	return parseModels(b), nil
}

func parseModels(b []byte) []string {
	var out []string
	for _, line := range strings.Split(string(b), "\n") {
		id, _, found := strings.Cut(line, "\t")
		if found && strings.TrimSpace(id) != "" {
			out = append(out, strings.TrimSpace(id))
		}
	}
	return out
}

type streamOut struct {
	events    []runner.Event
	text      string // the run's final answer
	sessionID string
	errMsg    string
	completed bool
	usage     *runner.Usage
}

func (o *streamOut) final() string { return strings.TrimSpace(o.text) }

type rawEvent struct {
	Event string `json:"event"`
	Init  *struct {
		ConversationID string `json:"conversation_id"`
	} `json:"init"`
	StepUpdate *struct {
		StepType  string `json:"step_type"`
		TextDelta string `json:"text_delta"`
	} `json:"step_update"`
	Result *struct {
		ConversationID string `json:"conversation_id"`
		Status         string `json:"status"`
		Response       string `json:"response"`
		Error          string `json:"error"`
		Usage          *struct {
			In    int64 `json:"input_tokens"`
			Out   int64 `json:"output_tokens"`
			Cache int64 `json:"cache_read_tokens"`
		} `json:"usage"`
	} `json:"result"`
}

// parseStream reads agy's stream-json events, one object per line. The final answer is the
// result event's response; a result whose status is not SUCCESS is the failure.
func parseStream(r io.Reader, log io.Writer, on func(runner.Event)) (*streamOut, error) {
	out := &streamOut{}
	br := bufio.NewReaderSize(r, 1<<20)
	for {
		line, err := br.ReadBytes('\n')
		if t := bytes.TrimSpace(line); len(t) > 0 && t[0] == '{' {
			_, _ = log.Write(append(append([]byte{}, t...), '\n'))
			var ev rawEvent
			if json.Unmarshal(t, &ev) == nil && ev.Event != "" {
				e := runner.Event{Type: ev.Event, Raw: append([]byte{}, t...)}
				switch ev.Event {
				case "init":
					if ev.Init != nil {
						out.sessionID = ev.Init.ConversationID
					}
				case "step_update":
					if ev.StepUpdate != nil && ev.StepUpdate.StepType == "agent_response" {
						e.Text = ev.StepUpdate.TextDelta
					}
				case "result":
					if ev.Result != nil {
						if ev.Result.ConversationID != "" {
							out.sessionID = ev.Result.ConversationID
						}
						if ev.Result.Status == "SUCCESS" {
							out.completed = true
							out.text = ev.Result.Response
						} else {
							out.errMsg = strings.TrimSpace(ev.Result.Error)
							if out.errMsg == "" {
								out.errMsg = "run ended: " + ev.Result.Status
							}
							e.Text = out.errMsg
						}
						if u := ev.Result.Usage; u != nil {
							in := u.In + u.Cache
							out.usage = &runner.Usage{InputTokens: in, OutputTokens: u.Out, TotalTokens: in + u.Out}
						}
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
