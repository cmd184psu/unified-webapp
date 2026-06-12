package obsidianoid_test

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cmd184psu/unified-webapp/internal/obsidianoid"
)

func TestVaultWatcherNotifiesOnWrite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping fsnotify test in short mode")
	}

	vault := t.TempDir()
	// Seed a .md file so the vault isn't empty.
	_ = os.WriteFile(filepath.Join(vault, "Init.md"), []byte("init"), 0o644)

	b := obsidianoid.MakeTestBrokers(1)[0]
	closer, err := obsidianoid.StartVaultWatcher(vault, b)
	if err != nil {
		t.Fatalf("StartVaultWatcher: %v", err)
	}
	t.Cleanup(func() { closer.Close() })

	// Serve SSE via a real httptest server.
	srv := httptest.NewServer(http.HandlerFunc(b.ServeSSE))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("connect to SSE: %v", err)
	}
	defer resp.Body.Close()

	// Write a .md file after connecting; the watcher should fire.
	go func() {
		time.Sleep(50 * time.Millisecond)
		_ = os.WriteFile(filepath.Join(vault, "New.md"), []byte("# New"), 0o644)
	}()

	scanner := bufio.NewScanner(resp.Body)
	deadline := time.After(4 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("timeout: did not receive note-changed event")
		default:
		}
		if !scanner.Scan() {
			if ctx.Err() != nil {
				t.Fatal("context expired before note-changed event")
			}
			t.Fatalf("scanner stopped: %v", scanner.Err())
		}
		line := scanner.Text()
		if strings.Contains(line, "note-changed") {
			return // success
		}
	}
}
