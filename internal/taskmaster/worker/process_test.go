package worker_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"cmd184psu/unified-webapp/internal/taskmaster/models"
	"cmd184psu/unified-webapp/internal/taskmaster/worker"

	"github.com/stretchr/testify/require"
)

// procState reports the single-character process state (R running, S
// sleeping, T stopped, ...). On Linux it reads /proc/<pid>/stat, skipping
// the comm field by locating the last ")" since it can itself contain
// parens; elsewhere (macOS/BSD have no /proc) it falls back to ps, whose
// state column uses the same leading letters.
func procState(pid int) string {
	if data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid)); err == nil {
		s := string(data)
		i := strings.LastIndex(s, ")")
		if i < 0 || i+2 >= len(s) {
			return "?"
		}
		fields := strings.Fields(s[i+2:])
		if len(fields) == 0 {
			return "?"
		}
		return fields[0]
	}
	out, err := exec.Command("ps", "-o", "state=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return "?"
	}
	s := strings.TrimSpace(string(out))
	if s == "" {
		return "?"
	}
	return s[:1]
}

// TestProcessRegistry_SuspendResume_RealProcess drives a real process group
// through TaskExecutor.Execute's onStart hook (Setpgid + PID capture), then
// verifies ProcessRegistry.Suspend/Resume actually stop/continue it.
func TestProcessRegistry_SuspendResume_RealProcess(t *testing.T) {
	te := &worker.TaskExecutor{}
	task := &models.Task{Command: "sleep 5"}

	procs := worker.NewProcessRegistry()
	const execID = int64(1)

	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	pidCh := make(chan int, 1)
	go func() {
		errCh <- te.Execute(ctx, task, io.Discard, io.Discard, func(pid int) {
			procs.Register(execID, pid, false)
			pidCh <- pid
		})
	}()

	var pid int
	select {
	case pid = <-pidCh:
	case <-time.After(5 * time.Second):
		t.Fatal("process did not start within timeout")
	}
	require.NotZero(t, pid, "PID must be captured and non-zero")

	require.Eventually(t, func() bool {
		s := procState(pid)
		return s == "S" || s == "R"
	}, 10*time.Second, 50*time.Millisecond, "process never reached running/sleeping state")

	require.False(t, procs.Suspended(execID))
	require.NoError(t, procs.Suspend(execID))
	require.Eventually(t, func() bool {
		return procState(pid) == "T"
	}, 10*time.Second, 50*time.Millisecond, "process did not stop after Suspend")
	require.True(t, procs.Suspended(execID))

	require.NoError(t, procs.Resume(execID))
	require.Eventually(t, func() bool {
		return procState(pid) != "T"
	}, 10*time.Second, 50*time.Millisecond, "process did not resume after Resume")
	require.False(t, procs.Suspended(execID))

	cancel()
	<-errCh
	procs.Unregister(execID)
}

// TestProcessRegistry_NotRegistered_ReturnsErrProcessNotRunning covers the
// 404 path: Suspend/Resume against an execID that never started (or already
// finished and was unregistered) must fail distinctly from a signal error.
func TestProcessRegistry_NotRegistered_ReturnsErrProcessNotRunning(t *testing.T) {
	procs := worker.NewProcessRegistry()

	err := procs.Suspend(999)
	require.ErrorIs(t, err, worker.ErrProcessNotRunning)

	err = procs.Resume(999)
	require.ErrorIs(t, err, worker.ErrProcessNotRunning)

	_, ok := procs.Get(999)
	require.False(t, ok)
	require.False(t, procs.Suspended(999))
}

// TestProcessRegistry_SignalFailure_IsDistinctFromNotRegistered registers a
// PID that (essentially certainly) doesn't correspond to a running process,
// so the underlying kill fails with ESRCH — a signal failure, which callers
// must be able to tell apart from "not registered" (404 vs 4xx).
func TestProcessRegistry_SignalFailure_IsDistinctFromNotRegistered(t *testing.T) {
	procs := worker.NewProcessRegistry()
	procs.Register(42, 1<<30, false)

	err := procs.Suspend(42)
	require.Error(t, err)
	require.False(t, errors.Is(err, worker.ErrProcessNotRunning), "a registered-but-unsignalable process must not report ErrProcessNotRunning")
}

// TestProcessRegistry_NilSafe mirrors CancelRegistry's nil-receiver
// tolerance so a nil ProcessRegistry (as might appear in a minimal test
// wiring) never panics.
func TestProcessRegistry_NilSafe(t *testing.T) {
	var procs *worker.ProcessRegistry
	procs.Register(1, 100, false)
	procs.Unregister(1)
	_, ok := procs.Get(1)
	require.False(t, ok)
	require.False(t, procs.Suspended(1))
	require.ErrorIs(t, procs.Suspend(1), worker.ErrProcessNotRunning)
	require.ErrorIs(t, procs.Resume(1), worker.ErrProcessNotRunning)
}
