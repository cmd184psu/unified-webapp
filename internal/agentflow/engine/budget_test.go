package engine

import (
	"context"
	"errors"
	"testing"

	"cmd184psu/unified-webapp/internal/agentflow/model"
	"cmd184psu/unified-webapp/internal/agentflow/runner"
	"cmd184psu/unified-webapp/internal/agentflow/store"
)

func TestAT12_BudgetThresholds(t *testing.T) {
	cases := []struct {
		name           string
		b              Budget
		noNew, exhaust bool
		fraction       float64
		set            bool
	}{
		{"no limit", Budget{Tokens: 1 << 40, Seconds: 1 << 40}, false, false, 0, false},
		{"under 90%", Budget{Tokens: 899, TokenLimit: 1000}, false, false, 0.899, true},
		{"90%", Budget{Tokens: 900, TokenLimit: 1000}, true, false, 0.9, true},
		{"100%", Budget{Tokens: 1000, TokenLimit: 1000}, true, true, 1, true},
		{"time decides", Budget{Tokens: 10, TokenLimit: 1000, Seconds: 95, SecondsLimit: 100}, true, false, 0.95, true},
	}
	for _, c := range cases {
		if c.b.Set() != c.set || c.b.NoNewTasks() != c.noNew || c.b.Exhausted() != c.exhaust || c.b.Fraction() != c.fraction {
			t.Errorf("%s: set=%v noNew=%v exhausted=%v fraction=%v", c.name, c.b.Set(), c.b.NoNewTasks(), c.b.Exhausted(), c.b.Fraction())
		}
	}
	if usageTokens(nil) != 0 || usageTokens(&runner.Usage{InputTokens: 3, OutputTokens: 4}) != 7 ||
		usageTokens(&runner.Usage{InputTokens: 3, OutputTokens: 4, TotalTokens: 9}) != 9 {
		t.Error("usageTokens must prefer the reported total, else input plus output")
	}
}

// AT12 / S5: the tokens of every runner call of a stage, the corrective
// follow-up included, are stored on the stage.
func TestAT12_StageTokensComeFromRunnerUsage(t *testing.T) {
	h := newStageHarness(t)
	h.fake.Script("reviewer",
		runner.Step{Reply: reviewBad, Usage: &runner.Usage{InputTokens: 100, OutputTokens: 20}},
		runner.Step{Reply: reviewPass, Usage: &runner.Usage{TotalTokens: 50}})
	f := h.attemptStage("alpha", "reviewer")
	h.run(f)
	st, err := h.st.GetStage(context.Background(), f.stage.ID)
	if err != nil {
		t.Fatal(err)
	}
	if st.Tokens != 170 {
		t.Fatalf("stage tokens = %d, want 120 + 50", st.Tokens)
	}
	if tokens, _, err := h.st.EffortUsage(context.Background(), f.payload.Effort, 0); err != nil || tokens != 170 {
		t.Fatalf("effort usage = %d (%v), want 170", tokens, err)
	}
}

// AC-AT12: at 90% no new task starts while the in-flight attempts continue; at
// 100% the project's run is stopped with reason budget and no task is failed.
func TestAT12_NinetyPercentHoldsNewTasksHundredStopsTheRun(t *testing.T) {
	h := newDispHarness(t)
	h.standard(2)
	fix := h.dispProject("alpha", "any", indep(4))
	ctx := context.Background()
	limit := int64(1000)
	if _, err := h.st.UpdateEffort(ctx, "t", fix.effort, store.EffortPatch{BudgetTokens: &limit}); err != nil {
		t.Fatal(err)
	}
	tokens := func(st model.Stage, n int64) {
		t.Helper()
		if err := h.st.RecordStageTokens(ctx, st.ID, n); err != nil {
			t.Fatal(err)
		}
	}
	h.startRun("alpha")
	h.pass()
	open := h.openStages()
	if len(open) != 2 {
		t.Fatalf("first pass placed %d stages, want the 2 slots", len(open))
	}
	t1, t2 := open[0], open[1]

	tokens(t1, 500)
	h.finish(t1)
	h.pass()
	if n := len(h.openStages()); n != 2 {
		t.Fatalf("at 50%% the next task must start, open stages = %d", n)
	}
	t3 := h.openStages()[1]

	tokens(t2, 400) // 900 of 1000 in total
	h.finish(t3)
	h.pass()
	if open := h.openStages(); len(open) != 1 || open[0].ID != t2.ID {
		t.Fatalf("at 90%% no new task may start, open stages = %+v", open)
	}
	if got := h.eng.Runs().State("alpha"); got != model.RunRunning {
		t.Fatalf("at 90%% the run keeps going, state = %s", got)
	}
	if a, _ := h.st.GetAttempt(ctx, t2.Attempt); a.Outcome != "" {
		t.Fatalf("at 90%% the in-flight attempt was closed: %q", a.Outcome)
	}

	tokens(t2, 500) // 1000 of 1000
	h.pass()
	if got := h.eng.Runs().State("alpha"); got != model.RunStopped || h.eng.Runs().StopReason("alpha") != BudgetReason {
		t.Fatalf("at 100%% state = %s reason = %q, want stopped/budget", got, h.eng.Runs().StopReason("alpha"))
	}
	if n := len(h.openStages()); n != 0 {
		t.Fatalf("stopped run left %d stages open", n)
	}
	if a, _ := h.st.GetAttempt(ctx, t2.Attempt); a.Outcome != model.OutcomeAbandoned {
		t.Fatalf("in-flight attempt outcome = %q, want abandoned (never counts)", a.Outcome)
	}
	status, err := h.eng.TaskStatuses(ctx, "alpha", fix.effort)
	if err != nil {
		t.Fatal(err)
	}
	for key, st := range status {
		// T1 and T3 passed before the stop; the interrupted T2 and the never
		// started T4 are ready again. Nothing is failed or halted.
		if st != model.StatusReady && st != model.StatusPassed {
			t.Fatalf("task %s is %s after a budget stop, want ready or passed", key, st)
		}
	}
	if status["T2"] != model.StatusReady {
		t.Fatalf("the interrupted task is %s, want ready", status["T2"])
	}
	evs, err := h.st.ListEvents(ctx, "project", "alpha", 0)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, ev := range evs {
		found = found || ev.Action == "run.stop.budget"
	}
	if !found {
		t.Fatalf("no run.stop.budget event: %+v", evs)
	}

	// Starting again is refused until the budget is raised.
	if err := h.eng.Runs().Start(ctx, "t", "alpha"); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("start with an exhausted budget = %v, want ErrConflict", err)
	}
	more := int64(100000)
	if _, err := h.st.UpdateEffort(ctx, "t", fix.effort, store.EffortPatch{BudgetTokens: &more}); err != nil {
		t.Fatal(err)
	}
	if err := h.eng.Runs().Start(ctx, "t", "alpha"); err != nil {
		t.Fatal(err)
	}
	if h.eng.Runs().StopReason("alpha") != "" {
		t.Fatal("a started run keeps no stop reason")
	}
}
