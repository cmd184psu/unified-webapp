package db_test

import (
	"database/sql"
	"fmt"
	"testing"
	"time"

	"cmd184psu/unified-webapp/internal/taskmaster/db"
	"cmd184psu/unified-webapp/internal/taskmaster/models"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
	_ "modernc.org/sqlite"
)

// sqlOpenLegacy opens a raw sqlite connection (bypassing db.Open's
// migration machinery entirely) so tests can hand-build an on-disk database
// in the shape a pre-B1 binary would have left behind.
func sqlOpenLegacy(path string) (*sql.DB, error) {
	return sql.Open("sqlite", path)
}

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

func newTestDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { d.Close() })
	return d
}

func seedLane(t *testing.T, d *db.DB, name string, width int) {
	t.Helper()
	err := d.UpsertLane(&models.Lane{Name: name, Width: width})
	require.NoError(t, err)
}

func seedTask(t *testing.T, d *db.DB, name, lane string) *models.Task {
	t.Helper()
	task := &models.Task{
		Name: name, LaneName: lane,
		Enabled: true, Command: "echo test",
	}
	id, err := d.AddTask(task)
	require.NoError(t, err)
	task.ID = id
	return task
}

// ─── Lane tests ──────────────────────────────────────────────────────────────

func TestUpsertLane_CreateAndUpdate(t *testing.T) {
	d := newTestDB(t)
	err := d.UpsertLane(&models.Lane{Name: "batch", Width: 5})
	require.NoError(t, err)

	l, err := d.GetLane("batch")
	require.NoError(t, err)
	require.NotNil(t, l)
	require.Equal(t, 5, l.Width)

	// update
	err = d.UpsertLane(&models.Lane{Name: "batch", Width: 10})
	require.NoError(t, err)
	l, _ = d.GetLane("batch")
	require.Equal(t, 10, l.Width)
}

func TestGetLane_NotFound(t *testing.T) {
	d := newTestDB(t)
	l, err := d.GetLane("nonexistent")
	require.NoError(t, err)
	require.Nil(t, l)
}

func TestListLanes(t *testing.T) {
	d := newTestDB(t)
	seedLane(t, d, "alpha", 2)
	seedLane(t, d, "beta", 3)

	lanes, err := d.ListLanes()
	require.NoError(t, err)
	require.Len(t, lanes, 2)
	require.Equal(t, "alpha", lanes[0].Name)
	require.Equal(t, "beta", lanes[1].Name)
}

func TestDeleteLane_NoTasks(t *testing.T) {
	d := newTestDB(t)
	seedLane(t, d, "empty", 1)
	require.NoError(t, d.DeleteLane("empty"))
	l, _ := d.GetLane("empty")
	require.Nil(t, l)
}

func TestDeleteLane_WithTasks(t *testing.T) {
	d := newTestDB(t)
	seedLane(t, d, "used", 1)
	seedTask(t, d, "t1", "used")
	err := d.DeleteLane("used")
	require.Error(t, err)
}

func TestSetLanePaused(t *testing.T) {
	d := newTestDB(t)
	seedLane(t, d, "g1", 2)
	require.NoError(t, d.SetLanePaused("g1", true, "admin"))

	l, _ := d.GetLane("g1")
	require.True(t, l.Paused)
	require.Equal(t, "admin", l.PausedBy)
	require.NotNil(t, l.PausedAt)
}

// ─── Task tests ───────────────────────────────────────────────────────────────

func TestAddTask_Basic(t *testing.T) {
	d := newTestDB(t)
	seedLane(t, d, "bg", 2)
	task := &models.Task{
		Name: "my-task", LaneName: "bg",
		Enabled: true, Position: 10, Command: "ls",
	}
	id, err := d.AddTask(task)
	require.NoError(t, err)
	require.Positive(t, id)

	got, err := d.GetTask("my-task")
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, "my-task", got.Name)
	require.Equal(t, 10, got.Position)
	require.Equal(t, "ls", got.Command)
	require.True(t, got.Enabled)
}

func TestAddTask_Upsert(t *testing.T) {
	d := newTestDB(t)
	seedLane(t, d, "bg", 2)
	task := &models.Task{Name: "t", LaneName: "bg", Enabled: true, Position: 50, Command: "echo hi"}
	id1, _ := d.AddTask(task)

	task.Position = 99
	id2, err := d.AddTask(task)
	require.NoError(t, err)
	// upsert returns lastInsertId which may be 0 on update; check the value was updated
	_ = id1
	_ = id2
	got, _ := d.GetTask("t")
	require.Equal(t, 99, got.Position)
}

func TestGetTask_NotFound(t *testing.T) {
	d := newTestDB(t)
	got, err := d.GetTask("nonexistent")
	require.NoError(t, err)
	require.Nil(t, got)
}

func TestListTasks_LaneFilter(t *testing.T) {
	d := newTestDB(t)
	seedLane(t, d, "g1", 1)
	seedLane(t, d, "g2", 1)
	seedTask(t, d, "t-g1", "g1")
	seedTask(t, d, "t-g2", "g2")

	all, _ := d.ListTasks("")
	require.Len(t, all, 2)

	g1tasks, _ := d.ListTasks("g1")
	require.Len(t, g1tasks, 1)
	require.Equal(t, "t-g1", g1tasks[0].Name)
}

func TestDeleteTask_CascadesExecutions(t *testing.T) {
	d := newTestDB(t)
	seedLane(t, d, "g", 1)
	task := seedTask(t, d, "mytask", "g")

	execID, err := d.CreateExecution(task.ID, "worker1", time.Now())
	require.NoError(t, err)
	require.Positive(t, execID)

	require.NoError(t, d.DeleteTask("mytask"))

	// execution should be gone via CASCADE
	exec, err := d.GetExecution(execID)
	require.NoError(t, err)
	require.Nil(t, exec)
}

func TestSetTaskPaused(t *testing.T) {
	d := newTestDB(t)
	seedLane(t, d, "g", 1)
	seedTask(t, d, "t", "g")

	require.NoError(t, d.SetTaskPaused("t", true))
	got, _ := d.GetTask("t")
	require.True(t, got.Paused)

	require.NoError(t, d.SetTaskPaused("t", false))
	got, _ = d.GetTask("t")
	require.False(t, got.Paused)
}

// ─── Eligible tasks ───────────────────────────────────────────────────────────

func TestGetEligibleTasks_NewTask(t *testing.T) {
	d := newTestDB(t)
	seedLane(t, d, "g", 2)
	seedTask(t, d, "new-task", "g")

	tasks, err := d.GetEligibleTasks()
	require.NoError(t, err)
	require.Len(t, tasks, 1)
}

func TestGetEligibleTasks_Position(t *testing.T) {
	d := newTestDB(t)
	seedLane(t, d, "g", 5)
	t1 := &models.Task{Name: "high", LaneName: "g", Enabled: true, Position: 10, Command: "echo hi"}
	t2 := &models.Task{Name: "low", LaneName: "g", Enabled: true, Position: 90, Command: "echo hi"}
	d.AddTask(t1)
	d.AddTask(t2)

	tasks, _ := d.GetEligibleTasks()
	require.Len(t, tasks, 2)
	require.Equal(t, "high", tasks[0].Name)
}

func TestGetEligibleTasks_Cooldown(t *testing.T) {
	d := newTestDB(t)
	seedLane(t, d, "g", 2)
	task := &models.Task{Name: "cooldown-task", LaneName: "g", Enabled: true,
		Position: 50, Command: "echo hi", Repeat: true, CooldownSeconds: 3600}
	id, _ := d.AddTask(task)

	execID, _ := d.CreateExecution(id, "w", time.Now())
	require.NoError(t, d.FinishExecution(execID, "success", nil, 100, 0))

	tasks, _ := d.GetEligibleTasks()
	require.Empty(t, tasks, "task in cooldown should not be eligible")
}

func TestGetEligibleTasks_LanePaused(t *testing.T) {
	d := newTestDB(t)
	seedLane(t, d, "g", 2)
	seedTask(t, d, "t", "g")
	require.NoError(t, d.SetLanePaused("g", true, "admin"))

	tasks, _ := d.GetEligibleTasks()
	require.Empty(t, tasks)
}

func TestGetEligibleTasks_TaskPaused(t *testing.T) {
	d := newTestDB(t)
	seedLane(t, d, "g", 2)
	seedTask(t, d, "t", "g")
	require.NoError(t, d.SetTaskPaused("t", true))

	tasks, _ := d.GetEligibleTasks()
	require.Empty(t, tasks)
}

func TestGetEligibleTasks_Locked(t *testing.T) {
	d := newTestDB(t)
	seedLane(t, d, "g", 2)
	task := seedTask(t, d, "locked-task", "g")

	ok, err := d.AcquireLock(task.ID, "worker1", 10*time.Minute)
	require.NoError(t, err)
	require.True(t, ok)

	tasks, _ := d.GetEligibleTasks()
	require.Empty(t, tasks)
}

func TestGetEligibleTasks_PendingEnqueue(t *testing.T) {
	d := newTestDB(t)
	seedLane(t, d, "g", 2)
	task := &models.Task{Name: "once", LaneName: "g", Enabled: true,
		Position: 50, Command: "echo hi", Repeat: false}
	id, _ := d.AddTask(task)

	// Mark as already completed once
	execID, _ := d.CreateExecution(id, "w", time.Now())
	require.NoError(t, d.FinishExecution(execID, "success", nil, 50, 0))

	// Without repeat and no pending, not eligible
	tasks, _ := d.GetEligibleTasks()
	require.Empty(t, tasks)

	// Force enqueue
	_, err := d.EnqueueTask("once", time.Now())
	require.NoError(t, err)

	tasks, _ = d.GetEligibleTasks()
	require.Len(t, tasks, 1)
}

// ─── Locking tests ────────────────────────────────────────────────────────────

func TestAcquireLock_Exclusive(t *testing.T) {
	d := newTestDB(t)
	seedLane(t, d, "g", 2)
	task := seedTask(t, d, "t", "g")

	ok1, err := d.AcquireLock(task.ID, "worker1", time.Minute)
	require.NoError(t, err)
	require.True(t, ok1)

	ok2, err := d.AcquireLock(task.ID, "worker2", time.Minute)
	require.NoError(t, err)
	require.False(t, ok2)
}

func TestAcquireLock_AfterExpiry(t *testing.T) {
	d := newTestDB(t)
	seedLane(t, d, "g", 2)
	task := seedTask(t, d, "t", "g")

	ok, _ := d.AcquireLock(task.ID, "worker1", -time.Second) // already expired
	require.True(t, ok)

	require.NoError(t, d.CleanupExpiredLocks())

	ok2, _ := d.AcquireLock(task.ID, "worker2", time.Minute)
	require.True(t, ok2)
}

func TestReleaseLock(t *testing.T) {
	d := newTestDB(t)
	seedLane(t, d, "g", 2)
	task := seedTask(t, d, "t", "g")

	d.AcquireLock(task.ID, "w1", time.Minute)
	require.NoError(t, d.ReleaseLock(task.ID, "w1"))

	ok, _ := d.AcquireLock(task.ID, "w2", time.Minute)
	require.True(t, ok)
}

// ─── Execution tests ──────────────────────────────────────────────────────────

func TestCreateExecution_FinishExecution(t *testing.T) {
	d := newTestDB(t)
	seedLane(t, d, "g", 2)
	task := seedTask(t, d, "t", "g")

	scheduled := time.Now().Add(-500 * time.Millisecond)
	execID, err := d.CreateExecution(task.ID, "worker1", scheduled)
	require.NoError(t, err)
	require.Positive(t, execID)

	require.NoError(t, d.StartExecution(execID, "worker1"))

	errMsg := "something failed"
	require.NoError(t, d.FinishExecution(execID, "failed", &errMsg, 1234, 55))

	exec, err := d.GetExecution(execID)
	require.NoError(t, err)
	require.NotNil(t, exec)
	require.Equal(t, "failed", exec.Status)
	require.NotNil(t, exec.ErrorMessage)
	require.Equal(t, "something failed", *exec.ErrorMessage)
	require.NotNil(t, exec.DurationMs)
	require.Equal(t, int64(1234), *exec.DurationMs)
}

func TestListExecutions(t *testing.T) {
	d := newTestDB(t)
	seedLane(t, d, "g", 2)
	task := seedTask(t, d, "t", "g")

	for i := 0; i < 3; i++ {
		id, _ := d.CreateExecution(task.ID, "w", time.Now())
		d.FinishExecution(id, "success", nil, 100, 0)
	}

	execs, err := d.ListExecutions("", 10)
	require.NoError(t, err)
	require.Len(t, execs, 3)
}

// ─── Metrics tests ────────────────────────────────────────────────────────────

func TestRecordMetric_GetMetrics(t *testing.T) {
	d := newTestDB(t)
	seedLane(t, d, "g", 2)
	task := seedTask(t, d, "t", "g")

	execID, _ := d.CreateExecution(task.ID, "w", time.Now())
	require.NoError(t, d.RecordMetric(task.ID, execID, "success", 500, 10))
	require.NoError(t, d.RecordMetric(task.ID, execID, "failed", 200, 5))

	summaries, err := d.GetMetrics("", "", 24)
	require.NoError(t, err)
	require.Len(t, summaries, 1)
	s := summaries[0]
	require.Equal(t, "t", s.TaskName)
	require.Equal(t, 1, s.SuccessCount)
	require.Equal(t, 1, s.FailedCount)
	require.NotNil(t, s.AvgDurationMs)
}

func TestGetMetrics_LaneFilter(t *testing.T) {
	d := newTestDB(t)
	seedLane(t, d, "g1", 1)
	seedLane(t, d, "g2", 1)
	task1 := seedTask(t, d, "t1", "g1")
	task2 := seedTask(t, d, "t2", "g2")

	e1, _ := d.CreateExecution(task1.ID, "w", time.Now())
	d.RecordMetric(task1.ID, e1, "success", 100, 0)
	e2, _ := d.CreateExecution(task2.ID, "w", time.Now())
	d.RecordMetric(task2.ID, e2, "success", 200, 0)

	summaries, _ := d.GetMetrics("g1", "", 24)
	require.Len(t, summaries, 1)
	require.Equal(t, "t1", summaries[0].TaskName)
}

func TestCountRunningInLane(t *testing.T) {
	d := newTestDB(t)
	seedLane(t, d, "g", 5)
	task := seedTask(t, d, "t", "g")

	count, err := d.CountRunningInLane("g")
	require.NoError(t, err)
	require.Equal(t, 0, count)

	execID, _ := d.CreateExecution(task.ID, "w", time.Now())
	d.StartExecution(execID, "w")

	count, err = d.CountRunningInLane("g")
	require.NoError(t, err)
	require.Equal(t, 1, count)
}

func TestSettingsRoundTrip(t *testing.T) {
	d := newTestDB(t)

	// Missing key: not present, no error.
	_, ok, err := d.GetSetting(db.SettingAllowSudo)
	require.NoError(t, err)
	require.False(t, ok)

	// Set then get.
	require.NoError(t, d.SetSetting(db.SettingAllowSudo, db.EncodeBoolSetting(true)))
	v, ok, err := d.GetSetting(db.SettingAllowSudo)
	require.NoError(t, err)
	require.True(t, ok)
	require.True(t, db.DecodeBoolSetting(v))

	// Upsert overwrites.
	require.NoError(t, d.SetSetting(db.SettingAllowSudo, db.EncodeBoolSetting(false)))
	v, ok, err = d.GetSetting(db.SettingAllowSudo)
	require.NoError(t, err)
	require.True(t, ok)
	require.False(t, db.DecodeBoolSetting(v))
}

// ─── Migration tests ──────────────────────────────────────────────────────────

// TestMigration_FreshDB verifies a brand new database opens cleanly and ends
// up with the final lane/task shape (no error implies the bootstrap +
// migration-3 rebuild both ran without a hitch).
func TestMigration_FreshDB(t *testing.T) {
	d := newTestDB(t)
	seedLane(t, d, "default", 2)
	task := &models.Task{Name: "t", LaneName: "default", Enabled: true, Command: "echo hi", Position: 1}
	_, err := d.AddTask(task)
	require.NoError(t, err)

	got, err := d.GetTask("t")
	require.NoError(t, err)
	require.Equal(t, "echo hi", got.Command)
	require.Equal(t, 1, got.Position)
}

// TestMigration_LegacyOnDiskDB simulates an existing on-disk database created
// by the pre-B1 schema (groups/tasks with pool_limit/allowed_types/
// group_name/task_type/args/priority, schema_version=2) and verifies that
// opening it with the current code migrates it cleanly to the lane/task
// shape, carrying forward a legacy shell task's command from its args JSON,
// and that a second Open() (simulating a service restart) still succeeds.
func TestMigration_LegacyOnDiskDB(t *testing.T) {
	path := t.TempDir() + "/legacy.db"

	legacy, err := sqlOpenLegacy(path)
	require.NoError(t, err)

	_, err = legacy.Exec(`
CREATE TABLE schema_version (version INTEGER PRIMARY KEY);
INSERT INTO schema_version (version) VALUES (2);

CREATE TABLE groups (
  name TEXT PRIMARY KEY,
  pool_limit INTEGER NOT NULL DEFAULT 1,
  allowed_types TEXT NOT NULL DEFAULT '[]',
  paused INTEGER NOT NULL DEFAULT 0,
  paused_at INTEGER,
  paused_by TEXT,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);
INSERT INTO groups (name, pool_limit, allowed_types, paused, created_at, updated_at)
  VALUES ('legacy-lane', 3, '[]', 0, 1000, 1000);

CREATE TABLE tasks (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT UNIQUE NOT NULL,
  group_name TEXT NOT NULL,
  enabled INTEGER NOT NULL DEFAULT 1,
  paused INTEGER NOT NULL DEFAULT 0,
  priority INTEGER NOT NULL DEFAULT 50,
  cooldown_seconds INTEGER NOT NULL DEFAULT 0,
  repeat INTEGER NOT NULL DEFAULT 0,
  task_type TEXT NOT NULL,
  args TEXT NOT NULL DEFAULT '{}',
  sudo INTEGER NOT NULL DEFAULT 0,
  output_file TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);
INSERT INTO tasks (name, group_name, enabled, paused, priority, cooldown_seconds, repeat, task_type, args, sudo, output_file, created_at, updated_at)
  VALUES ('legacy-shell', 'legacy-lane', 1, 0, 20, 0, 0, 'shell', '{"shell":"echo legacy"}', 0, '', 1000, 1000);
INSERT INTO tasks (name, group_name, enabled, paused, priority, cooldown_seconds, repeat, task_type, args, sudo, output_file, created_at, updated_at)
  VALUES ('legacy-exec', 'legacy-lane', 1, 0, 10, 0, 0, 'exec', '{"command":"true"}', 0, '', 1000, 1000);

CREATE TABLE task_executions (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  task_id INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  scheduled_at INTEGER,
  started_at INTEGER,
  finished_at INTEGER,
  status TEXT NOT NULL DEFAULT 'pending',
  error_message TEXT,
  worker_id TEXT,
  duration_ms INTEGER,
  schedule_delay_ms INTEGER
);
`)
	require.NoError(t, err)
	require.NoError(t, legacy.Close())

	// First Open(): must run migration 3 and land on the final shape.
	d, err := db.Open(path)
	require.NoError(t, err)

	lane, err := d.GetLane("legacy-lane")
	require.NoError(t, err)
	require.NotNil(t, lane, "lane should carry forward from the old group")
	require.Equal(t, 3, lane.Width)

	shellTask, err := d.GetTask("legacy-shell")
	require.NoError(t, err)
	require.NotNil(t, shellTask)
	require.Equal(t, "echo legacy", shellTask.Command, "legacy shell task_type should carry its command forward from args.shell")
	require.Equal(t, "legacy-lane", shellTask.LaneName)
	require.Equal(t, 20, shellTask.Position, "position carries forward from the old priority column")

	execTask, err := d.GetTask("legacy-exec")
	require.NoError(t, err)
	require.NotNil(t, execTask)
	require.Empty(t, execTask.Command, "non-shell legacy task_type is left blank for manual fixup")

	require.NoError(t, d.Close())

	// Second Open(): simulates a service restart against an already-migrated
	// DB file. Must not try to recreate the dropped groups table / old
	// indexes referencing columns that no longer exist.
	d2, err := db.Open(path)
	require.NoError(t, err)
	require.NoError(t, d2.Close())
}

// ─── Orphaned-execution reconciliation primitives ───────────────────────────
//
// The liveness-aware ORCHESTRATION (worker.ReconcileOrphans, which decides
// skip-vs-mark-failed by checking whether a candidate's PID is actually
// alive) is tested in the worker package, where spawning/killing real
// processes to test liveness naturally belongs. These test the smaller DB
// primitives it's built from.

func TestListOrphanCandidates_ReturnsOnlyRunning(t *testing.T) {
	d := newTestDB(t)
	seedLane(t, d, "g", 2)
	task := seedTask(t, d, "orphan-task", "g")
	finishedTask := seedTask(t, d, "done-task", "g")

	runningID, err := d.CreateExecution(task.ID, "hero", time.Now())
	require.NoError(t, err)
	require.NoError(t, d.StartExecution(runningID, "hero"))
	require.NoError(t, d.SetExecutionPID(runningID, 424242))

	doneID, err := d.CreateExecution(finishedTask.ID, "hero", time.Now())
	require.NoError(t, err)
	require.NoError(t, d.FinishExecution(doneID, "success", nil, 10, 0))

	candidates, err := d.ListOrphanCandidates()
	require.NoError(t, err)
	require.Len(t, candidates, 1)
	require.Equal(t, runningID, candidates[0].ExecID)
	require.Equal(t, task.ID, candidates[0].TaskID)
	require.NotNil(t, candidates[0].PID)
	require.Equal(t, 424242, *candidates[0].PID)
}

func TestListOrphanCandidates_NilPIDWhenNeverSet(t *testing.T) {
	d := newTestDB(t)
	seedLane(t, d, "g", 2)
	task := seedTask(t, d, "t", "g")
	execID, err := d.CreateExecution(task.ID, "hero", time.Now())
	require.NoError(t, err)
	require.NoError(t, d.StartExecution(execID, "hero"))

	candidates, err := d.ListOrphanCandidates()
	require.NoError(t, err)
	require.Len(t, candidates, 1)
	require.Nil(t, candidates[0].PID, "an execution whose PID was never persisted must report PID as unknown, not a zero value")
}

func TestMarkOrphanFailed_ClosesOutAndReleasesLock(t *testing.T) {
	d := newTestDB(t)
	seedLane(t, d, "g", 2)
	task := seedTask(t, d, "t", "g")
	execID, err := d.CreateExecution(task.ID, "hero", time.Now())
	require.NoError(t, err)
	require.NoError(t, d.StartExecution(execID, "hero"))
	// Simulate a lock held under a DIFFERENT worker_id than the one about to
	// call MarkOrphanFailed — e.g. the hostname changed across a restart.
	_, err = d.AcquireLock(task.ID, "old-hostname", 10*time.Minute)
	require.NoError(t, err)

	require.NoError(t, d.MarkOrphanFailed(execID, task.ID))

	exec, err := d.GetExecution(execID)
	require.NoError(t, err)
	require.Equal(t, "failed", exec.Status)
	require.NotNil(t, exec.FinishedAt)
	require.NotNil(t, exec.ErrorMessage)
	require.Contains(t, *exec.ErrorMessage, "interrupted")

	// The lock must be gone regardless of worker_id, or the task could
	// never be re-picked until its 10-minute TTL happened to expire.
	locked, err := d.AcquireLock(task.ID, "new-hostname", 10*time.Minute)
	require.NoError(t, err)
	require.True(t, locked, "lock should have been released by MarkOrphanFailed regardless of worker_id")
}

// ─── Migration 5 (owned lanes, func tasks, progress/result) ─────────────────

// tableHasColumn reports whether table has a column named col, via a raw
// (non-db.DB) connection's PRAGMA table_info — used to inspect the schema
// independently of the scan/column-list code under test.
func tableHasColumn(t *testing.T, raw *sql.DB, table, col string) bool {
	t.Helper()
	rows, err := raw.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	require.NoError(t, err)
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt any
		require.NoError(t, rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk))
		if name == col {
			return true
		}
	}
	require.NoError(t, rows.Err())
	return false
}

func TestMigration5_FreshDBHasColumns(t *testing.T) {
	path := t.TempDir() + "/fresh.db"
	d, err := db.Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { d.Close() })

	raw, err := sqlOpenLegacy(path)
	require.NoError(t, err)
	defer raw.Close()

	for _, c := range []string{"owner", "hidden", "retention_days"} {
		require.True(t, tableHasColumn(t, raw, "lanes", c), "lanes.%s", c)
	}
	for _, c := range []string{"kind", "label", "payload", "payload_version"} {
		require.True(t, tableHasColumn(t, raw, "tasks", c), "tasks.%s", c)
	}
	for _, c := range []string{"progress_pct", "progress_label", "result"} {
		require.True(t, tableHasColumn(t, raw, "task_executions", c), "task_executions.%s", c)
	}

	var version int
	require.NoError(t, raw.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_version`).Scan(&version))
	require.Equal(t, 5, version)
}

// TestMigration5_RollsBackAtomically manually undoes migration 5 EXCEPT the
// task_executions.result column, then re-opens the DB. Migration 5 re-runs
// from scratch (schema_version was rolled back to 4) and fails partway
// through, on "ADD COLUMN result" (already present) — which must roll back
// the entire migration transaction, not just that one statement, so an
// earlier successful ALTER in the same migration (lanes.owner) must not
// stick around either.
func TestMigration5_RollsBackAtomically(t *testing.T) {
	path := t.TempDir() + "/rollback.db"
	d, err := db.Open(path)
	require.NoError(t, err)
	require.NoError(t, d.Close())

	raw, err := sqlOpenLegacy(path)
	require.NoError(t, err)
	_, err = raw.Exec(`
DROP INDEX idx_tasks_kind;
ALTER TABLE lanes DROP COLUMN owner;
ALTER TABLE lanes DROP COLUMN hidden;
ALTER TABLE lanes DROP COLUMN retention_days;
ALTER TABLE tasks DROP COLUMN kind;
ALTER TABLE tasks DROP COLUMN label;
ALTER TABLE tasks DROP COLUMN payload;
ALTER TABLE tasks DROP COLUMN payload_version;
ALTER TABLE task_executions DROP COLUMN progress_pct;
ALTER TABLE task_executions DROP COLUMN progress_label;
DELETE FROM schema_version WHERE version = 5;
`)
	require.NoError(t, err)
	require.NoError(t, raw.Close())

	_, err = db.Open(path)
	require.Error(t, err)
	require.Contains(t, err.Error(), "migration 5")

	raw2, err := sqlOpenLegacy(path)
	require.NoError(t, err)
	defer raw2.Close()

	require.False(t, tableHasColumn(t, raw2, "lanes", "owner"), "the rolled-back migration must not leave lanes.owner behind")

	var version int
	require.NoError(t, raw2.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_version`).Scan(&version))
	require.Equal(t, 4, version)
}

// ─── Eligibility narrowing for func tasks (P3) ──────────────────────────────

func TestGetEligibleTasks_FuncTaskOnlyWithPending(t *testing.T) {
	d := newTestDB(t)
	seedLane(t, d, "owned", 2)

	_, execID, err := d.SubmitFuncTask(db.FuncTaskSpec{Name: "owned-abc123", Lane: "owned", Kind: "test.echo", PayloadVersion: 1})
	require.NoError(t, err)

	tasks, err := d.GetEligibleTasks()
	require.NoError(t, err)
	require.Len(t, tasks, 1, "the func task is eligible while its execution is pending")

	canceled, _, err := d.CancelPendingFuncExecution(execID)
	require.NoError(t, err)
	require.True(t, canceled)

	tasks, err = d.GetEligibleTasks()
	require.NoError(t, err)
	require.Empty(t, tasks, "a func task with no pending execution is not eligible")

	// A never-run shell task in the same lane is unaffected (R3).
	seedTask(t, d, "shell-task", "owned")
	tasks, err = d.GetEligibleTasks()
	require.NoError(t, err)
	require.Len(t, tasks, 1)
	require.Equal(t, "shell-task", tasks[0].Name)
}

func TestGetEligibleTasks_ShellCanceledQuirkUnchanged(t *testing.T) {
	d := newTestDB(t)
	seedLane(t, d, "g", 2)
	task := seedTask(t, d, "shell-once", "g")

	execID, err := d.CreateExecution(task.ID, "w", time.Now())
	require.NoError(t, err)
	require.NoError(t, d.FinishExecution(execID, "canceled", nil, 0, 0))

	tasks, err := d.GetEligibleTasks()
	require.NoError(t, err)
	require.Len(t, tasks, 1, "a shell one-shot whose only execution is canceled stays eligible today (P3 quirk, unchanged)")
	require.Equal(t, "shell-once", tasks[0].Name)
}

// ─── AddTask / UpdateTask guards (N1) ───────────────────────────────────────

func TestAddTask_DoesNotClobberFuncTask(t *testing.T) {
	d := newTestDB(t)
	seedLane(t, d, "owned", 2)
	seedLane(t, d, "other", 1)

	_, _, err := d.SubmitFuncTask(db.FuncTaskSpec{Name: "job-1", Lane: "owned", Kind: "test.echo", Label: "orig", PayloadVersion: 1})
	require.NoError(t, err)

	// An HTTP-style upsert must not clobber a func task's row.
	_, err = d.AddTask(&models.Task{Name: "job-1", LaneName: "other", Enabled: true, Command: "echo hi"})
	require.NoError(t, err)

	got, err := d.GetTask("job-1")
	require.NoError(t, err)
	require.Equal(t, "owned", got.LaneName, "AddTask must not clobber a func task via ON CONFLICT")
	require.Equal(t, "test.echo", got.Kind)
}

func TestUpdateTask_RejectsUnknownField(t *testing.T) {
	d := newTestDB(t)
	seedLane(t, d, "g", 1)
	seedTask(t, d, "t", "g")

	err := d.UpdateTask("t", map[string]any{"kind": "hacked"})
	require.ErrorIs(t, err, db.ErrUnknownTaskField)
}

// ─── Func-task lifecycle primitives ─────────────────────────────────────────

func TestSubmitFuncTask_FIFOPositionsAndPending(t *testing.T) {
	d := newTestDB(t)
	seedLane(t, d, "owned", 2)

	taskID1, execID1, err := d.SubmitFuncTask(db.FuncTaskSpec{Name: "j1", Lane: "owned", Kind: "test.echo", PayloadVersion: 1})
	require.NoError(t, err)
	taskID2, execID2, err := d.SubmitFuncTask(db.FuncTaskSpec{Name: "j2", Lane: "owned", Kind: "test.echo", PayloadVersion: 1})
	require.NoError(t, err)

	t1, err := d.GetTask("j1")
	require.NoError(t, err)
	require.Equal(t, 0, t1.Position)

	t2, err := d.GetTask("j2")
	require.NoError(t, err)
	require.Equal(t, 1, t2.Position, "second func task takes the next FIFO position")

	pending1, err := d.GetPendingExecution(taskID1)
	require.NoError(t, err)
	require.NotNil(t, pending1)
	require.Equal(t, execID1, pending1.ID)
	require.Equal(t, "pending", pending1.Status)

	pending2, err := d.GetPendingExecution(taskID2)
	require.NoError(t, err)
	require.NotNil(t, pending2)
	require.Equal(t, execID2, pending2.ID)
}

func TestClaimFuncExecution_OnlyFromPending(t *testing.T) {
	d := newTestDB(t)
	seedLane(t, d, "owned", 2)
	_, execID, err := d.SubmitFuncTask(db.FuncTaskSpec{Name: "j1", Lane: "owned", Kind: "test.echo", PayloadVersion: 1})
	require.NoError(t, err)

	claimed, err := d.ClaimFuncExecution(execID, "w1")
	require.NoError(t, err)
	require.True(t, claimed)

	exec, err := d.GetExecution(execID)
	require.NoError(t, err)
	require.Equal(t, "running", exec.Status)
	require.NotNil(t, exec.StartedAt)

	claimed2, err := d.ClaimFuncExecution(execID, "w2")
	require.NoError(t, err)
	require.False(t, claimed2, "a second claim on an already-running execution affects nothing")
}

func TestCancelPendingFuncExecution_RecordsMetric_ShellIgnored(t *testing.T) {
	d := newTestDB(t)
	seedLane(t, d, "owned", 2)
	taskID, execID, err := d.SubmitFuncTask(db.FuncTaskSpec{Name: "j1", Lane: "owned", Kind: "test.echo", PayloadVersion: 1})
	require.NoError(t, err)

	canceled, gotTaskID, err := d.CancelPendingFuncExecution(execID)
	require.NoError(t, err)
	require.True(t, canceled)
	require.Equal(t, taskID, gotTaskID)

	exec, err := d.GetExecution(execID)
	require.NoError(t, err)
	require.Equal(t, "canceled", exec.Status)

	metrics, err := d.GetMetrics("", "", 24)
	require.NoError(t, err)
	require.Len(t, metrics, 1)
	require.Equal(t, 1, metrics[0].CanceledCount)

	canceled2, _, err := d.CancelPendingFuncExecution(execID)
	require.NoError(t, err)
	require.False(t, canceled2, "a second call on an already-terminal execution is a no-op")

	// A pending SHELL execution is ignored entirely: not canceled, no metric.
	seedLane(t, d, "shell-lane", 1)
	shellTask := seedTask(t, d, "shell-t", "shell-lane")
	shellExecID, err := d.EnqueueTask(shellTask.Name, time.Now())
	require.NoError(t, err)

	shellCanceled, _, err := d.CancelPendingFuncExecution(shellExecID)
	require.NoError(t, err)
	require.False(t, shellCanceled)

	shellExec, err := d.GetExecution(shellExecID)
	require.NoError(t, err)
	require.Equal(t, "pending", shellExec.Status)
}

func TestRerunTask_BusyAndRequeuePosition(t *testing.T) {
	d := newTestDB(t)
	seedLane(t, d, "owned", 2)

	taskID, execID, err := d.SubmitFuncTask(db.FuncTaskSpec{Name: "j1", Lane: "owned", Kind: "test.echo", PayloadVersion: 1})
	require.NoError(t, err)
	require.NoError(t, d.FinishExecution(execID, "failed", nil, 10, 0))

	_, _, err = d.SubmitFuncTask(db.FuncTaskSpec{Name: "j2", Lane: "owned", Kind: "test.echo", PayloadVersion: 1})
	require.NoError(t, err)

	// Busy: a pending/running execution blocks rerun.
	seedLane(t, d, "busy-lane", 1)
	_, _, err = d.SubmitFuncTask(db.FuncTaskSpec{Name: "busy1", Lane: "busy-lane", Kind: "test.echo", PayloadVersion: 1})
	require.NoError(t, err)
	_, err = d.RerunTask("busy1")
	require.ErrorIs(t, err, db.ErrTaskBusy)

	newExecID, err := d.RerunTask("j1")
	require.NoError(t, err)
	require.NotZero(t, newExecID)

	got, err := d.GetTask("j1")
	require.NoError(t, err)
	require.Equal(t, taskID, got.ID)
	require.Equal(t, 2, got.Position, "func task requeues to the back of the lane (FIFO)")

	// Shell task: position unchanged after rerun (R3).
	seedLane(t, d, "shell-lane", 1)
	shellID, err := d.AddTask(&models.Task{Name: "shell1", LaneName: "shell-lane", Enabled: true, Position: 5, Command: "echo hi"})
	require.NoError(t, err)
	shellExecID, err := d.CreateExecution(shellID, "w", time.Now())
	require.NoError(t, err)
	require.NoError(t, d.FinishExecution(shellExecID, "success", nil, 10, 0))

	_, err = d.RerunTask("shell1")
	require.NoError(t, err)
	gotShell, err := d.GetTask("shell1")
	require.NoError(t, err)
	require.Equal(t, 5, gotShell.Position, "shell task position is left alone on rerun (R3)")
}

func TestRemoveIdleTask_RunningBusy_QueuedRemoved(t *testing.T) {
	d := newTestDB(t)
	seedLane(t, d, "owned", 2)

	_, execID, err := d.SubmitFuncTask(db.FuncTaskSpec{Name: "running-job", Lane: "owned", Kind: "test.echo", PayloadVersion: 1})
	require.NoError(t, err)
	claimed, err := d.ClaimFuncExecution(execID, "w1")
	require.NoError(t, err)
	require.True(t, claimed)

	require.ErrorIs(t, d.RemoveIdleTask("running-job"), db.ErrTaskBusy)

	_, _, err = d.SubmitFuncTask(db.FuncTaskSpec{Name: "queued-job", Lane: "owned", Kind: "test.echo", PayloadVersion: 1})
	require.NoError(t, err)

	require.NoError(t, d.RemoveIdleTask("queued-job"))
	got, err := d.GetTask("queued-job")
	require.NoError(t, err)
	require.Nil(t, got)

	require.ErrorIs(t, d.RemoveIdleTask("does-not-exist"), db.ErrTaskNotFound)
}

func TestPruneOwnedLane_DeletesTaskExecsMetrics(t *testing.T) {
	d := newTestDB(t)
	seedLane(t, d, "owned", 2)

	taskID, execID, err := d.SubmitFuncTask(db.FuncTaskSpec{Name: "finished-job", Lane: "owned", Kind: "test.echo", PayloadVersion: 1})
	require.NoError(t, err)
	claimed, err := d.ClaimFuncExecution(execID, "w1")
	require.NoError(t, err)
	require.True(t, claimed)
	require.NoError(t, d.FinishExecution(execID, "success", nil, 10, 0))
	require.NoError(t, d.RecordMetric(taskID, execID, "success", 10, 0))

	// A pending func task in the same lane must never be pruned.
	pendingTaskID, _, err := d.SubmitFuncTask(db.FuncTaskSpec{Name: "pending-job", Lane: "owned", Kind: "test.echo", PayloadVersion: 1})
	require.NoError(t, err)

	// A shell task in the same lane must never be pruned.
	seedTask(t, d, "shell-in-owned", "owned")

	// A cutoff in the past keeps the finished job.
	n, err := d.PruneOwnedLane("owned", time.Now().Add(-time.Hour))
	require.NoError(t, err)
	require.Equal(t, 0, n)
	got, err := d.GetTask("finished-job")
	require.NoError(t, err)
	require.NotNil(t, got)

	// A cutoff in the future removes the finished job, its execution and metric.
	n, err = d.PruneOwnedLane("owned", time.Now().Add(time.Hour))
	require.NoError(t, err)
	require.Equal(t, 1, n)

	got, err = d.GetTask("finished-job")
	require.NoError(t, err)
	require.Nil(t, got)
	exec, err := d.GetExecution(execID)
	require.NoError(t, err)
	require.Nil(t, exec)
	metrics, err := d.GetMetrics("owned", "", 24*365)
	require.NoError(t, err)
	require.Empty(t, metrics)

	pendingTask, err := d.GetTask("pending-job")
	require.NoError(t, err)
	require.NotNil(t, pendingTask)
	require.Equal(t, pendingTaskID, pendingTask.ID)

	shellStill, err := d.GetTask("shell-in-owned")
	require.NoError(t, err)
	require.NotNil(t, shellStill)
}

func TestEnsureOwnedLane_SeedOnceAndOwnerConflict(t *testing.T) {
	d := newTestDB(t)

	l, err := d.EnsureOwnedLane("owned", "acme-module", 3, 10, false)
	require.NoError(t, err)
	require.Equal(t, "owned", l.Name)
	require.Equal(t, 3, l.Width)
	require.Equal(t, "acme-module", l.Owner)
	require.Equal(t, 10, l.RetentionDays)
	require.False(t, l.Hidden)

	// A second call with the same owner is a no-op, even with different
	// seed values: the DB is authoritative once the lane exists (N7/P8).
	l2, err := d.EnsureOwnedLane("owned", "acme-module", 99, 99, true)
	require.NoError(t, err)
	require.Equal(t, 3, l2.Width)
	require.Equal(t, 10, l2.RetentionDays)
	require.False(t, l2.Hidden)

	// A different owner (including "") conflicts.
	_, err = d.EnsureOwnedLane("owned", "other-module", 1, 0, false)
	require.ErrorIs(t, err, db.ErrLaneOwnedByOther)

	seedLane(t, d, "regular", 2) // owner == "" (a plain config/UI lane)
	_, err = d.EnsureOwnedLane("regular", "acme-module", 1, 0, false)
	require.ErrorIs(t, err, db.ErrLaneOwnedByOther)
	require.Contains(t, err.Error(), "already exists")
}

// ─── Owned-lane visibility filters (N4) ─────────────────────────────────────

func TestListExecutionsExcludingLanes_KeepsDeletedTaskRows(t *testing.T) {
	path := t.TempDir() + "/exclude.db"
	d, err := db.Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { d.Close() })

	seedLane(t, d, "visible", 1)
	seedLane(t, d, "hidden-lane", 1)

	visTask := seedTask(t, d, "vis-task", "visible")
	hidTask := seedTask(t, d, "hid-task", "hidden-lane")
	goneTask := seedTask(t, d, "gone-task", "visible")

	visExec, err := d.CreateExecution(visTask.ID, "w", time.Now())
	require.NoError(t, err)
	require.NoError(t, d.FinishExecution(visExec, "success", nil, 10, 0))

	hidExec, err := d.CreateExecution(hidTask.ID, "w", time.Now())
	require.NoError(t, err)
	require.NoError(t, d.FinishExecution(hidExec, "success", nil, 10, 0))

	goneExec, err := d.CreateExecution(goneTask.ID, "w", time.Now())
	require.NoError(t, err)
	require.NoError(t, d.FinishExecution(goneExec, "success", nil, 10, 0))

	// Simulate a pre-existing orphaned execution row (task_id no longer
	// resolves), by deleting the task directly with FK enforcement off on a
	// second connection, bypassing the ON DELETE CASCADE the primary
	// connection would otherwise apply.
	raw, err := sqlOpenLegacy(path)
	require.NoError(t, err)
	_, err = raw.Exec(`PRAGMA foreign_keys = OFF`)
	require.NoError(t, err)
	_, err = raw.Exec(`DELETE FROM tasks WHERE name = 'gone-task'`)
	require.NoError(t, err)
	require.NoError(t, raw.Close())

	all, err := d.ListExecutionsExcludingLanes("", 50, nil)
	require.NoError(t, err)
	require.Len(t, all, 3, "the orphaned execution is still returned when nothing is excluded")

	filtered, err := d.ListExecutionsExcludingLanes("", 50, []string{"hidden-lane"})
	require.NoError(t, err)
	seen := make(map[int64]bool)
	for _, e := range filtered {
		seen[e.ID] = true
	}
	require.True(t, seen[visExec], "visible lane's execution stays")
	require.True(t, seen[goneExec], "the orphaned execution (no lane) is never excluded")
	require.False(t, seen[hidExec], "hidden lane's execution is excluded")
}

func TestGetMetricsExcludingLanes(t *testing.T) {
	d := newTestDB(t)
	seedLane(t, d, "visible", 1)
	seedLane(t, d, "hidden-lane", 1)

	visTask := seedTask(t, d, "vis-task", "visible")
	hidTask := seedTask(t, d, "hid-task", "hidden-lane")

	visExec, err := d.CreateExecution(visTask.ID, "w", time.Now())
	require.NoError(t, err)
	require.NoError(t, d.RecordMetric(visTask.ID, visExec, "success", 10, 0))

	hidExec, err := d.CreateExecution(hidTask.ID, "w", time.Now())
	require.NoError(t, err)
	require.NoError(t, d.RecordMetric(hidTask.ID, hidExec, "success", 10, 0))

	all, err := d.GetMetrics("", "", 24)
	require.NoError(t, err)
	require.Len(t, all, 2)

	filtered, err := d.GetMetricsExcludingLanes("", "", 24, []string{"hidden-lane"})
	require.NoError(t, err)
	require.Len(t, filtered, 1)
	require.Equal(t, "vis-task", filtered[0].TaskName)
}

// TestGetMetrics_FuncTasksAggregatedByKind pins P17: func-task metrics
// aggregate per (lane, kind), not per task, so N one-shot downloads of the
// same kind show as one card instead of flooding the metrics view.
func TestGetMetrics_FuncTasksAggregatedByKind(t *testing.T) {
	d := newTestDB(t)
	seedLane(t, d, "owned", 3)
	seedLane(t, d, "other", 1)

	var funcTaskNames []string
	for i := 0; i < 3; i++ {
		name := fmt.Sprintf("owned-job-%d", i)
		funcTaskNames = append(funcTaskNames, name)
		taskID, execID, err := d.SubmitFuncTask(db.FuncTaskSpec{Name: name, Lane: "owned", Kind: "test.echo", PayloadVersion: 1})
		require.NoError(t, err)
		require.NoError(t, d.RecordMetric(taskID, execID, "success", 10, 0))
	}

	shellTask := seedTask(t, d, "shell-task", "other")
	shellExec, err := d.CreateExecution(shellTask.ID, "w", time.Now())
	require.NoError(t, err)
	require.NoError(t, d.RecordMetric(shellTask.ID, shellExec, "success", 10, 0))

	rows, err := d.GetMetrics("", "", 24)
	require.NoError(t, err)
	require.Len(t, rows, 2)

	var funcRow, shellRow *models.MetricSummary
	for _, r := range rows {
		if r.Kind == "test.echo" {
			funcRow = r
		} else {
			shellRow = r
		}
	}
	require.NotNil(t, funcRow, "func tasks of the same kind collapse into one aggregate row")
	require.NotNil(t, shellRow)
	require.Equal(t, "test.echo", funcRow.TaskName)
	require.Equal(t, 3, funcRow.SuccessCount)
	require.Equal(t, "shell-task", shellRow.TaskName)
	require.Equal(t, 1, shellRow.SuccessCount)

	// Excluding "owned" removes the func aggregate but keeps the shell row
	// from "other": the hidden-lane exclusion still holds under kind
	// aggregation.
	excluded, err := d.GetMetricsExcludingLanes("", "", 24, []string{"owned"})
	require.NoError(t, err)
	require.Len(t, excluded, 1)
	require.Equal(t, "shell-task", excluded[0].TaskName)

	// Task-detail path: filtering by one func task's own name still
	// resolves to the single aggregate row for its kind.
	detail, err := d.GetMetrics("", funcTaskNames[0], 24)
	require.NoError(t, err)
	require.Len(t, detail, 1)
	require.Equal(t, 1, detail[0].SuccessCount)
	require.Equal(t, "test.echo", detail[0].Kind)
}
