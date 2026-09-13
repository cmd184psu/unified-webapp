// Package smbedit is a web UI for managing Samba shares: it renders and
// writes smb.conf from a persisted state file, restarts the Samba service,
// and streams operation and daemon logs to the browser. Ported from the
// standalone smbed project.
package smbedit

import (
	"fmt"
	"sync"
	"time"
)

// opEntry is a single operations-log line.
type opEntry struct {
	Time    time.Time `json:"time"`
	Message string    `json:"message"`
}

// opLog holds a bounded ring buffer of entries and fans new ones out to any
// active subscribers (e.g. SSE streams).
type opLog struct {
	mu      sync.Mutex
	entries []opEntry
	max     int
	subs    map[chan opEntry]struct{}
}

// newOpLog returns an opLog that retains at most max entries.
func newOpLog(max int) *opLog {
	return &opLog{max: max, subs: make(map[chan opEntry]struct{})}
}

// add appends message as a new entry, timestamped now, and notifies any
// active subscribers. Slow subscribers have entries dropped rather than
// blocking the caller.
func (l *opLog) add(message string) opEntry {
	e := opEntry{Time: time.Now(), Message: message}

	l.mu.Lock()
	l.entries = append(l.entries, e)
	if len(l.entries) > l.max {
		l.entries = l.entries[len(l.entries)-l.max:]
	}
	subs := make([]chan opEntry, 0, len(l.subs))
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

// addf is add with fmt.Sprintf-style formatting.
func (l *opLog) addf(format string, args ...any) opEntry {
	return l.add(fmt.Sprintf(format, args...))
}

// snapshot returns a copy of the currently retained entries, oldest first.
func (l *opLog) snapshot() []opEntry {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]opEntry, len(l.entries))
	copy(out, l.entries)
	return out
}

// subscribe registers a channel that receives every entry added after this
// call. The returned cancel func must be called to unregister and release
// the channel; failing to do so leaks the subscription.
func (l *opLog) subscribe() (<-chan opEntry, func()) {
	ch := make(chan opEntry, 32)
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
