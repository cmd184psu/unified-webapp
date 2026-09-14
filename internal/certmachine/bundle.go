// bundle.go builds the download artifacts served to operators: the
// single-file haproxy.pem assembly (slice 3's HAProxyPEM plus this slice's
// root-match guard), the gzipped tarball bundle, and the filename-safety
// helper both routes rely on for Content-Disposition (FR-7).
package certmachine

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// HAProxyPEM assembles the single-file bundle HAProxy consumes: leaf, then
// root CA, then private key, in that exact order -- copied from
// reference/certmachine/main.go:120-140 and must not be rearranged (binding
// decision: this is the byte layout the existing HAProxy deployment already
// consumes). Each argument is already PEM-encoded, so this is a pure
// concatenation; every byte in the output comes from the caller.
func HAProxyPEM(certPEM, caCertPEM, keyPEM []byte) []byte {
	out := make([]byte, 0, len(certPEM)+len(caCertPEM)+len(keyPEM))
	out = append(out, certPEM...)
	out = append(out, caCertPEM...)
	out = append(out, keyPEM...)
	return out
}

// Sentinel errors for the download builders below. Both are 409s per the
// route table (§FR-7): the request is well-formed and the row exists, but
// the artifact cannot be assembled without either emitting a confidently
// wrong chain or dereferencing a key that was never stored.
var (
	// ErrQuarantinedDownload means cert.Status is quarantined: assembly
	// needs a parseable cert and a matching key, and a quarantined row is
	// exactly the row that lacks one or both (FR-7). This is checked before
	// either PEM field is ever read, so a quarantined row with a NULL
	// cert_pem or key_pem (schema.go: both columns are nullable) yields this
	// deterministic sentinel rather than a nil dereference. The raw cert.pem
	// download is unaffected -- it goes through GetCert, never this guard.
	ErrQuarantinedDownload = errors.New("certmachine: cannot assemble haproxy.pem or a bundle from a quarantined certificate")

	// ErrRootMismatch means the leaf does not verify against the stored ca
	// row -- exactly the rows import marks with import_warning (slice 7).
	// Renew re-issues the same CN and SAN set under the stored CA and
	// genuinely clears this in one action, which is why the message always
	// names that remedy rather than reading as a dead end.
	ErrRootMismatch = errors.New("certmachine: certificate does not chain to the stored certificate authority; renew it to re-issue under the stored root")
)

// checkDownloadable is the root-match guard shared by AssembleHAProxyPEM and
// BundleTGZ (§2.1 Principle 2 -- never emit a confidently wrong chain).
// cert must carry both cert_pem and key_pem (getCertWithKey); caCertPEM is
// the stored ca row's cert_pem.
//
// Order matters: quarantine status is checked first, before either PEM
// field on cert is dereferenced, because a quarantined row's cert_pem and/or
// key_pem can be NULL. Only a non-quarantined row's leaf is parsed and
// checked against caCertPEM with CheckSignatureFrom.
func checkDownloadable(cert Cert, caCertPEM []byte) error {
	if cert.Status == StatusQuarantined {
		reason := ""
		if cert.QuarantineReason != nil {
			reason = *cert.QuarantineReason
		}
		return fmt.Errorf("%w: %s", ErrQuarantinedDownload, reason)
	}
	if cert.CertPEM == nil {
		return fmt.Errorf("certmachine: cert %d has no stored certificate", cert.ID)
	}
	if cert.KeyPEM == nil {
		return fmt.Errorf("certmachine: cert %d has no stored private key", cert.ID)
	}

	leaf, err := ParseCert([]byte(*cert.CertPEM))
	if err != nil {
		return fmt.Errorf("certmachine: parse stored cert %d: %w", cert.ID, err)
	}
	ca, err := ParseCert(caCertPEM)
	if err != nil {
		return fmt.Errorf("certmachine: parse stored ca cert: %w", err)
	}
	if err := leaf.CheckSignatureFrom(ca); err != nil {
		warning := ""
		if cert.ImportWarning != nil {
			warning = *cert.ImportWarning
		}
		return fmt.Errorf("%w: %s", ErrRootMismatch, warning)
	}
	return nil
}

// AssembleHAProxyPEM builds the single-file haproxy.pem download for cert,
// applying the root-match guard (checkDownloadable) before calling the pure
// HAProxyPEM concatenation. cert must carry both cert_pem and key_pem
// (getCertWithKey); caCertPEM is the stored ca row's cert_pem.
func AssembleHAProxyPEM(cert Cert, caCertPEM []byte) ([]byte, error) {
	if err := checkDownloadable(cert, caCertPEM); err != nil {
		return nil, err
	}
	return HAProxyPEM([]byte(*cert.CertPEM), caCertPEM, []byte(*cert.KeyPEM)), nil
}

// bundleEntry is one file inside the tarball BundleTGZ produces.
type bundleEntry struct {
	name string
	mode int64
	data []byte
}

// BundleTGZ builds the gzipped tar download for cert (FR-7): cert.pem
// (0644), key.pem (0600), haproxy.pem (0600), rootCA.crt (0644). caCertPEM
// is the stored ca row's cert_pem, embedded verbatim as rootCA.crt and used
// to assemble haproxy.pem. The root-match guard (checkDownloadable) runs
// before any entry is written, so a leaf that does not chain to caCertPEM,
// or a quarantined row, never produces a partial or wrong archive -- it
// returns an error instead. cert must carry both cert_pem and key_pem
// (getCertWithKey).
func BundleTGZ(cert Cert, caCertPEM []byte) ([]byte, error) {
	if err := checkDownloadable(cert, caCertPEM); err != nil {
		return nil, err
	}

	certPEM := []byte(*cert.CertPEM)
	keyPEM := []byte(*cert.KeyPEM)
	haproxyPEM := HAProxyPEM(certPEM, caCertPEM, keyPEM)

	entries := []bundleEntry{
		{name: "cert.pem", mode: 0o644, data: certPEM},
		{name: "key.pem", mode: 0o600, data: keyPEM},
		{name: "haproxy.pem", mode: 0o600, data: haproxyPEM},
		{name: "rootCA.crt", mode: 0o644, data: caCertPEM},
	}

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	now := time.Now()
	for _, e := range entries {
		hdr := &tar.Header{
			Name:     e.name,
			Mode:     e.mode,
			Size:     int64(len(e.data)),
			ModTime:  now,
			Typeflag: tar.TypeReg,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return nil, fmt.Errorf("certmachine: write tar header %s: %w", e.name, err)
		}
		if _, err := tw.Write(e.data); err != nil {
			return nil, fmt.Errorf("certmachine: write tar entry %s: %w", e.name, err)
		}
	}
	if err := tw.Close(); err != nil {
		return nil, fmt.Errorf("certmachine: close tar writer: %w", err)
	}
	if err := gz.Close(); err != nil {
		return nil, fmt.Errorf("certmachine: close gzip writer: %w", err)
	}
	return buf.Bytes(), nil
}

// safeFilenameDisallowed matches every character outside the filename-safe
// set [A-Za-z0-9._-]; safeFilenameDotRun matches a run of two or more dots
// (the traversal shape "..", even after the character pass leaves lone dots
// alone); safeFilenameAllFiller matches a result that is nothing but
// underscores -- including the empty string -- the "sanitizes to empty"
// fallback condition.
var (
	safeFilenameDisallowed = regexp.MustCompile(`[^A-Za-z0-9._-]`)
	safeFilenameDotRun     = regexp.MustCompile(`\.{2,}`)
	safeFilenameAllFiller  = regexp.MustCompile(`^_*$`)
)

// SafeFilename converts fqdn into a filename-safe basename for
// Content-Disposition (FR-7, FR-9, §2.1 Principle 2): a leading "*."
// wildcard becomes "_wildcard.", every character outside
// [A-Za-z0-9._-] becomes "_", and any run of two or more dots collapses to a
// single "_" (defeating ".." even though a lone "." is otherwise allowed). A
// result that sanitizes down to nothing but underscores -- including the
// empty string -- falls back to "cert". Filenames are always derived from
// the DB row's fqdn, never from any request path or query value.
func SafeFilename(fqdn string) string {
	s := fqdn
	if strings.HasPrefix(s, "*.") {
		s = "_wildcard." + s[2:]
	}
	s = safeFilenameDisallowed.ReplaceAllString(s, "_")
	s = safeFilenameDotRun.ReplaceAllString(s, "_")
	if safeFilenameAllFiller.MatchString(s) {
		return "cert"
	}
	return s
}
