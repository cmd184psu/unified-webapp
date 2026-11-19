// Package botchatseat is the runner.Runner for a backend whose instances are BotChat seats:
// callsigns run by a person or by their own agent, costing agentflow no tokens. A stage becomes
// a BotChat task addressed to the seat's callsign carrying the whole stage prompt and the
// worktree. The seat works in the worktree without committing and reports on the room, naming
// the BotChat task id it was given:
//
//	DONE TASK-<id>                                (worker; "<branch>@<sha>" may follow)
//	REVIEW TASK-<id>: PASS | CHANGES-REQUESTED    (reviewer)
//
// or moves the task on the board: done ends a worker stage, blocked puts the stage in front
// of the owner, cancelled ends it as blocked. Only messages posted after the task was
// announced count, so an old report about another stage can never end this one.
//
// The task id, the room and the read cursor are kept in the stage directory, so a restart
// adopts the open task instead of creating another. A seat is never retried for being slow:
// past the role's budget it is reported as needing attention and the stage keeps waiting.
// How many open tasks a seat may hold is the backend's slots, counted from BotChat itself.
package botchatseat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"cmd184psu/unified-webapp/internal/agentflow/botchat"
	"cmd184psu/unified-webapp/internal/agentflow/runner"
	"cmd184psu/unified-webapp/internal/agentflow/runner/seatkit"
)

// APIType is the backend api type this adapter serves.
const APIType = "botchat_seat"

// Room is the BotChat room stage tasks are created in. One room for every seat keeps the
// seat's inbox and the reports in one place.
const Room = "agentflow seats"

// wave is the room's wave id, used only when the room is created.
const wave = "af-seats"

// stateFile holds the stage's BotChat task, in the stage directory.
const stateFile = "botchat-seat.json"

// Adapter is the BotChat-seat runner.Runner.
type Adapter struct {
	// Settings returns the BotChat address and the key file, read on every call so a setting
	// or key change needs no restart. Nil reads nothing and the call fails as unreachable.
	Settings func(context.Context) (url, keyFile string)
	// Poll is how often the room and the task are read (default 5s).
	Poll time.Duration
	// Board receives the stage's question and silence notices.
	Board *runner.SeatBoard
	// Client, when set, builds the BotChat client; tests use it.
	Client func(baseURL, keyFile string) *botchat.Client
	// Git, when set, runs git for the adapter; tests use it.
	Git   func(ctx context.Context, dir string, args ...string) (string, error)
	Clock func() time.Time

	boardOnce sync.Once
}

var _ runner.Runner = (*Adapter)(nil)

func (a *Adapter) poll() time.Duration {
	if a.Poll > 0 {
		return a.Poll
	}
	return 5 * time.Second
}

func (a *Adapter) now() time.Time {
	if a.Clock != nil {
		return a.Clock()
	}
	return time.Now()
}

func (a *Adapter) board() *runner.SeatBoard {
	a.boardOnce.Do(func() {
		if a.Board == nil {
			a.Board = &runner.SeatBoard{}
		}
	})
	return a.Board
}

// Version reports what this runner is; there is no agent program or binary behind it.
func (a *Adapter) Version(context.Context) (string, error) { return "botchat seats", nil }

// state is the stage's BotChat task, kept so a restart adopts it.
type state struct {
	TaskID    string    `json:"task_id"`
	Room      string    `json:"room"`
	Seat      string    `json:"seat"`
	Floor     int64     `json:"floor"`
	Cursor    int64     `json:"cursor"`
	Created   time.Time `json:"created"`
	Cancelled bool      `json:"cancelled,omitempty"`
}

func readState(stageDir string) (state, bool) {
	var s state
	if stageDir == "" {
		return s, false
	}
	b, err := os.ReadFile(filepath.Join(stageDir, stateFile))
	if err != nil || json.Unmarshal(b, &s) != nil || s.TaskID == "" {
		return s, false
	}
	return s, true
}

func writeState(stageDir string, s state) error {
	if stageDir == "" {
		return nil
	}
	b, _ := json.MarshalIndent(s, "", "  ")
	tmp := filepath.Join(stageDir, stateFile+".tmp")
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(stageDir, stateFile))
}

func (a *Adapter) client(ctx context.Context, req runner.Request) (*botchat.Client, error) {
	base, keyFile := req.Backend.Host, ""
	if a.Settings != nil {
		url, kf := a.Settings(ctx)
		if base == "" {
			base = url
		}
		keyFile = kf
	}
	if base == "" || keyFile == "" {
		return nil, &runner.Error{Class: runner.ErrUnreachable, Msg: "botchat seat: botchat_url or botchat_key_file is not set"}
	}
	if a.Client != nil {
		return a.Client(base, keyFile), nil
	}
	return &botchat.Client{BaseURL: base, KeyFile: keyFile}, nil
}

// Run creates the stage's task (or adopts the one a previous process made), waits for the
// seat's report and returns what it says.
func (a *Adapter) Run(ctx context.Context, req runner.Request) (runner.Result, error) {
	seat := strings.TrimSpace(req.Model)
	if seat == "" {
		return failed(runner.ErrCrash, "botchat seat: the backend lists no callsign")
	}
	c, err := a.client(ctx, req)
	if err != nil {
		return runner.Result{}, err
	}
	st, adopted := readState(req.StageDir)
	if adopted && st.Seat != seat {
		adopted = false
	}
	if !adopted {
		if st, err = a.create(ctx, c, seat, req); err != nil {
			var re *runner.Error
			if errors.As(err, &re) {
				return runner.Result{}, re
			}
			return failed(runner.ErrUnreachable, "botchat seat: "+err.Error())
		}
	}
	if req.OnEvent != nil {
		verb := "created"
		if adopted {
			verb = "adopted"
		}
		req.OnEvent(runner.Event{Type: "seat." + verb, Text: fmt.Sprintf("BotChat task %s %s for seat %s", st.TaskID, verb, seat)})
	}
	return a.wait(ctx, c, req, st)
}

// openStates are the task states in which the seat still owes work.
var openStates = map[string]bool{"assigned": true, "acknowledged": true, "working": true, "blocked": true, "rework": true, "ready": true}

// create checks the seat's open task count against the backend's slots, then makes the task
// and announces it. The read floor is the room's newest seq before the announcement.
func (a *Adapter) create(ctx context.Context, c *botchat.Client, seat string, req runner.Request) (state, error) {
	prompt, err := stagePrompt(req)
	if err != nil {
		return state{}, &runner.Error{Class: runner.ErrCrash, Msg: err.Error()}
	}
	room, err := c.Room(ctx, Room, wave, botchat.Coordinator)
	if err != nil {
		return state{}, fmt.Errorf("no room: %w", err)
	}
	slots := req.Backend.Slots
	if slots < 1 {
		slots = 1
	}
	tasks, err := c.ListTasks(ctx, room, seat)
	if err != nil {
		return state{}, fmt.Errorf("list the seat's tasks: %w", err)
	}
	open := 0
	for _, t := range tasks {
		if openStates[strings.ToLower(t.State)] {
			open++
		}
	}
	if open >= slots {
		return state{}, &runner.Error{Class: runner.ErrRateLimit,
			Msg: fmt.Sprintf("seat %s already has %d open tasks (slots %d)", seat, open, slots)}
	}
	floor := int64(0)
	if msgs, err := c.Messages(ctx, room, 1); err == nil && len(msgs) > 0 {
		floor = msgs[len(msgs)-1].Seq
	}
	branch, sha := a.head(ctx, req.Workdir)
	taskID, err := c.CreateTask(ctx, map[string]any{
		"from":               botchat.Coordinator,
		"roomId":             room,
		"assignee":           seat,
		"title":              stageTitle(req),
		"objective":          objective(prompt, seat, req, branch, sha),
		"acceptanceCriteria": "Every acceptance criterion in the objective is met, with the changes left uncommitted in the worktree named there.",
		"exclusions":         "Do not commit, push or switch branches; agentflow commits the worktree.",
	})
	if err != nil {
		return state{}, fmt.Errorf("no task: %w", err)
	}
	if taskID == "" {
		return state{}, errors.New("the new task came back without an id")
	}
	st := state{TaskID: taskID, Room: room, Seat: seat, Floor: floor, Cursor: floor, Created: a.now()}
	if err := writeState(req.StageDir, st); err != nil {
		return state{}, &runner.Error{Class: runner.ErrCrash, Msg: "botchat seat: state: " + err.Error()}
	}
	// The task exists now: finish announcing it even when the call is being stopped, so an
	// adopted stage never waits on a task its seat was never told about.
	bg := context.WithoutCancel(ctx)
	// The board may already have moved it by hand; a refused transition is not a failure.
	_, _ = c.Transition(bg, taskID, botchat.Coordinator, "promote", nil)
	if err := c.Post(bg, room, botchat.Coordinator, seat, announcement(taskID, req)); err != nil {
		return state{}, fmt.Errorf("no announcement: %w", err)
	}
	return st, nil
}

func announcement(taskID string, req runner.Request) string {
	if req.Role == "reviewer" {
		return fmt.Sprintf("%s is yours: %s. Read the task, review the worktree, then post `REVIEW %s: PASS` or `REVIEW %s: CHANGES-REQUESTED` with what to fix on the next lines.", taskID, stageTitle(req), taskID, taskID)
	}
	return fmt.Sprintf("%s is yours: %s. Work in the worktree named in the task without committing, then post `DONE %s` (or mark the task done).", taskID, stageTitle(req), taskID)
}

// wait polls the room after the floor and the task's state until the seat answers.
func (a *Adapter) wait(ctx context.Context, c *botchat.Client, req runner.Request, st state) (runner.Result, error) {
	stage := req.Job.Stage
	board := a.board()
	started := st.Created
	if started.IsZero() {
		started = a.now()
	}
	lastSeen := a.now() // anything the seat posts or any state change resets the silence clock
	lastState := ""
	defer func() {
		if ctx.Err() == nil {
			board.Clear(stage)
		}
	}()
	for {
		msgs, err := c.MessagesAfter(ctx, st.Room, st.Cursor, 200)
		if err == nil {
			for _, m := range msgs {
				if m.Seq > st.Cursor {
					st.Cursor = m.Seq
				}
				if m.Sender() != st.Seat {
					continue
				}
				lastSeen = a.now()
				if rep, ok := parseReport(m.Content, st.TaskID); ok {
					a.saveCursor(req.StageDir, st)
					return a.finish(ctx, c, rep, st, req)
				}
			}
			a.saveCursor(req.StageDir, st)
		}
		if t, err := c.Task(ctx, st.TaskID); err == nil {
			s := strings.ToLower(t.State)
			if s != lastState {
				lastState, lastSeen = s, a.now()
			}
			switch s {
			case "done", "accepted", "closed":
				if req.Role == "worker" || req.Role == "cleanup" {
					return a.finish(ctx, c, report{status: seatkit.StatusDone, summary: "the seat marked " + st.TaskID + " " + s}, st, req)
				}
				board.Set(runner.SeatNotice{Stage: stage, Kind: runner.SeatQuestion, Since: a.now(),
					Message: fmt.Sprintf("Seat %s marked %s %s without posting a REVIEW verdict", st.Seat, st.TaskID, s)})
			case "cancelled", "canceled":
				return a.finish(ctx, c, report{status: seatkit.StatusBlocked, summary: "the seat's task " + st.TaskID + " was cancelled on BotChat"}, st, req)
			case "blocked":
				why := strings.TrimSpace(t.Blocker)
				if why == "" {
					why = "no reason given"
				}
				board.Set(runner.SeatNotice{Stage: stage, Kind: runner.SeatQuestion, Since: a.now(),
					Message: fmt.Sprintf("Seat %s is blocked on %s: %s", st.Seat, st.TaskID, why)})
			default:
				if n, ok := board.Get(stage); ok && n.Kind == runner.SeatQuestion {
					board.Clear(stage)
				}
			}
		}
		if req.Budget > 0 && a.now().Sub(lastSeen) >= req.Budget {
			if _, ok := board.Get(stage); !ok {
				board.Set(runner.SeatNotice{Stage: stage, Kind: runner.SeatSilent, Since: a.now(),
					Message: fmt.Sprintf("Seat %s has said nothing on %s for %s", st.Seat, st.TaskID, req.Budget.Round(time.Second))})
			}
		} else if n, ok := board.Get(stage); ok && n.Kind == runner.SeatSilent {
			board.Clear(stage)
		}
		select {
		case <-ctx.Done():
			if cur, ok := readState(req.StageDir); ok && cur.Cancelled {
				return runner.Result{Incomplete: true}, ctx.Err()
			}
			// The task stays open on BotChat; the next process adopts it.
			return runner.Result{Incomplete: true}, &runner.Error{Class: runner.ErrDetached,
				Msg: "seat " + st.Seat + " keeps " + st.TaskID + " (since " + started.Format(time.RFC3339) + ")", Cause: ctx.Err()}
		case <-time.After(a.poll()):
		}
	}
}

func failed(class runner.ErrClass, msg string) (runner.Result, error) {
	return runner.Result{}, &runner.Error{Class: class, Msg: msg}
}

// stageTitle is the task's board title: where in the effort the stage sits.
func stageTitle(req runner.Request) string {
	title := req.Role
	if req.Job.Task != "" {
		title = req.Job.Task + " " + req.Role
	}
	if req.Job.Effort > 0 {
		title = fmt.Sprintf("effort %d · %s", req.Job.Effort, title)
	}
	if req.Job.Stage > 0 {
		title += " (stage " + strconv.FormatInt(req.Job.Stage, 10) + ")"
	}
	return title
}

func stagePrompt(req runner.Request) (string, error) {
	if req.PromptPath == "" {
		return req.Message, nil
	}
	b, err := os.ReadFile(req.PromptPath)
	if err != nil {
		return "", fmt.Errorf("botchat seat: read the stage prompt: %w", err)
	}
	return string(b), nil
}

// objective is the task's text: the stage prompt verbatim, then where to work and how to
// report. The report names the BotChat task id, which the seat sees on its board.
func objective(prompt, seat string, req runner.Request, branch, sha string) string {
	var b strings.Builder
	b.WriteString(prompt)
	b.WriteString("\n\n---\nA BotChat seat task, created by agentflow.\n")
	fmt.Fprintf(&b, "Callsign: %s\nStage: %s (effort %d, stage %d)\n", seat, req.Role, req.Job.Effort, req.Job.Stage)
	if req.Workdir != "" {
		fmt.Fprintf(&b, "Worktree: %s\n", req.Workdir)
	}
	if branch != "" {
		fmt.Fprintf(&b, "Branch: %s@%s\n", branch, sha)
	}
	b.WriteString("Do not commit, push or switch branches: leave your changes in the worktree. Agentflow commits them and runs its own verify and review gates.\n")
	b.WriteString("Report on the room, naming this task's id as your board shows it:\n")
	b.WriteString("  worker:   DONE TASK-<id>   (or mark the task done)\n")
	b.WriteString("  reviewer: REVIEW TASK-<id>: PASS, or REVIEW TASK-<id>: CHANGES-REQUESTED with what to fix on the following lines\n")
	b.WriteString("If you need the owner, block the task with the exact question.\n")
	return b.String()
}

var (
	doneLine   = regexp.MustCompile(`(?m)^\s*DONE\s+(TASK-[A-Za-z0-9_-]+)(?:\s+(\S+)@([0-9a-fA-F]{7,40}))?\s*$`)
	reviewLine = regexp.MustCompile(`(?mi)^\s*REVIEW\s+(TASK-[A-Za-z0-9_-]+?):\s*(PASS|CHANGES[-_ ]REQUESTED)\s*$`)
)

// report is what a seat said about a task.
type report struct {
	status  string
	summary string
	branch  string
	sha     string
	verdict string
	notes   string
}

// parseReport finds the task's DONE or REVIEW line in a message; ok is false when the message
// says nothing about taskID. The id is matched exactly as BotChat issued it.
func parseReport(content, taskID string) (report, bool) {
	if m := doneLine.FindStringSubmatch(content); m != nil && strings.EqualFold(m[1], taskID) {
		return report{status: seatkit.StatusDone, branch: m[2], sha: m[3], summary: strings.TrimSpace(content)}, true
	}
	if loc := reviewLine.FindStringSubmatchIndex(content); loc != nil {
		m := reviewLine.FindStringSubmatch(content)
		if strings.EqualFold(m[1], taskID) {
			v := strings.ToUpper(strings.ReplaceAll(strings.ReplaceAll(m[2], "_", "-"), " ", "-"))
			return report{status: seatkit.StatusDone, verdict: v, notes: strings.TrimSpace(content[loc[1]:])}, true
		}
	}
	return report{}, false
}

// finish turns the seat's report into the stage's result and closes the BotChat task: the
// stage consumed it, and an open task would hold one of the seat's slots forever.
func (a *Adapter) finish(ctx context.Context, c *botchat.Client, rep report, st state, req runner.Request) (runner.Result, error) {
	r := seatkit.Report{Status: rep.status, Summary: rep.summary, Branch: rep.branch, Commit: rep.sha}
	if rep.status == seatkit.StatusBlocked {
		r.Blocked = rep.summary
	}
	if req.Role == "reviewer" && rep.status == seatkit.StatusDone {
		r.Summary = "seat review of " + st.TaskID
		if strings.HasPrefix(rep.verdict, "CHANGES") {
			r.Verdict = "repair"
			notes := rep.notes
			if notes == "" {
				notes = "the seat requested changes; see " + st.TaskID + " on BotChat"
			}
			r.Issues = []seatkit.Issue{{Description: notes, Severity: "major"}}
			r.RepairPrompt = notes
		} else {
			r.Verdict = "pass"
		}
	}
	if rep.sha != "" && req.Workdir != "" {
		// A seat that committed anyway: make sure the commit is reachable, so nothing is lost
		// when the git policy puts the branch back.
		_ = a.have(ctx, req.Workdir, rep.sha)
	}
	final, err := seatkit.Final(req.Role, r)
	if err != nil {
		return failed(runner.ErrCrash, "botchat seat: "+err.Error())
	}
	bg := context.WithoutCancel(ctx)
	if rep.status == seatkit.StatusDone {
		_, _ = c.Transition(bg, st.TaskID, botchat.Coordinator, "accept", nil)
		_, _ = c.Transition(bg, st.TaskID, botchat.Coordinator, "close", nil)
	}
	_ = os.Rename(filepath.Join(req.StageDir, stateFile), filepath.Join(req.StageDir, stateFile+".done"))
	a.board().Clear(req.Job.Stage)
	return runner.Result{Final: final}, nil
}

// Alive says the stage has an open BotChat task this process may adopt.
func (a *Adapter) Alive(_ context.Context, stageDir string) bool {
	st, ok := readState(stageDir)
	return ok && !st.Cancelled
}

// Cancel cancels the stage's BotChat task; the running call then returns as cancelled.
func (a *Adapter) Cancel(ctx context.Context, stage int64, stageDir string) error {
	st, ok := readState(stageDir)
	if !ok {
		return nil
	}
	st.Cancelled = true
	if err := writeState(stageDir, st); err != nil {
		return err
	}
	a.board().Clear(stage)
	if a.Settings == nil {
		return nil
	}
	url, kf := a.Settings(ctx)
	if url == "" || kf == "" {
		return nil
	}
	c := &botchat.Client{BaseURL: url, KeyFile: kf}
	if a.Client != nil {
		c = a.Client(url, kf)
	}
	_, err := c.Transition(ctx, st.TaskID, botchat.Coordinator, "cancel", nil)
	return err
}

// Answer posts the owner's answer to the seat on the room, and answers the block when the
// task is blocked on the coordinator.
func (a *Adapter) Answer(ctx context.Context, stage int64, stageDir, text string) error {
	st, ok := readState(stageDir)
	if !ok {
		return errors.New("botchat seat: the stage has no open task")
	}
	if a.Settings == nil {
		return errors.New("botchat seat: no BotChat settings")
	}
	url, kf := a.Settings(ctx)
	c := &botchat.Client{BaseURL: url, KeyFile: kf}
	if a.Client != nil {
		c = a.Client(url, kf)
	}
	if t, err := c.Task(ctx, st.TaskID); err == nil && strings.EqualFold(t.State, "blocked") && strings.EqualFold(t.WaitingOn, botchat.Coordinator) {
		_, _ = c.Transition(ctx, st.TaskID, botchat.Coordinator, "answer", map[string]any{"note": text})
	}
	if err := c.Post(ctx, st.Room, botchat.Coordinator, st.Seat, st.TaskID+": "+text); err != nil {
		return err
	}
	a.board().Clear(stage)
	return nil
}

// head is the worktree's branch and commit, which the task tells the seat to work on.
func (a *Adapter) head(ctx context.Context, dir string) (branch, sha string) {
	if dir == "" {
		return "", ""
	}
	if out, err := a.git(ctx, dir, "rev-parse", "--abbrev-ref", "HEAD"); err == nil {
		branch = out
	}
	if out, err := a.git(ctx, dir, "rev-parse", "HEAD"); err == nil {
		sha = out
	}
	return branch, sha
}

// have makes sure sha is in the worktree's repository, fetching when the seat pushed it.
func (a *Adapter) have(ctx context.Context, dir, sha string) error {
	if _, err := a.git(ctx, dir, "cat-file", "-e", sha+"^{commit}"); err == nil {
		return nil
	}
	if _, err := a.git(ctx, dir, "fetch", "--all", "--quiet"); err != nil {
		return fmt.Errorf("botchat seat: fetch: %w", err)
	}
	if _, err := a.git(ctx, dir, "cat-file", "-e", sha+"^{commit}"); err != nil {
		return fmt.Errorf("botchat seat: the reported commit %s is not in %s", sha, dir)
	}
	return nil
}

func (a *Adapter) git(ctx context.Context, dir string, args ...string) (string, error) {
	if a.Git != nil {
		return a.Git(ctx, dir, args...)
	}
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// saveCursor stores how far the room has been read, keeping a cancel written meanwhile.
func (a *Adapter) saveCursor(stageDir string, st state) {
	cur, ok := readState(stageDir)
	if !ok || cur.TaskID != st.TaskID {
		return // finished or replaced
	}
	cur.Cursor = st.Cursor
	_ = writeState(stageDir, cur)
}
