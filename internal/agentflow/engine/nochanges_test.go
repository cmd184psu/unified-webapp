package engine

import (
	"strings"
	"testing"

	"cmd184psu/unified-webapp/internal/agentflow/model"
	"cmd184psu/unified-webapp/internal/agentflow/runner"
)

const workerNoChanges = `{"summary":"already done","files_changed":[],"learnings":[],"no_changes_reason":"T1 helper F in lib.go already has the tests"}`

func noChangesDefs() []model.TaskDef { return chainDefs()[:2] }

// A worker that declares nothing to change, an unchanged tree and a passing
// review end the attempt passed with no commit and no merge stage; the
// dependent task runs and the event is recorded.
func TestNoChanges_DeclaredReasonPassesWithoutMerge(t *testing.T) {
	c := newChainRun(t, nil, noChangesDefs())
	c.fake.Script("worker", runner.Step{Reply: workerNoChanges}, runner.Step{Reply: workerOK, Edit: writeWork})
	c.fake.Script("reviewer", runner.Step{Reply: reviewPass}, runner.Step{Reply: reviewPass})
	c.run()
	ef := c.waitComplete()

	as := c.attempts(0)
	if len(as) != 1 || as[0].Outcome != model.OutcomePassed {
		t.Fatalf("T1 attempts = %+v", as)
	}
	if got := roles(t, c); got != "worker,verify,reviewer" {
		t.Fatalf("T1 stages = %s, want no merge stage", got)
	}
	if ev, ok := c.event("attempt", as[0].ID, "attempt.no_changes"); !ok || !strings.Contains(ev.DetailJSON, "lib.go") {
		t.Fatalf("attempt.no_changes event = %+v, %v", ev, ok)
	}
	if log := gitT(t, c.repo, "log", "--format=%B", ef.IntegrationBranch); strings.Contains(log, "Agentflow-Task: T1") || !strings.Contains(log, "Agentflow-Task: T2") {
		t.Fatalf("integration log:\n%s", log)
	}
	if rev := c.fake.CallsFor("reviewer")[0].Prompt; !strings.Contains(rev, "## The worker says nothing needs changing") || !strings.Contains(rev, "lib.go") {
		t.Fatalf("reviewer prompt lacks the declaration:\n%s", rev)
	}
	if c.exists(as[0].Worktree) {
		t.Fatal("worktree of the no-changes attempt was not removed")
	}
	if a2 := c.attempts(1); len(a2) != 1 || a2[0].Outcome != model.OutcomePassed {
		t.Fatalf("dependent T2 attempts = %+v", a2)
	}
}

// With cleanup enabled the cleanup pass is skipped when there is nothing to clean.
func TestNoChanges_SkipsCleanup(t *testing.T) {
	c := newChainRun(t, nil, chainDefs()[:1])
	c.enableCleanup()
	c.fake.Script("worker", runner.Step{Reply: workerNoChanges})
	c.fake.Script("reviewer", runner.Step{Reply: reviewPass})
	c.run()
	c.waitComplete()
	if got := roles(t, c); got != "worker,verify,reviewer" {
		t.Fatalf("stages = %s", got)
	}
	if n := len(c.fake.CallsFor("cleanup")); n != 0 {
		t.Fatalf("cleanup calls = %d", n)
	}
}

// Without a reason an unchanged tree still fails no_changes.
func TestNoChanges_WithoutReasonStillFails(t *testing.T) {
	c := newChainRun(t, nil, chainDefs()[:1])
	c.fake.Script("worker", runner.Step{Reply: workerOK})
	c.run()
	waitFor(t, "attempt failed", func() bool {
		as := c.attempts(0)
		return len(as) > 0 && as[0].Outcome == model.OutcomeFailed
	})
	if got := roles(t, c); got != "worker,verify" {
		t.Fatalf("stages = %s", got)
	}
}

// A reviewer who rejects the claim sends a repair prompt to the next worker.
func TestNoChanges_ReviewerRejectsClaim(t *testing.T) {
	c := newChainRun(t, nil, chainDefs()[:1])
	c.setLimits(map[string]string{"max_attempts": "3", "stuck_threshold": "3"})
	c.fake.Script("worker", runner.Step{Reply: workerNoChanges}, runner.Step{Reply: workerOK, Edit: writeWork})
	c.fake.Script("reviewer",
		runner.Step{Reply: `{"verdict":"repair","summary":"missing","issues":[{"description":"no test for G","severity":"major"}],"repair_prompt":"add the missing test for G"}`},
		runner.Step{Reply: reviewPass})
	c.run()
	c.waitComplete()
	as := c.attempts(0)
	if len(as) != 2 || as[0].Outcome != model.OutcomeFailed || as[1].Outcome != model.OutcomePassed {
		t.Fatalf("attempts = %+v", as)
	}
	if p := c.fake.CallsFor("worker")[1].Prompt; !strings.Contains(p, "add the missing test for G") {
		t.Fatalf("repair prompt did not reach the worker:\n%s", p)
	}
}

// A declared reason does not excuse real changes: they are committed and merged.
func TestNoChanges_ReasonWithChangesIsNormal(t *testing.T) {
	c := newChainRun(t, nil, chainDefs()[:1])
	c.fake.Script("worker", runner.Step{Reply: workerNoChanges, Edit: writeWork})
	c.fake.Script("reviewer", runner.Step{Reply: reviewPass})
	c.run()
	ef := c.waitComplete()
	if got := roles(t, c); got != "worker,verify,reviewer,merge" {
		t.Fatalf("stages = %s", got)
	}
	if !strings.Contains(gitT(t, c.repo, "ls-tree", "-r", "--name-only", ef.IntegrationBranch), "T1.txt") {
		t.Fatal("T1.txt was not merged")
	}
	if _, ok := c.event("attempt", c.attempts(0)[0].ID, "attempt.no_changes"); ok {
		t.Fatal("no_changes event recorded for an attempt that changed files")
	}
}
