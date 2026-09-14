package worker

import (
	"context"
	"io"
	"testing"
	"time"

	"cmd184psu/unified-webapp/internal/taskmaster/db"
	"cmd184psu/unified-webapp/internal/taskmaster/models"

	"github.com/stretchr/testify/require"
)

// raceMockExecutor records whether Execute was ever invoked, so the test can
// assert the brake pre-start check prevented the command from running.
type raceMockExecutor struct {
	called bool
}

func (m *raceMockExecutor) Execute(ctx context.Context, task *models.Task, stdout, stderr io.Writer, onStart func(pid int)) error {
	m.called = true
	return nil
}

// TestRunTask_BrakeEngagedBeforeStart_RecordsCanceledWithoutExecuting
// exercises the cancel-sweep race directly: poll() can spawn a runTask
// goroutine for a task whose execution row is still 'pending', then the
// hand brake engages (and its sweep of ListRunningExecutionIDs, which only
// sees 'running' rows, misses this one) before the goroutine reaches
// StartExecution/Execute. runTask's pre-start brake check must catch this
// and record "canceled" without ever invoking the executor.
func TestRunTask_BrakeEngagedBeforeStart_RecordsCanceledWithoutExecuting(t *testing.T) {
	d, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { d.Close() })
	require.NoError(t, d.UpsertLane(&models.Lane{Name: "test", Width: 2}))

	task := &models.Task{Name: "race-task", LaneName: "test", Enabled: true, Position: 50, Command: "echo hi"}
	id, err := d.AddTask(task)
	require.NoError(t, err)
	task.ID = id

	exec := &raceMockExecutor{}
	brake := NewBrakeGate(false)
	w := NewWithExecutor(d, NewRegistry(), "w", exec, NewCancelRegistry(), brake, NewProcessRegistry(), nil)
	w.runCtx = context.Background()

	// CreateExecution mirrors what poll() does before spawning runTask: the
	// row is 'pending', not yet 'running', so the brake's cancel-sweep
	// (ListRunningExecutionIDs) would not have seen it.
	execID, err := d.CreateExecution(task.ID, "w", time.Now())
	require.NoError(t, err)

	// The brake engages in the window between poll() spawning this
	// goroutine and it reaching StartExecution/Execute.
	brake.Set(true)

	w.wg.Add(1)
	w.runTask(task, execID, time.Now())

	require.False(t, exec.called, "Execute must not run once the brake is engaged before start")

	execRow, err := d.GetExecution(execID)
	require.NoError(t, err)
	require.Equal(t, "canceled", execRow.Status)
}
