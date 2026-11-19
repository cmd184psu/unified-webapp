// Package notify tells the owner that agentflow needs attention. It watches the attention
// list and, when an item appears that was not there before, sends exactly Text: no project,
// task or reason ever leaves the machine through a notification. Sends are debounced: at most
// one per Gap while items persist, and an item that appears inside the gap is announced when
// the gap ends if something still needs attention.
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

// Text is the one notification text.
const Text = "Agentflow needs attention"

// DefaultGap is the least time between two notifications.
const DefaultGap = 10 * time.Minute

// Config is where notifications go; read on every send, so a setting change needs no restart.
type Config struct {
	MacOS   bool
	Command string
	URL     string
}

// Watcher compares the attention list over time and sends when it gains an item.
type Watcher struct {
	// Keys returns the identity of every current attention item.
	Keys func(ctx context.Context) ([]string, error)
	// Config returns where to send.
	Config func(ctx context.Context) Config
	// Send delivers the text to one configured target; nil uses Deliver.
	Send  func(ctx context.Context, cfg Config, text string) error
	Gap   time.Duration
	Every time.Duration
	Clock func() time.Time

	mu      sync.Mutex
	primed  bool
	seen    map[string]bool
	last    time.Time
	pending bool
}

func (w *Watcher) now() time.Time {
	if w.Clock != nil {
		return w.Clock()
	}
	return time.Now()
}

func (w *Watcher) gap() time.Duration {
	if w.Gap > 0 {
		return w.Gap
	}
	return DefaultGap
}

// Check reads the attention list once and sends when it should; it reports whether it sent.
// The first check only learns what is already there, so a restart does not announce old items.
func (w *Watcher) Check(ctx context.Context) (bool, error) {
	keys, err := w.Keys(ctx)
	if err != nil {
		return false, err
	}
	w.mu.Lock()
	cur := make(map[string]bool, len(keys))
	gained := false
	for _, k := range keys {
		cur[k] = true
		if !w.seen[k] {
			gained = true
		}
	}
	first := !w.primed
	w.primed, w.seen = true, cur
	switch {
	case len(cur) == 0:
		w.pending = false
	case gained && !first:
		w.pending = true
	}
	send := w.pending && (w.last.IsZero() || w.now().Sub(w.last) >= w.gap())
	if send {
		w.pending, w.last = false, w.now()
	}
	w.mu.Unlock()
	if !send {
		return false, nil
	}
	cfg := Config{}
	if w.Config != nil {
		cfg = w.Config(ctx)
	}
	sender := w.Send
	if sender == nil {
		sender = Deliver
	}
	return true, sender(ctx, cfg, Text)
}

// Run checks every Every (default 30s) until ctx ends.
func (w *Watcher) Run(ctx context.Context) {
	every := w.Every
	if every <= 0 {
		every = 30 * time.Second
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		_, _ = w.Check(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Key is an attention item's identity for the watcher.
func Key(parts ...any) string {
	s := make([]string, len(parts))
	for i, p := range parts {
		s[i] = fmt.Sprint(p)
	}
	return strings.Join(s, "|")
}

// SortedKeys is a helper for tests and logs.
func SortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Deliver sends text to every configured target: the macOS notification centre, the hook
// command (text as its one argument) and the hook URL (POST {"text": text}).
func Deliver(ctx context.Context, cfg Config, text string) error {
	var errs []error
	if cfg.MacOS && runtime.GOOS == "darwin" {
		script := fmt.Sprintf("display notification %q", text)
		if out, err := exec.CommandContext(ctx, "/usr/bin/osascript", "-e", script).CombinedOutput(); err != nil {
			errs = append(errs, fmt.Errorf("osascript: %v: %s", err, strings.TrimSpace(string(out))))
		}
	}
	if cfg.Command != "" {
		cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		out, err := exec.CommandContext(cctx, "/bin/sh", "-c", cfg.Command+` "$1"`, "agentflow-notify", text).CombinedOutput()
		cancel()
		if err != nil {
			errs = append(errs, fmt.Errorf("notify command: %v: %s", err, strings.TrimSpace(string(out))))
		}
	}
	if cfg.URL != "" {
		body, _ := json.Marshal(map[string]string{"text": text})
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.URL, bytes.NewReader(body))
		if err == nil {
			req.Header.Set("Content-Type", "application/json")
			var resp *http.Response
			resp, err = (&http.Client{Timeout: 15 * time.Second}).Do(req)
			if err == nil {
				resp.Body.Close()
				if resp.StatusCode/100 != 2 {
					err = fmt.Errorf("status %s", resp.Status)
				}
			}
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("notify url: %w", err))
		}
	}
	return errors.Join(errs...)
}
