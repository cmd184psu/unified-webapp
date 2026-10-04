package config

import (
	"path/filepath"
	"testing"
)

// backup_dir is expanded like every other haproxy path (~ and $VARS), so the
// operator can write it the same way.
func TestHaproxyBackupDirIsExpanded(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	h := HaproxyConfig{BackupDir: "~/hp/backups"}
	if err := expandHaproxyPaths(&h); err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, "hp/backups"); h.BackupDir != want {
		t.Errorf("BackupDir = %q, want %q", h.BackupDir, want)
	}
}
