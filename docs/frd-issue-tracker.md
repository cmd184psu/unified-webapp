# FRD: issue-tracker module for unified-webapp

> **Revision 3 (2026-09-13).** Adds the identity/attribution model (FR 5) and
> the single sanctioned platform change it requires (FR 6), decided with the
> product owner. Carries forward Revision 2's decisions: (1) the reference
> app's built-in auth layer is **not** ported — the platform gate provides
> auth; (2) issue-tracker ships **protected** by the gate, and the
> Linear-compatible `/graphql` endpoint authenticates with **platform API
> keys** (the module's own token layer is removed); (3) the SPA is served by
> the shared **`internal/platform/static`** handler. New in Revision 3: (4) an
> issue's **reporter** is the request's actor — the API-key identity `api`, the
> logged-in LDAP/passkey user, or a configured **default user** in open mode —
> which requires the gate to expose the authenticated principal to the module
> (via request context) and a new platform-owned `GET /api/auth/whoami` route.
> `docs/adding-a-module.md` is the authoritative integration checklist and this
> FRD defers to it.

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
  every module, all of `/api/auth/*` on protected modules, and (added by FR 6)
  `GET /api/auth/whoami` on every module. The ported module must not define
  routes under `/healthz` or `/api/auth/`.
- **Caller identity** (added by FR 6): the gate attaches an `auth.Principal`
  (`{Method, Subject}`) to the request context in every allow branch, readable
  by the module via `auth.PrincipalFromContext(r.Context())`. This is the only
  channel by which the module learns who the caller is — the session cookie is
  signed with the gate's key and the module must not parse it. An unprotected
  (open-mode) module receives no principal.
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
`DataContext.tsx` currently imports `useAuth`/`me` from the dropped
`AuthContext`; it must be edited (not moved verbatim) to source the current
actor from `GET /api/auth/whoami` instead (FR 5). On the backend, the
reference's `currentUser(r)` helper (which reads the reference auth package's
`PrincipalFromContext`) is repointed at the platform's
`auth.PrincipalFromContext` — the same shape, a different package.

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
   with at least `static_dir` (default `./web/issuetracker`), `db_path`
   (default `./data/issuetracker/issues.db`), and `default_user` (name/email of
   the reporter/assignee actor used when there is no authenticated principal,
   i.e. open mode — see FR 5; sensible default e.g. `"Unassigned"` /
   `"unassigned@localhost"`), following existing conventions (defaults in
   `WriteDefault`, path expansion, entry in `unified-webapp-example.json`).
   `default_user` is **not** an auth setting — it names a seed data row, not a
   credential; module auth stays a platform concern.
4. **Auth via the platform gate — no module auth code.**
   - `unified-webapp-example.json` gains an `auth.modules` entry protecting
     the module: `"issuetracker": ["ldap", "passkey"]` (matching
     obsidianoid's browser-login posture). Platform API keys work on any
     protected non-admin module automatically — that is how service clients
     reach `/graphql`.
   - The module registers no `/api/auth/*`, `/api/me`, or `/healthz` routes
     and contains no session, LDAP, WebAuthn, or token-validation code. The
     reference's `/api/me` is dropped; the current identity comes from the
     platform-owned `GET /api/auth/whoami` instead (FR 6). The module may read
     the caller's identity only via `auth.PrincipalFromContext` (FR 5/6), never
     by inspecting cookies or keys itself.
   - `/graphql` performs **no authentication of its own**: the module token
     layer (`EnsureToken`/`ValidToken`, the `api_tokens` table, the token
     check in the GraphQL handler, and any `/api` endpoint that returns the
     token) is removed. External Linear-compatible clients authenticate to
     the gate with `Authorization: Bearer <platform API key>` or
     `X-API-Key: <key>` (keys minted with `-gen-api-key`, hashes in
     `auth.api_keys`).
   - The frontend's API page is reworded to document platform API-key access
     to `/graphql` (it must not display or fetch any token).
5. **Identity & attribution.** Every issue write resolves a single "actor"
   user from the request's principal (FR 6), and the module maps that actor to
   a row in the existing `users` table:
   - **API key** (`Method == "apikey"`) → the seeded **`api`** user.
   - **LDAP / passkey session** (`Method == "ldap"`/`"passkey"`, non-empty
     `Subject`) → the user whose identity is that `Subject`, **found-or-created**
     on first write (the reference already ensures the logged-in user is in the
     assignee pool; here it happens lazily at write time since there is no
     module login hook).
   - **No principal** (open mode) → the configured **`default_user`** (FR 3),
     resolved at **request time** by find-or-create on the current config value
     (not baked in at seed). Editing `default_user` in the JSON and restarting
     therefore re-points attribution for **subsequently** created issues;
     already-created issues keep their (immutable) reporter. Module config is
     read only at boot, so the change needs a restart, and — unlike `auth.*` —
     it is not editable through the admin UI.
   Attribution rules (matching the reference):
   - **Reporter** is set to the actor at create time and is **immutable**
     thereafter; clients cannot set or change it.
   - **Assignee** is a normal, freely editable field (default **unassigned**),
     and `assignee=me` in filters/queries resolves `me` to the actor.
   - Seed/first-run ensures the `api` user and the `default_user` rows exist
     (in addition to the reference's demo users). Assignee/reporter therefore
     always reference real `users` rows.
6. **Platform additions for caller identity (the one sanctioned platform
   change).** This FRD's only change outside `internal/issuetracker` and the
   config/wiring files. It is **purely additive** — it changes no existing gate
   access *decision* (who is allowed through is unchanged):
   - **Exported principal type + accessor** in `internal/platform/auth`:
     `type Principal struct { Method, Subject string }` (Method one of
     `"apikey"`, `"ldap"`, `"passkey"`, or `""`), plus
     `PrincipalFromContext(ctx) (Principal, bool)`.
   - **Gate context injection**: in each allow branch of `Gate`
     (`gate.go`) the gate attaches the resolved `Principal` to the request
     context before `next.ServeHTTP` — the API-key name for `checkAPIKey`
     matches, `claims.Subject` + the identity grant for a session. It must not
     wrap the `ResponseWriter` (the hijacker/SSE invariant the gate documents),
     only carry the request through with an enriched context. The unprotected
     (open-mode) passthrough attaches no principal.
   - **New route** `GET /api/auth/whoami`, gate-owned and available on **every**
     module like `/api/auth/mode` (before the protected/unprotected split). It
     reports the effective principal across **both** auth paths — session
     **and** API key (unlike the existing session-cookie-only
     `/api/auth/session`) — as `{"authenticated":bool,"method":...,"identity":...}`;
     an unauthenticated/open request gets `200 {"authenticated":false}`. Add it
     to the reserved paths and the `knownModules`-independent gate route set.
   - Platform-level tests cover context injection and `whoami` for each auth
     path; the module trusts the accessor and needs no auth tests of its own.
7. **SQLite lifecycle**: database created, schema applied, and demo data
   seeded on first run as the reference does (minus the API-token bootstrap,
   plus the `api`/`default_user` rows per FR 5). WAL mode with a single writer
   connection is preserved. The schema may either drop the `api_tokens` table
   or leave it inert — planner's choice; nothing may read or write it.
8. **Frontend port**: move `frontend/src/` + `index.html` + `styles.css` into
   `web/issuetracker/`, minus the not-ported auth files, built by the
   repo-root esbuild pipeline (`npm run build` / `make web`) into static
   assets. The module serves them with `platform/static` (SPA fallback;
   client-side routing must work, e.g. deep links to `/issue/TEAM-123`). Add
   `react`, `react-dom`, `react-router-dom` to the root `package.json`. The
   reference Vite/per-module npm setup is dropped; API calls become
   same-origin relative paths. `DataContext` sources the current actor from
   `GET /api/auth/whoami` (rendering "Me" and resolving `assignee=me`) rather
   than the dropped `AuthContext`. `npm run typecheck` must pass with the new
   sources included.
9. **Dependencies**: add `modernc.org/sqlite` (pure Go — compatible with the
   CGO-free constraint) to the root `go.mod`. No other new dependencies
   (`go-webauthn`/`go-ldap`/`bcrypt` are already present for the platform and
   are not used by the module). `make build` and `make build-rpi` (CGO-free,
   linux/arm64 cross-compile) must both succeed.
10. **Tests**: port/adapt reference tests and add module tests consistent with
    the existing modules (store + handler level; a GraphQL query smoke test
    against the handler is a good target — no token involved; plus reporter
    attribution across the three actor cases in FR 5). Gate behavior (401s,
    API-key acceptance, context injection, `whoami`) is platform-tested. `make
    test` (`go test -race ./...`) must pass.
11. **Housekeeping**: add `reference/` to `.gitignore` (untracked reference
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
- No changes to the platform gate's **access decisions**, middleware, or admin
  module. FR 6's additive principal-exposure (request-context injection, the
  `PrincipalFromContext` accessor, and the `GET /api/auth/whoami` route) is the
  sole sanctioned platform change and alters no allow/deny outcome.

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
- **Identity/attribution**: `GET /api/auth/whoami` reports
  `{"authenticated":false}` in open mode, the LDAP username after a browser
  login, and the API-key identity for a `Bearer`-key request. An issue created
  via an API key has reporter `api`; one created in an LDAP session has the
  logged-in user as reporter; one created in open mode has `default_user` as
  reporter. Reporter cannot be changed after creation; assignee can, and
  `assignee=me` returns the actor's issues.
- A build failure (e.g. unwritable `db_path`) yields the sanitized platform
  503 (`{"error":"module unavailable","module":"issuetracker"}`) with the
  cause in the boot log only, per `unavailableHandler`.
- Other modules' behavior is untouched.
