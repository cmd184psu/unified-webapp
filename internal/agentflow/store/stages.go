package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"cmd184psu/unified-webapp/internal/agentflow/model"
)

// StageSpec describes a stage to queue. Exactly one of Attempt and Effort is
// set: attempt stages are work, verify, review and merge; effort stages are
// plan, review and compile.
type StageSpec struct {
	Attempt   int64
	Effort    int64
	Role      string
	Backend   string
	Model     string
	QueueHash string
	PromptRef string
}

const stageCols = `id, COALESCE(attempt, 0), COALESCE(effort, 0), role, backend, model, lane_job_id, queue_hash, state, outcome, raw_output, parsed_json, prompt_path, started, finished, tokens`

func scanStage(r interface{ Scan(...any) error }) (model.Stage, error) {
	var st model.Stage
	var parsed string
	err := r.Scan(&st.ID, &st.Attempt, &st.Effort, &st.Role, &st.Backend, &st.Model, &st.LaneJobID, &st.QueueHash, &st.State, &st.Outcome, &st.RawOutput, &parsed, &st.PromptRef, &st.Started, &st.Finished, &st.Tokens)
	if parsed != "" {
		st.Parsed = json.RawMessage(parsed)
	}
	return st, err
}

// CreateStage inserts a queued stage.
func (s *Store) CreateStage(ctx context.Context, sp StageSpec) (model.Stage, error) {
	if (sp.Attempt == 0) == (sp.Effort == 0) {
		return model.Stage{}, Invalid("stage", "exactly one of attempt and effort is required")
	}
	if !slices.Contains(model.StageRoles, sp.Role) {
		return model.Stage{}, Invalid("role", "unknown stage role")
	}
	var att, eff any
	if sp.Attempt != 0 {
		att = sp.Attempt
	} else {
		eff = sp.Effort
	}
	var out model.Stage
	err := s.WithTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `INSERT INTO stages (attempt, effort, role, backend, model, queue_hash, prompt_path, state)
			VALUES (?, ?, ?, ?, ?, ?, ?, 'queued')`, att, eff, sp.Role, sp.Backend, sp.Model, sp.QueueHash, sp.PromptRef)
		if err != nil {
			return err
		}
		id, _ := res.LastInsertId()
		out, err = scanStage(tx.QueryRowContext(ctx, `SELECT `+stageCols+` FROM stages WHERE id = ?`, id))
		return err
	})
	return out, err
}

// StartStage marks a queued stage running and records its lane job.
func (s *Store) StartStage(ctx context.Context, id int64, laneJobID string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE stages SET state = 'running', lane_job_id = ?, started = ? WHERE id = ? AND state = 'queued'`, laneJobID, nowMS(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("stage %d is not queued: %w", id, ErrConflict)
	}
	return nil
}

// FinishStage closes a stage with its outcome, raw output and parsed JSON in
// one statement. A stage that is already done is ErrConflict.
func (s *Store) FinishStage(ctx context.Context, id int64, outcome, raw string, parsed json.RawMessage) error {
	if outcome == "" {
		return Invalid("outcome", "required")
	}
	res, err := s.db.ExecContext(ctx, `UPDATE stages SET state = 'done', outcome = ?, raw_output = ?, parsed_json = ?, finished = ? WHERE id = ? AND state <> 'done'`,
		outcome, raw, string(parsed), nowMS(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		if _, gerr := s.GetStage(ctx, id); gerr != nil {
			return gerr
		}
		return fmt.Errorf("stage %d is already done: %w", id, ErrConflict)
	}
	return nil
}

// GetStage returns one stage or ErrNotFound.
func (s *Store) GetStage(ctx context.Context, id int64) (model.Stage, error) {
	st, err := scanStage(s.db.QueryRowContext(ctx, `SELECT `+stageCols+` FROM stages WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return st, fmt.Errorf("stage %d: %w", id, ErrNotFound)
	}
	return st, err
}

func (s *Store) queryStages(ctx context.Context, where string, args ...any) ([]model.Stage, error) {
	return queryStages(ctx, s.db, where, args...)
}

func queryStages(ctx context.Context, q querier, where string, args ...any) ([]model.Stage, error) {
	rows, err := q.QueryContext(ctx, `SELECT `+stageCols+` FROM stages WHERE `+where+` ORDER BY id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Stage
	for rows.Next() {
		st, err := scanStage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

// ListAttemptStages returns an attempt's stages in order.
func (s *Store) ListAttemptStages(ctx context.Context, attempt int64) ([]model.Stage, error) {
	return s.queryStages(ctx, `attempt = ?`, attempt)
}

// ListEffortStages returns an effort's own (plan, review, compile) stages.
func (s *Store) ListEffortStages(ctx context.Context, effort int64) ([]model.Stage, error) {
	return s.queryStages(ctx, `effort = ?`, effort)
}

// OpenStages returns every queued or running stage, for reconciliation.
func (s *Store) OpenStages(ctx context.Context) ([]model.Stage, error) {
	return s.queryStages(ctx, `state <> 'done'`)
}

func taskStages(ctx context.Context, q querier, task int64) ([]model.Stage, error) {
	return queryStages(ctx, q, `attempt IN (SELECT id FROM attempts WHERE task = ?)`, task)
}

// dutyRestMax bounds one rest under a duty cap, so a very long stage under a
// low cap does not park its backend for hours.
const dutyRestMax = 30 * time.Minute

// BackendRestUntil is the time (unix ms) before which a backend under a duty
// cap of duty percent is given no new stage: each stage that ended within the
// longest rest is followed by a rest of its running time times (100-duty)/duty,
// up to dutyRestMax. It is 0 when no rest is owed.
func (s *Store) BackendRestUntil(ctx context.Context, backend string, duty int, now time.Time) (int64, error) {
	if duty <= 0 || duty >= 100 {
		return 0, nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT started, finished FROM stages WHERE backend = ? AND started > 0 AND finished >= ?`,
		backend, now.Add(-dutyRestMax).UnixMilli())
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	var until int64
	for rows.Next() {
		var started, finished int64
		if err := rows.Scan(&started, &finished); err != nil {
			return 0, err
		}
		rest := (finished - started) * int64(100-duty) / int64(duty)
		if max := dutyRestMax.Milliseconds(); rest > max {
			rest = max
		}
		if end := finished + rest; end > until {
			until = end
		}
	}
	if until <= now.UnixMilli() {
		return 0, rows.Err()
	}
	return until, rows.Err()
}
