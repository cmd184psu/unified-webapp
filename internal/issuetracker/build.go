// Package issuetracker wires the ported issue tracker (from
// reference/issue-tracker) into unified-webapp as a module: SQLite store +
// REST API + Linear-compatible
// GraphQL + SPA, all behind the platform auth gate (which supplies auth,
// CORS, body limits, and the caller principal this module attributes writes
// to). See docs/frd-issue-tracker.md and docs/adding-a-module.md.
package issuetracker

import (
	"net/http"

	"cmd184psu/unified-webapp/internal/issuetracker/api"
	"cmd184psu/unified-webapp/internal/issuetracker/db"
	"cmd184psu/unified-webapp/internal/issuetracker/graphql"
	"cmd184psu/unified-webapp/internal/issuetracker/store"
	"cmd184psu/unified-webapp/internal/platform/config"
	"cmd184psu/unified-webapp/internal/platform/static"
)

// Build returns the issuetracker module handler. A non-nil error makes the
// dispatcher serve this host a sanitized 503 (unavailableHandler) rather than
// crashing the binary. Build starts no goroutines, so the handler needs no
// io.Closer.
func Build(cfg config.IssueTrackerConfig) (http.Handler, error) {
	database, err := db.Open(cfg.DBPath)
	if err != nil {
		return nil, err
	}
	st := store.New(database)
	if err := st.Seed(); err != nil {
		return nil, err
	}
	// Ensure the attribution users exist on every boot (Seed only runs on an
	// empty DB): the fixed API-key user and the configured default user.
	if _, err := st.EnsureUserByUsername("api", "API"); err != nil {
		return nil, err
	}
	if _, err := st.EnsureUserByUsername(cfg.DefaultUser.Email, cfg.DefaultUser.Name); err != nil {
		return nil, err
	}

	mux := http.NewServeMux()
	api.New(st, cfg.DefaultUser.Name, cfg.DefaultUser.Email).Routes(mux)
	mux.Handle("/graphql", graphql.New(st, cfg.DefaultUser.Name, cfg.DefaultUser.Email))
	mux.Handle("/", static.NewHandler(cfg.StaticDir))

	return mux, nil
}
