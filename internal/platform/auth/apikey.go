package auth

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"strings"

	"cmd184psu/unified-webapp/internal/platform/config"
)

// sha256HashPrefix is the prefix stored on every NamedHash entry in
// config.AuthConfig.APIKeys, ahead of the lowercase hex-encoded SHA-256 digest
// of the key string.
const sha256HashPrefix = "sha256:"

// checkAPIKey extracts a presented API key from r -- an Authorization: Bearer
// header first, falling back to X-API-Key if that header is absent or not
// Bearer-shaped -- and compares its SHA-256 digest against every entry in
// keys, returning the name of the first match.
//
// A missing, malformed, or unrecognized key is a silent (\"\", false): no
// error, no logging, so callers can fall through to cookie auth. checkAPIKey
// only reads r; it never touches cookies or a ResponseWriter and never
// creates a session.
func checkAPIKey(keys []config.NamedHash, r *http.Request) (name string, ok bool) {
	presented := extractAPIKey(r)
	if presented == "" {
		return "", false
	}

	sum := sha256.Sum256([]byte(presented))
	presentedHex := hex.EncodeToString(sum[:])

	for _, entry := range keys {
		entryHex, ok := strings.CutPrefix(entry.Hash, sha256HashPrefix)
		if !ok {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(presentedHex), []byte(entryHex)) == 1 {
			return entry.Name, true
		}
	}
	return "", false
}

// extractAPIKey reads the presented key value from r: the Authorization
// header's Bearer token if present and well-formed, otherwise the
// X-API-Key header. It returns "" if neither is present.
func extractAPIKey(r *http.Request) string {
	if auth := r.Header.Get("Authorization"); auth != "" {
		const prefix = "Bearer "
		if strings.HasPrefix(auth, prefix) {
			return strings.TrimPrefix(auth, prefix)
		}
	}
	return r.Header.Get("X-API-Key")
}
