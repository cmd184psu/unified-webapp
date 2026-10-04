package admin

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"cmd184psu/unified-webapp/internal/platform/config"
)

func TestListFSShowsHiddenFilesFoldersFirst(t *testing.T) {
	_, mux, _ := newAdminTestHandler(t, config.AuthConfig{}, nil, false)
	dir := t.TempDir()
	for _, n := range []string{".secret.pin", "b.pin"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("1234\n"), 0o400); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "zdir"), 0o700); err != nil {
		t.Fatal(err)
	}
	rec := doAdmin(t, mux, http.MethodGet, "/api/config/fs?path="+url.QueryEscape(dir), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET = %d: %s", rec.Code, rec.Body.String())
	}
	var got struct {
		Path    string    `json:"path"`
		Entries []fsEntry `json:"entries"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range got.Entries {
		names = append(names, e.Name)
	}
	if len(names) != 3 || names[0] != "zdir" || names[1] != ".secret.pin" || names[2] != "b.pin" {
		t.Fatalf("entries = %v, want folder first, then .secret.pin, b.pin", names)
	}
	if !got.Entries[0].IsDir || got.Entries[1].Path != filepath.Join(dir, ".secret.pin") {
		t.Errorf("entries = %+v", got.Entries)
	}
}

func TestListFSRejectsRelativeAndMissingPaths(t *testing.T) {
	_, mux, _ := newAdminTestHandler(t, config.AuthConfig{}, nil, false)
	for _, p := range []string{"relative/dir", "/no/such/dir/at/all"} {
		rec := doAdmin(t, mux, http.MethodGet, "/api/config/fs?path="+url.QueryEscape(p), nil)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("path %q = %d, want 400", p, rec.Code)
		}
	}
}
