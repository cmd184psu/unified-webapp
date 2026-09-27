// carotate.go holds the CA-replacement plan's invariant checkers (§3.3):
// checkStructuralInvariants (cheap, boot-safe, run by Open) and checkInvariants
// (adds the signature and staleness checks, test-only, per R6); the shared
// dropPreviousIfUnusedTx step Renew, Edit, Delete, ReplaceCA and SwitchBack
// each call inside their own transaction; and the two CA-changing operations
// themselves, ReplaceCA and SwitchBack (§3.4, US-004).
package certmachine

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"reflect"
	"strings"
	"time"
)

// checkStructuralInvariants enforces invariants 1-3 of the plan's integrity
// contract. It runs on s.db, never inside a transaction, and never touches
// certificate content (R6) -- these are cheap SQL checks, safe to run on
// every boot even against a database with expired or oddly-shaped legacy
// rows. Open calls this after migrate and fails loudly if it returns an
// error.
//
//  1. At most one "current" and one "previous" ca row, and no row with a
//     NULL role -- a NULL role only ever exists transiently inside
//     SwitchBack's own transaction, never outside one.
//  2. If any ca row exists, exactly one of them is "current".
//  3. Every non-NULL certs.ca_id resolves to an existing ca row.
func (s *Store) checkStructuralInvariants(ctx context.Context) error {
	var nullRoleCount int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM ca WHERE role IS NULL`).Scan(&nullRoleCount); err != nil {
		return fmt.Errorf("certmachine: check ca null role: %w", err)
	}
	if nullRoleCount > 0 {
		return fmt.Errorf("certmachine: structural invariant 1 violated: %d ca row(s) with no role", nullRoleCount)
	}

	rows, err := s.db.QueryContext(ctx, `SELECT role, COUNT(*) FROM ca GROUP BY role HAVING COUNT(*) > 1`)
	if err != nil {
		return fmt.Errorf("certmachine: check ca role counts: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var role string
		var n int
		if err := rows.Scan(&role, &n); err != nil {
			return fmt.Errorf("certmachine: scan ca role count: %w", err)
		}
		return fmt.Errorf("certmachine: structural invariant 1 violated: %d ca rows with role %q", n, role)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("certmachine: check ca role counts: %w", err)
	}

	var total, current int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM ca`).Scan(&total); err != nil {
		return fmt.Errorf("certmachine: check ca total: %w", err)
	}
	if total > 0 {
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM ca WHERE role = 'current'`).Scan(&current); err != nil {
			return fmt.Errorf("certmachine: check ca current count: %w", err)
		}
		if current != 1 {
			return fmt.Errorf("certmachine: structural invariant 2 violated: %d ca row(s) but %d marked current, want exactly 1", total, current)
		}
	}

	var orphaned int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM certs WHERE ca_id IS NOT NULL AND ca_id NOT IN (SELECT id FROM ca)`).Scan(&orphaned); err != nil {
		return fmt.Errorf("certmachine: check orphaned ca_id: %w", err)
	}
	if orphaned > 0 {
		return fmt.Errorf("certmachine: structural invariant 3 violated: %d cert row(s) reference a nonexistent ca", orphaned)
	}

	return nil
}

// checkInvariants extends checkStructuralInvariants with invariants 4-5,
// which touch certificate content (R6) and so must never run at boot -- an
// expired or oddly-EKU'd legacy leaf is a routine stored state, not a chain
// problem, and Open must not refuse to start over one. Nothing in this
// package calls checkInvariants outside a test; it is the oracle every
// CA-changing test in a later story asserts against.
//
//  4. Every row with a non-NULL ca_id and a parseable cert_pem passes
//     leaf.CheckSignatureFrom(ca) -- CheckSignatureFrom, never Verify (R6),
//     matching the migration backfill's own check.
//  5. For every row ListCerts returns, Stale == (CAID != nil && *CAID !=
//     current.ID).
func (s *Store) checkInvariants(ctx context.Context) error {
	if err := s.checkStructuralInvariants(ctx); err != nil {
		return err
	}
	if err := s.checkSignatureInvariant(ctx); err != nil {
		return err
	}
	if err := s.checkStalenessInvariant(ctx); err != nil {
		return err
	}
	return nil
}

// checkSignatureInvariant is checkInvariants' invariant 4. It reads every
// (leaf, signing ca) pair into a slice before parsing any of it -- there is
// no write here, so R1's "close the cursor before writing" rule does not
// bind, but the read-everything-first shape is kept for consistency with the
// migration backfill this mirrors.
func (s *Store) checkSignatureInvariant(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `
		SELECT c.id, c.cert_pem, ca.cert_pem
		FROM certs c JOIN ca ON c.ca_id = ca.id
		WHERE c.ca_id IS NOT NULL AND c.cert_pem IS NOT NULL`)
	if err != nil {
		return fmt.Errorf("certmachine: check signature invariant: %w", err)
	}
	type signedPair struct {
		id             int64
		leafPEM, caPEM string
	}
	var pending []signedPair
	for rows.Next() {
		var p signedPair
		if err := rows.Scan(&p.id, &p.leafPEM, &p.caPEM); err != nil {
			rows.Close()
			return fmt.Errorf("certmachine: check signature invariant: scan: %w", err)
		}
		pending = append(pending, p)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("certmachine: check signature invariant: %w", err)
	}
	rows.Close()

	for _, p := range pending {
		leaf, err := ParseCert([]byte(p.leafPEM))
		if err != nil {
			// An unparseable stored leaf is not this invariant's concern --
			// there is no signature to check against.
			continue
		}
		caCert, err := ParseCert([]byte(p.caPEM))
		if err != nil {
			return fmt.Errorf("certmachine: invariant 4 violated: cert %d's recorded ca does not parse: %w", p.id, err)
		}
		if err := leaf.CheckSignatureFrom(caCert); err != nil {
			return fmt.Errorf("certmachine: invariant 4 violated: cert %d does not chain to its recorded ca: %w", p.id, err)
		}
	}
	return nil
}

// dropPreviousIfUnusedTx implements P2: "a CA signs a certificate only
// through an active row." It is called at the end of every transaction that
// archives, deletes, or re-signs rows -- Renew, Edit and Delete all call it
// inside their own WithTx (a later story adds Replace and SwitchBack). When
// a previous CA exists and countActiveByCATx finds no active row still
// signed by it, this deletes its archived rows (owner-confirmed 2026-09-27:
// delete, not NULL-out -- D5's "no unending history" would otherwise be
// defeated by an archive-then-insert leaving rows pinning it forever) and
// then calls dropPreviousCATx, which re-checks role='previous' itself (R4).
// It returns whether a drop happened, so a caller can emit the "drop-previous"
// hook only then and surface PreviousDropped.
func dropPreviousIfUnusedTx(ctx context.Context, tx *sql.Tx) (bool, error) {
	previous, err := getPreviousCATx(ctx, tx)
	if err != nil {
		return false, err
	}
	if previous == nil {
		return false, nil
	}
	n, err := countActiveByCATx(ctx, tx, previous.ID)
	if err != nil {
		return false, err
	}
	if n > 0 {
		return false, nil
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM certs WHERE ca_id = ? AND status = 'archived'`, previous.ID); err != nil {
		return false, fmt.Errorf("certmachine: delete archived certs for dropped ca %d: %w", previous.ID, err)
	}
	// D1 (architect review): a quarantined row can carry this ca_id (the
	// importer used to assign one even to a quarantined leaf; a row can also
	// be quarantined after the fact while still pointing at a since-retired
	// CA). Quarantined rows are never deleted or re-issued by this sweep --
	// only archived rows are (P2) -- so their ca_id must be cleared to NULL
	// (P1: "unknown signer") before the ca row itself disappears, or the next
	// boot's structural invariant 3 finds a dangling reference.
	if _, err := tx.ExecContext(ctx, `UPDATE certs SET ca_id = NULL WHERE ca_id = ? AND status = 'quarantined'`, previous.ID); err != nil {
		return false, fmt.Errorf("certmachine: clear quarantined ca_id for dropped ca %d: %w", previous.ID, err)
	}
	if err := dropPreviousCATx(ctx, tx, previous.ID); err != nil {
		return false, err
	}
	return true, nil
}

// dropPreviousIfUnusedAndHookTx wraps dropPreviousIfUnusedTx with the
// "drop-previous" hook every caller (Renew, Edit, Delete, ReplaceCA,
// SwitchBack) fires only when a drop actually happened -- the repeated
// "call, check, hook" shape those five call sites all shared before this was
// factored out.
func (s *Store) dropPreviousIfUnusedAndHookTx(ctx context.Context, tx *sql.Tx) (bool, error) {
	dropped, err := dropPreviousIfUnusedTx(ctx, tx)
	if err != nil {
		return false, err
	}
	if dropped {
		if err := s.runHook("drop-previous"); err != nil {
			return false, err
		}
	}
	return dropped, nil
}

// checkStalenessInvariant is checkInvariants' invariant 5, run against the
// exact same production path (ListCerts) that GET /api/certs uses, so a
// discrepancy here means the API itself would show the wrong badge.
func (s *Store) checkStalenessInvariant(ctx context.Context) error {
	currentID, _, err := s.currentCAID(ctx)
	if err != nil {
		return fmt.Errorf("certmachine: check staleness invariant: current ca: %w", err)
	}
	certs, err := s.ListCerts(ctx)
	if err != nil {
		return fmt.Errorf("certmachine: check staleness invariant: %w", err)
	}
	for _, c := range certs {
		want := c.CAID != nil && *c.CAID != currentID
		if c.Stale != want {
			return fmt.Errorf("certmachine: invariant 5 violated: cert %d stale=%v, want %v", c.ID, c.Stale, want)
		}
	}
	return nil
}

// -- Replace and SwitchBack (§3.4 of the CA-replacement plan, US-004) --

// ReplaceResult is what ReplaceCA returns: what happened to the P3 set (the
// current CA's active rows, plus any active row with an unknown signer) and
// to the outgoing previous CA's own active rows, folded into one set of
// totals -- the response body has no reason to tell an operator which
// sub-population a reissued/deleted row came from. Clamped counts every
// reissued leaf (from either sub-population) whose requested validity
// outlived the new CA and was pulled back to its NotAfter (GenerateLeaf's
// own clamp). PreviousDropped reports whether step 6 (dropPreviousIfUnusedTx)
// dropped the CA that Replace's own step 2 just demoted.
type ReplaceResult struct {
	Reissued        int
	Deleted         int
	Kept            int
	Clamped         int
	PreviousDropped bool
}

// SwitchBackResult is what SwitchBack returns: whether the CA that was
// current before the swap (now demoted back to previous) dropped
// immediately because nothing active still used it (P2). SwitchBack has no
// counts to report -- it moves nothing, it only flips roles.
type SwitchBackResult struct {
	PreviousDropped bool
}

// caRowKey is one cert row's identity for Replace's ErrConcurrentChange
// snapshot (§3.4): its id, status, and signer (0 stands for NULL, "unknown
// signer" per P1), read in id order.
type caRowKey struct {
	id     int64
	status string
	caID   int64
}

// replaceSnapshot is Replace's full concurrency key: the current and
// previous CA ids (0 for "no previous CA"), and the two row sets -- P3 (the
// `existing` blanket choice's target) and prevStale (the `previousStale`
// choice's target) -- whose membership and status must not move between the
// pre-transaction read and the transaction's own first read.
type replaceSnapshot struct {
	currentID  int64
	previousID int64
	p3         []caRowKey
	prevStale  []caRowKey
}

// caRowKeysFromCerts derives the concurrency snapshot's row keys from an
// already-fetched, already id-ordered Cert slice, rather than re-querying:
// activeCertsUnderCurrentOrUnknown and activeCertsForCA (below) are read
// before Replace's transaction opens for the crypto step (R2) anyway, so
// this reuses that same read instead of a second query that could
// theoretically disagree with the first. Returns nil (not an empty slice)
// when certs is empty, matching the tx-scoped row-key queries' own
// nil-when-empty result, so reflect.DeepEqual never sees an "empty slice vs
// nil" false mismatch between the two snapshots.
func caRowKeysFromCerts(certs []Cert) []caRowKey {
	if len(certs) == 0 {
		return nil
	}
	keys := make([]caRowKey, 0, len(certs))
	for _, c := range certs {
		var caID int64
		if c.CAID != nil {
			caID = *c.CAID
		}
		keys = append(keys, caRowKey{id: c.ID, status: c.Status, caID: caID})
	}
	return keys
}

// activeRowKeysTx reads caRowKeys inside tx, in id order -- the transaction's
// own first read of whichever row set where selects, used to rebuild
// replaceSnapshot for the ErrConcurrentChange comparison (R1: this never
// touches s.db).
func activeRowKeysTx(ctx context.Context, tx *sql.Tx, where string, args ...any) ([]caRowKey, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id, status, ca_id FROM certs WHERE `+where+` ORDER BY id`, args...)
	if err != nil {
		return nil, fmt.Errorf("certmachine: read active row keys (tx): %w", err)
	}
	defer rows.Close()
	var keys []caRowKey
	for rows.Next() {
		var k caRowKey
		var caID sql.NullInt64
		if err := rows.Scan(&k.id, &k.status, &caID); err != nil {
			return nil, fmt.Errorf("certmachine: scan row key (tx): %w", err)
		}
		if caID.Valid {
			k.caID = caID.Int64
		}
		keys = append(keys, k)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("certmachine: read active row keys (tx): %w", err)
	}
	return keys, nil
}

// activeRowKeysUnderCurrentOrUnknownTx is the in-transaction counterpart of
// activeCertsUnderCurrentOrUnknown: the P3 set's row keys.
func activeRowKeysUnderCurrentOrUnknownTx(ctx context.Context, tx *sql.Tx, currentID int64) ([]caRowKey, error) {
	return activeRowKeysTx(ctx, tx, `status = 'active' AND (ca_id = ? OR ca_id IS NULL)`, currentID)
}

// activeRowKeysForCATx is the in-transaction counterpart of
// activeCertsForCA: the previousStale set's row keys.
func activeRowKeysForCATx(ctx context.Context, tx *sql.Tx, caID int64) ([]caRowKey, error) {
	return activeRowKeysTx(ctx, tx, `status = 'active' AND ca_id = ?`, caID)
}

// buildReplaceSnapshotTx rebuilds replaceSnapshot from inside Replace's own
// transaction (R1), reading nothing but tx. currentID and previousID are the
// ids read at the very start of the same transaction, immediately before
// this call.
func buildReplaceSnapshotTx(ctx context.Context, tx *sql.Tx, currentID, previousID int64) (replaceSnapshot, error) {
	snap := replaceSnapshot{currentID: currentID, previousID: previousID}
	p3, err := activeRowKeysUnderCurrentOrUnknownTx(ctx, tx, currentID)
	if err != nil {
		return replaceSnapshot{}, err
	}
	snap.p3 = p3
	if previousID != 0 {
		prevStale, err := activeRowKeysForCATx(ctx, tx, previousID)
		if err != nil {
			return replaceSnapshot{}, err
		}
		snap.prevStale = prevStale
	}
	return snap, nil
}

// queryCertsByPredicate reads full (id, fqdn, sans, ca_id, status) rows
// matching where, in id order, via s.db -- always before Replace's
// transaction opens (R1/R2): these are the rows Replace's crypto step needs
// FQDN/SANs from in order to re-issue.
func (s *Store) queryCertsByPredicate(ctx context.Context, where string, args ...any) ([]Cert, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, fqdn, sans, ca_id, status FROM certs WHERE `+where+` ORDER BY id`, args...)
	if err != nil {
		return nil, fmt.Errorf("certmachine: query certs: %w", err)
	}
	defer rows.Close()

	var certs []Cert
	for rows.Next() {
		var c Cert
		var sansJSON string
		var caID sql.NullInt64
		if err := rows.Scan(&c.ID, &c.FQDN, &sansJSON, &caID, &c.Status); err != nil {
			return nil, fmt.Errorf("certmachine: scan cert row: %w", err)
		}
		if err := json.Unmarshal([]byte(sansJSON), &c.SANs); err != nil {
			return nil, fmt.Errorf("certmachine: parse sans for cert %d: %w", c.ID, err)
		}
		if caID.Valid {
			id := caID.Int64
			c.CAID = &id
		}
		certs = append(certs, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("certmachine: query certs: %w", err)
	}
	return certs, nil
}

// activeCertsUnderCurrentOrUnknown returns every active cert row signed by
// currentID or with no recorded signer (P1) -- the P3 set, the `existing`
// blanket choice's target.
func (s *Store) activeCertsUnderCurrentOrUnknown(ctx context.Context, currentID int64) ([]Cert, error) {
	return s.queryCertsByPredicate(ctx, `status = 'active' AND (ca_id = ? OR ca_id IS NULL)`, currentID)
}

// activeCertsForCA returns every active cert row signed by caID exactly
// (never NULL) -- the previousStale set, the `previousStale` choice's
// target.
func (s *Store) activeCertsForCA(ctx context.Context, caID int64) ([]Cert, error) {
	return s.queryCertsByPredicate(ctx, `status = 'active' AND ca_id = ?`, caID)
}

// reissuedLeaf pairs a freshly issued Cert with whether GenerateLeaf clamped
// its validity to the new CA's NotAfter -- ReplaceCA.Clamped's per-leaf
// source.
type reissuedLeaf struct {
	cert    Cert
	clamped bool
}

// issueLeavesUnder generates one fresh leaf under ca for every row in
// sources, entirely before any transaction opens (R2): each leaf copies its
// source row's fqdn and full SAN set, exactly as Renew does for its own
// single-row re-issue. The returned Certs' CAID fields are not to be
// trusted -- issueLeafWith sets them from ca's own (here always zero-value)
// ID; every caller overwrites CAID with insertCATx's real return value
// before inserting.
//
// ctx is checked between keys (never mid-key): a cancellation here means no
// writes have happened yet (the transaction hasn't opened), so returning
// ctx.Err() is always safe. onKey, when non-nil, is called once after each
// leaf is issued -- ReplaceCA's progress-hook seam (§ "real progress bar").
func issueLeavesUnder(ctx context.Context, ca *CA, sources []Cert, defaultValidityDays, expiryWarnDays int, onKey func()) ([]reissuedLeaf, error) {
	leaves := make([]reissuedLeaf, 0, len(sources))
	for _, src := range sources {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		ips, err := parseStoredIPs(src.ID, src.SANs.IP)
		if err != nil {
			return nil, err
		}
		req := CertRequest{FQDN: src.FQDN, DNSSans: src.SANs.DNS, IPSans: ips}
		c, leaf, err := issueLeafWith(ca, src.FQDN, req, defaultValidityDays, expiryWarnDays)
		if err != nil {
			return nil, err
		}
		leaves = append(leaves, reissuedLeaf{cert: c, clamped: leaf.Clamped})
		if onKey != nil {
			onKey()
		}
	}
	return leaves, nil
}

// ReplacePhase names one stage of ReplaceCA's progress hook (the owner
// feedback behind the CA-replacement panel's progress bar: a blanket
// re-issue of dozens of certs runs real RSA-4096/2048 keygen before its
// single short transaction, R2, and previously gave no sign anything was
// happening).
type ReplacePhase string

const (
	// ReplacePhaseCAKey fires once, before the new CA's RSA-4096 key is
	// generated.
	ReplacePhaseCAKey ReplacePhase = "ca-key"
	// ReplacePhaseLeafKeys fires once after each re-issued leaf's RSA-2048
	// key is generated. Done is 1-based and cumulative across both re-issued
	// sets (the P3 `existing` set, then the `previousStale` set); Total is
	// the sum of every leaf that will be re-issued in this call.
	ReplacePhaseLeafKeys ReplacePhase = "leaf-keys"
	// ReplacePhaseSaving fires once, immediately before the transaction
	// opens.
	ReplacePhaseSaving ReplacePhase = "saving"
)

// ReplaceProgress is one event ReplaceCA's progress hook receives. Done and
// Total are meaningful only for ReplacePhaseLeafKeys; they are zero
// otherwise.
type ReplaceProgress struct {
	Phase ReplacePhase
	Done  int
	Total int
}

// replaceOptions is ReplaceCA's optional-parameter bag, set via
// ReplaceOption. Zero value: no progress hook, identical to ReplaceCA's
// behavior before this feature existed.
type replaceOptions struct {
	progress func(ReplaceProgress)
}

// ReplaceOption configures one optional aspect of a ReplaceCA call. This is
// the least invasive shape for adding progress reporting: every existing
// caller and test keeps compiling and behaving unchanged, since the new
// parameter is variadic.
type ReplaceOption func(*replaceOptions)

// WithReplaceProgress makes ReplaceCA call fn once for each progress event
// (§ "real progress bar"): ReplacePhaseCAKey before the new CA's key is
// generated, ReplacePhaseLeafKeys after each re-issued leaf's key, and
// ReplacePhaseSaving just before the transaction opens. fn is never called
// before validation has passed -- a validation or P4 error is still
// returned with no event ever fired.
func WithReplaceProgress(fn func(ReplaceProgress)) ReplaceOption {
	return func(o *replaceOptions) { o.progress = fn }
}

// validReplaceExisting and validPreviousStale are ReplaceCA's two request
// enums (P4/§3.4). previousStale has no "keep" value -- P4 pins that a
// caller sending "keep" for previousStale is a 400, distinct from the
// `existing` enum's own "keep".
func validReplaceExisting(v string) bool {
	return v == "reissue" || v == "delete" || v == "keep"
}

func validPreviousStaleChoice(v string) bool {
	return v == "reissue" || v == "delete"
}

// ReplaceCA installs a new certificate authority in the current role (FR-R2,
// D1/D2/D5/D7): name becomes the new CA's Common Name (NormalizeCAName);
// existing says what happens to the outgoing current CA's own active rows
// plus any active row with no recorded signer (P3); previousStale -- required
// exactly when the *outgoing previous* CA (if one exists) still signs an
// active row (P4) -- says what happens to that row set. All crypto (the new
// CA, and any leaf re-issued by either choice) runs before the transaction
// opens (R2); the transaction re-reads its own concurrency snapshot as its
// first act and writes nothing at all if it has moved (ErrConcurrentChange),
// otherwise it performs, in exactly this order: retire the outgoing previous
// CA (if any), demote the outgoing current CA to previous, promote the new
// CA to current, insert the previousStale re-issues, apply `existing` to the
// P3 set, then drop the just-demoted CA if it now signs nothing active (P2).
func (s *Store) ReplaceCA(ctx context.Context, name, existing string, previousStale *string, defaultValidityDays, expiryWarnDays int, opts ...ReplaceOption) (ReplaceResult, error) {
	var opt replaceOptions
	for _, o := range opts {
		o(&opt)
	}

	// 1. Validation, outside the transaction.
	current, err := s.GetCurrentCA(ctx)
	if err != nil {
		return ReplaceResult{}, err
	}
	normalizedName, err := NormalizeCAName(name)
	if err != nil {
		return ReplaceResult{}, err
	}
	if !validReplaceExisting(existing) {
		return ReplaceResult{}, fmt.Errorf("%w: existing must be one of reissue, delete, keep, got %q", ErrValidation, existing)
	}
	if previousStale != nil && !validPreviousStaleChoice(*previousStale) {
		return ReplaceResult{}, fmt.Errorf("%w: previousStale must be reissue or delete, got %q", ErrValidation, *previousStale)
	}

	previous, err := s.GetPreviousCA(ctx)
	if err != nil {
		return ReplaceResult{}, err
	}

	// P6: the new CA's file stem must differ, case-insensitively, from both
	// the current and the previous CA's stems.
	newStem := CAFileStem(normalizedName)
	if strings.EqualFold(newStem, CAFileStem(current.Subject)) {
		return ReplaceResult{}, fmt.Errorf("%w: the new CA name %q collides with the current CA's file name", ErrValidation, normalizedName)
	}
	if previous != nil && strings.EqualFold(newStem, CAFileStem(previous.Subject)) {
		return ReplaceResult{}, fmt.Errorf("%w: the new CA name %q collides with the previous CA's file name", ErrValidation, normalizedName)
	}

	var previousID int64
	var oldPreviousActive []Cert
	if previous != nil {
		previousID = previous.ID
		oldPreviousActive, err = s.activeCertsForCA(ctx, previousID)
		if err != nil {
			return ReplaceResult{}, err
		}
		if len(oldPreviousActive) > 0 && previousStale == nil {
			return ReplaceResult{}, ErrPreviousStaleChoiceRequired
		}
	}

	p3Certs, err := s.activeCertsUnderCurrentOrUnknown(ctx, current.ID)
	if err != nil {
		return ReplaceResult{}, err
	}

	preSnap := replaceSnapshot{
		currentID:  current.ID,
		previousID: previousID,
		p3:         caRowKeysFromCerts(p3Certs),
		prevStale:  caRowKeysFromCerts(oldPreviousActive),
	}

	// 2. Crypto, outside the transaction (R2). Validation has fully passed
	// by this point, so the progress hook (if any) may now fire -- never
	// before (a validation/P4 error above is still returned with no event).
	if opt.progress != nil {
		opt.progress(ReplaceProgress{Phase: ReplacePhaseCAKey})
	}
	if err := ctx.Err(); err != nil {
		return ReplaceResult{}, err
	}
	certPEM, keyPEM, err := s.genCA(normalizedName)
	if err != nil {
		return ReplaceResult{}, err
	}
	newCACert, err := ParseCert(certPEM)
	if err != nil {
		return ReplaceResult{}, fmt.Errorf("certmachine: parse replacement ca cert: %w", err)
	}
	newCA := CA{
		CertPEM:     string(certPEM),
		KeyPEM:      string(keyPEM),
		Subject:     newCACert.Subject.CommonName,
		Serial:      SerialString(newCACert.SerialNumber),
		NotBefore:   newCACert.NotBefore.UTC().Format(time.RFC3339),
		NotAfter:    newCACert.NotAfter.UTC().Format(time.RFC3339),
		Fingerprint: Fingerprint(newCACert.Raw),
		Created:     time.Now().UTC().Format(time.RFC3339),
	}

	// leafKeyTotal is every leaf that will be re-issued in this call -- the
	// P3 `existing` set plus the `previousStale` set -- computed before
	// either loop runs so ReplacePhaseLeafKeys events carry the right Total
	// from the very first one.
	var leafKeyTotal int
	if existing == "reissue" {
		leafKeyTotal += len(p3Certs)
	}
	if previousStale != nil && *previousStale == "reissue" {
		leafKeyTotal += len(oldPreviousActive)
	}
	var leafKeyDone int
	onLeafKey := func() {
		if opt.progress == nil {
			return
		}
		leafKeyDone++
		opt.progress(ReplaceProgress{Phase: ReplacePhaseLeafKeys, Done: leafKeyDone, Total: leafKeyTotal})
	}

	var p3Leaves, prevStaleLeaves []reissuedLeaf
	if existing == "reissue" {
		p3Leaves, err = issueLeavesUnder(ctx, &newCA, p3Certs, defaultValidityDays, expiryWarnDays, onLeafKey)
		if err != nil {
			return ReplaceResult{}, err
		}
	}
	if previousStale != nil && *previousStale == "reissue" {
		prevStaleLeaves, err = issueLeavesUnder(ctx, &newCA, oldPreviousActive, defaultValidityDays, expiryWarnDays, onLeafKey)
		if err != nil {
			return ReplaceResult{}, err
		}
	}

	result := ReplaceResult{}
	switch existing {
	case "reissue":
		result.Reissued += len(p3Certs)
		for _, l := range p3Leaves {
			if l.clamped {
				result.Clamped++
			}
		}
	case "delete":
		result.Deleted += len(p3Certs)
	case "keep":
		result.Kept = len(p3Certs)
	}
	if previousStale != nil {
		switch *previousStale {
		case "reissue":
			result.Reissued += len(oldPreviousActive)
			for _, l := range prevStaleLeaves {
				if l.clamped {
					result.Clamped++
				}
			}
		case "delete":
			result.Deleted += len(oldPreviousActive)
		}
	}

	// 3. WithTx: re-read the concurrency snapshot first, write nothing if it
	// moved, otherwise run steps 1-6 in exactly this order.
	if opt.progress != nil {
		opt.progress(ReplaceProgress{Phase: ReplacePhaseSaving})
	}
	if err := ctx.Err(); err != nil {
		return ReplaceResult{}, err
	}
	if err := s.WithTx(ctx, func(tx *sql.Tx) error {
		txSnap, err := buildReplaceSnapshotTx(ctx, tx, current.ID, previousID)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(preSnap, txSnap) {
			return ErrConcurrentChange
		}

		// Step 1: retire the outgoing previous CA, if one exists.
		if previousID != 0 {
			if previousStale != nil {
				switch *previousStale {
				case "delete":
					for i, row := range oldPreviousActive {
						if err := deleteCertTx(ctx, tx, row.ID); err != nil {
							return err
						}
						if err := s.runHook(fmt.Sprintf("prev-delete:%d", i)); err != nil {
							return err
						}
					}
				case "reissue":
					for i, row := range oldPreviousActive {
						if err := archiveCertTx(ctx, tx, row.ID); err != nil {
							return err
						}
						if err := s.runHook(fmt.Sprintf("prev-archive:%d", i)); err != nil {
							return err
						}
					}
				}
			}
			// P2: delete its archived rows (including whatever this step
			// just archived above) before the CA row itself goes away.
			if _, err := tx.ExecContext(ctx, `DELETE FROM certs WHERE ca_id = ? AND status = 'archived'`, previousID); err != nil {
				return fmt.Errorf("certmachine: replace: delete archived certs under retired previous ca %d: %w", previousID, err)
			}
			// D1 (architect review): clear ca_id on any quarantined row still
			// pointing at the CA about to be dropped, same as
			// dropPreviousIfUnusedTx -- quarantined rows are never part of P2's
			// archived-row sweep, so a dangling ca_id would otherwise survive
			// into the next boot's structural invariant 3 check.
			if _, err := tx.ExecContext(ctx, `UPDATE certs SET ca_id = NULL WHERE ca_id = ? AND status = 'quarantined'`, previousID); err != nil {
				return fmt.Errorf("certmachine: replace: clear quarantined ca_id under retired previous ca %d: %w", previousID, err)
			}
			if err := dropPreviousCATx(ctx, tx, previousID); err != nil {
				return err
			}
			if err := s.runHook("retire-previous"); err != nil {
				return err
			}
		}

		// Step 2: demote the outgoing current CA to previous.
		demoted := "previous"
		if err := setCARoleTx(ctx, tx, current.ID, &demoted); err != nil {
			return err
		}
		if err := s.runHook("demote"); err != nil {
			return err
		}

		// Step 3: promote the new CA to current.
		newID, err := insertCATx(ctx, tx, newCA, "current")
		if err != nil {
			return err
		}
		if err := s.runHook("insert-ca"); err != nil {
			return err
		}

		// Step 4: insert the leaves re-issued under previousStale, ca_id
		// always the id insertCATx just returned, never a newCA field.
		if previousStale != nil && *previousStale == "reissue" {
			for i, l := range prevStaleLeaves {
				c := l.cert
				c.CAID = &newID
				if _, err := s.InsertCert(ctx, tx, c); err != nil {
					return err
				}
				if err := s.runHook(fmt.Sprintf("prev-reissue:%d", i)); err != nil {
					return err
				}
			}
		}

		// Step 5: apply `existing` to the P3 set.
		switch existing {
		case "reissue":
			for i, row := range p3Certs {
				if err := s.ArchiveAllForFQDN(ctx, tx, row.FQDN); err != nil {
					return err
				}
				c := p3Leaves[i].cert
				c.CAID = &newID
				if _, err := s.InsertCert(ctx, tx, c); err != nil {
					return err
				}
				if err := s.runHook(fmt.Sprintf("reissue:%d", i)); err != nil {
					return err
				}
			}
		case "delete":
			for i, row := range p3Certs {
				if err := deleteCertTx(ctx, tx, row.ID); err != nil {
					return err
				}
				if err := s.runHook(fmt.Sprintf("delete:%d", i)); err != nil {
					return err
				}
			}
		}

		// Step 6: drop the just-demoted CA if it now signs nothing active.
		dropped, err := s.dropPreviousIfUnusedAndHookTx(ctx, tx)
		if err != nil {
			return err
		}
		result.PreviousDropped = dropped
		return nil
	}); err != nil {
		return ReplaceResult{}, err
	}

	// 4. Audit log (P9).
	log.Printf("certmachine: ca replace old=%s new=%s reissued=%d deleted=%d kept=%d clamped=%d previous_dropped=%v",
		current.Fingerprint, newCA.Fingerprint, result.Reissued, result.Deleted, result.Kept, result.Clamped, result.PreviousDropped)

	return result, nil
}

// verifySwitchBackSnapshotTx is SwitchBack's D2 guard (architect review): it
// re-reads getCurrentCATx and getPreviousCATx as the very first statements
// inside the transaction and compares them against currentID/previousID --
// read from s.db before the transaction opened -- returning
// ErrConcurrentChange and writing nothing if either has moved (the previous
// CA disappearing entirely counts as moved). Without this, a Replace or
// another SwitchBack landing between SwitchBack's own pre-transaction reads
// and this transaction's role UPDATEs could demote/promote the wrong rows by
// id, since setCARoleTx trusts whatever id it is given.
func verifySwitchBackSnapshotTx(ctx context.Context, tx *sql.Tx, currentID, previousID int64) error {
	txCurrent, err := getCurrentCATx(ctx, tx)
	if err != nil {
		return err
	}
	txPrevious, err := getPreviousCATx(ctx, tx)
	if err != nil {
		return err
	}
	if txCurrent.ID != currentID || txPrevious == nil || txPrevious.ID != previousID {
		return ErrConcurrentChange
	}
	return nil
}

// SwitchBack promotes the previous CA back to current (FR-R9, D9): no
// previous CA -> ErrNoPreviousCA; the previous CA failing checkCAExpiry ->
// a 409-class error (P7), since switching back to a CA that could not issue
// anyway defeats the point. The swap uses a transient NULL role between the
// two live-role UPDATEs (§3.4): SQLite checks a UNIQUE constraint per row,
// not at statement end, so writing 'current' onto the previous row while the
// old current row still holds 'current' would trip ca_one_per_role -- the
// NULL step vacates the role first. dropPreviousIfUnusedTx then runs exactly
// as it does for Replace, dropping the swapped-out CA immediately if it now
// signs nothing active.
func (s *Store) SwitchBack(ctx context.Context, expiryWarnDays int) (SwitchBackResult, error) {
	current, err := s.GetCurrentCA(ctx)
	if err != nil {
		return SwitchBackResult{}, err
	}
	previous, err := s.GetPreviousCA(ctx)
	if err != nil {
		return SwitchBackResult{}, err
	}
	if previous == nil {
		return SwitchBackResult{}, ErrNoPreviousCA
	}
	previousCert, err := ParseCert([]byte(previous.CertPEM))
	if err != nil {
		return SwitchBackResult{}, fmt.Errorf("certmachine: parse previous ca cert: %w", err)
	}
	if err := checkCAExpiry(previousCert, expiryWarnDays); err != nil {
		return SwitchBackResult{}, err
	}

	currentID, previousID := current.ID, previous.ID

	var dropped bool
	if err := s.WithTx(ctx, func(tx *sql.Tx) error {
		if err := verifySwitchBackSnapshotTx(ctx, tx, currentID, previousID); err != nil {
			return err
		}

		if err := setCARoleTx(ctx, tx, currentID, nil); err != nil {
			return err
		}
		if err := s.runHook("swap-null"); err != nil {
			return err
		}

		promoted := "current"
		if err := setCARoleTx(ctx, tx, previousID, &promoted); err != nil {
			return err
		}
		if err := s.runHook("swap-promote"); err != nil {
			return err
		}

		demoted := "previous"
		if err := setCARoleTx(ctx, tx, currentID, &demoted); err != nil {
			return err
		}
		if err := s.runHook("swap-demote"); err != nil {
			return err
		}

		d, err := s.dropPreviousIfUnusedAndHookTx(ctx, tx)
		if err != nil {
			return err
		}
		dropped = d
		return nil
	}); err != nil {
		return SwitchBackResult{}, err
	}

	log.Printf("certmachine: ca switch-back old=%s new=%s previous_dropped=%v", current.Fingerprint, previous.Fingerprint, dropped)

	return SwitchBackResult{PreviousDropped: dropped}, nil
}
