package coordinator

import (
	"net/http"
	"strconv"

	"cmd184psu/unified-webapp/internal/platform/response"
	"cmd184psu/unified-webapp/internal/taskmaster/models"
)

func (c *Coordinator) handleListExecutions(w http.ResponseWriter, r *http.Request) {
	taskFilter := r.URL.Query().Get("task")
	limitStr := r.URL.Query().Get("limit")
	limit := 50
	if limitStr != "" {
		if n, err := strconv.Atoi(limitStr); err == nil && n > 0 {
			limit = n
		}
	}
	execs, err := c.db.ListExecutions(taskFilter, limit)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if execs == nil {
		execs = []*models.TaskExecution{}
	}
	// Pid/Suspended live only in the in-memory ProcessRegistry (never
	// persisted — the process, and any suspended state, is gone on
	// restart), so merge them in here rather than in the DB layer.
	for _, e := range execs {
		if pid, ok := c.procs.Get(e.ID); ok {
			p := pid
			e.Pid = &p
			e.Suspended = c.procs.Suspended(e.ID)
		}
	}
	response.WriteJSON(w, http.StatusOK, execs)
}
