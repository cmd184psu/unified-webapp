package menuserver_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cmd184psu/unified-webapp/internal/menuserver"
	"cmd184psu/unified-webapp/internal/platform/config"
)

func newTestHandler(t *testing.T) (*menuserver.Handler, string) {
	t.Helper()
	dir := t.TempDir()
	store, err := menuserver.NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	cfg := config.MenuserverConfig{ShowAllPages: false}
	return menuserver.NewHandler(store, cfg), dir
}

func serve(t *testing.T, h *menuserver.Handler, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	h.Register(mux)
	req := httptest.NewRequest(method, path, nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	return w
}

// ── Config ────────────────────────────────────────────────────────────────────

func TestHandleConfig(t *testing.T) {
	h, _ := newTestHandler(t)
	w := serve(t, h, "GET", "/config")
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", w.Code)
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := resp["showAllPages"]; !ok {
		t.Error("response missing showAllPages field")
	}
}

func TestHandleConfig_TrailingSlash(t *testing.T) {
	h, _ := newTestHandler(t)
	w := serve(t, h, "GET", "/config/")
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", w.Code)
	}
}

func TestHandleConfig_ShowAllPages(t *testing.T) {
	dir := t.TempDir()
	store, _ := menuserver.NewStore(dir)
	cfg := config.MenuserverConfig{ShowAllPages: true}
	h := menuserver.NewHandler(store, cfg)

	w := serve(t, h, "GET", "/config")
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["showAllPages"] != true {
		t.Errorf("showAllPages: got %v, want true", resp["showAllPages"])
	}
}

// ── Subjects ──────────────────────────────────────────────────────────────────

func TestHandleSubjects_Empty(t *testing.T) {
	h, _ := newTestHandler(t)
	w := serve(t, h, "GET", "/items")
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

func TestHandleSubjects_WithData(t *testing.T) {
	h, dir := newTestHandler(t)
	os.MkdirAll(filepath.Join(dir, "home"), 0755)
	os.WriteFile(filepath.Join(dir, "home", "networking.json"), []byte(`{}`), 0644)

	w := serve(t, h, "GET", "/items")
	var subjects []map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &subjects)
	if len(subjects) != 1 {
		t.Fatalf("want 1 subject, got %d", len(subjects))
	}
	if subjects[0]["subject"] != "home" {
		t.Errorf("subject: got %v", subjects[0]["subject"])
	}
}

// ── Menu ──────────────────────────────────────────────────────────────────────

func TestHandleMenu_ServesFile(t *testing.T) {
	h, dir := newTestHandler(t)
	os.MkdirAll(filepath.Join(dir, "home"), 0755)
	payload := `{"id":"net","title":"Networking","sites":[]}`
	os.WriteFile(filepath.Join(dir, "home", "networking.json"), []byte(payload), 0644)

	w := serve(t, h, "GET", "/menus/home/networking.json")
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Networking") {
		t.Errorf("body missing expected content: %s", w.Body.String())
	}
}

func TestHandleMenu_MissingFile_404(t *testing.T) {
	h, _ := newTestHandler(t)
	w := serve(t, h, "GET", "/menus/home/nonexistent.json")
	if w.Code != http.StatusNotFound {
		t.Errorf("want 404, got %d", w.Code)
	}
}

func TestHandleMenu_DotPrefixSubject_404(t *testing.T) {
	h, _ := newTestHandler(t)
	w := serve(t, h, "GET", "/menus/.hidden/file.json")
	if w.Code != http.StatusNotFound {
		t.Errorf("want 404 for dot-prefix subject, got %d", w.Code)
	}
}

func TestHandleMenu_ContentTypeJson(t *testing.T) {
	h, dir := newTestHandler(t)
	os.MkdirAll(filepath.Join(dir, "home"), 0755)
	os.WriteFile(filepath.Join(dir, "home", "net.json"), []byte(`{}`), 0644)

	w := serve(t, h, "GET", "/menus/home/net.json")
	ct := w.Header().Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type: got %q, want application/json", ct)
	}
}
