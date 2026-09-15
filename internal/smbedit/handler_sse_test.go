package smbedit

// SSE tests. Both mounting rules from the plan are load-bearing:
//   - transport: httptest.NewServer + a real http.Client with a cancellable
//     request (a ResponseRecorder read concurrently with handler writes races
//     under -race);
//   - what is mounted: srv.Handler(), never the bare mux — the D-9 recovery
//     wrapper lives in that chain and is exactly what can break http.Flusher.

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.uber.org/goleak"
)

// readSSEUntil scans resp lines until a "data: " line containing want appears
// or the deadline passes. Returns the matched line.
func readSSEUntil(t *testing.T, scanner *bufio.Scanner, want string) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") && strings.Contains(line, want) {
			return line
		}
		if time.Now().After(deadline) {
			break
		}
	}
	t.Fatalf("SSE stream ended before %q appeared (scan err: %v)", want, scanner.Err())
	return ""
}

func TestOpsLogStream_BacklogThenLiveThenCancel(t *testing.T) {
	defer goleak.VerifyNone(t)
	srv := newTestServer(t)
	srv.ops.add("backlog-marker-entry")

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/api/logs/ops/stream", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("expected text/event-stream, got %q", ct)
	}

	scanner := bufio.NewScanner(resp.Body)
	readSSEUntil(t, scanner, "backlog-marker-entry")

	// A live entry added after the stream is established must be delivered.
	srv.ops.add("live-marker-entry")
	readSSEUntil(t, scanner, "live-marker-entry")

	// Disconnect: the handler must return (goleak fails the test otherwise).
	cancel()
	io.Copy(io.Discard, resp.Body)
}

func TestSambaLogStream_400WithoutConfiguredPath(t *testing.T) {
	defer goleak.VerifyNone(t)
	srv := newTestServer(t) // newTestServer clears SambaLogPath

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := ts.Client().Get(ts.URL + "/api/logs/samba/stream")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 without samba_log_path, got %d", resp.StatusCode)
	}
}

func TestSambaLogStream_StreamsAndExitsOnDisconnect(t *testing.T) {
	defer goleak.VerifyNone(t)
	srv := newTestServer(t)
	if _, err := srv.store.update(func(st *State) {
		st.SambaLogPath = "/var/log/samba/log.smbd" // never touched: the seam is swapped
	}); err != nil {
		t.Fatal(err)
	}

	// Swap the streaming seam (D-5): one line, then EOF when the request
	// context is torn down — the shape a killed "sudo tail" produces.
	origStream := runStreamingCommand
	runStreamingCommand = func(ctx context.Context, name string, args ...string) (io.ReadCloser, func() error, error) {
		if name != "sudo" || len(args) < 1 || args[0] != "tail" {
			t.Errorf("unexpected streaming command: %s %v", name, args)
		}
		pr, pw := io.Pipe()
		go func() {
			pw.Write([]byte("fake tail line one\n"))
			<-ctx.Done()
			pw.Close()
		}()
		return pr, func() error { return nil }, nil
	}
	t.Cleanup(func() { runStreamingCommand = origStream })

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/api/logs/samba/stream", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("expected text/event-stream, got %q", ct)
	}

	scanner := bufio.NewScanner(resp.Body)
	readSSEUntil(t, scanner, "fake tail line one")

	// Disconnect. The handler and the fake tail goroutine must both exit —
	// goleak.VerifyNone is the assertion.
	cancel()
	io.Copy(io.Discard, resp.Body)
}
