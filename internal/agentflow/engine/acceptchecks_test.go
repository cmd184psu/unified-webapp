package engine

import (
	"context"
	"strings"
	"testing"

	"cmd184psu/unified-webapp/internal/agentflow/runner"
	"cmd184psu/unified-webapp/internal/agentflow/schema"
	"cmd184psu/unified-webapp/internal/agentflow/verify"
)

func TestCoveredGap(t *testing.T) {
	for _, tc := range []struct {
		g    schema.Gap
		want bool
	}{
		{schema.Gap{Requirement: "AC2", Problem: "gofmt, go vet and go test could not be verified", Evidence: "no output available"}, true},
		{schema.Gap{Requirement: "AC2", Problem: "the tests were not run", Evidence: "I cannot execute commands"}, true},
		{schema.Gap{Requirement: "AC3", Problem: "unable to confirm the build succeeds"}, true},
		{schema.Gap{Requirement: "R1", Problem: "the page does the sums itself", Evidence: "app.js line 4"}, false},
		{schema.Gap{Requirement: "R2", Problem: "TestPeace is missing", Evidence: "rules/peace_test.go has no test of the truce"}, false},
		{schema.Gap{Requirement: "R3", Problem: "the save could not be verified to round-trip", Evidence: "save.go line 9 drops the field"}, false},
	} {
		if got := coveredGap(tc.g); got != tc.want {
			t.Errorf("coveredGap(%q) = %v, want %v", tc.g.Problem, got, tc.want)
		}
	}
	green := verify.VerifyReport{Pass: true, Checks: []verify.Check{{Name: verify.CheckFeedbackPrefix + "go vet ./...", Pass: true}}}
	gaps := []schema.Gap{{Problem: "go vet could not be run"}, {Problem: "the page does the sums itself"}}
	if kept, dropped := dropCoveredGaps(gaps, green, true); len(kept) != 1 || len(dropped) != 1 {
		t.Fatalf("green report: kept %v dropped %v", kept, dropped)
	}
	red := green
	red.Pass = false
	if kept, _ := dropCoveredGaps(gaps, red, true); len(kept) != 2 {
		t.Fatal("a failing report settles nothing")
	}
	if kept, _ := dropCoveredGaps(gaps, verify.VerifyReport{Pass: true}, true); len(kept) != 2 {
		t.Fatal("no feedback commands: nothing is covered")
	}
}

const acceptUnverified = `{"verdict":"gaps","summary":"checks","gaps":[{"requirement":"AC1","evidence":"no command output is available to me","problem":"gofmt, go vet and go test could not be verified","fix":"run gofmt, go vet and go test","files":["T1.txt"]}]}`

// AF-13 (fs3-peace/9, A1-6): a gap that only says the project's checks could not be verified is
// settled by the merged head's verify report: no follow-up task, the effort completes, and the
// check was shown the report.
func TestAF13_AcceptanceTrustsTheMergedHeadsChecks(t *testing.T) {
	c := newChainRun(t, []string{"sh check.sh"}, chainDefs()[:1])
	c.frd()
	c.script(1, reviewPass, nil)
	c.fake.Script("reviewer", runner.Step{Reply: acceptUnverified})
	c.run()
	c.waitComplete()
	if tasks, _ := c.st.ListTasks(context.Background(), c.effort); len(tasks) != 1 {
		t.Fatalf("tasks = %d, want no follow-up", len(tasks))
	}
	evs, _ := c.eng.effortEvents(context.Background(), c.effort, eventAcceptancePassed)
	if len(evs) != 1 || !strings.Contains(evs[0].DetailJSON, "covered_by_verify") {
		t.Fatalf("passed events = %+v", evs)
	}
	calls := c.fake.CallsFor("reviewer")
	if p := calls[len(calls)-1].Prompt; !strings.Contains(p, "`sh check.sh`: passed") || strings.Contains(p, "{{ENGINE_CHECKS}}") {
		t.Fatalf("the check was not shown the merged head's report:\n%s", p)
	}
}

const acceptRealGap = `{"verdict":"gaps","summary":"one gap","gaps":[{"requirement":"R1","evidence":"T1.txt line 1","problem":"the page does the sums itself","fix":"call the API","files":["T1.txt"]}]}`

const workerGapNoChanges = `{"summary":"already met","files_changed":[],"learnings":[],"no_changes_reason":"T1.txt already calls the API on line 1"}`

// AF-13: a gap task whose worker changes nothing, with a reason, and whose verify passes on the
// unchanged head is done without a review or a coordinator answer; the check then runs again.
func TestAF13_ANoChangeGapTaskPassesOnVerify(t *testing.T) {
	c := newChainRun(t, []string{"sh check.sh"}, chainDefs()[:1])
	c.frd()
	c.script(1, reviewPass, nil)
	c.fake.Script("reviewer", runner.Step{Reply: acceptRealGap})
	c.fake.Script("worker", runner.Step{Reply: workerGapNoChanges})
	c.fake.Script("reviewer", runner.Step{Reply: acceptOK})
	c.run()
	c.waitComplete()
	tasks, _ := c.st.ListTasks(context.Background(), c.effort)
	if len(tasks) != 2 || tasks[1].Key != "A1-1" {
		t.Fatalf("tasks = %+v", tasks)
	}
	if n := len(c.fake.CallsFor("reviewer")); n != 3 {
		t.Fatalf("reviewer calls = %d, want 3 (T1's review and two checks, none for the gap task)", n)
	}
	as, _ := c.st.ListAttempts(context.Background(), tasks[1].ID)
	if len(as) != 1 || as[0].Outcome != "passed" {
		t.Fatalf("gap task attempts = %+v", as)
	}
	if ev, ok := c.event("attempt", as[0].ID, "attempt.no_changes"); !ok || !strings.Contains(ev.DetailJSON, "calls the API") {
		t.Fatalf("no_changes event = %+v %v", ev, ok)
	}
	if p := c.fake.CallsFor("worker")[1].Prompt; !strings.Contains(p, "do not stop or ask for input because you cannot run those commands") {
		t.Fatalf("the gap task's prompt does not say the engine runs the checks:\n%.600s", p)
	}
}
