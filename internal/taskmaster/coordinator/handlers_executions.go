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
	response.WriteJSON(w, http.StatusOK, execs)
}
