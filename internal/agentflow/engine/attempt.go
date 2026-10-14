package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"cmd184psu/unified-webapp/internal/agentflow/gitsvc"
	"cmd184psu/unified-webapp/internal/agentflow/model"
	"cmd184psu/unified-webapp/internal/agentflow/schema"
	"cmd184psu/unified-webapp/internal/agentflow/store"
	"cmd184psu/unified-webapp/internal/taskmaster/golane"
)

// Engine-owned stages record these as their backend; the names are reserved
// (no backend may use them).
const (
	verifyBackend = "verify"
	mergeBackend  = "merge"
)

// effortName is the effort's name in branch and worktree paths.
func effortName(id int64) string { return "effort-" + strconv.FormatInt(id, 10) }

var gitNameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// taskName is the task's name in branch and worktree paths: its key when git
// accepts it, else a name made from its id.
func taskName(t model.Task) string {
	k := t.Key
	if gitNameRE.MatchString(k) && !strings.Contains(k, "..") && !strings.HasSuffix(k, ".lock") && !strings.HasSuffix(k, ".") {
		return k
	}
	return "task-" + strconv.FormatInt(t.ID, 10)
}

// worktreeRoot is where every task and integration worktree lives (PX2).
func (e *Engine) worktreeRoot() string {
	if e.deps.Git != nil {
		return e.deps.Git.WorktreeRoot()
	}
	return filepath.Join(e.deps.DataDir, "worktrees")
}

// taskWorktreePath is the planned worktree of a task.
func (e *Engine) taskWorktreePath(project string, effort int64, t model.Task) string {
	return filepath.Join(e.worktreeRoot(), project, effortName(effort), taskName(t))
}

// baseCommitFile records the integration commit a task's worktree was forked
// from; verify diffs against it. It lives outside the worktree.
func (e *Engine) baseCommitFile(task int64) string {
	return filepath.Join(e.deps.DataDir, "tasks", strconv.FormatInt(task, 10), "base_commit")
}

// chain is everything one attempt's step needs.
type chain struct {
	proj   store.Project
	eff    model.Effort
	task   model.Task
	att    model.Attempt
	stages []model.Stage
}

func (c *chain) payload(queueHash string) StagePayload {
	return StagePayload{Project: c.proj.Slug, Effort: c.eff.ID, Task: c.task.ID, Attempt: c.att.ID, QueueHash: queueHash}
}

// reviewerModel is the model of the attempt's newest review stage, or "".
func (c *chain) reviewerModel() string {
	for i := len(c.stages) - 1; i >= 0; i-- {
		if c.stages[i].Role == "reviewer" {
			return c.stages[i].Model
		}
	}
	return ""
}

// workerModel is the model of the attempt's newest worker stage (a retried
// worker stage may have moved to another model), else the attempt's own.
func (c *chain) workerModel() string {
	for i := len(c.stages) - 1; i >= 0; i-- {
		if c.stages[i].Role == "worker" {
			return c.stages[i].Model
		}
	}
	return c.att.WorkerModel
}

// workerOut is the parsed output of the attempt's newest worker stage.
func (c *chain) workerOut() (schema.WorkerOut, bool) {
	for i := len(c.stages) - 1; i >= 0; i-- {
		if c.stages[i].Role == "worker" && len(c.stages[i].Parsed) > 0 {
			w, err := schema.Parse[schema.WorkerOut](c.stages[i].Parsed)
			return w, err == nil
		}
	}
	return schema.WorkerOut{}, false
}

// FailureKind says why an attempt's chain stopped short of a pass.
type FailureKind string

const (
	FailVerify    FailureKind = "verify"           // structural or feedback verify failed (V6)
	FailReview    FailureKind = "review_repair"    // the reviewer asked for a repair
	FailBlocked   FailureKind = "blocked"          // the worker or reviewer needs a human
	FailTimeout   FailureKind = "worker_timeout"   // the worker ran out of time (AT7)
	FailViolation FailureKind = "policy_violation" // a model stage broke the git policy
	FailMerge     FailureKind = "merge"            // merge conflict or post-merge verify failed (PX3)
	FailCommit    FailureKind = "commit"           // the worktree could not be committed
)

// Failure is a non-passing end of an attempt's chain.
type Failure struct {
	Kind  FailureKind
	Stage model.Stage
}

// Advance moves every open attempt of every active effort one step along the
// chain: work, verify, review, commit, merge. It is driven by stage
// completion, the reconcile tick and Start, and is idempotent: a step is
// taken only when the attempt's newest stage is done and its successor does
// not exist yet, so running it twice, or after a restart, does nothing extra.
func (e *Engine) Advance(ctx context.Context) error {
	if e.deps.Git == nil {
		return nil
	}
	e.advMu.Lock()
	defer e.advMu.Unlock()
	s := e.deps.Store
	projects, err := s.ListProjects(ctx)
	if err != nil {
		return err
	}
	var errs []error
	changed := false
	for _, p := range projects {
		ef, err := s.ActiveEffort(ctx, p.Slug)
		if errors.Is(err, store.ErrNotFound) {
			continue
		}
		if err != nil {
			errs = append(errs, err)
			continue
		}
		tasks, err := s.ListTasks(ctx, ef.ID)
		if errors.Is(err, store.ErrNotFound) {
			continue
		}
		if err != nil {
			errs = append(errs, err)
			continue
		}
		for _, t := range tasks {
			atts, err := s.ListAttempts(ctx, t.ID)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			for _, a := range atts {
				if a.Outcome != "" {
					continue
				}
				c := &chain{proj: p, eff: ef, task: t, att: a}
				did, err := e.advanceAttempt(ctx, c)
				changed = changed || did
				if err != nil {
					errs = append(errs, fmt.Errorf("task %s attempt %d: %w", t.Key, a.N, err))
				}
			}
		}
		if err := e.completeEffort(ctx, p, ef); err != nil {
			errs = append(errs, fmt.Errorf("effort %d: %w", ef.ID, err))
		}
	}
	if changed && e.deps.Kick != nil {
		e.deps.Kick()
	}
	return errors.Join(errs...)
}

// advanceAttempt takes the next step of one open attempt. It reports whether
// it changed anything.
func (e *Engine) advanceAttempt(ctx context.Context, c *chain) (bool, error) {
	stages, err := e.deps.Store.ListAttemptStages(ctx, c.att.ID)
	if err != nil || len(stages) == 0 {
		return false, err
	}
	for _, st := range stages {
		if st.State != model.StageDone {
			return false, nil // in flight
		}
	}
	c.stages = stages
	last := stages[len(stages)-1]
	if last.Outcome == OutcomeDiscarded {
		return true, e.finishAttempt(ctx, c, model.OutcomeAbandoned)
	}
	if IsInfra(last.Outcome) {
		handled, err := e.handleInfra(ctx, c, last)
		return handled, err
	}
	if last.Outcome != OutcomePass {
		return true, e.fail(ctx, c, failureOf(last))
	}
	switch last.Role {
	case "worker":
		return true, e.submitVerify(ctx, c, last)
	case "verify":
		return e.submitReview(ctx, c, last)
	case "reviewer":
		return e.commitAndMerge(ctx, c, last)
	case "merge":
		return true, e.completeAttempt(ctx, c)
	}
	return false, fmt.Errorf("stage %d: unexpected role %q in an attempt", last.ID, last.Role)
}

// failureOf maps a closed stage that did not pass to the chain failure.
func failureOf(st model.Stage) Failure {
	f := Failure{Stage: st}
	switch {
	case st.Outcome == OutcomeBlocked:
		f.Kind = FailBlocked
	case st.Outcome == OutcomeTimeout:
		f.Kind = FailTimeout
	case st.Outcome == OutcomePolicyViolation:
		f.Kind = FailViolation
	case st.Role == "verify":
		f.Kind = FailVerify
	case st.Role == "merge":
		f.Kind = FailMerge
	default:
		f.Kind = FailReview
	}
	return f
}

// fail ends the chain at f: the attempt is closed by the failure policy
// (failure.go) and no later stage is called.
func (e *Engine) fail(ctx context.Context, c *chain, f Failure) error {
	return e.failAttempt(ctx, c, f)
}

// finishAttempt closes the attempt, tolerating one that is already closed.
func (e *Engine) finishAttempt(ctx context.Context, c *chain, outcome string) error {
	err := e.deps.Store.FinishAttempt(ctx, c.att.ID, outcome, "", c.reviewerModel())
	if errors.Is(err, store.ErrConflict) {
		return nil
	}
	return err
}

// submit queues a chain stage on a lane.
func (e *Engine) submit(ctx context.Context, c *chain, lane, role, backend, mdl, hash string) error {
	_, _, err := e.Submit(ctx, lane,
		store.StageSpec{Attempt: c.att.ID, Role: role, Backend: backend, Model: mdl, QueueHash: hash},
		c.payload(hash))
	return err
}

func (e *Engine) submitVerify(ctx context.Context, c *chain, last model.Stage) error {
	return e.submit(ctx, c, VerifyLane, "verify", verifyBackend, "", last.QueueHash)
}

// submitReview picks the reviewer now that the work stage has closed (MB6,
// AT3) and submits the review. Only a green verify gets here (V6). When no
// reviewer is eligible the reason is on the task and the next Advance tries
// again.
func (e *Engine) submitReview(ctx context.Context, c *chain, last model.Stage) (bool, error) {
	if !verifyPassed(last) {
		return true, e.fail(ctx, c, Failure{Kind: FailVerify, Stage: last})
	}
	pk, reason, err := e.pickBackend(ctx, "reviewer", c.task, c.proj, c.workerModel())
	if err != nil || reason != "" {
		return false, err
	}
	if err := e.submit(ctx, c, BackendLane(pk.Backend), "reviewer", pk.Backend, pk.Model, last.QueueHash); err != nil {
		return false, err
	}
	if c.task.WaitingReason != "" {
		return true, e.deps.Store.SetWaitingReason(ctx, c.task.ID, "")
	}
	return true, nil
}

// verifyPassed reads the verify stage's report: a pass outcome is not enough,
// the report itself must be green.
func verifyPassed(st model.Stage) bool {
	var r struct {
		Pass bool `json:"pass"`
	}
	return st.Outcome == OutcomePass && json.Unmarshal(st.Parsed, &r) == nil && r.Pass
}

// commitAndMerge commits the reviewed worktree as the engine (GIT3) and queues
// the serialized merge (PX3).
func (e *Engine) commitAndMerge(ctx context.Context, c *chain, last model.Stage) (bool, error) {
	msg := c.task.Key + ": " + c.task.Title
	if w, ok := c.workerOut(); ok && strings.TrimSpace(w.Summary) != "" {
		msg += "\n\n" + strings.TrimSpace(w.Summary)
	}
	_, err := e.deps.Git.CommitAll(ctx, c.proj.RepoPath, c.att.Worktree, msg,
		gitsvc.Trailers{Task: taskName(c.task), Attempt: c.att.N})
	switch {
	case errors.Is(err, gitsvc.ErrNothingToCommit):
		// Committed by an earlier pass that died before queueing the merge.
	case errors.Is(err, gitsvc.ErrGitlink):
		return true, e.fail(ctx, c, Failure{Kind: FailCommit, Stage: last})
	case err != nil:
		return false, err
	}
	return true, e.submit(ctx, c, MergeLane, "merge", mergeBackend, "", last.QueueHash)
}

// policedStage runs a model stage of an attempt. A worker first gets its
// worktree (and the effort's integration branch and baseline on first use).
// The worktree's git state is snapshotted around the runner call and any
// change the model made through git is reverted and recorded (GIT5 layer 2).
func (e *Engine) policedStage(ctx context.Context, rc golane.RunContext, st model.Stage, p StagePayload) StageResult {
	fail := func(err error) StageResult {
		return StageResult{Outcome: OutcomeInfraCrash, Raw: err.Error(), cause: err}
	}
	g, s := e.deps.Git, e.deps.Store
	proj, err := s.GetProject(ctx, p.Project)
	if err != nil {
		return fail(err)
	}
	att, err := s.GetAttempt(ctx, st.Attempt)
	if err != nil {
		return fail(err)
	}
	if st.Role == "worker" {
		if err := e.prepareAttempt(ctx, proj, att, p); err != nil {
			return fail(fmt.Errorf("prepare worktree: %w", err))
		}
	}
	snap, err := g.Snapshot(ctx, proj.RepoPath, att.Worktree)
	if err != nil {
		return fail(err)
	}
	res := e.modelStage(ctx, rc, st, p)
	viols, err := g.DetectAndRevert(context.WithoutCancel(ctx), snap)
	if err != nil {
		return fail(fmt.Errorf("revert policy violation: %w", err))
	}
	if len(viols) > 0 {
		if err := e.auditViolations(context.WithoutCancel(ctx), st, viols); err != nil {
			return fail(fmt.Errorf("audit policy violation: %w", err))
		}
	}
	if len(viols) > 0 && !IsInfra(res.Outcome) {
		var b strings.Builder
		b.WriteString(res.Raw + "\n\npolicy violation (reverted):")
		for _, v := range viols {
			fmt.Fprintf(&b, "\n- %s: %s", v.Kind, v.Detail)
		}
		fmt.Fprintln(rc.Log(), b.String())
		res.Outcome, res.Raw = OutcomePolicyViolation, b.String()
	}
	return res
}

// prepareAttempt gets the attempt's worktree ready: the effort's integration
// branch and worktree, the baseline (recorded once, before any attempt has
// changed anything), and the task worktree forked from the integration
// commit. It is idempotent, so a retried work stage reuses what exists.
func (e *Engine) prepareAttempt(ctx context.Context, proj store.Project, att model.Attempt, p StagePayload) error {
	e.prepMu.Lock()
	defer e.prepMu.Unlock()
	g := e.deps.Git
	task, err := e.deps.Store.GetTask(ctx, att.Task)
	if err != nil {
		return err
	}
	_, intWT, err := e.ensureIntegration(ctx, proj, p.Effort)
	if err != nil {
		return err
	}
	if err := e.ensureBaseline(ctx, proj, p.Effort, intWT); err != nil {
		return err
	}
	name, tname := effortName(p.Effort), taskName(task)
	if _, err := g.ReuseWorktree(ctx, proj.RepoPath, proj.Slug, name, tname); err == nil {
		return nil
	}
	head, err := g.Head(ctx, intWT)
	if err != nil {
		return err
	}
	wt, err := g.AddTaskWorktree(ctx, proj.RepoPath, proj.Slug, name, tname, head.Commit)
	if err != nil {
		return err
	}
	if wt.Path != att.Worktree {
		return fmt.Errorf("worktree %s does not match the attempt's %s", wt.Path, att.Worktree)
	}
	f := e.baseCommitFile(task.ID)
	if err := os.MkdirAll(filepath.Dir(f), 0o700); err != nil {
		return err
	}
	return os.WriteFile(f, []byte(head.Commit), 0o600)
}

// ensureIntegration creates the effort's integration branch at the project's
// current HEAD on first use (the user's branch and tree are never touched,
// GIT2) and checks it out in the effort's integration worktree. It returns the
// branch and the worktree path.
func (e *Engine) ensureIntegration(ctx context.Context, proj store.Project, effort int64) (string, string, error) {
	g := e.deps.Git
	ef, err := e.deps.Store.GetEffort(ctx, effort)
	if err != nil {
		return "", "", err
	}
	name := effortName(effort)
	override := ""
	if ef.IntegrationBranch != "" && ef.IntegrationBranch != gitsvc.IntegrationBranch(name, "") {
		override = ef.IntegrationBranch
	}
	base := ef.BaseCommit
	if base == "" {
		h, err := g.Head(ctx, proj.RepoPath)
		if err != nil {
			return "", "", err
		}
		base = h.Commit
	}
	branch, err := g.EnsureIntegrationBranch(ctx, proj.RepoPath, name, base, override)
	if err != nil {
		return "", "", err
	}
	if ef.IntegrationBranch != branch || ef.BaseCommit != base {
		if _, err := e.deps.Store.UpdateEffort(ctx, SystemActor, effort, store.EffortPatch{IntegrationBranch: &branch, BaseCommit: &base}); err != nil {
			return "", "", err
		}
	}
	wt, err := g.AddIntegrationWorktree(ctx, proj.RepoPath, proj.Slug, name, branch)
	if err != nil {
		return "", "", err
	}
	return branch, wt.Path, nil
}

// PushInstructions returns the commands a human runs to merge and push a
// completed effort's integration branch (GIT4). Agentflow runs none of them.
func (e *Engine) PushInstructions(ctx context.Context, effort int64) ([]string, error) {
	ef, err := e.deps.Store.GetEffort(ctx, effort)
	if err != nil {
		return nil, err
	}
	if ef.Status != model.EffortComplete {
		return nil, fmt.Errorf("effort %d is %s, not complete: %w", effort, ef.Status, store.ErrConflict)
	}
	proj, err := e.deps.Store.GetProject(ctx, ef.Project)
	if err != nil {
		return nil, err
	}
	baseBranch := "main"
	if h, err := e.deps.Git.Head(ctx, proj.RepoPath); err == nil && h.Branch != "" {
		baseBranch = h.Branch
	}
	return gitsvc.PushInstructions(proj.RepoPath, ef.IntegrationBranch, baseBranch), nil
}

// touchedFiles are the files the attempt may be blamed for: the task's
// declared files and what the worker reported changing.
func (c *chain) touchedFiles() []string {
	out := slices.Clone(c.task.FilesTouched)
	if w, ok := c.workerOut(); ok {
		for _, f := range w.FilesChanged {
			if !slices.Contains(out, f) {
				out = append(out, f)
			}
		}
	}
	return out
}
