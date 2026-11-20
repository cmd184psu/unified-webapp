package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"sync"
	"syscall"
	"time"

	"cmd184psu/unified-webapp/internal/agentflow/model"
	"cmd184psu/unified-webapp/internal/agentflow/store"
)

// restartMaxWait bounds how long a restart waits for running stages to end
// before it becomes a service hold (AF-10); a worker stage may run for its
// whole timeout. A restart asked for with wait=hold does not use it.
var restartMaxWait = 60 * time.Minute

// holdRestartReason is the reason a hold carries when a drain restart ran out of
// time; the last open stage closing finishes that restart.
const holdRestartReason = "restart waiting for long stage"

// restartPoll is how often a pending restart looks for the work to have ended.
var restartPoll = 2 * time.Second

func (a *API) registerService(mux muxer) {
	mux.HandleFunc("GET /api/service/restart", a.restartStatus)
	mux.HandleFunc("POST /api/service/restart", a.restartService)
	mux.HandleFunc("DELETE /api/service/restart", a.cancelRestart)
}

// restartPlan is a restart that is waiting for running work to end.
type restartPlan struct {
	Requested time.Time `json:"requested"`
	// CancelRunning says running stages were cancelled instead of awaited.
	CancelRunning bool `json:"cancel_running"`
	// Resume says the projects in Paused are started again once the service is back.
	Resume bool `json:"resume"`
	// Paused are the projects that were running and were paused (or stopped) for
	// the restart; cancelling the restart starts them again.
	Paused []string `json:"paused"`
	// Wait is "hold" when the restart must never give up on a long stage: no
	// deadline applies, so it drains for as long as the work takes.
	Wait string `json:"wait,omitempty"`
	// Held is set when the deadline passed and the restart became a service
	// hold (AF-10): the projects stay paused and the restart finishes when the
	// last stage closes, or is abandoned when the hold is lifted.
	Held   bool `json:"held,omitempty"`
	cancel context.CancelFunc
}

// RestartView is the restart state GET /api/service/restart reports.
type RestartView struct {
	// State is "idle", "draining" or "held" (a drain that became a hold).
	State      string       `json:"state"`
	Plan       *restartPlan `json:"plan,omitempty"`
	OpenStages int          `json:"open_stages"`
	PID        int          `json:"pid"`
	Started    int64        `json:"started"`
}

type restartState struct {
	mu   sync.Mutex
	plan *restartPlan
}

var processStarted = time.Now()

func (a *API) terminate() {
	if a.deps.Terminate != nil {
		a.deps.Terminate()
		return
	}
	// The service shuts down gracefully on SIGTERM and systemd (Restart=always)
	// starts it again, on whatever binary is then on disk.
	_ = syscall.Kill(os.Getpid(), syscall.SIGTERM)
}

func (a *API) restartView(ctx context.Context) RestartView {
	v := RestartView{State: "idle", PID: os.Getpid(), Started: processStarted.UnixMilli()}
	if open, err := a.deps.Engine.OpenStagesExceptSeats(ctx); err == nil {
		v.OpenStages = open
	}
	a.restart.mu.Lock()
	defer a.restart.mu.Unlock()
	if a.restart.plan != nil {
		v.State, v.Plan = "draining", a.restart.plan
		if a.restart.plan.Held {
			v.State = "held"
		}
	}
	return v
}

// restartPlanNow returns the pending restart plan, if any.
func (a *API) restartPlanNow() *restartPlan {
	a.restart.mu.Lock()
	defer a.restart.mu.Unlock()
	return a.restart.plan
}

// takePlan removes the pending restart plan and stops its watcher, returning it.
func (a *API) takePlan() *restartPlan {
	a.restart.mu.Lock()
	plan := a.restart.plan
	a.restart.plan = nil
	a.restart.mu.Unlock()
	if plan != nil && plan.cancel != nil {
		plan.cancel()
	}
	return plan
}

func (a *API) restartStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, a.restartView(r.Context()))
}

// restartService pauses every running project, waits for the running stages to
// end (or cancels them when asked), then ends the process so the supervisor
// starts it again. It never cuts a stage short unless cancel_running is set.
func (a *API) restartService(w http.ResponseWriter, r *http.Request) {
	var body struct {
		CancelRunning bool `json:"cancel_running"`
		// Resume starts again, after the restart, the projects that were running;
		// absent means yes.
		Resume *bool `json:"resume"`
		// Wait is "hold" to drain for as long as it takes instead of becoming a
		// hold after restartMaxWait (AF-10); empty is the usual deadline.
		Wait string `json:"wait"`
	}
	if !decodeBody(w, r, &body, true) {
		return
	}
	if body.Wait != "" && body.Wait != "hold" {
		writeJSON(w, http.StatusBadRequest, errorBody{Error: "wait must be hold", Field: "wait"})
		return
	}
	a.restart.mu.Lock()
	if a.restart.plan != nil {
		a.restart.mu.Unlock()
		writeJSON(w, http.StatusConflict, errorBody{Error: "a restart is already waiting for the running work to end"})
		return
	}
	a.restart.plan = &restartPlan{Requested: time.Now(), CancelRunning: body.CancelRunning, Resume: body.Resume == nil || *body.Resume, Wait: body.Wait}
	a.restart.mu.Unlock()

	runs, actor := a.deps.Engine.Runs(), actorOf(r)
	list, err := runs.List(r.Context())
	if err != nil {
		a.clearRestart()
		writeErr(w, err)
		return
	}
	var paused []string
	for _, p := range list {
		if p.State != model.RunRunning && p.State != model.RunPausing {
			continue
		}
		if body.CancelRunning {
			err = runs.Stop(r.Context(), actor, p.Project)
		} else {
			err = runs.Pause(r.Context(), actor, p.Project)
		}
		if err != nil {
			a.clearRestart()
			writeErr(w, err)
			return
		}
		if p.State == model.RunRunning { // one already pausing was the owner's doing: leave it so after a cancel
			paused = append(paused, p.Project)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.restart.mu.Lock()
	a.restart.plan.Paused, a.restart.plan.cancel = paused, cancel
	a.restart.mu.Unlock()
	go a.awaitRestart(ctx)
	writeJSON(w, http.StatusAccepted, a.restartView(r.Context()))
}

func (a *API) clearRestart() {
	a.restart.mu.Lock()
	a.restart.plan = nil
	a.restart.mu.Unlock()
}

// awaitRestart ends the process once no stage is open. A restart that runs out
// of time becomes a service hold instead of giving up (AF-10): the projects
// stay paused, nothing new starts, and the restart finishes when the last stage
// closes, unless the owner lifts the hold first. A restart asked for with
// wait=hold never gives up and so never becomes a hold.
func (a *API) awaitRestart(ctx context.Context) {
	var deadline <-chan time.Time
	if plan := a.restartPlanNow(); plan != nil && plan.Wait != "hold" {
		timer := time.NewTimer(restartMaxWait)
		defer timer.Stop()
		deadline = timer.C
	}
	tick := time.NewTicker(restartPoll)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-deadline:
			a.enterHold(ctx)
		case <-tick.C:
			a.deps.Engine.Runs().Settle(ctx)
			// A seat stage survives the restart (its session or task is adopted again), so
			// the restart does not wait for it.
			open, err := a.deps.Engine.OpenStagesExceptSeats(ctx)
			if err != nil || open > 0 {
				continue
			}
			plan := a.restartPlanNow()
			if plan == nil {
				return
			}
			if plan.Held {
				h, err := a.deps.Store.ServiceHold(ctx)
				if err != nil {
					log.Printf("agentflow: restart: read the hold: %v", err)
					continue
				}
				if !h.Active || !h.Restart {
					// Lifted: the restart went with the hold.
					a.clearRestart()
					return
				}
				// The hold has done its job; the next process starts the paused
				// projects from the resume event, not from the hold.
				if _, err := a.deps.Store.LiftServiceHold(ctx, SystemActorName); err != nil {
					log.Printf("agentflow: restart: lift the hold: %v", err)
				}
			}
			if plan.Resume && len(plan.Paused) > 0 {
				// Written before the process ends, so the next one finds it.
				if err := a.noteService(ctx, "service.restart", plan.Paused); err != nil {
					log.Printf("agentflow: restart: could not record the projects to resume: %v", err)
				}
			}
			a.terminate()
			return
		}
	}
}

// enterHold turns a restart that ran out of time into a service hold with the
// projects it paused: nothing new starts in any project, and the restart ends
// when the last stage closes or is abandoned when the hold is lifted.
func (a *API) enterHold(ctx context.Context) {
	plan := a.restartPlanNow()
	if plan == nil || plan.Held {
		return
	}
	if _, err := a.deps.Store.HoldService(ctx, SystemActorName, holdRestartReason, plan.Paused, true); err != nil {
		log.Printf("agentflow: restart: could not hold the service at the deadline: %v", err)
		a.clearRestart()
		return
	}
	a.restart.mu.Lock()
	if a.restart.plan == plan {
		a.restart.plan.Held = true
	}
	a.restart.mu.Unlock()
}

// cancelRestart abandons a pending restart and starts the projects it paused;
// when the restart had become a hold, lifting the hold is what cancels it.
func (a *API) cancelRestart(w http.ResponseWriter, r *http.Request) {
	ctx, actor := r.Context(), actorOf(r)
	plan := a.takePlan()
	if plan == nil {
		writeJSON(w, http.StatusOK, a.restartView(ctx))
		return
	}
	if plan.Held {
		if _, err := a.liftHoldNow(ctx, actor); err != nil {
			writeErr(w, err)
			return
		}
	} else {
		runs := a.deps.Engine.Runs()
		for _, slug := range plan.Paused {
			if err := runs.Start(ctx, actor, slug); err != nil {
				writeErr(w, err)
				return
			}
		}
	}
	writeJSON(w, http.StatusOK, a.restartView(ctx))
}

// noteService records, in the audit log, that a restart is about to end the
// process with these projects running (service.restart), or that the next
// process started them again (service.restarted).
func (a *API) noteService(ctx context.Context, action string, projects []string) error {
	return a.deps.Store.WithTx(ctx, func(tx *sql.Tx) error {
		_, err := store.AppendEvent(tx, SystemActorName, action, "service", "restart", map[string]any{"projects": projects})
		return err
	})
}

// SystemActorName is the actor recorded for what the service does by itself.
const SystemActorName = "system"

// ResumeAfterRestart starts again the projects a restart paused, once, when the
// service is back. It does nothing when the last restart request was already
// answered or none carried projects.
func (a *API) ResumeAfterRestart(ctx context.Context) {
	if h, err := a.deps.Store.ServiceHold(ctx); err == nil && h.Active {
		// A hold survives the restart: the projects it paused stay paused until
		// it is lifted. A restart it was waiting for has now happened.
		if h.Restart {
			if err := a.deps.Store.SetServiceHoldRestart(ctx, false); err != nil {
				log.Printf("agentflow: service hold: clear the restart flag: %v", err)
			}
		}
		return
	}
	evs, err := a.deps.Store.ListEvents(ctx, "service", "restart", 100000)
	if err != nil || len(evs) == 0 {
		return
	}
	last := evs[len(evs)-1]
	if last.Action != "service.restart" {
		return
	}
	var d struct {
		Projects []string `json:"projects"`
	}
	if json.Unmarshal([]byte(last.DetailJSON), &d) != nil {
		return
	}
	var started []string
	for _, slug := range d.Projects {
		if err := a.deps.Engine.Runs().Start(ctx, SystemActorName, slug); err != nil {
			log.Printf("agentflow: resume %s after the restart: %v", slug, err)
			continue
		}
		started = append(started, slug)
	}
	if err := a.noteService(ctx, "service.restarted", started); err != nil {
		log.Printf("agentflow: could not record the resumed projects: %v", err)
	}
}
