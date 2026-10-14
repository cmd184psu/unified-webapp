package engine

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"cmd184psu/unified-webapp/internal/agentflow/model"
	"cmd184psu/unified-webapp/internal/agentflow/prompts"
	"cmd184psu/unified-webapp/internal/agentflow/schema"
	"cmd184psu/unified-webapp/internal/agentflow/store"
	"cmd184psu/unified-webapp/internal/agentflow/verify"
)

// FRD drafting (P3). A draft, polish or split is one planner-role stage on the
// effort with its own prompt (prompts "drafter"). The stage is told apart from
// the plan loop's planner stages by its prompt_path marker ("frd:<purpose>"),
// which the stage row carries from the moment it is created. The request
// (description, max_parts) is an audit event written before the stage is
// submitted; its result is a second event, so the driver (advanceDraft, called
// from AdvancePlans) is idempotent like the plan loop's.

const (
	PurposeDraft  = "draft"
	PurposePolish = "polish"
	PurposeSplit  = "split"

	draftMarker      = "frd:"
	eventDraftStart  = "frd.draft"
	eventDraftDone   = "frd.draft.done"
	eventDraftFailed = "frd.draft.failed"

	// partSummaryLen caps the one-line summary kept with a split part.
	partSummaryLen = 160
)

// DraftRequest asks for one drafter stage. Description is the plain-language
// text for a draft, and optional notes for a polish or split.
type DraftRequest struct {
	Purpose     string
	Description string
	MaxParts    int
}

// DraftView is the state of the effort's latest drafter run.
type DraftView struct {
	State   string `json:"state"` // idle, running or failed
	Purpose string `json:"purpose,omitempty"`
	Started int64  `json:"started"`
	Error   string `json:"error"`
}

// draftDetail is the payload of the start event; After is the effort's newest
// stage id when it was written, which names the run (as plan.start does).
type draftDetail struct {
	Purpose     string `json:"purpose"`
	Description string `json:"description,omitempty"`
	MaxParts    int    `json:"max_parts,omitempty"`
	After       int64  `json:"after_stage"`
}

// draftPurpose is the purpose of a drafter stage, "" for any other stage.
func draftPurpose(st model.Stage) string {
	if st.Role != "planner" {
		return ""
	}
	p, _ := strings.CutPrefix(st.PromptRef, draftMarker)
	if p == st.PromptRef {
		return ""
	}
	return p
}

// parseDraft is parseFinal for a drafter stage.
func parseDraft(purpose, final string) (json.RawMessage, string, error) {
	raw, err := schema.ExtractFinal(final)
	if err != nil {
		return nil, "", err
	}
	if purpose == PurposeSplit {
		_, err = schema.Parse[schema.FRDSplit](raw)
	} else {
		_, err = schema.Parse[schema.FRDDraft](raw)
	}
	if err != nil {
		return nil, "", err
	}
	return raw, OutcomePass, nil
}

// draftRun is the newest drafter run of an effort: its request and the events
// that closed it.
type draftRun struct {
	start  draftDetail
	at     int64
	result *store.Event
	stages []model.Stage
}

func (e *Engine) latestDraftRun(ctx context.Context, effort int64) (*draftRun, error) {
	evs, err := e.effortEvents(ctx, effort, eventDraftStart)
	if err != nil || len(evs) == 0 {
		return nil, err
	}
	last := evs[len(evs)-1]
	run := &draftRun{at: last.TS.UnixMilli()}
	if err := json.Unmarshal([]byte(last.DetailJSON), &run.start); err != nil {
		return nil, err
	}
	// A result closes the run when it was written after the run's start event;
	// after_stage cannot tell runs apart, a failed run and its retry share it.
	for _, action := range []string{eventDraftDone, eventDraftFailed} {
		results, err := e.effortEvents(ctx, effort, action)
		if err != nil {
			return nil, err
		}
		for _, ev := range results {
			if ev.ID > last.ID && (run.result == nil || ev.ID > run.result.ID) {
				ev := ev
				run.result = &ev
			}
		}
	}
	all, err := e.deps.Store.ListEffortStages(ctx, effort)
	if err != nil {
		return nil, err
	}
	for _, st := range all {
		if st.ID > run.start.After && draftPurpose(st) != "" {
			run.stages = append(run.stages, st)
		}
	}
	return run, nil
}

// DraftState reports the effort's latest drafter run.
func (e *Engine) DraftState(ctx context.Context, effort int64) (DraftView, error) {
	run, err := e.latestDraftRun(ctx, effort)
	if err != nil || run == nil {
		return DraftView{State: "idle"}, err
	}
	v := DraftView{State: "running", Purpose: run.start.Purpose, Started: run.at}
	if run.result == nil {
		return v, nil
	}
	v.State = "idle"
	if run.result.Action == eventDraftFailed {
		var d struct {
			Reason string `json:"reason"`
		}
		_ = json.Unmarshal([]byte(run.result.DetailJSON), &d)
		v.State, v.Error = "failed", d.Reason
	}
	return v, nil
}

// StartDraft submits a drafter stage for the effort: it returns at once and
// the result lands as a new FRD revision (draft, polish) or as the effort's
// FRD plus stored parts (split). The effort must be a draft with no stage in
// flight; a second request while one runs is ErrConflict.
func (e *Engine) StartDraft(ctx context.Context, actor string, effort int64, req DraftRequest) error {
	e.planMu.Lock()
	defer e.planMu.Unlock()
	s := e.deps.Store
	ef, err := s.GetEffort(ctx, effort)
	if err != nil {
		return err
	}
	p, err := s.GetProject(ctx, ef.Project)
	if err != nil {
		return err
	}
	if ef.Status != model.EffortDraft {
		return fmt.Errorf("effort %d is %s, its FRD can only be drafted while it is a draft: %w", effort, ef.Status, store.ErrConflict)
	}
	switch req.Purpose {
	case PurposeDraft:
		if strings.TrimSpace(req.Description) == "" {
			return store.Invalid("description", "describe what you want in a few words")
		}
	case PurposePolish, PurposeSplit:
		if _, err := s.GetDoc(ctx, effort, model.DocFRD, 0); errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("effort %d has no FRD to %s: %w", effort, req.Purpose, store.ErrConflict)
		} else if err != nil {
			return err
		}
	default:
		return store.Invalid("purpose", "must be draft, polish or split")
	}
	if req.MaxParts < 0 {
		return store.Invalid("max_parts", "must be 0 (no limit) or more")
	}
	if cur, err := e.DraftState(ctx, effort); err != nil {
		return err
	} else if cur.State == "running" {
		return fmt.Errorf("effort %d is already being %sed: %w", effort, cur.Purpose, store.ErrConflict)
	}
	stages, err := s.ListEffortStages(ctx, effort)
	if err != nil {
		return err
	}
	for _, st := range stages {
		if st.State != model.StageDone {
			return fmt.Errorf("effort %d has a %s stage in flight: %w", effort, st.Role, store.ErrConflict)
		}
	}
	d := draftDetail{Purpose: req.Purpose, Description: strings.TrimSpace(req.Description), MaxParts: req.MaxParts}
	if len(stages) > 0 {
		d.After = stages[len(stages)-1].ID
	}
	pk, _, reason, err := e.pickEffortStage(ctx, p, "planner", nil, "")
	if err != nil {
		return err
	}
	if reason != "" {
		return fmt.Errorf("no model can draft right now: %s: %w", reason, store.ErrConflict)
	}
	if err := e.noteEffort(ctx, actor, eventDraftStart, effort, d); err != nil {
		return err
	}
	if err := e.submitDraftStage(ctx, p, effort, d.Purpose, pk); err != nil {
		_ = e.failDraft(ctx, effort, d.After, 0, err.Error())
		return err
	}
	return nil
}

func (e *Engine) submitDraftStage(ctx context.Context, p store.Project, effort int64, purpose string, pk Pick) error {
	_, _, err := e.Submit(ctx, BackendLane(pk.Backend),
		store.StageSpec{Effort: effort, Role: "planner", Backend: pk.Backend, Model: pk.Model, PromptRef: draftMarker + purpose},
		StagePayload{Project: p.Slug, Effort: effort})
	return err
}

// advanceDraft takes the next step of an effort's drafter run: it applies a
// passed stage's result, retries an infrastructure failure like the plan loop
// does, and closes the run as failed when the stage cannot produce an FRD.
// planMu is held.
func (e *Engine) advanceDraft(ctx context.Context, p store.Project, ef model.Effort) error {
	run, err := e.latestDraftRun(ctx, ef.ID)
	if err != nil || run == nil || run.result != nil {
		return err
	}
	for _, st := range run.stages {
		if st.State != model.StageDone {
			return nil // in flight
		}
	}
	retry := func(avoid string) error {
		pk, _, reason, err := e.pickEffortStage(ctx, p, "planner", nil, avoid)
		if err == nil && reason != "" && avoid != "" {
			pk, _, reason, err = e.pickEffortStage(ctx, p, "planner", nil, "")
		}
		if err != nil {
			return err
		}
		if reason != "" {
			return e.failDraft(ctx, ef.ID, run.start.After, 0, "no model can draft: "+reason)
		}
		return e.submitDraftStage(ctx, p, ef.ID, run.start.Purpose, pk)
	}
	if len(run.stages) == 0 {
		return retry("")
	}
	last := run.stages[len(run.stages)-1]
	if IsInfra(last.Outcome) {
		return e.effortInfra(ctx, p, run.stages, last, retry,
			func(reason string) error { return e.failDraft(ctx, ef.ID, run.start.After, last.ID, reason) })
	}
	if last.Outcome != OutcomePass {
		return e.failDraft(ctx, ef.ID, run.start.After, last.ID, "stage ended "+last.Outcome)
	}
	if run.start.Purpose == PurposeSplit {
		err = e.applySplit(ctx, p, ef, last, run.start.After)
	} else {
		err = e.applyDraft(ctx, p, ef, run.start.Purpose, last, run.start.After)
	}
	defer e.changed()
	if err != nil {
		return e.failDraft(ctx, ef.ID, run.start.After, last.ID, err.Error())
	}
	return nil
}

// failDraft closes the run as failed; the FRD is left as it was.
func (e *Engine) failDraft(ctx context.Context, effort, after, stage int64, reason string) error {
	defer e.changed()
	return e.noteEffort(ctx, SystemActor, eventDraftFailed, effort, map[string]any{"after_stage": after, "stage": stage, "reason": reason})
}

// definitionOfDone is the project's definition of done as seen from dir.
func definitionOfDone(p store.Project, dir string) verify.DoD {
	return verify.DefinitionOfDone(dir, p.FeedbackCmds, p.DefinitionOfDone)
}

// DefinitionOfDone returns the definition of done of a project, derived from
// the files in its repository unless the project overrides it.
func (e *Engine) DefinitionOfDone(ctx context.Context, slug string) (verify.DoD, error) {
	p, err := e.deps.Store.GetProject(ctx, slug)
	if err != nil {
		return verify.DoD{}, err
	}
	return definitionOfDone(p, p.RepoPath), nil
}

// applyDraft turns a passed draft or polish stage into a new FRD revision.
// Criteria that only restate the definition of done are dropped; ids, links,
// status and date are filled by SaveDoc. A polish keeps what the drafter does
// not write (constraints, decisions, the id) from the FRD it tidied.
func (e *Engine) applyDraft(ctx context.Context, p store.Project, ef model.Effort, purpose string, st model.Stage, after int64) error {
	out, err := schema.Parse[schema.FRDDraft](st.Parsed)
	if err != nil {
		return err
	}
	drafted := out.FRD
	dropped, keptAll := model.DropRestatedCriteria(&drafted, definitionOfDone(p, p.RepoPath).Items)
	f := model.FRD{Title: ef.Title}
	if purpose == PurposePolish {
		cur, err := e.deps.Store.GetDoc(ctx, ef.ID, model.DocFRD, 0)
		if err != nil {
			return err
		}
		if f, err = model.ParseFRD(cur.JSON); err != nil {
			return err
		}
		f.Status, f.ApprovedDate, f.Groups = "", "", nil
	}
	if strings.TrimSpace(drafted.Title) != "" {
		f.Title = drafted.Title
	}
	f.Objective, f.Requirements, f.AcceptanceCriteria = drafted.Objective, drafted.Requirements, drafted.AcceptanceCriteria
	if drafted.Context != "" {
		f.Context = drafted.Context
	}
	if len(drafted.OutOfScope) > 0 {
		f.OutOfScope = drafted.OutOfScope
	}
	raw, err := json.Marshal(f)
	if err != nil {
		return err
	}
	_, err = e.deps.Store.SaveDocWith(ctx, "drafter:"+st.Model, ef.ID, model.DocFRD, raw, func(tx *sql.Tx, d model.Doc) error {
		return doneEvent(tx, ef.ID, after, st.ID, map[string]any{"revision": d.Revision, "dropped": dropped, "kept_all_restated": keptAll})
	})
	return err
}

// applySplit turns a passed split stage into the effort's FRD (part 1, a new
// revision) and the stored parts. New parts are appended to the parts already
// stored. The revision, the parts and the done event commit together, so a
// crash cannot leave a half-applied split or apply it twice.
func (e *Engine) applySplit(ctx context.Context, p store.Project, ef model.Effort, st model.Stage, after int64) error {
	out, err := schema.Parse[schema.FRDSplit](st.Parsed)
	if err != nil {
		return err
	}
	dod := definitionOfDone(p, p.RepoPath).Items
	var dropped []string
	var keptAll bool
	var first []byte
	var parts []store.FRDPart
	for i, part := range out.Parts {
		f := part.FRD
		d, k := model.DropRestatedCriteria(&f, dod)
		dropped, keptAll = append(dropped, d...), keptAll || k
		raw, err := json.Marshal(f)
		if err != nil {
			return err
		}
		if i == 0 {
			first = raw
			continue
		}
		parts = append(parts, store.FRDPart{Title: f.Title, Summary: clip(f.Objective, partSummaryLen), JSON: raw, Tokens: estimateTokens(raw)})
	}
	actor := "drafter:" + st.Model
	_, err = e.deps.Store.SaveDocWith(ctx, actor, ef.ID, model.DocFRD, first, func(tx *sql.Tx, d model.Doc) error {
		if err := store.AddFRDPartsTx(ctx, tx, actor, ef.ID, parts); err != nil {
			return err
		}
		return doneEvent(tx, ef.ID, after, st.ID, map[string]any{"revision": d.Revision, "parts": len(out.Parts), "dropped": dropped, "kept_all_restated": keptAll})
	})
	return err
}

// doneEvent writes the run's result event inside the transaction that saved
// its revision.
func doneEvent(tx *sql.Tx, effort, after, stage int64, detail map[string]any) error {
	detail["after_stage"], detail["stage"] = after, stage
	_, err := store.AppendEvent(tx, SystemActor, eventDraftDone, "effort", strconv.FormatInt(effort, 10), detail)
	return err
}

// estimateTokens is the rough size of a document in model tokens.
func estimateTokens(raw []byte) int { return len(raw) / 4 }

// SizeModel is one model a planning role may use and its known context.
type SizeModel struct {
	Backend       string `json:"backend"`
	Model         string `json:"model"`
	ContextTokens int    `json:"context_tokens"`
}

// UnknownModel is a planning model whose context size is not known.
type UnknownModel struct {
	Backend string `json:"backend"`
	Model   string `json:"model"`
}

// SizeView compares the size of an effort's FRD and plan with what the
// planning models can read. A context of 0 is unknown; Over is only ever set
// when a context is known.
type SizeView struct {
	FRDTokens        int            `json:"frd_tokens"`
	PlanTokens       int            `json:"plan_tokens"`
	BudgetTokens     int            `json:"budget_tokens"`
	ContextTokensMin int            `json:"context_tokens_min"`
	Over             bool           `json:"over"`
	Models           []SizeModel    `json:"models"`
	UnknownModels    []UnknownModel `json:"unknown_models"`
	Advice           string         `json:"advice"`
}

// sizeRoles are the roles whose prompts carry the FRD or the plan.
var sizeRoles = []string{"planner", "architect", "critic", "compiler"}

// FRDSize estimates the FRD and plan of an effort in tokens (a quarter of
// their JSON length) and sets them against half the smallest known context
// among the models the planning roles may use for the effort's project: the
// other half is left for the repository, the instructions and the reply.
func (e *Engine) FRDSize(ctx context.Context, effort int64) (SizeView, error) {
	s := e.deps.Store
	ef, err := s.GetEffort(ctx, effort)
	if err != nil {
		return SizeView{}, err
	}
	v := SizeView{Models: []SizeModel{}, UnknownModels: []UnknownModel{}}
	for kind, dst := range map[string]*int{model.DocFRD: &v.FRDTokens, model.DocPlan: &v.PlanTokens} {
		d, err := s.GetDoc(ctx, effort, kind, 0)
		if errors.Is(err, store.ErrNotFound) {
			continue
		}
		if err != nil {
			return v, err
		}
		*dst = estimateTokens(compactBytes(d.JSON))
	}
	snap, err := s.Snapshot(ctx)
	if err != nil {
		return v, err
	}
	known := map[[2]string]int{}
	for _, b := range snap.Backends {
		for _, m := range b.Models {
			known[[2]string{b.Name, m.Model}] = m.ContextTokens
		}
	}
	seen := map[[2]string]bool{}
	for _, role := range sizeRoles {
		for _, c := range snap.CandidatesFor(role, ef.Project) {
			key := [2]string{c.Backend, c.Model}
			if seen[key] {
				continue
			}
			seen[key] = true
			ctxTokens := known[key]
			v.Models = append(v.Models, SizeModel{Backend: c.Backend, Model: c.Model, ContextTokens: ctxTokens})
			if ctxTokens == 0 {
				v.UnknownModels = append(v.UnknownModels, UnknownModel{Backend: c.Backend, Model: c.Model})
			}
			if ctxTokens > 0 && (v.ContextTokensMin == 0 || ctxTokens < v.ContextTokensMin) {
				v.ContextTokensMin = ctxTokens
			}
		}
	}
	sort.Slice(v.Models, func(i, j int) bool {
		a, b := v.Models[i], v.Models[j]
		return a.Backend+"/"+a.Model < b.Backend+"/"+b.Model
	})
	sort.Slice(v.UnknownModels, func(i, j int) bool {
		a, b := v.UnknownModels[i], v.UnknownModels[j]
		return a.Backend+"/"+a.Model < b.Backend+"/"+b.Model
	})
	v.BudgetTokens = v.ContextTokensMin / 2
	v.Over = v.BudgetTokens > 0 && (v.FRDTokens > v.BudgetTokens || v.PlanTokens > v.BudgetTokens)
	switch {
	case len(v.Models) == 0:
		v.Advice = "No model is set for the planning roles yet, so the size cannot be checked."
	case v.ContextTokensMin == 0:
		v.Advice = "No planning model has a known context size: set it on the backend model to get a size check."
	case v.PlanTokens > v.BudgetTokens:
		v.Advice = fmt.Sprintf("The plan is about %d tokens against a budget of %d: split the FRD into smaller parts.", v.PlanTokens, v.BudgetTokens)
	case v.FRDTokens > v.BudgetTokens:
		v.Advice = fmt.Sprintf("The FRD is about %d tokens against a budget of %d: split it into parts.", v.FRDTokens, v.BudgetTokens)
	default:
		v.Advice = fmt.Sprintf("Fits: the FRD is about %d tokens against a budget of %d.", v.FRDTokens, v.BudgetTokens)
	}
	if len(v.UnknownModels) > 0 && v.ContextTokensMin > 0 {
		names := make([]string, len(v.UnknownModels))
		for i, m := range v.UnknownModels {
			names[i] = m.Backend + "/" + m.Model
		}
		v.Advice += " The context size is unknown for " + strings.Join(names, ", ") + ", so the check does not cover them."
	}
	return v, nil
}

func compactBytes(raw []byte) []byte { return []byte(compactJSON(raw)) }

// draftPrompt builds the prompt of a drafter stage from the run's request,
// the effort's current FRD, the repository and the definition of done.
func (e *Engine) draftPrompt(ctx context.Context, st model.Stage, purpose string) (string, error) {
	text, err := prompts.Load("drafter")
	if err != nil {
		return "", err
	}
	s := e.deps.Store
	ef, err := s.GetEffort(ctx, st.Effort)
	if err != nil {
		return "", err
	}
	p, err := s.GetProject(ctx, ef.Project)
	if err != nil {
		return "", err
	}
	var req draftDetail
	evs, err := e.effortEvents(ctx, st.Effort, eventDraftStart)
	if err != nil {
		return "", err
	}
	for _, ev := range evs {
		var d draftDetail
		if json.Unmarshal([]byte(ev.DetailJSON), &d) == nil && d.After < st.ID {
			req = d
		}
	}
	frd := ""
	if d, err := s.GetDoc(ctx, st.Effort, model.DocFRD, 0); err == nil {
		frd = compactJSON(d.JSON)
	} else if !errors.Is(err, store.ErrNotFound) {
		return "", err
	}
	var task, input, sizeRule, note string
	switch purpose {
	case PurposeDraft:
		task = "Draft an FRD from the description below."
		input = "Description from the person:\n\n" + req.Description
		sizeRule = "For a short description write at most 6 requirements and no more than two criteria for each; write more only when the description clearly lists more separate things."
	case PurposePolish:
		task = "Tidy the existing FRD below: shorter plain wording, duplicates merged, boilerplate and anything that restates the definition of done removed. Keep its meaning and its scope."
		input = "Existing FRD:\n\n```json\n" + frd + "\n```"
		if req.Description != "" {
			input += "\n\nNotes from the person:\n\n" + req.Description
		}
		sizeRule = "Do not add requirements the FRD does not already ask for."
	case PurposeSplit:
		size, err := e.FRDSize(ctx, st.Effort)
		if err != nil {
			return "", err
		}
		task = "Split the FRD below into the fewest ordered parts that are each self-contained and small enough to plan on their own."
		if size.BudgetTokens > 0 {
			task += fmt.Sprintf(" Each part, written out as JSON, must stay under about %d tokens (roughly four characters per token).", size.BudgetTokens)
		}
		task += " Part n may rely on the parts before it, never on a later one. Every requirement lands in exactly one part, together with the criteria that check it."
		if req.MaxParts > 0 {
			task += fmt.Sprintf(" Use at most %d parts.", req.MaxParts)
		}
		input = "FRD to split:\n\n```json\n" + frd + "\n```"
		if req.Description != "" {
			input += "\n\nNotes from the person:\n\n" + req.Description
		}
		sizeRule = "Each part is a complete small FRD with its own objective, requirements and criteria. Do not add scope."
		note = "The purpose is split: reply with one JSON object {\"parts\": [part, part, ...]} where every part has the shape above and its own `title`. Parts are in the order they should be built."
	}
	var dod strings.Builder
	for _, it := range definitionOfDone(p, p.RepoPath).Items {
		dod.WriteString("- " + it + "\n")
	}
	for k, v := range map[string]string{
		"{{TASK}}": task, "{{INPUT}}": input, "{{REPO_CONTEXT}}": repoHint(p.RepoPath),
		"{{DOD}}": strings.TrimRight(dod.String(), "\n"), "{{SIZE_RULE}}": sizeRule, "{{OUTPUT_NOTE}}": note,
	} {
		text = strings.ReplaceAll(text, k, v)
	}
	return text, nil
}

// repoHint is the repository context of a drafter prompt: the head of the
// README when there is one, else a pointer to the working directory.
func repoHint(dir string) string {
	const pointer = "inspect the repository in your working directory."
	for _, name := range []string{"README.md", "README", "readme.md"} {
		f, err := os.Open(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		buf := make([]byte, 2000)
		n, _ := f.Read(buf)
		f.Close()
		lines := strings.Split(strings.TrimSpace(strings.ToValidUTF8(string(buf[:n]), "")), "\n")
		if len(lines) > 30 {
			lines = lines[:30]
		}
		if text := strings.TrimSpace(strings.Join(lines, "\n")); text != "" {
			return "the start of " + name + " follows; for anything more, " + pointer + "\n\n```\n" + text + "\n```"
		}
	}
	return "(not inlined: " + pointer + ")"
}
