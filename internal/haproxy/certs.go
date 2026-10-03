package haproxy

// The cert tracking store (FRD §6.1, D16, FR-H10..H14). certs.json records only
// what the editor itself owns — the file name, enabled flag, a note, where the
// bytes came from (CertMachine id + fqdn) and the SHA-256 of the exact installed
// bytes. It holds NO copy of any cert detail (subject, SANs, issuer, expiry,
// status): those are read live from CertMachine (D15). The managed listing
// combines this store with the real certs directory; files that do not follow
// the naming convention are never touched, only counted as unmanaged.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// certStoreVersion is the certs.json schema version.
const certStoreVersion = 1

// CertSource records where a tracked file came from. It is deliberately just
// the CertMachine id and FQDN — never a detail.
type CertSource struct {
	ID   int64  `json:"id"`
	FQDN string `json:"fqdn"`
}

// CertEntry is one tracking row. These are the only fields certs.json holds.
type CertEntry struct {
	Name        string     `json:"name"`
	Enabled     bool       `json:"enabled"`
	Note        string     `json:"note"`
	CertMachine CertSource `json:"certmachine"`
	SHA256      string     `json:"sha256"`
	// Superseded is editor bookkeeping, not a cert detail: set only by
	// PullAndStage on the rows it disables, cleared when the owner re-enables
	// the row. Only disabled+Superseded rows are ever removed by cleanup.
	Superseded bool `json:"superseded,omitempty"`
	// Adopted marks a file that was already in the certs directory and was
	// imported as-is. The editor tracks it but never deletes the file.
	Adopted bool `json:"adopted,omitempty"`
}

// CertManaged is a tracked row combined with its on-disk presence.
type CertManaged struct {
	CertEntry
	Missing bool `json:"missing"`
}

// CertListing is the managed-certs view: the tracked rows plus a count of
// files in the directory that the editor does not own (FR-H10).
type CertListing struct {
	Certs     []CertManaged `json:"certs"`
	Unmanaged int           `json:"unmanaged"`
}

// CertStageResult is what PullAndStage reports: the installed file and the
// previously-tracked files for the same FQDN it supersedes (the Apply pipeline
// removes those only after a successful Apply).
type CertStageResult struct {
	Name       string   `json:"name"`
	Path       string   `json:"path"`
	SHA256     string   `json:"sha256"`
	Superseded []string `json:"superseded"`
}

// certStoreData is the on-disk shape of certs.json.
type certStoreData struct {
	Version int         `json:"version"`
	Dir     string      `json:"dir"`
	Certs   []CertEntry `json:"certs"`
}

// CertStore persists certs.json and combines it with the driver's view of the
// certs directory. Safe for concurrent use.
type CertStore struct {
	mu     sync.Mutex
	path   string
	driver Driver
}

// CertNewStore returns a store backed by <dataDir>/certs.json.
func CertNewStore(dataDir string, driver Driver) *CertStore {
	return &CertStore{path: filepath.Join(dataDir, "certs.json"), driver: driver}
}

// load reads certs.json, returning an empty store when the file is absent.
func (s *CertStore) load() (certStoreData, error) {
	data := certStoreData{Version: certStoreVersion, Dir: s.driver.CertsDir()}
	raw, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return data, nil
		}
		return data, fmt.Errorf("haproxy: read certs.json: %w", err)
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		return data, fmt.Errorf("haproxy: parse certs.json: %w", err)
	}
	return data, nil
}

// save writes certs.json atomically at mode 0600 (it names private key files).
func (s *CertStore) save(data certStoreData) error {
	data.Version = certStoreVersion
	if data.Dir == "" {
		data.Dir = s.driver.CertsDir()
	}
	raw, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("haproxy: encode certs.json: %w", err)
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return fmt.Errorf("haproxy: write certs.json: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("haproxy: replace certs.json: %w", err)
	}
	return nil
}

// List combines the tracking rows with the certs directory: each row is marked
// missing when its file is gone, and files that do not follow the convention
// are counted as unmanaged and otherwise left alone.
func (s *CertStore) List(ctx context.Context) (CertListing, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := s.load()
	if err != nil {
		return CertListing{}, err
	}
	files, err := s.driver.PrivilegedList(ctx, s.driver.CertsDir())
	if err != nil {
		return CertListing{}, fmt.Errorf("haproxy: list certs dir: %w", err)
	}
	tracked := map[string]bool{}
	for _, e := range data.Certs {
		tracked[e.Name] = true
	}
	present := map[string]bool{}
	unmanaged := 0
	for _, f := range files {
		if !NamingIsManaged(f.Name) && !tracked[f.Name] {
			unmanaged++
			continue
		}
		present[f.Name] = true
	}
	out := make([]CertManaged, 0, len(data.Certs))
	for _, e := range data.Certs {
		out = append(out, CertManaged{CertEntry: e, Missing: !present[e.Name]})
	}
	return CertListing{Certs: out, Unmanaged: unmanaged}, nil
}

// CrtList renders the managed crt-list from the currently enabled rows.
func (s *CertStore) CrtList() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := s.load()
	if err != nil {
		return "", err
	}
	return CrtListRender(data.Certs, s.driver.CertsDir()), nil
}

// SetEnabled toggles only a row's enabled flag; it never touches the file
// (FR-H13, D7).
func (s *CertStore) SetEnabled(name string, enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := s.load()
	if err != nil {
		return err
	}
	for i := range data.Certs {
		if data.Certs[i].Name == name {
			data.Certs[i].Enabled = enabled
			if enabled {
				data.Certs[i].Superseded = false
			}
			return s.save(data)
		}
	}
	return fmt.Errorf("haproxy: no tracked cert named %q", name)
}

// Remove deletes a cert's file and its row, refused while a service uses it
// (FR-H14). used reports whether any service references the name.
func (s *CertStore) Remove(ctx context.Context, driver Driver, name string, used func(name string) bool) error {
	if used != nil && used(name) {
		return fmt.Errorf("haproxy: cert %q is in use by a service; remove it from the service first", name)
	}
	if err := NamingValidateName(name); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := s.load()
	if err != nil {
		return err
	}
	idx := -1
	for i := range data.Certs {
		if data.Certs[i].Name == name {
			idx = i
			break
		}
	}
	if idx < 0 {
		return fmt.Errorf("haproxy: no tracked cert named %q", name)
	}
	if !data.Certs[idx].Adopted {
		if err := driver.PrivilegedRemove(ctx, filepath.Join(driver.CertsDir(), name)); err != nil {
			return fmt.Errorf("haproxy: remove cert file: %w", err)
		}
	}
	data.Certs = append(data.Certs[:idx], data.Certs[idx+1:]...)
	return s.save(data)
}

// PullAndStage pulls a cert's haproxy.pem (verified, FR-H45), names it from the
// verified bytes, writes it with the OS's CertMode, and records the tracking
// row. New bytes produce a NEW file name; the old same-FQDN rows are DISABLED
// here so the rendered crt-list swaps to the new cert only (FRD 6.4), but their
// files and rows are NOT removed (the Apply pipeline removes superseded files
// only after a successful Apply) — they are returned for that cleanup.
func (s *CertStore) PullAndStage(ctx context.Context, driver Driver, client CertMachinePuller, cert CertMachineCert, note string) (CertStageResult, error) {
	if cert.Fingerprint == nil || *cert.Fingerprint == "" {
		return CertStageResult{}, fmt.Errorf("haproxy: cert %d has no fingerprint to verify against", cert.ID)
	}
	body, err := client.PullHAProxyPEM(ctx, cert.ID, *cert.Fingerprint)
	if err != nil {
		return CertStageResult{}, err
	}

	name := NamingFileName(cert.FQDN, body)
	if err := NamingValidateName(name); err != nil {
		return CertStageResult{}, err
	}
	path := filepath.Join(driver.CertsDir(), name)
	if err := driver.PrivilegedWrite(ctx, path, body, driver.Ownership().CertMode); err != nil {
		return CertStageResult{}, fmt.Errorf("haproxy: write cert file: %w", err)
	}

	sum := sha256.Sum256(body)
	hexsum := hex.EncodeToString(sum[:])

	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := s.load()
	if err != nil {
		return CertStageResult{}, err
	}
	var superseded []string
	found := false
	for i := range data.Certs {
		if data.Certs[i].Name == name {
			data.Certs[i] = CertEntry{Name: name, Enabled: true, Note: note, CertMachine: CertSource{ID: cert.ID, FQDN: cert.FQDN}, SHA256: hexsum}
			found = true
			continue
		}
		// A row the owner disabled on purpose (not Superseded) is left alone.
		if strings.EqualFold(data.Certs[i].CertMachine.FQDN, cert.FQDN) && (data.Certs[i].Enabled || data.Certs[i].Superseded) {
			superseded = append(superseded, data.Certs[i].Name)
			// Swap the new cert in NOW (FRD 6.4): disable the superseded row so the
			// rendered/installed crt-list is the new cert only, never both. The row
			// and its file survive (disabled) until RemoveSuperseded runs after a
			// successful Apply, so a rolled-back-then-retried Apply still works.
			data.Certs[i].Enabled = false
			data.Certs[i].Superseded = true
		}
	}
	if !found {
		data.Certs = append(data.Certs, CertEntry{Name: name, Enabled: true, Note: note, CertMachine: CertSource{ID: cert.ID, FQDN: cert.FQDN}, SHA256: hexsum})
	}
	if err := s.save(data); err != nil {
		return CertStageResult{}, err
	}
	sort.Strings(superseded)
	return CertStageResult{Name: name, Path: path, SHA256: hexsum, Superseded: superseded}, nil
}

// RemoveSuperseded deletes the given files and their rows, the cleanup the
// Apply pipeline runs after a successful Apply (FR-H12 "never overwrite in
// place"; the old file lives until the new config is live). Only rows that are
// disabled and marked Superseded are removed; any other name is skipped.
func (s *CertStore) RemoveSuperseded(ctx context.Context, driver Driver, names []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := s.load()
	if err != nil {
		return err
	}
	eligible := map[string]bool{}
	for _, e := range data.Certs {
		if e.Superseded && !e.Enabled && !e.Adopted {
			eligible[e.Name] = true
		}
	}
	drop := map[string]bool{}
	for _, n := range names {
		if err := NamingValidateName(n); err != nil {
			return err
		}
		if !eligible[n] {
			continue
		}
		if err := driver.PrivilegedRemove(ctx, filepath.Join(driver.CertsDir(), n)); err != nil {
			return fmt.Errorf("haproxy: remove superseded cert: %w", err)
		}
		drop[n] = true
	}
	kept := data.Certs[:0]
	for _, e := range data.Certs {
		if !drop[e.Name] {
			kept = append(kept, e)
		}
	}
	data.Certs = kept
	return s.save(data)
}

// EnabledCount is how many tracked certs are enabled, i.e. how many lines the
// crt-list will have. HAProxy refuses a TLS bind with none.
func (s *CertStore) EnabledCount() (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := s.load()
	if err != nil {
		return 0, err
	}
	n := 0
	for _, e := range data.Certs {
		if e.Enabled {
			n++
		}
	}
	return n, nil
}

// EnabledFQDNs are the FQDNs of the enabled tracked certs, sorted and unique.
func (s *CertStore) EnabledFQDNs() ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := s.load()
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []string
	for _, e := range data.Certs {
		f := strings.ToLower(strings.TrimSpace(e.CertMachine.FQDN))
		if e.Enabled && f != "" && !seen[f] {
			seen[f] = true
			out = append(out, f)
		}
	}
	sort.Strings(out)
	return out, nil
}
