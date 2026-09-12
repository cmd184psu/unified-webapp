# Plan: `issuetracker` module for unified-webapp

**Status: pending approval**

Source FRD: [`docs/frd-issue-tracker.md`](./frd-issue-tracker.md)
Target executor: `ralph` (sequential execution agent)
Repo: `/opt/unified-webapp-issues` (branch `main`)

---

## RALPLAN-DR Summary

### Principles

1. **Port, do not redesign.** Behavior, schema, routes, and UI stay as-is. The only
   changes are the wiring seams: config source, handler construction, build pipeline.
2. **Conform to the host repo, not the source repo.** Where the two disagree
   (Vite vs esbuild, env vars vs JSON config, per-project `go.mod` vs root module),
   unified-webapp's convention wins.
3. **Additive blast radius on shared files.** Exactly ten shared files may be modified:
   `.gitignore`, `go.mod`, `go.sum`, `package.json`, `package-lock.json`, `tsconfig.json`,
   `cmd/server/main.go`, `internal/platform/config/config.go`,
   `unified-webapp-example.json`, `README.md` — the same ten Step 8's regression gate
   enumerates, and the authoritative list is the one there. They get **new entries only: no
   hunk may remove or alter a line belonging to an existing module.** That is the rule, and
   it is *not* the stronger "every hunk is a pure addition" — seven of the ten are modified
   in place by design (JSON trailing commas, extended npm script strings, `go mod tidy`'s
   direct/indirect moves; see Step 8's table). `internal/platform/middleware` is not touched
   at all, and neither is `unified-webapp.json`.
4. **Fail loud at `Build()` time.** A bad `static_dir` or unopenable DB must return an
   error so the dispatcher serves an explicit 503 for that hostname, never a silent 404.
5. **Every step ends in a command that proves it.** Ralph must not advance on
   "looks right"; each step below names its verification.

### Decision Drivers (top 3)

| # | Driver | Why it dominates |
|---|--------|------------------|
| D1 | **CGO-free single binary, cross-compilable to `linux/arm64`** | `make build-rpi` is an acceptance gate. Any dep that needs cgo, or a build that leaks a host-arch assumption, fails the port outright. Constrains the SQLite driver choice (already satisfied: `modernc.org/sqlite`) and forces a `CGO_ENABLED=0` check at every build step. |
| D2 | **No per-module node project, no Vite, no runtime Node** | The repo builds all TypeScript from one root `npm run build` (esbuild) and **commits the bundle output** (`web/multissh/js/bundle.js` is tracked). The reference frontend's entire build system must be discarded and re-expressed as one esbuild invocation. |
| D3 | **Other modules' behavior must be provably untouched** | Five modules ship from this binary. Shared-file edits are the only place this port can regress unrelated code, so they are constrained to append-only and gated by the existing test suite. |

### Viable Options

#### Decision 1 — Frontend build system

| Option | Pros | Cons |
|---|---|---|
| **A. Root esbuild bundle** *(CHOSEN)* | Matches D2 (TS sources in `web/<mod>/js/`, committed bundle, `css.d.ts` shim). One line added to `package.json` `build`/`build:dev`. `make web` and `npm run typecheck` cover it for free. No new toolchain. | Loses Vite HMR. esbuild does no type-checking while bundling — types are enforced separately by `tsc --noEmit`, so a type error will not fail `npm run build`. **Vite supplied two production defaults that esbuild does not, and both must be re-specified by hand or the port silently ships a worse artifact:** `process.env.NODE_ENV` (esbuild defaults browser bundles to `"development"` unless minifying — measured 1,263,149 bytes shipping `react.development.js`, vs 414,087 with the define alone and **210,305 with define + minify, which is what actually ships** — plus `React.StrictMode` double-invoking all 10 `useEffect` call sites across 9 files, a real behavior delta under "port, do not redesign"), and absolute asset hrefs in `index.html` (see the convention override below). Also requires `--jsx=automatic` and the CSS-import shim. |
| B. Keep Vite in a `web/issuetracker/` sub-project | Zero frontend changes; HMR preserved; `tsc -b && vite build` already known-good; the two defaults above come for free. | Directly violates D2 and FRD §5 ("The reference Vite/per-module npm setup is dropped"). Introduces a second lockfile, a second `node_modules`, and a build step neither `make web` nor `npm run typecheck` reaches. Rejected. |

**Chosen: A.** The reference frontend is unusually friendly to this swap — the audit found
**no `import.meta.env` / `VITE_*` usage, no path aliases, and exactly one non-JS import**
(`import "./styles.css"` in `main.tsx`). `vite.config.ts` only configures the React plugin
and a dev-server proxy for `/api` + `/graphql`, both of which are meaningless once the SPA
is served same-origin by the Go module. So option B buys nothing that A does not already have —
*provided* the two lost defaults are re-specified explicitly, which Steps 5.3 and 5.4 do.

#### Convention override — `issuetracker` diverges from `multissh` on two frontend details

Principle 2 says the host repo's convention wins. That principle is a shortcut for
"the host repo already solved this"; where it demonstrably did not, the convention is
evidence, not a rule. `web/multissh/` set both conventions below while having **no
client-side routing and no React**, so neither transfers:

| Detail | multissh | issuetracker | Why |
|---|---|---|---|
| Asset hrefs in `index.html` | relative (`js/bundle.js`) | **absolute (`/js/bundle.js`)** | issuetracker is the only module with history-API routing. On a hard refresh of `/issue/ENG-1` the document base is `/issue/`, so a relative `src` resolves to `GET /issue/js/bundle.js`, which the SPA fallback answers with `index.html` (200, `text/html`) — the browser then executes HTML as JavaScript and the page is blank. The reference's own Vite-built `dist/index.html` uses `/assets/…` for exactly this reason. multissh never hits it because it never changes the URL. |
| Production defines / minify | neither | **both** | Same cause: multissh's bundle has no framework with a dev/prod split. React does, and the difference is 3× the bytes plus a behavioral delta. |

Both overrides are stated here so they read as deliberate rather than as drift, and both
are pinned by automated test rows in Step 6.4 — the failure mode of the first is a blank
page that no build gate would otherwise catch.

#### Decision 2 — Backend package layout

| Option | Pros | Cons |
|---|---|---|
| **A. Keep the subpackage tree** under `internal/issuetracker/{db,models,store,api,auth,graphql}` + a new `build.go` in `package issuetracker` *(CHOSEN)* | Mechanical port: copy files, rewrite one import prefix, done. Preserves the reference's internal boundaries so future upstream diffs stay applicable. Keeps ~3.7k lines out of one flat package. `internal/multissh/sshproxy` is an existing in-repo precedent for a module with a subpackage. | Slightly deeper tree than `todo`/`grocery` (which are single-package). Six extra directories. |
| B. Flatten everything into `package issuetracker` | Matches the shape of the smaller modules exactly. Fewer directories. | Requires renaming across ~19 files to resolve collisions — three separate functions named `New` (`store.New`, `api.New`, `graphql.New`) would have to become three different identifiers, and their call sites with them — which turns a copy-and-rewrite into a hand edit of every file — the highest-risk way to do a "clean port". Destroys diffability against the source. Rejected. |

**Chosen: A.** The import rewrite is a single deterministic substitution
(`github.com/cdelezenski/newlinear/internal/` → `cmd184psu/unified-webapp/internal/issuetracker/`),
which is verifiable by `go build` rather than by reading.

#### Decision 3 — Auth

| Option | Pros | Cons |
|---|---|---|
| **A. Port auth now, driven by the new config section** *(CHOSEN)* | FRD §3 requires it. Default mode is already `"none"` (`auth.FromEnv` returns a pass-through `NoAuth` provider when `NEWLINEAR_AUTH_MODE` is unset), so the default deployment is login-free and the smoke test needs no LDAP. The frontend already ships `LoginPage.tsx` / `AuthContext.tsx` / `passkey.ts`, so a stub would leave that UI permanently inert. | Adds two dependencies (`go-webauthn`, `go-ldap`) whose non-`none` paths cannot be exercised in this environment. ~13 env vars must be re-expressed as nested JSON. |
| B. Stub auth to `mode: "none"` only; defer LDAP/passkey | Drops two dependencies and ~800 lines. Smaller review surface. | Violates FRD §2 feature parity and §3 outright — the module would ship with no way to turn authentication on. Creates a second port later, with the config schema already frozen the wrong way. Rejected. |

**The decision rests on FRD §3, which requires the auth surface — not on a "dead UI"
argument**, which does not survive inspection: `api/api.go:56` already registers
`/api/auth/*` only `if a.Auth != nil`, so in `mode: "none"` those routes are absent by
design in the reference too, and the frontend is built to cope with that. What a stub
would actually cost is a second port later, against a config schema already frozen the
wrong way.

**Chosen: A**, with the honest caveat recorded in Risks: only `mode: "none"` is
reachable by automated verification here. `ldap` / `ldap_passkey` are ported
faithfully and compile-checked, but their runtime behavior is untested — an accepted,
explicitly-scoped gap, not an oversight.

### Decision Record (ADR-condensed)

- **Decision:** Add `internal/issuetracker/` as a subpackage tree, config-driven
  (`IssueTrackerConfig` with a nested `auth` section), frontend re-homed to
  `web/issuetracker/` and bundled by root esbuild with the output committed.
- **Drivers:** D1 CGO-free arm64 cross-build · D2 no per-module node project ·
  D3 zero regression in other modules.
- **Alternatives considered:** keep Vite (D2 violation); flatten backend packages
  (high-touch rename, kills diffability); stub auth (FRD violation, dead UI).
- **Why chosen:** it is the lowest-edit path that satisfies every acceptance gate;
  each rejected alternative either breaks a stated requirement or converts a
  mechanical port into a manual rewrite.
- **Consequences:** frontend loses HMR; the committed bundle must be rebuilt and
  re-committed on every frontend change; `ldap`/`ldap_passkey` modes ship unverified;
  `go.mod` gains ~20 indirect dependencies and the static binary grows ~10–15 MB;
  `issuetracker` deviates from `multissh` on asset hrefs and production defines
  (rationale above); `db.SetMaxOpenConns(1)` serializes this module's requests inside a
  binary shared with five other modules.
- **Follow-ups:** see [Open Questions](#open-questions).

---

## Pre-flight facts ralph should treat as given

These were established by reading the code; do not re-derive them.

- `cmd/server/main.go` → `buildModule(module, cfg)` is a `switch` on module name
  returning `internal/<module>.Build(cfg.<Module>)`. A module that returns an error gets
  an `unavailableHandler` that 503s — so `Build` should error rather than degrade.
- `internal/todo/build.go` is the canonical shape: construct store → `mux := http.NewServeMux()`
  → register API routes → `mux.Handle("/", &staticHandler{dir: cfg.StaticDir})`.
- `internal/platform/middleware.Wrap` **already applies permissive CORS to every module**
  (`Allow-Origin: *`, `Allow-Headers: Content-Type`) at the dispatcher level.
  The reference's own `cors()` helper in `backend/main.go` is therefore **not ported**
  (FRD §5 forbids it) and `middleware.Wrap` is **not modified** (D3).
- The reference API already uses Go 1.22+ `ServeMux` method+wildcard patterns
  (`"GET /api/issues/{id}"`). No third-party router is involved; `go 1.26.2` serves them natively.
- `web/multissh/` is the frontend precedent for *layout*: TS sources live under
  `web/<mod>/js/`, `bundle.js` + `bundle.css` are **committed to git**, and `js/css.d.ts`
  contains `declare module "*.css";` so `tsc` tolerates the CSS import. It is **not** the
  precedent for asset hrefs or build flags — see the convention override above.
- `internal/multissh/server.go` (`staticHandler`, `staticFileExists`, `noDirList`,
  `onlyGet`, `writeError`) is the repo's hardened static handler, and its contract is
  already pinned by `internal/multissh/static_test.go`. It **already contains** the
  `strings.HasPrefix(r.URL.Path, "/api/")` JSON-404 guard this module needs, with the
  trailing slash correct. `internal/todo/build.go`'s `staticHandler` is a different,
  weaker handler (bare `os.Stat` + `http.ServeFile`) that lists directories and serves
  `index.html` for any method. Step 4.2 adopts the multissh one; do not mix them.
- Root `tsconfig.json` has **no `jsx` option** and `lib` lacks `DOM.Iterable`; the
  reference tsconfig sets `"jsx": "react-jsx"` and includes `DOM.Iterable`.
- Root `tsconfig.json` sets `moduleResolution: "bundler"`, which **implies
  `allowSyntheticDefaultImports`**. That is why `import React from "react"` and
  `import ReactDOM from "react-dom/client"` typecheck without further changes.
  If a default-import error ever appears here, the fix is not to add `esModuleInterop`
  to this shared file — that would change emit semantics for every module.
- `frontend/src/App.tsx` declares **13 client routes** plus a `path="*"` catch-all that
  redirects to `/issues`: `/` (→`/issues`), `/issues`, `/board`, `/my-issues`,
  `/my-board`, `/issue/:ident`, `/stories`, `/stories/:id`, `/epics`, `/epics/:id`,
  `/projects`, `/tags`, `/api`.
- `frontend/src/main.tsx` wraps the app in `<React.StrictMode>`. This is inert in a
  production React build and double-invokes effects in a development one — which is why
  Step 5.4's `--define` is a parity requirement, not a size optimization.
- `go env GOMODCACHE` already holds `modernc.org/sqlite`, `go-webauthn`, and `go-ldap`
  at nearby versions, and both `proxy.golang.org` and `registry.npmjs.org` are reachable.
  `node_modules/` is absent and gitignored; `package-lock.json` **is** tracked.
- `internal/multissh/build_test.go` is the only current `goleak` user, and it is
  package-scoped — it cannot observe goroutines from the issuetracker package.

Five findings from the source audit that the steps below depend on:

- **The SPA has a client route `/api`** (the API-docs page, `ApiPage.tsx`), while the REST
  API lives at `/api/<something>`. The Go mux registers no pattern for the *exact* path
  `/api`, so it must fall through to `index.html`. Any "unmatched API path" guard must
  therefore key on the prefix `"/api/"` **with the trailing slash** and deliberately let
  bare `/api` through. See Step 4.2 — this is the single easiest way to break this port.
- **`auth.isPublic` (in `auth/middleware.go`) matches literal absolute paths**:
  `HasPrefix(p, "/api/auth/") || p == "/api/health" || p == "/graphql" ||
  (!HasPrefix(p, "/api/") && (GET || HEAD))`. Host-header mounting preserves paths
  verbatim, so this works unchanged. It would break badly under a *subpath* mount
  (login endpoints would become unreachable and API GETs would bypass auth entirely) —
  which is one more reason this module must stay host-routed.
- **`/graphql` always answers HTTP 200**, even for auth failures, returning a GraphQL
  `errors` array. Tests and smoke checks must assert on the body, not the status code.
- **`GET /api/token` is unauthenticated in `mode: "none"`**, and `GET /api/bootstrap`
  also returns the token. Combined with the platform's `Allow-Origin: *`, that makes the
  GraphQL **write** token readable cross-origin by any page in the operator's browser.
  This is *not* the same posture the README documents for `multissh` — see **R9/OQ-5**,
  and do not restate it as "reaching the hostname is the access boundary".
- **The reference has zero `*_test.go` files.** There is nothing to port in Step 6;
  every test is written fresh.

---

## Steps

Each step is independently verifiable. Ralph must run the verification and see it pass
before starting the next step. Do not batch steps.

### Working-directory contract

**Every command in every step runs from the repo root, `/opt/unified-webapp-issues`.**
All relative paths (`./unified-webapp`, `web/issuetracker/…`, `./data/issuetracker/…`)
are relative to it. The one exception is Go test code, whose cwd is its own package
directory — which is why the Step 6.4 `index.html` row carries an explicit `../../` path.

### Commit policy

Steps 4 and 8 rely on git state, so the policy has to be stated rather than assumed.

- **Commit at the end of each step**, after its verification passes. This is what makes
  "recovery = rewind to the previous step" meaningful, and it is what Step 8's
  `git diff --stat $BASE` reads — **untracked files never appear in `git diff`**, so an
  uncommitted port would make that regression gate silently vacuous.
- **Never `git add -A`.** The working tree carries untracked paths that must stay
  untracked: `.omc/` (ralph's own state directory), `docs/frd-issue-tracker.md`,
  `docs/plan-issue-tracker.md`, and — until Step 1.2 gitignores it — `reference/`.
  Enumerate the paths the step touched, e.g.
  `git add internal/issuetracker web/issuetracker package.json package-lock.json` then
  `git commit -m "issuetracker: step N — <summary>"`.
- **Recovery for step N**, unless a step states otherwise, is `git reset --hard HEAD`
  (discarding that step's uncommitted work) plus `git clean -fd <the new paths>` for
  anything untracked the step created. The per-step **Recovery** lines below are the
  explicit form of that, written so they cannot reach back past the previous step's
  commit.

---

### Step 1 — Housekeeping, dependencies, and a green baseline

**Files touched:** `.gitignore`, `go.mod`, `go.sum`

1. **Capture the baseline as a revision, not just as output.** Record
   `git rev-parse HEAD` into the working notes as `$BASE` — Step 8's regression gate
   diffs against it, and every step's recovery path rewinds to it. Then confirm the tree
   is green on all five gates, not just the Go ones:
   ```
   git rev-parse HEAD                                   # -> $BASE
   make build && make test
   make web && npm run typecheck
   make build-rpi && ls -l unified-webapp-arm64-linux    # record the byte count
   git status --porcelain -- web/ package-lock.json      # must print NOTHING
   ```
   All five gates pass on `main` today. If any is red, stop and report — do not start a
   port on a broken baseline, because every later "still passes" claim would be
   unfalsifiable.

   **Two of these five lines exist only to make a *later* gate falsifiable, and both were
   added because the later gate was otherwise measuring nothing:**
   - `make build-rpi && ls -l` supplies the arm64 binary-size **"before"**. Step 4 compares
     the post-port binary against it; without a number captured here that comparison has no
     left-hand side. Record the byte count alongside `$BASE` in `.omc/` — it is as
     unrecoverable after the fact as the SHA is.
   - The `git status` scope must be `-- web/ package-lock.json`, matching Step 8.11 exactly.
     A narrower `-- web/` would let pre-existing lockfile drift surface for the first time at
     Step 8 and be charged to the port. This matters concretely: Step 1's `make web` runs
     `npm install` when `node_modules/` is absent (Makefile:25-27), which it is on a fresh
     clone, and `npm install` can renormalize `package-lock.json`.

   **Write `$BASE` down.** It is needed once, at Step 8, many steps later, and it cannot be
   recovered afterwards by inspection. Record it in ralph's working notes under `.omc/`
   (which is untracked and stays that way — see the commit policy) as a line like
   `BASE=<sha>`. Do not rely on a shell variable: per the single-shell contract below,
   variables do not survive between tool calls.

   **The `git status` line is the one that is easy to skip and expensive to lose.** Step 5 asserts
   that `make web` leaves the three pre-existing bundles byte-identical, and Step 8.11
   asserts a clean `web/` after a fresh `make web`. Both presuppose that today's committed
   bundles are exactly what today's esbuild reproduces — plausible, but unmeasured until
   this command is run. If it prints anything *now*, the committed bundles are already
   stale and that is a pre-existing condition to report, **not** something to fix inside
   this port: committing someone else's regenerated bundle would be a D3 violation
   disguised as tidiness.
2. Append `reference/` to `.gitignore` (FRD §8). Confirm with `git status --porcelain`
   that `reference/` no longer appears as untracked.
3. Add the three dependencies:
   ```
   go get modernc.org/sqlite@v1.52.0
   go get github.com/go-webauthn/webauthn@v0.17.4
   go get github.com/go-ldap/ldap/v3@v3.4.13
   ```
   **Do not run `go mod tidy` in this step.** Nothing imports these packages yet, so
   `go get` files them under `// indirect` and `tidy` would then delete all three outright —
   leaving Step 2 unable to build with `no required module provides package
   modernc.org/sqlite`. `tidy` belongs in Step 2, after the import rewrite gives the
   requirements a reason to exist.
   Versions come from `reference/issue-tracker/backend/go.mod`. If a pinned version cannot
   be fetched, the only acceptable substitution is **the nearest higher patch within the
   same minor** (e.g. `sqlite@v1.52.x`) — a minor or major bump is a plan change, not an
   implementation detail, and must be reported rather than absorbed. Record any
   substitution in the commit message.
   Note that `golang.org/x/crypto` and `golang.org/x/sys` already exist in the root
   `go.mod` at **higher** versions than the reference pins (`v0.53.0`/`v0.46.0` vs
   `v0.52.0`/`v0.45.0`) — MVS keeps the higher ones. Do **not** downgrade them.

**Verify:**
- `go build ./...` succeeds and `make test` still passes (unchanged from baseline).
- `git diff go.mod` shows the three new modules present **in the `// indirect` block** and
  no removals. Their landing as indirect is the expected state here, not a defect —
  Step 2 promotes them.
- No D1 claim is made at this step: nothing imports SQLite yet, so a `CGO_ENABLED=0`
  build here would pass no matter what the dependency turned out to be. The real
  cross-compile gate is in Step 2.

**Recovery:** `git checkout -- .gitignore go.mod go.sum`.

---

### Step 2 — Port the backend packages

**Files touched (all new):** `internal/issuetracker/{db,models,store,api,auth,graphql}/**`

1. Copy, preserving structure, from `reference/issue-tracker/backend/internal/` into
   `internal/issuetracker/`:
   - `db/db.go`, `db/schema.sql`
   - `models/models.go`
   - `store/{store.go,issues.go,folders.go,seed.go}`
   - `api/{api.go,handlers.go,auth_handlers.go}`
   - `auth/{auth.go,config.go,ldap.go,middleware.go,service.go,store.go}`
   - `graphql/{linear.go,nodes.go}`
   Do **not** copy `backend/main.go`, `backend/bin/`, `backend/newlinear.db*`,
   `backend/go.mod`, or `backend/go.sum`.
2. Rewrite every import path with the single substitution
   `github.com/cdelezenski/newlinear/internal/` → `cmd184psu/unified-webapp/internal/issuetracker/`.
   Nothing else in the import blocks changes.
3. Leave `//go:embed schema.sql` as-is — it resolves relative to the new package directory.
4. **Do not yet delete `auth.FromEnv`.** Step 4 replaces it; keeping it compiling here keeps
   this step's verification honest and independent.
5. Now that the packages are imported, run `go mod tidy` (deferred from Step 1). This is
   the point at which the three modules acquire a real importer and move from the
   `// indirect` block to the direct `require` block.

**Verify** — in this order; the tidy check must come *before* the build gate, because a
wrong order here is what makes the failure look like a porting bug instead of a
dependency-graph bug:
- `go mod tidy` completes, and `git diff go.mod` now shows `modernc.org/sqlite`,
  `github.com/go-webauthn/webauthn`, and `github.com/go-ldap/ldap/v3` **promoted to the
  direct `require` block** (no `// indirect` marker), alongside the new indirect closure.
- `go build ./internal/issuetracker/...` succeeds.
- `CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build ./internal/issuetracker/...` succeeds.
  **This is the first gate that actually tests D1** — it is the earliest moment the
  pure-Go SQLite driver is both required and compiled for the target platform. A failure
  here invalidates the dependency choice, not the wiring; stop and report rather than
  working around it.
- `go vet ./internal/issuetracker/...` is clean.
- `grep -rn "cdelezenski/newlinear" internal/` returns nothing (all 15 import occurrences
  are plain strings, so a single `sed` over the tree is sufficient and verifiable).
- `grep -rn "os.Getenv" internal/issuetracker/` returns hits **only** in
  `auth/config.go` — if it hits anywhere else, that env read must also move to config
  in Step 3, and the plan's config schema is incomplete. Report it rather than improvising.

**Recovery:** `rm -rf internal/issuetracker && git checkout -- go.mod go.sum` — **only valid
because Step 1 was committed.** `git checkout --` restores from the index/HEAD, so with
Step 1 committed it rewinds `go.mod` to the state that *has* the three `// indirect`
requires. If Step 1 was not committed, the same command reverts the `go get` results too
and recreates the exact `no required module provides package modernc.org/sqlite` failure
Step 1.3 exists to prevent — in that case re-run Step 1.3's three `go get` lines before
retrying. Verify with `grep -c "modernc.org/sqlite" go.mod` after recovering: it must be 1,
not 0.

---

### Step 3 — Config: `IssueTrackerConfig` and the auth settings struct

**Files touched:** `internal/platform/config/config.go`, `unified-webapp-example.json`,
`internal/issuetracker/auth/config.go`

1. In `internal/platform/config/config.go`, **append** (never reorder existing fields):
   - `IssueTracker IssueTrackerConfig \`json:"issuetracker"\`` on `Config`.
   - The struct tree below.
   - Defaults in `DefaultConfig()`.
   - `expandIssueTrackerPaths(&cfg.IssueTracker)` called from `Load()` alongside the
     other `expand*Paths` calls.
2. Target JSON shape (mirrors every `NEWLINEAR_*` variable read by `auth.FromEnv`,
   with the reference's own defaults preserved verbatim):
   ```json
   "issuetracker": {
     "static_dir": "./web/issuetracker",
     "db_path": "./data/issuetracker/issues.db",
     "auth": {
       "mode": "none",
       "session_ttl_minutes": 720,
       "idle_ttl_minutes": 240,
       "cookie_secure": false,
       "ldap": {
         "url": "", "start_tls": false, "insecure_tls": false,
         "base_dn": "", "user_search_base": "", "user_search_filter": "",
         "group_search_base": "", "required_groups": [],
         "bind_dn": "", "bind_password_file": ""
       },
       "passkey": {
         "store_path": "./data/issuetracker/passkeys.json",
         "rp_id": "localhost",
         "rp_display_name": "newlinear",
         "origins": ["http://localhost:8080"],
         "allow_registration": true
       }
     }
   }
   ```
   Defaults must match `auth.FromEnv` exactly: mode `none`, session TTL 720 min,
   idle TTL 240 min, cookie-secure false, RPID `localhost`, RP name `newlinear`,
   origins `["http://localhost:8080"]`, allow-registration true. The only intentional
   change is `passkey.store_path`, which moves from the reference's cwd-relative
   `newlinear-passkeys.json` into `./data/issuetracker/` so it lands beside the DB.

   As Go, in `internal/platform/config/config.go` — TTLs are **`int` minutes**, not
   `time.Duration`, so the JSON stays human-editable and the `time.Minute` multiply
   happens in `auth.FromSettings` exactly where `FromEnv` did it:
   ```go
   type IssueTrackerConfig struct {
       StaticDir string                 `json:"static_dir"`
       DBPath    string                 `json:"db_path"`
       Auth      IssueTrackerAuthConfig `json:"auth"`
   }

   type IssueTrackerAuthConfig struct {
       Mode              string                    `json:"mode"`
       SessionTTLMinutes int                       `json:"session_ttl_minutes"`
       IdleTTLMinutes    int                       `json:"idle_ttl_minutes"`
       CookieSecure      bool                      `json:"cookie_secure"`
       LDAP              IssueTrackerLDAPConfig    `json:"ldap"`
       Passkey           IssueTrackerPasskeyConfig `json:"passkey"`
   }

   type IssueTrackerLDAPConfig struct {
       URL              string   `json:"url"`
       StartTLS         bool     `json:"start_tls"`
       InsecureTLS      bool     `json:"insecure_tls"`
       BaseDN           string   `json:"base_dn"`
       UserSearchBase   string   `json:"user_search_base"`
       UserSearchFilter string   `json:"user_search_filter"`
       GroupSearchBase  string   `json:"group_search_base"`
       RequiredGroups   []string `json:"required_groups"`
       BindDN           string   `json:"bind_dn"`
       BindPasswordFile string   `json:"bind_password_file"`
   }

   type IssueTrackerPasskeyConfig struct {
       StorePath         string   `json:"store_path"`
       RPID              string   `json:"rp_id"`
       RPDisplayName     string   `json:"rp_display_name"`
       Origins           []string `json:"origins"`
       AllowRegistration bool     `json:"allow_registration"`
   }
   ```
   `auth.Settings` (in `internal/issuetracker/auth/config.go`) is the parameter object
   `FromSettings` takes. It has **no JSON tags** and no import of `platform/config` —
   that separation is what keeps the `auth` package portable. Critically, it **reuses the
   two config types the `auth` package already defines** rather than mirroring them into
   new nested types:
   ```go
   package auth

   // Settings replaces the NEWLINEAR_* environment variables FromEnv used to read.
   // LDAPConfig (ldap.go) and PasskeyConfig (service.go) are the existing types,
   // reused verbatim — do not define parallel copies of them here.
   type Settings struct {
       Mode              string // "" | "none" | "ldap" | "ldap_passkey"
       SessionTTLMinutes int
       IdleTTLMinutes    int
       CookieSecure      bool
       LDAP              LDAPConfig
       Passkey           PasskeyConfig
       PasskeyStorePath  string // EnablePasskeys' first argument; not a PasskeyConfig field
   }
   ```
   `PasskeyStorePath` sits beside `Passkey` rather than inside it because the reference's
   `svc.EnablePasskeys(storePath, PasskeyConfig{…})` takes it as a separate argument —
   folding it into `PasskeyConfig` would edit a ported type, which Principle 1 forbids.
   `settingsFrom` in `build.go` (Step 4) does the field-for-field copy from
   `config.IssueTrackerConfig.Auth` into this struct.

   **`RequiredGroups` and `Origins` are `[]string` in JSON**, replacing `FromEnv`'s
   comma-separated `splitCSV` parsing. That is the one representation change in the
   translation; everything else is a like-for-like move.
3. `expandIssueTrackerPaths` must expand **four** paths: `static_dir`, `db_path`,
   `auth.ldap.bind_password_file`, `auth.passkey.store_path`. Follow the
   `expandMultisshPaths` pattern (which expands unconditionally, empty strings included).
4. In `internal/issuetracker/auth/config.go`, replace `FromEnv()` with
   `FromSettings(s Settings) (Setup, error)`. Delete the now-unused
   `env`/`envBool`/`envInt`/`splitCSV` helpers.

   **The env helpers were not just parsers — they carried defaulting logic, and deleting
   them silently deletes that logic.** `FromEnv`'s behavior must survive the translation
   in full. Seven rules, all of which need an explicit check in `FromSettings` now that
   there is no `env(key, def)` call to hide them:

   | Rule | Source in `FromEnv` | Required in `FromSettings` |
   |---|---|---|
   | **mode normalized with `strings.ToLower(strings.TrimSpace(…))` before any comparison** | `FromEnv` line 1 | **same — keep it.** Without it a JSON `"mode": "LDAP"` or `"none "` falls through to the invalid-mode error, and a config that reads correctly to a human fails at boot |
   | empty mode → `"none"` | `if mode == ""` (after normalization) | same |
   | mode ∉ `{none, ldap, ldap_passkey}` → error | explicit | same |
   | `ldap.url` empty in a non-`none` mode → error | explicit | same |
   | **`session_ttl_minutes` / `idle_ttl_minutes` ≤ 0 → 720 / 240** | `envInt` returns `def` when the value is unparseable **or non-positive** | explicit `if n <= 0` — a JSON `0` (the zero value of an omitted field) must become the default, not a zero TTL that expires every session instantly. **Belt-and-braces:** `auth.NewService` already defaults a non-positive `SessionTTL` to `12 * time.Hour` and `IdleTTL` to `4 * time.Hour` (reference `auth/service.go:58-65`) — the same 720/240. Keep the rule anyway so `FromSettings` is self-contained and the values are visible where they are configured |
   | **empty `origins` → `["http://localhost:8080"]`** | `if len(origins) == 0` | same — an omitted array must not become zero allowed origins, which would reject every WebAuthn ceremony |
   | **empty `rp_id` → `"localhost"`, empty `rp_display_name` → `"newlinear"`** | `env(key, def)` treats empty as absent | explicit empty-string checks |

   The last three are the dangerous ones: each turns an *omitted* JSON field into a
   working default under `FromEnv` and into a broken configuration under a naive struct
   copy. **How loudly they fail depends on the mode.** In `ldap_passkey`, rules 6 and 7 fail
   *at boot*: `EnablePasskeys` calls `webauthn.New`, which rejects empty `RPOrigins`
   (`must provide at least one value to the 'RPOrigins' field`) and an empty
   `RPDisplayName`, so the error propagates out of `Build` and the module 503s — visible
   immediately. That makes the risk smaller than an earlier draft of this plan claimed. Rule
   4 (`ldap.url`) likewise errors at boot. The genuinely silent one is the TTL rule, and
   `NewService` re-defaults that anyway (see the table). Treat the table as the
   specification regardless: a `FromSettings` that relies on a downstream re-default is one
   refactor away from being wrong.

   `build.go` (Step 4) translates `config.IssueTrackerConfig.Auth` → `auth.Settings` as a
   plain field-for-field copy. **Two auth fields default elsewhere and are deliberately not
   in the table above:** `allow_registration` and `passkey_store_path` take their values
   from `config.DefaultConfig()` (Step 3.3) rather than from `FromSettings`, because they
   are config-shape concerns rather than auth-semantics ones. So "all defaulting lives in
   `FromSettings`" holds for the seven auth *rules*, not for every auth-adjacent field —
   when changing either of those two, change `DefaultConfig()`.
5. Add the same `"issuetracker"` block to `unified-webapp-example.json`, and add
   `"issuetracker-test.cmdhome.net": "issuetracker"` to its `host_routing`. Adding a JSON
   object member means a **trailing comma on the previous entry** — an in-line
   modification, not a pure append. Step 8's regression gate expects it.

   **`unified-webapp.json` — the other tracked config — deliberately gains nothing.** It is
   the live deployment's config, not a template; the operator opts this module in by adding
   the block and a `host_routing` entry when they choose to run it. That is why it is absent
   from Step 8's allow-list of modifiable files: a diff touching it is a defect, not an
   oversight in this plan.

**Verify:**
- `go build ./...` succeeds.
- Both of these return **nothing** — and **`grep`'s exit status 1 IS the pass here**, so do
  not wrap either in a `|| exit 1`:
  ```
  grep -rn "os\.Getenv" internal/issuetracker/
  grep -rn 'Getenv("NEWLINEAR' internal/issuetracker/
  ```
  **Do not grep for the bare string `NEWLINEAR_`.** Step 3.2 *mandates* the doc comment
  "Settings replaces the NEWLINEAR_* environment variables FromEnv used to read" — that
  mention is intentional and must survive. A `NEWLINEAR_` grep fails on the very comment
  this plan requires; the two greps above target the *call*, which is what actually matters.
- Default-config check — **must target a path that does not yet exist**:
  ```
  go run ./cmd/server -init-config -config "$(mktemp -d)/cfg.json"
  ```
  then inspect the written JSON for `static_dir`, `db_path`, and the full `auth` tree.
  Do **not** use bare `make init-config`: the Makefile hardcodes
  `CONFIG := ~/.unified-webapp.json`, and `config.WriteDefault` returns `nil` without
  writing when the file already exists — so on any machine that has ever run the server,
  that check passes while verifying nothing.
- `go test ./internal/platform/...` passes.
- Round-trip check: a config file containing only `{"issuetracker":{"db_path":"~/x.db"}}`
  loads with `db_path` tilde-expanded **and** `static_dir` still at its default. This is the
  one behavior that silently breaks if the new field is added to `Config` but omitted
  from `DefaultConfig()`.
- **Defaulting check — a unit test in `internal/issuetracker/auth/config_test.go`,
  `package auth`.** Write it here rather than in Step 6; it is the *only* thing in this plan
  that pins rules 4–7.

  **It must live in `package auth`, and it must drive a non-`none` mode.** Both constraints
  come from the source and an earlier draft violated both:
  - `FromSettings` normalizes `""` → `"none"` and **returns immediately** at the `none`
    branch (reference `auth/config.go:21-27`), *before* any TTL, origin, or RPID defaulting.
    So `FromSettings(Settings{})` pins nothing — every rule after rule 2 is unreachable.
  - The defaulted values land in the unexported `Service.cfg` / `Service.pkcfg`; `Setup`
    exposes only `Provider`, `Service`, `CookieSecure`, `Mode`. An external test package
    cannot see them.

  Rows to assert:

  | Input | Assert |
  |---|---|
  | `FromSettings(Settings{Mode: "LDAP ", LDAP: LDAPConfig{URL: "ldap://x"}})` | no error; `Mode == "ldap"` (pins rule 1's `ToLower`+`TrimSpace` in one shot); `SessionTTL == 720 * time.Minute`; `IdleTTL == 240 * time.Minute`. Safe to run offline: `NewLDAPClient` stores the URL and does **not** dial. |
  | `FromSettings(Settings{Mode: "ldap_passkey", LDAP: LDAPConfig{URL: "ldap://x"}, PasskeyStorePath: t.TempDir() + "/pk.json"})` with `rp_id`, `rp_display_name`, `origins` all empty | no error; `RPID == "localhost"`; `RPDisplayName == "newlinear"`; `len(Origins) == 1`. This is the row that pins rules 6 and 7. |
  | `FromSettings(Settings{Mode: "bogus"})` | returns an error |
  | `FromSettings(Settings{Mode: "ldap"})` (empty `ldap.url`) | returns an error — rule 4 |

  Use `t.TempDir()` for the passkey store so the test writes nothing into the repo.
- Config-defaults check (distinct from the above, and much weaker): a config omitting the
  `auth` sub-keys still loads with `allow_registration` and `passkey_store_path` at their
  `DefaultConfig()` values. This exercises `config.Load`, not `FromSettings`.

**Recovery:** `git checkout -- internal/platform/config/config.go unified-webapp-example.json
internal/issuetracker/auth/config.go` — with Step 2 committed, this restores all three to
their Step 2 state (`auth/config.go` back to the `FromEnv` version) in one command. Then
`rm -f internal/issuetracker/auth/config_test.go`, which is new in this step and so is
untracked rather than restorable by `git checkout`.

---

### Step 4 — `internal/issuetracker/build.go` and dispatcher registration

**Files touched:** `internal/issuetracker/build.go` (new), `cmd/server/main.go`

1. Write `Build(cfg config.IssueTrackerConfig) (http.Handler, error)` in
   `package issuetracker`, reproducing `reference/issue-tracker/backend/main.go`'s wiring
   order exactly:
   ```go
   // every error is returned wrapped, so the dispatcher's unavailableHandler
   // reports the cause; every path after db.Open closes it before returning.
   if err := checkStaticDir(cfg.StaticDir); err != nil { return nil, err }
   if err := os.MkdirAll(filepath.Dir(cfg.DBPath), 0o755); err != nil {
       return nil, fmt.Errorf("issuetracker: create dir for db_path %q: %w", cfg.DBPath, err)
   }

   database, err := db.Open(cfg.DBPath)
   if err != nil { return nil, fmt.Errorf("issuetracker: open db: %w", err) }

   st := store.New(database)
   if err := st.Seed(); err != nil {
       database.Close()
       return nil, fmt.Errorf("issuetracker: seed: %w", err)
   }
   token, err := st.EnsureToken("default")
   if err != nil {
       database.Close()
       return nil, fmt.Errorf("issuetracker: ensure api token: %w", err)
   }
   authSetup, err := auth.FromSettings(settingsFrom(cfg.Auth))
   if err != nil {
       database.Close()
       return nil, fmt.Errorf("issuetracker: auth: %w", err)
   }

   mux := http.NewServeMux()
   api.New(st).WithAuth(authSetup.Service, authSetup.Mode, authSetup.CookieSecure).Routes(mux)
   mux.Handle("/graphql", graphql.New(st))
   mux.Handle("/", staticHandler(cfg.StaticDir))
   log auth mode and the API token
   return authSetup.Provider.Middleware(mux), nil
   ```
   **The `MkdirAll` wrap text is pinned, not illustrative.** `filepath.Dir` drops the
   filename, so the raw `*PathError` names only the *parent*
   (`mkdir /tmp/it-notadir: not a directory`). `cfg.DBPath` must therefore appear verbatim in
   the wrapped message, or **Step 4's negative gate and Step 6.4's `db_path` rows both fail
   against a correct implementation.** `unavailableHandler` copies `cause.Error()` verbatim
   (`cmd/server/main.go:138-148`), so this wording is the whole path by which the db path
   reaches the 503 body.

   **Every error is captured — including `Seed()` and `EnsureToken()`, which the earlier
   draft of this plan discarded.** That matters more than it looks: the reference calls
   `log.Fatalf` on a seed failure, and a dropped `EnsureToken` error yields an empty
   token, which makes `ValidToken("")` false, which makes `/graphql` reject every request
   — at HTTP 200, with an `errors` body — while `Build` reports success and the module
   serves happily. That is precisely the silent-degradation failure Principle 4 exists to
   prevent. Likewise `database.Close()` on each error path after `db.Open`: without it, a
   config that fails `auth.FromSettings` leaks the `*sql.DB` and its SQLite background
   goroutines for the life of the process.

   Three further points that are easy to get wrong and must be right:
   - **`Provider.Middleware` wraps the whole mux**, static files and `/graphql` included —
     that is what the reference does (`cors(authSetup.Provider.Middleware(mux))`). Its
     `isPublic` exemptions (`/api/auth/*`, `/api/health`, `/graphql`, and any non-`/api/`
     GET/HEAD) are literal absolute paths and stay correct under host-header routing
     because the path is never rewritten. Port `auth/middleware.go` **byte-for-byte** and
     resist any urge to "generalize" those path checks — a prefix parameter here is what
     would turn an API GET into public traffic.
   - **`MkdirAll` on the DB's parent is new and required.** The reference ran from a cwd
     where the DB file's directory already existed; the new default is
     `./data/issuetracker/issues.db`, which does not. Without this, `Build` fails on a
     fresh machine. **The same applies to `auth.passkey.store_path`**, which Step 3 moved
     from a cwd-relative filename into `./data/issuetracker/passkeys.json`: add a second
     `os.MkdirAll(filepath.Dir(cfg.Auth.Passkey.StorePath), 0o755)` guarded by
     `if cfg.Auth.Mode == "ldap_passkey"`. Without it, `ldap_passkey` fails inside
     `EnablePasskeys` on a fresh machine — a failure nothing in this plan's verification
     reaches, because that mode is untestable here (R3).
   - **Do not port `cors()`**, and do not add a `defer database.Close()` — the handle must
     outlive the call. `Close` appears only on the error paths above.

   **The two helpers the pseudocode calls are new and local to `build.go`; define them,
   do not assume they exist.** So must every other line of new Go this step introduces —
   `Build`, both helpers, and the ported `staticHandler` family from 4.2 all live in
   **`internal/issuetracker/build.go` and nowhere else**. A second file (`static.go`,
   `helpers.go`) would compile fine but silently break this step's recovery line, which
   removes exactly one path.

   ```go
   // checkStaticDir fails loud per Principle 4. The reference (backend/main.go:45-52)
   // logs a warning and serves on anyway; we return an error so the dispatcher's
   // unavailableHandler gives an explicit 503 rather than 404-ing every asset.
   func checkStaticDir(dir string) error {
       info, err := os.Stat(dir)
       if err != nil {
           return fmt.Errorf("issuetracker: static_dir %q: %w", dir, err)
       }
       if !info.IsDir() {
           return fmt.Errorf("issuetracker: static_dir %q is not a directory", dir)
       }
       return nil
   }

   // settingsFrom is a flat copy; every default lives in auth.FromSettings (Step 3.4).
   func settingsFrom(c config.IssueTrackerAuthConfig) auth.Settings {
       return auth.Settings{
           Mode:              c.Mode,
           SessionTTLMinutes: c.SessionTTLMinutes,
           IdleTTLMinutes:    c.IdleTTLMinutes,
           CookieSecure:      c.CookieSecure,
           LDAP: auth.LDAPConfig{
               URL: c.LDAP.URL, StartTLS: c.LDAP.StartTLS,
               InsecureTLS: c.LDAP.InsecureTLS, BaseDN: c.LDAP.BaseDN,
               UserSearchBase: c.LDAP.UserSearchBase,
               UserSearchFilter: c.LDAP.UserSearchFilter,
               GroupSearchBase: c.LDAP.GroupSearchBase,
               RequiredGroups: c.LDAP.RequiredGroups,
               BindDN: c.LDAP.BindDN, BindPasswordFile: c.LDAP.BindPasswordFile,
           },
           Passkey: auth.PasskeyConfig{
               RPID: c.Passkey.RPID, RPDisplayName: c.Passkey.RPDisplayName,
               Origins: c.Passkey.Origins,
               AllowRegistration: c.Passkey.AllowRegistration,
           },
           PasskeyStorePath: c.Passkey.StorePath,
       }
   }
   ```

   `auth.PasskeyConfig` has a sixth field, `ChallengeTTL time.Duration`, which `FromEnv`
   never sets. Leave it at its zero value — adding a config key for it would be a schema
   addition the reference does not have.

   `checkStaticDir` is itself a **deviation from the reference**, and an intentional one.
   `backend/main.go:45-52` stats the directory and, when it is missing, logs
   `"frontend dir %s not found; run the Vite dev server separately"` and registers **no
   `/` handler at all** — so every client route 404s from an empty mux while the process
   reports a healthy start. Under Principle 4 that is exactly the silent degradation the
   dispatcher's `unavailableHandler` exists to replace with an explicit 503. Listed in
   **OQ-1** and pinned by the `Build()`-with-nonexistent-`static_dir` row in Step 6.4.
2. **`staticHandler`: port `internal/multissh/server.go`'s handler
   (`staticHandler`, `staticFileExists`, `noDirList`, `onlyGet`, `writeError`), not
   `internal/todo`'s.** The earlier draft said "todo's handler plus a guard, matching the
   multissh contract" — those are two different handlers and that instruction was not
   satisfiable. This is the resolution; adopt multissh's verbatim.

   Rationale — the decisive reason is the `/api/` guard. This module **must** have one
   (the reference's `spaHandler` has none; it falls through to `index.html` for every
   unmatched path, API paths included), and the trailing-slash detail below is the single
   easiest thing to get wrong in this port. multissh's handler **already contains
   `strings.HasPrefix(r.URL.Path, "/api/")`**, with the trailing slash already correct,
   and that behavior is already pinned by `internal/multissh/static_test.go`. Adopting a
   guard that has been written and tested beats writing the same guard again.

   Two secondary differences against todo's handler (bare `os.Stat` + `http.ServeFile`,
   fallback on any miss) point the same way:
   - `POST` to a client route would return `index.html` with a 200 instead of a 405.
   - `GET /js/` would return a **directory listing**. multissh's `staticFileExists` uses
     `info.Mode().IsRegular()`, so directories take the SPA fallback instead. Note what
     this does and does not buy: it prevents *enumeration* of the ported tree, but
     `GET /js/pages/IssuesPage.tsx` still returns the source either way — which is the
     repo's existing posture, since multissh's `.ts` sources are committed and served too.
     This is **not** a deviation from the reference: its `spaHandler` tests
     `err == nil && !info.IsDir()`, so directories already fall through to `index.html`
     there as well. The comparison is against todo only.

   **Honest cost:** this is a ~60-line copy, not a shared helper. `internal/multissh`'s
   tests cover multissh's copy, not this one — which is why Step 6.4 re-pins the contract
   here rather than citing `static_test.go`, and why any future hardening of the static
   path has to land in two places. Extracting a shared `internal/platform/web` helper
   would fix that, but it means editing multissh, which D3 forbids in this change. Noted
   as a follow-up in **OQ-6**, not solved here.

   The trailing slash is the detail to preserve above all others: the guard must **not**
   fire on the exact path `/api`. The SPA's own API-docs page is the client route `/api`,
   and the Go mux registers no pattern for that exact path, so it must reach the
   `index.html` fallback. A guard written `HasPrefix(path, "/api")` would 404 that page on
   every hard refresh while leaving in-app navigation working — a bug that survives
   casual testing. Step 6.4 pins both halves.

   Adopting this handler carries **exactly two deviations from the reference's
   `spaHandler`**: unmatched `/api/…` returns a JSON 404 (reference: `index.html` at 200),
   and non-GET/HEAD to a client route returns 405 (reference: `index.html`). Both are
   hardening, neither is reachable from the UI, and both are pinned by Step 6.4 rows.
   Together with `checkStaticDir`'s fail-loud behavior and the promoted `EnsureToken` error
   (both Step 4.1), that makes **four deviations in total** — the full list lives in
   **OQ-1** and nowhere else, so the counts stay reconciled.
3. In `cmd/server/main.go`: add the `internal/issuetracker` import and a
   `case "issuetracker": return issuetracker.Build(cfg.IssueTracker)` arm to `buildModule`.
   Change nothing else in that file.

   **Do not run `gofmt -w` (or an editor's format-on-save) on `cmd/server/main.go`.** The
   file is **not** gofmt-clean today — its import block is unsorted — so any formatter run
   rewrites lines this port did not touch, turning a two-line addition into a diff that
   fails Step 8's "no hunk alters another module's entry" gate. Insert the import into the
   existing block by hand, matching the surrounding style. If a tool reformats it anyway,
   `git checkout -- cmd/server/main.go` and redo the two edits; do not "fix" the formatting
   as a drive-by (that is a separate change, and D3 puts it out of scope here).

**Verify:**
- `make build` succeeds.
- `make build-rpi` succeeds (Step 2 already proved the module cross-compiles; this proves
  the whole binary does). **Compare `ls -l unified-webapp-arm64-linux` against the "before"
  byte count recorded in Step 1.1** — `modernc.org/sqlite` plus its `libc` shim are expected
  to add roughly 10–15 MB to the static binary, which matters for a Raspberry Pi deployment
  target and should be a known number rather than a surprise at install time. (If Step 1.1's
  number was not written down it is gone: this binary is the *after*, and the tree no longer
  builds the *before*. Re-deriving it means checking out `$BASE` in a scratch worktree.)
  If `make build-rpi` fails: do **not** work around it by adding build tags or swapping
  in a cgo driver (that breaks D1 outright). Stop, report, and treat it as invalidating
  Decision 3's dependency set — the plan needs revising before more work lands.
- `git diff cmd/server/main.go` shows exactly two edits: one import line and one `case` arm
  (the arm is two lines, so "two additions" would be the wrong count to look for).

#### The server harness — introduced here; defined in *The normative harness* below, reused verbatim by Steps 5 and 8

Three steps need a running server, and **none of them may run it in the foreground**:
`./unified-webapp` blocks until killed, and a sequential agent that starts it in the
foreground stalls with no escape. Equally, a server left running past the end of its step
is worse than a stall — Step 4's server has a *stub* `static_dir`, so if it still holds the
port when Step 5 sweeps the 13 client routes, Step 5 either fails against a stale binary or
"passes" while testing a stub. Every boot below is bracketed.

`cfg.Port` defaults to **8080**, and every `curl` in this plan targets
`http://localhost:8080`. The server writes its log to **`/tmp/it-server.log`** — that file,
not the terminal, is where Step 4's auth-mode and API-token lines and Step 8.1's token are
read from.

> **Single-shell contract — read this before using the harness.**
> `$SRV`, `$CFG`, and `trap … EXIT` are **shell-local**. Ralph issues discrete commands, and
> each one gets a fresh shell: if the boot is one tool call and the checks are another, the
> `EXIT` trap fires the instant the boot command returns, the server dies, `$SRV` and `$CFG`
> come back empty in the next shell, and every check reports connection-refused against a
> process that was killed on purpose. That failure looks exactly like a `Build` defect and
> would send ralph debugging working code.
>
> **Boot, checks, and teardown are ONE bash invocation per boot.** Compose the whole step
> into a single script — never split it across tool calls. Steps 5 and 8 consume this
> harness and are bound by the same rule.
>
> If a single invocation becomes unwieldy (Step 8 has twelve checks), use the PID-file
> fallback instead of splitting the trap: **keep `SRV=$!`** (the `kill -0` liveness check
> needs it) and add `echo "$SRV" > /tmp/it-server.pid` at boot, drop the `trap` line, and
> tear down with an explicit
> `kill "$(cat /tmp/it-server.pid)" 2>/dev/null; rm -f /tmp/it-server.pid`. Every *later*
> invocation must open with its own liveness check, since `$SRV` is gone by then:
> ```bash
> kill -0 "$(cat /tmp/it-server.pid)" 2>/dev/null \
>   || { echo "FAIL: server died"; cat /tmp/it-server.log; exit 1; }
> ```
> This trades the trap's crash-safety for the ability to span invocations, so two obligations
> come with it: **every `exit 1` path must kill via the PID file first** (otherwise a failed
> boot orphans the server and poisons the next one — see the pre-boot check below), and the
> teardown becomes a step you must not forget.
>
> **Heredoc terminators must sit at column 0.** `<<'JSON'` (as opposed to `<<-`) will not
> accept an indented delimiter. When lifting a fenced block out of an indented list item in
> this document, **strip the markdown indentation first**: an indented terminator does not
> close the heredoc, so bash swallows the remainder of the script into the config file and
> **exits 0 with no diagnostic whatsoever** — no server, no checks, no FAIL lines, and a
> clean exit code that reads as success.

### The normative harness

**This block is the single authoritative copy.** Every boot in Steps 4, 5, and 8 uses it
verbatim and supplies only two things: its `$CFG` body and its list of checks. Do not
re-derive it, and do not drop an assertion from it because a particular step "obviously"
cannot hit that failure.

```bash
# --- boot (run from the repo root; this whole block is ONE bash invocation) ---

# 1. The port must be free BEFORE we start. A stale server from an earlier step is the
#    single most dangerous state this harness can be in — see the note below.
curl -s -o /dev/null --max-time 1 localhost:8080/ \
  && { echo "FAIL: :8080 already in use"; command -v lsof >/dev/null && lsof -i :8080; exit 1; }

# 1b. The binary under test must be newer than the module it is supposed to contain.
[ ./unified-webapp -nt internal/issuetracker/build.go ] \
  || { echo "FAIL: stale binary — run make build"; exit 1; }

CFGDIR="$(mktemp -d)"; CFG="$CFGDIR/it.json"   # fresh config per boot; never reuse
cat > "$CFG" <<'JSON'
PLACEHOLDER: replace this whole line with the step's config body (given per boot below).
JSON
./unified-webapp -config "$CFG" > /tmp/it-server.log 2>&1 &
SRV=$!
trap 'kill $SRV 2>/dev/null' EXIT  # so an aborted step cannot orphan the process

# 2. Readiness: status-agnostic, see below.
ready=0
for i in $(seq 1 30); do
  curl -s -o /dev/null localhost:8080/ && { ready=1; break; }
  sleep 0.2
done
[ "$ready" = 1 ] || { echo "FAIL: server never became ready"; cat /tmp/it-server.log; exit 1; }

# 3. Liveness: the probe above proves *a* listener answered, not that it is ours.
kill -0 "$SRV" 2>/dev/null \
  || { echo "FAIL: server process died"; cat /tmp/it-server.log; exit 1; }

# ... the step's checks, each one exiting nonzero on failure ...

# --- teardown (end of step, before the next step starts) ---
kill "$SRV" 2>/dev/null; wait "$SRV" 2>/dev/null
rm -rf "$CFGDIR"
trap - EXIT
exit 0        # wait on a SIGTERM'd child returns 143; without this the
              # whole invocation exits nonzero on a fully correct run
```

**`wait` on a killed job returns 143, so a bare teardown as the script's last command makes a
green run exit nonzero — every boot must end with an explicit `exit 0`.** The cost is that
the terminal `exit 0` also masks any check that forgot its own `|| exit 1`, which is why
every check list in this plan is scripted with an explicit failure exit rather than left as
prose.

The `PLACEHOLDER:` line is **not valid JSON and is not meant to be** — replace the whole
`cat > "$CFG" <<'JSON' … JSON` block (delimiter line included — Boot A needs an unquoted
delimiter) with the step's config body. Leaving it in produces a config parse error at boot,
which is the intended loud failure.

**Every check must fail the script, not just print.** A check that `echo`s `FAIL` and
continues leaves the invocation exiting 0, which ralph reads as a pass — this plan had
exactly that bug in its route sweep. Where a check loops, accumulate:

```bash
fail=0
for x in …; do
  <test> || { echo "FAIL $x"; fail=1; }
done
[ "$fail" = 0 ] || exit 1
```

**Negated checks must be written `if cmd; then …; exit 1; fi` or `! cmd || { …; exit 1; }` —
never `cmd && { …; exit 1; }`.** The `&&` form returns `cmd`'s own status on the *success*
path, so an assertion that a pattern is **absent** exits 1 exactly when it passes.

**Why the pre-boot port check and the `kill -0` are both required.** `cmd/server/main.go:80`
is `log.Fatalf("HTTP error: %v", http.ListenAndServe(addr, handler))` — so a second server
launched against an occupied port **exits immediately**. The readiness probe then succeeds
against the *orphan*, `$SRV` is a zombie, and every subsequent check runs against the
previous step's binary and config. Step 5 sweeping 13 routes against Step 4's stub
`static_dir` would report a clean pass while having tested nothing. The pre-boot check
prevents that state; the `kill -0` catches it if the port was taken between the two.

**A stale server does not look like a failed poll — it looks like a passing run.** That is
why the bind-error advice belongs on the `kill -0` branch and not the readiness branch: if
readiness *succeeded* but `kill -0` failed, grep `/tmp/it-server.log` for
`listen tcp :8080: bind: address already in use`, and kill the orphan (`lsof -i :8080`)
rather than debugging `Build`.

**The readiness probe is deliberately status-agnostic, and both omissions are load-bearing.**
It requests `/` rather than `/api/health`, and omits `-f`:

- `curl` exits **0 on any HTTP status** and **7 while the port is closed**, so a bare
  `curl -s -o /dev/null` answers precisely the question the poll asks — *is a listener up?*
  With `-f`, curl exits 22 on any 4xx/5xx.
- That matters because `unavailableHandler` answers **every path on a failed module's
  hostname with 503**, `/api/health` included. A `-f` probe would spin out all 30 iterations
  and abort with "never became ready" on exactly the boot whose 503 is under test — the
  negative gate would never reach its assertion, and the message would name the wrong cause.
- It also un-breaks Step 8's other-module regression boot, which previously passed only by
  accident (`todo`'s weak fallback answers `/api/health` with a 200 without implementing it).

Because the probe is status-agnostic it proves only that *something* answered. Everything
else — including "the module built" — must be asserted explicitly by the step's checks.
`curl -s localhost:8080/api/health` remains a real positive assertion; it is simply not a
readiness signal.

**A failed readiness poll dumps the log and stops — that rule is for positive boots only.**
A negative-gate boot is *expected* to become ready and then 503.

### Step 4's boots

**Boot A — positive.** Step 5 has not run, so `./web/issuetracker` does **not exist**; the
stub `static_dir` goes in its own temp directory:

```bash
STUB="$(mktemp -d)"; printf '<!doctype html><div id="root"></div>' > "$STUB/index.html"
cat > "$CFG" <<JSON
{
  "port": 8080,
  "host_routing": { "localhost": "issuetracker" },
  "issuetracker": {
    "static_dir": "$STUB",
    "db_path": "./data/issuetracker/issues.db",
    "auth": { "mode": "none" }
  }
}
JSON
```
(Unquoted heredoc delimiter here, so `$STUB` interpolates — the only boot in this plan that
needs interpolation. Terminator still at column 0.)

Checks — scripted, because a bare `curl -s` exits 0 against a 503:

```bash
grep -q 'auth mode' /tmp/it-server.log || { echo "FAIL: no auth mode line"; exit 1; }
code=$(curl -s -o /tmp/h -w '%{http_code}' localhost:8080/api/health)
[ "$code" = 200 ] || { echo "FAIL: health $code"; cat /tmp/h; exit 1; }
[ -f ./data/issuetracker/issues.db ] || { echo "FAIL: db not created"; exit 1; }
curl -s localhost:8080/api/bootstrap | grep -q '"ENG"' \
  || { echo "FAIL: bootstrap missing seed data"; exit 1; }
```

Note what this boot does **not** prove. The omitted `auth` sub-keys are filled in by
`config.DefaultConfig()` before `json.Unmarshal` writes over it (`config.go:193`), so
`FromSettings` never sees a zero value here. This exercises the *config* defaults; it says
nothing about `FromSettings`' own defaulting, which is pinned only by the package-`auth`
unit test in Step 3.

**Boot B — the negative gate.** A second bracketed boot with its own `$CFG`, `db_path` set to
a path whose **parent is an existing regular file**:

```bash
rm -rf /tmp/it-notadir && touch /tmp/it-notadir   # a regular file, not a directory
STUB="$(mktemp -d)"; printf '<!doctype html><div id="root"></div>' > "$STUB/index.html"
cat > "$CFG" <<JSON
{
  "port": 8080,
  "host_routing": { "localhost": "issuetracker" },
  "issuetracker": {
    "static_dir": "$STUB",
    "db_path": "/tmp/it-notadir/issues.db",
    "auth": { "mode": "none" }
  }
}
JSON
```
(Unquoted delimiter again, so `$STUB` interpolates. Terminator at column 0.)

**`checkStaticDir` runs before `MkdirAll`, so `static_dir` must be a valid stub here too** —
otherwise the 503 names `static_dir` and the db-path grep below fails against a perfectly
correct `Build`, sending ralph to rewrite working Go. **Boot B re-creates its own `$STUB`
because it is a separate invocation**, and per the single-shell contract Boot A's variable
does not survive into it. The `rm -rf` prefix closes the inverse false pass: a surviving
`/tmp/it-notadir` *directory* would make `touch` succeed, `MkdirAll` succeed, and the gate
pass while testing nothing.

`os.MkdirAll` fails `ENOTDIR` there for **any** uid, which an "unwritable directory" would
not: ralph may well be running as root, where permission bits are ignored and the gate would
silently pass while testing nothing.

The server starts normally and readiness succeeds — it is the *module* that failed, not the
process, and `unavailableHandler` is serving its hostname. Assert on that response:

```bash
code=$(curl -s -o /tmp/neg -w '%{http_code}' localhost:8080/api/health)
[ "$code" = 503 ] || { echo "FAIL: want 503, got $code"; exit 1; }
grep -q 'module unavailable' /tmp/neg && grep -q 'issuetracker' /tmp/neg \
  && grep -q '/tmp/it-notadir/issues.db' /tmp/neg \
  || { echo "FAIL: body lost the cause"; cat /tmp/neg; exit 1; }
```

The body check is the point of the gate, not the status. `unavailableHandler` copies
`cause.Error()` **verbatim** into `{"error":"module unavailable","module":…,"reason":…}`
(`main.go:138-148`), so the full `db_path` reaches the response only if Step 4.1's wrap
includes it — which is why that wrap is pinned literally there rather than elided. The raw
`os.MkdirAll` error names only the *parent* (`mkdir /tmp/it-notadir: not a directory`); the
filename comes from the wrap alone. **If this grep fails, suspect the wrap text first** —
the most likely cause is a `Build` that wrapped `filepath.Dir(cfg.DBPath)` instead of
`cfg.DBPath`, not an error "swallowed" somewhere.

**Recovery:** kill any surviving server — `kill $SRV` in the same shell, or
`kill "$(cat /tmp/it-server.pid)" 2>/dev/null; rm -f /tmp/it-server.pid` under the fallback —
then `git checkout -- cmd/server/main.go`, `rm -f internal/issuetracker/build.go`,
`rm -rf ./data/issuetracker`, and `rm -f /tmp/it-notadir /tmp/neg`.
(If any of Step 4.1's Go code landed outside `build.go`, widen the `rm` accordingly — see
the note under 4.1 requiring it all to live in that one file.)

---

### Step 5 — Port the frontend to `web/issuetracker/`

**Files touched:** `web/issuetracker/**` (new), `package.json`, `tsconfig.json`,
`package-lock.json`

1. Copy `reference/issue-tracker/frontend/src/**` → `web/issuetracker/js/`
   (`main.tsx`, `App.tsx`, `api.ts`, `types.ts`, `passkey.ts`, `AuthContext.tsx`,
   `DataContext.tsx`, `styles.css`, `components/`, `pages/`). Do **not** copy
   `node_modules/`, `dist/`, `vite.config.ts`, `tsconfig.json`, `tsconfig.tsbuildinfo`,
   `package.json`, or `package-lock.json` — those are all replaced by root equivalents.
   (`tsconfig.tsbuildinfo` in particular is stale build state that would make an
   incremental `tsc` skip real work.) There is no `public/`, no `src/vite-env.d.ts`, and
   no image/font/SVG asset anywhere in the tree — all iconography is inline Unicode and
   CSS, and `styles.css` contains zero `url()`, `@import`, or `@font-face`. So the copy is
   the whole frontend; nothing else needs to follow it.
2. Add `web/issuetracker/js/css.d.ts` containing `declare module "*.css";`, copying the
   comment rationale from `web/multissh/js/css.d.ts`. `main.tsx`'s `import "./styles.css"`
   is the only non-JS import in the tree and it will fail `tsc` with TS2307 without this.
3. Write `web/issuetracker/index.html`. Do not copy the reference's source `index.html`
   (it carries Vite's `<script type="module" src="/src/main.tsx">`), and do **not** copy
   `web/multissh/index.html`'s relative hrefs:
   ```html
   <!doctype html>
   <html lang="en">
     <head>
       <meta charset="UTF-8" />
       <meta name="viewport" content="width=device-width, initial-scale=1.0" />
       <title>newlinear</title>
       <link rel="stylesheet" href="/js/bundle.css" />
     </head>
     <body>
       <div id="root"></div>
       <script src="/js/bundle.js"></script>
     </body>
   </html>
   ```
   **The leading slashes are load-bearing.** This is the only module in the repo with
   history-API routing. On a hard refresh of `/issue/ENG-1` the document base URL is
   `/issue/`, so a relative `src="js/bundle.js"` resolves to `GET /issue/js/bundle.js`;
   no such file exists, the SPA fallback answers with `index.html` at 200 `text/html`,
   and the browser tries to execute HTML as JavaScript — a blank page on every deep link,
   every hard refresh, with a 200 in the access log. The same applies to `/stories/:id`
   and `/epics/:id`. The reference's own Vite-built `dist/index.html` uses `/assets/…`
   for exactly this reason; multissh gets away with relative paths only because it never
   changes the URL. Step 6.4 pins this with a row asserting `index.html` contains `/js/`,
   because no build gate would otherwise catch it.
4. `package.json`:
   - `dependencies`: add `react@^18.3.1`, `react-dom@^18.3.1`, `react-router-dom@^6.26.2`
     (versions from the reference's `package.json`).
   - `devDependencies`: add `@types/react@^18.3.11`, `@types/react-dom@^18.3.0`.
   - An npm script is **one JSON string**, so these must be appended inline with ` && `,
     not written as multi-line shell. The `"production"` quotes also have to survive JSON
     escaping. Append **exactly** this to the end of the existing `build` string — this is
     the invocation whose output gets committed:
     ```
      && esbuild web/issuetracker/js/main.tsx --bundle --target=es2020 --jsx=automatic --define:process.env.NODE_ENV='\"production\"' --minify --outfile=web/issuetracker/js/bundle.js
     ```
     The four characters `'\"` … `\"'` are what the file must literally contain. Read back
     with `node -p "require('./package.json').scripts.build"`: the printed value must show
     `--define:process.env.NODE_ENV='"production"'` with plain double quotes. If the
     backslashes are missing, `npm install` will reject the file as malformed JSON; if the
     *single* quotes are missing, esbuild silently substitutes a bare identifier and the
     bundle comes out at ~552 KB with `react.development` still in it — which the size band
     and the grep in this step's Verify both catch, but only after a wasted build.
   - Append **exactly** this to the end of the existing `build:dev` string — the development
     counterpart, **no define, no minify, plus `--sourcemap`**, matching how the other three
     modules differ between the two scripts:
     ```
      && esbuild web/issuetracker/js/main.tsx --bundle --target=es2020 --jsx=automatic --outfile=web/issuetracker/js/bundle.js --sourcemap
     ```
   - **Leave the three existing esbuild invocations byte-identical** in both scripts.
     Their outputs rebuild reproducibly today, which is what makes the Step 5 "other
     bundles unchanged" check meaningful.

   On the flags:
   - `--jsx=automatic` is required — without it esbuild emits `React.createElement` calls
     against a `React` identifier the reference files never import. esbuild infers the
     `tsx` loader from the extension and emits the sibling `bundle.css` automatically from
     the CSS import, exactly as it does for multissh.
   - `--define:process.env.NODE_ENV='"production"'` is **a correctness requirement, not an
     optimization.** esbuild defaults browser bundles to `"development"`, which ships
     `react.development.js` — measured at 1,263,149 bytes, versus 414,087 with the define
     alone and 210,305 with define + minify (the shipped configuration) —
     and, more importantly, activates `React.StrictMode`'s double-invocation of effects
     and renders. All 10 `useEffect` call sites in the ported tree (across 9 files) would
     fire twice. They are
     read-only fetches so nothing corrupts, but doubling every effect is a behavior delta,
     and "port, do not redesign" forbids shipping one by accident.
   - `--minify` is deliberate and is the second place `issuetracker` overrides the
     multissh convention. multissh's bundle is unminified because nothing in it has a
     dev/prod split worth the flag; here it roughly halves a page-load-critical asset
     again on top of the define. Note that esbuild couples these two — `--minify` alone
     would imply the production define — but both are stated explicitly so the app's
     runtime behavior does not silently depend on whether someone later removes `--minify`
     for readability.
5. `tsconfig.json` — three additive edits:
   - add `"jsx": "react-jsx"`,
   - add `"DOM.Iterable"` to `lib` (the reference tsconfig had it),
   - add `"web/issuetracker/js/**/*.ts"` and `"web/issuetracker/js/**/*.tsx"` to `include`.

   These are expected to be inert for the existing three modules, but that is a claim to
   be **verified by `npm run typecheck`**, not asserted — widening `lib` can in principle
   change overload resolution, and this is a shared file. If the existing modules do start
   failing, the fix is a per-module tsconfig, not silently loosening the root.
   Deliberately **not** carried over from the reference tsconfig: `noUnusedLocals`,
   `noUnusedParameters`, `noFallthroughCasesInSwitch`, `allowImportingTsExtensions`,
   `isolatedModules`. Relaxing these is safe; adding them repo-wide would be a D3 violation.
   Two further omissions were checked rather than assumed and are **verified inert**:
   `resolveJsonModule` (the ported tree imports no `.json` file) and `moduleDetection`
   (every ported file has a top-level `import` or `export`, so none is treated as a script).
   Do not add either to the shared file on the theory that the reference had it.
   `esModuleInterop` must **not** be added — `moduleResolution: "bundler"` already implies
   `allowSyntheticDefaultImports`, which is what makes `import React from "react"` work.
6. Confirm `api.ts` already uses same-origin relative paths. The audit found no
   `import.meta.env` and no `VITE_*` anywhere in `src/`, no path aliases, and no
   configurable base URL — `req()` calls `fetch(path)` with a bare `/api/...` string and
   relies on the default `same-origin` credentials mode for the session cookie. No rewrite
   is expected. `ApiPage.tsx` builds its documented curl example from
   `window.location.origin + "/graphql"`, which is already correct under host routing.
   Two things to watch during typecheck, both benign but worth recognizing rather than
   "fixing": `api.ts` declares local interfaces named
   `PublicKeyCredentialCreationOptionsJSON` / `...RequestOptionsJSON` that shadow the DOM
   lib's same-named types within that module, and `EpicDetailPage.tsx` imports
   `StoryModal` from `StoriesPage.tsx` (a page exporting a component).
7. Run `make web` (which runs `npm install` since `node_modules/` is absent) and **commit
   the generated `web/issuetracker/js/bundle.js`, `bundle.css`, and the updated
   `package-lock.json`** — tracked build output is this repo's convention.
   **What gets committed is the output of `npm run build`, never `npm run build:dev`.**
   `make web` invokes `build`, so following the Makefile is sufficient; the hazard is a
   developer running `build:dev` for a sourcemap and then committing the development
   bundle. Step 8 re-runs `make web` and requires a clean `git status` afterwards, which
   catches exactly that.

**Verify:**
- `npm run typecheck` passes with the new sources in `include` — and the three existing
  modules still typecheck (see 5.5; this is the check, not an assumption).
- `make web` produces `web/issuetracker/js/bundle.js` and `bundle.css`, and leaves the
  three pre-existing bundles byte-identical (`git diff --stat web/` must show no change
  under `web/obsidianoid`, `web/slideshow`, or `web/multissh`). Step 1.1 measured that all
  three rebuild reproducibly, so any diff here means an existing esbuild invocation was
  edited rather than appended to. **If one of them is dirty: revert it
  (`git checkout -- web/<module>/`), fix the `package.json` edit, and re-run.** Never commit
  another module's regenerated bundle to make the check pass — that is precisely the D3
  regression this gate exists to catch, and it would sail through every other gate in the
  plan.
- The committed bundle is the **production** one:
  `grep -c "react.development" web/issuetracker/js/bundle.js` returns 0, and the file is
  on the order of 200–450 KB rather than ~1.26 MB.

  **The size band is the binding check; the grep is the weaker of the two.** `--minify`
  strips comments, so the literal string `react.development.js` — which appears in the dev
  bundle largely in path comments — is not guaranteed to survive into a minified artifact
  even when a dev build sneaks through some other route. The bytes cannot lie: ~210 KB is
  correct, ~414 KB means the define landed but `--minify` did not, ~552 KB means the define
  was mis-escaped into a bare identifier, and ~1.26 MB is a straight `build:dev` artifact.
  Read the number first, then the grep.
- `web/issuetracker/js/bundle.css` exists and is non-empty (`[ -s … ]`). A missing stylesheet
  still yields a 200 and an `id="root"` document, so the sweep below would pass on an
  unstyled page.
- `grep -rn "import.meta\|VITE_\|localhost:8080" web/issuetracker/js/` returns nothing
  (recursive over the directory — do not rely on `**` globbing, which needs `globstar`).
  **`grep`'s exit status 1 IS the pass here** — do not wrap it in a `|| exit 1`.
- **SPA routing sweep and asset sanity — scripted, not manual.** See the fenced blocks
  immediately below this list; they are deliberately un-indented, and all of them run
  **inside the one sweep invocation** because they need the live server (unlike
  `npm run typecheck` and `make web` above, which do not).

**The SPA routing sweep.** Boot under **the normative harness** (§ *The normative harness*),
**as a single bash invocation** (boot + sweep + teardown together — see the single-shell
contract), with a fresh `$CFG` whose `static_dir` is the real `web/issuetracker` — not Step
4's temp stub. A stub-`static_dir` process still holding :8080 would make this whole sweep
pass while testing nothing; the harness's **pre-boot port-free check and post-readiness
`kill -0`** are what prevent that, and neither may be dropped here.

The two blocks below sit at column 0 on purpose — **the `JSON` terminator must stay at
column 0** (see the heredoc rule in the single-shell contract). Do not re-indent them to
match the surrounding list, and do not paste them from an indented copy.

```bash
cat > "$CFG" <<'JSON'
{
  "port": 8080,
  "host_routing": { "localhost": "issuetracker" },
  "issuetracker": {
    "static_dir": "./web/issuetracker",
    "db_path": "./data/issuetracker/issues.db",
    "auth": { "mode": "none" }
  }
}
JSON
```

Then walk all 13 client routes and assert both the status and that a real document came
back. Note the `fail` accumulator and the trailing `exit 1`: without them the loop prints
`FAIL` lines and the invocation still exits 0, which ralph reads as a pass.

```bash
fail=0
for p in / /issues /board /my-issues /my-board /issue/ENG-1 /stories /stories/1 \
         /epics /epics/1 /projects /tags /api; do
  code=$(curl -s -o /tmp/body -w '%{http_code}' "http://localhost:8080$p")
  if [ "$code" = 200 ] && grep -q 'id="root"' /tmp/body && grep -q '/js/bundle.js' /tmp/body
  then echo "ok   $p $code"
  else echo "FAIL $p $code"; fail=1
  fi
done
[ "$fail" = 0 ] || { echo "FAIL: SPA routing sweep"; exit 1; }
```

Then asset sanity — **in this same invocation, after the loop and before teardown**, since
both curls need the live server:

```bash
hdr=$(curl -sI localhost:8080/js/bundle.js)
printf '%s' "$hdr" | grep -q ' 200' && printf '%s' "$hdr" | grep -qi 'content-type:.*javascript' \
  || { echo "FAIL: /js/bundle.js not served as JS"; printf '%s\n' "$hdr"; exit 1; }
curl -s localhost:8080/issue/js/bundle.js | head -c 20 | grep -qi '<!doctype\|<html' \
  || { echo "FAIL: /issue/js/bundle.js did not hit the SPA fallback"; exit 1; }
```

The second assertion confirms the fallback is what a relative href *would* have hit. Then
the harness teardown:

```bash
kill "$SRV" 2>/dev/null; wait "$SRV" 2>/dev/null
rm -rf "$CFGDIR"
trap - EXIT
exit 0
```

Every route must print `ok … 200`. The `/js/bundle.js` assertion is what catches a
relative-href regression: with relative hrefs the body still contains `id="root"`, so
checking only the status or only the root div would pass while the page is blank in a
browser. `/api` is the route that fails if Step 4.2's guard lost its trailing slash.

**Recovery:** kill any surviving server — `kill $SRV` in the same shell, or
`kill "$(cat /tmp/it-server.pid)" 2>/dev/null; rm -f /tmp/it-server.pid` under the
PID-file fallback — then
`rm -rf web/issuetracker && git checkout -- package.json package-lock.json tsconfig.json`.
Note `git checkout -- package-lock.json` reverts `npm install`'s lockfile changes too; if
`node_modules/` is left behind it is gitignored and harmless.

---

### Step 6 — Tests

**Files touched (all new):** `internal/issuetracker/*_test.go`

Match the existing modules' style (`internal/todo/store_test.go`,
`internal/multissh/static_test.go`): table-driven, `httptest`, `t.TempDir()`.

1. `store_test.go` — open a DB under `t.TempDir()` (`defer database.Close()`), then cover:
   **the first `Seed()` produces exactly 7 issues, 5 tags, and 3 users** (plus team `ENG`,
   epic "Launch v1", story "Issue tracking core", and one `blocks` relation); `Seed()` is
   idempotent across two calls — the same counts after the second; `EnsureToken("default")`
   returns the same token on a second call; creating an issue yields a `TEAM-1`-shaped
   identifier and the counter increments; adding a relation creates its automatic inverse;
   tag create/list/delete.

   **The exact counts belong here, not only in Step 8's smoke.** `store.Seed()` discards
   most of its own errors, so a partial seed is silent; asserting the numbers in a unit test
   means a regression fails `make test` in seconds rather than surviving to a hand-read
   `curl` at the acceptance gate.
2. `handler_test.go` — drive `api.New(st).Routes(mux)` through `httptest`:
   `GET /api/health`, `GET /api/bootstrap`, create→get an issue,
   `GET /api/issues/by-identifier/{ident}`, and a 404 for an unknown id.
3. `graphql_test.go` — the highest-value smoke per FRD §7: `POST /graphql` with no
   `Authorization` header is rejected; with the wrong token is rejected; with the right
   token (bare **and** `Bearer `-prefixed, since the reference strips the prefix) returns
   issue nodes.
   **Assert on the response body, not the status code.** The reference's `writeData`
   always emits HTTP 200 and signals auth failure through a GraphQL `errors` array, so a
   test that checks for 401 will fail against correct ported behavior.
4. `build_test.go` — the static contract, pinned as a table. The fixture `static_dir`
   needs an `index.html`, a `js/bundle.js`, and at least one extra file under `js/` so
   the directory-listing row is meaningful. **Every row that calls `Build` must also set
   `db_path` to a `t.TempDir()` path, not just `static_dir`** — the default
   `./data/issuetracker/issues.db` is relative to the *test's* cwd, which is the package
   directory, so a defaulted row silently writes `internal/issuetracker/data/` into the
   repo, where the `web/`-scoped Step 8.11 gate cannot see it:

   | Request | Expected | Pins |
   |---|---|---|
   | `GET /issue/ENG-1` | 200, body is `index.html` | deep-link fallback |
   | `GET /api` | 200, body is `index.html` | **SPA API-docs route** — breaks if the guard drops its trailing slash |
   | `GET /api/` | JSON 404 | accepted deviation, OQ-1 |
   | `GET /api/nope` | JSON 404, not HTML | the `/api/` guard |
   | `POST /api/nope` | JSON 404 | guard applies to all methods |
   | `POST /issue/ENG-1` | **405** with `Allow: GET, HEAD` | multissh handler deviation from the reference (OQ-1) |
   | `GET /js/` | 200, body is `index.html` — **never a directory listing** | matches the reference (not a deviation); prevents *enumeration* of the ported tree. Individual sources stay fetchable, as with multissh's committed `.ts` files |
   | `GET /js/bundle.js` | 200, serves the real asset | assets still work |
   | `../../web/issuetracker/index.html` contains `src="/js/` and `href="/js/` | assert on the **file** via `os.ReadFile`, not a response | **the absolute-href override** — the only automated guard against the blank-deep-link failure |
   | `Build()` with a nonexistent `static_dir` | returns an error | Principle 4 — pins `checkStaticDir`, OQ-1 deviation 3 (reference logs-and-skips) |
   | `Build()` with a `static_dir` pointing at a **regular file** | returns an error | same deviation; the reference only stats, so this case has no analogue there at all |
   | `Build()` with a **valid** `static_dir` and a `db_path` whose **parent is a regular file** | returns a wrapped error **naming the db path** | Principle 4 — `ENOTDIR` for any uid, unlike a permission-based case a root-run test would not trip |

   Two rows carry disproportionate weight. `GET /api` is the case a naive guard breaks
   while everything else still passes. The `index.html` href row is unusual — a test that
   greps a checked-in file rather than exercising a handler — but it is deliberate: the
   relative-href bug produces a 200 with a correct-looking body, so **no request-level
   assertion can detect it**. Checking the file content is the only cheap automated guard.
   Its path is relative to the **test's** cwd, which Go sets to the package directory
   `internal/issuetracker/` — hence `../../web/issuetracker/index.html`, not the repo-root
   spelling used everywhere else in this plan.

   **Fixture ordering matters for the two `db_path` rows.** `checkStaticDir` runs *first* in
   `Build`, so a test that passes a bad `db_path` alongside a bogus `static_dir` gets the
   static-dir error and proves nothing about the DB path. Give those rows a real
   `t.TempDir()` containing an `index.html`, and assert the returned error mentions the
   **db path** — not merely that some error came back.

   **OQ-1's deviation 4 (the promoted `EnsureToken` error) is not directly testable here**
   without fault injection the ported store does not offer. It is covered indirectly: the
   Step 4 negative gate proves `Build` errors reach the dispatcher as a 503 carrying the
   reason, and that is the mechanism deviation 4 relies on. Note it in the test file as a
   comment rather than leaving a reader to wonder why the list has four entries and the
   table has three.
5. **Do not use `goleak` in these tests.** `modernc.org/sqlite` keeps background goroutines
   alive and `Build` intentionally has no `Close`, so `goleak.VerifyNone` would fail for
   reasons unrelated to this code. `internal/multissh/build_test.go`'s existing `goleak`
   call is package-scoped and is unaffected.
6. There are **no tests to port** — the reference tree contains zero `*_test.go` files.
   Everything above is written fresh against the ported code.

**Verify:**
- `make test` (`go test -race ./...`) passes, including every pre-existing package.
- `go test -race -count=2 ./internal/issuetracker/...` passes — `-count=2` catches
  state leaking between runs through a shared temp DB, the most likely flake here.

**Recovery:** `rm -f internal/issuetracker/*_test.go`.

---

### Step 7 — Documentation

**Files touched:** `README.md`, `docs/` (optional `docs/issuetracker.md`)

1. `README.md`: add `issuetracker` to the opening one-line module list; add
   `"issuetracker.cmdhome.net"` / `"issuetracker-test.cmdhome.net"` rows to the
   `host_routing` sample; add an `"issuetracker"` block to the sample config; add a
   `#### The issuetracker section` table documenting `static_dir`, `db_path`, and the
   `auth` subtree (following the shape of the existing `#### The multissh section`).
2. Add a **Data Directory Layout → Issuetracker** subsection covering `issues.db`
   (plus WAL/SHM sidecars) and `passkeys.json`.
3. Document the GraphQL token: where it is printed at boot, that it is also on the
   in-app API page, and the `curl` invocation from the reference README. The security
   note must state the **actual** threat model, not the softer hostname framing:
   in `mode: "none"`, because the platform CORS layer sets `Allow-Origin: *` and
   `GET /api/token` is unauthenticated, **any website the operator's browser visits can
   read the GraphQL write token cross-origin and exfiltrate it** (R9/OQ-5). Also note the
   boot log contains that secret. Do not write "reaching the hostname is the whole access
   boundary" here — that sentence is true for `multissh` and false for this module.
4. Document the `db_path` constraint from R13: no spaces, `?`, or `#` in the path, since
   the DSN is built by string interpolation and tilde expansion can introduce them.
5. If the auth surface needs more than a table, put it in `docs/issuetracker.md` and link
   it from the README, mirroring how `docs/multissh.md` is referenced.

**Verify:** every config key named in the README exists in `config.go` and in
`unified-webapp-example.json`, and every key in the example JSON appears in the README —
check both directions; a stale key in either place is the failure mode this step exists to prevent.

**Recovery:** `git checkout -- README.md && rm -f docs/issuetracker.md`. This step touches
no code, so nothing else needs rewinding.

---

### Step 8 — Acceptance gate

Run the full FRD acceptance set from a clean tree, in order, and record each result.

**Build and test gates (FRD acceptance, bullet 1):**
```
make build
make build-rpi
make test
make web
npm run typecheck
```
All five must succeed. Additionally `CGO_ENABLED=0 go build ./cmd/server` to make D1
explicit rather than incidental, and a one-time `govulncheck ./...` (see OQ-7) — the port
adds ~20 transitive dependencies plus a networked LDAP client and a WebAuthn
implementation, and this is the only moment in the plan where the full closure exists and
nothing else is in flight.

**`govulncheck` is not installed in this environment** (verified during planning), so it
needs an install-or-skip, not a bare invocation:

```bash
VULN="$(command -v govulncheck || true)"
if [ -z "$VULN" ]; then
  go install golang.org/x/vuln/cmd/govulncheck@latest
  VULN="$(go env GOPATH)/bin/govulncheck"
fi
if [ -x "$VULN" ]; then "$VULN" ./...; else echo "govulncheck unavailable — not run"; fi
```

**`go install` puts the binary in `$(go env GOPATH)/bin`, which is frequently not on
`PATH`** — so a second `command -v govulncheck` right after a *successful* install still
finds nothing, and a naive script reports "unavailable" when the tool is sitting right
there. Hence the explicit path rather than a bare re-probe.

The install needs network and writes to `$GOPATH/bin` — **outside the repo**, so it cannot
dirty the tree or the regression gate. **If the install fails, record
"govulncheck unavailable — not run" in the acceptance report and continue.** This is a
report-and-stop *security* gate, not a build gate: a missing scanner must not block a port
whose five real acceptance gates are green. Likewise treat a finding as report-and-stop
rather than an auto-upgrade — bumping a pinned version is a plan change (Step 1.3), not an
implementation detail.

**End-to-end smoke (FRD acceptance, bullet 2)** — **inside the normative harness**
(pre-boot port-free check, fresh `$CFG`, backgrounded, readiness-polled with the
status-agnostic probe, `kill -0` liveness check, killed at the end). Twelve checks is where a
single bash invocation gets unwieldy, so this is the boot that most likely wants the
**PID-file fallback** from the harness section. If you take it: the explicit teardown is
yours to remember, **every `exit 1` path must kill via the PID file first**, and the
tree-state requirement before the human gate below says the server must end up killed. The
config:

```bash
cat > "$CFG" <<'JSON'
{
  "port": 8080,
  "host_routing": {
    "localhost": "issuetracker",
    "issuetracker-test.cmdhome.net": "issuetracker"
  },
  "issuetracker": {
    "static_dir": "./web/issuetracker",
    "db_path": "./data/issuetracker/issues.db",
    "auth": { "mode": "none" }
  }
}
JSON
```

**Every numbered check below must be scripted per the harness rule — a bare `curl -s` exits
0 against a 503, so a check written as prose asserts nothing.** Capture each response and
test its status and body explicitly, exactly as Boot A's check list does.

1. Capture the API token from **`/tmp/it-server.log`**, the harness's log file (it is also
   returned by `GET /api/token` and included in `GET /api/bootstrap`).
2. `curl -s localhost:8080/api/health` → healthy.
3. `curl -s localhost:8080/api/bootstrap` → the seeded demo data: team `ENG`, three users,
   five tags, epic "Launch v1", story "Issue tracking core", seven issues, one `blocks`
   relation. Check the counts — `store.Seed()` discards most of its own errors, so a
   partial seed leaves no trace in the log.
4. CRUD, per the routes that actually exist (`internal/issuetracker/api/api.go`):
   - Issues, stories, epics — full cycle: `POST` → `PATCH /{id}` → `GET /{id}` → `DELETE`.
   - **Tags — `POST /api/tags` → `GET /api/tags` (list) → `DELETE /api/tags/{id}`.**
     There is no `PATCH /api/tags/{id}` and no get-by-id; the full cycle is not
     expressible for tags and must not be attempted.
5. `GET /api/issues/by-identifier/{ENG-1}` resolves.
6. `GET /api/me` — the endpoint the frontend's `AuthContext` calls on load. In
   `mode: "none"` there is **no principal**, so `auth_handlers.go:40-45` returns
   **HTTP 200 with exactly `{"authenticated": false, "mode": "none", "user": null}`**.
   Assert that shape. Do not expect an identity object: asserting one would fail a
   *correct* port, and the obvious "repair" — synthesizing an anonymous user — is a
   port-don't-redesign violation that would also change what `/my-issues` and `/my-board`
   display.
7. **Relations, including the automatic inverse** — this is an explicit FRD §2 parity
   item and is otherwise covered only at store level:
   `POST /api/issues/{a}/relations` with a `blocks` relation to issue `b`, then confirm
   `GET /api/issues/{b}` reports the inverse `blocked by` relation. Then
   `DELETE /api/relations/{id}` and confirm both sides disappear.
8. GraphQL, per the reference README:
   ```
   curl -X POST http://localhost:8080/graphql \
     -H "Authorization: Bearer $TOKEN" \
     -H "Content-Type: application/json" \
     -d '{"query":"query { issues { nodes { identifier title state { name } } } }"}'
   ```
   → issue nodes with `identifier`, `title`, and `state.name`, matching the reference
   README's documented output. The same call with **no** token must be rejected — but
   note it still returns **HTTP 200** with a GraphQL `errors` array, so grep the body.
9. **Re-run the Step 5 scripted route sweep** against the production config (all 13
   routes, asserting `id="root"` and `/js/bundle.js` in each body), plus
   `/api/nope` → JSON 404 and `POST /issue/ENG-1` → 405.
10. Host-routing pass: the same checks via
    `curl -H 'Host: issuetracker-test.cmdhome.net' ...`.
11. `make web` once more, then **`git status --porcelain -- web/ package-lock.json` must
    print nothing** — proving the committed bundle is exactly what the build produces, and
    that nobody committed a `build:dev` artifact.

    **The path scope is required, not a convenience.** A bare `git status --porcelain` can
    never be clean in this tree: `.omc/` is ralph's own state directory, and
    `docs/frd-issue-tracker.md`, `docs/plan-issue-tracker.md`, and (before Step 1.2)
    `reference/` are all untracked. Ralph must **not** "fix" a dirty status with
    `git add -A` — see the commit policy above. Scoping to `web/` and `package-lock.json`
    asks the question that actually matters, which is the same build-artifact check
    Step 5.7 describes.

12. **Re-run 8.3's seed-count queries immediately before handing over.** The human gate
    below is a visual pass over seeded data, and checks 4–7 above created, mutated, and
    deleted issues, stories, epics, tags, and relations since 8.3 ran. Re-assert the counts
    (or state plainly what the smoke added and removed) so the reviewer knows whether an
    unexpected row is their bug to chase or ralph's leftover. A human comparing the board
    against a stale "seven issues" line is the cheapest false alarm in this plan.

**HUMAN GATE — ralph must stop here and request a person.** Everything above is
scriptable; the following is not, and ralph must not self-certify it or mark the plan
complete without it:

- **Kanban drag-and-drop** on `/board` — issues move between columns and the new state
  persists across a reload. This is the single largest piece of behavior with no
  automated coverage at any level.
- Visual pass: issue detail, Stories, Epics, Projects, Tags render without console errors.
- The API page displays the token.
- Confirm in devtools that `bundle.js` loads once per page (not a 200 `text/html`) on a
  hard refresh of `/issue/ENG-1` — the human-visible form of the absolute-href check.
- **`/my-issues` and `/my-board` will be empty, and that is correct.** In `mode: "none"`
  `GET /api/me` reports `"user": null` (8.6), so there is no "me" to filter by. Empty is
  the expected render, not a porting bug — do not let it trigger a "fix".

**State to leave the tree in while waiting:** all of Steps 1–7 committed, the server
**killed** — the harness teardown (`kill "$SRV" 2>/dev/null; wait "$SRV" 2>/dev/null;
rm -rf "$CFGDIR"; trap - EXIT; exit 0`) in the same shell, or
`kill "$(cat /tmp/it-server.pid)" 2>/dev/null; rm -f /tmp/it-server.pid` if Step 8 used the
PID-file fallback (`$SRV` expands empty in a later shell, so the bare `kill $SRV` form would
silently leave the server running) — and `git status --porcelain -- web/ package-lock.json`
empty. Do not start a fresh branch, do not merge, do not `git clean`, and do not delete
`./data/issuetracker/` — the human's pass needs a running server they can start from
exactly this state, and the seeded DB is what they will be looking at.

Ralph should report the scripted results and explicitly ask for this pass, rather than
treating a green script run as acceptance.

**Regression gate (FRD acceptance, bullet 3 — "other modules' behavior is untouched"):**
- `git diff --stat $BASE` — the SHA captured in Step 1.1. This reads **committed** work
  only; if the commit policy above was not followed, this gate reports nothing and proves
  nothing. The only modified (as opposed to added) files may be `.gitignore`, `go.mod`,
  `go.sum`, `package.json`, `package-lock.json`, `tsconfig.json`, `cmd/server/main.go`,
  `internal/platform/config/config.go`, `unified-webapp-example.json`, `README.md`.
  **Anything else modified is a defect** — in particular `internal/platform/middleware/`,
  `unified-webapp.json`, and any
  `internal/{todo,grocery,slideshow,menuserver,obsidianoid,multissh}/`
  or `web/{obsidianoid,slideshow,multissh}/` file. (Untracked paths — `.omc/`, `docs/*.md`,
  `reference/` — never appear in `git diff` at all, so they cannot pollute this gate.)
- Review each of those ten diffs line by line against **Principle 3's actual rule: no hunk
  removes or alters an existing module's entry.** Expected in-line modifications:

  | File | Expected in-line change | Why it is not an append |
  |---|---|---|
  | `tsconfig.json` | `lib` array gains `"DOM.Iterable"`; `jsx` added | editing an existing array member line |
  | `package.json` | `build` and `build:dev` strings extended with ` && esbuild …` | an npm script is one JSON string |
  | `unified-webapp-example.json` | trailing comma on the previous `host_routing` entry and on the previous top-level section | JSON object syntax |
  | `package-lock.json` | regenerated by `npm install` | normalization is expected; only the new React/router entries should be semantically new |
  | `go.mod` | three requires move from the `// indirect` block into the direct block | **Step 2.5's `go mod tidy` requires this** — a line removal by design. The net `$BASE`→HEAD diff may squash it into a single add, but do not rely on that. R11's contingency (bumping the root `go` line) is likewise legitimate if it arises |
  | `go.sum` | entries added; a line may also be **pruned** by `go mod tidy` | acceptable — tidy prunes hashes no longer reachable |
  | `README.md` | trailing-comma edits on the current last entries of the sample JSON (the `host_routing` list at README.md:49, and the `multissh` block) | JSON object syntax, same cause as the example config |

  **Do not apply a blanket "every hunk must be an addition" test** — seven of these ten files
  are modified in place by design, and a gate that fails on a correct run is a gate ralph
  learns to ignore. The rule is Principle 3's, and only Principle 3's: **no hunk in any of
  the ten may remove or alter a line belonging to another module.**
- **Other-module regression boot** — see the sub-section immediately below.

#### Other-module regression boot — scripted, with a real assertion

Under the normative harness, after the issuetracker server has been killed (the harness's
pre-boot port-free check enforces that). `todo` is the choice because it needs no external
state: `DefaultConfig()` supplies `./web/todo`, which is present, and `NewStore` creates
`./data/todo`. Config body — the whole body, since every other section falls back to
`DefaultConfig()`.

This block sits at column 0 on purpose — the `JSON` terminator must stay there; **do not
re-indent it to match the surrounding list.**

```bash
cat > "$CFG" <<'JSON'
{"port":8080,"host_routing":{"localhost":"todo"}}
JSON
```

Assertion — **required**, because the status-agnostic probe passes against
`unavailableHandler`'s 503 just as happily as against a working module:

```bash
code=$(curl -s -o /tmp/other -w '%{http_code}' localhost:8080/)
[ "$code" = 200 ] || { echo "FAIL: todo returned $code"; cat /tmp/other; exit 1; }
if grep -q 'module unavailable' /tmp/other; then
  echo "FAIL: todo failed to build"; cat /tmp/other; exit 1
fi
```

Note the `if` form rather than `grep … && { …; exit 1; }`: this is an assertion that a
pattern is **absent**, and the `&&` form would return grep's own status 1 on the healthy-todo
path — failing the final acceptance gate on a correct run. Then the harness teardown
(`kill`/`wait`/`rm -rf "$CFGDIR"`/`trap - EXIT`/`exit 0`).

Until this iteration the check was prose ("confirm it still serves") and before that it
borrowed an assertion by accident — `curl -f /api/health` happened to get a 200 from
`todo`'s weak fallback handler, which returns 200 for that path without implementing it.
Neither proved the module built. The lines above do.

**Recovery:** this step changes no files, so recovery is only cleanup: kill any surviving
server (`kill $SRV`, or `kill "$(cat /tmp/it-server.pid)" 2>/dev/null; rm -f
/tmp/it-server.pid` under the PID-file fallback), then
`rm -f /tmp/it-server.log /tmp/body /tmp/neg /tmp/other /tmp/it-notadir /tmp/it-server.pid`,
and re-run. If a
*gate* fails, do not patch at Step 8 — identify the owning step, recover to it, and
re-execute that step; a fix applied here lands outside any step's verification.

---

## Risks and mitigations

| # | Risk | Mitigation |
|---|---|---|
| R1 | `modernc.org/sqlite` fails or misbehaves cross-compiling to `linux/arm64`. | `make build-rpi` is verified at Step 4, before the frontend work lands — early enough that a failure invalidates the dependency choice rather than the whole port. |
| R2 | `auth.Provider.Middleware` wraps the whole mux and could gate `/graphql` or the static SPA in a non-`none` mode. | Step 4 requires reading `auth/middleware.go` and preserving its path exemptions verbatim before writing `build.go`. Step 6's GraphQL tests cover the token path. |
| R3 | `ldap` / `ldap_passkey` modes cannot be exercised in this environment. | Accepted and scoped: ported faithfully, compile-checked, config-round-trip tested. Runtime verification is deferred and recorded as **OQ-2**. |
| R4 | The platform's global CORS layer sets `Allow-Headers: Content-Type` only, so a genuine **cross-origin** browser call carrying `Authorization` to `/graphql` would fail preflight. | Out of scope by D3 — fixing it means editing shared middleware for all modules. Same-origin UI calls and `curl` (which does not preflight) are unaffected, so no acceptance criterion is at risk. Recorded as **OQ-3**. |
| R5 | The committed `bundle.js` drifts from its sources when someone edits `web/issuetracker/js/` without re-running `make web`. **And, unique to this module, `build` and `build:dev` emit behaviorally different bundles to the same tracked path** — dev ships `react.development.js` (1.26 MB) and re-activates `StrictMode`'s double-invocation of all 10 effect call sites. So a committed `build:dev` artifact is not a cosmetic size regression, it is a behavior change, and it looks identical in `git status` to a legitimate rebuild. | The drift risk is a pre-existing repo-wide property `multissh` already has; adopting the convention (Step 5.7) inherits it rather than introducing it. The dev/prod divergence is new and is gated at two points: Step 5's `grep -c "react.development"` + size band, and Step 8.11's `git status --porcelain -- web/ package-lock.json` after a fresh `make web`. Accepted and documented; a CI job that rebuilds and diffs would be the real fix and is out of scope. |
| R6 | `go mod tidy` pulls ~20 new indirect dependencies, widening the supply-chain surface. | Unavoidable given Decision 3A. Step 1 pins to the reference's exact versions and requires any substitution to be recorded. |
| R7 | The API token is written to the server log at every boot. | Reference behavior, preserved per FRD §2/§4. Noted in the README so operators know the log is sensitive. |
| R8 | **The SPA client route `/api` collides with the REST prefix `/api/`.** A fallback guard written as `HasPrefix(path, "/api")` 404s the API-docs page on hard refresh while in-app navigation keeps working — it survives casual testing. | Guard keys on `"/api/"` with the trailing slash (Step 4.2); pinned by an explicit `GET /api` → `index.html` case in `build_test.go` (Step 6.4), by the `/api` entry in Step 5's scripted 13-route sweep, and by the browser hard-refresh check in **Step 8's human gate**. |
| R9 | `GET /api/token` and `GET /api/bootstrap` expose the GraphQL **write** token unauthenticated in the default `mode: "none"` — and because the shared CORS layer sets `Allow-Origin: *`, a plain cross-origin `fetch()` (a CORS *simple request*, no preflight) means **any site the operator's browser visits can read and exfiltrate it**. | Faithful reference behavior — but *not* the same posture as `multissh`, whose README caveat ("reaching its hostname is the whole access boundary") does **not** apply here, because multissh exposes no bearer token to steal. Step 7 must state the real threat model plainly in the README. The stronger fixes (gate `/api/token`, or narrow CORS for this module) are recorded as **OQ-5** for a knowing decision. |
| R10 | Auth sessions and WebAuthn challenges are stored **in memory with no background eviction** — restarts drop all logins, the challenge map grows without bound for abandoned ceremonies, and the module cannot be run multi-replica. | Only reachable in `ldap`/`ldap_passkey` modes, which are out of scope for verification here (R3). Ported as-is per "port, don't redesign"; recorded as a follow-up, not fixed in this change. |
| R11 | The reference's `go.mod` declares `go 1.26.3` while the root module declares `go 1.26.2`. | Not a merge — the source files simply join the root module and obey its directive. Nothing in the ported code needs a 1.26.3 feature (the newest thing used is Go 1.22 `ServeMux` patterns). If `go build` objects, bump the root `go` line and note it. |
| R12 | **`db.Open` calls `d.SetMaxOpenConns(1)`, so every issuetracker request serializes behind one connection** — inside a binary that also serves five other modules. A slow query blocks all issuetracker traffic (not other modules, which have separate handlers, but the effect is invisible from the outside). | Ported faithfully — it is what prevents SQLite lock churn under WAL, and changing it is a redesign. Recorded so operators reading the README understand the concurrency profile rather than diagnosing it under load. |
| R13 | **`db.Open` builds its DSN with a raw `fmt.Sprintf("file:%s?_pragma=…", path)`.** A `db_path` containing a space, `?`, or `#` corrupts the DSN — and this is now *more* reachable than in the reference, because Step 3 adds tilde expansion, so a home directory with a space in it flows straight in. | Faithful port, but the new config surface widens the exposure. **Note what does *not* cover this:** Step 4's negative gate exercises `os.MkdirAll` failing `ENOTDIR`, which happens before the DSN is ever built — a malformed DSN with a valid parent directory sails straight past it. The actual mitigation is two things: `db.Open`'s `Ping` fails, so `Build` returns a wrapped error and the module 503s with the cause rather than misbehaving silently (Principle 4 doing its job generically); and the constraint is documented in the README's `db_path` row so an operator does not have to discover it. Escaping the DSN would be a behavior change and is left as a follow-up. |
| R14 | The ~20 new indirect dependencies are unreviewed transitively, and they include a networked LDAP client and a WebAuthn implementation. | Step 1 bounds version drift to same-minor patch bumps. **Step 8 runs a one-time `govulncheck ./...`** as a report-and-stop gate (OQ-7, closed). |

## Assumptions

- `reference/issue-tracker/` stays present and unmodified throughout execution; it is the
  source of truth for parity and is deleted by nobody (only gitignored).
- Network access to `proxy.golang.org` and `registry.npmjs.org` remains available
  (both verified reachable during planning).
- Feature parity is judged against the **code**, not the reference `README.md`, which is
  stale — it claims "Login/auth for the UI is intentionally skipped for now" while
  `internal/auth/` ships LDAP and WebAuthn and the frontend ships `LoginPage.tsx`.
- The reference DB (`backend/newlinear.db`) is not migrated; the module seeds a fresh
  database on first run, per FRD §4.
- The reference ships **no tests**, so "feature parity" cannot be verified by running an
  existing suite against the port. Parity is established by the fact that the packages are
  copied verbatim modulo import paths, plus the Step 8 end-to-end smoke. The tests written
  in Step 6 are new coverage, not a parity check.
- The module stays **host-routed**. `auth.isPublic` and every registered route are literal
  absolute paths; mounting this module under a URL subpath would break login and could
  turn authenticated API GETs into public traffic. Nothing in this plan should be read as
  making a subpath mount supported.

## Open Questions

- **OQ-1** — **This is the complete and only list of deviations from reference behavior in
  this port.** Four, all hardening, none reachable from the UI in `mode: "none"`:
  1. unmatched `/api/…` → JSON 404 instead of `index.html` at 200 (Step 4.2);
  2. non-GET/HEAD to a client route → 405 `Allow: GET, HEAD` instead of `index.html`
     (Step 4.2);
  3. a missing `static_dir` → `Build` returns an error and the dispatcher serves an
     explicit 503, instead of the reference's log-a-warning-and-register-no-`/`-handler
     (`backend/main.go:45-52`), which leaves every client route 404-ing from an empty mux
     while the process reports a healthy start (Step 4.1, `checkStaticDir`). A
     `static_dir` that exists but is **not a directory** also errors, which has no
     reference analogue at all — the reference only stats.
  4. **`EnsureToken("default")` failing → `Build` returns an error**, where the reference
     writes `token, _ := st.EnsureToken("default")` (`backend/main.go:34`) and serves on
     with an empty token (Step 4.1). This is the one deviation the plan *argues for* at
     length but had not listed: an empty token makes `ValidToken("")` false, so `/graphql`
     rejects every request — at HTTP 200, with an `errors` body — while the process looks
     healthy. Under Principle 4 that silent degradation is exactly what must become a 503.
     Same reasoning applies to the `Seed()` error, though there the reference already
     `log.Fatalf`s, so promoting it is parity rather than deviation.

  **Deviations 1–3 are each pinned by a Step 6.4 test row. Deviation 4 is not**, and cannot
  cheaply be: forcing `EnsureToken` to fail inside a unit test means an unwritable or
  corrupted DB, which is a different fixture from the rest of that table. It is covered
  *indirectly* — Step 4's negative gate proves the general shape (a `Build` error reaches the
  503 body with its cause intact), and Step 4.1 pins the code that returns it. Worth knowing
  which of the four has direct coverage and which has inference.

  *Confirm all four are wanted.* Dropping 1 or 2 means writing a custom handler instead of
  adopting multissh's — the deviations and the reuse are a package. Dropping 3 or 4 means
  giving up Principle 4 at exactly the points it was written for.

  **A previous draft listed a different deviation, "directories never list" — that was wrong
  and has been removed; it is unrelated to item 4 above, which was added later.** The
  reference's `spaHandler` tests `err == nil && !info.IsDir()`, so directory
  requests already fall through to `index.html` there. The non-listing behavior is a
  difference from `internal/todo`'s handler, not from the reference, and requires no
  confirmation.

  A separate and much narrower case rides along with deviation 1: **`GET /api/` (trailing slash) returns the JSON
  404, while React Router v6 would match `<Route path="/api">` for it.** So a hard refresh
  of `/api/` — typed by hand; nothing in the app generates it — breaks. The plan
  **accepts** this rather than special-casing it, because exempting `/api/` would put a
  hole exactly at the prefix boundary the guard exists to defend. Pinned as a test row so
  the behavior is chosen, not incidental. *Confirm, or ask for the exemption.*
- **OQ-2** — `ldap` and `ldap_passkey` auth modes ship compile-checked but runtime-unverified.
  *Is that acceptable for this port, or is a manual verification pass against a real LDAP
  server required before merge?*
- **OQ-3** — The platform CORS layer does not allow the `Authorization` header, so
  cross-origin GraphQL from a browser would fail preflight. Left alone to protect D3.
  *Should a follow-up widen the shared middleware, or is same-origin-only the intended contract?*
- **OQ-4** — `passkey.store_path` moves from the reference's cwd-relative
  `newlinear-passkeys.json` to `./data/issuetracker/passkeys.json`. *Confirm the relocation
  is desired* (it follows repo convention but is not a pure port).
- **OQ-5** — **The GraphQL write token is exfiltratable by any website the operator
  visits, in the default `mode: "none"`.** The earlier framing of this ("reaching the
  hostname is the access boundary") was wrong and understated it. Two behaviors compose —
  one from the host platform, one from the reference:
  1. `internal/platform/middleware.Wrap` sets `Access-Control-Allow-Origin: *` on **every**
     response from every module;
  2. in `mode: "none"` the provider is `auth.NoAuth`, so **no** endpoint is authenticated —
     `GET /api/token` and `GET /api/bootstrap` (which also returns the token) included.
     Note this is *not* `isPublic`'s doing: `/api/token` is an `/api/` path, so
     `isPublic`'s non-`/api/` GET exemption never applies to it. `NoAuth` alone accounts
     for the exposure.

  A plain `fetch()` GET from any origin is a CORS *simple request* — no preflight — so any
  page loaded in the operator's browser can read the token and send it anywhere. The
  attacker's *subsequent* browser-side GraphQL writes would hit preflight, but that is
  irrelevant: the token has already left, and it is usable from anywhere.

  This is faithful parity with the reference, not a defect introduced by the port — the
  reference ships the same permissive CORS layer. But the README wording must state the
  real threat model ("any site the operator's browser visits", not "the hostname is the
  boundary"), and this deserves an explicit decision. *Options: accept and document;
  gate `/api/token` behind auth; or narrow the CORS origin for this module only.* The
  plan currently assumes accept-and-document, which is the only option that touches no
  shared code — but it is the weakest of the three and should be confirmed knowingly.
- **OQ-6 — mostly closed, one follow-up left open.** *Closed:* `internal/multissh/sshproxy`
  is the in-repo precedent for a module-private subpackage; `internal/platform/` is for
  genuinely shared code, and nothing else in the repo uses SQLite or auth — so the ported
  `{db,models,store,api,auth,graphql}` packages stay module-private. No decision needed.

  *Still open:* Step 4.2 copies ~60 lines of static-handler code out of
  `internal/multissh/server.go`. That closure argument was about SQLite and auth and does
  not cover this: the copy is **not** covered by `internal/multissh/static_test.go`, and any
  future hardening of the SPA fallback now has to land in two places. Extracting a shared
  `internal/platform/web.SPAHandler` would fix it, but that means editing multissh, which D3
  forbids in this change. *Should that extraction be scheduled as a follow-up, or is the
  duplication acceptable at two modules?* (Revisit if a third module ever wants it.)
- **OQ-7 — CLOSED, folded into Step 8.** A one-time `govulncheck ./...` now runs in Step 8's
  build-and-test gate. The reasoning: this change is the largest single expansion of the
  repo's dependency graph, it adds a networked LDAP client and a WebAuthn implementation,
  and Step 8 is the one moment where the full closure exists with nothing else in flight.
  It is a report-and-stop gate, not an auto-upgrade — acting on a finding means changing a
  Step 1.3 pin, which is a plan change. No decision needed.
