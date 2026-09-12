package auth

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

// hashFor returns a bcrypt hash of pin at bcrypt.MinCost, for fast tests.
func hashFor(t *testing.T, pin string) string {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(pin), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("bcrypt.GenerateFromPassword: %v", err)
	}
	return string(hash)
}

func TestCheckPINFileMatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "todo.pin")
	if err := os.WriteFile(path, []byte("4242"), 0400); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	ok, err := checkPINFile(path, "4242")
	if err != nil || !ok {
		t.Fatalf("checkPINFile(match) = (%v, %v), want (true, nil)", ok, err)
	}
}

func TestCheckPINFileMismatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "todo.pin")
	if err := os.WriteFile(path, []byte("4242"), 0400); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	ok, err := checkPINFile(path, "0000")
	if err != nil || ok {
		t.Fatalf("checkPINFile(mismatch) = (%v, %v), want (false, nil)", ok, err)
	}
	if err != nil && strings.Contains(err.Error(), "0000") {
		t.Fatalf("error leaked attempted pin: %v", err)
	}
}

func TestCheckPINFileMissing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "does-not-exist.pin")

	ok, err := checkPINFile(path, "4242")
	if ok {
		t.Fatalf("checkPINFile(missing file) = ok=true, want false")
	}
	if err == nil {
		t.Fatalf("checkPINFile(missing file) = nil error, want an error")
	}
	if strings.Contains(err.Error(), "4242") {
		t.Fatalf("error leaked attempted pin: %v", err)
	}
}

func TestCheckPINFileWorldReadableRejected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "todo.pin")
	if err := os.WriteFile(path, []byte("4242"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	ok, err := checkPINFile(path, "4242")
	if ok {
		t.Fatalf("checkPINFile with mode 0644 = ok=true, want false")
	}
	if err == nil {
		t.Fatalf("checkPINFile with mode 0644 = nil error, want an error naming the fix")
	}
	if !strings.Contains(err.Error(), "chmod") {
		t.Fatalf("checkPINFile with mode 0644 error = %q, want it to contain %q", err.Error(), "chmod")
	}
	if strings.Contains(err.Error(), "4242") {
		t.Fatalf("error leaked file pin plaintext: %v", err)
	}
}

func TestCheckPINFileTrailingNewlineTolerated(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "todo.pin")
	if err := os.WriteFile(path, []byte("4242\n"), 0400); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	ok, err := checkPINFile(path, "4242")
	if err != nil || !ok {
		t.Fatalf("checkPINFile(trailing newline) = (%v, %v), want (true, nil)", ok, err)
	}
}

func TestCheckAdminPINInline(t *testing.T) {
	hash := hashFor(t, "4321")

	ok, err := checkAdminPIN(hash, "", "4321")
	if err != nil || !ok {
		t.Fatalf("checkAdminPIN(correct) = (%v, %v), want (true, nil)", ok, err)
	}

	ok, err = checkAdminPIN(hash, "", "0000")
	if err != nil || ok {
		t.Fatalf("checkAdminPIN(wrong) = (%v, %v), want (false, nil)", ok, err)
	}
	if err != nil && strings.Contains(err.Error(), "0000") {
		t.Fatalf("error leaked attempted pin: %v", err)
	}
}

func TestCheckAdminPINNeitherSet(t *testing.T) {
	ok, err := checkAdminPIN("", "", "1234")
	if err != nil || ok {
		t.Fatalf("checkAdminPIN(neither set) = (%v, %v), want (false, nil)", ok, err)
	}
}

func TestCheckAdminPINFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "admin.pin")

	if err := os.WriteFile(path, []byte("5678\n"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	ok, err := checkAdminPIN("", path, "5678")
	if err != nil || !ok {
		t.Fatalf("checkAdminPIN(correct) = (%v, %v), want (true, nil)", ok, err)
	}

	ok, err = checkAdminPIN("", path, "0000")
	if err != nil || ok {
		t.Fatalf("checkAdminPIN(wrong) = (%v, %v), want (false, nil)", ok, err)
	}
	if err != nil && strings.Contains(err.Error(), "0000") {
		t.Fatalf("error leaked attempted pin: %v", err)
	}

	// Loosen the mode: attempts must now be refused with a chmod hint, and
	// the error must never contain the file's own plaintext contents.
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	ok, err = checkAdminPIN("", path, "5678")
	if ok {
		t.Fatalf("checkAdminPIN with mode 0644 = ok=true, want false")
	}
	if err == nil {
		t.Fatalf("checkAdminPIN with mode 0644 = nil error, want an error naming the fix")
	}
	if !strings.Contains(err.Error(), "chmod") {
		t.Fatalf("checkAdminPIN with mode 0644 error = %q, want it to contain %q", err.Error(), "chmod")
	}
	if !strings.Contains(err.Error(), "auth.admin_pin_file") {
		t.Fatalf("checkAdminPIN with mode 0644 error = %q, want it to contain %q", err.Error(), "auth.admin_pin_file")
	}
	if strings.Contains(err.Error(), "5678") {
		t.Fatalf("error leaked file pin plaintext: %v", err)
	}

	// Restore a safe mode, then rewrite the file with a new pin: the very
	// next attempt must observe the new value (proving per-attempt reads)
	// and reject the old one.
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	if err := os.WriteFile(path, []byte("9999"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	ok, err = checkAdminPIN("", path, "9999")
	if err != nil || !ok {
		t.Fatalf("checkAdminPIN(new pin) = (%v, %v), want (true, nil)", ok, err)
	}
	ok, err = checkAdminPIN("", path, "5678")
	if err != nil || ok {
		t.Fatalf("checkAdminPIN(old pin after rewrite) = (%v, %v), want (false, nil)", ok, err)
	}
}

func TestCheckAdminPINFileMissing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "does-not-exist.pin")

	ok, err := checkAdminPIN("", path, "1234")
	if ok {
		t.Fatalf("checkAdminPIN(missing file) = ok=true, want false")
	}
	if err == nil {
		t.Fatalf("checkAdminPIN(missing file) = nil error, want an error")
	}
	if strings.Contains(err.Error(), "1234") {
		t.Fatalf("error leaked attempted pin: %v", err)
	}
}
