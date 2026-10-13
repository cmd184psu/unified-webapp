package agentflow_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"cmd184psu/unified-webapp/internal/agentflow"
	"cmd184psu/unified-webapp/internal/platform/config"
)

const testPrompts = `# Planner Prompt Template

You are a software implementation planner.

## Input

{{FRD_JSON}}

## Your Task

Produce a JSON implementation plan.

## Rules

Output ONLY the JSON.
`

func setupPromptDir(t *testing.T) string {
	promptDir := t.TempDir()
	prompts := []string{"planner.md", "worker.md", "reviewer.md", "plan-reviewer.md", "task-compiler.md"}
	for _, p := range prompts {
		if err := os.WriteFile(filepath.Join(promptDir, p), []byte(testPrompts), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return promptDir
}

func TestBuild_Success(t *testing.T) {
	staticDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(staticDir, "index.html"), []byte("test"), 0o644); err != nil {
		t.Fatal(err)
	}

	promptDir := setupPromptDir(t)
	projectPath := filepath.Join(t.TempDir(), "agentflow")

	cfg := config.AgentflowConfig{
		StaticDir:         staticDir,
		ProjectPath:       projectPath,
		SSEMaxSubscribers: 64,
		PromptDir:         promptDir,
	}

	h, err := agentflow.Build(cfg)
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	if h == nil {
		t.Fatal("Build returned nil handler")
	}

	// Test that the static file is served
	srv := httptest.NewServer(h)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

func TestBuild_Error_UnwritableProjectPath(t *testing.T) {
	promptDir := setupPromptDir(t)
	// A regular file used as the parent "directory" makes MkdirAll fail
	blocker := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := config.AgentflowConfig{
		StaticDir:   t.TempDir(),
		ProjectPath: filepath.Join(blocker, "sub", "agentflow"),
		PromptDir:   promptDir,
	}

	h, err := agentflow.Build(cfg)
	if err == nil {
		t.Fatal("expected error for unwritable project path, got nil")
	}
	if h != nil {
		t.Fatalf("expected nil handler on error, got %v", h)
	}
}

func TestBuild_DefaultsProjectPath(t *testing.T) {
	promptDir := setupPromptDir(t)
	staticDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(staticDir, "index.html"), []byte("test"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := config.AgentflowConfig{
		StaticDir: staticDir,
		PromptDir: promptDir,
		// ProjectPath left empty - should default
	}

	h, err := agentflow.Build(cfg)
	if err != nil {
		t.Fatalf("Build failed with default ProjectPath: %v", err)
	}
	if h == nil {
		t.Fatal("Build returned nil handler")
	}
}
func TestBuildStartsNoWorker(t *testing.T) {
	promptDir := setupPromptDir(t)
	projectPath := filepath.Join(t.TempDir(), "agentflow")
	// A ready task that the old worker would have picked up within its 5 s tick.
	qdir := filepath.Join(projectPath, ".agentflow")
	if err := os.MkdirAll(qdir, 0o755); err != nil {
		t.Fatal(err)
	}
	q := `{"tasks":[{"id":"t1","title":"x","status":"ready","verify":"touch ` + filepath.Join(projectPath, "ran") + `"}]}`
	if err := os.WriteFile(filepath.Join(qdir, "queue.json"), []byte(q), 0o644); err != nil {
		t.Fatal(err)
	}

	before := runtime.NumGoroutine()
	if _, err := agentflow.Build(config.AgentflowConfig{
		StaticDir:   t.TempDir(),
		ProjectPath: projectPath,
		PromptDir:   promptDir,
	}); err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	time.Sleep(2 * time.Second)

	if _, err := os.Stat(filepath.Join(qdir, "lock")); !os.IsNotExist(err) {
		t.Fatalf("lock file exists or unexpected stat error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(projectPath, "ran")); !os.IsNotExist(err) {
		t.Fatalf("a task ran: %v", err)
	}
	if after := runtime.NumGoroutine(); after > before+2 {
		t.Fatalf("goroutines grew from %d to %d; a worker is running", before, after)
	}
}
