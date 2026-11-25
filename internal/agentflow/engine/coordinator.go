package engine

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"cmd184psu/unified-webapp/internal/agentflow/model"
	"cmd184psu/unified-webapp/internal/agentflow/prompts"
	"cmd184psu/unified-webapp/internal/agentflow/schema"
	"cmd184psu/unified-webapp/internal/agentflow/store"
)

// The coordinator role (AF-19). A coordinator run is an effort stage of its own
// role: the engine launches it on a schedule, the stage reads the attention list
// and the runbook and declares what should happen next as JSON actions, and the
// engine applies the actions it may apply. A stage never changes anything
// itself; every change goes through the same engine call the HTTP API makes, with
// the stage's model as the actor, so it is audited like an operator's action.
//
// Failover is the role's candidate list, project first then global. When no
// coordinator list is configured a code-level default is tried (the Claude model
// the interim coordinator ran on, then any Beam model) so an unconfigured
// service still gets one. A run whose model is rate limited or unreachable rests
// that model (rest.go) and the next tick picks the next candidate.
//
// Two limits bound it: coordinator_runs_per_day starts per day and
// coordinator_actions_per_run actions per run. Actions that stay with the owner
// (an FRD approval, a merge, a promotion, a deletion, settings or candidate
// changes, a service restart) are refused and asked as a question; the attention
// list shows them until the next run replaces them.

const (
	coordinatorRef              = "coordinator:"
	eventCoordinatorStart       = "coordinator.start"
	eventCoordinatorRun         = "coordinator.run"
	eventCoordinatorUnavailable = "coordinator.unavailable"
	coordinatorService          = "service"
	coordinatorEventWindow      = 10000
	coordinatorRunbookLimit     = 64 * 1024
	coordinatorRunLookback      = 50
)

// defaultCoordinatorModels is the order a run is tried in when the service lists
// no coordinator candidates: the Claude model the owner's coordinator ran on
// first, then any Beam model, matching the stand-in list the interim coordinator
// used. Only models a backend serves are tried.
var defaultCoordinatorModels = []string{"claude-sonnet-5-5", "beam"}

// isCoordinator reports whether a stage is a coordinator run.
func isCoordinator(st model.Stage) bool { return st.Role == "coordinator" }

// coordinatorKinds are the attention kinds a run is asked about: things the
// engine's automatic rules did not resolve, which one of the coordinator's
// actions can move. A seat question, a service-wide item or an unassignable task
// waits for the owner instead.
func coordinatorKind(kind string) bool {
	switch kind {
	case "task_halted", "task_needs_files", "stalled", "plan_halted", "compile_halted", "draft_failed", "frd_contradiction":
		return true
	}
	return false
}

// CoordinatorTick applies the runs that finished and starts one when it is due.
// It runs from the engine's tick hook, so a coordinator needs no cron of its own.
func (e *Engine) CoordinatorTick(ctx context.Context) error {
	if err := e.applyCoordinatorRuns(ctx); err != nil {
		return err
	}
	return e.startCoordinatorRun(ctx)
}

// coordinatorPrompt builds a run's prompt: the role prompt, the owner's runbook,
// the attention list and the work in progress.
func (e *Engine) coordinatorPrompt(ctx context.Context, st model.Stage) (string, error) {
	text, err := prompts.Load("coordinator")
	if err != nil {
		return "", err
	}
	set, err := e.deps.Store.GetSettings(ctx)
	if err != nil {
		return "", err
	}
	items, err := e.Attention(ctx)
	if err != nil {
		return "", err
	}
	wanted := []AttentionItem{}
	for _, it := range items {
		if coordinatorKind(it.Kind) {
			wanted = append(wanted, it)
		}
	}
	work, err := e.coordinatorWork(ctx)
	if err != nil {
		return "", err
	}
	text = strings.ReplaceAll(text, "{{RUNBOOK}}", coordinatorRunbook(set.CoordinatorRunbook))
	text = strings.ReplaceAll(text, "{{ATTENTION_JSON}}", jsonText(wanted))
	text = strings.ReplaceAll(text, "{{PROJECTS_JSON}}", jsonText(work))
	text = strings.ReplaceAll(text, "{{ACTIONS_PER_RUN}}", strconv.Itoa(set.CoordinatorActionsPerRun))
	return text, nil
}

// jsonText is a value as one compact JSON line, for a prompt.
func jsonText(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "null"
	}
	return string(b)
}

// coordinatorRunbook reads the runbook file, or says it is missing.
func coordinatorRunbook(path string) string {
	if path == "" {
		return "(no runbook is configured: decide from the list below and ask about anything you are unsure of)"
	}
	home, _ := os.UserHomeDir()
	full := path
	if strings.HasPrefix(path, "~/") && home != "" {
		full = filepath.Join(home, path[2:])
	}
	b, err := os.ReadFile(full)
	if err != nil {
		return fmt.Sprintf("(no runbook at %s: decide from the list below and ask about anything you are unsure of)", path)
	}
	if len(b) > coordinatorRunbookLimit {
		b = b[:coordinatorRunbookLimit]
	}
	return string(b)
}

// coordinatorWork is the work in progress a run needs to name an effort: each
// project's open efforts with their status, budget use and running state.
func (e *Engine) coordinatorWork(ctx context.Context) ([]map[string]any, error) {
	projects, err := e.deps.Store.ListProjects(ctx)
	if err != nil {
		return nil, err
	}
	out := []map[string]any{}
	for _, p := range projects {
		efforts, err := e.deps.Store.ListEfforts(ctx, p.Slug)
		if err != nil {
			return nil, err
		}
		var open []map[string]any
		for _, ef := range efforts {
			if ef.Status == model.EffortComplete || ef.Status == model.EffortAbandoned {
				continue
			}
			row := map[string]any{"effort": ef.ID, "title": ef.Title, "status": ef.Status}
			if b, err := e.EffortBudget(ctx, ef); err == nil {
				row["tokens"] = b.Tokens
				row["token_limit"] = b.TokenLimit
			}
			open = append(open, row)
		}
		if len(open) > 0 {
			out = append(out, map[string]any{"project": p.Slug, "efforts": open})
		}
	}
	return out, nil
}

// coordinatorEvents reads the service's coordinator events, oldest first.
func (e *Engine) coordinatorEvents(ctx context.Context) ([]store.Event, error) {
	evs, err := e.deps.Store.ListEvents(ctx, coordinatorService, "", coordinatorEventWindow)
	if err != nil {
		return nil, err
	}
	out := []store.Event{}
	for _, ev := range evs {
		switch ev.Action {
		case eventCoordinatorStart, eventCoordinatorRun, eventCoordinatorUnavailable:
			out = append(out, ev)
		}
	}
	return out, nil
}

// noteCoordinator writes an audit event about the coordinator itself.
func (e *Engine) noteCoordinator(ctx context.Context, actor, action string, detail any) error {
	return e.deps.Store.WithTx(ctx, func(tx *sql.Tx) error {
		_, err := store.AppendEvent(tx, actor, action, coordinatorService, "", detail)
		return err
	})
}

type coordinatorRunEvent struct {
	Stage   int64  `json:"stage"`
	Backend string `json:"backend,omitempty"`
	Model   string `json:"model,omitempty"`
	Summary string `json:"summary,omitempty"`
	// Infra says the run could not be made at all (rate limited, unreachable,
	// timed out): it does not count as the period's check, so the next tick
	// tries the next candidate.
	Infra bool `json:"infra,omitempty"`
	// Applied, Refused and Failed are the actions by kind and target; Questions
	// are the decisions the run left with the owner.
	Applied   []coordinatorOutcome         `json:"applied,omitempty"`
	Refused   []coordinatorOutcome         `json:"refused,omitempty"`
	Failed    []coordinatorOutcome         `json:"failed,omitempty"`
	Skipped   int                          `json:"skipped,omitempty"`
	Questions []schema.CoordinatorQuestion `json:"questions,omitempty"`
}

type coordinatorOutcome struct {
	Kind   string `json:"kind"`
	Target string `json:"target,omitempty"`
	Detail string `json:"detail,omitempty"`
}

// startCoordinatorRun launches one run when the schedule, the day's cap and the
// attention list all allow it.
func (e *Engine) startCoordinatorRun(ctx context.Context) error {
	set, err := e.deps.Store.GetSettings(ctx)
	if err != nil {
		return err
	}
	if set.CoordinatorCheckMinutes <= 0 {
		return nil // the coordinator is off
	}
	if h, err := e.deps.Store.ServiceHold(ctx); err != nil {
		return err
	} else if h.Active {
		return nil
	}
	evs, err := e.coordinatorEvents(ctx)
	if err != nil {
		return err
	}
	if !coordinatorDue(set, evs, e.deps.Clock()) {
		return nil
	}
	// A run already in flight is the check; do not start a second one.
	if open, err := e.coordinatorRunOpen(ctx, evs); err != nil {
		return err
	} else if open {
		return nil
	}
	items, err := e.Attention(ctx)
	if err != nil {
		return err
	}
	var wanted []AttentionItem
	for _, it := range items {
		if coordinatorKind(it.Kind) && it.Project != "" && it.Effort != 0 {
			wanted = append(wanted, it)
		}
	}
	if len(wanted) == 0 {
		return nil // nothing the coordinator could move
	}
	proj, err := e.deps.Store.GetProject(ctx, wanted[0].Project)
	if err != nil {
		return err
	}
	pk, reason, err := e.pickCoordinator(ctx, proj)
	if err != nil {
		return err
	}
	if reason != "" {
		return e.noteCoordinator(ctx, SystemActor, eventCoordinatorUnavailable,
			map[string]any{"reason": reason, "items": len(wanted)})
	}
	ref := coordinatorRef + strconv.FormatInt(e.deps.Clock().UnixMilli(), 10)
	st, _, err := e.Submit(ctx, BackendLane(pk.Backend),
		store.StageSpec{Effort: wanted[0].Effort, Role: "coordinator", Backend: pk.Backend, Model: pk.Model, PromptRef: ref},
		StagePayload{Project: proj.Slug, Effort: wanted[0].Effort})
	if err != nil {
		return err
	}
	return e.noteCoordinator(ctx, SystemActor, eventCoordinatorStart,
		map[string]any{"stage": st.ID, "backend": pk.Backend, "model": pk.Model, "items": len(wanted)})
}

// coordinatorDue says whether a check may start now: the period since the last
// check that happened has passed, and the day's run cap is not used up. last is
// the newest check that happened, an unavailable check or a run that was made; a
// run that could not be made (an infra failure) is not a check, so the next tick
// tries the next candidate instead of waiting out the period.
func coordinatorDue(set store.Settings, evs []store.Event, now time.Time) bool {
	if set.CoordinatorCheckMinutes <= 0 {
		return false
	}
	cadence := time.Duration(set.CoordinatorCheckMinutes) * time.Minute
	var last time.Time
	starts := 0
	for _, ev := range evs {
		switch ev.Action {
		case eventCoordinatorStart:
			if now.Sub(ev.TS) < 24*time.Hour {
				starts++
			}
		case eventCoordinatorUnavailable:
			if ev.TS.After(last) {
				last = ev.TS
			}
		case eventCoordinatorRun:
			var d coordinatorRunEvent
			if json.Unmarshal([]byte(ev.DetailJSON), &d) == nil && !d.Infra && ev.TS.After(last) {
				last = ev.TS
			}
		}
	}
	if !last.IsZero() && now.Sub(last) < cadence {
		return false
	}
	return set.CoordinatorRunsPerDay <= 0 || starts < set.CoordinatorRunsPerDay
}

// coordinatorRunOpen reports whether a started run's stage has not finished.
func (e *Engine) coordinatorRunOpen(ctx context.Context, evs []store.Event) (bool, error) {
	for i := len(evs) - 1; i >= 0; i-- {
		if evs[i].Action != eventCoordinatorStart {
			continue
		}
		var d struct {
			Stage int64 `json:"stage"`
		}
		if json.Unmarshal([]byte(evs[i].DetailJSON), &d) != nil || d.Stage == 0 {
			continue
		}
		st, err := e.deps.Store.GetStage(ctx, d.Stage)
		if err != nil {
			if err == store.ErrNotFound {
				continue
			}
			return false, err
		}
		return st.State != model.StageDone, nil
	}
	return false, nil
}

// pickCoordinator is the run's candidate picker: the role's list (project first,
// then global), or the code-level default, walked past the models that are down,
// resting or benched. reason says why no candidate is eligible.
func (e *Engine) pickCoordinator(ctx context.Context, p store.Project) (Pick, string, error) {
	v, err := e.newView(ctx)
	if err != nil {
		return Pick{}, "", err
	}
	list := v.candidates("coordinator", p.Slug)
	if len(list) == 0 {
		list = v.defaultCoordinatorCandidates()
	}
	if len(list) == 0 {
		return Pick{}, "no coordinator candidates are configured and no claude or beam model is served by a backend", nil
	}
	var reasons []string
	for _, c := range list {
		if why := skipReason(v, p, c, false); why != "" {
			reasons = append(reasons, why)
			continue
		}
		if v.down[c.Backend] {
			reasons = append(reasons, "backend "+c.Backend+" is down")
			continue
		}
		if until := restTime(v, c); !until.IsZero() {
			reasons = append(reasons, restReason(c, until, restWhy(v, c)))
			continue
		}
		return Pick{Backend: c.Backend, Model: c.Model}, "", nil
	}
	return Pick{}, noEligible("coordinator", reasons), nil
}

// defaultCoordinatorCandidates is defaultCoordinatorModels as candidates over the
// backends that serve them, in the order listed.
func (v *view) defaultCoordinatorCandidates() []store.Candidate {
	var out []store.Candidate
	for _, want := range defaultCoordinatorModels {
		for _, b := range v.snap.Backends {
			if b.Retired {
				continue
			}
			for _, m := range b.Models {
				if !strings.Contains(strings.ToLower(m.Model), strings.ToLower(want)) {
					continue
				}
				out = append(out, store.Candidate{Scope: "global", Role: "coordinator", Backend: b.Name, Model: m.Model})
			}
		}
	}
	return out
}

// applyCoordinatorRuns applies the actions of every finished run that has none
// applied yet. It is derived from the audit log, so a restart applies a run that
// finished while the engine was down.
func (e *Engine) applyCoordinatorRuns(ctx context.Context) error {
	evs, err := e.coordinatorEvents(ctx)
	if err != nil {
		return err
	}
	applied := map[int64]bool{}
	var starts []store.Event
	for _, ev := range evs {
		switch ev.Action {
		case eventCoordinatorRun:
			var d coordinatorRunEvent
			if json.Unmarshal([]byte(ev.DetailJSON), &d) == nil && d.Stage != 0 {
				applied[d.Stage] = true
			}
		case eventCoordinatorStart:
			starts = append(starts, ev)
		}
	}
	if len(starts) > coordinatorRunLookback {
		starts = starts[len(starts)-coordinatorRunLookback:]
	}
	for _, ev := range starts {
		var d struct {
			Stage int64 `json:"stage"`
		}
		if json.Unmarshal([]byte(ev.DetailJSON), &d) != nil || d.Stage == 0 || applied[d.Stage] {
			continue
		}
		st, err := e.deps.Store.GetStage(ctx, d.Stage)
		if err != nil {
			if err == store.ErrNotFound {
				continue
			}
			return err
		}
		if st.State != model.StageDone {
			continue // still running; the wait is the runner's timeout
		}
		if err := e.applyCoordinatorRun(ctx, st); err != nil {
			return err
		}
	}
	return nil
}

// applyCoordinatorRun applies one finished run's actions and records what it did.
func (e *Engine) applyCoordinatorRun(ctx context.Context, st model.Stage) error {
	actor := "coordinator:" + st.Model
	rep := coordinatorRunEvent{Stage: st.ID, Backend: st.Backend, Model: st.Model}
	record := func() error {
		return e.noteCoordinator(ctx, actor, eventCoordinatorRun, rep)
	}
	if IsInfra(st.Outcome) {
		rep.Infra = true
		rep.Summary = "the run could not be made: " + st.Outcome
		return record()
	}
	if st.Outcome != OutcomePass {
		rep.Summary = "the run's reply could not be used: " + st.Outcome
		return record()
	}
	out, err := schema.Parse[schema.CoordinatorOut](st.Parsed)
	if err != nil {
		rep.Summary = "the run's reply could not be read: " + err.Error()
		return record()
	}
	set, err := e.deps.Store.GetSettings(ctx)
	if err != nil {
		return err
	}
	rep.Summary = out.Summary
	rep.Questions = out.Questions
	done := 0
	for _, a := range out.Actions {
		target := a.Task
		if target == "" && a.Effort != 0 {
			target = strconv.FormatInt(a.Effort, 10)
		}
		if schema.CoordinatorForbidden(a.Kind) {
			// stays with the owner: refused, and asked as a question
			rep.Refused = append(rep.Refused, coordinatorOutcome{Kind: a.Kind, Target: target, Detail: a.Reason})
			rep.Questions = append(rep.Questions, schema.CoordinatorQuestion{
				Question: fmt.Sprintf("The coordinator asked to %s %s: %s. Do it yourself?", a.Kind, coordinatorTarget(a), a.Reason),
				Options:  []string{"do it", "leave it"},
			})
			continue
		}
		if set.CoordinatorActionsPerRun > 0 && done >= set.CoordinatorActionsPerRun {
			rep.Skipped++
			continue
		}
		if err := e.applyCoordinatorAction(ctx, actor, a); err != nil {
			rep.Failed = append(rep.Failed, coordinatorOutcome{Kind: a.Kind, Target: target, Detail: err.Error()})
			continue
		}
		done++
		rep.Applied = append(rep.Applied, coordinatorOutcome{Kind: a.Kind, Target: target, Detail: a.Reason})
	}
	return record()
}

// coordinatorTarget names an action's subject for a question to the owner.
func coordinatorTarget(a schema.CoordinatorAction) string {
	switch {
	case a.Project != "" && a.Task != "":
		return a.Project + "/" + a.Task
	case a.Effort != 0:
		return "effort " + strconv.FormatInt(a.Effort, 10)
	}
	return "the service"
}

// applyCoordinatorAction carries out one action through the same engine call the
// HTTP API makes, so the store audits it with the run's model as the actor.
func (e *Engine) applyCoordinatorAction(ctx context.Context, actor string, a schema.CoordinatorAction) error {
	switch a.Kind {
	case "allow_files":
		t, _, err := e.coordinatorTask(ctx, a.Project, a.Task)
		if err != nil {
			return err
		}
		_, err = e.AllowFiles(ctx, actor, t.ID, a.Files)
		return err
	case "retry":
		t, _, err := e.coordinatorTask(ctx, a.Project, a.Task)
		if err != nil {
			return err
		}
		return e.Retry(ctx, actor, t.ID, a.Clean)
	case "override":
		t, _, err := e.coordinatorTask(ctx, a.Project, a.Task)
		if err != nil {
			return err
		}
		return e.Override(ctx, actor, t.ID, a.Reason)
	case "note":
		t, ef, err := e.coordinatorTask(ctx, a.Project, a.Task)
		if err != nil {
			return err
		}
		def := model.TaskDef{Key: t.Key, Phase: t.Phase, Title: t.Title, Prompt: t.Prompt,
			Acceptance: t.Acceptance, FilesTouched: t.FilesTouched, DependsOn: t.DependsOn, Toolchains: t.Toolchains}
		def.Prompt += fmt.Sprintf("\n\nCOORDINATOR NOTE (%s, %s, for the worker AND the reviewer): %s", actor, e.deps.Clock().Format("01-02 15:04"), a.Note)
		_, err = e.EditTaskAndResume(ctx, actor, ef.ID, def)
		return err
	case "budget":
		return e.raiseCoordinatorBudget(ctx, actor, a)
	case "park":
		_, err := e.Park(ctx, actor, a.Effort, nil, nil)
		return err
	case "unpark":
		_, err := e.Unpark(ctx, actor, a.Effort)
		return err
	case "approve_queue":
		q, err := e.deps.Store.GetQueue(ctx, a.Effort)
		if err != nil {
			return err
		}
		_, err = e.ApproveQueue(ctx, actor, a.Effort, q.Hash)
		return err
	}
	return fmt.Errorf("action %q is not one the coordinator may make", a.Kind)
}

// raiseCoordinatorBudget raises an effort's token budget. It may only go up, and
// at most double the current limit: a runaway budget is worse than a halted
// effort.
func (e *Engine) raiseCoordinatorBudget(ctx context.Context, actor string, a schema.CoordinatorAction) error {
	ef, err := e.deps.Store.GetEffort(ctx, a.Effort)
	if err != nil {
		return err
	}
	if ef.BudgetTokens <= 0 {
		return fmt.Errorf("effort %d has no token budget to raise: %w", a.Effort, store.ErrConflict)
	}
	if a.Tokens <= ef.BudgetTokens {
		return fmt.Errorf("effort %d's budget is %d; a raise must be above it: %w", a.Effort, ef.BudgetTokens, store.ErrConflict)
	}
	if a.Tokens > 2*ef.BudgetTokens {
		return fmt.Errorf("effort %d's budget is %d; at most %d in one raise: %w", a.Effort, ef.BudgetTokens, 2*ef.BudgetTokens, store.ErrConflict)
	}
	tokens := a.Tokens
	_, err = e.deps.Store.UpdateEffort(ctx, actor, a.Effort, store.EffortPatch{BudgetTokens: &tokens})
	return err
}

// coordinatorTask finds a task by its project and key in the project's active
// effort.
func (e *Engine) coordinatorTask(ctx context.Context, project, key string) (model.Task, model.Effort, error) {
	ef, err := e.deps.Store.ActiveEffort(ctx, project)
	if err != nil {
		return model.Task{}, model.Effort{}, err
	}
	tasks, err := e.deps.Store.ListTasks(ctx, ef.ID)
	if err != nil {
		return model.Task{}, model.Effort{}, err
	}
	for _, t := range tasks {
		if t.Key == key {
			return t, ef, nil
		}
	}
	return model.Task{}, model.Effort{}, fmt.Errorf("task %s in %s's active effort: %w", key, project, store.ErrNotFound)
}

// coordinatorQuestionResolved reports whether a question's exact task reference
// names a task that is passed or overridden, so the question is no longer the
// owner's to answer. Only a complete reference can resolve: a legacy question
// stays until it is acknowledged, and a reference that does not resolve to a
// task in the named effort keeps the question visible instead of hiding it.
func (e *Engine) coordinatorQuestionResolved(ctx context.Context, q schema.CoordinatorQuestion) bool {
	if !q.Ref() {
		return false
	}
	ef, err := e.deps.Store.GetEffort(ctx, q.Effort)
	if err != nil || ef.Project != q.Project {
		return false
	}
	statuses, err := e.TaskStatuses(ctx, ef.Project, ef.ID)
	if err != nil {
		return false
	}
	st, ok := statuses[q.Task]
	return ok && st.Done()
}

// coordinatorAttention is the coordinator's own items for the owner: a check
// that could not find a model to run on, and the decisions the newest run left
// with the owner.
func (e *Engine) coordinatorAttention(ctx context.Context) []AttentionItem {
	evs, err := e.coordinatorEvents(ctx)
	if err != nil {
		return nil
	}
	out := []AttentionItem{}
	var lastRun, lastUnavailable *store.Event
	for i := range evs {
		ev := evs[i]
		switch ev.Action {
		case eventCoordinatorRun:
			lastRun = &evs[i]
		case eventCoordinatorUnavailable:
			lastUnavailable = &evs[i]
		}
	}
	if lastRun != nil {
		var d coordinatorRunEvent
		if json.Unmarshal([]byte(lastRun.DetailJSON), &d) == nil && len(d.Questions) > 0 {
			var asked []string
			for _, q := range d.Questions {
				if e.coordinatorQuestionResolved(ctx, q) {
					continue
				}
				asked = append(asked, q.Label())
			}
			if len(asked) > 0 {
				out = append(out, AttentionItem{Kind: "coordinator_question", Since: lastRun.TS.UnixMilli(),
					Message: fmt.Sprintf("The coordinator (%s) asked: %s", d.Model, strings.Join(asked, " "))})
			}
		}
	}
	if lastUnavailable != nil && (lastRun == nil || lastUnavailable.ID > lastRun.ID) {
		var d struct {
			Reason string `json:"reason"`
		}
		_ = json.Unmarshal([]byte(lastUnavailable.DetailJSON), &d)
		out = append(out, AttentionItem{Kind: "coordinator_unavailable", Since: lastUnavailable.TS.UnixMilli(),
			Message: "No model could run the coordinator check: " + d.Reason})
	}
	return out
}
