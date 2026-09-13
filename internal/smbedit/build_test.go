package smbedit

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"cmd184psu/unified-webapp/internal/platform/config"

	"go.uber.org/goleak"
)

// buildTestConfig returns a config whose static_dir holds an index.html and
// whose data_dir does not exist yet.
func buildTestConfig(t *testing.T) config.SmbeditConfig {
	t.Helper()
	dir := t.TempDir()
	staticDir := filepath.Join(dir, "static")
	if err := os.MkdirAll(staticDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staticDir, "index.html"), []byte("<html>ok</html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	return config.SmbeditConfig{
		StaticDir:  staticDir,
		DataDir:    filepath.Join(dir, "data"),
		PickerRoot: dir,
	}
}

func TestBuild_Success(t *testing.T) {
	defer goleak.VerifyNone(t)
	cfg := buildTestConfig(t)

	h, err := Build(cfg)
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}
	if h == nil {
		t.Fatal("Build() returned nil handler")
	}

	// data_dir was created with a default state.json.
	if _, err := os.Stat(filepath.Join(cfg.DataDir, "state.json")); err != nil {
		t.Errorf("expected default state.json: %v", err)
	}

	// The handler actually serves.
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/version", nil))
	if rr.Code != http.StatusOK {
		t.Errorf("GET /api/version through built handler: %d", rr.Code)
	}
}

func TestBuild_LoadsExistingState(t *testing.T) {
	defer goleak.VerifyNone(t)
	cfg := buildTestConfig(t)
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	existing := `{"smb_conf_path":"/custom/smb.conf","theme":"light","globals":[],"shares":[]}`
	if err := os.WriteFile(filepath.Join(cfg.DataDir, "state.json"), []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}

	h, err := Build(cfg)
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	var st State
	if err := json.NewDecoder(rr.Body).Decode(&st); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if st.SmbConfPath != "/custom/smb.conf" || st.Theme != "light" {
		t.Errorf("existing state.json not loaded: %+v", st)
	}
}

func TestBuild_FailureModes(t *testing.T) {
	defer goleak.VerifyNone(t)
	valid := buildTestConfig(t)
	notADir := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(notADir, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		cfg  config.SmbeditConfig
	}{
		{"empty static_dir", config.SmbeditConfig{StaticDir: "", DataDir: valid.DataDir}},
		{"missing static_dir", config.SmbeditConfig{StaticDir: filepath.Join(t.TempDir(), "nope"), DataDir: valid.DataDir}},
		{"static_dir not a directory", config.SmbeditConfig{StaticDir: notADir, DataDir: valid.DataDir}},
		{"empty data_dir", config.SmbeditConfig{StaticDir: valid.StaticDir, DataDir: ""}},
	}
	for _, tc := range cases {
		h, err := Build(tc.cfg)
		if err == nil {
			t.Errorf("%s: expected error, got nil", tc.name)
		}
		if h != nil {
			t.Errorf("%s: expected nil handler on error", tc.name)
		}
	}
}

func TestBuild_MalformedStateIsError(t *testing.T) {
	defer goleak.VerifyNone(t)
	cfg := buildTestConfig(t)
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg.DataDir, "state.json"), []byte("{bad json"), 0o600); err != nil {
		t.Fatal(err)
	}

	h, err := Build(cfg)
	if err == nil {
		t.Error("expected error for malformed state.json (never silently overwritten)")
	}
	if h != nil {
		t.Error("expected nil handler on error")
	}
	// The malformed file is left in place for the operator to inspect.
	data, readErr := os.ReadFile(filepath.Join(cfg.DataDir, "state.json"))
	if readErr != nil || string(data) != "{bad json" {
		t.Errorf("malformed state.json was modified: %q, %v", data, readErr)
	}
}
