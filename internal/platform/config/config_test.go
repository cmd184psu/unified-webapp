package config_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"cmd184psu/unified-webapp/internal/platform/config"
)

func TestExpandPath_NoTilde(t *testing.T) {
	got, err := config.ExpandPath("/absolute/path")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "/absolute/path" {
		t.Errorf("got %q, want %q", got, "/absolute/path")
	}
}

func TestExpandPath_Empty(t *testing.T) {
	got, err := config.ExpandPath("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestExpandPath_Tilde(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot determine home dir")
	}
	got, err := config.ExpandPath("~/foo/bar")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Join(home, "foo/bar")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestLoadMissingFile_ReturnsDefaults(t *testing.T) {
	cfg, err := config.Load(filepath.Join(t.TempDir(), "nonexistent.json"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Port != 8080 {
		t.Errorf("default port: got %d, want 8080", cfg.Port)
	}
	if cfg.Grocery.SyncIntervalSeconds != 1 {
		t.Errorf("default sync interval: got %d, want 1", cfg.Grocery.SyncIntervalSeconds)
	}
	if cfg.Grocery.Title != "Grocery List" {
		t.Errorf("default title: got %q, want %q", cfg.Grocery.Title, "Grocery List")
	}
	if len(cfg.Grocery.Groups) == 0 {
		t.Error("default groups should be non-empty")
	}
}

func TestLoadFile_OverridesDefaults(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "test.json")

	data, _ := json.Marshal(map[string]any{
		"port": 9090,
		"grocery": map[string]any{
			"title":    "Test List",
			"data_dir": dir,
		},
	})
	if err := os.WriteFile(cfgFile, data, 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	cfg, err := config.Load(cfgFile)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Port != 9090 {
		t.Errorf("port: got %d, want 9090", cfg.Port)
	}
	if cfg.Grocery.Title != "Test List" {
		t.Errorf("title: got %q, want %q", cfg.Grocery.Title, "Test List")
	}
}

func TestLoadMissingFile_MenuserverDefaults(t *testing.T) {
	cfg, err := config.Load(filepath.Join(t.TempDir(), "nonexistent.json"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Menuserver.StaticDir == "" {
		t.Error("menuserver static_dir should not be empty")
	}
	if cfg.Menuserver.DataDir == "" {
		t.Error("menuserver data_dir should not be empty")
	}
}

func TestLoadMissingFile_SlideshowDefaults(t *testing.T) {
	cfg, err := config.Load(filepath.Join(t.TempDir(), "nonexistent.json"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Slideshow.Prefix != "slides" {
		t.Errorf("slideshow prefix: got %q, want %q", cfg.Slideshow.Prefix, "slides")
	}
	if cfg.Slideshow.StaticDir == "" {
		t.Error("slideshow static_dir should not be empty")
	}
}

func TestLoadMissingFile_TodoDefaults(t *testing.T) {
	cfg, err := config.Load(filepath.Join(t.TempDir(), "nonexistent.json"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Todo.Ext != "json" {
		t.Errorf("todo ext: got %q, want %q", cfg.Todo.Ext, "json")
	}
	if cfg.Todo.DefaultSubject != "home" {
		t.Errorf("todo defaultSubject: got %q, want %q", cfg.Todo.DefaultSubject, "home")
	}
	if cfg.Todo.SyncIntervalSeconds != 1 {
		t.Errorf("todo sync interval: got %d, want 1", cfg.Todo.SyncIntervalSeconds)
	}
}

func TestWriteDefault_CreatesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.json")
	if err := config.WriteDefault(path); err != nil {
		t.Fatalf("WriteDefault: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file not created: %v", err)
	}
}

func TestWriteDefault_Idempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.json")
	if err := config.WriteDefault(path); err != nil {
		t.Fatalf("first WriteDefault: %v", err)
	}
	// Overwrite with custom content.
	if err := os.WriteFile(path, []byte(`{"port":1234}`), 0644); err != nil {
		t.Fatalf("overwrite: %v", err)
	}
	// Second call must not overwrite.
	if err := config.WriteDefault(path); err != nil {
		t.Fatalf("second WriteDefault: %v", err)
	}
	data, _ := os.ReadFile(path)
	var cfg config.Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if cfg.Port != 1234 {
		t.Errorf("WriteDefault overwrote existing file: port=%d", cfg.Port)
	}
}
