package store

import (
	"context"
	"fmt"
)

// RecordStageTokens stores the runner-reported token usage of a stage (AT12).
// It is written before the stage's outcome, so the dispatcher, which is kicked
// after the outcome, always sees it.
func (s *Store) RecordStageTokens(ctx context.Context, stage, tokens int64) error {
	if tokens < 0 {
		return Invalid("tokens", "must not be negative")
	}
	res, err := s.db.ExecContext(ctx, `UPDATE stages SET tokens = ? WHERE id = ?`, tokens, stage)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("stage %d: %w", stage, ErrNotFound)
	}
	return nil
}

// EffortUsage totals what an effort has consumed: runner tokens, and the
// seconds its stages have run (stages in parallel add up; a stage still
// running counts up to nowMS). Both cover the effort's own stages (plan,
// review, compile) and those of every attempt of its tasks.
func (s *Store) EffortUsage(ctx context.Context, effort, nowMS int64) (tokens, seconds int64, err error) {
	err = s.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(tokens), 0),
		       COALESCE(SUM(CASE WHEN started = 0 THEN 0
		                         WHEN finished > 0 THEN MAX(finished - started, 0)
		                         ELSE MAX(? - started, 0) END), 0) / 1000
		FROM stages
		WHERE effort = ?
		   OR attempt IN (SELECT a.id FROM attempts a
		                    JOIN tasks t ON t.id = a.task
		                    JOIN queues q ON q.id = t.queue
		                   WHERE q.effort = ?)`, nowMS, effort, effort).Scan(&tokens, &seconds)
	return tokens, seconds, err
}
