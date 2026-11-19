// Package botchatseat is the runner.Runner for a backend whose instances are BotChat seats:
// callsigns run by a person or by their own agent, costing agentflow no tokens. A stage becomes
// a BotChat task addressed to the seat's callsign carrying the whole stage prompt, the branch
// and the acceptance criteria. The stage ends when the seat posts "DONE TASK-<key> <branch>@<sha>"
// (worker) or "REVIEW TASK-<key>: PASS|CHANGES-REQUESTED" (reviewer); the adapter then makes
// sure the named commit is in the worktree, so the engine's own verify and review gates run on
// it as they do for any other runner. Agentflow never moves a task on the seat's behalf, and a
// seat that does not answer by the stage deadline fails with ErrTimeout so the stage goes to
// needs_attention instead of being retried.
package botchatseat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"cmd184psu/unified-webapp/internal/agentflow/botchat"
	"cmd184psu/unified-webapp/internal/agentflow/runner"
)

// APIType is the backend api type this adapter serves.
const APIType = "botchat_seat"

// Room is the BotChat room stage tasks are created in. One room for every seat keeps the
// seat's inbox and the reports in one place.
const Room = "agentflow seats"

// wave is the room's wave id, used only when the room is created.
const wave = "af-seats"

// Adapter is the BotChat-seat runner.Runner.
type Adapter struct {
	// Settings returns the BotChat address and the key file, read on every call so a setting
	// or key change needs no restart. Nil reads nothing and the call fails as unreachable.
	Settings func(context.Context) (url, keyFile string)
	// Poll is how often the room is read for the seat's report (default 5s).
	Poll time.Duration
	// ReadyLimit is how many tasks one seat may have open at once (default 2).
	ReadyLimit int
	// Client, when set, builds the BotChat client; tests use it.
	Client func(baseURL, keyFile string) *botchat.Client
	// Git, when set, runs git for the adapter; tests use it.
	Git func(ctx context.Context, dir string, args ...string) (string, error)

	mu      sync.Mutex
	pending map[string]int
}

var _ runner.Runner = (*Adapter)(nil)

func (a *Adapter) poll() time.Duration {
	if a.Poll > 0 {
		return a.Poll
	}
	return 5 * time.Second
}

func (a *Adapter) readyLimit() int {
	if a.ReadyLimit > 0 {
		return a.ReadyLimit
	}
	return 2
}

// Version reports what this runner is; there is no agent program or binary behind it.
func (a *Adapter) Version(context.Context) (string, error) { return "botchat seats", nil }

// Run creates the stage's task, waits for the seat's report and returns what it says.
func (a *Adapter) Run(ctx context.Context, req runner.Request) (runner.Result, error) {
	seat := strings.TrimSpace(req.Model)
	if seat == "" {
		return failed(runner.ErrCrash, "botchat seat: the backend lists no callsign")
	}
	base, keyFile := req.Backend.Host, ""
	if a.Settings != nil {
		url, kf := a.Settings(ctx)
		if base == "" {
			base = url
		}
		keyFile = kf
	}
	if base == "" || keyFile == "" {
		return failed(runner.ErrUnreachable, "botchat seat: botchat_url or botchat_key_file is not set")
	}
	c := &botchat.Client{BaseURL: base, KeyFile: keyFile}
	if a.Client != nil {
		c = a.Client(base, keyFile)
	}

	prompt, err := stagePrompt(req)
	if err != nil {
		return failed(runner.ErrCrash, err.Error())
	}
	release, err := a.reserve(seat)
	if err != nil {
		return runner.Result{}, err
	}
	defer release()

	key := stageKey(req)
	branch, sha := a.head(ctx, req.Workdir)
	room, err := c.Room(ctx, Room, wave, botchat.Coordinator)
	if err != nil {
		return failed(runner.ErrUnreachable, "botchat seat: no room: "+err.Error())
	}
	taskID, err := c.CreateTask(ctx, map[string]any{
		"from":               botchat.Coordinator,
		"roomId":             room,
		"assignee":           seat,
		"title":              "TASK-" + key + " " + stageTitle(req),
		"objective":          objective(prompt, key, seat, req, branch, sha),
		"acceptanceCriteria": "Every acceptance criterion in the objective is met and the work is committed on the branch named there.",
		"exclusions":         "One branch per stage: the task's own branch only.",
	})
	if err != nil {
		return failed(runner.ErrUnreachable, "botchat seat: no task: "+err.Error())
	}
	// The board may already have moved it by hand; a refused transition is not a failure.
	_, _ = c.Transition(ctx, taskID, botchat.Coordinator, "promote", nil)
	if err := c.Post(ctx, room, botchat.Coordinator, seat,
		fmt.Sprintf("TASK-%s is yours: %s. Report `DONE TASK-%s <branch>@<sha>` when it is done, or `REVIEW TASK-%s: PASS` / `REVIEW TASK-%s: CHANGES-REQUESTED` after a review.", key, stageTitle(req), key, key, key)); err != nil {
		return failed(runner.ErrUnreachable, "botchat seat: no announcement: "+err.Error())
	}

	for {
		rep, err := a.report(ctx, c, room, seat, key)
		if err != nil {
			return failed(runner.ErrUnreachable, "botchat seat: no report: "+err.Error())
		}
		if rep != nil {
			return a.finish(ctx, *rep, key, req)
		}
		select {
		case <-ctx.Done():
			return runner.Result{Incomplete: true}, &runner.Error{
				Class: runner.ErrTimeout,
				Msg:   "seat " + seat + " did not report on TASK-" + key,
				Cause: ctx.Err(),
			}
		case <-time.After(a.poll()):
		}
	}
}

func failed(class runner.ErrClass, msg string) (runner.Result, error) {
	return runner.Result{}, &runner.Error{Class: class, Msg: msg}
}

// reserve counts the open task of a seat and refuses one when the seat is at its limit; the
// returned function gives the slot back. The engine's slots already cap concurrency, but a
// seat is a person's queue, so a stage pauses here rather than piling work on it.
func (a *Adapter) reserve(seat string) (func(), error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.pending == nil {
		a.pending = map[string]int{}
	}
	if a.pending[seat] >= a.readyLimit() {
		return nil, &runner.Error{Class: runner.ErrRateLimit,
			Msg: fmt.Sprintf("seat %s already has %d open tasks", seat, a.pending[seat])}
	}
	a.pending[seat]++
	var once sync.Once
	return func() {
		once.Do(func() {
			a.mu.Lock()
			a.pending[seat]--
			a.mu.Unlock()
		})
	}, nil
}

// stageKey is the task key the seat quotes back: the queue task's key when there is one, else
// the stage id, which is unique for the life of the database.
func stageKey(req runner.Request) string {
	if k := strings.TrimSpace(req.Job.Task); k != "" {
		return sanitize(k)
	}
	return strconv.FormatInt(req.Job.Stage, 10)
}

var notKey = regexp.MustCompile(`[^A-Za-z0-9_-]+`)

func sanitize(s string) string { return notKey.ReplaceAllString(s, "-") }

// stageTitle is the task's board title: where in the effort the stage sits.
func stageTitle(req runner.Request) string {
	title := req.Role
	if req.Job.Effort > 0 {
		title = fmt.Sprintf("effort %d · %s", req.Job.Effort, req.Role)
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
// report. The prompt already carries the task's acceptance criteria, so they travel with it.
func objective(prompt, key, seat string, req runner.Request, branch, sha string) string {
	var b strings.Builder
	b.WriteString(prompt)
	b.WriteString("\n\n---\nA BotChat seat task, created by agentflow.\n")
	fmt.Fprintf(&b, "Callsign: %s\nStage: %s (effort %d, stage %d)\n", seat, req.Role, req.Job.Effort, req.Job.Stage)
	if req.Workdir != "" {
		fmt.Fprintf(&b, "Worktree: %s\n", req.Workdir)
	}
	fmt.Fprintf(&b, "Branch: %s@%s\n", branch, sha)
	b.WriteString("Commit your work on that branch where agentflow can fetch it.\n")
	fmt.Fprintf(&b, "Report when done, on its own line: DONE TASK-%s %s@<sha>\n", key, branch)
	fmt.Fprintf(&b, "Or after a review: REVIEW TASK-%s: PASS, or REVIEW TASK-%s: CHANGES-REQUESTED\n", key, key)
	b.WriteString("Agentflow fetches the commit you name and runs its own verify and review gates; it never moves this task for you.\n")
	return b.String()
}

var (
	doneLine   = regexp.MustCompile(`(?m)^\s*DONE\s+TASK-(\S+)\s+(\S+)@([0-9a-fA-F]{7,40})\s*$`)
	reviewLine = regexp.MustCompile(`(?mi)^\s*REVIEW\s+TASK-(\S+?):\s*(PASS|CHANGES[-_ ]REQUESTED)\s*$`)
)

// report is what a seat said about a task.
type report struct {
	branch  string
	sha     string
	verdict string
}

// parseReport finds this task's DONE or REVIEW line in a message; ok is false when the message
// says nothing about key.
func parseReport(content, key string) (report, bool) {
	if m := doneLine.FindStringSubmatch(content); m != nil && m[1] == key {
		return report{branch: m[2], sha: m[3]}, true
	}
	if m := reviewLine.FindStringSubmatch(content); m != nil && m[1] == key {
		return report{verdict: strings.ToUpper(strings.ReplaceAll(m[2], "_", "-"))}, true
	}
	return report{}, false
}

// report reads the room and returns the first report from the seat about this task.
func (a *Adapter) report(ctx context.Context, c *botchat.Client, room, seat, key string) (*report, error) {
	msgs, err := c.Messages(ctx, room, 50)
	if err != nil {
		return nil, err
	}
	for i := len(msgs) - 1; i >= 0; i-- {
		m := msgs[i]
		if m.Sender() != seat {
			continue
		}
		if rep, ok := parseReport(m.Content, key); ok {
			return &rep, nil
		}
	}
	return nil, nil
}

// finish turns the seat's report into the stage's result. A worker's report names a commit,
// which must be in the worktree before the verify stage looks at it; a reviewer's report
// becomes the verdict JSON the engine's review parsing expects.
func (a *Adapter) finish(ctx context.Context, rep report, key string, req runner.Request) (runner.Result, error) {
	if rep.sha != "" {
		if err := a.have(ctx, req.Workdir, rep.sha); err != nil {
			return failed(runner.ErrCrash, err.Error())
		}
		return runner.Result{Final: fmt.Sprintf("DONE TASK-%s %s@%s", key, rep.branch, rep.sha)}, nil
	}
	verdict, issues := "pass", []string{}
	if strings.HasPrefix(rep.verdict, "CHANGES") {
		verdict = "repair"
		issues = append(issues, "the seat requested changes; see TASK-"+key+" on BotChat")
	}
	out := map[string]any{"verdict": verdict, "summary": "seat review of TASK-" + key, "issues": issues}
	b, err := json.Marshal(out)
	if err != nil {
		return failed(runner.ErrCrash, "botchat seat: render the verdict: "+err.Error())
	}
	return runner.Result{Final: string(b)}, nil
}

// head is the worktree's branch and commit, which the task tells the seat to build on. Unknown
// values are left empty rather than failing the stage: a plan stage may run outside a worktree.
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

// have makes sure sha is in the worktree, fetching when the seat pushed to a remote agentflow
// already knows. A commit that never arrives is a stage failure, not a silent empty verify.
func (a *Adapter) have(ctx context.Context, dir, sha string) error {
	if dir == "" {
		return errors.New("botchat seat: no worktree to bring the seat's commit into")
	}
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
