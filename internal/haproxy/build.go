package haproxy

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"cmd184psu/unified-webapp/internal/platform/config"
	"cmd184psu/unified-webapp/internal/platform/response"
)

// Version is the module version reported by the status endpoint.
const Version = "0.1.0"

// NewDefaultExec supplies the command layer Build hands to the OS driver. It is
// a TEST SEAM: tests in other packages override it so building the module never
// runs real sudo/systemctl/haproxy. Production code must not reassign it.
var NewDefaultExec = func() Exec { return NewExec() }

// Build returns a ready-to-use http.Handler for the haproxy editor module.
// The caller wraps it with the platform middleware and auth gate.
//
// Build fails loudly on a misconfiguration the operator must be told about at
// boot: a missing/unreadable static_dir or an unusable data_dir returns an
// error, which the dispatcher turns into a 503 for this hostname while the
// rest of the binary serves (the smbedit pattern).
//
// An unsupported OS is different: it is not an operator misconfiguration of
// this module's files, so the module still BUILDS and every request answers
// 503 with the reason (FR-H30). That keeps the failure scoped to this module
// exactly like a build failure, while making the reason visible.
func Build(cfg config.HaproxyConfig) (http.Handler, error) {
	staticDir := strings.TrimSpace(cfg.StaticDir)
	if err := checkStaticDir(staticDir); err != nil {
		return nil, err
	}

	dataDir := strings.TrimSpace(cfg.DataDir)
	if dataDir == "" {
		return nil, fmt.Errorf("haproxy: data_dir is not set")
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("haproxy: create data dir %s: %w", dataDir, err)
	}

	detected, err := DetectOS(cfg.OS)
	if err != nil {
		// The module builds; every request answers 503 with the reason.
		return scoped503Handler(err.Error()), nil
	}

	driver, err := NewDriver(detected, NewDefaultExec(), DriverOptions{
		ConfigPath:      strings.TrimSpace(cfg.ConfigPath),
		CertsDir:        strings.TrimSpace(cfg.CertsDir),
		CrtListPath:     strings.TrimSpace(cfg.CrtListPath),
		StatsSocketPath: strings.TrimSpace(cfg.StatsSocketPath),
		ServiceName:     strings.TrimSpace(cfg.ServiceName),
	})
	if err != nil {
		return nil, err
	}

	// The CertMachine client exists only when a URL is configured; without it
	// the certmachine routes answer 409. A bad url/ca_file is an operator
	// misconfiguration, so it fails the build loudly (scoped 503 by the
	// dispatcher) rather than degrading silently.
	var client *CertMachineClient
	if u := strings.TrimSpace(cfg.CertMachine.URL); u != "" {
		client, err = CertMachineNewClient(CertMachineSettings{
			URL:    u,
			APIKey: CertMachineAPIKey(cfg.CertMachine.APIKey),
			CAFile: strings.TrimSpace(cfg.CertMachine.CAFile),
		})
		if err != nil {
			return nil, err
		}
	}
	return buildWithDriver(cfg, driver, client)
}

// buildWithDriver assembles the module over an already-chosen driver and
// (possibly nil) CertMachine client. It is the seam handler tests use to run
// the real handlers over the fake driver, never touching sudo or haproxy.
func buildWithDriver(cfg config.HaproxyConfig, driver Driver, client *CertMachineClient) (http.Handler, error) {
	dataDir := strings.TrimSpace(cfg.DataDir)
	models, err := NewModelStore(dataDir)
	if err != nil {
		return nil, fmt.Errorf("haproxy: %w", err)
	}
	certs := CertNewStore(dataDir, driver)
	log := NewOpLog(500)

	s := &server{
		cfg: cfg, driver: driver, models: models, certs: certs, log: log, client: client,
		dataDir: dataDir, warnDays: cfg.ExpiryWarnDays, maxSubs: cfg.SSEMaxSubscribers,
	}
	if s.warnDays <= 0 {
		s.warnDays = config.DefaultHaproxyExpiryWarnDays
	}
	if s.maxSubs <= 0 {
		s.maxSubs = config.DefaultSSEMaxSubscribers
	}
	keep := cfg.BackupKeep
	if keep <= 0 {
		keep = config.DefaultHaproxyBackupKeep
	}
	s.applier = NewApplier(driver, models, certs, log, ApplyOptions{
		Verifier:     StatsVerifier{Driver: driver, Log: log, Timeout: statsTimeout},
		BackupKeep:   keep,
		StagingDir:   filepath.Join(dataDir, "staging"),
		AfterSuccess: s.removeSuperseded,
	})
	if err := os.MkdirAll(filepath.Join(dataDir, "staging"), 0o700); err != nil {
		return nil, fmt.Errorf("haproxy: create staging dir: %w", err)
	}
	return s.routes(strings.TrimSpace(cfg.StaticDir)), nil
}

// scoped503Handler answers every request with 503 and the reason. Used when
// the OS is unsupported: the module is up enough to explain why it cannot
// serve, and the failure is scoped to this hostname.
func scoped503Handler(reason string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error":  "module unavailable",
			"module": "haproxy",
			"reason": reason,
		})
	})
}

// checkStaticDir refuses a static_dir that is not a readable directory, for
// the same reason smbedit does: warning and serving 404s is indistinguishable
// at runtime from a routing mistake.
func checkStaticDir(dir string) error {
	if dir == "" {
		return fmt.Errorf("haproxy: static_dir is not set")
	}
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("haproxy: static_dir %s: %w", dir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("haproxy: static_dir %s is not a directory", dir)
	}
	f, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("haproxy: static_dir %s is not readable: %w", dir, err)
	}
	_ = f.Close()
	return nil
}
