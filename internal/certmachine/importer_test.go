package certmachine

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"io/fs"
	"math/big"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// writeLegacyRootFixture writes a legacy-shaped rootCA.crt/rootCA.key pair
// into dir, mirroring reference/certmachine/main.go:74-93 (initCA) and
// :197-205 (writeCert/writeKey) field-for-field except for key size --
// RSA-2048 here for test speed, since ImportCA's contract does not depend on
// the CA's bit size and the one real RSA-4096 exercise lives in
// TestGenerateCAShape (pki_test.go).
func writeLegacyRootFixture(t *testing.T, dir string) (certPEM, keyPEM []byte, cert *x509.Certificate, key *rsa.PrivateKey) {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate legacy root key: %v", err)
	}
	tpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "CertMachine Root CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(10, 0, 0),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create legacy root cert: %v", err)
	}
	cert, err = x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse legacy root cert: %v", err)
	}

	certPEM = encodeCertPEM(der)
	keyPEM = encodeKeyPEM(key)

	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir legacy dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, legacyRootCertFilename), certPEM, 0o600); err != nil {
		t.Fatalf("write rootCA.crt: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, legacyRootKeyFilename), keyPEM, 0o600); err != nil {
		t.Fatalf("write rootCA.key: %v", err)
	}
	return certPEM, keyPEM, cert, key
}

// TestImportRootCAIsByteForByte is one of the three Driver-1 contract tests
// (FR-3): ca.cert_pem and ca.key_pem must be bytes.Equal to the source
// files, not merely equivalent after a re-parse/re-encode round trip.
func TestImportRootCAIsByteForByte(t *testing.T) {
	dir := t.TempDir()
	certPEM, keyPEM, _, _ := writeLegacyRootFixture(t, dir)

	s := openTestStore(t)
	ctx := context.Background()

	imported, err := s.ImportCA(ctx, dir)
	if err != nil {
		t.Fatalf("ImportCA: %v", err)
	}
	if !imported {
		t.Fatal("ImportCA imported = false, want true on first import")
	}

	ca, err := s.GetCA(ctx)
	if err != nil {
		t.Fatalf("GetCA: %v", err)
	}
	if !bytes.Equal([]byte(ca.CertPEM), certPEM) {
		t.Errorf("ca.cert_pem is not byte-for-byte equal to the source rootCA.crt\ngot:  %q\nwant: %q", ca.CertPEM, certPEM)
	}
	if !bytes.Equal([]byte(ca.KeyPEM), keyPEM) {
		t.Errorf("ca.key_pem is not byte-for-byte equal to the source rootCA.key\ngot:  %q\nwant: %q", ca.KeyPEM, keyPEM)
	}
	if ca.ImportedFrom == nil || *ca.ImportedFrom != legacyRootCertFilename {
		t.Errorf("ca.imported_from = %v, want %q", ca.ImportedFrom, legacyRootCertFilename)
	}
}

// TestGeneratedCertChainsToImportedRoot is the second Driver-1 contract
// test: a leaf generated against the imported root must verify against a
// CertPool built from the original ON-DISK rootCA.crt, not merely against
// whatever the store round-tripped.
func TestGeneratedCertChainsToImportedRoot(t *testing.T) {
	dir := t.TempDir()
	onDiskCertPEM, _, rootCert, rootKey := writeLegacyRootFixture(t, dir)

	s := openTestStore(t)
	ctx := context.Background()
	if _, err := s.ImportCA(ctx, dir); err != nil {
		t.Fatalf("ImportCA: %v", err)
	}

	req := CertRequest{FQDN: "chain.example.local"}
	result, err := GenerateLeaf(rootCert, rootKey, req, 365)
	if err != nil {
		t.Fatalf("GenerateLeaf: %v", err)
	}
	leaf, err := ParseCert(result.CertPEM)
	if err != nil {
		t.Fatalf("ParseCert(leaf): %v", err)
	}

	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(onDiskCertPEM) {
		t.Fatal("failed to load the original on-disk rootCA.crt into a CertPool")
	}
	if _, err := leaf.Verify(x509.VerifyOptions{Roots: pool, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}); err != nil {
		t.Errorf("generated leaf does not chain to the original on-disk root: %v", err)
	}
}

func TestImportCASkipsOnFingerprintMatch(t *testing.T) {
	dir := t.TempDir()
	writeLegacyRootFixture(t, dir)

	s := openTestStore(t)
	ctx := context.Background()

	imported1, err := s.ImportCA(ctx, dir)
	if err != nil || !imported1 {
		t.Fatalf("first ImportCA: imported=%v err=%v, want true, nil", imported1, err)
	}
	before, err := s.GetCA(ctx)
	if err != nil {
		t.Fatalf("GetCA after first import: %v", err)
	}

	imported2, err := s.ImportCA(ctx, dir)
	if err != nil {
		t.Fatalf("second ImportCA (re-run) returned error: %v", err)
	}
	if imported2 {
		t.Error("second ImportCA imported = true, want false (fingerprint match should skip)")
	}

	after, err := s.GetCA(ctx)
	if err != nil {
		t.Fatalf("GetCA after second import: %v", err)
	}
	if after.CertPEM != before.CertPEM || after.Fingerprint != before.Fingerprint {
		t.Error("CA row changed across a fingerprint-matching re-import, want unchanged")
	}
}

func TestImportCARefusesOnFingerprintMismatch(t *testing.T) {
	dirA := t.TempDir()
	writeLegacyRootFixture(t, dirA)
	dirB := t.TempDir()
	writeLegacyRootFixture(t, dirB)

	s := openTestStore(t)
	ctx := context.Background()

	if _, err := s.ImportCA(ctx, dirA); err != nil {
		t.Fatalf("ImportCA(dirA): %v", err)
	}
	before, err := s.GetCA(ctx)
	if err != nil {
		t.Fatalf("GetCA after first import: %v", err)
	}

	_, err = s.ImportCA(ctx, dirB)
	if !errors.Is(err, ErrCAFingerprintMismatch) {
		t.Fatalf("ImportCA(dirB) error = %v, want ErrCAFingerprintMismatch", err)
	}
	if err.Error() == "" {
		t.Fatal("mismatch error has empty message")
	}

	after, err := s.GetCA(ctx)
	if err != nil {
		t.Fatalf("GetCA after mismatch: %v", err)
	}
	if after.CertPEM != before.CertPEM {
		t.Error("CA row changed after a refused mismatched import, want unchanged")
	}
}

func TestImportCARefusesKeyMismatch(t *testing.T) {
	dir := t.TempDir()
	writeLegacyRootFixture(t, dir)

	// Overwrite rootCA.key with an unrelated key so the pair no longer
	// matches.
	otherKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate unrelated key: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, legacyRootKeyFilename), encodeKeyPEM(otherKey), 0o600); err != nil {
		t.Fatalf("overwrite rootCA.key: %v", err)
	}

	s := openTestStore(t)
	ctx := context.Background()

	_, err = s.ImportCA(ctx, dir)
	if !errors.Is(err, ErrCAKeyMismatch) {
		t.Fatalf("ImportCA error = %v, want ErrCAKeyMismatch", err)
	}

	if _, err := s.GetCA(ctx); !errors.Is(err, ErrCANotFound) {
		t.Errorf("GetCA after refused key-mismatch import = %v, want ErrCANotFound (nothing stored)", err)
	}
}

func TestImportCAMissingRootFiles(t *testing.T) {
	dir := t.TempDir()

	s := openTestStore(t)
	ctx := context.Background()

	if _, err := s.ImportCA(ctx, dir); err == nil {
		t.Fatal("ImportCA on a directory with no rootCA.crt/rootCA.key = nil error, want error")
	}
	if _, err := s.GetCA(ctx); !errors.Is(err, ErrCANotFound) {
		t.Errorf("GetCA after failed import = %v, want ErrCANotFound", err)
	}
}

// ---------------------------------------------------------------------
// Slice 7: Preview/Execute fixtures and tests.
// ---------------------------------------------------------------------

// leafOpts customizes writeLegacyLeafFixture. The zero value produces a
// normal, valid, one-year leaf.
type leafOpts struct {
	notAfter  time.Time       // defaults to +1y
	sans      []string        // extra DNS SANs beyond the CN
	key       *rsa.PrivateKey // reuse an existing key instead of generating one (perf fixture)
	signer    *x509.Certificate
	signerKey *rsa.PrivateKey // sign with a CA other than the legacy root -> chain mismatch
}

// writeLegacyLeafFixture writes a valid <legacyDir>/certs/<dirName>/{cert.pem,key.pem}
// pair, signed by rootCert/rootKey unless opts overrides the signer.
func writeLegacyLeafFixture(t *testing.T, legacyDir, dirName, fqdn string, rootCert *x509.Certificate, rootKey *rsa.PrivateKey, opts leafOpts) (certPEM, keyPEM []byte, cert *x509.Certificate) {
	t.Helper()

	leafDir := filepath.Join(legacyDir, "certs", dirName)
	if err := os.MkdirAll(leafDir, 0o700); err != nil {
		t.Fatalf("mkdir leaf dir %s: %v", leafDir, err)
	}

	key := opts.key
	if key == nil {
		var err error
		key, err = rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatalf("generate leaf key: %v", err)
		}
	}
	notAfter := opts.notAfter
	if notAfter.IsZero() {
		notAfter = time.Now().AddDate(1, 0, 0)
	}
	signer, signerKey := rootCert, rootKey
	if opts.signer != nil {
		signer, signerKey = opts.signer, opts.signerKey
	}

	tpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: fqdn},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     notAfter,
		DNSNames:     append([]string{fqdn}, opts.sans...),
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, signer, &key.PublicKey, signerKey)
	if err != nil {
		t.Fatalf("create leaf cert for %s: %v", dirName, err)
	}
	cert, err = x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse leaf cert for %s: %v", dirName, err)
	}

	certPEM = encodeCertPEM(der)
	keyPEM = encodeKeyPEM(key)
	if err := os.WriteFile(filepath.Join(leafDir, legacyLeafCertFilename), certPEM, 0o600); err != nil {
		t.Fatalf("write cert.pem for %s: %v", dirName, err)
	}
	if err := os.WriteFile(filepath.Join(leafDir, legacyLeafKeyFilename), keyPEM, 0o600); err != nil {
		t.Fatalf("write key.pem for %s: %v", dirName, err)
	}
	return certPEM, keyPEM, cert
}

// writeBrokenLeaf writes one of the five quarantine-cause fixtures directly,
// bypassing writeLegacyLeafFixture's "always valid" shape.
func writeBrokenLeaf(t *testing.T, legacyDir, dirName string, write func(leafDir string)) {
	t.Helper()
	leafDir := filepath.Join(legacyDir, "certs", dirName)
	if err := os.MkdirAll(leafDir, 0o700); err != nil {
		t.Fatalf("mkdir leaf dir %s: %v", leafDir, err)
	}
	write(leafDir)
}

// writeLegacyTree assembles a full legacy tree covering every scan()
// classification outcome in one place: a valid leaf, an expired leaf, a
// wildcard-CN leaf, a chain-mismatched leaf, the five quarantine causes, a
// copied directory sharing a serial with the valid leaf, and stray
// (non-managed) files in the legacy root. It returns the root cert/key and
// the directory.
func writeLegacyTree(t *testing.T) (dir string, rootCert *x509.Certificate, rootKey *rsa.PrivateKey) {
	t.Helper()
	dir = t.TempDir()
	_, _, rootCert, rootKey = writeLegacyRootFixture(t, dir)

	// Normal, valid leaf.
	writeLegacyLeafFixture(t, dir, "valid.example.local", "valid.example.local", rootCert, rootKey, leafOpts{})

	// Already-expired leaf: still a normal, importable (as "expired" in the
	// report) row -- never quarantined, never stored as a distinct status.
	writeLegacyLeafFixture(t, dir, "expired.example.local", "expired.example.local", rootCert, rootKey, leafOpts{
		notAfter: time.Now().Add(-24 * time.Hour),
	})

	// Wildcard CN.
	writeLegacyLeafFixture(t, dir, "wildcard.example.local", "*.example.local", rootCert, rootKey, leafOpts{})

	// Chain mismatch: valid, self-consistent leaf signed by an unrelated CA.
	otherRootCert, otherRootKey, _ := newThrowawayCA(t, time.Now().AddDate(5, 0, 0))
	writeLegacyLeafFixture(t, dir, "nochain.example.local", "nochain.example.local", otherRootCert, otherRootKey, leafOpts{
		signer: otherRootCert, signerKey: otherRootKey,
	})

	// Quarantine cause 1: cert.pem missing entirely (key.pem present).
	writeBrokenLeaf(t, dir, "missing-cert", func(leafDir string) {
		key, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(leafDir, legacyLeafKeyFilename), encodeKeyPEM(key), 0o600); err != nil {
			t.Fatal(err)
		}
	})

	// Quarantine cause 2: cert.pem present but garbage (PEM decodes to
	// nothing / ParseCertificate fails).
	writeBrokenLeaf(t, dir, "garbage-cert", func(leafDir string) {
		if err := os.WriteFile(filepath.Join(leafDir, legacyLeafCertFilename), []byte("not a certificate\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	})

	// Quarantine cause 3: key.pem missing.
	writeBrokenLeaf(t, dir, "missing-key", func(leafDir string) {
		key, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatal(err)
		}
		tpl := &x509.Certificate{
			SerialNumber: big.NewInt(time.Now().UnixNano()),
			Subject:      pkix.Name{CommonName: "missing-key.example.local"},
			NotBefore:    time.Now().Add(-time.Hour),
			NotAfter:     time.Now().AddDate(1, 0, 0),
			DNSNames:     []string{"missing-key.example.local"},
		}
		der, err := x509.CreateCertificate(rand.Reader, tpl, rootCert, &key.PublicKey, rootKey)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(leafDir, legacyLeafCertFilename), encodeCertPEM(der), 0o600); err != nil {
			t.Fatal(err)
		}
	})

	// Quarantine cause 4: key.pem present but unparseable.
	writeBrokenLeaf(t, dir, "garbage-key", func(leafDir string) {
		key, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatal(err)
		}
		tpl := &x509.Certificate{
			SerialNumber: big.NewInt(time.Now().UnixNano()),
			Subject:      pkix.Name{CommonName: "garbage-key.example.local"},
			NotBefore:    time.Now().Add(-time.Hour),
			NotAfter:     time.Now().AddDate(1, 0, 0),
			DNSNames:     []string{"garbage-key.example.local"},
		}
		der, err := x509.CreateCertificate(rand.Reader, tpl, rootCert, &key.PublicKey, rootKey)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(leafDir, legacyLeafCertFilename), encodeCertPEM(der), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(leafDir, legacyLeafKeyFilename), []byte("not a key\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	})

	// Quarantine cause 5: key.pem parses but does not pair with cert.pem.
	writeBrokenLeaf(t, dir, "mismatched-key", func(leafDir string) {
		certKey, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatal(err)
		}
		otherKey, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatal(err)
		}
		tpl := &x509.Certificate{
			SerialNumber: big.NewInt(time.Now().UnixNano()),
			Subject:      pkix.Name{CommonName: "mismatched-key.example.local"},
			NotBefore:    time.Now().Add(-time.Hour),
			NotAfter:     time.Now().AddDate(1, 0, 0),
			DNSNames:     []string{"mismatched-key.example.local"},
		}
		der, err := x509.CreateCertificate(rand.Reader, tpl, rootCert, &certKey.PublicKey, rootKey)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(leafDir, legacyLeafCertFilename), encodeCertPEM(der), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(leafDir, legacyLeafKeyFilename), encodeKeyPEM(otherKey), 0o600); err != nil {
			t.Fatal(err)
		}
	})

	// Copied directory: byte-identical copy of valid.example.local sharing
	// its serial (a real-world "foo.local" + "foo.local.bak" backup copy).
	validCertPEM, err := os.ReadFile(filepath.Join(dir, "certs", "valid.example.local", legacyLeafCertFilename))
	if err != nil {
		t.Fatal(err)
	}
	validKeyPEM, err := os.ReadFile(filepath.Join(dir, "certs", "valid.example.local", legacyLeafKeyFilename))
	if err != nil {
		t.Fatal(err)
	}
	copyDir := filepath.Join(dir, "certs", "valid.example.local.bak")
	if err := os.MkdirAll(copyDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(copyDir, legacyLeafCertFilename), validCertPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(copyDir, legacyLeafKeyFilename), validKeyPEM, 0o600); err != nil {
		t.Fatal(err)
	}

	// Stray, non-managed files in the legacy root (the legacy app's own
	// listener cert).
	if err := os.WriteFile(filepath.Join(dir, "server.crt"), []byte("not managed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "server.key"), []byte("not managed\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	return dir, rootCert, rootKey
}

// This tree has 15 directories under certs/: valid, expired, wildcard,
// nochain, 5 broken, valid.bak (duplicate serial) = 4 + 5 + 1 = 10.
const legacyTreeLeafCount = 10

func TestPreviewIsReadOnlyAndMatchesExecuteCounts(t *testing.T) {
	dir, _, _ := writeLegacyTree(t)
	ctx := context.Background()
	s := openTestStore(t)

	before := snapshotTree(t, dir)

	preview, err := s.Preview(ctx, dir)
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if n, err := s.CountCerts(ctx); err != nil || n != 0 {
		t.Fatalf("CountCerts after Preview = %d, %v; want 0, nil", n, err)
	}
	assertSnapshotUnchanged(t, dir, before)

	if len(preview.Items) != legacyTreeLeafCount {
		t.Fatalf("Preview Items = %d, want %d", len(preview.Items), legacyTreeLeafCount)
	}
	// 4 clean-classification importable/expired candidates: valid,
	// wildcard, nochain are importable; expired is Expired. The copied
	// directory is Skipped (duplicate serial), not Importable/Expired.
	if preview.Importable != 3 {
		t.Errorf("Preview.Importable = %d, want 3 (valid, wildcard, nochain)", preview.Importable)
	}
	if preview.Expired != 1 {
		t.Errorf("Preview.Expired = %d, want 1", preview.Expired)
	}
	if preview.Broken != 5 {
		t.Errorf("Preview.Broken = %d, want 5", preview.Broken)
	}
	if preview.Skipped != 1 {
		t.Errorf("Preview.Skipped = %d, want 1 (the copied directory)", preview.Skipped)
	}
	if len(preview.StrayFiles) != 2 {
		t.Errorf("Preview.StrayFiles = %v, want 2 entries (server.crt, server.key)", preview.StrayFiles)
	}

	execute, err := s.Execute(ctx, dir, false)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	assertSnapshotUnchanged(t, dir, before)

	if execute.Importable != preview.Importable || execute.Expired != preview.Expired ||
		execute.Broken != preview.Broken || execute.Skipped != preview.Skipped {
		t.Errorf("Execute counts %+v != Preview counts %+v", execute, preview)
	}
	if len(execute.Items) != len(preview.Items) {
		t.Errorf("Execute Items = %d, Preview Items = %d, want equal", len(execute.Items), len(preview.Items))
	}

	wantRows := legacyTreeLeafCount - preview.Skipped // every non-skipped leaf gets a row
	n, err := s.CountCerts(ctx)
	if err != nil {
		t.Fatalf("CountCerts: %v", err)
	}
	if n != wantRows {
		t.Errorf("CountCerts after Execute = %d, want %d", n, wantRows)
	}
}

func TestSecondExecuteSkipsEverythingIncludingQuarantined(t *testing.T) {
	dir, _, _ := writeLegacyTree(t)
	ctx := context.Background()
	s := openTestStore(t)

	first, err := s.Execute(ctx, dir, false)
	if err != nil {
		t.Fatalf("first Execute: %v", err)
	}
	nAfterFirst, err := s.CountCerts(ctx)
	if err != nil {
		t.Fatalf("CountCerts: %v", err)
	}

	// Preview against the now-populated DB must itself report the skips
	// (M-A: Preview consults the DB too).
	preview2, err := s.Preview(ctx, dir)
	if err != nil {
		t.Fatalf("second Preview: %v", err)
	}
	if preview2.Skipped != legacyTreeLeafCount {
		t.Errorf("second Preview.Skipped = %d, want %d (everything)", preview2.Skipped, legacyTreeLeafCount)
	}
	if preview2.Importable != 0 || preview2.Expired != 0 || preview2.Broken != 0 {
		t.Errorf("second Preview should classify nothing as importable/expired/broken, got %+v", preview2)
	}

	if _, err := s.Execute(ctx, dir, false); !errors.Is(err, ErrImportConfirmRequired) {
		t.Fatalf("second Execute without confirmNonEmpty = %v, want ErrImportConfirmRequired", err)
	}
	if n, _ := s.CountCerts(ctx); n != nAfterFirst {
		t.Errorf("row count changed after a refused Execute: got %d, want %d", n, nAfterFirst)
	}

	second, err := s.Execute(ctx, dir, true)
	if err != nil {
		t.Fatalf("second Execute with confirmNonEmpty: %v", err)
	}
	if second.Skipped != legacyTreeLeafCount {
		t.Errorf("second Execute.Skipped = %d, want %d", second.Skipped, legacyTreeLeafCount)
	}
	nAfterSecond, err := s.CountCerts(ctx)
	if err != nil {
		t.Fatalf("CountCerts: %v", err)
	}
	if nAfterSecond != nAfterFirst {
		t.Errorf("second Execute added rows: before=%d after=%d, want equal (including quarantined rows)", nAfterFirst, nAfterSecond)
	}
	_ = first
}

func TestQuarantineReasonsAndSerialNullness(t *testing.T) {
	dir, _, _ := writeLegacyTree(t)
	ctx := context.Background()
	s := openTestStore(t)

	if _, err := s.Execute(ctx, dir, false); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	certs, err := s.ListCerts(ctx)
	if err != nil {
		t.Fatalf("ListCerts: %v", err)
	}
	byFQDN := make(map[string]Cert, len(certs))
	for _, c := range certs {
		byFQDN[c.FQDN] = c
	}

	cases := []struct {
		fqdn       string // directory name for the two no-CN cases, CN otherwise
		wantReason string
		serialNil  bool
	}{
		{"missing-cert", "cert.pem missing or not valid PEM", true},
		{"garbage-cert", "cert.pem missing or not valid PEM", true},
		{"missing-key.example.local", "private key file missing", false},
		{"garbage-key.example.local", "private key could not be parsed", false},
		{"mismatched-key.example.local", "private key does not match certificate", false},
	}
	for _, tc := range cases {
		c, ok := byFQDN[tc.fqdn]
		if !ok {
			t.Errorf("no row with fqdn %q", tc.fqdn)
			continue
		}
		if c.Status != StatusQuarantined {
			t.Errorf("%s: status = %s, want quarantined", tc.fqdn, c.Status)
		}
		if c.QuarantineReason == nil || !containsPrefix(*c.QuarantineReason, tc.wantReason) {
			t.Errorf("%s: quarantine_reason = %v, want prefix %q", tc.fqdn, c.QuarantineReason, tc.wantReason)
		}
		if tc.serialNil && c.Serial != nil {
			t.Errorf("%s: serial = %v, want nil", tc.fqdn, *c.Serial)
		}
		if !tc.serialNil && c.Serial == nil {
			t.Errorf("%s: serial = nil, want a real value", tc.fqdn)
		}
	}
}

func containsPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

func TestWildcardCNImports(t *testing.T) {
	dir, _, _ := writeLegacyTree(t)
	ctx := context.Background()
	s := openTestStore(t)

	if _, err := s.Execute(ctx, dir, false); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	certs, _ := s.ListCerts(ctx)
	found := false
	for _, c := range certs {
		if c.FQDN == "*.example.local" {
			found = true
			if c.Status != StatusActive {
				t.Errorf("wildcard cert status = %s, want active", c.Status)
			}
		}
	}
	if !found {
		t.Error("no imported row with fqdn *.example.local")
	}
}

func TestExpiredLeafImportsAsNormalActiveRow(t *testing.T) {
	dir, _, _ := writeLegacyTree(t)
	ctx := context.Background()
	s := openTestStore(t)

	if _, err := s.Execute(ctx, dir, false); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	certs, _ := s.ListCerts(ctx)
	for _, c := range certs {
		if c.FQDN == "expired.example.local" {
			if c.Status != StatusActive {
				t.Errorf("expired.example.local status = %s, want active (expired is never a stored status)", c.Status)
			}
			return
		}
	}
	t.Fatal("no imported row with fqdn expired.example.local")
}

func TestChainMismatchGetsImportWarningNotQuarantine(t *testing.T) {
	dir, _, _ := writeLegacyTree(t)
	ctx := context.Background()
	s := openTestStore(t)

	if _, err := s.Execute(ctx, dir, false); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	certs, _ := s.ListCerts(ctx)
	for _, c := range certs {
		if c.FQDN == "nochain.example.local" {
			if c.Status != StatusActive {
				t.Errorf("nochain.example.local status = %s, want active (not quarantined)", c.Status)
			}
			if c.ImportWarning == nil || *c.ImportWarning == "" {
				t.Error("nochain.example.local import_warning is unset, want a chain-mismatch warning")
			}
			return
		}
	}
	t.Fatal("no imported row with fqdn nochain.example.local")
}

func TestDuplicateSerialAcrossDirsSkipsOneNotAborts(t *testing.T) {
	dir, _, _ := writeLegacyTree(t)
	ctx := context.Background()
	s := openTestStore(t)

	report, err := s.Execute(ctx, dir, false)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var skippedItem *ImportItem
	for i := range report.Items {
		if report.Items[i].Path == "certs/valid.example.local.bak" {
			skippedItem = &report.Items[i]
		}
	}
	if skippedItem == nil {
		t.Fatal("no report item for certs/valid.example.local.bak")
	}
	if skippedItem.Status != ImportOutcomeSkipped {
		t.Errorf("valid.example.local.bak outcome = %s, want skipped", skippedItem.Status)
	}
	if skippedItem.Reason == "" {
		t.Error("skipped item has no reason naming the duplicate")
	}

	// Exactly one row for the shared serial: the import continued rather
	// than aborting.
	certs, _ := s.ListCerts(ctx)
	count := 0
	for _, c := range certs {
		if c.FQDN == "valid.example.local" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("rows with fqdn valid.example.local = %d, want 1 (copy skipped, not duplicated or aborted)", count)
	}
}

func TestDuplicateCNResolutionNewestWinsActive(t *testing.T) {
	dir := t.TempDir()
	_, _, rootCert, rootKey := writeLegacyRootFixture(t, dir)

	writeLegacyLeafFixture(t, dir, "old-dup", "dup.example.local", rootCert, rootKey, leafOpts{
		notAfter: time.Now().AddDate(0, 6, 0),
	})
	writeLegacyLeafFixture(t, dir, "new-dup", "dup.example.local", rootCert, rootKey, leafOpts{
		notAfter: time.Now().AddDate(1, 0, 0),
	})

	ctx := context.Background()
	s := openTestStore(t)
	if _, err := s.Execute(ctx, dir, false); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	certs, _ := s.ListCerts(ctx)
	var active, archived int
	for _, c := range certs {
		if c.FQDN != "dup.example.local" {
			continue
		}
		switch c.Status {
		case StatusActive:
			active++
			if c.ImportedFrom == nil || *c.ImportedFrom != "certs/new-dup" {
				t.Errorf("active dup.example.local imported_from = %v, want certs/new-dup (the newer not_after)", c.ImportedFrom)
			}
		case StatusArchived:
			archived++
		default:
			t.Errorf("unexpected status %s for dup.example.local row", c.Status)
		}
	}
	if active != 1 || archived != 1 {
		t.Errorf("dup.example.local rows: active=%d archived=%d, want 1 and 1", active, archived)
	}
}

func TestByteForByteLeafImport(t *testing.T) {
	dir := t.TempDir()
	_, _, rootCert, rootKey := writeLegacyRootFixture(t, dir)
	wantCertPEM, wantKeyPEM, _ := writeLegacyLeafFixture(t, dir, "byte.example.local", "byte.example.local", rootCert, rootKey, leafOpts{})

	ctx := context.Background()
	s := openTestStore(t)
	if _, err := s.Execute(ctx, dir, false); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	certs, _ := s.ListCerts(ctx)
	var id int64 = -1
	for _, c := range certs {
		if c.FQDN == "byte.example.local" {
			id = c.ID
		}
	}
	if id == -1 {
		t.Fatal("no imported row for byte.example.local")
	}
	got, err := s.getCertWithKey(ctx, id)
	if err != nil {
		t.Fatalf("getCertWithKey: %v", err)
	}
	if got.CertPEM == nil || !bytes.Equal([]byte(*got.CertPEM), wantCertPEM) {
		t.Error("imported cert_pem is not byte-for-byte equal to the source cert.pem")
	}
	if got.KeyPEM == nil || !bytes.Equal([]byte(*got.KeyPEM), wantKeyPEM) {
		t.Error("imported key_pem is not byte-for-byte equal to the source key.pem")
	}
}

func TestImportedFromIsRelativeAndCrossParentPathIdempotent(t *testing.T) {
	// Two entirely separate temp parents, byte-identical trees underneath.
	dir1 := filepath.Join(t.TempDir(), "one", "deep", "path")
	dir2 := filepath.Join(t.TempDir(), "somewhere", "else")

	_, _, rootCert, rootKey := writeLegacyRootFixture(t, dir1)
	certPEM, keyPEM, _ := writeLegacyLeafFixture(t, dir1, "rel.example.local", "rel.example.local", rootCert, rootKey, leafOpts{})

	// Byte-identical copy under a completely different absolute parent.
	rootCertPEM, err := os.ReadFile(filepath.Join(dir1, legacyRootCertFilename))
	if err != nil {
		t.Fatal(err)
	}
	rootKeyPEM, err := os.ReadFile(filepath.Join(dir1, legacyRootKeyFilename))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir2, "certs", "rel.example.local"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir2, legacyRootCertFilename), rootCertPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir2, legacyRootKeyFilename), rootKeyPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir2, "certs", "rel.example.local", legacyLeafCertFilename), certPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir2, "certs", "rel.example.local", legacyLeafKeyFilename), keyPEM, 0o600); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	s := openTestStore(t)
	if _, err := s.Execute(ctx, dir1, false); err != nil {
		t.Fatalf("Execute(dir1): %v", err)
	}

	certs, _ := s.ListCerts(ctx)
	for _, c := range certs {
		if c.FQDN == "rel.example.local" {
			if c.ImportedFrom == nil || *c.ImportedFrom != "certs/rel.example.local" {
				t.Errorf("imported_from = %v, want %q (relative, not absolute)", c.ImportedFrom, "certs/rel.example.local")
			}
		}
	}
	nBefore, _ := s.CountCerts(ctx)

	report, err := s.Execute(ctx, dir2, true)
	if err != nil {
		t.Fatalf("Execute(dir2): %v", err)
	}
	if report.Importable != 0 || report.Expired != 0 || report.Broken != 0 {
		t.Errorf("Execute(dir2) should classify nothing new, got %+v", report)
	}
	if report.Skipped == 0 {
		t.Error("Execute(dir2) reported zero skipped, want every leaf skipped (same relative imported_from)")
	}
	nAfter, _ := s.CountCerts(ctx)
	if nAfter != nBefore {
		t.Errorf("Execute(dir2) added rows: before=%d after=%d, want equal", nBefore, nAfter)
	}
}

func TestStrayFilesReported(t *testing.T) {
	dir, _, _ := writeLegacyTree(t)
	ctx := context.Background()
	s := openTestStore(t)

	preview, err := s.Preview(ctx, dir)
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	got := append([]string(nil), preview.StrayFiles...)
	sort.Strings(got)
	want := []string{"server.crt", "server.key"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("StrayFiles = %v, want %v", got, want)
	}
}

func TestMissingLegacyRootFailsPreviewAndExecute(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "certs"), 0o700); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	s := openTestStore(t)

	if _, err := s.Preview(ctx, dir); err == nil {
		t.Error("Preview with no rootCA.crt = nil error, want error")
	}
	if _, err := s.Execute(ctx, dir, false); err == nil {
		t.Error("Execute with no rootCA.crt = nil error, want error")
	}
	if n, _ := s.CountCerts(ctx); n != 0 {
		t.Errorf("CountCerts after failed Execute = %d, want 0", n)
	}
}

// snapshotEntry is one file's identity for the WalkDir before/after
// comparison: legacy_import_dir must never be modified by Preview or
// Execute.
type snapshotEntry struct {
	path    string
	size    int64
	modTime time.Time
	mode    fs.FileMode
}

func snapshotTree(t *testing.T, dir string) []snapshotEntry {
	t.Helper()
	var entries []snapshotEntry
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		entries = append(entries, snapshotEntry{path: p, size: info.Size(), modTime: info.ModTime(), mode: info.Mode()})
		return nil
	})
	if err != nil {
		t.Fatalf("snapshotTree: %v", err)
	}
	return entries
}

func assertSnapshotUnchanged(t *testing.T, dir string, before []snapshotEntry) {
	t.Helper()
	after := snapshotTree(t, dir)
	if len(after) != len(before) {
		t.Fatalf("legacy tree entry count changed: before=%d after=%d", len(before), len(after))
	}
	for i := range before {
		if before[i] != after[i] {
			t.Errorf("legacy tree entry changed: before=%+v after=%+v", before[i], after[i])
		}
	}
}

// TestLegacyDirReadOnlyTreeSurvivesImport proves Preview/Execute work
// against a tree where every file is 0400 and every directory is 0500 --
// legacy_import_dir must be treated as strictly read-only.
func TestLegacyDirReadOnlyTreeSurvivesImport(t *testing.T) {
	dir, _, _ := writeLegacyTree(t)

	var dirs []string
	if err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			dirs = append(dirs, p)
			return nil
		}
		return os.Chmod(p, 0o400)
	}); err != nil {
		t.Fatalf("chmod files read-only: %v", err)
	}
	// Deepest-first so a parent doesn't lose write access before its
	// children still need it chmod'd.
	sort.Sort(sort.Reverse(sort.StringSlice(dirs)))
	for _, d := range dirs {
		if err := os.Chmod(d, 0o500); err != nil {
			t.Fatalf("chmod dir read-only: %v", err)
		}
	}
	t.Cleanup(func() {
		// Restore write permission (deepest-last is fine here, any order
		// that lets t.TempDir()'s own cleanup remove everything works)
		// before TempDir's cleanup runs.
		for _, d := range dirs {
			_ = os.Chmod(d, 0o700)
		}
	})

	before := snapshotTree(t, dir)

	ctx := context.Background()
	s := openTestStore(t)
	if _, err := s.Preview(ctx, dir); err != nil {
		t.Fatalf("Preview against read-only tree: %v", err)
	}
	if _, err := s.Execute(ctx, dir, false); err != nil {
		t.Fatalf("Execute against read-only tree: %v", err)
	}

	assertSnapshotUnchanged(t, dir, before)
}

// FR-6's delete is a database operation only: the legacy tree is a read-only
// source, so removing an imported row must not remove (or touch) the directory
// it came from. Nothing in Delete's code path writes to disk today, but nothing
// structurally prevents a future "clean up the source too" convenience either,
// and the cost of being wrong is deleting an operator's only copy of a private
// key. This also pins the documented remediation path for a quarantined row
// (docs/certmachine.md §8): because DeleteCert is a hard delete, the row's
// imported_from and serial leave the skip-sets, so a later Execute re-imports
// the fixed-up directory.
func TestDeletingAnImportedRowLeavesTheLegacyTreeUntouched(t *testing.T) {
	dir, _, _ := writeLegacyTree(t)
	ctx := context.Background()
	s := openTestStore(t)

	if _, err := s.Execute(ctx, dir, false); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	before := snapshotTree(t, dir)

	certs, err := s.ListCerts(ctx)
	if err != nil {
		t.Fatalf("ListCerts: %v", err)
	}
	// The wildcard leaf, deliberately: it is the tree's one importable leaf with
	// neither a duplicate-serial sibling (valid.example.local.bak) nor a
	// duplicate CN, so deleting it isolates the skip-set behaviour being tested
	// from the duplicate-resolution rules.
	var target *Cert
	for i := range certs {
		if certs[i].FQDN == "*.example.local" && certs[i].ImportedFrom != nil {
			target = &certs[i]
			break
		}
	}
	if target == nil {
		t.Fatal("no imported row for the wildcard leaf to delete")
	}
	importedFrom := *target.ImportedFrom

	if err := s.Delete(ctx, target.ID, target.FQDN); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.GetCert(ctx, target.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetCert after delete = %v, want ErrNotFound (FR-6 is a hard delete)", err)
	}
	assertSnapshotUnchanged(t, dir, before)

	// The remediation path: the source directory is still there, so a re-run
	// imports it again rather than skipping it forever.
	report, err := s.Execute(ctx, dir, true)
	if err != nil {
		t.Fatalf("second Execute: %v", err)
	}
	if report.Importable != 1 {
		t.Errorf("second Execute Importable = %d, want 1 (only the deleted row's source)", report.Importable)
	}
	if report.Skipped != legacyTreeLeafCount-1 {
		t.Errorf("second Execute Skipped = %d, want %d (everything still held)", report.Skipped, legacyTreeLeafCount-1)
	}
	certs, err = s.ListCerts(ctx)
	if err != nil {
		t.Fatalf("ListCerts after re-import: %v", err)
	}
	var found bool
	for _, c := range certs {
		if c.ImportedFrom != nil && *c.ImportedFrom == importedFrom {
			found = true
		}
	}
	if !found {
		t.Errorf("%s was not re-imported after its row was deleted", importedFrom)
	}
	assertSnapshotUnchanged(t, dir, before)
}

// TestFailedLeafTransactionRollsBackEveryLeaf proves the leaf half of Execute
// is genuinely all-or-nothing: a pre-existing active row for one of the
// tree's FQDNs (not reachable via imported_from/serial skip-matching, so it
// slips past determineSkips) makes InsertCert refuse that one leaf with
// ErrDuplicateActive inside the shared WithTx -- which must roll back every
// other leaf insert attempted in the same Execute call, not just the
// colliding one. Execute still returns the full classification report
// alongside the error (the "partial classification" the FR-8 checklist and
// the doc comment on Execute describe), and a retry after removing the
// collision succeeds cleanly.
func TestFailedLeafTransactionRollsBackEveryLeaf(t *testing.T) {
	dir, _, _ := writeLegacyTree(t)
	ctx := context.Background()
	s := openTestStore(t)

	// A natively-generated row (no imported_from, unrelated serial) that
	// happens to share the FQDN of one of the tree's clean, importable
	// leaves -- this is not a skip case, so the leaf batch reaches
	// InsertCert and collides on certs_one_active_per_fqdn.
	if _, err := insertCert(s, fixtureCert("valid.example.local", StatusActive, "2030-01-01T00:00:00Z")); err != nil {
		t.Fatalf("seed collision row: %v", err)
	}
	nBefore, err := s.CountCerts(ctx)
	if err != nil {
		t.Fatalf("CountCerts: %v", err)
	}

	report, err := s.Execute(ctx, dir, true)
	if err == nil {
		t.Fatal("Execute with a colliding active FQDN = nil error, want ErrDuplicateActive")
	}
	if !errors.Is(err, ErrDuplicateActive) {
		t.Errorf("Execute error = %v, want ErrDuplicateActive", err)
	}
	// The message must name which fqdn collided. A bare "a certificate is
	// already active for this fqdn" leaves the operator to bisect the tree by
	// hand -- and the whole point of the import wizard is that the tree may
	// hold hundreds of directories.
	if !strings.Contains(err.Error(), "valid.example.local") {
		t.Errorf("Execute error names no fqdn: %v", err)
	}
	if report == nil {
		t.Fatal("Execute returned a nil report alongside the error, want the partial classification")
	}
	if len(report.Items) != legacyTreeLeafCount {
		t.Errorf("report.Items = %d, want %d (classification still rendered in full)", len(report.Items), legacyTreeLeafCount)
	}

	nAfter, err := s.CountCerts(ctx)
	if err != nil {
		t.Fatalf("CountCerts: %v", err)
	}
	if nAfter != nBefore {
		t.Errorf("row count changed after a failed Execute: before=%d after=%d, want equal (every leaf insert rolled back, not just the colliding one)", nBefore, nAfter)
	}
	// Specifically: none of the OTHER, non-colliding leaves (e.g. the
	// wildcard CN) were left behind by a partial commit.
	certs, _ := s.ListCerts(ctx)
	for _, c := range certs {
		if c.FQDN == "*.example.local" {
			t.Error("wildcard.example.local was committed despite the transaction failing on a different leaf")
		}
	}
}

// TestImportPerformance is the FR-8/non-functional perf budget: a few
// hundred certs import in seconds, not because signing is slow but because
// keygen is -- so this fixture mints ONE RSA key and reuses it across every
// leaf, only varying the CN/serial per certificate.
func TestImportPerformance(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping performance fixture in -short mode")
	}
	dir := t.TempDir()
	_, _, rootCert, rootKey := writeLegacyRootFixture(t, dir)

	sharedKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate shared leaf key: %v", err)
	}

	const n = 300
	for i := 0; i < n; i++ {
		fqdn := fmt.Sprintf("host%d.example.local", i)
		writeLegacyLeafFixture(t, dir, fqdn, fqdn, rootCert, rootKey, leafOpts{key: sharedKey})
	}

	ctx := context.Background()
	s := openTestStore(t)

	start := time.Now()
	report, err := s.Execute(ctx, dir, false)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if report.Importable != n {
		t.Errorf("Importable = %d, want %d", report.Importable, n)
	}
	t.Logf("imported %d certs in %s", n, elapsed)
	if elapsed > 5*time.Second {
		t.Errorf("Execute of %d certs took %s, want well under 5s", n, elapsed)
	}
}
