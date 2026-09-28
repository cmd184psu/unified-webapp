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

// dsnFor builds the sqlite DSN for path: a real file gets WAL mode and
// "_foreign_keys=1" (modernc sqlite's per-connection FK pragma, which
// survives a driver-forced reconnect where the one-off PRAGMA exec below
// would not — see the plan's fact 1).
// ":memory:" is returned unchanged; the PRAGMA exec is what enables FKs for
// it, since there's no query-string form of a memory DSN worth using here.
func dsnFor(path string) string {
	if path == ":memory:" {
		return path
	}
	return path + "?_journal_mode=WAL&_foreign_keys=1"
}

func Open(path string) (*DB, error) {
	conn, err := sql.Open("sqlite", dsnFor(path))
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
		// tx wraps the migration's SQL and its schema_version bump in one
		// transaction, rolled back atomically on any error. Migrations 1-4
		// predate this and stay non-transactional (fact 2 in the plan);
		// migration 5 needs it since it touches three tables together.
		tx bool
	}{
		{1, `ALTER TABLE tasks ADD COLUMN output_file TEXT NOT NULL DEFAULT ''`, false},
		{2, `CREATE TABLE IF NOT EXISTS settings (key TEXT PRIMARY KEY, value TEXT NOT NULL)`, false},
		{3, migration3SQL, false},
		// pid: the OS process-group-leader PID an execution's command was
		// actually started with. Persisted (not just kept in the in-memory
		// ProcessRegistry) so that after a restart — including a non-graceful
		// one — a still-genuinely-alive process can be recognized as such
		// instead of every "running" row being assumed dead. NULL for any
		// execution that predates this column, or that never got far enough
		// to start a process; both are treated as "PID unknown" (see
		// ReconcileOrphans), an accepted case with no way to know better.
		{4, `ALTER TABLE task_executions ADD COLUMN pid INTEGER`, false},
		{5, migration5SQL, true},
	}

	for _, m := range migrations {
		if m.version <= version {
			continue
		}
		if m.tx {
			if err := db.applyMigrationTx(m.version, m.sql); err != nil {
				return err
			}
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

// applyMigrationTx runs a single migration's SQL and its schema_version
// bump inside one transaction, rolling back on any error so a partially
// applied migration never leaves the schema in a mixed state.
func (db *DB) applyMigrationTx(version int, sql string) error {
	tx, err := db.conn.Begin()
	if err != nil {
		return fmt.Errorf("migration %d: %w", version, err)
	}
	if _, err := tx.Exec(sql); err != nil {
		tx.Rollback()
		return fmt.Errorf("migration %d: %w", version, err)
	}
	if _, err := tx.Exec(`INSERT OR REPLACE INTO schema_version (version) VALUES (?)`, version); err != nil {
		tx.Rollback()
		return fmt.Errorf("migration %d: %w", version, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("migration %d: %w", version, err)
	}
	return nil
}

// migration5SQL adds Phase-1 storage for module-owned lanes and Go-function
// ("func") tasks: lane ownership/visibility/retention, task kind/label/
// payload, and per-execution progress/result (see the plan's §4.1).
const migration5SQL = `
ALTER TABLE lanes ADD COLUMN owner TEXT NOT NULL DEFAULT '';
ALTER TABLE lanes ADD COLUMN hidden INTEGER NOT NULL DEFAULT 0;
ALTER TABLE lanes ADD COLUMN retention_days INTEGER NOT NULL DEFAULT 0;
ALTER TABLE tasks ADD COLUMN kind TEXT NOT NULL DEFAULT '';
ALTER TABLE tasks ADD COLUMN label TEXT NOT NULL DEFAULT '';
ALTER TABLE tasks ADD COLUMN payload TEXT NOT NULL DEFAULT '';
ALTER TABLE tasks ADD COLUMN payload_version INTEGER NOT NULL DEFAULT 0;
ALTER TABLE task_executions ADD COLUMN progress_pct INTEGER;
ALTER TABLE task_executions ADD COLUMN progress_label TEXT;
ALTER TABLE task_executions ADD COLUMN result TEXT;
CREATE INDEX IF NOT EXISTS idx_tasks_kind ON tasks(kind);
`

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

// withTx is the only way any method in this package opens a transaction
// (R1 in the plan): fn must call only tx.Exec/
// tx.Query/tx.QueryRow, never a db.conn.* or (*DB) method, since the pool
// has exactly one connection (SetMaxOpenConns(1) above) and a second
// request for it while the first's transaction is open blocks forever.
func (db *DB) withTx(fn func(tx *sql.Tx) error) error {
	tx, err := db.conn.Begin()
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
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

const laneColumns = `name, width, paused, paused_at, paused_by, owner, hidden, retention_days, created_at, updated_at`

func (db *DB) GetLane(name string) (*models.Lane, error) {
	row := db.conn.QueryRow(`SELECT `+laneColumns+` FROM lanes WHERE name = ?`, name)
	l, err := scanLane(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return l, err
}

func (db *DB) ListLanes() ([]*models.Lane, error) {
	rows, err := db.conn.Query(`SELECT ` + laneColumns + ` FROM lanes ORDER BY name`)
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

const taskColumns = `id, name, lane_name, enabled, paused, cooldown_seconds, repeat, command, position, sudo, output_file, kind, label, payload, payload_version, created_at, updated_at`

func (db *DB) AddTask(task *models.Task) (int64, error) {
	now := epochMs(time.Now())
	// The WHERE clause on the upsert guards a func task (kind != '') against
	// being clobbered by an HTTP-driven AddTask/upsert, which never sets
	// kind/label/payload; the coordinator also returns 409 for this (P20),
	// so this is defence in depth, not the only guard.
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
		  updated_at = excluded.updated_at
		WHERE tasks.kind = ''`,
		task.Name, task.LaneName, boolInt(task.Enabled), boolInt(task.Paused),
		task.CooldownSeconds, boolInt(task.Repeat),
		task.Command, task.Position, boolInt(task.Sudo), task.OutputFile, now, now)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

func (db *DB) GetTask(name string) (*models.Task, error) {
	row := db.conn.QueryRow(`SELECT `+taskColumns+` FROM tasks WHERE name = ?`, name)
	t, err := scanTask(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return t, err
}

func (db *DB) ListTasks(laneFilter string) ([]*models.Task, error) {
	q := `SELECT ` + taskColumns + ` FROM tasks`
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

// ErrUnknownTaskField is returned by UpdateTask for any key in updates that
// isn't in updatableTaskColumns (N1): every JSON key is otherwise
// concatenated straight into the UPDATE's SET clause as a column name, and
// the new kind/label/payload/payload_version columns must not become
// writable over HTTP just because they exist.
var ErrUnknownTaskField = errors.New("unknown or read-only field")

// updatableTaskColumns is the allowlist for UpdateTask (N1). id, name,
// created_at and updated_at are deliberately absent: they're read-only or
// (for updated_at) injected by this method itself, never by the caller.
var updatableTaskColumns = map[string]bool{
	"lane_name":        true,
	"enabled":          true,
	"paused":           true,
	"cooldown_seconds": true,
	"repeat":           true,
	"command":          true,
	"position":         true,
	"sudo":             true,
	"output_file":      true,
}

func (db *DB) UpdateTask(name string, updates map[string]any) error {
	if len(updates) == 0 {
		return nil
	}
	// Validated before updated_at is injected below, so that key is never
	// mistaken for a caller-supplied (and therefore checkable) field.
	for k := range updates {
		if !updatableTaskColumns[k] {
			return fmt.Errorf("%w %q", ErrUnknownTaskField, k)
		}
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
       t.kind, t.label, t.payload, t.payload_version,
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
    OR (t.kind = '' AND (lf.last_fin IS NULL
        OR (t.repeat = 1 AND (? - CAST(lf.last_fin AS INTEGER)) >= t.cooldown_seconds * 1000)))
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

const executionColumns = `te.id, te.task_id, te.scheduled_at, te.started_at, te.finished_at,
		       te.status, te.error_message, te.worker_id, te.duration_ms, te.schedule_delay_ms,
		       te.progress_pct, te.progress_label, te.result,
		       t.name`

func (db *DB) GetPendingExecution(taskID int64) (*models.TaskExecution, error) {
	row := db.conn.QueryRow(`
		SELECT `+executionColumns+`
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
		SELECT `+executionColumns+`
		FROM task_executions te
		LEFT JOIN tasks t ON t.id = te.task_id
		WHERE te.id = ?`, id)
	exec, err := scanExecution(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return exec, err
}

// ListExecutions is ListExecutionsExcludingLanes with no exclusion, kept as
// the stable name every existing caller uses.
func (db *DB) ListExecutions(taskFilter string, limit int) ([]*models.TaskExecution, error) {
	return db.ListExecutionsExcludingLanes(taskFilter, limit, nil)
}

// ListExecutionsExcludingLanes is ListExecutions plus an owned-lane
// visibility filter (N4): any lane name in exclude is dropped. The
// unfiltered branch's LEFT JOIN keeps executions of a since-deleted task
// visible exactly as before (t.lane_name IS NULL matches them).
func (db *DB) ListExecutionsExcludingLanes(taskFilter string, limit int, exclude []string) ([]*models.TaskExecution, error) {
	if limit <= 0 {
		limit = 50
	}
	var rows *sql.Rows
	var err error
	args := []any{}
	if taskFilter != "" {
		q := `
			SELECT ` + executionColumns + `
			FROM task_executions te
			JOIN tasks t ON t.id = te.task_id
			WHERE t.name = ?`
		args = append(args, taskFilter)
		if len(exclude) > 0 {
			placeholders, exArgs := placeholdersFor(exclude)
			q += ` AND t.lane_name NOT IN (` + placeholders + `)`
			args = append(args, exArgs...)
		}
		q += ` ORDER BY te.id DESC LIMIT ?`
		args = append(args, limit)
		rows, err = db.conn.Query(q, args...)
	} else {
		q := `
			SELECT ` + executionColumns + `
			FROM task_executions te
			LEFT JOIN tasks t ON t.id = te.task_id`
		if len(exclude) > 0 {
			placeholders, exArgs := placeholdersFor(exclude)
			q += ` WHERE (t.lane_name IS NULL OR t.lane_name NOT IN (` + placeholders + `))`
			args = append(args, exArgs...)
		}
		q += ` ORDER BY te.id DESC LIMIT ?`
		args = append(args, limit)
		rows, err = db.conn.Query(q, args...)
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

// placeholdersFor returns a "?,?,..." string sized to names, plus names
// itself as a []any ready to append to a query's args.
func placeholdersFor(names []string) (string, []any) {
	ph := make([]string, len(names))
	args := make([]any, len(names))
	for i, n := range names {
		ph[i] = "?"
		args[i] = n
	}
	return strings.Join(ph, ","), args
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

// orphanedExecutionMessage is the error_message written for an execution
// MarkOrphanFailed closes out, so its cause is visible in history rather
// than looking like an unexplained failure.
const orphanedExecutionMessage = "interrupted: the server was restarted (or crashed) while this execution was in progress"

// OrphanCandidate is one execution still recorded "running" at boot — a
// freshly-started process's in-memory registries are always empty, so
// nothing here was ever registered with THIS process. Whether it's a true
// ghost (the real process is long gone — the common case after a
// non-graceful stop: kill -9, crash, OOM) or genuinely still alive (its
// process group is isolated from the parent by Setpgid, so it does not
// automatically die with it) can only be told by checking PID liveness,
// which is OS-level and deliberately not this package's job — see
// worker.ProcessAlive, used by the taskmaster package's boot-time
// reconciliation (build.go) to decide MarkOrphanFailed vs. leaving it alone.
type OrphanCandidate struct {
	ExecID int64
	TaskID int64
	// PID is nil if this execution predates the pid column, or never got
	// far enough to start a process — an accepted case where liveness
	// simply cannot be checked, and the row is treated as dead.
	PID *int
}

// ListOrphanCandidates returns every execution still "running" at boot,
// with whatever PID was persisted for it (see the pid column, migration 4).
func (db *DB) ListOrphanCandidates() ([]OrphanCandidate, error) {
	rows, err := db.conn.Query(`SELECT id, task_id, pid FROM task_executions WHERE status = 'running'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OrphanCandidate
	for rows.Next() {
		var c OrphanCandidate
		var pid *int64
		if err := rows.Scan(&c.ExecID, &c.TaskID, &pid); err != nil {
			return nil, err
		}
		if pid != nil {
			p := int(*pid)
			c.PID = &p
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// MarkOrphanFailed closes out an execution whose process is confirmed gone
// (or whose PID was never known) as "failed", records a matching metric,
// and releases that task's lock unconditionally by task_id — a lock present
// at this point is stale by definition, regardless of which worker_id (e.g.
// a since-changed hostname) last held it, since the execution it belonged
// to has just been confirmed dead.
func (db *DB) MarkOrphanFailed(execID, taskID int64) error {
	var durationMs int64
	if exec, err := db.GetExecution(execID); err == nil && exec != nil && exec.StartedAt != nil {
		durationMs = time.Since(*exec.StartedAt).Milliseconds()
	}
	msg := orphanedExecutionMessage
	if err := db.FinishExecution(execID, "failed", &msg, durationMs, 0); err != nil {
		return err
	}
	if err := db.RecordMetric(taskID, execID, "failed", durationMs, 0); err != nil {
		return err
	}
	return db.ReleaseLockByTaskID(taskID)
}

// SetExecutionPID persists the OS process-group-leader PID an execution's
// command was actually started with (see migration 4's pid column) — set
// once, right after the process starts, alongside the in-memory
// ProcessRegistry.Register call.
func (db *DB) SetExecutionPID(execID int64, pid int) error {
	_, err := db.conn.Exec(`UPDATE task_executions SET pid = ? WHERE id = ?`, pid, execID)
	return err
}

// ReleaseLockByTaskID deletes taskID's lock regardless of which worker_id
// holds it — unlike ReleaseLock (which only releases a lock held by a
// SPECIFIC worker_id, for the normal case where the same process that
// acquired a lock is the one finishing its task), this is for boot-time
// reconciliation, where the lock (if any) was necessarily acquired by a
// previous, now-confirmed-dead process — possibly under a different
// worker_id (e.g. a changed hostname) that would never match here.
func (db *DB) ReleaseLockByTaskID(taskID int64) error {
	_, err := db.conn.Exec(`DELETE FROM task_locks WHERE task_id = ?`, taskID)
	return err
}

// GetMetrics is GetMetricsExcludingLanes with no exclusion.
func (db *DB) GetMetrics(laneFilter, taskFilter string, hours int) ([]*models.MetricSummary, error) {
	return db.GetMetricsExcludingLanes(laneFilter, taskFilter, hours, nil)
}

// GetMetricsExcludingLanes is GetMetrics plus an owned-lane visibility
// filter (N4), and the single implementation behind both. Per P17, a func
// task's rows (kind != ”) are aggregated per (lane, kind) rather than per
// task, since one row per one-shot download would otherwise flood the
// metrics view; task_name then holds the kind, not the task's generated
// name, and Kind carries the kind for the UI to key on. A shell task
// (kind == ”) keeps its existing per-task grouping.
func (db *DB) GetMetricsExcludingLanes(laneFilter, taskFilter string, hours int, exclude []string) ([]*models.MetricSummary, error) {
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
	if len(exclude) > 0 {
		placeholders, exArgs := placeholdersFor(exclude)
		conditions = append(conditions, "t.lane_name NOT IN ("+placeholders+")")
		args = append(args, exArgs...)
	}

	where := strings.Join(conditions, " AND ")
	q := fmt.Sprintf(`
		SELECT
		  CASE WHEN t.kind != '' THEN t.kind ELSE t.name END AS task_name,
		  t.lane_name,
		  t.kind,
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
		GROUP BY t.lane_name, CASE WHEN t.kind != '' THEN 'k:' || t.kind ELSE 'i:' || t.id END
		ORDER BY t.lane_name, task_name`, where)

	rows, err := db.conn.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []*models.MetricSummary
	for rows.Next() {
		var ms models.MetricSummary
		var lastExecMs *int64
		err := rows.Scan(&ms.TaskName, &ms.GroupName, &ms.Kind,
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

// ─── Module-owned lanes and func (Go-function) tasks ────────────────────────
//
// Errors returned by the methods below (see the plan's §4.2).
var (
	ErrTaskNotFound     = errors.New("task not found")
	ErrTaskBusy         = errors.New("task already has a queued or running execution")
	ErrTaskSucceeded    = errors.New("job already succeeded")
	ErrLaneOwnedByOther = errors.New("lane already exists with a different owner")
)

// EnsureOwnedLane seeds a lane owned by owner on first call; a later call
// for the same (name, owner) pair is a no-op that returns the lane
// unchanged (the DB, not the seed values, is authoritative afterward — see
// N7/P8). A lane that already exists with a different owner (including ""
// — an ordinary config/UI lane) fails with ErrLaneOwnedByOther, since a
// module must never silently repurpose someone else's lane.
func (db *DB) EnsureOwnedLane(name, owner string, width, retentionDays int, hidden bool) (models.Lane, error) {
	var result models.Lane
	err := db.withTx(func(tx *sql.Tx) error {
		var existingOwner string
		err := tx.QueryRow(`SELECT owner FROM lanes WHERE name = ?`, name).Scan(&existingOwner)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			now := epochMs(time.Now())
			if _, err := tx.Exec(`
				INSERT INTO lanes (name, width, paused, owner, hidden, retention_days, created_at, updated_at)
				VALUES (?, ?, 0, ?, ?, ?, ?, ?)`,
				name, width, owner, boolInt(hidden), retentionDays, now, now); err != nil {
				return err
			}
		case err != nil:
			return err
		case existingOwner != owner:
			return fmt.Errorf(`lane %q already exists as a regular taskmaster lane (from config taskmaster.lanes or the UI); rename or delete it: %w`, name, ErrLaneOwnedByOther)
		}
		row := tx.QueryRow(`SELECT `+laneColumns+` FROM lanes WHERE name = ?`, name)
		l, err := scanLane(row)
		if err != nil {
			return err
		}
		result = *l
		return nil
	})
	return result, err
}

// SetLaneHidden and SetLaneRetention update one lane's owned-lane settings
// (N4/P10); ListHiddenLanes and ListRetentionLanes are read back at boot to
// seed the in-memory HiddenLanes set and the retention pruner.

func (db *DB) SetLaneHidden(name string, hidden bool) error {
	_, err := db.conn.Exec(`UPDATE lanes SET hidden = ?, updated_at = ? WHERE name = ?`,
		boolInt(hidden), epochMs(time.Now()), name)
	return err
}

func (db *DB) SetLaneRetention(name string, days int) error {
	_, err := db.conn.Exec(`UPDATE lanes SET retention_days = ?, updated_at = ? WHERE name = ?`,
		days, epochMs(time.Now()), name)
	return err
}

func (db *DB) ListHiddenLanes() ([]string, error) {
	rows, err := db.conn.Query(`SELECT name FROM lanes WHERE hidden = 1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		names = append(names, n)
	}
	return names, rows.Err()
}

// RetentionLane is one row of ListRetentionLanes.
type RetentionLane struct {
	Name string
	Days int
}

func (db *DB) ListRetentionLanes() ([]RetentionLane, error) {
	rows, err := db.conn.Query(`SELECT name, retention_days FROM lanes WHERE retention_days > 0`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RetentionLane
	for rows.Next() {
		var l RetentionLane
		if err := rows.Scan(&l.Name, &l.Days); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// FuncTaskSpec is the input to SubmitFuncTask: the shape of a one-shot
// Go-function job as a TM task row plus its first pending execution
// (Option A in the plan — a job IS a task).
type FuncTaskSpec struct {
	Name, Lane, Kind, Label string
	Payload                 []byte
	PayloadVersion          int
}

// SubmitFuncTask inserts a one-shot func task (fixed shell-task fields:
// enabled=1, paused=0, cooldown=0, repeat=0, command=”, sudo=0,
// output_file=” — P2) at the end of its lane's position order, together
// with its first pending execution, in one transaction.
func (db *DB) SubmitFuncTask(s FuncTaskSpec) (taskID, execID int64, err error) {
	err = db.withTx(func(tx *sql.Tx) error {
		var pos int64
		if err := tx.QueryRow(`SELECT COALESCE(MAX(position), -1) + 1 FROM tasks WHERE lane_name = ?`, s.Lane).Scan(&pos); err != nil {
			return err
		}
		now := epochMs(time.Now())
		res, err := tx.Exec(`
			INSERT INTO tasks (name, lane_name, enabled, paused, cooldown_seconds, repeat, command, position, sudo, output_file, kind, label, payload, payload_version, created_at, updated_at)
			VALUES (?, ?, 1, 0, 0, 0, '', ?, 0, '', ?, ?, ?, ?, ?, ?)`,
			s.Name, s.Lane, pos, s.Kind, s.Label, string(s.Payload), s.PayloadVersion, now, now)
		if err != nil {
			return err
		}
		taskID, err = res.LastInsertId()
		if err != nil {
			return err
		}
		res2, err := tx.Exec(`INSERT INTO task_executions (task_id, scheduled_at, status) VALUES (?, ?, 'pending')`, taskID, now)
		if err != nil {
			return err
		}
		execID, err = res2.LastInsertId()
		return err
	})
	return taskID, execID, err
}

// ClaimFuncExecution atomically transitions a pending func execution to
// running. A false return (no error) means the row was no longer pending —
// e.g. it was canceled or removed while pending — and the caller must not
// run the callback.
func (db *DB) ClaimFuncExecution(execID int64, workerID string) (bool, error) {
	now := epochMs(time.Now())
	res, err := db.conn.Exec(`
		UPDATE task_executions SET status = 'running', started_at = ?, worker_id = ?
		WHERE id = ? AND status = 'pending'`, now, workerID, execID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n == 1, nil
}

// CancelPendingFuncExecution is the DB-first half of the cancel protocol
// (worker.CancelExecution owns the rest): it conditionally cancels a still-
// pending func execution and records exactly one metric for it, and is a
// no-op (canceled == false) if the row is no longer pending, isn't a func
// execution, or doesn't exist. Called from both the HTTP cancel path and the
// worker's brake path, so whichever caller wins this race is the only one
// that records the metric/event.
func (db *DB) CancelPendingFuncExecution(execID int64) (canceled bool, taskID int64, err error) {
	err = db.withTx(func(tx *sql.Tx) error {
		now := epochMs(time.Now())
		res, err := tx.Exec(`
			UPDATE task_executions SET status = 'canceled', finished_at = ?, duration_ms = 0, schedule_delay_ms = 0
			WHERE id = ? AND status = 'pending' AND task_id IN (SELECT id FROM tasks WHERE kind != '')`,
			now, execID)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return nil
		}
		canceled = true
		if err := tx.QueryRow(`SELECT task_id FROM task_executions WHERE id = ?`, execID).Scan(&taskID); err != nil {
			return err
		}
		_, err = tx.Exec(`
			INSERT INTO task_metrics (task_id, execution_id, recorded_at, duration_ms, schedule_delay_ms, status)
			VALUES (?, ?, ?, 0, 0, 'canceled')`, taskID, execID, now)
		return err
	})
	return canceled, taskID, err
}

// UpdateTaskLabel retitles a func task (RunContext.SetLabel, P13); a no-op
// on a shell task (kind = ”), which has no label concept.
func (db *DB) UpdateTaskLabel(name, label string) error {
	_, err := db.conn.Exec(`UPDATE tasks SET label = ?, updated_at = ? WHERE name = ? AND kind != ''`,
		label, epochMs(time.Now()), name)
	return err
}

// RerunTask creates a new pending execution for a finished task, reusing
// its existing payload by construction (Option A — payload lives on the
// task). A func task additionally: refuses (ErrTaskSucceeded) when its
// latest execution succeeded (P18 — a one-shot job that succeeded is done),
// and is moved to the back of its lane's queue (FIFO re-queue); a shell
// task's position is left alone (R3).
func (db *DB) RerunTask(name string) (execID int64, err error) {
	err = db.withTx(func(tx *sql.Tx) error {
		var taskID int64
		var kind, lane string
		err := tx.QueryRow(`SELECT id, kind, lane_name FROM tasks WHERE name = ?`, name).Scan(&taskID, &kind, &lane)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrTaskNotFound
		}
		if err != nil {
			return err
		}

		var busy int
		if err := tx.QueryRow(`
			SELECT COUNT(*) FROM task_executions WHERE task_id = ? AND status IN ('pending','running')`,
			taskID).Scan(&busy); err != nil {
			return err
		}
		if busy > 0 {
			return ErrTaskBusy
		}

		if kind != "" {
			var latestStatus string
			err := tx.QueryRow(`SELECT status FROM task_executions WHERE task_id = ? ORDER BY id DESC LIMIT 1`, taskID).Scan(&latestStatus)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			if latestStatus == "success" {
				return ErrTaskSucceeded
			}
			var newPos int64
			if err := tx.QueryRow(`SELECT COALESCE(MAX(position), -1) + 1 FROM tasks WHERE lane_name = ?`, lane).Scan(&newPos); err != nil {
				return err
			}
			if _, err := tx.Exec(`UPDATE tasks SET position = ?, updated_at = ? WHERE id = ?`,
				newPos, epochMs(time.Now()), taskID); err != nil {
				return err
			}
		}

		res, err := tx.Exec(`INSERT INTO task_executions (task_id, scheduled_at, status) VALUES (?, ?, 'pending')`,
			taskID, epochMs(time.Now()))
		if err != nil {
			return err
		}
		execID, err = res.LastInsertId()
		return err
	})
	return execID, err
}

// RemoveIdleTask deletes a task (any kind) and its metrics/executions/lock
// row, refusing (ErrTaskBusy) if it has a running execution. Used both by
// the func-task delete/remove path and (inlined, since it must stay
// tx-scoped — see R1) by PruneOwnedLane below.
func (db *DB) RemoveIdleTask(name string) error {
	return db.withTx(func(tx *sql.Tx) error {
		var taskID int64
		err := tx.QueryRow(`SELECT id FROM tasks WHERE name = ?`, name).Scan(&taskID)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrTaskNotFound
		}
		if err != nil {
			return err
		}
		var running int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM task_executions WHERE task_id = ? AND status = 'running'`, taskID).Scan(&running); err != nil {
			return err
		}
		if running > 0 {
			return ErrTaskBusy
		}
		return deleteTaskRowsTx(tx, taskID)
	})
}

// deleteTaskRowsTx deletes a task's metrics, executions, lock and the task
// row itself, in that order (children before parent). It must only ever be
// called with a *sql.Tx already open on this DB's single connection (R1) —
// never call a (*DB) method (e.g. RemoveIdleTask) from inside one of these
// transactions, which is why PruneOwnedLane calls this directly instead.
func deleteTaskRowsTx(tx *sql.Tx, taskID int64) error {
	if _, err := tx.Exec(`DELETE FROM task_metrics WHERE task_id = ?`, taskID); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM task_executions WHERE task_id = ?`, taskID); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM task_locks WHERE task_id = ?`, taskID); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM tasks WHERE id = ?`, taskID); err != nil {
		return err
	}
	return nil
}

// PruneOwnedLane deletes every one-shot func task in lane whose latest
// execution finished before cutoff and which has nothing pending or
// running (N2/P10). Returns the number of tasks removed.
func (db *DB) PruneOwnedLane(lane string, cutoff time.Time) (int, error) {
	var count int
	err := db.withTx(func(tx *sql.Tx) error {
		cutoffMs := epochMs(cutoff)
		rows, err := tx.Query(`
			SELECT t.id FROM tasks t
			WHERE t.lane_name = ? AND t.kind != '' AND t.repeat = 0
			  AND NOT EXISTS (SELECT 1 FROM task_executions e WHERE e.task_id = t.id AND e.status IN ('pending','running'))
			  AND (SELECT MAX(e.finished_at) FROM task_executions e WHERE e.task_id = t.id) < ?`,
			lane, cutoffMs)
		if err != nil {
			return err
		}
		var ids []int64
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			ids = append(ids, id)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close() // read everything before writing (R1)

		for _, id := range ids {
			if err := deleteTaskRowsTx(tx, id); err != nil {
				return err
			}
		}
		count = len(ids)
		return nil
	})
	return count, err
}

// SetExecutionProgress persists the latest progress report for a running
// func execution. Called at most once per progress_interval_ms per
// execution by the throttled reporter (Q3) — never inside a transaction.
func (db *DB) SetExecutionProgress(execID int64, p models.Progress) error {
	_, err := db.conn.Exec(`UPDATE task_executions SET progress_pct = ?, progress_label = ? WHERE id = ?`,
		p.Pct, p.Label, execID)
	return err
}

// FinishFuncExecution closes out a func execution: status, timing, error
// and (on success) result, plus a final progress snapshot when p != nil.
func (db *DB) FinishFuncExecution(execID int64, status string, errMsg *string, durationMs, schedDelay int64, result []byte, p *models.Progress) error {
	now := epochMs(time.Now())
	var resultVal any
	if len(result) > 0 {
		resultVal = string(result)
	}
	if p == nil {
		_, err := db.conn.Exec(`
			UPDATE task_executions
			SET status = ?, finished_at = ?, duration_ms = ?, schedule_delay_ms = ?, error_message = ?, result = ?
			WHERE id = ?`,
			status, now, durationMs, schedDelay, errMsg, resultVal, execID)
		return err
	}
	_, err := db.conn.Exec(`
		UPDATE task_executions
		SET status = ?, finished_at = ?, duration_ms = ?, schedule_delay_ms = ?, error_message = ?, result = ?, progress_pct = ?, progress_label = ?
		WHERE id = ?`,
		status, now, durationMs, schedDelay, errMsg, resultVal, p.Pct, p.Label, execID)
	return err
}

// ExecutionLane resolves an execution's lane and task kind, for the
// hidden-lane filter and the cancel event path — both run outside any
// transaction (R1).
func (db *DB) ExecutionLane(execID int64) (lane string, kind string, found bool, err error) {
	row := db.conn.QueryRow(`
		SELECT t.lane_name, t.kind FROM task_executions te
		JOIN tasks t ON t.id = te.task_id
		WHERE te.id = ?`, execID)
	if err := row.Scan(&lane, &kind); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", "", false, nil
		}
		return "", "", false, err
	}
	return lane, kind, true, nil
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

type scanner interface {
	Scan(dest ...any) error
}

func scanLane(s scanner) (*models.Lane, error) {
	var l models.Lane
	var pausedAt *int64
	var pausedBy *string
	var hidden int
	var createdAt, updatedAt int64
	err := s.Scan(&l.Name, &l.Width, &l.Paused, &pausedAt, &pausedBy,
		&l.Owner, &hidden, &l.RetentionDays, &createdAt, &updatedAt)
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
	l.Hidden = hidden == 1
	l.CreatedAt = msToTime(createdAt)
	l.UpdatedAt = msToTime(updatedAt)
	return &l, nil
}

func scanTask(s scanner) (*models.Task, error) {
	var t models.Task
	var createdAt, updatedAt int64
	var enabled, paused, repeat, sudo int
	var payload string
	err := s.Scan(&t.ID, &t.Name, &t.LaneName, &enabled, &paused,
		&t.CooldownSeconds, &repeat, &t.Command, &t.Position, &sudo, &t.OutputFile,
		&t.Kind, &t.Label, &payload, &t.PayloadVersion, &createdAt, &updatedAt)
	if err != nil {
		return nil, err
	}
	t.Enabled = enabled == 1
	t.Paused = paused == 1
	t.Repeat = repeat == 1
	t.Sudo = sudo == 1
	if payload != "" {
		t.Payload = json.RawMessage(payload)
	}
	t.CreatedAt = msToTime(createdAt)
	t.UpdatedAt = msToTime(updatedAt)
	return &t, nil
}

func scanExecution(s scanner) (*models.TaskExecution, error) {
	var e models.TaskExecution
	var scheduledAt, startedAt, finishedAt *int64
	var taskName *string
	var progressLabel *string
	var result *string
	err := s.Scan(&e.ID, &e.TaskID, &scheduledAt, &startedAt, &finishedAt,
		&e.Status, &e.ErrorMessage, &e.WorkerID, &e.DurationMs, &e.ScheduleDelayMs,
		&e.ProgressPct, &progressLabel, &result,
		&taskName)
	if err != nil {
		return nil, err
	}
	if taskName != nil {
		e.TaskName = *taskName
	}
	if progressLabel != nil {
		e.ProgressLabel = *progressLabel
	}
	if result != nil && *result != "" {
		e.Result = json.RawMessage(*result)
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
