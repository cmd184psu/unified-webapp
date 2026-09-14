package config_test

import (
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"cmd184psu/unified-webapp/internal/platform/config"
)

func TestUtuberDefaults(t *testing.T) {
	u := config.DefaultConfig().Utuber
	if u.StaticDir != "./web/utuber" {
		t.Errorf("static_dir = %q", u.StaticDir)
	}
	if u.DownloadDir != "./data/utuber/downloads" {
		t.Errorf("download_dir = %q", u.DownloadDir)
	}
	if u.Workers != config.DefaultUtuberWorkers {
		t.Errorf("workers = %d, want %d", u.Workers, config.DefaultUtuberWorkers)
	}
	if u.PythonBin != config.DefaultUtuberPythonBin {
		t.Errorf("python_bin = %q, want %q", u.PythonBin, config.DefaultUtuberPythonBin)
	}
}

func TestUtuberWorkersValidation(t *testing.T) {
	cases := []struct {
		name    string
		workers int
		want    int
		wantErr bool
	}{
		{"zero takes default", 0, config.DefaultUtuberWorkers, false},
		{"explicit value kept", 4, 4, false},
		{"negative rejected", -1, 0, true},
		{"oversized clamped", 100, config.MaxUtuberWorkers, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := writeCfg(t, `{"utuber": {"workers": `+strconv.Itoa(tc.workers)+`}}`)
			cfg, err := config.Load(path)
			if tc.wantErr {
				if err == nil {
					t.Fatal("Load succeeded, want error")
				}
				if !strings.Contains(err.Error(), "utuber") {
					t.Errorf("error %q does not name utuber", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if cfg.Utuber.Workers != tc.want {
				t.Errorf("workers = %d, want %d", cfg.Utuber.Workers, tc.want)
			}
		})
	}
}

func TestUtuberPythonBinDefaulting(t *testing.T) {
	path := writeCfg(t, `{"utuber": {"python_bin": ""}}`)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Utuber.PythonBin != config.DefaultUtuberPythonBin {
		t.Errorf("empty python_bin = %q, want %q", cfg.Utuber.PythonBin, config.DefaultUtuberPythonBin)
	}

	path = writeCfg(t, `{"utuber": {"python_bin": "python3.11"}}`)
	cfg, err = config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Utuber.PythonBin != "python3.11" {
		t.Errorf("explicit python_bin = %q, want python3.11", cfg.Utuber.PythonBin)
	}
}

func TestUtuberTildeExpansion(t *testing.T) {
	body := `{"utuber": {
		"static_dir": "~/utuber-static",
		"download_dir": "~/utuber-downloads",
		"python_bin": "python3.12"
	}}`
	cfg, err := config.Load(writeCfg(t, body))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if strings.HasPrefix(cfg.Utuber.StaticDir, "~") || !filepath.IsAbs(cfg.Utuber.StaticDir) {
		t.Errorf("static_dir not expanded: %q", cfg.Utuber.StaticDir)
	}
	if strings.HasPrefix(cfg.Utuber.DownloadDir, "~") || !filepath.IsAbs(cfg.Utuber.DownloadDir) {
		t.Errorf("download_dir not expanded: %q", cfg.Utuber.DownloadDir)
	}
	if cfg.Utuber.PythonBin != "python3.12" {
		t.Errorf("python_bin was altered: %q", cfg.Utuber.PythonBin)
	}
}
