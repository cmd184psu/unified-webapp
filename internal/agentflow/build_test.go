package agentflow

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"cmd184psu/unified-webapp/internal/agentflow/runner"
	"cmd184psu/unified-webapp/internal/platform/config"
	"cmd184psu/unified-webapp/internal/taskmaster"
)

// openHost opens a real taskmaster engine on a temp database.
func openHost(t *testing.T) *taskmaster.Engine {
	t.Helper()
	cfg := config.DefaultConfig().Taskmaster
	cfg.DBPath = filepath.Join(t.TempDir(), "taskmaster.db")
	cfg.StaticDir = t.TempDir()
	e, err := taskmaster.Open(cfg, taskmaster.OpenOptions{})
	if err != nil {
		t.Fatalf("taskmaster.Open: %v", err)
	}
	t.Cleanup(func() { _ = e.Close() })
	return e
}

func get(t *testing.T, h http.Handler, path string) (int, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	b, _ := io.ReadAll(rec.Body)
	return rec.Code, string(b)
}

// AC-ST1: a clean clone (no data/, no prompts/, cwd = an empty directory)
// boots, answers /api/health, registers a project over a temp git repository
// and takes an effort with an FRD.
func TestCleanCloneBoot(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	static := filepath.Join(dir, "web")
	if err := os.MkdirAll(static, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(static, "index.html"), []byte("AF INDEX"), 0o644); err != nil {
		t.Fatal(err)
	}
	fake := runner.NewFake()
	fake.SetVersion("9.9.9")

	h, err := buildWith(config.AgentflowConfig{StaticDir: static, DataDir: "./data/agentflow", SSEMaxSubscribers: 4}, openHost(t), fake)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer h.(io.Closer).Close()

	code, body := get(t, h, "/api/health")
	if code != http.StatusOK {
		t.Fatalf("GET /api/health = %d %s", code, body)
	}
	var hr struct {
		Status   string
		Opencode struct{ Status, Version string }
	}
	if err := json.Unmarshal([]byte(body), &hr); err != nil {
		t.Fatal(err)
	}
	if hr.Status != "ok" || hr.Opencode.Status != "ok" || hr.Opencode.Version != "9.9.9" {
		t.Errorf("health = %s", body)
	}
	if _, err := os.Stat(filepath.Join(dir, "data", "agentflow", "agentflow.db")); err != nil {
		t.Errorf("database not created under data_dir: %v", err)
	}
	if code, body := get(t, h, "/"); code != http.StatusOK || body != "AF INDEX" {
		t.Errorf("GET / = %d %q", code, body)
	}
	if code, _ := get(t, h, "/api/nope"); code != http.StatusNotFound {
		t.Errorf("unknown /api path = %d, want 404", code)
	}

	repo := t.TempDir()
	if out, err := exec.Command("git", "-C", repo, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	if code, body := send(t, h, "POST", "/api/projects", `{"slug":"demo","repo_path":`+strconv.Quote(repo)+`}`); code != http.StatusCreated {
		t.Fatalf("POST /api/projects = %d %s", code, body)
	}
	code, body = send(t, h, "POST", "/api/projects/demo/efforts", `{"title":"first"}`)
	var ef struct{ ID int64 }
	if err := json.Unmarshal([]byte(body), &ef); err != nil || code != http.StatusCreated || ef.ID == 0 {
		t.Fatalf("POST efforts = %d %s", code, body)
	}
	frd := `{"id":"F","title":"t","objective":"o","requirements":[{"id":"R1","description":"d"}]}`
	if code, body := send(t, h, "PUT", "/api/projects/demo/efforts/"+strconv.FormatInt(ef.ID, 10)+"/frd", frd); code != http.StatusOK {
		t.Fatalf("PUT frd = %d %s", code, body)
	}
}

// OC1: a missing opencode is a health status, never a Build failure.
func TestBuildToleratesMissingOpencode(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", t.TempDir())
	h, err := Build(config.AgentflowConfig{StaticDir: t.TempDir(), DataDir: filepath.Join(t.TempDir(), "af")}, openHost(t))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer h.(io.Closer).Close()
	code, body := get(t, h, "/api/health")
	if code != http.StatusOK {
		t.Fatalf("health = %d", code)
	}
	var hr struct {
		Status   string
		Opencode struct{ Status, Error string }
	}
	if err := json.Unmarshal([]byte(body), &hr); err != nil {
		t.Fatal(err)
	}
	if hr.Status != "degraded" || hr.Opencode.Status != "unavailable" || !strings.Contains(hr.Opencode.Error, "opencode") {
		t.Errorf("health = %s", body)
	}
}

// ST2: an unusable data_dir is an error that names the path.
func TestBuildDataDirError(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(blocker, "af")
	_, err := buildWith(config.AgentflowConfig{DataDir: want}, openHost(t), runner.NewFake())
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("Build error = %v, want it to name %s", err, want)
	}

	if os.Geteuid() != 0 {
		parent := t.TempDir()
		if err := os.Chmod(parent, 0o500); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(parent, 0o700) })
		want := filepath.Join(parent, "af")
		_, err := buildWith(config.AgentflowConfig{DataDir: want}, openHost(t), runner.NewFake())
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("read-only parent: error = %v, want it to name %s", err, want)
		}
	}
}

// The dispatcher closes the taskmaster engine before this handler. Close must
// cope with the host already gone, and with being called twice.
func TestCloseAfterHostClosed(t *testing.T) {
	host := openHost(t)
	h, err := buildWith(config.AgentflowConfig{StaticDir: t.TempDir(), DataDir: filepath.Join(t.TempDir(), "af")}, host, runner.NewFake())
	if err != nil {
		t.Fatal(err)
	}
	if err := host.Close(); err != nil {
		t.Fatal(err)
	}
	c := h.(io.Closer)
	if err := c.Close(); err != nil {
		t.Errorf("Close after host closed: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
}

// send issues a JSON request to the module and returns status and body.
func send(t *testing.T, h http.Handler, method, path, body string) (int, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	h.ServeHTTP(rec, req)
	b, _ := io.ReadAll(rec.Body)
	return rec.Code, string(b)
}

// AC-CF1: a limit changed over HTTP survives rebuilding the module on the same
// data_dir (CF1).
func TestCF1_SettingsSurviveRebuild(t *testing.T) {
	cfg := config.AgentflowConfig{StaticDir: t.TempDir(), DataDir: filepath.Join(t.TempDir(), "af")}
	h, err := buildWith(cfg, openHost(t), runner.NewFake())
	if err != nil {
		t.Fatal(err)
	}
	if code, body := send(t, h, "PUT", "/api/settings", `{"max_attempts":9}`); code != http.StatusOK {
		t.Fatalf("PUT = %d %s", code, body)
	}
	if err := h.(io.Closer).Close(); err != nil {
		t.Fatal(err)
	}

	h2, err := buildWith(cfg, openHost(t), runner.NewFake())
	if err != nil {
		t.Fatal(err)
	}
	defer h2.(io.Closer).Close()
	code, body := send(t, h2, "GET", "/api/settings", "")
	var s struct {
		MaxAttempts int `json:"max_attempts"`
	}
	if err := json.Unmarshal([]byte(body), &s); err != nil || code != http.StatusOK || s.MaxAttempts != 9 {
		t.Fatalf("GET after rebuild = %d %s", code, body)
	}
}
