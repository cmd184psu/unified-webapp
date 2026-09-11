package multissh

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"cmd184psu/unified-webapp/internal/platform/response"
)

type uploadMeta struct {
	ID   string
	Name string
	Size int64
	Path string
}

type uploadRegistry struct {
	mu        sync.Mutex
	uploads   map[string]uploadMeta
	uploadDir string
	maxBytes  int64
}

type uploadSummary struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Size int64  `json:"size"`
}

var errUploadTooLarge = errors.New("upload too large")

func newUploadRegistry(uploadDir string, maxBytes int64) (*uploadRegistry, error) {
	if strings.TrimSpace(uploadDir) == "" {
		return nil, fmt.Errorf("upload dir is required")
	}
	if maxBytes <= 0 {
		return nil, fmt.Errorf("max bytes must be > 0")
	}
	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		return nil, fmt.Errorf("create upload dir: %w", err)
	}
	return &uploadRegistry{uploads: make(map[string]uploadMeta), uploadDir: uploadDir, maxBytes: maxBytes}, nil
}

func (u *uploadRegistry) add(meta uploadMeta) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.uploads[meta.ID] = meta
}

func (u *uploadRegistry) get(id string) (uploadMeta, bool) {
	u.mu.Lock()
	defer u.mu.Unlock()
	meta, ok := u.uploads[id]
	return meta, ok
}

func (u *uploadRegistry) list() []uploadSummary {
	u.mu.Lock()
	defer u.mu.Unlock()
	out := make([]uploadSummary, 0, len(u.uploads))
	for _, meta := range u.uploads {
		out = append(out, uploadSummary{ID: meta.ID, Name: meta.Name, Size: meta.Size})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (u *uploadRegistry) remove(id string) (uploadMeta, bool) {
	u.mu.Lock()
	defer u.mu.Unlock()
	meta, ok := u.uploads[id]
	if !ok {
		return uploadMeta{}, false
	}
	delete(u.uploads, id)
	return meta, true
}

func (s *Server) handleUploadPost(w http.ResponseWriter, r *http.Request) {
	if s.uploads == nil {
		response.WriteError(w, http.StatusInternalServerError, "upload storage unavailable")
		return
	}
	mr, err := r.MultipartReader()
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, "invalid multipart request")
		return
	}

	part, err := nextUploadPart(mr)
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if part == nil {
		response.WriteError(w, http.StatusBadRequest, "missing file part")
		return
	}
	defer part.Close()

	name, err := sanitizeUploadName(part.FileName())
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, "invalid file name")
		return
	}
	id, err := randomHexID(16)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "unable to stage upload")
		return
	}
	stagedPath := filepath.Join(s.uploads.uploadDir, id+"-"+name)
	f, err := os.OpenFile(stagedPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "unable to stage upload")
		return
	}

	cw := &maxBytesWriter{w: f, maxBytes: s.uploads.maxBytes}
	_, copyErr := io.Copy(cw, part)
	closeErr := f.Close()
	if copyErr != nil || closeErr != nil {
		_ = os.Remove(stagedPath)
		if errors.Is(copyErr, errUploadTooLarge) {
			response.WriteError(w, http.StatusRequestEntityTooLarge, "upload exceeds maximum allowed size")
			return
		}
		response.WriteError(w, http.StatusInternalServerError, "unable to stage upload")
		return
	}

	meta := uploadMeta{ID: id, Name: name, Size: cw.written, Path: stagedPath}
	s.uploads.add(meta)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"id": meta.ID, "name": meta.Name, "size": meta.Size})
}

func (s *Server) handleUploadsGet(w http.ResponseWriter, r *http.Request) {
	if s.uploads == nil {
		response.WriteError(w, http.StatusInternalServerError, "upload storage unavailable")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"uploads": s.uploads.list()})
}

func (s *Server) handleUploadsDelete(w http.ResponseWriter, r *http.Request) {
	if s.uploads == nil {
		response.WriteError(w, http.StatusInternalServerError, "upload storage unavailable")
		return
	}
	id := r.PathValue("id")
	meta, ok := s.uploads.remove(id)
	if !ok {
		response.WriteError(w, http.StatusNotFound, "upload not found")
		return
	}
	if err := os.Remove(meta.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
		response.WriteError(w, http.StatusInternalServerError, "unable to remove upload")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func nextUploadPart(mr *multipart.Reader) (*multipart.Part, error) {
	for {
		part, err := mr.NextPart()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil, nil
			}
			return nil, fmt.Errorf("read multipart: %w", err)
		}
		if part.FormName() == "file" {
			return part, nil
		}
		_ = part.Close()
	}
}

func sanitizeUploadName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", errors.New("empty")
	}
	if name != filepath.Base(name) || strings.ContainsRune(name, '/') || strings.ContainsRune(name, filepath.Separator) {
		return "", errors.New("invalid")
	}
	if name == "." || name == ".." {
		return "", errors.New("invalid")
	}
	return filepath.Base(name), nil
}

func randomHexID(bytesLen int) (string, error) {
	b := make([]byte, bytesLen)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

type maxBytesWriter struct {
	w        io.Writer
	maxBytes int64
	written  int64
}

func (w *maxBytesWriter) Write(p []byte) (int, error) {
	if w.maxBytes <= 0 {
		return 0, errUploadTooLarge
	}
	remaining := w.maxBytes - w.written
	if remaining <= 0 {
		return 0, errUploadTooLarge
	}
	origLen := len(p)
	if int64(len(p)) > remaining {
		p = p[:remaining]
	}
	n, err := w.w.Write(p)
	w.written += int64(n)
	if err != nil {
		return n, err
	}
	if origLen > n {
		return n, errUploadTooLarge
	}
	return n, nil
}
