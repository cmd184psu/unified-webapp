package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"cmd184psu/unified-webapp/internal/agentflow/model"
	"cmd184psu/unified-webapp/internal/agentflow/store"
)

func (a *API) registerExport(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/efforts/{id}/export", a.exportEffort)
	mux.HandleFunc("GET /api/efforts/{id}/frd/export", a.exportDoc(model.DocFRD))
	mux.HandleFunc("GET /api/efforts/{id}/plan/export", a.exportDoc(model.DocPlan))
	mux.HandleFunc("POST /api/projects/{slug}/efforts/import", a.importFRD)
}

// EffortExport is the JSON export of a whole effort (DBS6): the effort, the
// latest FRD and plan, and the queue with each task's derived status.
type EffortExport struct {
	Effort   model.Effort            `json:"effort"`
	FRD      *DocView                `json:"frd,omitempty"`
	Plan     *DocView                `json:"plan,omitempty"`
	Queue    *model.Queue            `json:"queue,omitempty"`
	Statuses map[string]model.Status `json:"task_statuses,omitempty"`
}

// exportFormat reads ?format=json|md (json by default).
func exportFormat(w http.ResponseWriter, r *http.Request) (string, bool) {
	switch f := r.URL.Query().Get("format"); f {
	case "", "json":
		return "json", true
	case "md":
		return "md", true
	default:
		badRequest(w, "format", "format must be json or md")
		return "", false
	}
}

// writeExport sends an export as a download. Markdown goes out as text/markdown
// with nosniff, never as HTML.
func writeExport(w http.ResponseWriter, format, name string, body []byte) {
	ct := "application/json"
	if format == "md" {
		ct = "text/markdown; charset=utf-8"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name+"."+format))
	_, _ = w.Write(body)
}

// indentJSON pretty-prints a JSON document; the content is unchanged.
func indentJSON(v any) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var b bytes.Buffer
	if err := json.Indent(&b, raw, "", "  "); err != nil {
		return nil, err
	}
	b.WriteByte('\n')
	return b.Bytes(), nil
}

// exportedEffort resolves {id} (no project in the path) to an effort.
func (a *API) exportedEffort(w http.ResponseWriter, r *http.Request) (model.Effort, bool) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return model.Effort{}, false
	}
	ef, err := a.deps.Store.GetEffort(r.Context(), id)
	if err != nil {
		writeErr(w, err)
		return model.Effort{}, false
	}
	return ef, true
}

// latestDoc returns the effort's latest document of kind, or nil when it has
// none.
func (a *API) latestDoc(r *http.Request, effort int64, kind string, revision int) (*model.Doc, error) {
	d, err := a.deps.Store.GetDoc(r.Context(), effort, kind, revision)
	if errors.Is(err, store.ErrNotFound) && revision == 0 {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func (a *API) exportEffort(w http.ResponseWriter, r *http.Request) {
	format, ok := exportFormat(w, r)
	if !ok {
		return
	}
	ef, ok := a.exportedEffort(w, r)
	if !ok {
		return
	}
	ex := EffortExport{Effort: ef}
	report := model.EffortReport{Effort: ef}
	for _, kind := range []string{model.DocFRD, model.DocPlan} {
		d, err := a.latestDoc(r, ef.ID, kind, 0)
		if err != nil {
			writeErr(w, err)
			return
		}
		if d == nil {
			continue
		}
		v, err := a.docView(r, *d, true)
		if err != nil {
			writeErr(w, err)
			return
		}
		if kind == model.DocFRD {
			f, err := model.ParseFRD(d.JSON)
			if err != nil {
				writeErr(w, err)
				return
			}
			ex.FRD, report.FRD, report.FRDRevision = &v, &f, d.Revision
		} else {
			ex.Plan, report.Plan, report.PlanRevision = &v, d.JSON, d.Revision
		}
	}
	switch q, err := a.deps.Store.GetQueue(r.Context(), ef.ID); {
	case err == nil:
		ex.Queue, report.Queue = &q, &q
		if ex.Statuses, err = a.deps.Engine.TaskStatuses(r.Context(), ef.Project, ef.ID); err != nil {
			writeErr(w, err)
			return
		}
		report.Statuses = ex.Statuses
	case !errors.Is(err, store.ErrNotFound):
		writeErr(w, err)
		return
	}
	name := "effort-" + strconv.FormatInt(ef.ID, 10)
	if format == "md" {
		writeExport(w, format, name, []byte(model.RenderEffort(report)))
		return
	}
	body, err := indentJSON(ex)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeExport(w, format, name, body)
}

// exportDoc exports one FRD or plan revision (?revision=, the latest by
// default). The JSON export is the stored document itself, so it can be
// imported again.
func (a *API) exportDoc(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		format, ok := exportFormat(w, r)
		if !ok {
			return
		}
		revision := 0
		if s := r.URL.Query().Get("revision"); s != "" {
			n, err := strconv.Atoi(s)
			if err != nil || n < 1 {
				badRequest(w, "revision", "revision must be a positive integer")
				return
			}
			revision = n
		}
		ef, ok := a.exportedEffort(w, r)
		if !ok {
			return
		}
		d, err := a.deps.Store.GetDoc(r.Context(), ef.ID, kind, revision)
		if err != nil {
			writeErr(w, err)
			return
		}
		name := fmt.Sprintf("effort-%d-%s-r%d", ef.ID, kind, d.Revision)
		if format == "json" {
			var doc any
			if err := json.Unmarshal(d.JSON, &doc); err != nil {
				writeErr(w, err)
				return
			}
			body, err := indentJSON(doc)
			if err != nil {
				writeErr(w, err)
				return
			}
			writeExport(w, format, name, body)
			return
		}
		if kind == model.DocPlan {
			writeExport(w, format, name, []byte(model.RenderPlan(d.JSON)))
			return
		}
		f, err := model.ParseFRD(d.JSON)
		if err != nil {
			writeErr(w, err)
			return
		}
		writeExport(w, format, name, []byte(model.RenderFRD(f)))
	}
}

// importFRD creates a new effort from an FRD document: the body is decoded
// strictly against model.FRD, so a misspelt or mistyped field is a 400 naming
// it, and the FRD becomes revision 1 of the new effort (DBS6).
func (a *API) importFRD(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	if _, err := a.deps.Store.GetProject(r.Context(), slug); err != nil {
		writeErr(w, err)
		return
	}
	raw, ok := readFRD(w, r)
	if !ok {
		return
	}
	f, err := model.ParseFRD(raw)
	if err != nil {
		writeErr(w, err)
		return
	}
	actor := actorOf(r)
	ef, err := a.deps.Store.CreateEffort(r.Context(), actor, slug, f.Title, false)
	if err != nil {
		writeErr(w, err)
		return
	}
	if _, err := a.deps.Store.SaveDoc(r.Context(), actor, ef.ID, model.DocFRD, raw); err != nil {
		abandoned := model.EffortAbandoned // do not leave an empty effort holding the project
		_, _ = a.deps.Store.UpdateEffort(r.Context(), actor, ef.ID, store.EffortPatch{Status: &abandoned})
		writeErr(w, err)
		return
	}
	a.emit("effort.update", map[string]any{"project": slug, "effort": ef.ID})
	a.respondEffort(w, r, ef.ID, http.StatusCreated)
}
