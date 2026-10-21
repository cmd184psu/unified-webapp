package engine

import (
	"context"
	"os"
	"strings"
	"testing"

	"cmd184psu/unified-webapp/internal/agentflow/runner"
)

// H27: Check models tries each model with a one-word prompt, one after the
// other, and records which answered; a model that failed is passed over by the
// pickers with the reason, and a later passing check puts it back.
func TestH27_CheckModelsRecordsEachModelAndFailedOnesAreSkipped(t *testing.T) {
	f := newPlanFix(t, false)
	f.addBackend("gpu", 2, false, false, "m1", "m2", "m3")
	f.candidates("global", "", "critic", "gpu/m1", "gpu/m2", "gpu/m3")
	f.fake.Script("critic",
		runner.Step{Reply: "ok"},
		runner.Step{ErrClass: runner.ErrCrash, Err: os.ErrInvalid},
		runner.Step{ErrClass: runner.ErrRateLimit, Err: os.ErrInvalid})
	f.startEngine()
	ctx := context.Background()
	if err := f.eng.CheckModels(ctx, "alice", "gpu"); err != nil {
		t.Fatal(err)
	}
	var v CheckView
	waitFor(t, "check finished", func() bool {
		v, _ = f.eng.CheckStatus(ctx, "gpu")
		return v.State == "idle" && len(v.Results) == 3
	})
	got := map[string]string{}
	for _, c := range v.Results {
		got[c.Model] = c.Class
		if c.OK != (c.Class == "") {
			t.Fatalf("result %+v", c)
		}
	}
	if got["m1"] != "" || got["m2"] != "failing" || got["m3"] != "rate_limited" {
		t.Fatalf("results = %v", got)
	}
	p, err := f.st.GetProject(ctx, f.slug)
	if err != nil {
		t.Fatal(err)
	}
	vw, err := f.eng.newView(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var why []string
	for _, c := range vw.snap.CandidatesFor("critic", f.slug) {
		why = append(why, c.Model+": "+skipReason(vw, p, c, false))
	}
	if why[0] != "m1: " || !strings.Contains(why[1], "failed its check (failing)") || !strings.Contains(why[2], "(rate_limited)") {
		t.Fatalf("skip reasons = %q", why)
	}
	f.fake.Script("critic", runner.Step{Reply: "ok"}, runner.Step{Reply: "ok"}, runner.Step{Reply: "ok"})
	if err := f.eng.CheckModels(ctx, "alice", "gpu"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "recheck", func() bool {
		v, _ = f.eng.CheckStatus(ctx, "gpu")
		if v.State != "idle" {
			return false
		}
		for _, c := range v.Results {
			if !c.OK {
				return false
			}
		}
		return true
	})
	if vw, _ = f.eng.newView(ctx); len(vw.failing) != 0 {
		t.Fatalf("a passing check left %v failing", vw.failing)
	}
}
