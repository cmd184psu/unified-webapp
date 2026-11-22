// Package auth is the pass-through left after the module's own LDAP and
// passkey login moved to the platform gate. Machine API keys are separate.
package auth

import "net/http"

// Provider wraps a handler and reports the active mode. The only mode left
// is "none": human sign-in is the platform's LDAP and PIN gate.
type Provider interface {
	Middleware(next http.Handler) http.Handler
	Mode() string
}

// NoAuth lets every request through. The platform gate already decided
// whether the human is signed in.
type NoAuth struct{}

// Middleware returns next unchanged.
func (NoAuth) Middleware(next http.Handler) http.Handler { return next }

// Mode reports that this module does not run its own login.
func (NoAuth) Mode() string { return "none" }
