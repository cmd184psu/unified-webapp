package slideshow

import (
	"fmt"
	"net/http"
	"sync"
	"time"
)

// SSEBroker fans out JSON state strings to all connected SSE clients.
type SSEBroker struct {
	mu      sync.Mutex
	clients map[chan string]struct{}
}

// NewSSEBroker allocates a ready-to-use SSEBroker.
func NewSSEBroker() *SSEBroker {
	return &SSEBroker{clients: make(map[chan string]struct{})}
}

func (b *SSEBroker) subscribe() chan string {
	ch := make(chan string, 8)
	b.mu.Lock()
	b.clients[ch] = struct{}{}
	b.mu.Unlock()
	return ch
}

func (b *SSEBroker) unsubscribe(ch chan string) {
	b.mu.Lock()
	delete(b.clients, ch)
	b.mu.Unlock()
	close(ch)
}

// Publish sends msg to all connected clients, dropping any that are slow.
func (b *SSEBroker) Publish(msg string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.clients {
		select {
		case ch <- msg:
		default:
		}
	}
}

// ServeSSE returns an HTTP handler for GET /api/events.
// snapshot is called once on connect to send the current state immediately.
func (b *SSEBroker) ServeSSE(snapshot func() string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("X-Accel-Buffering", "no")

		// Send current state immediately so joining clients snap to position.
		fmt.Fprintf(w, "event: state\ndata: %s\n\n", snapshot())
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
				fmt.Fprintf(w, "event: state\ndata: %s\n\n", msg)
				flusher.Flush()
			case <-ticker.C:
				fmt.Fprintf(w, ": keep-alive\n\n")
				flusher.Flush()
			case <-r.Context().Done():
				return
			}
		}
	}
}
