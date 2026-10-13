package agentflow

import (
	"net/http"
	"os"
	"path/filepath"

	"cmd184psu/unified-webapp/internal/platform/config"
	"cmd184psu/unified-webapp/internal/platform/static"
)

// Build returns a ready-to-use http.Handler for the agentflow module.
// The caller is responsible for wrapping it with middleware (e.g. CORS).
func Build(cfg config.AgentflowConfig) (http.Handler, error) {
	if err := os.MkdirAll(filepath.Dir(cfg.ProjectPath), 0750); err != nil {
		return nil, err
	}

	s := NewStore(cfg.ProjectPath)
	e, err := NewEngine(cfg, s)
	if err != nil {
		return nil, err
	}

	h := NewHandler(s, e)

	mux := http.NewServeMux()
	h.Register(mux)
	mux.Handle("/", static.NewHandler(cfg.StaticDir))

	return mux, nil
}
