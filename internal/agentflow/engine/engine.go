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

func validatePayload(p StagePayload) error {
	if p.Stage <= 0 {
		return errors.New("stage id required")
	}
	return nil
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
	e.backendKinds = []golane.Kind{e.kind(KindWork), e.kind(KindReview), e.kind(KindPlan)}
	e.engineKinds = []golane.Kind{e.kind(KindVerify), e.kind(KindMerge)}
	if err := e.SyncLanes(context.Background()); err != nil {
		return nil, err
	}
	return e, nil
}

func (e *Engine) kind(name string) golane.Kind {
	return golane.NewKind[StagePayload](name, 1, validatePayload, e.run)
}

// run is the lane-agnostic job body. Model roles go through the runner with
// the backend named on the stage row.
func (e *Engine) run(ctx context.Context, rc golane.RunContext, p StagePayload) (any, error) {
	st, err := e.deps.Store.GetStage(ctx, p.Stage)
	if err != nil {
		return nil, err
	}
	if st.Role == "verify" || st.Role == "merge" {
		return map[string]int64{"stage": st.ID}, nil
	}
	b, err := e.deps.Store.GetBackend(ctx, st.Backend)
	if err != nil {
		return nil, err
	}
	res, err := e.deps.Runner.Run(ctx, runner.Request{
		Role:    st.Role,
		Backend: runner.Backend{Name: b.Name, Host: b.Host, APIType: b.APIType},
		Model:   st.Model,
		OnEvent: func(ev runner.Event) { fmt.Fprintln(rc.Log(), ev.Text) },
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{"stage": st.ID, "final": res.Final}, nil
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
