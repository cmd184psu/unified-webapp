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

// ── Age limit (by each image's own modified time) ──────────────────────────

// writeImage creates subject/name with its mtime set daysAgo days in the past.
func writeImage(t *testing.T, dir, subject, name string, daysAgo int) {
	t.Helper()
	sd := filepath.Join(dir, subject)
	if err := os.MkdirAll(sd, 0755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(sd, name)
	if err := os.WriteFile(p, []byte("img"), 0644); err != nil {
		t.Fatal(err)
	}
	when := time.Now().AddDate(0, 0, -daysAgo)
	if err := os.Chtimes(p, when, when); err != nil {
		t.Fatal(err)
	}
}

func TestSubjects_AgeCutoff_SkipsOldImagesKeepsNew(t *testing.T) {
	_, dir := newTempStore(t)
	writeImage(t, dir, "mixed", "old.jpg", 10)
	writeImage(t, dir, "mixed", "new.jpg", 1)

	s, _ := slideshow.NewStore(dir, 5)
	subs, _ := s.Subjects()
	if len(subs) != 1 || len(subs[0].Entries) != 1 || subs[0].Entries[0] != "mixed/new.jpg" {
		t.Errorf("want only mixed/new.jpg, got %+v", subs)
	}
}

func TestSubjects_AgeCutoff_SkipsSubjectWithOnlyOldImages(t *testing.T) {
	_, dir := newTempStore(t)
	writeImage(t, dir, "old", "a.jpg", 10)
	writeImage(t, dir, "recent", "b.jpg", 0)

	s, _ := slideshow.NewStore(dir, 5)
	subs, _ := s.Subjects()
	if len(subs) != 1 || subs[0].Subject != "recent" {
		t.Errorf("want only the recent subject, got %+v", subs)
	}
}

func TestSubjects_ZeroCutoff_IncludesAll(t *testing.T) {
	_, dir := newTempStore(t)
	writeImage(t, dir, "ancient", "a.jpg", 1000)

	s, _ := slideshow.NewStore(dir, 0) // 0 = no limit
	subs, _ := s.Subjects()
	if len(subs) != 1 {
		t.Errorf("with no limit want 1 subject, got %d", len(subs))
	}
}

func TestSubjectsWithin_OverridesDefault(t *testing.T) {
	_, dir := newTempStore(t)
	writeImage(t, dir, "s", "a.jpg", 10)

	s, _ := slideshow.NewStore(dir, 5)
	if subs, _ := s.SubjectsWithin(30); len(subs) != 1 {
		t.Errorf("a 30-day limit should include a 10-day-old image, got %d subjects", len(subs))
	}
	if subs, _ := s.SubjectsWithin(0); len(subs) != 1 {
		t.Errorf("0 should mean no limit, got %d subjects", len(subs))
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
