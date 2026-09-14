package coordinator_test

import (
	"bufio"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"cmd184psu/unified-webapp/internal/platform/broker"
	"cmd184psu/unified-webapp/internal/taskmaster/coordinator"
	"cmd184psu/unified-webapp/internal/taskmaster/db"
	"cmd184psu/unified-webapp/internal/taskmaster/models"
	"cmd184psu/unified-webapp/internal/taskmaster/worker"

	"github.com/stretchr/testify/require"
)

// TestHandleBoardEvents_ReceivesTaskEnqueued subscribes to
// GET /api/board/events, then fires POST /api/tasks/{name}/up-next, and
// asserts the subscriber sees a task-enqueued event carrying the task name.
func TestHandleBoardEvents_ReceivesTaskEnqueued(t *testing.T) {
	d, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { d.Close() })
	require.NoError(t, d.UpsertLane(&models.Lane{Name: "test", Width: 2}))
	_, err = d.AddTask(&models.Task{Name: "my-task", LaneName: "test", Enabled: false, Position: 50, Command: "echo hi"})
	require.NoError(t, err)

	registry := worker.NewRegistry()
	cancels := worker.NewCancelRegistry()
	brake := worker.NewBrakeGate(false)
	b := broker.NewBroker(0)
	c := coordinator.New(d, registry, worker.NewSudoGate(false), cancels, brake, worker.NewProcessRegistry(), 0, b)

	srv := httptest.NewServer(coordinator.Routes(c))
	defer srv.Close()

	sub, err := http.Get(srv.URL + "/api/board/events")
	require.NoError(t, err)
	defer sub.Body.Close()

	events := make(chan string, 16)
	go func() {
		scanner := bufio.NewScanner(sub.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "data:") {
				events <- strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			}
		}
	}()

	// The broker flushes its initial ": connected" comment (which our GET
	// above already waited on) before it registers this subscriber's
	// payload channel, so there's a short window right after connecting
	// where a Publish would be dropped. Retry the trigger action instead of
	// relying on a fixed delay, so the test isn't flaky under load.
	deadline := time.After(5 * time.Second)
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		resp, err := http.Post(srv.URL+"/api/tasks/my-task/up-next", "application/json", nil)
		require.NoError(t, err)
		resp.Body.Close()
		require.Equal(t, http.StatusCreated, resp.StatusCode)

		select {
		case ev := <-events:
			require.Contains(t, ev, `"type":"task-enqueued"`)
			require.Contains(t, ev, `"task":"my-task"`)
			return
		case <-tick.C:
			continue
		case <-deadline:
			t.Fatal("timed out waiting for task-enqueued board event")
		}
	}
}

// TestHandleBoardEvents_SSECap mirrors TestHandleExecutionOutput_SSECap: a
// second concurrent subscriber past the broker's SetMaxSubscribers cap must
// be rejected with 503 before any SSE header is written.
func TestHandleBoardEvents_SSECap(t *testing.T) {
	d, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { d.Close() })
	require.NoError(t, d.UpsertLane(&models.Lane{Name: "test", Width: 2}))

	registry := worker.NewRegistry()
	cancels := worker.NewCancelRegistry()
	brake := worker.NewBrakeGate(false)
	b := broker.NewBroker(0)
	b.SetMaxSubscribers(1)
	c := coordinator.New(d, registry, worker.NewSudoGate(false), cancels, brake, worker.NewProcessRegistry(), 0, b)

	srv := httptest.NewServer(coordinator.Routes(c))
	defer srv.Close()

	started := make(chan struct{})
	stop := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		resp, err := http.Get(srv.URL + "/api/board/events")
		if err != nil {
			return
		}
		defer resp.Body.Close()
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			close(started)
			break
		}
		<-stop
	}()

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for first board-events subscriber to start")
	}

	resp2, err := http.Get(srv.URL + "/api/board/events")
	require.NoError(t, err)
	defer resp2.Body.Close()
	require.Equal(t, http.StatusServiceUnavailable, resp2.StatusCode)

	close(stop)
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for first subscriber goroutine to finish")
	}
}
