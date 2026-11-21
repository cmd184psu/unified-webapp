package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"cmd184psu/unified-webapp/internal/agentflow/model"
	"cmd184psu/unified-webapp/internal/agentflow/runner"
	"cmd184psu/unified-webapp/internal/agentflow/sandbox"
	"cmd184psu/unified-webapp/internal/agentflow/schema"
	"cmd184psu/unified-webapp/internal/agentflow/store"
	"cmd184psu/unified-webapp/internal/agentflow/verify"
)

// RunReview runs one review: a detached checkout of the branch, the project's
// feedback commands under the same sandbox as verify, and one reviewer model
// from the project's reviewer candidates whose family differs from the
// author's. The verdict is stored before the method returns.
func (e *Engine) RunReview(ctx context.Context, id int64) (model.Review, error) {
	rev, err := e.deps.Store.GetReview(ctx, id)
	if err != nil {
		return rev, err
	}
	if rev.State != "queued" {
		return rev, fmt.Errorf("review %d is %s: %w", id, rev.State, store.ErrConflict)
	}
	running := "running"
	if rev, err = e.deps.Store.UpdateReview(ctx, id, store.ReviewPatch{State: &running}); err != nil {
		return rev, err
	}
	fail := func(cause error) (model.Review, error) {
		msg := cause.Error()
		state, done := "failed", time.Now().UnixMilli()
		out, uerr := e.deps.Store.UpdateReview(context.WithoutCancel(ctx), id, store.ReviewPatch{State: &state, Error: &msg, Finished: &done})
		if uerr != nil {
			return out, errors.Join(cause, uerr)
		}
		return out, cause
	}
	p, err := e.deps.Store.GetProject(ctx, rev.Project)
	if err != nil {
		return fail(err)
	}
	repo := rev.RepoPath
	if repo == "" {
		repo = p.RepoPath
	}
	if e.deps.Git == nil {
		return fail(errors.New("review: the git service is not configured"))
	}
	wt, err := e.deps.Git.AddReviewWorktree(ctx, repo, p.Slug, "review-"+strconv.FormatInt(id, 10), rev.Branch)
	if err != nil {
		return fail(fmt.Errorf("review checkout: %w", err))
	}
	defer func() { _ = e.deps.Git.RemoveWorktree(context.WithoutCancel(ctx), repo, wt.Path) }()
	commit := wt.Commit
	if rev, err = e.deps.Store.UpdateReview(ctx, id, store.ReviewPatch{Commit: &commit}); err != nil {
		return fail(err)
	}
	base := strings.TrimSpace(rev.Base)
	if base == "" {
		base = commit
	}
	diff, err := e.deps.Git.Diff(ctx, repo, wt.Path, base, 2<<20)
	if err != nil {
		return fail(fmt.Errorf("review diff: %w", err))
	}

	set, err := e.deps.Store.EffectiveSettings(ctx, p.Slug)
	if err != nil {
		return fail(err)
	}
	rep, results, err := e.reviewVerify(ctx, p, wt.Path, id, set)
	if err != nil {
		return fail(err)
	}
	vj := json.RawMessage(mustJSON(rep))
	if rev, err = e.deps.Store.UpdateReview(ctx, id, store.ReviewPatch{Verify: &vj}); err != nil {
		return fail(err)
	}
	if !rep.Pass {
		verdict, findings := "CHANGES-REQUESTED", reviewFindings(rep, results)
		done, state := time.Now().UnixMilli(), "done"
		return e.deps.Store.UpdateReview(ctx, id, store.ReviewPatch{Verdict: &verdict, Findings: &findings, State: &state, Finished: &done})
	}

	backend, reviewer, err := e.pickReviewer(ctx, p.Slug, rev.AuthorModel, rev.AuthorFamily)
	if err != nil {
		return fail(err)
	}
	dir := filepath.Join(e.deps.DataDir, "reviews", strconv.FormatInt(id, 10))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fail(err)
	}
	prompt := reviewPrompt(rev, diff, rep)
	promptPath := filepath.Join(dir, "prompt.md")
	if err := os.WriteFile(promptPath, []byte(prompt), 0o600); err != nil {
		return fail(err)
	}
	be := runner.Backend{Name: backend.Name, Host: backend.Host, APIType: backend.APIType, Slots: backend.Slots}
	if e.deps.FreezeConfig != nil {
		fb := FrozenBackend{Backend: be, Plugins: p.Plugins}
		for _, m := range backend.Models {
			fb.Models = append(fb.Models, m.Model)
		}
		if _, err := e.deps.FreezeConfig(fb, dir); err != nil {
			return fail(fmt.Errorf("review runner config: %w", err))
		}
	}
	req := runner.Request{
		Role: "reviewer", Backend: be, Model: reviewer, Workdir: wt.Path, StageDir: dir,
		PromptPath: promptPath, Message: promptMessage, Plugins: p.Plugins,
		Job: runner.Job{Project: p.Slug, Task: rev.TaskID},
	}
	if e.deps.KeyEnv != nil {
		name := e.deps.KeyEnv(backend.Name)
		key, err := e.deps.Store.BackendKey(ctx, backend.Name)
		if err != nil {
			return fail(err)
		}
		if key == "" {
			key = os.Getenv(name)
		}
		if key != "" {
			req.Env = append(req.Env, name+"="+key)
		}
	}
	callCtx := ctx
	if set.ReviewTimeout > 0 {
		var cancel context.CancelFunc
		callCtx, cancel = context.WithTimeout(ctx, set.ReviewTimeout)
		defer cancel()
	}
	res, err := e.deps.Runner.Run(callCtx, req)
	if err != nil {
		return fail(fmt.Errorf("reviewer %s/%s: %w", backend.Name, reviewer, err))
	}
	verdict, findings, err := parseReviewReply(res.Final)
	if err != nil {
		return fail(err)
	}
	chosen, done, state := backend.Name, time.Now().UnixMilli(), "done"
	return e.deps.Store.UpdateReview(ctx, id, store.ReviewPatch{
		Verdict: &verdict, Findings: &findings, ReviewerBackend: &chosen,
		ReviewerModel: &reviewer, State: &state, Finished: &done,
	})
}

// reviewVerify runs the project's feedback commands on wt exactly as verify
// does, but with no baseline: a review of a branch has no earlier run to
// compare with, so every failure is new.
func (e *Engine) reviewVerify(ctx context.Context, p store.Project, wt string, id int64, set store.Settings) (verify.VerifyReport, []verify.CommandResult, error) {
	admin, err := e.deps.Git.AdminGitDir(ctx, p.RepoPath, wt)
	if err != nil {
		return verify.VerifyReport{}, nil, err
	}
	home := filepath.Join(e.deps.DataDir, "reviews", strconv.FormatInt(id, 10), "home")
	spec, err := e.feedbackSpec(p, wt, admin, home, p.Toolchains)
	if err != nil {
		return verify.VerifyReport{}, nil, err
	}
	cmds, err := verify.Commands(p.FeedbackCmds, wt)
	if err != nil {
		return verify.VerifyReport{}, nil, err
	}
	cmds = verify.ForToolchains(cmds, p.Toolchains)
	if len(cmds) == 0 {
		return verify.VerifyReport{Pass: true}, nil, nil
	}
	runWT, writable, err := verifyWorktree(p, wt, home, false)
	if err != nil {
		return verify.VerifyReport{}, nil, err
	}
	spec.ReadOnlyWorktree = !writable
	if runWT != wt {
		spec.Worktree = runWT
	}
	if err := e.dockerSpec(ctx, p, sandbox.ContainerName(p.Slug, 0, 0, "review-"+strconv.FormatInt(id, 10)), &spec); err != nil {
		return verify.VerifyReport{}, nil, err
	}
	if spec.Docker != nil {
		defer e.deps.Containers.Release()
	}
	results, err := verify.RunFeedback(ctx, verify.FeedbackConfig{Spec: spec, Timeout: set.VerifyTimeout, WritableWorktree: writable}, cmds)
	if err != nil {
		return verify.VerifyReport{}, nil, err
	}
	rep := verify.VerifyReport{}
	verify.Compare(&rep, results, verify.Baseline{}, nil)
	return rep, results, nil
}

// pickReviewer chooses one reviewer candidate whose model family differs from
// the author's. A review request may name the author's model instead of its
// family; the family is derived from the model then.
func (e *Engine) pickReviewer(ctx context.Context, project, authorModel, authorFamily string) (store.Backend, string, error) {
	if authorFamily == "" {
		authorFamily = model.ModelFamily(authorModel)
	}
	snap, err := e.deps.Store.Snapshot(ctx)
	if err != nil {
		return store.Backend{}, "", err
	}
	c, err := chooseReviewer(snap.CandidatesFor("reviewer", project), authorModel, authorFamily)
	if err != nil {
		return store.Backend{}, "", err
	}
	b, err := e.deps.Store.GetBackend(ctx, c.Backend)
	if err != nil {
		return store.Backend{}, "", err
	}
	if b.Retired {
		return store.Backend{}, "", fmt.Errorf("review: reviewer backend %s is retired", c.Backend)
	}
	return b, c.Model, nil
}

// chooseReviewer picks the first candidate that is neither the author's model
// nor in the author's family. Retired backends are filtered by the caller's
// snapshot; this function keeps the family rule pure and testable.
func chooseReviewer(cands []store.Candidate, authorModel, authorFamily string) (store.Candidate, error) {
	if authorFamily == "" {
		authorFamily = model.ModelFamily(authorModel)
	}
	var reasons []string
	for _, c := range cands {
		if c.Model == authorModel {
			reasons = append(reasons, c.Model+" is the author's model")
			continue
		}
		if authorFamily != "" && model.ModelFamily(c.Model) == authorFamily {
			reasons = append(reasons, c.Model+" is in the author's family")
			continue
		}
		return c, nil
	}
	if len(reasons) == 0 {
		return store.Candidate{}, errors.New("review: the project has no reviewer candidates")
	}
	return store.Candidate{}, fmt.Errorf("review: no independent reviewer for author %q: %s", authorModel, strings.Join(reasons, "; "))
}

// reviewPrompt asks for one JSON verdict. The diff and verify report are in
// the prompt because a reviewer model must be able to decide without tools.
func reviewPrompt(rev model.Review, diff string, rep verify.VerifyReport) string {
	var b strings.Builder
	b.WriteString("You are an independent code reviewer. Review the branch in this working directory against the task below. Do not modify files, commit, push or run the project's tests again; verify has already done that.\n\n")
	fmt.Fprintf(&b, "## Task %s\n\nTitle: %s\n\nObjective:\n%s\n\nAcceptance:\n%s\n\nExclusions:\n%s\n\n", rev.TaskID, rev.Title, rev.Objective, rev.Acceptance, rev.Exclusions)
	fmt.Fprintf(&b, "## Branch\n\n%s at %s (base %s)\n\n", rev.Branch, rev.Commit, rev.Base)
	if strings.TrimSpace(diff) != "" {
		b.WriteString("## Diff\n\n```diff\n")
		b.WriteString(diff)
		b.WriteString("\n```\n\n")
	} else {
		b.WriteString("## Diff\n\n(no diff against the base)\n\n")
	}
	if vj, err := json.MarshalIndent(rep, "", " "); err == nil {
		fmt.Fprintf(&b, "## Verify report\n\n```json\n%s\n```\n\n", vj)
	}
	b.WriteString("Judge correctness, scope and the acceptance criteria. Request changes only for real defects, not style. Your final message must be exactly one JSON object:\n")
	b.WriteString(`{"verdict":"pass","summary":"one sentence","issues":[]}`)
	b.WriteString("\n\nUse verdict \"repair\" and put each defect in issues with description, file and line when the work must change. Use verdict \"pass\" when it meets the task.\n")
	return b.String()
}

// parseReviewReply reads the reviewer's JSON and turns it into the review
// verdict and numbered findings.
func parseReviewReply(final string) (string, string, error) {
	raw, err := schema.ExtractFinal(final)
	if err != nil {
		return "", "", fmt.Errorf("reviewer returned no JSON verdict: %w", err)
	}
	var out struct {
		Verdict string `json:"verdict"`
		Summary string `json:"summary"`
		Issues  []struct {
			Description string `json:"description"`
			File        string `json:"file"`
			Line        int    `json:"line"`
		} `json:"issues"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", "", fmt.Errorf("reviewer verdict: %w", err)
	}
	switch strings.ToLower(strings.TrimSpace(out.Verdict)) {
	case "pass", "passed":
		text := "1. No findings."
		if out.Summary != "" {
			text = "1. " + out.Summary
		}
		return "PASS", text, nil
	case "repair", "changes-requested", "changes_requested", "fail":
		if len(out.Issues) == 0 {
			text := "1. " + strings.TrimSpace(out.Summary)
			if text == "1. " {
				text = "1. The reviewer asked for changes without naming an issue."
			}
			return "CHANGES-REQUESTED", text, nil
		}
		var lines []string
		for i, is := range out.Issues {
			where := is.File
			if is.Line > 0 {
				where = fmt.Sprintf("%s:%d", where, is.Line)
			}
			if where != "" {
				lines = append(lines, fmt.Sprintf("%d. %s (%s)", i+1, is.Description, where))
			} else {
				lines = append(lines, fmt.Sprintf("%d. %s", i+1, is.Description))
			}
		}
		return "CHANGES-REQUESTED", strings.Join(lines, "\n"), nil
	}
	return "", "", fmt.Errorf("reviewer returned unknown verdict %q", out.Verdict)
}

// reviewFindings turns a failed verify report into numbered findings with the
// failing output, trimmed so a queue file stays readable.
func reviewFindings(rep verify.VerifyReport, results []verify.CommandResult) string {
	var lines []string
	seen := map[string]bool{}
	for _, f := range rep.NewFailures {
		if f == "" || seen[f] {
			continue
		}
		seen[f] = true
		lines = append(lines, f)
	}
	var out []string
	for i, line := range lines {
		out = append(out, fmt.Sprintf("%d. %s", i+1, line))
	}
	for _, ev := range rep.Evidence {
		text := strings.TrimSpace(ev.Text)
		if text != "" {
			out = append(out, "   "+clipReviewText(text, 3000))
			break
		}
	}
	if len(out) == 0 {
		for _, r := range results {
			if r.Failed() {
				out = append(out, fmt.Sprintf("1. %s exited %d", r.Argv.String(), r.ExitCode))
				if text := strings.TrimSpace(r.Output); text != "" {
					out = append(out, "   "+clipReviewText(text, 3000))
				}
				break
			}
		}
	}
	if len(out) == 0 {
		out = []string{"1. Verify failed without a named check."}
	}
	return strings.Join(out, "\n")
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

func clipReviewText(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "\n[truncated]"
}
