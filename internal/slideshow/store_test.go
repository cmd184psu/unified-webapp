package slideshow_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"cmd184psu/unified-webapp/internal/slideshow"
)

func newTempStore(t *testing.T) (*slideshow.Store, string) {
	t.Helper()
	dir := t.TempDir()
	s, err := slideshow.NewStore(dir, 0)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return s, dir
}

// ── Subjects ─────────────────────────────────────────────────────────────────

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

func TestSubjects_ScansImageFiles(t *testing.T) {
	_, dir := newTempStore(t)
	os.MkdirAll(filepath.Join(dir, "beach"), 0755)
	os.WriteFile(filepath.Join(dir, "beach", "photo1.jpg"), []byte("img"), 0644)
	os.WriteFile(filepath.Join(dir, "beach", "photo2.png"), []byte("img"), 0644)
	os.WriteFile(filepath.Join(dir, "beach", "readme.txt"), []byte("txt"), 0644) // not an image

	s, _ := slideshow.NewStore(dir, 0)
	subs, err := s.Subjects()
	if err != nil {
		t.Fatalf("Subjects: %v", err)
	}
	if len(subs) != 1 {
		t.Fatalf("want 1 subject, got %d", len(subs))
	}
	if subs[0].Subject != "beach" {
		t.Errorf("subject name: got %q", subs[0].Subject)
	}
	if len(subs[0].Entries) != 2 {
		t.Errorf("want 2 image entries, got %d: %v", len(subs[0].Entries), subs[0].Entries)
	}
}

func TestSubjects_EntriesFormatted(t *testing.T) {
	_, dir := newTempStore(t)
	os.MkdirAll(filepath.Join(dir, "beach"), 0755)
	os.WriteFile(filepath.Join(dir, "beach", "photo1.jpg"), []byte("img"), 0644)

	s, _ := slideshow.NewStore(dir, 0)
	subs, _ := s.Subjects()
	if len(subs) == 0 || len(subs[0].Entries) == 0 {
		t.Fatal("expected at least one entry")
	}
	if subs[0].Entries[0] != "beach/photo1.jpg" {
		t.Errorf("entry format: got %q, want %q", subs[0].Entries[0], "beach/photo1.jpg")
	}
}

func TestSubjects_SkipsEmptyDirectories(t *testing.T) {
	_, dir := newTempStore(t)
	os.MkdirAll(filepath.Join(dir, "empty"), 0755) // no images inside

	s, _ := slideshow.NewStore(dir, 0)
	subs, _ := s.Subjects()
	if len(subs) != 0 {
		t.Errorf("empty subject dir should be excluded, got %d subjects", len(subs))
	}
}

func TestSubjects_MultipleSubjects(t *testing.T) {
	_, dir := newTempStore(t)
	for _, name := range []string{"alpha", "beta"} {
		os.MkdirAll(filepath.Join(dir, name), 0755)
		os.WriteFile(filepath.Join(dir, name, "a.jpg"), []byte("img"), 0644)
	}

	s, _ := slideshow.NewStore(dir, 0)
	subs, _ := s.Subjects()
	if len(subs) != 2 {
		t.Errorf("want 2 subjects, got %d", len(subs))
	}
}

// ── Blacklist filter ──────────────────────────────────────────────────────────

func TestSubjects_BlacklistPrefix(t *testing.T) {
	_, dir := newTempStore(t)
	// Blacklisted subject.
	os.MkdirAll(filepath.Join(dir, "_private"), 0755)
	os.WriteFile(filepath.Join(dir, "_private", "a.jpg"), []byte("img"), 0644)
	// Normal subject.
	os.MkdirAll(filepath.Join(dir, "public"), 0755)
	os.WriteFile(filepath.Join(dir, "public", "b.jpg"), []byte("img"), 0644)

	s, _ := slideshow.NewStore(dir, 0)
	subs, _ := s.Subjects()
	if len(subs) != 1 {
		t.Fatalf("want 1 subject (blacklisted excluded), got %d: %v", len(subs), subs)
	}
	if subs[0].Subject != "public" {
		t.Errorf("expected public, got %q", subs[0].Subject)
	}
}

// ── Age cutoff filter ─────────────────────────────────────────────────────────

func TestSubjects_AgeCutoff_ExcludesOld(t *testing.T) {
	_, dir := newTempStore(t)
	// Create a subject directory.
	subjDir := filepath.Join(dir, "old")
	os.MkdirAll(subjDir, 0755)
	os.WriteFile(filepath.Join(subjDir, "a.jpg"), []byte("img"), 0644)
	// Wind back its mtime to 10 days ago.
	past := time.Now().AddDate(0, 0, -10)
	os.Chtimes(subjDir, past, past)

	// Cutoff of 5 days: the 10-day-old directory should be excluded.
	s, _ := slideshow.NewStore(dir, 5)
	subs, _ := s.Subjects()
	if len(subs) != 0 {
		t.Errorf("want 0 subjects (old excluded), got %d", len(subs))
	}
}

func TestSubjects_AgeCutoff_IncludesRecent(t *testing.T) {
	_, dir := newTempStore(t)
	subjDir := filepath.Join(dir, "recent")
	os.MkdirAll(subjDir, 0755)
	os.WriteFile(filepath.Join(subjDir, "a.jpg"), []byte("img"), 0644)
	// mtime is now — within any reasonable cutoff.

	s, _ := slideshow.NewStore(dir, 5)
	subs, _ := s.Subjects()
	if len(subs) != 1 {
		t.Errorf("want 1 subject, got %d", len(subs))
	}
}

func TestSubjects_ZeroCutoff_IncludesAll(t *testing.T) {
	_, dir := newTempStore(t)
	subjDir := filepath.Join(dir, "ancient")
	os.MkdirAll(subjDir, 0755)
	os.WriteFile(filepath.Join(subjDir, "a.jpg"), []byte("img"), 0644)
	// Set mtime 1000 days ago.
	past := time.Now().AddDate(0, 0, -1000)
	os.Chtimes(subjDir, past, past)

	s, _ := slideshow.NewStore(dir, 0) // 0 = no cutoff
	subs, _ := s.Subjects()
	if len(subs) != 1 {
		t.Errorf("with zero cutoff want 1 subject, got %d", len(subs))
	}
}

// ── ImagePath ─────────────────────────────────────────────────────────────────

func TestImagePath_ValidJpg(t *testing.T) {
	s, dir := newTempStore(t)
	os.MkdirAll(filepath.Join(dir, "beach"), 0755)
	os.WriteFile(filepath.Join(dir, "beach", "photo.jpg"), []byte("img"), 0644)

	path, err := s.ImagePath("beach", "photo.jpg")
	if err != nil {
		t.Fatalf("ImagePath: %v", err)
	}
	want := filepath.Join(dir, "beach", "photo.jpg")
	if path != want {
		t.Errorf("got %q, want %q", path, want)
	}
}

func TestImagePath_InvalidExtension(t *testing.T) {
	s, _ := newTempStore(t)
	_, err := s.ImagePath("beach", "secret.txt")
	if err == nil {
		t.Error("expected error for non-image extension")
	}
}

func TestImagePath_DotDotBlocked(t *testing.T) {
	s, _ := newTempStore(t)
	_, err := s.ImagePath("..", "photo.jpg")
	if err == nil {
		t.Error("expected error for path traversal subject")
	}
}

func TestImagePath_AllImageExts(t *testing.T) {
	s, dir := newTempStore(t)
	os.MkdirAll(filepath.Join(dir, "x"), 0755)
	for _, ext := range []string{".jpg", ".jpeg", ".png", ".gif"} {
		fname := "img" + ext
		os.WriteFile(filepath.Join(dir, "x", fname), []byte("img"), 0644)
		if _, err := s.ImagePath("x", fname); err != nil {
			t.Errorf("ImagePath for %s: %v", ext, err)
		}
	}
}
