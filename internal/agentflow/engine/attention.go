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
	"cmd184psu/unified-webapp/internal/agentflow/schema"
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
	// Backend is the backend an item is about (a metered cap), empty for an effort item.
	Backend string `json:"backend,omitempty"`
	// Kind is started_from_backlog, stalled, draft_failed, plan_halted, compile_halted, task_halted,
	// task_needs_files, task_resolved, task_unassignable, seat_question, seat_silent, service_hold or
	// metered_cap (Project and Effort are then zero).
	Kind    string `json:"kind"`
	Message string `json:"message"`
	// Since is when it happened (Unix ms) when the engine knows; 0 otherwise.
	Since int64 `json:"since"`
	// Stage is the stage the item is about, for a seat stage waiting on the owner.
	Stage int64 `json:"stage,omitempty"`
	// NeedsFiles are the files a task_needs_files item asks the owner to allow (AF-13), with
	// the worker's reasons; POST .../tasks/{key}/allow-files allows them.
	NeedsFiles []schema.NeededFile `json:"needs_files,omitempty"`
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
	if h, err := s.ServiceHold(ctx); err != nil {
		return nil, err
	} else if h.Active {
		// A held service starts nothing anywhere: one item, not one per project.
		out = append(out, AttentionItem{Kind: "service_hold", Message: "The service is held: " + h.Reason, Since: h.Since})
	}
	now := e.deps.Clock()
	backends, err := s.ListBackends(ctx)
	if err != nil {
		return nil, err
	}
	for _, b := range backends {
		if b.Retired || !b.Metered {
			continue
		}
		u, err := s.BackendUsage(ctx, b.Name, now)
		if err != nil {
			return nil, err
		}
		if atCap, until, why := u.AtCap(b.DailyTokenCap, b.MonthlyTokenCap); atCap {
			since := u.DayStart
			if why == "monthly" || why == "daily and monthly" {
				since = u.MonthStart
			}
			out = append(out, AttentionItem{Backend: b.Name, Kind: "metered_cap", Since: since,
				Message: fmt.Sprintf("Backend %s reached its %s token cap; it rests until %s", b.Name, why, time.UnixMilli(until).UTC().Format("2006-01-02 15:04 MST"))})
		}
	}
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
	if ev, ok := last[eventBacklogStart]; ok && ef.Status == model.EffortRunning {
		// AF-15: say plainly that this run was not asked for by a person.
		add("started_from_backlog", "", "started from backlog", ev.TS)
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
		resolved, err := e.effortEvents(ctx, ef.ID, eventTaskResolved)
		if err != nil {
			return nil, err
		}
		for _, t := range tasks {
			st := status[t.Key]
			// AF-13 3b: every resolver decision on an unfinished task is listed, so the owner
			// sees what was decided for them ("resolved by <model>: <decision>").
			if rs := readResolved(resolved, t.Key); len(rs) > 0 && !st.Done() {
				r := rs[len(rs)-1]
				by := "the resolver"
				if r.Model != "" {
					by = r.Model + " on " + r.Backend
				}
				what := "resolved by"
				if r.Advised && !r.Applied {
					what = "has advice waiting to be applied from" // resolver_mode advise
				}
				add("task_resolved", t.Key, fmt.Sprintf("%s %q %s %s: %s: %s", t.Key, t.Title, what, by, r.Decision, r.Reason), time.Time{})
			}
			switch {
			case st.Halted():
				if nf, err := e.PendingNeededFiles(ctx, t); err == nil && len(nf) > 0 {
					add("task_needs_files", t.Key, fmt.Sprintf("%s %q %s", t.Key, t.Title, needsFilesReason(nf)), time.Time{})
					out[len(out)-1].NeedsFiles = nf
					continue
				}
				add("task_halted", t.Key, fmt.Sprintf("%s %q is %s", t.Key, t.Title, st), time.Time{})
			case !st.Done() && strings.HasPrefix(t.WaitingReason, "no eligible backend"):
				add("task_unassignable", t.Key, fmt.Sprintf("%s %q is waiting: %s", t.Key, t.Title, t.WaitingReason), time.Time{})
			}
		}
		if ev, ok := last["auto_replan"]; ok {
			var d struct {
				Quote string `json:"quote"`
			}
			_ = json.Unmarshal([]byte(ev.DetailJSON), &d)
			msg := "FRD contradiction"
			if d.Quote != "" {
				msg += ": " + d.Quote
			}
			add("frd_contradiction", "", msg, ev.TS)
		}
	}
	return out, nil
}
