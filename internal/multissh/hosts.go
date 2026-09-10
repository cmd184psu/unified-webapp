package multissh

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"cmd184psu/unified-webapp/internal/multissh/sshproxy"
)

// hostConfig is the persisted host preset -- the only shape that reaches
// hosts_path. It has no password field, which is how a password is kept off
// disk (FR-H3, FR-N4): there is nothing to strip because there is nowhere to
// put one. Do not add a credential field here; see hostRequest.
type hostConfig struct {
	IP        string `json:"ip"`
	Port      int    `json:"port"`
	User      string `json:"user"`
	Key       string `json:"key"`
	RemoteDir string `json:"remoteDir"`
}

// hostRequest is the PUT /api/hosts wire shape. It is the only host type that
// carries a password, it is never written anywhere, and it never appears on the
// read-from-disk path.
type hostRequest struct {
	IP        string          `json:"ip"`
	Port      int             `json:"port"`
	User      string          `json:"user"`
	Key       string          `json:"key"`
	Password  sshproxy.Secret `json:"password"`
	RemoteDir string          `json:"remoteDir"`
}

type hostStore struct {
	mu          sync.Mutex
	path        string
	maxSessions int
	hosts       []hostConfig
}

func newHostStore(path string, maxSessions int) (*hostStore, error) {
	s := &hostStore{path: path, maxSessions: maxSessions, hosts: []hostConfig{}}
	if strings.TrimSpace(path) == "" {
		return s, nil
	}
	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		var payload struct {
			Hosts []hostConfig `json:"hosts"`
		}
		if err := json.Unmarshal(data, &payload); err != nil {
			return nil, fmt.Errorf("parse hosts file: %w", err)
		}
		normalized, err := normalizeHostConfigs(payload.Hosts)
		if err != nil {
			return nil, err
		}
		// An over-capacity file is accepted, not truncated: the operator may
		// have lowered max_sessions after saving more hosts, and rewriting the
		// file short would destroy those entries. The UI renders the first N.
		if len(normalized) > maxSessions {
			log.Printf("multissh: hosts file has %d entries, max_sessions is %d; extra entries preserved but not shown", len(normalized), maxSessions)
		}
		s.hosts = normalized
	case errors.Is(err, fs.ErrNotExist):
	default:
		return nil, fmt.Errorf("read hosts file: %w", err)
	}
	return s, nil
}

func (s *hostStore) list() []hostConfig {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]hostConfig, len(s.hosts))
	copy(out, s.hosts)
	return out
}

func (s *hostStore) set(hosts []hostRequest) error {
	normalized, err := normalizeHostRequests(hosts, s.maxSessions)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hosts = normalized
	if err := s.save(); err != nil {
		return err
	}
	return nil
}

func (s *hostStore) save() error {
	if strings.TrimSpace(s.path) == "" {
		return nil
	}
	dir := filepath.Dir(s.path)
	tmp, err := os.CreateTemp(dir, filepath.Base(s.path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp hosts file: %w", err)
	}
	tmpPath := tmp.Name()
	payload := struct {
		Hosts []hostConfig `json:"hosts"`
	}{Hosts: s.hosts}
	enc := json.NewEncoder(tmp)
	if err := enc.Encode(payload); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("encode hosts file: %w", err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("chmod temp hosts file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("close temp hosts file: %w", err)
	}
	if err := os.Rename(tmpPath, s.path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("rename hosts file: %w", err)
	}
	return nil
}

// normalizeHostRequests is the save path. It is also the password filter: it
// takes the only host type that carries a credential and returns the only host
// type that reaches disk, rebuilding each record field by field so the password
// is dropped by construction rather than by a step someone could forget to run.
// The count guard lives here because it belongs to the request path only -- a
// PUT over capacity is rejected, which is what leaves an over-capacity file on
// disk intact.
func normalizeHostRequests(hosts []hostRequest, maxSessions int) ([]hostConfig, error) {
	if len(hosts) > maxSessions {
		return nil, fmt.Errorf("hosts must contain at most %d entries", maxSessions)
	}
	out := make([]hostConfig, 0, len(hosts))
	for _, h := range hosts {
		cfg, err := normalizeHostFields(h.IP, h.Port, h.User, h.Key, h.RemoteDir)
		if err != nil {
			return nil, err
		}
		out = append(out, cfg)
	}
	return out, nil
}

// normalizeHostConfigs is the load path. It applies the same field rules with
// no count guard, so a hosts file written when max_sessions was larger still
// loads. hostRequest is deliberately not in scope here: nothing that can hold a
// password appears on the read-from-disk path.
func normalizeHostConfigs(hosts []hostConfig) ([]hostConfig, error) {
	out := make([]hostConfig, 0, len(hosts))
	for _, h := range hosts {
		cfg, err := normalizeHostFields(h.IP, h.Port, h.User, h.Key, h.RemoteDir)
		if err != nil {
			return nil, err
		}
		out = append(out, cfg)
	}
	return out, nil
}

// normalizeHostFields validates the key name and applies the port and
// remoteDir defaults. It is the only place a hostConfig is constructed, so both
// entry points above cannot drift apart.
func normalizeHostFields(ip string, port int, user, key, remoteDir string) (hostConfig, error) {
	key = strings.TrimSpace(key)
	if key != "" {
		if key == "." || key == ".." || key != filepath.Base(key) || strings.ContainsRune(key, '/') || strings.ContainsRune(key, filepath.Separator) {
			return hostConfig{}, fmt.Errorf("invalid key name")
		}
	}
	remoteDir = strings.TrimSpace(remoteDir)
	if remoteDir == "" {
		remoteDir = "/tmp"
	}
	if port <= 0 {
		port = 22
	}
	return hostConfig{
		IP:        ip,
		Port:      port,
		User:      user,
		Key:       key,
		RemoteDir: remoteDir,
	}, nil
}

func (s *Server) handleHostsGet(w http.ResponseWriter, r *http.Request) {
	if s.hosts == nil {
		writeError(w, http.StatusInternalServerError, "host storage unavailable")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"hosts": s.hosts.list()})
}

func (s *Server) handleHostsPut(w http.ResponseWriter, r *http.Request) {
	if s.hosts == nil {
		writeError(w, http.StatusInternalServerError, "host storage unavailable")
		return
	}
	var req struct {
		Hosts []hostRequest `json:"hosts"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := s.hosts.set(req.Hosts); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"hosts": s.hosts.list()})
}
