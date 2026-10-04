package admin

import (
	"net/http"
	"os"
	"path/filepath"
	"sort"

	"cmd184psu/unified-webapp/internal/platform/response"
)

// maxFSEntries caps one listing so a huge directory cannot flood the picker.
const maxFSEntries = 2000

type fsEntry struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	IsDir bool   `json:"isDir"`
	Size  int64  `json:"size"`
}

// handleListFS lists one directory of the server's filesystem for the console's
// file picker, so an admin points a module's pin file at the real file instead
// of typing a path. Names, kinds and sizes only; never file contents. Hidden
// files are included on purpose: key files are often dotfiles (~/.something.pin).
// The route is behind the admin session like every other admin API.
func (h *Handler) handleListFS(w http.ResponseWriter, r *http.Request) {
	dir := r.URL.Query().Get("path")
	if dir == "" {
		dir = string(filepath.Separator)
	}
	if !filepath.IsAbs(dir) {
		response.WriteError(w, http.StatusBadRequest, "path must be absolute")
		return
	}
	dir = filepath.Clean(dir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, "that folder cannot be opened")
		return
	}
	out := make([]fsEntry, 0, len(entries))
	for _, e := range entries {
		info, ierr := e.Info()
		isDir := e.IsDir()
		if e.Type()&os.ModeSymlink != 0 { // follow a link to see whether it is a folder
			if st, serr := os.Stat(filepath.Join(dir, e.Name())); serr == nil {
				isDir = st.IsDir()
			}
		}
		var size int64
		if ierr == nil && !isDir {
			size = info.Size()
		}
		out = append(out, fsEntry{Name: e.Name(), Path: filepath.Join(dir, e.Name()), IsDir: isDir, Size: size})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].IsDir != out[j].IsDir {
			return out[i].IsDir
		}
		return out[i].Name < out[j].Name
	})
	if len(out) > maxFSEntries {
		out = out[:maxFSEntries]
	}
	response.WriteJSON(w, http.StatusOK, map[string]any{"path": dir, "entries": out})
}
