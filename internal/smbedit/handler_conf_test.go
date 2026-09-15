package smbedit

// Import/preview/save-and-restart/version tests, ported from smbed's
// server_test.go, plus the save-and-restart in-band-failure test (§9.4).

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.uber.org/goleak"
)

func TestImportConf(t *testing.T) {
	defer goleak.VerifyNone(t)
	srv := newTestServer(t)

	confPath := filepath.Join(t.TempDir(), "smb.conf")
	content := "[global]\n   workgroup = IMPORTED\n\n[media]\n   path = /opt/media\n   valid users = mediauser\n   read only = no\n"
	if err := os.WriteFile(confPath, []byte(content), 0o644); err != nil {
		t.Fatalf("writing test smb.conf: %v", err)
	}

	rr := doJSON(t, srv, http.MethodPost, "/api/import", map[string]string{"path": confPath})
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body)
	}

	var got struct {
		Globals    []GlobalEntry `json:"globals"`
		Shares     []Share       `json:"shares"`
		ShareOwner string        `json:"share_owner"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Globals) != 1 || got.Globals[0].Value != "IMPORTED" {
		t.Errorf("unexpected globals: %+v", got.Globals)
	}
	if len(got.Shares) != 1 || got.Shares[0].Name != "media" || !got.Shares[0].Writable {
		t.Errorf("unexpected shares: %+v", got.Shares)
	}
	if got.ShareOwner != "mediauser" {
		t.Errorf("expected share owner 'mediauser', got %q", got.ShareOwner)
	}

	// FR-5: import stages, it does not persist.
	if shares := srv.store.snapshot().Shares; len(shares) != 0 {
		t.Errorf("import must not touch the saved state, got %+v", shares)
	}
}

func TestImportConf_DisablesMissingSharePath(t *testing.T) {
	defer goleak.VerifyNone(t)
	srv := newTestServer(t)

	confPath := filepath.Join(t.TempDir(), "smb.conf")
	content := "[global]\n   workgroup = IMPORTED\n\n[gone]\n   path = /does/not/exist/anywhere\n   valid users = mediauser\n   read only = no\n"
	if err := os.WriteFile(confPath, []byte(content), 0o644); err != nil {
		t.Fatalf("writing test smb.conf: %v", err)
	}

	rr := doJSON(t, srv, http.MethodPost, "/api/import", map[string]string{"path": confPath})
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body)
	}

	var got struct {
		Shares []Share `json:"shares"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Shares) != 1 || got.Shares[0].Enabled {
		t.Errorf("expected imported share with missing path to be disabled: %+v", got.Shares)
	}
}

func TestImportConf_MissingFile(t *testing.T) {
	defer goleak.VerifyNone(t)
	srv := newTestServer(t)
	rr := doJSON(t, srv, http.MethodPost, "/api/import", map[string]string{"path": "/does/not/exist.conf"})
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 for missing file, got %d", rr.Code)
	}
}

func TestPreview(t *testing.T) {
	defer goleak.VerifyNone(t)
	srv := newTestServer(t)
	rr := doJSON(t, srv, http.MethodGet, "/api/preview", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	body := rr.Body.String()
	if body == "" {
		t.Error("preview should return non-empty content")
	}
	if !strings.Contains(body, "[global]") {
		t.Errorf("preview should render a [global] section, got: %q", body)
	}
	if rr.Header().Get("Content-Type") != "text/plain; charset=utf-8" {
		t.Errorf("unexpected content type: %q", rr.Header().Get("Content-Type"))
	}
}

// POST /api/preview renders the posted draft, so an unsaved share the editor
// is holding appears in the preview even though it was never written to
// state.json.
func TestPreviewDraft_RendersUnsavedShare(t *testing.T) {
	defer goleak.VerifyNone(t)
	srv := newTestServer(t)

	draft := map[string]any{
		"share_owner": "nobody",
		"globals":     []map[string]string{{"key": "workgroup", "value": "DRAFT"}},
		"shares": []map[string]any{
			{"name": "media", "path": t.TempDir(), "enabled": true, "writable": true, "browseable": true},
		},
	}
	rr := doJSON(t, srv, http.MethodPost, "/api/preview", draft)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "[media]") {
		t.Errorf("draft preview should render the unsaved [media] share, got:\n%s", body)
	}
	if !strings.Contains(body, "workgroup = DRAFT") {
		t.Errorf("draft preview should render the posted globals, got:\n%s", body)
	}
	// The draft must not have been persisted.
	if shares := srv.store.snapshot().Shares; len(shares) != 0 {
		t.Errorf("preview must not touch saved state, got %+v", shares)
	}
}

// A draft share whose path is missing is auto-disabled before rendering, so
// the preview matches what a save would actually write (a dangling share is
// never exported).
func TestPreviewDraft_AutoDisablesMissingPath(t *testing.T) {
	defer goleak.VerifyNone(t)
	srv := newTestServer(t)

	draft := map[string]any{
		"share_owner": "nobody",
		"shares": []map[string]any{
			{"name": "gone", "path": "/does/not/exist/anywhere", "enabled": true},
		},
	}
	rr := doJSON(t, srv, http.MethodPost, "/api/preview", draft)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body)
	}
	if strings.Contains(rr.Body.String(), "[gone]") {
		t.Errorf("draft preview should skip a share with a missing path, got:\n%s", rr.Body.String())
	}
}

func TestVersion(t *testing.T) {
	defer goleak.VerifyNone(t)
	srv := newTestServer(t)
	rr := doJSON(t, srv, http.MethodGet, "/api/version", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var v map[string]string
	json.NewDecoder(rr.Body).Decode(&v)
	if v["version"] != "test" {
		t.Errorf("expected version 'test', got %q", v["version"])
	}
}

func TestPutConfig_BadJSON(t *testing.T) {
	defer goleak.VerifyNone(t)
	srv := newTestServer(t)
	req := httptest.NewRequest(http.MethodPut, "/api/config", strings.NewReader("{bad"))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for bad JSON, got %d", rr.Code)
	}
}

// TestSaveAndRestart_FailureReportedInBand swaps runCommand so both the sudo
// write fallback (not needed — the conf path is writable) and every restart
// attempt fail, and asserts the failure comes back in-band: HTTP 200,
// restart.success=false, ops-log entries, no panic (§9.4).
func TestSaveAndRestart_FailureReportedInBand(t *testing.T) {
	defer goleak.VerifyNone(t)
	srv := newTestServer(t)
	swapRunCommand(t, func(name string, args ...string) (string, error) {
		return "unit smbd.service not found", errors.New("exit status 1")
	})

	rr := doJSON(t, srv, http.MethodPost, "/api/save-and-restart", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("restart failure must be in-band, got HTTP %d: %s", rr.Code, rr.Body)
	}

	var got saveRestartResponse
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !got.Written {
		t.Error("expected written=true (conf path is writable in this test)")
	}
	if got.Restart.Success {
		t.Error("expected restart.success=false when every restart command fails")
	}

	// The conf file was actually written.
	confPath := srv.store.snapshot().SmbConfPath
	if data, err := os.ReadFile(confPath); err != nil || !strings.Contains(string(data), "[global]") {
		t.Errorf("expected rendered smb.conf at %s, err=%v", confPath, err)
	}

	// Ops log carries the story: the request and the failed restart.
	var sawRequest, sawFailure bool
	for _, e := range srv.ops.snapshot() {
		if strings.Contains(e.Message, "save & restart requested") {
			sawRequest = true
		}
		if strings.Contains(e.Message, "restart") && strings.Contains(e.Message, "failed") {
			sawFailure = true
		}
	}
	if !sawRequest || !sawFailure {
		t.Errorf("ops log missing request/failure entries (request=%v failure=%v)", sawRequest, sawFailure)
	}
}
