package admin

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"cmd184psu/unified-webapp/internal/platform/config"
)

// validateConfigFile re-checks a config file after it was rewritten. It is a
// variable so tests can force a validation failure.
var validateConfigFile = func(path string) error {
	_, err := config.Load(path)
	return err
}

// AddAPIKeyToConfigFile upserts {name, hash} into auth.api_keys of the config
// file at configPath, for the -gen-api-key CLI. An existing entry with the
// same name has its hash replaced (key rotation) and replaced is true. Only
// the top-level "auth" member is rewritten (spliceAuthConfig); every other
// byte is untouched. The file must already exist and keeps its mode. If the
// result fails to load, the original bytes are restored atomically.
func AddAPIKeyToConfigFile(configPath, name, hash string) (replaced bool, err error) {
	if strings.TrimSpace(name) == "" {
		return false, errors.New("admin: API key name must not be empty")
	}
	st, err := os.Stat(configPath)
	if err != nil {
		return false, fmt.Errorf("admin: config file: %w", err)
	}
	orig, err := os.ReadFile(configPath)
	if err != nil {
		return false, fmt.Errorf("admin: reading config: %w", err)
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		return false, err
	}

	auth := cfg.Auth
	for _, e := range auth.APIKeys {
		if e.Name == name {
			replaced = true
			break
		}
	}
	auth.APIKeys = upsertNamedHash(auth.APIKeys, name, hash)

	if err := spliceAuthConfig(configPath, auth); err != nil {
		return false, err
	}
	mode := st.Mode().Perm()
	if err := os.Chmod(configPath, mode); err != nil {
		restoreConfigFile(configPath, orig, mode)
		return false, fmt.Errorf("admin: restoring config mode: %w", err)
	}
	if err := validateConfigFile(configPath); err != nil {
		if rerr := restoreConfigFile(configPath, orig, mode); rerr != nil {
			return false, fmt.Errorf("admin: updated config is invalid (%v) and restoring the original failed: %w", err, rerr)
		}
		return false, fmt.Errorf("admin: updated config is invalid, original restored: %w", err)
	}
	return replaced, nil
}

// restoreConfigFile atomically puts orig back at path with the given mode.
func restoreConfigFile(path string, orig []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".auth-restore-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(orig); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}
