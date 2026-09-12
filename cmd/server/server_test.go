package main

import (
	"net/http"
	"testing"
	"time"
)

// TestNewServerTimeouts verifies ReadTimeout/WriteTimeout stay at zero (SSE
// and multissh WebSocket/terminal sessions are long-lived and must not be
// killed mid-stream) while ReadHeaderTimeout and IdleTimeout are configured.
func TestNewServerTimeouts(t *testing.T) {
	srv := newServer("0.0.0.0:8080", http.NotFoundHandler())

	if srv.Addr != "0.0.0.0:8080" {
		t.Errorf("Addr = %q, want %q", srv.Addr, "0.0.0.0:8080")
	}
	if srv.Handler == nil {
		t.Error("Handler is nil, want non-nil")
	}
	if got, want := srv.ReadHeaderTimeout, 10*time.Second; got != want {
		t.Errorf("ReadHeaderTimeout = %v, want %v", got, want)
	}
	if got, want := srv.IdleTimeout, 120*time.Second; got != want {
		t.Errorf("IdleTimeout = %v, want %v", got, want)
	}
	if srv.ReadTimeout != 0 {
		t.Errorf("ReadTimeout = %v, want 0 (SSE/WebSocket streams must not be killed)", srv.ReadTimeout)
	}
	if srv.WriteTimeout != 0 {
		t.Errorf("WriteTimeout = %v, want 0 (SSE/WebSocket streams must not be killed)", srv.WriteTimeout)
	}
}
