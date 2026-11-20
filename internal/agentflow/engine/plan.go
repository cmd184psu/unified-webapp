package engine

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"cmd184psu/unified-webapp/internal/agentflow/model"
	"cmd184psu/unified-webapp/internal/agentflow/prompts"
	"cmd184psu/unified-webapp/internal/agentflow/schema"
	"cmd184psu/unified-webapp/internal/agentflow/store"
)

// The ralplan planning loop (RP1 to RP4, MB6). A round is a planner stage, an
// architect stage and a critic stage, each its own agentflow.plan stage with
// its own candidates. The driver (AdvancePlans) is idempotent like Advance:
// it derives the next step from the effort's stages and documents alone, so a
// restart, a lost stage or a second call repeats nothing.

// planRoles are the stage roles of the loop.
var planRoles = []string{"planner", "architect", "critic"}

const (
	eventPlanStart  = "plan.start"
	eventPlanWarn   = "plan.warning"
	eventPlanHalted = "plan.halted"
)

// planDoc is a plan revision: the planner's PlanOut with the round and stage
// that produced it. Revisions are immutable (DBS4).
type planDoc struct {
	schema.PlanOut
	Round int   `json:"round"`
	Stage int64 `json:"stage"`
}

// PlanReview is one architect or critic review, stored as a "review"
// revision (RP4).
type PlanReview struct {
	Revision     int            `json:"revision"`
	Role         string         `json:"role"`
	Round        int            `json:"round"`
	PlanRevision int            `json:"plan_revision"`
	Stage        int64          `json:"stage"`
	Verdict      string         `json:"verdict"`
	Summary      string         `json:"summary,omitempty"`
	Issues       []schema.Issue `json:"issues"`
}

type planRec struct {
	Revision int
	planDoc
}

// planSession is the part of an effort's history that belongs to its current
// planning session: stages and documents newer than the plan.start event.
type planSession struct {
	floor   int64
	stages  []model.Stage
	plans   []planRec
	reviews []PlanReview
}

// PlanView is what the UI shows for an effort's plan.
type PlanView struct {
	Effort int64  `json:"effort"`
	Status string `json:"status"`
	// Rounds is the number of completed rounds (critic reviews).
	Rounds int `json:"rounds"`
	// Revision is the plan revision presented for approval: the latest plan
	// once architect and critic both approved it, else the best revision (the
	// latest critic revise over reject).
	Revision int             `json:"revision"`
	Plan     json.RawMessage `json:"plan,omitempty"`
	// NotCriticApproved marks a finished loop that ended without approval
	// from both reviewers (RP2).
	NotCriticApproved bool         `json:"not_critic_approved"`
	Reviews           []PlanReview `json:"reviews"`
	// Warnings say where planner, architect and critic could not be pairwise
	// different models (MB6).
	Warnings []string `json:"warnings"`
	// Waiting is why the loop cannot submit its next stage right now.
	Waiting string `json:"waiting,omitempty"`
}

// allDocs returns every revision of an effort's document kind, oldest first.
func (e *Engine) allDocs(ctx context.Context, effort int64, kind string) ([]model.Doc, error) {
	last, err := e.deps.Store.GetDoc(ctx, effort, kind, 0)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	docs := make([]model.Doc, 0, last.Revision)
	for rev := 1; rev < last.Revision; rev++ {
		d, err := e.deps.Store.GetDoc(ctx, effort, kind, rev)
		if err != nil {
			return nil, err
		}
		docs = append(docs, d)
	}
	return append(docs, last), nil
}

// effortEvents returns the effort's audit events of one action, oldest first.
func (e *Engine) effortEvents(ctx context.Context, effort int64, action string) ([]store.Event, error) {
	evs, err := e.deps.Store.ListEvents(ctx, "effort", strconv.FormatInt(effort, 10), 100000)
	if err != nil {
		return nil, err
	}
	var out []store.Event
	for _, ev := range evs {
		if ev.Action == action {
			out = append(out, ev)
		}
	}
	return out, nil
}

// noteEffort writes an audit event about an effort.
func (e *Engine) noteEffort(ctx context.Context, actor, action string, effort int64, detail any) error {
	return e.deps.Store.WithTx(ctx, func(tx *sql.Tx) error {
		_, err := store.AppendEvent(tx, actor, action, "effort", strconv.FormatInt(effort, 10), detail)
		return err
	})
}

// eventFloor is the after_stage of the newest event of action: stages up to it
// belong to an earlier session.
func (e *Engine) eventFloor(ctx context.Context, effort int64, action string) (int64, error) {
	evs, err := e.effortEvents(ctx, effort, action)
	if err != nil || len(evs) == 0 {
		return 0, err
	}
	var d struct {
		After int64 `json:"after_stage"`
	}
	if err := json.Unmarshal([]byte(evs[len(evs)-1].DetailJSON), &d); err != nil {
		return 0, err
	}
	return d.After, nil
}

// maxStageID is the newest stage id of the effort, or 0.
func (e *Engine) maxStageID(ctx context.Context, effort int64) (int64, error) {
	stages, err := e.deps.Store.ListEffortStages(ctx, effort)
	if err != nil || len(stages) == 0 {
		return 0, err
	}
	return stages[len(stages)-1].ID, nil
}

func (e *Engine) loadPlanSession(ctx context.Context, effort int64) (*planSession, error) {
	floor, err := e.eventFloor(ctx, effort, eventPlanStart)
	if err != nil {
		return nil, err
	}
	sess := &planSession{floor: floor}
	all, err := e.deps.Store.ListEffortStages(ctx, effort)
	if err != nil {
		return nil, err
	}
	for _, st := range all {
		if st.ID > floor && slices.Contains(planRoles, st.Role) && draftPurpose(st) == "" {
			sess.stages = append(sess.stages, st)
		}
	}
	plans, err := e.allDocs(ctx, effort, model.DocPlan)
	if err != nil {
		return nil, err
	}
	for _, d := range plans {
		var pd planDoc
		if err := json.Unmarshal(d.JSON, &pd); err != nil {
			return nil, fmt.Errorf("plan revision %d: %w", d.Revision, err)
		}
		sess.plans = append(sess.plans, planRec{Revision: d.Revision, planDoc: pd})
	}
	reviews, err := e.allDocs(ctx, effort, model.DocReview)
	if err != nil {
		return nil, err
	}
	for _, d := range reviews {
		var pr PlanReview
		if err := json.Unmarshal(d.JSON, &pr); err != nil {
			return nil, fmt.Errorf("review revision %d: %w", d.Revision, err)
		}
		if pr.Stage > floor && (pr.Role == "architect" || pr.Role == "critic") {
			pr.Revision = d.Revision
			sess.reviews = append(sess.reviews, pr)
		}
	}
	return sess, nil
}

// planDocFor is the session's plan produced by stage, if saved.
func (s *planSession) planDocFor(stage int64) (planRec, bool) {
	for _, p := range s.plans {
		if p.Stage == stage {
			return p, true
		}
	}
	return planRec{}, false
}

// latestPlan is the newest plan the session produced (before stage `before`
// when it is non-zero).
func (s *planSession) latestPlan(before int64) (planRec, bool) {
	for i := len(s.plans) - 1; i >= 0; i-- {
		if p := s.plans[i]; p.Stage > s.floor && (before == 0 || p.Stage < before) {
			return p, true
		}
	}
	return planRec{}, false
}

func (s *planSession) reviewFor(stage int64) bool {
	for _, r := range s.reviews {
		if r.Stage == stage {
			return true
		}
	}
	return false
}

func (s *planSession) review(role string, round int) (PlanReview, bool) {
	for _, r := range s.reviews {
		if r.Role == role && r.Round == round {
			return r, true
		}
	}
	return PlanReview{}, false
}

// roundOf is the round a stage belongs to: the number of planner stages that
// passed up to and including it.
func (s *planSession) roundOf(stage model.Stage) int {
	n := 0
	for _, st := range s.stages {
		if st.Role == "planner" && st.Outcome == OutcomePass {
			n++
		}
		if st.ID == stage.ID {
			break
		}
	}
	return n
}

// others are the models the other planning roles last used, for choosing
// pairwise different models (MB6).
func (s *planSession) others(role string) map[string]string {
	out := map[string]string{}
	for _, st := range s.stages {
		if st.Role != role {
			out[st.Role] = st.Model
		}
	}
	return out
}

// presented is the plan revision to offer for approval and whether it falls
// short of full approval. Rank: approve over revise over reject, the latest
// critic review winning within a rank (RP2).
func (s *planSession) presented() (rev int, notApproved bool) {
	var crits []PlanReview
	for _, r := range s.reviews {
		if r.Role == "critic" {
			crits = append(crits, r)
		}
	}
	if len(crits) == 0 {
		return 0, false
	}
	last := crits[len(crits)-1]
	if a, ok := s.review("architect", last.Round); ok && a.Verdict == "approve" && last.Verdict == "approve" && len(s.plans) > 0 {
		return s.plans[len(s.plans)-1].Revision, false
	}
	rank := map[string]int{"approve": 2, "revise": 1, "reject": 0}
	best := crits[0]
	for _, c := range crits[1:] {
		if rank[c.Verdict] >= rank[best.Verdict] {
			best = c
		}
	}
	return best.PlanRevision, true
}

func (e *Engine) setPlanWaiting(effort int64, reason string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if reason == "" {
		delete(e.planWaiting, effort)
		return
	}
	e.planWaiting[effort] = reason
}

// PlanView reports the effort's plan, its reviews and the revision that would
// be approved.
func (e *Engine) PlanView(ctx context.Context, effort int64) (PlanView, error) {
	ef, err := e.deps.Store.GetEffort(ctx, effort)
	if err != nil {
		return PlanView{}, err
	}
	sess, err := e.loadPlanSession(ctx, effort)
	if err != nil {
		return PlanView{}, err
	}
	v := PlanView{Effort: effort, Status: ef.Status, Reviews: sess.reviews, Warnings: []string{}}
	if v.Reviews == nil {
		v.Reviews = []PlanReview{}
	}
	for _, r := range sess.reviews {
		if r.Role == "critic" {
			v.Rounds++
		}
	}
	finished := ef.Status != model.EffortDraft && ef.Status != model.EffortPlanning
	if rev, notApproved := sess.presented(); rev != 0 {
		v.Revision, v.NotCriticApproved = rev, notApproved && finished
		for _, p := range sess.plans {
			if p.Revision == rev {
				raw, err := json.Marshal(p.PlanOut)
				if err != nil {
					return v, err
				}
				v.Plan = raw
			}
		}
	}
	warns, err := e.effortEvents(ctx, effort, eventPlanWarn)
	if err != nil {
		return v, err
	}
	for _, ev := range warns {
		var d struct {
			Message string `json:"message"`
		}
		if json.Unmarshal([]byte(ev.DetailJSON), &d) == nil && d.Message != "" {
			v.Warnings = append(v.Warnings, d.Message)
		}
	}
	e.mu.Lock()
	v.Waiting = e.planWaiting[effort]
	e.mu.Unlock()
	return v, nil
}

// GeneratePlan starts the planning loop on an effort that has an FRD: the
// effort moves to planning and the planner stage is submitted. It returns at
// once; the loop runs on stage completions (AdvancePlans). Called on an effort
// that is already planning it only nudges the loop; called on a plan_ready
// effort it starts a fresh session whose revisions follow the old ones.
func (e *Engine) GeneratePlan(ctx context.Context, actor string, effort int64) error {
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
	switch ef.Status {
	case model.EffortPlanning:
		return e.advancePlan(ctx, p, ef)
	case model.EffortDraft, model.EffortPlanReady:
	default:
		return fmt.Errorf("effort %d is %s, a plan cannot be generated: %w", effort, ef.Status, store.ErrConflict)
	}
	if _, err := s.GetDoc(ctx, effort, model.DocFRD, 0); errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("effort %d has no FRD: %w", effort, store.ErrConflict)
	} else if err != nil {
		return err
	}
	if cur, err := e.DraftState(ctx, effort); err != nil {
		return err
	} else if cur.State == "running" {
		return fmt.Errorf("effort %d is still drafting its FRD: %w", effort, store.ErrConflict)
	}
	floor, err := e.maxStageID(ctx, effort)
	if err != nil {
		return err
	}
	if err := e.noteEffort(ctx, actor, eventPlanStart, effort, map[string]any{"after_stage": floor}); err != nil {
		return err
	}
	planning := model.EffortPlanning
	if ef, err = s.UpdateEffort(ctx, actor, effort, store.EffortPatch{Status: &planning}); err != nil {
		return err
	}
	return e.advancePlan(ctx, p, ef)
}

// AdvancePlans moves every planning and compiling effort one step. It is
// driven by stage completion, the reconcile tick, Start and Retire, and is
// idempotent.
func (e *Engine) AdvancePlans(ctx context.Context) error {
	e.planMu.Lock()
	defer e.planMu.Unlock()
	s := e.deps.Store
	projects, err := s.ListProjects(ctx)
	if err != nil {
		return err
	}
	var errs []error
	for _, p := range projects {
		ef, err := s.ActiveEffort(ctx, p.Slug)
		if errors.Is(err, store.ErrNotFound) {
			continue
		}
		if err != nil {
			errs = append(errs, err)
			continue
		}
		switch ef.Status {
		case model.EffortDraft:
			err = e.advanceDraft(ctx, p, ef)
		case model.EffortPlanning:
			err = e.advancePlan(ctx, p, ef)
		case model.EffortCompiling:
			err = e.advanceCompile(ctx, p, ef)
		case model.EffortPlanApproved:
			err = e.resumeCompile(ctx, ef)
		case model.EffortQueueDraft:
			err = e.healQueueStatus(ctx, ef)
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("effort %d: %w", ef.ID, err))
		}
	}
	return errors.Join(errs...)
}

// resumeCompile starts the compile of an approved plan whose compile never
// began (a restart between approval and start, or a start that failed). A
// compile that ran and halted is left alone: retrying it every tick would loop
// on a failing compiler, so the human presses Compile instead.
func (e *Engine) resumeCompile(ctx context.Context, ef model.Effort) error {
	stages, err := e.deps.Store.ListEffortStages(ctx, ef.ID)
	if err != nil {
		return err
	}
	for _, st := range stages {
		if st.Role == "compiler" {
			return nil
		}
	}
	if err := e.startCompileLocked(ctx, SystemActor, ef.ID); err != nil {
		e.setPlanWaiting(ef.ID, "compile did not start: "+err.Error())
	}
	return nil
}

// advancePlan takes the next step of a planning effort. planMu is held.
func (e *Engine) advancePlan(ctx context.Context, p store.Project, ef model.Effort) error {
	sess, err := e.loadPlanSession(ctx, ef.ID)
	if err != nil {
		return err
	}
	for _, st := range sess.stages {
		if st.State != model.StageDone {
			return nil // in flight
		}
	}
	if len(sess.stages) == 0 {
		return e.startEffortStage(ctx, p, ef.ID, "planner", nil, "")
	}
	last := sess.stages[len(sess.stages)-1]
	if IsInfra(last.Outcome) {
		return e.effortInfra(ctx, p, sess.stages, last,
			func(avoid string) error {
				if avoid != "" { // keep away from every model of this role that crashed, so two flaky ones do not trade the stage
					var failed []model.Stage
					for _, st := range sess.stages {
						if st.Role == last.Role && IsInfra(st.Outcome) {
							failed = append(failed, st)
						}
					}
					avoid = avoidTried(failed)
				}
				return e.startEffortStage(ctx, p, ef.ID, last.Role, sess.others(last.Role), avoid)
			},
			func(reason string) error { return e.haltPlan(ctx, ef, last, reason) })
	}
	if last.Outcome != OutcomePass {
		return e.haltPlan(ctx, ef, last, "stage ended "+last.Outcome)
	}
	if err := e.recordPlanStage(ctx, sess, last); err != nil {
		return err
	}
	switch last.Role {
	case "planner":
		return e.startEffortStage(ctx, p, ef.ID, "architect", sess.others("architect"), "")
	case "architect":
		return e.startEffortStage(ctx, p, ef.ID, "critic", sess.others("critic"), "")
	}
	round := sess.roundOf(last)
	arch, _ := sess.review("architect", round)
	crit, _ := sess.review("critic", round)
	if arch.Verdict == "approve" && crit.Verdict == "approve" {
		return e.finishPlan(ctx, ef, true)
	}
	set, err := e.deps.Store.EffectiveSettings(ctx, p.Slug)
	if err != nil {
		return err
	}
	if round >= set.PlanMaxRounds {
		return e.finishPlan(ctx, ef, false)
	}
	return e.startEffortStage(ctx, p, ef.ID, "planner", sess.others("planner"), "")
}

// recordPlanStage saves the plan or review a passed stage produced as a
// document revision, unless an earlier pass already did.
func (e *Engine) recordPlanStage(ctx context.Context, sess *planSession, st model.Stage) error {
	s := e.deps.Store
	actor := st.Role + ":" + st.Model
	round := sess.roundOf(st)
	if st.Role == "planner" {
		if _, ok := sess.planDocFor(st.ID); ok {
			return nil
		}
		out, err := schema.Parse[schema.PlanOut](st.Parsed)
		if err != nil {
			return fmt.Errorf("stage %d: %w", st.ID, err)
		}
		pd := planDoc{PlanOut: out, Round: round, Stage: st.ID}
		raw, err := json.Marshal(pd)
		if err != nil {
			return err
		}
		doc, err := s.SaveDoc(ctx, actor, st.Effort, model.DocPlan, raw)
		if err != nil {
			return err
		}
		sess.plans = append(sess.plans, planRec{Revision: doc.Revision, planDoc: pd})
		return nil
	}
	if sess.reviewFor(st.ID) {
		return nil
	}
	out, err := schema.Parse[schema.PlanReviewOut](st.Parsed)
	if err != nil {
		return fmt.Errorf("stage %d: %w", st.ID, err)
	}
	pr := PlanReview{Role: st.Role, Round: round, Stage: st.ID, Verdict: out.Verdict, Summary: out.Summary, Issues: out.Issues}
	if pl, ok := sess.latestPlan(st.ID); ok {
		pr.PlanRevision = pl.Revision
	}
	if pr.Issues == nil {
		pr.Issues = []schema.Issue{}
	}
	raw, err := json.Marshal(pr)
	if err != nil {
		return err
	}
	doc, err := s.SaveDoc(ctx, actor, st.Effort, model.DocReview, raw)
	if err != nil {
		return err
	}
	pr.Revision = doc.Revision
	sess.reviews = append(sess.reviews, pr)
	return nil
}

// finishPlan ends the loop: the effort is plan_ready, waiting for a human
// (RP3). Only YOLO, and only for a plan both reviewers approved, approves it
// here (YO2).
func (e *Engine) finishPlan(ctx context.Context, ef model.Effort, approved bool) error {
	ready := model.EffortPlanReady
	if _, err := e.deps.Store.UpdateEffort(ctx, SystemActor, ef.ID, store.EffortPatch{Status: &ready}); err != nil {
		return err
	}
	e.changed()
	e.setPlanWaiting(ef.ID, "")
	if approved && e.yoloGate(ctx, ef.ID) {
		return e.approvePlanLocked(ctx, yoloActor, ef.ID)
	}
	return nil
}

// haltPlan gives up a planning session whose stages keep failing: the effort
// returns to draft so a human can fix the cause and generate again.
func (e *Engine) haltPlan(ctx context.Context, ef model.Effort, last model.Stage, reason string) error {
	draft := model.EffortDraft
	defer e.changed()
	if _, err := e.deps.Store.UpdateEffort(ctx, SystemActor, ef.ID, store.EffortPatch{Status: &draft}); err != nil {
		return err
	}
	e.setPlanWaiting(ef.ID, "")
	return e.noteEffort(ctx, SystemActor, eventPlanHalted, ef.ID, map[string]any{"stage": last.ID, "role": last.Role, "reason": reason})
}

// ApprovePlan records the human approval of the presented plan revision and
// moves the effort to plan_approved (RP3). It is ErrConflict unless the effort
// is plan_ready. The compile then starts at once, as the approver; if it
// cannot start (no compiler candidate) the approval stands and the effort
// waits with a reason; a tick starts it once it can, unless a compile already ran and halted.
func (e *Engine) ApprovePlan(ctx context.Context, actor string, effort int64) error {
	e.planMu.Lock()
	defer e.planMu.Unlock()
	return e.approvePlanLocked(ctx, actor, effort)
}

func (e *Engine) approvePlanLocked(ctx context.Context, actor string, effort int64) error {
	s := e.deps.Store
	ef, err := s.GetEffort(ctx, effort)
	if err != nil {
		return err
	}
	if ef.Status != model.EffortPlanReady {
		return fmt.Errorf("effort %d is %s, not plan_ready: %w", effort, ef.Status, store.ErrConflict)
	}
	v, err := e.PlanView(ctx, effort)
	if err != nil {
		return err
	}
	if v.Revision == 0 {
		return fmt.Errorf("effort %d has no plan to approve: %w", effort, store.ErrConflict)
	}
	if _, err := s.ApproveDoc(ctx, actor, effort, model.DocPlan, v.Revision); err != nil {
		return err
	}
	approved := model.EffortPlanApproved
	if _, err := s.UpdateEffort(ctx, actor, effort, store.EffortPatch{Status: &approved}); err != nil {
		return err
	}
	compileActor := actor
	if e.yoloGate(ctx, effort) {
		compileActor = yoloActor
	}
	if err := e.startCompileLocked(ctx, compileActor, effort); err != nil {
		e.setPlanWaiting(effort, "compile did not start: "+err.Error())
	}
	return nil
}

// Deliberate mode (RP5) adds a pre-mortem and an expanded test plan to the
// planner's job. The planner's reply is rejected without the pre-mortem
// (schema.PlanOut.ValidateDeliberate, which feeds the SO2 corrective), and the
// critic is told to check both.
const deliberatePlannerRules = "10. Deliberate mode is on. Add `premortem`: exactly three scenarios in which this plan fails, each one string naming the cause, the effect and the mitigation. " +
	"Add `test_plan`: an expanded test plan with entries for unit tests, integration tests, end-to-end tests and observability (what proves it works in operation), each tied to the phase that delivers it."

const deliberateCriticChecks = "## Deliberate mode\n\n" +
	"This plan was written in deliberate mode. Also check: `premortem` holds exactly three failure scenarios and each names a cause, an effect and a mitigation that a phase carries out; " +
	"`test_plan` covers unit, integration, end-to-end and observability checks for the behavior the FRD asks for. " +
	"A missing or hollow pre-mortem or test plan is a \"revise\" with severity \"major\".\n"

// EffortPrompt builds the prompt of a planning-loop or compiler stage: the role
// prompt with its inputs filled in. The critic gets the FRD and the plan only,
// never the architect's review (RP1). A custom Deps.Prompt serving effort
// stages delegates here.
func (e *Engine) EffortPrompt(ctx context.Context, st model.Stage) (string, error) {
	if purpose := draftPurpose(st); purpose != "" {
		return e.draftPrompt(ctx, st, purpose)
	}
	if isAcceptance(st) {
		return e.acceptancePrompt(ctx, st)
	}
	text, err := prompts.Load(st.Role)
	if err != nil {
		return "", err
	}
	s := e.deps.Store
	ef, err := s.GetEffort(ctx, st.Effort)
	if err != nil {
		return "", err
	}
	frd := "null"
	if d, err := s.GetDoc(ctx, st.Effort, model.DocFRD, 0); err == nil {
		frd = compactJSON(d.JSON)
	} else if !errors.Is(err, store.ErrNotFound) {
		return "", err
	}
	rep := map[string]string{
		"{{FRD_JSON}}":         frd,
		"{{REPO_CONTEXT}}":     "(not inlined: inspect the repository in your working directory)",
		"{{REVISION_CONTEXT}}": "",
		"{{DELIBERATE_RULES}}": "",
		"{{PLAN_JSON}}":        "null",
	}
	rep["{{DELIBERATE_CHECKS}}"] = ""
	if ef.Deliberate {
		rep["{{DELIBERATE_RULES}}"] = deliberatePlannerRules
		rep["{{DELIBERATE_CHECKS}}"] = deliberateCriticChecks
	}
	switch st.Role {
	case "planner":
		sess, err := e.loadPlanSession(ctx, st.Effort)
		if err != nil {
			return "", err
		}
		rep["{{REVISION_CONTEXT}}"] = revisionContext(sess, st.ID)
	case "architect", "critic":
		sess, err := e.loadPlanSession(ctx, st.Effort)
		if err != nil {
			return "", err
		}
		if pl, ok := sess.latestPlan(st.ID); ok {
			rep["{{PLAN_JSON}}"] = planJSON(pl.PlanOut)
		}
	case "compiler":
		a, err := s.ApprovedRevision(ctx, st.Effort, model.DocPlan)
		if err != nil {
			return "", err
		}
		d, err := s.GetDoc(ctx, st.Effort, model.DocPlan, a.Revision)
		if err != nil {
			return "", err
		}
		var pd planDoc
		if err := json.Unmarshal(d.JSON, &pd); err != nil {
			return "", err
		}
		rep["{{PLAN_JSON}}"] = planJSON(pd.PlanOut)
	}
	for k, v := range rep {
		text = strings.ReplaceAll(text, k, v)
	}
	if (st.Role == "planner" || st.Role == "compiler") && frd != "null" {
		var f model.FRD
		if json.Unmarshal([]byte(frd), &f) == nil {
			where := "the plan"
			if st.Role == "compiler" {
				where = "the task prompts and acceptance criteria"
			}
			text += model.LiteralsNote(model.ExtractLiterals(model.FRDText(f)), where)
			if st.Role == "compiler" {
				text += e.compilerFRDNote(ctx, st, f)
			}
		}
	}
	return text, nil
}

func compactJSON(raw []byte) string {
	var b bytes.Buffer
	if err := json.Compact(&b, raw); err != nil {
		return string(raw)
	}
	return b.String()
}

func planJSON(p schema.PlanOut) string {
	b, _ := json.Marshal(p)
	return string(b)
}

// revisionContext tells a revising planner what the last round concluded: its
// previous plan and both reviews (RP2). It is empty in round one.
func revisionContext(sess *planSession, stage int64) string {
	prev, ok := sess.latestPlan(stage)
	if !ok {
		return ""
	}
	var b strings.Builder
	b.WriteString("## Revision round\n\nYour previous plan was reviewed by an architect and a critic. Write a revised plan that resolves their issues, or records in the ADR why an issue is declined.\n\nPrevious plan:\n\n```json\n" + planJSON(prev.PlanOut) + "\n```\n")
	for _, r := range sess.reviews {
		if r.Round != prev.Round || r.Stage >= stage {
			continue
		}
		raw, _ := json.Marshal(struct {
			Verdict string         `json:"verdict"`
			Summary string         `json:"summary,omitempty"`
			Issues  []schema.Issue `json:"issues"`
		}{r.Verdict, r.Summary, r.Issues})
		fmt.Fprintf(&b, "\n%s review:\n\n```json\n%s\n```\n", strings.ToUpper(r.Role[:1])+r.Role[1:], raw)
	}
	return b.String()
}

// avoidKey names one backend and model, for a retry that must not use the
// pair that just failed. Avoiding the whole backend, as this once did, left a
// backend serving several models (opencode's free ones) with nothing else to
// try, so the same model crashed again until the infrastructure budget ran out.
func avoidKey(backend, model string) string { return backend + "\x00" + model }

// avoidTried names every backend and model the stages of a run already used,
// for a retry after a reply that was read but wrong (a draft or queue that left
// out required strings). Avoiding only the last one let two models trade the
// task back and forth while a third was never asked.
func avoidTried(stages []model.Stage) string {
	keys := make([]string, 0, len(stages))
	for _, s := range stages {
		keys = append(keys, avoidKey(s.Backend, s.Model))
	}
	return strings.Join(keys, "\x01")
}

// repliesRead counts the stages whose reply was read (outcome pass), so a crash
// or a timeout does not use up a try that was meant for a wrong answer.
func repliesRead(stages []model.Stage) int {
	n := 0
	for _, s := range stages {
		if s.Outcome == OutcomePass {
			n++
		}
	}
	return n
}

func avoided(avoid, key string) bool {
	return avoid != "" && slices.Contains(strings.Split(avoid, "\x01"), key)
}

// pickEffortStage chooses the backend and model for a planning-loop or
// compiler stage. Candidates are walked in order (MB4); a model another
// planning role already used is skipped unless nothing else is eligible, in
// which case warn says so (MB6). Free slots are not required: the lane queues
// the stage. The string reason is empty on success.
func (e *Engine) pickEffortStage(ctx context.Context, p store.Project, role string, others map[string]string, avoid string) (pk Pick, warn, reason string, err error) {
	v, err := e.newView(ctx)
	if err != nil {
		return Pick{}, "", "", err
	}
	var reasons []string
	var fallback *Pick
	for _, c := range v.candidates(role, p.Slug) {
		if avoided(avoid, avoidKey(c.Backend, c.Model)) {
			reasons = append(reasons, "model "+c.Model+" on "+c.Backend+" just failed")
			continue
		}
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
		clash := ""
		for r, m := range others {
			if m == c.Model {
				clash = r
			}
		}
		if clash != "" {
			if fallback == nil {
				fallback = &Pick{Backend: c.Backend, Model: c.Model}
				warn = fmt.Sprintf("%s and %s both use model %s: the pool has no other eligible model for %s (MB6)", role, clash, c.Model, role)
			}
			continue
		}
		return Pick{Backend: c.Backend, Model: c.Model}, "", "", nil
	}
	if fallback != nil {
		return *fallback, warn, "", nil
	}
	return Pick{}, "", noEligible(role, reasons), nil
}

// startEffortStage submits the next stage of an effort's loop. When no backend
// is eligible it records why (PlanView.Waiting) and submits nothing; the next
// pass tries again.
func (e *Engine) startEffortStage(ctx context.Context, p store.Project, effort int64, role string, others map[string]string, avoid string) error {
	return e.startEffortStageRef(ctx, p, effort, role, others, avoid, "")
}

// startEffortStageRef is startEffortStage for a stage that carries a prompt
// reference, which says what the stage is for (an FRD draft, an acceptance
// check).
func (e *Engine) startEffortStageRef(ctx context.Context, p store.Project, effort int64, role string, others map[string]string, avoid, ref string) error {
	pk, warn, reason, err := e.pickEffortStage(ctx, p, role, others, avoid)
	if err == nil && reason != "" && avoid != "" {
		pk, warn, reason, err = e.pickEffortStage(ctx, p, role, others, "") // nowhere else: the same model again
	}
	if err != nil {
		return err
	}
	if reason != "" {
		e.setPlanWaiting(effort, reason)
		return nil
	}
	if warn != "" {
		if err := e.warnOnce(ctx, effort, warn); err != nil {
			return err
		}
	}
	e.setPlanWaiting(effort, "")
	_, _, err = e.Submit(ctx, BackendLane(pk.Backend),
		store.StageSpec{Effort: effort, Role: role, Backend: pk.Backend, Model: pk.Model, PromptRef: ref},
		StagePayload{Project: p.Slug, Effort: effort})
	return err
}

// warnOnce records an effort warning unless the same one is already there.
func (e *Engine) warnOnce(ctx context.Context, effort int64, msg string) error {
	evs, err := e.effortEvents(ctx, effort, eventPlanWarn)
	if err != nil {
		return err
	}
	for _, ev := range evs {
		var d struct {
			Message string `json:"message"`
		}
		if json.Unmarshal([]byte(ev.DetailJSON), &d) == nil && d.Message == msg {
			return nil
		}
	}
	return e.noteEffort(ctx, SystemActor, eventPlanWarn, effort, map[string]string{"message": msg})
}

// effortInfra decides what to do about an effort stage that ended in an
// infrastructure failure (AT7): a stop, restart or reroute (infra:cancel,
// lost, rerouted) counts nothing and is repeated at once; any other failure is
// a strike, repeated after the exponential backoff, and the strike that
// reaches max_infra_retries halts. stages are the failed stage's run of
// siblings, oldest first.
func (e *Engine) effortInfra(ctx context.Context, p store.Project, stages []model.Stage, last model.Stage, retry func(avoid string) error, halt func(reason string) error) error {
	strikes := 0
	for i := len(stages) - 1; i >= 0 && IsInfra(stages[i].Outcome); i-- {
		switch stages[i].Outcome {
		case OutcomeInfraCancel, OutcomeInfraLost, OutcomeInfraRerouted:
		default:
			strikes++
		}
	}
	switch last.Outcome {
	case OutcomeInfraCancel, OutcomeInfraLost, OutcomeInfraRerouted:
		return retry("")
	}
	set, err := e.deps.Store.EffectiveSettings(ctx, p.Slug)
	if err != nil {
		return err
	}
	if limit := set.MaxInfraRetries; limit > 0 && strikes >= limit {
		return halt(fmt.Sprintf("%d infrastructure failures (limit %d); last: %s on %s: %s",
			strikes, limit, last.Outcome, last.Backend, clip(last.RawOutput, maxEntryDetail)))
	}
	due := time.UnixMilli(last.Finished).Add(infraBackoff(strikes - 1))
	if wait := due.Sub(e.deps.Clock()); wait > 0 {
		e.advanceAfter(last.ID, wait)
		return nil
	}
	return retry(avoidKey(last.Backend, last.Model))
}

// compilerFRDNote lists the FRD ids the compiler must cite and, on a retry,
// what was wrong with its last queue (H14).
func (e *Engine) compilerFRDNote(ctx context.Context, st model.Stage, f model.FRD) string {
	var b strings.Builder
	b.WriteString("\n\n## FRD ids\n\nCite these ids, and only these, as the label of an acceptance criterion; every one must be cited by at least one task:\n\n")
	for _, r := range f.Requirements {
		b.WriteString("- " + r.ID + ": " + r.Description + "\n")
	}
	for _, ac := range f.AcceptanceCriteria {
		b.WriteString("- " + ac.ID + ": " + ac.Criterion + "\n")
	}
	if all, err := e.deps.Store.ListEffortStages(ctx, st.Effort); err == nil {
		var prev model.Stage
		for _, o := range all {
			if o.Role == "compiler" && o.ID < st.ID && o.Outcome == OutcomePass {
				prev = o
			}
		}
		if out, err := schema.Parse[schema.CompileOut](prev.Parsed); prev.ID != 0 && err == nil {
			if defs, err := compiledTasks(out); err == nil {
				var fixes []string
				if miss, err := e.queueMissingLiterals(ctx, st.Effort, defs); err == nil && len(miss) > 0 {
					fixes = append(fixes, (&literalsError{missing: miss, from: "the task queue"}).Error())
				}
				if probs, err := e.queueProblems(ctx, st.Effort, defs); err == nil {
					fixes = append(fixes, probs...)
				}
				if len(fixes) > 0 {
					// The earlier queue is shown so the retry edits it, keeping what was right,
					// instead of writing a new one that misses other strings (the fourth
					// local webcalc trial: three queues, each missing different strings).
					b.WriteString("\n## Fix these in the queue below\n\nStart from this queue, which is almost right: keep every task that is fine and change only what these say.\n\n")
					for _, p := range fixes {
						b.WriteString("- " + p + "\n")
					}
					b.WriteString("\nThe queue so far:\n\n```json\n" + compactJSON(prev.Parsed) + "\n```\n")
				}
			}
		}
	}
	return b.String()
}
