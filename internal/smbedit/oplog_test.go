package smbedit

import (
	"testing"
	"time"

	"go.uber.org/goleak"
)

func TestAddAndSnapshot(t *testing.T) {
	defer goleak.VerifyNone(t)
	l := newOpLog(10)
	l.add("first")
	l.addf("second: %d", 2)

	got := l.snapshot()
	if len(got) != 2 {
		t.Fatalf("expected 2 entries, got %d: %+v", len(got), got)
	}
	if got[0].Message != "first" || got[1].Message != "second: 2" {
		t.Errorf("unexpected entries: %+v", got)
	}
}

func TestSnapshot_BoundedByMax(t *testing.T) {
	defer goleak.VerifyNone(t)
	l := newOpLog(3)
	for i := 0; i < 5; i++ {
		l.addf("entry %d", i)
	}
	got := l.snapshot()
	if len(got) != 3 {
		t.Fatalf("expected 3 entries (bounded), got %d", len(got))
	}
	if got[0].Message != "entry 2" || got[2].Message != "entry 4" {
		t.Errorf("expected oldest entries dropped, got %+v", got)
	}
}

func TestSubscribe_ReceivesNewEntries(t *testing.T) {
	defer goleak.VerifyNone(t)
	l := newOpLog(10)
	ch, cancel := l.subscribe()
	defer cancel()

	l.add("hello")

	select {
	case e := <-ch:
		if e.Message != "hello" {
			t.Errorf("expected 'hello', got %q", e.Message)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for subscribed entry")
	}
}

func TestSubscribe_CancelStopsDelivery(t *testing.T) {
	defer goleak.VerifyNone(t)
	l := newOpLog(10)
	ch, cancel := l.subscribe()
	cancel()

	l.add("after cancel")

	select {
	case _, ok := <-ch:
		if ok {
			t.Error("expected channel to be closed after cancel, got a value instead")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for channel close")
	}
}
