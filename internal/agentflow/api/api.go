// Package api is agentflow's HTTP surface. It sits on the store and the engine
// and holds no state of its own beyond the cached opencode probe. Routes are
// registered under /api/; the composition root mounts the static UI at /.
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"cmd184psu/unified-webapp/internal/agentflow/engine"
	"cmd184psu/unified-webapp/internal/agentflow/store"
)

// Deps are the collaborators of the API.
type Deps struct {
	Store  *store.Store
	Engine *engine.Engine
	// Version reports the agent program's version (OC1). An error is not fatal:
	// it is surfaced by /api/health so the operator sees why runs cannot start.
	Version func(ctx context.Context) (string, error)
	// SSEMaxSubscribers caps the event stream's subscribers (D3).
	SSEMaxSubscribers int
}

// API serves the agentflow routes.
type API struct {
	deps Deps

	mu sync.Mutex
	oc OpenCodeStatus
}

// OpenCodeStatus is the result of the last opencode version probe.
type OpenCodeStatus struct {
	Status  string `json:"status"` // "ok" or "unavailable"
	Version string `json:"version,omitempty"`
	Error   string `json:"error,omitempty"`
}

const probeTimeout = 10 * time.Second

// New builds the API and probes opencode once (OC1).
func New(d Deps) *API {
	a := &API{deps: d}
	a.probe(context.Background())
	return a
}

// probe asks the runner for its version and caches the answer. A missing or
// broken opencode is a status, not an error: agentflow still serves its data.
func (a *API) probe(ctx context.Context) OpenCodeStatus {
	st := OpenCodeStatus{Status: "unavailable", Error: "no runner"}
	if a.deps.Version != nil {
		ctx, cancel := context.WithTimeout(ctx, probeTimeout)
		defer cancel()
		if v, err := a.deps.Version(ctx); err != nil {
			st.Error = err.Error()
		} else {
			st = OpenCodeStatus{Status: "ok", Version: v}
		}
	}
	a.mu.Lock()
	a.oc = st
	a.mu.Unlock()
	return st
}

// openCode returns the cached probe result.
func (a *API) openCode() OpenCodeStatus {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.oc
}

// Register adds the routes to mux. Every path under /api/ that no route claims
// answers a JSON 404 instead of falling through to the static handler.
func (a *API) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/health", a.health)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	})
}

func (a *API) health(w http.ResponseWriter, r *http.Request) {
	oc := a.openCode()
	status := "ok"
	if oc.Status != "ok" {
		status = "degraded"
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": status, "opencode": oc})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
