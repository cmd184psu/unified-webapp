package haproxy

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func readHero(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "hero.cfg"))
	if err != nil {
		t.Fatalf("read hero fixture: %v", err)
	}
	return data
}

func TestImportHeroFixtureMapsToModel(t *testing.T) {
	m, _, err := Import(readHero(t))
	if err != nil {
		t.Fatalf("import: %v", err)
	}

	want := &Model{
		Version: 1,
		Global: []Directive{
			{Key: "log", Value: "127.0.0.1 local0 notice"},
			{Key: "maxconn", Value: "2000"},
			{Key: "user", Value: "haproxy"},
			{Key: "group", Value: "haproxy"},
		},
		Defaults: []Directive{
			{Key: "mode", Value: "http"},
			{Key: "log", Value: "global"},
			{Key: "timeout", Value: "connect 20000ms"},
			{Key: "timeout", Value: "server 1h"},
			{Key: "timeout", Value: "client 1h"},
			{Key: "timeout", Value: "tunnel 1h"},
		},
		DefaultService: DefaultService{
			Name:     "unified",
			Upstream: Upstream{Host: "127.0.0.1", Port: 8787},
			Check:    true,
		},
		Ports: []Port{{Port: 8443, DefaultService: "fakes3_ui"}},
		Services: []Service{
			{
				ID: "s1", Name: "brandx", Enabled: true,
				FQDNs: []string{"brandx.cmdhome.net"}, ExposedPort: 443,
				Upstream: Upstream{Host: "hero.cmdhome.net", Port: 8181}, Check: true,
				Cert: CertRef{FQDN: "brandx.cmdhome.net"}, Extra: []string{},
			},
			{
				ID: "s2", Name: "s3-hero", Enabled: true,
				FQDNs: []string{"s3-hero.cmdhome.net"}, ExposedPort: 443,
				Upstream: Upstream{Host: "127.0.0.1", Port: 6161}, Check: true,
				Cert: CertRef{FQDN: "s3-hero.cmdhome.net"}, Extra: []string{},
			},
			{
				ID: "s3", Name: "fakes3_ui", Enabled: true,
				FQDNs: []string{}, ExposedPort: 8443,
				Upstream: Upstream{Host: "127.0.0.1", Port: 6767}, Check: true,
				Cert: CertRef{FQDN: ""}, Extra: []string{},
			},
		},
		RawSections: []RawSection{{
			Header: "frontend stats",
			Lines: []string{
				"bind *:1936",
				"stats enable",
				"stats hide-version",
				`stats realm Haproxy\ Statistics`,
				"stats uri /",
				"stats auth user:password",
				"log 127.0.0.1 local0 notice",
			},
		}},
	}

	if !reflect.DeepEqual(m, want) {
		t.Fatalf("import mismatch:\n got %#v\nwant %#v", m, want)
	}
}

func TestImportReportListsUnmappedBits(t *testing.T) {
	_, rep, err := Import(readHero(t))
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if rep == nil {
		t.Fatal("nil report")
	}

	// The stats frontend is preserved raw and reported.
	if !containsStr(rep.RawKept, "frontend stats") {
		t.Errorf("RawKept = %v, want it to contain 'frontend stats'", rep.RawKept)
	}

	// The legacy per-bind crt arguments are not modelled (certs come via the
	// crt-list, D7) but are reported so nothing is lost silently.
	wantCrt := []string{
		"/etc/haproxy/certs/certmachine.cmdhome.net.pem",
		"/etc/haproxy/certs/brandx.cmdhome.net.pem",
		"/etc/haproxy/certs/s3-hero.cmdhome.net.pem",
	}
	for _, w := range wantCrt {
		if !containsStr(rep.LegacyCrtArgs, w) {
			t.Errorf("LegacyCrtArgs missing %q (got %v)", w, rep.LegacyCrtArgs)
		}
	}

	// Frontend options the generator drops are reported, not silent.
	if !containsStr(rep.Unmapped, "option http-server-close") {
		t.Errorf("Unmapped = %v, want it to contain 'option http-server-close'", rep.Unmapped)
	}
	// `option forwardfor` and `option httplog` are the generator's fixed
	// frontend policy (emitted on every TLS frontend), so they are not "lost".
	for _, kept := range []string{"option forwardfor", "option httplog"} {
		if containsStr(rep.Unmapped, kept) {
			t.Errorf("Unmapped = %v, must not report %q (the generator emits it)", rep.Unmapped, kept)
		}
	}
}

func TestImportRenderRoundTripStable(t *testing.T) {
	base, _, err := Import(readHero(t))
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	// Render with no driver baseline and no stats socket so the output carries
	// only model data (baseline/socket are driver-injected, not model state).
	rendered := Render(base, nil, "", "", "/etc/haproxy/crt-list.txt")
	again, _, err := Import([]byte(rendered))
	if err != nil {
		t.Fatalf("re-import rendered: %v\n--- rendered ---\n%s", err, rendered)
	}
	if !reflect.DeepEqual(base, again) {
		t.Fatalf("round trip not stable:\n--- rendered ---\n%s\n got %#v\nwant %#v", rendered, again, base)
	}
}

func containsStr(xs []string, want string) bool {
	for _, x := range xs {
		if x == want || strings.TrimSpace(x) == want {
			return true
		}
	}
	return false
}
