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
	"cmd184psu/unified-webapp/internal/agentflow/gitsvc"
	"cmd184psu/unified-webapp/internal/agentflow/store"
	"cmd184psu/unified-webapp/internal/platform/broker"
)

// Deps are the collaborators of the API.
type Deps struct {
	Store  *store.Store
	Engine *engine.Engine
	// Git offers git init for a directory that is not a repository (PJ1).
	Git *gitsvc.Service
	// Version reports the agent program's version (OC1). An error is not fatal:
	// it is surfaced by /api/health so the operator sees why runs cannot start.
	Version func(ctx context.Context) (string, error)
	// SSEMaxSubscribers caps the event stream's subscribers (D3).
	SSEMaxSubscribers int
}

// API serves the agentflow routes.
type API struct {
	deps Deps
	// bus carries the event stream (RC5, UI3).
	bus *broker.Broker

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
	a := &API{deps: d, bus: broker.NewBroker(0)}
	a.bus.SetMaxSubscribers(d.SSEMaxSubscribers)
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
	a.registerProjects(mux)
	a.registerBackends(mux)
	a.registerSettings(mux)
	a.registerStats(mux)
	a.registerEfforts(mux)
	a.registerDocs(mux)
	a.registerQueue(mux)
	a.registerTasks(mux)
	a.registerRun(mux)
	a.registerEvents(mux)
	a.registerExport(mux)
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

// emit pushes a state-change notice to every event stream subscriber. fields
// ride along with the type; a failed marshal drops the notice, which is only
// a hint to refetch.
func (a *API) emit(typ string, fields map[string]any) {
	msg := map[string]any{"type": typ}
	for k, v := range fields {
		msg[k] = v
	}
	if b, err := json.Marshal(msg); err == nil {
		a.bus.Publish(string(b))
	}
}

// PublishRun pushes a run-state transition to the event stream (RC5). The
// composition root hands it to the engine as Deps.PublishRun.
func (a *API) PublishRun(ev engine.RunEvent) {
	a.emit("project.run_state", map[string]any{
		"project": ev.Project, "effort": ev.Effort, "state": ev.State, "action": ev.Action, "actor": ev.Actor,
	})
}

// Notify tells event stream subscribers that engine-driven state (a stage
// ended, an effort moved on) may have changed, so they refetch what they show.
func (a *API) Notify() { a.emit("state.changed", nil) }
