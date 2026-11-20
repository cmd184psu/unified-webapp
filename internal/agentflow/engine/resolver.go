package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"cmd184psu/unified-webapp/internal/agentflow/model"
	"cmd184psu/unified-webapp/internal/agentflow/prompts"
	"cmd184psu/unified-webapp/internal/agentflow/schema"
	"cmd184psu/unified-webapp/internal/agentflow/store"
)

// The resolver (AF-13 3b, the owner's idea). Overnight coord-fs3 unstuck each exhausted task by
// hand: it read the evidence and amended the task. A task that halts (stuck: the same failure
// twice; exhausted; or a worker that is blocked or needs files) now goes to a resolver first: a
// reviewer-role stage of the effort (prompt "resolver") that reads the task, its requirement,
// the last attempts and the kept worktree, and decides one way through. The engine applies the
// decision and retries the task in the same worktree; verify and a reviewer still have to pass
// it. One resolver run per failure streak: a second streak after a decision goes to the owner.
//
// The resolver's candidates are the role "resolver" (per project like any role); without any,
// a reviewer candidate running DefaultResolverModel serves; without that the resolver is off and
// a halted task goes to the owner as before.

// DefaultResolverModel is the model a resolver runs on when the project lists no resolver.
const DefaultResolverModel = "claude-sonnet-5-5"

const (
	resolverRef         = "resolver:"
	eventTaskResolved   = "task.resolved"
	resolverInfraStrike = 2
)

func isResolver(st model.Stage) bool {
	return st.Role == "reviewer" && strings.HasPrefix(st.PromptRef, resolverRef)
}

// resolverTarget reads "resolver:<task key>:<last attempt id>".
func resolverTarget(ref string) (key string, after int64) {
	rest := strings.TrimPrefix(ref, resolverRef)
	i := strings.LastIndexByte(rest, ':')
	if i < 0 {
		return rest, 0
	}
	after, _ = strconv.ParseInt(rest[i+1:], 10, 64)
	return rest[:i], after
}

func parseResolver(final string) (json.RawMessage, string, error) {
	raw, err := schema.ExtractFinal(final)
	if err != nil {
		return nil, "", err
	}
	if _, err := schema.Parse[schema.ResolverOut](raw); err != nil {
		return nil, "", err
	}
	return raw, OutcomePass, nil
}

// resolverPick is the backend and model a resolver runs on, avoiding the model of the task's
// last reviewer where another is eligible; ok false when no resolver is configured.
func (e *Engine) resolverPick(ctx context.Context, p store.Project, lastReviewer string) (Pick, string, bool, error) {
	v, err := e.newView(ctx)
	if err != nil {
		return Pick{}, "", false, err
	}
	others := map[string]string{}
	if lastReviewer != "" {
		others["the task's reviewer"] = lastReviewer
	}
	if len(v.candidates("resolver", p.Slug)) > 0 {
		pk, _, reason, err := e.pickEffortStage(ctx, p, "resolver", others, "")
		return pk, reason, true, err
	}
	var avoid []string
	has := false
	for _, c := range v.candidates("reviewer", p.Slug) {
		if c.Model == DefaultResolverModel {
			has = true
		} else {
			avoid = append(avoid, avoidKey(c.Backend, c.Model))
		}
	}
	if !has {
		return Pick{}, "", false, nil
	}
	pk, _, reason, err := e.pickEffortStage(ctx, p, "reviewer", others, strings.Join(avoid, "\x01"))
	return pk, reason, true, err
}

// resolverMode is the project's resolver_mode (off, advise, auto).
func (e *Engine) resolverMode(ctx context.Context, p store.Project) string {
	set, err := e.deps.Store.EffectiveSettings(ctx, p.Slug)
	if err != nil {
		return store.ResolverOff
	}
	return set.ResolverMode
}

// resolverOn reports whether the project has a resolver and its mode is not off.
func (e *Engine) resolverOn(ctx context.Context, p store.Project) bool {
	if e.resolverMode(ctx, p) == store.ResolverOff {
		return false
	}
	_, _, ok, err := e.resolverPick(ctx, p, "")
	return err == nil && ok
}

// resolvePass sends each halted task of a running effort to the resolver, applies finished
// decisions, and hands a task to the owner after a second streak.
func (e *Engine) resolvePass(ctx context.Context, proj store.Project, ef model.Effort, status map[string]model.Status) error {
	if ef.Status == model.EffortPaused {
		return nil // a paused effort starts nothing; the pass runs again when it resumes
	}
	var halted []string
	for k, st := range status {
		if st == model.StatusStuck || st == model.StatusExhausted || st == model.StatusNeedsInput {
			halted = append(halted, k)
		}
	}
	if len(halted) == 0 || !e.resolverOn(ctx, proj) {
		return nil
	}
	tasks, err := e.deps.Store.ListTasks(ctx, ef.ID)
	if err != nil {
		return err
	}
	byKey := map[string]model.Task{}
	for _, t := range tasks {
		byKey[t.Key] = t
	}
	stages, err := e.deps.Store.ListEffortStages(ctx, ef.ID)
	if err != nil {
		return err
	}
	decided, err := e.effortEvents(ctx, ef.ID, eventTaskResolved)
	if err != nil {
		return err
	}
	for _, key := range halted {
		t, ok := byKey[key]
		if !ok {
			continue
		}
		if err := e.resolveTask(ctx, proj, ef, t, stages, decided); err != nil {
			return err
		}
	}
	return nil
}

type resolvedEvent struct {
	Task         string `json:"task"`
	Decision     string `json:"decision"`
	Reason       string `json:"reason"`
	Stage        int64  `json:"stage"`
	AfterAttempt int64  `json:"after_attempt"`
	Model        string `json:"model,omitempty"`
	Backend      string `json:"backend,omitempty"`
	Auto         bool   `json:"auto,omitempty"`
	// Advised: resolver_mode advise recorded the decision for the owner; Applied: it was
	// carried out (at once in auto mode, or when the owner applied the advice).
	Advised bool `json:"advised,omitempty"`
	Applied bool `json:"applied,omitempty"`
}

func readResolved(evs []store.Event, key string) []resolvedEvent {
	var out []resolvedEvent
	for _, ev := range evs {
		var d resolvedEvent
		if json.Unmarshal([]byte(ev.DetailJSON), &d) == nil && d.Task == key {
			out = append(out, d)
		}
	}
	return out
}

func (e *Engine) resolveTask(ctx context.Context, proj store.Project, ef model.Effort, t model.Task, stages []model.Stage, evs []store.Event) error {
	atts, err := e.deps.Store.ListAttempts(ctx, t.ID)
	if err != nil || len(atts) == 0 {
		return err
	}
	last := atts[len(atts)-1]
	ref := fmt.Sprintf("%s%s:%d", resolverRef, t.Key, last.ID)
	past := readResolved(evs, t.Key)
	for _, d := range past {
		if d.AfterAttempt == last.ID {
			return nil // this streak is decided (an escalation stays with the owner)
		}
	}
	var mine []model.Stage
	for _, st := range stages {
		if isResolver(st) && st.PromptRef == ref {
			mine = append(mine, st)
		}
	}
	for _, st := range mine {
		if st.State != model.StageDone {
			return nil // in flight
		}
	}
	escalate := func(reason string) error {
		if err := e.deps.Store.SetWaitingReason(ctx, t.ID, "needs a human (resolver): "+reason); err != nil {
			return err
		}
		return e.noteEffort(ctx, SystemActor, eventTaskResolved, ef.ID, resolvedEvent{Task: t.Key, Decision: "escalate", Reason: reason, AfterAttempt: last.ID, Auto: true})
	}
	if len(mine) == 0 {
		for _, d := range past {
			if d.Decision != "escalate" && d.Applied {
				return escalate(fmt.Sprintf("it failed again after the resolver's %s decision (%s)", d.Decision, clip(d.Reason, 200)))
			}
		}
		return e.startResolver(ctx, proj, ef, t, last, ref, "")
	}
	st := mine[len(mine)-1]
	if IsInfra(st.Outcome) {
		if infraRun(mine) >= resolverInfraStrike {
			return escalate("the resolver could not run: " + st.Outcome + " on " + st.Backend)
		}
		return e.startResolver(ctx, proj, ef, t, last, ref, avoidKey(st.Backend, st.Model))
	}
	if st.Outcome != OutcomePass {
		return escalate("the resolver's reply could not be used (" + st.Outcome + ")")
	}
	out, err := schema.Parse[schema.ResolverOut](st.Parsed)
	if err != nil {
		return escalate("the resolver's reply could not be read: " + err.Error())
	}
	if e.resolverMode(ctx, proj) != store.ResolverAuto && out.Decision != "escalate" {
		// advise (the default): record the decision for the owner; the task stays halted
		// until it is applied (POST .../tasks/{key}/resolution/apply).
		by := st.Model + " on " + st.Backend
		if err := e.deps.Store.SetWaitingReason(ctx, t.ID, fmt.Sprintf("the resolver (%s) advises %s: %s. Apply it from the task page.", by, out.Decision, out.Reason)); err != nil {
			return err
		}
		return e.noteEffort(ctx, SystemActor, eventTaskResolved, ef.ID, resolvedEvent{Task: t.Key, Decision: out.Decision, Reason: out.Reason, Stage: st.ID,
			AfterAttempt: last.ID, Model: st.Model, Backend: st.Backend, Advised: true})
	}
	return e.applyResolution(ctx, ef, t, last, st, out)
}

// ApplyResolution carries out the resolver's advice on a halted task (resolver_mode advise):
// the newest decision recorded for its current failure streak, not applied yet.
func (e *Engine) ApplyResolution(ctx context.Context, actor string, task int64) (Resolution, error) {
	_, ef, t, err := e.locateTask(ctx, task)
	if err != nil {
		return Resolution{}, err
	}
	atts, err := e.deps.Store.ListAttempts(ctx, t.ID)
	if err != nil {
		return Resolution{}, err
	}
	if len(atts) == 0 {
		return Resolution{}, fmt.Errorf("task %s has no resolver advice: %w", t.Key, store.ErrConflict)
	}
	last := atts[len(atts)-1]
	evs, err := e.effortEvents(ctx, ef.ID, eventTaskResolved)
	if err != nil {
		return Resolution{}, err
	}
	var adv *resolvedEvent
	for _, d := range readResolved(evs, t.Key) {
		d := d
		if d.AfterAttempt != last.ID {
			continue
		}
		if d.Applied {
			adv = nil
			continue
		}
		if d.Advised {
			adv = &d
		}
	}
	if adv == nil {
		return Resolution{}, fmt.Errorf("task %s has no resolver advice waiting to be applied: %w", t.Key, store.ErrConflict)
	}
	st, err := e.deps.Store.GetStage(ctx, adv.Stage)
	if err != nil {
		return Resolution{}, err
	}
	out, err := schema.Parse[schema.ResolverOut](st.Parsed)
	if err != nil {
		return Resolution{}, err
	}
	if err := e.applyResolution(ctx, ef, t, last, st, out); err != nil {
		return Resolution{}, err
	}
	return Resolution{Decision: out.Decision, Reason: out.Reason, Model: st.Model, Backend: st.Backend, Stage: st.ID, Applied: true}, nil
}

func (e *Engine) startResolver(ctx context.Context, proj store.Project, ef model.Effort, t model.Task, last model.Attempt, ref, avoid string) error {
	pk, reason, ok, err := e.resolverPick(ctx, proj, last.ReviewerModel)
	if err != nil || !ok {
		return err
	}
	if reason != "" && avoid == "" {
		return e.deps.Store.SetWaitingReason(ctx, t.ID, "waiting for a resolver: "+reason)
	}
	_, _, err = e.Submit(ctx, BackendLane(pk.Backend),
		store.StageSpec{Effort: ef.ID, Role: "reviewer", Backend: pk.Backend, Model: pk.Model, PromptRef: ref},
		StagePayload{Project: proj.Slug, Effort: ef.ID})
	return err
}

// applyResolution carries out one decision and, unless it escalates, retries the task in its
// worktree.
func (e *Engine) applyResolution(ctx context.Context, ef model.Effort, t model.Task, last model.Attempt, st model.Stage, out schema.ResolverOut) error {
	s := e.deps.Store
	by := st.Model + " on " + st.Backend
	ev := resolvedEvent{Task: t.Key, Decision: out.Decision, Reason: out.Reason, Stage: st.ID, AfterAttempt: last.ID, Model: st.Model, Backend: st.Backend, Applied: out.Decision != "escalate"}
	evidence := strings.Join(out.Evidence, ", ")
	var err error
	switch out.Decision {
	case "clarify":
		err = s.AmendTaskPrompt(ctx, SystemActor, t.ID, fmt.Sprintf("Clarification (resolver %s, evidence %s): %s", by, evidence, out.Clarification))
	case "override_finding":
		err = s.AmendTaskPrompt(ctx, SystemActor, t.ID, fmt.Sprintf("Out of scope for this task (resolver %s, evidence %s): the review finding %q. %s A reviewer must not fail this task on it.", by, evidence, out.Finding, out.Reason))
	case "allow_files":
		var files []string
		for _, f := range out.Files {
			files = append(files, strings.TrimPrefix(strings.TrimSpace(f.Path), "./"))
		}
		err = s.AllowTaskFiles(ctx, SystemActor, t.ID, files)
	case "split":
		defs := make([]model.TaskDef, 0, len(out.Tasks))
		for i, nt := range out.Tasks {
			key := nt.ID
			if key == "" || key == t.Key {
				key = fmt.Sprintf("%s-S%d", t.Key, i+1)
			}
			defs = append(defs, model.TaskDef{Key: key, Phase: t.Phase, Title: nt.Title, Prompt: nt.Prompt, Acceptance: nt.Acceptance, FilesTouched: nt.FilesTouched, Toolchains: nt.Toolchains})
		}
		if err = s.AddTasks(ctx, SystemActor, ef.ID, defs, "queue.split", map[string]any{"task": t.Key, "stage": st.ID}); err == nil {
			var keys []string
			for _, d := range defs {
				keys = append(keys, d.Key)
			}
			err = s.AmendTaskPrompt(ctx, SystemActor, t.ID, fmt.Sprintf("Split (resolver %s): tasks %s now do the rest; this task does only what its acceptance names.", by, strings.Join(keys, ", ")))
		}
	case "escalate":
		if err := s.SetWaitingReason(ctx, t.ID, "needs a human (resolver "+by+"): "+out.Reason); err != nil {
			return err
		}
		return e.noteEffort(ctx, SystemActor, eventTaskResolved, ef.ID, ev)
	}
	if err != nil {
		// A decision the store refuses (a split whose tasks do not validate) is the owner's.
		ev.Decision, ev.Reason, ev.Auto, ev.Applied = "escalate", fmt.Sprintf("the resolver's %s decision could not be applied: %v", out.Decision, err), true, false
		if err := s.SetWaitingReason(ctx, t.ID, "needs a human (resolver): "+ev.Reason); err != nil {
			return err
		}
		return e.noteEffort(ctx, SystemActor, eventTaskResolved, ef.ID, ev)
	}
	if err := e.noteEffort(ctx, SystemActor, eventTaskResolved, ef.ID, ev); err != nil {
		return err
	}
	if err := s.AppendProgress(ctx, t.ID, last.ID, fmt.Sprintf("Resolved by %s: %s. %s", by, out.Decision, out.Reason)); err != nil {
		return err
	}
	return e.Retry(ctx, SystemActor, t.ID, false)
}

// resolverPrompt fills the resolver prompt for the task its ref names.
func (e *Engine) resolverPrompt(ctx context.Context, st model.Stage) (string, error) {
	text, err := prompts.Load("resolver")
	if err != nil {
		return "", err
	}
	key, _ := resolverTarget(st.PromptRef)
	tasks, err := e.deps.Store.ListTasks(ctx, st.Effort)
	if err != nil {
		return "", err
	}
	var t model.Task
	for _, x := range tasks {
		if x.Key == key {
			t = x
		}
	}
	if t.ID == 0 {
		return "", fmt.Errorf("resolver stage %d: task %s not found", st.ID, key)
	}
	tj, _ := json.Marshal(t.TaskDef)
	atts, err := e.deps.Store.ListAttempts(ctx, t.ID)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	from := len(atts) - 3
	if from < 0 {
		from = 0
	}
	for _, a := range atts[from:] {
		fmt.Fprintf(&b, "### Attempt %d (%s): %s %s\n\n", a.N, a.WorkerModel, a.Outcome, a.Fingerprint)
		stages, err := e.deps.Store.ListAttemptStages(ctx, a.ID)
		if err != nil {
			return "", err
		}
		for _, s := range stages {
			if len(s.Parsed) == 0 {
				continue
			}
			fmt.Fprintf(&b, "%s (%s, %s):\n```json\n%s\n```\n\n", s.Role, s.Model, s.Outcome, clip(string(s.Parsed), 6000))
		}
	}
	diff := "(no kept worktree)"
	refs := "(none listed)"
	if ef, err := e.deps.Store.GetEffort(ctx, st.Effort); err == nil {
		if proj, err := e.deps.Store.GetProject(ctx, ef.Project); err == nil {
			if len(atts) > 0 && e.deps.Git != nil {
				if base, err := os.ReadFile(e.baseCommitFile(t.ID)); err == nil {
					if d, err := e.deps.Git.Diff(ctx, proj.RepoPath, atts[len(atts)-1].Worktree, strings.TrimSpace(string(base)), maxRepairDiff); err == nil {
						diff = "```diff\n" + d + "\n```"
					}
				}
			}
			if r := referenceTrees(proj); r != "" {
				refs = r
			}
		}
	}
	rep := strings.NewReplacer("{{TASK_JSON}}", string(tj), "{{FRD_SECTION}}", e.frdSection(ctx, t), "{{ATTEMPTS}}", b.String(), "{{DIFF}}", diff, "{{REFERENCES}}", refs)
	return rep.Replace(text), nil
}

// referenceTrees are the read-only trees the project lists: its read-only docker_mounts (for
// fs3 the OpenCiv3 source and ~/work/re).
func referenceTrees(p store.Project) string {
	var lines []string
	for _, m := range p.DockerMounts {
		parts := strings.Split(m, ":")
		if len(parts) >= 2 && (len(parts) == 2 || parts[2] != "rw") {
			lines = append(lines, "- `"+parts[0]+"`")
		}
	}
	return strings.Join(lines, "\n")
}

// Resolution is one resolver decision on a task, for its detail pane.
type Resolution struct {
	Decision string `json:"decision"`
	Reason   string `json:"reason"`
	Model    string `json:"model,omitempty"`
	Backend  string `json:"backend,omitempty"`
	Stage    int64  `json:"stage,omitempty"`
	Auto     bool   `json:"auto,omitempty"`
	// Pending: advice waiting for the owner to apply it; Applied: carried out.
	Pending bool `json:"pending,omitempty"`
	Applied bool `json:"applied,omitempty"`
}

// Resolutions lists the resolver's decisions on a task, oldest first.
func (e *Engine) Resolutions(ctx context.Context, effort int64, key string) ([]Resolution, error) {
	evs, err := e.effortEvents(ctx, effort, eventTaskResolved)
	if err != nil {
		return nil, err
	}
	out := []Resolution{}
	at := map[int64]int{} // stage -> index: an application replaces the advice it applied
	for _, r := range readResolved(evs, key) {
		res := Resolution{Decision: r.Decision, Reason: r.Reason, Model: r.Model, Backend: r.Backend, Stage: r.Stage, Auto: r.Auto, Pending: r.Advised && !r.Applied, Applied: r.Applied}
		if i, ok := at[r.Stage]; ok && r.Stage != 0 {
			out[i] = res
			continue
		}
		if r.Stage != 0 {
			at[r.Stage] = len(out)
		}
		out = append(out, res)
	}
	return out, nil
}
