package coordinator_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"cmd184psu/unified-webapp/internal/taskmaster/models"

	"github.com/stretchr/testify/require"
)

// TestHandlePauseExecution_NotRunning covers the 404 path: pausing an
// execution ID that was never registered as running.
func TestHandlePauseExecution_NotRunning(t *testing.T) {
	srv, _, _, _, _, _ := newTestServerFull(t, false, 0)
	resp, err := http.DefaultClient.Do(jsonReq(t, "POST", srv.URL+"/api/executions/99999/pause", nil))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
}

// TestHandleResumeExecution_NotRunning mirrors
// TestHandlePauseExecution_NotRunning for resume.
func TestHandleResumeExecution_NotRunning(t *testing.T) {
	srv, _, _, _, _, _ := newTestServerFull(t, false, 0)
	resp, err := http.DefaultClient.Do(jsonReq(t, "POST", srv.URL+"/api/executions/99999/resume", nil))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
}

// TestHandlePauseExecution_SignalFailure_ReturnsBadRequest registers a PID
// that (essentially certainly) doesn't correspond to a running process, so
// the underlying signal fails — a distinct outcome from "not registered"
// (400 vs 404).
func TestHandlePauseExecution_SignalFailure_ReturnsBadRequest(t *testing.T) {
	srv, d, _, _, _, procs := newTestServerFull(t, false, 0)
	taskID, err := d.AddTask(&models.Task{Name: "p2", LaneName: "test", Enabled: true, Position: 50, Command: "echo hi"})
	require.NoError(t, err)
	execID, err := d.CreateExecution(taskID, "w", time.Now())
	require.NoError(t, err)

	procs.Register(execID, 1<<30, false)

	resp, err := http.DefaultClient.Do(jsonReq(t, "POST", fmt.Sprintf("%s/api/executions/%d/pause", srv.URL, execID), nil))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

// TestListExecutions_ExposesPidAndSuspended verifies /api/executions merges
// the in-memory ProcessRegistry state (pid, suspended) into a running
// execution's JSON, and that pausing it flips suspended to true.
func TestListExecutions_ExposesPidAndSuspended(t *testing.T) {
	srv, d, _, _, _, procs := newTestServerFull(t, false, 0)
	taskID, err := d.AddTask(&models.Task{Name: "p", LaneName: "test", Enabled: true, Position: 50, Command: "echo hi"})
	require.NoError(t, err)
	execID, err := d.CreateExecution(taskID, "w", time.Now())
	require.NoError(t, err)
	require.NoError(t, d.StartExecution(execID, "w"))

	procs.Register(execID, 4242, false)

	resp, err := http.Get(srv.URL + "/api/executions")
	require.NoError(t, err)
	defer resp.Body.Close()
	var execs []models.TaskExecution
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&execs))
	require.Len(t, execs, 1)
	require.NotNil(t, execs[0].Pid)
	require.Equal(t, 4242, *execs[0].Pid)
	require.False(t, execs[0].Suspended)
}
