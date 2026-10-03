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

	s, err := newServer(cfg)
	if err != nil {
		return nil, err
	}
	// The effective settings are the config block overridden by settings.json.
	// A bad url/ca_file is an operator misconfiguration, so it fails the build
	// loudly (scoped 503 by the dispatcher); an unsupported OS builds a module
	// that explains itself (FR-H30).
	st, err := loadStored(s.dataDir)
	if err != nil {
		return nil, err
	}
	set, key := st.overlay(baseSettings(cfg), cfg.CertMachine.APIKey)
	rt, err := s.build(set, key)
	if err != nil {
		return nil, err
	}
	s.cur = rt
	return s.routes(staticDir), nil
}

// driverFactory makes the OS driver for the given overrides. Tests replace it.
type driverFactory func(OS, DriverOptions) (Driver, error)

func newServer(cfg config.HaproxyConfig) (*server, error) {
	dataDir := strings.TrimSpace(cfg.DataDir)
	models, err := NewModelStore(dataDir)
	if err != nil {
		return nil, fmt.Errorf("haproxy: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(dataDir, "staging"), 0o700); err != nil {
		return nil, fmt.Errorf("haproxy: create staging dir: %w", err)
	}
	s := &server{
		cfg: cfg, models: models, log: NewOpLog(500), dataDir: dataDir, maxSubs: cfg.SSEMaxSubscribers,
		mk: func(o OS, opts DriverOptions) (Driver, error) { return NewDriver(o, NewDefaultExec(), opts) },
	}
	if s.maxSubs <= 0 {
		s.maxSubs = config.DefaultSSEMaxSubscribers
	}
	return s, nil
}

// build makes a complete runtime from the settings: OS driver, CertMachine
// client, cert store, applier. An unsupported OS is not an error; it yields a
// runtime that only carries the reason.
func (s *server) build(set Settings, key string) (*live, error) {
	detected, err := DetectOS(set.OS)
	if err != nil {
		return &live{settings: set, apiKey: key, unavailable: err.Error()}, nil
	}
	driver, err := s.mk(detected, driverOptions(set))
	if err != nil {
		return nil, err
	}
	var client *CertMachineClient
	if set.CertMachineURL != "" {
		client, err = CertMachineNewClient(CertMachineSettings{
			URL: set.CertMachineURL, APIKey: CertMachineAPIKey(key), CAFile: set.CertMachineCAFile,
		})
		if err != nil {
			return nil, err
		}
	}
	return s.assemble(driver, client, set, key), nil
}

// assemble wires the collaborators over an already-chosen driver and (possibly
// nil) CertMachine client.
func (s *server) assemble(driver Driver, client *CertMachineClient, set Settings, key string) *live {
	rt := &live{driver: driver, client: client, settings: set, apiKey: key, warnDays: set.ExpiryWarnDays}
	rt.certs = CertNewStore(s.dataDir, driver)
	rt.applier = NewApplier(driver, s.models, rt.certs, s.log, ApplyOptions{
		Verifier:     StatsVerifier{Driver: driver, Log: s.log, Timeout: statsTimeout},
		BackupKeep:   set.BackupKeep,
		StagingDir:   filepath.Join(s.dataDir, "staging"),
		AfterSuccess: rt.removeSuperseded,
	})
	return rt
}

// buildServer assembles the module over an already-chosen driver and (possibly
// nil) CertMachine client. It is the seam handler tests use to run the real
// handlers over the fake driver, never touching sudo or haproxy.
func buildServer(cfg config.HaproxyConfig, driver Driver, client *CertMachineClient) (*server, error) {
	s, err := newServer(cfg)
	if err != nil {
		return nil, err
	}
	st, err := loadStored(s.dataDir)
	if err != nil {
		return nil, err
	}
	set, key := st.overlay(baseSettings(cfg), cfg.CertMachine.APIKey)
	s.cur = s.assemble(driver, client, set, key)
	return s, nil
}

func buildWithDriver(cfg config.HaproxyConfig, driver Driver, client *CertMachineClient) (http.Handler, error) {
	s, err := buildServer(cfg, driver, client)
	if err != nil {
		return nil, err
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

// driverOptions maps the effective settings onto the driver's overrides. An
// unset setting stays empty so the driver's per-OS default applies; every path
// the operator can set must be mapped here (a missing one silently falls back
// to the default location of the real HAProxy).
func driverOptions(set Settings) DriverOptions {
	return DriverOptions{
		ConfigPath:      set.ConfigPath,
		CertsDir:        set.CertsDir,
		CrtListPath:     set.CrtListPath,
		StatsSocketPath: set.StatsSocketPath,
		BackupDir:       set.BackupDir,
		ServiceName:     set.ServiceName,
	}
}
