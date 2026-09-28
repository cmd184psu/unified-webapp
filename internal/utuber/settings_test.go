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

	"github.com/stretchr/testify/require"
)

func TestPythonBinFallsBackToConfigDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	s := newSettingsStore(path, "python3.11", "")
	if got := s.PythonBin(); got != "python3.11" {
		t.Fatalf("PythonBin() = %q, want config default python3.11", got)
	}
}

func TestPythonBinFallsBackToPackageDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	s := newSettingsStore(path, "", "")
	if got := s.PythonBin(); got != "python3.12" {
		t.Fatalf("PythonBin() = %q, want python3.12", got)
	}
}

func TestSetPythonBinPersistsAcrossRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	s := newSettingsStore(path, "python3.12", "")
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
	restarted := newSettingsStore(path, "python3.12", "")
	if got := restarted.PythonBin(); got != "python3" {
		t.Fatalf("PythonBin() after restart = %q, want python3", got)
	}
}

func TestSetPythonBinRejectsShellMetacharacters(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	s := newSettingsStore(path, "python3.12", "")
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
	s := newSettingsStore(path, "python3.12", "")
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
	restarted := newSettingsStore(path, "python3.12", "")
	if got := restarted.PythonBin(); got != "python3.12" {
		t.Fatalf("PythonBin() after clear+restart = %q, want python3.12", got)
	}
}

func TestBlankSaveWithNoFileSucceeds(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	s := newSettingsStore(path, "python3.12", "")
	if err := s.SetPythonBin(""); err != nil {
		t.Fatalf("blank save with no file: %v", err)
	}
}

func TestCorruptSettingsFileIsIgnored(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatalf("write corrupt file: %v", err)
	}
	s := newSettingsStore(path, "python3.11", "")
	if got := s.PythonBin(); got != "python3.11" {
		t.Fatalf("PythonBin() with corrupt file = %q, want config default", got)
	}
}

// ── cookies (D5 Fix 2) ──────────────────────────────────────────────────────

const validNetscapeCookies = "# Netscape HTTP Cookie File\n" +
	".youtube.com\tTRUE\t/\tTRUE\t1999999999\tCONSENT\tYES+1\n"

func TestCookiesNotConfiguredInitially(t *testing.T) {
	dir := t.TempDir()
	s := newSettingsStore(filepath.Join(dir, "settings.json"), "", filepath.Join(dir, "cookies.txt"))
	configured, mtime := s.cookiesInfo()
	if configured || mtime != nil {
		t.Fatalf("cookiesInfo() = (%v, %v), want (false, nil) before any save", configured, mtime)
	}
}

func TestSetCookiesTextWritesFileMode0600(t *testing.T) {
	dir := t.TempDir()
	cookiesPath := filepath.Join(dir, "cookies.txt")
	s := newSettingsStore(filepath.Join(dir, "settings.json"), "", cookiesPath)

	if err := s.SetCookiesText(validNetscapeCookies); err != nil {
		t.Fatalf("SetCookiesText: %v", err)
	}
	fi, err := os.Stat(cookiesPath)
	if err != nil {
		t.Fatalf("stat cookie file: %v", err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("cookie file mode = %v, want 0600", fi.Mode().Perm())
	}
	configured, mtime := s.cookiesInfo()
	if !configured || mtime == nil {
		t.Fatalf("cookiesInfo() = (%v, %v), want (true, non-nil) after a save", configured, mtime)
	}
}

func TestSetCookiesTextRejectsUnparseableTextAndLeavesFileUntouched(t *testing.T) {
	dir := t.TempDir()
	cookiesPath := filepath.Join(dir, "cookies.txt")
	s := newSettingsStore(filepath.Join(dir, "settings.json"), "", cookiesPath)

	if err := s.SetCookiesText(validNetscapeCookies); err != nil {
		t.Fatalf("SetCookiesText: %v", err)
	}
	before, err := os.ReadFile(cookiesPath)
	if err != nil {
		t.Fatal(err)
	}

	if err := s.SetCookiesText("this is not a cookie file"); err == nil {
		t.Fatal("SetCookiesText accepted unparseable text, want rejection")
	}

	after, err := os.ReadFile(cookiesPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Errorf("existing cookie file was modified by a rejected save: before=%q after=%q", before, after)
	}
}

func TestSetCookiesTextBlankClearsFile(t *testing.T) {
	dir := t.TempDir()
	cookiesPath := filepath.Join(dir, "cookies.txt")
	s := newSettingsStore(filepath.Join(dir, "settings.json"), "", cookiesPath)

	if err := s.SetCookiesText(validNetscapeCookies); err != nil {
		t.Fatalf("SetCookiesText: %v", err)
	}
	if err := s.SetCookiesText(""); err != nil {
		t.Fatalf("clearing SetCookiesText: %v", err)
	}
	if _, err := os.Stat(cookiesPath); !os.IsNotExist(err) {
		t.Fatalf("cookie file should have been removed, stat err = %v", err)
	}
	configured, mtime := s.cookiesInfo()
	if configured || mtime != nil {
		t.Fatalf("cookiesInfo() after clear = (%v, %v), want (false, nil)", configured, mtime)
	}
}

// TestHandleSettingsHTTP exercises the /settings.json handler over a real
// engine-backed module (6d harness): the 9-key GET shape (D8's 7 plus D5's
// cookies_configured/cookies_updated_at), a python_bin POST, rejection of a
// shell-metacharacter python_bin and malformed JSON, and the 405 + Allow
// header on an unsupported method.
func TestHandleSettingsHTTP(t *testing.T) {
	h, _ := buildTestModule(t)

	get := req(h, http.MethodGet, "/settings.json", nil)
	require.Equal(t, http.StatusOK, get.Code)
	var m map[string]any
	require.NoError(t, json.Unmarshal(get.Body.Bytes(), &m))
	require.Len(t, m, 9, "GET returns exactly 9 keys")
	require.Equal(t, "python3.12", m["python_bin"])
	require.Equal(t, false, m["cookies_configured"])
	require.Nil(t, m["cookies_updated_at"])

	post := req(h, http.MethodPost, "/settings.json", strings.NewReader(`{"python_bin":"python3"}`))
	require.Equal(t, http.StatusOK, post.Code, post.Body.String())
	require.Contains(t, post.Body.String(), `"python_bin":"python3"`)

	bad := req(h, http.MethodPost, "/settings.json", strings.NewReader(`{"python_bin":"rm -rf /"}`))
	require.Equal(t, http.StatusBadRequest, bad.Code)

	malformed := req(h, http.MethodPost, "/settings.json", strings.NewReader(`{not json`))
	require.Equal(t, http.StatusBadRequest, malformed.Code)

	put := req(h, http.MethodPut, "/settings.json", nil)
	require.Equal(t, http.StatusMethodNotAllowed, put.Code)
	require.Equal(t, "GET, POST", put.Header().Get("Allow"))
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
	s := newSettingsStore(path, "python3.12", "")
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
	s := newSettingsStore(path, "python3.12", "")
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
