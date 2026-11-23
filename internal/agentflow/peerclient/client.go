// Copyright 2026 FreeSociety3 contributors
// SPDX-License-Identifier: Apache-2.0

package peerclient

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// HTTPError is a peer answer with a status >= 400. It is not retried: the
// request reached the peer and the peer refused it, so repeating it changes
// nothing.
type HTTPError struct {
	StatusCode int
	Body       string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("peer answered %d: %s", e.StatusCode, e.Body)
}

// breaker is one peer's circuit breaker. It is shared by every Client that
// names the same Peer, because callers build a fresh Client per request.
type breaker struct {
	mu        sync.Mutex
	failures  int
	openUntil time.Time
}

var sharedBreakers sync.Map // peer name -> *breaker

// breakerFor returns the shared breaker for peer, or nil when the client is
// not bound to a named peer (tests and one-off calls).
func breakerFor(peer string) *breaker {
	if peer == "" {
		return nil
	}
	v, _ := sharedBreakers.LoadOrStore(peer, &breaker{})
	return v.(*breaker)
}

// ResetBreakersForTest clears every shared breaker.
func ResetBreakersForTest() { sharedBreakers = sync.Map{} }

func (b *breaker) open() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	if b.failures >= 5 && now.Before(b.openUntil) {
		return true
	}
	if now.After(b.openUntil) {
		b.failures = 0
	}
	return false
}

func (b *breaker) recordFailure() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.failures++
	if b.failures >= 5 {
		b.openUntil = time.Now().Add(30 * time.Second)
	}
}

func (b *breaker) recordSuccess() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.failures = 0
}

type Client struct {
	// Peer names the peer this client talks to. Clients with the same Peer
	// share one circuit breaker, so a caller that builds a Client per request
	// still trips it. Empty keeps the breaker local to the instance.
	Peer               string
	Timeout            time.Duration
	InsecureSkipVerify bool

	local breaker
}

func (c *Client) timeout() time.Duration {
	if c.Timeout > 0 {
		return c.Timeout
	}
	return 30 * time.Second
}

func (c *Client) state() *breaker {
	if b := breakerFor(c.Peer); b != nil {
		return b
	}
	return &c.local
}

func (c *Client) circuitOpen() bool { return c.state().open() }

func (c *Client) recordFailure() { c.state().recordFailure() }

func (c *Client) recordSuccess() { c.state().recordSuccess() }

type Load struct {
	OneMinute float64 `json:"1m"`
	Cores     int     `json:"cores"`
	FreeGB    float64 `json:"free_gb"`
}

type PeerStatus struct {
	Protocol  int      `json:"protocol"`
	Node      string   `json:"node"`
	Build     string   `json:"build"`
	Schema    int      `json:"schema"`
	Time      int64    `json:"time"`
	Accepting bool     `json:"accepting"`
	Load      Load     `json:"load"`
	Backends  []any    `json:"backends"`
	Projects  []any    `json:"projects"`
	Delegable []string `json:"delegable"`
}

func (c *Client) Do(ctx context.Context, method, url, key string, hostHeader string, body any, out any) error {
	if c.circuitOpen() {
		return fmt.Errorf("circuit open for peer %q", c.Peer)
	}
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(attempt) * 200 * time.Millisecond):
			}
		}
		err := c.doOnce(ctx, method, url, key, hostHeader, body, out)
		if err == nil {
			c.recordSuccess()
			return nil
		}
		lastErr = err
		var he *HTTPError
		if errors.As(err, &he) {
			// The peer answered; retrying a refusal cannot help.
			return err
		}
		if ctx.Err() != nil {
			break
		}
	}
	c.recordFailure()
	return lastErr
}

func (c *Client) doOnce(ctx context.Context, method, url, key string, hostHeader string, body any, out any) error {
	var r *http.Request
	var err error
	if body != nil {
		b, _ := json.Marshal(body)
		r, err = http.NewRequestWithContext(ctx, method, url, bytes.NewReader(b))
	} else {
		r, err = http.NewRequestWithContext(ctx, method, url, nil)
	}
	if err != nil {
		return err
	}
	if key != "" {
		r.Header.Set("Authorization", "Bearer "+key)
	}
	if hostHeader != "" {
		r.Host = hostHeader
	}
	r.Header.Set("Content-Type", "application/json")
	hc := &http.Client{Timeout: c.timeout()}
	if c.InsecureSkipVerify {
		hc.Transport = &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	}
	resp, err := hc.Do(r)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return &HTTPError{StatusCode: resp.StatusCode, Body: string(data)}
	}
	if out != nil {
		return json.Unmarshal(data, out)
	}
	return nil
}
