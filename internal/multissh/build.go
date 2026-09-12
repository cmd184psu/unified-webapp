package multissh

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"cmd184psu/unified-webapp/internal/multissh/sshproxy"
	"cmd184psu/unified-webapp/internal/platform/config"
)

// Build returns a ready-to-use http.Handler for the multissh module. The
// caller is responsible for wrapping it with middleware.
//
// Build fails rather than degrades. A missing static_dir, or strict_host_key
// with no readable known_hosts, is a misconfiguration the operator needs told
// about at boot: serving 404s or falling back to unverified host keys would
// look like a routing bug instead. Nothing is started here -- the returned
// handler is inert until the dispatcher serves it.
func Build(cfg config.MultisshConfig) (http.Handler, error) {
	sshDir := strings.TrimSpace(cfg.SSHDir)
	if sshDir == "" {
		dir, err := sshproxy.DefaultSSHDir()
		if err != nil {
			return nil, fmt.Errorf("multissh: resolve ssh dir: %w", err)
		}
		sshDir = dir
	}

	uploadDir := strings.TrimSpace(cfg.UploadDir)
	if uploadDir == "" {
		uploadDir = filepath.Join(os.TempDir(), "multissh-uploads")
	}

	browseRoot := strings.TrimSpace(cfg.BrowseRoot)
	if browseRoot == "" {
		browseRoot = uploadDir
	}

	knownHosts := strings.TrimSpace(cfg.KnownHostsPath)
	if knownHosts == "" {
		knownHosts = filepath.Join(sshDir, "known_hosts")
	}

	staticDir := strings.TrimSpace(cfg.StaticDir)
	if err := checkStaticDir(staticDir); err != nil {
		return nil, err
	}

	hostKeyCB, err := sshproxy.HostKeyCallback(cfg.StrictHostKey, knownHosts)
	if err != nil {
		return nil, fmt.Errorf("multissh: known_hosts %s: %w", knownHosts, err)
	}

	if err := os.MkdirAll(uploadDir, 0o750); err != nil {
		return nil, fmt.Errorf("multissh: create upload dir %s: %w", uploadDir, err)
	}
	if hostsPath := strings.TrimSpace(cfg.HostsPath); hostsPath != "" {
		if err := os.MkdirAll(filepath.Dir(hostsPath), 0o750); err != nil {
			return nil, fmt.Errorf("multissh: create hosts dir for %s: %w", hostsPath, err)
		}
	}

	// One SFTPTransferrer serves both roles: the broadcast transfers and the
	// remote directory picker dial the same way.
	sftp := sshproxy.SFTPTransferrer{HostKeyCallback: hostKeyCB}
	srv := New(Options{
		StaticDir:      staticDir,
		SSHHandler:     sshproxy.NewHandler(sshproxy.SSHDialer{HostKeyCallback: hostKeyCB}, sshDir),
		SSHKeyDir:      sshDir,
		UploadDir:      uploadDir,
		HostsPath:      cfg.HostsPath,
		BrowseRoot:     browseRoot,
		MaxSessions:    cfg.MaxSessions,
		MaxUploadBytes: cfg.MaxUploadBytes,
		Transferrer:    sftp,
		RemoteLister:   sftp,
	})
	return srv.Handler(), nil
}

// checkStaticDir refuses a static_dir that is not a readable directory. The
// alternative -- warn and serve 404s -- is indistinguishable at runtime from a
// routing mistake, and the operator only finds out by loading the UI.
func checkStaticDir(dir string) error {
	if dir == "" {
		return fmt.Errorf("multissh: static_dir is not set")
	}
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("multissh: static_dir %s: %w", dir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("multissh: static_dir %s is not a directory", dir)
	}
	entries, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("multissh: static_dir %s is not readable: %w", dir, err)
	}
	_ = entries.Close()
	return nil
}
