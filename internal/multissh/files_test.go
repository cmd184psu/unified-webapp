package multissh

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

func TestFilesGet_ListsEntriesWithSizesAndDirsFirstSort(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "z-dir"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Mkdir(filepath.Join(root, "a-dir"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "b.txt"), []byte("bbbb"), 0o600); err != nil {
		t.Fatalf("write b.txt: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("a"), 0o600); err != nil {
		t.Fatalf("write a.txt: %v", err)
	}

	srv := New(Options{UploadDir: t.TempDir(), MaxUploadBytes: 1024, BrowseRoot: root})
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	res, err := http.Get(httpSrv.URL + "/api/files")
	if err != nil {
		t.Fatalf("get files: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", res.StatusCode)
	}
	var body struct {
		Path    string `json:"path"`
		Entries []struct {
			Name  string `json:"name"`
			IsDir bool   `json:"isDir"`
			Size  int64  `json:"size"`
		} `json:"entries"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Path != "" {
		t.Fatalf("path = %q, want empty", body.Path)
	}
	wantOrder := []string{"a-dir", "z-dir", "a.txt", "b.txt"}
	if len(body.Entries) != len(wantOrder) {
		t.Fatalf("entries = %#v", body.Entries)
	}
	for i, want := range wantOrder {
		if body.Entries[i].Name != want {
			t.Fatalf("entry[%d] = %q, want %q", i, body.Entries[i].Name, want)
		}
	}
	if !body.Entries[0].IsDir || body.Entries[0].Size != 0 {
		t.Fatalf("dir entry mismatch: %#v", body.Entries[0])
	}
	if body.Entries[2].IsDir || body.Entries[2].Size != 1 {
		t.Fatalf("file entry mismatch: %#v", body.Entries[2])
	}
	if body.Entries[3].IsDir || body.Entries[3].Size != 4 {
		t.Fatalf("file entry mismatch: %#v", body.Entries[3])
	}
}

func TestFilesGet_RejectsTraversalOutsideRoot(t *testing.T) {
	srv := New(Options{UploadDir: t.TempDir(), MaxUploadBytes: 1024, BrowseRoot: t.TempDir()})
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	res, err := http.Get(httpSrv.URL + "/api/files?path=" + url.QueryEscape("../x"))
	if err != nil {
		t.Fatalf("get files: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d", res.StatusCode)
	}
}

func TestFilesGet_RejectsPathThatIsNotDirectory(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "single.bin"), []byte("x"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	srv := New(Options{UploadDir: t.TempDir(), MaxUploadBytes: 1024, BrowseRoot: root})
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	res, err := http.Get(httpSrv.URL + "/api/files?path=" + url.QueryEscape("single.bin"))
	if err != nil {
		t.Fatalf("get files: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d", res.StatusCode)
	}
}

func TestFilesGet_MissingRootReturnsEmptyEntries(t *testing.T) {
	rootBase := t.TempDir()
	missingRoot := filepath.Join(rootBase, "missing-root")
	srv := New(Options{UploadDir: t.TempDir(), MaxUploadBytes: 1024, BrowseRoot: missingRoot})
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	res, err := http.Get(httpSrv.URL + "/api/files")
	if err != nil {
		t.Fatalf("get files: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", res.StatusCode)
	}
	var body struct {
		Path    string `json:"path"`
		Entries []any  `json:"entries"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Path != "" {
		t.Fatalf("path = %q", body.Path)
	}
	if len(body.Entries) != 0 {
		t.Fatalf("entries = %#v", body.Entries)
	}
}
