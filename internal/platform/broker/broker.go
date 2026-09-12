package broker

import (
	"fmt"
	"net/http"
	"sync"
	"time"

	"cmd184psu/unified-webapp/internal/platform/response"
)

// DefaultMaxSubscribers is the subscriber cap applied when SetMaxSubscribers
// is never called, or is called with n<=0.
const DefaultMaxSubscribers = 64

// Broker manages Server-Sent Event connections and broadcasts refresh
// notifications to all connected clients whenever the data changes.
type Broker struct {
	mu      sync.Mutex
	clients map[chan struct{}]struct{}
	payload map[chan string]struct{}
	retryMs int
	maxSubs int
	count   int // active SSE subscribers across clients and payload
}

// NewBroker creates a Broker. retryMs is the reconnect interval hint sent
// to SSE clients via the "retry:" directive. Pass 0 to use the browser default.
func NewBroker(retryMs int) *Broker {
	return &Broker{
		clients: make(map[chan struct{}]struct{}),
		payload: make(map[chan string]struct{}),
		retryMs: retryMs,
		maxSubs: DefaultMaxSubscribers,
	}
}

// SetMaxSubscribers sets the maximum number of concurrent SSE subscribers
// this Broker will accept, across both ServeHTTP and ServeSSE. n<=0 resets
// the cap to DefaultMaxSubscribers.
func (b *Broker) SetMaxSubscribers(n int) {
	if n <= 0 {
		n = DefaultMaxSubscribers
	}
	b.mu.Lock()
	b.maxSubs = n
	b.mu.Unlock()
}

// acquire reserves a subscriber slot, returning false if the Broker is at
// its subscriber cap. Callers must call release when the connection ends.
func (b *Broker) acquire() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.count >= b.maxSubs {
		return false
	}
	b.count++
	return true
}

func (b *Broker) release() {
	b.mu.Lock()
	b.count--
	b.mu.Unlock()
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
//
// If the Broker is already at its subscriber cap (see SetMaxSubscribers), the
// connection is rejected with 503 before any SSE headers are written.
func (b *Broker) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !b.acquire() {
		response.WriteError(w, http.StatusServiceUnavailable, "too many subscribers")
		return
	}
	defer b.release()

	fl, ok := w.(http.Flusher)
	if !ok {
		response.WriteError(w, http.StatusInternalServerError, "streaming unsupported")
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

// Publish sends msg to all clients connected via ServeSSE, dropping any that
// are slow. Lifted from the slideshow and obsidianoid modules' local brokers.
func (b *Broker) Publish(msg string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.payload {
		select {
		case ch <- msg:
		default:
		}
	}
}

func (b *Broker) subscribePayload() chan string {
	ch := make(chan string, 8)
	b.mu.Lock()
	b.payload[ch] = struct{}{}
	b.mu.Unlock()
	return ch
}

func (b *Broker) unsubscribePayload(ch chan string) {
	b.mu.Lock()
	delete(b.payload, ch)
	b.mu.Unlock()
	close(ch)
}

// ServeSSE returns an HTTP handler for a payload-carrying SSE endpoint (e.g.
// GET /api/events). Published messages are sent as "event: <eventName>"
// frames. If snapshot is non-nil, it is called once on connect and its result
// sent immediately as the first event, so joining clients snap to the current
// state (lifted from slideshow's SSEBroker.ServeSSE). If snapshot is nil, a
// bare ": connected" comment is sent instead, matching obsidianoid's
// eventBroker, which has no meaningful initial state to send. A 30-second
// keep-alive comment is sent when no event fires in that window, also lifted
// from both former implementations.
//
// If the Broker is already at its subscriber cap (see SetMaxSubscribers), the
// connection is rejected with 503 before any SSE headers are written.
func (b *Broker) ServeSSE(eventName string, snapshot func() string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !b.acquire() {
			response.WriteError(w, http.StatusServiceUnavailable, "too many subscribers")
			return
		}
		defer b.release()

		flusher, ok := w.(http.Flusher)
		if !ok {
			response.WriteError(w, http.StatusInternalServerError, "streaming not supported")
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("X-Accel-Buffering", "no")

		if snapshot != nil {
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventName, snapshot())
		} else {
			fmt.Fprintf(w, ": connected\n\n")
		}
		flusher.Flush()

		ch := b.subscribePayload()
		defer b.unsubscribePayload(ch)

		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case msg, ok := <-ch:
				if !ok {
					return
				}
				fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventName, msg)
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
