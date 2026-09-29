// Package sshclient is the app's one SSH client layer, shared by every module
// that reaches another machine over SSH: multissh's terminals and SFTP, and
// certmachine's remote CA trust. It owns the credential rules (a key or a
// password, never both), the key picker's view of the server's ~/.ssh, the
// host-key policy, and the password type that can't be logged. It is pure Go
// (golang.org/x/crypto/ssh, no CGO).
package sshclient

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

// Credentials are the inputs for one SSH login. KeyPath is an absolute path
// produced by ResolveKeyPath, never taken directly from a client.
//
// KeyPath and Password are exactly-one-of: a login uses a key or a password,
// never both and never neither. AuthMethods enforces that for every caller.
type Credentials struct {
	Host     string
	Port     int
	User     string
	KeyPath  string
	Password Secret
}

// ErrCredential reports Credentials carrying neither or both of key and password.
var ErrCredential = errors.New("sshclient: exactly one of key or password is required")

// AuthMethods resolves the exactly-one-of credential into SSH auth methods.
func (c Credentials) AuthMethods() ([]ssh.AuthMethod, error) {
	hasKey := strings.TrimSpace(c.KeyPath) != ""
	hasPassword := !c.Password.IsZero()
	if hasKey == hasPassword {
		return nil, ErrCredential
	}
	if hasPassword {
		return []ssh.AuthMethod{ssh.Password(c.Password.Reveal())}, nil
	}
	keyBytes, err := os.ReadFile(c.KeyPath)
	if err != nil {
		return nil, fmt.Errorf("sshclient: read key: %w", err)
	}
	signer, err := ssh.ParsePrivateKey(keyBytes)
	if err != nil {
		return nil, fmt.Errorf("sshclient: parse key (encrypted keys are not supported): %w", err)
	}
	return []ssh.AuthMethod{ssh.PublicKeys(signer)}, nil
}

// Addr is host:port, with port 22 when unset.
func (c Credentials) Addr() string {
	port := c.Port
	if port <= 0 {
		port = 22
	}
	return net.JoinHostPort(c.Host, strconv.Itoa(port))
}

// Settings is the SSH configuration every module shares (today it lives under
// multissh's config block): where the server's keys are, and how strictly
// host keys are checked.
type Settings struct {
	SSHDir         string // empty: the server user's ~/.ssh
	KnownHostsPath string // empty: <SSHDir>/known_hosts
	StrictHostKey  bool
}

// Resolved is Settings with defaults applied and the host-key policy built.
type Resolved struct {
	SSHDir          string
	KnownHostsPath  string
	HostKeyCallback ssh.HostKeyCallback
}

// Resolve applies the defaults and builds the host-key callback. It fails when
// strict host-key checking is on but known_hosts can't be read, so a
// misconfiguration is reported at boot instead of silently trusting any host.
func (s Settings) Resolve() (Resolved, error) {
	dir := strings.TrimSpace(s.SSHDir)
	if dir == "" {
		d, err := DefaultSSHDir()
		if err != nil {
			return Resolved{}, err
		}
		dir = d
	}
	known := strings.TrimSpace(s.KnownHostsPath)
	if known == "" {
		known = filepath.Join(dir, "known_hosts")
	}
	cb, err := HostKeyCallback(s.StrictHostKey, known)
	if err != nil {
		return Resolved{}, err
	}
	return Resolved{SSHDir: dir, KnownHostsPath: known, HostKeyCallback: cb}, nil
}

// DialTimeout bounds the TCP connect plus SSH handshake.
const DialTimeout = 15 * time.Second

// Dial opens an SSH client connection. It honors ctx for the connect phase;
// the caller closes the returned client.
func Dial(ctx context.Context, c Credentials, hostKeyCB ssh.HostKeyCallback) (*ssh.Client, error) {
	auth, err := c.AuthMethods()
	if err != nil {
		return nil, err
	}
	cfg := &ssh.ClientConfig{
		User:            c.User,
		Auth:            auth,
		HostKeyCallback: hostKeyCB,
		Timeout:         DialTimeout,
	}
	d := net.Dialer{Timeout: DialTimeout}
	conn, err := d.DialContext(ctx, "tcp", c.Addr())
	if err != nil {
		return nil, fmt.Errorf("sshclient: connect %s: %w", c.Addr(), err)
	}
	sc, chans, reqs, err := ssh.NewClientConn(conn, c.Addr(), cfg)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("sshclient: handshake with %s: %w", c.Addr(), err)
	}
	return ssh.NewClient(sc, chans, reqs), nil
}

// Run executes one command in a new session and returns its combined stdout
// and stderr. stdin, when non-nil, is fed to the command (e.g. file contents
// for `cat > path`), so data never has to be spliced into the command line.
// A non-zero exit returns the output together with the error. Canceling ctx
// closes the session.
func Run(ctx context.Context, client *ssh.Client, cmd string, stdin []byte) (string, error) {
	sess, err := client.NewSession()
	if err != nil {
		return "", fmt.Errorf("sshclient: new session: %w", err)
	}
	defer sess.Close()

	if stdin != nil {
		sess.Stdin = bytes.NewReader(stdin)
	}

	// CombinedOutput merges stdout and stderr through one locked writer; the
	// SSH library copies the two streams from separate goroutines, so they
	// must not share an unsynchronized buffer.
	type result struct {
		out []byte
		err error
	}
	done := make(chan result, 1)
	go func() {
		out, err := sess.CombinedOutput(cmd)
		done <- result{out, err}
	}()
	select {
	case r := <-done:
		return string(r.out), r.err
	case <-ctx.Done():
		sess.Close()
		r := <-done
		return string(r.out), ctx.Err()
	}
}
