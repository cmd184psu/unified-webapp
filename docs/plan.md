# Implementation Plan — Meld `multissh` into unified-webapp

**Date:** 2026-09-10
**Status:** v9 — **pending approval**. Scope: port the multissh module. Auth and security are a separate, later round — see §0. Do not execute without approval.
**Branch:** `dev` — commits allowed, **NEVER push**
**Authoritative requirements:** `/opt/unified-webapp/multissh-FRD.md` — **minus FR-A3, FR-A4, FR-A5 and AC-6**, deferred per §0
**Reference source:** `/opt/unified-webapp/reference/multissh/`

---

## 0. Scope

**This round ports the multissh module.** Auth, passkeys, TLS and origin policy are a **separate round of work to follow**, not cancelled work. Nothing in this plan is designed to be hard to add them to later.

The auth design from v1–v8 is kept in `shelved/` so round two starts from it rather than from scratch:

| Shelved | Contents |
|---|---|
| `plan-with-platform-auth.md` | The v8 plan — `auth.Gate`, the public/private prefix policy, `FromConfig`, LDAP, passkeys, the origin middleware |
| `multissh-FRD.md` | The FRD as it stood while auth was in scope |
| `README.md` | Which findings are worth not re-deriving |

**What that means concretely for this round:** `internal/auth` is not ported, `go-ldap` and `go-webauthn` stay out of `go.mod`, `secure_mode` and `protected_modules` are not added to config, and `internal/platform/middleware` is not touched. FRD items **FR-A3, FR-A4, FR-A5 and AC-6** belong to round two.

Everything the reference already does — strict host-key checking, path-traversal rejection, generic error text, the WebSocket origin check — ports verbatim under P2, because it is part of the working app being ported.

**`cmd/server/main.go` binds `0.0.0.0:%d`, not loopback.** Noted so it is not mistaken for an oversight later.

---

## 1. RALPLAN-DR Summary

### Principles

- **P1 — Pattern conformance over novelty.** multissh must look like the other five modules: `internal/multissh/Build(cfg) (http.Handler, error)`, a `MultisshConfig` JSON section, static assets from `web/multissh`, one `case` in `buildModule`. No new server, no new port, no new build system.
- **P2 — Preserve working behavior; change only what the FRD changes.** The reference code and tests are battle-tested. Port verbatim where possible; the diff should isolate to (a) import paths, (b) config/env removal, (c) embed→static_dir, (d) removal of the auth surface, (e) the five §3.4 redesign items.
- **P3 — Secrets never touch disk.** SSH private keys stay server-side and are referenced by name only; passwords (FR-N4) live in process memory for the life of a session/job and are excluded from `hosts_path`, logs, and error strings **by construction, not by convention** — the persisted type has no field for them.
- **P4 — Every phase independently verifiable.** Each phase compiles and tests green on its own, so execution can proceed in order without a "big bang" integration step at the end.
- **P5 — The existing five modules keep working exactly as they do now** (NFR-5), with two dispatcher-level exceptions that improve them: modules are no longer per-hostname isolated (**4.5**, a latent-bug fix for any module with shared on-disk state), and a `buildModule` failure 503s that module instead of `log.Fatalf`-ing the process (**4.7**). Shared files touched: `cmd/server/main.go` (one `case`, plus 4.5 and 4.7), `internal/platform/config` (one new section), `go.mod`, `package.json`, `tsconfig.json`.

### Decision Drivers (top 3)

1. **D1 — Test parity is the acceptance gate.** FR-I7 + AC-1 require the ported suites to pass under `go test -race ./...`. Any approach that discards reference tests loses the only cheap correctness signal for ~2,500 lines of concurrency-heavy Go.
2. **D2 — One build pipeline.** FR-I4 + NFR-1: root `package.json` esbuild and `make build` must remain the only build entry points. A second toolchain (Vite) inside the repo is an ongoing maintenance and CI cost.
3. **D3 — Redesign surface is mostly frontend.** Four of five FR-N items (N1 layout, N2 collapse, N3 history, N5 `key:` keywords) are UI-only; only N1's limit plumbing and N4's password path touch Go. This argues for maximum Go reuse and a willingness to restructure TS.

### Viable Options

#### Backend axis

**Option A′ — Port everything except `auth` (CHOSEN; promoted from runner-up).**
Copy `internal/{server,sshproxy,config}` into `internal/multissh/{,sshproxy}`, rewrite import paths, drop `embed.go`, **delete the auth surface from `server.go` outright** rather than relocating it, re-express `cmd/multissh/main.go`'s `run()` as `Build()`, fold config into `internal/platform/config`, thread `MaxSessions` through `hosts.go`/`broadcast.go`, add a password credential path.

- Pros: drops the largest dependency subtree (go-ldap, go-webauthn, go-tpm, cbor) from `go.mod`, shrinking the binary and the `CGO_ENABLED=0`/arm64 cross-build risk (R5); removes the **only untested reference package** from the diff, and with it the entire `internal/platform/auth` test-writing task; Phases 3, 4 and 6 all get materially shorter; `cmd/server/main.go` and `internal/platform/config` are touched shallowly.
- Cons: FR-A3/A4/A5 and AC-6 are round-two items (§0). The `Provider` indirection in `server.go` is deleted rather than stubbed, so a seam has to be re-introduced rather than filled in — `Handler()` is kept as the attachment point (3.2), and the shelved plan's task 3.1c is written to apply on top of this port.

**Why A′ now wins.** v1–v8 rejected it on one ground — AC-6 needs a real login — and its own invalidation note said: *"If the FRD's auth scope is ever renegotiated, A′ becomes the preferred backend option and should be reconsidered before A is re-derived."* Auth moved to round two, so A′ is adopted directly rather than re-derived.

**Option A — Port-and-adapt including `auth`.** The v1–v8 choice: port `internal/auth` too, and (under the since-shelved O-1) promote it to `internal/platform/auth` fronting all six modules.
- Pros: satisfies FR-A3/A4/A5 and AC-6; one login flow for the whole binary if auth is ever wanted.
- Cons: lands two rounds of work as one; carries LDAP + WebAuthn into `go.mod` before anything uses them.
- **Invalidation:** deferred to round two, not rejected on merit. Preserved in full at `shelved/plan-with-platform-auth.md`.

**Option B — Rewrite the module against unified's idioms.**
- Cons: discards the reference test suites (violates D1 and FR-I7 as literally written); reintroduces subtle bugs in PTY framing, origin checks, and partial-upload cleanup; multiplies effort with no FRD-visible benefit.
- **Invalidation:** FR-I7 names the specific suites to port and AC-1 gates on them; a rewrite cannot satisfy "port the reference test suites" without also rewriting them, at which point the tests validate the new code against itself. Rejected as failing a hard requirement, not merely as more expensive.

#### Frontend axis

**Option C — Port TS sources and build with `esbuild --bundle` (CHOSEN).**
Move `web/src/*.ts` + `ssh.css` to `web/multissh/js/`, add `@xterm/xterm` + `@xterm/addon-fit` to root `package.json` dependencies, add a bundling esbuild invocation to the root `build` script, hand-write `web/multissh/index.html`.

- Pros: satisfies D2 (one toolchain); sources are editable in-repo, which is mandatory since FR-N1..N5 require real TS changes; `tsc --noEmit` covers the new files via the existing root `tsconfig.json`; consistent with how every other module is built.
- Cons: this is unified's first bundled module — esbuild flags diverge from the existing bundleless invocations; xterm CSS must be routed through esbuild's CSS handling (it lives in `node_modules`, so hand-copying is not an option — see 5.3); `node_modules` gains runtime deps; the root `tsconfig.json` `include` list is an explicit allowlist that must be extended or typecheck silently skips the module (5.3b).

**Option D — Keep the prebuilt Vite `dist/` artifacts.**
- Cons: **fatal** — the redesign items FR-N1..N5 require source edits, and the `dist/` bundle is minified with hashed filenames; no `tsc` coverage; violates FR-I4's explicit "port ... compiled by esbuild via the root `package.json`".
- **Invalidation:** FR-I4 mandates esbuild-built sources under `web/multissh/`, and §3.4 cannot be implemented against a minified artifact. Rejected outright.

### Chosen combination

**A′ + C:** port the Go minus `auth`, port-and-restructure the TS with `esbuild --bundle`.

---

## 1A. Pre-mortem (deliberate mode)

*It is March 2027. This shipped. Four ways it went wrong.*

**Scenario 1 — "The password was in the logs the whole time."**
Someone debugging a broadcast failure added `log.Printf("target: %+v", t)` to `broadcast.go`. `broadcastTargetRequest` was a plain struct, so every password fanned out that day landed in journald in plaintext and stayed there for the journal's retention window. The redacting credential type existed and worked — but it sat one layer downstream of the wire struct the password actually arrived in, so it never saw the value. AC-10's grep had passed at ship time because it only walked the paths the manual pass happened to walk.
*Root cause:* redaction applied to *a* type rather than to **every** type that holds the secret.
*Task changes:* the `Secret` type is applied to `clientMsg`, `broadcastTargetRequest` **and** the credential (2.3b); redaction is asserted by name on all three under `%v`/`%+v`/`%s`/`%#v` and `json.Marshal` (2.4); 2.4c proves at the *type* level that the persisted struct has no password field at all.

**Scenario 2 — "Two hostnames, two host lists, and neither one was right."**
The operator mapped both `multissh.cmdhome.net` and `ssh.cmdhome.net` to the module. `cmd/server/main.go` called `buildModule` once per hostname, so each hostname got its own `Server` with its own `hostStore` reading the same file. Edits made through one hostname were invisible through the other, and whichever tab saved last silently overwrote the other's hosts. Nothing errored; the file was valid JSON throughout.
*Root cause:* the dispatcher's per-hostname construction loop is fine for stateless modules and wrong for the first module with shared mutable on-disk state. multissh is that module.
*Task changes:* **4.5** memoizes `buildModule` by module name so all hostnames mapped to a module share one handler; its test maps two hostnames to multissh and asserts a `PUT` through one is visible through the other. This is the one change to shared dispatcher code in the plan, and it is a bug fix for the existing five as much as for multissh.

**Scenario 3 — "Every terminal failed to connect in production and worked perfectly in dev."**
Behind nginx, `Host` arrived rewritten while `Origin` carried the browser's original value, so the reference's Origin-vs-Host equality check rejected every WebSocket upgrade. It worked on the dev box because there was no proxy. The failure was total and the error surfaced only in the browser console.
*Root cause:* an origin check ported verbatim (correctly, under P2) into a deployment whose proxy hop the reference never had.
*Task changes:* **4.8** tests the check behind the dispatcher with matching, absent, and foreign `Origin` values, and **6.2** documents the required nginx `proxy_set_header Host $host;` alongside the `Upgrade`/`Connection` headers. R1 tracks it.

**Scenario 4 — "The port was green and the app was blank."**
`go test -race ./...` passed, every handler suite included. The page loaded, xterm's CSS was missing, `tsc --noEmit` had never looked at `web/multissh/`, and a type error in `hosts.ts` shipped. The root `tsconfig.json` `include` is an explicit allowlist (`["web/obsidianoid/js/*.ts", "web/slideshow/js/*.ts"]`) — a new module is simply not typechecked, silently, and the Go suite cannot see any of it.
*Root cause:* the acceptance signal (Go tests) and the defect surface (TS + bundling) did not overlap at all, and the frontend toolchain fails open.
*Task changes:* **5.3b** extends the `include` allowlist **and** proves the extension works by introducing a deliberate type error and confirming `npm run typecheck` goes red; **5.3** routes xterm's CSS through esbuild rather than hand-copying; **6.4**'s manual pass is the only gate that exercises the rendered UI, and it is not optional.

---

## 2. Implementation Phases

> **Task numbers are stable, not contiguous.** Round two owns **0.2b**, **1.2c**, **3.1**, **3.1b**, **3.1c**, **3.8**, **4.6/4.6a/4.6b** and **4.9**, so those IDs are absent here. The gaps are deliberate — every cross-reference in this document and every task ID in `shelved/README.md` still points at the same thing. Do not renumber.

Each numbered task is designed to be an independently executable ralph task: it names the files it touches, what to do, and how it is verified. Tasks within a phase are ordered; phases are strictly sequential.

### Phase 0 — Baseline & scaffolding

**0.1** Confirm branch is `dev` and the working tree builds green before any change: `git rev-parse --abbrev-ref HEAD`, `go build ./...`, `go test -race ./...`, `npm run typecheck`. Record the baseline result. Do not proceed if baseline is red — fix or report first.

**0.2** Record the verified preconditions this plan depends on (re-check, do not assume):
- `internal/platform/middleware.Wrap` does **not** wrap `http.ResponseWriter`, so `http.Hijacker` survives to the module handler and WebSocket upgrades work behind the dispatcher.
- `middleware.Wrap` sets `Access-Control-Allow-Origin: *` on the **entire** dispatcher — left as-is; no task here changes it (R9).
- `cmd/server/main.go` calls `buildModule` once **per hostname** in the `cfg.Routing` loop — addressed in task 4.5.
- `reference/multissh/internal/auth` is **not ported** (§0) — noted so it is not ported by reflex.
- **No new middleware is inserted in front of any module**, so the only thing between the listener and multissh is the existing `middleware.Wrap`, verified above. Standing rule for anything added later (round two included): **do not wrap `ResponseWriter` in front of multissh** — it breaks the WebSocket upgrade. If something must, it implements `Hijack()` by delegation and its test drives a real upgrade through the full chain (4.8 is the template).

**0.3 (FR-I6)** Merge Go dependencies into `/opt/unified-webapp/go.mod` by adding the reference's direct requires and running `go mod tidy`:
`golang.org/x/crypto`, `github.com/pkg/sftp`, `github.com/gorilla/websocket` (indirects resolve automatically). `go-ldap` and `go-webauthn` are **not** added — they belong to round two, and with them go go-tpm, cbor and msgp. Also add `go.uber.org/goleak` as a **test-only** dependency if the preferred 6.1b approach is taken — added here so a version conflict surfaces alongside the rest of the merge rather than in Phase 6. Verify `CGO_ENABLED=0 go build ./...` still succeeds.

**Verify Phase 0:** `go build ./...` && `go test -race ./...` && `git status` shows only the intended new files.

---

### Phase 1 — Config plumbing (FR-I3)

**1.1** Add to `/opt/unified-webapp/internal/platform/config/config.go`:
- `MultisshConfig` struct with JSON tags: `static_dir`, `ssh_dir`, `upload_dir`, `hosts_path`, `browse_root`, `max_sessions`, `max_upload_bytes`, `strict_host_key`, `known_hosts_path`.

  **No `auth` tag and no `secure_mode`** — FR-I3 lists both; both are round-two fields (§0). Omit `secure_mode` rather than adding it as an inert boolean: round two will define what it gates. `encoding/json` ignores unknown fields, so a config carrying it still loads.
- `Multissh MultisshConfig \`json:"multissh"\`` field on `Config`.

**1.2** Extend `DefaultConfig()` with the multissh defaults: `static_dir: "./web/multissh"`, `hosts_path: "./data/multissh/multissh-hosts.json"`, `max_sessions: 3`, `max_upload_bytes: 8589934592`, `strict_host_key: false`. **Do not port `applyAuthDefaults` (reference `config.go:180-219`)** — it hardcodes private lab values for `ldap.url`, `baseDN`, `searchBase` and passkey `rpID`/`origins`, and under §0 there is no auth section for them to populate. Leave `ssh_dir`, `upload_dir`, `browse_root`, `known_hosts_path` empty — they are resolved at Build time (task 4.2), not baked into the file, so the defaults stay portable.

**1.2b** Validate `max_sessions` in **exactly one place** — `config.Load` — so no other code re-checks it: reject `< 1` with a clear error; treat `0` as "unset" and substitute the default `3`; clamp values above `16` to `16` with a `log.Printf` warning (an absurd N is an operator typo, and the UI cannot render hundreds of panels). `Build()` (4.3) then trusts the value and performs **no** re-validation.

**1.3** Add `expandMultisshPaths(*MultisshConfig) error` following the existing `expand*Paths` helpers (`ExpandPath` on `static_dir`, `ssh_dir`, `upload_dir`, `hosts_path`, `browse_root`, `known_hosts_path`) and call it from `Load()` alongside the other five.

**1.4** Port the applicable cases from `reference/multissh/internal/config/config_test.go` into `internal/platform/config/config_test.go` (or a new `config_multissh_test.go`): JSON round-trip, defaults, `~` expansion. **Drop** every env-var test case and every auth-object case — the env layer is intentionally removed (FR-I3).

**1.5** Add a `"multissh"` section to `/opt/unified-webapp/unified-webapp-example.json` with every field and a commented-style example `host_routing` entry `"multissh-test.cmdhome.net": "multissh"`; document the section in `/opt/unified-webapp/README.md` next to the existing five.

**1.6** Decide and document the `make init-config` representation of the deferred-default fields (`ssh_dir`, `upload_dir`, `browse_root`, `known_hosts_path`): emit them as **present-but-empty strings** (`"ssh_dir": ""`), never omitted. Empty means "resolve at Build time" and is a documented, round-trip-stable value; omission would be indistinguishable from a typo'd key. Note this convention in the README section from 1.5.

**Note on JSON tag style — settled, do not re-litigate.** **snake_case throughout**, matching unified's existing sections and FR-I3's own spelling. *(v9: this note ran to four paragraphs across v3–v8 because the contested part was the `auth` sub-object — whether to keep the reference's camelCase (`rpID`, `sessionTTLMinutes`) for a mechanical diff. With auth shelved, every field left in `MultisshConfig` is one FR-I3 names in snake_case, and the question no longer exists. v1's claim that camelCase would preserve existing operator configs was false in any case: every outer field is renamed by the move, so no reference config loads verbatim under any variant.)*

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

### Phase 3 — Port the `server` handlers (FR-H*, FR-U*, FR-A6, FR-A7, FR-N1 backend)

**3.2** Copy `reference/multissh/internal/server/{server,hosts,files,sftp,upload,broadcast}.go` to `internal/multissh/` (package `multissh`) with import rewrites. **Do not copy `embed.go`** — static assets come from `static_dir` (FR-I4). **Do not copy `auth_handlers.go`** — round two (§0).

**Consequence for `server.go` — the exact removal set.** `server.go` is the only one of the six copied files that touches auth; the other five are clean. Delete, precisely:

| What | Where |
|---|---|
| `Options.Auth`, `Options.AuthSvc`, `Options.AuthCookieSecure` | `server.go:32-34` |
| `Server.auth`, `Server.authSvc`, `Server.authCookieSecure` | `server.go:46-48` |
| the three assignments from `opts` in `New()` | `server.go:93-95` |
| the `GET /api/auth/mode` registration **and** the whole `if s.authSvc != nil { … }` block registering the other eight | `server.go:96-107` |
| the `if s.auth != nil { return s.auth.Middleware(s.mux) }` branch | `server.go:133-135` |

…plus the `internal/auth` import at the top of the file, leaving `func (s *Server) Handler() http.Handler { return s.mux }`.

**Why this must be spelled out:** `auth_handlers.go` is not copied, so a verbatim `server.go` leaves `New()` referencing nine handler methods that do not exist and **Phase 3 does not compile** (P4). *(This description was wrong in three ways from v4 through v7 — line numbers, field count, and a claim that `Handler()` mounts the routes when it mounts nothing. Corrected in v8, re-verified against source in v9.)*

`Handler()` becomes a pass-through. **Leave it in place** rather than returning `s.mux` at the call sites — it is where round two re-attaches, and one indirection costs nothing.

**Verified safe to remove (re-checked against source in v9):** `writeJSON`, `sessionCookieValue`, `passkeyInfoToResponse`, `writeAuthError`, `setSessionCookie` and `clearSessionCookie` are referenced **only** inside `auth_handlers.go` — nothing in `broadcast.go`, `files.go`, `hosts.go`, `sftp.go` or `upload.go` uses them — and **none** of the 18 `New(Options{…})` call sites across `files_test`, `hosts_test`, `sftp_test`, `broadcast_test` and `upload_test` sets `Auth`, `AuthSvc` or `AuthCookieSecure`. The ported suites stay green without edits. Note `writeError` (`server.go:159`) is **not** part of the removal — the five non-auth files use it, so it stays in `internal/multissh`.

**Also update `Handler()`'s doc comment** (`server.go:130`), which reads "…wrapped with the auth middleware when configured" — verified as the only other `auth` mention in the six copied files, and false once the branch below it is gone.

Assert the result: `go list -deps ./internal/multissh | grep -E 'go-ldap|go-webauthn'` returns nothing, and `grep -rni auth internal/multissh/*.go` returns nothing at all.

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

**3.6b — Expose `maxSessions` to the frontend (FR-N1 backend half).** The frontend needs `max_sessions` to size the host rail and the panel grid, including before any hosts exist.

*Why this is a plain endpoint.* v5–v8 instead widened the reference's `GET /api/auth/mode` to carry `maxSessions`, and spent three iterations on the consequences. With that route gone, the requirement is what it always was: one number, fetched at boot.

*The task.* Add **`GET /api/config`** to `internal/multissh`'s mux, returning `{"maxSessions": N}` from `Options.MaxSessions`. Plain handler, no middleware, no shared package, ~8 lines.

*Note the name collision is only apparent.* menuserver, todo and slideshow each serve a bare `GET /config` (their own, unrelated). multissh's is `/api/config`, on its own hostname, behind its own handler. No dispatcher-level ambiguity exists.

Test: `GET /api/config` returns 200 and `{"maxSessions":5}` when `Options.MaxSessions` is 5.

**3.7** Add tests for the new backend behavior: `max_sessions: 5` accepts a 5-target broadcast and rejects 6; and `PUT /api/hosts` round-trip proves a submitted password is absent from the file on disk (AC-10).

*(3.6b's `/api/config` test replaces the two `/api/auth/mode` assertions v7 listed here, and lives in this package, where the route actually is.)*

**Verify Phase 3:** `go test -race ./internal/multissh/...` all green; `go build ./...`. Commit checkpoint on `dev`.

---

### Phase 4 — Module `Build()` + `main.go` wiring (FR-I1, FR-I2, FR-I5)

**4.1** Create `/opt/unified-webapp/internal/multissh/build.go` exposing `Build(cfg config.MultisshConfig) (http.Handler, error)`, mirroring `internal/grocery/build.go`'s shape.

> **`reference/multissh/cmd/multissh/main.go` is the specification for `Build()`, not dead weight.** v1 of this plan listed it under "delete". Its `run()` (main.go:**63-144**) and `buildAuth()` (main.go:**149-185**) contain exactly the wiring `Build()` must reproduce: `sshproxy.DefaultSSHDir()`, the `upload_dir`/`known_hosts` defaulting, `sshproxy.HostKeyCallback`, the `SFTPTransferrer` used as **both** `Transferrer` and `RemoteLister`, `sshproxy.NewHandler(SSHDialer{...}, sshDir)`, and the `server.New(server.Options{...})` call. **That call is ten fields here, not the reference's twelve** (verified against `cmd/multissh/main.go:103-116`): drop `Auth`, `AuthSvc` and `AuthCookieSecure` (3.2), swap `StaticFS` for `StaticDir` (3.3), add `MaxSessions` (3.4). `buildAuth()` (main.go:**149-185**) is round two; nothing here calls it. Treat `reference/multissh/cmd/` as **read-only source material through the end of Phase 4**. Deleting it is a late follow-up (see §4 Follow-ups), not a task here.

**4.2 — Port `run()`'s setup half.** Reproduce `reference/multissh/cmd/multissh/main.go:63-96` inside `Build` (the setup/wiring seam sits around :90-96; the `server.DistFS()` call and its warning are at **:92-95** and are dropped), substituting `config.MultisshConfig` for the reference `config.Config`: `ssh_dir` → `sshproxy.DefaultSSHDir()` when empty; `upload_dir` → `filepath.Join(os.TempDir(), "multissh-uploads")`; `browse_root` → resolved `upload_dir`; `known_hosts_path` → `filepath.Join(sshDir, "known_hosts")`. `MaxSessions` is taken as-is from config (already validated in 1.2b). `MkdirAll` the upload dir and `hosts_path`'s parent. **Drop** from the port: `config.Load`/`envOr`, `server.DistFS()` and its warning, `cfg.Addr`, the `http.Server`, the `ListenAndServe` goroutine, and all signal handling — `Build` returns a handler and starts nothing.

**4.3 — Port `run()`'s wiring half.** Reproduce main.go:96-144: `sshproxy.HostKeyCallback(cfg.StrictHostKey, knownHosts)` (fail closed — see the note below), `sftp := sshproxy.SFTPTransferrer{HostKeyCallback: hostKeyCB}` passed as both `Transferrer` and `RemoteLister`, `sshproxy.NewHandler(sshproxy.SSHDialer{HostKeyCallback: hostKeyCB}, sshDir)`, then `server.New(server.Options{...})` with `StaticDir` (3.3) and `MaxSessions` (3.4) replacing `StaticFS`. Return `srv.Handler()`.

**`buildAuth` is not ported.** `server.New(server.Options{…})` is constructed without `Auth`, `AuthSvc` or `AuthCookieSecure` — 3.2 removed them from the struct, so the compiler enforces this.

**4.4** Wire into `/opt/unified-webapp/cmd/server/main.go`: add the `cmd184psu/unified-webapp/internal/multissh` import and `case "multissh": return multissh.Build(cfg.Multissh)` in `buildModule`.

**4.5 — Memoize `buildModule` per module name (shared-state bug).** `main.go` loops `for host, module := range cfg.Routing` and calls `buildModule(module, cfg)` **once per hostname**. Two `host_routing` entries pointing at `"multissh"` therefore construct **two independent `Server` instances** over the same `hosts_path` and `upload_dir`, with divergent in-memory state: the host store caches at construction, and the upload registry, broadcast job registry, and in-memory passwords are all per-instance. Symptoms: an upload staged via hostname A 404s when broadcast via hostname B; concurrent `PUT /api/hosts` from the two instances clobber each other's presets; a password entered on one is invisible to the other.

Fix in `cmd/server/main.go`: hold a `map[string]http.Handler` keyed by module name outside the routing loop; on a hit, register the cached handler for the additional hostname instead of rebuilding. This is a **general** dispatcher correctness fix — it applies to all six modules, and it is one of the two behavioral changes to shared code this work makes (the other is 4.7's 503-on-build-failure). Permitted under §6.8, which lists `cmd/server/main.go`.

Test: a config with two `host_routing` entries mapping to `"multissh"` produces **one** `Build` call and one `hostStore`; a host written through hostname A is visible through hostname B without a restart.

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

**4.8** Verify the WebSocket same-origin check (FR-S5, FR-I5) works behind the dispatcher: the dispatcher routes on `r.Host` and does not rewrite it, and `middleware.Wrap` does not wrap the `ResponseWriter` (so `http.Hijacker` survives the upgrade — confirmed in 0.2). Add a handler test asserting: matching Origin → accepted, absent Origin → accepted, foreign Origin → rejected, all with a `Host` header matching a routed multissh hostname.

**Verify Phase 4:** `make build`; `CGO_ENABLED=0 go build ./cmd/server` and `make build-rpi` (NFR-1); `go test -race ./internal/multissh/... ./cmd/... ./internal/platform/...`; start with a config containing `"multissh-test.cmdhome.net": "multissh"` and `curl -H 'Host: multissh-test.cmdhome.net' localhost:8080/api/config` → response **contains `"maxSessions":3`** (assert on containment, not byte equality, so the check stays true if the payload gains further fields); the other five module hostnames still respond (AC-3, NFR-5). Commit checkpoint on `dev`.

---

### Phase 5 — Frontend port + redesign (FR-I4, FR-N1..N5)

**5.1** Add `@xterm/xterm` and `@xterm/addon-fit` to `dependencies` in `/opt/unified-webapp/package.json`; `npm install`; commit the lockfile change.

**5.2** Copy `reference/multissh/web/src/*.ts` **and `ssh.css`** into `/opt/unified-webapp/web/multissh/js/` (both — see the CSS decision in 5.3). **Only `web/src/*.ts` + `ssh.css` move.** Explicitly **not** copied: `reference/multissh/web/vite.config.ts` (the Vite toolchain is rejected per Option D / D2 — one build pipeline), and the reference's **checked-in `node_modules/`** (unified resolves `@xterm/*` through its own root `package.json` and lockfile per 5.1; vendoring a second copy would shadow it and defeat the lockfile). Verify after the copy that neither path exists under `web/multissh/`. Create `/opt/unified-webapp/web/multissh/index.html` from `reference/multissh/web/index.html`, replacing the Vite `/src/main.ts` module script with the esbuild output path and linking the built CSS.

**5.3** Add the bundled build to the root `package.json` scripts — append to both `build` and `build:dev`:
`esbuild web/multissh/js/main.ts --bundle --target=es2020 --outfile=web/multissh/js/bundle.js` (add `--sourcemap` for the dev variant).

**CSS strategy — single decision, no alternatives.** esbuild emits the bundled CSS; keep the `import "./ssh.css"` in `main.ts`. The "drop the CSS import and link stylesheets from `index.html` instead" alternative floated in v1 **is not viable**: `terminal.ts` imports `@xterm/xterm/css/xterm.css` from `node_modules`, which has no stable servable path under `web/`, so at least one CSS import must go through the bundler regardless — and splitting the two stylesheets across two mechanisms is strictly worse than routing both through one. Consequence: `--outfile=web/multissh/js/bundle.js` causes esbuild to emit `web/multissh/js/bundle.css` **alongside the JS, not under `css/`**. Reconcile this explicitly: `index.html` links `js/bundle.css` (not `css/bundle.css`), and the hand-copied `ssh.css` source lives at `web/multissh/js/ssh.css` next to the TS that imports it — there is no `web/multissh/css/` directory. Apply the identical output layout in **both** `build` and `build:dev` so a dev build never produces a differently-located stylesheet.

**Output format — verified, no flag needed.** `esbuild --bundle` with `--outfile` defaults to **IIFE**, which is only a problem if the entry needs ESM semantics. Checked: `reference/multissh/web/src/main.ts` has **no top-level `await`** — its only async work is inside `bootstrap()`, so IIFE is correct. Therefore: **plain `<script src="js/bundle.js"></script>` in `index.html`, no `type="module"`, no `--format=esm`.** If a later edit introduces top-level `await`, esbuild fails the build with an explicit "Top-level await is currently not supported with the iife output format" error — at which point add `--format=esm` **and** `type="module"` together; they are a matched pair and changing one alone yields a silently non-executing page.

**Commit the built artifacts — yes, matching repo convention.** `git ls-files web/obsidianoid/js` shows **both** `app.ts`/`threads.ts` **and** the built `app.js`/`threads.js` are tracked. multissh follows suit: **commit `web/multissh/js/bundle.js` and `web/multissh/js/bundle.css`** alongside the sources. Consequence to accept knowingly: the bundle is a large generated blob (xterm is inlined) that will produce noisy diffs and can go stale relative to the `.ts` if someone edits sources without rerunning `npm run build`. Consistency with the existing five modules wins over diff hygiene here — a lone untracked module would break `make build`-free deploys that the other modules currently support. Rebuild and include the bundles in the Phase 5 commit checkpoint.

**5.3b — Extend typecheck coverage (`tsconfig.json` is an allowlist).** The root `tsconfig.json` `include` is an explicit list — `["web/obsidianoid/js/*.ts", "web/slideshow/js/*.ts"]` — with no wildcard over `web/`. Left alone, `npm run typecheck` would pass while never reading a single multissh file: a **false green**, and AC-2 would be satisfied vacuously. Imperative: **add `"web/multissh/js/*.ts"` to the `include` array.**

**First, though — the ambient CSS declaration, or this task fails on step one (v3 addition).** `tsc` has no notion of esbuild's CSS loader. The moment `web/multissh/js/*.ts` enters the `include` list, typecheck errors **TS2307 "Cannot find module"** on the two CSS imports the bundler handles fine: `main.ts:3` `import "./ssh.css"` and `terminal.ts:7` `import "@xterm/xterm/css/xterm.css"`. Create **`/opt/unified-webapp/web/multissh/js/css.d.ts`** containing `declare module "*.css";` (it is matched by the same `web/multissh/js/*.ts` glob — `.d.ts` files end in `.ts`). **Sequence this before the deliberate-type-error probe below**, otherwise the probe "fails" for the wrong reason and proves nothing about coverage.

Then prove the coverage rather than assuming it — introduce a deliberate type error into `web/multissh/js/hosts.ts` (e.g. assign a `number` to a `string`), run `npm run typecheck`, confirm it **fails and names that file**, then revert the error. Do not mark this task done on a passing typecheck alone.

**5.4 (FR-N1 — frontend only).** Replace the `HOST_COUNT = 3` constants in `hosts.ts:6` and `ui.ts:5` with the `maxSessions` value read from **`GET /api/config`** (the server side shipped in task 3.6b — **no Go changes in this phase**). Make the host rail and terminal panel grid CSS wrap/stack so N=5..8 stays usable (FR-N1's layout clause); update the hard-coded "up to 3 hosts" copy at `hosts.ts:87` to interpolate N.

*What is actually required.* v7 claimed `hosts.ts:90` is module scope and prescribed deferring rail construction into an async bootstrap. That premise was false and v8 refuted it: `hosts.ts:90` is inside `mountHostRail()` (`hosts.ts:80-95`). Under v9 the bootstrap is simpler still — 5.2 replaces `runAuthGate(root, mountTabs)` with a direct mount, so `main.ts` owns the one `await` this needs:

```ts
async function bootstrap(): Promise<void> {
  const root = document.getElementById("ssh-app");
  if (!root) { throw new Error("missing #ssh-app root element"); }
  const cfg = await fetchConfig();          // GET /api/config
  mountTabs(root, cfg.maxSessions);
}
void bootstrap();
```

Add `fetchConfig()` to `api.ts` alongside the existing helpers, thread `maxSessions` through `mountTabs` → `mountHostRail`, and have `ui.ts` take N as a parameter instead of reading a module constant. **Do not reuse `fetchAuthMode` (`api.ts:181-185`)** — it is typed `Promise<string>` and returns `body.mode ?? "none"`, discarding every other field; that discarding is what blocked `maxSessions` for four iterations. It is dead code after 5.2 and is left in place only because esbuild tree-shakes it (5.3).

**On failure:** if `GET /api/config` rejects or returns a non-number, use `3` and log to the console — the same default `hosts.ts:6` holds today, moved one layer out. Without it a transient boot failure renders `NaN` panels.

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

**6.2** Port `reference/multissh/docs/USER_GUIDE.md` into unified's docs convention (e.g. `docs/multissh.md` or a README section), updated for: JSON-only config, no env vars, no TLS in-module, `max_sessions`, collapsible panels, blast history, `key:ctrl+c`, and password auth. Include explicit notes that:

- passwords are memory-only and vanish on restart;
- `strict_host_key: true` with a bad `known_hosts` (or a missing `static_dir`) makes **multissh alone** fail to build and serve **503 on every route** while the other five modules keep running (4.7) — so a healthy-looking process is not proof multissh came up; check the boot log or the 503 body;
- the proxy must **preserve the `Host` header** on the multissh vhost, or every WebSocket upgrade fails the reference's Origin-vs-`Host` equality check (`proxy.go:243-245`) and no terminal connects (R1);
- there is **no login in this round** — anyone who can reach the multissh hostname can open a shell on every configured host. One line in the docs; auth lands in round two.


**6.3** Confirm `unified-webapp-example.json` (1.5) matches the final field set and that `make init-config` output round-trips through `config.Load` unchanged.

**6.4** Execute the manual acceptance pass against a reachable test SSH host, recording pass/fail per criterion. **Capture the server log for the grep-based criteria** by starting the binary as `./unified-webapp -config /tmp/ac.json > /tmp/multissh-ac.log 2>&1` (the binary logs via the stdlib `log` package to stderr; both streams are redirected so nothing escapes the capture). AC-10's grep runs against `/tmp/multissh-ac.log` and the `hosts_path` file.

**AC-6 is not executed** — it gates on an LDAP login, which is round two. Record it as **deferred, not failed**; accepted as such on 2026-09-10.

- **AC-4** — connect a terminal, type interactively, broadcast to 2+ connected hosts, pause one panel and confirm it ignores the broadcast while direct typing still works, press Ctrl-C. **Run this through the real nginx/HAProxy front end, not `localhost:8080`** — a handler test with a synthetic `Host` cannot observe what the proxy does to it (R1), and a WebSocket that works locally and fails in production is the likeliest way this port ships broken.
- **AC-5** — upload a file, broadcast to 2+ targets with independent progress rows; re-broadcast a server-resident file via `filePath` without re-uploading; confirm 413 on oversize (partial file removed) and 400 on an out-of-sandbox path.
- **AC-7** (restored to the FRD's original "module fails to build" wording, per 4.7) — `strict_host_key: true` with a missing `known_hosts` → **the multissh module fails to build**: every multissh hostname returns **503** with the missing `known_hosts` path named in the body, the boot log carries the same path clearly enough to diagnose unaided, and **the other five modules serve normally**. With a mismatched key → connect rejected.
- **AC-8** — `max_sessions: 5` → five host cards and five panels; a 5-target broadcast succeeds; collapse/expand does not drop sessions. Confirm in browser devtools that `GET /api/config` is the request the rail sizes itself from (3.6b/5.4).
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
| **Unit** | Ported `sshproxy` suites; credential exactly-one-of and redaction across all three password-carrying structs; strict-host-key missing-file error path; config round-trip, defaults and `~` expansion. |
| **Integration** | Ported server handler suites; `max_sessions` accept/reject at the configured bound; password absent from `hosts_path` (2.4c; the temp-file assertion v6 listed here is **dropped** — no task specifies it); `GET /api/config` returns the configured `maxSessions` (3.6b); path-traversal rejection on key names, `filePath` broadcasts and the file browser; WS connect with a password credential; `static_dir` four-row fallback table; **4.8** WebSocket upgrade with matching, absent and foreign `Origin` through the dispatcher; **4.5** two hostnames → one `Build`; **4.7** one module 503s while five serve. |
| **E2E** | Manual and labelled as such (6.4): AC-4..AC-10 plus NFR-2/3/4, executed **through the real nginx/HAProxy front end** rather than `localhost:8080` — a Go handler test with a synthetic `Host` cannot observe what the proxy does to `Host`, which is precisely R1. Frontend redesign items (collapse, history, `key:ctrl+c`, password entry, copy-ssh-command) are gated at Verify Phase 5, not deferred into the acceptance pass. |
| **Observability** | The reference logs a handful of failure-side lines via bare `log.Printf` and nothing in unified captures them — yet FR-A7 routes all client-facing error detail there and AC-10 greps it. Required: **(a)** audit lines on terminal connect/disconnect and on each broadcast target start/finish, carrying timestamp, target host/user and outcome — a browser-driven SSH executor with no record of which host was reached is not operable; **(b)** a test asserting audit output contains **no** credential field, alongside the redaction tests; **(c)** a **named sink** — `unified.service` exists, so stdout lands in journald; document the exact `journalctl -u unified` invocation AC-10's grep depends on, without which AC-10 is not executable as written; **(d)** per-risk detection signals — R1: successful WS upgrades at zero while origin rejections are non-zero; R4: the redaction tests plus the AC-10 grep. |

**Known coverage gap:** R1 is only falsifiable against the real proxy, so the 4.8 handler test is necessary but not sufficient — AC-4's manual pass through nginx is the actual verification.

---

## 3. Risks & Mitigations

| # | Risk | Impact | Mitigation |
|---|------|--------|-----------|
| R1 | **WebSocket same-origin behind the proxy.** HAProxy/nginx may rewrite `Host`, breaking the reference's Origin-vs-Host equality check → all terminals fail to connect in production while passing locally. | High | **Task 4.8** (v3: v2 cited 4.5, which is the dispatcher memoization task — the Origin/Host test through the dispatcher is 4.8) tests the check through the dispatcher. Log the observed `Origin`/`Host` pair on rejection so a proxy misconfiguration is diagnosable from one log line. Document the required `Host`-preserving proxy config in 6.2. Scope: a `Host`-rewriting proxy breaks the multissh WebSocket only, exactly as in the reference app. |
| R2 | **xterm bundling.** First `--bundle` module in the repo; CSS imports, ESM/CJS interop, and `tsc` include coverage can all break the root build script for the other modules. | Med | Add the multissh esbuild call as a **separate** invocation appended to the existing `build` script (5.3) so a failure cannot regress the bundleless modules. Verify `npm run build` rebuilds obsidianoid and slideshow correctly in the same run. |
| R3 | **N-session UI layout.** The reference grid assumes exactly 3 panels; at N=8 terminals may become unreadable slivers. | Med | FR-N1 only requires "usable" — use a wrapping flex/grid with a minimum panel height and vertical scroll, and lean on FR-N2 collapse as the pressure valve. Test at N=1, 3, and 8. |
| R4 | **In-memory password lifecycle.** A password leaking into `hosts_path`, a log line, an error string, or a `%+v` dump silently breaks AC-10 — and only a targeted test would catch it. | High | Redacting `String`/`GoString`/`MarshalJSON` on the credential type (2.3) makes leakage structurally hard rather than review-dependent. **v3: the persistence half is now structural, not a strip step** — the password field exists only on the `PUT /api/hosts` *request* DTO; the persisted `hostConfig` has no such field, and `normalizeHostRequests`' field-by-field reconstruction drops it by construction (2.3c, 3.4 — v4 splits the reference's `normalizeHosts` into a request-side and a config-side function so the load path never sees a `Secret`-bearing type). This also removes a v2 defect that would have *caused* the risk it meant to prevent: `Secret` is a struct, so `omitempty` is inert on it, and a shared DTO would have written `"password":"***"` to disk — which unmarshals back as a literal password. Type-level and file-level tests in 2.4c; explicit tests in 2.4 and 3.7; grep verification in 6.4. |
| R5 | **go.mod dependency merge.** The reference adds `golang.org/x/crypto` (ssh, sftp) to unified's tree; a version conflict or a CGO-requiring transitive dep would break `CGO_ENABLED=0` and the arm64 cross-build. | **Low** (was Med) | **v9: the large half of this risk left with auth.** go-ldap and go-webauthn pulled go-tpm, cbor and msgp; none of them enter `go.mod` now (§0). What remains is `x/crypto` + `pkg/sftp`, both pure Go. Still do the merge first (0.3) so any conflict surfaces before porting effort is sunk, and verify `CGO_ENABLED=0 go build` and `make build-rpi` at both Phase 0 and Phase 4. |
| R6 | **Race-detector cleanliness of ported concurrency.** The broadcast registry, per-target transfer goroutines, and the WebSocket bridge are the highest-risk code; unified runs `-race` across the whole repo, so a latent race becomes a repo-wide red build. | Med | Port the tests before adapting the code (2.2, 3.6) so any race is attributable to the adaptation, not the port. Run `go test -race -count=5` on `./internal/multissh/...` at the end of Phases 2 and 3 to shake out flakes. |
| R7 | **Reference config shape drift.** Renaming the JSON tags silently invalidates existing standalone-multissh operator configs. | Low | **Resolved to snake_case throughout** (see the note under 1.1). Existing operator configs are not loadable verbatim under *any* variant — every outer field is renamed because FR-I3 names the snake_case forms — so the choice costs nothing extra. Migration is a documented one-time config rewrite (1.5, README). |
| R8 | **Per-hostname module duplication.** `buildModule` runs once per `host_routing` entry, so two hostnames for `"multissh"` yield two `Server`s sharing `hosts_path`/`upload_dir` with independent caches and registries → cross-host upload 404s, preset clobbering, invisible passwords. Silent until someone adds a second hostname, possibly long after ship. | High | Memoize by module name in `cmd/server/main.go` (4.5) plus a two-hostname/one-`hostStore` test. Fixing it in the dispatcher rather than in multissh also inoculates the other five modules. |
| R9 | **Cross-origin writes reach multissh.** `middleware.Wrap` sets `Access-Control-Allow-Origin: *`; `POST /api/broadcast` sent as `Content-Type: text/plain` is a CORS simple request, so no preflight fires and `handleBroadcastPost` (`broadcast.go:195-213`) checks neither `Origin` nor `Content-Type`. | **Round two — accepted for round one** | Not closed here; the operator accepted it as in-scope-for-later on 2026-09-10. The fix is `shelved/plan-with-platform-auth.md` task **4.6b** — same-origin enforcement on non-`GET`/`HEAD` requests, ~30 lines and one test, reusing the `sameOrigin` helper at `proxy.go:243-245`. Listed so round two has it ready-scoped. |

---

## 4. ADR — Port-and-adapt multissh as unified's sixth module

**Decision.** Port the reference Go packages into `internal/multissh/{,sshproxy}` — **`auth` excluded (§0)** — with mechanical import rewrites plus targeted adaptation (config source, static serving, `MaxSessions`, password credentials), and port the TypeScript SPA into `web/multissh/` built by root-level `esbuild --bundle`. Reject both a from-scratch Go rewrite and reuse of the prebuilt Vite `dist/` bundle.

**Drivers.** (D1) Ported tests are the acceptance gate and the only affordable correctness signal for the concurrency-heavy code. (D2) A single build pipeline — `make` + root esbuild — is a standing constraint of the monolith. (D3) The approved redesign is overwhelmingly frontend work, so Go effort should go to reuse, not reconstruction.

**Alternatives considered.**
- *Rewrite the Go module against unified idioms* — cleaner long-term fit, but discards the reference suites that FR-I7 and AC-1 explicitly require, and risks regressing PTY framing, origin checks, and partial-upload cleanup.
- *Ship the prebuilt Vite `dist/`* — zero build work, but minified and hash-named, so FR-N1..N5 are literally un-implementable against it, with no `tsc` coverage and a second toolchain implied for any future change.
- *Port `auth` too, as `internal/platform/auth` serving all six modules (the v1–v8 choice)* — satisfies FR-A3..A5 and AC-6. **Deferred to round two, not refuted:** the design is sound and fully worked in `shelved/plan-with-platform-auth.md`. Splitting it out keeps this round's diff to the port itself.
- *Retain the `MULTISSH_*` env layer* — familiar to existing operators, but no other unified module has an env layer; two config sources is a support burden for a single-operator deployment. FR-I3 permits dropping it.

**Why chosen.** Port-and-adapt-minus-`auth` is the only option that satisfies FR-I7 as written while keeping the diff reviewable and the schedule bounded. The frontend must be source-ported regardless because the redesign demands it, so the esbuild bundle is forced by FR-I4 rather than chosen freely. Together these hold the module inside every existing unified convention — one binary, one port, one config file, one build.

**Consequences.**
- unified's `go.mod` gains `golang.org/x/crypto`, `pkg/sftp` and `gorilla/websocket`, all pure Go. Round two adds the LDAP/WebAuthn subtree.
- unified now has two frontend build modes (bundleless and bundled), so the root `build` script needs a comment explaining why multissh differs.
- The reference's in-memory upload registry is inherited: staged files survive restart but their IDs do not (accepted, §6).
- `internal/multissh/` will carry structural traces of a standalone app — a wide `Options` struct, a `Handler()` indirection that no longer wraps anything (3.2 keeps it deliberately, as the seam the shelved auth plan re-attaches to) — that a later refactor may want to align with unified's platform packages.
- **A failed module degrades to 503 instead of killing the binary** (v3, revised from v2). `buildModule` errors — a missing `known_hosts` under `strict_host_key: true`, a missing `static_dir` — no longer `log.Fatalf`. The module's entry in the 4.5 memoization map becomes a 503 handler naming the error, so multissh serves nothing while grocery, todo, obsidianoid and slideshow keep running (4.7). Still fail-closed under P5 — multissh provides zero SSH functionality in this state, so nothing degrades to unverified host keys; only the blast radius changed, from six modules to one. Cost: a misconfiguration is now quiet enough to overlook, so the boot log line and the 503 body must both name the offending path. This restores AC-7 to the FRD's original "module fails to build" wording.
- **The dispatcher gains a module-handler cache** (4.5). One handler now serves all hostnames mapped to a module, which is what shared on-disk state requires, but it means modules can no longer assume per-hostname isolation. No current module relies on that assumption.
- `internal/platform/middleware.Wrap` is untouched, so the existing five modules see no header or policy change. R9 is the open item this leaves for round two.
- **`GET /api/config` is a new route with no counterpart in the reference** (P2 deviation, 3.6b). FR-N1 requires the SPA to know `max_sessions` before it draws the rail, and the reference had nowhere to put it. Additive, read-only, ~8 lines.
- `reference/multissh/` — including `cmd/multissh/`, which is the specification for `Build()` — is retained **read-only** through Phase 4 and removed only in a follow-up after Phase 6 passes.

**Follow-ups (not in this plan).**
- **Round two — auth, passkeys, TLS, origin policy.** Designed in `shelved/plan-with-platform-auth.md`; start from its tasks **3.1c** (the `auth.Gate` type), **4.9** (the public/private prefix policy) and **4.6b** (R9).
- Extend `key:` keywords beyond `ctrl+c` (`ctrl+d`, `ctrl+z`) once the mechanism proves out.
- Persist the upload registry so IDs survive restart.
- Delete `reference/multissh/` (including `cmd/multissh/`, kept read-only as the `Build()` specification) once Phase 6 has passed and the module has run in production.

### Process note — carryover is this document's dominant failure mode

Three consecutive iterations have had "a task section was fixed; §3/§4/§5/§6 still describe the superseded design" as their largest finding class. v6 swept the ADR successfully and missed R9, §6.2 and both NFR-5 lists. **Before any future revision is submitted for review, grep the whole document for the phrases the revision changed** — for this round the sweep was `O-1|O-2|O-3|D-A|auth|passkey|LDAP|secure_mode|protected_modules|CORS|origin|4.6|4.9|3.1b|3.1c|1.2c|0.2b|3.8` — and diff every task-section claim against its restatement in §3, §4, §5 and §6.8. Each task edit should carry the list of downstream sections it invalidates.

**v9 note.** Splitting auth into round two removed about a third of this document — the largest carryover hazard it has faced. §2A, §3, §4, §5 and §6 were each swept against the task edits, and §6.4's acceptance list had to be restored after an over-broad excision. Treat any surviving `O-1`/`O-2`/`O-3`/`secure_mode`/`protected_modules`/`auth.Gate` reference as a defect; that design lives in `shelved/`.

**v8 adds a second rule, because grepping was not sufficient.** The v7 sweep was clean by its own standard and still shipped a task (3.2) that had mis-described `server.go` since **v4** — wrong line numbers, wrong count of fields, and a claim that `Handler()` mounts routes it does not mount. It survived four review rounds because a phrase-grep only looks at text the *revision* touched, and nobody re-read the file. So: **when a task cites specific lines of a reference file, re-read those lines against source in the revision that touches the task — do not carry the citation forward on trust.** The plan holds prose copies of things that live in source, and prose copies drift silently.

---

## 5. Traceability — every FRD item to its task(s)

Added in v3 so coverage is checkable rather than asserted. "**parity**" = verbatim port plus the reference's own ported suite; the port task and the test-port task together are the coverage, and no new behavior is introduced. Every row must have at least one task ID and at least one verification.

### Integration (FR-I)

| Req | Tasks | Verified by |
|---|---|---|
| FR-I1 Module registration | 4.4, 4.5 | Phase 4 verify (curl on routed hostname); 4.5 two-hostname test |
| FR-I2 Package layout | 2.1, 3.2 | `go build ./...`; Phase 2/3 verify |
| FR-I3 Config section | 1.1, 1.2, 1.2b, 1.3, 1.5, 1.6 | 1.4 config tests; Phase 1 round-trip verify; 6.3 |
| FR-I4 Frontend via esbuild | 5.1, 5.2, 5.3, 5.3b | `npm run build` + `npm run typecheck` (coverage proven per 5.3b probe) |
| FR-I5 Routing under one origin | 4.4, 4.8 | 4.8 Origin/Host dispatcher test |
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
| FR-S5 Same-origin WebSockets | **parity** (2.1) + 4.8 | 4.8 (match/absent/foreign Origin). *Scope note:* FR-S5 is a WebSocket requirement and is met at parity. The reference's **HTTP** API has no origin check at all — see R9, accepted not mitigated |

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
| FR-N1 `max_sessions` | 1.2, 1.2b, 3.4, 3.6b, 5.4 | 3.7 + 3.6b (`GET /api/config` returns N); **AC-8**; R3 layout test at N=1/3/8 |
| FR-N2 Collapsible panels | 5.5 | AC-8 (collapse/expand drops no session) |
| FR-N3 Blast history | 5.6 | **AC-9** |
| FR-N4 In-memory password auth | 2.3, 2.3b, 2.3c, 2.4, 2.4b, 2.4c, 3.5, 5.8 | **AC-10** (connect, grep, restart); 2.4b lifetime bound |
| FR-N5 `key:` keywords | 5.7 | AC-9 (`key:ctrl+c` interrupts all non-paused) |

### Auth & security (FR-A)

| Req | Tasks | Verified by |
|---|---|---|
| FR-A1 `strict_host_key` fail closed | 2.5, 4.3, 4.7 | 2.5 missing-file error path; **AC-7** (503, other modules up) — note FR-A1's own wording is "missing/unreadable file = **module** build error", which 4.7's v3 revision now matches literally |
| FR-A2 Encrypted keys unsupported | 2.5 (**parity** 2.1) | ported `keys_test` |
| FR-A3 `secure_mode` gates data routes | **round two** — shelved tasks 4.9, 3.1c, 3.1, 1.2c | AC-6 |
| FR-A4 LDAP login, groups, TTLs, cookie flags | **round two** — shelved tasks 3.1, 3.8, 4.9 | AC-6 |
| FR-A5 Passkeys | **round two** — shelved tasks 3.1, 3.8, 1.2c, 4.9 | — |
| FR-A6 Path traversal blocked | **parity** (2.1, 3.2) | ported `keys_test`, `files_test`, `upload_test`; AC-5 |
| FR-A7 Generic client errors | **parity** (2.1, 3.2) + 3.3 (JSON 404 on `/api/*` miss) | ported suites; 3.3 four-row table test |

### Non-functional (NFR)

| Req | Tasks | Verified by |
|---|---|---|
| NFR-1 One binary, one build, arm64 + `CGO_ENABLED=0` | 0.3, 5.3 | `make build`, `make build-rpi`, `CGO_ENABLED=0 go build` (Phase 4 + 6 verify) |
| NFR-2 Inert until first request | 4.2 (drops listener/signal handling) | **6.1b** (goleak, per v3 — not a `NumGoroutine` diff) |
| NFR-3 Sessions survive tab switches | **parity** (frontend) | 6.4 NFR-3 manual check |
| NFR-4 Streaming uploads, no full-file buffering | **parity** (3.2 — `r.MultipartReader()` + `io.Copy`, upload.go:94/129) | **6.4 NFR-4** (v3 addition): structural no-`ParseMultipartForm` assertion + 2 GiB bounded-RSS manual check |
| NFR-5 Existing five modules unchanged | 4.5, 4.7, 5.3 (separate esbuild call) | 6.1 full `go test -race ./...`; Phase 4 verify (other five hostnames respond). *Accepted deviations, both dispatcher-level:* modules are no longer per-hostname isolated (4.5 — a bug fix for them as much as for multissh); a `buildModule` failure now 503s that module instead of `log.Fatalf`-ing the process (4.7 — strictly better for the other five). `middleware.Wrap` is untouched and `GET /api/config` is mounted by `internal/multissh` alone, so no header or route of the other five changes. |

### Acceptance criteria

| AC | Tasks | Where run |
|---|---|---|
| AC-1 tests green | all port tasks | 6.1 |
| AC-2 typecheck green | 5.3b (+ css.d.ts) | Phase 5 verify |
| AC-3 other modules unaffected | 4.5 | Phase 4 verify, 6.1 |
| AC-4 terminals/broadcast/pause/Ctrl-C | parity + 5.5 | 6.4 |
| AC-5 upload/broadcast/413/sandbox | parity | 6.4 |
| AC-6 secure_mode + LDAP | **round two** | not run this round; record as deferred, not failed |
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
8. **Changes to the existing five modules.** Behavior must be untouched (NFR-5). Shared files this work may modify: `cmd/server/main.go` (4.5, 4.7), `internal/platform/config` (the `multissh` section only), `package.json`, `tsconfig.json` (5.3b), `README.md`, `unified-webapp-example.json`. `internal/platform/middleware` is **not** among them. Two accepted exceptions, both dispatcher-level and both recorded against NFR-5 in §5:
   - Modules are no longer per-hostname isolated (4.5). No current module relies on that, and for any module with shared on-disk state the current behavior is a latent bug.
   - `buildModule` failures yield a 503 module handler instead of `log.Fatalf` (4.7) — better availability for the other five, not worse.
9. **Auth, passkeys, TLS and origin policy — round two** (§0). Designed in `shelved/plan-with-platform-auth.md`; R9 is the item it closes.
10. **Pushing to any remote.** Commits on `dev` only (FR-I8).
11. **Persisting blast history, session layout, or collapse state across browser sessions.** In-memory only (FR-N3).
12. **Performance/scale work beyond NFR-2 and NFR-4.** No connection pooling, no transfer throttling, no resumable uploads.
