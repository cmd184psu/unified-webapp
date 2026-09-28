package worker

import (
	"sort"
	"sync"
)

// HiddenLanes is the shared, mutable set of module-owned lane names
// currently hidden from every TM HTTP route and board event (N4). nil-safe:
// a nil *HiddenLanes hides nothing, so ordinary (non-owned) taskmaster
// deployments pay nothing for this feature.
type HiddenLanes struct {
	mu  sync.RWMutex
	set map[string]bool
}

// NewHiddenLanes seeds a HiddenLanes set from the given lane names (read
// back at boot via db.ListHiddenLanes).
func NewHiddenLanes(names []string) *HiddenLanes {
	h := &HiddenLanes{set: make(map[string]bool)}
	for _, n := range names {
		h.set[n] = true
	}
	return h
}

// Hidden reports whether lane is currently hidden.
func (h *HiddenLanes) Hidden(lane string) bool {
	if h == nil || lane == "" {
		return false
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.set[lane]
}

// Set updates lane's hidden state.
func (h *HiddenLanes) Set(lane string, hidden bool) {
	if h == nil || lane == "" {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if hidden {
		h.set[lane] = true
	} else {
		delete(h.set, lane)
	}
}

// Names returns every currently hidden lane name, sorted.
func (h *HiddenLanes) Names() []string {
	if h == nil {
		return nil
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	names := make([]string, 0, len(h.set))
	for n := range h.set {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
