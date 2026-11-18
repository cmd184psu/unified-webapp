package api

import (
	"net/http"

	"cmd184psu/unified-webapp/internal/platform/auth"
)

// anonymousActor is recorded when the module runs unprotected and the request
// carries no principal.
const anonymousActor = "anonymous"

// actorOf names who a write is attributed to (DBS3): the principal the auth
// gate attached to the request (the API key's name or the session identity),
// else "anonymous". It is passed to every store and engine write, which
// records it in the same transaction as the change.
func actorOf(r *http.Request) string {
	p, ok := auth.PrincipalFromContext(r.Context())
	switch {
	case !ok:
		return anonymousActor
	case p.Subject != "":
		return p.Subject
	case p.Method != "":
		return p.Method
	}
	return anonymousActor
}
