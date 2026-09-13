package worker

import "sync/atomic"

// SudoGate is a runtime-mutable flag controlling whether tasks may run with
// sudo. It is shared by pointer between the coordinator (create/update
// gating and the capabilities endpoint) and the executor (defense-in-depth
// refusal at exec time), so a live toggle takes effect in both places at
// once. The zero value denies sudo; use NewSudoGate.
type SudoGate struct {
	allowed atomic.Bool
}

// NewSudoGate returns a gate seeded with the given initial value.
func NewSudoGate(allow bool) *SudoGate {
	g := &SudoGate{}
	g.allowed.Store(allow)
	return g
}

// Allowed reports whether sudo is currently permitted. A nil gate denies.
func (g *SudoGate) Allowed() bool {
	return g != nil && g.allowed.Load()
}

// Set updates the gate; safe to call concurrently with Allowed.
func (g *SudoGate) Set(allow bool) {
	if g != nil {
		g.allowed.Store(allow)
	}
}
