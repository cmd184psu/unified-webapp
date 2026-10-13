package agentflow

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testApprovedPlan() *Plan {
	p := testPlan()
	p.Status = "approved"
	return p
}

func seedPlan(t *testing.T, dir string, plan *Plan) {
	t.Helper()
	s := NewStore(dir)
	if err := os.MkdirAll(filepath.Join(dir, ".agentflow"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := s.WritePlan(plan); err != nil {
		t.Fatal(err)
	}
}

func validQueueJSON() string {
	return `{
		"frd_id": "frd-001",
		"plan_status": "approved",
		"tasks": [
			{
				"id": "T001", "phase": "P1", "title": "First",
				"status": "ready", "depends_on": [],
				"prompt": "Create a.go with the thing.",
				"working_dir": ".", "verify": "test -f a.go",
				"acceptance": ["R1: the thing exists"],
				"files_touched": ["a.go"],
				"attempts": 0, "max_attempts": 3
			},
			{
				"id": "T002", "phase": "P1", "title": "Second",
				"status": "blocked", "depends_on": ["T001"],
				"prompt": "Extend a.go with the other thing.",
				"working_dir": ".", "verify": "go test ./... ",
				"acceptance": ["R1: the other thing works"],
				"files_touched": ["a.go"],
				"attempts": 0, "max_attempts": 3
			}
		]
	}`
}

func doCompile(t *testing.T, h *Handler) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", "/tasks/compile", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	mux := http.NewServeMux()
	h.Register(mux)
	mux.ServeHTTP(w, req)
	return w
}

func TestPostCompileTasks_HappyPath(t *testing.T) {
	dir := t.TempDir()
	seedPlan(t, dir, testApprovedPlan())
	var gotPrompt, gotModel string
	e := testEngineWithStub(t, dir, func(ctx context.Context, model, prompt, host string) (string, error) {
		gotModel, gotPrompt = model, prompt
		return validQueueJSON(), nil
	})
	h := NewHandler(NewStore(dir), e)

	w := doCompile(t, h)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}
	var got struct {
		Queue    TaskQueue `json:"queue"`
		Warnings []string  `json:"warnings"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(got.Queue.Tasks) != 2 {
		t.Fatalf("tasks = %d, want 2", len(got.Queue.Tasks))
	}
	if len(got.Warnings) != 0 {
		t.Fatalf("warnings = %v, want none", got.Warnings)
	}
	if got.Queue.FRDID != "frd-001" || got.Queue.PlanStatus != "approved" {
		t.Fatalf("queue header = %+v, want frd-001/approved", got.Queue)
	}
	if got.Queue.CompiledDate == "" {
		t.Fatal("compiled_date empty, want today")
	}
	if gotModel != "p" {
		t.Fatalf("model = %q, want planner model p", gotModel)
	}
	if !strings.Contains(gotPrompt, "Write code, then tests.") {
		t.Fatalf("prompt missing plan approach; prompt = %q", gotPrompt)
	}
	// Persisted to queue.json
	persisted, err := NewStore(dir).GetQueue()
	if err != nil {
		t.Fatalf("queue.json not persisted: %v", err)
	}
	if len(persisted.Tasks) != 2 || persisted.Tasks[1].ID != "T002" {
		t.Fatalf("persisted tasks = %+v, want T001+T002", persisted.Tasks)
	}
}

func TestPostCompileTasks_StatusNormalization(t *testing.T) {
	dir := t.TempDir()
	seedPlan(t, dir, testApprovedPlan())
	stub := strings.Replace(validQueueJSON(), `"status": "blocked", "depends_on": ["T001"]`, `"status": "ready", "depends_on": ["T001"]`, 1)
	e := testEngineWithStub(t, dir, func(ctx context.Context, model, prompt, host string) (string, error) {
		return stub, nil
	})
	h := NewHandler(NewStore(dir), e)

	w := doCompile(t, h)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}
	var got struct {
		Queue    TaskQueue `json:"queue"`
		Warnings []string  `json:"warnings"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(got.Queue.Tasks) != 2 {
		t.Fatalf("tasks = %d, want 2", len(got.Queue.Tasks))
	}
	if got.Queue.Tasks[1].Status != "blocked" {
		t.Fatalf("T002 status = %q, want blocked", got.Queue.Tasks[1].Status)
	}
	if len(got.Warnings) != 1 || !strings.Contains(got.Warnings[0], "T002") {
		t.Fatalf("warnings = %v, want one mentioning T002", got.Warnings)
	}
}

func TestPostCompileTasks_ForwardDep(t *testing.T) {
	dir := t.TempDir()
	seedPlan(t, dir, testApprovedPlan())
	stub := strings.Replace(validQueueJSON(), `"depends_on": []`, `"depends_on": ["T002"]`, 1)
	e := testEngineWithStub(t, dir, func(ctx context.Context, model, prompt, host string) (string, error) {
		return stub, nil
	})
	h := NewHandler(NewStore(dir), e)

	w := doCompile(t, h)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusInternalServerError, w.Body.String())
	}
}

func TestPostCompileTasks_NonSequentialIDs(t *testing.T) {
	dir := t.TempDir()
	seedPlan(t, dir, testApprovedPlan())
	stub := strings.Replace(validQueueJSON(), `"id": "T002"`, `"id": "T003"`, 1)
	e := testEngineWithStub(t, dir, func(ctx context.Context, model, prompt, host string) (string, error) {
		return stub, nil
	})
	h := NewHandler(NewStore(dir), e)

	w := doCompile(t, h)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusInternalServerError, w.Body.String())
	}
}

func TestPostCompileTasks_AbsolutePath(t *testing.T) {
	dir := t.TempDir()
	seedPlan(t, dir, testApprovedPlan())
	stub := strings.Replace(validQueueJSON(), `"files_touched": ["a.go"]`, `"files_touched": ["/opt/x.go"]`, 1)
	e := testEngineWithStub(t, dir, func(ctx context.Context, model, prompt, host string) (string, error) {
		return stub, nil
	})
	h := NewHandler(NewStore(dir), e)

	w := doCompile(t, h)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusInternalServerError, w.Body.String())
	}
}

func TestPostCompileTasks_EmptyFilesTouched(t *testing.T) {
	dir := t.TempDir()
	seedPlan(t, dir, testApprovedPlan())
	stub := strings.Replace(validQueueJSON(), `"files_touched": ["a.go"]`, `"files_touched": []`, 1)
	e := testEngineWithStub(t, dir, func(ctx context.Context, model, prompt, host string) (string, error) {
		return stub, nil
	})
	h := NewHandler(NewStore(dir), e)

	w := doCompile(t, h)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusInternalServerError, w.Body.String())
	}
}

func TestPostCompileTasks_MissingPlan(t *testing.T) {
	dir := t.TempDir() // no plan.json
	e := testEngineWithStub(t, dir, func(ctx context.Context, model, prompt, host string) (string, error) {
		t.Fatal("model must not run without a plan")
		return "", nil
	})
	h := NewHandler(NewStore(dir), e)

	w := doCompile(t, h)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusNotFound, w.Body.String())
	}
}

func TestPostCompileTasks_DraftPlan(t *testing.T) {
	dir := t.TempDir()
	seedPlan(t, dir, testPlan()) // status draft
	e := testEngineWithStub(t, dir, func(ctx context.Context, model, prompt, host string) (string, error) {
		t.Fatal("model must not run on an unapproved plan")
		return "", nil
	})
	h := NewHandler(NewStore(dir), e)

	w := doCompile(t, h)

	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusConflict, w.Body.String())
	}
}

func TestPostCompileTasks_InvalidModelJSON(t *testing.T) {
	dir := t.TempDir()
	seedPlan(t, dir, testApprovedPlan())
	e := testEngineWithStub(t, dir, func(ctx context.Context, model, prompt, host string) (string, error) {
		return "not json at all", nil
	})
	h := NewHandler(NewStore(dir), e)

	w := doCompile(t, h)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusInternalServerError, w.Body.String())
	}
}

func TestPostCompileTasks_FrdIDMismatch(t *testing.T) {
	dir := t.TempDir()
	seedPlan(t, dir, testApprovedPlan())
	stub := strings.Replace(validQueueJSON(), `"frd_id": "frd-001"`, `"frd_id": "frd-999"`, 1)
	e := testEngineWithStub(t, dir, func(ctx context.Context, model, prompt, host string) (string, error) {
		return stub, nil
	})
	h := NewHandler(NewStore(dir), e)

	w := doCompile(t, h)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}
	var got struct {
		Queue    TaskQueue `json:"queue"`
		Warnings []string  `json:"warnings"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if got.Queue.FRDID != "frd-001" {
		t.Fatalf("frd_id = %q, want plan's frd-001", got.Queue.FRDID)
	}
	if len(got.Warnings) == 0 {
		t.Fatal("want a warning about the frd_id correction")
	}
}
