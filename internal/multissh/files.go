package multissh

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type fileBrowser struct {
	root string
}

type fileEntry struct {
	Name  string `json:"name"`
	IsDir bool   `json:"isDir"`
	Size  int64  `json:"size"`
}

func (b *fileBrowser) list(rel string) (string, []fileEntry, error) {
	absDir, relClean, err := resolveWithinRoot(b.root, rel)
	if err != nil {
		return "", nil, err
	}
	if _, err := os.Stat(b.root); err != nil {
		if os.IsNotExist(err) {
			return relClean, []fileEntry{}, nil
		}
		return "", nil, fmt.Errorf("read browse root: %w", err)
	}
	info, err := os.Stat(absDir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil, fmt.Errorf("path is not a directory")
		}
		return "", nil, fmt.Errorf("stat path: %w", err)
	}
	if !info.IsDir() {
		return "", nil, fmt.Errorf("path is not a directory")
	}
	entries, err := os.ReadDir(absDir)
	if err != nil {
		return "", nil, fmt.Errorf("read path: %w", err)
	}
	out := make([]fileEntry, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if name == "." || name == ".." {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return "", nil, fmt.Errorf("read entry info: %w", err)
		}
		if !entry.IsDir() && !info.Mode().IsRegular() {
			continue
		}
		size := info.Size()
		if entry.IsDir() {
			size = 0
		}
		out = append(out, fileEntry{Name: name, IsDir: entry.IsDir(), Size: size})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].IsDir != out[j].IsDir {
			return out[i].IsDir
		}
		return out[i].Name < out[j].Name
	})
	return relClean, out, nil
}

func resolveWithinRoot(root, rel string) (absDir string, relClean string, err error) {
	if strings.TrimSpace(root) == "" {
		return "", "", fmt.Errorf("browse root is required")
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", "", fmt.Errorf("resolve browse root: %w", err)
	}
	rel = strings.TrimSpace(rel)
	if rel == "" || rel == "." {
		return rootAbs, "", nil
	}
	joined := filepath.Join(rootAbs, rel)
	relPath, err := filepath.Rel(rootAbs, joined)
	if err != nil || relPath == ".." || strings.HasPrefix(relPath, ".."+string(filepath.Separator)) {
		return "", "", fmt.Errorf("path escapes browse root")
	}
	if relPath == "." {
		relPath = ""
	}
	return joined, relPath, nil
}

func (s *Server) handleFilesGet(w http.ResponseWriter, r *http.Request) {
	if s.files == nil {
		writeError(w, http.StatusInternalServerError, "file browser unavailable")
		return
	}
	relPath, entries, err := s.files.list(r.URL.Query().Get("path"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"path": relPath, "entries": entries})
}
