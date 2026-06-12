package obsidianoid_test

import (
	"os"
	"path/filepath"
	"testing"

	"cmd184psu/unified-webapp/internal/obsidianoid"
)

func TestReadThreadsMissingFiles(t *testing.T) {
	root := t.TempDir()
	states := []obsidianoid.ThreadState{{Disabled: false}, {Disabled: true}, {Disabled: false}, {Disabled: false}}
	threads, err := obsidianoid.ReadThreads(root, "Threads", 4, states)
	if err != nil {
		t.Fatalf("ReadThreads with missing files: %v", err)
	}
	if len(threads) != 4 {
		t.Fatalf("expected 4 threads, got %d", len(threads))
	}
	if threads[1].Disabled != true {
		t.Error("thread[1] should be disabled")
	}
}

func TestWriteAndReadThreads(t *testing.T) {
	root := t.TempDir()
	input := []obsidianoid.Thread{
		{Content: "# Thread 1", Disabled: false},
		{Content: "# Thread 2", Disabled: true},
		{Content: "", Disabled: false},
		{Content: "# Thread 4", Disabled: false},
	}
	if err := obsidianoid.WriteThreads(root, "Threads", input); err != nil {
		t.Fatalf("WriteThreads: %v", err)
	}

	// Verify files exist on disk.
	for i := 1; i <= 4; i++ {
		p := filepath.Join(root, "Threads", obsidianoid.ThreadFileName(i-1))
		if _, err := os.Stat(p); err != nil {
			t.Errorf("thread file %d missing: %v", i, err)
		}
	}

	states := make([]obsidianoid.ThreadState, 4)
	states[1].Disabled = true
	threads, err := obsidianoid.ReadThreads(root, "Threads", 4, states)
	if err != nil {
		t.Fatalf("ReadThreads: %v", err)
	}
	if threads[0].Content != "# Thread 1" {
		t.Errorf("thread[0] content mismatch: %q", threads[0].Content)
	}
	if threads[1].Disabled != true {
		t.Error("thread[1] should be disabled")
	}
}
