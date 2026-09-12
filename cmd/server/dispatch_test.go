package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cmd184psu/unified-webapp/internal/platform/config"
	"cmd184psu/unified-webapp/internal/platform/middleware"
)

// multisshTestConfig returns a config whose multissh module can actually build:
// a real static dir, a real ssh dir, and paths under t.TempDir().
func multisshTestConfig(t *testing.T, routing map[string]string) *config.Config {
	t.Helper()
	root := t.TempDir()
	staticDir := filepath.Join(root, "web")
	if err := os.MkdirAll(staticDir, 0o755); err != nil {
		t.Fatalf("mkdir static: %v", err)
	}
	if err := os.WriteFile(filepath.Join(staticDir, "index.html"), []byte("INDEX"), 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}
	sshDir := filepath.Join(root, "ssh")
	if err := os.MkdirAll(sshDir, 0o755); err != nil {
		t.Fatalf("mkdir ssh: %v", err)
	}

	cfg := config.DefaultConfig()
	cfg.Routing = routing
	cfg.Multissh = config.MultisshConfig{
		StaticDir:      staticDir,
		SSHDir:         sshDir,
		UploadDir:      filepath.Join(root, "uploads"),
		HostsPath:      filepath.Join(root, "data", "hosts.json"),
		BrowseRoot:     filepath.Join(root, "uploads"),
		MaxSessions:    3,
		MaxUploadBytes: 1 << 20,
	}
	return cfg
}

func doHost(t *testing.T, srv *httptest.Server, method, host, path, body string) *http.Response {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, srv.URL+path, reader)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Host = host
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	return res
}

// Two hostnames pointing at one module must share one handler, and therefore
// one set of on-disk and in-memory state. Building twice would give each
// hostname its own host store, so a preset saved through one would be missing
// through the other until a restart.
func TestTwoHostnamesShareOneModuleInstance(t *testing.T) {
	cfg := multisshTestConfig(t, map[string]string{
		"ssh-a.example": "multissh",
		"ssh-b.example": "multissh",
	})
	srv := httptest.NewServer(middleware.Wrap(buildDispatcher(cfg)))
	defer srv.Close()

	res := doHost(t, srv, http.MethodPut, "ssh-a.example", "/api/hosts",
		`{"hosts":[{"ip":"10.0.0.7","port":22,"user":"u","key":"","remoteDir":"/tmp"}]}`)
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("PUT via hostname A: status %d", res.StatusCode)
	}

	got := doHost(t, srv, http.MethodGet, "ssh-b.example", "/api/hosts", "")
	defer got.Body.Close()
	var decoded struct {
		Hosts []struct {
			IP string `json:"ip"`
		} `json:"hosts"`
	}
	if err := json.NewDecoder(got.Body).Decode(&decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(decoded.Hosts) != 1 || decoded.Hosts[0].IP != "10.0.0.7" {
		t.Fatalf("hostname B does not see hostname A's write: %#v -- the module was built twice", decoded.Hosts)
	}
}

// A module that cannot build takes down its own hostnames and nothing else.
// A known_hosts typo has no business stopping the shopping list from loading.
func TestModuleBuildFailureIsScopedToThatModule(t *testing.T) {
	cfg := multisshTestConfig(t, map[string]string{
		"ssh.example":     "multissh",
		"grocery.example": "grocery",
	})
	missing := filepath.Join(t.TempDir(), "no-such-frontend")
	cfg.Multissh.StaticDir = missing
	groceryDir := t.TempDir()
	cfg.Grocery.StaticDir = groceryDir
	cfg.Grocery.DataFile = filepath.Join(groceryDir, "grocery.json")

	srv := httptest.NewServer(middleware.Wrap(buildDispatcher(cfg)))
	defer srv.Close()

	res := doHost(t, srv, http.MethodGet, "ssh.example", "/api/config", "")
	defer res.Body.Close()
	if res.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("failed module status = %d, want 503", res.StatusCode)
	}
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if !strings.Contains(string(body), missing) {
		t.Errorf("503 body does not name the offending path %q: %s", missing, body)
	}
	if !strings.Contains(string(body), "multissh") {
		t.Errorf("503 body does not name the module: %s", body)
	}

	healthy := doHost(t, srv, http.MethodGet, "grocery.example", "/config", "")
	defer healthy.Body.Close()
	if healthy.StatusCode == http.StatusServiceUnavailable {
		t.Fatalf("a multissh misconfiguration took grocery offline (status %d)", healthy.StatusCode)
	}
}

// certmachine is the seventh module: the same build-failure isolation
// TestModuleBuildFailureIsScopedToThatModule proves for multissh must also
// hold for it, since it is wired into buildModule exactly like every other
// module (cmd/server/main.go).
func TestCertmachineBuildFailureIsScopedToThatModule(t *testing.T) {
	cfg := multisshTestConfig(t, map[string]string{
		"certmachine.example": "certmachine",
		"grocery.example":     "grocery",
	})
	missing := filepath.Join(t.TempDir(), "no-such-frontend")
	cfg.Certmachine.StaticDir = missing
	cfg.Certmachine.DBPath = filepath.Join(t.TempDir(), "certmachine.db")
	groceryDir := t.TempDir()
	cfg.Grocery.StaticDir = groceryDir
	cfg.Grocery.DataFile = filepath.Join(groceryDir, "grocery.json")

	srv := httptest.NewServer(middleware.Wrap(buildDispatcher(cfg)))
	defer srv.Close()

	res := doHost(t, srv, http.MethodGet, "certmachine.example", "/api/config", "")
	defer res.Body.Close()
	if res.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("failed module status = %d, want 503", res.StatusCode)
	}
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if !strings.Contains(string(body), missing) {
		t.Errorf("503 body does not name the offending path %q: %s", missing, body)
	}
	if !strings.Contains(string(body), "certmachine") {
		t.Errorf("503 body does not name the module: %s", body)
	}

	healthy := doHost(t, srv, http.MethodGet, "grocery.example", "/config", "")
	defer healthy.Body.Close()
	if healthy.StatusCode == http.StatusServiceUnavailable {
		t.Fatalf("a certmachine misconfiguration took grocery offline (status %d)", healthy.StatusCode)
	}
}

// middleware.Wrap sets Access-Control-Allow-Origin: * on every module's
// response, which for certmachine means any page on the internet could read a
// leaf's private key out of a logged-in operator's browser. certmachine strips
// those headers back off inside its own handler (stripCORS, build.go), and this
// test is the reason that approach is verifiable: it goes through the very same
// middleware.Wrap(buildDispatcher(cfg)) stack main.go builds, so if Wrap ever
// moves its header Set to after next.ServeHTTP -- where an inner Del can no
// longer win -- this fails instead of silently regressing.
//
// The grocery assertion at the end is the other half of the contract: the fix
// is certmachine-scoped, and the platform middleware other modules may rely on
// is untouched.
func TestCertmachineResponsesCarryNoCORSHeaders(t *testing.T) {
	cfg := multisshTestConfig(t, map[string]string{
		"certmachine.example": "certmachine",
		"grocery.example":     "grocery",
	})
	certDir := t.TempDir()
	staticDir := filepath.Join(certDir, "web")
	if err := os.MkdirAll(staticDir, 0o755); err != nil {
		t.Fatalf("mkdir certmachine static: %v", err)
	}
	if err := os.WriteFile(filepath.Join(staticDir, "index.html"), []byte("INDEX"), 0o644); err != nil {
		t.Fatalf("write certmachine index: %v", err)
	}
	cfg.Certmachine.StaticDir = staticDir
	cfg.Certmachine.DBPath = filepath.Join(certDir, "data", "certmachine.db")
	groceryDir := t.TempDir()
	cfg.Grocery.StaticDir = groceryDir
	cfg.Grocery.DataFile = filepath.Join(groceryDir, "grocery.json")

	srv := httptest.NewServer(middleware.Wrap(buildDispatcher(cfg)))
	defer srv.Close()

	corsHeaders := []string{
		"Access-Control-Allow-Origin",
		"Access-Control-Allow-Methods",
		"Access-Control-Allow-Headers",
	}
	// One route per response shape: JSON API, the SPA shell, and a 404 from
	// the API's catch-all -- the headers must be gone from all of them, not
	// just the happy path.
	for _, path := range []string{"/api/config", "/api/certs", "/", "/api/nope"} {
		res := doHost(t, srv, http.MethodGet, "certmachine.example", path, "")
		res.Body.Close()
		if res.StatusCode == http.StatusServiceUnavailable {
			t.Fatalf("certmachine failed to build; GET %s = 503", path)
		}
		for _, h := range corsHeaders {
			if got := res.Header.Get(h); got != "" {
				t.Errorf("GET %s carries %s: %q", path, h, got)
			}
		}
	}

	grocery := doHost(t, srv, http.MethodGet, "grocery.example", "/config", "")
	grocery.Body.Close()
	if got := grocery.Header.Get("Access-Control-Allow-Origin"); got == "" {
		t.Error("stripping CORS for certmachine also stripped it from grocery; the fix must stay module-scoped")
	}
}

// An unrecognized module name in the config gets the same treatment as a build
// failure: its hostnames 503 and the binary still serves everything else.
func TestUnknownModuleBecomesA503(t *testing.T) {
	cfg := multisshTestConfig(t, map[string]string{"weird.example": "not-a-module"})
	srv := httptest.NewServer(middleware.Wrap(buildDispatcher(cfg)))
	defer srv.Close()

	res := doHost(t, srv, http.MethodGet, "weird.example", "/", "")
	defer res.Body.Close()
	if res.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", res.StatusCode)
	}
	body, _ := io.ReadAll(res.Body)
	if !strings.Contains(string(body), "not-a-module") {
		t.Errorf("503 body does not name the unknown module: %s", body)
	}
}

// FR-S5/FR-I5: the same-origin check on the terminal WebSocket has to still
// work once the request has been through the dispatcher and the middleware --
// the dispatcher routes on r.Host and must not rewrite it, and nothing may
// wrap the ResponseWriter, or the upgrade loses http.Hijacker.
func TestWebSocketOriginCheckThroughDispatcher(t *testing.T) {
	const hostname = "ssh.example"
	cfg := multisshTestConfig(t, map[string]string{hostname: "multissh"})
	srv := httptest.NewServer(middleware.Wrap(buildDispatcher(cfg)))
	defer srv.Close()

	cases := []struct {
		name       string
		origin     string
		wantStatus int
	}{
		{"matching origin upgrades", "http://" + hostname, http.StatusSwitchingProtocols},
		{"absent origin upgrades", "", http.StatusSwitchingProtocols},
		{"foreign origin is rejected", "http://evil.example", http.StatusForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodGet, srv.URL+"/api/ssh/ws", nil)
			if err != nil {
				t.Fatalf("new request: %v", err)
			}
			req.Host = hostname
			req.Header.Set("Connection", "Upgrade")
			req.Header.Set("Upgrade", "websocket")
			req.Header.Set("Sec-WebSocket-Version", "13")
			req.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			res, err := http.DefaultTransport.RoundTrip(req)
			if err != nil {
				t.Fatalf("round trip: %v", err)
			}
			defer res.Body.Close()
			if res.StatusCode != tc.wantStatus {
				body, _ := io.ReadAll(res.Body)
				t.Fatalf("status = %d, want %d (body %q)", res.StatusCode, tc.wantStatus, body)
			}
		})
	}
}
