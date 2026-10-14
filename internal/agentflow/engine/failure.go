package engine

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"time"

	"cmd184psu/unified-webapp/internal/agentflow/gitsvc"
	"cmd184psu/unified-webapp/internal/agentflow/model"
	"cmd184psu/unified-webapp/internal/agentflow/store"
)

const (
	// DefaultMaxPolicyViolations is how many git-policy violations halt a
	// task as needs_attention (GIT5) unless Deps.MaxPolicyViolations says
	// otherwise. It is a constant, not a setting: the settings table has no
	// such key.
	DefaultMaxPolicyViolations = 3

	infraBackoffBase = 10 * time.Second
	infraBackoffCap  = 5 * time.Minute
)

// limitsFor is the effective limits the task status is derived with. The
// dispatcher, the effort completion check and the failure policy must all use
// it, or they would disagree on when a task is halted.
func (e *Engine) limitsFor(set store.Settings) model.Limits {
	maxViol := e.deps.MaxPolicyViolations
	if maxViol == 0 {
		maxViol = DefaultMaxPolicyViolations
	}
	return model.Limits{
		MaxAttempts: set.MaxAttempts, StuckThreshold: set.StuckThreshold,
		MaxInfraRetries: set.MaxInfraRetries, MaxPolicyViolations: maxViol,
	}
}

// infraBackoff is the wait before the retry that follows strikes earlier
// infrastructure failures: 10 s, doubling, capped at 5 min (AT7).
func infraBackoff(strikes int) time.Duration {
	d := infraBackoffBase
	for i := 0; i < strikes && d < infraBackoffCap; i++ {
		d *= 2
	}
	return min(d, infraBackoffCap)
}

// failAttempt closes the attempt a failed chain stage ended (AT2, AT4, AT5,
// AT6, AT10, GIT5). The next attempt is not started here: the task derives
// repair (or a halted status) and the dispatcher takes it from there, honoring
// the project's run state.
func (e *Engine) failAttempt(ctx context.Context, c *chain, f Failure) error {
	outcome, fp, result := model.OutcomeFailed, "", "failed ("+string(f.Kind)+")"
	switch f.Kind {
	case FailBlocked:
		outcome, result = model.OutcomeBlocked, "blocked"
	case FailViolation:
		outcome, result = model.OutcomePolicyViolation, "broke the git policy (reverted)"
	default:
		fp = fingerprintOf(f)
	}
	if err := e.appendProgress(ctx, c, result); err != nil {
		return err
	}
	if f.Kind == FailBlocked {
		if err := e.deps.Store.SetWaitingReason(ctx, c.task.ID, blockedReason(f.Stage)); err != nil {
			return err
		}
	}
	if err := e.deps.Store.FinishAttempt(ctx, c.att.ID, outcome, fp, c.reviewerModel()); err != nil {
		if errors.Is(err, store.ErrConflict) {
			return nil
		}
		return err
	}
	return e.explainHalt(ctx, c)
}

// explainHalt puts the reason on a task that this attempt just halted, so the
// operator sees why without reading attempts (needs_input already carries the
// worker's own words).
func (e *Engine) explainHalt(ctx context.Context, c *chain) error {
	set, err := e.deps.Store.EffectiveSettings(ctx, c.proj.Slug)
	if err != nil {
		return err
	}
	lim := e.limitsFor(set)
	status, err := e.deps.Store.TaskStatuses(ctx, c.eff.ID, lim)
	if err != nil {
		return err
	}
	var reason string
	switch status[c.task.Key] {
	case model.StatusStuck:
		reason = fmt.Sprintf("stuck: the same failure %d times in a row", lim.StuckThreshold)
	case model.StatusExhausted:
		reason = fmt.Sprintf("exhausted: %d attempts used", lim.MaxAttempts)
	case model.StatusNeedsAttention:
		reason = fmt.Sprintf("needs attention: %d git policy violations", lim.MaxPolicyViolations)
	default:
		return nil
	}
	return e.deps.Store.SetWaitingReason(ctx, c.task.ID, reason)
}

// handleInfra deals with an attempt whose newest stage ended in an
// infrastructure failure (AT7). The attempt stays open and the stage is
// repeated, on another eligible backend when there is one, after an
// exponential backoff; each failure is one strike against max_infra_retries
// and the strike that reaches the limit halts the task as needs_attention.
// A stop, brake or restart (infra:cancel, infra:lost) is not the backend's
// fault: it counts nothing and is left to reconcile, which abandons the
// attempt. A stage rerouted off a retired backend (infra:rerouted) counts
// nothing either and is submitted again at once. It reports whether it changed anything.
func (e *Engine) handleInfra(ctx context.Context, c *chain, last model.Stage) (bool, error) {
	if last.Outcome == OutcomeInfraCancel || last.Outcome == OutcomeInfraLost {
		return false, nil
	}
	if last.Outcome == OutcomeInfraRerouted {
		// Its backend was retired: not a failure, so no strike and no backoff.
		return true, e.resubmit(ctx, c, last)
	}
	s := e.deps.Store
	set, err := s.EffectiveSettings(ctx, c.proj.Slug)
	if err != nil {
		return false, err
	}
	strikes, err := e.infraStrikes(ctx, c)
	if err != nil {
		return false, err
	}
	limit := set.MaxInfraRetries
	if limit > 0 && strikes >= limit {
		return true, e.haltInfra(ctx, c, last, strikes, limit)
	}
	if limit > 0 && strikes+1 >= limit {
		if err := s.RecordInfraRetry(ctx, c.task.ID, last.ID, last.Outcome); err != nil {
			return false, err
		}
		return true, e.haltInfra(ctx, c, last, strikes+1, limit)
	}
	due := time.UnixMilli(last.Finished).Add(infraBackoff(strikes))
	if wait := due.Sub(e.deps.Clock()); wait > 0 {
		e.advanceAfter(last.ID, wait)
		return false, nil
	}
	if err := e.resubmit(ctx, c, last); err != nil {
		return false, err
	}
	return true, s.RecordInfraRetry(ctx, c.task.ID, last.ID, last.Outcome)
}

// infraStrikes counts the task's infrastructure retries since its last reset.
func (e *Engine) infraStrikes(ctx context.Context, c *chain) (int, error) {
	ins, err := e.deps.Store.TaskInputs(ctx, c.eff.ID)
	if err != nil {
		return 0, err
	}
	for _, in := range ins {
		if in.Key != c.task.Key {
			continue
		}
		var cut int64
		for _, r := range in.Resets {
			cut = max(cut, r.AfterInfraID)
		}
		n := 0
		for _, id := range in.InfraIDs {
			if id > cut {
				n++
			}
		}
		return n, nil
	}
	return 0, nil
}

// haltInfra gives up on the attempt: it is abandoned (it never counts) and the
// task derives needs_attention from the retry count.
func (e *Engine) haltInfra(ctx context.Context, c *chain, last model.Stage, strikes, limit int) error {
	reason := fmt.Sprintf("needs attention: %d infrastructure failures (limit %d); last: %s on %s: %s",
		strikes, limit, last.Outcome, last.Backend, clip(last.RawOutput, maxEntryDetail))
	if err := e.deps.Store.SetWaitingReason(ctx, c.task.ID, reason); err != nil {
		return err
	}
	return e.finishAttempt(ctx, c, model.OutcomeAbandoned)
}

// resubmit repeats a stage that ended in infra failure as a new stage of the
// same attempt, preferring a backend other than the one that failed.
func (e *Engine) resubmit(ctx context.Context, c *chain, last model.Stage) error {
	switch last.Role {
	case "verify":
		return e.submit(ctx, c, VerifyLane, "verify", verifyBackend, "", last.QueueHash)
	case "merge":
		return e.submit(ctx, c, MergeLane, "merge", mergeBackend, "", last.QueueHash)
	}
	pick := func(avoid string) (Pick, string, error) {
		v, err := e.newView(ctx)
		if err != nil {
			return Pick{}, "", err
		}
		if avoid != "" {
			v.down[avoid] = true // like a backend in cooldown: try the others first
		}
		sticky, err := e.stickyReviewer(ctx, c.task)
		if err != nil {
			return Pick{}, "", err
		}
		if last.Role == "reviewer" {
			pk, why := reviewerFor(v, c.proj, sticky, c.workerModel(), true)
			return pk, why, nil
		}
		pk, why := pickWorker(v, c.proj, sticky)
		return pk, why, nil
	}
	pk, why, err := pick(last.Backend)
	if err == nil && why != "" {
		pk, why, err = pick("") // nowhere else to go: try the same backend again
	}
	if err != nil {
		return err
	}
	if why != "" {
		return e.deps.Store.SetWaitingReason(ctx, c.task.ID, why)
	}
	if err := e.submit(ctx, c, BackendLane(pk.Backend), last.Role, pk.Backend, pk.Model, last.QueueHash); err != nil {
		return err
	}
	if c.task.WaitingReason != "" {
		return e.deps.Store.SetWaitingReason(ctx, c.task.ID, "")
	}
	return nil
}

// advanceAfter schedules one Advance for when a backoff ends. The reconcile
// tick would pick the retry up anyway; this only keeps a 10 s backoff from
// costing a whole tick. Timers are stopped by Close.
func (e *Engine) advanceAfter(stage int64, d time.Duration) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.retryTimers == nil || e.retryTimers[stage] != nil {
		return
	}
	e.retryTimers[stage] = time.AfterFunc(d, func() {
		e.mu.Lock()
		delete(e.retryTimers, stage)
		e.mu.Unlock()
		_ = e.Advance(context.Background())
	})
}

// auditViolations records a policy violation in the audit log (GIT5).
func (e *Engine) auditViolations(ctx context.Context, st model.Stage, viols []gitsvc.Violation) error {
	items := make([]map[string]string, len(viols))
	for i, v := range viols {
		items[i] = map[string]string{"kind": v.Kind, "detail": v.Detail}
	}
	return e.deps.Store.WithTx(ctx, func(tx *sql.Tx) error {
		_, err := store.AppendEvent(tx, SystemActor, "policy.violation", "stage", strconv.FormatInt(st.ID, 10),
			map[string]any{"attempt": st.Attempt, "role": st.Role, "backend": st.Backend, "violations": items})
		return err
	})
}
