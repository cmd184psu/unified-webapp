// principal.go implements FR6: a small, decision-neutral way for the gate
// to surface the identity it already computed to the module handler it lets
// through, via the request's context. It carries no authorization logic of
// its own -- grantsAllow (session.go) and checkAPIKey (apikey.go) already
// decided whether the request may proceed; Principal only names who it
// proceeded as, for a module that wants to attribute a write to an actor
// (e.g. FR5's reporter attribution in the issuetracker module).
package auth

import "context"

// Principal identifies the actor a request was authorized as, once the gate
// has already decided to let it through. Method is one of "apikey", "ldap",
// "passkey", or "" (unset/anonymous -- never attached to the context in that
// case, see WithPrincipal callers in gate.go). Subject is the API key's
// configured name for "apikey", or the session's identity (claims.Subject)
// for "ldap"/"passkey".
type Principal struct {
	Method  string
	Subject string
}

// principalCtxKey is the unexported context key under which a Principal is
// stored, so no other package can accidentally collide with or read it
// except through WithPrincipal/PrincipalFromContext.
type principalCtxKey struct{}

// WithPrincipal returns a copy of ctx carrying p as the request's resolved
// principal.
func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, principalCtxKey{}, p)
}

// PrincipalFromContext returns the Principal previously attached to ctx via
// WithPrincipal, and whether one was present. A context with no attached
// principal (an unprotected module's pass-through request, or any context
// outside the gate's control) returns the zero Principal and ok=false.
func PrincipalFromContext(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalCtxKey{}).(Principal)
	return p, ok
}

// identityGrantOf returns the first identity-shaped grant found in grants --
// "ldap", "passkey", or "admin_pin" -- or "" when grants carries none of
// them (e.g. a door-code-only session, whose grants are all "pin:<module>").
// It never inspects the module a session is being checked against; that
// decision already happened in grantsAllow before a caller reaches here.
func identityGrantOf(grants []string) string {
	for _, g := range grants {
		if isIdentityGrant(g) || g == adminPINMethod {
			return g
		}
	}
	return ""
}
