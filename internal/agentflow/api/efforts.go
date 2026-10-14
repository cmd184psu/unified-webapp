package api

import (
	"context"
	"errors"
	"net/http"

	"cmd184psu/unified-webapp/internal/agentflow/model"
	"cmd184psu/unified-webapp/internal/agentflow/store"
)

func (a *API) registerEfforts(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/projects/{slug}/efforts", a.listEfforts)
	mux.HandleFunc("POST /api/projects/{slug}/efforts", a.createEffort)
	mux.HandleFunc("GET /api/projects/{slug}/efforts/{id}", a.getEffort)
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

// EffortView is an effort with the state of its documents and queue.
type EffortView struct {
	model.Effort
	FRD   DocInfo    `json:"frd"`
	Plan  DocInfo    `json:"plan"`
	Queue *QueueInfo `json:"queue"`
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
		Title      string `json:"title"`
		Deliberate bool   `json:"deliberate"`
	}
	if !decodeBody(w, r, &body, false) {
		return
	}
	ef, err := a.deps.Store.CreateEffort(r.Context(), actorOf(r), slug, body.Title, body.Deliberate)
	if err != nil {
		writeErr(w, err)
		return
	}
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
