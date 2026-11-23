package api

import (
	"errors"
	"net/http"

	"cmd184psu/unified-webapp/internal/agentflow/store"
	"strconv"

	"cmd184psu/unified-webapp/internal/agentflow/coord"
	"cmd184psu/unified-webapp/internal/agentflow/runner/seat"
)

// registerCoord serves the external seats module (AF-13): the hand-run seats, their queue
// files, the nudge, the coordinator pass and the merge of one handed-off task.
func (a *API) registerCoord(mux muxer) {
	mux.HandleFunc("GET /api/coord", a.coordStatus)
	mux.HandleFunc("POST /api/coord/run", a.coordRun)
	mux.HandleFunc("GET /api/seats/external", a.externalSeats)
	mux.HandleFunc("POST /api/seats/external", a.saveSeat)
	mux.HandleFunc("PATCH /api/seats/external/{callsign}", a.saveSeat)
	mux.HandleFunc("DELETE /api/seats/external/{callsign}", a.deleteSeat)
	mux.HandleFunc("POST /api/seats/external/{callsign}/pause", a.pauseSeat)
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

// seatBody is the registry entry the Seats page edits. On PATCH every field left out keeps
// its stored value, so the pause and policy buttons send only what they change.
type seatBody struct {
	Callsign  string             `json:"callsign"`
	Kind      string             `json:"kind"`
	Where     string             `json:"where"`
	QueueFile string             `json:"queue_file"`
	Policy    *coord.NudgePolicy `json:"policy"`
}

// saveSeat is POST /api/seats/external (add or replace) and PATCH /api/seats/external/{callsign}
// (edit): the owner registers hand-run seats with no code change, including their nudge policy.
func (a *API) saveSeat(w http.ResponseWriter, r *http.Request) {
	s, ok := a.coordOn(w)
	if !ok {
		return
	}
	var body seatBody
	if !decodeBody(w, r, &body, false) {
		return
	}
	patch := r.Method == http.MethodPatch
	if patch {
		cs := r.PathValue("callsign")
		if body.Callsign != "" && body.Callsign != cs {
			badRequest(w, "callsign", "the path callsign and the body callsign differ")
			return
		}
		body.Callsign = cs
		existing, found, err := s.Seat(cs)
		if err != nil {
			writeErr(w, err)
			return
		}
		if !found {
			writeJSON(w, http.StatusNotFound, errorBody{Error: "no such seat"})
			return
		}
		if body.Kind == "" {
			body.Kind = string(existing.Kind)
		}
		if body.Where == "" {
			body.Where = existing.Where
		}
		if body.QueueFile == "" {
			body.QueueFile = existing.QueueFile
		}
		if body.Policy == nil {
			p := existing.Policy
			body.Policy = &p
		}
	}
	seat := coord.RegistrySeat{Callsign: body.Callsign, Kind: coord.SeatKind(body.Kind), Where: body.Where, QueueFile: body.QueueFile}
	if body.Policy != nil {
		seat.Policy = *body.Policy
	}
	saved, err := s.SaveSeat(seat)
	if err != nil {
		writeErr(w, err)
		return
	}
	a.emit("seats.update", map[string]any{"seat": saved.Callsign})
	writeJSON(w, http.StatusOK, saved)
}

// deleteSeat is DELETE /api/seats/external/{callsign}.
func (a *API) deleteSeat(w http.ResponseWriter, r *http.Request) {
	s, ok := a.coordOn(w)
	if !ok {
		return
	}
	if err := s.DeleteSeat(r.PathValue("callsign")); err != nil {
		if errors.Is(err, coord.ErrUnknownSeat) {
			writeJSON(w, http.StatusNotFound, errorBody{Error: "no such seat"})
			return
		}
		writeErr(w, err)
		return
	}
	a.emit("seats.update", map[string]any{"seat": r.PathValue("callsign")})
	writeJSON(w, http.StatusOK, map[string]string{"deleted": r.PathValue("callsign")})
}

// pauseSeat is POST /api/seats/external/{callsign}/pause {"paused": bool}: stop or resume
// nudges without losing the seat's Enabled choice.
func (a *API) pauseSeat(w http.ResponseWriter, r *http.Request) {
	s, ok := a.coordOn(w)
	if !ok {
		return
	}
	var body struct {
		Paused bool `json:"paused"`
	}
	if !decodeBody(w, r, &body, false) {
		return
	}
	seat, err := s.PauseSeat(r.PathValue("callsign"), body.Paused)
	if errors.Is(err, coord.ErrUnknownSeat) {
		writeJSON(w, http.StatusNotFound, errorBody{Error: "no such seat"})
		return
	}
	if err != nil {
		writeErr(w, err)
		return
	}
	a.emit("seats.update", map[string]any{"seat": seat.Callsign})
	writeJSON(w, http.StatusOK, seat)
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

// addRuling handles POST /api/projects/{slug}/efforts/{id}/tasks/{task}/add_ruling.
// It is the HTTP entrypoint for the guarded add_ruling action: a coordinator or
// resolver stage proposes a new ruling, which is recorded as a pending entry in
// the decision_inbox (the rulings file is never written directly). The owner or
// coord-fs3 confirms or rejects the entry through ConfirmDecisionInbox /
// RejectDecisionInbox.
func (a *API) addRuling(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, store.Invalid("id", "not a number"))
		return
	}
	task, err := strconv.ParseInt(r.PathValue("task"), 10, 64)
	if err != nil {
		writeErr(w, store.Invalid("task", "not a number"))
		return
	}
	var body struct {
		RulingID     string `json:"ruling_id"`
		ProposedText string `json:"proposed_text"`
	}
	if !decodeBody(w, r, &body, false) {
		return
	}
	if body.RulingID == "" {
		writeErr(w, store.Invalid("ruling_id", "required"))
		return
	}
	if body.ProposedText == "" {
		writeErr(w, store.Invalid("proposed_text", "required"))
		return
	}
	actor := r.Header.Get("X-Coordinator-Callsign")
	if actor == "" {
		actor = "coord-fs3"
	}
	if err := a.deps.Engine.AddRuling(r.Context(), actor, slug, id, task, body.RulingID, body.ProposedText); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true})
}
