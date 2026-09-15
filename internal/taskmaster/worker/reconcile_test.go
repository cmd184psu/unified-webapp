package worker_test

import (
	"os/exec"
	"testing"
	"time"

	"cmd184psu/unified-webapp/internal/taskmaster/db"
	"cmd184psu/unified-webapp/internal/taskmaster/models"
	"cmd184psu/unified-webapp/internal/taskmaster/worker"

	"github.com/stretchr/testify/require"
)

func newReconcileTestDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { d.Close() })
	require.NoError(t, d.UpsertLane(&models.Lane{Name: "g", Width: 2}))
	return d
}

func addReconcileTask(t *testing.T, d *db.DB, name string) *models.Task {
	t.Helper()
	task := &models.Task{Name: name, LaneName: "g", Enabled: true, Position: 50, Command: "echo hi"}
	id, err := d.AddTask(task)
	require.NoError(t, err)
	task.ID = id
	return task
}

// TestReconcileOrphans_GenuinelyAliveProcessLeftUntouched is the core proof
// of the owner's requirement: "PIDs of active tasks should be known on
// restart, and if they are, use that." A real OS process's PID is
// registered against a "running" execution — mirroring what a task whose
// parent process just restarted looks like, since a process is isolated
// into its own process group specifically so it survives that — and
// ReconcileOrphans must leave it completely alone: same status, same lock.
func TestReconcileOrphans_GenuinelyAliveProcessLeftUntouched(t *testing.T) {
	d := newReconcileTestDB(t)
	task := addReconcileTask(t, d, "still-alive")

	execID, err := d.CreateExecution(task.ID, "old-hero", time.Now())
	require.NoError(t, err)
	require.NoError(t, d.StartExecution(execID, "old-hero"))
	_, err = d.AcquireLock(task.ID, "old-hero", 10*time.Minute)
	require.NoError(t, err)

	cmd := exec.Command("sleep", "5")
	require.NoError(t, cmd.Start())
	require.NoError(t, d.SetExecutionPID(execID, cmd.Process.Pid))
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()

	skipped, failed, err := worker.ReconcileOrphans(d)
	require.NoError(t, err)
	require.Equal(t, 1, skipped)
	require.Equal(t, 0, failed)

	exec, err := d.GetExecution(execID)
	require.NoError(t, err)
	require.Equal(t, "running", exec.Status, "a genuinely still-alive process must be left exactly as-is")
	require.Nil(t, exec.FinishedAt)

	// The lock must ALSO be untouched — releasing it here, while the real
	// process is still running, would let the next poll cycle double-pick
	// the same task and spawn a second, redundant concurrent instance.
	locked, err := d.AcquireLock(task.ID, "new-hero", 10*time.Minute)
	require.NoError(t, err)
	require.False(t, locked, "the lock for a genuinely-still-running task must not have been released")
}

// TestReconcileOrphans_DeadPIDMarkedFailed covers the common case: the PID
// was persisted, but the process is confirmed gone (the usual outcome after
// kill -9/crash) — reconciliation must close it out, not leave it dangling
// just because a PID happens to be on record.
func TestReconcileOrphans_DeadPIDMarkedFailed(t *testing.T) {
	d := newReconcileTestDB(t)
	task := addReconcileTask(t, d, "actually-dead")

	execID, err := d.CreateExecution(task.ID, "old-hero", time.Now())
	require.NoError(t, err)
	require.NoError(t, d.StartExecution(execID, "old-hero"))
	_, err = d.AcquireLock(task.ID, "old-hero", 10*time.Minute)
	require.NoError(t, err)

	// A real PID that is confirmed reaped/gone: spawn+wait for it to exit.
	cmd := exec.Command("true")
	require.NoError(t, cmd.Run())
	require.NoError(t, d.SetExecutionPID(execID, cmd.Process.Pid))

	skipped, failed, err := worker.ReconcileOrphans(d)
	require.NoError(t, err)
	require.Equal(t, 0, skipped)
	require.Equal(t, 1, failed)

	exec, err := d.GetExecution(execID)
	require.NoError(t, err)
	require.Equal(t, "failed", exec.Status)

	locked, err := d.AcquireLock(task.ID, "new-hero", 10*time.Minute)
	require.NoError(t, err)
	require.True(t, locked, "lock must be released once the execution is confirmed dead")
}

// TestReconcileOrphans_UnknownPIDMarkedFailed is the "acceptable divergence"
// case the owner explicitly signed off on: a row with no persisted PID
// (predates the pid column, or never got far enough to start) can't be
// liveness-checked at all, so it's treated the same as a confirmed-dead one.
func TestReconcileOrphans_UnknownPIDMarkedFailed(t *testing.T) {
	d := newReconcileTestDB(t)
	task := addReconcileTask(t, d, "unknown-pid")

	execID, err := d.CreateExecution(task.ID, "old-hero", time.Now())
	require.NoError(t, err)
	require.NoError(t, d.StartExecution(execID, "old-hero"))
	// Deliberately never call SetExecutionPID.

	skipped, failed, err := worker.ReconcileOrphans(d)
	require.NoError(t, err)
	require.Equal(t, 0, skipped)
	require.Equal(t, 1, failed)

	exec, err := d.GetExecution(execID)
	require.NoError(t, err)
	require.Equal(t, "failed", exec.Status)
}
