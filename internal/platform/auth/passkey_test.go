package auth

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"

	"cmd184psu/unified-webapp/internal/platform/config"
)

// testPasskeyConfig returns a config.PasskeyConfig whose rp_id is a literal,
// distinctive string -- tests assert this exact literal comes back out of
// generated ceremony options, never a variable that would pass trivially
// even if the wiring dropped the configured value on the floor.
func testPasskeyConfig() config.PasskeyConfig {
	return config.PasskeyConfig{
		RPID:      "example.test",
		RPOrigins: []string{"https://example.test"},
	}
}

func newTestPasskeyService(t *testing.T, cfg config.PasskeyConfig, now func() time.Time) *passkeyService {
	t.Helper()
	ps, err := newPasskeyService(cfg, t.TempDir(), now)
	if err != nil {
		t.Fatalf("newPasskeyService: %v", err)
	}
	return ps
}

// injectTestCredential writes a syntactically-valid (but not
// cryptographically real) enrolled credential directly into ps's store,
// bypassing the registration ceremony. This is necessary because
// go-webauthn's Finish* methods verify a real signature over the
// authenticator data and client data hash -- producing one without an
// actual (hardware or software) authenticator is out of scope here (see
// TestPasskeyLoginE2E). BeginLogin/BeginRegistration, by contrast, only
// need a credential ID (for the exclude/allow list), so an injected
// credential is sufficient to exercise them.
func injectTestCredential(t *testing.T, ps *passkeyService, identity string) PasskeyCredential {
	t.Helper()
	cred := webauthn.Credential{
		ID:        []byte("cred-" + identity),
		PublicKey: []byte{0x01, 0x02, 0x03},
	}
	credJSON, err := json.Marshal(cred)
	if err != nil {
		t.Fatalf("marshal fake credential: %v", err)
	}
	c := PasskeyCredential{
		ID:             newPasskeyID("pkey"),
		Username:       identity,
		UserHandle:     []byte("handle-" + identity),
		CredentialID:   cred.ID,
		CredentialJSON: credJSON,
		FriendlyName:   "test device",
		CreatedAt:      time.Now(),
	}
	if err := ps.passkeys.put(c); err != nil {
		t.Fatalf("put fake credential: %v", err)
	}
	return c
}

func TestBeginPasskeyRegistrationCarriesLiteralRPID(t *testing.T) {
	ps := newTestPasskeyService(t, testPasskeyConfig(), time.Now)

	ceremony, err := ps.BeginPasskeyRegistration("alice", "laptop")
	if err != nil {
		t.Fatalf("BeginPasskeyRegistration: %v", err)
	}
	if ceremony.ChallengeID == "" {
		t.Fatal("ChallengeID is empty")
	}
	creation, ok := ceremony.Options.(*protocol.CredentialCreation)
	if !ok {
		t.Fatalf("Options type = %T, want *protocol.CredentialCreation", ceremony.Options)
	}
	if creation.Response.RelyingParty.ID != "example.test" {
		t.Errorf("RelyingParty.ID = %q, want the literal configured rp_id %q", creation.Response.RelyingParty.ID, "example.test")
	}
}

func TestBeginPasskeyLoginCarriesLiteralRPID(t *testing.T) {
	ps := newTestPasskeyService(t, testPasskeyConfig(), time.Now)
	injectTestCredential(t, ps, "alice")

	ceremony, err := ps.BeginPasskeyLogin("alice")
	if err != nil {
		t.Fatalf("BeginPasskeyLogin: %v", err)
	}
	if ceremony.ChallengeID == "" {
		t.Fatal("ChallengeID is empty")
	}
	assertion, ok := ceremony.Options.(*protocol.CredentialAssertion)
	if !ok {
		t.Fatalf("Options type = %T, want *protocol.CredentialAssertion", ceremony.Options)
	}
	if assertion.Response.RelyingPartyID != "example.test" {
		t.Errorf("RelyingPartyID = %q, want the literal configured rp_id %q", assertion.Response.RelyingPartyID, "example.test")
	}
}

func TestBeginPasskeyLoginNoCredentials(t *testing.T) {
	ps := newTestPasskeyService(t, testPasskeyConfig(), time.Now)

	if _, err := ps.BeginPasskeyLogin("nobody"); !errors.Is(err, ErrPasskeyNoCredentials) {
		t.Fatalf("BeginPasskeyLogin(nobody) error = %v, want ErrPasskeyNoCredentials", err)
	}
}

// TestFinishPasskeyRegistrationChallengeSingleUse proves a challenge cannot
// be replayed: the first Finish call consumes it (whether or not the
// browser-supplied credential itself passes verification), so a second call
// with the same challenge ID is rejected outright rather than re-attempting
// verification.
func TestFinishPasskeyRegistrationChallengeSingleUse(t *testing.T) {
	ps := newTestPasskeyService(t, testPasskeyConfig(), time.Now)

	ceremony, err := ps.BeginPasskeyRegistration("alice", "laptop")
	if err != nil {
		t.Fatalf("BeginPasskeyRegistration: %v", err)
	}

	garbage := []byte(`{}`)
	if _, err := ps.FinishPasskeyRegistration(context.Background(), "alice", ceremony.ChallengeID, "laptop", garbage); err == nil {
		t.Fatal("first FinishPasskeyRegistration with a garbage credential unexpectedly succeeded")
	}

	_, err = ps.FinishPasskeyRegistration(context.Background(), "alice", ceremony.ChallengeID, "laptop", garbage)
	if !errors.Is(err, ErrPasskeyChallengeInvalid) {
		t.Fatalf("second FinishPasskeyRegistration with a consumed challenge = %v, want ErrPasskeyChallengeInvalid", err)
	}
}

func TestFinishPasskeyLoginChallengeSingleUse(t *testing.T) {
	ps := newTestPasskeyService(t, testPasskeyConfig(), time.Now)
	injectTestCredential(t, ps, "alice")

	ceremony, err := ps.BeginPasskeyLogin("alice")
	if err != nil {
		t.Fatalf("BeginPasskeyLogin: %v", err)
	}

	garbage := []byte(`{}`)
	if _, err := ps.FinishPasskeyLogin(context.Background(), ceremony.ChallengeID, garbage); err == nil {
		t.Fatal("first FinishPasskeyLogin with a garbage assertion unexpectedly succeeded")
	}

	_, err = ps.FinishPasskeyLogin(context.Background(), ceremony.ChallengeID, garbage)
	if !errors.Is(err, ErrPasskeyChallengeInvalid) {
		t.Fatalf("second FinishPasskeyLogin with a consumed challenge = %v, want ErrPasskeyChallengeInvalid", err)
	}
}

// TestFinishPasskeyRegistrationChallengeExpiry drives the service's clock
// forward with an injected fakeClock (defined in throttle_test.go, shared
// within package auth) past passkeyChallengeTTL -- no test ever sleeps.
func TestFinishPasskeyRegistrationChallengeExpiry(t *testing.T) {
	clk := newFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	ps := newTestPasskeyService(t, testPasskeyConfig(), clk.now)

	ceremony, err := ps.BeginPasskeyRegistration("alice", "laptop")
	if err != nil {
		t.Fatalf("BeginPasskeyRegistration: %v", err)
	}

	clk.advance(passkeyChallengeTTL + time.Second)

	_, err = ps.FinishPasskeyRegistration(context.Background(), "alice", ceremony.ChallengeID, "laptop", []byte(`{}`))
	if !errors.Is(err, ErrPasskeyChallengeInvalid) {
		t.Fatalf("FinishPasskeyRegistration after TTL expiry = %v, want ErrPasskeyChallengeInvalid", err)
	}
}

func TestFinishPasskeyLoginChallengeExpiry(t *testing.T) {
	clk := newFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	ps := newTestPasskeyService(t, testPasskeyConfig(), clk.now)
	injectTestCredential(t, ps, "alice")

	ceremony, err := ps.BeginPasskeyLogin("alice")
	if err != nil {
		t.Fatalf("BeginPasskeyLogin: %v", err)
	}

	clk.advance(passkeyChallengeTTL + time.Second)

	_, err = ps.FinishPasskeyLogin(context.Background(), ceremony.ChallengeID, []byte(`{}`))
	if !errors.Is(err, ErrPasskeyChallengeInvalid) {
		t.Fatalf("FinishPasskeyLogin after TTL expiry = %v, want ErrPasskeyChallengeInvalid", err)
	}
}

// TestPasskeyServiceUsesConfiguredOrigins covers the foreign-origin
// rejection requirement via configuration plumbing rather than a forged
// assertion. Rejecting an assertion whose collected client data names an
// origin outside RPOrigins is go-webauthn's own, already-tested job inside
// FinishLogin/FinishRegistration; exercising that here would require
// building a complete, signature-valid attestation or assertion object
// (a real or emulated authenticator producing an ES256/EdDSA signature
// over the authenticator data and client data hash), which is
// impractical without hardware. Instead this test proves the plumbing
// that feeds go-webauthn's origin check is correct: webauthn.New is
// constructed with exactly the configured RPOrigins, no more and no
// fewer, so go-webauthn's own origin validation is checking against the
// operator's actual configuration.
func TestPasskeyServiceUsesConfiguredOrigins(t *testing.T) {
	origins := []string{"https://example.test", "https://other.example.test"}
	ps := newTestPasskeyService(t, config.PasskeyConfig{RPID: "example.test", RPOrigins: origins}, time.Now)

	if !reflect.DeepEqual(ps.webauthn.Config.RPOrigins, origins) {
		t.Fatalf("webauthn.Config.RPOrigins = %v, want %v", ps.webauthn.Config.RPOrigins, origins)
	}
}

func TestPasskeyStoreRoundTripThroughService(t *testing.T) {
	dir := t.TempDir()

	ps1 := mustNewPasskeyService(t, dir)
	cred := injectTestCredential(t, ps1, "alice")

	path := filepath.Join(dir, passkeyDataFileName)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("passkeys.json mode = %#o, want 0600", info.Mode().Perm())
	}

	ps2 := mustNewPasskeyService(t, dir)
	list, err := ps2.ListPasskeys("alice")
	if err != nil {
		t.Fatalf("ListPasskeys after reopen: %v", err)
	}
	if len(list) != 1 || list[0].ID != cred.ID {
		t.Fatalf("ListPasskeys after reopen = %+v, want one entry with ID %q", list, cred.ID)
	}

	if err := ps2.DeletePasskey("alice", cred.ID); err != nil {
		t.Fatalf("DeletePasskey: %v", err)
	}

	ps3 := mustNewPasskeyService(t, dir)
	list, err = ps3.ListPasskeys("alice")
	if err != nil {
		t.Fatalf("ListPasskeys after reopen post-delete: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("ListPasskeys after reopen post-delete = %+v, want empty", list)
	}
}

func mustNewPasskeyService(t *testing.T, dataDir string) *passkeyService {
	t.Helper()
	ps, err := newPasskeyService(testPasskeyConfig(), dataDir, time.Now)
	if err != nil {
		t.Fatalf("newPasskeyService: %v", err)
	}
	return ps
}

func TestNewPasskeyServiceCreatesDataDir(t *testing.T) {
	base := t.TempDir()
	dataDir := filepath.Join(base, "nested", "auth")

	if _, err := newPasskeyService(testPasskeyConfig(), dataDir, time.Now); err != nil {
		t.Fatalf("newPasskeyService: %v", err)
	}

	info, err := os.Stat(dataDir)
	if err != nil {
		t.Fatalf("stat dataDir: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("dataDir %s is not a directory", dataDir)
	}
	if info.Mode().Perm() != 0750 {
		t.Errorf("dataDir mode = %#o, want 0750", info.Mode().Perm())
	}
}

func TestPasskeyCeremoniesRequireIdentity(t *testing.T) {
	ps := newTestPasskeyService(t, testPasskeyConfig(), time.Now)

	if _, err := ps.ListPasskeys(""); !errors.Is(err, ErrPasskeyIdentityRequired) {
		t.Errorf("ListPasskeys(\"\") error = %v, want ErrPasskeyIdentityRequired", err)
	}
	if _, err := ps.BeginPasskeyRegistration("", "laptop"); !errors.Is(err, ErrPasskeyIdentityRequired) {
		t.Errorf("BeginPasskeyRegistration(\"\") error = %v, want ErrPasskeyIdentityRequired", err)
	}
	if _, err := ps.FinishPasskeyRegistration(context.Background(), "", "chal", "laptop", nil); !errors.Is(err, ErrPasskeyIdentityRequired) {
		t.Errorf("FinishPasskeyRegistration(\"\") error = %v, want ErrPasskeyIdentityRequired", err)
	}
	if err := ps.DeletePasskey("", "pkey_1"); !errors.Is(err, ErrPasskeyIdentityRequired) {
		t.Errorf("DeletePasskey(\"\") error = %v, want ErrPasskeyIdentityRequired", err)
	}
}

func TestFinishPasskeyRegistrationWrongIdentityRejected(t *testing.T) {
	ps := newTestPasskeyService(t, testPasskeyConfig(), time.Now)

	ceremony, err := ps.BeginPasskeyRegistration("alice", "laptop")
	if err != nil {
		t.Fatalf("BeginPasskeyRegistration: %v", err)
	}

	_, err = ps.FinishPasskeyRegistration(context.Background(), "mallory", ceremony.ChallengeID, "laptop", []byte(`{}`))
	if !errors.Is(err, ErrPasskeyChallengeInvalid) {
		t.Fatalf("FinishPasskeyRegistration with mismatched identity = %v, want ErrPasskeyChallengeInvalid", err)
	}
}

// TestPasskeyLoginE2E documents the one ceremony step this file cannot
// drive without real hardware: verifying a genuine, signed assertion from a
// physical or platform authenticator end to end.
func TestPasskeyLoginE2E(t *testing.T) {
	t.Skip("requires a hardware/platform authenticator")
}
