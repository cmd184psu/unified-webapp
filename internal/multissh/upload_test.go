package multissh

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestUpload_StreamedUploadStoresFileAndMetadata(t *testing.T) {
	uploadDir := t.TempDir()
	srv := New(Options{UploadDir: uploadDir, MaxUploadBytes: 1024})
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	content := []byte("hello large upload")
	body, contentType := multipartBody(t, "file", "archive.tar", content)

	req, err := http.NewRequest(http.MethodPost, httpSrv.URL+"/api/upload", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", contentType)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(res.Body)
		t.Fatalf("expected 200, got %d body=%s", res.StatusCode, b)
	}

	var got struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Size int64  `json:"size"`
	}
	if err := json.NewDecoder(res.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.ID == "" || got.Name != "archive.tar" || got.Size != int64(len(content)) {
		t.Fatalf("unexpected response: %+v", got)
	}

	meta, ok := srv.uploads.get(got.ID)
	if !ok {
		t.Fatalf("missing stored upload %q", got.ID)
	}
	onDisk, err := os.ReadFile(meta.Path)
	if err != nil {
		t.Fatalf("read staged file: %v", err)
	}
	if !bytes.Equal(onDisk, content) {
		t.Fatalf("staged bytes mismatch: got=%q want=%q", string(onDisk), string(content))
	}
}

func TestUpload_OversizeReturns413AndDoesNotKeepPartialFile(t *testing.T) {
	uploadDir := t.TempDir()
	srv := New(Options{UploadDir: uploadDir, MaxUploadBytes: 8})
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	content := []byte("123456789")
	body, contentType := multipartBody(t, "file", "big.bin", content)

	req, err := http.NewRequest(http.MethodPost, httpSrv.URL+"/api/upload", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", contentType)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusRequestEntityTooLarge {
		b, _ := io.ReadAll(res.Body)
		t.Fatalf("expected 413, got %d body=%s", res.StatusCode, b)
	}
	entries, err := os.ReadDir(uploadDir)
	if err != nil {
		t.Fatalf("read upload dir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected no staged files, got %d", len(entries))
	}
}

func TestUpload_PathTraversalFilenameIsSanitizedToBaseName(t *testing.T) {
	uploadDir := t.TempDir()
	srv := New(Options{UploadDir: uploadDir, MaxUploadBytes: 1024})
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	body, contentType := multipartBody(t, "file", "../secret", []byte("x"))

	req, err := http.NewRequest(http.MethodPost, httpSrv.URL+"/api/upload", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", contentType)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(res.Body)
		t.Fatalf("expected 200, got %d body=%s", res.StatusCode, b)
	}
	var got struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(res.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Name != "secret" {
		t.Fatalf("expected sanitized basename 'secret', got %q", got.Name)
	}
}

func multipartBody(t *testing.T, field, fileName string, data []byte) ([]byte, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	part, err := mw.CreateFormFile(field, fileName)
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatalf("write form file: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}
	return buf.Bytes(), mw.FormDataContentType()
}

func TestUpload_DeleteRemovesStagedFile(t *testing.T) {
	uploadDir := t.TempDir()
	srv := New(Options{UploadDir: uploadDir, MaxUploadBytes: 1024})
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	content := []byte("delete me")
	body, contentType := multipartBody(t, "file", "del.bin", content)
	req, err := http.NewRequest(http.MethodPost, httpSrv.URL+"/api/upload", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", contentType)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.StatusCode)
	}
	var up struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(res.Body).Decode(&up); err != nil {
		t.Fatalf("decode: %v", err)
	}
	meta, ok := srv.uploads.get(up.ID)
	if !ok {
		t.Fatalf("missing upload")
	}

	delReq, err := http.NewRequest(http.MethodDelete, httpSrv.URL+"/api/uploads/"+up.ID, nil)
	if err != nil {
		t.Fatalf("delete req: %v", err)
	}
	delRes, err := http.DefaultClient.Do(delReq)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	defer delRes.Body.Close()
	if delRes.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", delRes.StatusCode)
	}
	if _, err := os.Stat(meta.Path); !os.IsNotExist(err) {
		t.Fatalf("expected staged file removed, stat err=%v", err)
	}
	entries, err := os.ReadDir(filepath.Clean(uploadDir))
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected empty upload dir, got %d entries", len(entries))
	}
}
