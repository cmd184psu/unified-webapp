package coordinator_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"cmd184psu/unified-webapp/internal/platform/broker"
	"cmd184psu/unified-webapp/internal/taskmaster/coordinator"
	"cmd184psu/unified-webapp/internal/taskmaster/db"
	"cmd184psu/unified-webapp/internal/taskmaster/models"
	"cmd184psu/unified-webapp/internal/taskmaster/worker"

	"github.com/stretchr/testify/require"
)

// newFuncTestServer builds a coordinator wired with real HiddenLanes/
// ProgressRegistry instances (every pre-existing call site gets nil,nil per
// §4.12/§7.5 — these new tests need the real thing to exercise N4/§4.9).
func newFuncTestServer(t *testing.T) (*httptest.Server, *db.DB, *worker.HiddenLanes, *worker.ProgressRegistry, *worker.CancelRegistry, *broker.Broker) {
	t.Helper()
	d, err := db.Open(":memory:")
	require.NoError(t, err)

	require.NoError(t, d.UpsertLane(&models.Lane{Name: "test", Width: 2}))
	// UpsertLane (the config/UI path) deliberately never touches
	// owner/hidden/retention_days, so an owned lane in tests must go through
	// EnsureOwnedLane, exactly as Engine.RegisterLane does in production.
	_, err = d.EnsureOwnedLane("owned", "acme-module", 2, 0, false)
	require.NoError(t, err)

	registry := worker.NewRegistry()
	cancels := worker.NewCancelRegistry()
	brake := worker.NewBrakeGate(false)
	procs := worker.NewProcessRegistry()
	board := broker.NewBroker(0)
	hidden := worker.NewHiddenLanes(nil)
	progress := worker.NewProgressRegistry()
	c := coordinator.New(d, registry, worker.NewSudoGate(false), cancels, brake, procs, 0, board, hidden, progress)
	srv := httptest.NewServer(coordinator.Routes(c))
	t.Cleanup(func() {
		srv.Close()
		d.Close()
	})
	return srv, d, hidden, progress, cancels, board
}

// addFuncTask submits a one-shot func task directly at the DB layer
// (bypassing the golane/engine layer, which the coordinator package doesn't
// depend on) and returns the task and its pending execution id.
func addFuncTask(t *testing.T, d *db.DB, name, lane string) (*models.Task, int64) {
	t.Helper()
	_, execID, err := d.SubmitFuncTask(db.FuncTaskSpec{Name: name, Lane: lane, Kind: "acme-module.echo", Label: "job", Payload: []byte(`{}`), PayloadVersion: 1})
	require.NoError(t, err)
	task, err := d.GetTask(name)
	require.NoError(t, err)
	return task, execID
}

func doJSON(t *testing.T, method, url string, body any) *http.Response {
	t.Helper()
	var r *http.Request
	var err error
	if body != nil {
		b, merr := json.Marshal(body)
		require.NoError(t, merr)
		r, err = http.NewRequest(method, url, bytes.NewReader(b))
	} else {
		r, err = http.NewRequest(method, url, nil)
	}
	require.NoError(t, err)
	r.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(r)
	require.NoError(t, err)
	return resp
}

// ─── Rerun ────────────────────────────────────────────────────────────────

func TestRerunExecution_Shapes(t *testing.T) {
	srv, d, _, _, _, _ := newFuncTestServer(t)

	t.Run("invalid id -> 400", func(t *testing.T) {
		resp := doJSON(t, "POST", srv.URL+"/api/executions/notanumber/rerun", nil)
		defer resp.Body.Close()
		require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})

	t.Run("unknown id -> 404", func(t *testing.T) {
		resp := doJSON(t, "POST", srv.URL+"/api/executions/999999/rerun", nil)
		defer resp.Body.Close()
		require.Equal(t, http.StatusNotFound, resp.StatusCode)
	})

	t.Run("not finished -> 409", func(t *testing.T) {
		_, execID := addFuncTask(t, d, "owned-rerun0000001", "owned")
		resp := doJSON(t, "POST", fmt.Sprintf("%s/api/executions/%d/rerun", srv.URL, execID), nil)
		defer resp.Body.Close()
		require.Equal(t, http.StatusConflict, resp.StatusCode)
	})

	t.Run("func succeeded -> 409 ErrSucceeded message", func(t *testing.T) {
		task, execID := addFuncTask(t, d, "owned-rerun0000002", "owned")
		_, err := d.ClaimFuncExecution(execID, "w")
		require.NoError(t, err)
		require.NoError(t, d.FinishFuncExecution(execID, "success", nil, 5, 0, nil, nil))
		_ = task

		resp := doJSON(t, "POST", fmt.Sprintf("%s/api/executions/%d/rerun", srv.URL, execID), nil)
		defer resp.Body.Close()
		require.Equal(t, http.StatusConflict, resp.StatusCode)
		var body map[string]string
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
		require.Contains(t, body["error"], "already succeeded")
	})

	t.Run("finished failed -> 201 and re-queues", func(t *testing.T) {
		_, execID := addFuncTask(t, d, "owned-rerun0000003", "owned")
		_, err := d.ClaimFuncExecution(execID, "w")
		require.NoError(t, err)
		require.NoError(t, d.FinishFuncExecution(execID, "failed", nil, 5, 0, nil, nil))

		resp := doJSON(t, "POST", fmt.Sprintf("%s/api/executions/%d/rerun", srv.URL, execID), nil)
		defer resp.Body.Close()
		require.Equal(t, http.StatusCreated, resp.StatusCode)
		var body map[string]int64
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
		require.NotZero(t, body["execution_id"])
	})

	t.Run("shell task reruns from any terminal status (R3)", func(t *testing.T) {
		taskID, err := d.AddTask(&models.Task{Name: "shell-rerun", LaneName: "test", Enabled: true, Command: "echo hi"})
		require.NoError(t, err)
		execID, err := d.CreateExecution(taskID, "w", time.Now())
		require.NoError(t, err)
		require.NoError(t, d.StartExecution(execID, "w"))
		require.NoError(t, d.FinishExecution(execID, "canceled", nil, 1, 0))

		resp := doJSON(t, "POST", fmt.Sprintf("%s/api/executions/%d/rerun", srv.URL, execID), nil)
		defer resp.Body.Close()
		require.Equal(t, http.StatusCreated, resp.StatusCode)
	})
}

func TestCancelExecution_PendingFuncCanceled_PendingShellNotRunning(t *testing.T) {
	srv, d, _, _, _, _ := newFuncTestServer(t)

	t.Run("pending func execution is canceled outright (P4)", func(t *testing.T) {
		_, execID := addFuncTask(t, d, "owned-cancel0000001", "owned")
		resp := doJSON(t, "POST", fmt.Sprintf("%s/api/executions/%d/cancel", srv.URL, execID), nil)
		defer resp.Body.Close()
		require.Equal(t, http.StatusOK, resp.StatusCode)
		var body map[string]string
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
		require.Equal(t, "canceled", body["status"])

		e, err := d.GetExecution(execID)
		require.NoError(t, err)
		require.Equal(t, "canceled", e.Status)
	})

	t.Run("pending shell execution stays not_running (P4)", func(t *testing.T) {
		taskID, err := d.AddTask(&models.Task{Name: "shell-pending", LaneName: "test", Enabled: false, Command: "echo hi"})
		require.NoError(t, err)
		execID, err := d.CreateExecution(taskID, "w", time.Now())
		require.NoError(t, err)

		resp := doJSON(t, "POST", fmt.Sprintf("%s/api/executions/%d/cancel", srv.URL, execID), nil)
		defer resp.Body.Close()
		require.Equal(t, http.StatusOK, resp.StatusCode)
		var body map[string]string
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
		require.Equal(t, "not_running", body["status"])

		e, err := d.GetExecution(execID)
		require.NoError(t, err)
		require.Equal(t, "pending", e.Status, "a pending shell execution must not be canceled")
	})
}

// ─── Owned-lane / func-task guards (P20) ───────────────────────────────────

func TestOwnedLaneGuards(t *testing.T) {
	srv, d, _, _, _, _ := newFuncTestServer(t)

	task, _ := addFuncTask(t, d, "owned-guard00000001", "owned")

	cases := []struct {
		name   string
		method string
		path   string
		body   any
		want   int
	}{
		{"update func task -> 409", "PUT", "/api/tasks/" + task.Name, map[string]any{"paused": true}, http.StatusConflict},
		{"pause func task -> 409", "POST", "/api/tasks/" + task.Name + "/pause", nil, http.StatusConflict},
		{"resume func task -> 409", "POST", "/api/tasks/" + task.Name + "/resume", nil, http.StatusConflict},
		{"up-next func task -> 409", "POST", "/api/tasks/" + task.Name + "/up-next", nil, http.StatusConflict},
		{"move func task -> 409", "POST", "/api/tasks/" + task.Name + "/move", map[string]any{"lane_name": "test"}, http.StatusConflict},
		{"add task to owned lane -> 400", "POST", "/api/tasks", map[string]any{"name": "newone", "lane_name": "owned"}, http.StatusBadRequest},
		{"delete owned lane -> 409", "DELETE", "/api/lanes/owned", nil, http.StatusConflict},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := doJSON(t, tc.method, srv.URL+tc.path, tc.body)
			defer resp.Body.Close()
			require.Equal(t, tc.want, resp.StatusCode, tc.name)
		})
	}

	t.Run("width on owned lane is allowed (P7)", func(t *testing.T) {
		resp := doJSON(t, "PUT", srv.URL+"/api/lanes/owned/width", map[string]any{"width": 5})
		defer resp.Body.Close()
		require.Equal(t, http.StatusOK, resp.StatusCode)
	})

	t.Run("pause/resume on owned lane is allowed (P7)", func(t *testing.T) {
		resp := doJSON(t, "POST", srv.URL+"/api/lanes/owned/pause", nil)
		resp.Body.Close()
		require.Equal(t, http.StatusOK, resp.StatusCode)
		resp = doJSON(t, "POST", srv.URL+"/api/lanes/owned/resume", nil)
		defer resp.Body.Close()
		require.Equal(t, http.StatusOK, resp.StatusCode)
	})
}

// TestHandleAddTask_FuncTaskNameConflict pins the other half of
// handleAddTask's P20 guard table row, distinct from "target lane is
// owned": an HTTP POST /api/tasks whose name collides with an EXISTING func
// task must 409, naming the owning module, even when the request's own
// lane_name points at a perfectly ordinary (non-owned) lane.
func TestHandleAddTask_FuncTaskNameConflict(t *testing.T) {
	srv, d, _, _, _, _ := newFuncTestServer(t)
	task, _ := addFuncTask(t, d, "owned-nameconflict01", "owned")

	resp := doJSON(t, "POST", srv.URL+"/api/tasks", map[string]any{
		"name": task.Name, "lane_name": "test", "command": "echo hi",
	})
	defer resp.Body.Close()
	require.Equal(t, http.StatusConflict, resp.StatusCode)
	var body map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	require.Contains(t, body["error"], "is managed by module")
	require.Contains(t, body["error"], "acme-module")

	// The existing func task itself must be completely untouched by the
	// rejected request (AddTask's WHERE tasks.kind = '' guard, defence in
	// depth behind this 409).
	unchanged, err := d.GetTask(task.Name)
	require.NoError(t, err)
	require.Equal(t, "owned", unchanged.LaneName)
	require.Equal(t, "acme-module.echo", unchanged.Kind)
}

func TestHandleUpdateTask_RejectsUnknownField(t *testing.T) {
	srv, d, _, _, _, _ := newFuncTestServer(t)
	_, err := d.AddTask(&models.Task{Name: "shell1", LaneName: "test", Enabled: true, Command: "echo hi"})
	require.NoError(t, err)

	resp := doJSON(t, "PUT", srv.URL+"/api/tasks/shell1", map[string]any{"kind": "acme-module.echo"})
	defer resp.Body.Close()
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	var body map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	require.Contains(t, body["error"], "unknown or read-only field")
}

func TestHandleUpdateTask_FuncTaskConflict(t *testing.T) {
	srv, d, _, _, _, _ := newFuncTestServer(t)
	task, _ := addFuncTask(t, d, "owned-update00000001", "owned")

	resp := doJSON(t, "PUT", srv.URL+"/api/tasks/"+task.Name, map[string]any{"paused": true})
	defer resp.Body.Close()
	require.Equal(t, http.StatusConflict, resp.StatusCode)
}

func TestHandleDeleteTask_FuncRunningConflict(t *testing.T) {
	srv, d, _, _, _, _ := newFuncTestServer(t)
	task, execID := addFuncTask(t, d, "owned-delete00000001", "owned")
	_, err := d.ClaimFuncExecution(execID, "w")
	require.NoError(t, err)

	resp := doJSON(t, "DELETE", srv.URL+"/api/tasks/"+task.Name, nil)
	defer resp.Body.Close()
	require.Equal(t, http.StatusConflict, resp.StatusCode)
}

// ─── Hidden-lane visibility (N4) ────────────────────────────────────────────

func TestHiddenLane_InvisibleEverywhere(t *testing.T) {
	srv, d, hidden, _, _, _ := newFuncTestServer(t)
	task, execID := addFuncTask(t, d, "owned-hidden00000001", "owned")
	hidden.Set("owned", true)

	t.Run("lanes list omits it", func(t *testing.T) {
		resp := doJSON(t, "GET", srv.URL+"/api/lanes", nil)
		defer resp.Body.Close()
		var lanes []map[string]any
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&lanes))
		for _, l := range lanes {
			require.NotEqual(t, "owned", l["name"])
		}
	})

	t.Run("get lane -> 404", func(t *testing.T) {
		resp := doJSON(t, "GET", srv.URL+"/api/lanes/owned", nil)
		defer resp.Body.Close()
		require.Equal(t, http.StatusNotFound, resp.StatusCode)
	})

	t.Run("tasks list omits it", func(t *testing.T) {
		resp := doJSON(t, "GET", srv.URL+"/api/tasks", nil)
		defer resp.Body.Close()
		var tasks []map[string]any
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&tasks))
		for _, ts := range tasks {
			require.NotEqual(t, task.Name, ts["name"])
		}
	})

	t.Run("get task -> 404", func(t *testing.T) {
		resp := doJSON(t, "GET", srv.URL+"/api/tasks/"+task.Name, nil)
		defer resp.Body.Close()
		require.Equal(t, http.StatusNotFound, resp.StatusCode)
	})

	t.Run("executions list omits it", func(t *testing.T) {
		resp := doJSON(t, "GET", srv.URL+"/api/executions", nil)
		defer resp.Body.Close()
		var execs []map[string]any
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&execs))
		for _, e := range execs {
			require.NotEqual(t, float64(execID), e["id"])
		}
	})

	t.Run("output -> 404", func(t *testing.T) {
		resp := doJSON(t, "GET", fmt.Sprintf("%s/api/executions/%d/output", srv.URL, execID), nil)
		defer resp.Body.Close()
		require.Equal(t, http.StatusNotFound, resp.StatusCode)
	})

	t.Run("cancel -> 404", func(t *testing.T) {
		resp := doJSON(t, "POST", fmt.Sprintf("%s/api/executions/%d/cancel", srv.URL, execID), nil)
		defer resp.Body.Close()
		require.Equal(t, http.StatusNotFound, resp.StatusCode)
	})

	t.Run("rerun -> 404", func(t *testing.T) {
		resp := doJSON(t, "POST", fmt.Sprintf("%s/api/executions/%d/rerun", srv.URL, execID), nil)
		defer resp.Body.Close()
		require.Equal(t, http.StatusNotFound, resp.StatusCode)
	})

	t.Run("metrics excludes it", func(t *testing.T) {
		resp := doJSON(t, "GET", srv.URL+"/api/metrics", nil)
		defer resp.Body.Close()
		var rows []map[string]any
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&rows))
		for _, r := range rows {
			require.NotEqual(t, "owned", r["group_name"])
		}
	})
}

func TestMetrics_HiddenLaneExcludedByPolicy(t *testing.T) {
	// This test's existence, and its comment, name the switch this policy
	// hangs on: metricsIncludeHiddenLanes (handlers_metrics.go). Flipping
	// that const to true must be paired with flipping this test.
	srv, d, hidden, _, _, _ := newFuncTestServer(t)
	task, execID := addFuncTask(t, d, "owned-metrics00000001", "owned")
	_, err := d.ClaimFuncExecution(execID, "w")
	require.NoError(t, err)
	require.NoError(t, d.FinishFuncExecution(execID, "success", nil, 5, 0, nil, nil))
	require.NoError(t, d.RecordMetric(task.ID, execID, "success", 5, 0))
	hidden.Set("owned", true)

	resp := doJSON(t, "GET", srv.URL+"/api/metrics", nil)
	defer resp.Body.Close()
	var rows []map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&rows))
	for _, r := range rows {
		require.NotEqual(t, "acme-module.echo", r["task_name"])
	}
}

// ─── Progress overlay (§4.9) ────────────────────────────────────────────────

func TestListExecutions_ProgressOverlayForRunning(t *testing.T) {
	srv, d, _, progress, _, _ := newFuncTestServer(t)
	_, execID := addFuncTask(t, d, "owned-progress00000001", "owned")
	_, err := d.ClaimFuncExecution(execID, "w")
	require.NoError(t, err)

	pct := 42
	progress.Set(execID, models.Progress{Pct: &pct, Label: "Downloading"})

	resp := doJSON(t, "GET", srv.URL+"/api/executions?task=owned-progress00000001", nil)
	defer resp.Body.Close()
	var execs []map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&execs))
	require.Len(t, execs, 1)
	require.Equal(t, float64(42), execs[0]["progress_pct"])
	require.Equal(t, "Downloading", execs[0]["progress_label"])
}
