package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"cmd184psu/unified-webapp/internal/platform/config"
)

// Design fact (owner): every module has its own login, session and timeout
// cycle. Logging into todo does not grant grocery, or any other module.
//
// These tests pin that at the gate, independent of cookie scoping: they hand
// the *same* session cookie to every module, which is exactly what happens
// when a browser sends it to more than one host (a shared cookie_domain, one
// host serving several modules, a hand-built request). Isolation must not
// depend on the browser keeping the cookies apart.

var isolationModules = []string{"todo", "grocery", "smbedit", "multissh"}

func newIsolationService(t *testing.T) *Service {
	t.Helper()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	mods := map[string]ModulePolicy{}
	for _, m := range isolationModules {
		mods[m] = ModulePolicy{PinFile: writePinFile(t, "4242")}
	}
	p := &Policy{
		Modules:    mods,
		LDAP:       config.LDAPConfig{URL: "ldap://fake", BaseDN: "dc=example,dc=com"},
		SessionTTL: time.Hour,
	}
	svc := newGateService(t, now, p)
	conn := scriptedUserConn("uid=carol,ou=people,dc=example,dc=com", []string{"users"}, nil)
	svc.ldapDialer = func(ctx context.Context, opts ldapDialOptions) (ldapConn, error) { return conn, nil }
	return svc
}

// ldapLogin logs carol in on module, carrying cookie (may be "") like a
// browser would, and returns the cookie the response set.
func ldapLogin(t *testing.T, svc *Service, module, cookie string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", loginBody(t, "ldap", "", "carol", "correct-horse"))
	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: cookie})
	}
	svc.Gate(module, echoHandler()).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("ldap login on %s: status = %d, body=%s", module, rec.Code, rec.Body.String())
	}
	tok, ok := setCookieValue(rec)
	if !ok || tok == "" {
		t.Fatalf("ldap login on %s: no session cookie set", module)
	}
	return tok
}

// statusFor requests "/" on module with cookie and returns the status.
func statusFor(svc *Service, module, cookie string) int {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: cookie})
	}
	svc.Gate(module, echoHandler()).ServeHTTP(rec, req)
	return rec.Code
}

func TestLDAPLoginOpensOnlyItsOwnModule(t *testing.T) {
	svc := newIsolationService(t)
	tok := ldapLogin(t, svc, "todo", "")

	if got := statusFor(svc, "todo", tok); got != http.StatusOK {
		t.Fatalf("todo with its own login = %d, want 200", got)
	}
	for _, m := range isolationModules {
		if m == "todo" {
			continue
		}
		if got := statusFor(svc, m, tok); got != http.StatusUnauthorized {
			t.Errorf("%s with a todo login = %d, want 401 (logging into todo must not grant %s)", m, got, m)
		}
	}
}

// A session that has logged into two modules is admitted to exactly those
// two, and signing out of one leaves the other signed in.
func TestEachModuleNeedsItsOwnLoginAndSignsOutAlone(t *testing.T) {
	svc := newIsolationService(t)
	tok := ldapLogin(t, svc, "todo", "")
	tok = ldapLogin(t, svc, "grocery", tok)

	for _, m := range []string{"todo", "grocery"} {
		if got := statusFor(svc, m, tok); got != http.StatusOK {
			t.Fatalf("%s after logging into it = %d, want 200", m, got)
		}
	}
	for _, m := range []string{"smbedit", "multissh"} {
		if got := statusFor(svc, m, tok); got != http.StatusUnauthorized {
			t.Errorf("%s never logged into = %d, want 401", m, got)
		}
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: tok})
	svc.Gate("todo", echoHandler()).ServeHTTP(rec, req)
	after, _ := setCookieValue(rec)

	if got := statusFor(svc, "todo", after); got != http.StatusUnauthorized {
		t.Errorf("todo after signing out of todo = %d, want 401", got)
	}
	if got := statusFor(svc, "grocery", after); got != http.StatusOK {
		t.Errorf("grocery after signing out of todo = %d, want 200 (each module signs out alone)", got)
	}
}

// Tokens minted before grants were module-scoped carry a bare "ldap" or
// "passkey" that used to open every module. They must open none now, so
// those sessions are forced to log in again per module.
func TestLegacyUnscopedIdentityGrantOpensNothing(t *testing.T) {
	svc := newIsolationService(t)
	now := svc.now()
	for _, grant := range []string{"ldap", "passkey"} {
		tok, err := issueToken(svc.key, "carol", []string{grant}, time.Hour, now)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range isolationModules {
			if got := statusFor(svc, m, tok); got != http.StatusUnauthorized {
				t.Errorf("legacy %q token on %s = %d, want 401", grant, m, got)
			}
		}
	}
}
