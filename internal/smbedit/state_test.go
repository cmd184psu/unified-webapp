package smbedit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"go.uber.org/goleak"
)

// The first six tests are ported from smbed's internal/config/config_test.go,
// adapted to the store API: state lives at <data_dir>/state.json with the
// data dir injected, so tests pass t.TempDir() directly instead of mutating
// HOME. The listen_addr assertions are gone with the field.

func TestLoadDefaults_NoFile(t *testing.T) {
	defer goleak.VerifyNone(t)
	s, err := newStore(t.TempDir())
	if err != nil {
		t.Fatalf("newStore() unexpected error: %v", err)
	}
	st := s.snapshot()
	if st.SmbConfPath != "/etc/samba/smb.conf" {
		t.Errorf("expected default SmbConfPath '/etc/samba/smb.conf', got %q", st.SmbConfPath)
	}
	if st.Theme != "dark" {
		t.Errorf("expected default Theme 'dark', got %q", st.Theme)
	}
	if len(st.Globals) == 0 {
		t.Error("expected non-empty default Globals")
	}
}

func TestSaveAndReload(t *testing.T) {
	defer goleak.VerifyNone(t)
	dir := t.TempDir()

	s, err := newStore(dir)
	if err != nil {
		t.Fatalf("newStore() error: %v", err)
	}
	if _, err := s.update(func(st *State) {
		st.ShareOwner = "testuser"
		st.Shares = []Share{
			{Name: "docs", Path: "/opt/docs", Writable: true, BrowseAble: true, Enabled: true},
		}
	}); err != nil {
		t.Fatalf("update() error: %v", err)
	}

	// Verify file exists and contains expected data.
	data, err := os.ReadFile(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatalf("ReadFile error: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if raw["share_owner"] != "testuser" {
		t.Errorf("expected share_owner 'testuser', got %v", raw["share_owner"])
	}

	// Reload and verify round-trip. The docs share's /opt/docs path may not
	// exist on this machine, so reload legitimately auto-disables it; the
	// identity fields are what round-trip.
	s2, err := newStore(dir)
	if err != nil {
		t.Fatalf("newStore() after save error: %v", err)
	}
	st2 := s2.snapshot()
	if st2.ShareOwner != "testuser" {
		t.Errorf("round-trip ShareOwner: want 'testuser', got %q", st2.ShareOwner)
	}
	if len(st2.Shares) != 1 || st2.Shares[0].Name != "docs" {
		t.Errorf("round-trip Shares mismatch: %+v", st2.Shares)
	}
}

func TestDisableMissingPaths(t *testing.T) {
	defer goleak.VerifyNone(t)
	dir := t.TempDir()
	shares := []Share{
		{Name: "present", Path: dir, Enabled: true},
		{Name: "missing", Path: filepath.Join(dir, "does-not-exist"), Enabled: true},
		{Name: "already-off", Path: filepath.Join(dir, "also-missing"), Enabled: false},
	}

	changed := DisableMissingPaths(shares)
	if !changed {
		t.Error("expected DisableMissingPaths to report a change")
	}
	if !shares[0].Enabled {
		t.Error("share with existing directory should remain enabled")
	}
	if shares[1].Enabled {
		t.Error("share with missing directory should be disabled")
	}
	if shares[2].Enabled {
		t.Error("already-disabled share should stay disabled")
	}
}

func TestDisableMissingPaths_NoChange(t *testing.T) {
	defer goleak.VerifyNone(t)
	dir := t.TempDir()
	shares := []Share{{Name: "present", Path: dir, Enabled: true}}
	if DisableMissingPaths(shares) {
		t.Error("expected no change when all enabled shares' paths exist")
	}
}

func TestLoad_DisablesMissingSharePathOnReload(t *testing.T) {
	defer goleak.VerifyNone(t)
	dir := t.TempDir()

	s, err := newStore(dir)
	if err != nil {
		t.Fatalf("newStore() error: %v", err)
	}
	if _, err := s.update(func(st *State) {
		st.Shares = []Share{
			{Name: "gone", Path: filepath.Join(dir, "nope"), Enabled: true},
		}
	}); err != nil {
		t.Fatalf("update() error: %v", err)
	}

	s2, err := newStore(dir)
	if err != nil {
		t.Fatalf("newStore() error: %v", err)
	}
	if s2.snapshot().Shares[0].Enabled {
		t.Error("expected share with missing path to be disabled on reload")
	}
}

func TestLoadCorrupt(t *testing.T) {
	defer goleak.VerifyNone(t)
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "state.json"), []byte("{bad json"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := newStore(dir); err == nil {
		t.Error("expected error loading corrupt state, got nil")
	}
}

// The tests below are new with the port: they pin the atomic-save mechanism,
// the stale-temp sweep, the D-8 rollback, and snapshot isolation.

func TestSave_AtomicMode0600_NoTempLeftover(t *testing.T) {
	defer goleak.VerifyNone(t)
	dir := t.TempDir()

	s, err := newStore(dir)
	if err != nil {
		t.Fatalf("newStore() error: %v", err)
	}
	if _, err := s.update(func(st *State) { st.Theme = "light" }); err != nil {
		t.Fatalf("update() error: %v", err)
	}

	info, err := os.Stat(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatalf("stat state.json: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("state.json mode = %o, want 0600", got)
	}
	leftovers, err := filepath.Glob(filepath.Join(dir, "state-*.json.tmp"))
	if err != nil {
		t.Fatalf("Glob: %v", err)
	}
	if len(leftovers) != 0 {
		t.Errorf("expected no temp files after save, got %v", leftovers)
	}
}

func TestLoad_SweepsStaleTemps(t *testing.T) {
	defer goleak.VerifyNone(t)
	dir := t.TempDir()
	stale := filepath.Join(dir, "state-12345.json.tmp")
	if err := os.WriteFile(stale, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := newStore(dir); err != nil {
		t.Fatalf("newStore() error: %v", err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("expected stale temp %s to be swept on load, stat err = %v", stale, err)
	}
}

func TestUpdate_RollbackOnSaveFailure(t *testing.T) {
	defer goleak.VerifyNone(t)
	if os.Geteuid() == 0 {
		t.Skip("running as root bypasses permission checks")
	}
	dir := t.TempDir()

	s, err := newStore(dir)
	if err != nil {
		t.Fatalf("newStore() error: %v", err)
	}

	// Make the data dir unwritable so CreateTemp — the first step of the
	// atomic save — fails.
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	defer os.Chmod(dir, 0o755) // let t.TempDir() clean up

	if _, err := s.update(func(st *State) { st.ShareOwner = "intruder" }); err == nil {
		t.Fatal("expected update to fail when the save cannot be written")
	}
	if got := s.snapshot().ShareOwner; got != "nobody" {
		t.Errorf("in-memory state mutated despite failed save: ShareOwner = %q, want %q (D-8 rollback)", got, "nobody")
	}
}

func TestSnapshot_IsDeepCopy(t *testing.T) {
	defer goleak.VerifyNone(t)
	s, err := newStore(t.TempDir())
	if err != nil {
		t.Fatalf("newStore() error: %v", err)
	}

	snap := s.snapshot()
	if len(snap.Globals) == 0 {
		t.Fatal("expected default globals")
	}
	snap.Globals[0].Value = "TAMPERED"
	snap.ShareOwner = "tamperer"

	fresh := s.snapshot()
	if fresh.Globals[0].Value == "TAMPERED" {
		t.Error("mutating a snapshot's Globals leaked into the store")
	}
	if fresh.ShareOwner == "tamperer" {
		t.Error("mutating a snapshot's scalar fields leaked into the store")
	}
}
