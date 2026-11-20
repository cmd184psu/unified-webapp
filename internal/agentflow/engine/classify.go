package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"cmd184psu/unified-webapp/internal/agentflow/runner"
	"cmd184psu/unified-webapp/internal/agentflow/schema"
)

// Stage outcome classes. Anything starting with "infra:" is an infrastructure
// failure: it never consumes an attempt and is retried (AT7). Discarded stages
// are not counted either (QA2). Timeout is the one non-infra failure the
// engine itself classifies: a worker that ran out of time failed its attempt.
const (
	OutcomePass      = "pass"
	OutcomeRepair    = "repair"
	OutcomeBlocked   = "blocked"
	OutcomeTimeout   = "timeout"
	OutcomeDiscarded = "discarded"
	// OutcomePolicyViolation closes a model stage that changed git state it
	// may not touch (GIT5 layer 2); the change was reverted.
	OutcomePolicyViolation = "policy_violation"

	OutcomeInfraRateLimit   = "infra:rate_limit"
	OutcomeInfraUnreachable = "infra:unreachable"
	OutcomeInfraCrash       = "infra:crash"
	OutcomeInfraCancel      = "infra:cancel"
	OutcomeInfraTimeout     = "infra:timeout"
	OutcomeInfraUnparseable = "infra:unparseable"
	// OutcomeInfraContext: the host's context window filled up and cut the
	// model off mid-task. Not the model's fault, so no strike.
	OutcomeInfraContext = "infra:context_full"
	OutcomeInfraPanic   = "infra:panic"
	OutcomeInfraLost    = "infra:lost"
)

// IsInfra reports whether a stage outcome is an infrastructure failure.
func IsInfra(outcome string) bool {
	return len(outcome) > 6 && outcome[:6] == "infra:"
}

// classifyRun maps the end of a runner call to a stage outcome. parent is the
// job context: when it is done the stop came from outside (cancel, brake,
// shutdown) and is infra:cancel whatever the runner reported. A role deadline
// is a timeout, an attempt failure for workers and infra for every other role.
// It returns "" when the call succeeded.
func classifyRun(parent context.Context, role string, err error) string {
	if err == nil {
		return ""
	}
	if parent.Err() != nil {
		return OutcomeInfraCancel
	}
	var re *runner.Error
	class := runner.ErrNone
	switch {
	case errors.As(err, &re):
		class = re.Class
	case errors.Is(err, context.DeadlineExceeded):
		class = runner.ErrTimeout
	}
	switch class {
	case runner.ErrRateLimit:
		return OutcomeInfraRateLimit
	case runner.ErrUnreachable:
		return OutcomeInfraUnreachable
	case runner.ErrContext:
		return OutcomeInfraContext
	case runner.ErrTimeout:
		// A stage deadline is an infrastructure failure for every role (AF-13 3b): it is
		// retried without spending an attempt, up to max_infra_retries, after which the
		// task needs attention. Counting it as an attempt let one slow model burn every
		// attempt of a task on the same timeout.
		return OutcomeInfraTimeout
	case runner.ErrNone:
		if errors.Is(err, context.Canceled) {
			return OutcomeInfraCancel
		}
	}
	return OutcomeInfraCrash
}

// isStall says the runner gave up because the model went quiet (stalls.go).
func isStall(re *runner.Error) bool {
	return re != nil && strings.HasPrefix(re.Msg, "stalled:")
}

// onlyMinorIssues reports whether the review lists issues and every one is
// minor. A repair with no issues listed is not minor: it keeps its repair.
func onlyMinorIssues(issues []schema.Issue) bool {
	if len(issues) == 0 {
		return false
	}
	for _, i := range issues {
		if i.Severity != "minor" {
			return false
		}
	}
	return true
}

// parseFinal extracts and validates the role's final message; deliberate
// (RP5) makes a planner's plan need its pre-mortem. It returns the
// parsed JSON and the outcome the verdict implies.
func parseFinal(role, final string, deliberate bool) (json.RawMessage, string, error) {
	raw, err := schema.ExtractFinal(final)
	if err != nil {
		return nil, "", err
	}
	switch role {
	case "worker", "cleanup":
		w, err := schema.Parse[schema.WorkerOut](raw)
		if err != nil {
			return nil, "", err
		}
		if w.Blocked != "" || len(w.NeedsFiles) > 0 {
			return raw, OutcomeBlocked, nil
		}
	case "reviewer":
		r, err := schema.Parse[schema.ReviewOut](raw)
		if err != nil {
			return nil, "", err
		}
		switch {
		case r.Blocked != "":
			return raw, OutcomeBlocked, nil
		case r.Verdict == "repair" && !onlyMinorIssues(r.Issues):
			return raw, OutcomeRepair, nil
		}
		// A repair whose issues are all minor passes: the reviewer prompt
		// already says style and wording are never a reason to repair alone,
		// and a model that inflates a nit into a repair costs a full worker
		// cycle. The notes stay in the stored review.
	case "planner":
		_, err = schema.ParsePlan(raw, deliberate)
	case "architect", "critic":
		_, err = schema.Parse[schema.PlanReviewOut](raw)
	case "compiler":
		// A reply the queue would reject (a verify field, a cycle, a
		// dangling dependency, an absolute path) gets the corrective
		// follow-up like any malformed reply (SO2, V1).
		var out schema.CompileOut
		if out, err = schema.Parse[schema.CompileOut](raw); err == nil {
			_, err = compiledTasks(out)
		}
	default:
		err = fmt.Errorf("role %q produces no structured output", role)
	}
	if err != nil {
		return nil, "", err
	}
	return raw, OutcomePass, nil
}
