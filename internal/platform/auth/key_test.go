package auth

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadOrCreateKeyCreatesFile(t *testing.T) {
	base := t.TempDir()
	dataDir := filepath.Join(base, "nested", "data")

	key, err := loadOrCreateKey(dataDir)
	if err != nil {
		t.Fatalf("loadOrCreateKey: %v", err)
	}
	if len(key) != sessionKeySize {
		t.Fatalf("key length = %d, want %d", len(key), sessionKeySize)
	}

	dirInfo, err := os.Stat(dataDir)
	if err != nil {
		t.Fatalf("stat dataDir: %v", err)
	}
	if !dirInfo.IsDir() {
		t.Fatalf("dataDir %s is not a directory", dataDir)
	}
	if dirInfo.Mode().Perm() != 0750 {
		t.Errorf("dataDir mode = %#o, want 0750", dirInfo.Mode().Perm())
	}

	keyPath := filepath.Join(dataDir, sessionKeyFileName)
	fileInfo, err := os.Stat(keyPath)
	if err != nil {
		t.Fatalf("stat key file: %v", err)
	}
	if fileInfo.Mode().Perm()&0077 != 0 {
		t.Errorf("key file mode = %#o, has group/other bits set", fileInfo.Mode().Perm())
	}
}

func TestLoadOrCreateKeyIsIdempotent(t *testing.T) {
	dataDir := t.TempDir()

	key1, err := loadOrCreateKey(dataDir)
	if err != nil {
		t.Fatalf("loadOrCreateKey (first): %v", err)
	}

	key2, err := loadOrCreateKey(dataDir)
	if err != nil {
		t.Fatalf("loadOrCreateKey (second): %v", err)
	}

	if !bytes.Equal(key1, key2) {
		t.Fatal("second call to loadOrCreateKey returned a different key")
	}
}

func TestLoadOrCreateKeyRejectsShortFile(t *testing.T) {
	dataDir := t.TempDir()
	keyPath := filepath.Join(dataDir, sessionKeyFileName)
	if err := os.WriteFile(keyPath, []byte("too-short"), 0600); err != nil {
		t.Fatalf("writing corrupt key file: %v", err)
	}

	if _, err := loadOrCreateKey(dataDir); err == nil {
		t.Fatal("expected error for short/corrupt key file, got nil")
	}
}

func TestLoadOrCreateKeyRejectsOversizedFile(t *testing.T) {
	dataDir := t.TempDir()
	keyPath := filepath.Join(dataDir, sessionKeyFileName)
	if err := os.WriteFile(keyPath, bytes.Repeat([]byte("x"), sessionKeySize+1), 0600); err != nil {
		t.Fatalf("writing corrupt key file: %v", err)
	}

	if _, err := loadOrCreateKey(dataDir); err == nil {
		t.Fatal("expected error for oversized key file, got nil")
	}
}
