package broker

import "sync"

// MultiRoomBroker manages independent SSE rooms keyed by an arbitrary string
// (e.g. a todo list subject name). Rooms are created lazily on first access and
// are never destroyed, so callers should use stable, low-cardinality keys.
type MultiRoomBroker struct {
	mu      sync.Mutex
	rooms   map[string]*Broker
	retryMs int
}

// NewMultiRoomBroker creates a MultiRoomBroker. retryMs is passed to every
// Broker created by Room.
func NewMultiRoomBroker(retryMs int) *MultiRoomBroker {
	return &MultiRoomBroker{
		rooms:   make(map[string]*Broker),
		retryMs: retryMs,
	}
}

// Room returns the Broker for the given key, creating it if it does not exist.
func (m *MultiRoomBroker) Room(key string) *Broker {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.rooms[key]
	if !ok {
		b = NewBroker(m.retryMs)
		m.rooms[key] = b
	}
	return b
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
