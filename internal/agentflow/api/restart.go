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
// before it gives up and leaves the projects paused; a worker stage may run for
// its whole timeout.
const restartMaxWait = 60 * time.Minute

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
	cancel context.CancelFunc
}

// RestartView is the restart state GET /api/service/restart reports.
type RestartView struct {
	// State is "idle" or "draining".
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
	}
	return v
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
	}
	if !decodeBody(w, r, &body, true) {
		return
	}
	a.restart.mu.Lock()
	if a.restart.plan != nil {
		a.restart.mu.Unlock()
		writeJSON(w, http.StatusConflict, errorBody{Error: "a restart is already waiting for the running work to end"})
		return
	}
	a.restart.plan = &restartPlan{Requested: time.Now(), CancelRunning: body.CancelRunning, Resume: body.Resume == nil || *body.Resume}
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

// awaitRestart ends the process once no stage is open, or leaves things paused
// after restartMaxWait.
func (a *API) awaitRestart(ctx context.Context) {
	deadline := time.NewTimer(restartMaxWait)
	defer deadline.Stop()
	tick := time.NewTicker(restartPoll)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-deadline.C:
			a.clearRestart()
			return
		case <-tick.C:
			a.deps.Engine.Runs().Settle(ctx)
			// A seat stage survives the restart (its session or task is adopted again), so
			// the restart does not wait for it.
			if open, err := a.deps.Engine.OpenStagesExceptSeats(ctx); err == nil && open == 0 {
				a.restart.mu.Lock()
				plan := a.restart.plan
				a.restart.mu.Unlock()
				if plan != nil && plan.Resume && len(plan.Paused) > 0 {
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
}

// cancelRestart abandons a pending restart and starts the projects it paused.
func (a *API) cancelRestart(w http.ResponseWriter, r *http.Request) {
	a.restart.mu.Lock()
	plan := a.restart.plan
	a.restart.plan = nil
	a.restart.mu.Unlock()
	if plan == nil {
		writeJSON(w, http.StatusOK, a.restartView(r.Context()))
		return
	}
	if plan.cancel != nil {
		plan.cancel()
	}
	runs, actor := a.deps.Engine.Runs(), actorOf(r)
	for _, slug := range plan.Paused {
		if err := runs.Start(r.Context(), actor, slug); err != nil {
			writeErr(w, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, a.restartView(r.Context()))
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
