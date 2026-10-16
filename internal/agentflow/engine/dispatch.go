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
	// down are the backends in their unreachable cooldown (MB7).
	down map[string]bool
	// modelFree is the remaining per-model concurrency (PX5) of every model
	// that has a limit, keyed by modelKey.
	modelFree map[string]int
	// strikes are the uncleared strikes per model and role (H7); StrikeLimit of them benches the model.
	strikes map[store.StrikeKey]int
	// avoid are models to pass over for this pick, for one retry: the model whose last reply was unreadable (H11).
	avoid map[string]bool
}

func modelKey(backend, model string) string { return backend + "\x00" + model }

func (e *Engine) newView(ctx context.Context) (*view, error) {
	snap, err := e.deps.Store.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	open, err := e.deps.Store.OpenStages(ctx)
	if err != nil {
		return nil, err
	}
	strikes, err := e.deps.Store.StrikeCounts(ctx)
	if err != nil {
		return nil, err
	}
	v := &view{snap: snap, free: map[string]int{}, down: map[string]bool{}, modelFree: map[string]int{}, strikes: strikes}
	for b := range e.health.active(e.deps.Clock()) {
		v.down[b] = true
	}
	for _, b := range snap.Backends {
		if !b.Retired {
			v.free[b.Name] = b.Slots
			for _, m := range b.Models {
				if m.MaxConcurrent > 0 {
					v.modelFree[modelKey(b.Name, m.Model)] = m.MaxConcurrent
				}
			}
		}
	}
	for _, st := range open {
		if _, ok := v.free[st.Backend]; ok {
			v.free[st.Backend]--
		}
		if _, ok := v.modelFree[modelKey(st.Backend, st.Model)]; ok {
			v.modelFree[modelKey(st.Backend, st.Model)]--
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
	var errs []error
	for _, p := range v.snap.Projects {
		if d.e.runs.State(p.Slug) != model.RunRunning {
			continue
		}
		// A project whose effort has used up its budget is stopped, not run (AT12).
		if stopped, err := d.e.enforceBudget(ctx, p.Slug); err != nil {
			errs = append(errs, fmt.Errorf("project %s: budget: %w", p.Slug, err))
		} else if stopped {
			continue
		}
		running = append(running, p)
	}
	sort.Slice(running, func(i, j int) bool { return running[i].Slug < running[j].Slug })
	start := sort.Search(len(running), func(i int) bool { return running[i].Slug > d.cursor })
	order := append(slices.Clone(running[start:]), running[:start]...)
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
	// From 90% of a budget no new task attempt starts; the ones in flight go on (AT12).
	if b, err := e.budgetGate(ctx, ef); err != nil {
		return false, err
	} else if b.NoNewTasks() {
		return false, nil
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
		avoid, err := e.repeatFailers(ctx, t, eff.StuckThreshold)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		maxTier, err := e.escalationTier(ctx, v, t)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		pk, reason := pickWorker(v, p, sticky, avoid, maxTier)
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
			if _, err := s.UpdateEffortStatusIf(ctx, SystemActor, ef.ID, model.EffortQueueAppr, st); err != nil {
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
	if _, ok := v.modelFree[modelKey(pk.Backend, pk.Model)]; ok {
		v.modelFree[modelKey(pk.Backend, pk.Model)]--
	}
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
// is submitted. avoid holds the models that already failed this task the same
// way repeatedly (H7): they are passed over for another model, unless no other
// model could ever serve, in which case the repeat is allowed so the task
// reaches stuck instead of waiting for ever.
func pickWorker(v *view, p store.Project, sticky string, avoid map[string]int, maxTier int) (Pick, string) {
	var reasons []string
	cands := v.snap.CandidatesFor("worker", p.Slug)
	tierOK := func(c store.Candidate) bool {
		return maxTier == 0 || effectiveTier(v.snap, c.Backend, c.Model) <= maxTier
	}
	for _, c := range cands {
		if why := skipReason(v, p, c, true); why != "" {
			reasons = append(reasons, why)
			continue
		}
		if n := avoid[c.Model]; n > 0 {
			reasons = append(reasons, fmt.Sprintf("model %s failed the same way %d times on this task", c.Model, n))
			continue
		}
		if !tierOK(c) {
			reasons = append(reasons, fmt.Sprintf("model %s (tier %d) is not stronger than the tier-%d model that failed this task twice", c.Model, effectiveTier(v.snap, c.Backend, c.Model), maxTier+1))
			continue
		}
		if _, why := reviewerFor(v, p, sticky, c.Model, false); why != "" {
			return Pick{}, why
		}
		return Pick{Backend: c.Backend, Model: c.Model}, ""
	}
	if len(avoid) > 0 || maxTier > 0 {
		ever := false
		for _, c := range cands {
			if avoid[c.Model] == 0 && tierOK(c) && skipReason(v, p, c, false) == "" {
				ever = true
			}
		}
		if !ever { // nothing better could ever serve: relax the escalation first, then the avoidance
			if maxTier > 0 {
				return pickWorker(v, p, sticky, avoid, 0)
			}
			return pickWorker(v, p, sticky, nil, 0)
		}
	}
	return Pick{}, noEligible("worker", reasons)
}

// escalateAfter is how many failed attempts in a row a task may have before
// its next attempt must go to a stronger model (H11).
const escalateAfter = 2

// escalationTier is the weakest tier the task's next worker may have: after
// escalateAfter failed attempts in a row it is one better than the tier of the
// worker that failed last (tier 1 stays tier 1), otherwise 0, meaning no limit.
func (e *Engine) escalationTier(ctx context.Context, v *view, t model.Task) (int, error) {
	atts, err := e.deps.Store.ListAttempts(ctx, t.ID)
	if err != nil {
		return 0, err
	}
	run := 0
	for i := len(atts) - 1; i >= 0 && atts[i].Outcome == model.OutcomeFailed; i-- {
		run++
	}
	if run < escalateAfter {
		return 0, nil
	}
	return max(1, effectiveTier(v.snap, "", atts[len(atts)-1].WorkerModel)-1), nil
}

// repeatFailers names the worker model, if any, that has failed the task's
// latest attempts the same way: the trailing run of failed attempts with one
// fingerprint and one worker. From stuckThreshold-1 of them (at least 1) the
// model is passed over for the next attempt, so a different model gets a turn
// before the task is declared stuck. The value is the length of the run.
func (e *Engine) repeatFailers(ctx context.Context, t model.Task, stuckThreshold int) (map[string]int, error) {
	atts, err := e.deps.Store.ListAttempts(ctx, t.ID)
	if err != nil {
		return nil, err
	}
	run := 0
	for i := len(atts) - 1; i >= 0; i-- {
		a, last := atts[i], atts[len(atts)-1]
		if a.Outcome != model.OutcomeFailed || a.Fingerprint == "" || a.Fingerprint != last.Fingerprint || a.WorkerModel != last.WorkerModel {
			break
		}
		run++
	}
	if run == 0 || run < max(1, stuckThreshold-1) {
		return nil, nil
	}
	return map[string]int{atts[len(atts)-1].WorkerModel: run}, nil
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
//
// The check is only as good as the reviewer is independent of the worker (H2):
// a reviewer from another model family is preferred, a different model of the
// worker's family is the fallback, and the worker's own model is never used.
// When a different-family reviewer exists but has no free slot, the stage
// waits for it rather than settling for the weaker check.
func reviewerFor(v *view, p store.Project, sticky, workerModel string, needSlot bool) (Pick, string) {
	cands := v.snap.CandidatesFor("reviewer", p.Slug)
	level := func(c store.Candidate) int {
		if model.ModelFamily(c.Model) != model.ModelFamily(workerModel) {
			return 0
		}
		return 1
	}
	best := 2
	for _, c := range cands {
		if skipReviewer(v, p, c, workerModel, false) == "" && level(c) < best {
			best = level(c)
		}
	}
	if sticky != "" {
		for _, c := range cands {
			if c.Model == sticky && level(c) == best && skipReviewer(v, p, c, workerModel, needSlot) == "" {
				return Pick{Backend: c.Backend, Model: c.Model}, ""
			}
		}
	}
	var reasons []string
	for _, c := range cands {
		if best < 2 && level(c) != best && c.Model != workerModel {
			continue
		}
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
	if wt := effectiveTier(v.snap, "", workerModel); effectiveTier(v.snap, c.Backend, c.Model) > wt {
		return fmt.Sprintf("reviewer model %s (tier %d) is weaker than the worker %s (tier %d)", c.Model, effectiveTier(v.snap, c.Backend, c.Model), workerModel, wt)
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
// changed since it was listed. needSlot false asks only whether the candidate
// could ever serve, so a transient lack (no free slot, in its unreachable
// cooldown) does not count against it.
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
	case v.avoid[c.Model]:
		return fmt.Sprintf("model %s's last reply could not be read", c.Model)
	case v.strikes[store.StrikeKey{Backend: c.Backend, Model: c.Model, Role: c.Role}] >= store.StrikeLimit:
		return fmt.Sprintf("model %s is benched for %s: %d strikes (clear them on the Stats tab)", c.Model, c.Role, store.StrikeLimit)
	case lacksTools(v, c):
		return fmt.Sprintf("model %s does not support tool calls, which every role needs", c.Model)
	case lightForRole(v, c):
		return fmt.Sprintf("model %s is rated light (tier 3), too weak for role %s", c.Model, c.Role)
	case needSlot && v.down[c.Backend]:
		return "backend " + c.Backend + " is down"
	case needSlot && v.free[c.Backend] <= 0:
		return "no free slot on " + c.Backend
	case needSlot && modelFull(v, c):
		return "model " + c.Model + " is at its concurrency limit on " + c.Backend
	}
	return ""
}

// lacksTools says the host reported that the candidate's model does not support
// tool calls (H10). A model whose support is not known is not blocked.
func lacksTools(v *view, c store.Candidate) bool {
	for _, b := range v.snap.Backends {
		if b.Name != c.Backend {
			continue
		}
		for _, m := range b.Models {
			if m.Model == c.Model {
				return m.Tools == "no"
			}
		}
	}
	return false
}

// Model tiers (H3): 1 strong, 2 capable, 3 light; a model the owner has not
// rated counts as 2, so nothing changes until models are rated.
const defaultTier = 2

// planningRole is a role whose output every later stage builds on.
func planningRole(role string) bool {
	switch role {
	case "planner", "architect", "critic", "compiler":
		return true
	}
	return false
}

// effectiveTier is the tier of a model: of that backend's entry when backend
// is given, else of the first backend that lists it.
func effectiveTier(snap store.Snapshot, backend, mdl string) int {
	for _, b := range snap.Backends {
		if backend != "" && b.Name != backend {
			continue
		}
		for _, m := range b.Models {
			if m.Model == mdl && m.Tier != 0 {
				return m.Tier
			}
			if m.Model == mdl {
				return defaultTier
			}
		}
	}
	return defaultTier
}

// lightForRole reports whether c is rated light and its role is a planning one.
func lightForRole(v *view, c store.Candidate) bool {
	return planningRole(c.Role) && effectiveTier(v.snap, c.Backend, c.Model) == 3
}

// modelFull reports whether c's model has a concurrency limit and it is used up.
func modelFull(v *view, c store.Candidate) bool {
	free, limited := v.modelFree[modelKey(c.Backend, c.Model)]
	return limited && free <= 0
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
