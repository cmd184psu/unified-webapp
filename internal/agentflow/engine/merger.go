package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"cmd184psu/unified-webapp/internal/agentflow/gitsvc"
	"cmd184psu/unified-webapp/internal/agentflow/model"
	"cmd184psu/unified-webapp/internal/agentflow/prompts"
	"cmd184psu/unified-webapp/internal/agentflow/schema"
	"cmd184psu/unified-webapp/internal/agentflow/store"
)

// The merger (AF-18). A task branch can stop merging cleanly into the effort's
// integration branch. The deterministic merge leaves the conflicted files in
// the task worktree with markers and the engine gives them to a merger stage:
// a strong model that keeps both sides' behaviour, instead of sending the
// conflict back to the worker or to the coordinator. Its output is checked for
// markers and for dropped incoming changes, committed, and the merge is tried
// again. Two merger attempts, then the task fails as before.

const (
	mergerRef      = "merger:"
	mergeReviewRef = "merge-review:"
)

// mergerTryLimit is how many merger models may try one conflicted merge.
const mergerTryLimit = 2

func isMerger(st model.Stage) bool { return st.Role == "merger" }

// mergerOn reports whether the project lists a merger candidate.
func (e *Engine) mergerOn(ctx context.Context, p store.Project) bool {
	v, err := e.newView(ctx)
	return err == nil && len(v.candidates("merger", p.Slug)) > 0
}

// mergerPick chooses the next merger model, skipping the ones this attempt
// already tried.
func (e *Engine) mergerPick(ctx context.Context, p store.Project, tried []string) (Pick, string, bool, error) {
	pk, _, reason, err := e.pickEffortStage(ctx, p, "merger", nil, strings.Join(tried, "\x01"))
	if err != nil || reason != "" {
		return Pick{}, reason, false, err
	}
	return pk, "", true, nil
}

// startMerger queues a merger stage for a conflicted merge. started is false
// with an empty reason when the project cannot merge at all; a non-empty
// reason means a candidate exists but none is eligible yet, so the attempt
// waits. A chain without a real effort never starts a merger: it skips and
// records the skip once, so a broken chain cannot flood the log by retrying.
func (e *Engine) startMerger(ctx context.Context, c *chain, last model.Stage) (started, waiting bool, err error) {
	if c.eff.ID == 0 {
		if nerr := e.noteMergerSkipOnce(ctx, c, "no effort for merger"); nerr != nil {
			return false, false, nerr
		}
		return false, false, nil
	}
	var tried []string
	for _, st := range c.stages {
		if isMerger(st) {
			tried = append(tried, avoidKey(st.Backend, st.Model))
		}
	}
	if len(tried) >= mergerTryLimit {
		return false, false, nil
	}
	if !e.mergerOn(ctx, c.proj) {
		return false, false, nil
	}
	pk, reason, ok, err := e.mergerPick(ctx, c.proj, tried)
	if err != nil {
		return false, false, err
	}
	if !ok {
		if reason != "" {
			return false, true, e.deps.Store.SetWaitingReason(ctx, c.task.ID, "waiting for a merger: "+reason)
		}
		return false, false, nil
	}
	if err := e.noteEffort(ctx, SystemActor, "merge.merger", c.eff.ID, map[string]any{
		"task": c.task.Key, "attempt": c.att.N, "backend": pk.Backend, "model": pk.Model,
		"files": mergeConflictFiles(c), "verdict": "started",
	}); err != nil {
		return false, false, err
	}
	_, _, err = e.Submit(ctx, BackendLane(pk.Backend),
		store.StageSpec{Attempt: c.att.ID, Role: "merger", Backend: pk.Backend, Model: pk.Model, QueueHash: last.QueueHash, PromptRef: mergerRef + c.task.Key},
		c.payload(last.QueueHash))
	return true, false, err
}

// handleMergeConflict starts the merger for a conflicted merge. It reports
// whether the conflict was taken over, so the attempt does not fail as it
// would without a merger.
func (e *Engine) handleMergeConflict(ctx context.Context, c *chain, last model.Stage) (bool, error) {
	started, waiting, err := e.startMerger(ctx, c, last)
	if err != nil {
		return true, err
	}
	if started || waiting {
		return true, nil
	}
	return false, nil
}

// mergeConflictFiles are the files the newest merge stage reported conflicted.
func mergeConflictFiles(c *chain) []string {
	for i := len(c.stages) - 1; i >= 0; i-- {
		st := c.stages[i]
		if st.Role != "merge" {
			continue
		}
		var m mergeReport
		if json.Unmarshal(st.Parsed, &m) == nil && m.Kind == "conflict" {
			return m.Files
		}
	}
	return nil
}

// afterMerger judges a finished merger stage. On success it commits the merge
// resolution to the task branch and queues the merge again. A blocked merger,
// a leftover marker or an unreported dropped incoming change is one failed
// merger attempt; after mergerTryLimit the task fails as a merge failure.
func (e *Engine) afterMerger(ctx context.Context, c *chain, last model.Stage) (bool, error) {
	out, err := schema.Parse[schema.MergerOut](last.Parsed)
	if err != nil {
		return true, e.retryMerger(ctx, c, last, "the merger's reply could not be read: "+err.Error())
	}
	if out.Blocked != "" {
		return true, e.retryMerger(ctx, c, last, "the merger stopped: "+out.Blocked)
	}
	markers := conflictMarkers(c.att.Worktree, mergeConflictFiles(c))
	if len(markers) > 0 {
		return true, e.retryMerger(ctx, c, last, "conflict markers left in "+strings.Join(markers, ", "))
	}
	if missing := e.noLoss(ctx, c, out); len(missing) > 0 {
		return true, e.retryMerger(ctx, c, last, "incoming changes missing: "+strings.Join(missing, ", "))
	}
	if err := e.noteEffort(ctx, SystemActor, "merge.merger", c.eff.ID, map[string]any{
		"task": c.task.Key, "attempt": c.att.N, "backend": last.Backend, "model": last.Model,
		"files": out.Files, "verdict": "passed",
	}); err != nil {
		return false, err
	}
	return true, e.submitMergerReview(ctx, c, last)
}

// submitMergerReview queues a reviewer that reads the resolution diff before
// the merge is committed. Without an eligible reviewer the merge goes ahead,
// so a repository with no reviewer does not stall on one.
func (e *Engine) submitMergerReview(ctx context.Context, c *chain, last model.Stage) error {
	v, err := e.newView(ctx)
	if err != nil {
		return err
	}
	if len(v.candidates("reviewer", c.proj.Slug)) == 0 {
		return e.commitMergerResolution(ctx, c, last)
	}
	pk, _, reason, err := e.pickEffortStage(ctx, c.proj, "reviewer", nil, "")
	if err != nil {
		return err
	}
	if reason != "" {
		return e.deps.Store.SetWaitingReason(ctx, c.task.ID, "waiting for a reviewer of the merge: "+reason)
	}
	_, _, err = e.Submit(ctx, BackendLane(pk.Backend),
		store.StageSpec{Attempt: c.att.ID, Role: "reviewer", Backend: pk.Backend, Model: pk.Model, QueueHash: last.QueueHash, PromptRef: mergeReviewRef + c.task.Key},
		c.payload(last.QueueHash))
	return err
}

// commitMergerResolution commits the resolved merge on the task branch and
// queues the ordinary merge again, which now fast-forwards.
func (e *Engine) commitMergerResolution(ctx context.Context, c *chain, last model.Stage) error {
	msg := fmt.Sprintf("Resolve merge conflict for %s with both sides' behaviour (merger %s)", c.task.Key, last.Model)
	if _, err := e.deps.Git.CommitAll(ctx, c.proj.RepoPath, c.att.Worktree, msg,
		gitsvc.Trailers{Task: taskName(c.task), Attempt: c.att.N}); err != nil && !errors.Is(err, gitsvc.ErrNothingToCommit) {
		return err
	}
	return e.submitMerge(ctx, c, last)
}

func isMergeReview(st model.Stage) bool {
	return st.Role == "reviewer" && strings.HasPrefix(st.PromptRef, mergeReviewRef)
}

// afterMergeReview applies the merge reviewer's verdict. A pass commits the
// resolution; a repair hands the same conflict to the next merger with the
// reviewer's findings.
func (e *Engine) afterMergeReview(ctx context.Context, c *chain, last model.Stage) error {
	out, err := schema.Parse[schema.ReviewOut](last.Parsed)
	if err != nil {
		return e.retryMerger(ctx, c, last, "the merge review could not be read: "+err.Error())
	}
	if out.Verdict != "pass" {
		reason := out.Summary
		if len(out.Issues) > 0 {
			parts := make([]string, 0, len(out.Issues))
			for _, is := range out.Issues {
				parts = append(parts, is.Description)
			}
			reason = strings.Join(parts, "; ")
		}
		return e.retryMerger(ctx, c, last, "the merge reviewer asked for repair: "+reason)
	}
	if err := e.noteEffort(ctx, SystemActor, "merge.review", c.eff.ID, map[string]any{
		"task": c.task.Key, "attempt": c.att.N, "backend": last.Backend, "model": last.Model,
		"files": mergeConflictFiles(c), "verdict": "passed",
	}); err != nil {
		return err
	}
	return e.commitMergerResolution(ctx, c, last)
}

// retryMerger records a failed merger attempt and starts the next candidate,
// or fails the attempt when they are used up.
func (e *Engine) retryMerger(ctx context.Context, c *chain, last model.Stage, why string) error {
	_ = e.noteEffort(ctx, SystemActor, "merge.merger", c.eff.ID, map[string]any{
		"task": c.task.Key, "attempt": c.att.N, "backend": last.Backend, "model": last.Model,
		"files": mergeConflictFiles(c), "verdict": "failed", "reason": why,
	})
	n := 0
	for _, st := range c.stages {
		if isMerger(st) {
			n++
		}
	}
	if n >= mergerTryLimit {
		if err := e.deps.Store.SetWaitingReason(ctx, c.task.ID, "merge conflict needs a human: "+why); err != nil {
			return err
		}
		return e.fail(ctx, c, Failure{Kind: FailMerge, Stage: last})
	}
	started, waiting, err := e.startMerger(ctx, c, last)
	if err != nil {
		return err
	}
	if started || waiting {
		return nil
	}
	return e.fail(ctx, c, Failure{Kind: FailMerge, Stage: last})
}

// prepareMergerConflict puts the task worktree back on its branch tip and
// re-applies the conflicted merge, so every merger model starts from markers
// rather than from an earlier model's partial edit.
func (e *Engine) prepareMergerConflict(ctx context.Context, proj store.Project, att model.Attempt, effort int64) error {
	ef, err := e.deps.Store.GetEffort(ctx, effort)
	if err != nil {
		return err
	}
	if err := e.deps.Git.ResetTask(ctx, proj.RepoPath, att.Worktree, "HEAD"); err != nil {
		return err
	}
	_, err = e.deps.Git.PrepareConflictRepair(ctx, proj.RepoPath, att.Worktree, ef.IntegrationBranch)
	return err
}

// conflictMarkers lists the conflicted files still carrying merge markers.
func conflictMarkers(wt string, files []string) []string {
	var out []string
	for _, rel := range files {
		b, err := os.ReadFile(filepath.Join(wt, filepath.Clean(rel)))
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(line, "<<<<<<< ") || line == "=======" || strings.HasPrefix(line, ">>>>>>> ") {
				out = append(out, rel)
				break
			}
		}
	}
	return out
}

// noLoss checks that every line the incoming side added since the merge base
// is still in the merged file. A line missing from a file the merger's notes
// name is allowed (the merger reported the deliberate drop).
func (e *Engine) noLoss(ctx context.Context, c *chain, out schema.MergerOut) []string {
	taskBranch := gitsvc.TaskBranch(effortName(c.eff.ID), taskName(c.task))
	base, err := e.deps.Git.MergeBase(ctx, c.proj.RepoPath, c.eff.IntegrationBranch, taskBranch)
	if err != nil {
		return []string{"could not find the merge base: " + err.Error()}
	}
	diff, err := e.deps.Git.DiffRefs(ctx, c.proj.RepoPath, base, taskBranch, 1<<20)
	if err != nil {
		return []string{"could not read the incoming diff: " + err.Error()}
	}
	return incomingMissing(diff, func(file string) (string, error) {
		b, err := os.ReadFile(filepath.Join(c.att.Worktree, filepath.Clean(file)))
		return string(b), err
	}, out.Notes)
}

// incomingMissing is the no-loss check: every line the incoming side added is
// still in the result. Files named in the merger's notes are skipped because
// the drop was reported.
func incomingMissing(diff string, read func(string) (string, error), notes []string) []string {
	var missing []string
	file := ""
	lines := map[string]map[string]bool{}
	for _, line := range strings.Split(diff, "\n") {
		if strings.HasPrefix(line, "+++ b/") {
			file = strings.TrimPrefix(line, "+++ b/")
			continue
		}
		if !strings.HasPrefix(line, "+") || strings.HasPrefix(line, "+++") {
			continue
		}
		if file == "" || dropped(file, notes) {
			continue
		}
		want := strings.TrimSpace(strings.TrimPrefix(line, "+"))
		if want == "" {
			continue
		}
		body, ok := lines[file]
		if !ok {
			b, err := read(file)
			if err != nil {
				missing = appendOnce(missing, file)
				continue
			}
			body = map[string]bool{}
			for _, l := range strings.Split(b, "\n") {
				body[strings.TrimSpace(l)] = true
			}
			lines[file] = body
		}
		if !body[want] {
			missing = appendOnce(missing, file)
		}
	}
	return missing
}

func appendOnce(list []string, item string) []string {
	if slices.Contains(list, item) {
		return list
	}
	return append(list, item)
}

func dropped(file string, notes []string) bool {
	for _, n := range notes {
		if strings.Contains(n, file) {
			return true
		}
	}
	return false
}

// mergerEffort resolves the effort a merger stage belongs to. Merger stages
// are attempt stages, so st.Effort is always 0; the effort comes from the
// stage's attempt through its task's queue. Reading st.Effort here crashed
// every merger with "effort 0: not found".
func (e *Engine) mergerEffort(ctx context.Context, st model.Stage) (model.Effort, error) {
	if st.Effort != 0 {
		return e.deps.Store.GetEffort(ctx, st.Effort)
	}
	att, err := e.deps.Store.GetAttempt(ctx, st.Attempt)
	if err != nil {
		return model.Effort{}, err
	}
	task, err := e.deps.Store.GetTask(ctx, att.Task)
	if err != nil {
		return model.Effort{}, err
	}
	effortID, err := e.deps.Store.QueueEffort(ctx, task.Queue)
	if err != nil {
		return model.Effort{}, err
	}
	if effortID == 0 {
		return model.Effort{}, fmt.Errorf("merger stage %d: no effort", st.ID)
	}
	return e.deps.Store.GetEffort(ctx, effortID)
}

// noteMergerSkipOnce records why no merger started, the first time only.
func (e *Engine) noteMergerSkipOnce(ctx context.Context, c *chain, reason string) error {
	evs, err := e.effortEvents(ctx, c.eff.ID, "merge.merger.skip")
	if err != nil {
		return err
	}
	for _, ev := range evs {
		if strings.Contains(ev.DetailJSON, reason) {
			return nil
		}
	}
	return e.noteEffort(ctx, SystemActor, "merge.merger.skip", c.eff.ID, map[string]any{
		"task": c.task.Key, "attempt": c.att.N, "reason": reason, "verdict": "skipped",
	})
}

// mergerPrompt fills the merger prompt with the branches, conflict and logs.
func (e *Engine) mergerPrompt(ctx context.Context, project string, st model.Stage) (string, error) {
	text, err := prompts.Load("merger")
	if err != nil {
		return "", err
	}
	ef, err := e.mergerEffort(ctx, st)
	if err != nil {
		return "", err
	}
	key := strings.TrimPrefix(st.PromptRef, mergerRef)
	tasks, err := e.deps.Store.ListTasks(ctx, ef.ID)
	if err != nil {
		return "", err
	}
	var task model.Task
	for _, t := range tasks {
		if t.Key == key {
			task = t
		}
	}
	if task.ID == 0 {
		return "", fmt.Errorf("merger stage %d: task %s not found", st.ID, key)
	}
	proj, err := e.deps.Store.GetProject(ctx, project)
	if err != nil {
		return "", err
	}
	taskBranch := gitsvc.TaskBranch(effortName(ef.ID), taskName(task))
	tj, _ := json.Marshal(task.TaskDef)
	var files []string
	base, _ := e.deps.Git.MergeBase(ctx, proj.RepoPath, ef.IntegrationBranch, taskBranch)
	stages, _ := e.deps.Store.ListAttemptStages(ctx, st.Attempt)
	var prior strings.Builder
	for _, s := range stages {
		var m mergeReport
		if s.Role == "merge" && json.Unmarshal(s.Parsed, &m) == nil && m.Kind == "conflict" {
			files = m.Files
		}
		if isMerger(s) {
			fmt.Fprintf(&prior, "- %s: %s\n", s.Model, s.RawOutput)
		}
	}
	incoming, _ := e.deps.Git.LogRange(ctx, proj.RepoPath, base, taskBranch, 16<<10)
	baseLog, _ := e.deps.Git.LogRange(ctx, proj.RepoPath, base, ef.IntegrationBranch, 16<<10)
	rep := strings.NewReplacer(
		"{{TASK_JSON}}", string(tj),
		"{{FRD_SECTION}}", e.frdSection(ctx, task),
		"{{TASK_BRANCH}}", taskBranch,
		"{{BASE_BRANCH}}", ef.IntegrationBranch,
		"{{CONFLICT_FILES}}", bullet(files),
		"{{INCOMING_LOG}}", logOrNone(incoming),
		"{{BASE_LOG}}", logOrNone(baseLog),
		"{{PRIOR_ATTEMPTS}}", logOrNone(prior.String()),
	)
	return rep.Replace(text), nil
}

// mergerReviewPrompt fills the reviewer prompt with the resolution diff and
// the merger's account of it.
func (e *Engine) mergerReviewPrompt(ctx context.Context, project string, st model.Stage) (string, error) {
	text, err := prompts.Load("merger-review")
	if err != nil {
		return "", err
	}
	ef, err := e.mergerEffort(ctx, st)
	if err != nil {
		return "", err
	}
	att, err := e.deps.Store.GetAttempt(ctx, st.Attempt)
	if err != nil {
		return "", err
	}
	task, err := e.deps.Store.GetTask(ctx, att.Task)
	if err != nil {
		return "", err
	}
	taskBranch := gitsvc.TaskBranch(effortName(ef.ID), taskName(task))
	tj, _ := json.Marshal(task.TaskDef)
	var files []string
	mergerOut := "(no merger reply)"
	stages, _ := e.deps.Store.ListAttemptStages(ctx, att.ID)
	for _, s := range stages {
		var m mergeReport
		if s.Role == "merge" && json.Unmarshal(s.Parsed, &m) == nil && m.Kind == "conflict" {
			files = m.Files
		}
		if isMerger(s) && len(s.Parsed) > 0 {
			mergerOut = string(s.Parsed)
		}
	}
	proj, err := e.deps.Store.GetProject(ctx, project)
	if err != nil {
		return "", err
	}
	diff, _ := e.deps.Git.WorktreeDiff(ctx, proj.RepoPath, att.Worktree, 1<<20)
	rep := strings.NewReplacer(
		"{{TASK_JSON}}", string(tj),
		"{{FRD_SECTION}}", e.frdSection(ctx, task),
		"{{TASK_BRANCH}}", taskBranch,
		"{{BASE_BRANCH}}", ef.IntegrationBranch,
		"{{CONFLICT_FILES}}", bullet(files),
		"{{MERGER_OUT}}", mergerOut,
		"{{RESOLUTION_DIFF}}", diffOrNone(diff),
	)
	return rep.Replace(text), nil
}

func diffOrNone(s string) string {
	if strings.TrimSpace(s) == "" {
		return "(no diff: the resolution matches the task branch)"
	}
	return s
}

func bullet(items []string) string {
	if len(items) == 0 {
		return "- (none reported)"
	}
	var b strings.Builder
	for _, it := range items {
		b.WriteString("- " + it + "\n")
	}
	return b.String()
}

func logOrNone(s string) string {
	if strings.TrimSpace(s) == "" {
		return "(no commits)"
	}
	return "```\n" + s + "\n```"
}
