package static

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func setupDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("index"), 0644); err != nil {
		t.Fatalf("write index.html: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "app.js"), []byte("console.log(1)"), 0644); err != nil {
		t.Fatalf("write app.js: %v", err)
	}
	return dir
}

func TestHandlerServesExistingFile(t *testing.T) {
	dir := setupDir(t)
	h := NewHandler(dir)

	req := httptest.NewRequest(http.MethodGet, "/app.js", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if got := w.Body.String(); got != "console.log(1)" {
		t.Fatalf("expected app.js contents, got %q", got)
	}
}

func TestHandlerFallsBackToIndexForUnknownPath(t *testing.T) {
	dir := setupDir(t)
	h := NewHandler(dir)

	req := httptest.NewRequest(http.MethodGet, "/some/spa/route", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if got := w.Body.String(); got != "index" {
		t.Fatalf("expected index.html contents, got %q", got)
	}
}

func TestHandlerServesIndexAtRoot(t *testing.T) {
	dir := setupDir(t)
	h := NewHandler(dir)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if got := w.Body.String(); got != "index" {
		t.Fatalf("expected index.html contents, got %q", got)
	}
}

func TestHandlerTraversalStaysInsideRoot(t *testing.T) {
	dir := setupDir(t)

	// Create a sibling file outside dir that traversal would target if the
	// path were not rooted/cleaned.
	parent := filepath.Dir(dir)
	secretPath := filepath.Join(parent, "secret.txt")
	if err := os.WriteFile(secretPath, []byte("should not be served"), 0644); err != nil {
		t.Fatalf("write secret.txt: %v", err)
	}
	defer os.Remove(secretPath)

	h := NewHandler(dir)
	req := httptest.NewRequest(http.MethodGet, "/../secret.txt", nil)
	// httptest/net/http will clean the request URI, but simulate a raw path
	// as the handler receives it via URL.Path directly.
	req.URL.Path = "/../secret.txt"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	// The resolved on-disk path is rooted under dir (dir/secret.txt, which
	// does not exist there), so the handler falls through to serving
	// index.html via http.ServeFile. http.ServeFile itself additionally
	// refuses any request whose r.URL.Path still contains a ".." element,
	// responding 400 regardless of the file being served. Either way, the
	// secret file outside dir must never be served.
	if got := w.Body.String(); got == "should not be served" {
		t.Fatalf("traversal escaped root: got secret file contents %q", got)
	}
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 (http.ServeFile rejects \"..\" in URL path), got %d", w.Code)
	}
}

func TestHandlerMissingIndexReturns404(t *testing.T) {
	dir := t.TempDir() // no index.html created

	h := NewHandler(dir)
	req := httptest.NewRequest(http.MethodGet, "/does-not-exist", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 when index.html is also missing, got %d", w.Code)
	}
}
