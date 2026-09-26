package sshclient

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// KeyFile describes one entry the frontend file picker may select. Only the
// base name is exposed; the absolute path never leaves the server.
type KeyFile struct {
	Name  string `json:"name"`
	IsDir bool   `json:"isDir"`
}

// DefaultSSHDir returns the SSH directory of the user running the server
// (~/.ssh, i.e. /root/.ssh when running as root). It does not create or read
// the directory; callers decide how to handle a missing one.
func DefaultSSHDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("sshclient: resolve home dir: %w", err)
	}
	return filepath.Join(home, ".ssh"), nil
}

// ListKeys returns the regular files directly inside dir, sorted by name. The
// picker is intentionally non-navigable: subdirectories are reported (IsDir)
// only so the UI can grey them out, never to descend into them. Public-key
// (.pub) and bookkeeping files are still listed so the user can see the full
// directory, but the UI is expected to highlight private keys.
func ListKeys(dir string) ([]KeyFile, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("sshclient: read ssh dir: %w", err)
	}
	keys := make([]KeyFile, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		keys = append(keys, KeyFile{Name: name, IsDir: e.IsDir()})
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].Name < keys[j].Name })
	return keys, nil
}

// ResolveKeyPath turns a picker-selected base name into an absolute path that
// is provably inside dir. It rejects empty names, names containing a path
// separator, and any value that would escape dir (traversal). The returned
// path is verified to exist and be a regular file.
func ResolveKeyPath(dir, name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("sshclient: empty key name")
	}
	// A legitimate selection is a single base name. Reject anything that
	// carries directory structure or traversal up front.
	if name != filepath.Base(name) || strings.ContainsRune(name, '/') || strings.ContainsRune(name, filepath.Separator) {
		return "", fmt.Errorf("sshclient: invalid key name %q", name)
	}
	full := filepath.Join(dir, name)

	// Defense in depth: confirm the cleaned path is still rooted at dir.
	rel, err := filepath.Rel(dir, full)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || strings.ContainsRune(rel, filepath.Separator) {
		return "", fmt.Errorf("sshclient: key name %q escapes ssh dir", name)
	}

	info, err := os.Stat(full)
	if err != nil {
		return "", fmt.Errorf("sshclient: key %q: %w", name, err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("sshclient: key %q is not a regular file", name)
	}
	return full, nil
}
