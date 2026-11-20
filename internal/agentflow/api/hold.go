package api

import (
	"context"
	"log"
	"net/http"
	"strings"

	"cmd184psu/unified-webapp/internal/agentflow/model"
	"cmd184psu/unified-webapp/internal/agentflow/store"
)

// HoldView is what the hold routes answer: the hold itself plus the work still
// finishing under it (AF-10).
type HoldView struct {
	store.ServiceHold
	OpenStages int `json:"open_stages"`
}

func (a *API) registerHold(mux muxer) {
	mux.HandleFunc("GET /api/service/hold", a.holdStatus)
	mux.HandleFunc("POST /api/service/hold", a.holdService)
	mux.HandleFunc("DELETE /api/service/hold", a.liftHoldService)
}

func (a *API) holdView(ctx context.Context) (HoldView, error) {
	h, err := a.deps.Store.ServiceHold(ctx)
	if err != nil {
		return HoldView{}, err
	}
	v := HoldView{ServiceHold: h}
	if open, err := a.deps.Store.OpenStages(ctx); err == nil {
		v.OpenStages = len(open)
	}
	return v, nil
}

func (a *API) holdStatus(w http.ResponseWriter, r *http.Request) {
	v, err := a.holdView(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

// holdService holds the whole service: no project starts a new stage, a
// run/start is refused with the reason, and the projects that are running are
// paused and remembered so lifting the hold starts them again.
func (a *API) holdService(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Reason string `json:"reason"`
	}
	if !decodeBody(w, r, &body, true) {
		return
	}
	reason := strings.TrimSpace(body.Reason)
	if reason == "" {
		badRequest(w, "reason", "required")
		return
	}
	if a.restartPlanNow() != nil {
		writeJSON(w, http.StatusConflict, errorBody{Error: "a restart is already waiting for the running work to end"})
		return
	}
	ctx, actor := r.Context(), actorOf(r)
	runs := a.deps.Engine.Runs()
	list, err := runs.List(ctx)
	if err != nil {
		writeErr(w, err)
		return
	}
	// The hold is stored before anything is paused, so a dispatcher pass in
	// between cannot start another stage.
	var paused []string
	for _, p := range list {
		if p.State == model.RunRunning {
			paused = append(paused, p.Project)
		}
	}
	if _, err := a.deps.Store.HoldService(ctx, actor, reason, paused, false); err != nil {
		writeErr(w, err)
		return
	}
	for _, slug := range paused {
		if err := runs.Pause(ctx, actor, slug); err != nil {
			// The project went away between the list and the pause; the hold
			// still stands for the rest.
			log.Printf("agentflow: service hold: pause %s: %v", slug, err)
		}
	}
	v, err := a.holdView(ctx)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

// liftHoldService lifts the hold and starts again exactly the projects it
// paused; a restart that was waiting in the hold is abandoned with it.
func (a *API) liftHoldService(w http.ResponseWriter, r *http.Request) {
	ctx, actor := r.Context(), actorOf(r)
	plan := a.restartPlanNow()
	if plan != nil && !plan.Held {
		// Paused by a plain drain restart: that one is cancelled on its own route.
		writeJSON(w, http.StatusConflict, errorBody{Error: "a restart is waiting for the running work to end"})
		return
	}
	a.takePlan() // stops the watcher of a restart that lived in the hold
	if _, err := a.liftHoldNow(ctx, actor); err != nil {
		writeErr(w, err)
		return
	}
	v, err := a.holdView(ctx)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

// liftHoldNow removes the hold and starts again the projects it paused; a
// project that cannot start is logged and skipped, so one deleted project
// cannot strand the others.
func (a *API) liftHoldNow(ctx context.Context, actor string) (store.ServiceHold, error) {
	h, err := a.deps.Store.LiftServiceHold(ctx, actor)
	if err != nil {
		return h, err
	}
	for _, slug := range h.Paused {
		if err := a.deps.Engine.Runs().Start(ctx, actor, slug); err != nil {
			log.Printf("agentflow: lifting the hold: start %s: %v", slug, err)
		}
	}
	return h, nil
}
