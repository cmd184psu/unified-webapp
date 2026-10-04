package haproxy

import (
	"testing"
	"time"

	"go.uber.org/goleak"
)

func TestOpLogAddAndSnapshot(t *testing.T) {
	defer goleak.VerifyNone(t)
	l := NewOpLog(10)
	l.Add("first")
	l.Addf("second: %d", 2)

	got := l.Snapshot()
	if len(got) != 2 {
		t.Fatalf("expected 2 entries, got %d: %+v", len(got), got)
	}
	if got[0].Message != "first" || got[1].Message != "second: 2" {
		t.Errorf("unexpected entries: %+v", got)
	}
}

func TestOpLogBoundedByMax(t *testing.T) {
	defer goleak.VerifyNone(t)
	l := NewOpLog(3)
	for i := 0; i < 5; i++ {
		l.Addf("entry %d", i)
	}
	got := l.Snapshot()
	if len(got) != 3 {
		t.Fatalf("expected 3 entries (bounded), got %d", len(got))
	}
	if got[0].Message != "entry 2" || got[2].Message != "entry 4" {
		t.Errorf("expected oldest entries dropped, got %+v", got)
	}
}

func TestOpLogSubscribeReceivesNewEntries(t *testing.T) {
	defer goleak.VerifyNone(t)
	l := NewOpLog(10)
	ch, cancel := l.Subscribe()
	defer cancel()

	l.Add("hello")

	select {
	case e := <-ch:
		if e.Message != "hello" {
			t.Errorf("expected 'hello', got %q", e.Message)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for subscribed entry")
	}
}

func TestOpLogCancelStopsDelivery(t *testing.T) {
	defer goleak.VerifyNone(t)
	l := NewOpLog(10)
	ch, cancel := l.Subscribe()
	cancel()

	l.Add("after cancel")

	select {
	case _, ok := <-ch:
		if ok {
			t.Error("expected channel to be closed after cancel, got a value instead")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for channel close")
	}
}

// A slow subscriber (buffer full, not draining) must not block the writer.
func TestOpLogSlowSubscriberDropped(t *testing.T) {
	defer goleak.VerifyNone(t)
	l := NewOpLog(100)
	_, cancel := l.Subscribe()
	defer cancel()
	done := make(chan struct{})
	go func() {
		for i := 0; i < 1000; i++ {
			l.Addf("entry %d", i)
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("writer blocked on a slow subscriber")
	}
}
