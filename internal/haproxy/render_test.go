package haproxy

import (
	"fmt"
	"strings"
	"testing"
)

// stubValidateHAProxy is a structural stand-in for `haproxy -c`: it checks the
// config is well-formed (sections headed by a known keyword, directives
// indented under a section, every TLS frontend has a bind, every backend has a
// server). It does NOT run the real binary and does NOT require cert files.
func stubValidateHAProxy(cfg string) error {
	var section string // "" until the first section header
	var kind string    // global/defaults/frontend/backend/listen
	sawBind := map[string]bool{}
	sawServer := map[string]bool{}
	headers := []string{}

	isHeader := func(line string) (string, bool) {
		for _, kw := range []string{"global", "defaults", "frontend", "backend", "listen"} {
			if line == kw || strings.HasPrefix(line, kw+" ") {
				return kw, true
			}
		}
		return "", false
	}

	for n, raw := range strings.Split(cfg, "\n") {
		if strings.TrimSpace(raw) == "" || strings.HasPrefix(strings.TrimSpace(raw), "#") {
			continue
		}
		indented := raw[0] == ' ' || raw[0] == '\t'
		trimmed := strings.TrimSpace(raw)
		if !indented {
			kw, ok := isHeader(trimmed)
			if !ok {
				return fmt.Errorf("line %d: unknown section header %q", n+1, trimmed)
			}
			kind = kw
			section = trimmed
			headers = append(headers, section)
			continue
		}
		if section == "" {
			return fmt.Errorf("line %d: directive %q outside any section", n+1, trimmed)
		}
		switch {
		case kind == "frontend" && strings.HasPrefix(trimmed, "bind "):
			sawBind[section] = true
		case (kind == "backend" || kind == "listen") && strings.HasPrefix(trimmed, "server "):
			sawServer[section] = true
		}
	}

	for _, h := range headers {
		kw, _ := isHeader(h)
		if kw == "frontend" && !sawBind[h] {
			return fmt.Errorf("frontend %q has no bind", h)
		}
		if kw == "backend" && !sawServer[h] {
			return fmt.Errorf("backend %q has no server", h)
		}
	}
	return nil
}

func heroModel(t *testing.T) *Model {
	t.Helper()
	m, _, err := Import(readHero(t))
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	return m
}

func TestRenderStructureValidAndHasManagedHeader(t *testing.T) {
	m := heroModel(t)
	out := Render(m, []Directive{{Key: "chroot", Value: "/var/lib/haproxy"}}, "/run/haproxy/admin.sock", "", "/etc/haproxy/crt-list.txt")

	if !strings.Contains(out, "managed by the haproxy editor") {
		t.Error("missing managed header")
	}
	if !strings.Contains(out, "stats socket /run/haproxy/admin.sock mode 660 level user") {
		t.Error("missing stats socket directive")
	}
	if !strings.Contains(out, "crt-list /etc/haproxy/crt-list.txt") {
		t.Error("missing crt-list reference")
	}
	if !strings.Contains(out, "chroot /var/lib/haproxy") {
		t.Error("baseline directive not merged into global")
	}
	if err := stubValidateHAProxy(out); err != nil {
		t.Fatalf("rendered config not well-formed: %v\n%s", err, out)
	}
}

func TestRenderDeterministic(t *testing.T) {
	m := heroModel(t)
	a := Render(m, nil, "/run/haproxy/admin.sock", "", "/etc/haproxy/crt-list.txt")
	b := Render(m, nil, "/run/haproxy/admin.sock", "", "/etc/haproxy/crt-list.txt")
	if a != b {
		t.Fatal("render is not deterministic")
	}
}

func TestRenderDefaultServiceIs443DefaultBackend(t *testing.T) {
	m := heroModel(t)
	out := Render(m, nil, "", "", "/etc/haproxy/crt-list.txt")

	fe443 := frontendBlock(out, 443)
	if fe443 == "" {
		t.Fatal("no :443 frontend rendered")
	}
	if !strings.Contains(fe443, "default_backend "+m.DefaultService.Name+"_backend") {
		t.Errorf(":443 default_backend is not the default service:\n%s", fe443)
	}
}

func TestRenderTwoServicesShareOneFrontend(t *testing.T) {
	m := DefaultModel()
	m.Services = []Service{
		{ID: "s1", Name: "a", Enabled: true, FQDNs: []string{"a.example.net"}, ExposedPort: 443, Upstream: Upstream{Host: "127.0.0.1", Port: 1111}, Check: true},
		{ID: "s2", Name: "b", Enabled: true, FQDNs: []string{"b.example.net"}, ExposedPort: 443, Upstream: Upstream{Host: "127.0.0.1", Port: 2222}, Check: true},
	}
	out := Render(m, nil, "", "", "/etc/haproxy/crt-list.txt")

	if got := strings.Count(out, "\nfrontend fe_443\n"); got != 1 {
		t.Fatalf("expected exactly one :443 frontend, got %d\n%s", got, out)
	}
	fe := frontendBlock(out, 443)
	if !strings.Contains(fe, "use_backend a_backend") || !strings.Contains(fe, "use_backend b_backend") {
		t.Errorf("both services should route from the shared frontend:\n%s", fe)
	}
}

func TestRenderDisabledServiceAbsentButKeptInState(t *testing.T) {
	m := DefaultModel()
	m.Services = []Service{
		{ID: "s1", Name: "on", Enabled: true, FQDNs: []string{"on.example.net"}, ExposedPort: 443, Upstream: Upstream{Host: "127.0.0.1", Port: 1111}, Check: true},
		{ID: "s2", Name: "off", Enabled: false, FQDNs: []string{"off.example.net"}, ExposedPort: 443, Upstream: Upstream{Host: "127.0.0.1", Port: 2222}, Check: true},
	}
	out := Render(m, nil, "", "", "/etc/haproxy/crt-list.txt")

	if strings.Contains(out, "off.example.net") || strings.Contains(out, "off_backend") {
		t.Errorf("disabled service leaked into rendered config:\n%s", out)
	}
	if !strings.Contains(out, "on.example.net") {
		t.Error("enabled service missing from rendered config")
	}
	// Disabled service is still present in the model (state).
	if len(m.Services) != 2 {
		t.Fatalf("disabled service dropped from state: %d services", len(m.Services))
	}
}

// frontendBlock returns the text of the rendered `frontend fe_<port>` section.
func frontendBlock(cfg string, port int) string {
	header := fmt.Sprintf("frontend fe_%d", port)
	lines := strings.Split(cfg, "\n")
	var b strings.Builder
	in := false
	for _, l := range lines {
		if l == header {
			in = true
			b.WriteString(l + "\n")
			continue
		}
		if in {
			if l != "" && l[0] != ' ' && l[0] != '\t' {
				break
			}
			b.WriteString(l + "\n")
		}
	}
	return b.String()
}

// Every generated TLS frontend keeps hero's fixed policy: the real client
// address (X-Forwarded-For) and the HTTP log format.
func TestRenderFrontendsKeepForwardforAndHttplog(t *testing.T) {
	m, _, err := Import(readHero(t))
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	out := Render(m, nil, "/run/haproxy/admin.sock", "", "/etc/haproxy/crt-list.txt")
	fe := strings.Count(out, "\nfrontend fe_")
	if fe == 0 {
		t.Fatal("no generated frontends")
	}
	if got := strings.Count(out, "    option forwardfor\n"); got != fe {
		t.Errorf("option forwardfor appears %d times, want once per frontend (%d)", got, fe)
	}
	if got := strings.Count(out, "    option httplog\n"); got != fe {
		t.Errorf("option httplog appears %d times, want once per frontend (%d)", got, fe)
	}
}

func TestRenderStatsSocketWithOwner(t *testing.T) {
	m := heroModel(t)
	out := Render(m, nil, "/run/haproxy/admin.sock", "appuser", "/etc/haproxy/crt-list.txt")
	if !strings.Contains(out, "stats socket /run/haproxy/admin.sock mode 660 level user user appuser\n") {
		t.Errorf("owner not handed the socket:\n%s", out)
	}
	out = Render(m, nil, "/run/haproxy/admin.sock", "", "/etc/haproxy/crt-list.txt")
	if !strings.Contains(out, "stats socket /run/haproxy/admin.sock mode 660 level user\n") || strings.Contains(out, "level user user") {
		t.Errorf("empty owner must omit the user part:\n%s", out)
	}
}

// The app only ever reads the socket, so the generated global must never grant
// more than `level user`, and a baseline stats socket must not be emitted twice.
func TestRenderStatsSocketNeverAboveUserLevel(t *testing.T) {
	m := heroModel(t)
	base := []Directive{{Key: "stats socket", Value: "/run/haproxy/admin.sock mode 660 level admin"}}
	for _, owner := range []string{"", "appuser"} {
		out := Render(m, base, "/run/haproxy/admin.sock", owner, "/etc/haproxy/crt-list.txt")
		n := 0
		for _, l := range strings.Split(out, "\n") {
			if strings.Contains(l, "stats socket") {
				n++
				if !strings.Contains(l, "level user") || strings.Contains(l, "level operator") || strings.Contains(l, "level admin") {
					t.Errorf("socket line above user level: %q", l)
				}
			}
		}
		if n != 1 {
			t.Errorf("owner %q: %d stats socket lines, want 1:\n%s", owner, n, out)
		}
	}
}
