package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"cmd184psu/unified-webapp/internal/agentflow/engine"
	"cmd184psu/unified-webapp/internal/agentflow/model"
	"cmd184psu/unified-webapp/internal/agentflow/rulings"
	"cmd184psu/unified-webapp/internal/agentflow/store"
	"path/filepath"
)

func (a *API) registerExport(mux muxer) {
	mux.HandleFunc("GET /api/efforts/{id}/export", a.exportEffort)
	mux.HandleFunc("GET /api/efforts/{id}/frd/export", a.exportDoc(model.DocFRD))
	mux.HandleFunc("GET /api/efforts/{id}/plan/export", a.exportDoc(model.DocPlan))
	mux.HandleFunc("POST /api/projects/{slug}/efforts/import", a.importFRD)
	mux.HandleFunc("POST /api/import", a.importBundle)
}

// EffortExport is the JSON bundle of a whole effort (DBS6, AF-15): the
// effort's approved FRD and plan, its compiled queue with every task
// definition, its budget, priority and start condition, and the project's role
// candidates at export time. It carries no API keys and no run history.
type EffortExport struct {
	Format  string `json:"format,omitempty"`
	Version int    `json:"version,omitempty"`
	Project string `json:"project,omitempty"`
	// ContentHash covers the FRD, plan and queue; import keeps approvals only
	// when it still matches.
	ContentHash string          `json:"content_hash,omitempty"`
	Approvals   BundleApprovals `json:"approvals"`
	Effort      model.Effort    `json:"effort"`
	FRD         *DocView        `json:"frd,omitempty"`
	Plan        *DocView        `json:"plan,omitempty"`
	Queue       *model.Queue    `json:"queue,omitempty"`
	// Candidates is informational: the project's and global role candidate
	// lists at export time. Import never changes the receiving installation.
	Candidates []store.Candidate       `json:"role_candidates,omitempty"`
	Statuses   map[string]model.Status `json:"task_statuses,omitempty"`
}

// BundleApprovals says which parts of an exported effort were approved.
type BundleApprovals struct {
	FRD   bool `json:"frd"`
	Plan  bool `json:"plan"`
	Queue bool `json:"queue"`
}

const bundleFormat = "agentflow-effort"
const bundleVersion = 1

// bundleHash is the content fingerprint an import compares before keeping
// approvals. Only the content, not the IDs or timestamps, is covered.
func bundleHash(frd, plan json.RawMessage, queueHash string) string {
	sum := sha256.New()
	sum.Write(frd)
	sum.Write([]byte("\n"))
	sum.Write(plan)
	sum.Write([]byte("\n"))
	sum.Write([]byte(queueHash))
	return hex.EncodeToString(sum.Sum(nil))
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
	ex := EffortExport{Format: bundleFormat, Version: bundleVersion, Project: ef.Project, Effort: ef}
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
			ex.Approvals.FRD = v.Approved
		} else {
			ex.Plan, report.Plan, report.PlanRevision = &v, d.JSON, d.Revision
			ex.Approvals.Plan = v.Approved
		}
	}
	switch q, err := a.deps.Store.GetQueue(r.Context(), ef.ID); {
	case err == nil:
		ex.Queue, report.Queue = &q, &q
		ex.Approvals.Queue = q.Status == model.QueueApproved
		if ex.Statuses, err = a.deps.Engine.TaskStatuses(r.Context(), ef.Project, ef.ID); err != nil {
			writeErr(w, err)
			return
		}
		report.Statuses = ex.Statuses
	case !errors.Is(err, store.ErrNotFound):
		writeErr(w, err)
		return
	}
	// AF-22: per-effort rulings display.
	if set, err := a.deps.Store.EffectiveSettings(r.Context(), ef.Project); err == nil && set.RulingsFile != "" {
		report.RulingsFile = set.RulingsFile
		am := rulings.ParseAreaMap(set.RulingsFileAreaMap)
		// Collect all files_touched across the effort's tasks.
		var allFiles []string
		for _, t := range ex.Queue.Tasks {
			allFiles = append(allFiles, t.FilesTouched...)
		}
		proj, err := a.deps.Store.GetProject(r.Context(), ef.Project)
		if err == nil {
			if rf, err := rulings.Load(filepath.Join(proj.RepoPath, set.RulingsFile)); err == nil {
				for _, r := range rulings.Selected(rf, allFiles, am) {
					report.Rulings = append(report.Rulings, model.RulingExport{
						ID:     r.ID,
						Topic:  r.Topic,
						Ruling: r.Ruling,
						Areas:  strings.Join(r.Areas, ", "),
						Date:   r.Date,
					})
				}
			}
		}
	}
	if ex.FRD != nil && ex.Plan != nil && ex.Queue != nil {
		ex.ContentHash = bundleHash(ex.FRD.JSON, ex.Plan.JSON, ex.Queue.Hash)
	}
	if snap, err := a.deps.Store.Snapshot(r.Context()); err == nil {
		for _, c := range snap.Candidates {
			if c.Scope == "global" || c.ProjectID == ef.Project {
				ex.Candidates = append(ex.Candidates, c)
			}
		}
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
	ef, err := a.effortFromFRD(r.Context(), actorOf(r), slug, raw)
	if err != nil {
		writeErr(w, err)
		return
	}
	a.emit("effort.update", map[string]any{"project": slug, "effort": ef.ID})
	a.respondEffort(w, r, ef.ID, http.StatusCreated)
}

// importBundle creates a parked effort from an exported bundle. It keeps the
// FRD, plan and queue approvals only when the bundle's content hash still
// matches the content; otherwise the new effort lands in draft for a human to
// re-approve. A project query parameter overrides the bundle's project.
func (a *API) importBundle(w http.ResponseWriter, r *http.Request) {
	var b EffortExport
	if !decodeBody(w, r, &b, false) {
		return
	}
	if b.Format != bundleFormat || b.Version != bundleVersion {
		badRequest(w, "format", fmt.Sprintf("must be %s version %d", bundleFormat, bundleVersion))
		return
	}
	slug := r.URL.Query().Get("project")
	if slug == "" {
		slug = b.Project
	}
	if slug == "" {
		badRequest(w, "project", "a bundle must name its project, or the request must give one")
		return
	}
	if b.FRD == nil || b.Plan == nil || b.Queue == nil || len(b.Queue.Tasks) == 0 {
		badRequest(w, "bundle", "must carry an FRD, a plan and a queue with tasks")
		return
	}
	if len(b.FRD.JSON) == 0 || len(b.Plan.JSON) == 0 {
		badRequest(w, "bundle", "must carry the FRD and plan documents")
		return
	}
	f, err := model.ParseFilledFRD(b.FRD.JSON)
	if err != nil {
		writeErr(w, err)
		return
	}
	qhash := model.QueueHash(b.Queue.Tasks)
	if b.Queue.Hash != "" && b.Queue.Hash != qhash {
		badRequest(w, "queue", "its task hash does not match the task definitions")
		return
	}
	keep := b.Approvals.FRD && b.Approvals.Plan && b.Approvals.Queue &&
		b.ContentHash != "" && b.ContentHash == bundleHash(b.FRD.JSON, b.Plan.JSON, qhash)
	if b.Effort.StartCondition != "" {
		if _, err := engine.ParseStartCondition(b.Effort.StartCondition); err != nil {
			badRequest(w, "start_condition", err.Error())
			return
		}
	}
	ef, err := a.deps.Store.CreateEffort(r.Context(), actorOf(r), slug, f.Title, b.Effort.Deliberate)
	if err != nil {
		writeErr(w, err)
		return
	}
	abandon := func() {
		st := model.EffortAbandoned
		_, _ = a.deps.Store.UpdateEffort(context.WithoutCancel(r.Context()), actorOf(r), ef.ID, store.EffortPatch{Status: &st})
	}
	docFRD, err := a.deps.Store.SaveDoc(r.Context(), actorOf(r), ef.ID, model.DocFRD, b.FRD.JSON)
	if err != nil {
		abandon()
		writeErr(w, err)
		return
	}
	docPlan, err := a.deps.Store.SaveDoc(r.Context(), actorOf(r), ef.ID, model.DocPlan, b.Plan.JSON)
	if err != nil {
		abandon()
		writeErr(w, err)
		return
	}
	q, err := a.deps.Store.SaveQueue(r.Context(), actorOf(r), ef.ID, b.Queue.Tasks)
	if err != nil {
		abandon()
		writeErr(w, err)
		return
	}
	patch := store.EffortPatch{StartCondition: &b.Effort.StartCondition}
	if b.Effort.Priority != 0 {
		patch.Priority = &b.Effort.Priority
	}
	if b.Effort.BudgetTokens > 0 {
		patch.BudgetTokens = &b.Effort.BudgetTokens
	}
	if b.Effort.BudgetSeconds > 0 {
		patch.BudgetSeconds = &b.Effort.BudgetSeconds
	}
	if _, err := a.deps.Store.UpdateEffort(r.Context(), actorOf(r), ef.ID, patch); err != nil {
		abandon()
		writeErr(w, err)
		return
	}
	if keep {
		if _, err := a.deps.Store.ApproveDoc(r.Context(), actorOf(r), ef.ID, model.DocFRD, docFRD.Revision); err != nil {
			abandon()
			writeErr(w, err)
			return
		}
		if _, err := a.deps.Store.ApproveDoc(r.Context(), actorOf(r), ef.ID, model.DocPlan, docPlan.Revision); err != nil {
			abandon()
			writeErr(w, err)
			return
		}
		if _, err := a.deps.Store.ApproveQueue(r.Context(), actorOf(r), ef.ID, q.Hash); err != nil {
			abandon()
			writeErr(w, err)
			return
		}
		ready := model.EffortReady
		if _, err := a.deps.Store.UpdateEffort(r.Context(), actorOf(r), ef.ID, store.EffortPatch{Status: &ready}); err != nil {
			abandon()
			writeErr(w, err)
			return
		}
	}
	_, _ = a.deps.Engine.SetFreshBase(r.Context(), ef.ID)
	a.emit("effort.update", map[string]any{"project": slug, "effort": ef.ID})
	a.respondEffort(w, r, ef.ID, http.StatusCreated)
}

// effortFromFRD creates an effort titled after the FRD and saves the FRD as
// its revision 1. It is shared by the import route and by starting a part of
// a split FRD.
func (a *API) effortFromFRD(ctx context.Context, actor, slug string, raw []byte) (model.Effort, error) {
	f, err := model.ParseFilledFRD(raw)
	if err != nil {
		return model.Effort{}, err
	}
	ef, err := a.deps.Store.CreateEffort(ctx, actor, slug, f.Title, false)
	if err != nil {
		return ef, err
	}
	if _, err := a.deps.Store.SaveDoc(ctx, actor, ef.ID, model.DocFRD, raw); err != nil {
		abandoned := model.EffortAbandoned // do not leave an empty effort holding the project
		_, _ = a.deps.Store.UpdateEffort(ctx, actor, ef.ID, store.EffortPatch{Status: &abandoned})
		return ef, err
	}
	return ef, nil
}
