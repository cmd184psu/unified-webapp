package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"cmd184psu/unified-webapp/internal/agentflow/gitsvc"
	"cmd184psu/unified-webapp/internal/agentflow/model"
)

// The optional cleanup ("deslop") pass (AT11). After a review pass, and only
// for a project with cleanup_enabled, the engine commits the reviewed tree as
// a checkpoint on the task branch, a cleanup stage edits the worktree, and
// verify runs again. If that verify fails, or the cleanup stage itself does
// not finish, the engine resets the worktree to the checkpoint and carries on
// with the reviewed version: cleanup can never cost a task its pass.

// cleanupCheckpointFile records the checkpoint commit of an attempt. It lives
// outside the worktree, like base_commit.
func (e *Engine) cleanupCheckpointFile(c *chain) string {
	return filepath.Join(e.deps.DataDir, "tasks", strconv.FormatInt(c.task.ID, 10), "cleanup_checkpoint_"+strconv.FormatInt(c.att.ID, 10))
}

// hasStage reports whether the attempt has a stage of role.
func (c *chain) hasStage(role string) bool {
	for _, st := range c.stages {
		if st.Role == role {
			return true
		}
	}
	return false
}

// afterReview takes the step after a passing review: straight to commit and
// merge, or through the cleanup pass when the project enables it.
func (e *Engine) afterReview(ctx context.Context, c *chain, last model.Stage) (bool, error) {
	if more, err := e.askAnotherReviewer(ctx, c, last); more || err != nil {
		return more, err
	}
	if !c.proj.CleanupEnabled || c.hasStage("cleanup") {
		return e.commitAndMerge(ctx, c, last)
	}
	if err := e.checkpoint(ctx, c); err != nil {
		if errors.Is(err, gitsvc.ErrGitlink) {
			return true, e.fail(ctx, c, Failure{Kind: FailCommit, Stage: last})
		}
		return false, err
	}
	// Nothing to clean when the worker declared nothing to change and the
	// checkpoint found nothing to commit.
	if reason := c.noChangesReason(); reason != "" {
		if none, err := e.atBase(ctx, c); err != nil {
			return false, err
		} else if none {
			return true, e.completeNoChanges(ctx, c, reason)
		}
	}
	pk, reason, err := e.pickBackend(ctx, "worker", c.task, c.proj, "")
	if err != nil || reason != "" {
		return false, err // the reason is on the task; the next Advance tries again
	}
	if err := e.submit(ctx, c, BackendLane(pk.Backend), "cleanup", pk.Backend, pk.Model, last.QueueHash); err != nil {
		return false, err
	}
	if c.task.WaitingReason != "" {
		return true, e.deps.Store.SetWaitingReason(ctx, c.task.ID, "")
	}
	return true, nil
}

// checkpoint commits the reviewed worktree as the engine and records the
// commit, once per attempt.
func (e *Engine) checkpoint(ctx context.Context, c *chain) error {
	f := e.cleanupCheckpointFile(c)
	if _, err := os.Stat(f); err == nil {
		return nil
	}
	sha, err := e.deps.Git.CommitAll(ctx, c.proj.RepoPath, c.att.Worktree,
		c.task.Key+": "+c.task.Title+" (before cleanup)", gitsvc.Trailers{Task: taskName(c.task), Attempt: c.att.N})
	if errors.Is(err, gitsvc.ErrNothingToCommit) {
		var h gitsvc.HeadInfo
		if h, err = e.deps.Git.Head(ctx, c.att.Worktree); err == nil {
			sha = h.Commit
		}
	}
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(f), 0o700); err != nil {
		return err
	}
	return os.WriteFile(f, []byte(sha), 0o600)
}

// afterCleanup takes the step after a cleanup stage that finished: verify the
// cleaned tree. Any other end of the stage (blocked, timed out, a policy
// violation, an infrastructure failure) drops the cleanup.
func (e *Engine) afterCleanup(ctx context.Context, c *chain, last model.Stage) (bool, error) {
	if last.Outcome == OutcomePass {
		return true, e.submitVerify(ctx, c, last)
	}
	return e.dropCleanup(ctx, c, last, "the cleanup stage ended "+last.Outcome)
}

// afterCleanupVerify takes the step after the verify that follows a cleanup
// stage: a green report goes on to commit and merge, a red one rolls the
// cleanup back first.
func (e *Engine) afterCleanupVerify(ctx context.Context, c *chain, last model.Stage) (bool, error) {
	if verifyPassed(last) {
		return e.commitAndMerge(ctx, c, last)
	}
	return e.dropCleanup(ctx, c, last, "verify failed after cleanup")
}

// dropCleanup resets the worktree to the pre-cleanup checkpoint (an
// engine-owned write), notes why in the task's progress log and continues
// with the reviewed version.
func (e *Engine) dropCleanup(ctx context.Context, c *chain, last model.Stage, why string) (bool, error) {
	b, err := os.ReadFile(e.cleanupCheckpointFile(c))
	if err != nil {
		return false, fmt.Errorf("cleanup checkpoint: %w", err)
	}
	if err := e.deps.Git.ResetTask(ctx, c.proj.RepoPath, c.att.Worktree, strings.TrimSpace(string(b))); err != nil {
		return false, fmt.Errorf("roll back cleanup: %w", err)
	}
	if err := e.deps.Store.AppendProgress(ctx, c.task.ID, c.att.ID, "cleanup rolled back ("+why+"); merging the reviewed version"); err != nil {
		return false, err
	}
	return e.commitAndMerge(ctx, c, last)
}

// cleanupDropsOn says whether the newest stage belongs to the cleanup pass and
// ended in a way that is not a plain pass: a cleanup stage that did not finish,
// or the verify after it (a red report is a verify outcome of repair, which
// the generic path would turn into a failed attempt). A stop, restart or
// retired backend (cancel, lost, rerouted) keeps its own handling, as does an
// infrastructure failure of the verify itself, which is simply repeated.
func cleanupDropsOn(c *chain, st model.Stage) bool {
	switch st.Outcome {
	case OutcomeInfraCancel, OutcomeInfraLost, OutcomeInfraRerouted:
		return false
	}
	switch st.Role {
	case "cleanup":
		return st.Outcome != OutcomePass
	case "verify":
		return c.hasStage("cleanup") && !IsInfra(st.Outcome)
	}
	return false
}

// askAnotherReviewer widens the review to a panel: while fewer reviewers than the
// panel setting have passed the work, another model is asked. It reports true
// when it did, or when it is waiting for a reviewer's slot.
//
// A normal task keeps the best-effort review_panel path: when no further
// reviewer model could ever serve, the reviews that passed stand, so a thin
// pool of free models does not hold the task up forever.
//
// A risky task uses risky_review_panel and counts distinct reviewer FAMILIES:
// the second reviewer is never the first one's family. If the pool can only
// supply one family, the task does not merge on that single pass; it waits
// (surfaced as attention) instead of reviewing with two of a kind.
func (e *Engine) askAnotherReviewer(ctx context.Context, c *chain, last model.Stage) (bool, error) {
	set, err := e.deps.Store.EffectiveSettings(ctx, c.proj.Slug)
	if err != nil {
		return false, err
	}
	risky := c.task.Risk == model.RiskRisky
	panel := set.ReviewPanel
	if risky {
		panel = set.RiskyReviewPanel
		if panel <= 0 {
			panel = 2
		}
	}
	if panel <= 1 {
		return false, nil
	}
	passed := map[string]bool{}
	passedFamilies := map[string]bool{}
	for _, st := range c.stages {
		if st.Role == "reviewer" && st.Outcome == OutcomePass && st.Model != "" {
			passed[st.Model] = true
			passedFamilies[model.ModelFamily(st.Model)] = true
		}
	}
	var have int
	if risky {
		have = len(passedFamilies)
	} else {
		have = len(passed)
	}
	if have >= panel {
		return false, nil
	}
	v, err := e.newView(ctx)
	if err != nil {
		return false, err
	}
	v.panelAvoid = map[string]bool{}
	for m := range passed {
		v.panelAvoid[m] = true
	}
	if risky {
		// A risky task widens the panel until risky_review_panel distinct
		// families have passed: keep every reviewer whose family already
		// passed out of the next pick, so the next reviewer is a new family.
		// The worker's own family is NOT excluded on its own (H28): it may be
		// the only second family available, and reviewerFor already keeps the
		// worker's own model out.
		for _, cand := range v.candidates("reviewer", c.proj.Slug) {
			if passedFamilies[model.ModelFamily(cand.Model)] {
				v.panelAvoid[cand.Model] = true
			}
		}
	}
	if _, why := reviewerFor(v, c.proj, "", c.workerModel(), false); why != "" {
		if risky {
			// only one family can serve: do not let the single pass stand.
			// Say what a person has to do: a second distinct family is
			// required, so waiting for a slot in the same family will not help.
			why = "risky task: a reviewer from a second model family is required (risky_review_panel=" + strconv.Itoa(panel) + "); " + why
			return true, e.setWaiting(ctx, c.task, why)
		}
		return false, nil // no other reviewer model could serve: the panel is as wide as the pool
	}
	pk, why := reviewerFor(v, c.proj, "", c.workerModel(), true)
	if why != "" {
		if risky {
			why = "risky task: a reviewer from a second model family is required (risky_review_panel=" + strconv.Itoa(panel) + "); " + why
		}
		return true, e.setWaiting(ctx, c.task, why) // one could serve, but has no free slot yet
	}
	if err := e.submit(ctx, c, BackendLane(pk.Backend), "reviewer", pk.Backend, pk.Model, last.QueueHash); err != nil {
		return false, err
	}
	if c.task.WaitingReason != "" {
		return true, e.deps.Store.SetWaitingReason(ctx, c.task.ID, "")
	}
	return true, nil
}
