package slideshow_test

import (
	"os"
	"path/filepath"
	"testing"

	"cmd184psu/unified-webapp/internal/slideshow"
)

func newTempStore(t *testing.T) (*slideshow.Store, string) {
	t.Helper()
	dir := t.TempDir()
	s, err := slideshow.NewStore(dir)
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

	s, _ := slideshow.NewStore(dir)
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

	s, _ := slideshow.NewStore(dir)
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

	s, _ := slideshow.NewStore(dir)
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

	s, _ := slideshow.NewStore(dir)
	subs, _ := s.Subjects()
	if len(subs) != 2 {
		t.Errorf("want 2 subjects, got %d", len(subs))
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
