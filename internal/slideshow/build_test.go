package slideshow_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"cmd184psu/unified-webapp/internal/platform/config"
	"cmd184psu/unified-webapp/internal/slideshow"
)

// TestBuild_SSEMaxSubscribersFromConfig verifies that a config file's
// server.sse_max_subscribers value reaches the slideshow module's real SSE
// endpoint end-to-end through config.Load and Build: with a cap of 2, a
// third concurrent subscriber is rejected with 503.
func TestBuild_SSEMaxSubscribersFromConfig(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	imageDir := filepath.Join(dir, "images")
	staticDir := filepath.Join(dir, "static")
	if err := os.MkdirAll(imageDir, 0o755); err != nil {
		t.Fatalf("mkdir imageDir: %v", err)
	}
	if err := os.MkdirAll(staticDir, 0o755); err != nil {
		t.Fatalf("mkdir staticDir: %v", err)
	}

	data, _ := json.Marshal(map[string]any{
		"server": map[string]any{
			"sse_max_subscribers": 2,
		},
		"slideshow": map[string]any{
			"static_dir": staticDir,
			"image_dir":  imageDir,
		},
	})
	if err := os.WriteFile(cfgPath, data, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if cfg.Slideshow.SSEMaxSubscribers != 2 {
		t.Fatalf("SSEMaxSubscribers: got %d, want 2", cfg.Slideshow.SSEMaxSubscribers)
	}

	h, err := slideshow.Build(cfg.Slideshow)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if c, ok := h.(io.Closer); ok {
		t.Cleanup(func() { _ = c.Close() })
	}
	srv := httptest.NewServer(h)
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	connect := func() *http.Response {
		t.Helper()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/events", nil)
		if err != nil {
			t.Fatalf("build request: %v", err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("connect: %v", err)
		}
		return resp
	}

	resp1 := connect()
	defer resp1.Body.Close()
	resp2 := connect()
	defer resp2.Body.Close()
	if resp1.StatusCode != http.StatusOK || resp2.StatusCode != http.StatusOK {
		t.Fatalf("first two subscribers: got %d, %d, want 200, 200", resp1.StatusCode, resp2.StatusCode)
	}

	// Give the server time to register both subscribers before checking the cap.
	time.Sleep(50 * time.Millisecond)

	resp3 := connect()
	defer resp3.Body.Close()
	if resp3.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("third subscriber: got %d, want 503", resp3.StatusCode)
	}
	if ct := resp3.Header.Get("Content-Type"); ct == "text/event-stream" {
		t.Errorf("third subscriber: got SSE Content-Type, want none on 503")
	}
}
