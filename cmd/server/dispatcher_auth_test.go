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
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"go.uber.org/goleak"

	"cmd184psu/unified-webapp/internal/platform/auth"
	"cmd184psu/unified-webapp/internal/platform/config"
	"cmd184psu/unified-webapp/internal/platform/middleware"
)

// modulePinFile writes pin to a fresh 0400 file under a new t.TempDir() and
// returns its path -- the fixture every PinFile test below (module door
// codes and AdminPINFile alike) uses in place of the removed auth.pins
// identity-PIN table (a door-code login is now anonymous, not a named
// identity -- see internal/platform/auth/handlers_test.go).
func modulePinFile(t *testing.T, pin string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "module.pin")
	if err := os.WriteFile(path, []byte(pin), 0400); err != nil {
		t.Fatalf("write module pin file: %v", err)
	}
	return path
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
			hh, err := buildModule(module, cfg, nil)
			if err != nil {
				h = unavailableHandler(module, err)
			} else {
				if c, ok := hh.(io.Closer); ok {
					dispatch.closers = append(dispatch.closers, c)
				}
				h = middleware.BodyLimit(limitFor(module, cfg), hh)
			}
			built[module] = h
		}
		dispatch.register(host, h)
	}
	return dispatch
}

// newGateServer builds the production dispatcher for cfg/svc and serves it,
// registering dispatcher cleanup so the module goroutines slideshow and
// obsidianoid start are stopped when the test ends -- the goleak gate in
// TestMain depends on it.
func newGateServer(t *testing.T, cfg *config.Config, svc *auth.Service) *httptest.Server {
	t.Helper()
	dispatch := buildDispatcher(cfg, svc)
	t.Cleanup(dispatch.Close)
	return httptest.NewServer(middleware.Wrap(dispatch))
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

// mustEqualMethods fails t unless got equals want exactly, in order --
// GET /api/auth/mode's methods list is order-stable (OfferedMethods always
// builds it ["ldap", ["passkey"], ["pin"]]).
func mustEqualMethods(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("methods = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("methods = %v, want %v", got, want)
		}
	}
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

	real := newGateServer(t, cfg, svc)
	defer real.Close()
	controlDispatch := buildControlDispatcher(cfg)
	t.Cleanup(controlDispatch.Close)
	control := httptest.NewServer(middleware.Wrap(controlDispatch))
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
	sharedPinFile := modulePinFile(t, "4242")
	cfg.Auth = config.AuthConfig{
		Modules: map[string]config.ModuleAuthConfig{
			"grocery":     {PinFile: sharedPinFile},
			"todo":        {PinFile: sharedPinFile},
			"slideshow":   {PinFile: sharedPinFile},
			"menuserver":  {PinFile: sharedPinFile},
			"obsidianoid": {PinFile: sharedPinFile},
			"multissh":    {PinFile: sharedPinFile},
		},
		LDAP:    config.LDAPConfig{URL: "ldap://fake", BaseDN: "dc=example,dc=com"},
		DataDir: t.TempDir(),
	}
	svc, err := auth.FromConfig(cfg.Auth, knownModules, false)
	if err != nil {
		t.Fatalf("auth.FromConfig: %v", err)
	}
	srv := newGateServer(t, cfg, svc)
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
			mustEqualMethods(t, mode.Methods, []string{"ldap", "pin"})
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
	slideshowPinFile := modulePinFile(t, "4242")
	cfg.Auth = config.AuthConfig{
		Modules: map[string]config.ModuleAuthConfig{
			"slideshow": {PinFile: slideshowPinFile},
			"multissh":  {},
		},
		LDAP:    config.LDAPConfig{URL: "ldap://fake", BaseDN: "dc=example,dc=com"},
		DataDir: t.TempDir(),
	}
	svc, err := auth.FromConfig(cfg.Auth, knownModules, false)
	if err != nil {
		t.Fatalf("auth.FromConfig: %v", err)
	}
	srv := newGateServer(t, cfg, svc)
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
	Grants []string `json:"grants"`
	jwt.RegisteredClaims
}

// signSessionToken mints an HS256 session token with the given key, grants,
// and issued-at/expiry, matching auth's session.go token shape.
func signSessionToken(t *testing.T, key []byte, subject string, grants []string, iat, exp time.Time) string {
	t.Helper()
	claims := testSessionClaims{
		Grants: grants,
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
		Modules: map[string]config.ModuleAuthConfig{"slideshow": {PinFile: modulePinFile(t, "4242")}},
		LDAP:    config.LDAPConfig{URL: "ldap://fake", BaseDN: "dc=example,dc=com"},
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

	srv := newGateServer(t, cfg, svc)
	defer srv.Close()

	now := time.Now()

	t.Run("expired token", func(t *testing.T) {
		tok := signSessionToken(t, realKey, "carol", []string{"pin:slideshow"}, now.Add(-2*time.Hour), now.Add(-time.Hour))
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
		tok := signSessionToken(t, otherKey, "carol", []string{"pin:slideshow"}, now.Add(-time.Minute), now.Add(time.Hour))
		res := doHostWithCookie(t, srv, http.MethodGet, "slideshow.example", "/", &http.Cookie{Name: "uw_session", Value: tok})
		defer res.Body.Close()
		if res.StatusCode != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", res.StatusCode)
		}
	})

	t.Run("near-expiry refreshes the cookie", func(t *testing.T) {
		iat := now.Add(-40 * time.Minute) // > 50% of the 1h TTL has elapsed.
		exp := iat.Add(time.Hour)         // still valid: 20 minutes remain.
		tok := signSessionToken(t, realKey, "carol", []string{"pin:slideshow"}, iat, exp)
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
		Modules: map[string]config.ModuleAuthConfig{"slideshow": {PinFile: modulePinFile(t, "4242")}},
		LDAP:    config.LDAPConfig{URL: "ldap://fake", BaseDN: "dc=example,dc=com"},
		DataDir: t.TempDir(),
	}
	svc, err := auth.FromConfig(cfg.Auth, knownModules, false)
	if err != nil {
		t.Fatalf("auth.FromConfig: %v", err)
	}
	srv := newGateServer(t, cfg, svc)
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

// TestAC5b_APIKeys proves security-plan.md's AC-5b under the two-state model
// (L6): a valid API key is a bearer credential that passes ANY protected
// non-admin module unconditionally -- no per-module "key" opt-in exists any
// more -- with zero cookies; an invalid key is a normal 401; the key never
// authorizes admin (PIN-only, exclusively); and healthz is unaffected
// everywhere.
func TestAC5b_APIKeys(t *testing.T) {
	const validKey = "test-automation-key-0123456789ab"
	cfg := routeMatrixConfig(t, "off")
	cfg.Routing["admin.example"] = "admin"
	cfg.Admin.StaticDir = mkStaticDir(t)
	cfg.Auth = config.AuthConfig{
		Modules: map[string]config.ModuleAuthConfig{
			"grocery": {},
			"todo":    {},
		},
		LDAP:         config.LDAPConfig{URL: "ldap://fake", BaseDN: "dc=example,dc=com"},
		APIKeys:      []config.NamedHash{{Name: "automation", Hash: apiKeyHash(validKey)}},
		AdminPINFile: modulePinFile(t, "9999"),
		DataDir:      t.TempDir(),
	}
	svc, err := auth.FromConfig(cfg.Auth, knownModules, true)
	if err != nil {
		t.Fatalf("auth.FromConfig: %v", err)
	}
	srv := newGateServer(t, cfg, svc)
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

	t.Run("same key on todo (no per-module opt-in needed) also succeeds", func(t *testing.T) {
		res := doHostWithKey(t, srv, "todo.example", "/items", validKey)
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(res.Body)
			t.Fatalf("status = %d, want 200 (body %q)", res.StatusCode, body)
		}
	})

	t.Run("the key never authorizes admin", func(t *testing.T) {
		res := doHostWithKey(t, srv, "admin.example", "/api/config/auth", validKey)
		defer res.Body.Close()
		if res.StatusCode != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401 (admin is PIN-only, exclusively)", res.StatusCode)
		}
	})

	t.Run("invalid key on grocery is a normal 401", func(t *testing.T) {
		res := doHostWithKey(t, srv, "grocery.example", "/api/items", "wrong-key")
		defer res.Body.Close()
		if res.StatusCode != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", res.StatusCode)
		}
	})

	t.Run("healthz 200 everywhere", func(t *testing.T) {
		for _, host := range []string{"grocery.example", "todo.example", "admin.example"} {
			res := doHost(t, srv, http.MethodGet, host, "/healthz", "")
			defer res.Body.Close()
			if res.StatusCode != http.StatusOK {
				t.Fatalf("%s: status = %d, want 200", host, res.StatusCode)
			}
		}
	})
}

// ---------------------------------------------------------------------
// T5.2: admin break-glass enforcement, end-to-end through the dispatcher.
// ---------------------------------------------------------------------
//
// This covers only the dispatcher-level slice of security-plan.md's T5.2:
// one full round trip (unauthenticated admin route -> 401, operator-PIN
// login via POST /api/auth/login, cookie opens the admin shell) per
// "empty assignment matrix" encoding. Everything needing an unexported
// seam or an injected clock -- a live matrix swap, an edited PIN file with
// no swap, the throttle, mode's admin_pin floor surviving a matrix
// save -- is proven instead in internal/platform/auth/gate_test.go and
// handlers_test.go; see the comments there for the full split, including
// the one T5.2 item (a 0644 PIN file's login error) whose actual behavior
// diverges from the plan's spec text.

// TestT5_2_AdminBreakGlassBothEmptyMatrixEncodings proves, for both
// encodings of an empty admin assignment matrix (an explicit "admin": []
// entry, and no "admin" key in auth.modules at all), that: an
// unauthenticated request to an admin data route 401s; the operator PIN
// from a 0400 admin_pin_file logs in as identity "admin" via method
// "admin_pin"; and the resulting session cookie opens the admin shell.
func TestT5_2_AdminBreakGlassBothEmptyMatrixEncodings(t *testing.T) {
	encodings := map[string]map[string]config.ModuleAuthConfig{
		"explicit admin: []": {"admin": {}},
		"no admin key":       {},
	}

	for name, modules := range encodings {
		t.Run(name, func(t *testing.T) {
			cfg := routeMatrixConfig(t, "off")
			cfg.Routing["admin.example"] = "admin"
			cfg.Admin.StaticDir = mkStaticDir(t)
			cfg.Auth = config.AuthConfig{
				Modules:      modules,
				AdminPINFile: modulePinFile(t, "9999"),
				DataDir:      t.TempDir(),
			}
			svc, err := auth.FromConfig(cfg.Auth, knownModules, true)
			if err != nil {
				t.Fatalf("auth.FromConfig: %v", err)
			}
			srv := newGateServer(t, cfg, svc)
			defer srv.Close()

			// Unauthenticated admin data route -> 401.
			res := doHost(t, srv, http.MethodGet, "admin.example", "/api/config/auth", "")
			body, _ := io.ReadAll(res.Body)
			res.Body.Close()
			if res.StatusCode != http.StatusUnauthorized {
				t.Fatalf("unauthenticated admin route: status = %d, want 401 (body %q)", res.StatusCode, body)
			}

			// Operator PIN login via POST /api/auth/login succeeds.
			loginRes := doHost(t, srv, http.MethodPost, "admin.example", "/api/auth/login", `{"method":"pin","pin":"9999"}`)
			loginBodyBytes, _ := io.ReadAll(loginRes.Body)
			loginRes.Body.Close()
			if loginRes.StatusCode != http.StatusOK {
				t.Fatalf("admin operator PIN login: status = %d, want 200 (body %q)", loginRes.StatusCode, loginBodyBytes)
			}
			var decoded struct {
				Identity string   `json:"identity"`
				Methods  []string `json:"methods"`
			}
			if err := json.Unmarshal(loginBodyBytes, &decoded); err != nil {
				t.Fatalf("decode login body: %v", err)
			}
			if decoded.Identity != "admin" {
				t.Fatalf("identity = %q, want admin", decoded.Identity)
			}
			if len(decoded.Methods) != 1 || decoded.Methods[0] != "admin_pin" {
				t.Fatalf("methods = %v, want [admin_pin]", decoded.Methods)
			}
			cookie, ok := firstSetCookie(loginRes)
			if !ok {
				t.Fatal("expected a Set-Cookie after a successful admin PIN login")
			}

			// The cookie opens the admin shell.
			openRes := doHostWithCookie(t, srv, http.MethodGet, "admin.example", "/", cookie)
			defer openRes.Body.Close()
			if openRes.StatusCode != http.StatusOK {
				t.Fatalf("admin shell with the session cookie: status = %d, want 200", openRes.StatusCode)
			}
		})
	}
}

// ---------------------------------------------------------------------
// T5.6: restart equivalence + admin acceptance (AC-12/13/14).
// ---------------------------------------------------------------------
//
// TestT5_6_RestartEquivalence is the end-to-end acceptance script for
// security-plan.md's T5.6: one dispatcher built from a REAL temp config
// file (loaded via config.Load, exactly as cmd/server/main.go boots, so
// cfg.ConfigPath() is set and admin's live-apply has somewhere to splice)
// drives a scripted admin session -- add a PIN, generate a key, protect two
// previously-open modules, prove the protection (and its later removal) is
// live with no rebuild (AC-13) -- then the *written file* is loaded fresh
// and a brand-new dispatcher built from it (simulating a restart) is
// proven to behave identically (AC-14), including that a session cookie
// minted by the OLD dispatcher still opens the module on the NEW one (the
// FR-A2 stateless-restart property: sessions live in a JWT signed with a
// key read from auth.data_dir, not in any in-memory state that a restart
// would lose). Finally, the config file's bytes are compared before and
// after the whole live-apply sequence to confirm every byte outside the
// top-level "auth" member survived untouched (AC-12's config-is-truth
// property, proven the same way internal/admin/apply_test.go proves it for
// a single splice: this test's sequence performs five).

// doHostWithCookieAndBody issues method/path with body against host on srv,
// carrying cookie when non-nil. Neither doHost (no cookie) nor
// doHostWithCookie (no body) covers the admin API calls below, which need
// both at once -- a POST/PUT to an admin route only ever succeeds past the
// gate with a session cookie, and every admin mutation here carries a JSON
// body.
func doHostWithCookieAndBody(t *testing.T, srv *httptest.Server, method, host, path, body string, cookie *http.Cookie) *http.Response {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, srv.URL+path, reader)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Host = host
	req.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		req.AddCookie(cookie)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	return res
}

// t56ConfigTemplate is a distinctively-formatted config file (odd spacing
// around one colon, a top-level member ("custom_marker") that config.Config
// has no field for at all, and a "trailing_section" after "auth" with a
// nested array) so the byte-fidelity assertion below is meaningful --
// exactly the fixtureWithAuth technique internal/admin/apply_test.go uses,
// scaled up to a config that can actually boot three real modules
// (admin/slideshow/grocery). It is a raw string literal, not
// json.Marshal'd, so its exact bytes (spacing, key order, indentation) are
// known and asserted on directly. %[N]q placeholders take the temp-dir
// paths the modules and auth need to build.
const t56ConfigTemplate = `{
  "custom_marker"  :  "leave-me-untouched",
  "port": 9999,
  "host_routing": {
    "admin.example": "admin",
    "slideshow.example": "slideshow",
    "grocery.example": "grocery"
  },
  "admin": {
    "static_dir": %[1]q
  },
  "slideshow": {
    "static_dir": %[2]q,
    "image_dir": %[3]q
  },
  "grocery": {
    "static_dir": %[4]q,
    "data_file": %[5]q
  },
  "auth": {
    "admin_pin_file": %[6]q,
    "data_dir": %[7]q,
    "ldap": {
      "url": "ldap://fake",
      "base_dn": "dc=example,dc=com"
    }
  },
  "trailing_section": {
    "z": "keep-me-too",
    "list": [
      1,
      2,
      3
    ]
  }
}
`

// t56AuthValueRange locates the byte range of the top-level "auth" member's
// *value* in data, via the same json.Decoder Token()/InputOffset() walk
// internal/admin/apply.go's locateAuthMember uses to splice a config file
// leaving every other byte untouched. That function is unexported to
// package admin and cannot be called from here, so the walk is duplicated
// in this smaller, read-only form (this test never needs the
// insert-when-absent path, since its fixture always seeds "auth"
// explicitly) -- see apply.go's comments for why this Token()/Decode(
// &json.RawMessage{}) technique, rather than a map[string]json.RawMessage
// re-marshal, is the only one that recovers an exact byte range without
// reformatting anything else in the file.
func t56AuthValueRange(t *testing.T, data []byte) (start, end int64, found bool) {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(data))
	tok, err := dec.Token()
	if err != nil {
		t.Fatalf("t56AuthValueRange: reading opening token: %v", err)
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '{' {
		t.Fatalf("t56AuthValueRange: config root is not a JSON object")
	}
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			t.Fatalf("t56AuthValueRange: reading a key: %v", err)
		}
		key, ok := keyTok.(string)
		if !ok {
			t.Fatalf("t56AuthValueRange: config object has a non-string key")
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			t.Fatalf("t56AuthValueRange: decoding member %q: %v", key, err)
		}
		valueEnd := dec.InputOffset()
		valueStart := valueEnd - int64(len(raw))
		if key == "auth" {
			return valueStart, valueEnd, true
		}
	}
	return 0, 0, false
}

func TestT5_6_RestartEquivalence(t *testing.T) {
	const adminPIN = "9999"
	const alicePIN = "1111"
	const apiKeyName = "ci"

	adminPINPath := modulePinFile(t, adminPIN)
	slideshowPinPath := modulePinFile(t, alicePIN)
	authDataDir := t.TempDir()
	adminStaticDir := mkStaticDir(t)
	slideshowStaticDir := mkStaticDir(t)
	slideshowImageDir := t.TempDir()
	groceryStaticDir := mkStaticDir(t)
	groceryDataFile := filepath.Join(t.TempDir(), "grocery.json")

	configPath := filepath.Join(t.TempDir(), "config.json")
	initialContent := fmt.Sprintf(t56ConfigTemplate,
		adminStaticDir, slideshowStaticDir, slideshowImageDir,
		groceryStaticDir, groceryDataFile,
		adminPINPath, authDataDir,
	)
	if err := os.WriteFile(configPath, []byte(initialContent), 0o600); err != nil {
		t.Fatalf("writing initial config: %v", err)
	}

	originalBytes, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("reading initial config: %v", err)
	}

	cfgInitial, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("config.Load(initial): %v", err)
	}
	if cfgInitial.ConfigPath() != configPath {
		t.Fatalf("cfgInitial.ConfigPath() = %q, want %q", cfgInitial.ConfigPath(), configPath)
	}

	adminRouted := adminIsRouted(cfgInitial.Routing)
	svc, err := auth.FromConfig(cfgInitial.Auth, knownModules, adminRouted)
	if err != nil {
		t.Fatalf("auth.FromConfig(initial): %v", err)
	}
	srv := newGateServer(t, cfgInitial, svc)
	defer srv.Close()

	// --- Step 1: operator-PIN login on the admin host -> cookie. ---

	loginRes := doHost(t, srv, http.MethodPost, "admin.example", "/api/auth/login", `{"method":"pin","pin":"`+adminPIN+`"}`)
	loginBody, _ := io.ReadAll(loginRes.Body)
	loginRes.Body.Close()
	if loginRes.StatusCode != http.StatusOK {
		t.Fatalf("admin operator PIN login: status = %d, want 200 (body %q)", loginRes.StatusCode, loginBody)
	}
	adminCookie, ok := firstSetCookie(loginRes)
	if !ok {
		t.Fatal("admin operator PIN login: expected a Set-Cookie")
	}

	// --- Step 2: via the admin API, generate a key. The slideshow door code
	// is a pin_file (slideshowPinPath, written directly to disk above) named
	// in the module matrix below, not a value added through the admin API --
	// auth.pins (a named-identity PIN table with its own POST route) was
	// removed under the two-state model; a module's door code is now purely
	// a config-file (or, here, live-apply) pin_file reference.

	keyRes := doHostWithCookieAndBody(t, srv, http.MethodPost, "admin.example", "/api/keys", `{"name":"`+apiKeyName+`"}`, adminCookie)
	keyBody, _ := io.ReadAll(keyRes.Body)
	keyRes.Body.Close()
	if keyRes.StatusCode != http.StatusOK {
		t.Fatalf("POST /api/keys: status = %d, want 200 (body %q)", keyRes.StatusCode, keyBody)
	}
	var keyDecoded struct {
		Name string `json:"name"`
		Key  string `json:"key"`
	}
	if err := json.Unmarshal(keyBody, &keyDecoded); err != nil {
		t.Fatalf("decode POST /api/keys body: %v", err)
	}
	if keyDecoded.Key == "" {
		t.Fatalf("POST /api/keys: response carried no plaintext key (body %q)", keyBody)
	}
	capturedKey := keyDecoded.Key

	// --- AC-13, before the matrix PUT: both modules are still open. ---

	t.Run("before matrix PUT both modules are open", func(t *testing.T) {
		for _, host := range []string{"slideshow.example", "grocery.example"} {
			res := doHost(t, srv, http.MethodGet, host, "/", "")
			defer res.Body.Close()
			if res.StatusCode != http.StatusOK {
				t.Fatalf("%s: status = %d, want 200 (still unprotected)", host, res.StatusCode)
			}
		}
	})

	// --- Step 2 (cont'd): PUT the module protection matrix. slideshow gets
	// its own pin_file (a door code); grocery gets no pin_file at all --
	// under the two-state model an empty ModuleAuthConfig{} is still fully
	// protected, and the captured API key (a bearer credential, orthogonal
	// to any per-module opt-in) authorizes it unconditionally. ---

	protectMatrixJSON, err := json.Marshal(map[string]config.ModuleAuthConfig{
		"slideshow": {PinFile: slideshowPinPath},
		"grocery":   {},
	})
	if err != nil {
		t.Fatalf("marshal protect matrix: %v", err)
	}

	matrixRes := doHostWithCookieAndBody(t, srv, http.MethodPut, "admin.example", "/api/config/modules", string(protectMatrixJSON), adminCookie)
	matrixBody, _ := io.ReadAll(matrixRes.Body)
	matrixRes.Body.Close()
	if matrixRes.StatusCode != http.StatusOK {
		t.Fatalf("PUT /api/config/modules (protect): status = %d, want 200 (body %q)", matrixRes.StatusCode, matrixBody)
	}

	// --- AC-13, after the matrix PUT, no rebuild: both modules now 401. ---

	var slideshowCookie *http.Cookie
	t.Run("after matrix PUT both modules 401 with no rebuild", func(t *testing.T) {
		for _, host := range []string{"slideshow.example", "grocery.example"} {
			res := doHost(t, srv, http.MethodGet, host, "/", "")
			defer res.Body.Close()
			if res.StatusCode != http.StatusUnauthorized {
				t.Fatalf("%s: status = %d, want 401 (now protected, live, no rebuild)", host, res.StatusCode)
			}
		}

		// The PIN login opens slideshow.
		res := doHost(t, srv, http.MethodPost, "slideshow.example", "/api/auth/login", `{"method":"pin","pin":"`+alicePIN+`"}`)
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("pin login on slideshow: status = %d, want 200 (body %q)", res.StatusCode, body)
		}
		cookie, ok := firstSetCookie(res)
		if !ok {
			t.Fatal("pin login on slideshow: expected a Set-Cookie")
		}
		slideshowCookie = cookie

		openRes := doHostWithCookie(t, srv, http.MethodGet, "slideshow.example", "/", cookie)
		defer openRes.Body.Close()
		if openRes.StatusCode != http.StatusOK {
			t.Fatalf("slideshow with the new pin cookie: status = %d, want 200", openRes.StatusCode)
		}

		// The captured key opens grocery.
		keyOpenRes := doHostWithKey(t, srv, "grocery.example", "/", capturedKey)
		defer keyOpenRes.Body.Close()
		if keyOpenRes.StatusCode != http.StatusOK {
			t.Fatalf("grocery with the captured key: status = %d, want 200", keyOpenRes.StatusCode)
		}
	})

	// --- AC-13, live removal: PUT an empty matrix, next request passes. ---

	t.Run("removing the matrix is also live", func(t *testing.T) {
		removeRes := doHostWithCookieAndBody(t, srv, http.MethodPut, "admin.example", "/api/config/modules", `{}`, adminCookie)
		removeBody, _ := io.ReadAll(removeRes.Body)
		removeRes.Body.Close()
		if removeRes.StatusCode != http.StatusOK {
			t.Fatalf("PUT /api/config/modules (remove): status = %d, want 200 (body %q)", removeRes.StatusCode, removeBody)
		}

		for _, host := range []string{"slideshow.example", "grocery.example"} {
			res := doHost(t, srv, http.MethodGet, host, "/", "")
			defer res.Body.Close()
			if res.StatusCode != http.StatusOK {
				t.Fatalf("%s: status = %d, want 200 (removal is live too)", host, res.StatusCode)
			}
		}
	})

	// --- Re-add protection for the restart phase. ---

	restoreRes := doHostWithCookieAndBody(t, srv, http.MethodPut, "admin.example", "/api/config/modules", string(protectMatrixJSON), adminCookie)
	restoreBody, _ := io.ReadAll(restoreRes.Body)
	restoreRes.Body.Close()
	if restoreRes.StatusCode != http.StatusOK {
		t.Fatalf("PUT /api/config/modules (restore): status = %d, want 200 (body %q)", restoreRes.StatusCode, restoreBody)
	}

	finalBytes, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("reading final config: %v", err)
	}

	// --- AC-14: restart equivalence. Reload the WRITTEN file fresh and
	// build a brand-new dispatcher, simulating a restart. ---

	cfgRestarted, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("config.Load(restarted): %v", err)
	}
	adminRoutedRestarted := adminIsRouted(cfgRestarted.Routing)
	svcRestarted, err := auth.FromConfig(cfgRestarted.Auth, knownModules, adminRoutedRestarted)
	if err != nil {
		t.Fatalf("auth.FromConfig(restarted): %v", err)
	}
	srvRestarted := newGateServer(t, cfgRestarted, svcRestarted)
	defer srvRestarted.Close()

	t.Run("restart: identical behavior matrix", func(t *testing.T) {
		for _, host := range []string{"slideshow.example", "grocery.example"} {
			res := doHost(t, srvRestarted, http.MethodGet, host, "/", "")
			defer res.Body.Close()
			if res.StatusCode != http.StatusUnauthorized {
				t.Fatalf("%s (restarted): status = %d, want 401", host, res.StatusCode)
			}
		}

		pinLoginRes := doHost(t, srvRestarted, http.MethodPost, "slideshow.example", "/api/auth/login", `{"method":"pin","pin":"`+alicePIN+`"}`)
		pinLoginBody, _ := io.ReadAll(pinLoginRes.Body)
		pinLoginRes.Body.Close()
		if pinLoginRes.StatusCode != http.StatusOK {
			t.Fatalf("pin login on slideshow (restarted): status = %d, want 200 (body %q)", pinLoginRes.StatusCode, pinLoginBody)
		}
		newSlideshowCookie, ok := firstSetCookie(pinLoginRes)
		if !ok {
			t.Fatal("pin login on slideshow (restarted): expected a Set-Cookie")
		}
		openRes := doHostWithCookie(t, srvRestarted, http.MethodGet, "slideshow.example", "/", newSlideshowCookie)
		defer openRes.Body.Close()
		if openRes.StatusCode != http.StatusOK {
			t.Fatalf("slideshow with the restarted pin cookie: status = %d, want 200", openRes.StatusCode)
		}

		keyRes := doHostWithKey(t, srvRestarted, "grocery.example", "/", capturedKey)
		defer keyRes.Body.Close()
		if keyRes.StatusCode != http.StatusOK {
			t.Fatalf("grocery with the captured key (restarted): status = %d, want 200", keyRes.StatusCode)
		}

		adminLoginRes := doHost(t, srvRestarted, http.MethodPost, "admin.example", "/api/auth/login", `{"method":"pin","pin":"`+adminPIN+`"}`)
		adminLoginBody, _ := io.ReadAll(adminLoginRes.Body)
		adminLoginRes.Body.Close()
		if adminLoginRes.StatusCode != http.StatusOK {
			t.Fatalf("admin operator PIN login (restarted): status = %d, want 200 (body %q)", adminLoginRes.StatusCode, adminLoginBody)
		}

		// FR-A2: a session cookie minted by the OLD dispatcher still opens
		// the module on the NEW one -- the session's HMAC key lives on disk
		// under auth.data_dir, unchanged across the restart, not in any
		// in-memory state the new *auth.Service starts fresh.
		oldCookieRes := doHostWithCookie(t, srvRestarted, http.MethodGet, "slideshow.example", "/", slideshowCookie)
		defer oldCookieRes.Body.Close()
		if oldCookieRes.StatusCode != http.StatusOK {
			t.Fatalf("slideshow with the OLD dispatcher's session cookie, on the NEW dispatcher: status = %d, want 200", oldCookieRes.StatusCode)
		}
	})

	// --- Byte fidelity: every byte outside the top-level "auth" member is
	// unchanged across the whole five-splice sequence (AC-12/14). ---

	t.Run("byte fidelity outside the auth member", func(t *testing.T) {
		origStart, origEnd, found := t56AuthValueRange(t, originalBytes)
		if !found {
			t.Fatal("t56AuthValueRange(original): no top-level \"auth\" member found")
		}
		finalStart, finalEnd, found := t56AuthValueRange(t, finalBytes)
		if !found {
			t.Fatal("t56AuthValueRange(final): no top-level \"auth\" member found")
		}

		// Nothing before "auth"'s value ever changes length (every splice
		// rewrites only the value itself), so the value's start offset must
		// be identical in the original and final files.
		if origStart != finalStart {
			t.Fatalf("auth member's value start offset moved: %d -> %d (bytes before it should never change)", origStart, finalStart)
		}
		if !bytes.Equal(originalBytes[:origStart], finalBytes[:finalStart]) {
			t.Errorf("bytes before the auth member's value changed:\n got: %q\nwant: %q", finalBytes[:finalStart], originalBytes[:origStart])
		}

		origSuffixLen := int64(len(originalBytes)) - origEnd
		finalSuffixLen := int64(len(finalBytes)) - finalEnd
		if origSuffixLen != finalSuffixLen {
			t.Fatalf("suffix length after the auth member's value changed: %d -> %d", origSuffixLen, finalSuffixLen)
		}
		if !bytes.Equal(originalBytes[origEnd:], finalBytes[finalEnd:]) {
			t.Errorf("bytes after the auth member's value changed:\n got: %q\nwant: %q", finalBytes[finalEnd:], originalBytes[origEnd:])
		}

		// The non-auth sections also parse identically via config.Load.
		initialNoAuth := *cfgInitial
		initialNoAuth.Auth = config.AuthConfig{}
		restartedNoAuth := *cfgRestarted
		restartedNoAuth.Auth = config.AuthConfig{}
		wantJSON, err := json.Marshal(initialNoAuth)
		if err != nil {
			t.Fatalf("marshal initialNoAuth: %v", err)
		}
		gotJSON, err := json.Marshal(restartedNoAuth)
		if err != nil {
			t.Fatalf("marshal restartedNoAuth: %v", err)
		}
		if !bytes.Equal(gotJSON, wantJSON) {
			t.Errorf("non-auth config sections differ after config.Load:\n got: %s\nwant: %s", gotJSON, wantJSON)
		}
	})
}

// ---------------------------------------------------------------------
// goleak
// ---------------------------------------------------------------------
//
// Package-wide leak gate. The two module goroutine families this package
// starts -- slideshow's conductor tick loop and obsidianoid's per-vault
// fsnotify watchers -- now have stop paths (their Build handlers implement
// io.Closer), and every test that builds modules does so through
// newGateServer, newOriginCheckedServer, or the hoisted
// buildControlDispatcher call, all of which register Dispatcher.Close as
// test cleanup. A module goroutine that outlives its test is therefore a
// bug, and this gate keeps it that way.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
