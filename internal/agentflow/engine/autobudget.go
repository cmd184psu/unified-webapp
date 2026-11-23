package engine

import (
	"context"
	"strconv"

	"database/sql"

	"cmd184psu/unified-webapp/internal/agentflow/model"
	"cmd184psu/unified-webapp/internal/agentflow/store"
)

const eventAutoBudgetRaise = "auto_budget_raise"

// AutoBudget is the AF-17 tick step: an effort that has used up its budget
// (and whose recent tasks made progress) gets its budget raised by the
// configured percentage, at most the configured number of times, and its run
// restarted. Further raises past the cap are left for a human.
func (e *Engine) AutoBudget(ctx context.Context) error {
	set, err := e.deps.Store.GetSettings(ctx)
	if err != nil || !set.AutoBudget {
		return err
	}
	projects, err := e.deps.Store.ListProjects(ctx)
	if err != nil {
		return err
	}
	for _, p := range projects {
		efforts, err := e.deps.Store.ListEfforts(ctx, p.Slug)
		if err != nil {
			continue
		}
		for _, ef := range efforts {
			if ef.Status == model.EffortComplete || ef.Status == model.EffortAbandoned {
				continue
			}
			b, err := e.EffortBudget(ctx, ef)
			// An effort with no budget is unlimited: it is never raised (AF-20).
			if err != nil || !b.Exhausted() || !b.Set() {
				continue
			}
			// count raises already made
			evs, _ := e.deps.Store.ListEvents(ctx, "effort", strconv.FormatInt(ef.ID, 10), 100000)
			raises := 0
			for _, ev := range evs {
				if ev.Action == eventAutoBudgetRaise {
					raises++
				}
			}
			if raises >= set.BudgetRaiseCap {
				continue
			}
			// progress check: only raise if some task made progress
			if !e.budgetHasProgress(ctx, ef, set) {
				continue
			}
			// progress check: last tasks not all failures (best-effort, skip if unknown)
			newTokens := ef.BudgetTokens
			newSeconds := ef.BudgetSeconds
			if newTokens > 0 {
				newTokens = newTokens * int64(100+set.BudgetRaisePercent) / 100
			}
			if newSeconds > 0 {
				newSeconds = newSeconds * int64(100+set.BudgetRaisePercent) / 100
			}
			if _, err := e.deps.Store.UpdateEffort(ctx, SystemActor, ef.ID, store.EffortPatch{BudgetTokens: &newTokens, BudgetSeconds: &newSeconds}); err != nil {
				continue
			}
			_ = e.deps.Store.WithTx(ctx, func(tx *sql.Tx) error {
				_, err := store.AppendEvent(tx, SystemActor, eventAutoBudgetRaise, "effort", strconv.FormatInt(ef.ID, 10), map[string]any{"raise": raises + 1, "cap": set.BudgetRaiseCap, "percent": set.BudgetRaisePercent})
				return err
			})
			// restart if stopped
			if ef.RunState != model.RunRunning {
				_ = e.runs.Start(ctx, SystemActor, p.Slug)
			}
		}
	}
	return nil
}

func (e *Engine) budgetHasProgress(ctx context.Context, ef model.Effort, set store.Settings) bool {
	lim := e.limitsFor(set)
	status, err := e.deps.Store.TaskStatuses(ctx, ef.ID, lim)
	if err != nil || len(status) == 0 {
		return true
	}
	for _, st := range status {
		if st == model.StatusPassed || st == model.StatusOverridden || st == model.StatusRunning {
			return true
		}
	}
	return false
}
