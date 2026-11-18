package api

import (
	"fmt"
	"net/http"
	"strconv"

	"cmd184psu/unified-webapp/internal/agentflow/engine"
	"cmd184psu/unified-webapp/internal/agentflow/store"
)

func (a *API) registerFRDTools(mux muxer) {
	const e = "/api/projects/{slug}/efforts/{id}"
	mux.HandleFunc("POST "+e+"/frd/draft", a.draftFRD)
	mux.HandleFunc("GET "+e+"/frd/draft", a.getDraft)
	mux.HandleFunc("POST "+e+"/frd/split", a.splitFRD)
	mux.HandleFunc("GET "+e+"/frd/parts", a.listParts)
	mux.HandleFunc("POST "+e+"/frd/parts/{n}/start", a.startPart)
	mux.HandleFunc("GET "+e+"/size", a.getSize)
}

// startDraft submits one drafter stage and answers 202 with the run's state.
func (a *API) startDraft(w http.ResponseWriter, r *http.Request, req engine.DraftRequest) {
	ef, ok := a.loadEffort(w, r)
	if !ok {
		return
	}
	if err := a.deps.Engine.StartDraft(r.Context(), actorOf(r), ef.ID, req); err != nil {
		writeErr(w, err)
		return
	}
	a.emit("effort.update", map[string]any{"project": ef.Project, "effort": ef.ID})
	a.respondDraft(w, r, ef.ID, http.StatusAccepted)
}

func (a *API) respondDraft(w http.ResponseWriter, r *http.Request, effort int64, code int) {
	v, err := a.deps.Engine.DraftState(r.Context(), effort)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, code, v)
}

// draftFRD drafts the FRD from a plain-language description, or tidies the
// existing FRD when polish is set.
func (a *API) draftFRD(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Description string `json:"description"`
		Polish      bool   `json:"polish"`
	}
	if !decodeBody(w, r, &body, false) {
		return
	}
	req := engine.DraftRequest{Purpose: engine.PurposeDraft, Description: body.Description}
	if body.Polish {
		req.Purpose = engine.PurposePolish
	}
	a.startDraft(w, r, req)
}

func (a *API) getDraft(w http.ResponseWriter, r *http.Request) {
	ef, ok := a.loadEffort(w, r)
	if !ok {
		return
	}
	a.respondDraft(w, r, ef.ID, http.StatusOK)
}

// splitFRD asks the drafter to cut the FRD into ordered, self-contained parts.
func (a *API) splitFRD(w http.ResponseWriter, r *http.Request) {
	var body struct {
		MaxParts int `json:"max_parts"`
	}
	if !decodeBody(w, r, &body, true) {
		return
	}
	a.startDraft(w, r, engine.DraftRequest{Purpose: engine.PurposeSplit, MaxParts: body.MaxParts})
}

// PartView is a stored part of a split FRD.
type PartView struct {
	N       int    `json:"n"`
	Title   string `json:"title"`
	Summary string `json:"summary"`
	Tokens  int    `json:"tokens"`
	// StartedEffort is the effort the part became, 0 while it has not been started.
	StartedEffort int64 `json:"started_effort"`
}

func (a *API) listParts(w http.ResponseWriter, r *http.Request) {
	ef, ok := a.loadEffort(w, r)
	if !ok {
		return
	}
	parts, err := a.deps.Store.ListFRDParts(r.Context(), ef.ID)
	if err != nil {
		writeErr(w, err)
		return
	}
	out := []PartView{}
	for _, p := range parts {
		out = append(out, PartView{N: p.N, Title: p.Title, Summary: p.Summary, Tokens: p.Tokens, StartedEffort: p.StartedEffort})
	}
	writeJSON(w, http.StatusOK, out)
}

// startPart creates a new effort from a stored part, the way an import does.
// It is a 409 while the project has an active effort (the one-active-effort
// rule) and for a part that was started already.
func (a *API) startPart(w http.ResponseWriter, r *http.Request) {
	ef, ok := a.loadEffort(w, r)
	if !ok {
		return
	}
	n, err := strconv.Atoi(r.PathValue("n"))
	if err != nil || n < 1 {
		badRequest(w, "n", "part must be a positive integer")
		return
	}
	part, err := a.deps.Store.GetFRDPart(r.Context(), ef.ID, n)
	if err != nil {
		writeErr(w, err)
		return
	}
	if part.StartedEffort != 0 {
		writeErr(w, fmt.Errorf("part %d was already started as effort %d: %w", n, part.StartedEffort, store.ErrConflict))
		return
	}
	actor := actorOf(r)
	started, err := a.effortFromFRD(r.Context(), actor, ef.Project, part.JSON)
	if err != nil {
		writeErr(w, err)
		return
	}
	if err := a.deps.Store.MarkFRDPartStarted(r.Context(), actor, ef.ID, n, started.ID); err != nil {
		writeErr(w, err)
		return
	}
	a.emit("effort.update", map[string]any{"project": ef.Project, "effort": started.ID})
	a.respondEffort(w, r, started.ID, http.StatusCreated)
}

func (a *API) getSize(w http.ResponseWriter, r *http.Request) {
	ef, ok := a.loadEffort(w, r)
	if !ok {
		return
	}
	v, err := a.deps.Engine.FRDSize(r.Context(), ef.ID)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}
