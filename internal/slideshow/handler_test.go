package slideshow_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cmd184psu/unified-webapp/internal/platform/config"
	"cmd184psu/unified-webapp/internal/slideshow"
)

func newTestHandler(t *testing.T) (*slideshow.Handler, string) {
	t.Helper()
	dir := t.TempDir()
	store, err := slideshow.NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	cfg := config.SlideshowConfig{
		Prefix:         "slides",
		DefaultSubject: "beach",
	}
	return slideshow.NewHandler(store, cfg), dir
}

func serve(t *testing.T, h *slideshow.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	h.Register(mux)
	var reader *strings.Reader
	if body != "" {
		reader = strings.NewReader(body)
	} else {
		reader = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, reader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	return w
}

// ── Config ────────────────────────────────────────────────────────────────────

func TestHandleConfigGet(t *testing.T) {
	h, _ := newTestHandler(t)
	w := serve(t, h, "GET", "/config", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", w.Code)
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp["prefix"] != "slides" {
		t.Errorf("prefix: got %v", resp["prefix"])
	}
	if resp["defaultSubject"] != "beach" {
		t.Errorf("defaultSubject: got %v", resp["defaultSubject"])
	}
}

func TestHandleConfigGet_TrailingSlash(t *testing.T) {
	h, _ := newTestHandler(t)
	w := serve(t, h, "GET", "/config/", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", w.Code)
	}
}

func TestHandleConfigPost_UpdatesDefaultSubject(t *testing.T) {
	h, _ := newTestHandler(t)
	w := serve(t, h, "POST", "/config", `{"prefix":"slides","defaultSubject":"mountains"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200\nbody: %s", w.Code, w.Body.String())
	}
	// Verify the update is reflected in subsequent GET.
	w2 := serve(t, h, "GET", "/config", "")
	var resp map[string]interface{}
	json.Unmarshal(w2.Body.Bytes(), &resp)
	if resp["defaultSubject"] != "mountains" {
		t.Errorf("defaultSubject after POST: got %v", resp["defaultSubject"])
	}
}

func TestHandleConfigPost_InvalidJSON(t *testing.T) {
	h, _ := newTestHandler(t)
	w := serve(t, h, "POST", "/config", "not-json")
	if w.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d", w.Code)
	}
}

// ── Subjects ──────────────────────────────────────────────────────────────────

func TestHandleSubjects_Empty(t *testing.T) {
	h, _ := newTestHandler(t)
	w := serve(t, h, "GET", "/items", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", w.Code)
	}
	var subjects []interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &subjects); err != nil {
		t.Fatalf("unmarshal: %v\nbody: %s", err, w.Body.String())
	}
	if len(subjects) != 0 {
		t.Errorf("want empty array, got %d", len(subjects))
	}
}

func TestHandleSubjects_WithImages(t *testing.T) {
	h, dir := newTestHandler(t)
	os.MkdirAll(filepath.Join(dir, "beach"), 0755)
	os.WriteFile(filepath.Join(dir, "beach", "photo.jpg"), []byte("img"), 0644)

	w := serve(t, h, "GET", "/items", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d", w.Code)
	}
	var subjects []map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &subjects)
	if len(subjects) != 1 {
		t.Fatalf("want 1 subject, got %d", len(subjects))
	}
	if subjects[0]["subject"] != "beach" {
		t.Errorf("subject name: got %v", subjects[0]["subject"])
	}
}

// ── Image serving ─────────────────────────────────────────────────────────────

func TestHandleImage_ServesFile(t *testing.T) {
	h, dir := newTestHandler(t)
	os.MkdirAll(filepath.Join(dir, "beach"), 0755)
	os.WriteFile(filepath.Join(dir, "beach", "photo.jpg"), []byte("JPEG_DATA"), 0644)

	w := serve(t, h, "GET", "/slides/beach/photo.jpg", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", w.Code)
	}
	if !strings.Contains(w.Body.String(), "JPEG_DATA") {
		t.Error("response body should contain image data")
	}
}

func TestHandleImage_MissingFile_404(t *testing.T) {
	h, _ := newTestHandler(t)
	w := serve(t, h, "GET", "/slides/beach/nonexistent.jpg", "")
	if w.Code != http.StatusNotFound {
		t.Errorf("want 404, got %d", w.Code)
	}
}

func TestHandleImage_NonImageExtension_404(t *testing.T) {
	h, dir := newTestHandler(t)
	os.MkdirAll(filepath.Join(dir, "beach"), 0755)
	os.WriteFile(filepath.Join(dir, "beach", "secret.txt"), []byte("secret"), 0644)

	w := serve(t, h, "GET", "/slides/beach/secret.txt", "")
	if w.Code != http.StatusNotFound {
		t.Errorf("want 404 for non-image extension, got %d", w.Code)
	}
}

func TestHandleImage_DotPrefixSubject_404(t *testing.T) {
	h, _ := newTestHandler(t)
	w := serve(t, h, "GET", "/slides/.hidden/photo.jpg", "")
	if w.Code != http.StatusNotFound {
		t.Errorf("want 404 for dot-prefix subject, got %d", w.Code)
	}
}
