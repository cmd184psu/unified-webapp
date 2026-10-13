// Package engine runs agentflow's work on the shared taskmaster engine: it
// registers the lanes and kinds, and owns the stage runtime built on them. It
// depends on the runner interface, never on a concrete adapter.
package engine

import (
	"context"
	"errors"
	"sync"
	"time"

	"cmd184psu/unified-webapp/internal/agentflow/model"
	"cmd184psu/unified-webapp/internal/agentflow/runner"
	"cmd184psu/unified-webapp/internal/agentflow/store"
	"cmd184psu/unified-webapp/internal/taskmaster/golane"
)

// Kind names. work, review and plan run on backend lanes; verify and merge on
// the engine lanes.
const (
	KindWork   = "agentflow.work"
	KindReview = "agentflow.review"
	KindPlan   = "agentflow.plan"
	KindVerify = "agentflow.verify"
	KindMerge  = "agentflow.merge"
)

// Deps are the engine's collaborators. Clock and DataDir are carried for the
// stage runtime (C2 onwards).
type Deps struct {
	Store  *store.Store
	Host   golane.Host
	Runner runner.Runner
	Clock  func() time.Time
	// DataDir is the root of worktrees/ and stages/.
	DataDir string
	// Prompt builds the prompt a model stage sends. Nil uses the role prompt
	// plus, for attempt stages, the task's prompt and acceptance criteria. The
	// attempt chain and planning loop (C6, C10) supply their own.
	Prompt func(ctx context.Context, st model.Stage) (string, error)
	// FreezeConfig renders and freezes the runner's configuration into the
	// stage directory before the first call (BK5, OC2). The composition root
	// wires the concrete adapter's generator; the engine never imports it.
	// Nil skips freezing, for runners that need no config.
	FreezeConfig func(backend FrozenBackend, stageDir string) (path string, err error)
	// Verify and Merge run the engine-owned stages (C6 fills them in).
	Verify, Merge Executor
	// Kick wakes the dispatcher after a stage's outcome is written (C5).
	Kick func()
	// Tick, when set, replaces the reconcile ticker (tests); ReconcileEvery is
	// the period otherwise (default 60 s). The engine's one background
	// goroutine, started by Start and stopped by Close, waits on it (DBS5).
	Tick           <-chan time.Time
	ReconcileEvery time.Duration
	// PublishRun receives every run-state transition (D3 wires the bus).
	PublishRun func(RunEvent)
	// TestWidthZero registers every agentflow lane at width 0 and never raises
	// it, so no job runs unless a test sets a width itself.
	TestWidthZero bool
}

// StagePayload is what a lane job carries: IDs only (TM2).
type StagePayload struct {
	Project   string `json:"project"`
	Effort    int64  `json:"effort"`
	Task      int64  `json:"task"`
	Stage     int64  `json:"stage"`
	Attempt   int64  `json:"attempt"`
	QueueHash string `json:"queue_hash"`
}

// FrozenBackend is the backend a stage's runner config is frozen from: the
// stage's own backend and the models it may use.
type FrozenBackend struct {
	runner.Backend
	Models []string
}

// Engine owns agentflow's lanes on the shared taskmaster engine.
type Engine struct {
	deps Deps

	// backendKinds and engineKinds are built once; a shared kind's Run comes
	// from its first registration, so they must be lane-agnostic (the backend
	// is resolved from the stage row).
	backendKinds []golane.Kind
	engineKinds  []golane.Kind

	mu      sync.Mutex
	handles map[string]golane.Lane
	// holding keeps every lane paused until Resume (RC4).
	holding bool

	// submitMu makes "create stage row, submit lane job" atomic against a
	// reconcile scan (reconcile.go).
	submitMu sync.RWMutex
	// tickHooks run after every periodic reconcile.
	tickHooks []func(context.Context)
	ticker    *tickerState

	runs *RunControl
}

// New builds the engine, registers its lanes and leaves all of them paused
// (RC4). Call Resume once reconciliation is done.
func New(d Deps) (*Engine, error) {
	if d.Store == nil || d.Host == nil || d.Runner == nil {
		return nil, errors.New("engine: Store, Host and Runner are required")
	}
	if d.Clock == nil {
		d.Clock = time.Now
	}
	e := &Engine{deps: d, handles: map[string]golane.Lane{}, holding: true}
	e.runs = newRunControl(e)
	e.backendKinds = []golane.Kind{e.kind(KindWork), e.kind(KindReview), e.kind(KindPlan)}
	e.engineKinds = []golane.Kind{e.kind(KindVerify), e.kind(KindMerge)}
	if err := e.SyncLanes(context.Background()); err != nil {
		return nil, err
	}
	return e, nil
}

// Resume lifts the engine's hold: active backends' lanes are unpaused, except
// while the brake is engaged or where someone else paused them (S6).
func (e *Engine) Resume(ctx context.Context) error {
	e.mu.Lock()
	e.holding = false
	e.mu.Unlock()
	return e.SyncLanes(ctx)
}

// AddBackend stores a new backend and registers its lane (BK1, BK3).
func (e *Engine) AddBackend(ctx context.Context, actor string, b store.Backend) error {
	if err := e.deps.Store.CreateBackend(ctx, actor, b); err != nil {
		return err
	}
	return e.SyncLanes(ctx)
}

// UpdateBackend edits a backend; a slots change updates the lane width.
func (e *Engine) UpdateBackend(ctx context.Context, actor string, b store.Backend) error {
	if err := e.deps.Store.UpdateBackend(ctx, actor, b); err != nil {
		return err
	}
	return e.SyncLanes(ctx)
}

// RetireBackend pauses and hides the backend's lane, keeping its handle.
func (e *Engine) RetireBackend(ctx context.Context, actor, name string) error {
	if err := e.deps.Store.RetireBackend(ctx, actor, name); err != nil {
		return err
	}
	return e.SyncLanes(ctx)
}

// EnableBackend re-enables a retired backend and its lane.
func (e *Engine) EnableBackend(ctx context.Context, actor, name string) error {
	if err := e.deps.Store.EnableBackend(ctx, actor, name); err != nil {
		return err
	}
	return e.SyncLanes(ctx)
}
