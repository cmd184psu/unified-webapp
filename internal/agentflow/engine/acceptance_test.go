package engine

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"cmd184psu/unified-webapp/internal/agentflow/model"
	"cmd184psu/unified-webapp/internal/agentflow/runner"
)

const (
	acceptGap = `{"verdict":"gaps","summary":"one gap","gaps":[{"requirement":"R1","evidence":"a.txt line 1","problem":"the page does the sums itself","fix":"call the API","files":["fix.txt"]}]}`
	acceptOK  = `{"verdict":"pass","summary":"all met","gaps":[]}`
)

func (c *chainRun) frd() {
	c.t.Helper()
	raw := `{"title":"T","objective":"Do.","requirements":[{"id":"R1","description":"All sums go through the API","priority":"must"}],"acceptance_criteria":[{"id":"AC1","requirements":["R1"],"criterion":"it works"}]}`
	if _, err := c.st.SaveDoc(context.Background(), "t", c.effort, model.DocFRD, []byte(raw)); err != nil {
		c.t.Fatal(err)
	}
}

func (c *chainRun) acceptanceRounds(n string) {
	c.t.Helper()
	if err := c.st.UpdateSettings(context.Background(), "t", map[string]string{"acceptance_rounds": n}); err != nil {
		c.t.Fatal(err)
	}
}

func acceptStages(c *chainRun) int {
	n := 0
	stages, _ := c.st.ListEffortStages(context.Background(), c.effort)
	for _, st := range stages {
		if isAcceptance(st) {
			n++
		}
	}
	return n
}

// H31: a gap found in the finished work becomes a follow-up task that goes
// through the usual worker, verify and review; the effort completes when a
// second check passes.
func TestH31_AGapBecomesAFollowUpTaskAndTheEffortCompletesWhenTheCheckPasses(t *testing.T) {
	c := newChainRun(t, nil, chainDefs()[:1])
	c.frd()
	// Reviewer calls come in order: T1's review, the first check, the
	// follow-up's review, the second check.
	c.script(1, reviewPass, nil)
	c.fake.Script("reviewer", runner.Step{Reply: acceptGap})
	c.script(1, reviewPass, nil)
	c.fake.Script("reviewer", runner.Step{Reply: acceptOK})
	c.run()
	c.waitComplete()
	if n := acceptStages(c); n != 2 {
		t.Fatalf("acceptance checks = %d, want 2", n)
	}
	tasks, _ := c.st.ListTasks(context.Background(), c.effort)
	if len(tasks) != 2 || tasks[1].Key != "A1-1" || tasks[1].Phase != "acceptance" {
		t.Fatalf("tasks = %+v", tasks)
	}
	if !strings.Contains(tasks[1].Prompt, "the page does the sums itself") || !strings.Contains(tasks[1].Acceptance[0], "R1") {
		t.Fatalf("follow-up task = %+v", tasks[1])
	}
	if got := c.frdFor(tasks[1]); !strings.Contains(got, "All sums go through the API") {
		t.Fatalf("worker is not shown the requirement wording: %q", got)
	}
}

func (c *chainRun) frdFor(t model.Task) string { return c.eng.frdSection(context.Background(), t) }

// H31: a check that finds nothing completes the effort at once.
func TestH31_ACleanCheckCompletesTheEffort(t *testing.T) {
	c := newChainRun(t, nil, chainDefs()[:1])
	c.frd()
	c.script(1, reviewPass, nil)
	c.fake.Script("reviewer", runner.Step{Reply: acceptOK})
	c.run()
	c.waitComplete()
	if n := acceptStages(c); n != 1 {
		t.Fatalf("acceptance checks = %d, want 1", n)
	}
	if tasks, _ := c.st.ListTasks(context.Background(), c.effort); len(tasks) != 1 {
		t.Fatalf("tasks = %d", len(tasks))
	}
	calls := c.fake.CallsFor("reviewer")
	last := calls[len(calls)-1]
	if filepath.Base(last.Request.Workdir) != "_integration" || !strings.Contains(last.Prompt, "All sums go through the API") {
		t.Fatalf("the check ran in %s with prompt %.200q", last.Request.Workdir, last.Prompt)
	}
}

// H31: gaps left after the last round are recorded and the effort completes;
// acceptance_rounds 0 turns the check off.
func TestH31_GapsLeftAfterTheLastRoundDoNotBlockCompletion(t *testing.T) {
	c := newChainRun(t, nil, chainDefs()[:1])
	c.frd()
	c.acceptanceRounds("1")
	c.script(1, reviewPass, nil)
	c.fake.Script("reviewer", runner.Step{Reply: acceptGap})
	c.script(1, reviewPass, nil)
	c.fake.Script("reviewer", runner.Step{Reply: acceptGap})
	c.run()
	c.waitComplete()
	evs, _ := c.eng.effortEvents(context.Background(), c.effort, eventAcceptanceLeft)
	if len(evs) != 1 {
		t.Fatalf("gaps_left events = %d", len(evs))
	}
}

func TestH31_RoundsZeroTurnsTheCheckOff(t *testing.T) {
	c := newChainRun(t, nil, chainDefs()[:1])
	c.frd()
	c.acceptanceRounds("0")
	c.script(1, reviewPass, nil)
	c.run()
	c.waitComplete()
	if n := acceptStages(c); n != 0 {
		t.Fatalf("acceptance checks = %d, want 0", n)
	}
}

// H31: a check that cannot run never holds the effort back.
func TestH31_ACheckThatCannotRunIsSkippedAndTheEffortCompletes(t *testing.T) {
	c := newChainRun(t, nil, chainDefs()[:1])
	c.frd()
	c.script(1, reviewPass, nil)
	c.fake.Script("reviewer", runner.Step{Reply: reviewBad}, runner.Step{Reply: reviewBad}, runner.Step{Reply: reviewBad}, runner.Step{Reply: reviewBad})
	c.run()
	c.waitComplete()
	evs, _ := c.eng.effortEvents(context.Background(), c.effort, eventAcceptanceSkipped)
	if len(evs) != 1 {
		t.Fatalf("skipped events = %d", len(evs))
	}
}
