package store

import (
	"database/sql"
	"fmt"
)

// migration is one numbered schema step. Versions are positive, strictly
// increasing and never renumbered once released.
type migration struct {
	version int
	sql     string
}

// migrations is the append-only schema history. Numbering is owned per story:
//
//	1  B1  events (audit log)
//	2  B2  settings, backends, backend_models, role_candidates, projects, project_limits
//	3  B3  efforts, documents, approvals, queues, tasks, task_deps, attempts, stages, ...
//
// Later stories append the next integer; they never edit an earlier entry.
var migrations = []migration{
	{1, migration1SQL},
	{2, migration2SQL},
}

const migration1SQL = `
CREATE TABLE events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  ts INTEGER NOT NULL,
  actor TEXT NOT NULL,
  action TEXT NOT NULL,
  subject_type TEXT NOT NULL,
  subject_id TEXT NOT NULL,
  detail_json TEXT NOT NULL DEFAULT '{}'
);
CREATE INDEX idx_events_subject ON events(subject_type, subject_id, id);
`

// migration2SQL (B2) holds configuration: global settings (seeded with their
// defaults), backends and their models, role candidate lists and the project
// registry. No backend, model or project is seeded.
const migration2SQL = `
CREATE TABLE settings (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
INSERT INTO settings (key, value) VALUES
  ('max_attempts', '5'),
  ('stuck_threshold', '3'),
  ('max_infra_retries', '5'),
  ('plan_max_rounds', '5'),
  ('worker_timeout', '30m0s'),
  ('review_timeout', '15m0s'),
  ('plan_timeout', '30m0s'),
  ('verify_timeout', '15m0s'),
  ('opencode_path', 'opencode'),
  ('verify_slots', '2');
CREATE TABLE backends (
  name TEXT PRIMARY KEY,
  host TEXT NOT NULL,
  api_type TEXT NOT NULL CHECK (api_type IN ('ollama', 'openai_compatible', 'anthropic')),
  hosted INTEGER NOT NULL DEFAULT 0 CHECK (hosted IN (0, 1)),
  paid INTEGER NOT NULL DEFAULT 0 CHECK (paid IN (0, 1)),
  slots INTEGER NOT NULL CHECK (slots >= 1),
  retired INTEGER NOT NULL DEFAULT 0 CHECK (retired IN (0, 1)),
  created INTEGER NOT NULL,
  updated INTEGER NOT NULL
);
CREATE TABLE backend_models (
  backend TEXT NOT NULL REFERENCES backends(name),
  model TEXT NOT NULL,
  paid INTEGER NOT NULL DEFAULT 0 CHECK (paid IN (0, 1)),
  PRIMARY KEY (backend, model)
);
CREATE TABLE projects (
  slug TEXT PRIMARY KEY,
  repo_path TEXT NOT NULL UNIQUE,
  model_scope TEXT NOT NULL CHECK (model_scope IN ('local', 'any')),
  feedback_cmds_json TEXT NOT NULL DEFAULT '[]',
  created INTEGER NOT NULL,
  updated INTEGER NOT NULL
);
CREATE TABLE project_limits (
  project TEXT NOT NULL REFERENCES projects(slug),
  key TEXT NOT NULL,
  value TEXT NOT NULL,
  PRIMARY KEY (project, key)
);
CREATE TABLE role_candidates (
  scope TEXT NOT NULL CHECK (scope IN ('global', 'project')),
  project_id TEXT NOT NULL DEFAULT '',
  role TEXT NOT NULL CHECK (role IN ('worker', 'reviewer', 'planner', 'architect', 'critic', 'compiler')),
  position INTEGER NOT NULL CHECK (position >= 0),
  backend TEXT NOT NULL REFERENCES backends(name),
  model TEXT NOT NULL,
  PRIMARY KEY (scope, project_id, role, position),
  CHECK ((scope = 'global') = (project_id = '')),
  FOREIGN KEY (backend, model) REFERENCES backend_models(backend, model) DEFERRABLE INITIALLY DEFERRED
);
`

// migrate applies every migration in migs newer than the stored version, each
// with its schema_version bump in a single transaction. A database whose
// version exceeds the newest migration is refused with ErrSchemaTooNew before
// anything is changed.
func migrate(db *sql.DB, migs []migration) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_version (version INTEGER PRIMARY KEY)`); err != nil {
		return fmt.Errorf("create schema_version: %w", err)
	}
	var current int
	if err := db.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_version`).Scan(&current); err != nil {
		return fmt.Errorf("read schema_version: %w", err)
	}
	latest := 0
	if len(migs) > 0 {
		latest = migs[len(migs)-1].version
	}
	if current > latest {
		return fmt.Errorf("%w: database is at version %d, this build knows up to %d", ErrSchemaTooNew, current, latest)
	}
	for _, m := range migs {
		if m.version <= current {
			continue
		}
		if err := applyMigrationTx(db, m); err != nil {
			return err
		}
	}
	return nil
}

// applyMigrationTx runs one migration's SQL and its version bump in one
// transaction, so a failure leaves the schema and version unchanged.
func applyMigrationTx(db *sql.DB, m migration) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("migration %d: %w", m.version, err)
	}
	if _, err := tx.Exec(m.sql); err != nil {
		tx.Rollback()
		return fmt.Errorf("migration %d: %w", m.version, err)
	}
	if _, err := tx.Exec(`INSERT INTO schema_version (version) VALUES (?)`, m.version); err != nil {
		tx.Rollback()
		return fmt.Errorf("record migration %d: %w", m.version, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("migration %d: %w", m.version, err)
	}
	return nil
}
