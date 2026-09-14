package smbedit

// Ported from smbed's internal/samba/write_internal_test.go. These exercise
// the unexported writeFile sudo-fallback seam by swapping the runCommand var.

import (
	"os"
	"path/filepath"
	"testing"

	"go.uber.org/goleak"
)

func TestWriteFile_NoSudoWhenDirectWriteSucceeds(t *testing.T) {
	defer goleak.VerifyNone(t)
	target := filepath.Join(t.TempDir(), "smb.conf")

	called := false
	orig := runCommand
	runCommand = func(name string, args ...string) (string, error) {
		called = true
		return "", nil
	}
	defer func() { runCommand = orig }()

	if err := writeFile(target, []byte("hi"), noplog); err != nil {
		t.Fatalf("writeFile() error: %v", err)
	}
	if called {
		t.Error("expected sudo fallback NOT to be invoked when the destination is directly writable")
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("reading %s: %v", target, err)
	}
	if string(got) != "hi" {
		t.Errorf("got %q, want %q", got, "hi")
	}
}

func TestWriteFile_FallsBackToSudoOnPermissionError(t *testing.T) {
	defer goleak.VerifyNone(t)
	if os.Geteuid() == 0 {
		t.Skip("running as root bypasses permission checks")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	defer os.Chmod(dir, 0o755) // let t.TempDir() clean up
	target := filepath.Join(dir, "smb.conf")

	var calledArgs []string
	orig := runCommand
	runCommand = func(name string, args ...string) (string, error) {
		calledArgs = append([]string{name}, args...)
		// Simulate what "sudo install" would do: write with elevated perms.
		os.Chmod(dir, 0o755)
		defer os.Chmod(dir, 0o555)
		tmpPath, destPath := args[len(args)-2], args[len(args)-1]
		data, err := os.ReadFile(tmpPath)
		if err != nil {
			return "", err
		}
		if err := os.WriteFile(destPath, data, 0o644); err != nil {
			return "", err
		}
		return "", nil
	}
	defer func() { runCommand = orig }()

	if err := writeFile(target, []byte("hello"), noplog); err != nil {
		t.Fatalf("writeFile() error: %v", err)
	}
	if len(calledArgs) == 0 || calledArgs[0] != "sudo" {
		t.Fatalf("expected sudo fallback to be invoked, got %v", calledArgs)
	}

	os.Chmod(dir, 0o755)
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("reading %s: %v", target, err)
	}
	if string(got) != "hello" {
		t.Errorf("got %q, want %q", got, "hello")
	}
}
