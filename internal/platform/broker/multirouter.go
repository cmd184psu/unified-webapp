package broker

import "sync"

// MultiRoomBroker manages independent SSE rooms keyed by an arbitrary string
// (e.g. a todo list subject name). Rooms are created lazily on first access and
// are never destroyed, so callers should use stable, low-cardinality keys.
type MultiRoomBroker struct {
	mu      sync.Mutex
	rooms   map[string]*Broker
	retryMs int
	maxSubs int
}

// NewMultiRoomBroker creates a MultiRoomBroker. retryMs is passed to every
// Broker created by Room.
func NewMultiRoomBroker(retryMs int) *MultiRoomBroker {
	return &MultiRoomBroker{
		rooms:   make(map[string]*Broker),
		retryMs: retryMs,
		maxSubs: DefaultMaxSubscribers,
	}
}

// Room returns the Broker for the given key, creating it if it does not
// exist. Newly created rooms inherit the cap set via SetMaxSubscribers.
func (m *MultiRoomBroker) Room(key string) *Broker {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.rooms[key]
	if !ok {
		b = NewBroker(m.retryMs)
		b.SetMaxSubscribers(m.maxSubs)
		m.rooms[key] = b
	}
	return b
}

// SetMaxSubscribers sets the subscriber cap applied to rooms created from now
// on via Room, and updates every room already created. n<=0 resets the cap to
// DefaultMaxSubscribers.
func (m *MultiRoomBroker) SetMaxSubscribers(n int) {
	if n <= 0 {
		n = DefaultMaxSubscribers
	}
	m.mu.Lock()
	m.maxSubs = n
	for _, b := range m.rooms {
		b.SetMaxSubscribers(n)
	}
	m.mu.Unlock()
}

// Notify sends a refresh signal to all clients connected to the given room.
// If the room does not exist yet, Notify is a no-op.
func (m *MultiRoomBroker) Notify(key string) {
	m.mu.Lock()
	b, ok := m.rooms[key]
	m.mu.Unlock()
	if ok {
		b.Notify()
	}
}

// RoomCount returns the number of rooms that have been created.
func (m *MultiRoomBroker) RoomCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.rooms)
}
