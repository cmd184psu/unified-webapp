package sshclient_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cmd184psu/unified-webapp/internal/platform/sshclient"
	"cmd184psu/unified-webapp/internal/platform/sshclient/sshtest"

	"golang.org/x/crypto/ssh"
)

func TestRunFeedsStdinAndReportsExitStatus(t *testing.T) {
	srv := sshtest.Start(t, "alice", "pw", func(cmd string, stdin []byte) (string, int) {
		switch cmd {
		case "cat":
			return string(stdin), 0
		case "false":
			return "nope\n", 1
		}
		return "unknown\n", 127
	})
	creds := sshclient.Credentials{Host: srv.Host, Port: srv.Port, User: "alice", Password: sshclient.NewSecret("pw")}
	client, err := sshclient.Dial(context.Background(), creds, ssh.InsecureIgnoreHostKey())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close()

	out, err := sshclient.Run(context.Background(), client, "cat", []byte("hello"))
	if err != nil || out != "hello" {
		t.Errorf("cat: out %q, err %v", out, err)
	}
	out, err = sshclient.Run(context.Background(), client, "false", nil)
	var exitErr *ssh.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitStatus() != 1 || out != "nope\n" {
		t.Errorf("false: out %q, err %v", out, err)
	}
}

func TestDialRejectsWrongPassword(t *testing.T) {
	srv := sshtest.Start(t, "alice", "pw", func(string, []byte) (string, int) { return "", 0 })
	creds := sshclient.Credentials{Host: srv.Host, Port: srv.Port, User: "alice", Password: sshclient.NewSecret("wrong")}
	if _, err := sshclient.Dial(context.Background(), creds, ssh.InsecureIgnoreHostKey()); err == nil {
		t.Fatal("dial with a wrong password should fail")
	}
}

func TestCredentialsExactlyOneOf(t *testing.T) {
	both := sshclient.Credentials{KeyPath: "/k", Password: sshclient.NewSecret("p")}
	if _, err := both.AuthMethods(); !errors.Is(err, sshclient.ErrCredential) {
		t.Errorf("both: %v, want ErrCredential", err)
	}
	if _, err := (sshclient.Credentials{}).AuthMethods(); !errors.Is(err, sshclient.ErrCredential) {
		t.Errorf("neither: %v, want ErrCredential", err)
	}
	if got := (sshclient.Credentials{Host: "h"}).Addr(); got != "h:22" {
		t.Errorf("default port: %q", got)
	}
}

func TestSettingsResolve(t *testing.T) {
	dir := t.TempDir()
	r, err := sshclient.Settings{SSHDir: dir}.Resolve()
	if err != nil {
		t.Fatalf("resolve lax: %v", err)
	}
	if r.SSHDir != dir || r.KnownHostsPath != filepath.Join(dir, "known_hosts") || r.HostKeyCallback == nil {
		t.Errorf("defaults not applied: %+v", r)
	}
	// Strict mode needs a readable known_hosts.
	if _, err := (sshclient.Settings{SSHDir: dir, StrictHostKey: true}).Resolve(); err == nil ||
		!strings.Contains(err.Error(), "known_hosts") {
		t.Errorf("strict without known_hosts should fail, got %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "known_hosts"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := (sshclient.Settings{SSHDir: dir, StrictHostKey: true}).Resolve(); err != nil {
		t.Errorf("strict with known_hosts: %v", err)
	}
}
