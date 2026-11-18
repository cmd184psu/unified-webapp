package engine

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"cmd184psu/unified-webapp/internal/agentflow/model"
	"cmd184psu/unified-webapp/internal/agentflow/runner"
	"cmd184psu/unified-webapp/internal/agentflow/store"
)

// openAttempt starts an attempt on the first task with the given stages, each
// closed pass.
func (c *chainRun) openAttempt(roles ...string) model.Attempt {
	c.t.Helper()
	ctx := context.Background()
	wt := filepath.Join(c.t.TempDir(), "wt")
	att, err := c.st.StartAttempt(ctx, c.tasks[0].ID, model.AttemptNormal, wt, "m1")
	if err != nil {
		c.t.Fatal(err)
	}
	for _, r := range roles {
		backend := "gpu"
		if r == "verify" {
			backend = verifyBackend
		}
		st, err := c.st.CreateStage(ctx, store.StageSpec{Attempt: att.ID, Role: r, Backend: backend, Model: "m1"})
		if err != nil {
			c.t.Fatal(err)
		}
		if err := c.st.FinishStage(ctx, st.ID, OutcomePass, "", json.RawMessage(`{"pass":true}`)); err != nil {
			c.t.Fatal(err)
		}
	}
	return att
}

func (c *chainRun) attemptOutcome(id int64) string {
	c.t.Helper()
	a, err := c.st.GetAttempt(context.Background(), id)
	if err != nil {
		c.t.Fatal(err)
	}
	return a.Outcome
}

func (c *chainRun) stageCount(att int64) int {
	c.t.Helper()
	ss, err := c.st.ListAttemptStages(context.Background(), att)
	if err != nil {
		c.t.Fatal(err)
	}
	return len(ss)
}

// RC1: an attempt left at a pass boundary by the previous process is
// abandoned at start; nothing is submitted for the stopped project.
func TestB1_RestartAbandonsAttemptAtPassBoundary(t *testing.T) {
	c := newChainRun(t, nil, chainDefs()[:1])
	att := c.openAttempt("worker", "verify", "reviewer")
	if err := c.eng.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := c.attemptOutcome(att.ID); got != model.OutcomeAbandoned {
		t.Fatalf("outcome = %q, want abandoned", got)
	}
	if err := c.eng.Advance(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n := c.stageCount(att.ID); n != 3 {
		t.Fatalf("stages = %d, want 3 (no merge submitted)", n)
	}
	if n := len(c.attempts(0)); n != 1 {
		t.Fatalf("attempts = %d, want 1 until Start", n)
	}
}

// A crash between StartAttempt and CreateStage leaves an attempt without
// stages; reconcile at start abandons it.
func TestB1_RestartAbandonsStagelessAttempt(t *testing.T) {
	c := newChainRun(t, nil, chainDefs()[:1])
	att := c.openAttempt()
	if err := c.eng.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := c.attemptOutcome(att.ID); got != model.OutcomeAbandoned {
		t.Fatalf("outcome = %q, want abandoned", got)
	}
}

// RC1: Advance on a stopped project abandons instead of submitting; a pausing
// project keeps draining.
func TestB1_AdvanceHonoursRunState(t *testing.T) {
	c := newChainRun(t, nil, chainDefs()[:1])
	ctx := context.Background()
	if err := c.eng.Start(ctx); err != nil {
		t.Fatal(err)
	}
	att := c.openAttempt("worker")
	if err := c.eng.Advance(ctx); err != nil {
		t.Fatal(err)
	}
	if got := c.attemptOutcome(att.ID); got != model.OutcomeAbandoned {
		t.Fatalf("stopped: outcome = %q, want abandoned", got)
	}
	if n := c.stageCount(att.ID); n != 1 {
		t.Fatalf("stopped: stages = %d, want 1", n)
	}

	if err := c.eng.Runs().Start(ctx, "t", c.slug); err != nil {
		t.Fatal(err)
	}
	// Admit nothing new: a hand-made open attempt keeps the project pausing.
	att2 := c.openAttempt("worker")
	if err := c.eng.Runs().Pause(ctx, "t", c.slug); err != nil {
		t.Fatal(err)
	}
	if got := c.eng.Runs().State(c.slug); got != model.RunPausing {
		t.Fatalf("state = %s, want pausing", got)
	}
	if err := c.eng.Advance(ctx); err != nil {
		t.Fatal(err)
	}
	if n := c.stageCount(att2.ID); n != 2 {
		t.Fatalf("pausing: stages = %d, want 2 (verify submitted)", n)
	}
	if got := c.attemptOutcome(att2.ID); got != "" {
		t.Fatalf("pausing: outcome = %q, want open", got)
	}
}

// Stop waits for an Advance in progress, so a step that read "running" has
// submitted before Stop snapshots the in-flight stages.
func TestB1_StopWaitsForAdvance(t *testing.T) {
	c := newChainRun(t, nil, chainDefs()[:1])
	ctx := context.Background()
	if err := c.eng.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := c.eng.Runs().Start(ctx, "t", c.slug); err != nil {
		t.Fatal(err)
	}
	c.eng.advMu.Lock()
	done := make(chan error, 1)
	go func() { done <- c.eng.Runs().Stop(ctx, "t", c.slug) }()
	select {
	case <-done:
		t.Fatal("Stop returned while an Advance held advMu")
	case <-time.After(200 * time.Millisecond):
	}
	if got := c.eng.Runs().State(c.slug); got != model.RunStopped {
		t.Fatalf("state = %s, want stopped before the barrier", got)
	}
	c.eng.advMu.Unlock()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Stop did not return after Advance finished")
	}
}

// A merge stage lost to a crash leaves the integration branch where it was
// before the merge started.
func TestB1_LostMergeResetsIntegration(t *testing.T) {
	c := newChainRun(t, nil, chainDefs()[:1])
	ctx := context.Background()
	proj, err := c.st.GetProject(ctx, c.slug)
	if err != nil {
		t.Fatal(err)
	}
	branch, intWT, err := c.eng.ensureIntegration(ctx, proj, c.effort)
	if err != nil {
		t.Fatal(err)
	}
	before := gitT(t, intWT, "rev-parse", "HEAD")
	att := c.openAttempt("worker", "verify", "reviewer")
	st, err := c.st.CreateStage(ctx, store.StageSpec{Attempt: att.ID, Role: "merge", Backend: mergeBackend})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.eng.recordPreMerge(st.ID, StagePayload{Project: c.slug, Effort: c.effort}, before); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(intWT, "merged.txt"), "x\n")
	gitT(t, intWT, "add", "-A")
	gitT(t, intWT, "commit", "-q", "-m", "half-recorded merge")
	if gitT(t, intWT, "rev-parse", "HEAD") == before {
		t.Fatal("setup: commit did not move HEAD")
	}
	if err := c.eng.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if got := gitT(t, c.repo, "rev-parse", branch); got != before {
		t.Fatalf("integration branch = %s, want %s", got, before)
	}
	got, _ := c.st.GetStage(ctx, st.ID)
	if got.Outcome != OutcomeInfraLost {
		t.Fatalf("stage outcome = %q", got.Outcome)
	}
}

// PX3: a repair after a merge conflict finds the conflicting content with
// markers in its worktree, and the conflict file list in its prompt.
func TestB1_ConflictRepairSeesMarkers(t *testing.T) {
	defs := []model.TaskDef{
		{Key: "a", Phase: "P", Title: "a", Prompt: "do", Acceptance: []string{"ok"}},
		{Key: "b", Phase: "P", Title: "b", Prompt: "do", Acceptance: []string{"ok"}},
	}
	c := newChainRun(t, nil, defs)
	var sawMarkers atomic.Bool
	c.script(3, reviewPass, func(t *testing.T, r runner.Request) error {
		p := filepath.Join(r.Workdir, "shared.txt")
		if b, err := os.ReadFile(p); err == nil && strings.Contains(string(b), "<<<<<<<") {
			sawMarkers.Store(true)
			return os.WriteFile(p, []byte("resolved\n"), 0o644)
		}
		return os.WriteFile(p, []byte("from "+filepath.Base(r.Workdir)+"\n"), 0o644)
	})
	c.run()
	waitFor(t, "effort complete", func() bool {
		ef, _ := c.st.GetEffort(context.Background(), c.effort)
		return ef.Status == model.EffortComplete
	})
	if !sawMarkers.Load() {
		t.Fatal("repair worker never saw conflict markers")
	}
	ef, _ := c.st.GetEffort(context.Background(), c.effort)
	if got := gitT(t, c.repo, "show", ef.IntegrationBranch+":shared.txt"); got != "resolved" {
		t.Fatalf("shared.txt on the integration branch = %q", got)
	}
}
