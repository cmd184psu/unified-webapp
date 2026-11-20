package store

import (
	"context"
	"testing"
)

// AF-10: the hold is stored with the projects it paused and survives a restart;
// placing it again keeps them, and lifting it returns what it was.
func TestServiceHoldSurvivesReopen(t *testing.T) {
	ctx := context.Background()
	s, path := openTemp(t)
	if h, err := s.ServiceHold(ctx); err != nil || h.Active {
		t.Fatalf("a fresh store is held: %+v, %v", h, err)
	}
	if _, err := s.HoldService(ctx, "owner", "  ", nil, false); err == nil {
		t.Fatal("a hold without a reason was accepted")
	}
	h, err := s.HoldService(ctx, "owner", "trying something", []string{"p1", "p2"}, true)
	if err != nil || !h.Active || h.Reason != "trying something" || len(h.Paused) != 2 || !h.Restart {
		t.Fatalf("hold = %+v, %v", h, err)
	}
	s.Close()

	s2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	got, err := s2.ServiceHold(ctx)
	if err != nil || !got.Active || got.Reason != "trying something" || got.Since != h.Since || len(got.Paused) != 2 || !got.Restart {
		t.Fatalf("after reopen: %+v, %v", got, err)
	}
	// Placing it again keeps the paused projects and the start time.
	again, err := s2.HoldService(ctx, "owner2", "second reason", []string{"p3"}, false)
	if err != nil || again.Since != h.Since || len(again.Paused) != 2 || again.Reason != "second reason" {
		t.Fatalf("placing the hold again = %+v, %v", again, err)
	}
	lifted, err := s2.LiftServiceHold(ctx, "owner2")
	if err != nil || !lifted.Active || len(lifted.Paused) != 2 {
		t.Fatalf("lifted = %+v, %v", lifted, err)
	}
	if after, err := s2.ServiceHold(ctx); err != nil || after.Active {
		t.Fatalf("after lifting: %+v, %v", after, err)
	}
	if again, err := s2.LiftServiceHold(ctx, "owner2"); err != nil || again.Active {
		t.Fatalf("lifting a service that is not held: %+v, %v", again, err)
	}
	evs, err := s2.ListEvents(ctx, "service", "hold", 0)
	if err != nil {
		t.Fatal(err)
	}
	var actions []string
	for _, ev := range evs {
		actions = append(actions, ev.Action)
	}
	if len(actions) != 3 || actions[0] != "service.hold" || actions[1] != "service.hold" || actions[2] != "service.unhold" {
		t.Fatalf("events = %v", actions)
	}
}
