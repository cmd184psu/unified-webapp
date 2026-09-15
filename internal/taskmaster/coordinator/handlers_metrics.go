package coordinator

import (
	"net/http"
	"strconv"

	"cmd184psu/unified-webapp/internal/platform/response"
	"cmd184psu/unified-webapp/internal/taskmaster/models"
)

func (c *Coordinator) handleMetrics(w http.ResponseWriter, r *http.Request) {
	laneFilter := r.URL.Query().Get("lane")
	taskFilter := r.URL.Query().Get("task")
	hours := 24
	if h := r.URL.Query().Get("hours"); h != "" {
		if n, err := strconv.Atoi(h); err == nil && n > 0 {
			hours = n
		}
	}
	summaries, err := c.db.GetMetrics(laneFilter, taskFilter, hours)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if summaries == nil {
		summaries = []*models.MetricSummary{}
	}
	response.WriteJSON(w, http.StatusOK, summaries)
}
