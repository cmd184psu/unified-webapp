package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const DefaultConfigPath = "~/.unified-webapp.json"

// Config is the top-level unified server configuration.
type Config struct {
	Port       int               `json:"port"`
	TLSCert    string            `json:"tls_cert"`
	TLSKey     string            `json:"tls_key"`
	Routing    map[string]string `json:"host_routing"` // hostname → module name
	Grocery    GroceryConfig     `json:"grocery"`
	Todo       TodoConfig        `json:"todo"`
	Slideshow  SlideshowConfig   `json:"slideshow"`
	Menuserver MenuserverConfig  `json:"menuserver"`
}

// MenuserverConfig holds configuration specific to the menuserver module.
type MenuserverConfig struct {
	StaticDir    string `json:"static_dir"`
	DataDir      string `json:"data_dir"`
	ShowAllPages bool   `json:"show_all_pages"`
}

// SlideshowConfig holds configuration specific to the slideshow module.
type SlideshowConfig struct {
	StaticDir      string `json:"static_dir"`
	ImageDir       string `json:"image_dir"`
	Prefix         string `json:"prefix"`
	DefaultSubject string `json:"default_subject"`
}

// TodoConfig holds configuration specific to the todo module.
type TodoConfig struct {
	StaticDir           string `json:"static_dir"`
	DataDir             string `json:"data_dir"`
	Ext                 string `json:"ext"`
	DefaultSubject      string `json:"default_subject"`
	SyncIntervalSeconds int    `json:"sync_interval_seconds"`
}

// GroceryConfig holds configuration specific to the grocery module.
type GroceryConfig struct {
	StaticDir           string   `json:"static_dir"`
	DataFile            string   `json:"data_file"`
	Groups              []string `json:"groups"`
	Progress            bool     `json:"progress"`
	SyncIntervalSeconds int      `json:"sync_interval_seconds"`
	Title               string   `json:"title"`
}

// DefaultConfig returns a Config populated with safe defaults.
func DefaultConfig() *Config {
	return &Config{
		Port:    8080,
		Routing: map[string]string{},
		Grocery: GroceryConfig{
			StaticDir:           "./web/grocery",
			DataFile:            "./data/grocery.json",
			SyncIntervalSeconds: 1,
			Title:               "Grocery List",
			Groups: []string{
				"Produce",
				"Meats",
				"mid store",
				"back wall",
				"frozen",
				"deli area near front",
			},
		},
		Todo: TodoConfig{
			StaticDir:           "./web/todo",
			DataDir:             "./data/todo",
			Ext:                 "json",
			DefaultSubject:      "home",
			SyncIntervalSeconds: 1,
		},
		Slideshow: SlideshowConfig{
			StaticDir: "./web/slideshow",
			ImageDir:  "./data/slideshow",
			Prefix:    "slides",
		},
		Menuserver: MenuserverConfig{
			StaticDir: "./web/menuserver",
			DataDir:   "./data/menuserver",
		},
	}
}

// ExpandPath expands a leading ~ to the user home directory.
func ExpandPath(path string) (string, error) {
	if len(path) == 0 || path[0] != '~' {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("could not determine home directory: %w", err)
	}
	return filepath.Join(home, path[1:]), nil
}

// Load reads the config file at path (~ supported). Missing file returns defaults.
func Load(path string) (*Config, error) {
	expanded, err := ExpandPath(path)
	if err != nil {
		return nil, err
	}

	cfg := DefaultConfig()

	data, err := os.ReadFile(expanded)
	if os.IsNotExist(err) {
		return cfg, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading config %s: %w", expanded, err)
	}
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config %s: %w", expanded, err)
	}

	if err := expandGroceryPaths(&cfg.Grocery); err != nil {
		return nil, err
	}
	if err := expandTodoPaths(&cfg.Todo); err != nil {
		return nil, err
	}
	if err := expandSlideshowPaths(&cfg.Slideshow); err != nil {
		return nil, err
	}
	if err := expandMenuserverPaths(&cfg.Menuserver); err != nil {
		return nil, err
	}
	if cfg.TLSCert != "" {
		if cfg.TLSCert, err = ExpandPath(cfg.TLSCert); err != nil {
			return nil, err
		}
	}
	if cfg.TLSKey != "" {
		if cfg.TLSKey, err = ExpandPath(cfg.TLSKey); err != nil {
			return nil, err
		}
	}
	return cfg, nil
}

func expandMenuserverPaths(m *MenuserverConfig) error {
	var err error
	if m.StaticDir, err = ExpandPath(m.StaticDir); err != nil {
		return err
	}
	if m.DataDir, err = ExpandPath(m.DataDir); err != nil {
		return err
	}
	return nil
}

func expandSlideshowPaths(s *SlideshowConfig) error {
	var err error
	if s.StaticDir, err = ExpandPath(s.StaticDir); err != nil {
		return err
	}
	if s.ImageDir, err = ExpandPath(s.ImageDir); err != nil {
		return err
	}
	return nil
}

func expandTodoPaths(t *TodoConfig) error {
	var err error
	if t.StaticDir, err = ExpandPath(t.StaticDir); err != nil {
		return err
	}
	if t.DataDir, err = ExpandPath(t.DataDir); err != nil {
		return err
	}
	return nil
}

func expandGroceryPaths(g *GroceryConfig) error {
	var err error
	if g.StaticDir, err = ExpandPath(g.StaticDir); err != nil {
		return err
	}
	if g.DataFile, err = ExpandPath(g.DataFile); err != nil {
		return err
	}
	return nil
}

// WriteDefault writes a default config to path if the file does not already exist.
func WriteDefault(path string) error {
	expanded, err := ExpandPath(path)
	if err != nil {
		return err
	}
	if _, err := os.Stat(expanded); err == nil {
		return nil
	}
	data, err := json.MarshalIndent(DefaultConfig(), "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(expanded, data, 0644)
}
