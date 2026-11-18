package engine

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"cmd184psu/unified-webapp/internal/agentflow/model"
	"cmd184psu/unified-webapp/internal/agentflow/store"
)

// stopWait bounds how long Stop waits for a cancelled job to end before it
// closes the stage itself.
const stopWait = 15 * time.Second

// SystemActor is recorded for transitions nobody asked for (drain finished,
// run-one budget used).
const SystemActor = "system"

// RunEvent is published on every run-state transition (D3 puts it on the bus).
type RunEvent struct {
	Project string `json:"project"`
	Effort  int64  `json:"effort"`
	State   string `json:"state"`
	Action  string `json:"action"`
	Actor   string `json:"actor"`
	// Reason is why a stop happened when the system, not a person, stopped the
	// run ("budget"); empty otherwise.
	Reason string `json:"reason,omitempty"`
}

// DispatchGate is what the dispatcher (C5) asks before starting a task
// attempt for a project (RC1, RC2, QA1).
type DispatchGate interface {
	// Admit reports whether one more attempt may start for project. On true
	// it counts the attempt against a run-one and returns done, which the
	// caller must invoke once the attempt and its first stage exist, so a
	// draining project cannot settle into paused in between.
	Admit(ctx context.Context, project string) (done func(), ok bool)
	// State is the project's in-memory run state.
	State(project string) string
}

// ProjectRun is a project with its run state (PJ2). Remembered is the last
// state persisted on its active effort, shown as "was running before restart";
// it never drives anything.
type ProjectRun struct {
	Project    string `json:"project"`
	State      string `json:"state"`
	Effort     int64  `json:"effort"`
	Remembered string `json:"remembered"`
	// Reason is why the system stopped the run ("budget"), if it did.
	Reason string `json:"reason,omitempty"`
}

type runEntry struct {
	state   string
	oneShot bool
	budget  int
	pending int // admitted attempts whose first stage is not created yet
	// reason is why the run was last stopped by the system; cleared when the
	// run starts again.
	reason string
}

// RunControl holds each project's run controller. States live only in memory
// and every project starts stopped (RC1).
type RunControl struct {
	e    *Engine
	mu   sync.Mutex
	runs map[string]*runEntry
}

var _ DispatchGate = (*RunControl)(nil)

func newRunControl(e *Engine) *RunControl {
	r := &RunControl{e: e, runs: map[string]*runEntry{}}
	e.tickHooks = append(e.tickHooks, func(ctx context.Context) { r.Settle(ctx) })
	return r
}

// Runs returns the engine's run controllers.
func (e *Engine) Runs() *RunControl { return e.runs }

func (r *RunControl) entry(project string) *runEntry {
	en, ok := r.runs[project]
	if !ok {
		en = &runEntry{state: model.RunStopped}
		r.runs[project] = en
	}
	return en
}

// StopReason is why the project's run was stopped by the system ("budget"), or
// "" while it runs or after a person stopped it.
func (r *RunControl) StopReason(project string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if en := r.entry(project); en.state == model.RunStopped {
		return en.reason
	}
	return ""
}

// State returns the project's run state; unknown projects are stopped.
func (r *RunControl) State(project string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.entry(project).state
}

// List returns every project with its run state, stopped ones included (PJ2).
func (r *RunControl) List(ctx context.Context) ([]ProjectRun, error) {
	ps, err := r.e.deps.Store.ListProjects(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]ProjectRun, 0, len(ps))
	for _, p := range ps {
		pr := ProjectRun{Project: p.Slug, State: r.State(p.Slug), Remembered: model.RunStopped, Reason: r.StopReason(p.Slug)}
		if ef, err := r.e.deps.Store.ActiveEffort(ctx, p.Slug); err == nil {
			pr.Effort, pr.Remembered = ef.ID, ef.RunState
		} else if !errors.Is(err, store.ErrNotFound) {
			return nil, err
		}
		out = append(out, pr)
	}
	return out, nil
}

// approvedEffort returns the project's active effort, or ErrConflict unless
// its queue is approved (QA1).
func (r *RunControl) approvedEffort(ctx context.Context, project string) (model.Effort, error) {
	s := r.e.deps.Store
	if _, err := s.GetProject(ctx, project); err != nil {
		return model.Effort{}, err
	}
	ef, err := s.ActiveEffort(ctx, project)
	if errors.Is(err, store.ErrNotFound) {
		return ef, fmt.Errorf("project %q has no active effort: %w", project, store.ErrConflict)
	}
	if err != nil {
		return ef, err
	}
	if _, err := s.ApprovedQueueHash(ctx, ef.ID); errors.Is(err, store.ErrNotFound) {
		return ef, fmt.Errorf("queue of effort %d is not approved: %w", ef.ID, store.ErrConflict)
	} else if err != nil {
		return ef, err
	}
	return ef, nil
}

// record persists a transition for display, audits it with the actor and
// publishes it.
func (r *RunControl) record(ctx context.Context, actor, action, project string, effort int64, state string) error {
	return r.recordWhy(ctx, actor, action, project, effort, state, "")
}

func (r *RunControl) recordWhy(ctx context.Context, actor, action, project string, effort int64, state, reason string) error {
	if reason != "" {
		action += "." + reason
	}
	err := r.e.deps.Store.RecordRunState(context.WithoutCancel(ctx), actor, action, project, effort, state)
	if f := r.e.deps.PublishRun; f != nil {
		f(RunEvent{Project: project, Effort: effort, State: state, Action: action, Actor: actor, Reason: reason})
	}
	return err
}

// budgetOpen refuses to start a run on an effort whose budget is used up.
func (r *RunControl) budgetOpen(ctx context.Context, ef model.Effort) error {
	b, err := r.e.budgetGate(ctx, ef)
	if err != nil {
		return err
	}
	if b.Exhausted() {
		return fmt.Errorf("effort %d has used up its budget: raise it first: %w", ef.ID, store.ErrConflict)
	}
	return nil
}

// Start lets the dispatcher run the project. ErrConflict unless the active
// effort's queue is approved. Starting a pausing or paused project resumes it;
// a running one is left as it is.
func (r *RunControl) Start(ctx context.Context, actor, project string) error {
	ef, err := r.approvedEffort(ctx, project)
	if err != nil {
		return err
	}
	if err := r.budgetOpen(ctx, ef); err != nil {
		return err
	}
	r.mu.Lock()
	en := r.entry(project)
	if en.state == model.RunRunning && !en.oneShot {
		r.mu.Unlock()
		return nil
	}
	en.state, en.oneShot, en.budget, en.reason = model.RunRunning, false, 0, ""
	r.mu.Unlock()
	err = r.record(ctx, actor, "run.start", project, ef.ID, model.RunRunning)
	r.kick()
	return err
}

// RunOne lets the dispatcher start exactly one task's next attempt, then the
// project pauses. Only a stopped or paused project can run one (ErrConflict).
func (r *RunControl) RunOne(ctx context.Context, actor, project string) error {
	ef, err := r.approvedEffort(ctx, project)
	if err != nil {
		return err
	}
	if err := r.budgetOpen(ctx, ef); err != nil {
		return err
	}
	r.mu.Lock()
	en := r.entry(project)
	if en.state != model.RunStopped && en.state != model.RunPaused {
		st := en.state
		r.mu.Unlock()
		return fmt.Errorf("project %q is %s: %w", project, st, store.ErrConflict)
	}
	en.state, en.oneShot, en.budget, en.reason = model.RunRunning, true, 1, ""
	r.mu.Unlock()
	err = r.record(ctx, actor, "run.one", project, ef.ID, model.RunRunning)
	r.kick()
	return err
}

// Pause stops new dispatch; the project becomes paused once its in-flight
// work has drained. A stopped project cannot be paused (ErrConflict).
func (r *RunControl) Pause(ctx context.Context, actor, project string) error {
	r.mu.Lock()
	en := r.entry(project)
	switch en.state {
	case model.RunStopped:
		r.mu.Unlock()
		return fmt.Errorf("project %q is stopped: %w", project, store.ErrConflict)
	case model.RunPausing, model.RunPaused:
		r.mu.Unlock()
		return nil
	}
	en.state, en.oneShot, en.budget = model.RunPausing, false, 0
	r.mu.Unlock()
	if err := r.record(ctx, actor, "run.pause", project, r.effortID(ctx, project), model.RunPausing); err != nil {
		return err
	}
	return r.settle(ctx, project)
}

// effortID is the project's active effort id, or 0.
func (r *RunControl) effortID(ctx context.Context, project string) int64 {
	ef, err := r.e.deps.Store.ActiveEffort(ctx, project)
	if err != nil {
		return 0
	}
	return ef.ID
}

// Admit implements DispatchGate.
func (r *RunControl) Admit(ctx context.Context, project string) (func(), bool) {
	if _, err := r.approvedEffort(ctx, project); err != nil {
		return nil, false
	}
	r.mu.Lock()
	en := r.entry(project)
	if en.state != model.RunRunning || (en.oneShot && en.budget <= 0) {
		r.mu.Unlock()
		return nil, false
	}
	en.pending++
	spent := false
	if en.oneShot {
		en.budget--
		if en.budget == 0 {
			en.state, en.oneShot, spent = model.RunPausing, false, true
		}
	}
	r.mu.Unlock()
	if spent {
		_ = r.record(ctx, SystemActor, "run.pausing", project, r.effortID(ctx, project), model.RunPausing)
	}
	return func() {
		r.mu.Lock()
		en.pending--
		r.mu.Unlock()
		_ = r.settle(context.Background(), project)
	}, true
}

// openWork returns the project's unfinished stages and attempts.
func (r *RunControl) openWork(ctx context.Context, effort int64) ([]model.Stage, []model.Attempt, error) {
	s := r.e.deps.Store
	stages, err := s.ListEffortStages(ctx, effort)
	if err != nil {
		return nil, nil, err
	}
	var open []model.Stage
	for _, st := range stages {
		if st.State != model.StageDone {
			open = append(open, st)
		}
	}
	tasks, err := s.ListTasks(ctx, effort)
	if errors.Is(err, store.ErrNotFound) {
		return open, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	var atts []model.Attempt
	for _, t := range tasks {
		as, err := s.ListAttempts(ctx, t.ID)
		if err != nil {
			return nil, nil, err
		}
		for _, a := range as {
			if a.Outcome != "" {
				continue
			}
			atts = append(atts, a)
			ss, err := s.ListAttemptStages(ctx, a.ID)
			if err != nil {
				return nil, nil, err
			}
			for _, st := range ss {
				if st.State != model.StageDone {
					open = append(open, st)
				}
			}
		}
	}
	return open, atts, nil
}

// Settle moves every pausing project whose in-flight work has drained to
// paused. The dispatcher calls it after each pass; the engine's periodic tick
// calls it too.
func (r *RunControl) Settle(ctx context.Context) {
	r.mu.Lock()
	var pausing []string
	for p, en := range r.runs {
		if en.state == model.RunPausing {
			pausing = append(pausing, p)
		}
	}
	r.mu.Unlock()
	for _, p := range pausing {
		_ = r.settle(ctx, p)
	}
}

func (r *RunControl) settle(ctx context.Context, project string) error {
	r.mu.Lock()
	en := r.entry(project)
	waiting := en.state != model.RunPausing || en.pending > 0
	r.mu.Unlock()
	if waiting {
		return nil
	}
	eff := r.effortID(ctx, project)
	if eff != 0 {
		stages, atts, err := r.openWork(ctx, eff)
		if err != nil || len(stages) > 0 || len(atts) > 0 {
			return err
		}
	}
	r.mu.Lock()
	if en.state != model.RunPausing || en.pending > 0 {
		r.mu.Unlock()
		return nil
	}
	en.state = model.RunPaused
	r.mu.Unlock()
	return r.record(ctx, SystemActor, "run.paused", project, eff, model.RunPaused)
}

// Stop halts the project: nothing new is dispatched, every in-flight stage is
// cancelled through its lane job (the runner's whole process tree dies with
// the job context) and closed as infra:cancel, and open attempts are
// abandoned so they do not count (RC3). Tasks derive ready again.
func (r *RunControl) Stop(ctx context.Context, actor, project string) error {
	return r.stop(ctx, actor, project, "")
}

// stop is Stop with the reason the system stopped the run, if it did.
func (r *RunControl) stop(ctx context.Context, actor, project, reason string) error {
	s := r.e.deps.Store
	if _, err := s.GetProject(ctx, project); err != nil {
		return err
	}
	r.mu.Lock()
	en := r.entry(project)
	en.state, en.oneShot, en.budget, en.reason = model.RunStopped, false, 0, reason
	r.mu.Unlock()
	// An Advance that read the previous state may still be submitting; wait
	// for it so the snapshot below sees its stage. Later passes see stopped.
	r.e.advMu.Lock()
	r.e.advMu.Unlock() //nolint:staticcheck // barrier, not a critical section

	eff := r.effortID(ctx, project)
	if eff != 0 {
		if err := r.cancelInFlight(ctx, eff); err != nil {
			_ = r.recordWhy(ctx, actor, "run.stop", project, eff, model.RunStopped, reason)
			return err
		}
	}
	r.kick()
	return r.recordWhy(ctx, actor, "run.stop", project, eff, model.RunStopped, reason)
}

// finish stops a project whose effort just completed: nothing is left to
// dispatch, so the run state reads stopped (RC1) until a person starts the
// next effort. No stage or attempt is open, so there is nothing to cancel.
func (r *RunControl) finish(ctx context.Context, project string, ef model.Effort) error {
	r.mu.Lock()
	en := r.entry(project)
	was := en.state
	en.state, en.oneShot, en.budget, en.reason = model.RunStopped, false, 0, ""
	r.mu.Unlock()
	if was == model.RunStopped && ef.RunState == model.RunStopped {
		return nil
	}
	return r.record(ctx, SystemActor, "run.complete", project, ef.ID, model.RunStopped)
}

func (r *RunControl) cancelInFlight(ctx context.Context, effort int64) error {
	s := r.e.deps.Store
	r.e.submitMu.Lock() // no stage row without its job while we scan
	stages, _, err := r.openWork(ctx, effort)
	var idx map[int64][]jobRef
	if err == nil {
		idx, err = r.e.indexJobs()
	}
	r.e.submitMu.Unlock()
	if err != nil {
		return err
	}
	var errs []error
	for _, st := range stages {
		for _, ref := range idx[st.ID] {
			if jobTerminal(ref.job.Status) {
				continue
			}
			if _, err := ref.lane.Cancel(ref.job.ID); err != nil {
				errs = append(errs, fmt.Errorf("cancel job %s: %w", ref.job.ID, err))
			}
		}
	}
	deadline := time.Now().Add(stopWait)
	for _, st := range stages {
		for _, ref := range idx[st.ID] {
			for time.Now().Before(deadline) {
				j, err := ref.lane.Get(ref.job.ID)
				if err != nil || jobTerminal(j.Status) {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
		}
		// A queued job never ran, so nobody wrote its outcome.
		cur, err := s.GetStage(ctx, st.ID)
		if err == nil && cur.State != model.StageDone {
			err = s.FinishStage(ctx, st.ID, OutcomeInfraCancel, "stopped by operator", nil)
		}
		if err != nil && !errors.Is(err, store.ErrConflict) {
			errs = append(errs, err)
		}
	}
	_, atts, err := r.openWork(ctx, effort)
	if err != nil {
		return errors.Join(append(errs, err)...)
	}
	for _, a := range atts {
		if err := s.FinishAttempt(ctx, a.ID, model.OutcomeAbandoned, "", ""); err != nil && !errors.Is(err, store.ErrConflict) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (r *RunControl) kick() {
	if k := r.e.deps.Kick; k != nil {
		k()
	}
}
