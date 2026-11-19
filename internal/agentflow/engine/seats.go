package engine

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"cmd184psu/unified-webapp/internal/agentflow/model"
	"cmd184psu/unified-webapp/internal/agentflow/runner"
	"cmd184psu/unified-webapp/internal/agentflow/store"
)

// OutcomeDetached is never stored: a seat stage whose call stopped (a shutdown) while the
// seat keeps working outside the process. The stage stays open and is adopted again.
const OutcomeDetached = "detached"

// SeatControl is what the engine needs from the seat runners (AF-9, AF-11). A seat is a
// stage worked outside agentflow (a BotChat callsign, an interactive program in a terminal
// session) that survives a restart. The composition root wires the concrete runners.
type SeatControl interface {
	// IsSeat says backends of this api type are seats.
	IsSeat(apiType string) bool
	// Alive says the stage's seat is still working and can be adopted.
	Alive(ctx context.Context, apiType string, stage int64, stageDir string) bool
	// Cancel stops the stage's seat (kills its session, cancels its task); the running
	// call then returns as cancelled rather than detached.
	Cancel(ctx context.Context, apiType string, stage int64, stageDir string) error
	// Answer hands the owner's answer to the stage's seat.
	Answer(ctx context.Context, apiType string, stage int64, stageDir, text string) error
	// Attach is the command that opens the stage's terminal, "" when it has none.
	Attach(ctx context.Context, apiType string, stage int64) string
	// Sweep ends seat sessions whose stage is no longer open.
	Sweep(ctx context.Context, open func(stage int64) bool) error
	// Notices lists the seat stages waiting on the owner.
	Notices() []runner.SeatNotice
}

// SeatStage is one running seat stage, for the API and the UI.
type SeatStage struct {
	Stage   int64  `json:"stage"`
	Project string `json:"project"`
	Effort  int64  `json:"effort"`
	Task    string `json:"task,omitempty"`
	Role    string `json:"role"`
	Backend string `json:"backend"`
	APIType string `json:"api_type"`
	Model   string `json:"model"`
	// Attach is the command that opens the seat's terminal, empty for a BotChat seat.
	Attach string `json:"attach,omitempty"`
	// Waiting is the notice the seat is waiting on the owner with, if any.
	Waiting *runner.SeatNotice `json:"waiting,omitempty"`
}

// ErrNotSeat is returned for a stage that is not an open seat stage.
var ErrNotSeat = errors.New("not an open seat stage")

// seatType returns the api type of the stage's backend when it is a seat.
func (e *Engine) seatType(ctx context.Context, st model.Stage) (string, bool) {
	if e.deps.Seats == nil || st.Backend == "" {
		return "", false
	}
	b, err := e.deps.Store.GetBackend(ctx, st.Backend)
	if err != nil || !e.deps.Seats.IsSeat(b.APIType) {
		return "", false
	}
	return b.APIType, true
}

// isSeatAPI says a backend api type is a seat type.
func (e *Engine) isSeatAPI(apiType string) bool {
	return e.deps.Seats != nil && e.deps.Seats.IsSeat(apiType)
}

// cancelSeat stops a seat stage's seat before its lane job is cancelled, so the call ends as
// cancelled and not detached. A non-seat stage is left alone.
func (e *Engine) cancelSeat(ctx context.Context, st model.Stage) {
	if t, ok := e.seatType(ctx, st); ok {
		_ = e.deps.Seats.Cancel(context.WithoutCancel(ctx), t, st.ID, e.StageDir(st.ID))
	}
}

// adoptSeat resubmits the lane job of an open seat stage whose job is gone (a restart) while
// its seat is still working. payload is the earlier job's; without one the stage cannot be
// placed and is left to be closed as lost.
func (e *Engine) adoptSeat(ctx context.Context, st model.Stage, refs []jobRef) bool {
	t, ok := e.seatType(ctx, st)
	if !ok || !e.deps.Seats.Alive(ctx, t, st.ID, e.StageDir(st.ID)) {
		return false
	}
	var p StagePayload
	found := false
	for i := len(refs) - 1; i >= 0; i-- {
		if json.Unmarshal(refs[i].job.Payload, &p) == nil && p.Stage == st.ID {
			found = true
			break
		}
	}
	if !found {
		return false
	}
	l, ok := e.Lane(BackendLane(st.Backend))
	if !ok {
		return false
	}
	if _, err := l.Submit(kindForRole(st.Role), fmt.Sprintf("%s stage %d (seat adopted)", st.Role, st.ID), p); err != nil {
		return false
	}
	e.auditStage(ctx, SystemActor, "stage.seat_adopted", st.ID, map[string]string{"backend": st.Backend})
	return true
}

// sweepSeats ends the seat sessions whose stage is closed.
func (e *Engine) sweepSeats(ctx context.Context) {
	if e.deps.Seats == nil {
		return
	}
	open, err := e.deps.Store.OpenStages(ctx)
	if err != nil {
		return
	}
	ids := map[int64]bool{}
	for _, st := range open {
		ids[st.ID] = true
	}
	_ = e.deps.Seats.Sweep(ctx, func(id int64) bool { return ids[id] })
}

// stageWhere names the project, effort and task a stage belongs to.
func (e *Engine) stageWhere(ctx context.Context, st model.Stage) (project string, effort int64, task string) {
	s := e.deps.Store
	effort = st.Effort
	if st.Attempt != 0 {
		if att, err := s.GetAttempt(ctx, st.Attempt); err == nil {
			if t, err := s.GetTask(ctx, att.Task); err == nil {
				task = t.Key
			}
		}
	}
	if effort == 0 {
		if job, ok := e.jobPayload(st.ID); ok {
			effort = job.Effort
			project = job.Project
		}
	}
	if project == "" && effort != 0 {
		if ef, err := s.GetEffort(ctx, effort); err == nil {
			project = ef.Project
		}
	}
	return project, effort, task
}

// jobPayload finds the stage's lane job payload.
func (e *Engine) jobPayload(stage int64) (StagePayload, bool) {
	idx, err := e.indexJobs()
	if err != nil {
		return StagePayload{}, false
	}
	refs := idx[stage]
	for i := len(refs) - 1; i >= 0; i-- {
		var p StagePayload
		if json.Unmarshal(refs[i].job.Payload, &p) == nil && p.Stage == stage {
			return p, true
		}
	}
	return StagePayload{}, false
}

// SeatStages lists the open seat stages with how to reach each.
func (e *Engine) SeatStages(ctx context.Context) ([]SeatStage, error) {
	out := []SeatStage{}
	if e.deps.Seats == nil {
		return out, nil
	}
	open, err := e.deps.Store.OpenStages(ctx)
	if err != nil {
		return nil, err
	}
	notices := map[int64]runner.SeatNotice{}
	for _, n := range e.deps.Seats.Notices() {
		notices[n.Stage] = n
	}
	for _, st := range open {
		t, ok := e.seatType(ctx, st)
		if !ok {
			continue
		}
		project, effort, task := e.stageWhere(ctx, st)
		ss := SeatStage{Stage: st.ID, Project: project, Effort: effort, Task: task, Role: st.Role,
			Backend: st.Backend, APIType: t, Model: st.Model, Attach: e.deps.Seats.Attach(ctx, t, st.ID)}
		if n, ok := notices[st.ID]; ok {
			n := n
			ss.Waiting = &n
		}
		out = append(out, ss)
	}
	return out, nil
}

func (e *Engine) openSeatStage(ctx context.Context, stage int64) (model.Stage, string, error) {
	st, err := e.deps.Store.GetStage(ctx, stage)
	if err != nil {
		return st, "", err
	}
	t, ok := e.seatType(ctx, st)
	if !ok || st.State == model.StageDone {
		return st, "", ErrNotSeat
	}
	return st, t, nil
}

// AnswerSeat hands the owner's answer to a seat stage's seat.
func (e *Engine) AnswerSeat(ctx context.Context, actor string, stage int64, text string) error {
	st, t, err := e.openSeatStage(ctx, stage)
	if err != nil {
		return err
	}
	if err := e.deps.Seats.Answer(ctx, t, st.ID, e.StageDir(st.ID), text); err != nil {
		return err
	}
	e.auditStage(ctx, actor, "stage.seat_answered", st.ID, nil)
	e.changed()
	return nil
}

// CancelSeat stops a seat stage: its seat first, then its lane job. The stage ends as
// infra:cancel and its attempt is abandoned like any stage the operator stopped.
func (e *Engine) CancelSeat(ctx context.Context, actor string, stage int64) error {
	st, _, err := e.openSeatStage(ctx, stage)
	if err != nil {
		return err
	}
	e.cancelSeat(ctx, st)
	e.submitMu.Lock()
	idx, err := e.indexJobs()
	e.submitMu.Unlock()
	if err != nil {
		return err
	}
	for _, ref := range idx[st.ID] {
		if !jobTerminal(ref.job.Status) {
			_, _ = ref.lane.Cancel(ref.job.ID)
		}
	}
	cur, err := e.deps.Store.GetStage(ctx, st.ID)
	if err == nil && cur.State != model.StageDone {
		err = e.deps.Store.FinishStage(ctx, st.ID, OutcomeInfraCancel, "seat stage cancelled by "+actor, nil)
	}
	if err != nil && !errors.Is(err, store.ErrConflict) {
		return err
	}
	e.auditStage(ctx, actor, "stage.seat_cancelled", st.ID, nil)
	if e.deps.Git != nil {
		_ = e.Advance(context.WithoutCancel(ctx))
	}
	e.changed()
	return nil
}

// OpenStagesExceptSeats counts the open stages a restart must wait for: a seat stage
// survives a restart, so it is not one of them.
func (e *Engine) OpenStagesExceptSeats(ctx context.Context) (int, error) {
	open, err := e.deps.Store.OpenStages(ctx)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, st := range open {
		if _, ok := e.seatType(ctx, st); ok {
			continue
		}
		n++
	}
	return n, nil
}

// seatAttention turns the seat notices into attention items.
func (e *Engine) seatAttention(ctx context.Context) []AttentionItem {
	if e.deps.Seats == nil {
		return nil
	}
	var out []AttentionItem
	for _, n := range e.deps.Seats.Notices() {
		st, err := e.deps.Store.GetStage(ctx, n.Stage)
		if err != nil || st.State == model.StageDone {
			continue
		}
		project, effort, task := e.stageWhere(ctx, st)
		msg := fmt.Sprintf("Stage %d (%s on %s): %s", st.ID, st.Role, st.Backend, n.Message)
		if n.Attach != "" {
			msg += " — " + n.Attach
		}
		out = append(out, AttentionItem{Project: project, Effort: effort, Task: task, Kind: n.Kind,
			Message: msg, Since: n.Since.UnixMilli(), Stage: st.ID})
	}
	return out
}

// auditStage records an audit event about a stage; a failure to record is not the caller's.
func (e *Engine) auditStage(ctx context.Context, actor, action string, stage int64, detail any) {
	_ = e.deps.Store.WithTx(context.WithoutCancel(ctx), func(tx *sql.Tx) error {
		_, err := store.AppendEvent(tx, actor, action, "stage", strconv.FormatInt(stage, 10), detail)
		return err
	})
}
