package engine

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"cmd184psu/unified-webapp/internal/agentflow/model"
	"cmd184psu/unified-webapp/internal/agentflow/store"
)

const defaultDispatchEvery = 30 * time.Second

// Pick is the backend and model chosen for a stage.
type Pick struct {
	Backend string
	Model   string
}

// dispatcher is the engine's single submitting goroutine (TM4). It only
// creates attempts and submits their first stage; it never executes anything.
// Each pass reads one store Snapshot (SV2).
type dispatcher struct {
	e    *Engine
	wake chan struct{}

	// passMu serialises passes (the goroutine and DispatchOnce) and guards
	// cursor, the slug of the project served last (PJ3).
	passMu sync.Mutex
	cursor string

	mu   sync.Mutex
	stop chan struct{}
	done chan struct{}
}

func newDispatcher(e *Engine) *dispatcher {
	return &dispatcher{e: e, wake: make(chan struct{}, 1)}
}

// kick wakes the goroutine; kicks coalesce.
func (d *dispatcher) kick() {
	select {
	case d.wake <- struct{}{}:
	default:
	}
}

// start launches the goroutine: one wait on the kick channel and the safety
// tick, one pass per wake.
func (d *dispatcher) start() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.stop != nil {
		return
	}
	stop, done := make(chan struct{}), make(chan struct{})
	d.stop, d.done = stop, done
	ticks := d.e.deps.DispatchTick
	var stopTicker func()
	if ticks == nil {
		every := d.e.deps.DispatchEvery
		if every <= 0 {
			every = defaultDispatchEvery
		}
		t := time.NewTicker(every)
		ticks, stopTicker = t.C, t.Stop
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		defer close(done)
		defer cancel()
		if stopTicker != nil {
			defer stopTicker()
		}
		for {
			select {
			case <-stop:
				return
			case <-d.wake:
			case <-ticks:
			}
			_ = d.pass(ctx)
			d.e.runs.Settle(ctx)
		}
	}()
	d.kick()
}

func (d *dispatcher) halt() {
	d.mu.Lock()
	stop, done := d.stop, d.done
	d.stop, d.done = nil, nil
	d.mu.Unlock()
	if stop == nil {
		return
	}
	close(stop)
	<-done
}

// DispatchOnce runs one dispatcher pass synchronously, then settles draining
// projects. The goroutine does the same on every wake; tests and callers that
// want a pass now use this.
func (e *Engine) DispatchOnce(ctx context.Context) error {
	err := e.disp.pass(ctx)
	e.runs.Settle(ctx)
	return err
}

// view is what one pass knows: the configuration snapshot and the free slots
// of every backend, reduced as the pass places stages.
type view struct {
	snap store.Snapshot
	free map[string]int
}

func (e *Engine) newView(ctx context.Context) (*view, error) {
	snap, err := e.deps.Store.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	open, err := e.deps.Store.OpenStages(ctx)
	if err != nil {
		return nil, err
	}
	v := &view{snap: snap, free: map[string]int{}}
	for _, b := range snap.Backends {
		if !b.Retired {
			v.free[b.Name] = b.Slots
		}
	}
	for _, st := range open {
		if _, ok := v.free[st.Backend]; ok {
			v.free[st.Backend]--
		}
	}
	return v, nil
}

// pass places stages for every running project, round-robin from the cursor,
// one task per project per round, until nothing more can be placed.
func (d *dispatcher) pass(ctx context.Context) error {
	d.passMu.Lock()
	defer d.passMu.Unlock()
	v, err := d.e.newView(ctx)
	if err != nil {
		return err
	}
	var running []store.Project
	for _, p := range v.snap.Projects {
		if d.e.runs.State(p.Slug) == model.RunRunning {
			running = append(running, p)
		}
	}
	sort.Slice(running, func(i, j int) bool { return running[i].Slug < running[j].Slug })
	start := sort.Search(len(running), func(i int) bool { return running[i].Slug > d.cursor })
	order := append(slices.Clone(running[start:]), running[:start]...)
	var errs []error
	for len(order) > 0 {
		var next []store.Project
		for _, p := range order {
			placed, err := d.e.dispatchOne(ctx, v, p)
			if err != nil {
				errs = append(errs, fmt.Errorf("project %s: %w", p.Slug, err))
			}
			if placed {
				d.cursor = p.Slug
				next = append(next, p)
			}
		}
		order = next
	}
	return errors.Join(errs...)
}

// dispatchOne places the next eligible task of the project: it picks a
// backend (waiting_reason recorded on the task when none is eligible),
// then, with the run controller's admission, opens the attempt and submits its
// work stage. It reports whether a stage was submitted.
func (e *Engine) dispatchOne(ctx context.Context, v *view, p store.Project) (bool, error) {
	s := e.deps.Store
	ef, err := s.ActiveEffort(ctx, p.Slug)
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	hash, err := s.ApprovedQueueHash(ctx, ef.ID)
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	tasks, err := s.ListTasks(ctx, ef.ID)
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	eff := v.snap.Effective[p.Slug]
	status, err := s.TaskStatuses(ctx, ef.ID, e.limitsFor(eff))
	if err != nil {
		return false, err
	}
	var inflight []model.Task
	for _, t := range tasks {
		if status[t.Key] == model.StatusRunning {
			inflight = append(inflight, t)
		}
	}
	var errs []error
	for _, t := range tasks {
		if st := status[t.Key]; (st != model.StatusReady && st != model.StatusRepair) || coScheduleConflict(tasks, t, inflight) {
			continue
		}
		sticky, err := e.stickyReviewer(ctx, t)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		pk, reason := pickWorker(v, p, sticky)
		if reason != "" {
			errs = append(errs, e.setWaiting(ctx, t, reason))
			continue
		}
		done, ok := e.runs.Admit(ctx, p.Slug)
		if !ok {
			return false, errors.Join(errs...)
		}
		err = e.submitWork(ctx, v, ef, hash, p, t, pk, status[t.Key] == model.StatusRepair)
		done()
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if ef.Status == model.EffortQueueAppr {
			st := model.EffortRunning
			if _, err := s.UpdateEffort(ctx, SystemActor, ef.ID, store.EffortPatch{Status: &st}); err != nil {
				errs = append(errs, err)
			}
		}
		return true, errors.Join(errs...)
	}
	return false, errors.Join(errs...)
}

// submitWork opens the task's attempt and submits its work stage on the
// chosen backend's lane. The worktree path is the task's planned location in
// gitsvc's layout; the work stage creates the worktree when it starts (C6). A
// repair attempt (AT2) is the same call with kind repair: the planned path is
// the previous attempt's worktree, which the work stage reuses.
func (e *Engine) submitWork(ctx context.Context, v *view, ef model.Effort, hash string, p store.Project, t model.Task, pk Pick, repair bool) error {
	s := e.deps.Store
	wt := e.taskWorktreePath(p.Slug, ef.ID, t)
	kind := model.AttemptNormal
	if repair {
		kind = model.AttemptRepair
	}
	att, err := s.StartAttempt(ctx, t.ID, kind, wt, pk.Model)
	if err != nil {
		return err
	}
	_, _, err = e.Submit(ctx, BackendLane(pk.Backend),
		store.StageSpec{Attempt: att.ID, Role: "worker", Backend: pk.Backend, Model: pk.Model, QueueHash: hash},
		StagePayload{Project: p.Slug, Effort: ef.ID, Task: t.ID, Attempt: att.ID, QueueHash: hash})
	if err != nil {
		// Nothing runs: close the attempt so the task is not left "running".
		_ = s.FinishAttempt(context.WithoutCancel(ctx), att.ID, model.OutcomeAbandoned, "", "")
		return err
	}
	v.free[pk.Backend]--
	if t.WaitingReason != "" {
		return s.SetWaitingReason(ctx, t.ID, "")
	}
	return nil
}

func (e *Engine) setWaiting(ctx context.Context, t model.Task, reason string) error {
	if t.WaitingReason == reason {
		return nil
	}
	return e.deps.Store.SetWaitingReason(ctx, t.ID, reason)
}

// pickWorker chooses the worker backend. A worker is only taken when a
// reviewer other than it could still be picked (MB6), so a one-model pool
// waits instead of dispatching; the reviewer itself is picked when its stage
// is submitted.
func pickWorker(v *view, p store.Project, sticky string) (Pick, string) {
	var reasons []string
	for _, c := range v.snap.CandidatesFor("worker", p.Slug) {
		if why := skipReason(v, p, c, true); why != "" {
			reasons = append(reasons, why)
			continue
		}
		if _, why := reviewerFor(v, p, sticky, c.Model, false); why != "" {
			return Pick{}, why
		}
		return Pick{Backend: c.Backend, Model: c.Model}, ""
	}
	return Pick{}, noEligible("worker", reasons)
}

// pickBackend chooses the backend and model for a stage of role for task t of
// project p, walking the role's candidates in order (MB4). A reviewer is
// sticky per task (AT3) and never the worker's model (MB6); workerModel is
// the attempt's worker model. It reads a fresh snapshot and the in-flight
// stages from the DB; the string is empty on success and otherwise the reason
// no candidate is eligible, which is also recorded on the task (SV3). C6 calls
// it when chaining work to review. The caller must make sure a successful
// pick is submitted before the next call, since slots are counted from open
// stages.
func (e *Engine) pickBackend(ctx context.Context, role string, t model.Task, p store.Project, workerModel string) (Pick, string, error) {
	v, err := e.newView(ctx)
	if err != nil {
		return Pick{}, "", err
	}
	var pk Pick
	var reason string
	if role == "reviewer" {
		sticky, err := e.stickyReviewer(ctx, t)
		if err != nil {
			return Pick{}, "", err
		}
		pk, reason = reviewerFor(v, p, sticky, workerModel, true)
	} else {
		var reasons []string
		for _, c := range v.snap.CandidatesFor(role, p.Slug) {
			if why := skipReason(v, p, c, true); why != "" {
				reasons = append(reasons, why)
				continue
			}
			pk = Pick{Backend: c.Backend, Model: c.Model}
			break
		}
		if pk == (Pick{}) {
			reason = noEligible(role, reasons)
		}
	}
	if reason != "" {
		return Pick{}, reason, e.setWaiting(ctx, t, reason)
	}
	return pk, "", nil
}

// reviewerFor walks the reviewer candidates. needSlot false only asks whether
// some reviewer could ever be picked, ignoring free slots (the work stage has
// not used its slot yet, and a slot is a transient condition).
func reviewerFor(v *view, p store.Project, sticky, workerModel string, needSlot bool) (Pick, string) {
	cands := v.snap.CandidatesFor("reviewer", p.Slug)
	if sticky != "" {
		for _, c := range cands {
			if c.Model == sticky && skipReviewer(v, p, c, workerModel, needSlot) == "" {
				return Pick{Backend: c.Backend, Model: c.Model}, ""
			}
		}
	}
	var reasons []string
	for _, c := range cands {
		if why := skipReviewer(v, p, c, workerModel, needSlot); why != "" {
			reasons = append(reasons, why)
			continue
		}
		return Pick{Backend: c.Backend, Model: c.Model}, ""
	}
	return Pick{}, noEligible("reviewer", reasons)
}

func skipReviewer(v *view, p store.Project, c store.Candidate, workerModel string, needSlot bool) string {
	if c.Model == workerModel {
		return "reviewer model equals worker model"
	}
	return skipReason(v, p, c, needSlot)
}

// stickyReviewer is the reviewer model of the task's earliest attempt that
// had one, or "" (AT3).
func (e *Engine) stickyReviewer(ctx context.Context, t model.Task) (string, error) {
	atts, err := e.deps.Store.ListAttempts(ctx, t.ID)
	if err != nil {
		return "", err
	}
	for _, a := range atts {
		if a.ReviewerModel != "" {
			return a.ReviewerModel, nil
		}
	}
	return "", nil
}

// skipReason says why candidate c cannot be used, or "" when it can. A
// candidate exists only because it was listed explicitly, so a paid model is
// used only when named (MB5); this also guards a candidate whose backend
// changed since it was listed.
func skipReason(v *view, p store.Project, c store.Candidate, needSlot bool) string {
	var b *store.Backend
	for i := range v.snap.Backends {
		if v.snap.Backends[i].Name == c.Backend {
			b = &v.snap.Backends[i]
		}
	}
	switch {
	case b == nil || b.Retired:
		return "backend " + c.Backend + " is unavailable"
	case b.Hosted && p.ModelScope == "local":
		return "hosted under local scope"
	case needSlot && v.free[c.Backend] <= 0:
		return "no free slot on " + c.Backend
	}
	return ""
}

// noEligible words the reason a role has no usable candidate; when every
// candidate was skipped for the same reason it says so.
func noEligible(role string, reasons []string) string {
	if len(reasons) == 0 {
		return "no eligible backend for role " + role + ": no candidates configured"
	}
	uniq := reasons[:0:0]
	for _, r := range reasons {
		if !slices.Contains(uniq, r) {
			uniq = append(uniq, r)
		}
	}
	if len(uniq) == 1 && len(reasons) > 1 {
		return fmt.Sprintf("no eligible backend for role %s: all candidates %s", role, strings.TrimPrefix(uniq[0], "no "))
	}
	return fmt.Sprintf("no eligible backend for role %s: %s", role, strings.Join(uniq, "; "))
}

// coScheduleConflict reports whether a dependency path joins t and any
// in-flight task, in either direction (PX4).
func coScheduleConflict(tasks []model.Task, t model.Task, inflight []model.Task) bool {
	deps := map[string][]string{}
	for _, x := range tasks {
		deps[x.Key] = x.DependsOn
	}
	for _, o := range inflight {
		if o.Key != t.Key && (reaches(deps, t.Key, o.Key) || reaches(deps, o.Key, t.Key)) {
			return true
		}
	}
	return false
}

// reaches reports whether from depends, directly or not, on to.
func reaches(deps map[string][]string, from, to string) bool {
	seen := map[string]bool{}
	stack := []string{from}
	for len(stack) > 0 {
		k := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, d := range deps[k] {
			if d == to {
				return true
			}
			if !seen[d] {
				seen[d] = true
				stack = append(stack, d)
			}
		}
	}
	return false
}
