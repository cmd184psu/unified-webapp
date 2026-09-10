package sshproxy

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// Transferrer copies a local file to a remote path over SSH and reports
// cumulative transferred bytes.
type Transferrer interface {
	Transfer(ctx context.Context, p ConnectParams, localPath, remotePath string, progress func(bytes int64)) error
}

type RemoteEntry struct {
	Name  string `json:"name"`
	IsDir bool   `json:"isDir"`
}

type RemoteLister interface {
	ListDir(ctx context.Context, p ConnectParams, dir string) (string, []RemoteEntry, error)
}

// SFTPTransferrer is the production Transferrer backed by SSH+SFTP.
type SFTPTransferrer struct {
	Timeout time.Duration
	// HostKeyCallback verifies the remote host key. When nil the transferrer
	// falls back to ssh.InsecureIgnoreHostKey() (the trusted-lab default).
	HostKeyCallback ssh.HostKeyCallback
}

func (t SFTPTransferrer) Transfer(ctx context.Context, p ConnectParams, localPath, remotePath string, progress func(bytes int64)) error {
	client, sftpClient, err := t.dial(ctx, p)
	if err != nil {
		return err
	}
	defer client.Close()
	defer sftpClient.Close()

	localFile, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("sshproxy: open local file: %w", err)
	}
	defer localFile.Close()

	remoteFile, err := sftpClient.Create(remotePath)
	if err != nil {
		return fmt.Errorf("sshproxy: create remote file: %w", err)
	}
	defer remoteFile.Close()

	if progress == nil {
		progress = func(int64) {}
	}

	ctxReader := &contextReader{ctx: ctx, r: localFile}
	pw := &progressWriter{ctx: ctx, w: remoteFile, progress: progress}
	if _, err := io.Copy(pw, ctxReader); err != nil {
		return fmt.Errorf("sshproxy: transfer copy: %w", err)
	}
	return nil
}

func (t SFTPTransferrer) ListDir(ctx context.Context, p ConnectParams, dir string) (string, []RemoteEntry, error) {
	client, sftpClient, err := t.dial(ctx, p)
	if err != nil {
		return "", nil, err
	}
	defer client.Close()
	defer sftpClient.Close()

	dir = strings.TrimSpace(dir)
	if dir == "" {
		dir = "/tmp"
	}

	entries, err := sftpClient.ReadDir(dir)
	if err != nil {
		return "", nil, fmt.Errorf("sshproxy: read remote dir: %w", err)
	}
	out := make([]RemoteEntry, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if name == "." || name == ".." {
			continue
		}
		out = append(out, RemoteEntry{Name: name, IsDir: entry.IsDir()})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].IsDir != out[j].IsDir {
			return out[i].IsDir
		}
		return out[i].Name < out[j].Name
	})
	return dir, out, nil
}

func (t SFTPTransferrer) dial(ctx context.Context, p ConnectParams) (*ssh.Client, *sftp.Client, error) {
	if p.User == "" {
		return nil, nil, fmt.Errorf("sshproxy: missing user")
	}
	if p.Host == "" {
		return nil, nil, fmt.Errorf("sshproxy: missing host")
	}
	port := p.Port
	if port == 0 {
		port = 22
	}

	auth, err := p.AuthMethods()
	if err != nil {
		return nil, nil, err
	}

	timeout := t.Timeout
	if timeout == 0 {
		timeout = dialTimeoutDefault
	}

	hostKey := t.HostKeyCallback
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
	dialer := &net.Dialer{Timeout: timeout}
	netConn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, nil, fmt.Errorf("sshproxy: dial %s: %w", addr, err)
	}

	sshConn, chans, reqs, err := ssh.NewClientConn(netConn, addr, cfg)
	if err != nil {
		_ = netConn.Close()
		return nil, nil, fmt.Errorf("sshproxy: handshake %s: %w", addr, err)
	}
	client := ssh.NewClient(sshConn, chans, reqs)

	sftpClient, err := sftp.NewClient(client)
	if err != nil {
		_ = client.Close()
		return nil, nil, fmt.Errorf("sshproxy: sftp client: %w", err)
	}
	return client, sftpClient, nil
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r *contextReader) Read(p []byte) (int, error) {
	select {
	case <-r.ctx.Done():
		return 0, r.ctx.Err()
	default:
	}
	return r.r.Read(p)
}

type progressWriter struct {
	ctx      context.Context
	w        io.Writer
	progress func(bytes int64)
	written  int64
}

func (w *progressWriter) Write(p []byte) (int, error) {
	select {
	case <-w.ctx.Done():
		return 0, w.ctx.Err()
	default:
	}
	n, err := w.w.Write(p)
	if n > 0 {
		w.written += int64(n)
		w.progress(w.written)
	}
	return n, err
}
