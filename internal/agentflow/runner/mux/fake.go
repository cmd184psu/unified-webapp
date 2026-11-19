package mux

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"
)

// Fake is a multiplexer for tests: each session is a real child process with a stdin pipe,
// no terminal. Typed text goes to stdin, "Enter" is a newline, and the screen is everything
// the program wrote. Its sessions belong to the Fake, not to an adapter, so a test can hand
// the same Fake to a second adapter to act out a restart.
type Fake struct {
	mu       sync.Mutex
	sessions map[string]*fakeSession
	// Typed records every SendText and SendKeys, per session, in order.
	Typed map[string][]string
}

type fakeSession struct {
	cmd   *exec.Cmd
	stdin io.WriteCloser
	out   *syncBuffer
	tags  map[string]string
	done  chan struct{}
}

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

var _ Multiplexer = (*Fake)(nil)

func (f *Fake) get(name string) (*fakeSession, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.sessions[name]
	return s, ok
}

func (f *Fake) record(name, what string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Typed == nil {
		f.Typed = map[string][]string{}
	}
	f.Typed[name] = append(f.Typed[name], what)
}

// TypedInto returns what was typed into a session.
func (f *Fake) TypedInto(name string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.Typed[name]...)
}

// Create starts the program as a child process.
func (f *Fake) Create(_ context.Context, s Spec) error {
	if !Valid(s.Name) {
		return fmt.Errorf("mux: invalid session name %q", s.Name)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.sessions == nil {
		f.sessions = map[string]*fakeSession{}
	}
	if _, ok := f.sessions[s.Name]; ok {
		return fmt.Errorf("mux: duplicate session %s", s.Name)
	}
	cmd := exec.Command(s.Argv[0], s.Argv[1:]...)
	cmd.Dir, cmd.Env = s.Dir, s.Env
	if cmd.Env == nil {
		cmd.Env = []string{}
	}
	in, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	out := &syncBuffer{}
	cmd.Stdout, cmd.Stderr = out, out
	cmd.WaitDelay = 100 * time.Millisecond // a killed shell's children may hold the pipe
	if err := cmd.Start(); err != nil {
		return err
	}
	tags := map[string]string{}
	for k, v := range s.Tags {
		tags[k] = v
	}
	fs := &fakeSession{cmd: cmd, stdin: in, out: out, tags: tags, done: make(chan struct{})}
	go func() { _ = cmd.Wait(); close(fs.done) }()
	f.sessions[s.Name] = fs
	return nil
}

// SendText writes text to the program's stdin.
func (f *Fake) SendText(_ context.Context, name, text string) error {
	s, ok := f.get(name)
	if !ok {
		return ErrNoSession
	}
	f.record(name, text)
	_, err := io.WriteString(s.stdin, text)
	return err
}

// SendKeys writes a newline for "Enter" and nothing for other keys.
func (f *Fake) SendKeys(_ context.Context, name string, keys ...string) error {
	s, ok := f.get(name)
	if !ok {
		return ErrNoSession
	}
	for _, k := range keys {
		f.record(name, "<"+k+">")
		if k == "Enter" {
			if _, err := io.WriteString(s.stdin, "\n"); err != nil {
				return err
			}
		}
	}
	return nil
}

// Capture returns everything the program wrote.
func (f *Fake) Capture(_ context.Context, name string, _ int) (string, error) {
	s, ok := f.get(name)
	if !ok {
		return "", ErrNoSession
	}
	return s.out.String(), nil
}

// List lists sessions by prefix.
func (f *Fake) List(_ context.Context, prefix string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for n := range f.sessions {
		if strings.HasPrefix(n, prefix) {
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out, nil
}

// Has says the session exists.
func (f *Fake) Has(_ context.Context, name string) (bool, error) {
	_, ok := f.get(name)
	return ok, nil
}

// Dead says the program has exited.
func (f *Fake) Dead(_ context.Context, name string) (bool, error) {
	s, ok := f.get(name)
	if !ok {
		return false, ErrNoSession
	}
	select {
	case <-s.done:
		return true, nil
	default:
		return false, nil
	}
}

// Tag reads a label.
func (f *Fake) Tag(_ context.Context, name, key string) (string, error) {
	s, ok := f.get(name)
	if !ok {
		return "", ErrNoSession
	}
	return s.tags[key], nil
}

// Kill ends the program and forgets the session.
func (f *Fake) Kill(_ context.Context, name string) error {
	f.mu.Lock()
	s, ok := f.sessions[name]
	delete(f.sessions, name)
	f.mu.Unlock()
	if !ok {
		return nil
	}
	_ = s.stdin.Close()
	if s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
	}
	<-s.done
	return nil
}

// AttachCommand names the fake session.
func (f *Fake) AttachCommand(name string) string { return "fake attach " + name }

// KillAll ends every session; tests call it in Cleanup.
func (f *Fake) KillAll() {
	names, _ := f.List(context.Background(), "")
	for _, n := range names {
		_ = f.Kill(context.Background(), n)
	}
}
