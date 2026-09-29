package static

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// setupSharedDir builds a t.TempDir() fixture with dist/ and public/
// subtrees, matching the shape /shared/ is meant to serve.
// /shared/dist/shared.css does not exist in the repo tree until C4, so every
// case here is driven against this fixture, not the real web/shared tree.
func setupSharedDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "dist"), 0o755); err != nil {
		t.Fatalf("mkdir dist: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "public"), 0o755); err != nil {
		t.Fatalf("mkdir public: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "dist", "shared.css"), []byte("body{color:red}"), 0o644); err != nil {
		t.Fatalf("write shared.css: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "dist", "shared.mjs"), []byte("export {};"), 0o644); err != nil {
		t.Fatalf("write shared.mjs: %v", err)
	}
	return dir
}

func doShared(h http.Handler, method, target string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

// Clause 1 (A8.1): prefix stripping happens inside SharedHandler, and the
// resolved on-disk path for /shared/dist/shared.css is <dir>/dist/shared.css.
func TestSharedPrefixStrippingResolvesOnDiskPath(t *testing.T) {
	dir := setupSharedDir(t)
	h := SharedHandler(dir)

	w := doShared(h, http.MethodGet, "/shared/dist/shared.css")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if got := w.Body.String(); got != "body{color:red}" {
		t.Fatalf("expected shared.css contents, got %q", got)
	}

	// Confirm it is specifically <dir>/dist/shared.css being served, not
	// some other resolution: replace the file with different content at
	// exactly that on-disk path and confirm the response changes to match.
	want := "body{color:blue}"
	if err := os.WriteFile(filepath.Join(dir, "dist", "shared.css"), []byte(want), 0o644); err != nil {
		t.Fatalf("rewrite shared.css: %v", err)
	}
	w2 := doShared(h, http.MethodGet, "/shared/dist/shared.css")
	if got := w2.Body.String(); got != want {
		t.Fatalf("expected updated shared.css contents at <dir>/dist/shared.css, got %q", got)
	}
}

// Clause 2 (A8.4 clause 2 / R9): first-segment allowlist. Only dist/ and
// public/ are reachable; web/shared/ts and web/shared/css are never
// HTTP-reachable through /shared/.
func TestSharedFirstSegmentAllowlist(t *testing.T) {
	dir := setupSharedDir(t)
	if err := os.MkdirAll(filepath.Join(dir, "ts"), 0o755); err != nil {
		t.Fatalf("mkdir ts: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ts", "modal.ts"), []byte("export {}"), 0o644); err != nil {
		t.Fatalf("write modal.ts: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "css"), 0o755); err != nil {
		t.Fatalf("mkdir css: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "css", "tokens.css"), []byte("body{}"), 0o644); err != nil {
		t.Fatalf("write tokens.css: %v", err)
	}
	h := SharedHandler(dir)

	for _, target := range []string{"/shared/ts/modal.ts", "/shared/css/tokens.css"} {
		w := doShared(h, http.MethodGet, target)
		if w.Code != http.StatusNotFound {
			t.Errorf("%s: expected 404, got %d", target, w.Code)
		}
	}

	// dist/ and public/ remain reachable.
	if w := doShared(h, http.MethodGet, "/shared/dist/shared.css"); w.Code != http.StatusOK {
		t.Fatalf("/shared/dist/shared.css: expected 200, got %d", w.Code)
	}
}

// Clause 3 (A8.4 clause 1 / R12): lexical path confinement via path.Clean.
func TestSharedLexicalPathConfinement(t *testing.T) {
	dir := setupSharedDir(t)
	h := SharedHandler(dir)

	probes := []string{
		"/shared/../../etc/passwd",
		"/shared/dist/../../../etc/passwd",
		"/shared/dist/%2e%2e%2f%2e%2e%2fetc/passwd",
	}
	for _, target := range probes {
		w := doShared(h, http.MethodGet, target)
		if w.Code != http.StatusNotFound {
			t.Errorf("%s: expected 404, got %d", target, w.Code)
		}
	}
}

// Clause 4 (A8.4 clause 5): root confinement via os.Root / os.OpenRoot --
// symlinks that escape the root are 404 even though path.Clean cannot see
// them, and a regular file through the same handler still serves.
func TestSharedRootConfinementSymlink(t *testing.T) {
	dir := setupSharedDir(t)
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("nope"), 0o644); err != nil {
		t.Fatalf("write secret.txt: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "dist", "evil")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	h := SharedHandler(dir)

	w := doShared(h, http.MethodGet, "/shared/dist/evil/secret.txt")
	if w.Code != http.StatusNotFound {
		t.Fatalf("symlinked escape: expected 404, got %d", w.Code)
	}

	// A regular file through the same handler still serves.
	w2 := doShared(h, http.MethodGet, "/shared/dist/shared.css")
	if w2.Code != http.StatusOK {
		t.Fatalf("regular file after symlink probe: expected 200, got %d", w2.Code)
	}
}

// Clause 5 (A8.4 clause 6): dotfile refusal, even for a dotfile inside the
// root that does not escape it.
func TestSharedDotfileRefusal(t *testing.T) {
	dir := setupSharedDir(t)
	if err := os.WriteFile(filepath.Join(dir, "dist", ".hidden"), []byte("secret"), 0o644); err != nil {
		t.Fatalf("write .hidden: %v", err)
	}
	h := SharedHandler(dir)

	w := doShared(h, http.MethodGet, "/shared/dist/.hidden")
	if w.Code != http.StatusNotFound {
		t.Fatalf("dotfile: expected 404, got %d", w.Code)
	}
}

// Clause 6 (A8.4 clause 3): directory requests are 404, never a listing.
func TestSharedDirectoryRequestIs404(t *testing.T) {
	dir := setupSharedDir(t)
	h := SharedHandler(dir)

	w := doShared(h, http.MethodGet, "/shared/dist")
	if w.Code != http.StatusNotFound {
		t.Fatalf("directory: expected 404, got %d", w.Code)
	}
	w2 := doShared(h, http.MethodGet, "/shared/dist/")
	if w2.Code != http.StatusNotFound {
		t.Fatalf("directory with trailing slash: expected 404, got %d", w2.Code)
	}
}

// Clause 7 (A8.4 clause 4): method allowlist -- GET and HEAD only, anything
// else is 405 with Allow: GET, HEAD.
func TestSharedMethodAllowlist(t *testing.T) {
	dir := setupSharedDir(t)
	h := SharedHandler(dir)

	for _, method := range []string{http.MethodGet, http.MethodHead} {
		w := doShared(h, method, "/shared/dist/shared.css")
		if w.Code != http.StatusOK {
			t.Errorf("%s: expected 200, got %d", method, w.Code)
		}
	}

	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		w := doShared(h, method, "/shared/dist/shared.css")
		if w.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s: expected 405, got %d", method, w.Code)
		}
		if got := w.Header().Get("Allow"); got != "GET, HEAD" {
			t.Errorf("%s: expected Allow: GET, HEAD, got %q", method, got)
		}
	}
}

// Clause 8 (A8.6 / R11): caching contract -- Last-Modified present on GET
// and HEAD, If-Modified-Since yields a bodyless 304, and no ETag.
func TestSharedCachingContract(t *testing.T) {
	dir := setupSharedDir(t)
	h := SharedHandler(dir)

	for _, method := range []string{http.MethodGet, http.MethodHead} {
		w := doShared(h, method, "/shared/dist/shared.css")
		if w.Code != http.StatusOK {
			t.Fatalf("%s: expected 200, got %d", method, w.Code)
		}
		if w.Header().Get("Last-Modified") == "" {
			t.Errorf("%s: expected Last-Modified header, got none", method)
		}
		if w.Header().Get("ETag") != "" {
			t.Errorf("%s: expected no ETag, got %q", method, w.Header().Get("ETag"))
		}
	}

	// If-Modified-Since yields a bodyless 304.
	req := httptest.NewRequest(http.MethodGet, "/shared/dist/shared.css", nil)
	req.Header.Set("If-Modified-Since", time.Now().Add(time.Hour).UTC().Format(http.TimeFormat))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusNotModified {
		t.Fatalf("If-Modified-Since: expected 304, got %d", w.Code)
	}
	if w.Body.Len() != 0 {
		t.Fatalf("If-Modified-Since: expected empty body, got %d bytes", w.Body.Len())
	}
}

// Clause 9 (A8.6 / R10): .mjs Content-Type starts with text/javascript.
func TestSharedMjsContentType(t *testing.T) {
	dir := setupSharedDir(t)
	h := SharedHandler(dir)

	w := doShared(h, http.MethodGet, "/shared/dist/shared.mjs")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	ct := w.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "text/javascript") {
		t.Fatalf("expected Content-Type to start with text/javascript, got %q", ct)
	}
}

// WithShared("") returns next unchanged.
func TestWithSharedEmptyDirReturnsNextUnchanged(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})
	h := WithShared(next, "")

	w := doShared(h, http.MethodGet, "/shared/dist/shared.css")
	if w.Code != http.StatusTeapot {
		t.Fatalf("expected WithShared(\"\") to be next unchanged (418), got %d", w.Code)
	}
}

// WithShared diverts /shared/* to SharedHandler(dir) and leaves everything
// else to next.
func TestWithSharedDivertsSharedPrefix(t *testing.T) {
	dir := setupSharedDir(t)
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})
	h := WithShared(next, dir)

	w := doShared(h, http.MethodGet, "/shared/dist/shared.css")
	if w.Code != http.StatusOK {
		t.Fatalf("/shared/dist/shared.css: expected 200, got %d", w.Code)
	}

	w2 := doShared(h, http.MethodGet, "/other/path")
	if w2.Code != http.StatusTeapot {
		t.Fatalf("/other/path: expected next unchanged (418), got %d", w2.Code)
	}
}

// MountShared registers SharedHandler(dir) at /shared/ on a *http.ServeMux.
func TestMountSharedOnServeMux(t *testing.T) {
	dir := setupSharedDir(t)
	mux := http.NewServeMux()
	if err := MountShared(mux, dir); err != nil {
		t.Fatalf("MountShared: unexpected error: %v", err)
	}

	w := doShared(mux, http.MethodGet, "/shared/dist/shared.css")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

// MountShared errors when given a Muxer that is not a *http.ServeMux.
type fakeMuxer struct{}

func (fakeMuxer) Handle(pattern string, h http.Handler) {}

func TestMountSharedRejectsNonServeMux(t *testing.T) {
	dir := setupSharedDir(t)
	if err := MountShared(fakeMuxer{}, dir); err == nil {
		t.Fatal("expected MountShared to error on a non-*http.ServeMux Muxer")
	}
}

// SharedHandler with a directory that fails to open (does not exist)
// degrades to 404 on every request rather than panicking or erroring the
// caller, matching the boot-time non-fatal posture.
func TestSharedHandlerMissingDirDegradesTo404(t *testing.T) {
	h := SharedHandler(filepath.Join(t.TempDir(), "does-not-exist"))

	w := doShared(h, http.MethodGet, "/shared/dist/shared.css")
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}
