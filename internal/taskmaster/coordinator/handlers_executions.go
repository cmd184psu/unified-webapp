package coordinator

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"cmd184psu/unified-webapp/internal/platform/response"
	"cmd184psu/unified-webapp/internal/taskmaster/db"
	"cmd184psu/unified-webapp/internal/taskmaster/models"
	"cmd184psu/unified-webapp/internal/taskmaster/worker"
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
	execs, err := c.db.ListExecutionsExcludingLanes(taskFilter, limit, c.hidden.Names())
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if execs == nil {
		execs = []*models.TaskExecution{}
	}
	// Pid/Suspended live only in the in-memory ProcessRegistry (never
	// persisted — the process, and any suspended state, is gone on
	// restart), so merge them in here rather than in the DB layer. The same
	// applies to a running func execution's unthrottled progress (§4.9): a
	// finished row's progress_pct/progress_label already carry the last
	// persisted value from the DB scan.
	for _, e := range execs {
		if pid, ok := c.procs.Get(e.ID); ok {
			p := pid
			e.Pid = &p
			e.Suspended = c.procs.Suspended(e.ID)
		}
		if e.Status == "running" {
			if p, ok := c.progress.Get(e.ID); ok {
				e.ProgressPct = p.Pct
				e.ProgressLabel = p.Label
			}
		}
	}
	response.WriteJSON(w, http.StatusOK, execs)
}

// handleRerunExecution creates a new pending execution reusing the same
// task/payload (FR-T5, §2.4). P18: a func task whose latest execution
// already succeeded is refused with 409 (golane.ErrSucceeded via
// db.ErrTaskSucceeded); shell tasks may re-run from any terminal status
// (R3).
func (c *Coordinator) handleRerunExecution(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, "invalid execution id: "+idStr)
		return
	}
	exec, err := c.db.GetExecution(id)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if exec == nil {
		response.WriteError(w, http.StatusNotFound, "no such execution: "+idStr)
		return
	}
	lane, _, found, err := c.db.ExecutionLane(id)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !found || c.laneHidden(lane) {
		response.WriteError(w, http.StatusNotFound, "no such execution: "+idStr)
		return
	}
	if exec.Status == "pending" || exec.Status == "running" {
		response.WriteError(w, http.StatusConflict, "execution is not finished")
		return
	}

	newID, err := c.db.RerunTask(exec.TaskName)
	if err != nil {
		switch {
		case errors.Is(err, db.ErrTaskNotFound):
			response.WriteError(w, http.StatusNotFound, "no such execution: "+idStr)
		case errors.Is(err, db.ErrTaskBusy):
			response.WriteError(w, http.StatusConflict, "task already has a queued or running execution")
		case errors.Is(err, db.ErrTaskSucceeded):
			response.WriteError(w, http.StatusConflict, "module-managed job already succeeded")
		default:
			response.WriteError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	c.publishBoard(worker.BoardEvent{Type: "task-enqueued", Lane: lane, Task: exec.TaskName, ExecutionID: newID})
	response.WriteJSON(w, http.StatusCreated, map[string]int64{"execution_id": newID})
}
