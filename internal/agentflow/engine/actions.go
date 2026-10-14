package engine

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"cmd184psu/unified-webapp/internal/agentflow/gitsvc"
	"cmd184psu/unified-webapp/internal/agentflow/model"
	"cmd184psu/unified-webapp/internal/agentflow/store"
)

// The operator's actions on halted work (AT9). Each one is audited with the
// actor by the store call it makes (task.reset, queue.edit_task,
// task.override) or, for AbandonEffort, by an effort.abandon event.

// locateTask finds a task in its project's active effort.
func (e *Engine) locateTask(ctx context.Context, id int64) (store.Project, model.Effort, model.Task, error) {
	s := e.deps.Store
	projects, err := s.ListProjects(ctx)
	if err != nil {
		return store.Project{}, model.Effort{}, model.Task{}, err
	}
	for _, p := range projects {
		ef, err := s.ActiveEffort(ctx, p.Slug)
		if errors.Is(err, store.ErrNotFound) {
			continue
		}
		if err != nil {
			return store.Project{}, model.Effort{}, model.Task{}, err
		}
		tasks, err := s.ListTasks(ctx, ef.ID)
		if errors.Is(err, store.ErrNotFound) {
			continue
		}
		if err != nil {
			return store.Project{}, model.Effort{}, model.Task{}, err
		}
		for _, t := range tasks {
			if t.ID == id {
				return p, ef, t, nil
			}
		}
	}
	return store.Project{}, model.Effort{}, model.Task{}, fmt.Errorf("task %d in an active effort: %w", id, store.ErrNotFound)
}

// Retry resets a halted task's counters so the dispatcher starts it again
// (AT9). A clean retry also removes the task's worktree and branch, so its
// first attempt starts from the integration branch; otherwise the worktree is
// reused. Either way the first new attempt is a normal one.
func (e *Engine) Retry(ctx context.Context, actor string, task int64, clean bool) error {
	s := e.deps.Store
	proj, ef, t, err := e.locateTask(ctx, task)
	if err != nil {
		return err
	}
	set, err := s.EffectiveSettings(ctx, proj.Slug)
	if err != nil {
		return err
	}
	atts, err := s.ListAttempts(ctx, task)
	if err != nil {
		return err
	}
	if err := s.ResetTask(ctx, actor, task, clean, e.limitsFor(set)); err != nil {
		return err
	}
	// ResetTask abandoned any open attempt; its stages must not run on.
	for _, a := range atts {
		if a.Outcome == "" {
			if err := e.cancelAttemptStages(ctx, a.ID); err != nil {
				return err
			}
		}
	}
	if err := s.SetWaitingReason(ctx, task, ""); err != nil {
		return err
	}
	if clean {
		if err := e.removeTaskWork(ctx, proj, ef, t); err != nil {
			return err
		}
	}
	e.runs.kick()
	return nil
}

// EditTask saves a new definition of one task. The queue's hash is recomputed
// and the queue returns to draft, so stages carrying the old hash are
// discarded without counting (QA2). An effort that had an approved queue goes
// back to queue_draft until the new hash is approved.
func (e *Engine) EditTask(ctx context.Context, actor string, effort int64, def model.TaskDef) (model.Queue, error) {
	q, err := e.deps.Store.EditTask(ctx, actor, effort, def)
	if err != nil {
		return q, err
	}
	ef, err := e.deps.Store.GetEffort(ctx, effort)
	if err != nil {
		return q, err
	}
	switch ef.Status {
	case model.EffortQueueAppr, model.EffortRunning, model.EffortPaused:
		draft := model.EffortQueueDraft
		_, err = e.deps.Store.UpdateEffort(ctx, actor, effort, store.EffortPatch{Status: &draft})
	}
	return q, err
}

// Override marks a halted task passed by a human decision (AT9). Its
// dependents unblock, and the effort completes if it was the last open task.
// The task's worktree and branch are removed; its work is not merged.
func (e *Engine) Override(ctx context.Context, actor string, task int64, reason string) error {
	s := e.deps.Store
	proj, ef, t, err := e.locateTask(ctx, task)
	if err != nil {
		return err
	}
	set, err := s.EffectiveSettings(ctx, proj.Slug)
	if err != nil {
		return err
	}
	status, err := s.TaskStatuses(ctx, ef.ID, e.limitsFor(set))
	if err != nil {
		return err
	}
	if st := status[t.Key]; !st.Halted() {
		return fmt.Errorf("task %d is %s, not halted: %w", task, st, store.ErrConflict)
	}
	if err := s.OverrideTask(ctx, actor, task, reason); err != nil {
		return err
	}
	if err := s.SetWaitingReason(ctx, task, ""); err != nil {
		return err
	}
	if err := e.removeTaskWork(ctx, proj, ef, t); err != nil {
		return err
	}
	// Completes the effort when this was the last task; a failure here is
	// repeated by the next reconcile tick.
	_ = e.Advance(ctx)
	e.runs.kick()
	return nil
}

// AbandonEffort gives up the effort (AT9): its run stops, in-flight stages are
// cancelled, every worktree and agentflow branch it made is removed (ST4) and
// the effort is marked abandoned, which frees the project for a new one.
func (e *Engine) AbandonEffort(ctx context.Context, actor string, effort int64) error {
	s := e.deps.Store
	ef, err := s.GetEffort(ctx, effort)
	if err != nil {
		return err
	}
	if !model.IsActiveEffortStatus(ef.Status) {
		return fmt.Errorf("effort %d is %s: %w", effort, ef.Status, store.ErrConflict)
	}
	proj, err := s.GetProject(ctx, ef.Project)
	if err != nil {
		return err
	}
	// Stop dispatch and cancel what is in flight while the effort is still
	// the project's active one.
	if err := e.runs.Stop(ctx, actor, proj.Slug); err != nil {
		return err
	}
	tasks, err := s.ListTasks(ctx, effort)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return err
	}
	for _, t := range tasks {
		if err := e.removeTaskWork(ctx, proj, ef, t); err != nil {
			return err
		}
	}
	if err := e.removeEffortWork(ctx, proj, ef); err != nil {
		return err
	}
	abandoned := model.EffortAbandoned
	if _, err := s.UpdateEffort(ctx, actor, effort, store.EffortPatch{Status: &abandoned}); err != nil {
		return err
	}
	err = s.WithTx(ctx, func(tx *sql.Tx) error {
		_, err := store.AppendEvent(tx, actor, "effort.abandon", "effort", strconv.FormatInt(effort, 10),
			map[string]any{"project": proj.Slug, "was": ef.Status})
		return err
	})
	e.runs.kick()
	return err
}

// removeTaskWork deletes a task's worktree, branch and recorded base commit
// (ST4). Each step tolerates having already happened.
func (e *Engine) removeTaskWork(ctx context.Context, proj store.Project, ef model.Effort, t model.Task) error {
	if g := e.deps.Git; g != nil {
		wt := e.taskWorktreePath(proj.Slug, ef.ID, t)
		if err := g.RemoveWorktree(ctx, proj.RepoPath, wt); err != nil && !errors.Is(err, gitsvc.ErrNotRegistered) {
			return fmt.Errorf("remove task worktree: %w", err)
		}
		// A branch that is gone or not ours is nothing to clean up.
		_ = g.DeleteBranch(ctx, proj.RepoPath, gitsvc.TaskBranch(effortName(ef.ID), taskName(t)))
	}
	return os.RemoveAll(filepath.Dir(e.baseCommitFile(t.ID)))
}

// removeEffortWork deletes the effort's integration worktree and branch and
// its baselines. A user-named integration branch that agentflow did not
// create is kept (DeleteBranch refuses it).
func (e *Engine) removeEffortWork(ctx context.Context, proj store.Project, ef model.Effort) error {
	if g := e.deps.Git; g != nil {
		wt, err := g.IntegrationPath(proj.Slug, effortName(ef.ID))
		if err != nil {
			return err
		}
		if err := g.RemoveWorktree(ctx, proj.RepoPath, wt); err != nil && !errors.Is(err, gitsvc.ErrNotRegistered) {
			return fmt.Errorf("remove integration worktree: %w", err)
		}
		if ef.IntegrationBranch != "" {
			_ = g.DeleteBranch(ctx, proj.RepoPath, ef.IntegrationBranch)
		}
	}
	return os.RemoveAll(filepath.Join(e.deps.DataDir, "baselines", strconv.FormatInt(ef.ID, 10)))
}

// cancelAttemptStages cancels the lane jobs of an attempt's open stages and
// closes the stages that never got to write an outcome.
func (e *Engine) cancelAttemptStages(ctx context.Context, attempt int64) error {
	stages, err := e.deps.Store.ListAttemptStages(ctx, attempt)
	if err != nil {
		return err
	}
	e.submitMu.Lock() // no stage row without its job while we scan
	idx, err := e.indexJobs()
	e.submitMu.Unlock()
	if err != nil {
		return err
	}
	var errs []error
	for _, st := range stages {
		if st.State == model.StageDone {
			continue
		}
		for _, ref := range idx[st.ID] {
			if !jobTerminal(ref.job.Status) {
				if _, err := ref.lane.Cancel(ref.job.ID); err != nil {
					errs = append(errs, err)
				}
			}
		}
		if err := e.deps.Store.FinishStage(ctx, st.ID, OutcomeInfraCancel, "task reset by operator", nil); err != nil && !errors.Is(err, store.ErrConflict) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
