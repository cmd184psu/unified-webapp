// Package sshtest runs a minimal in-process SSH server for tests. Each "exec"
// request is answered by a Handler, so a test can script what a remote
// machine replies to `uname -s`, `cat /etc/os-release`, and so on, without a
// real host. Only password auth is offered.
package sshtest

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"io"
	"net"
	"strconv"
	"sync"
	"testing"

	"golang.org/x/crypto/ssh"
)

// Handler answers one command. stdin is everything the client sent before
// closing its input. The return values become the command's combined output
// and exit status.
type Handler func(cmd string, stdin []byte) (output string, exitStatus int)

// Server is a running test server.
type Server struct {
	Host string
	Port int

	mu   sync.Mutex
	cmds []string
}

// Commands returns every command the server has received, in order.
func (s *Server) Commands() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.cmds...)
}

// Start launches a server on 127.0.0.1 that accepts user/password and answers
// commands with h. It stops when the test ends.
func Start(t testing.TB, user, password string, h Handler) *Server {
	t.Helper()
	_, hostPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("sshtest: host key: %v", err)
	}
	hostSigner, err := ssh.NewSignerFromKey(hostPriv)
	if err != nil {
		t.Fatalf("sshtest: host signer: %v", err)
	}
	cfg := &ssh.ServerConfig{
		PasswordCallback: func(c ssh.ConnMetadata, pw []byte) (*ssh.Permissions, error) {
			if c.User() == user && string(pw) == password {
				return &ssh.Permissions{}, nil
			}
			return nil, errUnauthorized
		},
	}
	cfg.AddHostKey(hostSigner)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("sshtest: listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	host, portStr, _ := net.SplitHostPort(ln.Addr().String())
	port, _ := strconv.Atoi(portStr)
	s := &Server{Host: host, Port: port}

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go s.serve(conn, cfg, h)
		}
	}()
	return s
}

type unauthorized struct{}

func (unauthorized) Error() string { return "sshtest: unauthorized" }

var errUnauthorized error = unauthorized{}

func (s *Server) serve(conn net.Conn, cfg *ssh.ServerConfig, h Handler) {
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
		go s.session(ch, chReqs, h)
	}
}

func (s *Server) session(ch ssh.Channel, reqs <-chan *ssh.Request, h Handler) {
	defer ch.Close()
	for req := range reqs {
		if req.Type != "exec" {
			_ = req.Reply(false, nil)
			continue
		}
		// payload: uint32 length + command string
		if len(req.Payload) < 4 {
			_ = req.Reply(false, nil)
			return
		}
		n := binary.BigEndian.Uint32(req.Payload[:4])
		cmd := string(req.Payload[4 : 4+n])
		_ = req.Reply(true, nil)

		s.mu.Lock()
		s.cmds = append(s.cmds, cmd)
		s.mu.Unlock()

		stdin, _ := io.ReadAll(ch)
		out, status := h(cmd, stdin)
		_, _ = io.WriteString(ch, out)
		exit := make([]byte, 4)
		binary.BigEndian.PutUint32(exit, uint32(status))
		_, _ = ch.SendRequest("exit-status", false, exit)
		return
	}
}
