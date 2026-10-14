package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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
