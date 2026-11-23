package engine

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"cmd184psu/unified-webapp/internal/agentflow/model"
	"cmd184psu/unified-webapp/internal/agentflow/runner"
	"cmd184psu/unified-webapp/internal/agentflow/store"
)

// newCoordinatorBlocked is a run with one task stopped on needs_files (the worker
// asked for a file outside its list) and the coordinator role listed after the
// worker and reviewer: an attention item the coordinator can act on.
func newCoordinatorBlocked(t *testing.T) *chainRun {
	t.Helper()
	defs := []model.TaskDef{{Key: "T1", Phase: "P", Title: "port the truce", Prompt: "Port the truce into `src/rules/truce.go`.",
		Acceptance: []string{"ok"}, FilesTouched: []string{"src/rules/truce.go"}}}
	c := newChainRun(t, nil, defs)
	c.candidates("global", "", "coordinator", "gpu/m1", "gpu/m2")
	c.fake.Script("worker", runner.Step{Reply: workerNeedsState, Edit: func(r runner.Request) error {
		if err := os.MkdirAll(filepath.Join(r.Workdir, "src/rules"), 0o755); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(r.Workdir, "src/rules/truce.go"), []byte("package rules\n"), 0o644)
	}})
	c.run()
	waitFor(t, "the task stops for files", func() bool {
		as := c.attempts(0)
		return len(as) == 1 && as[0].Outcome == model.OutcomeBlocked
	})
	// Attention lists a running effort's stopped tasks; the run leaves the effort
	// as it was, so set it to running here.
	ctx := context.Background()
	ef, err := c.st.GetEffort(ctx, c.effort)
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := c.st.UpdateEffortStatusIf(ctx, "t", c.effort, ef.Status, model.EffortRunning); err != nil || !ok {
		t.Fatalf("effort to running: %v %v", ok, err)
	}
	return c
}

// coordinatorStages is the effort's coordinator stages, oldest first.
func coordinatorStages(t *testing.T, c *chainRun) []model.Stage {
	t.Helper()
	sts, err := c.st.ListEffortStages(context.Background(), c.effort)
	if err != nil {
		t.Fatal(err)
	}
	var out []model.Stage
	for _, st := range sts {
		if st.Role == "coordinator" {
			out = append(out, st)
		}
	}
	return out
}

// coordinatorRuns is the applied runs in the audit log, oldest first.
func coordinatorRuns(t *testing.T, c *chainRun) []coordinatorRunEvent {
	t.Helper()
	evs, err := c.eng.coordinatorEvents(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var out []coordinatorRunEvent
	for _, ev := range evs {
		if ev.Action != eventCoordinatorRun {
			continue
		}
		var d coordinatorRunEvent
		if err := json.Unmarshal([]byte(ev.DetailJSON), &d); err != nil {
			t.Fatalf("run event %d: %v", ev.ID, err)
		}
		out = append(out, d)
	}
	return out
}

// AF-19 item 7: the check rides the engine's tick and starts nothing when nothing
// needs a coordinator.
func TestCoordinatorTickLeavesAnEmptyServiceAlone(t *testing.T) {
	c := newChainRun(t, nil, chainDefs())
	c.candidates("global", "", "coordinator", "gpu/m1")
	ctx := context.Background()
	if err := c.eng.CoordinatorTick(ctx); err != nil {
		t.Fatal(err)
	}
	if sts := coordinatorStages(t, c); len(sts) != 0 {
		t.Fatalf("a run started with nothing to do: %+v", sts)
	}
	if evs, err := c.eng.coordinatorEvents(ctx); err != nil || len(evs) != 0 {
		t.Fatalf("events = %+v, %v", evs, err)
	}
}

// AF-19 item 3: a run whose model is rate limited rests that model and the next
// tick walks to the next candidate.
func TestCoordinatorWalksToTheNextCandidateAfterARateLimit(t *testing.T) {
	c := newCoordinatorBlocked(t)
	ctx := context.Background()
	c.fake.Script("coordinator", runner.Step{ErrClass: runner.ErrRateLimit})
	if err := c.eng.CoordinatorTick(ctx); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the first run ends", func() bool { return len(doneCoordinator(c)) == 1 })
	c.fake.Script("coordinator", runner.Step{Reply: `{"summary":"nothing to move yet","actions":[]}`})
	if err := c.eng.CoordinatorTick(ctx); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the second run ends", func() bool { return len(doneCoordinator(c)) == 2 })
	sts := coordinatorStages(t, c)
	if sts[0].Model != "m1" || sts[0].Outcome != OutcomeInfraRateLimit {
		t.Fatalf("first run = %s/%s %s", sts[0].Backend, sts[0].Model, sts[0].Outcome)
	}
	if sts[1].Model != "m2" {
		t.Fatalf("the second run did not fall through to the next candidate: %s/%s", sts[1].Backend, sts[1].Model)
	}
	rests, err := c.st.ModelRests(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var rested bool
	for _, r := range rests {
		if r.Model == "m1" {
			rested = true
		}
	}
	if !rested {
		t.Fatalf("the rate-limited model is not resting: %+v", rests)
	}
}

// doneCoordinator is the finished coordinator stages of a run.
func doneCoordinator(c *chainRun) []model.Stage {
	sts, err := c.st.ListEffortStages(context.Background(), c.effort)
	if err != nil {
		return nil
	}
	var out []model.Stage
	for _, st := range sts {
		if st.Role == "coordinator" && st.State == model.StageDone {
			out = append(out, st)
		}
	}
	return out
}

// AF-19 item 3: with every candidate down the tick starts nothing and says so in
// the attention list.
func TestCoordinatorAsksWhenNoCandidateIsEligible(t *testing.T) {
	c := newCoordinatorBlocked(t)
	ctx := context.Background()
	now := time.Now()
	for _, m := range []string{"m1", "m2"} {
		if _, err := c.st.RestModel(ctx, "gpu", m, "test", 10*time.Minute, 10*time.Minute, now); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.eng.CoordinatorTick(ctx); err != nil {
		t.Fatal(err)
	}
	if sts := coordinatorStages(t, c); len(sts) != 0 {
		t.Fatalf("a run started with every candidate resting: %+v", sts)
	}
	items, err := c.eng.Attention(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, it := range items {
		if it.Kind == "coordinator_unavailable" {
			found = true
		}
	}
	if !found {
		t.Fatalf("attention = %+v", items)
	}
}

// AF-19 items 4 and 5: an action that stays with the owner is refused and asked
// as a question, an allowed one is applied, and coordinator_actions_per_run caps
// how many are applied.
func TestCoordinatorRefusesOwnerActionsAndCapsTheRun(t *testing.T) {
	c := newCoordinatorBlocked(t)
	ctx := context.Background()
	if err := c.st.UpdateSettings(ctx, "t", map[string]string{"coordinator_actions_per_run": "1"}); err != nil {
		t.Fatal(err)
	}
	reply := `{"summary":"the branch is ready but the task needs its note","actions":[` +
		`{"kind":"merge","project":"` + c.slug + `","task":"T1","reason":"the work is done and reviewed"},` +
		`{"kind":"note","project":"` + c.slug + `","task":"T1","note":"keep the new API; the finding is about the old one","reason":"the finding is stale"},` +
		`{"kind":"retry","project":"` + c.slug + `","task":"T1","reason":"the note should let it finish"}]}`
	c.fake.Script("coordinator", runner.Step{Reply: reply})
	if err := c.eng.CoordinatorTick(ctx); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the run ends", func() bool { return len(doneCoordinator(c)) == 1 })
	if err := c.eng.CoordinatorTick(ctx); err != nil {
		t.Fatal(err)
	}
	// The stage's prompt carries the attention list and the action cap.
	calls := c.fake.CallsFor("coordinator")
	if len(calls) != 1 {
		t.Fatalf("coordinator calls = %d, want 1", len(calls))
	}
	if p := calls[0].Prompt; !contains(p, "T1") || !contains(p, "At most 1 actions") || !contains(p, "## What needs attention now") {
		t.Fatalf("the run's prompt is missing the attention list or the cap:\n%.1200s", p)
	}
	runs := coordinatorRuns(t, c)
	if len(runs) != 1 {
		t.Fatalf("applied runs = %d, want 1", len(runs))
	}
	rep := runs[0]
	if len(rep.Refused) != 1 || rep.Refused[0].Kind != "merge" {
		t.Fatalf("refused = %+v, want the merge", rep.Refused)
	}
	if len(rep.Applied) != 1 || rep.Applied[0].Kind != "note" {
		t.Fatalf("applied = %+v, want only the note", rep.Applied)
	}
	if rep.Skipped != 1 {
		t.Fatalf("skipped = %d, want 1 past coordinator_actions_per_run", rep.Skipped)
	}
	var asked bool
	for _, q := range rep.Questions {
		if len(q.Question) > 0 {
			asked = true
		}
	}
	if !asked {
		t.Fatalf("a refused action asked no question: %+v", rep.Questions)
	}
	tk, err := c.st.GetTask(ctx, c.tasks[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if want := "COORDINATOR NOTE (coordinator:m1"; !contains(tk.Prompt, want) {
		t.Fatalf("the note did not reach the task prompt:\n%s", tk.Prompt)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// AF-19 item 7: the schedule and the day's cap gate a run.
func TestCoordinatorDueFollowsTheScheduleAndTheDayCap(t *testing.T) {
	now := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
	set := store.Settings{CoordinatorCheckMinutes: 60, CoordinatorRunsPerDay: 2}
	at := func(minAgo int, action, detail string) store.Event {
		return store.Event{TS: now.Add(-time.Duration(minAgo) * time.Minute), Action: action, DetailJSON: detail}
	}
	start := func(minAgo int) store.Event { return at(minAgo, eventCoordinatorStart, `{"stage":1}`) }
	run := func(minAgo int, infra bool) store.Event {
		if infra {
			return at(minAgo, eventCoordinatorRun, `{"stage":1,"infra":true}`)
		}
		return at(minAgo, eventCoordinatorRun, `{"stage":1}`)
	}
	unavailable := func(minAgo int) store.Event { return at(minAgo, eventCoordinatorUnavailable, `{"reason":"none"}`) }
	cases := []struct {
		name string
		set  store.Settings
		evs  []store.Event
		want bool
	}{
		{"first check", set, nil, true},
		{"inside the period", set, []store.Event{run(10, false)}, false},
		{"the period passed", set, []store.Event{run(61, false)}, true},
		{"a run that could not be made retries at once", set, []store.Event{run(1, true)}, true},
		{"an unavailable check waits for the period", set, []store.Event{unavailable(10)}, false},
		{"the day cap is used up", set, []store.Event{start(600), start(300)}, false},
		{"the day cap counts only the last day", set, []store.Event{start(25 * 60), start(26 * 60)}, true},
		{"zero runs per day means no cap", store.Settings{CoordinatorCheckMinutes: 60, CoordinatorRunsPerDay: 0}, []store.Event{start(30)}, true},
		{"off", store.Settings{CoordinatorCheckMinutes: 0}, nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := coordinatorDue(tc.set, tc.evs, now); got != tc.want {
				t.Fatalf("coordinatorDue = %v, want %v", got, tc.want)
			}
		})
	}
}
