package engine

import (
	"context"
	"errors"
	"strings"
	"testing"

	"cmd184psu/unified-webapp/internal/agentflow/store"
)

// AF-10: while the service is held the dispatcher starts no new stage in any
// project and a run/start is refused with the reason; lifting the hold lets
// both work again.
func TestAF10_HoldStopsDispatchAndRunStart(t *testing.T) {
	h := newDispHarness(t)
	ctx := context.Background()
	h.standard(1)
	h.dispProject("alpha", "any", indep(2))
	h.startRun("alpha")
	// Without a hold the dispatcher places the first task, so this is not a
	// test of a project that could not start anyway.
	h.pass()
	open := h.openStages()
	if len(open) != 1 {
		t.Fatalf("dispatched %d stages before the hold, want 1", len(open))
	}
	if _, err := h.st.HoldService(ctx, "owner", "trying something", nil, false); err != nil {
		t.Fatal(err)
	}
	// The stage that was running finishes; nothing new takes its slot.
	h.finish(open[0])
	h.pass()
	if n := len(h.openStages()); n != 0 {
		t.Fatalf("dispatched %d stages while the service was held", n)
	}
	if err := h.eng.Runs().Start(ctx, "t", "alpha"); !errors.Is(err, store.ErrConflict) || !strings.Contains(err.Error(), "trying something") {
		t.Fatalf("start while held = %v, want a conflict naming the reason", err)
	}
	if err := h.eng.Runs().RunOne(ctx, "t", "alpha"); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("run-one while held = %v, want a conflict", err)
	}
	if _, err := h.st.LiftServiceHold(ctx, "owner"); err != nil {
		t.Fatal(err)
	}
	h.pass()
	if n := len(h.openStages()); n != 1 {
		t.Fatalf("after lifting the hold, dispatched %d stages, want 1", n)
	}
}
