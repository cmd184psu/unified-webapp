package haproxy

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
)

// goodConfig returns a HaproxyConfig that Build accepts: a real static_dir
// with an index.html, a data_dir under t.TempDir(), and an explicit,
// supported os so detection never depends on the test host.
func goodConfig(t *testing.T) config.HaproxyConfig {
	t.Helper()
	root := t.TempDir()
	staticDir := filepath.Join(root, "web")
	if err := os.MkdirAll(staticDir, 0o755); err != nil {
		t.Fatalf("mkdir static: %v", err)
	}
	if err := os.WriteFile(filepath.Join(staticDir, "index.html"), []byte("HAPROXY INDEX"), 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}
	return config.HaproxyConfig{
		StaticDir: staticDir,
		DataDir:   filepath.Join(root, "data"),
		OS:        "rocky",
	}
}

func TestBuildServesWithValidConfig(t *testing.T) {
	cfg := goodConfig(t)
	h, err := Build(cfg)
	if err != nil {
		t.Fatalf("Build with a valid config: %v", err)
	}
	if h == nil {
		t.Fatal("Build returned a nil handler with no error")
	}

	// The static frontend is served.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /: status %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "HAPROXY INDEX") {
		t.Errorf("GET / did not serve the planted index.html: %q", rec.Body.String())
	}

	// The JSON status stub reports the module name and the detected OS.
	srec := httptest.NewRecorder()
	h.ServeHTTP(srec, httptest.NewRequest(http.MethodGet, "/api/status", nil))
	if srec.Code != http.StatusOK {
		t.Fatalf("GET /api/status: status %d, want 200", srec.Code)
	}
	var st map[string]any
	if err := json.Unmarshal(srec.Body.Bytes(), &st); err != nil {
		t.Fatalf("decode status: %v (body %q)", err, srec.Body.String())
	}
	if st["module"] != "haproxy" {
		t.Errorf("status module = %v, want haproxy", st["module"])
	}
	// No OS-specific value reaches the API (FR-H31).
	if _, ok := st["os"]; ok {
		t.Errorf("status exposes an os field: %v", st)
	}
}

func TestBuildFailsOnMissingStaticDir(t *testing.T) {
	cfg := goodConfig(t)
	cfg.StaticDir = filepath.Join(t.TempDir(), "no-such-frontend")
	if _, err := Build(cfg); err == nil {
		t.Fatal("Build accepted a missing static_dir; want a loud failure")
	}

	cfg = goodConfig(t)
	cfg.StaticDir = ""
	if _, err := Build(cfg); err == nil {
		t.Fatal("Build accepted an empty static_dir; want a loud failure")
	}
}

func TestBuildFailsOnUnusableDataDir(t *testing.T) {
	cfg := goodConfig(t)
	// A regular file where the data dir should be: MkdirAll cannot turn it
	// into a directory, so Build must fail loudly rather than degrade.
	blocker := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatalf("write blocker: %v", err)
	}
	cfg.DataDir = filepath.Join(blocker, "data")
	if _, err := Build(cfg); err == nil {
		t.Fatal("Build accepted an unusable data_dir; want a loud failure")
	}

	cfg = goodConfig(t)
	cfg.DataDir = ""
	if _, err := Build(cfg); err == nil {
		t.Fatal("Build accepted an empty data_dir; want a loud failure")
	}
}

// An unsupported OS does not fail the build: the module builds and every
// request answers 503 with the reason, so the rest of the binary keeps
// serving (FR-H30, the smbedit failed-module pattern, but scoped inside the
// module rather than at the dispatcher).
func TestUnsupportedOSYieldsScoped503WithReason(t *testing.T) {
	cfg := goodConfig(t)
	cfg.OS = "windows"
	h, err := Build(cfg)
	if err != nil {
		t.Fatalf("Build with an unsupported OS returned an error (want a built handler that 503s): %v", err)
	}
	if h == nil {
		t.Fatal("Build returned a nil handler for an unsupported OS")
	}

	for _, path := range []string{"/api/model", "/api/status", "/api/anything"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("GET %s on unsupported OS: status %d, want 503", path, rec.Code)
		}
		body := rec.Body.String()
		if !strings.Contains(body, "windows") {
			t.Errorf("GET %s: 503 body does not name the offending OS: %q", path, body)
		}
	}
}

func TestAPIKeyRedactedInStatusView(t *testing.T) {
	const secret = "supersecret-haproxy-key-9f3a"
	cfg := goodConfig(t)
	cfg.CertMachine = config.HaproxyCertMachineConfig{
		URL:    "https://certmachine.example",
		APIKey: secret,
	}
	h, err := Build(cfg)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/status", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/status: status %d, want 200", rec.Code)
	}
	body, _ := io.ReadAll(rec.Body)
	if strings.Contains(string(body), secret) {
		t.Fatalf("status response leaks the raw api_key: %s", body)
	}

	if strings.Contains(string(body), "certmachine.example") && strings.Contains(string(body), "api_key") {
		t.Errorf("status echoes the certmachine settings: %s", body)
	}
}

// Every path the operator can set must reach the driver; backup_dir used to be
// missing, so a second instance silently wrote its backups into the default
// directory of the real one.
func TestDriverOptionsCarryEveryConfiguredPath(t *testing.T) {
	cfg := config.HaproxyConfig{
		ConfigPath: "/x/haproxy.cfg", CertsDir: "/x/certs", CrtListPath: "/x/crt-list.txt",
		StatsSocketPath: "/run/x/admin.sock", ServiceName: "haproxy-x", BackupDir: "/x/backups",
	}
	got := driverOptions(baseSettings(cfg))
	want := DriverOptions{
		ConfigPath: "/x/haproxy.cfg", CertsDir: "/x/certs", CrtListPath: "/x/crt-list.txt",
		StatsSocketPath: "/run/x/admin.sock", ServiceName: "haproxy-x", BackupDir: "/x/backups",
	}
	if got != want {
		t.Errorf("driverOptions = %+v, want %+v", got, want)
	}
	if empty := driverOptions(baseSettings(config.HaproxyConfig{})); empty != (DriverOptions{}) {
		t.Errorf("an unset config must leave every option empty so the driver defaults apply: %+v", empty)
	}
}
