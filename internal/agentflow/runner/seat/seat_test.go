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

func fakeSeat(t *testing.T, fm *mux.Fake, mode string, env map[string]string) *Adapter {
	t.Helper()
	script, err := filepath.Abs("testdata/fakeseat.sh")
	if err != nil {
		t.Fatal(err)
	}
	e := map[string]string{"SEAT_MODE": mode}
	for k, v := range env {
		e[k] = v
	}
	return &Adapter{
		Mux: func(context.Context) (mux.Multiplexer, error) { return fm, nil },
		Profiles: func(context.Context) map[string]Profile {
			return map[string]Profile{"fake": {Command: []string{"/bin/sh", script}, StartupWait: Duration(20 * time.Millisecond), ReadyText: "fake seat ready", Env: e}}
		},
		Instance: "test-instance",
		Board:    &runner.SeatBoard{},
		Poll:     5 * time.Millisecond,
	}
}

func seatReq(t *testing.T, role string) runner.Request {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "prompt.md")
	if err := os.WriteFile(p, []byte("Add a feature."), 0o600); err != nil {
		t.Fatal(err)
	}
	return runner.Request{Role: role, Model: "fake", Backend: runner.Backend{Name: "fb", APIType: APIType},
		Workdir: t.TempDir(), StageDir: dir, PromptPath: p,
		Job: runner.Job{Project: "demo.app", Effort: 3, Task: "T001", Stage: 42}}
}

type outcome struct {
	res runner.Result
	err error
}

func runAsync(a *Adapter, ctx context.Context, req runner.Request) chan outcome {
	ch := make(chan outcome, 1)
	go func() {
		res, err := a.Run(ctx, req)
		ch <- outcome{res, err}
	}()
	return ch
}

func get(t *testing.T, ch chan outcome) outcome {
	t.Helper()
	select {
	case o := <-ch:
		return o
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return")
	}
	return outcome{}
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestSessionName(t *testing.T) {
	name := SessionName(runner.Job{Project: "demo.app", Effort: 3, Task: "T001", Stage: 42})
	if name != "af-demo-app-3-T001-42" || !mux.Valid(name) {
		t.Fatalf("name = %q", name)
	}
	if id, ok := StageOf(name); !ok || id != 42 {
		t.Fatalf("StageOf = %d %v", id, ok)
	}
}

func TestDoneSeatFinishesTheStage(t *testing.T) {
	fm := &mux.Fake{}
	t.Cleanup(fm.KillAll)
	a := fakeSeat(t, fm, "done", nil)
	req := seatReq(t, "worker")
	o := get(t, runAsync(a, context.Background(), req))
	if o.err != nil {
		t.Fatalf("Run: %v", o.err)
	}
	w, err := schema.Parse[schema.WorkerOut](json.RawMessage(o.res.Final))
	if err != nil || w.Summary != "did it" || len(w.FilesChanged) != 1 {
		t.Fatalf("final %s: %v", o.res.Final, err)
	}
	name := SessionName(req.Job)
	typed := fm.TypedInto(name)
	if len(typed) != 2 || !strings.Contains(typed[0], filepath.Join(HomeDir(req.StageDir), TaskFile)) || typed[1] != "<Enter>" {
		t.Fatalf("typed %q; want one line naming TASK.md, then Enter", typed)
	}
	task, err := os.ReadFile(filepath.Join(HomeDir(req.StageDir), TaskFile))
	if err != nil || !strings.Contains(string(task), "Add a feature.") || !strings.Contains(string(task), ResultFile) {
		t.Fatalf("TASK.md = %q (%v)", task, err)
	}
	if ok, _ := fm.Has(context.Background(), name); ok {
		t.Error("the session should be ended once the stage has its report")
	}
}

func TestQuestionWaitsForTheOwnersAnswer(t *testing.T) {
	fm := &mux.Fake{}
	t.Cleanup(fm.KillAll)
	a := fakeSeat(t, fm, "question", nil)
	req := seatReq(t, "worker")
	ch := runAsync(a, context.Background(), req)
	waitFor(t, "the question", func() bool {
		n, ok := a.Board.Get(42)
		return ok && n.Kind == runner.SeatQuestion && strings.Contains(n.Message, "tabs or spaces?")
	})
	select {
	case o := <-ch:
		t.Fatalf("a question ended the stage: %+v", o)
	default:
	}
	if err := a.Answer(context.Background(), 42, req.StageDir, "tabs"); err != nil {
		t.Fatal(err)
	}
	o := get(t, ch)
	if o.err != nil || !strings.Contains(o.res.Final, "used tabs") {
		t.Fatalf("final %s: %v", o.res.Final, o.err)
	}
	if _, ok := a.Board.Get(42); ok {
		t.Error("the question should be cleared")
	}
}

// Timeouts never auto-retry a seat: silence past the budget is a notice, the stage waits.
func TestSilencePastTheBudgetIsANotice(t *testing.T) {
	fm := &mux.Fake{}
	t.Cleanup(fm.KillAll)
	a := fakeSeat(t, fm, "silent", nil)
	req := seatReq(t, "worker")
	req.Budget = 30 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	ch := runAsync(a, ctx, req)
	waitFor(t, "the silence notice", func() bool {
		n, ok := a.Board.Get(42)
		return ok && n.Kind == runner.SeatSilent && n.Attach != ""
	})
	select {
	case o := <-ch:
		t.Fatalf("silence ended the stage: %+v", o)
	default:
	}
	cancel()
	var re *runner.Error
	if o := get(t, ch); !errors.As(o.err, &re) || re.Class != runner.ErrDetached {
		t.Fatalf("err = %v, want detached: the session lives on", o.err)
	}
}

// A restart: the first process stops waiting, the session lives on, the next process adopts
// it by name without starting or typing again.
func TestARestartAdoptsTheSession(t *testing.T) {
	fm := &mux.Fake{}
	t.Cleanup(fm.KillAll)
	goFile := filepath.Join(t.TempDir(), "go")
	a := fakeSeat(t, fm, "wait", map[string]string{"SEAT_GO": goFile})
	req := seatReq(t, "worker")
	name := SessionName(req.Job)
	ctx, cancel := context.WithCancel(context.Background())
	ch := runAsync(a, ctx, req)
	waitFor(t, "the line", func() bool { return len(fm.TypedInto(name)) == 2 })
	cancel()
	var re *runner.Error
	if o := get(t, ch); !errors.As(o.err, &re) || re.Class != runner.ErrDetached {
		t.Fatalf("err = %v, want detached", o.err)
	}
	b := fakeSeat(t, fm, "wait", map[string]string{"SEAT_GO": goFile}) // the next process
	if !b.Alive(context.Background(), 42) {
		t.Fatal("the next process should see the session")
	}
	ch = runAsync(b, context.Background(), req)
	if err := os.WriteFile(goFile, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	o := get(t, ch)
	if o.err != nil || !strings.Contains(o.res.Final, "after the restart") {
		t.Fatalf("adopted: %s %v", o.res.Final, o.err)
	}
	if typed := fm.TypedInto(name); len(typed) != 2 {
		t.Fatalf("typed %q: the adopted session must not be started or typed into again", typed)
	}
}

func TestProgramExitWithoutReportIsACrash(t *testing.T) {
	fm := &mux.Fake{}
	t.Cleanup(fm.KillAll)
	a := fakeSeat(t, fm, "exit", nil)
	_, err := a.Run(context.Background(), seatReq(t, "worker"))
	var re *runner.Error
	if !errors.As(err, &re) || re.Class != runner.ErrCrash {
		t.Fatalf("err = %v, want crash", err)
	}
}

func TestKillEndsTheCallAsCancelled(t *testing.T) {
	fm := &mux.Fake{}
	t.Cleanup(fm.KillAll)
	a := fakeSeat(t, fm, "silent", nil)
	req := seatReq(t, "worker")
	ctx, cancel := context.WithCancel(context.Background())
	ch := runAsync(a, ctx, req)
	waitFor(t, "the session", func() bool { return a.Alive(context.Background(), 42) && len(fm.TypedInto(SessionName(req.Job))) == 2 })
	if err := a.Kill(context.Background(), 42); err != nil {
		t.Fatal(err)
	}
	cancel()
	o := get(t, ch)
	var re *runner.Error
	if errors.As(o.err, &re) && re.Class == runner.ErrDetached {
		t.Fatalf("a killed seat must not detach: %v", o.err)
	}
}

func TestSweepKillsOnlyThisInstancesOrphans(t *testing.T) {
	fm := &mux.Fake{}
	t.Cleanup(fm.KillAll)
	ctx := context.Background()
	mk := func(name, inst, stage string) {
		if err := fm.Create(ctx, mux.Spec{Name: name, Argv: []string{"/bin/sleep", "30"}, Tags: map[string]string{tagInstance: inst, tagStage: stage}}); err != nil {
			t.Fatal(err)
		}
	}
	mk("af-p-1-T1-10", "test-instance", "10") // open stage: kept
	mk("af-p-1-T2-11", "test-instance", "11") // closed stage: killed
	mk("af-p-1-T3-12", "other", "12")         // another agentflow's: kept
	mk("deepseek4", "", "")                   // a person's session: kept
	a := fakeSeat(t, fm, "done", nil)
	killed, err := a.Sweep(ctx, func(id int64) bool { return id == 10 })
	if err != nil {
		t.Fatal(err)
	}
	if len(killed) != 1 || killed[0] != "af-p-1-T2-11" {
		t.Fatalf("killed %v", killed)
	}
	left, _ := fm.List(ctx, "")
	if strings.Join(left, ",") != "af-p-1-T1-10,af-p-1-T3-12,deepseek4" {
		t.Fatalf("left %v", left)
	}
}

func TestReviewerReportBecomesAValidReview(t *testing.T) {
	final, err := finalFor("reviewer", `{"status":"done","verdict":"repair","summary":"bad","issues":[{"description":"nil deref","severity":"critical"}]}`)
	if err != nil {
		t.Fatal(err)
	}
	r, err := schema.Parse[schema.ReviewOut](json.RawMessage(final))
	if err != nil || r.Verdict != "repair" || r.RepairPrompt == "" || r.Issues[0].Severity != "blocker" {
		t.Fatalf("%s: %v", final, err)
	}
}
