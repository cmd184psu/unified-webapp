package sshproxy

import (
	"crypto/ed25519"
	"crypto/rand"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

func TestHostKeyCallbackInsecureWhenNotSecure(t *testing.T) {
	cb, err := HostKeyCallback(false, "/nonexistent/known_hosts")
	if err != nil {
		t.Fatalf("insecure mode should not error: %v", err)
	}
	if cb == nil {
		t.Fatal("expected a non-nil callback in insecure mode")
	}
}

func TestHostKeyCallbackSecureMissingFile(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	if _, err := HostKeyCallback(true, missing); err == nil {
		t.Fatal("secure mode must fail closed when known_hosts is missing")
	}
}

func TestHostKeyCallbackSecureValidFile(t *testing.T) {
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatalf("ssh public key: %v", err)
	}
	line := knownhosts.Line([]string{"example.com:22"}, sshPub)

	path := filepath.Join(t.TempDir(), "known_hosts")
	if err := os.WriteFile(path, []byte(line+"\n"), 0o600); err != nil {
		t.Fatalf("write known_hosts: %v", err)
	}

	cb, err := HostKeyCallback(true, path)
	if err != nil {
		t.Fatalf("secure mode with valid known_hosts should not error: %v", err)
	}
	if cb == nil {
		t.Fatal("expected a non-nil callback in secure mode")
	}
}
