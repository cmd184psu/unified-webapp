package store

import "context"

// ModelHold marks a model whose work waits for it: while it rests, the stages that
// would use it are not handed to a lower candidate, they wait until its rest ends.
type ModelHold struct {
	Backend string `json:"backend"`
	Model   string `json:"model"`
}

// SetModelHold holds or releases a model.
func (s *Store) SetModelHold(ctx context.Context, backend, model string, hold bool) error {
	if backend == "" || model == "" {
		return Invalid("model", "backend and model are required")
	}
	var err error
	if hold {
		_, err = s.db.ExecContext(ctx, `INSERT OR IGNORE INTO model_hold (backend, model) VALUES (?, ?)`, backend, model)
	} else {
		_, err = s.db.ExecContext(ctx, `DELETE FROM model_hold WHERE backend = ? AND model = ?`, backend, model)
	}
	return err
}

// ModelHolds lists the held models.
func (s *Store) ModelHolds(ctx context.Context) ([]ModelHold, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT backend, model FROM model_hold ORDER BY backend, model`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ModelHold
	for rows.Next() {
		var h ModelHold
		if err := rows.Scan(&h.Backend, &h.Model); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}
