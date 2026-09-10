package sshproxy

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

const testPassword = "hunter2-do-not-leak"

// renderings is every way fmt or encoding/json could be asked to print a value.
// A password must survive none of them.
func renderings(t *testing.T, v any) map[string]string {
	t.Helper()
	out := map[string]string{
		"%v":  fmt.Sprintf("%v", v),
		"%+v": fmt.Sprintf("%+v", v),
		"%s":  fmt.Sprintf("%s", v),
		"%#v": fmt.Sprintf("%#v", v),
	}
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	out["json"] = string(b)
	return out
}

func assertRedacted(t *testing.T, label string, v any) {
	t.Helper()
	for verb, got := range renderings(t, v) {
		if strings.Contains(got, testPassword) {
			t.Errorf("%s rendered with %s leaked the password: %s", label, verb, got)
		}
		if !strings.Contains(got, secretMask) {
			t.Errorf("%s rendered with %s has no %q mask: %s", label, verb, secretMask, got)
		}
	}
}

func TestSecretRedactsEveryRendering(t *testing.T) {
	s := NewSecret(testPassword)
	assertRedacted(t, "bare Secret", s)
	if s.Reveal() != testPassword {
		t.Errorf("Reveal() = %q, want the true value", s.Reveal())
	}
}

// The redaction has to reach the wire structs, not just the credential type
// (task 2.3b) -- a %+v of a decoded request is the realistic leak path.
func TestSecretRedactedInDecodedConnectFrame(t *testing.T) {
	var msg clientMsg
	body := fmt.Sprintf(`{"type":"connect","host":"h","user":"u","password":%q}`, testPassword)
	if err := json.Unmarshal([]byte(body), &msg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if msg.Password.Reveal() != testPassword {
		t.Fatalf("password did not decode: %q", msg.Password.Reveal())
	}
	assertRedacted(t, "decoded clientMsg", msg)
	assertRedacted(t, "decoded *clientMsg", &msg)
}

func TestSecretUnmarshalRejectsNonString(t *testing.T) {
	var s Secret
	if err := json.Unmarshal([]byte(`42`), &s); err == nil {
		t.Error("expected an error decoding a number into Secret")
	}
}

// The mask must not round-trip back into a live credential: a document that was
// serialized with a redacted password decodes to an empty Secret, not to the
// literal "***".
func TestSecretMaskDoesNotRoundTripIntoACredential(t *testing.T) {
	b, err := json.Marshal(NewSecret(testPassword))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back Secret
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !back.IsZero() {
		t.Errorf("mask decoded into a live secret %q", back.Reveal())
	}
}

func TestSecretNullDecodesEmpty(t *testing.T) {
	s := NewSecret(testPassword)
	if err := json.Unmarshal([]byte(`null`), &s); err != nil {
		t.Fatalf("unmarshal null: %v", err)
	}
	if !s.IsZero() {
		t.Error("null should decode to an empty Secret")
	}
}

func TestSecretZero(t *testing.T) {
	s := NewSecret(testPassword)
	s.Zero()
	if !s.IsZero() || s.Reveal() != "" {
		t.Errorf("Zero() left %q", s.Reveal())
	}
}

// --- exactly-one-of credential resolution (task 2.3/2.4) ---

func TestAuthMethodsExactlyOneOf(t *testing.T) {
	keyPath := writeTestKey(t)

	cases := []struct {
		name    string
		params  ConnectParams
		wantErr bool
	}{
		{"password only", ConnectParams{Password: NewSecret(testPassword)}, false},
		{"key only", ConnectParams{KeyPath: keyPath}, false},
		{"neither", ConnectParams{}, true},
		{"both", ConnectParams{KeyPath: keyPath, Password: NewSecret(testPassword)}, true},
		{"blank key is not a key", ConnectParams{KeyPath: "   "}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			methods, err := tc.params.AuthMethods()
			if tc.wantErr {
				if !errors.Is(err, ErrCredential) {
					t.Fatalf("err = %v, want ErrCredential", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("AuthMethods: %v", err)
			}
			if len(methods) != 1 {
				t.Fatalf("got %d auth methods, want 1", len(methods))
			}
		})
	}
}

// A password credential must produce ssh.Password, not ssh.PublicKeys.
func TestAuthMethodsPasswordProducesPasswordMethod(t *testing.T) {
	p := ConnectParams{Password: NewSecret(testPassword)}
	methods, err := p.AuthMethods()
	if err != nil {
		t.Fatalf("AuthMethods: %v", err)
	}
	want := fmt.Sprintf("%T", ssh.Password(""))
	if got := fmt.Sprintf("%T", methods[0]); got != want {
		t.Errorf("auth method type = %s, want %s", got, want)
	}
}

func TestAuthMethodsKeyProducesPublicKeyMethod(t *testing.T) {
	p := ConnectParams{KeyPath: writeTestKey(t)}
	methods, err := p.AuthMethods()
	if err != nil {
		t.Fatalf("AuthMethods: %v", err)
	}
	if got := fmt.Sprintf("%T", methods[0]); strings.Contains(got, "passwordCallback") {
		t.Errorf("key credential produced a password method: %s", got)
	}
}

// FR-A2: passphrase-protected keys are not supported and must fail with a
// message that says so rather than a bare parse error.
func TestAuthMethodsRejectsEncryptedKey(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("gen key: %v", err)
	}
	block, err := ssh.MarshalPrivateKeyWithPassphrase(priv, "", []byte("correct horse"))
	if err != nil {
		t.Fatalf("marshal encrypted key: %v", err)
	}
	path := filepath.Join(t.TempDir(), "id_encrypted")
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, err = (ConnectParams{KeyPath: path}).AuthMethods()
	if err == nil {
		t.Fatal("expected an error for a passphrase-protected key")
	}
	if !strings.Contains(err.Error(), "encrypted keys are not supported") {
		t.Errorf("error does not explain the limitation: %v", err)
	}
}

// --- FR-N4 lifetime bound (task 2.4b) ---

func TestSessionCredentialZeroedOnClose(t *testing.T) {
	s := &session{}
	s.cred = NewSecret(testPassword)
	if s.credential().Reveal() != testPassword {
		t.Fatal("setup: session should hold the password while connected")
	}
	s.closeConn()
	if got := s.credential().Reveal(); got != "" {
		t.Errorf("closed session still holds %q", got)
	}
}

// writeTestKey writes a usable unencrypted private key and returns its path,
// reusing the ed25519 fixture the session suite already generates.
func writeTestKey(t *testing.T) string {
	t.Helper()
	dir, name, _ := writeClientKey(t)
	return filepath.Join(dir, name)
}
