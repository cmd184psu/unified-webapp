package todo_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"cmd184psu/unified-webapp/internal/todo"
)

func newTempStore(t *testing.T) *todo.Store {
	t.Helper()
	s, err := todo.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return s
}

// ── Subjects ─────────────────────────────────────────────────────────────────

func TestSubjects_EmptyDir(t *testing.T) {
	s := newTempStore(t)
	subs, err := s.Subjects()
	if err != nil {
		t.Fatalf("Subjects: %v", err)
	}
	if len(subs) != 0 {
		t.Errorf("want 0 subjects, got %d", len(subs))
	}
}

func TestSubjects_ListsDirectories(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "home"), 0755)
	os.MkdirAll(filepath.Join(dir, "work"), 0755)
	os.WriteFile(filepath.Join(dir, "home", "shopping.json"), []byte(`[]`), 0644)

	s, _ := todo.NewStore(dir)
	subs, err := s.Subjects()
	if err != nil {
		t.Fatalf("Subjects: %v", err)
	}
	if len(subs) != 2 {
		t.Fatalf("want 2 subjects, got %d", len(subs))
	}
	found := map[string]bool{}
	for _, sub := range subs {
		found[sub.Subject] = true
	}
	if !found["home"] || !found["work"] {
		t.Errorf("missing subjects: %v", found)
	}
}

func TestSubjects_IndexAlwaysFirst(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "home"), 0755)
	os.WriteFile(filepath.Join(dir, "home", "shopping.json"), []byte(`[]`), 0644)

	s, _ := todo.NewStore(dir)
	subs, _ := s.Subjects()
	if len(subs) != 1 {
		t.Fatalf("want 1 subject, got %d", len(subs))
	}
	if subs[0].Entries[0] != "home/index.json" {
		t.Errorf("first entry should be index.json, got %q", subs[0].Entries[0])
	}
	if len(subs[0].Entries) != 2 {
		t.Errorf("want 2 entries, got %d: %v", len(subs[0].Entries), subs[0].Entries)
	}
}

// ── GenerateIndex ─────────────────────────────────────────────────────────────

func TestGenerateIndex_ExcludesIndexJson(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "home"), 0755)
	os.WriteFile(filepath.Join(dir, "home", "index.json"), []byte(`{}`), 0644)
	os.WriteFile(filepath.Join(dir, "home", "shopping.json"), []byte(`[]`), 0644)

	s, _ := todo.NewStore(dir)
	data, err := s.GenerateIndex("home")
	if err != nil {
		t.Fatalf("GenerateIndex: %v", err)
	}
	var idx todo.IndexFile
	if err := json.Unmarshal(data, &idx); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if idx.Title != "Index of home" {
		t.Errorf("title: got %q", idx.Title)
	}
	if len(idx.List) != 1 {
		t.Fatalf("want 1 item, got %d: %v", len(idx.List), idx.List)
	}
	if idx.List[0].JSON != "home/shopping.json" {
		t.Errorf("json field: got %q", idx.List[0].JSON)
	}
}

func TestGenerateIndex_MissingDirectory(t *testing.T) {
	s := newTempStore(t)
	data, err := s.GenerateIndex("nosuchsubject")
	if err != nil {
		t.Fatalf("GenerateIndex on missing dir: %v", err)
	}
	var idx todo.IndexFile
	json.Unmarshal(data, &idx)
	if len(idx.List) != 0 {
		t.Errorf("want empty list, got %d items", len(idx.List))
	}
}

// ── ReadFile ──────────────────────────────────────────────────────────────────

func TestReadFile_ExistingFile(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "home"), 0755)
	want := []byte(`{"title":"test","list":[]}`)
	os.WriteFile(filepath.Join(dir, "home", "tasks.json"), want, 0644)

	s, _ := todo.NewStore(dir)
	got, err := s.ReadFile("home", "tasks.json")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestReadFile_CreatesWhenMissing(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "home"), 0755)

	s, _ := todo.NewStore(dir)
	data, err := s.ReadFile("home", "new.json")
	if err != nil {
		t.Fatalf("ReadFile missing: %v", err)
	}
	if len(data) == 0 {
		t.Error("expected non-empty content for new file")
	}
	// File should now exist on disk.
	if _, err := os.Stat(filepath.Join(dir, "home", "new.json")); err != nil {
		t.Errorf("file not created on disk: %v", err)
	}
}

// ── WriteFile ─────────────────────────────────────────────────────────────────

func TestWriteFile_Persists(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "home"), 0755)
	s, _ := todo.NewStore(dir)

	payload := []byte(`{"title":"saved","list":[]}`)
	if err := s.WriteFile("home", "saved.json", payload); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got, _ := os.ReadFile(filepath.Join(dir, "home", "saved.json"))
	if string(got) != string(payload) {
		t.Errorf("got %q, want %q", got, payload)
	}
}

func TestWriteFile_CreatesSubjectDir(t *testing.T) {
	s := newTempStore(t)
	if err := s.WriteFile("newsubject", "list.json", []byte(`[]`)); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

// ── MoveFile ──────────────────────────────────────────────────────────────────

func TestMoveFile_MovesFile(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "src"), 0755)
	os.WriteFile(filepath.Join(dir, "src", "list.json"), []byte(`[]`), 0644)

	s, _ := todo.NewStore(dir)
	if err := s.MoveFile("src", "list.json", "dst"); err != nil {
		t.Fatalf("MoveFile: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "dst", "list.json")); err != nil {
		t.Errorf("file not at destination: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "src", "list.json")); !os.IsNotExist(err) {
		t.Error("file still exists at source")
	}
}

func TestMoveFile_SameSubject_NoOp(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "home"), 0755)
	os.WriteFile(filepath.Join(dir, "home", "list.json"), []byte(`[]`), 0644)

	s, _ := todo.NewStore(dir)
	if err := s.MoveFile("home", "list.json", "home"); err != nil {
		t.Fatalf("MoveFile same subject: %v", err)
	}
}

// ── Concurrency ───────────────────────────────────────────────────────────────

func TestConcurrentWrites_NoRace(t *testing.T) {
	s := newTempStore(t)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			s.WriteFile("home", "list.json", []byte(`{"title":"t","list":[]}`))
		}(i)
	}
	wg.Wait()
}

func TestConcurrentDifferentSubjects_NoRace(t *testing.T) {
	s := newTempStore(t)
	var wg sync.WaitGroup
	subjects := []string{"alpha", "beta", "gamma"}
	for _, sub := range subjects {
		wg.Add(1)
		go func(subject string) {
			defer wg.Done()
			for i := 0; i < 10; i++ {
				s.WriteFile(subject, "list.json", []byte(`[]`))
				s.ReadFile(subject, "list.json")
			}
		}(sub)
	}
	wg.Wait()
}
