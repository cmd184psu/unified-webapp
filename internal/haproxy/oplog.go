package haproxy

// The editor's operations log (FR-H24). A bounded ring buffer of timestamped
// lines that the Apply pipeline, backups and service actions write to, and that
// the HTTP layer (B4b) streams to the browser over SSE. It mirrors the proven
// ring-buffer + fan-out pattern in internal/smbedit/oplog.go: new entries are
// delivered to every active subscriber, and a subscriber too slow to keep up
// has entries dropped rather than blocking the writer.

import (
	"fmt"
	"sync"
	"time"
)

// OpEntry is a single operations-log line.
type OpEntry struct {
	Time    time.Time `json:"time"`
	Message string    `json:"message"`
}

// OpLog holds a bounded ring buffer of entries and fans new ones out to any
// active subscribers. Safe for concurrent use.
type OpLog struct {
	mu      sync.Mutex
	entries []OpEntry
	max     int
	subs    map[chan OpEntry]struct{}
}

// NewOpLog returns an OpLog that retains at most max entries (at least 1).
func NewOpLog(max int) *OpLog {
	if max < 1 {
		max = 1
	}
	return &OpLog{max: max, subs: make(map[chan OpEntry]struct{})}
}

// Add appends message as a new entry, timestamped now, and notifies every
// active subscriber. Slow subscribers have the entry dropped rather than
// blocking the caller.
func (l *OpLog) Add(message string) OpEntry {
	e := OpEntry{Time: time.Now(), Message: message}

	l.mu.Lock()
	l.entries = append(l.entries, e)
	if len(l.entries) > l.max {
		l.entries = l.entries[len(l.entries)-l.max:]
	}
	subs := make([]chan OpEntry, 0, len(l.subs))
	for ch := range l.subs {
		subs = append(subs, ch)
	}
	l.mu.Unlock()

	for _, ch := range subs {
		select {
		case ch <- e:
		default:
		}
	}
	return e
}

// Addf is Add with fmt.Sprintf-style formatting.
func (l *OpLog) Addf(format string, args ...any) OpEntry {
	return l.Add(fmt.Sprintf(format, args...))
}

// Snapshot returns a copy of the currently retained entries, oldest first.
func (l *OpLog) Snapshot() []OpEntry {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]OpEntry, len(l.entries))
	copy(out, l.entries)
	return out
}

// Subscribe registers a channel that receives every entry added after this
// call. The returned cancel func must be called to unregister and release the
// channel; failing to do so leaks the subscription.
func (l *OpLog) Subscribe() (<-chan OpEntry, func()) {
	ch := make(chan OpEntry, 32)
	l.mu.Lock()
	l.subs[ch] = struct{}{}
	l.mu.Unlock()

	cancel := func() {
		l.mu.Lock()
		if _, ok := l.subs[ch]; ok {
			delete(l.subs, ch)
			close(ch)
		}
		l.mu.Unlock()
	}
	return ch, cancel
}
