package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"cmd184psu/unified-webapp/internal/agentflow/model"
	"cmd184psu/unified-webapp/internal/agentflow/store"
)

// AttentionItem is one thing that has stopped on its own and needs the owner:
// a draft, plan or compile that gave up, a task that exhausted its attempts, a
// task no model can take. Nothing else tells the owner; a halted planning gate
// once sat for eight hours.
type AttentionItem struct {
	Project string `json:"project"`
	Effort  int64  `json:"effort"`
	// Task is the task key, empty for an item about the whole effort.
	Task string `json:"task,omitempty"`
	// Kind is stalled, draft_failed, plan_halted, compile_halted, task_halted,
	// task_unassignable, seat_question or seat_silent.
	Kind    string `json:"kind"`
	Message string `json:"message"`
	// Since is when it happened (Unix ms) when the engine knows; 0 otherwise.
	Since int64 `json:"since"`
	// Stage is the stage the item is about, for a seat stage waiting on the owner.
	Stage int64 `json:"stage,omitempty"`
}

// Attention lists everything that needs the owner, newest first. It is derived
// from the audit log and task state, not stored, so it is right after a restart
// and clears itself when the owner acts.
func (e *Engine) Attention(ctx context.Context) ([]AttentionItem, error) {
	s := e.deps.Store
	projects, err := s.ListProjects(ctx)
	if err != nil {
		return nil, err
	}
	out := []AttentionItem{}
	for _, p := range projects {
		efforts, err := s.ListEfforts(ctx, p.Slug)
		if err != nil {
			return nil, err
		}
		set, err := s.EffectiveSettings(ctx, p.Slug)
		if err != nil {
			return nil, err
		}
		for _, ef := range efforts {
			if ef.Status == model.EffortComplete || ef.Status == model.EffortAbandoned {
				continue
			}
			items, err := e.effortAttention(ctx, p.Slug, ef, e.limitsFor(set))
			if err != nil {
				return nil, err
			}
			out = append(out, items...)
		}
	}
	out = append(out, e.seatAttention(ctx)...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Since > out[j].Since })
	return out, nil
}

func (e *Engine) effortAttention(ctx context.Context, project string, ef model.Effort, lim model.Limits) ([]AttentionItem, error) {
	evs, err := e.deps.Store.ListEvents(ctx, "effort", strconv.FormatInt(ef.ID, 10), 100000)
	if err != nil {
		return nil, err
	}
	// the newest event of each kind, to tell "halted after it started" from "started again"
	last := map[string]store.Event{}
	for _, ev := range evs {
		last[ev.Action] = ev
	}
	reasonOf := func(ev store.Event) string {
		var d struct {
			Reason string `json:"reason"`
		}
		_ = json.Unmarshal([]byte(ev.DetailJSON), &d)
		return d.Reason
	}
	later := func(a, b string) bool { return last[a].ID > last[b].ID } // a happened after b (b may never have)
	var out []AttentionItem
	add := func(kind, task, msg string, at time.Time) {
		out = append(out, AttentionItem{Project: project, Effort: ef.ID, Task: task, Kind: kind, Message: msg, Since: at.UnixMilli()})
	}
	if ss, err := e.stallSettingsFor(ctx, project); err == nil {
		if at, reasons, err := e.IdleStall(ctx, project, ef, ss); err == nil && !at.IsZero() {
			msg := fmt.Sprintf("Nothing has moved for %d minutes", int(e.deps.Clock().Sub(at)/stallUnit))
			if len(reasons) > 0 {
				msg += ": " + strings.Join(reasons, "; ")
			}
			add("stalled", "", msg, at)
		}
	}
	switch ef.Status {
	case model.EffortDraft:
		if later(eventDraftFailed, eventDraftStart) && later(eventDraftFailed, eventDraftDone) {
			add("draft_failed", "", "The FRD draft failed: "+reasonOf(last[eventDraftFailed]), last[eventDraftFailed].TS)
		}
		// a halted plan drops the effort back to draft, so it is reported here too
		if later(eventPlanHalted, eventPlanStart) {
			add("plan_halted", "", "Planning halted: "+reasonOf(last[eventPlanHalted]), last[eventPlanHalted].TS)
		}
	case model.EffortPlanning:
		if later(eventPlanHalted, eventPlanStart) {
			add("plan_halted", "", "Planning halted: "+reasonOf(last[eventPlanHalted]), last[eventPlanHalted].TS)
		}
	case model.EffortPlanApproved, model.EffortCompiling, model.EffortQueueDraft:
		if later(eventCompileHalted, eventCompileStart) {
			add("compile_halted", "", "Compiling the task queue halted: "+reasonOf(last[eventCompileHalted]), last[eventCompileHalted].TS)
		}
	case model.EffortRunning, model.EffortPaused:
		status, err := e.deps.Store.TaskStatuses(ctx, ef.ID, lim)
		if err != nil {
			return nil, err
		}
		tasks, err := e.deps.Store.ListTasks(ctx, ef.ID)
		if err != nil {
			return nil, err
		}
		for _, t := range tasks {
			st := status[t.Key]
			switch {
			case st.Halted():
				add("task_halted", t.Key, fmt.Sprintf("%s %q is %s", t.Key, t.Title, st), time.Time{})
			case !st.Done() && strings.HasPrefix(t.WaitingReason, "no eligible backend"):
				add("task_unassignable", t.Key, fmt.Sprintf("%s %q is waiting: %s", t.Key, t.Title, t.WaitingReason), time.Time{})
			}
		}
	}
	return out, nil
}
