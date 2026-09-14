package smbedit

import (
	"os"
	"path/filepath"
	"testing"

	"go.uber.org/goleak"
)

// Ported from smbed's internal/api/picker_test.go.
func TestListOptFolders_ReturnsDirectories(t *testing.T) {
	defer goleak.VerifyNone(t)
	dir := t.TempDir()

	for _, sub := range []string{"media", "data", ".hidden"} {
		if err := os.Mkdir(filepath.Join(dir, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// Create a file — should NOT appear.
	if err := os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}

	entries, err := ListFolders(dir)
	if err != nil {
		t.Fatalf("ListFolders() error: %v", err)
	}

	names := map[string]bool{}
	for _, e := range entries {
		names[e.Name] = true
	}

	if names[".hidden"] {
		t.Error("hidden directories should be excluded")
	}
	if !names["media"] || !names["data"] {
		t.Errorf("expected 'media' and 'data', got %v", names)
	}
	if names["readme.txt"] {
		t.Error("files should not appear in folder listing")
	}
}

func TestListFolders_EmptyRootDefaultsToOpt(t *testing.T) {
	defer goleak.VerifyNone(t)
	if _, err := os.Stat(defaultPickerRoot); err != nil {
		t.Skipf("%s not available on this machine: %v", defaultPickerRoot, err)
	}

	got, gotErr := ListFolders("")
	want, wantErr := ListFolders(defaultPickerRoot)
	if (gotErr == nil) != (wantErr == nil) {
		t.Fatalf("error mismatch: empty root err = %v, %s err = %v", gotErr, defaultPickerRoot, wantErr)
	}
	if len(got) != len(want) {
		t.Fatalf("empty root returned %d entries, %s returned %d", len(got), defaultPickerRoot, len(want))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("entry %d: empty root %+v != explicit root %+v", i, got[i], want[i])
		}
	}
}

func TestListFolders_MissingRootIsError(t *testing.T) {
	defer goleak.VerifyNone(t)
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	if _, err := ListFolders(missing); err == nil {
		t.Error("expected error for missing picker root, got nil")
	}
}
