package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"cmd184psu/unified-webapp/internal/agentflow/model"
	"cmd184psu/unified-webapp/internal/agentflow/store"
)

// dispHarness is a width-zero engine (stages stay queued in the DB, so the
// dispatcher's submissions can be asserted without running anything).
func newDispHarness(t *testing.T) *harness {
	h := newHarness(t)
	h.widthZ = true
	h.start()
	return h
}

func (h *harness) addBackend(name string, slots int, hosted, paid bool, models ...string) {
	h.t.Helper()
	b := store.Backend{Name: name, Host: "http://127.0.0.1:11434", APIType: "ollama", Slots: slots, Hosted: hosted, Paid: paid}
	for _, m := range models {
		b.Models = append(b.Models, store.BackendModel{Model: m})
	}
	if err := h.eng.AddBackend(context.Background(), "t", b); err != nil {
		h.t.Fatal(err)
	}
}

func (h *harness) candidates(scope, project, role string, refs ...string) {
	h.t.Helper()
	var cs []store.CandidateRef
	for _, r := range refs {
		b, m, _ := strings.Cut(r, "/")
		cs = append(cs, store.CandidateRef{Backend: b, Model: m})
	}
	if err := h.st.SetRoleCandidates(context.Background(), "t", scope, project, role, cs); err != nil {
		h.t.Fatal(err)
	}
}

// project creates a project with the given scope and queue.
func (h *harness) dispProject(slug, scope string, defs []model.TaskDef) runFix {
	h.t.Helper()
	ctx := context.Background()
	repo := h.t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
		h.t.Fatal(err)
	}
	if err := h.st.CreateProject(ctx, "t", store.Project{Slug: slug, RepoPath: repo, ModelScope: scope}); err != nil {
		h.t.Fatal(err)
	}
	ef, err := h.st.CreateEffort(ctx, "t", slug, "e", false)
	if err != nil {
		h.t.Fatal(err)
	}
	q, err := h.st.SaveQueue(ctx, "t", ef.ID, defs)
	if err != nil {
		h.t.Fatal(err)
	}
	if _, err := h.st.ApproveQueue(ctx, "t", ef.ID, q.Hash); err != nil {
		h.t.Fatal(err)
	}
	tasks, err := h.st.ListTasks(ctx, ef.ID)
	if err != nil {
		h.t.Fatal(err)
	}
	return runFix{slug: slug, effort: ef.ID, hash: q.Hash, tasks: tasks}
}

func indep(n int) []model.TaskDef {
	var defs []model.TaskDef
	for i := 1; i <= n; i++ {
		defs = append(defs, model.TaskDef{Key: fmt.Sprintf("T%d", i), Phase: "P1", Title: "t", Prompt: "do", Acceptance: []string{"ok"}})
	}
	return defs
}

func (h *harness) startRun(slug string) {
	h.t.Helper()
	if err := h.eng.Runs().Start(context.Background(), "t", slug); err != nil {
		h.t.Fatal(err)
	}
}

func (h *harness) pass() {
	h.t.Helper()
	if err := h.eng.DispatchOnce(context.Background()); err != nil {
		h.t.Fatal(err)
	}
}

// openStages lists the stages still in flight.
func (h *harness) openStages() []model.Stage {
	h.t.Helper()
	st, err := h.st.OpenStages(context.Background())
	if err != nil {
		h.t.Fatal(err)
	}
	return st
}

// finish closes a dispatched work stage and its attempt as passed, freeing
// the slot as a finished chain would.
func (h *harness) finish(st model.Stage) {
	h.t.Helper()
	ctx := context.Background()
	if err := h.st.FinishStage(ctx, st.ID, OutcomePass, "", nil); err != nil {
		h.t.Fatal(err)
	}
	if err := h.st.FinishAttempt(ctx, st.Attempt, model.OutcomePassed, "", "r"); err != nil {
		h.t.Fatal(err)
	}
}

// projectOf reads the project from the attempt's planned worktree path
// (<data>/worktrees/<project>/<effort>/<task>).
func (h *harness) projectOf(st model.Stage) string {
	h.t.Helper()
	a, err := h.st.GetAttempt(context.Background(), st.Attempt)
	if err != nil {
		h.t.Fatal(err)
	}
	return filepath.Base(filepath.Dir(filepath.Dir(a.Worktree)))
}

// standard sets one free backend serving m1 and m2 with both roles listed.
func (h *harness) standard(slots int) {
	h.addBackend("gpu", slots, false, false, "m1", "m2")
	h.candidates("global", "", "worker", "gpu/m1")
	h.candidates("global", "", "reviewer", "gpu/m2")
}

// AC-PJ1: only the running project gets jobs; with two running projects on a
// one-slot backend, six dispatches alternate A, B, A, B, ...
func TestPJ1_OnlyRunningProjectsAndRoundRobin(t *testing.T) {
	h := newDispHarness(t)
	h.standard(1)
	h.dispProject("alpha", "any", indep(3))
	h.dispProject("beta", "any", indep(3))

	h.startRun("alpha")
	h.pass()
	open := h.openStages()
	if len(open) != 1 || h.projectOf(open[0]) != "alpha" {
		t.Fatalf("stopped beta must get nothing, alpha one stage: %+v", open)
	}
	h.finish(open[0])

	h.startRun("beta")
	got := []string{"alpha"}
	for i := 0; i < 5; i++ {
		h.pass()
		open := h.openStages()
		if len(open) != 1 {
			t.Fatalf("pass %d: %d open stages, want 1 (slot width 1)", i, len(open))
		}
		got = append(got, h.projectOf(open[0]))
		h.finish(open[0])
	}
	want := "alpha,beta,alpha,beta,alpha,beta"
	// The first dispatch above was alpha alone; the cursor then favours beta.
	if strings.Join(got, ",") != want {
		t.Fatalf("order = %v, want %s", got, want)
	}
}

// Free slots are the lane width minus in-flight stages from the DB: a pass on
// a 2-slot backend places two stages and no more.
func TestTM4_SlotsFromDBBoundDispatch(t *testing.T) {
	h := newDispHarness(t)
	h.standard(2)
	h.dispProject("alpha", "any", indep(5))
	h.startRun("alpha")
	h.pass()
	h.pass()
	if n := len(h.openStages()); n != 2 {
		t.Fatalf("open stages = %d, want 2", n)
	}
	h.finish(h.openStages()[0])
	h.pass()
	if n := len(h.openStages()); n != 2 {
		t.Fatalf("after one finished, open stages = %d, want 2", n)
	}
}

// AC-MB2: with no paid model listed, 50 dispatches never use a paid backend
// or model; listing one explicitly is what makes it usable (MB5).
func TestMB2_NoPaidModelUnlessListed(t *testing.T) {
	h := newDispHarness(t)
	h.addBackend("gpu", 5, false, false, "m1", "m2")
	h.addBackend("cloud", 5, false, true, "big")
	h.candidates("global", "", "worker", "gpu/m1")
	h.candidates("global", "", "reviewer", "gpu/m2")
	h.dispProject("alpha", "any", indep(50))
	h.startRun("alpha")
	seen := 0
	for seen < 50 {
		h.pass()
		open := h.openStages()
		if len(open) == 0 {
			t.Fatalf("stalled after %d dispatches", seen)
		}
		for _, st := range open {
			if st.Backend == "cloud" || st.Model == "big" {
				t.Fatalf("paid model dispatched: %+v", st)
			}
			h.finish(st)
			seen++
		}
	}

	// Explicitly listed, the paid model is used.
	h.candidates("global", "", "worker", "cloud/big")
	h.dispProject("beta", "any", indep(1))
	h.startRun("beta")
	h.pass()
	open := h.openStages()
	if len(open) != 1 || open[0].Backend != "cloud" {
		t.Fatalf("listed paid model not used: %+v", open)
	}
}

// AC-MB3: over 50 attempts the reviewer chosen is never the worker's model; a
// one-model pool gives a waiting reason and dispatches nothing.
func TestMB3_ReviewerNeverEqualsWorker(t *testing.T) {
	h := newDispHarness(t)
	h.addBackend("gpu", 5, false, false, "m1", "m2")
	h.candidates("global", "", "worker", "gpu/m1", "gpu/m2")
	h.candidates("global", "", "reviewer", "gpu/m1", "gpu/m2")
	f := h.dispProject("alpha", "any", indep(50))
	h.startRun("alpha")
	ctx := context.Background()
	seen := 0
	for seen < 50 {
		h.pass()
		for _, st := range h.openStages() {
			a, _ := h.st.GetAttempt(ctx, st.Attempt)
			tk, _ := h.st.GetTask(ctx, a.Task)
			h.finish(st) // the work stage is over by review time, freeing its slot
			pk, why, err := h.eng.pickBackend(ctx, "reviewer", tk, store.Project{Slug: "alpha", ModelScope: "any"}, a.WorkerModel)
			if err != nil || why != "" {
				t.Fatalf("reviewer pick: %v %q", err, why)
			}
			if pk.Model == a.WorkerModel {
				t.Fatalf("reviewer %s equals worker %s", pk.Model, a.WorkerModel)
			}
			seen++
		}
	}

	g := newDispHarness(t)
	g.addBackend("gpu", 5, false, false, "m1")
	g.candidates("global", "", "worker", "gpu/m1")
	g.candidates("global", "", "reviewer", "gpu/m1")
	f = g.dispProject("solo", "any", indep(1))
	g.startRun("solo")
	g.pass()
	if n := len(g.openStages()); n != 0 {
		t.Fatalf("one-model pool dispatched %d stages", n)
	}
	tk, _ := g.st.GetTask(ctx, f.tasks[0].ID)
	if !strings.Contains(tk.WaitingReason, "role reviewer") {
		t.Fatalf("waiting reason = %q", tk.WaitingReason)
	}
}

// Reviewer is sticky per task (AT3) while it stays eligible.
func TestAT3_ReviewerSticky(t *testing.T) {
	h := newDispHarness(t)
	h.addBackend("gpu", 5, false, false, "m1", "m2", "m3")
	h.candidates("global", "", "worker", "gpu/m1")
	h.candidates("global", "", "reviewer", "gpu/m2", "gpu/m3")
	f := h.dispProject("alpha", "any", indep(1))
	ctx := context.Background()
	att, err := h.st.StartAttempt(ctx, f.tasks[0].ID, model.AttemptNormal, "wt", "m1")
	if err != nil {
		t.Fatal(err)
	}
	if err := h.st.FinishAttempt(ctx, att.ID, model.OutcomeFailed, "fp", "m3"); err != nil {
		t.Fatal(err)
	}
	pk, why, err := h.eng.pickBackend(ctx, "reviewer", f.tasks[0], store.Project{Slug: "alpha", ModelScope: "any"}, "m1")
	if err != nil || why != "" || pk.Model != "m3" {
		t.Fatalf("pick = %+v %q %v, want sticky m3", pk, why, err)
	}
}

// AC-PP1: a local project whose only free backend is hosted never gets a
// stage and shows the reason.
func TestPP1_LocalScopeSkipsHosted(t *testing.T) {
	h := newDispHarness(t)
	h.addBackend("hosted", 5, true, false, "m1", "m2")
	h.candidates("global", "", "worker", "hosted/m1")
	h.candidates("global", "", "reviewer", "hosted/m2")
	f := h.dispProject("loc", "local", indep(1))
	h.startRun("loc")
	h.pass()
	if n := len(h.openStages()); n != 0 {
		t.Fatalf("local project got %d stages on a hosted backend", n)
	}
	tk, _ := h.st.GetTask(context.Background(), f.tasks[0].ID)
	if !strings.Contains(tk.WaitingReason, "no eligible backend for role worker") || !strings.Contains(tk.WaitingReason, "hosted under local scope") {
		t.Fatalf("waiting reason = %q", tk.WaitingReason)
	}

	// Reviewer hosted only: the worker is not dispatched either.
	h.addBackend("local1", 5, false, false, "w1")
	h.candidates("global", "", "worker", "local1/w1")
	h.pass()
	if n := len(h.openStages()); n != 0 {
		t.Fatalf("dispatched without an eligible reviewer: %d", n)
	}
	tk, _ = h.st.GetTask(context.Background(), f.tasks[0].ID)
	if !strings.Contains(tk.WaitingReason, "no eligible backend for role reviewer") || !strings.Contains(tk.WaitingReason, "hosted under local scope") {
		t.Fatalf("waiting reason = %q", tk.WaitingReason)
	}
}

// SV3 / AC-SV1 engine half: no backends and empty candidate lists neither
// panic nor fail; the task waits with a reason, and the reason clears once a
// stage is dispatched.
func TestSV3_EmptyCandidatesAndBackendsWait(t *testing.T) {
	h := newDispHarness(t)
	f := h.dispProject("alpha", "any", indep(1))
	h.startRun("alpha")
	h.pass()
	tk, _ := h.st.GetTask(context.Background(), f.tasks[0].ID)
	if !strings.Contains(tk.WaitingReason, "no candidates configured") {
		t.Fatalf("waiting reason = %q", tk.WaitingReason)
	}
	if n := len(h.openStages()); n != 0 {
		t.Fatal("dispatched without candidates")
	}
	if _, why, err := h.eng.pickBackend(context.Background(), "reviewer", tk, store.Project{Slug: "alpha"}, ""); err != nil || why == "" {
		t.Fatalf("empty reviewer list: %q %v", why, err)
	}

	h.standard(1)
	h.pass()
	tk, _ = h.st.GetTask(context.Background(), f.tasks[0].ID)
	if tk.WaitingReason != "" || len(h.openStages()) != 1 {
		t.Fatalf("reason %q, open %d", tk.WaitingReason, len(h.openStages()))
	}
}

// PX4: with two slots, a task is never co-scheduled with one it has a
// dependency path to or from.
func TestPX4_NoCoScheduleAlongDependencyPath(t *testing.T) {
	h := newDispHarness(t)
	h.standard(2)
	f := h.dispProject("px", "any", []model.TaskDef{
		{Key: "T1", Phase: "P", Title: "t", Prompt: "p", Acceptance: []string{"a"}},
		{Key: "T2", Phase: "P", Title: "t", Prompt: "p", Acceptance: []string{"a"}, DependsOn: []string{"T1"}},
		{Key: "T3", Phase: "P", Title: "t", Prompt: "p", Acceptance: []string{"a"}},
	})
	h.startRun("px")
	h.pass()
	h.pass()
	byTask := map[int64]bool{}
	for _, st := range h.openStages() {
		a, _ := h.st.GetAttempt(context.Background(), st.Attempt)
		byTask[a.Task] = true
	}
	if len(byTask) != 2 || !byTask[f.tasks[0].ID] || !byTask[f.tasks[2].ID] || byTask[f.tasks[1].ID] {
		t.Fatalf("scheduled tasks = %v, want T1 and T3 only", byTask)
	}

	tasks := []model.Task{
		{TaskDef: model.TaskDef{Key: "A"}},
		{TaskDef: model.TaskDef{Key: "B", DependsOn: []string{"A"}}},
		{TaskDef: model.TaskDef{Key: "C", DependsOn: []string{"B"}}},
		{TaskDef: model.TaskDef{Key: "D"}},
	}
	if !coScheduleConflict(tasks, tasks[2], tasks[:1]) || !coScheduleConflict(tasks, tasks[0], tasks[2:3]) {
		t.Fatal("transitive path in either direction must conflict")
	}
	if coScheduleConflict(tasks, tasks[3], tasks[:3]) {
		t.Fatal("independent task must not conflict")
	}
}

// The first dispatch moves an approved effort to running.
func TestDispatchMarksEffortRunning(t *testing.T) {
	h := newDispHarness(t)
	h.standard(1)
	f := h.dispProject("alpha", "any", indep(1))
	ctx := context.Background()
	st := model.EffortQueueAppr
	if _, err := h.st.UpdateEffort(ctx, "t", f.effort, store.EffortPatch{Status: &st}); err != nil {
		t.Fatal(err)
	}
	h.startRun("alpha")
	h.pass()
	ef, _ := h.st.GetEffort(ctx, f.effort)
	if ef.Status != model.EffortRunning {
		t.Fatalf("effort status = %s", ef.Status)
	}
}

// TM4: the single goroutine submits on a kick and on the safety tick, and the
// caller's own Deps.Kick still fires.
func TestTM4_GoroutineWakesOnKickAndTick(t *testing.T) {
	tick := make(chan time.Time)
	kicks := make(chan struct{}, 8)
	h := newHarness(t)
	h.widthZ = true
	h.tweak = func(d *Deps) {
		d.DispatchTick = tick
		d.Kick = func() { kicks <- struct{}{} }
	}
	h.start()
	h.standard(1)
	h.dispProject("alpha", "any", indep(3))
	h.startEngine()
	h.startRun("alpha") // kicks the dispatcher
	waitOpen(t, h, 1)
	select {
	case <-kicks:
	default:
		t.Fatal("caller Deps.Kick not invoked")
	}
	h.finish(h.openStages()[0])
	tick <- time.Now() // safety tick: the slot is free again
	waitOpen(t, h, 1)
	if n := len(h.attemptsOfProject("alpha")); n != 2 {
		t.Fatalf("attempts = %d, want 2", n)
	}
}

func (h *harness) attemptsOfProject(slug string) []model.Attempt {
	h.t.Helper()
	ef, err := h.st.ActiveEffort(context.Background(), slug)
	if err != nil {
		h.t.Fatal(err)
	}
	tasks, _ := h.st.ListTasks(context.Background(), ef.ID)
	var out []model.Attempt
	for _, tk := range tasks {
		as, _ := h.st.ListAttempts(context.Background(), tk.ID)
		out = append(out, as...)
	}
	return out
}

func waitOpen(t *testing.T, h *harness, n int) {
	t.Helper()
	for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
		if len(h.openStages()) == n {
			return
		}
	}
	t.Fatalf("open stages = %d, want %d", len(h.openStages()), n)
}

// AC-PX5: on a backend with 2 slots and model X limited to 1, two X stages
// never run at once, while an X and a Y stage do.
func TestPX5_ModelConcurrencyLimit(t *testing.T) {
	setup := func(t *testing.T, workers ...string) *harness {
		h := newDispHarness(t)
		b := store.Backend{Name: "gpu", Host: "http://127.0.0.1:11434", APIType: "ollama", Slots: 2,
			Models: []store.BackendModel{{Model: "X", MaxConcurrent: 1}, {Model: "Y"}, {Model: "R"}}}
		if err := h.eng.AddBackend(context.Background(), "t", b); err != nil {
			t.Fatal(err)
		}
		h.candidates("global", "", "worker", workers...)
		h.candidates("global", "", "reviewer", "gpu/R")
		h.dispProject("alpha", "any", indep(4))
		h.startRun("alpha")
		return h
	}
	models := func(h *harness) string {
		var ms []string
		for _, st := range h.openStages() {
			ms = append(ms, st.Model)
		}
		sort.Strings(ms)
		return strings.Join(ms, ",")
	}

	t.Run("only X: second X waits though a slot is free", func(t *testing.T) {
		h := setup(t, "gpu/X")
		h.pass()
		h.pass()
		if got := models(h); got != "X" {
			t.Fatalf("open models = %q, want X only", got)
		}
		h.finish(h.openStages()[0])
		h.pass()
		if got := models(h); got != "X" {
			t.Fatalf("after the first X finished = %q, want one X again", got)
		}
	})
	t.Run("X then Y: an X and a Y run together", func(t *testing.T) {
		h := setup(t, "gpu/X", "gpu/Y")
		for i := 0; i < 4; i++ {
			h.pass()
		}
		if got := models(h); got != "X,Y" {
			t.Fatalf("open models = %q, want X,Y", got)
		}
	})
}

// H2: a reviewer from another model family is preferred over a first-listed one
// of the worker's family; with only same-family reviewers the other model of the
// family is used, and the worker's own model never is.
func TestH2_ReviewerPrefersAnotherFamily(t *testing.T) {
	ctx := context.Background()
	pick := func(reviewers ...string) (string, string) {
		h := newDispHarness(t)
		h.addBackend("gpu", 5, false, false, "qwen3", "qwen3-coder", "mistral-small")
		h.candidates("global", "", "worker", "gpu/qwen3")
		h.candidates("global", "", "reviewer", reviewers...)
		f := h.dispProject("alpha", "any", indep(1))
		tk, _ := h.st.GetTask(ctx, f.tasks[0].ID)
		pk, why, err := h.eng.pickBackend(ctx, "reviewer", tk, store.Project{Slug: "alpha", ModelScope: "any"}, "qwen3")
		if err != nil {
			t.Fatal(err)
		}
		return pk.Model, why
	}
	if m, why := pick("gpu/qwen3-coder", "gpu/mistral-small"); m != "mistral-small" || why != "" {
		t.Fatalf("with a mistral reviewer listed second, picked %q (%q)", m, why)
	}
	if m, why := pick("gpu/qwen3", "gpu/qwen3-coder"); m != "qwen3-coder" || why != "" {
		t.Fatalf("with only qwens, picked %q (%q)", m, why)
	}
	if m, why := pick("gpu/qwen3"); m != "" || why == "" {
		t.Fatalf("the worker's own model was picked: %q (%q)", m, why)
	}
}

// tiered adds a backend whose models carry the given tiers (model -> tier).
func (h *harness) tiered(name string, slots int, tiers map[string]int) {
	h.t.Helper()
	// hosted, so that tier 1 means something: a local model never counts above tier 2 (H29)
	b := store.Backend{Name: name, Host: "http://127.0.0.1:11434", APIType: "ollama", Slots: slots, Hosted: true}
	for m, tier := range tiers {
		b.Models = append(b.Models, store.BackendModel{Model: m, Tier: tier})
	}
	if err := h.eng.AddBackend(context.Background(), "t", b); err != nil {
		h.t.Fatal(err)
	}
}

// H3: a model rated light (tier 3) is skipped for planning roles with the
// reason stated, but still serves as a worker; unrated models are unaffected.
func TestH3_LightModelsAreSkippedForPlanningRoles(t *testing.T) {
	ctx := context.Background()
	h := newDispHarness(t)
	h.tiered("gpu", 5, map[string]int{"big": 1, "small": 3, "plain": 0})
	for _, role := range []string{"planner", "architect", "critic", "compiler"} {
		h.candidates("global", "", role, "gpu/small")
		_, _, why, err := h.eng.pickEffortStage(ctx, store.Project{Slug: "x", ModelScope: "any"}, role, nil, "")
		if err != nil || !strings.Contains(why, "tier 3") {
			t.Fatalf("%s: err %v, reason %q, want the light model skipped", role, err, why)
		}
		h.candidates("global", "", role, "gpu/small", "gpu/plain")
		pk, _, why, err := h.eng.pickEffortStage(ctx, store.Project{Slug: "x", ModelScope: "any"}, role, nil, "")
		if err != nil || why != "" || pk.Model != "plain" {
			t.Fatalf("%s: picked %+v (%q, %v), want the unrated model", role, pk, why, err)
		}
	}
	h.candidates("global", "", "worker", "gpu/small")
	h.candidates("global", "", "reviewer", "gpu/big")
	f := h.dispProject("alpha", "any", indep(1))
	h.startRun("alpha")
	h.pass()
	if open := h.openStages(); len(open) != 1 || open[0].Model != "small" {
		t.Fatalf("a light model must still work as a worker: %+v (%v)", open, f)
	}
}

// H3: a reviewer is not weaker than the worker whose work it judges when the
// pool has one; H28: when it has none, a weaker one reviews rather than the
// task waiting for ever.
func TestH3_ReviewerIsNotWeakerThanTheWorker(t *testing.T) {
	ctx := context.Background()
	h := newDispHarness(t)
	h.tiered("gpu", 5, map[string]int{"strong": 1, "mid": 2, "weak": 3, "unrated": 0})
	h.candidates("global", "", "worker", "gpu/strong")
	h.candidates("global", "", "reviewer", "gpu/weak", "gpu/unrated", "gpu/mid")
	f := h.dispProject("alpha", "any", indep(1))
	tk, _ := h.st.GetTask(ctx, f.tasks[0].ID)
	pk, why, err := h.eng.pickBackend(ctx, "reviewer", tk, store.Project{Slug: "alpha", ModelScope: "any"}, "strong")
	if err != nil || why != "" || pk.Model == "" || pk.Model == "strong" {
		t.Fatalf("a strong worker with only weaker reviewers got %+v (%q, %v), want a weaker one to review (H28)", pk, why, err)
	}
	pk, why, err = h.eng.pickBackend(ctx, "reviewer", tk, store.Project{Slug: "alpha", ModelScope: "any"}, "weak")
	if err != nil || why != "" || pk.Model != "unrated" {
		t.Fatalf("a weak worker got reviewer %+v (%q, %v), want the first reviewer that is not weaker", pk, why, err)
	}
}

// failedAttempt records one finished, failed attempt of task by model with the
// given fingerprint, and a worker stage with stageOutcome on backend gpu.
func (h *harness) failedAttempt(task int64, mdl, fp, stageOutcome string) {
	h.t.Helper()
	ctx := context.Background()
	a, err := h.st.StartAttempt(ctx, task, model.AttemptNormal, "wt", mdl)
	if err != nil {
		h.t.Fatal(err)
	}
	st, err := h.st.CreateStage(ctx, store.StageSpec{Attempt: a.ID, Role: "worker", Backend: "gpu", Model: mdl})
	if err != nil {
		h.t.Fatal(err)
	}
	if err := h.st.FinishStage(ctx, st.ID, stageOutcome, "", nil); err != nil {
		h.t.Fatal(err)
	}
	if err := h.st.FinishAttempt(ctx, a.ID, model.OutcomeFailed, fp, ""); err != nil {
		h.t.Fatal(err)
	}
}

func (h *harness) workerModelDispatched() string {
	h.t.Helper()
	h.pass()
	for _, st := range h.openStages() {
		if st.Role == "worker" {
			return st.Model
		}
	}
	return ""
}

// H7: a model that fails a task the same way twice in a row is passed over
// for the next attempt; with nobody else to try, the repeat is allowed so the
// task can reach stuck.
func TestH7_RepeatFailureMovesToAnotherModel(t *testing.T) {
	setup := func(workers ...string) (*harness, runFix) {
		h := newDispHarness(t)
		// These rules predate AF-13's default of 2; they are about the model choice in between.
		if err := h.st.UpdateSettings(context.Background(), "t", map[string]string{"stuck_threshold": "3"}); err != nil {
			t.Fatal(err)
		}
		h.addBackend("gpu", 5, false, false, "m1", "m2", "m3")
		h.candidates("global", "", "worker", workers...)
		h.candidates("global", "", "reviewer", "gpu/m3")
		f := h.dispProject("alpha", "any", indep(1))
		h.startRun("alpha")
		return h, f
	}
	h, f := setup("gpu/m1", "gpu/m2")
	h.failedAttempt(f.tasks[0].ID, "m1", "v:x", "pass")
	if got := h.workerModelDispatched(); got != "m1" {
		t.Fatalf("after one failure the worker was %q, want m1 again", got)
	}
	h, f = setup("gpu/m1", "gpu/m2")
	h.failedAttempt(f.tasks[0].ID, "m1", "v:x", "pass")
	h.failedAttempt(f.tasks[0].ID, "m1", "v:x", "pass")
	if got := h.workerModelDispatched(); got != "m2" {
		t.Fatalf("after the same failure twice the worker was %q, want m2", got)
	}
	h, f = setup("gpu/m1", "gpu/m2")
	h.failedAttempt(f.tasks[0].ID, "m1", "v:x", "pass")
	h.failedAttempt(f.tasks[0].ID, "m1", "v:y", "pass")
	if got := h.workerModelDispatched(); got != "m1" {
		t.Fatalf("two different failures moved the worker to %q", got)
	}
	h, f = setup("gpu/m1")
	h.failedAttempt(f.tasks[0].ID, "m1", "v:x", "pass")
	h.failedAttempt(f.tasks[0].ID, "m1", "v:x", "pass")
	if got := h.workerModelDispatched(); got != "m1" {
		t.Fatalf("with only m1 listed the worker was %q, want the repeat", got)
	}
}

// H7: three strikes bench a model in that role, wherever it runs, until the
// owner clears them; the task waits with the reason when nobody else is listed.
func TestH7_ThreeStrikesBenchAModel(t *testing.T) {
	h := newDispHarness(t)
	// These rules predate AF-13's default of 2; they are about the model choice in between.
	if err := h.st.UpdateSettings(context.Background(), "t", map[string]string{"stuck_threshold": "3"}); err != nil {
		t.Fatal(err)
	}
	h.addBackend("gpu", 5, false, false, "m1", "m2", "m3")
	h.candidates("global", "", "worker", "gpu/m1", "gpu/m2")
	h.candidates("global", "", "reviewer", "gpu/m3")
	f := h.dispProject("alpha", "any", indep(1))
	h.startRun("alpha")
	for i := 1; i <= 2; i++ {
		h.failedAttempt(f.tasks[0].ID, "m1", fmt.Sprintf("u%d", i), "infra:unparseable")
	}
	if got := h.workerModelDispatched(); got != "m1" {
		t.Fatalf("two strikes already benched m1: got %q", got)
	}
	h2 := newDispHarness(t)
	h2.addBackend("gpu", 5, false, false, "m1", "m2", "m3")
	h2.candidates("global", "", "worker", "gpu/m1", "gpu/m2")
	h2.candidates("global", "", "reviewer", "gpu/m3")
	f2 := h2.dispProject("alpha", "any", indep(1))
	h2.startRun("alpha")
	for i := 1; i <= 3; i++ {
		h2.failedAttempt(f2.tasks[0].ID, "m1", fmt.Sprintf("u%d", i), "infra:unparseable")
	}
	if got := h2.workerModelDispatched(); got != "m2" {
		t.Fatalf("three strikes: worker %q, want m2", got)
	}
	// With nobody else listed, the task waits and says why; clearing frees m1.
	h3 := newDispHarness(t)
	h3.addBackend("gpu", 5, false, false, "m1", "m3")
	h3.candidates("global", "", "worker", "gpu/m1")
	h3.candidates("global", "", "reviewer", "gpu/m3")
	f3 := h3.dispProject("alpha", "any", indep(1))
	h3.startRun("alpha")
	for i := 1; i <= 3; i++ {
		h3.failedAttempt(f3.tasks[0].ID, "m1", fmt.Sprintf("u%d", i), "infra:unparseable")
	}
	if got := h3.workerModelDispatched(); got != "" {
		t.Fatalf("a benched model was dispatched: %q", got)
	}
	tk, _ := h3.st.GetTask(context.Background(), f3.tasks[0].ID)
	if !strings.Contains(tk.WaitingReason, "benched") {
		t.Fatalf("waiting reason %q does not say the model is benched", tk.WaitingReason)
	}
	time.Sleep(3 * time.Millisecond)
	if err := h3.st.ClearStrikes(context.Background(), "owner", store.StrikeKey{Backend: "gpu", Model: "m1", Role: "worker"}); err != nil {
		t.Fatal(err)
	}
	if got := h3.workerModelDispatched(); got != "m1" {
		t.Fatalf("after clearing the worker was %q, want m1", got)
	}
}

// H8: a backend that stopped answering is suspended, not retried on a timer:
// nothing is dispatched to it, it is probed, and it comes back as soon as it
// answers.
func TestH8_ASuspendedBackendReturnsWhenItAnswers(t *testing.T) {
	ctx := context.Background()
	h := newDispHarness(t)
	h.addBackend("gpu", 5, false, false, "m1", "m3")
	h.candidates("global", "", "worker", "gpu/m1")
	h.candidates("global", "", "reviewer", "gpu/m3")
	f := h.dispProject("alpha", "any", indep(1))
	h.startRun("alpha")
	h.eng.health.suspend("gpu", time.Now())
	if _, ok := h.eng.SuspendedBackends()["gpu"]; !ok {
		t.Fatal("not suspended")
	}
	if got := h.workerModelDispatched(); got != "" {
		t.Fatalf("a suspended backend was given work: %q", got)
	}
	tk, _ := h.st.GetTask(ctx, f.tasks[0].ID)
	if !strings.Contains(tk.WaitingReason, "down") {
		t.Fatalf("waiting reason %q does not say the backend is down", tk.WaitingReason)
	}
	answers := false
	h.eng.deps.Reachable = func(context.Context, string) bool { return answers }
	h.eng.probeSuspended(ctx)
	if _, ok := h.eng.SuspendedBackends()["gpu"]; !ok {
		t.Fatal("a silent backend came back")
	}
	answers = true
	h.eng.probeSuspended(ctx)
	if len(h.eng.SuspendedBackends()) != 0 {
		t.Fatal("an answering backend stayed suspended")
	}
	if got := h.workerModelDispatched(); got != "m1" {
		t.Fatalf("after it answered the worker was %q, want m1", got)
	}
}

// H10: a model its host says has no tool support is never dispatched, whatever
// the lists say, and the task shows why when it was the only choice.
func TestH10_ModelWithoutToolsIsSkipped(t *testing.T) {
	ctx := context.Background()
	mark := func(h *harness, mdl string) {
		b, err := h.st.GetBackend(ctx, "gpu")
		if err != nil {
			t.Fatal(err)
		}
		for i := range b.Models {
			if b.Models[i].Model == mdl {
				b.Models[i].Tools = "no"
			}
		}
		if err := h.eng.UpdateBackend(ctx, "t", b); err != nil {
			t.Fatal(err)
		}
	}
	h := newDispHarness(t)
	h.addBackend("gpu", 5, false, false, "m1", "m2", "m3")
	h.candidates("global", "", "worker", "gpu/m1", "gpu/m2")
	h.candidates("global", "", "reviewer", "gpu/m3")
	h.dispProject("alpha", "any", indep(1))
	h.startRun("alpha")
	mark(h, "m1")
	if got := h.workerModelDispatched(); got != "m2" {
		t.Fatalf("worker = %q, want m2 (m1 has no tool support)", got)
	}
	h2 := newDispHarness(t)
	h2.addBackend("gpu", 5, false, false, "m1", "m3")
	h2.candidates("global", "", "worker", "gpu/m1")
	h2.candidates("global", "", "reviewer", "gpu/m3")
	f := h2.dispProject("alpha", "any", indep(1))
	h2.startRun("alpha")
	mark(h2, "m1")
	if got := h2.workerModelDispatched(); got != "" {
		t.Fatalf("a model without tool support was dispatched: %q", got)
	}
	tk, _ := h2.st.GetTask(ctx, f.tasks[0].ID)
	if !strings.Contains(tk.WaitingReason, "tool calls") {
		t.Fatalf("waiting reason %q does not say why", tk.WaitingReason)
	}
}

// H11: after two failed attempts in a row the next worker must be a stronger
// tier than the one that failed; with no stronger model listed, the same tier
// goes on trying.
func TestH11_RepeatedFailureEscalatesTheTier(t *testing.T) {
	setup := func(workers ...string) (*harness, runFix) {
		h := newDispHarness(t)
		// These rules predate AF-13's default of 2; they are about the model choice in between.
		if err := h.st.UpdateSettings(context.Background(), "t", map[string]string{"stuck_threshold": "3"}); err != nil {
			t.Fatal(err)
		}
		h.tiered("gpu", 5, map[string]int{"lo": 2, "hi": 1, "rev": 1})
		h.candidates("global", "", "worker", workers...)
		h.candidates("global", "", "reviewer", "gpu/rev")
		f := h.dispProject("alpha", "any", indep(1))
		h.startRun("alpha")
		return h, f
	}
	h, f := setup("gpu/lo", "gpu/hi")
	h.failedAttempt(f.tasks[0].ID, "lo", "v:a", "pass")
	if got := h.workerModelDispatched(); got != "lo" {
		t.Fatalf("after one failure the worker was %q, want lo", got)
	}
	h, f = setup("gpu/lo", "gpu/hi")
	h.failedAttempt(f.tasks[0].ID, "lo", "v:a", "pass")
	h.failedAttempt(f.tasks[0].ID, "lo", "v:b", "pass")
	if got := h.workerModelDispatched(); got != "hi" {
		t.Fatalf("after two failures the worker was %q, want the tier-1 hi", got)
	}
	h, f = setup("gpu/lo")
	h.failedAttempt(f.tasks[0].ID, "lo", "v:a", "pass")
	h.failedAttempt(f.tasks[0].ID, "lo", "v:b", "pass")
	if got := h.workerModelDispatched(); got != "lo" {
		t.Fatalf("with no stronger model listed the worker was %q, want lo", got)
	}
}

// H11: the files a task must not touch are those of the tasks it is not built
// on: its own dependencies (direct and indirect) are already in its tree.
func TestH11_OwnedByOthersLeavesOutTheTasksItBuildsOn(t *testing.T) {
	h := newDispHarness(t)
	def := func(key string, files []string, deps ...string) model.TaskDef {
		return model.TaskDef{Key: key, Phase: "P1", Title: "t", Prompt: "do " + strings.Join(files, " "), Acceptance: []string{"ok"}, FilesTouched: files, DependsOn: deps}
	}
	f := h.dispProject("alpha", "any", []model.TaskDef{
		def("T1", []string{"a.go"}), def("T2", []string{"b.go"}, "T1"), def("T3", []string{"c.go"}, "T2"), def("T4", []string{"d.go"}),
	})
	byKey := map[string]model.Task{}
	for _, tk := range f.tasks {
		byKey[tk.Key] = tk
	}
	got, err := h.eng.ownedByOthers(context.Background(), &chain{eff: model.Effort{ID: f.effort}, task: byKey["T3"]}, model.Limits{MaxAttempts: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "d.go" {
		t.Fatalf("T3 is built on T2 and T1, so only d.go is another task's: %v", got)
	}
}

// A task that has passed and merged is no longer "to write" anything: a later
// task that finds a defect in its file may fix it. A file an unfinished task
// still owns stays out of reach.
func TestOwnedByOthersLeavesOutFinishedTasks(t *testing.T) {
	h := newDispHarness(t)
	def := func(key string, files []string, deps ...string) model.TaskDef {
		return model.TaskDef{Key: key, Phase: "P1", Title: "t", Prompt: "do " + strings.Join(files, " "), Acceptance: []string{"ok"}, FilesTouched: files, DependsOn: deps}
	}
	f := h.dispProject("alpha", "any", []model.TaskDef{
		def("T1", []string{"main.go"}), def("T2", []string{"x.go"}), def("T3", []string{"main_test.go"}), def("T4", []string{"y.go"}),
	})
	byKey := map[string]model.Task{}
	for _, tk := range f.tasks {
		byKey[tk.Key] = tk
	}
	ctx := context.Background()
	a, err := h.st.StartAttempt(ctx, byKey["T1"].ID, model.AttemptNormal, "wt", "m")
	if err != nil {
		t.Fatal(err)
	}
	if err := h.st.FinishAttempt(ctx, a.ID, model.OutcomePassed, "", "r"); err != nil {
		t.Fatal(err)
	}
	got, err := h.eng.ownedByOthers(ctx, &chain{eff: model.Effort{ID: f.effort}, task: byKey["T3"]}, model.Limits{MaxAttempts: 5})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, ",") != "x.go,y.go" {
		t.Fatalf("T1 has passed, so main.go is no longer another task's: %v", got)
	}
}

// AF-13 (fs3-peace/9, A1-2): 23 tasks listed src/rules/peace_test.go, so any task that edited it
// failed scope while another was unfinished. A test file is shared and never another task's.
func TestOwnedByOthersLeavesOutSharedTestFiles(t *testing.T) {
	h := newDispHarness(t)
	def := func(key string, files ...string) model.TaskDef {
		return model.TaskDef{Key: key, Phase: "P1", Title: "t", Prompt: "do " + strings.Join(files, " "), Acceptance: []string{"ok"}, FilesTouched: files}
	}
	f := h.dispProject("alpha", "any", []model.TaskDef{
		def("T1", "src/rules/truce.go", "src/rules/peace_test.go"),
		def("T2", "src/rules/border.go", "src/rules/peace_test.go"),
		def("T3", "src/rules/war.go"),
	})
	byKey := map[string]model.Task{}
	for _, tk := range f.tasks {
		byKey[tk.Key] = tk
	}
	got, err := h.eng.ownedByOthers(context.Background(), &chain{eff: model.Effort{ID: f.effort}, task: byKey["T3"]}, model.Limits{MaxAttempts: 5})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, ",") != "src/rules/truce.go,src/rules/border.go" {
		t.Fatalf("the shared test file must not be another task's: %v", got)
	}
}
