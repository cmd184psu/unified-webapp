// pki.go holds the certmachine module's cryptographic primitives: parsing,
// generation, fingerprinting, serial formatting, and request validation.
// ImportCA (the store-level orchestration that uses ParseCert/ParseKey/
// Fingerprint below) lives in importer.go; HAProxyPEM lives in bundle.go.
// NormalizeFQDN and ValidateRequest (slice 4) are the only supported way to
// build a CertRequest from user input -- GenerateLeaf itself performs no
// validation and trusts its caller completely.
package certmachine

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
)

// Legacy root CA filenames inside legacy_import_dir
// (reference/certmachine/main.go:89-90).
const (
	legacyRootCertFilename = "rootCA.crt"
	legacyRootKeyFilename  = "rootCA.key"
)

// PEM block type strings. These are literal matches for
// reference/certmachine/main.go:124,131,136,199,204 -- ParseCert, ParseKey
// and HAProxyPEM's callers all depend on these exact strings, not merely on
// the DER shape underneath them.
const (
	pemTypeCertificate   = "CERTIFICATE"
	pemTypeRSAPrivateKey = "RSA PRIVATE KEY"
)

// CertRequest is the already-normalized shape GenerateLeaf consumes.
// ValidateRequest is the only supported way to build one from raw API input;
// GenerateLeaf itself performs no validation.
type CertRequest struct {
	FQDN    string
	DNSSans []string
	IPSans  []net.IP
}

// maxSANCount is FR-4's structural guard against a huge dns_sans/ip_sans
// array on an unauthenticated endpoint (Principle 2: a two-line guard beats
// letting free CPU be spent parsing/signing an arbitrarily large SAN list).
// It counts every SAN that will actually land in the issued certificate --
// req.FQDN plus DNSSans, deduplicated exactly as GenerateLeaf deduplicates
// them, plus IPSans -- not merely the length of the request's dnsSans array,
// so the cap cannot be bypassed by relying on GenerateLeaf's auto-injection
// of the FQDN as a DNS SAN.
const maxSANCount = 64

// ErrValidation is the sentinel every ValidateRequest/NormalizeFQDN
// rejection wraps. A single sentinel (rather than one per rejection reason)
// keeps slice 8's error-to-status mapping a one-line errors.Is check --
// every ValidateRequest failure is a structural 400, never a 409 or 500 --
// while fmt.Errorf's %w still lets each specific message name the offending
// input, per Principle 4.
var ErrValidation = errors.New("certmachine: invalid certificate request")

// NormalizeFQDN trims surrounding whitespace, strips a single trailing dot,
// and lowercases raw. It is used both for the request's primary FQDN and for
// each DNS SAN, since both end up in the same certificate's DNSNames list
// and must normalize identically to dedupe correctly.
//
// IDN/punycode is explicitly out of scope (CM-3): raw is never converted,
// and any non-ASCII byte is rejected outright rather than silently mangled.
func NormalizeFQDN(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	trimmed = strings.TrimSuffix(trimmed, ".")
	if trimmed == "" {
		return "", fmt.Errorf("%w: hostname is required", ErrValidation)
	}
	for i := 0; i < len(trimmed); i++ {
		if trimmed[i] > unicode.MaxASCII {
			return "", fmt.Errorf("%w: %q contains non-ASCII characters (IDN/punycode is not supported)", ErrValidation, raw)
		}
	}
	return strings.ToLower(trimmed), nil
}

// validateWildcardName enforces FR-4's wildcard rules on one already-
// normalized DNS name (the FQDN or a single DNS SAN): a "*", if present,
// must be the entire leftmost label of a name that has at least one further
// label -- so "*.example.local" is accepted, while a bare "*", a "*"
// elsewhere in the leftmost label ("ab*.local"), and a "*" outside the
// leftmost label ("foo.*.local") are all rejected as "not a leading
// *.example.local wildcard". More than one "*" anywhere in the name
// ("*.*.local") is rejected separately, before that check. Every rejection
// names the offending name.
func validateWildcardName(name string) error {
	if strings.Count(name, "*") > 1 {
		return fmt.Errorf("%w: %q has more than one wildcard", ErrValidation, name)
	}
	if strings.Contains(name, "*") {
		labels := strings.SplitN(name, ".", 2)
		if labels[0] != "*" || len(labels) < 2 {
			return fmt.Errorf("%w: %q has an invalid wildcard (only a leading *.example.local wildcard is valid)", ErrValidation, name)
		}
	}
	return nil
}

// ValidateRequest is the only supported way to turn raw API input (fqdn,
// dnsSans, ipSans -- the POST /api/certs body, FR-4) into the CertRequest
// GenerateLeaf consumes. It normalizes fqdn and every dnsSans entry with
// NormalizeFQDN, enforces the wildcard rules on all of them, parses ipSans
// with net.ParseIP, and caps the total SAN count (see maxSANCount) that will
// actually appear in the issued certificate. Every rejection wraps
// ErrValidation and names the offending input.
func ValidateRequest(fqdn string, dnsSans []string, ipSans []string) (CertRequest, error) {
	// Cheap pre-check on the raw array lengths, before any per-entry work.
	// The authoritative cap is the deduplicated count below; this one exists
	// so a body with 100,000 SAN entries is rejected without first running
	// NormalizeFQDN over every one of them. It is a real tightening, not just
	// an optimization: a request whose dnsSans array exceeds the cap is
	// refused even if the entries would have deduplicated down under it.
	if len(dnsSans)+len(ipSans) > maxSANCount {
		return CertRequest{}, fmt.Errorf("%w: %d subject alternative names exceeds the maximum of %d",
			ErrValidation, len(dnsSans)+len(ipSans), maxSANCount)
	}

	normFQDN, err := NormalizeFQDN(fqdn)
	if err != nil {
		return CertRequest{}, err
	}
	if err := validateWildcardName(normFQDN); err != nil {
		return CertRequest{}, err
	}

	normSans := make([]string, 0, len(dnsSans))
	for _, raw := range dnsSans {
		n, err := NormalizeFQDN(raw)
		if err != nil {
			return CertRequest{}, err
		}
		if err := validateWildcardName(n); err != nil {
			return CertRequest{}, err
		}
		normSans = append(normSans, n)
	}

	ips := make([]net.IP, 0, len(ipSans))
	for _, raw := range ipSans {
		ip := net.ParseIP(raw)
		if ip == nil {
			return CertRequest{}, fmt.Errorf("%w: %q is not a valid IP address", ErrValidation, raw)
		}
		ips = append(ips, ip)
	}

	total := len(dedupeDNSNames(normFQDN, normSans)) + len(ips)
	if total > maxSANCount {
		return CertRequest{}, fmt.Errorf("%w: %d subject alternative names exceeds the maximum of %d", ErrValidation, total, maxSANCount)
	}

	return CertRequest{FQDN: normFQDN, DNSSans: normSans, IPSans: ips}, nil
}

// LeafResult is what GenerateLeaf produces for one issued certificate.
//
// RequestedNotAfter and Clamped report the FR-4 validity clamp: when the
// requested validity would outlive the signing CA, NotAfter is pulled back to
// the CA's own NotAfter and Clamped is set, so a caller can tell the operator
// exactly what happened rather than silently issuing a shorter cert
// (Principle 4 -- nothing is silently dropped or swallowed).
type LeafResult struct {
	CertPEM []byte
	KeyPEM  []byte

	NotBefore         time.Time
	NotAfter          time.Time
	RequestedNotAfter time.Time
	Clamped           bool
}

// ParseCert decodes a single PEM-encoded certificate.
func ParseCert(pemBytes []byte) (*x509.Certificate, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, errors.New("certmachine: no PEM block found in certificate data")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("certmachine: parse certificate: %w", err)
	}
	return cert, nil
}

// ParseKey decodes a single PEM-encoded PKCS#1 RSA private key. PKCS#1 (PEM
// type "RSA PRIVATE KEY") is the only format this module ever writes -- a
// binding decision so imported and generated keys share one format and one
// parser -- so it is also the only format ParseKey needs to read.
func ParseKey(pemBytes []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, errors.New("certmachine: no PEM block found in key data")
	}
	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("certmachine: parse private key: %w", err)
	}
	return key, nil
}

// ReadLegacyRoot reads the legacy root CA's certificate and key verbatim
// from dir (rootCA.crt / rootCA.key), returning their raw bytes exactly as
// stored on disk. FR-3 requires byte-for-byte preservation of the inherited
// PKI, so nothing here re-encodes what it reads.
func ReadLegacyRoot(dir string) (certPEM, keyPEM []byte, err error) {
	certPEM, err = os.ReadFile(filepath.Join(dir, legacyRootCertFilename))
	if err != nil {
		return nil, nil, fmt.Errorf("certmachine: read legacy root cert: %w", err)
	}
	keyPEM, err = os.ReadFile(filepath.Join(dir, legacyRootKeyFilename))
	if err != nil {
		return nil, nil, fmt.Errorf("certmachine: read legacy root key: %w", err)
	}
	return certPEM, keyPEM, nil
}

// Fingerprint returns der's SHA-256 fingerprint formatted as colon-separated
// uppercase hex, matching `openssl x509 -fingerprint -sha256` (minus its
// "SHA256 Fingerprint=" label).
func Fingerprint(der []byte) string {
	sum := sha256.Sum256(der)
	return hexColon(sum[:])
}

// SerialString formats a certificate serial number as colon-separated
// uppercase hex, the same convention Fingerprint uses, so ca.serial and
// certs.serial read consistently with ca.fingerprint / certs.fingerprint.
func SerialString(serial *big.Int) string {
	b := serial.Bytes()
	if len(b) == 0 {
		b = []byte{0}
	}
	return hexColon(b)
}

// hexColon formats b as colon-separated uppercase hex pairs.
func hexColon(b []byte) string {
	parts := make([]string, len(b))
	for i, v := range b {
		parts[i] = fmt.Sprintf("%02X", v)
	}
	return strings.Join(parts, ":")
}

// randomSerial returns a random, non-zero 128-bit serial number. FR-4:
// legacy used big.NewInt(time.Now().UnixNano())
// (reference/certmachine/main.go:104), which collides for two certs minted
// in the same nanosecond and leaks mint time in the serial itself. Every
// serial minted by this package is random instead, retried on the
// vanishingly unlikely zero draw.
func randomSerial() (*big.Int, error) {
	limit := new(big.Int).Lsh(big.NewInt(1), 128)
	for i := 0; i < 5; i++ {
		n, err := rand.Int(rand.Reader, limit)
		if err != nil {
			return nil, fmt.Errorf("certmachine: generate serial: %w", err)
		}
		if n.Sign() != 0 {
			return n, nil
		}
	}
	return nil, errors.New("certmachine: generate serial: repeated zero draws")
}

// subjectKeyID derives a conventional SHA-1-of-public-key SubjectKeyId. The
// legacy root has no SubjectKeyId at all (binding decision) -- roots this
// package generates do.
func subjectKeyID(pub *rsa.PublicKey) ([]byte, error) {
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return nil, fmt.Errorf("certmachine: marshal public key: %w", err)
	}
	sum := sha1.Sum(der)
	return sum[:], nil
}

// keysMatch reports whether key's public half matches cert's. ImportCA
// (importer.go) applies this to the legacy root before storing it; the
// analogous per-leaf check is slice 7's scan/classify table.
func keysMatch(cert *x509.Certificate, key *rsa.PrivateKey) bool {
	pub, ok := cert.PublicKey.(*rsa.PublicKey)
	if !ok {
		return false
	}
	return pub.E == key.PublicKey.E && pub.N.Cmp(key.PublicKey.N) == 0
}

// encodeCertPEM PEM-encodes a certificate DER exactly as legacy's writeCert
// does (reference/certmachine/main.go:197-200).
func encodeCertPEM(der []byte) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: pemTypeCertificate, Bytes: der})
}

// encodeKeyPEM PEM-encodes an RSA private key as PKCS#1, exactly as legacy's
// writeKey does (reference/certmachine/main.go:202-205).
func encodeKeyPEM(key *rsa.PrivateKey) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: pemTypeRSAPrivateKey, Bytes: x509.MarshalPKCS1PrivateKey(key)})
}

// GenerateCA creates a new root CA: RSA-4096, ~10-year validity, CN
// "CertMachine Root CA", and a random 128-bit serial -- not legacy's
// big.NewInt(1) (reference/certmachine/main.go:79); this only affects roots
// this code creates, never an imported one.
func GenerateCA() (certPEM, keyPEM []byte, err error) {
	key, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		return nil, nil, fmt.Errorf("certmachine: generate ca key: %w", err)
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
		Subject:               pkix.Name{CommonName: "CertMachine Root CA"},
		NotBefore:             now,
		NotAfter:              now.AddDate(10, 0, 0),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		SubjectKeyId:          ski,
	}

	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		return nil, nil, fmt.Errorf("certmachine: create ca certificate: %w", err)
	}
	return encodeCertPEM(der), encodeKeyPEM(key), nil
}

// GenerateLeaf issues a leaf certificate for req, signed by caCert/caKey,
// valid for validityDays (always default_validity_days -- FR-4 forbids a
// per-cert override) unless that would outlive the CA, in which case the
// leaf's NotAfter is clamped to caCert.NotAfter and LeafResult.Clamped is
// set. The clamp is owned here, not by a validator: it is not a rejection,
// and two owners could disagree about the resulting NotAfter.
//
// req.FQDN is always the certificate's CN and its first DNS SAN
// (deduplicated against req.DNSSans), so a generated leaf always validates
// against its own hostname without the caller having to repeat it.
//
// KeyUsage is DigitalSignature|KeyEncipherment plus ExtKeyUsageServerAuth.
// Legacy sets no KeyUsage at all (reference/certmachine/main.go:103-110);
// modern clients expect it and it does not affect chain validation against
// the existing root -- a deliberate deviation from parity (open question
// CM-1), not an oversight.
func GenerateLeaf(caCert *x509.Certificate, caKey *rsa.PrivateKey, req CertRequest, validityDays int) (LeafResult, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return LeafResult{}, fmt.Errorf("certmachine: generate leaf key: %w", err)
	}
	serial, err := randomSerial()
	if err != nil {
		return LeafResult{}, err
	}

	notBefore := time.Now().UTC()
	requestedNotAfter := notBefore.AddDate(0, 0, validityDays)
	notAfter := requestedNotAfter
	clamped := false
	if notAfter.After(caCert.NotAfter) {
		notAfter = caCert.NotAfter
		clamped = true
	}

	tpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: req.FQDN},
		NotBefore:    notBefore,
		NotAfter:     notAfter,
		DNSNames:     dedupeDNSNames(req.FQDN, req.DNSSans),
		IPAddresses:  req.IPSans,
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}

	der, err := x509.CreateCertificate(rand.Reader, tpl, caCert, &key.PublicKey, caKey)
	if err != nil {
		return LeafResult{}, fmt.Errorf("certmachine: create leaf certificate: %w", err)
	}

	return LeafResult{
		CertPEM:           encodeCertPEM(der),
		KeyPEM:            encodeKeyPEM(key),
		NotBefore:         notBefore,
		NotAfter:          notAfter,
		RequestedNotAfter: requestedNotAfter,
		Clamped:           clamped,
	}, nil
}

// dedupeDNSNames returns fqdn followed by every entry of extra not already
// equal to fqdn, preserving order and introducing no duplicates.
func dedupeDNSNames(fqdn string, extra []string) []string {
	out := make([]string, 0, len(extra)+1)
	out = append(out, fqdn)
	seen := map[string]bool{fqdn: true}
	for _, name := range extra {
		if seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	return out
}
