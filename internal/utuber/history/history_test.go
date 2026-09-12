package history

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpenNewFile(t *testing.T) {
	dir := t.TempDir()
	l, err := Open(filepath.Join(dir, "history.json"))
	if err != nil {
		t.Fatal(err)
	}
	if l.Lookup("http://example.com") != nil {
		t.Fatal("expected nil for unseen URL")
	}
}

func TestRecordAndLookup(t *testing.T) {
	dir := t.TempDir()
	l, _ := Open(filepath.Join(dir, "history.json"))

	e := Entry{URL: "http://a.com", OutputFile: "a.mp4", ShowName: "Show A", Mode: "video"}
	if err := l.Record(e); err != nil {
		t.Fatal(err)
	}

	got := l.Lookup("http://a.com")
	if got == nil {
		t.Fatal("expected entry, got nil")
	}
	if got.OutputFile != "a.mp4" {
		t.Errorf("OutputFile: got %q, want %q", got.OutputFile, "a.mp4")
	}
	if got.Mode != "video" {
		t.Errorf("Mode: got %q, want %q", got.Mode, "video")
	}
}

func TestLookupMiss(t *testing.T) {
	dir := t.TempDir()
	l, _ := Open(filepath.Join(dir, "history.json"))
	l.Record(Entry{URL: "http://a.com", OutputFile: "a.mp4"})

	if l.Lookup("http://b.com") != nil {
		t.Fatal("expected nil for unknown URL")
	}
}

func TestPersistenceRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "history.json")

	l, _ := Open(path)
	l.Record(Entry{URL: "http://a.com", OutputFile: "a.mp4", ShowName: "A", Mode: "video"})
	l.Record(Entry{URL: "http://b.com", OutputFile: "b.mp3", ShowName: "B", Mode: "audio"})

	// reopen and verify both entries survive
	l2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ url, file string }{
		{"http://a.com", "a.mp4"},
		{"http://b.com", "b.mp3"},
	} {
		got := l2.Lookup(tc.url)
		if got == nil {
			t.Fatalf("missing entry for %s after reopen", tc.url)
		}
		if got.OutputFile != tc.file {
			t.Errorf("%s: OutputFile got %q, want %q", tc.url, got.OutputFile, tc.file)
		}
	}
}

func TestRecordOverwritesSameURL(t *testing.T) {
	dir := t.TempDir()
	l, _ := Open(filepath.Join(dir, "history.json"))

	l.Record(Entry{URL: "http://a.com", OutputFile: "old.mp4"})
	l.Record(Entry{URL: "http://a.com", OutputFile: "new.mp4"})

	got := l.Lookup("http://a.com")
	if got.OutputFile != "new.mp4" {
		t.Errorf("expected overwrite: got %q", got.OutputFile)
	}
}

func TestOpenCorruptFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "history.json")
	os.WriteFile(path, []byte("not json {{{{"), 0644)

	_, err := Open(path)
	if err == nil {
		t.Fatal("expected error for corrupt JSON, got nil")
	}
}
