package certmachine

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// setupCA inserts a throwaway (2048-bit, cheap) CA directly into the store,
// bypassing GenerateCA's real 4096-bit keygen -- every test below that needs
// "an initialized CA" uses this except the one exercising InitCA's real
// production path.
func setupCA(t *testing.T, s *Store, notAfter time.Time) {
	t.Helper()
	caCert, caKey, caCertPEM := newThrowawayCA(t, notAfter)
	caKeyPEM := encodeKeyPEM(caKey)

	ca := CA{
		CertPEM:     string(caCertPEM),
		KeyPEM:      string(caKeyPEM),
		Subject:     caCert.Subject.CommonName,
		Serial:      SerialString(caCert.SerialNumber),
		NotBefore:   caCert.NotBefore.UTC().Format(time.RFC3339),
		NotAfter:    caCert.NotAfter.UTC().Format(time.RFC3339),
		Fingerprint: Fingerprint(caCert.Raw),
		Created:     time.Now().UTC().Format(time.RFC3339),
	}
	ctx := context.Background()
	if err := s.WithTx(ctx, func(tx *sql.Tx) error {
		return s.InsertCA(ctx, tx, ca)
	}); err != nil {
		t.Fatalf("setup ca: %v", err)
	}
}

func TestInitCACreatesRootWhenNoneExists(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	if err := s.InitCA(ctx, ""); err != nil {
		t.Fatalf("InitCA: %v", err)
	}

	ca, err := s.GetCA(ctx)
	if err != nil {
		t.Fatalf("GetCA: %v", err)
	}
	if ca.Subject != "CertMachine Root CA" {
		t.Fatalf("ca.Subject = %q, want CertMachine Root CA", ca.Subject)
	}
	if ca.Fingerprint == "" {
		t.Fatal("ca.Fingerprint is empty")
	}
}

func TestInitCATwiceReturnsErrCAExists(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	setupCA(t, s, time.Now().AddDate(10, 0, 0))

	if err := s.InitCA(ctx, ""); !errors.Is(err, ErrCAExists) {
		t.Fatalf("second InitCA error = %v, want ErrCAExists", err)
	}
}

func TestInitCAWithPendingImportRefuses(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	dir := t.TempDir()
	_, _, certPEM := newThrowawayCA(t, time.Now().AddDate(10, 0, 0))
	if err := os.WriteFile(filepath.Join(dir, legacyRootCertFilename), certPEM, 0o600); err != nil {
		t.Fatalf("write legacy root: %v", err)
	}

	err := s.InitCA(ctx, dir)
	if !errors.Is(err, ErrImportPending) {
		t.Fatalf("InitCA error = %v, want ErrImportPending", err)
	}
	if !strings.Contains(err.Error(), "DELETE FROM ca") {
		t.Fatalf("InitCA error = %q, want it to quote the manual remedy", err.Error())
	}

	if _, err := s.GetCA(ctx); !errors.Is(err, ErrCANotFound) {
		t.Fatalf("GetCA after refused InitCA error = %v, want ErrCANotFound (no CA written)", err)
	}
}

func TestInitCAWithPendingImportDoesNotBlockOnceCertsExist(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	dir := t.TempDir()
	_, _, certPEM := newThrowawayCA(t, time.Now().AddDate(10, 0, 0))
	if err := os.WriteFile(filepath.Join(dir, legacyRootCertFilename), certPEM, 0o600); err != nil {
		t.Fatalf("write legacy root: %v", err)
	}
	// CountCerts() == 0 is one of the two ErrImportPending conditions; with
	// a cert already present (e.g. an import already ran), InitCA proceeds.
	mustInsertCert(t, s, fixtureCert("already.example.local", StatusActive, "2030-01-01T00:00:00Z"))

	if err := s.InitCA(ctx, dir); err != nil {
		t.Fatalf("InitCA with certs already present: %v", err)
	}
}

func TestGenerateInsertsActiveCert(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	setupCA(t, s, time.Now().AddDate(5, 0, 0))

	req, err := ValidateRequest("gen.example.local", []string{"alt.example.local"}, []string{"10.0.0.1"})
	if err != nil {
		t.Fatalf("ValidateRequest: %v", err)
	}

	result, err := s.Generate(ctx, req, 365, 30)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if result.Cert.ID == 0 {
		t.Fatal("result.Cert.ID is zero")
	}
	if result.Cert.Status != StatusActive {
		t.Fatalf("result.Cert.Status = %q, want active", result.Cert.Status)
	}
	if result.Cert.Serial == nil || *result.Cert.Serial == "" {
		t.Fatal("result.Cert.Serial is nil/empty")
	}
	if len(result.Cert.SANs.DNS) != 2 || result.Cert.SANs.DNS[0] != "gen.example.local" {
		t.Fatalf("result.Cert.SANs.DNS = %v, want [gen.example.local alt.example.local]", result.Cert.SANs.DNS)
	}
	if len(result.Cert.SANs.IP) != 1 || result.Cert.SANs.IP[0] != "10.0.0.1" {
		t.Fatalf("result.Cert.SANs.IP = %v, want [10.0.0.1]", result.Cert.SANs.IP)
	}

	stored, err := s.GetCert(ctx, result.Cert.ID)
	if err != nil {
		t.Fatalf("GetCert: %v", err)
	}
	if stored.CertPEM == nil || *stored.CertPEM == "" {
		t.Fatal("stored cert_pem is empty")
	}
}

func TestGenerateDuplicateActiveFQDNWritesNoRow(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	setupCA(t, s, time.Now().AddDate(5, 0, 0))

	req, err := ValidateRequest("dup.example.local", nil, nil)
	if err != nil {
		t.Fatalf("ValidateRequest: %v", err)
	}
	if _, err := s.Generate(ctx, req, 365, 30); err != nil {
		t.Fatalf("first Generate: %v", err)
	}

	before, err := s.CountCerts(ctx)
	if err != nil {
		t.Fatalf("CountCerts: %v", err)
	}

	if _, err := s.Generate(ctx, req, 365, 30); !errors.Is(err, ErrDuplicateActive) {
		t.Fatalf("second Generate error = %v, want ErrDuplicateActive", err)
	}

	after, err := s.CountCerts(ctx)
	if err != nil {
		t.Fatalf("CountCerts: %v", err)
	}
	if after != before {
		t.Fatalf("CountCerts after failed Generate = %d, want unchanged %d", after, before)
	}
}

func TestGenerateRefusesWhenCANearingExpiry(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	setupCA(t, s, time.Now().Add(10*24*time.Hour)) // 10 days left, warn threshold below

	req, err := ValidateRequest("soon.example.local", nil, nil)
	if err != nil {
		t.Fatalf("ValidateRequest: %v", err)
	}

	_, err = s.Generate(ctx, req, 365, 30)
	if !errors.Is(err, ErrCAExpiringSoon) {
		t.Fatalf("Generate error = %v, want ErrCAExpiringSoon", err)
	}
	if !strings.Contains(err.Error(), "DELETE FROM ca") {
		t.Fatalf("Generate error = %q, want it to quote the manual remedy", err.Error())
	}

	n, err := s.CountCerts(ctx)
	if err != nil {
		t.Fatalf("CountCerts: %v", err)
	}
	if n != 0 {
		t.Fatalf("CountCerts after refused Generate = %d, want 0", n)
	}
}

func TestGenerateSurfacesValidityClamp(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	setupCA(t, s, time.Now().AddDate(0, 0, 60)) // 60 days left, well above a 30-day warn threshold

	req, err := ValidateRequest("clamp.example.local", nil, nil)
	if err != nil {
		t.Fatalf("ValidateRequest: %v", err)
	}

	result, err := s.Generate(ctx, req, 365, 30)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !result.Clamped {
		t.Fatal("result.Clamped = false, want true (365-day request under a 60-day CA)")
	}
	if !result.RequestedNotAfter.After(time.Now().AddDate(0, 0, 300)) {
		t.Fatalf("result.RequestedNotAfter = %v, want ~365 days out", result.RequestedNotAfter)
	}
}

func TestGenerateNoCAReturnsErrCANotFound(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	req, err := ValidateRequest("noca.example.local", nil, nil)
	if err != nil {
		t.Fatalf("ValidateRequest: %v", err)
	}
	if _, err := s.Generate(ctx, req, 365, 30); !errors.Is(err, ErrCANotFound) {
		t.Fatalf("Generate error = %v, want ErrCANotFound", err)
	}
}

func TestRenewArchivesPredecessorAndInsertsNewActive(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	setupCA(t, s, time.Now().AddDate(5, 0, 0))

	req, err := ValidateRequest("renew.example.local", []string{"alt.renew.example.local"}, nil)
	if err != nil {
		t.Fatalf("ValidateRequest: %v", err)
	}
	// Generate with a short validity and Renew with a much longer one, so
	// the new row's not_after is unambiguously later regardless of how much
	// wall-clock time elapses between the two calls (RFC3339 timestamps
	// carry only second precision, and this test runs both calls well
	// within the same second).
	first, err := s.Generate(ctx, req, 10, 30)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	second, err := s.Renew(ctx, first.Cert.ID, 400, 30)
	if err != nil {
		t.Fatalf("Renew: %v", err)
	}

	if second.Cert.ID == first.Cert.ID {
		t.Fatal("Renew returned the same row id as the predecessor")
	}
	if second.Cert.Status != StatusActive {
		t.Fatalf("second.Cert.Status = %q, want active", second.Cert.Status)
	}
	if len(second.Cert.SANs.DNS) != 2 || second.Cert.SANs.DNS[0] != "renew.example.local" || second.Cert.SANs.DNS[1] != "alt.renew.example.local" {
		t.Fatalf("second.Cert.SANs.DNS = %v, want the predecessor's SAN set preserved", second.Cert.SANs.DNS)
	}
	if first.Cert.KeyPEM == nil || second.Cert.KeyPEM == nil || *first.Cert.KeyPEM == *second.Cert.KeyPEM {
		t.Fatal("second.Cert.KeyPEM must differ from the predecessor's key")
	}

	certs, err := s.ListCerts(ctx)
	if err != nil {
		t.Fatalf("ListCerts: %v", err)
	}
	var active, archived int
	for _, c := range certs {
		if c.FQDN != "renew.example.local" {
			continue
		}
		switch c.Status {
		case StatusActive:
			active++
		case StatusArchived:
			archived++
		}
	}
	if active != 1 || archived != 1 {
		t.Fatalf("active=%d archived=%d for renew.example.local, want 1 and 1", active, archived)
	}

	predecessor, err := s.GetCert(ctx, first.Cert.ID)
	if err != nil {
		t.Fatalf("GetCert(predecessor): %v", err)
	}
	if predecessor.Status != StatusArchived {
		t.Fatalf("predecessor.Status = %q, want archived", predecessor.Status)
	}
	if predecessor.NotAfter == nil || second.Cert.NotAfter == nil || *predecessor.NotAfter >= *second.Cert.NotAfter {
		t.Fatalf("predecessor.NotAfter=%v, new.NotAfter=%v, want new later", predecessor.NotAfter, second.Cert.NotAfter)
	}
}

// An imported row keeps its legacy CN's case (FR-3) while everything arriving
// through the API is lowercased (NormalizeFQDN), so the two writers routinely
// disagree on the case of the same hostname. Generate and Renew must see them
// as one FQDN: otherwise an operator who types the host an imported cert
// already covers silently gets a second live certificate for it, and
// activeCertForFQDN misses the active row that makes renewing an archived
// sibling wrong.
func TestGenerateAndRenewTreatImportedFQDNCaseInsensitively(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	setupCA(t, s, time.Now().AddDate(5, 0, 0))

	const importedFQDN = "Mixed.Example.Local"
	imported := fixtureCert(importedFQDN, StatusActive, "2030-01-01T00:00:00Z")
	imported.ImportedFrom = strPtr("legacy/Mixed.Example.Local")
	importedID := mustInsertCert(t, s, imported)

	// An archived lowercase sibling: legal (the invariant covers active rows
	// only) and the setup activeCertForFQDN has to reason about.
	archivedSibling := fixtureCert("mixed.example.local", StatusArchived, "2029-01-01T00:00:00Z")
	archivedSibling.Serial = strPtr("archived-sibling-serial")
	archivedSibling.Fingerprint = strPtr("archived-sibling-fingerprint")
	siblingID := mustInsertCert(t, s, archivedSibling)

	// The operator types the name the imported cert already covers; the API
	// lowercases it. The collision must still be reported as a duplicate, and
	// the message must name the FQDN -- "a certificate is already active" with
	// no host named is unactionable in a list of hundreds.
	req, err := ValidateRequest(importedFQDN, nil, nil)
	if err != nil {
		t.Fatalf("ValidateRequest: %v", err)
	}
	if req.FQDN != "mixed.example.local" {
		t.Fatalf("ValidateRequest did not normalize case: req.FQDN = %q", req.FQDN)
	}
	_, err = s.Generate(ctx, req, 365, 30)
	if !errors.Is(err, ErrDuplicateActive) {
		t.Fatalf("Generate over a mixed-case active row = %v, want ErrDuplicateActive", err)
	}
	if !strings.Contains(err.Error(), "mixed.example.local") {
		t.Errorf("ErrDuplicateActive message names no fqdn: %v", err)
	}

	if _, err := s.Renew(ctx, siblingID, 365, 30); !errors.Is(err, ErrNewerActiveExists) {
		t.Fatalf("Renew(archived sibling) = %v, want ErrNewerActiveExists (the active row differs only in case)", err)
	}

	// Renewing the imported row itself is the supported move, and it must
	// archive that row rather than trip over its own invariant.
	renewed, err := s.Renew(ctx, importedID, 365, 30)
	if err != nil {
		t.Fatalf("Renew(imported row): %v", err)
	}
	predecessor, err := s.GetCert(ctx, importedID)
	if err != nil {
		t.Fatalf("GetCert(imported): %v", err)
	}
	if predecessor.Status != StatusArchived {
		t.Errorf("imported row status after renew = %q, want archived", predecessor.Status)
	}
	if predecessor.FQDN != importedFQDN {
		t.Errorf("imported fqdn = %q, want the legacy bytes %q verbatim", predecessor.FQDN, importedFQDN)
	}

	certs, err := s.ListCerts(ctx)
	if err != nil {
		t.Fatalf("ListCerts: %v", err)
	}
	var active []int64
	for _, c := range certs {
		if c.Status == StatusActive && strings.EqualFold(c.FQDN, importedFQDN) {
			active = append(active, c.ID)
		}
	}
	if len(active) != 1 || active[0] != renewed.Cert.ID {
		t.Fatalf("active rows for %s (any case) = %v, want only the renewal %d", importedFQDN, active, renewed.Cert.ID)
	}
}

func TestRenewOnArchivedRowWithNewerActiveReturnsErrNewerActiveExists(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	setupCA(t, s, time.Now().AddDate(5, 0, 0))

	req, err := ValidateRequest("conflict.example.local", nil, nil)
	if err != nil {
		t.Fatalf("ValidateRequest: %v", err)
	}
	first, err := s.Generate(ctx, req, 365, 30)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	second, err := s.Renew(ctx, first.Cert.ID, 365, 30)
	if err != nil {
		t.Fatalf("Renew (first -> second): %v", err)
	}

	// first.Cert.ID is now archived, and second.Cert.ID is the newer active row.
	_, err = s.Renew(ctx, first.Cert.ID, 365, 30)
	if !errors.Is(err, ErrNewerActiveExists) {
		t.Fatalf("Renew(archived, newer active present) error = %v, want ErrNewerActiveExists", err)
	}
	if !strings.Contains(err.Error(), "renew that one instead") {
		t.Fatalf("error = %q, want it to name the newer active row", err.Error())
	}
	_ = second
}

func TestRenewOnArchivedRowWithNoActiveSucceeds(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	setupCA(t, s, time.Now().AddDate(5, 0, 0))

	req, err := ValidateRequest("orphaned.example.local", nil, nil)
	if err != nil {
		t.Fatalf("ValidateRequest: %v", err)
	}
	first, err := s.Generate(ctx, req, 365, 30)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	second, err := s.Renew(ctx, first.Cert.ID, 365, 30)
	if err != nil {
		t.Fatalf("Renew (first -> second): %v", err)
	}

	// Remove the only active row, leaving first.Cert.ID archived with no
	// active row for the fqdn -- Renew on the archived row must now succeed.
	if err := s.Delete(ctx, second.Cert.ID, "orphaned.example.local"); err != nil {
		t.Fatalf("Delete(second): %v", err)
	}

	third, err := s.Renew(ctx, first.Cert.ID, 365, 30)
	if err != nil {
		t.Fatalf("Renew(archived, no active present) error = %v, want success", err)
	}
	if third.Cert.Status != StatusActive {
		t.Fatalf("third.Cert.Status = %q, want active", third.Cert.Status)
	}
}

func TestRenewQuarantinedReturnsErrQuarantined(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	setupCA(t, s, time.Now().AddDate(5, 0, 0))

	id := mustInsertCert(t, s, fixtureCert("quarantined.example.local", StatusQuarantined, ""))

	if _, err := s.Renew(ctx, id, 365, 30); !errors.Is(err, ErrQuarantined) {
		t.Fatalf("Renew(quarantined) error = %v, want ErrQuarantined", err)
	}
}

func TestRenewNotFound(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	setupCA(t, s, time.Now().AddDate(5, 0, 0))

	if _, err := s.Renew(ctx, 9999, 365, 30); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Renew(missing id) error = %v, want ErrNotFound", err)
	}
}

func TestRenewRefusesWhenCANearingExpiry(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	setupCA(t, s, time.Now().AddDate(5, 0, 0))

	req, err := ValidateRequest("renewexpiry.example.local", nil, nil)
	if err != nil {
		t.Fatalf("ValidateRequest: %v", err)
	}
	first, err := s.Generate(ctx, req, 365, 30)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	// Replace the CA with one that is now too close to expiry.
	if _, err := s.db.ExecContext(ctx, `DELETE FROM ca`); err != nil {
		t.Fatalf("delete ca: %v", err)
	}
	setupCA(t, s, time.Now().Add(10*24*time.Hour))

	if _, err := s.Renew(ctx, first.Cert.ID, 365, 30); !errors.Is(err, ErrCAExpiringSoon) {
		t.Fatalf("Renew error = %v, want ErrCAExpiringSoon", err)
	}
}

func TestRenewRollbackLeavesPredecessorActive(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	id := mustInsertCert(t, s, fixtureCert("rollback.example.local", StatusActive, "2030-01-01T00:00:00Z"))

	sentinel := errors.New("boom")
	err := s.WithTx(ctx, func(tx *sql.Tx) error {
		if err := s.ArchiveAllForFQDN(ctx, tx, "rollback.example.local"); err != nil {
			return err
		}
		// Simulate the insert half of Renew's transaction failing after the
		// archive has run.
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("WithTx error = %v, want sentinel", err)
	}

	got, err := s.GetCert(ctx, id)
	if err != nil {
		t.Fatalf("GetCert: %v", err)
	}
	if got.Status != StatusActive {
		t.Fatalf("predecessor.Status after rolled-back renew = %q, want active", got.Status)
	}
}

func TestDeleteRequiresMatchingConfirmCaseInsensitive(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	id := mustInsertCert(t, s, fixtureCert("delete.example.local", StatusActive, "2030-01-01T00:00:00Z"))

	if err := s.Delete(ctx, id, "wrong.example.local"); !errors.Is(err, ErrConfirmMismatch) {
		t.Fatalf("Delete(wrong confirm) error = %v, want ErrConfirmMismatch", err)
	}
	if _, err := s.GetCert(ctx, id); err != nil {
		t.Fatalf("GetCert after mismatched delete: %v, want row still present", err)
	}

	if err := s.Delete(ctx, id, "DELETE.EXAMPLE.LOCAL"); err != nil {
		t.Fatalf("Delete(case-insensitive confirm): %v", err)
	}
	if _, err := s.GetCert(ctx, id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetCert after delete error = %v, want ErrNotFound", err)
	}
}

func TestDeleteNotFound(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	if err := s.Delete(ctx, 9999, "whatever.example.local"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Delete(missing id) error = %v, want ErrNotFound", err)
	}
}

func TestDeleteQuarantinedRowSucceeds(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	id := mustInsertCert(t, s, fixtureCert("quarantined-delete.example.local", StatusQuarantined, ""))

	if err := s.Delete(ctx, id, "quarantined-delete.example.local"); err != nil {
		t.Fatalf("Delete(quarantined): %v", err)
	}
	if _, err := s.GetCert(ctx, id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetCert after delete error = %v, want ErrNotFound", err)
	}
}
