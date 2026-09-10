package multissh

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"cmd184psu/unified-webapp/internal/multissh/sshproxy"
)

type transferCall struct {
	Host       string
	LocalPath  string
	RemotePath string
}

type fakeTransferrer struct {
	mu        sync.Mutex
	errorsBy  map[string]error
	transfers []transferCall
}

func (f *fakeTransferrer) Transfer(ctx context.Context, p sshproxy.ConnectParams, localPath, remotePath string, progress func(bytes int64)) error {
	_ = remotePath
	data, err := os.ReadFile(localPath)
	if err != nil {
		return err
	}
	half := int64(len(data) / 2)
	if half == 0 {
		half = int64(len(data))
	}
	progress(half)
	time.Sleep(15 * time.Millisecond)
	progress(int64(len(data)))

	f.mu.Lock()
	f.transfers = append(f.transfers, transferCall{Host: p.Host, LocalPath: localPath, RemotePath: remotePath})
	err = f.errorsBy[p.Host]
	f.mu.Unlock()
	if err != nil {
		return err
	}
	return nil
}

func (f *fakeTransferrer) callsByHost() map[string]transferCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make(map[string]transferCall, len(f.transfers))
	for _, call := range f.transfers {
		out[call.Host] = call
	}
	return out
}

func TestBroadcast_ProgressFramesAndCompleteWithIsolatedErrors(t *testing.T) {
	sshDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(sshDir, "id1"), []byte("KEY1"), 0o600); err != nil {
		t.Fatalf("write key1: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sshDir, "id2"), []byte("KEY2"), 0o600); err != nil {
		t.Fatalf("write key2: %v", err)
	}

	uploadDir := t.TempDir()
	fake := &fakeTransferrer{errorsBy: map[string]error{"bad.example": fmt.Errorf("boom")}}
	srv := New(Options{
		SSHKeyDir:      sshDir,
		UploadDir:      uploadDir,
		MaxUploadBytes: 1024,
		MaxSessions:    3,
		Transferrer:    fake,
	})

	uploadID := stageUploadForBroadcastTest(t, srv, "bundle.tar", []byte("12345678"))
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	reqBody := map[string]any{
		"uploadId": uploadID,
		"targets": []map[string]any{
			{"host": "ok.example", "port": 22, "user": "u", "key": "id1", "remoteDir": "/opt/a"},
			{"host": "bad.example", "port": 22, "user": "u", "key": "id2", "remoteDir": "/opt/b"},
		},
	}
	jobID := startBroadcastJob(t, httpSrv.URL, reqBody)

	wsURL := "ws" + strings.TrimPrefix(httpSrv.URL, "http") + "/api/broadcast/ws?job=" + jobID
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("ws dial: %v", err)
	}
	defer ws.Close()

	progressByHost := map[string][]broadcastProgressFrame{}
	completeSeen := false
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		_ = ws.SetReadDeadline(time.Now().Add(1 * time.Second))
		_, data, err := ws.ReadMessage()
		if err != nil {
			t.Fatalf("read ws frame: %v", err)
		}
		var envelope struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(data, &envelope); err != nil {
			t.Fatalf("decode envelope: %v", err)
		}
		if envelope.Type == "complete" {
			completeSeen = true
			break
		}
		var frame broadcastProgressFrame
		if err := json.Unmarshal(data, &frame); err != nil {
			t.Fatalf("decode progress: %v", err)
		}
		progressByHost[frame.Host] = append(progressByHost[frame.Host], frame)
	}

	if !completeSeen {
		t.Fatalf("expected complete frame")
	}
	if len(progressByHost["ok.example"]) == 0 || len(progressByHost["bad.example"]) == 0 {
		t.Fatalf("expected progress for both targets, got %#v", progressByHost)
	}
	if !hasState(progressByHost["ok.example"], "done") {
		t.Fatalf("expected done state for ok.example, got %#v", progressByHost["ok.example"])
	}
	if !hasState(progressByHost["bad.example"], "error") {
		t.Fatalf("expected error state for bad.example, got %#v", progressByHost["bad.example"])
	}

	calls := fake.callsByHost()
	if calls["ok.example"].RemotePath != "/opt/a/bundle.tar" {
		t.Fatalf("ok remote path = %q", calls["ok.example"].RemotePath)
	}
	if calls["bad.example"].RemotePath != "/opt/b/bundle.tar" {
		t.Fatalf("bad remote path = %q", calls["bad.example"].RemotePath)
	}
}

func TestBroadcast_RejectsMoreTargetsThanMaxSessions(t *testing.T) {
	sshDir := t.TempDir()
	for i := 0; i < 4; i++ {
		if err := os.WriteFile(filepath.Join(sshDir, fmt.Sprintf("id%d", i)), []byte("KEY"), 0o600); err != nil {
			t.Fatalf("write key: %v", err)
		}
	}
	srv := New(Options{SSHKeyDir: sshDir, UploadDir: t.TempDir(), MaxUploadBytes: 1024, MaxSessions: 3, Transferrer: &fakeTransferrer{}})
	uploadID := stageUploadForBroadcastTest(t, srv, "bundle.tar", []byte("abc"))
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	targets := make([]map[string]any, 0, 4)
	for i := 0; i < 4; i++ {
		targets = append(targets, map[string]any{"host": fmt.Sprintf("h%d", i), "port": 22, "user": "u", "key": fmt.Sprintf("id%d", i), "remoteDir": "/tmp"})
	}
	body := map[string]any{"uploadId": uploadID, "targets": targets}

	res := postBroadcast(t, httpSrv.URL, body)
	defer res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.StatusCode)
	}
}

func TestBroadcast_RejectsUnknownUploadID(t *testing.T) {
	srv := New(Options{SSHKeyDir: t.TempDir(), UploadDir: t.TempDir(), MaxUploadBytes: 1024, MaxSessions: 3, Transferrer: &fakeTransferrer{}})
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	body := map[string]any{"uploadId": "missing", "targets": []map[string]any{{"host": "h", "port": 22, "user": "u", "key": "id", "remoteDir": "/tmp"}}}
	res := postBroadcast(t, httpSrv.URL, body)
	defer res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.StatusCode)
	}
}

func TestBroadcast_RejectsWhenNeitherUploadIDNorFilePathSpecified(t *testing.T) {
	sshDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(sshDir, "id1"), []byte("KEY1"), 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	srv := New(Options{SSHKeyDir: sshDir, UploadDir: t.TempDir(), MaxUploadBytes: 1024, MaxSessions: 3, Transferrer: &fakeTransferrer{}})
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	body := map[string]any{"targets": []map[string]any{{"host": "h", "port": 22, "user": "u", "key": "id1", "remoteDir": "/tmp"}}}
	res := postBroadcast(t, httpSrv.URL, body)
	defer res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.StatusCode)
	}
}

func TestBroadcast_RejectsWhenBothUploadIDAndFilePathSpecified(t *testing.T) {
	sshDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(sshDir, "id1"), []byte("KEY1"), 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	browseRoot := t.TempDir()
	filePath := filepath.Join(browseRoot, "from-browse.bin")
	if err := os.WriteFile(filePath, []byte("abc"), 0o600); err != nil {
		t.Fatalf("write browse file: %v", err)
	}
	srv := New(Options{SSHKeyDir: sshDir, UploadDir: t.TempDir(), BrowseRoot: browseRoot, MaxUploadBytes: 1024, MaxSessions: 3, Transferrer: &fakeTransferrer{}})
	uploadID := stageUploadForBroadcastTest(t, srv, "bundle.tar", []byte("abc"))
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	body := map[string]any{"uploadId": uploadID, "filePath": "from-browse.bin", "targets": []map[string]any{{"host": "h", "port": 22, "user": "u", "key": "id1", "remoteDir": "/tmp"}}}
	res := postBroadcast(t, httpSrv.URL, body)
	defer res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.StatusCode)
	}
}

func TestBroadcast_FilePathSourceUsesBrowseRootAndTargetRemoteDirs(t *testing.T) {
	sshDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(sshDir, "id1"), []byte("KEY1"), 0o600); err != nil {
		t.Fatalf("write key1: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sshDir, "id2"), []byte("KEY2"), 0o600); err != nil {
		t.Fatalf("write key2: %v", err)
	}
	browseRoot := t.TempDir()
	localFile := filepath.Join(browseRoot, "payload.bin")
	if err := os.WriteFile(localFile, []byte("abcdef"), 0o600); err != nil {
		t.Fatalf("write browse file: %v", err)
	}

	fake := &fakeTransferrer{}
	srv := New(Options{
		SSHKeyDir:      sshDir,
		UploadDir:      t.TempDir(),
		BrowseRoot:     browseRoot,
		MaxUploadBytes: 1024,
		MaxSessions:    3,
		Transferrer:    fake,
	})
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	jobID := startBroadcastJob(t, httpSrv.URL, map[string]any{
		"filePath": "payload.bin",
		"targets": []map[string]any{
			{"host": "h1", "port": 22, "user": "u", "key": "id1", "remoteDir": "/var/tmp"},
			{"host": "h2", "port": 22, "user": "u", "key": "id2", "remoteDir": ""},
		},
	})
	waitBroadcastComplete(t, httpSrv.URL, jobID)

	calls := fake.callsByHost()
	if calls["h1"].LocalPath != localFile {
		t.Fatalf("h1 local path = %q, want %q", calls["h1"].LocalPath, localFile)
	}
	if calls["h1"].RemotePath != "/var/tmp/payload.bin" {
		t.Fatalf("h1 remote path = %q", calls["h1"].RemotePath)
	}
	if calls["h2"].RemotePath != "/tmp/payload.bin" {
		t.Fatalf("h2 remote path = %q", calls["h2"].RemotePath)
	}
}

func stageUploadForBroadcastTest(t *testing.T, s *Server, name string, data []byte) string {
	t.Helper()
	id, err := randomHexID(16)
	if err != nil {
		t.Fatalf("random id: %v", err)
	}
	path := filepath.Join(s.uploads.uploadDir, id+"-"+name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write staged upload: %v", err)
	}
	s.uploads.add(uploadMeta{ID: id, Name: name, Size: int64(len(data)), Path: path})
	return id
}

func startBroadcastJob(t *testing.T, baseURL string, body map[string]any) string {
	t.Helper()
	res := postBroadcast(t, baseURL, body)
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.StatusCode)
	}
	var got struct {
		JobID string `json:"jobId"`
	}
	if err := json.NewDecoder(res.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.JobID == "" {
		t.Fatalf("missing job id")
	}
	return got.JobID
}

func waitBroadcastComplete(t *testing.T, baseURL, jobID string) {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(baseURL, "http") + "/api/broadcast/ws?job=" + jobID
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("ws dial: %v", err)
	}
	defer ws.Close()

	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		_ = ws.SetReadDeadline(time.Now().Add(1 * time.Second))
		_, data, err := ws.ReadMessage()
		if err != nil {
			t.Fatalf("read ws frame: %v", err)
		}
		var envelope struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(data, &envelope); err != nil {
			t.Fatalf("decode envelope: %v", err)
		}
		if envelope.Type == "complete" {
			return
		}
	}
	t.Fatalf("timed out waiting for complete frame")
}

func postBroadcast(t *testing.T, baseURL string, body map[string]any) *http.Response {
	t.Helper()
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, baseURL+"/api/broadcast", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	return res
}

func hasState(frames []broadcastProgressFrame, state string) bool {
	for _, frame := range frames {
		if frame.State == state {
			return true
		}
	}
	return false
}
