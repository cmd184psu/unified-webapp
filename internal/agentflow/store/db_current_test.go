package store

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenCurrentNeverMigrates(t *testing.T) {
	dir := t.TempDir()
	if _, err := OpenCurrent(filepath.Join(dir, "missing.db")); err == nil {
		t.Fatal("a missing database must not be created")
	}
	p := filepath.Join(dir, "old.db")
	old, err := openWith(p, migrations[:len(migrations)-1])
	if err != nil {
		t.Fatal(err)
	}
	old.Close()
	if _, err := OpenCurrent(p); err == nil || !strings.Contains(err.Error(), "schema is version") {
		t.Fatalf("an older schema must be refused, not migrated: %v", err)
	}
	cur, err := Open(filepath.Join(dir, "cur.db"))
	if err != nil {
		t.Fatal(err)
	}
	cur.Close()
	st, err := OpenCurrent(filepath.Join(dir, "cur.db"))
	if err != nil {
		t.Fatal(err)
	}
	st.Close()
}
