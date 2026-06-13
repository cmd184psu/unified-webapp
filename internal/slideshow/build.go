package slideshow

import (
	"net/http"
	"os"
	"path/filepath"

	"cmd184psu/unified-webapp/internal/platform/config"
)

// Build returns a ready-to-use http.Handler for the slideshow module.
func Build(cfg config.SlideshowConfig) (http.Handler, error) {
	store, err := NewStore(cfg.ImageDir, cfg.AgeCutoffDays)
	if err != nil {
		return nil, err
	}
	music := NewMusicStore(cfg.Music.AudioDir)
	broker := NewSSEBroker()
	conductor := NewConductor(store, music, broker, cfg)
	h := NewHandler(store, conductor, broker, music, cfg)

	mux := http.NewServeMux()
	h.Register(mux)
	mux.Handle("/", &staticHandler{dir: cfg.StaticDir})

	go conductor.Run()

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
