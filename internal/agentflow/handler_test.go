package agentflow

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func newTestHandler(t *testing.T, dir string) *httptest.ResponseRecorder {
	t.Helper()
	h := NewHandler(NewStore(dir), nil)
	mux := http.NewServeMux()
	h.Register(mux)
	return httptest.NewRecorder()
}

func doRequest(t *testing.T, dir, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	h := NewHandler(NewStore(dir), nil)
	mux := http.NewServeMux()
	h.Register(mux)
	req := httptest.NewRequest(method, path, nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	return w
}

func TestHandleQueue_Success(t *testing.T) {
	dir := t.TempDir()
	qp := filepath.Join(dir, ".agentflow", "queue.json")
	if err := os.MkdirAll(filepath.Dir(qp), 0o755); err != nil {
		t.Fatal(err)
	}
	wantData := QueueData{
		Tasks: []Task{
			{ID: "task-a", Title: "First task", Status: "ready"},
			{ID: "task-b", Title: "Second task", Status: "passed"},
		},
	}
	if err := os.WriteFile(qp, mustMarshal(t, wantData), 0o644); err != nil {
		t.Fatal(err)
	}

	w := doRequest(t, dir, "GET", "/queue")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var got QueueData
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if len(got.Tasks) != 2 || got.Tasks[0].ID != "task-a" {
		t.Fatalf("tasks = %+v, want 2 tasks starting with id=task-a", got.Tasks)
	}
}

func TestHandleQueue_Error_404(t *testing.T) {
	dir := t.TempDir() // empty dir — no queue.json

	w := doRequest(t, dir, "GET", "/queue")

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
	var got map[string]string
	json.Unmarshal(w.Body.Bytes(), &got)
	if got["error"] != "state file not found" {
		t.Fatalf("body = %q, want error about state file", w.Body.String())
	}
}

func TestHandleProgress_Success(t *testing.T) {
	dir := t.TempDir()
	qp := filepath.Join(dir, ".agentflow", "queue.json")
	if err := os.MkdirAll(filepath.Dir(qp), 0o755); err != nil {
		t.Fatal(err)
	}
	wantData := QueueData{
		Tasks: []Task{
			{ID: "t1", Title: "Ready", Status: "ready"},
			{ID: "t2", Title: "Passed", Status: "passed"},
			{ID: "t3", Title: "Blocked task", Status: "blocked"},
			{ID: "t4", Title: "Review needed", Status: "review"},
			{ID: "t5", Title: "Needs repair", Status: "repair"},
			{ID: "t6", Title: "Failed job", Status: "failed"},
			{ID: "t7", Title: "Another ready", Status: "ready"},
		},
	}
	if err := os.WriteFile(qp, mustMarshal(t, wantData), 0o644); err != nil {
		t.Fatal(err)
	}

	w := doRequest(t, dir, "GET", "/progress")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var got ProgressCount
	json.Unmarshal(w.Body.Bytes(), &got)
	// total should count all tasks regardless of status
	if got.Total != 7 {
		t.Fatalf("total = %d, want %d", got.Total, 7)
	}
	if got.Ready != 2 {
		t.Fatalf("ready = %d, want 2", got.Ready)
	}
	if got.Passed != 1 {
		t.Fatalf("passed = %d, want 1", got.Passed)
	}
	if got.Blocked != 1 {
		t.Fatalf("blocked = %d, want 1", got.Blocked)
	}
	if got.Review != 1 {
		t.Fatalf("review = %d, want 1", got.Review)
	}
	if got.Repair != 1 {
		t.Fatalf("repair = %d, want 1", got.Repair)
	}
	if got.Failed != 1 {
		t.Fatalf("failed = %d, want 1", got.Failed)
	}
}

func TestHandleProgress_ZeroCounts_WhenNoQueueFile(t *testing.T) {
	dir := t.TempDir() // no queue.json → return zero counts, not an error

	w := doRequest(t, dir, "GET", "/progress")

	// Progress returns 200 with empty counts rather than 404/503.
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var got ProgressCount
	json.Unmarshal(w.Body.Bytes(), &got)
	if got.Total != 0 || got.Passed != 0 || got.Ready != 0 {
		t.Fatalf("expected all-zero counts, got %+v", got)
	}
}

func TestHandleProgress_MalformedQueueJson(t *testing.T) {
	dir := t.TempDir()
	qp := filepath.Join(dir, ".agentflow", "queue.json")
	if err := os.MkdirAll(filepath.Dir(qp), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(qp, []byte("<not json>"), 0o644); err != nil {
		t.Fatal(err)
	}

	w := doRequest(t, dir, "GET", "/progress")

	// Malformed queue.json → GetProgress returns zero counts (not 503).
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var got ProgressCount
	json.Unmarshal(w.Body.Bytes(), &got)
	if got.Total != 0 {
		t.Fatalf("expected zero total for malformed queue.json, got %d", got.Total)
	}
}

func TestHandleUnknownRoute(t *testing.T) {
	dir := t.TempDir()
	w := doRequest(t, dir, "POST", "/unknown")

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestRemovedExecutionRoutes404(t *testing.T) {
	dir := t.TempDir()
	h := NewHandler(NewStore(dir), nil)
	mux := http.NewServeMux()
	h.Register(mux)

	routes := []struct{ method, path string }{
		{"POST", "/run-next"},
		{"POST", "/tasks/run"},
		{"POST", "/tasks/task-1/approve"},
		{"POST", "/tasks/task-1/repair"},
		{"POST", "/tasks/task-1/fail"},
		{"GET", "/tasks/task-1/stream"},
	}
	for _, rt := range routes {
		req := httptest.NewRequest(rt.method, rt.path, nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		if w.Code != http.StatusNotFound && w.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s %s: status = %d, want 404", rt.method, rt.path, w.Code)
		}
		if w.Code == http.StatusMethodNotAllowed {
			t.Errorf("%s %s: route still registered (405)", rt.method, rt.path)
		}
	}
}
