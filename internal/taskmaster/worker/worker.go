package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"cmd184psu/unified-webapp/internal/platform/broker"
	"cmd184psu/unified-webapp/internal/taskmaster/db"
	"cmd184psu/unified-webapp/internal/taskmaster/golane"
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

// beforeFuncClaimHook and afterFuncClaimHook are named test seams (R6): nil
// in production, called by runFunc immediately before/after
// ClaimFuncExecution. Only internal (package worker) tests set them,
// following the hook lifecycle rule (set before Start/runTask is invoked,
// cleared in a t.Cleanup registered so it runs after cancel();w.Wait() —
// LIFO, so the clearing cleanup is registered first).
var beforeFuncClaimHook func(execID int64)
var afterFuncClaimHook func(execID int64)

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

	// Func-task support (plan §4.4), wired by EnableFuncTasks before Start.
	// All are nil-safe: a Worker that never calls EnableFuncTasks never runs
	// a func task and never hides a board event.
	funcs            *FuncRegistry
	progress         *ProgressRegistry
	hidden           *HiddenLanes
	progressInterval time.Duration
	ownedLanesOnly   bool

	missingKindsMu sync.Mutex
	missingKinds   map[string]bool
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

// FuncOptions configures a Worker's Go-function task support. Funcs and
// Progress may be created fresh per Worker; Hidden is normally shared with
// the coordinator so both agree on what's currently hidden.
// OwnedLanesOnly (P16): the worker schedules only lanes with owner != "" —
// used when a module is routed but taskmaster itself is not.
type FuncOptions struct {
	Funcs            *FuncRegistry
	Progress         *ProgressRegistry
	Hidden           *HiddenLanes
	ProgressInterval time.Duration
	OwnedLanesOnly   bool
}

// EnableFuncTasks wires func-task support into the worker. Must be called
// before Start.
func (w *Worker) EnableFuncTasks(o FuncOptions) {
	w.funcs = o.Funcs
	w.progress = o.Progress
	w.hidden = o.Hidden
	w.progressInterval = o.ProgressInterval
	w.ownedLanesOnly = o.OwnedLanesOnly
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
		// P16: a headless engine (module routed, taskmaster itself is not)
		// only schedules owned lanes — a regular config/UI lane never runs
		// anything in that mode.
		if w.ownedLanesOnly && lane.Owner == "" {
			continue
		}

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
		missingKindCounts := map[string]int{}
		for _, t := range eligible {
			if t.LaneName != lane.Name {
				continue
			}
			// A func task whose kind isn't (yet) registered on this
			// process is filtered out here, before toRun below, so it
			// never consumes a slot or blocks a width-1 lane (Q1/FR-U2):
			// it stays pending until the kind is registered.
			if t.Kind != "" && !w.funcs.Has(t.Kind) {
				missingKindCounts[t.Kind]++
				continue
			}
			candidates = append(candidates, t)
		}
		for kind, n := range missingKindCounts {
			w.logMissingKindOnce(kind, n)
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

// logMissingKindOnce logs the "no function registered for kind" message at
// most once per kind per process (not per poll), as required by §7.4.
func (w *Worker) logMissingKindOnce(kind string, n int) {
	w.missingKindsMu.Lock()
	defer w.missingKindsMu.Unlock()
	if w.missingKinds == nil {
		w.missingKinds = make(map[string]bool)
	}
	if w.missingKinds[kind] {
		return
	}
	w.missingKinds[kind] = true
	log.Printf("taskmaster: no function registered for kind %q; %d task(s) waiting", kind, n)
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

		if task.Kind != "" {
			// Func brake path (§4.4): CancelPendingFuncExecution is
			// conditional on status='pending', so whichever caller (this
			// brake sweep or a concurrent explicit cancel) actually flips
			// the row records the metric/event — never both.
			canceled, _, cerr := w.db.CancelPendingFuncExecution(execID)
			if cerr != nil {
				log.Printf("cancel pending func execution %d: %v", execID, cerr)
			}
			if canceled {
				PublishBoardEvent(w.board, w.hidden, BoardEvent{Type: "task-finished", Lane: task.LaneName, Task: task.Name, ExecutionID: execID, Status: "canceled"})
			}
			if lerr := w.db.ReleaseLock(task.ID, w.workerID); lerr != nil {
				log.Printf("release lock for task %d: %v", task.ID, lerr)
			}
			return
		}

		finishedAt := time.Now()
		schedDelay := finishedAt.Sub(scheduledAt).Milliseconds()
		if ferr := w.db.FinishExecution(execID, "canceled", nil, 0, schedDelay); ferr != nil {
			log.Printf("finish execution %d: %v", execID, ferr)
		}
		PublishBoardEvent(w.board, w.hidden, BoardEvent{Type: "task-finished", Lane: task.LaneName, Task: task.Name, ExecutionID: execID, Status: "canceled"})
		if merr := w.db.RecordMetric(task.ID, execID, "canceled", 0, schedDelay); merr != nil {
			log.Printf("record metric for task %d: %v", task.ID, merr)
		}
		if lerr := w.db.ReleaseLock(task.ID, w.workerID); lerr != nil {
			log.Printf("release lock for task %d: %v", task.ID, lerr)
		}
		return
	}

	if task.Kind != "" {
		w.runFunc(task, execID, scheduledAt, execCtx, stdout, stderr)
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

	// startedAt, and the "started"/"running" state this task, are only set
	// once the process has ACTUALLY started (inside onStart below) — not
	// before Execute() is even called. Marking an execution "running" and
	// registering it as controllable must happen at the same moment: doing
	// it earlier (as the reference/earlier version of this code did) leaves
	// a window where the DB and the board say "running" but there is no
	// process yet for pause/resume/cancel to act on — any click landing in
	// that window 404s ("execution not running") deterministically, not as
	// a rare race. Zero window now: onStart fires exactly once Start()
	// succeeded and a PID exists.
	var startedAt time.Time
	var stopHB func()

	onStart := func(pid int) {
		startedAt = time.Now()
		if err := w.db.StartExecution(execID, w.workerID); err != nil {
			log.Printf("start execution %d: %v", execID, err)
		}
		w.procs.Register(execID, pid, task.Sudo)
		// Persisted (not just in the in-memory ProcessRegistry) so that a
		// still-genuinely-alive process can be recognized as such after a
		// restart, instead of every "running" row being assumed dead — see
		// db.OrphanCandidate / worker.ProcessAlive / build.go's boot-time
		// reconciliation. A failure here doesn't stop the task: liveness
		// detection just degrades to "PID unknown", an accepted case.
		if err := w.db.SetExecutionPID(execID, pid); err != nil {
			log.Printf("persist pid for execution %d: %v", execID, err)
		}
		PublishBoardEvent(w.board, w.hidden, BoardEvent{Type: "task-started", Lane: task.LaneName, Task: task.Name, ExecutionID: execID})

		// Heartbeat: while the command is in flight, periodically re-extend
		// this task's lock (acquired with lockTTL in poll()) so a long or
		// suspended run doesn't let it expire — which would let
		// CleanupExpiredLocks + another worker's poll() double-pick the same
		// task. Only meaningful once there is an actual process to protect;
		// stopped deterministically below (goleak-safe).
		stopHB = w.startHeartbeat(task.ID)
	}

	err := w.executor.Execute(execCtx, task, stdoutW, stderrW, onStart)

	// stopHB is nil if onStart never fired (cmd.Start() itself failed) —
	// nothing was ever registered as running, so there is no heartbeat to
	// stop and no "started" state to correct; FinishExecution below still
	// records the failure.
	if stopHB != nil {
		stopHB()
	}

	stdout.MarkDone()
	stderr.MarkDone()
	w.registry.MarkFinished(execID)

	finishedAt := time.Now()
	// startedAt stays zero if the process never actually started (Start()
	// itself failed, before onStart could run) — report 0 rather than a
	// nonsense multi-decade duration computed against the zero time.
	var durationMs, schedDelay int64
	if !startedAt.IsZero() {
		durationMs = finishedAt.Sub(startedAt).Milliseconds()
		schedDelay = startedAt.Sub(scheduledAt).Milliseconds()
	}

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
	PublishBoardEvent(w.board, w.hidden, BoardEvent{Type: "task-finished", Lane: task.LaneName, Task: task.Name, ExecutionID: execID, Status: status})
	if merr := w.db.RecordMetric(task.ID, execID, status, durationMs, schedDelay); merr != nil {
		log.Printf("record metric for task %d: %v", task.ID, merr)
	}
	if lerr := w.db.ReleaseLock(task.ID, w.workerID); lerr != nil {
		log.Printf("release lock for task %d: %v", task.ID, lerr)
	}
}

// startHeartbeat periodically re-extends taskID's lock (acquired with
// lockTTL in poll()) while a command is in flight, so a long or suspended
// run doesn't let it expire. Shared by the shell and func execution paths.
// The returned stop func blocks until the heartbeat goroutine has exited
// (goleak-safe).
func (w *Worker) startHeartbeat(taskID int64) (stop func()) {
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
				if err := w.db.RefreshLock(taskID, w.workerID, lockTTL); err != nil {
					log.Printf("refresh lock for task %d: %v", taskID, err)
				}
			}
		}
	}()
	return func() {
		close(hbStop)
		hbWG.Wait()
	}
}

// runFunc runs a Go-function ("func") task's execution, per the steps in
// plan §4.4. task.Kind != "" is already established by the caller.
func (w *Worker) runFunc(task *models.Task, execID int64, scheduledAt time.Time, execCtx context.Context, stdout, stderr *OutputCapture) {
	releaseNoRun := func() {
		stdout.MarkDone()
		stderr.MarkDone()
		w.registry.MarkFinished(execID)
		if lerr := w.db.ReleaseLock(task.ID, w.workerID); lerr != nil {
			log.Printf("release lock for task %d: %v", task.ID, lerr)
		}
	}

	// Step 1: the kind must be registered. In production this is already
	// filtered out at poll() time, so reaching here with an unregistered
	// kind is a defensive branch (e.g. a kind unregistered mid-flight in a
	// test); still finish it cleanly rather than hang the execution.
	k, ok := w.funcs.Get(task.Kind)
	if !ok {
		stdout.MarkDone()
		stderr.MarkDone()
		w.registry.MarkFinished(execID)
		now := time.Now()
		w.finishFunc(task, execID, "failed", fmt.Sprintf("no function registered for kind %q", task.Kind), nil, nil, 0, now.Sub(scheduledAt).Milliseconds())
		return
	}

	// Step 2: test seam, called before the claim.
	if beforeFuncClaimHook != nil {
		beforeFuncClaimHook(execID)
	}

	// Step 2a: pre-claim shutdown check. execCtx.Err() != nil but this
	// execID was never explicitly canceled means the whole worker is
	// shutting down, not that this job was targeted — leave the row
	// pending so it survives the restart (FR-U2), with no claim, no
	// metric, no event.
	if execCtx.Err() != nil && !w.cancels.WasCanceled(execID) {
		releaseNoRun()
		return
	}

	// Step 3: claim.
	claimed, err := w.db.ClaimFuncExecution(execID, w.workerID)
	if err != nil {
		log.Printf("claim func execution %d: %v", execID, err)
		releaseNoRun()
		return
	}
	if !claimed {
		// The row was canceled or removed while pending — the canceller
		// already recorded the metric/event; nothing to do here.
		releaseNoRun()
		return
	}

	// Step 3a: test seam, called after a successful claim.
	if afterFuncClaimHook != nil {
		afterFuncClaimHook(execID)
	}

	// Step 3b: close the window between runTask's brake check and the
	// claim, where the brake's cancel sweep (running rows only) could not
	// yet see this row, plus the ordinary worker-shutdown case.
	if execCtx.Err() != nil || w.brake.Engaged() {
		status := "failed"
		var errMsg string
		if execCtx.Err() != nil {
			errMsg = execCtx.Err().Error()
			if w.cancels.WasCanceled(execID) {
				status = "canceled"
			}
		} else {
			status = "canceled"
			errMsg = "hand brake engaged"
		}
		stdout.MarkDone()
		stderr.MarkDone()
		w.registry.MarkFinished(execID)
		now := time.Now()
		w.finishFunc(task, execID, status, errMsg, nil, nil, 0, now.Sub(scheduledAt).Milliseconds())
		return
	}

	startedAt := time.Now()
	PublishBoardEvent(w.board, w.hidden, BoardEvent{Type: "task-started", Lane: task.LaneName, Task: task.Name, ExecutionID: execID})
	stopHB := w.startHeartbeat(task.ID)

	// Step 5: decode+validate again at run time (Q2 restart validation) —
	// a row written by an older binary, or hand-edited, fails cleanly
	// instead of crashing.
	p, derr := k.Decode(task.PayloadVersion, task.Payload)
	if derr != nil {
		stopHB()
		stdout.MarkDone()
		stderr.MarkDone()
		w.registry.MarkFinished(execID)
		now := time.Now()
		w.finishFunc(task, execID, "failed", fmt.Sprintf("invalid payload: %v", derr), nil, nil, now.Sub(startedAt).Milliseconds(), startedAt.Sub(scheduledAt).Milliseconds())
		return
	}

	// Step 6: build RunContext.
	reporter := newProgressReporter(execID, task.LaneName, task.Name, w.db, w.progress, w.board, w.hidden, w.progressInterval)
	rc := &runContext{jobID: task.Name, execID: execID, db: w.db, reporter: reporter, logw: stdout}

	// Step 7: run (panic-safe).
	res, runErr := safeRun(k.Run, execCtx, rc, p)
	rc.closed.Store(true)

	// Step 8: close the reporter (persists a final snapshot via
	// FinishFuncExecution below) and stop the heartbeat.
	final := reporter.close()
	stopHB()

	stdout.MarkDone()
	stderr.MarkDone()
	w.registry.MarkFinished(execID)

	finishedAt := time.Now()
	durationMs := finishedAt.Sub(startedAt).Milliseconds()
	schedDelay := startedAt.Sub(scheduledAt).Milliseconds()

	// Step 9: decide the terminal status exactly as the shell path does.
	status := "success"
	var errMsg string
	var resultJSON []byte
	if runErr != nil {
		if w.cancels.WasCanceled(execID) {
			status = "canceled"
		} else {
			status = "failed"
		}
		errMsg = runErr.Error()
	} else {
		b, merr := json.Marshal(res)
		switch {
		case merr != nil:
			status = "failed"
			errMsg = fmt.Sprintf("marshal result: %v", merr)
		case len(b) > golane.MaxPayloadBytes:
			status = "failed"
			errMsg = "result too large"
		default:
			resultJSON = b
		}
	}

	// Step 10: finish.
	w.finishFunc(task, execID, status, errMsg, resultJSON, final, durationMs, schedDelay)
}

// finishFunc closes out a func execution: FinishFuncExecution, the
// task-finished board event, the metric, the lock release and the progress
// registry cleanup — shared by every runFunc exit that got past a
// successful claim (or that finishes without ever claiming, e.g. the
// unregistered-kind branch).
func (w *Worker) finishFunc(task *models.Task, execID int64, status, errMsg string, resultJSON []byte, progress *models.Progress, durationMs, schedDelay int64) {
	var errPtr *string
	if errMsg != "" {
		errPtr = &errMsg
	}
	if ferr := w.db.FinishFuncExecution(execID, status, errPtr, durationMs, schedDelay, resultJSON, progress); ferr != nil {
		log.Printf("finish func execution %d: %v", execID, ferr)
	}
	PublishBoardEvent(w.board, w.hidden, BoardEvent{Type: "task-finished", Lane: task.LaneName, Task: task.Name, ExecutionID: execID, Status: status})
	if merr := w.db.RecordMetric(task.ID, execID, status, durationMs, schedDelay); merr != nil {
		log.Printf("record metric for task %d: %v", task.ID, merr)
	}
	if lerr := w.db.ReleaseLock(task.ID, w.workerID); lerr != nil {
		log.Printf("release lock for task %d: %v", task.ID, lerr)
	}
	if w.progress != nil {
		w.progress.Delete(execID)
	}
}

// safeRun invokes a Kind's Run, recovering a panic into an error (and
// logging the stack) so a single bad callback can't take down the worker
// goroutine.
func safeRun(run func(ctx context.Context, rc golane.RunContext, payload any) (any, error), ctx context.Context, rc golane.RunContext, payload any) (result any, err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("taskmaster: func task (exec %d) panicked: %v\n%s", rc.ExecID(), r, debug.Stack())
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	return run(ctx, rc, payload)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
