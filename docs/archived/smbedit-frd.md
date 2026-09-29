# FRD: smbedit — port of smbed into unified-webapp

**Status:** Approved for planning (all decisions resolved — see §10)
**Date:** 2026-09-11
**Source of truth for the original app:** `reference/smbed/` (full smbed checkout, vendored in this repo for reference only — never imported, never built)

---

## 1. Summary

smbed is a standalone, self-contained web UI for managing Samba shares: a Go binary (chi router, embedded Vite/React frontend) that edits `~/.smbed.json`, renders `smb.conf`, writes it via sudo, and restarts `smbd`. This project ports it as a new module named **smbedit** inside unified-webapp, following the same integration pattern as the six existing modules (grocery, todo, slideshow, menuserver, obsidianoid, multissh): a `Build(cfg) (http.Handler, error)` package under `internal/smbedit/`, a static frontend under `web/smbedit/`, dispatch by `Host` header, configuration in `~/.unified-webapp.json`.

This is a **straight port**: identical feature set, identical REST API shape, identical UI pages and behavior. The only changes are those forced by the platform (no own listener, no embedded FS, no chi, config split described below) — no feature additions, no redesign.

## 2. Goals

- G1: smbedit is reachable via a hostname in `host_routing` (e.g. `smbedit.cmdhome.net` / `smbedit-test.cmdhome.net`) served by the single unified-webapp binary.
- G2: Every smbed feature works unchanged: Shares editor, Globals editor, folder picker, smb.conf Preview, Import of an existing smb.conf, Save & Restart, Settings, ops-log SSE stream, Samba daemon log SSE stream, dark/light/system theme.
- G3: The module obeys unified-webapp conventions: `Build()` fails loudly at boot on misconfiguration (dispatcher already turns that into a 503 handler), static frontend served from `static_dir` on disk, mutable state in a data directory, stdlib-only routing, tests pass under `make test` (`go test -race ./...`).
- G4: The repo builds with the existing toolchain: `make build` needs no npm (compiled frontend bundle is committed); `make web` regenerates it; `make typecheck` covers the new TS/TSX.

## 3. Non-goals

- No authentication/authorization. Consistent with the rest of the platform (see multissh): reaching the hostname is the access boundary. Worth restating in README because this module can rewrite `/etc/samba/smb.conf` and restart services.
- No macOS/Windows Samba operation. The module must **compile and unit-test** on darwin (the repo is developed on a Mac), but restart/sudo-write behavior is only expected to function on Linux.
- No new features: no multi-file smb.conf includes, no per-share owners, no user management, no validation of Samba option names.
- No standalone smbed binary in this repo. `reference/smbed/` stays untouched and out of the build (`reference/` gets a `.gitignore` entry — see D4).

## 4. Reference: what smbed is today

Backend (`reference/smbed/internal/`), ~900 LOC + tests:

| Package | Responsibility |
|---|---|
| `config` | `~/.smbed.json` load/save/defaults. Types: `Config`, `Share`, `GlobalEntry`. `DisableMissingPaths()` auto-disables shares whose path no longer exists. |
| `samba` | `smb.conf` text/template rendering; timestamped backup of the previous file; write with sudo fallback (`sudo install -m 0644`); `ParseConf`/`ReadConf` for import; `Restart()` via `sudo systemctl restart smbd` → `smb` → `sudo service smbd restart`; `Logf` hook for the ops log. |
| `oplog` | Bounded in-memory ring buffer (500 entries) with subscribe/fan-out for SSE. |
| `api` | chi router: REST endpoints (below), `/opt` folder picker, two SSE streams, embedded SPA serving with index.html fallback. |

REST API (all under `/api`, JSON unless noted):

| Endpoint | Behavior |
|---|---|
| `GET /api/config` | Full config object. |
| `PUT /api/config` | Partial patch: `listen_addr`, `smb_conf_path`, `samba_log_path`, `share_owner`, `theme`. Persists. |
| `GET/PUT /api/shares` | Whole-list replace. PUT runs `DisableMissingPaths` and ops-logs any auto-disabled names. Persists. |
| `GET/PUT /api/globals` | Ordered key/value list, whole-list replace. Persists. |
| `GET /api/folders` | Immediate non-hidden subdirectories of `/opt` (hardcoded root; max 1 level). |
| `POST /api/import` | Parse an smb.conf (body `{path}`, default = configured `smb_conf_path`), return `{globals, shares, share_owner}` **without saving** — client stages the result. |
| `GET /api/preview` | Rendered smb.conf as `text/plain`. |
| `POST /api/save-and-restart` | Backup + write smb.conf (sudo fallback), restart Samba; returns `{written, restart:{success,output}, path}`. |
| `GET /api/logs/ops/stream` | SSE: ops-log backlog then live tail. Event data: `{"time":…,"message":…}`. |
| `GET /api/logs/samba/stream` | SSE: `sudo tail -n 200 -F <samba_log_path>`, killed on client disconnect. 400 if no path configured. |
| `GET /api/version` | `{"version": …}`. |

Frontend (`reference/smbed/web/`): React 18 + TypeScript, built by Vite, embedded via `go:embed`. Components: `App` (nav + staged-edit model — edits accumulate client-side, one Save persists), `SharesPage`, `GlobalsPage`, `PreviewPage`, `SettingsPage`, `LogsPage` (EventSource consumers), `FolderPicker`, `Toast`, `theme` (dark/light/system with CSS variables). One Jest/testing-library test file (`App.test.tsx`).

## 5. Target architecture

### 5.1 Layout

```
internal/smbedit/
  build.go          Build(cfg config.SmbeditConfig) (http.Handler, error)
  handler.go        stdlib ServeMux routes (ported from api/server.go, chi removed)
  picker.go         folder picker (root becomes configurable)
  state.go          module state load/save (ported from smbed internal/config)
  samba.go          rendering/backup/write/parse/restart (ported verbatim-ish)
  oplog.go          ops log ring buffer (ported verbatim)
  *_test.go         ported tests for all of the above
web/smbedit/
  index.html
  src/              ported React/TSX sources (kept in-tree)
  js/bundle.js      committed esbuild output (+ bundle.css if extracted)
  style.css         (if kept separate from the bundle)
docs/smbedit.md     operator docs (sudoers, migration, hostnames)
```

`internal/smbedit` may keep everything in one package (the smbed sub-packages are small); splitting into sub-packages is acceptable but not required. Import path prefix is `cmd184psu/unified-webapp/internal/smbedit`.

### 5.2 Dispatch wiring

- `cmd/server/main.go`: add `case "smbedit": return smbedit.Build(cfg.Smbedit)` to `buildModule`.
- `internal/platform/config/config.go`: add `Smbedit SmbeditConfig \`json:"smbedit"\`` to `Config`, defaults in `DefaultConfig()`, and `~`-expansion for its paths in `Load()` (follow `expandMultisshPaths` precedent).

### 5.3 Configuration split (the one real design change)

smbed kept everything — operator paths *and* UI-mutable state — in `~/.smbed.json`, which the server rewrote on every save. unified-webapp's config file is operator-owned and never written by the server, so the port splits it:

**Operator config — `smbedit` section of `~/.unified-webapp.json`** (static, read at boot):

```json
"smbedit": {
  "static_dir": "./web/smbedit",
  "data_dir":   "./data/smbedit",
  "picker_root": "/opt"
}
```

| Field | Default | Meaning |
|---|---|---|
| `static_dir` | `./web/smbedit` | Built frontend. `Build()` fails if missing/unreadable (copy `multissh.checkStaticDir`). |
| `data_dir` | `./data/smbedit` | Holds `state.json` and nothing else. Created by `Build()` if absent. |
| `picker_root` | `/opt` | Root for the folder picker (was hardcoded `/opt` in smbed; making it a config knob keeps tests hermetic and non-Linux dev usable). Empty string means the default. |

**Module state — `<data_dir>/state.json`** (mutable via the UI, written by the server, mode 0600, precedent: obsidianoid's `state.json`):

```json
{
  "smb_conf_path":  "/etc/samba/smb.conf",
  "samba_log_path": "/var/log/samba/log.smbd",
  "share_owner":    "nobody",
  "theme":          "dark",
  "globals":        [ {"key": "workgroup", "value": "WORKGROUP"}, … ],
  "shares":         [ {"name":…, "path":…, "comment":…, "writable":…, "public":…, "browseable":…, "enabled":…} ]
}
```

This is exactly smbed's `~/.smbed.json` **minus `listen_addr`**, which no longer exists — the unified server owns the listener and hostnames own the routing. Defaults on first run are smbed's defaults (same 17 global entries, empty shares). Loading applies `DisableMissingPaths` exactly as smbed did.

Consequences for the API/UI (the only user-visible deltas from smbed):

- `PUT /api/config` no longer accepts `listen_addr`; the Settings page drops that field. All other Settings fields (`smb_conf_path`, `samba_log_path`, `share_owner`, `theme`) stay runtime-editable and persist to `state.json`.
- `GET /api/config` returns the state object above (no `listen_addr`).

### 5.4 Router port (chi → stdlib)

The repo has **zero HTTP router dependencies** and must stay that way — do not add chi to `go.mod`. Port the routes onto `http.ServeMux` with method patterns (`mux.HandleFunc("GET /api/shares", …)`, a Go 1.22+ stdlib feature; `go.mod` already targets 1.26), exactly as `internal/multissh/server.go` does. Upgrading the `go` directive further is permitted if a newer stdlib feature genuinely helps, but is not expected to be necessary. chi middleware (`Logger`, `CleanPath`) is dropped; the platform's `middleware.Wrap` (CORS + OPTIONS) already wraps the dispatcher. `Recoverer` is replaced, not dropped: the module wraps its mux in a small module-local panic-recovery handler that logs the stack and returns a clean 500, preserving the client-visible behavior chi provided (without it, `net/http`'s built-in recovery keeps the process alive but drops the connection with no response body). The recovery wrapper's `ResponseWriter` must forward `Flush()` (and `Unwrap()`) so the SSE endpoints' `http.Flusher` assertion still succeeds. Static serving replaces the embedded-FS fallback with the on-disk `staticHandler` pattern from multissh (`internal/multissh/server.go:174` — real file → serve; miss under `/api/` → 404; other GET/HEAD miss → index.html).

### 5.5 Concurrency (new requirement, not in smbed)

smbed's handlers mutate `s.cfg` with no locking — tolerable for a single-user binary, not for a long-lived shared server. The ported module MUST guard its state (the in-memory state object + `state.json` writes) with a mutex. `oplog` is already thread-safe. Write `state.json` via temp-file-then-rename in `data_dir` for atomicity.

### 5.6 SSE through the platform

`middleware.Wrap` is a plain pass-through (no buffering), so SSE works; keep smbed's `http.Flusher` checks and headers as-is. Note in operator docs (as multissh's docs already warn) that a fronting proxy must not buffer these two endpoints and must preserve the Host header.

### 5.7 Frontend port (React + Vite → React + esbuild)

Straight port keeps the React 18 sources. What changes is only the toolchain:

- Copy `reference/smbed/web/src/*.tsx|*.ts|*.css` → `web/smbedit/src/` and `index.html` → `web/smbedit/index.html`, minus the Vite scaffolding (`vite.config.ts`, per-app `package.json`, jest config).
- Add `react` + `react-dom` (and `@types/*` as devDeps) to the **root** `package.json`. esbuild bundles TSX natively — no plugin needed.
- Extend the root `build` / `build:dev` scripts: `esbuild web/smbedit/src/main.tsx --bundle --target=es2020 --outfile=web/smbedit/js/bundle.js` (let esbuild emit `bundle.css` from the CSS import, or link `style.css` directly from index.html — either, consistently).
- `index.html` references the bundle with relative paths (no Vite asset hashing).
- Commit the built bundle, per repo convention (`make build` must not require npm).
- Remove the `listen_addr` field from `SettingsPage`, from `api.ts`'s `AppConfig`/patch types, and from the two `api.putConfig({…})` call sites in `App.tsx` that pass it (lines 83 and 136 in the reference source).
- Update the three user-visible strings that name `~/.smbed.json` (`App.tsx:92`, `App.tsx:241`, `SettingsPage.tsx:27`) to say `state.json` — after the config split those strings would be false and would contradict the migration doc (FR-17).
- **No other source changes** beyond what compilation outside Vite requires (verified: the sources contain no `import.meta`/`process.env` references).
- Root `tsconfig.json`: include `web/smbedit/src` with `"jsx": "react-jsx"` so `make typecheck` covers it (a per-directory tsconfig referenced by the root one is fine if JSX settings conflict).
- The jest/testing-library suite (`App.test.tsx`, `test-setup.ts`) is **not** ported — the repo has no jest infrastructure and adding it violates the toolchain convention. Frontend safety net = `make typecheck` + the ported Go API tests. (Recorded as a scope decision, see Q3.)

### 5.8 Privilege model / deployment

Unchanged from smbed and must be documented in `docs/smbedit.md` + README: the service user (see `unified.service`, currently `cdelezenski`) needs passwordless sudo for exactly:

```
sudo install -m 0644 <tmp> <smb_conf_path>     # write smb.conf when not directly writable
sudo systemctl restart smbd                    # and fallbacks: smb, service smbd restart
sudo tail -n 200 -F <samba_log_path>           # Samba log stream
```

Provide a sample sudoers snippet in the docs. On hosts without these grants, Save & Restart reports failure in-band (`restart.success=false`, ops log entries) — it must not crash the module.

## 6. Functional requirements

Backend:

- **FR-1** `internal/smbedit.Build(cfg)` returns an `http.Handler`; it fails (error, not degrade) when `static_dir` is unset/missing/unreadable; it creates `data_dir` if absent and fails if it cannot; it loads or default-initializes `state.json`.
- **FR-2** All API endpoints from §4 — 13 method+path registrations across 10 distinct paths (`GET|PUT /api/config`, `GET|PUT /api/shares`, `GET|PUT /api/globals`, `GET /api/folders`, `POST /api/import`, `GET /api/preview`, `POST /api/save-and-restart`, `GET /api/logs/ops/stream`, `GET /api/logs/samba/stream`, `GET /api/version`) — are served with identical paths, methods, request/response shapes, except `listen_addr` removed from config GET/PUT (§5.3).
- **FR-3** smb.conf rendering output is **byte-identical** to smbed's for the same state (same template, including the "Generated by smbed" header line — changing it to "smbedit" is allowed but do it in template + tests together).
- **FR-4** Save & Restart: timestamped `.bak` backup of the existing file, direct write with sudo-install fallback, restart attempt chain `systemctl restart smbd` → `systemctl restart smb` → `service smbd restart`, all ops-logged.
- **FR-5** Import parses globals/shares/share_owner from an arbitrary smb.conf path (default: configured path), returns staged data without persisting, ops-logs the summary. Parser semantics identical (case-insensitive keys, whitespace collapsing, line continuations, `read only`/`writable`/`writeable` handling, comment/`;` skipping).
- **FR-6** `DisableMissingPaths` runs on state load, on `PUT /api/shares`, and on import results; auto-disabled share names are ops-logged on PUT.
- **FR-7** Folder picker lists immediate, non-hidden subdirectories of `picker_root` (default `/opt`), never deeper.
- **FR-8** Both SSE streams behave as in smbed: ops stream = snapshot backlog then live tail via subscription; samba stream = `sudo tail` bridge with the process killed on disconnect, 400 when `samba_log_path` is empty.
- **FR-9** All state mutation is mutex-guarded; `state.json` writes are atomic (temp + rename), mode 0600.
- **FR-10** Platform wiring: `SmbeditConfig` in platform config with defaults + `~`-expansion; `buildModule` case; `make init-config` output includes the `smbedit` section.
- **FR-11** The module compiles and its tests pass on darwin and linux (`go test -race ./...`); anything exec-ing external commands is behind the swappable `runCommand` var as in smbed, so tests never shell out.

Frontend:

- **FR-12** All six UI surfaces work against the ported backend: Shares (add via folder picker, edit flags, enable/disable, delete), Globals (inline-editable ordered table), Preview, Settings (minus listen address), Logs (both streams), Import flow with staged-then-Save semantics and toasts.
- **FR-13** The staged-edit model is preserved: edits accumulate client-side; Save persists; Save & Restart persists then writes/restarts.
- **FR-14** Theme dark/light/system persists via Settings and applies as in smbed.
- **FR-15** Deep links (e.g. a bookmarked page path, if the SPA uses paths) resolve via the index.html fallback; unknown `/api/*` paths return 404, not index.html.

Docs/ops:

- **FR-16** README gains: smbedit in the module list, `host_routing` example entries (`smbedit.cmdhome.net`, `smbedit-test.cmdhome.net`), the `smbedit` config section table, data-directory layout entry, pointer to `docs/smbedit.md`.
- **FR-17** `docs/smbedit.md` covers: privilege/sudoers setup (§5.8), the no-auth warning, proxy/SSE caveats, and migration from standalone smbed (§8).
- **FR-18** `unified-webapp-example.json` gains a filled-in `smbedit` section.

## 7. Test requirements

- **TR-1** Port every Go test from `reference/smbed` (config, samba render/parse/write-fallback, oplog, api server, picker), adapted to the new package layout, state-file location (use `t.TempDir()`), and `Build()`/handler construction. Coverage must not regress relative to the originals.
- **TR-2** New tests for the deltas: `Build()` failure on bad `static_dir`; state.json round-trip incl. atomic write; `picker_root` config; `listen_addr` absent from config API; concurrent PUTs don't race (`-race` is already on in `make test`).
- **TR-3** A dispatch test in `cmd/server` confirming `"smbedit"` builds and unknown-module behavior is unchanged (extend `dispatch_test.go`).
- **TR-4** `make typecheck` passes with the new TSX included.

## 8. Migration from standalone smbed

Documented, manual: copy `~/.smbed.json` → `<data_dir>/state.json`, delete the `listen_addr` key (it is ignored if left, since unmarshal is into the new struct — but tell users to remove it), add the hostname to `host_routing` and DNS/HAProxy/`/etc/hosts`, set up sudoers for the unified service user, stop/disable the old smbed service. No automatic migration code.

## 9. Acceptance criteria

1. `make test` green on darwin (repo dev machine) — includes all ported + new Go tests under `-race`.
2. `make build` produces the binary without npm present; `make web && git status` shows only expected regenerated bundle changes; `make typecheck` green.
3. With a config routing `smbedit-test.cmdhome.net → smbedit` (via `/etc/hosts`), the full UI loads at `http://smbedit-test.cmdhome.net:8080`, and a manual smoke pass covers: add share via picker → edit a global → Preview shows both → Save → `state.json` updated → Import a sample smb.conf → staged data appears → ops-log stream shows the import entry live.
4. On a Linux host with Samba + sudoers configured: Save & Restart writes `/etc/samba/smb.conf` (with `.bak` created), restarts smbd, and the Samba log stream tails; without sudoers, the same action reports failure in the UI without crashing the module.
5. Other modules' behavior is untouched (existing tests green, no changes outside the files listed in §5.1/§5.2 plus package.json/tsconfig/README/example-config).

## 10. Decisions (resolved with the owner, 2026-09-11)

All questions below are settled — ralplan should treat these as fixed constraints, not open items.

- **D1 — Frontend stack: React + esbuild** (§5.7). Owner note: this is the better choice; migrating the older vanilla-TS modules toward it can happen in a separate future round — **not** in this project.
- **D2 — Runtime-editable conf paths: keep editable.** `smb_conf_path` / `samba_log_path` remain UI-editable and persist to `state.json`. No behavior change to the Settings page beyond removing `listen_addr`.
- **D3 — Frontend tests: drop the jest suite.** Rely on `make typecheck` + the ported Go API tests. Revisit only if a concrete regression motivates it.
- **D4 — `reference/smbed/` stays untracked.** Add `reference/` to `.gitignore`.
- **D5 — Persistence model: keep `state.json`** (§5.3 as written). A "smb.conf as single source of truth" design was evaluated (would eliminate drift after hand edits, at the cost of privileged writes on every save and needing passthrough of unrecognized share keys to avoid data loss) and **declined** — no behavior change from smbed for now. If revisited later, the prerequisites are: per-share passthrough of unknown keys, and `enabled:false` ↔ `available = no` mapping.
- **D6 — Go toolchain.** `go.mod` already targets Go 1.26; upgrading beyond it is authorized if beneficial, but no upgrade is required for this work (§5.4).

## 11. Out of scope for ralph

Do not modify anything under `reference/`. Do not touch other modules' code except the shared wiring points (`cmd/server/main.go`, `internal/platform/config/config.go`, root `package.json`/`tsconfig.json`/`Makefile` if needed, README, example config).
