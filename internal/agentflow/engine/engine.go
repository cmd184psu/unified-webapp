// Package engine runs agentflow's work on the shared taskmaster engine: it
// registers the lanes and kinds, and owns the stage runtime built on them. It
// depends on the runner interface, never on a concrete adapter.
package engine

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"cmd184psu/unified-webapp/internal/agentflow/dockervm"
	"cmd184psu/unified-webapp/internal/agentflow/gitsvc"
	"cmd184psu/unified-webapp/internal/agentflow/model"
	"cmd184psu/unified-webapp/internal/agentflow/runner"
	"cmd184psu/unified-webapp/internal/agentflow/sandbox"
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
	// KeyEnv names the environment variable that carries a backend's API key
	// (the frozen config refers to it as {env:NAME}). With it set, each model
	// stage resolves the key when it starts, from the backend's stored key and
	// else from that variable in the server's environment, and hands the
	// runner only that one variable. Nil passes no key.
	KeyEnv func(backend string) string
	// Git is the engine git service. With it set the engine runs the attempt
	// chain (work, verify, review, commit, merge) and, unless overridden
	// below, the built-in verify and merge executors; without it only the
	// stage runtime and the dispatcher run (C2 to C5 tests).
	Git *gitsvc.Service
	// Sandbox is the template for feedback commands: extra read-only binds
	// (a Go module cache), PATH dirs and environment. The engine fills in the
	// worktree and git dirs; feedback commands never get a writable worktree.
	Sandbox sandbox.Spec
	// Containers is the container runtime a docker project needs (AF-12). Nil
	// makes a docker project fail with a clear error instead of silently
	// running on the host.
	Containers *dockervm.Manager
	// DockerProxy is the allowlisting proxy a provider network uses.
	DockerProxy string
	// Verify and Merge run the engine-owned stages. Nil uses the built-in
	// executors when Git is set.
	Verify, Merge Executor
	// Kick wakes the dispatcher after a stage's outcome is written (C5).
	Kick func()
	// Tick, when set, replaces the reconcile ticker (tests); ReconcileEvery is
	// the period otherwise (default 60 s). The engine's one background
	// goroutine, started by Start and stopped by Close, waits on it (DBS5).
	// Reachable reports whether the host of a suspended backend answers (H8);
	// nil does an HTTP GET with a short timeout, tests replace it.
	Reachable      func(ctx context.Context, host string) bool
	Tick           <-chan time.Time
	ReconcileEvery time.Duration
	// DispatchTick, when set, replaces the dispatcher's safety ticker (tests);
	// DispatchEvery is the period otherwise (default 30 s).
	DispatchTick  <-chan time.Time
	DispatchEvery time.Duration
	// PublishRun receives every run-state transition (D3 wires the bus).
	PublishRun func(RunEvent)
	// MaxPolicyViolations is how many git-policy violations halt a task as
	// needs_attention; 0 means DefaultMaxPolicyViolations.
	MaxPolicyViolations int
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
	// Plugins are the project's pinned, opted-in runner plugins (OA3).
	Plugins []runner.Plugin
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
	disp *dispatcher

	// advMu serialises chain advancement (attempt.go); prepMu serialises
	// first-use setup of an effort's integration branch, worktree and
	// baseline.
	advMu  sync.Mutex
	prepMu sync.Mutex

	// retryTimers (guarded by mu) hold the pending Advance of each infra
	// retry waiting out its backoff, by stage id.
	retryTimers map[int64]*time.Timer
	// closed (guarded by mu) stops advanceAfter arming timers after Close.
	closed bool

	health healthState

	// checks tracks the running model checks (checks.go).
	checks checkState
	evals  evalState

	// planMu serialises the planning and compile driver (plan.go,
	// compile.go) with the calls that start or approve them. planWaiting
	// (guarded by mu) says why an effort's next stage is not submitted.
	planMu      sync.Mutex
	planWaiting map[int64]string
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
	e := &Engine{deps: d, handles: map[string]golane.Lane{}, holding: true, retryTimers: map[int64]*time.Timer{}, planWaiting: map[int64]string{}}
	e.runs = newRunControl(e)
	e.disp = newDispatcher(e)
	// Every kick, including the caller's own hook, wakes the dispatcher.
	userKick := d.Kick
	e.deps.Kick = func() {
		if userKick != nil {
			userKick()
		}
		e.disp.kick()
	}
	e.backendKinds = []golane.Kind{e.kind(KindWork), e.kind(KindReview), e.kind(KindPlan)}
	e.engineKinds = []golane.Kind{e.kind(KindVerify), e.kind(KindMerge)}
	e.tickHooks = append(e.tickHooks, func(ctx context.Context) { _ = e.AdvancePlans(ctx) })
	e.tickHooks = append(e.tickHooks, e.watchStalls)
	if d.Git != nil {
		if e.deps.Verify == nil {
			e.deps.Verify = verifyExecutor{e}
		}
		if e.deps.Merge == nil {
			e.deps.Merge = mergeExecutor{e}
		}
		e.tickHooks = append(e.tickHooks, func(ctx context.Context) { _ = e.Advance(ctx) })
	}
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
	e.health.clear(b.Name) // the operator may have fixed what made it unreachable
	return e.SyncLanes(ctx)
}

// DeleteBackend removes a disabled backend that no candidate list names. Its
// lane was paused and emptied when it was disabled and stays hidden; stage
// history keeps the name as text.
func (e *Engine) DeleteBackend(ctx context.Context, actor, name string) error {
	if err := e.deps.Store.DeleteBackend(ctx, actor, name); err != nil {
		return err
	}
	e.health.clear(name)
	return nil
}

// ModelRests lists the models with a rest on record; those whose Until is in the
// future are resting now.
func (e *Engine) ModelRests(ctx context.Context) ([]store.ModelRest, error) {
	return e.deps.Store.ModelRests(ctx)
}

// WakeModel ends a model's rest and its streak at once, and kicks the dispatcher so
// work that waited for it starts. It is ErrNotFound when the model has no rest.
func (e *Engine) WakeModel(ctx context.Context, backend, model string) error {
	ok, err := e.deps.Store.ClearModelRest(ctx, backend, model)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("model %s on %s is not resting: %w", model, backend, store.ErrNotFound)
	}
	if e.deps.Kick != nil {
		e.deps.Kick()
	}
	return nil
}

// ModelHolds lists the held models.
func (e *Engine) ModelHolds(ctx context.Context) ([]store.ModelHold, error) {
	return e.deps.Store.ModelHolds(ctx)
}

// SetModelHold holds a model (its work waits for it while it rests) or releases it.
func (e *Engine) SetModelHold(ctx context.Context, backend, model string, hold bool) error {
	if err := e.deps.Store.SetModelHold(ctx, backend, model, hold); err != nil {
		return err
	}
	if e.deps.Kick != nil {
		e.deps.Kick()
	}
	return nil
}

// EnableBackend re-enables a retired backend and its lane.
func (e *Engine) EnableBackend(ctx context.Context, actor, name string) error {
	if err := e.deps.Store.EnableBackend(ctx, actor, name); err != nil {
		return err
	}
	return e.SyncLanes(ctx)
}
