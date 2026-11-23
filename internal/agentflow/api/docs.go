package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"cmd184psu/unified-webapp/internal/agentflow/engine"
	"cmd184psu/unified-webapp/internal/agentflow/model"
	"cmd184psu/unified-webapp/internal/agentflow/store"
)

func (a *API) registerDocs(mux muxer) {
	const e = "/api/projects/{slug}/efforts/{id}"
	mux.HandleFunc("GET "+e+"/frd", a.getDoc(model.DocFRD, false))
	mux.HandleFunc("PUT "+e+"/frd", a.putFRD)
	mux.HandleFunc("GET "+e+"/frd/revisions", a.listRevisions(model.DocFRD))
	mux.HandleFunc("GET "+e+"/frd/revisions/{n}", a.getDoc(model.DocFRD, true))
	mux.HandleFunc("POST "+e+"/frd/approve", a.approveFRD)

	mux.HandleFunc("POST "+e+"/plan/generate", a.generatePlan)
	mux.HandleFunc("GET "+e+"/plan", a.getPlan)
	mux.HandleFunc("PUT "+e+"/plan", a.putPlan)
	mux.HandleFunc("GET "+e+"/plan/rounds", a.getRounds)
	mux.HandleFunc("GET "+e+"/plan/revisions", a.listRevisions(model.DocPlan))
	mux.HandleFunc("GET "+e+"/plan/revisions/{n}", a.getDoc(model.DocPlan, true))
	mux.HandleFunc("POST "+e+"/plan/approve", a.approvePlan)
}

// DocView is one revision of an effort's FRD or plan. Approved marks the
// revision a human approved (DBS4).
type DocView struct {
	Revision  int             `json:"revision"`
	CreatedBy string          `json:"created_by"`
	Created   int64           `json:"created"`
	Approved  bool            `json:"approved"`
	JSON      json.RawMessage `json:"json,omitempty"`
}

func (a *API) docView(ctx context.Context, d model.Doc, withBody bool) (DocView, error) {
	v := DocView{Revision: d.Revision, CreatedBy: d.CreatedBy, Created: d.Created}
	if withBody {
		v.JSON = d.JSON
	}
	ap, err := a.deps.Store.ApprovedRevision(ctx, d.Effort, d.Kind)
	switch {
	case err == nil:
		v.Approved = ap.Revision == d.Revision
	case !errors.Is(err, store.ErrNotFound):
		return v, err
	}
	return v, nil
}

// getDoc serves the latest revision of a document kind, or revision {n}.
func (a *API) getDoc(kind string, byRevision bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ef, ok := a.loadEffort(w, r)
		if !ok {
			return
		}
		n := 0
		if byRevision {
			var ok bool
			if n, ok = pathRevision(w, r); !ok {
				return
			}
		}
		d, err := a.deps.Store.GetDoc(r.Context(), ef.ID, kind, n)
		if err != nil {
			writeErr(w, err)
			return
		}
		v, err := a.docView(r.Context(), d, true)
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, v)
	}
}

func pathRevision(w http.ResponseWriter, r *http.Request) (int, bool) {
	n, err := strconv.Atoi(r.PathValue("n"))
	if err != nil || n < 1 {
		badRequest(w, "n", "revision must be a positive integer")
		return 0, false
	}
	return n, true
}

// listRevisions lists the revisions of a document kind, oldest first, without
// their bodies.
func (a *API) listRevisions(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ef, ok := a.loadEffort(w, r)
		if !ok {
			return
		}
		out := []DocView{}
		latest, err := a.deps.Store.GetDoc(r.Context(), ef.ID, kind, 0)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			writeErr(w, err)
			return
		}
		for n := 1; err == nil && n <= latest.Revision; n++ {
			d, derr := a.deps.Store.GetDoc(r.Context(), ef.ID, kind, n)
			if derr != nil {
				writeErr(w, derr)
				return
			}
			v, verr := a.docView(r.Context(), d, false)
			if verr != nil {
				writeErr(w, verr)
				return
			}
			out = append(out, v)
		}
		writeJSON(w, http.StatusOK, out)
	}
}

// frdFieldRE picks the field out of a model.ParseFRD message such as
// "frd: requirements[2] needs an id and a description".
var frdFieldRE = regexp.MustCompile(`^frd: ([a-z_]+(?:\[\d+\])?) `)

// frdField names the field a model.ParseFRD failure is about.
func frdField(msg string) string {
	switch {
	case strings.Contains(msg, "requirement is required"), strings.Contains(msg, "duplicate requirement"):
		return "requirements"
	case strings.Contains(msg, "references unknown requirement"):
		return "acceptance_criteria"
	}
	if m := frdFieldRE.FindStringSubmatch(msg); m != nil {
		return m[1]
	}
	return "body"
}

// readFRD decodes an FRD from the request body strictly against model.FRD
// (unknown fields are refused), validates it, and returns the canonical JSON
// that gets stored. It writes the 400 naming the field itself on failure.
func readFRD(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	var f model.FRD
	if !decodeBody(w, r, &f, false) {
		return nil, false
	}
	raw, err := json.Marshal(f)
	if err == nil {
		// Ids the author left out are filled on save, so a link to R1 is
		// checked against the ids the filler will assign.
		var probe model.FRD
		if err = json.Unmarshal(raw, &probe); err == nil {
			model.FillFRD(&probe, 0, time.Time{})
			var filled []byte
			if filled, err = json.Marshal(probe); err == nil {
				_, err = model.ParseFRD(filled)
			}
		}
	}
	if err != nil {
		badRequest(w, frdField(err.Error()), strings.TrimPrefix(err.Error(), "frd: "))
		return nil, false
	}
	return raw, true
}

// putFRD saves the body as the next FRD revision. An edit after approval
// leaves the approved revision untouched (DBS4).
func (a *API) putFRD(w http.ResponseWriter, r *http.Request) {
	ef, ok := a.loadEffort(w, r)
	if !ok {
		return
	}
	raw, ok := readFRD(w, r)
	if !ok {
		return
	}
	a.saveDoc(w, r, ef, model.DocFRD, raw)
}

// putPlan saves the body, a JSON object, as the next plan revision.
func (a *API) putPlan(w http.ResponseWriter, r *http.Request) {
	ef, ok := a.loadEffort(w, r)
	if !ok {
		return
	}
	var plan map[string]json.RawMessage
	if !decodeBody(w, r, &plan, false) {
		return
	}
	raw, err := json.Marshal(plan)
	if err != nil {
		badRequest(w, "body", err.Error())
		return
	}
	a.saveDoc(w, r, ef, model.DocPlan, raw)
}

func (a *API) saveDoc(w http.ResponseWriter, r *http.Request, ef model.Effort, kind string, raw []byte) {
	d, err := a.deps.Store.SaveDoc(r.Context(), actorOf(r), ef.ID, kind, raw)
	if err != nil {
		writeErr(w, err)
		return
	}
	a.emit("effort.update", map[string]any{"project": ef.Project, "effort": ef.ID})
	v, err := a.docView(r.Context(), d, true)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

// approveFRD records approval of an FRD revision (the latest when the body
// names none).
func (a *API) approveFRD(w http.ResponseWriter, r *http.Request) {
	ef, ok := a.loadEffort(w, r)
	if !ok {
		return
	}
	var body struct {
		Revision int `json:"revision"`
	}
	if !decodeBody(w, r, &body, true) {
		return
	}
	if body.Revision == 0 {
		d, err := a.deps.Store.GetDoc(r.Context(), ef.ID, model.DocFRD, 0)
		if err != nil {
			writeErr(w, err)
			return
		}
		body.Revision = d.Revision
	}
	if _, err := a.deps.Store.ApproveDoc(r.Context(), actorOf(r), ef.ID, model.DocFRD, body.Revision); err != nil {
		writeErr(w, err)
		return
	}
	if set, err := a.deps.Store.EffectiveSettings(r.Context(), ef.Project); err == nil && set.AutoApprove && a.deps.Engine != nil {
		_ = a.deps.Engine.GeneratePlan(r.Context(), "auto_approve", ef.ID)
		_ = a.deps.Store.WithTx(r.Context(), func(tx *sql.Tx) error {
			_, err := store.AppendEvent(tx, "auto_approve", "auto_approve", "effort", strconv.FormatInt(ef.ID, 10), map[string]any{"stage": "frd"})
			return err
		})
	}
	a.emit("effort.update", map[string]any{"project": ef.Project, "effort": ef.ID})
	a.respondEffort(w, r, ef.ID, http.StatusOK)
}

// generatePlan starts the ralplan loop; it returns at once and the loop runs
// on stage completions (RP1).
func (a *API) generatePlan(w http.ResponseWriter, r *http.Request) {
	ef, ok := a.loadEffort(w, r)
	if !ok {
		return
	}
	if err := a.deps.Engine.GeneratePlan(r.Context(), actorOf(r), ef.ID); err != nil {
		writeErr(w, err)
		return
	}
	a.emit("effort.update", map[string]any{"project": ef.Project, "effort": ef.ID})
	a.respondEffort(w, r, ef.ID, http.StatusAccepted)
}

func (a *API) getPlan(w http.ResponseWriter, r *http.Request) {
	ef, ok := a.loadEffort(w, r)
	if !ok {
		return
	}
	v, err := a.deps.Engine.PlanView(r.Context(), ef.ID)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

// Round is one planning round: the reviews of the plan it produced.
type Round struct {
	Round   int                 `json:"round"`
	Reviews []engine.PlanReview `json:"reviews"`
}

// getRounds groups the planning loop's reviews by round (RP2).
func (a *API) getRounds(w http.ResponseWriter, r *http.Request) {
	ef, ok := a.loadEffort(w, r)
	if !ok {
		return
	}
	v, err := a.deps.Engine.PlanView(r.Context(), ef.ID)
	if err != nil {
		writeErr(w, err)
		return
	}
	rounds := []Round{}
	for _, rv := range v.Reviews {
		if n := len(rounds); n == 0 || rounds[n-1].Round != rv.Round {
			rounds = append(rounds, Round{Round: rv.Round, Reviews: []engine.PlanReview{}})
		}
		rounds[len(rounds)-1].Reviews = append(rounds[len(rounds)-1].Reviews, rv)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"rounds": rounds, "revision": v.Revision, "not_critic_approved": v.NotCriticApproved,
		"warnings": v.Warnings, "waiting": v.Waiting,
	})
}

// approvePlan records the human approval of the presented plan (RP3). The
// compile is a separate call, except under YOLO.
func (a *API) approvePlan(w http.ResponseWriter, r *http.Request) {
	ef, ok := a.loadEffort(w, r)
	if !ok {
		return
	}
	if err := a.deps.Engine.ApprovePlan(r.Context(), actorOf(r), ef.ID); err != nil {
		writeErr(w, err)
		return
	}
	a.emit("effort.update", map[string]any{"project": ef.Project, "effort": ef.ID})
	a.respondEffort(w, r, ef.ID, http.StatusOK)
}
