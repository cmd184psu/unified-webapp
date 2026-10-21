package api

import (
	"context"
	"net/http"
	"slices"
	"strings"

	"cmd184psu/unified-webapp/internal/agentflow/engine"
	"cmd184psu/unified-webapp/internal/agentflow/store"
)

func (a *API) registerBackends(mux muxer) {
	mux.HandleFunc("GET /api/backends", a.listBackends)
	mux.HandleFunc("POST /api/backends", a.createBackend)
	mux.HandleFunc("POST /api/backends/probe", a.probeBackend)
	mux.HandleFunc("PATCH /api/backends/{name}", a.patchBackend)
	mux.HandleFunc("POST /api/backends/{name}/extend-windows", a.extendWindows)
	mux.HandleFunc("POST /api/backends/{name}/retire", a.retireBackend)
	mux.HandleFunc("POST /api/backends/{name}/enable", a.enableBackend)
	mux.HandleFunc("GET /api/backends/{name}/check", a.checkStatus)
	mux.HandleFunc("POST /api/backends/{name}/check", a.checkModels)
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
	// ContextTokens is the model's context window, 0 when unknown.
	ContextTokens int `json:"context_tokens"`
	// WindowTokens is the context window the model is served with, 0 when not known (H15).
	// A value sent in is the owner's setting; one left out keeps what the server learned.
	WindowTokens int `json:"window_tokens"`
	// WindowSource is where that number came from: set, observed (a run was cut off at
	// this size), host, or empty. The server sets it; a value sent in is ignored.
	WindowSource string `json:"window_source"`
	// Tier rates the model: 1 strong, 2 capable, 3 light, 0 not rated.
	Tier int `json:"tier"`
	// Tools is whether the host says the model supports tool calls, which every role needs (H10): yes,
	// no, or empty when not known. The server sets it from the host; a value sent in is ignored.
	Tools string `json:"tools"`
	// TooBig is set when the model's weights do not fit the host's memory budget; it is passed over (H15e).
	TooBig bool `json:"too_big"`
}

// BackendView is a backend as the API shows it. DownUntil (unix ms) is set
// while the backend sits in its unreachable cooldown (MB7).
type BackendView struct {
	Name    string `json:"name"`
	Host    string `json:"host"`
	APIType string `json:"api_type"`
	Hosted  bool   `json:"hosted"`
	Paid    bool   `json:"paid"`
	Slots   int    `json:"slots"`
	// MemoryBudgetMB is what one loaded model may use on the host, weights and context cache (H15d); 0 = not set.
	MemoryBudgetMB int `json:"memory_budget_mb"`
	// WindowCapTokens caps an extended window; 0 = no cap.
	WindowCapTokens int `json:"window_cap_tokens"`
	// DutyPercent is the share of time the backend may be kept busy; 100 = no cap.
	DutyPercent int  `json:"duty_percent"`
	Retired     bool `json:"retired"`
	// HasKey says an API key is stored; the key is write-only.
	HasKey    bool               `json:"has_key"`
	Models    []BackendModelView `json:"models"`
	DownUntil int64              `json:"down_until,omitempty"`
	// SuspendedSince (unix ms) is set while the backend is out of use because it stopped
	// answering (H8); it comes back by itself when it answers again.
	SuspendedSince int64 `json:"suspended_since,omitempty"`
}

// dutyShown is a stored duty cap as the API states it: 100 for no cap.
func dutyShown(d int) int {
	if d <= 0 {
		return 100
	}
	return d
}

func (a *API) backendView(b store.Backend) BackendView {
	v := BackendView{Name: b.Name, Host: b.Host, APIType: b.APIType, Hosted: b.Hosted, Paid: b.Paid,
		Slots: b.Slots, MemoryBudgetMB: b.MemoryBudgetMB, WindowCapTokens: b.WindowCapTokens, DutyPercent: dutyShown(b.DutyPercent), Retired: b.Retired, HasKey: b.HasKey, Models: []BackendModelView{}}
	for _, m := range b.Models {
		v.Models = append(v.Models, BackendModelView{Model: m.Model, Paid: m.Paid, MaxConcurrent: m.MaxConcurrent, ContextTokens: m.ContextTokens, WindowTokens: m.WindowTokens, WindowSource: m.WindowSource, Tier: m.Tier, Tools: m.Tools, TooBig: m.TooBig})
	}
	if since, ok := a.deps.Engine.SuspendedBackends()[b.Name]; ok {
		v.SuspendedSince = since.UnixMilli()
	} else if until, down := a.deps.Engine.DownBackends()[b.Name]; down {
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
	Name            *string             `json:"name"`
	Host            *string             `json:"host"`
	APIType         *string             `json:"api_type"`
	Hosted          *bool               `json:"hosted"`
	Paid            *bool               `json:"paid"`
	Slots           *int                `json:"slots"`
	MemoryBudgetMB  *int                `json:"memory_budget_mb"`
	WindowCapTokens *int                `json:"window_cap_tokens"`
	DutyPercent     *int                `json:"duty_percent"`
	Models          *[]BackendModelView `json:"models"`
	// APIKey is write-only: absent keeps the stored key, "" clears it.
	APIKey *string `json:"api_key"`
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
	if bb.MemoryBudgetMB != nil {
		b.MemoryBudgetMB = *bb.MemoryBudgetMB
	}
	if bb.WindowCapTokens != nil {
		b.WindowCapTokens = *bb.WindowCapTokens
	}
	if bb.DutyPercent != nil {
		b.DutyPercent = *bb.DutyPercent
	}
	if bb.Models != nil {
		b.Models = make([]store.BackendModel, 0, len(*bb.Models))
		for _, m := range *bb.Models {
			b.Models = append(b.Models, store.BackendModel{Model: m.Model, Paid: m.Paid, MaxConcurrent: m.MaxConcurrent, ContextTokens: m.ContextTokens, WindowTokens: m.WindowTokens, Tier: m.Tier})
		}
	}
}

// checkTools sets each model's tool support from its host and refuses a model
// that lacks it (H10): every agentflow role calls tools, so a model that
// cannot would only crash its stages. A client's value is ignored; what was
// stored before is kept until the host answers. An Ollama host that cannot be
// reached leaves a model's support unknown rather than blocking the save.
func (a *API) checkTools(ctx context.Context, b *store.Backend, prev []store.BackendModel) error {
	known := map[string]string{}
	for _, m := range prev {
		known[m.Model] = m.Tools
	}
	var ask []string
	for i := range b.Models {
		b.Models[i].Tools = known[b.Models[i].Model]
		if b.Models[i].Tools != "yes" {
			ask = append(ask, b.Models[i].Model)
		}
	}
	if len(ask) > 0 {
		info := probeInfo(ctx, http.DefaultClient, b.Host, b.APIType, ask)
		for i := range b.Models {
			if got := info[b.Models[i].Model].Tools; got != "" {
				b.Models[i].Tools = got
			}
		}
	}
	var bad []string
	for _, m := range b.Models {
		if m.Tools == "no" {
			bad = append(bad, m.Model)
		}
	}
	if len(bad) > 0 {
		return store.Invalid("models", strings.Join(bad, ", ")+" does not support tool calls, which every agentflow role needs; remove it from the backend")
	}
	return nil
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
	keepWindows(&b, nil)
	if err := needModels(b); err != nil {
		writeErr(w, err)
		return
	}
	if err := a.checkTools(r.Context(), &b, nil); err != nil {
		writeErr(w, err)
		return
	}
	if body.APIKey != nil {
		if err := store.ValidateAPIKey(*body.APIKey); err != nil {
			writeErr(w, err)
			return
		}
	}
	if err := a.deps.Engine.AddBackend(r.Context(), actorOf(r), b); err != nil {
		writeErr(w, err)
		return
	}
	if !a.storeKey(w, r, b.Name, body.APIKey) {
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
	prev := slices.Clone(b.Models)
	body.apply(&b)
	keepWindows(&b, prev)
	if err := needModels(b); err != nil {
		writeErr(w, err)
		return
	}
	if err := a.checkTools(r.Context(), &b, prev); err != nil {
		writeErr(w, err)
		return
	}
	if body.APIKey != nil {
		if err := store.ValidateAPIKey(*body.APIKey); err != nil {
			writeErr(w, err)
			return
		}
	}
	if err := a.deps.Engine.UpdateBackend(r.Context(), actorOf(r), b); err != nil {
		writeErr(w, err)
		return
	}
	if !a.storeKey(w, r, name, body.APIKey) {
		return
	}
	a.emit("backend.update", map[string]any{"backend": name})
	a.respondBackend(w, r.Context(), name, http.StatusOK, engine.RetireReport{})
}

// storeKey saves the write-only API key when the request carried one. It
// writes the error response and returns false on failure.
func (a *API) storeKey(w http.ResponseWriter, r *http.Request, name string, key *string) bool {
	if key == nil {
		return true
	}
	if err := a.deps.Store.SetBackendKey(r.Context(), actorOf(r), name, *key); err != nil {
		writeErr(w, err)
		return false
	}
	return true
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

// keepWindows settles each model's served window after a client's save (H15).
// A window the client left at 0 keeps what the server held (the owner's value
// or one learned from a cut-off run); a different number is the owner's own
// setting; the same number keeps its source, so a round trip through the UI
// does not turn a learned value into a configured one.
func keepWindows(b *store.Backend, prev []store.BackendModel) {
	old := map[string]store.BackendModel{}
	for _, m := range prev {
		old[m.Model] = m
	}
	for i := range b.Models {
		p := old[b.Models[i].Model]
		b.Models[i].TooBig = p.TooBig
		switch w := b.Models[i].WindowTokens; {
		case w == 0:
			b.Models[i].WindowTokens, b.Models[i].WindowSource = p.WindowTokens, p.WindowSource
		case w == p.WindowTokens:
			b.Models[i].WindowSource = p.WindowSource
		default:
			b.Models[i].WindowSource = "set"
		}
	}
}

// checkModels starts a check of every model of a backend (H27); checkStatus
// reports its progress and the last result of each model.
func (a *API) checkModels(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if err := a.deps.Engine.CheckModels(r.Context(), actorOf(r), name); err != nil {
		writeErr(w, err)
		return
	}
	v, err := a.deps.Engine.CheckStatus(r.Context(), name)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, v)
}

func (a *API) checkStatus(w http.ResponseWriter, r *http.Request) {
	v, err := a.deps.Engine.CheckStatus(r.Context(), r.PathValue("name"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}
