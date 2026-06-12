package obsidianoid_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cmd184psu/unified-webapp/internal/obsidianoid"
	"cmd184psu/unified-webapp/internal/platform/config"
)

// harness wires up a Handler with two temp vaults.
type harness struct {
	vault0 string
	vault1 string
	state  *obsidianoid.StateStore
	h      *obsidianoid.Handler
	mux    *http.ServeMux
	server *httptest.Server
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	v0 := t.TempDir()
	v1 := t.TempDir()
	_ = os.WriteFile(filepath.Join(v0, "Hello.md"), []byte("# Hello"), 0o644)

	dataDir := t.TempDir()
	cfg := config.ObsidianoidConfig{
		StaticDir: t.TempDir(),
		DataDir:   dataDir,
		Vaults: []config.ObsidianoidVault{
			{Path: v0, Name: "Vault0", Theme: "dark"},
			{Path: v1, Name: "Vault1", Theme: "forest"},
		},
		ThreadsFolder:    "Threads",
		ThreadCount:      4,
		AutoSaveDisabled: false,
	}

	state, err := obsidianoid.NewStateStore(dataDir, 4)
	if err != nil {
		t.Fatalf("NewStateStore: %v", err)
	}
	// No real watchers in tests — pass nil brokers slice from Build, use stub brokers.
	brokers := obsidianoid.MakeTestBrokers(2)
	h := obsidianoid.NewHandler(cfg, state, brokers)

	mux := http.NewServeMux()
	h.Register(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return &harness{vault0: v0, vault1: v1, state: state, h: h, mux: mux, server: srv}
}

func (hh *harness) do(t *testing.T, method, path string, body any) *http.Response {
	t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, hh.server.URL+path, r)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request %s %s: %v", method, path, err)
	}
	return resp
}

func (hh *harness) doRaw(t *testing.T, method, path string, contentType, rawBody string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(method, hh.server.URL+path, strings.NewReader(rawBody))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request %s %s: %v", method, path, err)
	}
	return resp
}

func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("readBody: %v", err)
	}
	return string(b)
}

// ── Tests ────────────────────────────────────────────────────────────────────

func TestHandlerVaults(t *testing.T) {
	hh := newHarness(t)
	resp := hh.do(t, "GET", "/api/vaults", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var vaults []map[string]string
	_ = json.NewDecoder(resp.Body).Decode(&vaults)
	resp.Body.Close()
	if len(vaults) != 2 {
		t.Fatalf("expected 2 vaults, got %d", len(vaults))
	}
	if vaults[0]["name"] != "Vault0" {
		t.Errorf("vault[0] name: %q", vaults[0]["name"])
	}
	// Path must not be exposed.
	if _, ok := vaults[0]["path"]; ok {
		t.Error("vault response must not expose filesystem path")
	}
}

func TestHandlerVaultSelector(t *testing.T) {
	hh := newHarness(t)
	resp := hh.do(t, "GET", "/api/vaults", nil)
	var vaults []map[string]string
	_ = json.NewDecoder(resp.Body).Decode(&vaults)
	resp.Body.Close()
	// vault=99 out of range → falls back to vault0 name.
	resp2 := hh.do(t, "GET", "/api/tree?vault=99", nil)
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("vault=99 fallback: expected 200, got %d", resp2.StatusCode)
	}
	resp2.Body.Close()
}

func TestHandlerConfig(t *testing.T) {
	hh := newHarness(t)
	resp := hh.do(t, "GET", "/api/config", nil)
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}
	var cfg map[string]any
	_ = json.Unmarshal([]byte(body), &cfg)
	if cfg["autosave"] != true {
		t.Errorf("expected autosave true, got %v", cfg["autosave"])
	}
}

func TestHandlerTree(t *testing.T) {
	hh := newHarness(t)
	resp := hh.do(t, "GET", "/api/tree", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var node map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&node)
	resp.Body.Close()
	if node["is_dir"] != true {
		t.Error("root tree node should be a directory")
	}
	// vault=1 tree should also work (empty vault).
	resp2 := hh.do(t, "GET", "/api/tree?vault=1", nil)
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("vault=1 tree: expected 200, got %d", resp2.StatusCode)
	}
	resp2.Body.Close()
}

func TestHandlerNoteGetPut(t *testing.T) {
	hh := newHarness(t)

	// PUT a note.
	putResp := hh.doRaw(t, "PUT", "/api/note?path=Test.md", "text/plain", "# Test content")
	if putResp.StatusCode != http.StatusNoContent {
		t.Fatalf("PUT note: expected 204, got %d: %s", putResp.StatusCode, readBody(t, putResp))
	}
	putResp.Body.Close()

	// GET it back.
	getResp := hh.doRaw(t, "GET", "/api/note?path=Test.md", "", "")
	body := readBody(t, getResp)
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("GET note: expected 200, got %d", getResp.StatusCode)
	}
	if body != "# Test content" {
		t.Errorf("unexpected note content: %q", body)
	}
	ct := getResp.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/plain") {
		t.Errorf("expected text/plain content type, got %q", ct)
	}
}

func TestHandlerNoteMissingPath(t *testing.T) {
	hh := newHarness(t)
	resp := hh.doRaw(t, "GET", "/api/note", "", "")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("missing path: expected 400, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestHandlerNoteNotFound(t *testing.T) {
	hh := newHarness(t)
	resp := hh.doRaw(t, "GET", "/api/note?path=missing.md", "", "")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("missing note: expected 404, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestHandlerRender(t *testing.T) {
	hh := newHarness(t)
	resp := hh.doRaw(t, "POST", "/api/render", "text/plain", "# Hello\n\nWorld")
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("render: expected 200, got %d", resp.StatusCode)
	}
	if !strings.Contains(body, "<h1") {
		t.Errorf("render output missing <h1>: %s", body)
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/html") {
		t.Errorf("expected text/html, got %q", ct)
	}
}

func TestHandlerThreadsGetPut(t *testing.T) {
	hh := newHarness(t)

	// GET — should return 4 threads.
	getResp := hh.do(t, "GET", "/api/threads", nil)
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("GET threads: expected 200, got %d", getResp.StatusCode)
	}
	var threads []map[string]any
	_ = json.NewDecoder(getResp.Body).Decode(&threads)
	getResp.Body.Close()
	if len(threads) != 4 {
		t.Fatalf("expected 4 threads, got %d", len(threads))
	}

	// PUT with one disabled.
	input := []map[string]any{
		{"content": "Thread A", "disabled": false},
		{"content": "Thread B", "disabled": true},
		{"content": "", "disabled": false},
		{"content": "", "disabled": false},
	}
	putResp := hh.do(t, "PUT", "/api/threads", input)
	if putResp.StatusCode != http.StatusNoContent {
		t.Fatalf("PUT threads: expected 204, got %d: %s", putResp.StatusCode, readBody(t, putResp))
	}
	putResp.Body.Close()

	// Verify state.json persisted the disabled flag.
	states := hh.state.States()
	if !states[1].Disabled {
		t.Error("thread[1] disabled flag not persisted to StateStore")
	}
}

func TestHandlerThreadsWrongCount(t *testing.T) {
	hh := newHarness(t)
	input := []map[string]any{{"content": "only one", "disabled": false}}
	resp := hh.do(t, "PUT", "/api/threads", input)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("wrong thread count: expected 400, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestHandlerGitStatus(t *testing.T) {
	hh := newHarness(t)
	resp := hh.do(t, "GET", "/api/git/status", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("git/status: expected 200, got %d", resp.StatusCode)
	}
	var result map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&result)
	resp.Body.Close()
	if _, ok := result["available"]; !ok {
		t.Error("git/status response missing 'available' key")
	}
}

func TestHandlerEventsContentType(t *testing.T) {
	hh := newHarness(t)
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "GET", hh.server.URL+"/api/events", nil)
	resp, err := http.DefaultClient.Do(req)
	cancel() // immediately cancel after headers are received
	if err != nil {
		t.Fatalf("events request: %v", err)
	}
	defer resp.Body.Close()
	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/event-stream") {
		t.Errorf("expected text/event-stream, got %q", ct)
	}
}

func TestHandlerThreadsPutPersistsToStateFile(t *testing.T) {
	hh := newHarness(t)
	input := []map[string]any{
		{"content": "A", "disabled": false},
		{"content": "B", "disabled": true},
		{"content": "C", "disabled": false},
		{"content": "D", "disabled": false},
	}
	putResp := hh.do(t, "PUT", "/api/threads", input)
	putResp.Body.Close()

	// Reconstruct a new StateStore over the same data dir → should see persisted value.
	s2, err := obsidianoid.NewStateStore(hh.state.DataDir(), 4)
	if err != nil {
		t.Fatalf("reload StateStore: %v", err)
	}
	if !s2.States()[1].Disabled {
		t.Error("disabled flag not durable across StateStore reload")
	}
}
