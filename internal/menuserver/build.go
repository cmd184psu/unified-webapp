package menuserver

import (
	"net/http"

	"cmd184psu/unified-webapp/internal/platform/config"
	"cmd184psu/unified-webapp/internal/platform/static"
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
	mux.Handle("/", static.NewHandler(cfg.StaticDir))

	return mux, nil
}
