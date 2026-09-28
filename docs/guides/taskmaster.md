# Taskmaster

A command runner: define **tasks** (each a single shell command) that live
in ordered, width-limited **lanes**; run them on demand or let them repeat
after a cooldown *rest*; watch their output live over SSE; and drive
everything from a live lane board. Ported from the standalone
`continuous-task-runner-queue` reference project into unified-webapp's
  module contract (`docs/guides/adding-a-module.md`); the object-model/UI
  rework described here is specced in
  `docs/archived/taskmaster-ui-FRD-14Sept2026.md` and
  `docs/archived/taskmaster-ui-plan-26Sept2026.md`.

**Read this before turning on `allow_sudo` or exposing this module beyond a
trusted operator group** — see [Security](#security) below.

---

## Concepts

- **Task** — one command. Durable, named, reusable, so repeat/history/
  metrics are meaningful. Runs on demand or repeats on a cooldown. Lives in
  exactly one lane, at a specific **position** within it.
- **Lane** — an ordered playlist of tasks with a **width**:
  - **width 1** — one task at a time, in order (the old "sequence").
  - **width N** — up to N tasks run concurrently, pulled off the top of the
    lane's ready order (the old "group"/"pool").
  - A lane plays straight through: one task's pass/fail never gates the
    next. There is no conditional/branching logic — that's what a script is
    for.
  - **"Groups" is gone**, both as a term and as a first-class entity. There
    are only lanes.
- **No task "type" for shell tasks.** A shell task is a single **command**
  string, run through a shell (`sh -c "<command>"`). There is no `exec`/
  `shell`/`script`/`migration` selector, and no JSON `args` blob — those are
  gone.
- **No `priority`.** Ordering within a lane is the task's persisted
  `position`; reorder by drag (UI) or `PUT /api/lanes/{name}/order`.
- **Func tasks** — the second task kind (below), registered in-process by
  another module rather than typed in through the UI/API.

---

## Func (Go-function) tasks

Alongside shell tasks, a lane can run **func tasks**: registered Go
callbacks, not shell commands. A module — currently only utuber — calls
`golane.Host.RegisterLane(spec, kinds...)` against the shared taskmaster
`*Engine` (`internal/taskmaster/golane`) to declare an **owned lane** (a
lane with `owner != ""`) and the `Kind`s it runs on that lane. Kinds are
named `<owner>.<verb>` (e.g. `utuber.download`), each declaring an integer
`Version`, a strict JSON `Decode` (`json.Decoder.DisallowUnknownFields`,
version must match exactly), and a `Run(ctx, rc, payload)` callback.

How a func task differs from a shell task:

- **No shell, no sudo.** A func task's `command` is always `""`, `sudo` is
  always `0`, and it never touches the sudo gate. It runs entirely in-process.
- **One-shot only.** `repeat=0`, `cooldown_seconds=0` — a func task never
  re-fires on a cooldown; a new one is submitted by calling `Lane.Submit`
  again (e.g. utuber's `/enqueue`).
- **Not editable over plain task HTTP.** `POST /api/tasks` rejects a request
  that sets `kind` (400 — `"kind" cannot be set over HTTP`); `PUT`, `POST
  .../pause|resume`, `POST .../up-next` and `POST .../move` against an
  existing func task all answer **409** (`task "<n>" is managed by module
  "<o>"`, or, for up-next, `use POST /api/executions/{id}/rerun for
  module-managed tasks`). `DELETE /api/tasks/{name}` on a func task with a
  running execution is **409**; otherwise it removes the task via the same
  path the retention pruner uses (below).
- **Payload/result live on the row.** `tasks.payload` (JSON, capped at
  `golane.MaxPayloadBytes` = 64 KiB) is the callback's input, validated by
  `Decode` both at submit time and again at the start of every run (so a row
  written by an older binary fails that one execution cleanly instead of
  crashing). `task_executions.result` is the callback's JSON return value,
  populated only on success and also capped at 64 KiB.
- **Cancel reaches queued jobs too.** For a shell task, `POST
  /api/executions/{id}/cancel` on a still-pending (queued) execution answers
  `200 {"status":"not_running"}` — cancelling a queue slot). For a func task,
  the same call on a pending execution actually cancels it:
  `200 {"status":"canceled"}`. A running func task's cancel still answers
  `{"status":"canceling"}`, the same as shell.
- **Metrics group by kind, not by task name.** See "Func-task metrics" below.
- **Width/order/pause/resume on the *lane*** (not the task) work the same as
  any lane — an owned lane's width, position order, and pause state are
  fully controllable through the normal lane routes and UI. Only per-task
  mutation is blocked.

A lane an owning module registers is invisible as a *config* lane: any
`taskmaster.lanes` entry in the server config that names an already-owned
lane is skipped at every boot rather than overwritten (see "Config lanes vs
owned lanes" below).

---

## Progress reporting

A running task — currently only a func task — can report progress through
`golane.RunContext`:

```go
rc.Progress(pct, "Downloading")  // pct 0-100; pct < 0 = indeterminate
rc.SetLabel("Show — Episode")     // retitle the task, immediately, <= 200 runes
```

- `Progress` clamps `pct` to `[0,100]`, or `nil` (indeterminate) for any
  negative value, and truncates `label` to 120 runes.
- **In-memory value is unthrottled.** Every `Progress` call updates an
  in-memory `ProgressRegistry` entry immediately, which `GET
  /api/executions` and the board overlay for `running` rows read — so the
  live progress bar is always current between polls.
- **DB persistence and the `task-progress` board event are throttled** to at
  most once per `progress_interval_ms` (config, see below), except: the
  first report for an execution always persists immediately, and a **label
  change always persists immediately** even if it lands inside the throttle
  window — so a phase change like "Downloading" → "Converting" is never
  dropped by the throttle. A final write happens when the execution
  finishes, regardless of the throttle.
- Shell tasks have the same storage, API and UI (progress bar renders for
  any execution with a non-null `progress_pct`/`progress_label`), but ship
  with **no reporter** — nothing currently writes progress for a shell
  task. Adding one later (e.g. a stdout marker parser) needs no schema, API
  or UI change.

**Config: `progress_interval_ms`.** `taskmaster.progress_interval_ms` in the
server config sets the throttle above. Default (and floor-matching value)
**2000**. `config.Load` normalizes it: `0` → default; negative → a load
error (`taskmaster: progress_interval_ms must be >= 0`); below 100 or above
2000 is clamped to that bound with a log line (`taskmaster:
progress_interval_ms %d below minimum %d; clamping` / `… exceeds %d
(progress must update at least every 2s); clamping`) — so progress can never
be configured to update slower than every 2 seconds.

---

## Config reference

Section `taskmaster` in the server config (`internal/platform/config.TaskmasterConfig`):

```json
"taskmaster": {
  "static_dir": "./web/taskmaster",
  "db_path": "./data/taskmaster/taskmaster.db",
  "lanes": [
    { "name": "default", "width": 2 },
    { "name": "sequential", "width": 1 }
  ],
  "allow_sudo": false
}
```

| Field | Type | Meaning |
|---|---|---|
| `static_dir` | string | Directory serving the module's frontend (`web/taskmaster`); mounted at `/` via `platform/static`. |
| `db_path` | string | Path to the SQLite database file. The parent directory is created (`MkdirAll`) if missing. `~` and env vars are expanded. WAL mode + `SetMaxOpenConns(1)` (single-writer by construction). |
| `lanes` | array | Lanes upserted into the DB at **every** boot, by `name`. **Config is authoritative for width on every restart, not the DB** — `seedLanes` calls `UpsertLane`, whose `ON CONFLICT DO UPDATE SET width = excluded.width` overwrites the lane's width from this file on every boot, even if it was changed since via the API/UI. If you resize a lane through the UI, either also update this file or expect the next restart to revert it. Each entry: `name` (string), `width` (int, max concurrently-running tasks in the lane). **Skipped entirely for owned lanes** — see "Config lanes vs owned lanes" below, where the DB genuinely is authoritative. |
| `allow_sudo` | bool | Default **false**. Gates whether any task in this module instance may run with `sudo`. **Runtime-togglable** — the DB is authoritative once seeded from this config value; flip it live via the UI or `POST /api/capabilities`, and the change survives restarts. See [Security](#security). |
| `progress_interval_ms` | int | Throttle for func-task progress persistence and the `task-progress` board event. Default 2000, clamped to [100, 2000]. See [Progress reporting](#progress-reporting). |

### Config lanes vs owned lanes

`taskmaster.lanes` (above) and a module's `golane.RegisterLane` call both
create/touch rows in the same `lanes` table, but they never step on each
other:

- **`seedLanes`** (run from every config lane at `Open`, unless the engine
  is headless — see below) **skips any lane that already exists with
  `owner != ""`**, and logs `taskmaster: config lane "<name>" is owned by
  module "<owner>"; ignoring its config entry`. A config lane can never
  overwrite an owned lane's width, even though `seedLanes` otherwise upserts
  unconditionally.
- **`RegisterLane`** (a module's side) calls `EnsureOwnedLane`, which fails
  with `ErrLaneOwnedByOther` if a lane by that name already exists **without**
  an owner (i.e. it was created as a plain config/UI lane). The error names
  the conflict and the fix: `lane "<name>" already exists as a regular
  taskmaster lane (from config taskmaster.lanes or the UI); rename or delete
  it`. The owning module then fails its own `Build`/`Open` and that module
  alone answers 503 — taskmaster itself is unaffected.

In short: whichever side claims a lane name first (config seeding, or a
module's `RegisterLane`) keeps it; the loser is rejected loudly rather than
silently overwritten.

### Headless "owned lanes only" mode

When a module that owns a lane (e.g. utuber) is routed to a hostname but
**taskmaster itself is not**, `cmd/server` still opens the shared engine —
just with `taskmaster.OpenOptions{OwnedLanesOnly: true}`. In that mode:

- The worker's `poll` loop schedules **only** lanes with `owner != ""` —
  every config/UI lane (`owner == ""`) is skipped entirely, so routing only
  utuber never starts running arbitrary shell tasks (including sudo tasks,
  if `allow_sudo` happens to be persisted true) with no UI in front of them.
- `seedLanes` does not run at all (there is no taskmaster config surface to
  seed from in this mode).
- `Engine.Handler()` returns `nil` — no taskmaster HTTP routes exist, and
  none are mounted. The owning module's own routes (e.g. utuber's
  `/jobs.json`) still work normally against the same engine.
- Boot reconciliation (`ReconcileOrphans`) still runs over **every** row
  regardless of mode — a still-genuinely-running shell task from a previous
  taskmaster-routed boot is never silently abandoned just because this boot
  doesn't serve the shell UI.

This is what lets a deployment route only `utuber` (no `taskmaster` hostname
at all) while utuber's download lane still runs on the same shared engine,
DB, and worker as it would if taskmaster were also routed.

There is no `auth.modules` requirement specific to taskmaster — like every
other module, add a `taskmaster` entry under `auth.modules` (e.g.
`{"pin_file": ""}` for LDAP + API key only, matching `multissh`) to put it
behind the platform login gate. With no entry, the module is open.

---

## Scheduling semantics — "we're not cron"

- **Poll interval:** the worker loop wakes every **5 seconds**, cleans up
  expired locks, then (unless the [hand brake](#cancel--hand-brake) is
  engaged) queries eligible tasks (enabled, not paused, due) and fills each
  lane's free slots — `width - running`, computed per lane — from the ready
  candidates in that lane's **position order**. A lane at capacity is simply
  skipped for that poll.
- **Cooldown is a minimum *rest*, not a cadence.** After a task finishes it
  must cool off *at least* `cooldown_seconds` before becoming eligible again
  — but it actually runs only when its lane has a free slot and it's next in
  position order, which may be much later. A task not running for 10 minutes
  because a lane-mate was occupying the only slot is correct, expected
  behavior, not a bug.
- **Hard non-goals: no wall-clock scheduling.** No cron expressions, no
  time-of-day, no guaranteed interval. The only clock that matters is
  *finished-plus-rest*. "Run nightly at 2am" is not a taskmaster feature —
  if you need that, point cron (or anything else with a clock) at
  `POST /api/tasks/{name}/up-next`; that endpoint is the seam between the
  world's clock and taskmaster's lanes.
- **Locks:** before starting a task, the worker acquires a per-task lock
  with a **10-minute TTL** (`AcquireLock`/`ReleaseLock`, table
  `task_locks`). A task normally releases its own lock when its execution
  finishes; the TTL exists so a worker crash or a task that runs longer
  than expected doesn't wedge that task forever — the lock expires and
  another poll can pick it back up. **If you expect a task to legitimately
  run longer than 10 minutes, don't rely on the TTL as a safety net** —
  give it a `cooldown_seconds` long enough that a stray restart doesn't
  double-run it.
- **Repeat vs. one-shot:** a task with `repeat: true` re-becomes eligible
  `cooldown_seconds` after its last execution finishes; `repeat: false`
  tasks run once per manual "up next" (or once, ever, if never triggered
  again).
- **Up next:** `POST /api/tasks/{name}/up-next` creates (or reuses, if one
  is already `pending`) an execution row immediately, independent of the
  repeat/cooldown clock — this is how the UI/CLI put a task at the front of
  its lane's queue on demand. It is deliberately *not* called "run now" or
  "enqueue" — the task still runs only when its lane's turn and a free slot
  line up.

---

## API route table

All routes below live under the module's own hostname (`host_routing`
target) and are wrapped by the platform gate/body-limit/origin-check
  middleware like any other module (`docs/guides/adding-a-module.md`) — no
taskmaster-specific auth code exists.

| Method | Path | Purpose |
|---|---|---|
| GET | `/api/health` | DB-aware health check (`{"status":"ok"}` or 503) — see the runbook note below vs. platform `/healthz`. |
| GET | `/api/capabilities` | `{"allow_sudo": bool}` — what the UI uses to show/hide the sudo control. |
| POST | `/api/capabilities` | `{"allow_sudo": bool}` — toggles sudo gating at runtime and persists it (DB-authoritative from then on). |
| GET | `/api/lanes` | List lanes, each with `running_count`. |
| POST | `/api/lanes` | Create a lane (`name`, `width`; width defaults to 1 if omitted/≤0). |
| GET | `/api/lanes/{name}` | Get one lane. |
| PUT | `/api/lanes/{name}` | Update a lane (currently: `width`). |
| DELETE | `/api/lanes/{name}` | Delete a lane. |
| POST | `/api/lanes/{name}/pause` | Pause a lane (running tasks finish; no new ones start). |
| POST | `/api/lanes/{name}/resume` | Resume a paused lane. |
| PUT | `/api/lanes/{name}/width` | Set only a lane's width, leaving its paused state untouched. |
| PUT | `/api/lanes/{name}/order` | Reorder tasks within a lane: body `{"order": ["task-a","task-b",...]}` sets each named task's `position` to its index in the list. Names outside this lane are silently ignored (safe against a stale client snapshot). |
| GET | `/api/tasks` | List tasks (optional `?lane=` filter, by lane name). |
| POST | `/api/tasks` | Create a task. 403 if `sudo:true` and `allow_sudo` is false; 400 if `lane_name` doesn't name an existing lane. **400** if `kind` is set (func tasks can't be created over HTTP); **400**/**409** if the target lane/an existing task by that name is owned by a module — see [Func tasks](#func-go-function-tasks). |
| GET | `/api/tasks/{name}` | Get one task. |
| PUT | `/api/tasks/{name}` | Update a task (partial). Re-validates the effective post-merge `sudo` flag the same way creation is validated, so flipping `sudo` on via update is caught too. Unknown JSON keys are **400** `unknown or read-only field "<k>"` (allow-listed: `lane_name, enabled, paused, cooldown_seconds, repeat, command, position, sudo, output_file`). A func task is **409**. |
| DELETE | `/api/tasks/{name}` | Delete a task. A func task with a running execution is **409**. |
| POST | `/api/tasks/{name}/pause` | Pause a task. A func task is **409**. |
| POST | `/api/tasks/{name}/resume` | Resume a task. A func task is **409**. |
| POST | `/api/tasks/{name}/up-next` | Put the task at the front of its lane's queue now; returns `{"execution_id": N}`. A func task is **409** (`use POST /api/executions/{id}/rerun for module-managed tasks`). |
| POST | `/api/tasks/{name}/move` | Move a task to a different lane: body `{"lane_name": "..."}`. 400 if the target lane doesn't exist or is owned. A func task is **409**. |
| GET | `/api/executions` | List executions (optional `?task=`, `?limit=`). `running` rows carry the live `progress_pct`/`progress_label` overlay. |
| GET | `/api/executions/{id}/output` | SSE stream of an execution's output (see below). |
| POST | `/api/executions/{id}/cancel` | Force-kill (SIGKILL) a running execution. For a **pending func-task** execution, this cancels the still-queued row instead (`200 {"status":"canceled"}`) — see [Func tasks](#func-go-function-tasks). 404 if it isn't currently running/registered/pending-func. See [Cancel + hand brake](#cancel--hand-brake). |
| POST | `/api/executions/{id}/pause` | Suspend (SIGSTOP) a running shell execution's process group. No effect on a func task (no PID). |
| POST | `/api/executions/{id}/resume` | Resume (SIGCONT) a suspended execution. |
| POST | `/api/executions/{id}/rerun` | Re-queue a **finished** execution's task with a new pending execution. `400` non-integer id; `404` unknown id or hidden lane; `409` execution not finished; `409` task already has a pending/running execution; `409` (func task only) latest execution already `success` — a succeeded one-shot job is done, re-run it explicitly instead (see [Func tasks](#func-go-function-tasks)); otherwise `201 {"execution_id": N}`. Shell tasks may re-run from any terminal status (`success`, `failed`, or `canceled`); a func task refuses `success`. |
| GET | `/api/metrics` | Per-task success/failed/canceled counts and duration stats (optional `?lane=`, `?task=`, `?hours=`). Func tasks are aggregated by kind, not by individual task name — see [Func-task metrics](#func-task-metrics). |
| GET | `/api/board/events` | SSE stream of compact board-change events for the live lane board. See [Board-events SSE](#board-events-sse). |
| GET | `/api/brake` | `{"engaged": bool}` — whether the hand brake is currently on. |
| POST | `/api/brake` | Engage the hand brake. See [Cancel + hand brake](#cancel--hand-brake). |
| DELETE | `/api/brake` | Release the hand brake. |

Error responses use the platform envelope (`{"error": "..."}`,
`internal/platform/response`).

---

## Cancel + hand brake

Two related but distinct controls, both built on the same per-execution
cancelable context (each running execution gets its own child context,
registered by execution ID, so it can be force-killed without tearing down
the whole worker):

- **Per-execution cancel** (`POST /api/executions/{id}/cancel`) force-kills
  (SIGKILL, via the same `exec.CommandContext` + `WaitDelay` machinery used
  for shutdown) exactly one running execution. Its terminal status is
  recorded as **`canceled`** — a distinct state from `failed`, so
  history/metrics can tell an operator-initiated stop apart from a real
  failure. Returns 404 if the execution isn't currently running/registered
  (already finished, or never started).
- **Hand brake** (`POST`/`DELETE /api/brake`, `GET /api/brake` to check) is
  a global, durable emergency stop:
  - Engaging it **pauses every lane that isn't already paused** (recording
    which ones it paused, so release only un-pauses those — a lane paused
    independently of the brake stays paused) and **cancels every currently
    running execution** the same way per-execution cancel does (all
    recorded `canceled`).
  - It **latches**: nothing new launches anywhere until the operator
    explicitly releases it.
  - It is **persisted in the DB** (like `allow_sudo`), so it **survives an
    unclean restart** — a server that comes back up while the brake was
    engaged boots braked and launches nothing until released. A restart
    never silently un-pauses a braked deployment.
  - Releasing it restores exactly the pre-brake pause state of the lanes it
    paused, then clears the persisted flag.

**Root-owned-sudo-child caveat (both controls):** a task running via `sudo`
spawns a **root-owned** child process. The unprivileged unified-webapp
process can ask it to terminate but cannot force-kill it if the signal is
ignored or `sudo`'s own child-reaping gets in the way — such a process may
outlive the cancel/brake and keep running until it finishes or an operator
kills it directly. The execution row is still marked `canceled` from
taskmaster's point of view; the underlying command's actual lifetime is not
guaranteed. This is the same limitation documented for shutdown, below.

---

## Func-task metrics

`GET /api/metrics` groups **func-task** rows by `(lane, kind)`, not by
individual task name (P17): every `utuber.download` execution in the
`utuber` lane rolls into one metrics card, with `task_name` reported as the
kind (e.g. `utuber.download`) and a new `kind` field carrying the same
value. Without this, every download would get its own randomly-named task
(`utuber-<hex>`) and its own one-off metrics card — noise, not a signal.
Shell tasks are unaffected: they keep the existing per-task grouping.

Task-detail metrics (`GET /api/metrics?task=<name>`) for one func task's own
name still work: because the row is grouped by kind, the single matching
row (`task_name == kind`) is that job's own aggregate — not the whole lane's.

## Retention and pruning (owned lanes)

Each **owned** lane (one created via `RegisterLane`, e.g. utuber's `utuber`
lane) carries a `retention_days` value (`lanes.retention_days`, seeded by
the module's `LaneSpec.InitialRetentionDays`; `0` means "never prune" — the
default for every config/UI lane). A background pruner:

- runs once at `Open`,
- then on an hourly ticker (`pruneInterval`, 1 hour in production — tests
  can shorten it via `taskmaster.SetPruneIntervalForTest`),
- and again immediately after a retention change (via the module's own
  settings UI, since that goes through `Lane.UpdateSettings`).

For each lane with `retention_days > 0`, it deletes every **func** task
(`kind != ''`) in that lane whose latest execution finished before
`now - retention_days` and that has nothing pending or running — the task
row, its executions, its metrics, and any lock row all go together
(`PruneOwnedLane`/`RemoveIdleTask`). A shell task in the same lane is never
touched by this pruner, and a func task with an in-flight execution is never
pruned regardless of age. Each sweep that removes anything logs
`taskmaster: pruned N job(s) from lane "<lane>" finished before <RFC3339
cutoff>`.

This is a narrowing of the older "`task_metrics` retained indefinitely"
guidance below: that guidance still holds for **shell** task metrics — there
is no automatic pruning for those — but a pruned/removed func task's metrics
are deleted along with it.

---

## Board-events SSE

`GET /api/board/events` is a `text/event-stream` of compact JSON change
events, meant to drive the live lane board without polling or full-page
repaints:

```
data: {"type":"task-started","lane":"default","task":"backup","execution_id":42}
data: {"type":"task-finished","lane":"default","task":"backup","execution_id":42,"status":"success"}
data: {"type":"task-enqueued","lane":"default","task":"backup","execution_id":43}
data: {"type":"lane-updated","lane":"default"}
data: {"type":"brake","engaged":true}
```

Only the fields relevant to a given `type` are set (others are omitted from
the JSON). This stream **does not replay history on connect** — a client is
expected to fetch a snapshot via the REST endpoints (`GET /api/lanes`,
`GET /api/tasks`) first, then subscribe here for subsequent changes. It
shares the same `sse_max_subscribers` cap as the per-execution output SSE
below (503 before any SSE header is written once the cap is hit).

---

## SSE event format (per-execution output)

`GET /api/executions/{id}/output` is a standard `text/event-stream`
response, capped by `server.sse_max_subscribers` (503 before any SSE header
is written if the cap is already hit). Three event types:

- **`output`** — one line of captured stdout/stderr:
  ```
  event: output
  data: {"stream":"stdout","line":"tick 1","ts":"2026-09-12T10:00:00.000Z"}
  ```
- **`status`** — sent only when the execution isn't (or is no longer) held
  in the in-memory output registry (see the replay-window note below); the
  event's `data` is the bare execution status string (e.g. `running`,
  `success`, `failed`, `canceled`), not JSON.
- **`done`** — `data: {}` — sent once both stdout and stderr streams have
  closed; the client should close its `EventSource` on receipt.

On connect, any already-captured lines are replayed first (oldest to
newest, stdout then stderr), then the stream switches to live delivery via
per-line subscriber channels. If the client is already fully caught up
(both streams already closed), `done` is sent immediately after replay with
no live phase.

The frontend (`web/taskmaster/js/taskdetail.ts`) consumes this with a
native `EventSource` — no bespoke fetch/ReadableStream reader, no auth
header (the platform session cookie rides along automatically); it
registers `output`/`status`/`done` listeners and closes the `EventSource`
on `done`, on stream error, or on page navigation.

---

## `output_file` caveat

A task may set `output_file` to also tee its captured output to a file on
disk (in addition to the in-memory ring buffer + DB record), e.g.
`/var/log/taskmaster/{task}.log` — the placeholders `{exec_id}` and `{task}`
are substituted with the execution's numeric ID and the task's name. Be
aware:

- The file is written **as the service user** the unified-webapp process
  runs as (or as `root`, if the specific task also runs with `sudo` and the
  command itself writes there — taskmaster's own tee always runs
  unprivileged).
- Writes go **outside the SQLite DB** — they are not part of any backup/
  retention story the DB has, and are not visible through the API/UI.
  Losing or rotating the file loses that copy of the output (the DB/replay
  registry copy is unaffected).
- **There is no path confinement in v1** — `output_file` accepts any path
  the service user can write to; there's no `platform/fspath` allow-list or
  root-jail applied to it. Treat any operator who can create/edit tasks as
  able to write arbitrary files the service account has access to.

---

## Shutdown semantics

Server shutdown (SIGTERM/SIGINT reaching `cmd/server`, or a test calling
`Close()` on the built handler) does the following, in order:

1. Cancels the worker's context. Every in-flight task's command was started
   with `exec.CommandContext(ctx, ...)`, so cancellation delivers a kill
   signal to each running command immediately.
2. Each command's `cmd.WaitDelay` is **10 seconds** — bounding how long
   `cmd.Wait()` blocks after cancellation even if the process doesn't die
   promptly (a backgrounded grandchild holding the stdout/stderr pipes
   open, or a `sudo`-spawned root-owned child the unprivileged service
   can't signal). After the delay, `Wait` force-closes the pipes and
   returns rather than hanging the shutdown.
3. The worker joins (`Wait()`s on) every in-flight `runTask` goroutine, so
   shutdown is deterministic and leak-free (this is what the module's
   `goleak` test gate depends on) rather than abandoning goroutines.
4. Any execution that was interrupted this way is recorded **`failed`** in
   the database, with the context-cancellation error as its error message
   — this is a whole-worker shutdown, not a per-execution/hand-brake
   cancel, so it is not recorded `canceled` (that distinction is reserved
   for an explicit operator-initiated stop; see
   [Cancel + hand brake](#cancel--hand-brake)). It is not silently left
   `running` or retried automatically.
5. The output GC goroutine is stopped, then the database connection is
   closed.

**Important caveat:** a `sudo`-run task's actual child process is
**root-owned**. The unprivileged unified-webapp process can ask it to
terminate but cannot force-kill a process it doesn't own if the signal is
ignored or `sudo`'s own child-reaping gets in the way — such a process
**may outlive the server** past the 10-second `WaitDelay` window. The
execution row is still marked `failed` (from taskmaster's point of view the
task was interrupted), but the underlying command may keep running until it
finishes or an operator kills it directly. This is a known, accepted
consequence of config-gated sudo (see [Security](#security)) — plan
restarts of an `allow_sudo: true` deployment accordingly.

---

## Output replay-window semantics

Captured stdout/stderr for an execution lives in an in-memory
`OutputRegistry`, keyed by execution ID, independent of the SQLite DB. It is
available for replay (via the SSE endpoint) for **up to ~1 hour after the
execution finishes** — the window is stamped from `FinishedAt`, not from
when the execution started, so a long-running task keeps its **full**
post-finish replay window regardless of how long it ran. A background
reaper tick runs every **5 minutes**, sweeping any execution whose
finish-stamped expiry has passed.

Once an execution's output has been reaped (or if the server restarted
since it ran), `GET /api/executions/{id}/output` falls back to a single
`status` event carrying the execution's terminal status from the database,
then closes — the line-by-line output itself is gone; only the DB's
summary fields (status, duration, error message) survive indefinitely.

---

## `task_metrics` retention guidance

`task_metrics` rows (one per finished execution: duration, schedule delay,
status — success/failed/canceled) are **retained indefinitely** by design —
there is no automatic pruning in v1. For long-running deployments, prune
periodically, e.g.:

```sql
DELETE FROM task_metrics WHERE recorded_at < <cutoff-epoch-ms>;
```

or scoped to one task (ported from the reference CLI's delete-metrics
verb):

```sql
DELETE FROM task_metrics WHERE task_id = <id>;
```

Run this with the server stopped, or accept that it competes with the
single writer connection (`SetMaxOpenConns(1)`) for a moment under WAL.

---

## `taskmasterctl` usage

`taskmasterctl` is a small CLI that drives the same API as the UI, useful
for scripting and CI. Build it with `make build` (or
`go build -o taskmasterctl ./cmd/taskmasterctl`); an `arm64` Linux build is
produced by `make build-rpi`.

```
taskmasterctl [-url URL] [-key KEY] <noun> <verb> [flags]

nouns: lane | task | executions | output | cancel | brake | metrics | health
```

```
lane verbs:
  lane list
  lane create -name NAME [-width N]
  lane update <name> [-width N]
  lane delete <name>
  lane pause <name>
  lane resume <name>
  lane width <name> <n>
  lane order <name> <t1,t2,...>

task verbs:
  task list [-lane L]
  task add -name NAME -lane LANE -command CMD [-repeat] [-cooldown N]
           [-sudo] [-enabled] [-output-file PATH]
  task update <name> [-command CMD] [-cooldown N] [-repeat] [-no-repeat]
              [-enabled] [-disabled] [-sudo] [-no-sudo] [-output-file PATH]
  task pause <name>
  task resume <name>
  task delete <name>
  task up-next <name>
  task move <name> <lane>

cancel <execution-id>
brake [on|off]          (no verb: prints "engaged"/"released")
metrics [-lane L] [-task T] [-hours N]
```

Every request sends `Authorization: Bearer <key>` (including `health`) —
there is no session/cookie mode, matching the platform's API-key auth path.
The target module must have an `auth.modules` entry (any shape) for API
keys to be accepted at all, and the request's `Host` header must route to
`taskmaster` (`host_routing`) — if you can't set a custom `Host` header from
your environment, hit it via its `.test`/real hostname directly instead of
by IP.

**Config resolution — three mechanisms, in this precedence order:**

1. **Flags:** `-url http://taskmaster.test:8080 -key <key>`
2. **Environment:** `TASKMASTER_URL`, `TASKMASTER_KEY`
3. **Config file:** `~/.taskmasterctl.json` — `{"url": "...", "key": "..."}`,
   must be `0600` (a looser mode gets a stderr warning, not a hard failure).

An empty resolved URL is a fatal usage error that names all three
mechanisms; there's no default-port guessing.

**Minting a key:** taskmaster has no key-management UI of its own — API
keys are platform-wide, minted from the **admin** module (`auth.api_keys`)
or `go run ./cmd/server -gen-api-key`, then added to `auth.api_keys` in the
server config. Any resulting key works against taskmaster the same way it
works against any other protected non-admin module (see the flattening
risk in [Security](#security)).

On a 401/403, the CLI prints:

```
authentication failed: check the API key (minted in the admin panel) and that taskmaster has an auth.modules entry
```

**SSE follow:** `taskmasterctl output <execution-id|task-name>` line-scans
the same SSE stream the browser UI uses, printing each `output`/`status`/
`done` event as it arrives, with no client-side timeout — it follows a
running execution to completion. Passing a task name instead of a numeric
ID resolves to that task's most recent execution first.

---

## Security

Taskmaster is deliberately capable of running arbitrary commands as the
service user (and, when configured, via `sudo`). These are **known,
accepted risks for v1**, not open questions:

1. **Sudo gating is coarse and binary.** `allow_sudo: true` enables sudo
   for *every* task author on this module instance, for *any* command your
   `sudoers` configuration permits the service account to run. There is no
   per-task, per-command, or per-user granularity — flipping the boolean
   (in config, or live via `POST /api/capabilities`) can turn "a web-
   authenticated user can run commands as the service user" into "…as
   root," depending on how permissive `sudoers` is.
2. **The real policy lives in `sudoers`, which taskmaster neither reads nor
   validates.** The module cannot tell an operator what `allow_sudo`
   actually grants on a given host — that's entirely a function of the
   host's `sudoers` file.
3. **API-key flattening.** Platform API keys are valid on **every**
   protected non-admin module, not just taskmaster. A key minted for some
   low-stakes module can create tasks and put them up next. Until the
   platform grows scoped/per-module keys, treat **every** API key in the
   fleet as taskmaster-grade.
4. **Arbitrary exec is inherent, sudo or not.** Even with `allow_sudo:
   false`, taskmaster is, by design, remote command execution as the
   service user (every task command runs through `sh -c`). The auth gate is
   the only control in front of it — there is no command allow-listing,
   sandboxing, or resource limiting. Removing the old task "type" selector
   from the UI does not change this: a shell-executed command line was
   already the underlying reality, just previously dressed up as a type
   choice.

**Intended future direction:** replace the `allow_sudo` flag with a
curated, separately-audited **setuid-root helper** that hard-codes a fixed
allow-list of privileged operations (an "own sudo" taskmaster can call
directly, dropping the `sudoers` dependency entirely), plus platform-level
scoped/per-module API keys to close the flattening risk. See
  `docs/archived/taskmaster-ui-FRD-14Sept2026.md` /
  `docs/archived/taskmaster-ui-plan-26Sept2026.md` for the rationale.

**Until that lands:** only enable `allow_sudo` on a deployment where every
holder of a platform API key, and every user who can log into taskmaster at
all, is someone you'd trust with `sudo` on the host directly.

---

## Schema v5: no downgrade

Migration 5 (the func-task/owned-lane/progress support documented above)
adds columns to `lanes`, `tasks` and `task_executions` — `owner`, `hidden`,
`retention_days`, `kind`, `label`, `payload`, `payload_version`,
`progress_pct`, `progress_label`, `result` — plus an index. It is the first
**transactional** migration (`tx: true`): it runs inside a single
transaction and rolls back atomically on any failure, unlike migrations 1–4.

**Do not run an older (pre-migration-5) taskmaster binary against a database
that has already been migrated to v5.** An older binary's SQL was written
before these columns existed:

- Any older-binary code path that builds a column list by hand (e.g. the
  scan/insert code for lanes, tasks, or executions) will not know about the
  new columns and will either error or silently drop them, and reads that
  expect the old column count will fail outright.
- An owned lane's rows (`owner != ''`) are invisible/meaningless to an older
  binary's logic — it has no concept of a module-owned lane, so it may treat
  a func task as an ordinary shell task with an empty `command`, which is
  not a task an older binary can usefully run (K9).
- The older binary never wrote `schema_version = 5`, so if it opens the
  database and then a newer binary reopens it later, the newer binary
  re-runs migration 5's `ALTER TABLE ... ADD COLUMN` statements — which fail
  because those columns already exist — unless the intervening older-binary
  session never touched `schema_version` (in which case it's still fine).
  The real risk is not a corrupt `schema_version` row; it's data written by
  an older binary that the newer schema's owned-lane/func-task machinery
  then encounters and cannot make sense of.

**If you've already downgraded and something looks wrong:** the two ways
back are

1. **Restore from a backup taken before the v5 migration ran** (before
   upgrading in the first place), and stay on the older binary — you lose
   any taskmaster activity recorded since that backup.
2. **Accept the newer binary going forward.** Re-upgrade to the binary that
   applied migration 5 (or newer) and stay there. Migration 5 is additive
   (new columns, not renamed/removed ones), so a straight re-upgrade with no
   intervening older-binary writes is safe and loses nothing.

There is no supported automatic downgrade path (no migration "down" step is
implemented, matching migrations 1–4). Plan any rollback around one of the
two recovery options above, not around running the old binary directly
against the migrated file.
