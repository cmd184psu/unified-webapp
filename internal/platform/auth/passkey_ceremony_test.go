package auth

// End-to-end passkey ceremonies through the real handlers (via svc.Gate),
// driven by a software authenticator that builds genuine ES256 attestations
// and assertions. This is the test that proves register-then-login works;
// the older tests only reach Begin or finish with injected credentials.

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"cmd184psu/unified-webapp/internal/platform/config"
)

const (
	e2eRPID   = "example.test"
	e2eOrigin = "https://app.example.test"
	e2eModule = "todo"
)

var b64 = base64.RawURLEncoding

// --- minimal CBOR encoder (just what a "none" attestation needs) ---

func cborHead(major byte, n uint64) []byte {
	switch {
	case n < 24:
		return []byte{major<<5 | byte(n)}
	case n < 256:
		return []byte{major<<5 | 24, byte(n)}
	default:
		return []byte{major<<5 | 25, byte(n >> 8), byte(n)}
	}
}
func cborText(s string) []byte  { return append(cborHead(3, uint64(len(s))), s...) }
func cborBytes(b []byte) []byte { return append(cborHead(2, uint64(len(b))), b...) }
func cborInt(v int64) []byte {
	if v >= 0 {
		return cborHead(0, uint64(v))
	}
	return cborHead(1, uint64(-1-v))
}
func cborMap(pairs ...[]byte) []byte {
	out := cborHead(5, uint64(len(pairs)/2))
	for _, p := range pairs {
		out = append(out, p...)
	}
	return out
}

// softAuthenticator is a software WebAuthn authenticator holding one
// discoverable ES256 credential.
type softAuthenticator struct {
	key         *ecdsa.PrivateKey
	credID      []byte
	userHandle  []byte
	counter     uint32
	rpID        string
	origin      string
	omitHandle  bool
	tamperSig   bool
	altUserHand []byte
}

func newSoftAuthenticator(t *testing.T) *softAuthenticator {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	id := make([]byte, 16)
	_, _ = rand.Read(id)
	return &softAuthenticator{key: k, credID: id, rpID: e2eRPID, origin: e2eOrigin}
}

func rpHash(rpID string) []byte { h := sha256.Sum256([]byte(rpID)); return h[:] }

func (a *softAuthenticator) clientData(typ, challenge string) []byte {
	b, _ := json.Marshal(map[string]any{"type": typ, "challenge": challenge, "origin": a.origin, "crossOrigin": false})
	return b
}

// attest answers a registration options object with a "none" attestation.
func (a *softAuthenticator) attest(t *testing.T, publicKey map[string]any) map[string]any {
	t.Helper()
	user := publicKey["user"].(map[string]any)
	h, err := b64.DecodeString(user["id"].(string))
	if err != nil {
		t.Fatalf("user.id: %v", err)
	}
	a.userHandle = h
	x := a.key.PublicKey.X.FillBytes(make([]byte, 32))
	y := a.key.PublicKey.Y.FillBytes(make([]byte, 32))
	cose := cborMap(cborInt(1), cborInt(2), cborInt(3), cborInt(-7), cborInt(-1), cborInt(1), cborInt(-2), cborBytes(x), cborInt(-3), cborBytes(y))
	authData := append([]byte{}, rpHash(a.rpID)...)
	authData = append(authData, 0x45) // UP | UV | AT
	authData = binary.BigEndian.AppendUint32(authData, 0)
	authData = append(authData, make([]byte, 16)...) // aaguid
	authData = binary.BigEndian.AppendUint16(authData, uint16(len(a.credID)))
	authData = append(authData, a.credID...)
	authData = append(authData, cose...)
	att := cborMap(cborText("fmt"), cborText("none"), cborText("attStmt"), cborMap(), cborText("authData"), cborBytes(authData))
	return map[string]any{
		"id": b64.EncodeToString(a.credID), "rawId": b64.EncodeToString(a.credID), "type": "public-key",
		"response": map[string]any{
			"clientDataJSON":    b64.EncodeToString(a.clientData("webauthn.create", publicKey["challenge"].(string))),
			"attestationObject": b64.EncodeToString(att),
		},
	}
}

// assert answers a login options object with a signed assertion.
func (a *softAuthenticator) assert(t *testing.T, publicKey map[string]any) map[string]any {
	t.Helper()
	a.counter++
	cd := a.clientData("webauthn.get", publicKey["challenge"].(string))
	authData := append([]byte{}, rpHash(a.rpID)...)
	authData = append(authData, 0x05) // UP | UV
	authData = binary.BigEndian.AppendUint32(authData, a.counter)
	cdh := sha256.Sum256(cd)
	digest := sha256.Sum256(append(append([]byte{}, authData...), cdh[:]...))
	sig, err := ecdsa.SignASN1(rand.Reader, a.key, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	if a.tamperSig {
		sig[len(sig)-1] ^= 0xff
	}
	resp := map[string]any{
		"clientDataJSON":    b64.EncodeToString(cd),
		"authenticatorData": b64.EncodeToString(authData),
		"signature":         b64.EncodeToString(sig),
	}
	if !a.omitHandle {
		h := a.userHandle
		if a.altUserHand != nil {
			h = a.altUserHand
		}
		resp["userHandle"] = b64.EncodeToString(h)
	}
	return map[string]any{
		"id": b64.EncodeToString(a.credID), "rawId": b64.EncodeToString(a.credID), "type": "public-key",
		"response": resp,
	}
}

// --- rig ---

type ceremonyRig struct {
	t      *testing.T
	svc    *Service
	groups []string
	now    time.Time
	token  string
}

func newCeremonyRig(t *testing.T) *ceremonyRig {
	return newCeremonyRigWith(t, config.PasskeyConfig{RPID: e2eRPID, RPOrigins: []string{e2eOrigin}})
}

func newCeremonyRigWith(t *testing.T, pk config.PasskeyConfig) *ceremonyRig {
	t.Helper()
	now := time.Now()
	r := &ceremonyRig{t: t, now: now, groups: []string{"household"}}
	p := &Policy{
		Modules:    map[string]ModulePolicy{e2eModule: {PinFile: "/tmp/does-not-matter"}},
		LDAP:       config.LDAPConfig{URL: "ldap://fake", BaseDN: "dc=example,dc=com", RequiredGroups: []string{"household"}},
		Passkey:    pk,
		SessionTTL: time.Hour,
	}
	ps, err := newPasskeyService(p.Passkey, t.TempDir(), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	p.passkeys = ps
	r.svc = newGateService(t, now, p)
	r.svc.ldapDialer = func(context.Context, ldapDialOptions) (ldapConn, error) {
		return scriptedUserConn("uid=chris,ou=people,dc=example,dc=com", r.groups, nil), nil
	}
	r.token = sessionCookieToken(t, "chris", []string{"ldap:" + e2eModule}, time.Hour, now)
	return r
}

func (r *ceremonyRig) post(path string, body any, withSession bool) *httptest.ResponseRecorder {
	r.t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(b))
	if withSession {
		req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: r.token})
	}
	rec := httptest.NewRecorder()
	r.svc.Gate(e2eModule, echoHandler()).ServeHTTP(rec, req)
	mustNotReached(r.t, rec)
	return rec
}

// begin returns the challenge ID and the publicKey options of a begin call.
func (r *ceremonyRig) begin(path string, body any, withSession bool) (string, map[string]any) {
	r.t.Helper()
	rec := r.post(path, body, withSession)
	if rec.Code != http.StatusOK {
		r.t.Fatalf("%s: status %d body %s", path, rec.Code, rec.Body.String())
	}
	out := decodeJSON(r.t, rec)
	pk := out["options"].(map[string]any)["publicKey"].(map[string]any)
	return out["challengeId"].(string), pk
}

func (r *ceremonyRig) register(a *softAuthenticator) {
	r.t.Helper()
	id, pk := r.begin("/api/auth/passkey/register/begin", map[string]string{"friendlyName": "laptop"}, true)
	rec := r.post("/api/auth/passkey/register/finish", map[string]any{"challengeId": id, "friendlyName": "laptop", "credential": a.attest(r.t, pk)}, true)
	if rec.Code != http.StatusOK {
		r.t.Fatalf("register finish: %d %s", rec.Code, rec.Body.String())
	}
}

// loginFinish begins a login (username optional) and finishes it with the
// authenticator's assertion, returning the finish response.
func (r *ceremonyRig) login(a *softAuthenticator, username string) *httptest.ResponseRecorder {
	r.t.Helper()
	body := map[string]string{}
	if username != "" {
		body["username"] = username
	}
	id, pk := r.begin("/api/auth/passkey/login/begin", body, false)
	return r.post("/api/auth/passkey/login/finish", map[string]any{"challengeId": id, "credential": a.assert(r.t, pk)}, false)
}

func (r *ceremonyRig) wantRejected(rec *httptest.ResponseRecorder, code int, msg string) {
	r.t.Helper()
	if rec.Code != code {
		r.t.Fatalf("status = %d, want %d; body %s", rec.Code, code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), msg) {
		r.t.Fatalf("body %q does not contain %q", rec.Body.String(), msg)
	}
	if _, ok := setCookieValue(rec); ok {
		r.t.Fatal("a rejected passkey login must not set a session cookie")
	}
}

const (
	msgNotAccepted = "That passkey was not accepted."
	msgTimedOut    = "The passkey sign-in timed out. Try again."
	msgNoPasskey   = "No passkey is registered for that account."
)

func (r *ceremonyRig) wantSession(rec *httptest.ResponseRecorder) {
	r.t.Helper()
	if rec.Code != http.StatusOK {
		r.t.Fatalf("login status = %d, body %s", rec.Code, rec.Body.String())
	}
	if _, ok := setCookieValue(rec); !ok {
		r.t.Fatal("expected a session cookie")
	}
	body := decodeJSON(r.t, rec)
	if body["identity"] != "chris" {
		r.t.Fatalf("identity = %v, want chris", body["identity"])
	}
	mustEqualStrings(r.t, methodsOf(r.t, body), []string{"passkey:" + e2eModule})
}

// --- tests ---

func TestPasskeyE2EDiscoverableLoginWithoutUsername(t *testing.T) {
	r := newCeremonyRig(t)
	a := newSoftAuthenticator(t)
	r.register(a)
	r.wantSession(r.login(a, ""))
}

func TestPasskeyE2ETargetedLoginWithUsername(t *testing.T) {
	r := newCeremonyRig(t)
	a := newSoftAuthenticator(t)
	r.register(a)
	r.wantSession(r.login(a, "chris"))
}

func TestPasskeyE2EBeginWithEmptyUsernameReturnsChallenge(t *testing.T) {
	r := newCeremonyRig(t)
	id, pk := r.begin("/api/auth/passkey/login/begin", map[string]string{}, false)
	if id == "" || pk["challenge"] == "" {
		t.Fatalf("want a challenge, got id=%q pk=%v", id, pk)
	}
	if list, _ := pk["allowCredentials"].([]any); len(list) != 0 {
		t.Fatalf("discoverable login must not carry an allowList, got %v", list)
	}
}

func TestPasskeyE2ERegistrationRequestsResidentKey(t *testing.T) {
	r := newCeremonyRig(t)
	_, pk := r.begin("/api/auth/passkey/register/begin", map[string]string{}, true)
	sel, _ := pk["authenticatorSelection"].(map[string]any)
	if sel["residentKey"] != "required" || sel["requireResidentKey"] != true {
		t.Fatalf("authenticatorSelection = %v, want residentKey required", sel)
	}
}

func TestPasskeyE2ERejections(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(a *softAuthenticator)
	}{
		{"tampered signature", func(a *softAuthenticator) { a.tamperSig = true }},
		{"wrong rp id", func(a *softAuthenticator) { a.rpID = "evil.test" }},
		{"unknown credential id", func(a *softAuthenticator) { a.credID = []byte("not-an-enrolled-credential") }},
		{"mismatched user handle", func(a *softAuthenticator) { a.altUserHand = []byte("someone-elses-handle-0123456789ab") }},
		{"missing user handle", func(a *softAuthenticator) { a.omitHandle = true }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newCeremonyRig(t)
			a := newSoftAuthenticator(t)
			r.register(a)
			tc.mutate(a)
			r.wantRejected(r.login(a, ""), http.StatusUnauthorized, msgNotAccepted)
		})
	}
}

// A wrong origin is not a bad passkey: it says so, instead of the generic
// "not accepted", so the owner knows the address is what needs fixing.
func TestPasskeyE2EWrongOriginSaysSo(t *testing.T) {
	r := newCeremonyRig(t)
	a := newSoftAuthenticator(t)
	r.register(a)
	a.origin = "https://evil.test"
	r.wantRejected(r.login(a, ""), http.StatusBadRequest, msgPasskeyOrigin)
}

// A routed host under the RP ID needs no entry in rp_origins.
func TestPasskeyE2ERoutedHostIsAllowedAutomatically(t *testing.T) {
	u, _ := url.Parse(e2eOrigin)
	r := newCeremonyRigWith(t, config.PasskeyConfig{RPID: e2eRPID, Hosts: []string{u.Hostname()}})
	a := newSoftAuthenticator(t)
	r.register(a)
	if rec := r.login(a, ""); rec.Code != http.StatusOK {
		t.Fatalf("login from a routed host = %d %s", rec.Code, rec.Body.String())
	}
}

func TestPasskeyE2EReplayedChallengeRejected(t *testing.T) {
	r := newCeremonyRig(t)
	a := newSoftAuthenticator(t)
	r.register(a)
	id, pk := r.begin("/api/auth/passkey/login/begin", map[string]string{}, false)
	body := map[string]any{"challengeId": id, "credential": a.assert(t, pk)}
	r.wantSession(r.post("/api/auth/passkey/login/finish", body, false))
	r.wantRejected(r.post("/api/auth/passkey/login/finish", body, false), http.StatusBadRequest, msgTimedOut)
}

func TestPasskeyE2ERefusedAfterLeavingLDAPGroup(t *testing.T) {
	r := newCeremonyRig(t)
	a := newSoftAuthenticator(t)
	r.register(a)
	r.groups = []string{"strangers"}
	r.wantRejected(r.login(a, ""), http.StatusUnauthorized, msgNotAccepted)
}

func TestPasskeyE2ETargetedLoginNoPasskeysSaysSo(t *testing.T) {
	r := newCeremonyRig(t)
	rec := r.post("/api/auth/passkey/login/begin", map[string]string{"username": "chris"}, false)
	r.wantRejected(rec, http.StatusNotFound, msgNoPasskey)
}
