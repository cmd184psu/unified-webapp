package certmachine

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenCreatesFileAndSetsSchemaVersion(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "sub", "certmachine.db")

	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("db file was not created: %v", err)
	}

	var version int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatalf("read user_version: %v", err)
	}
	if version != schemaVersion {
		t.Errorf("user_version = %d, want %d", version, schemaVersion)
	}
}

func TestOpenIsIdempotentOnReopen(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "certmachine.db")

	s1, err := Open(dbPath)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	if _, err := s1.db.Exec(`INSERT INTO ca(id, cert_pem, key_pem, subject, serial, not_before, not_after, fingerprint, created)
		VALUES (1, 'CERT', 'KEY', 'CN=test', '1', '2024-01-01T00:00:00Z', '2034-01-01T00:00:00Z', 'FP', '2024-01-01T00:00:00Z')`); err != nil {
		t.Fatalf("insert ca row: %v", err)
	}
	if err := s1.Close(); err != nil {
		t.Fatalf("close first store: %v", err)
	}

	s2, err := Open(dbPath)
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	defer s2.Close()

	var version int
	if err := s2.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatalf("read user_version: %v", err)
	}
	if version != schemaVersion {
		t.Errorf("user_version after reopen = %d, want %d", version, schemaVersion)
	}

	var subject string
	if err := s2.db.QueryRow("SELECT subject FROM ca WHERE id = 1").Scan(&subject); err != nil {
		t.Fatalf("row did not survive reopen: %v", err)
	}
	if subject != "CN=test" {
		t.Errorf("subject = %q, want CN=test", subject)
	}
}

func TestDSNContainsTxlockImmediateNotJournalMode(t *testing.T) {
	got := dsn("/tmp/certmachine.db")
	if !strings.Contains(got, "_txlock=immediate") {
		t.Errorf("dsn %q does not contain _txlock=immediate", got)
	}
	if strings.Contains(got, "journal_mode") {
		t.Errorf("dsn %q sets journal_mode, which must be left at the default", got)
	}
}

func TestOpenRejectsUncreatableDirectory(t *testing.T) {
	// A regular file in place of the parent directory makes MkdirAll fail.
	base := t.TempDir()
	blocker := filepath.Join(base, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatalf("write blocker file: %v", err)
	}

	if _, err := Open(filepath.Join(blocker, "sub", "certmachine.db")); err == nil {
		t.Fatal("expected Open to fail when the data dir cannot be created")
	}
}

// -- Slice 2: store reads, writes, and lifecycle transitions --

func openTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "certmachine.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func strPtr(v string) *string { return &v }

func fixtureCert(fqdn, status, notAfter string) Cert {
	var notAfterPtr, notBeforePtr, serialPtr, fpPtr *string
	if notAfter != "" {
		notAfterPtr = strPtr(notAfter)
		notBeforePtr = strPtr("2024-01-01T00:00:00Z")
		serialPtr = strPtr(fqdn + "-serial")
		fpPtr = strPtr(fqdn + "-fingerprint")
	}
	return Cert{
		FQDN:        fqdn,
		Serial:      serialPtr,
		NotBefore:   notBeforePtr,
		NotAfter:    notAfterPtr,
		SANs:        SANs{DNS: []string{fqdn}, IP: []string{}},
		Fingerprint: fpPtr,
		Status:      status,
		CertPEM:     strPtr("CERT-" + fqdn),
		KeyPEM:      strPtr("KEY-" + fqdn),
		Created:     "2024-01-01T00:00:00Z",
	}
}

// insertCert runs InsertCert inside a WithTx and returns whatever error the
// transaction produced (nil on success), without failing the test -- callers
// assert on the returned error themselves.
func insertCert(s *Store, c Cert) (int64, error) {
	var id int64
	err := s.WithTx(context.Background(), func(tx *sql.Tx) error {
		var err error
		id, err = s.InsertCert(context.Background(), tx, c)
		return err
	})
	return id, err
}

func mustInsertCert(t *testing.T, s *Store, c Cert) int64 {
	t.Helper()
	id, err := insertCert(s, c)
	if err != nil {
		t.Fatalf("insert cert %s: %v", c.FQDN, err)
	}
	return id
}

func TestInsertCertDuplicateActiveThenSucceedsAfterArchive(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	mustInsertCert(t, s, fixtureCert("a.example.local", StatusActive, "2030-01-01T00:00:00Z"))

	if _, err := insertCert(s, fixtureCert("a.example.local", StatusActive, "2031-01-01T00:00:00Z")); !errors.Is(err, ErrDuplicateActive) {
		t.Fatalf("second active insert error = %v, want ErrDuplicateActive", err)
	}

	// The predecessor must remain untouched by the failed attempt.
	certs, err := s.ListCerts(ctx)
	if err != nil {
		t.Fatalf("ListCerts: %v", err)
	}
	if len(certs) != 1 {
		t.Fatalf("len(certs) = %d, want 1 (failed insert must write nothing)", len(certs))
	}

	if err := s.WithTx(ctx, func(tx *sql.Tx) error {
		return s.ArchiveAllForFQDN(ctx, tx, "a.example.local")
	}); err != nil {
		t.Fatalf("ArchiveAllForFQDN: %v", err)
	}

	if _, err := insertCert(s, fixtureCert("a.example.local", StatusActive, "2031-01-01T00:00:00Z")); err != nil {
		t.Fatalf("insert after archive: %v", err)
	}

	certs, err = s.ListCerts(ctx)
	if err != nil {
		t.Fatalf("ListCerts: %v", err)
	}
	if len(certs) != 2 {
		t.Fatalf("len(certs) = %d, want 2", len(certs))
	}
}

// The one-active-per-fqdn invariant has to be case-insensitive, because the two
// writers disagree on case by design: the UI lowercases what the operator types
// (NormalizeFQDN), while the importer stores a legacy CN's bytes verbatim
// (FR-3). A case-sensitive invariant therefore lets "Mixed.Example.Local" and
// "mixed.example.local" both be active -- two live certificates for one host,
// which is the exact state the invariant exists to prevent, and which haproxy
// would resolve by arbitrary file order.
//
// Stored bytes stay verbatim throughout: the collation changes comparison, not
// content.
func TestActiveInvariantIsCaseInsensitive(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	const imported = "Mixed.Example.Local"
	const normalized = "mixed.example.local"
	mustInsertCert(t, s, fixtureCert(imported, StatusActive, "2030-01-01T00:00:00Z"))

	if _, err := insertCert(s, fixtureCert(normalized, StatusActive, "2031-01-01T00:00:00Z")); !errors.Is(err, ErrDuplicateActive) {
		t.Fatalf("insert of a case-variant active = %v, want ErrDuplicateActive", err)
	}

	// ArchiveAllForFQDN must clear the same set the pre-check refuses against,
	// or a Renew arriving with the other casing archives nothing and then
	// collides with the row it meant to supersede.
	if err := s.WithTx(ctx, func(tx *sql.Tx) error {
		return s.ArchiveAllForFQDN(ctx, tx, normalized)
	}); err != nil {
		t.Fatalf("ArchiveAllForFQDN(%q): %v", normalized, err)
	}

	certs, err := s.ListCerts(ctx)
	if err != nil {
		t.Fatalf("ListCerts: %v", err)
	}
	if len(certs) != 1 {
		t.Fatalf("len(certs) = %d, want 1", len(certs))
	}
	if certs[0].Status != StatusArchived {
		t.Errorf("status after case-variant archive = %q, want archived", certs[0].Status)
	}
	if certs[0].FQDN != imported {
		t.Errorf("stored fqdn = %q, want the imported bytes %q verbatim", certs[0].FQDN, imported)
	}

	if _, err := insertCert(s, fixtureCert(normalized, StatusActive, "2031-01-01T00:00:00Z")); err != nil {
		t.Fatalf("insert after case-variant archive: %v", err)
	}
}

func TestInsertCertDuplicateImportedFromNullsCoexist(t *testing.T) {
	s := openTestStore(t)

	first := fixtureCert("b.example.local", StatusArchived, "2030-01-01T00:00:00Z")
	first.ImportedFrom = strPtr("legacy/b")
	mustInsertCert(t, s, first)

	dup := fixtureCert("c.example.local", StatusArchived, "2030-01-01T00:00:00Z")
	dup.ImportedFrom = strPtr("legacy/b")
	if _, err := insertCert(s, dup); !errors.Is(err, ErrDuplicateImport) {
		t.Fatalf("duplicate imported_from error = %v, want ErrDuplicateImport", err)
	}

	// Two NULL imported_from rows must coexist.
	mustInsertCert(t, s, fixtureCert("d.example.local", StatusArchived, "2030-01-01T00:00:00Z"))
	mustInsertCert(t, s, fixtureCert("e.example.local", StatusArchived, "2030-01-01T00:00:00Z"))

	certs, err := s.ListCerts(context.Background())
	if err != nil {
		t.Fatalf("ListCerts: %v", err)
	}
	if len(certs) != 3 {
		t.Fatalf("len(certs) = %d, want 3 (one imported_from row plus two NULLs; the duplicate must not have written)", len(certs))
	}
}

func TestInsertCertNullSerialsCoexist(t *testing.T) {
	s := openTestStore(t)

	c1 := fixtureCert("f.example.local", StatusQuarantined, "")
	c1.QuarantineReason = strPtr("unparseable")
	c2 := fixtureCert("g.example.local", StatusQuarantined, "")
	c2.QuarantineReason = strPtr("unparseable")

	mustInsertCert(t, s, c1)
	mustInsertCert(t, s, c2)

	certs, err := s.ListCerts(context.Background())
	if err != nil {
		t.Fatalf("ListCerts: %v", err)
	}
	if len(certs) != 2 {
		t.Fatalf("len(certs) = %d, want 2 (two NULL-serial rows must coexist)", len(certs))
	}
	for _, c := range certs {
		if c.Serial != nil {
			t.Errorf("cert %d serial = %q, want nil", c.ID, *c.Serial)
		}
	}
}

func TestFindBySerialDoesNotMatchNullSerial(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	c := fixtureCert("h.example.local", StatusQuarantined, "")
	c.QuarantineReason = strPtr("unparseable")
	mustInsertCert(t, s, c)

	// Ground truth (Appendix C-5): WHERE serial = '' never matches a NULL
	// serial column -- SQL string comparison against NULL is never true.
	got, err := s.FindBySerial(ctx, "")
	if err != nil {
		t.Fatalf("FindBySerial: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("FindBySerial(\"\") returned %d rows, want 0", len(got))
	}

	active := fixtureCert("i.example.local", StatusActive, "2030-01-01T00:00:00Z")
	mustInsertCert(t, s, active)
	got, err = s.FindBySerial(ctx, "i.example.local-serial")
	if err != nil {
		t.Fatalf("FindBySerial: %v", err)
	}
	if len(got) != 1 || got[0].FQDN != "i.example.local" {
		t.Fatalf("FindBySerial(matching) = %+v, want one row for i.example.local", got)
	}
}

func TestFindByImportedFromNilWhenAbsent(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	got, err := s.FindByImportedFrom(ctx, "legacy/nope")
	if err != nil {
		t.Fatalf("FindByImportedFrom: %v", err)
	}
	if got != nil {
		t.Fatalf("FindByImportedFrom(absent) = %+v, want nil, nil", got)
	}

	c := fixtureCert("j.example.local", StatusArchived, "2030-01-01T00:00:00Z")
	c.ImportedFrom = strPtr("legacy/j")
	mustInsertCert(t, s, c)

	got, err = s.FindByImportedFrom(ctx, "legacy/j")
	if err != nil {
		t.Fatalf("FindByImportedFrom: %v", err)
	}
	if got == nil || got.FQDN != "j.example.local" {
		t.Fatalf("FindByImportedFrom(present) = %+v, want row for j.example.local", got)
	}
}

func TestListCertsProjectsNeitherPEMOrderedByNotAfter(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	mustInsertCert(t, s, fixtureCert("k2033.example.local", StatusActive, "2033-06-01T00:00:00Z"))
	mustInsertCert(t, s, fixtureCert("k2025.example.local", StatusActive, "2025-06-01T00:00:00Z"))
	mustInsertCert(t, s, fixtureCert("k2030.example.local", StatusActive, "2030-06-01T00:00:00Z"))

	certs, err := s.ListCerts(ctx)
	if err != nil {
		t.Fatalf("ListCerts: %v", err)
	}
	if len(certs) != 3 {
		t.Fatalf("len(certs) = %d, want 3", len(certs))
	}
	wantOrder := []string{"k2025.example.local", "k2030.example.local", "k2033.example.local"}
	for i, want := range wantOrder {
		if certs[i].FQDN != want {
			t.Errorf("certs[%d].FQDN = %q, want %q (ORDER BY not_after must be chronological)", i, certs[i].FQDN, want)
		}
		if certs[i].CertPEM != nil {
			t.Errorf("certs[%d].CertPEM = %v, want nil (ListCerts projects no PEM)", i, certs[i].CertPEM)
		}
		if certs[i].KeyPEM != nil {
			t.Errorf("certs[%d].KeyPEM = %v, want nil", i, certs[i].KeyPEM)
		}
	}
}

func TestGetCertIncludesCertPEMButNotKeyPEM(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	id := mustInsertCert(t, s, fixtureCert("l.example.local", StatusActive, "2030-01-01T00:00:00Z"))

	c, err := s.GetCert(ctx, id)
	if err != nil {
		t.Fatalf("GetCert: %v", err)
	}
	if c.CertPEM == nil || *c.CertPEM != "CERT-l.example.local" {
		t.Errorf("CertPEM = %v, want CERT-l.example.local", c.CertPEM)
	}
	if c.KeyPEM != nil {
		t.Errorf("KeyPEM = %v, want nil (GetCert must never expose key_pem)", c.KeyPEM)
	}
}

func TestGetCertNotFound(t *testing.T) {
	s := openTestStore(t)

	if _, err := s.GetCert(context.Background(), 999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetCert(missing) error = %v, want ErrNotFound", err)
	}
}

func TestGetCertWithKeyIncludesKeyPEM(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	id := mustInsertCert(t, s, fixtureCert("m.example.local", StatusActive, "2030-01-01T00:00:00Z"))

	c, err := s.getCertWithKey(ctx, id)
	if err != nil {
		t.Fatalf("getCertWithKey: %v", err)
	}
	if c.CertPEM == nil || *c.CertPEM != "CERT-m.example.local" {
		t.Errorf("CertPEM = %v, want CERT-m.example.local", c.CertPEM)
	}
	if c.KeyPEM == nil || *c.KeyPEM != "KEY-m.example.local" {
		t.Errorf("KeyPEM = %v, want KEY-m.example.local", c.KeyPEM)
	}
}

func TestWithTxRollsBackOnError(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	sentinel := errors.New("boom")
	err := s.WithTx(ctx, func(tx *sql.Tx) error {
		if _, err := s.InsertCert(ctx, tx, fixtureCert("n.example.local", StatusActive, "2030-01-01T00:00:00Z")); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("WithTx error = %v, want sentinel", err)
	}

	n, err := s.CountCerts(ctx)
	if err != nil {
		t.Fatalf("CountCerts: %v", err)
	}
	if n != 0 {
		t.Fatalf("CountCerts = %d, want 0 (a failing closure must write nothing)", n)
	}
}

func TestArchiveAllForFQDNThenRenewInsertOneTransaction(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	mustInsertCert(t, s, fixtureCert("o.example.local", StatusActive, "2030-01-01T00:00:00Z"))

	err := s.WithTx(ctx, func(tx *sql.Tx) error {
		if err := s.ArchiveAllForFQDN(ctx, tx, "o.example.local"); err != nil {
			return err
		}
		_, err := s.InsertCert(ctx, tx, fixtureCert("o.example.local", StatusActive, "2032-01-01T00:00:00Z"))
		return err
	})
	if err != nil {
		t.Fatalf("renew transaction: %v", err)
	}

	certs, err := s.ListCerts(ctx)
	if err != nil {
		t.Fatalf("ListCerts: %v", err)
	}
	var active, archived int
	for _, c := range certs {
		switch c.Status {
		case StatusActive:
			active++
		case StatusArchived:
			archived++
		}
	}
	if active != 1 || archived != 1 {
		t.Fatalf("active=%d archived=%d, want 1 and 1", active, archived)
	}
}

func TestDeleteCert(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	id := mustInsertCert(t, s, fixtureCert("p.example.local", StatusActive, "2030-01-01T00:00:00Z"))

	if err := s.DeleteCert(ctx, id); err != nil {
		t.Fatalf("DeleteCert: %v", err)
	}
	if _, err := s.GetCert(ctx, id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetCert after delete error = %v, want ErrNotFound", err)
	}
	if err := s.DeleteCert(ctx, id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("DeleteCert(already deleted) error = %v, want ErrNotFound", err)
	}
}

func TestCountCerts(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	n, err := s.CountCerts(ctx)
	if err != nil {
		t.Fatalf("CountCerts: %v", err)
	}
	if n != 0 {
		t.Fatalf("CountCerts (empty) = %d, want 0", n)
	}

	mustInsertCert(t, s, fixtureCert("q.example.local", StatusActive, "2030-01-01T00:00:00Z"))
	mustInsertCert(t, s, fixtureCert("r.example.local", StatusActive, "2030-01-01T00:00:00Z"))

	n, err = s.CountCerts(ctx)
	if err != nil {
		t.Fatalf("CountCerts: %v", err)
	}
	if n != 2 {
		t.Fatalf("CountCerts = %d, want 2", n)
	}
}

func fixtureCA(subject string) CA {
	return CA{
		CertPEM:     "CA-CERT-" + subject,
		KeyPEM:      "CA-KEY-" + subject,
		Subject:     subject,
		Serial:      "1",
		NotBefore:   "2024-01-01T00:00:00Z",
		NotAfter:    "2044-01-01T00:00:00Z",
		Fingerprint: "FP-" + subject,
		Created:     "2024-01-01T00:00:00Z",
	}
}

func TestInsertCASecondReturnsErrCAExistsFirstUnchanged(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	if err := s.WithTx(ctx, func(tx *sql.Tx) error {
		return s.InsertCA(ctx, tx, fixtureCA("first"))
	}); err != nil {
		t.Fatalf("first InsertCA: %v", err)
	}

	err := s.WithTx(ctx, func(tx *sql.Tx) error {
		return s.InsertCA(ctx, tx, fixtureCA("second"))
	})
	if !errors.Is(err, ErrCAExists) {
		t.Fatalf("second InsertCA error = %v, want ErrCAExists", err)
	}

	ca, err := s.GetCA(ctx)
	if err != nil {
		t.Fatalf("GetCA: %v", err)
	}
	if ca.Subject != "first" || ca.CertPEM != "CA-CERT-first" {
		t.Fatalf("CA after rejected second insert = %+v, want unchanged first CA", ca)
	}
}

func TestGetCANotFound(t *testing.T) {
	s := openTestStore(t)

	if _, err := s.GetCA(context.Background()); !errors.Is(err, ErrCANotFound) {
		t.Fatalf("GetCA(empty) error = %v, want ErrCANotFound", err)
	}
}
