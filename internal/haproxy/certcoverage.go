package haproxy

// Coverage (FRD §6.5, D12, FR-H18) and freshness (§6.4, FR-H46). Both consult
// CertMachine's own SAN list and active-cert id; neither reads a cert file.
// Coverage is a WARNING, never a blocker; when CertMachine is unreachable it is
// "unknown", not an error.

import (
	"context"
	"strings"
)

// CoverageStatus is a service's coverage outcome.
type CoverageStatus string

const (
	CoverageCovered CoverageStatus = "covered"
	CoverageWarning CoverageStatus = "warning"
	CoverageUnknown CoverageStatus = "unknown"
)

// CoverageService is the minimal view of a service the check needs: its name,
// the FQDN(s) it serves, and the FQDN of the cert it is linked to. (Defined
// here so this task does not depend on the B2 model type.)
type CoverageService struct {
	Name     string
	FQDNs    []string
	CertFQDN string
}

// CoverageResult is one service's outcome.
type CoverageResult struct {
	Service   string         `json:"service"`
	CertFQDN  string         `json:"certFqdn"`
	Covered   []string       `json:"covered"`
	Uncovered []string       `json:"uncovered"`
	Status    CoverageStatus `json:"status"`
}

// CoverageLookup returns the names (FQDN plus SANs) a cert covers, as reported
// by CertMachine, or an error (e.g. unreachable) which yields an "unknown"
// result rather than a failure.
type CoverageLookup func(certFQDN string) (names []string, err error)

// CoverageCheck classifies each service against its linked cert's names. One
// cert may cover several services; each is checked independently.
func CoverageCheck(services []CoverageService, lookup CoverageLookup) []CoverageResult {
	results := make([]CoverageResult, 0, len(services))
	for _, svc := range services {
		r := CoverageResult{Service: svc.Name, CertFQDN: svc.CertFQDN}
		names, err := lookup(svc.CertFQDN)
		if err != nil {
			r.Status = CoverageUnknown
			results = append(results, r)
			continue
		}
		for _, fqdn := range svc.FQDNs {
			if coverageCoveredBy(fqdn, names) {
				r.Covered = append(r.Covered, fqdn)
			} else {
				r.Uncovered = append(r.Uncovered, fqdn)
			}
		}
		if len(r.Uncovered) > 0 {
			r.Status = CoverageWarning
		} else {
			r.Status = CoverageCovered
		}
		results = append(results, r)
	}
	return results
}

// coverageCoveredBy reports whether fqdn matches any of the cert's names.
func coverageCoveredBy(fqdn string, names []string) bool {
	for _, n := range names {
		if coverageMatches(fqdn, n) {
			return true
		}
	}
	return false
}

// coverageMatches is case-insensitive exact matching plus the standard TLS
// single-label wildcard ("*.example" matches "a.example" but not "a.b.example").
func coverageMatches(name, san string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	san = strings.ToLower(strings.TrimSpace(san))
	if name == san {
		return true
	}
	if strings.HasPrefix(san, "*.") {
		suffix := san[1:] // ".example"
		if !strings.HasSuffix(name, suffix) {
			return false
		}
		label := name[:len(name)-len(suffix)]
		return label != "" && !strings.Contains(label, ".")
	}
	return false
}

// CertFreshness is FR-H46's three-way result.
type CertFreshness string

const (
	CertFreshUpToDate        CertFreshness = "up to date"
	CertFreshUpdateAvailable CertFreshness = "update available"
	CertFreshNoActiveCert    CertFreshness = "no active cert"
)

// CertFreshnessFor asks CertMachine for the active cert for the recorded FQDN
// and compares ids: a different id means a re-issue/edit/CA-replacement, so an
// update is available; the same id is up to date; no active cert is reported
// plainly. No key material is downloaded — only the list call.
func CertFreshnessFor(ctx context.Context, lookup CertActiveLookup, recordedID int64, fqdn string) (CertFreshness, error) {
	active, err := lookup.ActiveCertForFQDN(ctx, fqdn)
	if err != nil {
		return "", err
	}
	if active == nil {
		return CertFreshNoActiveCert, nil
	}
	if active.ID == recordedID {
		return CertFreshUpToDate, nil
	}
	return CertFreshUpdateAvailable, nil
}
