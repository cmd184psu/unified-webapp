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
// Methods records the ordered list of auth methods that were used to
// establish the session (e.g. ["pin"], ["ldap", "passkey"], ["admin_pin"]).
type sessionClaims struct {
	Methods []string `json:"methods"`
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
// methods, with iat=now and exp=now+ttl.
func issueToken(key []byte, sub string, methods []string, ttl time.Duration, now time.Time) (string, error) {
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
		Methods: methods,
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

// accumulate computes the (subject, methods) pair for a freshly issued
// session token, given the caller's existing valid session (if any) and the
// auth method that was just completed.
//
//   - existing == nil (no valid prior session): the new session starts
//     fresh with only newMethod.
//   - existing.Subject == newSub (same identity re-authenticating with
//     another method): newMethod is folded into the existing method list,
//     preserving order and without duplicating a method already present.
//   - existing.Subject != newSub (a different identity): the new session
//     replaces the old outright; the prior subject's methods are dropped.
func accumulate(existing *sessionClaims, newSub string, newMethod string) (sub string, methods []string) {
	if existing == nil || existing.Subject != newSub {
		return newSub, []string{newMethod}
	}
	methods = make([]string, len(existing.Methods), len(existing.Methods)+1)
	copy(methods, existing.Methods)
	for _, m := range methods {
		if m == newMethod {
			return newSub, methods
		}
	}
	return newSub, append(methods, newMethod)
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
