package api

import (
	"context"
	"net/http"
	"testing"
	"time"

	"cmd184psu/unified-webapp/internal/agentflow/store"
)

// H25: a service restart waits for the running stages to end and only then ends
// the process; cancelling it leaves the process alone and a second request is
// refused while one is waiting.
func TestH25_RestartWaitsForOpenStagesThenEndsTheProcess(t *testing.T) {
	restartPoll = 20 * time.Millisecond
	t.Cleanup(func() { restartPoll = 2 * time.Second })
	e := newEnv(t, envOpt{})
	e.addProject("p1")
	ef := decode[EffortView](t, e.ok("POST", "/api/projects/p1/efforts", map[string]any{"title": "x"}, http.StatusCreated))
	st, err := e.st.CreateStage(context.Background(), store.StageSpec{Effort: ef.ID, Role: "planner", Backend: "b", Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	v := decode[RestartView](t, e.ok("POST", "/api/service/restart", nil, http.StatusAccepted))
	if v.State != "draining" || v.OpenStages != 1 {
		t.Fatalf("view = %+v", v)
	}
	e.ok("POST", "/api/service/restart", nil, http.StatusConflict)
	time.Sleep(200 * time.Millisecond)
	if n := e.terminated.Load(); n != 0 {
		t.Fatalf("the process was ended %d times while a stage was open", n)
	}
	// cancelling lets the work go on; nothing ends the process
	e.ok("DELETE", "/api/service/restart", nil, http.StatusOK)
	if v := decode[RestartView](t, e.ok("GET", "/api/service/restart", nil, http.StatusOK)); v.State != "idle" {
		t.Fatalf("after cancelling: %+v", v)
	}
	// a new request, then the stage ends: the process is ended once
	e.ok("POST", "/api/service/restart", nil, http.StatusAccepted)
	if err := e.st.FinishStage(context.Background(), st.ID, "pass", "", nil); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(5 * time.Second); e.terminated.Load() == 0; time.Sleep(20 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the restart never ended the process after the stage ended")
		}
	}
	if n := e.terminated.Load(); n != 1 {
		t.Fatalf("terminated %d times", n)
	}
}

// H25: the projects a restart paused are started again by the next process,
// once: the request leaves a service.restart event and the resume answers it
// with service.restarted, after which another boot does nothing.
func TestH25_ResumeAfterRestartAnswersTheRequestOnce(t *testing.T) {
	e := newEnv(t, envOpt{})
	ctx := context.Background()
	last := func() string {
		evs, err := e.st.ListEvents(ctx, "service", "restart", 100)
		if err != nil || len(evs) == 0 {
			return ""
		}
		return evs[len(evs)-1].Action
	}
	e.api.ResumeAfterRestart(ctx)
	if got := last(); got != "" {
		t.Fatalf("a boot with no restart request wrote %q", got)
	}
	// a project that cannot start (no such project) is skipped, not fatal
	if err := e.api.noteService(ctx, "service.restart", []string{"gone"}); err != nil {
		t.Fatal(err)
	}
	e.api.ResumeAfterRestart(ctx)
	if got := last(); got != "service.restarted" {
		t.Fatalf("last event = %q, want service.restarted", got)
	}
	e.api.ResumeAfterRestart(ctx)
	if evs, _ := e.st.ListEvents(ctx, "service", "restart", 100); len(evs) != 2 {
		t.Fatalf("a second boot wrote more events: %d", len(evs))
	}
}
