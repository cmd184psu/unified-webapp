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
