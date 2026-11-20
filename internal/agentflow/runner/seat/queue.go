package seat

// Queue mode (AF-11b): a seat the owner runs by hand in their own terminal session (the
// deepseek4 / freebuff-beam seats), working from a queue file. agentflow never starts, kills
// or sandboxes that session. It writes the stage's task into the queue file as a marked block,
// nudges the seat with one short line ("check your queue", Enter, a pause, Enter), and reads
// the seat's RESULT.json like any other seat. A profile with tmux_session set is a queue seat.
//
// This is the only path on which agentflow types into a session it did not start, and only
// once the owner has configured such a profile. A halt text on the screen (a login, a "take
// over" prompt) stops all typing and lists the stage as needing attention instead.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"cmd184psu/unified-webapp/internal/agentflow/runner"
	"cmd184psu/unified-webapp/internal/agentflow/runner/mux"
	"cmd184psu/unified-webapp/internal/agentflow/runner/seatkit"
)

// Queue-mode defaults.
const (
	DefaultNudgeText = "check your queue"
	defaultNudgeWait = 60 * time.Second
	maxNudges        = 3
	queueStateFile   = "queue-seat.json"
	queueDirName     = "queue"
)

// queueState is what a queue seat keeps in the stage dir, so a restart adopts the stage.
type queueState struct {
	Session   string    `json:"session"`
	QueueFile string    `json:"queue_file"`
	Result    string    `json:"result"`
	Profile   string    `json:"profile"`
	Nudges    int       `json:"nudges"`
	LastNudge time.Time `json:"last_nudge"`
	// Engaged: the screen changed after a nudge, so the seat took the task; no more nudges.
	Engaged   bool `json:"engaged"`
	Questions int  `json:"questions"`
	// Cancelled: the stage was cancelled; its block is gone from the queue file.
	Cancelled bool `json:"cancelled"`
}

func readQueueState(stageDir string) (queueState, bool) {
	var s queueState
	if stageDir == "" {
		return s, false
	}
	b, err := os.ReadFile(filepath.Join(stageDir, queueStateFile))
	if err != nil || json.Unmarshal(b, &s) != nil {
		return s, false
	}
	return s, true
}

// queueMu serializes queue state writes: the run loop and a cancel both write the file.
var queueMu sync.Mutex

// saveQueueState writes the run loop's state, keeping a cancel written meanwhile.
func saveQueueState(stageDir string, s *queueState) error {
	queueMu.Lock()
	defer queueMu.Unlock()
	if cur, ok := readQueueState(stageDir); ok && cur.Cancelled {
		s.Cancelled = true
	}
	return writeQueueState(stageDir, *s)
}

func writeQueueState(stageDir string, s queueState) error {
	b, _ := json.MarshalIndent(s, "", "  ")
	tmp := filepath.Join(stageDir, queueStateFile+".tmp")
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(stageDir, queueStateFile))
}

// isQueue says the stage is worked by a queue seat.
func isQueue(stageDir string) bool {
	_, ok := readQueueState(stageDir)
	return ok
}

// expandPath resolves "~/x" against the server user's home; other paths must be absolute.
func expandPath(p string) (string, error) {
	if strings.HasPrefix(p, "~/") {
		return underHome(p)
	}
	if !filepath.IsAbs(p) {
		return "", fmt.Errorf("%q must be absolute or start with ~/", p)
	}
	return filepath.Clean(p), nil
}

func blockMarks(stage int64) (string, string) {
	return fmt.Sprintf("<!-- agentflow stage %d begin -->", stage), fmt.Sprintf("<!-- agentflow stage %d end -->", stage)
}

// putBlock adds the stage's block to the queue file, keeping whatever else the file holds. A
// block that is already there (an adopted stage) is left as it is.
func putBlock(file string, stage int64, body string) error {
	begin, end := blockMarks(stage)
	old, err := os.ReadFile(file)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if strings.Contains(string(old), begin) {
		return nil
	}
	text := string(old)
	if text != "" && !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	if text != "" {
		text += "\n"
	}
	text += begin + "\n" + strings.TrimRight(body, "\n") + "\n" + end + "\n"
	return writeKeepingMode(file, text)
}

// dropBlock removes the stage's block from the queue file, and nothing else.
func dropBlock(file string, stage int64) error {
	begin, end := blockMarks(stage)
	old, err := os.ReadFile(file)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	text := string(old)
	i := strings.Index(text, begin)
	if i < 0 {
		return nil
	}
	j := strings.Index(text[i:], end)
	if j < 0 {
		return fmt.Errorf("queue file %s: stage %d's block has no end mark", file, stage)
	}
	rest := strings.TrimLeft(text[i+j+len(end):], "\n")
	head := strings.TrimRight(text[:i], "\n")
	switch {
	case head == "":
		text = rest
	case rest == "":
		text = head + "\n"
	default:
		text = head + "\n\n" + rest
	}
	return writeKeepingMode(file, text)
}

func writeKeepingMode(file, text string) error {
	mode := os.FileMode(0o600)
	if fi, err := os.Stat(file); err == nil {
		mode = fi.Mode().Perm()
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return err
	}
	tmp := file + ".agentflow-tmp"
	if err := os.WriteFile(tmp, []byte(text), mode); err != nil {
		return err
	}
	return os.Rename(tmp, file)
}

// queueTask is the block written into the queue file: where to work, the stage prompt, and how
// to report.
func queueTask(req runner.Request, result string) (string, error) {
	prompt := req.Message
	if req.PromptPath != "" {
		b, err := os.ReadFile(req.PromptPath)
		if err != nil {
			return "", fmt.Errorf("read the stage prompt: %w", err)
		}
		prompt = string(b)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# agentflow task: %s effort %d, task %s, stage %d (%s)\n\n", req.Job.Project, req.Job.Effort, req.Job.Task, req.Job.Stage, req.Role)
	fmt.Fprintf(&b, "Work only in the directory `%s` (a git worktree agentflow made for this task).", req.Workdir)
	if req.Role != "worker" && req.Role != "cleanup" {
		b.WriteString(" Do not change any file there: this is a review.")
	}
	b.WriteString(" When this task is done, agentflow removes this block from the queue file.\n\n")
	b.WriteString(prompt)
	b.WriteString(seatkit.Instructions(req.Role, result))
	return b.String(), nil
}

// nudgeWait is how long a nudged screen may stand still before the next nudge.
func nudgeWait(prof Profile) time.Duration {
	if prof.NudgeWait > 0 {
		return time.Duration(prof.NudgeWait)
	}
	return defaultNudgeWait
}

func (a *Adapter) nudgeGap() time.Duration {
	if a.NudgeGap > 0 {
		return a.NudgeGap
	}
	return 2 * time.Second
}

// nudge types the nudge text, Enter, waits, and presses Enter again, unless a halt text shows.
func (a *Adapter) nudge(ctx context.Context, m mux.Multiplexer, session string, prof Profile) error {
	if screen, err := m.Capture(ctx, session, 200); err != nil {
		return err
	} else if halted(prof, screen) != "" {
		return errHalted
	}
	text := prof.NudgeText
	if text == "" {
		text = DefaultNudgeText
	}
	if err := m.SendText(ctx, session, text); err != nil {
		return err
	}
	time.Sleep(200 * time.Millisecond) // some TUIs drop a key pressed in the same burst as a paste
	if err := m.SendKeys(ctx, session, "Enter"); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(a.nudgeGap()):
	}
	return m.SendKeys(ctx, session, "Enter")
}

// runQueue works a stage through a queue seat.
func (a *Adapter) runQueue(ctx context.Context, m mux.Multiplexer, req runner.Request, profName string, prof Profile) (runner.Result, error) {
	stage := req.Job.Stage
	st, have := readQueueState(req.StageDir)
	if have && st.Cancelled {
		return runner.Result{Incomplete: true}, context.Canceled
	}
	if !have {
		file, err := expandPath(prof.QueueFile)
		if err != nil {
			return failed(runner.ErrCrash, "seat: queue_file: "+err.Error(), err)
		}
		dir, err := filepath.Abs(filepath.Join(req.StageDir, queueDirName))
		if err != nil {
			return failed(runner.ErrCrash, "seat: stage dir", err)
		}
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return failed(runner.ErrCrash, "seat: stage dir", err)
		}
		st = queueState{Session: prof.TmuxSession, QueueFile: file, Result: filepath.Join(dir, ResultFile), Profile: profName}
		_ = os.Remove(st.Result)
		body, err := queueTask(req, st.Result)
		if err != nil {
			return failed(runner.ErrCrash, "seat: "+err.Error(), err)
		}
		if err := putBlock(file, stage, body); err != nil {
			return failed(runner.ErrCrash, "seat: write the queue file: "+err.Error(), err)
		}
		if err := saveQueueState(req.StageDir, &st); err != nil {
			_ = dropBlock(file, stage)
			return failed(runner.ErrCrash, "seat: "+err.Error(), err)
		}
		if req.OnEvent != nil {
			req.OnEvent(runner.Event{Type: "seat.queued", Text: fmt.Sprintf("task written to %s for session %s", file, st.Session)})
		}
	} else if req.OnEvent != nil {
		req.OnEvent(runner.Event{Type: "seat.adopted", Text: "queue seat " + st.Session + " adopted"})
	}
	board := a.Board
	if board == nil {
		board = &runner.SeatBoard{}
	}
	attach := "tmux attach -t " + st.Session
	resDir := filepath.Dir(st.Result)
	finish := func() {
		_ = dropBlock(st.QueueFile, stage)
		board.Clear(stage)
	}
	notice := func(kind, msg string) {
		if n, ok := board.Get(stage); ok && n.Kind == kind && n.Message == msg {
			return
		}
		board.Set(runner.SeatNotice{Stage: stage, Kind: kind, Message: msg, Since: a.now(), Session: st.Session, Attach: attach})
	}
	clearOwn := func(marker string) {
		if n, ok := board.Get(stage); ok && strings.Contains(n.Message, marker) {
			board.Clear(stage)
		}
	}
	lastScreen, lastChange := "", a.now()
	silent := false
	for {
		if rep, ok, err := readReport(resDir); err != nil {
			_ = os.Rename(st.Result, filepath.Join(resDir, fmt.Sprintf("RESULT.rejected.%d.json", a.now().UnixNano())))
			notice(runner.SeatQuestion, fmt.Sprintf("The seat wrote an unreadable %s (%s); it was set aside. Attach with `%s`", st.Result, oneLine(err.Error()), attach))
		} else if ok {
			switch rep.Status {
			case seatkit.StatusQuestion:
				st.Questions++
				st.Engaged = true // it has the task: never nudge it again
				_ = os.Rename(st.Result, filepath.Join(resDir, fmt.Sprintf("QUESTION.%d.json", st.Questions)))
				_ = os.WriteFile(filepath.Join(resDir, "QUESTION.open"), []byte(rep.Question), 0o600)
				_ = saveQueueState(req.StageDir, &st)
			default:
				final, ferr := seatkit.Final(req.Role, rep)
				if ferr != nil {
					_ = os.Rename(st.Result, filepath.Join(resDir, fmt.Sprintf("RESULT.rejected.%d.json", a.now().UnixNano())))
					notice(runner.SeatQuestion, fmt.Sprintf("agentflow rejected the seat's RESULT.json (%s). Attach with `%s`", oneLine(ferr.Error()), attach))
					break
				}
				_ = os.Rename(st.Result, filepath.Join(resDir, "RESULT.final.json"))
				finish()
				return runner.Result{Final: final}, nil
			}
		}
		questionOpen := false
		if q, err := os.ReadFile(filepath.Join(resDir, "QUESTION.open")); err == nil {
			questionOpen = true
			notice(runner.SeatQuestion, "The seat asks: "+oneLine(string(q)))
		}
		alive, err := m.Has(ctx, st.Session)
		switch {
		case err == nil && !alive:
			notice(runner.SeatQuestion, fmt.Sprintf("The seat's session %q is not running; agentflow never starts it. Start it, and the seat is nudged.", st.Session))
		case err == nil:
			clearOwn("is not running")
			screen, cerr := m.Capture(ctx, st.Session, 200)
			if cerr != nil {
				break
			}
			if h := halted(prof, screen); h != "" {
				notice(runner.SeatQuestion, fmt.Sprintf("The seat shows %q and needs the owner; agentflow types nothing until it is gone. Attach with `%s`", h, attach))
				break
			}
			clearOwn("needs the owner")
			view := stillView(prof, screen)
			if view != lastScreen {
				if lastScreen != "" && st.Nudges > 0 && !st.Engaged {
					st.Engaged = true
					_ = saveQueueState(req.StageDir, &st)
					clearOwn("nudged")
				}
				lastScreen, lastChange = view, a.now()
				if silent {
					silent = false
					clearOwn("shown nothing new")
				}
			}
			if st.Engaged || questionOpen {
				if req.Budget > 0 && !silent && a.now().Sub(lastChange) >= req.Budget {
					silent = true
					if _, ok := board.Get(stage); !ok {
						notice(runner.SeatSilent, fmt.Sprintf("The seat has shown nothing new for %s; attach with `%s`", req.Budget.Round(time.Second), attach))
					}
				}
				break
			}
			if st.Nudges > 0 && a.now().Sub(st.LastNudge) < nudgeWait(prof) {
				break
			}
			if st.Nudges >= maxNudges {
				notice(runner.SeatSilent, fmt.Sprintf("The seat was nudged %d times and its screen never changed; attach with `%s`", st.Nudges, attach))
				break
			}
			if err := a.nudge(ctx, m, st.Session, prof); err != nil {
				if ctx.Err() != nil {
					return a.queueStopped(ctx, req.StageDir)
				}
				break
			}
			st.Nudges++
			st.LastNudge = a.now()
			_ = saveQueueState(req.StageDir, &st)
			if req.OnEvent != nil {
				req.OnEvent(runner.Event{Type: "seat.nudged", Text: fmt.Sprintf("nudged %s (%d of %d)", st.Session, st.Nudges, maxNudges)})
			}
			// What the nudge itself put on the screen is the new baseline.
			if s, err := m.Capture(ctx, st.Session, 200); err == nil {
				lastScreen, lastChange = stillView(prof, s), a.now()
			}
		}
		select {
		case <-ctx.Done():
			return a.queueStopped(ctx, req.StageDir)
		case <-time.After(a.poll()):
		}
	}
}

// queueStopped ends a queue seat's call whose context is done: cancelled after a cancel,
// otherwise detached (the queue block stays and the next process adopts the stage).
func (a *Adapter) queueStopped(ctx context.Context, stageDir string) (runner.Result, error) {
	if st, ok := readQueueState(stageDir); ok && st.Cancelled {
		return runner.Result{Incomplete: true}, ctx.Err()
	}
	return runner.Result{Incomplete: true}, &runner.Error{Class: runner.ErrDetached, Msg: "queue seat keeps the task", Cause: ctx.Err()}
}

// cancelQueue removes the stage's task from the queue file and does nothing else: the
// owner's session is never touched.
func (a *Adapter) cancelQueue(stage int64, stageDir string) error {
	queueMu.Lock()
	st, ok := readQueueState(stageDir)
	if !ok {
		queueMu.Unlock()
		return nil
	}
	st.Cancelled = true
	err := writeQueueState(stageDir, st)
	queueMu.Unlock()
	if err != nil {
		return err
	}
	if a.Board != nil {
		a.Board.Clear(stage)
	}
	return dropBlock(st.QueueFile, stage)
}

// answerQueue types the owner's answer into the queue seat's session.
func (a *Adapter) answerQueue(ctx context.Context, stage int64, stageDir, text string) error {
	st, _ := readQueueState(stageDir)
	m, err := a.Mux(ctx)
	if err != nil {
		return err
	}
	prof, _ := a.profile(ctx, st.Profile)
	screen, err := m.Capture(ctx, st.Session, 200)
	if err != nil {
		return err
	}
	if h := halted(prof, screen); h != "" {
		return fmt.Errorf("seat: the session shows %q; answer it yourself with `tmux attach -t %s`, agentflow types nothing there", h, st.Session)
	}
	if err := m.SendText(ctx, st.Session, oneLine(text)); err != nil {
		return err
	}
	time.Sleep(200 * time.Millisecond)
	if err := m.SendKeys(ctx, st.Session, "Enter"); err != nil {
		return err
	}
	_ = os.Remove(filepath.Join(filepath.Dir(st.Result), "QUESTION.open"))
	if a.Board != nil {
		if n, ok := a.Board.Get(stage); ok && n.Kind == runner.SeatQuestion {
			a.Board.Clear(stage)
		}
	}
	return nil
}
