package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"cmd184psu/unified-webapp/internal/platform/config"
)

func TestRunGenAPIKey_WithNameStoresHash(t *testing.T) {
	p := filepath.Join(t.TempDir(), "cfg.json")
	os.WriteFile(p, []byte(`{"port": 8081}`), 0o600)
	var out bytes.Buffer
	if err := runGenAPIKey("haproxy-editor", p, &out); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	m := regexp.MustCompile(`key:\s+(\S+)`).FindStringSubmatch(s)
	if m == nil || !strings.Contains(s, "hash: sha256:") ||
		!strings.Contains(s, `stored as "haproxy-editor" in `+p) || !strings.Contains(s, "restarted") {
		t.Fatalf("output:\n%s", s)
	}
	sum := sha256.Sum256([]byte(m[1]))
	want := "sha256:" + hex.EncodeToString(sum[:])
	c, err := config.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Auth.APIKeys) != 1 || c.Auth.APIKeys[0].Name != "haproxy-editor" || c.Auth.APIKeys[0].Hash != want {
		t.Fatalf("keys=%+v want %s", c.Auth.APIKeys, want)
	}
}

func TestRunGenAPIKey_EmptyNameWritesNothing(t *testing.T) {
	p := filepath.Join(t.TempDir(), "cfg.json")
	orig := []byte(`{"port": 8081}`)
	os.WriteFile(p, orig, 0o600)
	var out bytes.Buffer
	if err := runGenAPIKey("", p, &out); err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^key:  \S+\nhash: sha256:[0-9a-f]{64}\n$`).MatchString(out.String()) {
		t.Fatalf("output:\n%q", out.String())
	}
	if got, _ := os.ReadFile(p); !bytes.Equal(got, orig) {
		t.Fatal("config changed")
	}
}

func TestRunGenAPIKey_MissingConfigPrintsNoKey(t *testing.T) {
	p := filepath.Join(t.TempDir(), "nope.json")
	var out bytes.Buffer
	if err := runGenAPIKey("x", p, &out); err == nil {
		t.Fatal("want error")
	}
	if out.Len() != 0 {
		t.Fatalf("printed: %q", out.String())
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatal("file created")
	}
}
