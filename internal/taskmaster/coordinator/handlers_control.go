package coordinator

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"cmd184psu/unified-webapp/internal/platform/response"
	"cmd184psu/unified-webapp/internal/taskmaster/db"
	"cmd184psu/unified-webapp/internal/taskmaster/worker"
)

// writeNotRunningResult answers a pause/resume/cancel request for an
// execution ID that isn't currently registered as a controllable process.
// That is NOT necessarily a client mistake: an execution that finished
// naturally between the board rendering it as running and this request
// landing is a normal, expected outcome (fast tasks especially), not an
// error — so a real execution ID gets a plain 200 "not_running" no-op
// instead of a 404. Only an ID that doesn't correspond to any execution at
// all is a genuine 404, distinguished here by looking the row up in the DB.
func writeNotRunningResult(w http.ResponseWriter, database *db.DB, id int64) {
	exec, err := database.GetExecution(id)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if exec == nil {
		response.WriteError(w, http.StatusNotFound, "no such execution: "+strconv.FormatInt(id, 10))
		return
	}
	response.WriteJSON(w, http.StatusOK, map[string]string{"status": "not_running"})
}

// handleCancelExecution cancels a running execution by ID. The "canceled"
// status itself is written by the worker's runTask once Execute unwinds —
// this handler only signals the cancel and reports whether the execution
// was actually running (registered) to cancel.
func (c *Coordinator) handleCancelExecution(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, "invalid execution id: "+idStr)
		return
	}
	if !c.cancels.Cancel(id) {
		writeNotRunningResult(w, c.db, id)
		return
	}
	response.WriteJSON(w, http.StatusOK, map[string]string{"status": "canceling"})
}

// handlePauseExecution suspends (SIGSTOP, escalated via sudo when the task
// ran with sudo) a running execution's whole process group. Status stays
// "running" in the DB — Suspended is in-memory only, merged into
// /api/executions by handleListExecutions.
func (c *Coordinator) handlePauseExecution(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, "invalid execution id: "+idStr)
		return
	}
	if err := c.procs.Suspend(id); err != nil {
		if errors.Is(err, worker.ErrProcessNotRunning) {
			writeNotRunningResult(w, c.db, id)
			return
		}
		response.WriteError(w, http.StatusBadRequest, "pause failed: "+err.Error())
		return
	}
	response.WriteJSON(w, http.StatusOK, map[string]string{"status": "paused"})
}

// handleResumeExecution sends SIGCONT to a paused execution's process group,
// mirroring handlePauseExecution.
func (c *Coordinator) handleResumeExecution(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, "invalid execution id: "+idStr)
		return
	}
	if err := c.procs.Resume(id); err != nil {
		if errors.Is(err, worker.ErrProcessNotRunning) {
			writeNotRunningResult(w, c.db, id)
			return
		}
		response.WriteError(w, http.StatusBadRequest, "resume failed: "+err.Error())
		return
	}
	response.WriteJSON(w, http.StatusOK, map[string]string{"status": "resumed"})
}

// handleGetBrake reports whether the hand brake is currently engaged.
func (c *Coordinator) handleGetBrake(w http.ResponseWriter, r *http.Request) {
	response.WriteJSON(w, http.StatusOK, map[string]bool{"engaged": c.brake.Engaged()})
}

// handleEngageBrake sets the hand brake: pauses every lane that isn't
// already paused (recording which ones, so release restores exactly the
// pre-brake pause state), cancels every currently running execution, and
// persists the flag so a restart boots braked.
//
// Idempotent: if the brake is already engaged, this is a no-op that just
// reports the current state. Without this guard, a second engage call would
// see every lane already paused (by the first engage), recompute an empty
// "paused by brake" set, and overwrite SettingBrakePausedLanes with it —
// losing the original restore set and leaving lanes stuck paused after
// release.
func (c *Coordinator) handleEngageBrake(w http.ResponseWriter, r *http.Request) {
	c.brakeMu.Lock()
	defer c.brakeMu.Unlock()

	if c.brake.Engaged() {
		response.WriteJSON(w, http.StatusOK, map[string]bool{"engaged": true})
		return
	}

	lanes, err := c.db.ListLanes()
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	pausedByBrake := []string{}
	for _, l := range lanes {
		if l.Paused {
			continue
		}
		if err := c.db.SetLanePaused(l.Name, true, "brake"); err != nil {
			response.WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
		pausedByBrake = append(pausedByBrake, l.Name)
	}
	if err := c.db.SetBrakePausedLanes(pausedByBrake); err != nil {
		response.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	running, err := c.db.ListRunningExecutionIDs()
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	for _, id := range running {
		c.cancels.Cancel(id)
	}

	if err := c.db.SetSetting(db.SettingBrakeEngaged, db.EncodeBoolSetting(true)); err != nil {
		response.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	c.brake.Set(true)
	c.publishBoard(worker.EngagedEvent(true))
	response.WriteJSON(w, http.StatusOK, map[string]bool{"engaged": true})
}

// handleReleaseBrake clears the hand brake: unpauses exactly the lanes the
// brake paused (a lane paused independently of the brake stays paused),
// clears the recorded set, and persists the released flag.
func (c *Coordinator) handleReleaseBrake(w http.ResponseWriter, r *http.Request) {
	c.brakeMu.Lock()
	defer c.brakeMu.Unlock()

	lanes, err := c.db.GetBrakePausedLanes()
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	for _, name := range lanes {
		if err := c.db.SetLanePaused(name, false, ""); err != nil {
			response.WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if err := c.db.SetBrakePausedLanes(nil); err != nil {
		response.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := c.db.SetSetting(db.SettingBrakeEngaged, db.EncodeBoolSetting(false)); err != nil {
		response.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	c.brake.Set(false)
	c.publishBoard(worker.EngagedEvent(false))
	response.WriteJSON(w, http.StatusOK, map[string]bool{"engaged": false})
}
