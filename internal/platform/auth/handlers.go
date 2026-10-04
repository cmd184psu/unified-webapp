// handlers.go implements the gate-owned auth routes (FR-A10b, FR-A12b):
// login, logout, session inspection, and the passkey login/registration
// ceremonies. gate.go owns routing, the unauthenticated-allowlist vs
// session-required split, and the session precondition (a session-required
// route never reaches its handler here without an already-valid cookie);
// this file only implements what each route does once it is reached.
//
// Method enforcement (FR-A10b) is the first thing every handler here does,
// before any credential or ceremony work: a login attempt naming a method
// the module does not accept -- and a passkey ceremony route hit on a
// module that does not accept "passkey" -- is rejected 400 before a
// password is checked, an LDAP bind attempted, or a WebAuthn challenge
// issued. The one exception is the "admin" pseudo-module, for which the
// operator PIN path is always acceptable regardless of its matrix entry
// (L6; see policy.go's OfferedMethods and pin.go's adminPINMethod).
//
// The auth event log (FR-A12b) records one line per login attempt --
// success or failure, across every method including passkey -- via the
// standard library log package, matching this codebase's convention
// (cmd/server/main.go's log.Printf/log.Fatalf). It never includes a PIN,
// password, key, token, or request body; only the module, method,
// identity (or "unknown"), and, on failure, a coarse reason class.
package auth

import (
	"encoding/json"
	"errors"
	"log"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"cmd184psu/unified-webapp/internal/platform/response"
)

// Auth event log failure reason classes (FR-A12b). These are the only
// values ever recorded for a failed login attempt.
const (
	reasonBadCredential    = "bad_credential"
	reasonDisallowedMethod = "disallowed_method"
	reasonThrottled        = "throttled"
	reasonAdminPINConfig   = "admin_pin_config"
	reasonPinFileConfig    = "pin_file_config"

	// Passkey ceremony failure classes: each names what actually went wrong
	// so the log line is useful, instead of one bad_credential for all.
	reasonPasskeyBeginFailed        = "passkey_begin_failed"
	reasonPasskeyNoCredentials      = "passkey_no_credentials"
	reasonPasskeyVerificationFailed = "passkey_verification_failed"
	reasonPasskeyOriginNotAllowed   = "passkey_origin_not_allowed"
	reasonPasskeyChallengeExpired   = "passkey_challenge_expired"
	reasonPasskeyNotAuthorized      = "passkey_not_authorized"
	reasonPasskeyFinishFailed       = "passkey_finish_failed"
)

// containsMethod reports whether method appears in list -- used against
// OfferedMethods' output, both here (login-method enforcement) and by the
// passkey ceremony guards below. It has no notion of authorization; that is
// grantsAllow's job (session.go), consulted only by gate.go.
func containsMethod(list []string, method string) bool {
	for _, m := range list {
		if m == method {
			return true
		}
	}
	return false
}

// logLoginAttempt writes the one auth event log line (FR-A12b) for a login
// attempt against module using method, whether it succeeded, its resolved
// identity (blank becomes "unknown"), and -- for a failure -- reason. It
// never receives or logs a PIN, password, key, token, or request body: only
// identity strings (a PIN/API-key entry's configured name, an LDAP
// username, or "admin"/"unknown") and the small fixed vocabulary of module,
// method, and reason values ever reach it.
func logLoginAttempt(ok bool, module, method, identity, reason string) {
	if identity == "" {
		identity = "unknown"
	}
	if ok {
		log.Printf("event=auth_login ok=true module=%q method=%q identity=%q", module, method, identity)
		return
	}
	log.Printf("event=auth_login ok=false module=%q method=%q identity=%q reason=%q", module, method, identity, reason)
}

// authLoginRequest is the POST /api/auth/login body.
type authLoginRequest struct {
	Method   string `json:"method"`
	PIN      string `json:"pin"`
	Username string `json:"username"`
	Password string `json:"password"`
}

// handleLogin backs POST /api/auth/login (FR-A10b, FR-A12b). Order, exactly:
// method enforcement, then the throttle, then the named authenticator, then
// session accumulation and cookie issuance.
func (s *Service) handleLogin(w http.ResponseWriter, r *http.Request, module string) {
	var req authLoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteDecodeError(w, err)
		return
	}

	p := s.policy()
	offered := p.OfferedMethods(module)

	stepUp := false

	// Method enforcement (FR-A10b): before any credential is examined.
	// login.html posts method "pin" on every module including admin, while
	// OfferedMethods("admin") is ["admin_pin"] -- so posted "pin" is
	// acceptable iff the module offers "pin" (a module PinFile) or
	// "admin_pin" (the admin pseudo-module), and on admin it selects the
	// admin-PIN path below exactly like today's special case.
	switch req.Method {
	case "pin":
		if !containsMethod(offered, pinMethod) && !containsMethod(offered, adminPINMethod) {
			logLoginAttempt(false, module, req.Method, "", reasonDisallowedMethod)
			response.WriteError(w, http.StatusBadRequest, "method not accepted for this module")
			return
		}
	case "ldap":
		// Admin never offers ldap as a way in, but an operator already
		// inside on the admin PIN may add an LDAP identity to that same
		// session (step-up): passkeys belong to a person, and the admin PIN
		// has none. The PIN session is the precondition, so LDAP alone still
		// cannot open admin.
		stepUp = module == "admin" && p.LDAP.URL != "" && s.hasAdminPINSession(r, p)
		if !containsMethod(offered, "ldap") && !stepUp {
			logLoginAttempt(false, module, req.Method, "", reasonDisallowedMethod)
			response.WriteError(w, http.StatusBadRequest, "method not accepted for this module")
			return
		}
	default:
		// "passkey" goes through the ceremony routes, "key" is per-request
		// not a login, and anything else is simply unrecognized.
		logLoginAttempt(false, module, req.Method, "", reasonDisallowedMethod)
		response.WriteError(w, http.StatusBadRequest, "unsupported login method")
		return
	}

	if d := s.throttle.delay(); d > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(d.Seconds()))))
		logLoginAttempt(false, module, req.Method, "", reasonThrottled)
		response.WriteError(w, http.StatusTooManyRequests, "too many login attempts")
		return
	}

	var identity, grant string
	var ok bool
	switch req.Method {
	case "pin":
		if module == "admin" {
			pinOK, err := s.checkAdminPIN(req.PIN)
			if err != nil {
				// FR-M2: a misconfigured operator-PIN file (unreadable, or
				// mode too open) fails loudly with the fix in the response
				// -- the operator PIN is the break-glass path, and a
				// generic "invalid credentials" here would strand the
				// operator with no diagnostic. Not a credential failure,
				// so it does not feed the throttle.
				logLoginAttempt(false, module, req.Method, "", reasonAdminPINConfig)
				response.WriteError(w, http.StatusInternalServerError, err.Error())
				return
			}
			if pinOK {
				identity, grant, ok = adminIdentity, adminPINMethod, true
			}
		} else {
			mp := p.Modules[module]
			pinOK, err := checkPINFile(mp.PinFile, req.PIN)
			if err != nil {
				// FR-M2's break-glass pattern, generalized to any module's
				// own door-code file: a config error (missing file, bad
				// perms, unreadable) is a loud 500 naming the fix, logged
				// under its own reason, and never feeds the throttle.
				logLoginAttempt(false, module, req.Method, "", reasonPinFileConfig)
				response.WriteError(w, http.StatusInternalServerError, err.Error())
				return
			}
			if pinOK {
				fp, fpOK := s.currentPINFingerprint(mp.PinFile)
				if !fpOK {
					logLoginAttempt(false, module, req.Method, "", reasonPinFileConfig)
					response.WriteError(w, http.StatusInternalServerError, "unable to read the PIN file")
					return
				}
				grant, ok = pinGrantFP(module, fp), true
			}
		}
	case "ldap":
		client := s.ldapClient(p.LDAP)
		name, err := client.Authenticate(r.Context(), req.Username, req.Password)
		if err == nil {
			identity, grant, ok = name, identityGrant("ldap", module), true
		} else if !errors.Is(err, ErrLDAPAuth) && !errors.Is(err, ErrLDAPForbidden) {
			// Connection-level failure (directory down, unreachable, TLS),
			// not a credential problem. The client still gets the uniform
			// 401 below, but the operator can tell the two apart here.
			log.Printf("event=auth_ldap_error module=%q err=%q", module, err.Error())
		}
	}

	if !ok {
		s.throttle.fail()
		logLoginAttempt(false, module, req.Method, identity, reasonBadCredential)
		response.WriteError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	now := s.now()
	existing := s.existingForLogin(r, module, grant, now)
	sub, grants := accumulate(existing, identity, grant)
	if stepUp {
		// accumulate would replace the admin session ("admin" != the LDAP
		// user) and drop admin_pin, locking the operator out of admin. Keep
		// every existing grant and add ldap under the LDAP identity.
		sub, grants = identity, appendGrant(existing.Grants, grant)
	}
	tok, err := issueClaims(s.key, loginClaims(existing, sub, grants, grant, module, now), p.tokenLifetime(), now)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "unable to issue session")
		return
	}
	setSessionCookie(w, tok, p.tokenLifetime(), p.CookieSecure, p.CookieDomain)
	s.throttle.success()
	logMethod := grant
	if isIdentityGrant(grant) {
		logMethod = grantKind(grant)
	}
	logLoginAttempt(true, module, logMethod, sub, "")
	response.WriteJSON(w, http.StatusOK, map[string]any{"identity": sub, "methods": grants})
}

// handleLogout backs POST /api/auth/logout and always answers 200, whether
// or not a session was present. It signs out of this module only: the
// grant that let the session in here is dropped (this module's door code,
// or admin_pin on admin) and the session is reissued if anything remains,
// so the other modules stay signed in (each module's login, ldap and
// passkey included, belongs to that module alone).
func (s *Service) handleLogout(w http.ResponseWriter, r *http.Request, module string) {
	p := s.policy()
	now := s.now()
	identity := ""
	var remaining *sessionClaims
	if claims, ok := s.sessionClaimsFromRequest(r, now); ok {
		identity = claims.Subject
		remaining = signedOutOf(claims, module)
	}
	if remaining != nil {
		tok, err := issueClaims(s.key, *remaining, p.tokenLifetime(), now)
		if err == nil {
			setSessionCookie(w, tok, p.tokenLifetime(), p.CookieSecure, p.CookieDomain)
		} else {
			clearSessionCookie(w, p.CookieSecure, p.CookieDomain)
		}
	} else {
		clearSessionCookie(w, p.CookieSecure, p.CookieDomain)
	}
	logIdentity := identity
	if logIdentity == "" {
		logIdentity = "unknown"
	}
	log.Printf("event=auth_logout module=%q identity=%q", module, logIdentity)
	response.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleSession backs GET /api/auth/session: 200 with the session's
// identity/methods and idleSeconds (how long until this module signs out
// without further use) when the cookie is valid for this module
// (sessionAllows); 401 otherwise. It is not use, so it renews nothing.
// Deliberately not logged (FR-A12b: "too chatty" for a read-only check).
func (s *Service) handleSession(w http.ResponseWriter, r *http.Request, module string) {
	now := s.now()
	claims, ok := s.sessionClaimsFromRequest(r, now)
	var left time.Duration
	if ok {
		left = s.idleRemaining(claims, module, s.policy(), now)
	}
	if left <= 0 {
		response.WriteError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	response.WriteJSON(w, http.StatusOK, map[string]any{"identity": claims.Subject, "methods": claims.Grants, "idleSeconds": int(left.Seconds())})
}

// handleActivity backs POST /api/auth/activity: the shared page code's
// report that a person is using this module (clicks, typing, scrolling).
// Being a POST it is user activity, so the gate would renew the idle clock
// anyway; this renews it and answers like GET /api/auth/session.
func (s *Service) handleActivity(w http.ResponseWriter, r *http.Request, module string) {
	p := s.policy()
	now := s.now()
	claims, ok := s.sessionClaimsFromRequest(r, now)
	if !ok || !s.sessionAllows(claims, module, p, now) {
		response.WriteError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if moduleNeedsRenewal(claims, module, now) {
		s.renewModule(w, claims, module, p, now)
		renewed := renewedModule(claims, module, now)
		claims = &renewed
	}
	left := s.idleRemaining(claims, module, p, now)
	response.WriteJSON(w, http.StatusOK, map[string]any{"identity": claims.Subject, "methods": claims.Grants, "idleSeconds": int(left.Seconds())})
}

// Plain-language answers to a failed passkey sign-in, one per failure class.
const (
	msgPasskeyNoCredentials = "No passkey is registered for that account. Sign in another way, then register one in Admin > Passkeys."
	msgPasskeyNotAccepted   = "That passkey was not accepted."
	msgPasskeyOrigin        = "Passkeys are not enabled for this address."
	msgPasskeyTimedOut      = "The passkey sign-in timed out. Try again."
)

// passkeyLoginBeginRequest is the POST /api/auth/passkey/login/begin body.
type passkeyLoginBeginRequest struct {
	Username string `json:"username"`
}

// handlePasskeyLoginBegin backs POST /api/auth/passkey/login/begin
// (FR-A10b): rejects 400 before any ceremony work when module does not
// accept "passkey", including the defensive case where the policy snapshot
// has no configured passkey service at all.
func (s *Service) handlePasskeyLoginBegin(w http.ResponseWriter, r *http.Request, module string) {
	p := s.policy()
	if !containsMethod(p.OfferedMethods(module), "passkey") || p.passkeys == nil {
		logLoginAttempt(false, module, "passkey", "", reasonDisallowedMethod)
		response.WriteError(w, http.StatusBadRequest, "method not accepted for this module")
		return
	}

	var req passkeyLoginBeginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteDecodeError(w, err)
		return
	}

	ceremony, err := p.passkeys.BeginPasskeyLogin(req.Username)
	if errors.Is(err, ErrPasskeyNoCredentials) {
		logLoginAttempt(false, module, "passkey", req.Username, reasonPasskeyNoCredentials)
		response.WriteError(w, http.StatusNotFound, msgPasskeyNoCredentials)
		return
	}
	if err != nil {
		log.Printf("event=auth_passkey_error module=%q stage=%q err=%q", module, "login_begin", err.Error())
		logLoginAttempt(false, module, "passkey", "", reasonPasskeyBeginFailed)
		response.WriteError(w, http.StatusInternalServerError, "The server could not start the passkey sign-in. See the server log.")
		return
	}
	writePasskeyCeremony(w, ceremony)
}

// passkeyLoginFinishRequest is the POST /api/auth/passkey/login/finish body.
type passkeyLoginFinishRequest struct {
	ChallengeID string          `json:"challengeId"`
	Credential  json.RawMessage `json:"credential"`
}

// handlePasskeyLoginFinish backs POST /api/auth/passkey/login/finish
// (FR-A10b, FR-A12b): the same disallowed-method guard as begin, then
// verification, then session accumulation and cookie issuance identical to
// a successful handleLogin.
func (s *Service) handlePasskeyLoginFinish(w http.ResponseWriter, r *http.Request, module string) {
	p := s.policy()
	if !containsMethod(p.OfferedMethods(module), "passkey") || p.passkeys == nil {
		logLoginAttempt(false, module, "passkey", "", reasonDisallowedMethod)
		response.WriteError(w, http.StatusBadRequest, "method not accepted for this module")
		return
	}

	var req passkeyLoginFinishRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteDecodeError(w, err)
		return
	}

	if d := s.throttle.delay(); d > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(d.Seconds()))))
		logLoginAttempt(false, module, "passkey", "", reasonThrottled)
		response.WriteError(w, http.StatusTooManyRequests, "too many login attempts")
		return
	}

	identity, err := p.passkeys.FinishPasskeyLogin(r.Context(), req.ChallengeID, req.Credential)
	switch {
	case errors.Is(err, ErrPasskeyChallengeInvalid):
		logLoginAttempt(false, module, "passkey", "", reasonPasskeyChallengeExpired)
		response.WriteError(w, http.StatusBadRequest, msgPasskeyTimedOut)
		return
	case errors.Is(err, ErrPasskeyOriginNotAllowed):
		log.Printf("event=auth_passkey_origin_not_allowed module=%q host=%q", module, r.Host)
		logLoginAttempt(false, module, "passkey", "", reasonPasskeyOriginNotAllowed)
		response.WriteError(w, http.StatusBadRequest, msgPasskeyOrigin)
		return
	case errors.Is(err, ErrPasskeyVerificationFailed), errors.Is(err, ErrPasskeyNoCredentials):
		s.throttle.fail()
		logLoginAttempt(false, module, "passkey", "", reasonPasskeyVerificationFailed)
		response.WriteError(w, http.StatusUnauthorized, msgPasskeyNotAccepted)
		return
	case err != nil:
		log.Printf("event=auth_passkey_error module=%q stage=%q err=%q", module, "login_finish", err.Error())
		logLoginAttempt(false, module, "passkey", "", reasonPasskeyFinishFailed)
		response.WriteError(w, http.StatusInternalServerError, "The server could not finish the passkey sign-in. See the server log.")
		return
	}

	// A valid assertion proves possession of the passkey, not that the
	// person is still allowed in: re-check the directory (fails closed when
	// LDAP is unreachable or not configured).
	if _, err := s.ldapClient(p.LDAP).Authorize(r.Context(), identity); err != nil {
		if !errors.Is(err, ErrLDAPAuth) && !errors.Is(err, ErrLDAPForbidden) {
			log.Printf("event=auth_ldap_error module=%q err=%q", module, err.Error())
		}
		s.throttle.fail()
		logLoginAttempt(false, module, "passkey", identity, reasonPasskeyNotAuthorized)
		response.WriteError(w, http.StatusUnauthorized, msgPasskeyNotAccepted)
		return
	}

	now := s.now()
	existing := s.existingForLogin(r, module, "passkey", now)
	grant := identityGrant("passkey", module)
	sub, methods := accumulate(existing, identity, grant)
	tok, err := issueClaims(s.key, loginClaims(existing, sub, methods, grant, module, now), p.tokenLifetime(), now)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "unable to issue session")
		return
	}
	setSessionCookie(w, tok, p.tokenLifetime(), p.CookieSecure, p.CookieDomain)
	s.throttle.success()
	logLoginAttempt(true, module, "passkey", sub, "")
	response.WriteJSON(w, http.StatusOK, map[string]any{"identity": sub, "methods": methods})
}

// handlePasskeysList backs GET /api/auth/passkeys (session required,
// enforced by Gate before this runs): the caller's enrolled credentials.
func (s *Service) handlePasskeysList(w http.ResponseWriter, r *http.Request, module string) {
	p := s.policy()
	claims, ok := s.sessionClaimsFromRequest(r, s.now())
	if !ok || p.passkeys == nil {
		writePasskeysNotConfigured(w)
		return
	}
	items, err := p.passkeys.ListPasskeys(claims.Subject)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "unable to list passkeys")
		return
	}
	out := make([]passkeyResponse, 0, len(items))
	for _, it := range items {
		out = append(out, passkeyResponseFrom(it))
	}
	response.WriteJSON(w, http.StatusOK, map[string]any{"passkeys": out})
}

// passkeyRegisterBeginRequest is the POST /api/auth/passkey/register/begin
// body.
type passkeyRegisterBeginRequest struct {
	FriendlyName string `json:"friendlyName"`
}

// handlePasskeyRegisterBegin backs POST /api/auth/passkey/register/begin
// (session required, enforced by Gate before this runs).
func (s *Service) handlePasskeyRegisterBegin(w http.ResponseWriter, r *http.Request, module string) {
	p := s.policy()
	claims, ok := s.sessionClaimsFromRequest(r, s.now())
	if !ok || p.passkeys == nil {
		writePasskeysNotConfigured(w)
		return
	}
	var req passkeyRegisterBeginRequest
	_ = json.NewDecoder(r.Body).Decode(&req) // friendlyName is optional; an empty/missing body is fine.

	ceremony, err := p.passkeys.BeginPasskeyRegistration(claims.Subject, req.FriendlyName)
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, "unable to begin passkey registration")
		return
	}
	writePasskeyCeremony(w, ceremony)
}

// passkeyRegisterFinishRequest is the POST /api/auth/passkey/register/finish
// body.
type passkeyRegisterFinishRequest struct {
	ChallengeID  string          `json:"challengeId"`
	FriendlyName string          `json:"friendlyName"`
	Credential   json.RawMessage `json:"credential"`
}

// handlePasskeyRegisterFinish backs POST /api/auth/passkey/register/finish
// (session required, enforced by Gate before this runs).
func (s *Service) handlePasskeyRegisterFinish(w http.ResponseWriter, r *http.Request, module string) {
	p := s.policy()
	claims, ok := s.sessionClaimsFromRequest(r, s.now())
	if !ok || p.passkeys == nil {
		writePasskeysNotConfigured(w)
		return
	}
	var req passkeyRegisterFinishRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteDecodeError(w, err)
		return
	}
	info, err := p.passkeys.FinishPasskeyRegistration(r.Context(), claims.Subject, req.ChallengeID, req.FriendlyName, req.Credential)
	if err != nil {
		if errors.Is(err, ErrPasskeyOriginNotAllowed) {
			response.WriteError(w, http.StatusBadRequest, msgPasskeyOrigin)
			return
		}
		response.WriteError(w, http.StatusBadRequest, "passkey registration failed")
		return
	}
	response.WriteJSON(w, http.StatusOK, passkeyResponseFrom(info))
}

// handlePasskeyDelete backs DELETE /api/auth/passkeys/{id} (session
// required, enforced by Gate before this runs).
func (s *Service) handlePasskeyDelete(w http.ResponseWriter, r *http.Request, module string) {
	p := s.policy()
	claims, ok := s.sessionClaimsFromRequest(r, s.now())
	if !ok || p.passkeys == nil {
		writePasskeysNotConfigured(w)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/auth/passkeys/")
	if err := p.passkeys.DeletePasskey(claims.Subject, id); err != nil {
		response.WriteError(w, http.StatusBadRequest, "unable to delete passkey")
		return
	}
	response.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// passkeyResponse is the redacted, JSON-shaped view of a PasskeyInfo
// returned to a caller -- never the credential's public key or raw ID.
type passkeyResponse struct {
	ID           string `json:"id"`
	FriendlyName string `json:"friendlyName"`
	CreatedAt    int64  `json:"createdAt"`
	LastUsedAt   int64  `json:"lastUsedAt,omitempty"`
}

func passkeyResponseFrom(p PasskeyInfo) passkeyResponse {
	out := passkeyResponse{ID: p.ID, FriendlyName: p.FriendlyName, CreatedAt: p.CreatedAt.Unix()}
	if !p.LastUsedAt.IsZero() {
		out.LastUsedAt = p.LastUsedAt.Unix()
	}
	return out
}

// writePasskeyCeremony writes the go-webauthn ceremony options for a begin
// call, paired with the challenge ID the browser's finish call must carry
// back.
func writePasskeyCeremony(w http.ResponseWriter, c PasskeyCeremony) {
	response.WriteJSON(w, http.StatusOK, map[string]any{"challengeId": c.ChallengeID, "options": c.Options})
}

// hasAdminPINSession reports whether r carries a live session that the admin
// module currently accepts on the strength of the operator PIN.
func (s *Service) hasAdminPINSession(r *http.Request, p *Policy) bool {
	now := s.now()
	claims, ok := s.sessionClaimsFromRequest(r, now)
	return ok && hasGrant(claims.Grants, adminPINMethod) && s.sessionAllows(claims, "admin", p, now)
}
