package haproxy

// Module settings editable from the UI (Settings tab). The config-file
// `haproxy` block supplies optional starting values; <data_dir>/settings.json
// holds only the fields the user changed (and the CertMachine API key) and
// overrides the block. A save rebuilds the driver, CertMachine client, cert
// store and applier and swaps them in under s.rtMu, so nothing needs a restart.

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"cmd184psu/unified-webapp/internal/platform/config"
	"cmd184psu/unified-webapp/internal/platform/response"
)

const settingsFile = "settings.json"

// Settings is every editable setting (the effective values). An empty path or
// service name means "the driver's default for this OS".
type Settings struct {
	OS                string `json:"os"`
	CertMachineURL    string `json:"certmachineUrl"`
	CertMachineCAFile string `json:"certmachineCaFile"`
	// CertMachineInsecure skips the certificate check on the CertMachine
	// connection: the way back in when the proxy in front of CertMachine is
	// serving a bad certificate.
	CertMachineInsecure bool   `json:"certmachineInsecure"`
	ConfigPath          string `json:"configPath"`
	CertsDir          string `json:"certsDir"`
	CrtListPath       string `json:"crtListPath"`
	StatsSocketPath   string `json:"statsSocketPath"`
	BackupDir         string `json:"backupDir"`
	ServiceName       string `json:"serviceName"`
	BackupKeep        int    `json:"backupKeep"`
	ExpiryWarnDays    int    `json:"expiryWarnDays"`
}

// storedSettings is settings.json: a nil field means "not overridden".
type storedSettings struct {
	OS                *string `json:"os,omitempty"`
	CertMachineURL    *string `json:"certmachine_url,omitempty"`
	CertMachineCAFile *string `json:"certmachine_ca_file,omitempty"`
	CertMachineInsecure *bool  `json:"certmachine_insecure,omitempty"`
	ConfigPath        *string `json:"config_path,omitempty"`
	CertsDir          *string `json:"certs_dir,omitempty"`
	CrtListPath       *string `json:"crt_list_path,omitempty"`
	StatsSocketPath   *string `json:"stats_socket_path,omitempty"`
	BackupDir         *string `json:"backup_dir,omitempty"`
	ServiceName       *string `json:"service_name,omitempty"`
	BackupKeep        *int    `json:"backup_keep,omitempty"`
	ExpiryWarnDays    *int    `json:"expiry_warn_days,omitempty"`
	APIKey            *string `json:"api_key,omitempty"`
}

// baseSettings is the config-file block as effective starting values.
func baseSettings(cfg config.HaproxyConfig) Settings {
	t := strings.TrimSpace
	s := Settings{
		OS: strings.ToLower(t(cfg.OS)), CertMachineURL: t(cfg.CertMachine.URL), CertMachineCAFile: t(cfg.CertMachine.CAFile),
		ConfigPath: t(cfg.ConfigPath), CertsDir: t(cfg.CertsDir), CrtListPath: t(cfg.CrtListPath),
		StatsSocketPath: t(cfg.StatsSocketPath), BackupDir: t(cfg.BackupDir), ServiceName: t(cfg.ServiceName),
		BackupKeep: cfg.BackupKeep, ExpiryWarnDays: cfg.ExpiryWarnDays,
	}
	if s.OS == "" {
		s.OS = "auto"
	}
	if s.BackupKeep <= 0 {
		s.BackupKeep = config.DefaultHaproxyBackupKeep
	}
	if s.ExpiryWarnDays <= 0 {
		s.ExpiryWarnDays = config.DefaultHaproxyExpiryWarnDays
	}
	return s
}

// overlay returns base with every field settings.json holds applied, and the
// effective API key (the block's unless settings.json has one).
func (st storedSettings) overlay(base Settings, baseKey string) (Settings, string) {
	str := func(dst *string, v *string) {
		if v != nil {
			*dst = *v
		}
	}
	num := func(dst *int, v *int) {
		if v != nil {
			*dst = *v
		}
	}
	str(&base.OS, st.OS)
	str(&base.CertMachineURL, st.CertMachineURL)
	str(&base.CertMachineCAFile, st.CertMachineCAFile)
	if st.CertMachineInsecure != nil {
		base.CertMachineInsecure = *st.CertMachineInsecure
	}
	str(&base.ConfigPath, st.ConfigPath)
	str(&base.CertsDir, st.CertsDir)
	str(&base.CrtListPath, st.CrtListPath)
	str(&base.StatsSocketPath, st.StatsSocketPath)
	str(&base.BackupDir, st.BackupDir)
	str(&base.ServiceName, st.ServiceName)
	num(&base.BackupKeep, st.BackupKeep)
	num(&base.ExpiryWarnDays, st.ExpiryWarnDays)
	str(&baseKey, st.APIKey)
	return base, baseKey
}

// diffStored keeps only what differs from the config-file block.
func diffStored(base Settings, baseKey string, eff Settings, key string) storedSettings {
	var st storedSettings
	str := func(dst **string, b, e string) {
		if b != e {
			v := e
			*dst = &v
		}
	}
	num := func(dst **int, b, e int) {
		if b != e {
			v := e
			*dst = &v
		}
	}
	str(&st.OS, base.OS, eff.OS)
	str(&st.CertMachineURL, base.CertMachineURL, eff.CertMachineURL)
	str(&st.CertMachineCAFile, base.CertMachineCAFile, eff.CertMachineCAFile)
	if base.CertMachineInsecure != eff.CertMachineInsecure {
		v := eff.CertMachineInsecure
		st.CertMachineInsecure = &v
	}
	str(&st.ConfigPath, base.ConfigPath, eff.ConfigPath)
	str(&st.CertsDir, base.CertsDir, eff.CertsDir)
	str(&st.CrtListPath, base.CrtListPath, eff.CrtListPath)
	str(&st.StatsSocketPath, base.StatsSocketPath, eff.StatsSocketPath)
	str(&st.BackupDir, base.BackupDir, eff.BackupDir)
	str(&st.ServiceName, base.ServiceName, eff.ServiceName)
	num(&st.BackupKeep, base.BackupKeep, eff.BackupKeep)
	num(&st.ExpiryWarnDays, base.ExpiryWarnDays, eff.ExpiryWarnDays)
	str(&st.APIKey, baseKey, key)
	return st
}

// loadStored reads settings.json; a missing file is "nothing overridden".
func loadStored(dataDir string) (storedSettings, error) {
	var st storedSettings
	b, err := os.ReadFile(filepath.Join(dataDir, settingsFile))
	if errors.Is(err, os.ErrNotExist) {
		return st, nil
	}
	if err != nil {
		return st, fmt.Errorf("haproxy: reading %s: %w", settingsFile, err)
	}
	if err := json.Unmarshal(b, &st); err != nil {
		return st, fmt.Errorf("haproxy: %s is not valid JSON: %w", settingsFile, err)
	}
	return st, nil
}

// saveStored writes settings.json atomically with mode 0600 (it holds the key).
func saveStored(dataDir string, st storedSettings) error {
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(dataDir, settingsFile+".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dataDir, settingsFile))
}

// ---- validation ------------------------------------------------------------

var serviceNameRE = regexp.MustCompile(`^[A-Za-z0-9_.@-]+$`)

// validateCertMachine checks the URL (https, or http on loopback) and the CA
// file with the same rules the client enforces. Empty URL is fine (not
// configured).
func validateCertMachine(rawURL, caFile string) error {
	if caFile != "" {
		if _, err := os.ReadFile(caFile); err != nil {
			return fmt.Errorf("The CertMachine CA file %q cannot be read: %s.", caFile, plainOSError(err))
		}
		if _, err := CertMachineHTTPClient(CertMachineSettings{CAFile: caFile}); err != nil {
			return fmt.Errorf("The CertMachine CA file %q does not contain a usable certificate.", caFile)
		}
	}
	if rawURL == "" {
		return nil
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return errors.New("The CertMachine URL is not a valid address; use something like https://certmachine.example.com.")
	}
	if err := certMachineCheckTransport(u); err != nil {
		msg := strings.TrimPrefix(err.Error(), "haproxy: ")
		return errors.New(strings.ToUpper(msg[:1]) + msg[1:] + ".")
	}
	return nil
}

func plainOSError(err error) string {
	var pe *os.PathError
	if errors.As(err, &pe) {
		return pe.Err.Error()
	}
	return err.Error()
}

func validatePath(label, p string) error {
	if p == "" {
		return nil
	}
	for _, r := range p {
		if unicode.IsControl(r) {
			return fmt.Errorf("The %s must not contain control characters.", label)
		}
	}
	if !filepath.IsAbs(p) {
		return fmt.Errorf("The %s must be an absolute path (start with /).", label)
	}
	for _, part := range strings.Split(p, "/") {
		if part == ".." {
			return fmt.Errorf(`The %s must not contain "..".`, label)
		}
	}
	return nil
}

// validateSettings returns the first problem as a plain sentence, or nil.
func validateSettings(s Settings) error {
	switch s.OS {
	case "auto", "ubuntu", "rocky", "macos", "darwin":
	default:
		return errors.New("The operating system must be one of auto, ubuntu, rocky or macos.")
	}
	if err := validateCertMachine(s.CertMachineURL, s.CertMachineCAFile); err != nil {
		return err
	}
	for _, p := range []struct{ label, v string }{
		{"config path", s.ConfigPath}, {"certs directory", s.CertsDir}, {"crt-list path", s.CrtListPath},
		{"stats socket path", s.StatsSocketPath}, {"backup directory", s.BackupDir},
	} {
		if err := validatePath(p.label, p.v); err != nil {
			return err
		}
	}
	if s.ServiceName != "" && !serviceNameRE.MatchString(s.ServiceName) {
		return errors.New("The service name may only contain letters, digits and . _ @ -")
	}
	if s.BackupKeep < 1 || s.BackupKeep > 100 {
		return errors.New("The backup count must be between 1 and 100.")
	}
	if s.ExpiryWarnDays < 1 || s.ExpiryWarnDays > 365 {
		return errors.New("The expiry warning must be between 1 and 365 days.")
	}
	return nil
}

func (s Settings) trimmed() Settings {
	t := strings.TrimSpace
	s.OS = strings.ToLower(t(s.OS))
	s.CertMachineURL, s.CertMachineCAFile = t(s.CertMachineURL), t(s.CertMachineCAFile)
	s.ConfigPath, s.CertsDir, s.CrtListPath = t(s.ConfigPath), t(s.CertsDir), t(s.CrtListPath)
	s.StatsSocketPath, s.BackupDir, s.ServiceName = t(s.StatsSocketPath), t(s.BackupDir), t(s.ServiceName)
	return s
}

// ---- handlers --------------------------------------------------------------

func (s *server) handleSettingsGet(w http.ResponseWriter, r *http.Request) {
	rt := s.rt()
	defaults := map[string]string{}
	if rt.unavailable == "" {
		if d, err := s.mk(rt.driver.OSKind(), DriverOptions{}); err == nil {
			defaults = map[string]string{
				"configPath": d.ConfigPath(), "certsDir": d.CertsDir(), "crtListPath": d.CrtListPath(),
				"statsSocketPath": d.StatsSocketPath(), "backupDir": d.BackupDir(), "serviceName": d.ServiceName(),
			}
		}
	}
	response.WriteJSON(w, http.StatusOK, map[string]any{
		"effective":             rt.settings,
		"defaults":              defaults,
		"apiKeySet":             rt.apiKey != "",
		"certmachineConfigured": rt.client != nil,
		"unavailable":           rt.unavailable,
	})
}

func (s *server) handleSettingsPut(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Settings
		APIKey      string `json:"apiKey"`
		ClearAPIKey bool   `json:"clearApiKey"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	s.putMu.Lock()
	defer s.putMu.Unlock()
	cur := s.rt()
	set, key := req.Settings.trimmed(), cur.apiKey
	if req.APIKey != "" && req.ClearAPIKey {
		fail(w, http.StatusBadRequest, "Give a new API key or clear the stored one, not both.")
		return
	}
	if req.ClearAPIKey {
		key = ""
	} else if req.APIKey != "" {
		key = req.APIKey
	}
	if err := validateSettings(set); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	next, err := s.build(set, key)
	if err != nil {
		fail(w, http.StatusBadRequest, strings.TrimPrefix(err.Error(), "haproxy: "))
		return
	}
	base := baseSettings(s.cfg)
	if err := saveStored(s.dataDir, diffStored(base, s.cfg.CertMachine.APIKey, set, key)); err != nil {
		s.log.Addf("settings: saving failed: %v", err)
		fail(w, http.StatusInternalServerError, "Saving the settings failed; nothing was changed. See the operations log.")
		return
	}
	// Wait out an in-flight Apply on the old applier, then swap.
	if cur.applier != nil {
		_ = cur.applier.WithLock(func() error { s.swap(next); return nil })
	} else {
		s.swap(next)
	}
	s.log.Add("settings: saved and applied")
	response.WriteJSON(w, http.StatusOK, map[string]any{
		"ok": true, "needsRestart": false, "message": "Settings saved and applied.", "unavailable": next.unavailable,
	})
}

// handleSettingsTestConnection lists CertMachine's active certs. With no body it
// tests the current effective settings; an optional JSON body
// {certmachineUrl, certmachineCaFile, apiKey} tests unsaved form values instead
// (an empty or absent apiKey means the stored key). It never changes anything.
func (s *server) handleSettingsTestConnection(w http.ResponseWriter, r *http.Request) {
	cur := s.rt()
	u, ca, key := cur.settings.CertMachineURL, cur.settings.CertMachineCAFile, cur.apiKey
	var body struct {
		URL    *string `json:"certmachineUrl"`
		CAFile *string `json:"certmachineCaFile"`
		Insecure *bool `json:"certmachineInsecure"`
		APIKey string  `json:"apiKey"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil && !errors.Is(err, io.EOF) {
		response.WriteDecodeError(w, err)
		return
	}
	if body.URL != nil {
		u = strings.TrimSpace(*body.URL)
	}
	if body.CAFile != nil {
		ca = strings.TrimSpace(*body.CAFile)
	}
	insecure := cur.settings.CertMachineInsecure
	if body.Insecure != nil {
		insecure = *body.Insecure
	}
	if body.APIKey != "" {
		key = body.APIKey
	}
	reply := func(ok bool, class, msg string) {
		response.WriteJSON(w, http.StatusOK, map[string]any{"ok": ok, "class": class, "message": msg})
	}
	if u == "" {
		reply(false, "notConfigured", "CertMachine is not configured; enter its URL first.")
		return
	}
	if err := validateCertMachine(u, ca); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	client, err := CertMachineNewClient(CertMachineSettings{URL: u, APIKey: CertMachineAPIKey(key), CAFile: ca, InsecureSkipVerify: insecure})
	if err != nil {
		fail(w, http.StatusBadRequest, strings.TrimPrefix(err.Error(), "haproxy: "))
		return
	}
	ctx, cancel := reqCtx(r)
	defer cancel()
	certs, err := client.ListCerts(ctx, "", "active")
	switch {
	case err == nil:
		noun := "certs"
		if len(certs) == 1 {
			noun = "cert"
		}
		reply(true, "", fmt.Sprintf("Connected: %d active %s", len(certs), noun))
	case errors.Is(err, CertMachineErrUnauthorized):
		reply(false, "unauthorized", "CertMachine refused the API key (unauthorized).")
	case errors.Is(err, CertMachineErrTLS):
		reply(false, "tls", "TLS problem: the CertMachine certificate could not be verified. Check the CA file, or turn on Skip certificate check.")
	case errors.Is(err, CertMachineErrServer), errors.Is(err, CertMachineErrRefused):
		reply(false, "server", "CertMachine answered with a server error.")
	default:
		reply(false, "unreachable", "Cannot reach CertMachine at "+u+". Check the address and that it is running.")
	}
}
