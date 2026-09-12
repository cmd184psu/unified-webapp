// Package fspath provides shared filesystem-safety helpers: validating a
// single path-component name and confining a relative path so it cannot
// escape a root directory. It consolidates logic that used to be
// duplicated (nearly verbatim) across several modules.
package fspath

import (
	"fmt"
	"path/filepath"
	"strings"
)

// ValidName reports whether name is a safe single-path-component identifier:
// non-empty, not dot-prefixed, and free of path separators.
func ValidName(name string) bool {
	return name != "" &&
		!strings.HasPrefix(name, ".") &&
		!strings.Contains(name, "/") &&
		!strings.Contains(name, "\\")
}

// ConfineTo joins root and rel, then verifies the resulting path does not
// escape root. It returns the joined path (in root's own absolute/relative
// form) on success, or an error if rel would resolve outside root.
func ConfineTo(root, rel string) (string, error) {
	joined := filepath.Join(root, rel)
	relPath, err := filepath.Rel(root, joined)
	if err != nil || relPath == ".." || strings.HasPrefix(relPath, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes root")
	}
	return joined, nil
}
