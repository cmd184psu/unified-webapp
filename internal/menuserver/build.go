package menuserver

import (
	"net/http"
	"os"
	"path/filepath"

	"cmd184psu/unified-webapp/internal/platform/config"
)

// Build returns a ready-to-use http.Handler for the menuserver module.
func Build(cfg config.MenuserverConfig) (http.Handler, error) {
	store, err := NewStore(cfg.DataDir)
	if err != nil {
		return nil, err
	}
	h := NewHandler(store, cfg)

	mux := http.NewServeMux()
	h.Register(mux)
	mux.Handle("/", &staticHandler{dir: cfg.StaticDir})

	return mux, nil
}

// staticHandler serves files from dir with an index.html fallback.
type staticHandler struct {
	dir string
}

func (sh *staticHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := filepath.Join(sh.dir, filepath.Clean("/"+r.URL.Path))
	if _, err := os.Stat(path); os.IsNotExist(err) {
		http.ServeFile(w, r, filepath.Join(sh.dir, "index.html"))
		return
	}
	http.ServeFile(w, r, path)
}
