package store

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
)

// StrikeLimit is the number of strikes in a row that benches a model in a role
// (H7), like failed password attempts: a success in between resets the count,
// and a benched model stays out until the owner clears it.
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

// Strike is one objectively bad act by a model, kept for ever. Counted says
// whether it is part of the model's current streak: a success by the model in
// that role (an attempt that passed, or a planning or review stage that
// passed) or the owner's Clear ends the streak, and strikes before that stay
// in the history with Ended saying why they no longer count.
type Strike struct {
	StrikeKey
	Kind    string `json:"kind"`
	Task    int64  `json:"task,omitempty"`
	Effort  int64  `json:"effort,omitempty"`
	Project string `json:"project,omitempty"`
	At      int64  `json:"at"`
	Counted bool   `json:"counted"`
	Ended   string `json:"ended,omitempty"` // "success" or "cleared"; empty while counted
}

// Strikes lists every strike, oldest first. They are derived, not stored:
// unparseable stage output, a git policy violation, and an attempt that failed
// with the same verify or review fingerprint as the task's previous attempt
// under the same worker. A timeout, a merge conflict or a refused commit is
// not the model's doing and never makes a strike, nor does an unreachable host.
func (s *Store) Strikes(ctx context.Context) ([]Strike, error) {
	var out []Strike
	// The effort and project of a stage or an attempt's task.
	const stageEffort = ` LEFT JOIN attempts at ON at.id = s.attempt LEFT JOIN tasks t ON t.id = at.task
		LEFT JOIN queues q ON q.id = t.queue LEFT JOIN efforts e ON e.id = COALESCE(s.effort, q.effort) `
	const attEffort = ` JOIN tasks t ON t.id = a.task JOIN queues q ON q.id = t.queue JOIN efforts e ON e.id = q.effort `
	const lastWorker = ` JOIN stages s ON s.id = (SELECT MAX(id) FROM stages WHERE attempt = a.id AND role = 'worker') `

	rows, err := s.db.QueryContext(ctx, `SELECT s.backend, s.model, s.role, COALESCE(t.id, 0), COALESCE(e.id, 0), COALESCE(e.project, ''), s.finished
		FROM stages s`+stageEffort+`WHERE s.state = 'done' AND s.outcome = 'infra:unparseable' AND s.model <> ''`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var k Strike
		if err := rows.Scan(&k.Backend, &k.Model, &k.Role, &k.Task, &k.Effort, &k.Project, &k.At); err != nil {
			rows.Close()
			return nil, err
		}
		k.Kind = StrikeUnparseable
		out = append(out, k)
	}
	if err := closeRows(rows); err != nil {
		return nil, err
	}
	rows, err = s.db.QueryContext(ctx, `SELECT s.backend, s.model, a.task, e.id, e.project, a.finished FROM attempts a`+attEffort+lastWorker+`
		WHERE a.outcome = 'policy_violation' AND s.model <> ''`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var k Strike
		if err := rows.Scan(&k.Backend, &k.Model, &k.Task, &k.Effort, &k.Project, &k.At); err != nil {
			rows.Close()
			return nil, err
		}
		k.Role, k.Kind = "worker", StrikeViolation
		out = append(out, k)
	}
	if err := closeRows(rows); err != nil {
		return nil, err
	}
	rows, err = s.db.QueryContext(ctx, `SELECT a.task, a.n, a.fingerprint, s.backend, s.model, e.id, e.project, a.finished FROM attempts a`+attEffort+lastWorker+`
		WHERE a.outcome = 'failed' AND (a.fingerprint LIKE 'v:%' OR a.fingerprint LIKE 'r:%') AND s.model <> '' ORDER BY a.task, a.n`)
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
		var task, effort, at int64
		var n int
		var fp, backend, mdl, project string
		if err := rows.Scan(&task, &n, &fp, &backend, &mdl, &effort, &project, &at); err != nil {
			rows.Close()
			return nil, err
		}
		if prev.task == task && prev.n == n-1 && prev.fp == fp && prev.backend == backend && prev.model == mdl {
			out = append(out, Strike{StrikeKey: StrikeKey{backend, mdl, "worker"}, Kind: StrikeRepeat, Task: task, Effort: effort, Project: project, At: at})
		}
		prev.task, prev.n, prev.fp, prev.backend, prev.model = task, n, fp, backend, mdl
	}
	if err := closeRows(rows); err != nil {
		return nil, err
	}

	// A success ends a streak: a worker's attempt that passed, or a stage of
	// any other model role that passed.
	last := map[StrikeKey]int64{}
	note := func(k StrikeKey, at int64) {
		if at > last[k] {
			last[k] = at
		}
	}
	rows, err = s.db.QueryContext(ctx, `SELECT backend, model, role, finished FROM stages
		WHERE state = 'done' AND outcome = 'pass' AND model <> '' AND role NOT IN ('worker', 'verify', 'merge', 'cleanup')`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var k StrikeKey
		var at int64
		if err := rows.Scan(&k.Backend, &k.Model, &k.Role, &at); err != nil {
			rows.Close()
			return nil, err
		}
		note(k, at)
	}
	if err := closeRows(rows); err != nil {
		return nil, err
	}
	rows, err = s.db.QueryContext(ctx, `SELECT s.backend, s.model, a.finished FROM attempts a`+lastWorker+`WHERE a.outcome = 'passed' AND s.model <> ''`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var k StrikeKey
		var at int64
		k.Role = "worker"
		if err := rows.Scan(&k.Backend, &k.Model, &at); err != nil {
			rows.Close()
			return nil, err
		}
		note(k, at)
	}
	if err := closeRows(rows); err != nil {
		return nil, err
	}
	resets, err := s.strikeResets(ctx)
	if err != nil {
		return nil, err
	}
	for i := range out {
		k := out[i]
		switch {
		case k.At <= resets[k.StrikeKey]:
			out[i].Ended = "cleared"
		case k.At < last[k.StrikeKey]:
			out[i].Ended = "success"
		default:
			out[i].Counted = true
		}
	}
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

// StrikeCounts is each model's current streak per role: the strikes since its
// last success or clear. StrikeLimit of them benches it.
func (s *Store) StrikeCounts(ctx context.Context) (map[StrikeKey]int, error) {
	list, err := s.Strikes(ctx)
	if err != nil {
		return nil, err
	}
	out := map[StrikeKey]int{}
	for _, k := range list {
		if k.Counted {
			out[k.StrikeKey]++
		}
	}
	return out, nil
}

// ClearStrikes forgives a model in a role: its streak goes back to zero. The
// strikes stay in the history, marked cleared.
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
