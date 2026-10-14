package api

import (
	"fmt"
	"net/http"

	"cmd184psu/unified-webapp/internal/agentflow/engine"
	"cmd184psu/unified-webapp/internal/agentflow/model"
	"cmd184psu/unified-webapp/internal/agentflow/store"
)

func (a *API) registerTasks(mux muxer) {
	const e = "/api/projects/{slug}/efforts/{id}"
	mux.HandleFunc("GET "+e+"/tasks", a.listTasks)
	mux.HandleFunc("GET "+e+"/tasks/{key}", a.getTask)
	mux.HandleFunc("PUT "+e+"/tasks/{key}", a.editTask)
	mux.HandleFunc("POST "+e+"/tasks/{key}/retry", a.retryTask)
	mux.HandleFunc("POST "+e+"/tasks/{key}/override", a.overrideTask)
	mux.HandleFunc("POST "+e+"/abandon", a.abandonEffort)
	mux.HandleFunc("GET "+e+"/git-commands", a.gitCommands)
	mux.HandleFunc("POST "+e+"/yolo", a.setYOLO)
	mux.HandleFunc("GET /api/yolo/invariants", a.yoloInvariants)
}

// TaskView is a task with its derived status (never stored) and where it is.
type TaskView struct {
	model.Task
	Status   model.Status `json:"status"`
	Halted   bool         `json:"halted"`
	Attempts int          `json:"attempts"`
	Current  *WorkItem    `json:"current"`
}

// AttemptView is an attempt with its stages.
type AttemptView struct {
	model.Attempt
	Stages []model.Stage `json:"stages"`
}

// TaskDetail is the board's detail pane: the task, its attempts with their
// stages (VerifyReport and review issues are in the stages' parsed output) and
// its progress log.
type TaskDetail struct {
	TaskView
	AttemptList []AttemptView `json:"attempt_list"`
	Progress    []string      `json:"progress"`
}

func (a *API) taskViews(r *http.Request, ef model.Effort) ([]TaskView, error) {
	ctx := r.Context()
	statuses, err := a.deps.Engine.TaskStatuses(ctx, ef.Project, ef.ID)
	if err != nil {
		return nil, err
	}
	tasks, err := a.deps.Store.ListTasks(ctx, ef.ID)
	if err != nil {
		return nil, err
	}
	work, err := a.currentWork(ctx, ef.ID, statuses)
	if err != nil {
		return nil, err
	}
	out := make([]TaskView, 0, len(tasks))
	for _, t := range tasks {
		atts, err := a.deps.Store.ListAttempts(ctx, t.ID)
		if err != nil {
			return nil, err
		}
		v := TaskView{Task: t, Status: statuses[t.Key], Halted: statuses[t.Key].Halted(), Attempts: len(atts)}
		for i := range work {
			if work[i].Task == t.Key {
				v.Current = &work[i]
			}
		}
		out = append(out, v)
	}
	return out, nil
}

func (a *API) listTasks(w http.ResponseWriter, r *http.Request) {
	ef, ok := a.loadEffort(w, r)
	if !ok {
		return
	}
	out, err := a.taskViews(r, ef)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// taskByKey finds the task of the effort's queue named by {key}.
func (a *API) taskByKey(w http.ResponseWriter, r *http.Request, ef model.Effort) (model.Task, bool) {
	tasks, err := a.deps.Store.ListTasks(r.Context(), ef.ID)
	if err != nil {
		writeErr(w, err)
		return model.Task{}, false
	}
	key := r.PathValue("key")
	for _, t := range tasks {
		if t.Key == key {
			return t, true
		}
	}
	writeErr(w, fmt.Errorf("task %q in effort %d: %w", key, ef.ID, store.ErrNotFound))
	return model.Task{}, false
}

func (a *API) getTask(w http.ResponseWriter, r *http.Request) {
	ef, ok := a.loadEffort(w, r)
	if !ok {
		return
	}
	t, ok := a.taskByKey(w, r, ef)
	if !ok {
		return
	}
	views, err := a.taskViews(r, ef)
	if err != nil {
		writeErr(w, err)
		return
	}
	d := TaskDetail{AttemptList: []AttemptView{}}
	for _, v := range views {
		if v.ID == t.ID {
			d.TaskView = v
		}
	}
	atts, err := a.deps.Store.ListAttempts(r.Context(), t.ID)
	if err != nil {
		writeErr(w, err)
		return
	}
	for _, at := range atts {
		stages, err := a.deps.Store.ListAttemptStages(r.Context(), at.ID)
		if err != nil {
			writeErr(w, err)
			return
		}
		if stages == nil {
			stages = []model.Stage{}
		}
		d.AttemptList = append(d.AttemptList, AttemptView{Attempt: at, Stages: stages})
	}
	if d.Progress, err = a.deps.Store.ListProgress(r.Context(), t.ID, 200); err != nil {
		writeErr(w, err)
		return
	}
	if d.Progress == nil {
		d.Progress = []string{}
	}
	writeJSON(w, http.StatusOK, d)
}

// editTask replaces one task's definition; the queue returns to draft (AT9,
// QA2). The key comes from the path.
func (a *API) editTask(w http.ResponseWriter, r *http.Request) {
	ef, ok := a.loadEffort(w, r)
	if !ok {
		return
	}
	var def model.TaskDef
	if !decodeBody(w, r, &def, false) {
		return
	}
	if def.Key != "" && def.Key != r.PathValue("key") {
		badRequest(w, "key", "a task's key cannot be changed")
		return
	}
	def.Key = r.PathValue("key")
	if _, err := a.deps.Engine.EditTask(r.Context(), actorOf(r), ef.ID, def); err != nil {
		writeErr(w, err)
		return
	}
	a.emit("task.update", map[string]any{"project": ef.Project, "effort": ef.ID, "task": def.Key})
	a.respondQueue(w, r, ef)
}

func (a *API) respondQueue(w http.ResponseWriter, r *http.Request, ef model.Effort) {
	v, err := a.deps.Engine.QueueView(r.Context(), ef.ID)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

// retryTask resets a halted task's counters; clean also drops its worktree.
func (a *API) retryTask(w http.ResponseWriter, r *http.Request) {
	ef, ok := a.loadEffort(w, r)
	if !ok {
		return
	}
	var body struct {
		Clean bool `json:"clean"`
	}
	if !decodeBody(w, r, &body, true) {
		return
	}
	t, ok := a.taskByKey(w, r, ef)
	if !ok {
		return
	}
	if err := a.deps.Engine.Retry(r.Context(), actorOf(r), t.ID, body.Clean); err != nil {
		writeErr(w, err)
		return
	}
	a.taskChanged(w, r, ef, t.Key)
}

// overrideTask marks a halted task passed by the operator, with a reason.
func (a *API) overrideTask(w http.ResponseWriter, r *http.Request) {
	ef, ok := a.loadEffort(w, r)
	if !ok {
		return
	}
	var body struct {
		Reason string `json:"reason"`
	}
	if !decodeBody(w, r, &body, false) {
		return
	}
	t, ok := a.taskByKey(w, r, ef)
	if !ok {
		return
	}
	if err := a.deps.Engine.Override(r.Context(), actorOf(r), t.ID, body.Reason); err != nil {
		writeErr(w, err)
		return
	}
	a.taskChanged(w, r, ef, t.Key)
}

// taskChanged publishes the change and answers with the task's new detail.
func (a *API) taskChanged(w http.ResponseWriter, r *http.Request, ef model.Effort, key string) {
	a.emit("task.update", map[string]any{"project": ef.Project, "effort": ef.ID, "task": key})
	a.getTask(w, r)
}

func (a *API) abandonEffort(w http.ResponseWriter, r *http.Request) {
	ef, ok := a.loadEffort(w, r)
	if !ok {
		return
	}
	if err := a.deps.Engine.AbandonEffort(r.Context(), actorOf(r), ef.ID); err != nil {
		writeErr(w, err)
		return
	}
	a.emit("effort.update", map[string]any{"project": ef.Project, "effort": ef.ID})
	a.respondEffort(w, r, ef.ID, http.StatusOK)
}

// setYOLO turns YOLO on (needs confirm) or off (YO1, YO3).
func (a *API) setYOLO(w http.ResponseWriter, r *http.Request) {
	ef, ok := a.loadEffort(w, r)
	if !ok {
		return
	}
	var body struct {
		On      bool `json:"on"`
		Confirm bool `json:"confirm"`
	}
	if !decodeBody(w, r, &body, false) {
		return
	}
	if err := a.deps.Engine.SetYOLO(r.Context(), actorOf(r), ef.ID, body.On, body.Confirm); err != nil {
		writeErr(w, err)
		return
	}
	a.emit("effort.update", map[string]any{"project": ef.Project, "effort": ef.ID})
	a.respondEffort(w, r, ef.ID, http.StatusOK)
}

// yoloInvariants lists what stays enforced under YOLO.
func (a *API) yoloInvariants(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, engine.EnforcedInvariants())
}

// gitCommands lists the commands a human runs to merge and push a completed
// effort's integration branch (GIT4). Agentflow runs none of them; an effort
// that is not complete answers 409.
func (a *API) gitCommands(w http.ResponseWriter, r *http.Request) {
	ef, ok := a.loadEffort(w, r)
	if !ok {
		return
	}
	cmds, err := a.deps.Engine.PushInstructions(r.Context(), ef.ID)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"commands": cmds})
}
