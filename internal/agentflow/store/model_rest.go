package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// ModelRest is a model that is resting: it hit a rate limit or stalled, so it is
// given no new stage until Until. Streak counts the failures in a row, and each one
// doubles the rest up to a cap.
type ModelRest struct {
	Backend string `json:"backend"`
	Model   string `json:"model"`
	Until   int64  `json:"until"` // unix ms
	Reason  string `json:"reason"`
	Streak  int    `json:"streak"`
	Since   int64  `json:"since"` // unix ms of the first failure in the streak
}

// RestModel puts a model to rest for base doubled once per earlier failure in a row, up
// to max. A streak that ended long ago (two maxes) starts over.
func (s *Store) RestModel(ctx context.Context, backend, model, reason string, base, max time.Duration, now time.Time) (ModelRest, error) {
	r := ModelRest{Backend: backend, Model: model, Reason: reason, Streak: 1, Since: now.UnixMilli()}
	var prev ModelRest
	err := s.db.QueryRowContext(ctx, `SELECT until, streak, since FROM model_rest WHERE backend = ? AND model = ?`, backend, model).
		Scan(&prev.Until, &prev.Streak, &prev.Since)
	switch {
	case err == nil && prev.Until > now.Add(-2*max).UnixMilli():
		r.Streak, r.Since = prev.Streak+1, prev.Since
	case err != nil && !errors.Is(err, sql.ErrNoRows):
		return r, err
	}
	d := base
	for i := 1; i < r.Streak && d < max; i++ {
		d *= 2
	}
	if d > max {
		d = max
	}
	r.Until = now.Add(d).UnixMilli()
	_, err = s.db.ExecContext(ctx, `INSERT INTO model_rest (backend, model, until, reason, streak, since) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (backend, model) DO UPDATE SET until = excluded.until, reason = excluded.reason, streak = excluded.streak, since = excluded.since`,
		r.Backend, r.Model, r.Until, r.Reason, r.Streak, r.Since)
	return r, err
}

// RestModelUntil rests a model until an explicit time, used by a metered
// backend's token cap (AF-20). It is idempotent: an existing row keeps its
// streak and since, and only the reason and until are set.
func (s *Store) RestModelUntil(ctx context.Context, backend, model, reason string, until, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO model_rest (backend, model, until, reason, streak, since) VALUES (?, ?, ?, ?, 1, ?)
		ON CONFLICT (backend, model) DO UPDATE SET until = excluded.until, reason = excluded.reason`,
		backend, model, until.UnixMilli(), reason, now.UnixMilli())
	return err
}

// ClearModelRest ends a model's rest and its streak; it reports whether there was one.
func (s *Store) ClearModelRest(ctx context.Context, backend, model string) (bool, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM model_rest WHERE backend = ? AND model = ?`, backend, model)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// ModelRests lists every model with a rest row, resting now or not.
func (s *Store) ModelRests(ctx context.Context) ([]ModelRest, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT backend, model, until, reason, streak, since FROM model_rest ORDER BY until DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ModelRest
	for rows.Next() {
		var r ModelRest
		if err := rows.Scan(&r.Backend, &r.Model, &r.Until, &r.Reason, &r.Streak, &r.Since); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
