package engine

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"cmd184psu/unified-webapp/internal/agentflow/model"
	"cmd184psu/unified-webapp/internal/agentflow/runner"
)

const (
	portAsk       = "A greeter that listens on port 7070 and serves GET /api/hello"
	draftWithPort = `{"title":"Greeter","objective":"Serve a greeting on port 7070.","requirements":[{"description":"GET /api/hello returns hello"}],"acceptance_criteria":[{"criterion":"GET /api/hello returns hello"}]}`
)

// H1: a draft that drops an exact string of the request (the port) gets one
// more try whose prompt names the string; a draft that still drops it fails
// with the string named, and the FRD stays as it was.
func TestH1_DraftThatDropsTheRequestsPortIsRetriedThenAccepted(t *testing.T) {
	f := newPlanFix(t, false)
	f.standardPool()
	f.fake.Script("planner", runner.Step{Reply: draftReply}, runner.Step{Reply: draftWithPort})
	f.startEngine()
	ctx := context.Background()
	if err := f.eng.StartDraft(ctx, "alice", f.effort, DraftRequest{Purpose: PurposeDraft, Description: portAsk}); err != nil {
		t.Fatal(err)
	}
	if v := f.waitDraft("idle"); v.Error != "" {
		t.Fatalf("state = %+v", v)
	}
	calls := f.fake.CallsFor("planner")
	if len(calls) != 2 {
		t.Fatalf("planner calls = %d, want the draft and one retry", len(calls))
	}
	for i, c := range calls {
		if !strings.Contains(c.Prompt, "Exact strings that must appear") || !strings.Contains(c.Prompt, "`7070`") || !strings.Contains(c.Prompt, "`/api/hello`") {
			t.Fatalf("call %d does not name the exact strings:\n%s", i, c.Prompt)
		}
	}
	frd, rev := f.latestFRD()
	if rev != 2 || !strings.Contains(model.FRDText(frd), "7070") {
		t.Fatalf("revision %d, FRD text %q", rev, model.FRDText(frd))
	}
}

func TestH1_DraftThatKeepsDroppingAStringFailsNamingIt(t *testing.T) {
	f := newPlanFix(t, false)
	f.standardPool()
	f.fake.Script("planner", runner.Step{Reply: draftReply}, runner.Step{Reply: draftReply}, runner.Step{Reply: draftReply}, runner.Step{Reply: draftReply})
	f.startEngine()
	ctx := context.Background()
	if err := f.eng.StartDraft(ctx, "alice", f.effort, DraftRequest{Purpose: PurposeDraft, Description: portAsk}); err != nil {
		t.Fatal(err)
	}
	v := f.waitDraft("failed")
	if !strings.Contains(v.Error, "7070") || !strings.Contains(v.Error, "/api/hello") {
		t.Fatalf("error = %q, want the missing strings named", v.Error)
	}
	if n := len(f.fake.CallsFor("planner")); n != 4 {
		t.Fatalf("planner calls = %d, want the draft and three retries", n)
	}
	if _, rev := f.latestFRD(); rev != 1 {
		t.Fatalf("a failed draft changed the FRD: revision %d", rev)
	}
}

// A request without exact strings is never retried or failed on their account.
func TestH1_DescriptionWithoutExactStringsIsNotChecked(t *testing.T) {
	f := newPlanFix(t, false)
	f.standardPool()
	f.fake.Script("planner", runner.Step{Reply: draftReply})
	f.startEngine()
	if err := f.eng.StartDraft(context.Background(), "alice", f.effort, DraftRequest{Purpose: PurposeDraft, Description: "make a friendly greeter"}); err != nil {
		t.Fatal(err)
	}
	f.waitDraft("idle")
	if n := len(f.fake.CallsFor("planner")); n != 1 {
		t.Fatalf("planner calls = %d", n)
	}
}

const (
	compileNoPath   = `{"tasks":[{"id":"T001","phase":"P1","title":"api","prompt":"create the endpoints in api.go","acceptance":["R1: the endpoints answer","AC1: it answers"],"files_touched":["api.go"]}]}`
	compileWithPath = `{"tasks":[{"id":"T001","phase":"P1","title":"api","prompt":"create GET /api/hello in api.go","acceptance":["R1: GET /api/hello returns hello","AC1: it answers"],"files_touched":["api.go"]}]}`
)

func (f *planFix) frdNaming(path string) {
	f.t.Helper()
	raw := `{"title":"Greeter","objective":"Greet.","requirements":[{"id":"R1","description":"GET ` + path + ` returns hello","priority":"must"}],"acceptance_criteria":[{"id":"AC1","requirements":["R1"],"criterion":"it answers"}]}`
	if _, err := f.st.SaveDoc(context.Background(), "t", f.effort, model.DocFRD, []byte(raw)); err != nil {
		f.t.Fatal(err)
	}
}

// H1: a compiled queue must carry every exact string of the FRD; one that does
// not gets one more try, whose prompt names the strings.
func TestH1_CompiledQueueThatDropsAPathIsRetried(t *testing.T) {
	f := newPlanFix(t, false)
	f.standardPool()
	f.frdNaming("/api/hello")
	f.approvedPlan()
	f.fake.Script("compiler", runner.Step{Reply: compileNoPath}, runner.Step{Reply: compileWithPath})
	f.startEngine()
	if err := f.eng.Compile(context.Background(), "alice", f.effort); err != nil {
		t.Fatal(err)
	}
	f.waitStatus(model.EffortQueueDraft)
	calls := f.fake.CallsFor("compiler")
	if len(calls) != 2 {
		t.Fatalf("compiler calls = %d, want the compile and one retry", len(calls))
	}
	for i, c := range calls {
		if !strings.Contains(c.Prompt, "`/api/hello`") {
			t.Fatalf("call %d does not name the path:\n%s", i, c.Prompt)
		}
	}
}

func TestH1_CompiledQueueThatKeepsDroppingAPathHaltsNamingIt(t *testing.T) {
	f := newPlanFix(t, false)
	f.standardPool()
	f.frdNaming("/api/hello")
	f.approvedPlan()
	f.fake.Script("compiler", runner.Step{Reply: compileNoPath}, runner.Step{Reply: compileNoPath}, runner.Step{Reply: compileNoPath})
	f.startEngine()
	if err := f.eng.Compile(context.Background(), "alice", f.effort); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "compile halted", func() bool {
		st := f.effortStages("compiler")
		return len(st) == 3 && st[2].State == model.StageDone && f.status() == model.EffortPlanApproved
	})
	evs, err := f.eng.effortEvents(context.Background(), f.effort, eventCompileHalted)
	if err != nil || len(evs) != 1 || !strings.Contains(evs[0].DetailJSON, "/api/hello") {
		t.Fatalf("halt events = %+v, %v", evs, err)
	}
}

// H1: the worker and the reviewer see the approved requirement behind a
// criterion word for word, so a paraphrase cannot hide a path or a port.
func TestH1_WorkerAndReviewerPromptsCarryTheApprovedRequirementText(t *testing.T) {
	h := newHarness(t)
	h.start()
	ctx := context.Background()
	for _, role := range []string{"worker", "reviewer"} {
		fx := h.attemptStage("b-"+role, role)
		frd := `{"title":"T","objective":"o","requirements":[{"id":"R1","description":"Listen on port 7070"},{"id":"R2","description":"POST /api/add adds"}],` +
			`"acceptance_criteria":[{"id":"AC1","requirements":["R2"],"criterion":"POST /api/add returns {result}"}]}`
		if _, err := h.st.SaveDoc(ctx, "t", fx.payload.Effort, model.DocFRD, []byte(frd)); err != nil {
			t.Fatal(err)
		}
		if _, err := h.st.EditTask(ctx, "t", fx.payload.Effort, model.TaskDef{Key: "T1", Phase: "P1", Title: "first", Prompt: "do it",
			Acceptance: []string{"AC1: adds work"}}); err != nil {
			t.Fatal(err)
		}
		st, err := h.st.GetStage(ctx, fx.stage.ID)
		if err != nil {
			t.Fatal(err)
		}
		got, err := h.eng.buildPrompt(ctx, st, fx.payload.Project)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"approved requirements behind these criteria", "R2: POST /api/add adds", "AC1: POST /api/add returns {result}"} {
			if !strings.Contains(got, want) {
				t.Fatalf("%s prompt lacks %q:\n%s", role, want, got)
			}
		}
	}
}

// H19: a drafter that links every criterion to every requirement must not
// hand a task the whole FRD; a task that names a requirement is held to the
// requirements it names.
func TestH19_TaskNamingARequirementIsNotGivenEveryLinkedRequirement(t *testing.T) {
	h := newHarness(t)
	h.start()
	ctx := context.Background()
	fx := h.attemptStage("b-narrow", "worker")
	frd := `{"title":"T","objective":"o","requirements":[{"id":"R1","description":"Listen on port 7070"},{"id":"R2","description":"POST /api/add adds"}],` +
		`"acceptance_criteria":[{"id":"AC1","requirements":["R1","R2"],"criterion":"the page loads"}]}`
	if _, err := h.st.SaveDoc(ctx, "t", fx.payload.Effort, model.DocFRD, []byte(frd)); err != nil {
		t.Fatal(err)
	}
	if _, err := h.st.EditTask(ctx, "t", fx.payload.Effort, model.TaskDef{Key: "T1", Phase: "P1", Title: "first", Prompt: "do it",
		Acceptance: []string{"R1: the server starts", "AC1: the page loads"}}); err != nil {
		t.Fatal(err)
	}
	st, err := h.st.GetStage(ctx, fx.stage.ID)
	if err != nil {
		t.Fatal(err)
	}
	got, err := h.eng.buildPrompt(ctx, st, fx.payload.Project)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "R1: Listen on port 7070") || strings.Contains(got, "R2: POST /api/add adds") {
		t.Fatalf("prompt:\n%s", got)
	}
}

func TestH1_TaskWithoutRequirementReferencesGetsNoSection(t *testing.T) {
	h := newHarness(t)
	h.start()
	fx := h.attemptStage("b-none", "worker")
	st, err := h.st.GetStage(context.Background(), fx.stage.ID)
	if err != nil {
		t.Fatal(err)
	}
	got, err := h.eng.buildPrompt(context.Background(), st, fx.payload.Project)
	if err != nil || strings.Contains(got, "approved requirements") {
		t.Fatalf("err %v, prompt:\n%s", err, got)
	}
}

// H14: a queue that leaves an FRD requirement or acceptance criterion uncited,
// or cites an id the FRD does not have, gets one more try whose prompt lists the
// ids and says what was wrong; a second such queue halts the compile.
func TestH14_QueueThatCitesNoAcceptanceCriterionIsRetried(t *testing.T) {
	f := newPlanFix(t, false)
	f.standardPool()
	f.frdNaming("/api/hello")
	f.approvedPlan()
	uncited := strings.Replace(compileWithPath, `,"AC1: it answers"`, "", 1)
	f.fake.Script("compiler", runner.Step{Reply: uncited}, runner.Step{Reply: compileWithPath})
	f.startEngine()
	if err := f.eng.Compile(context.Background(), "alice", f.effort); err != nil {
		t.Fatal(err)
	}
	f.waitStatus(model.EffortQueueDraft)
	calls := f.fake.CallsFor("compiler")
	if len(calls) != 2 {
		t.Fatalf("compiler calls = %d, want the compile and one retry", len(calls))
	}
	if !strings.Contains(calls[0].Prompt, "## FRD ids") || strings.Contains(calls[0].Prompt, "Fix these") {
		t.Fatalf("first prompt should list the ids and no problems:\n%s", calls[0].Prompt)
	}
	if !strings.Contains(calls[1].Prompt, "Fix these") || !strings.Contains(calls[1].Prompt, "no task cites AC1") {
		t.Fatalf("retry prompt does not say what was wrong:\n%s", calls[1].Prompt)
	}
}

func TestH14_QueueThatKeepsLeavingARequirementUncitedHalts(t *testing.T) {
	f := newPlanFix(t, false)
	f.standardPool()
	f.frdNaming("/api/hello")
	f.approvedPlan()
	foreign := strings.Replace(compileWithPath, `"AC1: it answers"`, `"R18: my own step"`, 1)
	f.fake.Script("compiler", runner.Step{Reply: foreign}, runner.Step{Reply: foreign}, runner.Step{Reply: foreign})
	f.startEngine()
	if err := f.eng.Compile(context.Background(), "alice", f.effort); err != nil {
		t.Fatal(err)
	}
	f.waitStatus(model.EffortPlanApproved)
	if n := len(f.fake.CallsFor("compiler")); n != 3 {
		t.Fatalf("compiler calls = %d, want 3 (the compile and two retries)", n)
	}
}

// A draft that gave up is listed for the owner, and the list clears when the
// owner starts the draft again.
func TestAttentionListsAFailedDraft(t *testing.T) {
	f := newPlanFix(t, false)
	f.standardPool()
	f.fake.Script("planner", runner.Step{Reply: draftReply}, runner.Step{Reply: draftReply}, runner.Step{Reply: draftReply}, runner.Step{Reply: draftReply}, runner.Step{Reply: draftReply})
	f.startEngine()
	ctx := context.Background()
	if items, err := f.eng.Attention(ctx); err != nil || len(items) != 0 {
		t.Fatalf("before anything: %v, %v", items, err)
	}
	if err := f.eng.StartDraft(ctx, "alice", f.effort, DraftRequest{Purpose: PurposeDraft, Description: portAsk}); err != nil {
		t.Fatal(err)
	}
	f.waitDraft("failed")
	items, err := f.eng.Attention(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Kind != "draft_failed" || items[0].Effort != f.effort || !strings.Contains(items[0].Message, "7070") || items[0].Since == 0 {
		t.Fatalf("attention = %+v", items)
	}
	if err := f.eng.StartDraft(ctx, "alice", f.effort, DraftRequest{Purpose: PurposeDraft, Description: portAsk}); err != nil {
		t.Fatal(err)
	}
	if items, _ := f.eng.Attention(ctx); len(items) != 0 {
		t.Fatalf("a new draft left the old failure listed: %+v", items)
	}
}

// H26: a critic that crashes is not handed back to the model that crashed
// before it, and when planning gives up the owner is told (the effort is back
// at draft by then, which the attention list used to skip).
func TestH26_PlanCrashesMoveToOtherModelsAndAreListedWhenPlanningHalts(t *testing.T) {
	f := newPlanFix(t, false)
	f.addBackend("gpu", 2, false, false, "m1", "m2", "m3", "m4", "m5", "m6")
	f.candidates("global", "", "planner", "gpu/m1")
	f.candidates("global", "", "architect", "gpu/m2")
	f.candidates("global", "", "critic", "gpu/m3", "gpu/m4", "gpu/m5", "gpu/m6")
	ctx := context.Background()
	if err := f.st.UpdateSettings(ctx, "t", map[string]string{"max_infra_retries": "3"}); err != nil {
		t.Fatal(err)
	}
	f.eng.deps.Clock = func() time.Time { return time.Now().Add(time.Hour) }
	crash := runner.Step{ErrClass: runner.ErrCrash, Err: os.ErrInvalid}
	f.fake.Script("planner", runner.Step{Reply: planReply})
	f.fake.Script("architect", runner.Step{Reply: approveReply})
	f.fake.Script("critic", crash, crash, crash)
	f.startEngine()
	if err := f.eng.GeneratePlan(ctx, "alice", f.effort); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "planning halted", func() bool {
		items, _ := f.eng.Attention(ctx)
		return len(items) == 1 && items[0].Kind == "plan_halted"
	})
	seen := map[string]bool{}
	for _, st := range f.effortStages("critic") {
		if seen[st.Model] {
			t.Fatalf("critic crashed on %s and was handed to it again: %+v", st.Model, f.effortStages("critic"))
		}
		seen[st.Model] = true
	}
	if len(seen) != 3 {
		t.Fatalf("critic stages = %d, want 3", len(seen))
	}
}
