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
	srv := httptest.NewServer(middleware.Wrap(buildDispatcher(cfg)))
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
	srv := httptest.NewServer(middleware.Wrap(buildDispatcher(cfg)))
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
// DownloadDir verbatim, which the body assertion depends on.
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

	srv := httptest.NewServer(middleware.Wrap(buildDispatcher(cfg)))
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
	if !strings.Contains(string(body), blocker) {
		t.Errorf("503 body does not name the offending path %q: %s", blocker, body)
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
