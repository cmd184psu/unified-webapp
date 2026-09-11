package auth

import (
	"bufio"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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

// --- Step 1: healthz ---

func TestGateHealthzAlwaysOK(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	unprotected := newGateService(t, now, &Policy{})
	protected := newGateService(t, now, &Policy{Modules: map[string][]string{"grocery": {"pin"}}})

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

// --- Step 2: mode ---

func TestGateModeReportsAcceptedMethods(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name    string
		policy  *Policy
		module  string
		want    string // exact expected JSON body
		wantSub string // substring check when want == ""
	}{
		{
			name:   "unprotected module",
			policy: &Policy{Modules: map[string][]string{}},
			module: "grocery",
			want:   `{"methods":[]}` + "\n",
		},
		{
			name:   "pin-protected module",
			policy: &Policy{Modules: map[string][]string{"grocery": {"pin"}}},
			module: "grocery",
			want:   `{"methods":["pin"]}` + "\n",
		},
		{
			name:    "admin with empty matrix",
			policy:  &Policy{Modules: map[string][]string{}},
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

// --- Step 3: unprotected pass-through, including login routes ---

func TestGateUnprotectedModulePassesEverythingThrough(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	svc := newGateService(t, now, &Policy{Modules: map[string][]string{}})

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
	svc := newGateService(t, now, &Policy{Modules: map[string][]string{}})

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

// --- Step 4/6: protected module, no credentials ---

func TestGateProtectedNoCredentials(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	svc := newGateService(t, now, &Policy{Modules: map[string][]string{"grocery": {"pin"}}})

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

// --- Step 5: cookie verification ---

func TestGateValidCookieIntersectingMethod(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	p := &Policy{
		Modules:         map[string][]string{"grocery": {"pin", "ldap"}},
		SessionTTL:      time.Hour,
		RefreshFraction: 0.5,
	}
	svc := newGateService(t, now, p)

	tok, err := issueToken(gateTestKey(), "alice", []string{"pin"}, time.Hour, now)
	if err != nil {
		t.Fatalf("issueToken: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/items", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: tok})
	svc.Gate("grocery", echoHandler()).ServeHTTP(rec, req)
	mustReached(t, rec)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestGateValidCookieNonIntersectingMethod(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	p := &Policy{
		Modules:         map[string][]string{"grocery": {"ldap"}},
		SessionTTL:      time.Hour,
		RefreshFraction: 0.5,
	}
	svc := newGateService(t, now, p)

	tok, err := issueToken(gateTestKey(), "alice", []string{"pin"}, time.Hour, now)
	if err != nil {
		t.Fatalf("issueToken: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/items", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: tok})
	svc.Gate("grocery", echoHandler()).ServeHTTP(rec, req)
	mustNotReached(t, rec)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

// --- Step 5: API key ---

func TestGateAPIKeyModule(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	p := &Policy{
		Modules: map[string][]string{"grocery": {"key"}},
		APIKeys: []config.NamedHash{{Name: "svc", Hash: hashKey("secret-key")}},
	}
	svc := newGateService(t, now, p)

	t.Run("valid key reaches next with no Set-Cookie", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/items", nil)
		req.Header.Set("X-API-Key", "secret-key")
		svc.Gate("grocery", echoHandler()).ServeHTTP(rec, req)
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
		svc.Gate("grocery", echoHandler()).ServeHTTP(rec, req)
		mustNotReached(t, rec)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", rec.Code)
		}
	})
}

// --- Sliding refresh ---

func TestGateSlidingRefresh(t *testing.T) {
	issuedAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	ttl := time.Hour
	p := &Policy{
		Modules:         map[string][]string{"grocery": {"pin"}},
		SessionTTL:      ttl,
		RefreshFraction: 0.5,
	}

	tok, err := issueToken(gateTestKey(), "alice", []string{"pin"}, ttl, issuedAt)
	if err != nil {
		t.Fatalf("issueToken: %v", err)
	}

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
		Modules:         map[string][]string{"multissh": {"pin"}},
		SessionTTL:      time.Hour,
		RefreshFraction: 0.5,
	}
	svc := newGateService(t, now, p)

	tok, err := issueToken(gateTestKey(), "alice", []string{"pin"}, time.Hour, now)
	if err != nil {
		t.Fatalf("issueToken: %v", err)
	}

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
		Modules:         map[string][]string{"grocery": {"pin", "passkey"}},
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

	t.Run("with valid session -> handler reached (no passkey service configured in this policy)", func(t *testing.T) {
		tok, err := issueToken(gateTestKey(), "alice", []string{"pin"}, time.Hour, now)
		if err != nil {
			t.Fatalf("issueToken: %v", err)
		}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/auth/passkeys", nil)
		req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: tok})
		svc.Gate("grocery", echoHandler()).ServeHTTP(rec, req)
		mustNotReached(t, rec)
		// This test's Policy lists "passkey" in grocery's matrix but never
		// configures a passkey service (p.passkeys is nil) -- an
		// inconsistent state BuildPolicy would never produce, but one the
		// handler must still fail closed on defensively, proving the
		// session precondition ran before the handler.
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
}

// --- Unauthenticated allowlist routes reach their real handlers directly ---

// TestGateUnauthenticatedAuthRoutesReachHandler proves routing reaches
// each unauthenticated-allowlist handler (as opposed to next, or a 401) --
// the handlers' full behavior is exercised in handlers_test.go, so this
// only checks each route is dispatched to its own handler's logic given a
// minimal/empty request, not next.ServeHTTP.
func TestGateUnauthenticatedAuthRoutesReachHandler(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	p := &Policy{Modules: map[string][]string{"grocery": {"pin"}}}
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
		// grocery's matrix here is ["pin"] only -- "passkey" disallowed,
		// rejected before the (empty) body would even matter.
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
	p := &Policy{Modules: map[string][]string{"grocery": {"pin"}}}
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

	for name, modules := range map[string]map[string][]string{
		"no admin entry":    {},
		"empty admin entry": {"admin": {}},
	} {
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
