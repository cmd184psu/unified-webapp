package engine

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"cmd184psu/unified-webapp/internal/agentflow/model"
	"cmd184psu/unified-webapp/internal/agentflow/store"
)

// conflictChain builds an attempt whose newest stage is a merge reporting a
// conflict, the trigger for the merger path.
func conflictChain(t *testing.T, c *chainRun, eff model.Effort) *chain {
	t.Helper()
	ctx := context.Background()
	proj, err := c.st.GetProject(ctx, c.slug)
	if err != nil {
		t.Fatal(err)
	}
	task := c.tasks[0]
	att, err := c.st.StartAttempt(ctx, task.ID, model.AttemptNormal, t.TempDir(), "m1")
	if err != nil {
		t.Fatal(err)
	}
	hash, err := c.st.ApprovedQueueHash(ctx, eff.ID)
	if err != nil {
		t.Fatal(err)
	}
	merge := model.Stage{Role: "merge", QueueHash: hash,
		Parsed: json.RawMessage(`{"kind":"conflict","files":["notes.txt"]}`)}
	return &chain{proj: proj, eff: eff, task: task, att: att, stages: []model.Stage{merge}}
}

// A merge needing a merger with a valid effort starts a stage, picked from
// the merger candidate list.
func TestMergerStartsWithValidEffort(t *testing.T) {
	defs := []model.TaskDef{{Key: "T1", Phase: "P", Title: "first", Prompt: "do", Acceptance: []string{"ok"}}}
	c := newChainRun(t, nil, defs)
	c.candidates("global", "", "merger", "gpu/m2")
	ctx := context.Background()
	ef, err := c.st.GetEffort(ctx, c.effort)
	if err != nil {
		t.Fatal(err)
	}
	ch := conflictChain(t, c, ef)
	handled, err := c.eng.handleMergeConflict(ctx, ch, ch.stages[0])
	if err != nil || !handled {
		t.Fatalf("handleMergeConflict handled=%v err=%v", handled, err)
	}
	stages, err := c.st.ListAttemptStages(ctx, ch.att.ID)
	if err != nil {
		t.Fatal(err)
	}
	var mergers []model.Stage
	for _, st := range stages {
		if st.Role == "merger" {
			mergers = append(mergers, st)
		}
	}
	if len(mergers) != 1 {
		t.Fatalf("want 1 merger stage, got %d", len(mergers))
	}
	if mergers[0].Backend != "gpu" || mergers[0].Model != "m2" {
		t.Fatalf("merger picked outside the merger list: %s/%s", mergers[0].Backend, mergers[0].Model)
	}
	// The prompt for the stored attempt stage must resolve: st.Effort is 0
	// for attempt stages, which used to crash with "effort 0: not found".
	if _, err := c.eng.mergerPrompt(ctx, c.slug, mergers[0]); err != nil {
		t.Fatalf("mergerPrompt for a stored merger stage: %v", err)
	}
}

// With no real effort no merger starts, and the skip is logged once no
// matter how often the chain retries.
func TestMergerSkipsWithoutEffortOnce(t *testing.T) {
	defs := []model.TaskDef{{Key: "T1", Phase: "P", Title: "first", Prompt: "do", Acceptance: []string{"ok"}}}
	c := newChainRun(t, nil, defs)
	c.candidates("global", "", "merger", "gpu/m2")
	ctx := context.Background()
	ef, err := c.st.GetEffort(ctx, c.effort)
	if err != nil {
		t.Fatal(err)
	}
	ch := conflictChain(t, c, ef)
	ch.eff = model.Effort{}
	for i := 0; i < 2; i++ {
		started, waiting, err := c.eng.startMerger(ctx, ch, ch.stages[0])
		if err != nil || started || waiting {
			t.Fatalf("pass %d: startMerger started=%v waiting=%v err=%v", i, started, waiting, err)
		}
	}
	stages, err := c.st.ListAttemptStages(ctx, ch.att.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, st := range stages {
		if st.Role == "merger" {
			t.Fatalf("merger started without an effort: %+v", st)
		}
	}
	evs, err := c.st.ListEvents(ctx, "effort", "0", 100000)
	if err != nil {
		t.Fatal(err)
	}
	skips := 0
	for _, ev := range evs {
		if ev.Action == "merge.merger.skip" {
			skips++
		}
	}
	if skips != 1 {
		t.Fatalf("want exactly 1 merger skip note, got %d", skips)
	}
}

// A crashed merger retries from the merger list, not the worker list, and
// stops after the merger try limit with a human waiting reason instead of
// crashing in a loop.
func TestMergerRetryBounded(t *testing.T) {
	defs := []model.TaskDef{{Key: "T1", Phase: "P", Title: "first", Prompt: "do", Acceptance: []string{"ok"}}}
	c := newChainRun(t, nil, defs)
	c.candidates("global", "", "merger", "gpu/m1", "gpu/m2")
	ctx := context.Background()
	ef, err := c.st.GetEffort(ctx, c.effort)
	if err != nil {
		t.Fatal(err)
	}
	ch := conflictChain(t, c, ef)
	hash, err := c.st.ApprovedQueueHash(ctx, ef.ID)
	if err != nil {
		t.Fatal(err)
	}
	crash := func(backend, mdl string) model.Stage {
		st, err := c.st.CreateStage(ctx, store.StageSpec{Attempt: ch.att.ID, Role: "merger",
			Backend: backend, Model: mdl, QueueHash: hash, PromptRef: mergerRef + ch.task.Key})
		if err != nil {
			t.Fatal(err)
		}
		if err := c.st.FinishStage(ctx, st.ID, OutcomeInfraCrash, "effort 0: not found", nil); err != nil {
			t.Fatal(err)
		}
		st, err = c.st.GetStage(ctx, st.ID)
		if err != nil {
			t.Fatal(err)
		}
		return st
	}
	first := crash("gpu", "m1")
	ch.stages = append(ch.stages, first)
	if err := c.eng.resubmitMerger(ctx, ch, first); err != nil {
		t.Fatalf("first resubmitMerger: %v", err)
	}
	stages, err := c.st.ListAttemptStages(ctx, ch.att.ID)
	if err != nil {
		t.Fatal(err)
	}
	var second *model.Stage
	for i, st := range stages {
		if st.Role == "merger" && st.ID != first.ID {
			second = &stages[i]
		}
	}
	if second == nil {
		t.Fatal("a first crashed merger must be retried once")
	}
	if second.Backend != "gpu" || (second.Model != "m1" && second.Model != "m2") {
		t.Fatalf("retry picked outside the merger list: %s/%s", second.Backend, second.Model)
	}
	// The retry also crashes: no third merger, a human reason instead.
	ch.stages = append(ch.stages, *second)
	if err := c.eng.resubmitMerger(ctx, ch, *second); err != nil {
		t.Fatalf("second resubmitMerger: %v", err)
	}
	after, err := c.st.ListAttemptStages(ctx, ch.att.ID)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, st := range after {
		if st.Role == "merger" {
			n++
		}
	}
	if n != 2 {
		t.Fatalf("merger retries are not bounded: %d merger stages", n)
	}
	task, err := c.st.GetTask(ctx, ch.task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(task.WaitingReason, "human") {
		t.Fatalf("exhausted merger must page a human, got %q", task.WaitingReason)
	}
}
