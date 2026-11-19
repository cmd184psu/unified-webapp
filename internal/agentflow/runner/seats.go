package runner

import (
	"sort"
	"sync"
	"time"
)

// Seat attention kinds: what a seat stage is waiting on the owner for.
const (
	// SeatQuestion: the seat asked something and waits for the answer.
	SeatQuestion = "seat_question"
	// SeatSilent: the seat has shown nothing for longer than the role's budget. A
	// seat is never retried for silence; the owner looks at it.
	SeatSilent = "seat_silent"
)

// SeatNotice is one seat stage that needs the owner.
type SeatNotice struct {
	Stage   int64     `json:"stage"`
	Kind    string    `json:"kind"`
	Message string    `json:"message"`
	Since   time.Time `json:"since"`
	// Session is the terminal session the seat runs in, empty for a BotChat seat.
	Session string `json:"session,omitempty"`
	// Attach is the command that opens the seat's terminal, empty for a BotChat seat.
	Attach string `json:"attach,omitempty"`
}

// SeatBoard holds what the seat stages of this process are waiting on. Runners set and
// clear their stage's notice as they poll; the engine lists the board as attention items.
// It is memory only: after a restart an adopted stage sets its notice again on its first poll.
type SeatBoard struct {
	mu sync.Mutex
	m  map[int64]SeatNotice
}

// Set records the stage's notice; an unchanged kind and message keep their first Since.
func (b *SeatBoard) Set(n SeatNotice) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.m == nil {
		b.m = map[int64]SeatNotice{}
	}
	if old, ok := b.m[n.Stage]; ok && old.Kind == n.Kind && old.Message == n.Message {
		return
	}
	b.m[n.Stage] = n
}

// Clear forgets the stage's notice.
func (b *SeatBoard) Clear(stage int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.m, stage)
}

// Get returns the stage's notice.
func (b *SeatBoard) Get(stage int64) (SeatNotice, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n, ok := b.m[stage]
	return n, ok
}

// List returns every notice, oldest first.
func (b *SeatBoard) List() []SeatNotice {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]SeatNotice, 0, len(b.m))
	for _, n := range b.m {
		out = append(out, n)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Since.Before(out[j].Since) })
	return out
}
