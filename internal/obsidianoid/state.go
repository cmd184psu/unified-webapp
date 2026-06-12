package obsidianoid

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// ThreadState holds the disabled flag for a single thread slot.
type ThreadState struct {
	Disabled bool `json:"disabled"`
}

type stateFile struct {
	ThreadStates []ThreadState `json:"thread_states"`
}

// StateStore persists thread disabled-flags in {DataDir}/state.json.
// Missing or corrupt file → all-enabled, sized to threadCount.
type StateStore struct {
	path        string
	threadCount int
	mu          sync.Mutex
	states      []ThreadState
}

// NewStateStore initialises the store, creating DataDir if needed.
func NewStateStore(dataDir string, threadCount int) (*StateStore, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, err
	}
	s := &StateStore{
		path:        filepath.Join(dataDir, "state.json"),
		threadCount: threadCount,
		states:      make([]ThreadState, threadCount),
	}
	data, err := os.ReadFile(s.path)
	if err == nil {
		var sf stateFile
		if json.Unmarshal(data, &sf) == nil {
			s.states = sf.ThreadStates
		}
	}
	// Pad or truncate to match threadCount.
	for len(s.states) < threadCount {
		s.states = append(s.states, ThreadState{})
	}
	s.states = s.states[:threadCount]
	return s, nil
}

// DataDir returns the directory in which state.json lives. Exported for tests.
func (s *StateStore) DataDir() string { return filepath.Dir(s.path) }

// States returns a copy of the current thread states.
func (s *StateStore) States() []ThreadState {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]ThreadState, len(s.states))
	copy(out, s.states)
	return out
}

// SetDisabled updates disabled flags from a slice of bool values and saves to disk.
func (s *StateStore) SetDisabled(disabled []bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, d := range disabled {
		if i < len(s.states) {
			s.states[i].Disabled = d
		}
	}
	sf := stateFile{ThreadStates: s.states}
	data, err := json.Marshal(sf)
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0o644)
}
