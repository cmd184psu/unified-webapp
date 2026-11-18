package engine

import (
	"context"
	"encoding/json"
	"errors"

	"cmd184psu/unified-webapp/internal/agentflow/model"
	"cmd184psu/unified-webapp/internal/taskmaster/golane"
)

// StageResult is what an Executor reports for an engine-owned stage.
type StageResult struct {
	Outcome string
	Raw     string
	Parsed  json.RawMessage
	// Tokens is what the stage's runner calls used (AT12).
	Tokens int64

	// cause is the failure behind an infra or timeout outcome; skip marks a
	// stage somebody else already closed, which is left alone.
	cause error
	skip  bool
}

// Executor runs an engine-owned stage (verify, merge). The runtime around it
// supplies the stale-hash check, panic recovery, outcome write-before-return
// and the dispatcher kick; the executor only does the work. An error return is
// classified like a runner failure (cancel when the job context is done,
// otherwise infra:crash).
type Executor interface {
	Execute(ctx context.Context, rc golane.RunContext, st model.Stage, p StagePayload) (StageResult, error)
}

var errNoExecutor = errors.New("no executor registered for this stage kind")

func validatePayload(p StagePayload) error {
	if p.Stage <= 0 {
		return errors.New("stage id required")
	}
	return nil
}

// kind builds one lane-agnostic kind: the backend and model come from the
// stage row, so the same Run serves every lane that registers it.
func (e *Engine) kind(name string) golane.Kind {
	return golane.NewKind[StagePayload](name, 1, validatePayload, func(ctx context.Context, rc golane.RunContext, p StagePayload) (any, error) {
		return e.runStage(ctx, rc, p)
	})
}
