package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"

	"cmd184psu/unified-webapp/internal/agentflow/model"
)

const attemptCols = `id, task, n, kind, worktree, outcome, fingerprint, worker_model, reviewer_model, started, finished`

func scanAttempt(r interface{ Scan(...any) error }) (model.Attempt, error) {
	var a model.Attempt
	err := r.Scan(&a.ID, &a.Task, &a.N, &a.Kind, &a.Worktree, &a.Outcome, &a.Fingerprint, &a.WorkerModel, &a.ReviewerModel, &a.Started, &a.Finished)
	a.ReviewIndependence = model.ReviewIndependence(a.WorkerModel, a.ReviewerModel)
	return a, err
}

// StartAttempt opens the task's next attempt. A task has at most one attempt
// in flight (ErrConflict otherwise); different tasks run attempts concurrently
// (PX1).
func (s *Store) StartAttempt(ctx context.Context, taskID int64, kind, worktree, workerModel string) (model.Attempt, error) {
	if kind != model.AttemptNormal && kind != model.AttemptRepair {
		return model.Attempt{}, Invalid("kind", "must be normal or repair")
	}
	var out model.Attempt
	err := s.WithTx(ctx, func(tx *sql.Tx) error {
		if err := requireTask(ctx, tx, taskID); err != nil {
			return err
		}
		var open, n int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FILTER (WHERE outcome = ''), COALESCE(MAX(n), 0) + 1 FROM attempts WHERE task = ?`, taskID).Scan(&open, &n); err != nil {
			return err
		}
		if open > 0 {
			return fmt.Errorf("task %d already has an attempt in flight: %w", taskID, ErrConflict)
		}
		res, err := tx.ExecContext(ctx, `INSERT INTO attempts (task, n, kind, worktree, worker_model, started) VALUES (?, ?, ?, ?, ?, ?)`,
			taskID, n, kind, worktree, workerModel, nowMS())
		if err != nil {
			return err
		}
		id, _ := res.LastInsertId()
		out, err = scanAttempt(tx.QueryRowContext(ctx, `SELECT `+attemptCols+` FROM attempts WHERE id = ?`, id))
		return err
	})
	return out, err
}

// FinishAttempt records an attempt's outcome. Finishing twice is ErrConflict.
func (s *Store) FinishAttempt(ctx context.Context, id int64, outcome, fingerprint, reviewerModel string) error {
	if !slices.Contains([]string{model.OutcomePassed, model.OutcomeFailed, model.OutcomeBlocked, model.OutcomePolicyViolation, model.OutcomeAbandoned}, outcome) {
		return Invalid("outcome", "unknown attempt outcome")
	}
	return s.WithTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE attempts SET outcome = ?, fingerprint = ?, reviewer_model = ?, finished = ?
			WHERE id = ? AND outcome = ''`, outcome, fingerprint, reviewerModel, nowMS(), id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			var c int
			if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM attempts WHERE id = ?`, id).Scan(&c); err != nil {
				return err
			}
			if c == 0 {
				return fmt.Errorf("attempt %d: %w", id, ErrNotFound)
			}
			return fmt.Errorf("attempt %d is already finished: %w", id, ErrConflict)
		}
		return nil
	})
}

// GetAttempt returns one attempt or ErrNotFound.
func (s *Store) GetAttempt(ctx context.Context, id int64) (model.Attempt, error) {
	a, err := scanAttempt(s.db.QueryRowContext(ctx, `SELECT `+attemptCols+` FROM attempts WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return a, fmt.Errorf("attempt %d: %w", id, ErrNotFound)
	}
	return a, err
}

// ListAttempts returns a task's attempts ordered by n.
func (s *Store) ListAttempts(ctx context.Context, taskID int64) ([]model.Attempt, error) {
	return listAttempts(ctx, s.db, taskID)
}

func listAttempts(ctx context.Context, q querier, taskID int64) ([]model.Attempt, error) {
	rows, err := q.QueryContext(ctx, `SELECT `+attemptCols+` FROM attempts WHERE task = ? ORDER BY n`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Attempt
	for rows.Next() {
		a, err := scanAttempt(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
