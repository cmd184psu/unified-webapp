package taskmaster_test

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"cmd184psu/unified-webapp/internal/platform/config"
	"cmd184psu/unified-webapp/internal/taskmaster"
	"cmd184psu/unified-webapp/internal/taskmaster/worker"

	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	worker.SetPollIntervalForTest(50 * time.Millisecond)
	goleak.VerifyTestMain(m)
}

func writeIndexHTML(t *testing.T, dir string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "index.html"), []byte("index"), 0o644))
}

func TestBuild_FullCycle(t *testing.T) {
	staticDir := t.TempDir()
	writeIndexHTML(t, staticDir)

	cfg := config.TaskmasterConfig{
		StaticDir: staticDir,
		DBPath:    filepath.Join(t.TempDir(), "taskmaster.db"),
		Groups: []config.TaskmasterGroup{
			{Name: "seeded", PoolLimit: 2, AllowedTypes: []string{"shell"}},
		},
		AllowSudo:         false,
		SSEMaxSubscribers: 0,
	}

	h, err := taskmaster.Build(cfg)
	require.NoError(t, err)
	closer, ok := h.(interface{ Close() error })
	require.True(t, ok, "Build handler must implement io.Closer")
	defer func() {
		require.NoError(t, closer.Close())
	}()

	srv := httptest.NewServer(h)
	defer srv.Close()

	// GET /api/groups shows the seeded group.
	resp, err := http.Get(srv.URL + "/api/groups")
	require.NoError(t, err)
	var groups []map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&groups))
	resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	found := false
	for _, g := range groups {
		if g["name"] == "seeded" {
			found = true
		}
	}
	require.True(t, found, "seeded group missing from /api/groups: %+v", groups)

	// GET /api/capabilities reflects allow_sudo from config.
	resp, err = http.Get(srv.URL + "/api/capabilities")
	require.NoError(t, err)
	var caps map[string]bool
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&caps))
	resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.False(t, caps["allow_sudo"])

	// POST a trivial shell task, enabled so the worker's poll loop picks it
	// up without an explicit enqueue.
	taskBody := `{"name":"echo-task","group_name":"seeded","task_type":"shell","enabled":true,"args":"{\"shell\":\"echo hi\"}"}`
	resp, err = http.Post(srv.URL+"/api/tasks", "application/json", strings.NewReader(taskBody))
	require.NoError(t, err)
	resp.Body.Close()
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	// Poll /api/executions until the task completes.
	var execID float64
	require.Eventually(t, func() bool {
		resp, err := http.Get(srv.URL + "/api/executions")
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		var execs []map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&execs); err != nil {
			return false
		}
		for _, e := range execs {
			if e["status"] == "success" {
				execID = e["id"].(float64)
				return true
			}
		}
		return false
	}, 5*time.Second, 25*time.Millisecond, "task never reached success")

	// Stream the execution's output over SSE until "done".
	resp, err = http.Get(srv.URL + "/api/executions/" + strconv.FormatInt(int64(execID), 10) + "/output")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	scanner := bufio.NewScanner(resp.Body)
	sawDone := false
	for scanner.Scan() {
		if strings.Contains(scanner.Text(), "event: done") || strings.Contains(scanner.Text(), "event: status") {
			sawDone = true
			break
		}
	}
	require.True(t, sawDone, "SSE stream never reported completion")
}

func TestBuild_ErrorPath_UnwritableDBPath(t *testing.T) {
	// A regular file used as the parent "directory" makes MkdirAll fail
	// before Build ever opens the DB.
	blocker := filepath.Join(t.TempDir(), "not-a-dir")
	require.NoError(t, os.WriteFile(blocker, []byte("x"), 0o644))

	cfg := config.TaskmasterConfig{
		StaticDir: t.TempDir(),
		DBPath:    filepath.Join(blocker, "sub", "taskmaster.db"),
	}

	h, err := taskmaster.Build(cfg)
	require.Error(t, err)
	require.Nil(t, h)
}
