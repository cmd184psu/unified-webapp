// session_scope.go: per-module scoping of a session, on top of grants.
//
// Two rules the plain grant check (grantsAllow) can't express:
//
//  1. A PIN login is tied to the PIN it used. Its grant is
//     "pin:<module>:<fp>", fp being a keyed fingerprint of the module's PIN
//     file contents (never the PIN itself). Changing the PIN file changes
//     the fingerprint, so every session made with the old PIN stops working
//     on its next request -- for exactly the modules that share that file.
//
//  2. Each module signs out on its own idle time, even when one cookie (a
//     shared cookie_domain) carries several modules' grants. The token
//     records when each grant was established (GrantTimes) and when each
//     module was last used (ModuleSeen). A module is allowed only while its
//     own clock is within its idle limit (auth.modules.<m>.idle_minutes,
//     default 60), and only real use renews it (isUserActivity): page
//     loads, changes, and the shared page code's activity pings for clicks
//     and typing. A page's background polling and live streams still work
//     but renew nothing, so an untouched tab idles out.
//
//  3. However active, a login lasts at most the maximum session length
//     (auth.session.ttl_hours), counted from when its grant was established.
//
// Tokens issued before these fields existed fall back to their IssuedAt.
package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// pinFingerprint is a keyed, truncated hash of a PIN: stable for a given PIN
// and server key, useless for recovering the PIN.
func pinFingerprint(key []byte, pin string) string {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte("uw-pin-v1\x00" + pin))
	return hex.EncodeToString(mac.Sum(nil))[:16]
}

// pinGrantFP is the fingerprinted door-code grant a PIN login produces.
func pinGrantFP(module, fp string) string {
	return pinGrant(module) + ":" + fp
}

// modulePINGrant returns module's fingerprinted pin grant from grants, if any.
func modulePINGrant(grants []string, module string) (string, bool) {
	prefix := pinGrant(module) + ":"
	for _, g := range grants {
		if strings.HasPrefix(g, prefix) && len(g) > len(prefix) {
			return g, true
		}
	}
	return "", false
}

// withoutModulePIN returns grants minus any pin grant for module.
func withoutModulePIN(grants []string, module string) []string {
	prefix := pinGrant(module) + ":"
	out := make([]string, 0, len(grants))
	for _, g := range grants {
		if g == pinGrant(module) || strings.HasPrefix(g, prefix) {
			continue
		}
		out = append(out, g)
	}
	return out
}

// withoutModuleIdentity returns grants minus module's own ldap/passkey grants.
func withoutModuleIdentity(grants []string, module string) []string {
	out := make([]string, 0, len(grants))
	for _, g := range grants {
		if g == identityGrant("ldap", module) || g == identityGrant("passkey", module) {
			continue
		}
		out = append(out, g)
	}
	return out
}

// pinFPCache memoizes each PIN file's fingerprint, recomputed only when the
// file's modification time or size changes, so the per-request check costs
// one stat, not a read.
type pinFPCache struct {
	mu      sync.Mutex
	entries map[string]pinFPEntry
}

type pinFPEntry struct {
	mod  time.Time
	size int64
	fp   string
}

// currentPINFingerprint is the fingerprint of the PIN now in path, or false
// if the file can't be read (a session can't be vouched for then).
func (s *Service) currentPINFingerprint(path string) (string, bool) {
	info, err := os.Stat(path)
	if err != nil {
		return "", false
	}
	c := &s.pinFP
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.entries[path]; ok && e.mod.Equal(info.ModTime()) && e.size == info.Size() {
		return e.fp, true
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	fp := pinFingerprint(s.key, strings.TrimSpace(string(data)))
	if c.entries == nil {
		c.entries = make(map[string]pinFPEntry)
	}
	c.entries[path] = pinFPEntry{mod: info.ModTime(), size: info.Size(), fp: fp}
	return fp, true
}

// authorizingGrant is the grant that lets claims into module: admin_pin for
// admin, else module's own identity grant, else module's own pin grant.
func authorizingGrant(claims *sessionClaims, module string) (string, bool) {
	if module == "admin" {
		if hasGrant(claims.Grants, adminPINMethod) {
			return adminPINMethod, true
		}
		return "", false
	}
	if g, ok := identityGrantFor(claims.Grants, module); ok {
		return g, true
	}
	return modulePINGrant(claims.Grants, module)
}

// moduleClock is when module's login period last started: its last renewal,
// else when its authorizing grant was established, else the token's issue
// time (tokens from before per-module clocks existed).
func moduleClock(claims *sessionClaims, module, grant string) time.Time {
	var t int64
	if ts, ok := claims.ModuleSeen[module]; ok {
		t = ts
	}
	if ts, ok := claims.GrantTimes[grant]; ok && ts > t {
		t = ts
	}
	if t == 0 && claims.IssuedAt != nil {
		return claims.IssuedAt.Time
	}
	return time.Unix(t, 0)
}

// sessionAllows is the full check for a session request into module: the
// grant (grantsAllow), a pin grant's fingerprint against the module's
// current PIN file, the module's idle limit, and the maximum session length.
func (s *Service) sessionAllows(claims *sessionClaims, module string, p *Policy, now time.Time) bool {
	return s.idleRemaining(claims, module, p, now) > 0
}

// idleRemaining is how much longer claims stays signed in to module without
// further use (0 when it isn't allowed at all): the smaller of what's left
// of the module's idle limit and of the maximum session length.
func (s *Service) idleRemaining(claims *sessionClaims, module string, p *Policy, now time.Time) time.Duration {
	if !grantsAllow(claims, module) {
		return 0
	}
	grant, ok := authorizingGrant(claims, module)
	if !ok {
		return 0
	}
	if strings.HasPrefix(grant, pinGrant(module)+":") {
		cur, ok := s.currentPINFingerprint(p.Modules[module].PinFile)
		if !ok || !hmac.Equal([]byte(strings.TrimPrefix(grant, pinGrant(module)+":")), []byte(cur)) {
			return 0
		}
	}
	left := p.moduleIdle(module) - now.Sub(moduleClock(claims, module, grant))
	if cap := p.SessionTTL - now.Sub(grantTime(claims, grant)); cap < left {
		left = cap
	}
	if left < 0 {
		return 0
	}
	return left
}

// grantTime is when grant was established, for the maximum session length;
// tokens from before per-grant times fall back to their issue time.
func grantTime(claims *sessionClaims, grant string) time.Time {
	if ts, ok := claims.GrantTimes[grant]; ok {
		return time.Unix(ts, 0)
	}
	if claims.IssuedAt != nil {
		return claims.IssuedAt.Time
	}
	return time.Time{}
}

// renewEvery limits how often use renews a module's idle clock, so a busy
// page reissues its cookie at most once a minute rather than per request.
const renewEvery = time.Minute

// moduleNeedsRenewal reports whether module's idle clock is old enough to
// be worth renewing.
func moduleNeedsRenewal(claims *sessionClaims, module string, now time.Time) bool {
	grant, _ := authorizingGrant(claims, module)
	return now.Sub(moduleClock(claims, module, grant)) >= renewEvery
}

// isUserActivity reports whether r is something a person did, which renews
// the module's idle clock: loading a page (an HTML GET outside /api/), or
// changing something (any method but GET/HEAD/OPTIONS -- saves, deletes,
// and the shared page code's POST /api/auth/activity for clicks, typing,
// scrolling). Background GETs -- polling, SSE, WebSocket upgrades -- are not.
func isUserActivity(r *http.Request) bool {
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		return !strings.HasPrefix(r.URL.Path, "/api/") &&
			strings.Contains(r.Header.Get("Accept"), "text/html") &&
			!strings.EqualFold(r.Header.Get("Upgrade"), "websocket")
	case http.MethodOptions:
		return false
	}
	return true
}

// renewModule reissues the session cookie with module's idle clock set to
// now, leaving every other module's clock as it was.
func (s *Service) renewModule(w http.ResponseWriter, claims *sessionClaims, module string, p *Policy, now time.Time) {
	if tok, err := issueClaims(s.key, renewedModule(claims, module, now), p.tokenLifetime(), now); err == nil {
		setSessionCookie(w, tok, p.tokenLifetime(), p.CookieSecure, p.CookieDomain)
	}
}

// renewedModule returns claims with module's clock renewed at now, leaving
// every other module's clock as it was.
func renewedModule(claims *sessionClaims, module string, now time.Time) sessionClaims {
	next := *claims
	next.GrantTimes = copyTimes(claims.GrantTimes)
	next.ModuleSeen = copyTimes(claims.ModuleSeen)
	next.ModuleSeen[module] = now.Unix()
	return next
}

// loginClaims builds the session after a login: the accumulated subject and
// grants, carrying forward the clocks of grants that remain, stamping the
// new grant, and starting module's login period now.
func loginClaims(existing *sessionClaims, sub string, grants []string, newGrant, module string, now time.Time) sessionClaims {
	c := sessionClaims{Grants: grants, GrantTimes: map[string]int64{}, ModuleSeen: map[string]int64{}}
	c.Subject = sub
	if existing != nil {
		for _, g := range grants {
			if ts, ok := existing.GrantTimes[g]; ok {
				c.GrantTimes[g] = ts
			}
		}
		for m, ts := range existing.ModuleSeen {
			c.ModuleSeen[m] = ts
		}
	}
	c.GrantTimes[newGrant] = now.Unix()
	c.ModuleSeen[module] = now.Unix()
	return c
}

func copyTimes(in map[string]int64) map[string]int64 {
	out := make(map[string]int64, len(in)+1)
	for k, v := range in {
		out[k] = v
	}
	return out
}

// existingForLogin is the session a login producing newGrant in module
// builds on: the request's valid session, minus any older door-code grant
// for module when the login is itself a door-code login (the new grant
// replaces it, e.g. after a PIN change).
func (s *Service) existingForLogin(r *http.Request, module, newGrant string, now time.Time) *sessionClaims {
	existing, ok := s.sessionClaimsFromRequest(r, now)
	if !ok {
		return nil
	}
	if !strings.HasPrefix(newGrant, pinGrant(module)+":") {
		return existing
	}
	c := *existing
	c.Grants = withoutModulePIN(existing.Grants, module)
	return &c
}

// signedOutOf returns what's left of claims after signing out of module, or
// nil when nothing is (the cookie should be cleared). Every grant belongs to
// one module, so signing out of a module drops only that module's grants.
func signedOutOf(claims *sessionClaims, module string) *sessionClaims {
	grants := withoutModuleIdentity(withoutModulePIN(claims.Grants, module), module)
	if module == "admin" {
		grants = withoutGrant(grants, adminPINMethod)
	}
	if len(grants) == 0 {
		return nil
	}
	next := *claims
	next.Grants = grants
	next.GrantTimes = map[string]int64{}
	for _, g := range grants {
		if ts, ok := claims.GrantTimes[g]; ok {
			next.GrantTimes[g] = ts
		}
	}
	next.ModuleSeen = copyTimes(claims.ModuleSeen)
	delete(next.ModuleSeen, module)
	if identityGrantOf(grants) == "" {
		// No login left that names a person, so the session is anonymous
		// (door codes only) again.
		next.Subject = ""
	}
	return &next
}

func withoutGrant(grants []string, grant string) []string {
	out := make([]string, 0, len(grants))
	for _, g := range grants {
		if g != grant {
			out = append(out, g)
		}
	}
	return out
}
