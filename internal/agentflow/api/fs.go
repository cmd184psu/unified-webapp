package api

import (
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func (a *API) registerFS(mux muxer) {
	mux.HandleFunc("GET /api/fs/dirs", a.listDirs)
}

// DirEntry is one subdirectory in a directory listing.
type DirEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Git  bool   `json:"git"`
}

// DirListing is what the directory picker shows: the resolved directory, its
// parent (empty at a browse root's filesystem root) and its subdirectories.
type DirListing struct {
	Path   string     `json:"path"`
	Parent string     `json:"parent"`
	Dirs   []DirEntry `json:"dirs"`
}

// listDirs lists the subdirectories of ?path. By default any folder may be
// browsed; saved browse_roots narrow it (read now, so a saved change applies
// to the next call). An empty path starts at the first root, or at the home
// directory when browsing is unrestricted.
func (a *API) listDirs(w http.ResponseWriter, r *http.Request) {
	set, err := a.deps.Store.GetSettings(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	var roots []string
	for _, root := range set.BrowseRoots {
		if resolved, err := filepath.EvalSymlinks(root); err == nil {
			roots = append(roots, resolved)
		}
	}
	if len(roots) == 0 {
		badRequest(w, "browse_roots", "none of the browse roots exists")
		return
	}
	path := r.URL.Query().Get("path")
	if path == "" {
		path = roots[0]
		// Unrestricted browsing starts at the server user's home, not at "/".
		if home, err := os.UserHomeDir(); err == nil && path == "/" {
			path = home
		}
	}
	if !filepath.IsAbs(path) {
		badRequest(w, "path", "must be an absolute path")
		return
	}
	dir, err := filepath.EvalSymlinks(filepath.Clean(path))
	if errors.Is(err, fs.ErrNotExist) {
		writeJSON(w, http.StatusNotFound, errorBody{Error: "directory does not exist", Field: "path"})
		return
	}
	if err != nil {
		badRequest(w, "path", "cannot read this directory")
		return
	}
	if !withinAny(roots, dir) {
		badRequest(w, "browse_roots", "path is outside the browse roots")
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		badRequest(w, "path", "cannot read this directory")
		return
	}
	out := DirListing{Path: dir, Dirs: []DirEntry{}}
	// The parent is offered only while it is still inside a browse root, so
	// the picker's Up control never leads outside them.
	if parent := filepath.Dir(dir); parent != dir && withinAny(roots, parent) {
		out.Parent = parent
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		p := filepath.Join(dir, e.Name())
		// A symlink to a directory is listed as a directory.
		if fi, err := os.Stat(p); err != nil || !fi.IsDir() {
			continue
		}
		_, gitErr := os.Lstat(filepath.Join(p, ".git"))
		out.Dirs = append(out.Dirs, DirEntry{Name: e.Name(), Path: p, Git: gitErr == nil})
	}
	sort.Slice(out.Dirs, func(i, j int) bool { return out.Dirs[i].Name < out.Dirs[j].Name })
	writeJSON(w, http.StatusOK, out)
}

// withinAny reports whether dir is one of roots or lies below one.
func withinAny(roots []string, dir string) bool {
	for _, root := range roots {
		if rel, err := filepath.Rel(root, dir); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return true
		}
	}
	return false
}
