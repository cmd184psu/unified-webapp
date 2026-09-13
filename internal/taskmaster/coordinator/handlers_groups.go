package coordinator

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"cmd184psu/unified-webapp/internal/platform/response"
	"cmd184psu/unified-webapp/internal/taskmaster/models"
)

func (c *Coordinator) handleListGroups(w http.ResponseWriter, r *http.Request) {
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

func (c *Coordinator) handleGetGroup(w http.ResponseWriter, r *http.Request) {
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

func (c *Coordinator) handleCreateGroup(w http.ResponseWriter, r *http.Request) {
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
	response.WriteJSON(w, http.StatusCreated, created)
}

func (c *Coordinator) handleUpdateGroup(w http.ResponseWriter, r *http.Request) {
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
	response.WriteJSON(w, http.StatusOK, updated)
}

func (c *Coordinator) handleDeleteGroup(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if err := c.db.DeleteLane(name); err != nil {
		response.WriteError(w, http.StatusConflict, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (c *Coordinator) handlePauseGroup(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	by := r.URL.Query().Get("by")
	if by == "" {
		by = "api"
	}
	if err := c.db.SetLanePaused(name, true, by); err != nil {
		response.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.WriteJSON(w, http.StatusOK, map[string]string{"status": "paused"})
}

func (c *Coordinator) handleResumeGroup(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if err := c.db.SetLanePaused(name, false, ""); err != nil {
		response.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.WriteJSON(w, http.StatusOK, map[string]string{"status": "resumed"})
}
