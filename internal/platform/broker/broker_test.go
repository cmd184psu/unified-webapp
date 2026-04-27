package broker

import (
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

	go b.ServeHTTP(rec, req)
	time.Sleep(20 * time.Millisecond)
	b.Notify()
	time.Sleep(20 * time.Millisecond)
	cancel()
	time.Sleep(20 * time.Millisecond)

	body := rec.Body.String()
	if !strings.Contains(body, "data: refresh") {
		t.Errorf("expected refresh event in body, got: %q", body)
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
