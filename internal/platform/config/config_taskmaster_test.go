package config_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"cmd184psu/unified-webapp/internal/platform/config"
)

func TestTaskmasterDefaults(t *testing.T) {
	c := config.DefaultConfig()
	tm := c.Taskmaster
	if tm.StaticDir != "./web/taskmaster" {
		t.Errorf("static_dir = %q", tm.StaticDir)
	}
	if tm.DBPath != "./data/taskmaster/taskmaster.db" {
		t.Errorf("db_path = %q", tm.DBPath)
	}
	if tm.AllowSudo {
		t.Error("allow_sudo should default to false")
	}
	if len(tm.Groups) != 0 {
		t.Errorf("groups = %v, want empty", tm.Groups)
	}
}

func TestTaskmasterLoadFromFile(t *testing.T) {
	path := writeCfg(t, `{"taskmaster":{
		"static_dir":"/srv/web/taskmaster",
		"db_path":"/var/lib/taskmaster/taskmaster.db",
		"groups":[{"name":"default","pool_limit":2,"allowed_types":["shell","exec"]}],
		"allow_sudo":true
	}}`)

	c, err := config.Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	tm := c.Taskmaster
	if tm.StaticDir != "/srv/web/taskmaster" {
		t.Errorf("static_dir = %q", tm.StaticDir)
	}
	if tm.DBPath != "/var/lib/taskmaster/taskmaster.db" {
		t.Errorf("db_path = %q", tm.DBPath)
	}
	if !tm.AllowSudo {
		t.Error("allow_sudo should be true from file")
	}
	if len(tm.Groups) != 1 {
		t.Fatalf("groups = %v, want 1 entry", tm.Groups)
	}
	g := tm.Groups[0]
	if g.Name != "default" || g.PoolLimit != 2 || len(g.AllowedTypes) != 2 || g.AllowedTypes[0] != "shell" || g.AllowedTypes[1] != "exec" {
		t.Errorf("group = %+v", g)
	}
}

func TestTaskmasterRoundTrip(t *testing.T) {
	want := config.DefaultConfig().Taskmaster
	want.Groups = []config.TaskmasterGroup{
		{Name: "default", PoolLimit: 2, AllowedTypes: []string{"shell"}},
	}
	data, err := json.Marshal(map[string]any{"taskmaster": want})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	path := writeCfg(t, string(data))

	c, err := config.Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	got := c.Taskmaster
	if got.StaticDir != want.StaticDir || got.DBPath != want.DBPath || got.AllowSudo != want.AllowSudo {
		t.Errorf("round trip changed the struct:\n got %+v\nwant %+v", got, want)
	}
	if len(got.Groups) != len(want.Groups) ||
		got.Groups[0].Name != want.Groups[0].Name ||
		got.Groups[0].PoolLimit != want.Groups[0].PoolLimit ||
		len(got.Groups[0].AllowedTypes) != len(want.Groups[0].AllowedTypes) ||
		got.Groups[0].AllowedTypes[0] != want.Groups[0].AllowedTypes[0] {
		t.Errorf("groups round trip changed:\n got %+v\nwant %+v", got.Groups, want.Groups)
	}
}

func TestTaskmasterTildeExpansion(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot determine home dir")
	}
	path := writeCfg(t, `{"taskmaster":{
		"static_dir":"~/web/taskmaster",
		"db_path":"~/taskmaster/taskmaster.db"
	}}`)

	c, err := config.Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	tm := c.Taskmaster
	for name, pair := range map[string][2]string{
		"static_dir": {tm.StaticDir, filepath.Join(home, "web/taskmaster")},
		"db_path":    {tm.DBPath, filepath.Join(home, "taskmaster/taskmaster.db")},
	} {
		if pair[0] != pair[1] {
			t.Errorf("%s = %q, want %q", name, pair[0], pair[1])
		}
	}
}

// applyServerDefaults copies the effective SSE subscriber cap into
// Taskmaster.SSEMaxSubscribers, mirroring every other SSE-serving module.
func TestTaskmasterSSEMaxSubscribers(t *testing.T) {
	t.Run("explicit server setting", func(t *testing.T) {
		path := writeCfg(t, `{"server":{"sse_max_subscribers":10},"taskmaster":{}}`)
		c, err := config.Load(path)
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		if c.Taskmaster.SSEMaxSubscribers != 10 {
			t.Errorf("SSEMaxSubscribers = %d, want 10", c.Taskmaster.SSEMaxSubscribers)
		}
	})

	t.Run("default when unset", func(t *testing.T) {
		path := writeCfg(t, `{"taskmaster":{}}`)
		c, err := config.Load(path)
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		if c.Taskmaster.SSEMaxSubscribers != config.DefaultSSEMaxSubscribers {
			t.Errorf("SSEMaxSubscribers = %d, want %d", c.Taskmaster.SSEMaxSubscribers, config.DefaultSSEMaxSubscribers)
		}
	})
}
