package worker

import (
	"io"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"cmd184psu/unified-webapp/internal/platform/broker"
	"cmd184psu/unified-webapp/internal/taskmaster/db"
	"cmd184psu/unified-webapp/internal/taskmaster/models"
)

// ProgressRegistry is the in-memory, unthrottled "latest progress" for every
// running func execution (Q3(a)): updated on every RunContext.Progress call,
// served by Lane.List/Get and the GET /api/executions overlay. nil-safe.
type ProgressRegistry struct {
	mu sync.RWMutex
	m  map[int64]models.Progress
}

// NewProgressRegistry returns an empty ProgressRegistry.
func NewProgressRegistry() *ProgressRegistry {
	return &ProgressRegistry{m: make(map[int64]models.Progress)}
}

func (r *ProgressRegistry) Set(execID int64, p models.Progress) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.m[execID] = p
}

func (r *ProgressRegistry) Get(execID int64) (models.Progress, bool) {
	if r == nil {
		return models.Progress{}, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.m[execID]
	return p, ok
}

func (r *ProgressRegistry) Delete(execID int64) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.m, execID)
}

// progressPersistHook is a named test seam (R6): nil in production, called
// after each successful persisted (throttled or label-change) progress
// write. Only internal (package worker) tests set it, following the hook
// lifecycle rule (set before Start/runTask, cleared after cancel+Wait/Close,
// LIFO t.Cleanup registration).
var progressPersistHook func(execID int64, p models.Progress)

// progressReporter is the throttled half of the progress pipeline (Q3):
// every Report call updates the unthrottled in-memory registry immediately,
// but persists to the DB (and publishes task-progress) at most once per
// interval, plus immediately on the first report and on any label change
// (so a phase transition like "Converting" is never dropped by the
// leading-edge throttle) and once more at close() (via FinishFuncExecution).
// now is a field (not a direct time.Now call) so internal tests can drive it
// with a fake clock deterministically (R6).
type progressReporter struct {
	mu            sync.Mutex
	closed        bool
	persistedOnce bool
	last          models.Progress
	lastPersist   time.Time
	interval      time.Duration
	now           func() time.Time
	execID        int64
	lane, task    string
	db            *db.DB
	reg           *ProgressRegistry
	board         *broker.Broker
	hidden        *HiddenLanes
}

func newProgressReporter(execID int64, lane, task string, database *db.DB, reg *ProgressRegistry, board *broker.Broker, hidden *HiddenLanes, interval time.Duration) *progressReporter {
	return &progressReporter{
		execID:   execID,
		lane:     lane,
		task:     task,
		db:       database,
		reg:      reg,
		board:    board,
		hidden:   hidden,
		interval: interval,
		now:      time.Now,
	}
}

// Report clamps pct (< 0 => indeterminate; > 100 => 100), truncates label to
// 120 runes, unthrottled-updates the registry, and persists per the rules
// above.
func (p *progressReporter) Report(pct int, label string) {
	var pctPtr *int
	if pct >= 0 {
		v := pct
		if v > 100 {
			v = 100
		}
		pctPtr = &v
	}
	label = truncateRunes(label, 120)

	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return
	}

	prevLabel := p.last.Label
	p.last = models.Progress{Pct: pctPtr, Label: label}
	if p.reg != nil {
		p.reg.Set(p.execID, p.last)
	}

	now := p.now()
	shouldPersist := !p.persistedOnce || label != prevLabel || now.Sub(p.lastPersist) >= p.interval
	if !shouldPersist {
		return
	}
	if err := p.db.SetExecutionProgress(p.execID, p.last); err != nil {
		log.Printf("set execution progress %d: %v", p.execID, err)
	}
	PublishBoardEvent(p.board, p.hidden, BoardEvent{
		Type: "task-progress", Lane: p.lane, Task: p.task, ExecutionID: p.execID,
		ProgressPct: pctPtr, ProgressLabel: label,
	})
	p.lastPersist = now
	p.persistedOnce = true
	if progressPersistHook != nil {
		progressPersistHook(p.execID, p.last)
	}
}

// close marks the reporter closed (any later Report is a no-op) and returns
// the last reported progress, or nil if Report was never called.
func (p *progressReporter) close() *models.Progress {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	if !p.persistedOnce {
		return nil
	}
	out := p.last
	return &out
}

// runContext is the golane.RunContext handed to a Kind's Run. It is a
// no-op after Run returns (its "closed" flag is set by runFunc immediately
// once Run returns, before the reporter itself is closed) — a callback that
// leaked a goroutine calling back into rc can't corrupt a since-finished
// execution's state.
type runContext struct {
	jobID    string
	execID   int64
	db       *db.DB
	reporter *progressReporter
	logw     io.Writer
	closed   atomic.Bool
}

func (rc *runContext) JobID() string { return rc.jobID }
func (rc *runContext) ExecID() int64 { return rc.execID }

func (rc *runContext) Progress(pct int, label string) {
	if rc.closed.Load() {
		return
	}
	rc.reporter.Report(pct, label)
}

// SetLabel retitles the job immediately (P13), truncated to 200 runes.
func (rc *runContext) SetLabel(label string) {
	if rc.closed.Load() {
		return
	}
	label = truncateRunes(label, 200)
	if err := rc.db.UpdateTaskLabel(rc.jobID, label); err != nil {
		log.Printf("update task label for %s: %v", rc.jobID, err)
	}
}

func (rc *runContext) Log() io.Writer {
	if rc.closed.Load() {
		return io.Discard
	}
	return rc.logw
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
