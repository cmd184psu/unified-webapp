package smbedit

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// stateFileName is the state file kept under the module's data_dir. It plays
// the role ~/.smbed.json played in standalone smbed, minus the listen_addr
// field (the unified server owns listening).
const stateFileName = "state.json"

// Share represents a single [share] stanza in smb.conf.
type Share struct {
	// Name is the share label, e.g. "media".
	Name string `json:"name"`
	// Path is the filesystem path, e.g. "/opt/media".
	Path string `json:"path"`
	// Comment is the optional comment line.
	Comment string `json:"comment,omitempty"`
	// Writable controls the "writable" flag (default true).
	Writable bool `json:"writable"`
	// Public controls "guest ok" (default false).
	Public bool `json:"public"`
	// BrowseAble controls "browseable" (default true).
	BrowseAble bool `json:"browseable"`
	// Enabled allows soft-disabling a share without deletion.
	Enabled bool `json:"enabled"`
}

// GlobalEntry is one [global] section key/value pair. Stored as an ordered
// slice so round-trip rendering preserves order.
type GlobalEntry struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// State is the root state struct persisted to <data_dir>/state.json.
type State struct {
	// SmbConfPath is the destination path for the rendered smb.conf.
	SmbConfPath string `json:"smb_conf_path"`
	// SambaLogPath is the smbd daemon log file tailed by the Logs page.
	SambaLogPath string `json:"samba_log_path"`
	// ShareOwner is the Linux user that owns all shares.
	ShareOwner string `json:"share_owner"`
	// Globals holds [global] section lines in order.
	Globals []GlobalEntry `json:"globals"`
	// Shares is the editable share list.
	Shares []Share `json:"shares"`
	// Theme is one of "dark", "light", "system".
	Theme string `json:"theme"`
}

// clone returns a deep copy of s. Share and GlobalEntry are value types with
// no reference fields, so copying the slices is a full deep copy.
func (s *State) clone() *State {
	out := *s
	out.Globals = append([]GlobalEntry(nil), s.Globals...)
	out.Shares = append([]Share(nil), s.Shares...)
	return &out
}

// defaultState returns a State populated with sensible defaults.
func defaultState() *State {
	return &State{
		SmbConfPath:  "/etc/samba/smb.conf",
		SambaLogPath: "/var/log/samba/log.smbd",
		ShareOwner:   "nobody",
		Theme:        "dark",
		Globals: []GlobalEntry{
			{Key: "workgroup", Value: "WORKGROUP"},
			{Key: "server string", Value: "Samba Server %v"},
			{Key: "netbios name", Value: "SMBSERVER"},
			{Key: "security", Value: "user"},
			{Key: "map to guest", Value: "bad user"},
			{Key: "dns proxy", Value: "no"},
			{Key: "log file", Value: "/var/log/samba/log.%m"},
			{Key: "max log size", Value: "1000"},
			{Key: "logging", Value: "file"},
			{Key: "panic action", Value: "/usr/share/samba/panic-action %d"},
			{Key: "server role", Value: "standalone server"},
			{Key: "obey pam restrictions", Value: "yes"},
			{Key: "unix password sync", Value: "yes"},
			{Key: "passwd program", Value: "/usr/bin/passwd %u"},
			{Key: "passwd chat", Value: "*Enter\\snew\\s*\\spassword:* %n\\n *Retype\\snew\\s*\\spassword:* %n\\n *password\\supdated\\ssuccessfully* ."},
			{Key: "pam password change", Value: "yes"},
			{Key: "usershare allow guests", Value: "yes"},
		},
		Shares: []Share{},
	}
}

// DisableMissingPaths clears Enabled on any share whose Path no longer
// exists or is not a directory, so a stale reference never reaches
// smb.conf. It mutates shares in place and reports whether anything changed.
func DisableMissingPaths(shares []Share) bool {
	changed := false
	for i := range shares {
		if !shares[i].Enabled {
			continue
		}
		info, err := os.Stat(shares[i].Path)
		if err != nil || !info.IsDir() {
			shares[i].Enabled = false
			changed = true
		}
	}
	return changed
}

// store owns the persisted State. All access goes through it: readers get a
// deep copy, writers mutate a deep copy that only replaces the in-memory
// state after the save succeeds (so a failed save rolls back, D-8). The
// mutex is taken exactly once per operation.
type store struct {
	mu      sync.Mutex
	dataDir string
	path    string
	state   *State
}

// newStore loads <dataDir>/state.json. A missing file writes and returns
// defaults; a malformed file is an error (never silently overwritten). Any
// stale temp files from interrupted saves are swept first.
func newStore(dataDir string) (*store, error) {
	s := &store{
		dataDir: dataDir,
		path:    filepath.Join(dataDir, stateFileName),
	}

	// Sweep leftovers from a save interrupted between CreateTemp and Rename.
	if stale, err := filepath.Glob(filepath.Join(dataDir, "state-*.json.tmp")); err == nil {
		for _, p := range stale {
			os.Remove(p)
		}
	}

	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		s.state = defaultState()
		if saveErr := s.save(s.state); saveErr != nil {
			// Non-fatal: continue with defaults even if we can't persist yet.
			fmt.Fprintf(os.Stderr, "smbedit: warning: could not write default state: %v\n", saveErr)
		}
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", s.path, err)
	}

	st := defaultState() // start from defaults so new fields get zero values
	if err := json.Unmarshal(data, st); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", s.path, err)
	}
	DisableMissingPaths(st.Shares)
	s.state = st
	return s, nil
}

// snapshot returns a deep copy of the current state.
func (s *store) snapshot() *State {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state.clone()
}

// update applies mutate to a copy of the current state and persists it. The
// in-memory state is replaced only after the save succeeds; on any error the
// previous state stays in force (D-8).
func (s *store) update(mutate func(*State)) (*State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	next := s.state.clone()
	mutate(next)
	if err := s.save(next); err != nil {
		return nil, err
	}
	s.state = next
	return next.clone(), nil
}

// save atomically persists st to s.path: temp file in the same directory,
// write, fsync, rename over the target, then fsync the parent directory so
// the rename itself is durable. The temp file is removed on every error
// path. Callers must hold s.mu (or be the sole owner, as in newStore).
func (s *store) save(st *State) error {
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling state: %w", err)
	}

	tmp, err := os.CreateTemp(s.dataDir, "state-*.json.tmp")
	if err != nil {
		return fmt.Errorf("creating temp state file: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := func() {
		tmp.Close()
		os.Remove(tmpPath)
	}

	// CreateTemp already creates 0600; keep an explicit Chmod as
	// belt-and-braces against a permissive umask-driven future change.
	if err := tmp.Chmod(0o600); err != nil {
		cleanup()
		return fmt.Errorf("chmod temp state file: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		cleanup()
		return fmt.Errorf("writing temp state file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return fmt.Errorf("syncing temp state file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("closing temp state file: %w", err)
	}
	if err := os.Rename(tmpPath, s.path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("renaming state file into place: %w", err)
	}

	// Fsync the parent directory so the rename survives a crash.
	if dir, err := os.Open(s.dataDir); err == nil {
		dir.Sync()
		dir.Close()
	}
	return nil
}
