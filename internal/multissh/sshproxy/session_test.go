package sshproxy

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

var errAuth = errors.New("unauthorized key")

// writeClientKey generates an ed25519 keypair, writes the private key (PEM) into
// a temp ssh dir, and returns the dir, the base name, and the public key.
func writeClientKey(t *testing.T) (dir, name string, pub ssh.PublicKey) {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("gen key: %v", err)
	}
	block, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	dir = t.TempDir()
	name = "id_ed25519"
	if err := os.WriteFile(filepath.Join(dir, name), pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatalf("signer: %v", err)
	}
	return dir, name, signer.PublicKey()
}

// startEchoSSHServer launches a minimal in-process SSH server that authorizes
// authorized and echoes every byte of the shell back to the client.
func startEchoSSHServer(t *testing.T, authorized ssh.PublicKey) string {
	t.Helper()
	_, hostPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("host key: %v", err)
	}
	hostSigner, err := ssh.NewSignerFromKey(hostPriv)
	if err != nil {
		t.Fatalf("host signer: %v", err)
	}

	cfg := &ssh.ServerConfig{
		PublicKeyCallback: func(_ ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			if bytes.Equal(key.Marshal(), authorized.Marshal()) {
				return &ssh.Permissions{}, nil
			}
			return nil, errAuth
		},
	}
	cfg.AddHostKey(hostSigner)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go serveEcho(conn, cfg)
		}
	}()
	return ln.Addr().String()
}

func serveEcho(conn net.Conn, cfg *ssh.ServerConfig) {
	sc, chans, reqs, err := ssh.NewServerConn(conn, cfg)
	if err != nil {
		return
	}
	defer sc.Close()
	go ssh.DiscardRequests(reqs)
	for newCh := range chans {
		if newCh.ChannelType() != "session" {
			_ = newCh.Reject(ssh.UnknownChannelType, "only sessions")
			continue
		}
		ch, chReqs, err := newCh.Accept()
		if err != nil {
			return
		}
		go func() {
			for req := range chReqs {
				switch req.Type {
				case "pty-req", "window-change":
					_ = req.Reply(true, nil)
				case "shell":
					_ = req.Reply(true, nil)
					go func() {
						_, _ = io.Copy(ch, ch)
						_ = ch.Close()
					}()
				default:
					_ = req.Reply(false, nil)
				}
			}
		}()
	}
}

func TestSSHDialer_EchoRoundTrip(t *testing.T) {
	dir, name, pub := writeClientKey(t)
	addr := startEchoSSHServer(t, pub)
	host, port := splitHostPort(t, addr)

	keyPath, err := ResolveKeyPath(dir, name)
	if err != nil {
		t.Fatalf("resolve key: %v", err)
	}

	conn, err := SSHDialer{Timeout: 5 * time.Second}.Dial(ConnectParams{
		Host: host, Port: port, User: "tester", KeyPath: keyPath, Cols: 80, Rows: 24,
	})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	if _, err := conn.Write([]byte("ping\n")); err != nil {
		t.Fatalf("write: %v", err)
	}

	got := readN(t, conn.Output(), len("ping\n"))
	if string(got) != "ping\n" {
		t.Fatalf("echo = %q, want %q", got, "ping\n")
	}

	if err := conn.Resize(120, 40); err != nil {
		t.Fatalf("resize: %v", err)
	}
}

func TestSSHDialer_WrongKeyFails(t *testing.T) {
	// Server authorizes a different key than the one the client presents.
	_, _, authorized := writeClientKey(t)
	addr := startEchoSSHServer(t, authorized)
	host, port := splitHostPort(t, addr)

	dir, name, _ := writeClientKey(t)
	keyPath, err := ResolveKeyPath(dir, name)
	if err != nil {
		t.Fatalf("resolve key: %v", err)
	}

	_, err = SSHDialer{Timeout: 5 * time.Second}.Dial(ConnectParams{
		Host: host, Port: port, User: "tester", KeyPath: keyPath,
	})
	if err == nil {
		t.Fatalf("expected auth failure")
	}
}

func readN(t *testing.T, r io.Reader, n int) []byte {
	t.Helper()
	buf := make([]byte, 0, n)
	tmp := make([]byte, n)
	deadline := time.Now().Add(5 * time.Second)
	for len(buf) < n && time.Now().Before(deadline) {
		m, err := r.Read(tmp)
		buf = append(buf, tmp[:m]...)
		if err != nil {
			break
		}
	}
	return buf
}

func splitHostPort(t *testing.T, addr string) (string, int) {
	t.Helper()
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("split addr: %v", err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("parse port: %v", err)
	}
	return host, port
}
