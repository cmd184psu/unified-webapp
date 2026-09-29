# FRD — Platform security & auth (round two)

**Status:** Draft for planning (ralplan input)
**Date:** 2026-09-10
**Sources:** `shelved/plan-with-platform-auth.md` (v8 design — reuse, don't regenerate), `docs/SECURITY-NOTES-deferred.md` (S-1…S-8), this session's fresh sweep (N-1…N-5), `reference/multissh/internal/auth` (LDAP + WebAuthn code to port).

---

## 1. Goal

Round two of unified-webapp: add **optional per-module authentication** (LDAP[S], PIN with JWT-style stateless sessions, WebAuthn passkeys, API keys), a **seventh module — `admin`** — that manages auth configuration live, **same-origin posture** for all modules, and the **mechanical hardening** deferred from prior rounds — while consolidating the platform code the modules currently copy-paste, so the next module (more are coming) inherits all of it for free.

**Deployment context this FRD is calibrated to:** the binary listens on a port on 127.0.0.1 behind nginx or HAProxy, which terminates TLS and preserves `Host`. Home LAN, single operator, not a product. Controls are enabled-by-default but configurable-off; nothing is enforced so strictly that a proxy quirk bricks a module. The backend never trusts the frontend for any security decision.

**Baseline already done (this session, keep as part of the branch):** Go toolchain and `go.mod` at 1.26.2 (latest); deps updated (`fsnotify` 1.10.1, `sftp` 1.13.11, `x/crypto` 0.57.0, `x/sys` 0.48.0); `go test -race ./...` green.

## 2. Platform consolidation (FR-P) — DRY groundwork

These come first because the security features land *in* them. Each is an extraction of code that already exists in near-identical copies; behavior must not change (existing module tests stay green unmodified except import paths).

- **FR-P1 — `internal/platform/static`.** One `Handler{Dir string}` replacing the byte-identical `staticHandler` in grocery, menuserver, obsidianoid, slideshow, and todo `build.go`. Keeps the rooted-`Clean` join and the index.html SPA fallback exactly as-is. multissh's richer four-row-fallback static handler stays module-local (it has different, tested semantics).
- **FR-P2 — one SSE broker.** Unify the four implementations — `platform/broker.Broker` (signal-only), `platform/broker.MultiRoomBroker`, `slideshow.SSEBroker` (payload + snapshot-on-connect), `obsidianoid.eventBroker` (payload) — into `internal/platform/broker` supporting: signal and payload events, optional snapshot-on-connect, rooms, and a **per-broker max-subscriber cap** (closes **S-5**; default 64, configurable; over-cap connects get 503). Slideshow and obsidianoid migrate to it; their tests carry over.
- **FR-P3 — `internal/platform/fspath`.** One home for the two path-confinement idioms: `ValidName(s)` (single component, no `/` `\` leading-dot — currently duplicated in todo and menuserver) and `ConfineTo(root, rel)` (the `filepath.Rel`-based check slideshow uses, compatible with obsidianoid's prefix check and multissh's `resolveWithinRoot`). Modules migrate; semantics identical (all five call sites were verified correct in the sweep — this is consolidation, not a fix).
- **FR-P4 — `internal/platform/response` everywhere.** obsidianoid's hand-rolled `http.Error`/`fmt.Fprintf` JSON and multissh's private `writeJSON`/`writeError` converge on `platform/response`. Error bodies keep their current shapes where tests assert them (adapt, don't break).
- **FR-P5 — grocery mux modernization.** Rewrite grocery's pre-1.22 routes (`mux.HandleFunc("/api/items", …)` + ~15 in-handler `r.Method` checks) as Go 1.22 method patterns (`"POST /api/items"`), matching the other five modules. Route-for-route behavior identical, including the 405 error body shape; existing handler tests updated mechanically.
- **FR-P6 — "adding a module" checklist.** A short doc (`docs/adding-a-module.md`): `Build(cfg) (http.Handler, error)`, config section, `buildModule` case, `host_routing` entry, and which platform packages to use (static, broker, response, fspath) plus how to opt in to auth (an `auth.modules` entry naming accepted methods) — so future modules inherit the whole security posture by following the pattern.

## 3. Authentication (FR-A)

One package: `internal/platform/auth`. Three **authenticators** that all converge on **one session mechanism**. Auth is **off by default**; a module is protected iff it has an entry in `auth.modules`, and that entry names **which methods the module accepts** — per-module policy, not a global on/off. Example: slideshow accepts `["pin"]`, multissh `["ldap"]`, grocery has no entry and stays open. Authenticator *definitions* (LDAP settings, the PIN table, passkey RP) are global and shared; only the accepted-methods list varies per module.

**Method lists are literal.** A module listing only `pin` does not accept an LDAP session — there is no implied strength hierarchy. Wanting LDAP to also open a PIN module means writing `["pin","ldap"]`. This keeps the gate a set-membership check.

### 3.1 Session mechanism (shared by all three authenticators)

- **FR-A1 — Stateless signed session token.** Successful login (any method) issues a JWT (`golang-jwt/jwt/v5`, HS256) carrying: identity name, the **set of satisfied auth methods**, issued-at, expiry. Delivered as an **HttpOnly, SameSite=Lax cookie**; `Secure` flag from config (the TLS-terminating proxy makes the browser origin https). No server-side session store: the gate verifies signature + expiry, then checks that the token's method set **intersects the module's accepted list**. Sessions survive restarts.
- **FR-A1b — Method accumulation.** If a login request arrives with an existing valid token (same identity or none-yet), the new token's method set is the union of old and new — so with a shared `cookie_domain`, logging in with PIN on slideshow does not clobber an LDAP session needed by multissh. Identity conflict (valid token for a different identity) → the new login replaces the token outright.
- **FR-A2 — Signing key management.** HMAC key auto-generated on first use (32 random bytes), persisted at `<auth.data_dir>/session.key` mode **0600**. Rotating/deleting the file invalidates all sessions (documented as the global-logout lever). The key never appears in the config file.
- **FR-A3 — Session TTL.** `auth.session.ttl_hours`, default 720 (30 days), **sliding**: the gate re-issues the cookie when more than a configurable fraction (default half) of the TTL has elapsed. `POST /api/auth/logout` clears the cookie (token expiry does the rest; no revocation list — accepted for this deployment, see §7).
- **FR-A4 — Cookie domain.** `auth.cookie_domain` (optional). When set to the common parent domain of the routed hostnames (e.g. `.cmdhome.net`), one login covers every protected module. Unset → host-only cookie, per-module login.

### 3.2 Authenticators

- **FR-A5 — LDAP[S].** Port `reference/multissh/internal/auth/ldap.go` (supports `ldaps://` URLs, StartTLS, `insecure_tls` escape hatch, timeout, optional `required_groups`). Successful bind → session token with the LDAP username as identity.
- **FR-A6 — PIN (standalone, named).** `auth.pins` is a list of `{name, hash}` entries; entering a PIN that bcrypt-matches any entry logs in as that name. Hashes are bcrypt (`x/crypto/bcrypt`, already a dep); **plaintext PINs never appear in config, logs, or errors**. A `-hash-pin` CLI flag prompts for a PIN and prints the bcrypt hash for pasting into config.
- **FR-A7 — Login throttle.** PINs have a small keyspace, so `POST /api/auth/login` (all methods) gets a simple in-memory global throttle: after N consecutive failures (default 5), a backoff delay (default doubling from 2s, cap 60s) applies to further attempts; any success resets. Global, not per-IP — per-IP is meaningless behind a 127.0.0.1 proxy without trusting XFF, which we don't.
- **FR-A7b — API keys (method `"key"`).** For non-browser clients — curl, cron, CI smoke tests, automation. `auth.api_keys` is a list of `{name, hash}`; a request carrying `Authorization: Bearer <key>` (or `X-API-Key: <key>`) whose SHA-256 matches an entry passes the gate on any module listing `"key"`, per-request — no session, no cookie ever set. Keys are generated 32-byte random values (`-gen-api-key` CLI flag prints the key and its hash), so a constant-time SHA-256 compare suffices — no bcrypt cost per request and no throttle needed at that entropy. Header check happens before cookie check; a bad key falls through to the other methods rather than erroring. (In-process Go tests don't need this — they can mint a JWT with the test signing key; API keys are for external automation against a live instance.)
- **FR-A8 — Passkeys (WebAuthn).** Port the reference's ceremony code (`go-webauthn/webauthn`): register begin/finish (requires an existing session), login begin/finish, list, delete. Credentials persist at `<auth.data_dir>/passkeys.json` mode 0600. `auth.passkey.rp_id` should be the common parent domain (e.g. `cmdhome.net`) with `auth.passkey.origins` listing the module URLs, so **one passkey works across all protected modules**. Challenge state is short-lived server-side memory (the one statefulness exception; it is inherent to WebAuthn).
- **FR-A9 — Method advertisement, per module.** `GET /api/auth/mode` is **always** served, unauthenticated, on every module — protected or not — reporting **that module's** accepted methods (`{"methods":["pin"]}` on slideshow, `{"methods":["ldap"]}` on multissh, `{"methods":[]}` on an unprotected module). This is the SPA/login-page bootstrap route; folding it behind the gate breaks the default config (shelved finding C1 — preserved).

### 3.3 The gate

- **FR-A10 — `auth.Gate`, fail-closed by construction.** For a protected module, **every route requires a valid session — including static assets** — except a fixed allowlist owned by the gate itself: `GET /api/auth/mode` and the login/ceremony routes (`POST /api/auth/login`, `POST /api/auth/logout`, `GET /api/auth/session`, passkey begin/finish routes). There are **no per-module public-prefix lists**.
  *Deliberate divergence from the shelved v8 design:* v8's two-list `PublicPrefixes`/`PrivatePrefixes` predicate existed to keep static assets public while protecting data routes, and its C4 history shows how easy that predicate is to get wrong (every module is a catch-all-at-`/` SPA). Protecting everything removes the predicate — there is nothing to misclassify, and the per-module route inventory (v8 task 0.2b) is no longer a gate dependency. The cost (assets need a cookie) is paid by FR-A12's login page.
- **FR-A9b — Health endpoint.** `GET /healthz` answers 200 unauthenticated on every module (gate allowlist), so proxy HTTP health checks keep working against protected modules. Body: `{"ok":true}`; no per-module detail (a 503-ing module already reports its build failure on every route).
- **FR-A10b — Per-module method enforcement, server-side.** The gate on module M accepts a session token iff its method set intersects M's accepted list, and M's login routes **reject** an attempt with a method M does not accept (400, before any credential is checked) — the login page hiding the wrong form is cosmetic, never load-bearing.
- **FR-A11 — Mounting: universal gate, hot-swappable policy.** `auth.FromConfig(cfg.Auth) (*Service, error)` is called once in `cmd/server/main.go`; **every** module is wrapped with the gate at registration (inside the existing build-once memoization map — two hostnames → one module → one gate). The gate consults the Service's current policy **per request** through an atomically-swappable snapshot, which is what lets the admin module apply changes live (FR-M3) with no re-wiring. For a module with no `auth.modules` entry the gate serves only `GET /api/auth/mode` (`{"methods":[]}`) and `GET /healthz` and passes everything else through unchanged — behaviorally today's module; login routes are **not** served on it (v8's mounting-guard intent, preserved). Unknown module names in `auth.modules`, or a method name with no configured authenticator (e.g. `"pin"` with an empty `pins` table), are **boot errors**, and the same validation rejects an admin-UI save (FR-M5) — a typo must not silently unprotect a module or brick its login.
- **FR-A12 — Login page.** When an unauthenticated request wants HTML (`Accept: text/html` on a GET), the gate answers 401 with a **platform-owned login page** (one page, served from the gate, styled minimally; shows PIN and/or LDAP form and a passkey button per `/api/auth/mode`). Non-HTML requests get a bare 401 JSON body. No module frontend contains auth logic; module SPAs never see an unauthenticated state.
- **FR-A12b — Auth event log.** One log line per login success and failure (method, identity name or `unknown`, module, and for failures the reason class — bad credential, disallowed method, throttled). Never log credentials, PINs, key material, or tokens.
- **FR-A13 — Gate transparency.** The gate must not wrap `http.ResponseWriter` (multissh's WebSocket upgrade needs `http.Hijacker`; v8 Phase-0 rule, preserved) and must pass SSE (`EventSource` sends cookies same-origin) and WebSocket connects (browser sends cookies on the upgrade request) through session verification unchanged.

### 3.4 Config surface

```json
"auth": {
  "modules": {
    "multissh":  ["ldap", "passkey", "key"],
    "slideshow": ["pin"],
    "obsidianoid": ["ldap", "pin", "passkey"],
    "admin": ["passkey"]
  },
  "admin_pin_file": "/etc/unified-webapp/admin.pin",
  "data_dir": "…",
  "cookie_secure": true,
  "cookie_domain": ".cmdhome.net",
  "session": { "ttl_hours": 720 },
  "ldap": { "url": "ldaps://…", "start_tls": false, "insecure_tls": false,
             "bind_dn_template": "…", "required_groups": [] },
  "pins": [ { "name": "chris", "hash": "$2a$…" } ],
  "api_keys": [ { "name": "ci-smoke", "hash": "sha256:…" } ],
  "passkey": { "rp_id": "cmdhome.net", "origins": ["https://multissh.cmdhome.net"] }
}
```

- **FR-A14.** An authenticator is configured iff its config block is present/non-empty; it is *used* by the modules that list it. `auth.modules` empty or `auth` absent → binary behaves exactly as today (regression-tested; the universal gate's pass-through must be behaviorally invisible). Included in `WriteDefault` (commented-out/empty form), `unified-webapp-example.json`, and README.

### 3.5 The `admin` module (FR-M)

A seventh module managing auth configuration from the browser. Built exactly per the FR-P6 checklist (its first consumer): `internal/admin`, `Build(cfg) (http.Handler, error)`, `host_routing` keyword `admin`, SPA under `web/admin`.

- **FR-M1 — Scope.** Panels: LDAP settings with a **test-connection** button (attempts a bind server-side, reports success/failure class — never echoes credentials); PIN management (add/remove named PINs — the server bcrypts, plaintext never round-trips); API keys (generate — key shown exactly once — and revoke); passkeys (register/list/delete, reusing FR-A8's ceremony routes); the **module × method assignment matrix**; session settings (TTL, cookie domain/secure) read-write. All mutations are `/api/*` routes on the admin module — gate-protected, origin-checked, body-limited like everything else; the frontend is a convenience per NFR-1.
- **FR-M2 — The operator PIN (break-glass, always on).** The admin module's gate **always accepts the operator PIN**, regardless of the assignment matrix — the matrix can grant admin *additional* methods but can never remove this one. It is defined outside the UI's reach, in exactly one of two config forms:
  - `auth.admin_pin` — a bcrypt hash inline in the config file (generated with `-hash-pin`), or
  - `auth.admin_pin_file` — a path to a file containing the **plaintext PIN by itself** (trimmed). The server **refuses the file if its mode grants any group/other permissions** (0400 or 0600 required; anything looser is a boot/login error naming the fix). Read per login attempt, so editing the file takes effect immediately — and the FR-A7 throttle bounds the read rate.
  Setting both, or neither while `admin` is routed, is a **boot error** — the admin module never builds unprotected. The admin UI displays the PIN as "defined by config" and cannot edit or delete it. This is both the first-run bootstrap (`echo`+`chmod 0400`, done) and the lockout guarantee: no sequence of UI edits can lock the operator out — worst case, the operator PIN still works. Logs in as identity `admin` with method `pin`; the FR-A7 throttle applies to it like any PIN.
- **FR-M3 — Live apply.** Save → server-side validation (the same code path as boot validation — one validator, per FR-A11) → **atomic 0600 write of the config file** (tmp + rename, preserving all non-auth sections) → atomic in-process swap of the auth policy snapshot. The next request anywhere in the binary sees the new policy; no restart, no half-applied state — a rejected save changes nothing on disk or in memory.
- **FR-M4 — Restart equivalence.** The config file remains the single source of truth: a restart after any sequence of admin-UI edits yields exactly the state the UI showed. (No shadow stores; passkeys stay in their existing FR-A8 file, which the config references.)
- **FR-M5 — Self-preservation guardrails.** Validation rejects a save that names an unknown module or an unconfigured method (FR-A11's rules). Removing the admin module's own extra methods is allowed — the operator PIN makes it non-lockout-able by construction.
- **FR-M6 — A deliberate special case.** admin follows the module pattern (Build/config/routing/static per FR-P6) but knowingly deviates where its job requires: it is the only module that writes the config file, the only one with an always-on method outside the assignment matrix (FR-M2), and the only one that cannot exist unprotected. These exceptions are this list — anything else about it stays pattern-conformant, and no other module gets any of these powers.

## 4. Origin posture & headers (FR-O)

Per the deployment context: enabled by default, never strict, one switch to relax.

- **FR-O1 — Retire the wildcard.** `middleware.Wrap` stops emitting `Access-Control-Allow-Origin: *`. Same-origin requests need no CORS headers at all; the header is **reflected only when `Origin` matches the request's own scheme+host**. (Shelved 4.6a.)
- **FR-O2 — Server-side origin check on writes.** In `internal/platform/middleware`: every non-`GET`/`HEAD` request, at any path, on every module, with an `Origin` header that mismatches the request host → 403, logged with the observed `Origin`/`Host` pair (one-line diagnosable). **Absent `Origin` is always permitted** — curl, scripts, and non-browser clients are unaffected. (Shelved 4.6b; the method-not-path scoping is load-bearing — todo's whole write surface is non-`/api/`.) Runs **outside** the auth gate: cross-origin writes die before credentials are examined.
- **FR-O3 — The relax switch.** `server.origin_check: "enforce" | "log" | "off"` (default `enforce`). `log` answers normally but logs would-be rejections — the escape hatch if a proxy rewrites `Host` (shelved risk R1). Document in README that the proxy must preserve `Host` (it already must, for dispatch).
- **FR-O4 — Security headers.** Same middleware adds `X-Content-Type-Options: nosniff` on everything. Nothing further (no CSP project — see §7).

## 5. Hardening (FR-R)

- **FR-R1 — Server timeouts** (S-4). Replace bare `ListenAndServe` with an `http.Server`: `ReadHeaderTimeout: 10s`, `IdleTimeout: 120s`, `ReadTimeout: 0` (multissh streams multi-GB uploads), `WriteTimeout: 0` (SSE and WebSockets are long-lived by design). This is the S-4-note-approved shape: slow-header connections die, streams live.
- **FR-R2 — Request body limits** (S-3). Per-module `max_body_bytes` applied via `http.MaxBytesReader` in a platform middleware wrapped at module registration. Default 1 MiB; multissh's default derives from its existing `max_upload_bytes` (+ multipart overhead) so uploads keep working. Handlers already return 400 on decode errors; oversized bodies surface as 413 (`MaxBytesError`).
- **FR-R3 — SSE subscriber cap** (S-5). Delivered by FR-P2's unified broker.
- **FR-R4 — File modes** (S-8, N-1). Config writes (`config.go:358`) and all module **data-file** writes go 0644 → **0600**; data directory creation 0755 → 0750. Auth artifacts (`session.key`, `passkeys.json`) are 0600 from birth (FR-A2/A8). Static-asset serving is untouched. Existing files are not chmodded retroactively — new writes carry the new mode (per S-8's own note).
- **FR-R5 — Render endpoint hygiene** (N-3). obsidianoid `POST /api/render` responses gain `X-Content-Type-Options: nosniff` (via FR-O4) and a `Content-Security-Policy: sandbox` header on that response only, neutering script execution if the rendered HTML is ever opened as a document. No sanitizer dependency. One line, noted once.

## 6. Non-functional requirements

- **NFR-1 — No frontend trust.** Every control above is server-side. The login page and module SPAs are conveniences; deleting all JS must not weaken any guarantee.
- **NFR-2 — Testability.** The gate is a pure function of (request, key, clock) — unit-testable without a network. LDAP client tested against the reference's existing fake; PIN and throttle are table tests; passkey ceremonies keep the reference's test coverage. Every new platform package gets its own test file (`middleware` and `response` currently have none — fix that while touching them).
- **NFR-3 — Uniformity.** After FR-P1…P5, a grep for `staticHandler`, `validName`, hand-rolled JSON errors, or `r.Method !=` method checks in module code returns nothing.
- **NFR-4 — Build discipline.** `CGO_ENABLED=0` buildable; `make test` (`go test -race ./...`) green; goleak stays green (the broker unification and gate must not leak goroutines).
- **NFR-5 — Additive config.** An existing config file from today loads unchanged and produces today's behavior (auth off, origin check `enforce` being the one new default — its README note is part of the deliverable).
- **NFR-6 — No push.** Work on `security-fix`; commits allowed, never push.

## 7. Non-goals (recorded once)

- TLS in the binary (the proxy owns it; existing `tls_cert`/`tls_key` config stays as-is, untouched).
- Session revocation store / per-session logout-everywhere (key rotation is the global lever).
- Per-IP rate limiting or `X-Forwarded-For` trust.
- A CSP project for the SPAs; markdown sanitization in obsidianoid (FR-R5's sandbox header is the whole ambition).
- Host-header dispatch hardening beyond the proxy contract (S-7 stays a deployment note).
- Multi-user account management, roles, or per-module identities — an identity is a name in a token.

## 8. Traceability

| Prior finding | Resolved by |
|---|---|
| S-1 no auth | §3 entire (FR-A1…A14) |
| S-2 wildcard CORS | FR-O1, FR-O2 |
| S-3 body limits | FR-R2 |
| S-4 timeouts | FR-R1 |
| S-5 SSE unbounded | FR-P2 / FR-R3 |
| S-6 TLS optional | Non-goal (proxy terminates; §7) |
| S-7 Host routing | Non-goal (proxy contract; §7 + FR-O3 README note) |
| S-8 file modes | FR-R4 |
| N-1 config secrets 0644 | FR-R4 + FR-A2 (secrets out of config where possible) |
| N-2 PIN brute force | FR-A7 |
| N-3 render XSS | FR-R5 |
| N-4 security headers | FR-O4 |
| N-5 git-push surface | FR-A10/A11 (an `auth.modules` entry for obsidianoid — operator's choice) |
| Shelved C1 (auth/mode public) | FR-A9 |
| Shelved C4 (public-path predicate) | FR-A10 (predicate eliminated, not fixed) |
| Shelved `Service != nil` guard | FR-A11 |
| Shelved R1 (Host-rewriting proxy) | FR-O3 `log` mode + README note |
| Shelved R9 (cross-origin writes execute) | FR-O2 |
| Shelved 0.2b route inventory | Obsolete for the gate (FR-A10); still informs FR-O2's tests |

## 9. Acceptance criteria

1. With `auth` absent from config: every module behaves as today (existing tests green, no login routes served, no auth cookie ever set) — the universal gate's only observable additions being `GET /api/auth/mode` (`{"methods":[]}`) and `GET /healthz`.
2. For **every** module in `auth.modules`, iterated from config in one test: an unauthenticated request to a data route, a static asset, an SSE endpoint, and (multissh) a WebSocket upgrade all yield 401; `GET /api/auth/mode` yields 200.
3. Each authenticator logs in end-to-end: LDAP (against the fake), PIN (bcrypt match → token → gate passes), passkey (ceremony tests). All three produce tokens the same gate verifies. **Per-module policy enforced:** with slideshow=`["pin"]` and multissh=`["ldap"]`, a PIN-only token opens slideshow and gets 401 on multissh; a PIN login attempt on multissh is rejected before credential check (FR-A10b); after an additional LDAP login the accumulated token (FR-A1b) opens both.
4. Expired token → 401; garbage token → 401; token signed with a rotated key → 401; near-expiry token → response carries a refreshed cookie.
5. Five consecutive failed PIN logins → measurable backoff delay on the sixth; success resets.
5b. A request with a valid `Authorization: Bearer` API key passes the gate on a module listing `"key"` with no cookie involved; the same key gets 401 on a module not listing `"key"`; an invalid key falls through to normal 401. `GET /healthz` answers 200 unauthenticated on protected and unprotected modules alike.
6. Cross-origin `POST` (mismatched `Origin`, including `Content-Type: text/plain` simple requests) → 403 on a sample route of **each** module — including todo's `POST /config/columns` and non-`/api/` writes — with no state change (assert the column list unchanged, no multissh job created). Absent-`Origin` writes succeed. `origin_check: "off"` restores today's behavior.
7. Body over the module's `max_body_bytes` → 413; a multissh upload at its configured size still succeeds.
8. Broker at its subscriber cap refuses the next SSE connect with 503; existing streams unaffected; goleak green.
9. New config and data files land 0600 (dirs 0750); `session.key` and `passkeys.json` are 0600.
10. `staticHandler`/`validName`/method-check greps per NFR-3 are empty; slideshow and obsidianoid run on the platform broker with their existing behavior tests green.
11. `make test` green with `-race` on Go 1.26.x with the updated dependency set.
12. **Admin module:** the operator PIN (from `admin_pin_file`, 0400) logs in to admin even with an empty assignment matrix; a PIN file with mode 0644 is refused with an error naming the fix; routing `admin` with neither `admin_pin` nor `admin_pin_file` (or both) fails boot.
13. **Live apply:** via the admin API, add a previously-open module to the matrix → its very next unauthenticated request 401s, no restart; remove it → next request passes; an invalid save (unknown module name, unconfigured method) is rejected with the config file and in-memory policy both provably unchanged.
14. **Restart equivalence:** after a sequence of admin edits (PIN added, API key generated, matrix changed), restart the binary → behavior identical to pre-restart (FR-M4); the rewritten config file preserves all non-auth sections byte-for-byte.
15. Admin UI mutations carry no plaintext secrets in responses (generated API key appears exactly once, in the generate response only); LDAP test-connection reports outcome class without echoing credentials.
