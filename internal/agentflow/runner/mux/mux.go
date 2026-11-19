// Package mux is the terminal multiplexer an interactive seat runs in. Agentflow starts a
// seat's program in a named session, types into it, reads its screen, and leaves it running
// when the process restarts. Nothing else in agentflow talks to tmux: the backend is chosen
// by the seat_mux setting, so another multiplexer can replace tmux behind this interface.
package mux

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// ErrNoSession is returned for a session that does not exist.
var ErrNoSession = errors.New("mux: no such session")

// Spec is one session to create.
type Spec struct {
	// Name is the session name; Valid says which names a multiplexer accepts.
	Name string
	// Dir is the working directory of the program.
	Dir string
	// Argv is the program and its arguments.
	Argv []string
	// Env is the program's whole environment (KEY=VALUE); nothing else is inherited.
	Env []string
	// Tags are labels stored on the session (instance id, stage id), read back with Tag.
	Tags map[string]string
	// ScriptDir, when set, is a private directory where a multiplexer that cannot pass a long
	// command line (tmux sends it to its server in one size-limited message) writes a
	// launcher script instead.
	ScriptDir string
}

// Multiplexer runs programs in named sessions that outlive the process that made them.
type Multiplexer interface {
	// Create starts Argv in a new detached session.
	Create(ctx context.Context, s Spec) error
	// SendText types text into the session literally, with no key interpretation.
	SendText(ctx context.Context, name, text string) error
	// SendKeys presses named keys ("Enter", "Escape", "C-c").
	SendKeys(ctx context.Context, name string, keys ...string) error
	// Capture returns the visible screen plus up to history lines of scrollback.
	Capture(ctx context.Context, name string, history int) (string, error)
	// List returns the names of the live sessions that start with prefix.
	List(ctx context.Context, prefix string) ([]string, error)
	// Has says the session exists.
	Has(ctx context.Context, name string) (bool, error)
	// Dead says the session exists but its program has exited.
	Dead(ctx context.Context, name string) (bool, error)
	// Tag reads a label stored on the session at Create; "" when unset.
	Tag(ctx context.Context, name, key string) (string, error)
	// Kill ends the session and its program. A missing session is not an error.
	Kill(ctx context.Context, name string) error
	// AttachCommand is the shell command a person runs to watch and type into the session.
	AttachCommand(name string) string
}

// Valid reports whether name is a safe session name: letters, digits, '-' and '_' only.
// tmux treats '.' and ':' as target separators, so they are never allowed.
func Valid(name string) bool {
	if name == "" || len(name) > 200 {
		return false
	}
	for _, r := range name {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

// Clean turns s into a name fragment Valid accepts: other characters become '-'.
func Clean(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

// New returns the multiplexer named kind; "" and "tmux" are tmux.
func New(kind string) (Multiplexer, error) {
	switch strings.TrimSpace(kind) {
	case "", "tmux":
		return &Tmux{}, nil
	}
	return nil, fmt.Errorf("mux: unknown multiplexer %q", kind)
}
