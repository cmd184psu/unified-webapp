package taskmaster_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"cmd184psu/unified-webapp/internal/platform/config"
	"cmd184psu/unified-webapp/internal/taskmaster"
	"cmd184psu/unified-webapp/internal/taskmaster/db"
	"cmd184psu/unified-webapp/internal/taskmaster/golane"
	"cmd184psu/unified-webapp/internal/taskmaster/models"
	"cmd184psu/unified-webapp/internal/taskmaster/worker"

	"github.com/stretchr/testify/require"
)

type enginePayload struct {
	// FailOnce makes the FIRST invocation for a given job id fail, and every
	// later invocation (i.e. after a rerun) succeed — since a rerun reuses
	// the same payload by construction (FR-T5), a payload that always fails
	// could never demonstrate a fail-then-rerun-succeeds cycle.
	FailOnce bool `json:"fail_once"`
	Fail     bool `json:"fail"`
}

func engineTestKind(name string) golane.Kind {
	var seen sync.Map
	return golane.NewKind[enginePayload](name, 1, nil, func(ctx context.Context, rc golane.RunContext, p enginePayload) (any, error) {
		if p.Fail {
			return nil, errors.New("requested failure")
		}
		if p.FailOnce {
			if _, already := seen.LoadOrStore(rc.JobID(), true); !already {
				return nil, errors.New("first attempt fails on purpose")
			}
		}
		return map[string]bool{"ok": true}, nil
	})
}

func openTestEngine(t *testing.T, cfg config.TaskmasterConfig, opts taskmaster.OpenOptions) *taskmaster.Engine {
	t.Helper()
	e, err := taskmaster.Open(cfg, opts)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, e.Close()) })
	return e
}

func testTaskmasterConfig(t *testing.T) config.TaskmasterConfig {
	t.Helper()
	staticDir := t.TempDir()
	writeIndexHTML(t, staticDir)
	return config.TaskmasterConfig{
		StaticDir:          staticDir,
		DBPath:             filepath.Join(t.TempDir(), "taskmaster.db"),
		ProgressIntervalMs: 50,
	}
}

// TestSeedLanes_SkipsOwnedLane pins P19: a config lane whose name collides
// with an already-owned lane must never be overwritten by seedLanes.
func TestSeedLanes_SkipsOwnedLane(t *testing.T) {
	cfg := testTaskmasterConfig(t)

	e := openTestEngine(t, cfg, taskmaster.OpenOptions{})
	_, err := e.RegisterLane(golane.LaneSpec{Name: "owned", Owner: "acme-module", InitialWidth: 3}, engineTestKind("acmemodule.echo"))
	require.NoError(t, err)
	require.NoError(t, e.Close())

	cfg.Lanes = []config.TaskmasterLane{{Name: "owned", Width: 9}}
	e2, err := taskmaster.Open(cfg, taskmaster.OpenOptions{})
	require.NoError(t, err)
	defer func() { require.NoError(t, e2.Close()) }()

	srv := httptest.NewServer(e2.Handler())
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/api/lanes/owned")
	require.NoError(t, err)
	defer resp.Body.Close()
	var lane map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&lane))
	require.Equal(t, float64(3), lane["width"], "config must never overwrite an owned lane's width")
	require.Equal(t, "acme-module", lane["owner"])
}

// TestRegisterLane_ConfigLaneCollisionError pins P19's other half: a config
// lane (owner "") that already exists must make RegisterLane fail, naming
// the conflict.
func TestRegisterLane_ConfigLaneCollisionError(t *testing.T) {
	cfg := testTaskmasterConfig(t)
	cfg.Lanes = []config.TaskmasterLane{{Name: "owned", Width: 5}}

	e := openTestEngine(t, cfg, taskmaster.OpenOptions{})
	_, err := e.RegisterLane(golane.LaneSpec{Name: "owned", Owner: "acme-module", InitialWidth: 1}, engineTestKind("acmemodule.echo"))
	require.Error(t, err)
	require.True(t, errors.Is(err, db.ErrLaneOwnedByOther))
	require.Contains(t, err.Error(), "already exists as a regular taskmaster lane")
}

// TestOpen_OwnedLanesOnlySkipsShellLanes pins P16: a headless engine
// (OwnedLanesOnly) never schedules a shell task, but still runs a
// registered func lane's jobs.
func TestOpen_OwnedLanesOnlySkipsShellLanes(t *testing.T) {
	cfg := testTaskmasterConfig(t)

	seedDB, err := db.Open(cfg.DBPath)
	require.NoError(t, err)
	require.NoError(t, seedDB.UpsertLane(&models.Lane{Name: "shell", Width: 2}))
	_, err = seedDB.AddTask(&models.Task{Name: "shell-task", LaneName: "shell", Enabled: true, Command: "echo hi"})
	require.NoError(t, err)
	require.NoError(t, seedDB.Close())

	worker.SetPollIntervalForTest(20 * time.Millisecond)
	t.Cleanup(func() { worker.SetPollIntervalForTest(5 * time.Second) })

	e := openTestEngine(t, cfg, taskmaster.OpenOptions{OwnedLanesOnly: true})
	require.Nil(t, e.Handler(), "a headless engine must expose no HTTP handler")

	lane, err := e.RegisterLane(golane.LaneSpec{Name: "owned", Owner: "acme-module", InitialWidth: 1}, engineTestKind("acmemodule.echo"))
	require.NoError(t, err)

	job, err := lane.Submit("acmemodule.echo", "job", enginePayload{})
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		j, err := lane.Get(job.ID)
		return err == nil && j.Status == "success"
	}, 10*time.Second, 20*time.Millisecond)

	time.Sleep(300 * time.Millisecond)
	rawDB, err := db.Open(cfg.DBPath)
	require.NoError(t, err)
	defer rawDB.Close()
	execs, err := rawDB.ListExecutions("shell-task", 10)
	require.NoError(t, err)
	require.Empty(t, execs, "a shell lane must never be scheduled by a headless (OwnedLanesOnly) engine")
}

// TestOpen_FuncLaneFullCycle exercises submit -> failure -> rerun -> success
// -> rerun refused (409/ErrSucceeded on both the HTTP route and Lane.Rerun)
// -> remove, end to end through the real engine and its HTTP handler.
// Retention pruning itself is pinned at the DB layer
// (TestPruneOwnedLane_DeletesTaskExecsMetrics, Phase 1) since backdating a
// finished_at timestamp has no exported seam to do deterministically here.
func TestOpen_FuncLaneFullCycle(t *testing.T) {
	cfg := testTaskmasterConfig(t)
	worker.SetPollIntervalForTest(20 * time.Millisecond)
	t.Cleanup(func() { worker.SetPollIntervalForTest(5 * time.Second) })

	e := openTestEngine(t, cfg, taskmaster.OpenOptions{})
	lane, err := e.RegisterLane(golane.LaneSpec{Name: "owned", Owner: "acme-module", InitialWidth: 2}, engineTestKind("acmemodule.echo"))
	require.NoError(t, err)

	srv := httptest.NewServer(e.Handler())
	defer srv.Close()

	failing, err := lane.Submit("acmemodule.echo", "will fail", enginePayload{FailOnce: true})
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		j, err := lane.Get(failing.ID)
		return err == nil && j.Status == "failed"
	}, 10*time.Second, 20*time.Millisecond)

	// Rerun via the HTTP route.
	j, err := lane.Get(failing.ID)
	require.NoError(t, err)
	resp, err := http.Post(srv.URL+"/api/executions/"+strconv.FormatInt(j.ExecID, 10)+"/rerun", "application/json", nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	resp.Body.Close()

	require.Eventually(t, func() bool {
		j, err := lane.Get(failing.ID)
		return err == nil && j.Status == "success"
	}, 10*time.Second, 20*time.Millisecond)

	// Rerun of a succeeded func job is refused (P18) on both routes.
	j, err = lane.Get(failing.ID)
	require.NoError(t, err)
	resp, err = http.Post(srv.URL+"/api/executions/"+strconv.FormatInt(j.ExecID, 10)+"/rerun", "application/json", nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusConflict, resp.StatusCode)
	resp.Body.Close()

	_, err = lane.Rerun(failing.ID)
	require.ErrorIs(t, err, golane.ErrSucceeded)

	require.NoError(t, lane.Remove(failing.ID))
	_, err = lane.Get(failing.ID)
	require.ErrorIs(t, err, golane.ErrNotFound)
}
