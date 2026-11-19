package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"cmd184psu/unified-webapp/internal/agentflow/engine"
	"cmd184psu/unified-webapp/internal/agentflow/runner/mux"
)

// registerSeats serves the seat stages (AF-11): stages worked by a BotChat callsign or by an
// interactive program in a terminal session.
func (a *API) registerSeats(mux muxer) {
	mux.HandleFunc("GET /api/seats", a.listSeats)
	mux.HandleFunc("POST /api/seats/{stage}/answer", a.answerSeat)
	mux.HandleFunc("POST /api/seats/{stage}/cancel", a.cancelSeat)
}

// listSeats is GET /api/seats: every open seat stage, with `tmux attach -t <name>` for a
// terminal seat and the question or silence it is waiting on the owner with.
func (a *API) listSeats(w http.ResponseWriter, r *http.Request) {
	list, err := a.deps.Engine.SeatStages(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func seatStageID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("stage"), 10, 64)
	if err != nil || id <= 0 {
		badRequest(w, "stage", "stage must be a stage id")
		return 0, false
	}
	return id, true
}

func writeSeatErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, engine.ErrNotSeat):
		writeJSON(w, http.StatusConflict, errorBody{Error: "the stage is not an open seat stage"})
	case errors.Is(err, mux.ErrNoSession):
		writeJSON(w, http.StatusConflict, errorBody{Error: "the seat's session is gone"})
	default:
		writeErr(w, err)
	}
}

// answerSeat is POST /api/seats/{stage}/answer {"text"}: the answer is typed into a terminal
// seat's session (or posted to a BotChat seat) and the stage's question is closed.
func (a *API) answerSeat(w http.ResponseWriter, r *http.Request) {
	id, ok := seatStageID(w, r)
	if !ok {
		return
	}
	var body struct {
		Text string `json:"text"`
	}
	if !decodeBody(w, r, &body, false) {
		return
	}
	if strings.TrimSpace(body.Text) == "" {
		badRequest(w, "text", "the answer is empty")
		return
	}
	if err := a.deps.Engine.AnswerSeat(r.Context(), actorOf(r), id, body.Text); err != nil {
		writeSeatErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"stage": id, "answered": true})
}

// cancelSeat is POST /api/seats/{stage}/cancel: the seat's session is killed (or its BotChat
// task cancelled) and the stage ends as cancelled.
func (a *API) cancelSeat(w http.ResponseWriter, r *http.Request) {
	id, ok := seatStageID(w, r)
	if !ok {
		return
	}
	if err := a.deps.Engine.CancelSeat(r.Context(), actorOf(r), id); err != nil {
		writeSeatErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"stage": id, "cancelled": true})
}
