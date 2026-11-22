package api

import (
	"context"
	"errors"
	"net/http"

	"cmd184psu/unified-webapp/internal/agentflow/engine"
	"cmd184psu/unified-webapp/internal/agentflow/model"
	"cmd184psu/unified-webapp/internal/agentflow/store"
)

func (a *API) registerEfforts(mux muxer) {
	mux.HandleFunc("GET /api/projects/{slug}/efforts", a.listEfforts)
	mux.HandleFunc("POST /api/projects/{slug}/efforts", a.createEffort)
	mux.HandleFunc("GET /api/projects/{slug}/efforts/{id}", a.getEffort)
	mux.HandleFunc("PATCH /api/projects/{slug}/efforts/{id}", a.patchEffort)
	mux.HandleFunc("POST /api/projects/{slug}/efforts/{id}/park", a.parkEffort)
	mux.HandleFunc("POST /api/projects/{slug}/efforts/{id}/unpark", a.unparkEffort)
	mux.HandleFunc("GET /api/backlog", a.listBacklog)
}

// DocInfo says which revisions of a document exist: the latest, and the
// approved one (0 for none).
type DocInfo struct {
	Latest   int `json:"latest"`
	Approved int `json:"approved"`
}

// QueueInfo summarises an effort's queue.
type QueueInfo struct {
	Revision int    `json:"revision"`
	Hash     string `json:"hash"`
	Status   string `json:"status"`
}

// BudgetView is an effort's usage against its token and time budgets (AT12).
// State is ok, no_new_tasks (from 90%: in-flight work continues, nothing new
// starts) or exhausted (from 100%: the run is stopped).
type BudgetView struct {
	engine.Budget
	Fraction float64 `json:"fraction"`
	State    string  `json:"state"`
}

// EffortView is an effort with the state of its documents and queue. Budget
// is set only for an effort that has a budget.
type EffortView struct {
	model.Effort
	FRD    DocInfo     `json:"frd"`
	Plan   DocInfo     `json:"plan"`
	Queue  *QueueInfo  `json:"queue"`
	Budget *BudgetView `json:"budget"`
}

func (a *API) docInfo(ctx context.Context, effort int64, kind string) (DocInfo, error) {
	var info DocInfo
	d, err := a.deps.Store.GetDoc(ctx, effort, kind, 0)
	switch {
	case err == nil:
		info.Latest = d.Revision
	case !errors.Is(err, store.ErrNotFound):
		return info, err
	}
	ap, err := a.deps.Store.ApprovedRevision(ctx, effort, kind)
	switch {
	case err == nil:
		info.Approved = ap.Revision
	case !errors.Is(err, store.ErrNotFound):
		return info, err
	}
	return info, nil
}

func (a *API) effortView(ctx context.Context, ef model.Effort) (EffortView, error) {
	v := EffortView{Effort: ef}
	var err error
	if v.FRD, err = a.docInfo(ctx, ef.ID, model.DocFRD); err != nil {
		return v, err
	}
	if v.Plan, err = a.docInfo(ctx, ef.ID, model.DocPlan); err != nil {
		return v, err
	}
	if ef.BudgetTokens > 0 || ef.BudgetSeconds > 0 {
		b, err := a.deps.Engine.EffortBudget(ctx, ef)
		if err != nil {
			return v, err
		}
		bv := BudgetView{Budget: b, Fraction: b.Fraction(), State: "ok"}
		switch {
		case b.Exhausted():
			bv.State = "exhausted"
		case b.NoNewTasks():
			bv.State = "no_new_tasks"
		}
		v.Budget = &bv
	}
	switch q, err := a.deps.Store.GetQueue(ctx, ef.ID); {
	case err == nil:
		v.Queue = &QueueInfo{Revision: q.Revision, Hash: q.Hash, Status: q.Status}
	case !errors.Is(err, store.ErrNotFound):
		return v, err
	}
	return v, nil
}

// loadEffort resolves {slug} and {id} to an effort of that project, writing
// the 400 or 404 itself when it cannot.
func (a *API) loadEffort(w http.ResponseWriter, r *http.Request) (model.Effort, bool) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return model.Effort{}, false
	}
	ef, err := a.deps.Store.GetEffort(r.Context(), id)
	if err == nil && ef.Project != r.PathValue("slug") {
		err = store.ErrNotFound
	}
	if err != nil {
		writeErr(w, err)
		return model.Effort{}, false
	}
	return ef, true
}

// respondEffort writes the effort's current view.
func (a *API) respondEffort(w http.ResponseWriter, r *http.Request, id int64, code int) {
	ef, err := a.deps.Store.GetEffort(r.Context(), id)
	if err != nil {
		writeErr(w, err)
		return
	}
	v, err := a.effortView(r.Context(), ef)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, code, v)
}

func (a *API) listEfforts(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	if _, err := a.deps.Store.GetProject(r.Context(), slug); err != nil {
		writeErr(w, err)
		return
	}
	efs, err := a.deps.Store.ListEfforts(r.Context(), slug)
	if err != nil {
		writeErr(w, err)
		return
	}
	out := make([]EffortView, 0, len(efs))
	for _, ef := range efs {
		v, err := a.effortView(r.Context(), ef)
		if err != nil {
			writeErr(w, err)
			return
		}
		out = append(out, v)
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *API) createEffort(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	var body struct {
		Title         string `json:"title"`
		Deliberate    bool   `json:"deliberate"`
		BudgetTokens  *int64 `json:"budget_tokens"`
		BudgetSeconds *int64 `json:"budget_seconds"`
	}
	if !decodeBody(w, r, &body, false) {
		return
	}
	ef, err := a.deps.Store.CreateEffort(r.Context(), actorOf(r), slug, body.Title, body.Deliberate)
	if err != nil {
		writeErr(w, err)
		return
	}
	if body.BudgetTokens != nil || body.BudgetSeconds != nil {
		if _, err := a.deps.Store.UpdateEffort(r.Context(), actorOf(r), ef.ID,
			store.EffortPatch{BudgetTokens: body.BudgetTokens, BudgetSeconds: body.BudgetSeconds}); err != nil {
			writeErr(w, err)
			return
		}
	}
	// AF-15: start from the configured base branch's fetched tip, not from a
	// clone HEAD that may be months behind.
	_, _ = a.deps.Engine.SetFreshBase(r.Context(), ef.ID)
	a.emit("effort.update", map[string]any{"project": slug, "effort": ef.ID})
	a.respondEffort(w, r, ef.ID, http.StatusCreated)
}

func (a *API) getEffort(w http.ResponseWriter, r *http.Request) {
	ef, ok := a.loadEffort(w, r)
	if !ok {
		return
	}
	a.respondEffort(w, r, ef.ID, http.StatusOK)
}

// patchEffort changes the settings of an active effort: the deliberate planning
// mode (RP5) and the token and time budgets (AT12). Absent fields stay as they
// are; a budget of 0 removes the limit.
func (a *API) patchEffort(w http.ResponseWriter, r *http.Request) {
	ef, ok := a.loadEffort(w, r)
	if !ok {
		return
	}
	var body struct {
		Deliberate     *bool   `json:"deliberate"`
		BudgetTokens   *int64  `json:"budget_tokens"`
		BudgetSeconds  *int64  `json:"budget_seconds"`
		Priority       *int64  `json:"priority"`
		StartCondition *string `json:"start_condition"`
	}
	if !decodeBody(w, r, &body, false) {
		return
	}
	if body.StartCondition != nil {
		if _, err := engine.ParseStartCondition(*body.StartCondition); err != nil {
			badRequest(w, "start_condition", err.Error())
			return
		}
	}
	if _, err := a.deps.Store.UpdateEffort(r.Context(), actorOf(r), ef.ID,
		store.EffortPatch{Deliberate: body.Deliberate, BudgetTokens: body.BudgetTokens, BudgetSeconds: body.BudgetSeconds,
			Priority: body.Priority, StartCondition: body.StartCondition}); err != nil {
		writeErr(w, err)
		return
	}
	a.emit("effort.update", map[string]any{"project": ef.Project, "effort": ef.ID})
	a.respondEffort(w, r, ef.ID, http.StatusOK)
}

// parkEffort puts a prepared effort in the backlog. The body may carry a
// priority (higher starts first) and a git start condition.
func (a *API) parkEffort(w http.ResponseWriter, r *http.Request) {
	ef, ok := a.loadEffort(w, r)
	if !ok {
		return
	}
	var body struct {
		Priority       *int64  `json:"priority"`
		StartCondition *string `json:"start_condition"`
	}
	if !decodeBody(w, r, &body, false) {
		return
	}
	if _, err := a.deps.Engine.Park(r.Context(), actorOf(r), ef.ID, body.Priority, body.StartCondition); err != nil {
		writeErr(w, err)
		return
	}
	a.emit("effort.update", map[string]any{"project": ef.Project, "effort": ef.ID})
	a.respondEffort(w, r, ef.ID, http.StatusOK)
}

// unparkEffort returns a parked effort to queue_approved without starting it.
func (a *API) unparkEffort(w http.ResponseWriter, r *http.Request) {
	ef, ok := a.loadEffort(w, r)
	if !ok {
		return
	}
	if _, err := a.deps.Engine.Unpark(r.Context(), actorOf(r), ef.ID); err != nil {
		writeErr(w, err)
		return
	}
	a.emit("effort.update", map[string]any{"project": ef.Project, "effort": ef.ID})
	a.respondEffort(w, r, ef.ID, http.StatusOK)
}

// BacklogItem is one ready effort in GET /api/backlog (AF-15).
func (a *API) listBacklog(w http.ResponseWriter, r *http.Request) {
	efs, err := a.deps.Store.ListReadyEfforts(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	out := make([]EffortView, 0, len(efs))
	for _, ef := range efs {
		v, err := a.effortView(r.Context(), ef)
		if err != nil {
			writeErr(w, err)
			return
		}
		out = append(out, v)
	}
	writeJSON(w, http.StatusOK, out)
}
