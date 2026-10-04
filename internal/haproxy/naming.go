package haproxy

// Cert file naming (FRD §6.2, D16). The editor owns the certs directory and
// names every file itself from CertMachine's data, by a fixed, content-
// addressed convention:
//
//	<safe-fqdn>-<first 12 hex of the bundle's SHA-256>.pem
//
// The hash part makes a name identify its contents: a new or changed bundle is
// a new file, so Update and rollback never overwrite in place. A file that
// does not follow the convention is left alone and only counted (NamingIsManaged).

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
)

var (
	// namingDisallowed matches every character outside the filename-safe set
	// [A-Za-z0-9._-]; namingDotRun a run of two or more dots; namingAllFiller a
	// result that is nothing but underscores. These reimplement CertMachine's
	// SafeFilename rule (internal/certmachine/bundle.go) without importing it.
	namingDisallowed = regexp.MustCompile(`[^A-Za-z0-9._-]`)
	namingDotRun     = regexp.MustCompile(`\.{2,}`)
	namingAllFiller  = regexp.MustCompile(`^_*$`)
	// namingConvention recognizes a managed name: a non-empty safe-fqdn, a
	// hyphen, exactly 12 lowercase hex, then ".pem".
	namingConvention = regexp.MustCompile(`^[A-Za-z0-9._-]+-[0-9a-f]{12}\.pem$`)
)

// NamingSafeFQDN makes an FQDN filesystem-safe by CertMachine's own rule: a
// leading "*." becomes "_wildcard.", anything unsafe becomes "_", runs of dots
// collapse, and an all-filler result falls back to "cert".
func NamingSafeFQDN(fqdn string) string {
	s := fqdn
	if strings.HasPrefix(s, "*.") {
		s = "_wildcard." + s[2:]
	}
	s = namingDisallowed.ReplaceAllString(s, "_")
	s = namingDotRun.ReplaceAllString(s, "_")
	if namingAllFiller.MatchString(s) {
		return "cert"
	}
	return s
}

// NamingFileName returns the managed file name for a cert FQDN and the exact
// bundle bytes. It is deterministic and content-addressed: identical bytes
// yield an identical name; different bytes yield a different name.
func NamingFileName(fqdn string, bundle []byte) string {
	sum := sha256.Sum256(bundle)
	return NamingSafeFQDN(fqdn) + "-" + hex.EncodeToString(sum[:])[:12] + ".pem"
}

// NamingValidateName rejects a name that is empty or carries a path separator
// or traversal, so a name can never escape the certs directory when joined to
// it. A name produced by NamingFileName always passes.
func NamingValidateName(name string) error {
	if name == "" {
		return fmt.Errorf("haproxy: empty cert file name")
	}
	if strings.ContainsAny(name, `/\`) || strings.Contains(name, "..") {
		return fmt.Errorf("haproxy: unsafe cert file name %q", name)
	}
	return nil
}

// NamingIsManaged reports whether a file name follows the convention, i.e. the
// editor owns it. A file that does not is never touched (FR-H10, D16).
func NamingIsManaged(name string) bool {
	return namingConvention.MatchString(name)
}
