package obsidianoid_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
			{Path: v0, Name: "Vault0", Theme: "obsidian"},
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
	if cfg["threads_folder"] != "Threads" {
		t.Errorf("expected threads_folder Threads, got %v", cfg["threads_folder"])
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
	if csp := resp.Header.Get("Content-Security-Policy"); csp != "sandbox" {
		t.Errorf("expected Content-Security-Policy: sandbox, got %q", csp)
	}
}

func TestHandlerRender_CSPOnlyOnRenderRoute(t *testing.T) {
	hh := newHarness(t)
	resp := hh.do(t, "GET", "/api/config", nil)
	resp.Body.Close()
	if csp := resp.Header.Get("Content-Security-Policy"); csp != "" {
		t.Errorf("expected no Content-Security-Policy on /api/config, got %q", csp)
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

func TestHandlerTreeCarriesMtime(t *testing.T) {
	hh := newHarness(t)
	_ = os.MkdirAll(filepath.Join(hh.vault0, "Sub"), 0o755)
	_ = os.WriteFile(filepath.Join(hh.vault0, "Sub", "Newer.md"), []byte("x"), 0o644)
	older := time.Now().Add(-48 * time.Hour)
	_ = os.Chtimes(filepath.Join(hh.vault0, "Hello.md"), older, older)

	resp := hh.do(t, "GET", "/api/tree", nil)
	var root struct {
		Mtime    int64 `json:"mtime"`
		Children []struct {
			Name     string `json:"name"`
			IsDir    bool   `json:"is_dir"`
			Mtime    int64  `json:"mtime"`
			Children []struct {
				Mtime int64 `json:"mtime"`
			} `json:"children"`
		} `json:"children"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&root)
	resp.Body.Close()

	var hello, sub int64
	var subChild int64
	for _, c := range root.Children {
		switch c.Name {
		case "Hello":
			hello = c.Mtime
		case "Sub":
			sub = c.Mtime
			if len(c.Children) == 1 {
				subChild = c.Children[0].Mtime
			}
		}
	}
	if hello == 0 || sub == 0 {
		t.Fatalf("missing mtimes: hello=%d sub=%d", hello, sub)
	}
	if sub != subChild {
		t.Errorf("folder mtime %d should equal its newest note's %d", sub, subChild)
	}
	if hello >= sub {
		t.Errorf("older note mtime %d should be below the newer folder's %d", hello, sub)
	}
	if root.Mtime != sub {
		t.Errorf("root mtime %d should be the newest note's %d", root.Mtime, sub)
	}
}

func TestHandlerSearch(t *testing.T) {
	hh := newHarness(t)
	_ = os.WriteFile(filepath.Join(hh.vault0, "Recipes.md"), []byte("Bake at 350 && enjoy"), 0o644)
	_ = os.WriteFile(filepath.Join(hh.vault0, "Other.md"), []byte("nothing here"), 0o644)

	search := func(q string) []string {
		resp := hh.do(t, "GET", "/api/search?q="+q, nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("search %q: status %d", q, resp.StatusCode)
		}
		var out struct {
			Paths []string `json:"paths"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&out)
		resp.Body.Close()
		return out.Paths
	}

	if got := search("BAKE"); len(got) != 1 || got[0] != "Recipes.md" {
		t.Errorf("content match (case-insensitive): got %v", got)
	}
	if got := search("hell"); len(got) != 1 || got[0] != "Hello.md" {
		t.Errorf("name match: got %v", got)
	}
	if got := search("zzz-no-match"); len(got) != 0 {
		t.Errorf("no match should be empty, got %v", got)
	}
	if resp := hh.do(t, "GET", "/api/search", nil); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("empty q: status %d, want 400", resp.StatusCode)
	}
}

func TestHandlerNoteRename(t *testing.T) {
	hh := newHarness(t)
	_ = os.MkdirAll(filepath.Join(hh.vault0, "Sub"), 0o755)
	_ = os.WriteFile(filepath.Join(hh.vault0, "Sub", "Old.md"), []byte("keep me"), 0o644)
	_ = os.WriteFile(filepath.Join(hh.vault0, "Sub", "Taken.md"), []byte("x"), 0o644)

	rename := func(path, name string) (int, string) {
		resp := hh.do(t, "POST", "/api/note/rename", map[string]string{"path": path, "name": name})
		var out struct {
			Path string `json:"path"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&out)
		resp.Body.Close()
		return resp.StatusCode, out.Path
	}

	code, newPath := rename("Sub/Old.md", " New Name.md ")
	if code != http.StatusOK || newPath != "Sub/New Name.md" {
		t.Fatalf("rename: status %d path %q", code, newPath)
	}
	if b, err := os.ReadFile(filepath.Join(hh.vault0, "Sub", "New Name.md")); err != nil || string(b) != "keep me" {
		t.Errorf("renamed note content: %q, %v", b, err)
	}
	if _, err := os.Stat(filepath.Join(hh.vault0, "Sub", "Old.md")); !os.IsNotExist(err) {
		t.Error("old note should be gone")
	}

	if code, _ := rename("Sub/New Name.md", "Taken"); code != http.StatusConflict {
		t.Errorf("rename onto an existing note: status %d, want 409", code)
	}
	for _, bad := range []string{"", "a/b", ".hidden", "..\\x"} {
		if code, _ := rename("Sub/New Name.md", bad); code != http.StatusBadRequest {
			t.Errorf("name %q: status %d, want 400", bad, code)
		}
	}
	if code, _ := rename("Missing.md", "Whatever"); code != http.StatusNotFound {
		t.Errorf("missing note: status %d, want 404", code)
	}
	if code, _ := rename("../outside.md", "x"); code != http.StatusNotFound {
		t.Errorf("escaping path: status %d, want 404", code)
	}
}

func TestHandlerNoteDelete(t *testing.T) {
	hh := newHarness(t)
	_ = os.MkdirAll(filepath.Join(hh.vault0, "Sub"), 0o755)
	_ = os.WriteFile(filepath.Join(hh.vault0, "Sub", "Gone.md"), []byte("bye"), 0o644)
	_ = os.WriteFile(filepath.Join(hh.vault0, "notes.txt"), []byte("not a note"), 0o644)

	del := func(path string) int {
		resp := hh.do(t, "DELETE", "/api/note?path="+path, nil)
		resp.Body.Close()
		return resp.StatusCode
	}

	if code := del("Sub/Gone.md"); code != http.StatusOK {
		t.Fatalf("delete: status %d, want 200", code)
	}
	if _, err := os.Stat(filepath.Join(hh.vault0, "Sub", "Gone.md")); !os.IsNotExist(err) {
		t.Error("note should be deleted")
	}
	if code := del("Sub/Gone.md"); code != http.StatusNotFound {
		t.Errorf("deleting again: status %d, want 404", code)
	}
	for _, refused := range []string{"notes.txt", "Sub", "../outside.md"} {
		if code := del(refused); code != http.StatusNotFound {
			t.Errorf("%q: status %d, want 404", refused, code)
		}
	}
	if _, err := os.Stat(filepath.Join(hh.vault0, "notes.txt")); err != nil {
		t.Error("a non-note file must survive")
	}
	if _, err := os.Stat(filepath.Join(hh.vault0, "Sub")); err != nil {
		t.Error("a folder must survive")
	}
	if code := del(""); code != http.StatusBadRequest {
		t.Errorf("empty path: status %d, want 400", code)
	}
}

func TestHandlerThreadTitles(t *testing.T) {
	hh := newHarness(t)

	threads := make([]map[string]any, 4)
	for i := range threads {
		threads[i] = map[string]any{"content": "body", "disabled": false, "title": ""}
	}
	threads[1]["title"] = "  Groceries & errands  "
	threads[2]["title"] = strings.Repeat("t", 100)

	resp := hh.do(t, "PUT", "/api/threads", threads)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("PUT threads: status %d: %s", resp.StatusCode, readBody(t, resp))
	}
	resp.Body.Close()

	get := func() []obsidianoid.Thread {
		resp := hh.do(t, "GET", "/api/threads", nil)
		var out []obsidianoid.Thread
		_ = json.NewDecoder(resp.Body).Decode(&out)
		resp.Body.Close()
		return out
	}
	got := get()
	if got[1].Title != "Groceries & errands" {
		t.Errorf("title not trimmed/kept: %q", got[1].Title)
	}
	if n := len([]rune(got[2].Title)); n != 80 {
		t.Errorf("long title kept %d runes, want 80", n)
	}
	if got[0].Title != "" {
		t.Errorf("unset title should be empty, got %q", got[0].Title)
	}

	// The vault file keeps its fixed name and carries only the content.
	b, err := os.ReadFile(filepath.Join(hh.vault0, "Threads", obsidianoid.ThreadFileName(1)))
	if err != nil || string(b) != "body" {
		t.Errorf("thread file should hold only its content: %q, %v", b, err)
	}

	// Titles survive a restart: a fresh store over the same data dir reloads them.
	reloaded, err := obsidianoid.NewStateStore(hh.state.DataDir(), 4)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.States()[1].Title != "Groceries & errands" {
		t.Errorf("title not persisted: %+v", reloaded.States()[1])
	}
}

func TestHandlerThreadFileMoveOut(t *testing.T) {
	hh := newHarness(t)

	threads := make([]map[string]any, 4)
	for i := range threads {
		threads[i] = map[string]any{"content": fmt.Sprintf("body %d", i+1), "disabled": false, "title": ""}
	}
	threads[1]["title"] = "Errands"
	threads[1]["disabled"] = true
	threads[2]["title"] = "Ideas"
	resp := hh.do(t, "PUT", "/api/threads", threads)
	resp.Body.Close()

	threadFile := func(i int) string { return filepath.Join(hh.vault0, "Threads", obsidianoid.ThreadFileName(i)) }

	// Renaming Thread02 moves it out: the note keeps its content under the new
	// name, and slot 2 starts over with a fresh empty file and no title.
	resp = hh.do(t, "POST", "/api/note/rename", map[string]string{"path": "Threads/" + obsidianoid.ThreadFileName(1), "name": "Errands archive"})
	var rn struct {
		Path        string `json:"path"`
		ThreadReset bool   `json:"thread_reset"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&rn)
	resp.Body.Close()
	if !rn.ThreadReset || rn.Path != "Threads/Errands archive.md" {
		t.Fatalf("rename out of threads: %+v", rn)
	}
	if b, _ := os.ReadFile(filepath.Join(hh.vault0, "Threads", "Errands archive.md")); string(b) != "body 2" {
		t.Errorf("moved-out note content: %q", b)
	}
	if b, err := os.ReadFile(threadFile(1)); err != nil || len(b) != 0 {
		t.Errorf("slot 2 should have a fresh empty file: %q, %v", b, err)
	}

	// Deleting Thread03 resets slot 3 the same way.
	resp = hh.do(t, "DELETE", "/api/note?path=Threads/"+obsidianoid.ThreadFileName(2), nil)
	var dl struct {
		ThreadReset bool `json:"thread_reset"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&dl)
	resp.Body.Close()
	if !dl.ThreadReset {
		t.Error("deleting a thread file should report thread_reset")
	}
	if b, err := os.ReadFile(threadFile(2)); err != nil || len(b) != 0 {
		t.Errorf("slot 3 should have a fresh empty file: %q, %v", b, err)
	}

	resp = hh.do(t, "GET", "/api/threads", nil)
	var got []obsidianoid.Thread
	_ = json.NewDecoder(resp.Body).Decode(&got)
	resp.Body.Close()
	if got[1].Title != "" || got[1].Disabled || got[1].Content != "" {
		t.Errorf("slot 2 should be fresh: %+v", got[1])
	}
	if got[2].Title != "" || got[2].Content != "" {
		t.Errorf("slot 3 should be fresh: %+v", got[2])
	}
	if got[0].Content != "body 1" || got[3].Content != "body 4" {
		t.Errorf("other slots must be untouched: %+v / %+v", got[0], got[3])
	}

	// An ordinary note reports no thread reset.
	resp = hh.do(t, "DELETE", "/api/note?path=Hello.md", nil)
	_ = json.NewDecoder(resp.Body).Decode(&dl)
	resp.Body.Close()
	if dl.ThreadReset {
		t.Error("an ordinary note must not report thread_reset")
	}
}

func TestHandlerFolderCreateAndTree(t *testing.T) {
	hh := newHarness(t)
	post := func(p string) int {
		resp := hh.do(t, "POST", "/api/folder", map[string]string{"path": p})
		resp.Body.Close()
		return resp.StatusCode
	}
	if code := post("Projects/2026"); code != http.StatusCreated {
		t.Fatalf("create nested folder: status %d", code)
	}
	if code := post("Projects/2026"); code != http.StatusConflict {
		t.Errorf("existing folder: status %d, want 409", code)
	}
	for _, bad := range []string{"", "../escape", ".hidden", "a//b", "a/.git"} {
		if code := post(bad); code != http.StatusBadRequest {
			t.Errorf("folder %q: status %d, want 400", bad, code)
		}
	}

	// The empty folders appear in the tree, carrying their paths.
	resp := hh.do(t, "GET", "/api/tree", nil)
	body := readBody(t, resp)
	if !strings.Contains(body, `"path":"Projects"`) || !strings.Contains(body, `"path":"Projects/2026"`) {
		t.Errorf("empty folders missing from tree: %s", body)
	}
}

func TestHandlerNoteMove(t *testing.T) {
	hh := newHarness(t)
	_ = os.MkdirAll(filepath.Join(hh.vault0, "Archive"), 0o755)
	_ = os.WriteFile(filepath.Join(hh.vault0, "Archive", "Clash.md"), []byte("x"), 0o644)
	_ = os.WriteFile(filepath.Join(hh.vault0, "Clash.md"), []byte("y"), 0o644)

	move := func(p, folder string) (int, string) {
		resp := hh.do(t, "POST", "/api/note/move", map[string]string{"path": p, "folder": folder})
		var out struct {
			Path string `json:"path"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&out)
		resp.Body.Close()
		return resp.StatusCode, out.Path
	}

	if code, p := move("Hello.md", "Archive"); code != http.StatusOK || p != "Archive/Hello.md" {
		t.Fatalf("move into folder: %d %q", code, p)
	}
	if b, _ := os.ReadFile(filepath.Join(hh.vault0, "Archive", "Hello.md")); string(b) != "# Hello" {
		t.Errorf("moved content: %q", b)
	}
	if code, p := move("Archive/Hello.md", ""); code != http.StatusOK || p != "Hello.md" {
		t.Errorf("move to root: %d %q", code, p)
	}
	if code, _ := move("Clash.md", "Archive"); code != http.StatusConflict {
		t.Errorf("name clash: status %d, want 409", code)
	}
	if code, _ := move("Hello.md", "Nope"); code != http.StatusNotFound {
		t.Errorf("missing folder: status %d, want 404", code)
	}
	if code, _ := move("Hello.md", "../out"); code != http.StatusBadRequest {
		t.Errorf("escaping folder: status %d, want 400", code)
	}
	if code, _ := move("Missing.md", "Archive"); code != http.StatusNotFound {
		t.Errorf("missing note: status %d, want 404", code)
	}

	// Moving a thread file out of the thread folder resets its slot.
	threads := make([]map[string]any, 4)
	for i := range threads {
		threads[i] = map[string]any{"content": "t", "disabled": false, "title": "T"}
	}
	resp := hh.do(t, "PUT", "/api/threads", threads)
	resp.Body.Close()
	resp = hh.do(t, "POST", "/api/note/move", map[string]string{"path": "Threads/" + obsidianoid.ThreadFileName(0), "folder": "Archive"})
	var mv struct {
		ThreadReset bool `json:"thread_reset"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&mv)
	resp.Body.Close()
	if !mv.ThreadReset {
		t.Error("moving a thread file out should report thread_reset")
	}
	if b, err := os.ReadFile(filepath.Join(hh.vault0, "Threads", obsidianoid.ThreadFileName(0))); err != nil || len(b) != 0 {
		t.Errorf("slot 1 should have a fresh empty file: %q, %v", b, err)
	}
}

func TestHandlerFolderDelete(t *testing.T) {
	hh := newHarness(t)
	_ = os.MkdirAll(filepath.Join(hh.vault0, "Empty"), 0o755)
	_ = os.MkdirAll(filepath.Join(hh.vault0, "Full"), 0o755)
	_ = os.WriteFile(filepath.Join(hh.vault0, "Full", "Note.md"), []byte("x"), 0o644)
	_ = os.MkdirAll(filepath.Join(hh.vault0, "Hidden"), 0o755)
	_ = os.WriteFile(filepath.Join(hh.vault0, "Hidden", ".DS_Store"), []byte("x"), 0o644)

	del := func(p string) int {
		resp := hh.do(t, "DELETE", "/api/folder?path="+p, nil)
		resp.Body.Close()
		return resp.StatusCode
	}
	if code := del("Empty"); code != http.StatusNoContent {
		t.Fatalf("empty folder: status %d, want 204", code)
	}
	if _, err := os.Stat(filepath.Join(hh.vault0, "Empty")); !os.IsNotExist(err) {
		t.Error("empty folder should be gone")
	}
	if code := del("Full"); code != http.StatusConflict {
		t.Errorf("folder with a note: status %d, want 409", code)
	}
	if code := del("Hidden"); code != http.StatusConflict {
		t.Errorf("folder with a hidden file: status %d, want 409", code)
	}
	if code := del("Missing"); code != http.StatusNotFound {
		t.Errorf("missing folder: status %d, want 404", code)
	}
	if code := del("Hello.md"); code != http.StatusNotFound {
		t.Errorf("a note is not a folder: status %d, want 404", code)
	}
	for _, bad := range []string{"", "../x"} {
		if code := del(bad); code != http.StatusBadRequest {
			t.Errorf("%q: status %d, want 400", bad, code)
		}
	}
	if _, err := os.Stat(filepath.Join(hh.vault0, "Full", "Note.md")); err != nil {
		t.Error("a non-empty folder's contents must survive")
	}
}

func TestHandlerFolderRename(t *testing.T) {
	hh := newHarness(t)
	_ = os.MkdirAll(filepath.Join(hh.vault0, "Projects", "Old"), 0o755)
	_ = os.WriteFile(filepath.Join(hh.vault0, "Projects", "Old", "Plan.md"), []byte("plan"), 0o644)
	_ = os.MkdirAll(filepath.Join(hh.vault0, "Projects", "Taken"), 0o755)
	_ = os.MkdirAll(filepath.Join(hh.vault0, "Threads"), 0o755)

	rename := func(p, name string) (int, string) {
		resp := hh.do(t, "POST", "/api/folder/rename", map[string]string{"path": p, "name": name})
		var out struct {
			Path string `json:"path"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&out)
		resp.Body.Close()
		return resp.StatusCode, out.Path
	}

	if code, p := rename("Projects/Old", "New"); code != http.StatusOK || p != "Projects/New" {
		t.Fatalf("rename nested folder: %d %q", code, p)
	}
	if b, _ := os.ReadFile(filepath.Join(hh.vault0, "Projects", "New", "Plan.md")); string(b) != "plan" {
		t.Errorf("contents should move with the folder: %q", b)
	}
	if code, p := rename("Projects", "Work"); code != http.StatusOK || p != "Work" {
		t.Errorf("rename top-level folder: %d %q", code, p)
	}
	if code, _ := rename("Work/New", "Taken"); code != http.StatusConflict {
		t.Errorf("taken name: status %d, want 409", code)
	}
	if code, _ := rename("Threads", "Other"); code != http.StatusForbidden {
		t.Errorf("thread folder: status %d, want 403", code)
	}
	for _, bad := range []string{"a/b", ".hidden", ""} {
		if code, _ := rename("Work/New", bad); code != http.StatusBadRequest {
			t.Errorf("name %q: status %d, want 400", bad, code)
		}
	}
	if code, _ := rename("Missing", "X"); code != http.StatusNotFound {
		t.Errorf("missing folder: status %d, want 404", code)
	}
	if code, _ := rename("Hello.md", "X"); code != http.StatusNotFound {
		t.Errorf("a note is not a folder: status %d, want 404", code)
	}
}

func TestHandlerThreadCount(t *testing.T) {
	hh := newHarness(t)
	setCount := func(n int) int {
		resp := hh.do(t, "PUT", "/api/threads/count", map[string]int{"count": n})
		resp.Body.Close()
		return resp.StatusCode
	}
	getThreads := func() []obsidianoid.Thread {
		resp := hh.do(t, "GET", "/api/threads", nil)
		var out []obsidianoid.Thread
		_ = json.NewDecoder(resp.Body).Decode(&out)
		resp.Body.Close()
		return out
	}

	// Title slot 4, then shrink to 2 and grow to 6.
	threads := make([]map[string]any, 4)
	for i := range threads {
		threads[i] = map[string]any{"content": "c", "disabled": false, "title": ""}
	}
	threads[3]["title"] = "Fourth"
	resp := hh.do(t, "PUT", "/api/threads", threads)
	resp.Body.Close()

	if code := setCount(2); code != http.StatusOK {
		t.Fatalf("set count 2: status %d", code)
	}
	if n := len(getThreads()); n != 2 {
		t.Errorf("after shrinking, %d threads, want 2", n)
	}
	// The dropped slot's file stays in the vault as an ordinary note.
	if _, err := os.Stat(filepath.Join(hh.vault0, "Threads", obsidianoid.ThreadFileName(3))); err != nil {
		t.Errorf("a dropped slot's file must stay in the vault: %v", err)
	}
	if code := setCount(6); code != http.StatusOK {
		t.Fatalf("set count 6: status %d", code)
	}
	got := getThreads()
	if len(got) != 6 || got[3].Title != "Fourth" {
		t.Errorf("after growing, want 6 threads with slot 4's title back: %d, %q", len(got), got[3].Title)
	}

	for _, bad := range []int{0, -1, obsidianoid.MaxThreadCount + 1} {
		if code := setCount(bad); code != http.StatusBadRequest {
			t.Errorf("count %d: status %d, want 400", bad, code)
		}
	}

	// The chosen count survives a restart.
	reloaded, _ := obsidianoid.NewStateStore(hh.state.DataDir(), 4)
	if reloaded.Count() != 6 {
		t.Errorf("count not persisted: %d", reloaded.Count())
	}

	resp = hh.do(t, "GET", "/api/config", nil)
	var cfg map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&cfg)
	resp.Body.Close()
	if cfg["thread_count"] != float64(6) {
		t.Errorf("config thread_count = %v, want 6", cfg["thread_count"])
	}
}

func TestHandlerOverwrite(t *testing.T) {
	hh := newHarness(t)
	_ = os.MkdirAll(filepath.Join(hh.vault0, "Box"), 0o755)
	_ = os.WriteFile(filepath.Join(hh.vault0, "A.md"), []byte("from A"), 0o644)
	_ = os.WriteFile(filepath.Join(hh.vault0, "B.md"), []byte("old B"), 0o644)
	_ = os.WriteFile(filepath.Join(hh.vault0, "Box", "A.md"), []byte("old boxed A"), 0o644)

	post := func(url string, body map[string]any) int {
		resp := hh.do(t, "POST", url, body)
		resp.Body.Close()
		return resp.StatusCode
	}

	// Rename onto an existing note: refused without overwrite, replaced with it.
	if code := post("/api/note/rename", map[string]any{"path": "A.md", "name": "B"}); code != http.StatusConflict {
		t.Fatalf("rename clash without overwrite: %d, want 409", code)
	}
	if code := post("/api/note/rename", map[string]any{"path": "A.md", "name": "B", "overwrite": true}); code != http.StatusOK {
		t.Fatalf("rename with overwrite: %d", code)
	}
	if b, _ := os.ReadFile(filepath.Join(hh.vault0, "B.md")); string(b) != "from A" {
		t.Errorf("overwritten note content: %q", b)
	}

	// Move onto an existing note: same rule.
	_ = os.WriteFile(filepath.Join(hh.vault0, "A.md"), []byte("new A"), 0o644)
	if code := post("/api/note/move", map[string]any{"path": "A.md", "folder": "Box"}); code != http.StatusConflict {
		t.Fatalf("move clash without overwrite: %d, want 409", code)
	}
	if code := post("/api/note/move", map[string]any{"path": "A.md", "folder": "Box", "overwrite": true}); code != http.StatusOK {
		t.Fatalf("move with overwrite: %d", code)
	}
	if b, _ := os.ReadFile(filepath.Join(hh.vault0, "Box", "A.md")); string(b) != "new A" {
		t.Errorf("overwritten moved note: %q", b)
	}

	// The example: a note moved out of the thread pool goes back in by
	// overwriting the fresh empty slot file.
	threads := make([]map[string]any, 4)
	for i := range threads {
		threads[i] = map[string]any{"content": fmt.Sprintf("t%d", i+1), "disabled": false, "title": ""}
	}
	resp := hh.do(t, "PUT", "/api/threads", threads)
	resp.Body.Close()
	slot2 := "Threads/" + obsidianoid.ThreadFileName(1)
	if code := post("/api/note/rename", map[string]any{"path": slot2, "name": "Parked"}); code != http.StatusOK {
		t.Fatalf("move thread out: %d", code)
	}
	if code := post("/api/note/rename", map[string]any{"path": "Threads/Parked.md", "name": strings.TrimSuffix(obsidianoid.ThreadFileName(1), ".md"), "overwrite": true}); code != http.StatusOK {
		t.Fatalf("move thread back in: %d", code)
	}
	resp = hh.do(t, "GET", "/api/threads", nil)
	var got []obsidianoid.Thread
	_ = json.NewDecoder(resp.Body).Decode(&got)
	resp.Body.Close()
	if got[1].Content != "t2" {
		t.Errorf("thread 2 should hold its original content again, got %q", got[1].Content)
	}
}
