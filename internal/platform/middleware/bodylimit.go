package middleware

import "net/http"

// BodyLimit caps the request body at n bytes using http.MaxBytesReader. It
// does not wrap the ResponseWriter -- a downstream handler that needs
// http.Hijacker (WebSocket upgrades) or http.Flusher (SSE) keeps direct
// access to it. A body that exceeds n surfaces as an *http.MaxBytesError from
// the next read/decode of r.Body; response.WriteDecodeError maps that to 413.
func BodyLimit(n int64, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, n)
		next.ServeHTTP(w, r)
	})
}
