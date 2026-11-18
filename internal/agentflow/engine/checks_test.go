package engine

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"cmd184psu/unified-webapp/internal/agentflow/runner"
	"cmd184psu/unified-webapp/internal/agentflow/store"
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

// H29: a model nobody has rated counts as tier 3 if local and tier 2 if
// hosted; a rating the owner set is kept.
func TestH29_UnratedLocalModelsCountAsLight(t *testing.T) {
	f := newPlanFix(t, true)
	f.addBackend("box", 1, false, false, "a")
	f.addBackend("cloud", 1, true, false, "b")
	snap, err := f.st.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if a, b := effectiveTier(snap, "box", "a"), effectiveTier(snap, "cloud", "b"); a != 3 || b != 2 {
		t.Fatalf("unrated local = tier %d, hosted = tier %d, want 3 and 2", a, b)
	}
	// a local model rated strong still never counts above tier 2; a hosted one does
	if err := f.eng.AddBackend(context.Background(), "t", store.Backend{Name: "rated", Host: "http://127.0.0.1:11436", APIType: "ollama", Slots: 1, Models: []store.BackendModel{{Model: "x", Tier: 1}}}); err != nil {
		t.Fatal(err)
	}
	if err := f.eng.AddBackend(context.Background(), "t", store.Backend{Name: "cloud2", Host: "", APIType: "opencode", Hosted: true, Slots: 1, Models: []store.BackendModel{{Model: "y", Tier: 1}}}); err != nil {
		t.Fatal(err)
	}
	snap, _ = f.st.Snapshot(context.Background())
	if l, h := effectiveTier(snap, "rated", "x"), effectiveTier(snap, "cloud2", "y"); l != 2 || h != 1 {
		t.Fatalf("local rated 1 = tier %d, hosted rated 1 = tier %d, want 2 and 1", l, h)
	}
}

// H29: before a run starts, Preflight says which role has nothing that can
// serve it and which worker would have no reviewer.
func TestH29_PreflightNamesRolesWithNoModelAndWorkersWithNoReviewer(t *testing.T) {
	f := newPlanFix(t, true)
	f.addBackend("gpu", 2, false, false, "m1", "m2")
	f.candidates("global", "", "planner", "gpu/m1")
	f.candidates("global", "", "architect", "gpu/m2")
	f.candidates("global", "", "critic", "gpu/m2")
	f.candidates("global", "", "compiler", "gpu/m1")
	f.candidates("global", "", "worker", "gpu/m1")
	f.candidates("global", "", "reviewer", "gpu/m1") // the worker's own model: no one else can review
	ctx := context.Background()
	v, err := f.eng.Preflight(ctx, f.slug)
	if err != nil {
		t.Fatal(err)
	}
	all := strings.Join(v.Problems, "\n")
	if v.OK || !strings.Contains(all, "worker gpu/m1 would have no reviewer") {
		t.Fatalf("preflight = %+v", v)
	}
	f.candidates("global", "", "reviewer", "gpu/m2")
	v, err = f.eng.Preflight(ctx, f.slug)
	if err != nil || !v.OK || len(v.Roles) != 6 {
		t.Fatalf("with a second model to review: %+v (%v)", v, err)
	}
	if err := f.st.SaveModelCheck(ctx, store.ModelCheck{Backend: "gpu", Model: "m2", Class: "failing", Message: "gone", Checked: time.Now().UnixMilli()}); err != nil {
		t.Fatal(err)
	}
	if v, _ = f.eng.Preflight(ctx, f.slug); v.OK || !strings.Contains(strings.Join(v.Problems, "\n"), "no model can serve critic") {
		t.Fatalf("a role with no candidates: %+v", v)
	}
}

// AF-1: a backend whose stored last check failed is re-checked at startup.
func TestAF1_RecheckFailingBackendsAtStartup(t *testing.T) {
	f := newPlanFix(t, false)
	f.addBackend("gpu", 1, false, false, "m1")
	f.fake.Script("critic", runner.Step{Reply: "ok"})
	ctx := context.Background()
	// store a failing last result for the model
	if err := f.st.SaveModelCheck(ctx, store.ModelCheck{Backend: "gpu", Model: "m1", OK: false, Class: "failing", Checked: f.eng.deps.Clock().UnixMilli()}); err != nil {
		t.Fatal(err)
	}
	f.eng.recheckFailingBackends(ctx)
	var v CheckView
	waitFor(t, "recheck finished", func() bool {
		v, _ = f.eng.CheckStatus(ctx, "gpu")
		return v.State == "idle" && len(v.Results) == 1
	})
	if !v.Results[0].OK {
		t.Fatalf("model not rechecked: %+v", v.Results[0])
	}
}
