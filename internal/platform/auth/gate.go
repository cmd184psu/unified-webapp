// gate.go implements the single platform-owned auth gate (FR-A10, FR-A9,
// FR-A9b, FR-A13) that the dispatcher wraps around every module. Its job is
// to decide, for every request reaching a module, whether that request may
// proceed to the module's own handler (next) unmodified -- never to answer
// the request itself except for the small fixed set of gate-owned routes
// (healthz, mode, login/logout/session/passkey ceremony) and the 401
// fallback.
//
// The gate deliberately never wraps http.ResponseWriter: every branch that
// lets a request through calls next.ServeHTTP with the exact writer the
// gate itself received. This is load-bearing for FR-A13 -- multissh's
// WebSocket upgrade needs http.Hijacker on that writer to survive the auth
// check, and any wrapping/recording writer would hide it. Sliding-refresh
// re-issues the session cookie with a plain http.SetCookie (a header
// mutation) before calling next, never through a wrapped writer.
package auth

import (
	_ "embed"
	"log"
	"net/http"
	"strings"
	"time"

	"cmd184psu/unified-webapp/internal/platform/response"
)

//go:embed login.html
var loginPageHTML []byte

// authGateRoute describes one gate-owned auth route: an exact path (or,
// when prefix is true, a path prefix requiring at least one more character
// after it -- used for DELETE /api/auth/passkeys/{id}), the one HTTP method
// it accepts (any other method on the same path is a 405), whether it
// requires an already-valid session before the handler runs, and the
// handler itself. requiredGrant, when non-empty, is an additional
// precondition beyond a merely-valid session: the session's claims must
// carry that grant (see grantsAllow's grant vocabulary) or the gate answers
// 403 before the handler runs -- used to require a full (LDAP) login for
// passkey management, as opposed to a door-code-only session.
type authGateRoute struct {
	path          string
	prefix        bool
	method        string
	session       bool
	requiredGrant string
	handle        func(*Service, http.ResponseWriter, *http.Request, string)
}

// authGateRoutes is the fixed set of routes the gate itself owns on every
// protected module. The five unauthenticated-allowlist routes let a caller
// establish or inspect a session; the four session-required routes manage
// passkeys and may only run once a valid session cookie carrying the
// "ldap" grant is present -- the gate enforces both preconditions itself
// (see Gate below) so every handler here can assume they hold. Passkey
// *management* (list/register/delete) requires a full LDAP login; passkey
// *login* (begin/finish, above) is unauthenticated on purpose -- it is how
// a session is established in the first place.
var authGateRoutes = []authGateRoute{
	{path: "/api/auth/login", method: http.MethodPost, handle: (*Service).handleLogin},
	{path: "/api/auth/logout", method: http.MethodPost, handle: (*Service).handleLogout},
	{path: "/api/auth/session", method: http.MethodGet, handle: (*Service).handleSession},
	{path: "/api/auth/passkey/login/begin", method: http.MethodPost, handle: (*Service).handlePasskeyLoginBegin},
	{path: "/api/auth/passkey/login/finish", method: http.MethodPost, handle: (*Service).handlePasskeyLoginFinish},
	{path: "/api/auth/passkeys", method: http.MethodGet, session: true, requiredGrant: "ldap", handle: (*Service).handlePasskeysList},
	{path: "/api/auth/passkey/register/begin", method: http.MethodPost, session: true, requiredGrant: "ldap", handle: (*Service).handlePasskeyRegisterBegin},
	{path: "/api/auth/passkey/register/finish", method: http.MethodPost, session: true, requiredGrant: "ldap", handle: (*Service).handlePasskeyRegisterFinish},
	{path: "/api/auth/passkeys/", prefix: true, method: http.MethodDelete, session: true, requiredGrant: "ldap", handle: (*Service).handlePasskeyDelete},
}

// findAuthGateRoute returns the authGateRoute matching path, if any,
// regardless of the request's method -- callers use the returned route's
// method field to decide between running the handler and answering 405.
func findAuthGateRoute(path string) (authGateRoute, bool) {
	for _, rt := range authGateRoutes {
		if rt.prefix {
			if strings.HasPrefix(path, rt.path) && len(path) > len(rt.path) {
				return rt, true
			}
			continue
		}
		if path == rt.path {
			return rt, true
		}
	}
	return authGateRoute{}, false
}

// Gate returns a handler that wraps next with the platform auth gate for
// module. The request order is fixed and evaluated fresh on every request
// against a single Policy snapshot (captured once, below, so a concurrent
// SwapPolicy can never produce an internally-inconsistent decision):
//
//  1. GET /healthz -> 200 {"ok":true}, always, before anything else.
//  2. GET /api/auth/mode -> 200 {"methods":[...]}, always unauthenticated.
//  3. If module is not protected by the current policy, everything else
//     (including the gate's own auth routes) passes through to next
//     unchanged -- an unprotected module never sees a login route.
//  4. If module is protected, the gate-owned auth routes (authGateRoutes)
//     are served here, not by the module; a session-required route 401s
//     before its handler runs if there is no valid session cookie, and a
//     route with a requiredGrant additionally 403s if the session lacks it
//     (passkey management requires a full LDAP login).
//  5. Any other request: module == "admin" checks only the session cookie
//     (grantsAllow, admin_pin grant only -- no API key ever satisfies
//     admin); every other protected module tries checkAPIKey first (no
//     cookie involved, no per-module opt-in), then the uw_session cookie
//     via grantsAllow, with a sliding cookie re-issue when the session is
//     due for refresh.
//  6. Otherwise, 401 -- HTML login-page placeholder for a browser-shaped
//     GET on a non-/api/ path, bare JSON everywhere else (including every
//     /api/ path, regardless of Accept).
func (s *Service) Gate(module string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := s.policy()
		now := s.now()

		// Step 1: healthz, always, before anything else.
		if r.Method == http.MethodGet && r.URL.Path == "/healthz" {
			response.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
			return
		}

		// Step 2: mode, always unauthenticated.
		if r.Method == http.MethodGet && r.URL.Path == "/api/auth/mode" {
			methods := p.OfferedMethods(module)
			if methods == nil {
				methods = []string{}
			}
			response.WriteJSON(w, http.StatusOK, map[string]any{"methods": methods})
			return
		}

		// Step 3: protected := module has a matrix entry, or is "admin".
		// Admin is always protected when routed, with or without a matrix
		// entry, evaluated fresh against this request's snapshot.
		_, hasEntry := p.Modules[module]
		protected := hasEntry || module == "admin"
		if !protected {
			next.ServeHTTP(w, r)
			return
		}

		// Step 4: gate-owned auth routes, only reachable once protected.
		if rt, ok := findAuthGateRoute(r.URL.Path); ok {
			if r.Method != rt.method {
				response.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
				return
			}
			if rt.session {
				claims, valid := s.sessionClaimsFromRequest(r, now)
				if !valid {
					response.WriteError(w, http.StatusUnauthorized, "unauthorized")
					return
				}
				if rt.requiredGrant != "" && !hasGrant(claims.Grants, rt.requiredGrant) {
					log.Printf("event=auth_passkey_denied module=%q reason=%q", module, "requires_ldap")
					response.WriteError(w, http.StatusForbidden, "passkey management requires a full (LDAP) login")
					return
				}
			}
			rt.handle(s, w, r, module)
			return
		}

		// Step 5: everything else -- static assets, SSE, WS upgrades
		// included, with no special-casing. admin is PIN-only, exclusively:
		// no API key ever satisfies it, and only a session carrying the
		// admin_pin grant does (grantsAllow). Every other protected module
		// accepts a bearer API key unconditionally (no per-module opt-in),
		// falling through to the session cookie -- an identity grant
		// (ldap/passkey) or that module's own scoped door-code grant.
		if module == "admin" {
			if claims, ok := s.sessionClaimsFromRequest(r, now); ok && grantsAllow(claims, "admin") {
				if needsRefresh(claims, p.SessionTTL, p.RefreshFraction, now) {
					if tok, err := issueToken(s.key, claims.Subject, claims.Grants, p.SessionTTL, now); err == nil {
						setSessionCookie(w, tok, p.SessionTTL, p.CookieSecure, p.CookieDomain)
					}
				}
				next.ServeHTTP(w, r)
				return
			}
		} else {
			if _, ok := s.checkAPIKey(r); ok {
				next.ServeHTTP(w, r)
				return
			}

			if claims, ok := s.sessionClaimsFromRequest(r, now); ok && grantsAllow(claims, module) {
				if needsRefresh(claims, p.SessionTTL, p.RefreshFraction, now) {
					if tok, err := issueToken(s.key, claims.Subject, claims.Grants, p.SessionTTL, now); err == nil {
						setSessionCookie(w, tok, p.SessionTTL, p.CookieSecure, p.CookieDomain)
					}
				}
				next.ServeHTTP(w, r)
				return
			}
		}

		// Step 6: 401. The HTML heuristic never masks an API status: any
		// /api/ path always gets bare JSON regardless of Accept.
		if r.Method == http.MethodGet && strings.Contains(r.Header.Get("Accept"), "text/html") && !strings.HasPrefix(r.URL.Path, "/api/") {
			s.loginPage(w, r, module)
			return
		}
		response.WriteError(w, http.StatusUnauthorized, "unauthorized")
	})
}

// sessionClaimsFromRequest reads and validates the uw_session cookie from
// r against now, returning its claims. A missing cookie, or one that fails
// parseToken (bad signature, wrong algorithm, expired), is a silent
// (nil, false) -- there is no distinct error path here, only "has a valid
// session" or not.
func (s *Service) sessionClaimsFromRequest(r *http.Request, now time.Time) (*sessionClaims, bool) {
	c, err := r.Cookie(sessionCookieName)
	if err != nil || c.Value == "" {
		return nil, false
	}
	claims, err := parseToken(s.key, c.Value, now)
	if err != nil {
		return nil, false
	}
	return claims, true
}

// loginPage serves the embedded, platform-owned login page (T4.4, FR-A12):
// a single module-agnostic HTML file with inline CSS/JS that fetches
// /api/auth/mode and shows only the relevant sign-in forms. It is always a
// 401 -- the page is an error response that happens to be usable, not a
// distinct success surface.
func (s *Service) loginPage(w http.ResponseWriter, r *http.Request, module string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusUnauthorized)
	w.Write(loginPageHTML)
}

// The handlers backing the gate-owned auth routes (authGateRoutes, above)
// -- login/logout/session, the passkey login ceremony, and passkey
// management -- live in handlers.go (T4.3). Routing, the
// unauthenticated-allowlist vs session-required split, and the session
// precondition are enforced here in Gate and never touched by that file.
