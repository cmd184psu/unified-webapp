package seat

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cmd184psu/unified-webapp/internal/agentflow/runner"
	"cmd184psu/unified-webapp/internal/agentflow/runner/mux"
	"cmd184psu/unified-webapp/internal/agentflow/schema"
)

const ownerSession = "owner-seat"

// queueSeat starts a fake owner-run session (as the owner would) and an adapter whose "q"
// profile is a queue seat on it. The queue file already holds the owner's own brief.
func queueSeat(t *testing.T, mode string) (*Adapter, *mux.Fake, string) {
	t.Helper()
	fm := &mux.Fake{}
	t.Cleanup(fm.KillAll)
	queue := filepath.Join(t.TempDir(), "seat.md")
	if err := os.WriteFile(queue, []byte("# The owner's brief\n\nKeep this.\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	script, err := filepath.Abs("testdata/queueseat.sh")
	if err != nil {
		t.Fatal(err)
	}
	if err := fm.Create(context.Background(), mux.Spec{Name: ownerSession, Argv: []string{"/bin/sh", script},
		Env: []string{"QSEAT_MODE=" + mode, "QUEUE_FILE=" + queue, "PATH=/usr/bin:/bin"}}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the owner's session", func() bool {
		s, _ := fm.Capture(context.Background(), ownerSession, 0)
		return strings.Contains(s, "owner seat idle")
	})
	a := &Adapter{
		Mux: func(context.Context) (mux.Multiplexer, error) { return fm, nil },
		Profiles: func(context.Context) map[string]Profile {
			return map[string]Profile{"q": {TmuxSession: ownerSession, QueueFile: queue, NudgeWait: Duration(150 * time.Millisecond),
				HaltTexts: []string{"in use elsewhere"}}}
		},
		Instance: "test-instance",
		Board:    &runner.SeatBoard{},
		Poll:     5 * time.Millisecond,
		NudgeGap: time.Millisecond,
	}
	return a, fm, queue
}

func queueReq(t *testing.T, role string) runner.Request {
	req := seatReq(t, role)
	req.Model = "q"
	return req
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestQueueSeatIsNudgedAndReports(t *testing.T) {
	a, fm, queue := queueSeat(t, "react")
	req := queueReq(t, "worker")
	res, err := a.Run(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	w, err := schema.Parse[schema.WorkerOut](json.RawMessage(res.Final))
	if err != nil || w.Summary != "queue task done" {
		t.Fatalf("final %s: %v", res.Final, err)
	}
	typed := strings.Join(fm.TypedInto(ownerSession), "|")
	if typed != "check your queue|<Enter>|<Enter>" {
		t.Fatalf("typed %q, want the nudge text, Enter, Enter", typed)
	}
	if got := readFile(t, queue); got != "# The owner's brief\n\nKeep this.\n" {
		t.Fatalf("queue file after the stage = %q: the block must go and the owner's text stay", got)
	}
	if fi, _ := os.Stat(queue); fi.Mode().Perm() != 0o640 {
		t.Fatalf("queue file mode %v changed", fi.Mode().Perm())
	}
	if alive, _ := fm.Has(context.Background(), ownerSession); !alive {
		t.Fatal("the owner's session must never be killed")
	}
}

func TestQueueBlockNamesTheWorktreeAndAnAbsoluteResult(t *testing.T) {
	a, _, queue := queueSeat(t, "deaf")
	req := queueReq(t, "worker")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := runAsync(a, ctx, req)
	waitFor(t, "the block", func() bool {
		b, _ := os.ReadFile(queue)
		return strings.Contains(string(b), "agentflow stage 42 begin")
	})
	text := readFile(t, queue)
	if !strings.HasPrefix(text, "# The owner's brief\n\nKeep this.\n\n<!-- agentflow stage 42 begin -->") || !strings.Contains(text, "`"+req.Workdir+"`") || !strings.Contains(text, "Add a feature.") {
		t.Fatalf("queue file = %q", text)
	}
	st, _ := readQueueState(req.StageDir)
	if !filepath.IsAbs(st.Result) || !strings.Contains(text, st.Result) {
		t.Fatalf("result path %q must be absolute and in the block", st.Result)
	}
	cancel()
	get(t, ch)
}

func TestADeafQueueSeatIsNudgedThreeTimesThenNeedsAttention(t *testing.T) {
	a, fm, _ := queueSeat(t, "deaf")
	req := queueReq(t, "worker")
	ctx, cancel := context.WithCancel(context.Background())
	ch := runAsync(a, ctx, req)
	waitFor(t, "the attention notice", func() bool {
		n, ok := a.Board.Get(42)
		return ok && n.Kind == runner.SeatSilent && strings.Contains(n.Message, "nudged 3 times")
	})
	time.Sleep(300 * time.Millisecond) // more than one nudge wait: no fourth nudge
	if n := strings.Count(strings.Join(fm.TypedInto(ownerSession), "|"), "check your queue"); n != 3 {
		t.Fatalf("nudged %d times, want 3", n)
	}
	cancel()
	var re *runner.Error
	if o := get(t, ch); !errors.As(o.err, &re) || re.Class != runner.ErrDetached {
		t.Fatalf("a stopped queue seat call detaches (the task stays queued): %v", o.err)
	}
}

func TestAQueueSeatShowingAHaltTextGetsNoKeys(t *testing.T) {
	a, fm, _ := queueSeat(t, "halt")
	req := queueReq(t, "worker")
	ctx, cancel := context.WithCancel(context.Background())
	ch := runAsync(a, ctx, req)
	waitFor(t, "the halt notice", func() bool {
		n, ok := a.Board.Get(42)
		return ok && n.Kind == runner.SeatQuestion && strings.Contains(n.Message, "in use elsewhere")
	})
	time.Sleep(100 * time.Millisecond)
	if typed := fm.TypedInto(ownerSession); len(typed) != 0 {
		t.Fatalf("typed %q into a session showing a halt text", typed)
	}
	if err := a.Answer(context.Background(), 42, req.StageDir, "yes"); err == nil {
		t.Fatal("an answer must be refused while the halt text shows")
	}
	cancel()
	get(t, ch)
	if typed := fm.TypedInto(ownerSession); len(typed) != 0 {
		t.Fatalf("typed %q", typed)
	}
}

func TestQueueSeatQuestionAndAnswer(t *testing.T) {
	a, _, _ := queueSeat(t, "question")
	req := queueReq(t, "worker")
	ch := runAsync(a, context.Background(), req)
	waitFor(t, "the question", func() bool {
		n, ok := a.Board.Get(42)
		return ok && n.Kind == runner.SeatQuestion && strings.Contains(n.Message, "tabs or spaces?")
	})
	if err := a.Answer(context.Background(), 42, req.StageDir, "tabs"); err != nil {
		t.Fatal(err)
	}
	o := get(t, ch)
	if o.err != nil || !strings.Contains(o.res.Final, "used tabs") {
		t.Fatalf("%s %v", o.res.Final, o.err)
	}
}

func TestCancellingAQueueSeatOnlyRemovesItsTask(t *testing.T) {
	a, fm, queue := queueSeat(t, "deaf")
	req := queueReq(t, "worker")
	ctx, cancel := context.WithCancel(context.Background())
	ch := runAsync(a, ctx, req)
	waitFor(t, "a nudge", func() bool { return len(fm.TypedInto(ownerSession)) >= 3 })
	if !a.Alive(context.Background(), 42, req.StageDir) || a.Attach(context.Background(), 42, req.StageDir) != "tmux attach -t "+ownerSession {
		t.Fatal("a queued stage is alive and attaches to the owner's session")
	}
	if err := a.Kill(context.Background(), 42, req.StageDir); err != nil {
		t.Fatal(err)
	}
	cancel()
	o := get(t, ch)
	var re *runner.Error
	if errors.As(o.err, &re) && re.Class == runner.ErrDetached {
		t.Fatalf("a cancelled queue seat must not detach: %v", o.err)
	}
	if got := readFile(t, queue); got != "# The owner's brief\n\nKeep this.\n" {
		t.Fatalf("queue file = %q", got)
	}
	if alive, _ := fm.Has(context.Background(), ownerSession); !alive {
		t.Fatal("cancel must not touch the owner's session")
	}
	if a.Alive(context.Background(), 42, req.StageDir) {
		t.Fatal("a cancelled stage is not adoptable")
	}
	if killed, _ := a.Sweep(context.Background(), func(int64) bool { return false }); len(killed) != 0 {
		t.Fatalf("sweep killed %v", killed)
	}
}

func TestARestartAdoptsAQueuedStageWithoutWritingItAgain(t *testing.T) {
	a, fm, queue := queueSeat(t, "deaf")
	req := queueReq(t, "worker")
	ctx, cancel := context.WithCancel(context.Background())
	ch := runAsync(a, ctx, req)
	waitFor(t, "a nudge", func() bool { return len(fm.TypedInto(ownerSession)) >= 3 })
	cancel()
	get(t, ch)
	before := readFile(t, queue)
	st, _ := readQueueState(req.StageDir)
	ch = runAsync(a, context.Background(), req) // the next process
	time.Sleep(50 * time.Millisecond)
	if got := readFile(t, queue); got != before {
		t.Fatalf("the adopted stage rewrote the queue file:\n%s", got)
	}
	if err := os.WriteFile(st.Result, []byte(`{"status":"done","summary":"by hand"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if o := get(t, ch); o.err != nil || !strings.Contains(o.res.Final, "by hand") {
		t.Fatalf("%s %v", o.res.Final, o.err)
	}
}

func TestQueueSeatSessionNotRunningIsAttentionNotACrash(t *testing.T) {
	a, fm, _ := queueSeat(t, "react")
	_ = fm.Kill(context.Background(), ownerSession)
	req := queueReq(t, "worker")
	ctx, cancel := context.WithCancel(context.Background())
	ch := runAsync(a, ctx, req)
	waitFor(t, "the notice", func() bool {
		n, ok := a.Board.Get(42)
		return ok && strings.Contains(n.Message, "not running")
	})
	if names, _ := fm.List(context.Background(), ""); len(names) != 0 {
		t.Fatalf("agentflow must never start the owner's session: %v", names)
	}
	cancel()
	get(t, ch)
}

func TestQueueBlocksKeepOtherText(t *testing.T) {
	f := filepath.Join(t.TempDir(), "q.md")
	if err := putBlock(f, 1, "one"); err != nil {
		t.Fatal(err)
	}
	if err := putBlock(f, 2, "two"); err != nil {
		t.Fatal(err)
	}
	if err := dropBlock(f, 1); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, f); got != "<!-- agentflow stage 2 begin -->\ntwo\n<!-- agentflow stage 2 end -->\n" {
		t.Fatalf("%q", got)
	}
	if err := dropBlock(f, 2); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, f); got != "" {
		t.Fatalf("%q", got)
	}
}
