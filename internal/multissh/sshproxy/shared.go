// Package sshproxy bridges browser terminals to remote hosts: it proxies an SSH
// PTY session over a WebSocket and moves files over SFTP. The generic SSH
// pieces (credentials, the server's key directory, host-key policy, and the
// password type that can't be logged) live in the shared
// internal/platform/sshclient package; the names below re-export them so
// multissh's call sites are unchanged.
package sshproxy

import (
	"cmd184psu/unified-webapp/internal/platform/sshclient"

	"golang.org/x/crypto/ssh"
)

// Secret is sshclient.Secret: a password that can't be formatted or
// serialized in the clear.
type Secret = sshclient.Secret

// KeyFile is sshclient.KeyFile: one entry in the key picker.
type KeyFile = sshclient.KeyFile

// secretMask is what a Secret renders as (kept for this package's tests).
const secretMask = sshclient.SecretMask

// ErrCredential reports a ConnectParams carrying neither or both credentials.
var ErrCredential = sshclient.ErrCredential

// NewSecret wraps a plaintext password.
func NewSecret(v string) Secret { return sshclient.NewSecret(v) }

// DefaultSSHDir returns the server user's ~/.ssh.
func DefaultSSHDir() (string, error) { return sshclient.DefaultSSHDir() }

// ListKeys lists the key picker's entries in dir.
func ListKeys(dir string) ([]KeyFile, error) { return sshclient.ListKeys(dir) }

// HostKeyCallback is the shared host-key policy (see sshclient.HostKeyCallback).
func HostKeyCallback(secure bool, knownHostsPath string) (ssh.HostKeyCallback, error) {
	return sshclient.HostKeyCallback(secure, knownHostsPath)
}

// ResolveKeyPath turns a picker-selected name into a path proven inside dir.
func ResolveKeyPath(dir, name string) (string, error) { return sshclient.ResolveKeyPath(dir, name) }
