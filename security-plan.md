# Implementation Plan — Platform security & auth (round two)

**Date:** 2026-09-10
**Status: pending approval — consensus reached (iteration 3; Architect APPROVE, Critic APPROVE)**
**Branch:** `security-fix` — commits allowed, **NEVER push** (NFR-6)
**Authoritative requirements:** `/opt/unified-webapp/security-FRD.md` (FR-P*, FR-A*, FR-O*, FR-R*, FR-M*, NFR-1..6, AC 1–15). On any conflict with prior documents, **the FRD wins.**
**Reused design source:** `/opt/unified-webapp/shelved/plan-with-platform-auth.md` (v8) — task granularity, LDAP/WebAuthn porting notes, origin-middleware specifics (4.6a/4.6b), risks R1/R9. The FRD deliberately diverges from v8 on: universal fail-closed gate (no public/private prefix lists), stateless JWT sessions (no server-side session store), per-module accepted-method lists, the `admin` module with break-glass operator PIN, and API keys as a fourth authenticator.
**Baseline (already on the branch, uncommitted):** Go 1.26.2, deps bumped (`fsnotify` 1.10.1, `sftp` 1.13.11, `x/crypto` 0.57.0, `x/sys` 0.48.0), `make test` (`go test -race ./...`) green. This plan builds on it; Phase 0 verifies it, nothing re-does it.
**Build/test commands:** `make test` (= `go test -race ./...`), `make build`, `npm run build` (esbuild web assets).
**Executor calibration:** tasks are written for a Sonnet executor — explicit file paths, signatures, and a runnable verify per task; many small tasks over few large ones.

**Design invariant (more modules are coming):** adding module N+1 with full security posture must require only (a) a config section + `host_routing` entry, (b) the standard `Build(cfg) (http.Handler, error)` + one `buildModule` case, and (c) optionally one `auth.modules` entry naming its accepted methods. Everything else — static serving, broker cap, response helpers, path confinement, origin checks, body limits, headers, the auth gate, the login page — is inherited from `internal/platform/*` and the dispatcher with zero per-module code. Every task below must preserve this invariant; `docs/adding-a-module.md` (T6.2) states it.

---

## 1. RALPLAN-DR Summary

### Principles

- **P1 — Fail closed by construction, not by predicate.** No per-module public-path lists anywhere. The gate protects *everything* on a protected module except a fixed allowlist the gate itself owns. There is nothing to misclassify (the C4 hazard class from the shelved plan is eliminated, not fixed).
- **P2 — Server-side only; the frontend is a convenience.** Deleting all JS must not weaken any guarantee (NFR-1). No control in this plan depends on the login page, the admin SPA, or any module SPA.
- **P3 — Consolidate first, secure once.** The FR-P extractions land before the security features so that the origin checks, body limits, broker cap, and gate live in platform code every current and future module inherits (the design invariant above).
- **P4 — Additive config, calibrated-off escape hatches.** A config file from today loads unchanged and produces today's behavior (auth off; `origin_check: "enforce"` is the one new default, README-noted). Every enabled-by-default control has a relax switch; nothing bricks a module on a proxy quirk.
- **P5 — Every phase independently green.** Each phase ends with `make test` green under `-race`, goleak green, and a commit checkpoint on `security-fix`.

### Decision Drivers (top 3)

1. **D1 — The FRD is final and self-contained.** Fifteen acceptance criteria gate this work; every task must trace to an FR/AC and every FR/AC to a task. Where the shelved v8 design conflicts, the FRD's five divergences win.
2. **D2 — Home lab, single operator, 127.0.0.1 behind a TLS-terminating, Host-preserving proxy.** Rigor is calibrated: real fixes for real breakage, deliberate deferrals recorded once (§8), no enterprise hardening theater. But the backend never trusts the frontend, and auth toggles fail closed.
3. **D3 — Sonnet executes this plan.** Tasks are mechanical where possible, with exact files, signatures, and verify commands. Reviewed v8 detail is reused verbatim where still valid rather than re-derived.

### Options considered

**Option A — Universal fail-closed gate + stateless JWT + per-module method lists (the FRD design). CHOSEN.**
Every module is wrapped by the gate at registration; a protected module requires a valid session on *every* route (static assets included) except the gate-owned allowlist; sessions are HS256 JWTs carrying an accumulating method set; the gate check is a set intersection between token methods and the module's `auth.modules` entry.

- Pros: eliminates the public-path predicate and its whole defect class (v8 spent three iterations on C4-shaped bugs); no per-module route inventory as a gate dependency; sessions survive restarts with no store; per-module policy is one literal list in config; live-apply is an atomic pointer swap; module N+1 inherits everything via one config entry.
- Cons: unauthenticated users can't load static assets — paid for by the gate-owned login page (FR-A12); no per-session revocation — accepted non-goal, key-file rotation is the global lever (FR-A2, §8); method accumulation adds token re-issue logic (FR-A1b).

**Option B — v8's two-list `PublicPrefixes`/`PrivatePrefixes` gate + server-side session store.**
The shelved, four-times-reviewed design: assets public before login, data routes gated per a per-module prefix table (v8 task 0.2b), sessions in an in-memory store.

- Pros: module SPAs load before login (no platform login page needed); sessions are revocable server-side; the design detail is already reviewed.
- Cons: the prefix predicate is exactly the hazard v8's own history documents — every module is a catch-all-at-`/` SPA with data routes inside the prefix space, the table must be re-verified per module and re-maintained per new route, and a stale entry fails *open* on a gated module; the boolean protected list cannot express per-module methods; the session store dies on restart and adds state the FRD explicitly removed.
- **Rejected** because the FRD supersedes it by name (§3.3 "deliberate divergence") and because the per-module inventory cost violates the module-N+1 invariant: every new module (more are coming) would add a fail-open-on-drift table row. The v8 route inventory still informs the origin-check tests (T2.3), which is all the FRD keeps of it.

**Option C — Auth at the proxy (nginx basic-auth / oauth2-proxy), binary untouched.**

- Pros: near-zero code; battle-tested.
- Cons: cannot express per-module method lists, passkeys-per-module, API keys with per-module scope, or the admin live-apply; moves security config outside the repo and the config file (breaking FR-M4's single source of truth); the FRD's backend-never-trusts-frontend posture would rest entirely on an external process. **Rejected on requirement coverage** — FR-A5..A8, FR-M1..M6 are unimplementable at the proxy.

---

## 2. Pre-mortem (deliberate mode)

*It is December 2026. This shipped. Three ways it failed.*

**Scenario 1 — "A module was protected on paper and open in practice."**
The operator added `obsidianoid` to `auth.modules` via the admin UI, saw the login page on slideshow, and assumed the posture was uniform. But a later refactor of `cmd/server/main.go` registered a new module's handler outside the memoization map, so the gate never wrapped it — every test stayed green because the auth tests named modules by hand.
*Mitigations in this plan:* the gate is applied at exactly one choke point — inside `buildDispatcher`'s existing memoization loop (T4.5), the only place handlers are ever registered; the AC-2 test **iterates `auth.modules` from config** rather than naming modules (T4.6); the AC-1 invisibility test asserts `GET /api/auth/mode` answers 200 on **every routed hostname**, which proves the gate wraps every module — a module the gate missed has no mode route and fails that test even when unprotected; and the gate's protected-predicate is `protected := (module has an auth.modules entry) || module == "admin"` — admin is protected whenever routed, regardless of matrix contents, so neither an absent `"admin"` key nor a live matrix save that deletes admin's entry can unprotect it (converged reviewer ruling, iteration 1 — T4.2 step 3, T5.2 tests).

**Scenario 2 — "The admin module ate the config file."**
A save from the admin UI rewrote the whole config through `json.Marshal`, silently dropping an unknown key another tool had added and reordering everything; a later hand edit merge-conflicted; eventually a bad save half-applied and the operator was locked out.
*Mitigations:* one validator shared by boot and save — a rejected save provably changes nothing on disk or in memory (T5.3, AC-13); the file rewrite is surgical — a `json.Decoder` offset walk locates the byte range of the top-level `"auth"` member and only those bytes are replaced, tmp + rename at 0600 — so every byte outside the auth member is preserved literally (T5.3, AC-14); the operator PIN is defined outside the UI's reach and always accepted on admin (T5.2, AC-12), so no sequence of UI edits can lock the operator out.

**Scenario 3 — "One nginx line 403'd every write in every module."**
A proxy config regeneration switched `proxy_set_header Host $host` to `$proxy_host`. The origin write-check (FR-O2) compares `Origin` against `r.Host`, so every browser write in todo, grocery, and the rest started returning 403 — modules that had worked for years, broken by a header the operator never thinks about (risk R1).
*Mitigations:* `server.origin_check: "log"` is a one-line recovery that answers normally while logging would-be rejections (T2.3, FR-O3); every rejection logs the observed `Origin`/`Host` pair so the diagnosis is one log line (FR-O2); the README documents the Host-preservation proxy contract as load-bearing (T6.1); dispatch already requires a preserved Host, so a rewriting proxy breaks *routing* loudly before it breaks writes subtly.

---

## 3. Decisions taken where the FRD leaves latitude

Recorded here so reviewers see them as choices, not accidents. Each is implemented by the named task.

| # | Decision | Task |
|---|---|---|
| L1 | **Phase order:** consolidation (FR-P) → origin/hardening (FR-O, FR-R) → auth core (FR-A authenticators/sessions) → gate + mounting → admin (FR-M) → docs. Origin checks land before auth so they demonstrably sit outside the gate from birth. | §4 |
| L2 | **New config sub-section `"server"`** carrying `origin_check` (FRD names `server.origin_check`) and `sse_max_subscribers` (broker cap default 64). Existing top-level fields untouched. | T1.3, T2.3 |
| L3 | **`max_body_bytes` is a per-module-section field** (each module config struct gains it), default 1 MiB applied at registration; multissh's default derived from `max_upload_bytes` + 1 MiB multipart overhead. | T2.6 |
| L4 | **413 mapping:** a shared `response.WriteDecodeError(w, err)` maps `*http.MaxBytesError` → 413, everything else → 400; decode-error sites updated mechanically. | T2.6 |
| L5 | **Session cookie name `uw_session`.** No compatibility constraint exists (round one shipped no auth; the reference's `multissh_session` store is not ported). | T3.2 |
| L6 | **Operator PIN is a distinct token method `"admin_pin"`** (identity `admin`). The admin gate's effective accepted set is `matrix ∪ {"admin_pin"}`; the validator rejects `"admin_pin"` as a method name in any `auth.modules` list, so a named PIN entry cannot impersonate the break-glass path. This is how "always accepts the operator PIN, regardless of the matrix" (FR-M2) is made spoof-proof without letting every named PIN into admin. **Explicit deviation from FR-M2's literal wording:** FR-M2 says the operator PIN logs in with "method pin"; a literal `"pin"` method would let any named-PIN token satisfy admin's union set, so the token records `"admin_pin"` instead — both reviewers independently endorsed this encoding in iteration 1 (settled). Corollaries: `GET /api/auth/mode` on admin reports the **effective** set including `"admin_pin"` (never `[]`), and the login page shows the PIN form when methods intersect `{"pin","admin_pin"}` (T4.2 step 2, T4.4). | T3.4, T4.2, T5.2 |
| L7 | **The login page is `go:embed`-ed in `internal/platform/auth`** (one HTML file with inline CSS/JS, including the WebAuthn ceremony calls). The gate must serve it with no filesystem or build-pipeline dependency; module `static_dir` conventions don't apply to a platform-owned page. | T4.4 |
| L8 | **`auth.FromConfig(a config.AuthConfig, knownModules []string, adminRouted bool) (*Service, error)`** — two return values (FRD §3.3 wording; the v8 three-value form existed for a Provider type this design doesn't have). `adminRouted` is threaded in because the "admin routed with neither/both PIN forms" validation rule needs routing info, which lives on `config.Config` (`Routing`, config.go:18), not on `AuthConfig` — and `knownModules` is the `buildModule` universe, not the routed set (converged reviewer ruling). `Service` is always non-nil on success, even with auth absent (empty policy), so the gate wrap is unconditional and uniform. The known-module-name list comes from `cmd/server/main.go` (the `buildModule` switch owns it); `adminRouted` = whether `"admin"` appears among `host_routing` values. | T4.5 |
| L9 | **Boot-error means refuse to start** (`log.Fatalf` after `config.Load`/`FromConfig`): unknown module in `auth.modules`, method with no configured authenticator, admin routed with neither/both of `admin_pin`/`admin_pin_file`, PIN file with loose mode. The FRD says "boot errors"; a home-lab binary that refuses to boot on a security typo is strictly clearer than a degraded state. (This intentionally differs from build-failure 503s, which remain per-module.) | T3.1, T4.5 |
| L10 | **`store.go` ports partially:** `passkeyStore` and `challengeStore` are needed (FR-A8 keeps credential persistence and in-memory challenge state) and port; `sessionStore` does not (JWT replaces it). The reference `service.go` passkey ceremonies are rewired from session-IDs to gate-verified JWT identities. | T3.8 |
| L11 | **Admin SPA is plain JS/CSS** under `web/admin` (like grocery/todo) — no TypeScript, no `package.json`/build-script change. | T5.5 |
| L12 | **`admin.Build` deviates from the standard signature** — `admin.Build(cfg *config.Config, deps admin.Deps) (http.Handler, error)` where `Deps{Service *auth.Service, ConfigPath string, KnownModules []string, AdminRouted bool}` — because live-apply needs the auth service, the config file path, and the exact arguments boot passes to `ValidatePolicy` (so FR-M3's "same code path" is literally the same call). Recorded as an FR-M6 sanctioned special case; no other module gets it. | T5.1 |
| L13 | **Grocery keeps its JSON 405 bodies** under method patterns by registering a bare-path fallback per route path that answers `{"error":"method not allowed"}` — Go 1.22 patterns alone would emit the stdlib's plain-text 405 and change the body shape FR-P5 says to keep. | T1.5 |
| L14 | **`0.0.0.0` bind stays.** The FRD's deployment context says 127.0.0.1 behind the proxy, but the bind address is an operator/deployment matter the FRD gives no FR for; v8's O-4 note (bind understood, wanted for testing) carries forward. README note only. | T6.1 |
| L15 | **Open questions** live in §9 of this document, not a separate file (this plan modifies only `security-plan.md`). | §9 |

---

## 4. Implementation Phases

Each task: **Goal / Files / Do / Verify / Satisfies.** Tasks within a phase are ordered; phases are sequential. Every phase ends: `make test` green, commit checkpoint on `security-fix`, never push.

### Phase 0 — Baseline & dependencies

**T0.1 — Verify the baseline.**
Files: none (read-only).
Do: `git rev-parse --abbrev-ref HEAD` → `security-fix`; `make test` green; `npm run build` green; `CGO_ENABLED=0 go build ./...` green. Do not proceed on red — report first.
Verify: the four commands above.
Satisfies: NFR-4, NFR-6 precondition.

**T0.2 — Add auth dependencies.**
Files: `go.mod`, `go.sum`.
Do: `go get github.com/golang-jwt/jwt/v5 github.com/go-ldap/ldap/v3 github.com/go-webauthn/webauthn golang.org/x/term` then `go mod tidy`. (`x/term` is for `-hash-pin`'s no-echo TTY prompt, T3.4; `golang.org/x/crypto` for bcrypt and `go.uber.org/goleak` are already present.)
Verify: `CGO_ENABLED=0 go build ./...` && `make test`.
Satisfies: FR-A1/A5/A8 prerequisites; NFR-4.

**T0.3 — Record standing constraints (no code).**
Do: confirm and note in the commit message for Phase 0: (a) no new middleware may wrap `http.ResponseWriter` — multissh WebSocket upgrades need `http.Hijacker` (FR-A13); (b) `buildDispatcher`'s memoization map (`cmd/server/main.go:96-114`) is the single registration choke point — the gate, body limit, and any per-module wrap land there; (c) the shared same-origin predicate already exists at `internal/multissh/sshproxy/proxy.go:303` (`sameOrigin`) and `:319` (`originHost`) and will be lifted, not duplicated.
Verify: text present in commit message.
Satisfies: FR-A13 groundwork.

---

### Phase 1 — Platform consolidation (FR-P) — behavior-preserving

Ground rule for the whole phase: **existing module tests stay green unmodified except import paths** (FRD §2). Any test that needs a semantic edit means the extraction changed behavior — stop and fix the extraction.

**T1.1 — `internal/platform/static` (FR-P1).**
Files: new `internal/platform/static/static.go` + `static_test.go`; edit `internal/{grocery,todo,menuserver,obsidianoid,slideshow}/build.go`.
Do: create `package static` with `type Handler struct{ Dir string }` and `func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request)` reproducing the byte-identical copies exactly (grocery `build.go:42-53`, todo `:29-40`, menuserver `:27-38`, obsidianoid `:63-74`, slideshow `:32-43`): rooted-clean join `filepath.Join(h.Dir, filepath.Clean("/"+r.URL.Path))`, serve `index.html` on miss (SPA fallback). Replace the five module-local `staticHandler` types with `static.Handler{Dir: ...}`. **multissh's four-row static handler stays module-local** (different, tested semantics — do not touch).
Verify: `go test -race ./internal/...` green; `grep -rn "staticHandler" internal/ --include='*.go' | grep -v multissh` → empty.
Satisfies: FR-P1; NFR-3; AC-10.

**T1.2 — `internal/platform/fspath` (FR-P3).**
Files: new `internal/platform/fspath/fspath.go` + `fspath_test.go`; edit `internal/todo/handler.go`, `internal/menuserver/handler.go`, `internal/slideshow/store.go`, `internal/slideshow/music.go`, `internal/obsidianoid` (its prefix check site), `internal/multissh/files.go`.
Do: `func ValidName(s string) bool` — exact body of the duplicate at `internal/todo/handler.go:238` / `internal/menuserver/handler.go:70` (non-empty, no leading dot, no `/`, no `\`). `func ConfineTo(root, rel string) (string, error)` — the `filepath.Rel`-based check from `internal/slideshow/store.go:108-112`: join, `Rel`, reject `..` escape; returns the joined absolute path. Migrate: todo + menuserver `validName` call sites → `fspath.ValidName`; slideshow `ImagePath`/`AudioPath` → `fspath.ConfineTo`; obsidianoid's prefix check → `fspath.ConfineTo` where the call shape allows; multissh's `resolveWithinRoot` (`files.go:76`) keeps its `(absDir, relClean, err)` signature but delegates its escape check to `fspath.ConfineTo`. **Semantics identical — this is consolidation, not a fix** (all five call sites were verified correct in the sweep). **Executor note (obsidianoid):** `vault.go:83/92` passes an already-joined absolute path — confirm the call shape actually fits `ConfineTo(root, rel)` before migrating; if it does not, add a second helper (e.g. `ConfineAbs(root, abs string) error`) rather than force-fitting the migration — and if that fallback helper lands, `fspath_test.go`'s table-driven tests must cover it with the same traversal table (the test list below predates the fallback). Table-driven tests: traversal (`../`, `..\\`, absolute, dot-prefixed), clean names, exact parity with the old behavior.
Verify: `go test -race ./internal/...` green with module tests unmodified; `grep -rn "func validName" internal/` → empty.
Satisfies: FR-P3; NFR-3; AC-10.

**T1.3 — One SSE broker with a subscriber cap (FR-P2, FR-R3 / S-5).**
Files: `internal/platform/broker/broker.go`, `broker_test.go`, `multirouter.go`; delete `internal/slideshow/broker.go`, `internal/obsidianoid/events.go` broker half; edit their call sites; `internal/platform/config/config.go` (new `Server` section, see L2).
Do: extend `platform/broker` in place (grocery and todo already import it — no churn for them):
- `Broker` gains payload events — `func (b *Broker) Publish(data string)` — alongside the existing signal `Notify()`; subscriber channel becomes `chan string` internally with `""` as the signal sentinel, or keep two maps — executor's choice, provided both existing `Broker` tests and the migrated slideshow/obsidianoid tests pass unchanged.
- Optional snapshot-on-connect: `func (b *Broker) ServeSSE(snapshot func() string) http.HandlerFunc` reproducing `internal/slideshow/broker.go:50-62` (snapshot sent first when non-nil); the existing `ServeHTTP` remains for signal-only users.
- **Per-broker max-subscriber cap:** `func (b *Broker) SetMaxSubscribers(n int)`; at cap, a new SSE connect gets **503** before any headers stream; existing streams unaffected. Default 64. **Config→broker path (Architect I2, the iteration-2 carryover):** brokers are constructed inside module Build functions that receive only their own config section (`grocery.Build(cfg.Grocery)` at main.go:119-129; broker sites grocery/build.go:32, todo/build.go:18, slideshow, obsidianoid/build.go:38-40), so `cfg.Server` is unreachable there and Build signatures cannot change (module-N+1 invariant). Mirror the existing expander pattern at config.go:206-221: `config.Load` copies the effective cap (`cfg.Server.SSEMaxSubscribers`, 0 → 64) into a new `SSEMaxSubscribers int \`json:"-"\`` field on each broker-owning module's config struct (grocery, todo, slideshow, obsidianoid); each Build passes its own field to the broker's `SetMaxSubscribers`. The `json:"-"` tag is a directed final ruling: `Load` clobbers the field unconditionally, so a serializable tag would create a silently-dead per-module knob and `WriteDefault` would emit a phantom setting; it has zero AC-14 interaction (the T5.3 splice never re-marshals module sections).
- `MultiRoomBroker` retained; rooms inherit the cap.
- Migrate slideshow (`SSEBroker` → `broker.Broker` with `ServeSSE(snapshot)`) and obsidianoid (`eventBroker` → `broker.Broker`); **their tests carry over** with import-path edits only.
Add `ServerConfig struct { OriginCheck string \`json:"origin_check"\`; SSEMaxSubscribers int \`json:"sse_max_subscribers"\` }` and `Server ServerConfig \`json:"server"\`` on `Config` (`internal/platform/config/config.go:14`), plus the cap copy-down in `Load` described above — `origin_check` is consumed in T2.3.
Verify: `go test -race ./internal/platform/broker/... ./internal/slideshow/... ./internal/obsidianoid/...` green; goleak green (`go test -race ./internal/multissh/...` still passes); new cap test: 64 subscribers connected, 65th gets 503, first 64 still receive events; and a config-path test: `Load` of a config with `sse_max_subscribers: 2` → a module built from it 503s the **third** subscriber through a real module route (proves the copy-down, not just the broker unit).
Satisfies: FR-P2, FR-R3; AC-8, AC-10; NFR-4.

**T1.4 — `platform/response` everywhere (FR-P4).**
Files: `internal/obsidianoid/handler.go` (16 `http.Error` sites, 2 JSON `fmt.Fprintf` sites), `internal/slideshow/handler.go` (`http.Error` at :95, :100, :110, :115), `internal/platform/broker/broker.go:63` (`http.Error`), `internal/multissh/server.go:144` (`writeError`) and its call sites; new `internal/platform/response/response_test.go`.
Do: converge on `response.WriteJSON` / `response.WriteError` (`internal/platform/response/response.go:9,16`). multissh's `writeError` envelope is already `{"error": msg}` — identical to `response.WriteError`; delete the private helper. For obsidianoid, **keep body shapes where tests assert them** — where a test asserts a plain-text `http.Error` body, adapt the test only if the new body is the `{"error":...}` envelope and the assertion is trivially mechanical; where a shape is load-bearing (external client), keep it and note it. The same keep-shape-where-tested rule governs slideshow's four sites and `broker.go:63` — slideshow is named explicitly because its tests may assert the plain-text bodies; the executor must check the asserting tests before converting each site. (Both reviewers converged: without slideshow and broker in scope, the Verify grep below can never come up empty.) Add the response package's first test file (status code, content-type, envelope shape) — `middleware` gets its tests in T2.3.
Verify: `go test -race ./internal/...` green; `grep -rn "http.Error(" internal/ --include='*.go' | grep -v _test` → empty; `grep -rn "func writeError" internal/` → empty.
Satisfies: FR-P4; NFR-2 (response tests), NFR-3; AC-10.

**T1.5 — Grocery mux modernization (FR-P5).**
Files: `internal/grocery/handler.go`, `internal/grocery/handler_test.go`.
Do: rewrite the pre-1.22 registrations (`handler.go:29-48`) as Go 1.22 method patterns (`"POST /api/items"`, `"GET /api/items"`, …), one pattern per method-route pair — including the sub-routes: `POST /api/recipes/reorder`, `PATCH /api/recipes/{id}`, `DELETE /api/recipes/{id}`, `POST /api/recipes/{id}/ingredients`, `DELETE /api/recipes/{id}/ingredients/{itemID}`, and likewise `/api/items/{id}`; keep the "reorder"-before-`{id}` disambiguation. Preserve the deliberate empty-id 404 for `DELETE /api/recipes/` (`handler.go:385-387`). Delete all **13** in-handler `r.Method !=` checks (grep the file for the authoritative list — iteration 1's count of 17 was wrong); grocery's four `switch r.Method` multiplexers (`handler.go:168, 210, 357, 420`) dissolve into the enumerated method patterns too — named here because the greps don't catch a leftover switch. **Keep the 405 body shape** (`{"error":"method not allowed"}` — the existing mechanism at `handler.go:53` is confirmed sound): register the bare-path 405 fallback **only for the exact paths that carry method patterns** (`mux.HandleFunc("/api/items", ...)`, `"/api/recipes/{id}"`, …) — **never** subtree registrations like `"/api/recipes/"`, which would turn unknown-path 404s into 405s — Go's mux prefers the method-specific pattern, so the bare pattern only catches wrong-method requests (L13). Update handler tests mechanically (the boundary behavior — status codes, bodies — must not change).
Verify: `go test -race ./internal/grocery/...` green; `grep -rn "r.Method !=" internal/ --include='*.go' | grep -v _test | grep -v multissh` → empty — **multissh is exempted with reason**: its remaining sites (`server.go:130` `handleSSHKeys`, registered method-less at `:53`; `:153` `onlyGet`; `:194` module-local static) are tested module-local semantics FR-P5 does not target (grocery only); the exemption and reason are mirrored in T6.3's final sweep. A wrong-method request to `/api/items` still yields 405 with the JSON body; an unknown path under `/api/recipes/` still 404s.
Satisfies: FR-P5; NFR-3; AC-10.

**Verify Phase 1:** `make test` green; all four NFR-3 greps empty; goleak green. Commit checkpoint.

---

### Phase 2 — Origin posture & mechanical hardening (FR-O, FR-R1/R2/R4/R5)

**T2.1 — Lift `sameOrigin` into `internal/platform/middleware`.**
Files: new `internal/platform/middleware/origin.go`; edit `internal/multissh/sshproxy/proxy.go`, `internal/multissh/broadcast.go`.
Do: move `sameOrigin(r *http.Request) bool` (proxy.go:303) and `originHost(origin string) string` (proxy.go:319) into `middleware` as `SameOrigin(r *http.Request) bool` / `originHost`. **`middleware.SameOrigin` is a pure predicate — no logging**: the real `sameOrigin` calls `Auditf("ws upgrade rejected origin=%q host=%q", ...)` at proxy.go:315, and `audit_test.go:136` asserts that exact line, so multissh's upgrader `CheckOrigin` (proxy.go:61) becomes a module-local wrapper that calls `middleware.SameOrigin` and emits the existing Auditf line on rejection — the audit test stays green unmodified (converged reviewer ruling). Rejection logging for T2.3's `OriginCheck` lives in the middleware itself, separately. **broadcast.go (directed final ruling, supersedes iteration 3's "must not touch" note, which contradicted the one-predicate rule):** `internal/multissh/broadcast.go:388-409` (package `multissh`, not `sshproxy`) is a complete second copy of **both** `sameOrigin` and `originHost`, untouched by the sshproxy wrapper. Fix: broadcast.go's `sameOrigin` becomes a second module-local wrapper — `if middleware.SameOrigin(r) { return true }`, then the existing `sshproxy.Auditf("broadcast ws upgrade rejected origin=%q host=%q", origin, host)` line verbatim (broadcast.go:399), `return false`; **explicitly delete** the now-dead private `originHost` at broadcast.go:403-409 (Go will not flag an unused function — the executor must remove it deliberately); add one test assertion mirroring audit_test.go:136 with the `broadcast ws upgrade rejected` prefix. Semantics unchanged: absent `Origin` → true; else `originHost(origin) == host`. One predicate binary-wide — never a second copy (v8 4.6b rule).
Verify: `go test -race ./internal/multissh/...` green (ported WS origin tests **and** the audit-line assertion at audit_test.go:136 still pass).
Satisfies: FR-O2 groundwork; FR-A13.

**T2.2 — Retire the wildcard: same-origin CORS headers (FR-O1 / shelved 4.6a).**
Files: `internal/platform/middleware/cors.go`, new `internal/platform/middleware/cors_test.go`.
Do: rewrite `Wrap` (cors.go:6-17): **delete** `Access-Control-Allow-Origin: *` and the unconditional method/header grants. New behavior: if `Origin` present and same-origin (T2.1 predicate), reflect it in `Access-Control-Allow-Origin` with the existing methods/headers grants; always emit `Vary: Origin`; keep the `OPTIONS` 204 short-circuit **only** for same-origin requests (foreign-origin preflight falls through un-granted). Same-origin requests functionally need no CORS headers; reflection is harmless belt-and-braces. Tests (middleware's first — NFR-2): same-origin → reflected + Vary; foreign → no ACAO; absent `Origin` → no ACAO, request unaffected; all-modules regression: each routed module still serves its own page normally.
Verify: `go test -race ./internal/platform/middleware/...`; `grep -n 'Allow-Origin.*\*' internal/` → empty.
Satisfies: FR-O1 (S-2); AC-6 (header half).

**T2.3 — Server-side origin check on writes (FR-O2/FR-O3 / shelved 4.6b, risk R9).**
Files: `internal/platform/middleware/origin.go` + `origin_test.go`; `cmd/server/main.go` (apply point); `internal/platform/config/config.go` (consume `server.origin_check`).
Do: `func OriginCheck(mode string, next http.Handler) http.Handler`. For every request with method other than `GET`/`HEAD`, at **any path**, on every module: if `Origin` is present and `!SameOrigin(r)` → per mode: `"enforce"` (the default for an empty value) → 403 JSON via `response.WriteError`, body never read, and one log line with the observed `Origin`/`Host` pair; `"log"` → log the same line, serve normally; `"off"` → passthrough. An **unknown `origin_check` value is a boot error** (`log.Fatalf` in `cmd/server/main.go`'s startup validation after `config.Load`, per L9's fatal-boot pattern; never a silent fallback to enforce). **Absent `Origin` is always permitted** (curl/scripts/automation). Method-not-path scoping is load-bearing: todo's whole write surface is non-`/api/` (`internal/todo/handler.go:31,33,35,37,38`). Apply in `main()` **outside everything**: `handler := middleware.Wrap(middleware.OriginCheck(cfg.Server.OriginCheck, dispatch))` — cross-origin writes die before host dispatch, before body limits, before any credential is examined. Must not wrap `ResponseWriter` (FR-A13).
**Per-module route matrix** (shared by T2.3 and T4.6 — the catch-all-SPA hazard means synthetic paths prove nothing; menuserver and multissh rows are reviewer-converged corrections; executor confirms exact paths against the code / v8 0.2b):

| Module | Write route (AC-6) | Data route | Static asset | Stream |
|---|---|---|---|---|
| grocery | `POST /api/items` | `GET /api/items` | `/` (index.html) | its SSE endpoint (platform broker) |
| todo | `POST /config/columns` (assert column list unchanged) | a `GET` items route | a static asset | SSE `GET /api/events` (handler.go:39) |
| slideshow | `POST /api/control` | `GET /api/state` | a static asset | its SSE endpoint |
| menuserver | **none — GET-only module** (handler.go:24-27), no SSE | `GET /items` | a static asset | — |
| obsidianoid | `POST /api/render` | a `GET /api/` route | a static asset | its SSE events endpoint |
| multissh | `POST /api/broadcast` (assert no job created) | a `GET /api/` route | a static asset | **WS upgrade** (not SSE) |

Tests (drive AC-6): foreign-origin `POST` with `Content-Type: text/plain` (simple request, no preflight) to each matrix write route → 403 **and no state change**. **menuserver's line:** the middleware runs before dispatch, so a foreign-origin `POST /items` → 403 under `"enforce"` even though no write handler exists; under `"off"` the same POST gets the module's own 404/405 (no handler, no state to change — this proves the check without inventing a phantom route). Matching `Origin` → proceeds. Absent `Origin` → proceeds. `"off"` → today's behavior. `"log"` → 200 + a logged line.
Verify: `go test -race ./internal/platform/middleware/... ./cmd/...`.
Satisfies: FR-O2, FR-O3 (S-2, shelved R9); AC-6.

**T2.4 — Security headers + render hygiene (FR-O4, FR-R5 / N-3, N-4).**
Files: `internal/platform/middleware/cors.go` (or a `headers.go`), `internal/obsidianoid/handler.go` (render handler).
Do: `Wrap` adds `X-Content-Type-Options: nosniff` on every response. obsidianoid's `POST /api/render` response additionally sets `Content-Security-Policy: sandbox` — that response only, one line. Nothing further (no CSP project, §8).
Verify: middleware test asserts nosniff on an arbitrary route; obsidianoid handler test asserts both headers on `/api/render` and no CSP header on other routes.
Satisfies: FR-O4, FR-R5; AC-6 environment.

**T2.5 — Server timeouts (FR-R1 / S-4).**
Files: `cmd/server/main.go:71-81`.
Do: replace the bare `http.ListenAndServe`/`ListenAndServeTLS` calls with an `http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 120 * time.Second}` — `ReadTimeout`/`WriteTimeout` left zero **deliberately** (multissh streams multi-GB uploads; SSE and WebSockets are long-lived). TLS branch uses `srv.ListenAndServeTLS(cfg.TLSCert, cfg.TLSKey)`.
Verify: `go build ./cmd/server`; a cmd test (or `httptest`-level assertion) that the constructed server carries the four values; manual: an idle SSE stream survives >120s of silence only via its retry/reconnect semantics — confirm the slideshow SSE test still passes (IdleTimeout applies between requests on a keep-alive connection, not to an in-flight response — note this in the code comment).
Satisfies: FR-R1 (S-4); AC-11 environment.

**T2.6 — Request body limits (FR-R2 / S-3).**
Files: new `internal/platform/middleware/bodylimit.go` + test; `internal/platform/config/config.go` (add `MaxBodyBytes int64 \`json:"max_body_bytes"\`` to each module config struct); `internal/platform/response/response.go` (+`WriteDecodeError`); `cmd/server/main.go` (`buildDispatcher`); mechanical sweep of decode-error branches.
Do: `func BodyLimit(n int64, next http.Handler) http.Handler` setting `r.Body = http.MaxBytesReader(w, r.Body, n)`. Wired per module inside `buildDispatcher`'s memoization: `h = middleware.BodyLimit(limitFor(module, cfg), h)`. Defaults: 1 MiB (`0` → default); multissh: `cfg.Multissh.MaxUploadBytes + 1<<20` so configured-size uploads keep working. Add `response.WriteDecodeError(w http.ResponseWriter, err error)`: `errors.As(&http.MaxBytesError{})` → 413, else 400 (L4); sweep `json.NewDecoder(r.Body).Decode` error branches in module handlers (grocery has **nine** — iteration 1's count of seven was wrong; todo/menuserver/slideshow/obsidianoid/multissh analogous) to call it.
Verify: table test — body over limit → 413; at limit → 200; a multissh upload of `max_upload_bytes` size passes its existing upload test.
Satisfies: FR-R2 (S-3); AC-7.

**T2.7 — File modes (FR-R4 / S-8, N-1).**
Files: `internal/platform/config/config.go:358` (`WriteDefault` 0644 → 0600); every module data-file write and data-dir create.
Do: sweep `grep -rn "0644\|0755" internal/ cmd/ --include='*.go' | grep -v _test`: data-file writes 0644 → **0600** (grocery `store.go:281`, and equivalents in todo/menuserver/slideshow/obsidianoid/multissh); data-directory creation 0755 → **0750** (grocery `store.go:101`, `build.go:15`, equivalents). **Static-asset serving untouched; existing files not chmodded retroactively** (new writes carry the new mode, per S-8's note). Auth artifacts are born 0600 in Phase 3.
Verify: `go test -race ./internal/...`; a store test asserting the temp-file/rename write lands 0600 and a fresh data dir is 0750.
Satisfies: FR-R4 (S-8, N-1); AC-9.

**Verify Phase 2:** `make test` green; `curl` smoke: foreign-origin POST → 403, absent-origin POST → normal. Commit checkpoint.

---

### Phase 3 — Auth core: `internal/platform/auth` (FR-A1..A8, FR-A14)

New package. **Zero module coupling**: imports `platform/config`, `platform/response`, stdlib, and the three new deps only — `go list -deps ./internal/platform/auth | grep -E 'internal/(grocery|todo|slideshow|menuserver|obsidianoid|multissh|admin)'` must return nothing, forever.

**T3.1 — Config surface + the one validator (FR-A14, FR-A11 validation half, FR-M2 config half).**
Files: `internal/platform/config/config.go`; new `internal/platform/auth/config.go` + `config_test.go`.
Do: add to `config.Config`: `Auth AuthConfig \`json:"auth"\`` with:

```go
type AuthConfig struct {
    Modules      map[string][]string `json:"modules"`        // module -> accepted methods: "ldap","pin","passkey","key"
    AdminPIN     string              `json:"admin_pin"`      // bcrypt hash, inline (XOR AdminPINFile)
    AdminPINFile string              `json:"admin_pin_file"` // plaintext PIN file, mode 0400/0600 required
    DataDir      string              `json:"data_dir"`
    CookieSecure bool                `json:"cookie_secure"`
    CookieDomain string              `json:"cookie_domain"`
    Session      SessionConfig       `json:"session"`        // TTLHours int (default 720), RefreshAfterFraction float64 (default 0.5)
    LDAP         LDAPConfig          `json:"ldap"`           // url, start_tls, insecure_tls, bind_dn_template, required_groups, timeout
    PINs         []NamedHash         `json:"pins"`           // {name, hash(bcrypt)}
    APIKeys      []NamedHash         `json:"api_keys"`       // {name, hash("sha256:…")}
    Passkey      PasskeyConfig       `json:"passkey"`        // rp_id, origins
}
```

`expandAuthPaths` (ExpandPath on `data_dir`, `admin_pin_file`) called from `Load` alongside the existing six expanders. **One validator, used verbatim by boot and by admin save (FR-M3's "same code path"):** `func ValidatePolicy(a config.AuthConfig, knownModules []string, adminRouted bool) error` — `adminRouted` is a parameter because the admin/PIN-form rule needs routing info, which lives on `config.Config` (`Routing`, config.go:18), not on `AuthConfig`, and `knownModules` is the `buildModule` universe, not the routed set (converged reviewer ruling; threaded through `FromConfig` in T4.5 and admin's save path in T5.3 so the same-code-path claim stays literally true). Rejecting: a module name in `Modules` not in `knownModules`; a method name outside `{"ldap","pin","passkey","key"}`; a listed method whose authenticator block is absent/empty (e.g. `"pin"` with empty `PINs`, `"ldap"` with empty `LDAP.URL`, `"passkey"` with empty `Passkey.RPID` or empty `Passkey.Origins`, `"key"` with empty `APIKeys`); the literal `"admin_pin"` anywhere in a method list (L6); `adminRouted` true with neither or both of `AdminPIN`/`AdminPINFile`; `AdminPINFile` set but the file's mode grants any group/other bits beyond 0400/0600 (error names the fix: `chmod 0400 <path>`). An existing config with no `auth` key validates clean (NFR-5). Tests: table-driven over every rejection and the empty-config pass.
Verify: `go test -race ./internal/platform/config/... ./internal/platform/auth/...`.
Satisfies: FR-A14, FR-A11 (validation), FR-M2 (config forms), FR-M5; AC-1, AC-12.

**T3.2 — Stateless JWT sessions + signing key + cookies (FR-A1, FR-A2, FR-A3, FR-A4).**
Files: new `internal/platform/auth/session.go` + `session_test.go`.
Do: HS256 via `golang-jwt/jwt/v5`. Claims: `sub` (identity name), `methods` ([]string — the satisfied-method set), `iat`, `exp`. `type Session struct{ Identity string; Methods map[string]bool }`.
- `func loadOrCreateKey(dataDir string) ([]byte, error)`: read `<data_dir>/session.key`; if absent, 32 random bytes written **0600** (`O_CREATE|O_EXCL`), dir created 0750. Key never appears in config. Deleting/rotating the file invalidates all sessions (the global-logout lever — documented T6.1).
- `func (s *Service) issueToken(identity string, methods []string, now time.Time) (string, error)`; `func (s *Service) verifyToken(raw string, now time.Time) (Session, error)` — signature + `exp` check only, clock injected for tests; parse **must** pass `jwt.WithValidMethods([]string{"HS256"})` so algorithm confusion is excluded at the parser (Architect ruling).
- Cookie: name `uw_session` (L5), `HttpOnly`, `SameSite=Lax`, `Secure` from `cookie_secure`, `Domain` from `cookie_domain` (empty → host-only, FR-A4), `Path=/`, `Max-Age` = TTL. TTL from `session.ttl_hours` default 720. **Sliding:** on gate pass, if elapsed > `RefreshAfterFraction` (default 0.5) of TTL, re-issue the cookie with same identity+methods, fresh `iat`/`exp`.
- `POST /api/auth/logout` handler clears the cookie (Max-Age -1). No revocation list (§8).
Tests: issue→verify round-trip; expired → error; garbage → error; token signed with a different key → error; sliding refresh at 0.49×TTL absent, at 0.51×TTL present; cookie attribute assertions (HttpOnly, Lax, Secure per config, Domain per config).
Verify: `go test -race ./internal/platform/auth/...`.
Satisfies: FR-A1, FR-A2, FR-A3, FR-A4; AC-4, AC-9.

**T3.3 — Method accumulation (FR-A1b).**
Files: `internal/platform/auth/session.go` (+tests).
Do: `func accumulate(existing *Session, identity string, method string) (resultIdentity string, methods []string)` — used by every login handler: no existing valid token, or existing token for the **same** identity → union of methods; existing valid token for a **different** identity → replace outright (new identity, only the new method). Tests: pin-then-ldap same identity → both methods; conflicting identity → replaced set.
Verify: `go test -race ./internal/platform/auth/...`.
Satisfies: FR-A1b; AC-3 (accumulated-token clause).

**T3.4 — PIN authenticator + operator PIN + `-hash-pin` (FR-A6, FR-M2 credential half).**
Files: new `internal/platform/auth/pin.go` + `pin_test.go`; `cmd/server/main.go` (flag).
Do: `func (s *Service) checkPIN(pin string) (name string, ok bool)` — bcrypt-compare against every `auth.pins` entry; first match wins; plaintext never logged/echoed. `func (s *Service) checkAdminPIN(pin string) bool` — against `AdminPIN` (bcrypt) or `AdminPINFile` (read **per attempt**, trimmed, plaintext compare via `subtle.ConstantTimeCompare`; re-check file mode 0400/0600 per read, refuse with the naming-the-fix error otherwise). Successful operator-PIN login → identity `admin`, method `"admin_pin"` (L6); named PIN → identity = entry name, method `"pin"`. CLI: `-hash-pin` flag in `cmd/server/main.go` — when stdin is a TTY, prompt with no echo via `x/term.ReadPassword`; when it is not (`!term.IsTerminal(fd)` — piped/redirected input), fall back to a buffered single-line read from stdin so `go run ./cmd/server -hash-pin <<< "1234"` works (Critic ruling: `ReadPassword` alone fails without a TTY; `x/term` added in T0.2). Print the bcrypt hash, exit.
Tests: match/mismatch; 0644 pin file refused with the chmod hint; file edit takes effect next attempt; no plaintext in any error string.
Verify: `go test -race ./internal/platform/auth/...`; `go run ./cmd/server -hash-pin <<< "1234"` prints a `$2a$` hash.
Satisfies: FR-A6, FR-M2; AC-3, AC-12.

**T3.5 — Login throttle (FR-A7 / N-2).**
Files: new `internal/platform/auth/throttle.go` + `throttle_test.go`.
Do: one in-memory global throttle for `POST /api/auth/login` (all methods, operator PIN included): after 5 consecutive failures, delay before processing further attempts — 2s doubling to a 60s cap; any success resets. Global, not per-IP (per-IP is meaningless behind the 127.0.0.1 proxy without trusting XFF — §8). Clock injected; the delay is a computed wait the handler enforces (test asserts the computed delay, not wall-clock sleeps).
Verify: `go test -race ./internal/platform/auth/...` — 5 failures → 6th sees ≥2s delay; success resets to zero.
Satisfies: FR-A7 (N-2); AC-5. Also bounds the FR-M2 pin-file read rate.

**T3.6 — API keys (FR-A7b) + `-gen-api-key`.**
Files: new `internal/platform/auth/apikey.go` + `apikey_test.go`; `cmd/server/main.go` (flag).
Do: `func (s *Service) checkAPIKey(r *http.Request) (name string, ok bool)`: read `Authorization: Bearer <key>` else `X-API-Key`; SHA-256 the value; constant-time compare against `auth.api_keys` hashes (`sha256:<hex>` format). Per-request, **no session, no cookie ever set**; checked by the gate **before** the cookie (T4.2); a bad key falls through to the other methods (no error). No throttle (32-byte random keyspace). CLI `-gen-api-key`: print the base64url key and its `sha256:` hash, exit.
Tests: valid key passes; invalid falls through; both header forms; no Set-Cookie on a key-authenticated response.
Verify: `go test -race ./internal/platform/auth/...`.
Satisfies: FR-A7b; AC-5b.

**T3.7 — LDAP[S] port (FR-A5).**
Files: new `internal/platform/auth/ldap.go` + `ldap_test.go` (from `reference/multissh/internal/auth/ldap.go`).
Do: port `LDAPConfig` (struct at ldap.go:16 — retag snake_case to match T3.1), `LDAPClient` (`:30`), `NewLDAPClient` (`:35`), `Authenticate(ctx, username, password)` (`:44`), `Authorize` (`:68`) and the unexported `dial`/`findUser`/`findGroups`/`allowed` — supports `ldaps://`, StartTLS, `insecure_tls`, timeout, optional `required_groups`. **Add the `ldap.Client` seam** (v8 3.1b — required: the reference ships **zero test files**, so the FRD's mention of an existing fake is factually wrong and the seam + fake are new code in this plan — Critic-confirmed; NFR-2 note): a minimal interface at the dial/bind boundary with the go-ldap implementation as default and a fake for tests. Successful bind → login handler issues token with the LDAP username as identity, method `"ldap"`. LDAP reachability is deliberately **not** validated at boot (the DC may boot slower than the webapp; v8 1.2c note carries).
Tests (against the fake): bind success → identity; bind failure → error, no token; `required_groups` mismatch → error even on good bind; `ldaps` vs `start_tls` dial paths constructed correctly.
Verify: `go test -race ./internal/platform/auth/...`.
Satisfies: FR-A5; AC-3.

**T3.8 — WebAuthn / passkeys port (FR-A8).**
Files: new `internal/platform/auth/passkey.go`, `passkey_store.go`, tests (from `reference/multissh/internal/auth/service.go` ceremony functions and `store.go`).
Do: port `passkeyStore` and `challengeStore` from `reference/.../store.go` (**not** `sessionStore` — JWT replaced it, L10); credentials persist at `<auth.data_dir>/passkeys.json` mode **0600**. Port the six ceremony functions from `service.go` (`BeginPasskeyRegistration:164`, `FinishPasskeyRegistration:197`, `BeginPasskeyLogin:242`, `FinishPasskeyLogin:270`, `ListPasskeys:144`, `DeletePasskey:323`), **rewired**: registration/list/delete take the identity from the gate-verified JWT session (register requires an existing session, FR-A8) instead of a `sessionID`; `FinishPasskeyLogin` issues a JWT with method `"passkey"` instead of a store session. Challenge state stays short-lived server-side memory (the one statefulness exception). `webauthn.New` config from `auth.passkey.rp_id` + `origins`; empty `rp_id`/`origins` with `"passkey"` listed anywhere is already a boot error (T3.1 — go-webauthn does not validate empty RPID itself; v8 M5 finding carries).
Tests (no hardware): registration/assertion option generation asserts the **literal configured `rp_id` string**, never the config value echoed back (v8 pre-mortem 4); challenge single-use (replay rejected) and expiry; assertion from an origin outside `origins` rejected; store round-trip (register, reopen store, still resolvable); `passkeys.json` lands 0600. End-to-end ceremony with a real authenticator: `t.Skip` with the dependency named.
Verify: `go test -race ./internal/platform/auth/...`; `stat -f '%Lp' <tmp>/passkeys.json` → `600` in test.
Satisfies: FR-A8; AC-3, AC-9.

**Verify Phase 3:** `go test -race ./internal/platform/auth/... ./internal/platform/config/...` green; `go list -deps ./internal/platform/auth | grep -E 'internal/(grocery|todo|slideshow|menuserver|obsidianoid|multissh)'` → empty. Commit checkpoint.

---

### Phase 4 — The gate, mounting, login page (FR-A9..A13)

**T4.1 — Policy snapshot (FR-A11 hot-swap half).**
Files: new `internal/platform/auth/policy.go` + test.
Do: `func BuildPolicy(a config.AuthConfig) (*Policy, error)` — **the one constructor of the immutable snapshot**, and the snapshot carries **all** hot-swappable auth state, not just the matrix (converged reviewer ruling — T5.3 swaps PIN/key/LDAP tables and T5.4 edits session settings, so a matrix-only Policy would silently require restarts, breaking FR-M3/AC-13): module accepted-method lists; the parsed PIN table and API-key hash table; the admin-PIN source (inline bcrypt hash or file path); LDAP settings; passkey RP config (`rp_id`, `origins`); session TTL, refresh fraction, and cookie attributes. `Service` holds `atomic.Pointer[Policy]`; `func (s *Service) policy() *Policy` (per-request read); `func (s *Service) SwapPolicy(p *Policy)` (used by admin live-apply, T5.3). `FromConfig` = validate + `BuildPolicy` + store; live-apply = validate + `BuildPolicy` + the same single atomic pointer swap. The gate **and every authenticator** consult the snapshot per request — this is what makes FR-M3 live-apply a pointer swap with no re-wiring and no half-applied state.
Verify: race test — concurrent gate reads during a swap, `-race` clean.
Satisfies: FR-A11; AC-13.

**T4.2 — The gate (FR-A10, FR-A10b, FR-A9, FR-A9b, FR-A13).**
Files: new `internal/platform/auth/gate.go` + `gate_test.go`.
Do: `func (s *Service) Gate(module string, next http.Handler) http.Handler`. Request order, exactly:
1. `GET /healthz` → 200 `{"ok":true}`, always, every module (FR-A9b).
2. `GET /api/auth/mode` → 200 `{"methods":[...]}` — **that module's** accepted list from the current policy snapshot; `{"methods":[]}` for an unprotected module. For `admin` the reported set is the **effective** set `matrix ∪ {"admin_pin"}` — an empty matrix must never render admin indistinguishable from unprotected, or the login page dead-ends with no form (Critic ruling). Always unauthenticated (FR-A9 / shelved C1 preserved).
3. `protected := (module has an auth.modules entry) || module == "admin"` — **admin is always protected when routed**, with or without a matrix entry, and stays protected when a live matrix save deletes its entry (converged reviewer ruling — the old "no entry → pass through" branch would have shipped admin fully unauthenticated; evaluated per request against the current snapshot). If **not protected**: everything else → `next` unchanged. Login/ceremony routes are **not** served (v8 `Service != nil` mounting-guard intent, preserved as a policy check) — they fall through to the module.
4. Module **protected** — gate-owned auth routes: `POST /api/auth/login`, `POST /api/auth/logout`, `GET /api/auth/session`, `POST /api/auth/passkey/login/begin`, `POST /api/auth/passkey/login/finish` (unauthenticated allowlist); `GET /api/auth/passkeys`, `POST /api/auth/passkey/register/begin|finish`, `DELETE /api/auth/passkeys/{id}` (require a valid session).
5. Everything else — **including static assets, SSE endpoints, WebSocket upgrades**: if the module's list contains `"key"`, try `checkAPIKey` first (per-request pass, no cookie; bad key falls through, T3.6). Then cookie: verify JWT; token methods ∩ module's accepted list non-empty → `next` (+ sliding re-issue per T3.2). For `module == "admin"`, the effective set is `matrix ∪ {"admin_pin"}` (L6).
6. Otherwise **401**: `GET` with `Accept: text/html` **on a non-`/api/` path** → the login page (T4.4); everything else — including any `/api/` path regardless of `Accept` — → bare JSON 401 (the HTML heuristic must never mask an API status; Critic minor, tested in T4.4).
Constraints: the gate **never wraps `http.ResponseWriter`** (`http.Hijacker` must survive for multissh WS — FR-A13); no per-module public-prefix lists exist anywhere (P1); cookies ride SSE (`EventSource`) and WS upgrade requests through the same verification — no special-casing.
Tests: table over the six steps; an upgrade-shaped request (Connection/Upgrade headers) with a valid cookie reaches `next` with `Hijacker` intact (assert the interface on the writer `next` receives); unprotected-module pass-through is byte-identical (record/compare a response with gate vs without, mode/healthz routes excepted).
Verify: `go test -race ./internal/platform/auth/...`.
Satisfies: FR-A10, FR-A9, FR-A9b, FR-A13 (S-1 root); AC-1, AC-2, AC-5b.

**T4.3 — Login/logout/session handlers + method enforcement + event log (FR-A10b, FR-A12b).**
Files: `internal/platform/auth/handlers.go` + `handlers_test.go`.
Do: `POST /api/auth/login` body `{"method":"pin"|"ldap","pin":…,"username":…,"password":…}`. **First check:** the named method ∉ this module's accepted list → **400 before any credential is examined** (FR-A10b; on `admin`, `"pin"` is always acceptable — the operator PIN path, with named-PIN fallback only if `"pin"` is in admin's matrix). **The same policy check guards the passkey ceremony: both `POST /api/auth/passkey/login/begin` and `/finish` reject with 400 before any ceremony work when the module's accepted list ∌ `"passkey"`** (Architect ruling — the method-field check on `/api/auth/login` alone leaves the ceremony routes running full WebAuthn on modules that don't accept passkeys). Then throttle (T3.5), then the authenticator, then `accumulate` (T3.3), then set-cookie. `GET /api/auth/session` → `{"identity":…,"methods":[…]}` or 401. Logout per T3.2. **Auth event log (FR-A12b):** one line per success and failure — method, identity (or `unknown`), module, and failure reason class (`bad_credential` | `disallowed_method` | `throttled`); never credentials, PINs, key material, or tokens.
Tests: PIN login on a module accepting only `ldap` → 400, and the authenticator was never invoked (fake records calls); `passkey/login/begin` on a non-passkey module → 400, no challenge issued; log-line assertions including the never-log-secrets grep over captured output.
Verify: `go test -race ./internal/platform/auth/...`.
Satisfies: FR-A10b, FR-A12b; AC-3, AC-15 environment.

**T4.4 — The login page (FR-A12).**
Files: new `internal/platform/auth/login.html` (go:embed, L7), `gate.go` wiring.
Do: one minimal platform-owned page, embedded, served by the gate on unauthenticated `GET` + `Accept: text/html` with status **401**. On load it fetches `/api/auth/mode` and shows only the relevant forms: PIN input (methods intersect `{"pin","admin_pin"}` — admin's mode response always includes `"admin_pin"` per T4.2 step 2, so the empty-matrix admin page never dead-ends formless), LDAP username/password (∋ ldap), a passkey button (∋ passkey) driving the begin/finish ceremony via inline JS. Successful login → `location.reload()` (the SPA then loads normally — module SPAs never see an unauthenticated state). No module frontend contains auth logic (NFR-1: the page is a convenience; curl + cookie jar must work identically).
Tests: HTML 401 on `Accept: text/html` GET; JSON 401 on XHR-style `Accept: application/json` and on POSTs; an unauthenticated `GET` to an admin `/api/` route with `Accept: text/html` → **401 JSON, never the login page** (T4.2 step 6); admin with an empty matrix → mode body contains `"admin_pin"` and the page shows the PIN form (Critic ruling); page body reflects per-module methods (contains the pin form for a pin module, not the ldap form).
Verify: `go test -race ./internal/platform/auth/...`; manual: browser hit on a protected module shows the page.
Satisfies: FR-A12; NFR-1; AC-2 usability.

**T4.5 — Mount: universal gate in `cmd/server/main.go` (FR-A11).**
Files: `cmd/server/main.go`.
Do: after `config.Load`: `svc, err := auth.FromConfig(cfg.Auth, knownModules, adminRouted)` (L8; `knownModules` = the `buildModule` switch's names, written once beside it; `adminRouted` = whether `"admin"` appears among `cfg.Routing`'s values) — `err != nil` → `log.Fatalf` (boot error, L9: validation failures per T3.1, unreadable `data_dir`, bad key file). In `buildDispatcher`'s memoization loop, wrap every **successfully built** module exactly once:
```go
h = middleware.BodyLimit(limitFor(module, cfg), svc.Gate(module, h))
```
(gate innermost of the two so limits also cover login POSTs). **503 stubs are replaced wholesale, ungated** — a module that failed to build has no auth surface, only the `unavailableHandler`. `FromConfig` with `auth` absent returns a Service with an empty policy: every gate is pass-through + mode/healthz — behaviorally today's binary (FR-A14).
Verify: `go build ./...`; T4.6's tests.
Satisfies: FR-A11, FR-A14 (S-1, N-5 mechanism); AC-1.

**T4.6 — Gate acceptance tests (the AC engine).**
Files: new `cmd/server/dispatcher_auth_test.go` (package main tests; `buildDispatcher` is in scope).
Do — each test builds a dispatcher from a config literal with temp dirs:
- **AC-1:** config without `auth`: every routed module serves exactly today's behavior; no login route exists (`POST /api/auth/login` → **the module's own response for that path, asserted identical to today's** — not a hard-coded 404/405, because SPA static handlers serve `index.html` for unknown paths, e.g. grocery `build.go:47-53`; Architect ruling); no `Set-Cookie` ever; the only additions are `GET /api/auth/mode` → `{"methods":[]}` and `GET /healthz` → 200 — asserted on **every routed hostname** (pre-mortem 1).
- **AC-2:** a config protecting every module (`auth.modules` covering all six + admin), **iterated from the config map, not hand-named**: per module, unauthenticated requests to every applicable row of the **per-module route matrix in T2.3** — a real data route, a static asset path, the SSE endpoint where the module has one (menuserver has none), and multissh's WS upgrade → all **401**; `GET /api/auth/mode` → 200 with that module's methods.
- **AC-3:** slideshow=`["pin"]`, multissh=`["ldap"]` (fake LDAP): PIN login on slideshow → token opens slideshow, 401 on multissh; PIN login attempt **on multissh** → 400 before credential check; LDAP login with the existing cookie → accumulated token opens both.
- **AC-4:** expired / garbage / rotated-key tokens → 401; near-expiry request → refreshed cookie in the response.
- **AC-5:** five failed PIN logins → sixth sees backoff (injected clock); success resets.
- **AC-5b:** valid API key passes a `"key"` module with zero cookies; same key → 401 on a non-`"key"` module; invalid key → normal 401; healthz 200 everywhere.
Verify: `go test -race ./cmd/...`; goleak green.
Satisfies: AC-1, AC-2, AC-3, AC-4, AC-5, AC-5b; NFR-1, NFR-4, NFR-5.

**Verify Phase 4:** `make test` green; manual smoke: protect slideshow with a PIN in a scratch config, browser shows login page, PIN opens it, curl without cookie 401s. Commit checkpoint.

---

### Phase 5 — The `admin` module (FR-M)

**T5.1 — Scaffold per the FR-P6 checklist (its first consumer).**
Files: new `internal/admin/build.go`, `internal/admin/handler.go`; new `web/admin/index.html` (+`app.js`, `style.css`); `cmd/server/main.go` (`buildModule` case); `internal/platform/config/config.go` (`AdminConfig{ StaticDir string \`json:"static_dir"\`; MaxBodyBytes int64 \`json:"max_body_bytes"\` }` + `Admin AdminConfig \`json:"admin"\`` + expander).
Do: `admin.Build(cfg *config.Config, deps admin.Deps) (http.Handler, error)` with `Deps{Service *auth.Service, ConfigPath string, KnownModules []string, AdminRouted bool}` (L12 — the FR-M6 sanctioned deviation; `buildModule` gains the extra arguments it already has in scope; `KnownModules`/`AdminRouted` exist so T5.3's save calls `ValidatePolicy` with boot's exact arguments). Static serving via `platform/static`; routes via method patterns; responses via `platform/response`; `host_routing` keyword `admin`. Boot validation (T3.1) already refuses `admin` routed without exactly one operator-PIN form.
Verify: route `admin` in a scratch config + `admin_pin_file` → module builds, serves the SPA shell; without a PIN form → boot error.
Satisfies: FR-M6, FR-P6 (checklist proven by use); AC-12.

**T5.2 — Break-glass enforcement tests (FR-M2).**
Files: `internal/platform/auth/gate_test.go`, `cmd/server/dispatcher_auth_test.go` additions.
Do (behavior landed in T3.1/T3.4/T4.2; this task proves it end-to-end), covering **both encodings of "empty assignment matrix"** (converged reviewer ruling): (a) an `"admin": []` entry present, and (b) no `"admin"` key at all — in each: an unauthenticated admin data route → **401**, the login routes are served, and the operator PIN from a 0400 `admin_pin_file` logs in (identity `admin`, method `admin_pin`); a live matrix save that **deletes** admin's entry (legal per FR-M5) → admin still protected on the very next request; `GET /api/auth/mode` on empty-matrix admin contains `"admin_pin"` and the login page shows the PIN form (T4.2 step 2 / T4.4); a 0644 PIN file → login error naming `chmod`; editing the file takes effect next attempt (read per attempt); the throttle applies to operator-PIN attempts; the matrix can add methods to admin but a matrix save can never remove `admin_pin` (it isn't in the matrix at all); admin UI shows the PIN as "defined by config" (T5.5).
Verify: `go test -race ./internal/platform/auth/... ./cmd/...`.
Satisfies: FR-M2; AC-12.

**T5.3 — Live apply: validate → atomic write → atomic swap (FR-M3, FR-M4).**
Files: new `internal/admin/apply.go` + `apply_test.go`.
Do: `func (h *Handler) applyAuth(newAuth config.AuthConfig) error`:
1. `auth.ValidatePolicy(newAuth, deps.KnownModules, deps.AdminRouted)` — **the same function with the same arguments boot uses** (T3.1, L12). Reject → nothing changes anywhere.
2. Surgical config rewrite — **textual splice, the only mechanism** (converged reviewer ruling: a `map[string]json.RawMessage` re-marshal fails AC-14 deterministically — `encoding/json` sorts map keys alphabetically, compacts `RawMessage` contents, and HTML-escapes `<`, `>`, `&`; `RawMessage` preserves value bytes but never member order): walk the file with a `json.Decoder` using `Token()` + `InputOffset()` to locate the byte range of the top-level `"auth"` member's value (when the member is absent, the insertion point is before the final `}`, with a comma appended after the preceding member — the byte-compare test catches a missed comma, but write it deliberately); splice in `json.MarshalIndent(newAuth, "", "  ")` re-indented to the member's depth; **every byte outside that range is left untouched**; write tmp file **0600** in the same directory, `os.Rename` (pre-mortem 2). Test: **whole-file byte comparison excluding only the auth member's range** — strictly stronger than a per-section compare (AC-14).
3. `p, err := auth.BuildPolicy(newAuth)` then `svc.SwapPolicy(p)` — the snapshot carries *all* hot-swappable state (T4.1: matrix, PIN/key tables, admin-PIN source, LDAP, passkey RP, session settings), so any admin edit is live on the next request with no half-applied window and no restart.
Restart equivalence (FR-M4): the file is the single source of truth; passkeys stay in their FR-A8 file.
Tests: valid save → file changed only in `"auth"`, next request enforces; invalid save (unknown module / unconfigured method) → file byte-identical, in-memory policy unchanged (probe before/after with a request); crash-window safety: tmp+rename means the file is never partial.
Verify: `go test -race ./internal/admin/...`.
Satisfies: FR-M3, FR-M4, FR-M5; AC-13, AC-14.

**T5.4 — Admin API panels (FR-M1, FR-M5, AC-15).**
Files: `internal/admin/handler.go` + `handler_test.go`.
Do — all under `/api/` on the admin module (gate-protected, origin-checked, body-limited like everything else; the SPA is a convenience per NFR-1):
- `GET /api/config/auth` — current auth config, **secrets redacted**: hashes elided to `"(set)"`, operator PIN shown as `{"defined_by":"config"}`.
- `PUT /api/config/modules` — the module × method matrix → T5.3 apply.
- `POST /api/pins` `{name, pin}` — server bcrypts; plaintext never round-trips back. `DELETE /api/pins/{name}`.
- `POST /api/keys` `{name}` — generate 32-byte key server-side; response carries the key **exactly once**; stores only the hash. `DELETE /api/keys/{name}`.
- `POST /api/ldap/test` — attempt a bind server-side with posted credentials; respond outcome **class** only (`ok` | `bad_credentials` | `unreachable` | `tls_error`); never echo credentials.
- `PUT /api/config/session` — TTL, cookie domain/secure → T5.3 apply.
- Passkey management reuses the gate's FR-A8 routes (register/list/delete) — no admin duplicates.
Each mutation runs through T5.3 (validate → write → swap).
Tests: AC-15 — responses carry no plaintext secrets (generated key appears exactly once, in the generate response only; grep every other response body); LDAP test never echoes the password; matrix save with a typo'd module → 400 + nothing changed.
Verify: `go test -race ./internal/admin/...`.
Satisfies: FR-M1, FR-M5; AC-15.

**T5.5 — Admin SPA.**
Files: `web/admin/index.html`, `web/admin/app.js`, `web/admin/style.css` (plain JS, L11).
Do: panels calling the T5.4 API: matrix editor, PIN add/remove, API key generate (show-once modal)/revoke, LDAP settings + test button, session settings, passkey register/list/delete (gate routes). Operator PIN rendered read-only as "defined by config". No auth logic beyond calling the API (NFR-1); no build-pipeline change.
Verify: `npm run build` still green (untouched); manual: full click-through of AC-13's add/remove flow.
Satisfies: FR-M1 frontend; AC-13 usability.

**T5.6 — Restart equivalence + admin acceptance (AC-12/13/14).**
Files: `cmd/server/dispatcher_auth_test.go` additions.
Do: scripted sequence against a dispatcher + real temp config file: add a PIN, generate a key, add a module to the matrix (each via the admin API) → assert enforcement changed live (AC-13: previously-open module's next unauthenticated request 401s; removal → next request passes); then **rebuild the dispatcher from the written file** (simulated restart) → identical behavior (AC-14); non-auth config sections byte-identical before/after the whole sequence.
Verify: `go test -race ./cmd/...`.
Satisfies: FR-M3, FR-M4; AC-12, AC-13, AC-14.

**Verify Phase 5:** `make test` green; manual admin click-through. Commit checkpoint.

---

### Phase 6 — Config surface, docs, final sweep

**T6.1 — Config surface + README (FR-A14, FR-O3 note, NFR-5).**
Files: `internal/platform/config/config.go` (`WriteDefault`/`DefaultConfig`), `unified-webapp-example.json`, `README.md`.
Do: `WriteDefault` emits the `auth` section in empty/commented-out form (empty `modules` map, empty tables — loads as auth-off) and `server` (`origin_check: "enforce"`, `sse_max_subscribers: 64`); example config gains a fully-populated `auth` example (the FRD §3.4 shape) + `admin` section + an `"admin"` `host_routing` entry. README: the auth model (per-module method lists are literal — no strength hierarchy); `origin_check` is the one new enforced default and `"log"` is the escape hatch; **the proxy must preserve `Host`** (load-bearing for dispatch and FR-O2 — risk R1); key-file rotation/deletion = global logout (FR-A2); `cookie_domain`/`rp_id` guidance (common parent domain → one login/one passkey across modules); first-run bootstrap: `echo $PIN > admin.pin && chmod 0400 admin.pin`; bind stays `0.0.0.0` (L14, deployment note); API-key usage for automation.
Verify: `go run ./cmd/server -init-config -config /tmp/x.json && go run ./cmd/server -config /tmp/x.json` boots with today's behavior; example config passes `config.Load` + `auth.ValidatePolicy`.
Satisfies: FR-A14, FR-O3, NFR-5; AC-1.

**T6.2 — `docs/adding-a-module.md` (FR-P6).**
Files: new `docs/adding-a-module.md`.
Do: the checklist: `Build(cfg) (http.Handler, error)`; config section + expander; `buildModule` case; `host_routing` entry; which platform packages to use (`static`, `broker` + cap, `response`, `fspath`); what the dispatcher gives for free (CORS posture, origin check, nosniff, body limit via `max_body_bytes`, timeouts, the gate); and auth opt-in = **one `auth.modules` entry naming accepted methods** — state the module-N+1 invariant verbatim. Note `admin`'s three sanctioned deviations (FR-M6) as the exceptions that prove the pattern.
Verify: doc review — a reader can add a module without touching platform code.
Satisfies: FR-P6.

**T6.3 — Final sweep + acceptance run (NFR-3, NFR-4, AC-10, AC-11).**
Files: none (verification) + fixes as needed.
Do: greps — `staticHandler` (non-multissh), `func validName`, `http.Error(` (non-test), `r.Method !=` (non-test, `| grep -v multissh` — mirroring T1.5's stated exemption: multissh's `onlyGet`/static/`handleSSHKeys` sites are tested module-local semantics outside FR-P5's scope) → all empty (NFR-3, AC-10); `make test` green (`-race`, Go 1.26.x, updated deps — AC-11); goleak green; `CGO_ENABLED=0 go build ./...`; `npm run build`; walk AC 1–15 against the test list in §6 and record pass/fail per criterion in the final commit message — that message also restates the multissh `r.Method` grep exemption and its reason, so AC-10's "greps per NFR-3 are empty" reads honestly; `git log origin/main..HEAD` local-only, **never push** (NFR-6).
Verify: the commands above; the recorded AC table.
Satisfies: NFR-3, NFR-4, NFR-6; AC-10, AC-11.

---

## 5. Traceability

### Requirements → tasks

| Req | Task(s) | Verified by |
|---|---|---|
| FR-P1 static | T1.1 | grep + module tests (AC-10) |
| FR-P2 broker | T1.3 | migrated tests + cap test (AC-8, AC-10) |
| FR-P3 fspath | T1.2 | grep + parity tests (AC-10) |
| FR-P4 response | T1.4 | grep + response tests (AC-10) |
| FR-P5 grocery mux | T1.5 | grocery tests + 405-shape test (AC-10) |
| FR-P6 checklist | T6.2 (doc), T5.1 (first consumer) | doc review; admin built by the checklist |
| FR-A1 JWT sessions | T3.2 | AC-4 token tests |
| FR-A1b accumulation | T3.3 | AC-3 accumulated-token test |
| FR-A2 signing key | T3.2 | 0600 assertion; rotated-key 401 (AC-4, AC-9) |
| FR-A3 TTL/sliding/logout | T3.2 | AC-4 refresh test |
| FR-A4 cookie domain | T3.2 | cookie attribute tests |
| FR-A5 LDAP | T3.7 | fake-directory tests (AC-3) |
| FR-A6 PIN + `-hash-pin` | T3.4 | AC-3, AC-12 |
| FR-A7 throttle | T3.5 | AC-5 |
| FR-A7b API keys + `-gen-api-key` | T3.6, T4.2 | AC-5b |
| FR-A8 passkeys | T3.8 | ceremony/store tests (AC-3, AC-9) |
| FR-A9 mode route | T4.2 | AC-1, AC-2 |
| FR-A9b healthz | T4.2 | AC-5b |
| FR-A10 fail-closed gate | T4.2 | AC-2 |
| FR-A10b method enforcement | T4.3 | AC-3 (400-before-credential, incl. passkey ceremony routes) |
| FR-A11 mounting + validation + hot swap | T3.1, T4.1, T4.5 | AC-1, AC-13; boot-error tests |
| FR-A12 login page | T4.4 | HTML-401 tests |
| FR-A12b auth event log | T4.3 | log assertions (AC-15 adjacent) |
| FR-A13 gate transparency | T2.1, T4.2 | Hijacker/SSE/WS pass-through tests (AC-2) |
| FR-A14 additive config | T3.1, T4.5, T6.1 | AC-1 |
| FR-M1 admin scope | T5.4, T5.5 | AC-15 |
| FR-M2 operator PIN | T3.1, T3.4, T4.2, T5.2 | AC-12 (incl. admin-always-protected + effective mode set) |
| FR-M3 live apply | T4.1, T5.3 | AC-13 |
| FR-M4 restart equivalence | T5.3, T5.6 | AC-14 |
| FR-M5 guardrails | T3.1, T5.3, T5.4 | AC-13 invalid-save clause |
| FR-M6 special case | T5.1 (L12) | plan review; deviations listed once |
| FR-O1 no wildcard | T2.2 | header tests (AC-6 env) |
| FR-O2 origin check on writes | T2.1, T2.3 | AC-6 |
| FR-O3 relax switch | T2.3, T6.1 | AC-6 `"off"` clause; README |
| FR-O4 nosniff | T2.4 | header test |
| FR-R1 timeouts | T2.5 | server-struct assertion (AC-11 env) |
| FR-R2 body limits | T2.6 | AC-7 |
| FR-R3 SSE cap | T1.3 | AC-8 |
| FR-R4 file modes | T2.7 (+T3.2/T3.8 for auth artifacts) | AC-9 |
| FR-R5 render hygiene | T2.4 | header test |
| NFR-1 no frontend trust | T4.4, T5.5 design; all controls server-side | AC-2 via curl-only paths |
| NFR-2 testability | T3.x clock/seam injection; T1.4/T2.2 first tests for response/middleware | package test files exist |
| NFR-3 uniformity | T1.1–T1.5, T6.3 | greps (AC-10) |
| NFR-4 build discipline | T0.1/T0.2, every phase gate | `make test`, goleak, CGO=0 |
| NFR-5 additive config | T3.1, T6.1 | AC-1 |
| NFR-6 no push | T6.3 + every checkpoint | `git log` local-only |

### Acceptance criteria → tasks

| AC | Task(s) |
|---|---|
| 1 (auth absent = today) | T3.1, T4.5, T4.6, T6.1 |
| 2 (universal 401, iterated from config) | T4.2, T4.5, T4.6 |
| 3 (three authenticators e2e; per-module policy; accumulation) | T3.3, T3.4, T3.7, T3.8, T4.3, T4.6 |
| 4 (token lifecycle; sliding refresh) | T3.2, T4.6 |
| 5 (throttle) | T3.5, T4.6 |
| 5b (API keys; healthz) | T3.6, T4.2, T4.6 |
| 6 (cross-origin writes 403, no state change; off-switch) | T2.2, T2.3 |
| 7 (413; multissh upload survives) | T2.6 |
| 8 (broker cap 503; goleak) | T1.3 |
| 9 (file modes 0600/0750; auth artifacts) | T2.7, T3.2, T3.8 |
| 10 (NFR-3 greps; broker migration green) | T1.1–T1.5, T6.3 |
| 11 (make test -race green, Go 1.26.x) | every phase gate, T6.3 |
| 12 (operator PIN; loose-mode refusal; XOR boot error) | T3.1, T3.4, T5.1, T5.2 |
| 13 (live apply; invalid save provably no-op) | T5.3, T5.6 |
| 14 (restart equivalence; byte-for-byte non-auth) | T5.3, T5.6 |
| 15 (no plaintext secrets in admin responses) | T5.4 |

### Prior-findings map (FRD §8, restated for reviewers)

S-1→Phase 3/4 · S-2→T2.2/T2.3 · S-3→T2.6 · S-4→T2.5 · S-5→T1.3 · S-6/S-7→§8 non-goals · S-8/N-1→T2.7 · N-2→T3.5 · N-3→T2.4 · N-4→T2.4 · N-5→operator's `auth.modules` choice (mechanism T4.5) · shelved C1→T4.2 step 2 · shelved C4→eliminated by design (Option A) · shelved R1→T2.3 `"log"` + T6.1 · shelved R9→T2.3 · shelved 0.2b→informs T2.3/T4.6 test routes only.

---

## 6. Test & Observability Plan (deliberate mode)

| Layer | Coverage (→ AC) |
|---|---|
| **Unit** | fspath parity tables (T1.2); static handler (T1.1); broker cap/snapshot/rooms (T1.3 → AC-8); response envelope + `WriteDecodeError` mapping (T1.4, T2.6 → AC-7); `SameOrigin` predicate (T2.1); JWT issue/verify/expiry/rotation/sliding with injected clock (T3.2 → AC-4); accumulation semantics (T3.3 → AC-3); PIN + operator-PIN incl. file-mode refusal (T3.4 → AC-12); throttle backoff/reset (T3.5 → AC-5); API-key hashing + constant-time compare (T3.6 → AC-5b); LDAP against the fake seam (T3.7 → AC-3); passkey options/challenge single-use/origin validation/store round-trip (T3.8 → AC-3); validator rejection table (T3.1 → AC-12/13). |
| **Integration** | CORS header matrix + all-modules regression (T2.2 → AC-6); cross-origin write 403 with **no state change** on named real routes of every module, absent-Origin pass, `log`/`off` modes (T2.3 → AC-6); body limits incl. multissh upload at configured size (T2.6 → AC-7); gate table incl. Hijacker survival and SSE/WS pass-through (T4.2 → AC-2); login handlers incl. 400-before-credential (T4.3 → AC-3); dispatcher-level AC-1..5b suite iterating `auth.modules` from config (T4.6); admin apply/reject/restart-equivalence with byte-compare (T5.3, T5.6 → AC-13/14); admin API secret-hygiene greps (T5.4 → AC-15); file-mode assertions (T2.7, T3.2, T3.8 → AC-9). |
| **E2E (manual, recorded in the T6.3 commit)** | Through the real nginx/HAProxy front end (a synthetic-Host handler test cannot see what the proxy does to `Host` — R1): browser login-page flow per method on a protected module; passkey ceremony with a real authenticator (the one genuinely blocked-on-hardware item — named, not silently skipped); admin click-through of AC-12/13/15; `curl` API-key automation path; `origin_check: "log"` recovery drill. |
| **Observability** | Auth event log (T4.3): one line per login success/failure with method/identity/module/reason-class — never secrets (tested). Origin rejections log the `Origin`/`Host` pair — one-line proxy diagnosis (T2.3, R1's detection signal). Boot log states which modules are gated with which methods (one line at `FromConfig`) — pre-mortem 1's visibility. Broker 503s at cap are visible as status codes. Throttle engagement logged as reason-class `throttled`. Detection signals per risk: R1 → origin-403 count nonzero while writes fail; R3 → key-file mtime vs complaint of surprise logouts; R5 → byte-compare test + `git diff` of the config file after admin edits. |

---

## 7. Risk register

| # | Risk | Impact | Mitigation |
|---|---|---|---|
| R1 *(carried)* | **Host-rewriting proxy.** FR-O2 compares `Origin` vs `r.Host`; one wrong `proxy_set_header` line 403s every browser write in every module. | High | `origin_check: "log"` one-line recovery (T2.3); every rejection logs the `Origin`/`Host` pair; README documents Host preservation as load-bearing (T6.1); dispatch already requires preserved Host, so the misconfig also breaks routing loudly. Pre-mortem 3. |
| R9 *(carried)* | **`text/plain` simple-request POST executes cross-origin** — no preflight, ACAO removal alone changes nothing; requests still run server-side. | High | T2.3 is the load-bearing server-side rejection (shelved 4.6b, declared unrevertible there — carried: `"off"` exists for the operator, but the default is enforce and the tests assert **no state change**, not just a status code). T2.2 is defense-in-depth for the read half only. |
| R13 | **The universal gate breaks streaming** — a wrapped `ResponseWriter` loses `http.Hijacker` (multissh WS) or `Flusher` (SSE). | High | Hard rule (T0.3, FR-A13): no middleware wraps the writer; T4.2 asserts Hijacker survival through the full chain; goleak + existing multissh/SSE suites at every phase gate. |
| R14 | **Stateless JWT = no revocation**; a leaked cookie is valid up to 30 days. | Med | Accepted non-goal (§8, FRD §7): key-file rotation is the global lever, documented (T6.1); TTL configurable down; HttpOnly+SameSite=Lax+Secure narrow the leak surface. Single-operator LAN calculus. |
| R15 | **Broker unification drifts slideshow/obsidianoid semantics** (snapshot-on-connect, payload framing). | Med | Their existing tests migrate unmodified except imports (T1.3 ground rule); any semantic test edit is a stop-the-line signal; goleak green. |
| R16 | **Admin live-apply corrupts or reorders the config file**, or a half-applied state splits disk from memory. | High | One validator for boot and save; `json.Decoder`-offset textual splice + tmp/rename 0600; whole-file byte-compare excluding only the auth member's range; swap is a single atomic pointer after the write succeeds (T5.3, AC-13/14). Pre-mortem 2. |
| R17 | **Global login throttle is a self-DoS lever**: any LAN client failing 5 logins delays the operator too (operator PIN included). | Low | Accepted for this deployment (per-IP is a non-goal without XFF trust): delay caps at 60s, success resets, API keys bypass the throttle, and the alternative (no throttle) loses N-2. Recorded, not engineered against. |
| R18 | **Method accumulation surprises**: logging in as a different identity replaces the token, silently dropping the other identity's accumulated methods. | Low | FR-A1b specifies exactly this (replacement on identity conflict); T3.3 tests pin the semantics; the session route (`GET /api/auth/session`) makes the current identity/methods inspectable. |
| R19 | **Dependency tree growth** — go-webauthn pulls go-tpm/cbor; a CGO-requiring transitive dep would break `CGO_ENABLED=0`. | Low | Deps land first (T0.2) with a CGO=0 build check, so a conflict surfaces before any porting effort is sunk (v8 R5 pattern). |
| R20 | **`Accept: text/html` login-page heuristic misfires** — an API client sending `Accept: */*` gets JSON (fine), but a non-browser HTML scraper gets a 401 login page it can't use. | Low | The page **is** the 401 (correct status either way); content negotiation only changes the body, and `/api/` paths always get JSON regardless of `Accept` (T4.2 step 6). No client in this deployment is harmed; recorded so nobody "fixes" it into a 200. |
| R21 | **Kiosk/passive clients hard-fail at 401 when their cookie ages out** — a wall-mounted slideshow tablet nobody touches silently stops updating when its session expires, and someone has to walk over and log in again (Architect steelman: a real recurring operational chore, not hypothetical). | Low | Not engineered away — acknowledged: the 30-day **sliding** TTL means any kiosk that keeps rendering keeps refreshing itself indefinitely; an API key (`"key"` on the module) removes the cookie dependency for scripted clients; when it does expire, the login page appearing on the kiosk *is* the recovery UI. Recorded here so the re-cookie chore isn't discovered in production. |

---

## 8. Out of scope (mirrors FRD §7 — final, recorded once)

1. **TLS in the binary.** The proxy owns it; existing `tls_cert`/`tls_key` config stays as-is, untouched.
2. **Session revocation store / per-session logout-everywhere.** Key-file rotation is the global lever (FR-A2).
3. **Per-IP rate limiting or `X-Forwarded-For` trust.** The throttle is global by design (FR-A7).
4. **A CSP project for the SPAs; markdown sanitization in obsidianoid.** FR-R5's one `sandbox` header on `/api/render` is the whole ambition.
5. **Host-header dispatch hardening beyond the proxy contract.** S-7 stays a deployment note (README, T6.1).
6. **Multi-user account management, roles, or per-module identities.** An identity is a name in a token.
7. *(carried from L14)* Changing the `0.0.0.0` bind — deployment note only.

---

## 9. ADR — Universal fail-closed gate with stateless JWT sessions and per-module method lists

**Decision.** Protect modules with one platform-owned gate applied to every module at the dispatcher's single registration choke point; on a protected module every route requires a valid session except a fixed gate-owned allowlist (mode, healthz, login/ceremony); sessions are stateless HS256 JWTs carrying an accumulating set of satisfied methods; module policy is a per-module accepted-method list and the gate check is set intersection; the `admin` module manages the policy live behind an always-on break-glass operator PIN; origin checks, body limits, timeouts, headers, and the consolidated platform packages land underneath, so future modules inherit the whole posture from one config entry.

**Drivers.** (D1) The FRD is final: fifteen ACs, five named divergences from the shelved design. (D2) Home-lab calibration: fail closed on security toggles, one relax switch per control, no enterprise machinery. (D3) Sonnet-executable mechanics: the eliminated public-path predicate removes the one design element that took the shelved plan three review iterations to state correctly.

**Alternatives considered.** (§1 Options): the v8 two-list prefix gate + session store — rejected because the FRD supersedes it and the prefix table fails open on drift, violating the module-N+1 invariant; proxy-level auth — rejected on requirement coverage (per-module methods, passkeys, API keys, live-apply admin are unimplementable there).

**Why chosen.** It is the FRD's own design, and it is the smaller honest design: no route inventory, no session store, no predicate to misclassify. Its one real cost — assets require auth — is paid once by a platform login page instead of per-module, which is also what keeps NFR-1 true.

**Consequences.**
- A protected module's static assets 401 before login; the browser experience is carried entirely by the gate's login page (T4.4). `curl` flows need a cookie jar or an API key.
- No per-session revocation; the key file is the global lever. Sessions survive restarts and binary upgrades.
- Every module — protected or not — gains `GET /api/auth/mode` and `GET /healthz`. Recorded as the universal gate's only observable additions under a no-auth config (AC-1).
- `cmd/server/main.go` becomes security-relevant wiring: gate + body limit inside the memoization, origin check + headers outside dispatch, `FromConfig` fatal on invalid policy.
- The admin module holds three powers no other module gets (writes config, always-on method, cannot exist unprotected) — enumerated once in FR-M6 and L12, closed to extension.
- `go.mod` gains golang-jwt, go-ldap, go-webauthn (+transitive tree) even when auth is off.

**Follow-ups (not in this plan).**
- Revisit obsidianoid's `auth.modules` entry (N-5 git-push surface) — operator's choice once the mechanism exists.
- Passkey e2e with a real authenticator once hardware exists (T3.8's named skip).
- Consider surfacing throttle state (`GET /api/auth/mode` extras) if the login page ever wants a "try again in Ns" hint.
- Delete `reference/multissh/` once round two has run in production (round-one follow-up, still pending).

---

## 10. Open questions

None. Both iteration-1 questions were ruled by review:

- **L6 (`admin_pin` as a distinct token method):** endorsed by both reviewers independently — settled. The deviation from FR-M2's literal "method pin" wording is now recorded in the L6 row itself, with its rationale and the mode/login-page corollaries.
- **T5.3 splice mechanism:** ruled — the `map[string]json.RawMessage` re-marshal fails AC-14 deterministically (`encoding/json` sorts map keys, compacts `RawMessage` contents, HTML-escapes `<`, `>`, `&`); the `json.Decoder`-offset textual splice is the only mechanism (T5.3).

---

## 11. Revision log

### Iteration 2 (2026-09-10) — response to Architect + Critic REVISE

Findings marked *converged* were raised independently by both reviewers and are settled rulings.

| Issue | What changed | Tasks touched |
|---|---|---|
| 1 *(converged: Architect C1 = Critic C1)* | Admin gate hole closed: protected-predicate is now `(module ∈ auth.modules) \|\| module == "admin"` — admin is always protected when routed; a matrix save deleting admin's entry cannot unprotect it. Tests added for both empty-matrix encodings (`"admin": []` and no key) and the delete-entry save. Absorbed into pre-mortem scenario 1's mitigations. | T4.2 (step 3), T5.2, pre-mortem 1 |
| 2 *(converged: Architect C4 = Critic C2)* | `RawMessage` re-marshal dropped as deterministically AC-14-breaking; the `json.Decoder` `Token()`+`InputOffset()` textual splice is the only mechanism; test upgraded to whole-file byte compare excluding only the auth member's range. §10 open question removed (ruled). | T5.3, pre-mortem 2, R16, §10 |
| 3 *(converged: Architect C2 = Critic I1)* | `ValidatePolicy` gains `adminRouted bool` (routing lives on `config.Config`, not `AuthConfig`; `knownModules` is the build universe, not the routed set); threaded through `FromConfig` and admin's save path so FR-M3's "same code path" is the same call. | T3.1, T4.5, T5.3, L8, L12, T5.1 |
| 4 *(converged: Architect I1 = Critic I4)* | `BuildPolicy(a config.AuthConfig) (*Policy, error)` now defined in T4.1; `Policy` carries **all** hot-swappable state (matrix, PIN/key tables, admin-PIN source, LDAP, passkey RP, session settings). `FromConfig` = validate+build+store; live-apply = validate+build+one atomic swap — no restart-needing admin edits. | T4.1, T5.3 |
| 5 *(converged: Architect C3 = Critic minor)* | `middleware.SameOrigin` is a pure predicate; multissh's `CheckOrigin` becomes a module-local wrapper emitting the existing `Auditf` line (audit_test.go:136 stays green unmodified); `OriginCheck` logs its own rejections. | T2.1 |
| 6 *(converged: Architect I4 = Critic I2)* | NFR-3 greps made real: slideshow `handler.go:95,100,110,115` + `broker.go:63` added to T1.4's scope (keep-shape-where-tested; slideshow named explicitly); obsidianoid count corrected to 16. `r.Method !=` grep exempts multissh (`server.go:130/:153/:194` — tested module-local semantics, reason stated), mirrored in T6.3. Grocery counts corrected: 13 `r.Method !=` sites, 9 Decode sites. | T1.4, T1.5, T2.6, T6.3 |
| 7 *(Architect I3)* | FR-A10b now enforced on passkey ceremony routes: `login/begin` and `login/finish` reject 400 before any ceremony work when the module's list ∌ `"passkey"`; test added. | T4.3 |
| 8 *(Architect I5 + Critic minor)* | Grocery sub-route patterns enumerated (`/api/recipes/reorder`, `{id}`, ingredients, `/api/items/{id}`); 405 fallbacks exact-path-only (never subtree); empty-id `DELETE /api/recipes/` 404 preserved; reorder-before-`{id}` kept; `handler.go:53` 405 mechanism confirmed sound. | T1.5 |
| 9 *(Architect minors)* | `jwt.WithValidMethods(["HS256"])` required on parse; AC-1 asserts identity with today's response for unknown paths (SPA index.html fallback), not a hard-coded 404/405. | T3.2, T4.6 |
| 10 *(Critic I3)* | Admin login-page dead end fixed: `/api/auth/mode` on admin reports the effective set incl. `"admin_pin"`; login page shows the PIN form on methods ∩ `{"pin","admin_pin"}`; assertions added. L6 row now records the explicit deviation from FR-M2's literal "method pin" wording and why (reviewer-endorsed). | T4.2 (step 2), T4.4, T5.2, L6 |
| 11 *(Critic I5)* | Per-module route matrix added (shared by T2.3/T4.6): menuserver corrected to zero write routes / no SSE (foreign-origin POST → 403 under enforce, module's own 404/405 under `"off"`); multissh corrected to WS, not SSE. | T2.3, T4.6 |
| 12 *(Critic I6)* | `-hash-pin` gets a non-TTY fallback (`!term.IsTerminal` → buffered stdin line read) so the piped Verify command works; `golang.org/x/term` added to the dependency task. | T3.4, T0.2 |
| 13 *(Critic minors)* | T4.5 contradiction fixed ("503 stubs are replaced wholesale, ungated"); unknown `origin_check` value is a boot error, never silent enforce; T3.7 notes the reference ships zero test files (the FRD's "existing fake" claim is wrong — the seam is new code); T1.2 executor note on obsidianoid `vault.go:83/92`'s call shape vs `ConfineTo` (add a second helper if it doesn't fit); test added: admin `/api/` routes return 401 JSON regardless of `Accept`, never the login page. | T4.5, T2.3, T3.7, T1.2, T4.2 (step 6), T4.4 |
| — *(Architect steelman)* | Kiosk re-cookie chore acknowledged honestly as risk R21 (sliding TTL + API keys as mitigations; not engineered away). | §7 (R21) |

### Iteration 3 (2026-09-10) — response to Architect REVISE (Critic approved iteration 2)

| Issue | What changed | Tasks touched |
|---|---|---|
| Architect I2 *(iteration-1 carryover — dropped without a log row in iteration 2)* | `server.sse_max_subscribers` now has a real config→broker path. The old wiring sentence was unimplementable: brokers are built inside module Build functions that see only their own config section, and Build signatures cannot change (module-N+1 invariant). Fix mirrors the existing expander pattern (config.go:206-221): `config.Load` copies the effective cap (0 → 64) into a new `SSEMaxSubscribers` field on each broker-owning module's config struct (grocery, todo, slideshow, obsidianoid); each Build passes its own field to `SetMaxSubscribers`. Test added: `sse_max_subscribers: 2` → third subscriber through a real module route gets 503 (proves the copy-down, not just the broker unit). | T1.3 |
| Minor *(both reviewers)* | Grocery's four `switch r.Method` multiplexers (`handler.go:168, 210, 357, 420`) named explicitly as dissolving into the method patterns — the greps don't catch a leftover switch. | T1.5 |
| Minor | The T6.3 AC-walk commit message restates the multissh `r.Method` grep exemption and its reason, so AC-10's "greps empty" claim reads honestly. | T6.3 |
| Minor | If the `ConfineAbs` fallback helper lands, `fspath_test.go`'s table-driven tests must cover it with the same traversal table. | T1.2 |
| Minor | multissh `broadcast.go:399`'s second, differently-prefixed `Auditf` call noted: covered by the wrapper approach without change, no test asserts that variant, executor must not touch it. | T2.1 |
| Minor | Unknown-`origin_check` boot error located: `log.Fatalf` in `cmd/server/main.go`'s startup validation after `config.Load` (L9's fatal-boot pattern). | T2.3 |
| Minor | Absent-`"auth"` insertion requires a comma after the preceding member — stated for the executor (the byte-compare test would catch it, but it is now written down). | T5.3 |

### Iteration 3 — final (2026-09-10) — consensus reached (Architect APPROVE, Critic APPROVE); two directed wording edits, independently converged, settled

| Issue | What changed | Tasks touched |
|---|---|---|
| Final edit 1 *(converged — supersedes iteration 3's "must not touch" note, which contradicted T2.1's own one-predicate rule)* | `internal/multissh/broadcast.go:388-409` (package `multissh`, not `sshproxy`) is a complete second copy of **both** `sameOrigin` and `originHost`, untouched by the sshproxy wrapper. Directed fix now in T2.1: broadcast.go's `sameOrigin` becomes a second module-local wrapper (`middleware.SameOrigin` → true, else the existing `sshproxy.Auditf("broadcast ws upgrade rejected origin=%q host=%q", ...)` line verbatim, return false); the now-dead private `originHost` at broadcast.go:403-409 is **explicitly deleted** (Go won't flag it unused); `internal/multissh/broadcast.go` added to T2.1's Files; one test assertion added mirroring audit_test.go:136 with the `broadcast ws upgrade rejected` prefix. | T2.1 |
| Final edit 2 *(converged)* | The per-module `SSEMaxSubscribers int` copy-down fields carry `json:"-"`: `Load` clobbers them unconditionally, so a serializable tag would be a silently-dead per-module knob and `WriteDefault` would emit a phantom setting; zero AC-14 interaction (the T5.3 splice never re-marshals module sections). | T1.3 |

Status advanced to **pending approval — consensus reached**. No further review iterations; awaiting operator go-ahead for execution.
