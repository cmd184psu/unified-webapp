package sshproxy

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

// ConnectParams are the per-terminal connection inputs supplied by the
// frontend. KeyPath is the absolute path produced by ResolveKeyPath; it is
// never taken directly from the client.
//
// KeyPath and Password are exactly-one-of: a connection authenticates with a
// key or with a password, never both and never neither. AuthMethods enforces
// that for every dial path in this package.
type ConnectParams struct {
	Host     string
	Port     int
	User     string
	KeyPath  string
	Password Secret
	Cols     int
	Rows     int
}

// ErrCredential reports a ConnectParams carrying neither or both credentials.
var ErrCredential = errors.New("sshproxy: exactly one of key or password is required")

// AuthMethods resolves the exactly-one-of credential into SSH auth methods.
// Both dial paths (terminal and SFTP) go through it, so the rule cannot drift
// between them.
func (p ConnectParams) AuthMethods() ([]ssh.AuthMethod, error) {
	hasKey := strings.TrimSpace(p.KeyPath) != ""
	hasPassword := !p.Password.IsZero()
	if hasKey == hasPassword {
		return nil, ErrCredential
	}
	if hasPassword {
		return []ssh.AuthMethod{ssh.Password(p.Password.Reveal())}, nil
	}
	keyBytes, err := os.ReadFile(p.KeyPath)
	if err != nil {
		return nil, fmt.Errorf("sshproxy: read key: %w", err)
	}
	signer, err := ssh.ParsePrivateKey(keyBytes)
	if err != nil {
		return nil, fmt.Errorf("sshproxy: parse key (encrypted keys are not supported): %w", err)
	}
	return []ssh.AuthMethod{ssh.PublicKeys(signer)}, nil
}

// Conn is one live remote shell. Output yields the merged PTY stdout/stderr.
// The bridge copies browser bytes into the Conn via Write and Conn bytes back
// out via Output. Implementations must be safe for one reader (Output) and one
// writer (Write/Resize) used concurrently with Close.
type Conn interface {
	io.Writer
	Output() io.Reader
	Resize(cols, rows int) error
	// Wait blocks until the remote shell exits or the connection drops.
	Wait() error
	Close() error
}

// Dialer establishes a Conn from ConnectParams. It is injectable so the
// WebSocket bridge can be tested without a real SSH server.
type Dialer interface {
	Dial(p ConnectParams) (Conn, error)
}

// SSHDialer is the production Dialer backed by golang.org/x/crypto/ssh.
type SSHDialer struct {
	// Timeout bounds the TCP+handshake phase. Zero uses dialTimeoutDefault.
	Timeout time.Duration
	// HostKeyCallback verifies the remote host key. When nil the dialer falls
	// back to ssh.InsecureIgnoreHostKey() (the trusted-lab default).
	HostKeyCallback ssh.HostKeyCallback
}

const dialTimeoutDefault = 15 * time.Second

// Dial reads and parses the private key, opens the TCP connection, performs
// the SSH handshake, and requests an interactive PTY shell.
func (d SSHDialer) Dial(p ConnectParams) (Conn, error) {
	if p.User == "" {
		return nil, fmt.Errorf("sshproxy: missing user")
	}
	if p.Host == "" {
		return nil, fmt.Errorf("sshproxy: missing host")
	}
	port := p.Port
	if port == 0 {
		port = 22
	}

	auth, err := p.AuthMethods()
	if err != nil {
		return nil, err
	}

	timeout := d.Timeout
	if timeout == 0 {
		timeout = dialTimeoutDefault
	}

	hostKey := d.HostKeyCallback
	if hostKey == nil {
		hostKey = ssh.InsecureIgnoreHostKey()
	}
	cfg := &ssh.ClientConfig{
		User:            p.User,
		Auth:            auth,
		HostKeyCallback: hostKey,
		Timeout:         timeout,
	}

	addr := net.JoinHostPort(p.Host, fmt.Sprintf("%d", port))
	client, err := ssh.Dial("tcp", addr, cfg)
	if err != nil {
		return nil, fmt.Errorf("sshproxy: dial %s: %w", addr, err)
	}

	sess, err := client.NewSession()
	if err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("sshproxy: new session: %w", err)
	}

	stdin, err := sess.StdinPipe()
	if err != nil {
		_ = sess.Close()
		_ = client.Close()
		return nil, fmt.Errorf("sshproxy: stdin pipe: %w", err)
	}

	// Merge stdout and stderr through a single pipe so the bridge has one
	// ordered byte stream to forward to the terminal.
	pr, pw := io.Pipe()
	sess.Stdout = pw
	sess.Stderr = pw

	cols, rows := p.Cols, p.Rows
	if cols <= 0 {
		cols = 80
	}
	if rows <= 0 {
		rows = 24
	}
	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}
	if err := sess.RequestPty("xterm-256color", rows, cols, modes); err != nil {
		_ = sess.Close()
		_ = client.Close()
		return nil, fmt.Errorf("sshproxy: request pty: %w", err)
	}
	if err := sess.Shell(); err != nil {
		_ = sess.Close()
		_ = client.Close()
		return nil, fmt.Errorf("sshproxy: start shell: %w", err)
	}

	return &sshConn{
		client: client,
		sess:   sess,
		stdin:  stdin,
		out:    pr,
		outW:   pw,
	}, nil
}

// sshConn adapts an ssh.Session to the Conn interface.
type sshConn struct {
	client *ssh.Client
	sess   *ssh.Session
	stdin  io.WriteCloser
	out    *io.PipeReader
	outW   *io.PipeWriter
}

func (c *sshConn) Write(p []byte) (int, error) { return c.stdin.Write(p) }

func (c *sshConn) Output() io.Reader { return c.out }

func (c *sshConn) Resize(cols, rows int) error {
	if cols <= 0 || rows <= 0 {
		return nil
	}
	return c.sess.WindowChange(rows, cols)
}

func (c *sshConn) Wait() error {
	err := c.sess.Wait()
	// Unblock any pending Output read once the shell ends.
	_ = c.outW.Close()
	return err
}

func (c *sshConn) Close() error {
	_ = c.stdin.Close()
	_ = c.outW.Close()
	_ = c.sess.Close()
	return c.client.Close()
}
