package engine

import (
	"context"

	"cmd184psu/unified-webapp/internal/agentflow/model"
)

// TaskStatuses derives the status of every task of the effort's queue with
// the project's effective limits, for the API's board and overview.
func (e *Engine) TaskStatuses(ctx context.Context, project string, effort int64) (map[string]model.Status, error) {
	set, err := e.deps.Store.EffectiveSettings(ctx, project)
	if err != nil {
		return nil, err
	}
	return e.deps.Store.TaskStatuses(ctx, effort, e.limitsFor(set))
}
