package certmachine

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"math/big"
	mrand "math/rand"
	"net/http"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// TestDropPreviousCATx_RefusesCurrent is R4's unit test: dropPreviousCATx
// must refuse to delete a row whose role is not "previous" -- even when
// asked to drop the current CA's own id -- and it must delete nothing when
// it refuses (Principle 2's "the current CA is never deleted through any
// code path").
func TestDropPreviousCATx_RefusesCurrent(t *testing.T) {
	s := openTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	setupCA(t, s, time.Now().AddDate(5, 0, 0))

	current, err := s.GetCurrentCA(ctx)
	if err != nil {
		t.Fatalf("GetCurrentCA: %v", err)
	}

	err = s.WithTx(ctx, func(tx *sql.Tx) error {
		return dropPreviousCATx(ctx, tx, current.ID)
	})
	if !errors.Is(err, errRefuseDropCurrent) {
		t.Fatalf("dropPreviousCATx(current) error = %v, want errRefuseDropCurrent", err)
	}

	still, err := s.GetCurrentCA(ctx)
	if err != nil {
		t.Fatalf("GetCurrentCA after refused drop: %v", err)
	}
	if still.ID != current.ID {
		t.Fatalf("current ca id changed across a refused drop: got %d, want %d", still.ID, current.ID)
	}
}

// TestDropPreviousCATx_DropsPrevious is dropPreviousCATx's positive case:
// a row whose role really is "previous" is deleted.
func TestDropPreviousCATx_DropsPrevious(t *testing.T) {
	s := openTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	setupCA(t, s, time.Now().AddDate(5, 0, 0))

	_, _, previousCertPEM := newThrowawayCA(t, time.Now().AddDate(5, 0, 0))
	previous := fixtureCA("previous-ca")
	previous.CertPEM = string(previousCertPEM)

	var previousID int64
	if err := s.WithTx(ctx, func(tx *sql.Tx) error {
		var err error
		previousID, err = insertCATx(ctx, tx, previous, "previous")
		return err
	}); err != nil {
		t.Fatalf("insertCATx(previous): %v", err)
	}

	if err := s.WithTx(ctx, func(tx *sql.Tx) error {
		return dropPreviousCATx(ctx, tx, previousID)
	}); err != nil {
		t.Fatalf("dropPreviousCATx(previous): %v", err)
	}

	if got, err := s.GetPreviousCA(ctx); err != nil || got != nil {
		t.Fatalf("GetPreviousCA after drop = (%+v, %v), want (nil, nil)", got, err)
	}
}

// TestSetCARoleTx_RefusesWhenRowMissing is setCARoleTx's second guard
// (architect review, D2): it must check RowsAffected and refuse unless
// exactly one row changed, rather than silently committing a no-op UPDATE
// against an id a concurrent drop already removed.
func TestSetCARoleTx_RefusesWhenRowMissing(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	setupCA(t, s, time.Now().AddDate(5, 0, 0))

	current, err := s.GetCurrentCA(ctx)
	if err != nil {
		t.Fatalf("GetCurrentCA: %v", err)
	}

	// The positive case: addressing the real row by id succeeds.
	demoted := "previous"
	if err := s.WithTx(ctx, func(tx *sql.Tx) error {
		return setCARoleTx(ctx, tx, current.ID, &demoted)
	}); err != nil {
		t.Fatalf("setCARoleTx(existing row) = %v, want nil", err)
	}

	// The negative case: an id that names no row at all.
	promoted := "current"
	err = s.WithTx(ctx, func(tx *sql.Tx) error {
		return setCARoleTx(ctx, tx, current.ID+1000, &promoted)
	})
	if !errors.Is(err, ErrConcurrentChange) {
		t.Fatalf("setCARoleTx(missing row) = %v, want ErrConcurrentChange", err)
	}
}

// TestRequireCurrentCATx_RefusesStaleID is the D2 issuance guard's unit test
// (architect review): Generate, Renew and Edit each call this just before
// InsertCert whenever the new row's CAID is non-nil. Exercised directly with
// a deliberately stale id, per the architect's own suggestion, rather than
// through a real race.
func TestRequireCurrentCATx_RefusesStaleID(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	setupCA(t, s, time.Now().AddDate(5, 0, 0))

	current, err := s.GetCurrentCA(ctx)
	if err != nil {
		t.Fatalf("GetCurrentCA: %v", err)
	}

	if err := s.WithTx(ctx, func(tx *sql.Tx) error {
		return requireCurrentCATx(ctx, tx, current.ID)
	}); err != nil {
		t.Fatalf("requireCurrentCATx(current id) = %v, want nil", err)
	}

	if err := s.WithTx(ctx, func(tx *sql.Tx) error {
		return requireCurrentCATx(ctx, tx, current.ID+1000)
	}); !errors.Is(err, ErrConcurrentChange) {
		t.Fatalf("requireCurrentCATx(stale id) = %v, want ErrConcurrentChange", err)
	}
}

// TestCAExistsTx is the importer's D2 guard's unit test (architect review):
// Execute calls this just before InsertCert whenever a resolved leaf's CAID
// is non-nil, so it must accept the current CA's id and refuse an id that
// names no ca row at all (e.g. one dropped between storedCARefs' read and
// Execute's own transaction).
func TestCAExistsTx(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	setupCA(t, s, time.Now().AddDate(5, 0, 0))

	current, err := s.GetCurrentCA(ctx)
	if err != nil {
		t.Fatalf("GetCurrentCA: %v", err)
	}

	if err := s.WithTx(ctx, func(tx *sql.Tx) error {
		exists, err := caExistsTx(ctx, tx, current.ID)
		if err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("caExistsTx(current id) = false, want true")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	if err := s.WithTx(ctx, func(tx *sql.Tx) error {
		exists, err := caExistsTx(ctx, tx, current.ID+1000)
		if err != nil {
			return err
		}
		if exists {
			return fmt.Errorf("caExistsTx(nonexistent id) = true, want false")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// TestCheckInvariants_HealthyStore exercises checkInvariants' invariants 4-5
// against a normal store: one current CA and one active leaf genuinely
// signed by it, inserted with its ca_id set (item 3 of the CA-replacement
// plan's US-002 slice) -- the case Generate/Renew will produce once a later
// story wires ca_id into their own inserts.
func TestCheckInvariants_HealthyStore(t *testing.T) {
	s := openTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	caCert, caKey, caCertPEM := newThrowawayCA(t, time.Now().AddDate(5, 0, 0))
	ca := CA{
		CertPEM:     string(caCertPEM),
		KeyPEM:      string(encodeKeyPEM(caKey)),
		Subject:     caCert.Subject.CommonName,
		Serial:      SerialString(caCert.SerialNumber),
		NotBefore:   caCert.NotBefore.UTC().Format(time.RFC3339),
		NotAfter:    caCert.NotAfter.UTC().Format(time.RFC3339),
		Fingerprint: Fingerprint(caCert.Raw),
		Created:     time.Now().UTC().Format(time.RFC3339),
	}
	var caID int64
	if err := s.WithTx(ctx, func(tx *sql.Tx) error {
		var err error
		caID, err = insertCATx(ctx, tx, ca, "current")
		return err
	}); err != nil {
		t.Fatalf("insertCATx: %v", err)
	}

	leaf, err := GenerateLeaf(caCert, caKey, CertRequest{FQDN: "healthy.example.local"}, 365)
	if err != nil {
		t.Fatalf("GenerateLeaf: %v", err)
	}
	leafCert, err := ParseCert(leaf.CertPEM)
	if err != nil {
		t.Fatalf("ParseCert(leaf): %v", err)
	}
	c := certFromLeaf("healthy.example.local", leaf, leafCert)
	c.CAID = &caID

	if _, err := insertCert(s, c); err != nil {
		t.Fatalf("insert leaf: %v", err)
	}

	if err := s.checkInvariants(ctx); err != nil {
		t.Fatalf("checkInvariants: %v", err)
	}
}

// insertPreviousCA inserts a "previous"-role ca row (a placeholder fixture,
// like fixtureCA -- its cert_pem is deliberately unparseable, so
// checkInvariants' signature invariant skips every row that references it,
// matching the leaf fixtures below) and returns its id.
func insertPreviousCA(t *testing.T, s *Store, subject string) int64 {
	t.Helper()
	ctx := context.Background()
	var id int64
	if err := s.WithTx(ctx, func(tx *sql.Tx) error {
		var err error
		id, err = insertCATx(ctx, tx, fixtureCA(subject), "previous")
		return err
	}); err != nil {
		t.Fatalf("insertCATx(previous %s): %v", subject, err)
	}
	return id
}

// TestAutoDrop_ArchivedRowsDeleted is P2's test: a previous CA that signs
// exactly one active row drops -- deleting its archived rows in the same
// transaction (owner-confirmed 2026-09-27: delete, not NULL-out) -- as soon
// as that active row stops being signed by it. Renew is the trigger here;
// Edit and Delete share the same dropPreviousIfUnusedTx call.
func TestAutoDrop_ArchivedRowsDeleted(t *testing.T) {
	s := openTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	setupCA(t, s, time.Now().AddDate(5, 0, 0))

	previousID := insertPreviousCA(t, s, "previous-autodrop")

	active := fixtureCert("autodrop-active.example.local", StatusActive, "2030-01-01T00:00:00Z")
	active.CAID = &previousID
	activeID := mustInsertCert(t, s, active)

	archived := fixtureCert("autodrop-archived.example.local", StatusArchived, "2029-01-01T00:00:00Z")
	archived.CAID = &previousID
	archivedID := mustInsertCert(t, s, archived)

	result, err := s.Renew(ctx, activeID, 365, 30)
	if err != nil {
		t.Fatalf("Renew: %v", err)
	}
	if !result.PreviousDropped {
		t.Fatal("result.PreviousDropped = false, want true (the previous CA's only active row was just re-issued under the current CA)")
	}

	if got, err := s.GetPreviousCA(ctx); err != nil || got != nil {
		t.Fatalf("GetPreviousCA after drop = (%+v, %v), want (nil, nil)", got, err)
	}
	if _, err := s.GetCert(ctx, archivedID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetCert(archived row under the dropped ca) error = %v, want ErrNotFound (P2: its archived rows are deleted with it)", err)
	}

	if err := s.checkInvariants(ctx); err != nil {
		t.Fatalf("checkInvariants: %v", err)
	}
}

// TestAutoDrop_NotTriggeredWhileStillUsed is the negative case: a previous
// CA with more than one active row must survive a Renew of only one of
// them, and its archived rows must survive with it.
func TestAutoDrop_NotTriggeredWhileStillUsed(t *testing.T) {
	s := openTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	setupCA(t, s, time.Now().AddDate(5, 0, 0))

	previousID := insertPreviousCA(t, s, "previous-keep")

	firstActive := fixtureCert("autodrop-keep-1.example.local", StatusActive, "2030-01-01T00:00:00Z")
	firstActive.CAID = &previousID
	firstID := mustInsertCert(t, s, firstActive)

	secondActive := fixtureCert("autodrop-keep-2.example.local", StatusActive, "2030-01-01T00:00:00Z")
	secondActive.CAID = &previousID
	mustInsertCert(t, s, secondActive)

	archived := fixtureCert("autodrop-keep-archived.example.local", StatusArchived, "2029-01-01T00:00:00Z")
	archived.CAID = &previousID
	archivedID := mustInsertCert(t, s, archived)

	result, err := s.Renew(ctx, firstID, 365, 30)
	if err != nil {
		t.Fatalf("Renew: %v", err)
	}
	if result.PreviousDropped {
		t.Fatal("result.PreviousDropped = true, want false (the previous CA still signs one active row)")
	}

	got, err := s.GetPreviousCA(ctx)
	if err != nil {
		t.Fatalf("GetPreviousCA: %v", err)
	}
	if got == nil || got.ID != previousID {
		t.Fatalf("GetPreviousCA after non-triggering renew = %+v, want the still-present previous CA %d", got, previousID)
	}
	if _, err := s.GetCert(ctx, archivedID); err != nil {
		t.Fatalf("GetCert(archived row under the still-used ca) = %v, want it still present", err)
	}

	if err := s.checkInvariants(ctx); err != nil {
		t.Fatalf("checkInvariants: %v", err)
	}
}

// TestAutoDrop_ClearsQuarantinedCAID is D1's regression test (architect
// review): a previous CA that also has a quarantined row pointing at it
// (inserted directly, reproducing the importer bug fixed alongside this)
// must still drop cleanly when its last active row stops using it --
// dropPreviousIfUnusedTx now clears that quarantined row's ca_id to NULL
// before deleting the ca row, so structural invariant 3 still holds after a
// close/reopen.
func TestAutoDrop_ClearsQuarantinedCAID(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "certmachine.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	setupCA(t, s, time.Now().AddDate(5, 0, 0))

	previousID := insertPreviousCA(t, s, "previous-quarantine-drop")

	active := fixtureCert("quarantine-drop-active.example.local", StatusActive, "2030-01-01T00:00:00Z")
	active.CAID = &previousID
	activeID := mustInsertCert(t, s, active)

	quarantined := fixtureCert("quarantine-drop-quarantined.example.local", StatusQuarantined, "")
	quarantined.CAID = &previousID
	quarantined.QuarantineReason = strPtr("test fixture: reproduces the D1 dangling ca_id")
	quarantinedID := mustInsertCert(t, s, quarantined)

	result, err := s.Renew(ctx, activeID, 365, 30)
	if err != nil {
		t.Fatalf("Renew: %v", err)
	}
	if !result.PreviousDropped {
		t.Fatal("Renew's PreviousDropped = false, want true (the previous CA's only active row was just re-issued under the current CA)")
	}
	if got, err := s.GetPreviousCA(ctx); err != nil || got != nil {
		t.Fatalf("GetPreviousCA after drop = (%+v, %v), want (nil, nil)", got, err)
	}

	quarantinedRow, err := s.GetCert(ctx, quarantinedID)
	if err != nil {
		t.Fatalf("GetCert(quarantined row) after drop: %v", err)
	}
	if quarantinedRow.CAID != nil {
		t.Fatalf("quarantined row's caId = %d after its ca dropped, want nil", *quarantinedRow.CAID)
	}

	if err := s.checkInvariants(ctx); err != nil {
		t.Fatalf("checkInvariants: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer reopened.Close()
	if err := reopened.checkInvariants(ctx); err != nil {
		t.Fatalf("checkInvariants after reopen: %v", err)
	}
}

// -- US-004 test helpers (ReplaceCA / SwitchBack) --

// throwawayGenCA is a 2048-bit stand-in for GenerateNamedCA (Store.genCA,
// R7's seam), used by every Replace/SwitchBack test below so a 4096-bit
// keygen never runs inside a test loop or the randomized sequence.
func throwawayGenCA(name string) (certPEM, keyPEM []byte, err error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, err
	}
	serial, err := randomSerial()
	if err != nil {
		return nil, nil, err
	}
	ski, err := subjectKeyID(&key.PublicKey)
	if err != nil {
		return nil, nil, err
	}
	now := time.Now().UTC()
	tpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: name},
		NotBefore:             now,
		NotAfter:              now.AddDate(10, 0, 0),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		SubjectKeyId:          ski,
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		return nil, nil, err
	}
	return encodeCertPEM(der), encodeKeyPEM(key), nil
}

// namedThrowawayCA is newThrowawayCA with an explicit Common Name -- used
// where a test needs to distinguish CAs by name (P6 collisions) rather than
// accepting newThrowawayCA's fixed "Throwaway Test CA" subject.
func namedThrowawayCA(t *testing.T, name string, notAfter time.Time) (*x509.Certificate, *rsa.PrivateKey, []byte) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate named throwaway ca key: %v", err)
	}
	tpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: name},
		NotBefore:             time.Now().UTC().Add(-time.Hour),
		NotAfter:              notAfter,
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create named throwaway ca cert: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse named throwaway ca cert: %v", err)
	}
	return cert, key, encodeCertPEM(der)
}

// insertRealPreviousCANamed inserts a real, parseable "previous"-role ca row
// (unlike insertPreviousCA's fixtureCA, whose cert_pem never parses), so a
// test can sign real leaves under it and exercise checkInvariants'
// signature check (invariant 4) against it.
func insertRealPreviousCANamed(t *testing.T, s *Store, name string, notAfter time.Time) (int64, *x509.Certificate, *rsa.PrivateKey) {
	t.Helper()
	caCert, caKey, caCertPEM := namedThrowawayCA(t, name, notAfter)
	ca := CA{
		CertPEM:     string(caCertPEM),
		KeyPEM:      string(encodeKeyPEM(caKey)),
		Subject:     caCert.Subject.CommonName,
		Serial:      SerialString(caCert.SerialNumber),
		NotBefore:   caCert.NotBefore.UTC().Format(time.RFC3339),
		NotAfter:    caCert.NotAfter.UTC().Format(time.RFC3339),
		Fingerprint: Fingerprint(caCert.Raw),
		Created:     time.Now().UTC().Format(time.RFC3339),
	}
	ctx := context.Background()
	var id int64
	if err := s.WithTx(ctx, func(tx *sql.Tx) error {
		var err error
		id, err = insertCATx(ctx, tx, ca, "previous")
		return err
	}); err != nil {
		t.Fatalf("insertCATx(previous %s): %v", name, err)
	}
	return id, caCert, caKey
}

// issueActiveUnder signs and inserts one active leaf under caCert/caKey,
// recording caID as its signer -- the real, chain-verifiable counterpart to
// fixtureCert/mustInsertCert, needed wherever a Replace/SwitchBack test must
// exercise actual re-issuance or R6's pinned x509.Verify.
func issueActiveUnder(t *testing.T, s *Store, caCert *x509.Certificate, caKey *rsa.PrivateKey, caID int64, fqdn string) (int64, *x509.Certificate) {
	t.Helper()
	leaf, err := GenerateLeaf(caCert, caKey, CertRequest{FQDN: fqdn}, 365)
	if err != nil {
		t.Fatalf("GenerateLeaf(%s): %v", fqdn, err)
	}
	leafCert, err := ParseCert(leaf.CertPEM)
	if err != nil {
		t.Fatalf("ParseCert(leaf %s): %v", fqdn, err)
	}
	c := certFromLeaf(fqdn, leaf, leafCert)
	c.CAID = &caID
	id := mustInsertCert(t, s, c)
	return id, leafCert
}

// assertVerifiesAgainstCA is the test-only pinned x509.Verify assertion R6
// requires for a freshly issued certificate: CurrentTime pinned inside its
// own validity window (never time.Now(), which a short leaf could outlive by
// the time this runs), KeyUsages Any (this checks the chain, not the leaf's
// own narrower EKU), and a roots pool holding only the CA it is expected to
// chain to.
func assertVerifiesAgainstCA(t *testing.T, leafCert, caCert *x509.Certificate) {
	t.Helper()
	pool := x509.NewCertPool()
	pool.AddCert(caCert)
	opts := x509.VerifyOptions{
		Roots:       pool,
		CurrentTime: leafCert.NotBefore.Add(time.Hour),
		KeyUsages:   []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
	}
	if _, err := leafCert.Verify(opts); err != nil {
		t.Fatalf("cert %q does not verify against its ca %q: %v", leafCert.Subject.CommonName, caCert.Subject.CommonName, err)
	}
}

// TestReplace_Reissue is §6's 50-cert case: every active row under the
// current CA is re-issued under the new one, each re-issued leaf actually
// chains to the new CA (R6), and the whole call stays comfortably inside the
// generous bound §6 sets for -race on slow hardware.
func TestReplace_Reissue(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	s.genCA = throwawayGenCA
	setupCtx := context.Background()
	setupCA(t, s, time.Now().AddDate(5, 0, 0))

	current, err := s.GetCurrentCA(setupCtx)
	if err != nil {
		t.Fatalf("GetCurrentCA: %v", err)
	}
	curCert, err := ParseCert([]byte(current.CertPEM))
	if err != nil {
		t.Fatalf("ParseCert(current): %v", err)
	}
	curKey, err := ParseKey([]byte(current.KeyPEM))
	if err != nil {
		t.Fatalf("ParseKey(current): %v", err)
	}

	// 50 RSA-2048 issuances, under -race on slow hardware, can themselves
	// take several seconds -- this setup work runs unbounded, so the
	// deadline below times only the call actually under test (R1's deadline
	// exists to fail a deadlock fast, not to bound fixture setup or RSA
	// keygen speed).
	const n = 50
	for i := 0; i < n; i++ {
		issueActiveUnder(t, s, curCert, curKey, current.ID, fmt.Sprintf("reissue-%03d.example.local", i))
	}

	// ReplaceCA's own crypto step (R2) signs n more leaves under the new CA
	// before its transaction ever opens -- on this environment that alone
	// measured ~15s under -race (a RSA-2048/-race property of the sandbox,
	// not of the code under test), well past R1's usual 10s deadlock-detect
	// window. The context below is sized to survive that crypto instead of
	// mistaking slow RSA for a hang; what the plan actually asks to be
	// bounded at 3s is the transaction itself, timed separately below via
	// the hook seam (R7), which fires only for the transaction's own writes.
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	var hookTimes []time.Time
	s.hook = func(string) error {
		hookTimes = append(hookTimes, time.Now())
		return nil
	}

	start := time.Now()
	result, err := s.ReplaceCA(ctx, "Reissue Target CA", "reissue", nil, 365, 30)
	elapsed := time.Since(start)
	t.Logf("ReplaceCA(reissue, %d certs) took %v end-to-end (including pre-transaction RSA crypto)", n, elapsed)
	if err != nil {
		t.Fatalf("ReplaceCA: %v", err)
	}
	if len(hookTimes) >= 2 {
		txSpan := hookTimes[len(hookTimes)-1].Sub(hookTimes[0])
		t.Logf("ReplaceCA(reissue, %d certs) transaction span (first hook to last hook) took %v", n, txSpan)
		if txSpan > 3*time.Second {
			t.Fatalf("ReplaceCA(reissue, %d certs) transaction span took %v, want under 3s", n, txSpan)
		}
	}
	if result.Reissued != n {
		t.Fatalf("result.Reissued = %d, want %d", result.Reissued, n)
	}
	if result.Deleted != 0 || result.Kept != 0 {
		t.Fatalf("result = %+v, want only Reissued set", result)
	}

	newCurrent, err := s.GetCurrentCA(ctx)
	if err != nil {
		t.Fatalf("GetCurrentCA after replace: %v", err)
	}
	if newCurrent.Fingerprint == current.Fingerprint {
		t.Fatal("current ca unchanged after replace")
	}
	newCurrentCert, err := ParseCert([]byte(newCurrent.CertPEM))
	if err != nil {
		t.Fatalf("ParseCert(new current): %v", err)
	}

	certs, err := s.ListCerts(ctx)
	if err != nil {
		t.Fatalf("ListCerts: %v", err)
	}
	found := 0
	for _, c := range certs {
		if !strings.HasPrefix(c.FQDN, "reissue-") {
			continue
		}
		found++
		if c.Status != StatusActive {
			t.Fatalf("cert %d status = %s, want active", c.ID, c.Status)
		}
		if c.CAID == nil || *c.CAID != newCurrent.ID {
			t.Fatalf("cert %d ca_id = %v, want %d", c.ID, c.CAID, newCurrent.ID)
		}
		if c.Stale {
			t.Fatalf("cert %d stale = true, want false (signed by the current ca)", c.ID)
		}
		full, err := s.GetCert(ctx, c.ID) // ListCerts never projects cert_pem
		if err != nil {
			t.Fatalf("GetCert(%d): %v", c.ID, err)
		}
		leafCert, err := ParseCert([]byte(*full.CertPEM))
		if err != nil {
			t.Fatalf("ParseCert(reissued leaf %d): %v", c.ID, err)
		}
		assertVerifiesAgainstCA(t, leafCert, newCurrentCert)
	}
	if found != n {
		t.Fatalf("found %d reissued certs, want %d", found, n)
	}

	if err := s.checkInvariants(ctx); err != nil {
		t.Fatalf("checkInvariants: %v", err)
	}
}

// TestReplace_Delete is §6's "existing=delete" case: every active row under
// the outgoing current CA is removed outright.
func TestReplace_Delete(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	s.genCA = throwawayGenCA
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	setupCA(t, s, time.Now().AddDate(5, 0, 0))

	current, err := s.GetCurrentCA(ctx)
	if err != nil {
		t.Fatalf("GetCurrentCA: %v", err)
	}
	curCert, err := ParseCert([]byte(current.CertPEM))
	if err != nil {
		t.Fatalf("ParseCert(current): %v", err)
	}
	curKey, err := ParseKey([]byte(current.KeyPEM))
	if err != nil {
		t.Fatalf("ParseKey(current): %v", err)
	}

	var ids []int64
	for i := 0; i < 3; i++ {
		id, _ := issueActiveUnder(t, s, curCert, curKey, current.ID, fmt.Sprintf("delete-%d.example.local", i))
		ids = append(ids, id)
	}

	result, err := s.ReplaceCA(ctx, "Delete Target CA", "delete", nil, 365, 30)
	if err != nil {
		t.Fatalf("ReplaceCA: %v", err)
	}
	if result.Deleted != 3 {
		t.Fatalf("result.Deleted = %d, want 3", result.Deleted)
	}
	if result.Reissued != 0 || result.Kept != 0 {
		t.Fatalf("result = %+v, want only Deleted set", result)
	}

	for _, id := range ids {
		if _, err := s.GetCert(ctx, id); !errors.Is(err, ErrNotFound) {
			t.Fatalf("GetCert(%d) after delete = %v, want ErrNotFound", id, err)
		}
	}

	// Nothing signs the outgoing (now previous) ca any more, so it must
	// have dropped immediately (P2).
	if got, err := s.GetPreviousCA(ctx); err != nil || got != nil {
		t.Fatalf("GetPreviousCA after delete-everything replace = (%+v, %v), want (nil, nil)", got, err)
	}
	if !result.PreviousDropped {
		t.Fatal("result.PreviousDropped = false, want true")
	}

	if err := s.checkInvariants(ctx); err != nil {
		t.Fatalf("checkInvariants: %v", err)
	}
}

// TestReplace_Keep is §6's "existing=keep" case: the outgoing CA's active
// rows survive, unmoved, and become stale once it is demoted.
func TestReplace_Keep(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	s.genCA = throwawayGenCA
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	setupCA(t, s, time.Now().AddDate(5, 0, 0))

	current, err := s.GetCurrentCA(ctx)
	if err != nil {
		t.Fatalf("GetCurrentCA: %v", err)
	}
	curCert, err := ParseCert([]byte(current.CertPEM))
	if err != nil {
		t.Fatalf("ParseCert(current): %v", err)
	}
	curKey, err := ParseKey([]byte(current.KeyPEM))
	if err != nil {
		t.Fatalf("ParseKey(current): %v", err)
	}

	var ids []int64
	for i := 0; i < 3; i++ {
		id, _ := issueActiveUnder(t, s, curCert, curKey, current.ID, fmt.Sprintf("keep-%d.example.local", i))
		ids = append(ids, id)
	}

	result, err := s.ReplaceCA(ctx, "Keep Target CA", "keep", nil, 365, 30)
	if err != nil {
		t.Fatalf("ReplaceCA: %v", err)
	}
	if result.Kept != 3 {
		t.Fatalf("result.Kept = %d, want 3", result.Kept)
	}
	if result.Reissued != 0 || result.Deleted != 0 {
		t.Fatalf("result = %+v, want only Kept set", result)
	}
	if result.PreviousDropped {
		t.Fatal("result.PreviousDropped = true, want false (the demoted ca still signs the kept rows)")
	}

	for _, id := range ids {
		c, err := s.GetCert(ctx, id)
		if err != nil {
			t.Fatalf("GetCert(%d): %v", id, err)
		}
		if c.Status != StatusActive {
			t.Fatalf("cert %d status = %s, want active", id, c.Status)
		}
		if c.CAID == nil || *c.CAID != current.ID {
			t.Fatalf("cert %d ca_id = %v, want the demoted (old current) ca %d", id, c.CAID, current.ID)
		}
		if !c.Stale {
			t.Fatalf("cert %d stale = false, want true (its signer is now the previous ca)", id)
		}
	}

	previous, err := s.GetPreviousCA(ctx)
	if err != nil {
		t.Fatalf("GetPreviousCA: %v", err)
	}
	if previous == nil || previous.ID != current.ID {
		t.Fatalf("GetPreviousCA = %+v, want the demoted original current ca %d", previous, current.ID)
	}

	if err := s.checkInvariants(ctx); err != nil {
		t.Fatalf("checkInvariants: %v", err)
	}
}

// TestReplace_PreviousStaleRequired is P4's test: omitting previousStale
// while the outgoing previous CA still signs an active row is a 409, and
// the value "keep" is a 400 -- previousStale has no "keep" (unlike
// `existing`).
func TestReplace_PreviousStaleRequired(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	s.genCA = throwawayGenCA
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	setupCA(t, s, time.Now().AddDate(5, 0, 0))

	prevID, prevCert, prevKey := insertRealPreviousCANamed(t, s, "Stale Required Previous CA", time.Now().AddDate(5, 0, 0))
	issueActiveUnder(t, s, prevCert, prevKey, prevID, "stale-required.example.local")

	if _, err := s.ReplaceCA(ctx, "Stale Required New CA", "keep", nil, 365, 30); !errors.Is(err, ErrPreviousStaleChoiceRequired) {
		t.Fatalf("ReplaceCA(previousStale omitted) error = %v, want ErrPreviousStaleChoiceRequired", err)
	} else if status, _ := storeErrorStatus(err); status != http.StatusConflict {
		t.Fatalf("status = %d, want 409", status)
	}

	keep := "keep"
	if _, err := s.ReplaceCA(ctx, "Stale Required New CA", "keep", &keep, 365, 30); !errors.Is(err, ErrValidation) {
		t.Fatalf("ReplaceCA(previousStale=keep) error = %v, want ErrValidation", err)
	} else if status, _ := storeErrorStatus(err); status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", status)
	}

	bogus := "bogus"
	if _, err := s.ReplaceCA(ctx, "Stale Required New CA", "keep", &bogus, 365, 30); !errors.Is(err, ErrValidation) {
		t.Fatalf("ReplaceCA(previousStale=bogus) error = %v, want ErrValidation", err)
	}

	// None of the failed calls should have written anything.
	if got, err := s.GetPreviousCA(ctx); err != nil || got == nil || got.ID != prevID {
		t.Fatalf("GetPreviousCA after refused replaces = (%+v, %v), want the unchanged previous ca %d", got, err, prevID)
	}
	if err := s.checkInvariants(ctx); err != nil {
		t.Fatalf("checkInvariants: %v", err)
	}
}

// TestReplace_PreviousStaleReissue and TestReplace_PreviousStaleDelete both
// exercise §6's "drop the outgoing previous CA and its archived rows" case:
// regardless of which choice previousStale makes about its *active* rows,
// the outgoing previous CA's pre-existing *archived* rows are deleted with
// it (P2) as part of retiring it in step 1, and the previous CA row itself
// is gone afterward.
func TestReplace_PreviousStaleReissue(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	s.genCA = throwawayGenCA
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	setupCA(t, s, time.Now().AddDate(5, 0, 0))

	prevID, prevCert, prevKey := insertRealPreviousCANamed(t, s, "Stale Reissue Previous CA", time.Now().AddDate(5, 0, 0))
	activeID1, _ := issueActiveUnder(t, s, prevCert, prevKey, prevID, "stale-reissue-1.example.local")
	activeID2, _ := issueActiveUnder(t, s, prevCert, prevKey, prevID, "stale-reissue-2.example.local")
	archived := fixtureCert("stale-reissue-archived.example.local", StatusArchived, "2029-01-01T00:00:00Z")
	archived.CAID = &prevID
	archivedID := mustInsertCert(t, s, archived)

	reissue := "reissue"
	result, err := s.ReplaceCA(ctx, "Stale Reissue New CA", "keep", &reissue, 365, 30)
	if err != nil {
		t.Fatalf("ReplaceCA: %v", err)
	}
	if result.Reissued != 2 {
		t.Fatalf("result.Reissued = %d, want 2", result.Reissued)
	}

	newCurrent, err := s.GetCurrentCA(ctx)
	if err != nil {
		t.Fatalf("GetCurrentCA: %v", err)
	}
	certs, err := s.ListCerts(ctx)
	if err != nil {
		t.Fatalf("ListCerts: %v", err)
	}
	found := 0
	for _, c := range certs {
		if !strings.HasPrefix(c.FQDN, "stale-reissue-") || c.Status != StatusActive {
			continue
		}
		found++
		if c.CAID == nil || *c.CAID != newCurrent.ID {
			t.Fatalf("re-issued cert %d ca_id = %v, want the new current ca %d", c.ID, c.CAID, newCurrent.ID)
		}
	}
	if found != 2 {
		t.Fatalf("found %d re-issued previousStale certs, want 2", found)
	}

	// The old active rows were archived-then-deleted (P2), not left behind.
	for _, id := range []int64{activeID1, activeID2, archivedID} {
		if _, err := s.GetCert(ctx, id); !errors.Is(err, ErrNotFound) {
			t.Fatalf("GetCert(%d) after retiring the old previous ca = %v, want ErrNotFound", id, err)
		}
	}
	if _, err := s.GetCAByID(ctx, prevID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetCAByID(old previous %d) = %v, want ErrNotFound", prevID, err)
	}

	if err := s.checkInvariants(ctx); err != nil {
		t.Fatalf("checkInvariants: %v", err)
	}
}

func TestReplace_PreviousStaleDelete(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	s.genCA = throwawayGenCA
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	setupCA(t, s, time.Now().AddDate(5, 0, 0))

	prevID, prevCert, prevKey := insertRealPreviousCANamed(t, s, "Stale Delete Previous CA", time.Now().AddDate(5, 0, 0))
	activeID1, _ := issueActiveUnder(t, s, prevCert, prevKey, prevID, "stale-delete-1.example.local")
	activeID2, _ := issueActiveUnder(t, s, prevCert, prevKey, prevID, "stale-delete-2.example.local")
	archived := fixtureCert("stale-delete-archived.example.local", StatusArchived, "2029-01-01T00:00:00Z")
	archived.CAID = &prevID
	archivedID := mustInsertCert(t, s, archived)

	del := "delete"
	result, err := s.ReplaceCA(ctx, "Stale Delete New CA", "keep", &del, 365, 30)
	if err != nil {
		t.Fatalf("ReplaceCA: %v", err)
	}
	if result.Deleted != 2 {
		t.Fatalf("result.Deleted = %d, want 2", result.Deleted)
	}

	for _, id := range []int64{activeID1, activeID2, archivedID} {
		if _, err := s.GetCert(ctx, id); !errors.Is(err, ErrNotFound) {
			t.Fatalf("GetCert(%d) after retiring the old previous ca = %v, want ErrNotFound", id, err)
		}
	}
	if _, err := s.GetCAByID(ctx, prevID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetCAByID(old previous %d) = %v, want ErrNotFound", prevID, err)
	}

	if err := s.checkInvariants(ctx); err != nil {
		t.Fatalf("checkInvariants: %v", err)
	}
}

// TestReplace_PreviousStaleDropClearsQuarantinedCAID is D1's other
// regression test (architect review): the outgoing previous CA also has a
// quarantined row pointing at it (inserted directly), and ReplaceCA's
// existing=delete + previousStale=delete drops that previous CA in step 1.
// Its quarantined row must have its ca_id cleared in the same transaction --
// otherwise the row's ca_id dangles and Open fails structural invariant 3 on
// the next boot.
func TestReplace_PreviousStaleDropClearsQuarantinedCAID(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "certmachine.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	s.genCA = throwawayGenCA
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	setupCA(t, s, time.Now().AddDate(5, 0, 0))

	prevID, prevCert, prevKey := insertRealPreviousCANamed(t, s, "Quarantine Drop Previous CA", time.Now().AddDate(5, 0, 0))
	activeID, _ := issueActiveUnder(t, s, prevCert, prevKey, prevID, "quarantine-drop-replace-active.example.local")

	quarantined := fixtureCert("quarantine-drop-replace-quarantined.example.local", StatusQuarantined, "")
	quarantined.CAID = &prevID
	quarantined.QuarantineReason = strPtr("test fixture: reproduces the D1 dangling ca_id via ReplaceCA")
	quarantinedID := mustInsertCert(t, s, quarantined)

	del := "delete"
	result, err := s.ReplaceCA(ctx, "Quarantine Drop New CA", "delete", &del, 365, 30)
	if err != nil {
		t.Fatalf("ReplaceCA: %v", err)
	}
	if result.Deleted != 1 {
		t.Fatalf("result.Deleted = %d, want 1", result.Deleted)
	}
	if _, err := s.GetCert(ctx, activeID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetCert(active row under the dropped previous ca) = %v, want ErrNotFound", err)
	}
	if _, err := s.GetCAByID(ctx, prevID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetCAByID(old previous %d) = %v, want ErrNotFound", prevID, err)
	}

	quarantinedRow, err := s.GetCert(ctx, quarantinedID)
	if err != nil {
		t.Fatalf("GetCert(quarantined row) after replace: %v", err)
	}
	if quarantinedRow.CAID != nil {
		t.Fatalf("quarantined row's caId = %d after its ca dropped, want nil", *quarantinedRow.CAID)
	}

	if err := s.checkInvariants(ctx); err != nil {
		t.Fatalf("checkInvariants: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer reopened.Close()
	if err := reopened.checkInvariants(ctx); err != nil {
		t.Fatalf("checkInvariants after reopen: %v", err)
	}
}

// TestReplace_NameCollision is P6's test: the new CA's file stem must differ
// case-insensitively from both the current and the previous CA's stems.
func TestReplace_NameCollision(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	s.genCA = throwawayGenCA
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	setupCA(t, s, time.Now().AddDate(5, 0, 0))

	current, err := s.GetCurrentCA(ctx)
	if err != nil {
		t.Fatalf("GetCurrentCA: %v", err)
	}

	// Same case.
	if _, err := s.ReplaceCA(ctx, current.Subject, "keep", nil, 365, 30); !errors.Is(err, ErrValidation) {
		t.Fatalf("ReplaceCA(name=current subject) error = %v, want ErrValidation", err)
	}
	// Different case, same stem.
	if _, err := s.ReplaceCA(ctx, strings.ToUpper(current.Subject), "keep", nil, 365, 30); !errors.Is(err, ErrValidation) {
		t.Fatalf("ReplaceCA(name=upper(current subject)) error = %v, want ErrValidation", err)
	}

	insertRealPreviousCANamed(t, s, "Previous Collision CA", time.Now().AddDate(5, 0, 0))
	if _, err := s.ReplaceCA(ctx, "previous collision ca", "keep", nil, 365, 30); !errors.Is(err, ErrValidation) {
		t.Fatalf("ReplaceCA(name=lower(previous subject)) error = %v, want ErrValidation", err)
	}

	// The current ca must be unchanged after every refused call.
	if still, err := s.GetCurrentCA(ctx); err != nil || still.Fingerprint != current.Fingerprint {
		t.Fatalf("current ca changed after refused replaces: got %+v, err %v", still, err)
	}
	if err := s.checkInvariants(ctx); err != nil {
		t.Fatalf("checkInvariants: %v", err)
	}
}

// TestReplace_KeepUnderExpiredOutgoingCA is P8's test: Replace is allowed
// even when the current CA is already expired, and a "keep" choice leaves
// its rows stale but intact -- Open and checkInvariants both still pass
// after a reopen, even though the kept rows' NotAfter is now before their
// NotBefore (GenerateLeaf's clamp against an already-expired CA, which the
// staleness and signature checks both ignore, since neither is time-aware).
func TestReplace_KeepUnderExpiredOutgoingCA(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "certmachine.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	s.genCA = throwawayGenCA
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	past := time.Now().Add(-24 * time.Hour)
	setupCA(t, s, past)

	current, err := s.GetCurrentCA(ctx)
	if err != nil {
		t.Fatalf("GetCurrentCA: %v", err)
	}
	curCert, err := ParseCert([]byte(current.CertPEM))
	if err != nil {
		t.Fatalf("ParseCert(current): %v", err)
	}
	curKey, err := ParseKey([]byte(current.KeyPEM))
	if err != nil {
		t.Fatalf("ParseKey(current): %v", err)
	}

	var ids []int64
	for i, fqdn := range []string{"expired-keep-1.example.local", "expired-keep-2.example.local"} {
		leaf, err := GenerateLeaf(curCert, curKey, CertRequest{FQDN: fqdn}, 365)
		if err != nil {
			t.Fatalf("GenerateLeaf(%d): %v", i, err)
		}
		leafCert, err := ParseCert(leaf.CertPEM)
		if err != nil {
			t.Fatalf("ParseCert(leaf %d): %v", i, err)
		}
		c := certFromLeaf(fqdn, leaf, leafCert)
		c.CAID = &current.ID
		c.Status = StatusActive
		id := mustInsertCert(t, s, c)
		ids = append(ids, id)
	}

	result, err := s.ReplaceCA(ctx, "Post-Expiry CA", "keep", nil, 365, 30)
	if err != nil {
		t.Fatalf("ReplaceCA(keep) under an expired outgoing ca: %v", err)
	}
	if result.Kept != 2 {
		t.Fatalf("result.Kept = %d, want 2", result.Kept)
	}

	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer reopened.Close()

	if err := reopened.checkInvariants(ctx); err != nil {
		t.Fatalf("checkInvariants after reopen: %v", err)
	}
	for _, id := range ids {
		c, err := reopened.GetCert(ctx, id)
		if err != nil {
			t.Fatalf("GetCert(%d) after reopen: %v", id, err)
		}
		if !c.Stale {
			t.Fatalf("cert %d stale = false, want true", id)
		}
	}
}

// newAtomicityFixture builds a fresh store with a real, signable current CA
// (2 active rows) and a real, signable previous CA (2 active rows) -- both
// row sets TestReplace_Atomicity's two configurations need in order for
// every hook name in the table to be reachable.
func newAtomicityFixture(t *testing.T) *Store {
	t.Helper()
	s := openTestStore(t)
	s.genCA = throwawayGenCA
	ctx := context.Background()
	setupCA(t, s, time.Now().AddDate(5, 0, 0))

	current, err := s.GetCurrentCA(ctx)
	if err != nil {
		t.Fatalf("GetCurrentCA: %v", err)
	}
	curCert, err := ParseCert([]byte(current.CertPEM))
	if err != nil {
		t.Fatalf("ParseCert(current): %v", err)
	}
	curKey, err := ParseKey([]byte(current.KeyPEM))
	if err != nil {
		t.Fatalf("ParseKey(current): %v", err)
	}
	issueActiveUnder(t, s, curCert, curKey, current.ID, "atomicity-current-1.example.local")
	issueActiveUnder(t, s, curCert, curKey, current.ID, "atomicity-current-2.example.local")

	prevID, prevCert, prevKey := insertRealPreviousCANamed(t, s, "Atomicity Previous CA", time.Now().AddDate(5, 0, 0))
	issueActiveUnder(t, s, prevCert, prevKey, prevID, "atomicity-previous-1.example.local")
	issueActiveUnder(t, s, prevCert, prevKey, prevID, "atomicity-previous-2.example.local")

	return s
}

// expectedAtomicityHooks computes every hook name §3.2's table says is
// reachable for one Replace call over newAtomicityFixture's 2-and-2 row
// sets, given existing and previousStale.
func expectedAtomicityHooks(existing, previousStale string) map[string]bool {
	m := map[string]bool{
		"retire-previous": true,
		"demote":          true,
		"insert-ca":       true,
		"drop-previous":   true,
	}
	switch previousStale {
	case "delete":
		for i := 0; i < 2; i++ {
			m[fmt.Sprintf("prev-delete:%d", i)] = true
		}
	case "reissue":
		for i := 0; i < 2; i++ {
			m[fmt.Sprintf("prev-archive:%d", i)] = true
			m[fmt.Sprintf("prev-reissue:%d", i)] = true
		}
	}
	switch existing {
	case "reissue":
		for i := 0; i < 2; i++ {
			m[fmt.Sprintf("reissue:%d", i)] = true
		}
	case "delete":
		for i := 0; i < 2; i++ {
			m[fmt.Sprintf("delete:%d", i)] = true
		}
	}
	return m
}

// TestReplace_Atomicity is §3.2's fully specified atomicity test: two
// configurations, a recording pass that must cover every reachable hook
// name, then a fresh fixture and an injected failure at each recorded name
// in turn, asserting both that ReplaceCA returns an error and that the ca
// and certs tables are byte-identical, column for column, to before the
// call.
func TestReplace_Atomicity(t *testing.T) {
	t.Parallel()
	configs := []struct {
		name          string
		existing      string
		previousStale string
	}{
		{"reissue-existing_delete-previousStale", "reissue", "delete"},
		{"delete-existing_reissue-previousStale", "delete", "reissue"},
	}

	for _, cfg := range configs {
		cfg := cfg
		t.Run(cfg.name, func(t *testing.T) {
			// t.Parallel() first: the deadline below must start ticking only
			// once this subtest actually resumes and runs, not while it sits
			// paused waiting for other parallel tests/subtests to get a
			// scheduling slot.
			t.Parallel()
			ps := cfg.previousStale

			expected := expectedAtomicityHooks(cfg.existing, ps)

			// The recording pass runs synchronously here (this subtest's own
			// goroutine, already resumed), so one ctx for it is fine.
			recCtx, recCancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer recCancel()
			rec := newAtomicityFixture(t)
			recorded := map[string]bool{}
			rec.hook = func(step string) error {
				recorded[step] = true
				return nil
			}
			if _, err := rec.ReplaceCA(recCtx, "Atomicity Recording CA "+cfg.name, cfg.existing, &ps, 365, 30); err != nil {
				t.Fatalf("recording pass ReplaceCA: %v", err)
			}
			for name := range expected {
				if !recorded[name] {
					t.Errorf("recording pass never emitted hook %q", name)
				}
			}
			for name := range recorded {
				if !expected[name] {
					t.Errorf("recording pass emitted unexpected hook %q", name)
				}
			}

			for name := range expected {
				name := name
				t.Run(name, func(t *testing.T) {
					// Same reasoning: t.Parallel() first, and this subtest's
					// own 10s ctx is created only after it, so a long queue
					// of sibling parallel subtests waiting for a scheduling
					// slot never eats into the deadline meant to bound this
					// subtest's own ReplaceCA call.
					t.Parallel()
					ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
					defer cancel()

					fx := newAtomicityFixture(t)
					beforeCA := dumpTable(t, fx.db, "ca")
					beforeCerts := dumpTable(t, fx.db, "certs")

					fx.hook = func(step string) error {
						if step == name {
							return fmt.Errorf("injected failure at %s", name)
						}
						return nil
					}
					if _, err := fx.ReplaceCA(ctx, "Atomicity Injected CA "+cfg.name, cfg.existing, &ps, 365, 30); err == nil {
						t.Fatalf("ReplaceCA with injected failure at %q succeeded, want error", name)
					}

					afterCA := dumpTable(t, fx.db, "ca")
					afterCerts := dumpTable(t, fx.db, "certs")
					if !reflect.DeepEqual(beforeCA, afterCA) {
						t.Errorf("ca table changed after failure at %q:\nbefore=%v\nafter=%v", name, beforeCA, afterCA)
					}
					if !reflect.DeepEqual(beforeCerts, afterCerts) {
						t.Errorf("certs table changed after failure at %q:\nbefore=%v\nafter=%v", name, beforeCerts, afterCerts)
					}
				})
			}
		})
	}
}

// TestSwitchBack_NoPrevious409 is D9's test: no previous CA means nothing
// to switch back to.
func TestSwitchBack_NoPrevious409(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	setupCA(t, s, time.Now().AddDate(5, 0, 0))

	if _, err := s.SwitchBack(ctx, 30); !errors.Is(err, ErrNoPreviousCA) {
		t.Fatalf("SwitchBack error = %v, want ErrNoPreviousCA", err)
	} else if status, _ := storeErrorStatus(err); status != http.StatusConflict {
		t.Fatalf("status = %d, want 409", status)
	}
}

// TestSwitchBack_Expiring409 is P7's test: a previous CA too close to its
// own expiry refuses SwitchBack -- switching back to a CA that could not
// issue anyway defeats the point.
func TestSwitchBack_Expiring409(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	setupCA(t, s, time.Now().AddDate(5, 0, 0))
	insertRealPreviousCANamed(t, s, "Expiring Previous CA", time.Now().Add(24*time.Hour))

	if _, err := s.SwitchBack(ctx, 30); !errors.Is(err, ErrCAExpiringSoon) {
		t.Fatalf("SwitchBack error = %v, want ErrCAExpiringSoon", err)
	} else if status, _ := storeErrorStatus(err); status != http.StatusConflict {
		t.Fatalf("status = %d, want 409", status)
	}
}

// TestSwitchBack_Swap is D9's positive case: SwitchBack undoes a Replace,
// restoring the original CA to current and demoting the replaced-in one
// back to previous.
func TestSwitchBack_Swap(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	s.genCA = throwawayGenCA
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	setupCA(t, s, time.Now().AddDate(5, 0, 0))

	origCurrent, err := s.GetCurrentCA(ctx)
	if err != nil {
		t.Fatalf("GetCurrentCA: %v", err)
	}
	curCert, err := ParseCert([]byte(origCurrent.CertPEM))
	if err != nil {
		t.Fatalf("ParseCert: %v", err)
	}
	curKey, err := ParseKey([]byte(origCurrent.KeyPEM))
	if err != nil {
		t.Fatalf("ParseKey: %v", err)
	}
	issueActiveUnder(t, s, curCert, curKey, origCurrent.ID, "swap-keep.example.local")

	if _, err := s.ReplaceCA(ctx, "Swap New CA", "keep", nil, 365, 30); err != nil {
		t.Fatalf("ReplaceCA: %v", err)
	}
	newCurrent, err := s.GetCurrentCA(ctx)
	if err != nil {
		t.Fatalf("GetCurrentCA after replace: %v", err)
	}
	if newCurrent.Fingerprint == origCurrent.Fingerprint {
		t.Fatal("current ca unchanged after replace")
	}

	// The new CA must sign something active, or it would drop the instant
	// SwitchBack demotes it back to previous (P2) -- that is
	// TestSwitchBack_DropsUnused's case, not this one, which wants a
	// genuine swap that leaves both CAs present afterward.
	req, err := ValidateRequest("swap-new.example.local", nil, nil)
	if err != nil {
		t.Fatalf("ValidateRequest: %v", err)
	}
	if _, err := s.Generate(ctx, req, 365, 30); err != nil {
		t.Fatalf("Generate under the new current ca: %v", err)
	}

	result, err := s.SwitchBack(ctx, 30)
	if err != nil {
		t.Fatalf("SwitchBack: %v", err)
	}
	if result.PreviousDropped {
		t.Fatal("result.PreviousDropped = true, want false (the demoted ca still signs the new-current cert)")
	}

	afterCurrent, err := s.GetCurrentCA(ctx)
	if err != nil {
		t.Fatalf("GetCurrentCA after switch-back: %v", err)
	}
	if afterCurrent.Fingerprint != origCurrent.Fingerprint {
		t.Fatalf("current after switch-back = %s, want the original %s", afterCurrent.Fingerprint, origCurrent.Fingerprint)
	}
	afterPrevious, err := s.GetPreviousCA(ctx)
	if err != nil {
		t.Fatalf("GetPreviousCA after switch-back: %v", err)
	}
	if afterPrevious == nil || afterPrevious.Fingerprint != newCurrent.Fingerprint {
		t.Fatalf("previous after switch-back = %+v, want the replaced-out new ca", afterPrevious)
	}

	if err := s.checkInvariants(ctx); err != nil {
		t.Fatalf("checkInvariants: %v", err)
	}
}

// TestSwitchBack_ConcurrentChangeGuard is D2's test (architect review):
// SwitchBack's own pre-transaction current/previous CA read must be re-
// verified as the first statement inside its transaction, or a concurrent
// Replace/SwitchBack landing in that window could swap the wrong rows'
// roles by id. This exercises verifySwitchBackSnapshotTx directly with a
// deliberately stale id -- deterministic, not timing-based, per the
// architect's own suggestion ("call the tx-level helper directly with a
// stale id").
func TestSwitchBack_ConcurrentChangeGuard(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	setupCA(t, s, time.Now().AddDate(5, 0, 0))

	current, err := s.GetCurrentCA(ctx)
	if err != nil {
		t.Fatalf("GetCurrentCA: %v", err)
	}
	previousID := insertPreviousCA(t, s, "switchback-guard-previous")

	// The correct, up-to-date snapshot passes.
	if err := s.WithTx(ctx, func(tx *sql.Tx) error {
		return verifySwitchBackSnapshotTx(ctx, tx, current.ID, previousID)
	}); err != nil {
		t.Fatalf("verifySwitchBackSnapshotTx(correct snapshot) = %v, want nil", err)
	}

	// A stale previous id (as if the previous CA had been dropped and
	// replaced with a different one between SwitchBack's pre-transaction
	// read and this transaction) must refuse.
	if err := s.WithTx(ctx, func(tx *sql.Tx) error {
		return verifySwitchBackSnapshotTx(ctx, tx, current.ID, previousID+1000)
	}); !errors.Is(err, ErrConcurrentChange) {
		t.Fatalf("verifySwitchBackSnapshotTx(stale previous id) = %v, want ErrConcurrentChange", err)
	}

	// A stale current id (as if a Replace had demoted a different CA to
	// current between the read and this transaction) must also refuse.
	if err := s.WithTx(ctx, func(tx *sql.Tx) error {
		return verifySwitchBackSnapshotTx(ctx, tx, current.ID+1000, previousID)
	}); !errors.Is(err, ErrConcurrentChange) {
		t.Fatalf("verifySwitchBackSnapshotTx(stale current id) = %v, want ErrConcurrentChange", err)
	}

	// Nothing must have been written by any of the above.
	if err := s.checkInvariants(ctx); err != nil {
		t.Fatalf("checkInvariants: %v", err)
	}
}

// TestSwitchBack_DropsUnused is P2's test through SwitchBack: the CA that
// was current before the swap drops immediately, along with its (here,
// nonexistent) archived rows, once demoted back to previous with nothing
// active signed by it.
func TestSwitchBack_DropsUnused(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	s.genCA = throwawayGenCA
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	setupCA(t, s, time.Now().AddDate(5, 0, 0))

	origCurrent, err := s.GetCurrentCA(ctx)
	if err != nil {
		t.Fatalf("GetCurrentCA: %v", err)
	}
	curCert, err := ParseCert([]byte(origCurrent.CertPEM))
	if err != nil {
		t.Fatalf("ParseCert: %v", err)
	}
	curKey, err := ParseKey([]byte(origCurrent.KeyPEM))
	if err != nil {
		t.Fatalf("ParseKey: %v", err)
	}
	issueActiveUnder(t, s, curCert, curKey, origCurrent.ID, "dropsunused.example.local")

	// existing=keep: the new CA (about to become current) signs nothing at
	// all, so it is primed to drop as soon as SwitchBack demotes it back to
	// previous.
	if _, err := s.ReplaceCA(ctx, "DropsUnused New CA", "keep", nil, 365, 30); err != nil {
		t.Fatalf("ReplaceCA: %v", err)
	}

	result, err := s.SwitchBack(ctx, 30)
	if err != nil {
		t.Fatalf("SwitchBack: %v", err)
	}
	if !result.PreviousDropped {
		t.Fatal("result.PreviousDropped = false, want true")
	}
	if got, err := s.GetPreviousCA(ctx); err != nil || got != nil {
		t.Fatalf("GetPreviousCA after drop = (%+v, %v), want (nil, nil)", got, err)
	}

	if err := s.checkInvariants(ctx); err != nil {
		t.Fatalf("checkInvariants: %v", err)
	}
}

// TestReplaceAndSwitchBack_AuditLog is §6's observability test: one
// log.Printf line per successful Replace and per successful SwitchBack
// (P9).
//
// Deliberately NOT t.Parallel(): it calls log.SetOutput, which redirects the
// package-wide default logger for every goroutine, including any other test
// running concurrently in this binary. Go runs every non-parallel top-level
// test to completion, one at a time, before it starts running the paused
// parallel ones together (https://pkg.go.dev/testing#T.Parallel), so this
// test's log.SetOutput/defer-restore window can never overlap a parallel
// test's own log.Printf calls.
func TestReplaceAndSwitchBack_AuditLog(t *testing.T) {
	s := openTestStore(t)
	s.genCA = throwawayGenCA
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	setupCA(t, s, time.Now().AddDate(5, 0, 0))

	current, err := s.GetCurrentCA(ctx)
	if err != nil {
		t.Fatalf("GetCurrentCA: %v", err)
	}
	curCert, err := ParseCert([]byte(current.CertPEM))
	if err != nil {
		t.Fatalf("ParseCert: %v", err)
	}
	curKey, err := ParseKey([]byte(current.KeyPEM))
	if err != nil {
		t.Fatalf("ParseKey: %v", err)
	}
	issueActiveUnder(t, s, curCert, curKey, current.ID, "audit-keep.example.local")

	var buf bytes.Buffer
	origOut := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(origOut)

	if _, err := s.ReplaceCA(ctx, "Audit CA", "keep", nil, 365, 30); err != nil {
		t.Fatalf("ReplaceCA: %v", err)
	}
	if !strings.Contains(buf.String(), "certmachine: ca replace") {
		t.Fatalf("audit log missing the replace line: %q", buf.String())
	}

	buf.Reset()
	if _, err := s.SwitchBack(ctx, 30); err != nil {
		t.Fatalf("SwitchBack: %v", err)
	}
	if !strings.Contains(buf.String(), "certmachine: ca switch-back") {
		t.Fatalf("audit log missing the switch-back line: %q", buf.String())
	}
}

// TestRandomizedSequence is §6's oracle test: 100 random operations per
// seed, drawn from Replace (every existing/previousStale combination),
// Renew, Edit, Delete, SwitchBack and Generate, with checkInvariants run
// after every single one. A failure here means some combination of two
// operations violates an invariant that no single-operation test would ever
// catch.
func TestRandomizedSequence(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("randomized CA-rotation sequence skipped under -short")
	}
	for _, seed := range []int64{1, 2, 3} {
		seed := seed
		t.Run(fmt.Sprintf("seed=%d", seed), func(t *testing.T) {
			// t.Parallel() first, so the 120s ctx runRandomizedSequence opens
			// starts ticking only once this subtest is actually scheduled,
			// each seed gets its own Store/temp DB, so the three seeds share
			// no state and are safe to run concurrently.
			t.Parallel()
			runRandomizedSequence(t, seed)
		})
	}
}

func runRandomizedSequence(t *testing.T, seed int64) {
	// Opened directly (rather than via openTestStore) so the hardening pass
	// at the end of this function can Close and reopen the same file --
	// t.Cleanup below still guarantees the final handle is closed.
	dbPath := filepath.Join(t.TempDir(), "certmachine.db")
	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	s.genCA = throwawayGenCA
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	setupCA(t, s, time.Now().AddDate(5, 0, 0))

	// Hardening (architect review): every seed's fixture also carries a
	// quarantined row with a non-NULL ca_id pointing at the current CA,
	// mirroring the importer's real quarantine shapes (D1) -- it must
	// survive every random operation, including a drop of whichever CA it
	// points at, without ever leaving a dangling ca_id behind.
	current, err := s.GetCurrentCA(ctx)
	if err != nil {
		t.Fatalf("GetCurrentCA: %v", err)
	}
	quarantined := fixtureCert(fmt.Sprintf("rand-%d-quarantined.example.local", seed), StatusQuarantined, "")
	quarantined.CAID = &current.ID
	quarantined.QuarantineReason = strPtr("randomized-sequence fixture")
	mustInsertCert(t, s, quarantined)

	rng := mrand.New(mrand.NewSource(seed))
	var activeIDs []int64
	counter := 0

	ops := []string{
		"replace-keep", "replace-reissue", "replace-delete",
		"renew", "edit", "delete", "switch-back", "generate",
	}

	for i := 0; i < 100; i++ {
		op := ops[rng.Intn(len(ops))]
		switch op {
		case "generate":
			counter++
			req, err := ValidateRequest(fmt.Sprintf("rand-%d-%d.example.local", seed, counter), nil, nil)
			if err != nil {
				t.Fatalf("seed=%d op=%d(%s): ValidateRequest: %v", seed, i, op, err)
			}
			if res, err := s.Generate(ctx, req, 365, 30); err == nil {
				activeIDs = append(activeIDs, res.Cert.ID)
			}

		case "renew":
			if len(activeIDs) > 0 {
				id := activeIDs[rng.Intn(len(activeIDs))]
				if res, err := s.Renew(ctx, id, 365, 30); err == nil {
					activeIDs = append(activeIDs, res.Cert.ID)
				}
			}

		case "edit":
			if len(activeIDs) > 0 {
				id := activeIDs[rng.Intn(len(activeIDs))]
				counter++
				req := CertRequest{FQDN: fmt.Sprintf("rand-edit-%d-%d.example.local", seed, counter)}
				if res, err := s.Edit(ctx, id, req, 200, 30); err == nil {
					activeIDs = append(activeIDs, res.Cert.ID)
				}
			}

		case "delete":
			if len(activeIDs) > 0 {
				idx := rng.Intn(len(activeIDs))
				id := activeIDs[idx]
				if c, err := s.GetCert(ctx, id); err == nil {
					if _, err := s.Delete(ctx, id, c.FQDN); err == nil {
						activeIDs = append(activeIDs[:idx], activeIDs[idx+1:]...)
					}
				}
			}

		case "switch-back":
			_, _ = s.SwitchBack(ctx, 30) // ErrNoPreviousCA etc. are expected, not a bug

		case "replace-keep", "replace-reissue", "replace-delete":
			existing := strings.TrimPrefix(op, "replace-")
			var previousStale *string
			if previous, err := s.GetPreviousCA(ctx); err != nil {
				t.Fatalf("seed=%d op=%d(%s): GetPreviousCA: %v", seed, i, op, err)
			} else if previous != nil {
				choice := []string{"reissue", "delete"}[rng.Intn(2)]
				previousStale = &choice
			}
			counter++
			name := fmt.Sprintf("Rand CA %d-%d", seed, counter)
			_, _ = s.ReplaceCA(ctx, name, existing, previousStale, 365, 30) // some refusals are expected, not a bug
		}

		if err := s.checkInvariants(ctx); err != nil {
			t.Fatalf("seed=%d op=%d(%s): checkInvariants: %v", seed, i, op, err)
		}
	}

	// Hardening (architect review): close and reopen at the end of the seed
	// and assert Open succeeds and checkInvariants still passes -- Open runs
	// checkStructuralInvariants unconditionally, so this exercises the exact
	// boot path a real deployment would hit after 100 mixed CA-rotation
	// operations against a database that also carries a quarantined row.
	if err := s.Close(); err != nil {
		t.Fatalf("seed=%d: Close: %v", seed, err)
	}
	reopened, err := Open(dbPath)
	if err != nil {
		t.Fatalf("seed=%d: reopen: %v", seed, err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	if err := reopened.checkInvariants(ctx); err != nil {
		t.Fatalf("seed=%d: checkInvariants after reopen: %v", seed, err)
	}
}

// TestDelete_PreviousDropped exercises the same P2 drop through Delete
// rather than Renew.
func TestDelete_PreviousDropped(t *testing.T) {
	s := openTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	setupCA(t, s, time.Now().AddDate(5, 0, 0))

	previousID := insertPreviousCA(t, s, "previous-delete")

	active := fixtureCert("delete-drop.example.local", StatusActive, "2030-01-01T00:00:00Z")
	active.CAID = &previousID
	activeID := mustInsertCert(t, s, active)

	dropped, err := s.Delete(ctx, activeID, "delete-drop.example.local")
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if !dropped {
		t.Fatal("Delete's dropped = false, want true (the previous CA's only active row was just deleted)")
	}
	if got, err := s.GetPreviousCA(ctx); err != nil || got != nil {
		t.Fatalf("GetPreviousCA after drop = (%+v, %v), want (nil, nil)", got, err)
	}

	if err := s.checkInvariants(ctx); err != nil {
		t.Fatalf("checkInvariants: %v", err)
	}
}

// -- ReplaceCA progress hook (owner feedback: a blanket re-issue of dozens
// of certs gave no sign anything was happening) --

// newProgressFixture builds a store with a current CA signing n active
// certs, ready for a reissue-only ReplaceCA call (no previous CA, so
// previousStale is never required).
func newProgressFixture(t *testing.T, n int) *Store {
	t.Helper()
	s := openTestStore(t)
	s.genCA = throwawayGenCA
	ctx := context.Background()
	setupCA(t, s, time.Now().AddDate(5, 0, 0))

	current, err := s.GetCurrentCA(ctx)
	if err != nil {
		t.Fatalf("GetCurrentCA: %v", err)
	}
	curCert, err := ParseCert([]byte(current.CertPEM))
	if err != nil {
		t.Fatalf("ParseCert: %v", err)
	}
	curKey, err := ParseKey([]byte(current.KeyPEM))
	if err != nil {
		t.Fatalf("ParseKey: %v", err)
	}
	for i := 0; i < n; i++ {
		issueActiveUnder(t, s, curCert, curKey, current.ID, fmt.Sprintf("progress-%d.example.local", i))
	}
	return s
}

// TestReplaceCA_Progress_Reissue asserts the progress hook's event sequence
// for a reissue replace: one ReplacePhaseCAKey event, then one
// ReplacePhaseLeafKeys event per re-issued leaf with Done running 1..n and
// Total==n throughout, then one ReplacePhaseSaving event -- and nothing
// after that (the hook is never called again once the transaction starts).
// Total==n is carried on every event, including ca-key and saving, not just
// leaf-keys (this follow-up's own assertion: the client needs the overall
// size from the first event).
func TestReplaceCA_Progress_Reissue(t *testing.T) {
	t.Parallel()
	const n = 3
	s := newProgressFixture(t, n)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var events []ReplaceProgress
	_, err := s.ReplaceCA(ctx, "Progress Reissue CA", "reissue", nil, 365, 30,
		WithReplaceProgress(func(ev ReplaceProgress) { events = append(events, ev) }))
	if err != nil {
		t.Fatalf("ReplaceCA: %v", err)
	}

	if len(events) != n+2 {
		t.Fatalf("got %d events, want %d (ca-key + %d leaf-keys + saving): %+v", len(events), n+2, n, events)
	}
	if events[0] != (ReplaceProgress{Phase: ReplacePhaseCAKey, Total: n}) {
		t.Fatalf("events[0] = %+v, want {Phase: ca-key, Total: %d}", events[0], n)
	}
	for i := 0; i < n; i++ {
		want := ReplaceProgress{Phase: ReplacePhaseLeafKeys, Done: i + 1, Total: n}
		if events[i+1] != want {
			t.Fatalf("events[%d] = %+v, want %+v", i+1, events[i+1], want)
		}
	}
	if events[n+1] != (ReplaceProgress{Phase: ReplacePhaseSaving, Total: n}) {
		t.Fatalf("events[%d] = %+v, want {Phase: saving, Total: %d}", n+1, events[n+1], n)
	}
}

// TestReplaceCA_Progress_KeepFiresNoLeafEvents covers existing="keep": the
// ca-key and saving events still fire (crypto for the new CA always runs),
// but no leaf is re-issued, so no ReplacePhaseLeafKeys event fires and Total
// is 0 on both events.
func TestReplaceCA_Progress_KeepFiresNoLeafEvents(t *testing.T) {
	t.Parallel()
	s := newProgressFixture(t, 2)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var events []ReplaceProgress
	_, err := s.ReplaceCA(ctx, "Progress Keep CA", "keep", nil, 365, 30,
		WithReplaceProgress(func(ev ReplaceProgress) { events = append(events, ev) }))
	if err != nil {
		t.Fatalf("ReplaceCA: %v", err)
	}
	want := []ReplaceProgress{{Phase: ReplacePhaseCAKey, Total: 0}, {Phase: ReplacePhaseSaving, Total: 0}}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("events = %+v, want %+v", events, want)
	}
}

// TestReplaceCA_Progress_NoEventsOnValidationError: a validation failure
// (bad `existing` enum) must return its normal error with the progress hook
// never called even once -- crypto, and the events tied to it, only begin
// after validation has fully passed.
func TestReplaceCA_Progress_NoEventsOnValidationError(t *testing.T) {
	t.Parallel()
	s := newProgressFixture(t, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var events []ReplaceProgress
	_, err := s.ReplaceCA(ctx, "Progress Bad Enum CA", "bogus", nil, 365, 30,
		WithReplaceProgress(func(ev ReplaceProgress) { events = append(events, ev) }))
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("ReplaceCA(bad existing) error = %v, want ErrValidation", err)
	}
	if len(events) != 0 {
		t.Fatalf("events = %+v, want none for a validation failure", events)
	}
}

// TestReplaceCA_ContextCancelledBetweenLeafKeys: canceling ctx from inside
// the progress hook, after some leaf keys have already been generated, must
// make ReplaceCA return the context error with nothing written -- the
// transaction never opens (R2: all crypto, including the leaf loop, runs
// before WithTx). "Nothing written" is checked the same way
// TestReplace_Atomicity checks it: a full column-for-column dump of both
// tables, identical before and after.
func TestReplaceCA_ContextCancelledBetweenLeafKeys(t *testing.T) {
	t.Parallel()
	const n = 4
	const cancelAfter = 2
	s := newProgressFixture(t, n)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cancelCtx, cancelNow := context.WithCancel(ctx)
	defer cancelNow()

	beforeCA := dumpTable(t, s.db, "ca")
	beforeCerts := dumpTable(t, s.db, "certs")

	var leafEvents int
	_, err := s.ReplaceCA(cancelCtx, "Progress Cancel CA", "reissue", nil, 365, 30,
		WithReplaceProgress(func(ev ReplaceProgress) {
			if ev.Phase != ReplacePhaseLeafKeys {
				return
			}
			leafEvents++
			if ev.Done == cancelAfter {
				cancelNow()
			}
		}))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ReplaceCA error = %v, want context.Canceled", err)
	}
	if leafEvents != cancelAfter {
		t.Fatalf("leaf-key events = %d, want exactly %d (the loop must stop right after cancellation)", leafEvents, cancelAfter)
	}

	afterCA := dumpTable(t, s.db, "ca")
	afterCerts := dumpTable(t, s.db, "certs")
	if !reflect.DeepEqual(beforeCA, afterCA) {
		t.Errorf("ca table changed after context cancellation:\nbefore=%v\nafter=%v", beforeCA, afterCA)
	}
	if !reflect.DeepEqual(beforeCerts, afterCerts) {
		t.Errorf("certs table changed after context cancellation:\nbefore=%v\nafter=%v", beforeCerts, afterCerts)
	}
}
