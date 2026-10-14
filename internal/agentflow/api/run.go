package api

import (
	"net/http"
)

func (a *API) registerRun(mux muxer) {
	mux.HandleFunc("POST /api/projects/{slug}/run/{op}", a.runControl)
}

// runControl drives a project's run state (RC2): start, pause, stop or
// run-one. The state change is published by the engine; the answer is the
// project's overview after it.
func (a *API) runControl(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	runs, actor := a.deps.Engine.Runs(), actorOf(r)
	var err error
	switch op := r.PathValue("op"); op {
	case "start":
		err = runs.Start(r.Context(), actor, slug)
	case "pause":
		err = runs.Pause(r.Context(), actor, slug)
	case "stop":
		err = runs.Stop(r.Context(), actor, slug)
	case "run-one":
		err = runs.RunOne(r.Context(), actor, slug)
	default:
		writeJSON(w, http.StatusNotFound, errorBody{Error: "unknown run operation " + op, Field: "op"})
		return
	}
	if err != nil {
		writeErr(w, err)
		return
	}
	a.respondProject(w, r, slug, http.StatusOK)
}
