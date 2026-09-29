package coordinator

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"cmd184psu/unified-webapp/internal/platform/response"
	"cmd184psu/unified-webapp/internal/taskmaster/worker"
)

// handleBoardEvents streams the shared board-events broker as text/event-
// stream. It does not replay history on connect (see the route comment in
// coordinator.go); the broker enforces the SSEMaxSubscribers cap itself,
// rejecting with 503 before any SSE header is written once the cap is hit.
func (c *Coordinator) handleBoardEvents(w http.ResponseWriter, r *http.Request) {
	if c.board == nil {
		response.WriteError(w, http.StatusServiceUnavailable, "board events unavailable")
		return
	}
	c.board.ServeSSE("board", nil)(w, r)
}

// handleExecutionOutput is a thin coordinator-specific wrapper around the
// shared worker.StreamExecutionOutput: it parses the chi {id} URL param, does
// the coordinator's hidden-lane check (N4), acquires an SSE subscriber slot,
// and then delegates the actual replay+subscribe streaming to the worker
// package (which both the coordinator and headless module lanes reuse, DRY).
func (c *Coordinator) handleExecutionOutput(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	execID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, "invalid execution id")
		return
	}

	// N4: a hidden lane's executions are invisible here too, using the
	// route's existing 404 (no separate message).
	if lane, _, found, lerr := c.db.ExecutionLane(execID); lerr == nil && found && c.laneHidden(lane) {
		response.WriteError(w, http.StatusNotFound, "execution not found")
		return
	}

	release, ok := c.sseCap.Acquire()
	if !ok {
		response.WriteError(w, http.StatusServiceUnavailable, "sse subscriber limit reached")
		return
	}
	defer release()

	// StreamExecutionOutput writes all its own responses; its returned error
	// (only the "streaming not supported" case, already written) is ignored,
	// as before.
	_ = worker.StreamExecutionOutput(w, r, execID, c.registry, c.db)
}
