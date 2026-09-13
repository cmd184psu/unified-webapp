package coordinator

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"cmd184psu/unified-webapp/internal/platform/response"
	"cmd184psu/unified-webapp/internal/taskmaster/models"
)

func (c *Coordinator) handleListTasks(w http.ResponseWriter, r *http.Request) {
	groupFilter := r.URL.Query().Get("group")
	tasks, err := c.db.ListTasks(groupFilter)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if tasks == nil {
		tasks = []*models.Task{}
	}
	response.WriteJSON(w, http.StatusOK, tasks)
}

// validateTaskPolicy enforces allow_sudo and the group's allowed_types.
func (c *Coordinator) validateTaskPolicy(taskType, groupName string, sudo bool) (status int, msg string) {
	if sudo && !c.sudo.Allowed() {
		return http.StatusForbidden, "sudo tasks are disabled (enable allow_sudo to permit them)"
	}
	g, err := c.db.GetGroup(groupName)
	if err != nil {
		return http.StatusInternalServerError, err.Error()
	}
	if g != nil && len(g.AllowedTypes) > 0 {
		allowed := false
		for _, t := range g.AllowedTypes {
			if t == taskType {
				allowed = true
				break
			}
		}
		if !allowed {
			return http.StatusBadRequest, fmt.Sprintf("task_type %q not permitted in group %q (allowed: %v)", taskType, groupName, g.AllowedTypes)
		}
	}
	return 0, ""
}

func (c *Coordinator) handleAddTask(w http.ResponseWriter, r *http.Request) {
	var task models.Task
	if err := json.NewDecoder(r.Body).Decode(&task); err != nil {
		response.WriteDecodeError(w, err)
		return
	}
	if task.Name == "" || task.GroupName == "" || task.TaskType == "" {
		response.WriteError(w, http.StatusBadRequest, "name, group_name, and task_type are required")
		return
	}
	if task.Args == "" {
		task.Args = "{}"
	}
	if !json.Valid([]byte(task.Args)) {
		response.WriteError(w, http.StatusBadRequest, "args must be a valid JSON object")
		return
	}
	if task.Priority == 0 {
		task.Priority = 50
	}

	// validate group exists
	g, err := c.db.GetGroup(task.GroupName)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if g == nil {
		response.WriteError(w, http.StatusBadRequest, "unknown group: "+task.GroupName)
		return
	}

	if status, msg := c.validateTaskPolicy(task.TaskType, task.GroupName, task.Sudo); status != 0 {
		response.WriteError(w, status, msg)
		return
	}

	_, err = c.db.AddTask(&task)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	created, _ := c.db.GetTask(task.Name)
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

	if argsVal, ok := updates["args"]; ok {
		argsStr, _ := argsVal.(string)
		if argsStr == "" {
			updates["args"] = "{}"
		} else if !json.Valid([]byte(argsStr)) {
			response.WriteError(w, http.StatusBadRequest, "args must be a valid JSON object")
			return
		}
	}

	// Compute the effective post-merge (task_type, group_name, sudo) triple and
	// validate it — covers moving a task into a stricter group and flipping sudo on.
	taskType := existing.TaskType
	if v, ok := updates["task_type"]; ok {
		s, ok := v.(string)
		if !ok {
			response.WriteError(w, http.StatusBadRequest, fmt.Sprintf("field %q must be a %s", "task_type", "string"))
			return
		}
		taskType = s
	}
	groupName := existing.GroupName
	if v, ok := updates["group_name"]; ok {
		s, ok := v.(string)
		if !ok {
			response.WriteError(w, http.StatusBadRequest, fmt.Sprintf("field %q must be a %s", "group_name", "string"))
			return
		}
		groupName = s
	}
	sudo := existing.Sudo
	if v, ok := updates["sudo"]; ok {
		b, ok := v.(bool)
		if !ok {
			response.WriteError(w, http.StatusBadRequest, fmt.Sprintf("field %q must be a %s", "sudo", "bool"))
			return
		}
		sudo = b
	}

	if status, msg := c.validateTaskPolicy(taskType, groupName, sudo); status != 0 {
		response.WriteError(w, status, msg)
		return
	}

	if err := c.db.UpdateTask(name, updates); err != nil {
		response.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	updated, _ := c.db.GetTask(name)
	response.WriteJSON(w, http.StatusOK, updated)
}

func (c *Coordinator) handleDeleteTask(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if err := c.db.DeleteTask(name); err != nil {
		response.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (c *Coordinator) handlePauseTask(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if err := c.db.SetTaskPaused(name, true); err != nil {
		response.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.WriteJSON(w, http.StatusOK, map[string]string{"status": "paused"})
}

func (c *Coordinator) handleResumeTask(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if err := c.db.SetTaskPaused(name, false); err != nil {
		response.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.WriteJSON(w, http.StatusOK, map[string]string{"status": "resumed"})
}

func (c *Coordinator) handleEnqueueTask(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	execID, err := c.db.EnqueueTask(name, time.Now())
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.WriteJSON(w, http.StatusCreated, map[string]int64{"execution_id": execID})
}
