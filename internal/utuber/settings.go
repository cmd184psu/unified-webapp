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

	"cmd184psu/unified-webapp/internal/platform/config"
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
	mu            sync.RWMutex
	pythonBin     string // "" = no saved override
}

type settingsFile struct {
	PythonBin string `json:"python_bin"`
}

// newSettingsStore reads path if present. A missing file means no override;
// an unreadable or corrupt file is logged and ignored (settings are a UI
// convenience, unlike history.json which is load-bearing for dedup — so this
// is never a Build failure).
func newSettingsStore(path, configDefault string) *settingsStore {
	s := &settingsStore{path: path, configDefault: configDefault}
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
// default → the package-level default (defence in depth for a hand-built
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

func handleSettings(s *settingsStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(settingsFile{PythonBin: s.PythonBin()})
		case http.MethodPost:
			var f settingsFile
			if err := json.NewDecoder(r.Body).Decode(&f); err != nil {
				http.Error(w, "malformed JSON: "+err.Error(), http.StatusBadRequest)
				return
			}
			if err := s.SetPythonBin(f.PythonBin); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(settingsFile{PythonBin: s.PythonBin()})
		default:
			w.Header().Set("Allow", "GET, POST")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}
}
