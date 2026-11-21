package opencode

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"cmd184psu/unified-webapp/internal/agentflow/sandbox"
)

// kiloDirs prepares the read-only Kilo state a sandboxed run needs. Kilo keeps
// its provider config in ~/.config/kilo and its login in
// ~/.local/share/kilo/auth.json. The config directory is bound read-only; the
// auth file is exposed through the stage's own data directory so Kilo can
// create its session database there without writing to the real one. The link
// is not a copy of the credential: on macOS Seatbelt denies writes that resolve
// outside the stage, and bwrap binds the target read-only.
//
// macOS cannot remap a mount, so its bind destinations are the host paths
// themselves; bwrap uses /sandbox paths. The generated opencode config is not
// used by Kilo, so this only affects the environment and mounts.
func kiloDirs(stageDir string) ([]sandbox.Bind, map[string]string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, nil, fmt.Errorf("opencode: kilo: user home: %w", err)
	}
	if home == "" {
		return nil, nil, fmt.Errorf("opencode: kilo: user home is empty")
	}
	config := filepath.Join(home, ".config", "kilo")
	auth := filepath.Join(home, ".local", "share", "kilo", "auth.json")
	if fi, err := os.Stat(config); err != nil || !fi.IsDir() {
		return nil, nil, fmt.Errorf("opencode: kilo: %s is missing; log in with the Kilo CLI first", config)
	}
	if fi, err := os.Stat(auth); err != nil || !fi.Mode().IsRegular() {
		return nil, nil, fmt.Errorf("opencode: kilo: %s is missing; log in with the Kilo CLI first", auth)
	}
	// Kilo writes its session database and repos under XDG_DATA_HOME. Keep that
	// in the stage's writable home and link only the auth file into it.
	dataDir := filepath.Join(HomeDir(stageDir), ".local", "share", "kilo")
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, nil, fmt.Errorf("opencode: kilo: stage data: %w", err)
	}
	link := filepath.Join(dataDir, "auth.json")
	if err := os.Remove(link); err != nil && !os.IsNotExist(err) {
		return nil, nil, fmt.Errorf("opencode: kilo: replace auth link: %w", err)
	}
	env := map[string]string{}
	var binds []sandbox.Bind
	if runtime.GOOS == "darwin" {
		// Seatbelt cannot remap paths: the config and auth are read at their host
		// paths, and writes outside the stage home are denied by the profile.
		env["XDG_CONFIG_HOME"] = filepath.Join(home, ".config")
		binds = append(binds,
			sandbox.Bind{Src: config, Dst: config},
			sandbox.Bind{Src: auth, Dst: auth},
		)
		if err := os.Symlink(auth, link); err != nil {
			return nil, nil, fmt.Errorf("opencode: kilo: auth link: %w", err)
		}
		return binds, env, nil
	}
	// bwrap can place both the config directory and the single auth file at
	// /sandbox, while the stage home remains writable.
	env["XDG_CONFIG_HOME"] = "/sandbox/kilo-config"
	const authBind = "/sandbox/kilo-auth"
	binds = append(binds,
		sandbox.Bind{Src: config, Dst: "/sandbox/kilo-config/kilo"},
		sandbox.Bind{Src: auth, Dst: authBind},
	)
	if err := os.Symlink(authBind, link); err != nil {
		return nil, nil, fmt.Errorf("opencode: kilo: auth link: %w", err)
	}
	return binds, env, nil
}
