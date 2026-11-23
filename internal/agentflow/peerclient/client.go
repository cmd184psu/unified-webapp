// Copyright 2026 FreeSociety3 contributors
// SPDX-License-Identifier: Apache-2.0

package peerclient

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

type Client struct {
	Timeout            time.Duration
	InsecureSkipVerify bool
	// circuit breaker state
	mu        sync.Mutex
	failures  int
	openUntil time.Time
}

func (c *Client) timeout() time.Duration {
	if c.Timeout > 0 {
		return c.Timeout
	}
	return 30 * time.Second
}

func (c *Client) circuitOpen() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.failures >= 5 && time.Now().Before(c.openUntil) {
		return true
	}
	if time.Now().After(c.openUntil) {
		c.failures = 0
	}
	return false
}

func (c *Client) recordFailure() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.failures++
	if c.failures >= 5 {
		c.openUntil = time.Now().Add(30 * time.Second)
	}
}

func (c *Client) recordSuccess() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.failures = 0
}

// PeerBackend is one backend in a peer's status.
type PeerBackend struct {
	Name      string   `json:"name"`
	Slots     int      `json:"slots"`
	Running   int      `json:"running"`
	RestUntil []any    `json:"rest_until"`
	Models    []string `json:"models,omitempty"`
}

type PeerStatus struct {
	Protocol  int    `json:"protocol"`
	Node      string `json:"node"`
	Build     string `json:"build"`
	Schema    int    `json:"schema"`
	Time      int64  `json:"time"`
	Accepting bool   `json:"accepting"`
	Load      struct {
		Load1m    float64 `json:"1m"`
		Cores     int     `json:"cores"`
		FreeGB    float64 `json:"free_gb"`
		FreeMemGB float64 `json:"free_mem_gb"`
	} `json:"load"`
	Backends  []PeerBackend `json:"backends"`
	Projects  []any         `json:"projects"`
	Delegable []string      `json:"delegable"`
	// RestUntil maps "backend\x00model" to the epoch millisecond the peer's
	// last rate limit holds it until (AF-25). A node that uses the same
	// account applies the later of the two rests.
	RestUntil map[string]int64 `json:"rest_until,omitempty"`
}

func (c *Client) Do(ctx context.Context, method, url, key string, hostHeader string, body any, out any) error {
	if c.circuitOpen() {
		return fmt.Errorf("circuit open")
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
		// retry only on network errors
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
		r.Header.Set("Host", hostHeader)
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
		return fmt.Errorf("peer %s: %d %s", url, resp.StatusCode, string(data))
	}
	if out != nil {
		return json.Unmarshal(data, out)
	}
	return nil
}
