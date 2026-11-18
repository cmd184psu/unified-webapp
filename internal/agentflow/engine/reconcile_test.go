package engine

import (
	"context"
	"testing"
	"time"

	"cmd184psu/unified-webapp/internal/agentflow/model"
	"cmd184psu/unified-webapp/internal/agentflow/runner"
	"cmd184psu/unified-webapp/internal/agentflow/store"
)

var testLimits = model.Limits{MaxAttempts: 5, StuckThreshold: 3, MaxInfraRetries: 5}

// startEngine reconciles and resumes the harness engine; the periodic
// goroutine stops when the test ends.
func (h *harness) startEngine() {
	h.t.Helper()
	if err := h.eng.Start(context.Background()); err != nil {
		h.t.Fatal(err)
	}
	h.t.Cleanup(func() { h.eng.Close() })
}

func (h *harness) taskStatus(f fixture) model.Status {
	h.t.Helper()
	m, err := h.st.TaskStatuses(context.Background(), f.payload.Effort, testLimits)
	if err != nil {
		h.t.Fatal(err)
	}
	for _, s := range m {
		return s
	}
	h.t.Fatal("no tasks")
	return ""
}

// countingAttempts are the attempts that count toward the limit.
func countingAttempts(as []model.Attempt) int {
	n := 0
	for _, a := range as {
		if a.Outcome != model.OutcomeAbandoned {
			n++
		}
	}
	return n
}

func (h *harness) agentflowJobs() int {
	h.t.Helper()
	idx, err := h.eng.indexJobs()
	if err != nil {
		h.t.Fatal(err)
	}
	n := 0
	for _, refs := range idx {
		n += len(refs)
	}
	return n
}

// AC-DBS2: the process dies during a stage (modelled by closing the agentflow
// DB so the outcome cannot land, then closing taskmaster, whose own orphan
// reconcile fails the execution); after reopening both, the stage is
// infra:lost, the attempt does not count and the task derives ready.
func TestDBS2_CrashDuringStageIsLostAfterRestart(t *testing.T) {
	h := newStageHarness(t)
	h.fake.Script("worker", runner.Step{Block: true})
	h.startEngine()
	f := h.attemptStage("alpha", "worker")
	jobID := h.submitFixture(f)
	waitFor(t, "stage running", func() bool {
		st, _ := h.st.GetStage(context.Background(), f.stage.ID)
		return st.State == model.StageRunning
	})
	if got := h.taskStatus(f); got != model.StatusRunning {
		t.Fatalf("status while running = %s", got)
	}

	h.eng.Close()
	h.st.Close() // the outcome write can no longer land: the process is gone
	h.st = nil
	h.closeTM()
	st, err := store.Open(h.afPath)
	if err != nil {
		t.Fatal(err)
	}
	h.st = st
	if got, _ := h.st.GetStage(context.Background(), f.stage.ID); got.State != model.StageRunning || got.Outcome != "" {
		t.Fatalf("stage after the crash = %+v, want still running with no outcome", got)
	}
	h.openTM()
	h.start()
	if j, err := h.lane("agentflow-alpha").Get(jobID); err != nil || j.Status != jobFailed {
		t.Fatalf("job after restart = %+v, %v; want failed (taskmaster orphan reconcile)", j, err)
	}
	h.startEngine()

	got, err := h.st.GetStage(context.Background(), f.stage.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != model.StageDone || got.Outcome != OutcomeInfraLost {
		t.Fatalf("stage = %s/%q, want done/%s", got.State, got.Outcome, OutcomeInfraLost)
	}
	as := h.attemptsOf(f)
	if countingAttempts(as) != 0 || len(as) != 1 || as[0].Outcome != model.OutcomeAbandoned {
		t.Fatalf("attempts = %+v, want the interrupted one abandoned and nothing counted", as)
	}
	if s := h.taskStatus(f); s != model.StatusReady {
		t.Fatalf("task status = %s, want ready", s)
	}
}

// RC4 and DBS5: lanes stay paused until Start has reconciled; a queued job
// left by the previous process is removed and its stage closed as lost.
func TestRC4_StartRemovesStaleQueuedJobsBeforeResume(t *testing.T) {
	h := newHarness(t)
	h.widthZ = true
	h.start()
	h.mustAdd("alpha", 1)
	f := h.attemptStage("alpha", "worker")
	h.submitFixture(f)
	if n := h.agentflowJobs(); n != 1 {
		t.Fatalf("jobs before restart = %d", n)
	}

	h.restart()
	if n := h.agentflowJobs(); n != 1 {
		t.Fatalf("job should survive a taskmaster restart as queued, have %d", n)
	}
	if !h.settings("agentflow-alpha").Paused {
		t.Fatal("lane not paused before Start")
	}
	h.startEngine()
	if n := h.agentflowJobs(); n != 0 {
		t.Fatalf("stale jobs after Start = %d, want 0", n)
	}
	if s := h.settings("agentflow-alpha"); s.Paused {
		t.Fatalf("lane still paused after Start: %+v", s)
	}
	if got, _ := h.st.GetStage(context.Background(), f.stage.ID); got.Outcome != OutcomeInfraLost {
		t.Fatalf("stage outcome = %q, want %s", got.Outcome, OutcomeInfraLost)
	}
	if s := h.taskStatus(f); s != model.StatusReady {
		t.Fatalf("task status = %s, want ready", s)
	}
}

// DBS5: a periodic tick closes a stage whose job was cancelled or removed out
// from under it, and leaves a live one alone.
func TestDBS5_PeriodicTickClosesVanishedJobs(t *testing.T) {
	ticks := make(chan time.Time)
	h := newHarness(t)
	h.widthZ = true
	h.tweak = func(d *Deps) { d.Tick = ticks }
	h.start()
	h.mustAdd("alpha", 1)
	h.startEngine()
	live, removed, canceled := h.attemptStage("alpha", "worker"), h.attemptStage("alpha", "worker"), h.attemptStage("alpha", "worker")
	for _, f := range []fixture{live, removed, canceled} {
		h.submitFixture(f)
	}

	// The tick loop takes the next tick only after finishing the previous one,
	// so the second send proves the first pass is complete.
	ticks <- time.Now()
	ticks <- time.Now()
	for _, f := range []fixture{live, removed, canceled} {
		if st, _ := h.st.GetStage(context.Background(), f.stage.ID); st.State != model.StageQueued {
			t.Fatalf("stage %d closed while its job is alive: %+v", f.stage.ID, st)
		}
	}

	idx, err := h.eng.indexJobs()
	if err != nil {
		t.Fatal(err)
	}
	if err := idx[removed.stage.ID][0].lane.Remove(idx[removed.stage.ID][0].job.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := idx[canceled.stage.ID][0].lane.Cancel(idx[canceled.stage.ID][0].job.ID); err != nil {
		t.Fatal(err)
	}
	ticks <- time.Now()
	ticks <- time.Now()
	for _, f := range []fixture{removed, canceled} {
		st := h.waitDone(f.stage.ID)
		if st.Outcome != OutcomeInfraLost {
			t.Fatalf("stage %d outcome = %q, want %s", f.stage.ID, st.Outcome, OutcomeInfraLost)
		}
		if s := h.taskStatus(f); s != model.StatusReady {
			t.Fatalf("task status = %s, want ready", s)
		}
	}
	if st, _ := h.st.GetStage(context.Background(), live.stage.ID); st.State != model.StageQueued {
		t.Fatalf("live stage was touched: %+v", st)
	}
}

// Submit and Reconcile do not race: a stage created and submitted while
// reconcile passes run is never closed as lost.
func TestSubmitIsAtomicAgainstReconcile(t *testing.T) {
	h := newHarness(t)
	h.widthZ = true
	h.start()
	h.mustAdd("alpha", 1)
	h.startEngine()
	f := h.attemptStage("alpha", "worker")
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			default:
				_, _ = h.eng.Reconcile(context.Background())
			}
		}
	}()
	ctx := context.Background()
	for i := 0; i < 20; i++ {
		sg, _, err := h.eng.Submit(ctx, BackendLane("alpha"), store.StageSpec{Attempt: f.attempt.ID, Role: "worker", Backend: "alpha", Model: "m1", QueueHash: f.hash}, f.payload)
		if err != nil {
			t.Fatal(err)
		}
		if got, _ := h.st.GetStage(ctx, sg.ID); got.State != model.StageQueued {
			t.Fatalf("stage %d = %+v, closed while its job was queued", sg.ID, got)
		}
	}
	close(stop)
	<-done
}
