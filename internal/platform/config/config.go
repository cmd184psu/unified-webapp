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
	Port    int               `json:"port"`
	TLSCert string            `json:"tls_cert"`
	TLSKey  string            `json:"tls_key"`
	Routing map[string]string `json:"host_routing"` // hostname → module name
	Grocery GroceryConfig     `json:"grocery"`
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
