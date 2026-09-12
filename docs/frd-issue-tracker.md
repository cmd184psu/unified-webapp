# FRD: issue-tracker module for unified-webapp

## Summary

Port the standalone issue tracker in `reference/issue-tracker/` ("newlinear" — a
self-hosted, Linear-inspired issue tracker: Go backend + React/TypeScript
frontend + SQLite) into unified-webapp as a new module, following the same
module pattern as `todo`, `grocery`, etc. This is intended to be a fairly clean
port: preserve features and behavior; adapt only the wiring (config, routing,
build) to unified-webapp conventions.

## Background: how unified-webapp modules work

- Single Go binary (`cmd/server`) dispatches on the Host header
  (`cmd/server/main.go` → `buildDispatcher`/`buildModule`). Each module lives in
  `internal/<module>` and exposes `Build(cfg config.<Module>Config) (http.Handler, error)`
  returning a self-contained mux (API routes + static file serving with SPA
  fallback — see `internal/todo/build.go` for the canonical shape).
- Per-module config is a struct in `internal/platform/config/config.go`, a JSON
  section in the unified config file (see `unified-webapp-example.json`), a
  default in `WriteDefault`, and path expansion (`expand<Module>Paths`).
- Frontends are static assets under `web/<module>`, built (where TS is
  involved) by repo-root `npm run build` using esbuild (see `package.json`).
  No per-module node projects, no Vite, no runtime Node dependency.
- Builds must stay CGO-free (`build-rpi` cross-compiles for linux/arm64).
- `make test` runs `go test -race ./...`; modules carry their own tests
  (e.g. `internal/todo/handler_test.go`, `store_test.go`).

## Source being ported (`reference/issue-tracker/`)

Backend (Go, `backend/`): `internal/db` (SQLite open + embedded schema, WAL,
single writer), `internal/models`, `internal/store` (issues, tags, relations,
stories, epics, seed + API token), `internal/api` (REST under `/api`, auth
handlers), `internal/auth` (session auth service with LDAP and WebAuthn/passkey
support, configured from env via `auth.FromEnv`), `internal/graphql`
(Linear-compatible token-authenticated GraphQL at `/graphql`). `main.go` adds
CORS, SPA serving, and flag/env config — that wiring is replaced by the
unified-webapp equivalents.

Dependencies: `modernc.org/sqlite` (pure Go, no CGO — compatible with the
CGO-free constraint), `github.com/go-webauthn/webauthn`,
`github.com/go-ldap/ldap/v3`.

Frontend (React + TypeScript + React Router, `frontend/src/`): pages (Issues,
Board/Kanban, Issue detail, Stories, Epics, Projects, Tags, API), components,
contexts (Auth, Data), typed REST client, passkey helper, `styles.css`,
`index.html`.

Do NOT port: `backend/bin/`, `backend/newlinear.db*` (checked-in database),
`frontend/dist/`, `frontend/node_modules/`, `.git/`, `.zenflow*/`.

## Functional requirements

1. **New module `issuetracker`** at `internal/issuetracker/` (routing/config
   key `"issuetracker"`; Go package names cannot contain hyphens), with
   `Build(cfg config.IssueTrackerConfig) (http.Handler, error)` registered in
   `cmd/server/main.go`'s `buildModule`, alongside the existing modules.
2. **Feature parity** with the reference app: issues (types, priorities,
   states, assignee/reporter, `TEAM-123` identifiers), tags, relations with
   automatic inverses, stories, epics, filtered views, Kanban board, stable
   issue URLs (`/issue/TEAM-123`), REST API under `/api`, and the
   Linear-compatible GraphQL endpoint at `/graphql` (token-authenticated,
   token generated/printed on first run and shown on the API page).
3. **Config**: new `IssueTrackerConfig` struct + `"issuetracker"` JSON section
   with at least `static_dir` (default `./web/issuetracker`) and `db_path`
   (default `./data/issuetracker/issues.db`), following existing conventions
   (defaults in `WriteDefault`, path expansion, entry in
   `unified-webapp-example.json`). Auth settings (mode, LDAP, WebAuthn/passkey,
   cookie-secure) move from env vars into this config section, preserving the
   reference behavior and defaults (env fallback optional).
4. **SQLite lifecycle**: database created, schema applied, demo data seeded,
   and API token generated on first run, exactly as the reference does. WAL
   mode with a single writer connection is preserved.
5. **Frontend port**: move `frontend/src/` + `index.html` + `styles.css` into
   `web/issuetracker/`, built by the repo-root esbuild pipeline (`npm run
   build` / `make web`) into static assets that the module serves with SPA
   fallback (client-side routing must work, e.g. deep links to
   `/issue/TEAM-123`). Add `react`, `react-dom`, `react-router-dom` to the
   root `package.json`. The reference Vite/per-module npm setup is dropped;
   API calls become same-origin relative paths (the reference's permissive
   CORS layer for the Vite dev server is not needed and must not be ported).
   `npm run typecheck` must pass with the new sources included.
6. **Dependencies**: add `modernc.org/sqlite`, `go-webauthn`, and `go-ldap` to
   the root `go.mod`. `make build` and `make build-rpi` (CGO-free, linux/arm64
   cross-compile) must both succeed.
7. **Tests**: port/adapt any reference tests and add module tests consistent
   with the existing modules (store + handler level; GraphQL token auth is a
   good smoke target). `make test` (`go test -race ./...`, goleak in use
   elsewhere) must pass.
8. **Housekeeping**: add `reference/` to `.gitignore` (it is untracked
   reference material, not part of the product). Update `README.md` /
   `docs/` where modules are enumerated.

## Non-goals

- No feature additions, redesigns, or schema changes beyond what the wiring
  swap requires.
- No integration with the platform `broker` (live-sync) — the reference app
  has no such feature.
- No standalone binary/Vite dev-server workflow for the module; it runs only
  inside unified-webapp.

## Acceptance criteria

- `make build`, `make build-rpi`, `make test`, `make web`, and
  `npm run typecheck` all succeed.
- With a config routing e.g. `"issuetracker-test.cmdhome.net": "issuetracker"`
  (plus `localhost` for local testing), the UI loads, issues/stories/epics/tags
  CRUD works, the Kanban board renders, deep links resolve, and a
  `curl -X POST .../graphql` with the printed token returns issue nodes as in
  the reference README.
- Other modules' behavior is untouched.
