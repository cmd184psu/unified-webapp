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
//	4  C8  backend_cooldown setting
//	5  F1  backend_models.max_concurrent
//
// Later stories append the next integer; they never edit an earlier entry.
var migrations = []migration{
	{1, migration1SQL},
	{2, migration2SQL},
	{3, migration3SQL},
	{4, migration4SQL},
	{5, migration5SQL},
}

// migration5SQL (F1) adds the per-model concurrency hint (PX5): 0 means no
// limit beyond the backend's slots.
const migration5SQL = `
ALTER TABLE backend_models ADD COLUMN max_concurrent INTEGER NOT NULL DEFAULT 0 CHECK (max_concurrent >= 0);
`

// migration4SQL (C8) adds the backend health cooldown setting (MB7).
const migration4SQL = `
INSERT INTO settings (key, value) VALUES ('backend_cooldown', '2m0s');
`

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

// migration3SQL (B3) holds efforts and their documents, queues, tasks,
// attempts and stages. Task status is deliberately not a column: it is derived
// (model.DeriveTaskStatus) from attempts, stages, overrides and resets.
const migration3SQL = `
CREATE TABLE efforts (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  project TEXT NOT NULL REFERENCES projects(slug),
  title TEXT NOT NULL,
  status TEXT NOT NULL CHECK (status IN ('draft', 'planning', 'plan_ready', 'plan_approved', 'compiling', 'queue_draft', 'queue_approved', 'running', 'paused', 'complete', 'abandoned')),
  run_state TEXT NOT NULL DEFAULT 'stopped' CHECK (run_state IN ('stopped', 'running', 'pausing', 'paused')),
  integration_branch TEXT NOT NULL DEFAULT '',
  base_commit TEXT NOT NULL DEFAULT '',
  yolo INTEGER NOT NULL DEFAULT 0 CHECK (yolo IN (0, 1)),
  deliberate INTEGER NOT NULL DEFAULT 0 CHECK (deliberate IN (0, 1)),
  created INTEGER NOT NULL,
  updated INTEGER NOT NULL
);
CREATE UNIQUE INDEX idx_efforts_one_active ON efforts(project) WHERE status NOT IN ('complete', 'abandoned');
CREATE TABLE documents (
  effort INTEGER NOT NULL REFERENCES efforts(id) ON DELETE CASCADE,
  kind TEXT NOT NULL CHECK (kind IN ('frd', 'plan', 'review')),
  revision INTEGER NOT NULL CHECK (revision >= 1),
  json TEXT NOT NULL,
  created_by TEXT NOT NULL,
  created INTEGER NOT NULL,
  PRIMARY KEY (effort, kind, revision)
);
CREATE TABLE approvals (
  effort INTEGER NOT NULL,
  kind TEXT NOT NULL CHECK (kind IN ('frd', 'plan')),
  revision INTEGER NOT NULL,
  approved_by TEXT NOT NULL,
  at INTEGER NOT NULL,
  PRIMARY KEY (effort, kind, revision),
  FOREIGN KEY (effort, kind, revision) REFERENCES documents(effort, kind, revision) ON DELETE CASCADE
);
CREATE TABLE queues (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  effort INTEGER NOT NULL UNIQUE REFERENCES efforts(id) ON DELETE CASCADE,
  revision INTEGER NOT NULL CHECK (revision >= 1),
  hash TEXT NOT NULL,
  status TEXT NOT NULL CHECK (status IN ('draft', 'approved')),
  approved_by TEXT NOT NULL DEFAULT '',
  approved_at INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE tasks (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  queue INTEGER NOT NULL REFERENCES queues(id) ON DELETE CASCADE,
  task_key TEXT NOT NULL,
  phase TEXT NOT NULL DEFAULT '',
  title TEXT NOT NULL,
  prompt TEXT NOT NULL,
  acceptance_json TEXT NOT NULL,
  files_touched_json TEXT NOT NULL,
  waiting_reason TEXT NOT NULL DEFAULT '',
  UNIQUE (queue, task_key)
);
CREATE TABLE task_deps (
  task INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  depends_on INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  PRIMARY KEY (task, depends_on),
  CHECK (task <> depends_on)
);
CREATE TABLE attempts (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  task INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  n INTEGER NOT NULL CHECK (n >= 1),
  kind TEXT NOT NULL CHECK (kind IN ('normal', 'repair')),
  worktree TEXT NOT NULL DEFAULT '',
  outcome TEXT NOT NULL DEFAULT '' CHECK (outcome IN ('', 'passed', 'failed', 'blocked', 'policy_violation', 'abandoned')),
  fingerprint TEXT NOT NULL DEFAULT '',
  worker_model TEXT NOT NULL DEFAULT '',
  reviewer_model TEXT NOT NULL DEFAULT '',
  started INTEGER NOT NULL,
  finished INTEGER NOT NULL DEFAULT 0,
  UNIQUE (task, n)
);
CREATE UNIQUE INDEX idx_attempts_one_open ON attempts(task) WHERE outcome = '';
CREATE INDEX idx_task_deps_on ON task_deps(depends_on);
CREATE TABLE stages (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  attempt INTEGER REFERENCES attempts(id) ON DELETE CASCADE,
  effort INTEGER REFERENCES efforts(id) ON DELETE CASCADE,
  role TEXT NOT NULL CHECK (role IN ('worker', 'reviewer', 'planner', 'architect', 'critic', 'compiler', 'verify', 'merge')),
  backend TEXT NOT NULL DEFAULT '',
  model TEXT NOT NULL DEFAULT '',
  lane_job_id TEXT NOT NULL DEFAULT '',
  queue_hash TEXT NOT NULL DEFAULT '',
  state TEXT NOT NULL CHECK (state IN ('queued', 'running', 'done')),
  outcome TEXT NOT NULL DEFAULT '',
  raw_output TEXT NOT NULL DEFAULT '',
  parsed_json TEXT NOT NULL DEFAULT '',
  prompt_path TEXT NOT NULL DEFAULT '',
  started INTEGER NOT NULL DEFAULT 0,
  finished INTEGER NOT NULL DEFAULT 0,
  CHECK ((attempt IS NULL) <> (effort IS NULL))
);
CREATE INDEX idx_stages_attempt ON stages(attempt);
CREATE INDEX idx_stages_effort ON stages(effort);
CREATE INDEX idx_stages_open ON stages(state) WHERE state <> 'done';
CREATE TABLE infra_retries (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  task INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  stage INTEGER REFERENCES stages(id) ON DELETE SET NULL,
  reason TEXT NOT NULL,
  at INTEGER NOT NULL
);
CREATE INDEX idx_infra_retries_task ON infra_retries(task);
CREATE TABLE overrides (
  task INTEGER PRIMARY KEY REFERENCES tasks(id) ON DELETE CASCADE,
  actor TEXT NOT NULL,
  reason TEXT NOT NULL,
  at INTEGER NOT NULL
);
CREATE TABLE task_resets (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  task INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  after_attempt_id INTEGER NOT NULL,
  after_infra_id INTEGER NOT NULL,
  clean INTEGER NOT NULL DEFAULT 0 CHECK (clean IN (0, 1)),
  actor TEXT NOT NULL,
  at INTEGER NOT NULL
);
CREATE INDEX idx_task_resets_task ON task_resets(task);
CREATE TABLE progress_log (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  task INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  attempt INTEGER REFERENCES attempts(id) ON DELETE SET NULL,
  entry TEXT NOT NULL,
  at INTEGER NOT NULL
);
CREATE INDEX idx_progress_log_task ON progress_log(task);
CREATE TABLE baselines (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  effort INTEGER NOT NULL REFERENCES efforts(id) ON DELETE CASCADE,
  commit_sha TEXT NOT NULL,
  report_json TEXT NOT NULL,
  at INTEGER NOT NULL
);
CREATE INDEX idx_baselines_effort ON baselines(effort);
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
