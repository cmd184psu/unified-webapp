package config_test

import (
	"encoding/json"
	"fmt"
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
	if len(tm.Lanes) != 0 {
		t.Errorf("lanes = %v, want empty", tm.Lanes)
	}
}

func TestTaskmasterLoadFromFile(t *testing.T) {
	path := writeCfg(t, `{"taskmaster":{
		"static_dir":"/srv/web/taskmaster",
		"db_path":"/var/lib/taskmaster/taskmaster.db",
		"lanes":[{"name":"default","width":2}],
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
	if len(tm.Lanes) != 1 {
		t.Fatalf("lanes = %v, want 1 entry", tm.Lanes)
	}
	l := tm.Lanes[0]
	if l.Name != "default" || l.Width != 2 {
		t.Errorf("lane = %+v", l)
	}
}

func TestTaskmasterRoundTrip(t *testing.T) {
	want := config.DefaultConfig().Taskmaster
	want.Lanes = []config.TaskmasterLane{
		{Name: "default", Width: 2},
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
	if len(got.Lanes) != len(want.Lanes) ||
		got.Lanes[0].Name != want.Lanes[0].Name ||
		got.Lanes[0].Width != want.Lanes[0].Width {
		t.Errorf("lanes round trip changed:\n got %+v\nwant %+v", got.Lanes, want.Lanes)
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

// TestTaskmasterProgressIntervalNormalization pins normalizeTaskmaster's
// clamp table (plan Q3/§4.8): 0 takes the 2000ms default (today's cadence),
// a value below the 100ms floor or above the 2000ms ceiling clamps with a
// log line, a value inside the range passes through unchanged, and a
// negative value is a hard config error.
func TestTaskmasterProgressIntervalNormalization(t *testing.T) {
	cases := []struct {
		name    string
		in      int
		want    int
		wantErr bool
	}{
		{name: "zero takes default", in: 0, want: 2000},
		{name: "below minimum clamps up", in: 50, want: 100},
		{name: "above maximum clamps down", in: 5000, want: 2000},
		{name: "in range passes through", in: 500, want: 500},
		{name: "negative is an error", in: -1, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := writeCfg(t, fmt.Sprintf(`{"taskmaster":{"progress_interval_ms":%d}}`, tc.in))
			c, err := config.Load(path)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("Load(progress_interval_ms=%d) succeeded, want error", tc.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("Load(progress_interval_ms=%d): %v", tc.in, err)
			}
			if c.Taskmaster.ProgressIntervalMs != tc.want {
				t.Errorf("progress_interval_ms = %d, want %d", c.Taskmaster.ProgressIntervalMs, tc.want)
			}
		})
	}
}

// TestTaskmasterDefaultConfigProgressInterval pins DefaultConfig's
// progress_interval_ms (the value a fresh config file is written with) to
// the same 2000ms floor normalizeTaskmaster falls back to.
func TestTaskmasterDefaultConfigProgressInterval(t *testing.T) {
	c := config.DefaultConfig()
	if c.Taskmaster.ProgressIntervalMs != config.DefaultTaskmasterProgressIntervalMs {
		t.Errorf("DefaultConfig progress_interval_ms = %d, want %d", c.Taskmaster.ProgressIntervalMs, config.DefaultTaskmasterProgressIntervalMs)
	}
}
