package utuber

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestPythonBinFallsBackToConfigDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	s := newSettingsStore(path, "python3.11")
	if got := s.PythonBin(); got != "python3.11" {
		t.Fatalf("PythonBin() = %q, want config default python3.11", got)
	}
}

func TestPythonBinFallsBackToPackageDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	s := newSettingsStore(path, "")
	if got := s.PythonBin(); got != "python3.12" {
		t.Fatalf("PythonBin() = %q, want python3.12", got)
	}
}

func TestSetPythonBinPersistsAcrossRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	s := newSettingsStore(path, "python3.12")
	if err := s.SetPythonBin("python3"); err != nil {
		t.Fatalf("SetPythonBin: %v", err)
	}
	if got := s.PythonBin(); got != "python3" {
		t.Fatalf("PythonBin() after save = %q, want python3", got)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read settings file: %v", err)
	}
	if !strings.Contains(string(data), `"python_bin":"python3"`) {
		t.Errorf("settings file does not hold the saved value: %s", data)
	}
	// AC-6: a second store on the same path — the restart — reads it back.
	restarted := newSettingsStore(path, "python3.12")
	if got := restarted.PythonBin(); got != "python3" {
		t.Fatalf("PythonBin() after restart = %q, want python3", got)
	}
}

func TestSetPythonBinRejectsShellMetacharacters(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	s := newSettingsStore(path, "python3.12")
	for _, bad := range []string{"rm -rf /", "py;ls", "$(id)", "py thon", "py|x"} {
		if err := s.SetPythonBin(bad); err == nil {
			t.Errorf("SetPythonBin(%q) accepted, want rejection", bad)
		}
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("a rejected save touched the settings file (stat err = %v)", err)
	}
	if got := s.PythonBin(); got != "python3.12" {
		t.Fatalf("PythonBin() after rejected saves = %q, want python3.12", got)
	}
}

// A blank save clears the override by REMOVING the file (appendix item 7):
// a persisted {"python_bin":""} would pass the restart test below too, but
// would leave settings.json in the download dir's newest-file scan.
func TestBlankSaveClearsOverrideAndPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	s := newSettingsStore(path, "python3.12")
	if err := s.SetPythonBin("python3"); err != nil {
		t.Fatalf("SetPythonBin: %v", err)
	}
	if err := s.SetPythonBin(""); err != nil {
		t.Fatalf("blank SetPythonBin: %v", err)
	}
	if got := s.PythonBin(); got != "python3.12" {
		t.Fatalf("PythonBin() after clear = %q, want config default python3.12", got)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("blank save must remove the file, stat err = %v", err)
	}
	// The clear survives a restart: an in-memory-only clear would resurrect
	// the old override here.
	restarted := newSettingsStore(path, "python3.12")
	if got := restarted.PythonBin(); got != "python3.12" {
		t.Fatalf("PythonBin() after clear+restart = %q, want python3.12", got)
	}
}

func TestBlankSaveWithNoFileSucceeds(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	s := newSettingsStore(path, "python3.12")
	if err := s.SetPythonBin(""); err != nil {
		t.Fatalf("blank save with no file: %v", err)
	}
}

func TestCorruptSettingsFileIsIgnored(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatalf("write corrupt file: %v", err)
	}
	s := newSettingsStore(path, "python3.11")
	if got := s.PythonBin(); got != "python3.11" {
		t.Fatalf("PythonBin() with corrupt file = %q, want config default", got)
	}
}

func TestHandleSettingsHTTP(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	h := handleSettings(newSettingsStore(path, "python3.12"))

	get := httptest.NewRecorder()
	h(get, httptest.NewRequest(http.MethodGet, "/settings.json", nil))
	if get.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want 200", get.Code)
	}
	var f settingsFile
	if err := json.Unmarshal(get.Body.Bytes(), &f); err != nil {
		t.Fatalf("GET body decode: %v", err)
	}
	if f.PythonBin != "python3.12" {
		t.Errorf("GET python_bin = %q, want python3.12", f.PythonBin)
	}

	post := httptest.NewRecorder()
	h(post, httptest.NewRequest(http.MethodPost, "/settings.json",
		strings.NewReader(`{"python_bin":"python3"}`)))
	if post.Code != http.StatusOK {
		t.Fatalf("POST valid status = %d, want 200 (body %s)", post.Code, post.Body)
	}
	if !strings.Contains(post.Body.String(), `"python_bin":"python3"`) {
		t.Errorf("POST valid does not echo the effective value: %s", post.Body)
	}

	bad := httptest.NewRecorder()
	h(bad, httptest.NewRequest(http.MethodPost, "/settings.json",
		strings.NewReader(`{"python_bin":"rm -rf /"}`)))
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("POST invalid status = %d, want 400", bad.Code)
	}

	malformed := httptest.NewRecorder()
	h(malformed, httptest.NewRequest(http.MethodPost, "/settings.json",
		strings.NewReader(`{not json`)))
	if malformed.Code != http.StatusBadRequest {
		t.Fatalf("POST malformed JSON status = %d, want 400", malformed.Code)
	}

	put := httptest.NewRecorder()
	h(put, httptest.NewRequest(http.MethodPut, "/settings.json", nil))
	if put.Code != http.StatusMethodNotAllowed {
		t.Fatalf("PUT status = %d, want 405", put.Code)
	}
	if allow := put.Header().Get("Allow"); allow != "GET, POST" {
		t.Errorf("PUT Allow header = %q, want \"GET, POST\"", allow)
	}
}

// capturingExec records the command it was asked to run. (fakeExec is taken
// by the ported executor in handler_test.go — same package.)
type capturingExec struct {
	mu   sync.Mutex
	name string
	args []string
	err  error
}

func (c *capturingExec) Run(_ context.Context, name string, args []string, onLine func(string)) error {
	c.mu.Lock()
	c.name = name
	c.args = append([]string(nil), args...)
	c.mu.Unlock()
	onLine("Collecting yt-dlp")
	return c.err
}

func TestYtdlpUpdateUsesResolvedInterpreter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	s := newSettingsStore(path, "python3.12")
	if err := s.SetPythonBin("python3"); err != nil {
		t.Fatalf("SetPythonBin: %v", err)
	}
	exec := &capturingExec{}
	rec := httptest.NewRecorder()
	handleYtdlpUpdate(exec, s)(rec, httptest.NewRequest(http.MethodGet, "/ytdlp-update", nil))

	if exec.name != "python3" {
		t.Errorf("update ran %q, want the saved override python3", exec.name)
	}
	want := []string{"-m", "pip", "install", "-U", "yt-dlp"}
	if len(exec.args) != len(want) {
		t.Fatalf("args = %v, want %v", exec.args, want)
	}
	for i := range want {
		if exec.args[i] != want[i] {
			t.Fatalf("args = %v, want %v", exec.args, want)
		}
	}
	if !strings.Contains(rec.Body.String(), "data: __done__") {
		t.Errorf("success stream is missing the __done__ sentinel: %s", rec.Body)
	}
}

func TestYtdlpUpdateStreamsError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	s := newSettingsStore(path, "python3.12")
	exec := &capturingExec{err: errors.New("pip exploded")}
	rec := httptest.NewRecorder()
	handleYtdlpUpdate(exec, s)(rec, httptest.NewRequest(http.MethodGet, "/ytdlp-update", nil))

	if exec.name != "python3.12" {
		t.Errorf("update ran %q, want the config default python3.12", exec.name)
	}
	if !strings.Contains(rec.Body.String(), "data: ERROR: pip exploded") {
		t.Errorf("failure stream is missing the ERROR line: %s", rec.Body)
	}
	if strings.Contains(rec.Body.String(), "__done__") {
		t.Errorf("failure stream must not send __done__: %s", rec.Body)
	}
}
