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
	"cmd184psu/unified-webapp/internal/taskmaster/db"
	"cmd184psu/unified-webapp/internal/taskmaster/models"
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

// TestBuild_ReconcilesOrphanedRunningExecutionOnBoot is the integration-level
// regression test for a real production bug: an execution left "running" by
// a previous, non-gracefully-killed process (kill -9, crash, OOM — anything
// that skips Close()) used to stay "running" in the DB forever, since
// nothing ever reconciled it on the next boot. The UI would show it as
// perpetually active, and pause/cancel against it would (correctly, but
// confusingly) report "not_running", since no live process was ever
// registered for it in THIS process's registries. This proves Build() fixes
// that by the time the module is serving requests.
func TestBuild_ReconcilesOrphanedRunningExecutionOnBoot(t *testing.T) {
	staticDir := t.TempDir()
	writeIndexHTML(t, staticDir)
	dbPath := filepath.Join(t.TempDir(), "taskmaster.db")

	// Simulate exactly what a hard-killed previous process leaves behind:
	// open the DB directly, start an execution, then close without ever
	// finishing it.
	seedDB, err := db.Open(dbPath)
	require.NoError(t, err)
	require.NoError(t, seedDB.UpsertLane(&models.Lane{Name: "seeded", Width: 2}))
	taskID, err := seedDB.AddTask(&models.Task{Name: "orphan", LaneName: "seeded", Enabled: true, Command: "echo hi"})
	require.NoError(t, err)
	execID, err := seedDB.CreateExecution(taskID, "old-hero", time.Now())
	require.NoError(t, err)
	require.NoError(t, seedDB.StartExecution(execID, "old-hero"))
	require.NoError(t, seedDB.Close())

	cfg := config.TaskmasterConfig{
		StaticDir: staticDir,
		DBPath:    dbPath,
		Lanes:     []config.TaskmasterLane{{Name: "seeded", Width: 2}},
	}
	h, err := taskmaster.Build(cfg)
	require.NoError(t, err)
	closer := h.(interface{ Close() error })
	defer func() {
		require.NoError(t, closer.Close())
	}()

	srv := httptest.NewServer(h)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/executions")
	require.NoError(t, err)
	defer resp.Body.Close()
	var execs []map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&execs))
	require.Len(t, execs, 1)
	require.Equal(t, "failed", execs[0]["status"],
		"an execution left running by a previous process must be reconciled before Build() returns, not shown as still running")
}

func TestBuild_FullCycle(t *testing.T) {
	staticDir := t.TempDir()
	writeIndexHTML(t, staticDir)

	cfg := config.TaskmasterConfig{
		StaticDir: staticDir,
		DBPath:    filepath.Join(t.TempDir(), "taskmaster.db"),
		Lanes: []config.TaskmasterLane{
			{Name: "seeded", Width: 2},
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

	// GET /api/lanes shows the seeded lane.
	resp, err := http.Get(srv.URL + "/api/lanes")
	require.NoError(t, err)
	var lanes []map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&lanes))
	resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	found := false
	for _, l := range lanes {
		if l["name"] == "seeded" {
			found = true
		}
	}
	require.True(t, found, "seeded lane missing from /api/lanes: %+v", lanes)

	// GET /api/capabilities reflects allow_sudo from config.
	resp, err = http.Get(srv.URL + "/api/capabilities")
	require.NoError(t, err)
	var caps map[string]bool
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&caps))
	resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.False(t, caps["allow_sudo"])

	// POST a trivial task, enabled so the worker's poll loop picks it up
	// without an explicit enqueue.
	taskBody := `{"name":"echo-task","lane_name":"seeded","command":"echo hi","enabled":true}`
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

// TestBuild_BrakePersistsAcrossRestart engages the hand brake, closes the
// module (simulating shutdown), rebuilds it against the same DB file
// (simulating a restart), and verifies the brake gate boots engaged —
// SettingBrakeEngaged is DB-authoritative like allow_sudo.
func TestBuild_BrakePersistsAcrossRestart(t *testing.T) {
	staticDir := t.TempDir()
	writeIndexHTML(t, staticDir)
	dbPath := filepath.Join(t.TempDir(), "taskmaster.db")

	cfg := config.TaskmasterConfig{
		StaticDir: staticDir,
		DBPath:    dbPath,
		Lanes: []config.TaskmasterLane{
			{Name: "seeded", Width: 2},
		},
	}

	h, err := taskmaster.Build(cfg)
	require.NoError(t, err)
	srv := httptest.NewServer(h)

	resp, err := http.Post(srv.URL+"/api/brake", "application/json", nil)
	require.NoError(t, err)
	var body map[string]bool
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	resp.Body.Close()
	require.True(t, body["engaged"])

	srv.Close()
	closer := h.(interface{ Close() error })
	require.NoError(t, closer.Close())

	// Rebuild the module against the same DB file, simulating a restart.
	h2, err := taskmaster.Build(cfg)
	require.NoError(t, err)
	defer func() {
		closer2 := h2.(interface{ Close() error })
		require.NoError(t, closer2.Close())
	}()
	srv2 := httptest.NewServer(h2)
	defer srv2.Close()

	resp2, err := http.Get(srv2.URL + "/api/brake")
	require.NoError(t, err)
	defer resp2.Body.Close()
	var body2 map[string]bool
	require.NoError(t, json.NewDecoder(resp2.Body).Decode(&body2))
	require.True(t, body2["engaged"], "hand brake should stay engaged across a simulated restart")
}

// TestBuild_BoardEvents_PublishAndClose exercises the full board-events
// lifecycle wired by Build: a subscriber connects to GET /api/board/events,
// a task is posted and immediately enqueued via up-next, and the subscriber
// must see a task-enqueued event. Close() must then shut down cleanly with
// no goroutine leak (checked by this package's TestMain via goleak).
func TestBuild_BoardEvents_PublishAndClose(t *testing.T) {
	staticDir := t.TempDir()
	writeIndexHTML(t, staticDir)

	cfg := config.TaskmasterConfig{
		StaticDir: staticDir,
		DBPath:    filepath.Join(t.TempDir(), "taskmaster.db"),
		Lanes: []config.TaskmasterLane{
			{Name: "seeded", Width: 2},
		},
		SSEMaxSubscribers: 0,
	}

	h, err := taskmaster.Build(cfg)
	require.NoError(t, err)
	closer := h.(interface{ Close() error })
	// t.Cleanup (not defer) so teardown runs exactly once, in LIFO order,
	// on every exit path including t.Fatal: sub.Body.Close() first (so the
	// still-open SSE stream unblocks and srv.Close() doesn't deadlock
	// waiting for it), then srv.Close(), then closer.Close().
	t.Cleanup(func() { require.NoError(t, closer.Close()) })

	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	taskBody := `{"name":"board-task","lane_name":"seeded","command":"echo hi","enabled":false}`
	resp, err := http.Post(srv.URL+"/api/tasks", "application/json", strings.NewReader(taskBody))
	require.NoError(t, err)
	resp.Body.Close()
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	sub, err := http.Get(srv.URL + "/api/board/events")
	require.NoError(t, err)
	t.Cleanup(func() { sub.Body.Close() })

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

	// See the equivalent comment in coordinator's board_test.go: the broker
	// subscribes this connection's payload channel just after flushing its
	// initial ": connected" line, so retry the trigger instead of assuming
	// a single POST lands after the subscription is in place.
	deadline := time.After(5 * time.Second)
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		resp, err = http.Post(srv.URL+"/api/tasks/board-task/up-next", "application/json", nil)
		require.NoError(t, err)
		resp.Body.Close()
		require.Equal(t, http.StatusCreated, resp.StatusCode)

		select {
		case ev := <-events:
			require.Contains(t, ev, `"type":"task-enqueued"`)
			require.Contains(t, ev, `"task":"board-task"`)
			return
		case <-tick.C:
			continue
		case <-deadline:
			t.Fatal("timed out waiting for task-enqueued board event")
		}
	}
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
