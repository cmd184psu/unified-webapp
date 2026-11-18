package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"cmd184psu/unified-webapp/internal/agentflow/engine"
	"cmd184psu/unified-webapp/internal/taskmaster/golane"
)

// eventName is the SSE event every message is sent under; the JSON data
// carries its own "type".
const eventName = "agentflow"

func (a *API) registerEvents(mux muxer) {
	mux.HandleFunc("GET /api/events", a.bus.ServeSSE(eventName, a.snapshot))
	mux.HandleFunc("GET /api/stages/{id}/stream", a.streamStage)
}

// snapshot is the first message of every event stream connection: each
// project's run state, so a joining client starts from the current state
// (RC5).
func (a *API) snapshot() string {
	runs, err := a.deps.Engine.Runs().List(context.Background())
	if err != nil {
		runs = []engine.ProjectRun{}
	}
	b, _ := json.Marshal(map[string]any{"type": "snapshot", "projects": runs})
	return string(b)
}

// streamStage proxies to the stage's lane job output stream (UI3). The lane
// is the stage's backend lane, or the engine's verify or merge lane. Taskmaster
// applies its own subscriber cap.
func (a *API) streamStage(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	st, err := a.deps.Store.GetStage(r.Context(), id)
	if err != nil {
		writeErr(w, err)
		return
	}
	if st.LaneJobID == "" {
		writeJSON(w, http.StatusConflict, errorBody{Error: "stage " + strconv.FormatInt(id, 10) + " has not started"})
		return
	}
	name := engine.BackendLane(st.Backend)
	switch st.Role {
	case "verify":
		name = engine.VerifyLane
	case "merge":
		name = engine.MergeLane
	}
	lane, ok := a.deps.Engine.Lane(name)
	if !ok {
		writeJSON(w, http.StatusNotFound, errorBody{Error: "lane " + name + " not found"})
		return
	}
	switch err := lane.StreamOutput(w, r, st.LaneJobID); {
	case err == nil:
		// StreamOutput wrote the stream, or its own 503 at the cap.
	case errors.Is(err, golane.ErrNotFound):
		writeJSON(w, http.StatusNotFound, errorBody{Error: "the stage's job is gone (retention)"})
	default:
		writeErr(w, err)
	}
}
