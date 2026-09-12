package certmachine

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeLegacyHAProxyPEMFixture reproduces reference/certmachine/main.go's
// initCA (:74-93) and genCert (:95-148) verbatim -- the same
// x509.CreateCertificate calls with the same templates, the same
// writeCert/writeKey PEM encoding, and the same three-block
// pem.Encode sequence for haproxy.pem (:120-140) -- and writes the whole
// legacy directory tree (rootCA.crt/rootCA.key, certs/<fqdn>/{cert.pem,
// key.pem,haproxy.pem}) to disk under dir. It returns the leaf/root/key DER
// and PEM bytes plus the path to the legacy-written haproxy.pem, so the test
// can rebuild the same three PEM inputs independently and compare
// HAProxyPEM's output against the actual on-disk file byte-for-byte.
func writeLegacyHAProxyPEMFixture(t *testing.T, dir, fqdn string) (leafDER, leafCertPEM, rootCertPEM, leafKeyPEM []byte, haproxyPath string) {
	t.Helper()

	rootKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate legacy root key: %v", err)
	}
	rootTpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "CertMachine Root CA"},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().AddDate(10, 0, 0),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
	}
	rootDER, err := x509.CreateCertificate(rand.Reader, rootTpl, rootTpl, &rootKey.PublicKey, rootKey)
	if err != nil {
		t.Fatalf("create legacy root cert: %v", err)
	}
	rootCert, err := x509.ParseCertificate(rootDER)
	if err != nil {
		t.Fatalf("parse legacy root cert: %v", err)
	}

	leafKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate legacy leaf key: %v", err)
	}
	leafTpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: fqdn},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().AddDate(1, 0, 0),
		DNSNames:     []string{fqdn},
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	leafDER, err = x509.CreateCertificate(rand.Reader, leafTpl, rootCert, &leafKey.PublicKey, rootKey)
	if err != nil {
		t.Fatalf("create legacy leaf cert: %v", err)
	}

	// writeCert/writeKey (reference/certmachine/main.go:197-205).
	rootCertPath := filepath.Join(dir, "rootCA.crt")
	if err := writePEMFile(rootCertPath, pemTypeCertificate, rootCert.Raw); err != nil {
		t.Fatalf("write rootCA.crt: %v", err)
	}

	certDir := filepath.Join(dir, "certs", fqdn)
	if err := os.MkdirAll(certDir, 0o700); err != nil {
		t.Fatalf("mkdir cert dir: %v", err)
	}
	leafCertPath := filepath.Join(certDir, "cert.pem")
	if err := writePEMFile(leafCertPath, pemTypeCertificate, leafDER); err != nil {
		t.Fatalf("write cert.pem: %v", err)
	}
	leafKeyPath := filepath.Join(certDir, "key.pem")
	if err := writePEMFile(leafKeyPath, pemTypeRSAPrivateKey, x509.MarshalPKCS1PrivateKey(leafKey)); err != nil {
		t.Fatalf("write key.pem: %v", err)
	}

	// genCert's haproxy.pem assembly (reference/certmachine/main.go:120-140):
	// leaf, then root CA (re-encoded from the parsed certificate's Raw),
	// then the private key -- written with os.WriteFile, mode 0600.
	hap := &bytes.Buffer{}
	_ = pem.Encode(hap, &pem.Block{Type: pemTypeCertificate, Bytes: leafDER})
	_ = pem.Encode(hap, &pem.Block{Type: pemTypeCertificate, Bytes: rootCert.Raw})
	_ = pem.Encode(hap, &pem.Block{Type: pemTypeRSAPrivateKey, Bytes: x509.MarshalPKCS1PrivateKey(leafKey)})

	haproxyPath = filepath.Join(certDir, "haproxy.pem")
	if err := os.WriteFile(haproxyPath, hap.Bytes(), 0o600); err != nil {
		t.Fatalf("write haproxy.pem: %v", err)
	}

	leafCertPEM, err = os.ReadFile(leafCertPath)
	if err != nil {
		t.Fatalf("read back cert.pem: %v", err)
	}
	rootCertPEM, err = os.ReadFile(rootCertPath)
	if err != nil {
		t.Fatalf("read back rootCA.crt: %v", err)
	}
	leafKeyPEM, err = os.ReadFile(leafKeyPath)
	if err != nil {
		t.Fatalf("read back key.pem: %v", err)
	}
	return leafDER, leafCertPEM, rootCertPEM, leafKeyPEM, haproxyPath
}

// writePEMFile matches reference/certmachine/main.go's writeCert/writeKey:
// open (truncate/create) and pem.Encode straight into the file.
func writePEMFile(path, blockType string, der []byte) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return pem.Encode(f, &pem.Block{Type: blockType, Bytes: der})
}

// TestAssembledHAProxyPEMMatchesLegacyFile is the third and strongest
// Driver-1 contract test (FR-3): it builds a legacy-style tree using the
// exact same x509.CreateCertificate templates and pem.Encode calls the
// legacy app uses (reference/certmachine/main.go:74-148), writes an actual
// haproxy.pem to disk exactly as legacy's genCert does, then asserts that
// HAProxyPEM -- fed the independently-read cert.pem/rootCA.crt/key.pem --
// reproduces that on-disk file byte-for-byte.
func TestAssembledHAProxyPEMMatchesLegacyFile(t *testing.T) {
	dir := t.TempDir()
	_, leafCertPEM, rootCertPEM, leafKeyPEM, haproxyPath := writeLegacyHAProxyPEMFixture(t, dir, "svc.example.local")

	legacyHAProxyPEM, err := os.ReadFile(haproxyPath)
	if err != nil {
		t.Fatalf("read legacy-written haproxy.pem: %v", err)
	}

	got := HAProxyPEM(leafCertPEM, rootCertPEM, leafKeyPEM)

	if !bytes.Equal(got, legacyHAProxyPEM) {
		t.Fatalf("HAProxyPEM output is not byte-for-byte equal to the legacy-written haproxy.pem\n--- got (%d bytes) ---\n%s\n--- want (%d bytes) ---\n%s",
			len(got), got, len(legacyHAProxyPEM), legacyHAProxyPEM)
	}
}

// HAProxyPEM must decode to exactly three blocks, CERTIFICATE / CERTIFICATE
// / RSA PRIVATE KEY, with block 1 the leaf and block 2 the root (slice-3
// verification, beyond the three named contract tests).
func TestHAProxyPEMBlockOrderAndCount(t *testing.T) {
	dir := t.TempDir()
	leafDER, leafCertPEM, rootCertPEM, leafKeyPEM, _ := writeLegacyHAProxyPEMFixture(t, dir, "blocks.example.local")
	rootBlock, _ := pem.Decode(rootCertPEM)
	if rootBlock == nil {
		t.Fatal("failed to decode root cert PEM fixture")
	}

	assembled := HAProxyPEM(leafCertPEM, rootCertPEM, leafKeyPEM)

	block1, rest := pem.Decode(assembled)
	if block1 == nil {
		t.Fatal("first PEM block failed to decode")
	}
	if block1.Type != pemTypeCertificate {
		t.Errorf("block 1 type = %q, want %q", block1.Type, pemTypeCertificate)
	}
	if !bytes.Equal(block1.Bytes, leafDER) {
		t.Error("block 1 bytes do not match the leaf certificate DER")
	}

	block2, rest := pem.Decode(rest)
	if block2 == nil {
		t.Fatal("second PEM block failed to decode")
	}
	if block2.Type != pemTypeCertificate {
		t.Errorf("block 2 type = %q, want %q", block2.Type, pemTypeCertificate)
	}
	if !bytes.Equal(block2.Bytes, rootBlock.Bytes) {
		t.Error("block 2 bytes do not match the root certificate DER")
	}

	block3, rest := pem.Decode(rest)
	if block3 == nil {
		t.Fatal("third PEM block failed to decode")
	}
	if block3.Type != pemTypeRSAPrivateKey {
		t.Errorf("block 3 type = %q, want %q", block3.Type, pemTypeRSAPrivateKey)
	}

	if trailing, _ := pem.Decode(rest); trailing != nil {
		t.Error("HAProxyPEM output decodes to more than three blocks")
	}
}

// untarEntry is one entry read back out of a BundleTGZ archive.
type untarEntry struct {
	name string
	mode int64
	data []byte
}

// untarBundle reads a gzipped tar produced by BundleTGZ back into its
// entries, in on-disk order, failing the test on any read error.
func untarBundle(t *testing.T, gzData []byte) []untarEntry {
	t.Helper()
	gzr, err := gzip.NewReader(bytes.NewReader(gzData))
	if err != nil {
		t.Fatalf("gzip.NewReader: %v", err)
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)
	var entries []untarEntry
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar.Next: %v", err)
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			t.Fatalf("read tar entry %s: %v", hdr.Name, err)
		}
		entries = append(entries, untarEntry{name: hdr.Name, mode: hdr.Mode, data: data})
	}
	return entries
}

// TestBundleTGZContainsFourEntriesWithModesAndContent is slice 6's
// verification bullet: BundleTGZ read back through gzip.Reader + tar.Reader
// yields exactly four entries with modes 0644/0600/0600/0644, and each
// entry's content matches the store row it was built from.
func TestBundleTGZContainsFourEntriesWithModesAndContent(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	setupCA(t, s, time.Now().AddDate(10, 0, 0))
	ca, err := s.GetCA(ctx)
	if err != nil {
		t.Fatalf("GetCA: %v", err)
	}

	req, err := ValidateRequest("bundle.example.local", nil, nil)
	if err != nil {
		t.Fatalf("ValidateRequest: %v", err)
	}
	issued, err := s.Generate(ctx, req, 365, 30)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	cert, err := s.getCertWithKey(ctx, issued.Cert.ID)
	if err != nil {
		t.Fatalf("getCertWithKey: %v", err)
	}

	gz, err := BundleTGZ(*cert, []byte(ca.CertPEM))
	if err != nil {
		t.Fatalf("BundleTGZ: %v", err)
	}

	entries := untarBundle(t, gz)
	if len(entries) != 4 {
		t.Fatalf("got %d entries, want 4: %+v", len(entries), entries)
	}

	want := map[string]struct {
		mode int64
		data []byte
	}{
		"cert.pem":    {0o644, []byte(*cert.CertPEM)},
		"key.pem":     {0o600, []byte(*cert.KeyPEM)},
		"haproxy.pem": {0o600, HAProxyPEM([]byte(*cert.CertPEM), []byte(ca.CertPEM), []byte(*cert.KeyPEM))},
		"rootCA.crt":  {0o644, []byte(ca.CertPEM)},
	}
	seen := map[string]bool{}
	for _, e := range entries {
		w, ok := want[e.name]
		if !ok {
			t.Errorf("unexpected entry %q", e.name)
			continue
		}
		seen[e.name] = true
		if e.mode != w.mode {
			t.Errorf("entry %q mode = %o, want %o", e.name, e.mode, w.mode)
		}
		if !bytes.Equal(e.data, w.data) {
			t.Errorf("entry %q content does not match the store", e.name)
		}
	}
	for name := range want {
		if !seen[name] {
			t.Errorf("missing entry %q", name)
		}
	}
}

// TestSafeFilenameTable covers slice 6's verification bullet for
// SafeFilename: wildcard mapping, traversal neutralization, embedded slash,
// an unchanged plain name, and the all-filler fallback to "cert".
func TestSafeFilenameTable(t *testing.T) {
	cases := []struct {
		name  string
		fqdn  string
		want  string
		check func(t *testing.T, got string)
	}{
		{name: "wildcard", fqdn: "*.example.local", want: "_wildcard.example.local"},
		{name: "plain unchanged", fqdn: "foo.local", want: "foo.local"},
		{name: "embedded slash", fqdn: "a/b", want: "a_b"},
		{name: "bare wildcard falls back", fqdn: "*", want: "cert"},
		{name: "empty falls back", fqdn: "", want: "cert"},
		{
			name: "traversal neutralized",
			fqdn: "../../etc/passwd",
			check: func(t *testing.T, got string) {
				if strings.Contains(got, "/") {
					t.Errorf("SafeFilename(%q) = %q, contains a slash", "../../etc/passwd", got)
				}
				if strings.Contains(got, "..") {
					t.Errorf("SafeFilename(%q) = %q, contains a \"..\" run", "../../etc/passwd", got)
				}
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := SafeFilename(c.fqdn)
			if c.check != nil {
				c.check(t, got)
				return
			}
			if got != c.want {
				t.Errorf("SafeFilename(%q) = %q, want %q", c.fqdn, got, c.want)
			}
		})
	}
}

// TestRootMismatchRefusesHAProxyPEMAndBundle is slice 6's root-match guard
// verification: a leaf signed by a CA other than the one stored must yield
// an error from both AssembleHAProxyPEM and BundleTGZ, never a file carrying
// the wrong root.
func TestRootMismatchRefusesHAProxyPEMAndBundle(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	setupCA(t, s, time.Now().AddDate(10, 0, 0))
	ca, err := s.GetCA(ctx)
	if err != nil {
		t.Fatalf("GetCA: %v", err)
	}

	// Sign a leaf under a throwaway CA that is *not* the stored one, then
	// hand-build a Cert row exactly like getCertWithKey would return for an
	// imported leaf carrying an import_warning (slice 7's classification
	// table, "does not verify against the stored root").
	otherCA, otherKey, _ := newThrowawayCA(t, time.Now().AddDate(10, 0, 0))
	req := CertRequest{FQDN: "mismatch.example.local"}
	leaf, err := GenerateLeaf(otherCA, otherKey, req, 365)
	if err != nil {
		t.Fatalf("GenerateLeaf: %v", err)
	}

	warning := "certificate does not verify against the stored root"
	cert := Cert{
		ID:            1,
		FQDN:          req.FQDN,
		Status:        StatusActive,
		CertPEM:       strPtr(string(leaf.CertPEM)),
		KeyPEM:        strPtr(string(leaf.KeyPEM)),
		ImportWarning: strPtr(warning),
	}

	if _, err := AssembleHAProxyPEM(cert, []byte(ca.CertPEM)); !errors.Is(err, ErrRootMismatch) {
		t.Fatalf("AssembleHAProxyPEM error = %v, want ErrRootMismatch", err)
	} else if !strings.Contains(err.Error(), warning) {
		t.Errorf("AssembleHAProxyPEM error %q does not quote import_warning %q", err.Error(), warning)
	} else if !strings.Contains(err.Error(), "renew it to re-issue under the stored root") {
		t.Errorf("AssembleHAProxyPEM error %q does not name the renew remedy", err.Error())
	}

	if _, err := BundleTGZ(cert, []byte(ca.CertPEM)); !errors.Is(err, ErrRootMismatch) {
		t.Fatalf("BundleTGZ error = %v, want ErrRootMismatch", err)
	} else if !strings.Contains(err.Error(), "renew it to re-issue under the stored root") {
		t.Errorf("BundleTGZ error %q does not name the renew remedy", err.Error())
	}
}

// TestQuarantinedRefusesHAProxyPEMAndBundleWithoutNilDeref is slice 6's
// quarantine guard verification: a quarantined row with NULL cert_pem and
// key_pem (schema.go: both columns are nullable) must yield a deterministic
// sentinel from both builders, never a nil-pointer panic.
func TestQuarantinedRefusesHAProxyPEMAndBundleWithoutNilDeref(t *testing.T) {
	reason := "cert.pem missing or not valid PEM"
	cert := Cert{
		ID:               1,
		FQDN:             "broken-leaf",
		Status:           StatusQuarantined,
		CertPEM:          nil,
		KeyPEM:           nil,
		QuarantineReason: strPtr(reason),
	}

	if _, err := AssembleHAProxyPEM(cert, nil); !errors.Is(err, ErrQuarantinedDownload) {
		t.Fatalf("AssembleHAProxyPEM error = %v, want ErrQuarantinedDownload", err)
	} else if !strings.Contains(err.Error(), reason) {
		t.Errorf("AssembleHAProxyPEM error %q does not quote quarantine_reason %q", err.Error(), reason)
	}

	if _, err := BundleTGZ(cert, nil); !errors.Is(err, ErrQuarantinedDownload) {
		t.Fatalf("BundleTGZ error = %v, want ErrQuarantinedDownload", err)
	} else if !strings.Contains(err.Error(), reason) {
		t.Errorf("BundleTGZ error %q does not quote quarantine_reason %q", err.Error(), reason)
	}
}
