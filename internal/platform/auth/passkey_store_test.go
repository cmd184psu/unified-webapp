package auth

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestPasskeyStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "passkeys.json")

	s1, err := newPasskeyStore(path)
	if err != nil {
		t.Fatalf("newPasskeyStore: %v", err)
	}
	c := PasskeyCredential{ID: "pkey_1", Username: "alice", CredentialID: []byte("cred-1"), FriendlyName: "laptop"}
	if err := s1.put(c); err != nil {
		t.Fatalf("put: %v", err)
	}

	s2, err := newPasskeyStore(path)
	if err != nil {
		t.Fatalf("newPasskeyStore (reopen): %v", err)
	}
	got, err := s2.getByCredentialID([]byte("cred-1"))
	if err != nil {
		t.Fatalf("getByCredentialID after reopen: %v", err)
	}
	if got.ID != c.ID || got.Username != c.Username {
		t.Fatalf("getByCredentialID after reopen = %+v, want %+v", got, c)
	}

	list, err := s2.listByUsername("alice")
	if err != nil {
		t.Fatalf("listByUsername: %v", err)
	}
	if len(list) != 1 || list[0].ID != c.ID {
		t.Fatalf("listByUsername = %+v, want one entry with ID %q", list, c.ID)
	}
}

func TestPasskeyStoreFileMode0600(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "passkeys.json")

	s, err := newPasskeyStore(path)
	if err != nil {
		t.Fatalf("newPasskeyStore: %v", err)
	}
	if err := s.put(PasskeyCredential{ID: "pkey_1", Username: "alice", CredentialID: []byte("cred-1")}); err != nil {
		t.Fatalf("put: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("passkeys.json mode = %#o, want 0600", info.Mode().Perm())
	}
}

func TestPasskeyStoreDeletePersists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "passkeys.json")

	s1, err := newPasskeyStore(path)
	if err != nil {
		t.Fatalf("newPasskeyStore: %v", err)
	}
	a := PasskeyCredential{ID: "pkey_a", Username: "alice", CredentialID: []byte("cred-a")}
	b := PasskeyCredential{ID: "pkey_b", Username: "alice", CredentialID: []byte("cred-b")}
	if err := s1.put(a); err != nil {
		t.Fatalf("put a: %v", err)
	}
	if err := s1.put(b); err != nil {
		t.Fatalf("put b: %v", err)
	}
	if err := s1.delete(a.ID, "alice"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	s2, err := newPasskeyStore(path)
	if err != nil {
		t.Fatalf("newPasskeyStore (reopen): %v", err)
	}
	list, err := s2.listByUsername("alice")
	if err != nil {
		t.Fatalf("listByUsername: %v", err)
	}
	if len(list) != 1 || list[0].ID != b.ID {
		t.Fatalf("listByUsername after delete+reopen = %+v, want only %q", list, b.ID)
	}
}

func TestPasskeyStoreGetByCredentialIDNotFound(t *testing.T) {
	s, err := newPasskeyStore(filepath.Join(t.TempDir(), "passkeys.json"))
	if err != nil {
		t.Fatalf("newPasskeyStore: %v", err)
	}
	if _, err := s.getByCredentialID([]byte("nope")); err != errNotFound {
		t.Fatalf("getByCredentialID(unknown) error = %v, want errNotFound", err)
	}
}

func TestChallengeStoreGetMissing(t *testing.T) {
	cs := newChallengeStore()
	if _, err := cs.get("nope"); err != errNotFound {
		t.Fatalf("get(unknown) error = %v, want errNotFound", err)
	}
}

func TestChallengeStorePutGetDelete(t *testing.T) {
	cs := newChallengeStore()
	c := challenge{ID: "wchal_1", Username: "alice", Ceremony: ceremonyRegister, SessionJSON: []byte(`{}`)}
	cs.put(c)

	got, err := cs.get(c.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Username != "alice" || !bytes.Equal(got.SessionJSON, []byte(`{}`)) {
		t.Fatalf("get = %+v, want %+v", got, c)
	}

	cs.delete(c.ID)
	if _, err := cs.get(c.ID); err != errNotFound {
		t.Fatalf("get after delete = %v, want errNotFound", err)
	}
}

// TestPasskeyCredentialJSONRoundTrip is a sanity check that PasskeyCredential
// (in particular its []byte fields) survives the same base64-in-JSON
// encoding passkeyStore.save/newPasskeyStore rely on.
func TestPasskeyCredentialJSONRoundTrip(t *testing.T) {
	c := PasskeyCredential{
		ID:             "pkey_1",
		Username:       "alice",
		UserHandle:     []byte{1, 2, 3, 4},
		CredentialID:   []byte{5, 6, 7, 8},
		CredentialJSON: []byte(`{"id":"x"}`),
		FriendlyName:   "laptop",
	}
	b, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var got PasskeyCredential
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !bytes.Equal(got.UserHandle, c.UserHandle) || !bytes.Equal(got.CredentialID, c.CredentialID) {
		t.Fatalf("round trip = %+v, want %+v", got, c)
	}
}
