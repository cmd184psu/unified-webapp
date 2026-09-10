package sshproxy

import (
	"encoding/json"
	"errors"
)

// secretMask is what a Secret renders as through every formatting and
// serialization path. It is deliberately not the empty string: a redacted
// value should be visibly redacted in a log line, not silently missing.
const secretMask = "***"

// Secret holds an SSH password for the life of a session or broadcast job and
// nothing longer (FR-N4). The value is unexported and reachable only through
// Reveal, and every path fmt or encoding/json could take to it is overridden to
// emit secretMask instead. It decodes from the wire normally so a browser can
// send one, but it cannot be re-encoded or formatted in the clear.
//
// Secret is a struct, not a string alias, so `omitempty` is inert on it. That
// is intentional: a Secret field must never be placed on a type that is written
// to disk (see hostConfig in the multissh package), and the absence of a
// suppression mechanism makes that a design constraint rather than an option.
type Secret struct {
	v string
}

// NewSecret wraps a plaintext password.
func NewSecret(v string) Secret { return Secret{v: v} }

// Reveal returns the plaintext. This is the only way out, and the only caller
// that should use it is the code building an ssh.AuthMethod.
func (s Secret) Reveal() string { return s.v }

// IsZero reports whether no password is held.
func (s Secret) IsZero() bool { return s.v == "" }

// Zero erases the held value. Callers holding a Secret past the life of the
// session or job it belongs to must call this (FR-N4).
func (s *Secret) Zero() { s.v = "" }

func (s Secret) String() string { return secretMask }

func (s Secret) GoString() string { return secretMask }

// MarshalJSON always emits the mask, so a Secret cannot reach a response body,
// a persisted file, or a structured log line in the clear.
func (s Secret) MarshalJSON() ([]byte, error) { return []byte(`"` + secretMask + `"`), nil }

// UnmarshalJSON accepts a JSON string or null. The mask decodes to an empty
// Secret rather than to the literal "***", so a round-tripped document can
// never resurrect a fake credential.
func (s *Secret) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		s.v = ""
		return nil
	}
	var v string
	if err := json.Unmarshal(data, &v); err != nil {
		return errors.New("password must be a string")
	}
	if v == secretMask {
		v = ""
	}
	s.v = v
	return nil
}
