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
	methods := []string{"ldap", "passkey"}

	tok, err := issueToken(key, "chris", methods, time.Hour, now)
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
	if !reflect.DeepEqual(claims.Methods, methods) {
		t.Errorf("Methods = %v, want %v", claims.Methods, methods)
	}
	if claims.IssuedAt == nil || !claims.IssuedAt.Time.Equal(now) {
		t.Errorf("IssuedAt = %v, want %v", claims.IssuedAt, now)
	}
	if claims.ExpiresAt == nil || !claims.ExpiresAt.Time.Equal(now.Add(time.Hour)) {
		t.Errorf("ExpiresAt = %v, want %v", claims.ExpiresAt, now.Add(time.Hour))
	}
}

func TestParseTokenExpired(t *testing.T) {
	key := testKey()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	tok, err := issueToken(key, "chris", []string{"pin"}, time.Hour, now)
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
	tok, err := issueToken(testKey(), "chris", []string{"pin"}, time.Hour, now)
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
	payload := `{"sub":"chris","methods":["pin"],"iat":1735689600,"exp":9999999999}`
	tok := forgeToken(t, "none", payload, "")
	if _, err := parseToken(testKey(), tok, time.Unix(1735689600, 0)); err == nil {
		t.Fatal("expected error for alg=none forged token, got nil")
	}
}

func TestParseTokenRejectsAlgRS256(t *testing.T) {
	payload := `{"sub":"chris","methods":["pin"],"iat":1735689600,"exp":9999999999}`
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

func TestAccumulate(t *testing.T) {
	tests := []struct {
		name        string
		existing    *sessionClaims
		newSub      string
		newMethod   string
		wantSub     string
		wantMethods []string
	}{
		{
			name:        "nil prior session",
			existing:    nil,
			newSub:      "chris",
			newMethod:   "pin",
			wantSub:     "chris",
			wantMethods: []string{"pin"},
		},
		{
			name:        "same identity, new method appended",
			existing:    &sessionClaims{Methods: []string{"pin"}, RegisteredClaims: jwt.RegisteredClaims{Subject: "chris"}},
			newSub:      "chris",
			newMethod:   "ldap",
			wantSub:     "chris",
			wantMethods: []string{"pin", "ldap"},
		},
		{
			name:        "same method twice, no duplicate",
			existing:    &sessionClaims{Methods: []string{"pin", "ldap"}, RegisteredClaims: jwt.RegisteredClaims{Subject: "chris"}},
			newSub:      "chris",
			newMethod:   "ldap",
			wantSub:     "chris",
			wantMethods: []string{"pin", "ldap"},
		},
		{
			name:        "conflicting identity replaces set",
			existing:    &sessionClaims{Methods: []string{"pin", "ldap"}, RegisteredClaims: jwt.RegisteredClaims{Subject: "chris"}},
			newSub:      "alex",
			newMethod:   "pin",
			wantSub:     "alex",
			wantMethods: []string{"pin"},
		},
		{
			name:        "empty existing methods",
			existing:    &sessionClaims{Methods: []string{}, RegisteredClaims: jwt.RegisteredClaims{Subject: "chris"}},
			newSub:      "chris",
			newMethod:   "pin",
			wantSub:     "chris",
			wantMethods: []string{"pin"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sub, methods := accumulate(tt.existing, tt.newSub, tt.newMethod)
			if sub != tt.wantSub {
				t.Errorf("sub = %q, want %q", sub, tt.wantSub)
			}
			if !reflect.DeepEqual(methods, tt.wantMethods) {
				t.Errorf("methods = %v, want %v", methods, tt.wantMethods)
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
