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
// (L6; see policy.go's EffectiveMethods and pin.go's adminPINMethod).
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
	"log"
	"math"
	"net/http"
	"strconv"
	"strings"

	"cmd184psu/unified-webapp/internal/platform/response"
)

// Auth event log failure reason classes (FR-A12b). These are the only
// values ever recorded for a failed login attempt.
const (
	reasonBadCredential    = "bad_credential"
	reasonDisallowedMethod = "disallowed_method"
	reasonThrottled        = "throttled"
)

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
	accepted := p.EffectiveMethods(module)

	// Method enforcement (FR-A10b): before any credential is examined. The
	// operator PIN path is always acceptable on "admin" regardless of its
	// matrix entry.
	switch req.Method {
	case "pin":
		if module != "admin" && !containsMethod(accepted, "pin") {
			logLoginAttempt(false, module, req.Method, "", reasonDisallowedMethod)
			response.WriteError(w, http.StatusBadRequest, "method not accepted for this module")
			return
		}
	case "ldap":
		if !containsMethod(accepted, "ldap") {
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

	var identity, method string
	switch req.Method {
	case "pin":
		if module == "admin" {
			if ok, err := s.checkAdminPIN(req.PIN); err == nil && ok {
				identity, method = adminIdentity, adminPINMethod
			} else if containsMethod(p.Modules["admin"], "pin") {
				if name, ok2 := s.checkPIN(req.PIN); ok2 {
					identity, method = name, pinMethod
				}
			}
		} else if name, ok := s.checkPIN(req.PIN); ok {
			identity, method = name, pinMethod
		}
	case "ldap":
		client := s.ldapClient(p.LDAP)
		if name, err := client.Authenticate(r.Context(), req.Username, req.Password); err == nil {
			identity, method = name, "ldap"
		}
	}

	if identity == "" {
		s.throttle.fail()
		logLoginAttempt(false, module, req.Method, "", reasonBadCredential)
		response.WriteError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	existing, _ := s.sessionClaimsFromRequest(r, s.now())
	sub, methods := accumulate(existing, identity, method)
	tok, err := issueToken(s.key, sub, methods, p.SessionTTL, s.now())
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "unable to issue session")
		return
	}
	setSessionCookie(w, tok, p.SessionTTL, p.CookieSecure, p.CookieDomain)
	s.throttle.success()
	logLoginAttempt(true, module, method, sub, "")
	response.WriteJSON(w, http.StatusOK, map[string]any{"identity": sub, "methods": methods})
}

// handleLogout backs POST /api/auth/logout: clears the session cookie and
// always answers 200, whether or not a session was present.
func (s *Service) handleLogout(w http.ResponseWriter, r *http.Request, module string) {
	p := s.policy()
	identity := ""
	if claims, ok := s.sessionClaimsFromRequest(r, s.now()); ok {
		identity = claims.Subject
	}
	clearSessionCookie(w, p.CookieSecure, p.CookieDomain)
	logIdentity := identity
	if logIdentity == "" {
		logIdentity = "unknown"
	}
	log.Printf("event=auth_logout identity=%q", logIdentity)
	response.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleSession backs GET /api/auth/session: 200 with the session's
// identity/methods when the cookie is present and valid, 401 otherwise.
// Deliberately not logged (FR-A12b: "too chatty" for a read-only check).
func (s *Service) handleSession(w http.ResponseWriter, r *http.Request, module string) {
	claims, ok := s.sessionClaimsFromRequest(r, s.now())
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	response.WriteJSON(w, http.StatusOK, map[string]any{"identity": claims.Subject, "methods": claims.Methods})
}

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
	if !containsMethod(p.EffectiveMethods(module), "passkey") || p.passkeys == nil {
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
	if err != nil {
		logLoginAttempt(false, module, "passkey", "", reasonBadCredential)
		response.WriteError(w, http.StatusBadRequest, "unable to begin passkey login")
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
	if !containsMethod(p.EffectiveMethods(module), "passkey") || p.passkeys == nil {
		logLoginAttempt(false, module, "passkey", "", reasonDisallowedMethod)
		response.WriteError(w, http.StatusBadRequest, "method not accepted for this module")
		return
	}

	var req passkeyLoginFinishRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteDecodeError(w, err)
		return
	}

	identity, err := p.passkeys.FinishPasskeyLogin(r.Context(), req.ChallengeID, req.Credential)
	if err != nil {
		logLoginAttempt(false, module, "passkey", "", reasonBadCredential)
		response.WriteError(w, http.StatusUnauthorized, "passkey verification failed")
		return
	}

	existing, _ := s.sessionClaimsFromRequest(r, s.now())
	sub, methods := accumulate(existing, identity, "passkey")
	tok, err := issueToken(s.key, sub, methods, p.SessionTTL, s.now())
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "unable to issue session")
		return
	}
	setSessionCookie(w, tok, p.SessionTTL, p.CookieSecure, p.CookieDomain)
	logLoginAttempt(true, module, "passkey", sub, "")
	response.WriteJSON(w, http.StatusOK, map[string]any{"identity": sub, "methods": methods})
}

// handlePasskeysList backs GET /api/auth/passkeys (session required,
// enforced by Gate before this runs): the caller's enrolled credentials.
func (s *Service) handlePasskeysList(w http.ResponseWriter, r *http.Request, module string) {
	p := s.policy()
	claims, ok := s.sessionClaimsFromRequest(r, s.now())
	if !ok || p.passkeys == nil {
		response.WriteError(w, http.StatusBadRequest, "passkeys not available")
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
		response.WriteError(w, http.StatusBadRequest, "passkeys not available")
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
		response.WriteError(w, http.StatusBadRequest, "passkeys not available")
		return
	}
	var req passkeyRegisterFinishRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteDecodeError(w, err)
		return
	}
	info, err := p.passkeys.FinishPasskeyRegistration(r.Context(), claims.Subject, req.ChallengeID, req.FriendlyName, req.Credential)
	if err != nil {
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
		response.WriteError(w, http.StatusBadRequest, "passkeys not available")
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
