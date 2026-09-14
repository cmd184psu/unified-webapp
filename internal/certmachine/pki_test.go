package certmachine

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newThrowawayCA builds a self-signed RSA-2048 CA entirely outside GenerateCA
// (the plan's binding decision: a 4096-bit keygen under -race costs seconds
// per test, so only one test -- TestGenerateCAShape -- exercises the real
// 4096 default; every other test that needs "a CA" to sign against uses this
// cheaper fixture instead).
func newThrowawayCA(t *testing.T, notAfter time.Time) (*x509.Certificate, *rsa.PrivateKey, []byte) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate throwaway ca key: %v", err)
	}
	tpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Throwaway Test CA"},
		NotBefore:             time.Now().UTC().Add(-time.Hour),
		NotAfter:              notAfter,
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create throwaway ca cert: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse throwaway ca cert: %v", err)
	}
	return cert, key, encodeCertPEM(der)
}

func TestParseCertAndParseKeyRoundTrip(t *testing.T) {
	caCert, caKey, caCertPEM := newThrowawayCA(t, time.Now().AddDate(10, 0, 0))
	caKeyPEM := encodeKeyPEM(caKey)

	gotCert, err := ParseCert(caCertPEM)
	if err != nil {
		t.Fatalf("ParseCert: %v", err)
	}
	if gotCert.SerialNumber.Cmp(caCert.SerialNumber) != 0 {
		t.Errorf("ParseCert serial = %v, want %v", gotCert.SerialNumber, caCert.SerialNumber)
	}

	gotKey, err := ParseKey(caKeyPEM)
	if err != nil {
		t.Fatalf("ParseKey: %v", err)
	}
	if gotKey.N.Cmp(caKey.N) != 0 {
		t.Errorf("ParseKey N does not match original key")
	}
}

func TestParseCertRejectsGarbage(t *testing.T) {
	if _, err := ParseCert([]byte("not a pem block")); err == nil {
		t.Fatal("ParseCert(garbage) = nil error, want error")
	}
}

func TestParseKeyRejectsGarbage(t *testing.T) {
	if _, err := ParseKey([]byte("not a pem block")); err == nil {
		t.Fatal("ParseKey(garbage) = nil error, want error")
	}
}

// TestGenerateCAShape is the one test in this package that exercises the
// real RSA-4096 default (binding decision: expensive under -race, so kept to
// exactly one test).
func TestGenerateCAShape(t *testing.T) {
	certPEM, keyPEM, err := GenerateCA()
	if err != nil {
		t.Fatalf("GenerateCA: %v", err)
	}

	cert, err := ParseCert(certPEM)
	if err != nil {
		t.Fatalf("ParseCert(generated ca): %v", err)
	}
	key, err := ParseKey(keyPEM)
	if err != nil {
		t.Fatalf("ParseKey(generated ca): %v", err)
	}

	if key.N.BitLen() != 4096 {
		t.Errorf("ca key bit length = %d, want 4096", key.N.BitLen())
	}
	if cert.Subject.CommonName != "CertMachine Root CA" {
		t.Errorf("ca CN = %q, want %q", cert.Subject.CommonName, "CertMachine Root CA")
	}
	if !cert.IsCA {
		t.Error("generated ca cert IsCA = false, want true")
	}
	wantNotAfter := time.Now().UTC().AddDate(10, 0, 0)
	if diff := wantNotAfter.Sub(cert.NotAfter); diff < -time.Hour || diff > time.Hour {
		t.Errorf("ca NotAfter = %v, want ~%v", cert.NotAfter, wantNotAfter)
	}
	if cert.SerialNumber.Cmp(big.NewInt(1)) == 0 {
		t.Error("ca serial = 1, want a random 128-bit serial (legacy used big.NewInt(1))")
	}
	if cert.SerialNumber.BitLen() < 64 {
		t.Errorf("ca serial bit length = %d, too small to be a random 128-bit draw", cert.SerialNumber.BitLen())
	}
	if len(cert.SubjectKeyId) == 0 {
		t.Error("generated ca cert has no SubjectKeyId, want one (legacy root lacks it entirely)")
	}

	// Two calls must not collide.
	certPEM2, _, err := GenerateCA()
	if err != nil {
		t.Fatalf("second GenerateCA: %v", err)
	}
	cert2, err := ParseCert(certPEM2)
	if err != nil {
		t.Fatalf("ParseCert(second generated ca): %v", err)
	}
	if cert.SerialNumber.Cmp(cert2.SerialNumber) == 0 {
		t.Error("two GenerateCA calls produced the same serial")
	}
}

func TestGenerateLeafShape(t *testing.T) {
	caCert, caKey, _ := newThrowawayCA(t, time.Now().AddDate(10, 0, 0))

	req := CertRequest{
		FQDN:    "svc.example.local",
		DNSSans: []string{"alt.example.local"},
		IPSans:  []net.IP{net.ParseIP("10.0.0.9")},
	}
	result, err := GenerateLeaf(caCert, caKey, req, 365)
	if err != nil {
		t.Fatalf("GenerateLeaf: %v", err)
	}

	leaf, err := ParseCert(result.CertPEM)
	if err != nil {
		t.Fatalf("ParseCert(leaf): %v", err)
	}
	key, err := ParseKey(result.KeyPEM)
	if err != nil {
		t.Fatalf("ParseKey(leaf): %v", err)
	}
	if key.N.BitLen() != 2048 {
		t.Errorf("leaf key bit length = %d, want 2048", key.N.BitLen())
	}
	if leaf.Subject.CommonName != req.FQDN {
		t.Errorf("leaf CN = %q, want %q", leaf.Subject.CommonName, req.FQDN)
	}
	wantDNS := []string{"svc.example.local", "alt.example.local"}
	if !equalStrings(leaf.DNSNames, wantDNS) {
		t.Errorf("leaf DNSNames = %v, want %v", leaf.DNSNames, wantDNS)
	}
	if len(leaf.IPAddresses) != 1 || !leaf.IPAddresses[0].Equal(req.IPSans[0]) {
		t.Errorf("leaf IPAddresses = %v, want %v", leaf.IPAddresses, req.IPSans)
	}

	var hasServerAuth bool
	for _, eku := range leaf.ExtKeyUsage {
		if eku == x509.ExtKeyUsageServerAuth {
			hasServerAuth = true
		}
	}
	if !hasServerAuth {
		t.Error("leaf ExtKeyUsage does not include ServerAuth")
	}
	wantKeyUsage := x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment
	if leaf.KeyUsage != wantKeyUsage {
		t.Errorf("leaf KeyUsage = %v, want %v (CM-1 deviation from legacy's unset KeyUsage)", leaf.KeyUsage, wantKeyUsage)
	}

	// Verify the leaf actually chains to the CA that signed it.
	pool := x509.NewCertPool()
	pool.AddCert(caCert)
	if _, err := leaf.Verify(x509.VerifyOptions{Roots: pool, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}); err != nil {
		t.Errorf("leaf does not verify against its signing CA: %v", err)
	}
}

func TestGenerateLeafDeduplicatesFQDNFromDNSSans(t *testing.T) {
	caCert, caKey, _ := newThrowawayCA(t, time.Now().AddDate(10, 0, 0))
	req := CertRequest{FQDN: "dup.example.local", DNSSans: []string{"dup.example.local", "other.example.local"}}

	result, err := GenerateLeaf(caCert, caKey, req, 30)
	if err != nil {
		t.Fatalf("GenerateLeaf: %v", err)
	}
	leaf, err := ParseCert(result.CertPEM)
	if err != nil {
		t.Fatalf("ParseCert: %v", err)
	}
	want := []string{"dup.example.local", "other.example.local"}
	if !equalStrings(leaf.DNSNames, want) {
		t.Errorf("leaf DNSNames = %v, want %v (deduplicated)", leaf.DNSNames, want)
	}
}

// FR-4: two consecutive generations must produce different serials, neither
// a plausible timestamp. Legacy used big.NewInt(time.Now().UnixNano())
// (reference/certmachine/main.go:104), which fits comfortably in 63 bits; a
// random 128-bit draw essentially never does.
func TestGenerateLeafSerialsAreRandomNotTimestamps(t *testing.T) {
	caCert, caKey, _ := newThrowawayCA(t, time.Now().AddDate(10, 0, 0))
	req := CertRequest{FQDN: "serial.example.local"}

	r1, err := GenerateLeaf(caCert, caKey, req, 30)
	if err != nil {
		t.Fatalf("first GenerateLeaf: %v", err)
	}
	r2, err := GenerateLeaf(caCert, caKey, req, 30)
	if err != nil {
		t.Fatalf("second GenerateLeaf: %v", err)
	}

	c1, err := ParseCert(r1.CertPEM)
	if err != nil {
		t.Fatalf("ParseCert(c1): %v", err)
	}
	c2, err := ParseCert(r2.CertPEM)
	if err != nil {
		t.Fatalf("ParseCert(c2): %v", err)
	}

	if c1.SerialNumber.Cmp(c2.SerialNumber) == 0 {
		t.Fatal("two consecutive GenerateLeaf calls produced the same serial")
	}
	for _, c := range []*x509.Certificate{c1, c2} {
		if c.SerialNumber.BitLen() < 64 {
			t.Errorf("serial %v has only %d bits, too small to rule out a UnixNano timestamp", c.SerialNumber, c.SerialNumber.BitLen())
		}
	}
}

func TestGenerateLeafClampsToCARemainingLifetime(t *testing.T) {
	shortNotAfter := time.Now().UTC().Add(30 * 24 * time.Hour)
	caCert, caKey, _ := newThrowawayCA(t, shortNotAfter)

	req := CertRequest{FQDN: "clamped.example.local"}
	result, err := GenerateLeaf(caCert, caKey, req, 365)
	if err != nil {
		t.Fatalf("GenerateLeaf: %v", err)
	}

	if !result.Clamped {
		t.Fatal("Clamped = false, want true when requested validity outlives the CA")
	}
	if !result.NotAfter.Equal(caCert.NotAfter) {
		t.Errorf("NotAfter = %v, want the CA's NotAfter %v", result.NotAfter, caCert.NotAfter)
	}
	if result.RequestedNotAfter.Before(result.NotAfter) {
		t.Errorf("RequestedNotAfter = %v, want it to be later than the clamped NotAfter %v", result.RequestedNotAfter, result.NotAfter)
	}

	leaf, err := ParseCert(result.CertPEM)
	if err != nil {
		t.Fatalf("ParseCert: %v", err)
	}
	if !leaf.NotAfter.Equal(caCert.NotAfter) {
		t.Errorf("issued leaf NotAfter = %v, want the CA's NotAfter %v", leaf.NotAfter, caCert.NotAfter)
	}
}

func TestGenerateLeafNoClampWhenWithinCALifetime(t *testing.T) {
	caCert, caKey, _ := newThrowawayCA(t, time.Now().AddDate(10, 0, 0))

	req := CertRequest{FQDN: "unclamped.example.local"}
	result, err := GenerateLeaf(caCert, caKey, req, 365)
	if err != nil {
		t.Fatalf("GenerateLeaf: %v", err)
	}
	if result.Clamped {
		t.Fatal("Clamped = true, want false when the requested validity fits inside the CA's remaining lifetime")
	}
	if !result.NotAfter.Equal(result.RequestedNotAfter) {
		t.Errorf("NotAfter = %v, want RequestedNotAfter %v when unclamped", result.NotAfter, result.RequestedNotAfter)
	}
}

func TestFingerprintMatchesManualSHA256(t *testing.T) {
	der := []byte("not a real certificate, just fingerprint input")
	want := sha256.Sum256(der)

	got := Fingerprint(der)
	parts := strings.Split(got, ":")
	if len(parts) != len(want) {
		t.Fatalf("Fingerprint produced %d groups, want %d", len(parts), len(want))
	}

	rebuilt := strings.ReplaceAll(got, ":", "")
	wantHex := hexColon(want[:])
	if got != wantHex {
		t.Errorf("Fingerprint(der) = %q, want %q", got, wantHex)
	}
	if rebuilt != strings.ReplaceAll(wantHex, ":", "") {
		t.Errorf("Fingerprint hex digits = %q, want %q", rebuilt, strings.ReplaceAll(wantHex, ":", ""))
	}
}

// TestFingerprintMatchesOpenSSLFixture verifies Fingerprint's output against
// `openssl x509 -fingerprint -sha256`, per the slice 4 verification list.
// Skips (rather than fails) when openssl is unavailable on PATH -- this is a
// cross-check against an external tool's convention, not a test of this
// package's own logic (that's TestFingerprintMatchesManualSHA256 above).
func TestFingerprintMatchesOpenSSLFixture(t *testing.T) {
	opensslPath, err := exec.LookPath("openssl")
	if err != nil {
		t.Skip("openssl not found on PATH")
	}

	caCert, _, caCertPEM := newThrowawayCA(t, time.Now().AddDate(10, 0, 0))

	certFile := filepath.Join(t.TempDir(), "cert.pem")
	if err := os.WriteFile(certFile, caCertPEM, 0o600); err != nil {
		t.Fatalf("write cert fixture: %v", err)
	}

	out, err := exec.Command(opensslPath, "x509", "-in", certFile, "-noout", "-fingerprint", "-sha256").Output()
	if err != nil {
		t.Fatalf("openssl x509 -fingerprint -sha256: %v", err)
	}
	// Output looks like "sha256 Fingerprint=AA:BB:...\n" (older openssl:
	// "SHA256 Fingerprint=..."); take everything after the last '='.
	line := strings.TrimSpace(string(out))
	idx := strings.LastIndex(line, "=")
	if idx == -1 {
		t.Fatalf("unexpected openssl output: %q", line)
	}
	want := line[idx+1:]

	got := Fingerprint(caCert.Raw)
	if got != want {
		t.Errorf("Fingerprint = %q, want %q (from openssl)", got, want)
	}
}

func TestSerialStringFormat(t *testing.T) {
	cases := []struct {
		serial int64
		want   string
	}{
		{1, "01"},
		{255, "FF"},
		{256, "01:00"},
	}
	for _, c := range cases {
		got := SerialString(big.NewInt(c.serial))
		if got != c.want {
			t.Errorf("SerialString(%d) = %q, want %q", c.serial, got, c.want)
		}
	}
}

func TestKeysMatch(t *testing.T) {
	caCert, caKey, _ := newThrowawayCA(t, time.Now().AddDate(10, 0, 0))
	otherKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate other key: %v", err)
	}

	if !keysMatch(caCert, caKey) {
		t.Error("keysMatch(cert, its own key) = false, want true")
	}
	if keysMatch(caCert, otherKey) {
		t.Error("keysMatch(cert, unrelated key) = true, want false")
	}
}

func TestNormalizeFQDN(t *testing.T) {
	got, err := NormalizeFQDN(" Foo.Example.Local. ")
	if err != nil {
		t.Fatalf("NormalizeFQDN: %v", err)
	}
	if got != "foo.example.local" {
		t.Errorf("NormalizeFQDN = %q, want %q", got, "foo.example.local")
	}
}

func TestNormalizeFQDNRejectsEmpty(t *testing.T) {
	for _, raw := range []string{"", "   ", "."} {
		if _, err := NormalizeFQDN(raw); !errors.Is(err, ErrValidation) {
			t.Errorf("NormalizeFQDN(%q) err = %v, want ErrValidation", raw, err)
		}
	}
}

func TestNormalizeFQDNRejectsNonASCII(t *testing.T) {
	if _, err := NormalizeFQDN("café.example.local"); !errors.Is(err, ErrValidation) {
		t.Errorf("NormalizeFQDN(non-ASCII) err = %v, want ErrValidation", err)
	}
}

func TestValidateRequestWildcardTable(t *testing.T) {
	cases := []struct {
		name    string
		fqdn    string
		wantErr bool
	}{
		{"plain", "svc.example.local", false},
		{"leading wildcard", "*.example.local", false},
		{"bare wildcard", "*", true},
		{"wildcard not leftmost", "foo.*.local", true},
		{"double wildcard", "*.*.local", true},
		{"wildcard mid-label", "ab*.example.local", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := ValidateRequest(c.fqdn, nil, nil)
			if c.wantErr && !errors.Is(err, ErrValidation) {
				t.Errorf("ValidateRequest(%q) err = %v, want ErrValidation", c.fqdn, err)
			}
			if !c.wantErr && err != nil {
				t.Errorf("ValidateRequest(%q) err = %v, want nil", c.fqdn, err)
			}
			if c.wantErr && err != nil && !strings.Contains(err.Error(), c.fqdn) {
				t.Errorf("ValidateRequest(%q) err = %v, does not name the offending input", c.fqdn, err)
			}
		})
	}
}

func TestValidateRequestAcceptsWildcardInDNSSans(t *testing.T) {
	req, err := ValidateRequest("svc.example.local", []string{"*.example.local"}, nil)
	if err != nil {
		t.Fatalf("ValidateRequest: %v", err)
	}
	if len(req.DNSSans) != 1 || req.DNSSans[0] != "*.example.local" {
		t.Errorf("DNSSans = %v, want [*.example.local]", req.DNSSans)
	}
}

func TestValidateRequestRejectsWildcardInDNSSans(t *testing.T) {
	_, err := ValidateRequest("svc.example.local", []string{"foo.*.local"}, nil)
	if !errors.Is(err, ErrValidation) {
		t.Errorf("ValidateRequest err = %v, want ErrValidation", err)
	}
	if !strings.Contains(err.Error(), "foo.*.local") {
		t.Errorf("ValidateRequest err = %v, does not name the offending SAN", err)
	}
}

func TestValidateRequestNormalizesFQDNAndSans(t *testing.T) {
	req, err := ValidateRequest(" Svc.Example.Local. ", []string{" Alt.Example.Local. "}, nil)
	if err != nil {
		t.Fatalf("ValidateRequest: %v", err)
	}
	if req.FQDN != "svc.example.local" {
		t.Errorf("FQDN = %q, want %q", req.FQDN, "svc.example.local")
	}
	if len(req.DNSSans) != 1 || req.DNSSans[0] != "alt.example.local" {
		t.Errorf("DNSSans = %v, want [alt.example.local]", req.DNSSans)
	}
}

func TestValidateRequestRejectsEmptyFQDN(t *testing.T) {
	if _, err := ValidateRequest("", nil, nil); !errors.Is(err, ErrValidation) {
		t.Errorf("ValidateRequest(\"\") err = %v, want ErrValidation", err)
	}
}

func TestValidateRequestParsesIPSans(t *testing.T) {
	req, err := ValidateRequest("svc.example.local", nil, []string{"10.0.0.9", "::1"})
	if err != nil {
		t.Fatalf("ValidateRequest: %v", err)
	}
	if len(req.IPSans) != 2 {
		t.Fatalf("IPSans = %v, want 2 entries", req.IPSans)
	}
	if !req.IPSans[0].Equal(net.ParseIP("10.0.0.9")) || !req.IPSans[1].Equal(net.ParseIP("::1")) {
		t.Errorf("IPSans = %v, want [10.0.0.9, ::1]", req.IPSans)
	}
}

func TestValidateRequestRejectsUnparseableIPSan(t *testing.T) {
	_, err := ValidateRequest("svc.example.local", nil, []string{"not-an-ip"})
	if !errors.Is(err, ErrValidation) {
		t.Errorf("ValidateRequest err = %v, want ErrValidation", err)
	}
	if !strings.Contains(err.Error(), "not-an-ip") {
		t.Errorf("ValidateRequest err = %v, does not name the offending value", err)
	}
}

// FR-4: 64 SANs (FQDN + dnsSans, deduplicated, as they will actually appear
// in the issued certificate) is accepted; 65 is a 400.
func TestValidateRequestSANCapBoundary(t *testing.T) {
	makeSans := func(n int) []string {
		sans := make([]string, n)
		for i := range sans {
			sans[i] = fmt.Sprintf("alt%d.example.local", i)
		}
		return sans
	}

	// FQDN (1) + 63 extra sans = 64 total, must be accepted.
	if _, err := ValidateRequest("svc.example.local", makeSans(63), nil); err != nil {
		t.Errorf("64 total SANs: ValidateRequest err = %v, want nil", err)
	}

	// FQDN (1) + 64 extra sans = 65 total, must be rejected.
	_, err := ValidateRequest("svc.example.local", makeSans(64), nil)
	if !errors.Is(err, ErrValidation) {
		t.Errorf("65 total SANs: ValidateRequest err = %v, want ErrValidation", err)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
