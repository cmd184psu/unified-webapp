package haproxy

import (
	"context"
	"errors"
	"testing"
)

// A lookup fed from CertMachine's SAN list. CoverageCheck must never read a
// cert file; it only consults these names.
func coverageLookupFrom(byFQDN map[string][]string, err error) CoverageLookup {
	return func(certFQDN string) ([]string, error) {
		if err != nil {
			return nil, err
		}
		return byFQDN[certFQDN], nil
	}
}

func TestCoverageCheckFlagsUncoveredWithoutBlocking(t *testing.T) {
	services := []CoverageService{
		{Name: "brandx", FQDNs: []string{"brandx.cmdhome.net"}, CertFQDN: "brandx.cmdhome.net"},
		{Name: "utuber", FQDNs: []string{"utuber.hero.cmdhome.net"}, CertFQDN: "utuber.cmdhome.net"},
	}
	lookup := coverageLookupFrom(map[string][]string{
		"brandx.cmdhome.net": {"brandx.cmdhome.net"},
		"utuber.cmdhome.net": {"utuber.cmdhome.net"}, // missing the .hero. name
	}, nil)

	results := CoverageCheck(services, lookup)
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}
	byName := map[string]CoverageResult{}
	for _, r := range results {
		byName[r.Service] = r
	}
	if byName["brandx"].Status != CoverageCovered {
		t.Errorf("brandx = %q, want covered", byName["brandx"].Status)
	}
	utuber := byName["utuber"]
	if utuber.Status != CoverageWarning {
		t.Errorf("utuber = %q, want warning (not a blocker)", utuber.Status)
	}
	if len(utuber.Uncovered) != 1 || utuber.Uncovered[0] != "utuber.hero.cmdhome.net" {
		t.Errorf("utuber uncovered = %v, want the .hero. name", utuber.Uncovered)
	}
}

func TestCoverageCheckSharedCertCoversBothServices(t *testing.T) {
	services := []CoverageService{
		{Name: "utuber-a", FQDNs: []string{"utuber.cmdhome.net"}, CertFQDN: "utuber.cmdhome.net"},
		{Name: "utuber-b", FQDNs: []string{"utuber.hero.cmdhome.net"}, CertFQDN: "utuber.cmdhome.net"},
	}
	lookup := coverageLookupFrom(map[string][]string{
		"utuber.cmdhome.net": {"utuber.cmdhome.net", "utuber.hero.cmdhome.net"},
	}, nil)
	for _, r := range CoverageCheck(services, lookup) {
		if r.Status != CoverageCovered {
			t.Errorf("%s = %q, want covered by the shared cert", r.Service, r.Status)
		}
	}
}

func TestCoverageCheckWildcard(t *testing.T) {
	services := []CoverageService{
		{Name: "one-label", FQDNs: []string{"utuber.cmdhome.net"}, CertFQDN: "wild"},
		{Name: "two-labels", FQDNs: []string{"a.b.cmdhome.net"}, CertFQDN: "wild"},
	}
	lookup := coverageLookupFrom(map[string][]string{"wild": {"*.cmdhome.net"}}, nil)
	byName := map[string]CoverageResult{}
	for _, r := range CoverageCheck(services, lookup) {
		byName[r.Service] = r
	}
	if byName["one-label"].Status != CoverageCovered {
		t.Errorf("*.cmdhome.net should cover utuber.cmdhome.net")
	}
	if byName["two-labels"].Status != CoverageWarning {
		t.Errorf("*.cmdhome.net should NOT cover a.b.cmdhome.net")
	}
}

func TestCoverageCheckUnreachableIsUnknown(t *testing.T) {
	services := []CoverageService{{Name: "brandx", FQDNs: []string{"brandx.cmdhome.net"}, CertFQDN: "brandx.cmdhome.net"}}
	lookup := coverageLookupFrom(nil, errors.New("certmachine down"))
	results := CoverageCheck(services, lookup)
	if len(results) != 1 || results[0].Status != CoverageUnknown {
		t.Errorf("unreachable coverage = %+v, want unknown (never a blocker)", results)
	}
}

type fakeActiveLookup struct {
	active *CertMachineCert
	err    error
	calls  int
}

func (f *fakeActiveLookup) ActiveCertForFQDN(ctx context.Context, fqdn string) (*CertMachineCert, error) {
	f.calls++
	return f.active, f.err
}

func TestCertFreshnessThreeCases(t *testing.T) {
	ctx := context.Background()

	// Same id => up to date.
	same := &fakeActiveLookup{active: &CertMachineCert{ID: 12, FQDN: "brandx.cmdhome.net"}}
	if got, err := CertFreshnessFor(ctx, same, 12, "brandx.cmdhome.net"); err != nil || got != CertFreshUpToDate {
		t.Errorf("same id = %q (%v), want up to date", got, err)
	}

	// Different id => update available, and no key was downloaded (only the
	// active lookup was consulted).
	diff := &fakeActiveLookup{active: &CertMachineCert{ID: 13, FQDN: "brandx.cmdhome.net"}}
	if got, err := CertFreshnessFor(ctx, diff, 12, "brandx.cmdhome.net"); err != nil || got != CertFreshUpdateAvailable {
		t.Errorf("different id = %q (%v), want update available", got, err)
	}
	if diff.calls != 1 {
		t.Errorf("freshness made %d lookups, want exactly 1 (no key pull)", diff.calls)
	}

	// No active cert => reported plainly.
	none := &fakeActiveLookup{active: nil}
	if got, err := CertFreshnessFor(ctx, none, 12, "gone.cmdhome.net"); err != nil || got != CertFreshNoActiveCert {
		t.Errorf("no active = %q (%v), want no active cert", got, err)
	}
}
