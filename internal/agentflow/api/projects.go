package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"cmd184psu/unified-webapp/internal/agentflow/model"
	"cmd184psu/unified-webapp/internal/agentflow/store"
)

func (a *API) registerProjects(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/projects", a.listProjects)
	mux.HandleFunc("POST /api/projects", a.createProject)
	mux.HandleFunc("POST /api/projects/git-init", a.gitInit)
	mux.HandleFunc("GET /api/projects/{slug}", a.getProject)
	mux.HandleFunc("PATCH /api/projects/{slug}", a.patchProject)
	mux.HandleFunc("DELETE /api/projects/{slug}", a.deleteProject)
	mux.HandleFunc("GET /api/projects/{slug}/limits", a.getLimits)
	mux.HandleFunc("PUT /api/projects/{slug}/limits", a.putLimits)
}

// flexStrings converts a JSON object of numbers and strings to the text the
// store parses, so a setting can be sent as 9 or "9" and a limit as "45m". A
// value of any other type is a 400 naming prefix+key.
func flexStrings(w http.ResponseWriter, in map[string]json.RawMessage, prefix string) (map[string]string, bool) {
	out := make(map[string]string, len(in))
	for k, raw := range in {
		var s string
		if json.Unmarshal(raw, &s) == nil {
			out[k] = s
			continue
		}
		var n json.Number
		if err := json.Unmarshal(raw, &n); err != nil {
			badRequest(w, prefix+k, prefix+k+" must be a number or a string")
			return nil, false
		}
		out[k] = n.String()
	}
	return out, true
}

// WorkItem is the stage a running task is in right now (PJ5).
type WorkItem struct {
	Task    string `json:"task"`
	Attempt int    `json:"attempt"`
	Stage   int64  `json:"stage"`
	Role    string `json:"role"`
	Backend string `json:"backend"`
	Model   string `json:"model"`
	State   string `json:"state"`
}

// ProjectView is a project with what the overview shows: run state, active
// effort, task progress and the stages in flight.
type ProjectView struct {
	Slug         string            `json:"slug"`
	RepoPath     string            `json:"repo_path"`
	ModelScope   string            `json:"model_scope"`
	FeedbackCmds []string          `json:"feedback_cmds"`
	Limits       map[string]string `json:"limits"`
	Cleanup      bool              `json:"cleanup_enabled"`
	Created      int64             `json:"created"`
	Updated      int64             `json:"updated"`
	RunState     string            `json:"run_state"`
	Remembered   string            `json:"remembered_run_state"`
	StopReason   string            `json:"stop_reason"`
	Effort       *model.Effort     `json:"effort"`
	Progress     map[string]int    `json:"progress"`
	Current      []WorkItem        `json:"current"`
}

// projectView assembles the overview of one project.
func (a *API) projectView(ctx context.Context, p store.Project) (ProjectView, error) {
	v := ProjectView{
		Slug: p.Slug, RepoPath: p.RepoPath, ModelScope: p.ModelScope, FeedbackCmds: p.FeedbackCmds, Limits: p.Limits, Cleanup: p.CleanupEnabled,
		Created: p.Created.UnixMilli(), Updated: p.Updated.UnixMilli(),
		RunState: a.deps.Engine.Runs().State(p.Slug), Remembered: model.RunStopped,
		StopReason: a.deps.Engine.Runs().StopReason(p.Slug),
		Progress:   map[string]int{}, Current: []WorkItem{},
	}
	if v.FeedbackCmds == nil {
		v.FeedbackCmds = []string{}
	}
	if v.Limits == nil {
		v.Limits = map[string]string{}
	}
	ef, err := a.deps.Store.ActiveEffort(ctx, p.Slug)
	if errors.Is(err, store.ErrNotFound) {
		return v, nil
	}
	if err != nil {
		return v, err
	}
	v.Effort, v.Remembered = &ef, ef.RunState
	statuses, err := a.deps.Engine.TaskStatuses(ctx, p.Slug, ef.ID)
	if errors.Is(err, store.ErrNotFound) {
		return v, nil // no queue yet
	}
	if err != nil {
		return v, err
	}
	for _, s := range statuses {
		v.Progress["total"]++
		v.Progress[string(s)]++
	}
	v.Current, err = a.currentWork(ctx, ef.ID, statuses)
	return v, err
}

// currentWork lists the open stage of every running task.
func (a *API) currentWork(ctx context.Context, effort int64, statuses map[string]model.Status) ([]WorkItem, error) {
	out := []WorkItem{}
	tasks, err := a.deps.Store.ListTasks(ctx, effort)
	if err != nil {
		return nil, err
	}
	for _, t := range tasks {
		if statuses[t.Key] != model.StatusRunning {
			continue
		}
		atts, err := a.deps.Store.ListAttempts(ctx, t.ID)
		if err != nil {
			return nil, err
		}
		for _, at := range atts {
			if at.Outcome != "" {
				continue
			}
			stages, err := a.deps.Store.ListAttemptStages(ctx, at.ID)
			if err != nil {
				return nil, err
			}
			for _, st := range stages {
				if st.State != model.StageDone {
					out = append(out, WorkItem{Task: t.Key, Attempt: at.N, Stage: st.ID, Role: st.Role, Backend: st.Backend, Model: st.Model, State: st.State})
				}
			}
		}
	}
	return out, nil
}

func (a *API) listProjects(w http.ResponseWriter, r *http.Request) {
	ps, err := a.deps.Store.ListProjects(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	out := make([]ProjectView, 0, len(ps))
	for _, p := range ps {
		v, err := a.projectView(r.Context(), p)
		if err != nil {
			writeErr(w, err)
			return
		}
		out = append(out, v)
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *API) getProject(w http.ResponseWriter, r *http.Request) {
	a.respondProject(w, r, r.PathValue("slug"), http.StatusOK)
}

func (a *API) respondProject(w http.ResponseWriter, r *http.Request, slug string, code int) {
	p, err := a.deps.Store.GetProject(r.Context(), slug)
	if err != nil {
		writeErr(w, err)
		return
	}
	v, err := a.projectView(r.Context(), p)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, code, v)
}

// projectBody is the writable part of a project. Absent fields keep their
// value on PATCH and take the default on POST.
type projectBody struct {
	Slug         *string            `json:"slug"`
	RepoPath     *string            `json:"repo_path"`
	ModelScope   *string            `json:"model_scope"`
	FeedbackCmds *[]string          `json:"feedback_cmds"`
	Limits       *map[string]string `json:"limits"`
	Cleanup      *bool              `json:"cleanup_enabled"`
}

func (b projectBody) apply(p *store.Project) {
	if b.Slug != nil {
		p.Slug = *b.Slug
	}
	if b.RepoPath != nil {
		p.RepoPath = *b.RepoPath
	}
	if b.ModelScope != nil {
		p.ModelScope = *b.ModelScope
	}
	if b.FeedbackCmds != nil {
		p.FeedbackCmds = *b.FeedbackCmds
	}
	if b.Limits != nil {
		p.Limits = *b.Limits
	}
	if b.Cleanup != nil {
		p.CleanupEnabled = *b.Cleanup
	}
}

func (a *API) createProject(w http.ResponseWriter, r *http.Request) {
	var body projectBody
	if !decodeBody(w, r, &body, false) {
		return
	}
	p := store.Project{ModelScope: "local"} // local models only until the owner opts in (PP1)
	body.apply(&p)
	if err := a.deps.Store.CreateProject(r.Context(), actorOf(r), p); err != nil {
		writeErr(w, err)
		return
	}
	a.emit("project.update", map[string]any{"project": p.Slug})
	a.respondProject(w, r, p.Slug, http.StatusCreated)
}

func (a *API) patchProject(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	var body projectBody
	if !decodeBody(w, r, &body, false) {
		return
	}
	if body.Slug != nil && *body.Slug != slug {
		badRequest(w, "slug", "a project's slug cannot be changed")
		return
	}
	p, err := a.deps.Store.GetProject(r.Context(), slug)
	if err != nil {
		writeErr(w, err)
		return
	}
	body.apply(&p)
	if err := a.deps.Store.UpdateProject(r.Context(), actorOf(r), p); err != nil {
		writeErr(w, err)
		return
	}
	a.emit("project.update", map[string]any{"project": slug})
	a.respondProject(w, r, slug, http.StatusOK)
}

func (a *API) deleteProject(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	if err := a.deps.Store.DeleteProject(r.Context(), actorOf(r), slug); err != nil {
		writeErr(w, err)
		return
	}
	a.emit("project.delete", map[string]any{"project": slug})
	w.WriteHeader(http.StatusNoContent)
}

// gitInit runs git init on a directory that is not a repository yet, after
// explicit confirmation (PJ1). It is not slug-scoped: the directory has to be
// a repository before a project can be registered on it. The same protected
// directories as for registration are refused (WC2).
func (a *API) gitInit(w http.ResponseWriter, r *http.Request) {
	var body struct {
		RepoPath string `json:"repo_path"`
		Confirm  bool   `json:"confirm"`
	}
	if !decodeBody(w, r, &body, false) {
		return
	}
	if !body.Confirm {
		badRequest(w, "confirm", "git init needs explicit confirmation")
		return
	}
	if a.deps.Git == nil {
		writeJSON(w, http.StatusServiceUnavailable, errorBody{Error: "git service unavailable"})
		return
	}
	dir, err := a.deps.Store.ValidateRepoDir(strings.TrimSpace(body.RepoPath))
	if err != nil {
		writeErr(w, err)
		return
	}
	if a.deps.Git.IsRepo(r.Context(), dir) {
		writeErr(w, fmt.Errorf("%s is already a git repository: %w", dir, store.ErrConflict))
		return
	}
	if err := a.deps.Git.Init(r.Context(), dir); err != nil {
		writeErr(w, err)
		return
	}
	err = a.deps.Store.WithTx(r.Context(), func(tx *sql.Tx) error {
		_, err := store.AppendEvent(tx, actorOf(r), "project.git_init", "project", dir, nil)
		return err
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"repo_path": dir})
}

// LimitsView is a project's limit overrides next to the settings they
// override (CF2): a missing key inherits.
type LimitsView struct {
	Limits    map[string]string `json:"limits"`
	Effective map[string]string `json:"effective"`
}

func (a *API) limitsView(ctx context.Context, slug string) (LimitsView, error) {
	p, err := a.deps.Store.GetProject(ctx, slug)
	if err != nil {
		return LimitsView{}, err
	}
	eff, err := a.deps.Store.EffectiveSettings(ctx, slug)
	if err != nil {
		return LimitsView{}, err
	}
	v := LimitsView{Limits: map[string]string{}, Effective: limitMap(eff)}
	for k, val := range p.Limits {
		v.Limits[k] = val
	}
	return v, nil
}

func (a *API) getLimits(w http.ResponseWriter, r *http.Request) {
	v, err := a.limitsView(r.Context(), r.PathValue("slug"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

// putLimits replaces the project's overrides; a blank value means inherit.
func (a *API) putLimits(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	var body struct {
		Limits map[string]json.RawMessage `json:"limits"`
	}
	if !decodeBody(w, r, &body, false) {
		return
	}
	p, err := a.deps.Store.GetProject(r.Context(), slug)
	if err != nil {
		writeErr(w, err)
		return
	}
	limits, ok := flexStrings(w, body.Limits, "limits.")
	if !ok {
		return
	}
	p.Limits = limits
	if err := a.deps.Store.UpdateProject(r.Context(), actorOf(r), p); err != nil {
		writeErr(w, err)
		return
	}
	a.emit("project.update", map[string]any{"project": slug})
	v, err := a.limitsView(r.Context(), slug)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}
