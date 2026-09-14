# FRD: issue-tracker module for unified-webapp

> **Revision 2 (2026-09-12).** Rewritten after the security merge (PR #6:
> platform auth gate, admin module, CORS/middleware rewrite). Decisions
> recorded with the product owner: (1) the reference app's built-in auth layer
> is **not** ported — the platform gate provides auth; (2) issue-tracker ships
> **protected** by the gate, and the Linear-compatible `/graphql` endpoint
> authenticates with **platform API keys** (the module's own token layer is
> removed); (3) the SPA is served by the shared **`internal/platform/static`**
> handler. `docs/adding-a-module.md` is the authoritative integration
> checklist and this FRD defers to it.

## Summary

Port the standalone issue tracker in `reference/issue-tracker/` ("newlinear" —
a self-hosted, Linear-inspired issue tracker: Go backend + React/TypeScript
frontend + SQLite) into unified-webapp as a new module, following the same
module pattern as `todo`, `grocery`, etc. This is intended to be a fairly clean
port: preserve the issue-tracking features and behavior; adapt the wiring
(config, routing, build) to unified-webapp conventions; and **replace the
reference's own auth/CORS/serving layers with the platform's**, which now
provide all of that with zero per-module code.

## Background: how unified-webapp modules work

- Single Go binary (`cmd/server`) dispatches on the Host header
  (`cmd/server/main.go` → `buildDispatcher`/`buildModule`). Each module lives in
  `internal/<module>` and exposes `Build(cfg config.<Module>Config) (http.Handler, error)`
  returning a self-contained mux. The full integration checklist — config
  struct + expander, `buildModule` case, **`knownModules` entry**,
  `host_routing` entry, `web/<module>` assets — is `docs/adding-a-module.md`;
  follow it exactly.
- The dispatcher wraps every module, for free, in: the **platform auth gate**
  (`internal/platform/auth`, `svc.Gate(module, h)`), a **1 MiB request body
  limit** (`middleware.BodyLimit` via `limitFor`; adequate for issue JSON — no
  override needed), **same-origin CORS** (`middleware.Wrap`: ACAO reflected
  only for same-origin, never `*`, plus `nosniff`), and **cross-origin write
  rejection** (`middleware.OriginCheck`, default `enforce`). The module must
  not reimplement any of this; the reference's permissive CORS layer is
  dropped, and the platform posture supersedes it.
- Shared platform packages are mandatory where applicable
  (`docs/adding-a-module.md`, "don't reimplement"): `platform/static` (SPA
  serving with `index.html` fallback), `platform/response` (JSON
  `{"error":...}` envelope, decode-error → 413/400 mapping), `platform/fspath`
  (path confinement).
- **Reserved paths**: the gate owns `GET /healthz` and `GET /api/auth/mode` on
  every module, and all of `/api/auth/*` on protected modules. The ported
  module must not define routes under `/healthz` or `/api/auth/`.
- Per-module config is a struct in `internal/platform/config/config.go`, a JSON
  section in the unified config file (see `unified-webapp-example.json`), a
  default in `WriteDefault`, and path expansion (`expand<Module>Paths`).
- Frontends are static assets under `web/<module>`, built (where TS is
  involved) by repo-root `npm run build` using esbuild (see `package.json` —
  which now also bundles multissh). No per-module node projects, no Vite, no
  runtime Node dependency.
- Builds must stay CGO-free (`build-rpi` cross-compiles for linux/arm64).
- `make test` runs `go test -race ./...`; modules carry their own tests, and
  `cmd/server`'s tests run under a goleak gate (a module whose `Build` starts
  goroutines must implement `io.Closer`; this module starts none).

## Source being ported (`reference/issue-tracker/`)

Backend (Go, `backend/`): `internal/db` (SQLite open + embedded schema, WAL,
single writer), `internal/models`, `internal/store` (issues, tags, relations,
stories, epics, seed data), `internal/api` (REST under `/api`), and
`internal/graphql` (Linear-compatible GraphQL at `/graphql`). The reference's
`main.go` wiring (CORS, SPA serving, flag/env config) is replaced by
unified-webapp equivalents.

**Not ported** (superseded by the platform auth gate): `internal/auth` (the
session/LDAP/WebAuthn service), `internal/api/auth_handlers.go`, the
`WithAuth` wiring and `/api/me`, and the module API-token layer
(`Store.EnsureToken` / `Store.ValidToken` / the `api_tokens` table and the
token check in `internal/graphql`). The platform already carries the LDAP,
WebAuthn, and bcrypt dependencies for its own gate; the module needs none of
them.

Also not ported: `backend/bin/`, `backend/newlinear.db*` (checked-in
database), `frontend/dist/`, `frontend/node_modules/`, `.git/`, `.zenflow*/`.

Frontend (React + TypeScript + React Router, `frontend/src/`): pages (Issues,
Board/Kanban, Issue detail, Stories, Epics, Projects, Tags, API), components,
DataContext, typed REST client, `styles.css`, `index.html`. **Not ported**:
`AuthContext.tsx`, `LoginPage.tsx`, the passkey helper, and the auth-dependent
parts of `AccountBar`/`App` (the platform gate serves its own login page on
401; once a request reaches the module it is already authorized).

## Functional requirements

1. **New module `issuetracker`** at `internal/issuetracker/` (routing/config
   key `"issuetracker"`; Go package names cannot contain hyphens), with
   `Build(cfg config.IssueTrackerConfig) (http.Handler, error)` integrated per
   `docs/adding-a-module.md`: `buildModule` case **and** `knownModules` entry
   in `cmd/server/main.go`, config struct + expander, `host_routing` example
   entry.
2. **Feature parity** with the reference app's issue tracking: issues (types,
   priorities, states, assignee/reporter, `TEAM-123` identifiers), tags,
   relations with automatic inverses, stories, epics, filtered views, Kanban
   board, stable issue URLs (`/issue/TEAM-123`), REST API under `/api`, and
   the Linear-compatible GraphQL endpoint at `/graphql` (same query/response
   shapes as the reference). Auth-related surfaces are exempt from parity per
   FR 4.
3. **Config**: new `IssueTrackerConfig` struct + `"issuetracker"` JSON section
   with at least `static_dir` (default `./web/issuetracker`) and `db_path`
   (default `./data/issuetracker/issues.db`), following existing conventions
   (defaults in `WriteDefault`, path expansion, entry in
   `unified-webapp-example.json`). **No auth settings in the module config** —
   module auth is a platform concern.
4. **Auth via the platform gate — no module auth code.**
   - `unified-webapp-example.json` gains an `auth.modules` entry protecting
     the module: `"issuetracker": ["ldap", "passkey"]` (matching
     obsidianoid's browser-login posture). Platform API keys work on any
     protected non-admin module automatically — that is how service clients
     reach `/graphql`.
   - The module registers no `/api/auth/*`, `/api/me`, or `/healthz` routes
     and contains no session, LDAP, WebAuthn, or token-validation code.
   - `/graphql` performs **no authentication of its own**: the module token
     layer (`EnsureToken`/`ValidToken`, the `api_tokens` table, the token
     check in the GraphQL handler, and any `/api` endpoint that returns the
     token) is removed. External Linear-compatible clients authenticate to
     the gate with `Authorization: Bearer <platform API key>` or
     `X-API-Key: <key>` (keys minted with `-gen-api-key`, hashes in
     `auth.api_keys`).
   - The frontend's API page is reworded to document platform API-key access
     to `/graphql` (it must not display or fetch any token).
5. **SQLite lifecycle**: database created, schema applied, and demo data
   seeded on first run as the reference does (minus the API-token bootstrap).
   WAL mode with a single writer connection is preserved. The schema may
   either drop the `api_tokens` table or leave it inert — planner's choice;
   nothing may read or write it.
6. **Frontend port**: move `frontend/src/` + `index.html` + `styles.css` into
   `web/issuetracker/`, minus the not-ported auth files, built by the
   repo-root esbuild pipeline (`npm run build` / `make web`) into static
   assets. The module serves them with `platform/static` (SPA fallback;
   client-side routing must work, e.g. deep links to `/issue/TEAM-123`). Add
   `react`, `react-dom`, `react-router-dom` to the root `package.json`. The
   reference Vite/per-module npm setup is dropped; API calls become
   same-origin relative paths. `npm run typecheck` must pass with the new
   sources included.
7. **Dependencies**: add `modernc.org/sqlite` (pure Go — compatible with the
   CGO-free constraint) to the root `go.mod`. No other new dependencies
   (`go-webauthn`/`go-ldap`/`bcrypt` are already present for the platform and
   are not used by the module). `make build` and `make build-rpi` (CGO-free,
   linux/arm64 cross-compile) must both succeed.
8. **Tests**: port/adapt reference tests and add module tests consistent with
   the existing modules (store + handler level; a GraphQL query smoke test
   against the handler is a good target — no token involved). Gate behavior
   (401s, API-key acceptance) is platform-tested and needs no module tests.
   `make test` (`go test -race ./...`) must pass.
9. **Housekeeping**: add `reference/` to `.gitignore` (untracked reference
   material, not part of the product). Update `README.md` / `docs/` where
   modules are enumerated.

## Non-goals

- No feature additions, redesigns, or schema changes beyond what the wiring
  swap and the auth-layer removal require.
- No module-level auth of any kind (sessions, tokens, LDAP, passkeys) — that
  is the platform gate's job.
- No integration with the platform `broker` (live-sync) — the reference app
  has no such feature.
- No standalone binary/Vite dev-server workflow for the module; it runs only
  inside unified-webapp.
- No changes to the platform gate, middleware, or admin module.

## Acceptance criteria

- `make build`, `make build-rpi`, `make test`, `make web`, and
  `npm run typecheck` all succeed.
- With a config routing e.g. `"issuetracker-test.cmdhome.net": "issuetracker"`
  (plus `localhost` for local testing) and **no** `auth.modules` entry (open
  mode, for smoke-testing), the UI loads, issues/stories/epics/tags CRUD
  works, the Kanban board renders, deep links resolve, and a `curl -X POST
  .../graphql` with a Linear-style query returns issue nodes as in the
  reference README — with no token.
- With an `auth.modules` entry `"issuetracker": [...]` and a configured
  `auth.api_keys` hash: an unauthenticated `curl` to `/api/...` or `/graphql`
  gets `401 {"error":"unauthorized"}` from the gate; the same `/graphql` call
  with `Authorization: Bearer <key>` returns issue nodes; a browser-shaped
  `GET /` receives the platform login page.
- A build failure (e.g. unwritable `db_path`) yields the sanitized platform
  503 (`{"error":"module unavailable","module":"issuetracker"}`) with the
  cause in the boot log only, per `unavailableHandler`.
- Other modules' behavior is untouched.
