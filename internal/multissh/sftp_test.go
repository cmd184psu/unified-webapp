package multissh

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"cmd184psu/unified-webapp/internal/multissh/sshproxy"
)

type fakeRemoteLister struct {
	path    string
	entries []sshproxy.RemoteEntry
	err     error
	seenDir string
	seen    sshproxy.ConnectParams
}

func (f *fakeRemoteLister) ListDir(ctx context.Context, p sshproxy.ConnectParams, dir string) (string, []sshproxy.RemoteEntry, error) {
	_ = ctx
	f.seen = p
	f.seenDir = dir
	if f.err != nil {
		return "", nil, f.err
	}
	return f.path, f.entries, nil
}

func TestSFTPListDir_ReturnsSuccessShape(t *testing.T) {
	sshDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(sshDir, "id1"), []byte("KEY"), 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	lister := &fakeRemoteLister{
		path: "/tmp",
		entries: []sshproxy.RemoteEntry{
			{Name: "dir", IsDir: true},
			{Name: "f.bin", IsDir: false},
		},
	}
	srv := New(Options{SSHKeyDir: sshDir, UploadDir: t.TempDir(), MaxUploadBytes: 1024, RemoteLister: lister})
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	payload := []byte(`{"host":"1.2.3.4","port":2200,"user":"root","key":"id1","path":"/tmp"}`)
	res, err := http.Post(httpSrv.URL+"/api/sftp/listdir", "application/json", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", res.StatusCode)
	}
	var body struct {
		Path    string                 `json:"path"`
		Entries []sshproxy.RemoteEntry `json:"entries"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Path != "/tmp" {
		t.Fatalf("path = %q", body.Path)
	}
	if len(body.Entries) != 2 {
		t.Fatalf("entries = %#v", body.Entries)
	}
	if lister.seen.Host != "1.2.3.4" || lister.seen.Port != 2200 || lister.seen.User != "root" {
		t.Fatalf("seen params = %#v", lister.seen)
	}
	if lister.seenDir != "/tmp" {
		t.Fatalf("seen dir = %q", lister.seenDir)
	}
}

func TestSFTPListDir_RejectsBadTarget(t *testing.T) {
	srv := New(Options{SSHKeyDir: t.TempDir(), UploadDir: t.TempDir(), MaxUploadBytes: 1024, RemoteLister: &fakeRemoteLister{}})
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	payload := []byte(`{"host":"","port":22,"user":"","key":"id1","path":"/tmp"}`)
	res, err := http.Post(httpSrv.URL+"/api/sftp/listdir", "application/json", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d", res.StatusCode)
	}
}

func TestSFTPListDir_RejectsUnresolvableKey(t *testing.T) {
	sshDir := t.TempDir()
	srv := New(Options{SSHKeyDir: sshDir, UploadDir: t.TempDir(), MaxUploadBytes: 1024, RemoteLister: &fakeRemoteLister{}})
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	payload := []byte(`{"host":"1.2.3.4","port":22,"user":"root","key":"missing","path":"/tmp"}`)
	res, err := http.Post(httpSrv.URL+"/api/sftp/listdir", "application/json", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d", res.StatusCode)
	}
}
