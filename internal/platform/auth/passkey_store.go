package auth

// passkey_store.go persists enrolled WebAuthn credentials to a JSON file and
// holds short-lived, single-use ceremony challenges in memory.
//
// Ported from reference/multissh/internal/auth/store.go: passkeyStore and
// challengeStore only. multissh's sessionStore is deliberately not ported --
// JWT sessions (session.go) replace it here, so there is no server-side
// session table to keep in sync.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// errNotFound is returned by store lookups when no record matches.
var errNotFound = errors.New("auth: not found")

// PasskeyCredential is a stored WebAuthn credential bound to an identity
// (the JWT subject the credential was enrolled under).
type PasskeyCredential struct {
	ID             string    `json:"id"`
	Username       string    `json:"username"`
	UserHandle     []byte    `json:"userHandle"`
	CredentialID   []byte    `json:"credentialID"`
	CredentialJSON []byte    `json:"credentialJSON"`
	FriendlyName   string    `json:"friendlyName"`
	CreatedAt      time.Time `json:"createdAt"`
	LastUsedAt     time.Time `json:"lastUsedAt"`
}

// challenge is a short-lived, single-use WebAuthn ceremony challenge held
// only in memory -- it is never persisted to disk. This is the one
// statefulness exception in the auth package: every other piece of ceremony
// state either lives in the JWT session cookie or the passkeys.json file.
type challenge struct {
	ID          string
	Username    string
	Ceremony    string
	SessionJSON []byte
	ExpiresAt   time.Time
}

type challengeStore struct {
	mu sync.Mutex
	m  map[string]challenge
}

func newChallengeStore() *challengeStore { return &challengeStore{m: map[string]challenge{}} }

func (s *challengeStore) put(c challenge) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[c.ID] = c
}

func (s *challengeStore) get(id string) (challenge, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.m[id]
	if !ok {
		return challenge{}, errNotFound
	}
	return c, nil
}

// delete removes id from the store. Callers use this to enforce single-use
// challenges: once a Begin/Finish pair has consumed a challenge (whether the
// Finish succeeded or failed), the challenge ID must never be usable again.
func (s *challengeStore) delete(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, id)
}

// passkeyStore persists WebAuthn credentials to a JSON side-car file so
// enrollments survive restarts, mirroring the host-store persistence
// pattern used elsewhere in this package (see key.go's loadOrCreateKey).
type passkeyStore struct {
	mu    sync.Mutex
	path  string
	creds []PasskeyCredential
}

// newPasskeyStore loads any existing credentials from path. path's
// directory is expected to already exist (the caller, passkey.go's
// newPasskeyService, creates it with mode 0750 before calling this).
func newPasskeyStore(path string) (*passkeyStore, error) {
	s := &passkeyStore{path: path, creds: []PasskeyCredential{}}
	if strings.TrimSpace(path) == "" {
		return s, nil
	}
	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		var payload struct {
			Passkeys []PasskeyCredential `json:"passkeys"`
		}
		if err := json.Unmarshal(data, &payload); err != nil {
			return nil, fmt.Errorf("auth: parse passkeys file: %w", err)
		}
		s.creds = payload.Passkeys
	case errors.Is(err, fs.ErrNotExist):
	default:
		return nil, fmt.Errorf("auth: read passkeys file: %w", err)
	}
	return s, nil
}

func (s *passkeyStore) put(c PasskeyCredential) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	replaced := false
	for i := range s.creds {
		if s.creds[i].ID == c.ID {
			s.creds[i] = c
			replaced = true
			break
		}
	}
	if !replaced {
		s.creds = append(s.creds, c)
	}
	return s.save()
}

func (s *passkeyStore) getByCredentialID(credentialID []byte) (PasskeyCredential, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.creds {
		if bytes.Equal(c.CredentialID, credentialID) {
			return c, nil
		}
	}
	return PasskeyCredential{}, errNotFound
}

func (s *passkeyStore) listByUsername(username string) ([]PasskeyCredential, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]PasskeyCredential, 0)
	for _, c := range s.creds {
		if c.Username == username {
			out = append(out, c)
		}
	}
	return out, nil
}

func (s *passkeyStore) delete(id, username string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.creds[:0:0]
	for _, c := range s.creds {
		if c.ID == id && c.Username == username {
			continue
		}
		out = append(out, c)
	}
	s.creds = out
	return s.save()
}

// save writes s.creds to s.path via a temp-file-then-rename, so a reader
// never observes a partially-written file. Both the temp file and (via
// rename, which preserves it) the final file land at mode 0600. Callers
// hold s.mu.
func (s *passkeyStore) save() error {
	if strings.TrimSpace(s.path) == "" {
		return nil
	}
	dir := filepath.Dir(s.path)
	tmp, err := os.CreateTemp(dir, filepath.Base(s.path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("auth: create temp passkeys file: %w", err)
	}
	tmpPath := tmp.Name()
	payload := struct {
		Passkeys []PasskeyCredential `json:"passkeys"`
	}{Passkeys: s.creds}
	if err := json.NewEncoder(tmp).Encode(payload); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("auth: encode passkeys file: %w", err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("auth: chmod temp passkeys file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("auth: close temp passkeys file: %w", err)
	}
	if err := os.Rename(tmpPath, s.path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("auth: rename passkeys file: %w", err)
	}
	return nil
}
