// dispatcher_auth_test.go implements the T4.6 gate acceptance suite: the six
// ACs from security-plan.md (AC-1, AC-2, AC-3, AC-4, AC-5, AC-5b), each
// exercised end-to-end through buildDispatcher exactly as cmd/server/main.go
// wires it -- a real *auth.Service built from a config.AuthConfig literal via
// auth.FromConfig, gating a real dispatcher over real (temp-dir-backed)
// module builds.
//
// Two ACs are only partly provable from this package:
//
//   - AC-3's LDAP half ("LDAP login with the existing cookie -> accumulated
//     token opens both") needs a fake LDAP dialer with no network, and that
//     seam (Service.ldapDialer) is unexported -- package main cannot set it
//     on a Service built via auth.FromConfig. That half is proven instead in
//     internal/platform/auth/handlers_test.go's
//     TestCrossModuleAccumulationPINThenLDAP, against two Gate calls sharing
//     one Service (one Gate per module, exactly as buildDispatcher wires
//     it). This file proves the PIN half plus the 400-before-credential-check
//     behavior on the LDAP-only module.
//   - AC-5's "success resets" half needs an injectable clock to avoid
//     sleeping through a real backoff window; Service.now is real-time
//     (time.Now) once built via auth.FromConfig. That half is already proven
//     at the auth-package level, with a fake clock, by
//     internal/platform/auth/throttle_test.go's
//     TestThrottleSuccessResetsFailuresAndDelay and
//     internal/platform/auth/handlers_test.go's TestHandleLoginThrottle. This
//     file proves the sixth-attempt-429-with-Retry-After half, which needs
//     no clock control.
//
// goleak: this package has no TestMain wrapping tests in goleak.VerifyTestMain
// -- see the note at the bottom of this file for why one was not added.
package main

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	"cmd184psu/unified-webapp/internal/platform/auth"
	"cmd184psu/unified-webapp/internal/platform/config"
	"cmd184psu/unified-webapp/internal/platform/middleware"
)

// bcryptHash returns pin's bcrypt hash at the minimum cost -- these tests
// need a valid hash, not a slow one.
func bcryptHash(t *testing.T, pin string) string {
	t.Helper()
	h, err := bcrypt.GenerateFromPassword([]byte(pin), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("bcrypt hash: %v", err)
	}
	return string(h)
}

// apiKeyHash returns the "sha256:<hex>" config hash for the literal key
// string, matching what a client sends and what checkAPIKey computes
// (apikey.go).
func apiKeyHash(key string) string {
	sum := sha256.Sum256([]byte(key))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// buildControlDispatcher builds a dispatcher identical to buildDispatcher
// except it never applies the auth gate. It stands in for "today's"
// dispatcher -- the one that existed before the gate was mounted -- so AC-1
// can assert a byte-for-byte identical response instead of a hard-coded
// status/body: every SPA module's static handler serves index.html for an
// unmatched path (some 200, some -- multissh's /api/ prefix rule -- 404), and
// a hand-picked expectation would be wrong for at least one module.
func buildControlDispatcher(cfg *config.Config) *Dispatcher {
	dispatch := newDispatcher()
	built := make(map[string]http.Handler, len(cfg.Routing))
	for host, module := range cfg.Routing {
		h, ok := built[module]
		if !ok {
			hh, err := buildModule(module, cfg)
			if err != nil {
				h = unavailableHandler(module, err)
			} else {
				h = middleware.BodyLimit(limitFor(module, cfg), hh)
			}
			built[module] = h
		}
		dispatch.register(host, h)
	}
	return dispatch
}

// doHostWithCookie issues method/path against host on srv, carrying cookie
// when non-nil.
func doHostWithCookie(t *testing.T, srv *httptest.Server, method, host, path string, cookie *http.Cookie) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, srv.URL+path, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Host = host
	if cookie != nil {
		req.AddCookie(cookie)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	return res
}

// doHostWithKey issues a GET against host/path on srv, carrying key as
// X-API-Key when non-empty.
func doHostWithKey(t *testing.T, srv *httptest.Server, host, path, key string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, srv.URL+path, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Host = host
	if key != "" {
		req.Header.Set("X-API-Key", key)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	return res
}

// firstSetCookie returns the value of the first Set-Cookie header on res, if
// any.
func firstSetCookie(res *http.Response) (*http.Cookie, bool) {
	cookies := res.Cookies()
	if len(cookies) == 0 {
		return nil, false
	}
	return cookies[0], true
}

// ---------------------------------------------------------------------
// AC-1: config without auth serves exactly today's behavior.
// ---------------------------------------------------------------------

// TestAC1_NoAuthConfigIsToday proves security-plan.md's AC-1: a config with
// no "auth" section changes nothing about any routed module's behavior. It
// compares the dispatcher-under-test (built with a Service from a zero-value
// AuthConfig, exactly as buildDispatcher wires it in production) against a
// control dispatcher that never applies the gate at all, over every routed
// hostname -- proving the gate is invisible rather than asserting a
// hand-picked status code, which the per-module SPA fallback would make
// wrong for at least one module (menuserver/multissh differ on an unmatched
// /api/ path, see buildControlDispatcher). It also asserts the two additions
// AC-1 permits (GET /api/auth/mode, GET /healthz) and that no response ever
// carries a Set-Cookie.
func TestAC1_NoAuthConfigIsToday(t *testing.T) {
	cfg := routeMatrixConfig(t, "off")
	// cfg.Auth is left at its zero value: "config without auth".
	svc := noAuthService(t)

	real := httptest.NewServer(middleware.Wrap(buildDispatcher(cfg, svc)))
	defer real.Close()
	control := httptest.NewServer(middleware.Wrap(buildControlDispatcher(cfg)))
	defer control.Close()

	for host := range cfg.Routing {
		t.Run(host, func(t *testing.T) {
			loginBody := `{"method":"pin","pin":"0000"}`

			realRes := doHost(t, real, http.MethodPost, host, "/api/auth/login", loginBody)
			realBody, _ := io.ReadAll(realRes.Body)
			realRes.Body.Close()
			if _, ok := firstSetCookie(realRes); ok {
				t.Fatalf("POST /api/auth/login on an unprotected module set a cookie: %v", realRes.Header.Values("Set-Cookie"))
			}

			ctrlRes := doHost(t, control, http.MethodPost, host, "/api/auth/login", loginBody)
			ctrlBody, _ := io.ReadAll(ctrlRes.Body)
			ctrlRes.Body.Close()

			if realRes.StatusCode != ctrlRes.StatusCode {
				t.Fatalf("POST /api/auth/login: status = %d, control (no gate at all) = %d", realRes.StatusCode, ctrlRes.StatusCode)
			}
			if !bytes.Equal(realBody, ctrlBody) {
				t.Fatalf("POST /api/auth/login body differs from the no-gate control:\n real = %q\n ctrl = %q", realBody, ctrlBody)
			}

			// GET /api/auth/mode: the one addition, unauthenticated, empty
			// methods (no module has an auth.modules entry).
			modeRes := doHost(t, real, http.MethodGet, host, "/api/auth/mode", "")
			defer modeRes.Body.Close()
			if modeRes.StatusCode != http.StatusOK {
				t.Fatalf("GET /api/auth/mode: status = %d, want 200", modeRes.StatusCode)
			}
			if _, ok := firstSetCookie(modeRes); ok {
				t.Fatal("GET /api/auth/mode set a cookie")
			}
			var mode struct {
				Methods []string `json:"methods"`
			}
			if err := json.NewDecoder(modeRes.Body).Decode(&mode); err != nil {
				t.Fatalf("decode /api/auth/mode body: %v", err)
			}
			if len(mode.Methods) != 0 {
				t.Fatalf("methods = %v, want []", mode.Methods)
			}

			// GET /healthz: the other addition.
			hzRes := doHost(t, real, http.MethodGet, host, "/healthz", "")
			defer hzRes.Body.Close()
			if hzRes.StatusCode != http.StatusOK {
				t.Fatalf("GET /healthz: status = %d, want 200", hzRes.StatusCode)
			}
			if _, ok := firstSetCookie(hzRes); ok {
				t.Fatal("GET /healthz set a cookie")
			}
			var hz struct {
				OK bool `json:"ok"`
			}
			if err := json.NewDecoder(hzRes.Body).Decode(&hz); err != nil {
				t.Fatalf("decode /healthz body: %v", err)
			}
			if !hz.OK {
				t.Fatalf("healthz body ok = %v, want true", hz.OK)
			}

			// No cookie on an ordinary GET either.
			rootRes := doHost(t, real, http.MethodGet, host, "/", "")
			defer rootRes.Body.Close()
			if _, ok := firstSetCookie(rootRes); ok {
				t.Fatal("GET / set a cookie")
			}
		})
	}
}

// ---------------------------------------------------------------------
// AC-2: a config protecting all six modules 401s every route-matrix row.
// ---------------------------------------------------------------------

// ac2RouteMatrixRow names one unauthenticated request AC-2 must see 401 for.
type ac2RouteMatrixRow struct {
	name   string
	method string
	path   string
}

// ac2RouteMatrix is the per-module route matrix shared with T2.3
// (origincheck_route_test.go): a real data route, the static asset path, and
// the module's SSE endpoint where it has one (menuserver has none; multissh
// has a WS upgrade instead).
var ac2RouteMatrix = map[string][]ac2RouteMatrixRow{
	"grocery":     {{"data", http.MethodGet, "/api/items"}, {"static", http.MethodGet, "/"}, {"sse", http.MethodGet, "/api/events"}},
	"todo":        {{"data", http.MethodGet, "/items"}, {"static", http.MethodGet, "/"}, {"sse", http.MethodGet, "/api/events"}},
	"slideshow":   {{"data", http.MethodGet, "/api/state"}, {"static", http.MethodGet, "/"}, {"sse", http.MethodGet, "/api/events"}},
	"menuserver":  {{"data", http.MethodGet, "/items"}, {"static", http.MethodGet, "/"}},
	"obsidianoid": {{"data", http.MethodGet, "/api/tree"}, {"static", http.MethodGet, "/"}, {"sse", http.MethodGet, "/api/events"}},
	"multissh":    {{"data", http.MethodGet, "/api/hosts"}, {"static", http.MethodGet, "/"}, {"ws", http.MethodGet, "/api/ssh/ws"}},
}

// TestAC2_AllModulesProtected proves security-plan.md's AC-2: a config
// protecting every module (pin, for all six) 401s an unauthenticated request
// to every route-matrix row, and reports that module's accepted methods on
// GET /api/auth/mode. The modules under test are iterated from cfg.Auth.Modules
// itself, not hand-named, so a module the gate failed to wrap would be
// silently skipped rather than passed.
func TestAC2_AllModulesProtected(t *testing.T) {
	cfg := routeMatrixConfig(t, "off")
	cfg.Auth = config.AuthConfig{
		Modules: map[string][]string{
			"grocery":     {"pin"},
			"todo":        {"pin"},
			"slideshow":   {"pin"},
			"menuserver":  {"pin"},
			"obsidianoid": {"pin"},
			"multissh":    {"pin"},
		},
		PINs:    []config.NamedHash{{Name: "carol", Hash: bcryptHash(t, "4242")}},
		DataDir: t.TempDir(),
	}
	svc, err := auth.FromConfig(cfg.Auth, knownModules, false)
	if err != nil {
		t.Fatalf("auth.FromConfig: %v", err)
	}
	srv := httptest.NewServer(middleware.Wrap(buildDispatcher(cfg, svc)))
	defer srv.Close()

	hostForModule := make(map[string]string, len(cfg.Routing))
	for host, module := range cfg.Routing {
		hostForModule[module] = host
	}

	for module := range cfg.Auth.Modules {
		t.Run(module, func(t *testing.T) {
			host, ok := hostForModule[module]
			if !ok {
				t.Fatalf("no routed hostname for module %q", module)
			}
			rows := ac2RouteMatrix[module]
			if len(rows) == 0 {
				t.Fatalf("no route-matrix rows defined for module %q", module)
			}
			for _, row := range rows {
				t.Run(row.name, func(t *testing.T) {
					res := doHost(t, srv, row.method, host, row.path, "")
					defer res.Body.Close()
					if res.StatusCode != http.StatusUnauthorized {
						body, _ := io.ReadAll(res.Body)
						t.Fatalf("%s %s: status = %d, want 401 (body %q)", row.method, row.path, res.StatusCode, body)
					}
				})
			}

			modeRes := doHost(t, srv, http.MethodGet, host, "/api/auth/mode", "")
			defer modeRes.Body.Close()
			if modeRes.StatusCode != http.StatusOK {
				t.Fatalf("GET /api/auth/mode: status = %d, want 200", modeRes.StatusCode)
			}
			var mode struct {
				Methods []string `json:"methods"`
			}
			if err := json.NewDecoder(modeRes.Body).Decode(&mode); err != nil {
				t.Fatalf("decode /api/auth/mode body: %v", err)
			}
			if len(mode.Methods) != 1 || mode.Methods[0] != "pin" {
				t.Fatalf("methods = %v, want [pin]", mode.Methods)
			}
		})
	}
}

// ---------------------------------------------------------------------
// AC-3: accumulation across modules.
// ---------------------------------------------------------------------

// TestAC3_AccumulationAcrossModules proves the PIN half of security-plan.md's
// AC-3 at the dispatcher level: slideshow accepts only "pin", multissh only
// "ldap". A PIN login on slideshow opens slideshow but not multissh (the
// methods don't intersect), and a PIN login attempt on multissh is rejected
// 400 before any credential is examined (method enforcement runs first,
// handlers.go's handleLogin). The LDAP half ("LDAP login with the existing
// cookie -> accumulated token opens both") is proven in
// internal/platform/auth/handlers_test.go's
// TestCrossModuleAccumulationPINThenLDAP -- see this file's package doc
// comment for why it lives there.
func TestAC3_AccumulationAcrossModules(t *testing.T) {
	cfg := routeMatrixConfig(t, "off")
	cfg.Auth = config.AuthConfig{
		Modules: map[string][]string{
			"slideshow": {"pin"},
			"multissh":  {"ldap"},
		},
		PINs:    []config.NamedHash{{Name: "carol", Hash: bcryptHash(t, "4242")}},
		LDAP:    config.LDAPConfig{URL: "ldap://fake", BaseDN: "dc=example,dc=com"},
		DataDir: t.TempDir(),
	}
	svc, err := auth.FromConfig(cfg.Auth, knownModules, false)
	if err != nil {
		t.Fatalf("auth.FromConfig: %v", err)
	}
	srv := httptest.NewServer(middleware.Wrap(buildDispatcher(cfg, svc)))
	defer srv.Close()

	// PIN login on slideshow succeeds and sets a cookie.
	loginRes := doHost(t, srv, http.MethodPost, "slideshow.example", "/api/auth/login", `{"method":"pin","pin":"4242"}`)
	if loginRes.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(loginRes.Body)
		loginRes.Body.Close()
		t.Fatalf("pin login on slideshow: status = %d, want 200 (body %q)", loginRes.StatusCode, body)
	}
	cookie, ok := firstSetCookie(loginRes)
	loginRes.Body.Close()
	if !ok {
		t.Fatal("pin login on slideshow: expected a Set-Cookie")
	}

	// The cookie opens slideshow.
	openRes := doHostWithCookie(t, srv, http.MethodGet, "slideshow.example", "/", cookie)
	defer openRes.Body.Close()
	if openRes.StatusCode != http.StatusOK {
		t.Fatalf("slideshow with the pin cookie: status = %d, want 200", openRes.StatusCode)
	}

	// The same cookie 401s on multissh: its methods ("pin") don't intersect
	// multissh's accepted set ("ldap").
	blockedRes := doHostWithCookie(t, srv, http.MethodGet, "ssh.example", "/api/hosts", cookie)
	defer blockedRes.Body.Close()
	if blockedRes.StatusCode != http.StatusUnauthorized {
		t.Fatalf("multissh with the slideshow pin cookie: status = %d, want 401", blockedRes.StatusCode)
	}

	// A PIN login attempt on multissh (which does not accept "pin") is
	// rejected 400 before any credential is checked.
	badMethodRes := doHost(t, srv, http.MethodPost, "ssh.example", "/api/auth/login", `{"method":"pin","pin":"4242"}`)
	defer badMethodRes.Body.Close()
	if badMethodRes.StatusCode != http.StatusBadRequest {
		body, _ := io.ReadAll(badMethodRes.Body)
		t.Fatalf("pin login attempt on multissh: status = %d, want 400 (body %q)", badMethodRes.StatusCode, body)
	}
}

// ---------------------------------------------------------------------
// AC-4: token lifecycle.
// ---------------------------------------------------------------------

// testSessionClaims mirrors the JSON shape of auth's unexported sessionClaims
// (internal/platform/auth/session.go) so this test can mint and parse session
// tokens directly, signed with the real signing key read from the auth
// data_dir: Service exposes no seam for issuing a token with an arbitrary
// iat/exp, and the near-expiry/rotated-key/expired cases all need exactly
// that control.
type testSessionClaims struct {
	Methods []string `json:"methods"`
	jwt.RegisteredClaims
}

// signSessionToken mints an HS256 session token with the given key, methods,
// and issued-at/expiry, matching auth's session.go token shape.
func signSessionToken(t *testing.T, key []byte, subject string, methods []string, iat, exp time.Time) string {
	t.Helper()
	claims := testSessionClaims{
		Methods: methods,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   subject,
			IssuedAt:  jwt.NewNumericDate(iat),
			ExpiresAt: jwt.NewNumericDate(exp),
		},
	}
	tok, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(key)
	if err != nil {
		t.Fatalf("sign session token: %v", err)
	}
	return tok
}

// TestAC4_TokenLifecycle proves security-plan.md's AC-4: an expired,
// garbage, or wrong-key-signed session cookie is rejected 401, and a cookie
// more than half-way through its TTL (but not yet expired) is accepted and
// refreshed with a new Set-Cookie. Tokens are signed directly with
// golang-jwt against the real HMAC key read from the auth data_dir --
// Service issues tokens only through a real login, which cannot control
// iat/exp precisely enough for the near-expiry and rotated-key cases.
func TestAC4_TokenLifecycle(t *testing.T) {
	authDataDir := t.TempDir()
	cfg := routeMatrixConfig(t, "off")
	cfg.Auth = config.AuthConfig{
		Modules: map[string][]string{"slideshow": {"pin"}},
		PINs:    []config.NamedHash{{Name: "carol", Hash: bcryptHash(t, "4242")}},
		DataDir: authDataDir,
		Session: config.SessionConfig{TTLHours: 1}, // 1h TTL, default 0.5 refresh fraction.
	}
	svc, err := auth.FromConfig(cfg.Auth, knownModules, false)
	if err != nil {
		t.Fatalf("auth.FromConfig: %v", err)
	}

	// loadOrCreateKey (auth/key.go) writes <data_dir>/session.key on the
	// FromConfig call above, since a module is protected.
	realKey, err := os.ReadFile(filepath.Join(authDataDir, "session.key"))
	if err != nil {
		t.Fatalf("read session key: %v", err)
	}

	srv := httptest.NewServer(middleware.Wrap(buildDispatcher(cfg, svc)))
	defer srv.Close()

	now := time.Now()

	t.Run("expired token", func(t *testing.T) {
		tok := signSessionToken(t, realKey, "carol", []string{"pin"}, now.Add(-2*time.Hour), now.Add(-time.Hour))
		res := doHostWithCookie(t, srv, http.MethodGet, "slideshow.example", "/", &http.Cookie{Name: "uw_session", Value: tok})
		defer res.Body.Close()
		if res.StatusCode != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", res.StatusCode)
		}
	})

	t.Run("garbage cookie", func(t *testing.T) {
		res := doHostWithCookie(t, srv, http.MethodGet, "slideshow.example", "/", &http.Cookie{Name: "uw_session", Value: "not-a-jwt-at-all"})
		defer res.Body.Close()
		if res.StatusCode != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", res.StatusCode)
		}
	})

	t.Run("rotated key", func(t *testing.T) {
		otherKey := make([]byte, 32)
		if _, err := rand.Read(otherKey); err != nil {
			t.Fatalf("generate other key: %v", err)
		}
		tok := signSessionToken(t, otherKey, "carol", []string{"pin"}, now.Add(-time.Minute), now.Add(time.Hour))
		res := doHostWithCookie(t, srv, http.MethodGet, "slideshow.example", "/", &http.Cookie{Name: "uw_session", Value: tok})
		defer res.Body.Close()
		if res.StatusCode != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", res.StatusCode)
		}
	})

	t.Run("near-expiry refreshes the cookie", func(t *testing.T) {
		iat := now.Add(-40 * time.Minute) // > 50% of the 1h TTL has elapsed.
		exp := iat.Add(time.Hour)         // still valid: 20 minutes remain.
		tok := signSessionToken(t, realKey, "carol", []string{"pin"}, iat, exp)
		res := doHostWithCookie(t, srv, http.MethodGet, "slideshow.example", "/", &http.Cookie{Name: "uw_session", Value: tok})
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(res.Body)
			t.Fatalf("status = %d, want 200 (body %q)", res.StatusCode, body)
		}
		refreshed, ok := firstSetCookie(res)
		if !ok {
			t.Fatal("expected a refreshed Set-Cookie, got none")
		}
		if refreshed.Value == tok {
			t.Fatal("refreshed cookie has the same value as the near-expiry token")
		}
		claims := &testSessionClaims{}
		if _, err := jwt.ParseWithClaims(refreshed.Value, claims, func(*jwt.Token) (interface{}, error) { return realKey, nil }, jwt.WithValidMethods([]string{"HS256"})); err != nil {
			t.Fatalf("refreshed cookie does not parse as a valid session token: %v", err)
		}
		if claims.IssuedAt == nil || claims.IssuedAt.Time.Before(now.Add(-time.Minute)) {
			t.Fatalf("refreshed token's iat = %v, want close to now (%v)", claims.IssuedAt, now)
		}
	})
}

// ---------------------------------------------------------------------
// AC-5: throttle backoff.
// ---------------------------------------------------------------------

// TestAC5_ThrottleBackoff proves the half of security-plan.md's AC-5 that
// needs no clock control: five failed PIN logins arm the throttle, and the
// sixth attempt sees 429 with Retry-After regardless of whether the
// credential presented this time is correct -- the throttle is checked
// before the authenticator runs (handlers.go's handleLogin order). The
// "success resets" half needs an injectable clock (Service.now is real-time
// once built via auth.FromConfig, and this test must not sleep through a
// real backoff window) -- that half is already proven at the auth-package
// level by internal/platform/auth/throttle_test.go's
// TestThrottleSuccessResetsFailuresAndDelay and
// internal/platform/auth/handlers_test.go's TestHandleLoginThrottle.
func TestAC5_ThrottleBackoff(t *testing.T) {
	cfg := routeMatrixConfig(t, "off")
	cfg.Auth = config.AuthConfig{
		Modules: map[string][]string{"slideshow": {"pin"}},
		PINs:    []config.NamedHash{{Name: "carol", Hash: bcryptHash(t, "4242")}},
		DataDir: t.TempDir(),
	}
	svc, err := auth.FromConfig(cfg.Auth, knownModules, false)
	if err != nil {
		t.Fatalf("auth.FromConfig: %v", err)
	}
	srv := httptest.NewServer(middleware.Wrap(buildDispatcher(cfg, svc)))
	defer srv.Close()

	for i := 0; i < 5; i++ {
		res := doHost(t, srv, http.MethodPost, "slideshow.example", "/api/auth/login", `{"method":"pin","pin":"wrong"}`)
		res.Body.Close()
		if res.StatusCode != http.StatusUnauthorized {
			t.Fatalf("failure %d: status = %d, want 401", i+1, res.StatusCode)
		}
	}

	// The sixth attempt, even with the CORRECT pin, is throttled.
	res := doHost(t, srv, http.MethodPost, "slideshow.example", "/api/auth/login", `{"method":"pin","pin":"4242"}`)
	defer res.Body.Close()
	if res.StatusCode != http.StatusTooManyRequests {
		body, _ := io.ReadAll(res.Body)
		t.Fatalf("sixth attempt: status = %d, want 429 (body %q)", res.StatusCode, body)
	}
	if res.Header.Get("Retry-After") == "" {
		t.Fatal("sixth attempt: expected a Retry-After header")
	}
}

// ---------------------------------------------------------------------
// AC-5b: API keys.
// ---------------------------------------------------------------------

// TestAC5b_APIKeys proves security-plan.md's AC-5b: a valid API key passes a
// "key" module with zero cookies, the same key is rejected on a non-"key"
// module, an invalid key is a normal 401, and healthz is unaffected on both
// hosts.
func TestAC5b_APIKeys(t *testing.T) {
	const validKey = "test-automation-key-0123456789ab"
	cfg := routeMatrixConfig(t, "off")
	cfg.Auth = config.AuthConfig{
		Modules: map[string][]string{
			"grocery": {"key"},
			"todo":    {"pin"},
		},
		APIKeys: []config.NamedHash{{Name: "automation", Hash: apiKeyHash(validKey)}},
		PINs:    []config.NamedHash{{Name: "carol", Hash: bcryptHash(t, "4242")}},
		DataDir: t.TempDir(),
	}
	svc, err := auth.FromConfig(cfg.Auth, knownModules, false)
	if err != nil {
		t.Fatalf("auth.FromConfig: %v", err)
	}
	srv := httptest.NewServer(middleware.Wrap(buildDispatcher(cfg, svc)))
	defer srv.Close()

	t.Run("valid key on grocery succeeds with zero cookies", func(t *testing.T) {
		res := doHostWithKey(t, srv, "grocery.example", "/api/items", validKey)
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(res.Body)
			t.Fatalf("status = %d, want 200 (body %q)", res.StatusCode, body)
		}
		if len(res.Header.Values("Set-Cookie")) != 0 {
			t.Fatalf("unexpected Set-Cookie for an API-key request: %v", res.Header.Values("Set-Cookie"))
		}
	})

	t.Run("same key on todo (a non-key module) is 401", func(t *testing.T) {
		res := doHostWithKey(t, srv, "todo.example", "/items", validKey)
		defer res.Body.Close()
		if res.StatusCode != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", res.StatusCode)
		}
	})

	t.Run("invalid key on grocery is a normal 401", func(t *testing.T) {
		res := doHostWithKey(t, srv, "grocery.example", "/api/items", "wrong-key")
		defer res.Body.Close()
		if res.StatusCode != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", res.StatusCode)
		}
	})

	t.Run("healthz 200 on both hosts", func(t *testing.T) {
		for _, host := range []string{"grocery.example", "todo.example"} {
			res := doHost(t, srv, http.MethodGet, host, "/healthz", "")
			defer res.Body.Close()
			if res.StatusCode != http.StatusOK {
				t.Fatalf("%s: status = %d, want 200", host, res.StatusCode)
			}
		}
	})
}

// ---------------------------------------------------------------------
// goleak
// ---------------------------------------------------------------------
//
// This package intentionally does NOT add
//
//	func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }
//
// Empirically, doing so fails the entire cmd/server suite -- not just this
// file's tests -- with goroutine leaks that predate T4.6 and have nothing to
// do with the auth gate:
//
//   - internal/slideshow.Conductor.Run (started by slideshow.Build via
//     "go conductor.Run()", conductor.go) has no shutdown/cancellation path:
//     it loops on an unbuffered select forever. Every test in this package
//     that builds a slideshow module (routeMatrixConfig is used throughout
//     this file and origincheck_route_test.go) leaks one such goroutine.
//   - internal/obsidianoid.startVaultWatcher (events.go) starts an fsnotify
//     watcher goroutine per vault with no corresponding Close/Stop, leaking
//     both the watcher goroutine and its underlying kqueue/inotify
//     goroutine.
//
// Both are pre-existing product-code lifecycle gaps, not new leaks from the
// auth gate, and this task (T4.6) is tests-only -- it must not patch
// slideshow or obsidianoid to add shutdown paths. Wrapping the package in
// goleak.VerifyTestMain today would fail on every run of `go test ./cmd/...`,
// for anyone, regardless of whether the auth gate leaks anything, which
// defeats the point of a leak gate. This was verified directly: adding the
// TestMain above and running `go test -race ./cmd/...` reports exactly these
// two goroutine families as leaked, with every other goroutine clean.
//
// Recommendation: add the goleak TestMain once slideshow.Conductor.Run and
// obsidianoid.startVaultWatcher gain a stop mechanism (a separate, non-test
// change); until then this file's tests are individually leak-clean (none of
// them start a goroutine of their own), and the package-wide gate is left
// off rather than filtered around the known leaks.
