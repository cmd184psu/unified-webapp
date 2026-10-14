package config_test

import (
	"path/filepath"
	"testing"

	"cmd184psu/unified-webapp/internal/platform/config"
)

func TestAgentflowDefaults(t *testing.T) {
	a := config.DefaultConfig().Agentflow
	if a.StaticDir != "./web/agentflow" {
		t.Errorf("static_dir = %q", a.StaticDir)
	}
	if a.DataDir != "./data/agentflow" {
		t.Errorf("data_dir = %q", a.DataDir)
	}
	if a.SSEMaxSubscribers != config.DefaultSSEMaxSubscribers {
		t.Errorf("sse cap = %d", a.SSEMaxSubscribers)
	}
}

func TestAgentflowPathsExpanded(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfg, err := config.Load(writeCfg(t, `{"agentflow": {"static_dir": "~/af/web", "data_dir": "~/af/data"}}`))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if want := filepath.Join(home, "af/web"); cfg.Agentflow.StaticDir != want {
		t.Errorf("static_dir = %q, want %q", cfg.Agentflow.StaticDir, want)
	}
	if want := filepath.Join(home, "af/data"); cfg.Agentflow.DataDir != want {
		t.Errorf("data_dir = %q, want %q", cfg.Agentflow.DataDir, want)
	}
}

// CF3: the removed keys (profiles, prompt_dir, ...) in an old config file are
// ignored rather than failing the load or re-appearing in the struct.
func TestAgentflowOldKeysIgnored(t *testing.T) {
	cfg, err := config.Load(writeCfg(t, `{"agentflow": {"default_profile": "m5", "prompt_dir": "/p", "profiles": [{"name": "m5"}], "data_dir": "/d"}}`))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Agentflow.DataDir != "/d" {
		t.Errorf("data_dir = %q", cfg.Agentflow.DataDir)
	}
}

func TestAgentflowSSECapFromServer(t *testing.T) {
	cfg, err := config.Load(writeCfg(t, `{"server": {"sse_max_subscribers": 7}}`))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Agentflow.SSEMaxSubscribers != 7 {
		t.Errorf("sse cap = %d, want 7", cfg.Agentflow.SSEMaxSubscribers)
	}
}
