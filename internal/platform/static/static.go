// Package static provides a shared static-file handler used by the simple
// SPA modules (grocery, todo, menuserver, slideshow, obsidianoid). It serves
// files from a configured directory and falls back to index.html for SPA
// routing, with the requested path rooted under dir so it cannot escape it.
package static

import (
	"net/http"
	"os"
	"path/filepath"
)

// Handler serves files from dir with an index.html fallback for SPA routing.
type Handler struct {
	dir string
}

// NewHandler returns a Handler that serves files from dir.
func NewHandler(dir string) *Handler {
	return &Handler{dir: dir}
}

func (sh *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := filepath.Join(sh.dir, filepath.Clean("/"+r.URL.Path))
	if _, err := os.Stat(path); os.IsNotExist(err) {
		http.ServeFile(w, r, filepath.Join(sh.dir, "index.html"))
		return
	}
	http.ServeFile(w, r, path)
}
