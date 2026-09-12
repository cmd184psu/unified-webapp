package middleware

import (
	"net/http"
	"net/url"
)

// SameOrigin permits same-origin upgrades and requests with no Origin header
// (non-browser clients). A mismatched Origin returns false; callers are
// responsible for any rejection logging/auditing.
func SameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	host := r.Host
	// Compare the Origin's host:port against the request Host.
	return OriginHost(origin) == host
}

// OriginHost extracts the host[:port] from an Origin header value, returning
// the raw value if it cannot be parsed (which will simply fail the equality
// check in SameOrigin and reject the upgrade).
func OriginHost(origin string) string {
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return origin
	}
	return u.Host
}
