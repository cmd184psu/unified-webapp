package middleware

import (
	"log"
	"net/http"

	"cmd184psu/unified-webapp/internal/platform/response"
)

// ModeEnforce, ModeLog and ModeOff are the valid runtime values for
// config.ServerConfig.OriginCheck. An empty string is accepted at the
// call site below and treated as ModeEnforce (the secure default); any
// other value is a boot-time configuration error and must be rejected by
// the caller before it ever reaches OriginCheck.
const (
	ModeEnforce = "enforce"
	ModeLog     = "log"
	ModeOff     = "off"
)

// ValidOriginCheckModes lists every value config.ServerConfig.OriginCheck may
// hold, including the empty string (treated as ModeEnforce). Boot validation
// should reject anything not in this set.
var ValidOriginCheckModes = []string{"", ModeEnforce, ModeLog, ModeOff}

// OriginCheck rejects cross-origin state-changing requests. It only inspects
// methods other than GET and HEAD -- those are assumed safe/idempotent and
// are left to Wrap's CORS handling. A request with no Origin header always
// passes through, since that covers curl and other non-browser clients this
// deployment relies on.
//
// mode selects the behavior for a foreign Origin on a write method:
//   - ModeEnforce (or ""): respond 403 and never invoke next -- the request
//     body is never read.
//   - ModeLog: log the mismatch and invoke next as usual.
//   - ModeOff: no check, no logging.
func OriginCheck(mode string, next http.Handler) http.Handler {
	if mode == "" {
		mode = ModeEnforce
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if mode == ModeOff {
			next.ServeHTTP(w, r)
			return
		}
		if r.Method == http.MethodGet || r.Method == http.MethodHead {
			next.ServeHTTP(w, r)
			return
		}
		origin := r.Header.Get("Origin")
		if origin == "" || SameOrigin(r) {
			next.ServeHTTP(w, r)
			return
		}

		log.Printf("origin check: rejecting %s %s -- Origin %q does not match Host %q", r.Method, r.URL.Path, origin, r.Host)

		if mode == ModeLog {
			next.ServeHTTP(w, r)
			return
		}

		// mode == ModeEnforce: reject without reading the body.
		response.WriteError(w, http.StatusForbidden, "cross-origin request rejected")
	})
}
