package certmachine

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

func buildConfig(t *testing.T) config.CertmachineConfig {
	t.Helper()
	base := t.TempDir()
	return config.CertmachineConfig{
		StaticDir:           staticDirForBuild(t),
		DBPath:              filepath.Join(base, "data", "certmachine.db"),
		DefaultValidityDays: 365,
		ExpiryWarnDays:      30,
	}
}

// NFR-2 (mirroring multissh): Build wires a handler and starts nothing beyond
// the single DB connection. A leaked goroutine at construction time would
// make the whole single-binary process unrestartable in-place.
func TestBuildStartsNoBackgroundGoroutines(t *testing.T) {
	defer goleak.VerifyNone(t)

	cfg := buildConfig(t)

	srv, err := New(Options{
		StaticDir:           cfg.StaticDir,
		DBPath:              cfg.DBPath,
		LegacyImportDir:     cfg.LegacyImportDir,
		DefaultValidityDays: cfg.DefaultValidityDays,
		ExpiryWarnDays:      cfg.ExpiryWarnDays,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != 200 {
		t.Fatalf("GET / = %d, want 200", rec.Code)
	}

	if err := srv.Close(); err != nil {
		t.Fatalf("Close: %v", err)
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

// Build fails when the db_path directory cannot be created -- part of FR-1's
// build failure policy (an uncreatable directory is not a degrade-in-place
// situation).
func TestBuildRejectsUnwritableDBDir(t *testing.T) {
	cfg := buildConfig(t)
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatalf("write blocker file: %v", err)
	}
	cfg.DBPath = filepath.Join(blocker, "sub", "certmachine.db")

	if _, err := Build(cfg); err == nil {
		t.Fatal("expected Build to fail when the db directory cannot be created")
	}
}

// Permissions: the data directory ends at 0700 and the DB file at 0600, even
// when the directory pre-exists at a looser mode -- os.MkdirAll never
// re-modes an existing directory, so this only holds if Build chmods it
// explicitly (measured, Appendix C-3).
func TestBuildSetsPermissionsEvenWhenDirPreexists(t *testing.T) {
	cfg := buildConfig(t)
	dbDir := filepath.Dir(cfg.DBPath)
	if err := os.MkdirAll(dbDir, 0o755); err != nil {
		t.Fatalf("pre-create db dir: %v", err)
	}

	srv, err := New(Options{
		StaticDir: cfg.StaticDir,
		DBPath:    cfg.DBPath,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer srv.Close()

	dirInfo, err := os.Stat(dbDir)
	if err != nil {
		t.Fatalf("stat db dir: %v", err)
	}
	if perm := dirInfo.Mode().Perm(); perm != 0o700 {
		t.Errorf("db dir mode = %o, want 0700", perm)
	}

	dbInfo, err := os.Stat(cfg.DBPath)
	if err != nil {
		t.Fatalf("stat db file: %v", err)
	}
	if perm := dbInfo.Mode().Perm(); perm != 0o600 {
		t.Errorf("db file mode = %o, want 0600", perm)
	}

	// No -wal or -shm sidecar: the default rollback journal is used, not WAL.
	for _, suffix := range []string{"-wal", "-shm"} {
		if _, err := os.Stat(cfg.DBPath + suffix); !os.IsNotExist(err) {
			t.Errorf("sidecar %s exists (or stat errored: %v), want none", suffix, err)
		}
	}
}

// Close releases the DB handle; a leaked handle would make t.TempDir()
// cleanup flaky on some platforms and would trip a goleak check of the kind
// multissh's build test uses.
func TestCloseReleasesHandle(t *testing.T) {
	defer goleak.VerifyNone(t)

	cfg := buildConfig(t)
	srv, err := New(Options{StaticDir: cfg.StaticDir, DBPath: cfg.DBPath})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := srv.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}
