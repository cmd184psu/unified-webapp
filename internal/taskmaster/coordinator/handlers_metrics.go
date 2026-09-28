package coordinator

import (
	"net/http"
	"strconv"

	"cmd184psu/unified-webapp/internal/platform/response"
	"cmd184psu/unified-webapp/internal/taskmaster/models"
)

// metricsIncludeHiddenLanes: owner decision 2026-09-27 (FRD Q4) — hidden
// lanes are excluded from Metrics "for the time being". Flip to true (and
// flip TestMetrics_HiddenLaneExcludedByPolicy) to include them.
const metricsIncludeHiddenLanes = false

func (c *Coordinator) handleMetrics(w http.ResponseWriter, r *http.Request) {
	laneFilter := r.URL.Query().Get("lane")
	taskFilter := r.URL.Query().Get("task")
	hours := 24
	if h := r.URL.Query().Get("hours"); h != "" {
		if n, err := strconv.Atoi(h); err == nil && n > 0 {
			hours = n
		}
	}
	var exclude []string
	if !metricsIncludeHiddenLanes {
		exclude = c.hidden.Names()
	}
	summaries, err := c.db.GetMetricsExcludingLanes(laneFilter, taskFilter, hours, exclude)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if summaries == nil {
		summaries = []*models.MetricSummary{}
	}
	response.WriteJSON(w, http.StatusOK, summaries)
}
