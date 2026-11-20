package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"cmd184psu/unified-webapp/internal/agentflow/model"
)

// GetTask returns one stored task or ErrNotFound.
func (s *Store) GetTask(ctx context.Context, id int64) (model.Task, error) {
	var queue int64
	if err := s.db.QueryRowContext(ctx, `SELECT queue FROM tasks WHERE id = ?`, id).Scan(&queue); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.Task{}, fmt.Errorf("task %d: %w", id, ErrNotFound)
		}
		return model.Task{}, err
	}
	ts, err := loadTasks(ctx, s.db, queue)
	if err != nil {
		return model.Task{}, err
	}
	for _, t := range ts {
		if t.ID == id {
			return t, nil
		}
	}
	return model.Task{}, fmt.Errorf("task %d: %w", id, ErrNotFound)
}

// ListTasks returns the effort's tasks, or ErrNotFound without a queue.
func (s *Store) ListTasks(ctx context.Context, effort int64) ([]model.Task, error) {
	_, qid, err := loadQueue(ctx, s.db, effort)
	if err != nil {
		return nil, err
	}
	return loadTasks(ctx, s.db, qid)
}

// ListQueueTasks returns the tasks of one queue by queue id, for callers that
// hold a task (which knows its queue) and not the effort.
func (s *Store) ListQueueTasks(ctx context.Context, queue int64) ([]model.Task, error) {
	return loadTasks(ctx, s.db, queue)
}

// SetWaitingReason sets (or with "" clears) the task's mutable waiting reason,
// shown when the dispatcher cannot place it (SV3).
func (s *Store) SetWaitingReason(ctx context.Context, taskID int64, reason string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE tasks SET waiting_reason = ? WHERE id = ?`, reason, taskID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("task %d: %w", taskID, ErrNotFound)
	}
	return nil
}

func requireTask(ctx context.Context, tx *sql.Tx, id int64) error {
	var n int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM tasks WHERE id = ?`, id).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("task %d: %w", id, ErrNotFound)
	}
	return nil
}

// OverrideTask marks a task passed by a human decision. A second override is
// ErrConflict.
func (s *Store) OverrideTask(ctx context.Context, actor string, taskID int64, reason string) error {
	if strings.TrimSpace(reason) == "" {
		return Invalid("reason", "required")
	}
	return s.WithTx(ctx, func(tx *sql.Tx) error {
		if err := requireTask(ctx, tx, taskID); err != nil {
			return err
		}
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM overrides WHERE task = ?`, taskID).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			return fmt.Errorf("task %d is already overridden: %w", taskID, ErrConflict)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO overrides (task, actor, reason, at) VALUES (?, ?, ?, ?)`, taskID, actor, reason, nowMS()); err != nil {
			return err
		}
		_, err := AppendEvent(tx, actor, "task.override", "task", fmt.Sprint(taskID), map[string]any{"reason": reason})
		return err
	})
}

// ResetTask inserts a task_resets epoch: attempts and infra retries recorded
// so far stop counting toward the task's status, and any attempt still open is
// closed as abandoned so a new one can start. Only a halted task can be reset
// (ErrConflict otherwise). clean asks the engine to remove the worktree.
func (s *Store) ResetTask(ctx context.Context, actor string, taskID int64, clean bool, lim model.Limits) error {
	return s.WithTx(ctx, func(tx *sql.Tx) error {
		if err := requireTask(ctx, tx, taskID); err != nil {
			return err
		}
		in, err := taskInput(ctx, tx, model.Task{ID: taskID})
		if err != nil {
			return err
		}
		if st := model.DeriveTaskStatus(in, nil, lim); !st.Halted() {
			return fmt.Errorf("task %d is %s, not halted: %w", taskID, st, ErrConflict)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE attempts SET outcome = 'abandoned', finished = ? WHERE task = ? AND outcome = ''`, nowMS(), taskID); err != nil {
			return err
		}
		var att, inf int64
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(id), 0) FROM attempts WHERE task = ?`, taskID).Scan(&att); err != nil {
			return err
		}
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(id), 0) FROM infra_retries WHERE task = ?`, taskID).Scan(&inf); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO task_resets (task, after_attempt_id, after_infra_id, clean, actor, at) VALUES (?, ?, ?, ?, ?, ?)`,
			taskID, att, inf, b2i(clean), actor, nowMS()); err != nil {
			return err
		}
		_, err = AppendEvent(tx, actor, "task.reset", "task", fmt.Sprint(taskID), map[string]any{"clean": clean})
		return err
	})
}

// RecordInfraRetry counts one infrastructure retry against the task.
func (s *Store) RecordInfraRetry(ctx context.Context, taskID, stageID int64, reason string) error {
	var stage any
	if stageID != 0 {
		stage = stageID
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO infra_retries (task, stage, reason, at) VALUES (?, ?, ?, ?)`, taskID, stage, reason, nowMS())
	return err
}

// AppendProgress adds a progress-log entry for a task (attemptID 0: none).
func (s *Store) AppendProgress(ctx context.Context, taskID, attemptID int64, entry string) error {
	var att any
	if attemptID != 0 {
		att = attemptID
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO progress_log (task, attempt, entry, at) VALUES (?, ?, ?, ?)`, taskID, att, entry, nowMS())
	return err
}

// ListProgress returns the task's last limit entries, oldest first.
func (s *Store) ListProgress(ctx context.Context, taskID int64, limit int) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT entry FROM (SELECT id, entry FROM progress_log WHERE task = ? ORDER BY id DESC LIMIT ?) ORDER BY id`, taskID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var e string
		if err := rows.Scan(&e); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// SaveBaseline records the verify baseline captured for an effort.
func (s *Store) SaveBaseline(ctx context.Context, effort int64, commit, reportJSON string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO baselines (effort, commit_sha, report_json, at) VALUES (?, ?, ?, ?)`, effort, commit, reportJSON, nowMS())
	return err
}

// LatestBaseline returns the newest baseline's commit and report, or ErrNotFound.
func (s *Store) LatestBaseline(ctx context.Context, effort int64) (commit, reportJSON string, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT commit_sha, report_json FROM baselines WHERE effort = ? ORDER BY id DESC LIMIT 1`, effort).Scan(&commit, &reportJSON)
	if errors.Is(err, sql.ErrNoRows) {
		err = fmt.Errorf("baseline of effort %d: %w", effort, ErrNotFound)
	}
	return
}

// taskInput gathers, for one task, what model.DeriveTaskStatus needs. Only
// t.ID, t.Key and t.DependsOn are used.
func taskInput(ctx context.Context, q querier, t model.Task) (model.TaskInput, error) {
	in := model.TaskInput{Key: t.Key, Deps: t.DependsOn}
	var err error
	if in.Attempts, err = listAttempts(ctx, q, t.ID); err != nil {
		return in, err
	}
	if in.Stages, err = taskStages(ctx, q, t.ID); err != nil {
		return in, err
	}
	var n int
	if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM overrides WHERE task = ?`, t.ID).Scan(&n); err != nil {
		return in, err
	}
	in.Overridden = n > 0
	if in.Resets, err = taskResets(ctx, q, t.ID); err != nil {
		return in, err
	}
	in.InfraIDs, err = infraIDs(ctx, q, t.ID)
	return in, err
}

func taskResets(ctx context.Context, q querier, task int64) ([]model.Reset, error) {
	rows, err := q.QueryContext(ctx, `SELECT after_attempt_id, after_infra_id FROM task_resets WHERE task = ? ORDER BY id`, task)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Reset
	for rows.Next() {
		var r model.Reset
		if err := rows.Scan(&r.AfterAttemptID, &r.AfterInfraID); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func infraIDs(ctx context.Context, q querier, task int64) ([]int64, error) {
	rows, err := q.QueryContext(ctx, `SELECT id FROM infra_retries WHERE task = ? ORDER BY id`, task)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// TaskInputs gathers the derivation inputs for every task of the effort's
// queue from one consistent snapshot.
func (s *Store) TaskInputs(ctx context.Context, effort int64) ([]model.TaskInput, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	_, qid, err := loadQueue(ctx, tx, effort)
	if err != nil {
		return nil, err
	}
	tasks, err := loadTasks(ctx, tx, qid)
	if err != nil {
		return nil, err
	}
	out := make([]model.TaskInput, 0, len(tasks))
	for _, t := range tasks {
		in, err := taskInput(ctx, tx, t)
		if err != nil {
			return nil, err
		}
		out = append(out, in)
	}
	return out, nil
}

// TaskStatuses derives every task's status for the effort. Nothing is stored.
func (s *Store) TaskStatuses(ctx context.Context, effort int64, lim model.Limits) (map[string]model.Status, error) {
	in, err := s.TaskInputs(ctx, effort)
	if err != nil {
		return nil, err
	}
	return model.DeriveAll(in, lim), nil
}

// AllowTaskFiles adds files to a task's files_touched (AF-13: the owner allows files a worker
// asked for). Like an acceptance follow-up task, it changes the approved queue in place: the
// queue's hash, and so every stage already queued, stays valid.
func (s *Store) AllowTaskFiles(ctx context.Context, actor string, task int64, files []string) error {
	return s.WithTx(ctx, func(tx *sql.Tx) error {
		var key, js string
		if err := tx.QueryRowContext(ctx, `SELECT task_key, files_touched_json FROM tasks WHERE id = ?`, task).Scan(&key, &js); errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("task %d: %w", task, ErrNotFound)
		} else if err != nil {
			return err
		}
		var cur []string
		if err := json.Unmarshal([]byte(js), &cur); err != nil {
			return fmt.Errorf("task %s files_touched: %w", key, err)
		}
		have := map[string]bool{}
		for _, f := range cur {
			have[f] = true
		}
		var added []string
		for _, f := range files {
			if f == "" || strings.HasPrefix(f, "/") || f == ".." || strings.HasPrefix(f, "../") {
				return Invalid("files", fmt.Sprintf("%q is not a path relative to the repository root", f))
			}
			if !have[f] {
				have[f] = true
				cur = append(cur, f)
				added = append(added, f)
			}
		}
		if _, err := tx.ExecContext(ctx, `UPDATE tasks SET files_touched_json = ? WHERE id = ?`, jsonList(cur), task); err != nil {
			return err
		}
		_, err := AppendEvent(tx, actor, "task.files_allowed", "task", fmt.Sprint(task), map[string]any{"task": key, "files": added})
		return err
	})
}

// AmendTaskPrompt appends text to a task's prompt, which its worker and reviewer read (AF-13:
// a resolver's clarification or an out-of-scope finding). Like AllowTaskFiles it changes the
// approved queue in place.
func (s *Store) AmendTaskPrompt(ctx context.Context, actor string, task int64, text string) error {
	return s.WithTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE tasks SET prompt = prompt || ? WHERE id = ?`, "\n\n"+strings.TrimSpace(text), task)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return fmt.Errorf("task %d: %w", task, ErrNotFound)
		}
		_, err = AppendEvent(tx, actor, "task.prompt_amended", "task", fmt.Sprint(task), map[string]any{"text": text})
		return err
	})
}
