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
//	6  F2  stages.role gains 'cleanup' (table rebuild); projects.cleanup_enabled
//	7  F3  stages.tokens; efforts.budget_tokens, efforts.budget_seconds
//	8  F4  project_plugins
//	9  P2  backends.api_key, browse_roots setting
//	10 P3  backend_models.context_tokens, projects.definition_of_done_json, frd_parts
//	11     backends.api_type may be 'opencode' (opencode's own built-in provider)
//
// Later stories append the next integer; they never edit an earlier entry.
var migrations = []migration{
	{1, migration1SQL},
	{2, migration2SQL},
	{3, migration3SQL},
	{4, migration4SQL},
	{5, migration5SQL},
	{6, migration6SQL},
	{7, migration7SQL},
	{8, migration8SQL},
	{9, migration9SQL},
	{10, migration10SQL},
	{11, migration11SQL},
	{12, migration12SQL},
	{13, migration13SQL},
	{14, migration14SQL},
	{15, migration15SQL},
	{16, migration16SQL},
	{17, migration17SQL},
	{18, migration18SQL},
}

// migration18SQL (H15e) marks a model whose weights leave no room for the
// minimum context inside its host's memory budget. Dispatch and planning pass
// over it; Extend windows sets and clears the mark.
const migration18SQL = `
ALTER TABLE backend_models ADD COLUMN too_big INTEGER NOT NULL DEFAULT 0 CHECK (too_big IN (0, 1));
`

// migration17SQL (H15d) gives each backend a memory budget and a cap on the
// context window it is extended to. 0 means no budget set (windows are not
// extended) and no cap.
const migration17SQL = `
ALTER TABLE backends ADD COLUMN memory_budget_mb INTEGER NOT NULL DEFAULT 0 CHECK (memory_budget_mb >= 0);
ALTER TABLE backends ADD COLUMN window_cap_tokens INTEGER NOT NULL DEFAULT 0 CHECK (window_cap_tokens >= 0);
`

// migration16SQL (H15) records the context window a model is actually served
// with, apart from the maximum it supports (context_tokens): configured by the
// owner, or learned when a host cut a model off. 0 means not known; the source
// says where the number came from.
const migration16SQL = `
ALTER TABLE backend_models ADD COLUMN window_tokens INTEGER NOT NULL DEFAULT 0 CHECK (window_tokens >= 0);
ALTER TABLE backend_models ADD COLUMN window_source TEXT NOT NULL DEFAULT '' CHECK (window_source IN ('', 'set', 'observed', 'host'));
INSERT INTO settings (key, value) VALUES ('context_floor_tokens', '16384');
`

// migration15SQL (H10) records whether the host says a model supports tool
// calls, which every runner role needs: yes, no, or empty when not known.
const migration15SQL = `
ALTER TABLE backend_models ADD COLUMN tools TEXT NOT NULL DEFAULT '' CHECK (tools IN ('', 'yes', 'no'));
`

// migration14SQL (H6) tracks the installs of a model on an Ollama host: one row
// per pull, with its progress, so a page reload or a restart still shows it.
const migration14SQL = `
CREATE TABLE model_installs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  backend TEXT NOT NULL,
  model TEXT NOT NULL,
  tier INTEGER NOT NULL DEFAULT 0 CHECK (tier BETWEEN 0 AND 3),
  state TEXT NOT NULL CHECK (state IN ('running', 'done', 'failed', 'cancelled')),
  status TEXT NOT NULL DEFAULT '',
  completed INTEGER NOT NULL DEFAULT 0,
  total INTEGER NOT NULL DEFAULT 0,
  error TEXT NOT NULL DEFAULT '',
  started INTEGER NOT NULL,
  finished INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX idx_model_installs_backend ON model_installs(backend);
`

// migration13SQL (H7) records when the owner cleared a model's strikes. The
// strikes themselves are derived from the stages and attempts tables; only
// those after the reset count.
const migration13SQL = `
CREATE TABLE strike_resets (
  backend TEXT NOT NULL,
  model TEXT NOT NULL,
  role TEXT NOT NULL,
  at INTEGER NOT NULL,
  PRIMARY KEY (backend, model, role)
);
`

// migration12SQL (H3) rates a model: 1 strong, 2 capable, 3 light, 0 not rated
// yet (treated as 2 where a tier matters).
const migration12SQL = `
ALTER TABLE backend_models ADD COLUMN tier INTEGER NOT NULL DEFAULT 0 CHECK (tier BETWEEN 0 AND 3);
`

// migration11SQL lets a backend be opencode's built-in provider (no host, no
// key). backends is rebuilt with the wider api_type CHECK; the rows copy as
// they are.
const migration11SQL = `
CREATE TABLE backends_new (
  name TEXT PRIMARY KEY,
  host TEXT NOT NULL,
  api_type TEXT NOT NULL CHECK (api_type IN ('ollama', 'openai_compatible', 'anthropic', 'opencode')),
  hosted INTEGER NOT NULL DEFAULT 0 CHECK (hosted IN (0, 1)),
  paid INTEGER NOT NULL DEFAULT 0 CHECK (paid IN (0, 1)),
  slots INTEGER NOT NULL CHECK (slots >= 1),
  retired INTEGER NOT NULL DEFAULT 0 CHECK (retired IN (0, 1)),
  created INTEGER NOT NULL,
  updated INTEGER NOT NULL,
  api_key TEXT NOT NULL DEFAULT ''
);
INSERT INTO backends_new (name, host, api_type, hosted, paid, slots, retired, created, updated, api_key)
  SELECT name, host, api_type, hosted, paid, slots, retired, created, updated, api_key FROM backends;
DROP TABLE backends;
ALTER TABLE backends_new RENAME TO backends;
`

// migration10SQL (P3) adds what the FRD automation needs: a model's context
// window (0 means unknown), a project's optional definition-of-done override
// (empty means derive it from the toolchain), and the parts a split FRD is
// stored in. started_effort is the effort a part was started as (0 = not yet).
const migration10SQL = `
ALTER TABLE backend_models ADD COLUMN context_tokens INTEGER NOT NULL DEFAULT 0 CHECK (context_tokens >= 0);
ALTER TABLE projects ADD COLUMN definition_of_done_json TEXT NOT NULL DEFAULT '';
CREATE TABLE frd_parts (
  effort INTEGER NOT NULL REFERENCES efforts(id) ON DELETE CASCADE,
  n INTEGER NOT NULL CHECK (n >= 1),
  title TEXT NOT NULL,
  summary TEXT NOT NULL DEFAULT '',
  json TEXT NOT NULL,
  tokens INTEGER NOT NULL DEFAULT 0,
  started_effort INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (effort, n)
);
`

// migration9SQL (P2) keeps a hosted backend's API key in the database, so it
// can be changed without a restart, and adds the directory picker's roots
// (empty means the server user's home directory).
const migration9SQL = `
ALTER TABLE backends ADD COLUMN api_key TEXT NOT NULL DEFAULT '';
INSERT INTO settings (key, value) VALUES ('browse_roots', '');
`

// migration8SQL (F4) holds the runner plugins a project opted into (OA3). A
// row is a pin: name and an exact version. A project with no rows uses none.
const migration8SQL = `
CREATE TABLE project_plugins (
  project TEXT NOT NULL REFERENCES projects(slug),
  name TEXT NOT NULL,
  version TEXT NOT NULL,
  PRIMARY KEY (project, name)
);
`

// migration7SQL (F3) adds the runner-reported token usage of a stage and the
// per-effort budgets (AT12); 0 budgets mean no limit.
const migration7SQL = `
ALTER TABLE stages ADD COLUMN tokens INTEGER NOT NULL DEFAULT 0 CHECK (tokens >= 0);
ALTER TABLE efforts ADD COLUMN budget_tokens INTEGER NOT NULL DEFAULT 0 CHECK (budget_tokens >= 0);
ALTER TABLE efforts ADD COLUMN budget_seconds INTEGER NOT NULL DEFAULT 0 CHECK (budget_seconds >= 0);
`

// rebuildsTables lists the migrations that rebuild a table other tables refer
// to. SQLite cannot widen a CHECK constraint in place, and dropping the old
// table with foreign keys on would fire the referencing rows' ON DELETE
// actions, so these run with foreign keys off (the documented procedure) and
// are checked with foreign_key_check before they commit.
var rebuildsTables = map[int]bool{6: true, 11: true}

// migration6SQL (F2) lets a stage have the cleanup role (AT11) and adds the
// per-project cleanup switch. stages is rebuilt with the same columns, so rows
// copy in order.
const migration6SQL = `
CREATE TABLE stages_new (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  attempt INTEGER REFERENCES attempts(id) ON DELETE CASCADE,
  effort INTEGER REFERENCES efforts(id) ON DELETE CASCADE,
  role TEXT NOT NULL CHECK (role IN ('worker', 'reviewer', 'planner', 'architect', 'critic', 'compiler', 'verify', 'merge', 'cleanup')),
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
INSERT INTO stages_new SELECT * FROM stages;
DROP TABLE stages;
ALTER TABLE stages_new RENAME TO stages;
CREATE INDEX idx_stages_attempt ON stages(attempt);
CREATE INDEX idx_stages_effort ON stages(effort);
CREATE INDEX idx_stages_open ON stages(state) WHERE state <> 'done';
ALTER TABLE projects ADD COLUMN cleanup_enabled INTEGER NOT NULL DEFAULT 0 CHECK (cleanup_enabled IN (0, 1));
`

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
	if rebuildsTables[m.version] {
		// The pool holds one connection, so the pragma and the transaction
		// below share it.
		if _, err := db.Exec(`PRAGMA foreign_keys = OFF`); err != nil {
			return fmt.Errorf("migration %d: %w", m.version, err)
		}
		defer db.Exec(`PRAGMA foreign_keys = ON`)
	}
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("migration %d: %w", m.version, err)
	}
	if _, err := tx.Exec(m.sql); err != nil {
		tx.Rollback()
		return fmt.Errorf("migration %d: %w", m.version, err)
	}
	if rebuildsTables[m.version] {
		var bad int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM pragma_foreign_key_check`).Scan(&bad); err != nil || bad > 0 {
			tx.Rollback()
			return fmt.Errorf("migration %d: foreign key check failed (%d violations): %v", m.version, bad, err)
		}
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
