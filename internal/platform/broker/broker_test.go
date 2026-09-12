package broker

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSubscribeReceivesNotify(t *testing.T) {
	b := NewBroker(0)
	ch := make(chan struct{}, 1)
	b.Subscribe(ch)
	b.Notify()
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("expected signal after Notify")
	}
}

func TestUnsubscribeReceivesNoSignal(t *testing.T) {
	b := NewBroker(0)
	ch := make(chan struct{}, 1)
	b.Subscribe(ch)
	b.Unsubscribe(ch)
	b.Notify()
	select {
	case <-ch:
		t.Fatal("expected no signal after Unsubscribe")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestNotifyFanOut(t *testing.T) {
	b := NewBroker(0)
	const n = 5
	channels := make([]chan struct{}, n)
	for i := range channels {
		channels[i] = make(chan struct{}, 1)
		b.Subscribe(channels[i])
	}
	b.Notify()
	for i, ch := range channels {
		select {
		case <-ch:
		case <-time.After(time.Second):
			t.Fatalf("channel %d did not receive signal", i)
		}
	}
}

func TestNotifyNonBlocking(t *testing.T) {
	b := NewBroker(0)
	// Unbuffered channel — Notify must not block.
	ch := make(chan struct{})
	b.Subscribe(ch)
	done := make(chan struct{})
	go func() {
		b.Notify()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Notify blocked on slow client")
	}
}

func TestServeHTTPContextCancel(t *testing.T) {
	b := NewBroker(0)
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/api/events", nil).WithContext(ctx)
	rec := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		b.ServeHTTP(rec, req)
		close(done)
	}()

	// Give ServeHTTP time to register the client and block.
	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("ServeHTTP did not return after context cancel")
	}
}

func TestServeHTTPWritesConnectedComment(t *testing.T) {
	b := NewBroker(0)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := httptest.NewRequest(http.MethodGet, "/api/events", nil).WithContext(ctx)
	rec := httptest.NewRecorder()

	go b.ServeHTTP(rec, req)
	time.Sleep(20 * time.Millisecond)
	cancel()
	time.Sleep(20 * time.Millisecond)

	body := rec.Body.String()
	if !strings.Contains(body, ": connected") {
		t.Errorf("expected connected comment in body, got: %q", body)
	}
}

func TestServeHTTPWritesRetryDirective(t *testing.T) {
	b := NewBroker(1000)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := httptest.NewRequest(http.MethodGet, "/api/events", nil).WithContext(ctx)
	rec := httptest.NewRecorder()

	go b.ServeHTTP(rec, req)
	time.Sleep(20 * time.Millisecond)
	cancel()
	time.Sleep(20 * time.Millisecond)

	body := rec.Body.String()
	if !strings.Contains(body, "retry: 1000") {
		t.Errorf("expected retry directive in body, got: %q", body)
	}
}

func TestServeHTTPDeliversRefreshOnNotify(t *testing.T) {
	b := NewBroker(0)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := httptest.NewRequest(http.MethodGet, "/api/events", nil).WithContext(ctx)
	rec := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		b.ServeHTTP(rec, req)
		close(done)
	}()
	time.Sleep(20 * time.Millisecond)
	b.Notify()
	time.Sleep(20 * time.Millisecond)
	cancel()
	<-done // wait for ServeHTTP to exit before reading shared recorder

	body := rec.Body.String()
	if !strings.Contains(body, "data: refresh") {
		t.Errorf("expected refresh event in body, got: %q", body)
	}
}

// TestServeSSECapRejects65thSubscriber connects 64 clients (the default cap),
// verifies all 64 receive a published event, then verifies a 65th connect is
// rejected with 503 before any SSE headers are written.
func TestServeSSECapRejects65thSubscriber(t *testing.T) {
	b := NewBroker(0)
	b.SetMaxSubscribers(64)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/events", b.ServeSSE("state", nil))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	type conn struct {
		resp *http.Response
	}
	conns := make([]conn, 0, 64)
	for i := 0; i < 64; i++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/events", nil)
		if err != nil {
			t.Fatalf("subscriber %d: build request: %v", i, err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("subscriber %d: connect: %v", i, err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("subscriber %d: status = %d, want 200", i, resp.StatusCode)
		}
		conns = append(conns, conn{resp: resp})
	}
	defer func() {
		for _, c := range conns {
			c.resp.Body.Close()
		}
	}()

	// Give ServeSSE time to register every subscriber before checking the cap.
	time.Sleep(50 * time.Millisecond)

	// 65th connect must be rejected with 503 before any SSE headers are set.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/events", nil)
	if err != nil {
		t.Fatalf("65th subscriber: build request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("65th subscriber: connect: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("65th subscriber: status = %d, want 503", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); strings.Contains(ct, "text/event-stream") {
		t.Errorf("65th subscriber: got SSE Content-Type %q, want no SSE headers on 503", ct)
	}

	// The first 64 subscribers must still receive a published event.
	b.Publish("hello")
	readers := make([]*bufio.Reader, len(conns))
	for i, c := range conns {
		readers[i] = bufio.NewReader(c.resp.Body)
	}
	for i, r := range readers {
		found := false
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			line, err := r.ReadString('\n')
			if err != nil {
				break
			}
			if strings.Contains(line, "hello") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("subscriber %d did not receive published event", i)
		}
	}
}

func TestServeHTTPClientRemovedAfterDisconnect(t *testing.T) {
	b := NewBroker(0)
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/api/events", nil).WithContext(ctx)
	rec := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		b.ServeHTTP(rec, req)
		close(done)
	}()

	time.Sleep(20 * time.Millisecond)
	cancel()
	<-done

	b.mu.Lock()
	count := len(b.clients)
	b.mu.Unlock()
	if count != 0 {
		t.Errorf("expected 0 clients after disconnect, got %d", count)
	}
}
