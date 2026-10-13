package store

import (
	"context"
	"database/sql"
	"slices"

	"cmd184psu/unified-webapp/internal/agentflow/model"
)

// RecordRunState writes a run-controller transition: the audit event for the
// project and, when effort is not 0, the last run state kept on that effort
// for display only (RC1). Both commit together.
func (s *Store) RecordRunState(ctx context.Context, actor, action, project string, effort int64, state string) error {
	if !slices.Contains(model.RunStates, state) {
		return Invalid("run_state", "unknown run state")
	}
	return s.WithTx(ctx, func(tx *sql.Tx) error {
		if effort != 0 {
			if _, err := tx.ExecContext(ctx, `UPDATE efforts SET run_state = ? WHERE id = ?`, state, effort); err != nil {
				return err
			}
		}
		_, err := AppendEvent(tx, actor, action, "project", project, map[string]any{"state": state, "effort": effort})
		return err
	})
}
