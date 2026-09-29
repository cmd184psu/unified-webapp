package utuber

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"cmd184psu/unified-webapp/internal/platform/config"
	"cmd184psu/unified-webapp/internal/taskmaster/golane"
)

// pythonBinPattern accepts a bare command name or path — no spaces, no shell
// metacharacters (FR-8). The value is passed as argv[0] to the executor,
// never through a shell.
var pythonBinPattern = regexp.MustCompile(`^[A-Za-z0-9._/-]+$`)

// settingsStore holds the UI-editable settings, persisted server-side at
// <download_dir>/settings.json so they survive across browsers and devices.
type settingsStore struct {
	path          string // <download_dir>/settings.json
	configDefault string // cfg.PythonBin, already defaulted by config.Load
	cookiesPath   string // resolved utuber.cookies_file path (D5 Fix 2)
	mu            sync.RWMutex
	pythonBin     string // "" = no saved override
}

type settingsFile struct {
	PythonBin string `json:"python_bin"`
}

// newSettingsStore reads path if present. A missing file means no override;
// an unreadable or corrupt file is logged and ignored (settings are a UI
// convenience, unlike history.json which is load-bearing for dedup — so this
// is never a Build failure). cookiesPath is the resolved location of the
// Netscape-format cookie jar (D5 Fix 2); the cookie file itself is never
// stored inside settings.json.
func newSettingsStore(path, configDefault, cookiesPath string) *settingsStore {
	s := &settingsStore{path: path, configDefault: configDefault, cookiesPath: cookiesPath}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return s
	}
	if err != nil {
		log.Printf("utuber: ignoring unreadable %s: %v", path, err)
		return s
	}
	var f settingsFile
	if err := json.Unmarshal(data, &f); err != nil {
		log.Printf("utuber: ignoring unreadable %s: %v", path, err)
		return s
	}
	s.pythonBin = f.PythonBin
	return s
}

// PythonBin resolves the effective interpreter: saved override → config
// default → the package-level default (defense in depth for a hand-built
// config that skipped Load).
func (s *settingsStore) PythonBin() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.pythonBin != "" {
		return s.pythonBin
	}
	if s.configDefault != "" {
		return s.configDefault
	}
	return config.DefaultUtuberPythonBin
}

// SetPythonBin validates and persists v. A blank value clears the override:
// the file is removed (not rewritten empty) so the clear survives a restart
// and settings.json drops out of the download dir's newest-file scan. The
// in-memory value changes only after the disk operation succeeds.
func (s *settingsStore) SetPythonBin(v string) error {
	v = strings.TrimSpace(v)
	if v == "" {
		if err := os.Remove(s.path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("clearing %s: %w", s.path, err)
		}
		s.mu.Lock()
		s.pythonBin = ""
		s.mu.Unlock()
		log.Printf("utuber: python interpreter override cleared; using %s", s.PythonBin())
		return nil
	}
	if !pythonBinPattern.MatchString(v) {
		return fmt.Errorf("invalid python interpreter %q: only letters, digits, '.', '_', '/' and '-' are allowed", v)
	}
	data, err := json.Marshal(settingsFile{PythonBin: v})
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return err
	}
	s.mu.Lock()
	s.pythonBin = v
	s.mu.Unlock()
	log.Printf("utuber: python interpreter set to %s", v)
	return nil
}

// CookiesPath returns the resolved cookie-jar path (D5 Fix 2). Safe to call
// on a nil receiver: tests that build a processor directly (without going
// through Build/buildWith) never set a settingsStore, and a nil store simply
// means "no cookies configured".
func (s *settingsStore) CookiesPath() string {
	if s == nil {
		return ""
	}
	return s.cookiesPath
}

// cookiesInfo reports whether the cookie jar currently exists on disk and,
// if so, its mtime -- used for the GET /settings.json "cookies_configured"
// and "cookies_updated_at" fields. It never reads the file's contents.
func (s *settingsStore) cookiesInfo() (bool, *time.Time) {
	if s == nil || s.cookiesPath == "" {
		return false, nil
	}
	fi, err := os.Stat(s.cookiesPath)
	if err != nil {
		return false, nil
	}
	t := fi.ModTime()
	return true, &t
}

// SetCookiesText validates v as Netscape-format cookie text and writes it to
// s.cookiesPath (0600, atomic tmp+rename, mirroring SetPythonBin). A blank
// (whitespace-only) v clears the jar by removing the file instead of writing
// an empty one -- deleting it must return the module to cookie-less behavior
// with no config error, matching SetPythonBin's blank-clears convention.
// Invalid, non-blank text is rejected before anything on disk changes, so a
// bad save leaves any existing jar untouched.
func (s *settingsStore) SetCookiesText(v string) error {
	if strings.TrimSpace(v) == "" {
		if s.cookiesPath == "" {
			return nil
		}
		if err := os.Remove(s.cookiesPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("clearing %s: %w", s.cookiesPath, err)
		}
		log.Printf("utuber: cookie jar cleared")
		return nil
	}
	if s.cookiesPath == "" {
		return fmt.Errorf("cookies_file is not configured")
	}
	if err := validateNetscapeCookies(v); err != nil {
		return err
	}
	tmp := s.cookiesPath + ".tmp"
	if err := os.WriteFile(tmp, []byte(v), 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.cookiesPath); err != nil {
		return err
	}
	log.Printf("utuber: cookie jar updated (%d bytes)", len(v))
	return nil
}

// validateNetscapeCookies checks that v looks like a Netscape-format cookie
// file: every non-blank line is either a comment (starts with "#", except
// the "#HttpOnly_"-prefixed lines some exporters use to mark HttpOnly
// cookies, which are data lines) or has exactly 7 tab-separated fields
// (domain, includeSubdomains, path, secure, expiry, name, value), and at
// least one data line is present.
func validateNetscapeCookies(v string) error {
	dataLines := 0
	for _, raw := range strings.Split(v, "\n") {
		line := strings.TrimRight(raw, "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "#") && !strings.HasPrefix(trimmed, "#HttpOnly_") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 7 {
			return fmt.Errorf("invalid cookie file: expected 7 tab-separated fields, got %d on line %q", len(fields), trimmed)
		}
		dataLines++
	}
	if dataLines == 0 {
		return fmt.Errorf("invalid cookie file: no cookie entries found")
	}
	return nil
}

// settingsResponse is the fixed 9-key GET/POST reply (D8's 7 plus D5's
// cookies_configured/cookies_updated_at). python_bin lives in the on-disk
// settings.json (settingsStore); age_out_days through brake_engaged are
// lane-DB state. queue_paused_by, brake_engaged, cookies_configured and
// cookies_updated_at are all read-only; cookies_updated_at is nil until a
// cookie jar exists. The cookie file's contents are never returned.
type settingsResponse struct {
	PythonBin           string     `json:"python_bin"`
	AgeOutDays          int        `json:"age_out_days"`
	ConcurrentDownloads int        `json:"concurrent_downloads"`
	ShowInTaskmaster    bool       `json:"show_in_taskmaster"`
	QueuePaused         bool       `json:"queue_paused"`
	QueuePausedBy       string     `json:"queue_paused_by"`
	BrakeEngaged        bool       `json:"brake_engaged"`
	CookiesConfigured   bool       `json:"cookies_configured"`
	CookiesUpdatedAt    *time.Time `json:"cookies_updated_at"`
}

// settingsPatch is the POST body. Every field is an optional pointer so an
// absent field is left untouched, while a present one (including an empty
// python_bin, which clears the override — N6) is applied. queue_paused_by and
// brake_engaged are read-only: they are ignored if sent. CookiesTxt is
// write-only (D5 Fix 2): a present, non-blank value validates and saves the
// cookie jar; a present, blank value clears it; absent leaves it untouched.
type settingsPatch struct {
	PythonBin           *string `json:"python_bin"`
	AgeOutDays          *int    `json:"age_out_days"`
	ConcurrentDownloads *int    `json:"concurrent_downloads"`
	ShowInTaskmaster    *bool   `json:"show_in_taskmaster"`
	QueuePaused         *bool   `json:"queue_paused"`
	CookiesTxt          *string `json:"cookies_txt"`
}

func handleSettings(s *settingsStore, lane golane.Lane) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			writeSettings(w, s, lane)
		case http.MethodPost:
			var patch settingsPatch
			if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
				http.Error(w, "malformed JSON: "+err.Error(), http.StatusBadRequest)
				return
			}

			// Validate every field before applying anything, so a bad field
			// leaves all settings untouched (naming the offending field).
			if patch.PythonBin != nil {
				if v := strings.TrimSpace(*patch.PythonBin); v != "" && !pythonBinPattern.MatchString(v) {
					http.Error(w, "invalid python_bin", http.StatusBadRequest)
					return
				}
			}
			if patch.AgeOutDays != nil && (*patch.AgeOutDays < 1 || *patch.AgeOutDays > 365) {
				http.Error(w, "invalid age_out_days: must be between 1 and 365", http.StatusBadRequest)
				return
			}
			if patch.ConcurrentDownloads != nil && (*patch.ConcurrentDownloads < 1 || *patch.ConcurrentDownloads > config.MaxUtuberWorkers) {
				http.Error(w, fmt.Sprintf("invalid concurrent_downloads: must be between 1 and %d", config.MaxUtuberWorkers), http.StatusBadRequest)
				return
			}
			if patch.CookiesTxt != nil && strings.TrimSpace(*patch.CookiesTxt) != "" {
				if err := validateNetscapeCookies(*patch.CookiesTxt); err != nil {
					http.Error(w, "invalid cookies_txt: "+err.Error(), http.StatusBadRequest)
					return
				}
			}

			// Apply the lane-DB fields from the present pointers.
			lp := golane.SettingsPatch{}
			if patch.ConcurrentDownloads != nil {
				lp.Width = patch.ConcurrentDownloads
			}
			if patch.AgeOutDays != nil {
				lp.RetentionDays = patch.AgeOutDays
			}
			if patch.QueuePaused != nil {
				lp.Paused = patch.QueuePaused
			}
			if patch.ShowInTaskmaster != nil {
				hidden := !*patch.ShowInTaskmaster
				lp.Hidden = &hidden
			}
			if _, err := lane.UpdateSettings(lp); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			// python_bin is applied only when the pointer is present. If its
			// file write fails after the lane patch already committed, the
			// settings are partially saved.
			if patch.PythonBin != nil {
				if err := s.SetPythonBin(*patch.PythonBin); err != nil {
					http.Error(w, "settings partially saved: "+err.Error(), http.StatusInternalServerError)
					return
				}
			}
			if patch.CookiesTxt != nil {
				if err := s.SetCookiesText(*patch.CookiesTxt); err != nil {
					http.Error(w, "settings partially saved: "+err.Error(), http.StatusInternalServerError)
					return
				}
			}
			writeSettings(w, s, lane)
		default:
			w.Header().Set("Allow", "GET, POST")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}
}

func writeSettings(w http.ResponseWriter, s *settingsStore, lane golane.Lane) {
	ls, err := lane.Settings()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	configured, mtime := s.cookiesInfo()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(settingsResponse{
		PythonBin:           s.PythonBin(),
		AgeOutDays:          ls.RetentionDays,
		ConcurrentDownloads: ls.Width,
		ShowInTaskmaster:    !ls.Hidden,
		QueuePaused:         ls.Paused,
		QueuePausedBy:       ls.PausedBy,
		BrakeEngaged:        ls.BrakeEngaged,
		CookiesConfigured:   configured,
		CookiesUpdatedAt:    mtime,
	})
}
