package engine

import (
	"context"
	"errors"
	"strings"
	"testing"

	"cmd184psu/unified-webapp/internal/agentflow/model"
	"cmd184psu/unified-webapp/internal/agentflow/runner"
	"cmd184psu/unified-webapp/internal/agentflow/store"
)

func TestWindowResolvesServedThenFloor(t *testing.T) {
	cases := []struct {
		name   string
		m      store.BackendModel
		want   int
		source string
	}{
		{"nothing known uses the floor", store.BackendModel{}, 16384, "floor"},
		{"the model max does not count as the served window", store.BackendModel{ContextTokens: 393216}, 16384, "floor"},
		{"a model that supports less than the floor", store.BackendModel{ContextTokens: 8192}, 8192, "model max"},
		{"a learned window", store.BackendModel{WindowTokens: 32768, WindowSource: "observed", ContextTokens: 393216}, 32768, "observed"},
		{"the owner's window", store.BackendModel{WindowTokens: 65536, WindowSource: "set"}, 65536, "set"},
	}
	for _, c := range cases {
		got, src := Window(c.m, 16384)
		if got != c.want || src != c.source {
			t.Errorf("%s: window = %d (%s), want %d (%s)", c.name, got, src, c.want, c.source)
		}
	}
}

// The measured case: a task that writes a Go server, a page and a test was cut
// off at 16K in its repair attempts, and a source file with its test fits.
func TestTaskNeedMatchesWhatWasMeasured(t *testing.T) {
	three := model.TaskDef{Key: "T001", Prompt: strings.Repeat("x", 1200), Acceptance: []string{"R1: a", "R2: b"},
		FilesTouched: []string{"main.go", "calculator.html", "main_test.go"}}
	two := model.TaskDef{Key: "T002", Prompt: strings.Repeat("x", 1200), Acceptance: []string{"R1: a"},
		FilesTouched: []string{"calc.go", "calc_test.go"}}
	if p := QueueContextProblems([]model.TaskDef{two}, []int{16384}); len(p) != 0 {
		t.Errorf("a source file with its test should fit 16K: %v", p)
	}
	p := QueueContextProblems([]model.TaskDef{three, two}, []int{16384})
	if len(p) != 1 || !strings.Contains(p[0], "T001") || !strings.Contains(p[0], "split") {
		t.Fatalf("problems = %v, want one naming T001 and asking for a split", p)
	}
	if p := QueueContextProblems([]model.TaskDef{three}, []int{65536}); len(p) != 0 {
		t.Errorf("a larger window takes the three-file task: %v", p)
	}
	if p := QueueContextProblems([]model.TaskDef{three}, nil); len(p) != 0 {
		t.Errorf("with no known window nothing is flagged: %v", p)
	}
}

// H15: a compiled task that writes more than the largest worker window can
// hold is sent back to the compiler with the numbers, and the second queue is
// taken. The windows here are the floor, because no window is configured.
func TestH15_QueueWithATaskTooBigForEveryWindowIsRetried(t *testing.T) {
	f := newPlanFix(t, false)
	f.standardPool()
	f.candidates("global", "", "worker", "gpu/m1")
	f.frdNaming("/api/hello")
	f.approvedPlan()
	big := strings.Replace(compileWithPath, `"files_touched":["api.go"]`, `"files_touched":["api.go","api_test.go","page.html","style.css"]`, 1)
	f.fake.Script("compiler", runner.Step{Reply: big}, runner.Step{Reply: compileWithPath})
	f.startEngine()
	if err := f.eng.Compile(context.Background(), "alice", f.effort); err != nil {
		t.Fatal(err)
	}
	f.waitStatus(model.EffortQueueDraft)
	calls := f.fake.CallsFor("compiler")
	if len(calls) != 2 {
		t.Fatalf("compiler calls = %d, want the compile and one retry", len(calls))
	}
	if !strings.Contains(calls[1].Prompt, "Fix these") || !strings.Contains(calls[1].Prompt, "task T001 needs about") || !strings.Contains(calls[1].Prompt, "split it") {
		t.Fatalf("retry prompt does not ask for a split:\n%s", calls[1].Prompt)
	}
}

// H15: a window the owner set, or a larger one, takes the same queue as is.
func TestH15_ALargerWindowTakesTheSameQueue(t *testing.T) {
	f := newPlanFix(t, false)
	f.standardPool()
	f.candidates("global", "", "worker", "gpu/m1")
	f.frdNaming("/api/hello")
	f.approvedPlan()
	f.setWindows(65536)
	big := strings.Replace(compileWithPath, `"files_touched":["api.go"]`, `"files_touched":["api.go","api_test.go","page.html","style.css"]`, 1)
	f.fake.Script("compiler", runner.Step{Reply: big})
	f.startEngine()
	if err := f.eng.Compile(context.Background(), "alice", f.effort); err != nil {
		t.Fatal(err)
	}
	f.waitStatus(model.EffortQueueDraft)
	if n := len(f.fake.CallsFor("compiler")); n != 1 {
		t.Fatalf("compiler calls = %d, want 1", n)
	}
}

// A cut-off run teaches the engine the window the host served; the owner's own
// value is never replaced, and a learned one only moves down.
func TestH15_CutOffRunLearnsTheWindow(t *testing.T) {
	h := newHarness(t)
	h.start()
	ctx := context.Background()
	cut := func(tokens int64) error {
		return &runner.Error{Class: runner.ErrContext, Tokens: tokens, Msg: "full"}
	}
	h.mustAdd("gpu", 1) // one model, m1
	st := model.Stage{Backend: "gpu", Model: "m1"}
	window := func() (int, string) {
		b, err := h.st.GetBackend(ctx, "gpu")
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range b.Models {
			if m.Model == "m1" {
				return m.WindowTokens, m.WindowSource
			}
		}
		return -1, ""
	}
	h.eng.learnWindow(ctx, st, cut(16384))
	if w, src := window(); w != 16384 || src != "observed" {
		t.Fatalf("window = %d (%s), want 16384 observed", w, src)
	}
	h.eng.learnWindow(ctx, st, cut(32768))
	if w, _ := window(); w != 16384 {
		t.Fatalf("a larger cut-off raised the window to %d", w)
	}
	h.eng.learnWindow(ctx, st, cut(8192))
	if w, _ := window(); w != 8192 {
		t.Fatalf("a smaller cut-off left the window at %d", w)
	}
	if err := h.st.SetModelWindow(ctx, "gpu", "m1", 40000, "set"); err != nil {
		t.Fatal(err)
	}
	h.eng.learnWindow(ctx, st, cut(4096))
	if w, src := window(); w != 40000 || src != "set" {
		t.Fatalf("the owner's window became %d (%s)", w, src)
	}
}

// setWindows gives every model of the backend "gpu" an owner-set window.
func (f *planFix) setWindows(tokens int) {
	f.t.Helper()
	ctx := context.Background()
	b, err := f.st.GetBackend(ctx, "gpu")
	if err != nil {
		f.t.Fatal(err)
	}
	for _, m := range b.Models {
		if err := f.st.SetModelWindow(ctx, "gpu", m.Model, tokens, "set"); err != nil {
			f.t.Fatal(err)
		}
	}
}

// H15: a worker whose context window is smaller than the task needs is passed
// over for one that can hold it; when none can, the task is not left waiting.
func TestH15_DispatchPassesOverAWindowTooSmall(t *testing.T) {
	setup := func(small, big int) *harness {
		h := newDispHarness(t)
		h.tiered("gpu", 5, map[string]int{"small": 1, "big": 1, "rev": 1})
		ctx := context.Background()
		if err := h.st.SetModelWindow(ctx, "gpu", "small", small, "set"); err != nil {
			t.Fatal(err)
		}
		if err := h.st.SetModelWindow(ctx, "gpu", "big", big, "set"); err != nil {
			t.Fatal(err)
		}
		h.candidates("global", "", "worker", "gpu/small", "gpu/big")
		h.candidates("global", "", "reviewer", "gpu/rev")
		h.dispProject("alpha", "any", indep(1))
		h.startRun("alpha")
		return h
	}
	if got := setup(6000, 65536).workerModelDispatched(); got != "big" {
		t.Fatalf("the worker was %q, want big: small's 6000-token window cannot hold the task", got)
	}
	if got := setup(65536, 65536).workerModelDispatched(); got != "small" {
		t.Fatalf("the worker was %q, want small, first in the list and large enough", got)
	}
	if got := setup(3000, 4000).workerModelDispatched(); got != "small" {
		t.Fatalf("the worker was %q: with no window big enough the task runs in list order, not waiting", got)
	}
}

// A retry after a failure never goes back to the model that just failed while
// another is eligible. Before this, a retry avoided the backend, and a backend
// serving five models (opencode's free ones) had nothing else to try, so the
// same model crashed five times and planning halted.
func TestRetryGoesToAnotherModelOnTheSameBackend(t *testing.T) {
	f := newPlanFix(t, false)
	f.addBackend("free", 2, false, false, "m1", "m2", "m3")
	f.candidates("global", "", "planner", "free/m1", "free/m2", "free/m3")
	f.candidates("global", "", "architect", "free/m2", "free/m3")
	f.candidates("global", "", "critic", "free/m3")
	f.candidates("global", "", "compiler", "free/m1", "free/m2", "free/m3")
	f.approvedPlan()
	f.fake.Script("compiler",
		runner.Step{ErrClass: runner.ErrCrash, Err: errors.New("Unexpected server error")},
		runner.Step{Reply: compileClean})
	f.startEngine()
	if err := f.eng.Compile(context.Background(), "alice", f.effort); err != nil {
		t.Fatal(err)
	}
	f.waitStatus(model.EffortQueueDraft)
	calls := f.fake.CallsFor("compiler")
	if len(calls) != 2 {
		t.Fatalf("compiler calls = %d, want the crash and one retry", len(calls))
	}
	if a, b := calls[0].Request.Model, calls[1].Request.Model; a == b {
		t.Fatalf("the retry used the model that crashed (%s)", a)
	}
}

// A queue that fails the requirement check is recompiled by another model, and
// a third try is allowed.
func TestQueueProblemRetryUsesAnotherModelAndAThirdTry(t *testing.T) {
	f := newPlanFix(t, false)
	f.addBackend("free", 2, false, false, "m1", "m2", "m3")
	f.candidates("global", "", "planner", "free/m1")
	f.candidates("global", "", "architect", "free/m2")
	f.candidates("global", "", "critic", "free/m3")
	f.candidates("global", "", "compiler", "free/m1", "free/m2", "free/m3")
	f.candidates("global", "", "worker", "free/m1")
	f.frdNaming("/api/hello")
	f.approvedPlan()
	f.fake.Script("compiler",
		runner.Step{Reply: compileClean}, runner.Step{Reply: compileClean}, runner.Step{Reply: compileWithPath})
	f.startEngine()
	if err := f.eng.Compile(context.Background(), "alice", f.effort); err != nil {
		t.Fatal(err)
	}
	f.waitStatus(model.EffortQueueDraft)
	calls := f.fake.CallsFor("compiler")
	if len(calls) != 3 {
		t.Fatalf("compiler calls = %d, want 3", len(calls))
	}
	if calls[0].Request.Model == calls[1].Request.Model || calls[1].Request.Model == calls[2].Request.Model {
		t.Fatalf("a retry reused the previous model: %s, %s, %s", calls[0].Request.Model, calls[1].Request.Model, calls[2].Request.Model)
	}
}
