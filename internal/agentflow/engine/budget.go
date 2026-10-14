package engine

import (
	"context"

	"cmd184psu/unified-webapp/internal/agentflow/model"
	"cmd184psu/unified-webapp/internal/agentflow/runner"
)

// Per-effort budgets (AT12). Usage is what the runner reported (tokens) and
// how long the effort's stages ran (seconds, summed, so stages in parallel
// add up). At 90% of either limit the dispatcher starts no new task attempt;
// attempts already in flight run on to their end. At 100% the project's run is
// stopped with the reason "budget". Exhaustion is a stop, not a failure: the
// stop abandons the open attempts, which never count against a task.

// BudgetReason is the stop reason a budget stop reports.
const BudgetReason = "budget"

// Budget is an effort's usage against its limits; a zero limit is no limit.
type Budget struct {
	Tokens       int64 `json:"tokens"`
	Seconds      int64 `json:"seconds"`
	TokenLimit   int64 `json:"token_limit"`
	SecondsLimit int64 `json:"seconds_limit"`
}

// Set reports whether any limit is configured.
func (b Budget) Set() bool { return b.TokenLimit > 0 || b.SecondsLimit > 0 }

// reached says whether usage is at least pct percent of a configured limit.
func (b Budget) reached(pct int64) bool {
	return (b.TokenLimit > 0 && b.Tokens*100 >= b.TokenLimit*pct) ||
		(b.SecondsLimit > 0 && b.Seconds*100 >= b.SecondsLimit*pct)
}

// NoNewTasks is true from 90% of a limit.
func (b Budget) NoNewTasks() bool { return b.reached(90) }

// Exhausted is true from 100% of a limit.
func (b Budget) Exhausted() bool { return b.reached(100) }

// Fraction is the largest used/limit ratio over the configured limits.
func (b Budget) Fraction() float64 {
	var f float64
	if b.TokenLimit > 0 {
		f = float64(b.Tokens) / float64(b.TokenLimit)
	}
	if b.SecondsLimit > 0 {
		if s := float64(b.Seconds) / float64(b.SecondsLimit); s > f {
			f = s
		}
	}
	return f
}

// usageTokens is the token count of one runner call: the total when the
// runner reports one, else input plus output.
func usageTokens(u *runner.Usage) int64 {
	if u == nil {
		return 0
	}
	if u.TotalTokens > 0 {
		return u.TotalTokens
	}
	return u.InputTokens + u.OutputTokens
}

// EffortBudget reads the effort's usage against its limits.
func (e *Engine) EffortBudget(ctx context.Context, ef model.Effort) (Budget, error) {
	b := Budget{TokenLimit: ef.BudgetTokens, SecondsLimit: ef.BudgetSeconds}
	tokens, seconds, err := e.deps.Store.EffortUsage(ctx, ef.ID, e.deps.Clock().UnixMilli())
	b.Tokens, b.Seconds = tokens, seconds
	return b, err
}

// budgetGate reads the budget of an effort that has a limit; an effort
// without one is never gated and costs no query.
func (e *Engine) budgetGate(ctx context.Context, ef model.Effort) (Budget, error) {
	if ef.BudgetTokens == 0 && ef.BudgetSeconds == 0 {
		return Budget{}, nil
	}
	return e.EffortBudget(ctx, ef)
}

// enforceBudget stops the project's run when its active effort's budget is
// used up, and reports whether it did.
func (e *Engine) enforceBudget(ctx context.Context, project string) (bool, error) {
	ef, err := e.deps.Store.ActiveEffort(ctx, project)
	if err != nil {
		return false, nil // no active effort, nothing to budget
	}
	b, err := e.budgetGate(ctx, ef)
	if err != nil || !b.Exhausted() {
		return false, err
	}
	return true, e.runs.stop(ctx, SystemActor, project, BudgetReason)
}
