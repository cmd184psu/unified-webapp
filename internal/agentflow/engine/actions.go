package engine

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
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
	if clean {
		// Halted tasks have no open attempt, so nothing can start in the
		// worktree between its removal and the reset. Check halted first so a
		// refused retry removes nothing.
		status, err := s.TaskStatuses(ctx, ef.ID, e.limitsFor(set))
		if err != nil {
			return err
		}
		if st := status[t.Key]; !st.Halted() {
			return fmt.Errorf("task %d is %s, not halted: %w", task, st, store.ErrConflict)
		}
		if err := e.removeTaskWork(ctx, proj, ef, t); err != nil {
			return err
		}
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
	for range 3 {
		ef, err := e.deps.Store.GetEffort(ctx, effort)
		if err != nil {
			return q, err
		}
		switch ef.Status {
		case model.EffortQueueAppr, model.EffortRunning, model.EffortPaused:
		default:
			return q, nil
		}
		// Conditional on the status just read, so a concurrent transition is
		// re-read rather than overwritten.
		changed, err := e.deps.Store.UpdateEffortStatusIf(ctx, actor, effort, ef.Status, model.EffortQueueDraft)
		if err != nil || changed {
			return q, err
		}
	}
	return q, nil
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

// Unregistered is what UnregisterProject leaves for the human: the project's
// branches that still exist in the repository, and the commands that delete
// them. The engine never deletes them.
type Unregistered struct {
	Branches []string
	Commands []string
}

// UnregisterProject removes a stopped project with no active effort: its
// AgentFlow records in one transaction, then (best effort, logged) its
// working files under the data directory and its stale worktree
// registrations. The repository and its branches are not touched.
func (e *Engine) UnregisterProject(ctx context.Context, actor, slug string) (Unregistered, error) {
	s := e.deps.Store
	proj, err := s.GetProject(ctx, slug)
	if err != nil {
		return Unregistered{}, err
	}
	defer e.runs.kick()
	e.runs.mu.Lock()
	defer e.runs.mu.Unlock()
	if st := e.runs.entry(slug).state; st != model.RunStopped {
		return Unregistered{}, fmt.Errorf("project %q is %s: stop the run first: %w", slug, st, store.ErrConflict)
	}
	// Collect what the rows will no longer tell us once they are gone.
	efforts, err := s.ListEfforts(ctx, slug)
	if err != nil {
		return Unregistered{}, err
	}
	stageIDs, err := s.ProjectStageIDs(ctx, slug)
	if err != nil {
		return Unregistered{}, err
	}
	var names []string
	var taskIDs []int64
	for _, ef := range efforts {
		if ef.IntegrationBranch != "" {
			names = append(names, ef.IntegrationBranch)
		} else {
			names = append(names, gitsvc.IntegrationBranch(effortName(ef.ID), ""))
		}
		tasks, err := s.ListTasks(ctx, ef.ID)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return Unregistered{}, err
		}
		for _, t := range tasks {
			names = append(names, gitsvc.TaskBranch(effortName(ef.ID), taskName(t)))
			taskIDs = append(taskIDs, t.ID)
		}
	}
	if err := s.DeleteProject(ctx, actor, slug); err != nil {
		return Unregistered{}, err
	}
	delete(e.runs.runs, slug)

	e.mu.Lock()
	for _, ef := range efforts {
		delete(e.planWaiting, ef.ID)
	}
	for _, id := range stageIDs {
		if t := e.retryTimers[id]; t != nil {
			t.Stop()
			delete(e.retryTimers, id)
		}
	}
	e.mu.Unlock()

	// Best-effort file cleanup, only below the data directory.
	var paths []string
	paths = append(paths, filepath.Join(e.worktreeRoot(), slug))
	for _, ef := range efforts {
		paths = append(paths, filepath.Join(e.deps.DataDir, "baselines", strconv.FormatInt(ef.ID, 10)))
	}
	for _, id := range taskIDs {
		paths = append(paths, filepath.Join(e.deps.DataDir, "tasks", strconv.FormatInt(id, 10)))
	}
	for _, id := range stageIDs {
		paths = append(paths, e.StageDir(id))
	}
	for _, p := range paths {
		if err := os.RemoveAll(p); err != nil {
			log.Printf("agentflow: unregister %s: remove %s: %v", slug, p, err)
		}
	}
	out := Unregistered{Branches: []string{}, Commands: []string{}}
	if g := e.deps.Git; g != nil {
		if err := g.PruneWorktrees(ctx, proj.RepoPath); err != nil {
			log.Printf("agentflow: unregister %s: prune worktrees: %v", slug, err)
		}
		seen := map[string]bool{}
		var uniq []string
		for _, n := range names {
			if !seen[n] {
				seen[n] = true
				uniq = append(uniq, n)
			}
		}
		out.Branches = append(out.Branches, g.ExistingBranches(ctx, proj.RepoPath, uniq)...)
		for _, b := range out.Branches {
			out.Commands = append(out.Commands, gitsvc.DeleteBranchCommand(proj.RepoPath, b))
		}
	}
	return out, nil
}

// Dropped is what DropAllProjects removed: the slugs, the repository branches
// and delete commands left behind (nothing in a repository is touched), and
// the database copy taken first.
type Dropped struct {
	Projects []string
	Branches []string
	Commands []string
	Archive  string
}

// DropAllProjects removes every project and effort in one call, for a clean
// slate between trials. It first saves a copy of the whole database under the
// data directory's archive/ (history, statistics and strikes stay readable
// there), then for each project abandons its active effort, stops its run and
// unregisters it. Backends, models, tiers, keys, candidate lists and settings
// are kept. It stops at the first project it cannot remove and returns what it
// had dropped so far with the error.
func (e *Engine) DropAllProjects(ctx context.Context, actor string) (Dropped, error) {
	s := e.deps.Store
	out := Dropped{Projects: []string{}, Branches: []string{}, Commands: []string{}}
	projects, err := s.ListProjects(ctx)
	if err != nil {
		return out, err
	}
	if len(projects) == 0 {
		return out, nil
	}
	dir := filepath.Join(e.deps.DataDir, "archive")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return out, err
	}
	path := filepath.Join(dir, "agentflow-"+e.deps.Clock().Format("20060102-150405")+"-drop-all.db")
	if _, err := s.DB().ExecContext(ctx, "VACUUM INTO ?", path); err != nil {
		return out, fmt.Errorf("archive the database before dropping: %w", err)
	}
	out.Archive = path
	for _, p := range projects {
		efforts, err := s.ListEfforts(ctx, p.Slug)
		if err != nil {
			return out, err
		}
		for _, ef := range efforts {
			if model.IsActiveEffortStatus(ef.Status) {
				if err := e.AbandonEffort(ctx, actor, ef.ID); err != nil {
					return out, err
				}
			}
		}
		if err := e.runs.Stop(ctx, actor, p.Slug); err != nil {
			return out, err
		}
		left, err := e.UnregisterProject(ctx, actor, p.Slug)
		if err != nil {
			return out, err
		}
		out.Projects = append(out.Projects, p.Slug)
		out.Branches = append(out.Branches, left.Branches...)
		out.Commands = append(out.Commands, left.Commands...)
	}
	return out, nil
}
