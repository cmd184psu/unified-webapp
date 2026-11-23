package engine

import (
	"context"
	"testing"
	"time"

	"cmd184psu/unified-webapp/internal/agentflow/model"
)

func TestAF20_UnlimitedBudgetIsNeverGated(t *testing.T) {
	b, err := (&Engine{}).budgetGate(context.Background(), model.Effort{})
	if err != nil {
		t.Fatal(err)
	}
	if b.Set() || b.NoNewTasks() || b.Exhausted() {
		t.Fatalf("an effort without a budget must never gate: %+v", b)
	}
}

func setMetered(t *testing.T, h *harness, name string, metered bool, daily int64) {
	t.Helper()
	ctx := context.Background()
	b, err := h.st.GetBackend(ctx, name)
	if err != nil {
		t.Fatal(err)
	}
	b.Metered, b.DailyTokenCap = metered, daily
	if err := h.st.UpdateBackend(ctx, "t", b); err != nil {
		t.Fatal(err)
	}
}

func spend(t *testing.T, h *harness, effort int64, tokens int64) {
	t.Helper()
	now := time.Now()
	if _, err := h.st.DB().ExecContext(context.Background(), `INSERT INTO stages (effort, role, state, started, finished, tokens, backend, model)
		VALUES (?, 'planner', 'done', ?, ?, ?, 'metered', 'm1')`, effort, now.Add(-time.Second).UnixMilli(), now.UnixMilli(), tokens); err != nil {
		t.Fatal(err)
	}
}

func TestAF20_MeteredCapRestsAndFallsThrough(t *testing.T) {
	h := newDispHarness(t)
	h.addBackend("metered", 1, false, false, "m1", "m2")
	h.addBackend("free", 1, false, false, "m3", "m4")
	h.candidates("global", "", "worker", "metered/m1", "free/m3")
	h.candidates("global", "", "reviewer", "metered/m2", "free/m4")
	setMetered(t, h, "metered", true, 100)
	fix := h.dispProject("alpha", "any", indep(1))
	spend(t, h, fix.effort, 100)
	h.startRun("alpha")
	h.pass()
	open := h.openStages()
	if len(open) != 1 || open[0].Backend != "free" {
		t.Fatalf("work at the metered cap must fall through to free: %+v", open)
	}
	rests, err := h.st.ModelRests(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range rests {
		if r.Backend == "metered" && r.Reason == "budget" {
			found = true
		}
	}
	if !found {
		t.Fatalf("metred cap did not rest a model until the window reset: %+v", rests)
	}
}

func TestAF20_NonMeteredBackendNeverRestsForBudget(t *testing.T) {
	h := newDispHarness(t)
	h.addBackend("metered", 1, false, false, "m1", "m2")
	h.addBackend("free", 1, false, false, "m3", "m4")
	h.candidates("global", "", "worker", "metered/m1", "free/m3")
	h.candidates("global", "", "reviewer", "metered/m2", "free/m4")
	setMetered(t, h, "metered", false, 100)
	fix := h.dispProject("alpha", "any", indep(1))
	spend(t, h, fix.effort, 100)
	h.startRun("alpha")
	h.pass()
	open := h.openStages()
	if len(open) != 1 || open[0].Backend != "metered" {
		t.Fatalf("a non-metered backend must not rest for budget: %+v", open)
	}
	rests, err := h.st.ModelRests(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rests {
		if r.Backend == "metered" && r.Reason == "budget" {
			t.Fatalf("non-metered backend got a budget rest: %+v", r)
		}
	}
}
