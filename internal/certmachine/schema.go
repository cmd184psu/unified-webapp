package certmachine

import (
	"database/sql"
	"fmt"
)

// sqlDriver is the database/sql driver name registered by modernc.org/sqlite.
// It is "sqlite", not "sqlite3" -- that name belongs to the cgo driver this
// module does not use (measured, Appendix C-1).
const sqlDriver = "sqlite"

// schemaVersion is stored in PRAGMA user_version. migrate applies ddl and
// bumps this exactly once; a database already at this version is left alone.
const schemaVersion = 1

// ddl creates the certmachine schema. Every CREATE is IF NOT EXISTS so this
// string is safe to run against an already-migrated database, though migrate
// skips it entirely once user_version is current.
//
// No foreign keys: ca and certs are independent tables, so there is nothing
// for `_pragma=foreign_keys(1)` to enforce (binding decision, slice 1 doc).
const ddl = `
CREATE TABLE IF NOT EXISTS ca (
  id            INTEGER PRIMARY KEY CHECK (id = 1),   -- singleton: FR-3 no re-init
  cert_pem      TEXT NOT NULL,
  key_pem       TEXT NOT NULL,
  subject       TEXT NOT NULL,
  serial        TEXT NOT NULL,
  not_before    TEXT NOT NULL,
  not_after     TEXT NOT NULL,
  fingerprint   TEXT NOT NULL,
  imported_from TEXT,
  created       TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS certs (
  id                INTEGER PRIMARY KEY AUTOINCREMENT,
  fqdn              TEXT NOT NULL,
  serial            TEXT,          -- NULL (never '') when the cert did not parse
  not_before        TEXT,
  not_after         TEXT,
  sans              TEXT NOT NULL DEFAULT '{"dns":[],"ip":[]}',
  fingerprint       TEXT,
  status            TEXT NOT NULL CHECK (status IN ('active','archived','quarantined')),
  cert_pem          TEXT,
  key_pem           TEXT,
  imported_from     TEXT,          -- legacy source dir RELATIVE to legacy_import_dir; NULL if natively generated
  quarantine_reason TEXT,
  import_warning    TEXT,          -- e.g. does not chain to the stored CA
  created           TEXT NOT NULL
);

-- FR-2 "one active row per FQDN at most", enforced by the engine. COLLATE
-- NOCASE is load-bearing, not cosmetic: NormalizeFQDN lowercases everything
-- the UI submits, but the importer stores a legacy CN verbatim (FR-3's
-- import-everything principle), so "Web.Example.Local" and
-- "web.example.local" are the same host arriving through two doors. Under the
-- default BINARY collation both could be active at once, which is exactly the
-- state FR-2 exists to forbid. Every active-row lookup (InsertCert's
-- pre-check, ArchiveAllForFQDN) repeats this collation so the pre-check and
-- the constraint can never disagree.
CREATE UNIQUE INDEX IF NOT EXISTS certs_one_active_per_fqdn
  ON certs(fqdn COLLATE NOCASE) WHERE status = 'active';

-- FR-8 import idempotency. One row per legacy source directory; NULL
-- (natively generated) rows are exempt because SQLite treats NULLs as distinct.
CREATE UNIQUE INDEX IF NOT EXISTS certs_imported_from
  ON certs(imported_from) WHERE imported_from IS NOT NULL;

-- Lookup only, deliberately NOT unique -- see the binding decision in the
-- slice 1 doc (duplicate legacy serials from copied directories are handled
-- per-leaf in slice 7, not by a constraint here).
CREATE INDEX IF NOT EXISTS certs_serial    ON certs(serial);
CREATE INDEX IF NOT EXISTS certs_not_after ON certs(not_after);
CREATE INDEX IF NOT EXISTS certs_fqdn      ON certs(fqdn);
`

// dsn builds the connection string for absPath, an already-resolved absolute
// path to the database file.
//
//   - _pragma=busy_timeout(5000): the single connection (SetMaxOpenConns(1))
//     still serializes with any other process that might open the same file.
//   - _txlock=immediate is mandatory and is the only way to get it:
//     modernc.org/sqlite selects deferred-vs-immediate from this DSN
//     parameter alone, not from sql.TxOptions (measured, Appendix C-4).
//   - No journal_mode is set. The default rollback journal is used
//     (measured journal_mode = delete), which never creates -wal/-shm files.
//   - No foreign_keys pragma: see the ddl comment above.
func dsn(absPath string) string {
	return fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_txlock=immediate", absPath)
}

// migrate applies ddl and advances user_version, but only once: a database
// already at schemaVersion is left untouched, so reopening never re-runs DDL
// destructively (FR-2).
func migrate(db *sql.DB) error {
	var version int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return fmt.Errorf("certmachine: read schema version: %w", err)
	}
	if version >= schemaVersion {
		return nil
	}
	if _, err := db.Exec(ddl); err != nil {
		return fmt.Errorf("certmachine: apply schema: %w", err)
	}
	if _, err := db.Exec(fmt.Sprintf("PRAGMA user_version = %d", schemaVersion)); err != nil {
		return fmt.Errorf("certmachine: set schema version: %w", err)
	}
	return nil
}
