package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

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
	OutcomeInfraPanic       = "infra:panic"
	OutcomeInfraLost        = "infra:lost"
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
	case runner.ErrTimeout:
		if role == "worker" {
			return OutcomeTimeout
		}
		return OutcomeInfraTimeout
	case runner.ErrNone:
		if errors.Is(err, context.Canceled) {
			return OutcomeInfraCancel
		}
	}
	return OutcomeInfraCrash
}

// parseFinal extracts and validates the role's final message. It returns the
// parsed JSON and the outcome the verdict implies.
func parseFinal(role, final string) (json.RawMessage, string, error) {
	raw, err := schema.ExtractFinal(final)
	if err != nil {
		return nil, "", err
	}
	switch role {
	case "worker":
		w, err := schema.Parse[schema.WorkerOut](raw)
		if err != nil {
			return nil, "", err
		}
		if w.Blocked != "" {
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
		case r.Verdict == "repair":
			return raw, OutcomeRepair, nil
		}
	case "planner":
		_, err = schema.Parse[schema.PlanOut](raw)
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
