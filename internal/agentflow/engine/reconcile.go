package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"cmd184psu/unified-webapp/internal/agentflow/model"
	"cmd184psu/unified-webapp/internal/agentflow/store"
	"cmd184psu/unified-webapp/internal/taskmaster/golane"
)

const defaultReconcileEvery = 60 * time.Second

// Lane job statuses as taskmaster reports them.
const (
	jobQueued   = "queued"
	jobSuccess  = "success"
	jobFailed   = "failed"
	jobCanceled = "canceled"
)

func jobTerminal(status string) bool {
	return status == jobSuccess || status == jobFailed || status == jobCanceled
}

// ReconcileReport says what one reconcile pass changed.
type ReconcileReport struct {
	// Lost are the stages closed as infra:lost.
	Lost []int64
	// Removed are the stale queued lane jobs deleted (start only).
	Removed []string
	// Abandoned are the attempts closed as non-counting because their last
	// stage was cancelled or lost.
	Abandoned []int64
}

type jobRef struct {
	lane golane.Lane
	job  golane.Job
}

// indexJobs lists every agentflow job on every agentflow lane, by stage id.
func (e *Engine) indexJobs() (map[int64][]jobRef, error) {
	e.mu.Lock()
	lanes := make([]golane.Lane, 0, len(e.handles))
	for _, l := range e.handles {
		lanes = append(lanes, l)
	}
	e.mu.Unlock()
	idx := map[int64][]jobRef{}
	for _, l := range lanes {
		jobs, err := l.List()
		if err != nil {
			return nil, fmt.Errorf("list lane %s: %w", l.Name(), err)
		}
		for _, j := range jobs {
			if !strings.HasPrefix(j.Kind, "agentflow.") {
				continue
			}
			var p struct {
				Stage int64 `json:"stage"`
			}
			if json.Unmarshal(j.Payload, &p) != nil || p.Stage == 0 {
				continue
			}
			idx[p.Stage] = append(idx[p.Stage], jobRef{lane: l, job: j})
		}
	}
	return idx, nil
}

// kindForRole is the lane kind that runs a stage role.
func kindForRole(role string) string {
	switch role {
	case "worker":
		return KindWork
	case "reviewer":
		return KindReview
	case "verify":
		return KindVerify
	case "merge":
		return KindMerge
	}
	return KindPlan
}

// Submit creates the stage row and submits its lane job as one step with
// respect to reconciliation, which would otherwise see the new row without a
// job and close it as lost. p.Stage is filled in. If the job cannot be
// submitted the stage is closed as infra:crash.
func (e *Engine) Submit(ctx context.Context, laneName string, sp store.StageSpec, p StagePayload) (model.Stage, golane.Job, error) {
	e.submitMu.RLock()
	defer e.submitMu.RUnlock()
	l, ok := e.Lane(laneName)
	if !ok {
		return model.Stage{}, golane.Job{}, fmt.Errorf("no such lane %q", laneName)
	}
	st, err := e.deps.Store.CreateStage(ctx, sp)
	if err != nil {
		return st, golane.Job{}, err
	}
	p.Stage = st.ID
	job, err := l.Submit(kindForRole(sp.Role), fmt.Sprintf("%s stage %d", sp.Role, st.ID), p)
	if err != nil {
		_ = e.deps.Store.FinishStage(context.WithoutCancel(ctx), st.ID, OutcomeInfraCrash, "submit: "+err.Error(), nil)
		return st, job, err
	}
	return st, job, nil
}

// Reconcile compares every queued or running stage with the lane jobs and
// closes the ones whose job vanished or ended without an agentflow outcome as
// infra:lost (DBS5). It is safe to call at any time.
func (e *Engine) Reconcile(ctx context.Context) (ReconcileReport, error) {
	return e.reconcile(ctx, false)
}

// reconcile does one pass. At start (fromPrevious) every queued agentflow job
// belongs to a previous process and is removed first; the stage it carried is
// then closed like any other whose job is missing.
func (e *Engine) reconcile(ctx context.Context, fromPrevious bool) (ReconcileReport, error) {
	e.submitMu.Lock()
	defer e.submitMu.Unlock()
	var rep ReconcileReport
	idx, err := e.indexJobs()
	if err != nil {
		return rep, err
	}
	var errs []error
	if fromPrevious {
		for sid, refs := range idx {
			kept := refs[:0]
			for _, r := range refs {
				if r.job.Status != jobQueued {
					kept = append(kept, r)
					continue
				}
				if err := r.lane.Remove(r.job.ID); err != nil {
					errs = append(errs, fmt.Errorf("remove stale job %s: %w", r.job.ID, err))
					kept = append(kept, r)
					continue
				}
				rep.Removed = append(rep.Removed, r.job.ID)
			}
			idx[sid] = kept
		}
	}
	open, err := e.deps.Store.OpenStages(ctx)
	if err != nil {
		return rep, err
	}
	for _, st := range open {
		reason := "lane job missing"
		alive := false
		for _, r := range idx[st.ID] {
			if !jobTerminal(r.job.Status) {
				alive = true
				break
			}
			reason = fmt.Sprintf("job %s ended %s without an outcome", r.job.ID, r.job.Status)
		}
		if alive {
			continue
		}
		// The job list was read before this re-read, so a job that finished
		// since has its outcome written (write-before-return) and shows done.
		cur, err := e.deps.Store.GetStage(ctx, st.ID)
		if err != nil || cur.State == model.StageDone {
			continue
		}
		if err := e.deps.Store.FinishStage(ctx, st.ID, OutcomeInfraLost, reason, nil); err != nil {
			if !errors.Is(err, store.ErrConflict) {
				errs = append(errs, err)
			}
			continue
		}
		rep.Lost = append(rep.Lost, st.ID)
	}
	ab, err := e.abandonInterrupted(ctx)
	rep.Abandoned = ab
	if err != nil {
		errs = append(errs, err)
	}
	return rep, errors.Join(errs...)
}

// abandonInterrupted closes, as non-counting, every open attempt whose newest
// stage ended infra:cancel or infra:lost and has nothing left in flight: the
// operator or a restart interrupted it, and the task becomes eligible again.
func (e *Engine) abandonInterrupted(ctx context.Context) ([]int64, error) {
	s := e.deps.Store
	projects, err := s.ListProjects(ctx)
	if err != nil {
		return nil, err
	}
	var out []int64
	for _, p := range projects {
		ef, err := s.ActiveEffort(ctx, p.Slug)
		if errors.Is(err, store.ErrNotFound) {
			continue
		}
		if err != nil {
			return out, err
		}
		tasks, err := s.ListTasks(ctx, ef.ID)
		if errors.Is(err, store.ErrNotFound) {
			continue
		}
		if err != nil {
			return out, err
		}
		for _, t := range tasks {
			atts, err := s.ListAttempts(ctx, t.ID)
			if err != nil {
				return out, err
			}
			for _, a := range atts {
				if a.Outcome != "" {
					continue
				}
				stages, err := s.ListAttemptStages(ctx, a.ID)
				if err != nil {
					return out, err
				}
				if len(stages) == 0 || !interrupted(stages) {
					continue
				}
				if err := s.FinishAttempt(ctx, a.ID, model.OutcomeAbandoned, "", ""); err != nil {
					if errors.Is(err, store.ErrConflict) {
						continue
					}
					return out, err
				}
				out = append(out, a.ID)
			}
		}
	}
	return out, nil
}

func interrupted(stages []model.Stage) bool {
	for _, st := range stages {
		if st.State != model.StageDone {
			return false
		}
	}
	last := stages[len(stages)-1].Outcome
	return last == OutcomeInfraCancel || last == OutcomeInfraLost
}

type tickerState struct {
	stop   chan struct{}
	done   chan struct{}
	cancel context.CancelFunc
}

// Start reconciles, and only then lifts the lane hold and starts the periodic
// reconcile (RC4, DBS5). Call it once after New.
func (e *Engine) Start(ctx context.Context) error {
	e.mu.Lock()
	running := e.ticker != nil
	e.mu.Unlock()
	if running {
		return errors.New("engine: already started")
	}
	if _, err := e.reconcile(ctx, true); err != nil {
		return fmt.Errorf("reconcile at start: %w", err)
	}
	if err := e.Resume(ctx); err != nil {
		return err
	}
	// Planning and compile stages closed as lost by the reconcile above are
	// submitted again now, not a tick later.
	if err := e.AdvancePlans(ctx); err != nil {
		return fmt.Errorf("advance plans at start: %w", err)
	}
	bg, cancel := context.WithCancel(context.Background())
	ts := &tickerState{stop: make(chan struct{}), done: make(chan struct{}), cancel: cancel}
	e.mu.Lock()
	e.ticker = ts
	e.mu.Unlock()
	ticks := e.deps.Tick
	var stopTicker func()
	if ticks == nil {
		every := e.deps.ReconcileEvery
		if every <= 0 {
			every = defaultReconcileEvery
		}
		t := time.NewTicker(every)
		ticks, stopTicker = t.C, t.Stop
	}
	go func() {
		defer close(ts.done)
		if stopTicker != nil {
			defer stopTicker()
		}
		for {
			select {
			case <-ts.stop:
				return
			case <-ticks:
				_, _ = e.Reconcile(bg)
				for _, h := range e.tickHooks {
					h(bg)
				}
			}
		}
	}()
	e.disp.start()
	return nil
}

// Close stops the periodic reconcile. It does not close the taskmaster engine
// or the store, which the composition root owns.
func (e *Engine) Close() {
	e.mu.Lock()
	ts := e.ticker
	e.ticker = nil
	for id, t := range e.retryTimers {
		t.Stop()
		delete(e.retryTimers, id)
	}
	e.mu.Unlock()
	if ts == nil {
		return
	}
	e.disp.halt()
	close(ts.stop)
	ts.cancel()
	<-ts.done
}
