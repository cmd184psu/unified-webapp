package haproxy

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func strptr(s string) *string { return &s }

func TestCertDetailsFromSurfacesCertMachineData(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	cert := CertMachineCert{
		FQDN:      "brandx.cmdhome.net",
		SANs:      CertMachineSANs{DNS: []string{"brandx.cmdhome.net", "www.brandx.cmdhome.net"}, IP: []string{"10.0.0.1"}},
		NotBefore: strptr("2025-12-01T00:00:00Z"),
		NotAfter:  strptr("2026-02-01T00:00:00Z"),
		Status:    "active",
		CASubject: strptr("CN=cmdhome Root CA"),
	}
	d := CertDetailsFrom(cert, now, 30)
	if d.FQDN != "brandx.cmdhome.net" || d.Status != "active" {
		t.Errorf("details = %+v, want fqdn/status surfaced", d)
	}
	if len(d.SANsDNS) != 2 || len(d.SANsIP) != 1 {
		t.Errorf("SANs not surfaced: %+v", d)
	}
	if d.Issuer != "CN=cmdhome Root CA" {
		t.Errorf("issuer = %q, want the CertMachine-supplied subject", d.Issuer)
	}
	if d.NotAfter != "2026-02-01T00:00:00Z" {
		t.Errorf("validity window not surfaced: %q", d.NotAfter)
	}

	// Issuer is omitted when CertMachine supplies none (never guessed locally).
	noIssuer := CertDetailsFrom(CertMachineCert{FQDN: "x", NotAfter: strptr("2026-02-01T00:00:00Z")}, now, 30)
	if noIssuer.Issuer != "" {
		t.Errorf("issuer = %q, want empty when CertMachine supplies none", noIssuer.Issuer)
	}

	// Details never carry key material.
	b, _ := json.Marshal(d)
	if strings.Contains(string(b), "PRIVATE KEY") {
		t.Error("cert details JSON leaked key material")
	}
}

func TestCertExpiryStateFor(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	rfc := func(d time.Duration) string { return now.Add(d).Format(time.RFC3339) }
	cases := []struct {
		name     string
		notAfter string
		want     CertExpiryState
	}{
		{"far future", rfc(60 * 24 * time.Hour), CertExpiryOK},
		{"within warn window", rfc(10 * 24 * time.Hour), CertExpirySoon},
		{"exactly at the warn boundary", rfc(30 * 24 * time.Hour), CertExpirySoon},
		{"already past", rfc(-1 * time.Hour), CertExpiryExpired},
	}
	for _, tc := range cases {
		got, err := CertExpiryStateFor(tc.notAfter, now, 30)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if got != tc.want {
			t.Errorf("%s: state = %q, want %q", tc.name, got, tc.want)
		}
	}
	if _, err := CertExpiryStateFor("not-a-time", now, 30); err == nil {
		t.Error("an unparseable notAfter should error, not silently pass")
	}
}
