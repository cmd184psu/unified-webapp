package db

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"cmd184psu/unified-webapp/internal/taskmaster/models"
	_ "modernc.org/sqlite"
)

type DB struct {
	conn *sql.DB
}

func Open(path string) (*DB, error) {
	dsn := path
	if path != ":memory:" {
		dsn = path + "?_journal_mode=WAL"
	}
	conn, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	conn.SetMaxOpenConns(1)
	// Enable foreign keys for every connection; must run before any DML.
	if _, err := conn.Exec("PRAGMA foreign_keys = ON"); err != nil {
		conn.Close()
		return nil, fmt.Errorf("enable foreign keys: %w", err)
	}
	db := &DB{conn: conn}
	if err := db.migrate(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return db, nil
}

func (db *DB) Close() error {
	return db.conn.Close()
}

func (db *DB) Ping() error {
	return db.conn.Ping()
}

// migrate ensures the stable (never-restructured) tables exist, bootstraps
// the original v0 lanes/tasks shape for a genuinely new database file, and
// then runs the versioned migrations forward. The bootstrap only fires when
// the "tasks" table doesn't already exist — an existing on-disk database
// (any prior schema_version) already has it from a previous run and must go
// through applyMigrations instead of having it stamped out again, which
// matters once a migration renames/drops that table (see migration 3).
func (db *DB) migrate() error {
	stable := `
CREATE TABLE IF NOT EXISTS schema_version (version INTEGER PRIMARY KEY);

CREATE TABLE IF NOT EXISTS task_locks (
  task_id INTEGER PRIMARY KEY,
  worker_id TEXT NOT NULL,
  acquired_at INTEGER NOT NULL,
  expires_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS task_metrics (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  task_id INTEGER NOT NULL,
  execution_id INTEGER NOT NULL,
  recorded_at INTEGER NOT NULL,
  duration_ms INTEGER,
  schedule_delay_ms INTEGER,
  status TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_locks_expires ON task_locks(expires_at);
CREATE INDEX IF NOT EXISTS idx_metrics_task ON task_metrics(task_id, recorded_at);
`
	if _, err := db.conn.Exec(stable); err != nil {
		return err
	}

	var tasksExists int
	if err := db.conn.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='tasks'`).Scan(&tasksExists); err != nil {
		return err
	}
	if tasksExists == 0 {
		bootstrap := `
CREATE TABLE groups (
  name TEXT PRIMARY KEY,
  pool_limit INTEGER NOT NULL DEFAULT 1,
  allowed_types TEXT NOT NULL DEFAULT '[]',
  paused INTEGER NOT NULL DEFAULT 0,
  paused_at INTEGER,
  paused_by TEXT,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);

CREATE TABLE tasks (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT UNIQUE NOT NULL,
  group_name TEXT NOT NULL,
  enabled INTEGER NOT NULL DEFAULT 1,
  paused INTEGER NOT NULL DEFAULT 0,
  priority INTEGER NOT NULL DEFAULT 50,
  cooldown_seconds INTEGER NOT NULL DEFAULT 0,
  repeat INTEGER NOT NULL DEFAULT 0,
  task_type TEXT NOT NULL,
  args TEXT NOT NULL DEFAULT '{}',
  sudo INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);

CREATE TABLE task_executions (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  task_id INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  scheduled_at INTEGER,
  started_at INTEGER,
  finished_at INTEGER,
  status TEXT NOT NULL DEFAULT 'pending',
  error_message TEXT,
  worker_id TEXT,
  duration_ms INTEGER,
  schedule_delay_ms INTEGER
);

CREATE INDEX idx_tasks_group ON tasks(group_name);
CREATE INDEX idx_tasks_enabled ON tasks(enabled, priority);
CREATE INDEX idx_executions_task ON task_executions(task_id, status);
CREATE INDEX idx_executions_finished ON task_executions(finished_at);
`
		if _, err := db.conn.Exec(bootstrap); err != nil {
			return err
		}
	}

	return db.applyMigrations()
}

func (db *DB) applyMigrations() error {
	var version int
	_ = db.conn.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_version`).Scan(&version)

	migrations := []struct {
		version int
		sql     string
	}{
		{1, `ALTER TABLE tasks ADD COLUMN output_file TEXT NOT NULL DEFAULT ''`},
		{2, `CREATE TABLE IF NOT EXISTS settings (key TEXT PRIMARY KEY, value TEXT NOT NULL)`},
		{3, migration3SQL},
	}

	for _, m := range migrations {
		if m.version <= version {
			continue
		}
		if _, err := db.conn.Exec(m.sql); err != nil {
			return fmt.Errorf("migration %d: %w", m.version, err)
		}
		if _, err := db.conn.Exec(`INSERT OR REPLACE INTO schema_version (version) VALUES (?)`, m.version); err != nil {
			return fmt.Errorf("record migration %d: %w", m.version, err)
		}
	}
	return nil
}

// migration3SQL rebuilds groups→lanes and reshapes tasks (group_name→
// lane_name; drops task_type/args/priority; adds command/position) using
// SQLite's table-rebuild pattern, since columns are renamed/dropped.
// Legacy shell tasks carry their command forward from args.shell on a
// best-effort basis; every other task_type is left with an empty command
// for manual fixup (documented in docs/taskmaster.md).
const migration3SQL = `
PRAGMA foreign_keys=OFF;

CREATE TABLE lanes (
  name TEXT PRIMARY KEY,
  width INTEGER NOT NULL DEFAULT 1,
  paused INTEGER NOT NULL DEFAULT 0,
  paused_at INTEGER,
  paused_by TEXT,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);

INSERT INTO lanes (name, width, paused, paused_at, paused_by, created_at, updated_at)
SELECT name, pool_limit, paused, paused_at, paused_by, created_at, updated_at FROM groups;

DROP TABLE groups;

CREATE TABLE tasks_new (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT UNIQUE NOT NULL,
  lane_name TEXT NOT NULL,
  enabled INTEGER NOT NULL DEFAULT 1,
  paused INTEGER NOT NULL DEFAULT 0,
  cooldown_seconds INTEGER NOT NULL DEFAULT 0,
  repeat INTEGER NOT NULL DEFAULT 0,
  command TEXT NOT NULL DEFAULT '',
  position INTEGER NOT NULL DEFAULT 0,
  sudo INTEGER NOT NULL DEFAULT 0,
  output_file TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);

INSERT INTO tasks_new (id, name, lane_name, enabled, paused, cooldown_seconds, repeat, command, position, sudo, output_file, created_at, updated_at)
SELECT id, name, group_name, enabled, paused, cooldown_seconds, repeat,
  CASE WHEN task_type = 'shell' THEN COALESCE(json_extract(args, '$.shell'), '') ELSE '' END,
  priority, sudo, output_file, created_at, updated_at
FROM tasks;

DROP TABLE tasks;
ALTER TABLE tasks_new RENAME TO tasks;

CREATE INDEX idx_tasks_lane ON tasks(lane_name);
CREATE INDEX idx_tasks_enabled ON tasks(enabled);

PRAGMA foreign_keys=ON;
`

// ─── Settings (key/value) ────────────────────────────────────────────────────

// SettingAllowSudo is the settings key holding the persisted allow_sudo flag.
// The DB is authoritative for it once build.go seeds it from config.
const SettingAllowSudo = "allow_sudo"

// SettingBrakeEngaged is the settings key holding the persisted hand-brake
// flag. build.go reads it on boot and starts the worker braked if set;
// the /api/brake handlers keep it in sync with the runtime BrakeGate.
const SettingBrakeEngaged = "brake_engaged"

// SettingBrakePausedLanes is the settings key holding the JSON list of lane
// names the hand brake paused (i.e. the ones that were NOT already paused
// when the brake engaged), so release can restore exactly those lanes and
// leave independently-paused lanes alone.
const SettingBrakePausedLanes = "brake_paused_lanes"

// EncodeBoolSetting / DecodeBoolSetting are the shared "1"/"0" encoding used
// for boolean settings, so the reader (build.go) and writer (coordinator)
// never diverge.
func EncodeBoolSetting(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

func DecodeBoolSetting(v string) bool { return v == "1" }

// GetSetting returns the stored value for key and whether it was present.
func (db *DB) GetSetting(key string) (value string, ok bool, err error) {
	row := db.conn.QueryRow(`SELECT value FROM settings WHERE key = ?`, key)
	switch err := row.Scan(&value); {
	case errors.Is(err, sql.ErrNoRows):
		return "", false, nil
	case err != nil:
		return "", false, err
	default:
		return value, true, nil
	}
}

// SetSetting upserts key = value.
func (db *DB) SetSetting(key, value string) error {
	_, err := db.conn.Exec(`
		INSERT INTO settings (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

// GetBrakePausedLanes returns the lane names recorded under
// SettingBrakePausedLanes (the lanes the hand brake paused and should
// restore on release), or nil if the key is absent/empty.
func (db *DB) GetBrakePausedLanes() ([]string, error) {
	v, ok, err := db.GetSetting(SettingBrakePausedLanes)
	if err != nil || !ok || v == "" {
		return nil, err
	}
	var lanes []string
	if err := json.Unmarshal([]byte(v), &lanes); err != nil {
		return nil, err
	}
	return lanes, nil
}

// SetBrakePausedLanes persists the lane names the hand brake paused, as
// JSON, under SettingBrakePausedLanes.
func (db *DB) SetBrakePausedLanes(lanes []string) error {
	b, err := json.Marshal(lanes)
	if err != nil {
		return err
	}
	return db.SetSetting(SettingBrakePausedLanes, string(b))
}

// ─── Lane CRUD ───────────────────────────────────────────────────────────────

func (db *DB) UpsertLane(l *models.Lane) error {
	now := epochMs(time.Now())
	_, err := db.conn.Exec(`
		INSERT INTO lanes (name, width, paused, created_at, updated_at)
		VALUES (?, ?, 0, ?, ?)
		ON CONFLICT(name) DO UPDATE SET
		  width = excluded.width,
		  updated_at = excluded.updated_at`,
		l.Name, l.Width, now, now)
	return err
}

func (db *DB) GetLane(name string) (*models.Lane, error) {
	row := db.conn.QueryRow(`SELECT name, width, paused, paused_at, paused_by, created_at, updated_at FROM lanes WHERE name = ?`, name)
	l, err := scanLane(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return l, err
}

func (db *DB) ListLanes() ([]*models.Lane, error) {
	rows, err := db.conn.Query(`SELECT name, width, paused, paused_at, paused_by, created_at, updated_at FROM lanes ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var lanes []*models.Lane
	for rows.Next() {
		l, err := scanLane(rows)
		if err != nil {
			return nil, err
		}
		lanes = append(lanes, l)
	}
	return lanes, rows.Err()
}

func (db *DB) DeleteLane(name string) error {
	var count int
	err := db.conn.QueryRow(`SELECT COUNT(*) FROM tasks WHERE lane_name = ?`, name).Scan(&count)
	if err != nil {
		return err
	}
	if count > 0 {
		return fmt.Errorf("lane %q has %d task(s); delete or move them first", name, count)
	}
	_, err = db.conn.Exec(`DELETE FROM lanes WHERE name = ?`, name)
	return err
}

// SetLaneWidth updates only a lane's width, leaving paused/paused_at/paused_by
// untouched (UpsertLane would require reloading those fields first to avoid
// clobbering them).
func (db *DB) SetLaneWidth(name string, width int) error {
	_, err := db.conn.Exec(`UPDATE lanes SET width = ?, updated_at = ? WHERE name = ?`,
		width, epochMs(time.Now()), name)
	return err
}

func (db *DB) SetLanePaused(name string, paused bool, by string) error {
	var pausedAt any
	if paused {
		pausedAt = epochMs(time.Now())
	}
	pausedInt := 0
	if paused {
		pausedInt = 1
	}
	_, err := db.conn.Exec(`UPDATE lanes SET paused = ?, paused_at = ?, paused_by = ?, updated_at = ? WHERE name = ?`,
		pausedInt, pausedAt, by, epochMs(time.Now()), name)
	return err
}

func (db *DB) CountRunningInLane(laneName string) (int, error) {
	var count int
	err := db.conn.QueryRow(`
		SELECT COUNT(*) FROM task_executions te
		JOIN tasks t ON t.id = te.task_id
		WHERE t.lane_name = ? AND te.status = 'running'`, laneName).Scan(&count)
	return count, err
}

// ─── Task CRUD ────────────────────────────────────────────────────────────────

func (db *DB) AddTask(task *models.Task) (int64, error) {
	now := epochMs(time.Now())
	result, err := db.conn.Exec(`
		INSERT INTO tasks (name, lane_name, enabled, paused, cooldown_seconds, repeat, command, position, sudo, output_file, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(name) DO UPDATE SET
		  lane_name = excluded.lane_name,
		  enabled = excluded.enabled,
		  paused = excluded.paused,
		  cooldown_seconds = excluded.cooldown_seconds,
		  repeat = excluded.repeat,
		  command = excluded.command,
		  position = excluded.position,
		  sudo = excluded.sudo,
		  output_file = excluded.output_file,
		  updated_at = excluded.updated_at`,
		task.Name, task.LaneName, boolInt(task.Enabled), boolInt(task.Paused),
		task.CooldownSeconds, boolInt(task.Repeat),
		task.Command, task.Position, boolInt(task.Sudo), task.OutputFile, now, now)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

func (db *DB) GetTask(name string) (*models.Task, error) {
	row := db.conn.QueryRow(`SELECT id, name, lane_name, enabled, paused, cooldown_seconds, repeat, command, position, sudo, output_file, created_at, updated_at FROM tasks WHERE name = ?`, name)
	t, err := scanTask(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return t, err
}

func (db *DB) ListTasks(laneFilter string) ([]*models.Task, error) {
	q := `SELECT id, name, lane_name, enabled, paused, cooldown_seconds, repeat, command, position, sudo, output_file, created_at, updated_at FROM tasks`
	args := []any{}
	if laneFilter != "" {
		q += " WHERE lane_name = ?"
		args = append(args, laneFilter)
	}
	q += " ORDER BY lane_name, position, name"
	rows, err := db.conn.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tasks []*models.Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

func (db *DB) UpdateTask(name string, updates map[string]any) error {
	if len(updates) == 0 {
		return nil
	}
	updates["updated_at"] = epochMs(time.Now())
	setClauses := make([]string, 0, len(updates))
	vals := make([]any, 0, len(updates)+1)
	for k, v := range updates {
		setClauses = append(setClauses, k+" = ?")
		vals = append(vals, v)
	}
	vals = append(vals, name)
	q := "UPDATE tasks SET " + strings.Join(setClauses, ", ") + " WHERE name = ?"
	_, err := db.conn.Exec(q, vals...)
	return err
}

// SetTaskPositions sets each named task's position to its index in
// orderedNames. The UPDATE is scoped to lane_name, so a name that doesn't
// belong to this lane simply matches no row and is silently ignored.
func (db *DB) SetTaskPositions(laneName string, orderedNames []string) error {
	now := epochMs(time.Now())
	for i, name := range orderedNames {
		if _, err := db.conn.Exec(`UPDATE tasks SET position = ?, updated_at = ? WHERE name = ? AND lane_name = ?`,
			i, now, name, laneName); err != nil {
			return err
		}
	}
	return nil
}

// MaxTaskPosition returns the highest position value among tasks currently
// in laneName, or -1 if the lane has no tasks (so callers can add 1 to get
// the first/next position uniformly).
func (db *DB) MaxTaskPosition(laneName string) (int, error) {
	var max sql.NullInt64
	err := db.conn.QueryRow(`SELECT MAX(position) FROM tasks WHERE lane_name = ?`, laneName).Scan(&max)
	if err != nil {
		return 0, err
	}
	if !max.Valid {
		return -1, nil
	}
	return int(max.Int64), nil
}

func (db *DB) DeleteTask(name string) error {
	_, err := db.conn.Exec(`DELETE FROM tasks WHERE name = ?`, name)
	return err
}

func (db *DB) SetTaskPaused(name string, paused bool) error {
	_, err := db.conn.Exec(`UPDATE tasks SET paused = ?, updated_at = ? WHERE name = ?`,
		boolInt(paused), epochMs(time.Now()), name)
	return err
}

// ─── Lane scheduling ─────────────────────────────────────────────────────────

func (db *DB) GetEligibleTasks() ([]*models.Task, error) {
	nowMs := epochMs(time.Now())
	q := `
WITH last_finished AS (
  SELECT task_id, MAX(finished_at) as last_fin
  FROM task_executions
  WHERE status IN ('success','failed')
  GROUP BY task_id
)
SELECT t.id, t.name, t.lane_name, t.enabled, t.paused,
       t.cooldown_seconds, t.repeat, t.command, t.position, t.sudo, t.output_file,
       t.created_at, t.updated_at
FROM tasks t
LEFT JOIN task_locks tl ON tl.task_id = t.id AND tl.expires_at > ?
LEFT JOIN last_finished lf ON lf.task_id = t.id
LEFT JOIN lanes ls ON ls.name = t.lane_name
WHERE t.enabled = 1
  AND t.paused = 0
  AND tl.task_id IS NULL
  AND COALESCE(ls.paused, 0) = 0
  AND (
    EXISTS (SELECT 1 FROM task_executions pe WHERE pe.task_id = t.id AND pe.status = 'pending')
    OR lf.last_fin IS NULL
    OR (t.repeat = 1 AND (? - CAST(lf.last_fin AS INTEGER)) >= t.cooldown_seconds * 1000)
  )
ORDER BY t.position ASC, COALESCE(lf.last_fin, 0) ASC`
	rows, err := db.conn.Query(q, nowMs, nowMs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tasks []*models.Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

// ─── Execution lifecycle ─────────────────────────────────────────────────────

func (db *DB) EnqueueTask(taskName string, scheduledAt time.Time) (int64, error) {
	var taskID int64
	err := db.conn.QueryRow(`SELECT id FROM tasks WHERE name = ?`, taskName).Scan(&taskID)
	if err != nil {
		return 0, fmt.Errorf("task %q not found: %w", taskName, err)
	}
	result, err := db.conn.Exec(`
		INSERT INTO task_executions (task_id, scheduled_at, status)
		VALUES (?, ?, 'pending')`, taskID, epochMs(scheduledAt))
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

func (db *DB) GetPendingExecution(taskID int64) (*models.TaskExecution, error) {
	row := db.conn.QueryRow(`
		SELECT te.id, te.task_id, te.scheduled_at, te.started_at, te.finished_at,
		       te.status, te.error_message, te.worker_id, te.duration_ms, te.schedule_delay_ms,
		       t.name
		FROM task_executions te
		LEFT JOIN tasks t ON t.id = te.task_id
		WHERE te.task_id = ? AND te.status = 'pending' ORDER BY te.id ASC LIMIT 1`, taskID)
	exec, err := scanExecution(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return exec, err
}

func (db *DB) CreateExecution(taskID int64, workerID string, scheduledAt time.Time) (int64, error) {
	result, err := db.conn.Exec(`
		INSERT INTO task_executions (task_id, scheduled_at, status, worker_id)
		VALUES (?, ?, 'pending', ?)`, taskID, epochMs(scheduledAt), workerID)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

func (db *DB) StartExecution(execID int64, workerID string) error {
	now := epochMs(time.Now())
	_, err := db.conn.Exec(`
		UPDATE task_executions SET status = 'running', started_at = ?, worker_id = ?
		WHERE id = ?`, now, workerID, execID)
	return err
}

func (db *DB) FinishExecution(execID int64, status string, errMsg *string, durationMs, schedDelay int64) error {
	now := epochMs(time.Now())
	_, err := db.conn.Exec(`
		UPDATE task_executions SET status = ?, finished_at = ?, duration_ms = ?, schedule_delay_ms = ?, error_message = ?
		WHERE id = ?`, status, now, durationMs, schedDelay, errMsg, execID)
	return err
}

func (db *DB) GetExecution(id int64) (*models.TaskExecution, error) {
	row := db.conn.QueryRow(`
		SELECT te.id, te.task_id, te.scheduled_at, te.started_at, te.finished_at,
		       te.status, te.error_message, te.worker_id, te.duration_ms, te.schedule_delay_ms,
		       t.name
		FROM task_executions te
		LEFT JOIN tasks t ON t.id = te.task_id
		WHERE te.id = ?`, id)
	exec, err := scanExecution(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return exec, err
}

func (db *DB) ListExecutions(taskFilter string, limit int) ([]*models.TaskExecution, error) {
	if limit <= 0 {
		limit = 50
	}
	var rows *sql.Rows
	var err error
	if taskFilter != "" {
		rows, err = db.conn.Query(`
			SELECT te.id, te.task_id, te.scheduled_at, te.started_at, te.finished_at,
			       te.status, te.error_message, te.worker_id, te.duration_ms, te.schedule_delay_ms,
			       t.name
			FROM task_executions te
			JOIN tasks t ON t.id = te.task_id
			WHERE t.name = ?
			ORDER BY te.id DESC LIMIT ?`, taskFilter, limit)
	} else {
		rows, err = db.conn.Query(`
			SELECT te.id, te.task_id, te.scheduled_at, te.started_at, te.finished_at,
			       te.status, te.error_message, te.worker_id, te.duration_ms, te.schedule_delay_ms,
			       t.name
			FROM task_executions te
			LEFT JOIN tasks t ON t.id = te.task_id
			ORDER BY te.id DESC LIMIT ?`, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var execs []*models.TaskExecution
	for rows.Next() {
		e, err := scanExecution(rows)
		if err != nil {
			return nil, err
		}
		execs = append(execs, e)
	}
	return execs, rows.Err()
}

// ListRunningExecutionIDs returns the IDs of every execution currently
// recorded as "running", for the hand-brake's cancel-all-running step.
func (db *DB) ListRunningExecutionIDs() ([]int64, error) {
	rows, err := db.conn.Query(`SELECT id FROM task_executions WHERE status = 'running'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// ─── Locking ──────────────────────────────────────────────────────────────────

func (db *DB) AcquireLock(taskID int64, workerID string, ttl time.Duration) (bool, error) {
	now := time.Now()
	nowMs := epochMs(now)
	expiresMs := epochMs(now.Add(ttl))
	result, err := db.conn.Exec(`
		INSERT INTO task_locks (task_id, worker_id, acquired_at, expires_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(task_id) DO NOTHING`,
		taskID, workerID, nowMs, expiresMs)
	if err != nil {
		return false, err
	}
	n, _ := result.RowsAffected()
	return n > 0, nil
}

func (db *DB) ReleaseLock(taskID int64, workerID string) error {
	_, err := db.conn.Exec(`DELETE FROM task_locks WHERE task_id = ? AND worker_id = ?`, taskID, workerID)
	return err
}

// RefreshLock extends the expiry of a lock already held by workerID on
// taskID, as if just acquired with the given ttl. Used by the worker's
// per-run heartbeat so a long-running (or suspended) task's lock doesn't
// expire mid-run and let CleanupExpiredLocks + another worker's poll()
// double-pick the same task. A no-op (not an error) if the lock isn't held
// by workerID — e.g. it already expired and was cleaned up.
func (db *DB) RefreshLock(taskID int64, workerID string, ttl time.Duration) error {
	expiresMs := epochMs(time.Now().Add(ttl))
	_, err := db.conn.Exec(`
		UPDATE task_locks SET expires_at = ? WHERE task_id = ? AND worker_id = ?`,
		expiresMs, taskID, workerID)
	return err
}

func (db *DB) CleanupExpiredLocks() error {
	_, err := db.conn.Exec(`DELETE FROM task_locks WHERE expires_at < ?`, epochMs(time.Now()))
	return err
}

// ─── Metrics ──────────────────────────────────────────────────────────────────

func (db *DB) RecordMetric(taskID, execID int64, status string, durationMs, schedDelay int64) error {
	_, err := db.conn.Exec(`
		INSERT INTO task_metrics (task_id, execution_id, recorded_at, duration_ms, schedule_delay_ms, status)
		VALUES (?, ?, ?, ?, ?, ?)`,
		taskID, execID, epochMs(time.Now()), durationMs, schedDelay, status)
	return err
}

func (db *DB) GetMetrics(laneFilter, taskFilter string, hours int) ([]*models.MetricSummary, error) {
	if hours <= 0 {
		hours = 24
	}
	sinceMs := epochMs(time.Now().Add(-time.Duration(hours) * time.Hour))

	conditions := []string{"m.recorded_at >= ?"}
	args := []any{sinceMs}

	if laneFilter != "" {
		conditions = append(conditions, "t.lane_name = ?")
		args = append(args, laneFilter)
	}
	if taskFilter != "" {
		conditions = append(conditions, "t.name = ?")
		args = append(args, taskFilter)
	}

	where := strings.Join(conditions, " AND ")
	q := fmt.Sprintf(`
		SELECT t.name, t.lane_name,
		  SUM(CASE WHEN m.status = 'success' THEN 1 ELSE 0 END) as success_count,
		  SUM(CASE WHEN m.status = 'failed' THEN 1 ELSE 0 END) as failed_count,
		  SUM(CASE WHEN m.status = 'canceled' THEN 1 ELSE 0 END) as canceled_count,
		  AVG(m.duration_ms) as avg_duration_ms,
		  MIN(m.duration_ms) as min_duration_ms,
		  MAX(m.duration_ms) as max_duration_ms,
		  AVG(m.schedule_delay_ms) as avg_delay_ms,
		  MAX(m.recorded_at) as last_execution
		FROM task_metrics m
		JOIN tasks t ON t.id = m.task_id
		WHERE %s
		GROUP BY t.id, t.name, t.lane_name
		ORDER BY t.lane_name, t.name`, where)

	rows, err := db.conn.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []*models.MetricSummary
	for rows.Next() {
		var ms models.MetricSummary
		var lastExecMs *int64
		err := rows.Scan(&ms.TaskName, &ms.GroupName,
			&ms.SuccessCount, &ms.FailedCount, &ms.CanceledCount,
			&ms.AvgDurationMs, &ms.MinDurationMs, &ms.MaxDurationMs,
			&ms.AvgDelayMs, &lastExecMs)
		if err != nil {
			return nil, err
		}
		if lastExecMs != nil {
			t := msToTime(*lastExecMs)
			ms.LastExecution = &t
		}
		results = append(results, &ms)
	}
	return results, rows.Err()
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

type scanner interface {
	Scan(dest ...any) error
}

func scanLane(s scanner) (*models.Lane, error) {
	var l models.Lane
	var pausedAt *int64
	var pausedBy *string
	var createdAt, updatedAt int64
	err := s.Scan(&l.Name, &l.Width, &l.Paused, &pausedAt, &pausedBy, &createdAt, &updatedAt)
	if err != nil {
		return nil, err
	}
	if pausedAt != nil {
		t := msToTime(*pausedAt)
		l.PausedAt = &t
	}
	if pausedBy != nil {
		l.PausedBy = *pausedBy
	}
	l.CreatedAt = msToTime(createdAt)
	l.UpdatedAt = msToTime(updatedAt)
	return &l, nil
}

func scanTask(s scanner) (*models.Task, error) {
	var t models.Task
	var createdAt, updatedAt int64
	var enabled, paused, repeat, sudo int
	err := s.Scan(&t.ID, &t.Name, &t.LaneName, &enabled, &paused,
		&t.CooldownSeconds, &repeat, &t.Command, &t.Position, &sudo, &t.OutputFile, &createdAt, &updatedAt)
	if err != nil {
		return nil, err
	}
	t.Enabled = enabled == 1
	t.Paused = paused == 1
	t.Repeat = repeat == 1
	t.Sudo = sudo == 1
	t.CreatedAt = msToTime(createdAt)
	t.UpdatedAt = msToTime(updatedAt)
	return &t, nil
}

func scanExecution(s scanner) (*models.TaskExecution, error) {
	var e models.TaskExecution
	var scheduledAt, startedAt, finishedAt *int64
	var taskName *string
	err := s.Scan(&e.ID, &e.TaskID, &scheduledAt, &startedAt, &finishedAt,
		&e.Status, &e.ErrorMessage, &e.WorkerID, &e.DurationMs, &e.ScheduleDelayMs,
		&taskName)
	if err != nil {
		return nil, err
	}
	if taskName != nil {
		e.TaskName = *taskName
	}
	if scheduledAt != nil {
		t := msToTime(*scheduledAt)
		e.ScheduledAt = &t
	}
	if startedAt != nil {
		t := msToTime(*startedAt)
		e.StartedAt = &t
	}
	if finishedAt != nil {
		t := msToTime(*finishedAt)
		e.FinishedAt = &t
	}
	return &e, nil
}

func epochMs(t time.Time) int64 {
	return t.UnixMilli()
}

func msToTime(ms int64) time.Time {
	return time.UnixMilli(ms)
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
