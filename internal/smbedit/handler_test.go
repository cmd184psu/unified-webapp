package smbedit

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.uber.org/goleak"
)

// newTestServer builds a server backed by t.TempDir() state, no static dir
// (the smbed NewServer(cfg, "test", nil) shape). SmbConfPath is pointed into
// the temp dir so conf writes never touch the real system, and SambaLogPath
// is cleared so nothing shells out to sudo tail by accident.
func newTestServer(t *testing.T) *server {
	t.Helper()
	dir := t.TempDir()
	st, err := newStore(dir)
	if err != nil {
		t.Fatalf("newStore(): %v", err)
	}
	if _, err := st.update(func(s *State) {
		s.SmbConfPath = filepath.Join(dir, "smb.conf")
		s.SambaLogPath = ""
	}); err != nil {
		t.Fatalf("seeding state: %v", err)
	}
	return newServer(serverOptions{store: st, pickerRoot: dir, version: "test"})
}

// doJSON sends a request through the full wrapped chain (Handler(), never the
// bare mux) so every test exercises the D-9 recovery wrapper.
func doJSON(t *testing.T, srv *server, method, target string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, target, &buf)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	return rr
}

// swapRunCommand replaces the batch command seam for one test.
func swapRunCommand(t *testing.T, fn func(name string, args ...string) (string, error)) {
	t.Helper()
	orig := runCommand
	runCommand = fn
	t.Cleanup(func() { runCommand = orig })
}

func TestAllRoutesReachable(t *testing.T) {
	defer goleak.VerifyNone(t)
	srv := newTestServer(t)
	swapRunCommand(t, func(name string, args ...string) (string, error) { return "", nil })

	// The normative 13-registration table. Reachable means the request hit a
	// handler: any status except the mux's own 404/405.
	routes := []struct{ method, path string }{
		{http.MethodGet, "/api/config"},
		{http.MethodPut, "/api/config"},
		{http.MethodGet, "/api/shares"},
		{http.MethodPut, "/api/shares"},
		{http.MethodGet, "/api/globals"},
		{http.MethodPut, "/api/globals"},
		{http.MethodGet, "/api/folders"},
		{http.MethodPost, "/api/import"},
		{http.MethodPost, "/api/save-and-restart"},
		{http.MethodGet, "/api/preview"},
		{http.MethodGet, "/api/logs/ops/stream"},
		{http.MethodGet, "/api/logs/samba/stream"},
		{http.MethodGet, "/api/version"},
	}
	if len(routes) != 13 {
		t.Fatalf("route table has %d entries, want 13", len(routes))
	}

	for _, rt := range routes {
		var body io.Reader
		switch {
		case rt.method == http.MethodPut && rt.path == "/api/config":
			body = strings.NewReader(`{}`)
		case rt.method == http.MethodPut:
			body = strings.NewReader(`[]`)
		default:
			body = strings.NewReader("")
		}
		req := httptest.NewRequest(rt.method, rt.path, body)
		req.Header.Set("Content-Type", "application/json")
		// Pre-cancel so the ops stream flushes its backlog and returns
		// instead of blocking on the live tail.
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		req = req.WithContext(ctx)

		rr := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rr, req)
		if rr.Code == http.StatusNotFound || rr.Code == http.StatusMethodNotAllowed {
			t.Errorf("%s %s: got %d — registration missing", rt.method, rt.path, rr.Code)
		}
	}
}

func TestWrongMethod_NoStaticDir_405(t *testing.T) {
	defer goleak.VerifyNone(t)
	srv := newTestServer(t)
	rr := doJSON(t, srv, http.MethodDelete, "/api/shares", nil)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("DELETE /api/shares without static dir: got %d, want 405", rr.Code)
	}
}

// newStaticTestServer builds a server WITH a static dir containing index.html,
// app.js, and a populated subdirectory.
func newStaticTestServer(t *testing.T) *server {
	t.Helper()
	dir := t.TempDir()
	staticDir := filepath.Join(dir, "static")
	if err := os.MkdirAll(filepath.Join(staticDir, "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		"index.html":     "<html>INDEX</html>",
		"app.js":         "console.log('app')",
		"subdir/file.js": "console.log('sub')",
	} {
		if err := os.WriteFile(filepath.Join(staticDir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	dataDir := filepath.Join(dir, "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	st, err := newStore(dataDir)
	if err != nil {
		t.Fatalf("newStore(): %v", err)
	}
	return newServer(serverOptions{store: st, staticDir: staticDir, pickerRoot: dir, version: "test"})
}

func TestStaticHandlerRules(t *testing.T) {
	defer goleak.VerifyNone(t)
	srv := newStaticTestServer(t)

	// Real file served.
	rr := doJSON(t, srv, http.MethodGet, "/app.js", nil)
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "console.log('app')") {
		t.Errorf("GET /app.js: got %d %q, want the file", rr.Code, rr.Body.String())
	}

	// Mistyped API path fails as an API call, not index.html.
	rr = doJSON(t, srv, http.MethodGet, "/api/nope", nil)
	if rr.Code != http.StatusNotFound {
		t.Errorf("GET /api/nope: got %d, want 404", rr.Code)
	}
	var errBody map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&errBody); err != nil || errBody["error"] == "" {
		t.Errorf("GET /api/nope: want a JSON error body, got %q (decode err %v)", rr.Body.String(), err)
	}

	// Unknown non-API GET → SPA fallback.
	rr = doJSON(t, srv, http.MethodGet, "/some/client/route", nil)
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "INDEX") {
		t.Errorf("GET /some/client/route: got %d %q, want index.html", rr.Code, rr.Body.String())
	}

	// Directory path → no listing (SPA fallback, never the file names).
	rr = doJSON(t, srv, http.MethodGet, "/subdir/", nil)
	if strings.Contains(rr.Body.String(), "file.js") {
		t.Errorf("GET /subdir/: directory listing leaked: %q", rr.Body.String())
	}

	// Non-GET miss on a non-API path → 405.
	rr = doJSON(t, srv, http.MethodPost, "/not/a/file", nil)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST /not/a/file: got %d, want 405", rr.Code)
	}
}

func TestWrongMethod_WithStaticDir_404JSON(t *testing.T) {
	defer goleak.VerifyNone(t)
	srv := newStaticTestServer(t)
	rr := doJSON(t, srv, http.MethodDelete, "/api/shares", nil)
	if rr.Code != http.StatusNotFound {
		t.Errorf("DELETE /api/shares with static dir: got %d, want 404 (house convention)", rr.Code)
	}
	var errBody map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&errBody); err != nil || errBody["error"] == "" {
		t.Errorf("want a JSON error body, got %q (decode err %v)", rr.Body.String(), err)
	}
}

// captureLog redirects the standard logger for the duration of the test.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prevOut := log.Writer()
	prevFlags := log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(prevOut)
		log.SetFlags(prevFlags)
	})
	return &buf
}

func TestPanicRecovery_500WithBodyAndLoggedStack(t *testing.T) {
	defer goleak.VerifyNone(t)
	srv := newTestServer(t)
	srv.mux.HandleFunc("GET /api/test-panic", func(w http.ResponseWriter, r *http.Request) {
		panic("boom for test")
	})
	logged := captureLog(t)

	rr := doJSON(t, srv, http.MethodGet, "/api/test-panic", nil)
	if rr.Code != http.StatusInternalServerError {
		t.Errorf("panicking route: got %d, want 500", rr.Code)
	}
	if rr.Body.Len() == 0 {
		t.Error("panicking route: want a response body, got none")
	}
	out := logged.String()
	if !strings.Contains(out, "boom for test") || !strings.Contains(out, "goroutine") {
		t.Errorf("want panic value and stack in the log, got: %q", out)
	}
}

func TestHandlerChain_PreservesFlusher(t *testing.T) {
	defer goleak.VerifyNone(t)
	srv := newTestServer(t)
	var isFlusher, isUnwrapped bool
	srv.mux.HandleFunc("GET /api/test-flusher", func(w http.ResponseWriter, r *http.Request) {
		_, isFlusher = w.(http.Flusher)
		if u, ok := w.(interface{ Unwrap() http.ResponseWriter }); ok {
			isUnwrapped = u.Unwrap() != nil
		}
		w.WriteHeader(http.StatusOK)
	})

	doJSON(t, srv, http.MethodGet, "/api/test-flusher", nil)
	if !isFlusher {
		t.Error("ResponseWriter through Handler() lost http.Flusher — SSE would return 500 streaming unsupported (D-9)")
	}
	if !isUnwrapped {
		t.Error("ResponseWriter through Handler() does not forward Unwrap()")
	}
}
