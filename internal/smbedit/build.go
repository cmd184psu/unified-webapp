package smbedit

import (
	"fmt"
	"net/http"
	"os"
	"strings"

	"cmd184psu/unified-webapp/internal/platform/config"
)

// Build returns a ready-to-use http.Handler for the smbedit module. The
// caller is responsible for wrapping it with middleware.
//
// Build fails rather than degrades. A missing static_dir or an unparseable
// state.json is a misconfiguration the operator needs told about at boot:
// serving 404s, or silently overwriting operator state with defaults, would
// look like a routing bug instead. Nothing is started here — the returned
// handler is inert until the dispatcher serves it.
func Build(cfg config.SmbeditConfig) (http.Handler, error) {
	staticDir := strings.TrimSpace(cfg.StaticDir)
	if err := checkStaticDir(staticDir); err != nil {
		return nil, err
	}

	dataDir := strings.TrimSpace(cfg.DataDir)
	if dataDir == "" {
		return nil, fmt.Errorf("smbedit: data_dir is not set")
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("smbedit: create data dir %s: %w", dataDir, err)
	}

	st, err := newStore(dataDir)
	if err != nil {
		return nil, fmt.Errorf("smbedit: %w", err)
	}

	pickerRoot := strings.TrimSpace(cfg.PickerRoot)
	if pickerRoot == "" {
		pickerRoot = defaultPickerRoot
	}

	srv := newServer(serverOptions{
		store:      st,
		staticDir:  staticDir,
		pickerRoot: pickerRoot,
		version:    Version,
	})
	return srv.Handler(), nil
}

// checkStaticDir refuses a static_dir that is not a readable directory. The
// alternative — warn and serve 404s — is indistinguishable at runtime from a
// routing mistake, and the operator only finds out by loading the UI.
func checkStaticDir(dir string) error {
	if dir == "" {
		return fmt.Errorf("smbedit: static_dir is not set")
	}
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("smbedit: static_dir %s: %w", dir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("smbedit: static_dir %s is not a directory", dir)
	}
	entries, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("smbedit: static_dir %s is not readable: %w", dir, err)
	}
	_ = entries.Close()
	return nil
}
