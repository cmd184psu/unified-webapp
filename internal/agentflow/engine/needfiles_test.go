package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cmd184psu/unified-webapp/internal/agentflow/model"
	"cmd184psu/unified-webapp/internal/agentflow/runner"
)

const workerNeedsState = `{"summary":"ported the truce step; the state needs a field","files_changed":["src/rules/truce.go"],"learnings":[],"needs_files":[{"path":"src/game/state.go","reason":"the truce needs a TruceTurns field on State"}]}`

// AF-13 (fs3-engine/10, T005): a worker that needs a file outside its list says so; the task
// stops as needs_input with the file and reason (no second model is tried, nothing is
// exhausted), and allowing it adds the file to files_touched and retries in the same worktree.
func TestAF13_NeedsFilesStopsForTheOwnerAndAllowRetriesInTheSameWorktree(t *testing.T) {
	ctx := context.Background()
	defs := []model.TaskDef{{Key: "T005", Phase: "P", Title: "port the truce", Prompt: "Port the truce into `src/rules/truce.go`.",
		Acceptance: []string{"ok"}, FilesTouched: []string{"src/rules/truce.go"}}}
	c := newChainRun(t, nil, defs)
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
	tk, _ := c.st.GetTask(ctx, c.tasks[0].ID)
	if !strings.Contains(tk.WaitingReason, "src/game/state.go") || !strings.Contains(tk.WaitingReason, "TruceTurns") {
		t.Fatalf("waiting reason = %q", tk.WaitingReason)
	}
	// The harness leaves its effort in draft; attention lists a running effort's tasks.
	if ok, err := c.st.UpdateEffortStatusIf(ctx, "t", c.effort, model.EffortDraft, model.EffortRunning); err != nil || !ok {
		t.Fatalf("effort to running: %v %v", ok, err)
	}
	items, err := c.eng.Attention(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, it := range items {
		if it.Kind == "task_needs_files" && it.Task == "T005" && len(it.NeedsFiles) == 1 && it.NeedsFiles[0].Path == "src/game/state.go" {
			found = true
		}
	}
	if !found {
		ef, _ := c.st.GetEffort(ctx, c.effort)
		st, _ := c.st.TaskStatuses(ctx, c.effort, model.Limits{MaxAttempts: 5})
		t.Fatalf("attention = %+v (effort %s, status %v)", items, ef.Status, st)
	}
	if _, err := c.eng.AllowFiles(ctx, "owner", tk.ID, []string{"src/other.go"}); err == nil {
		t.Fatal("a file the worker did not ask for must be refused")
	}

	first := c.attempts(0)[0].Worktree
	c.fake.Script("worker", runner.Step{Reply: workerOK, Edit: func(r runner.Request) error {
		if r.Workdir != first {
			t.Errorf("the retry ran in %s, want the same worktree %s", r.Workdir, first)
		}
		if _, err := os.Stat(filepath.Join(r.Workdir, "src/rules/truce.go")); err != nil {
			t.Errorf("the first attempt's work is gone: %v", err)
		}
		if err := os.MkdirAll(filepath.Join(r.Workdir, "src/game"), 0o755); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(r.Workdir, "src/game/state.go"), []byte("package game\n"), 0o644)
	}})
	c.fake.Script("reviewer", runner.Step{Reply: reviewPass})
	allowed, err := c.eng.AllowFiles(ctx, "owner", tk.ID, nil)
	if err != nil || strings.Join(allowed, ",") != "src/game/state.go" {
		t.Fatalf("allow = %v %v", allowed, err)
	}
	c.waitComplete()
	tk, _ = c.st.GetTask(ctx, tk.ID)
	if strings.Join(tk.FilesTouched, ",") != "src/rules/truce.go,src/game/state.go" {
		t.Fatalf("files_touched = %v", tk.FilesTouched)
	}
	if n := len(c.fake.CallsFor("worker")); n != 2 {
		t.Fatalf("worker calls = %d, want 2 (no second model tried before the owner)", n)
	}
	if p := c.fake.CallsFor("worker")[1].Prompt; !strings.Contains(p, "The owner allowed these files") {
		t.Fatalf("the retry's prompt does not say the files were allowed:\n%.800s", p)
	}
}
