package multissh

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.uber.org/goleak"

	"cmd184psu/unified-webapp/internal/platform/config"
)

func staticDirForBuild(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html></html>"), 0o644); err != nil {
		t.Fatalf("write index.html: %v", err)
	}
	return dir
}

func buildConfig(t *testing.T) config.MultisshConfig {
	t.Helper()
	base := t.TempDir()
	return config.MultisshConfig{
		StaticDir:   staticDirForBuild(t),
		SSHDir:      filepath.Join(base, "ssh"),
		UploadDir:   filepath.Join(base, "uploads"),
		HostsPath:   filepath.Join(base, "state", "hosts.json"),
		MaxSessions: 3,
	}
}

// NFR-2: Build wires a handler and starts nothing. A module that leaks a
// goroutine at construction time makes the whole single-binary process
// unrestartable in-place, and the leak is invisible until it accumulates --
// so it is asserted rather than reasoned about.
func TestBuildStartsNoBackgroundGoroutines(t *testing.T) {
	defer goleak.VerifyNone(t)

	cfg := buildConfig(t)
	if err := os.MkdirAll(cfg.SSHDir, 0o700); err != nil {
		t.Fatalf("mkdir ssh dir: %v", err)
	}

	h, err := Build(cfg)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if h == nil {
		t.Fatal("Build returned a nil handler")
	}
	// Serving one request must not leave anything running either.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/config", nil))
	if rec.Code != 200 {
		t.Fatalf("GET /api/config = %d, want 200", rec.Code)
	}
}

// Build fails loudly on a bad static_dir instead of serving 404s that look
// like a routing bug (the boot-time contract the dispatcher's 503 depends on).
func TestBuildRejectsUnusableStaticDir(t *testing.T) {
	cfg := buildConfig(t)
	cfg.StaticDir = filepath.Join(t.TempDir(), "does-not-exist")

	if _, err := Build(cfg); err == nil {
		t.Fatal("expected Build to fail on a missing static_dir")
	} else if !strings.Contains(err.Error(), "static_dir") {
		t.Fatalf("error should name static_dir, got: %v", err)
	}
}
