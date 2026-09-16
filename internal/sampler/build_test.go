package sampler_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"cmd184psu/unified-webapp/internal/platform/config"
	"cmd184psu/unified-webapp/internal/platform/static"
	"cmd184psu/unified-webapp/internal/sampler"
)

func writeFile(t *testing.T, dir, name, contents string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(contents), 0o644); err != nil {
		t.Fatalf("writeFile %s: %v", name, err)
	}
}

// Build succeeds and returns a handler serving the module's own static
// content at "/".
func TestBuild_Succeeds(t *testing.T) {
	staticDir := t.TempDir()
	writeFile(t, staticDir, "index.html", "sampler index")

	cfg := config.SamplerConfig{StaticDir: staticDir}
	h, err := sampler.Build(cfg)
	if err != nil {
		t.Fatalf("Build: unexpected error: %v", err)
	}

	srv := httptest.NewServer(h)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /: expected 200, got %d", resp.StatusCode)
	}
}

// Build mounts the shared asset tree at /shared/ when SharedStaticDir is
// set, via the same static.MountShared call the handler makes.
func TestBuild_ServesSharedAssets(t *testing.T) {
	staticDir := t.TempDir()
	writeFile(t, staticDir, "index.html", "sampler index")

	sharedDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(sharedDir, "dist"), 0o750); err != nil {
		t.Fatalf("mkdir dist: %v", err)
	}
	writeFile(t, filepath.Join(sharedDir, "dist"), "shared.css", "body{}")

	cfg := config.SamplerConfig{StaticDir: staticDir, SharedStaticDir: sharedDir}
	h, err := sampler.Build(cfg)
	if err != nil {
		t.Fatalf("Build: unexpected error: %v", err)
	}

	srv := httptest.NewServer(h)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/shared/dist/shared.css")
	if err != nil {
		t.Fatalf("GET /shared/dist/shared.css: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /shared/dist/shared.css: expected 200, got %d", resp.StatusCode)
	}
}

// fakeMuxer is a static.Muxer implementation that is not a *http.ServeMux.
// This is the error path Build's static.MountShared call propagates via its
// `if err != nil { return nil, err }` guard -- Build's own mux is always a
// real *http.ServeMux, so Build itself can never hit this branch; this test
// exercises the underlying static.MountShared contract Build relies on
// directly, mirroring internal/platform/static/shared_test.go's
// TestMountSharedRejectsNonServeMux.
type fakeMuxer struct{}

func (fakeMuxer) Handle(pattern string, h http.Handler) {}

func TestMountShared_RejectsNonServeMux(t *testing.T) {
	if err := static.MountShared(fakeMuxer{}, t.TempDir()); err == nil {
		t.Fatal("expected static.MountShared to error on a non-*http.ServeMux Muxer")
	}
}
