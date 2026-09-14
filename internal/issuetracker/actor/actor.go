// Package actor resolves the user an issue-tracker write is attributed to,
// from the platform auth principal on the request. This is the single place
// that maps "who is calling" to a stored users row (FR5): the fixed "api"
// user for an API key, the LDAP/passkey user for a browser session, or the
// configured default user when the module runs unprotected (no principal).
package actor

import (
	"net/http"

	"cmd184psu/unified-webapp/internal/issuetracker/models"
	"cmd184psu/unified-webapp/internal/issuetracker/store"
	"cmd184psu/unified-webapp/internal/platform/auth"
)

// Resolve returns the user to attribute the current request's action to.
// defaultName/defaultEmail configure the open-mode fallback user.
func Resolve(r *http.Request, st *store.Store, defaultName, defaultEmail string) (*models.User, error) {
	p, ok := auth.PrincipalFromContext(r.Context())
	if !ok {
		return st.EnsureUserByUsername(defaultEmail, defaultName)
	}
	switch p.Method {
	case "apikey":
		return st.EnsureUserByUsername("api", "API")
	case "ldap", "passkey":
		if p.Subject != "" {
			return st.EnsureUserByUsername(p.Subject, p.Subject)
		}
	}
	return st.EnsureUserByUsername(defaultEmail, defaultName)
}
