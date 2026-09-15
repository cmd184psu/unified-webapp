package timetracker

import (
	"net/http"
	"os"
	"path/filepath"

	"cmd184psu/unified-webapp/internal/platform/config"
	"cmd184psu/unified-webapp/internal/platform/static"
)

// Build returns a ready-to-use http.Handler for the timetracker module.
// The caller is responsible for wrapping it with middleware (e.g. CORS).
func Build(cfg config.TimetrackerConfig) (http.Handler, error) {
	if err := os.MkdirAll(filepath.Dir(cfg.DataFile), 0750); err != nil {
		return nil, err
	}
	s, err := New(cfg.DataFile)
	if err != nil {
		return nil, err
	}
	h := NewHandler(s)

	mux := http.NewServeMux()
	h.Register(mux)
	mux.Handle("/", static.NewHandler(cfg.StaticDir))

	return mux, nil
}
