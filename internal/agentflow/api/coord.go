package api

import (
	"errors"
	"net/http"

	"cmd184psu/unified-webapp/internal/agentflow/coord"
	"cmd184psu/unified-webapp/internal/agentflow/runner/seat"
)

// registerCoord serves the external seats module (AF-13): the hand-run seats, their queue
// files, the nudge, the coordinator pass and the merge of one handed-off task.
func (a *API) registerCoord(mux muxer) {
	mux.HandleFunc("GET /api/coord", a.coordStatus)
	mux.HandleFunc("POST /api/coord/run", a.coordRun)
	mux.HandleFunc("GET /api/seats/external", a.externalSeats)
	mux.HandleFunc("GET /api/seats/{callsign}/queue", a.seatQueue)
	mux.HandleFunc("POST /api/seats/{callsign}/nudge", a.seatNudge)
	mux.HandleFunc("POST /api/handoffs/{task}/merge", a.handoffMerge)
}

func (a *API) coordOn(w http.ResponseWriter) (*coord.Service, bool) {
	if a.deps.Coord == nil {
		writeJSON(w, http.StatusNotFound, errorBody{Error: "the coordinator module is off: there is no coord.json in agentflow's data directory"})
		return nil, false
	}
	return a.deps.Coord, true
}

// coordStatus is GET /api/coord: whether the loops run, dry-run mode, and the newest reports.
func (a *API) coordStatus(w http.ResponseWriter, r *http.Request) {
	s, ok := a.coordOn(w)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled": s.Cfg.Enabled, "dry_run": s.Cfg.DryRun, "me": s.Cfg.Me, "base": s.Cfg.Base,
		"last_run": s.Coord.Last(), "watch": s.WatchLast(),
	})
}

// coordRun is POST /api/coord/run {"dry": bool}: one coordinator pass now, its report back.
func (a *API) coordRun(w http.ResponseWriter, r *http.Request) {
	s, ok := a.coordOn(w)
	if !ok {
		return
	}
	var body struct {
		Dry bool `json:"dry"`
	}
	if !decodeBody(w, r, &body, true) {
		return
	}
	writeJSON(w, http.StatusOK, s.Coord.Run(r.Context(), body.Dry))
}

// externalSeats is GET /api/seats/external: each hand-run seat, its task and screen state.
func (a *API) externalSeats(w http.ResponseWriter, r *http.Request) {
	s, ok := a.coordOn(w)
	if !ok {
		return
	}
	list, err := s.Seats()
	if err != nil {
		writeErr(w, err)
		return
	}
	if list == nil {
		list = []coord.ExternalSeat{}
	}
	writeJSON(w, http.StatusOK, list)
}

// seatQueue is GET /api/seats/{callsign}/queue: the seat's queue file as the seat reads it.
func (a *API) seatQueue(w http.ResponseWriter, r *http.Request) {
	s, ok := a.coordOn(w)
	if !ok {
		return
	}
	text, err := s.QueueFile(r.PathValue("callsign"))
	if errors.Is(err, coord.ErrUnknownSeat) {
		writeJSON(w, http.StatusNotFound, errorBody{Error: "no such seat"})
		return
	}
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"callsign": r.PathValue("callsign"), "text": text})
}

// seatNudge is POST /api/seats/{callsign}/nudge: type the seat's nudge now (text, Enter, wait,
// Enter). 409 while its screen shows a halt text (login, take-over): nothing is typed.
func (a *API) seatNudge(w http.ResponseWriter, r *http.Request) {
	s, ok := a.coordOn(w)
	if !ok {
		return
	}
	if s.Cfg.DryRun {
		writeJSON(w, http.StatusConflict, errorBody{Error: "the coordinator is in dry-run mode: nothing is typed"})
		return
	}
	err := s.Watch.NudgeNow(r.Context(), r.PathValue("callsign"))
	switch {
	case errors.Is(err, coord.ErrUnknownSeat):
		writeJSON(w, http.StatusNotFound, errorBody{Error: "no such seat, or it has no session"})
	case errors.Is(err, seat.ErrHalted):
		writeJSON(w, http.StatusConflict, errorBody{Error: "the seat's screen shows a halt text: the owner must act"})
	case err != nil:
		writeErr(w, err)
	default:
		writeJSON(w, http.StatusOK, map[string]string{"nudged": r.PathValue("callsign")})
	}
}

// handoffMerge is POST /api/handoffs/{task}/merge {"branches": [...], "dry": bool}: merge one
// BotChat dev task's branches now, with the gates, as a PASS review would.
func (a *API) handoffMerge(w http.ResponseWriter, r *http.Request) {
	s, ok := a.coordOn(w)
	if !ok {
		return
	}
	var body struct {
		Branches []string `json:"branches"`
		Dry      bool     `json:"dry"`
	}
	if !decodeBody(w, r, &body, true) {
		return
	}
	writeJSON(w, http.StatusOK, s.Coord.MergeTask(r.Context(), r.PathValue("task"), body.Branches, body.Dry))
}
