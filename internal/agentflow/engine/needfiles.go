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

// autoAllowFiles reports whether the project has the AF-17 auto-allow-files rule
// enabled.
func (e *Engine) autoAllowFiles(ctx context.Context, proj string) bool {
	set, err := e.deps.Store.EffectiveSettings(ctx, proj)
	if err != nil {
		return false
	}
	return set.AutoAllowFiles
}

// autoRetry reports whether the project has the AF-17 auto-retry rule enabled.
func (e *Engine) autoRetry(ctx context.Context, proj string) bool {
	set, err := e.deps.Store.EffectiveSettings(ctx, proj)
	if err != nil {
		return true
	}
	return set.AutoRetry
}

// autoReplan reports whether the project has the AF-17 auto-replan rule enabled.
func (e *Engine) autoReplan(ctx context.Context, proj string) bool {
	set, err := e.deps.Store.EffectiveSettings(ctx, proj)
	if err != nil {
		return false
	}
	return set.AutoReplan
}

// contradictionQuote returns the quoted FRD constraint the blocked reason
// references, or "" when none.
func contradictionQuote(reason string, f model.FRD) string {
	// find the first quoted substring in the reason
	start := strings.IndexByte(reason, '"')
	if start < 0 {
		return ""
	}
	end := strings.IndexByte(reason[start+1:], '"')
	if end < 0 {
		return ""
	}
	q := strings.TrimSpace(reason[start+1 : start+1+end])
	if q == "" {
		return ""
	}
	ql := strings.ToLower(q)
	for _, c := range f.Constraints {
		if strings.Contains(strings.ToLower(c), ql) {
			return q
		}
	}
	return ""
}

// safeNeededFiles reports whether every requested file may be allowed without a
// human, per AF-17: inside the FRD's MAY-touch list, a test file beside code
// the task already edits, or a docs/ gaps table the acceptance names. A path in
// a MUST-NOT list makes the whole request unsafe.
func (e *Engine) safeNeededFiles(ctx context.Context, effort int64, t model.Task, nf []schema.NeededFile) bool {
	f, _, err := e.effortFRD(ctx, effort)
	if err != nil {
		return false
	}
	acceptsGaps := false
	for _, a := range t.Acceptance {
		if strings.Contains(strings.ToLower(a), "gap") {
			acceptsGaps = true
		}
	}
	for _, n := range nf {
		p := n.Path
		if !safeNeededPath(f, t, p, acceptsGaps) {
			return false
		}
	}
	return len(nf) > 0
}

func safeNeededPath(f model.FRD, t model.Task, p string, acceptsGaps bool) bool {
	pl := strings.ToLower(p)
	// MUST NOT touch: any FRD constraint or out-of-scope entry naming the path.
	for _, c := range f.Constraints {
		cl := strings.ToLower(c)
		if strings.Contains(cl, pl) && (strings.Contains(cl, "must not") || strings.Contains(cl, "mustn't")) {
			return false
		}
	}
	for _, o := range f.OutOfScope {
		if strings.Contains(strings.ToLower(o), pl) {
			return false
		}
	}
	// test file beside code the task already edits
	if strings.HasSuffix(p, "_test.go") {
		dir := path.Dir(p)
		base := strings.TrimSuffix(path.Base(p), "_test.go")
		for _, f0 := range t.FilesTouched {
			if path.Dir(f0) == dir && strings.TrimSuffix(path.Base(f0), ".go") == base {
				return true
			}
		}
		// any test file next to an edited file in same dir
		for _, f0 := range t.FilesTouched {
			if path.Dir(f0) == dir {
				return true
			}
		}
	}
	// docs/ gaps table
	if strings.HasPrefix(p, "docs/") && acceptsGaps {
		return true
	}
	// MAY touch list in FRD constraints
	for _, c := range f.Constraints {
		cl := strings.ToLower(c)
		if strings.Contains(cl, "may touch") || strings.Contains(cl, "may edit") || strings.Contains(cl, "may change") {
			if strings.Contains(cl, pl) {
				return true
			}
		}
	}
	// any constraint mentioning the path without must-not
	for _, c := range f.Constraints {
		cl := strings.ToLower(c)
		if strings.Contains(cl, pl) && !strings.Contains(cl, "must not") {
			return true
		}
	}
	// If FRD has no constraints at all, be conservative: stop for human.
	return false
}
