package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cmd184psu/unified-webapp/internal/platform/config"
)

// loginBody encodes a POST /api/auth/login request body.
func loginBody(t *testing.T, method, pin, username, password string) *strings.Reader {
	t.Helper()
	b, err := json.Marshal(map[string]string{
		"method":   method,
		"pin":      pin,
		"username": username,
		"password": password,
	})
	if err != nil {
		t.Fatalf("marshal login body: %v", err)
	}
	return strings.NewReader(string(b))
}

// decodeJSON unmarshals rec's body into a map for field assertions.
func decodeJSON(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response body %q: %v", rec.Body.String(), err)
	}
	return out
}

// setCookieValue returns the uw_session cookie's value from rec, if any.
func setCookieValue(rec *httptest.ResponseRecorder) (string, bool) {
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookieName {
			return c.Value, true
		}
	}
	return "", false
}

// methodsOf converts a decoded JSON "methods" field (a []any of strings)
// into a []string for easy comparison.
func methodsOf(t *testing.T, body map[string]any) []string {
	t.Helper()
	raw, ok := body["methods"].([]any)
	if !ok {
		t.Fatalf("body[methods] = %#v, want []any", body["methods"])
	}
	out := make([]string, len(raw))
	for i, m := range raw {
		s, ok := m.(string)
		if !ok {
			t.Fatalf("methods[%d] = %#v, want string", i, m)
		}
		out[i] = s
	}
	return out
}

func mustEqualStrings(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

// --- Method enforcement (FR-A10b) ---

func TestHandleLoginDisallowedMethodNeverInvokesAuthenticator(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// A pin that WOULD match, on a module whose matrix only accepts ldap --
	// the PIN table is never consulted because method enforcement runs
	// first.
	p := &Policy{
		Modules: map[string][]string{"multissh": {"ldap"}},
		PINs:    []config.NamedHash{{Name: "carol", Hash: hashFor(t, "4242")}},
		LDAP:    config.LDAPConfig{URL: "ldap://fake", BaseDN: "dc=example,dc=com"},
	}
	svc := newGateService(t, now, p)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", loginBody(t, "pin", "4242", "", ""))
	svc.Gate("multissh", echoHandler()).ServeHTTP(rec, req)

	mustNotReached(t, rec)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if _, ok := setCookieValue(rec); ok {
		t.Fatal("Set-Cookie present for a disallowed-method login, want none")
	}
}

func TestHandlePasskeyLoginBeginDisallowedMethodNoChallenge(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	ps := newTestPasskeyService(t, testPasskeyConfig(), func() time.Time { return now })
	p := &Policy{
		Modules:  map[string][]string{"grocery": {"pin"}},
		passkeys: ps,
	}
	svc := newGateService(t, now, p)

	rec := httptest.NewRecorder()
	body, _ := json.Marshal(map[string]string{"username": "alice"})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/passkey/login/begin", bytes.NewReader(body))
	svc.Gate("grocery", echoHandler()).ServeHTTP(rec, req)

	mustNotReached(t, rec)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if got := len(ps.challenges.m); got != 0 {
		t.Fatalf("challengeStore has %d entries, want 0 (no ceremony work before the method check)", got)
	}
}

func TestHandlePasskeyLoginFinishDisallowedMethod(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	ps := newTestPasskeyService(t, testPasskeyConfig(), func() time.Time { return now })
	p := &Policy{
		Modules:  map[string][]string{"grocery": {"pin"}},
		passkeys: ps,
	}
	svc := newGateService(t, now, p)

	rec := httptest.NewRecorder()
	body, _ := json.Marshal(map[string]string{"challengeId": "whatever"})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/passkey/login/finish", bytes.NewReader(body))
	svc.Gate("grocery", echoHandler()).ServeHTTP(rec, req)

	mustNotReached(t, rec)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

// --- Successful pin login, then accumulation via a second (ldap) login ---

func TestHandleLoginPinSuccessThenLDAPAccumulates(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	conn := scriptedUserConn("uid=carol,ou=people,dc=example,dc=com", []string{"users"}, nil)
	p := &Policy{
		Modules:         map[string][]string{"multissh": {"pin", "ldap"}},
		PINs:            []config.NamedHash{{Name: "carol", Hash: hashFor(t, "4242")}},
		LDAP:            config.LDAPConfig{URL: "ldap://fake", BaseDN: "dc=example,dc=com"},
		SessionTTL:      time.Hour,
		RefreshFraction: 0.5,
	}
	svc := newGateService(t, now, p)
	svc.ldapDialer = func(ctx context.Context, opts ldapDialOptions) (ldapConn, error) { return conn, nil }

	// First login: pin.
	rec1 := httptest.NewRecorder()
	req1 := httptest.NewRequest(http.MethodPost, "/api/auth/login", loginBody(t, "pin", "4242", "", ""))
	svc.Gate("multissh", echoHandler()).ServeHTTP(rec1, req1)
	mustNotReached(t, rec1)
	if rec1.Code != http.StatusOK {
		t.Fatalf("pin login status = %d, want 200, body=%s", rec1.Code, rec1.Body.String())
	}
	tok, ok := setCookieValue(rec1)
	if !ok || tok == "" {
		t.Fatal("expected Set-Cookie after a successful pin login")
	}
	body1 := decodeJSON(t, rec1)
	if body1["identity"] != "carol" {
		t.Fatalf("identity = %v, want carol", body1["identity"])
	}
	mustEqualStrings(t, methodsOf(t, body1), []string{"pin"})

	// Second login, carrying the first session's cookie: ldap, same
	// identity -> methods accumulate (union), not replace.
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost, "/api/auth/login", loginBody(t, "ldap", "", "carol", "correct-horse"))
	req2.AddCookie(&http.Cookie{Name: sessionCookieName, Value: tok})
	svc.Gate("multissh", echoHandler()).ServeHTTP(rec2, req2)
	mustNotReached(t, rec2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("ldap login status = %d, want 200, body=%s", rec2.Code, rec2.Body.String())
	}
	body2 := decodeJSON(t, rec2)
	if body2["identity"] != "carol" {
		t.Fatalf("identity = %v, want carol", body2["identity"])
	}
	mustEqualStrings(t, methodsOf(t, body2), []string{"pin", "ldap"})
}

func TestHandleLoginLDAPFakeDialer(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	conn := scriptedUserConn("uid=dana,ou=people,dc=example,dc=com", []string{"users"}, nil)
	p := &Policy{
		Modules:         map[string][]string{"multissh": {"ldap"}},
		LDAP:            config.LDAPConfig{URL: "ldap://fake", BaseDN: "dc=example,dc=com"},
		SessionTTL:      time.Hour,
		RefreshFraction: 0.5,
	}
	svc := newGateService(t, now, p)
	svc.ldapDialer = func(ctx context.Context, opts ldapDialOptions) (ldapConn, error) { return conn, nil }

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", loginBody(t, "ldap", "", "dana", "correct-horse"))
	svc.Gate("multissh", echoHandler()).ServeHTTP(rec, req)
	mustNotReached(t, rec)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	if _, ok := setCookieValue(rec); !ok {
		t.Fatal("expected Set-Cookie after a successful ldap login")
	}
	body := decodeJSON(t, rec)
	if body["identity"] != "dana" {
		t.Fatalf("identity = %v, want dana", body["identity"])
	}
	mustEqualStrings(t, methodsOf(t, body), []string{"ldap"})
}

// TestCrossModuleAccumulationPINThenLDAP proves the LDAP half of AC-3
// (security-plan.md T4.6): a PIN login on one module and an LDAP login on a
// second module, carrying the first module's session cookie, accumulate into
// one session that opens both -- exactly as production wires it, since
// buildDispatcher hands every module's Gate call the same *Service. This
// lives here rather than in cmd/server/dispatcher_auth_test.go because
// Service.ldapDialer (the fake-LDAP-without-a-network seam used throughout
// this file) is unexported: package main has no way to inject a fake dialer
// into a Service built via auth.FromConfig, so the LDAP half of AC-3 is
// proven here, against two Gate calls on one Service (one per module) in
// place of two real dispatcher hostnames. The PIN half of AC-3 (PIN login
// opens its own module, the same cookie 401s on the LDAP-only module, and a
// PIN login attempt on the LDAP-only module 400s before any credential
// check) is proven at the dispatcher level in
// cmd/server/dispatcher_auth_test.go's TestAC3_AccumulationAcrossModules,
// which cross-references this test for the LDAP half.
func TestCrossModuleAccumulationPINThenLDAP(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	conn := scriptedUserConn("uid=carol,ou=people,dc=example,dc=com", []string{"users"}, nil)
	p := &Policy{
		Modules:         map[string][]string{"slideshow": {"pin"}, "multissh": {"ldap"}},
		PINs:            []config.NamedHash{{Name: "carol", Hash: hashFor(t, "4242")}},
		LDAP:            config.LDAPConfig{URL: "ldap://fake", BaseDN: "dc=example,dc=com"},
		SessionTTL:      time.Hour,
		RefreshFraction: 0.5,
	}
	svc := newGateService(t, now, p)
	svc.ldapDialer = func(ctx context.Context, opts ldapDialOptions) (ldapConn, error) { return conn, nil }

	// PIN login on slideshow.
	rec1 := httptest.NewRecorder()
	req1 := httptest.NewRequest(http.MethodPost, "/api/auth/login", loginBody(t, "pin", "4242", "", ""))
	svc.Gate("slideshow", echoHandler()).ServeHTTP(rec1, req1)
	mustNotReached(t, rec1)
	if rec1.Code != http.StatusOK {
		t.Fatalf("pin login status = %d, want 200, body=%s", rec1.Code, rec1.Body.String())
	}
	tok, ok := setCookieValue(rec1)
	if !ok || tok == "" {
		t.Fatal("expected Set-Cookie after a successful pin login")
	}

	// That cookie opens slideshow.
	recOpen := httptest.NewRecorder()
	reqOpen := httptest.NewRequest(http.MethodGet, "/", nil)
	reqOpen.AddCookie(&http.Cookie{Name: sessionCookieName, Value: tok})
	svc.Gate("slideshow", echoHandler()).ServeHTTP(recOpen, reqOpen)
	mustReached(t, recOpen)

	// The same cookie 401s on multissh -- its methods ("pin") don't
	// intersect multissh's accepted set ("ldap").
	recBlocked := httptest.NewRecorder()
	reqBlocked := httptest.NewRequest(http.MethodGet, "/", nil)
	reqBlocked.AddCookie(&http.Cookie{Name: sessionCookieName, Value: tok})
	svc.Gate("multissh", echoHandler()).ServeHTTP(recBlocked, reqBlocked)
	mustNotReached(t, recBlocked)
	if recBlocked.Code != http.StatusUnauthorized {
		t.Fatalf("multissh with a pin-only cookie: status = %d, want 401", recBlocked.Code)
	}

	// LDAP login on multissh, carrying the slideshow cookie: same identity
	// -> methods accumulate (union), not replace.
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost, "/api/auth/login", loginBody(t, "ldap", "", "carol", "correct-horse"))
	req2.AddCookie(&http.Cookie{Name: sessionCookieName, Value: tok})
	svc.Gate("multissh", echoHandler()).ServeHTTP(rec2, req2)
	mustNotReached(t, rec2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("ldap login status = %d, want 200, body=%s", rec2.Code, rec2.Body.String())
	}
	body2 := decodeJSON(t, rec2)
	if body2["identity"] != "carol" {
		t.Fatalf("identity = %v, want carol", body2["identity"])
	}
	mustEqualStrings(t, methodsOf(t, body2), []string{"pin", "ldap"})
	tok2, ok := setCookieValue(rec2)
	if !ok || tok2 == "" {
		t.Fatal("expected Set-Cookie after the accumulating ldap login")
	}

	// The accumulated token now opens both modules.
	for _, module := range []string{"slideshow", "multissh"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: tok2})
		svc.Gate(module, echoHandler()).ServeHTTP(rec, req)
		mustReached(t, rec)
	}
}

// --- Admin operator PIN ---

func TestHandleLoginAdminOperatorPIN(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	p := &Policy{
		Modules:         map[string][]string{},
		AdminPIN:        hashFor(t, "9999"),
		SessionTTL:      time.Hour,
		RefreshFraction: 0.5,
	}
	svc := newGateService(t, now, p)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", loginBody(t, "pin", "9999", "", ""))
	svc.Gate("admin", echoHandler()).ServeHTTP(rec, req)
	mustNotReached(t, rec)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	body := decodeJSON(t, rec)
	if body["identity"] != "admin" {
		t.Fatalf("identity = %v, want admin", body["identity"])
	}
	mustEqualStrings(t, methodsOf(t, body), []string{"admin_pin"})
}

// --- Throttle (FR-A7) ---

func TestHandleLoginThrottle(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	p := &Policy{
		Modules:         map[string][]string{"grocery": {"pin"}},
		PINs:            []config.NamedHash{{Name: "dave", Hash: hashFor(t, "1234")}},
		SessionTTL:      time.Hour,
		RefreshFraction: 0.5,
	}
	svc := newGateService(t, now, p)

	// Five consecutive wrong-pin failures.
	for i := 0; i < 5; i++ {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/auth/login", loginBody(t, "pin", "0000", "", ""))
		svc.Gate("grocery", echoHandler()).ServeHTTP(rec, req)
		mustNotReached(t, rec)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: status = %d, want 401", i+1, rec.Code)
		}
	}

	// The 6th attempt is throttled -- even with the CORRECT pin, the
	// throttle is checked before the authenticator runs.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", loginBody(t, "pin", "1234", "", ""))
	svc.Gate("grocery", echoHandler()).ServeHTTP(rec, req)
	mustNotReached(t, rec)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", rec.Code)
	}
	if ra := rec.Header().Get("Retry-After"); ra == "" {
		t.Fatal("expected a Retry-After header on a throttled response")
	}
}

// --- Logout ---

func TestHandleLogoutClearsCookie(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	p := &Policy{Modules: map[string][]string{"grocery": {"pin"}}}
	svc := newGateService(t, now, p)

	tok, err := issueToken(gateTestKey(), "alice", []string{"pin"}, time.Hour, now)
	if err != nil {
		t.Fatalf("issueToken: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: tok})
	svc.Gate("grocery", echoHandler()).ServeHTTP(rec, req)
	mustNotReached(t, rec)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var cleared *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookieName {
			cleared = c
		}
	}
	if cleared == nil {
		t.Fatal("expected a Set-Cookie clearing the session")
	}
	if cleared.MaxAge >= 0 {
		t.Fatalf("MaxAge = %d, want negative (expired)", cleared.MaxAge)
	}
	body := decodeJSON(t, rec)
	if body["ok"] != true {
		t.Fatalf("body = %v, want ok=true", body)
	}
}

// --- Session ---

func TestHandleSessionWithAndWithoutCookie(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	p := &Policy{Modules: map[string][]string{"grocery": {"pin"}}}
	svc := newGateService(t, now, p)

	t.Run("without cookie -> 401", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/auth/session", nil)
		svc.Gate("grocery", echoHandler()).ServeHTTP(rec, req)
		mustNotReached(t, rec)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", rec.Code)
		}
	})

	t.Run("with valid cookie -> 200 identity/methods", func(t *testing.T) {
		tok, err := issueToken(gateTestKey(), "alice", []string{"pin"}, time.Hour, now)
		if err != nil {
			t.Fatalf("issueToken: %v", err)
		}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/auth/session", nil)
		req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: tok})
		svc.Gate("grocery", echoHandler()).ServeHTTP(rec, req)
		mustNotReached(t, rec)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		body := decodeJSON(t, rec)
		if body["identity"] != "alice" {
			t.Fatalf("identity = %v, want alice", body["identity"])
		}
		mustEqualStrings(t, methodsOf(t, body), []string{"pin"})
	})
}

// --- Auth event log (FR-A12b) ---

func TestAuthEventLog(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	p := &Policy{
		Modules:         map[string][]string{"grocery": {"pin"}, "multissh": {"ldap"}},
		PINs:            []config.NamedHash{{Name: "dave", Hash: hashFor(t, "1234")}},
		LDAP:            config.LDAPConfig{URL: "ldap://fake", BaseDN: "dc=example,dc=com"},
		SessionTTL:      time.Hour,
		RefreshFraction: 0.5,
	}
	svc := newGateService(t, now, p)

	var buf bytes.Buffer
	prevOut := log.Writer()
	prevFlags := log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(prevOut)
		log.SetFlags(prevFlags)
	})

	const secretPIN = "1234"
	const wrongPIN = "0000"
	const secretPassword = "hunter2-super-secret"

	// Success.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", loginBody(t, "pin", secretPIN, "", ""))
	svc.Gate("grocery", echoHandler()).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("success login status = %d, want 200", rec.Code)
	}

	// disallowed_method: pin on an ldap-only module.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/auth/login", loginBody(t, "pin", wrongPIN, "", ""))
	svc.Gate("multissh", echoHandler()).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("disallowed-method status = %d, want 400", rec.Code)
	}

	// bad_credential: wrong pin on grocery (still below the throttle
	// threshold -- this is attempt #2 against grocery's PIN table).
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/auth/login", loginBody(t, "pin", wrongPIN, "", ""))
	svc.Gate("grocery", echoHandler()).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("bad-credential status = %d, want 401", rec.Code)
	}

	// throttled: drive the remaining failures, then one more attempt,
	// carrying secretPassword in an ldap attempt so it would show up in
	// the buffer if the log leaked bodies.
	for i := 0; i < 4; i++ {
		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodPost, "/api/auth/login", loginBody(t, "pin", wrongPIN, "", ""))
		svc.Gate("grocery", echoHandler()).ServeHTTP(rec, req)
	}
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/auth/login", loginBody(t, "ldap", "", "someone", secretPassword))
	svc.Gate("multissh", echoHandler()).ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("throttled status = %d, want 429, body=%s", rec.Code, rec.Body.String())
	}

	out := buf.String()
	for _, want := range []string{
		`event=auth_login ok=true`,
		`reason="disallowed_method"`,
		`reason="bad_credential"`,
		`reason="throttled"`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("log output missing %q; got:\n%s", want, out)
		}
	}
	for _, secret := range []string{secretPIN, wrongPIN, secretPassword} {
		if strings.Contains(out, secret) {
			t.Fatalf("log output leaked a secret %q; got:\n%s", secret, out)
		}
	}
}

// ---------------------------------------------------------------------
// T5.2 -- break-glass enforcement (FR-M2), login-handler level.
//
// Item 1, 3, 4, and 8 of the T5.2 checklist are proven at the Gate/Policy
// level in gate_test.go (adminEmptyMatrixEncodings is defined there and
// shared with this file). This section covers item 2 (operator PIN file
// login, both empty-matrix encodings) and items 5-7 (the 0644 PIN file,
// editing the file taking effect without a swap, and the throttle
// applying to operator-PIN attempts) -- all of which go through
// handleLogin.
// ---------------------------------------------------------------------

// TestHandleLoginAdminPINFileBothEmptyMatrixEncodings proves T5.2 item 2:
// the operator PIN from a 0400 admin_pin_file logs in as identity "admin"
// via method "admin_pin", regardless of which empty-matrix encoding admin
// is configured with, confirmed both in the login response and in a
// follow-up GET /api/auth/session using the issued cookie.
func TestHandleLoginAdminPINFileBothEmptyMatrixEncodings(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for name, modules := range adminEmptyMatrixEncodings() {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "admin.pin")
			if err := os.WriteFile(path, []byte("9999"), 0400); err != nil {
				t.Fatalf("write pin file: %v", err)
			}
			p := &Policy{
				Modules:         modules,
				AdminPINFile:    path,
				SessionTTL:      time.Hour,
				RefreshFraction: 0.5,
			}
			svc := newGateService(t, now, p)

			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/api/auth/login", loginBody(t, "pin", "9999", "", ""))
			svc.Gate("admin", echoHandler()).ServeHTTP(rec, req)
			mustNotReached(t, rec)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
			}
			body := decodeJSON(t, rec)
			if body["identity"] != "admin" {
				t.Fatalf("identity = %v, want admin", body["identity"])
			}
			mustEqualStrings(t, methodsOf(t, body), []string{adminPINMethod})

			tok, ok := setCookieValue(rec)
			if !ok {
				t.Fatal("expected Set-Cookie after a successful admin PIN login")
			}

			recSession := httptest.NewRecorder()
			reqSession := httptest.NewRequest(http.MethodGet, "/api/auth/session", nil)
			reqSession.AddCookie(&http.Cookie{Name: sessionCookieName, Value: tok})
			svc.Gate("admin", echoHandler()).ServeHTTP(recSession, reqSession)
			mustNotReached(t, recSession)
			if recSession.Code != http.StatusOK {
				t.Fatalf("session status = %d, want 200", recSession.Code)
			}
			sessionBody := decodeJSON(t, recSession)
			if sessionBody["identity"] != "admin" {
				t.Fatalf("session identity = %v, want admin", sessionBody["identity"])
			}
			mustEqualStrings(t, methodsOf(t, sessionBody), []string{adminPINMethod})
		})
	}
}

// TestHandleLoginAdminPINFileTooOpenPermissions proves T5.2 item 5
// (FR-M2): a too-open admin_pin_file fails the login loudly with an error
// naming chmod and the file's mode -- never the generic "invalid
// credentials" 401, which would strand the operator on the break-glass
// path with no diagnostic. The misconfiguration is surfaced as a 500 (it
// is a server-side config error, not a credential failure) and must not
// feed the throttle.
func TestHandleLoginAdminPINFileTooOpenPermissions(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	path := filepath.Join(dir, "admin.pin")
	if err := os.WriteFile(path, []byte("9999"), 0644); err != nil {
		t.Fatalf("write pin file: %v", err)
	}
	p := &Policy{
		Modules:      map[string][]string{},
		AdminPINFile: path,
	}
	svc := newGateService(t, now, p)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", loginBody(t, "pin", "9999", "", ""))
	svc.Gate("admin", echoHandler()).ServeHTTP(rec, req)
	mustNotReached(t, rec)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (admin_pin_file misconfiguration is a server-side error)", rec.Code)
	}
	body := decodeJSON(t, rec)
	errMsg := fmt.Sprint(body["error"])
	if !strings.Contains(errMsg, "chmod") {
		t.Fatalf("body[error] = %q, want it to name chmod (FR-M2: the fix must be in the response)", errMsg)
	}
	if strings.Contains(errMsg, "9999") {
		t.Fatalf("body[error] = %q leaks the attempted pin", errMsg)
	}

	// The config error must not have fed the throttle: fixing the file's
	// mode makes the very next attempt succeed with no backoff.
	if err := os.Chmod(path, 0400); err != nil {
		t.Fatalf("chmod pin file: %v", err)
	}
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost, "/api/auth/login", loginBody(t, "pin", "9999", "", ""))
	svc.Gate("admin", echoHandler()).ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("after chmod 0400: status = %d, want 200 (config errors must not throttle)", rec2.Code)
	}
}

// TestHandleLoginAdminPINFileEditTakesEffectWithoutSwap proves T5.2 item 6:
// rewriting the admin_pin_file's contents takes effect on the very next
// login attempt with no SwapPolicy/BuildPolicy call at all -- checkAdminPIN
// re-reads the file per attempt (pin.go).
func TestHandleLoginAdminPINFileEditTakesEffectWithoutSwap(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	path := filepath.Join(dir, "admin.pin")
	if err := os.WriteFile(path, []byte("1111"), 0400); err != nil {
		t.Fatalf("write pin file: %v", err)
	}
	p := &Policy{
		Modules:         map[string][]string{},
		AdminPINFile:    path,
		SessionTTL:      time.Hour,
		RefreshFraction: 0.5,
	}
	svc := newGateService(t, now, p)

	rec1 := httptest.NewRecorder()
	req1 := httptest.NewRequest(http.MethodPost, "/api/auth/login", loginBody(t, "pin", "1111", "", ""))
	svc.Gate("admin", echoHandler()).ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusOK {
		t.Fatalf("original pin: status = %d, want 200, body=%s", rec1.Code, rec1.Body.String())
	}

	// Edit the file's contents in place -- no SwapPolicy call at all.
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatalf("chmod for rewrite: %v", err)
	}
	if err := os.WriteFile(path, []byte("2222"), 0400); err != nil {
		t.Fatalf("rewrite pin file: %v", err)
	}

	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost, "/api/auth/login", loginBody(t, "pin", "1111", "", ""))
	svc.Gate("admin", echoHandler()).ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusUnauthorized {
		t.Fatalf("original pin after rewrite: status = %d, want 401", rec2.Code)
	}

	rec3 := httptest.NewRecorder()
	req3 := httptest.NewRequest(http.MethodPost, "/api/auth/login", loginBody(t, "pin", "2222", "", ""))
	svc.Gate("admin", echoHandler()).ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusOK {
		t.Fatalf("new pin, next attempt: status = %d, want 200, body=%s", rec3.Code, rec3.Body.String())
	}
	body3 := decodeJSON(t, rec3)
	if body3["identity"] != "admin" {
		t.Fatalf("identity = %v, want admin", body3["identity"])
	}
}

// TestHandleLoginThrottleAppliesToAdminPIN proves T5.2 item 7: the global
// login throttle (throttle.go) applies to operator-PIN attempts exactly as
// it does to any other login method -- five consecutive failures arm a
// delay that throttles the sixth attempt even with the correct PIN, and
// advancing the injected clock past the armed delay lets a subsequent
// correct attempt through (and resets the throttle).
func TestHandleLoginThrottleAppliesToAdminPIN(t *testing.T) {
	clk := newFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	p := &Policy{
		Modules:         map[string][]string{},
		AdminPIN:        hashFor(t, "9999"),
		SessionTTL:      time.Hour,
		RefreshFraction: 0.5,
	}
	svc := &Service{
		key:      gateTestKey(),
		now:      clk.now,
		throttle: newThrottle(clk.now),
	}
	svc.SwapPolicy(p)

	for i := 0; i < 5; i++ {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/auth/login", loginBody(t, "pin", "0000", "", ""))
		svc.Gate("admin", echoHandler()).ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: status = %d, want 401", i+1, rec.Code)
		}
	}

	// The 6th attempt, even with the correct operator PIN, is throttled.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", loginBody(t, "pin", "9999", "", ""))
	svc.Gate("admin", echoHandler()).ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", rec.Code)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Fatal("expected a Retry-After header on a throttled response")
	}

	// Advance the injected clock past the armed 2s delay: the correct PIN
	// now succeeds.
	clk.advance(3 * time.Second)
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost, "/api/auth/login", loginBody(t, "pin", "9999", "", ""))
	svc.Gate("admin", echoHandler()).ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("status after clock advance = %d, want 200, body=%s", rec2.Code, rec2.Body.String())
	}
	body := decodeJSON(t, rec2)
	if body["identity"] != "admin" {
		t.Fatalf("identity = %v, want admin", body["identity"])
	}
}

func TestAuthEventLogPasskeyDisallowed(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	ps := newTestPasskeyService(t, testPasskeyConfig(), func() time.Time { return now })
	p := &Policy{
		Modules:  map[string][]string{"grocery": {"pin"}},
		passkeys: ps,
	}
	svc := newGateService(t, now, p)

	var buf bytes.Buffer
	prevOut := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(prevOut) })

	rec := httptest.NewRecorder()
	body, _ := json.Marshal(map[string]string{"username": "alice"})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/passkey/login/begin", bytes.NewReader(body))
	svc.Gate("grocery", echoHandler()).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}

	if !strings.Contains(buf.String(), `reason="disallowed_method"`) {
		t.Fatalf("expected a disallowed_method log line; got:\n%s", buf.String())
	}
}
