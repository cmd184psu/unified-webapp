package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"cmd184psu/unified-webapp/internal/agentflow/gitsvc"
	"cmd184psu/unified-webapp/internal/agentflow/model"
	"cmd184psu/unified-webapp/internal/agentflow/store"
	"cmd184psu/unified-webapp/internal/agentflow/verify"
	"cmd184psu/unified-webapp/internal/taskmaster/golane"
)

// mergeExecutor is the engine-owned merge stage on the width-1 merge lane
// (PX3): merge the task branch into the integration worktree, then verify the
// integrated tree inside the same stage, so no other merge can slip in
// between. A conflict or a post-merge verify failure leaves the integration
// branch exactly as it was and ends the stage as repair.
type mergeExecutor struct{ e *Engine }

// mergeReport is the merge stage's parsed output.
type mergeReport struct {
	Kind   string               `json:"kind"` // merged, conflict, verify
	Commit string               `json:"commit,omitempty"`
	Files  []string             `json:"files,omitempty"`
	Report *verify.VerifyReport `json:"report,omitempty"`
}

func mergeResult(outcome, raw string, m mergeReport) (StageResult, error) {
	b, err := json.Marshal(m)
	return StageResult{Outcome: outcome, Raw: raw, Parsed: b}, err
}

// Execute implements Executor.
func (x mergeExecutor) Execute(ctx context.Context, rc golane.RunContext, st model.Stage, p StagePayload) (StageResult, error) {
	e := x.e
	g := e.deps.Git
	c, err := e.loadChain(ctx, p)
	if err != nil {
		return StageResult{}, err
	}
	set, err := e.deps.Store.EffectiveSettings(ctx, p.Project)
	if err != nil {
		return StageResult{}, err
	}
	repo, name := c.proj.RepoPath, effortName(p.Effort)
	branch, intWT, err := e.ensureIntegration(ctx, c.proj, p.Effort)
	if err != nil {
		return StageResult{}, err
	}
	before, err := g.Head(ctx, intWT)
	if err != nil {
		return StageResult{}, err
	}
	if err := e.recordPreMerge(st.ID, p, before.Commit); err != nil {
		return StageResult{}, err
	}
	merged, err := g.Merge(ctx, repo, intWT, branch, gitsvc.TaskBranch(name, taskName(c.task)))
	var conflict *gitsvc.ConflictError
	if errors.As(err, &conflict) {
		return mergeResult(OutcomeRepair, conflict.Error(), mergeReport{Kind: "conflict", Files: conflict.Files})
	}
	if err != nil {
		return StageResult{}, err
	}

	rep, results, err := x.verifyMerged(ctx, rc, c, st, intWT, set)
	if err == nil && reportOK(rep) {
		if err := e.saveBaseline(ctx, p.Effort, baselineFrom(merged, results)); err != nil {
			return StageResult{}, err
		}
		return mergeResult(OutcomePass, "merged "+merged, mergeReport{Kind: "merged", Commit: merged, Report: &rep})
	}
	// Anything short of a green integrated tree undoes the merge; the user
	// never sees an unverified integration commit.
	if rerr := g.ResetIntegration(context.WithoutCancel(ctx), repo, intWT, branch, before.Commit); rerr != nil {
		return StageResult{}, errors.Join(err, fmt.Errorf("reset integration branch after failed merge: %w", rerr))
	}
	if err != nil {
		return StageResult{}, err
	}
	return mergeResult(OutcomeRepair, "post-merge verify failed: "+failingChecks(rep), mergeReport{Kind: "verify", Report: &rep})
}

// preMerge is what a merge stage writes into its stage directory before it
// touches the integration branch, so reconcile can undo a merge whose stage
// was lost to a crash.
type preMerge struct {
	Project string `json:"project"`
	Effort  int64  `json:"effort"`
	Commit  string `json:"commit"`
}

func (e *Engine) preMergeFile(stage int64) string {
	return filepath.Join(e.StageDir(stage), "pre_merge.json")
}

func (e *Engine) recordPreMerge(stage int64, p StagePayload, commit string) error {
	b, err := json.Marshal(preMerge{Project: p.Project, Effort: p.Effort, Commit: commit})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(e.StageDir(stage), 0o700); err != nil {
		return err
	}
	return os.WriteFile(e.preMergeFile(stage), b, 0o600)
}

// undoLostMerge resets the integration branch to the commit recorded before a
// merge stage that was closed infra:lost, so an unverified (or unrecorded)
// merge commit does not stay on it. A stage that never started its merge has
// no record and nothing to undo.
func (e *Engine) undoLostMerge(ctx context.Context, stage int64) error {
	g := e.deps.Git
	if g == nil {
		return nil
	}
	b, err := os.ReadFile(e.preMergeFile(stage))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var pm preMerge
	if err := json.Unmarshal(b, &pm); err != nil {
		return err
	}
	proj, err := e.deps.Store.GetProject(ctx, pm.Project)
	if err != nil {
		return err
	}
	ef, err := e.deps.Store.GetEffort(ctx, pm.Effort)
	if err != nil {
		return err
	}
	intWT, err := g.IntegrationPath(proj.Slug, effortName(ef.ID))
	if err != nil {
		return err
	}
	if _, err := os.Stat(intWT); err != nil {
		return nil // effort already cleaned up
	}
	if err := g.ResetIntegration(ctx, proj.RepoPath, intWT, ef.IntegrationBranch, pm.Commit); err != nil {
		return fmt.Errorf("reset integration after lost merge stage %d: %w", stage, err)
	}
	return os.Remove(e.preMergeFile(stage))
}

// verifyMerged runs the feedback commands on the integrated tree against the
// baseline, with verify_timeout.
func (x mergeExecutor) verifyMerged(ctx context.Context, rc golane.RunContext, c *chain, st model.Stage, intWT string, set store.Settings) (verify.VerifyReport, []verify.CommandResult, error) {
	e := x.e
	ctx, cancel := withTimeout(ctx, set.VerifyTimeout)
	defer cancel()
	admin, err := e.deps.Git.AdminGitDir(ctx, c.proj.RepoPath, intWT)
	if err != nil {
		return verify.VerifyReport{}, nil, err
	}
	rep := verify.VerifyReport{Checks: []verify.Check{}, Warnings: []string{}, NewFailures: []string{}, BaselineFailures: []string{}}
	toolchains := model.MergeToolchains(c.proj.Toolchains, model.TaskToolchains(c.task.TaskDef))
	dotnet := model.HasToolchain(c.task.TaskDef, model.ToolchainDotnet)
	results, err := e.feedback(ctx, c, &rep, intWT, admin, filepath.Join(e.StageDir(st.ID), "home"), set, c.touchedFiles(), toolchains, dotnet)
	return rep, results, err
}

// completeAttempt runs once a merge stage passed: the attempt is passed, its
// worktree and branch are removed (ST4), dependents become ready (derived) and
// the effort completes when every task is passed or overridden (AT8). The
// baseline was refreshed by the merge stage itself (V5). Every step tolerates
// having run before, so a restart between steps repeats safely.
func (e *Engine) completeAttempt(ctx context.Context, c *chain) error {
	g := e.deps.Git
	name := effortName(c.eff.ID)
	if err := g.RemoveWorktree(ctx, c.proj.RepoPath, c.att.Worktree); err != nil &&
		!errors.Is(err, gitsvc.ErrNotRegistered) {
		return fmt.Errorf("remove task worktree: %w", err)
	}
	// Already merged, so the branch only clutters the user's repo. A re-run
	// finds it gone.
	_ = g.DeleteBranch(ctx, c.proj.RepoPath, gitsvc.TaskBranch(name, taskName(c.task)))
	if err := os.RemoveAll(filepath.Dir(e.baseCommitFile(c.task.ID))); err != nil {
		return err
	}
	if err := e.appendProgress(ctx, c, "passed"); err != nil {
		return err
	}
	if err := e.deps.Store.FinishAttempt(ctx, c.att.ID, model.OutcomePassed, "", c.reviewerModel()); err != nil && !errors.Is(err, store.ErrConflict) {
		return err
	}
	return nil
}

// completeEffort marks the effort complete when every task is passed or
// overridden (AT8) and removes its integration worktree; the integration
// branch stays for the human to merge and push (GIT4, PushInstructions).
// Advance calls it for every active effort, so a restart between the last
// task's finish and this point still completes the effort.
func (e *Engine) completeEffort(ctx context.Context, proj store.Project, ef model.Effort) error {
	set, err := e.deps.Store.EffectiveSettings(ctx, proj.Slug)
	if err != nil {
		return err
	}
	status, err := e.deps.Store.TaskStatuses(ctx, ef.ID, e.limitsFor(set))
	if err != nil {
		return err
	}
	if !model.AllDone(status) {
		return nil
	}
	if ok, err := e.acceptance(ctx, proj, ef); err != nil || !ok {
		return err
	}
	intWT, err := e.deps.Git.IntegrationPath(proj.Slug, effortName(ef.ID))
	if err != nil {
		return err
	}
	if err := e.deps.Git.RemoveWorktree(ctx, proj.RepoPath, intWT); err != nil && !errors.Is(err, gitsvc.ErrNotRegistered) {
		return fmt.Errorf("remove integration worktree: %w", err)
	}
	if err := os.RemoveAll(filepath.Join(e.deps.DataDir, "baselines", strconv.FormatInt(ef.ID, 10))); err != nil {
		return err
	}
	done := model.EffortComplete
	if _, err = e.deps.Store.UpdateEffort(ctx, SystemActor, ef.ID, store.EffortPatch{Status: &done}); err != nil {
		return err
	}
	return e.runs.finish(ctx, proj.Slug, ef)
}
