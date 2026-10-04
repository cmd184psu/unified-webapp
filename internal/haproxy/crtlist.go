package haproxy

// The managed crt-list (FRD §6.7, D7, FR-H17). The TLS binds reference this
// file with `crt-list`; it lists one absolute path per ENABLED tracked cert.
// Disabling a cert leaves it out here while its file and tracking row remain
// (nothing is renamed or deleted to disable); enabling puts the line back.

import (
	"path/filepath"
	"sort"
	"strings"
)

// CrtListRender builds the managed crt-list text from tracking entries: one
// "<certsDir>/<name>" line per enabled cert, stably sorted by name so the
// output is deterministic (and diffs cleanly for pending-changes detection).
func CrtListRender(entries []CertEntry, certsDir string) string {
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.Enabled {
			names = append(names, e.Name)
		}
	}
	sort.Strings(names)
	var b strings.Builder
	for _, n := range names {
		b.WriteString(filepath.Join(certsDir, n))
		b.WriteByte('\n')
	}
	return b.String()
}
