package db_test

import (
	"database/sql"
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
