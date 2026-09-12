# Implementation Plan — Meld `multissh` into unified-webapp

**Date:** 2026-09-09
**Status:** v8 — **pending approval**. Consensus iteration 4 returned `ITERATE`, narrowly: Architect found **no blockers** on v6→v7 ("v7's sweep is the first one in four iterations I could not break"), and Critic found **no critical findings**, both landing on the same verdict — the two v7 blocker fixes are correct in *policy*, but the object they both hang on, `auth.Gate`, was described in three fragments and defined by none of them. v8 defines it (new task **3.1c**) and closes the seam around it: the `FromConfig` signature, the predicate's call site, the mounting guard that keeps `POST /api/auth/login` off unprotected modules, the removal set for `server.go`, and the Phase-3 verify command that was not running the package Phase 3 creates. Every remaining fix was compile-visible and had exactly one correct form. Do not execute.
**Branch:** `dev` — commits allowed, **NEVER push**
**Authoritative requirements:** `/opt/unified-webapp/multissh-FRD.md`
**Reference source:** `/opt/unified-webapp/reference/multissh/`

---

## 0. Operator decisions folded into v5/v6

Three decisions taken with the operator **after** v4 was approved. None is settled by the FRD; each widens scope beyond it. They are recorded here because the rest of the plan now depends on them, and because v4 decided two of them the other way.

### O-1 — Authentication is a platform capability, landed now for all six modules

The operator's direction: *"we want to land this for all modules rather than wait. DRY all the way."* v4 placed the ported auth stack at `internal/multissh/auth` and recorded "consider lifting it to `internal/platform/auth` if a second module ever needs LDAP" as a follow-up. That follow-up is now the work.

- The package lands at **`internal/platform/auth`**, with **zero multissh-specific coupling**.
- Auth config is a **top-level `"auth"` section** on `Config` — `mode`, `ldap{}`, `passkey{}`, session/idle TTLs, cookie flags, store path — **not** nested under `multissh`.
- Opt-in is per module: **`auth.protected_modules: ["multissh"]`**. `multissh.secure_mode: true` is **retained as an alias** that implies membership, so **AC-6 still passes against its literal FRD wording** (`secure_mode: true` + `auth.mode: "ldap"` → 401 + login page).
- The gate is applied **once, in `cmd/server/main.go` at registration**, wrapping the module handler for every protected module. Modules stay auth-unaware; **`multissh.Build()` returns its handler without any auth middleware.**
- The login / logout / mode / passkey-ceremony routes are mounted **by the platform gate**, in front of whichever module it protects, so all six share **one implementation**. The reference's `auth_handlers.go` logic therefore moves into `internal/platform/auth` rather than into `internal/multissh` (amends task 3.2).

**One implementation is not one login (C3 — corrected in v6).** v5 claimed "one session cookie" for all six. That is false as specified. `setSessionCookie` (`reference/multissh/internal/server/auth_handlers.go:233-243`) sets no `Domain` attribute, so the cookie is **host-scoped**; unified dispatches modules by **Host header**, so each module lives on a different hostname. A session established on `multissh.lan` is not sent to `grocery.lan`. The operator therefore logs in **once per protected module hostname**. Accepted as-is: this is a home lab with a single operator, and domain-wide cookies would require a shared parent domain plus a `Domain=` attribute that widens the cookie to every host under it. Recorded here so nobody discovers it as a bug later.

**What this buys.** One LDAP integration, one session implementation, one login flow for all six modules — the operator's stated end state, reached directly instead of via a port-then-lift.

**What it does *not* yet buy (M9).** Only multissh ships a login UI. `internal/platform/auth`'s gate returns 401 JSON; it does not serve a login page. A module added to `protected_modules` today will serve its `index.html` and then 401 every XHR the SPA issues — a **broken** app, not a gated one. Therefore: **`protected_modules` is operationally usable for `multissh` only until a shared login page lands** (§6 follow-up). 4.9 logs a startup warning for any non-multissh module in the list. The platform placement is still the right call — it means the shared login page is a frontend task, not another backend lift.

### Deployment context this plan is calibrated to

Single-operator home lab on a LAN, behind nginx (which terminates TLS), reachable from outside only over Tailscale. It is not a product and will not be served on the open internet over HTTP. Consequences for this plan, stated once so reviewers stop re-deriving them:
- Threats are modeled at "a hostile web page the operator visits while on the LAN" (pre-mortem scenario 2), **not** at an internet-facing attacker.
- Where a constraint can be **documented** or **structurally guaranteed**, this plan documents it. No deny-by-default policy types, no compile-time enrollment obligations. The operator holds these invariants; encoding them in the type system buys little here and costs real complexity.
- **O-4 (note, not a decision).** `main.go:78` binds `0.0.0.0:%d`, not loopback. A `bind_addr` config field defaulting to `127.0.0.1` was considered and **demoted to a note** — the operator may want `0.0.0.0` for testing and considers the exposure understood. No task; recorded so a future reader does not mistake the bind for an oversight.

**What it costs, stated plainly.** This is the largest single scope addition in the plan. It converts a module-local port into a platform feature with its own config surface, its own test suite, and a login path in front of five modules that have never had one. It also touches `cmd/server/main.go` and `internal/platform/config` more deeply than v4 did. If the schedule matters more than the generalization, the fallback is v4's placement plus the lift as a follow-up — that option is real and is not being dismissed, only declined.

**New hazard it introduces.** v4's pre-mortem hazard (returning `mux` instead of `srv.Handler()` silently disables auth) disappears, because multissh no longer owns its gate. It is replaced by a different one: **a module can be registered without the gate and be silently unprotected**, with every green test still green. Mitigation is task 4.9's assertion that *every* module named in `protected_modules` returns 401 unauthenticated — see pre-mortem scenario 1 (§1A).

### O-2 — Passkeys are a supported option, not a construction-only stub

The operator's direction: *"yes, I would like passkeys as an option as well."* v4 shipped FR-A5 "exercised only by construction", reasoning that WebAuthn needs an HTTPS origin and TLS is out of scope.

**That reasoning conflated two different things and is withdrawn.** *(v6 — the premise stated in v5 was itself wrong and is corrected here; the conclusion survives.)* v5 wrote that "TLS out of scope" means *no TLS terminator inside the Go binary*. It does not: `cmd/server/main.go:84` already calls `http.ListenAndServeTLS(addr, cfg.TLSCert, cfg.TLSKey, handler)` when certs are configured. What §6.1 actually declines is *new* TLS work — cert management, ACME, renewal — not TLS itself. The conclusion is unchanged and in fact stronger: an HTTPS origin is available **two** ways, via the binary's own listener or via the nginx front end this deployment uses. Either supplies exactly the `https://…` origin `passkey.rp_id` and `passkey.origins` are configured with. Passkeys are deployable today; v4 declined a feature the deployment already supports.

**Scope note on `rp_id` (M5).** `rp_id` is single-valued and must be a registrable-domain suffix of every origin in `passkey.origins`. Modules are dispatched by Host header, so six modules mean up to six hostnames — a single `rp_id` covering all of them requires setting it to their shared parent domain, which scopes credentials to that parent. Since passkeys front **multissh only** for now (see M9 in §0), set `rp_id` to multissh's own hostname and list only that origin. Widening it is a decision to take when a second module actually gets a login page, not now.

- `auth.mode: "ldap_passkey"` ships as a **supported** mode, available to any module in `protected_modules`.
- **Testable without a hardware authenticator** (task 3.8): registration- and assertion-option generation; challenge storage, single-use invalidation, and expiry; `rp_id`/`origins` validation rejecting a mismatched origin; credential-store persistence round-trip; and the `EnablePasskeys` config-validation path (missing `rp_id` or empty `origins` → `Build` error).
- **Genuinely blocked until the lab exists:** an end-to-end ceremony with a real authenticator over real HTTPS. Written against the fixture seam and `t.Skip`ped with the missing dependency named — not silently omitted.

### O-3 — The origin control is platform-wide, not multissh-only

The operator's decision, taken before v4 was written and carried forward by *"DRY all the way"*: **same-origin everywhere, plus a regression test**, on the grounds that the six modules share the middleware and should share the fix.

v4 instead scoped it to multissh: 4.6a moved `Wrap` per-module so multissh alone emits no `Access-Control-Allow-Origin` while the other five keep `*`, and 4.6b put the server-side origin check **inside `internal/multissh`**. That leaves the wildcard on five modules and puts the load-bearing control in a place only one module can use — the opposite of the decision. **v5 reverses it.**

- **`internal/platform/middleware.Wrap` itself becomes same-origin for all six**: no wildcard, reflect `Access-Control-Allow-Origin` only when the request `Origin`'s host matches `r.Host`, always emit `Vary: Origin`.
- The **state-changing origin check** (v4's 4.6b — the half that actually rejects the no-preflight `text/plain` POST) also lives in `internal/platform/middleware` and applies to **all six modules**, not just multissh.
- The `sameOrigin` predicate lifts out of `reference/multissh/internal/sshproxy/proxy.go:243-245` into `internal/platform/middleware` so the WebSocket upgrader and the HTTP gate share **one** implementation binary-wide.
- **Accepted consequence:** any cross-origin consumer of the existing five modules breaks. There are none today. This is a deliberate, regression-tested exception to NFR-5, not an accident — see task 4.6 and R9.

**v4's rollback note is void.** It argued 4.6a was safely revertible because the load-bearing control (4.6b) lived in a separate, module-local file. Under O-3 both halves live in the same shared file, so that separation no longer exists. Rewritten in task 4.6.

---

## 1. RALPLAN-DR Summary

### Principles

- **P1 — Pattern conformance over novelty.** multissh must look like the other five modules: `internal/multissh/Build(cfg) (http.Handler, error)`, a `MultisshConfig` JSON section, static assets from `web/multissh`, one `case` in `buildModule`. No new server, no new port, no new build system.
- **P2 — Preserve working behavior; change only what the FRD changes.** The reference code and tests are battle-tested. Port verbatim where possible; the diff should isolate to (a) import paths, (b) config/env removal, (c) embed→static_dir, (d) the five §3.4 redesign items.
- **P3 — Secrets never touch disk.** SSH private keys stay server-side and are referenced by name only; passwords (FR-N4) live in process memory for the life of a session/job and are excluded from `hosts_path`, logs, and error strings by construction, not by convention.
- **P4 — Every phase independently verifiable.** Each phase compiles and tests green on its own, so ralph can execute tasks in order without a "big bang" integration step at the end.
- **P5 — Fail closed on security toggles, open on convenience defaults.** `strict_host_key` and `secure_mode` default off (lab usage) but when enabled they must hard-fail rather than degrade.

### Decision Drivers (top 3)

1. **D1 — Test parity is the acceptance gate.** FR-I7 + AC-1 require the ported suites to pass under `go test -race ./...`. Any approach that discards reference tests loses the only cheap correctness signal for ~2,500 lines of concurrency-heavy Go.
2. **D2 — One build pipeline.** FR-I4 + NFR-1: root `package.json` esbuild and `make build` must remain the only build entry points. A second toolchain (Vite) inside the repo is an ongoing maintenance and CI cost.
3. **D3 — Redesign surface is mostly frontend.** Four of five FR-N items (N1 layout, N2 collapse, N3 history, N5 `key:` keywords) are UI-only; only N1's limit plumbing and N4's password path touch Go. This argues for maximum Go reuse and a willingness to restructure TS.

### Viable Options

#### Backend axis

**Option A — Port-and-adapt the reference Go code (CHOSEN).**
Copy `internal/{server,sshproxy,auth,config}` into `internal/multissh/{,sshproxy,auth}`, rewrite import paths, drop `embed.go`, re-express `cmd/multissh/main.go`'s `run()`/`buildAuth()` as `Build()` (Phase 4) rather than discarding it, fold config into `internal/platform/config`, thread `MaxSessions` through `hosts.go`/`broadcast.go`, add a password credential path.

- Pros: preserves all ported tests (D1); smallest reviewable diff; concurrency semantics (WebSocket bridge, per-target transfer goroutines, broadcast registry) already race-clean; fastest to first green build.
- Cons: inherits the reference's structure (e.g. `Options` struct with 12 fields, in-memory upload registry) whether or not it is ideal for unified; carries the `auth` package's LDAP+WebAuthn dependency weight into unified's `go.mod` even when off by default; reference `internal/auth` ships **zero tests**, so the ported auth stack arrives uncovered (mitigated by task 3.8).

**Option A′ — Port everything except `auth`; hard-wire `secure_mode: false`; defer FR-A4/A5.**
Copy `internal/{server,sshproxy,config}` only, stub the `Provider` interface with `NoAuth`, and leave LDAP/passkey for a follow-up.

- Pros: drops the largest dependency subtree (go-ldap, go-webauthn, go-tpm, cbor) from `go.mod`, shrinking the binary and the `CGO_ENABLED=0`/arm64 cross-build risk (R5); removes the only untested reference package from the diff; Phases 3 and 6 both get materially shorter.
- Cons: FR-A3, FR-A4 and FR-A5 are in-scope requirements, and **AC-6 gates explicitly on `secure_mode: true` + `auth.mode: "ldap"` producing a 401 and a login page**. A′ cannot satisfy that criterion at all.

**Invalidation of A′:** rejected on requirement coverage, not on merit — it is genuinely the lower-risk build. It loses solely because AC-6 is a hard acceptance gate on LDAP login. If the FRD's auth scope is ever renegotiated, A′ becomes the preferred backend option and should be reconsidered before A is re-derived.

**Option B — Rewrite the module against unified's idioms.**
Re-implement the SSH bridge, SFTP fan-out, and handlers fresh, reusing only `sshproxy` primitives.

- Pros: cleaner fit with unified conventions; opportunity to drop LDAP/WebAuthn until actually needed; no dead env-var/embed code to prune.
- Cons: discards the reference test suites (violates D1 and FR-I7 as literally written); reintroduces subtle bugs in PTY framing, origin checks, and partial-upload cleanup; multiplies effort with no FRD-visible benefit.

**Invalidation of B:** FR-I7 names the specific suites to port and AC-1 gates on them; a rewrite cannot satisfy "port the reference test suites" without also rewriting them, at which point the tests validate the new code against itself. B is rejected as failing a hard requirement, not merely as more expensive.

#### Frontend axis

**Option C — Port TS sources and build with `esbuild --bundle` (CHOSEN).**
Move `web/src/*.ts` + `ssh.css` to `web/multissh/js/`, add `@xterm/xterm` + `@xterm/addon-fit` to root `package.json` dependencies, add a bundling esbuild invocation to the root `build` script, hand-write `web/multissh/index.html`.

- Pros: satisfies D2 (one toolchain); sources are editable in-repo, which is mandatory since FR-N1..N5 require real TS changes; `tsc --noEmit` covers the new files via the existing root `tsconfig.json`; consistent with how every other module is built.
- Cons: this is unified's first bundled module — esbuild flags diverge from the existing bundleless invocations; xterm CSS must be routed through esbuild's CSS handling (it lives in `node_modules`, so hand-copying is not an option — see 5.3); `node_modules` gains runtime deps; the root `tsconfig.json` `include` list is an explicit allowlist that must be extended or typecheck silently skips the module (5.3b).

**Option D — Keep the prebuilt Vite `dist/` artifacts.**
Copy `reference/multissh/internal/server/dist/` into `web/multissh/` and serve as-is.

- Pros: zero frontend build work; immediately serves a working UI; useful as a temporary smoke-test artifact.
- Cons: **fatal** — the redesign items FR-N1..N5 require source edits, and the `dist/` bundle is minified with hashed filenames; no `tsc` coverage; violates FR-I4's explicit "port ... compiled by esbuild via the root `package.json`".

**Invalidation of D:** FR-I4 mandates esbuild-built sources under `web/multissh/`, and §3.4 cannot be implemented against a minified artifact. D is rejected outright. *(v1 kept D as a throwaway Phase-4 smoke test; that task is dropped — copying in a `dist/` only to delete it in Phase 5 adds a checked-in artifact, a cleanup step, and a chance of shipping stale bundles, while proving only that a file server serves files, which task 3.3's handler tests already prove.)*

### Chosen combination

**A + C:** port-and-adapt the Go, port-and-restructure the TS with `esbuild --bundle`.

---

## 1A. Pre-mortem (deliberate mode)

*It is March 2027. This shipped. Four ways it went wrong.*

**Scenario 1 — "Five modules were behind a login page that was never actually mounted."**
The operator stood up LDAP, added `grocery` and `todo` to `auth.protected_modules`, restarted, saw the login page on multissh, and considered it done. Grocery and todo served unauthenticated the entire time: the gate is applied in `cmd/server/main.go`'s registration loop, and the branch that consults `protected_modules` was only ever exercised for multissh, whose membership arrives via the `secure_mode` alias on a different code path. Every test was green, because the auth test named multissh explicitly instead of iterating the configured list.
*Root cause:* under O-1 the enforcement point moved from the module to the dispatcher, and the tests did not move with it. This is v4's `mux`-vs-`Handler()` hazard reappearing one layer up — the same failure shape, a different address.
*Task changes:* 4.9's 401 test **iterates `protected_modules` from config**; the `secure_mode` alias is resolved into that list *before* the loop, so both routes into membership share one code path; 3.2 removes the auth field from multissh's `Options` so the module-local variant cannot come back.

**Scenario 2 — "A web page fanned a file out to production hosts."**
The operator visited a hostile page while on the LAN. Its JavaScript `POST`ed to `/api/broadcast` with `Content-Type: text/plain` — a CORS simple request, no preflight — and the payload SFTP'd to every saved host. No login was required because `protected_modules` was empty, by design, on a trusted LAN; network position *was* the security model, and the shared `Access-Control-Allow-Origin: *` extended that position to any page the operator opened. The team had "fixed CORS" by removing the header, which changed nothing: ACAO governs whether the attacker may *read* the reply, never whether the request executes.
*Root cause:* a posture change introduced by integration (the reference never set CORS headers; it ran standalone), plus a fix aimed at the wrong half of the mechanism.
*Task changes:* 4.6b — server-side rejection, not header removal — is declared load-bearing and unrevertible; its test asserts **no job was created**, not merely that a header is absent; O-3 puts it in front of all six modules.

**Scenario 3 — "The password was in the logs the whole time."**
Someone debugging a broadcast failure added `log.Printf("target: %+v", t)` to `broadcast.go`. `broadcastTargetRequest` was a plain struct, so every password fanned out that day landed in journald in plaintext and stayed there for the journal's retention window. The redacting credential type existed and worked — but it sat one layer downstream of the wire struct the password actually arrived in, so it never saw the value. AC-10's grep had passed at ship time because it only walked the paths the manual pass happened to walk.
*Root cause:* redaction applied to *a* type rather than to **every** type that holds the secret.
*Task changes:* the `secret` type is applied to `clientMsg`, `broadcastTargetRequest` **and** the credential; redaction is asserted by name on all three under `%v`/`%+v`/`%s`/`%#v` and `json.Marshal`; 6.1's audit-line test asserts no credential field ever reaches the log sink.

**Scenario 4 (O-2) — "Passkeys had 400 lines of green tests and had never authenticated anybody."**
The operator enrolled a passkey, it registered, and login worked — until they reached the lab from a second hostname and the browser reported no credential available. `rp_id` had been left empty in the config. `go-webauthn` accepted it: `webauthn/types.go:141` validates `RPID` only `if len(config.RPID) != 0`, and errors solely on empty `RPOrigins` (`:166`). Every test in 3.8 passed, because they all fed the *configured* `rp_id` into option generation and asserted it came back — a tautology when the value is empty on both sides.
*Root cause:* O-2's test list was written against the library's behavior as assumed rather than as read, and the one validation the plan relied on the library to perform is one it does not perform.
*Task changes:* the empty-`rp_id` rejection is **ours**, and it moves to config-load time (**1.2c**) so a bad value cannot reach a running process; 3.8's option-generation tests assert against a **literal expected `rp_id` string**, never against the config value they were fed; `rp_id` is scoped to multissh's hostname alone (M5) rather than a parent domain, so the multi-hostname failure this scenario describes cannot arise while only one module has a login page.

---

## 2. Implementation Phases

Each numbered task is designed to be an independently executable ralph task: it names the files it touches, what to do, and how it is verified. Tasks within a phase are ordered; phases are strictly sequential.

### Phase 0 — Baseline & scaffolding

**0.1** Confirm branch is `dev` and the working tree builds green before any change: `git rev-parse --abbrev-ref HEAD`, `go build ./...`, `go test -race ./...`, `npm run typecheck`. Record the baseline result. Do not proceed if baseline is red — fix or report first.

**0.2** Record the verified preconditions this plan depends on (re-check, do not assume):
- `internal/platform/middleware.Wrap` does **not** wrap `http.ResponseWriter`, so `http.Hijacker` survives to the module handler and WebSocket upgrades work behind the dispatcher.
- `middleware.Wrap` sets `Access-Control-Allow-Origin: *` on the **entire** dispatcher — addressed in task 4.6.
- `cmd/server/main.go` calls `buildModule` once **per hostname** in the `cfg.Routing` loop — addressed in task 4.5.
- `reference/multissh/internal/auth` defines its **own** `auth.Config`/`auth.LDAPConfig`/`auth.PasskeyConfig` and contains **no test files**.
- **The `http.Hijacker` constraint binds the *new* middleware too.** 4.6a/4.6b/4.9 all insert handlers in front of the module. Any of them that wraps `http.ResponseWriter` (to capture a status code, say) breaks the multissh WebSocket upgrade. Rule for all three: **do not wrap `ResponseWriter`.** If one ever must, it implements `Hijack()` by delegation, and 4.9's test suite includes a real upgrade through the full chain.

**0.2b — Per-module route inventory (verified; precondition for 4.6b and 4.9).** The auth gate and the origin middleware both need to know which paths a module serves. multissh's URL contract — `/api/*` is data, everything else is a static asset — is **not** shared by the existing five. Verified from source:

**(v7: rebuilt line-by-line from the handlers. v6's table was labelled "verified" but was assembled from review quotes rather than read from source, and was wrong for `todo` and `slideshow` — it missed four routes including a write route, and rendered a config-derived prefix as a literal. Corrected below; this is the table 4.6b and 4.9 are derived from, so it had to be right.)**

| Module | Data routes (must be gated) | Private prefixes for 4.9 |
|---|---|---|
| `menuserver` (`handler.go:24-27`) | `GET /config`, `/config/`, `/items`, `/menus/{subject}/{item}` | `/config`, `/items`, `/menus/`, `/api/` |
| `todo` (`handler.go:28-39`) | `GET /config`, `/config/`, `/config/columns`, `/config/settings`, `/items`, `/items/{subject}/{item}`, **`/api/events`** (`:39`); **`POST` `/config/columns`, `/config/settings`, `/items/{subject}`, `/items/{subject}/{item}`, `/items/{subject}/{item}/{newSubject}`** | `/config`, `/items`, `/api/` |
| `slideshow` (`handler.go:27-42`) | `GET /config`, `/config/` (`:30`), **`/api/state`** (`:33`), **`/api/events`** (`:34`), **`POST /api/control`** (`:35`), `GET /items`, `GET /{cfg.Prefix}/{subject}/{item}` (`:39`), `GET /audio/{collection}/{track}` | `/config`, `/items`, `/audio/`, `/api/`, **`/` + `cfg.Slideshow.Prefix` + `/`** |
| `grocery` (`handler.go:29-43`) | all under `/api/` | `/api/` |
| `obsidianoid` (`handler.go:29-39`) | all under `/api/` | `/api/` |
| `multissh` | all under `/api/` | `/api/` |

**Two corrections that change the derived tasks.** (1) `todo` and `slideshow` are **mixed**, not bare-path — each has real `/api/` routes, and slideshow's `POST /api/control` is a cross-origin *write* target that v6's "No" column hid. The C4 conclusion survives (bare-path data routes do exist on all three of menuserver/todo/slideshow, which is what breaks the multissh-shaped predicate) but the "`/api/`-shaped? No" framing was false and is dropped. (2) **`slideshow`'s image prefix comes from config** (`handler.go:39`: `"GET /"+h.cfg.Prefix+"/{subject}/{item}"`), so its private list must be computed at registration — it cannot be written as a constant.

Every module also registers a catch-all `mux.Handle("/", …)` static file server. **This is why a naive `/api/*`-scoped test passes on a module the control does not actually cover** — the catch-all answers, so the assertion sees a response and concludes the route exists. Tasks 4.6b and 4.9 are specified against this table, not against the `/api/` assumption.

**0.3 (FR-I6)** Merge Go dependencies into `/opt/unified-webapp/go.mod` by adding the reference's direct requires and running `go mod tidy`:
`golang.org/x/crypto`, `github.com/pkg/sftp`, `github.com/gorilla/websocket`, `github.com/go-ldap/ldap/v3`, `github.com/go-webauthn/webauthn` (indirects resolve automatically). Also add `go.uber.org/goleak` as a **test-only** dependency if the preferred 6.1b approach is taken — added here so a version conflict surfaces alongside the rest of the merge rather than in Phase 6. Verify `CGO_ENABLED=0 go build ./...` still succeeds.

**Verify Phase 0:** `go build ./...` && `go test -race ./...` && `git status` shows only the intended new files.

---

### Phase 1 — Config plumbing (FR-I3)

**1.1** Add to `/opt/unified-webapp/internal/platform/config/config.go`:
- `MultisshConfig` struct with JSON tags: `static_dir`, `ssh_dir`, `upload_dir`, `hosts_path`, `browse_root`, `max_sessions`, `max_upload_bytes`, `secure_mode`, `strict_host_key`, `known_hosts_path`. **No `auth` tag** — *(v7: v4 copied FR-I3's field list verbatim, including `auth`, and the bullet two lines down has said "It gains no `auth` sub-object" ever since; both survived v5 and v6 as a live contradiction in the very first config task. O-1 moved `auth` to top level, so the tag is deleted here and the two bullets now agree.)*
- **Auth config is top-level, not nested under `multissh` (O-1 — changed from v4).** Add `Auth AuthConfig `json:"auth"`` to `Config`, with `AuthConfig`/`LDAPConfig`/`PasskeyConfig` mirroring `reference/multissh/internal/config/config.go` **field-for-field, minus `Addr` and minus all `MULTISSH_*` env handling**, plus one new field: **`protected_modules []string`**, naming which modules the gate fronts (4.9). Tags are normalized to snake_case (`session_ttl_minutes`, `rp_id`, `origins`, `passkey_store_path`, …) to match unified's existing sections — no config survives the move to a new file under a new key anyway, so preserving the reference's camelCase would buy nothing and leave a mixed schema forever.
- `MultisshConfig` keeps **`secure_mode`** as an alias that implies membership in `protected_modules` (O-1), so AC-6's literal wording still holds. It gains no `auth` sub-object.
- `Multissh MultisshConfig \`json:"multissh"\`` field on `Config`.

**1.2** Extend `DefaultConfig()` with the multissh defaults: `static_dir: "./web/multissh"`, `hosts_path: "./data/multissh/multissh-hosts.json"`, `max_sessions: 3`, `max_upload_bytes: 8589934592`, `secure_mode: false`, `strict_host_key: false`; and on the top-level section, `auth.mode: "none"` with `auth.protected_modules: []`. **Do not port `applyAuthDefaults` (reference `config.go:180-219`)** — it hardcodes private lab values for `ldap.url`, `baseDN`, `searchBase` and passkey `rpID`/`origins`, which `WriteDefault` would then emit into every operator's config file. Those fields get *validation* at config-load time (**1.2c**) instead of defaults — v5 said 4.4, which under O-1 no longer touches auth. Leave `ssh_dir`, `upload_dir`, `browse_root`, `known_hosts_path` empty — they are resolved at Build time (task 4.2), not baked into the file, so the defaults stay portable.

**1.2b** Validate `max_sessions` in **exactly one place** — `config.Load` — so no other code re-checks it: reject `< 1` with a clear error; treat `0` as "unset" and substitute the default `3`; clamp values above `16` to `16` with a `log.Printf` warning (an absurd N is an operator typo, and the UI cannot render hundreds of panels). `Build()` (4.3) then trusts the value and performs **no** re-validation.

**1.2c — Reject auth configurations that would silently fail open (M7, C5).** `config.Load` errors — refusing to start, not warning — on any of:
- `multissh.secure_mode: true` **and** `auth.mode: "none"`. v5 would have wrapped multissh in a `NoAuth` gate: every request "authenticated", no login, no error, a config that reads as secured. This is the exact shape of pre-mortem scenario 1.
- `auth.protected_modules` non-empty **and** `auth.mode: "none"` — same failure, stated the other way round.
- `auth.mode: "ldap_passkey"` with empty `passkey.rp_id` or empty `passkey.origins` (see 3.8/M5 — go-webauthn does **not** reject an empty `RPID`; only empty `RPOrigins` errors, so this check is ours to write).
- *(recorded, not implemented)* `auth.mode ∈ {"ldap","ldap_passkey"}` with an **empty resolved protected set** is the third member of this family — a config that reads as secured, starts clean, and protects nothing. It is deliberately **not** rejected: 4.3's conjunction correctly yields `NoAuth{}`, and the symptom (no login page appears anywhere) is immediate and unmissable. Noted so a future reader does not mistake the omission for an oversight.
- a name in `auth.protected_modules` that is not routed in `host_routing` — an unreachable gate is a typo, and typos here fail open. **This half is implementable in `config.Load`; the "is it a known module" half is not** — the module-name set lives in `cmd/server/main.go`'s `buildModule` switch, and duplicating it into the config package would drift the moment a seventh module is added. That check therefore runs in `main.go` at registration (4.9), not at load.

**Error policy for the auth path is fail-closed, scoped to the protected set (C5; v7 revision).** `auth.FromConfig` returning an error means **every module in the resolved protected set gets 4.7's 503 handler** — it is never downgraded to a logged warning and never falls back to `NoAuth{}`, because the degraded form of an auth gate is no auth gate. Unprotected modules serve normally.

*(v7 — why this changed from v6's `log.Fatalf`.* v6 made a `FromConfig` error fatal for the whole binary. That is strictly *wider* than 4.7's blast radius for an equally security-relevant failure, and 4.7 rejects exactly that outcome in its own words: "a `known_hosts` typo should not take grocery's shopping list offline." Under v6, an unwritable `passkey_store_path` would have taken grocery, todo, slideshow, menuserver and obsidianoid down. The narrow form is still fully fail-closed — a protected module serves **nothing**, not even unauthenticated — and it stops being an exception to D-B at all, which is simpler to reason about.)*

**What can actually fail at construction, verified against source.** `auth.NewLDAPClient` (`reference/multissh/internal/auth/ldap.go:35-41`) defaults `UserSearchFilter`, stores the struct, and returns **no error** — it performs no I/O; the dial happens in `Authenticate` (`ldap.go:48`). `auth.NewService` (`service.go:58`) likewise cannot fail. The **only** construction-time error source in the whole path is `EnablePasskeys` (`service.go:69-93`), via `newPasskeyStore` or `webauthn.New`.

Two consequences the plan must state rather than imply:
- **LDAP reachability is deliberately not validated at startup.** An unreachable directory surfaces as a 401 at first login, not as a boot failure. That is the right behavior here — the DC may well boot slower than the webapp — but v6 implied it was checked, and it is not.
- Tests: `mode: "ldap_passkey"` + `protected_modules: ["multissh"]` + **a valid `rp_id` and exactly one `origins` entry** *(v8 — without these the fixture is rejected by 1.2c's own third bullet at `config.Load` and never reaches `EnablePasskeys`, so the test would pass for the wrong reason)* + a `passkey_store_path` holding **invalid JSON** (`store.go:114-121` returns `parse passkeys file: %w`) → multissh 503s, the other five serve normally. A `mode: "ldap"` config **cannot** produce a construction error at all; assert that it starts cleanly even with a bogus `ldap.url`.

**Also validate the fail-*closed* mirror.** `auth.mode: "ldap"` or `"ldap_passkey"` with an empty `ldap.url` or empty `base_dn` → `config.Load` error. Such a config constructs cleanly (per the above) and then 401s every login attempt forever, which reads as "LDAP is broken" rather than "LDAP is unconfigured".

**1.3** Add `expandMultisshPaths(*MultisshConfig) error` following the existing `expand*Paths` helpers (`ExpandPath` on `static_dir`, `ssh_dir`, `upload_dir`, `hosts_path`, `browse_root`, `known_hosts_path`, and, on the new top-level `Auth` section, `auth.passkey_store_path`) and call it from `Load()` alongside the other five.

**1.4** Port the applicable cases from `reference/multissh/internal/config/config_test.go` into `internal/platform/config/config_test.go` (or a new `config_multissh_test.go`): JSON round-trip, defaults, `~` expansion, auth-object parsing. **Drop** every env-var test case — the env layer is intentionally removed (FR-I3).

**1.5** Add a `"multissh"` section to `/opt/unified-webapp/unified-webapp-example.json` with every field and a commented-style example `host_routing` entry `"multissh-test.cmdhome.net": "multissh"`; document the section in `/opt/unified-webapp/README.md` next to the existing five.

**1.6** Decide and document the `make init-config` representation of the deferred-default fields (`ssh_dir`, `upload_dir`, `browse_root`, `known_hosts_path`): emit them as **present-but-empty strings** (`"ssh_dir": ""`), never omitted. Empty means "resolve at Build time" and is a documented, round-trip-stable value; omission would be indistinguishable from a typo'd key. Note this convention in the README section from 1.5.

**Note on JSON tag style (v6 — resolved).** **snake_case everywhere**, outer and inner: `session_ttl_minutes`, `rp_id`, `passkey_store_path`. v5 contained a live contradiction — 1.1 said snake_case, this note and R7 said the `auth` sub-object keeps the reference's camelCase, both imperative. Resolved in favor of snake_case because O-1 moved `auth` **out of** the `multissh` section to top-level platform config: it is no longer a mirror of the reference's `AuthConfig`, so "keep the tags identical for a mechanical diff" no longer applies. No existing operator config loads verbatim under any variant (every outer field is renamed and FR-I3 names the snake_case forms), so the migration cost is identical either way. The 4.3-era translation is now a rename map, written once and covered by 1.1's round-trip test.

*History (v3–v5), retained so the decision is not re-litigated.* v3 corrected v1's false claim that camelCase preserved operator configs — it never did. v3–v5 then kept the inner tags camelCase on three grounds: FR-I3 constrains only the outer names; the `auth` sub-object mirrored the reference field-for-field; `rpID` is a WebAuthn spec term. **v6 overrides all three**: under O-1 `auth` is top-level platform config, not a multissh sub-object, so the mirror argument is void and the spec-term argument does not outweigh a single consistent convention across the whole file. The migration is a documented one-time config rewrite (README, 1.5), unchanged in cost.

**Also deliberate (v3): the `max_sessions > 16` clamp in 1.2b is a deviation beyond the FRD.** The FRD requires only `>= 1`; the upper clamp is a plan-level addition on usability grounds (the UI cannot render hundreds of panels). It is a *clamp with a warning*, not a rejection, so no config the FRD calls valid is refused — but an operator asking for 64 gets 16. Flagged here so it is a reviewed choice rather than a silent narrowing.

**Verify Phase 1:** `go build ./...`; `go test -race ./internal/platform/config/...`; `go run ./cmd/server -init-config -config /tmp/mc.json` then confirm the written JSON contains a populated `multissh` block and that re-loading it yields an identical struct.

---

### Phase 2 — Port `sshproxy` (FR-S1, FR-S4, FR-U4, FR-A1, FR-A2, FR-N4 backend)

**2.1** Copy `reference/multissh/internal/sshproxy/{proxy,session,keys,hostkey,transfer}.go` to `internal/multissh/sshproxy/`; rewrite `github.com/puma/multissh/internal/...` imports to `cmd184psu/unified-webapp/internal/multissh/...`. No logic changes in this task.

**2.2** Copy the matching `*_test.go` files (`proxy_test`, `session_test`, `keys_test`, `hostkey_test`, `transfer_test`) with the same import rewrite. This is the correctness net for the whole phase — port before adapting.

**2.3** Add password credential support (FR-N4) to the auth-method plumbing. Define a `Secret` type: a struct with an **unexported** string field, a `Reveal()` accessor, and `String()`, `GoString()`, `MarshalJSON()` (emitting `"***"`) plus `UnmarshalJSON()` so it decodes from the wire normally but can never be re-encoded or formatted in the clear. Extend the credential used by `proxy.go` and `transfer.go` so a target carries **either** a key name **or** a `Secret` password, exactly one populated. Map to `ssh.Password(...)` alongside the existing `ssh.PublicKeys(...)` path.

**2.3b — Redaction must reach the wire structs, not just the credential.** A redacting credential type is useless if the request DTO holds a bare `string`. Use `Secret` as the **field type** on every struct the password passes through:
- `internal/multissh/sshproxy/proxy.go` `clientMsg` (currently `Type/Host/Port/User/Key/Cols/Rows` at proxy.go:17-23) — add `Password Secret \`json:"password"\``. The password travels **only** in this JSON connect frame; it must never appear as a query parameter, a URL path segment, or a WebSocket subprotocol value.
- the `POST /api/broadcast` target struct (`broadcast.go`).
- the **request** host DTO used by `PUT /api/hosts` (`hosts.go`) — see the split immediately below.

**2.3c — Split the hosts wire DTO from the persisted struct (v3 correction; do not skip).** In the reference, `hostConfig` (`hosts.go:15`) is used for **both** the `PUT /api/hosts` request body and the on-disk JSON written by the save path. Putting a `Password Secret` field on that single type is a **defect, not a fix**: `Secret` is a struct, so `omitempty` does not suppress it (omitempty is inert for struct types), and every persisted record would gain `"password":"***"` on disk. Worse, that file round-trips: `UnmarshalJSON` decodes `"***"` back into a live `Secret` holding the literal string `***`, so the store silently acquires a fake credential — a direct AC-10 violation introduced by the redaction machinery itself.

Required shape:
- Introduce a **request-only** DTO (e.g. `hostRequest`) carrying the wire fields **plus** `Password Secret \`json:"password"\``. Only the `PUT /api/hosts` decoder uses it.
- `hostConfig` — the persisted struct — **gains no password field at all.** It stays exactly `IP/Port/User/Key/RemoteDir`. Absence of the field is the guarantee; there is nothing to strip at write time because there is nothing to hold.
- The filter is already written: `normalizeHosts` (`hosts.go:112-140`) does not copy its input, it **reconstructs** each record field-by-field into a fresh `hostConfig{IP, Port, User, Key, RemoteDir}` (hosts.go:132-138). That field-by-field reconstruction **is** the structural filter task 3.5 asks for. **v4: `normalizeHosts` is split in two (see 3.4) rather than having its single signature retargeted** — the save/PUT path gets **`normalizeHostRequests(hosts []hostRequest, maxSessions int) ([]hostConfig, error)`**, and *that* function is the structural password filter: it takes the only type that carries a `Password Secret`, returns the only type that reaches disk, and drops the password by construction, silently and unconditionally. The load path gets `normalizeHostConfigs([]hostConfig) ([]hostConfig, error)`, which never has `hostRequest` in scope at all — so the read-from-disk path stays entirely free of Secret-bearing types, which is the property this section exists to guarantee. Do not add a separate `stripPassword` pass; a second mechanism would only be a thing that can drift.

**2.4** Add unit tests for 2.3/2.3b: exactly-one-of validation; redaction of `fmt.Sprintf("%v")`, `%+v`, and `json.Marshal` applied to a **decoded request struct** (not just a bare `Secret`) for each of the three DTOs above; a password credential produces an `ssh.Password` auth method; `Reveal()` still returns the true value.

**2.4c — Type-level and file-level proof for 2.3c.** Two assertions, both stronger than AC-10's grep (which only proves *this* password was absent from *this* file):
- **Type-level:** reflect over the persisted `hostConfig` type — **explicitly `hostConfig` and not `hostRequest`, because `hostConfig` is the only type that is ever written to disk; `hostRequest` is expected to carry a `Password Secret` and must not be asserted against** — and assert **no field's JSON tag or name is `password`** (case-insensitive), and that no field's type is `Secret`. This fails at test time if anyone later "helpfully" adds the field back, with no live password needed to trigger it.
- **File-level:** `PUT /api/hosts` with a populated `password`, then read `hosts_path` and assert the raw JSON contains **no `"password"` key at all** — not merely that the secret's plaintext is absent. Decode the file into a `map[string]any` per record and assert the key set is exactly `{ip, port, user, key, remoteDir}`. This catches the `"***"` case that a plaintext grep would pass.

**2.4b** Assert the FR-N4 lifetime bound ("life of the session/job"): after a broadcast job reaches a terminal state, the job record's credential is zeroed and unreachable — test by holding a reference to the completed job and asserting `Reveal()` returns empty. Same for a closed terminal session.

**2.5** Confirm FR-A2 (encrypted keys unsupported) and FR-A1 (strict host key, fail closed) behavior survived the port unchanged; add a test for the strict-host-key missing-file error path if the reference lacks one.

**Verify Phase 2:** `go test -race ./internal/multissh/sshproxy/...` — all ported tests plus the new ones green. `go vet ./internal/multissh/...`. Commit checkpoint on `dev`.

---

### Phase 3 — Port `auth` and `server` handlers (FR-H*, FR-U*, FR-A3..A7, FR-N1 backend)

**3.1 — Port the auth stack to `internal/platform/auth` (O-1).** Copy `reference/multissh/internal/auth/{auth,service,store,ldap,middleware}.go` to **`internal/platform/auth/`** with import rewrites and no behavioral changes — **with one assigned exception: `middleware.go`'s `isPublic` predicate is replaced here, in 3.1, by the two-list form 4.9 specifies.** *(v7: v6 left this unassigned — 3.1 said "verbatim" while 4.9 said the predicate changes, so no task owned the edit. It lands in 3.1 because that is where the file arrives; 4.9 only supplies the per-module lists.)* Everything else in the file — session lookup, cookie read, 401 shape — ports verbatim **except the `isPublic` call site**, which is removed.

**How the two pieces compose (v8 — both reviewers flagged this; it had no stated call site).** `isPublic` becomes a **method on `Gate`** (3.1c), because the two lists are `Gate` fields and the reference's package-level `isPublic(r)` takes no arguments. `Gate.Wrap` evaluates it **before** delegating to the provider. `SessionAuth.Middleware` (`middleware.go:27`) therefore **deletes its `isPublic(r)` call entirely** and becomes unconditional session enforcement. *Both halves are required.* If an executor adds the two-list check to `Gate` and also leaves `SessionAuth`'s original predicate in place — which the word "verbatim" invited in v7 — then on menuserver an unauthenticated `GET /items` is correctly judged private by the Gate, handed down to `SessionAuth.Middleware`, and passed straight through by the surviving `!strings.HasPrefix(p, "/api/")` clause. That is C4 again, one layer lower, and only 4.9's own test would catch it. Additionally copy the handler logic from `reference/multissh/internal/server/auth_handlers.go` here rather than into `internal/multissh` (see 3.2), since under O-1 the login/logout/mode/ceremony routes are owned by the platform gate and shared by all six modules.

**The package must carry zero multissh-specific coupling** — no import of `internal/multissh`, no multissh-shaped config type, no assumption about which module it fronts. This is the property that makes it a platform capability rather than a lifted module package, and it is worth asserting in review: `go list -deps ./internal/platform/auth | grep multissh` must return nothing. *(v8 — state what this does and does not prove.* The package **will** import `internal/platform/config` for `FromConfig`, and `config` carries `MultisshConfig`, so the assertion passes on package paths while a multissh-shaped dependency exists one hop away. It also cannot see a string literal — `SessionCookieName = "multissh_session"` (`auth.go:30`) ports verbatim under P2 and the grep will never notice it. Keep the name for cookie-compatibility and record it here; the real work of keeping the package multissh-blind is done by `ModeExtras` (3.6b), not by this command.)*

**3.1b — Introduce an `ldap.Client` seam.** The reference binds directly against `go-ldap`. Extract a minimal interface at the bind boundary (`DialURL`, then `Bind`/`Search`/`Close` on the returned connection), with the `go-ldap` implementation as the default. This is what makes 3.8's tests possible without a live directory, and it is required new work — the untested reference code has no such seam today.

**3.1c — Define `auth.Gate` (v8 — new task; both reviewers found this seam undefined).** v7 described the Gate in three fragments — 3.6b gave it `ModeExtras`, 4.9 gave it two prefix lists and a mounting sentence, 3.6b gave its construction site — and **no task defined it**. It is the central new abstraction of both v7 blocker fixes, and two competent executors would have produced materially different types, one of them fail-open. Define it here, in full:

```go
type Gate struct {
    Provider        Provider           // NoAuth{} when this module is not protected
    Service         *Service           // nil when Provider is NoAuth{}
    CookieSecure    bool               // from cfg.Auth.CookieSecure (4.9)
    PublicPrefixes  []string           // 4.9
    PrivatePrefixes []string           // 4.9
    ModeExtras      map[string]any     // 3.6b; multissh passes {"maxSessions": N}, others nil
}

func (g *Gate) Wrap(next http.Handler) http.Handler
```

`Wrap` returns a handler that, in order:
1. Serves `GET /api/auth/mode` itself, **always**, whatever the provider is.
2. Serves the other eight `/api/auth/*` routes **only when `g.Service != nil`** — the ported bodies from `auth_handlers.go` (3.1). When `Service` is nil these paths are not registered at all and fall through to `next`.
3. For everything else: if `g.isPublic(r)` (the two-list predicate, 3.1/4.9), call `next`; otherwise delegate to `g.Provider.Middleware(next)`, which enforces the session and 401s.

**Step 2's guard is the point of this task.** It reproduces `reference/multissh/internal/server/server.go:97`'s `if s.authSvc != nil`, which is what makes FR-A3's "when auth is off, `GET /api/auth/mode` is the only auth route that exists" true. That guard currently lives in `server.go` — the file whose auth block 3.2 deletes — and **v7 carried it nowhere**, while 4.9 said the Gate "mounts the public login/logout/mode/ceremony routes" unconditionally. Read literally, a `NoAuth` Gate would mount `POST /api/auth/login` with a nil `*Service`, and `handleAuthLogin` (`auth_handlers.go:73`) dereferences it: an **unauthenticated 500 on all six modules under the default config**. 3.8 tests this branch directly.

Note on step 1 vs step 3 ordering: `/api/auth/` is the first clause of the predicate, so the mode route would be public even if step 1 ran last. Handling it first anyway removes the dependency on that ordering rather than relying on it.

`writeJSON` comes along from `auth_handlers.go`. `writeError` does **not** — it lives in `server.go:159`, which stays in `internal/multissh`, so duplicate the four-line helper into this package rather than exporting it across the seam.

> **Correction to v1 of this plan.** v1 claimed this package consumes `config.AuthConfig` and needed "a type swap". It does not. `internal/auth` is self-contained: it defines its own `auth.Config` (service.go:17), `auth.LDAPConfig` (in `ldap.go` — *v3: the earlier `ldap.go:35` cite was wrong, that line is `NewLDAPClient`; the struct is declared above it*) and `auth.PasskeyConfig`, and imports the reference `config` package **not at all**. The translation from config types to `auth.*Config` lives entirely in `reference/multissh/cmd/multissh/main.go:149` (`buildAuth`). Therefore: **port `internal/auth` verbatim**, and write the config → `auth.*Config` translation elsewhere. *(v6/M3: v5 left this pointing at `build.go` / task 4.3, which O-1 superseded. Under O-1 the translation is `auth.FromConfig(config.AuthConfig) (Provider, *Service, error)` inside `internal/platform/auth`, called once from `cmd/server/main.go` — **task 4.9**. `internal/multissh/build.go` takes no auth parameter at all.)*

**3.2** Copy `reference/multissh/internal/server/{server,hosts,files,sftp,upload,broadcast}.go` to `internal/multissh/` (package `multissh`) with import rewrites. **Do not copy `embed.go`** — static assets come from `static_dir` (FR-I4). **Do not copy `auth_handlers.go` here** — under O-1 its logic lands in `internal/platform/auth` (3.1), because the login flow is shared by all six modules rather than owned by multissh.

**Consequence for `server.go` — the exact removal set (v8; v7's description of this file was wrong in three ways and had been since v4).** `Handler()` is at **server.go:132-137**, not 127-133, it is four lines long, and it **mounts nothing** — it only applies `auth.Provider.Middleware`. The auth *routes* are mounted in `New()`. v7 also said `Options` loses "its auth-provider field", singular; it loses three. Delete, precisely:

| What | Where |
|---|---|
| `Options.Auth`, `Options.AuthSvc`, `Options.AuthCookieSecure` | `server.go:32-34` |
| `Server.auth`, `Server.authSvc`, `Server.authCookieSecure` | `server.go:46-48` |
| the three assignments from `opts` in `New()` | `server.go:93-95` |
| the `GET /api/auth/mode` registration **and** the whole `if s.authSvc != nil { … }` block registering the other eight | `server.go:96-107` |
| the `if s.auth != nil { return s.auth.Middleware(s.mux) }` branch | `server.go:133-135` |

leaving `func (s *Server) Handler() http.Handler { return s.mux }`. **Why this must be spelled out:** 3.1 takes `auth_handlers.go` to `internal/platform/auth`, so if 3.2 copies `server.go` verbatim as v7's wording allowed, `New()` retains nine references to methods that no longer exist in the package and **Phase 3 does not compile** — a direct violation of P4.

**Verified safe to remove:** `writeJSON`, `sessionCookieValue`, `passkeyInfoToResponse`, `writeAuthError`, `setSessionCookie` and `clearSessionCookie` are referenced **only** inside `auth_handlers.go` — nothing in `broadcast.go`, `files.go`, `hosts.go`, `sftp.go` or `upload.go` uses them — and **none** of the 18 `New(Options{…})` call sites across `files_test`, `hosts_test`, `sftp_test`, `broadcast_test` and `upload_test` sets `Auth`, `AuthSvc` or `AuthCookieSecure`. The ported suites stay green without edits. Note `writeError` (`server.go:159`) does **not** move — it stays in `internal/multissh` and is duplicated into `internal/platform/auth` (see 3.1c).

The upshot is unchanged: multissh has no way to serve unauthenticated by accident, because the mistake is no longer expressible in this package. Assert it — `go list -deps ./internal/multissh | grep platform/auth` returns nothing.

**3.3** Replace the embedded-FS static serving in `server.go` with a `static_dir`-backed handler: add `StaticDir string` to `Options`, drop `StaticFS fs.FS`, and preserve the reference's `onlyGet(noDirList(...))` wrapping for asset requests.

**Static fallback contract — implement exactly this, do not copy grocery's handler verbatim.** `internal/grocery/build.go`'s `staticHandler` serves `index.html` for *any* miss, which for multissh would turn a typo'd API path into a 200 HTML body and break client error handling. Required behavior:
- unmatched path beginning `/api/` → **404**, JSON error body, for every method. Never index.html.
- non-`/api/` `GET` (or `HEAD`) that matches no file on disk → serve `index.html` (SPA deep-link fallback, 200).
- non-`/api/` non-GET that matches no file → **405**.
- existing files → served under `onlyGet(noDirList(...))` as before; directory listings stay suppressed.

Add table-driven handler tests covering all four rows.

**`static_dir` missing or unreadable — decide once, here.** The reference logged a warning and served 404s (`DistFS()` "no built frontend embedded", main.go:92-95). **This plan does not carry that over.** `Build()` (4.2) **`os.Stat`s the resolved `static_dir` and returns an error** if it does not exist, is not a directory, or is not readable. Rationale: a warn-and-serve-404 module is indistinguishable at runtime from a routing bug, and the operator discovers it only by loading the UI; unified already declares fail-fast for the other Build-time precondition (`strict_host_key`, 4.7), and two different failure postures for two missing-file conditions in the same constructor is the kind of inconsistency nobody remembers. Blast radius is the same as 4.7's and is governed by the same 503-vs-`log.Fatalf` decision recorded there — under the adopted 503 handling, a missing `static_dir` degrades **multissh only**. Test: `Build()` with a nonexistent `static_dir` returns an error naming the path.

**3.4** Thread `MaxSessions` (FR-N1) through the backend, replacing every hard-coded 3:
- `internal/multissh/broadcast.go:210` — `len(req.Targets) < 1 || len(req.Targets) > 3` → `> s.maxSessions`. This half is a straight receiver-method edit: `handleBroadcastPost` is a method on `*Server` (broadcast.go:195), so `s.maxSessions` resolves. Update the error string at :211 too ("targets must contain 1 to %d entries").
- `internal/multissh/hosts.go:113` — **v3 correction: `s.maxSessions` does not compile here.** The guard at hosts.go:113 lives inside `func normalizeHosts(hosts []hostConfig) ([]hostConfig, error)` (hosts.go:112) — a **package-level function with no receiver**; there is no `s` in scope.

  **v4 correction: do not simply change that one signature.** A single `normalizeHosts([]hostRequest, int)` breaks both callers in different ways: the load path (`hosts.go:43`) decodes the on-disk file into the payload struct `struct{ Hosts []hostConfig }` (hosts.go:36-38), so a `[]hostRequest` parameter does not compile there — and the obvious workaround, decoding disk into `[]hostRequest`, would put a `Secret`-bearing type on the read-from-disk path and undo 2.3c outright. Threading `maxSessions` into the load path is also wrong on behavior: it turns an over-capacity hosts file into a hard `Build()` error, i.e. a 503 for the whole module under 4.7, contradicting the "accept and warn" contract in bullet 5 below and the tests that assert it. Required rewrite — **two functions, not one**:
  - **`normalizeHostRequests(hosts []hostRequest, maxSessions int) ([]hostConfig, error)`** — used **only** by the save/PUT path. Performs the field-by-field reconstruction of hosts.go:132-138 (which is what drops the password, per 2.3c) **and** carries the count guard: `len(hosts) > maxSessions` → error, with the error string at :114 phrased against the configured max (`"hosts must contain at most %d entries"`), replacing the literal `3` at hosts.go:113-114.
  - **`normalizeHostConfigs(hosts []hostConfig) ([]hostConfig, error)`** — used **only** by the load path (`newHostStore`, hosts.go:43). Identical field normalization (key-name validation, `remoteDir` default `/tmp`, `port` default `22`), **no count guard, no `maxSessions` parameter, and no `hostRequest` type in scope.** The on-disk payload struct stays `[]hostConfig` — do not change it.
  - **`hostStore.set` becomes `set(hosts []hostRequest) error`** (hosts.go:63) and calls `normalizeHostRequests`; the `PUT /api/hosts` handler decodes the request body into `[]hostRequest`. `hostStore` carries `maxSessions`, set at construction from `Options.MaxSessions`; `newHostStore` gains that parameter even though the load path does not use it, because `set` does.
  - update `hosts_test.go` to the two-function shape: tests exercising the count boundary call `normalizeHostRequests(..., 3)` with an explicit `3` (preserving the boundary the reference tests assert — the same explicit-`3` treatment 3.6 applies to the handler suites); tests exercising load-path/field normalization call `normalizeHostConfigs(...)`. Existing `set(...)` call sites in the tests take `[]hostRequest` literals.
- Add `MaxSessions int` to `Options` and the `Server` struct. **Do not re-validate** — `config.Load` (1.2b) is the single validation point and guarantees `1 <= MaxSessions <= 16`.
- Error strings must be phrased against the configured max ("at most %d targets"), per FR-U3.
- **Over-capacity `hosts_path`:** when the persisted hosts file contains more entries than `max_sessions` (operator lowered N after saving 5 hosts), **accept and warn** — load **all** entries and let the UI render only the first N cards. This is exactly why the load path calls the guard-free `normalizeHostConfigs` above: an over-capacity file must never become a `Build()` error. **Emission point, explicitly: in `newHostStore` (hosts.go:43), immediately after `normalizeHostConfigs` returns successfully**, compare `len(normalized)` against the store's `maxSessions` and, if greater, log once: `multissh: hosts file has %d entries, max_sessions is %d; extra entries preserved but not shown`. Chosen over truncation because truncation would silently destroy operator data on the next `PUT /api/hosts`. Keep the existing bullet-5 tests, aligned to this shape: an over-capacity file loads without error, all entries are present, and the file is not rewritten short.

  Spell the contract out so the `> maxSessions` guard above is not read as contradicting it: **the guard applies to the `PUT` request path only.** `GET /api/hosts` returns **all** persisted entries, unfiltered and untruncated, however many there are. The **client** renders only the first `maxSessions` cards (5.4). The server never truncates on read, and `PUT` never destroys the extra entries — a `PUT` carrying more than `maxSessions` entries is **rejected with 400 and the file is left untouched**, which is precisely what preserves the over-capacity data. Test all three: over-capacity `GET` returns every entry; over-capacity `PUT` 400s; the file's byte content is unchanged after that 400.

**3.5** Wire the password credential (2.3) through the handler surface:
- `POST /api/broadcast` targets accept `password` as an alternative to `key`. The line that changes is **`broadcast.go:224`**, whose current target validation hard-rejects an empty key (`strings.TrimSpace(t.Key) == ""` → 400 "invalid target"). Replace that clause with an **exactly-one-of** check: exactly one of `t.Key` / `t.Password` non-empty → accept; neither or both → 400. The `Host`/`User` emptiness checks on the same line stay as-is.
- the terminal WebSocket connect path accepts a password (the `clientMsg.Password` field from 2.3b), with the same exactly-one-of rule.
- **`PUT /api/hosts` password exclusion is structural, per 2.3c** — the persisted `hostConfig` has no password field, and **`normalizeHostRequests`** (the save-path half of the 3.4 split) is the filter: its field-by-field reconstruction (hosts.go:132-138) takes `[]hostRequest` in and returns `[]hostConfig` out. The load-path counterpart `normalizeHostConfigs` never sees a password type at all. There is no "strip" step to write and no caller contract to honor; the field cannot reach disk because it does not exist on the type that reaches disk (FR-H3, FR-N4). Verified by 2.4c.

**3.6** Copy the server test suites (`hosts_test`, `files_test`, `sftp_test`, `upload_test`, `broadcast_test`) with import rewrites; update the assertions that assume a hard 3 to construct the server with an explicit `MaxSessions: 3` so they keep testing the same boundary.

**3.6b — Expose `maxSessions` to the frontend (FR-N1 backend half). (v6 — rewritten; v5's version was unimplementable, C2.)**

*The defect.* v5 said to add `maxSessions` to the `GET /api/auth/mode` handler in `auth_handlers.go` — but O-1 moved that file into `internal/platform/auth`, a package task 3.1 forbids from carrying any multissh coupling (`go list -deps … | grep multissh` must be empty). A platform package cannot read `cfg.Multissh.MaxSessions`. As written, an executor reaching 3.6b has two mutually exclusive instructions and no way to satisfy both.

*The resolution — a per-module extras map.* `auth.Gate` takes a `ModeExtras map[string]any` at construction, supplied by the caller in `cmd/server/main.go` (4.9). **The Gate is constructed for all six modules unconditionally** — see the note below; this is what guarantees `ModeExtras` always has a carrier. The mode handler marshals `{"mode": …}` merged with those extras, **`mode` winning on collision** — an extras map containing a `mode` key cannot shadow the real answer. multissh's registration passes `{"maxSessions": cfg.Multissh.MaxSessions}`; the other five pass nil. The platform package stays multissh-blind — it never names the key — and the SPA's contract (`GET /api/auth/mode` → `{"mode":"none","maxSessions":3}`) is a **superset of the reference's on one key and a subset on another** — record it rather than claiming parity (P2). The reference emits `{"mode": …, "secureOptIn": mode != "none"}` (`auth_handlers.go:39`); this design adds `maxSessions` and **drops `secureOptIn`**. Verified harmless: `grep -rn secureOptIn reference/multissh/web/src/` returns nothing, and `fetchAuthMode` (`api.ts:181-185`) reads only `body.mode`. Dropping it rather than porting it is deliberate — a flag derived entirely from `mode` is redundant with `mode`. This is one map parameter, not a plugin system.

**`GET /api/auth/mode` is mounted unconditionally, for every module, protected or not (C1).** The reference mounts it outside its `if s.authSvc != nil` guard (`reference/multissh/internal/server/server.go:96`) precisely because the SPA calls it **before** any login exists. v5 folded the route into the auth gate, which is only attached to modules in `protected_modules` — so under the **default** config (`auth.mode: "none"`, empty list) the endpoint would not exist at all. That breaks three things at once: FR-A3, the SPA's `maxSessions` bootstrap (FR-N1) on a fresh install, and **Phase 4's own verify step, which curls it**. **The Gate wraps all six modules unconditionally; only the *provider* is conditional (v7 — this is the second blocker both reviewers found in v6).** v6 said the route is mounted for all six but left `ModeExtras` hanging off `auth.Gate`, while stating two sections later that no Gate wraps an unprotected module. Under the **default** config multissh is unprotected → no Gate → no carrier → `/api/auth/mode` returns `{"mode":"none"}` with **no `maxSessions`**, failing this task's own test *and* Phase 4's verify step *and* FR-N1's login-screen sizing on a fresh install. C1 was "the endpoint vanishes"; v6 restored the endpoint and left its payload bound to the conditional object — the same defect one field deeper.

*Resolution.* `cmd/server/main.go` wraps **every** module in an `auth.Gate`, constructed with the provider `FromConfig` returns for that module: the real provider when the module is protected, `NoAuth{}` when it is not. `PublicPrefixes`/`PrivatePrefixes` are evaluated on every request regardless of provider — a `NoAuth` provider passes everything anyway, so the lists are simply inert there. *(v8: v7 said they were "consulted only when the provider is not `NoAuth`", implying a branch the 4.9 pseudocode does not have and does not need.)* This gives one code path instead of two — which is exactly what pre-mortem scenario 1 asks for — and makes `ModeExtras` unconditionally available.

Test: with a default config (auth off, empty `protected_modules`), `GET /api/auth/mode` on a routed multissh hostname returns **200** and a body containing both `"mode":"none"` and the configured `maxSessions`.

**Do not add a new `GET /api/config`** — under `secure_mode: true` a new endpoint would sit behind the auth middleware and return 401 before login, so the SPA could not size its layout on the login screen. `/api/auth/mode` is already, by necessity, unauthenticated. All Go handler work for FR-N1 lands here in Phase 3; Phase 5 is frontend-only.

**3.7** Add tests for the new backend behavior: `max_sessions: 5` accepts a 5-target broadcast and rejects 6; and `PUT /api/hosts` round-trip proves a submitted password is absent from the file on disk (AC-10).

*(v8: the two `GET /api/auth/mode` assertions v7 listed here **move to 3.8**. Under 3.6b that route is served by the platform Gate, not by `internal/multissh` — an executor writing them here gets a failing test with no route to hit.)*

**3.8 — Cover the untested auth stack (O-1, O-2).** Reference `internal/auth` ships **zero** `_test.go` files, so the port inherits no coverage on the security-critical path — and under O-1 that path now fronts all six modules, not one. Tests land in `internal/platform/auth/` and use the 3.1b seam plus fakes; **no live directory and no hardware authenticator are required for anything in this list.**

`GET /api/auth/mode` (FR-A3, C1) — construct a `Gate{Provider: NoAuth{}, ModeExtras: {"maxSessions": 3}}` directly; **no `cmd/server/main.go` and no module handler are needed**:
- Auth off → `200` with a body containing `"mode":"none"` **and** `"maxSessions":3`, asserted by containment (extras merge, `mode` wins on collision).
- Provider is a real LDAP provider (protected module) → still **200 unauthenticated**, proving the route sits ahead of session enforcement.
- Provider is `NoAuth{}` → `POST /api/auth/login` is **not served by the gate** (it falls through to the wrapped handler), proving the `Service != nil` mounting guard of 3.1c. This is the test for the nil-`*Service` dereference described there.

Session and middleware (FR-A4):
- `SessionAuth` middleware: no cookie → 401 on a protected route; valid session cookie → passthrough; expired session (`SessionTTL`) → 401; idle-expired session (`IdleTTL`) → 401. Inject the clock rather than sleeping.
- Public-path exemptions (`middleware.go:46-55`) are honored: the login and mode routes stay reachable unauthenticated in every mode.
- Cookie flags: `HttpOnly` always set; `Secure` follows `AuthCookieSecure`. **`SameSite` is *not* configurable** — `setSessionCookie` hardcodes `http.SameSiteLaxMode` (`reference/multissh/internal/server/auth_handlers.go:239`). v5 said "as configured", which was wrong. Port the hardcoded `Lax` verbatim (P2) and assert it; adding a config knob is out of scope. Also note the cookie sets **no `Domain`**, which is what makes sessions host-scoped (see C3 in §0).
- Session invalidation on logout; a logged-out cookie is not replayable.

LDAP (FR-A4), against the 3.1b fake:
- Bind failure → 401, **no cookie set**; bind success → cookie set.
- `requiredGroups` mismatch → 401 even on a successful bind; match → pass.
- `NoAuth` provider → every request passes and `Mode()` reports `"none"`.

Passkeys (FR-A5) — **now real coverage, per O-2**:
- Registration- and assertion-option generation produces a well-formed challenge for the configured `rp_id`.
- Challenge storage is single-use (replay → rejected) and expires.
- Origin validation: an assertion presented from an origin outside `passkey.origins` → rejected.
- Credential store persistence round-trip (`store.go`): register, restart the store, still resolvable.
- Config validation: `mode: "ldap_passkey"` with empty `rp_id` or empty `origins` → **`config.Load` error (1.2c)**, not a `Build` error — under O-1 auth is constructed once in `main.go`, not per module. **This check is ours to write (M5):** `go-webauthn@v0.17.4` validates `RPID` only when it is non-empty (`webauthn/types.go:141`) and errors solely on empty `RPOrigins` (`:166`), so an empty `rp_id` passes its validation and fails later at ceremony time. v5 implied the library rejects it; it does not.

**Explicitly blocked until the lab has a directory and an authenticator** — write against the seam, `t.Skip` with the missing dependency named, and list both in the 6.2 docs:
- A real LDAP bind round-trip against a live directory.
- An end-to-end WebAuthn ceremony with a real authenticator over real HTTPS.

This is the honest boundary: everything that can be tested without hardware the operator does not yet own **is** tested; the two items that cannot be are named rather than quietly skipped.

**Verify Phase 3:** `go test -race ./internal/multissh/... ./internal/platform/auth/...` all green; `go build ./...`. *(v8: v7 named only `./internal/multissh/...`, so 3.8 — the entire new auth suite, written precisely because the reference ships zero auth tests — never ran at its own phase gate. P4 violation.)* Commit checkpoint on `dev`.

---

### Phase 4 — Module `Build()` + `main.go` wiring (FR-I1, FR-I2, FR-I5)

**4.1** Create `/opt/unified-webapp/internal/multissh/build.go` exposing `Build(cfg config.MultisshConfig) (http.Handler, error)`, mirroring `internal/grocery/build.go`'s shape.

> **`reference/multissh/cmd/multissh/main.go` is the specification for `Build()`, not dead weight.** v1 of this plan listed it under "delete". Its `run()` (main.go:**63-144**) and `buildAuth()` (main.go:**149-185**) contain exactly the wiring `Build()` must reproduce: `sshproxy.DefaultSSHDir()`, the `upload_dir`/`known_hosts` defaulting, `sshproxy.HostKeyCallback`, the `SFTPTransferrer` used as **both** `Transferrer` and `RemoteLister`, `sshproxy.NewHandler(SSHDialer{...}, sshDir)`, the 12-field `server.New(server.Options{...})` call, `EnablePasskeys`, and `SessionAuth{Service:…, ModeName:…}`. Treat `reference/multissh/cmd/` as **read-only source material through the end of Phase 4**. Deleting it is a late follow-up (see §4 Follow-ups), not a task here.

**4.2 — Port `run()`'s setup half.** Reproduce `reference/multissh/cmd/multissh/main.go:63-96` inside `Build` (the setup/wiring seam sits around :90-96; the `server.DistFS()` call and its warning are at **:92-95** and are dropped), substituting `config.MultisshConfig` for the reference `config.Config`: `ssh_dir` → `sshproxy.DefaultSSHDir()` when empty; `upload_dir` → `filepath.Join(os.TempDir(), "multissh-uploads")`; `browse_root` → resolved `upload_dir`; `known_hosts_path` → `filepath.Join(sshDir, "known_hosts")`. `MaxSessions` is taken as-is from config (already validated in 1.2b). `MkdirAll` the upload dir and `hosts_path`'s parent. **Drop** from the port: `config.Load`/`envOr`, `server.DistFS()` and its warning, `cfg.Addr`, the `http.Server`, the `ListenAndServe` goroutine, and all signal handling — `Build` returns a handler and starts nothing.

**4.3 — Port `run()`'s wiring half.** *(v6/M3: v5's title still said "plus `buildAuth`", contradicting the note below it — under O-1 `buildAuth` becomes `auth.FromConfig` and lands in 4.9. No auth work happens in this task.)* Reproduce main.go:96-144: `sshproxy.HostKeyCallback(cfg.StrictHostKey, knownHosts)` (fail closed — see the note below), `sftp := sshproxy.SFTPTransferrer{HostKeyCallback: hostKeyCB}` passed as both `Transferrer` and `RemoteLister`, `sshproxy.NewHandler(sshproxy.SSHDialer{HostKeyCallback: hostKeyCB}, sshDir)`, then `server.New(server.Options{...})` with `StaticDir` (3.3) and `MaxSessions` (3.4) replacing `StaticFS`. Return `srv.Handler()`.

**`buildAuth` does not land here (O-1 — changed from v4).** v4 put `buildAuth` (main.go:149-185) in `internal/multissh/build.go`. Under O-1 the auth provider is constructed **once** for the whole binary, so that translation moves to `internal/platform/auth.FromConfig(config.AuthConfig) (Provider, *Service, error)` and is called from `cmd/server/main.go` (4.9), not per module. **The signature returns three values, matching `cmd/multissh/main.go:149` exactly** *(v8 — v7 said `(Provider, error)`, which cannot work: eight of the nine handlers moving into the Gate dereference `s.authSvc` (`auth_handlers.go:48,53,73,92,104,126,149,165,182,204`), so the Gate needs the `*Service` and `FromConfig` is the only thing that builds it).* Preserve the reference gate exactly, generalized as the **full conjunction**: `NoAuth{}` unless `mode ∈ {"ldap","ldap_passkey"}` **and** the resolved protected set (`protected_modules` plus the `secure_mode` alias) is non-empty; `EnablePasskeys` only for `ldap_passkey`. *(v7: v6's generalization dropped the reference's `!cfg.SecureMode ||` half — `cmd/multissh/main.go:151` is a conjunction. Without it, `mode: "ldap_passkey"` with an empty protected set would still call `EnablePasskeys` and could error, contradicting 1.2c's "no module protected → no construction error" invariant.)* `multissh.Build()` therefore takes **no** auth parameter and returns a handler with **no** auth middleware.

**4.4** Wire into `/opt/unified-webapp/cmd/server/main.go`: add the `cmd184psu/unified-webapp/internal/multissh` import and `case "multissh": return multissh.Build(cfg.Multissh)` in `buildModule`.

**4.5 — Memoize `buildModule` per module name (shared-state bug).** `main.go` loops `for host, module := range cfg.Routing` and calls `buildModule(module, cfg)` **once per hostname**. Two `host_routing` entries pointing at `"multissh"` therefore construct **two independent `Server` instances** over the same `hosts_path` and `upload_dir`, with divergent in-memory state: the host store caches at construction, and the upload registry, broadcast job registry, and in-memory passwords are all per-instance. Symptoms: an upload staged via hostname A 404s when broadcast via hostname B; concurrent `PUT /api/hosts` from the two instances clobber each other's presets; a password entered on one is invisible to the other.

Fix in `cmd/server/main.go`: hold a `map[string]http.Handler` keyed by module name outside the routing loop; on a hit, register the cached handler for the additional hostname instead of rebuilding. This is a **general** dispatcher correctness fix — it applies to all six modules, and it is one of the two behavioral changes to shared code this work makes (the other is 4.6a; see the rollback note there) (permitted under §6.8, which lists `cmd/server/main.go`).

Test: a config with two `host_routing` entries mapping to `"multissh"` produces **one** `Build` call and one `hostStore`; a host written through hostname A is visible through hostname B without a restart.

**4.6 — Make the origin policy same-origin for all six modules (FR-S5, O-3).** `internal/platform/middleware.Wrap` sets `Access-Control-Allow-Origin: *` (plus `Allow-Methods: GET, POST, PATCH, DELETE, OPTIONS`) on the **whole dispatcher**. With multissh's default `secure_mode: false`, any web page the operator visits can cross-origin `fetch` `/api/ssh/keys` to enumerate private key names, `/api/files` to browse the sandbox, and `POST /api/broadcast` to execute commands on every configured host. The reference app's same-origin posture (FR-S5) is silently undone by the host application.

> **Two distinct problems, and the obvious fix only addresses one of them.** Removing the `Access-Control-Allow-Origin` header does **not** reject a cross-origin request. ACAO is a *response* header: the browser uses it to decide whether the attacker's page may **read** the reply. The request still executes server-side. And `POST /api/broadcast` qualifies as a CORS **"simple request"** — an attacker sends it with `Content-Type: text/plain`, which triggers **no preflight at all**, so there is nothing for a missing ACAO to block. `handleBroadcastPost` (`reference/multissh/internal/server/broadcast.go:195-213`) decodes the body with `json.NewDecoder(r.Body).Decode(&req)` and proceeds; it checks **neither `Origin` nor `Content-Type`**, and a JSON decoder does not care what the `Content-Type` claims. The commands run on every configured SSH host; the attacker merely does not get to read the output. That is not a mitigated attack.
>
> So the work splits in two: **header policy** (4.6a) and **server-side request rejection** (4.6b). 4.6b is the load-bearing half. **Under O-3 both live in `internal/platform/middleware` and apply to all six modules** — v4 scoped 4.6a per-module and put 4.6b inside `internal/multissh`, which left the wildcard on five modules and the real control where only one module could use it.

**4.6a — `Wrap` becomes same-origin, binary-wide.** Rewrite `internal/platform/middleware/cors.go`: drop the wildcard entirely; reflect `Access-Control-Allow-Origin` **only** when the request `Origin`'s host[:port] equals `r.Host`; always emit `Vary: Origin`; keep the `OPTIONS` 204 short-circuit but only for same-origin requests. `Wrap` stays applied once around the dispatcher — **no per-module wrapping**, so v4's `OPTIONS`-on-an-unrouted-host 204→404 side effect does not arise and its NFR-5 deviation entry is withdrawn.

Tests: same-origin request → ACAO reflected and `Vary: Origin` present; foreign origin → **no** ACAO header; absent `Origin` → unaffected; and a **regression test that all six modules still serve their own same-origin pages normally** under the new policy (this is the operator's explicitly requested regression test).

**4.6b — Reject cross-origin state-changing requests server-side, for all six modules.** In `internal/platform/middleware`, add a middleware covering **every non-`GET`/non-`HEAD` request, at any path**, on every module. **(v6 — M1: v5 scoped this to `/api/*`, which missed todo's entire write surface.** Per 0.2b, todo mutates via `POST /config/columns`, `/config/settings`, `/items/{subject}`, `/items/{subject}/{item}`, and `/items/{subject}/{item}/{newSubject}` — not one of them starts with `/api/`. A path-prefix condition here is a filter on multissh's URL style, not on danger. Dropping the prefix condition entirely is both simpler and correct: the method **is** the signal.)
- `Origin` present and its host[:port] ≠ `r.Host` → **403**, request body never decoded, module handler never reached.
- `Origin` absent → **permitted** (non-browser clients: `curl`, scripts, the acceptance pass in 6.4).

**Application point (v8 — v7 constrained this only relatively, "outside the auth gate", and never named it).** Fold it into the existing single `middleware.Wrap(dispatch)` call at `cmd/server/main.go:79`, alongside 4.6a. That is one application for the whole binary, before host dispatch, and it is necessarily outside every module's `Gate` — so a cross-origin write is 403'd before any credential is examined.
- `GET`/`HEAD` out of scope here — they are the safe methods, and gating them would break same-origin navigation edge cases for no gain. Cross-origin `GET /api/ssh/keys` remains *unreadable* to an attacker page because of 4.6a's withheld ACAO, which is the correct tool for that half.
- **No path condition.** Any method other than `GET`/`HEAD` is covered wherever it lands. There is no module in unified-webapp for which a cross-origin write is legitimate.

**One policy, one implementation, binary-wide.** This is the same rule the multissh WebSocket upgrader already enforces: `sameOrigin(r)` in `reference/multissh/internal/sshproxy/proxy.go:243-245` — empty `Origin` → `true`, else `originHost(origin) == r.Host`. **Lift that helper into `internal/platform/middleware`** and call it from both the upgrader and this middleware. Do **not** write a second origin comparison; two copies of a security predicate is how they drift apart.

Tests: `POST /api/broadcast` with `Origin: https://evil.example` and a `Host` matching a routed multissh hostname → **403**, and assert the broadcast did **not** execute (no job created); matching `Origin` → proceeds; **no** `Origin` → proceeds; a foreign-origin `OPTIONS` preflight is not granted.

**The platform-wide assertion names real routes (M4 — v5's version was vacuous).** v5 said "a write route on one of the other five modules", which a `/api/`-scoped implementation would have passed by hitting the catch-all. v6 pins it: a foreign-origin `POST` to **`todo`'s `/config/columns` and `/items/{subject}`** and to **`slideshow`'s `POST /api/control`** (`internal/slideshow/handler.go:35`) each return **403**, and the todo case additionally asserts the column list is **unchanged** afterwards — a status code alone does not prove the handler was not reached. These are named routes from 0.2b, so the test fails if the middleware is ever re-scoped to a path prefix.

**Deliberate extension beyond parity, and beyond FR-S5 as written.** FR-S5 states the same-origin posture for WebSockets only; the reference applies it only there, which is why the reference itself is vulnerable to the `text/plain` simple-request POST above. 4.6b hardens beyond a verbatim port, and O-3 extends that hardening to five modules the FRD says nothing about. Recorded in §3/R9 and the ADR so it is reviewed as an intentional scope addition, not mistaken for parity work.

**Accepted NFR-5 exception.** The other five modules' response headers **change** (wildcard ACAO withdrawn) and their write routes gain a cross-origin 403. Any cross-origin consumer of those modules would break; there are none today. This is the deliberate exception the operator chose, gated by 4.6a's all-six regression test.

**Rollback note (v4's is void).** v4 argued 4.6a was safely revertible because the load-bearing control lived in a separate module-local file. Under O-3 both halves sit in `internal/platform/middleware`, so that separation is gone. Revised: **4.6b must not ship reverted under any circumstances** — it is the sole mitigation for R9. **4.6a** may be reverted to the wildcard independently (it is a header-policy change and defense-in-depth for the read half) if a cross-origin consumer of the other five turns up, and doing so does not reopen R9. Reverting **both** reopens R9 fully.

Add `internal/platform/middleware` to the shared-files list in §6.8.

**4.7 — `strict_host_key` fail-closed, scoped to the failing module (v3: revised).** When `strict_host_key: true` and `known_hosts_path` is missing or unreadable, `Build` returns a wrapped error naming the path. Same for a missing `static_dir` (3.3).

v2 let `cmd/server/main.go`'s existing `log.Fatalf` handle that, so **the entire binary — all six modules — refused to start**, and AC-7 had to be reworded away from the FRD's own phrasing to match. **v3 adopts the narrower behavior:** in the routing loop, a `buildModule` error **no longer calls `log.Fatalf`**. Instead the failure is recorded in the **same `map[string]http.Handler` memoization table introduced by 4.5** — the module's entry is set to a handler that logs once at startup and returns **503** with a JSON body naming the module and the underlying error, for every request to that module's hostnames.

Why this is still fail-closed under P5: multissh serves **no** SSH functionality in this state — every route 503s, so there is no degradation to unverified host keys, which is the property P5 actually protects. What changes is only *who else* is punished. A `known_hosts` typo should not take grocery's shopping list offline; those modules have no relationship to multissh's SSH configuration, and coupling their availability to it is an accident of the dispatcher's error handling, not a security decision.

Consequences of adopting this:
- **AC-7 returns to the FRD's original wording** ("module fails to build") instead of the v2 rewording about whole-binary refusal. Update AC-7 in 6.4 accordingly.
- The startup log must still name the offending path unambiguously — it is now the *only* signal, since the binary comes up healthy-looking. Log it at `ERROR` prominence at boot **and** include it in each 503 body.
- The blast radius shrinks from six modules to one; the ADR consequence entry is rewritten to match.
- Cost: a misconfigured module now fails *quietly enough to miss* if nobody reads the boot log or visits the hostname. Mitigated by the 503 body carrying the reason, so the first person to load the page sees the cause rather than a blank error.
- This makes 4.5's memoization map do double duty (cache + failure record), which is why it is one map and not two.
- **In scope: unknown-module errors get the same treatment.** A `buildModule` returning `fmt.Errorf("unknown module %q", ...)` for an unrecognized module name in the config is recorded as a 503 handler on the same path — not `log.Fatalf` — with the same boot-log banner mitigation applying to it.

Test: `Build` failure for multissh → the other five hostnames respond normally, multissh hostnames return 503 with the path named in the body, and the process is still running.

**4.9 — Mount the platform auth gate in `cmd/server/main.go` (O-1).** Construct the provider once via `provider, svc, err := auth.FromConfig(cfg.Auth)`; for each module named in `cfg.Auth.ProtectedModules` (plus `"multissh"` implicitly when `multissh.secure_mode` is true), wrap that module's handler with a `Gate` (3.1c) at registration, in the same memoization map introduced by 4.5. `Gate.CookieSecure` comes from `cfg.Auth.CookieSecure`; `Gate.Service` is the `svc` returned above (nil for unprotected modules). Per 3.1c the gate serves `GET /api/auth/mode` always, the other eight `/api/auth/*` routes **only when `Service != nil`**, and 401s any non-public path without a valid session.

**If `err != nil`, every module in the resolved protected set is replaced by 4.7's 503 handler** — never a logged warning, never a fallback to `NoAuth{}`. This is the implementation of the fail-closed policy stated in **1.2c**; *(v8: v7 stated that policy in 1.2c, a Phase-1 task, but `FromConfig` is not called until here and 4.7's 503 mechanism is scoped to `buildModule` errors — so no task implemented it and 1.2c's own test had nothing to make it pass.)* Unprotected modules serve normally. **Every module is wrapped in a Gate, protected or not** (3.6b) — an unprotected module's Gate carries `NoAuth{}`, serves `GET /api/auth/mode`, and passes everything else straight through. *(v7: v6 said "unprotected modules register exactly as they do today", which was false the moment C1 added a route to all six, and which stranded `ModeExtras`.)* A module in 4.7's 503 state is **not** wrapped — the 503 handler replaces the module entirely, including its mode route; a module that failed to build has no auth surface to report on.

**The public-path predicate is per-module, not `/api/`-shaped (C4 — v6 fix).** The reference's gate (`reference/multissh/internal/auth/middleware.go:46-55`) decides what may bypass auth with:

```go
if strings.HasPrefix(p, "/api/auth/") { return true }
if !strings.HasPrefix(p, "/api/") && (r.Method == GET || r.Method == HEAD) { return true }
return false
```

That second clause reads "a non-`/api/` GET is a static asset". True for multissh, grocery and obsidianoid. **False for menuserver, todo and slideshow** — per 0.2b, every one of their data routes is a bare path, so the predicate would let `GET /items` and `GET /config` through unauthenticated on exactly the modules whose data lives there. Promoting a multissh-shaped predicate to a platform address is what O-1 quietly did, and it is the one place O-1 is genuinely dangerous.

*v6's fix was wrong, and this is the v7 replacement.* v6 specified a single **`PublicPaths []string`** allow-list and then told the executor to register `[]string{"/"}` **"minus `/api/`"** — a subtraction with no operator, no field, and no mechanism. Taken literally, prefix `/` matches everything, so `GET /api/hosts`, `/api/ssh/keys`, `/api/files` and `/api/broadcast/ws` all become public under `secure_mode: true` — the exact fail-open C4 existed to close, relocated onto the one module that actually ships a login page. It also contradicted this task's own test (401 on multissh's `/api/` routes) and FR-A3, which states the rule in **deny-list** terms (`multissh-FRD.md:74`).

The deeper reason a single allow-list cannot work: **every module in this repo is a catch-all-at-`/` SPA** (verified — `internal/{grocery,todo,slideshow,menuserver,obsidianoid}/build.go` each register `mux.Handle("/", …)`) with data routes living *inside* that prefix space. "Static bundle minus data routes" is a complement, and a complement is not an allow-list. v6's own enrolment guidance gave the tell: it read "everything except `/config`, `/items`, `/menus/`, `/audio/`".

*Fix — two prefix lists, private evaluated first.* The gate takes:

```go
type Gate struct {
    PublicPrefixes  []string // GET/HEAD here bypass auth …
    PrivatePrefixes []string // … unless the path also matches one of these
}
// isPublic(r):
//   strings.HasPrefix(p, "/api/auth/")                          -> true
//   matchAny(PrivatePrefixes, p)                                -> false
//   (r.Method == GET || r.Method == HEAD) && matchAny(Public, p)-> true
//   default                                                     -> false
```

Both lists empty (the zero value) still means **nothing is public** — deny-by-default is preserved without a policy type or a compile-time obligation. Registrations, derived directly from 0.2b's corrected table:

| Module | `PublicPrefixes` | `PrivatePrefixes` |
|---|---|---|
| `multissh`, `grocery`, `obsidianoid` | `["/"]` | `["/api/"]` |
| `menuserver` | `["/"]` | `["/config", "/items", "/menus/", "/api/"]` |
| `todo` | `["/"]` | `["/config", "/items", "/api/"]` |
| `slideshow` | `["/"]` | `["/config", "/items", "/audio/", "/api/", "/" + cfg.Slideshow.Prefix + "/"]` |

The multissh row reproduces `reference/multissh/internal/auth/middleware.go:46-55` **exactly**, including the SPA deep-link fallback task 3.3 requires (an unknown non-`/api/` GET stays public and reaches the `index.html` fallback). A single allow-list could not express that fallback at all — another reason v6's shape was unworkable.

**`slideshow`'s private set is not statically knowable.** Its image route is built from config — `internal/slideshow/handler.go:39` registers `"GET /"+h.cfg.Prefix+"/{subject}/{item}"` — so that prefix must be computed at registration from `cfg.Slideshow.Prefix`, not hardcoded. 0.2b's table notes this; do not copy a literal `/{prefix}/`. **Guard the empty case:** `"/" + "" + "/"` is `"//"`, and a `Prefix` of `""` would yield `"/"`, marking the entire slideshow SPA private. The default is `"slides"` (`internal/platform/config/config.go:117`) and slideshow is not gated in practice (M9), so this is a two-line `if cfg.Slideshow.Prefix != ""` guard, not a validation rule.

*Documented, not enforced.* No `Policy` type and no compile-time obligation to supply either list. The residual hazard is a **stale `PrivatePrefixes`** after someone adds a data route — that route would be public on a gated module. Accepted and recorded rather than engineered against: the lists live next to 0.2b's table, adding a route to an existing module is rare, and only multissh is gated in practice (see M9 in §0).

*Considered and deferred: move the lists into the module packages.* Each module could export `var DataPrefixes = []string{…}` next to its `Register`, and 4.9 could consume those instead of this table — removing the drift source rather than documenting it. It is genuinely attractive (five one-line declarations, no interface, no enforcement) and it is **not** adopted here, for two reasons. Slideshow's prefix is config-derived, so its export would have to be `func DataPrefixes(cfg) []string` while the other four are plain vars — the uniformity that makes the idea appealing does not survive contact with the one module that needs it most. And introducing a mechanism in the final consensus iteration, unreviewed, is precisely how the preceding three iterations generated their defects. Recorded as a follow-up in §4 instead.

Order relative to 4.6: the origin middleware (4.6b) runs **outside** the auth gate, so a cross-origin write is rejected 403 before any credential is examined.

**Tests — this is the task that catches O-1's new hazard (pre-mortem scenario 1):**
- Every module named in `protected_modules` returns **401** on an unauthenticated request to **a data route that module actually serves** (from 0.2b), not to a synthetic `/api/…` path. Assert this by iterating the configured list, **not** by naming multissh — a hand-written per-module test is exactly what fails to notice a module added later without a gate.
  - **Why the route table matters (C4/M4).** v5's version probed `/api/*` on every module. Every module registers a catch-all `mux.Handle("/", …)`, so on menuserver an unauthenticated `GET /api/anything` **does** get gated — the test goes green — while the real route `GET /items` sails through the fail-open predicate. The mitigation test passed on precisely the module the mitigation did not cover. The fix is to drive the test from 0.2b's table: menuserver asserts on `/items`, todo on `/items` and `POST /config/columns`, slideshow on `/items`, multissh/grocery/obsidianoid on their `/api/` routes.
- A module **not** in the list is unaffected and serves normally — **except** `GET /api/auth/mode`, which returns 200 on every module (3.6b/C1).
- **Startup warning (M9):** any module in `protected_modules` other than `multissh` logs `WARN: module %q is gated but has no login UI; its SPA will 401 after loading` at registration. Until the shared login page ships, gating those modules produces a broken app rather than a secured one.
- `multissh.secure_mode: true` + `auth.mode: "ldap"` → multissh 401s unauthenticated (AC-6, literal FRD wording preserved).
- `GET /api/auth/mode` returns 200 unauthenticated in every mode.

**4.8** Verify the WebSocket same-origin check (FR-S5, FR-I5) works behind the dispatcher: the dispatcher routes on `r.Host` and does not rewrite it, and `middleware.Wrap` does not wrap the `ResponseWriter` (so `http.Hijacker` survives the upgrade — confirmed in 0.2). Add a handler test asserting: matching Origin → accepted, absent Origin → accepted, foreign Origin → rejected, all with a `Host` header matching a routed multissh hostname.

**Verify Phase 4:** `make build`; `CGO_ENABLED=0 go build ./cmd/server` and `make build-rpi` (NFR-1); `go test -race ./internal/multissh/... ./cmd/... ./internal/platform/...`; start with a config containing `"multissh-test.cmdhome.net": "multissh"` and `curl -H 'Host: multissh-test.cmdhome.net' localhost:8080/api/auth/mode` → response **contains `"mode":"none"` and `"maxSessions":3`** (v3 fix: task 3.6b already added `maxSessions` in Phase 3, so the bare `{"mode":"none"}` this step previously expected can never match — assert on containment, not byte equality, so the check stays true if the payload gains further fields); the other five module hostnames still respond (AC-3, NFR-5). Commit checkpoint on `dev`.

---

### Phase 5 — Frontend port + redesign (FR-I4, FR-N1..N5)

**5.1** Add `@xterm/xterm` and `@xterm/addon-fit` to `dependencies` in `/opt/unified-webapp/package.json`; `npm install`; commit the lockfile change.

**5.2** Copy `reference/multissh/web/src/*.ts` **and `ssh.css`** into `/opt/unified-webapp/web/multissh/js/` (both — see the CSS decision in 5.3). **Only `web/src/*.ts` + `ssh.css` move.** Explicitly **not** copied: `reference/multissh/web/vite.config.ts` (the Vite toolchain is rejected per Option D / D2 — one build pipeline), and the reference's **checked-in `node_modules/`** (unified resolves `@xterm/*` through its own root `package.json` and lockfile per 5.1; vendoring a second copy would shadow it and defeat the lockfile). Verify after the copy that neither path exists under `web/multissh/`. Create `/opt/unified-webapp/web/multissh/index.html` from `reference/multissh/web/index.html`, replacing the Vite `/src/main.ts` module script with the esbuild output path and linking the built CSS.

**5.3** Add the bundled build to the root `package.json` scripts — append to both `build` and `build:dev`:
`esbuild web/multissh/js/main.ts --bundle --target=es2020 --outfile=web/multissh/js/bundle.js` (add `--sourcemap` for the dev variant).

**CSS strategy — single decision, no alternatives.** esbuild emits the bundled CSS; keep the `import "./ssh.css"` in `main.ts`. The "drop the CSS import and link stylesheets from `index.html` instead" alternative floated in v1 **is not viable**: `terminal.ts` imports `@xterm/xterm/css/xterm.css` from `node_modules`, which has no stable servable path under `web/`, so at least one CSS import must go through the bundler regardless — and splitting the two stylesheets across two mechanisms is strictly worse than routing both through one. Consequence: `--outfile=web/multissh/js/bundle.js` causes esbuild to emit `web/multissh/js/bundle.css` **alongside the JS, not under `css/`**. Reconcile this explicitly: `index.html` links `js/bundle.css` (not `css/bundle.css`), and the hand-copied `ssh.css` source lives at `web/multissh/js/ssh.css` next to the TS that imports it — there is no `web/multissh/css/` directory. Apply the identical output layout in **both** `build` and `build:dev` so a dev build never produces a differently-located stylesheet.

**Output format — verified, no flag needed.** `esbuild --bundle` with `--outfile` defaults to **IIFE**, which is only a problem if the entry needs ESM semantics. Checked: `reference/multissh/web/src/main.ts` has **no top-level `await`** — its only async work is inside `bootstrap()` (`void runAuthGate(...)`), so IIFE is correct. Therefore: **plain `<script src="js/bundle.js"></script>` in `index.html`, no `type="module"`, no `--format=esm`.** If a later edit introduces top-level `await`, esbuild fails the build with an explicit "Top-level await is currently not supported with the iife output format" error — at which point add `--format=esm` **and** `type="module"` together; they are a matched pair and changing one alone yields a silently non-executing page.

**Commit the built artifacts — yes, matching repo convention.** `git ls-files web/obsidianoid/js` shows **both** `app.ts`/`threads.ts` **and** the built `app.js`/`threads.js` are tracked. multissh follows suit: **commit `web/multissh/js/bundle.js` and `web/multissh/js/bundle.css`** alongside the sources. Consequence to accept knowingly: the bundle is a large generated blob (xterm is inlined) that will produce noisy diffs and can go stale relative to the `.ts` if someone edits sources without rerunning `npm run build`. Consistency with the existing five modules wins over diff hygiene here — a lone untracked module would break `make build`-free deploys that the other modules currently support. Rebuild and include the bundles in the Phase 5 commit checkpoint.

**5.3b — Extend typecheck coverage (`tsconfig.json` is an allowlist).** The root `tsconfig.json` `include` is an explicit list — `["web/obsidianoid/js/*.ts", "web/slideshow/js/*.ts"]` — with no wildcard over `web/`. Left alone, `npm run typecheck` would pass while never reading a single multissh file: a **false green**, and AC-2 would be satisfied vacuously. Imperative: **add `"web/multissh/js/*.ts"` to the `include` array.**

**First, though — the ambient CSS declaration, or this task fails on step one (v3 addition).** `tsc` has no notion of esbuild's CSS loader. The moment `web/multissh/js/*.ts` enters the `include` list, typecheck errors **TS2307 "Cannot find module"** on the two CSS imports the bundler handles fine: `main.ts:3` `import "./ssh.css"` and `terminal.ts:7` `import "@xterm/xterm/css/xterm.css"`. Create **`/opt/unified-webapp/web/multissh/js/css.d.ts`** containing `declare module "*.css";` (it is matched by the same `web/multissh/js/*.ts` glob — `.d.ts` files end in `.ts`). **Sequence this before the deliberate-type-error probe below**, otherwise the probe "fails" for the wrong reason and proves nothing about coverage.

Then prove the coverage rather than assuming it — introduce a deliberate type error into `web/multissh/js/hosts.ts` (e.g. assign a `number` to a `string`), run `npm run typecheck`, confirm it **fails and names that file**, then revert the error. Do not mark this task done on a passing typecheck alone.

**5.4 (FR-N1 — frontend only)** Replace the `HOST_COUNT = 3` constants in `hosts.ts:6` and `ui.ts:5` with the `maxSessions` value read from the existing `GET /api/auth/mode` bootstrap response (the server side of this shipped in task 3.6b — **no Go changes in this phase**). Make the host rail and terminal panel grid CSS wrap/stack so N=5..8 stays usable (FR-N1's layout clause); update the hard-coded "up to 3 hosts" copy at `hosts.ts:87` to interpolate N. **The real work is at the API layer, not the bootstrap (v8 — v7's premise here was false).** v7 claimed `hosts.ts:90` is module scope and prescribed deferring rail construction into an async bootstrap. Neither holds: `hosts.ts:90` is inside `mountHostRail()` (`hosts.ts:81-…`), and `runAuthGate` (`web/src/auth.ts:27-51`) **already** `await`s `fetchAuthMode()` at `:30` before calling `mountApp` at `:35`/`:47`/`:50` — so rail construction already happens after the fetch resolves. That work is done.

What actually blocks `maxSessions` is that `fetchAuthMode` (`api.ts:181-185`) is typed `Promise<string>` and returns `body.mode ?? "none"`, **discarding every other field of the response**. So: widen it to return the parsed object (or a `{mode, maxSessions}` pair), thread that value through `runAuthGate` → `bootApp` → `mountApp`, and have `mountHostRail`/`ui.ts` take N as a parameter instead of reading a module constant. No restructuring of the bootstrap is required.

**5.5 (FR-N2)** Add per-panel collapse/expand in `web/multissh/js/terminal.ts` + `ui.ts`. Collapsing hides the terminal viewport via CSS only — it must **not** close the WebSocket, dispose the xterm instance, or alter pause state. Keep the status badge on the collapsed header. On expand, call the fit addon to re-fit.

**5.6 (FR-N3)** Add blast-line history to the broadcast input in `ui.ts`: an in-memory array of sent lines, Up/Down arrow traversal with shell semantics (Up from the newest returns the most recent; Down past the newest restores the in-progress draft), no persistence, per browser session.

**5.7 (FR-N5)** Add an extensible `key:` keyword layer to the blast send path. Parse a leading `key:` prefix, look the remainder up in a keyword→bytes table (`ctrl+c` → `\x03`), and send the mapped bytes to every connected, non-paused panel instead of the literal line plus newline. Unknown `key:` names: send nothing and surface an inline hint rather than transmitting the raw text. Table structure must make `ctrl+d` a one-line addition later.

**5.8 (FR-N4)** Add per-host password entry to the host card in `hosts.ts`: an auth-method toggle (key | password), a `type="password"` input, `autocomplete="off"`. The password is sent as the `password` field of the **WebSocket `connect` JSON control frame** (see 2.3b — never as a query parameter, and never in the WebSocket URL) and in the broadcast target body; it is explicitly excluded from the object sent to `PUT /api/hosts`. Never write it to `localStorage`/`sessionStorage`.

**5.9 (FR-H5)** Reconcile the "Copy ssh command" affordance with password hosts: for a key host, emit `ssh -i <keypath> user@host -p port` as today. For a **password** host, emit `ssh user@host -p port` — **no `-i` flag, and never the password in any form** (not as a comment, not as a `sshpass` invocation). Add a hint next to the button that the password must be entered interactively. Test that the generated string for a password host contains neither `-i` nor the secret.

**Verify Phase 5:** `npm run build` && `npm run typecheck` (AC-2, with coverage proven per 5.3b) && `go test -race ./internal/multissh/...` (guards against accidental Go edits in a frontend phase); load the UI on the multissh hostname and confirm the terminal renders and the panel count equals `max_sessions`. Commit checkpoint on `dev`.

---

### Phase 6 — Tests, docs, and full verification (FR-I7, AC-1..10)

**6.1** Run the full suite and fix fallout: `go test -race ./...` must be green including all five pre-existing modules (NFR-5). Investigate any race report in ported concurrency code rather than serializing around it.

**6.1b (NFR-2)** Assert `Build()` starts **no background goroutines**. **Do not use a `runtime.NumGoroutine()` before/after comparison** (v3 correction): it is flaky under `-race` and parallel test execution — GC workers, the race detector's own goroutines, and any other package's `TestMain` can move the count in either direction between the two samples, so the assertion is simultaneously prone to false failures and, with "allow some settling" slack, blind to a single leaked goroutine. Pick one of:
- **Preferred:** `go.uber.org/goleak` — `defer goleak.VerifyNone(t)` around the `Build` call. It identifies goroutines by stack, not by count, so it names the leak instead of reporting a number. Add the dependency in **task 0.3** with the other go.mod work, not here.
- **If adding a dependency is unwanted:** a structural assertion instead of a runtime one — grep/AST-check that no `go ` statement is reachable from `Build` and its callees within `internal/multissh` construction paths. Weaker (it cannot see into library calls) but deterministic.

NFR-2 requires the module to be inert until a request arrives — the reference started a listener goroutine in `run()`, and 4.2 drops it; this test is what keeps it dropped.

**6.2** Port `reference/multissh/docs/USER_GUIDE.md` into unified's docs convention (e.g. `docs/multissh.md` or a README section), updated for: JSON-only config, no env vars, no TLS in-module, `max_sessions`, collapsible panels, blast history, `key:ctrl+c`, and password auth. Include explicit notes that: passwords are memory-only and vanish on restart; `strict_host_key: true` with a bad `known_hosts` (or a missing `static_dir`) makes **multissh alone** fail to build and serve **503 on every route** while the other five modules keep running (4.7) — so a healthy-looking process is not proof multissh came up; check the boot log or the 503 body; `ldap_passkey` is supported and requires the fronting proxy to terminate TLS so the browser origin matches `auth.passkey.origins` (O-2), with the end-to-end authenticator ceremony still untested (3.8); **auth is configured once at the top level and applies to whichever modules are listed in `auth.protected_modules`** (O-1), `multissh.secure_mode: true` being an alias for multissh's membership; **all six modules are same-origin — the wildcard CORS header is gone and cross-origin writes to any module are rejected 403** (4.6a/4.6b, O-3); and the proxy must preserve the `Host` header for WebSockets **and for the 4.6b middleware, which compares `Origin` against `r.Host`** (R1) — a `Host`-rewriting proxy now breaks HTTP writes as well as WebSocket upgrades, so this configuration note is load-bearing, not advisory.

**Also document the auth surface honestly (v7 — rewritten; this paragraph still described the `isPublic` predicate 4.9 replaced).** The gate **denies by default**: each module declares its public static prefixes and its private data prefixes (4.9), and anything not matching a public prefix requires a session. multissh's static bundle (`index.html`, `js/bundle.js`, `js/bundle.css`) stays readable before login because multissh **declares `/` public and `/api/` private** — not because non-`/api/` GETs are exempt as a general rule. That distinction matters for the other five modules, whose data lives at bare paths like `/items` and `/config`. State it plainly so nobody reads `secure_mode: true` as "nothing is served unauthenticated". No secrets are in the bundle; the host list and keys come from `/api/*`, which is gated.

**6.3** Confirm `unified-webapp-example.json` (1.5) matches the final field set and that `make init-config` output round-trips through `config.Load` unchanged.

**6.4** Execute the manual acceptance pass against a reachable test SSH host, recording pass/fail per criterion. **Capture the server log for the grep-based criteria** by starting the binary as `./unified-webapp -config /tmp/ac.json > /tmp/multissh-ac.log 2>&1` (the binary logs via the stdlib `log` package to stderr; both streams are redirected so nothing escapes the capture). AC-10's grep runs against `/tmp/multissh-ac.log` and the `hosts_path` file.
- **AC-4** — connect a terminal, type interactively, broadcast to 2+ connected hosts, pause one panel and confirm it ignores the broadcast while direct typing still works, press Ctrl-C.
- **AC-5** — upload a file, broadcast to 2+ targets with independent progress rows; re-broadcast a server-resident file via `filePath` without re-uploading; confirm 413 on oversize (partial file removed) and 400 on an out-of-sandbox path.
- **AC-6** — `secure_mode: false` → no login; `secure_mode: true` + `auth.mode: "ldap"` → unauthenticated `/api/*` returns 401 and the login page renders.
- **AC-7** (v3: restored to the FRD's original "module fails to build" wording, per 4.7) — `strict_host_key: true` with a missing `known_hosts` → **the multissh module fails to build**: every multissh hostname returns **503** with the missing `known_hosts` path named in the body, the boot log carries the same path clearly enough to diagnose unaided, and **the other five modules serve normally**. With a mismatched key → connect rejected.
- **AC-8** — `max_sessions: 5` → five host cards and five panels; a 5-target broadcast succeeds; collapse/expand does not drop sessions. **Run this pass once with `secure_mode: true`** to confirm the login screen sizes its layout correctly — i.e. that `maxSessions` reached the SPA via the unauthenticated `/api/auth/mode` (3.6b) rather than a 401'd endpoint.
- **AC-9** — Up-arrow recalls sent blast lines; `key:ctrl+c` interrupts every connected, non-paused terminal.
- **AC-10** — a password-auth host connects for both terminal and SFTP; `grep` the `hosts_path` file and the server log for the password → no hits; restart → password gone.
- **NFR-3** — switch browser tabs within the SPA and confirm live sessions survive.
- **NFR-4 (streaming uploads, no full-file buffering) — v3: this NFR had no verification path at all in v2.** Two-part check, structural first because it is the cheap and durable one:
  - **Structural (automated, keep in the suite):** assert the upload handler consumes the request via `r.MultipartReader()` streaming into `io.Copy` — the reference already does exactly this (`upload.go:94` `r.MultipartReader()`, `upload.go:129` `io.Copy(cw, part)`), so this is a **regression guard on a property the port already has**, not new work. Add a test asserting the handler **never calls `r.ParseMultipartForm`** (which buffers to memory up to its bound and spills the rest to temp files, defeating the NFR) — enforce by grep/AST check over `internal/multissh/upload.go`, since a behavioral assertion cannot easily distinguish the two.
  - **Manual (in this pass):** upload a file **substantially larger than any plausible memory bound** — at least 2 GiB, well above the 8 GiB `max_upload_bytes` default's midpoint and far above any buffer — while sampling the process RSS (`ps -o rss=` in a loop, or `/proc/<pid>/status`). Record peak RSS; assert it stays **bounded and roughly flat**, not proportional to file size. Record the observed peak in the results table so a future regression has a number to compare against.

**6.5** Confirm the per-phase commit checkpoints (taken at the end of Phases 1–5, each after that phase's verify passed) form a coherent history on `dev`; add a final commit for Phase 6's docs and fixes. **Never push, at any point.** Final check: `git log --oneline origin/dev..dev` shows the work as local-only and `git status` is clean.

**Verify Phase 6:** `make test` && `make build` && `make build-rpi` && `npm run build` && `npm run typecheck`, plus the recorded AC-1..10 results.

---

## 2A. Test & Observability Plan (deliberate mode)

| Layer | Coverage |
|---|---|
| **Unit** | Ported `sshproxy` suites; credential exactly-one-of and redaction across all three password-carrying structs; strict-host-key missing-file error path; config round-trip, defaults and `~` expansion; **platform auth** — session issue/validate, `SessionTTL` and `IdleTTL` expiry with an injected clock, cookie flags, logout invalidation, LDAP bind and `requiredGroups` against the 3.1b fake, passkey option generation, challenge single-use and expiry, origin validation, credential-store round-trip (3.8). |
| **Integration** | Ported server handler suites; `max_sessions` accept/reject at the configured bound; password absent from `hosts_path` (2.4c; the temp-file assertion v6 listed here is **dropped** — no task specifies it); `GET /api/auth/mode` shape and 200-unauthenticated in every mode **including the default config** (3.6b/C1); path-traversal rejection on key names, `filePath` broadcasts and the file browser; WS connect with a password credential; `static_dir` four-row fallback table; **4.6a** same-origin header policy plus the all-six regression test; **4.6b** cross-origin 403 with no job created on multissh, plus named-route assertions on **todo's `POST /config/columns` and `/items/{subject}`** (with the column list unchanged afterwards) and **slideshow's `POST /api/control`**; **4.9** 401 for every module in `protected_modules`, iterated from config; **4.5** two hostnames → one `Build`; **4.7** one module 503s while five serve. |
| **E2E** | Manual and labelled as such (6.4): AC-4..AC-10 plus NFR-2/3/4, executed **through the real nginx/HAProxy front end** rather than `localhost:8080` — a Go handler test with a synthetic `Host` cannot observe what the proxy does to `Host`, which is precisely R1. Frontend redesign items (collapse, history, `key:ctrl+c`, password entry, copy-ssh-command) are gated at Verify Phase 5, not deferred into the acceptance pass. |
| **Observability** | The reference logs a handful of failure-side lines via bare `log.Printf` and nothing in unified captures them — yet FR-A7 routes all client-facing error detail there and AC-10 greps it. Required: **(a)** audit lines on terminal connect/disconnect and on each broadcast target start/finish, carrying timestamp, principal (from the session when auth is on, else `-`), target host/user and outcome — a browser-driven SSH executor with no record of who reached which host is not operable; **(b)** a test asserting audit output contains **no** credential field, alongside the redaction tests; **(c)** a **named sink** — `unified.service` exists, so stdout lands in journald; document the exact `journalctl -u unified` invocation AC-10's grep depends on, without which AC-10 is not executable as written; **(d)** auth-gate decisions logged at boot (which modules are protected, in which mode) so scenario 1 is visible from one line; **(e)** per-risk detection signals — R1: successful WS upgrades at zero while origin rejections are non-zero; R9/R11: count of cross-origin 403s and of 401s per module; R4: the redaction tests plus the AC-10 grep. |

**Known coverage gaps, named rather than absorbed:** a live LDAP bind round-trip and an end-to-end WebAuthn ceremony with a real authenticator (both blocked on lab hardware, both `t.Skip`ped against the 3.1b seam with the dependency named, both listed in the 6.2 docs).

---

## 3. Risks & Mitigations

| # | Risk | Impact | Mitigation |
|---|------|--------|-----------|
| R1 | **WebSocket same-origin behind the proxy.** HAProxy/nginx may rewrite `Host`, breaking the reference's Origin-vs-Host equality check → all terminals fail to connect in production while passing locally. | High | **Task 4.8** (v3: v2 cited 4.5, which is the dispatcher memoization task — the Origin/Host test through the dispatcher is 4.8) tests the check through the dispatcher. Note the scope widened in v3: 4.6b now compares `Origin` against `r.Host` for HTTP writes too, so a `Host`-rewriting proxy breaks the REST API as well as WebSocket upgrades. Log the observed `Origin`/`Host` pair on rejection so a proxy misconfiguration is diagnosable from one log line. Document the required `Host`-preserving proxy config in 6.2. **(v7 — naming the cost O-3 actually carries.)** Before this work, no origin check exists anywhere in the binary, so `proxy_set_header Host $host` vs `$proxy_host` costs nothing today. After 4.6b it is load-bearing for **all six** modules: one wrong line in an nginx config the operator may not have touched in years turns every write in todo, grocery and the rest into a 403 — modules that work today, breaking as collateral from a module being added. R9 is a real risk *because multissh executes shell commands*; todo's worst cross-origin outcome is a scrambled column list. Scoping 4.6b to multissh alone would avoid the coupling but reopens R9 for the other five and directly reverses O-3, which the operator chose explicitly. **The coupling is accepted; it is recorded here as a consequence of O-3 rather than filed as a documentation task, because a proxy misconfiguration now has a five-module blast radius.** |
| R2 | **xterm bundling.** First `--bundle` module in the repo; CSS imports, ESM/CJS interop, and `tsc` include coverage can all break the root build script for the other modules. | Med | Add the multissh esbuild call as a **separate** invocation appended to the existing `build` script (5.3) so a failure cannot regress the bundleless modules. Verify `npm run build` rebuilds obsidianoid and slideshow correctly in the same run. |
| R3 | **N-session UI layout.** The reference grid assumes exactly 3 panels; at N=8 terminals may become unreadable slivers. | Med | FR-N1 only requires "usable" — use a wrapping flex/grid with a minimum panel height and vertical scroll, and lean on FR-N2 collapse as the pressure valve. Test at N=1, 3, and 8. |
| R4 | **In-memory password lifecycle.** A password leaking into `hosts_path`, a log line, an error string, or a `%+v` dump silently breaks AC-10 — and only a targeted test would catch it. | High | Redacting `String`/`GoString`/`MarshalJSON` on the credential type (2.3) makes leakage structurally hard rather than review-dependent. **v3: the persistence half is now structural, not a strip step** — the password field exists only on the `PUT /api/hosts` *request* DTO; the persisted `hostConfig` has no such field, and `normalizeHostRequests`' field-by-field reconstruction drops it by construction (2.3c, 3.4 — v4 splits the reference's `normalizeHosts` into a request-side and a config-side function so the load path never sees a `Secret`-bearing type). This also removes a v2 defect that would have *caused* the risk it meant to prevent: `Secret` is a struct, so `omitempty` is inert on it, and a shared DTO would have written `"password":"***"` to disk — which unmarshals back as a literal password. Type-level and file-level tests in 2.4c; explicit tests in 2.4 and 3.7; grep verification in 6.4. |
| R5 | **go.mod dependency merge.** LDAP + WebAuthn pull a large indirect tree (go-tpm, cbor, msgp); a version conflict or a CGO-requiring transitive dep would break `CGO_ENABLED=0` and the arm64 cross-build. | Med | Do the merge first (0.3) so a conflict surfaces before any porting effort is sunk. Verify `CGO_ENABLED=0 go build` and `make build-rpi` at both Phase 0 and Phase 4. |
| R6 | **Race-detector cleanliness of ported concurrency.** The broadcast registry, per-target transfer goroutines, and the WebSocket bridge are the highest-risk code; unified runs `-race` across the whole repo, so a latent race becomes a repo-wide red build. | Med | Port the tests before adapting the code (2.2, 3.6) so any race is attributable to the adaptation, not the port. Run `go test -race -count=5` on `./internal/multissh/...` at the end of Phases 2 and 3 to shake out flakes. |
| — | *(v7 note, not a risk)* **FR-I3 places `auth` inside `MultisshConfig`** (`multissh-FRD.md:33`); O-1 relocates it to top-level `Config.Auth`. Recorded as a deliberate deviation from FR-I3's literal field list, for the same reason O-1 exists: one auth surface for six modules. §5's FR-I3 row carries the same note. | — | Deviation accepted by operator decision O-1. |
| R7 | **Reference config shape drift.** Renaming the auth JSON tags would silently invalidate existing multissh operator configs. | Low | **v6: resolved to snake_case throughout** (see the note under 1.1). Existing operator configs are not loadable verbatim under *any* variant — every outer field is renamed because FR-I3 names the snake_case forms — so the choice costs nothing extra. v3–v5's carve-out keeping the inner `auth` tags camelCase is withdrawn: O-1 moved `auth` to top-level platform config, so it no longer mirrors the reference type. Migration is a documented one-time config rewrite (1.5, README). |
| R8 | **Per-hostname module duplication.** `buildModule` runs once per `host_routing` entry, so two hostnames for `"multissh"` yield two `Server`s sharing `hosts_path`/`upload_dir` with independent caches and registries → cross-host upload 404s, preset clobbering, invisible passwords. Silent until someone adds a second hostname, possibly long after ship. | High | Memoize by module name in `cmd/server/main.go` (4.5) plus a two-hostname/one-`hostStore` test. Fixing it in the dispatcher rather than in multissh also inoculates the other five modules. |
| R9 | **Host-app CORS undoes module security.** `middleware.Wrap` puts `Access-Control-Allow-Origin: *` on everything; with the default `secure_mode: false`, any page in the operator's browser can `POST /api/broadcast` to every configured SSH host. | High | **v3: mitigation corrected — removing the ACAO header does not mitigate this.** ACAO governs whether the attacker page may *read* the response; the request still executes. `POST /api/broadcast` is a CORS **simple request** when sent as `Content-Type: text/plain`, so it triggers **no preflight**, and `handleBroadcastPost` (broadcast.go:195-213) checks neither `Origin` nor `Content-Type` before decoding and executing. The commands run regardless of ACAO. **The mitigation is 4.6b**: server-side same-origin enforcement (403 on mismatched `Origin`, absent `Origin` permitted) over **every non-`GET`/`HEAD` request at any path on any module** — *(v7/M1: the `/api/*` scoping stated here through v6 was the exact gap M1 closed; todo's entire write surface is non-`/api/`)* — reusing the `sameOrigin` helper lifted from proxy.go:243-245 so WebSocket and HTTP share one policy. **v5/O-3: both 4.6a and 4.6b now live in `internal/platform/middleware` and apply to all six modules**, not multissh alone — v4 left the wildcard on the other five and put the real control where only one module could reach it. **4.6a** (wildcard withdrawn, ACAO reflected only same-origin) is defense-in-depth for the *read* half; it is **not** what closes this risk. This deliberately changes the other five modules' headers — an accepted NFR-5 exception (§6.8), gated by 4.6a's all-six regression test. |
| R10 | **Ported auth stack is untested upstream, and O-1 widens its blast radius.** `reference/multissh/internal/auth` has zero test files, and under O-1 it now stands between the network and **all six** modules, not one. | **High** (raised from Med by O-1) | 3.1b adds the `ldap.Client` seam; 3.8 covers session/TTL/idle/cookie/logout, LDAP bind and group filtering against fakes, and — per O-2 — passkey option generation, challenge single-use, origin validation and store persistence. 4.9 asserts every module in `protected_modules` 401s. Residual gap: live bind and real-authenticator ceremony, both `t.Skip`ped with the dependency named. |
| R11 | **A module is registered without the auth gate and is silently unprotected** (new, introduced by O-1). Replaces v4's `mux`-vs-`Handler()` hazard: multissh no longer owns its gate, so the failure mode moves to the dispatcher. Every test stays green. | High | 4.9's 401 assertion **iterates `protected_modules` from config** rather than naming modules by hand, so a module added to the list later without a gate fails the test. 3.2 removes **all three** auth fields (`Auth`, `AuthSvc`, `AuthCookieSecure`) from multissh's `Options`, making the module-local variant of the mistake inexpressible. Pre-mortem scenario 1 (§1A). |
| R12 | **O-1 scope risk.** Auth-as-platform is the largest single addition in the plan: a new config surface, a new shared package, deeper edits to `cmd/server/main.go` and `internal/platform/config`, and a login path in front of five modules that never had one. | Med | Default `protected_modules: []` means the existing five are untouched out of the box, so the risk is carried by *adoption*, not by *landing*. The v4 fallback (module-local `internal/multissh/auth` plus a later lift) remains a real, documented retreat if the schedule bites — declined, not unavailable. |

---

## 4. ADR — Port-and-adapt multissh as unified's sixth module

**Decision.** Port the reference Go packages into `internal/multissh/{,sshproxy,auth}` with mechanical import rewrites plus targeted adaptation (config source, static serving, `MaxSessions`, password credentials), and port the TypeScript SPA into `web/multissh/` built by root-level `esbuild --bundle`. Reject both a from-scratch Go rewrite and reuse of the prebuilt Vite `dist/` bundle.

**Drivers.** (D1) Ported tests are the acceptance gate and the only affordable correctness signal for the concurrency-heavy code. (D2) A single build pipeline — `make` + root esbuild — is a standing constraint of the monolith. (D3) The approved redesign is overwhelmingly frontend work, so Go effort should go to reuse, not reconstruction.

**Alternatives considered.**
- *Rewrite the Go module against unified idioms* — cleaner long-term fit, drops unused LDAP/WebAuthn weight, but discards the reference suites that FR-I7 and AC-1 explicitly require, and risks regressing PTY framing, origin checks, and partial-upload cleanup.
- *Ship the prebuilt Vite `dist/`* — zero build work, but minified and hash-named, so FR-N1..N5 are literally un-implementable against it, with no `tsc` coverage and a second toolchain implied for any future change.
- *Port everything except `auth`, hard-wiring `secure_mode: false` (Option A′)* — the lower-risk build on its own terms: it drops the go-ldap/go-webauthn/go-tpm subtree, shrinks the arm64 and `CGO_ENABLED=0` risk, and excludes the one reference package with no tests. Rejected solely because FR-A3..A5 are in-scope and AC-6 gates on an LDAP login producing a 401 and a login page, which A′ cannot deliver. Worth revisiting first if auth scope is ever renegotiated.
- *Retain the `MULTISSH_*` env layer* — familiar to existing operators, but no other unified module has an env layer; two config sources is a support burden for a single-operator deployment. FR-I3 permits dropping it.

**Why chosen.** Port-and-adapt is the only option that satisfies FR-I7 as written while keeping the diff reviewable and the schedule bounded. The frontend must be source-ported regardless because the redesign demands it, so the esbuild bundle is forced by FR-I4 rather than chosen freely. Together these hold the module inside every existing unified convention — one binary, one port, one config file, one build.

**Consequences.**
- unified's `go.mod` gains LDAP + WebAuthn transitively even though auth defaults to off; binary size grows and the dependency-update surface widens.
- unified now has two frontend build modes (bundleless and bundled), so the root `build` script needs a comment explaining why multissh differs.
- The reference's in-memory upload registry is inherited: staged files survive restart but their IDs do not (accepted, §6).
- `internal/multissh/` will carry structural traces of a standalone app — a wide `Options` struct, a `Handler()` indirection that no longer wraps anything — that a later refactor may want to align with unified's platform packages. *(v8: this bullet used to read "…and its own auth stack". Under O-1 it does not have one: 3.2 deletes all three auth fields from `Options`, all three from `Server`, and the route block from `New()`. Stale since O-1 was taken.)*
- **A failed module degrades to 503 instead of killing the binary** (v3, revised from v2). `buildModule` errors — a missing `known_hosts` under `strict_host_key: true`, a missing `static_dir` — no longer `log.Fatalf`. The module's entry in the 4.5 memoization map becomes a 503 handler naming the error, so multissh serves nothing while grocery, todo, obsidianoid and slideshow keep running (4.7). Still fail-closed under P5 — multissh provides zero SSH functionality in this state, so nothing degrades to unverified host keys; only the blast radius changed, from six modules to one. Cost: a misconfiguration is now quiet enough to overlook, so the boot log line and the 503 body must both name the offending path. This restores AC-7 to the FRD's original "module fails to build" wording.
- **The dispatcher gains a module-handler cache** (4.5). One handler now serves all hostnames mapped to a module, which is what shared on-disk state requires, but it means modules can no longer assume per-hostname isolation. No current module relies on that assumption.
- **CORS becomes same-origin binary-wide, not per-module** (4.6a, per **O-3**). *(v6 — M2: this bullet and the next still described the per-module architecture O-3 reversed; they were v4 carryover the v5 task sweep missed. Corrected here.)* The wildcard `Access-Control-Allow-Origin: *` is withdrawn from `middleware.Wrap` for **all six** modules; ACAO is reflected only for a same-origin request. `Wrap` stays applied **once** around the dispatcher, so v4's per-module wrapping and its `OPTIONS`-on-an-unrouted-host 204→404 side effect **do not arise** and that deviation is withdrawn. Any future module inherits the same-origin policy automatically rather than making a choice — which is the DRY property the operator asked for.
- **All six modules enforce same-origin server-side on state-changing HTTP requests** (4.6b, per **O-3**) — a deliberate **hardening beyond reference parity and beyond FR-S5 as written.** FR-S5 specifies the same-origin posture for WebSockets only, and the reference applies it only there; its HTTP API is consequently exposed to cross-origin `text/plain` simple-request POSTs that execute without preflight (see R9). This plan extends the identical policy — reusing the same `sameOrigin` helper so there is one implementation, not two — to **every non-`GET`/`HEAD` request at any path on any module** (v6/M1: the `/api/*` scoping is dropped; per 0.2b it missed todo's entire write surface). Consequence: the HTTP APIs of all six are now strictly stricter than the reference's, and any legitimate cross-origin client (there are none today) would break. Absent-`Origin` requests stay permitted, so `curl` and scripted use are unaffected. This is the one place the port deliberately diverges from "preserve working behavior" (P2) on security grounds, and under O-3 the divergence applies to five modules the FRD says nothing about.
- **FR-A5 (passkeys) ships as a supported option with real coverage** (O-2). v4's rationale for deferring it — "WebAuthn needs HTTPS and TLS is out of scope" — conflated in-binary TLS with the deployment's TLS-terminating proxy, which supplies exactly the HTTPS origin `rp_id`/`origins` expect. Option generation, challenge single-use and expiry, origin validation, credential persistence and config validation are all tested (3.8). The single remaining gap is an end-to-end ceremony with a real authenticator, blocked on hardware the operator does not yet have, and `t.Skip`ped with that dependency named.
- **Auth is a platform capability serving all six modules** (O-1), not a multissh-local package. One LDAP integration, one session implementation, one login flow. Cost: a new config surface, a new shared package with its own test suite, and deeper edits to `cmd/server/main.go` and `internal/platform/config` than a module-local port would need. Benefit: the operator's stated end state reached directly rather than via port-then-lift.
- **`GET /api/auth/mode`'s payload is not byte-identical to the reference** (P2 deviation, v8). It gains `maxSessions` (required by FR-N1) and drops `secureOptIn` (`auth_handlers.go:39`), a flag derived entirely from `mode` and read by nothing in the reference SPA. Verified unreferenced before dropping.
- `reference/multissh/` — including `cmd/multissh/`, which is the specification for `Build()` — is retained **read-only** through Phase 4 and removed only in a follow-up after Phase 6 passes.

**Follow-ups (not in this plan).**
- ~~Lift multissh's `auth` package into `internal/platform/auth`.~~ **Done in this plan** — promoted from a follow-up to task 3.1 by operator decision O-1.
- Add a shared login page for the five non-multissh modules (the gate works without one; only multissh ships login UI today).
- Consider a configurable `allowed_origins` list if a legitimate cross-origin consumer of any module ever appears (4.6a).
- Extend `key:` keywords beyond `ctrl+c` (`ctrl+d`, `ctrl+z`) once the mechanism proves out.
- Persist the upload registry so IDs survive restart.
- **Move each module's data-route prefixes into its own package** (`var DataPrefixes` next to `Register`) so 4.9's table stops being a second copy of information that lives in the handlers. See the deferral note in 4.9 for why this is a follow-up and not a task: slideshow's prefix is config-derived and would need a function rather than a var, and the change arrived too late in consensus to be reviewed. This is the standing fix for the drift that produced defects in three consecutive iterations.
- Delete `reference/multissh/` (including `cmd/multissh/`, kept read-only as the `Build()` specification) once Phase 6 has passed and the module has run in production.
- Complete the two blocked auth tests once the lab has an LDAP directory and a real authenticator: a live bind round-trip and an end-to-end WebAuthn ceremony (3.8). Everything else in the auth stack is covered now.
- **A shared login page**, so `protected_modules` becomes usable for modules other than multissh (M9). Until then the gate returns 401 JSON with no way to authenticate from the other five SPAs.

### Recorded antithesis — O-1's sequencing (v7)

Both reviewers, independently, spent the majority of three iterations' findings on O-1's downstream consequences. That is data about where the complexity sits, and it deserves recording even though the decision stands.

*The argument.* Five of the six defects v6 had to fix exist **only** because auth was generalized to a platform capability before a second consumer existed: C1 (the mode route vanishing), C2 (`maxSessions` unable to cross the platform boundary), C4 (multissh's URL predicate promoted to a platform address), M9 (five modules gateable but with no login UI), R11 (a silently unprotected module). None arise under v4's `internal/multissh/auth` placement. O-1 buys a generalization that, per M9, **cannot be exercised today**, and pays with a new top-level config surface, a shared package between the network and all six modules, R10 raised Med→High, a `map[string]any` whose only job is to smuggle one module's config through a package forbidden to know it exists, and a per-module path policy that took two iterations to get into an expressible shape. The v4 fallback would deliver **identical operator-visible behavior** — multissh gated, five modules untouched — and the lift is *cheaper later*, because by then you would extract an interface from two real implementations instead of guessing one from zero.

*Why the decision stands anyway.* The operator asked for it in plain words ("DRY all the way"), `internal/platform/auth` is where this code ends up regardless, and migrating a security-critical package once beats migrating it twice. The antithesis is about **sequencing, not destination** — which is why it is recorded rather than adopted. If schedule pressure ever appears, this is the first thing to reconsider, and §0 already names the fallback as real.

*What was adopted from it.* The two-list gate now expresses every module's contract correctly (4.9), so the generalization is at least **sound** even while only multissh exercises it. Enrolling a second module is a frontend task, not another backend lift.

### Process note — carryover is this document's dominant failure mode

Three consecutive iterations have had "a task section was fixed; §3/§4/§5/§6 still describe the superseded design" as their largest finding class. v6 swept the ADR successfully and missed R9, §6.2 and both NFR-5 lists. **Before any future revision is submitted for review, grep the whole document for the phrases the revision changed** — for this round: `FromConfig`, `isPublic`, `Gate`, `auth-provider field`, `Verify Phase`, `secureOptIn`, `hosts.go:131` — and diff every task-section claim against its restatement in §3, §4, §5 and §6.8. Each task edit should carry the list of downstream sections it invalidates.

**v8 adds a second rule, because grepping was not sufficient.** The v7 sweep was clean by its own standard and still shipped a task (3.2) that had mis-described `server.go` since **v4** — wrong line numbers, wrong count of fields, and a claim that `Handler()` mounts routes it does not mount. It survived four review rounds because a phrase-grep only looks at text the *revision* touched, and nobody re-read the file. So: **when a task cites specific lines of a reference file, re-read those lines against source in the revision that touches the task — do not carry the citation forward on trust.** The plan holds prose copies of things that live in source, and prose copies drift silently.

---

## 5. Traceability — every FRD item to its task(s)

Added in v3 so coverage is checkable rather than asserted. "**parity**" = verbatim port plus the reference's own ported suite; the port task and the test-port task together are the coverage, and no new behavior is introduced. Every row must have at least one task ID and at least one verification.

### Integration (FR-I)

| Req | Tasks | Verified by |
|---|---|---|
| FR-I1 Module registration | 4.4, 4.5 | Phase 4 verify (curl on routed hostname); 4.5 two-hostname test |
| FR-I2 Package layout | 2.1, 3.1, 3.2 | `go build ./...`; Phase 2/3 verify |
| FR-I3 Config section | 1.1, 1.2, 1.2b, 1.3, 1.5, 1.6 | 1.4 config tests; Phase 1 round-trip verify; 6.3 |
| FR-I4 Frontend via esbuild | 5.1, 5.2, 5.3, 5.3b | `npm run build` + `npm run typecheck` (coverage proven per 5.3b probe) |
| FR-I5 Routing under one origin | 4.4, 4.8, 4.6b | 4.8 Origin/Host dispatcher test; 4.6b 403 test |
| FR-I6 Dependencies, CGO_ENABLED=0 | 0.3 | `CGO_ENABLED=0 go build ./...`; `make build-rpi` at Phase 0 and 4 |
| FR-I7 Ported test suites | 2.2, 3.6, 1.4 | **AC-1** — `go test -race ./...` (6.1) |
| FR-I8 No push | 6.5 | `git log --oneline origin/dev..dev`; `git status` clean |

### Hosts (FR-H)

| Req | Tasks | Verified by |
|---|---|---|
| FR-H1 Host rail, ≤`max_sessions` cards | 3.4, 3.6b, 5.4, 5.8 | AC-8 (five cards at `max_sessions: 5`) |
| FR-H2 `GET /api/ssh/keys` | **parity** (3.2 + 3.6) | ported `hosts_test`/handler suite |
| FR-H3 Presets, never key material or passwords | 2.3c, 3.5, 3.4 | 2.4c type-level + file-level tests; 3.7; **AC-10** grep (6.4) |
| FR-H4 SFTP listdir picker | **parity** (3.2 + 3.6 `sftp_test`) | ported suite |
| FR-H5 Copy ssh command | 5.9 | 5.9 test (password host: no `-i`, no secret) |

### Sessions (FR-S)

| Req | Tasks | Verified by |
|---|---|---|
| FR-S1 N terminals, own WebSocket each | 2.1, 3.4, 5.4 | ported `proxy_test`/`session_test`; AC-8 |
| FR-S2 Status / pause / Ctrl-C | **parity** (3.2) + 5.5 (collapse must not alter pause) | **AC-4** |
| FR-S3 Broadcast to connected, non-paused | **parity** (3.2) | AC-4 |
| FR-S4 Generic connect failure text | **parity** (2.1 + 2.2) | ported `proxy_test` |
| FR-S5 Same-origin WebSockets | **parity** (2.1) + 4.8; **extended** by 4.6b to HTTP writes | 4.8 (match/absent/foreign Origin); 4.6b 403 test |

### Uploads & broadcast (FR-U)

| Req | Tasks | Verified by |
|---|---|---|
| FR-U1 Streaming upload, 413 + partial removal | **parity** (3.2 + 3.6 `upload_test`) | ported suite; **AC-5**; NFR-4 checks (6.4) |
| FR-U2 `browse_root` sandbox | **parity** (3.2 + 3.6 `files_test`) | ported suite; AC-5 (400 on escape) |
| FR-U3 1–N targets, limits phrased against max | 3.4, 3.5 | 3.7 (`max_sessions: 5` accepts 5, rejects 6); AC-5 |
| FR-U4 Parallel per-target SFTP, isolated failures | **parity** (2.1 + 2.2 `transfer_test`) | ported suite; AC-5 |
| FR-U5 Progress WebSocket | **parity** (3.2 + 3.6 `broadcast_test`) | ported suite; AC-5 |

### Redesign (FR-N)

| Req | Tasks | Verified by |
|---|---|---|
| FR-N1 `max_sessions` | 1.2, 1.2b, 3.4, 3.6b, 5.4 | 3.7; **AC-8** (incl. the `secure_mode: true` login-screen sizing pass); R3 layout test at N=1/3/8 |
| FR-N2 Collapsible panels | 5.5 | AC-8 (collapse/expand drops no session) |
| FR-N3 Blast history | 5.6 | **AC-9** |
| FR-N4 In-memory password auth | 2.3, 2.3b, 2.3c, 2.4, 2.4b, 2.4c, 3.5, 5.8 | **AC-10** (connect, grep, restart); 2.4b lifetime bound |
| FR-N5 `key:` keywords | 5.7 | AC-9 (`key:ctrl+c` interrupts all non-paused) |

### Auth & security (FR-A)

| Req | Tasks | Verified by |
|---|---|---|
| FR-A1 `strict_host_key` fail closed | 2.5, 4.3, 4.7 | 2.5 missing-file error path; **AC-7** (503, other modules up) — note FR-A1's own wording is "missing/unreadable file = **module** build error", which 4.7's v3 revision now matches literally |
| FR-A2 Encrypted keys unsupported | 2.5 (**parity** 2.1) | ported `keys_test` |
| FR-A3 `secure_mode` gates data routes, `/api/auth/*` exempt | **4.9** (gate + `PublicPrefixes`/`PrivatePrefixes`, deny-by-default), **3.1c** (Gate type + the `Service != nil` mounting guard that makes "only `/api/auth/mode` exists when auth is off" true), 3.1 (predicate edit), 3.6b, 1.2c, 3.8 | 3.7 (`/api/auth/mode` 200 unauthenticated under ldap); **AC-6**. *Deviation:* FR-A3 says the response is `{"mode":"none"}`; 3.6b returns a **superset** `{"mode":"none","maxSessions":N}` — additive, required by FR-N1's login-screen sizing, and asserted by containment (Phase 4 verify) |
| FR-A4 LDAP login, groups, TTLs, cookie flags | 3.1, 3.8, **4.9** | 3.8 (middleware/login/TTL/group tests with fakes); AC-6 |
| FR-A5 Passkeys | 3.1, 3.8, 1.2c, 4.9 (`EnablePasskeys`) | **Supported option with real coverage (O-2):** option generation, challenge single-use + expiry, origin validation, credential-store round-trip, config validation. Residual gap: end-to-end ceremony with a real authenticator, `t.Skip`ped with the dependency named |
| FR-A6 Path traversal blocked | **parity** (2.1, 3.2) | ported `keys_test`, `files_test`, `upload_test`; AC-5 |
| FR-A7 Generic client errors | **parity** (2.1, 3.2) + 3.3 (JSON 404 on `/api/*` miss) | ported suites; 3.3 four-row table test |

### Non-functional (NFR)

| Req | Tasks | Verified by |
|---|---|---|
| NFR-1 One binary, one build, arm64 + `CGO_ENABLED=0` | 0.3, 5.3 | `make build`, `make build-rpi`, `CGO_ENABLED=0 go build` (Phase 4 + 6 verify) |
| NFR-2 Inert until first request | 4.2 (drops listener/signal handling) | **6.1b** (goleak, per v3 — not a `NumGoroutine` diff) |
| NFR-3 Sessions survive tab switches | **parity** (frontend) | 6.4 NFR-3 manual check |
| NFR-4 Streaming uploads, no full-file buffering | **parity** (3.2 — `r.MultipartReader()` + `io.Copy`, upload.go:94/129) | **6.4 NFR-4** (v3 addition): structural no-`ParseMultipartForm` assertion + 2 GiB bounded-RSS manual check |
| NFR-5 Existing five modules unchanged | 4.5, 4.6a, 5.3 (separate esbuild call) | 6.1 full `go test -race ./...`; Phase 4 verify (other five hostnames respond). *Accepted deviations (v6):* wildcard ACAO withdrawn from all six and cross-origin writes 403'd (4.6a/4.6b, per O-3) — the substantial one; modules no longer per-hostname isolated (4.5); a `buildModule` failure now 503s that module instead of `log.Fatalf`-ing the process (4.7 — strictly better for the other five); **(v7) all six modules gain `GET /api/auth/mode`** (3.6b/C1), returning `{"mode":"none"}` on unprotected ones — additive, unauthenticated, read-only. **(v8) One exception:** a module in 4.7's 503 state has no mode route either — the 503 handler replaces the module entirely, and a module that failed to build has no auth surface to report on (4.9). *(v4's `OPTIONS`-to-an-unrouted-host 204→404 deviation is **withdrawn** — it followed from per-module `Wrap`, which O-3 does not do.)* |

### Acceptance criteria

| AC | Tasks | Where run |
|---|---|---|
| AC-1 tests green | all port tasks | 6.1 |
| AC-2 typecheck green | 5.3b (+ css.d.ts) | Phase 5 verify |
| AC-3 other modules unaffected | 4.5, 4.6a | Phase 4 verify, 6.1 |
| AC-4 terminals/broadcast/pause/Ctrl-C | parity + 5.5 | 6.4 |
| AC-5 upload/broadcast/413/sandbox | parity | 6.4 |
| AC-6 secure_mode + LDAP | 3.8, 4.9 | 6.4 |
| AC-7 strict_host_key build failure | 4.7 | 6.4 |
| AC-8 `max_sessions: 5` | 3.4, 3.6b, 5.4 | 6.4 |
| AC-9 history + `key:ctrl+c` | 5.6, 5.7 | 6.4 |
| AC-10 password never persisted | 2.3c, 2.4c, 3.5 | 6.4 (grep) + 2.4c (stronger: no `"password"` key at all) |

---

## 6. Out of Scope

Explicitly **not** part of this work:

1. **TLS inside the module or the binary.** nginx/HAProxy terminates TLS in front of unified-webapp. No cert handling, no HTTPS listener changes, no ACME.
2. **The `MULTISSH_*` environment-variable layer.** JSON config only (FR-I3). Env parsing, `boolEnv`, `splitCSV`, and `MULTISSH_CONFIG` are dropped, along with their tests.
3. **`scp` transfers.** SFTP only (§5).
4. **Encrypted / passphrase-protected private keys.** Unsupported; log-and-generic-fail only (FR-A2).
5. **Persisting passwords anywhere.** No password storage, no credential vault, no "remember me" (FR-N4).
6. **Go `embed` of frontend assets.** Static files come from `static_dir` like every other module (FR-I4).
7. **`key:` keywords beyond `ctrl+c`.** The mechanism must be extensible; only `ctrl+c` is required (FR-N5).
8. **Changes to the existing five modules.** Behavior must be untouched (NFR-5); only `cmd/server/main.go` (module-handler memoization 4.5, auth gate 4.9), `internal/platform/config` (top-level `auth` section, O-1), `internal/platform/middleware` (same-origin policy for all six, 4.6a/4.6b), `internal/platform/auth` (new package, O-1), `package.json`, `tsconfig.json` (include list, 5.3b), `README.md`, and `unified-webapp-example.json` are shared files this work may modify — and each such change must leave the other five modules' observable behavior identical, **with four reviewed and accepted exceptions** (all recorded against NFR-5 in §5), of which O-3 makes the first one substantial:
   - **Wildcard CORS is withdrawn from all six and cross-origin writes are 403'd** (4.6a/4.6b, per O-3). This changes the other five modules' response headers and rejects cross-origin writes to them. No such consumer exists today; the operator chose this deliberately for DRY reasons. Gated by 4.6a's all-six regression test. *(v4's separate `OPTIONS` 204→404 deviation is withdrawn — it came from per-module `Wrap`, which O-3 does not do.)*
   - `buildModule` failures now yield a 503 module handler instead of `log.Fatalf` (4.7) — this *improves* the other five modules' availability rather than degrading it.
   - Modules listed in `auth.protected_modules` gain a login gate (4.9, per O-1). Modules not listed are otherwise untouched; the default list is empty.
   - **(v7) All six modules serve `GET /api/auth/mode` unauthenticated** (3.6b/C1) — on the five non-multissh modules this is a **new route** returning `{"mode":"none"}`. Additive, read-only, no secrets. This is the one thing that does change out of the box for the existing five, and it is recorded here because C1's fix introduced it and v6 did not propagate it.
9. **Multi-directory LDAP, SSO beyond LDAP/passkey, or RBAC.** Single directory with optional `requiredGroups` only (§5).
10. **Pushing to any remote.** Commits on `dev` only (FR-I8).
11. **Persisting blast history, session layout, or collapse state across browser sessions.** In-memory only (FR-N3).
12. **Performance/scale work beyond NFR-2 and NFR-4.** No connection pooling, no transfer throttling, no resumable uploads.
