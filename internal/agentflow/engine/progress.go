package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"cmd184psu/unified-webapp/internal/agentflow/model"
	"cmd184psu/unified-webapp/internal/agentflow/schema"
)

const (
	// recentProgress is how many progress-log entries a worker prompt carries.
	recentProgress = 5
	// maxRepairDiff bounds the previous attempt's diff in a repair prompt.
	maxRepairDiff = 200 << 10
	// maxEntryDetail bounds the failure detail in a progress entry.
	maxEntryDetail = 600
)

// clip shortens s to at most n bytes for display.
func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return strings.ToValidUTF8(s[:n], "") + "..."
}

// appendProgress adds the attempt's entry to the task's progress log (AT10):
// what the worker said it changed, the files, its learnings and how the
// attempt ended. result is "passed" or the failure that closed the attempt.
func (e *Engine) appendProgress(ctx context.Context, c *chain, result string) error {
	var b strings.Builder
	fmt.Fprintf(&b, "Attempt %d (%s) %s.", c.att.N, c.att.Kind, result)
	if w, ok := c.workerOut(); ok {
		fmt.Fprintf(&b, " %s", strings.TrimSpace(w.Summary))
		if len(w.FilesChanged) > 0 {
			fmt.Fprintf(&b, "\nFiles: %s", strings.Join(w.FilesChanged, ", "))
		}
		for _, l := range w.Learnings {
			fmt.Fprintf(&b, "\nLearned: %s", strings.TrimSpace(l))
		}
	}
	return e.deps.Store.AppendProgress(ctx, c.task.ID, c.att.ID, b.String())
}

// blockedReason is what a blocked worker or reviewer asked a human for.
func blockedReason(st model.Stage) string {
	switch st.Role {
	case "worker", "cleanup":
		if w, err := schema.Parse[schema.WorkerOut](st.Parsed); err == nil && w.Blocked != "" {
			return w.Blocked
		}
	case "reviewer":
		if r, err := schema.Parse[schema.ReviewOut](st.Parsed); err == nil && r.Blocked != "" {
			return r.Blocked
		}
	}
	return "blocked: " + clip(st.RawOutput, maxEntryDetail)
}

// WorkerContext is the part of a worker prompt that carries the task's history:
// the last progress-log entries (every attempt is a fresh runner session, so
// this is its only memory, AT10) and, for a repair attempt, what the previous
// attempt left behind (AT2): the VerifyReport, the reviewer's issues and repair
// prompt, and the previous diff against the attempt base. A custom
// Deps.Prompt should append it to worker prompts.
func (e *Engine) WorkerContext(ctx context.Context, project string, attemptID int64) (string, error) {
	s := e.deps.Store
	att, err := s.GetAttempt(ctx, attemptID)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	entries, err := s.ListProgress(ctx, att.Task, recentProgress)
	if err != nil {
		return "", err
	}
	if len(entries) > 0 {
		b.WriteString("\n## Progress log (earlier attempts on this task)\n")
		for _, en := range entries {
			b.WriteString("\n- " + strings.ReplaceAll(en, "\n", "\n  ") + "\n")
		}
	}
	if att.Kind != model.AttemptRepair {
		return b.String(), nil
	}
	atts, err := s.ListAttempts(ctx, att.Task)
	if err != nil {
		return "", err
	}
	var prev *model.Attempt
	for i := range atts {
		if a := atts[i]; a.N < att.N && a.Outcome != "" && a.Outcome != model.OutcomeAbandoned {
			prev = &atts[i]
		}
	}
	if prev == nil {
		return b.String(), nil
	}
	stages, err := s.ListAttemptStages(ctx, prev.ID)
	if err != nil {
		return "", err
	}
	fmt.Fprintf(&b, "\n## Repair attempt\n\nThe previous attempt (%d) ended %s. Your worktree still holds its changes: fix what is wrong, do not start over.\n", prev.N, prev.Outcome)
	for i := len(stages) - 1; i >= 0; i-- {
		st := stages[i]
		switch {
		case st.Role == "verify" && len(st.Parsed) > 0:
			b.WriteString("\n### Verify report\n\n```json\n" + string(st.Parsed) + "\n```\n")
		case st.Role == "merge" && len(st.Parsed) > 0 && st.Outcome != OutcomePass:
			b.WriteString("\n### Merge result (the integration branch was left unchanged)\n\n```json\n" + string(st.Parsed) + "\n```\n")
			var m mergeReport
			if json.Unmarshal(st.Parsed, &m) == nil && m.Kind == "conflict" {
				b.WriteString("\nThe integration branch has been merged into your worktree and the files listed above contain conflict markers. Resolve them, keeping the intent of both sides.\n")
			}
		case st.Role == "reviewer" && st.Outcome == OutcomeRepair:
			if r, err := schema.Parse[schema.ReviewOut](st.Parsed); err == nil {
				writeReview(&b, r)
			}
		case st.Role == "worker" && st.Outcome == OutcomeBlocked && prev.Fingerprint == string(FailBlocked):
			b.WriteString("\n### The previous worker said it was blocked\n\n" + clip(st.RawOutput, maxEntryDetail) +
				"\n\nThat may be a mistake: a model can name a file that does not exist or misread the task. Check the files the task names yourself and do the work. Report blocked only if you truly cannot go on.\n")
		case st.Role == "worker" && (st.Outcome == OutcomePolicyViolation || st.Outcome == OutcomeTimeout):
			b.WriteString("\n### Previous worker run\n\n" + clip(st.RawOutput, maxEntryDetail) + "\n")
		}
	}
	if err := e.writeDiff(ctx, &b, project, att, prev.N); err != nil {
		return "", err
	}
	return b.String(), nil
}

func writeReview(b *strings.Builder, r schema.ReviewOut) {
	b.WriteString("\n### Reviewer issues\n\n")
	for _, is := range r.Issues {
		loc := is.File
		if is.File != "" && is.Line > 0 {
			loc = fmt.Sprintf("%s:%d", is.File, is.Line)
		}
		fmt.Fprintf(b, "- [%s] %s %s\n", is.Severity, loc, is.Description)
	}
	b.WriteString("\n### Reviewer repair instructions\n\n" + r.RepairPrompt + "\n")
}

// writeDiff appends the previous attempt's changes against the commit the task
// worktree was forked from.
func (e *Engine) writeDiff(ctx context.Context, b *strings.Builder, project string, att model.Attempt, prevN int) error {
	if e.deps.Git == nil || att.Worktree == "" {
		return nil
	}
	base, err := os.ReadFile(e.baseCommitFile(att.Task))
	if err != nil {
		return fmt.Errorf("task base commit: %w", err)
	}
	proj, err := e.deps.Store.GetProject(ctx, project)
	if err != nil {
		return err
	}
	diff, err := e.deps.Git.Diff(ctx, proj.RepoPath, att.Worktree, strings.TrimSpace(string(base)), maxRepairDiff)
	if err != nil {
		return fmt.Errorf("previous diff: %w", err)
	}
	fmt.Fprintf(b, "\n### Changes so far (diff against the commit this task started from, after attempt %d)\n\n```diff\n%s\n```\n", prevN, diff)
	return nil
}
