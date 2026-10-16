package store

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
)

// StrikeLimit is the number of uncleared strikes that benches a model in a
// role (H7): three strikes and it is out until the owner clears them.
const StrikeLimit = 3

// Strike kinds.
const (
	StrikeUnparseable = "unparseable" // the stage's output could not be read
	StrikeViolation   = "violation"   // the worker broke the git policy
	StrikeRepeat      = "repeat"      // the same failure twice in a row on one task
)

// StrikeKey names what a strike counts against: a model in a role, wherever it
// runs and whatever it costs.
type StrikeKey struct{ Backend, Model, Role string }

// Strike is one objectively bad act by a model.
type Strike struct {
	StrikeKey
	Kind string `json:"kind"`
	Task int64  `json:"task,omitempty"`
	At   int64  `json:"at"`
}

// Strikes lists every strike since the owner last cleared that model and role.
// It is derived, not stored: unparseable stage output, a git policy violation,
// and an attempt that failed with the same fingerprint as the task's previous
// attempt under the same worker.
func (s *Store) Strikes(ctx context.Context) ([]Strike, error) {
	var out []Strike
	rows, err := s.db.QueryContext(ctx, `SELECT backend, model, role, finished FROM stages
		WHERE state = 'done' AND outcome = 'infra:unparseable' AND model <> ''`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var k Strike
		if err := rows.Scan(&k.Backend, &k.Model, &k.Role, &k.At); err != nil {
			rows.Close()
			return nil, err
		}
		k.Kind = StrikeUnparseable
		out = append(out, k)
	}
	if err := closeRows(rows); err != nil {
		return nil, err
	}
	const lastWorker = `JOIN stages s ON s.id = (SELECT MAX(id) FROM stages WHERE attempt = a.id AND role = 'worker')`
	rows, err = s.db.QueryContext(ctx, `SELECT s.backend, s.model, a.task, a.finished FROM attempts a `+lastWorker+`
		WHERE a.outcome = 'policy_violation' AND s.model <> ''`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var k Strike
		if err := rows.Scan(&k.Backend, &k.Model, &k.Task, &k.At); err != nil {
			rows.Close()
			return nil, err
		}
		k.Role, k.Kind = "worker", StrikeViolation
		out = append(out, k)
	}
	if err := closeRows(rows); err != nil {
		return nil, err
	}
	rows, err = s.db.QueryContext(ctx, `SELECT a.task, a.n, a.fingerprint, s.backend, s.model, a.finished FROM attempts a `+lastWorker+`
		WHERE a.outcome = 'failed' AND a.fingerprint <> '' AND s.model <> '' ORDER BY a.task, a.n`)
	if err != nil {
		return nil, err
	}
	var prev struct {
		task    int64
		n       int
		fp      string
		backend string
		model   string
	}
	for rows.Next() {
		var task int64
		var n int
		var fp, backend, mdl string
		var at int64
		if err := rows.Scan(&task, &n, &fp, &backend, &mdl, &at); err != nil {
			rows.Close()
			return nil, err
		}
		if prev.task == task && prev.n == n-1 && prev.fp == fp && prev.backend == backend && prev.model == mdl {
			out = append(out, Strike{StrikeKey: StrikeKey{backend, mdl, "worker"}, Kind: StrikeRepeat, Task: task, At: at})
		}
		prev.task, prev.n, prev.fp, prev.backend, prev.model = task, n, fp, backend, mdl
	}
	if err := closeRows(rows); err != nil {
		return nil, err
	}
	resets, err := s.strikeResets(ctx)
	if err != nil {
		return nil, err
	}
	out = slices.DeleteFunc(out, func(k Strike) bool { return k.At <= resets[k.StrikeKey] })
	slices.SortStableFunc(out, func(a, b Strike) int { return int(a.At - b.At) })
	return out, nil
}

func closeRows(rows *sql.Rows) error {
	err := rows.Err()
	if cerr := rows.Close(); err == nil {
		err = cerr
	}
	return err
}

func (s *Store) strikeResets(ctx context.Context) (map[StrikeKey]int64, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT backend, model, role, at FROM strike_resets`)
	if err != nil {
		return nil, err
	}
	out := map[StrikeKey]int64{}
	for rows.Next() {
		var k StrikeKey
		var at int64
		if err := rows.Scan(&k.Backend, &k.Model, &k.Role, &at); err != nil {
			rows.Close()
			return nil, err
		}
		out[k] = at
	}
	return out, closeRows(rows)
}

// StrikeCounts is the number of uncleared strikes per model and role.
func (s *Store) StrikeCounts(ctx context.Context) (map[StrikeKey]int, error) {
	list, err := s.Strikes(ctx)
	if err != nil {
		return nil, err
	}
	out := map[StrikeKey]int{}
	for _, k := range list {
		out[k.StrikeKey]++
	}
	return out, nil
}

// ClearStrikes forgives a model in a role: strikes up to now stop counting.
func (s *Store) ClearStrikes(ctx context.Context, actor string, k StrikeKey) error {
	if k.Backend == "" || k.Model == "" || k.Role == "" {
		return Invalid("strikes", "backend, model and role are required")
	}
	return s.WithTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO strike_resets (backend, model, role, at) VALUES (?, ?, ?, ?)
			ON CONFLICT(backend, model, role) DO UPDATE SET at = excluded.at`, k.Backend, k.Model, k.Role, nowMS()); err != nil {
			return err
		}
		_, err := AppendEvent(tx, actor, "strikes.clear", "model", fmt.Sprintf("%s/%s/%s", k.Backend, k.Model, k.Role), nil)
		return err
	})
}
