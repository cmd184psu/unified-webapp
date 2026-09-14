package worker

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"cmd184psu/unified-webapp/internal/platform/broker"
	"cmd184psu/unified-webapp/internal/taskmaster/db"
	"cmd184psu/unified-webapp/internal/taskmaster/models"
)

// pollInterval controls how often the worker checks for eligible tasks.
// Unexported so tests can shorten it via SetPollIntervalForTest; production
// code always uses the 5s default.
var pollInterval = 5 * time.Second

// SetPollIntervalForTest overrides pollInterval for tests. Not for production use.
func SetPollIntervalForTest(d time.Duration) {
	pollInterval = d
}

// lockTTL is how long AcquireLock's row is valid for before
// CleanupExpiredLocks reaps it. Unexported var (not const) so tests can
// shorten it via SetLockTTLForTest to exercise the heartbeat without waiting
// 10 real minutes; production code always uses the 10-minute default.
var lockTTL = 10 * time.Minute

// SetLockTTLForTest overrides lockTTL for tests. Not for production use.
func SetLockTTLForTest(d time.Duration) {
	lockTTL = d
}

type Worker struct {
	db       *db.DB
	registry *OutputRegistry
	executor Executor
	workerID string
	wg       sync.WaitGroup
	runCtx   context.Context
	cancels  *CancelRegistry
	brake    *BrakeGate
	board    *broker.Broker
	procs    *ProcessRegistry
}

// New builds a Worker with the standard TaskExecutor, wiring the shared
// sudo gate into the executor's sudo-gating policy. cancels is the shared
// per-execution cancel registry (also wired into the coordinator's cancel
// endpoint), brake is the shared hand-brake gate, procs is the shared
// per-execution process registry (also wired into the coordinator's
// pause/resume endpoints), and board is the shared board-events broker (also
// wired into the coordinator; GET /api/board/events streams what it
// publishes). board may be nil in tests that don't care about board events.
func New(database *db.DB, registry *OutputRegistry, workerID string, sudo *SudoGate, cancels *CancelRegistry, brake *BrakeGate, procs *ProcessRegistry, board *broker.Broker) *Worker {
	return &Worker{
		db:       database,
		registry: registry,
		executor: &TaskExecutor{Sudo: sudo},
		workerID: workerID,
		cancels:  cancels,
		brake:    brake,
		procs:    procs,
		board:    board,
	}
}

// NewWithExecutor allows injecting a mock executor for testing.
func NewWithExecutor(database *db.DB, registry *OutputRegistry, workerID string, exec Executor, cancels *CancelRegistry, brake *BrakeGate, procs *ProcessRegistry, board *broker.Broker) *Worker {
	return &Worker{db: database, registry: registry, executor: exec, workerID: workerID, cancels: cancels, brake: brake, procs: procs, board: board}
}

func (w *Worker) Start(ctx context.Context) {
	w.runCtx = ctx
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := w.poll(); err != nil {
				log.Printf("worker poll error: %v", err)
			}
		}
	}
}

// Wait blocks until all in-flight runTask goroutines have returned.
func (w *Worker) Wait() {
	w.wg.Wait()
}

func (w *Worker) poll() error {
	if err := w.db.CleanupExpiredLocks(); err != nil {
		log.Printf("cleanup locks: %v", err)
	}

	if w.brake.Engaged() {
		return nil
	}

	eligible, err := w.db.GetEligibleTasks()
	if err != nil {
		return err
	}
	if len(eligible) == 0 {
		return nil
	}

	lanes, err := w.db.ListLanes()
	if err != nil {
		return err
	}

	for _, lane := range lanes {
		running, err := w.db.CountRunningInLane(lane.Name)
		if err != nil {
			log.Printf("count running in %s: %v", lane.Name, err)
			continue
		}
		slots := lane.Width - running
		if slots <= 0 {
			continue
		}

		var candidates []*models.Task
		for _, t := range eligible {
			if t.LaneName == lane.Name {
				candidates = append(candidates, t)
			}
		}

		toRun := min(slots, len(candidates))
		for i := 0; i < toRun; i++ {
			task := candidates[i]
			ok, err := w.db.AcquireLock(task.ID, w.workerID, lockTTL)
			if err != nil || !ok {
				continue
			}
			now := time.Now()
			// Reuse an existing pending execution (e.g. from EnqueueTask) if
			// one exists, rather than creating a second pending record that
			// would be orphaned and cause infinite re-runs.
			var execID int64
			pending, perr := w.db.GetPendingExecution(task.ID)
			if perr != nil {
				w.db.ReleaseLock(task.ID, w.workerID)
				log.Printf("get pending execution for %s: %v", task.Name, perr)
				continue
			}
			if pending != nil {
				execID = pending.ID
			} else {
				execID, err = w.db.CreateExecution(task.ID, w.workerID, now)
				if err != nil {
					w.db.ReleaseLock(task.ID, w.workerID)
					log.Printf("create execution for %s: %v", task.Name, err)
					continue
				}
			}
			w.wg.Add(1)
			go w.runTask(task, execID, now)
		}
	}
	return nil
}

func (w *Worker) runTask(task *models.Task, execID int64, scheduledAt time.Time) {
	defer w.wg.Done()

	// Give this execution its own cancelable child context, registered by
	// execID so a per-execution cancel (or the hand brake) can kill just
	// this command without tearing down the whole worker. Unregistering
	// clears the registry entry; calling cancel releases the context's
	// resources regardless of how Execute returned.
	execCtx, cancel := context.WithCancel(w.runCtx)
	w.cancels.Register(execID, cancel)
	defer func() {
		w.cancels.Unregister(execID)
		w.procs.Unregister(execID)
		cancel()
	}()

	stdout, stderr := w.registry.Register(execID)

	// Close the race between the hand brake's cancel-sweep (which only sees
	// executions already flipped to 'running') and this goroutine, which was
	// spawned by poll() before the brake check but hasn't started its
	// command yet. If the brake engaged in that window, this execution's
	// context was never registered with a running command to cancel — bail
	// out here instead of starting it, and record it as canceled rather than
	// letting it run to completion despite the brake.
	if w.brake.Engaged() {
		stdout.MarkDone()
		stderr.MarkDone()
		w.registry.MarkFinished(execID)
		w.registry.Unregister(execID)

		finishedAt := time.Now()
		schedDelay := finishedAt.Sub(scheduledAt).Milliseconds()
		if ferr := w.db.FinishExecution(execID, "canceled", nil, 0, schedDelay); ferr != nil {
			log.Printf("finish execution %d: %v", execID, ferr)
		}
		PublishBoardEvent(w.board, BoardEvent{Type: "task-finished", Lane: task.LaneName, Task: task.Name, ExecutionID: execID, Status: "canceled"})
		if merr := w.db.RecordMetric(task.ID, execID, "canceled", 0, schedDelay); merr != nil {
			log.Printf("record metric for task %d: %v", task.ID, merr)
		}
		if lerr := w.db.ReleaseLock(task.ID, w.workerID); lerr != nil {
			log.Printf("release lock for task %d: %v", task.ID, lerr)
		}
		return
	}

	var stdoutW, stderrW io.Writer = stdout, stderr
	if task.OutputFile != "" {
		path := strings.ReplaceAll(task.OutputFile, "{exec_id}", strconv.FormatInt(execID, 10))
		path = strings.ReplaceAll(path, "{task}", task.Name)
		if dir := filepath.Dir(path); dir != "." {
			if err := os.MkdirAll(dir, 0755); err != nil {
				log.Printf("create output dir %s: %v", dir, err)
			}
		}
		f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			log.Printf("open output file %s: %v", path, err)
		} else {
			defer f.Close()
			fmt.Fprintf(f, "--- exec %d  task %s  started %s ---\n",
				execID, task.Name, time.Now().Format(time.RFC3339))
			stdoutW = io.MultiWriter(stdout, f)
			stderrW = io.MultiWriter(stderr, f)
		}
	}

	startedAt := time.Now()
	if err := w.db.StartExecution(execID, w.workerID); err != nil {
		log.Printf("start execution %d: %v", execID, err)
	}
	PublishBoardEvent(w.board, BoardEvent{Type: "task-started", Lane: task.LaneName, Task: task.Name, ExecutionID: execID})

	// Heartbeat: while the command is in flight, periodically re-extend this
	// task's lock (acquired with lockTTL in poll()) so a long or suspended
	// run doesn't let it expire — which would let CleanupExpiredLocks +
	// another worker's poll() double-pick the same task. Stopped
	// deterministically (hbWG.Wait) before runTask returns, so it never
	// leaks past this goroutine (goleak-safe).
	hbStop := make(chan struct{})
	var hbWG sync.WaitGroup
	hbWG.Add(1)
	go func() {
		defer hbWG.Done()
		ticker := time.NewTicker(lockTTL / 3)
		defer ticker.Stop()
		for {
			select {
			case <-hbStop:
				return
			case <-ticker.C:
				if err := w.db.RefreshLock(task.ID, w.workerID, lockTTL); err != nil {
					log.Printf("refresh lock for task %d: %v", task.ID, err)
				}
			}
		}
	}()
	defer func() {
		close(hbStop)
		hbWG.Wait()
	}()

	err := w.executor.Execute(execCtx, task, stdoutW, stderrW, func(pid int) {
		w.procs.Register(execID, pid, task.Sudo)
	})

	stdout.MarkDone()
	stderr.MarkDone()
	w.registry.MarkFinished(execID)

	finishedAt := time.Now()
	durationMs := finishedAt.Sub(startedAt).Milliseconds()
	schedDelay := startedAt.Sub(scheduledAt).Milliseconds()

	status := "success"
	var errMsg *string
	if err != nil {
		// An explicit per-execution or hand-brake cancel (routed through
		// w.cancels.Cancel) records "canceled" — a distinct terminal state
		// from "failed" so metrics/history don't conflate the two. A whole-
		// worker shutdown cancels w.runCtx directly (not via w.cancels), so
		// it still records "failed", preserving existing shutdown semantics.
		if w.cancels.WasCanceled(execID) {
			status = "canceled"
		} else {
			status = "failed"
		}
		s := err.Error()
		errMsg = &s
	}

	if ferr := w.db.FinishExecution(execID, status, errMsg, durationMs, schedDelay); ferr != nil {
		log.Printf("finish execution %d: %v", execID, ferr)
	}
	PublishBoardEvent(w.board, BoardEvent{Type: "task-finished", Lane: task.LaneName, Task: task.Name, ExecutionID: execID, Status: status})
	if merr := w.db.RecordMetric(task.ID, execID, status, durationMs, schedDelay); merr != nil {
		log.Printf("record metric for task %d: %v", task.ID, merr)
	}
	if lerr := w.db.ReleaseLock(task.ID, w.workerID); lerr != nil {
		log.Printf("release lock for task %d: %v", task.ID, lerr)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
