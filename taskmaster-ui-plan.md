# Plan: Taskmaster UI & Model Rework

**Status:** Ready for execution (ralph → Sonnet builders). 2026-09-13.
**Inputs:** `taskmaster-ui-FRD.md` (the rework — authoritative for intent) and
`taskmaster-FRD.md` (the settled port spec — background context; do not
re-litigate its backend decisions except where §9 of the UI-FRD explicitly
supersedes them).
**Model policy:** builders on **Sonnet**; planner/critic on **Fable**; **never
Opus**. Slices below are sized to be independently buildable and verifiable.
**Golden rule for every slice:** `go build ./... && go test -race ./...`,
`npm run typecheck`, `npm run build` stay green; the `internal/taskmaster`
goleak gate stays clean.

---

## A. Architectural decisions (plan-level)

| # | Decision | Rationale |
|---|----------|-----------|
| D1 | **Rename group→lane everywhere** (DB table, model, config, API routes, CLI). Not an alias — a real rename. | The premise change is total; a half-rename leaves two vocabularies. `pool_limit`→`width`. |
| D2 | **Drop `allowed_types` entirely.** | It only existed to restrict task *types*; types are gone (D4), so it's meaningless. |
| D3 | **Drop per-task `priority`; add per-lane `position` (integer).** Ordering = position; drag reorders. | FRD §6. Worker fills slots in `position` order among ready tasks. |
| D4 | **Collapse `task_type` + JSON `args` into a single `command` string.** Executor runs `sh -c command`. | FRD §6/§9. No type, no JSON, no `alfredo`. |
| D5 | **New terminal status `canceled`** alongside `success`/`failed`. | Distinguish operator kills from real failures in history/metrics. |
| D6 | **Per-execution cancel + durable emergency-stop ("hand brake").** Brake state persists in `settings`; worker boots braked if set. | FRD §5/§9. Fail-safe across violent reboot. |
| D7 | **Live lane board via server push (SSE board-events), surgically patched client-side.** Reuse `platform/broker`. | FRD §8a live-refresh edict: only update on change, patch only what changed, never full repaint. |
| D8 | **Frontend rewritten from scratch** under `web/taskmaster/`; backend kept and modified. | FRD §10. |
| D9 | **UI primitives (toggle, modal, live/pause control) built as self-contained, extractable components.** | FRD §8b: pay toward the future shared UI layer, don't add a 9th bespoke pile. |
| D10 | **Keep the port's execution lifecycle** (`CommandContext` + 10s `WaitDelay` + context-cancel shutdown + goleak-clean `Close()` + line-streaming). | It's correct and unrelated to alfredo. |

---

## B. Backend phases

### Phase B1 — Data model migration (group→lane, task reshape)
Files: `internal/taskmaster/db/db.go`, `internal/taskmaster/models/models.go`,
`internal/platform/config/config.go`, `local-test/config.json`,
`taskmaster-test.json`, `docs/taskmaster.md`.

Steps:
1. **DB migration (next version number)** via the existing `applyMigrations`
   list. SQLite table-rebuild pattern (create new → copy → drop → rename) since
   this renames a table and drops columns:
   - `groups` → `lanes` (`name`, `width` [was `pool_limit`], `paused`,
     `paused_at`, `paused_by`, `created_at`, `updated_at`); **drop**
     `allowed_types`.
   - `tasks`: rename `group_name`→`lane_name`; **add** `command TEXT NOT NULL
     DEFAULT ''`, `position INTEGER NOT NULL DEFAULT 0`; **drop** `task_type`,
     `args`, `priority`. Best-effort data carry: for legacy rows, set
     `command` from `args`→`shell` when `task_type='shell'`; otherwise leave
     blank for manual fixup (document this).
   - `task_executions.status` gains `canceled` as a valid value (no schema
     change needed — it's free text; just document the new value).
2. **models.go:** `Group`→`Lane` (`Width`); `Task` gains `Command`, `Position`;
   drops `TaskType`, `Args`, `Priority`, and `Lane.AllowedTypes`.
3. **config.go:** `TaskmasterConfig.Groups`→`Lanes` (`{name, width}`; drop
   `allowed_types`); update `DefaultConfig()` and the seed path. Update both
   config files' `taskmaster` blocks (`groups`→`lanes`, `pool_limit`→`width`,
   remove `allowed_types`).
4. **build.go `seedGroups`→`seedLanes`.**

Done-check: `go build ./...`; a fresh DB opens, migrates, and seeds lanes; an
existing test DB migrates without error.

### Phase B2 — Execution rewrite (single shell path)
Files: `internal/taskmaster/worker/executor.go` (+ its tests).

Steps:
1. Replace the `task_type` switch and all JSON-args structs with one path:
   `exec.CommandContext(ctx, "/bin/sh", "-c", task.Command)`.
2. Sudo: when the gate is open, wrap as `sudo sh -c "<command>"` (keep the
   `SudoGate` check at both executor and validation).
3. Keep D10 lifecycle (`WaitDelay`, streaming into the output registry).
4. Delete the type/args tests; add tests: command runs & streams; empty command
   is a clear validation error; sudo-wrapping when open; refusal when closed.

Done-check: `go test -race ./internal/taskmaster/worker/...` green incl. goleak.

### Phase B3 — Scheduling by position + readiness
Files: `internal/taskmaster/worker/worker.go`, `db.go` (task queries).

Steps:
1. Per lane, fill `width - running` slots among **ready** tasks — enabled, not
   paused, lane not paused, brake not engaged, cooldown-elapsed
   (finished_at + cooldown ≤ now) — ordered by `position` ascending. Remove all
   `priority` ordering.
2. Confirm cooldown is **rest** not cadence (no wall-clock math); keep the 5s
   poll and per-task lock/TTL.

Done-check: unit test — width-1 lane with a cooling task + a ready task runs the
ready one first (FRD decision (b)); position order respected.

### Phase B4 — Cancel + hand brake (backend)
Files: `worker/worker.go`, `worker/output.go` or a new `worker/cancel.go`,
`coordinator/*.go`, `db.go`, `build.go`.

Steps:
1. Give each `runTask` its **own child context**; register `execution_id →
   cancel` in an instance-scoped map (owned by the module, cleaned on finish).
   `Close()` still cancels all (unchanged shutdown semantics).
2. **Cancel one:** `POST /api/executions/{id}/cancel` → look up cancel func,
   call it (SIGKILL via CommandContext + WaitDelay), write status `canceled`
   with a message. Idempotent/404 if not running.
3. **Hand brake:** durable `settings` key `brake_engaged` (reuse
   `SettingAllowSudo` machinery / `EncodeBoolSetting`). `POST /api/brake` →
   set flag + pause all lanes + cancel all running; `DELETE /api/brake`
   (release) → clear flag (lanes stay as they are unless the operator resumes
   them — document whether release auto-resumes lanes or just lifts the launch
   freeze; **recommend: release lifts the freeze, individual lane pause states
   restored to pre-brake**, so record pre-brake pause state). `GET /api/brake`
   → `{engaged: bool}`.
4. Worker loop **honors `brake_engaged`**: launches nothing while engaged. On
   boot, read the flag; if set, start braked (log it).
5. Metrics: count `canceled` separately from `failed` (don't inflate failures).

Done-check: tests — cancel a running task → `canceled`; brake engaged →
nothing launches + running killed; brake flag persists across a simulated
restart (reopen DB, rebuild module, assert braked). goleak clean.

### Phase B5 — API surface finalization
Files: `coordinator/coordinator.go` (routes) + handlers.

Steps:
1. Rename `/api/groups*` → `/api/lanes*` (list/create/get/update/delete,
   pause/resume). Add `PUT /api/lanes/{name}/width`. Add reorder:
   `PUT /api/lanes/{name}/order` (task-name list → positions) and
   `POST /api/tasks/{name}/move` (to another lane).
2. Tasks: create/update take `command`, `lane_name`, advanced fields
   (`repeat`, `cooldown_seconds`, `sudo`, `output_file`, optional
   `workdir`/`env` — if added, fold into how the command is run). Drop
   `task_type`/`args`/`priority` from payloads. Rename enqueue action to
   **"up next"**: `POST /api/tasks/{name}/up-next` (insert at front of the
   lane's ready order for the next free slot).
3. Keep `/api/executions`, `/api/executions/{id}/output` (SSE),
   `/api/metrics`, `/api/health`, `/api/capabilities` (GET/POST).
4. **Board events SSE (D7):** `GET /api/board/events` — a broker-backed stream
   emitting compact change events (`task-enqueued`, `task-started`,
   `task-finished` {status}, `lane-updated`, `brake`), so the client patches
   surgically. Reuse `platform/broker`; respect `SSEMaxSubscribers`.

Done-check: coordinator tests updated to lane vocabulary; new endpoints have
happy-path + error tests.

### Phase B6 — CLI (taskmasterctl) alignment
Files: `cmd/taskmasterctl/main.go` (+ any test).

Steps: rename `group`→`lane`; task create takes `-command`/`-lane`; add
`cancel`, `brake`/`release`, `up-next`; drop type/args flags. Ensure the flag
syntax matches what the Designer's **taskmasterctl export** will emit (Phase F3).

Done-check: builds; `--help` reflects the new surface.

---

## C. Frontend phases (full rewrite of `web/taskmaster/`)

Delete the old page modules (`groups.ts`, `tasks.ts`, `executions.ts`,
`metrics.ts`, `main.ts`, `output.ts`, `api.ts`, `style.css`) and rebuild.
esbuild entry stays `web/taskmaster/js/main.ts` → committed `bundle.js`
(`package.json` build entry unchanged). Keep it TS, no framework unless a
builder judges one warranted (prefer none — matches the other modules and keeps
the extractable components portable).

### Phase F0 — Extractable UI primitives (build first; D9)
New `web/taskmaster/js/ui/`:
1. **`toggle.ts`** — a toggle-switch component (replaces every checkbox;
   [ui edict]).
2. **`modal.ts`** — lightbox/modal + `confirm`/`alert`/`prompt` replacements
   (no native dialogs; [ui edict]).
3. **`live.ts`** — a live/pause controller: subscribe to board-events SSE,
   expose a pause toggle + hamburger-set interval fallback, and a **surgical
   patch** helper (update one row/cell; never repaint). [ui edict]
Keep these dependency-free and self-contained so the future shared layer can
lift them (§8b). Done-check: `npm run typecheck`.

### Phase F1 — api client
`api.ts`: lanes CRUD/reorder/width/pause, tasks CRUD/move/up-next/cancel,
executions + output SSE (native `EventSource`), metrics, capabilities GET/POST,
brake GET/POST/DELETE, board-events SSE. No `alert` on error — surface via
`modal.ts`. Done-check: typecheck.

### Phase F2 — Lane board (home)
The board: vertical lanes (columns), each a playlist — **ran** (above),
**running** (now), **up-next** (below, with cooldown countdowns). Live via
`live.ts` (surgical patch on board events; no full repaint). Per-lane header:
name, **width control** (toggle/stepper), **"+ add task"** (opens Designer,
lane pre-selected). Drag within a lane (reorder → `order` API) and between lanes
(→ `move` API). Empty state. Done-check: board renders from `/api/lanes` +
tasks + executions; a task start/finish patches only its card (verify no
scroll/selection loss).

### Phase F3 — Task Designer + exports
Modal (via `modal.ts`): primary = **name + command + lane**; **Advanced**
(folded): repeat + cooldown, output-file, sudo (only when capabilities allow),
workdir/env. Primary action **"Add to lane."** Collapsible **taskmasterctl**
and **curl** export panels that live-render from the form (curl carries the
`Host` header + API-key placeholder; taskmasterctl matches Phase B6 flags).
Done-check: creating adds to the chosen lane; exports match what the CLI/API
actually accept (cross-check B5/B6).

### Phase F4 — Task drill-in, cancel, up-next, global controls
1. Click a task → detail view: run **history** (its executions) + **live
   output** (per-execution SSE) + **per-task metrics**. (No standalone History
   tab.)
2. **Cancel** control on running tasks; **"Up next"** on existing tasks.
3. **Hand brake:** prominent, always-visible global control (engaged state
   obvious); latches; release resumes. Reflects `/api/brake` + board `brake`
   events.
4. **Hamburger:** live/pause + interval, logout icon (auth-aware, existing
   logic), server status. All toggles use `toggle.ts`.
Done-check: cancel → card shows `canceled`; brake engaged greys the board and
stops launches; release restores.

### Phase F5 — Metrics tab (global)
A single **Metrics** nav entry = the cross-task overview (replaces the old
per-task-cards tab). Per-task metrics remain in F4's drill-in. Nav total:
**board + Metrics + hamburger.** Done-check: typecheck + build; bundle
committed.

---

## D. Docs, tests, acceptance

- **Docs:** rewrite `docs/taskmaster.md` (lanes, command model, cancel/brake,
  "we're not cron", exports) and update `docs/USERGUIDE.md`. Note the migration
  in a changelog line.
- **Backend tests:** lane CRUD/reorder; position+readiness scheduling incl.
  decision-(b) cooling case; single shell exec; cancel→`canceled`; brake
  engage/persist-across-restart/release; metrics separate `canceled`; goleak.
- **Frontend:** `npm run typecheck` + `npm run build` green; committed bundle
  byte-matches a fresh build.
- **Acceptance (manual, local-test):** lane board live-updates surgically while
  a task runs (no jitter); "+ add task" → Designer → Add to lane; exports
  copyable and valid; drag reorder + move persist; cancel kills a running task;
  hand brake stops everything and survives a restart (boots braked); Metrics
  tab shows the global view; every on/off control is a toggle; no native
  `alert`/`confirm`/`prompt` anywhere; no Refresh buttons.

---

## E. Risks / notes

- **Migration data loss:** legacy non-shell tasks can't be mechanically mapped
  to `command`; document that they need manual fixup (acceptable — test data).
- **Board-events SSE scope:** keep events compact and idempotent; the client
  should tolerate missed events by reconciling on (re)connect (fetch board
  snapshot, then stream). Respect `SSEMaxSubscribers`.
- **Sudo children on cancel/brake/shutdown:** root-owned `sudo` processes may
  outlive a kill (documented caveat); surface this honestly in the UI copy near
  the brake.
- **Extractable components:** resist coupling `ui/` to taskmaster specifics —
  they're the seed of the future shared platform UI layer.

## F. Explicit non-goals (do not build here)
Scoped/per-module API keys; the setuid-root sudo helper; themeable skin / shared
platform UI layer (build extractable, don't build the layer); SSH / remote
execution; taskmaster⊕multissh merge; any conditional/dependency logic between
tasks (that's a script's job); any wall-clock/cron scheduling.
