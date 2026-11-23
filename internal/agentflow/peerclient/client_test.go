// Copyright 2026 FreeSociety3 contributors
// SPDX-License-Identifier: Apache-2.0

package peerclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestClientDoesNotRetryHTTPError(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		http.Error(w, "no", http.StatusInternalServerError)
	}))
	defer srv.Close()
	c := &Client{Peer: t.Name()}
	err := c.Do(context.Background(), http.MethodGet, srv.URL, "", "", nil, nil)
	if err == nil {
		t.Fatal("want an error")
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("requests = %d, want 1 (an HTTP 500 is not retried)", got)
	}
}

func TestClientRetriesNetworkError(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) < 3 {
			// Drop the connection: a transport error, which is retried.
			hj, ok := w.(http.Hijacker)
			if !ok {
				t.Fatal("no hijacker")
			}
			conn, _, _ := hj.Hijack()
			conn.Close()
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	c := &Client{Peer: t.Name()}
	if err := c.Do(context.Background(), http.MethodGet, srv.URL, "", "", nil, nil); err != nil {
		t.Fatalf("Do: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Fatalf("requests = %d, want 3", got)
	}
}

func TestClientSetsRequestHost(t *testing.T) {
	got := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got <- r.Host
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	c := &Client{Peer: t.Name()}
	if err := c.Do(context.Background(), http.MethodGet, srv.URL, "", "peer.example", nil, nil); err != nil {
		t.Fatal(err)
	}
	if seen := <-got; seen != "peer.example" {
		t.Fatalf("Host seen by server = %q", seen)
	}
}

func TestClientBreakerSharedAcrossInstances(t *testing.T) {
	ResetBreakersForTest()
	peer := t.Name()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hj, _ := w.(http.Hijacker)
		conn, _, _ := hj.Hijack()
		conn.Close()
	}))
	defer srv.Close()

	for i := 0; i < 5; i++ {
		breakerFor(peer).recordFailure()
	}
	// A fresh Client for the same peer must see the shared open breaker.
	c := &Client{Peer: peer}
	if !c.circuitOpen() {
		t.Fatal("a new Client for the peer did not share the open breaker")
	}
	err := c.Do(context.Background(), http.MethodGet, srv.URL, "", "", nil, nil)
	if err == nil || !strings.Contains(err.Error(), "circuit open") {
		t.Fatalf("error = %v, want circuit open", err)
	}
}
