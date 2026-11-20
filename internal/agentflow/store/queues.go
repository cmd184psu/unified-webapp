package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"cmd184psu/unified-webapp/internal/agentflow/model"
	"cmd184psu/unified-webapp/internal/agentflow/sandbox"
)

func jsonList(l []string) string {
	if l == nil {
		l = []string{}
	}
	b, _ := json.Marshal(l)
	return string(b)
}

// validateTaskToolchains rejects a task that names a toolchain the sandbox
// cannot mount, so a bad definition fails when it is saved rather than at the
// task's first feedback command.
func validateTaskToolchains(defs []model.TaskDef) error {
	for _, d := range defs {
		for i, n := range d.Toolchains {
			if !sandbox.ValidToolchain(n) {
				return Invalid(fmt.Sprintf("tasks.%s.toolchains[%d]", d.Key, i),
					fmt.Sprintf("unknown toolchain %q (known: %s)", n, strings.Join(sandbox.Toolchains, ", ")))
			}
		}
	}
	return nil
}

func insertTasks(ctx context.Context, tx *sql.Tx, queue int64, defs []model.TaskDef) error {
	ids := map[string]int64{}
	for _, d := range defs {
		res, err := tx.ExecContext(ctx, `INSERT INTO tasks (queue, task_key, phase, title, prompt, acceptance_json, files_touched_json, toolchains_json)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, queue, d.Key, d.Phase, d.Title, d.Prompt, jsonList(d.Acceptance), jsonList(d.FilesTouched), jsonList(d.Toolchains))
		if err != nil {
			return err
		}
		ids[d.Key], _ = res.LastInsertId()
	}
	for _, d := range defs {
		for _, dep := range d.DependsOn {
			if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO task_deps (task, depends_on) VALUES (?, ?)`, ids[d.Key], ids[dep]); err != nil {
				return err
			}
		}
	}
	return nil
}

func loadQueue(ctx context.Context, q querier, effort int64) (model.Queue, int64, error) {
	var qu model.Queue
	var id int64
	err := q.QueryRowContext(ctx, `SELECT id, revision, hash, status, approved_by FROM queues WHERE effort = ?`, effort).
		Scan(&id, &qu.Revision, &qu.Hash, &qu.Status, &qu.ApprovedBy)
	if errors.Is(err, sql.ErrNoRows) {
		return qu, 0, fmt.Errorf("queue of effort %d: %w", effort, ErrNotFound)
	}
	if err != nil {
		return qu, 0, err
	}
	qu.Effort = effort
	tasks, err := loadTasks(ctx, q, id)
	if err != nil {
		return qu, 0, err
	}
	for _, t := range tasks {
		qu.Tasks = append(qu.Tasks, t.TaskDef)
	}
	return qu, id, nil
}

// loadTasks returns a queue's tasks in insertion order with dependency keys.
func loadTasks(ctx context.Context, q querier, queue int64) ([]model.Task, error) {
	rows, err := q.QueryContext(ctx, `SELECT id, task_key, phase, title, prompt, acceptance_json, files_touched_json, toolchains_json, waiting_reason
		FROM tasks WHERE queue = ? ORDER BY id`, queue)
	if err != nil {
		return nil, err
	}
	var out []model.Task
	for rows.Next() {
		t := model.Task{Queue: queue}
		var acc, files, toolchains string
		if err := rows.Scan(&t.ID, &t.Key, &t.Phase, &t.Title, &t.Prompt, &acc, &files, &toolchains, &t.WaitingReason); err != nil {
			rows.Close()
			return nil, err
		}
		if err := json.Unmarshal([]byte(acc), &t.Acceptance); err != nil {
			rows.Close()
			return nil, fmt.Errorf("task %s acceptance: %w", t.Key, err)
		}
		if err := json.Unmarshal([]byte(files), &t.FilesTouched); err != nil {
			rows.Close()
			return nil, fmt.Errorf("task %s files_touched: %w", t.Key, err)
		}
		if toolchains != "" {
			if err := json.Unmarshal([]byte(toolchains), &t.Toolchains); err != nil {
				rows.Close()
				return nil, fmt.Errorf("task %s toolchains: %w", t.Key, err)
			}
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	for i := range out {
		// JSON lists are always arrays, never null, so clients can read .length.
		out[i].DependsOn = []string{}
		if out[i].Acceptance == nil {
			out[i].Acceptance = []string{}
		}
		if out[i].FilesTouched == nil {
			out[i].FilesTouched = []string{}
		}
		if out[i].Toolchains == nil {
			out[i].Toolchains = []string{}
		}
		drows, err := q.QueryContext(ctx, `SELECT t.task_key FROM task_deps d JOIN tasks t ON t.id = d.depends_on
			WHERE d.task = ? ORDER BY t.task_key`, out[i].ID)
		if err != nil {
			return nil, err
		}
		for drows.Next() {
			var k string
			if err := drows.Scan(&k); err != nil {
				drows.Close()
				return nil, err
			}
			out[i].DependsOn = append(out[i].DependsOn, k)
		}
		if err := drows.Err(); err != nil {
			drows.Close()
			return nil, err
		}
		drows.Close()
	}
	return out, nil
}

func queueHasAttempts(ctx context.Context, tx *sql.Tx, queue int64) (bool, error) {
	var n int
	err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM attempts a JOIN tasks t ON t.id = a.task WHERE t.queue = ?`, queue).Scan(&n)
	return n > 0, err
}

// SaveQueue replaces the effort's queue with defs (validated, V1) as a new
// draft revision. It is ErrConflict once any task has an attempt.
func (s *Store) SaveQueue(ctx context.Context, actor string, effort int64, defs []model.TaskDef) (model.Queue, error) {
	if err := model.ValidateTasks(defs); err != nil {
		return model.Queue{}, Invalid("queue", err.Error())
	}
	if err := validateTaskToolchains(defs); err != nil {
		return model.Queue{}, err
	}
	hash := model.QueueHash(defs)
	err := s.WithTx(ctx, func(tx *sql.Tx) error {
		if _, err := requireEffort(ctx, tx, effort); err != nil {
			return err
		}
		rev := 1
		var qid int64
		err := tx.QueryRowContext(ctx, `SELECT id, revision + 1 FROM queues WHERE effort = ?`, effort).Scan(&qid, &rev)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			res, err := tx.ExecContext(ctx, `INSERT INTO queues (effort, revision, hash, status) VALUES (?, 1, ?, 'draft')`, effort, hash)
			if err != nil {
				return err
			}
			qid, _ = res.LastInsertId()
		case err != nil:
			return err
		default:
			if has, err := queueHasAttempts(ctx, tx, qid); err != nil {
				return err
			} else if has {
				return fmt.Errorf("queue of effort %d has attempts and cannot be replaced: %w", effort, ErrConflict)
			}
			if _, err := tx.ExecContext(ctx, `DELETE FROM tasks WHERE queue = ?`, qid); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `UPDATE queues SET revision = ?, hash = ?, status = 'draft', approved_by = '', approved_at = 0 WHERE id = ?`, rev, hash, qid); err != nil {
				return err
			}
		}
		if err := insertTasks(ctx, tx, qid, defs); err != nil {
			return err
		}
		_, err = AppendEvent(tx, actor, "queue.save", "effort", fmt.Sprint(effort), map[string]any{"revision": rev, "hash": hash})
		return err
	})
	if err != nil {
		return model.Queue{}, err
	}
	return s.GetQueue(ctx, effort)
}

// GetQueue returns the effort's queue or ErrNotFound.
func (s *Store) GetQueue(ctx context.Context, effort int64) (model.Queue, error) {
	q, _, err := loadQueue(ctx, s.db, effort)
	return q, err
}

// EditTask replaces one task's definition (matched by def.Key). The queue's
// hash is recomputed and the queue returns to draft (QA2).
func (s *Store) EditTask(ctx context.Context, actor string, effort int64, def model.TaskDef) (model.Queue, error) {
	err := s.WithTx(ctx, func(tx *sql.Tx) error {
		cur, qid, err := loadQueue(ctx, tx, effort)
		if err != nil {
			return err
		}
		defs := make([]model.TaskDef, len(cur.Tasks))
		found := false
		for i, t := range cur.Tasks {
			defs[i] = t
			if t.Key == def.Key {
				defs[i], found = def, true
			}
		}
		if !found {
			return fmt.Errorf("task %q in effort %d: %w", def.Key, effort, ErrNotFound)
		}
		if err := model.ValidateTasks(defs); err != nil {
			return Invalid("queue", err.Error())
		}
		if err := validateTaskToolchains(defs); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE tasks SET phase = ?, title = ?, prompt = ?, acceptance_json = ?, files_touched_json = ?, toolchains_json = ?
			WHERE queue = ? AND task_key = ?`, def.Phase, def.Title, def.Prompt, jsonList(def.Acceptance), jsonList(def.FilesTouched), jsonList(def.Toolchains), qid, def.Key); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM task_deps WHERE task IN (SELECT id FROM tasks WHERE queue = ? AND task_key = ?)`, qid, def.Key); err != nil {
			return err
		}
		for _, dep := range def.DependsOn {
			if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO task_deps (task, depends_on)
				SELECT a.id, b.id FROM tasks a, tasks b WHERE a.queue = ? AND a.task_key = ? AND b.queue = ? AND b.task_key = ?`,
				qid, def.Key, qid, dep); err != nil {
				return err
			}
		}
		hash := model.QueueHash(defs)
		if _, err := tx.ExecContext(ctx, `UPDATE queues SET revision = revision + 1, hash = ?, status = 'draft', approved_by = '', approved_at = 0 WHERE id = ?`, hash, qid); err != nil {
			return err
		}
		_, err = AppendEvent(tx, actor, "queue.edit_task", "effort", fmt.Sprint(effort), map[string]any{"task": def.Key, "hash": hash})
		return err
	})
	if err != nil {
		return model.Queue{}, err
	}
	return s.GetQueue(ctx, effort)
}

// ApproveQueue approves a draft queue, recording who approved it. expectedHash
// is the hash the approver reviewed; if the queue was edited since, the hash
// differs and the result is ErrConflict.
func (s *Store) ApproveQueue(ctx context.Context, actor string, effort int64, expectedHash string) (model.Queue, error) {
	err := s.WithTx(ctx, func(tx *sql.Tx) error {
		cur, qid, err := loadQueue(ctx, tx, effort)
		if err != nil {
			return err
		}
		if cur.Status != model.QueueDraft {
			return fmt.Errorf("queue of effort %d is already %s: %w", effort, cur.Status, ErrConflict)
		}
		if cur.Hash != expectedHash {
			return fmt.Errorf("queue of effort %d changed since it was reviewed: %w", effort, ErrConflict)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE queues SET status = 'approved', approved_by = ?, approved_at = ? WHERE id = ?`, actor, nowMS(), qid); err != nil {
			return err
		}
		_, err = AppendEvent(tx, actor, "queue.approve", "effort", fmt.Sprint(effort), map[string]any{"hash": cur.Hash})
		return err
	})
	if err != nil {
		return model.Queue{}, err
	}
	return s.GetQueue(ctx, effort)
}

// ApprovedQueueHash returns the hash of the effort's approved queue, or
// ErrNotFound when the queue is missing or not approved. A stage whose
// queue_hash differs is stale and must be discarded (QA2).
func (s *Store) ApprovedQueueHash(ctx context.Context, effort int64) (string, error) {
	q, err := s.GetQueue(ctx, effort)
	if err != nil {
		return "", err
	}
	if q.Status != model.QueueApproved {
		return "", fmt.Errorf("queue of effort %d is not approved: %w", effort, ErrNotFound)
	}
	return q.Hash, nil
}

// QueueEffort is the effort a queue belongs to.
func (s *Store) QueueEffort(ctx context.Context, queue int64) (int64, error) {
	var effort int64
	err := s.db.QueryRowContext(ctx, `SELECT effort FROM queues WHERE id = ?`, queue).Scan(&effort)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	return effort, err
}

// AddTasks appends tasks to the effort's approved queue and records event in
// the same transaction (H31). The queue's hash and revision stay as they were:
// the person approved those tasks, these are follow-ups the engine adds for
// gaps it found in the finished work, and a stage already in flight must not
// become stale for it. A key that is already in the queue is ErrConflict.
func (s *Store) AddTasks(ctx context.Context, actor string, effort int64, defs []model.TaskDef, action string, detail map[string]any) error {
	if err := model.ValidateTasks(defs); err != nil {
		return Invalid("tasks", err.Error())
	}
	if err := validateTaskToolchains(defs); err != nil {
		return err
	}
	return s.WithTx(ctx, func(tx *sql.Tx) error {
		q, qid, err := loadQueue(ctx, tx, effort)
		if err != nil {
			return err
		}
		if q.Status != model.QueueApproved {
			return fmt.Errorf("queue of effort %d is %s, not approved: %w", effort, q.Status, ErrConflict)
		}
		for _, d := range defs {
			var n int
			if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM tasks WHERE queue = ? AND task_key = ?`, qid, d.Key).Scan(&n); err != nil {
				return err
			}
			if n > 0 {
				return fmt.Errorf("task %s is already in the queue of effort %d: %w", d.Key, effort, ErrConflict)
			}
		}
		if err := insertTasks(ctx, tx, qid, defs); err != nil {
			return err
		}
		_, err = AppendEvent(tx, actor, action, "effort", fmt.Sprint(effort), detail)
		return err
	})
}
