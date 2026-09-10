package config_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"cmd184psu/unified-webapp/internal/platform/config"
)

// writeCfg writes body to a temp config file and returns its path.
func writeCfg(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "unified.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func TestMultisshDefaults(t *testing.T) {
	c := config.DefaultConfig()
	m := c.Multissh
	if m.StaticDir != "./web/multissh" {
		t.Errorf("static_dir = %q", m.StaticDir)
	}
	if m.HostsPath != "./data/multissh/multissh-hosts.json" {
		t.Errorf("hosts_path = %q", m.HostsPath)
	}
	if m.MaxSessions != config.DefaultMaxSessions {
		t.Errorf("max_sessions = %d, want %d", m.MaxSessions, config.DefaultMaxSessions)
	}
	if m.MaxUploadBytes != 8<<30 {
		t.Errorf("max_upload_bytes = %d, want %d", m.MaxUploadBytes, int64(8<<30))
	}
	if m.StrictHostKey {
		t.Error("strict_host_key should default to false")
	}
	// The Build-time-resolved fields stay empty in the defaults so the written
	// config file is portable across machines (task 1.2/1.6).
	for name, got := range map[string]string{
		"ssh_dir":          m.SSHDir,
		"upload_dir":       m.UploadDir,
		"browse_root":      m.BrowseRoot,
		"known_hosts_path": m.KnownHostsPath,
	} {
		if got != "" {
			t.Errorf("%s = %q, want empty", name, got)
		}
	}
}

func TestMultisshLoadFromFile(t *testing.T) {
	path := writeCfg(t, `{"multissh":{
		"static_dir":"/srv/web/multissh",
		"ssh_dir":"/home/op/.ssh",
		"upload_dir":"/var/tmp/mssh",
		"hosts_path":"/var/lib/multissh-hosts.json",
		"browse_root":"/srv/browse",
		"max_sessions":5,
		"max_upload_bytes":1024,
		"strict_host_key":true,
		"known_hosts_path":"/etc/known_hosts"
	}}`)

	c, err := config.Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	m := c.Multissh
	if m.StaticDir != "/srv/web/multissh" {
		t.Errorf("static_dir = %q", m.StaticDir)
	}
	if m.SSHDir != "/home/op/.ssh" {
		t.Errorf("ssh_dir = %q", m.SSHDir)
	}
	if m.UploadDir != "/var/tmp/mssh" {
		t.Errorf("upload_dir = %q", m.UploadDir)
	}
	if m.HostsPath != "/var/lib/multissh-hosts.json" {
		t.Errorf("hosts_path = %q", m.HostsPath)
	}
	if m.BrowseRoot != "/srv/browse" {
		t.Errorf("browse_root = %q", m.BrowseRoot)
	}
	if m.MaxSessions != 5 {
		t.Errorf("max_sessions = %d, want 5", m.MaxSessions)
	}
	if m.MaxUploadBytes != 1024 {
		t.Errorf("max_upload_bytes = %d", m.MaxUploadBytes)
	}
	if !m.StrictHostKey {
		t.Error("strict_host_key should be true from file")
	}
	if m.KnownHostsPath != "/etc/known_hosts" {
		t.Errorf("known_hosts_path = %q", m.KnownHostsPath)
	}
}

func TestMultisshRoundTrip(t *testing.T) {
	want := config.DefaultConfig().Multissh
	data, err := json.Marshal(map[string]any{"multissh": want})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	path := writeCfg(t, string(data))

	c, err := config.Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if c.Multissh != want {
		t.Errorf("round trip changed the struct:\n got %+v\nwant %+v", c.Multissh, want)
	}
}

func TestMultisshTildeExpansion(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot determine home dir")
	}
	path := writeCfg(t, `{"multissh":{
		"static_dir":"~/web",
		"ssh_dir":"~/.ssh",
		"upload_dir":"~/uploads",
		"hosts_path":"~/hosts.json",
		"browse_root":"~/browse",
		"known_hosts_path":"~/.ssh/known_hosts"
	}}`)

	c, err := config.Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	m := c.Multissh
	for name, pair := range map[string][2]string{
		"static_dir":       {m.StaticDir, filepath.Join(home, "web")},
		"ssh_dir":          {m.SSHDir, filepath.Join(home, ".ssh")},
		"upload_dir":       {m.UploadDir, filepath.Join(home, "uploads")},
		"hosts_path":       {m.HostsPath, filepath.Join(home, "hosts.json")},
		"browse_root":      {m.BrowseRoot, filepath.Join(home, "browse")},
		"known_hosts_path": {m.KnownHostsPath, filepath.Join(home, ".ssh/known_hosts")},
	} {
		if pair[0] != pair[1] {
			t.Errorf("%s = %q, want %q", name, pair[0], pair[1])
		}
	}
}

// Load is the single validation point for max_sessions (task 1.2b).
func TestMultisshMaxSessionsValidation(t *testing.T) {
	cases := []struct {
		name    string
		value   string
		want    int
		wantErr bool
	}{
		{"unset is the default", `{"multissh":{}}`, config.DefaultMaxSessions, false},
		{"zero means unset", `{"multissh":{"max_sessions":0}}`, config.DefaultMaxSessions, false},
		{"one is the floor", `{"multissh":{"max_sessions":1}}`, 1, false},
		{"negative is rejected", `{"multissh":{"max_sessions":-1}}`, 0, true},
		{"at the ceiling", `{"multissh":{"max_sessions":16}}`, config.MaxMaxSessions, false},
		{"above the ceiling clamps", `{"multissh":{"max_sessions":64}}`, config.MaxMaxSessions, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, err := config.Load(writeCfg(t, tc.value))
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected an error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("load: %v", err)
			}
			if c.Multissh.MaxSessions != tc.want {
				t.Errorf("max_sessions = %d, want %d", c.Multissh.MaxSessions, tc.want)
			}
		})
	}
}

// What `make init-config` writes must load back unchanged. The two halves --
// WriteDefault and Load -- can drift independently: Load applies tilde
// expansion and max_sessions validation, so a default that needed either would
// mean the generated file is not the file the server actually runs on.
func TestWrittenDefaultConfigLoadsUnchanged(t *testing.T) {
	path := filepath.Join(t.TempDir(), "unified-webapp.json")
	if err := config.WriteDefault(path); err != nil {
		t.Fatalf("WriteDefault: %v", err)
	}

	loaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Compare against the file's own bytes rather than DefaultConfig(), so a
	// field that WriteDefault omits is caught rather than silently defaulted.
	var onDisk config.Config
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if err := json.Unmarshal(data, &onDisk); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if loaded.Multissh != onDisk.Multissh {
		t.Errorf("init-config output did not survive Load:\n on disk %+v\n loaded  %+v", onDisk.Multissh, loaded.Multissh)
	}

	// Every multissh key must be present in the generated file. An absent key
	// is indistinguishable from a typo'd one when an operator edits it.
	var raw struct {
		Multissh map[string]json.RawMessage `json:"multissh"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}
	for _, key := range []string{
		"static_dir", "ssh_dir", "upload_dir", "hosts_path", "browse_root",
		"max_sessions", "max_upload_bytes", "strict_host_key", "known_hosts_path",
	} {
		if _, ok := raw.Multissh[key]; !ok {
			t.Errorf("init-config output is missing the %q key", key)
		}
	}
}
