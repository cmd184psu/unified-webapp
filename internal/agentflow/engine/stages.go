package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"cmd184psu/unified-webapp/internal/agentflow/model"
	"cmd184psu/unified-webapp/internal/agentflow/prompts"
	"cmd184psu/unified-webapp/internal/agentflow/rulings"
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
	if res.Outcome == OutcomeDetached {
		// A seat that keeps working outside the process: no outcome, the stage stays open and
		// the next process adopts it (reconcile.go).
		fmt.Fprintf(rc.Log(), "seat detached: %v\n", res.cause)
		return nil, fmt.Errorf("stage %d: seat detached: %w", p.Stage, res.cause)
	}
	if res.Outcome == OutcomeInfraCrash && ctx.Err() != nil {
		// Cancelled (stop, brake, shutdown) while still setting up: the store
		// calls failed on the dead context, which is a cancel, not a crash.
		res.Outcome = OutcomeInfraCancel
	}
	if !IsInfra(res.Outcome) && res.Outcome != OutcomeDiscarded {
		if stale, _ := e.stale(ctx, p); stale {
			res.Outcome = OutcomeDiscarded
		}
	}
	// Usage lands before the outcome: the dispatcher is kicked after the
	// outcome and must see it when it checks the effort's budget.
	if res.Tokens > 0 {
		if err := e.deps.Store.RecordStageTokens(context.WithoutCancel(ctx), p.Stage, res.Tokens); err != nil {
			fmt.Fprintf(rc.Log(), "record token usage: %v\n", err)
		}
	}
	// The job context may be done (cancel, brake); the outcome must still land.
	if err := e.deps.Store.FinishStage(context.WithoutCancel(ctx), p.Stage, res.Outcome, res.Raw, res.Parsed); err != nil && !errors.Is(err, store.ErrConflict) {
		return nil, fmt.Errorf("stage %d: write outcome %s: %w", p.Stage, res.Outcome, err)
	}
	if res.Outcome == OutcomeInfraUnreachable {
		if err := e.markUnreachable(context.WithoutCancel(ctx), p.Stage); err != nil {
			fmt.Fprintf(rc.Log(), "mark backend down: %v\n", err)
		}
	}
	if err := e.noteRest(context.WithoutCancel(ctx), p.Stage, res.Outcome); err != nil {
		fmt.Fprintf(rc.Log(), "rest model: %v\n", err)
	}
	// AF-15b: drop the stage's home directory once it is decided, when the
	// keep window is zero; larger windows are swept by the retention pruner.
	e.pruneAfterStage(context.WithoutCancel(ctx), p.Stage)
	if e.deps.Kick != nil {
		e.deps.Kick()
	}
	if p.Attempt == 0 {
		// A planning-loop or compile stage: the next step follows from its
		// outcome. A failure here is picked up again by the next tick.
		if err := e.AdvancePlans(context.WithoutCancel(ctx)); err != nil {
			fmt.Fprintf(rc.Log(), "advance plans: %v\n", err)
		}
	}
	if e.deps.Git != nil {
		// The outcome is durable; chain the next stage from it. A failure
		// here is picked up again by the next Advance (tick or completion).
		if err := e.Advance(context.WithoutCancel(ctx)); err != nil {
			fmt.Fprintf(rc.Log(), "advance: %v\n", err)
		}
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
	// Every relaxation is written to the stage log as well as the stage row,
	// so the run is auditable from either place (AF-6b).
	if len(st.Relaxations) > 0 {
		fmt.Fprintf(rc.Log(), "relaxations for this stage: %s\n", strings.Join(st.Relaxations, ", "))
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
	if e.deps.Git != nil && st.Attempt != 0 {
		return e.policedStage(ctx, rc, st, p)
	}
	return e.modelStage(ctx, rc, st, p)
}

// engineStage delegates verify and merge to their executors.
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

// changed tells event stream subscribers that an effort's status just moved,
// after the status is written: the notice sent when a stage's outcome is
// stored comes earlier, so a view that refetches on it still sees the old
// status.
func (e *Engine) changed() {
	if e.deps.Kick != nil {
		e.deps.Kick()
	}
}

func (e *Engine) rulingHeader(ctx context.Context, st model.Stage, project string, files []string) string {
	set, err := e.deps.Store.EffectiveSettings(ctx, project)
	if err != nil {
		return ""
	}
	wd, err := e.workdir(ctx, st, project)
	if err != nil {
		return ""
	}
	return rulings.Header(wd, set.RulingsFile, files, rulings.ParseAreaMap(set.RulingsFileAreaMap))
}

func (e *Engine) effortRulingHeader(ctx context.Context, st model.Stage, project string) string {
	// effortRulingHeader is called only from the buildPrompt path for effort
	// stages (attempt == 0). The project slug is passed in by the caller.
	// The header is the effort-level ruling header: files=nil, so only preamble + process.
	return e.rulingHeader(ctx, st, project, nil)
}

func roleTimeout(set store.Settings, role string) time.Duration {
	switch role {
	case "worker", "cleanup":
		return set.WorkerTimeout
	case "reviewer":
		return set.ReviewTimeout
	case "verify":
		return set.VerifyTimeout
	case "merger":
		return set.MergerTimeout
	}
	return set.PlanTimeout
}

func (e *Engine) buildPrompt(ctx context.Context, st model.Stage, project string) (string, error) {
	if e.deps.Prompt != nil {
		return e.deps.Prompt(ctx, st)
	}
	text, err := prompts.Load(st.Role)
	if err != nil {
		return "", err
	}
	if st.Attempt == 0 {
		// Effort stages (attempt == 0) get the effort-level ruling header,
		// files=nil, so only the preamble + process rulings apply.
		if h := e.effortRulingHeader(ctx, st, project); h != "" {
			text += "\n\n" + h
		}
		return e.EffortPrompt(ctx, st)
	}
	if st.Role == "merger" {
		// merger ships its own prompt shape; the ruling header still applies
		// for the merge decision, files=nil.
		if h := e.rulingHeader(ctx, st, project, nil); h != "" {
			text += "\n\n" + h
		}
		return e.mergerPrompt(ctx, project, st)
	}
	if isMergeReview(st) {
		if h := e.rulingHeader(ctx, st, project, nil); h != "" {
			text += "\n\n" + h
		}
		return e.mergerReviewPrompt(ctx, project, st)
	}
	if st.Role == "reviewer" {
		ev, err := e.reviewEvidence(ctx, st.Attempt)
		if err != nil {
			return "", err
		}
		text += ev
	}
	att, err := e.deps.Store.GetAttempt(ctx, st.Attempt)
	if err != nil {
		return "", err
	}
	task, err := e.deps.Store.GetTask(ctx, att.Task)
	if err != nil {
		return "", err
	}
	// Attempt-stage prompt: the task's files_touched determine which rulings
	// apply, so the reviewer/worker/planner/architect/critic/compiler/cleanup
	// get the ruling header that matches this task.
	if h := e.rulingHeader(ctx, st, project, task.FilesTouched); h != "" {
		text += "\n\n" + h
	}
	var b strings.Builder
	if st.Role == "reviewer" {
		taskJSON, _ := json.Marshal(map[string]any{"key": task.Key, "title": task.Title, "prompt": task.Prompt,
			"acceptance": task.Acceptance, "files_touched": task.FilesTouched})
		text = strings.ReplaceAll(text, "{{TASK_JSON}}", string(taskJSON))
	}
	b.WriteString(text)
	fmt.Fprintf(&b, "\n\n## Task: %s\n\n%s\n", task.Title, task.Prompt)
	if len(task.Acceptance) > 0 {
		b.WriteString("\n## Acceptance criteria\n")
		for _, a := range task.Acceptance {
			b.WriteString("- " + a + "\n")
		}
	}
	if st.Role == "worker" || st.Role == "reviewer" {
		b.WriteString(e.frdSection(ctx, task))
		dod, err := e.dodSection(ctx, st.Role, project, att)
		if err != nil {
			return "", err
		}
		b.WriteString(dod)
	}
	if st.Role == "reviewer" {
		b.WriteString(e.reviewFiles(ctx, att, task))
		stages, err := e.deps.Store.ListAttemptStages(ctx, att.ID)
		if err != nil {
			return "", err
		}
		if reason := (&chain{stages: stages}).noChangesReason(); reason != "" {
			fmt.Fprintf(&b, "\n## The worker says nothing needs changing\n\nThe worker reports: %s\n\nJudge whether the FILES section already satisfies every acceptance criterion. If it does, the verdict is \"pass\" and the summary cites the evidence (file and symbol). If not, the verdict is \"repair\" and the repair prompt lists the exact missing items.\n", reason)
		}
	}
	if st.Role == "worker" {
		wc, err := e.WorkerContext(ctx, project, att.ID)
		if err != nil {
			return "", err
		}
		b.WriteString(wc)
	}
	return b.String(), nil
}

// dodSection is the "Definition of done" section of a worker or reviewer
// prompt: the project's standard for every task. It is read from the
// attempt's worktree, which has the files the task has built so far.
func (e *Engine) dodSection(ctx context.Context, role, project string, att model.Attempt) (string, error) {
	p, err := e.deps.Store.GetProject(ctx, project)
	if err != nil {
		return "", err
	}
	dir := att.Worktree
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		dir = p.RepoPath
	}
	var b strings.Builder
	b.WriteString("\n## Definition of done\n\nEvery task is held to this standard; the engine runs the commands after each task.\n")
	for _, it := range definitionOfDone(p, dir).Items {
		b.WriteString("- " + it + "\n")
	}
	if role == "reviewer" {
		b.WriteString("\nJudge the work against this standard as well as the task's acceptance criteria.\n")
	} else {
		b.WriteString("\nLeave the code meeting it; running these commands is not a task of its own.\n")
	}
	return b.String(), nil
}

// workdir is the attempt's worktree, or the project repo for effort stages;
// an acceptance check reads the effort's merged result, in the integration
// worktree.
func (e *Engine) workdir(ctx context.Context, st model.Stage, project string) (string, error) {
	if isAcceptance(st) && e.deps.Git != nil {
		if dir, err := e.deps.Git.IntegrationPath(project, effortName(st.Effort)); err == nil {
			if fi, err := os.Stat(dir); err == nil && fi.IsDir() {
				return dir, nil
			}
		}
	}
	if isResolver(st) {
		// The resolver reads the task's kept worktree: its last attempt is on disk.
		if _, after := resolverTarget(st.PromptRef); after > 0 {
			if att, err := e.deps.Store.GetAttempt(ctx, after); err == nil && att.Worktree != "" {
				if fi, err := os.Stat(att.Worktree); err == nil && fi.IsDir() {
					return att.Worktree, nil
				}
			}
		}
	}
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

// modelStage runs a model role and reports the tokens its runner calls used
// (AT12), whether or not the stage passed.
func (e *Engine) modelStage(ctx context.Context, rc golane.RunContext, st model.Stage, p StagePayload) StageResult {
	var tokens int64
	res := e.modelCall(ctx, rc, st, p, &tokens)
	res.Tokens = tokens
	return res
}

// modelCall runs a model role: prompt.md, frozen config, the runner with a
// role timeout, one corrective follow-up, then the stored output (SO2, SO3).
// Each runner call adds its usage to *tokens.
func (e *Engine) modelCall(ctx context.Context, rc golane.RunContext, st model.Stage, p StagePayload, tokens *int64) StageResult {
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
	proj, err := d.Store.GetProject(ctx, p.Project)
	if err != nil {
		return fail(err)
	}
	workdir, err := e.workdir(ctx, st, p.Project)
	if err != nil {
		return fail(err)
	}
	deliberate := false
	if st.Role == "planner" {
		ef, err := d.Store.GetEffort(ctx, st.Effort)
		if err != nil {
			return fail(err)
		}
		deliberate = ef.Deliberate
	}
	prompt, err := e.buildPrompt(ctx, st, p.Project)
	if err != nil {
		return fail(err)
	}
	// parse reads the stage's reply; an FRD draft, polish or split is a
	// planner-role stage with its own reply shape.
	purpose := draftPurpose(st)
	parse := func(final string) (json.RawMessage, string, error) {
		if purpose != "" {
			return parseDraft(purpose, final)
		}
		if isAcceptance(st) {
			return parseAcceptance(final)
		}
		if isResolver(st) {
			return parseResolver(final)
		}
		return parseFinal(st.Role, final, deliberate)
	}
	dir := e.StageDir(st.ID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fail(err)
	}
	promptPath := filepath.Join(dir, "prompt.md")
	if err := os.WriteFile(promptPath, []byte(prompt), 0o600); err != nil {
		return fail(err)
	}
	be := runner.Backend{Name: b.Name, Host: b.Host, APIType: b.APIType, CLIPath: b.CLIPath, Slots: b.Slots}
	// A seat keeps its own clock: no deadline and no stall watch on the call, the role's
	// timeout goes to the runner as a budget, and a seat past it is reported as needing
	// attention instead of being failed and retried (AF-9 review finding 1).
	seat := e.isSeatAPI(b.APIType)
	if d.FreezeConfig != nil {
		fb := FrozenBackend{Backend: be, Plugins: proj.Plugins}
		for _, m := range b.Models {
			fb.Models = append(fb.Models, m.Model)
		}
		if _, err := d.FreezeConfig(fb, dir); err != nil {
			return fail(fmt.Errorf("freeze runner config: %w", err))
		}
	}

	job := runner.Job{Project: p.Project, Effort: p.Effort, Stage: p.Stage, Attempt: p.Attempt}
	if p.Task != 0 {
		if t, err := d.Store.GetTask(ctx, p.Task); err == nil {
			job.Task = t.Key
		}
	}
	timeout := roleTimeout(set, st.Role)
	var lastSeen atomic.Int64 // when the model last said anything (H30)
	lastSeen.Store(d.Clock().UnixNano())
	req := runner.Request{
		Role: st.Role, Backend: be, Model: st.Model,
		Workdir: workdir, StageDir: dir, PromptPath: promptPath, Message: promptMessage, Plugins: proj.Plugins, Job: job,
		OnEvent: func(ev runner.Event) {
			lastSeen.Store(d.Clock().UnixNano())
			if ev.Text != "" {
				fmt.Fprintln(rc.Log(), ev.Text)
			}
			rc.Progress(-1, st.Role+": "+ev.Type)
		},
	}
	// Each call gets the full role timeout, derived here so the parent context
	// can be checked separately: parent cancel is infra:cancel, the role
	// deadline is a timeout (attempt failure for workers).
	if d.KeyEnv != nil {
		name := d.KeyEnv(b.Name)
		key, err := d.Store.BackendKey(ctx, b.Name)
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
	// A worker may need files the project names in worker_env, such as an
	// oracle save (AF-14). A regular file is copied into the stage directory,
	// which the sandbox already allows, and the variable points at the copy;
	// other values pass through unchanged.
	if st.Role == "worker" {
		for k, v := range proj.WorkerEnv {
			if filepath.IsAbs(v) {
				if fi, err := os.Stat(v); err == nil && fi.Mode().IsRegular() {
					dst := filepath.Join(dir, "worker-env", k, filepath.Base(v))
					if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
						return fail(err)
					}
					data, err := os.ReadFile(v)
					if err != nil {
						return fail(fmt.Errorf("worker_env %s: %w", k, err))
					}
					if err := os.WriteFile(dst, data, 0o600); err != nil {
						return fail(fmt.Errorf("worker_env %s: %w", k, err))
					}
					v = dst
				}
			}
			req.Env = append(req.Env, k+"="+v)
		}
	}
	if seat {
		req.Budget = timeout
	}
	call := func(r runner.Request) (runner.Result, string, error) {
		cctx := ctx
		if timeout > 0 && !seat {
			var cancel context.CancelFunc
			cctx, cancel = context.WithTimeout(ctx, timeout)
			defer cancel()
		}
		stall := set.StallMinutes
		if seat {
			stall = 0
		}
		res, err := e.runWatched(cctx, r, stall, &lastSeen)
		*tokens += usageTokens(res.Usage)
		if isDetached(err) {
			return res, OutcomeDetached, err
		}
		out := classifyRun(ctx, st.Role, err)
		if out == OutcomeInfraContext {
			e.learnWindow(ctx, st, err)
		}
		return res, out, err
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
	parsed, verdict, perr := parse(res.Final)
	if perr == nil {
		// AF-22: a reviewer repair that contradicts an injected ruling is
		// invalid. Override it to pass, record a coordinator note, and raise a
		// conflicting_guidance attention item. Do not fail the attempt.
		if verdict == OutcomeRepair && st.Role == "reviewer" && p.Task != 0 {
			if t, err := d.Store.GetTask(ctx, p.Task); err == nil {
				rulingsSel := func() []rulings.Ruling {
					if set.RulingsFile == "" || !rulings.CheapExists(filepath.Join(workdir, filepath.Clean(set.RulingsFile))) {
						return nil
					}
					rf, lerr := rulings.Load(filepath.Join(workdir, filepath.Clean(set.RulingsFile)))
					if lerr != nil {
						return nil
					}
					am := rulings.ParseAreaMap(set.RulingsFileAreaMap)
					return rulings.Selected(rf, t.FilesTouched, am)
				}()
				if len(rulingsSel) > 0 && rulings.ContradictsRuling(rulings.ParseReviewIssues(parsed), rulingsSel) {
					verdict = OutcomePass
					ids := make([]string, 0, len(rulingsSel))
					for _, r := range rulingsSel {
						ids = append(ids, r.ID)
					}
					note := fmt.Sprintf("reviewer repair dropped: it contradicted an injected ruling (%s); not a repair — coordinator note", strings.Join(ids, ", "))
					_ = d.Store.AppendProgress(ctx, t.ID, st.Attempt, note)
					e.noteEffort(ctx, SystemActor, "guidance.conflict", p.Effort, map[string]string{"task": t.Key, "note": note})
				}
			}
		}
		return StageResult{Outcome: verdict, Raw: res.Final, Parsed: parsed}
	}
	// A reply with no JSON object that is also not coherent text is gibberish,
	// not a slip: the model is not doing the job, and a second run of it would
	// only cost minutes. Give up on the first try; the retry goes to another
	// model (H11). A coherent prose summary lacks only the object: that gets
	// the corrective follow-up below (H14).
	if errors.Is(perr, schema.ErrNoObject) && schema.Incoherent(res.Final) {
		return failed(res, OutcomeInfraUnparseable, perr)
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
	parsed, verdict, perr2 := parse(res2.Final)
	if perr2 != nil {
		return failed(res2, OutcomeInfraUnparseable, perr2)
	}
	return StageResult{Outcome: verdict, Raw: res2.Final, Parsed: parsed}
}

// Caps on what the reviewer's prompt carries, so one large file cannot swamp it.
const (
	reviewFileCap  = 24 << 10
	reviewTotalCap = 96 << 10
)

// reviewFiles is the FILES section of the reviewer's prompt: the worker's own
// log and the current content of every file the task or the worker touched,
// read from the attempt's worktree. A reviewer model with unreliable tool use
// can then judge from the prompt alone instead of guessing about files it
// never opened.
func (e *Engine) reviewFiles(ctx context.Context, att model.Attempt, task model.Task) string {
	var b strings.Builder
	names := append([]string{}, task.FilesTouched...)
	// Files of the tasks this one depends on are context: code a test calls may
	// live there, and a reviewer that cannot see it reports it as missing.
	ctxNames := e.dependencyFiles(ctx, att, task)
	if stages, err := e.deps.Store.ListAttemptStages(ctx, att.ID); err == nil {
		for i := len(stages) - 1; i >= 0; i-- {
			st := stages[i]
			if st.Role != "worker" || len(st.Parsed) == 0 {
				continue
			}
			var log struct {
				Summary      string   `json:"summary"`
				FilesChanged []string `json:"files_changed"`
				Blocked      string   `json:"blocked"`
			}
			if json.Unmarshal(st.Parsed, &log) == nil {
				fmt.Fprintf(&b, "\n\n## Worker log\n\nSummary: %s\n", log.Summary)
				if log.Blocked != "" {
					fmt.Fprintf(&b, "Blocked: %s\n", log.Blocked)
				}
				names = append(names, log.FilesChanged...)
			}
			break
		}
	}
	b.WriteString("\n\n## FILES (read from the worktree after the worker finished)\n")
	seen, total := map[string]bool{}, 0
	for i, n := range append(names, ctxNames...) {
		if i == len(names) && len(ctxNames) > 0 {
			b.WriteString("\n## Files of the tasks this task depends on (context, not part of this task)\n")
		}
		rel := filepath.Clean(n)
		if rel == "." || filepath.IsAbs(rel) || strings.HasPrefix(rel, "..") || seen[rel] {
			continue
		}
		seen[rel] = true
		data, err := os.ReadFile(filepath.Join(att.Worktree, rel))
		switch {
		case err != nil:
			fmt.Fprintf(&b, "\n### %s\n\nMISSING: the file does not exist in the worktree.\n", rel)
		case total >= reviewTotalCap:
			fmt.Fprintf(&b, "\n### %s\n\n(omitted: prompt size limit; the file is %s in your working directory; read it before judging)\n", rel, rel)
		default:
			if len(data) > reviewFileCap {
				data = append(data[:reviewFileCap], []byte(fmt.Sprintf("\n... (truncated at %d of %d bytes: the whole file is %s in your working directory; read it before judging)", reviewFileCap, len(data), rel))...)
			}
			total += len(data)
			fmt.Fprintf(&b, "\n### %s\n\n```\n%s\n```\n", rel, strings.TrimRight(string(data), "\n"))
		}
	}
	return b.String()
}

// dependencyFiles lists the files the tasks this task depends on touch, for
// the reviewer's context. Errors just mean less context.
func (e *Engine) dependencyFiles(ctx context.Context, att model.Attempt, task model.Task) []string {
	if len(task.DependsOn) == 0 {
		return nil
	}
	all, err := e.deps.Store.ListQueueTasks(ctx, task.Queue)
	if err != nil {
		return nil
	}
	want := map[string]bool{}
	for _, k := range task.DependsOn {
		want[k] = true
	}
	var out []string
	for _, d := range all {
		if want[d.Key] {
			out = append(out, d.FilesTouched...)
		}
	}
	return out
}

// isDetached says the runner stopped waiting while its seat works on outside the process.
func isDetached(err error) bool {
	var re *runner.Error
	return errors.As(err, &re) && re.Class == runner.ErrDetached
}
