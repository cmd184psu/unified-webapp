package auth

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"cmd184psu/unified-webapp/internal/platform/config"
)

func testKey() []byte {
	return []byte("0123456789abcdef0123456789abcdef")
}

func TestIssueParseRoundTrip(t *testing.T) {
	key := testKey()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	grants := []string{"ldap", "passkey"}

	tok, err := issueToken(key, "chris", grants, time.Hour, now)
	if err != nil {
		t.Fatalf("issueToken: %v", err)
	}

	claims, err := parseToken(key, tok, now)
	if err != nil {
		t.Fatalf("parseToken: %v", err)
	}
	if claims.Subject != "chris" {
		t.Errorf("Subject = %q, want %q", claims.Subject, "chris")
	}
	if !reflect.DeepEqual(claims.Grants, grants) {
		t.Errorf("Grants = %v, want %v", claims.Grants, grants)
	}
	if claims.IssuedAt == nil || !claims.IssuedAt.Time.Equal(now) {
		t.Errorf("IssuedAt = %v, want %v", claims.IssuedAt, now)
	}
	if claims.ExpiresAt == nil || !claims.ExpiresAt.Time.Equal(now.Add(time.Hour)) {
		t.Errorf("ExpiresAt = %v, want %v", claims.ExpiresAt, now.Add(time.Hour))
	}
}

// TestParseTokenClaimKeyIsGrants locks in the claim-key rename (Decision B1,
// PLAN-auth-two-state.md): the wire JSON key is "grants", not the
// pre-cutover "methods" -- a token whose payload uses the old key must
// decode with an empty Grants slice, not populate it.
func TestParseTokenClaimKeyIsGrants(t *testing.T) {
	key := testKey()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	// A token signed exactly like a pre-cutover session, using the old
	// "methods" claim key instead of "grants".
	forged := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": "chris", "methods": []string{"ldap"},
		"iat": now.Unix(), "exp": now.Add(time.Hour).Unix(),
	})
	signed, err := forged.SignedString(key)
	if err != nil {
		t.Fatalf("signing legacy-shaped token: %v", err)
	}

	claims, err := parseToken(key, signed, now)
	if err != nil {
		t.Fatalf("parseToken(legacy methods-keyed token): %v", err)
	}
	if len(claims.Grants) != 0 {
		t.Errorf("Grants = %v, want empty for a token signed under the old \"methods\" claim key", claims.Grants)
	}
}

func TestParseTokenExpired(t *testing.T) {
	key := testKey()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	tok, err := issueToken(key, "chris", []string{pinGrant("todo")}, time.Hour, now)
	if err != nil {
		t.Fatalf("issueToken: %v", err)
	}

	later := now.Add(time.Hour + time.Second)
	if _, err := parseToken(key, tok, later); err == nil {
		t.Fatal("expected error for expired token, got nil")
	}
}

func TestParseTokenGarbage(t *testing.T) {
	if _, err := parseToken(testKey(), "not.a.jwt", time.Now()); err == nil {
		t.Fatal("expected error for garbage token, got nil")
	}
}

func TestParseTokenWrongKey(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	tok, err := issueToken(testKey(), "chris", []string{pinGrant("todo")}, time.Hour, now)
	if err != nil {
		t.Fatalf("issueToken: %v", err)
	}

	otherKey := []byte("fedcba9876543210fedcba9876543210")
	if _, err := parseToken(otherKey, tok, now); err == nil {
		t.Fatal("expected error for token signed with different key, got nil")
	}
}

// forgeToken builds a JWT string by hand, bypassing the jwt library's
// signing path entirely, so tests can exercise algorithm-confusion attacks
// (e.g. a header claiming "none" or "RS256") that a well-behaved signer
// would never produce.
func forgeToken(t *testing.T, alg string, payload string, sig string) string {
	t.Helper()
	header := `{"alg":"` + alg + `","typ":"JWT"}`
	enc := base64.RawURLEncoding.EncodeToString
	return enc([]byte(header)) + "." + enc([]byte(payload)) + "." + enc([]byte(sig))
}

func TestParseTokenRejectsAlgNone(t *testing.T) {
	payload := `{"sub":"chris","grants":["pin:todo"],"iat":1735689600,"exp":9999999999}`
	tok := forgeToken(t, "none", payload, "")
	if _, err := parseToken(testKey(), tok, time.Unix(1735689600, 0)); err == nil {
		t.Fatal("expected error for alg=none forged token, got nil")
	}
}

func TestParseTokenRejectsAlgRS256(t *testing.T) {
	payload := `{"sub":"chris","grants":["pin:todo"],"iat":1735689600,"exp":9999999999}`
	tok := forgeToken(t, "RS256", payload, "garbage-signature-bytes")
	if _, err := parseToken(testKey(), tok, time.Unix(1735689600, 0)); err == nil {
		t.Fatal("expected error for alg=RS256 forged token, got nil")
	}
}

func TestNeedsRefresh(t *testing.T) {
	ttl := 100 * time.Hour
	iat := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	claims := &sessionClaims{}
	claims.IssuedAt = jwt.NewNumericDate(iat)

	notYet := iat.Add(time.Duration(0.49 * float64(ttl)))
	if needsRefresh(claims, ttl, 0.5, notYet) {
		t.Errorf("needsRefresh at 0.49*ttl = true, want false")
	}

	yes := iat.Add(time.Duration(0.51 * float64(ttl)))
	if !needsRefresh(claims, ttl, 0.5, yes) {
		t.Errorf("needsRefresh at 0.51*ttl = false, want true")
	}
}

func TestSessionTTLAndRefreshFractionDefaults(t *testing.T) {
	if got := sessionTTL(config.SessionConfig{}); got != defaultSessionTTL {
		t.Errorf("sessionTTL(zero) = %v, want %v", got, defaultSessionTTL)
	}
	if got := sessionTTL(config.SessionConfig{TTLHours: 24}); got != 24*time.Hour {
		t.Errorf("sessionTTL(24) = %v, want 24h", got)
	}
	if got := refreshFraction(config.SessionConfig{}); got != defaultRefreshFraction {
		t.Errorf("refreshFraction(zero) = %v, want %v", got, defaultRefreshFraction)
	}
	if got := refreshFraction(config.SessionConfig{RefreshAfterFraction: 0.75}); got != 0.75 {
		t.Errorf("refreshFraction(0.75) = %v, want 0.75", got)
	}
}

func TestSetSessionCookie(t *testing.T) {
	tests := []struct {
		name   string
		secure bool
		domain string
	}{
		{"secure with domain", true, "example.com"},
		{"insecure host-only", false, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			setSessionCookie(rec, "the-token", 2*time.Hour, tt.secure, tt.domain)

			resp := rec.Result()
			cookies := resp.Cookies()
			if len(cookies) != 1 {
				t.Fatalf("got %d cookies, want 1", len(cookies))
			}
			c := cookies[0]

			if c.Name != sessionCookieName {
				t.Errorf("Name = %q, want %q", c.Name, sessionCookieName)
			}
			if c.Value != "the-token" {
				t.Errorf("Value = %q, want %q", c.Value, "the-token")
			}
			if !c.HttpOnly {
				t.Error("HttpOnly = false, want true")
			}
			if c.SameSite != http.SameSiteLaxMode {
				t.Errorf("SameSite = %v, want Lax", c.SameSite)
			}
			if c.Path != "/" {
				t.Errorf("Path = %q, want %q", c.Path, "/")
			}
			if c.MaxAge != 7200 {
				t.Errorf("MaxAge = %d, want 7200", c.MaxAge)
			}
			if c.Secure != tt.secure {
				t.Errorf("Secure = %v, want %v", c.Secure, tt.secure)
			}
			if c.Domain != tt.domain {
				t.Errorf("Domain = %q, want %q", c.Domain, tt.domain)
			}
		})
	}
}

// TestGrantsAllowTotality exercises grantsAllow across the two-state grant
// vocabulary, including legacy/garbage values that must deny rather than
// panic or accidentally match.
func TestGrantsAllowTotality(t *testing.T) {
	tests := []struct {
		name   string
		claims *sessionClaims
		module string
		want   bool
	}{
		{"nil claims", nil, "todo", false},
		{"admin with admin_pin grant", &sessionClaims{Grants: []string{"admin_pin"}}, "admin", true},
		{"admin with ldap grant does not satisfy admin", &sessionClaims{Grants: []string{"ldap"}}, "admin", false},
		{"admin with matching pin grant does not satisfy admin", &sessionClaims{Grants: []string{"pin:admin"}}, "admin", false},
		{"admin with no grants", &sessionClaims{Grants: nil}, "admin", false},
		{"module with ldap identity grant", &sessionClaims{Grants: []string{"ldap"}}, "todo", true},
		{"module with passkey identity grant", &sessionClaims{Grants: []string{"passkey"}}, "todo", true},
		{"module with its own pin grant", &sessionClaims{Grants: []string{"pin:todo"}}, "todo", true},
		{"module with a different module's pin grant", &sessionClaims{Grants: []string{"pin:slideshow"}}, "todo", false},
		{"module with admin_pin grant does not satisfy a module", &sessionClaims{Grants: []string{"admin_pin"}}, "todo", false},
		{"module with legacy bare \"pin\" grant denies", &sessionClaims{Grants: []string{"pin"}}, "todo", false},
		{"module with legacy bare \"key\" grant denies", &sessionClaims{Grants: []string{"key"}}, "todo", false},
		{"module with garbage grant denies", &sessionClaims{Grants: []string{"carrier_pigeon"}}, "todo", false},
		{"module with no grants", &sessionClaims{Grants: nil}, "todo", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := grantsAllow(tt.claims, tt.module); got != tt.want {
				t.Errorf("grantsAllow(%+v, %q) = %v, want %v", tt.claims, tt.module, got, tt.want)
			}
		})
	}
}

func TestAccumulate(t *testing.T) {
	tests := []struct {
		name       string
		existing   *sessionClaims
		newSub     string
		newGrant   string
		wantSub    string
		wantGrants []string
	}{
		{
			name:       "door-code grant with no prior session creates an anonymous session",
			existing:   nil,
			newSub:     "",
			newGrant:   pinGrant("todo"),
			wantSub:    "",
			wantGrants: []string{"pin:todo"},
		},
		{
			name:       "door-code grant folds into an existing session without touching its subject",
			existing:   &sessionClaims{Grants: []string{"ldap"}, RegisteredClaims: jwt.RegisteredClaims{Subject: "chris"}},
			newSub:     "",
			newGrant:   pinGrant("todo"),
			wantSub:    "chris",
			wantGrants: []string{"ldap", "pin:todo"},
		},
		{
			name:       "identity login onto an anonymous pin-only session adopts the subject and keeps pin grants",
			existing:   &sessionClaims{Grants: []string{"pin:todo"}, RegisteredClaims: jwt.RegisteredClaims{Subject: ""}},
			newSub:     "chris",
			newGrant:   "ldap",
			wantSub:    "chris",
			wantGrants: []string{"pin:todo", "ldap"},
		},
		{
			name:       "identity login onto a different existing subject replaces the session outright",
			existing:   &sessionClaims{Grants: []string{"ldap", "pin:todo"}, RegisteredClaims: jwt.RegisteredClaims{Subject: "chris"}},
			newSub:     "alex",
			newGrant:   "ldap",
			wantSub:    "alex",
			wantGrants: []string{"ldap"},
		},
		{
			name:       "identity login onto the same subject unions without duplicating",
			existing:   &sessionClaims{Grants: []string{"ldap"}, RegisteredClaims: jwt.RegisteredClaims{Subject: "chris"}},
			newSub:     "chris",
			newGrant:   "passkey",
			wantSub:    "chris",
			wantGrants: []string{"ldap", "passkey"},
		},
		{
			name:       "admin_pin is its own identity, authenticating as admin",
			existing:   nil,
			newSub:     "admin",
			newGrant:   "admin_pin",
			wantSub:    "admin",
			wantGrants: []string{"admin_pin"},
		},
		{
			name:       "admin_pin unions with an existing admin session rather than folding like a pin grant",
			existing:   &sessionClaims{Grants: []string{"admin_pin"}, RegisteredClaims: jwt.RegisteredClaims{Subject: "admin"}},
			newSub:     "admin",
			newGrant:   "admin_pin",
			wantSub:    "admin",
			wantGrants: []string{"admin_pin"},
		},
		{
			// A door-code login accumulating onto a pre-deploy token that
			// used the old "methods" JSON key: the old grants are invisible
			// (Grants decodes empty), but the subject -- an ordinary JWT
			// registered claim, unaffected by the claim-key rename -- is
			// still there. The door-code grant folds into that stale
			// session without touching its subject, exactly as it would for
			// a session with visible grants.
			name:       "door-code grant onto a stale pre-rename token preserves its subject and starts a fresh grant list",
			existing:   &sessionClaims{Grants: nil, RegisteredClaims: jwt.RegisteredClaims{Subject: "someuser"}},
			newSub:     "",
			newGrant:   pinGrant("todo"),
			wantSub:    "someuser",
			wantGrants: []string{"pin:todo"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sub, grants := accumulate(tt.existing, tt.newSub, tt.newGrant)
			if sub != tt.wantSub {
				t.Errorf("sub = %q, want %q", sub, tt.wantSub)
			}
			if !reflect.DeepEqual(grants, tt.wantGrants) {
				t.Errorf("grants = %v, want %v", grants, tt.wantGrants)
			}
		})
	}
}

func TestClearSessionCookie(t *testing.T) {
	rec := httptest.NewRecorder()
	clearSessionCookie(rec, true, "example.com")

	resp := rec.Result()
	cookies := resp.Cookies()
	if len(cookies) != 1 {
		t.Fatalf("got %d cookies, want 1", len(cookies))
	}
	c := cookies[0]

	if c.Name != sessionCookieName {
		t.Errorf("Name = %q, want %q", c.Name, sessionCookieName)
	}
	if c.Value != "" {
		t.Errorf("Value = %q, want empty", c.Value)
	}
	if c.MaxAge != -1 {
		t.Errorf("MaxAge = %d, want -1", c.MaxAge)
	}
	if !c.Secure {
		t.Error("Secure = false, want true")
	}
}

// TestTokenFunctionsRefuseEmptyKey locks in the empty-key guard: HMAC-SHA256
// happily "signs" with an empty key, which would make every session token
// trivially forgeable if a Service ever reached issueToken/parseToken without
// its key loaded (it cannot today -- FromConfig creates the key for every
// protected state, and ValidatePolicy ties admin routing to an operator PIN
// -- but the guard makes the failure loud instead of silently exploitable).
func TestTokenFunctionsRefuseEmptyKey(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if _, err := issueToken(nil, "admin", []string{"admin_pin"}, time.Hour, now); err == nil {
		t.Fatal("issueToken with an empty key succeeded, want refusal")
	}
	if _, err := issueToken([]byte{}, "admin", []string{"admin_pin"}, time.Hour, now); err == nil {
		t.Fatal("issueToken with a zero-length key succeeded, want refusal")
	}
	// A token hand-signed with an empty key must not verify either.
	forged := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": "admin", "grants": []string{"admin_pin"},
		"iat": now.Unix(), "exp": now.Add(time.Hour).Unix(),
	})
	forgedStr, err := forged.SignedString([]byte{})
	if err != nil {
		t.Fatalf("signing forged token: %v", err)
	}
	if _, err := parseToken(nil, forgedStr, now); err == nil {
		t.Fatal("parseToken with an empty key accepted a forged token, want refusal")
	}
}
