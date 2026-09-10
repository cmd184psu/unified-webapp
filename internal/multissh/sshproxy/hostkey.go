package sshproxy

import (
	"fmt"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// HostKeyCallback returns the SSH host-key verification strategy for both the
// terminal dialer and the SFTP transferrer.
//
// When secure is false it returns ssh.InsecureIgnoreHostKey(): the default for
// the trusted lab networks this tool targets, where VMs are frequently rebuilt
// and their host keys legitimately churn.
//
// When secure is true it verifies the remote host key against the known_hosts
// file at knownHostsPath and fails closed: a missing/unreadable file is an
// error here, and an unknown or mismatched host key is rejected at dial time.
func HostKeyCallback(secure bool, knownHostsPath string) (ssh.HostKeyCallback, error) {
	if !secure {
		return ssh.InsecureIgnoreHostKey(), nil
	}
	cb, err := knownhosts.New(knownHostsPath)
	if err != nil {
		return nil, fmt.Errorf("sshproxy: secure mode requires a readable known_hosts at %q: %w", knownHostsPath, err)
	}
	return cb, nil
}
