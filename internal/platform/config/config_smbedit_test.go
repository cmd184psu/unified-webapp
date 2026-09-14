package config_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"cmd184psu/unified-webapp/internal/platform/config"
)

func TestSmbeditDefaults(t *testing.T) {
	s := config.DefaultConfig().Smbedit
	if s.StaticDir != "./web/smbedit" {
		t.Errorf("static_dir = %q", s.StaticDir)
	}
	if s.DataDir != "./data/smbedit" {
		t.Errorf("data_dir = %q", s.DataDir)
	}
	if s.PickerRoot != "/opt" {
		t.Errorf("picker_root = %q", s.PickerRoot)
	}
}

func TestSmbeditLoadFromFile(t *testing.T) {
	path := writeCfg(t, `{"smbedit":{
		"static_dir":"/srv/web/smbedit",
		"data_dir":"/var/lib/smbedit",
		"picker_root":"/srv/shares"
	}}`)

	c, err := config.Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	s := c.Smbedit
	if s.StaticDir != "/srv/web/smbedit" {
		t.Errorf("static_dir = %q", s.StaticDir)
	}
	if s.DataDir != "/var/lib/smbedit" {
		t.Errorf("data_dir = %q", s.DataDir)
	}
	if s.PickerRoot != "/srv/shares" {
		t.Errorf("picker_root = %q", s.PickerRoot)
	}
}

func TestSmbeditTildeExpansion(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot determine home dir")
	}
	path := writeCfg(t, `{"smbedit":{
		"static_dir":"~/web/smbedit",
		"data_dir":"~/data/smbedit",
		"picker_root":"~/shares"
	}}`)

	c, err := config.Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	s := c.Smbedit
	for name, pair := range map[string][2]string{
		"static_dir":  {s.StaticDir, filepath.Join(home, "web/smbedit")},
		"data_dir":    {s.DataDir, filepath.Join(home, "data/smbedit")},
		"picker_root": {s.PickerRoot, filepath.Join(home, "shares")},
	} {
		if pair[0] != pair[1] {
			t.Errorf("%s = %q, want %q", name, pair[0], pair[1])
		}
	}
}

// What `make init-config` writes must load back unchanged, and every smbedit
// key must be present in the generated file (mirrors the multissh check).
func TestSmbeditWrittenDefaultLoadsUnchanged(t *testing.T) {
	path := filepath.Join(t.TempDir(), "unified-webapp.json")
	if err := config.WriteDefault(path); err != nil {
		t.Fatalf("WriteDefault: %v", err)
	}

	loaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	var onDisk config.Config
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if err := json.Unmarshal(data, &onDisk); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if loaded.Smbedit != onDisk.Smbedit {
		t.Errorf("init-config output did not survive Load:\n on disk %+v\n loaded  %+v", onDisk.Smbedit, loaded.Smbedit)
	}

	var raw struct {
		Smbedit map[string]json.RawMessage `json:"smbedit"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}
	for _, key := range []string{"static_dir", "data_dir", "picker_root"} {
		if _, ok := raw.Smbedit[key]; !ok {
			t.Errorf("init-config output is missing the %q key", key)
		}
	}
}
