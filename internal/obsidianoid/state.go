package obsidianoid

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// ThreadState holds a thread slot's app-side settings: its disabled flag and
// its display title. Neither is stored in the vault, so the thread's file
// (Thread01.md …) keeps its name whatever the title says.
type ThreadState struct {
	Disabled bool   `json:"disabled"`
	Title    string `json:"title,omitempty"`
}

// maxThreadTitleLen caps a thread's display title, in runes.
const maxThreadTitleLen = 80

type stateFile struct {
	ThreadStates []ThreadState `json:"thread_states"`
	// ThreadCount, when set, overrides the config's thread_count; it is set
	// from the Settings menu. 0 means "use the configured count".
	ThreadCount int `json:"thread_count,omitempty"`
}

// MaxThreadCount bounds how many thread slots the Settings menu can ask for.
const MaxThreadCount = 24

var errBadThreadCount = errors.New("thread count out of range")

// StateStore persists thread disabled-flags, titles and the thread count in
// {DataDir}/state.json. Missing or corrupt file → all-enabled, sized to the
// configured count.
//
// Slots beyond the current count are kept (not truncated) so lowering the
// count and raising it again brings their titles back; States() returns only
// the live slots.
type StateStore struct {
	path     string
	mu       sync.Mutex
	count    int // live slot count
	override int // count chosen in Settings; 0 = configured default
	states   []ThreadState
}

// NewStateStore initialises the store, creating DataDir if needed.
// defaultCount is the configured thread_count, used unless Settings set one.
func NewStateStore(dataDir string, defaultCount int) (*StateStore, error) {
	if err := os.MkdirAll(dataDir, 0o750); err != nil {
		return nil, err
	}
	s := &StateStore{path: filepath.Join(dataDir, "state.json"), count: defaultCount}
	data, err := os.ReadFile(s.path)
	if err == nil {
		var sf stateFile
		if json.Unmarshal(data, &sf) == nil {
			s.states = sf.ThreadStates
			if sf.ThreadCount > 0 && sf.ThreadCount <= MaxThreadCount {
				s.override = sf.ThreadCount
				s.count = sf.ThreadCount
			}
		}
	}
	s.pad()
	return s, nil
}

// pad grows states to cover every live slot. Caller holds mu (or owns s).
func (s *StateStore) pad() {
	for len(s.states) < s.count {
		s.states = append(s.states, ThreadState{})
	}
}

// DataDir returns the directory in which state.json lives. Exported for tests.
func (s *StateStore) DataDir() string { return filepath.Dir(s.path) }

// Count returns the number of live thread slots.
func (s *StateStore) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.count
}

// States returns a copy of the live slots' states.
func (s *StateStore) States() []ThreadState {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]ThreadState, s.count)
	copy(out, s.states[:s.count])
	return out
}

// SetCount changes the number of live thread slots (1..MaxThreadCount) and
// saves it, overriding the configured count from now on.
func (s *StateStore) SetCount(n int) error {
	if n < 1 || n > MaxThreadCount {
		return errBadThreadCount
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.count = n
	s.override = n
	s.pad()
	return s.saveLocked()
}

// ResetThread returns slot i to a fresh thread: enabled, no title. Used when
// the slot's file is renamed or deleted out of the thread group.
func (s *StateStore) ResetThread(i int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if i < 0 || i >= s.count {
		return nil
	}
	s.states[i] = ThreadState{}
	return s.saveLocked()
}

// SetDisabled updates disabled flags from a slice of bool values and saves to disk.
func (s *StateStore) SetDisabled(disabled []bool) error {
	return s.SetThreads(disabled, nil)
}

// SetThreads updates each live slot's disabled flag and, when titles is
// non-nil, its title (trimmed, capped at maxThreadTitleLen runes), then saves.
func (s *StateStore) SetThreads(disabled []bool, titles []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, d := range disabled {
		if i < s.count {
			s.states[i].Disabled = d
		}
	}
	for i, t := range titles {
		if i < s.count {
			t = strings.TrimSpace(t)
			if r := []rune(t); len(r) > maxThreadTitleLen {
				t = string(r[:maxThreadTitleLen])
			}
			s.states[i].Title = t
		}
	}
	return s.saveLocked()
}

// saveLocked writes state.json. Caller holds mu.
func (s *StateStore) saveLocked() error {
	data, err := json.Marshal(stateFile{ThreadStates: s.states, ThreadCount: s.override})
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0o600)
}
