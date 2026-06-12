package obsidianoid_test

import (
	"testing"

	"cmd184psu/unified-webapp/internal/obsidianoid"
)

func TestStateStoreDefaults(t *testing.T) {
	dir := t.TempDir()
	s, err := obsidianoid.NewStateStore(dir, 4)
	if err != nil {
		t.Fatalf("NewStateStore: %v", err)
	}
	states := s.States()
	if len(states) != 4 {
		t.Fatalf("expected 4 states, got %d", len(states))
	}
	for i, st := range states {
		if st.Disabled {
			t.Errorf("state[%d] should default to enabled", i)
		}
	}
}

func TestStateStorePersistAndReload(t *testing.T) {
	dir := t.TempDir()
	s, err := obsidianoid.NewStateStore(dir, 4)
	if err != nil {
		t.Fatalf("NewStateStore: %v", err)
	}

	if err := s.SetDisabled([]bool{false, true, false, true}); err != nil {
		t.Fatalf("SetDisabled: %v", err)
	}

	// Reload from same dir.
	s2, err := obsidianoid.NewStateStore(dir, 4)
	if err != nil {
		t.Fatalf("NewStateStore reload: %v", err)
	}
	states := s2.States()
	if states[1].Disabled != true || states[3].Disabled != true {
		t.Errorf("disabled flags not persisted: got %+v", states)
	}
	if states[0].Disabled || states[2].Disabled {
		t.Errorf("enabled flags not preserved: got %+v", states)
	}
}

func TestStateStorePadToThreadCount(t *testing.T) {
	dir := t.TempDir()
	// Create with 2 threads.
	s, _ := obsidianoid.NewStateStore(dir, 2)
	_ = s.SetDisabled([]bool{true, false})

	// Reload with 4 threads — should pad.
	s2, err := obsidianoid.NewStateStore(dir, 4)
	if err != nil {
		t.Fatalf("NewStateStore: %v", err)
	}
	states := s2.States()
	if len(states) != 4 {
		t.Fatalf("expected 4 states after pad, got %d", len(states))
	}
	if !states[0].Disabled {
		t.Error("states[0] should remain disabled after pad")
	}
	if states[2].Disabled || states[3].Disabled {
		t.Error("padded states should default to enabled")
	}
}

func TestStateStoreTruncateToThreadCount(t *testing.T) {
	dir := t.TempDir()
	// Create with 4 threads.
	s, _ := obsidianoid.NewStateStore(dir, 4)
	_ = s.SetDisabled([]bool{true, true, true, true})

	// Reload with 2 threads — should truncate.
	s2, err := obsidianoid.NewStateStore(dir, 2)
	if err != nil {
		t.Fatalf("NewStateStore: %v", err)
	}
	states := s2.States()
	if len(states) != 2 {
		t.Fatalf("expected 2 states after truncate, got %d", len(states))
	}
}

func TestStateStoreMissingFile(t *testing.T) {
	dir := t.TempDir()
	// No state.json — should return all-enabled without error.
	s, err := obsidianoid.NewStateStore(dir, 3)
	if err != nil {
		t.Fatalf("NewStateStore with missing file: %v", err)
	}
	for i, st := range s.States() {
		if st.Disabled {
			t.Errorf("state[%d] should default to enabled when file is missing", i)
		}
	}
}
