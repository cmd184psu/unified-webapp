package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"cmd184psu/unified-webapp/internal/agentflow/model"
	"cmd184psu/unified-webapp/internal/agentflow/store"
	"cmd184psu/unified-webapp/internal/taskmaster/golane"
)

// OutcomeInfraRerouted closes a queued stage whose backend was retired (BK4).
// It counts nothing; the stage is submitted again on another backend.
const OutcomeInfraRerouted = "infra:rerouted"

// healthState remembers which backends are down and until when (MB7). It is
// in memory: a restart forgets it, and a backend that is still down is marked
// again by its next unreachable stage.
type healthState struct {
	mu    sync.Mutex
	until map[string]time.Time
	// suspended are the backends that stay out of use until they answer again
	// (H8), with when they were suspended. They have no end time.
	suspended map[string]time.Time
}

func (h *healthState) suspend(backend string, since time.Time) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.suspended == nil {
		h.suspended = map[string]time.Time{}
	}
	if _, ok := h.suspended[backend]; !ok {
		h.suspended[backend] = since
	}
}

func (h *healthState) suspendedSince() map[string]time.Time {
	h.mu.Lock()
	defer h.mu.Unlock()
	return maps.Clone(h.suspended)
}

func (h *healthState) mark(backend string, until time.Time) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.until == nil {
		h.until = map[string]time.Time{}
	}
	h.until[backend] = until
}

func (h *healthState) clear(backend string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.until, backend)
	delete(h.suspended, backend)
}

// active returns the backends whose cooldown has not ended at now.
func (h *healthState) active(now time.Time) map[string]time.Time {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := map[string]time.Time{}
	for b, since := range h.suspended {
		out[b] = since
	}
	for b, u := range h.until {
		if now.Before(u) {
			out[b] = u
		} else {
			delete(h.until, b)
		}
	}
	return out
}

// DownBackends lists the backends currently in cooldown, with when each is
// tried again.
func (e *Engine) DownBackends() map[string]time.Time {
	return e.health.active(e.deps.Clock())
}

// SuspendedBackends lists the backends suspended because they stopped
// answering (H8), with when each was suspended. They come back by themselves
// when a check of their address succeeds.
func (e *Engine) SuspendedBackends() map[string]time.Time {
	return e.health.suspendedSince()
}

// markUnreachable takes the backend of a stage that just ended
// infra:unreachable out of use (MB7, H8). A backend with an address is
// suspended until probeSuspended finds it answering again, so no more stages
// are wasted on it; one without (opencode's built-in provider) cannot be
// checked and sits in the timed cooldown.
func (e *Engine) markUnreachable(ctx context.Context, stage int64) error {
	st, err := e.deps.Store.GetStage(ctx, stage)
	if err != nil {
		return err
	}
	if st.Backend == "" || st.Backend == verifyBackend || st.Backend == mergeBackend {
		return nil
	}
	b, err := e.deps.Store.GetBackend(ctx, st.Backend)
	if err != nil {
		return err
	}
	if b.Host != "" {
		e.health.suspend(st.Backend, e.deps.Clock())
		return nil
	}
	set, err := e.deps.Store.GetSettings(ctx)
	if err != nil {
		return err
	}
	e.health.mark(st.Backend, e.deps.Clock().Add(set.BackendCooldown))
	return nil
}

// reachable says whether anything answers at host: any HTTP reply counts, even
// an error status, because it only matters that the machine and the server are
// up. Deps.Reachable replaces it in tests.
func (e *Engine) reachable(ctx context.Context, host string) bool {
	if e.deps.Reachable != nil {
		return e.deps.Reachable(ctx, host)
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, host, nil)
	if err != nil {
		return false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return true
}

// probeSuspended checks each suspended backend and brings back the ones that
// answer, then wakes the dispatcher and the stages that were waiting for it.
// The engine's periodic tick calls it.
func (e *Engine) probeSuspended(ctx context.Context) {
	back := false
	for name := range e.health.suspendedSince() {
		b, err := e.deps.Store.GetBackend(ctx, name)
		if err != nil || b.Retired || e.reachable(ctx, b.Host) {
			e.health.clear(name)
			back = true
		}
	}
	if !back {
		return
	}
	if e.deps.Kick != nil {
		e.deps.Kick()
	}
	_ = e.Advance(ctx)
	_ = e.AdvancePlans(ctx)
}

// RetireOptions are the operator's choices when retiring a backend.
type RetireOptions struct {
	// CancelRunning cancels the backend's running jobs; their stages end
	// infra:cancel and are retried elsewhere. Without it they finish.
	CancelRunning bool
}

// RetireReport says what a retire did.
type RetireReport struct {
	// Rerouted are the queued stages closed infra:rerouted and resubmitted.
	Rerouted []int64
	// Canceled are the running lane jobs cancelled (CancelRunning).
	Canceled []string
	// Warnings name roles left without a backend and the running projects
	// that need them (SV3). The retire is done regardless.
	Warnings []string
}

// RetireBackend retires a backend (BK3): its lane is paused and hidden, its
// queued stages are taken off the lane and submitted again on another
// eligible backend, and its running jobs finish unless CancelRunning (BK4).
// Queued planning and compile stages are rerouted the same way.
func (e *Engine) RetireBackend(ctx context.Context, actor, name string, opt RetireOptions) (RetireReport, error) {
	var rep RetireReport
	s := e.deps.Store
	before, err := s.Snapshot(ctx)
	if err != nil {
		return rep, err
	}
	if err := s.RetireBackend(ctx, actor, name); err != nil {
		return rep, err
	}
	// Pausing first means nothing queued starts while it is being emptied.
	if err := e.SyncLanes(ctx); err != nil {
		return rep, err
	}
	after, err := s.Snapshot(ctx)
	if err != nil {
		return rep, err
	}
	rep.Warnings = e.lastBackendWarnings(before, after)
	l, ok := e.Lane(BackendLane(name))
	if !ok {
		return rep, nil
	}
	rep.Rerouted, rep.Canceled, err = e.drainLane(ctx, l, name, opt)
	if err != nil {
		return rep, err
	}
	if len(rep.Rerouted) > 0 {
		// Advance and AdvancePlans see the rerouted stage as an uncounted
		// infra stage and submit it again (handleInfra, effortInfra).
		return rep, errors.Join(e.Advance(ctx), e.AdvancePlans(ctx))
	}
	return rep, nil
}

// drainLane removes the lane's queued jobs, closing their stages as
// rerouted, and cancels running jobs when asked. Reconcile is held off while a
// job is gone and its stage is not yet closed, or it would call the stage lost.
func (e *Engine) drainLane(ctx context.Context, l golane.Lane, backend string, opt RetireOptions) (rerouted []int64, canceled []string, err error) {
	e.submitMu.RLock()
	defer e.submitMu.RUnlock()
	jobs, err := l.List()
	if err != nil {
		return nil, nil, err
	}
	var errs []error
	for _, j := range jobs {
		var p StagePayload
		if !strings.HasPrefix(j.Kind, "agentflow.") || json.Unmarshal(j.Payload, &p) != nil || p.Stage == 0 {
			continue
		}
		switch {
		case j.Status == jobQueued:
			st, err := e.deps.Store.GetStage(ctx, p.Stage)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			if st.State != model.StageQueued {
				continue
			}
			if err := l.Remove(j.ID); err != nil {
				continue // it started in the meantime: let it finish
			}
			err = e.deps.Store.FinishStage(ctx, st.ID, OutcomeInfraRerouted, "backend "+backend+" retired", nil)
			if err != nil && !errors.Is(err, store.ErrConflict) {
				errs = append(errs, err)
				continue
			}
			rerouted = append(rerouted, st.ID)
		case opt.CancelRunning && !jobTerminal(j.Status):
			if st, err := e.deps.Store.GetStage(ctx, p.Stage); err == nil {
				e.cancelSeat(ctx, st)
			}
			if _, err := l.Cancel(j.ID); err != nil {
				errs = append(errs, fmt.Errorf("cancel job %s: %w", j.ID, err))
				continue
			}
			canceled = append(canceled, j.ID)
		}
	}
	return rerouted, canceled, errors.Join(errs...)
}

// lastBackendWarnings reports, per role, the running projects that had a
// candidate on the retired backend and now have none (SV3).
func (e *Engine) lastBackendWarnings(before, after store.Snapshot) []string {
	roles := map[string]bool{}
	for _, c := range before.Candidates {
		roles[c.Role] = true
	}
	var out []string
	for _, role := range slices.Sorted(maps.Keys(roles)) {
		var affected []string
		for _, p := range after.Projects {
			if len(before.CandidatesFor(role, p.Slug)) > 0 && len(after.CandidatesFor(role, p.Slug)) == 0 &&
				e.runs.State(p.Slug) == model.RunRunning {
				affected = append(affected, p.Slug)
			}
		}
		if len(affected) > 0 {
			out = append(out, fmt.Sprintf("no backend is left for role %s; running projects affected: %s", role, strings.Join(affected, ", ")))
		}
	}
	return out
}
