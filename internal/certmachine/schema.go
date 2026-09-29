package certmachine

import (
	"database/sql"
	"errors"
	"fmt"
)

// sqlDriver is the database/sql driver name registered by modernc.org/sqlite.
// It is "sqlite", not "sqlite3" -- that name belongs to the cgo driver this
// module does not use (measured, Appendix C-1).
const sqlDriver = "sqlite"

// schemaVersion is stored in PRAGMA user_version. migrate applies the
// versioned steps below and bumps this exactly once per step; a database
// already at this version is left alone.
const schemaVersion = 2

// ddlV1 is the original (slice 1) schema. It is no longer applied by migrate,
// which now goes straight to ddlV2 on a fresh database, but it is kept for
// test fixtures that build a v1 database and exercise migrateV1toV2 (§3.1 of
// the CA-replacement plan).
//
// No foreign keys: ca and certs are independent tables, so there is nothing
// for `_pragma=foreign_keys(1)` to enforce (binding decision, slice 1 doc).
const ddlV1 = `
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

// ddlV2 is the current schema, applied directly to a fresh database. It is
// v1 plus the CA-replacement shape: ca gains role (current/previous) instead
// of the fixed id=1 singleton, and certs gains ca_id (nullable, no FK -- R3).
// A v1 database instead runs migrateV1toV2, which produces the same
// structure (verified by TestMigrate_FreshEqualsMigrated) through a rebuild
// rather than by running this string.
const ddlV2 = `
CREATE TABLE IF NOT EXISTS ca (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  role          TEXT CHECK (role IN ('current','previous')), -- nullable: see ca_one_per_role
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

-- At most one row per role. role is nullable on purpose: switch-back needs a
-- transient NULL between its role UPDATEs (a NULL passes the CHECK above and
-- this partial index), and a NULL row outside a transaction is caught by
-- checkStructuralInvariants instead.
CREATE UNIQUE INDEX IF NOT EXISTS ca_one_per_role ON ca(role) WHERE role IS NOT NULL;

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
  ca_id             INTEGER,       -- signer's ca.id; NULL means "unknown signer" (P1), no FK (R3)
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
CREATE INDEX IF NOT EXISTS certs_ca_id     ON certs(ca_id);
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
//   - No foreign_keys pragma: see the ddl comments above.
func dsn(absPath string) string {
	return fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_txlock=immediate", absPath)
}

// migrate advances a database to schemaVersion, one version step at a time,
// each step in its own transaction with PRAGMA user_version set inside that
// same transaction (SQLite rolls the pragma back with the transaction, so a
// failed step never leaves user_version out of sync with the schema). A
// database already at schemaVersion is left untouched, so reopening never
// re-runs a step destructively (FR-2).
func migrate(db *sql.DB) error {
	for {
		var version int
		if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
			return fmt.Errorf("certmachine: read schema version: %w", err)
		}
		if version >= schemaVersion {
			return nil
		}

		var step func(tx *sql.Tx) error
		switch version {
		case 0:
			step = func(tx *sql.Tx) error {
				if _, err := tx.Exec(ddlV2); err != nil {
					return fmt.Errorf("certmachine: apply schema: %w", err)
				}
				if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", schemaVersion)); err != nil {
					return fmt.Errorf("certmachine: set schema version: %w", err)
				}
				return nil
			}
		case 1:
			step = func(tx *sql.Tx) error {
				return migrateV1toV2(tx, nil)
			}
		default:
			return fmt.Errorf("certmachine: no migration step from schema version %d", version)
		}

		if err := runMigrationStep(db, step); err != nil {
			return err
		}
	}
}

// runMigrationStep runs fn inside its own transaction, committing on success
// and rolling back on error (R1: this is the only place migrate opens a
// transaction; fn must use only the tx it is given, never db).
func runMigrationStep(db *sql.DB, fn func(tx *sql.Tx) error) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("certmachine: begin migration step: %w", err)
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("certmachine: commit migration step: %w", err)
	}
	return nil
}

// migrateV1toV2 rebuilds ca into its v2 shape and adds certs.ca_id, entirely
// inside tx (R1: no db.* calls here, ever -- SetMaxOpenConns(1) means a
// second statement against db while this transaction holds the connection
// deadlocks). hook, when non-nil, is called after each numbered step; a test
// seam (R7) that the rollback test uses to fail deterministically at a named
// step, and that the atomicity tests never take for this schema-only path.
//
// Steps, matching the plan's §3.1 numbering exactly:
//  1. create ca_new in the v2 shape;
//  2. copy the existing ca row (0 or 1) into ca_new with role='current';
//  3. drop ca, rename ca_new to ca;
//  4. create the partial unique index enforcing one row per role;
//  5. add certs.ca_id and its index;
//  6. backfill ca_id in Go by checking each cert's signature against the
//     migrated CA (R6: CheckSignatureFrom, never Verify);
//  7. bump user_version.
func migrateV1toV2(tx *sql.Tx, hook func(step int) error) error {
	run := func(step int, query string, args ...any) error {
		if _, err := tx.Exec(query, args...); err != nil {
			return fmt.Errorf("certmachine: migrate v1->v2 step %d: %w", step, err)
		}
		return nil
	}
	after := func(step int) error {
		if hook == nil {
			return nil
		}
		return hook(step)
	}

	// Step 1: create ca_new in the v2 shape.
	if err := run(1, `
		CREATE TABLE ca_new (
		  id            INTEGER PRIMARY KEY AUTOINCREMENT,
		  role          TEXT CHECK (role IN ('current','previous')),
		  cert_pem      TEXT NOT NULL,
		  key_pem       TEXT NOT NULL,
		  subject       TEXT NOT NULL,
		  serial        TEXT NOT NULL,
		  not_before    TEXT NOT NULL,
		  not_after     TEXT NOT NULL,
		  fingerprint   TEXT NOT NULL,
		  imported_from TEXT,
		  created       TEXT NOT NULL
		)`); err != nil {
		return err
	}
	if err := after(1); err != nil {
		return err
	}

	// Step 2: copy the existing row (0 or 1) with role='current', keeping
	// its id.
	if err := run(2, `
		INSERT INTO ca_new (id, role, cert_pem, key_pem, subject, serial, not_before, not_after, fingerprint, imported_from, created)
		SELECT id, 'current', cert_pem, key_pem, subject, serial, not_before, not_after, fingerprint, imported_from, created FROM ca`); err != nil {
		return err
	}
	if err := after(2); err != nil {
		return err
	}

	// Step 3: create/copy/drop/rename -- the documented order for a SQLite
	// table rebuild. There are no FKs to rewrite (R3).
	if err := run(3, `DROP TABLE ca`); err != nil {
		return err
	}
	if err := run(3, `ALTER TABLE ca_new RENAME TO ca`); err != nil {
		return err
	}
	if err := after(3); err != nil {
		return err
	}

	// Step 4: one row per role.
	if err := run(4, `CREATE UNIQUE INDEX ca_one_per_role ON ca(role) WHERE role IS NOT NULL`); err != nil {
		return err
	}
	if err := after(4); err != nil {
		return err
	}

	// Step 5: certs gains a nullable signer reference, no FK (R3).
	if err := run(5, `ALTER TABLE certs ADD COLUMN ca_id INTEGER`); err != nil {
		return err
	}
	if err := run(5, `CREATE INDEX certs_ca_id ON certs(ca_id)`); err != nil {
		return err
	}
	if err := after(5); err != nil {
		return err
	}

	// Step 6: backfill ca_id in Go. Read everything into a slice and close
	// the cursor before issuing any UPDATE -- the single connection cannot
	// interleave a write with an open read cursor (R1).
	if err := backfillCAIDTx(tx); err != nil {
		return err
	}
	if err := after(6); err != nil {
		return err
	}

	// Step 7: advance the version.
	if err := run(7, fmt.Sprintf("PRAGMA user_version = %d", schemaVersion)); err != nil {
		return err
	}
	return after(7)
}

// v1CertRow is one row read from certs during the v1->v2 backfill: just
// enough to decide ca_id without holding an open cursor while writing (R1).
type v1CertRow struct {
	id      int64
	status  string
	certPEM sql.NullString
}

// backfillCAIDTx sets certs.ca_id for every row whose stored certificate's
// signature checks out against the migrated CA (R6: CheckSignatureFrom,
// never Verify -- an expired or oddly-EKU'd legacy leaf is a routine stored
// state, not a chain problem). ca_id is left NULL (P1, "unknown signer") for
// quarantined rows, rows with no parseable cert_pem, rows that fail to
// parse, and every row when no CA has been migrated yet. tx-only (R1).
func backfillCAIDTx(tx *sql.Tx) error {
	rows, err := tx.Query(`SELECT id, status, cert_pem FROM certs`)
	if err != nil {
		return fmt.Errorf("certmachine: migrate v1->v2: read certs for backfill: %w", err)
	}
	var pending []v1CertRow
	for rows.Next() {
		var r v1CertRow
		if err := rows.Scan(&r.id, &r.status, &r.certPEM); err != nil {
			rows.Close()
			return fmt.Errorf("certmachine: migrate v1->v2: scan cert for backfill: %w", err)
		}
		pending = append(pending, r)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("certmachine: migrate v1->v2: read certs for backfill: %w", err)
	}
	rows.Close() // cursor must be closed before any UPDATE below (R1)

	var caID int64
	var caCertPEM string
	switch err := tx.QueryRow(`SELECT id, cert_pem FROM ca WHERE role = 'current'`).Scan(&caID, &caCertPEM); {
	case err == nil:
		// fall through to the backfill loop below
	case errors.Is(err, sql.ErrNoRows):
		return nil // no CA yet; every row keeps ca_id = NULL
	default:
		return fmt.Errorf("certmachine: migrate v1->v2: read ca for backfill: %w", err)
	}

	caCert, err := ParseCert([]byte(caCertPEM))
	if err != nil {
		// An unparseable stored CA leaves every row NULL; this is not
		// a migration failure -- Open's structural checks don't touch
		// cert content, and an operator can still see and fix the row.
		return nil
	}

	for _, r := range pending {
		if r.status == StatusQuarantined || !r.certPEM.Valid || r.certPEM.String == "" {
			continue
		}
		leaf, err := ParseCert([]byte(r.certPEM.String))
		if err != nil {
			continue
		}
		if err := leaf.CheckSignatureFrom(caCert); err != nil {
			continue
		}
		if _, err := tx.Exec(`UPDATE certs SET ca_id = ? WHERE id = ?`, caID, r.id); err != nil {
			return fmt.Errorf("certmachine: migrate v1->v2: backfill ca_id for cert %d: %w", r.id, err)
		}
	}
	return nil
}
