// Package muse is the runner.Runner for the Muse Code CLI (`muse exec`), which runs on the
// user's own Muse login. Stages run headless with `--json` events on stdout. Muse keeps its
// own login and brings its own shell sandbox; agentflow never copies a credential. Roles that
// write (worker, cleanup) may write the worktree, every other role has Muse's write tools off.
// Muse offers no way to deny reads of chosen paths, so a model on this backend can read what
// the user can read: keep it off work that touches secrets.
package muse

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
const APIType = "muse"

// Adapter is the Muse Code CLI runner.Runner.
type Adapter struct {
	// Path is the muse binary: an absolute path or a name on PATH.
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
		p = "muse"
	}
	if home, herr := os.UserHomeDir(); herr == nil && p == "muse" {
		if alt := filepath.Join(home, ".local", "bin", "muse"); fileExists(alt) {
			return alt, nil
		}
	}
	resolved, err := exec.LookPath(p)
	if err != nil {
		return "", fmt.Errorf("muse: binary not found: %w", err)
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
		for _, p := range []string{"PATH=", "HOME=", "LANG=", "LC_ALL=", "TERM=", "XDG_RUNTIME_DIR=", "XDG_CONFIG_HOME="} {
			if strings.HasPrefix(kv, p) {
				env = append(env, kv)
			}
		}
	}
	return append(env, req.Env...)
}

func writesWorktree(role string) bool { return role == "worker" || role == "cleanup" }

// Argv is the muse command line for req. The prompt is read from its file.
func (a *Adapter) Argv(path string, req runner.Request) []string {
	argv := []string{path, "exec", "--json",
		"--prompt-file", req.PromptPath,
		"--model", req.Model,
		"--workspace", req.Workdir,
		"--approval-mode", "never",
		"--reasoning-effort", "medium",
		"--disable-web-tools", "--disable-request-user-input", "--no-session-log",
		"--max-model-steps", "80",
	}
	if !writesWorktree(req.Role) {
		argv = append(argv, "--disable-write")
	}
	return argv
}

// Run executes one model call and classifies its failures.
func (a *Adapter) Run(ctx context.Context, req runner.Request) (runner.Result, error) {
	var res runner.Result
	if req.PromptPath == "" || req.StageDir == "" || req.Workdir == "" || req.Role == "" || req.Model == "" {
		return res, errors.New("muse: Role, Model, Workdir, StageDir and PromptPath are required")
	}
	path, err := a.path()
	if err != nil {
		return res, &runner.Error{Class: runner.ErrCrash, Msg: err.Error(), Cause: err}
	}
	events, err := os.OpenFile(filepath.Join(req.StageDir, "events.jsonl"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return res, fmt.Errorf("muse: events log: %w", err)
	}
	defer events.Close()

	argv := a.Argv(path, req)
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

	res = runner.Result{Final: out.final(), Events: out.events, Usage: out.usage}
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

// Version runs `muse --version`.
func (a *Adapter) Version(ctx context.Context) (string, error) {
	path, err := a.path()
	if err != nil {
		return "", err
	}
	cmd := exec.CommandContext(ctx, path, "--version")
	cmd.Env = a.env(runner.Request{})
	b, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("muse: --version: %w", err)
	}
	return parseVersion(string(b))
}

var versionRe = regexp.MustCompile(`\d+\.\d+\.\d+\S*`)

func parseVersion(out string) (string, error) {
	if v := versionRe.FindString(out); v != "" {
		return v, nil
	}
	return "", fmt.Errorf("muse: cannot parse version from %q", strings.TrimSpace(out))
}

// BuiltinModels lists the models agentflow offers on the Muse login. The CLI has no
// plain model listing, so the list is fixed.
func (a *Adapter) BuiltinModels(ctx context.Context) ([]string, error) {
	return []string{"muse-spark-1.3-contributor"}, nil
}

type streamOut struct {
	events    []runner.Event
	text      string // the run's final text
	errMsg    string
	completed bool
	usage     *runner.Usage
}

func (o *streamOut) final() string { return strings.TrimSpace(o.text) }

type rawEvent struct {
	PayloadType string `json:"payload_type"`
	Payload     struct {
		Terminal string `json:"terminal"`
		Reason   string `json:"reason"`
		Text     string `json:"text"`
		Message  string `json:"message"`
		Models   []struct {
			In  int64 `json:"input_tokens"`
			Out int64 `json:"output_tokens"`
		} `json:"models"`
	} `json:"payload"`
}

// parseStream reads muse's --json events, one object per line. The final answer is the text
// of the run's terminal "completed" event; any other terminal is the failure.
func parseStream(r io.Reader, log io.Writer, on func(runner.Event)) (*streamOut, error) {
	out := &streamOut{}
	br := bufio.NewReaderSize(r, 1<<20)
	for {
		line, err := br.ReadBytes('\n')
		if t := bytes.TrimSpace(line); len(t) > 0 && t[0] == '{' {
			_, _ = log.Write(append(append([]byte{}, t...), '\n'))
			var ev rawEvent
			if json.Unmarshal(t, &ev) == nil && ev.PayloadType != "" {
				e := runner.Event{Type: ev.PayloadType, Raw: append([]byte{}, t...)}
				switch {
				case ev.PayloadType == "run.output.delta":
					e.Text = ev.Payload.Text
				case ev.PayloadType == "run.usage":
					var in, o int64
					for _, m := range ev.Payload.Models {
						in += m.In
						o += m.Out
					}
					out.usage = &runner.Usage{InputTokens: in, OutputTokens: o, TotalTokens: in + o}
				case strings.HasPrefix(ev.PayloadType, "run.terminal"):
					if ev.Payload.Terminal == "completed" || ev.PayloadType == "run.terminal.completed" {
						out.completed = true
						out.text = ev.Payload.Text
					} else {
						out.errMsg = strings.TrimSpace(ev.Payload.Reason + " " + ev.Payload.Message + " " + ev.Payload.Text)
						if out.errMsg == "" {
							out.errMsg = "run ended: " + ev.PayloadType
						}
						e.Text = out.errMsg
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
