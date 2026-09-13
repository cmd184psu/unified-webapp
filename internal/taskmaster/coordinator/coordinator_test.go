package coordinator_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

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
	d, err := db.Open(":memory:")
	require.NoError(t, err)

	err = d.UpsertGroup(&models.Group{Name: "test", PoolLimit: 2, AllowedTypes: []string{}})
	require.NoError(t, err)

	registry := worker.NewRegistry()
	c := coordinator.New(d, registry, worker.NewSudoGate(allowSudo), sseMax)
	srv := httptest.NewServer(coordinator.Routes(c))
	t.Cleanup(func() {
		srv.Close()
		d.Close()
	})
	return srv, d, registry
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
	require.NoError(t, d.UpsertGroup(&models.Group{Name: "sg", PoolLimit: 1, AllowedTypes: []string{}}))
	taskResp, err := http.DefaultClient.Do(jsonReq(t, http.MethodPost, srv.URL+"/api/tasks",
		map[string]any{"name": "sudo-task", "group_name": "sg", "task_type": "shell", "sudo": true, "args": `{"shell":"echo hi"}`}))
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

// ─── Group tests ─────────────────────────────────────────────────────────────

func TestHandleListGroups(t *testing.T) {
	srv, _, _ := newTestServer(t, false, 0)
	resp, err := http.DefaultClient.Do(jsonReq(t, "GET", srv.URL+"/api/groups", nil))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var groups []models.GroupStatus
	json.NewDecoder(resp.Body).Decode(&groups)
	require.Len(t, groups, 1)
	require.Equal(t, "test", groups[0].Name)
}

func TestHandleCreateGroup(t *testing.T) {
	srv, _, _ := newTestServer(t, false, 0)
	req := jsonReq(t, "POST", srv.URL+"/api/groups", map[string]any{
		"name": "new-group", "pool_limit": 3,
	})
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusCreated, resp.StatusCode)
}

func TestHandlePauseGroup(t *testing.T) {
	srv, _, _ := newTestServer(t, false, 0)
	req := jsonReq(t, "POST", srv.URL+"/api/groups/test/pause", nil)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestHandleResumeGroup(t *testing.T) {
	srv, _, _ := newTestServer(t, false, 0)
	req := jsonReq(t, "POST", srv.URL+"/api/groups/test/resume", nil)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestHandleDeleteGroup_WithTasks(t *testing.T) {
	srv, d, _ := newTestServer(t, false, 0)
	_, err := d.AddTask(&models.Task{Name: "t", GroupName: "test", TaskType: "shell", Enabled: true, Priority: 50, Args: "{}"})
	require.NoError(t, err)

	req := jsonReq(t, "DELETE", srv.URL+"/api/groups/test", nil)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusConflict, resp.StatusCode)
}

// ─── Task tests ───────────────────────────────────────────────────────────────

func TestHandleAddTask_Valid(t *testing.T) {
	srv, _, _ := newTestServer(t, false, 0)
	req := jsonReq(t, "POST", srv.URL+"/api/tasks", map[string]any{
		"name":       "my-task",
		"group_name": "test",
		"task_type":  "shell",
		"args":       `{"shell":"echo hi"}`,
		"enabled":    true,
	})
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusCreated, resp.StatusCode)
}

func TestHandleAddTask_UnknownGroup(t *testing.T) {
	srv, _, _ := newTestServer(t, false, 0)
	req := jsonReq(t, "POST", srv.URL+"/api/tasks", map[string]any{
		"name": "bad", "group_name": "no-such-group", "task_type": "shell",
	})
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestHandleAddTask_SudoDisallowed(t *testing.T) {
	srv, _, _ := newTestServer(t, false, 0)
	req := jsonReq(t, "POST", srv.URL+"/api/tasks", map[string]any{
		"name": "sudo-task", "group_name": "test", "task_type": "shell", "sudo": true,
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
		"name": "sudo-task", "group_name": "test", "task_type": "shell", "sudo": true,
	})
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusCreated, resp.StatusCode)
}

func TestHandleAddTask_DisallowedType(t *testing.T) {
	srv, d, _ := newTestServer(t, false, 0)
	require.NoError(t, d.UpsertGroup(&models.Group{Name: "restricted", PoolLimit: 1, AllowedTypes: []string{"shell"}}))

	req := jsonReq(t, "POST", srv.URL+"/api/tasks", map[string]any{
		"name": "exec-task", "group_name": "restricted", "task_type": "exec",
	})
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	msg := decodeError(t, resp)
	require.Contains(t, msg, "exec")
	require.Contains(t, msg, "restricted")
}

func TestHandleUpdateTask_DisallowedType(t *testing.T) {
	srv, d, _ := newTestServer(t, false, 0)
	require.NoError(t, d.UpsertGroup(&models.Group{Name: "restricted", PoolLimit: 1, AllowedTypes: []string{"shell"}}))
	_, err := d.AddTask(&models.Task{Name: "t1", GroupName: "restricted", TaskType: "shell", Enabled: true, Priority: 50, Args: "{}"})
	require.NoError(t, err)

	req := jsonReq(t, "PUT", srv.URL+"/api/tasks/t1", map[string]any{"task_type": "exec"})
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestHandleUpdateTask_MoveToRestrictedGroup(t *testing.T) {
	srv, d, _ := newTestServer(t, false, 0)
	require.NoError(t, d.UpsertGroup(&models.Group{Name: "restricted", PoolLimit: 1, AllowedTypes: []string{"exec"}}))
	_, err := d.AddTask(&models.Task{Name: "t2", GroupName: "test", TaskType: "shell", Enabled: true, Priority: 50, Args: "{}"})
	require.NoError(t, err)

	req := jsonReq(t, "PUT", srv.URL+"/api/tasks/t2", map[string]any{"group_name": "restricted"})
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestHandleUpdateTask_SudoDisallowed(t *testing.T) {
	srv, d, _ := newTestServer(t, false, 0)
	_, err := d.AddTask(&models.Task{Name: "t3", GroupName: "test", TaskType: "shell", Enabled: true, Priority: 50, Args: "{}"})
	require.NoError(t, err)

	req := jsonReq(t, "PUT", srv.URL+"/api/tasks/t3", map[string]any{"sudo": true})
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestHandleUpdateTask_WrongTypedField(t *testing.T) {
	srv, d, _ := newTestServer(t, false, 0)
	_, err := d.AddTask(&models.Task{Name: "t4", GroupName: "test", TaskType: "shell", Enabled: true, Priority: 50, Args: "{}"})
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

func TestHandleDeleteTask(t *testing.T) {
	srv, d, _ := newTestServer(t, false, 0)
	_, err := d.AddTask(&models.Task{Name: "del-me", GroupName: "test", TaskType: "shell", Enabled: true, Priority: 50, Args: "{}"})
	require.NoError(t, err)

	req := jsonReq(t, "DELETE", srv.URL+"/api/tasks/del-me", nil)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusNoContent, resp.StatusCode)
}

func TestHandleEnqueueTask(t *testing.T) {
	srv, d, _ := newTestServer(t, false, 0)
	_, err := d.AddTask(&models.Task{Name: "enq", GroupName: "test", TaskType: "shell", Enabled: true, Priority: 50, Args: "{}"})
	require.NoError(t, err)

	req := jsonReq(t, "POST", srv.URL+"/api/tasks/enq/enqueue", nil)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	var result map[string]int64
	json.NewDecoder(resp.Body).Decode(&result)
	require.Positive(t, result["execution_id"])
}

func TestHandlePauseResumeTask(t *testing.T) {
	srv, d, _ := newTestServer(t, false, 0)
	_, err := d.AddTask(&models.Task{Name: "pausable", GroupName: "test", TaskType: "shell", Enabled: true, Priority: 50, Args: "{}"})
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

	task := &models.Task{Name: "t", GroupName: "test", TaskType: "shell", Enabled: true, Priority: 50, Args: "{}"}
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
