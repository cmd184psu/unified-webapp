package coordinator

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"cmd184psu/unified-webapp/internal/platform/response"
	"cmd184psu/unified-webapp/internal/taskmaster/db"
	"cmd184psu/unified-webapp/internal/taskmaster/models"
	"cmd184psu/unified-webapp/internal/taskmaster/worker"
)

func (c *Coordinator) handleListTasks(w http.ResponseWriter, r *http.Request) {
	laneFilter := r.URL.Query().Get("lane")
	if laneFilter != "" && c.laneHidden(laneFilter) {
		response.WriteJSON(w, http.StatusOK, []*models.Task{})
		return
	}
	tasks, err := c.db.ListTasks(laneFilter)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	visible := make([]*models.Task, 0, len(tasks))
	for _, t := range tasks {
		if c.laneHidden(t.LaneName) {
			continue
		}
		visible = append(visible, t)
	}
	response.WriteJSON(w, http.StatusOK, visible)
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
	// kind/label/payload/payload_version are set only by a module through
	// golane.Lane.Submit, never over HTTP (P2/D2).
	if task.Kind != "" {
		response.WriteError(w, http.StatusBadRequest, `"kind" cannot be set over HTTP`)
		return
	}

	// A hidden lane is treated as if it doesn't exist (N4); an owned but
	// visible lane exists but is off-limits to plain task creation (P7/P20).
	if c.laneHidden(task.LaneName) {
		response.WriteError(w, http.StatusBadRequest, "unknown lane: "+task.LaneName)
		return
	}
	l, err := c.db.GetLane(task.LaneName)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if l == nil {
		response.WriteError(w, http.StatusBadRequest, "unknown lane: "+task.LaneName)
		return
	}
	if l.Owner != "" {
		response.WriteError(w, http.StatusBadRequest, fmt.Sprintf("lane %q is managed by module %q", l.Name, l.Owner))
		return
	}

	if existing, _ := c.db.GetTask(task.Name); existing != nil && existing.Kind != "" {
		owner := ""
		if el, _ := c.db.GetLane(existing.LaneName); el != nil {
			owner = el.Owner
		}
		response.WriteError(w, http.StatusConflict, fmt.Sprintf("task %q is managed by module %q", task.Name, owner))
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
	if task == nil || c.laneHidden(task.LaneName) {
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
	if existing == nil || c.laneHidden(existing.LaneName) {
		response.WriteError(w, http.StatusNotFound, "task not found: "+name)
		return
	}
	if c.funcTaskGuard(w, existing) {
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

	if v, ok := updates["lane_name"]; ok {
		laneName, _ := v.(string)
		if l, _ := c.db.GetLane(laneName); l != nil && l.Owner != "" {
			response.WriteError(w, http.StatusBadRequest, fmt.Sprintf("lane %q is managed by module %q", l.Name, l.Owner))
			return
		}
	}

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
		if errors.Is(err, db.ErrUnknownTaskField) {
			response.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
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
	if existing == nil || c.laneHidden(existing.LaneName) {
		response.WriteError(w, http.StatusNotFound, "task not found: "+name)
		return
	}
	if existing.Kind != "" {
		// N3: a func task refuses deletion while it has a running
		// execution; a shell task's delete stays unconditional (R3).
		if err := c.db.RemoveIdleTask(name); err != nil {
			if errors.Is(err, db.ErrTaskBusy) {
				response.WriteError(w, http.StatusConflict, "task is running")
				return
			}
			response.WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
		c.publishBoard(worker.BoardEvent{Type: "lane-updated", Lane: existing.LaneName, Task: name})
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := c.db.DeleteTask(name); err != nil {
		response.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	c.publishBoard(worker.BoardEvent{Type: "lane-updated", Lane: existing.LaneName, Task: name})
	w.WriteHeader(http.StatusNoContent)
}

func (c *Coordinator) handlePauseTask(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	existing, _ := c.db.GetTask(name)
	if existing == nil || c.laneHidden(existing.LaneName) {
		response.WriteError(w, http.StatusNotFound, "task not found: "+name)
		return
	}
	if c.funcTaskGuard(w, existing) {
		return
	}
	if err := c.db.SetTaskPaused(name, true); err != nil {
		response.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	c.publishBoard(worker.BoardEvent{Type: "lane-updated", Lane: existing.LaneName, Task: name})
	response.WriteJSON(w, http.StatusOK, map[string]string{"status": "paused"})
}

func (c *Coordinator) handleResumeTask(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	existing, _ := c.db.GetTask(name)
	if existing == nil || c.laneHidden(existing.LaneName) {
		response.WriteError(w, http.StatusNotFound, "task not found: "+name)
		return
	}
	if c.funcTaskGuard(w, existing) {
		return
	}
	if err := c.db.SetTaskPaused(name, false); err != nil {
		response.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	c.publishBoard(worker.BoardEvent{Type: "lane-updated", Lane: existing.LaneName, Task: name})
	response.WriteJSON(w, http.StatusOK, map[string]string{"status": "resumed"})
}

func (c *Coordinator) handleUpNext(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	task, err := c.db.GetTask(name)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if task == nil || c.laneHidden(task.LaneName) {
		response.WriteError(w, http.StatusNotFound, "task not found: "+name)
		return
	}
	if task.Kind != "" {
		response.WriteError(w, http.StatusConflict, "use POST /api/executions/{id}/rerun for module-managed tasks")
		return
	}
	execID, err := c.db.EnqueueTask(name, time.Now())
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	c.publishBoard(worker.BoardEvent{Type: "task-enqueued", Lane: task.LaneName, Task: name, ExecutionID: execID})
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
	if existing == nil || c.laneHidden(existing.LaneName) {
		response.WriteError(w, http.StatusNotFound, "task not found: "+name)
		return
	}
	if c.funcTaskGuard(w, existing) {
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

	if c.laneHidden(req.LaneName) {
		response.WriteError(w, http.StatusBadRequest, "unknown lane: "+req.LaneName)
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
	if lane.Owner != "" {
		response.WriteError(w, http.StatusBadRequest, fmt.Sprintf("lane %q is managed by module %q", lane.Name, lane.Owner))
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
