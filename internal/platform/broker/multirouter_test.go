package broker

import (
	"sync"
	"testing"
	"time"
)

func TestRoomLazyCreation(t *testing.T) {
	m := NewMultiRoomBroker(0)
	if m.RoomCount() != 0 {
		t.Fatal("expected 0 rooms initially")
	}
	m.Room("alpha")
	if m.RoomCount() != 1 {
		t.Fatal("expected 1 room after first access")
	}
	m.Room("alpha")
	if m.RoomCount() != 1 {
		t.Fatal("expected same room returned on second access")
	}
}

func TestRoomReturnsDistinctBrokers(t *testing.T) {
	m := NewMultiRoomBroker(0)
	a := m.Room("alpha")
	b := m.Room("beta")
	if a == b {
		t.Fatal("expected distinct brokers for different keys")
	}
}

func TestRoomReturnsSameInstance(t *testing.T) {
	m := NewMultiRoomBroker(0)
	first := m.Room("alpha")
	second := m.Room("alpha")
	if first != second {
		t.Fatal("expected same broker instance for same key")
	}
}

func TestNotifyOnlyReachesTargetRoom(t *testing.T) {
	m := NewMultiRoomBroker(0)

	alphaClient := make(chan struct{}, 1)
	betaClient := make(chan struct{}, 1)
	m.Room("alpha").Subscribe(alphaClient)
	m.Room("beta").Subscribe(betaClient)

	m.Notify("alpha")

	select {
	case <-alphaClient:
	case <-time.After(time.Second):
		t.Fatal("alpha client did not receive signal")
	}
	select {
	case <-betaClient:
		t.Fatal("beta client should not have received signal from alpha Notify")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestNotifyNonExistentRoomIsNoop(t *testing.T) {
	m := NewMultiRoomBroker(0)
	// Must not panic or block.
	done := make(chan struct{})
	go func() {
		m.Notify("does-not-exist")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Notify on non-existent room blocked")
	}
}

func TestConcurrentRoomAccess(t *testing.T) {
	m := NewMultiRoomBroker(0)
	const goroutines = 20
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			m.Room("shared")
		}()
	}
	wg.Wait()
	if m.RoomCount() != 1 {
		t.Errorf("expected exactly 1 room after concurrent access, got %d", m.RoomCount())
	}
}

func TestConcurrentNotifyDifferentRooms(t *testing.T) {
	m := NewMultiRoomBroker(0)
	const n = 10
	clients := make([]chan struct{}, n)
	for i := 0; i < n; i++ {
		key := string(rune('a' + i))
		clients[i] = make(chan struct{}, 1)
		m.Room(key).Subscribe(clients[i])
	}

	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			m.Notify(string(rune('a' + i)))
		}(i)
	}
	wg.Wait()

	for i, ch := range clients {
		select {
		case <-ch:
		case <-time.After(time.Second):
			t.Errorf("client %d did not receive signal", i)
		}
	}
}

func TestRetryMsPassedToBroker(t *testing.T) {
	const retryMs = 2000
	m := NewMultiRoomBroker(retryMs)
	b := m.Room("x")
	if b.retryMs != retryMs {
		t.Errorf("expected retryMs %d, got %d", retryMs, b.retryMs)
	}
}
