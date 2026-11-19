package verify

import (
	"os"
	"path/filepath"
	"testing"
)

func writeCopyFixture(t *testing.T, src string) {
	t.Helper()
	for rel, body := range map[string]string{
		"Main.cs":                     "class A {}\n",
		"sub/Deep.cs":                 "class B {}\n",
		".git":                        "gitdir: elsewhere\n",
		"bin/Debug/app.dll":           "x",
		"src/obj/project.assets.json": "{}",
	} {
		p := filepath.Join(src, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// TestCopyWorktreeSkipsGitAndBuildOutput: a verify copy holds the sources and
// leaves out the gitfile, bin and obj at any depth (AF-6).
func TestCopyWorktreeSkipsGitAndBuildOutput(t *testing.T) {
	src := t.TempDir()
	writeCopyFixture(t, src)
	dst := filepath.Join(t.TempDir(), "copy")
	if err := CopyWorktree(dst, src); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Main.cs", "sub/Deep.cs"} {
		if _, err := os.Stat(filepath.Join(dst, want)); err != nil {
			t.Errorf("%s was not copied: %v", want, err)
		}
	}
	for _, gone := range []string{".git", "bin", "src/obj"} {
		if _, err := os.Stat(filepath.Join(dst, gone)); !os.IsNotExist(err) {
			t.Errorf("%s must not be copied (err = %v)", gone, err)
		}
	}
}

// TestCopyWorktreeReplacesAndLinks: a second run replaces the copy, a symlink
// stays a link and a file's mode is kept.
func TestCopyWorktreeReplacesAndLinks(t *testing.T) {
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "run.sh"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("run.sh", filepath.Join(src, "link")); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "copy")
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dst, "stale.txt"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := CopyWorktree(dst, src); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dst, "stale.txt")); !os.IsNotExist(err) {
		t.Error("the previous copy's files must be replaced")
	}
	if target, err := os.Readlink(filepath.Join(dst, "link")); err != nil || target != "run.sh" {
		t.Errorf("link = %q (%v), want run.sh", target, err)
	}
	if fi, err := os.Stat(filepath.Join(dst, "run.sh")); err != nil || fi.Mode().Perm()&0o100 == 0 {
		t.Errorf("run.sh lost its mode: %v (%v)", fi.Mode(), err)
	}
}
