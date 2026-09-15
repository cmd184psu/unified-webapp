package coordinator

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"cmd184psu/unified-webapp/internal/platform/response"
	"cmd184psu/unified-webapp/internal/taskmaster/db"
	"cmd184psu/unified-webapp/internal/taskmaster/worker"
)

// pendingPollInterval is how often handleExecutionOutput re-checks whether a
// not-yet-started execution has been picked up by the worker while a client
// is waiting on its output stream. Short relative to the worker's 5s poll so
// a client sees output promptly once the task actually starts.
const pendingPollInterval = 250 * time.Millisecond

// sseKeepaliveInterval bounds how long an execution's output stream can go
// completely silent (e.g. a "sleep 60" between two echo lines) before a
// keep-alive comment is sent. Without this, a long enough gap in real output
// looks identical, from any intermediary's point of view (a reverse proxy,
// HAProxy, some corporate/home network gear), to a dead connection — many
// default to closing an idle connection around 30-60s of silence, well
// inside what a real task can legitimately go quiet for. A bare SSE comment
// line (leading ':') is invisible to EventSource — it fires no event, the
// UI never sees it — its only job is proving to anything in between that
// the connection is still alive and flowing.
const sseKeepaliveInterval = 15 * time.Second

// isTerminalExecStatus reports whether status is one an execution reaches
// only after actually running (or failing to) — as opposed to "pending",
// which just means "not started yet, will be soon."
func isTerminalExecStatus(status string) bool {
	return status == "success" || status == "failed" || status == "canceled"
}

// waitForRegistration blocks until execID is registered in registry (the
// worker picked it up and started it), the execution reaches a terminal
// status without ever being registered (rare — e.g. it was canceled while
// still pending), or ctx is done (client disconnected).
//
// found reports whether the execution became registered (stdout/stderr are
// then valid and the caller should replay+subscribe as usual). When found
// is false, finalStatus carries the terminal status to report to the client
// (e.g. "canceled") if the wait ended because of that rather than because
// the client disconnected, in which case finalStatus is empty and the
// caller has nothing left to send.
func waitForRegistration(ctx context.Context, registry *worker.OutputRegistry, database *db.DB, execID int64) (stdout, stderr *worker.OutputCapture, found bool, finalStatus string) {
	ticker := time.NewTicker(pendingPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil, nil, false, ""
		case <-ticker.C:
			if so, se, ok := registry.Get(execID); ok {
				return so, se, true, ""
			}
			if exec, err := database.GetExecution(execID); err == nil && exec != nil && isTerminalExecStatus(exec.Status) {
				return nil, nil, false, exec.Status
			}
		}
	}
}

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

func (c *Coordinator) handleExecutionOutput(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	execID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, "invalid execution id")
		return
	}

	if c.sseMax > 0 {
		if n := c.sseSubs.Add(1); n > int64(c.sseMax) {
			c.sseSubs.Add(-1)
			response.WriteError(w, http.StatusServiceUnavailable, "sse subscriber limit reached")
			return
		}
		defer c.sseSubs.Add(-1)
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	stdout, stderr, found := c.registry.Get(execID)
	if !found {
		// Not registered yet. If it already exists but is truly finished
		// (or its output was reaped after the ~1h replay window), report
		// that status once and close, as before. But if it just hasn't
		// STARTED yet ("pending") — e.g. the client opened this the moment
		// a task was enqueued — closing here would be wrong: the worker's
		// next poll (≤5s) will pick it up and start producing real output
		// that this client asked to see. Stay connected and wait for that
		// instead of forcing the client to guess when to retry.
		exec, dbErr := c.db.GetExecution(execID)
		if dbErr != nil || exec == nil {
			response.WriteError(w, http.StatusNotFound, "execution not found")
			return
		}
		if isTerminalExecStatus(exec.Status) {
			fmt.Fprintf(w, "event: status\ndata: %s\n\n", exec.Status)
			flusher.Flush()
			return
		}
		var finalStatus string
		stdout, stderr, found, finalStatus = waitForRegistration(r.Context(), c.registry, c.db, execID)
		if !found {
			if finalStatus != "" {
				fmt.Fprintf(w, "event: status\ndata: %s\n\n", finalStatus)
				flusher.Flush()
			}
			return
		}
	}

	// Replay buffered stdout lines
	for _, line := range stdout.Lines() {
		sendSSELine(w, line)
		flusher.Flush()
	}
	// Replay buffered stderr lines
	for _, line := range stderr.Lines() {
		sendSSELine(w, line)
		flusher.Flush()
	}

	if stdout.Done() && stderr.Done() {
		fmt.Fprintf(w, "event: done\ndata: {}\n\n")
		flusher.Flush()
		return
	}

	// Subscribe to live output
	stdoutCh := stdout.Subscribe()
	stderrCh := stderr.Subscribe()

	keepalive := time.NewTicker(sseKeepaliveInterval)
	defer keepalive.Stop()

	for {
		select {
		case line, ok := <-stdoutCh:
			if !ok {
				stdoutCh = nil
			} else {
				sendSSELine(w, line)
				flusher.Flush()
			}
		case line, ok := <-stderrCh:
			if !ok {
				stderrCh = nil
			} else {
				sendSSELine(w, line)
				flusher.Flush()
			}
		case <-keepalive.C:
			fmt.Fprintf(w, ": keepalive\n\n")
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
		if stdoutCh == nil && stderrCh == nil {
			fmt.Fprintf(w, "event: done\ndata: {}\n\n")
			flusher.Flush()
			return
		}
	}
}

func sendSSELine(w http.ResponseWriter, line worker.OutputLine) {
	data, err := json.Marshal(map[string]string{
		"stream": line.Stream,
		"line":   line.Line,
		"ts":     line.Timestamp.Format("2006-01-02T15:04:05.000Z07:00"),
	})
	if err != nil {
		return
	}
	fmt.Fprintf(w, "event: output\ndata: %s\n\n", data)
}
