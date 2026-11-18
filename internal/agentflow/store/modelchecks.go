package store

import "context"

// ModelCheck is the result of the last check of one model (H27): OK when a
// one-word prompt was answered. Class names why it was not: failing (the
// provider errors at once, as a model that was withdrawn does), rate_limited
// (out of quota), unreachable, timeout or empty.
type ModelCheck struct {
	Backend, Model string
	OK             bool
	Class, Message string
	Millis         int64
	Checked        int64
}

// SaveModelCheck records a check result, replacing the model's earlier one.
func (s *Store) SaveModelCheck(ctx context.Context, c ModelCheck) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO model_checks (backend, model, ok, class, message, millis, checked) VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (backend, model) DO UPDATE SET ok = excluded.ok, class = excluded.class, message = excluded.message, millis = excluded.millis, checked = excluded.checked`,
		c.Backend, c.Model, c.OK, c.Class, c.Message, c.Millis, c.Checked)
	return err
}

// ModelChecks lists the last check of every model that has one.
func (s *Store) ModelChecks(ctx context.Context) ([]ModelCheck, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT backend, model, ok, class, message, millis, checked FROM model_checks ORDER BY backend, model`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ModelCheck
	for rows.Next() {
		var c ModelCheck
		if err := rows.Scan(&c.Backend, &c.Model, &c.OK, &c.Class, &c.Message, &c.Millis, &c.Checked); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
