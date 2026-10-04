package haproxy

import (
	"strings"
	"testing"
)

func TestHintMissingCertsOffersExistingFiles(t *testing.T) {
	found := []ImportableCert{
		{Name: "brandx.cmdhome.net.pem", FQDN: "brandx.cmdhome.net"},
		{Name: "brandx.cmdhome.net-old.pem", FQDN: "brandx.cmdhome.net-old"},
		{Name: "admin.cmdhome.net.pem", FQDN: "admin.cmdhome.net"},
	}
	issues := []Issue{{Severity: issueError, Where: "service:1", Cert: "brandx.cmdhome.net",
		Message: `service "brandx" names certificate "brandx.cmdhome.net" but there is no tracked certificate for it`}}
	out := hintMissingCerts(issues, found)
	if got := out[0].Suggest; len(got) != 2 || got[0] != "brandx.cmdhome.net.pem" {
		t.Fatalf("suggest = %v", got)
	}
	if out[0].Message != "brandx needs a certificate for brandx.cmdhome.net." {
		t.Errorf("message = %q", out[0].Message)
	}
	none := hintMissingCerts([]Issue{{Severity: issueError, Cert: "zzz.example.com", Message: `service "z" names certificate`}}, found)
	if len(none[0].Suggest) != 0 || !strings.Contains(none[0].Message, "CertMachine") {
		t.Errorf("no-match message = %q", none[0].Message)
	}
}
