package store

import (
	"context"
	"database/sql"
)

// ReviewerEval is one run of a seeded-defect case against a reviewer model
// (H32). Variant is "defect" or "fixed". Outcome is caught or missed for a
// defect, clean or false_alarm for a fixed variant, and unparseable or failed
// when no verdict was read.
type ReviewerEval struct {
	Backend, Model, Layer, Case, Variant string
	Run                                  int
	Outcome, Detail                      string
	Millis, Ran                          int64
}

// ReplaceReviewerEvals replaces the rows of one model and layer with rows.
func (s *Store) ReplaceReviewerEvals(ctx context.Context, backend, model, layer string, rows []ReviewerEval) error {
	return s.WithTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM reviewer_evals WHERE backend = ? AND model = ? AND layer = ?`, backend, model, layer); err != nil {
			return err
		}
		for _, r := range rows {
			if _, err := tx.ExecContext(ctx, `INSERT INTO reviewer_evals (backend, model, layer, case_id, variant, run, outcome, detail, millis, ran) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				backend, model, layer, r.Case, r.Variant, r.Run, r.Outcome, r.Detail, r.Millis, r.Ran); err != nil {
				return err
			}
		}
		return nil
	})
}

// ReviewerEvals lists the stored rows of a backend.
func (s *Store) ReviewerEvals(ctx context.Context, backend string) ([]ReviewerEval, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT backend, model, layer, case_id, variant, run, outcome, detail, millis, ran FROM reviewer_evals WHERE backend = ? ORDER BY model, layer, case_id, variant, run`, backend)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ReviewerEval
	for rows.Next() {
		var r ReviewerEval
		if err := rows.Scan(&r.Backend, &r.Model, &r.Layer, &r.Case, &r.Variant, &r.Run, &r.Outcome, &r.Detail, &r.Millis, &r.Ran); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
