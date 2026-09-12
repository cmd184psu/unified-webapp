package middleware

import "net/http"

// Wrap reflects the request's Origin in Access-Control-Allow-Origin only when
// it is same-origin, and handles preflight OPTIONS requests for that case.
// Requests with no Origin header (curl/same-host clients) pass through
// unaffected. Foreign origins are never blessed with an ACAO header; blocking
// foreign requests outright is handled separately by OriginCheck.
func Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		origin := r.Header.Get("Origin")
		sameOrigin := origin != "" && SameOrigin(r)
		if origin != "" {
			w.Header().Set("Vary", "Origin")
			if sameOrigin {
				w.Header().Set("Access-Control-Allow-Origin", origin)
			}
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions && sameOrigin {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
