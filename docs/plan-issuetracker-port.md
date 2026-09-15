# Implementation Plan — `issuetracker` module port (ralplan consensus draft)

**Status:** pending approval · **Source of truth:** `docs/frd-issue-tracker.md` (Rev 3) + `docs/adding-a-module.md`
**Mode:** deliberate (touches the platform auth gate — security-sensitive)

---

## RALPLAN-DR summary

### Principles
1. **The platform posture is inherited, never reimplemented.** Auth gate, CORS, body limits, static serving, JSON envelope, path confinement all come from `internal/platform/*`. The module ports issue-tracking logic *only*.
2. **The platform change changes no allow/deny outcome for module routes.** FR6 only *surfaces* identity the gate already computes and adds one always-open info route (`whoami`); it must not alter which module requests are allowed or denied. (Verified: no existing module or `web/` route defines `/api/auth/whoami`, so the added route shadows nothing.)
3. **Follow the repo's own precedents, not the reference's.** `utuber` for subpackages, `smbedit` for React+esbuild, `certmachine` for a SQLite+DBPath config — copy these shapes rather than inventing.
4. **CGO-free, always.** Pure-Go `modernc.org/sqlite`; `make build-rpi` (linux/arm64) is the canary.
5. **Every issue write attributes a reporter** deterministically from the request actor — REST *and* GraphQL, no client override.

### Decision drivers (top 3)
1. **Blast radius of the gate change** — it wraps every module; a regression there breaks all of them. Highest-risk item; gets its own phase, its own tests, and lands first behind existing tests.
2. **CGO-free cross-compile** — one impure dependency fails `build-rpi`. Constrains the SQLite driver and forbids any transitive CGO.
3. **Convention drift** — the reference has its own auth/CORS/token/Vite stack; porting verbatim would fight the platform. Drivers favor strip-then-adapt over copy-verbatim.

### Viable options considered

**O1 — Backend package layout.**
- **O1a (chosen): keep reference subpackages** under `internal/issuetracker/{models,store,db,api,graphql}` + `internal/issuetracker/build.go` (package `issuetracker`). *Pros:* minimal per-file churn (only import-path + package rewrites), matches `utuber`'s subpackage precedent, smaller diff to review. *Cons:* more packages than `todo`/`grocery`.
- **O1b: flatten** into one `issuetracker` package. *Pros:* matches the smaller modules. *Cons:* forces renaming every type reference and merging 4 packages by hand — larger, error-prone diff for no functional gain. **Invalidated:** churn/risk with no benefit; `utuber` already legitimizes subpackages.

**O2 — Frontend build.**
- **O2a (chosen): mirror `smbedit`** — sources at `web/issuetracker/src/`, one esbuild `main.tsx --bundle --jsx=automatic --define:process.env.NODE_ENV` entry → `web/issuetracker/js/bundle.js` (+ `bundle.css`), served by `platform/static`. *Pros:* proven in-repo, react/react-dom/@types/tsconfig already present. *Cons:* none material.
- **O2b: keep Vite.** **Invalidated** by FRD non-goal (no per-module node project / dev server).

**O3 — `api_tokens` table.**
- **O3a (chosen): leave the table inert**, delete only the code paths that read/write it (`EnsureToken`/`ValidToken`, `/api/token`, `token` in bootstrap, graphql `authorized()`). *Pros:* smallest schema diff, no migration concern on existing dev DBs. *Cons:* one unused table.
- **O3b: drop the table** from `schema.sql`. Acceptable per FRD; marginally cleaner but risks confusing a pre-existing DB. Either is FRD-sanctioned; **O3a chosen** for minimal blast radius.

---

## Pre-mortem (3 scenarios)

1. **The gate change breaks an existing module's auth.** *Cause:* editing `Gate`'s control flow (e.g. handling `whoami` after the protected/unprotected split, or attaching context in a way that reorders branches). *Mitigation:* insert `whoami` as a standalone early branch adjacent to `/api/auth/mode` (step 2, before the `protected` check); attach context via `r = r.WithContext(...)` **only inside existing allow branches**, changing no condition; run the full existing `internal/platform/auth` + `cmd/server` test suites *before and after* and diff; add explicit tests asserting a request that was previously allowed/denied still is.
2. **esbuild can't resolve `react-router-dom` / the bundle balloons or breaks at runtime.** *Cause:* missing dep, ESM/CJS interop, or `process.env.NODE_ENV` not defined. *Mitigation:* add `react-router-dom` to root `dependencies`; copy smbedit's exact flag set (`--jsx=automatic --define:process.env.NODE_ENV`); verify with `npm run build` + a manual load of the SPA and a deep-link before declaring done; `npm run typecheck` must include the new sources.
3. **CGO creeps in / `build-rpi` fails.** *Cause:* an accidentally-added dependency pulls CGO, or a `go.sum` version skew between the reference's pins and root's newer `ldap`/`webauthn`. *Mitigation:* add only `modernc.org/sqlite` (pure Go); do not import `go-ldap`/`go-webauthn`/`bcrypt` from the module; run `CGO_ENABLED=0 make build-rpi` as a gate in the final phase; reconcile `go.mod`/`go.sum` with `go mod tidy` and build against root's existing versions.
4. **(bonus) find-or-create races the single SQLite writer** for a new LDAP user on concurrent first writes. *Cause:* two requests both `INSERT` the same username. *Mitigation:* `EnsureUserByUsername` uses `INSERT ... ON CONFLICT(username) DO NOTHING` then `SELECT` (or a unique constraint + upsert); `db.Open` already sets `SetMaxOpenConns(1)`, serializing writes, so this is low-risk but the upsert makes it correct.

---

## Phased work plan

> Each phase ends with a **gate**: it must build and its tests pass before the next phase starts. Phases A–C are backend; D frontend; E wiring/docs; F full verification.

### Phase A — Platform FR6 additions (the sensitive, decision-neutral change)
**Files (modify/create under `internal/platform/auth/`):**
- **`principal.go` (new):** `type Principal struct { Method, Subject string }`; unexported `principalCtxKey`; `func WithPrincipal(ctx, Principal) context.Context`; `func PrincipalFromContext(ctx) (Principal, bool)`.
- **`gate.go` (modify — surgical):**
  - Add a **step-2-adjacent** branch: `GET /api/auth/whoami` → resolve the effective principal for this request (try `checkAPIKey` → `{apikey, name}`; else `sessionClaimsFromRequest` → `{<identity-grant>, claims.Subject}`; else anonymous) and `response.WriteJSON(200, {"authenticated":bool,"method":...,"identity":...})`. Available on **every** module, before the `protected` check, exactly like `/api/auth/mode`.
  - **Enumerate every `next.ServeHTTP` site and its principal disposition** (executor must account for all; missing one = an un-enriched actor, not a wrong decision):
    - `gate.go:140` unprotected passthrough → **no principal** (open mode; `PrincipalFromContext` returns `ok=false`, module falls back to `default_user`).
    - `gate.go:180` admin session branch → `{Method:<grant>, Subject:claims.Subject}` (harmless; admin isn't this module but keep the gate uniform).
    - `gate.go:185` API-key branch → `{Method:"apikey", Subject:<key name from checkAPIKey>}`.
    - `gate.go:195` session branch → `{Method:<identity grant: "ldap"/"passkey">, Subject:claims.Subject}`.
    - The gate-owned auth routes (`rt.handle`, `gate.go:162`) do **not** reach the module and need no principal.
  - Attach via `r = r.WithContext(auth.WithPrincipal(...))` immediately before each `next.ServeHTTP`. **Do not** wrap `w` (hijacker/SSE invariant).
  - Add `/api/auth/whoami` to the reserved-path documentation/route set.
- **`gate_test.go` / `principal_test.go` (new/extend):** assert (a) context principal present+correct on API-key and session paths, absent on open; (b) `whoami` returns the three shapes; (c) **regression:** allow/deny outcomes for a representative protected + unprotected + admin request are unchanged.

**Gate:** `go test ./internal/platform/... ./cmd/server/...` green (compare to a pre-change run).

### Phase B — Module backend port (`internal/issuetracker/…`)
**Copy `reference/issue-tracker/backend/internal/{models,store,db,api,graphql}` → `internal/issuetracker/{...}`, then:**
- **Rewrite import paths** `github.com/cdelezenski/newlinear/internal/X` → `cmd184psu/unified-webapp/internal/issuetracker/X`.
- **`db/`**: keep `db.go` (WAL, `SetMaxOpenConns(1)`, `//go:embed schema.sql`) verbatim. **`schema.sql`**: leave `api_tokens` table inert (O3a).
- **`store/`**: **remove** `EnsureToken`/`ValidToken` (`store.go:269–291`). Keep `EnsureUserByUsername` (find-or-create; harden to upsert on `username` unique — pre-mortem #4). Keep `Seed`; **add** seeding of the `api` user and the configured `default_user` (idempotent, via `EnsureUserByUsername`).
- **`api/`**: **delete** `auth_handlers.go`; drop `WithAuth`, the `Auth/Mode/CookieSecure` fields, and the whole `if a.Auth != nil {…}` auth-route block (`api.go:56–66`), plus routes `/api/me`, `/api/token`, `/api/health` (gate owns `/healthz`), and the `token` field from `bootstrap` (`handlers.go:22,28`). **Repoint** `currentUser` (`handlers.go:224`) at the actor resolver below. `New(st, defaultUser)` drops the auth deps but gains the `DefaultUser` config (needed by `currentActor`); `build.go` passes `cfg`/`cfg.DefaultUser` through.
- **Identity/attribution (FR5)** — new `actor.go` in `api/` (and shared with graphql):
  ```
  // ensure(username, display) == Store.EnsureUserByUsername(username, display)
  // default() == ensure(cfg.DefaultUser.Email, cfg.DefaultUser.Name)
  currentActor(r) *models.User:
    p, ok := auth.PrincipalFromContext(r.Context())   // platform auth
    if !ok { return default() }                       // open mode
    switch p.Method {
      case "apikey":            return ensure("api", "API")
      case "ldap","passkey":    if p.Subject!="" { return ensure(p.Subject, p.Subject) }
    }
    return default()
  ```
  `createIssue`: always `b.ReporterID = &currentActor(r).ID` (**no** client fallback — divergence from reference, per FR5). `updateIssue`: `ReporterID = nil` (immutable — unchanged). `assignee=me` filter (`handlers.go:152`) resolves via `currentActor`.
  > **Reconciliation note:** the reference `auth.Principal` exposed `{Username, DisplayName}`; the platform `Principal` is `{Method, Subject}`. Use `Subject` as both username and display name (the gate's session claims carry no display name). This is the single semantic adapter of the port.
- **`graphql/`**: **remove** `authorized()` and its call (`linear.go:34,77–82`) — the gate authenticates. **New behavior (not a port):** reference `issueCreate` (`linear.go:163–196`) sets **no** reporter — add reporter attribution via `auth.PrincipalFromContext` so GraphQL writes attribute the actor identically to REST (`nodes.go:41` already maps `creator`←`Reporter`). Switch `viewer` (`linear.go:93`) from token-identity to the actor. The `graphql.Handler` runs behind the gate on the module mux, so `r.Context()` carries the principal; `ServeHTTP` must thread `r` (not just vars) into `issueCreate`/`viewer`.
- **`internal/issuetracker/build.go` (new):** `func Build(cfg config.IssueTrackerConfig) (http.Handler, error)` — `db.Open(cfg.DBPath)` → `store.New` → `store.Seed` (+ api/default users) → `mux`: `api.New(st, cfg).Routes(mux)`, `mux.Handle("/graphql", graphql.New(st))`, `mux.Handle("/", static.NewHandler(cfg.StaticDir))`. No goroutines → no `io.Closer`. Return `(nil, err)` on any failure → dispatcher serves the sanitized 503.

**Tests (port/adapt reference tests):** store CRUD + relations inverse; graphql query smoke (no token); actor resolution across the 3 cases.
**Gate:** `go test ./internal/issuetracker/...` green.

### Phase C — Config + dispatcher integration (`docs/adding-a-module.md` steps 1–4)
- **`internal/platform/config/config.go`:** add `IssueTrackerConfig` with `StaticDir string \`json:"static_dir"\``, `DBPath string \`json:"db_path"\``, and a `DefaultUser` **struct** `{ Name, Email string }` (`json:"default_user"`) — two fields, because `users` rows carry name+email and `EnsureUserByUsername(username, displayName)` keys on a username (use `Email` as the stable username key, `Name` as display). Model the rest on `CertmachineConfig` (`StaticDir`+`DBPath`). Add `IssueTracker IssueTrackerConfig \`json:"issuetracker"\`` to `Config`; add `expandIssueTrackerPaths` (expand `StaticDir`, `DBPath` only) called in `Load` next to `expandTodoPaths`; add defaults in `DefaultConfig` (`static_dir: ./web/issuetracker`, `db_path: ./data/issuetracker/issues.db`, `default_user: {Name:"Unassigned", Email:"unassigned@localhost"}`). No SSE field (module has no broker). The `api` seed user is a fixed constant, not configurable.
- **`cmd/server/main.go`:** add `case "issuetracker": return issuetracker.Build(cfg.IssueTracker)` to `buildModule`; add `"issuetracker"` to `knownModules`. `limitFor` default (1 MiB) is fine — no entry needed.
- **Tests:** a `config_issuetracker_test.go` mirroring `config_certmachine_test.go` (load/expand/default); a `cmd/server/dispatch_test.go` case if the pattern exists for other modules.
**Gate:** `go build ./... && go test ./internal/platform/config/... ./cmd/server/...` green.

### Phase D — Frontend port (mirror `smbedit`)
- Copy `reference/issue-tracker/frontend/src/*` → `web/issuetracker/src/`, **excluding** `AuthContext.tsx`, `LoginPage.tsx`, `passkey.ts`. Copy `index.html`, `styles.css`.
- **Edit `DataContext.tsx`:** remove `useAuth`/`AuthContext` import; source the current actor from `GET /api/auth/whoami` (`{authenticated,method,identity}`); `me` = the user whose identity matches, `userLabel` renders "Me" accordingly; drop `me` from bootstrap consumption where it came from `/api/me`.
- **Edit `App.tsx` / `AccountBar.tsx`:** remove `authenticated`/`LoginPage` gating and passkey UI; keep the rest of the shell.
- **Edit `api.ts`:** drop the `me()`/token calls; API base becomes same-origin relative (`/api`, `/graphql`); remove any `Authorization` token header.
- **Rework `ApiPage.tsx`:** document platform API-key access to `/graphql` (`Authorization: Bearer <key>` / `X-API-Key`); **no** token display or fetch.
- **`index.html`:** reference `js/bundle.js` + `js/bundle.css` (like smbedit), a `<div id="root">`.
- **`package.json`:** add `react-router-dom` to `dependencies` (react/react-dom/@types already present); append two esbuild entries (build + build:dev) for `web/issuetracker/src/main.tsx --bundle --jsx=automatic --define:process.env.NODE_ENV --outfile=web/issuetracker/js/bundle.js` (copy smbedit's flags).
- **`tsconfig.json`:** add `web/issuetracker/src/**/*.ts`, `**/*.tsx` to `include`.
**Gate:** `npm run build` produces the bundle; `npm run typecheck` passes; manual SPA load + a `/issue/TEAM-123` deep link resolve.

### Phase E — Example config, ignore, docs
- **`unified-webapp-example.json`:** add an `issuetracker` config section; a `host_routing` entry (`"issuetracker-test.cmdhome.net": "issuetracker"` + `localhost`); an `auth.modules` entry `"issuetracker": ["ldap","passkey"]` (protected posture per FRD) — plus note the open-mode smoke-test variant.
- **`.gitignore`:** add `reference/` and `data/issuetracker/`.
- **`README.md` / `docs/`:** add issuetracker to the module enumeration.

### Phase F — Full verification (the acceptance gate)
Run all: `make build` · `make build-rpi` (CGO-free) · `make test` (`go test -race ./...`, goleak) · `make web` · `npm run typecheck`. Then the FRD acceptance-criteria curl flows (below).

---

## Expanded test plan (deliberate)

- **Unit:** store CRUD/relations/seed (incl. api+default users); graphql query smoke (no token); `PrincipalFromContext`/`WithPrincipal`; `whoami` JSON for apikey/session/anon; config load/expand/default.
- **Integration (module through the real gate):** unauth `/api/*` and `/graphql` → `401 {"error":"unauthorized"}`; `Authorization: Bearer <key>` → issue nodes; browser `GET /` on protected → platform login page; reporter attribution = `api` (key) / ldap-user (session) / default_user (open) across REST **and** GraphQL `issueCreate`; SPA fallback resolves a deep link; `assignee=me` returns the actor's issues.
- **e2e (FRD acceptance):** the two curl scenarios verbatim (open-mode no-token graphql; protected 401→Bearer flow) + `whoami` in each mode.
- **Observability:** unwritable `db_path` → `Build` returns error → dispatcher serves `503 {"error":"module unavailable","module":"issuetracker"}` with cause in boot log only (`unavailableHandler`); no secret/token ever logged (token layer gone).
- **Regression:** existing `internal/platform/auth` + `cmd/server` + every other module's tests unchanged and green (proves FR6 decision-neutrality).

---

## Acceptance criteria (testable checklist → FRD)
- [ ] `make build`, `make build-rpi`, `make test`, `make web`, `npm run typecheck` all pass.
- [ ] Open mode: UI loads; issues/stories/epics/tags CRUD; Kanban renders; deep links resolve; `curl -X POST …/graphql` (Linear query) returns issue nodes with no token.
- [ ] Protected mode: unauth `/api|/graphql` → 401; `Bearer <key>` → issue nodes; browser `GET /` → login page.
- [ ] `GET /api/auth/whoami`: `{authenticated:false}` open; ldap username after login; api-key identity for Bearer.
- [ ] Reporter = api / ldap-user / default_user by mode; reporter immutable on update; assignee editable; `assignee=me` works.
- [ ] Changing `default_user` in JSON + restart re-points new-issue reporter; existing issues unchanged.
- [ ] Unwritable `db_path` → sanitized 503, cause in boot log only.
- [ ] Other modules' behavior untouched (regression suite green).

---

## ADR
- **Decision:** Port newlinear as `internal/issuetracker` with reference subpackages; strip its auth/CORS/token/Vite layers; add a minimal, decision-neutral platform capability (context principal + `whoami`) to feed FR5 attribution; mirror `smbedit` for the React/esbuild frontend.
- **Drivers:** gate blast radius; CGO-free cross-compile; convention alignment (utuber/smbedit/certmachine precedents).
- **Alternatives considered:** flatten packages (O1b), keep Vite (O2b), drop `api_tokens` (O3b), header-based identity instead of context (rejected in FRD Rev 3 — spoofable, worse for in-process).
- **Why chosen:** smallest correct diff that satisfies the FRD; leans on proven in-repo patterns; isolates the one risky change behind its own tested phase.
- **Consequences:** one inert DB table; a single semantic adapter (`Subject`↔`Username`, no display name); the module now depends on the platform exposing a principal (a new, small platform contract).
- **Follow-ups:** if per-user open-mode attribution is ever needed, protect the module (out of scope); consider carrying a display name in session claims if UX needs richer names.

## Risk register
| Risk | Sev | Mitigation |
|---|---|---|
| Gate edit regresses existing-module auth | High | Surgical, decision-neutral edits; before/after full auth+cmd/server suites; explicit allow/deny regression tests |
| CGO/`build-rpi` breakage | High | Only `modernc.org/sqlite`; no ldap/webauthn/bcrypt in module; `CGO_ENABLED=0 make build-rpi` gate; `go mod tidy` |
| React-router/esbuild bundle issues | Med | Copy smbedit flags; add `react-router-dom`; typecheck+manual deep-link |
| `EnsureUserByUsername` write race | Low | upsert on unique `username`; single-writer SQLite |
| `go.sum` version skew (ref pins vs root) | Med | Build against root versions; tidy; do not vendor reference go.mod |
| Frontend fidelity loss (no display name) | Low | Documented adapter; acceptable per FRD |
