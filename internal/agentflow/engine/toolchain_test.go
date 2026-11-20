package engine

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"cmd184psu/unified-webapp/internal/agentflow/model"
	"cmd184psu/unified-webapp/internal/agentflow/store"
)

func (h *harness) addTypedBackend(name, apiType string, slots int, models ...string) {
	h.t.Helper()
	b := store.Backend{Name: name, Host: "http://127.0.0.1:11434", APIType: apiType, Slots: slots}
	for _, m := range models {
		b.Models = append(b.Models, store.BackendModel{Model: m})
	}
	if err := h.eng.AddBackend(context.Background(), "t", b); err != nil {
		h.t.Fatal(err)
	}
}

// AF-6b: a .NET task's verify stage gets exactly loopback and (when the
// project has not set it) verify_in_copy, and its worker stage records that it
// ran unconfined.
func TestAF6bDotnetRelaxations(t *testing.T) {
	cs := model.Task{TaskDef: model.TaskDef{Toolchains: []string{"dotnet"}}}
	if got := verifyRelaxations(cs, store.Project{}); !reflect.DeepEqual(got, []string{"loopback", "verify_in_copy"}) {
		t.Fatalf("verify relaxations = %v", got)
	}
	if got := verifyRelaxations(cs, store.Project{VerifyInCopy: true}); !reflect.DeepEqual(got, []string{"loopback"}) {
		t.Fatalf("verify relaxations with project verify_in_copy = %v", got)
	}
	if got := workerRelaxations(cs); !reflect.DeepEqual(got, []string{"unconfined_runner"}) {
		t.Fatalf("worker relaxations = %v", got)
	}
	plain := model.Task{TaskDef: model.TaskDef{FilesTouched: []string{"main.go"}}}
	if len(verifyRelaxations(plain, store.Project{})) != 0 || len(workerRelaxations(plain)) != 0 {
		t.Fatal("a non-dotnet task relaxes nothing")
	}
}

// AF-6b: the engine infers dotnet from the task's own files, skips the
// sandboxed opencode candidate for it, and lets a non-.NET task use it.
func TestAF6bDotnetTaskSkipsSandboxedWorker(t *testing.T) {
	h := newDispHarness(t)
	h.addTypedBackend("sandbox", "opencode", 5, "s1")
	h.addBackend("gpu", 5, false, false, "g1", "g2")
	h.candidates("global", "", "worker", "sandbox/s1", "gpu/g1")
	h.candidates("global", "", "reviewer", "gpu/g2")
	f := h.dispProject("alpha", "any", []model.TaskDef{{
		Key: "T1", Phase: "P1", Title: "t", Prompt: "do", Acceptance: []string{"ok"},
		FilesTouched: []string{"src/App.cs"},
	}})
	h.startRun("alpha")
	h.pass()
	var worker *model.Stage
	for _, st := range h.openStages() {
		if st.Role == "worker" {
			s := st
			worker = &s
		}
	}
	if worker == nil || worker.Backend != "gpu" || worker.Model != "g1" {
		t.Fatalf("worker = %+v, want the unconfined gpu backend (%v)", worker, f.hash)
	}
	if !reflect.DeepEqual(worker.Relaxations, []string{"unconfined_runner"}) {
		t.Fatalf("worker relaxations = %v", worker.Relaxations)
	}
}

func TestAF6bNonDotnetTaskKeepsSandboxedWorker(t *testing.T) {
	h := newDispHarness(t)
	h.addTypedBackend("sandbox", "opencode", 5, "s1")
	h.addBackend("gpu", 5, false, false, "g2")
	h.candidates("global", "", "worker", "sandbox/s1")
	h.candidates("global", "", "reviewer", "gpu/g2")
	h.dispProject("alpha", "any", []model.TaskDef{{
		Key: "T1", Phase: "P1", Title: "t", Prompt: "do", Acceptance: []string{"ok"},
		FilesTouched: []string{"main.go"},
	}})
	h.startRun("alpha")
	h.pass()
	open := h.openStages()
	if len(open) != 1 || open[0].Backend != "sandbox" || len(open[0].Relaxations) != 0 {
		t.Fatalf("open stages = %+v, want the sandboxed worker with no relaxation", open)
	}
}

// With no unconfined candidate the task waits, and the reason says why.
func TestAF6bNoUnconfinedWorkerWaits(t *testing.T) {
	ctx := context.Background()
	h := newDispHarness(t)
	h.addTypedBackend("sandbox", "opencode", 5, "s1")
	h.addBackend("gpu", 5, false, false, "g2")
	h.candidates("global", "", "worker", "sandbox/s1")
	h.candidates("global", "", "reviewer", "gpu/g2")
	f := h.dispProject("alpha", "any", []model.TaskDef{{
		Key: "T1", Phase: "P1", Title: "t", Prompt: "do", Acceptance: []string{"ok"},
		FilesTouched: []string{"App.csproj"},
	}})
	h.startRun("alpha")
	h.pass()
	if open := h.openStages(); len(open) != 0 {
		t.Fatalf("stages were submitted: %+v", open)
	}
	tk, err := h.st.GetTask(ctx, f.tasks[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(tk.WaitingReason, "sandbox") || !strings.Contains(tk.WaitingReason, "unconfined") {
		t.Fatalf("waiting reason = %q", tk.WaitingReason)
	}
}
