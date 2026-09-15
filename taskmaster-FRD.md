# FRD: Taskmaster Module for unified-webapp

**Date:** 2026-09-12
**Status:** Draft for planning (feeds ralplan)
**Source:** `reference/continuous-task-runner-queue` (read-only reference; do not modify)

---

## 1. Purpose

Fold the standalone **continuous-task-runner-queue** ("ctrq") application into unified-webapp as a new module named **taskmaster**, following the established module contract in `docs/adding-a-module.md`. Taskmaster is a task queuing/scheduling system: named concurrency groups with pool limits, tasks with priority/cooldown/repeat semantics, command execution via `os/exec`, live SSE output streaming, execution history, and metrics — with an embedded web UI.

Modifications to the ported code should be **minimal**, except where this FRD explicitly requires changes (auth removal, lifecycle correctness, security gating, build-convention conformance).

## 2. Source scope

The live code in the reference is `cmd/` + `internal/` + `web/` of the **root module** (`github.com/cmd184psu/ctrq`, Go 1.25, chi router, `modernc.org/sqlite`).

**Explicitly excluded — do not port:**
- `ctq/` — a separate legacy Go module implementing an earlier single-queue design; depends on an unavailable external library (`github.com/cmd184psu/alfredo`).
- `ctq-example/` — stale example code that does not build.
- `ctrqctl` (top-level file) — an accidentally committed 8.6 MB compiled arm64 binary, not source.
- `cmd/run.go`'s `RunServices()` process scaffolding (signal handling, standalone server bootstrap) — replaced by the module contract.
- All passcode/JWT auth code (`internal/coordinator/auth.go`, `POST /api/auth/token`, frontend login page and token storage).

## 3. Decisions (settled with the owner, 2026-09-12)

| # | Decision |
|---|----------|
| D1 | **Auth: platform gate only.** Taskmaster's passcode → JWT scheme, login UI, and `authMiddleware` are deleted. Protection comes from one `auth.modules` entry, like every other module. Zero auth code in the module. |
| D2 | **CLI ported.** The `ctrqctl` command surface survives as a new binary that authenticates with platform API keys (see FR-C*). |
| D3 | **Sudo kept but config-gated.** A module-config flag (default **off**) controls whether tasks may set `sudo: true`. §8 documents why this is insufficient and the intended future direction (curated setuid helper). |
| D4 | **`allowed_types` enforcement fixed.** The reference stores a group's `allowed_types` but never enforces it (latent bug in `handleAddTask`). Taskmaster enforces it at task create/update time. |
| D5 | **Frontend conforms to unified conventions.** TypeScript moves under `web/taskmaster/`, built by esbuild via the root `package.json`, compiled JS committed, assets served off disk via `platform/static` — not `go:embed` + `tsc`. |
| D6 | **Model policy (process, not product):** planning/critic roles run on Fable, worker/builder roles on Sonnet. Opus 5 is never used. |

## 4. Functional requirements — module integration

- **FR-M1:** Add `TaskmasterConfig` to `internal/platform/config/config.go` with a `Taskmaster` field on `Config` (json tag `"taskmaster"`). Fields (names indicative; final naming may follow existing module conventions):
  - `db_path` (string) — SQLite database file. Default under the module's data directory following existing `DefaultConfig()` conventions. `~` expansion via an `expandTaskmasterPaths` function wired into `Load`, like other modules.
  - `static_dir` (string) — frontend assets, default `web/taskmaster`.
  - `groups` ([]{name, pool_limit, allowed_types}) — seeded/upserted into the DB at startup, as in the reference. DB is authoritative at runtime thereafter.
  - `allow_sudo` (bool, **default false**) — see FR-X2.
  - unexported `SSEMaxSubscribers int` populated by `applyServerDefaults`, matching the platform convention for SSE-owning modules.
- **FR-M2:** Add a `DefaultConfig()` entry for taskmaster so `-init-config` emits a sane zero-value config.
- **FR-M3:** Register the module: `case "taskmaster":` in `buildModule` (`cmd/server/main.go`) calling `taskmaster.Build(cfg.Taskmaster)`, and add `"taskmaster"` to `knownModules`.
- **FR-M4:** Module contract: `func Build(cfg config.TaskmasterConfig) (http.Handler, error)` in `internal/taskmaster`. `Build` opens the DB, seeds groups, starts the worker loop, and returns a handler that **implements `io.Closer`** (precedents: slideshow's conductor, obsidianoid's watchers). `Close` must stop the worker, the output-registry GC, all per-execution goroutines, and close the DB. The goleak test gate must pass.
- **FR-M5:** Routing is by vhost only. A `host_routing` entry (e.g. `"taskmaster.example.com": "taskmaster"`) turns the module on. No path-prefix mounting is required; the module owns `/` on its hostnames, so the reference frontend's absolute paths (`/api/...`, `/js/...`) remain valid.
- **FR-M6:** `local-test/` support: add a `taskmaster` section and `host_routing` entry to `local-test/config.json` (hostname `taskmaster.test`), and add `taskmaster.test` to the `/etc/hosts` lines checked/printed by `local-test/setup.sh` (both `127.0.0.1` and `::1`).
- **FR-M7:** Body limit: use the platform default (1 MiB) unless porting reveals a payload that needs more; if so, add a `limitFor` override with a comment justifying it.

## 5. Functional requirements — backend port

- **FR-B1:** Port `internal/db`, `internal/worker`, `internal/coordinator` (minus auth), and `internal/models` into `internal/taskmaster/` (package layout may flatten or keep subpackages — planner's choice, but no other unified module may import taskmaster internals and vice versa).
- **FR-B2:** Keep the **chi** router (`github.com/go-chi/chi/v5`) and **`modernc.org/sqlite`** dependencies; add them to the unified `go.mod`. Keeping chi preserves the handler code near-verbatim. (Converting to Go 1.26 `http.ServeMux` patterns is acceptable if the planner judges it low-risk, but is not required.)
- **FR-B3:** API surface (all under the taskmaster vhost, protected by the platform gate):
  - Groups: `GET/POST /api/groups`, `GET/PUT/DELETE /api/groups/{name}`, `POST /api/groups/{name}/pause|resume`
  - Tasks: `GET/POST /api/tasks`, `GET/PUT/DELETE /api/tasks/{name}`, `POST /api/tasks/{name}/pause|resume|enqueue`
  - Executions: `GET /api/executions`, `GET /api/executions/{id}/output` (SSE)
  - Metrics: `GET /api/metrics`
  - Health: the platform gate already serves `GET /healthz` unauthenticated; the reference's `/api/health` may be kept for CLI compatibility or dropped in favor of `/healthz` — planner's choice, but the CLI (FR-C*) must agree with whichever survives.
  - **Removed:** `POST /api/auth/token`.
- **FR-B4:** Scheduling semantics are preserved verbatim: 5 s poll loop, per-group `pool_limit` concurrency, DB advisory locks with 10 min TTL, repeat/cooldown re-enqueue, enable/disable and pause/resume at task and group level, priority ordering.
- **FR-B5:** Persistence preserved: SQLite (WAL, `modernc.org/sqlite`, single write connection), existing schema and the small migration mechanism, `task_metrics` recording. Metrics retention remains manual (documented; see §9 non-goals).
- **FR-B6:** SSE output streaming preserved: ring-buffer replay then live subscribe, `event: output|status|done`. The subscriber count must respect the platform `SSEMaxSubscribers` cap (FR-M1). Whether this reuses `platform/broker` or keeps taskmaster's own fan-out is the planner's choice; keeping its own is acceptable if the cap is honored.
- **FR-B7:** **Lifecycle correctness (required deviations from the reference):**
  - `worker.Registry` (package-level global `OutputRegistry`) becomes instance-scoped, owned by the built module.
  - `OutputRegistry.StartGC` gains context/`Stop` cancellation (the reference version runs an unstoppable `for range ticker.C` goroutine).
  - The per-execution deferred-unregister goroutine (`time.Sleep(time.Hour)` in `runTask`) becomes cancellable (timer + context, or GC-driven expiry).
  - No `signal.Notify` anywhere in the module; shutdown is exclusively via `Close()`/context.
- **FR-B8:** `output_file` teeing (with `{task}`/`{exec_id}` substitution) is preserved. Paths are written as the service user; document that this writes outside the DB. Path confinement via `platform/fspath` is **not** required for v1 (task authors already have arbitrary exec), but note it in §8.

## 6. Functional requirements — execution security

- **FR-X1:** All four task types (`exec`, `shell`, `script`, `migration`) are preserved.
- **FR-X2:** **Sudo gating.** The per-task `sudo` flag is only honored when module config `allow_sudo` is true. Enforcement at **both** points:
  - Task create/update rejects `sudo: true` with a clear 4xx error when `allow_sudo` is false.
  - The executor independently refuses to prepend `sudo` when `allow_sudo` is false (defense in depth — covers tasks created before the flag was flipped off).
  - The UI hides or disables the sudo control when the server reports `allow_sudo: false` (a small capability endpoint or field on an existing response — planner's choice).
- **FR-X3:** **`allowed_types` enforcement.** Task create/update validates `task_type` against the target group's `allowed_types` (empty/absent list = all types allowed, matching the reference's config shape). Reject with a clear 4xx error otherwise.
- **FR-X4:** No other sandboxing is added in v1. §8 records the residual risk.

## 7. Functional requirements — frontend and CLI

- **FR-W1:** Frontend lives at `web/taskmaster/` (TS sources + committed compiled JS + `index.html` + CSS), served off disk via `platform/static` from `static_dir`. No `go:embed`.
- **FR-W2:** Root `package.json` gains esbuild build entries for taskmaster's TS (and `tsconfig.json` `include` gains the directory). `npm run build`, `build:dev`, and `typecheck` all cover it.
- **FR-W3:** The login page, JWT/sessionStorage token handling in `api.ts`, and the manual fetch-ReadableStream SSE consumption are removed. Under platform cookie auth, SSE uses native `EventSource`. Unauthenticated responses are handled by the platform gate (login page redirect for browser GETs), not by the module UI.
- **FR-W4:** Preserved UI surface: groups, tasks (per-group), executions (per-task), metrics, live output view — the existing hash-router pages, restyled only as needed (keeping its dark theme is fine).
- **FR-C1:** New CLI binary `cmd/taskmasterctl` (built by the Makefile alongside the server, including the `build-rpi` cross-compile) preserving the `ctrqctl` command surface: `group|task|executions|output|metrics|health` subcommands.
- **FR-C2:** CLI auth: platform API key sent as a bearer token, plus a configurable base URL (flag/env/config file — planner's choice; no `~/.ctrq-token` JWT cache). Document that the operator mints the key in the admin panel and that taskmaster must have an `auth.modules` entry for API keys to be accepted.
- **FR-C3:** CLI output-follow (`output` subcommand) consumes the SSE endpoint with the API key header (the header-on-SSE problem only applied to browsers).

## 8. Security write-up: why config-gated sudo is insufficient (accepted for v1, revisit later)

Recorded per owner decision — these are **known accepted risks**, not open questions:

1. **Sudo gating is coarse and binary.** `allow_sudo: true` enables sudo for *every* task author on the module, for *any* command sudoers permits the service account. There is no per-task, per-command, or per-user granularity. Flipping one JSON boolean converts "web-authenticated user can run commands as the service user" into "…as root" (given permissive sudoers).
2. **It delegates the real policy to sudoers**, an external file the webapp neither reads nor validates. The module cannot tell the operator what `allow_sudo` actually grants on a given host.
3. **API-key flattening.** Platform API keys are valid on **every** protected non-admin module. A key minted for a low-stakes module (e.g. grocery automation) can create and execute taskmaster tasks. Until the platform grows scoped/per-module keys, the effective security of taskmaster equals the security of the *weakest-handled* API key in the fleet. Operators should treat every API key as taskmaster-grade while this holds.
4. **Arbitrary exec is inherent.** Even with sudo off, taskmaster is by design remote command execution as the service user. The gate is the only control; there is no command allow-listing, no sandbox, no resource limits.

**Intended future direction (out of scope here):** replace the sudo flag with a **curated setuid-root helper** — a small, separately-audited binary that hard-codes the specific privileged operations taskmaster may perform (an "own sudo" with a fixed allow-list), removing the dependency on sudoers and host configuration entirely. Complementary platform work: scoped API keys (per-module validity) and, longer-term, the LDAP-groups access matrix already foreshadowed in `docs/FRD-admin-identity.md`.

## 9. Non-goals / out of scope

- The setuid-root curated helper (§8) and any sudo replacement.
- Scoped/per-module API keys (platform-level change).
- Multi-instance taskmaster (the `"instances"` schema from `docs/FRD-admin-identity.md` is future platform work).
- Automatic `task_metrics` pruning/retention (stays manual; document the `DELETE` guidance from the reference README).
- Sandboxing/resource-limiting of executed commands.
- Porting or resurrecting `ctq/`, `ctq-example/`, or the committed `ctrqctl` binary.
- Any modification to files under `reference/`.

## 10. Acceptance criteria

- `make build` produces the unified binary including taskmaster; `make web` / `npm run build` and `npm run typecheck` pass with taskmaster's TS included.
- `go test -race ./...` green, including ported taskmaster tests (testify may be added to `go.mod` if ported tests use it) and the goleak gate — build, run, and `Close()` a taskmaster module in tests with zero leaked goroutines.
- With `local-test`: `taskmaster.test` serves the UI; groups/tasks CRUD, enqueue, live SSE output, and metrics all work through the browser behind the platform gate; a protected config (auth.modules entry) shows the platform login page, not any taskmaster-native login.
- `taskmasterctl` drives the API end-to-end against a protected module using an admin-panel-minted API key.
- With `allow_sudo` absent/false: creating a task with `sudo: true` fails with a clear error; with it true, the sudo path works as in the reference.
- Creating a task whose `task_type` is not in its group's non-empty `allowed_types` fails with a clear error.
- New module documented: `docs/taskmaster.md` (module deep-dive incl. §8 security write-up reference), `docs/USERGUIDE.md` updated, config example updated in whatever file `docs/adding-a-module.md` designates as current (note: root `unified-webapp-example.json` is stale/legacy-format; `local-test/config.json` is the accurate exemplar).

## 11. Planning notes (for ralplan)

- Follow `docs/adding-a-module.md` step-by-step; `session.md` records the same fold-in pattern used for the original four modules.
- The reference Makefile/README binary-name mismatch (`ctrq` vs `ctrq-service` name-dispatch) is irrelevant post-port — the name-based dispatch in `cmd/ctrq.go` is not carried over; unified has one server binary plus `taskmasterctl`.
- Go: unified is on 1.26.2; the reference's 1.25 code needs no changes for that. Dependency additions: `go-chi/chi/v5`, `modernc.org/sqlite` (+ transitive), possibly `testify` (tests only). `golang-jwt/jwt/v5` is already present platform-side but must NOT be used by taskmaster (auth is deleted).
- Model policy for the run: Fable for planner/critic roles, Sonnet for workers/builders. Never Opus 5.
