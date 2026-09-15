package coordinator_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"cmd184psu/unified-webapp/internal/platform/broker"
	"cmd184psu/unified-webapp/internal/taskmaster/coordinator"
	"cmd184psu/unified-webapp/internal/taskmaster/db"
	"cmd184psu/unified-webapp/internal/taskmaster/models"
	"cmd184psu/unified-webapp/internal/taskmaster/worker"

	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

func newTestServer(t *testing.T, allowSudo bool, sseMax int) (*httptest.Server, *db.DB, *worker.OutputRegistry) {
	t.Helper()
	srv, d, registry, _, _, _ := newTestServerFull(t, allowSudo, sseMax)
	return srv, d, registry
}

// newTestServerFull is newTestServer plus access to the shared cancel
// registry and brake gate, for tests exercising /api/executions/{id}/cancel
// and /api/brake.
func newTestServerFull(t *testing.T, allowSudo bool, sseMax int) (*httptest.Server, *db.DB, *worker.OutputRegistry, *worker.CancelRegistry, *worker.BrakeGate, *worker.ProcessRegistry) {
	t.Helper()
	d, err := db.Open(":memory:")
	require.NoError(t, err)

	err = d.UpsertLane(&models.Lane{Name: "test", Width: 2})
	require.NoError(t, err)

	registry := worker.NewRegistry()
	cancels := worker.NewCancelRegistry()
	brake := worker.NewBrakeGate(false)
	procs := worker.NewProcessRegistry()
	board := broker.NewBroker(0)
	board.SetMaxSubscribers(sseMax)
	c := coordinator.New(d, registry, worker.NewSudoGate(allowSudo), cancels, brake, procs, sseMax, board)
	srv := httptest.NewServer(coordinator.Routes(c))
	t.Cleanup(func() {
		srv.Close()
		d.Close()
	})
	return srv, d, registry, cancels, brake, procs
}

func jsonReq(t *testing.T, method, url string, body any) *http.Request {
	t.Helper()
	var bodyReader io.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		bodyReader = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, url, bodyReader)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	return req
}

func decodeError(t *testing.T, resp *http.Response) string {
	t.Helper()
	var body map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	return body["error"]
}

// ─── Health / capabilities ──────────────────────────────────────────────────

func TestHandleHealth(t *testing.T) {
	srv, _, _ := newTestServer(t, false, 0)
	resp, err := http.Get(srv.URL + "/api/health")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestHandleCapabilities_AllowSudoTrue(t *testing.T) {
	srv, _, _ := newTestServer(t, true, 0)
	resp, err := http.Get(srv.URL + "/api/capabilities")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var body map[string]bool
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	require.True(t, body["allow_sudo"])
}

func TestHandleCapabilities_AllowSudoFalse(t *testing.T) {
	srv, _, _ := newTestServer(t, false, 0)
	resp, err := http.Get(srv.URL + "/api/capabilities")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var body map[string]bool
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	require.False(t, body["allow_sudo"])
}

func TestSetCapabilities_TogglesPersistsAndGates(t *testing.T) {
	srv, d, _ := newTestServer(t, false, 0)

	// Turn sudo on at runtime.
	resp, err := http.DefaultClient.Do(jsonReq(t, http.MethodPost, srv.URL+"/api/capabilities", map[string]bool{"allow_sudo": true}))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var body map[string]bool
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	require.True(t, body["allow_sudo"])

	// GET reflects it, and it is persisted to the DB.
	getResp, err := http.Get(srv.URL + "/api/capabilities")
	require.NoError(t, err)
	defer getResp.Body.Close()
	var getBody map[string]bool
	require.NoError(t, json.NewDecoder(getResp.Body).Decode(&getBody))
	require.True(t, getBody["allow_sudo"])

	v, ok, err := d.GetSetting(db.SettingAllowSudo)
	require.NoError(t, err)
	require.True(t, ok)
	require.True(t, db.DecodeBoolSetting(v))

	// A sudo task is now permitted where it was a 403 before.
	require.NoError(t, d.UpsertLane(&models.Lane{Name: "sg", Width: 1}))
	taskResp, err := http.DefaultClient.Do(jsonReq(t, http.MethodPost, srv.URL+"/api/tasks",
		map[string]any{"name": "sudo-task", "lane_name": "sg", "sudo": true, "command": "echo hi"}))
	require.NoError(t, err)
	defer taskResp.Body.Close()
	require.NotEqual(t, http.StatusForbidden, taskResp.StatusCode)
}

func TestSetCapabilities_MissingField(t *testing.T) {
	srv, _, _ := newTestServer(t, false, 0)
	resp, err := http.DefaultClient.Do(jsonReq(t, http.MethodPost, srv.URL+"/api/capabilities", map[string]any{}))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	require.Contains(t, decodeError(t, resp), "allow_sudo")
}

// ─── Lane (lanes route) tests ───────────────────────────────────────────────

func TestHandleListLanes(t *testing.T) {
	srv, _, _ := newTestServer(t, false, 0)
	resp, err := http.DefaultClient.Do(jsonReq(t, "GET", srv.URL+"/api/lanes", nil))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var lanes []models.LaneStatus
	json.NewDecoder(resp.Body).Decode(&lanes)
	require.Len(t, lanes, 1)
	require.Equal(t, "test", lanes[0].Name)
}

func TestHandleCreateLane(t *testing.T) {
	srv, _, _ := newTestServer(t, false, 0)
	req := jsonReq(t, "POST", srv.URL+"/api/lanes", map[string]any{
		"name": "new-lane", "width": 3,
	})
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusCreated, resp.StatusCode)
}

func TestHandlePauseLane(t *testing.T) {
	srv, _, _ := newTestServer(t, false, 0)
	req := jsonReq(t, "POST", srv.URL+"/api/lanes/test/pause", nil)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestHandleResumeLane(t *testing.T) {
	srv, _, _ := newTestServer(t, false, 0)
	req := jsonReq(t, "POST", srv.URL+"/api/lanes/test/resume", nil)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestHandleDeleteLane_WithTasks(t *testing.T) {
	srv, d, _ := newTestServer(t, false, 0)
	_, err := d.AddTask(&models.Task{Name: "t", LaneName: "test", Enabled: true, Position: 50, Command: "echo hi"})
	require.NoError(t, err)

	req := jsonReq(t, "DELETE", srv.URL+"/api/lanes/test", nil)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusConflict, resp.StatusCode)
}

func TestHandleSetLaneWidth_Valid(t *testing.T) {
	srv, d, _ := newTestServer(t, false, 0)
	req := jsonReq(t, "PUT", srv.URL+"/api/lanes/test/width", map[string]any{"width": 5})
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	lane, err := d.GetLane("test")
	require.NoError(t, err)
	require.Equal(t, 5, lane.Width)
}

func TestHandleSetLaneWidth_Invalid(t *testing.T) {
	srv, _, _ := newTestServer(t, false, 0)
	req := jsonReq(t, "PUT", srv.URL+"/api/lanes/test/width", map[string]any{"width": 0})
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestHandleSetLaneWidth_UnknownLane(t *testing.T) {
	srv, _, _ := newTestServer(t, false, 0)
	req := jsonReq(t, "PUT", srv.URL+"/api/lanes/no-such-lane/width", map[string]any{"width": 3})
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestHandleSetLaneWidth_PreservesPausedState(t *testing.T) {
	srv, d, _ := newTestServer(t, false, 0)
	require.NoError(t, d.SetLanePaused("test", true, "operator"))

	req := jsonReq(t, "PUT", srv.URL+"/api/lanes/test/width", map[string]any{"width": 4})
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	lane, err := d.GetLane("test")
	require.NoError(t, err)
	require.Equal(t, 4, lane.Width)
	require.True(t, lane.Paused, "width update must not clobber paused state")
}

func TestHandleSetLaneOrder_ReordersPositions(t *testing.T) {
	srv, d, _ := newTestServer(t, false, 0)
	_, err := d.AddTask(&models.Task{Name: "a", LaneName: "test", Enabled: true, Position: 0, Command: "echo a"})
	require.NoError(t, err)
	_, err = d.AddTask(&models.Task{Name: "b", LaneName: "test", Enabled: true, Position: 1, Command: "echo b"})
	require.NoError(t, err)
	_, err = d.AddTask(&models.Task{Name: "c", LaneName: "test", Enabled: true, Position: 2, Command: "echo c"})
	require.NoError(t, err)

	req := jsonReq(t, "PUT", srv.URL+"/api/lanes/test/order", map[string]any{"order": []string{"c", "a", "b"}})
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	ta, _ := d.GetTask("a")
	tb, _ := d.GetTask("b")
	tc, _ := d.GetTask("c")
	require.Equal(t, 1, ta.Position)
	require.Equal(t, 2, tb.Position)
	require.Equal(t, 0, tc.Position)
}

func TestHandleSetLaneOrder_IgnoresNamesOutsideLane(t *testing.T) {
	srv, d, _ := newTestServer(t, false, 0)
	require.NoError(t, d.UpsertLane(&models.Lane{Name: "other", Width: 1}))
	_, err := d.AddTask(&models.Task{Name: "in-lane", LaneName: "test", Enabled: true, Position: 0, Command: "echo hi"})
	require.NoError(t, err)
	_, err = d.AddTask(&models.Task{Name: "elsewhere", LaneName: "other", Enabled: true, Position: 0, Command: "echo hi"})
	require.NoError(t, err)

	req := jsonReq(t, "PUT", srv.URL+"/api/lanes/test/order", map[string]any{"order": []string{"in-lane", "elsewhere"}})
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	elsewhere, _ := d.GetTask("elsewhere")
	require.Equal(t, 0, elsewhere.Position, "task in a different lane must not be repositioned")
}

func TestHandleSetLaneOrder_UnknownLane(t *testing.T) {
	srv, _, _ := newTestServer(t, false, 0)
	req := jsonReq(t, "PUT", srv.URL+"/api/lanes/no-such-lane/order", map[string]any{"order": []string{"a"}})
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
}

// ─── Task tests ───────────────────────────────────────────────────────────────

func TestHandleAddTask_Valid(t *testing.T) {
	srv, _, _ := newTestServer(t, false, 0)
	req := jsonReq(t, "POST", srv.URL+"/api/tasks", map[string]any{
		"name":      "my-task",
		"lane_name": "test",
		"command":   "echo hi",
		"enabled":   true,
	})
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusCreated, resp.StatusCode)
}

func TestHandleAddTask_UnknownLane(t *testing.T) {
	srv, _, _ := newTestServer(t, false, 0)
	req := jsonReq(t, "POST", srv.URL+"/api/tasks", map[string]any{
		"name": "bad", "lane_name": "no-such-lane", "command": "echo hi",
	})
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestHandleAddTask_SudoDisallowed(t *testing.T) {
	srv, _, _ := newTestServer(t, false, 0)
	req := jsonReq(t, "POST", srv.URL+"/api/tasks", map[string]any{
		"name": "sudo-task", "lane_name": "test", "command": "echo hi", "sudo": true,
	})
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusForbidden, resp.StatusCode)
	require.Contains(t, decodeError(t, resp), "allow_sudo")
}

func TestHandleAddTask_SudoAllowed(t *testing.T) {
	srv, _, _ := newTestServer(t, true, 0)
	req := jsonReq(t, "POST", srv.URL+"/api/tasks", map[string]any{
		"name": "sudo-task", "lane_name": "test", "command": "echo hi", "sudo": true,
	})
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusCreated, resp.StatusCode)
}

func TestHandleUpdateTask_SudoDisallowed(t *testing.T) {
	srv, d, _ := newTestServer(t, false, 0)
	_, err := d.AddTask(&models.Task{Name: "t3", LaneName: "test", Enabled: true, Position: 50, Command: "echo hi"})
	require.NoError(t, err)

	req := jsonReq(t, "PUT", srv.URL+"/api/tasks/t3", map[string]any{"sudo": true})
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestHandleUpdateTask_WrongTypedField(t *testing.T) {
	srv, d, _ := newTestServer(t, false, 0)
	_, err := d.AddTask(&models.Task{Name: "t4", LaneName: "test", Enabled: true, Position: 50, Command: "echo hi"})
	require.NoError(t, err)

	req := jsonReq(t, "PUT", srv.URL+"/api/tasks/t4", map[string]any{"sudo": "yes"})
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	msg := decodeError(t, resp)
	require.Contains(t, msg, `"sudo"`)
	require.Contains(t, msg, "must be a")
}

func TestHandleUpdateTask_Command(t *testing.T) {
	srv, d, _ := newTestServer(t, false, 0)
	_, err := d.AddTask(&models.Task{Name: "t5", LaneName: "test", Enabled: true, Position: 50, Command: "echo hi"})
	require.NoError(t, err)

	req := jsonReq(t, "PUT", srv.URL+"/api/tasks/t5", map[string]any{"command": "echo bye"})
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	got, _ := d.GetTask("t5")
	require.Equal(t, "echo bye", got.Command)
}

func TestHandleDeleteTask(t *testing.T) {
	srv, d, _ := newTestServer(t, false, 0)
	_, err := d.AddTask(&models.Task{Name: "del-me", LaneName: "test", Enabled: true, Position: 50, Command: "echo hi"})
	require.NoError(t, err)

	req := jsonReq(t, "DELETE", srv.URL+"/api/tasks/del-me", nil)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusNoContent, resp.StatusCode)
}

func TestHandleUpNext(t *testing.T) {
	srv, d, _ := newTestServer(t, false, 0)
	_, err := d.AddTask(&models.Task{Name: "enq", LaneName: "test", Enabled: true, Position: 50, Command: "echo hi"})
	require.NoError(t, err)

	req := jsonReq(t, "POST", srv.URL+"/api/tasks/enq/up-next", nil)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	var result map[string]int64
	json.NewDecoder(resp.Body).Decode(&result)
	require.Positive(t, result["execution_id"])
}

func TestHandleMoveTask_Valid(t *testing.T) {
	srv, d, _ := newTestServer(t, false, 0)
	require.NoError(t, d.UpsertLane(&models.Lane{Name: "other", Width: 1}))
	_, err := d.AddTask(&models.Task{Name: "movable", LaneName: "test", Enabled: true, Position: 50, Command: "echo hi"})
	require.NoError(t, err)

	req := jsonReq(t, "POST", srv.URL+"/api/tasks/movable/move", map[string]any{"lane_name": "other"})
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	task, err := d.GetTask("movable")
	require.NoError(t, err)
	require.Equal(t, "other", task.LaneName)
}

func TestHandleMoveTask_SetsDestLanePosition(t *testing.T) {
	srv, d, _ := newTestServer(t, false, 0)
	require.NoError(t, d.UpsertLane(&models.Lane{Name: "other", Width: 1}))
	_, err := d.AddTask(&models.Task{Name: "existing-1", LaneName: "other", Enabled: true, Position: 0, Command: "echo hi"})
	require.NoError(t, err)
	_, err = d.AddTask(&models.Task{Name: "existing-2", LaneName: "other", Enabled: true, Position: 3, Command: "echo hi"})
	require.NoError(t, err)
	_, err = d.AddTask(&models.Task{Name: "movable-pos", LaneName: "test", Enabled: true, Position: 50, Command: "echo hi"})
	require.NoError(t, err)

	req := jsonReq(t, "POST", srv.URL+"/api/tasks/movable-pos/move", map[string]any{"lane_name": "other"})
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	task, err := d.GetTask("movable-pos")
	require.NoError(t, err)
	require.Equal(t, "other", task.LaneName)
	require.Equal(t, 4, task.Position, "moved task's position should be at the end of the destination lane")
}

func TestHandleListTasks_LaneQueryParamFilters(t *testing.T) {
	srv, d, _ := newTestServer(t, false, 0)
	require.NoError(t, d.UpsertLane(&models.Lane{Name: "other", Width: 1}))
	_, err := d.AddTask(&models.Task{Name: "in-test", LaneName: "test", Enabled: true, Position: 0, Command: "echo hi"})
	require.NoError(t, err)
	_, err = d.AddTask(&models.Task{Name: "in-other", LaneName: "other", Enabled: true, Position: 0, Command: "echo hi"})
	require.NoError(t, err)

	resp, err := http.Get(srv.URL + "/api/tasks?lane=other")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var tasks []models.Task
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&tasks))
	require.Len(t, tasks, 1)
	require.Equal(t, "in-other", tasks[0].Name)
}

func TestHandleMetrics_LaneQueryParamFilters(t *testing.T) {
	srv, d, _ := newTestServer(t, false, 0)
	require.NoError(t, d.UpsertLane(&models.Lane{Name: "other", Width: 1}))
	testTaskID, err := d.AddTask(&models.Task{Name: "m-test", LaneName: "test", Enabled: true, Position: 0, Command: "echo hi"})
	require.NoError(t, err)
	otherTaskID, err := d.AddTask(&models.Task{Name: "m-other", LaneName: "other", Enabled: true, Position: 0, Command: "echo hi"})
	require.NoError(t, err)

	execID, err := d.CreateExecution(testTaskID, "w", time.Now())
	require.NoError(t, err)
	require.NoError(t, d.RecordMetric(testTaskID, execID, "success", 10, 0))
	execID2, err := d.CreateExecution(otherTaskID, "w", time.Now())
	require.NoError(t, err)
	require.NoError(t, d.RecordMetric(otherTaskID, execID2, "success", 10, 0))

	resp, err := http.Get(srv.URL + "/api/metrics?lane=other")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var summaries []models.MetricSummary
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&summaries))
	require.Len(t, summaries, 1)
	require.Equal(t, "m-other", summaries[0].TaskName)
}

func TestHandleMoveTask_UnknownLane(t *testing.T) {
	srv, d, _ := newTestServer(t, false, 0)
	_, err := d.AddTask(&models.Task{Name: "movable2", LaneName: "test", Enabled: true, Position: 50, Command: "echo hi"})
	require.NoError(t, err)

	req := jsonReq(t, "POST", srv.URL+"/api/tasks/movable2/move", map[string]any{"lane_name": "no-such-lane"})
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)

	task, err := d.GetTask("movable2")
	require.NoError(t, err)
	require.Equal(t, "test", task.LaneName, "lane must not change when target is unknown")
}

func TestHandleMoveTask_UnknownTask(t *testing.T) {
	srv, _, _ := newTestServer(t, false, 0)
	req := jsonReq(t, "POST", srv.URL+"/api/tasks/no-such-task/move", map[string]any{"lane_name": "test"})
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestHandlePauseResumeTask(t *testing.T) {
	srv, d, _ := newTestServer(t, false, 0)
	_, err := d.AddTask(&models.Task{Name: "pausable", LaneName: "test", Enabled: true, Position: 50, Command: "echo hi"})
	require.NoError(t, err)

	req := jsonReq(t, "POST", srv.URL+"/api/tasks/pausable/pause", nil)
	resp, _ := http.DefaultClient.Do(req)
	resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	task, _ := d.GetTask("pausable")
	require.True(t, task.Paused)

	req = jsonReq(t, "POST", srv.URL+"/api/tasks/pausable/resume", nil)
	resp, _ = http.DefaultClient.Do(req)
	resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	task, _ = d.GetTask("pausable")
	require.False(t, task.Paused)
}

// ─── Metrics/Executions ───────────────────────────────────────────────────────

func TestHandleMetrics(t *testing.T) {
	srv, _, _ := newTestServer(t, false, 0)
	resp, err := http.DefaultClient.Do(jsonReq(t, "GET", srv.URL+"/api/metrics", nil))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestHandleListExecutions(t *testing.T) {
	srv, _, _ := newTestServer(t, false, 0)
	resp, err := http.DefaultClient.Do(jsonReq(t, "GET", srv.URL+"/api/executions", nil))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
}

// ─── SSE output tests ──────────────────────────────────────────────────────────

func TestHandleExecutionOutput_NotFound(t *testing.T) {
	srv, _, _ := newTestServer(t, false, 0)
	resp, err := http.DefaultClient.Do(jsonReq(t, "GET", srv.URL+"/api/executions/99999/output", nil))
	require.NoError(t, err)
	defer resp.Body.Close()
	// not in registry, not in DB → 404
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestHandleExecutionOutput_CompletedExecution(t *testing.T) {
	srv, d, _ := newTestServer(t, false, 0)

	task := &models.Task{Name: "t", LaneName: "test", Enabled: true, Position: 50, Command: "echo hi"}
	id, _ := d.AddTask(task)
	execID, _ := d.CreateExecution(id, "w", time.Now())
	d.StartExecution(execID, "w")
	d.FinishExecution(execID, "success", nil, 100, 0)

	resp, err := http.DefaultClient.Do(jsonReq(t, "GET", srv.URL+"/api/executions/"+itoa(execID)+"/output", nil))
	require.NoError(t, err)
	defer resp.Body.Close()
	// exec is in DB but not in registry → returns status event
	require.Equal(t, http.StatusOK, resp.StatusCode)
	body, _ := io.ReadAll(resp.Body)
	require.Contains(t, string(body), "success")
}

// TestHandleExecutionOutput_PendingBecomesRegistered is the regression test
// for the real bug behind "View output shows nothing": a client opening the
// output stream for an execution that hasn't started yet ("pending" — e.g.
// the instant after enqueueing, before the worker's next poll) used to get
// one "status: pending" event and an immediate close, with nothing telling
// it to come back once real output existed. The fix keeps that SAME
// connection open and waits; this test proves it without ever reconnecting.
func TestHandleExecutionOutput_PendingBecomesRegistered(t *testing.T) {
	srv, d, registry := newTestServer(t, false, 0)

	task := &models.Task{Name: "pending-output-task", LaneName: "test", Enabled: true, Position: 50, Command: "echo hi"}
	taskID, err := d.AddTask(task)
	require.NoError(t, err)
	execID, err := d.CreateExecution(taskID, "w", time.Now())
	require.NoError(t, err)
	// Deliberately NOT calling d.StartExecution / registry.Register yet —
	// this execution is genuinely still "pending" when the client connects.

	received := make(chan string, 1)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		resp, getErr := http.Get(srv.URL + "/api/executions/" + itoa(execID) + "/output")
		if getErr != nil {
			return
		}
		defer resp.Body.Close()
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "data:") && strings.Contains(line, "hello-from-worker") {
				received <- line
				return
			}
		}
	}()

	// Give the handler a moment to reach its wait loop, then simulate the
	// worker picking the task up moments later — exactly what a real 5s
	// poll cycle does.
	time.Sleep(100 * time.Millisecond)
	require.NoError(t, d.StartExecution(execID, "w"))
	stdout, stderr := registry.Register(execID)
	stdout.Write([]byte("hello-from-worker\n"))

	select {
	case <-received:
	case <-time.After(5 * time.Second):
		t.Fatal("client never received output after the execution was registered — the original connection was not kept open")
	}

	stdout.MarkDone()
	stderr.MarkDone()
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the subscriber goroutine to finish")
	}
}

func TestHandleExecutionOutput_SSECap(t *testing.T) {
	srv, _, registry := newTestServer(t, false, 1)

	const execID int64 = 555
	stdout, stderr := registry.Register(execID)
	stdout.Write([]byte("hello\n"))

	started := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		resp, err := http.Get(srv.URL + "/api/executions/" + itoa(execID) + "/output")
		if err != nil {
			return
		}
		defer resp.Body.Close()
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			if strings.HasPrefix(scanner.Text(), "event:") {
				close(started)
				break
			}
		}
		// Drain until the server closes the stream (after MarkDone below).
		for scanner.Scan() {
		}
	}()

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for first SSE subscriber to start")
	}

	// Second concurrent subscriber must be rejected: cap is 1.
	resp2, err := http.Get(srv.URL + "/api/executions/" + itoa(execID) + "/output")
	require.NoError(t, err)
	defer resp2.Body.Close()
	require.Equal(t, http.StatusServiceUnavailable, resp2.StatusCode)

	// Release the first subscriber's slot and let its goroutine drain before
	// the test (and the eventual goleak check) proceeds.
	stdout.MarkDone()
	stderr.MarkDone()

	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for first SSE subscriber to finish")
	}
}

func itoa(n int64) string {
	return strconv.FormatInt(n, 10)
}

// ─── Cancel / brake tests ────────────────────────────────────────────────────

type mockExec struct {
	fn func(ctx context.Context, task *models.Task, stdout, stderr io.Writer) error
}

func (m *mockExec) Execute(ctx context.Context, task *models.Task, stdout, stderr io.Writer, onStart func(pid int)) error {
	if onStart != nil {
		onStart(0)
	}
	return m.fn(ctx, task, stdout, stderr)
}

func TestHandleCancelExecution_NotRunning(t *testing.T) {
	srv, _, _, _, _, _ := newTestServerFull(t, false, 0)
	resp, err := http.DefaultClient.Do(jsonReq(t, "POST", srv.URL+"/api/executions/99999/cancel", nil))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
}

// TestHandleCancelExecution_ExistsButFinished_ReturnsOKNotRunning covers the
// benign race: canceling an execution that finished naturally before this
// request landed is a normal outcome, not a client error — a real execution
// ID gets 200 {"status":"not_running"}, distinct from the 404 above for an
// ID that never existed at all.
func TestHandleCancelExecution_ExistsButFinished_ReturnsOKNotRunning(t *testing.T) {
	srv, d, _, _, _, _ := newTestServerFull(t, false, 0)
	taskID, err := d.AddTask(&models.Task{Name: "p5", LaneName: "test", Enabled: true, Position: 50, Command: "echo hi"})
	require.NoError(t, err)
	execID, err := d.CreateExecution(taskID, "w", time.Now())
	require.NoError(t, err)
	require.NoError(t, d.FinishExecution(execID, "success", nil, 5, 0))

	resp, err := http.DefaultClient.Do(jsonReq(t, "POST", fmt.Sprintf("%s/api/executions/%d/cancel", srv.URL, execID), nil))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var body map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	require.Equal(t, "not_running", body["status"])
}

func TestHandleCancelExecution_Running(t *testing.T) {
	srv, _, _, cancels, _, _ := newTestServerFull(t, false, 0)
	// Simulate a running execution by registering a cancel func directly,
	// as runTask would.
	cancels.Register(42, func() {})

	resp, err := http.DefaultClient.Do(jsonReq(t, "POST", srv.URL+"/api/executions/42/cancel", nil))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.True(t, cancels.WasCanceled(42))
}

func TestBrake_GetDefaultDisengaged(t *testing.T) {
	srv, _, _, _, _, _ := newTestServerFull(t, false, 0)
	resp, err := http.Get(srv.URL + "/api/brake")
	require.NoError(t, err)
	defer resp.Body.Close()
	var body map[string]bool
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	require.False(t, body["engaged"])
}

// TestBrake_EngageCancelsRunningAndPausesLanes_ReleaseRestores drives a real
// worker (long-running mock command) alongside the coordinator sharing the
// same DB, registry, cancel registry and brake gate. Engaging the brake must
// pause the lane, cancel the running execution (recorded "canceled"), and
// persist the flag; releasing must restore only the lanes the brake paused.
func TestBrake_EngageCancelsRunningAndPausesLanes_ReleaseRestores(t *testing.T) {
	d, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { d.Close() })
	require.NoError(t, d.UpsertLane(&models.Lane{Name: "test", Width: 2}))
	_, err = d.AddTask(&models.Task{Name: "long-runner", LaneName: "test", Enabled: true, Position: 50, Command: "sleep 300"})
	require.NoError(t, err)

	outReg := worker.NewRegistry()
	cancels := worker.NewCancelRegistry()
	brake := worker.NewBrakeGate(false)

	started := make(chan struct{})
	exec := &mockExec{fn: func(ctx context.Context, task *models.Task, stdout, stderr io.Writer) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}}
	board := broker.NewBroker(0)
	procs := worker.NewProcessRegistry()
	w := worker.NewWithExecutor(d, outReg, "w", exec, cancels, brake, procs, board)
	worker.SetPollIntervalForTest(20 * time.Millisecond)
	wctx, wcancel := context.WithCancel(context.Background())
	go w.Start(wctx)
	defer func() {
		wcancel()
		w.Wait()
	}()

	c := coordinator.New(d, outReg, worker.NewSudoGate(false), cancels, brake, procs, 0, board)
	srv := httptest.NewServer(coordinator.Routes(c))
	defer srv.Close()

	select {
	case <-started:
	case <-time.After(15 * time.Second):
		t.Fatal("long-runner did not start within timeout")
	}

	// Engage the brake.
	resp, err := http.DefaultClient.Do(jsonReq(t, "POST", srv.URL+"/api/brake", nil))
	require.NoError(t, err)
	var engageBody map[string]bool
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&engageBody))
	resp.Body.Close()
	require.True(t, engageBody["engaged"])
	require.True(t, brake.Engaged())

	lane, err := d.GetLane("test")
	require.NoError(t, err)
	require.True(t, lane.Paused, "brake should pause the lane")

	require.Eventually(t, func() bool {
		execs, err := d.ListExecutions("long-runner", 1)
		return err == nil && len(execs) > 0 && execs[0].Status == "canceled"
	}, 5*time.Second, 25*time.Millisecond, "running execution should be canceled by the brake")

	v, ok, err := d.GetSetting(db.SettingBrakeEngaged)
	require.NoError(t, err)
	require.True(t, ok)
	require.True(t, db.DecodeBoolSetting(v))

	// Release the brake.
	req := jsonReq(t, "DELETE", srv.URL+"/api/brake", nil)
	resp, err = http.DefaultClient.Do(req)
	require.NoError(t, err)
	var releaseBody map[string]bool
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&releaseBody))
	resp.Body.Close()
	require.False(t, releaseBody["engaged"])
	require.False(t, brake.Engaged())

	lane, err = d.GetLane("test")
	require.NoError(t, err)
	require.False(t, lane.Paused, "release should restore the lane the brake paused")

	pausedLanes, err := d.GetBrakePausedLanes()
	require.NoError(t, err)
	require.Empty(t, pausedLanes)
}

// TestBrake_ReleaseDoesNotUnpauseIndependentlyPausedLane verifies release
// restores only the lanes the brake itself paused, leaving a lane that was
// already paused before the brake engaged untouched.
func TestBrake_ReleaseDoesNotUnpauseIndependentlyPausedLane(t *testing.T) {
	srv, d, _, _, brake, _ := newTestServerFull(t, false, 0)
	require.NoError(t, d.SetLanePaused("test", true, "operator"))

	resp, err := http.DefaultClient.Do(jsonReq(t, "POST", srv.URL+"/api/brake", nil))
	require.NoError(t, err)
	resp.Body.Close()
	require.True(t, brake.Engaged())

	pausedLanes, err := d.GetBrakePausedLanes()
	require.NoError(t, err)
	require.Empty(t, pausedLanes, "a lane already paused before the brake engaged should not be recorded")

	resp, err = http.DefaultClient.Do(jsonReq(t, "DELETE", srv.URL+"/api/brake", nil))
	require.NoError(t, err)
	resp.Body.Close()

	lane, err := d.GetLane("test")
	require.NoError(t, err)
	require.True(t, lane.Paused, "independently paused lane must stay paused after release")
}

// TestBrake_DoubleEngageIsIdempotent_ReleaseStillRestores verifies that
// engaging an already-engaged brake does not recompute (and overwrite) the
// "paused by brake" restore set. Without the idempotency guard, a second
// engage call would see every lane already paused (by the first engage),
// record an empty set, and a later release would fail to restore the
// originally-paused lane.
func TestBrake_DoubleEngageIsIdempotent_ReleaseStillRestores(t *testing.T) {
	srv, d, _, _, brake, _ := newTestServerFull(t, false, 0)
	require.NoError(t, d.UpsertLane(&models.Lane{Name: "independent", Width: 1}))
	require.NoError(t, d.SetLanePaused("independent", true, "operator"))

	// First engage: pauses "test" (not yet paused) and records it as
	// brake-paused; "independent" is already paused so it's left alone.
	resp, err := http.DefaultClient.Do(jsonReq(t, "POST", srv.URL+"/api/brake", nil))
	require.NoError(t, err)
	resp.Body.Close()
	require.True(t, brake.Engaged())

	pausedLanes, err := d.GetBrakePausedLanes()
	require.NoError(t, err)
	require.Equal(t, []string{"test"}, pausedLanes)

	// Second engage while already engaged must be a no-op: it must not
	// recompute pausedByBrake (every lane is now paused) and overwrite the
	// restore set with an empty one.
	resp, err = http.DefaultClient.Do(jsonReq(t, "POST", srv.URL+"/api/brake", nil))
	require.NoError(t, err)
	var body map[string]bool
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	resp.Body.Close()
	require.True(t, body["engaged"])
	require.True(t, brake.Engaged())

	pausedLanes, err = d.GetBrakePausedLanes()
	require.NoError(t, err)
	require.Equal(t, []string{"test"}, pausedLanes, "restore set must survive a redundant engage call")

	// Release must restore "test" (the lane the brake originally paused)
	// while leaving "independent" (paused before the brake engaged) paused.
	resp, err = http.DefaultClient.Do(jsonReq(t, "DELETE", srv.URL+"/api/brake", nil))
	require.NoError(t, err)
	resp.Body.Close()
	require.False(t, brake.Engaged())

	lane, err := d.GetLane("test")
	require.NoError(t, err)
	require.False(t, lane.Paused, "lane the brake paused must be restored despite the double engage")

	independent, err := d.GetLane("independent")
	require.NoError(t, err)
	require.True(t, independent.Paused, "independently paused lane must remain paused")
}

// TestMetrics_CountsCanceledSeparately verifies GetMetrics/handleMetrics
// tallies "canceled" executions into canceled_count, not failed_count.
func TestMetrics_CountsCanceledSeparately(t *testing.T) {
	srv, d, _, _, _, _ := newTestServerFull(t, false, 0)
	taskID, err := d.AddTask(&models.Task{Name: "m", LaneName: "test", Enabled: true, Position: 50, Command: "echo hi"})
	require.NoError(t, err)

	execID, err := d.CreateExecution(taskID, "w", time.Now())
	require.NoError(t, err)
	require.NoError(t, d.StartExecution(execID, "w"))
	require.NoError(t, d.FinishExecution(execID, "canceled", nil, 10, 0))
	require.NoError(t, d.RecordMetric(taskID, execID, "canceled", 10, 0))

	resp, err := http.Get(srv.URL + "/api/metrics")
	require.NoError(t, err)
	defer resp.Body.Close()
	var summaries []models.MetricSummary
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&summaries))
	require.Len(t, summaries, 1)
	require.Equal(t, 1, summaries[0].CanceledCount)
	require.Equal(t, 0, summaries[0].FailedCount)
}
