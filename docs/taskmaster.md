# Taskmaster

A scheduled/on-demand command runner: define **tasks** (shell/exec/script/
migration commands) grouped into concurrency-limited **groups**, run them
now or on a repeat/cooldown schedule, and watch their output live over SSE.
Ported from the standalone `continuous-task-runner-queue` reference project
into unified-webapp's module contract (`docs/adding-a-module.md`) — see
`taskmaster-FRD.md` and `taskmaster-plan.md` for the full porting rationale.

**Read this before turning on `allow_sudo` or exposing this module beyond a
trusted operator group** — see [Security](#security) below.

---

## Config reference

Section `taskmaster` in the server config (`internal/platform/config.TaskmasterConfig`):

```json
"taskmaster": {
  "static_dir": "./web/taskmaster",
  "db_path": "./data/taskmaster/taskmaster.db",
  "groups": [
    { "name": "default", "pool_limit": 2, "allowed_types": [] },
    { "name": "shell-only", "pool_limit": 1, "allowed_types": ["shell"] }
  ],
  "allow_sudo": false
}
```

| Field | Type | Meaning |
|---|---|---|
| `static_dir` | string | Directory serving the module's frontend (`web/taskmaster`); mounted at `/` via `platform/static`. |
| `db_path` | string | Path to the SQLite database file. The parent directory is created (`MkdirAll`) if missing. `~` and env vars are expanded. WAL mode + `SetMaxOpenConns(1)` (single-writer by construction). |
| `groups` | array | Concurrency groups seeded into the DB at every boot (upsert by `name` — the DB is authoritative afterward, so editing a group via the API/UI persists across restarts even though it isn't reflected back into this file). Each entry: `name` (string), `pool_limit` (int, max concurrently-running tasks in the group), `allowed_types` (string array). |
| `allow_sudo` | bool | Default **false**. Gates whether any task in this module instance may run with `sudo`. See [Security](#security). |

`allowed_types` semantics: **empty list (`[]`) allows every task type**
(`exec`, `shell`, `script`, `migration`); a non-empty list is an allow-list —
only task types named in it may be created in, or moved into, that group.
Enforced on task create and update (including moving a task into a
stricter group, or changing its `task_type` in place).

There is no `auth.modules` requirement specific to taskmaster — like every
other module, add a `taskmaster` entry under `auth.modules` (e.g.
`{"pin_file": ""}` for LDAP + API key only, matching `multissh`) to put it
behind the platform login gate. With no entry, the module is open.

---

## Scheduling semantics

- **Poll interval:** the worker loop wakes every **5 seconds**, cleans up
  expired locks, then queries eligible tasks (enabled, not paused, due) and
  fills each group's free pool slots (`pool_limit - running`) in priority
  order (`priority` — a task's `priority` field is used as a simple ordering
  weight when multiple candidates compete for group slots; default `50`
  when unset).
- **Locks:** before starting a task, the worker acquires a per-task lock
  with a **10-minute TTL** (`AcquireLock`/`ReleaseLock`, table
  `task_locks`). A task normally releases its own lock when its execution
  finishes; the TTL exists so a worker crash or a task that runs longer
  than expected doesn't wedge that task forever — the lock expires and
  another poll can pick it back up. **If you expect a task to legitimately
  run longer than 10 minutes, don't rely on the TTL as a safety net** —
  give it a `cooldown_seconds` long enough that a stray restart doesn't
  double-run it.
- **Repeat / cooldown:** a task with `repeat: true` re-becomes eligible
  `cooldown_seconds` after its last execution finishes; `repeat: false`
  tasks run once per manual `enqueue` (or once, ever, if never enqueued
  again).
- **Enqueue vs. schedule:** `POST /api/tasks/{name}/enqueue` creates (or
  reuses, if one is already `pending`) an execution row immediately,
  independent of the repeat/cooldown clock — this is how the UI/CLI trigger
  an on-demand run.

---

## API route table

All routes below live under the module's own hostname (`host_routing`
target) and are wrapped by the platform gate/body-limit/origin-check
middleware like any other module (`docs/adding-a-module.md`) — no
taskmaster-specific auth code exists.

| Method | Path | Purpose |
|---|---|---|
| GET | `/api/health` | DB-aware health check (`{"status":"ok"}` or 503) — see the runbook note below vs. platform `/healthz`. |
| GET | `/api/capabilities` | `{"allow_sudo": bool}` — what the UI uses to show/hide the sudo checkbox. |
| GET | `/api/groups` | List groups with `running_count`. |
| POST | `/api/groups` | Create a group. |
| GET | `/api/groups/{name}` | Get one group. |
| PUT | `/api/groups/{name}` | Update a group. |
| DELETE | `/api/groups/{name}` | Delete a group. |
| POST | `/api/groups/{name}/pause` | Pause a group (running tasks finish; no new ones start). |
| POST | `/api/groups/{name}/resume` | Resume a paused group. |
| GET | `/api/tasks` | List tasks (optional `?group=` filter). |
| POST | `/api/tasks` | Create a task. 400 if `task_type` isn't in the target group's non-empty `allowed_types`; 403 if `sudo:true` and `allow_sudo` is false. |
| GET | `/api/tasks/{name}` | Get one task. |
| PUT | `/api/tasks/{name}` | Update a task (partial). Re-validates the *effective* post-merge `task_type`/`group_name`/`sudo` triple, so moving a task into a stricter group or flipping `sudo` on is caught the same way creation is. |
| DELETE | `/api/tasks/{name}` | Delete a task. |
| POST | `/api/tasks/{name}/pause` | Pause a task. |
| POST | `/api/tasks/{name}/resume` | Resume a task. |
| POST | `/api/tasks/{name}/enqueue` | Enqueue an execution now; returns `{"execution_id": N}`. |
| GET | `/api/executions` | List executions. |
| GET | `/api/executions/{id}/output` | SSE stream of an execution's output (see below). |
| GET | `/api/metrics` | Per-task success/failure counts and duration stats (from `task_metrics`). |

Error responses use the platform envelope (`{"error": "..."}`,
`internal/platform/response`).

---

## SSE event format

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
  `success`, `failed`), not JSON.
- **`done`** — `data: {}` — sent once both stdout and stderr streams have
  closed; the client should close its `EventSource` on receipt.

On connect, any already-captured lines are replayed first (oldest to
newest, stdout then stderr), then the stream switches to live delivery via
per-line subscriber channels. If the client is already fully caught up
(both streams already closed), `done` is sent immediately after replay with
no live phase.

The frontend (`web/taskmaster/js/output.ts`) consumes this with a native
`EventSource` — no bespoke fetch/ReadableStream reader, no auth header (the
platform session cookie rides along automatically); it registers
`output`/`status`/`done` listeners and closes the `EventSource` on `done`,
on stream error, or on page navigation.

---

## `output_file` caveat

A task may set `output_file` to also tee its captured output to a file on
disk (in addition to the in-memory ring buffer + DB record), e.g.
`/var/log/taskmaster/{task}.log` — the placeholder `{exec_id}` is
substituted with the execution's numeric ID. Be aware:

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
  root-jail applied to it (tracked as a follow-up in `taskmaster-plan.md`'s
  ADR). Treat any operator who can create/edit tasks as able to write
  arbitrary files the service account has access to.

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
   — it is not silently left `running` or retried automatically.
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
consequence of config-gated sudo (see [Security](#security)) — plan restarts
of an `allow_sudo: true` deployment accordingly.

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
status) are **retained indefinitely** by design — there is no automatic
pruning in v1 (see `taskmaster-FRD.md` §9, non-goals). For long-running
deployments, prune periodically, e.g.:

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

nouns: group | task | executions | output | metrics | health
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
running execution to completion.

---

## Security

Taskmaster is deliberately capable of running arbitrary commands as the
service user (and, when configured, via `sudo`). This section summarizes
`taskmaster-FRD.md` §8 — read that section for the full write-up; these are
**known, accepted risks for v1**, not open questions:

1. **Sudo gating is coarse and binary.** `allow_sudo: true` enables sudo
   for *every* task author on this module instance, for *any* command your
   `sudoers` configuration permits the service account to run. There is no
   per-task, per-command, or per-user granularity — flipping one JSON
   boolean can turn "a web-authenticated user can run commands as the
   service user" into "…as root," depending on how permissive `sudoers` is.
2. **The real policy lives in `sudoers`, which taskmaster neither reads nor
   validates.** The module cannot tell an operator what `allow_sudo`
   actually grants on a given host — that's entirely a function of the
   host's `sudoers` file.
3. **API-key flattening.** Platform API keys are valid on **every**
   protected non-admin module, not just taskmaster. A key minted for some
   low-stakes module can create and enqueue taskmaster tasks. Until the
   platform grows scoped/per-module keys, treat **every** API key in the
   fleet as taskmaster-grade.
4. **Arbitrary exec is inherent, sudo or not.** Even with `allow_sudo:
   false`, taskmaster is, by design, remote command execution as the
   service user. The auth gate is the only control in front of it — there
   is no command allow-listing, sandboxing, or resource limiting.

**Intended future direction (out of scope for this port):** replace the
`allow_sudo` flag with a curated, separately-audited **setuid-root helper**
that hard-codes a fixed allow-list of privileged operations (an "own sudo"
taskmaster can call directly, dropping the `sudoers` dependency entirely),
plus platform-level scoped/per-module API keys to close the flattening
risk. See the ADR "Follow-ups" in `taskmaster-plan.md`.

**Until that lands:** only enable `allow_sudo` on a deployment where every
holder of a platform API key, and every user who can log into taskmaster at
all, is someone you'd trust with `sudo` on the host directly.
