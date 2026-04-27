package broker

import (
	"fmt"
	"net/http"
	"sync"
)

// Broker manages Server-Sent Event connections and broadcasts refresh
// notifications to all connected clients whenever the data changes.
type Broker struct {
	mu      sync.Mutex
	clients map[chan struct{}]struct{}
	retryMs int
}

// NewBroker creates a Broker. retryMs is the reconnect interval hint sent
// to SSE clients via the "retry:" directive. Pass 0 to use the browser default.
func NewBroker(retryMs int) *Broker {
	return &Broker{clients: make(map[chan struct{}]struct{}), retryMs: retryMs}
}

// Notify sends a refresh signal to every connected SSE client.
func (b *Broker) Notify() {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.clients {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

func (b *Broker) add(ch chan struct{}) {
	b.mu.Lock()
	b.clients[ch] = struct{}{}
	b.mu.Unlock()
}

func (b *Broker) remove(ch chan struct{}) {
	b.mu.Lock()
	delete(b.clients, ch)
	b.mu.Unlock()
}

// Subscribe registers a channel to receive Notify signals.
func (b *Broker) Subscribe(ch chan struct{}) { b.add(ch) }

// Unsubscribe removes a previously registered channel.
func (b *Broker) Unsubscribe(ch chan struct{}) { b.remove(ch) }

func sseEvent(payload string) string {
	return fmt.Sprintf("data: %s\n\n", payload)
}

// ServeHTTP implements an SSE endpoint. The caller mounts this at a route such
// as GET /api/events. Each connected client receives a "data: refresh" message
// whenever Notify is called.
func (b *Broker) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	fmt.Fprintf(w, ": connected\n\n")
	if b.retryMs > 0 {
		fmt.Fprintf(w, "retry: %d\n\n", b.retryMs)
	}
	fl.Flush()

	ch := make(chan struct{}, 1)
	b.add(ch)
	defer b.remove(ch)

	for {
		select {
		case <-ch:
			fmt.Fprint(w, sseEvent("refresh"))
			fl.Flush()
		case <-r.Context().Done():
			return
		}
	}
}
