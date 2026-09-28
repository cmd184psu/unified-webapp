package taskmaster

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"cmd184psu/unified-webapp/internal/taskmaster/db"

	"github.com/stretchr/testify/require"
)

// TestPruneOwnedLane_RemovesOnlyBackdatedJob restores the engine-level
// retention assertion dropped from TestOpen_FuncLaneFullCycle (no exported
// seam exists to backdate finished_at through the public API). It opens a
// real file-based *db.DB directly (no Engine.Open, so there is no live
// worker/pruner goroutine racing this test's writes), builds a bare *Engine
// wrapping it (this file is package taskmaster, so the unexported `db`
// field is directly settable), backdates one finished execution's
// finished_at via a second raw connection to the same file (outside any
// transaction — R1 is about this package's own transactions, not an
// independent test connection), and asserts pruneOnce removes exactly the
// backdated job's task/executions/metrics while a young job survives. No
// sleeps: every "elapsed time" here is a value written directly into the
// DB, not real wall-clock waiting.
func TestPruneOwnedLane_RemovesOnlyBackdatedJob(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "prune.db")
	d, err := db.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { d.Close() })

	_, err = d.EnsureOwnedLane("owned", "acme-module", 2, 1, false) // retention_days=1
	require.NoError(t, err)

	// old: submitted, claimed, finished successfully, then backdated to 2
	// days ago (older than the 1-day retention cutoff).
	oldTaskID, oldExecID, err := d.SubmitFuncTask(db.FuncTaskSpec{
		Name: "owned-old0000000001", Lane: "owned", Kind: "acmemodule.echo",
		Payload: []byte(`{}`), PayloadVersion: 1,
	})
	require.NoError(t, err)
	_, err = d.ClaimFuncExecution(oldExecID, "w")
	require.NoError(t, err)
	require.NoError(t, d.FinishFuncExecution(oldExecID, "success", nil, 5, 0, nil, nil))
	require.NoError(t, d.RecordMetric(oldTaskID, oldExecID, "success", 5, 0))

	// young: submitted, claimed, finished successfully, left at "now" —
	// well inside the 1-day retention window.
	youngTaskID, youngExecID, err := d.SubmitFuncTask(db.FuncTaskSpec{
		Name: "owned-young0000001", Lane: "owned", Kind: "acmemodule.echo",
		Payload: []byte(`{}`), PayloadVersion: 1,
	})
	require.NoError(t, err)
	_, err = d.ClaimFuncExecution(youngExecID, "w")
	require.NoError(t, err)
	require.NoError(t, d.FinishFuncExecution(youngExecID, "success", nil, 5, 0, nil, nil))
	require.NoError(t, d.RecordMetric(youngTaskID, youngExecID, "success", 5, 0))

	// Backdate only the old execution's finished_at, through a second,
	// independent connection to the same file (mirrors db_test.go's
	// sqlOpenLegacy pattern for exercising storage the exported API has no
	// seam to reach).
	raw, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	twoDaysAgoMs := time.Now().Add(-48 * time.Hour).UnixMilli()
	_, err = raw.Exec(`UPDATE task_executions SET finished_at = ? WHERE id = ?`, twoDaysAgoMs, oldExecID)
	require.NoError(t, err)
	require.NoError(t, raw.Close())

	e := &Engine{db: d}
	e.pruneOnce()

	oldTask, err := d.GetTask("owned-old0000000001")
	require.NoError(t, err)
	require.Nil(t, oldTask, "the backdated (finished > retention_days ago) job's task row must be pruned")

	oldExecs, err := d.ListExecutions("owned-old0000000001", 10)
	require.NoError(t, err)
	require.Empty(t, oldExecs, "the pruned job's executions must be gone")

	oldMetrics, err := d.GetMetrics("", "owned-old0000000001", 24*365)
	require.NoError(t, err)
	require.Empty(t, oldMetrics, "the pruned job's metrics must be gone")

	youngTask, err := d.GetTask("owned-young0000001")
	require.NoError(t, err)
	require.NotNil(t, youngTask, "a job finished inside the retention window must survive")

	youngExecs, err := d.ListExecutions("owned-young0000001", 10)
	require.NoError(t, err)
	require.Len(t, youngExecs, 1)

	youngMetrics, err := d.GetMetrics("", "owned-young0000001", 24*365)
	require.NoError(t, err)
	require.NotEmpty(t, youngMetrics)
}
