package worker_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"cmd184psu/unified-webapp/internal/taskmaster/db"
	"cmd184psu/unified-webapp/internal/taskmaster/golane"
	"cmd184psu/unified-webapp/internal/taskmaster/models"
	"cmd184psu/unified-webapp/internal/taskmaster/worker"

	"github.com/stretchr/testify/require"
)

// echoPayload is the test Kind's payload (acme-module.echo — A5 requires
// internal/taskmaster to name no specific module).
type echoPayload struct {
	Msg  string `json:"msg"`
	Fail bool   `json:"fail"`
}

func echoKind(name string) golane.Kind {
	return golane.NewKind[echoPayload](name, 1, nil, func(ctx context.Context, rc golane.RunContext, p echoPayload) (any, error) {
		if p.Fail {
			return nil, errors.New("requested failure")
		}
		return map[string]string{"echo": p.Msg}, nil
	})
}

// newFuncTestWorker builds a Worker with func-task support enabled, mirroring
// newTestWorker above but wiring EnableFuncTasks with the given registry (or
// a fresh one if nil) and a short progress interval.
func newFuncTestWorker(t *testing.T, funcs *worker.FuncRegistry) (*worker.Worker, *db.DB) {
	t.Helper()
	d, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { d.Close() })
	require.NoError(t, d.UpsertLane(&models.Lane{Name: "owned", Width: 2, Owner: "acme-module"}))

	if funcs == nil {
		funcs = worker.NewFuncRegistry()
	}
	w := worker.New(d, worker.NewRegistry(), "test-worker", worker.NewSudoGate(false),
		worker.NewCancelRegistry(), worker.NewBrakeGate(false), worker.NewProcessRegistry(), nil)
	w.EnableFuncTasks(worker.FuncOptions{
		Funcs: funcs, Progress: worker.NewProgressRegistry(), Hidden: worker.NewHiddenLanes(nil),
		ProgressInterval: 20 * time.Millisecond,
	})
	return w, d
}

func submitEcho(t *testing.T, d *db.DB, name string, payload echoPayload) int64 {
	t.Helper()
	raw, err := json.Marshal(payload)
	require.NoError(t, err)
	_, execID, err := d.SubmitFuncTask(db.FuncTaskSpec{Name: name, Lane: "owned", Kind: "acme-module.echo", Label: "test job", Payload: raw, PayloadVersion: 1})
	require.NoError(t, err)
	return execID
}

func TestFuncTask_RunsWithPayloadAndStoresResult(t *testing.T) {
	funcs := worker.NewFuncRegistry()
	require.NoError(t, funcs.Register(echoKind("acme-module.echo")))
	w, d := newFuncTestWorker(t, funcs)

	execID := submitEcho(t, d, "owned-aaaaaaaaaaaa", echoPayload{Msg: "hi"})

	worker.SetPollIntervalForTest(20 * time.Millisecond)
	t.Cleanup(func() { worker.SetPollIntervalForTest(5 * time.Second) })
	ctx, cancel := context.WithCancel(context.Background())
	go w.Start(ctx)
	t.Cleanup(func() { cancel(); w.Wait() })

	require.Eventually(t, func() bool {
		e, err := d.GetExecution(execID)
		return err == nil && e != nil && e.Status == "success"
	}, 10*time.Second, 20*time.Millisecond)

	e, err := d.GetExecution(execID)
	require.NoError(t, err)
	require.Contains(t, string(e.Result), "hi")
}

func TestFuncTask_RespectsLaneWidth(t *testing.T) {
	release := make(chan struct{})
	started := make(chan string, 10)
	funcs := worker.NewFuncRegistry()
	require.NoError(t, funcs.Register(golane.NewKind[echoPayload]("acme-module.echo", 1, nil, func(ctx context.Context, rc golane.RunContext, p echoPayload) (any, error) {
		started <- rc.JobID()
		<-release
		return nil, nil
	})))

	d, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { d.Close() })
	require.NoError(t, d.UpsertLane(&models.Lane{Name: "owned", Width: 1, Owner: "acme-module"}))

	w := worker.New(d, worker.NewRegistry(), "test-worker", worker.NewSudoGate(false),
		worker.NewCancelRegistry(), worker.NewBrakeGate(false), worker.NewProcessRegistry(), nil)
	w.EnableFuncTasks(worker.FuncOptions{Funcs: funcs, Progress: worker.NewProgressRegistry(), Hidden: worker.NewHiddenLanes(nil), ProgressInterval: 20 * time.Millisecond})

	submitEcho(t, d, "owned-000000000001", echoPayload{Msg: "a"})
	submitEcho(t, d, "owned-000000000002", echoPayload{Msg: "b"})

	worker.SetPollIntervalForTest(20 * time.Millisecond)
	t.Cleanup(func() { worker.SetPollIntervalForTest(5 * time.Second) })
	ctx, cancel := context.WithCancel(context.Background())
	go w.Start(ctx)
	t.Cleanup(func() { cancel(); w.Wait() })

	var first string
	select {
	case first = <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("first job never started")
	}
	select {
	case second := <-started:
		t.Fatalf("second job %q started before the first (%q) finished; width 1 was not respected", second, first)
	case <-time.After(300 * time.Millisecond):
	}
	release <- struct{}{}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("second job never started after the first released")
	}
	close(release)
}

func TestFuncTask_UnregisteredKindStaysPending_ThenRunsAfterRegister(t *testing.T) {
	funcs := worker.NewFuncRegistry()
	w, d := newFuncTestWorker(t, funcs)

	execID := submitEcho(t, d, "owned-aaaaaaaaaaab", echoPayload{Msg: "later"})

	worker.SetPollIntervalForTest(20 * time.Millisecond)
	t.Cleanup(func() { worker.SetPollIntervalForTest(5 * time.Second) })
	ctx, cancel := context.WithCancel(context.Background())
	go w.Start(ctx)
	t.Cleanup(func() { cancel(); w.Wait() })

	time.Sleep(200 * time.Millisecond)
	e, err := d.GetExecution(execID)
	require.NoError(t, err)
	require.Equal(t, "pending", e.Status, "an unregistered kind must stay pending, not fail or run")

	require.NoError(t, funcs.Register(echoKind("acme-module.echo")))

	require.Eventually(t, func() bool {
		e, err := d.GetExecution(execID)
		return err == nil && e != nil && e.Status == "success"
	}, 10*time.Second, 20*time.Millisecond)
}

func TestFuncTask_LabelUpdate(t *testing.T) {
	funcs := worker.NewFuncRegistry()
	require.NoError(t, funcs.Register(golane.NewKind[echoPayload]("acme-module.echo", 1, nil, func(ctx context.Context, rc golane.RunContext, p echoPayload) (any, error) {
		rc.SetLabel("relabeled")
		return nil, nil
	})))
	w, d := newFuncTestWorker(t, funcs)
	execID := submitEcho(t, d, "owned-aaaaaaaaaaac", echoPayload{Msg: "x"})

	worker.SetPollIntervalForTest(20 * time.Millisecond)
	t.Cleanup(func() { worker.SetPollIntervalForTest(5 * time.Second) })
	ctx, cancel := context.WithCancel(context.Background())
	go w.Start(ctx)
	t.Cleanup(func() { cancel(); w.Wait() })

	require.Eventually(t, func() bool {
		e, err := d.GetExecution(execID)
		return err == nil && e != nil && e.Status == "success"
	}, 10*time.Second, 20*time.Millisecond)

	task, err := d.GetTask("owned-aaaaaaaaaaac")
	require.NoError(t, err)
	require.Equal(t, "relabeled", task.Label)
}

func TestFuncTask_CancelRunningRecordsCanceled(t *testing.T) {
	release := make(chan struct{})
	funcs := worker.NewFuncRegistry()
	require.NoError(t, funcs.Register(golane.NewKind[echoPayload]("acme-module.echo", 1, nil, func(ctx context.Context, rc golane.RunContext, p echoPayload) (any, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	})))

	d, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { d.Close() })
	require.NoError(t, d.UpsertLane(&models.Lane{Name: "owned", Width: 1, Owner: "acme-module"}))

	cancels := worker.NewCancelRegistry()
	w := worker.New(d, worker.NewRegistry(), "test-worker", worker.NewSudoGate(false),
		cancels, worker.NewBrakeGate(false), worker.NewProcessRegistry(), nil)
	hidden := worker.NewHiddenLanes(nil)
	w.EnableFuncTasks(worker.FuncOptions{Funcs: funcs, Progress: worker.NewProgressRegistry(), Hidden: hidden, ProgressInterval: 20 * time.Millisecond})

	execID := submitEcho(t, d, "owned-aaaaaaaaaaad", echoPayload{Msg: "x"})

	worker.SetPollIntervalForTest(20 * time.Millisecond)
	t.Cleanup(func() { worker.SetPollIntervalForTest(5 * time.Second) })
	ctx, cancel := context.WithCancel(context.Background())
	go w.Start(ctx)
	t.Cleanup(func() { cancel(); w.Wait() })

	require.Eventually(t, func() bool {
		e, err := d.GetExecution(execID)
		return err == nil && e != nil && e.Status == "running"
	}, 10*time.Second, 20*time.Millisecond)

	outcome, err := worker.CancelExecution(d, cancels, nil, hidden, execID)
	require.NoError(t, err)
	require.Equal(t, golane.CancelOutcome("canceling"), outcome)
	close(release)

	require.Eventually(t, func() bool {
		e, err := d.GetExecution(execID)
		return err == nil && e != nil && e.Status == "canceled"
	}, 10*time.Second, 20*time.Millisecond)
}
