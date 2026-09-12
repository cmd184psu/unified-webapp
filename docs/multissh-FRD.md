# FRD — multissh module for unified-webapp

**Status:** Draft for planning (ralplan input)
**Date:** 2026-09-09
**Source of truth:** `reference/multissh/` (standalone project: code, tests, and `docs/USER_GUIDE.md`)

---

## 1. Goal

Meld the standalone **multissh** application into **unified-webapp** as a sixth module, following the exact pattern used by the existing five (grocery, todo, slideshow, menuserver, obsidianoid): one Go binary, one process, Host-header dispatch, per-module config section, per-module `internal/<module>` package exposing `Build(cfg) (http.Handler, error)`, and static assets served from `web/<module>`.

multissh gives a browser page that drives **N interactive SSH terminals** in parallel, **broadcasts ("blasts") typed commands** to all connected terminals, and **uploads a file once then fans it out over SFTP** to the configured hosts with live per-target progress.

This is mostly a port/integration with feature parity, plus a small approved redesign (§3.4): configurable N sessions instead of a hard-coded 3, collapsible session panels, blast-line history, a `key:ctrl+c` blast keyword, and in-memory-only password auth as an alternative to server-side keys.

TLS is **out of scope for the module and the binary in this deployment**: nginx or HAProxy terminates TLS in front of unified-webapp. The module itself serves plain HTTP on unified's shared port, selected by the `host_routing` keyword `multissh`. LDAP/passkey login remains strictly **optional and opt-in** (off by default).

## 2. Integration requirements (the "meld" pattern)

- **FR-I1 — Module registration.** Add `case "multissh"` to `buildModule` in `cmd/server/main.go`, returning `multissh.Build(cfg.Multissh)`. Requests reach the module only via `host_routing` entries (e.g. `multissh.cmdhome.net` / `multissh-test.cmdhome.net`).
- **FR-I2 — Package layout.** Port `reference/multissh/internal/{server,sshproxy,auth,config}` into `internal/multissh/` (subpackages allowed, e.g. `internal/multissh/sshproxy`). The reference's own `cmd/`, `go.mod`, and embed machinery are dropped; unified's module (`cmd184psu/unified-webapp`) owns the code.
- **FR-I3 — Config section.** Add `MultisshConfig` to `internal/platform/config`, keyed `"multissh"` in the JSON config, included in `WriteDefault`, and documented in `unified-webapp-example.json` and `README.md`. Fields (mirroring the reference config, minus `addr` which unified owns):
  - `static_dir` (like other modules; replaces the reference's embedded `dist/`)
  - `ssh_dir` (default: running user's `~/.ssh`)
  - `upload_dir` (default: `os.TempDir()/multissh-uploads`)
  - `hosts_path` (saved host presets JSON; default under `data_dir` or `multissh-hosts.json`)
  - `browse_root` (server-side file browser sandbox; default = resolved `upload_dir`)
  - `max_sessions` (int, default 3) — number of host cards / terminal panels / max broadcast targets (see §3.4)
  - `max_upload_bytes` (default 8 GiB)
  - `secure_mode` (bool, default false)
  - `strict_host_key` (bool, default false), `known_hosts_path` (default `<ssh_dir>/known_hosts`)
  - `auth` object (mode `none|ldap|ldap_passkey`, session/idle TTLs, cookie secure flag, passkey store path, LDAP settings, WebAuthn rpID/origins) — same shape as the reference `AuthConfig`.
  - The reference's `MULTISSH_*` environment-variable layer is **not required**; unified modules configure via the JSON file only (drop env handling unless trivially retained).
- **FR-I4 — Frontend.** Port the Vite/TypeScript SPA (`reference/multissh/web/src/*.ts`, xterm.js) to unified's frontend convention: sources under `web/multissh/`, compiled by esbuild via the root `package.json` `build` script. Because the app depends on `@xterm/xterm` + `@xterm/addon-fit`, the esbuild invocation for this module must use `--bundle` (unlike the existing bundleless modules); add the xterm packages to root `package.json` dependencies. `tsc --noEmit` typecheck must pass. Served as static files from `static_dir` (no Go embed).
- **FR-I5 — Routing under one origin.** All API paths stay as in the reference (`/api/...`) and are only reachable on the multissh-routed hostnames. WebSocket same-origin checks must work behind the unified dispatcher (Host header preserved by HAProxy).
- **FR-I6 — Dependencies.** Merge required Go deps into unified's `go.mod`: `golang.org/x/crypto/ssh`, `github.com/pkg/sftp` (or whatever the reference uses), LDAP and WebAuthn libraries used by `internal/auth`. Keep CGO_ENABLED=0 buildable.
- **FR-I7 — Tests.** Port the reference test suites (`config`, `server` handlers, `sshproxy`, broadcast, upload, sftp, hosts, files) so `make test` (`go test -race ./...`) passes.
- **FR-I8 — No push.** All work on branch `dev`; commits allowed, never push.

## 3. Functional requirements (feature parity)

### 3.1 Host management
- **FR-H1.** UI shows a persistent left "host rail" of up to **`max_sessions` host cards**: IP/hostname, user, port (default 22), auth method (SSH key picked by name from the server's `ssh_dir`, **or** in-memory password — see FR-N4), remote directory (default `/tmp`).
- **FR-H2.** `GET /api/ssh/keys` lists non-hidden file names in `ssh_dir` (names only; keys never leave the server; path traversal blocked).
- **FR-H3.** Host presets persist server-side at `hosts_path` via `GET/PUT /api/hosts` (≤`max_sessions` hosts, safe fields only — never key material **and never passwords**; atomic write).
- **FR-H4.** Remote-directory picker: `POST /api/sftp/listdir` lists a directory on a target live over SFTP (502 with generic message on failure).
- **FR-H5.** Each card offers a "Copy ssh command" button producing `ssh [-i ~/.ssh/<key>] [-p <port>] user@host` (flags omitted for defaults; disabled with hint when host/user unset).

### 3.2 SSH consoles
- **FR-S1.** Up to `max_sessions` independent xterm.js terminal panels; each opens its own WebSocket (`GET /api/ssh/ws`) bridged to its own SSH PTY session; keystrokes/output as binary frames, control messages as JSON text frames.
- **FR-S2.** Per-panel status (`disconnected/connecting/connected/error: <reason>`), Connect/Disconnect, Pause (ignores broadcast but still allows direct typing), and Ctrl-C button (sends `^C`, works while paused, enabled only when connected).
- **FR-S3.** Broadcast bar sends the typed line + newline to every **connected, non-paused** panel.
- **FR-S4.** Connect failures show generic `connection failed: check host, user, and key`; full detail goes to the server log. Failed connects leave the panel usable for retry.
- **FR-S5.** WebSockets accept same-origin requests only (or no Origin header).

### 3.3 Upload & SFTP broadcast
- **FR-U1.** `POST /api/upload` streams multi-GB `multipart/form-data` uploads straight to disk under `upload_dir`; rejects over `max_upload_bytes` with 413 and removes the partial file. `GET /api/uploads` lists the in-memory registry; `DELETE /api/uploads/{id}` removes one.
- **FR-U2.** Server-resident source: `GET /api/files?path=<rel>` browses a sandbox rooted at `browse_root` (dirs first; escape attempts → 400).
- **FR-U3.** `POST /api/broadcast` takes exactly one of `uploadId` or `filePath` plus 1–`max_sessions` targets `{host,port,user,key|password,remoteDir}` → `{jobId}`; validation errors per the reference (400s, limits phrased against the configured max).
- **FR-U4.** Transfers run in parallel, one goroutine per target, over **SFTP** into each target's remote directory keeping the original filename; failures are isolated per target.
- **FR-U5.** `GET /api/broadcast/ws?job=<id>` streams per-target progress (`pending → transferring → done` or `error: <reason>`, then `complete`); UI shows a progress row per target with transferred/total.

### 3.4 Redesign items (new vs. reference)
- **FR-N1 — N sessions.** The hard-coded 3-session limit becomes `max_sessions` (config, default 3, must be ≥1). Host rail, terminal panels, broadcast target validation, and SFTP fan-out all honor it. UI layout must stay usable at larger N (panels wrap/stack).
- **FR-N2 — Collapsible sessions.** Each terminal panel can be collapsed/expanded individually. Collapsing hides the terminal viewport but does **not** disconnect the session or change its pause state; status stays visible on the collapsed header.
- **FR-N3 — Blast-line history.** The broadcast ("blast") input keeps a history of sent lines; Up/Down arrows walk it (shell-style). History is per browser session (in-memory; no persistence required).
- **FR-N4 — In-memory password auth.** A host may authenticate with an SSH **password instead of a key**. The password is entered in the UI, held **in memory only** on the server for the life of the session/job, used for both terminal connects and SFTP broadcasts, and **never** written to `hosts_path`, logs, or any file. Key-based auth remains the default; per host, exactly one of key or password is used.
- **FR-N5 — `key:` blast keywords.** A blast line of the form `key:ctrl+c` sends Ctrl-C (`^C`) to every connected, non-paused terminal instead of literal text. Design should make the keyword mechanism extensible (e.g. later `key:ctrl+d`), but only `key:ctrl+c` is required now.

### 3.5 Security & auth
- **FR-A1.** `strict_host_key: false` (default) skips host-key verification; `true` verifies against `known_hosts_path` for both terminals and SFTP, failing closed (missing/unreadable file = module build error; unknown/mismatched key = rejected connect).
- **FR-A2.** Encrypted (passphrase) private keys are unsupported: log a specific message, surface the generic connect failure.
- **FR-A3.** When `secure_mode: true` and `auth.mode` is `ldap`/`ldap_passkey`, all `/api/*` routes and WebSocket bridges require a valid session cookie (401 otherwise); `/api/auth/*` routes are exempt. When auth is off, only `GET /api/auth/mode` exists among auth routes and reports `{"mode":"none"}`.
- **FR-A4.** LDAP login (`POST /api/auth/login`/`logout`, `GET /api/auth/session`), optional `requiredGroups` restriction, session + idle TTLs, cookie Secure flag.
- **FR-A5.** `ldap_passkey` mode adds WebAuthn passkeys: register begin/finish (requires an existing login), login begin/finish, list, delete; passkeys persist at `auth.passkeyStorePath`.
- **FR-A6.** Path traversal blocked everywhere paths are accepted (key names, uploads, file browser, `filePath` broadcasts).
- **FR-A7.** Client-facing errors are generic; details go to the server log (unified's logging middleware/stdout).

## 4. Non-functional requirements

- **NFR-1.** Pure Go, `CGO_ENABLED=0`; `make build` and `make build-rpi` (linux/arm64) keep working.
- **NFR-2.** No new long-lived resource cost when the module is idle (consistent with the memory/CPU-saving motivation of unified-webapp); goroutines exist only per active terminal/transfer.
- **NFR-3.** Switching browser tabs never tears down live terminal sessions (SPA behavior preserved).
- **NFR-4.** Multi-GB uploads must stream (no full-file buffering in memory).
- **NFR-5.** Existing five modules are untouched behaviorally; all existing tests keep passing.

## 5. Known limitations (accepted, carried over)

- Max `max_sessions` hosts for consoles and broadcasts (configurable; default 3).
- Unencrypted private keys only (passwords supported per FR-N4, in-memory only).
- Upload registry is in-memory (staged files survive restart, IDs don't).
- Single LDAP directory; passkeys require HTTPS + matching rpID/origins.
- SFTP only (no scp).

## 6. Acceptance criteria

1. `make test` passes (including ported multissh tests) and `make build` produces one binary.
2. `npm run build` + `npm run typecheck` pass with the new `web/multissh` sources.
3. With a `host_routing` entry `"multissh-test.cmdhome.net": "multissh"`, browsing that host serves the multissh UI; the other five modules still serve on their hosts.
4. Against a reachable test SSH host: connect a terminal, type interactively, broadcast a command to 2+ connected hosts, pause one panel and confirm it ignores broadcast, send Ctrl-C.
5. Upload a file, broadcast it to 2+ targets, watch independent progress; re-broadcast a server-resident file without re-upload; verify 413 on oversize and 400 on out-of-sandbox path.
6. `secure_mode` off: no login required. On with `ldap`: unauthenticated `/api/*` → 401 and login page shown.
7. `strict_host_key` on with missing known_hosts → module fails to build with a clear error; with a mismatched key → connect rejected.
8. With `max_sessions: 5`, five host cards/panels appear and a 5-target broadcast succeeds; panels collapse/expand without dropping their sessions.
9. Blast line: Up-arrow recalls previously sent lines; sending `key:ctrl+c` interrupts every connected, non-paused terminal.
10. A host configured with password auth connects (terminal and SFTP); the password appears nowhere on disk (`hosts_path`, logs) and is gone after restart.
