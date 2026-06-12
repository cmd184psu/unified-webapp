package obsidianoid

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// eventBroker fans out vault-change messages to all connected SSE clients.
type eventBroker struct {
	mu      sync.Mutex
	clients map[chan string]struct{}
}

func newEventBroker() *eventBroker {
	return &eventBroker{clients: make(map[chan string]struct{})}
}

func (b *eventBroker) subscribe() chan string {
	ch := make(chan string, 8)
	b.mu.Lock()
	b.clients[ch] = struct{}{}
	b.mu.Unlock()
	return ch
}

func (b *eventBroker) unsubscribe(ch chan string) {
	b.mu.Lock()
	delete(b.clients, ch)
	b.mu.Unlock()
	close(ch)
}

func (b *eventBroker) publish(msg string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.clients {
		select {
		case ch <- msg:
		default: // drop rather than block if a client is slow
		}
	}
}

// serveSSE is the HTTP handler for GET /api/events.
func (b *eventBroker) serveSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	fmt.Fprintf(w, ": connected\n\n")
	flusher.Flush()

	ch := b.subscribe()
	defer b.unsubscribe(ch)

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case msg, ok := <-ch:
			if !ok {
				return
			}
			fmt.Fprintf(w, "event: note-changed\ndata: %s\n\n", msg)
			flusher.Flush()
		case <-ticker.C:
			fmt.Fprintf(w, ": keep-alive\n\n")
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

// MakeTestBrokers creates n event brokers without starting any file watchers.
// Intended for use in tests where file watching is not needed.
func MakeTestBrokers(n int) []*eventBroker {
	brokers := make([]*eventBroker, n)
	for i := range brokers {
		brokers[i] = newEventBroker()
	}
	return brokers
}

// StartVaultWatcher is exported for tests.
func StartVaultWatcher(vaultPath string, b *eventBroker) (io.Closer, error) {
	return startVaultWatcher(vaultPath, b)
}

// ServeSSE is exported for tests (called as a method on the broker returned by MakeTestBrokers).
func (b *eventBroker) ServeSSE(w http.ResponseWriter, r *http.Request) {
	b.serveSSE(w, r)
}

// startVaultWatcher watches every directory under vaultPath for .md file changes
// and publishes note-changed events to broker. Returns the watcher for cleanup and
// any startup error.
func startVaultWatcher(vaultPath string, broker *eventBroker) (io.Closer, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	// addDirs registers a directory and all its non-hidden subdirectories.
	var addDirs func(root string)
	addDirs = func(root string) {
		filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil || !d.IsDir() {
				return nil
			}
			if strings.HasPrefix(d.Name(), ".") && path != root {
				return filepath.SkipDir
			}
			_ = watcher.Add(path)
			return nil
		})
	}
	addDirs(vaultPath)

	go func() {
		defer watcher.Close()
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if event.Has(fsnotify.Create) {
					if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
						addDirs(event.Name)
					}
				}
				if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) {
					if strings.ToLower(filepath.Ext(event.Name)) == ".md" {
						rel, err := filepath.Rel(vaultPath, event.Name)
						if err == nil {
							broker.publish(fmt.Sprintf(`{"path":%q}`, filepath.ToSlash(rel)))
						}
					}
				}
			case _, ok := <-watcher.Errors:
				if !ok {
					return
				}
			}
		}
	}()

	return watcher, nil
}
