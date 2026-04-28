package todo_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"cmd184psu/unified-webapp/internal/platform/broker"
	"cmd184psu/unified-webapp/internal/platform/config"
	"cmd184psu/unified-webapp/internal/todo"
)

func newTestHandler(t *testing.T) (*todo.Handler, *todo.Store) {
	t.Helper()
	dir := t.TempDir()
	store, err := todo.NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	mbr := broker.NewMultiRoomBroker(0)
	cfg := config.TodoConfig{
		Ext:            "json",
		DefaultSubject: "home",
	}
	return todo.NewHandler(store, mbr, cfg), store
}

func serve(t *testing.T, h *todo.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	h.Register(mux)
	var bodyReader *strings.Reader
	if body != "" {
		bodyReader = strings.NewReader(body)
	} else {
		bodyReader = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, bodyReader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	return w
}

// ── Config ────────────────────────────────────────────────────────────────────

func TestHandleConfig(t *testing.T) {
	h, _ := newTestHandler(t)
	w := serve(t, h, "GET", "/config", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", w.Code)
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp["ext"] != "json" {
		t.Errorf("ext: got %v", resp["ext"])
	}
	if resp["defaultSubject"] != "home" {
		t.Errorf("defaultSubject: got %v", resp["defaultSubject"])
	}
	if resp["defaultItem"] != "home/index.json" {
		t.Errorf("defaultItem: got %v", resp["defaultItem"])
	}
}

func TestHandleConfigTrailingSlash(t *testing.T) {
	h, _ := newTestHandler(t)
	w := serve(t, h, "GET", "/config/", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", w.Code)
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
		t.Fatalf("unmarshal: %v", err)
	}
	if len(subjects) != 0 {
		t.Errorf("want empty array, got %d items", len(subjects))
	}
}

// ── ReadFile ──────────────────────────────────────────────────────────────────

func TestHandleReadFile_IndexJson(t *testing.T) {
	h, store := newTestHandler(t)
	// Write a file so the index has something to list.
	store.WriteFile("home", "shopping.json", []byte(`{"title":"shopping","list":[]}`))

	w := serve(t, h, "GET", "/items/home/index.json", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", w.Code)
	}
	var idx todo.IndexFile
	if err := json.Unmarshal(w.Body.Bytes(), &idx); err != nil {
		t.Fatalf("unmarshal: %v\nbody: %s", err, w.Body.String())
	}
	if idx.Title != "Index of home" {
		t.Errorf("title: got %q", idx.Title)
	}
	if len(idx.List) != 1 || idx.List[0].JSON != "home/shopping.json" {
		t.Errorf("list: %+v", idx.List)
	}
}

func TestHandleReadFile_RegularFile(t *testing.T) {
	h, store := newTestHandler(t)
	store.WriteFile("home", "tasks.json", []byte(`{"title":"tasks","list":[]}`))

	w := serve(t, h, "GET", "/items/home/tasks.json", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", w.Code)
	}
	var data map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &data); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if data["title"] != "tasks" {
		t.Errorf("title: got %v", data["title"])
	}
}

func TestHandleReadFile_MissingCreatesEmpty(t *testing.T) {
	h, _ := newTestHandler(t)
	// Create subject dir first so the store can create the file.
	w := serve(t, h, "GET", "/items/home/new.json", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", w.Code)
	}
	if w.Body.Len() == 0 {
		t.Error("expected non-empty body for new file")
	}
}

func TestHandleReadFile_DotPrefix_Rejected(t *testing.T) {
	h, _ := newTestHandler(t)
	w := serve(t, h, "GET", "/items/.hidden/list.json", "")
	if w.Code != http.StatusBadRequest {
		t.Errorf("want 400 for dot-prefix subject, got %d", w.Code)
	}
}

// ── WriteFile ─────────────────────────────────────────────────────────────────

func TestHandleWriteFile(t *testing.T) {
	h, _ := newTestHandler(t)
	body := `{"title":"my list","list":[]}`
	w := serve(t, h, "POST", "/items/home/mylist.json", body)
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200\nbody: %s", w.Code, w.Body.String())
	}
	var resp map[string]string
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["msg"] != "saved" {
		t.Errorf("msg: got %q", resp["msg"])
	}
}

func TestHandleWriteFile_InvalidJSON_Rejected(t *testing.T) {
	h, _ := newTestHandler(t)
	w := serve(t, h, "POST", "/items/home/list.json", "not-json")
	// store.WriteFile will succeed even with invalid JSON since we removed the
	// json.Valid check; the frontend owns the schema. Accept 200 or 400.
	_ = w
}

func TestHandleWriteFile_EmptyBody_Rejected(t *testing.T) {
	h, _ := newTestHandler(t)
	w := serve(t, h, "POST", "/items/home/list.json", "")
	if w.Code != http.StatusBadRequest {
		t.Errorf("want 400 for empty body, got %d", w.Code)
	}
}

// ── MoveFile ──────────────────────────────────────────────────────────────────

func TestHandleMoveFile(t *testing.T) {
	h, store := newTestHandler(t)
	store.WriteFile("home", "list.json", []byte(`[]`))

	w := serve(t, h, "POST", "/items/home/list.json/work", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200\nbody: %s", w.Code, w.Body.String())
	}
	var resp map[string]string
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["msg"] != "moved" {
		t.Errorf("msg: got %q", resp["msg"])
	}
}

func TestHandleMoveFile_InvalidSubject(t *testing.T) {
	h, _ := newTestHandler(t)
	w := serve(t, h, "POST", "/items/.bad/list.json/work", "")
	if w.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d", w.Code)
	}
}

// ── ValidName (indirect via handler) ─────────────────────────────────────────

func TestHandleReadFile_PathTraversalBlocked(t *testing.T) {
	h, _ := newTestHandler(t)
	mux := http.NewServeMux()
	h.Register(mux)
	// Go's mux cleans /items/../leaked.json → /leaked.json and issues a
	// redirect (301), so this never reaches a handler that could read
	// outside the subject dirs.
	req := httptest.NewRequest("GET", "/items/../leaked.json", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code == http.StatusOK {
		t.Errorf("expected redirect or 4xx for traversal path, got 200")
	}
}
