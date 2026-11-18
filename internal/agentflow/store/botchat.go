package store

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
)

// BotchatState reads one value of the relay's own bookkeeping, or "" when unset.
func (s *Store) BotchatState(ctx context.Context, key string) (string, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM botchat_state WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return v, err
}

// SetBotchatState stores one value of the relay's own bookkeeping.
func (s *Store) SetBotchatState(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO botchat_state (key, value) VALUES (?, ?)
		ON CONFLICT (key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

// FinishedStage is a stage that ended, with the task it belonged to, for the BotChat relay.
type FinishedStage struct {
	ID       int64
	Effort   int64 // 0 when the stage belongs to no effort
	Task     string
	Attempt  int
	Role     string
	Backend  string
	Model    string
	Outcome  string
	Tokens   int64
	Started  int64
	Finished int64
	// Parsed is the stage's parsed final message (JSON) and RawHead the first 500 bytes of
	// its raw output, which is all a verify or merge stage has to say.
	Parsed  string
	RawHead string
}

// FinishedStagesAfter lists stages that finished after (ts, id) in finish order: the pair is a
// cursor, so two stages that end in the same millisecond are neither skipped nor repeated.
func (s *Store) FinishedStagesAfter(ctx context.Context, ts, id int64, limit int) ([]FinishedStage, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT s.id, COALESCE(s.effort, q.effort, 0), COALESCE(t.task_key, ''), COALESCE(a.n, 0),
       s.role, s.backend, s.model, s.outcome, s.tokens, s.started, s.finished, s.parsed_json, substr(s.raw_output, 1, 500)
FROM stages s
LEFT JOIN attempts a ON s.attempt = a.id
LEFT JOIN tasks t ON a.task = t.id
LEFT JOIN queues q ON t.queue = q.id
WHERE s.state = 'done' AND (s.finished > ? OR (s.finished = ? AND s.id > ?))
ORDER BY s.finished, s.id LIMIT ?`, ts, ts, id, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FinishedStage
	for rows.Next() {
		var f FinishedStage
		if err := rows.Scan(&f.ID, &f.Effort, &f.Task, &f.Attempt, &f.Role, &f.Backend, &f.Model, &f.Outcome, &f.Tokens, &f.Started, &f.Finished, &f.Parsed, &f.RawHead); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// RelayEvent is a recorded event for the BotChat relay.
type RelayEvent struct {
	ID          int64
	Action      string
	SubjectType string
	SubjectID   string
	DetailJSON  string
}

// EventsAfter lists the events recorded after id, oldest first.
func (s *Store) EventsAfter(ctx context.Context, id int64, limit int) ([]RelayEvent, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, action, subject_type, subject_id, detail_json FROM events WHERE id > ? ORDER BY id LIMIT ?`, id, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RelayEvent
	for rows.Next() {
		var e RelayEvent
		if err := rows.Scan(&e.ID, &e.Action, &e.SubjectType, &e.SubjectID, &e.DetailJSON); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// RelayHead is where a fresh relay starts so it does not replay history: the newest finished
// stage's (finished, id) and the newest event id.
func (s *Store) RelayHead(ctx context.Context) (ts, stage, event int64, err error) {
	// (the start cursor begins at the same present: see StartHead)
	err = s.db.QueryRowContext(ctx, `SELECT COALESCE(finished, 0), id FROM stages WHERE state = 'done' ORDER BY finished DESC, id DESC LIMIT 1`).Scan(&ts, &stage)
	if errors.Is(err, sql.ErrNoRows) {
		ts, stage, err = 0, 0, nil
	}
	if err != nil {
		return
	}
	err = s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(id), 0) FROM events`).Scan(&event)
	return
}

// itoa64 is for the relay's bookkeeping values.
func Itoa64(n int64) string { return strconv.FormatInt(n, 10) }

// StartedStagesAfter lists stages that started after (ts, id) in start order, whether or not they
// have finished since: a stage too quick to be seen running is still announced.
func (s *Store) StartedStagesAfter(ctx context.Context, ts, id int64, limit int) ([]FinishedStage, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT s.id, COALESCE(s.effort, q.effort, 0), COALESCE(t.task_key, ''), COALESCE(a.n, 0),
       s.role, s.backend, s.model, s.started
FROM stages s
LEFT JOIN attempts a ON s.attempt = a.id
LEFT JOIN tasks t ON a.task = t.id
LEFT JOIN queues q ON t.queue = q.id
WHERE s.started > 0 AND (s.started > ? OR (s.started = ? AND s.id > ?))
ORDER BY s.started, s.id LIMIT ?`, ts, ts, id, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FinishedStage
	for rows.Next() {
		var f FinishedStage
		if err := rows.Scan(&f.ID, &f.Effort, &f.Task, &f.Attempt, &f.Role, &f.Backend, &f.Model, &f.Started); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// StartHead is the newest started stage's (started, id), where a fresh relay's start cursor begins.
func (s *Store) StartHead(ctx context.Context) (ts, id int64, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT started, id FROM stages WHERE started > 0 ORDER BY started DESC, id DESC LIMIT 1`).Scan(&ts, &id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, 0, nil
	}
	return
}
