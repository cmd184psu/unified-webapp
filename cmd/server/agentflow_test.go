package main

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cmd184psu/unified-webapp/internal/agentflow/store"
	"cmd184psu/unified-webapp/internal/platform/config"
)

func agentflowTestConfig(t *testing.T, routing map[string]string) *config.Config {
	t.Helper()
	root := t.TempDir()
	static := filepath.Join(root, "web")
	if err := os.MkdirAll(static, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(static, "index.html"), []byte("AF INDEX"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig()
	cfg.Routing = routing
	cfg.Agentflow = config.AgentflowConfig{StaticDir: static, DataDir: filepath.Join(root, "af")}
	cfg.Taskmaster.DBPath = filepath.Join(root, "taskmaster.db")
	cfg.Taskmaster.StaticDir = static
	return cfg
}

// AC-TM1: agentflow's backend lanes are lanes of the shared taskmaster engine,
// visible on its own /api/lanes; there is no second worker pool.
func TestAgentflowLanesOnTaskmaster(t *testing.T) {
	cfg := agentflowTestConfig(t, map[string]string{"tm.example": "taskmaster", "af.example": "agentflow"})
	// Seed a backend before boot; the engine registers its lane at start. The
	// HTTP route to add one at runtime arrives with D2.
	if err := os.MkdirAll(cfg.Agentflow.DataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(cfg.Agentflow.DataDir, "agentflow.db"))
	if err != nil {
		t.Fatal(err)
	}
	err = st.CreateBackend(t.Context(), "test", store.Backend{
		Name: "box", Host: "http://127.0.0.1:11434", APIType: "ollama", Slots: 1,
		Models: []store.BackendModel{{Model: "m1"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	dispatch := buildDispatcher(cfg, noAuthService(t))
	defer dispatch.Close()

	res := doDispatchHost(t, dispatch, http.MethodGet, "tm.example", "/api/lanes")
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/lanes = %d", res.StatusCode)
	}
	body, _ := io.ReadAll(res.Body)
	var lanes []struct {
		Name  string `json:"name"`
		Owner string `json:"owner"`
	}
	if err := json.Unmarshal(body, &lanes); err != nil {
		t.Fatalf("decode lanes: %v: %s", err, body)
	}
	found := map[string]bool{}
	for _, l := range lanes {
		if strings.HasPrefix(l.Name, "agentflow") && l.Owner == "agentflow" {
			found[l.Name] = true
		}
	}
	if !found["agentflow-box"] {
		t.Errorf("lane agentflow-box missing from /api/lanes: %s", body)
	}

	health := doDispatchHost(t, dispatch, http.MethodGet, "af.example", "/api/health")
	defer health.Body.Close()
	if health.StatusCode != http.StatusOK {
		t.Errorf("agentflow GET /api/health = %d, want 200", health.StatusCode)
	}
}

// ST2: an unusable data_dir takes only agentflow down: the module serves 503
// through unavailableHandler, with the path in the log and not the response.
func TestAgentflowDataDirError503(t *testing.T) {
	cfg := agentflowTestConfig(t, map[string]string{"af.example": "agentflow"})
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg.Agentflow.DataDir = filepath.Join(blocker, "af")

	dispatch := buildDispatcher(cfg, noAuthService(t))
	defer dispatch.Close()

	res := doDispatchHost(t, dispatch, http.MethodGet, "af.example", "/api/health")
	defer res.Body.Close()
	if res.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", res.StatusCode)
	}
	body, _ := io.ReadAll(res.Body)
	if !strings.Contains(string(body), "agentflow") || strings.Contains(string(body), blocker) {
		t.Errorf("503 body = %s", body)
	}
	if _, err := buildModule("agentflow", cfg, noAuthService(t), &moduleDeps{cfg: cfg, closers: &dispatch.closers}); err == nil || !strings.Contains(err.Error(), cfg.Agentflow.DataDir) {
		t.Errorf("buildModule error = %v, want it to name %s", err, cfg.Agentflow.DataDir)
	}
}
