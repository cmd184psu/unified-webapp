package api

import (
	"net/http"
)

func (a *API) registerQueue(mux muxer) {
	const e = "/api/projects/{slug}/efforts/{id}"
	mux.HandleFunc("POST "+e+"/compile", a.compile)
	mux.HandleFunc("GET "+e+"/queue", a.getQueue)
	mux.HandleFunc("POST "+e+"/queue/approve", a.approveQueue)
}

// compile starts compiling the approved plan into a task queue. It returns at
// once; the compiler stage runs on its backend.
func (a *API) compile(w http.ResponseWriter, r *http.Request) {
	ef, ok := a.loadEffort(w, r)
	if !ok {
		return
	}
	if err := a.deps.Engine.Compile(r.Context(), actorOf(r), ef.ID); err != nil {
		writeErr(w, err)
		return
	}
	a.emit("effort.update", map[string]any{"project": ef.Project, "effort": ef.ID})
	a.respondEffort(w, r, ef.ID, http.StatusAccepted)
}

// getQueue is the approval view (QA3): every task with its prompt,
// files_touched, acceptance criteria and dependencies, and the hash an
// approval must quote.
func (a *API) getQueue(w http.ResponseWriter, r *http.Request) {
	ef, ok := a.loadEffort(w, r)
	if !ok {
		return
	}
	v, err := a.deps.Engine.QueueView(r.Context(), ef.ID)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

// approveQueue approves the whole queue the caller reviewed, pinned by its
// hash: a queue edited since is 409 and nothing changes (QA2).
func (a *API) approveQueue(w http.ResponseWriter, r *http.Request) {
	ef, ok := a.loadEffort(w, r)
	if !ok {
		return
	}
	var body struct {
		Hash string `json:"hash"`
	}
	if !decodeBody(w, r, &body, false) {
		return
	}
	if body.Hash == "" {
		badRequest(w, "hash", "the hash of the reviewed queue is required")
		return
	}
	if _, err := a.deps.Engine.ApproveQueue(r.Context(), actorOf(r), ef.ID, body.Hash); err != nil {
		writeErr(w, err)
		return
	}
	a.emit("effort.update", map[string]any{"project": ef.Project, "effort": ef.ID})
	v, err := a.deps.Engine.QueueView(r.Context(), ef.ID)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}
