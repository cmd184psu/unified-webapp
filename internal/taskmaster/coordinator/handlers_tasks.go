package coordinator

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"cmd184psu/unified-webapp/internal/platform/response"
	"cmd184psu/unified-webapp/internal/taskmaster/models"
	"cmd184psu/unified-webapp/internal/taskmaster/worker"
)

func (c *Coordinator) handleListTasks(w http.ResponseWriter, r *http.Request) {
	laneFilter := r.URL.Query().Get("lane")
	tasks, err := c.db.ListTasks(laneFilter)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if tasks == nil {
		tasks = []*models.Task{}
	}
	response.WriteJSON(w, http.StatusOK, tasks)
}

// validateTaskPolicy enforces allow_sudo. The old allowed_types/task_type
// check is gone along with task "types" (FRD §6, plan D2/D4) — sudo is the
// only remaining gate.
func (c *Coordinator) validateTaskPolicy(sudo bool) (status int, msg string) {
	if sudo && !c.sudo.Allowed() {
		return http.StatusForbidden, "sudo tasks are disabled (enable allow_sudo to permit them)"
	}
	return 0, ""
}

func (c *Coordinator) handleAddTask(w http.ResponseWriter, r *http.Request) {
	var task models.Task
	if err := json.NewDecoder(r.Body).Decode(&task); err != nil {
		response.WriteDecodeError(w, err)
		return
	}
	if task.Name == "" || task.LaneName == "" {
		response.WriteError(w, http.StatusBadRequest, "name and lane_name are required")
		return
	}

	// validate lane exists
	l, err := c.db.GetLane(task.LaneName)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if l == nil {
		response.WriteError(w, http.StatusBadRequest, "unknown lane: "+task.LaneName)
		return
	}

	if status, msg := c.validateTaskPolicy(task.Sudo); status != 0 {
		response.WriteError(w, status, msg)
		return
	}

	_, err = c.db.AddTask(&task)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	created, _ := c.db.GetTask(task.Name)
	c.publishBoard(worker.BoardEvent{Type: "lane-updated", Lane: task.LaneName, Task: task.Name})
	response.WriteJSON(w, http.StatusCreated, created)
}

func (c *Coordinator) handleGetTask(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	task, err := c.db.GetTask(name)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if task == nil {
		response.WriteError(w, http.StatusNotFound, "task not found: "+name)
		return
	}
	response.WriteJSON(w, http.StatusOK, task)
}

func (c *Coordinator) handleUpdateTask(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	existing, err := c.db.GetTask(name)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if existing == nil {
		response.WriteError(w, http.StatusNotFound, "task not found: "+name)
		return
	}

	var updates map[string]any
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		response.WriteDecodeError(w, err)
		return
	}
	// Strip read-only fields
	delete(updates, "id")
	delete(updates, "name")
	delete(updates, "created_at")

	// Compute the effective post-merge sudo flag and validate it — covers
	// flipping sudo on via update.
	sudo := existing.Sudo
	if v, ok := updates["sudo"]; ok {
		b, ok := v.(bool)
		if !ok {
			response.WriteError(w, http.StatusBadRequest, `field "sudo" must be a bool`)
			return
		}
		sudo = b
	}

	if status, msg := c.validateTaskPolicy(sudo); status != 0 {
		response.WriteError(w, status, msg)
		return
	}

	if err := c.db.UpdateTask(name, updates); err != nil {
		response.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	updated, _ := c.db.GetTask(name)
	if updated != nil {
		c.publishBoard(worker.BoardEvent{Type: "lane-updated", Lane: updated.LaneName, Task: name})
	}
	response.WriteJSON(w, http.StatusOK, updated)
}

func (c *Coordinator) handleDeleteTask(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	existing, _ := c.db.GetTask(name)
	if err := c.db.DeleteTask(name); err != nil {
		response.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if existing != nil {
		c.publishBoard(worker.BoardEvent{Type: "lane-updated", Lane: existing.LaneName, Task: name})
	}
	w.WriteHeader(http.StatusNoContent)
}

func (c *Coordinator) handlePauseTask(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	existing, _ := c.db.GetTask(name)
	if err := c.db.SetTaskPaused(name, true); err != nil {
		response.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if existing != nil {
		c.publishBoard(worker.BoardEvent{Type: "lane-updated", Lane: existing.LaneName, Task: name})
	}
	response.WriteJSON(w, http.StatusOK, map[string]string{"status": "paused"})
}

func (c *Coordinator) handleResumeTask(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	existing, _ := c.db.GetTask(name)
	if err := c.db.SetTaskPaused(name, false); err != nil {
		response.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if existing != nil {
		c.publishBoard(worker.BoardEvent{Type: "lane-updated", Lane: existing.LaneName, Task: name})
	}
	response.WriteJSON(w, http.StatusOK, map[string]string{"status": "resumed"})
}

func (c *Coordinator) handleUpNext(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	execID, err := c.db.EnqueueTask(name, time.Now())
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	lane := ""
	if task, _ := c.db.GetTask(name); task != nil {
		lane = task.LaneName
	}
	c.publishBoard(worker.BoardEvent{Type: "task-enqueued", Lane: lane, Task: name, ExecutionID: execID})
	response.WriteJSON(w, http.StatusCreated, map[string]int64{"execution_id": execID})
}

// handleMoveTask moves a task to a different lane, validating the target
// lane exists first.
func (c *Coordinator) handleMoveTask(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	existing, err := c.db.GetTask(name)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if existing == nil {
		response.WriteError(w, http.StatusNotFound, "task not found: "+name)
		return
	}

	var req struct {
		LaneName string `json:"lane_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteDecodeError(w, err)
		return
	}
	if req.LaneName == "" {
		response.WriteError(w, http.StatusBadRequest, "lane_name is required")
		return
	}

	lane, err := c.db.GetLane(req.LaneName)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if lane == nil {
		response.WriteError(w, http.StatusBadRequest, "unknown lane: "+req.LaneName)
		return
	}

	maxPos, err := c.db.MaxTaskPosition(req.LaneName)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if err := c.db.UpdateTask(name, map[string]any{"lane_name": req.LaneName, "position": maxPos + 1}); err != nil {
		response.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	updated, _ := c.db.GetTask(name)
	c.publishBoard(worker.BoardEvent{Type: "lane-updated", Lane: existing.LaneName, Task: name})
	c.publishBoard(worker.BoardEvent{Type: "lane-updated", Lane: req.LaneName, Task: name})
	response.WriteJSON(w, http.StatusOK, updated)
}
