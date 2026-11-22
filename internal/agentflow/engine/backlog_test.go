package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"cmd184psu/unified-webapp/internal/agentflow/model"
	"cmd184psu/unified-webapp/internal/agentflow/store"
)

// readyEffort creates and parks a queue-approved effort with a valid budget.
func (h *harness) readyEffort(t *testing.T, project string, priority int64, budget int64) int64 {
	t.Helper()
	ctx := context.Background()
	ef, err := h.st.CreateEffort(ctx, "test", project, "ready effort", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.st.SaveDoc(ctx, "test", ef.ID, model.DocFRD, []byte(frdJSON)); err != nil {
		t.Fatal(err)
	}
	if _, err := h.st.SaveDoc(ctx, "test", ef.ID, model.DocPlan, []byte(planReply)); err != nil {
		t.Fatal(err)
	}
	if _, err := h.st.ApproveDoc(ctx, "test", ef.ID, model.DocFRD, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := h.st.ApproveDoc(ctx, "test", ef.ID, model.DocPlan, 1); err != nil {
		t.Fatal(err)
	}
	q, err := h.st.SaveQueue(ctx, "test", ef.ID, []model.TaskDef{{Key: "T1", Phase: "P1", Title: "one", Prompt: "do it",
		Acceptance: []string{"works"}, FilesTouched: []string{"a.txt"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.st.ApproveQueue(ctx, "test", ef.ID, q.Hash); err != nil {
		t.Fatal(err)
	}
	ready := model.EffortReady
	p := priority
	if budget > 0 {
		if _, err := h.st.UpdateEffort(ctx, "test", ef.ID, store.EffortPatch{BudgetTokens: &budget}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := h.st.UpdateEffort(ctx, "test", ef.ID, store.EffortPatch{Status: &ready, Priority: &p}); err != nil {
		t.Fatal(err)
	}
	return ef.ID
}

func (h *harness) project(t *testing.T, slug string) {
	t.Helper()
	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := h.st.CreateProject(context.Background(), "test", store.Project{Slug: slug, RepoPath: repo, ModelScope: "any"}); err != nil {
		t.Fatal(err)
	}
}

func TestAF15_AutoStartHonoursMinimumPriorityAndBudget(t *testing.T) {
	h := newHarness(t)
	h.start()
	h.project(t, "low")
	h.project(t, "high")
	h.project(t, "unfunded")
	h.readyEffort(t, "low", 1, 1000)
	h.readyEffort(t, "high", 9, 1000)
	h.readyEffort(t, "unfunded", 99, 0)
	if err := h.st.UpdateSettings(context.Background(), "test", map[string]string{"min_running_efforts": "1"}); err != nil {
		t.Fatal(err)
	}
	if err := h.eng.AutoStartBacklog(context.Background()); err != nil {
		t.Fatal(err)
	}
	high, _ := h.st.ActiveEffort(context.Background(), "high")
	if high.Status != model.EffortRunning {
		t.Fatalf("highest priority effort is %s, want running", high.Status)
	}
	if n, _ := h.st.CountEffortsByStatus(context.Background(), "low", model.EffortRunning); n != 0 {
		t.Fatalf("lower priority effort started with the minimum already met")
	}
	if n, _ := h.st.CountEffortsByStatus(context.Background(), "unfunded", model.EffortRunning); n != 0 {
		t.Fatalf("an effort without a budget auto-started")
	}
	if st := h.eng.Runs().State("high"); st != model.RunRunning {
		t.Fatalf("run state = %s, want running", st)
	}
}

func TestAF15_ParkAndUnpark(t *testing.T) {
	h := newHarness(t)
	h.start()
	h.project(t, "p")
	id := h.readyEffort(t, "p", 3, 1000)
	// readyEffort parks it already; check the stored metadata, then unpark.
	ef, err := h.st.GetEffort(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if ef.Status != model.EffortReady || ef.Priority != 3 {
		t.Fatalf("effort = %+v", ef)
	}
	if _, err := h.eng.Unpark(context.Background(), "owner", id); err != nil {
		t.Fatal(err)
	}
	if ef, _ = h.st.GetEffort(context.Background(), id); ef.Status != model.EffortQueueAppr {
		t.Fatalf("after unpark status = %s", ef.Status)
	}
	if _, err := h.eng.Park(context.Background(), "owner", id, nil, nil); err != nil {
		t.Fatal(err)
	}
	if ef, _ = h.st.GetEffort(context.Background(), id); ef.Status != model.EffortReady {
		t.Fatalf("after park status = %s", ef.Status)
	}
}

func TestAF15_StartConditionParserAndSkip(t *testing.T) {
	c, err := ParseStartCondition("branch feature/turn-loop contains feature/fs3-engine-10")
	if err != nil || c.Branch != "feature/turn-loop" || c.Contains != "feature/fs3-engine-10" {
		t.Fatalf("condition = %+v, %v", c, err)
	}
	if _, err := ParseStartCondition("when it is ready"); err == nil {
		t.Fatal("a malformed condition was accepted")
	}
	h := newHarness(t)
	h.start()
	h.project(t, "p")
	id := h.readyEffort(t, "p", 1, 1000)
	if _, err := h.eng.Unpark(context.Background(), "test", id); err != nil {
		t.Fatal(err)
	}
	cond := "branch main contains feature/not-there"
	if _, err := h.eng.Park(context.Background(), "test", id, nil, &cond); err != nil {
		t.Fatal(err)
	}
	if err := h.st.UpdateSettings(context.Background(), "test", map[string]string{"min_running_efforts": "1"}); err != nil {
		t.Fatal(err)
	}
	if err := h.eng.AutoStartBacklog(context.Background()); err != nil {
		t.Fatal(err)
	}
	if ef, _ := h.st.GetEffort(context.Background(), id); ef.Status != model.EffortReady {
		t.Fatalf("condition did not hold but status = %s", ef.Status)
	}
}
