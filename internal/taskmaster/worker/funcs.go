package worker

import (
	"fmt"
	"sync"

	"cmd184psu/unified-webapp/internal/taskmaster/golane"
)

// FuncRegistry maps a registered Go-function kind name to its golane.Kind.
// nil-safe: a nil *FuncRegistry has nothing registered, so a Worker that
// never calls EnableFuncTasks never runs a func task.
type FuncRegistry struct {
	mu     sync.RWMutex
	m      map[string]golane.Kind
	owners map[string]string // kind name -> owner
}

// NewFuncRegistry returns an empty FuncRegistry.
func NewFuncRegistry() *FuncRegistry {
	return &FuncRegistry{m: make(map[string]golane.Kind), owners: make(map[string]string)}
}

// Check reports whether Register(owner, k) would succeed, without changing
// anything, so a caller can validate before committing other state.
func (r *FuncRegistry) Check(owner string, k golane.Kind) error {
	if r == nil {
		return golane.ErrKindRegistered
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.checkLocked(owner, k)
}

func (r *FuncRegistry) checkLocked(owner string, k golane.Kind) error {
	prev, exists := r.m[k.Name]
	if !exists {
		return nil
	}
	if r.owners[k.Name] != owner {
		return fmt.Errorf("%w: kind %q is owned by %q", golane.ErrKindRegistered, k.Name, r.owners[k.Name])
	}
	if prev.Version != k.Version {
		return fmt.Errorf("%w: kind %q already registered by %q at version %d, not %d",
			golane.ErrKindRegistered, k.Name, owner, prev.Version, k.Version)
	}
	return nil
}

// Register adds k on behalf of owner. A name already registered by the same
// owner is shared: the first Kind (and so its Run) is kept and k is ignored,
// unless k.Version differs, which is an error. A name registered by a
// different owner fails with golane.ErrKindRegistered.
//
// Callers must serialise Check/Register pairs (only Engine.RegisterLane calls
// them, under its lanesMu). Same-owner shared kinds must be identical: a lane
// handle decodes with its own Kind while the worker runs the registry's first.
func (r *FuncRegistry) Register(owner string, k golane.Kind) error {
	if r == nil {
		return golane.ErrKindRegistered
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.checkLocked(owner, k); err != nil {
		return err
	}
	if _, exists := r.m[k.Name]; exists {
		return nil
	}
	r.m[k.Name] = k
	r.owners[k.Name] = owner
	return nil
}

// Get returns the registered Kind for name, if any.
func (r *FuncRegistry) Get(name string) (golane.Kind, bool) {
	if r == nil {
		return golane.Kind{}, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	k, ok := r.m[name]
	return k, ok
}

// Has reports whether name is registered.
func (r *FuncRegistry) Has(name string) bool {
	_, ok := r.Get(name)
	return ok
}
