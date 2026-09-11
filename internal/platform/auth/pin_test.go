package auth

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"cmd184psu/unified-webapp/internal/platform/config"
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

func TestCheckPIN(t *testing.T) {
	pins := []config.NamedHash{
		{Name: "alice", Hash: hashFor(t, "1111")},
		{Name: "bob", Hash: hashFor(t, "2222")},
	}

	if name, ok := checkPIN(pins, "1111"); !ok || name != "alice" {
		t.Fatalf("checkPIN(1111) = (%q, %v), want (alice, true)", name, ok)
	}
	if name, ok := checkPIN(pins, "2222"); !ok || name != "bob" {
		t.Fatalf("checkPIN(2222) = (%q, %v), want (bob, true)", name, ok)
	}
	if name, ok := checkPIN(pins, "9999"); ok {
		t.Fatalf("checkPIN(9999) = (%q, %v), want ok=false", name, ok)
	}
	if name, ok := checkPIN(nil, "1111"); ok {
		t.Fatalf("checkPIN(nil, 1111) = (%q, %v), want ok=false", name, ok)
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
