package watcher

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
)

// hmacHex returns the lowercase hex HMAC-SHA256 of body keyed by secret.
func hmacHex(body []byte, secret string) string {
	h := hmac.New(sha256.New, []byte(secret))
	h.Write(body)
	return hex.EncodeToString(h.Sum(nil))
}

// subtleEqual does a constant-time comparison of two hex strings.
func subtleEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	return hmac.Equal([]byte(a), []byte(b))
}
