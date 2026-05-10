package menuserver_test

import (
	"os"
	"path/filepath"
	"testing"

	"cmd184psu/unified-webapp/internal/menuserver"
)

func newTempStore(t *testing.T) (*menuserver.Store, string) {
	t.Helper()
	dir := t.TempDir()
	s, err := menuserver.NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return s, dir
}

// ── Subjects ──────────────────────────────────────────────────────────────────

func TestSubjects_Empty(t *testing.T) {
	s, _ := newTempStore(t)
	subs, err := s.Subjects()
	if err != nil {
		t.Fatalf("Subjects: %v", err)
	}
	if len(subs) != 0 {
		t.Errorf("want 0 subjects, got %d", len(subs))
	}
}

func TestSubjects_ScansJsonFiles(t *testing.T) {
	_, dir := newTempStore(t)
	os.MkdirAll(filepath.Join(dir, "home"), 0755)
	os.WriteFile(filepath.Join(dir, "home", "networking.json"), []byte(`{}`), 0644)
	os.WriteFile(filepath.Join(dir, "home", "servers.json"), []byte(`{}`), 0644)
	os.WriteFile(filepath.Join(dir, "home", "readme.txt"), []byte("txt"), 0644) // not JSON

	s, _ := menuserver.NewStore(dir)
	subs, err := s.Subjects()
	if err != nil {
		t.Fatalf("Subjects: %v", err)
	}
	if len(subs) != 1 {
		t.Fatalf("want 1 subject, got %d", len(subs))
	}
	if subs[0].Subject != "home" {
		t.Errorf("subject: got %q", subs[0].Subject)
	}
	if len(subs[0].Entries) != 2 {
		t.Errorf("want 2 entries, got %d: %v", len(subs[0].Entries), subs[0].Entries)
	}
}

func TestSubjects_EntriesFormatted(t *testing.T) {
	_, dir := newTempStore(t)
	os.MkdirAll(filepath.Join(dir, "home"), 0755)
	os.WriteFile(filepath.Join(dir, "home", "networking.json"), []byte(`{}`), 0644)

	s, _ := menuserver.NewStore(dir)
	subs, _ := s.Subjects()
	if len(subs) == 0 || len(subs[0].Entries) == 0 {
		t.Fatal("expected at least one entry")
	}
	if subs[0].Entries[0] != "home/networking.json" {
		t.Errorf("entry format: got %q, want %q", subs[0].Entries[0], "home/networking.json")
	}
}

func TestSubjects_SkipsEmptyDirs(t *testing.T) {
	_, dir := newTempStore(t)
	os.MkdirAll(filepath.Join(dir, "empty"), 0755)

	s, _ := menuserver.NewStore(dir)
	subs, _ := s.Subjects()
	if len(subs) != 0 {
		t.Errorf("empty dir should be excluded, got %d subjects", len(subs))
	}
}

// ── ReadMenu ──────────────────────────────────────────────────────────────────

func TestReadMenu_ReturnsContent(t *testing.T) {
	_, dir := newTempStore(t)
	os.MkdirAll(filepath.Join(dir, "home"), 0755)
	want := []byte(`{"id":"net","title":"Networking","sites":[]}`)
	os.WriteFile(filepath.Join(dir, "home", "networking.json"), want, 0644)

	s, _ := menuserver.NewStore(dir)
	got, err := s.ReadMenu("home", "networking.json")
	if err != nil {
		t.Fatalf("ReadMenu: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestReadMenu_MissingFile(t *testing.T) {
	s, _ := newTempStore(t)
	_, err := s.ReadMenu("home", "nonexistent.json")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestReadMenu_PathTraversalBlocked(t *testing.T) {
	s, _ := newTempStore(t)
	_, err := s.ReadMenu("..", "passwd")
	if err == nil {
		t.Error("expected error for path traversal")
	}
}
