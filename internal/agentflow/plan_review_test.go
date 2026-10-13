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

	"cmd184psu/unified-webapp/internal/platform/config"
)

func testFRD() *FRD {
	return &FRD{
		ID:        "frd-001",
		Title:     "Test FRD",
		Status:    "approved",
		Objective: "Build the thing.",
		Requirements: []FRDRequirement{
			{ID: "R1", Description: "It must work.", Priority: "must"},
		},
		AcceptanceCriteria: []FRDAC{
			{ID: "AC1", Criterion: "Tests pass."},
		},
	}
}

func testPlan() *Plan {
	return &Plan{
		FRDID:    "frd-001",
		Status:   "draft",
		Approach: "Write code, then tests.",
		Phases: []PlanPhase{
			{ID: "P1", Name: "Build", TasksDescription: "Build the thing in internal/thing."},
		},
	}
}

func testEngineWithStub(t *testing.T, dir string, stub func(ctx context.Context, model, prompt, host string) (string, error)) *Engine {
	t.Helper()
	promptDir := t.TempDir()
	tmpl := "FRD:\n{{FRD_JSON}}\nPLAN:\n{{PLAN_JSON}}"
	for _, name := range []string{"planner.md", "worker.md", "reviewer.md", "plan-reviewer.md", "task-compiler.md"} {
		if err := os.WriteFile(filepath.Join(promptDir, name), []byte(tmpl), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	prompts, err := loadPrompts(promptDir)
	if err != nil {
		t.Fatal(err)
	}
	return &Engine{
		store:   NewStore(dir),
		prompts: prompts,
		profiles: []config.ProfileConfig{
			{Name: "m5", OllamaHost: "http://localhost:11434", WorkerModel: "w", PlannerModel: "p", ReviewModel: "r", TimeoutSecs: 30},
		},
		defaultProf: "m5",
		promptDir:   promptDir,
		timeoutSecs: 30,
		modelRun:    stub,
	}
}

func seedFRD(t *testing.T, dir string) {
	t.Helper()
	s := NewStore(dir)
	if err := os.MkdirAll(filepath.Join(dir, ".agentflow"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteFRD(testFRD()); err != nil {
		t.Fatal(err)
	}
}

func doPlanReview(t *testing.T, h *Handler, plan *Plan) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"plan": plan})
	req := httptest.NewRequest("POST", "/plan/review", strings.NewReader(string(body)))
	w := httptest.NewRecorder()
	mux := http.NewServeMux()
	h.Register(mux)
	mux.ServeHTTP(w, req)
	return w
}

func TestPostPlanReview_PassVerdict(t *testing.T) {
	dir := t.TempDir()
	seedFRD(t, dir)
	var gotPrompt, gotModel string
	e := testEngineWithStub(t, dir, func(ctx context.Context, model, prompt, host string) (string, error) {
		gotModel, gotPrompt = model, prompt
		return `{"verdict":"pass","summary":"all covered","issues":[]}`, nil
	})
	h := NewHandler(NewStore(dir), e)

	w := doPlanReview(t, h, testPlan())

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}
	var got PlanReview
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if got.Verdict != "pass" {
		t.Fatalf("verdict = %q, want pass", got.Verdict)
	}
	if gotModel != "r" {
		t.Fatalf("model = %q, want review model r", gotModel)
	}
	if !strings.Contains(gotPrompt, "frd-001") {
		t.Fatalf("prompt missing FRD id; prompt = %q", gotPrompt)
	}
	if !strings.Contains(gotPrompt, "Write code, then tests.") {
		t.Fatalf("prompt missing plan approach; prompt = %q", gotPrompt)
	}
}

func TestPostPlanReview_RepairVerdict(t *testing.T) {
	dir := t.TempDir()
	seedFRD(t, dir)
	e := testEngineWithStub(t, dir, func(ctx context.Context, model, prompt, host string) (string, error) {
		return `{"verdict":"repair","summary":"missing R1","issues":[{"description":"R1 uncovered","severity":"blocker"}],"repair_prompt":"cover R1 in P1"}`, nil
	})
	h := NewHandler(NewStore(dir), e)

	w := doPlanReview(t, h, testPlan())

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}
	var got PlanReview
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if got.Verdict != "repair" {
		t.Fatalf("verdict = %q, want repair", got.Verdict)
	}
	if len(got.Issues) != 1 || got.Issues[0].Severity != "blocker" {
		t.Fatalf("issues = %+v, want one blocker", got.Issues)
	}
	if got.RepairPrompt != "cover R1 in P1" {
		t.Fatalf("repair_prompt = %q, want the reviewer text", got.RepairPrompt)
	}
}

func TestPostPlanReview_FencedOutput(t *testing.T) {
	dir := t.TempDir()
	seedFRD(t, dir)
	e := testEngineWithStub(t, dir, func(ctx context.Context, model, prompt, host string) (string, error) {
		return "Here is the review:\n```json\n{\"verdict\":\"fail\",\"summary\":\"cannot satisfy\",\"issues\":[]}\n```", nil
	})
	h := NewHandler(NewStore(dir), e)

	w := doPlanReview(t, h, testPlan())

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}
	var got PlanReview
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if got.Verdict != "fail" {
		t.Fatalf("verdict = %q, want fail", got.Verdict)
	}
}

func TestPostPlanReview_InvalidVerdict(t *testing.T) {
	dir := t.TempDir()
	seedFRD(t, dir)
	e := testEngineWithStub(t, dir, func(ctx context.Context, model, prompt, host string) (string, error) {
		return `{"verdict":"maybe","summary":"unsure","issues":[]}`, nil
	})
	h := NewHandler(NewStore(dir), e)

	w := doPlanReview(t, h, testPlan())

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusInternalServerError, w.Body.String())
	}
}

func TestPostPlanReview_MissingFRD(t *testing.T) {
	dir := t.TempDir() // no frd.json
	e := testEngineWithStub(t, dir, func(ctx context.Context, model, prompt, host string) (string, error) {
		t.Fatal("model must not run without an FRD")
		return "", nil
	})
	h := NewHandler(NewStore(dir), e)

	w := doPlanReview(t, h, testPlan())

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusNotFound, w.Body.String())
	}
}

func TestPostPlanReview_MissingPlan(t *testing.T) {
	dir := t.TempDir()
	seedFRD(t, dir)
	e := testEngineWithStub(t, dir, func(ctx context.Context, model, prompt, host string) (string, error) {
		t.Fatal("model must not run without a plan")
		return "", nil
	})
	h := NewHandler(NewStore(dir), e)

	req := httptest.NewRequest("POST", "/plan/review", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	mux := http.NewServeMux()
	h.Register(mux)
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
}
