package worker

import (
	"context"
	"sync"
)

// CancelRegistry maps a running execution's ID to the context.CancelFunc
// that stops it, plus a set recording which execution IDs were explicitly
// canceled via Cancel. runTask consults the set after Execute returns to
// distinguish an explicit per-execution (or hand-brake) cancel — recorded as
// status "canceled" — from the whole worker shutting down via w.runCtx,
// which is not routed through Cancel and must still record "failed"
// (preserves existing shutdown semantics). Instance-scoped: one is created
// per Worker/Coordinator pair in build.go.
type CancelRegistry struct {
	mu       sync.Mutex
	cancels  map[int64]context.CancelFunc
	canceled map[int64]bool
}

// NewCancelRegistry returns an empty CancelRegistry.
func NewCancelRegistry() *CancelRegistry {
	return &CancelRegistry{
		cancels:  make(map[int64]context.CancelFunc),
		canceled: make(map[int64]bool),
	}
}

// Register records execID's cancel func so it can later be canceled by ID.
func (r *CancelRegistry) Register(execID int64, cancel context.CancelFunc) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cancels[execID] = cancel
}

// Cancel explicitly cancels execID's context and records execID in the
// canceled set. Returns whether execID was registered (i.e. currently
// running) — callers use this to answer 200 vs 404.
func (r *CancelRegistry) Cancel(execID int64) bool {
	if r == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	cancel, ok := r.cancels[execID]
	if !ok {
		return false
	}
	r.canceled[execID] = true
	cancel()
	return true
}

// WasCanceled reports whether execID was explicitly canceled via Cancel.
func (r *CancelRegistry) WasCanceled(execID int64) bool {
	if r == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.canceled[execID]
}

// Unregister clears execID from both the cancel-func map and the canceled
// set. Call once the execution has finished.
func (r *CancelRegistry) Unregister(execID int64) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.cancels, execID)
	delete(r.canceled, execID)
}
