package sshproxy

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestListKeys_SortsAndSkipsHidden(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "id_rsa"), "key")
	writeFile(t, filepath.Join(dir, "id_ed25519"), "key")
	writeFile(t, filepath.Join(dir, "id_rsa.pub"), "pub")
	writeFile(t, filepath.Join(dir, ".hidden"), "x")
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	keys, err := ListKeys(dir)
	if err != nil {
		t.Fatalf("ListKeys: %v", err)
	}

	var names []string
	for _, k := range keys {
		names = append(names, k.Name)
	}
	want := []string{"id_ed25519", "id_rsa", "id_rsa.pub", "sub"}
	if len(names) != len(want) {
		t.Fatalf("names = %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("names = %v, want %v", names, want)
		}
	}

	for _, k := range keys {
		if k.Name == "sub" && !k.IsDir {
			t.Fatalf("sub should be reported as a directory")
		}
	}
}

func TestListKeys_MissingDir(t *testing.T) {
	if _, err := ListKeys(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Fatalf("expected error for missing dir")
	}
}

func TestResolveKeyPath_Valid(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "id_rsa"), "key")

	got, err := ResolveKeyPath(dir, "id_rsa")
	if err != nil {
		t.Fatalf("ResolveKeyPath: %v", err)
	}
	if got != filepath.Join(dir, "id_rsa") {
		t.Fatalf("got %q", got)
	}
}

func TestResolveKeyPath_RejectsTraversal(t *testing.T) {
	dir := t.TempDir()
	// A real secret outside the ssh dir.
	parent := filepath.Dir(dir)
	writeFile(t, filepath.Join(parent, "secret"), "nope")

	bad := []string{
		"",
		"   ",
		"../secret",
		"sub/key",
		"/etc/passwd",
		"..",
		"a/../b",
	}
	for _, name := range bad {
		if _, err := ResolveKeyPath(dir, name); err == nil {
			t.Fatalf("expected rejection for %q", name)
		}
	}
}

func TestResolveKeyPath_RejectsNonRegular(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if _, err := ResolveKeyPath(dir, "sub"); err == nil {
		t.Fatalf("expected rejection for directory")
	}
	if _, err := ResolveKeyPath(dir, "missing"); err == nil {
		t.Fatalf("expected rejection for missing file")
	}
}
