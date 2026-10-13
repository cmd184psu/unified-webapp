package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"cmd184psu/unified-webapp/internal/agentflow/model"
	"cmd184psu/unified-webapp/internal/agentflow/prompts"
	"cmd184psu/unified-webapp/internal/agentflow/runner"
	"cmd184psu/unified-webapp/internal/agentflow/schema"
	"cmd184psu/unified-webapp/internal/agentflow/store"
	"cmd184psu/unified-webapp/internal/taskmaster/golane"
)

const promptMessage = "Follow the instructions in the prompt."

// StageDir is the stage's private directory: prompt.md, the frozen runner
// config, events.jsonl and the agent's home.
func (e *Engine) StageDir(stage int64) string {
	return filepath.Join(e.deps.DataDir, "stages", strconv.FormatInt(stage, 10))
}

// runStage is the body of every agentflow kind. The stage's outcome is written
// to the agentflow DB before it returns (DBS5), so a lane job that ends in any
// state has an outcome behind it, and a panic is recorded as infra:panic even
// though taskmaster's own recover would also catch it (SV4).
func (e *Engine) runStage(ctx context.Context, rc golane.RunContext, p StagePayload) (any, error) {
	res := e.guarded(ctx, rc, p)
	if res.skip {
		return map[string]int64{"stage": p.Stage}, nil
	}
	if !IsInfra(res.Outcome) && res.Outcome != OutcomeDiscarded {
		if stale, _ := e.stale(ctx, p); stale {
			res.Outcome = OutcomeDiscarded
		}
	}
	// The job context may be done (cancel, brake); the outcome must still land.
	if err := e.deps.Store.FinishStage(context.WithoutCancel(ctx), p.Stage, res.Outcome, res.Raw, res.Parsed); err != nil && !errors.Is(err, store.ErrConflict) {
		return nil, fmt.Errorf("stage %d: write outcome %s: %w", p.Stage, res.Outcome, err)
	}
	if e.deps.Kick != nil {
		e.deps.Kick()
	}
	if IsInfra(res.Outcome) || res.Outcome == OutcomeTimeout {
		if res.cause == nil {
			return nil, fmt.Errorf("stage %d: %s", p.Stage, res.Outcome)
		}
		return nil, fmt.Errorf("stage %d: %s: %w", p.Stage, res.Outcome, res.cause)
	}
	return map[string]any{"stage": p.Stage, "outcome": res.Outcome}, nil
}

// guarded runs the stage and turns a panic into infra:panic.
func (e *Engine) guarded(ctx context.Context, rc golane.RunContext, p StagePayload) (res StageResult) {
	defer func() {
		if r := recover(); r != nil {
			msg := fmt.Sprintf("panic: %v", r)
			res = StageResult{Outcome: OutcomeInfraPanic, Raw: msg + "\n" + string(debug.Stack()), cause: errors.New(msg)}
		}
	}()
	return e.execute(ctx, rc, p)
}

// stale reports whether the stage's queue hash is no longer the approved one
// (QA2). Stages without a hash (effort stages) are never stale.
func (e *Engine) stale(ctx context.Context, p StagePayload) (bool, error) {
	st, err := e.deps.Store.GetStage(ctx, p.Stage)
	if err != nil || st.QueueHash == "" {
		return false, err
	}
	cur, err := e.deps.Store.ApprovedQueueHash(ctx, p.Effort)
	if errors.Is(err, store.ErrNotFound) {
		return true, nil
	}
	return err == nil && cur != st.QueueHash, err
}

func (e *Engine) execute(ctx context.Context, rc golane.RunContext, p StagePayload) StageResult {
	fail := func(err error) StageResult {
		return StageResult{Outcome: OutcomeInfraCrash, Raw: err.Error(), cause: err}
	}
	st, err := e.deps.Store.GetStage(ctx, p.Stage)
	if err != nil {
		return fail(err)
	}
	if st.State == model.StageDone {
		return StageResult{skip: true}
	}
	if st.State == model.StageQueued {
		if err := e.deps.Store.StartStage(ctx, st.ID, rc.JobID()); err != nil && !errors.Is(err, store.ErrConflict) {
			return fail(err)
		}
	}
	if stale, err := e.stale(ctx, p); err != nil {
		return fail(err)
	} else if stale {
		return StageResult{Outcome: OutcomeDiscarded}
	}
	switch st.Role {
	case "verify", "merge":
		return e.engineStage(ctx, rc, st, p)
	}
	return e.modelStage(ctx, rc, st, p)
}

// engineStage delegates verify and merge to their executors (filled by C6).
func (e *Engine) engineStage(ctx context.Context, rc golane.RunContext, st model.Stage, p StagePayload) StageResult {
	ex := e.deps.Verify
	if st.Role == "merge" {
		ex = e.deps.Merge
	}
	if ex == nil {
		return StageResult{Outcome: OutcomeInfraCrash, Raw: errNoExecutor.Error(), cause: errNoExecutor}
	}
	res, err := ex.Execute(ctx, rc, st, p)
	if err != nil {
		out := classifyRun(ctx, st.Role, err)
		return StageResult{Outcome: out, Raw: err.Error(), cause: err}
	}
	if res.Outcome == "" {
		err := errors.New("executor returned no outcome")
		return StageResult{Outcome: OutcomeInfraCrash, Raw: err.Error(), cause: err}
	}
	return res
}

func roleTimeout(set store.Settings, role string) time.Duration {
	switch role {
	case "worker":
		return set.WorkerTimeout
	case "reviewer":
		return set.ReviewTimeout
	case "verify":
		return set.VerifyTimeout
	}
	return set.PlanTimeout
}

func (e *Engine) buildPrompt(ctx context.Context, st model.Stage) (string, error) {
	if e.deps.Prompt != nil {
		return e.deps.Prompt(ctx, st)
	}
	text, err := prompts.Load(st.Role)
	if err != nil {
		return "", err
	}
	if st.Attempt == 0 {
		return text, nil
	}
	att, err := e.deps.Store.GetAttempt(ctx, st.Attempt)
	if err != nil {
		return "", err
	}
	task, err := e.deps.Store.GetTask(ctx, att.Task)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString(text)
	fmt.Fprintf(&b, "\n\n## Task: %s\n\n%s\n", task.Title, task.Prompt)
	if len(task.Acceptance) > 0 {
		b.WriteString("\n## Acceptance criteria\n")
		for _, a := range task.Acceptance {
			b.WriteString("- " + a + "\n")
		}
	}
	return b.String(), nil
}

// workdir is the attempt's worktree, or the project repo for effort stages.
func (e *Engine) workdir(ctx context.Context, st model.Stage, project string) (string, error) {
	if st.Attempt != 0 {
		att, err := e.deps.Store.GetAttempt(ctx, st.Attempt)
		if err != nil {
			return "", err
		}
		if att.Worktree == "" {
			return "", fmt.Errorf("attempt %d has no worktree", att.ID)
		}
		return att.Worktree, nil
	}
	pr, err := e.deps.Store.GetProject(ctx, project)
	if err != nil {
		return "", err
	}
	return pr.RepoPath, nil
}

// modelStage runs a model role: prompt.md, frozen config, the runner with a
// role timeout, one corrective follow-up, then the stored output (SO2, SO3).
func (e *Engine) modelStage(ctx context.Context, rc golane.RunContext, st model.Stage, p StagePayload) StageResult {
	d := e.deps
	fail := func(err error) StageResult {
		return StageResult{Outcome: OutcomeInfraCrash, Raw: err.Error(), cause: err}
	}
	b, err := d.Store.GetBackend(ctx, st.Backend)
	if err != nil {
		return fail(err)
	}
	set, err := d.Store.EffectiveSettings(ctx, p.Project)
	if err != nil {
		return fail(err)
	}
	workdir, err := e.workdir(ctx, st, p.Project)
	if err != nil {
		return fail(err)
	}
	prompt, err := e.buildPrompt(ctx, st)
	if err != nil {
		return fail(err)
	}
	dir := e.StageDir(st.ID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fail(err)
	}
	promptPath := filepath.Join(dir, "prompt.md")
	if err := os.WriteFile(promptPath, []byte(prompt), 0o600); err != nil {
		return fail(err)
	}
	be := runner.Backend{Name: b.Name, Host: b.Host, APIType: b.APIType}
	if d.FreezeConfig != nil {
		fb := FrozenBackend{Backend: be}
		for _, m := range b.Models {
			fb.Models = append(fb.Models, m.Model)
		}
		if _, err := d.FreezeConfig(fb, dir); err != nil {
			return fail(fmt.Errorf("freeze runner config: %w", err))
		}
	}

	timeout := roleTimeout(set, st.Role)
	req := runner.Request{
		Role: st.Role, Backend: be, Model: st.Model,
		Workdir: workdir, StageDir: dir, PromptPath: promptPath, Message: promptMessage,
		OnEvent: func(ev runner.Event) {
			if ev.Text != "" {
				fmt.Fprintln(rc.Log(), ev.Text)
			}
			rc.Progress(-1, st.Role+": "+ev.Type)
		},
	}
	// Each call gets the full role timeout, derived here so the parent context
	// can be checked separately: parent cancel is infra:cancel, the role
	// deadline is a timeout (attempt failure for workers).
	call := func(r runner.Request) (runner.Result, string, error) {
		cctx := ctx
		if timeout > 0 {
			var cancel context.CancelFunc
			cctx, cancel = context.WithTimeout(ctx, timeout)
			defer cancel()
		}
		res, err := d.Runner.Run(cctx, r)
		return res, classifyRun(ctx, st.Role, err), err
	}
	failed := func(res runner.Result, outcome string, err error) StageResult {
		raw := res.Final
		if raw == "" {
			raw = err.Error()
		}
		fmt.Fprintf(rc.Log(), "%s: %v\n", outcome, err)
		return StageResult{Outcome: outcome, Raw: raw, cause: err}
	}

	res, outcome, err := call(req)
	if outcome != "" {
		return failed(res, outcome, err)
	}
	parsed, verdict, perr := parseFinal(st.Role, res.Final)
	if perr == nil {
		return StageResult{Outcome: verdict, Raw: res.Final, Parsed: parsed}
	}

	// One corrective follow-up with the same model (SO2): continue the session
	// when the runner gave one, otherwise re-run with the previous reply.
	fmt.Fprintf(rc.Log(), "reply rejected, sending corrective follow-up: %v\n", perr)
	corrective := schema.CorrectiveMessage(perr)
	retry := req
	retryPath := filepath.Join(dir, "corrective.md")
	text := corrective
	if res.SessionID != "" {
		retry.SessionID = res.SessionID
	} else {
		text = prompt + "\n\n---\n\nYour previous reply:\n\n" + res.Final + "\n\n---\n\n" + corrective
	}
	if err := os.WriteFile(retryPath, []byte(text), 0o600); err != nil {
		return fail(err)
	}
	retry.PromptPath = retryPath
	res2, outcome, err := call(retry)
	if outcome != "" {
		return failed(res2, outcome, err)
	}
	parsed, verdict, perr2 := parseFinal(st.Role, res2.Final)
	if perr2 != nil {
		return failed(res2, OutcomeInfraUnparseable, perr2)
	}
	return StageResult{Outcome: verdict, Raw: res2.Final, Parsed: parsed}
}
