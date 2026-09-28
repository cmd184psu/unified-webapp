package worker

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"cmd184psu/unified-webapp/internal/platform/broker"
	"cmd184psu/unified-webapp/internal/taskmaster/db"
	"cmd184psu/unified-webapp/internal/taskmaster/golane"
	"cmd184psu/unified-webapp/internal/taskmaster/models"

	"github.com/stretchr/testify/require"
)

// subscribeBoardEvents mounts board's payload SSE stream on a test server
// (mirroring coordinator/board_test.go's pattern, since Broker.Publish's
// payload channel has no exported direct-subscribe API outside an HTTP
// handler) and returns a channel of "data:" line bodies plus a closer.
func subscribeBoardEvents(t *testing.T, board *broker.Broker) (events chan string, closeSub func()) {
	t.Helper()
	srv := httptest.NewServer(board.ServeSSE("board", nil))
	resp, err := http.Get(srv.URL)
	require.NoError(t, err)

	events = make(chan string, 16)
	go func() {
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "data:") {
				events <- strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			}
		}
	}()
	return events, func() {
		resp.Body.Close()
		srv.Close()
	}
}

// internalEchoPayload mirrors func_test.go's echoPayload; kept separate
// since this file is package worker (internal), not worker_test.
type internalEchoPayload struct {
	Msg  string `json:"msg"`
	Fail bool   `json:"fail"`
}

// newInternalFuncTestDB opens an in-memory DB with one owned lane, mirroring
// the plan's §7.1 internal-test setup (a real func execution row, direct
// runTask call — no poll loop, so the tests are deterministic).
func newInternalFuncTestDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { d.Close() })
	require.NoError(t, d.UpsertLane(&models.Lane{Name: "owned", Width: 2, Owner: "acme-module"}))
	return d
}

// submitInternalEcho submits a func task of kind "acme-module.echo" and
// returns (task with ID populated, execID). Mirrors what poll() would set up
// before spawning runTask: the row is loaded fresh via GetTask so Kind/
// Payload/PayloadVersion are populated.
func submitInternalEcho(t *testing.T, d *db.DB, name string, payload internalEchoPayload, kind string, version int) (*models.Task, int64) {
	t.Helper()
	raw, err := json.Marshal(payload)
	require.NoError(t, err)
	_, execID, err := d.SubmitFuncTask(db.FuncTaskSpec{Name: name, Lane: "owned", Kind: kind, Label: "test", Payload: raw, PayloadVersion: version})
	require.NoError(t, err)
	task, err := d.GetTask(name)
	require.NoError(t, err)
	return task, execID
}

// countMetrics returns the number of task_metrics rows recorded for execID.
func countMetrics(t *testing.T, d *db.DB, taskName string) int {
	t.Helper()
	summaries, err := d.GetMetrics("", taskName, 24*365)
	require.NoError(t, err)
	n := 0
	for _, s := range summaries {
		n += s.SuccessCount + s.FailedCount + s.CanceledCount
	}
	return n
}

func lockHeld(t *testing.T, d *db.DB, taskID int64) bool {
	t.Helper()
	ok, err := d.AcquireLock(taskID, "probe", time.Minute)
	require.NoError(t, err)
	if ok {
		require.NoError(t, d.ReleaseLock(taskID, "probe"))
	}
	return !ok
}

func newInternalFuncWorker(d *db.DB, funcs *FuncRegistry, cancels *CancelRegistry, brake *BrakeGate, board *broker.Broker, hidden *HiddenLanes) *Worker {
	w := New(d, NewRegistry(), "w", NewSudoGate(false), cancels, brake, NewProcessRegistry(), board)
	w.EnableFuncTasks(FuncOptions{Funcs: funcs, Progress: NewProgressRegistry(), Hidden: hidden, ProgressInterval: 20 * time.Millisecond})
	return w
}

func setHook(t *testing.T, hook *func(int64), fn func(int64)) {
	t.Helper()
	*hook = fn
	t.Cleanup(func() { *hook = nil })
}

func TestFuncTask_PanicRecordedFailed(t *testing.T) {
	d := newInternalFuncTestDB(t)
	funcs := NewFuncRegistry()
	require.NoError(t, funcs.Register(golane.NewKind[internalEchoPayload]("acme-module.echo", 1, nil, func(ctx context.Context, rc golane.RunContext, p internalEchoPayload) (any, error) {
		panic("boom")
	})))
	w := newInternalFuncWorker(d, funcs, NewCancelRegistry(), NewBrakeGate(false), nil, NewHiddenLanes(nil))
	w.runCtx = context.Background()

	task, execID := submitInternalEcho(t, d, "owned-panic00001", internalEchoPayload{}, "acme-module.echo", 1)
	_, err := d.AcquireLock(task.ID, "w", time.Minute)
	require.NoError(t, err)

	w.wg.Add(1)
	w.runTask(task, execID, time.Now())

	e, err := d.GetExecution(execID)
	require.NoError(t, err)
	require.Equal(t, "failed", e.Status)
	require.NotNil(t, e.ErrorMessage)
	require.Contains(t, *e.ErrorMessage, "panic: boom")
}

func TestFuncTask_BadPayloadVersionFailsCleanly(t *testing.T) {
	d := newInternalFuncTestDB(t)
	funcs := NewFuncRegistry()
	require.NoError(t, funcs.Register(golane.NewKind[internalEchoPayload]("acme-module.echo", 1, nil, func(ctx context.Context, rc golane.RunContext, p internalEchoPayload) (any, error) {
		return nil, nil
	})))
	w := newInternalFuncWorker(d, funcs, NewCancelRegistry(), NewBrakeGate(false), nil, NewHiddenLanes(nil))
	w.runCtx = context.Background()

	// PayloadVersion 2 while the kind only supports 1 — simulates a row
	// written by an older/newer binary, or hand-edited (Q2).
	task, execID := submitInternalEcho(t, d, "owned-badver00001", internalEchoPayload{Msg: "x"}, "acme-module.echo", 2)
	_, err := d.AcquireLock(task.ID, "w", time.Minute)
	require.NoError(t, err)

	w.wg.Add(1)
	w.runTask(task, execID, time.Now())

	e, err := d.GetExecution(execID)
	require.NoError(t, err)
	require.Equal(t, "failed", e.Status)
	require.NotNil(t, e.ErrorMessage)
	require.Contains(t, *e.ErrorMessage, "invalid payload")
}

// TestFuncTask_BrakeEngagedBeforeStart mirrors
// TestRunTask_BrakeEngagedBeforeStart_RecordsCanceledWithoutExecuting for a
// func task: the brake engages between poll() spawning the goroutine and it
// reaching the claim, and runTask's pre-start check must record "canceled"
// (via CancelPendingFuncExecution) without ever invoking Run.
func TestFuncTask_BrakeEngagedBeforeStart(t *testing.T) {
	d := newInternalFuncTestDB(t)
	ran := false
	funcs := NewFuncRegistry()
	require.NoError(t, funcs.Register(golane.NewKind[internalEchoPayload]("acme-module.echo", 1, nil, func(ctx context.Context, rc golane.RunContext, p internalEchoPayload) (any, error) {
		ran = true
		return nil, nil
	})))
	brake := NewBrakeGate(false)
	w := newInternalFuncWorker(d, funcs, NewCancelRegistry(), brake, nil, NewHiddenLanes(nil))
	w.runCtx = context.Background()

	task, execID := submitInternalEcho(t, d, "owned-brake000001", internalEchoPayload{}, "acme-module.echo", 1)
	_, err := d.AcquireLock(task.ID, "w", time.Minute)
	require.NoError(t, err)

	brake.Set(true)

	w.wg.Add(1)
	w.runTask(task, execID, time.Now())

	require.False(t, ran, "Run must not be called once the brake is engaged before start")
	e, err := d.GetExecution(execID)
	require.NoError(t, err)
	require.Equal(t, "canceled", e.Status)
	require.Equal(t, 1, countMetrics(t, d, task.Name))
	require.False(t, lockHeld(t, d, task.ID))
}

func TestCancelExecution_PendingCancelBeforeClaim(t *testing.T) {
	d := newInternalFuncTestDB(t)
	ran := false
	funcs := NewFuncRegistry()
	require.NoError(t, funcs.Register(golane.NewKind[internalEchoPayload]("acme-module.echo", 1, nil, func(ctx context.Context, rc golane.RunContext, p internalEchoPayload) (any, error) {
		ran = true
		return nil, nil
	})))
	cancels := NewCancelRegistry()
	board := broker.NewBroker(0)
	hidden := NewHiddenLanes(nil)
	w := newInternalFuncWorker(d, funcs, cancels, NewBrakeGate(false), board, hidden)
	w.runCtx = context.Background()

	task, execID := submitInternalEcho(t, d, "owned-cancel00001", internalEchoPayload{}, "acme-module.echo", 1)
	_, err := d.AcquireLock(task.ID, "w", time.Minute)
	require.NoError(t, err)

	var outcome golane.CancelOutcome
	setHook(t, &beforeFuncClaimHook, func(id int64) {
		outcome, err = CancelExecution(d, cancels, board, hidden, id)
		require.NoError(t, err)
	})

	w.wg.Add(1)
	w.runTask(task, execID, time.Now())

	require.Equal(t, golane.CancelOutcome("canceled"), outcome)
	require.False(t, ran, "Run must never be called for an execution canceled before claim")

	e, err := d.GetExecution(execID)
	require.NoError(t, err)
	require.Equal(t, "canceled", e.Status)
	require.Equal(t, 1, countMetrics(t, d, task.Name))
	require.False(t, lockHeld(t, d, task.ID))
}

func TestCancelExecution_ClaimThenCancel(t *testing.T) {
	d := newInternalFuncTestDB(t)
	ran := false
	funcs := NewFuncRegistry()
	require.NoError(t, funcs.Register(golane.NewKind[internalEchoPayload]("acme-module.echo", 1, nil, func(ctx context.Context, rc golane.RunContext, p internalEchoPayload) (any, error) {
		ran = true
		<-ctx.Done()
		return nil, ctx.Err()
	})))
	cancels := NewCancelRegistry()
	board := broker.NewBroker(0)
	hidden := NewHiddenLanes(nil)
	w := newInternalFuncWorker(d, funcs, cancels, NewBrakeGate(false), board, hidden)
	w.runCtx = context.Background()

	task, execID := submitInternalEcho(t, d, "owned-cancel00002", internalEchoPayload{}, "acme-module.echo", 1)
	_, err := d.AcquireLock(task.ID, "w", time.Minute)
	require.NoError(t, err)

	var outcome golane.CancelOutcome
	setHook(t, &afterFuncClaimHook, func(id int64) {
		outcome, err = CancelExecution(d, cancels, board, hidden, id)
		require.NoError(t, err)
	})

	w.wg.Add(1)
	w.runTask(task, execID, time.Now())

	require.Equal(t, golane.CancelOutcome("canceling"), outcome)
	require.False(t, ran, "step 3b's post-claim ctx check must catch the cancel and finish without ever calling Run")

	e, err := d.GetExecution(execID)
	require.NoError(t, err)
	require.Equal(t, "canceled", e.Status)
	require.Equal(t, 1, countMetrics(t, d, task.Name))
}

func TestCancelExecution_CanceledBeforeRegistration(t *testing.T) {
	d := newInternalFuncTestDB(t)
	ran := false
	funcs := NewFuncRegistry()
	require.NoError(t, funcs.Register(golane.NewKind[internalEchoPayload]("acme-module.echo", 1, nil, func(ctx context.Context, rc golane.RunContext, p internalEchoPayload) (any, error) {
		ran = true
		return nil, nil
	})))
	cancels := NewCancelRegistry()
	board := broker.NewBroker(0)
	hidden := NewHiddenLanes(nil)
	w := newInternalFuncWorker(d, funcs, cancels, NewBrakeGate(false), board, hidden)
	w.runCtx = context.Background()

	task, execID := submitInternalEcho(t, d, "owned-cancel00003", internalEchoPayload{}, "acme-module.echo", 1)
	_, err := d.AcquireLock(task.ID, "w", time.Minute)
	require.NoError(t, err)

	outcome, err := CancelExecution(d, cancels, board, hidden, execID)
	require.NoError(t, err)
	require.Equal(t, golane.CancelOutcome("canceled"), outcome)
	require.Equal(t, 1, countMetrics(t, d, task.Name))

	w.wg.Add(1)
	w.runTask(task, execID, time.Now())

	require.False(t, ran, "Run must not run once the row is already canceled")
	require.Equal(t, 1, countMetrics(t, d, task.Name), "runTask's own claim-miss path must not record a second metric")
	require.False(t, lockHeld(t, d, task.ID))
}

func TestFuncTask_BrakeVsPendingCancel_OneMetric(t *testing.T) {
	d := newInternalFuncTestDB(t)
	ran := false
	funcs := NewFuncRegistry()
	require.NoError(t, funcs.Register(golane.NewKind[internalEchoPayload]("acme-module.echo", 1, nil, func(ctx context.Context, rc golane.RunContext, p internalEchoPayload) (any, error) {
		ran = true
		return nil, nil
	})))
	cancels := NewCancelRegistry()
	board := broker.NewBroker(0)
	hidden := NewHiddenLanes(nil)
	brake := NewBrakeGate(false)
	w := newInternalFuncWorker(d, funcs, cancels, brake, board, hidden)
	w.runCtx = context.Background()

	task, execID := submitInternalEcho(t, d, "owned-brakevs0001", internalEchoPayload{}, "acme-module.echo", 1)
	_, err := d.AcquireLock(task.ID, "w", time.Minute)
	require.NoError(t, err)

	events, closeSub := subscribeBoardEvents(t, board)
	defer closeSub()
	time.Sleep(100 * time.Millisecond) // let the payload subscription land (see board_test.go)

	brake.Set(true)
	outcome, err := CancelExecution(d, cancels, board, hidden, execID)
	require.NoError(t, err)
	require.Equal(t, golane.CancelOutcome("canceled"), outcome)

	w.wg.Add(1)
	w.runTask(task, execID, time.Now())

	require.False(t, ran)
	require.Equal(t, 1, countMetrics(t, d, task.Name))

	select {
	case <-events:
	case <-time.After(2 * time.Second):
		t.Fatal("expected exactly one task-finished board event")
	}
}

func TestFuncTask_BrakeAfterPreCheck_CanceledWithoutRun(t *testing.T) {
	d := newInternalFuncTestDB(t)
	ran := false
	funcs := NewFuncRegistry()
	require.NoError(t, funcs.Register(golane.NewKind[internalEchoPayload]("acme-module.echo", 1, nil, func(ctx context.Context, rc golane.RunContext, p internalEchoPayload) (any, error) {
		ran = true
		return nil, nil
	})))
	brake := NewBrakeGate(false)
	w := newInternalFuncWorker(d, funcs, NewCancelRegistry(), brake, nil, NewHiddenLanes(nil))
	w.runCtx = context.Background()

	task, execID := submitInternalEcho(t, d, "owned-brakeafter1", internalEchoPayload{}, "acme-module.echo", 1)
	_, err := d.AcquireLock(task.ID, "w", time.Minute)
	require.NoError(t, err)

	setHook(t, &beforeFuncClaimHook, func(id int64) {
		brake.Set(true)
	})

	w.wg.Add(1)
	w.runTask(task, execID, time.Now())

	require.False(t, ran)
	e, err := d.GetExecution(execID)
	require.NoError(t, err)
	require.Equal(t, "canceled", e.Status)
	require.NotNil(t, e.ErrorMessage)
	require.Equal(t, "hand brake engaged", *e.ErrorMessage)
	require.Equal(t, 1, countMetrics(t, d, task.Name))
}

func TestFuncTask_ShutdownBeforeClaimStaysPending(t *testing.T) {
	d := newInternalFuncTestDB(t)
	ran := false
	funcs := NewFuncRegistry()
	require.NoError(t, funcs.Register(golane.NewKind[internalEchoPayload]("acme-module.echo", 1, nil, func(ctx context.Context, rc golane.RunContext, p internalEchoPayload) (any, error) {
		ran = true
		return nil, nil
	})))
	w := newInternalFuncWorker(d, funcs, NewCancelRegistry(), NewBrakeGate(false), nil, NewHiddenLanes(nil))
	runCtx, runCancel := context.WithCancel(context.Background())
	w.runCtx = runCtx

	task, execID := submitInternalEcho(t, d, "owned-shutdown0001", internalEchoPayload{}, "acme-module.echo", 1)
	_, err := d.AcquireLock(task.ID, "w", time.Minute)
	require.NoError(t, err)

	setHook(t, &beforeFuncClaimHook, func(id int64) {
		runCancel() // a worker shutdown, not routed through w.cancels
	})

	w.wg.Add(1)
	w.runTask(task, execID, time.Now())

	require.False(t, ran)
	e, err := d.GetExecution(execID)
	require.NoError(t, err)
	require.Equal(t, "pending", e.Status)
	require.Nil(t, e.StartedAt)
	require.Equal(t, 0, countMetrics(t, d, task.Name))
	require.False(t, lockHeld(t, d, task.ID))

	// FR-U2: the queued job survives, and a fresh worker with a live ctx
	// eventually runs it to success.
	w2 := newInternalFuncWorker(d, funcs, NewCancelRegistry(), NewBrakeGate(false), nil, NewHiddenLanes(nil))
	w2.runCtx = context.Background()
	SetPollIntervalForTest(20 * time.Millisecond)
	t.Cleanup(func() { SetPollIntervalForTest(5 * time.Second) })
	ctx2, cancel2 := context.WithCancel(context.Background())
	go w2.Start(ctx2)
	t.Cleanup(func() { cancel2(); w2.Wait() })

	require.Eventually(t, func() bool {
		e, err := d.GetExecution(execID)
		return err == nil && e != nil && e.Status == "success"
	}, 10*time.Second, 20*time.Millisecond)
}

func TestPublishBoardEvent_DropsHiddenLane(t *testing.T) {
	board := broker.NewBroker(0)
	hidden := NewHiddenLanes([]string{"owned"})

	events, closeSub := subscribeBoardEvents(t, board)
	defer closeSub()

	PublishBoardEvent(board, hidden, BoardEvent{Type: "task-finished", Lane: "owned"})
	select {
	case ev := <-events:
		t.Fatalf("event for a hidden lane must not be published, got %q", ev)
	case <-time.After(200 * time.Millisecond):
	}

	PublishBoardEvent(board, hidden, BoardEvent{Type: "task-finished", Lane: "visible"})
	select {
	case ev := <-events:
		require.Contains(t, ev, `"lane":"visible"`)
	case <-time.After(2 * time.Second):
		t.Fatal("event for a visible lane must be published")
	}
}
