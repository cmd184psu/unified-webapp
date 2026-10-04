package haproxy

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// TestLiveCertMachineClient is the B5b check: it runs the editor's real
// CertMachine client against a REAL CertMachine instead of the fake. It is
// read-only and opt-in: it only runs when both variables are set, so `make test`
// skips it.
//
//	HAPROXY_LIVE_CERTMACHINE_URL=https://certmachine.cmdhome.net \
//	HAPROXY_LIVE_CERTMACHINE_KEY=<api key> \
//	  go test -race ./internal/haproxy -run TestLiveCertMachineClient -v
//
// It pulls one real haproxy.pem into memory to prove the integrity check
// (body hash == ETag, X-Cert-Id, X-Cert-Fingerprint) passes against the real
// server. Nothing is written to disk and the bundle is never logged.
func TestLiveCertMachineClient(t *testing.T) {
	url := os.Getenv("HAPROXY_LIVE_CERTMACHINE_URL")
	key := os.Getenv("HAPROXY_LIVE_CERTMACHINE_KEY")
	if url == "" || key == "" {
		t.Skip("set HAPROXY_LIVE_CERTMACHINE_URL and HAPROXY_LIVE_CERTMACHINE_KEY to run against a real CertMachine")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	c, err := CertMachineNewClient(CertMachineSettings{URL: url, APIKey: CertMachineAPIKey(key), CAFile: os.Getenv("HAPROXY_LIVE_CERTMACHINE_CAFILE")})
	if err != nil {
		t.Fatalf("client: %v", err)
	}

	active, err := c.ListCerts(ctx, "", "active")
	if err != nil {
		t.Fatalf("ListCerts(active): %v", err)
	}
	if len(active) == 0 {
		t.Fatal("the real CertMachine has no active certs to test against")
	}
	var pick *CertMachineCert
	for i := range active {
		if !strings.HasPrefix(active[i].FQDN, "*") && active[i].Fingerprint != nil {
			pick = &active[i]
			break
		}
	}
	if pick == nil {
		t.Fatal("no active non-wildcard cert with a fingerprint")
	}
	t.Logf("testing on cert id %d (%s); %d active certs total", pick.ID, pick.FQDN, len(active))

	// FR-C1 through the real filter: the active cert for the FQDN is that cert.
	got, err := c.ActiveCertForFQDN(ctx, pick.FQDN)
	if err != nil || got == nil || got.ID != pick.ID {
		t.Fatalf("ActiveCertForFQDN(%s) = %+v, %v; want id %d", pick.FQDN, got, err, pick.ID)
	}
	if one, err := c.GetCert(ctx, pick.ID); err != nil || one.ID != pick.ID || one.FQDN != pick.FQDN {
		t.Errorf("GetCert(%d) = %+v, %v", pick.ID, one, err)
	}

	// FR-H45 against the real server: the verified pull succeeds and the bytes are a PEM bundle.
	bundle, err := c.PullHAProxyPEM(ctx, pick.ID, *pick.Fingerprint)
	if err != nil {
		t.Fatalf("verified pull failed against the real CertMachine: %v", err)
	}
	if !strings.Contains(string(bundle), "BEGIN CERTIFICATE") || !strings.Contains(string(bundle), "PRIVATE KEY") {
		t.Errorf("pulled %d bytes that do not look like a haproxy.pem bundle", len(bundle))
	}
	name := NamingFileName(pick.FQDN, bundle)
	if err := NamingValidateName(name); err != nil {
		t.Errorf("convention file name %q rejected: %v", name, err)
	}
	t.Logf("verified pull ok: %d bytes, convention name %s", len(bundle), name)

	// A wrong expected fingerprint must be refused with no bytes returned.
	if bad, err := c.PullHAProxyPEM(ctx, pick.ID, "00:00:00"); err == nil || len(bad) != 0 {
		t.Errorf("a wrong fingerprint must be rejected with no bytes; got %d bytes, err=%v", len(bad), err)
	}

	// FR-H46 freshness against the real server.
	if f, err := CertFreshnessFor(ctx, c, pick.ID, pick.FQDN); err != nil || f != CertFreshUpToDate {
		t.Errorf("freshness for the current id = %q, %v; want up to date", f, err)
	}
	if f, err := CertFreshnessFor(ctx, c, pick.ID+100000, pick.FQDN); err != nil || f != CertFreshUpdateAvailable {
		t.Errorf("freshness for an older id = %q, %v; want update available", f, err)
	}
	if f, err := CertFreshnessFor(ctx, c, pick.ID, "no-such-name.invalid"); err != nil || f != CertFreshNoActiveCert {
		t.Errorf("freshness for an unknown FQDN = %q, %v; want no active cert", f, err)
	}
}
