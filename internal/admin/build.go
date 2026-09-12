// Package admin implements the admin module (FR-M): the operator console
// for live-editing auth.* configuration without a restart.
//
// admin.Build deliberately deviates from every other module's
// Build(cfg config.XConfig) (http.Handler, error) signature
// (security-plan.md L12, the FR-M6 sanctioned special case): live-apply
// (T5.3) needs the running *auth.Service to swap in a new Policy, the config
// file's on-disk path to splice the auth section in place, and the exact
// knownModules/adminRouted arguments boot passed to auth.ValidatePolicy, so
// a live edit is validated by the same code path with the same inputs boot
// used. No other module needs any of that, so no other module gets this
// shape.
//
// auth may not import admin (that would break platform/auth's
// zero-module-coupling invariant); the reverse -- admin importing auth -- is
// fine and expected, since admin is auth's sanctioned consumer.
package admin

import (
	"net/http"

	"cmd184psu/unified-webapp/internal/platform/auth"
	"cmd184psu/unified-webapp/internal/platform/config"
	"cmd184psu/unified-webapp/internal/platform/static"
)

// Deps carries what admin.Build needs beyond cfg.Admin -- see the package
// doc comment for why these exist even though T5.1 does not use them yet.
type Deps struct {
	// Service is the running auth service. T5.3's live-apply calls
	// Service.SwapPolicy on it after validating and persisting a new
	// config.AuthConfig.
	Service *auth.Service

	// ConfigPath is the absolute path of the on-disk config file (from
	// config.Config.ConfigPath). T5.3's live-apply splices the "auth"
	// member's bytes in this file in place.
	ConfigPath string

	// KnownModules is the buildModule universe cmd/server/main.go passed to
	// auth.FromConfig at boot. T5.3's live-apply passes the same slice to
	// auth.ValidatePolicy so a save is validated by the identical rule set
	// boot enforces.
	KnownModules []string

	// AdminRouted is whether "admin" appears among cfg.Routing's values, as
	// computed at boot. T5.3's live-apply passes the same value to
	// auth.ValidatePolicy for the same reason as KnownModules.
	AdminRouted bool
}

// Build returns a ready-to-use http.Handler for the admin module: a static
// SPA shell (web/admin, served with the shared SPA-fallback static.Handler)
// plus the Handler's (currently empty) API routes. T5.1 is a scaffold --
// the data endpoints (T5.4) and live-apply (T5.3) build on the Deps and
// Handler this wires up now.
func Build(cfg *config.Config, deps Deps) (http.Handler, error) {
	h := NewHandler(cfg.Admin, cfg.Auth, deps)

	mux := http.NewServeMux()
	h.Register(mux)
	mux.Handle("/", static.NewHandler(cfg.Admin.StaticDir))

	return mux, nil
}
