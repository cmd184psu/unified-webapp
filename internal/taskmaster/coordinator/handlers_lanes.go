package coordinator

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"cmd184psu/unified-webapp/internal/platform/response"
	"cmd184psu/unified-webapp/internal/taskmaster/models"
	"cmd184psu/unified-webapp/internal/taskmaster/worker"
)

func (c *Coordinator) handleListLanes(w http.ResponseWriter, r *http.Request) {
	lanes, err := c.db.ListLanes()
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	statuses := make([]models.LaneStatus, 0, len(lanes))
	for _, l := range lanes {
		count, _ := c.db.CountRunningInLane(l.Name)
		statuses = append(statuses, models.LaneStatus{Lane: *l, RunningCount: count})
	}
	response.WriteJSON(w, http.StatusOK, statuses)
}

func (c *Coordinator) handleGetLane(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	l, err := c.db.GetLane(name)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if l == nil {
		response.WriteError(w, http.StatusNotFound, "lane not found: "+name)
		return
	}
	count, _ := c.db.CountRunningInLane(name)
	response.WriteJSON(w, http.StatusOK, models.LaneStatus{Lane: *l, RunningCount: count})
}

func (c *Coordinator) handleCreateLane(w http.ResponseWriter, r *http.Request) {
	var l models.Lane
	if err := json.NewDecoder(r.Body).Decode(&l); err != nil {
		response.WriteDecodeError(w, err)
		return
	}
	if l.Name == "" {
		response.WriteError(w, http.StatusBadRequest, "name is required")
		return
	}
	if l.Width <= 0 {
		l.Width = 1
	}
	if err := c.db.UpsertLane(&l); err != nil {
		response.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	created, _ := c.db.GetLane(l.Name)
	c.publishBoard(worker.BoardEvent{Type: "lane-updated", Lane: l.Name})
	response.WriteJSON(w, http.StatusCreated, created)
}

func (c *Coordinator) handleUpdateLane(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	existing, err := c.db.GetLane(name)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if existing == nil {
		response.WriteError(w, http.StatusNotFound, "lane not found: "+name)
		return
	}
	var updates struct {
		Width *int `json:"width"`
	}
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		response.WriteDecodeError(w, err)
		return
	}
	if updates.Width != nil {
		existing.Width = *updates.Width
	}
	if err := c.db.UpsertLane(existing); err != nil {
		response.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	updated, _ := c.db.GetLane(name)
	c.publishBoard(worker.BoardEvent{Type: "lane-updated", Lane: name})
	response.WriteJSON(w, http.StatusOK, updated)
}

func (c *Coordinator) handleDeleteLane(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if err := c.db.DeleteLane(name); err != nil {
		response.WriteError(w, http.StatusConflict, err.Error())
		return
	}
	c.publishBoard(worker.BoardEvent{Type: "lane-updated", Lane: name})
	w.WriteHeader(http.StatusNoContent)
}

func (c *Coordinator) handlePauseLane(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	by := r.URL.Query().Get("by")
	if by == "" {
		by = "api"
	}
	if err := c.db.SetLanePaused(name, true, by); err != nil {
		response.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	c.publishBoard(worker.BoardEvent{Type: "lane-updated", Lane: name})
	response.WriteJSON(w, http.StatusOK, map[string]string{"status": "paused"})
}

func (c *Coordinator) handleResumeLane(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if err := c.db.SetLanePaused(name, false, ""); err != nil {
		response.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	c.publishBoard(worker.BoardEvent{Type: "lane-updated", Lane: name})
	response.WriteJSON(w, http.StatusOK, map[string]string{"status": "resumed"})
}

// handleSetLaneWidth updates only a lane's width, leaving its paused state
// untouched.
func (c *Coordinator) handleSetLaneWidth(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	existing, err := c.db.GetLane(name)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if existing == nil {
		response.WriteError(w, http.StatusNotFound, "lane not found: "+name)
		return
	}
	var req struct {
		Width *int `json:"width"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteDecodeError(w, err)
		return
	}
	if req.Width == nil || *req.Width < 1 {
		response.WriteError(w, http.StatusBadRequest, "width must be >= 1")
		return
	}
	if err := c.db.SetLaneWidth(name, *req.Width); err != nil {
		response.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	updated, _ := c.db.GetLane(name)
	c.publishBoard(worker.BoardEvent{Type: "lane-updated", Lane: name})
	response.WriteJSON(w, http.StatusOK, updated)
}

// handleSetLaneOrder reorders the tasks within a lane. The request body
// carries the desired task-name order; each task's position is set to its
// index in that list. Names that don't belong to this lane are silently
// ignored (the underlying UPDATE is scoped to lane_name, so it's a no-op
// for them) rather than rejected — this keeps drag-reorder resilient to a
// stale client-side snapshot.
func (c *Coordinator) handleSetLaneOrder(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	existing, err := c.db.GetLane(name)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if existing == nil {
		response.WriteError(w, http.StatusNotFound, "lane not found: "+name)
		return
	}
	var req struct {
		Order []string `json:"order"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteDecodeError(w, err)
		return
	}
	if err := c.db.SetTaskPositions(name, req.Order); err != nil {
		response.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	tasks, err := c.db.ListTasks(name)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if tasks == nil {
		tasks = []*models.Task{}
	}
	c.publishBoard(worker.BoardEvent{Type: "lane-updated", Lane: name})
	response.WriteJSON(w, http.StatusOK, tasks)
}
