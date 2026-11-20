package engine

import (
	"context"
	"strings"
	"testing"

	"cmd184psu/unified-webapp/internal/agentflow/runner"
	"cmd184psu/unified-webapp/internal/agentflow/schema"
)

const (
	reviewPhasePlayers1 = `{"verdict":"repair","summary":"no","issues":[{"description":"PhasePlayers is never enabled after the players step","severity":"major","file":"src/engine/turn.go","line":40}],"repair_prompt":"enable PhasePlayers"}`
	reviewPhasePlayers2 = `{"verdict":"repair","summary":"still no","issues":[{"description":"the players step still never enables PhasePlayers","severity":"major","file":"src/engine/turn.go","line":44}],"repair_prompt":"enable it"}`
	resolverClarify     = `{"decision":"override_finding","reason":"PhasePlayers can only be enabled by the last players sub-step, which task T011 writes.","evidence":["src/engine/turn.go:40","EngineTests/TurnTests.cs:88"],"finding":"PhasePlayers is never enabled after the players step"}`
)

func TestSameFinding(t *testing.T) {
	a := schema.Issue{File: "src/engine/turn.go", Line: 40, Description: "PhasePlayers is never enabled after the players step"}
	b := schema.Issue{File: "src/engine/turn.go", Line: 44, Description: "the players step still never enables PhasePlayers"}
	if !sameFinding(a, b) {
		t.Fatal("the same finding in other words")
	}
	if sameFinding(a, schema.Issue{File: "src/engine/turn.go", Line: 200, Description: a.Description}) {
		t.Fatal("a line far away is another finding")
	}
	if sameFinding(a, schema.Issue{File: "src/engine/turn.go", Line: 41, Description: "the save loader drops the truce counter"}) {
		t.Fatal("other words are another finding")
	}
}

// AF-13 3b (fs3-engine/10, T010): the reviewer raises the same finding twice in other words, so
// the loop stops after two attempts (not all of them), the resolver decides the finding is out of
// scope, the task's prompt says so, and the next attempt passes.
func TestAF13_ARepeatedFindingGoesToTheResolverAndTheTaskContinues(t *testing.T) {
	ctx := context.Background()
	c := newChainRun(t, nil, chainDefs()[:1])
	c.addBackend("big", 2, false, false, "claude-sonnet-5-5")
	c.candidates("global", "", "resolver", "big/claude-sonnet-5-5")
	c.fake.Script("worker", runner.Step{Reply: workerOK, Edit: writeWork}, runner.Step{Reply: workerOK, Edit: writeWork}, runner.Step{Reply: workerOK, Edit: writeWork})
	c.fake.Script("reviewer", runner.Step{Reply: reviewPhasePlayers1}, runner.Step{Reply: reviewPhasePlayers2}, runner.Step{Reply: resolverClarify}, runner.Step{Reply: reviewPass})
	c.run()
	c.waitComplete()
	as := c.attempts(0)
	if len(as) != 3 {
		t.Fatalf("attempts = %d, want 3 (two repeats, then the resolved one)", len(as))
	}
	if as[0].Fingerprint != as[1].Fingerprint {
		t.Fatalf("the reworded finding should share the fingerprint: %q %q", as[0].Fingerprint, as[1].Fingerprint)
	}
	evs, _ := c.eng.effortEvents(ctx, c.effort, eventTaskResolved)
	if len(evs) != 1 || !strings.Contains(evs[0].DetailJSON, "override_finding") || !strings.Contains(evs[0].DetailJSON, "claude-sonnet-5-5") {
		t.Fatalf("resolved events = %+v", evs)
	}
	tk, _ := c.st.GetTask(ctx, c.tasks[0].ID)
	if !strings.Contains(tk.Prompt, "Out of scope for this task") || !strings.Contains(tk.Prompt, "PhasePlayers is never enabled") {
		t.Fatalf("task prompt = %q", tk.Prompt)
	}
	calls := c.fake.CallsFor("reviewer")
	if p := calls[2].Prompt; !strings.Contains(p, "# Resolver") || !strings.Contains(p, "PhasePlayers") || calls[2].Request.Workdir != as[1].Worktree {
		t.Fatalf("resolver call: workdir %s, prompt %.300q", calls[2].Request.Workdir, p)
	}
	if p := c.fake.CallsFor("worker")[2].Prompt; !strings.Contains(p, "Out of scope for this task") {
		t.Fatalf("the next worker is not shown the decision")
	}
}

// A second failure streak after a resolver decision goes to the owner, not to the resolver again.
func TestAF13_ASecondStreakAfterAResolutionEscalates(t *testing.T) {
	ctx := context.Background()
	c := newChainRun(t, nil, chainDefs()[:1])
	c.addBackend("big", 2, false, false, "claude-sonnet-5-5")
	c.candidates("global", "", "resolver", "big/claude-sonnet-5-5")
	c.fake.Script("worker", runner.Step{Reply: workerOK, Edit: writeWork}, runner.Step{Reply: workerOK, Edit: writeWork}, runner.Step{Reply: workerOK, Edit: writeWork}, runner.Step{Reply: workerOK, Edit: writeWork})
	c.fake.Script("reviewer", runner.Step{Reply: reviewPhasePlayers1}, runner.Step{Reply: reviewPhasePlayers2}, runner.Step{Reply: resolverClarify},
		runner.Step{Reply: reviewPhasePlayers1}, runner.Step{Reply: reviewPhasePlayers2})
	c.run()
	waitFor(t, "the escalation", func() bool {
		tk, _ := c.st.GetTask(ctx, c.tasks[0].ID)
		return strings.Contains(tk.WaitingReason, "needs a human (resolver)")
	})
	if n := len(c.fake.CallsFor("reviewer")); n != 5 {
		t.Fatalf("reviewer-role calls = %d, want 5 (no second resolver run)", n)
	}
}
