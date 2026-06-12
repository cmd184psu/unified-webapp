package obsidianoid_test

import (
	"os"
	"path/filepath"
	"testing"

	"cmd184psu/unified-webapp/internal/obsidianoid"
)

func createVault(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"Note One.md":          "# Note One\nHello",
		"subdir/Note Two.md":   "# Note Two\nWorld",
		"subdir/Note Three.md": "# Note Three\nFoo",
		".hidden/secret.md":    "should be ignored",
		"image.png":            "binary",
	}
	for rel, content := range files {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		_ = os.MkdirAll(filepath.Dir(abs), 0o755)
		_ = os.WriteFile(abs, []byte(content), 0o644)
	}
	return root
}

func TestVaultTree(t *testing.T) {
	root := createVault(t)
	tree, err := obsidianoid.VaultTree(root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tree == nil {
		t.Fatal("tree is nil")
	}
	if !tree.IsDir {
		t.Error("root should be a directory")
	}
	if len(tree.Children) == 0 {
		t.Error("root should have children")
	}
}

func TestReadNote(t *testing.T) {
	root := createVault(t)
	content, err := obsidianoid.ReadNote(root, "Note One.md")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(content) != "# Note One\nHello" {
		t.Errorf("unexpected content: %s", content)
	}
}

func TestReadNotePathTraversal(t *testing.T) {
	root := createVault(t)
	_, err := obsidianoid.ReadNote(root, "../../etc/passwd")
	if err == nil {
		t.Error("expected error for path traversal")
	}
}

func TestWriteNote(t *testing.T) {
	root := createVault(t)
	err := obsidianoid.WriteNote(root, "New Note.md", []byte("# New\ncontent"))
	if err != nil {
		t.Fatalf("write failed: %v", err)
	}
	content, err := obsidianoid.ReadNote(root, "New Note.md")
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if string(content) != "# New\ncontent" {
		t.Errorf("unexpected content: %s", content)
	}
}

func TestWriteNotePathTraversal(t *testing.T) {
	root := createVault(t)
	err := obsidianoid.WriteNote(root, "../outside.md", []byte("bad"))
	if err == nil {
		t.Error("expected error for path traversal on write")
	}
}

func TestWriteNoteCreatesSubdirectory(t *testing.T) {
	root := createVault(t)
	err := obsidianoid.WriteNote(root, "newdir/Deep Note.md", []byte("deep"))
	if err != nil {
		t.Fatalf("write failed: %v", err)
	}
	content, err := obsidianoid.ReadNote(root, "newdir/Deep Note.md")
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if string(content) != "deep" {
		t.Errorf("unexpected content: %s", content)
	}
}
