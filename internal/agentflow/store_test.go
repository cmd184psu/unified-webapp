package agentflow

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestGetQueue(t *testing.T) {
	dir := t.TempDir()

	queueFile := filepath.Join(dir, ".agentflow", "queue.json")
	if err := os.MkdirAll(filepath.Dir(queueFile), 0o755); err != nil {
		t.Fatal(err)
	}
	wantTasks := []Task{
		{ID: "foo", Title: "First task", Status: "ready", Attempts: 1},
		{ID: "bar", Title: "Second task", Status: "passed", Attempts: 2},
	}
	wantData := QueueData{Tasks: wantTasks}
	if err := os.WriteFile(queueFile, mustMarshal(t, wantData), 0o644); err != nil {
		t.Fatal(err)
	}

	s := NewStore(dir)
	got, err := s.GetQueue()
	if err != nil {
		t.Fatalf("GetQueue: %v", err)
	}

	tasks := got.Tasks
	if len(tasks) != 2 {
		t.Fatalf("got %d tasks, want 2", len(tasks))
	}
	if tasks[0].ID != "foo" || tasks[1].Status != "passed" {
		t.Fatalf("tasks = %+v, want [{id:foo,...},{...,status:passed}]", tasks)
	}

	// --- file does not exist: error returned ---
	empty := t.TempDir()
	sEmpty := NewStore(empty)
	if _, err := sEmpty.GetQueue(); err == nil {
		t.Error("expected error for missing queue.json, got nil")
	}
}

func TestGetProgress_DerivedFromQueue(t *testing.T) {
	dir := t.TempDir()

	queueFile := filepath.Join(dir, ".agentflow", "queue.json")
	if err := os.MkdirAll(filepath.Dir(queueFile), 0o755); err != nil {
		t.Fatal(err)
	}
	wantData := QueueData{
		Tasks: []Task{
			{ID: "t1", Title: "", Status: "ready"},
			{ID: "t2", Title: "", Status: "passed"},
			{ID: "t3", Title: "", Status: "blocked"},
			{ID: "t4", Title: "", Status: "review"},
			{ID: "t5", Title: "", Status: "repair"},
			{ID: "t6", Title: "", Status: "failed"},
			{ID: "t7", Title: "", Status: "ready"},
		},
	}
	if err := os.WriteFile(queueFile, mustMarshal(t, wantData), 0o644); err != nil {
		t.Fatal(err)
	}

	s := NewStore(dir)
	got, err := s.GetProgress()
	if err != nil {
		t.Fatalf("GetProgress: %v", err)
	}
	if got.Total != 7 {
		t.Fatalf("total = %d, want 7", got.Total)
	}
	if got.Ready != 2 || got.Passed != 1 || got.Blocked != 1 ||
		got.Review != 1 || got.Repair != 1 || got.Failed != 1 {
		t.Fatalf("counts = %+v", got)
	}
}

func TestGetProgress_EmptyQueueReturnsZeroCounts(t *testing.T) {
	dir := t.TempDir()

	queueFile := filepath.Join(dir, ".agentflow", "queue.json")
	if err := os.MkdirAll(filepath.Dir(queueFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(queueFile, mustMarshal(t, QueueData{Tasks: []Task{}}), 0o644); err != nil {
		t.Fatal(err)
	}

	s := NewStore(dir)
	got, err := s.GetProgress()
	if err != nil {
		t.Fatalf("GetProgress: %v", err)
	}
	if got.Total != 0 || got.Passed != 0 {
		t.Fatalf("expected zero counts for empty queue, got %+v", got)
	}
}

func TestGetProgress_MissingQueueFileReturnsZeroCounts(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	got, err := s.GetProgress()
	if err != nil {
		t.Fatalf("expected no error for missing queue.json, got: %v", err)
	}
	if got.Total != 0 {
		t.Fatalf("expected zero total when queue.json is absent, got %+v", got)
	}
}

func TestGetQueue_MalformedJSON(t *testing.T) {
	dir := t.TempDir()
	queueFile := filepath.Join(dir, ".agentflow", "queue.json")
	if err := os.MkdirAll(filepath.Dir(queueFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(queueFile, []byte("{not valid json"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := NewStore(dir)
	if _, err := s.GetQueue(); err == nil {
		t.Error("expected error for malformed queue.json, got nil")
	}
}

func TestNewStore_DefaultsToDefaultAgentflowProjectPath(t *testing.T) {
	s := NewStore("")
	if s.ProjectPath != DefaultAgentflowProjectPath {
		t.Fatalf("default path = %q, want %q", s.ProjectPath, DefaultAgentflowProjectPath)
	}
}

func mustMarshal(t *testing.T, v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return b
}
