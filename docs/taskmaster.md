# Taskmaster

A command runner: define **tasks** (each a single shell command) that live
in ordered, width-limited **lanes**; run them on demand or let them repeat
after a cooldown *rest*; watch their output live over SSE; and drive
everything from a live lane board. Ported from the standalone
`continuous-task-runner-queue` reference project into unified-webapp's
module contract (`docs/adding-a-module.md`); the object-model/UI rework
described here is specced in `taskmaster-ui-FRD.md` and
`taskmaster-ui-plan.md`.

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
- **No task "type".** A task is a single **command** string, run through a
  shell (`sh -c "<command>"`). There is no `exec`/`shell`/`script`/
  `migration` selector, and no JSON `args` blob — those are gone.
- **No `priority`.** Ordering within a lane is the task's persisted
  `position`; reorder by drag (UI) or `PUT /api/lanes/{name}/order`.

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
| `lanes` | array | Lanes seeded into the DB at every boot (upsert by `name` — the DB is authoritative afterward, so editing a lane's width via the API/UI persists across restarts even though it isn't reflected back into this file). Each entry: `name` (string), `width` (int, max concurrently-running tasks in the lane). |
| `allow_sudo` | bool | Default **false**. Gates whether any task in this module instance may run with `sudo`. **Runtime-togglable** — the DB is authoritative once seeded from this config value; flip it live via the UI or `POST /api/capabilities`, and the change survives restarts. See [Security](#security). |

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
middleware like any other module (`docs/adding-a-module.md`) — no
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
| GET | `/api/tasks` | List tasks (optional `?group=` filter, by lane name — a legacy query-param name kept from the pre-rework API). |
| POST | `/api/tasks` | Create a task. 403 if `sudo:true` and `allow_sudo` is false; 400 if `lane_name` doesn't name an existing lane. |
| GET | `/api/tasks/{name}` | Get one task. |
| PUT | `/api/tasks/{name}` | Update a task (partial). Re-validates the effective post-merge `sudo` flag the same way creation is validated, so flipping `sudo` on via update is caught too. |
| DELETE | `/api/tasks/{name}` | Delete a task. |
| POST | `/api/tasks/{name}/pause` | Pause a task. |
| POST | `/api/tasks/{name}/resume` | Resume a task. |
| POST | `/api/tasks/{name}/up-next` | Put the task at the front of its lane's queue now; returns `{"execution_id": N}`. |
| POST | `/api/tasks/{name}/move` | Move a task to a different lane: body `{"lane_name": "..."}`. 400 if the target lane doesn't exist. |
| GET | `/api/executions` | List executions (optional `?task=`, `?limit=`). |
| GET | `/api/executions/{id}/output` | SSE stream of an execution's output (see below). |
| POST | `/api/executions/{id}/cancel` | Force-kill (SIGKILL) a running execution; 404 if it isn't currently running/registered. See [Cancel + hand brake](#cancel--hand-brake). |
| GET | `/api/metrics` | Per-task success/failed/canceled counts and duration stats (optional `?group=` [lane], `?task=`, `?hours=`). |
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
`taskmaster-ui-FRD.md` / `taskmaster-ui-plan.md` for the rationale.

**Until that lands:** only enable `allow_sudo` on a deployment where every
holder of a platform API key, and every user who can log into taskmaster at
all, is someone you'd trust with `sudo` on the host directly.
