package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"

	"cmd184psu/unified-webapp/internal/platform/response"
	"cmd184psu/unified-webapp/internal/taskmaster/db"
)

// pendingPollInterval is how often StreamExecutionOutput re-checks whether a
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

// SSECap is a shared subscriber budget for the per-execution output SSE
// stream. A max <= 0 means uncapped (mirroring the old `if sseMax > 0` guard).
// One SSECap can be shared across several HTTP entry points (e.g. the
// coordinator's route and a module lane's route) so they draw on a single
// subscriber budget rather than two independent ones.
type SSECap struct {
	max  int
	subs atomic.Int64
}

// NewSSECap returns an SSECap with the given cap (<= 0 = uncapped).
func NewSSECap(max int) *SSECap {
	return &SSECap{max: max}
}

// Acquire reserves one subscriber slot. It returns ok=false (and a no-op
// release) when the cap is already reached; otherwise release must be called
// once to return the slot. A nil SSECap, or one with max <= 0, is uncapped.
func (c *SSECap) Acquire() (release func(), ok bool) {
	if c == nil || c.max <= 0 {
		return func() {}, true
	}
	if n := c.subs.Add(1); n > int64(c.max) {
		c.subs.Add(-1)
		return func() {}, false
	}
	return func() { c.subs.Add(-1) }, true
}

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
func waitForRegistration(ctx context.Context, registry *OutputRegistry, database *db.DB, execID int64) (stdout, stderr *OutputCapture, found bool, finalStatus string) {
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

// StreamExecutionOutput streams execID's stdout+stderr as SSE: it replays the
// buffered lines, then live-subscribes with a keepalive, closing when both
// streams are done or the client disconnects. It depends only on the shared
// OutputRegistry and DB, so both the coordinator's HTTP route and a module
// lane's route can reuse it (DRY). It writes all its own HTTP responses
// (status/done/output events, or a 404/status short-circuit); it returns a
// non-nil error only for the "streaming not supported" case (the http.Flusher
// assertion), after having already written that 500 — so callers may ignore
// the return.
//
// Caller-specific concerns stay with the caller: the coordinator does its own
// hidden-lane check and SSECap acquisition; a module lane resolves the job id
// to its execution id and acquires its own SSECap slot before calling this.
func StreamExecutionOutput(w http.ResponseWriter, r *http.Request, execID int64, registry *OutputRegistry, database *db.DB) error {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return errors.New("streaming not supported")
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	stdout, stderr, found := registry.Get(execID)
	if !found {
		// Not registered yet. If it already exists but is truly finished
		// (or its output was reaped after the ~1h replay window), report
		// that status once and close, as before. But if it just hasn't
		// STARTED yet ("pending") — e.g. the client opened this the moment
		// a task was enqueued — closing here would be wrong: the worker's
		// next poll (≤5s) will pick it up and start producing real output
		// that this client asked to see. Stay connected and wait for that
		// instead of forcing the client to guess when to retry.
		exec, dbErr := database.GetExecution(execID)
		if dbErr != nil || exec == nil {
			response.WriteError(w, http.StatusNotFound, "execution not found")
			return nil
		}
		if isTerminalExecStatus(exec.Status) {
			fmt.Fprintf(w, "event: status\ndata: %s\n\n", exec.Status)
			flusher.Flush()
			return nil
		}
		var finalStatus string
		stdout, stderr, found, finalStatus = waitForRegistration(r.Context(), registry, database, execID)
		if !found {
			if finalStatus != "" {
				fmt.Fprintf(w, "event: status\ndata: %s\n\n", finalStatus)
				flusher.Flush()
			}
			return nil
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
		return nil
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
			return nil
		}
		if stdoutCh == nil && stderrCh == nil {
			fmt.Fprintf(w, "event: done\ndata: {}\n\n")
			flusher.Flush()
			return nil
		}
	}
}

func sendSSELine(w http.ResponseWriter, line OutputLine) {
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
