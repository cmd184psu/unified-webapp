package engine

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"cmd184psu/unified-webapp/internal/agentflow/model"
	"cmd184psu/unified-webapp/internal/agentflow/runner"
	"cmd184psu/unified-webapp/internal/agentflow/store"
	"cmd184psu/unified-webapp/internal/taskmaster/worker"
)

const (
	frdJSON      = `{"id":"FRD-T","title":"t","objective":"o","requirements":[{"id":"R1","description":"d"}]}`
	planReply    = `{"principles":["p"],"drivers":["d"],"options":[{"name":"A","description":"a"},{"name":"B","description":"b"}],"phases":[{"id":"P1","name":"n","tasks_description":"t"}],"adr":{"decision":"d","drivers":["x"],"alternatives":["y"],"why":"w","consequences":["c"],"followups":["f"]}}`
	approveReply = `{"verdict":"approve","issues":[]}`
	archMarker   = "ARCH-MARKER-7f3a9c"
	archRevise   = `{"verdict":"revise","summary":"` + archMarker + `","issues":[{"description":"` + archMarker + ` split phase P1","severity":"major"}]}`
	criticRevise = `{"verdict":"revise","summary":"needs a verification phase","issues":[{"description":"no phase verifies R1","severity":"major"}]}`
)

// fastPoll makes taskmaster's worker poll every 20 ms; it must run before the
// harness opens taskmaster.
func fastPoll(t *testing.T) {
	t.Helper()
	worker.SetPollIntervalForTest(20 * time.Millisecond)
	t.Cleanup(func() { worker.SetPollIntervalForTest(5 * time.Second) })
}

// planFix is a started harness with a project whose effort has a saved FRD.
type planFix struct {
	*harness
	slug   string
	effort int64
}

// newPlanFix builds the harness (lanes at width 0 when widthZero, so no stage
// ever runs) and a project with an effort holding an FRD. The engine is not
// started and no backend exists yet.
func newPlanFix(t *testing.T, widthZero bool) *planFix {
	t.Helper()
	fastPoll(t)
	h := newHarness(t)
	h.widthZ = widthZero
	h.start()
	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	return seedPlanFix(t, h, repo)
}

// seedPlanFix creates the project over repo and an effort with a saved FRD on
// a started harness.
func seedPlanFix(t *testing.T, h *harness, repo string) *planFix {
	t.Helper()
	ctx := context.Background()
	f := &planFix{harness: h, slug: "proj"}
	if err := h.st.CreateProject(ctx, "t", store.Project{Slug: f.slug, RepoPath: repo, ModelScope: "any"}); err != nil {
		t.Fatal(err)
	}
	ef, err := h.st.CreateEffort(ctx, "t", f.slug, "e", false)
	if err != nil {
		t.Fatal(err)
	}
	f.effort = ef.ID
	if _, err := h.st.SaveDoc(ctx, "t", ef.ID, model.DocFRD, []byte(frdJSON)); err != nil {
		t.Fatal(err)
	}
	return f
}

// standardPool is one backend with three models and one planning role each,
// so planner, architect and critic can be pairwise different.
func (f *planFix) standardPool() {
	f.addBackend("gpu", 2, false, false, "m1", "m2", "m3")
	f.candidates("global", "", "planner", "gpu/m1")
	f.candidates("global", "", "architect", "gpu/m2")
	f.candidates("global", "", "critic", "gpu/m3")
	f.candidates("global", "", "compiler", "gpu/m1")
}

func (f *planFix) status() string {
	f.t.Helper()
	ef, err := f.st.GetEffort(context.Background(), f.effort)
	if err != nil {
		f.t.Fatal(err)
	}
	return ef.Status
}

func (f *planFix) waitStatus(want string) {
	f.t.Helper()
	waitFor(f.t, "effort status "+want, func() bool { return f.status() == want })
}

func (f *planFix) effortStages(role string) []model.Stage {
	f.t.Helper()
	all, err := f.st.ListEffortStages(context.Background(), f.effort)
	if err != nil {
		f.t.Fatal(err)
	}
	var out []model.Stage
	for _, st := range all {
		if role == "" || st.Role == role {
			out = append(out, st)
		}
	}
	return out
}

// AC-RP1, RP1, RP2, RP3, RP4, MB6: the critic's prompts never carry the
// architect's review (a unique marker in every architect reply is absent from
// all five), a critic that always says revise runs exactly plan_max_rounds
// rounds with the planner receiving both reviews, the effort then waits at
// plan_ready flagged not critic-approved, every plan and review is a revision,
// nothing was approved, and the three roles ran on three different models.
func TestC10_CriticNeverSeesTheArchitectAndTheLoopStopsAtMaxRounds(t *testing.T) {
	f := newPlanFix(t, false)
	f.standardPool()
	// This test is about plan_max_rounds (5); keep AF-15's default revise cap
	// (3) out of the way so the older stop is the one observed.
	if err := f.st.UpdateSettings(context.Background(), "test", map[string]string{"plan_revise_limit": "10"}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		f.fake.Script("planner", runner.Step{Reply: planReply})
		f.fake.Script("architect", runner.Step{Reply: archRevise})
		f.fake.Script("critic", runner.Step{Reply: criticRevise})
	}
	f.startEngine()
	ctx := context.Background()
	if err := f.eng.GeneratePlan(ctx, "alice", f.effort); err != nil {
		t.Fatal(err)
	}
	f.waitStatus(model.EffortPlanReady)

	for role, want := range map[string]int{"planner": 5, "architect": 5, "critic": 5} {
		if n := len(f.fake.CallsFor(role)); n != want {
			t.Fatalf("%s calls = %d, want %d", role, n, want)
		}
	}
	for i, c := range f.fake.CallsFor("critic") {
		if strings.Contains(c.Prompt, archMarker) || !strings.Contains(c.Prompt, "FRD-T") || !strings.Contains(c.Prompt, `"principles"`) {
			t.Fatalf("critic prompt %d must hold the FRD and the plan and none of the architect's review:\n%s", i, c.Prompt)
		}
	}
	for i, c := range f.fake.CallsFor("planner") {
		if has := strings.Contains(c.Prompt, archMarker); has != (i > 0) {
			t.Fatalf("planner prompt %d contains the architect review = %v, want %v (revision rounds get both reviews)", i, has, i > 0)
		}
		if i > 0 && !strings.Contains(c.Prompt, "no phase verifies R1") {
			t.Fatalf("planner prompt %d lacks the critic's review", i)
		}
	}
	models := map[string]string{}
	for _, st := range f.effortStages("") {
		models[st.Role] = st.Model
	}
	if models["planner"] != "m1" || models["architect"] != "m2" || models["critic"] != "m3" {
		t.Fatalf("models = %v, want three different ones", models)
	}

	v, err := f.eng.PlanView(ctx, f.effort)
	if err != nil {
		t.Fatal(err)
	}
	if v.Rounds != 5 || v.Revision != 5 || !v.NotCriticApproved || len(v.Warnings) != 0 || len(v.Reviews) != 10 {
		t.Fatalf("view = rounds %d revision %d notApproved %v warnings %v reviews %d", v.Rounds, v.Revision, v.NotCriticApproved, v.Warnings, len(v.Reviews))
	}
	if d, err := f.st.GetDoc(ctx, f.effort, model.DocPlan, 0); err != nil || d.Revision != 5 {
		t.Fatalf("plan revisions = %d (%v), want 5", d.Revision, err)
	}
	if d, err := f.st.GetDoc(ctx, f.effort, model.DocReview, 0); err != nil || d.Revision != 10 {
		t.Fatalf("review revisions = %d (%v), want 10", d.Revision, err)
	}
	if _, err := f.st.ApprovedRevision(ctx, f.effort, model.DocPlan); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("exhaustion must not approve the plan: %v", err)
	}
}

// RP3, RP4, MB6: critic approval alone leaves the effort at plan_ready; only
// ApprovePlan moves it on, recording who approved which revision. With a
// one-model pool the loop still runs and warns that the roles share a model.
func TestC10_CriticApprovalStillWaitsForAHuman(t *testing.T) {
	f := newPlanFix(t, false)
	f.addBackend("solo", 2, false, false, "m1")
	for _, role := range []string{"planner", "architect", "critic"} {
		f.candidates("global", "", role, "solo/m1")
	}
	f.fake.Script("planner", runner.Step{Reply: planReply})
	f.startEngine()
	ctx := context.Background()
	if err := f.eng.GeneratePlan(ctx, "alice", f.effort); err != nil {
		t.Fatal(err)
	}
	f.waitStatus(model.EffortPlanReady)

	v, err := f.eng.PlanView(ctx, f.effort)
	if err != nil {
		t.Fatal(err)
	}
	if v.Rounds != 1 || v.Revision != 1 || v.NotCriticApproved || len(v.Warnings) != 2 || !strings.Contains(v.Warnings[0]+v.Warnings[1], "MB6") {
		t.Fatalf("view = %+v", v)
	}
	if _, err := f.st.ApprovedRevision(ctx, f.effort, model.DocPlan); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("critic approval alone must not approve the plan: %v", err)
	}
	if err := f.eng.ApprovePlan(ctx, "alice", f.effort); err != nil {
		t.Fatal(err)
	}
	a, err := f.st.ApprovedRevision(ctx, f.effort, model.DocPlan)
	if err != nil || a.Revision != 1 || a.ApprovedBy != "alice" || f.status() != model.EffortCompiling {
		t.Fatalf("approval = %+v (%v), status %s", a, err, f.status())
	}
	// No compiler candidate exists: the approval stands and the effort says why it waits.
	if pv, err := f.eng.PlanView(ctx, f.effort); err != nil || pv.Waiting == "" {
		t.Fatalf("waiting reason = %q (%v), want one", pv.Waiting, err)
	}
	if err := f.eng.ApprovePlan(ctx, "alice", f.effort); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("second approval = %v, want conflict", err)
	}
}

// RP2: the presented revision is the latest critic revise over any reject, and
// the latest wins among equals; full approval presents the newest plan.
func TestC10_BestRevisionPrefersReviseOverReject(t *testing.T) {
	plans := []planRec{{Revision: 1}, {Revision: 2}, {Revision: 3}, {Revision: 4}}
	crit := func(round int, verdict string) PlanReview {
		return PlanReview{Role: "critic", Round: round, PlanRevision: round, Verdict: verdict}
	}
	arch := func(round int, verdict string) PlanReview {
		return PlanReview{Role: "architect", Round: round, PlanRevision: round, Verdict: verdict}
	}
	for _, tc := range []struct {
		name    string
		reviews []PlanReview
		rev     int
		notAppr bool
	}{
		{"revise beats later rejects", []PlanReview{crit(1, "reject"), crit(2, "revise"), crit(3, "reject"), crit(4, "reject")}, 2, true},
		{"latest revise wins", []PlanReview{crit(1, "revise"), crit(2, "reject"), crit(3, "revise"), crit(4, "reject")}, 3, true},
		{"all rejects: latest", []PlanReview{crit(1, "reject"), crit(2, "reject")}, 2, true},
		{"both approve: newest plan", []PlanReview{crit(1, "revise"), arch(4, "approve"), crit(4, "approve")}, 4, false},
	} {
		s := &planSession{plans: plans, reviews: tc.reviews}
		if rev, notAppr := s.presented(); rev != tc.rev || notAppr != tc.notAppr {
			t.Errorf("%s: presented = %d, %v; want %d, %v", tc.name, rev, notAppr, tc.rev, tc.notAppr)
		}
	}
}

// RP1 restart and BK4: a planning stage queued on a backend that is then
// retired is rerouted to another backend, and one lost with a server restart
// is submitted again; neither counts against the stage's retries.
func TestC10_PlanStagesSurviveBackendRetireAndRestart(t *testing.T) {
	f := newPlanFix(t, true)
	f.addBackend("gpu1", 1, false, false, "m1")
	f.addBackend("gpu2", 1, false, false, "m2")
	f.candidates("global", "", "planner", "gpu1/m1", "gpu2/m2")
	ctx := context.Background()
	if err := f.eng.GeneratePlan(ctx, "alice", f.effort); err != nil {
		t.Fatal(err)
	}
	if st := f.effortStages("planner"); len(st) != 1 || st[0].Backend != "gpu1" {
		t.Fatalf("stages = %+v, want one planner on gpu1", st)
	}
	if _, err := f.eng.RetireBackend(ctx, "alice", "gpu1", RetireOptions{}); err != nil {
		t.Fatal(err)
	}
	st := f.effortStages("planner")
	if len(st) != 2 || st[0].Outcome != OutcomeInfraRerouted || st[1].Backend != "gpu2" || st[1].State != model.StageQueued {
		t.Fatalf("after retire: %+v", st)
	}

	f.restart()
	f.startEngine()
	st = f.effortStages("planner")
	if len(st) != 3 || st[1].Outcome != OutcomeInfraLost || st[2].Backend != "gpu2" || st[2].State != model.StageQueued {
		t.Fatalf("after restart: %+v", st)
	}
	if n := len(f.openStages()); n != 1 {
		t.Fatalf("open stages = %d, want exactly the resubmitted planner", n)
	}
	if f.status() != model.EffortPlanning {
		t.Fatalf("status = %s, want planning", f.status())
	}
}

// AC-RP5: in deliberate mode the planner prompt carries the pre-mortem and
// test-plan rules, a plan without three scenarios goes to the SO2 corrective
// (one extra call on the same model), and the critic is told to check both.
// A plain effort sees none of it.
func TestRP5_DeliberateModeAddsPremortemRulesAndValidation(t *testing.T) {
	deliberatePlan := strings.TrimSuffix(planReply, "}") + `,"premortem":["s1: cause, effect, mitigation","s2: cause, effect, mitigation","s3: cause, effect, mitigation"],"test_plan":["unit","integration","e2e","observability"]}`
	run := func(t *testing.T, deliberate bool, planner ...runner.Step) *planFix {
		f := newPlanFix(t, false)
		f.standardPool()
		on := deliberate
		if _, err := f.st.UpdateEffort(context.Background(), "t", f.effort, store.EffortPatch{Deliberate: &on}); err != nil {
			t.Fatal(err)
		}
		f.fake.Script("planner", planner...)
		f.startEngine()
		if err := f.eng.GeneratePlan(context.Background(), "alice", f.effort); err != nil {
			t.Fatal(err)
		}
		f.waitStatus(model.EffortPlanReady)
		return f
	}

	t.Run("deliberate", func(t *testing.T) {
		f := run(t, true, runner.Step{Reply: planReply}, runner.Step{Reply: deliberatePlan})
		calls := f.fake.CallsFor("planner")
		if len(calls) != 2 {
			t.Fatalf("planner calls = %d, want the plan and one corrective", len(calls))
		}
		if !strings.Contains(calls[0].Prompt, "exactly three scenarios") || !strings.Contains(calls[0].Prompt, "observability") {
			t.Fatalf("planner prompt lacks the deliberate rules:\n%s", calls[0].Prompt)
		}
		if !strings.Contains(calls[1].Prompt, "premortem: deliberate mode needs exactly 3 failure scenarios") {
			t.Fatalf("corrective does not name the missing pre-mortem:\n%s", calls[1].Prompt)
		}
		if calls[1].Request.Model != calls[0].Request.Model {
			t.Fatal("the corrective must go to the same model")
		}
		crit := f.fake.CallsFor("critic")
		if len(crit) == 0 || !strings.Contains(crit[0].Prompt, "premortem") || !strings.Contains(crit[0].Prompt, "unit, integration, end-to-end and observability") {
			t.Fatalf("critic prompt lacks the deliberate checks")
		}
		v, err := f.eng.PlanView(context.Background(), f.effort)
		var pd struct {
			Premortem []string `json:"premortem"`
		}
		if err != nil || json.Unmarshal(v.Plan, &pd) != nil || len(pd.Premortem) != 3 {
			t.Fatalf("plan view = %s (%v), want three scenarios", v.Plan, err)
		}
	})
	t.Run("plain", func(t *testing.T) {
		f := run(t, false, runner.Step{Reply: planReply})
		calls := f.fake.CallsFor("planner")
		if len(calls) != 1 || strings.Contains(calls[0].Prompt, "Deliberate mode") || strings.Contains(calls[0].Prompt, "{{") {
			t.Fatalf("a plain effort must get no deliberate text and no raw placeholder: %d calls", len(calls))
		}
		if c := f.fake.CallsFor("critic"); len(c) == 0 || strings.Contains(c[0].Prompt, "Deliberate mode") || strings.Contains(c[0].Prompt, "{{") {
			t.Fatal("critic prompt for a plain effort")
		}
	})
}

// A view that refetches on the engine's notice must see the new status: the
// notice sent when a stage's outcome is stored comes before the effort moves
// to plan_ready, so one more notice must follow the status change.
func TestPlanReadyIsAnnouncedAfterTheStatusChanges(t *testing.T) {
	fastPoll(t)
	h := newHarness(t)
	var f *planFix
	var seenReady atomic.Int64
	h.tweak = func(d *Deps) {
		d.Kick = func() {
			if f == nil {
				return
			}
			if ef, err := h.st.GetEffort(context.Background(), f.effort); err == nil && ef.Status == model.EffortPlanReady {
				seenReady.Add(1)
			}
		}
	}
	h.start()
	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	f = seedPlanFix(t, h, repo)
	f.addBackend("solo", 2, false, false, "m1")
	for _, role := range []string{"planner", "architect", "critic"} {
		f.candidates("global", "", role, "solo/m1")
	}
	f.fake.Script("planner", runner.Step{Reply: planReply})
	f.startEngine()
	if err := f.eng.GeneratePlan(context.Background(), "alice", f.effort); err != nil {
		t.Fatal(err)
	}
	f.waitStatus(model.EffortPlanReady)
	waitFor(t, "a notice after plan_ready", func() bool { return seenReady.Load() >= 1 })
}

// Approving the plan starts the compile by itself, as the approver; the
// queue still waits for a human to approve its hash.
func TestP2_PlanApprovalStartsCompileAndQueueStaysHumanGated(t *testing.T) {
	f := newPlanFix(t, false)
	f.standardPool()
	f.fake.Script("planner", runner.Step{Reply: planReply})
	f.fake.Script("architect", runner.Step{Reply: approveReply})
	f.fake.Script("critic", runner.Step{Reply: approveReply})
	f.fake.Script("compiler", runner.Step{Reply: compileClean})
	f.startEngine()
	ctx := context.Background()
	if err := f.eng.GeneratePlan(ctx, "alice", f.effort); err != nil {
		t.Fatal(err)
	}
	f.waitStatus(model.EffortPlanReady)
	if err := f.eng.ApprovePlan(ctx, "alice", f.effort); err != nil {
		t.Fatal(err)
	}
	f.waitStatus(model.EffortQueueDraft)
	if n := len(f.fake.CallsFor("compiler")); n != 1 {
		t.Fatalf("compiler calls = %d, want 1", n)
	}
	evs, err := f.st.ListEvents(ctx, "", "", 100000)
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range evs {
		if ev.Action == "compile.start" && ev.Actor != "alice" {
			t.Errorf("compile.start actor = %s, want the approver", ev.Actor)
		}
		if ev.Action == "queue.approve" {
			t.Errorf("the queue was approved without a human")
		}
	}
	if q, err := f.st.GetQueue(ctx, f.effort); err != nil || q.Status != model.QueueDraft {
		t.Fatalf("queue = %+v (%v), want a draft", q, err)
	}
}

// With no compiler candidate the approval still succeeds and the effort waits
// in compiling; once a candidate exists the next tick compiles, with no
// second click.
func TestP2_CompileWaitingForACandidateStartsWhenOneAppears(t *testing.T) {
	f := newPlanFix(t, false)
	f.addBackend("gpu", 2, false, false, "m1", "m2", "m3")
	f.candidates("global", "", "planner", "gpu/m1")
	f.candidates("global", "", "architect", "gpu/m2")
	f.candidates("global", "", "critic", "gpu/m3")
	f.fake.Script("planner", runner.Step{Reply: planReply})
	f.fake.Script("architect", runner.Step{Reply: approveReply})
	f.fake.Script("critic", runner.Step{Reply: approveReply})
	f.fake.Script("compiler", runner.Step{Reply: compileClean})
	f.startEngine()
	ctx := context.Background()
	if err := f.eng.GeneratePlan(ctx, "alice", f.effort); err != nil {
		t.Fatal(err)
	}
	f.waitStatus(model.EffortPlanReady)
	if err := f.eng.ApprovePlan(ctx, "alice", f.effort); err != nil {
		t.Fatalf("approval must not fail without a compiler candidate: %v", err)
	}
	if pv, err := f.eng.PlanView(ctx, f.effort); err != nil || f.status() != model.EffortCompiling || pv.Waiting == "" {
		t.Fatalf("status %s, waiting %q (%v)", f.status(), pv.Waiting, err)
	}
	f.candidates("global", "", "compiler", "gpu/m1")
	if err := f.eng.AdvancePlans(ctx); err != nil {
		t.Fatal(err)
	}
	f.waitStatus(model.EffortQueueDraft)
}

// An approved plan whose compile never began (a restart between approval and
// start) is started by the next tick; one whose compile already ran and halted
// is left for the human, so a failing compiler is not retried every tick.
func TestP2_ApprovedPlanResumesCompileOnlyIfItNeverRan(t *testing.T) {
	f := newPlanFix(t, false)
	f.addBackend("gpu", 2, false, false, "m1", "m2", "m3")
	f.candidates("global", "", "planner", "gpu/m1")
	f.candidates("global", "", "architect", "gpu/m2")
	f.candidates("global", "", "critic", "gpu/m3")
	f.candidates("global", "", "compiler", "gpu/m1")
	f.fake.Script("planner", runner.Step{Reply: planReply})
	f.fake.Script("architect", runner.Step{Reply: approveReply})
	f.fake.Script("critic", runner.Step{Reply: approveReply})
	f.fake.Script("compiler", runner.Step{Reply: compileClean})
	f.startEngine()
	ctx := context.Background()
	if err := f.eng.GeneratePlan(ctx, "alice", f.effort); err != nil {
		t.Fatal(err)
	}
	f.waitStatus(model.EffortPlanReady)
	if err := f.eng.ApprovePlan(ctx, "alice", f.effort); err != nil {
		t.Fatal(err)
	}
	f.waitStatus(model.EffortQueueDraft)

	// Never ran: roll the effort back to plan_approved and delete nothing; the
	// compiler stage exists, so a tick must leave it alone (it halted).
	approved := model.EffortPlanApproved
	if _, err := f.st.UpdateEffort(ctx, "t", f.effort, store.EffortPatch{Status: &approved}); err != nil {
		t.Fatal(err)
	}
	before := len(f.fake.CallsFor("compiler"))
	if err := f.eng.AdvancePlans(ctx); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	if got := len(f.fake.CallsFor("compiler")); got != before || f.status() != model.EffortPlanApproved {
		t.Fatalf("a halted compile was retried: calls %d -> %d, status %s", before, got, f.status())
	}
}

func TestP2_ApprovedPlanWithNoCompileYetIsResumedByTheNextTick(t *testing.T) {
	f := newPlanFix(t, false)
	f.addBackend("gpu", 2, false, false, "m1", "m2", "m3")
	f.candidates("global", "", "planner", "gpu/m1")
	f.candidates("global", "", "architect", "gpu/m2")
	f.candidates("global", "", "critic", "gpu/m3")
	f.candidates("global", "", "compiler", "gpu/m1")
	f.fake.Script("planner", runner.Step{Reply: planReply})
	f.fake.Script("architect", runner.Step{Reply: approveReply})
	f.fake.Script("critic", runner.Step{Reply: approveReply})
	f.fake.Script("compiler", runner.Step{Reply: compileClean})
	f.startEngine()
	ctx := context.Background()
	if err := f.eng.GeneratePlan(ctx, "alice", f.effort); err != nil {
		t.Fatal(err)
	}
	f.waitStatus(model.EffortPlanReady)
	// The state a crash leaves between the approval and the compile start.
	v, err := f.eng.PlanView(ctx, f.effort)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.st.ApproveDoc(ctx, "alice", f.effort, model.DocPlan, v.Revision); err != nil {
		t.Fatal(err)
	}
	approved := model.EffortPlanApproved
	if _, err := f.st.UpdateEffort(ctx, "alice", f.effort, store.EffortPatch{Status: &approved}); err != nil {
		t.Fatal(err)
	}
	if err := f.eng.AdvancePlans(ctx); err != nil {
		t.Fatal(err)
	}
	f.waitStatus(model.EffortQueueDraft)
}
