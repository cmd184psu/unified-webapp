package coordinator_test

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
	"cmd184psu/unified-webapp/internal/taskmaster/coordinator"
	"cmd184psu/unified-webapp/internal/taskmaster/db"
	"cmd184psu/unified-webapp/internal/taskmaster/golane"
	"cmd184psu/unified-webapp/internal/taskmaster/worker"

	"github.com/stretchr/testify/require"
)

type progressShapePayload struct {
	Report bool `json:"report"`
}

// TestBoardEvents_TaskProgressShape drives a real func-task execution
// through a worker sharing its DB/board/registries with a coordinator, and
// asserts two things over GET /api/board/events: a job whose Kind reports
// progress produces a "task-progress" event carrying progress_pct and
// progress_label, and a job that never reports produces no such event at
// all (only task-started/task-finished).
func TestBoardEvents_TaskProgressShape(t *testing.T) {
	d, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { d.Close() })
	_, err = d.EnsureOwnedLane("owned", "acme-module", 2, 0, false)
	require.NoError(t, err)

	release := make(chan struct{})
	funcs := worker.NewFuncRegistry()
	require.NoError(t, funcs.Register(golane.NewKind[progressShapePayload]("acme-module.echo", 1, nil, func(ctx context.Context, rc golane.RunContext, p progressShapePayload) (any, error) {
		if p.Report {
			rc.Progress(42, "Downloading")
			<-release // hold the row "running" long enough for the test to observe the event before it finishes
		}
		return nil, nil
	})))

	registry := worker.NewRegistry()
	cancels := worker.NewCancelRegistry()
	brake := worker.NewBrakeGate(false)
	procs := worker.NewProcessRegistry()
	board := broker.NewBroker(0)
	hidden := worker.NewHiddenLanes(nil)
	progress := worker.NewProgressRegistry()

	w := worker.New(d, registry, "w", worker.NewSudoGate(false), cancels, brake, procs, board)
	w.EnableFuncTasks(worker.FuncOptions{Funcs: funcs, Progress: progress, Hidden: hidden, ProgressInterval: 10 * time.Millisecond})

	c := coordinator.New(d, registry, worker.NewSudoGate(false), cancels, brake, procs, worker.NewSSECap(0), board, hidden, progress)
	srv := httptest.NewServer(coordinator.Routes(c))
	defer srv.Close()

	// Subscribe to the board stream BEFORE anything runs, so there is no
	// race between a Publish and this test's subscription landing.
	sub, err := http.Get(srv.URL + "/api/board/events")
	require.NoError(t, err)
	defer sub.Body.Close()
	events := make(chan string, 64)
	go func() {
		scanner := bufio.NewScanner(sub.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "data:") {
				events <- strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			}
		}
	}()
	time.Sleep(150 * time.Millisecond) // let the payload subscription land (see board_test.go)

	worker.SetPollIntervalForTest(20 * time.Millisecond)
	t.Cleanup(func() { worker.SetPollIntervalForTest(5 * time.Second) })
	ctx, cancel := context.WithCancel(context.Background())
	go w.Start(ctx)
	t.Cleanup(func() { cancel(); w.Wait() })

	_, reportingExecID, err := d.SubmitFuncTask(db.FuncTaskSpec{Name: "owned-progshape01", Lane: "owned", Kind: "acme-module.echo", Label: "reports", Payload: []byte(`{"report":true}`), PayloadVersion: 1})
	require.NoError(t, err)
	_, silentExecID, err := d.SubmitFuncTask(db.FuncTaskSpec{Name: "owned-progshape02", Lane: "owned", Kind: "acme-module.echo", Label: "silent", Payload: []byte(`{"report":false}`), PayloadVersion: 1})
	require.NoError(t, err)

	// Collect events for both jobs until the reporting one finishes.
	var reportingProgressEvents []map[string]any
	var silentProgressEvents []map[string]any
	reportingFinished := false

	deadline := time.After(10 * time.Second)
	for !reportingFinished {
		select {
		case raw := <-events:
			var ev map[string]any
			require.NoError(t, json.Unmarshal([]byte(raw), &ev))
			execID, _ := ev["execution_id"].(float64)
			switch ev["type"] {
			case "task-progress":
				if int64(execID) == reportingExecID {
					reportingProgressEvents = append(reportingProgressEvents, ev)
				}
				if int64(execID) == silentExecID {
					silentProgressEvents = append(silentProgressEvents, ev)
				}
			case "task-finished":
				if int64(execID) == reportingExecID {
					reportingFinished = true
				}
			}
			if ev["type"] == "task-started" && int64(execID) == reportingExecID {
				// Give the reporter a moment to persist+publish before
				// releasing, then let the reporting job finish.
				go func() {
					time.Sleep(200 * time.Millisecond)
					close(release)
				}()
			}
		case <-deadline:
			t.Fatal("timed out waiting for board events")
		}
	}

	require.NotEmpty(t, reportingProgressEvents, "a job that calls Progress must publish at least one task-progress event")
	got := reportingProgressEvents[0]
	require.Equal(t, "owned", got["lane"])
	require.Equal(t, float64(42), got["progress_pct"])
	require.Equal(t, "Downloading", got["progress_label"])

	require.Empty(t, silentProgressEvents, "a job that never calls Progress must publish no task-progress event")
}
