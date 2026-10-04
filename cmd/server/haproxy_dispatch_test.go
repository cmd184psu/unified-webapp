package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"cmd184psu/unified-webapp/internal/haproxy"
	"cmd184psu/unified-webapp/internal/platform/config"
	"cmd184psu/unified-webapp/internal/platform/middleware"
)

// haproxyTestConfig returns a config whose haproxy module can actually build:
// a real static dir with an index.html and a data dir under t.TempDir(). The
// os is explicit so detection never depends on the test host.
func haproxyTestConfig(t *testing.T, routing map[string]string, osName string) *config.Config {
	t.Helper()
	root := t.TempDir()
	staticDir := filepath.Join(root, "web")
	if err := os.MkdirAll(staticDir, 0o755); err != nil {
		t.Fatalf("mkdir static: %v", err)
	}
	if err := os.WriteFile(filepath.Join(staticDir, "index.html"), []byte("HAPROXY INDEX"), 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}
	cfg := config.DefaultConfig()
	cfg.Routing = routing
	cfg.Haproxy = config.HaproxyConfig{
		StaticDir: staticDir,
		DataDir:   filepath.Join(root, "data"),
		OS:        osName,
	}
	return cfg
}

// recordingExec is a scripted haproxy.Exec: it records every command and runs
// nothing, so dispatcher tests never reach real sudo/systemctl/haproxy.
type recordingExec struct {
	mu    sync.Mutex
	calls []string
}

func (r *recordingExec) Run(_ context.Context, _ []byte, name string, args ...string) ([]byte, []byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, strings.Join(append([]string{name}, args...), " "))
	return nil, nil, errors.New("recordingExec: command not run")
}

func (r *recordingExec) recorded() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.calls...)
}

// stubHaproxyExec replaces the haproxy module's default command layer for one
// test and restores it afterwards.
func stubHaproxyExec(t *testing.T) *recordingExec {
	t.Helper()
	rec := &recordingExec{}
	prev := haproxy.NewDefaultExec
	haproxy.NewDefaultExec = func() haproxy.Exec { return rec }
	t.Cleanup(func() { haproxy.NewDefaultExec = prev })
	return rec
}

func TestHaproxyInKnownModules(t *testing.T) {
	found := false
	for _, m := range knownModules {
		if m == "haproxy" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("knownModules does not contain %q: %v", "haproxy", knownModules)
	}
}

// Registration is useless unless the build pipeline bundles the module too:
// the Makefile's bundle-shape gate must --require haproxy, and
// scripts/descriptors.mjs must carry a sharedConsumer descriptor for it.
func TestHaproxyWiredIntoWebBuild(t *testing.T) {
	root := repoRoot(t)

	mk, err := os.ReadFile(filepath.Join(root, "Makefile"))
	if err != nil {
		t.Fatalf("read Makefile: %v", err)
	}
	requireLine := ""
	for _, line := range strings.Split(string(mk), "\n") {
		if strings.Contains(line, "bundle-shape.mjs") && strings.Contains(line, "--require=") {
			requireLine = line
			break
		}
	}
	if requireLine == "" {
		t.Fatal("Makefile has no bundle-shape --require line")
	}
	if !strings.Contains(requireLine, "haproxy") {
		t.Errorf("bundle-shape --require list does not include haproxy: %s", requireLine)
	}

	desc, err := os.ReadFile(filepath.Join(root, "scripts", "descriptors.mjs"))
	if err != nil {
		t.Fatalf("read descriptors.mjs: %v", err)
	}
	if !strings.Contains(string(desc), `name: "haproxy"`) {
		t.Error("scripts/descriptors.mjs has no descriptor named haproxy")
	}
}

// repoRoot walks up from the test's working directory until it finds go.mod.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find go.mod above the test working directory")
		}
		dir = parent
	}
}

// A supported-OS haproxy routing entry builds and serves through the
// dispatcher, routed by Host header like every other module.
func TestHaproxyRoutesThroughDispatcher(t *testing.T) {
	stubHaproxyExec(t)
	cfg := haproxyTestConfig(t, map[string]string{"hap.example": "haproxy"}, "rocky")
	dispatch := buildDispatcher(cfg, noAuthService(t))
	t.Cleanup(dispatch.Close)
	srv := httptest.NewServer(middleware.Wrap(dispatch))
	defer srv.Close()

	res := doHost(t, srv, http.MethodGet, "hap.example", "/api/status", "")
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/status via dispatcher: status %d", res.StatusCode)
	}
	var st map[string]any
	if err := json.NewDecoder(res.Body).Decode(&st); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if st["module"] != "haproxy" {
		t.Errorf("status module = %v, want haproxy", st["module"])
	}
}

// An unsupported OS 503s the haproxy hostname with the reason while another
// module in the same dispatcher keeps serving (FR-H30, scoped).
func TestHaproxyUnsupportedOSScopedWhileOtherServes(t *testing.T) {
	stubHaproxyExec(t)
	cfg := haproxyTestConfig(t, map[string]string{
		"hap.example":     "haproxy",
		"grocery.example": "grocery",
	}, "windows")
	groceryDir := t.TempDir()
	cfg.Grocery.StaticDir = groceryDir
	cfg.Grocery.DataFile = filepath.Join(groceryDir, "grocery.json")

	dispatch := buildDispatcher(cfg, noAuthService(t))
	t.Cleanup(dispatch.Close)
	srv := httptest.NewServer(middleware.Wrap(dispatch))
	defer srv.Close()

	res := doHost(t, srv, http.MethodGet, "hap.example", "/api/status", "")
	defer res.Body.Close()
	if res.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("unsupported-OS haproxy status = %d, want 503", res.StatusCode)
	}
	body, _ := io.ReadAll(res.Body)
	if !strings.Contains(string(body), "windows") {
		t.Errorf("503 body does not name the offending OS: %s", body)
	}

	healthy := doHost(t, srv, http.MethodGet, "grocery.example", "/config", "")
	defer healthy.Body.Close()
	if healthy.StatusCode == http.StatusServiceUnavailable {
		t.Fatalf("an unsupported-OS haproxy took grocery offline (status %d)", healthy.StatusCode)
	}
}

// FR-H30: an explicit os "macos" on an Intel Mac is still a scoped 503 naming
// Apple silicon, while another module keeps serving.
func TestHaproxyIntelMacExplicitIsScoped503(t *testing.T) {
	stubHaproxyExec(t)
	t.Cleanup(haproxy.OverrideHostPlatform("darwin", "amd64"))
	cfg := haproxyTestConfig(t, map[string]string{"hap.example": "haproxy"}, "macos")
	dispatch := buildDispatcher(cfg, noAuthService(t))
	t.Cleanup(dispatch.Close)
	srv := httptest.NewServer(middleware.Wrap(dispatch))
	defer srv.Close()
	res := doHost(t, srv, http.MethodGet, "hap.example", "/api/status", "")
	defer res.Body.Close()
	if res.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", res.StatusCode)
	}
	body, _ := io.ReadAll(res.Body)
	if !strings.Contains(string(body), "Apple silicon only") {
		t.Errorf("503 body missing the Apple-silicon-only reason: %s", body)
	}
}

// Routing /api/status through the dispatcher must reach the command layer only
// through the injected seam, never the real exec.
func TestHaproxyDispatcherUsesInjectedExecOnly(t *testing.T) {
	rec := stubHaproxyExec(t)
	cfg := haproxyTestConfig(t, map[string]string{"hap.example": "haproxy"}, "rocky")
	dispatch := buildDispatcher(cfg, noAuthService(t))
	t.Cleanup(dispatch.Close)
	srv := httptest.NewServer(middleware.Wrap(dispatch))
	defer srv.Close()

	res := doHost(t, srv, http.MethodGet, "hap.example", "/api/status", "")
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/status: status %d", res.StatusCode)
	}
	if len(rec.recorded()) == 0 {
		t.Fatal("status never used the injected Exec: the real exec layer was used")
	}
}
