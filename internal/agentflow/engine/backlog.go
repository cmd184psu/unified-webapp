package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"cmd184psu/unified-webapp/internal/agentflow/model"
	"cmd184psu/unified-webapp/internal/agentflow/store"
)

const eventBacklogStart = "backlog.start"

// StartCondition is a git condition that holds a ready effort back. The only
// form today is "branch <name> contains <commit-or-branch>"; the branch may be
// local or origin's copy, and contains means an ancestor of the branch tip.
type StartCondition struct {
	Branch   string
	Contains string
}

// ParseStartCondition reads the optional condition shown on a ready effort.
func ParseStartCondition(s string) (StartCondition, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return StartCondition{}, nil
	}
	f := strings.Fields(s)
	if len(f) != 4 || f[0] != "branch" || f[2] != "contains" || f[1] == "" || f[3] == "" {
		return StartCondition{}, fmt.Errorf("start condition %q must read \"branch <name> contains <commit-or-branch>\"", s)
	}
	return StartCondition{Branch: f[1], Contains: f[3]}, nil
}

// StartConditionMet reports whether an effort's condition holds. An empty
// condition always holds; a malformed one does not, so auto-start skips it and
// the owner sees why in the backlog.
func (e *Engine) StartConditionMet(ctx context.Context, ef model.Effort) (bool, error) {
	c, err := ParseStartCondition(ef.StartCondition)
	if err != nil {
		return false, err
	}
	if c.Branch == "" {
		return true, nil
	}
	if e.deps.Git == nil {
		return false, nil
	}
	p, err := e.deps.Store.GetProject(ctx, ef.Project)
	if err != nil {
		return false, err
	}
	return e.deps.Git.BranchContains(ctx, p.RepoPath, c.Branch, c.Contains)
}

// FreshBase fetches the project's remote and returns its configured base
// branch tip (or HEAD when no base branch is set). A fetch failure is not fatal:
// the best known local tip is still better than a stale stored one.
func (e *Engine) FreshBase(ctx context.Context, p store.Project) (string, error) {
	if e.deps.Git == nil {
		return "", nil
	}
	_ = e.deps.Git.Fetch(ctx, p.RepoPath)
	return e.deps.Git.BranchTip(ctx, p.RepoPath, p.BaseBranch)
}

// SetFreshBase records the project base tip on an effort. It is called when an
// effort is created and again before a parked effort starts (AF-15 item 8).
func (e *Engine) SetFreshBase(ctx context.Context, effort int64) (string, error) {
	ef, err := e.deps.Store.GetEffort(ctx, effort)
	if err != nil {
		return "", err
	}
	p, err := e.deps.Store.GetProject(ctx, ef.Project)
	if err != nil {
		return "", err
	}
	sha, err := e.FreshBase(ctx, p)
	if err != nil || sha == "" {
		return ef.BaseCommit, err
	}
	if sha != ef.BaseCommit {
		if _, err := e.deps.Store.UpdateEffort(ctx, SystemActor, effort, store.EffortPatch{BaseCommit: &sha}); err != nil {
			return "", err
		}
	}
	return sha, nil
}

// Park stops a prepared effort from running and puts it in the backlog. Its
// FRD, plan and queue must already be approved; priority and an optional git
// start condition are saved with the new ready status.
func (e *Engine) Park(ctx context.Context, actor string, effort int64, priority *int64, condition *string) (model.Effort, error) {
	s := e.deps.Store
	ef, err := s.GetEffort(ctx, effort)
	if err != nil {
		return ef, err
	}
	if ef.Status != model.EffortQueueAppr {
		return ef, fmt.Errorf("effort %d is %s, not queue_approved: %w", effort, ef.Status, store.ErrConflict)
	}
	if _, err := s.ApprovedRevision(ctx, effort, model.DocFRD); err != nil {
		return ef, fmt.Errorf("effort %d has no approved FRD: %w", effort, store.ErrConflict)
	}
	if _, err := s.ApprovedRevision(ctx, effort, model.DocPlan); err != nil {
		return ef, fmt.Errorf("effort %d has no approved plan: %w", effort, store.ErrConflict)
	}
	q, err := s.GetQueue(ctx, effort)
	if err != nil {
		return ef, err
	}
	if q.Status != model.QueueApproved {
		return ef, fmt.Errorf("queue of effort %d is %s, not approved: %w", effort, q.Status, store.ErrConflict)
	}
	if condition != nil {
		if _, err := ParseStartCondition(*condition); err != nil {
			return ef, store.Invalid("start_condition", err.Error())
		}
	}
	ready := model.EffortReady
	return s.UpdateEffort(ctx, actor, effort, store.EffortPatch{Status: &ready, Priority: priority, StartCondition: condition})
}

// Unpark returns a ready effort to queue_approved, where a person can start
// it or park it again. It does not start anything.
func (e *Engine) Unpark(ctx context.Context, actor string, effort int64) (model.Effort, error) {
	ef, err := e.deps.Store.GetEffort(ctx, effort)
	if err != nil {
		return ef, err
	}
	if ef.Status != model.EffortReady {
		return ef, fmt.Errorf("effort %d is %s, not ready: %w", effort, ef.Status, store.ErrConflict)
	}
	approved := model.EffortQueueAppr
	return e.deps.Store.UpdateEffort(ctx, actor, effort, store.EffortPatch{Status: &approved})
}

// AutoStartBacklog is the engine tick's AF-15 step: while fewer efforts run
// than min_running_efforts, start the highest-priority ready effort whose
// project has room, whose budget is set and not exhausted, and whose git
// condition holds. It starts at most enough efforts to reach the minimum.
func (e *Engine) AutoStartBacklog(ctx context.Context) error {
	set, err := e.deps.Store.GetSettings(ctx)
	if err != nil || set.MinRunningEfforts <= 0 {
		return err
	}
	running, err := e.deps.Store.CountEffortsByStatus(ctx, "", model.EffortRunning)
	if err != nil {
		return err
	}
	if running >= set.MinRunningEfforts {
		return nil
	}
	ready, err := e.deps.Store.ListReadyEfforts(ctx)
	if err != nil {
		return err
	}
	var errs []error
	for _, ef := range ready {
		if running >= set.MinRunningEfforts {
			break
		}
		p, err := e.deps.Store.GetProject(ctx, ef.Project)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		cap := p.MaxRunningEfforts
		if cap < 1 {
			cap = 1
		}
		n, err := e.deps.Store.CountEffortsByStatus(ctx, p.Slug, model.EffortRunning)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if n >= cap {
			continue
		}
		if ef.BudgetTokens <= 0 && ef.BudgetSeconds <= 0 {
			continue // a backlog effort without a budget never starts on its own
		}
		b, err := e.EffortBudget(ctx, ef)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if b.Exhausted() {
			continue
		}
		ok, err := e.StartConditionMet(ctx, ef)
		if err != nil {
			errs = append(errs, fmt.Errorf("effort %d start condition: %w", ef.ID, err))
			continue
		}
		if !ok {
			continue
		}
		if _, err := e.SetFreshBase(ctx, ef.ID); err != nil {
			errs = append(errs, fmt.Errorf("effort %d fresh base: %w", ef.ID, err))
			// A failed fetch leaves the stored base; branch creation can still
			// use it, so this is not a reason to skip the start.
		}
		changed, err := e.deps.Store.UpdateEffortStatusIf(ctx, SystemActor, ef.ID, model.EffortReady, model.EffortRunning)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if !changed {
			continue // someone unparked or started it between the read and now
		}
		if err := e.runs.Start(ctx, SystemActor, p.Slug); err != nil {
			// Put it back so the next tick can try again; the run controller
			// may have refused because the service is held or the budget moved.
			_, _ = e.deps.Store.UpdateEffortStatusIf(context.WithoutCancel(ctx), SystemActor, ef.ID, model.EffortRunning, model.EffortReady)
			errs = append(errs, fmt.Errorf("effort %d auto-start: %w", ef.ID, err))
			continue
		}
		running++
		_ = e.noteEffort(ctx, SystemActor, eventBacklogStart, ef.ID, map[string]any{
			"priority": ef.Priority, "condition": ef.StartCondition,
		})
		e.changed()
	}
	return errors.Join(errs...)
}
