package engine

import (
	"context"
	"fmt"
	"path"
	"strings"

	"cmd184psu/unified-webapp/internal/agentflow/model"
	"cmd184psu/unified-webapp/internal/agentflow/schema"
	"cmd184psu/unified-webapp/internal/agentflow/store"
)

// AF-13 item 3 (fs3-engine/10, T005 and T010): porting a phase needed new fields in
// src/game/state.go or the save loader, outside the task's files_touched; each task exhausted
// and reviewers failed it on scope. A worker may now say so: its result carries needs_files, the
// task stops as needs_input showing the files and reasons, and the owner's "allow" adds them to
// files_touched and retries in the same worktree.

// neededFiles are the files st's worker asked for that the task does not list yet.
func neededFiles(st model.Stage, t model.Task) []schema.NeededFile {
	if st.Role != "worker" {
		return nil
	}
	w, err := schema.Parse[schema.WorkerOut](st.Parsed)
	if err != nil {
		return nil
	}
	listed := map[string]bool{}
	for _, f := range t.FilesTouched {
		listed[path.Clean(f)] = true
	}
	var out []schema.NeededFile
	for _, f := range w.NeedsFiles {
		f.Path = path.Clean(strings.TrimPrefix(strings.TrimSpace(f.Path), "./"))
		if !listed[f.Path] {
			listed[f.Path] = true
			out = append(out, f)
		}
	}
	return out
}

func neededPaths(nf []schema.NeededFile) []string {
	out := make([]string, len(nf))
	for i, f := range nf {
		out[i] = f.Path
	}
	return out
}

// needsFilesReason is the waiting reason of a task stopped for files.
func needsFilesReason(nf []schema.NeededFile) string {
	parts := make([]string, len(nf))
	for i, f := range nf {
		parts[i] = f.Path + " (" + strings.TrimSpace(f.Reason) + ")"
	}
	return "needs files outside its list: " + strings.Join(parts, "; ") + ". Allow them to retry in the same worktree."
}

// PendingNeededFiles is what a task stopped for files is waiting to be allowed: the needs_files
// of its newest attempt's worker, when that attempt ended blocked. Empty otherwise.
func (e *Engine) PendingNeededFiles(ctx context.Context, t model.Task) ([]schema.NeededFile, error) {
	atts, err := e.deps.Store.ListAttempts(ctx, t.ID)
	if err != nil || len(atts) == 0 {
		return nil, err
	}
	last := atts[len(atts)-1]
	if last.Outcome != model.OutcomeBlocked {
		return nil, nil
	}
	stages, err := e.deps.Store.ListAttemptStages(ctx, last.ID)
	if err != nil {
		return nil, err
	}
	for i := len(stages) - 1; i >= 0; i-- {
		if stages[i].Role == "worker" {
			return neededFiles(stages[i], t), nil
		}
	}
	return nil, nil
}

// AllowFiles adds files a stopped task asked for to its files_touched and retries it, keeping
// its worktree. files empty means every file it asked for; a file it did not ask for is refused.
func (e *Engine) AllowFiles(ctx context.Context, actor string, task int64, files []string) ([]string, error) {
	_, _, t, err := e.locateTask(ctx, task)
	if err != nil {
		return nil, err
	}
	pending, err := e.PendingNeededFiles(ctx, t)
	if err != nil {
		return nil, err
	}
	if len(pending) == 0 {
		return nil, fmt.Errorf("task %s is not waiting for files to be allowed: %w", t.Key, store.ErrConflict)
	}
	asked := map[string]bool{}
	for _, f := range pending {
		asked[f.Path] = true
	}
	var allow []string
	if len(files) == 0 {
		for _, f := range pending {
			allow = append(allow, f.Path)
		}
	}
	for _, f := range files {
		f = path.Clean(strings.TrimPrefix(strings.TrimSpace(f), "./"))
		if !asked[f] {
			return nil, store.Invalid("files", fmt.Sprintf("task %s did not ask for %s", t.Key, f))
		}
		allow = append(allow, f)
	}
	if err := e.deps.Store.AllowTaskFiles(ctx, actor, task, allow); err != nil {
		return nil, err
	}
	if atts, err := e.deps.Store.ListAttempts(ctx, task); err == nil && len(atts) > 0 {
		note := "The owner allowed these files, now in this task's files_touched: " + strings.Join(allow, ", ") + ". Finish the task in the same worktree; you may change them."
		if err := e.deps.Store.AppendProgress(ctx, task, atts[len(atts)-1].ID, note); err != nil {
			return nil, err
		}
	}
	return allow, e.Retry(ctx, actor, task, false)
}
