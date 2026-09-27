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

	// ErrUnknownSigner means a cert row's ca_id is NULL (P1): certAndCA
	// cannot resolve its signer, so haproxy.pem and the bundle -- both of
	// which must embed a specific CA cert -- have nothing correct to embed.
	// Renewing the row clears this by assigning it a current ca_id.
	ErrUnknownSigner = errors.New("certmachine: certificate's signing CA is unknown; re-issue it")

	// ErrNoPreviousCA means SwitchBack was called with no previous CA to
	// promote back to current (D9).
	ErrNoPreviousCA = errors.New("certmachine: there is no previous certificate authority to switch back to")

	// ErrPreviousStaleChoiceRequired means ReplaceCA was called with
	// previousStale omitted while the outgoing previous CA still signs at
	// least one active certificate (P4): the caller must say reissue or
	// delete before Replace can proceed.
	ErrPreviousStaleChoiceRequired = errors.New("certmachine: previousStale is required because the previous certificate authority still signs active certificates")

	// ErrConcurrentChange means ReplaceCA's in-transaction re-read of its
	// concurrency snapshot (§3.4) did not match the snapshot taken before
	// the transaction opened: some other call changed the current/previous
	// CA or one of the row sets Replace was about to act on. Nothing is
	// written when this is returned.
	ErrConcurrentChange = errors.New("certmachine: the certificate authorities or certificates changed concurrently; retry")
)

// Store wraps the certmachine SQLite database. It carries no expiry logic --
// expiry is computed by callers from notAfter, never by the store.
//
// hook and genCA are the CA-replacement plan's only test seams (R7). Both
// are nil-safe: hook is nil in production (s.runHook turns that into a
// no-op), and genCA defaults to GenerateNamedCA (set by Open) so every
// CA-changing operation can call s.genCA directly rather than branching on
// whether a test overrode it.
type Store struct {
	db    *sql.DB
	hook  func(step string) error
	genCA func(name string) (certPEM, keyPEM []byte, err error)
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

	s := &Store{db: db, genCA: GenerateNamedCA}
	if err := s.checkStructuralInvariants(context.Background()); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("certmachine: %s failed structural invariants after migration: %w", abs, err)
	}

	return s, nil
}

// Close releases the underlying database handle.
func (s *Store) Close() error {
	return s.db.Close()
}

// runHook calls s.hook(step) when a test has set one (R7), turning the nil
// production default into a no-op so every CA-changing operation can call
// this unconditionally rather than checking s.hook itself.
func (s *Store) runHook(step string) error {
	if s.hook == nil {
		return nil
	}
	return s.hook(step)
}

// certMetaColumns lists every certs column except cert_pem and key_pem, plus
// the CA-replacement plan's ca.subject join column, in scan order. Reused by
// ListCerts, FindBySerial and FindByImportedFrom, none of which may project
// either leaf PEM (ListCerts: 500 rows of PEM would be 750KB-1MB against a
// 50ms render budget; the two Find* methods are internal idempotency lookups
// that only ever need metadata). Every column is qualified with c. (and
// ca.subject with ca.) because certsJoinCA's LEFT JOIN makes bare "id" and
// bare "subject" ambiguous.
const certMetaColumns = `c.id, c.fqdn, c.serial, c.not_before, c.not_after, c.sans, c.fingerprint, c.status, c.imported_from, c.quarantine_reason, c.import_warning, c.ca_id, ca.subject, c.created`

// certsJoinCA is the FROM clause every cert metadata query uses to resolve
// CAID/CASubject/Stale (§3.2): LEFT JOIN because ca_id is nullable (P1,
// "unknown signer") and a NULL ca_id must still produce a row, just with
// CAID/CASubject left nil.
const certsJoinCA = `certs c LEFT JOIN ca ON c.ca_id = ca.id`

// rowScanner is satisfied by both *sql.Row and *sql.Rows, letting the scan
// helpers below serve single-row and multi-row queries alike.
type rowScanner interface {
	Scan(dest ...any) error
}

// currentCAID returns the current CA's id, or 0 (ok=false) when none exists.
// Every cert metadata scan needs this once per query to compute Stale
// (§3.3 invariant 5: Stale holds exactly when ca_id is non-NULL and differs
// from the current CA's id).
func (s *Store) currentCAID(ctx context.Context) (id int64, ok bool, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT id FROM ca WHERE role = 'current'`).Scan(&id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, false, nil
		}
		return 0, false, err
	}
	return id, true, nil
}

// applyCASignerFields fills CAID, CASubject and Stale from a certMetaColumns
// join's trailing ca_id/ca.subject columns. currentOK is false when no
// current CA exists, in which case every non-NULL ca_id is stale by
// construction (there is nothing for it to equal).
func applyCASignerFields(c *Cert, caID sql.NullInt64, caSubject sql.NullString, currentID int64, currentOK bool) {
	if caID.Valid {
		id := caID.Int64
		c.CAID = &id
		c.Stale = !currentOK || id != currentID
	}
	if caSubject.Valid {
		subj := caSubject.String
		c.CASubject = &subj
	}
}

// scanCertMeta scans a row produced by a query selecting certMetaColumns
// over certsJoinCA, in that order. Neither leaf PEM column is present.
func scanCertMeta(row rowScanner, currentID int64, currentOK bool) (Cert, error) {
	var c Cert
	var sansJSON string
	var caID sql.NullInt64
	var caSubject sql.NullString
	if err := row.Scan(&c.ID, &c.FQDN, &c.Serial, &c.NotBefore, &c.NotAfter, &sansJSON,
		&c.Fingerprint, &c.Status, &c.ImportedFrom, &c.QuarantineReason, &c.ImportWarning,
		&caID, &caSubject, &c.Created); err != nil {
		return Cert{}, err
	}
	if err := json.Unmarshal([]byte(sansJSON), &c.SANs); err != nil {
		return Cert{}, fmt.Errorf("certmachine: parse sans for cert %d: %w", c.ID, err)
	}
	applyCASignerFields(&c, caID, caSubject, currentID, currentOK)
	return c, nil
}

// ListCerts returns every cert row, all statuses, ordered chronologically by
// expiry. It projects neither leaf PEM column -- metadata only (see
// certMetaColumns) -- but does resolve the signing CA's id/subject and the
// Stale fact via certsJoinCA.
func (s *Store) ListCerts(ctx context.Context) ([]Cert, error) {
	currentID, currentOK, err := s.currentCAID(ctx)
	if err != nil {
		return nil, fmt.Errorf("certmachine: list certs: current ca: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, `SELECT `+certMetaColumns+` FROM `+certsJoinCA+` ORDER BY c.not_after ASC`)
	if err != nil {
		return nil, fmt.Errorf("certmachine: list certs: %w", err)
	}
	defer rows.Close()

	certs := make([]Cert, 0)
	for rows.Next() {
		c, err := scanCertMeta(rows, currentID, currentOK)
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
	currentID, currentOK, err := s.currentCAID(ctx)
	if err != nil {
		return nil, fmt.Errorf("certmachine: get cert %d: current ca: %w", id, err)
	}
	row := s.db.QueryRowContext(ctx, `SELECT `+certMetaColumns+`, c.cert_pem FROM `+certsJoinCA+` WHERE c.id = ?`, id)
	c, err := scanCertMetaThenCertPEM(row, currentID, currentOK)
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
	currentID, currentOK, err := s.currentCAID(ctx)
	if err != nil {
		return nil, fmt.Errorf("certmachine: get cert with key %d: current ca: %w", id, err)
	}
	row := s.db.QueryRowContext(ctx, `SELECT `+certMetaColumns+`, c.cert_pem, c.key_pem FROM `+certsJoinCA+` WHERE c.id = ?`, id)
	c, err := scanCertMetaThenKey(row, currentID, currentOK)
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
func scanCertMetaThenCertPEM(row rowScanner, currentID int64, currentOK bool) (Cert, error) {
	var c Cert
	var sansJSON string
	var caID sql.NullInt64
	var caSubject sql.NullString
	if err := row.Scan(&c.ID, &c.FQDN, &c.Serial, &c.NotBefore, &c.NotAfter, &sansJSON,
		&c.Fingerprint, &c.Status, &c.ImportedFrom, &c.QuarantineReason, &c.ImportWarning,
		&caID, &caSubject, &c.Created, &c.CertPEM); err != nil {
		return Cert{}, err
	}
	if err := json.Unmarshal([]byte(sansJSON), &c.SANs); err != nil {
		return Cert{}, fmt.Errorf("certmachine: parse sans for cert %d: %w", c.ID, err)
	}
	applyCASignerFields(&c, caID, caSubject, currentID, currentOK)
	return c, nil
}

// scanCertMetaThenKey scans a row selecting certMetaColumns followed by
// cert_pem then key_pem (matching getCertWithKey's query column order).
func scanCertMetaThenKey(row rowScanner, currentID int64, currentOK bool) (Cert, error) {
	var c Cert
	var sansJSON string
	var caID sql.NullInt64
	var caSubject sql.NullString
	if err := row.Scan(&c.ID, &c.FQDN, &c.Serial, &c.NotBefore, &c.NotAfter, &sansJSON,
		&c.Fingerprint, &c.Status, &c.ImportedFrom, &c.QuarantineReason, &c.ImportWarning,
		&caID, &caSubject, &c.Created, &c.CertPEM, &c.KeyPEM); err != nil {
		return Cert{}, err
	}
	if err := json.Unmarshal([]byte(sansJSON), &c.SANs); err != nil {
		return Cert{}, fmt.Errorf("certmachine: parse sans for cert %d: %w", c.ID, err)
	}
	applyCASignerFields(&c, caID, caSubject, currentID, currentOK)
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
		INSERT INTO certs (fqdn, serial, not_before, not_after, sans, fingerprint, status, cert_pem, key_pem, imported_from, quarantine_reason, import_warning, ca_id, created)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.FQDN, c.Serial, c.NotBefore, c.NotAfter, string(sansJSON), c.Fingerprint, c.Status,
		c.CertPEM, c.KeyPEM, c.ImportedFrom, c.QuarantineReason, c.ImportWarning, c.CAID, c.Created,
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
	currentID, currentOK, err := s.currentCAID(ctx)
	if err != nil {
		return nil, fmt.Errorf("certmachine: find by serial %s: current ca: %w", serial, err)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+certMetaColumns+` FROM `+certsJoinCA+` WHERE c.serial = ?`, serial)
	if err != nil {
		return nil, fmt.Errorf("certmachine: find by serial %s: %w", serial, err)
	}
	defer rows.Close()

	certs := make([]Cert, 0)
	for rows.Next() {
		c, err := scanCertMeta(rows, currentID, currentOK)
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
	currentID, currentOK, err := s.currentCAID(ctx)
	if err != nil {
		return nil, fmt.Errorf("certmachine: find by imported_from %s: current ca: %w", path, err)
	}
	row := s.db.QueryRowContext(ctx, `SELECT `+certMetaColumns+` FROM `+certsJoinCA+` WHERE c.imported_from = ?`, path)
	c, err := scanCertMeta(row, currentID, currentOK)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("certmachine: find by imported_from %s: %w", path, err)
	}
	return &c, nil
}

// caColumns lists every ca column in scan order, shared by GetCurrentCA,
// GetCAByID and GetPreviousCA so the three queries can never drift apart on
// column order.
const caColumns = `id, role, cert_pem, key_pem, subject, serial, not_before, not_after, fingerprint, imported_from, created`

// scanCA scans a row selecting caColumns, in that order.
func scanCA(row rowScanner) (CA, error) {
	var c CA
	err := row.Scan(&c.ID, &c.Role, &c.CertPEM, &c.KeyPEM, &c.Subject, &c.Serial, &c.NotBefore, &c.NotAfter, &c.Fingerprint, &c.ImportedFrom, &c.Created)
	return c, err
}

// GetCurrentCA returns the current CA row, or ErrCANotFound if none has been
// initialized or imported yet (R5 of the CA-replacement plan: the GetCA ->
// GetCurrentCA rename, with no alias, so every call site had to decide which
// CA it means).
func (s *Store) GetCurrentCA(ctx context.Context) (*CA, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+caColumns+` FROM ca WHERE role = 'current'`)
	c, err := scanCA(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrCANotFound
		}
		return nil, fmt.Errorf("certmachine: get current ca: %w", err)
	}
	return &c, nil
}

// GetCAByID returns the ca row with id, or ErrNotFound. certAndCA
// (handler.go, FR-R3) uses this to resolve a cert's recorded signer, which
// may be the previous CA for a stale cert -- GetCurrentCA would silently
// substitute the wrong root.
func (s *Store) GetCAByID(ctx context.Context, id int64) (*CA, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+caColumns+` FROM ca WHERE id = ?`, id)
	c, err := scanCA(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("certmachine: get ca %d: %w", id, err)
	}
	return &c, nil
}

// GetPreviousCA returns the previous CA row, or (nil, nil) when there is
// none -- absence is the common case (most databases have never replaced
// their CA), not an error.
func (s *Store) GetPreviousCA(ctx context.Context) (*CA, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+caColumns+` FROM ca WHERE role = 'previous'`)
	c, err := scanCA(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("certmachine: get previous ca: %w", err)
	}
	return &c, nil
}

// countActiveByCA counts the active rows currently signed by caID -- GET
// /api/ca's "previous.activeCount" (§4 of the CA-replacement plan, US-005),
// the same predicate countActiveByCATx checks in-transaction, run here
// against s.db since the caller is never inside a WithTx (R1).
func (s *Store) countActiveByCA(ctx context.Context, caID int64) (int, error) {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM certs WHERE ca_id = ? AND status = 'active'`, caID).Scan(&n); err != nil {
		return 0, fmt.Errorf("certmachine: count active certs for ca %d: %w", caID, err)
	}
	return n, nil
}

// countActiveUnknownSigner counts active rows with no recorded signer (P1:
// NULL ca_id) -- GET /api/ca's top-level "unknownSignerActiveCount", which
// the Replace dialog uses to warn that the blanket choice also covers these
// rows (they are part of the P3 set alongside the current CA's own active
// rows).
func (s *Store) countActiveUnknownSigner(ctx context.Context) (int, error) {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM certs WHERE ca_id IS NULL AND status = 'active'`).Scan(&n); err != nil {
		return 0, fmt.Errorf("certmachine: count active certs with unknown signer: %w", err)
	}
	return n, nil
}

// InsertCA inserts c as the current CA row inside tx. It refuses with
// ErrCAExists if any ca row already exists -- init and import are still
// only valid against an empty ca table; replacing an existing CA is a later
// story's ReplaceCA, not this method. ca.id is no longer pinned to 1 (schema
// v2 uses AUTOINCREMENT instead of the old id=1 CHECK), so the pre-check and
// the insert no longer name an explicit id.
func (s *Store) InsertCA(ctx context.Context, tx *sql.Tx, c CA) error {
	var exists int
	switch err := tx.QueryRowContext(ctx, `SELECT 1 FROM ca LIMIT 1`).Scan(&exists); {
	case err == nil:
		return ErrCAExists
	case !errors.Is(err, sql.ErrNoRows):
		return fmt.Errorf("certmachine: check existing ca: %w", err)
	}

	_, err := tx.ExecContext(ctx, `
		INSERT INTO ca (role, cert_pem, key_pem, subject, serial, not_before, not_after, fingerprint, imported_from, created)
		VALUES ('current', ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
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

// -- tx-scoped helpers (§3.2 of the CA-replacement plan) --
//
// These, plus InsertCert, ArchiveAllForFQDN and InsertCA above, are the only
// reads and writes a transaction started by WithTx may perform (R1): no
// s.db.* call may ever run while one of these transactions holds the single
// connection (SetMaxOpenConns(1)), or the call deadlocks forever waiting for
// a connection the transaction is still holding.

// errRefuseDropCurrent means dropPreviousCATx re-read a row's role inside
// its own transaction and found it was not "previous" -- the row named by
// id is the current CA, or (in SwitchBack's transient state) a NULL-role
// row. R4 makes this the one and only refusal that keeps the current CA
// permanently non-deletable through every code path in this package.
var errRefuseDropCurrent = errors.New("certmachine: refusing to drop a ca row that is not the previous CA")

// getCurrentCATx is getCurrentCA's tx-scoped counterpart: the same "WHERE
// role = 'current'" lookup, run against tx instead of s.db, for operations
// that must see the row inside the transaction they are about to modify it
// in (R1).
func getCurrentCATx(ctx context.Context, tx *sql.Tx) (*CA, error) {
	row := tx.QueryRowContext(ctx, `SELECT `+caColumns+` FROM ca WHERE role = 'current'`)
	c, err := scanCA(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrCANotFound
		}
		return nil, fmt.Errorf("certmachine: get current ca (tx): %w", err)
	}
	return &c, nil
}

// getPreviousCATx is GetPreviousCA's tx-scoped counterpart: (nil, nil) when
// there is no previous CA, never an error -- absence is the common case.
func getPreviousCATx(ctx context.Context, tx *sql.Tx) (*CA, error) {
	row := tx.QueryRowContext(ctx, `SELECT `+caColumns+` FROM ca WHERE role = 'previous'`)
	c, err := scanCA(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("certmachine: get previous ca (tx): %w", err)
	}
	return &c, nil
}

// getCertTx reads one cert row, with both PEM columns, inside tx. It
// deliberately does not resolve CASubject/Stale (unlike GetCert): every
// caller runs inside a transaction that is about to change the very rows
// staleness depends on, so a value computed mid-transaction would already be
// stale by the time the transaction commits. CAID (the plain column) is
// still populated, since it is not derived from other rows.
func getCertTx(ctx context.Context, tx *sql.Tx, id int64) (*Cert, error) {
	row := tx.QueryRowContext(ctx, `SELECT id, fqdn, serial, not_before, not_after, sans, fingerprint, status, imported_from, quarantine_reason, import_warning, ca_id, created, cert_pem, key_pem FROM certs WHERE id = ?`, id)
	var c Cert
	var sansJSON string
	if err := row.Scan(&c.ID, &c.FQDN, &c.Serial, &c.NotBefore, &c.NotAfter, &sansJSON,
		&c.Fingerprint, &c.Status, &c.ImportedFrom, &c.QuarantineReason, &c.ImportWarning,
		&c.CAID, &c.Created, &c.CertPEM, &c.KeyPEM); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("certmachine: get cert %d (tx): %w", id, err)
	}
	if err := json.Unmarshal([]byte(sansJSON), &c.SANs); err != nil {
		return nil, fmt.Errorf("certmachine: parse sans for cert %d: %w", id, err)
	}
	return &c, nil
}

// countActiveByCATx counts the active rows currently signed by caID -- P2's
// "a CA signs a certificate only through an active row", the test a later
// story's dropPreviousIfUnusedTx runs before retiring a previous CA.
func countActiveByCATx(ctx context.Context, tx *sql.Tx, caID int64) (int, error) {
	var n int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM certs WHERE ca_id = ? AND status = 'active'`, caID).Scan(&n); err != nil {
		return 0, fmt.Errorf("certmachine: count active certs for ca %d: %w", caID, err)
	}
	return n, nil
}

// deleteCertTx permanently removes the cert row with id inside tx.
func deleteCertTx(ctx context.Context, tx *sql.Tx, id int64) error {
	res, err := tx.ExecContext(ctx, `DELETE FROM certs WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("certmachine: delete cert %d (tx): %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("certmachine: delete cert %d (tx): rows affected: %w", id, err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// archiveCertTx archives one cert row by id inside tx -- the single-row
// counterpart to ArchiveAllForFQDN, for operations (Edit, Replace) that must
// archive a specific row rather than every active row for an fqdn.
func archiveCertTx(ctx context.Context, tx *sql.Tx, id int64) error {
	res, err := tx.ExecContext(ctx, `UPDATE certs SET status = 'archived' WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("certmachine: archive cert %d (tx): %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("certmachine: archive cert %d (tx): rows affected: %w", id, err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// insertCATx inserts ca with the given role inside tx and returns the new
// row's id. Unlike InsertCA (init/import-only, always role='current' and
// refusing if any row exists), this is Replace/SwitchBack's insert: it
// performs no existence pre-check, because those operations have already
// made room for the row they are about to insert (demoting or dropping
// whatever occupied that role).
func insertCATx(ctx context.Context, tx *sql.Tx, ca CA, role string) (int64, error) {
	res, err := tx.ExecContext(ctx, `
		INSERT INTO ca (role, cert_pem, key_pem, subject, serial, not_before, not_after, fingerprint, imported_from, created)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		role, ca.CertPEM, ca.KeyPEM, ca.Subject, ca.Serial, ca.NotBefore, ca.NotAfter, ca.Fingerprint, ca.ImportedFrom, ca.Created,
	)
	if err != nil {
		return 0, fmt.Errorf("certmachine: insert ca (role=%s): %w", role, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("certmachine: insert ca (role=%s): last insert id: %w", role, err)
	}
	return id, nil
}

// setCARoleTx sets the role of the ca row named by id -- always by id, never
// by a caller's belief about the current role, so a stale snapshot can never
// retarget the wrong row. role may be nil: SwitchBack's swap needs a
// transient NULL between its role UPDATEs to avoid tripping ca_one_per_role
// mid-swap (a NULL passes that partial index; checkStructuralInvariants is
// what catches a NULL left outside a transaction).
func setCARoleTx(ctx context.Context, tx *sql.Tx, id int64, role *string) error {
	res, err := tx.ExecContext(ctx, `UPDATE ca SET role = ? WHERE id = ?`, role, id)
	if err != nil {
		return fmt.Errorf("certmachine: set ca %d role: %w", id, err)
	}
	// Second guard (architect review, D2): id was read outside this
	// transaction (or earlier in it) by every caller, so a concurrent drop of
	// that exact row between the read and this UPDATE would otherwise commit
	// a silent no-op -- the caller believes it demoted/promoted a row that no
	// longer exists.
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("certmachine: set ca %d role: rows affected: %w", id, err)
	}
	if n != 1 {
		return fmt.Errorf("certmachine: set ca %d role: expected to change exactly 1 row, changed %d: %w", id, n, ErrConcurrentChange)
	}
	return nil
}

// dropPreviousCATx is the only function in this package that deletes a ca
// row (R4). It re-reads id's role inside tx -- never trusting a caller's
// earlier read, which could be stale by the time this runs later in the same
// transaction -- and refuses with errRefuseDropCurrent for anything but
// role='previous'. There is no separate deleteCATx.
func dropPreviousCATx(ctx context.Context, tx *sql.Tx, id int64) error {
	var role sql.NullString
	if err := tx.QueryRowContext(ctx, `SELECT role FROM ca WHERE id = ?`, id).Scan(&role); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("certmachine: drop ca %d: %w", id, ErrNotFound)
		}
		return fmt.Errorf("certmachine: drop ca %d: read role: %w", id, err)
	}
	if !role.Valid || role.String != "previous" {
		return errRefuseDropCurrent
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM ca WHERE id = ?`, id); err != nil {
		return fmt.Errorf("certmachine: drop ca %d: %w", id, err)
	}
	return nil
}

// requireCurrentCATx is the D2 issuance guard (architect review): Generate,
// Renew and Edit each read the current CA and run their leaf's crypto (R2)
// before opening their transaction, then carry that CA's id in the new row's
// CAID. Called just before InsertCert whenever CAID is non-nil, it re-reads
// the current CA inside the same transaction and returns ErrConcurrentChange
// unless id still matches -- otherwise a Replace or SwitchBack landing in
// that window could commit a leaf whose ca_id no longer names the current CA
// (or no longer exists at all), which invariant 3/5 would only catch later.
func requireCurrentCATx(ctx context.Context, tx *sql.Tx, id int64) error {
	current, err := getCurrentCATx(ctx, tx)
	if err != nil {
		if errors.Is(err, ErrCANotFound) {
			return ErrConcurrentChange
		}
		return err
	}
	if current.ID != id {
		return ErrConcurrentChange
	}
	return nil
}

// caExistsTx reports whether a ca row with id still exists inside tx --
// the importer's D2 guard: resolveImportCAID resolves id against a
// pre-transaction read of the current and/or previous CA (storedCARefs), so
// by the time Execute's own transaction runs InsertCert, id must still name
// a ca row (current or previous; either is a valid signer per P1/§3.6's
// FR-R8) or the row would carry a dangling ca_id.
func caExistsTx(ctx context.Context, tx *sql.Tx, id int64) (bool, error) {
	var exists int
	err := tx.QueryRowContext(ctx, `SELECT 1 FROM ca WHERE id = ?`, id).Scan(&exists)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("certmachine: check ca %d exists (tx): %w", id, err)
	}
	return true, nil
}

// setCertCAIDTx sets one cert row's ca_id, nil clearing it back to "unknown
// signer" (P1).
func setCertCAIDTx(ctx context.Context, tx *sql.Tx, certID int64, caID *int64) error {
	if _, err := tx.ExecContext(ctx, `UPDATE certs SET ca_id = ? WHERE id = ?`, caID, certID); err != nil {
		return fmt.Errorf("certmachine: set cert %d ca_id: %w", certID, err)
	}
	return nil
}
