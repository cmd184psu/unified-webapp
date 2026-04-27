package grocery

import (
	"net/http"
	"os"
	"path/filepath"

	"cmd184psu/unified-webapp/internal/platform/broker"
	"cmd184psu/unified-webapp/internal/platform/config"
)

// Build returns a ready-to-use http.Handler for the grocery module.
// The caller is responsible for wrapping it with middleware (e.g. CORS).
func Build(cfg config.GroceryConfig) (http.Handler, error) {
	if err := os.MkdirAll(filepath.Dir(cfg.DataFile), 0755); err != nil {
		return nil, err
	}
	s, err := New(cfg.DataFile)
	if err != nil {
		return nil, err
	}

	groups := cfg.Groups
	if len(groups) == 0 {
		groups = s.Groups()
	}
	if s.Title() == "" && cfg.Title != "" {
		_ = s.SetTitle(cfg.Title)
	}

	b := broker.NewBroker(cfg.SyncIntervalSeconds * 1000)
	h := NewHandler(s, groups, cfg.Progress, cfg.SyncIntervalSeconds, cfg.Title, b)

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
