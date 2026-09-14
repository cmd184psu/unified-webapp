package smbedit

// State-handler tests: ported from smbed's server_test.go plus the port's
// new assertions — listen_addr ignored, persistence after each PUT, D-8
// save-failure rollback with pinned ops-log ordering, and the TR-2
// concurrency test.

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"go.uber.org/goleak"
)

func TestGetConfig(t *testing.T) {
	defer goleak.VerifyNone(t)
	srv := newTestServer(t)
	rr := doJSON(t, srv, http.MethodGet, "/api/config", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var raw map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if raw["theme"] != "dark" {
		t.Errorf("expected default theme 'dark', got %v", raw["theme"])
	}
	if _, present := raw["listen_addr"]; present {
		t.Error("config response must not contain listen_addr")
	}
}

func TestPutConfig_UpdateTheme(t *testing.T) {
	defer goleak.VerifyNone(t)
	srv := newTestServer(t)
	rr := doJSON(t, srv, http.MethodPut, "/api/config", map[string]string{"theme": "light"})
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body)
	}
	var st State
	json.NewDecoder(rr.Body).Decode(&st)
	if st.Theme != "light" {
		t.Errorf("expected theme 'light', got %q", st.Theme)
	}
	// Persistence: state.json reflects the PUT.
	if got := srv.store.snapshot().Theme; got != "light" {
		t.Errorf("store after PUT: theme %q, want light", got)
	}
	reloaded, err := newStore(srv.store.dataDir)
	if err != nil {
		t.Fatalf("reloading state.json: %v", err)
	}
	if got := reloaded.snapshot().Theme; got != "light" {
		t.Errorf("state.json after PUT: theme %q, want light", got)
	}
}

func TestPutConfig_IgnoresListenAddr(t *testing.T) {
	defer goleak.VerifyNone(t)
	srv := newTestServer(t)
	rr := doJSON(t, srv, http.MethodPut, "/api/config", map[string]string{
		"listen_addr": ":9999",
		"theme":       "light",
	})
	// Ignored, not rejected (§5.3): the request succeeds and the rest of the
	// patch is applied.
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT with listen_addr must be accepted, got %d: %s", rr.Code, rr.Body)
	}
	if got := srv.store.snapshot().Theme; got != "light" {
		t.Errorf("theme from same patch not applied: got %q", got)
	}
	if strings.Contains(rr.Body.String(), "listen_addr") {
		t.Errorf("listen_addr leaked into the response: %s", rr.Body)
	}
}

func TestSharesRoundTrip(t *testing.T) {
	defer goleak.VerifyNone(t)
	srv := newTestServer(t)
	dir := t.TempDir() // existing path so the share stays enabled

	shares := []Share{
		{Name: "movies", Path: dir, Writable: true, BrowseAble: true, Enabled: true},
	}
	rr := doJSON(t, srv, http.MethodPut, "/api/shares", shares)
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT /api/shares: %d %s", rr.Code, rr.Body)
	}

	rr = doJSON(t, srv, http.MethodGet, "/api/shares", nil)
	var got []Share
	json.NewDecoder(rr.Body).Decode(&got)
	if len(got) != 1 || got[0].Name != "movies" || !got[0].Enabled {
		t.Errorf("unexpected shares: %+v", got)
	}

	// Persistence: a fresh store sees the share.
	reloaded, err := newStore(srv.store.dataDir)
	if err != nil {
		t.Fatalf("reloading state.json: %v", err)
	}
	if rs := reloaded.snapshot().Shares; len(rs) != 1 || rs[0].Name != "movies" {
		t.Errorf("state.json after PUT: %+v", rs)
	}
}

func TestPutShares_DisablesMissingPath(t *testing.T) {
	defer goleak.VerifyNone(t)
	srv := newTestServer(t)

	shares := []Share{
		{Name: "gone", Path: "/does/not/exist/anywhere", Writable: true, BrowseAble: true, Enabled: true},
	}
	rr := doJSON(t, srv, http.MethodPut, "/api/shares", shares)
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT /api/shares: %d %s", rr.Code, rr.Body)
	}

	var got []Share
	json.NewDecoder(rr.Body).Decode(&got)
	if len(got) != 1 || got[0].Enabled {
		t.Errorf("expected share with missing path to come back disabled: %+v", got)
	}

	// The auto-disable ops entry is present — and only after a successful save.
	var found bool
	for _, e := range srv.ops.snapshot() {
		if strings.Contains(e.Message, "auto-disabled") {
			found = true
		}
	}
	if !found {
		t.Error("expected an auto-disabled ops-log entry after successful save")
	}
}

func TestGlobalsRoundTrip(t *testing.T) {
	defer goleak.VerifyNone(t)
	srv := newTestServer(t)

	globals := []GlobalEntry{{Key: "workgroup", Value: "TESTGROUP"}}
	rr := doJSON(t, srv, http.MethodPut, "/api/globals", globals)
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT /api/globals: %d %s", rr.Code, rr.Body)
	}

	rr = doJSON(t, srv, http.MethodGet, "/api/globals", nil)
	var got []GlobalEntry
	json.NewDecoder(rr.Body).Decode(&got)
	if len(got) != 1 || got[0].Value != "TESTGROUP" {
		t.Errorf("unexpected globals: %+v", got)
	}

	reloaded, err := newStore(srv.store.dataDir)
	if err != nil {
		t.Fatalf("reloading state.json: %v", err)
	}
	if rg := reloaded.snapshot().Globals; len(rg) != 1 || rg[0].Value != "TESTGROUP" {
		t.Errorf("state.json after PUT: %+v", rg)
	}
}

func TestGetFolders(t *testing.T) {
	defer goleak.VerifyNone(t)
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "media"), 0o755); err != nil {
		t.Fatal(err)
	}
	st, err := newStore(filepath.Join(dir, "data"))
	if err != nil {
		t.Fatalf("newStore(): %v", err)
	}
	srv := newServer(serverOptions{store: st, pickerRoot: dir, version: "test"})

	rr := doJSON(t, srv, http.MethodGet, "/api/folders", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body)
	}
	var got []FolderEntry
	json.NewDecoder(rr.Body).Decode(&got)
	// "data" (the store dir) and "media" both qualify; require media present.
	names := map[string]bool{}
	for _, e := range got {
		names[e.Name] = true
	}
	if !names["media"] {
		t.Errorf("expected 'media' in folder list, got %+v", got)
	}
}

func TestGetFolders_MissingRoot500(t *testing.T) {
	defer goleak.VerifyNone(t)
	st, err := newStore(t.TempDir())
	if err != nil {
		t.Fatalf("newStore(): %v", err)
	}
	srv := newServer(serverOptions{store: st, pickerRoot: filepath.Join(t.TempDir(), "nope"), version: "test"})
	rr := doJSON(t, srv, http.MethodGet, "/api/folders", nil)
	if rr.Code != http.StatusInternalServerError {
		t.Errorf("missing picker root: got %d, want 500 (A5: no softening)", rr.Code)
	}
}

func TestPutShares_SaveFailureRollsBack(t *testing.T) {
	defer goleak.VerifyNone(t)
	if os.Geteuid() == 0 {
		t.Skip("running as root bypasses permission checks")
	}
	srv := newTestServer(t)
	before := srv.store.snapshot()

	// Make the data dir unwritable so the atomic save fails.
	if err := os.Chmod(srv.store.dataDir, 0o555); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	defer os.Chmod(srv.store.dataDir, 0o755)

	// A share whose missing path would trigger the auto-disable entry — which
	// must NOT be logged, because the save fails (pinned D-8 ordering).
	shares := []Share{
		{Name: "gone", Path: "/does/not/exist/anywhere", Enabled: true},
	}
	rr := doJSON(t, srv, http.MethodPut, "/api/shares", shares)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 on save failure, got %d: %s", rr.Code, rr.Body)
	}

	after := srv.store.snapshot()
	if len(after.Shares) != len(before.Shares) {
		t.Errorf("in-memory state mutated despite failed save: %+v", after.Shares)
	}

	var sawSaveFailure, sawAutoDisable bool
	for _, e := range srv.ops.snapshot() {
		if strings.Contains(e.Message, "auto-disabled") {
			sawAutoDisable = true
		}
		if strings.Contains(e.Message, "ERROR: saving state.json failed") {
			sawSaveFailure = true
		}
	}
	if !sawSaveFailure {
		t.Error("expected the save-failure ops entry")
	}
	if sawAutoDisable {
		t.Error("auto-disable entry logged despite rolled-back save (D-8 ordering violation)")
	}
}

// TR-2: 16 goroutines interleaving PUT /api/shares, PUT /api/globals, and
// GET /api/config. Correctness is "no race, no torn state" under -race.
func TestConcurrentStateMutation(t *testing.T) {
	defer goleak.VerifyNone(t)
	srv := newTestServer(t)
	dir := t.TempDir()

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 5; j++ {
				switch n % 3 {
				case 0:
					doJSON(t, srv, http.MethodPut, "/api/shares", []Share{
						{Name: "s", Path: dir, Enabled: true},
					})
				case 1:
					doJSON(t, srv, http.MethodPut, "/api/globals", []GlobalEntry{
						{Key: "workgroup", Value: "WG"},
					})
				default:
					doJSON(t, srv, http.MethodGet, "/api/config", nil)
				}
			}
		}(i)
	}
	wg.Wait()

	// The persisted file must still be valid JSON that reloads cleanly.
	if _, err := newStore(srv.store.dataDir); err != nil {
		t.Fatalf("state.json corrupt after concurrent mutation: %v", err)
	}
}
