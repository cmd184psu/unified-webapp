package engine

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"cmd184psu/unified-webapp/internal/agentflow/model"
	"cmd184psu/unified-webapp/internal/agentflow/runner"
)

// stallUnit is what one stall minute is; tests make it short.
var stallUnit = time.Minute

var errStalled = errors.New("stalled")

// runWatched runs one model call and ends it as a timeout when the model says
// nothing for stallMinutes (H30): a stage that is hung or has lost its
// provider is told apart from one that is merely slow by its silence, not by
// the role's long timeout, and the usual rules then move the work to another
// model. lastSeen is updated by the request's OnEvent. A model that streams
// output is never stalled, however long it takes.
func (e *Engine) runWatched(ctx context.Context, req runner.Request, stallMinutes int, lastSeen *atomic.Int64) (runner.Result, error) {
	after := time.Duration(stallMinutes) * stallUnit
	if after <= 0 {
		return e.deps.Runner.Run(ctx, req)
	}
	cctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	done := make(chan struct{})
	defer close(done)
	go func() {
		tick := time.NewTicker(max(after/10, time.Millisecond))
		defer tick.Stop()
		for {
			select {
			case <-done:
				return
			case <-cctx.Done():
				return
			case <-tick.C:
				if time.Duration(e.deps.Clock().UnixNano()-lastSeen.Load()) >= after {
					cancel(errStalled)
					return
				}
			}
		}
	}()
	res, err := e.deps.Runner.Run(cctx, req)
	if err != nil && errors.Is(context.Cause(cctx), errStalled) && ctx.Err() == nil {
		return res, &runner.Error{Class: runner.ErrTimeout, Msg: fmt.Sprintf("stalled: no output for %d minutes", stallMinutes), Cause: err}
	}
	return res, err
}

// IdleStall says an effort that should be moving has had nothing in flight
// and nothing happen for the stall time (H30), and why it is waiting. It
// returns the time of the last activity, or zero when the effort is not idle:
// it is finished, waiting for a person, in a backoff or resting under a duty
// cap, or has a stage in flight.
func (e *Engine) IdleStall(ctx context.Context, project string, ef model.Effort, set stallSettings) (time.Time, []string, error) {
	planning := ef.Status == model.EffortPlanning || ef.Status == model.EffortCompiling
	if !planning && ef.RunState != model.RunRunning {
		return time.Time{}, nil, nil
	}
	busy, err := e.deps.Store.EffortInFlight(ctx, ef.ID)
	if err != nil || busy {
		return time.Time{}, nil, err
	}
	e.mu.Lock()
	waiting := len(e.retryTimers) > 0
	e.mu.Unlock()
	if waiting {
		return time.Time{}, nil, nil
	}
	v, err := e.newView(ctx)
	if err != nil {
		return time.Time{}, nil, err
	}
	if len(v.resting) > 0 {
		return time.Time{}, nil, nil
	}
	var reasons []string
	if !planning {
		tasks, err := e.deps.Store.ListTasks(ctx, ef.ID)
		if err != nil {
			return time.Time{}, nil, err
		}
		status, err := e.deps.Store.TaskStatuses(ctx, ef.ID, set.limits)
		if err != nil {
			return time.Time{}, nil, err
		}
		pending := false
		for _, t := range tasks {
			st := status[t.Key]
			if st.Done() || st.Halted() {
				continue
			}
			pending = true
			if t.WaitingReason != "" {
				reasons = append(reasons, t.Key+": "+t.WaitingReason)
			}
		}
		if !pending {
			return time.Time{}, nil, nil
		}
	}
	last, err := e.deps.Store.EffortLastActivity(ctx, ef.ID)
	if err != nil || last == 0 {
		return time.Time{}, nil, err
	}
	at := time.UnixMilli(last)
	if e.deps.Clock().Sub(at) < time.Duration(set.minutes)*stallUnit {
		return time.Time{}, nil, nil
	}
	return at, reasons, nil
}

// stallSettings is what IdleStall needs of the effective settings.
type stallSettings struct {
	minutes int
	limits  model.Limits
}

func (e *Engine) stallSettingsFor(ctx context.Context, project string) (stallSettings, error) {
	set, err := e.deps.Store.EffectiveSettings(ctx, project)
	if err != nil {
		return stallSettings{}, err
	}
	return stallSettings{minutes: set.StallMinutes, limits: e.limitsFor(set)}, nil
}

// watchStalls runs after every periodic reconcile: an effort that has sat idle
// for the stall time is nudged (the planning and dispatch passes run again,
// which cures a missed wakeup) and the stall is recorded once, with what each
// task is waiting for, so it shows on the attention list.
func (e *Engine) watchStalls(ctx context.Context) {
	projects, err := e.deps.Store.ListProjects(ctx)
	if err != nil {
		return
	}
	for _, p := range projects {
		efforts, err := e.deps.Store.ListEfforts(ctx, p.Slug)
		if err != nil {
			continue
		}
		set, err := e.stallSettingsFor(ctx, p.Slug)
		if err != nil {
			continue
		}
		for _, ef := range efforts {
			at, reasons, err := e.IdleStall(ctx, p.Slug, ef, set)
			if err != nil || at.IsZero() {
				continue
			}
			evs, _ := e.effortEvents(ctx, ef.ID, eventStallDetected)
			if len(evs) == 0 || evs[len(evs)-1].TS.Before(at) {
				_ = e.noteEffort(ctx, SystemActor, eventStallDetected, ef.ID, map[string]any{"idle_since": at.UnixMilli(), "minutes": set.minutes, "waiting": reasons})
			}
			_ = e.AdvancePlans(ctx)
			_ = e.Advance(ctx)
			_ = e.DispatchOnce(ctx)
		}
	}
}

const eventStallDetected = "stall.detected"
