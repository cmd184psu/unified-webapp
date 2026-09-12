package auth

import (
	"bufio"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"cmd184psu/unified-webapp/internal/platform/config"
)

// gateTestKey returns a fixed HMAC key for gate tests -- long enough to be
// a plausible HS256 key, and stable across calls so tokens issued in one
// call verify in another.
func gateTestKey() []byte {
	return []byte("gate-test-key-0123456789abcdef")
}

// newGateService builds a minimal Service for gate tests: a fixed clock
// (so sliding-refresh math is deterministic) and the given policy
// installed as the current snapshot.
func newGateService(t *testing.T, now time.Time, p *Policy) *Service {
	t.Helper()
	nowFn := func() time.Time { return now }
	s := &Service{
		key:      gateTestKey(),
		now:      nowFn,
		throttle: newThrottle(nowFn),
	}
	s.SwapPolicy(p)
	return s
}

// echoHandler is a next handler that always answers 200 with a fixed body
// and a marker header, so tests can assert the gate actually reached it
// (as opposed to answering the request itself).
func echoHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Reached-Next", "yes")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("next reached: " + r.Method + " " + r.URL.Path))
	})
}

func mustReached(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if rec.Header().Get("X-Reached-Next") != "yes" {
		t.Fatalf("expected request to reach next, got status=%d body=%q headers=%v", rec.Code, rec.Body.String(), rec.Header())
	}
}

func mustNotReached(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if rec.Header().Get("X-Reached-Next") == "yes" {
		t.Fatalf("expected request NOT to reach next, but it did: status=%d body=%q", rec.Code, rec.Body.String())
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (func() bool {
		for i := 0; i+len(substr) <= len(s); i++ {
			if s[i:i+len(substr)] == substr {
				return true
			}
		}
		return false
	})()
}

// sessionCookieToken issues and returns a session JWT carrying sub/grants,
// signed with gateTestKey, for direct use as a cookie value in gate tests.
func sessionCookieToken(t *testing.T, sub string, grants []string, ttl time.Duration, now time.Time) string {
	t.Helper()
	tok, err := issueToken(gateTestKey(), sub, grants, ttl, now)
	if err != nil {
		t.Fatalf("issueToken: %v", err)
	}
	return tok
}

// --- Step 1: healthz ---

func TestGateHealthzAlwaysOK(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	unprotected := newGateService(t, now, &Policy{})
	protected := newGateService(t, now, &Policy{Modules: map[string]ModulePolicy{"grocery": {}}})

	for name, svc := range map[string]*Service{"unprotected": unprotected, "protected": protected} {
		t.Run(name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
			svc.Gate("grocery", echoHandler()).ServeHTTP(rec, req)
			mustNotReached(t, rec)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}
			if got := rec.Body.String(); got != `{"ok":true}`+"\n" {
				t.Fatalf("body = %q, want {\"ok\":true}", got)
			}
		})
	}
}

// --- Step 2: mode, per state ---

func TestGateModeReportsOfferedMethods(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name    string
		policy  *Policy
		module  string
		want    string // exact expected JSON body
		wantSub string // substring check when want == ""
	}{
		{
			name:   "open module",
			policy: &Policy{Modules: map[string]ModulePolicy{}},
			module: "grocery",
			want:   `{"methods":[]}` + "\n",
		},
		{
			name: "protected module, ldap only",
			policy: &Policy{
				Modules: map[string]ModulePolicy{"grocery": {}},
				LDAP:    config.LDAPConfig{URL: "ldap://fake"},
			},
			module: "grocery",
			want:   `{"methods":["ldap"]}` + "\n",
		},
		{
			name: "protected module with pin_file",
			policy: &Policy{
				Modules: map[string]ModulePolicy{"grocery": {PinFile: "/tmp/does-not-matter"}},
				LDAP:    config.LDAPConfig{URL: "ldap://fake"},
			},
			module: "grocery",
			want:   `{"methods":["ldap","pin"]}` + "\n",
		},
		{
			name:    "admin with empty matrix",
			policy:  &Policy{Modules: map[string]ModulePolicy{}},
			module:  "admin",
			wantSub: `"admin_pin"`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc := newGateService(t, now, tc.policy)
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/api/auth/mode", nil)
			svc.Gate(tc.module, echoHandler()).ServeHTTP(rec, req)
			mustNotReached(t, rec)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}
			body := rec.Body.String()
			if tc.want != "" && body != tc.want {
				t.Fatalf("body = %q, want %q", body, tc.want)
			}
			if tc.wantSub != "" && !contains(body, tc.wantSub) {
				t.Fatalf("body = %q, want it to contain %q", body, tc.wantSub)
			}
		})
	}
}

// TestGateModeAdminIsByteForByte proves admin's mode output is exactly
// ["admin_pin"], never more, regardless of whether admin has its own
// (irrelevant) Modules entry.
func TestGateModeAdminIsByteForByte(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for name, modules := range map[string]map[string]ModulePolicy{
		"no admin entry":    {},
		"empty admin entry": {"admin": {}},
	} {
		t.Run(name, func(t *testing.T) {
			svc := newGateService(t, now, &Policy{Modules: modules})
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/api/auth/mode", nil)
			svc.Gate("admin", echoHandler()).ServeHTTP(rec, req)
			mustNotReached(t, rec)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}
			if got := rec.Body.String(); got != `{"methods":["admin_pin"]}`+"\n" {
				t.Fatalf("body = %q, want exactly admin_pin", got)
			}
		})
	}
}

// --- Step 3: unprotected pass-through, including login routes ---

func TestGateUnprotectedModulePassesEverythingThrough(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	svc := newGateService(t, now, &Policy{Modules: map[string]ModulePolicy{}})

	paths := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/some/asset.js"},
		{http.MethodPost, "/api/auth/login"},
		{http.MethodPost, "/api/auth/passkey/register/begin"},
		{http.MethodDelete, "/api/auth/passkeys/abc"},
		{http.MethodGet, "/api/whatever"},
	}

	for _, p := range paths {
		t.Run(p.method+" "+p.path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(p.method, p.path, nil)
			svc.Gate("grocery", echoHandler()).ServeHTTP(rec, req)
			mustReached(t, rec)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 (next's own status)", rec.Code)
			}
		})
	}
}

// TestGateUnprotectedPassthroughByteIdentical verifies that for an
// unprotected module, running a request through the gate produces a
// byte-identical response (status, headers, body) to calling next
// directly -- mode/healthz paths excepted, since those are intercepted by
// design.
func TestGateUnprotectedPassthroughByteIdentical(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	svc := newGateService(t, now, &Policy{Modules: map[string]ModulePolicy{}})

	next := func() http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Custom", "module-value")
			w.WriteHeader(http.StatusTeapot)
			w.Write([]byte("module body for " + r.Method + " " + r.URL.Path))
		})
	}

	cases := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/some/asset.js"},
		{http.MethodPost, "/api/auth/login"},
		{http.MethodGet, "/api/items"},
	}

	for _, c := range cases {
		t.Run(c.method+" "+c.path, func(t *testing.T) {
			recGate := httptest.NewRecorder()
			reqGate := httptest.NewRequest(c.method, c.path, nil)
			svc.Gate("grocery", next()).ServeHTTP(recGate, reqGate)

			recDirect := httptest.NewRecorder()
			reqDirect := httptest.NewRequest(c.method, c.path, nil)
			next().ServeHTTP(recDirect, reqDirect)

			if recGate.Code != recDirect.Code {
				t.Fatalf("status: gate=%d direct=%d", recGate.Code, recDirect.Code)
			}
			if recGate.Body.String() != recDirect.Body.String() {
				t.Fatalf("body: gate=%q direct=%q", recGate.Body.String(), recDirect.Body.String())
			}
			if recGate.Header().Get("X-Custom") != recDirect.Header().Get("X-Custom") {
				t.Fatalf("X-Custom header differs: gate=%q direct=%q", recGate.Header().Get("X-Custom"), recDirect.Header().Get("X-Custom"))
			}
		})
	}
}

// TestGateBearerOnOpenModulePassesThroughIdentically proves a bearer token
// presented against an open module is inert: the request passes through
// exactly as it would with no header at all (no key check ever runs on an
// unprotected module).
func TestGateBearerOnOpenModulePassesThroughIdentically(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	svc := newGateService(t, now, &Policy{Modules: map[string]ModulePolicy{}})

	withKey := httptest.NewRecorder()
	reqWithKey := httptest.NewRequest(http.MethodGet, "/api/items", nil)
	reqWithKey.Header.Set("Authorization", "Bearer whatever-not-configured")
	svc.Gate("grocery", echoHandler()).ServeHTTP(withKey, reqWithKey)

	withoutKey := httptest.NewRecorder()
	reqWithoutKey := httptest.NewRequest(http.MethodGet, "/api/items", nil)
	svc.Gate("grocery", echoHandler()).ServeHTTP(withoutKey, reqWithoutKey)

	mustReached(t, withKey)
	mustReached(t, withoutKey)
	if withKey.Code != withoutKey.Code || withKey.Body.String() != withoutKey.Body.String() {
		t.Fatalf("bearer on open module diverged: withKey=%d/%q withoutKey=%d/%q", withKey.Code, withKey.Body.String(), withoutKey.Code, withoutKey.Body.String())
	}
}

// --- Step 4/6: protected module, no credentials ---

func TestGateProtectedNoCredentials(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	svc := newGateService(t, now, &Policy{
		Modules: map[string]ModulePolicy{"grocery": {PinFile: "/tmp/does-not-matter"}},
		LDAP:    config.LDAPConfig{URL: "ldap://fake"},
	})

	t.Run("API path, no accept header -> JSON 401", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/items", nil)
		svc.Gate("grocery", echoHandler()).ServeHTTP(rec, req)
		mustNotReached(t, rec)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
			t.Fatalf("Content-Type = %q, want application/json", ct)
		}
	})

	t.Run("GET html non-api path -> 401 html login page", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/some/page.html", nil)
		req.Header.Set("Accept", "text/html")
		svc.Gate("grocery", echoHandler()).ServeHTTP(rec, req)
		mustNotReached(t, rec)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", rec.Code)
		}
		ct := rec.Header().Get("Content-Type")
		if !contains(ct, "text/html") {
			t.Fatalf("Content-Type = %q, want text/html", ct)
		}
	})

	t.Run("GET /api/ path with Accept text/html -> still JSON 401", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/items", nil)
		req.Header.Set("Accept", "text/html")
		svc.Gate("grocery", echoHandler()).ServeHTTP(rec, req)
		mustNotReached(t, rec)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", rec.Code)
		}
		ct := rec.Header().Get("Content-Type")
		if ct != "application/json" {
			t.Fatalf("Content-Type = %q, want application/json (heuristic must never mask API status)", ct)
		}
		body := rec.Body.String()
		if !strings.HasPrefix(strings.TrimSpace(body), "{") {
			t.Fatalf("body = %q, want a JSON envelope, never the HTML login page", body)
		}
		if contains(body, "pin-form") || contains(body, "<!DOCTYPE") {
			t.Fatalf("body = %q, must never be the HTML login page on an /api/ path", body)
		}
	})
}

// --- Step 5: grant-based authorization ---

// TestGatePinGrantScopedToItsModule proves a door-code session (grant
// "pin:todo") authorizes exactly the module it was issued for -- denied on
// a different protected module, accepted on its own.
func TestGatePinGrantScopedToItsModule(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	p := &Policy{
		Modules: map[string]ModulePolicy{
			"todo":        {PinFile: "/tmp/todo-pin"},
			"obsidianoid": {PinFile: "/tmp/obsidianoid-pin"},
		},
		LDAP:            config.LDAPConfig{URL: "ldap://fake"},
		SessionTTL:      time.Hour,
		RefreshFraction: 0.5,
	}
	svc := newGateService(t, now, p)
	tok := sessionCookieToken(t, "", []string{pinGrant("todo")}, time.Hour, now)

	t.Run("denied on obsidianoid", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/items", nil)
		req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: tok})
		svc.Gate("obsidianoid", echoHandler()).ServeHTTP(rec, req)
		mustNotReached(t, rec)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", rec.Code)
		}
	})

	t.Run("accepted on todo", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/items", nil)
		req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: tok})
		svc.Gate("todo", echoHandler()).ServeHTTP(rec, req)
		mustReached(t, rec)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
	})
}

// TestGateLDAPGrantReachesEveryProtectedNonAdminModule proves an "ldap"
// identity grant authorizes every protected non-admin module.
func TestGateLDAPGrantReachesEveryProtectedNonAdminModule(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	p := &Policy{
		Modules: map[string]ModulePolicy{
			"menuserver":  {},
			"obsidianoid": {},
			"multissh":    {},
		},
		LDAP:            config.LDAPConfig{URL: "ldap://fake"},
		SessionTTL:      time.Hour,
		RefreshFraction: 0.5,
	}
	svc := newGateService(t, now, p)
	tok := sessionCookieToken(t, "alice", []string{"ldap"}, time.Hour, now)

	for _, module := range []string{"menuserver", "obsidianoid", "multissh"} {
		t.Run(module, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/api/items", nil)
			req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: tok})
			svc.Gate(module, echoHandler()).ServeHTTP(rec, req)
			mustReached(t, rec)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}
		})
	}
}

// TestGateLegacyClaimsTokenIsDenied proves a session token signed under the
// old "methods" claim key (pre-cutover) decodes to an empty Grants slice
// under the new sessionClaims shape and is denied on a protected module --
// no panic, no accidental authorization.
func TestGateLegacyClaimsTokenIsDenied(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	p := &Policy{
		Modules:         map[string]ModulePolicy{"grocery": {}},
		LDAP:            config.LDAPConfig{URL: "ldap://fake"},
		SessionTTL:      time.Hour,
		RefreshFraction: 0.5,
	}
	svc := newGateService(t, now, p)

	for name, oldMethods := range map[string][]string{
		"pin":  {"pin"},
		"ldap": {"ldap"},
		"key":  {"key"},
	} {
		t.Run(name, func(t *testing.T) {
			tok := legacyMethodsToken(t, "alice", oldMethods, time.Hour, now)
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/api/items", nil)
			req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: tok})

			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("panic handling legacy-claims token: %v", r)
					}
				}()
				svc.Gate("grocery", echoHandler()).ServeHTTP(rec, req)
			}()

			mustNotReached(t, rec)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", rec.Code)
			}
		})
	}
}

// legacyClaims mirrors the pre-cutover (Decision B1) session claim shape --
// "methods" instead of "grants" -- so tests can simulate a token signed
// before the rename.
type legacyClaims struct {
	Methods []string `json:"methods"`
	jwt.RegisteredClaims
}

// legacyMethodsToken hand-signs a JWT carrying the pre-cutover "methods"
// claim key instead of "grants", simulating a session issued before the
// grants rename (Decision B1).
func legacyMethodsToken(t *testing.T, sub string, methods []string, ttl time.Duration, now time.Time) string {
	t.Helper()
	claims := legacyClaims{
		Methods: methods,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   sub,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
	}
	tok, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(gateTestKey())
	if err != nil {
		t.Fatalf("sign legacy claims: %v", err)
	}
	return tok
}

// --- Step 5: bearer API key ---

// TestGateAPIKeyModule proves a bearer key satisfies any protected
// non-admin module unconditionally, with no per-module opt-in.
func TestGateAPIKeyModule(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	p := &Policy{
		Modules: map[string]ModulePolicy{"menuserver": {}},
		APIKeys: []config.NamedHash{{Name: "svc", Hash: hashKey("secret-key")}},
	}
	svc := newGateService(t, now, p)

	t.Run("valid key reaches next with no Set-Cookie", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/items", nil)
		req.Header.Set("X-API-Key", "secret-key")
		svc.Gate("menuserver", echoHandler()).ServeHTTP(rec, req)
		mustReached(t, rec)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		if rec.Header().Get("Set-Cookie") != "" {
			t.Fatalf("Set-Cookie present for API key auth, want none: %q", rec.Header().Get("Set-Cookie"))
		}
	})

	t.Run("invalid key, no cookie -> 401", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/items", nil)
		req.Header.Set("X-API-Key", "wrong-key")
		svc.Gate("menuserver", echoHandler()).ServeHTTP(rec, req)
		mustNotReached(t, rec)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", rec.Code)
		}
	})
}

// TestGateAPIKeyNeverAuthorizesAdmin proves a valid bearer key never
// satisfies admin -- admin stays on its 401/login path even with a
// perfectly valid API key presented.
func TestGateAPIKeyNeverAuthorizesAdmin(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	p := &Policy{
		Modules:  map[string]ModulePolicy{},
		AdminPIN: hashFor(t, "9999"),
		APIKeys:  []config.NamedHash{{Name: "svc", Hash: hashKey("secret-key")}},
	}
	svc := newGateService(t, now, p)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/config/auth", nil)
	req.Header.Set("X-API-Key", "secret-key")
	svc.Gate("admin", echoHandler()).ServeHTTP(rec, req)
	mustNotReached(t, rec)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (bearer key must never authorize admin)", rec.Code)
	}
}

// --- Sliding refresh ---

func TestGateSlidingRefresh(t *testing.T) {
	issuedAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	ttl := time.Hour
	p := &Policy{
		Modules:         map[string]ModulePolicy{"grocery": {PinFile: "/tmp/does-not-matter"}},
		LDAP:            config.LDAPConfig{URL: "ldap://fake"},
		SessionTTL:      ttl,
		RefreshFraction: 0.5,
	}

	tok := sessionCookieToken(t, "alice", []string{"ldap"}, ttl, issuedAt)

	t.Run("stale token gets a refreshed Set-Cookie", func(t *testing.T) {
		now := issuedAt.Add(45 * time.Minute) // > 50% of ttl
		svc := newGateService(t, now, p)
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/items", nil)
		req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: tok})
		svc.Gate("grocery", echoHandler()).ServeHTTP(rec, req)
		mustReached(t, rec)
		if rec.Header().Get("Set-Cookie") == "" {
			t.Fatal("expected Set-Cookie for a stale session, got none")
		}
	})

	t.Run("fresh token gets no Set-Cookie", func(t *testing.T) {
		now := issuedAt.Add(5 * time.Minute) // < 50% of ttl
		svc := newGateService(t, now, p)
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/items", nil)
		req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: tok})
		svc.Gate("grocery", echoHandler()).ServeHTTP(rec, req)
		mustReached(t, rec)
		if rec.Header().Get("Set-Cookie") != "" {
			t.Fatalf("expected no Set-Cookie for a fresh session, got %q", rec.Header().Get("Set-Cookie"))
		}
	})
}

// --- Hijacker survival ---

// hijackableRecorder wraps httptest.ResponseRecorder to additionally
// satisfy http.Hijacker, so the test can assert that a handler downstream
// of the gate still sees a hijackable writer -- proof the gate never wraps
// the ResponseWriter it was given.
type hijackableRecorder struct {
	*httptest.ResponseRecorder
}

func (h *hijackableRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return nil, nil, nil
}

func TestGateHijackerSurvivesUpgradeRequest(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	p := &Policy{
		Modules:         map[string]ModulePolicy{"multissh": {}},
		LDAP:            config.LDAPConfig{URL: "ldap://fake"},
		SessionTTL:      time.Hour,
		RefreshFraction: 0.5,
	}
	svc := newGateService(t, now, p)

	tok := sessionCookieToken(t, "alice", []string{"ldap"}, time.Hour, now)

	var sawHijacker bool
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, sawHijacker = w.(http.Hijacker)
		w.WriteHeader(http.StatusOK)
	})

	rec := &hijackableRecorder{ResponseRecorder: httptest.NewRecorder()}
	req := httptest.NewRequest(http.MethodGet, "/ws", nil)
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: tok})

	svc.Gate("multissh", next).ServeHTTP(rec, req)

	if !sawHijacker {
		t.Fatal("next's ResponseWriter did not type-assert to http.Hijacker; the gate must never wrap the writer")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (request should have reached next)", rec.Code)
	}
}

// --- Session-required passkey routes ---

func TestGateSessionRequiredPasskeyRoutes(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	p := &Policy{
		Modules:         map[string]ModulePolicy{"grocery": {PinFile: "/tmp/does-not-matter"}},
		LDAP:            config.LDAPConfig{URL: "ldap://fake"},
		SessionTTL:      time.Hour,
		RefreshFraction: 0.5,
	}
	svc := newGateService(t, now, p)

	t.Run("without session -> 401 before the stub", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/auth/passkeys", nil)
		svc.Gate("grocery", echoHandler()).ServeHTTP(rec, req)
		mustNotReached(t, rec)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401 (session precondition must run before the stub)", rec.Code)
		}
	})

	t.Run("door-code-only session -> 403 requires_ldap", func(t *testing.T) {
		tok := sessionCookieToken(t, "", []string{pinGrant("grocery")}, time.Hour, now)
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/auth/passkeys", nil)
		req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: tok})
		svc.Gate("grocery", echoHandler()).ServeHTTP(rec, req)
		mustNotReached(t, rec)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403 (door-code session must not manage passkeys)", rec.Code)
		}
		if !contains(rec.Body.String(), "requires a full (LDAP) login") {
			t.Fatalf("body = %q, want it to explain the LDAP requirement", rec.Body.String())
		}
	})

	t.Run("ldap session -> handler reached (no passkey service configured in this policy)", func(t *testing.T) {
		tok := sessionCookieToken(t, "alice", []string{"ldap"}, time.Hour, now)
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/auth/passkeys", nil)
		req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: tok})
		svc.Gate("grocery", echoHandler()).ServeHTTP(rec, req)
		mustNotReached(t, rec)
		// This policy has no configured passkey service (p.passkeys is nil)
		// -- the handler itself fails closed on that, proving the ldap-grant
		// precondition ran and let the request through to the stub.
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400 (handler reached, no passkey service configured)", rec.Code)
		}
	})

	t.Run("DELETE /api/auth/passkeys/{id} without session -> 401", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodDelete, "/api/auth/passkeys/abc123", nil)
		svc.Gate("grocery", echoHandler()).ServeHTTP(rec, req)
		mustNotReached(t, rec)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", rec.Code)
		}
	})

	t.Run("DELETE /api/auth/passkeys/{id} door-code session -> 403", func(t *testing.T) {
		tok := sessionCookieToken(t, "", []string{pinGrant("grocery")}, time.Hour, now)
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodDelete, "/api/auth/passkeys/abc123", nil)
		req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: tok})
		svc.Gate("grocery", echoHandler()).ServeHTTP(rec, req)
		mustNotReached(t, rec)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403", rec.Code)
		}
	})
}

// --- Unauthenticated allowlist routes reach their real handlers directly ---

// TestGateUnauthenticatedAuthRoutesReachHandler proves routing reaches
// each unauthenticated-allowlist handler (as opposed to next, or a 401) --
// the handlers' full behavior is exercised in handlers_test.go, so this
// only checks each route is dispatched to its own handler's logic given a
// minimal/empty request, not next.ServeHTTP.
func TestGateUnauthenticatedAuthRoutesReachHandler(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	p := &Policy{Modules: map[string]ModulePolicy{"grocery": {PinFile: "/tmp/does-not-matter"}}}
	svc := newGateService(t, now, p)

	routes := []struct {
		method string
		path   string
		body   string
		want   int
	}{
		// Empty body -> JSON decode failure.
		{http.MethodPost, "/api/auth/login", "", http.StatusBadRequest},
		// Logout never inspects the body and always succeeds.
		{http.MethodPost, "/api/auth/logout", "", http.StatusOK},
		// No cookie -> unauthorized.
		{http.MethodGet, "/api/auth/session", "", http.StatusUnauthorized},
		// grocery's policy has no passkey service configured -- "passkey"
		// disallowed, rejected before the (empty) body would even matter.
		{http.MethodPost, "/api/auth/passkey/login/begin", "", http.StatusBadRequest},
		{http.MethodPost, "/api/auth/passkey/login/finish", "", http.StatusBadRequest},
	}

	for _, rt := range routes {
		t.Run(rt.method+" "+rt.path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(rt.method, rt.path, strings.NewReader(rt.body))
			svc.Gate("grocery", echoHandler()).ServeHTTP(rec, req)
			mustNotReached(t, rec)
			if rec.Code != rt.want {
				t.Fatalf("status = %d, want %d", rec.Code, rt.want)
			}
		})
	}
}

// --- Method mismatch on gate-owned routes -> 405 ---

func TestGateAuthRouteMethodMismatch(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	p := &Policy{Modules: map[string]ModulePolicy{"grocery": {}}}
	svc := newGateService(t, now, p)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/auth/login", nil)
	svc.Gate("grocery", echoHandler()).ServeHTTP(rec, req)
	mustNotReached(t, rec)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

// --- admin always protected, even with an empty/absent matrix entry ---

func TestGateAdminAlwaysProtected(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	for name, modules := range adminEmptyMatrixEncodings() {
		t.Run(name, func(t *testing.T) {
			p := &Policy{Modules: modules}
			svc := newGateService(t, now, p)
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/api/settings", nil)
			svc.Gate("admin", echoHandler()).ServeHTTP(rec, req)
			mustNotReached(t, rec)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", rec.Code)
			}
		})
	}
}

// ---------------------------------------------------------------------
// T5.2 -- break-glass enforcement (FR-M2), auth-package level.
//
// This section covers the items of security-plan.md's T5.2 that need the
// unexported Policy/Service seams (a hand-built Policy, SwapPolicy, an
// injected clock) rather than a real dispatcher: items 1, 3, 4, and 8 from
// the task's checklist. Item 2 (operator PIN file login, both encodings)
// and items 5-7 (the 0644 PIN file, editing the file without a swap, and
// the throttle) live in handlers_test.go since they go through
// handleLogin. One end-to-end dispatcher-level round trip per encoding
// lives in cmd/server/dispatcher_auth_test.go.
// ---------------------------------------------------------------------

// adminEmptyMatrixEncodings names the two ways the converged reviewer
// ruling (security-plan.md T5.2) requires "empty assignment matrix" to be
// exercised: an explicit "admin": {} entry, and no "admin" key in the
// matrix at all. Every admin break-glass test in this file and
// handlers_test.go that claims to cover "both encodings" runs both of
// these.
func adminEmptyMatrixEncodings() map[string]map[string]ModulePolicy {
	return map[string]map[string]ModulePolicy{
		"explicit admin: {}": {"admin": {}},
		"no admin key":       {},
	}
}

// TestGateAdminEmptyMatrixUnauthenticatedAndLoginRoutes proves T5.2 item 1
// for both empty-matrix encodings: an unauthenticated admin data route
// 401s, and the gate-owned login route is still served (reaching
// handleLogin, not the module) even though admin has no matrix entry.
func TestGateAdminEmptyMatrixUnauthenticatedAndLoginRoutes(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for name, modules := range adminEmptyMatrixEncodings() {
		t.Run(name, func(t *testing.T) {
			p := &Policy{Modules: modules}
			svc := newGateService(t, now, p)

			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/api/config/auth", nil)
			svc.Gate("admin", echoHandler()).ServeHTTP(rec, req)
			mustNotReached(t, rec)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("unauthenticated admin data route: status = %d, want 401", rec.Code)
			}

			// A gate-owned login route reaches handleLogin -- not the
			// module -- regardless of admin's (empty) matrix entry: an
			// empty body reaches its JSON-decode step (400), never next.
			recLogin := httptest.NewRecorder()
			reqLogin := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
			svc.Gate("admin", echoHandler()).ServeHTTP(recLogin, reqLogin)
			mustNotReached(t, recLogin)
			if recLogin.Code != http.StatusBadRequest {
				t.Fatalf("POST /api/auth/login: status = %d, want 400 (reached handleLogin's decode step, not the module)", recLogin.Code)
			}
		})
	}
}

// TestGateAdminEmptyMatrixModeListsAdminPIN proves T5.2 item 3 for both
// empty-matrix encodings: GET /api/auth/mode on admin always contains
// "admin_pin", unauthenticated, regardless of which encoding produced the
// empty matrix.
func TestGateAdminEmptyMatrixModeListsAdminPIN(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for name, modules := range adminEmptyMatrixEncodings() {
		t.Run(name, func(t *testing.T) {
			p := &Policy{Modules: modules}
			svc := newGateService(t, now, p)

			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/api/auth/mode", nil)
			svc.Gate("admin", echoHandler()).ServeHTTP(rec, req)
			mustNotReached(t, rec)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}
			if !contains(rec.Body.String(), `"admin_pin"`) {
				t.Fatalf("body = %q, want it to contain \"admin_pin\"", rec.Body.String())
			}
		})
	}
}

// TestGateAdminSwapDeletingMatrixEntryStaysProtected proves T5.2 item 4: a
// live matrix save that deletes admin's entry entirely (legal per FR-M5,
// modeling what T5.3's applyAuth does -- BuildPolicy from a fresh
// config.AuthConfig, then SwapPolicy) leaves admin protected on the very
// next request, exactly as if it had never had an entry.
func TestGateAdminSwapDeletingMatrixEntryStaysProtected(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	initial, err := BuildPolicy(config.AuthConfig{
		Modules:  map[string]config.ModuleAuthConfig{"admin": {}},
		AdminPIN: hashFor(t, "9999"),
	})
	if err != nil {
		t.Fatalf("BuildPolicy(initial): %v", err)
	}
	svc := newGateService(t, now, initial)

	// Sanity: admin currently has a matrix entry and is (still) protected.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/config/auth", nil)
	svc.Gate("admin", echoHandler()).ServeHTTP(rec, req)
	mustNotReached(t, rec)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("before swap: status = %d, want 401", rec.Code)
	}

	// A live save deletes admin's matrix entry entirely.
	deleted, err := BuildPolicy(config.AuthConfig{
		Modules:  map[string]config.ModuleAuthConfig{},
		AdminPIN: hashFor(t, "9999"),
	})
	if err != nil {
		t.Fatalf("BuildPolicy(deleted): %v", err)
	}
	svc.SwapPolicy(deleted)

	// Admin is still protected on the very next request.
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/api/config/auth", nil)
	svc.Gate("admin", echoHandler()).ServeHTTP(rec2, req2)
	mustNotReached(t, rec2)
	if rec2.Code != http.StatusUnauthorized {
		t.Fatalf("after swap deleting admin's matrix entry: status = %d, want 401", rec2.Code)
	}
}

// adminModeMethods issues GET /api/auth/mode against svc for the admin
// module and returns the decoded methods list.
func adminModeMethods(t *testing.T, svc *Service) []string {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/auth/mode", nil)
	svc.Gate("admin", echoHandler()).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/auth/mode: status = %d, want 200", rec.Code)
	}
	var body struct {
		Methods []string `json:"methods"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode mode body: %v", err)
	}
	return body.Methods
}

// TestGateAdminModeNeverGainsMethodsFromMatrix proves T5.2 item 8's new-model
// equivalent: since OfferedMethods("admin") is exactly ["admin_pin"] by
// construction (never read off Modules["admin"]), no matrix save -- adding
// an admin entry, deleting it, anything -- ever changes what admin's mode
// endpoint reports.
func TestGateAdminModeNeverGainsMethodsFromMatrix(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	start, err := BuildPolicy(config.AuthConfig{Modules: map[string]config.ModuleAuthConfig{}})
	if err != nil {
		t.Fatalf("BuildPolicy(start): %v", err)
	}
	svc := newGateService(t, now, start)

	assertAdminPINOnly := func(t *testing.T) {
		t.Helper()
		methods := adminModeMethods(t, svc)
		if len(methods) != 1 || methods[0] != adminPINMethod {
			t.Fatalf("methods = %v, want [admin_pin] only", methods)
		}
	}
	assertAdminPINOnly(t)

	// A matrix save adds an admin entry with a pin_file -- admin's mode is
	// still exactly admin_pin; a module-scoped door code never applies to
	// admin.
	withEntry, err := BuildPolicy(config.AuthConfig{
		Modules: map[string]config.ModuleAuthConfig{"admin": {PinFile: "/tmp/whatever"}},
	})
	if err != nil {
		t.Fatalf("BuildPolicy(withEntry): %v", err)
	}
	svc.SwapPolicy(withEntry)
	assertAdminPINOnly(t)

	// Deleting it again changes nothing either.
	removed, err := BuildPolicy(config.AuthConfig{Modules: map[string]config.ModuleAuthConfig{}})
	if err != nil {
		t.Fatalf("BuildPolicy(removed): %v", err)
	}
	svc.SwapPolicy(removed)
	assertAdminPINOnly(t)
}
