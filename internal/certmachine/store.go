package certmachine

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// Sentinel errors, following grocery's precedent (internal/grocery/model.go:41-47,
// internal/grocery/handler.go:335-352, whose doc comment explains why
// collapsing them is a bug): callers discriminate on these with errors.Is
// rather than on error strings. There is deliberately no ErrDuplicateSerial:
// certs_serial is not unique, and a duplicate serial is a per-leaf "skipped"
// outcome in the import report (slice 7), never an error a store method
// raises.
//
// ErrQuarantined and ErrConfirmMismatch are declared here (per the slice 2
// binding decision) but are raised by lifecycle.go (slice 5), not by any
// method in this file.
var (
	ErrNotFound        = errors.New("certmachine: not found")
	ErrDuplicateActive = errors.New("certmachine: an active certificate already exists for this fqdn")
	ErrDuplicateImport = errors.New("certmachine: this legacy source has already been imported")
	ErrCAExists        = errors.New("certmachine: a certificate authority already exists")
	ErrCANotFound      = errors.New("certmachine: no certificate authority has been initialized")
	ErrQuarantined     = errors.New("certmachine: certificate is quarantined")
	ErrConfirmMismatch = errors.New("certmachine: confirmation does not match")
)

// Store wraps the certmachine SQLite database. It carries no expiry logic --
// expiry is computed by callers from notAfter, never by the store.
type Store struct {
	db *sql.DB
}

// Open opens (creating if necessary) the SQLite database at dbPath, applying
// permissions and running migrations in the order pre-mortem 3.3 requires:
// MkdirAll(0700) -> Chmod(dir, 0700) -> open -> Ping() -> Chmod(db, 0600) ->
// migrate(). MkdirAll alone never re-modes an existing directory, which is
// why the explicit Chmod follows it even when the directory already exists.
func Open(dbPath string) (*Store, error) {
	abs, err := filepath.Abs(dbPath)
	if err != nil {
		return nil, fmt.Errorf("certmachine: resolve db path %s: %w", dbPath, err)
	}

	dir := filepath.Dir(abs)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("certmachine: create data dir %s: %w", dir, err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return nil, fmt.Errorf("certmachine: chmod data dir %s: %w", dir, err)
	}

	db, err := sql.Open(sqlDriver, dsn(abs))
	if err != nil {
		return nil, fmt.Errorf("certmachine: open db %s: %w", abs, err)
	}
	db.SetMaxOpenConns(1)

	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("certmachine: ping db %s: %w", abs, err)
	}
	if err := os.Chmod(abs, 0o600); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("certmachine: chmod db %s: %w", abs, err)
	}
	if err := migrate(db); err != nil {
		_ = db.Close()
		return nil, err
	}

	return &Store{db: db}, nil
}

// Close releases the underlying database handle.
func (s *Store) Close() error {
	return s.db.Close()
}

// certMetaColumns lists every certs column except cert_pem and key_pem, in
// scan order. Reused by ListCerts, FindBySerial and FindByImportedFrom, none
// of which may project either PEM (ListCerts: 500 rows of PEM would be
// 750KB-1MB against a 50ms render budget; the two Find* methods are internal
// idempotency lookups that only ever need metadata).
const certMetaColumns = `id, fqdn, serial, not_before, not_after, sans, fingerprint, status, imported_from, quarantine_reason, import_warning, created`

// rowScanner is satisfied by both *sql.Row and *sql.Rows, letting the scan
// helpers below serve single-row and multi-row queries alike.
type rowScanner interface {
	Scan(dest ...any) error
}

// scanCertMeta scans a row produced by a query selecting certMetaColumns, in
// that order. Neither PEM column is present.
func scanCertMeta(row rowScanner) (Cert, error) {
	var c Cert
	var sansJSON string
	if err := row.Scan(&c.ID, &c.FQDN, &c.Serial, &c.NotBefore, &c.NotAfter, &sansJSON,
		&c.Fingerprint, &c.Status, &c.ImportedFrom, &c.QuarantineReason, &c.ImportWarning, &c.Created); err != nil {
		return Cert{}, err
	}
	if err := json.Unmarshal([]byte(sansJSON), &c.SANs); err != nil {
		return Cert{}, fmt.Errorf("certmachine: parse sans for cert %d: %w", c.ID, err)
	}
	return c, nil
}

// ListCerts returns every cert row, all statuses, ordered chronologically by
// expiry. It projects neither PEM column -- metadata only (see
// certMetaColumns).
func (s *Store) ListCerts(ctx context.Context) ([]Cert, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+certMetaColumns+` FROM certs ORDER BY not_after ASC`)
	if err != nil {
		return nil, fmt.Errorf("certmachine: list certs: %w", err)
	}
	defer rows.Close()

	certs := make([]Cert, 0)
	for rows.Next() {
		c, err := scanCertMeta(rows)
		if err != nil {
			return nil, fmt.Errorf("certmachine: scan cert row: %w", err)
		}
		certs = append(certs, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("certmachine: list certs: %w", err)
	}
	return certs, nil
}

// GetCert returns one cert row plus cert_pem. key_pem is never included --
// the only path to it is getCertWithKey.
func (s *Store) GetCert(ctx context.Context, id int64) (*Cert, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+certMetaColumns+`, cert_pem FROM certs WHERE id = ?`, id)
	c, err := scanCertMetaThenCertPEM(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("certmachine: get cert %d: %w", id, err)
	}
	return &c, nil
}

// getCertWithKey returns one cert row plus both cert_pem and key_pem. It is
// unexported: the only path to key_pem, with exactly three callers, all
// download builders (slice 6).
func (s *Store) getCertWithKey(ctx context.Context, id int64) (*Cert, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+certMetaColumns+`, cert_pem, key_pem FROM certs WHERE id = ?`, id)
	c, err := scanCertMetaThenKey(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("certmachine: get cert with key %d: %w", id, err)
	}
	return &c, nil
}

// scanCertMetaThenCertPEM scans a row selecting certMetaColumns followed by
// cert_pem (matching GetCert's query column order).
func scanCertMetaThenCertPEM(row rowScanner) (Cert, error) {
	var c Cert
	var sansJSON string
	if err := row.Scan(&c.ID, &c.FQDN, &c.Serial, &c.NotBefore, &c.NotAfter, &sansJSON,
		&c.Fingerprint, &c.Status, &c.ImportedFrom, &c.QuarantineReason, &c.ImportWarning, &c.Created, &c.CertPEM); err != nil {
		return Cert{}, err
	}
	if err := json.Unmarshal([]byte(sansJSON), &c.SANs); err != nil {
		return Cert{}, fmt.Errorf("certmachine: parse sans for cert %d: %w", c.ID, err)
	}
	return c, nil
}

// scanCertMetaThenKey scans a row selecting certMetaColumns followed by
// cert_pem then key_pem (matching getCertWithKey's query column order).
func scanCertMetaThenKey(row rowScanner) (Cert, error) {
	var c Cert
	var sansJSON string
	if err := row.Scan(&c.ID, &c.FQDN, &c.Serial, &c.NotBefore, &c.NotAfter, &sansJSON,
		&c.Fingerprint, &c.Status, &c.ImportedFrom, &c.QuarantineReason, &c.ImportWarning, &c.Created, &c.CertPEM, &c.KeyPEM); err != nil {
		return Cert{}, err
	}
	if err := json.Unmarshal([]byte(sansJSON), &c.SANs); err != nil {
		return Cert{}, fmt.Errorf("certmachine: parse sans for cert %d: %w", c.ID, err)
	}
	return c, nil
}

// InsertCert inserts c inside tx, mapping the two unique indexes it can
// collide with onto sentinels via a pre-check SELECT rather than parsing
// driver error text (measured, Appendix C-5: SQLite names the *column*, not
// the index, in constraint error text, so there is no index name to match).
// The check is race-free because the write lock is already held
// (_txlock=immediate) by the time this runs inside tx. The unique index
// itself remains as a backstop and is not otherwise interpreted; any
// constraint failure that reaches ExecContext despite the pre-check is
// returned wrapped, uninterpreted.
//
// It returns the new row's id.
func (s *Store) InsertCert(ctx context.Context, tx *sql.Tx, c Cert) (int64, error) {
	if c.Status == StatusActive {
		var exists int
		switch err := tx.QueryRowContext(ctx, `SELECT 1 FROM certs WHERE fqdn = ? COLLATE NOCASE AND status = 'active'`, c.FQDN).Scan(&exists); {
		case err == nil:
			// Name the fqdn: an import of 200 directories that trips this
			// rolls back every leaf, and the operator's only way to find the
			// one offending row is for the error to say which host collided.
			return 0, fmt.Errorf("%w: %s", ErrDuplicateActive, c.FQDN)
		case !errors.Is(err, sql.ErrNoRows):
			return 0, fmt.Errorf("certmachine: check active fqdn %s: %w", c.FQDN, err)
		}
	}
	if c.ImportedFrom != nil {
		var exists int
		switch err := tx.QueryRowContext(ctx, `SELECT 1 FROM certs WHERE imported_from = ?`, *c.ImportedFrom).Scan(&exists); {
		case err == nil:
			return 0, ErrDuplicateImport
		case !errors.Is(err, sql.ErrNoRows):
			return 0, fmt.Errorf("certmachine: check imported_from %s: %w", *c.ImportedFrom, err)
		}
	}

	sansJSON, err := json.Marshal(c.SANs)
	if err != nil {
		return 0, fmt.Errorf("certmachine: marshal sans for %s: %w", c.FQDN, err)
	}

	res, err := tx.ExecContext(ctx, `
		INSERT INTO certs (fqdn, serial, not_before, not_after, sans, fingerprint, status, cert_pem, key_pem, imported_from, quarantine_reason, import_warning, created)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.FQDN, c.Serial, c.NotBefore, c.NotAfter, string(sansJSON), c.Fingerprint, c.Status,
		c.CertPEM, c.KeyPEM, c.ImportedFrom, c.QuarantineReason, c.ImportWarning, c.Created,
	)
	if err != nil {
		return 0, fmt.Errorf("certmachine: insert cert %s: %w", c.FQDN, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("certmachine: insert cert %s: last insert id: %w", c.FQDN, err)
	}
	return id, nil
}

// ArchiveAllForFQDN archives every currently-active row for fqdn inside tx.
// Used by Renew (slice 5), which must archive the predecessor and insert the
// new row in one transaction to keep certs_one_active_per_fqdn satisfied
// throughout. The match is COLLATE NOCASE for the same reason the index is
// (see schema.go): renewing a lowercase FQDN has to archive the mixed-case
// legacy row it is superseding, or the insert that follows collides with a
// predecessor this UPDATE failed to see.
func (s *Store) ArchiveAllForFQDN(ctx context.Context, tx *sql.Tx, fqdn string) error {
	if _, err := tx.ExecContext(ctx, `UPDATE certs SET status = 'archived' WHERE fqdn = ? COLLATE NOCASE AND status = 'active'`, fqdn); err != nil {
		return fmt.Errorf("certmachine: archive active certs for %s: %w", fqdn, err)
	}
	return nil
}

// DeleteCert permanently removes the row with id (FR-6). Confirmation
// (matching the row's fqdn) is enforced by the caller, lifecycle.go's
// Delete, not here.
func (s *Store) DeleteCert(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM certs WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("certmachine: delete cert %d: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("certmachine: delete cert %d: rows affected: %w", id, err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// CountCerts returns the total row count across all statuses. It drives the
// import-wizard offer (FR-8) and the boot log.
func (s *Store) CountCerts(ctx context.Context) (int, error) {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM certs`).Scan(&n); err != nil {
		return 0, fmt.Errorf("certmachine: count certs: %w", err)
	}
	return n, nil
}

// FindBySerial returns every row with the given serial, metadata only.
// serial is deliberately not unique (certs_serial is a lookup index, not a
// constraint), so this can return more than one row; it returns an empty
// slice, not an error, when none match. Note SQL's NULL semantics: a row
// whose serial is NULL never matches any string comparison, including one
// against "" (measured, Appendix C-5), so an unparsed cert is invisible to
// this lookup by construction -- it is one of the two skip queries for FR-8
// idempotency.
func (s *Store) FindBySerial(ctx context.Context, serial string) ([]Cert, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+certMetaColumns+` FROM certs WHERE serial = ?`, serial)
	if err != nil {
		return nil, fmt.Errorf("certmachine: find by serial %s: %w", serial, err)
	}
	defer rows.Close()

	certs := make([]Cert, 0)
	for rows.Next() {
		c, err := scanCertMeta(rows)
		if err != nil {
			return nil, fmt.Errorf("certmachine: scan cert row: %w", err)
		}
		certs = append(certs, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("certmachine: find by serial %s: %w", serial, err)
	}
	return certs, nil
}

// FindByImportedFrom returns the row imported from path, metadata only, or
// nil (with a nil error) when none exists -- this is the other of the two
// skip queries for FR-8 idempotency, and "not yet imported" is the common
// case, not an error. imported_from is unique (certs_imported_from), so at
// most one row can match.
func (s *Store) FindByImportedFrom(ctx context.Context, path string) (*Cert, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+certMetaColumns+` FROM certs WHERE imported_from = ?`, path)
	c, err := scanCertMeta(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("certmachine: find by imported_from %s: %w", path, err)
	}
	return &c, nil
}

// GetCA returns the singleton CA row, or ErrCANotFound if the CA has not
// been initialized or imported yet.
func (s *Store) GetCA(ctx context.Context) (*CA, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, cert_pem, key_pem, subject, serial, not_before, not_after, fingerprint, imported_from, created FROM ca WHERE id = 1`)
	var c CA
	err := row.Scan(&c.ID, &c.CertPEM, &c.KeyPEM, &c.Subject, &c.Serial, &c.NotBefore, &c.NotAfter, &c.Fingerprint, &c.ImportedFrom, &c.Created)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrCANotFound
		}
		return nil, fmt.Errorf("certmachine: get ca: %w", err)
	}
	return &c, nil
}

// InsertCA inserts c as the singleton CA row inside tx, always with an
// explicit id = 1 (measured, Appendix C-5: an explicit id=1 insert against an
// occupied table fails with the deterministic "UNIQUE constraint failed:
// ca.id", while an implicit insert lands on rowid 2 and fails the id=1 CHECK
// instead -- one spelling, plus this preceding pre-check, makes ErrCAExists
// deterministic).
func (s *Store) InsertCA(ctx context.Context, tx *sql.Tx, c CA) error {
	var exists int
	switch err := tx.QueryRowContext(ctx, `SELECT 1 FROM ca WHERE id = 1`).Scan(&exists); {
	case err == nil:
		return ErrCAExists
	case !errors.Is(err, sql.ErrNoRows):
		return fmt.Errorf("certmachine: check existing ca: %w", err)
	}

	_, err := tx.ExecContext(ctx, `
		INSERT INTO ca (id, cert_pem, key_pem, subject, serial, not_before, not_after, fingerprint, imported_from, created)
		VALUES (1, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.CertPEM, c.KeyPEM, c.Subject, c.Serial, c.NotBefore, c.NotAfter, c.Fingerprint, c.ImportedFrom, c.Created,
	)
	if err != nil {
		return fmt.Errorf("certmachine: insert ca: %w", err)
	}
	return nil
}

// WithTx runs fn inside a transaction opened with the DSN's
// _txlock=immediate, committing on success and rolling back on error or
// panic (the panic is re-raised after rollback, never swallowed).
func (s *Store) WithTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("certmachine: begin transaction: %w", err)
	}
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback()
			panic(p)
		}
	}()

	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("certmachine: commit transaction: %w", err)
	}
	return nil
}
