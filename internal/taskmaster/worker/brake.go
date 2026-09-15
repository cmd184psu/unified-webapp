package worker

import "sync/atomic"

// BrakeGate is a runtime-mutable flag for the durable "hand brake": while
// engaged, the worker's poll loop launches nothing. It is shared by pointer
// between the coordinator (the /api/brake endpoints) and the worker (the
// poll-time check), mirroring SudoGate's shared-pointer pattern so a live
// toggle takes effect in both places at once. The zero value is disengaged;
// use NewBrakeGate.
type BrakeGate struct {
	engaged atomic.Bool
}

// NewBrakeGate returns a gate seeded with the given initial value.
func NewBrakeGate(engaged bool) *BrakeGate {
	g := &BrakeGate{}
	g.engaged.Store(engaged)
	return g
}

// Engaged reports whether the hand brake is currently engaged. A nil gate
// reports disengaged.
func (g *BrakeGate) Engaged() bool {
	return g != nil && g.engaged.Load()
}

// Set updates the gate; safe to call concurrently with Engaged.
func (g *BrakeGate) Set(engaged bool) {
	if g != nil {
		g.engaged.Store(engaged)
	}
}
