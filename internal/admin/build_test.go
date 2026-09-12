package admin

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"cmd184psu/unified-webapp/internal/platform/auth"
	"cmd184psu/unified-webapp/internal/platform/config"
)

// staticDirForBuild returns a temp dir containing an index.html, mirroring
// the other modules' build_test.go fixtures.
func staticDirForBuild(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("ADMIN SHELL"), 0o644); err != nil {
		t.Fatalf("write index.html: %v", err)
	}
	return dir
}

func buildDeps(t *testing.T) Deps {
	t.Helper()
	svc, err := auth.FromConfig(config.AuthConfig{}, []string{"admin"}, false)
	if err != nil {
		t.Fatalf("auth.FromConfig: %v", err)
	}
	return Deps{
		Service:      svc,
		ConfigPath:   filepath.Join(t.TempDir(), "config.json"),
		KnownModules: []string{"grocery", "todo", "admin"},
		AdminRouted:  true,
	}
}

func TestBuildServesIndexAtRoot(t *testing.T) {
	cfg := &config.Config{Admin: config.AdminConfig{StaticDir: staticDirForBuild(t)}}

	h, err := Build(cfg, buildDeps(t))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if h == nil {
		t.Fatal("Build returned a nil handler")
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET / = %d, want 200", rec.Code)
	}
	if got := rec.Body.String(); got != "ADMIN SHELL" {
		t.Fatalf("GET / body = %q, want %q", got, "ADMIN SHELL")
	}
}

func TestBuildFallsBackToIndexForUnknownPath(t *testing.T) {
	cfg := &config.Config{Admin: config.AdminConfig{StaticDir: staticDirForBuild(t)}}

	h, err := Build(cfg, buildDeps(t))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/some/spa/route", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /some/spa/route = %d, want 200 (SPA fallback)", rec.Code)
	}
	if got := rec.Body.String(); got != "ADMIN SHELL" {
		t.Fatalf("GET /some/spa/route body = %q, want index.html contents %q", got, "ADMIN SHELL")
	}
}

// T5.3's live-apply needs the exact Deps handed to Build to still be
// reachable off the constructed Handler -- this locks in that NewHandler
// retains them rather than dropping any field.
func TestNewHandlerRetainsDeps(t *testing.T) {
	deps := buildDeps(t)
	h := NewHandler(config.AdminConfig{StaticDir: staticDirForBuild(t)}, config.AuthConfig{}, deps)

	if h.deps.Service != deps.Service {
		t.Error("Handler did not retain Deps.Service")
	}
	if h.deps.ConfigPath != deps.ConfigPath {
		t.Errorf("Handler.deps.ConfigPath = %q, want %q", h.deps.ConfigPath, deps.ConfigPath)
	}
	if len(h.deps.KnownModules) != len(deps.KnownModules) {
		t.Errorf("Handler.deps.KnownModules = %v, want %v", h.deps.KnownModules, deps.KnownModules)
	}
	if h.deps.AdminRouted != deps.AdminRouted {
		t.Errorf("Handler.deps.AdminRouted = %v, want %v", h.deps.AdminRouted, deps.AdminRouted)
	}
}
