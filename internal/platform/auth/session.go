package auth

import (
	"fmt"
	"net/http"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"cmd184psu/unified-webapp/internal/platform/config"
)

// sessionCookieName is the name of the cookie carrying the session JWT.
const sessionCookieName = "uw_session"

// Default session lifetime and sliding-refresh fraction, applied when the
// corresponding config.SessionConfig field is left at its zero value.
const (
	defaultSessionTTL      = 720 * time.Hour
	defaultRefreshFraction = 0.5
)

// sessionClaims is the JWT claim set for a unified-webapp session token.
// Grants records the session's scoped grant strings: "ldap", "passkey", and
// "admin_pin" are identity-wide (they reach every protected non-admin
// module, or admin itself for admin_pin); "pin:<module>" is a module-scoped
// door-code grant reaching that module alone (see pinGrant/grantsAllow).
//
// The claim's JSON key is deliberately "grants", not the pre-cutover
// "methods" (PLAN-auth-two-state.md, Decision B1): every session token
// signed before this change carries a "methods" key that this struct does
// not recognize, so it decodes to a nil Grants slice and grantsAllow denies
// it outright. That forces exactly one re-login per browser at deploy, with
// no runtime token-version checking code required.
type sessionClaims struct {
	Grants []string `json:"grants"`
	jwt.RegisteredClaims
}

// sessionTTL returns the configured session TTL, or the default (720h)
// when unset.
func sessionTTL(cfg config.SessionConfig) time.Duration {
	if cfg.TTLHours == 0 {
		return defaultSessionTTL
	}
	return time.Duration(cfg.TTLHours) * time.Hour
}

// refreshFraction returns the configured sliding-refresh fraction, or the
// default (0.5) when unset.
func refreshFraction(cfg config.SessionConfig) float64 {
	if cfg.RefreshAfterFraction == 0 {
		return defaultRefreshFraction
	}
	return cfg.RefreshAfterFraction
}

// issueToken creates and signs an HS256 session JWT for sub, recording
// grants, with iat=now and exp=now+ttl.
func issueToken(key []byte, sub string, grants []string, ttl time.Duration, now time.Time) (string, error) {
	if len(key) == 0 {
		// HMAC-SHA256 signs "successfully" with an empty key, producing
		// trivially forgeable tokens. A Service whose policy protects
		// anything always has a key (FromConfig's creation condition is
		// implied by every protected state, including admin routed, which
		// ValidatePolicy ties to an operator PIN) -- so an empty key here
		// is a wiring bug, and refusing beats signing.
		return "", fmt.Errorf("auth: refusing to sign a session token with an empty key")
	}
	claims := sessionClaims{
		Grants: grants,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   sub,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(key)
	if err != nil {
		return "", fmt.Errorf("auth: signing session token: %w", err)
	}
	return signed, nil
}

// parseToken validates tokenString against key and returns its claims.
// Only HS256-signed tokens are accepted (guarding against algorithm
// confusion, e.g. a forged "none" or RS256 token), and expiry is checked
// against the injected now with no leeway, so tests can control time
// deterministically.
func parseToken(key []byte, tokenString string, now time.Time) (*sessionClaims, error) {
	if len(key) == 0 {
		// Mirror of issueToken's guard: never accept a token verified
		// against an empty key.
		return nil, fmt.Errorf("auth: refusing to verify a session token with an empty key")
	}
	claims := &sessionClaims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(t *jwt.Token) (interface{}, error) {
		return key, nil
	}, jwt.WithValidMethods([]string{"HS256"}), jwt.WithTimeFunc(func() time.Time { return now }))
	if err != nil {
		return nil, fmt.Errorf("auth: invalid session token: %w", err)
	}
	if !token.Valid {
		return nil, fmt.Errorf("auth: invalid session token")
	}
	return claims, nil
}

// needsRefresh reports whether a session established with claims should be
// reissued with a fresh iat/exp: true once more than fraction*ttl has
// elapsed since the token was issued (sliding-window refresh).
func needsRefresh(claims *sessionClaims, ttl time.Duration, fraction float64, now time.Time) bool {
	if claims == nil || claims.IssuedAt == nil {
		return true
	}
	elapsed := now.Sub(claims.IssuedAt.Time)
	return elapsed > time.Duration(fraction*float64(ttl))
}

// pinGrant returns the scoped door-code grant string for module -- the
// grant a successful PIN login against that module's pin_file produces.
func pinGrant(module string) string {
	return pinMethod + ":" + module
}

// isIdentityGrant reports whether g is one of the identity-wide login
// grants ("ldap"/"passkey") that reach every protected non-admin module.
// Deliberately narrow: a total function over an open string space, not a
// membership check against a mutable table, so an unrecognized grant
// (legacy bare "pin", legacy "key", garbage) is never mistaken for an
// identity. admin_pin is its own identity and is handled separately by
// grantsAllow/accumulate -- it is not an isIdentityGrant.
func isIdentityGrant(g string) bool {
	return g == "ldap" || g == "passkey"
}

// hasGrant reports whether grant appears verbatim in grants.
func hasGrant(grants []string, grant string) bool {
	for _, g := range grants {
		if g == grant {
			return true
		}
	}
	return false
}

// grantsAllow reports whether claims authorizes a request to module, under
// the two-state grant vocabulary that accumulate below populates
// (grantsAllow is a pure function of claims.Grants; gate.go, S3, is its
// caller):
//
//   - module == "admin": claims must carry the "admin_pin" grant. Nothing
//     else -- not "ldap", not a module door-code grant -- ever satisfies
//     admin.
//   - any other module: an identity grant ("ldap" or "passkey") reaches
//     every protected non-admin module, or the module's own scoped
//     door-code grant ("pin:"+module, see pinGrant) reaches that module
//     alone.
//
// This is a total function over any string content claims.Grants might
// carry. claims == nil is a safe deny, never a panic; a legacy or garbage
// grant string ("pin", "key", anything else) matches neither branch and so
// authorizes nothing.
func grantsAllow(claims *sessionClaims, module string) bool {
	if claims == nil {
		return false
	}
	if module == "admin" {
		return hasGrant(claims.Grants, adminPINMethod)
	}
	for _, g := range claims.Grants {
		if isIdentityGrant(g) {
			return true
		}
	}
	return hasGrant(claims.Grants, pinGrant(module))
}

// accumulate computes the (subject, grants) pair for a freshly issued
// session token, given the caller's existing valid session (if any) and the
// single grant a just-completed login established. Callers pass exactly one
// of "ldap", "passkey", "admin_pin", or pinGrant(module) as newGrant.
//
// A door-code grant (pinGrant(module)) is never an identity: it folds into
// *any* existing valid session without touching that session's subject
// (adding a module grant never changes who the session belongs to), and
// with no prior session it creates a fresh anonymous session (Subject == "")
// holding just that grant.
//
// An identity grant ("ldap", "passkey", or "admin_pin" -- admin_pin
// authenticates as the fixed subject "admin" and is its own identity, never
// a door code, so it never folds the way a pin grant does) follows identity
// rules:
//
//   - onto an anonymous (pin-only, Subject == "") session: adopts newSub as
//     the subject and keeps the existing pin grants.
//   - onto a session with a *different* existing subject: replaces the
//     session outright -- that subject's grants are dropped.
//   - onto a session with the *same* subject: unions the grant set without
//     duplicating an already-present grant.
func accumulate(existing *sessionClaims, newSub string, newGrant string) (sub string, grants []string) {
	isIdentityLogin := isIdentityGrant(newGrant) || newGrant == adminPINMethod

	if !isIdentityLogin {
		if existing == nil {
			return "", appendGrant(nil, newGrant)
		}
		return existing.Subject, appendGrant(existing.Grants, newGrant)
	}

	if existing == nil || existing.Subject == "" {
		var base []string
		if existing != nil {
			base = existing.Grants
		}
		return newSub, appendGrant(base, newGrant)
	}
	if existing.Subject != newSub {
		return newSub, []string{newGrant}
	}
	return newSub, appendGrant(existing.Grants, newGrant)
}

// appendGrant returns existing with grant appended, unless grant is already
// present, in which case existing's contents are returned unchanged (order
// preserved, no duplication). The returned slice never aliases existing's
// backing array.
func appendGrant(existing []string, grant string) []string {
	out := make([]string, len(existing), len(existing)+1)
	copy(out, existing)
	for _, g := range out {
		if g == grant {
			return out
		}
	}
	return append(out, grant)
}

// setSessionCookie sets the uw_session cookie carrying token, valid for
// ttl. secure and domain come from the operator's cookie configuration; an
// empty domain yields a host-only cookie.
func setSessionCookie(w http.ResponseWriter, token string, ttl time.Duration, secure bool, domain string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		Domain:   domain,
		MaxAge:   int(ttl.Seconds()),
		Secure:   secure,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

// clearSessionCookie removes the uw_session cookie from the client.
func clearSessionCookie(w http.ResponseWriter, secure bool, domain string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		Domain:   domain,
		MaxAge:   -1,
		Secure:   secure,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}
