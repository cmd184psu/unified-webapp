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

	"cmd184psu/unified-webapp/internal/platform/auth"
	"cmd184psu/unified-webapp/internal/platform/config"
	"cmd184psu/unified-webapp/internal/platform/middleware"
)

// noAuthService builds a Service from an empty AuthConfig -- no modules
// protected, no admin routed -- so buildDispatcher's gate is a pass-through
// and these dispatcher-focused tests observe the same behavior they did
// before the gate was mounted.
func noAuthService(t *testing.T) *auth.Service {
	t.Helper()
	svc, err := auth.FromConfig(config.AuthConfig{}, knownModules, false)
	if err != nil {
		t.Fatalf("noAuthService: %v", err)
	}
	return svc
}

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
	srv := newGateServer(t, cfg, noAuthService(t))
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

	srv := newGateServer(t, cfg, noAuthService(t))
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
	// The 503 surface sits outside the auth gate, so the build error -- which
	// names filesystem paths -- must never reach the response body. The cause
	// is boot-log-only; the body names the module and nothing else.
	if strings.Contains(string(body), missing) {
		t.Errorf("503 body leaks the offending path %q to unauthenticated callers: %s", missing, body)
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

	srv := newGateServer(t, cfg, noAuthService(t))
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
	// Same contract as TestModuleBuildFailureIsScopedToThatModule: the 503
	// surface sits outside the auth gate, so the build error -- which names
	// filesystem paths -- must never reach the response body.
	if strings.Contains(string(body), missing) {
		t.Errorf("503 body leaks the offending path %q to unauthenticated callers: %s", missing, body)
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

// middleware.Wrap sets CORS headers on module responses: it reflects the
// request's Origin into Access-Control-Allow-Origin when it is same-origin,
// and sets Access-Control-Allow-Methods/-Headers unconditionally. certmachine
// serves private keys, so it strips all of those back off inside its own
// handler (stripCORS, build.go) -- defense in depth on top of Wrap's
// same-origin policy. This test is the reason that approach is verifiable: it
// goes through the very same middleware.Wrap(dispatcher) stack main.go builds
// (via newGateServer), so if Wrap ever moves its header Set to after
// next.ServeHTTP -- where an inner Del can no longer win -- this fails
// instead of silently regressing.
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

	srv := newGateServer(t, cfg, noAuthService(t))
	defer srv.Close()

	corsHeaders := []string{
		"Access-Control-Allow-Origin",
		"Access-Control-Allow-Methods",
		"Access-Control-Allow-Headers",
	}
	// Send a same-origin Origin header so Wrap would bless the request with
	// Access-Control-Allow-Origin -- the strongest case stripCORS must undo.
	doOrigin := func(host, path string) *http.Response {
		t.Helper()
		req, err := http.NewRequest(http.MethodGet, srv.URL+path, nil)
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		req.Host = host
		req.Header.Set("Origin", "http://"+host)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("do request: %v", err)
		}
		return res
	}

	// One route per response shape: JSON API, the SPA shell, and a 404 from
	// the API's catch-all -- the headers must be gone from all of them, not
	// just the happy path.
	for _, path := range []string{"/api/config", "/api/certs", "/", "/api/nope"} {
		res := doOrigin("certmachine.example", path)
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

	grocery := doOrigin("grocery.example", "/config")
	grocery.Body.Close()
	if got := grocery.Header.Get("Access-Control-Allow-Origin"); got != "http://grocery.example" {
		t.Errorf("stripping CORS for certmachine also broke grocery's same-origin ACAO; got %q, the fix must stay module-scoped", got)
	}
}

// An unrecognized module name in the config gets the same treatment as a build
// failure: its hostnames 503 and the binary still serves everything else.
func TestUnknownModuleBecomesA503(t *testing.T) {
	cfg := multisshTestConfig(t, map[string]string{"weird.example": "not-a-module"})
	srv := newGateServer(t, cfg, noAuthService(t))
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

// utuberTestConfig returns a config whose utuber module can actually build:
// a real static dir with an index.html and a download dir under t.TempDir().
//
// Workers: 0 is deliberate and load-bearing: buildDispatcher consumes the
// struct directly, so config.Load's normalization never runs here and
// StartWorkers(..., 0) starts no worker goroutines. yt-dlp IS on this host's
// PATH — a live worker would run real network downloads inside `make test`.
// These tests exercise the queue, not the worker.
func utuberTestConfig(t *testing.T, routing map[string]string) *config.Config {
	t.Helper()
	root := t.TempDir()
	staticDir := filepath.Join(root, "web")
	if err := os.MkdirAll(staticDir, 0o755); err != nil {
		t.Fatalf("mkdir static: %v", err)
	}
	if err := os.WriteFile(filepath.Join(staticDir, "index.html"), []byte("UTUBER INDEX"), 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}

	cfg := config.DefaultConfig()
	cfg.Routing = routing
	cfg.Utuber = config.UtuberConfig{
		StaticDir:   staticDir,
		DownloadDir: filepath.Join(root, "dl"),
		Workers:     0,
		PythonBin:   "python3.12",
	}
	return cfg
}

// The utuber module owns its hostname's whole path space, so its root-relative
// endpoints must come through the dispatcher unchanged: / serves the SPA and
// /jobs.json answers with the (empty) queue.
func TestUtuberRoutesAndServesIndex(t *testing.T) {
	cfg := utuberTestConfig(t, map[string]string{"utuber.example": "utuber"})
	srv := newGateServer(t, cfg, noAuthService(t))
	defer srv.Close()

	res := doHost(t, srv, http.MethodGet, "utuber.example", "/", "")
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET /: status %d, want 200", res.StatusCode)
	}
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if !strings.Contains(string(body), "UTUBER INDEX") {
		t.Errorf("GET / does not serve the planted index.html: %q", body)
	}

	jobsRes := doHost(t, srv, http.MethodGet, "utuber.example", "/jobs.json", "")
	defer jobsRes.Body.Close()
	if jobsRes.StatusCode != http.StatusOK {
		t.Fatalf("GET /jobs.json: status %d, want 200", jobsRes.StatusCode)
	}
	jobsBody, err := io.ReadAll(jobsRes.Body)
	if err != nil {
		t.Fatalf("read jobs body: %v", err)
	}
	// json.NewEncoder.Encode appends \n, hence the TrimSpace.
	if got := strings.TrimSpace(string(jobsBody)); got != "[]" {
		t.Errorf("GET /jobs.json = %q, want []", got)
	}
}

// FR-1: two hostnames routed to utuber share one module instance, and
// therefore one queue. A job enqueued through hostname A must be visible
// through hostname B. The URL is query-encoded because handleEnqueue uses
// r.FormValue, which reads the query string regardless of doHost's JSON
// Content-Type. No worker runs (Workers: 0), so the job stays queued.
func TestUtuberTwoHostnamesShareOneInstance(t *testing.T) {
	cfg := utuberTestConfig(t, map[string]string{
		"utuber-a.example": "utuber",
		"utuber-b.example": "utuber",
	})
	srv := newGateServer(t, cfg, noAuthService(t))
	defer srv.Close()

	res := doHost(t, srv, http.MethodPost, "utuber-a.example",
		"/enqueue?url=http%3A%2F%2Fexample.com%2Fv", "")
	defer res.Body.Close()
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("POST /enqueue via hostname A: status %d, want 204", res.StatusCode)
	}

	got := doHost(t, srv, http.MethodGet, "utuber-b.example", "/jobs.json", "")
	defer got.Body.Close()
	var decoded []struct {
		URL    string `json:"url"`
		Status string `json:"status"`
	}
	if err := json.NewDecoder(got.Body).Decode(&decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(decoded) != 1 || decoded[0].URL != "http://example.com/v" {
		t.Fatalf("hostname B does not see hostname A's job: %#v -- the module was built twice", decoded)
	}
	if decoded[0].Status != "queued" {
		t.Errorf("job status = %q, want queued (no worker should run with Workers: 0)", decoded[0].Status)
	}
}

// Deliberately overlaps TestModuleBuildFailureIsScopedToThatModule: this one
// covers utuber's own Build error path (history dir creation), keeping
// per-module coverage of the 503 scoping rule.
//
// DownloadDir is the regular file ITSELF, not a path under it: MkdirAll's
// first Stat then succeeds with !IsDir() and the returned *PathError names
// DownloadDir verbatim, which the leak assertion depends on.
func TestUtuberBuildFailureIsScopedToThatModule(t *testing.T) {
	cfg := utuberTestConfig(t, map[string]string{
		"utuber.example":  "utuber",
		"grocery.example": "grocery",
	})
	blocker := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatalf("write blocker file: %v", err)
	}
	cfg.Utuber.DownloadDir = blocker
	groceryDir := t.TempDir()
	cfg.Grocery.StaticDir = groceryDir
	cfg.Grocery.DataFile = filepath.Join(groceryDir, "grocery.json")

	srv := newGateServer(t, cfg, noAuthService(t))
	defer srv.Close()

	res := doHost(t, srv, http.MethodGet, "utuber.example", "/jobs.json", "")
	defer res.Body.Close()
	if res.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("failed module status = %d, want 503", res.StatusCode)
	}
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	// The 503 surface sits outside the auth gate, so the build error -- which
	// names filesystem paths -- must never reach the response body.
	if strings.Contains(string(body), blocker) {
		t.Errorf("503 body leaks the offending path %q to unauthenticated callers: %s", blocker, body)
	}
	if !strings.Contains(string(body), "utuber") {
		t.Errorf("503 body does not name the module: %s", body)
	}

	healthy := doHost(t, srv, http.MethodGet, "grocery.example", "/config", "")
	defer healthy.Body.Close()
	if healthy.StatusCode == http.StatusServiceUnavailable {
		t.Fatalf("a utuber misconfiguration took grocery offline (status %d)", healthy.StatusCode)
	}
}

// FR-S5/FR-I5: the same-origin check on the terminal WebSocket has to still
// work once the request has been through the dispatcher and the middleware --
// the dispatcher routes on r.Host and must not rewrite it, and nothing may
// wrap the ResponseWriter, or the upgrade loses http.Hijacker.
func TestWebSocketOriginCheckThroughDispatcher(t *testing.T) {
	const hostname = "ssh.example"
	cfg := multisshTestConfig(t, map[string]string{hostname: "multissh"})
	srv := newGateServer(t, cfg, noAuthService(t))
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

// smbeditTestConfig returns a config whose smbedit module can actually build:
// a real static dir with an index.html and a data dir under t.TempDir().
func smbeditTestConfig(t *testing.T, routing map[string]string) *config.Config {
	t.Helper()
	root := t.TempDir()
	staticDir := filepath.Join(root, "web")
	if err := os.MkdirAll(staticDir, 0o755); err != nil {
		t.Fatalf("mkdir static: %v", err)
	}
	if err := os.WriteFile(filepath.Join(staticDir, "index.html"), []byte("SMBEDIT INDEX"), 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}

	cfg := config.DefaultConfig()
	cfg.Routing = routing
	cfg.Smbedit = config.SmbeditConfig{
		StaticDir:  staticDir,
		DataDir:    filepath.Join(root, "data"),
		PickerRoot: root,
	}
	return cfg
}

// A smbedit routing entry builds and serves through the dispatcher, routed by
// Host header like every other module.
func TestSmbeditRoutesThroughDispatcher(t *testing.T) {
	cfg := smbeditTestConfig(t, map[string]string{"smb.example": "smbedit"})
	dispatch := buildDispatcher(cfg, noAuthService(t))
	t.Cleanup(dispatch.Close)
	srv := httptest.NewServer(middleware.Wrap(dispatch))
	defer srv.Close()

	res := doHost(t, srv, http.MethodGet, "smb.example", "/api/version", "")
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/version via dispatcher: status %d", res.StatusCode)
	}
	var v map[string]string
	if err := json.NewDecoder(res.Body).Decode(&v); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if v["version"] == "" {
		t.Errorf("version endpoint returned empty version: %#v", v)
	}

	index := doHost(t, srv, http.MethodGet, "smb.example", "/", "")
	defer index.Body.Close()
	body, err := io.ReadAll(index.Body)
	if err != nil {
		t.Fatalf("read index: %v", err)
	}
	if !strings.Contains(string(body), "SMBEDIT INDEX") {
		t.Errorf("dispatcher did not serve smbedit's index.html: %q", body)
	}
}

// A broken smbedit config 503s its own hostname and nothing else.
func TestSmbeditBuildFailureIsScopedToSmbedit(t *testing.T) {
	cfg := smbeditTestConfig(t, map[string]string{
		"smb.example":     "smbedit",
		"grocery.example": "grocery",
	})
	missing := filepath.Join(t.TempDir(), "no-such-frontend")
	cfg.Smbedit.StaticDir = missing
	groceryDir := t.TempDir()
	cfg.Grocery.StaticDir = groceryDir
	cfg.Grocery.DataFile = filepath.Join(groceryDir, "grocery.json")

	dispatch := buildDispatcher(cfg, noAuthService(t))
	t.Cleanup(dispatch.Close)
	srv := httptest.NewServer(middleware.Wrap(dispatch))
	defer srv.Close()

	res := doHost(t, srv, http.MethodGet, "smb.example", "/api/config", "")
	defer res.Body.Close()
	if res.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("failed smbedit status = %d, want 503", res.StatusCode)
	}
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if strings.Contains(string(body), missing) {
		t.Errorf("503 body leaks the offending path %q: %s", missing, body)
	}
	if !strings.Contains(string(body), "smbedit") {
		t.Errorf("503 body does not name the module: %s", body)
	}

	healthy := doHost(t, srv, http.MethodGet, "grocery.example", "/config", "")
	defer healthy.Body.Close()
	if healthy.StatusCode == http.StatusServiceUnavailable {
		t.Fatalf("a smbedit misconfiguration took grocery offline (status %d)", healthy.StatusCode)
	}
}

// Adding smbedit to the switch must not change the unknown-module path.
func TestUnknownModuleStill503sWithSmbeditWired(t *testing.T) {
	cfg := smbeditTestConfig(t, map[string]string{"weird.example": "still-not-a-module"})
	dispatch := buildDispatcher(cfg, noAuthService(t))
	t.Cleanup(dispatch.Close)
	srv := httptest.NewServer(middleware.Wrap(dispatch))
	defer srv.Close()

	res := doHost(t, srv, http.MethodGet, "weird.example", "/", "")
	defer res.Body.Close()
	if res.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", res.StatusCode)
	}
	body, _ := io.ReadAll(res.Body)
	if !strings.Contains(string(body), "still-not-a-module") {
		t.Errorf("503 body does not name the unknown module: %s", body)
	}
}
