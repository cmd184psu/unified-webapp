package haproxy

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

const modelStateFile = "state.json"

// ModelStore owns the persisted Model under dataDir/state.json. Readers get a
// deep copy; writers mutate a copy that replaces the in-memory model only
// after the atomic save succeeds.
type ModelStore struct {
	mu   sync.Mutex
	dir  string
	path string
	m    *Model
}

// NewModelStore loads dataDir/state.json. A missing file yields DefaultModel
// (not written until the first Update); a malformed file is an error.
func NewModelStore(dataDir string) (*ModelStore, error) {
	s := &ModelStore{dir: dataDir, path: filepath.Join(dataDir, modelStateFile)}
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		s.m = DefaultModel()
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", s.path, err)
	}
	m := DefaultModel()
	if err := json.Unmarshal(data, m); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", s.path, err)
	}
	if m.Version == 0 {
		m.Version = modelVersion
	}
	s.m = m
	return s, nil
}

// Snapshot returns a deep copy of the current model.
func (s *ModelStore) Snapshot() *Model {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.m.clone()
}

// Update applies mutate to a copy and persists it; on error the previous
// model stays in force.
func (s *ModelStore) Update(mutate func(*Model)) (*Model, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.m.clone()
	mutate(next)
	next.Version = modelVersion
	if err := s.save(next); err != nil {
		return nil, err
	}
	s.m = next
	return next.clone(), nil
}

// save writes atomically: temp file (0600) in the same dir, fsync, rename,
// fsync the directory.
func (s *ModelStore) save(m *Model) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling state: %w", err)
	}
	tmp, err := os.CreateTemp(s.dir, "state-*.json.tmp")
	if err != nil {
		return fmt.Errorf("creating temp state file: %w", err)
	}
	name := tmp.Name()
	fail := func(err error) error {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		return fail(fmt.Errorf("chmod temp state file: %w", err))
	}
	if _, err := tmp.Write(data); err != nil {
		return fail(fmt.Errorf("writing temp state file: %w", err))
	}
	if err := tmp.Sync(); err != nil {
		return fail(fmt.Errorf("syncing temp state file: %w", err))
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return fmt.Errorf("closing temp state file: %w", err)
	}
	if err := os.Rename(name, s.path); err != nil {
		os.Remove(name)
		return fmt.Errorf("renaming state file into place: %w", err)
	}
	if d, err := os.Open(s.dir); err == nil {
		d.Sync()
		d.Close()
	}
	return nil
}
