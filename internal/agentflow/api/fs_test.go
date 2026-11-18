package api

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cmd184psu/unified-webapp/internal/agentflow/engine"
)

func TestFSDirs_ListsConfinesAndFollowsTheLiveSetting(t *testing.T) {
	e := newEnv(t, envOpt{})
	root := t.TempDir()
	for _, d := range []string{"beta", "alpha/inner", ".hidden", "repo/.git"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "file.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	resolved, _ := filepath.EvalSymlinks(root)

	// By default nothing is confined: the listing starts at the server user's
	// home and Up can leave it, all the way to "/".
	home, _ := os.UserHomeDir()
	got := decode[DirListing](t, e.ok("GET", "/api/fs/dirs", nil, 200))
	if want, _ := filepath.EvalSymlinks(home); got.Path != want || got.Parent == "" {
		t.Fatalf("default listing = %+v, want home %q with a parent", got, want)
	}
	top := decode[DirListing](t, e.ok("GET", "/api/fs/dirs?path=/", nil, 200))
	if top.Path != "/" || top.Parent != "" {
		t.Fatalf("root listing = %+v", top)
	}

	// Saving browse_roots applies to the very next call.
	e.ok("PUT", "/api/settings", map[string]any{"browse_roots": []string{root}}, 200)
	got = decode[DirListing](t, e.ok("GET", "/api/fs/dirs", nil, 200))
	if got.Path != resolved || got.Parent != "" { // a browse root has no parent to offer
		t.Fatalf("listing = %+v", got)
	}
	var names []string
	for _, d := range got.Dirs {
		names = append(names, d.Name)
		if d.Path != filepath.Join(resolved, d.Name) || d.Git != (d.Name == "repo") {
			t.Errorf("entry %+v", d)
		}
	}
	// Sorted, no files, no hidden directories; the symlinked directory is a directory.
	if want := []string{"alpha", "beta", "escape", "repo"}; len(names) != len(want) || names[0] != want[0] || names[1] != want[1] || names[2] != want[2] || names[3] != want[3] {
		t.Fatalf("names = %v, want %v", names, want)
	}
	sub := decode[DirListing](t, e.ok("GET", "/api/fs/dirs?path="+resolved+"/alpha", nil, 200))
	if len(sub.Dirs) != 1 || sub.Dirs[0].Name != "inner" || sub.Parent != resolved {
		t.Fatalf("sub listing = %+v", sub)
	}

	// Outside every root, including through a symlink, is a 400 naming browse_roots.
	e.field("GET", "/api/fs/dirs?path="+filepath.Dir(resolved), nil, "browse_roots")
	e.field("GET", "/api/fs/dirs?path="+resolved+"/escape", nil, "browse_roots")
	e.field("GET", "/api/fs/dirs?path=relative/dir", nil, "path")
	if code, _ := e.call(nil, "GET", "/api/fs/dirs?path="+resolved+"/nope", nil); code != 404 {
		t.Fatalf("missing directory = %d, want 404", code)
	}
	e.field("PUT", "/api/settings", map[string]any{"browse_roots": []string{"rel"}}, "browse_roots")
}

func TestSettings_VerifySlotsResizeTheLaneOnSave(t *testing.T) {
	e := newEnv(t, envOpt{})
	lane, ok := e.eng.Lane(engine.VerifyLane)
	if !ok {
		t.Fatal("no verify lane")
	}
	e.ok("PUT", "/api/settings", map[string]any{"verify_slots": 5}, 200)
	if st, err := lane.Settings(); err != nil || st.Width != 5 {
		t.Fatalf("verify lane width = %d (%v), want 5 without a restart", st.Width, err)
	}
}

func TestBackendKey_IsWriteOnly(t *testing.T) {
	e := newEnv(t, envOpt{})
	b := backendReq("hosted")
	b["api_key"] = "sk-secret-value"
	out := e.ok("POST", "/api/backends", b, 201)
	if !decode[backendResult](t, out).Backend.HasKey || strings.Contains(string(out), "sk-secret-value") {
		t.Fatalf("create response: %s", out)
	}
	if got, err := e.st.BackendKey(t.Context(), "hosted"); err != nil || got != "sk-secret-value" {
		t.Fatalf("stored key = %q (%v)", got, err)
	}
	// Absent keeps it; empty clears it.
	e.ok("PATCH", "/api/backends/hosted", map[string]any{"slots": 3}, 200)
	if got, _ := e.st.BackendKey(t.Context(), "hosted"); got != "sk-secret-value" {
		t.Fatalf("key after an unrelated patch = %q", got)
	}
	out = e.ok("PATCH", "/api/backends/hosted", map[string]any{"api_key": ""}, 200)
	if decode[backendResult](t, out).Backend.HasKey {
		t.Fatalf("has_key after clear: %s", out)
	}
	e.field("PATCH", "/api/backends/hosted", map[string]any{"api_key": " padded "}, "api_key")
	if list := e.ok("GET", "/api/backends", nil, 200); strings.Contains(string(list), "api_key") {
		t.Fatalf("list exposes api_key: %s", list)
	}
}
