package todo

import (
	"net/http"
	"os"
	"path/filepath"

	"cmd184psu/unified-webapp/internal/platform/broker"
	"cmd184psu/unified-webapp/internal/platform/config"
)

// Build returns a ready-to-use http.Handler for the todo module.
func Build(cfg config.TodoConfig) (http.Handler, error) {
	store, err := NewStore(cfg.DataDir)
	if err != nil {
		return nil, err
	}
	mbr := broker.NewMultiRoomBroker(cfg.SyncIntervalSeconds * 1000)
	h := NewHandler(store, mbr, cfg)

	mux := http.NewServeMux()
	h.Register(mux)
	mux.Handle("/", &staticHandler{dir: cfg.StaticDir})

	return mux, nil
}

// staticHandler serves files from dir with an index.html fallback for SPA routing.
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
