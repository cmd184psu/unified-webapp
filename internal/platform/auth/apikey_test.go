package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"testing"

	"cmd184psu/unified-webapp/internal/platform/config"
)

// hashKey returns the "sha256:<hex>" form stored in config for the given
// plaintext key string.
func hashKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func newReq(t *testing.T, authHeader, apiKeyHeader string) *http.Request {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	if authHeader != "" {
		r.Header.Set("Authorization", authHeader)
	}
	if apiKeyHeader != "" {
		r.Header.Set("X-API-Key", apiKeyHeader)
	}
	return r
}

func TestCheckAPIKeyBearer(t *testing.T) {
	keys := []config.NamedHash{
		{Name: "svc-a", Hash: hashKey("key-a")},
	}
	r := newReq(t, "Bearer key-a", "")
	name, ok := checkAPIKey(keys, r)
	if !ok || name != "svc-a" {
		t.Fatalf("checkAPIKey(Bearer key-a) = (%q, %v), want (svc-a, true)", name, ok)
	}
}

func TestCheckAPIKeyXAPIKeyHeader(t *testing.T) {
	keys := []config.NamedHash{
		{Name: "svc-a", Hash: hashKey("key-a")},
	}
	r := newReq(t, "", "key-a")
	name, ok := checkAPIKey(keys, r)
	if !ok || name != "svc-a" {
		t.Fatalf("checkAPIKey(X-API-Key key-a) = (%q, %v), want (svc-a, true)", name, ok)
	}
}

func TestCheckAPIKeyBearerTakesPrecedence(t *testing.T) {
	keys := []config.NamedHash{
		{Name: "svc-a", Hash: hashKey("key-a")},
		{Name: "svc-b", Hash: hashKey("key-b")},
	}
	r := newReq(t, "Bearer key-a", "key-b")
	name, ok := checkAPIKey(keys, r)
	if !ok || name != "svc-a" {
		t.Fatalf("checkAPIKey(both headers) = (%q, %v), want (svc-a, true) since Bearer takes precedence", name, ok)
	}
}

func TestCheckAPIKeyInvalidKey(t *testing.T) {
	keys := []config.NamedHash{
		{Name: "svc-a", Hash: hashKey("key-a")},
	}
	r := newReq(t, "Bearer wrong-key", "")
	name, ok := checkAPIKey(keys, r)
	if ok {
		t.Fatalf("checkAPIKey(wrong key) = (%q, %v), want ok=false", name, ok)
	}
}

func TestCheckAPIKeyNoHeaders(t *testing.T) {
	keys := []config.NamedHash{
		{Name: "svc-a", Hash: hashKey("key-a")},
	}
	r := newReq(t, "", "")
	name, ok := checkAPIKey(keys, r)
	if ok {
		t.Fatalf("checkAPIKey(no headers) = (%q, %v), want ok=false", name, ok)
	}
}

func TestCheckAPIKeyMalformedAuthorizationFallsBackToXAPIKey(t *testing.T) {
	keys := []config.NamedHash{
		{Name: "svc-a", Hash: hashKey("key-a")},
	}
	// Authorization present but not "Bearer "-shaped (e.g. Basic auth) --
	// must fall back to X-API-Key rather than failing outright.
	r := newReq(t, "Basic dXNlcjpwYXNz", "key-a")
	name, ok := checkAPIKey(keys, r)
	if !ok || name != "svc-a" {
		t.Fatalf("checkAPIKey(malformed Authorization + valid X-API-Key) = (%q, %v), want (svc-a, true)", name, ok)
	}
}

func TestCheckAPIKeyExactHashForm(t *testing.T) {
	// Entry hash built by hand in the canonical "sha256:<hex>" form, to
	// verify comparison isn't accidentally tied to the hashKey helper.
	sum := sha256.Sum256([]byte("literal-key"))
	entryHash := "sha256:" + hex.EncodeToString(sum[:])
	keys := []config.NamedHash{
		{Name: "literal", Hash: entryHash},
	}
	r := newReq(t, "Bearer literal-key", "")
	name, ok := checkAPIKey(keys, r)
	if !ok || name != "literal" {
		t.Fatalf("checkAPIKey(literal hash form) = (%q, %v), want (literal, true)", name, ok)
	}
}

func TestCheckAPIKeyMultipleEntriesSecondMatches(t *testing.T) {
	keys := []config.NamedHash{
		{Name: "svc-a", Hash: hashKey("key-a")},
		{Name: "svc-b", Hash: hashKey("key-b")},
		{Name: "svc-c", Hash: hashKey("key-c")},
	}
	r := newReq(t, "Bearer key-b", "")
	name, ok := checkAPIKey(keys, r)
	if !ok || name != "svc-b" {
		t.Fatalf("checkAPIKey(key-b) = (%q, %v), want (svc-b, true)", name, ok)
	}
}

func TestCheckAPIKeyEmptyKeys(t *testing.T) {
	r := newReq(t, "Bearer anything", "")
	name, ok := checkAPIKey(nil, r)
	if ok {
		t.Fatalf("checkAPIKey(nil keys) = (%q, %v), want ok=false", name, ok)
	}
}
