package haproxy

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStateAtomic0600AndReload(t *testing.T) {
	dir := t.TempDir()
	s, err := NewModelStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Update(func(m *Model) {
		m.Services = append(m.Services, Service{ID: "s1", Name: "a", ExposedPort: 443, Upstream: Upstream{Host: "127.0.0.1", Port: 1}})
	}); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, want 0600", fi.Mode().Perm())
	}
	s2, err := NewModelStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := s2.Snapshot()
	if got.Version != 1 || len(got.Services) != 1 || got.Services[0].Name != "a" {
		t.Fatalf("reload mismatch: %+v", got)
	}
}
