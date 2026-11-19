package mux

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// Tmux is the tmux multiplexer. It uses the person's default tmux server, so a plain
// `tmux attach -t <name>` reaches the session. Sessions are made by tmux's server, not as
// children of agentflow, so they survive an agentflow restart.
type Tmux struct {
	// Bin is the tmux binary; empty finds tmux on PATH or in /opt/homebrew/bin.
	Bin string
}

var _ Multiplexer = (*Tmux)(nil)

func (t *Tmux) bin() string {
	if t.Bin != "" {
		return t.Bin
	}
	if p, err := exec.LookPath("tmux"); err == nil {
		return p
	}
	for _, p := range []string{"/opt/homebrew/bin/tmux", "/usr/local/bin/tmux", "/usr/bin/tmux"} {
		if _, err := exec.LookPath(p); err == nil {
			return p
		}
	}
	return "tmux"
}

func (t *Tmux) run(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, t.bin(), args...)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		if strings.Contains(msg, "can't find session") || strings.Contains(msg, "no server running") ||
			strings.Contains(msg, "session not found") || strings.Contains(msg, "error connecting") ||
			strings.Contains(msg, "no such session") || strings.Contains(msg, "can't find pane") || strings.Contains(msg, "can't find window") {
			return "", ErrNoSession
		}
		return "", fmt.Errorf("tmux %s: %v: %s", args[0], err, msg)
	}
	return out.String(), nil
}

// session is the exact-match session target ("=name" stops tmux matching a prefix), for
// has-session and kill-session; pane is the same session's current pane, which the commands
// that take a window or pane target need (tmux reads a bare "=name" there as a window name).
func session(name string) string { return "=" + name }
func pane(name string) string    { return "=" + name + ":" }

// Create starts the program under `env -i`, so the session carries exactly s.Env and none of
// the tmux server's environment.
func (t *Tmux) Create(ctx context.Context, s Spec) error {
	if !Valid(s.Name) {
		return fmt.Errorf("mux: invalid session name %q", s.Name)
	}
	if len(s.Argv) == 0 {
		return fmt.Errorf("mux: session %s has no program", s.Name)
	}
	args := []string{"new-session", "-d", "-s", s.Name, "-x", "200", "-y", "50"}
	if s.Dir != "" {
		args = append(args, "-c", s.Dir)
	}
	cmdline := append(append([]string{"/usr/bin/env", "-i"}, s.Env...), s.Argv...)
	size := 0
	for _, a := range cmdline {
		size += len(a) + 1
	}
	if size > maxInline {
		if s.ScriptDir == "" {
			return fmt.Errorf("mux: the command for %s is %d bytes, too long for tmux; set ScriptDir", s.Name, size)
		}
		script := filepath.Join(s.ScriptDir, s.Name+".launch.sh")
		if err := os.WriteFile(script, []byte("#!/bin/sh\nexec "+shellJoin(cmdline)+"\n"), 0o700); err != nil {
			return err
		}
		cmdline = []string{"/bin/sh", script}
	}
	args = append(args, "--")
	args = append(args, cmdline...)
	if _, err := t.run(ctx, args...); err != nil {
		return err
	}
	// Keep the pane after the program exits, so its last screen can be read and the stage
	// can tell a crash from a finish; the session is killed by agentflow afterwards.
	_, _ = t.run(ctx, "set-option", "-w", "-t", pane(s.Name), "remain-on-exit", "on")
	keys := make([]string, 0, len(s.Tags))
	for k := range s.Tags {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if _, err := t.run(ctx, "set-option", "-t", pane(s.Name), "@"+k, s.Tags[k]); err != nil {
			_ = t.Kill(ctx, s.Name)
			return err
		}
	}
	return nil
}

// SendText types text literally (send-keys -l).
func (t *Tmux) SendText(ctx context.Context, name, text string) error {
	_, err := t.run(ctx, "send-keys", "-t", pane(name), "-l", "--", text)
	return err
}

// SendKeys presses named keys.
func (t *Tmux) SendKeys(ctx context.Context, name string, keys ...string) error {
	if len(keys) == 0 {
		return nil
	}
	_, err := t.run(ctx, append([]string{"send-keys", "-t", pane(name)}, keys...)...)
	return err
}

// Capture prints the pane with history lines of scrollback.
func (t *Tmux) Capture(ctx context.Context, name string, history int) (string, error) {
	return t.run(ctx, "capture-pane", "-p", "-J", "-t", pane(name), "-S", fmt.Sprintf("-%d", max(history, 0)))
}

// List lists sessions whose name starts with prefix. No tmux server means no sessions.
func (t *Tmux) List(ctx context.Context, prefix string) ([]string, error) {
	out, err := t.run(ctx, "list-sessions", "-F", "#{session_name}")
	if err == ErrNoSession {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var names []string
	for _, l := range strings.Split(out, "\n") {
		if l = strings.TrimSpace(l); l != "" && strings.HasPrefix(l, prefix) {
			names = append(names, l)
		}
	}
	return names, nil
}

// Has asks tmux whether the session exists.
func (t *Tmux) Has(ctx context.Context, name string) (bool, error) {
	_, err := t.run(ctx, "has-session", "-t", session(name))
	if err == ErrNoSession {
		return false, nil
	}
	if err != nil {
		// has-session exits 1 with an empty or varying message when the session is missing.
		if strings.Contains(err.Error(), "exit status 1") {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// Dead says the program in the session's pane has exited (the pane is kept by remain-on-exit).
func (t *Tmux) Dead(ctx context.Context, name string) (bool, error) {
	out, err := t.run(ctx, "display-message", "-p", "-t", pane(name), "#{pane_dead}")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) == "1", nil
}

// Tag reads a session user option.
func (t *Tmux) Tag(ctx context.Context, name, key string) (string, error) {
	out, err := t.run(ctx, "show-options", "-v", "-t", pane(name), "@"+key)
	if err != nil {
		if err == ErrNoSession {
			return "", err
		}
		return "", nil // unset option
	}
	return strings.TrimSpace(out), nil
}

// Kill ends the session.
func (t *Tmux) Kill(ctx context.Context, name string) error {
	_, err := t.run(ctx, "kill-session", "-t", session(name))
	if err == ErrNoSession {
		return nil
	}
	return err
}

// AttachCommand is `tmux attach -t <name>`.
func (t *Tmux) AttachCommand(name string) string { return "tmux attach -t " + name }

// maxInline is the longest command tmux is given on its command line; tmux passes the whole
// new-session request to its server in one message of at most 16 KiB.
const maxInline = 8 << 10

// shellJoin quotes each word for /bin/sh.
func shellJoin(words []string) string {
	q := make([]string, len(words))
	for i, w := range words {
		q[i] = "'" + strings.ReplaceAll(w, "'", `'\''`) + "'"
	}
	return strings.Join(q, " ")
}
