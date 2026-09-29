package worker

import (
	"sync"

	"cmd184psu/unified-webapp/internal/taskmaster/golane"
)

// FuncRegistry maps a registered Go-function kind name to its golane.Kind.
// nil-safe: a nil *FuncRegistry has nothing registered, so a Worker that
// never calls EnableFuncTasks never runs a func task.
type FuncRegistry struct {
	mu sync.RWMutex
	m  map[string]golane.Kind
}

// NewFuncRegistry returns an empty FuncRegistry.
func NewFuncRegistry() *FuncRegistry {
	return &FuncRegistry{m: make(map[string]golane.Kind)}
}

// Register adds k, returning golane.ErrKindRegistered if that name is
// already registered.
func (r *FuncRegistry) Register(k golane.Kind) error {
	if r == nil {
		return golane.ErrKindRegistered
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.m[k.Name]; exists {
		return golane.ErrKindRegistered
	}
	r.m[k.Name] = k
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
