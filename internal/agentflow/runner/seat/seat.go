// Package seat is the runner.Runner for interactive seats: a terminal coding assistant with
// no headless mode (freebuff first) that agentflow starts itself, in a multiplexer session per
// stage, inside the stage's sandbox. The whole stage prompt goes to a file in the stage home
// and one short line typed into the session tells the seat to read it; the seat ends the
// stage by writing RESULT.json next to it (see seatkit). The session is not a child of
// agentflow: a restart leaves it running and the next process adopts it by name.
//
// A seat is never retried for being slow. Past the role's budget with nothing changing on
// its screen it is reported as needing attention and keeps its session, so the owner can
// attach to it, nudge it, or cancel the stage.
package seat

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"cmd184psu/unified-webapp/internal/agentflow/runner"
	"cmd184psu/unified-webapp/internal/agentflow/runner/mux"
	"cmd184psu/unified-webapp/internal/agentflow/runner/seatkit"
)

// APIType is the backend api type this adapter serves. The backend's model names the profile.
const APIType = "freebuff_seat"

// SessionPrefix starts every seat session name.
const SessionPrefix = "af-"

// Session tags.
const (
	tagInstance = "agentflow_instance"
	tagStage    = "agentflow_stage"
	tagStageDir = "agentflow_stage_dir"
)

// Files in the stage home.
const (
	TaskFile   = "TASK.md"
	ResultFile = "RESULT.json"
	stateFile  = "seat.json"
)

// Duration is a time.Duration that reads "8s" style text from JSON.
type Duration time.Duration

// UnmarshalJSON reads a Go duration string or a number of seconds.
func (d *Duration) UnmarshalJSON(b []byte) error {
	var s string
	if json.Unmarshal(b, &s) == nil {
		v, err := time.ParseDuration(s)
		*d = Duration(v)
		return err
	}
	var n float64
	if err := json.Unmarshal(b, &n); err != nil {
		return err
	}
	*d = Duration(time.Duration(n * float64(time.Second)))
	return nil
}

// MarshalJSON writes the Go duration string.
func (d Duration) MarshalJSON() ([]byte, error) { return json.Marshal(time.Duration(d).String()) }

// Profile is how to run one interactive program as a seat.
type Profile struct {
	// Command is the program and arguments; "{workdir}" becomes the stage's worktree.
	Command []string `json:"command"`
	// StartupWait is how long to wait after start before typing; with ReadyText set it is
	// the most to wait for that text to show on the screen.
	StartupWait Duration `json:"startup_wait"`
	// ReadyText, when set, is text on the screen that says the program takes input.
	ReadyText string `json:"ready_text,omitempty"`
	// SubmitKeys are pressed after the line is typed (default Enter).
	SubmitKeys []string `json:"submit_keys,omitempty"`
	// Net gives the program the network; a hosted assistant needs it.
	Net bool `json:"net"`
	// ConfigDir is the program's own config directory under the server user's home
	// ("~/.config/manicode" for freebuff). The stage home gets the same directory, holding:
	ConfigDir string `json:"config_dir,omitempty"`
	// CopyFiles: files of ConfigDir copied in (mode 0600): the login. They are removed when
	// the stage ends and never logged.
	CopyFiles []string `json:"copy_files,omitempty"`
	// LinkFiles: large read-only files of ConfigDir linked in (the program's real binary, its
	// tools). The sandbox may read exactly these from the user's config.
	LinkFiles []string `json:"link_files,omitempty"`
	// HideDirs are directories under the server user's home the program may not read
	// ("~/.config"); LinkFiles stay readable inside them.
	HideDirs []string `json:"hide_dirs,omitempty"`
	// HaltTexts are screen texts that mean the program wants a decision only the owner may
	// make (log in; take over a session that is in use elsewhere, which would stop the
	// owner's other instance). While one shows, agentflow types nothing and presses no key,
	// not even an answer, and the stage waits as needing attention.
	HaltTexts []string `json:"halt_texts,omitempty"`
	// Env is extra environment for the program.
	Env map[string]string `json:"env,omitempty"`

	// TmuxSession makes the profile a queue seat (AF-11b): the owner's own, already running
	// session. agentflow never starts, kills or sandboxes it; Command is not used.
	TmuxSession string `json:"tmux_session,omitempty"`
	// QueueFile is the queue seat's queue file (absolute or "~/..."): the stage's task is
	// written into it as a marked block, removed when the stage ends or is cancelled.
	QueueFile string `json:"queue_file,omitempty"`
	// NudgeText is typed to wake a queue seat (default "check your queue"), then Enter, a
	// pause and Enter again.
	NudgeText string `json:"nudge_text,omitempty"`
	// NudgeWait is how long a nudged screen may stand still before the next nudge (60s);
	// after three nudges the stage needs attention.
	NudgeWait Duration `json:"nudge_wait,omitempty"`
}

// Freebuff is the built-in profile: freebuff in the worktree, given the network.
// freebuff 0.2.x keeps its login and its real CLI in ~/.config/manicode, found through $HOME.
func Freebuff() Profile {
	return Profile{
		Command:     []string{"/opt/homebrew/bin/freebuff", "--cwd", "{workdir}"},
		StartupWait: Duration(60 * time.Second),
		ReadyText:   "Enter a coding task",
		SubmitKeys:  []string{"Enter"},
		Net:         true,
		Env:         map[string]string{"TERM": "xterm-256color", "COLORTERM": "truecolor"},
		ConfigDir:   "~/.config/manicode",
		CopyFiles:   []string{"credentials.json", "device-key.json", "settings.json"},
		LinkFiles:   []string{"freebuff", "rg", "tree-sitter.wasm", "freebuff-metadata.json"},
		HideDirs:    []string{"~/.config"},
		// One freebuff login has one live session: Enter on this prompt would stop the owner's
		// own freebuff elsewhere.
		HaltTexts: []string{"in use elsewhere", "Take over to move it here", "Press ENTER to login"},
	}
}

// halted returns the halt text on the screen, or "".
// stillView is the screen as silence detection compares it.
func stillView(prof Profile, screen string) string { return screen }

func halted(prof Profile, screen string) string {
	for _, t := range prof.HaltTexts {
		if t != "" && strings.Contains(screen, t) {
			return t
		}
	}
	return ""
}

// errHalted: the program shows a halt text; nothing may be typed until the owner acts.
var errHalted = errors.New("the seat waits for the owner")

// Launch is a sandboxed command ready to run, and the stage home as the program sees it.
type Launch struct {
	Argv []string
	Env  []string
	Dir  string
	// Home is the stage home's path inside the sandbox, which the seat is told to use.
	Home string
}

// SandboxOpts is what a seat program needs from its sandbox.
type SandboxOpts struct {
	Argv []string
	Env  map[string]string
	Net  bool
	// HideDirs and ReadFiles are absolute host paths (see sandbox.Spec).
	HideDirs  []string
	ReadFiles []string
}

// Sandboxer builds the sandboxed command for a stage: the program runs in the stage's sandbox
// with the stage home, a writable worktree only for writing roles, and the network when asked.
type Sandboxer func(req runner.Request, o SandboxOpts) (Launch, error)

// Adapter is the interactive seat runner.
type Adapter struct {
	// Mux returns the multiplexer (read per call, so the setting needs no restart).
	Mux func(ctx context.Context) (mux.Multiplexer, error)
	// Profiles returns the configured profiles; missing names fall back to the built-ins.
	Profiles func(ctx context.Context) map[string]Profile
	// Sandbox wraps the program in the stage sandbox. Nil runs it unsandboxed (tests only).
	Sandbox Sandboxer
	// Instance tags every session with this process's data directory, so one agentflow
	// never adopts or kills another's sessions on the same tmux server.
	Instance string
	// Board receives the stage's question and silence notices.
	Board *runner.SeatBoard
	// Poll is how often RESULT.json and the screen are read (default 3s).
	Poll  time.Duration
	Clock func() time.Time
	// NudgeGap is the pause between a queue seat nudge's two Enters (default 2s).
	NudgeGap time.Duration
}

var _ runner.Runner = (*Adapter)(nil)

// Version names the runner; the program behind a profile reports nothing headless.
func (a *Adapter) Version(context.Context) (string, error) { return "interactive seats", nil }

func (a *Adapter) now() time.Time {
	if a.Clock != nil {
		return a.Clock()
	}
	return time.Now()
}

func (a *Adapter) poll() time.Duration {
	if a.Poll > 0 {
		return a.Poll
	}
	return 3 * time.Second
}

// InstanceID is the instance tag for a data directory.
func InstanceID(dataDir string) string {
	sum := sha256.Sum256([]byte(filepath.Clean(dataDir)))
	return fmt.Sprintf("%x", sum[:6])
}

// SessionName is af-<project>-<effort>-<task>-<stage>; empty parts are left out, and the
// stage id is always last, so StageOf can read it back.
func SessionName(job runner.Job) string {
	parts := []string{"af"}
	if p := mux.Clean(job.Project); p != "" {
		parts = append(parts, p)
	}
	if job.Effort > 0 {
		parts = append(parts, strconv.FormatInt(job.Effort, 10))
	}
	if t := mux.Clean(job.Task); t != "" {
		parts = append(parts, t)
	}
	parts = append(parts, strconv.FormatInt(job.Stage, 10))
	return strings.Join(parts, "-")
}

// StageOf reads the stage id at the end of a session name.
func StageOf(name string) (int64, bool) {
	if !strings.HasPrefix(name, SessionPrefix) {
		return 0, false
	}
	i := strings.LastIndexByte(name, '-')
	id, err := strconv.ParseInt(name[i+1:], 10, 64)
	return id, err == nil && id > 0
}

// HomeDir is the stage home: the seat's private HOME, where TASK.md and RESULT.json live.
func HomeDir(stageDir string) string { return filepath.Join(stageDir, "home") }

func (a *Adapter) profile(ctx context.Context, name string) (Profile, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "freebuff"
	}
	if a.Profiles != nil {
		if p, ok := a.Profiles(ctx)[name]; ok {
			if p.TmuxSession != "" {
				if !mux.Valid(p.TmuxSession) || p.QueueFile == "" {
					return p, fmt.Errorf("queue seat profile %q needs a valid tmux_session and a queue_file", name)
				}
				return p, nil
			}
			if len(p.Command) == 0 {
				return p, fmt.Errorf("seat profile %q has no command", name)
			}
			if len(p.SubmitKeys) == 0 {
				p.SubmitKeys = []string{"Enter"}
			}
			return p, nil
		}
	}
	if name == "freebuff" {
		return Freebuff(), nil
	}
	return Profile{}, fmt.Errorf("no seat profile %q", name)
}

// state is what the adapter keeps in the stage dir so a later process can adopt the session.
type state struct {
	Session    string    `json:"session"`
	Created    time.Time `json:"created"`
	PromptSent bool      `json:"prompt_sent"`
	Home       string    `json:"home"`
	Profile    string    `json:"profile"`
	Questions  int       `json:"questions"`
}

func readState(stageDir string) (state, bool) {
	var s state
	b, err := os.ReadFile(filepath.Join(stageDir, stateFile))
	if err != nil || json.Unmarshal(b, &s) != nil {
		return s, false
	}
	return s, true
}

func writeState(stageDir string, s state) error {
	b, _ := json.MarshalIndent(s, "", "  ")
	tmp := filepath.Join(stageDir, stateFile+".tmp")
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(stageDir, stateFile))
}

func failed(class runner.ErrClass, msg string, cause error) (runner.Result, error) {
	return runner.Result{}, &runner.Error{Class: class, Msg: msg, Cause: cause}
}

// Run starts or adopts the stage's session and waits for the seat's report.
func (a *Adapter) Run(ctx context.Context, req runner.Request) (runner.Result, error) {
	res, err := a.run(ctx, req)
	var re *runner.Error
	if req.StageDir != "" && !(errors.As(err, &re) && re.Class == runner.ErrDetached) {
		// The call is over unless it detached (the next process adopts the session and still
		// needs the login): the stage's copy of the login goes, whatever ended it.
		a.forgetLogin(ctx, state{Profile: req.Model}, HomeDir(req.StageDir))
	}
	return res, err
}

func (a *Adapter) run(ctx context.Context, req runner.Request) (runner.Result, error) {
	if req.StageDir == "" || req.Job.Stage == 0 {
		return failed(runner.ErrCrash, "seat: a seat runs only as a stage", nil)
	}
	if a.Mux == nil {
		return failed(runner.ErrCrash, "seat: no multiplexer configured", nil)
	}
	m, err := a.Mux(ctx)
	if err != nil {
		return failed(runner.ErrCrash, "seat: "+err.Error(), err)
	}
	if prof, err := a.profile(ctx, req.Model); err == nil && prof.TmuxSession != "" {
		return a.runQueue(ctx, m, req, req.Model, prof)
	} else if q, ok := readQueueState(req.StageDir); ok {
		return failed(runner.ErrCrash, fmt.Sprintf("seat: stage was queued to %s but profile %q is no longer a queue seat", q.Session, req.Model), err)
	}
	name := SessionName(req.Job)
	home := HomeDir(req.StageDir)
	if err := os.MkdirAll(home, 0o700); err != nil {
		return failed(runner.ErrCrash, "seat: stage home", err)
	}
	st, have := readState(req.StageDir)
	alive, err := m.Has(ctx, name)
	if err != nil {
		return failed(runner.ErrUnreachable, "seat: multiplexer: "+err.Error(), err)
	}
	if alive {
		if inst, _ := m.Tag(ctx, name, tagInstance); inst != a.Instance {
			return failed(runner.ErrCrash, fmt.Sprintf("seat: session %s belongs to another agentflow (%q)", name, inst), nil)
		}
	}
	emit := func(t, text string) {
		if req.OnEvent != nil {
			req.OnEvent(runner.Event{Type: t, Text: text})
		}
	}
	if !alive || !have {
		if alive { // a session with no state: left by a crash between create and state write
			_ = m.Kill(ctx, name)
		}
		st, err = a.start(ctx, m, name, req)
		if err != nil {
			var re *runner.Error
			if errors.As(err, &re) {
				return runner.Result{}, re
			}
			return failed(runner.ErrCrash, "seat: start: "+err.Error(), err)
		}
		emit("seat.started", "seat session "+name+" started: "+m.AttachCommand(name))
	} else {
		emit("seat.adopted", "seat session "+name+" adopted: "+m.AttachCommand(name))
	}
	if !st.PromptSent && alive && have {
		// Adopted before the line was confirmed: it may have gone in after all.
		if screen, err := m.Capture(ctx, name, 2000); err == nil && strings.Contains(screen, TaskFile) {
			st.PromptSent = true
		}
	}
	if !st.PromptSent {
		if err := a.sendPrompt(ctx, m, name, req, st.Home); err != nil && !errors.Is(err, errHalted) {
			if ctx.Err() != nil {
				return stopped(ctx, m, name)
			}
			return failed(runner.ErrCrash, "seat: type the task: "+err.Error(), err)
		} else if err == nil {
			st.PromptSent = true
			if err := writeState(req.StageDir, st); err != nil {
				return failed(runner.ErrCrash, "seat: state", err)
			}
		}
		// Halted: wait sends the line once the owner has dealt with the program.
	}
	return a.wait(ctx, m, name, req, st)
}

// start creates the session: auth files into the stage home, TASK.md, then the program.
func (a *Adapter) start(ctx context.Context, m mux.Multiplexer, name string, req runner.Request) (state, error) {
	prof, err := a.profile(ctx, req.Model)
	if err != nil {
		return state{}, &runner.Error{Class: runner.ErrCrash, Msg: "seat: " + err.Error()}
	}
	home := HomeDir(req.StageDir)
	readFiles, err := installConfig(prof, home)
	if err != nil {
		return state{}, &runner.Error{Class: runner.ErrCrash, Msg: "seat: " + err.Error()}
	}
	hide := make([]string, 0, len(prof.HideDirs))
	for _, d := range prof.HideDirs {
		if p, err := underHome(d); err == nil {
			hide = append(hide, p)
		}
	}
	_ = os.Remove(filepath.Join(home, ResultFile))
	argv := make([]string, len(prof.Command))
	for i, s := range prof.Command {
		argv[i] = strings.ReplaceAll(s, "{workdir}", req.Workdir)
	}
	env := map[string]string{}
	for k, v := range prof.Env {
		env[k] = v
	}
	for _, kv := range req.Env {
		if k, v, ok := strings.Cut(kv, "="); ok {
			env[k] = v
		}
	}
	var l Launch
	if a.Sandbox != nil {
		if l, err = a.Sandbox(req, SandboxOpts{Argv: argv, Env: env, Net: prof.Net, HideDirs: hide, ReadFiles: readFiles}); err != nil {
			return state{}, fmt.Errorf("sandbox: %w", err)
		}
	} else {
		l = Launch{Argv: argv, Dir: req.Workdir, Home: home, Env: []string{"HOME=" + home, "PATH=/usr/bin:/bin:/opt/homebrew/bin:/usr/local/bin"}}
		for k, v := range env {
			l.Env = append(l.Env, k+"="+v)
		}
	}
	if err := writeTask(req, home, l.Home); err != nil {
		return state{}, err
	}
	if err := m.Create(ctx, mux.Spec{Name: name, Dir: l.Dir, Argv: l.Argv, Env: l.Env, ScriptDir: req.StageDir,
		Tags: map[string]string{tagInstance: a.Instance, tagStage: strconv.FormatInt(req.Job.Stage, 10), tagStageDir: req.StageDir}}); err != nil {
		return state{}, err
	}
	st := state{Session: name, Created: a.now(), Home: l.Home, Profile: req.Model}
	if err := writeState(req.StageDir, st); err != nil {
		_ = m.Kill(ctx, name)
		return state{}, err
	}
	// Wait for the program to take input.
	deadline := a.now().Add(time.Duration(prof.StartupWait))
	for {
		if dead, _ := m.Dead(ctx, name); dead {
			screen, _ := m.Capture(ctx, name, 200)
			_ = m.Kill(ctx, name)
			return state{}, &runner.Error{Class: runner.ErrCrash, Msg: "seat: the program exited at start: " + tail(screen, 1500)}
		}
		if prof.ReadyText != "" || len(prof.HaltTexts) > 0 {
			if screen, err := m.Capture(ctx, name, 0); err == nil &&
				((prof.ReadyText != "" && strings.Contains(screen, prof.ReadyText)) || halted(prof, screen) != "") {
				break
			}
		}
		if !a.now().Before(deadline) {
			break
		}
		select {
		case <-ctx.Done():
			return st, nil // the prompt goes in when the stage is adopted again
		case <-time.After(min(500*time.Millisecond, time.Duration(prof.StartupWait)+time.Millisecond)):
		}
	}
	return st, nil
}

// writeTask writes TASK.md: the stage prompt and how to finish, in the stage home.
func writeTask(req runner.Request, home, seenHome string) error {
	prompt := req.Message
	if req.PromptPath != "" {
		b, err := os.ReadFile(req.PromptPath)
		if err != nil {
			return fmt.Errorf("read the stage prompt: %w", err)
		}
		prompt = string(b)
	}
	if seenHome == "" {
		seenHome = home
	}
	text := prompt + seatkit.Instructions(req.Role, filepath.Join(seenHome, ResultFile))
	return os.WriteFile(filepath.Join(home, TaskFile), []byte(text), 0o600)
}

// sendPrompt types the one line that points the seat at TASK.md.
func (a *Adapter) sendPrompt(ctx context.Context, m mux.Multiplexer, name string, req runner.Request, seenHome string) error {
	prof, err := a.profile(ctx, req.Model)
	if err != nil {
		return err
	}
	if seenHome == "" {
		seenHome = HomeDir(req.StageDir)
	}
	task := filepath.Join(seenHome, TaskFile)
	line := fmt.Sprintf("Read the file %s and do exactly what it says. When you stop, write %s as that file describes.",
		task, filepath.Join(seenHome, ResultFile))
	// A TUI that is still drawing can drop what is typed: the line counts as sent once the
	// screen shows it, and is typed again (at most three times) when it does not.
	for try := 0; try < 3; try++ {
		if screen, err := m.Capture(ctx, name, 200); err == nil && halted(prof, screen) != "" {
			return errHalted
		}
		if err := m.SendText(ctx, name, line); err != nil {
			return err
		}
		time.Sleep(200 * time.Millisecond) // some TUIs drop a key pressed in the same burst as a paste
		if err := m.SendKeys(ctx, name, prof.SubmitKeys...); err != nil {
			return err
		}
		for wait := 0; wait < 20; wait++ {
			if screen, err := m.Capture(ctx, name, 200); err == nil && strings.Contains(screen, TaskFile) {
				return nil
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(a.echoWait()):
			}
		}
	}
	return fmt.Errorf("the seat never showed the typed line naming %s", task)
}

// echoWait is how long to wait between looks for the typed line on the screen.
func (a *Adapter) echoWait() time.Duration {
	if a.Poll > 0 && a.Poll < 500*time.Millisecond {
		return a.Poll
	}
	return 500 * time.Millisecond
}

// wait polls RESULT.json and the screen until the seat reports, the program dies, or the
// call is stopped.
func (a *Adapter) wait(ctx context.Context, m mux.Multiplexer, name string, req runner.Request, st state) (runner.Result, error) {
	stage := req.Job.Stage
	home := HomeDir(req.StageDir)
	attach := m.AttachCommand(name)
	board := a.Board
	if board == nil {
		board = &runner.SeatBoard{}
	}
	lastScreen, lastChange := "", a.now()
	silent, haltedNow := false, false
	defer func() {
		if ctx.Err() == nil {
			board.Clear(stage)
		}
	}()
	for {
		if rep, ok, err := readReport(home); err != nil {
			// A half-written or malformed file: tell the seat once, keep waiting.
			_ = os.Rename(filepath.Join(home, ResultFile), filepath.Join(home, fmt.Sprintf("RESULT.rejected.%d.json", a.now().UnixNano())))
			_ = m.SendText(ctx, name, "agentflow could not read RESULT.json ("+oneLine(err.Error())+"). Write it again as TASK.md describes.")
			_ = m.SendKeys(ctx, name, "Enter")
		} else if ok {
			switch rep.Status {
			case seatkit.StatusQuestion:
				st.Questions++
				_ = os.Rename(filepath.Join(home, ResultFile), filepath.Join(home, fmt.Sprintf("QUESTION.%d.json", st.Questions)))
				_ = writeState(req.StageDir, st)
				_ = os.WriteFile(filepath.Join(home, "QUESTION.open"), []byte(rep.Question), 0o600)
			default:
				final, ferr := seatkit.Final(req.Role, rep)
				if ferr != nil {
					_ = os.Rename(filepath.Join(home, ResultFile), filepath.Join(home, fmt.Sprintf("RESULT.rejected.%d.json", a.now().UnixNano())))
					_ = m.SendText(ctx, name, "agentflow rejected RESULT.json: "+oneLine(ferr.Error())+". Write it again as TASK.md describes.")
					_ = m.SendKeys(ctx, name, "Enter")
					break
				}
				screen, _ := m.Capture(ctx, name, 2000)
				_ = os.WriteFile(filepath.Join(req.StageDir, "seat-screen.txt"), []byte(screen), 0o600)
				_ = os.Rename(filepath.Join(home, ResultFile), filepath.Join(home, "RESULT.final.json"))
				_ = m.Kill(context.WithoutCancel(ctx), name)
				a.forgetLogin(ctx, st, home)
				board.Clear(stage)
				return runner.Result{Final: final}, nil
			}
		}
		if q, err := os.ReadFile(filepath.Join(home, "QUESTION.open")); err == nil {
			board.Set(runner.SeatNotice{Stage: stage, Kind: runner.SeatQuestion, Message: "The seat asks: " + oneLine(string(q)), Since: a.now(), Session: name, Attach: attach})
		}
		alive, err := m.Has(ctx, name)
		if err == nil && !alive {
			a.forgetLogin(ctx, st, home)
			if ctx.Err() != nil {
				return runner.Result{Incomplete: true}, ctx.Err() // cancelled: the session was killed first
			}
			return failed(runner.ErrCrash, "seat: session "+name+" is gone without a report", nil)
		}
		if dead, err := m.Dead(ctx, name); err == nil && dead {
			if _, ok, _ := readReport(home); !ok {
				screen, _ := m.Capture(ctx, name, 200)
				_ = os.WriteFile(filepath.Join(req.StageDir, "seat-screen.txt"), []byte(screen), 0o600)
				_ = m.Kill(context.WithoutCancel(ctx), name)
				a.forgetLogin(ctx, st, home)
				return failed(runner.ErrCrash, "seat: the program exited without a report: "+tail(screen, 1500), nil)
			}
			continue
		}
		prof, _ := a.profile(ctx, st.Profile)
		if screen, err := m.Capture(ctx, name, 200); err == nil {
			if h := halted(prof, screen); h != "" {
				board.Set(runner.SeatNotice{Stage: stage, Kind: runner.SeatQuestion, Since: a.now(), Session: name, Attach: attach,
					Message: fmt.Sprintf("The seat program shows %q and needs the owner; agentflow types nothing until it is gone. Attach with `%s`", h, attach)})
				haltedNow = true
			} else if haltedNow {
				haltedNow = false
				if n, ok := board.Get(stage); ok && n.Kind == runner.SeatQuestion && strings.Contains(n.Message, "needs the owner") {
					board.Clear(stage)
				}
			}
			if !haltedNow && !st.PromptSent {
				if err := a.sendPrompt(ctx, m, name, req, st.Home); err == nil {
					st.PromptSent = true
					_ = writeState(req.StageDir, st)
				}
			}
		}
		if screen, err := m.Capture(ctx, name, 0); err == nil && screen != lastScreen {
			lastScreen, lastChange = screen, a.now()
			if silent {
				silent = false
				if n, ok := board.Get(stage); ok && n.Kind == runner.SeatSilent {
					board.Clear(stage)
				}
			}
			if req.OnEvent != nil {
				req.OnEvent(runner.Event{Type: "seat.screen"})
			}
		}
		if req.Budget > 0 && !silent && a.now().Sub(lastChange) >= req.Budget {
			silent = true
			if _, ok := board.Get(stage); !ok {
				board.Set(runner.SeatNotice{Stage: stage, Kind: runner.SeatSilent,
					Message: fmt.Sprintf("The seat has shown nothing new for %s; attach with `%s`", req.Budget.Round(time.Second), attach),
					Since:   a.now(), Session: name, Attach: attach})
			}
		}
		select {
		case <-ctx.Done():
			return stopped(ctx, m, name)
		case <-time.After(a.poll()):
		}
	}
}

// readReport reads RESULT.json; ok is false when there is none yet.
func readReport(home string) (seatkit.Report, bool, error) {
	b, err := os.ReadFile(filepath.Join(home, ResultFile))
	if err != nil {
		return seatkit.Report{}, false, nil
	}
	if len(strings.TrimSpace(string(b))) == 0 {
		return seatkit.Report{}, false, nil // being written
	}
	r, err := seatkit.ParseReport(b)
	if err != nil {
		return r, false, err
	}
	return r, true, nil
}

// Answer types the owner's answer into the seat's session and closes its open question.
func (a *Adapter) Answer(ctx context.Context, stage int64, stageDir, text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return errors.New("seat: the answer is empty")
	}
	if isQueue(stageDir) {
		return a.answerQueue(ctx, stage, stageDir, text)
	}
	m, err := a.Mux(ctx)
	if err != nil {
		return err
	}
	name, err := a.SessionFor(ctx, stage)
	if err != nil {
		return err
	}
	st, _ := readState(stageDir)
	prof, perr := a.profile(ctx, st.Profile)
	if perr != nil {
		prof = Freebuff()
	}
	if screen, err := m.Capture(ctx, name, 200); err == nil {
		if h := halted(prof, screen); h != "" {
			return fmt.Errorf("seat: the program shows %q; answer it yourself with `%s`, agentflow types nothing there", h, m.AttachCommand(name))
		}
	}
	if err := m.SendText(ctx, name, oneLine(text)); err != nil {
		return err
	}
	time.Sleep(200 * time.Millisecond)
	if err := m.SendKeys(ctx, name, prof.SubmitKeys...); err != nil {
		return err
	}
	_ = os.Remove(filepath.Join(HomeDir(stageDir), "QUESTION.open"))
	if a.Board != nil {
		if n, ok := a.Board.Get(stage); ok && n.Kind == runner.SeatQuestion {
			a.Board.Clear(stage)
		}
	}
	return nil
}

// Sessions lists this instance's live seat sessions by stage.
func (a *Adapter) Sessions(ctx context.Context) (map[int64]string, error) {
	m, err := a.Mux(ctx)
	if err != nil {
		return nil, err
	}
	names, err := m.List(ctx, SessionPrefix)
	if err != nil {
		return nil, err
	}
	out := map[int64]string{}
	for _, n := range names {
		id, ok := StageOf(n)
		if !ok {
			continue
		}
		if inst, err := m.Tag(ctx, n, tagInstance); err != nil || inst != a.Instance {
			continue
		}
		if tag, _ := m.Tag(ctx, n, tagStage); tag != strconv.FormatInt(id, 10) {
			continue
		}
		out[id] = n
	}
	return out, nil
}

// SessionFor finds the stage's live session.
func (a *Adapter) SessionFor(ctx context.Context, stage int64) (string, error) {
	all, err := a.Sessions(ctx)
	if err != nil {
		return "", err
	}
	n, ok := all[stage]
	if !ok {
		return "", mux.ErrNoSession
	}
	return n, nil
}

// Attach is the command that opens the stage's session, or "" with none.
func (a *Adapter) Attach(ctx context.Context, stage int64, stageDir string) string {
	if q, ok := readQueueState(stageDir); ok {
		return "tmux attach -t " + q.Session
	}
	n, err := a.SessionFor(ctx, stage)
	if err != nil {
		return ""
	}
	m, err := a.Mux(ctx)
	if err != nil {
		return ""
	}
	return m.AttachCommand(n)
}

// Alive says the stage has a live session of this instance.
func (a *Adapter) Alive(ctx context.Context, stage int64, stageDir string) bool {
	if q, ok := readQueueState(stageDir); ok {
		return !q.Cancelled // the task waits in the queue file even while the session is down
	}
	_, err := a.SessionFor(ctx, stage)
	return err == nil
}

// Kill ends the stage's session; the running call then returns as cancelled. A queue seat's
// session is never killed: its task leaves the queue file instead.
func (a *Adapter) Kill(ctx context.Context, stage int64, stageDir string) error {
	if isQueue(stageDir) {
		return a.cancelQueue(stage, stageDir)
	}
	n, err := a.SessionFor(ctx, stage)
	if errors.Is(err, mux.ErrNoSession) {
		return nil
	}
	if err != nil {
		return err
	}
	m, err := a.Mux(ctx)
	if err != nil {
		return err
	}
	if a.Board != nil {
		a.Board.Clear(stage)
	}
	return m.Kill(ctx, n)
}

// Sweep kills this instance's sessions whose stage is no longer open and returns their names.
func (a *Adapter) Sweep(ctx context.Context, open func(stage int64) bool) ([]string, error) {
	all, err := a.Sessions(ctx)
	if err != nil {
		return nil, err
	}
	m, err := a.Mux(ctx)
	if err != nil {
		return nil, err
	}
	var killed []string
	for id, n := range all {
		if open(id) {
			continue
		}
		dir, _ := m.Tag(ctx, n, tagStageDir)
		if err := m.Kill(ctx, n); err == nil {
			killed = append(killed, n)
			if st, ok := readState(dir); ok && dir != "" {
				a.forgetLogin(ctx, st, HomeDir(dir))
			}
			if a.Board != nil {
				a.Board.Clear(id)
			}
		}
	}
	return killed, nil
}

// underHome resolves "~/x" or "x" (relative to the home directory) to an absolute path under
// the server user's home, refusing anything outside it.
func underHome(p string) (string, error) {
	userHome, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	src := p
	if strings.HasPrefix(p, "~/") {
		src = filepath.Join(userHome, p[2:])
	} else if !filepath.IsAbs(p) {
		src = filepath.Join(userHome, p)
	}
	src = filepath.Clean(src)
	rel, err := filepath.Rel(userHome, src)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("%s is not under the home directory", p)
	}
	return src, nil
}

// configDirs is the profile's config directory on the host and in the stage home.
func configDirs(prof Profile, stageHome string) (host, stage string, err error) {
	host, err = underHome(prof.ConfigDir)
	if err != nil {
		return "", "", err
	}
	userHome, _ := os.UserHomeDir()
	rel, _ := filepath.Rel(userHome, host)
	return host, filepath.Join(stageHome, rel), nil
}

// installConfig gives the stage home the program's config directory: a private copy of the
// login files and links to the large read-only ones. It returns the linked files' host paths,
// which the sandbox lets the program read. Nothing is logged.
func installConfig(prof Profile, stageHome string) ([]string, error) {
	if prof.ConfigDir == "" {
		return nil, nil
	}
	host, stage, err := configDirs(prof, stageHome)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(stage, 0o700); err != nil {
		return nil, err
	}
	for _, f := range prof.CopyFiles {
		if f != filepath.Base(f) {
			return nil, fmt.Errorf("copy file %q must be a plain name in %s", f, prof.ConfigDir)
		}
		b, err := os.ReadFile(filepath.Join(host, f))
		if err != nil {
			return nil, fmt.Errorf("the seat's login file %s in %s cannot be read: %w", f, prof.ConfigDir, errors.Unwrap(err))
		}
		if err := os.WriteFile(filepath.Join(stage, f), b, 0o600); err != nil {
			return nil, err
		}
	}
	var read []string
	for _, f := range prof.LinkFiles {
		if f != filepath.Base(f) {
			return nil, fmt.Errorf("link file %q must be a plain name in %s", f, prof.ConfigDir)
		}
		src := filepath.Join(host, f)
		if _, err := os.Stat(src); err != nil {
			continue // an optional tool the program may not have installed
		}
		dst := filepath.Join(stage, f)
		_ = os.Remove(dst)
		if err := os.Symlink(src, dst); err != nil {
			return nil, err
		}
		read = append(read, src)
	}
	return read, nil
}

// removeLogin deletes the stage's copy of the login files once the stage is over.
func removeLogin(prof Profile, stageHome string) {
	if prof.ConfigDir == "" {
		return
	}
	if _, stage, err := configDirs(prof, stageHome); err == nil {
		for _, f := range prof.CopyFiles {
			_ = os.Remove(filepath.Join(stage, filepath.Base(f)))
		}
	}
}

func tail(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		return "…" + s[len(s)-n:]
	}
	return s
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// forgetLogin removes the stage's copy of the seat's login.
func (a *Adapter) forgetLogin(ctx context.Context, st state, home string) {
	if prof, err := a.profile(context.WithoutCancel(ctx), st.Profile); err == nil {
		removeLogin(prof, home)
	}
}

// stopped ends a call whose context is done: as cancelled when the session was killed (a
// cancel kills it first), as detached when it lives on (a restart): the next process adopts it.
func stopped(ctx context.Context, m mux.Multiplexer, name string) (runner.Result, error) {
	if ok, err := m.Has(context.WithoutCancel(ctx), name); err == nil && !ok {
		return runner.Result{Incomplete: true}, ctx.Err()
	}
	return runner.Result{Incomplete: true}, &runner.Error{Class: runner.ErrDetached, Msg: "seat " + name + " keeps running", Cause: ctx.Err()}
}
