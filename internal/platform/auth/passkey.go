// passkey.go runs WebAuthn/passkey enrollment and login ceremonies (FR-A8).
//
// Ported from reference/multissh/internal/auth/service.go: BeginPasskeyRegistration,
// FinishPasskeyRegistration, BeginPasskeyLogin, FinishPasskeyLogin, ListPasskeys, and
// DeletePasskey, plus their unexported helpers (passkeyUserFrom, userHandleFor,
// ceremonyRequest).
//
// Rewiring versus the reference: multissh keyed ceremonies by a sessionID looked up in
// its own sessionStore. Here identity comes from the gate-verified JWT session (see
// session.go) -- a caller (the auth gate, wired up in a later task) extracts identity
// from the session cookie and passes it in explicitly. Registration, listing, and
// deletion therefore take an identity string directly instead of a session ID; they
// only require it to be non-empty; enforcing that registration requires an existing
// session is the gate's job, not this file's. FinishPasskeyLogin verifies the
// assertion and returns the authenticated identity -- it never sets cookies or issues
// a JWT itself; the login handler (a later task) does that.
package auth

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"

	"cmd184psu/unified-webapp/internal/platform/config"
)

// passkeyDataFileName is the name of the file, relative to auth.data_dir,
// that persists enrolled WebAuthn credentials.
const passkeyDataFileName = "passkeys.json"

// passkeyChallengeTTL bounds how long a registration or login challenge may
// be outstanding before it is rejected as expired. The reference this file
// is ported from made this configurable (default 5m); config.PasskeyConfig
// carries no such field here, so it is a fixed constant instead.
const passkeyChallengeTTL = 2 * time.Minute

// passkeyRPDisplayName is the human-readable relying-party name a browser's
// passkey UI may present to the user.
const passkeyRPDisplayName = "Unified Webapp"

// Ceremony kinds recorded on a challenge, so a challenge issued for one
// ceremony can never be replayed to finish the other.
const (
	ceremonyRegister = "register"
	ceremonyLogin    = "login"
)

// Sentinel errors for passkey ceremonies.
var (
	// ErrPasskeyIdentityRequired is returned by any ceremony function that
	// requires a caller-supplied identity (registration, listing, deletion)
	// when given an empty one.
	ErrPasskeyIdentityRequired = errors.New("auth: identity required")
	// ErrPasskeyChallengeInvalid is returned when a challenge ID is unknown,
	// already consumed, expired, or was issued for a different ceremony or
	// identity than the one now finishing it.
	ErrPasskeyChallengeInvalid = errors.New("auth: passkey challenge invalid or expired")
	// ErrPasskeyNoCredentials is returned when a login ceremony is started
	// or finished for a username with no enrolled credentials.
	ErrPasskeyNoCredentials = errors.New("auth: no passkeys enrolled")
	// ErrPasskeyVerificationFailed is returned when the browser's assertion
	// or attestation fails WebAuthn verification.
	ErrPasskeyVerificationFailed = errors.New("auth: passkey verification failed")
)

// PasskeyCeremony carries the opaque options returned to the browser for a
// registration or login ceremony, plus the challenge ID the browser's
// response must be paired with on Finish.
type PasskeyCeremony struct {
	ChallengeID string
	Options     any
}

// PasskeyInfo is a redacted view of a stored credential for listing -- it
// never exposes the credential's public key or raw ID.
type PasskeyInfo struct {
	ID           string
	FriendlyName string
	CreatedAt    time.Time
	LastUsedAt   time.Time
}

// passkeyService runs WebAuthn ceremonies against a persisted credential
// store and an in-memory challenge store. Later tasks wrap it into the
// shared Service/Policy surface; it has no knowledge of cookies, JWTs, or
// LDAP on its own.
type passkeyService struct {
	webauthn   *webauthn.WebAuthn
	passkeys   *passkeyStore
	challenges *challengeStore
	now        func() time.Time
}

// newPasskeyService builds a passkeyService from cfg, persisting enrolled
// credentials at <dataDir>/passkeys.json. dataDir is created (mode 0750) if
// missing, matching key.go's loadOrCreateKey convention. now is the
// service's clock source for challenge expiry; a nil now defaults to
// time.Now, but tests inject a fake clock so expiry never requires
// sleeping.
//
// cfg is expected to be non-empty (validate.go's ValidatePolicy already
// refuses to boot with the "passkey" method configured and no rp_id), but
// an empty or invalid cfg here returns an error from webauthn.New rather
// than panicking.
func newPasskeyService(cfg config.PasskeyConfig, dataDir string, now func() time.Time) (*passkeyService, error) {
	if now == nil {
		now = time.Now
	}

	w, err := webauthn.New(&webauthn.Config{
		RPID:          cfg.RPID,
		RPDisplayName: passkeyRPDisplayName,
		RPOrigins:     cfg.RPOrigins,
		AuthenticatorSelection: protocol.AuthenticatorSelection{
			ResidentKey:      protocol.ResidentKeyRequirementRequired,
			UserVerification: protocol.VerificationPreferred,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("auth: configuring passkey relying party: %w", err)
	}

	if err := os.MkdirAll(dataDir, 0750); err != nil {
		return nil, fmt.Errorf("auth: creating passkey data directory: %w", err)
	}

	store, err := newPasskeyStore(filepath.Join(dataDir, passkeyDataFileName))
	if err != nil {
		return nil, err
	}

	return &passkeyService{
		webauthn:   w,
		passkeys:   store,
		challenges: newChallengeStore(),
		now:        now,
	}, nil
}

// ListPasskeys returns identity's enrolled credentials.
func (s *passkeyService) ListPasskeys(identity string) ([]PasskeyInfo, error) {
	if identity == "" {
		return nil, ErrPasskeyIdentityRequired
	}
	creds, err := s.passkeys.listByUsername(identity)
	if err != nil {
		return nil, err
	}
	out := make([]PasskeyInfo, 0, len(creds))
	for _, c := range creds {
		out = append(out, PasskeyInfo{ID: c.ID, FriendlyName: c.FriendlyName, CreatedAt: c.CreatedAt, LastUsedAt: c.LastUsedAt})
	}
	return out, nil
}

// BeginPasskeyRegistration starts enrolling a new credential for identity.
// Requiring identity to belong to an existing session is the caller's (the
// auth gate's) responsibility -- this only rejects an empty identity.
func (s *passkeyService) BeginPasskeyRegistration(identity, friendlyName string) (PasskeyCeremony, error) {
	if identity == "" {
		return PasskeyCeremony{}, ErrPasskeyIdentityRequired
	}
	creds, err := s.passkeys.listByUsername(identity)
	if err != nil {
		return PasskeyCeremony{}, err
	}
	userHandle, err := userHandleFor(creds)
	if err != nil {
		return PasskeyCeremony{}, fmt.Errorf("auth: generating passkey user handle: %w", err)
	}
	user, err := passkeyUserFrom(identity, userHandle, creds)
	if err != nil {
		return PasskeyCeremony{}, err
	}
	creation, session, err := s.webauthn.BeginRegistration(
		user,
		webauthn.WithResidentKeyRequirement(protocol.ResidentKeyRequirementRequired),
		webauthn.WithExclusions(webauthn.Credentials(user.WebAuthnCredentials()).CredentialDescriptors()),
	)
	if err != nil {
		return PasskeyCeremony{}, fmt.Errorf("auth: beginning passkey registration: %w", err)
	}
	id, err := s.storeChallenge(identity, ceremonyRegister, session)
	if err != nil {
		return PasskeyCeremony{}, err
	}
	// friendlyName is recorded when registration is finished, not here.
	_ = friendlyName
	return PasskeyCeremony{ChallengeID: id, Options: creation}, nil
}

// FinishPasskeyRegistration completes enrollment and persists the
// credential under identity. challengeID must be the one BeginPasskeyRegistration
// returned for the same identity; it is single-use -- a second call with the
// same challengeID, whether this call succeeds or fails, is rejected.
func (s *passkeyService) FinishPasskeyRegistration(ctx context.Context, identity, challengeID, friendlyName string, credentialJSON []byte) (PasskeyInfo, error) {
	if identity == "" {
		return PasskeyInfo{}, ErrPasskeyIdentityRequired
	}
	ch, session, err := s.loadChallenge(challengeID, ceremonyRegister)
	if err != nil {
		return PasskeyInfo{}, err
	}
	defer s.challenges.delete(challengeID)
	if ch.Username != identity {
		return PasskeyInfo{}, ErrPasskeyChallengeInvalid
	}
	creds, err := s.passkeys.listByUsername(identity)
	if err != nil {
		return PasskeyInfo{}, err
	}
	user, err := passkeyUserFrom(identity, session.UserID, creds)
	if err != nil {
		return PasskeyInfo{}, err
	}
	req, err := ceremonyRequest(ctx, credentialJSON)
	if err != nil {
		return PasskeyInfo{}, fmt.Errorf("auth: building passkey registration request: %w", err)
	}
	credential, err := s.webauthn.FinishRegistration(user, session, req)
	if err != nil {
		return PasskeyInfo{}, fmt.Errorf("%w: %v", ErrPasskeyVerificationFailed, err)
	}
	stored, err := json.Marshal(credential)
	if err != nil {
		return PasskeyInfo{}, fmt.Errorf("auth: marshal passkey credential: %w", err)
	}
	now := s.now()
	c := PasskeyCredential{
		ID:             newPasskeyID("pkey"),
		Username:       identity,
		UserHandle:     session.UserID,
		CredentialID:   credential.ID,
		CredentialJSON: stored,
		FriendlyName:   friendlyName,
		CreatedAt:      now,
	}
	if err := s.passkeys.put(c); err != nil {
		return PasskeyInfo{}, err
	}
	return PasskeyInfo{ID: c.ID, FriendlyName: c.FriendlyName, CreatedAt: c.CreatedAt}, nil
}

// BeginPasskeyLogin starts a passwordless login ceremony for a claimed
// username. This is pre-authentication -- the returned identity is not
// trusted until FinishPasskeyLogin verifies the assertion.
func (s *passkeyService) BeginPasskeyLogin(username string) (PasskeyCeremony, error) {
	if username == "" {
		return PasskeyCeremony{}, ErrPasskeyIdentityRequired
	}
	creds, err := s.passkeys.listByUsername(username)
	if err != nil {
		return PasskeyCeremony{}, err
	}
	if len(creds) == 0 {
		return PasskeyCeremony{}, ErrPasskeyNoCredentials
	}
	user, err := passkeyUserFrom(username, creds[0].UserHandle, creds)
	if err != nil {
		return PasskeyCeremony{}, err
	}
	assertion, session, err := s.webauthn.BeginLogin(user, webauthn.WithUserVerification(protocol.VerificationPreferred))
	if err != nil {
		return PasskeyCeremony{}, fmt.Errorf("auth: beginning passkey login: %w", err)
	}
	id, err := s.storeChallenge(username, ceremonyLogin, session)
	if err != nil {
		return PasskeyCeremony{}, err
	}
	return PasskeyCeremony{ChallengeID: id, Options: assertion}, nil
}

// FinishPasskeyLogin verifies the assertion against challengeID and returns
// the authenticated identity on success. It never sets a cookie or issues a
// token -- the login handler does that with the returned identity. Like
// registration, challengeID is single-use.
func (s *passkeyService) FinishPasskeyLogin(ctx context.Context, challengeID string, credentialJSON []byte) (string, error) {
	ch, session, err := s.loadChallenge(challengeID, ceremonyLogin)
	if err != nil {
		return "", err
	}
	defer s.challenges.delete(challengeID)
	creds, err := s.passkeys.listByUsername(ch.Username)
	if err != nil {
		return "", err
	}
	if len(creds) == 0 {
		return "", ErrPasskeyNoCredentials
	}
	user, err := passkeyUserFrom(ch.Username, creds[0].UserHandle, creds)
	if err != nil {
		return "", err
	}
	req, err := ceremonyRequest(ctx, credentialJSON)
	if err != nil {
		return "", fmt.Errorf("auth: building passkey login request: %w", err)
	}
	credential, err := s.webauthn.FinishLogin(user, session, req)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrPasskeyVerificationFailed, err)
	}
	stored, err := json.Marshal(credential)
	if err != nil {
		return "", fmt.Errorf("auth: marshal passkey credential: %w", err)
	}
	c, err := s.passkeys.getByCredentialID(credential.ID)
	if err != nil {
		return "", err
	}
	c.CredentialJSON = stored
	c.LastUsedAt = s.now()
	if err := s.passkeys.put(c); err != nil {
		return "", err
	}
	return ch.Username, nil
}

// DeletePasskey removes identity's credential id. Like the reference this
// is ported from, deleting an id that does not belong to identity (or does
// not exist) is a silent no-op, not an error.
func (s *passkeyService) DeletePasskey(identity, id string) error {
	if identity == "" {
		return ErrPasskeyIdentityRequired
	}
	return s.passkeys.delete(id, identity)
}

// storeChallenge records a freshly-begun ceremony's session data under a
// new challenge ID, expiring passkeyChallengeTTL from now.
func (s *passkeyService) storeChallenge(username, ceremony string, session *webauthn.SessionData) (string, error) {
	expires := s.now().Add(passkeyChallengeTTL)
	session.Expires = expires
	b, err := json.Marshal(session)
	if err != nil {
		return "", fmt.Errorf("auth: marshal passkey challenge session: %w", err)
	}
	id := newPasskeyID("wchal")
	s.challenges.put(challenge{ID: id, Username: username, Ceremony: ceremony, SessionJSON: b, ExpiresAt: expires})
	return id, nil
}

// loadChallenge looks up id, rejecting it (via ErrPasskeyChallengeInvalid)
// if it does not exist, was issued for a different ceremony, or has
// expired per s.now. It does not delete id -- callers are responsible for
// deleting it (typically via defer) once they have loaded it, so a
// challenge is consumable exactly once regardless of whether the rest of
// the ceremony succeeds.
func (s *passkeyService) loadChallenge(id, ceremony string) (challenge, webauthn.SessionData, error) {
	ch, err := s.challenges.get(id)
	if err != nil {
		return challenge{}, webauthn.SessionData{}, ErrPasskeyChallengeInvalid
	}
	if ch.Ceremony != ceremony || s.now().After(ch.ExpiresAt) {
		return challenge{}, webauthn.SessionData{}, ErrPasskeyChallengeInvalid
	}
	var session webauthn.SessionData
	if err := json.Unmarshal(ch.SessionJSON, &session); err != nil {
		return challenge{}, webauthn.SessionData{}, fmt.Errorf("auth: unmarshal passkey challenge session: %w", err)
	}
	return ch, session, nil
}

// passkeyUser adapts a stored identity and its credentials to
// webauthn.User.
type passkeyUser struct {
	username    string
	handle      []byte
	credentials []webauthn.Credential
}

func (u passkeyUser) WebAuthnID() []byte                         { return u.handle }
func (u passkeyUser) WebAuthnName() string                       { return u.username }
func (u passkeyUser) WebAuthnDisplayName() string                { return u.username }
func (u passkeyUser) WebAuthnCredentials() []webauthn.Credential { return u.credentials }

// passkeyUserFrom decodes creds' stored CredentialJSON into webauthn.Credential
// values, building the User the webauthn package's Begin/Finish methods
// operate on.
func passkeyUserFrom(username string, handle []byte, creds []PasskeyCredential) (passkeyUser, error) {
	out := passkeyUser{username: username, handle: handle, credentials: make([]webauthn.Credential, 0, len(creds))}
	for _, c := range creds {
		var credential webauthn.Credential
		if err := json.Unmarshal(c.CredentialJSON, &credential); err != nil {
			return passkeyUser{}, fmt.Errorf("auth: unmarshal stored passkey credential: %w", err)
		}
		out.credentials = append(out.credentials, credential)
	}
	return out, nil
}

// userHandleFor returns the WebAuthn user handle to register a new
// credential under: the existing handle shared by identity's other
// credentials, or a fresh random 32-byte handle for a first enrollment.
func userHandleFor(creds []PasskeyCredential) ([]byte, error) {
	if len(creds) > 0 && len(creds[0].UserHandle) > 0 {
		return creds[0].UserHandle, nil
	}
	b := make([]byte, 32)
	_, err := rand.Read(b)
	return b, err
}

// ceremonyRequest wraps the browser's credential JSON in the *http.Request
// shape webauthn.WebAuthn.FinishRegistration/FinishLogin expect (they parse
// the ceremony response from a request body).
func ceremonyRequest(ctx context.Context, body []byte) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "/", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return req, nil
}

// newPasskeyID returns a random, prefixed identifier for a credential or
// challenge record.
func newPasskeyID(prefix string) string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return prefix + "_" + hex.EncodeToString(b)
}
