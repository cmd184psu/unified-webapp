package api

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"cmd184psu/unified-webapp/internal/agentflow/model"
	"cmd184psu/unified-webapp/internal/agentflow/store"
)

// approvedProject creates a project with one approved task, so it can be run;
// it returns the effort's id.
func (e *env) approvedProject(slug string) int64 {
	e.t.Helper()
	ctx := context.Background()
	e.addProject(slug)
	ef := decode[EffortView](e.t, e.ok("POST", "/api/projects/"+slug+"/efforts", map[string]any{"title": "x"}, http.StatusCreated))
	defs := []model.TaskDef{{Key: "T1", Phase: "P1", Title: "t", Prompt: "do", Acceptance: []string{"ok"}}}
	q, err := e.st.SaveQueue(ctx, "t", ef.ID, defs)
	if err != nil {
		e.t.Fatal(err)
	}
	if _, err := e.st.ApproveQueue(ctx, "t", ef.ID, q.Hash); err != nil {
		e.t.Fatal(err)
	}
	return ef.ID
}

// AF-10: a hold pauses the running projects, a run/start is refused with its
// reason while it is set, and lifting it starts again exactly those projects.
func TestAF10_HoldPausesAndLiftsExactlyThePausedProjects(t *testing.T) {
	e := newEnv(t, envOpt{})
	ctx := context.Background()
	for _, slug := range []string{"p1", "p2", "p3"} {
		e.approvedProject(slug)
	}
	e.ok("POST", "/api/projects/p1/run/start", nil, http.StatusOK)
	e.ok("POST", "/api/projects/p2/run/start", nil, http.StatusOK)

	h := decode[HoldView](t, e.ok("POST", "/api/service/hold", map[string]any{"reason": "trying something"}, http.StatusOK))
	if !h.Active || h.Reason != "trying something" || strings.Join(h.Paused, ",") != "p1,p2" {
		t.Fatalf("hold = %+v", h)
	}
	for _, slug := range []string{"p1", "p2"} {
		if st := e.eng.Runs().State(slug); st != model.RunPaused {
			t.Fatalf("%s is %s while held, want paused", slug, st)
		}
	}
	// A run/start names the hold's reason and conflicts.
	code, body := e.call(nil, "POST", "/api/projects/p3/run/start", nil)
	if code != http.StatusConflict || !strings.Contains(string(body), "trying something") {
		t.Fatalf("run/start while held = %d %s", code, body)
	}
	// A second hold keeps the projects it paused and updates the reason.
	if again := decode[HoldView](t, e.ok("POST", "/api/service/hold", map[string]any{"reason": "still trying"}, http.StatusOK)); strings.Join(again.Paused, ",") != "p1,p2" {
		t.Fatalf("placing the hold again = %+v", again)
	}
	if got := decode[HoldView](t, e.ok("GET", "/api/service/hold", nil, http.StatusOK)); !got.Active || got.OpenStages != 0 {
		t.Fatalf("GET hold = %+v", got)
	}
	// One item in the attention list, not one per project.
	items, err := e.eng.Attention(ctx)
	if err != nil {
		t.Fatal(err)
	}
	held := 0
	for _, it := range items {
		if it.Kind == "service_hold" {
			held++
			if !strings.Contains(it.Message, "still trying") || it.Project != "" {
				t.Fatalf("hold item = %+v", it)
			}
		}
	}
	if held != 1 {
		t.Fatalf("attention has %d hold items: %+v", held, items)
	}
	// Lifting starts p1 and p2 again and leaves p3 alone.
	if v := decode[HoldView](t, e.ok("DELETE", "/api/service/hold", nil, http.StatusOK)); v.Active || v.Reason != "" {
		t.Fatalf("after lifting: %+v", v)
	}
	for _, slug := range []string{"p1", "p2"} {
		if st := e.eng.Runs().State(slug); st != model.RunRunning {
			t.Fatalf("%s is %s after lifting the hold, want running", slug, st)
		}
	}
	if st := e.eng.Runs().State("p3"); st != model.RunStopped {
		t.Fatalf("p3 is %s after lifting the hold, want stopped", st)
	}
	// a request without a reason is refused
	e.field("POST", "/api/service/hold", map[string]any{}, "reason")
}

// AF-10: a restart that runs out of time becomes a hold with the reason and the
// projects it paused; lifting the hold abandons that restart.
func TestAF10_DrainTimeoutBecomesAHold(t *testing.T) {
	restartMaxWait = 40 * time.Millisecond
	t.Cleanup(func() { restartMaxWait = 60 * time.Minute })
	e := newEnv(t, envOpt{})
	eff := e.approvedProject("p1")
	e.ok("POST", "/api/projects/p1/run/start", nil, http.StatusOK)
	if _, err := e.st.CreateStage(context.Background(), store.StageSpec{Effort: eff, Role: "planner", Backend: "b", Model: "m"}); err != nil {
		t.Fatal(err)
	}
	e.ok("POST", "/api/service/restart", nil, http.StatusAccepted)

	var h HoldView
	for deadline := time.Now().Add(5 * time.Second); !h.Active; time.Sleep(20 * time.Millisecond) {
		h = decode[HoldView](t, e.ok("GET", "/api/service/hold", nil, http.StatusOK))
		if time.Now().After(deadline) {
			t.Fatal("the drain restart never became a hold")
		}
	}
	if h.Reason != holdRestartReason || !h.Restart || strings.Join(h.Paused, ",") != "p1" {
		t.Fatalf("hold after the deadline = %+v", h)
	}
	if v := decode[RestartView](t, e.ok("GET", "/api/service/restart", nil, http.StatusOK)); v.State != "held" || v.Plan == nil || !v.Plan.Held {
		t.Fatalf("restart while held = %+v", v)
	}
	// The stage is still open, so the project is pausing rather than paused;
	// either way it is not running.
	if st := e.eng.Runs().State("p1"); st != model.RunPaused && st != model.RunPausing {
		t.Fatalf("p1 is %s while the restart waits in a hold, want paused or pausing", st)
	}
	// A second restart request is refused while one waits.
	e.ok("POST", "/api/service/restart", nil, http.StatusConflict)
	// Lifting the hold resumes p1 and abandons the restart.
	e.ok("DELETE", "/api/service/hold", nil, http.StatusOK)
	if v := decode[RestartView](t, e.ok("GET", "/api/service/restart", nil, http.StatusOK)); v.State != "idle" {
		t.Fatalf("after lifting: %+v", v)
	}
	if st := e.eng.Runs().State("p1"); st != model.RunRunning {
		t.Fatalf("p1 is %s after lifting, want running", st)
	}
	if n := e.terminated.Load(); n != 0 {
		t.Fatalf("the process was ended %d times by a lifted hold", n)
	}
}

// AF-10: when the last stage closes under a held restart, the restart finishes
// and the hold is gone, so the next process starts the paused projects and is
// not held itself.
func TestAF10_HeldRestartFinishesWhenTheLastStageEnds(t *testing.T) {
	restartMaxWait = 40 * time.Millisecond
	t.Cleanup(func() { restartMaxWait = 60 * time.Minute })
	e := newEnv(t, envOpt{})
	ctx := context.Background()
	eff := e.approvedProject("p1")
	e.ok("POST", "/api/projects/p1/run/start", nil, http.StatusOK)
	st, err := e.st.CreateStage(ctx, store.StageSpec{Effort: eff, Role: "planner", Backend: "b", Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	e.ok("POST", "/api/service/restart", nil, http.StatusAccepted)
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		if h := decode[HoldView](t, e.ok("GET", "/api/service/hold", nil, http.StatusOK)); h.Active {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the drain restart never became a hold")
		}
	}
	if err := e.st.FinishStage(ctx, st.ID, "pass", "", nil); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(5 * time.Second); e.terminated.Load() == 0; time.Sleep(20 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the restart never ended the process after the last stage closed")
		}
	}
	if h := decode[HoldView](t, e.ok("GET", "/api/service/hold", nil, http.StatusOK)); h.Active {
		t.Fatalf("the hold outlived the restart it was waiting for: %+v", h)
	}
	// The next boot starts the paused project from the resume event.
	e.api.ResumeAfterRestart(ctx)
	if st := e.eng.Runs().State("p1"); st != model.RunRunning {
		t.Fatalf("p1 is %s after the boot, want running", st)
	}
}

// AF-10: a restart asked for with wait=hold never gives up and so never becomes
// a hold.
func TestAF10_WaitHoldRestartNeverTimesOut(t *testing.T) {
	restartMaxWait = 30 * time.Millisecond
	t.Cleanup(func() { restartMaxWait = 60 * time.Minute })
	e := newEnv(t, envOpt{})
	ctx := context.Background()
	eff := e.approvedProject("p1")
	if _, err := e.st.CreateStage(ctx, store.StageSpec{Effort: eff, Role: "planner", Backend: "b", Model: "m"}); err != nil {
		t.Fatal(err)
	}
	e.ok("POST", "/api/service/restart", map[string]any{"wait": "hold"}, http.StatusAccepted)
	time.Sleep(150 * time.Millisecond)
	if v := decode[RestartView](t, e.ok("GET", "/api/service/restart", nil, http.StatusOK)); v.State != "draining" || v.Plan == nil || v.Plan.Wait != "hold" {
		t.Fatalf("a wait=hold restart after the deadline = %+v", v)
	}
	if h := decode[HoldView](t, e.ok("GET", "/api/service/hold", nil, http.StatusOK)); h.Active {
		t.Fatalf("a wait=hold restart became a hold: %+v", h)
	}
	e.ok("POST", "/api/service/restart", map[string]any{"wait": "later"}, http.StatusBadRequest)
	e.ok("DELETE", "/api/service/restart", nil, http.StatusOK)
}
