package api

import (
	"context"
	"net/http"
	"strings"

	"cmd184psu/unified-webapp/internal/agentflow/engine"
	"cmd184psu/unified-webapp/internal/agentflow/store"
)

func (a *API) registerBackends(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/backends", a.listBackends)
	mux.HandleFunc("POST /api/backends", a.createBackend)
	mux.HandleFunc("POST /api/backends/probe", a.probeBackend)
	mux.HandleFunc("PATCH /api/backends/{name}", a.patchBackend)
	mux.HandleFunc("POST /api/backends/{name}/retire", a.retireBackend)
	mux.HandleFunc("POST /api/backends/{name}/enable", a.enableBackend)
	mux.HandleFunc("GET /api/candidates", a.listCandidates)
	mux.HandleFunc("PUT /api/candidates/{role}", a.putGlobalCandidates)
	mux.HandleFunc("PUT /api/projects/{slug}/candidates/{role}", a.putProjectCandidates)
	mux.HandleFunc("DELETE /api/projects/{slug}/candidates/{role}", a.clearProjectCandidates)
}

// BackendModelView is one model a backend serves.
type BackendModelView struct {
	Model         string `json:"model"`
	Paid          bool   `json:"paid"`
	MaxConcurrent int    `json:"max_concurrent"`
}

// BackendView is a backend as the API shows it. DownUntil (unix ms) is set
// while the backend sits in its unreachable cooldown (MB7).
type BackendView struct {
	Name      string             `json:"name"`
	Host      string             `json:"host"`
	APIType   string             `json:"api_type"`
	Hosted    bool               `json:"hosted"`
	Paid      bool               `json:"paid"`
	Slots     int                `json:"slots"`
	Retired   bool               `json:"retired"`
	Models    []BackendModelView `json:"models"`
	DownUntil int64              `json:"down_until,omitempty"`
}

func (a *API) backendView(b store.Backend) BackendView {
	v := BackendView{Name: b.Name, Host: b.Host, APIType: b.APIType, Hosted: b.Hosted, Paid: b.Paid,
		Slots: b.Slots, Retired: b.Retired, Models: []BackendModelView{}}
	for _, m := range b.Models {
		v.Models = append(v.Models, BackendModelView{Model: m.Model, Paid: m.Paid, MaxConcurrent: m.MaxConcurrent})
	}
	if until, down := a.deps.Engine.DownBackends()[b.Name]; down {
		v.DownUntil = until.UnixMilli()
	}
	return v
}

// backendResult is what a backend write returns: the backend and the SV3
// warnings, which are empty unless the write left a role without a backend.
type backendResult struct {
	Backend  BackendView `json:"backend"`
	Rerouted []int64     `json:"rerouted,omitempty"`
	Canceled []string    `json:"canceled,omitempty"`
	Warnings []string    `json:"warnings"`
}

func (a *API) respondBackend(w http.ResponseWriter, ctx context.Context, name string, code int, rep engine.RetireReport) {
	b, err := a.deps.Store.GetBackend(ctx, name)
	if err != nil {
		writeErr(w, err)
		return
	}
	res := backendResult{Backend: a.backendView(b), Rerouted: rep.Rerouted, Canceled: rep.Canceled, Warnings: rep.Warnings}
	if res.Warnings == nil {
		res.Warnings = []string{}
	}
	writeJSON(w, code, res)
}

func (a *API) listBackends(w http.ResponseWriter, r *http.Request) {
	bs, err := a.deps.Store.ListBackends(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	out := make([]BackendView, 0, len(bs))
	for _, b := range bs {
		out = append(out, a.backendView(b))
	}
	writeJSON(w, http.StatusOK, out)
}

// backendBody is the writable part of a backend; absent fields keep their
// value on PATCH.
type backendBody struct {
	Name    *string             `json:"name"`
	Host    *string             `json:"host"`
	APIType *string             `json:"api_type"`
	Hosted  *bool               `json:"hosted"`
	Paid    *bool               `json:"paid"`
	Slots   *int                `json:"slots"`
	Models  *[]BackendModelView `json:"models"`
}

func (bb backendBody) apply(b *store.Backend) {
	if bb.Name != nil {
		b.Name = *bb.Name
	}
	if bb.Host != nil {
		b.Host = *bb.Host
	}
	if bb.APIType != nil {
		b.APIType = *bb.APIType
	}
	if bb.Hosted != nil {
		b.Hosted = *bb.Hosted
	}
	if bb.Paid != nil {
		b.Paid = *bb.Paid
	}
	if bb.Slots != nil {
		b.Slots = *bb.Slots
	}
	if bb.Models != nil {
		b.Models = make([]store.BackendModel, 0, len(*bb.Models))
		for _, m := range *bb.Models {
			b.Models = append(b.Models, store.BackendModel{Model: m.Model, Paid: m.Paid, MaxConcurrent: m.MaxConcurrent})
		}
	}
}

// needModels rejects a backend that serves no model: nothing could ever be
// dispatched to it (SV1).
func needModels(b store.Backend) error {
	if len(b.Models) == 0 {
		return store.Invalid("models", "at least one model is required")
	}
	return nil
}

func (a *API) createBackend(w http.ResponseWriter, r *http.Request) {
	var body backendBody
	if !decodeBody(w, r, &body, false) {
		return
	}
	b := store.Backend{Slots: 1}
	body.apply(&b)
	if err := needModels(b); err != nil {
		writeErr(w, err)
		return
	}
	if err := a.deps.Engine.AddBackend(r.Context(), actorOf(r), b); err != nil {
		writeErr(w, err)
		return
	}
	a.emit("backend.update", map[string]any{"backend": b.Name})
	a.respondBackend(w, r.Context(), b.Name, http.StatusCreated, engine.RetireReport{})
}

func (a *API) patchBackend(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	var body backendBody
	if !decodeBody(w, r, &body, false) {
		return
	}
	if body.Name != nil && *body.Name != name {
		badRequest(w, "name", "a backend's name cannot be changed")
		return
	}
	b, err := a.deps.Store.GetBackend(r.Context(), name)
	if err != nil {
		writeErr(w, err)
		return
	}
	body.apply(&b)
	if err := needModels(b); err != nil {
		writeErr(w, err)
		return
	}
	if err := a.deps.Engine.UpdateBackend(r.Context(), actorOf(r), b); err != nil {
		writeErr(w, err)
		return
	}
	a.emit("backend.update", map[string]any{"backend": name})
	a.respondBackend(w, r.Context(), name, http.StatusOK, engine.RetireReport{})
}

// retireBackend retires a backend; retiring the last one for a role is
// allowed and answered with warnings (SV3).
func (a *API) retireBackend(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	var body struct {
		CancelRunning bool `json:"cancel_running"`
	}
	if !decodeBody(w, r, &body, true) {
		return
	}
	rep, err := a.deps.Engine.RetireBackend(r.Context(), actorOf(r), name, engine.RetireOptions{CancelRunning: body.CancelRunning})
	if err != nil {
		writeErr(w, err)
		return
	}
	a.emit("backend.update", map[string]any{"backend": name})
	a.respondBackend(w, r.Context(), name, http.StatusOK, rep)
}

func (a *API) enableBackend(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if err := a.deps.Engine.EnableBackend(r.Context(), actorOf(r), name); err != nil {
		writeErr(w, err)
		return
	}
	a.emit("backend.update", map[string]any{"backend": name})
	a.respondBackend(w, r.Context(), name, http.StatusOK, engine.RetireReport{})
}

// CandidateView is one entry of a role's ordered candidate list (MB4). Paid
// marks a candidate on a paid backend or a paid model (MB5).
type CandidateView struct {
	Scope    string `json:"scope"`
	Project  string `json:"project,omitempty"`
	Role     string `json:"role"`
	Position int    `json:"position"`
	Backend  string `json:"backend"`
	Model    string `json:"model"`
	Paid     bool   `json:"paid"`
}

func (a *API) listCandidates(w http.ResponseWriter, r *http.Request) {
	snap, err := a.deps.Store.Snapshot(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	out := make([]CandidateView, 0, len(snap.Candidates))
	for _, c := range snap.Candidates {
		out = append(out, CandidateView{Scope: c.Scope, Project: c.ProjectID, Role: c.Role, Position: c.Position,
			Backend: c.Backend, Model: c.Model, Paid: c.Paid})
	}
	writeJSON(w, http.StatusOK, out)
}

// candidatesBody is a role's whole ordered list; an empty list is refused.
type candidatesBody struct {
	Candidates []struct {
		Backend string `json:"backend"`
		Model   string `json:"model"`
	} `json:"candidates"`
}

func (a *API) putCandidates(w http.ResponseWriter, r *http.Request, scope, project string) {
	var body candidatesBody
	if !decodeBody(w, r, &body, false) {
		return
	}
	refs := make([]store.CandidateRef, 0, len(body.Candidates))
	for _, c := range body.Candidates {
		refs = append(refs, store.CandidateRef{Backend: strings.TrimSpace(c.Backend), Model: strings.TrimSpace(c.Model)})
	}
	role := r.PathValue("role")
	if err := a.deps.Store.SetRoleCandidates(r.Context(), actorOf(r), scope, project, role, refs); err != nil {
		writeErr(w, err)
		return
	}
	a.emit("candidates.update", map[string]any{"role": role, "project": project})
	a.listCandidates(w, r)
}

func (a *API) putGlobalCandidates(w http.ResponseWriter, r *http.Request) {
	a.putCandidates(w, r, "global", "")
}

func (a *API) putProjectCandidates(w http.ResponseWriter, r *http.Request) {
	a.putCandidates(w, r, "project", r.PathValue("slug"))
}

// clearProjectCandidates drops a project's override of a role, so the global
// list applies again.
func (a *API) clearProjectCandidates(w http.ResponseWriter, r *http.Request) {
	slug, role := r.PathValue("slug"), r.PathValue("role")
	if err := a.deps.Store.ClearProjectRoleCandidates(r.Context(), actorOf(r), slug, role); err != nil {
		writeErr(w, err)
		return
	}
	a.emit("candidates.update", map[string]any{"role": role, "project": slug})
	w.WriteHeader(http.StatusNoContent)
}
