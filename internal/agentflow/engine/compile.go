package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"cmd184psu/unified-webapp/internal/agentflow/model"
	"cmd184psu/unified-webapp/internal/agentflow/schema"
	"cmd184psu/unified-webapp/internal/agentflow/store"
)

// Compilation turns an approved plan into a queue draft (QA1) and the queue's
// approval pins the whole queue by hash (QA2, QA3). The compiler emits no
// executable content: a reply with a verify field, a cycle or a dangling
// dependency is rejected inside the stage like any malformed reply, with the
// one corrective follow-up and then infra:unparseable (SO2, V1).

const (
	eventCompileStart  = "compile.start"
	eventCompileHalted = "compile.halted"
)

// compiledTasks converts the compiler's tasks to queue definitions and
// validates them: required fields, unique keys, no dangling dependency, no
// cycle, and no absolute path in a prompt or a touched file.
func compiledTasks(out schema.CompileOut) ([]model.TaskDef, error) {
	defs := make([]model.TaskDef, len(out.Tasks))
	for i, t := range out.Tasks {
		defs[i] = model.TaskDef{
			Key: t.ID, Phase: t.Phase, Title: t.Title, Prompt: t.Prompt,
			Acceptance: t.Acceptance, FilesTouched: t.FilesTouched, DependsOn: t.DependsOn,
		}
		for _, f := range append([]string{t.Prompt}, t.FilesTouched...) {
			if strings.HasPrefix(f, "/") {
				return nil, fmt.Errorf("task %s contains absolute path %q: use paths relative to the project", t.ID, f)
			}
		}
	}
	if err := model.ValidateTasks(defs); err != nil {
		return nil, err
	}
	return defs, nil
}

// Compile starts compilation of the effort's approved plan: the effort moves
// to compiling and the compiler stage is submitted. It is ErrConflict unless
// the plan is approved (or a draft queue is being recompiled). On an effort
// that is already compiling it only nudges the driver.
func (e *Engine) Compile(ctx context.Context, actor string, effort int64) error {
	e.planMu.Lock()
	defer e.planMu.Unlock()
	return e.startCompileLocked(ctx, actor, effort)
}

func (e *Engine) startCompileLocked(ctx context.Context, actor string, effort int64) error {
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
	case model.EffortCompiling:
		return e.advanceCompile(ctx, p, ef)
	case model.EffortPlanApproved, model.EffortQueueDraft:
	default:
		return fmt.Errorf("effort %d is %s, the plan is not approved: %w", effort, ef.Status, store.ErrConflict)
	}
	if _, err := s.ApprovedRevision(ctx, effort, model.DocPlan); errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("effort %d has no approved plan: %w", effort, store.ErrConflict)
	} else if err != nil {
		return err
	}
	floor, err := e.maxStageID(ctx, effort)
	if err != nil {
		return err
	}
	if err := e.noteEffort(ctx, actor, eventCompileStart, effort, map[string]any{"after_stage": floor}); err != nil {
		return err
	}
	compiling := model.EffortCompiling
	if ef, err = s.UpdateEffort(ctx, actor, effort, store.EffortPatch{Status: &compiling}); err != nil {
		return err
	}
	return e.advanceCompile(ctx, p, ef)
}

// advanceCompile takes the next step of a compiling effort. planMu is held.
func (e *Engine) advanceCompile(ctx context.Context, p store.Project, ef model.Effort) error {
	floor, err := e.eventFloor(ctx, ef.ID, eventCompileStart)
	if err != nil {
		return err
	}
	all, err := e.deps.Store.ListEffortStages(ctx, ef.ID)
	if err != nil {
		return err
	}
	var stages []model.Stage
	for _, st := range all {
		if st.Role == "compiler" && st.ID > floor {
			stages = append(stages, st)
		}
	}
	for _, st := range stages {
		if st.State != model.StageDone {
			return nil // in flight
		}
	}
	if len(stages) == 0 {
		return e.startEffortStage(ctx, p, ef.ID, "compiler", nil, "")
	}
	last := stages[len(stages)-1]
	if IsInfra(last.Outcome) {
		return e.effortInfra(ctx, p, stages, last,
			func(avoid string) error { return e.startEffortStage(ctx, p, ef.ID, "compiler", nil, avoid) },
			func(reason string) error { return e.haltCompile(ctx, ef, last, reason) })
	}
	if last.Outcome != OutcomePass {
		return e.haltCompile(ctx, ef, last, "stage ended "+last.Outcome)
	}
	out, err := schema.Parse[schema.CompileOut](last.Parsed)
	if err != nil {
		return e.haltCompile(ctx, ef, last, err.Error())
	}
	defs, err := compiledTasks(out)
	if err != nil {
		return e.haltCompile(ctx, ef, last, err.Error())
	}
	_, err = e.deps.Store.SaveQueue(ctx, "compiler:"+last.Model, ef.ID, defs)
	if err != nil {
		if errors.Is(err, store.ErrConflict) {
			return e.haltCompile(ctx, ef, last, err.Error())
		}
		return err
	}
	draft := model.EffortQueueDraft
	if _, err := e.deps.Store.UpdateEffort(ctx, SystemActor, ef.ID, store.EffortPatch{Status: &draft}); err != nil {
		return err
	}
	e.setPlanWaiting(ef.ID, "")
	return nil
}

// haltCompile gives up compilation: the effort goes back to where it came from
// (a draft queue if one exists, else the approved plan) with an audit event.
func (e *Engine) haltCompile(ctx context.Context, ef model.Effort, last model.Stage, reason string) error {
	back := model.EffortPlanApproved
	if _, err := e.deps.Store.GetQueue(ctx, ef.ID); err == nil {
		back = model.EffortQueueDraft
	}
	if _, err := e.deps.Store.UpdateEffort(ctx, SystemActor, ef.ID, store.EffortPatch{Status: &back}); err != nil {
		return err
	}
	e.setPlanWaiting(ef.ID, "")
	return e.noteEffort(ctx, SystemActor, eventCompileHalted, ef.ID, map[string]any{"stage": last.ID, "reason": reason})
}

// ApproveQueue approves the whole draft queue the approver reviewed: the hash
// pins what was shown, so a queue edited or recompiled since is ErrConflict
// (409) and nothing changes (QA2). Under YOLO the run starts at once.
func (e *Engine) ApproveQueue(ctx context.Context, actor string, effort int64, expectedHash string) (model.Queue, error) {
	return e.approveQueue(ctx, actor, effort, expectedHash)
}

func (e *Engine) approveQueue(ctx context.Context, actor string, effort int64, expectedHash string) (model.Queue, error) {
	s := e.deps.Store
	ef, err := s.GetEffort(ctx, effort)
	if err != nil {
		return model.Queue{}, err
	}
	if ef.Status != model.EffortQueueDraft {
		return model.Queue{}, fmt.Errorf("effort %d is %s, not queue_draft: %w", effort, ef.Status, store.ErrConflict)
	}
	q, err := s.ApproveQueue(ctx, actor, effort, expectedHash)
	if err != nil {
		return q, err
	}
	if err := e.healQueueStatus(ctx, ef); err != nil {
		return q, err
	}
	return q, nil
}

// healQueueStatus moves a queue_draft effort whose queue is approved to
// queue_approved. It completes an approval that stopped between the queue
// write and the status write.
func (e *Engine) healQueueStatus(ctx context.Context, ef model.Effort) error {
	s := e.deps.Store
	q, err := s.GetQueue(ctx, ef.ID)
	if errors.Is(err, store.ErrNotFound) || (err == nil && q.Status != model.QueueApproved) {
		return nil
	}
	if err != nil {
		return err
	}
	cur, err := s.GetEffort(ctx, ef.ID)
	if err != nil || cur.Status != model.EffortQueueDraft {
		return err
	}
	appr := model.EffortQueueAppr
	_, err = s.UpdateEffort(ctx, q.ApprovedBy, ef.ID, store.EffortPatch{Status: &appr})
	return err
}

// QueueView is the approval view of an effort's queue (QA3): every task with
// its prompt, files_touched, acceptance criteria and dependencies, and the
// hash an approval must quote.
type QueueView struct {
	Effort       int64        `json:"effort"`
	EffortStatus string       `json:"effort_status"`
	Revision     int          `json:"revision"`
	Hash         string       `json:"hash"`
	Status       string       `json:"status"`
	ApprovedBy   string       `json:"approved_by,omitempty"`
	Tasks        []model.Task `json:"tasks"`
}

// QueueView returns the effort's queue for approval.
func (e *Engine) QueueView(ctx context.Context, effort int64) (QueueView, error) {
	s := e.deps.Store
	ef, err := s.GetEffort(ctx, effort)
	if err != nil {
		return QueueView{}, err
	}
	q, err := s.GetQueue(ctx, effort)
	if err != nil {
		return QueueView{}, err
	}
	tasks, err := s.ListTasks(ctx, effort)
	if err != nil {
		return QueueView{}, err
	}
	return QueueView{Effort: effort, EffortStatus: ef.Status, Revision: q.Revision, Hash: q.Hash,
		Status: q.Status, ApprovedBy: q.ApprovedBy, Tasks: tasks}, nil
}
