# Plan: utuber on a taskmaster lane (Go-function tasks, progress, retention, shared queue panel)

Source FRD: `docs/FRD-utuber-taskmaster-lane.md`
Mode: RALPLAN-DR consensus, **DELIBERATE** (the FRD deletes `internal/utuber/jobs`; schema migration; new delete/prune paths; cross-module wiring change in `cmd/server`)
Status: **APPROVED by owner 2026-09-27 (as-is, v3)** — consensus reached round 2 (Architect SOUND-WITH-CHANGES, Critic APPROVE) + improvements applied. Execution: ralph, Sonnet builders.

Provenance: this file was restored to `docs/` on the owner's instruction, after `.omc/plans/utuber-taskmaster-lane.md` was overwritten with 16 bytes of binary data by an unrelated process. It is **not a from-memory reconstruction**. It was rebuilt by an exact, deterministic replay of the planner's recorded Write/Edit/script operations (v1 write → v2 edits → v3 edits), and every recorded edit re-applied cleanly. The only change from the pre-corruption text is the Phase 6 FRD status pointer, which now names this file's new path. §12 still mentions `.omc/plans/open-questions.md` as it did originally.

All line numbers were verified against the working tree at commit `29ac6c7` (branch `ui-upgrade`). Paths are repo-relative. "TM" = taskmaster.

---

## 0. Binding rules for the executor (read first)

Breaking any rule is a defect even when the tests pass.

- **R1. No `db.conn` or `(*DB)` method calls inside a transaction.** `internal/taskmaster/db/db.go:28` sets `SetMaxOpenConns(1)`. Any `db.conn.*` or `(*DB).Method` call made while a `*sql.Tx` is open blocks forever. Inside a transaction, use only `tx.Exec`/`tx.Query`/`tx.QueryRow`. Every new transactional method goes through the one helper `withTx` (§4.2). If a transaction reads rows and then writes, it reads everything into a slice, closes the cursor, and only then runs the writes. Every test that exercises a tx method must finish within `require.Eventually`/`context.WithTimeout` bounds of 10 s, so a regression fails instead of hanging.
- **R2. Slow work stays outside the DB.** yt-dlp and ffmpeg subprocesses, JSON (un)marshalling of payloads, and `Kind.Decode` all run before a transaction opens or after it commits. The only DB write on the progress path is a single `UPDATE` (no transaction), made at most once per `progress_interval_ms` per execution (§4.9).
- **R3. Shell-task behaviour is frozen.** Every existing test in `internal/taskmaster/...` passes, and only the edits listed in §7.5 are allowed. Shell tasks (`kind = ''`) keep their scheduling, eligibility, cancel, pause/resume, output_file, sudo and shutdown semantics exactly. The only new shell-visible behaviours are the ones listed in §2.3 ("narrowings"). Each has a test.
- **R4. No utuber names in taskmaster core.** After Phase 6, `grep -rni utuber internal/taskmaster | wc -l` prints `0`. The kind name `utuber.download`, the lane name `utuber` and the owner `utuber` are literals in `internal/utuber` only (owner decision Q5).
- **R5. Compile-forced call-site migration.** `utuber.Build` gains a required `golane.Host` parameter, `worker.PublishBoardEvent` gains a `*HiddenLanes` parameter and `coordinator.New` gains two trailing parameters. There are no aliases, so every call site fails to build until it is updated. The complete call-site lists are in §4.12.
- **R6. Named test seams only.** These are the only new seams. Production code never sets them.
  - `worker.SetPollIntervalForTest` and `worker.SetLockTTLForTest` already exist (`worker/worker.go:26-40`).
  - `taskmaster.SetPruneIntervalForTest(d time.Duration)` is a new package var `pruneInterval = time.Hour` in `internal/taskmaster/engine.go`.
  - `worker.beforeFuncClaimHook func(execID int64)` is a package var, nil in production, called in `runFunc` immediately before `ClaimFuncExecution`. Only internal (`package worker`) tests set it.
  - `worker.afterFuncClaimHook func(execID int64)` is a package var, nil in production, called in `runFunc` immediately after a **successful** claim and before the `execCtx.Err()` check.
  - `worker.progressPersistHook func(execID int64, p models.Progress)` is a package var, nil in production, called after each successful progress write (throttled or label-change).
  - `progressReporter.now func() time.Time` is a field set to `time.Now` by `runFunc`. Internal tests construct the reporter directly with a fake clock.
  - **Hook lifecycle (race detector):** a test sets any package-var hook **before** the worker's `Start` (or before its direct `runTask` call), and clears it in `t.Cleanup` registered so that it runs **after** `cancel(); w.Wait()` (or `Engine.Close()`). Because `t.Cleanup` runs LIFO, register the clearing cleanup first and the Close/Wait cleanup second. Hooks are never reassigned while a worker goroutine may read them.
  - `config.TaskmasterConfig.ProgressIntervalMs`: tests set a small positive value (e.g. `50`). `Open` uses the value as given when it is > 0. Only `config.Load` clamps it (§4.8).
  - `utuber.buildWith(cfg, host golane.Host, exec media.Executor)` replaces `buildWithExecutor` (`internal/utuber/build.go:25`).
  - `golane.RunContext` is an interface, so utuber unit tests can pass a recording fake.
- **R7. yt-dlp never runs in `make test`.** The `Workers: 0` test-safety contract (`internal/utuber/build.go:43-47`, `cmd/server/dispatch_test.go:301-308`) is preserved. The utuber lane is **seeded** with width `cfg.Workers`, and a width of 0 means the lane never starts anything (`worker/worker.go:129-131`: `slots := lane.Width - running; if slots <= 0 { continue }`). Every test that needs a download to run injects a fake `media.Executor` through `buildWith` and sets the width to ≥1 via `Lane.UpdateSettings`.
- **R8. Timeouts under `-race`.** Every wait in a new Go test uses `require.Eventually(t, cond, 10*time.Second, 20*time.Millisecond)`, and every test that starts a worker calls `worker.SetPollIntervalForTest(50 * time.Millisecond)` with `t.Cleanup(func(){ worker.SetPollIntervalForTest(5 * time.Second) })`. The exception is `internal/taskmaster/build_test.go`, whose `TestMain` already sets 50 ms (`build_test.go:27`).

---

## 1. RALPLAN-DR Summary

### Principles
1. **One queue engine.** utuber holds no queue state. Scheduling, width, pause, cancel, restart reconciliation and persistence all come from TM's existing machinery. utuber adds only a payload, a callback and a UI.
2. **The shell-task path is not touched.** Go-function (func) tasks branch off after the existing brake check in `runTask` (`worker/worker.go:201`). Shell tasks keep every documented invariant (R3).
3. **In-process and typed, never shell.** User input (URL, titles) travels as a JSON payload that `Kind.Decode` validates, reaches Go code only, and is passed to yt-dlp as argv (D1/D2).
4. **Generic, but only one consumer.** The registration API names no module, which satisfies owner decision Q5. There is no plugin framework, no dynamic loading and no HTTP registration.
5. **Every narrowing is explicit.** Each behaviour change to an existing documented surface is listed in §2.3, written into the relevant doc, and pinned by a test.

### Decision drivers (top 3)
1. **Risk to TM's scheduler.** The scheduler is lock-per-task (`task_locks.task_id` PK, `db.go:61-66`). It reuses one pending execution per task (`worker/worker.go:150-167`), and its eligibility is task-level (`db.go:514-554`). A design that fits this model avoids a scheduler rewrite.
2. **Restart safety and no leaks under `-race`/goleak.** Queued jobs must survive restarts (FR-U2). Interrupted jobs must reconcile. Shutdown must join every goroutine, because `goleak.VerifyTestMain` runs in `internal/taskmaster/build_test.go:26-29`, `coordinator_test.go:27-29`, `worker_test.go:17-19` and `cmd/server/dispatcher_auth_test.go:1548-1549`.
3. **Build-order independence in `cmd/server`.** `buildDispatcher` iterates `cfg.Routing`, a Go map (`cmd/server/main.go:317`), so the order in which taskmaster and utuber are built is random. utuber can also be routed while taskmaster is not.

### Viable options

**Option A — one TM task per job (chosen).** Each submitted download becomes a one-shot TM task with `kind`, `label`, `payload` and `payload_version` on the **task** row, plus one pending execution created in the same transaction. Executions are attempts: a re-run inserts a new pending execution of the same task, so it reuses the same payload by construction. Retention deletes whole one-shot func tasks, together with their executions and metrics.
- Pros: fits the per-task lock and "reuse pending" logic unchanged. Lane width works as it does today, since width counts running executions per lane (`db.go:386-393`). Cancel, brake, reconcile and heartbeat all apply. The re-run semantics ("same payload") are structural.
- Cons: task rows grow one per download, bounded by retention. The TM board's "Up next" would list every func task unless filtered, so the adapter shows only func tasks that have a pending execution (§4.11). Task names are machine-generated (`<lane>-<12 hex>`), so a human `label` column is needed.

**Option B — one durable task per kind, one execution per job (payload on the execution).** This matches the FRD wording literally: FR-T4 "finished executions are pruned" and FR-T5 "re-run … execution".
- Pros: no per-job task rows. The TM concept "task = utuber.download" is clean. Pruning is execution-only.
- Cons: `task_locks` is keyed by `task_id`, and `poll` acquires a per-task lock before launching (`worker/worker.go:144`). Only one execution of a task can run at a time, so lane width (D7) is meaningless unless locking, `GetPendingExecution` reuse (`worker/worker.go:153`, which picks the *oldest* pending row for the task, `db.go:573-586`) and eligibility are all rewritten per execution. That rewrite touches every shell-task invariant (R3) and the reconcile/brake paths. Higher risk for no user-visible gain.

**Invalidated alternatives**
- **C. utuber keeps its worker pool and only persists to TM's DB.** This violates FR-U1 ("delete `internal/utuber/jobs`") and D1 (TM runs the callback). It keeps two schedulers, which contradicts the FRD's stated goal (§1).
- **D. HTTP loopback between modules.** Explicitly excluded by D1 ("in-process through a Go interface, not over HTTP").

**Wiring sub-decision (inside Option A)**
- **A1. Explicit dependency injection (chosen).** `cmd/server` opens a single `*taskmaster.Engine` lazily through a memoised `moduleDeps.taskmasterEngine()` and passes it to both `taskmaster` (for its HTTP handler) and `utuber.Build(cfg, engine)`. Pros: order-independent, testable with fresh engines per test, no global state. Cons: `buildModule` gains a parameter (two call sites: `cmd/server/main.go:319`, `cmd/server/dispatcher_auth_test.go:94`).
- **A2. Process-global registry (`init()`-time registration).** Rejected. Global mutable state breaks test isolation (each test opens its own engine), hides the dependency, and still needs an engine instance for utuber's list/cancel calls.

**Choice: A + A1.** Driver 1 dominates. Option B's only advantage is wording, and the FRD's intent (same payload re-run; age-out of finished work) is fully met by A. §2.4 records that the FRD's execution-level wording is satisfied at the job level.

---

## 2. Decisions

### 2.1 FRD §4 open questions

| # | Question | Resolution | Type |
|---|----------|-----------|------|
| Q1 | Go interface: build time vs runtime registration; payload typing and versioning | **Registration happens at runtime, during utuber's `Build`**, through `golane.Host.RegisterLane(spec, kinds...)` on the engine that `cmd/server` passes in (§4.3, §4.7). Kinds may be registered after the TM worker has started, and `poll` skips func tasks whose kind is not (yet) registered, leaving them pending (§4.4). This makes build order irrelevant. **Typing:** `golane.NewKind[P any](name, version, validate, run)` builds a `Kind` whose `Decode` strictly decodes JSON into `P` (`DisallowUnknownFields`) and runs `validate`. **Versioning:** each Kind declares an integer `Version ≥ 1`, stored per task in `tasks.payload_version`. `NewKind`'s `Decode` accepts only `v == Version`. A mismatch fails that execution with `unsupported payload version <v> for kind <k> (supports <Version>)`. An upgrade path is deliberately not built (YAGNI); a future Kind can supply its own `Decode` that handles older versions. | PLANNER DECISION — owner may override (owner stated no preference) |
| Q2 | Where payloads are stored; validation on restart | **In JSON `TEXT` columns on the `tasks` row** (`payload`, `payload_version`), added by migration 5 (§4.1). A re-run needs the payload on the task, not the execution (Option A). The JSON result goes in `task_executions.result`. **Validation:** `Decode` runs at `Submit` (a rejected payload never reaches the DB) and **again at the start of every run** (in `runFunc`, before the callback), so a row written by an older binary, or hand-edited, fails that execution cleanly with the decode error instead of crashing. Payload and result are each capped at 64 KiB (`golane.MaxPayloadBytes`). | PLANNER DECISION — owner may override (owner stated no preference) |
| Q3 | Progress rate limiting | **Floor: never slower than today.** Measured current cadence: utuber writes `Progress` on **every** yt-dlp `[download] N%` line. See `internal/utuber/media/downloader.go:69` (`--newline`), `:73-76` (regex gate per line) and `internal/utuber/handler.go:61-63` (`q.Update(... "download " + s)` per call), with no throttle. The page polls `/jobs.json` **every 2000 ms** (`web/utuber/js/main.ts:378`). So the user-visible cadence today is 2000 ms, with an unthrottled in-memory value. **Design:** (a) the in-memory latest progress is updated on every `Report` call, unthrottled as today, and served by `Lane.List`/`GET /api/executions`. (b) DB persistence and the `task-progress` board event are throttled to at most one per `progress_interval_ms`, plus a final write at finish. (c) utuber's page keeps its 2000 ms poll. **Setting:** `taskmaster.progress_interval_ms` in the server config (`config.TaskmasterConfig.ProgressIntervalMs`). Default **2000** (= current cadence). `config.Load` clamps it to [100, 2000] with a log line, so the floor can never be configured away (§4.8). | **OWNER DECISION** (floor + configurable); the constants and the location are PLANNER DECISIONS |
| Q4 | Hidden lane and Metrics | **Hidden lanes are excluded from `GET /api/metrics` "for the time being".** One named constant, `metricsIncludeHiddenLanes = false`, in `internal/taskmaster/coordinator/handlers_metrics.go`, is the only switch. `TestMetrics_HiddenLaneExcludedByPolicy` pins it. Reversal path: see ADR follow-ups. | **OWNER DECISION** |
| Q5 | General "module lane" pattern | The registration API (`golane`) takes an owner, a lane name and kinds, and contains no module-specific names (R4). A second module could call `RegisterLane` without any refactor. **Not built:** no second consumer, no plugin discovery, no config-driven kinds, no HTTP registration. | **OWNER DECISION** |

### 2.2 Other planner decisions (owner may override each)

| ID | Decision | Rationale |
|----|----------|-----------|
| P1 | Job = one-shot func TM task (Option A). Job ID = task name `<lane>-<12 lowercase hex>` (crypto/rand), generated by the engine. | §1. The generated name is safe for URLs, file names and `/api/tasks/{name}`. |
| P2 | Func tasks are **one-shot only**: `repeat=0`, `cooldown_seconds=0`, `sudo=0`, `command=''`, `output_file=''`, fixed at submit and not editable over HTTP. | D2 (no sudo). Keeps the eligibility change minimal. |
| P3 | Func tasks are eligible **only** when a pending execution exists. Shell eligibility is unchanged. | This avoids an existing quirk: `last_finished` ignores `canceled` (`db.go:517-521`), so a canceled never-completed one-shot *shell* task is re-picked on the next poll. For func tasks that would re-run a canceled download. Shell behaviour is intentionally left as is (R3). The quirk is recorded as a follow-up. |
| P4 | Cancelling a **queued** (pending) execution is supported for **func tasks only**. For a pending shell execution, `POST /api/executions/{id}/cancel` keeps today's answer (`200 {"status":"not_running"}`). | Because of the same quirk, cancelling a pending shell up-next would not stop the task from running. It is not offered for shell tasks rather than half-offered. |
| P5 | Per-execution pause/resume (SIGSTOP) is **not offered for func tasks**. They register no PID, so the existing endpoints answer `200 {"status":"not_running"}` (`handlers_control.go:23-34`). The shared panel's pause action for utuber is **lane pause** ("Pause queue"). | FR-T2 "pause" is met at lane level (the scheduler already honours lane pause, `db.go:533`). A Go callback has no process group to stop. |
| P6 | utuber job statuses are TM's statuses: `queued` (= pending), `running`, `success`, `failed`, `canceled`. The old `completed` is renamed to `success`. | One status vocabulary for the shared panel. |
| P7 | utuber lane settings (width, age-out days, hidden, paused) live **only** in the TM `lanes` row. utuber's `/settings.json` reads and writes them through `Lane.Settings`/`UpdateSettings`. `python_bin` stays in `<download_dir>/settings.json`. | Single source of truth, so the ☰ menu and the TM board can never disagree. |
| P8 | `utuber.workers` config becomes the **initial seed** for the lane width, applied only when the lane is first created. After that, the DB (☰ menu) is authoritative. | D7 puts concurrency in the ☰ menu. Seeding keeps R7's `Workers: 0` safety. |
| P9 | "Show in taskmaster" defaults to **shown** (`hidden = false`). | Discoverability. The lane still runs either way (D9). |
| P10 | Age-out: `retention_days` on the lane. 0 means never prune (the default for every non-owned lane). utuber seeds 10 (D6), and the ☰ menu accepts 1–365. The pruner runs at `Open`, every `pruneInterval` (1 h) and immediately after a retention change. | D6. An hourly sweep is plenty at day granularity. |
| P11 | Progress model: `pct` is an integer 0–100 or *indeterminate* (null), plus a `label` string. A `Report(pct<0, label)` call means indeterminate. | FRD D3: "0–100 plus a label". utuber shows whole percents today (`main.ts:254` `toFixed(0)`). |
| P12 | **D3 for shell tasks:** the progress *storage, API and UI* are kind-agnostic, so any execution with progress shows a bar. The only *reporter* shipped is `RunContext.Progress` (func tasks). A stdout marker protocol for shell tasks is deferred (ADR follow-up). | This narrows D3's "any task", explicitly. Adding the reporter later is a parser only, with no schema, API or UI change. |
| P13 | `RunContext.SetLabel` lets the callback retitle a job mid-run. utuber calls it after the metadata fetch, which preserves today's behaviour of names appearing mid-download (`handler.go:37-47`). | Avoids a UX regression. |
| P14 | The utuber page keeps polling `/jobs.json` every 2000 ms. No new SSE surface is added. | Owner floor Q3. Simplest. |
| P15 | `media.OSExecutor.Run` is rewritten to join all output before returning, with the process group killed on cancel. | Today it does not join its scanner goroutines (`internal/utuber/media/exec.go:30-31`, the "Known limitation" in `docs/utuber.md`). Under TM, a late `onLine` would report progress to a finished execution, and goleak or the race detector would flag it. |
| P16 | **Headless engine polls owned lanes only.** When utuber is routed and taskmaster is not, `cmd/server` opens the engine with `taskmaster.OpenOptions{OwnedLanesOnly: true}`. The worker then skips every lane whose `owner == ''` when building candidates, the coordinator router is never built or mounted, and `seedLanes` is not run. Boot reconciliation still runs over all rows. That is DB-only, and it produces the same outcome the next taskmaster-routed boot would (`build.go:67-72`). | Today an unrouted taskmaster runs nothing. Without this option, routing only utuber would start the shell worker, including sudo tasks if `allow_sudo` is stored, with no UI. **No behaviour change for shell tasks.** Test: `TestOpen_OwnedLanesOnlySkipsShellLanes`. |
| P17 | **Func-task metrics are aggregated per (lane, kind).** `GetMetrics…` groups `kind != ''` rows by `(t.lane_name, t.kind)` and reports `task_name = kind`, with a new `kind` field. Shell rows keep per-task grouping. | Grouping by `t.id, t.name` (`db.go:848-862`) would give one Metrics card per download (`utuber-<hex>`) whenever the lane is visible. Test: `TestGetMetrics_FuncTasksAggregatedByKind`. |
| P18 | **Func tasks re-run only from `failed` or `canceled`.** A func task whose latest execution is `success` is refused (`golane.ErrSucceeded`; HTTP 409 on both `POST /api/executions/{id}/rerun` and utuber's `/jobs/rerun`). | A one-shot job that succeeded is done. This rule is generic, not utuber-specific (Q5), and it stops the TM route from bypassing utuber's D5 duplicate check. |
| P19 | Config lanes never touch owned lanes. `seedLanes` skips any configured lane that already exists with `owner != ''` and logs `taskmaster: config lane %q is owned by module %q; ignoring its config entry`. `EnsureOwnedLane` on a lane that exists with `owner == ''` fails with `ErrLaneOwnedByOther`, whose message names the conflict and the fix: `lane "utuber" already exists as a regular taskmaster lane (from config taskmaster.lanes or the UI); rename or delete it`. The engine logs it, and utuber returns 503 for that module only. | `seedLanes` → `UpsertLane` runs on every boot and overwrites the width (`build.go:150-160`, `db.go:312-321`). Tests: `TestSeedLanes_SkipsOwnedLane`, `TestRegisterLane_ConfigLaneCollisionError`. |
| P20 | **Architect synthesis (hide func tasks from `GET /api/tasks` by default): not adopted.** | It would not shrink the guard surface. Func task names stay discoverable through `GET /api/executions` (`task_name`, `db.go:637-650`), so the per-name mutation guards are still required. And the board's lane view needs queued func tasks and their labels (`board.ts:116-131` builds from `listTasks`), so it would need a new endpoint. That means more API for the same number of guards. The guards instead live in one helper, `c.funcTaskGuard(w, task) bool`, called by each handler in the §4.6 table. |

### 2.3 Narrowings of existing documented behaviour (each written into docs and tested)

| # | Surface | Before | After | Test |
|---|---------|--------|-------|------|
| N1 (**added scope**, beyond the FRD, justified because the new `kind`/`payload` columns would otherwise be writable over HTTP) | `PUT /api/tasks/{name}` (`handlers_tasks.go:89-137`, `db.go:455-470`) | Any JSON key is concatenated into SQL as a column name. Unknown keys give 500. | Keys are allow-listed (`lane_name, enabled, paused, cooldown_seconds, repeat, command, position, sudo, output_file`). Anything else gives **400** `unknown or read-only field "<k>"`. A func task gives **409**. | `TestHandleUpdateTask_RejectsUnknownField`, `TestHandleUpdateTask_FuncTaskConflict` |
| N2 | `task_metrics` retention (`docs/taskmaster.md` "retained indefinitely") | Never pruned. | Rows of **pruned or removed func tasks** are deleted together with the task. Shell metrics are unchanged. | `TestPruneOwnedLane_DeletesTaskExecsMetrics` |
| N3 | `DELETE /api/tasks/{name}` (`handlers_tasks.go:139-150`) | Unconditional. | Shell: unchanged. Func task with a running execution: **409**. | `TestHandleDeleteTask_FuncRunningConflict` |
| N4 | Hidden lanes (new) | n/a | A hidden lane and its tasks and executions are invisible to every TM HTTP route (404 or filtered, §4.6) and to board events. The brake still applies to them. | §7 coordinator tests |
| N5 | utuber `/jobs.json` wire format (`docs/utuber.md`, `build_test.go:86-123`) | 11 PascalCase keys; `completed`. | The snake_case shape in §4.10; `success`. | `TestJobsWireFormat` (replaces `TestJobsWireFormatElevenKeys`) |
| N6 | utuber `POST /settings.json` (`settings.go:109-131`) | Body `{}` clears `python_bin` (it decodes as `""`). | Fields are optional pointers. An absent `python_bin` leaves it untouched, and `"python_bin": ""` still clears it. | `TestSettingsPOST_PartialLeavesPythonBin` |
| N7 | `utuber.workers` (`config.go:452-465`) | Worker count, applied every boot. | Seed for the lane width on first creation only (P8). | `TestBuild_WorkersSeedsWidthOnlyOnce` |
| N8 | `docs/utuber.md` "Shutdown behavior (FR-6)" | Workers are un-stoppable; queued jobs are lost on restart. | In-flight downloads are cancelled on `Close` and recorded `failed` (TM shutdown semantics). Queued jobs survive a restart. | `TestUtuber_QueuedJobSurvivesEngineRestart` |

### 2.4 FRD wording satisfied differently (not a behaviour change)
- FR-T4 "finished executions are pruned": in Option A a job is a task, so the pruner deletes the whole one-shot func task (all its executions) when its **latest** execution finished before the cutoff and it has none pending or running.
- FR-T5 "re-run a finished or failed execution with the same payload": the payload is on the task, so `POST /api/executions/{id}/rerun` creates a new pending execution of that execution's task. `canceled` executions are re-runnable too.

### 2.5 OWNER GATES and owner-visible narrowings
There is no blocking gate. The owner must explicitly accept or override each of these narrowings of FRD decisions at plan approval.

| ID | Narrowing | FRD text narrowed | If overridden |
|---|---|---|---|
| G1 (P12) | Only Go-function tasks can *report* progress. Shell tasks have the storage, API and bar, but no reporter. | D3 "optional progress for **any** taskmaster task" | Add a stdout marker parser (`##progress <pct\|-> <label>`) in `OutputCapture.Write`. No schema or API change. |
| G2 (P5) | Pausing a Go-function task means pausing the lane or task (scheduler level). There is no per-execution SIGSTOP. | FR-T2 "…pause…" and D10 "pause" action | Requires `RunContext.RegisterProcess(pid)`, plus reconcile changes for func PIDs. Out of scope for v2. |
| G3 (D9 + brake) | Engaging the hand brake also pauses the (possibly hidden) utuber lane (`handlers_control.go:112-158`). utuber reports it through `queue_paused: true` **plus** `queue_paused_by: "brake"` (from `lanes.paused_by`, `db.go:381`), and the panel header shows "Paused by taskmaster hand brake". While hidden, the TM UI shows no utuber trace, but utuber explains the pause. | D9 "hidden … no trace", with FRD §5 "hand brake … may fall out for free" | Exempt owned lanes from the brake. That is a brake-semantics change and needs its own decision. |
| G4 (P9) | "Show in taskmaster" defaults to shown. | D9 (no default given) | Flip `InitialHidden` in utuber's `LaneSpec`. |

---

## 3. Facts verified in the code that shape the plan (and FRD/doc inaccuracies)

1. **FK enforcement is fragile.** `PRAGMA foreign_keys = ON` is executed once on the pool (`db.go:29-33`). It only holds while the single pooled connection lives: database/sql reopens a connection after `driver.ErrBadConn`, and the pragma is then lost. modernc v1.58 supports a `_foreign_keys` DSN key (verified in the module source). **Fix:** the file DSN becomes `path + "?_journal_mode=WAL&_foreign_keys=1"` (`db.go:22`). New delete paths still delete child rows explicitly and do not rely on cascade.
2. **Migrations are not transactional** (`db.go:166-176`: `Exec(m.sql)`, then a separate `INSERT` of the version). Migration 3 relies on `PRAGMA foreign_keys=OFF`, which is a no-op inside a transaction (`db.go:186-233`). So only the **new** migration is wrapped (`tx: true`, §4.1).
3. **Wrong-method requests to `/api/*` fall through** to the static catch-all (`build.go:135` `r.Handle("/*", …)`), which answers 200 with index.html. This was probed with chi v5.3.2: `GET /api/executions/1/cancel` gives 200 from static. TM has no 405 tests. New TM routes follow the same convention, and no 405 test is added. utuber's `/jobs/*` handlers answer 405 via `http.Error` without an `Allow` header (`handler.go:180-185`), and the new utuber handlers mirror that.
4. **The canceled-eligibility quirk** is described under P3.
5. **Build order is random** (`cmd/server/main.go:317`, map iteration), and utuber may be routed without taskmaster.
6. **Doc inaccuracies (not FRD).** `docs/taskmaster.md` says the `GET /api/tasks` filter is `?group=`, but the code reads `?lane=` (`handlers_tasks.go:16`, and metrics `handlers_metrics.go:12`). The doc's route table omits `POST /api/executions/{id}/pause|resume` (`coordinator.go:81-82`). It says configured lane width is "DB authoritative afterward", but `seedLanes` → `UpsertLane` overwrites the width on every boot (`build.go:150-160`, `db.go:312-321`). `docs/utuber.md` omits the interim `POST /jobs/delete` route. Phase 6 fixes all four.
7. **The FRD's "pause" for Go tasks** (FR-T2) can only mean lane or task pause (P5).
8. **The FRD's "hand brake not required"**: the brake already pauses every lane and cancels every running execution (`handlers_control.go:112-158`). The utuber lane is therefore braked too, which "falls out for free" per FRD §5. It is documented, not prevented.
9. **`UpdateTask` builds SQL from JSON keys** (`db.go:462-467`). N1 closes this.
10. **FRD §6 interim is done:** `handler.go:180-195` and `web/utuber/js/main.ts:353-376`.

---

## 4. Design and exact shapes

### 4.1 Migration 5 (`internal/taskmaster/db/db.go`)

`applyMigrations` (`db.go:144-178`): the migration struct gains `tx bool`. For `tx: true`, the loop runs `tx := db.conn.Begin()`, then `tx.Exec(m.sql)`, then `tx.Exec("INSERT OR REPLACE INTO schema_version (version) VALUES (?)", m.version)`, then `tx.Commit()`. It rolls back on any error and returns `fmt.Errorf("migration %d: %w", …)`. Migrations 1–4 keep `tx: false` (fact 2).

```sql
-- migration 5 (tx: true)
ALTER TABLE lanes ADD COLUMN owner TEXT NOT NULL DEFAULT '';
ALTER TABLE lanes ADD COLUMN hidden INTEGER NOT NULL DEFAULT 0;
ALTER TABLE lanes ADD COLUMN retention_days INTEGER NOT NULL DEFAULT 0;
ALTER TABLE tasks ADD COLUMN kind TEXT NOT NULL DEFAULT '';
ALTER TABLE tasks ADD COLUMN label TEXT NOT NULL DEFAULT '';
ALTER TABLE tasks ADD COLUMN payload TEXT NOT NULL DEFAULT '';
ALTER TABLE tasks ADD COLUMN payload_version INTEGER NOT NULL DEFAULT 0;
ALTER TABLE task_executions ADD COLUMN progress_pct INTEGER;   -- NULL = never reported or indeterminate
ALTER TABLE task_executions ADD COLUMN progress_label TEXT;    -- NULL = never reported
ALTER TABLE task_executions ADD COLUMN result TEXT;            -- func result JSON; NULL otherwise
CREATE INDEX IF NOT EXISTS idx_tasks_kind ON tasks(kind);
```

### 4.2 DB layer (`internal/taskmaster/db/db.go`, `models/models.go`)

**Models** (`models/models.go`):
- `Lane` gains `Owner string json:"owner,omitempty"`, `Hidden bool json:"-"` and `RetentionDays int json:"retention_days,omitempty"`.
- `Task` gains `Kind string json:"kind,omitempty"`, `Label string json:"label,omitempty"`, `Payload json.RawMessage json:"payload,omitempty"` and `PayloadVersion int json:"payload_version,omitempty"`.
- `TaskExecution` gains `ProgressPct *int json:"progress_pct,omitempty"`, `ProgressLabel string json:"progress_label,omitempty"` and `Result json.RawMessage json:"result,omitempty"`.
- New type: `type Progress struct { Pct *int; Label string }`.

**Scan and column lists to extend.** All must stay in sync:
- lane columns: `db.go:325` `GetLane`, `:334` `ListLanes`, `scanLane` `:896-915`;
- task columns: `:423` `GetTask`, `:432` `ListTasks`, `:523-525` `GetEligibleTasks`, `scanTask` `:917-933`;
- execution columns: `:575-577` `GetPendingExecution`, `:616-618` `GetExecution`, `:637-639` and `:646-648` `ListExecutions`, `scanExecution` `:935-961`.

`Payload`/`Result` are scanned from `TEXT`, and an empty string or NULL becomes a nil `RawMessage`.

**Changed methods**
- `Open` (`db.go:19-40`): file DSN `path + "?_journal_mode=WAL&_foreign_keys=1"`. The `PRAGMA` exec at `:30` stays, for `:memory:`.
- `AddTask` (`db.go:397-420`): append `WHERE tasks.kind = ''` to the `ON CONFLICT … DO UPDATE`, so an HTTP upsert can never clobber a func task. This is defence in depth, since the coordinator also returns 409.
- `UpdateTask` (`db.go:455-470`): add `var updatableTaskColumns = map[string]bool{…N1 list…}` and `var ErrUnknownTaskField = errors.New("unknown or read-only field")`. The allowlist loop runs **before** the `updates["updated_at"] = …` injection at `db.go:459`, since `updated_at` is not user-settable, and before any SQL. An unknown key returns `fmt.Errorf("%w %q", ErrUnknownTaskField, k)`.
- `Open` DSN: extract `func dsnFor(path string) string` (`db.go:20-23`) so the DSN is unit-testable.
- `GetMetrics…` (`db.go:848-862`), P17: the SELECT becomes `CASE WHEN t.kind != '' THEN t.kind ELSE t.name END AS task_name, t.lane_name, t.kind, …`, and the grouping becomes `GROUP BY t.lane_name, CASE WHEN t.kind != '' THEN 'k:' || t.kind ELSE 'i:' || t.id END`. `models.MetricSummary` gains `Kind string json:"kind,omitempty"`. `taskFilter` still matches `t.name`. `web/taskmaster/js/metrics.ts:80` keys cards by `m.kind ? 'k:' + m.group_name + '/' + m.kind : 'n:' + m.task_name`. `group_name` carries the lane (`models.go:66`). A shell task that happens to be named like a kind can no longer collide with a card. **`GetMetricsExcludingLanes` is the single implementation.** `GetMetrics` delegates to it, so the P17 SELECT/GROUP is written once and the exclusion adds only `AND t.lane_name NOT IN (…)`. `ORDER BY t.lane_name, task_name` (the alias) replaces `ORDER BY t.lane_name, t.name` (`db.go:862`), which gives a deterministic order under aggregation. **Task-detail metrics:** `taskdetail.ts:168` calls `getMetrics(undefined, task.name)`. For a func task, `taskFilter` matches `t.name`, so the single returned row is that one job's aggregate, but with `task_name = kind`. The `rows.find(r => r.task_name === task.name) ?? rows[0]` at `taskdetail.ts:174` then falls back to `rows[0]`, which is the correct row. This is documented, and it needs no code change.
- `GetEligibleTasks` (`db.go:534-538`): the eligibility disjunction becomes
  ```sql
  AND (
    EXISTS (SELECT 1 FROM task_executions pe WHERE pe.task_id = t.id AND pe.status = 'pending')
    OR (t.kind = '' AND (lf.last_fin IS NULL
        OR (t.repeat = 1 AND (? - CAST(lf.last_fin AS INTEGER)) >= t.cooldown_seconds * 1000)))
  )
  ```
- New sibling `ListExecutionsExcludingLanes(taskFilter string, limit int, exclude []string)`. `ListExecutions` becomes a one-line delegate with `nil`, so existing callers are unchanged. The exclusion is `AND t.lane_name NOT IN (…)`. The unfiltered branch changes `LEFT JOIN tasks` to `LEFT JOIN tasks t` with `(t.lane_name IS NULL OR t.lane_name NOT IN (…))`, which keeps executions of deleted tasks exactly as today.
- New sibling `GetMetricsExcludingLanes(laneFilter, taskFilter string, hours int, exclude []string)`. `GetMetrics` delegates with `nil`.

**New helpers and methods** (all obey R1). `withTx(fn func(*sql.Tx) error) error` is the only way to open a transaction.

| Method | Semantics |
|---|---|
| `EnsureOwnedLane(name, owner string, width, retentionDays int, hidden bool) (models.Lane, error)` | tx: `SELECT owner … WHERE name=?`. If absent, `INSERT` with the given values and `paused=0`. If present with the same owner, return the row unchanged (DB authoritative, N7). If present with a different owner (including `''`), fail with `ErrLaneOwnedByOther`. |
| `SubmitFuncTask(s FuncTaskSpec) (taskID, execID int64, err error)` | `FuncTaskSpec{Name, Lane, Kind, Label string; Payload []byte; PayloadVersion int}`. tx: `pos := SELECT COALESCE(MAX(position),-1)+1 FROM tasks WHERE lane_name=?`. Then a **plain** `INSERT INTO tasks (…)` with enabled=1, paused=0, cooldown 0, repeat 0, command '', sudo 0, output_file '', kind, label, payload, payload_version. Then `INSERT INTO task_executions (task_id, scheduled_at, status) VALUES (?, now, 'pending')`. A UNIQUE violation is returned as is, and the engine retries with a new name up to 3 times. |
| `ClaimFuncExecution(execID int64, workerID string) (bool, error)` | Single `UPDATE task_executions SET status='running', started_at=?, worker_id=? WHERE id=? AND status='pending'`. Returns RowsAffected == 1. |
| `CancelPendingFuncExecution(execID int64) (canceled bool, taskID int64, err error)` | tx: `UPDATE task_executions SET status='canceled', finished_at=?, duration_ms=0, schedule_delay_ms=0 WHERE id=? AND status='pending' AND task_id IN (SELECT id FROM tasks WHERE kind != '')`. If 1 row: `SELECT task_id`, then `INSERT INTO task_metrics(… 'canceled', 0, 0)`. |
| `UpdateTaskLabel(name, label string) error` | Single `UPDATE tasks SET label=?, updated_at=? WHERE name=? AND kind != ''`. The label is truncated to 200 runes by the caller. |
| `RerunTask(name string) (execID int64, err error)` | tx: look up `id, kind, lane_name` (missing → `ErrTaskNotFound`). `SELECT COUNT(*) … status IN ('pending','running')` > 0 → `ErrTaskBusy`. If `kind != ''` and the latest execution's status is `success` → `ErrTaskSucceeded` (P18). If `kind != ''`: `UPDATE tasks SET position = (max+1), updated_at=?` (FIFO re-queue). Shell tasks keep their position. Then `INSERT` a pending execution. |
| `RemoveIdleTask(name string) error` | tx: look up the id (missing → `ErrTaskNotFound`). A running execution → `ErrTaskBusy`. Then `DELETE FROM task_metrics WHERE task_id=?`, `DELETE FROM task_executions WHERE task_id=?`, `DELETE FROM task_locks WHERE task_id=?`, `DELETE FROM tasks WHERE id=?`. |
| `PruneOwnedLane(lane string, cutoff time.Time) (int, error)` | tx: read ids `SELECT t.id FROM tasks t WHERE t.lane_name=? AND t.kind!='' AND t.repeat=0 AND NOT EXISTS (SELECT 1 FROM task_executions e WHERE e.task_id=t.id AND e.status IN ('pending','running')) AND (SELECT MAX(e.finished_at) FROM task_executions e WHERE e.task_id=t.id) < ?` into a slice, close the rows, then run the four DELETEs from `RemoveIdleTask` for each id. Returns the count. |
| `SetExecutionProgress(execID int64, p models.Progress) error` | Single `UPDATE … SET progress_pct=?, progress_label=?`. |
| `FinishFuncExecution(execID int64, status string, errMsg *string, durationMs, schedDelay int64, result []byte, p *models.Progress) error` | Single `UPDATE` of status, finished_at, duration, delay, error, result, and progress when p != nil. |
| `ExecutionLane(execID int64) (lane string, kind string, found bool, err error)` | Joins executions to tasks. Used by hidden-lane checks and by cancel. |
| `SetLaneHidden(name string, hidden bool)`, `SetLaneRetention(name string, days int)`, `ListHiddenLanes() ([]string, error)`, `ListRetentionLanes() ([]struct{Name string; Days int}, error)` | Single statements. |

Errors are exported from `db`: `ErrTaskNotFound`, `ErrTaskBusy`, `ErrTaskSucceeded`, `ErrLaneOwnedByOther`, `ErrUnknownTaskField`.

`CancelPendingFuncExecution` is also the **brake path** for func tasks (§4.4), so a pending→canceled transition and its metric happen exactly once, whoever gets there first.

### 4.3 The in-process interface — new leaf package `internal/taskmaster/golane`

This package imports only the standard library, and both utuber and TM import it. It contains no module names (R4).

```go
package golane

const MaxPayloadBytes = 64 << 10

var (
    ErrNotFound        = errors.New("job not found")
    ErrBusy            = errors.New("job is queued or running")
    ErrRunning         = errors.New("job is running")
    ErrUnknownKind     = errors.New("kind not registered on this lane")
    ErrKindRegistered  = errors.New("kind already registered")
    ErrLaneRegistered  = errors.New("lane already registered in this process")
    ErrInvalidSettings = errors.New("invalid lane settings")
    ErrSucceeded       = errors.New("job already succeeded") // P18: Rerun refuses success
)

// RunContext is handed to a Kind's Run. It is safe for concurrent use and is a no-op after Run returns.
type RunContext interface {
    JobID() string                     // the task name
    ExecID() int64
    Progress(pct int, label string)    // pct 0..100; pct < 0 = indeterminate; values > 100 clamp to 100
    SetLabel(label string)             // retitle the job (db.UpdateTaskLabel, immediately; ≤ 200 runes)
    Log() io.Writer                    // lines appear in TM's per-execution output (SSE replay)
}

type Kind struct {
    Name    string // ^[a-z0-9]+(\.[a-z0-9_-]+)+$  e.g. "<owner>.<verb>"
    Version int    // >= 1; stored per task as payload_version
    Decode  func(version int, raw json.RawMessage) (any, error)
    Run     func(ctx context.Context, rc RunContext, payload any) (result any, err error)
}

// NewKind builds a typed Kind: strict JSON decode into P (DisallowUnknownFields),
// version must equal `version`, then validate. Run receives the decoded P.
func NewKind[P any](name string, version int, validate func(P) error,
    run func(ctx context.Context, rc RunContext, p P) (any, error)) Kind

type LaneSpec struct {
    Name                 string // ^[a-z0-9][a-z0-9_-]{0,31}$
    Owner                string // same pattern
    InitialWidth         int    // seed only (>= 0; 0 = never runs, see R7)
    InitialRetentionDays int    // seed only (>= 0)
    InitialHidden        bool   // seed only
}

type Progress struct {
    Pct   *int   `json:"pct"`   // null = indeterminate
    Label string `json:"label"`
}

type Job struct {
    ID             string          // task name
    Kind, Label    string
    Payload        json.RawMessage
    PayloadVersion int
    Status         string          // queued | running | success | failed | canceled
    ExecID         int64           // latest execution
    Progress       *Progress       // nil = never reported
    Result         json.RawMessage // latest execution's result (nil unless success)
    Error          string
    CreatedAt      time.Time
    StartedAt      *time.Time
    FinishedAt     *time.Time
}

type LaneSettings struct {
    Width, RetentionDays int
    Hidden, Paused       bool
    PausedBy             string // from lanes.paused_by ("brake", "owner:<o>", "api", …)
    BrakeEngaged         bool   // read-only: the engine's BrakeGate (persisted setting brake_engaged, build.go:89-106); ignored by UpdateSettings
}
type SettingsPatch struct { Width, RetentionDays *int; Hidden, Paused *bool }

type CancelOutcome string // "canceled" (was queued) | "canceling" (was running) | "not_running"

type Lane interface {
    Name() string
    Submit(kind, label string, payload any) (Job, error)
    List() ([]Job, error)                  // ordered by task position ASC (FIFO)
    Get(id string) (Job, error)
    Cancel(id string) (CancelOutcome, error)
    Rerun(id string) (Job, error)          // ErrBusy if queued/running; ErrSucceeded if latest is success
    Remove(id string) error                // ErrRunning if running; queued removal is allowed
    Settings() (LaneSettings, error)
    UpdateSettings(p SettingsPatch) (LaneSettings, error) // width >= 1, retention >= 0 → else ErrInvalidSettings
}

type Host interface {
    RegisterLane(spec LaneSpec, kinds ...Kind) (Lane, error)
}
```

The status mapping for the latest execution is pending → `queued`, and every other status passes through unchanged. `List` is one query that joins each task to its latest execution (`e.id = (SELECT MAX(id) FROM task_executions WHERE task_id = t.id)`). For `running` rows it overlays the in-memory `ProgressRegistry` value (§4.9).

### 4.4 Worker changes (`internal/taskmaster/worker/`)

New files:
- `funcs.go`: `FuncRegistry{mu sync.RWMutex; m map[string]golane.Kind}`, with `Register(k) error` (returns `ErrKindRegistered` on a duplicate), `Get(name)` and `Has(name)`. It is nil-safe: a nil registry has nothing.
- `visibility.go`: `HiddenLanes{mu sync.RWMutex; set map[string]bool}`, with `NewHiddenLanes([]string)`, `Hidden(lane) bool`, `Set(lane, bool)` and `Names() []string` (sorted). It is nil-safe: nothing is hidden.
- `progress.go`: `ProgressRegistry` (a mutex-guarded map `execID → models.Progress`, with `Set`, `Get`, `Delete`) and the unexported `progressReporter` (§4.9).
- `cancelexec.go`: a single shared cancel protocol used by both the HTTP handler and `Lane.Cancel`. It is **DB-first**:
  ```go
  // CancelExecution:
  //  (a) canceled, _, err := db.CancelPendingFuncExecution(id)
  //      if canceled { cancels.Cancel(id); publish task-finished/canceled; return "canceled" }
  //  (b) else if cancels.Cancel(id) { return "canceling" }
  //  (c) else return "not_running"
  func CancelExecution(d *db.DB, c *CancelRegistry, b *broker.Broker, h *HiddenLanes, execID int64) (golane.CancelOutcome, error)
  ```
  **Event resolution in path (a):** after the UPDATE commits, `lane, _, found, err := db.ExecutionLane(id)` and `exec, _ := db.GetExecution(id)` supply the lane and task name (`exec.TaskName`, via the existing LEFT JOIN at `db.go:614-627`). Both calls run outside any tx (R1). The event `BoardEvent{Type: "task-finished", Lane: lane, Task: exec.TaskName, ExecutionID: id, Status: "canceled"}` goes through `PublishBoardEvent(b, h, ev)`, so the hidden-lane filter applies and nothing is published for a hidden lane. A lookup error is logged, and the event is skipped (the cancel itself already committed).
  Why the order matters: `runTask` registers its cancel func (`worker.go:185`) **before** the claim, and `CancelRegistry.Cancel` returns true as soon as an ID is registered (`cancel.go:43-55`). A registry-first protocol would therefore report "canceling" for a still-pending row and leave it pending, and the claim would then run Run with a dead context. With DB-first ordering, every interleaving ends in exactly one terminal state:
  1. **UPDATE before claim** (whether or not runTask is registered yet): the row becomes `canceled` with one metric. If registered, its ctx is canceled too. The claim then affects 0 rows and `runFunc` bails without Run.
  2. **Claim before UPDATE:** the UPDATE affects 0 rows. Step (b) finds the registration (registration precedes the claim) and cancels the ctx. `runFunc`'s post-claim `execCtx.Err()` check, or Run itself, observes it, and the row finishes `canceled` because `WasCanceled` is true.
  3. **Not pending and not registered** (finished): `not_running`.

**`Worker` wiring.** The `New`/`NewWithExecutor` signatures stay unchanged (`worker.go:62,76`). A new method is added:
```go
type FuncOptions struct { Funcs *FuncRegistry; Progress *ProgressRegistry; Hidden *HiddenLanes; ProgressInterval time.Duration }
func (w *Worker) EnableFuncTasks(o FuncOptions) // must be called before Start; engine does so
```
- `poll()`: the filter goes where **candidates are built** (`worker.go:134-139`), before `toRun := min(slots, len(candidates))` (`worker.go:141`), so an unregistered func task never consumes a slot or blocks a width-1 lane:
  ```go
  for _, t := range eligible {
      if t.LaneName != lane.Name { continue }
      if t.Kind != "" && !w.funcs.Has(t.Kind) { w.logMissingKindOnce(t.Kind); continue }
      candidates = append(candidates, t)
  }
  ```
  The lane loop (`worker.go:123`) also skips `lane.Owner == ""` when `w.ownedLanesOnly` is set (P16). A worker without `EnableFuncTasks` never runs a func task.
- `runTask` (`worker.go:176`):
  - **Brake path** (`worker.go:201-220`): for `task.Kind != ""`, the unconditional `FinishExecution` + `RecordMetric` (`worker.go:208-215`) is replaced by `db.CancelPendingFuncExecution(execID)`, which is conditional on `status='pending'` and records the metric only when 1 row changed. The `task-finished` event is published only when it changed. Shell tasks keep the existing code unchanged.
  - Right after the brake block: `if task.Kind != "" { w.runFunc(task, execID, scheduledAt, execCtx, stdout, stderr); return }`. The deferred unregister and cancel (`worker.go:186-190`) still apply.
- The heartbeat is extracted into `func (w *Worker) startHeartbeat(taskID int64) (stop func())` (body moved from `worker.go:280-296`), which both paths use. Shell behaviour is identical.
- `runFunc` steps, in order:
  1. `k, ok := w.funcs.Get(task.Kind)`. If not ok, finish `failed` with "no function registered for kind <k>".
  2. `if beforeFuncClaimHook != nil { beforeFuncClaimHook(execID) }`.
  2a. **Pre-claim shutdown check:** `if execCtx.Err() != nil && !w.cancels.WasCanceled(execID)`, the worker is shutting down, not cancelling this job. Do **not** claim: `stdout.MarkDone(); stderr.MarkDone(); w.registry.MarkFinished(execID)`, then `ReleaseLock`, then return. The row stays `pending`, so the queued job survives the restart (FR-U2). There is no metric and no event. If `WasCanceled` is true, fall through to the claim; the claim either affects 0 rows (already canceled in the DB) or step 3b finishes it `canceled`.
  3. `claimed, err := w.db.ClaimFuncExecution(execID, w.workerID)`. If it is not claimed, the row was canceled or removed while pending. Then: `stdout.MarkDone(); stderr.MarkDone(); w.registry.MarkFinished(execID)`, then `ReleaseLock`, then return. There is no metric and no event here, because the canceller already did both.
  3a. `if afterFuncClaimHook != nil { afterFuncClaimHook(execID) }`.
  3b. **If `execCtx.Err() != nil` or `w.brake.Engaged()`, do not call Run.** Finish with status `canceled` if `w.cancels.WasCanceled(execID)` or the brake is engaged, else `failed` (worker shutdown, matching `worker.go:332-336`). The brake check closes the window between runTask's brake check (`worker.go:201`) and the claim, where the brake's cancel sweep (`handlers_control.go:142-149`, running rows only) could not yet see this row. The error message is `execCtx.Err().Error()`, or `"hand brake engaged"`. Use `errMsg = execCtx.Err().Error()`, duration 0, then `FinishFuncExecution`, the event, `RecordMetric` and `ReleaseLock`.
  4. `startedAt = now`, publish `task-started`, `stopHB := w.startHeartbeat(task.ID)`.
  5. `p, derr := k.Decode(task.PayloadVersion, task.Payload)` (Q2 restart validation). An error fails the execution with `invalid payload: <err>`.
  6. Build `rc` (`JobID`, `ExecID`, `Progress` → reporter, `SetLabel` → `db.UpdateTaskLabel` (a single UPDATE, ≤ 200 runes), `Log()` → `stdout`). `rc` shares the reporter's `closed` flag: **after Run returns, `Progress`, `SetLabel` and `Log` writes are all no-ops.**
  7. `res, err := safeRun(k.Run, execCtx, rc, p)`, where `safeRun` recovers a panic into `fmt.Errorf("panic: %v", r)` and logs `debug.Stack()`.
  8. `final := reporter.close()`, `stopHB()`.
  9. The status is decided exactly as at `worker.go:323-338`: nil error → success; otherwise `WasCanceled` → canceled, else failed. On success, a result that fails `json.Marshal` or exceeds 64 KiB gives `failed` / "result too large".
  10. `FinishFuncExecution(…, resultJSON, final)`, then `MarkDone`/`MarkFinished`, publish `task-finished`, `RecordMetric`, `ReleaseLock`, `w.progress.Delete(execID)`.
- **Reconcile:** func executions never persist a PID, so `ReconcileOrphans` (`reconcile.go:17-33`) marks them `failed` with `orphanedExecutionMessage` (`db.go:742`). No code change is needed, only a test.
- **Board events** (`board_events.go`): `BoardEvent` gains `ProgressPct *int json:"progress_pct,omitempty"` and `ProgressLabel string json:"progress_label,omitempty"`. The signature becomes `PublishBoardEvent(b *broker.Broker, h *HiddenLanes, ev BoardEvent)`, which drops any event whose `Lane != ""` is hidden. There are two new types: `task-progress` (lane, task, execution_id, progress_pct, progress_label) and `lanes-changed` (no lane field, published on hide/show).

### 4.5 Engine (`internal/taskmaster/engine.go`, `lane.go`; `build.go` becomes a wrapper)

```go
type Engine struct { /* db, worker, registries, gates, broker, coordinator router, funcs, hidden, progress, lanes map[string]*laneHandle, cancel, workerDone, pruneDone, closeOnce */ }

type OpenOptions struct {
    // OwnedLanesOnly (P16): the worker schedules only lanes with owner != "";
    // seedLanes is skipped; Handler() returns nil. Used when utuber is routed but taskmaster is not.
    OwnedLanesOnly bool
}
func Open(cfg config.TaskmasterConfig, opts OpenOptions) (*Engine, error) // everything Build does today at build.go:38-131, plus:
    //  seedLanes skips owned lanes (P19); seed HiddenLanes from db.ListHiddenLanes(); create FuncRegistry, ProgressRegistry;
    //  w.EnableFuncTasks(FuncOptions{..., OwnedLanesOnly: opts.OwnedLanesOnly}) with ProgressInterval = cfg.ProgressIntervalMs (<=0 → 2000ms);
    //  run prune once, then start the pruner goroutine (ticker pruneInterval); start worker.
func (e *Engine) Handler() http.Handler                // coordinator routes + r.Handle("/*", static) — NOT an io.Closer; nil when OwnedLanesOnly
func (e *Engine) Close() error                         // sync.Once: cancel ctx; <-workerDone; w.Wait(); stop pruner (<-pruneDone); stopGC(); db.Close()
func (e *Engine) RegisterLane(spec golane.LaneSpec, kinds ...golane.Kind) (golane.Lane, error)

func Build(cfg config.TaskmasterConfig) (http.Handler, error) // e, err := Open(cfg, OpenOptions{}); return &closer{Handler: e.Handler(), close: e.Close}, nil
```
- `seedLanes` (`build.go:150-160`) is given the DB and, for each config lane, calls `GetLane(name)` first. If the lane exists with `Owner != ""`, it logs the P19 line and skips it. Otherwise it calls `UpsertLane` as today.
- `FuncOptions` (§4.4) gains `OwnedLanesOnly bool`, stored as `w.ownedLanesOnly`. `ListLanes` returns `Owner` (§4.2 scan change), so `poll` can skip `lane.Owner == ""` at the top of the lane loop (`worker.go:123`).
- `RegisterLane` validates the spec and kinds (patterns in §4.3, `Version >= 1`, non-nil `Decode`/`Run`, at least one kind, each kind name unique), then calls `db.EnsureOwnedLane`, then `funcs.Register` for each kind, then sets `hidden` from the returned lane row. A second call for the same lane name returns `ErrLaneRegistered`. It logs `taskmaster: lane %q registered by %q (kinds %v, width %d, retention %dd, hidden %v)`.
- `laneHandle` implements `golane.Lane` through the db methods of §4.2. `Submit` marshals the payload, enforces the 64 KiB cap, runs `Decode(Version, raw)`, checks that the kind belongs to this lane's registered set (`ErrUnknownKind` otherwise), generates the name, and calls `SubmitFuncTask`. It then publishes `task-enqueued`. `Cancel` calls `worker.CancelExecution` on the latest execution of the job, and a finished job returns `"not_running"`. `UpdateSettings` validates first, then applies width (`SetLaneWidth`), retention (`SetLaneRetention` + an immediate prune), paused (`SetLanePaused(name, v, "owner:"+owner)`) and hidden (`SetLaneHidden` + `hidden.Set` + publish `lanes-changed`). It publishes `lane-updated` for the other changes.
- Pruner log: `taskmaster: pruned %d job(s) from lane %q finished before %s`, printed only when n > 0.

### 4.6 Coordinator HTTP changes (`internal/taskmaster/coordinator/`)

`coordinator.New` (`coordinator.go:38-40`) gains two trailing params, `hidden *worker.HiddenLanes, progress *worker.ProgressRegistry` (both nil-safe). `publishBoard` (`coordinator.go:44-46`) passes `c.hidden`.

**New route** (`coordinator.go` routes, next to `:80`): `r.Post("/api/executions/{id}/rerun", c.handleRerunExecution)`.

| Request | Response |
|---|---|
| `POST /api/executions/{id}/rerun` | Non-integer id → `400 {"error":"invalid execution id: <id>"}`. Unknown id, or its lane hidden → `404 {"error":"no such execution: <id>"}`. Execution not terminal → `409 {"error":"execution is not finished"}`. Task has a pending or running execution → `409 {"error":"task already has a queued or running execution"}`. Func task whose latest execution is `success` (`db.ErrTaskSucceeded`, P18) → `409 {"error":"module-managed job already succeeded"}`. Shell tasks may re-run from any terminal status. Otherwise → `201 {"execution_id": N}`, and publishes `task-enqueued`. |
| `POST /api/executions/{id}/cancel` (changed) | Now calls `worker.CancelExecution`. Running → `200 {"status":"canceling"}` (unchanged). Pending func → `200 {"status":"canceled"}` (new). Pending shell or finished → `200 {"status":"not_running"}` (unchanged, P4). Unknown or hidden → 404 (unchanged message). |

**Changed response shapes** (additive; `omitempty`): tasks gain `kind`, `label`, `payload` and `payload_version`. Executions gain `progress_pct`, `progress_label` and `result`. Lanes gain `owner` and `retention_days`. `handleListExecutions` (`handlers_executions.go:11-39`) overlays `c.progress.Get(e.ID)` for rows with status `running`, the same way it merges the pid at `:31-37`.

**Owned-lane and func-task guards.** The error envelope is `response.WriteError`.

| Handler (file:line) | Guard |
|---|---|
| `handleAddTask` `handlers_tasks.go:38` | `task.Kind != ""` → 400 `"kind" cannot be set over HTTP`. Target lane owned → 400 `lane "<l>" is managed by module "<o>"`. An existing task with that name has `kind != ""` → 409 `task "<n>" is managed by module "<o>"`. |
| `handleUpdateTask` `:89` | Func task → 409 (same message). `ErrUnknownTaskField` → 400. `lane_name` pointing at an owned lane → 400. |
| `handleMoveTask` `:195` | Func task → 409. Destination owned → 400. |
| `handleUpNext` `:178` | Func task → 409 `use POST /api/executions/{id}/rerun for module-managed tasks`. |
| `handlePauseTask` `:152`, `handleResumeTask` `:165` | Func task → 409 `task "<n>" is managed by module "<o>"`. Queue holding is done at lane level (G2). |
| `handleDeleteTask` `:139` | Func task → `db.RemoveIdleTask`; `ErrTaskBusy` → 409 `task is running`. Shell tasks unchanged. |
| `handleCreateLane` `handlers_lanes.go:43` | The name exists and is owned → 409. |
| `handleDeleteLane` `:95` | Owned → 409 `lane "<l>" is managed by module "<o>"`. |

Width, order, pause and resume on an owned **lane** are **allowed** (single source of truth, P7). Every func-task row above goes through one helper, `func (c *Coordinator) funcTaskGuard(w http.ResponseWriter, t *models.Task) (blocked bool)`, which writes the 409 and returns true for `t.Kind != ""`. `TestOwnedLaneGuards` is table-driven over **every** row of this table, including pause and resume.

**Hidden-lane filtering (N4).** A helper `c.laneHidden(name) bool` calls `c.hidden.Hidden`. Its effects:
- `handleListLanes`, `handleListTasks`: skip hidden rows.
- `handleGetLane` and every `/api/lanes/{name}/…` mutation: 404 `lane not found: <name>`.
- Every `/api/tasks/{name}…` route: 404 `task not found: <name>` when the task's lane is hidden.
- `handleAddTask`/`handleMoveTask` targeting a hidden lane: 400 `unknown lane: <l>` (same as a missing lane).
- `handleListExecutions`: `ListExecutionsExcludingLanes(…, c.hidden.Names())`.
- `/api/executions/{id}/output|cancel|pause|resume|rerun`: 404 when `ExecutionLane` is hidden. The output route uses its existing `404 "execution not found"`.
- `handleMetrics` (`handlers_metrics.go:20`):
  ```go
  // metricsIncludeHiddenLanes: owner decision 2026-09-27 (FRD Q4) — hidden lanes are
  // excluded from Metrics "for the time being". Flip to true (and flip
  // TestMetrics_HiddenLaneExcludedByPolicy) to include them.
  const metricsIncludeHiddenLanes = false
  ```
  When false, it passes `c.hidden.Names()` to `GetMetricsExcludingLanes`.
- Brake handlers are unchanged (fact 8).

### 4.7 `cmd/server` wiring (`cmd/server/main.go`, `dispatcher_auth_test.go`)

```go
type moduleDeps struct {
    cfg     *config.Config
    tm      *taskmaster.Engine
    tmErr   error
    tmTried bool
    closers *[]io.Closer
}
// taskmasterEngine opens the shared engine exactly once per dispatcher; on success it appends e to *closers.
// It calls taskmaster.Open(d.cfg.Taskmaster, taskmaster.OpenOptions{OwnedLanesOnly: !moduleIsRouted(d.cfg.Routing, "taskmaster")}).
func (d *moduleDeps) taskmasterEngine() (*taskmaster.Engine, error)

// moduleIsRouted generalises adminIsRouted (main.go:367-374); adminIsRouted becomes a one-line call to it.
func moduleIsRouted(routing map[string]string, module string) bool
```
- `buildModule(module string, cfg *config.Config, svc *auth.Service, deps *moduleDeps)` (`main.go:376`):
  - `"taskmaster"`: `e, err := deps.taskmasterEngine(); if err … return nil, err; return e.Handler(), nil`.
  - `"utuber"`: `e, err := deps.taskmasterEngine(); if err != nil { return nil, fmt.Errorf("utuber needs the taskmaster engine: %w", err) }; return utuber.Build(cfg.Utuber, e)`.
- Both callers create `deps := &moduleDeps{cfg: cfg, closers: &dispatch.closers}` before the loop: `buildDispatcher` (`main.go:314-333`) and `buildControlDispatcher` (`dispatcher_auth_test.go:88-108`).
- The `io.Closer` append at `main.go:324-326` stays. `e.Handler()` is not a Closer, so the engine is closed exactly once, via deps.
- A utuber-only routing still opens the engine, headless with `OwnedLanesOnly` (P16): no shell task runs and no TM routes exist. Its DB path is `cfg.Taskmaster.DBPath` (default `./data/taskmaster/taskmaster.db`, `config.go:528`).
- The 503 body is unchanged: `unavailableHandler` names the module only (`main.go:425-436`).

### 4.8 Config (`internal/platform/config/config.go`)

- `TaskmasterConfig` (`config.go:436-444`) gains `ProgressIntervalMs int json:"progress_interval_ms"`.
- New constants: `DefaultTaskmasterProgressIntervalMs = 2000`, `MinTaskmasterProgressIntervalMs = 100`, `MaxTaskmasterProgressIntervalMs = 2000`.
- New `normalizeTaskmaster(t *TaskmasterConfig)`, called in `Load` next to `normalizeUtuber` (`config.go:663`):
  - 0 → default.
  - Negative → error `taskmaster: progress_interval_ms must be >= 0`.
  - Below the minimum → clamp with log `taskmaster: progress_interval_ms %d below minimum %d; clamping`.
  - Above the maximum → clamp with log `taskmaster: progress_interval_ms %d exceeds %d (progress must update at least every 2s); clamping`.
- `DefaultConfig` (`config.go:526-531`) sets the default.
- `UtuberConfig.Workers` comment updated (N7). `normalizeUtuber` is unchanged.

### 4.9 Progress pipeline

`progressReporter{mu; closed bool; persistedOnce bool; last models.Progress; lastPersist time.Time; interval; now func() time.Time; execID; lane, task string; db; reg *ProgressRegistry; board; hidden}`. It never calls `time.Now` directly; it always uses `p.now()`.
- `Report(pct, label)`:
  1. Clamp pct: `< 0` → nil (indeterminate); `> 100` → 100. Truncate the label to 120 runes.
  2. Under `mu`, if closed return. Set `last`, then `reg.Set(execID, last)` (unthrottled; freshness equals today's).
  3. Persist if `!persistedOnce`, **or** `label != previousLast.Label` (phase change: always persisted immediately, so "Converting" is never dropped by the leading-edge throttle) **or** `p.now().Sub(lastPersist) >= interval`. Still under `mu` (this serialises writes; at most one per interval): run `db.SetExecutionProgress`, publish `task-progress` (filtered by hidden), set `lastPersist = now`, and call `progressPersistHook`.
  4. No transaction is open (R1/R2).
- The reporter has an explicit `persistedOnce bool` field. The persist condition is `!persistedOnce || label != previousLast.Label || p.now().Sub(lastPersist) >= interval`, and `persistedOnce = true` is set after the first successful write. So the first report always persists, independent of both the label rule and the zero value of `lastPersist`.
- `close()`: under `mu`, sets `closed = true` and returns `last` (or nil if never reported). `FinishFuncExecution` persists it. Any later `Report` is a no-op.
- Readers: `Lane.List`/`Get` and `GET /api/executions` overlay `reg.Get` for running rows. Finished rows read the persisted columns.

### 4.10 utuber backend (`internal/utuber/`)

**Deleted:** the whole `internal/utuber/jobs/` directory (`job.go`, `queue.go`, `worker.go`, `job_race_test.go`, `queue_test.go`, `worker_test.go`).

**`build.go`:**
```go
func Build(cfg config.UtuberConfig, host golane.Host) (http.Handler, error) { return buildWith(cfg, host, media.OSExecutor{}) }
func buildWith(cfg config.UtuberConfig, host golane.Host, exec media.Executor) (http.Handler, error)
    // host == nil → error "utuber: no taskmaster engine"
    // MkdirAll, history.Open, newSettingsStore (unchanged)
    // lane, err := host.RegisterLane(golane.LaneSpec{Name: "utuber", Owner: "utuber",
    //     InitialWidth: cfg.Workers, InitialRetentionDays: 10, InitialHidden: false},
    //     golane.NewKind[downloadPayload]("utuber.download", 1, validatePayload, proc.run))
    // routes below
```

**Payload and result (v1):**
```go
type downloadPayload struct {
    URL          string `json:"url"`
    ShowName     string `json:"show_name"`
    EpisodeTitle string `json:"episode_title"`
    Season       int    `json:"season"`
    Episode      int    `json:"episode"`
    Mode         string `json:"mode"` // "video" | "audio"
}
// validatePayload: URL non-empty and <= 2048 bytes; Mode ∈ {video,audio}; 0 <= Season,Episode <= 999.
type downloadResult struct {
    OutputFile   string `json:"output_file"`
    ShowName     string `json:"show_name"`
    EpisodeTitle string `json:"episode_title"`
}
```

**`processor.run(ctx, rc, p)`** ports `Process` (`handler.go:27-104`) line for line, with these changes:
- `rc.Progress(-1, "Fetching metadata")` replaces `"fetching metadata"`.
- After metadata, `rc.SetLabel(show + " — " + title)`.
- `rc.Progress(-1, "Downloading")`.
- The download callback calls `rc.Progress(int(pct), "Downloading")`, with pct parsed by `strconv.ParseFloat`. The raw line also goes to `rc.Log()`.
- Audio conversion calls `rc.Progress(-1, "Converting")`.
- The download stem is `fmt.Sprintf("%s-%d", rc.JobID(), rc.ExecID())` (e.g. `utuber-0a1b2c3d4e5f-17`). Today the stem is `job.ID` (the `media.Download` call at `handler.go:55-64`), and that ID comes from `randID()` at `handler.go:137`. A re-run after a `kill -9` therefore never shares an output path with an orphaned yt-dlp from the previous attempt (K5). `randID` (`handler.go:224-228`) is deleted, since it has no other users (`grep -rn randID internal/utuber` shows only `handler.go`).
- It returns `downloadResult`.
- `history.Record` stays in the callback (D4/D5).
- On ctx cancellation it returns `ctx.Err()`. Partial `<stem>.*` files are left in place (risk K6).

**`media/exec.go` (P15):** `OSExecutor.Run` sets `cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}`, `cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }` and `cmd.WaitDelay = 10 * time.Second`. `cmd.Stdout` and `cmd.Stderr` are one shared `*lineWriter` that splits on `\n`, buffers partial lines, serialises `onLine` under a mutex and flushes the remainder after `Wait` returns. So `Run` returns only after every `onLine` call has completed. `StdoutPipe` and the goroutines are removed.

**HTTP (`handler.go`, `settings.go`)**

| Method | Path | Behaviour |
|---|---|---|
| POST (method unchecked, as today) | `/enqueue` | Form fields unchanged (`handler.go:106-150`). The duplicate check against `history.json` is unchanged (409 body unchanged). It builds a `downloadPayload` and calls `lane.Submit("utuber.download", label, p)`, where `label` = `show — title` if both are given, else the URL. **204** on success. **400** `missing url`, or the Decode/validate error text. **500** on a store error. |
| GET | `/jobs.json` | `200`, `Content-Type: application/json`, an array in `List` order. Each element has **exactly** these keys: `{"id","status","label","url","show_name","episode_title","season","episode","mode","progress","output_file","error","created_at","started_at","finished_at"}`. `progress` is `{"pct":int\|null,"label":string}` or `null`. `show_name`/`episode_title` come from the result when present, else from the payload. `output_file` is `""` unless `success`. `started_at`/`finished_at` are RFC 3339 or `null`. |
| POST | `/jobs/cancel?id=` | `200 {"status":"canceled"\|"canceling"}`. `404` `job not found`. `409` `that download already finished` (outcome not_running). Non-POST → `405` `method not allowed` (via `http.Error`, as `handler.go:182-184`). |
| POST | `/jobs/rerun?id=` | `204`. `404`. `409` `that download is queued or running` (`golane.ErrBusy`). `409` `that download already succeeded` (`golane.ErrSucceeded`, P18/D5). Non-POST → 405. |
| POST | `/jobs/delete?id=` | Unchanged contract (`handler.go:180-195`): 204 for queued or finished (`Lane.Remove`), 409 `that job is running and can't be removed`, 404 `job not found`, 405. |
| GET | `/settings.json` | `200 {"python_bin":string,"age_out_days":int,"concurrent_downloads":int,"show_in_taskmaster":bool,"queue_paused":bool,"queue_paused_by":string}`. `queue_paused_by` is `LaneSettings.PausedBy`: `""` when running, `"brake"` for the hand brake (G3), `"owner:utuber"` for the ☰/panel toggle. It is read-only, and a POST ignores it. The response also includes `"brake_engaged":bool` (`LaneSettings.BrakeEngaged`, read-only), which covers the case where the brake was persisted as engaged in the DB. That matters in headless mode (P16): no taskmaster UI exists to release the brake, so nothing in the lane runs, and utuber must say why. |
| POST | `/settings.json` | Body `{"python_bin"?:string,"age_out_days"?:int,"concurrent_downloads"?:int,"show_in_taskmaster"?:bool,"queue_paused"?:bool}`, all pointer-decoded. Validation runs **first**, and nothing is applied on failure: python_bin must match the pattern (`settings.go:19`); age_out_days must be in 1–365; concurrent_downloads in 1–`config.MaxUtuberWorkers` (8). Invalid → **400** with a message naming the field. Then it applies the lane patch (DB) and then python_bin (file). If the python_bin write fails after the lane patch succeeded → **500** `settings partially saved: <err>` (documented). Returns the same shape as GET. Other methods → 405 with `Allow: GET, POST` (unchanged, `settings.go:127-129`). |
| GET | `/ytdlp-update`, `/downloads/…`, `/` | Unchanged. |

### 4.11 Shared queue panel (`web/shared/ts/queuepanel.ts`, `web/shared/ts/patchlist.ts`)

- **`patchlist.ts`**: move `patchList` and `PatchListOptions` verbatim from `web/taskmaster/js/ui/live.ts:176-241`. The key attribute becomes `data-ui-key`. `live.ts` keeps `LiveController` only. Taskmaster importers switch to `@shared`: `board.ts:26`, `metrics.ts:11`, `taskdetail.ts:27`. `board.ts:419` changes `data-tm-key` to `data-ui-key`.
- **`status.ts` move**: `ExecStatus` (the type, `web/taskmaster/js/status.ts:9`), `statusSymbol` (`:24-32`) and `effectiveStatus` (`:37-39`) move to `web/shared/ts/status.ts`. **All three are barrel exports**, because the build externalises every `@shared` and `@shared/*` specifier to the single `/shared/dist/shared.mjs` (`scripts/build-web.mjs:82`, filter `/^@shared(\/.*)?$/`). A name missing from the barrel would be `undefined` at runtime, even if a deep import typechecks via `tsconfig.json` `paths`. The taskmaster `status.ts` does `import { statusSymbol, effectiveStatus } from '@shared'` and `export type { ExecStatus } from '@shared'`, so its existing importers are unchanged. It keeps `statusBadgeClass`/`renderStatusBadge` (their CSS classes carry color literals, `web/taskmaster/style.css:250-254`, which shared CSS gate clause 7 forbids).
- **`queuepanel.ts`**:
```ts
export type QueueSection = 'running' | 'upnext' | 'recent';
export type QueueActionKind = 'cancel' | 'pause' | 'resume' | 'rerun' | 'remove';
export interface QueueProgress { pct: number | null; label: string }
export interface QueuePanelAdapter<T> {
  key(item: T): string;
  section(item: T): QueueSection | null;          // null → not rendered
  title(item: T): string;
  status(item: T): string;                        // e.g. 'running' | 'queued' | 'success' …
  progress?(item: T): QueueProgress | null;       // bar only when non-null (FR-T3)
  meta?(item: T): string;
  actions(item: T): QueueActionKind[];
  onAction(action: QueueActionKind, item: T, button: HTMLButtonElement): void | Promise<void>;
  renderBadge?(badge: HTMLElement, item: T): void; // default: shared symbol badge (ui-queue-badge)
  decorate?(row: HTMLElement, item: T, created: boolean): void; // module extras (links, pid, drag)
  onTitleClick?(item: T): void;
}
export interface QueuePanelOptions {
  titles?: Partial<Record<QueueSection, string>>;     // defaults: Running / Up next / Recent
  emptyText?: Partial<Record<QueueSection, string>>;  // defaults: 'nothing running' / 'queue is empty' / 'no history yet'
  recentLimit?: number;                               // default Infinity
  recentCollapsible?: boolean;                        // default true (<details>)
  recentOpen?: boolean;                               // default false
  header?: { paused: boolean; note?: string; onTogglePause(): void | Promise<void> } | null; // default null
}
export class QueuePanel<T> {
  constructor(host: HTMLElement, adapter: QueuePanelAdapter<T>, opts?: QueuePanelOptions);
  update(items: T[]): void;            // within a section, input order is preserved
  setPaused(paused: boolean, note?: string): void; // header pause state + optional note
  list(section: QueueSection): HTMLElement;
  destroy(): void;
}
// pure, exported from the module (not the barrel) for tests:
export function bucketQueue<T>(items: T[], section: (t: T) => QueueSection | null, recentLimit?: number): Record<QueueSection, T[]>;
export function progressView(p: QueueProgress | null | undefined): { show: boolean; pct: number; indeterminate: boolean; label: string };
export const ACTION_GLYPHS: Record<QueueActionKind, { glyph: string; label: string }>;
// cancel '✖' Cancel · pause '⏸' Pause · resume '▶' Resume · rerun '↻' Re-run · remove '🗑' Remove
```
- Rendering: `.ui-queue` > (`.ui-queue-header`?) + `.ui-queue-section.ui-queue-section-{running|upnext|recent}`. Each section has a `.ui-queue-title` and a `.ui-queue-list`. Rows are `.ui-queue-row` with `.ui-queue-name`, `.ui-queue-badge`, `.ui-queue-meta`, `.ui-queue-progress` > `.ui-queue-progress-bar` (width = pct%, class `.ui-queue-progress-indeterminate` when pct is null), `.ui-queue-progress-label`, and `.ui-queue-actions` > `button.ui-queue-action.ui-queue-action-{kind}` (text = glyph; `title` and `aria-label` = label). Rows are patched via `patchList`; there is no innerHTML. The empty note is keyed `__empty__`.
- CSS goes in `web/shared/css/components.css`, uses tokens only, and every selector starts with `.ui-` (gate clause 7, `scripts/check-shared-css.mjs:342-377`). Badge tones use `var(--color-success|--color-danger|--color-warning|--color-primary|--color-text-muted)` text on `var(--color-surface-2)`.
- **Barrel** (`web/shared/ts/index.ts`). The exact additions are:
  - **4 values:** `QueuePanel`, `patchList`, `statusSymbol`, `effectiveStatus`.
  - **7 types:** `QueuePanelAdapter`, `QueuePanelOptions`, `QueueSection`, `QueueActionKind`, `QueueProgress`, `PatchListOptions`, `ExecStatus`.
  
  `scripts/check-shared-barrel.mjs:37` (`ALLOWED_VALUES`, 13 → 17) and `:38` (`ALLOWED_TYPES`, 14 → 21) add exactly these names, and nothing else. The header comment's counts are updated. `bucketQueue`, `progressView` and `ACTION_GLYPHS` are **not** barrel exports: the tests import them from `./queuepanel` directly, because the test driver bundles sources and does not go through the externalised barrel.
- **Tests:** add `web/shared/ts/queuepanel.test.ts` and `web/shared/ts/patchlist.test.ts` to `scripts/test-web.mjs` `suites` (`:43-53`) as `esbuild-cjs`. The hand-rolled element stubs follow the `toast.test.ts` idiom (no jsdom).

**Taskmaster adoption (first, per D10)** in `web/taskmaster/js/board.ts`:
- `createLaneEl` (`board.ts:169-243`): replace `buildSection`/`buildRanSection` and the three lists with `new QueuePanel<BoardItem>(body, tmAdapter, { recentLimit: RAN_PER_LANE, titles: { recent: 'Recent runs' }, emptyText: { running: 'nothing running', upnext: 'lane is empty', recent: 'no history yet' } })`, stored in a `WeakMap<HTMLElement, QueuePanel<BoardItem>>`. Delete `buildSection`, `buildRanSection` and `toggleEmptyNote` (`board.ts:245-274, 413-426`).
- `type BoardItem = { t: 'exec'; exec: TaskExecution; sec: 'running' | 'recent' } | { t: 'task'; task: Task; pending: TaskExecution | null }`.
- `updateLaneEl` (`board.ts:318-411`) computes the same three sets (`runningExecs`, `ranExecs`, `upNextTasks`) with one change. `upNextTasks` excludes func tasks (`task.kind`) that have **no** pending execution, which stops finished downloads from showing as "Up next". It then calls `panel.update([...running, ...upnext, ...recent])`.
- Adapter actions:
  - Running shell exec: `['pause'|'resume', 'cancel']`. Pause/resume keeps today's SIGSTOP/SIGCONT endpoints (`requestProcessToggle`), and the choice follows `exec.suspended`.
  - Running func exec: `['cancel']`.
  - Up-next func task with pending: `['cancel']` (POST cancel on the pending id).
  - Recent: shell → `['rerun']`; func → `['rerun', 'remove']` (remove = `DELETE /api/tasks/{name}`, behind `confirmDialog`).
- `decorate` re-adds the module-specific parts. For running execs: the pid label and the ↗ output button (`board.ts:430-480` markup). For shell task rows: `draggable`, dragstart, the "Up next" button and the cooldown status (`board.ts:609-679`). It also adds the legacy classes `exec-row exec-row-{kind}` / `task-row`, so the existing `style.css` still applies. `title` returns `task.label || task.name`.
- `wireDropTarget(panel.list('upnext'), el)` replaces `board.ts:240`.
- `handleBoardEvent` (`board.ts:103-114`): `task-progress` patches `state.executions[i].progress_pct/label` and calls `render()`, **without** `refreshAll()`, so there is no refetch storm on a small host. Every other type keeps calling `refreshAll()`.
- `api.ts` adds `rerunExecution(id)` and the `Task`/`TaskExecution`/`Lane` field additions.
- `designer.ts` lane dropdown: exclude lanes with `owner`.
- `taskdetail.ts:56-57`: for a func task, render `kind`, `label` and pretty-printed `payload` read-only instead of `command`.

**utuber adoption (second)** in `web/utuber/js/main.ts`:
- `refreshJobs`/`jobRow`/`parseProgress` (`main.ts:172-263`) are replaced by one `QueuePanel<UJob>` mounted in `#jobs-list`, with `header: { paused, onTogglePause }` driven by `/settings.json` `queue_paused`. When `queue_paused_by === "brake"`, the header shows "Paused by taskmaster hand brake" and disables the toggle, because releasing the brake is taskmaster's action (G3). When `brake_engaged` is true (even if the lane itself is not marked paused), the header shows "Taskmaster hand brake is engaged — nothing will start until it is released in taskmaster" and disables the toggle. `docs/utuber.md` documents that releasing it requires the taskmaster module to be routed (`DELETE /api/brake`). A new `QueuePanelOptions.header.note?: string` renders that text in `.ui-queue-header-note`, and `setPaused(paused, note?)` updates it.
- Adapter:
  - section: queued → upnext, running → running, else recent.
  - title: `label`.
  - meta: the SxxEyy tag (same rule as `main.ts:192-193`) plus the mode.
  - progress: running → `progress`, else null.
  - actions: queued `['cancel', 'remove']`; running `['cancel']`; failed/canceled `['rerun', 'remove']`; success `['remove']`.
  - decorate: on success, the Play/Download links (the markup from `main.ts:199-214`, built with DOM APIs and `encodeURIComponent`); on failure, the error line (textContent).
- Keep the confirm texts from `main.ts:362-367`. Cancel of a running job asks "Stop this download? The partial download is abandoned."
- Keep `setInterval(refreshJobs, 2000)` (`main.ts:378`, owner floor Q3). `jobs-count` stays.
- ☰ (`main.ts:6-68`) gains a "Queue" section with three fields: "Delete finished downloads after (days)" (number, 1–365), "Downloads at once" (number, 1–8) and "Show in taskmaster" (checkbox). Each saves on `change` via `POST /settings.json` with only that field, and shows the returned values.

### 4.12 Compile-forced call sites (R5), verified by grep

- `utuber.Build`: `cmd/server/main.go:405`, `internal/utuber/build_test.go:37`, `internal/utuber/build_test.go:156`.
- `taskmaster.Open` is new, so it has no existing callers. `taskmaster.Build` keeps its signature (`build.go:37`), and its callers in `internal/taskmaster/build_test.go` are unchanged.
- `worker.PublishBoardEvent`: `internal/taskmaster/coordinator/coordinator.go:45` and `internal/taskmaster/worker/worker.go:212,272,344`.
- `coordinator.New`: `internal/taskmaster/build.go:133` (moves to `engine.go`), `coordinator/board_test.go:35,96`, `coordinator/coordinator_test.go:54,823`.
- `buildModule`: `cmd/server/main.go:319`, `cmd/server/dispatcher_auth_test.go:94`.
- `patchList` importers: `web/taskmaster/js/board.ts:26`, `metrics.ts:11`, `taskdetail.ts:27`.

---

## 5. Requirements → implementation map

| Req | Where (files) | Notes |
|---|---|---|
| FR-T1 | `golane/golane.go` (Kind, NewKind, LaneSpec, Host); `worker/funcs.go`; `engine.go` `RegisterLane`; `db.go` migration 5 (`tasks.kind/payload/payload_version`, `lanes.owner`) | A lane declares its kinds at `RegisterLane`, and `Submit` rejects any other kind (`ErrUnknownKind`). |
| FR-T2 | `worker/worker.go` `poll` (`:134-139` candidate-building kind filter, before `toRun` at `:141`), `runTask` branch after `:201-220`, `runFunc`, `startHeartbeat`; `worker/cancelexec.go`; `db.ClaimFuncExecution`/`CancelPendingFuncExecution` | Width via `CountRunningInLane` (`db.go:386`); lane/task pause via eligibility (`db.go:531-533`); cancel via context; reconcile via `ReconcileOrphans` (no PID → failed). |
| FR-T3 | migration 5 (`progress_pct/progress_label`); `worker/progress.go`; `BoardEvent` `task-progress`; `handlers_executions.go` overlay; `queuepanel.ts` bar; `board.ts` event patch | Persist throttled to `progress_interval_ms` (config); memory unthrottled. |
| FR-T4 | `lanes.retention_days`; `db.PruneOwnedLane`; `engine.go` pruner | §2.4 |
| FR-T5 | `db.RerunTask`; `POST /api/executions/{id}/rerun`; `Lane.Rerun`; panel `rerun` action | Same payload by construction (task row). |
| FR-T6 | `lanes.hidden`; `worker/visibility.go`; coordinator filtering (§4.6); `PublishBoardEvent` filter; `Lane.UpdateSettings(Hidden)` | Metrics excluded per Q4. |
| FR-U1 | `internal/utuber/build.go` `buildWith` + `RegisterLane`; `handler.go` `/enqueue` → `Lane.Submit`; delete `internal/utuber/jobs/` | |
| FR-U2 | TM storage; `poll` skip-unregistered; `TestUtuber_QueuedJobSurvivesEngineRestart` | |
| FR-U3 | `/jobs/cancel`, `/jobs/rerun`, `/jobs/delete`; `Lane.Cancel/Rerun/Remove` | |
| FR-U4 | `/settings.json` extension; ☰ fields in `main.ts` | |
| FR-U5 | `web/utuber/js/main.ts` QueuePanel adapter | |
| D1 | `golane` in-process; payload → argv (`media/downloader.go:54-72` unchanged argv building) | No shell anywhere. |
| D2 | func tasks never touch `TaskExecutor`/`SudoGate`; `sudo=0` fixed at submit; PUT blocked (409) | `/ytdlp-update` unchanged (`handler.go:152-176`). |
| D3 | progress model + `RunContext.Progress` | P12 narrows the shell reporter. |
| D4 | `processor.run` keeps tagging/filing/rename/`history.Record` | |
| D5 | `/enqueue` duplicate check unchanged (`handler.go:116-126`) | `/jobs/rerun` refuses `success`. |
| D6 | `LaneSpec.InitialRetentionDays: 10`; ☰ `age_out_days` | |
| D7 | lane width via ☰ `concurrent_downloads` | The brake still applies (fact 8). |
| D8 | nothing migrated; the lane starts empty | |
| D9 | `lanes.hidden` + filtering; ☰ "Show in taskmaster" | The lane still runs (the worker ignores `hidden`). |
| D10 | `web/shared/ts/queuepanel.ts`; taskmaster adoption (Phase 4) before utuber (Phase 5) | |

---

## 6. Implementation phases

Each phase ends green on its own verification. Commit per phase. Gates: `go vet ./...` and `go test -race ./...` (= `make test`) after every Go phase; `make web-verify gates test-web` after every web phase.

### Phase 1 — TM storage: migration 5, models, DB methods (no behaviour change yet)
Files: `internal/taskmaster/db/db.go`, `models/models.go`, `db/db_test.go`.
- Add the DSN `_foreign_keys=1`, the transactional migration 5, the scan/column changes (§4.2), the `AddTask` WHERE guard, the `UpdateTask` allowlist, the eligibility change, and every new method.
- Verify:
  ```sh
  go test -race -count=1 ./internal/taskmaster/db/
  go test -race -count=1 ./internal/taskmaster/...
  ```
  All pre-existing tests pass unmodified, except `TestHandleUpdateTask_*` if one relied on a 500 for unknown keys. None does today (checked: `coordinator_test.go:360-400` uses `sudo`/`command` only). New tests: §7.1.

### Phase 2 — TM engine: golane, worker func path, progress, retention, visibility, HTTP
Files: new `internal/taskmaster/golane/golane.go`, `worker/{funcs,visibility,progress,cancelexec}.go`, `engine.go`, `lane.go`; edits to `worker/worker.go`, `worker/board_events.go`, `build.go`, `coordinator/*.go`, and the tests listed in §4.12.
- Verify:
  ```sh
  go test -race -count=1 ./internal/taskmaster/...
  grep -rni utuber internal/taskmaster | wc -l   # → 0
  ```

### Phase 3 — Server wiring and config
Files: `cmd/server/main.go`, `cmd/server/dispatcher_auth_test.go` (`buildControlDispatcher`), `cmd/server/dispatch_test.go` (`utuberTestConfig` gains `cfg.Taskmaster.DBPath = filepath.Join(root, "taskmaster.db")` and `cfg.Taskmaster.StaticDir = staticDir`), `internal/platform/config/config.go`, `config_taskmaster_test.go`.
- utuber still compiles against a **temporary** `utuber.Build(cfg, host)` that ignores `host`, so the phase is shippable. Phase 5 replaces its body.
- Verify:
  ```sh
  go test -race -count=1 ./cmd/server/ ./internal/platform/config/
  ```

### Phase 4 — Shared queue panel, taskmaster adoption (D10 first consumer)
Files: `web/shared/ts/{queuepanel,patchlist,status}.ts`, `index.ts`, `queuepanel.test.ts`, `patchlist.test.ts`, `web/shared/css/components.css`, `scripts/check-shared-barrel.mjs`, `scripts/test-web.mjs`, `web/taskmaster/js/{board,api,status,metrics,taskdetail,designer}.ts`, `web/taskmaster/js/ui/live.ts`, and the rebuilt artifacts (`web/shared/dist/shared.mjs`, `web/shared/dist/shared.css`, `web/taskmaster/js/bundle.js`).
- Verify:
  ```sh
  npm run typecheck
  npm run test:web
  npm run build && git status --porcelain web/*/js/bundle.js web/shared/dist   # artifacts committed
  make web-verify gates
  ```
  Then the manual TM board checklist (§7.3 E1).

### Phase 5 — utuber migration and deletion of `internal/utuber/jobs`
Files: `internal/utuber/{build,handler,settings}.go`, `media/exec.go`, `media/exec_test.go`, `build_test.go`, `handler_test.go`, `settings_test.go`, a new `main_test.go` (goleak `TestMain` + `SetPollIntervalForTest(50ms)`), `web/utuber/js/main.ts`, `web/utuber/style.css` (remove dead `.progress-*` rules only where no selector remains), `web/utuber/js/bundle.js`; delete `internal/utuber/jobs/`.
- Verify:
  ```sh
  test ! -e internal/utuber/jobs && echo gone          # → gone
  grep -rn "utuber/jobs" --include='*.go' . | wc -l    # → 0
  go test -race -count=1 ./internal/utuber/... ./cmd/server/
  npm run typecheck && npm run build && make gates
  ```

### Phase 6 — Docs, full gate, e2e-equivalent run
Files: `docs/taskmaster.md` (func tasks, progress, retention, hidden lanes, rerun route, progress_interval_ms, headless OwnedLanesOnly mode (P16), config-lane vs owned-lane rule (P19), func metrics aggregation (P17), the fact-6 corrections, N1–N4, and a **"Schema v5: no downgrade"** warning box with the K9 consequence and recovery), `docs/utuber.md` (new endpoints and shapes, N5–N8, remove the "Known limitation" section, since P15 fixes it), `docs/FRD-utuber-taskmaster-lane.md` status line → "planned; see docs/PLAN-utuber-taskmaster-lane.md".
- Verify:
  ```sh
  make check
  ```
  (This is `web-verify test-web gates test`, `Makefile:71`.) The §7.3 E1–E3 runs are owner sign-off (A12). The executor lists them as pending.

---

## 7. Test plan (expanded, deliberate mode)

### 7.1 Unit

**`internal/taskmaster/db/db_test.go`** (`package db_test`, `:memory:` unless stated)
- `TestDSNFor_SetsWALAndForeignKeys` (internal, `package db`, new file `db_internal_test.go`): `dsnFor("/x/t.db") == "/x/t.db?_journal_mode=WAL&_foreign_keys=1"` and `dsnFor(":memory:") == ":memory:"`. (A behavioural FK test would already pass today via the pool PRAGMA at `db.go:30`, so it could not detect the fix.)
- `TestSeedLanes_SkipsOwnedLane` (in `internal/taskmaster/build_test.go`). `Open`, `RegisterLane` "owned" with width 3, `Close`. Re-`Open` with config `Lanes: [{Name:"owned", Width:9}]`. The width is still 3 and the owner unchanged.
- `TestRegisterLane_ConfigLaneCollisionError`. With a config lane "owned" (owner `''`), `RegisterLane({Name:"owned"…})` returns an error wrapping `db.ErrLaneOwnedByOther` whose message contains `already exists as a regular taskmaster lane`.
- `TestGetMetrics_FuncTasksAggregatedByKind`. Three func tasks of kind `test.echo` in lane "owned", each with a success metric, plus one shell task. This gives 2 summary rows, in deterministic order; the func row has `task_name == "test.echo"`, `kind == "test.echo"` and `success_count == 3`. Then `GetMetricsExcludingLanes("", "", 24, []string{"owned"})` returns only the shell row, which proves the hidden-lane exclusion still holds under kind aggregation. And `GetMetrics("", <one func task name>, 24)` returns 1 row with `success_count == 1` (the task-detail path).
- `TestOpen_OwnedLanesOnlySkipsShellLanes`. Pre-seed a lane "shell" and one enabled shell task (`echo hi`) via a raw `db.Open` + `Close`, as `build_test.go:53-62` does. Then `Open(cfg, OpenOptions{OwnedLanesOnly: true})` and register one func lane. The func job reaches `success` and the shell task has 0 executions after 1 s at a 50 ms poll.
- `TestMigration5_FreshDBHasColumns`: `PRAGMA table_info` for lanes, tasks and task_executions lists every §4.1 column; `MAX(version)=5`.
- `TestMigration5_RollsBackAtomically`:
  1. Open a file DB (v5), then close it.
  2. Using a raw `sql.Open`: `DROP INDEX idx_tasks_kind`; `ALTER TABLE … DROP COLUMN` for every migration-5 column **except** `task_executions.result`; `DELETE FROM schema_version WHERE version=5`.
  3. `db.Open` returns an error containing `migration 5`.
  4. Raw re-open: `lanes.owner` is absent and `MAX(version)=4`.
- `TestGetEligibleTasks_FuncTaskOnlyWithPending`: a func task with no pending execution is not eligible (after its pending is canceled); a shell never-run task is still eligible (R3 pin).
- `TestGetEligibleTasks_ShellCanceledQuirkUnchanged`: a shell one-shot whose only execution is `canceled` is eligible, which pins today's behaviour (P3).
- `TestAddTask_DoesNotClobberFuncTask`.
- `TestUpdateTask_RejectsUnknownField`: `errors.Is(err, db.ErrUnknownTaskField)`.
- `TestSubmitFuncTask_FIFOPositionsAndPending`.
- `TestClaimFuncExecution_OnlyFromPending`.
- `TestCancelPendingFuncExecution_RecordsMetric_ShellIgnored`.
- `TestRerunTask_BusyAndRequeuePosition`: a func task's position becomes max+1; a shell task's position is unchanged.
- `TestRemoveIdleTask_RunningBusy_QueuedRemoved`.
- `TestPruneOwnedLane_DeletesTaskExecsMetrics`: finish an execution, then `PruneOwnedLane(lane, time.Now().Add(time.Hour))` removes it with its metrics; a cutoff of `time.Now().Add(-time.Hour)` keeps it; a pending or running task is never pruned; a shell task in the same lane is never pruned.
- `TestEnsureOwnedLane_SeedOnceAndOwnerConflict`.
- `TestListExecutionsExcludingLanes_KeepsDeletedTaskRows`.
- `TestGetMetricsExcludingLanes`.

**`internal/taskmaster/golane/golane_test.go`**
- `TestNewKind_StrictDecodeVersionAndValidate`: unknown field → error; version 2 → `unsupported payload version`; validate error surfaces.

**`internal/taskmaster/worker/`** (`func_test.go` in `package worker_test`; `func_internal_test.go` in `package worker`)
- `TestFuncTask_RunsWithPayloadAndStoresResult`.
- `TestFuncTask_RespectsLaneWidth`: width 1, two jobs, the second starts only after the first returns.
- `TestFuncTask_UnregisteredKindStaysPending_ThenRunsAfterRegister` (FR-U2, Q1).
- `TestFuncTask_ShutdownRecordsFailed`.
- `TestFuncTask_PanicRecordedFailed`.
- `TestFuncTask_BadPayloadVersionFailsCleanly` (Q2).
- `TestFuncTask_LabelUpdate`.
- `TestFuncTask_BrakeEngagedBeforeStart` (mirrors `worker_race_internal_test.go:33-67`).
The race and shutdown tests below are internal (`package worker`). Each builds a worker with `NewWithExecutor` + `EnableFuncTasks`, sets `w.runCtx = context.Background()`, creates the pending func execution via `db.SubmitFuncTask`, then loads the task with `task, _ := db.GetTask(name)` (so `task.Kind`, `Payload` and `PayloadVersion` are populated and `runTask` takes the func branch), calls `db.AcquireLock(taskID, "w", time.Minute)` (mirroring `poll`, `worker.go:144`), and calls `w.wg.Add(1); w.runTask(task, execID, time.Now())` directly on the test goroutine, as `worker_race_internal_test.go:33-67` does. There is no poll loop, so the tests are deterministic.
- `TestCancelExecution_PendingCancelBeforeClaim`. `beforeFuncClaimHook` (runTask is already registered at this point) calls `CancelExecution(execID)` and records the outcome. Assert:
  - the outcome is `canceled`;
  - the Kind's Run was never called;
  - the row is `canceled`;
  - exactly 1 `task_metrics` row exists for the execution;
  - the lock row is gone.
- `TestCancelExecution_ClaimThenCancel`. `afterFuncClaimHook` calls `CancelExecution(execID)`. Assert:
  - the outcome is `canceling`;
  - Run is never called (the step 3b ctx check);
  - the row is `canceled`, not `failed`;
  - exactly 1 metric.
- `TestCancelExecution_CanceledBeforeRegistration`. With no runTask started, `CancelExecution(execID)` → `canceled`. Then calling `runTask` bails at the claim: Run is not called, the metric count is still 1, and the lock is released.
- `TestFuncTask_BrakeVsPendingCancel_OneMetric`. Subscribe to the board broker **before** anything else, then engage the brake, call `CancelExecution` (→ `canceled`), and call `runTask` directly. Its brake path finds 0 rows, so there is exactly 1 metric and exactly 1 `task-finished` event (drain the subscription with a 1 s timeout after `runTask` returns).
- `TestFuncTask_BrakeAfterPreCheck_CanceledWithoutRun`. Brake off at `runTask` entry. `beforeFuncClaimHook` sets `brake.Set(true)`. Assert that Run is never called, the row is `canceled` with error `hand brake engaged`, and there is 1 metric.
- `TestFuncTask_ShutdownBeforeClaimStaysPending`. `w.runCtx` is a cancelable context. `beforeFuncClaimHook` cancels it (a worker shutdown, not via `cancels`). Assert that Run is never called, the row is still `pending` with `started_at` NULL, there are 0 metrics, and the lock row is gone. Then build a second worker with a fresh ctx and run it through `poll` with `SetPollIntervalForTest(50ms)`: the job reaches `success` (FR-U2).
- `TestFuncTask_CancelRunningRecordsCanceled` covers the end-to-end running cancel. Run blocks on `<-ctx.Done()`. Wait for `running`, call `CancelExecution` (→ `canceling`), then use `require.Eventually` to wait for status `canceled`.
- `TestProgressReporter_ThrottleAndFreshness` (internal, **deterministic**). Build a `progressReporter` directly on a `:memory:` DB with a real func execution row, `interval = 100ms`, and `now` = a fake clock (`var t0 time.Time; now = func() time.Time { return t0 }`). Make 50 `Report(i, "Downloading")` calls, advancing `t0` by exactly 10 ms before each. Assert:
  - every call is immediately visible via `ProgressRegistry.Get`, equal to `i`;
  - `progressPersistHook` fires **exactly 5** times, at clock offsets 10, 110, 210, 310 and 410 ms (the first call always persists);
  - the DB row holds the last persisted value;
  - after `close()`, `Report` is a no-op and the hook count is unchanged.
- `TestProgressReporter_LabelChangePersistsImmediately` (internal, fake clock). `Report(10, "Downloading")` persists. Advance 1 ms, then `Report(-1, "Converting")` persists immediately (hook count 2), even though it is inside the interval. `Report(-1, "Converting")` again after 1 ms does not persist.
- `TestPublishBoardEvent_DropsHiddenLane`.
- `TestReconcileOrphans_FuncRunningMarkedFailed` (in `reconcile_test.go`).

**`internal/taskmaster/coordinator/`**
- `TestRerunExecution_Shapes`: 201 / 400 / 404 / 409×2.
- `TestCancelExecution_PendingFuncCanceled_PendingShellNotRunning`.
- `TestOwnedLaneGuards`: table-driven over every row of the §4.6 guard table.
- `TestHandleUpdateTask_RejectsUnknownField` (400), `TestHandleUpdateTask_FuncTaskConflict` (409), `TestHandleDeleteTask_FuncRunningConflict` (409).
- `TestHiddenLane_InvisibleEverywhere`: lanes and tasks lists, GET by name (404), executions list, `/output`, cancel and rerun (404), metrics.
- `TestMetrics_HiddenLaneExcludedByPolicy`: its comment names `metricsIncludeHiddenLanes`.
- `TestListExecutions_ProgressOverlayForRunning`.
- `TestBoardEvents_TaskProgressShape`.

**`internal/platform/config/config_taskmaster_test.go`**
- `TestTaskmasterProgressIntervalNormalization`: 0→2000, 50→100, 5000→2000, 500→500, -1→error.

**`internal/utuber/`**
- `media/exec_test.go`:
  - `TestOSExecutorRunJoinsOutput`: `sh -c 'i=0; while [ $i -lt 500 ]; do echo line$i; i=$((i+1)); done'`. The count is exactly 500 immediately after `Run` returns.
  - `TestOSExecutorCancelKillsGroup`: `sh -c 'sleep 30 & sleep 30'`, cancel after 100 ms, `Run` returns in under 5 s.
- `handler_test.go`: the `processor.run` tests are ported from `handler_test.go:226-545` to a recording `fakeRunContext`. `TestRunProgressSequence` asserts the labels in order `Fetching metadata`, `Downloading`, `Downloading`@42, and for audio also `Converting`. It also covers video/audio file naming, errors and history records, `TestSafe` and `TestOutputNameOmitsZeroSeasonEpisode` (kept).
- `build_test.go` uses a real `taskmaster.Open(config.TaskmasterConfig{DBPath: <tmp>/tm.db, StaticDir: <tmp>, ProgressIntervalMs: 50}, taskmaster.OpenOptions{})` with `t.Cleanup(func(){ require.NoError(t, e.Close()) })`:
  - `TestJobsWireFormat`: exact key set.
  - `TestWorkersZeroNeverRuns`: ported from `:174-202`, 500 ms of `queued`.
  - `TestBuild_WorkersSeedsWidthOnlyOnce`.
  - `TestCancelRerunDeleteHTTP` covers every status code in §4.10, plus 405 on GET.
  - `TestSettingsJSON_RoundTripAndValidation`, `TestSettingsPOST_PartialLeavesPythonBin` (N6).
  - `TestUtuber_FullDownloadWithFakeExecutor`: width 1 → `success` with `output_file`, `history.json` updated.
  - `TestUtuber_QueuedJobSurvivesEngineRestart`: width 0, submit, `Close`, re-`Open`, re-`buildWith` → the job is still `queued`.
  - `TestUtuber_HiddenLaneStillRuns`.
- `settings_test.go`: existing tests are kept. Their construction is updated only where `settingsStore` is unchanged, and it is.

**Web (`npm run test:web`)**
- `queuepanel.test.ts`: `bucketQueue` (order preserved, `recentLimit`, null section dropped); `progressView` (null → hidden; pct clamp −5→0 and 150→100; null pct → indeterminate with label); `ACTION_GLYPHS` completeness; barrel exports `QueuePanel`/`patchList`/`statusSymbol`/`effectiveStatus`; and a stubbed-DOM `QueuePanel.update` that proves a row is **reused** (same node identity) across updates and that the progress element exists only when `progress()` is non-null.
- `patchlist.test.ts`: insert, reorder, remove, update in place, and `data-ui-key`.

### 7.2 Integration
- `internal/taskmaster/build_test.go` gains `TestOpen_FuncLaneFullCycle`, which uses `Open` plus `RegisterLane` with a test kind named `test.echo` and asserts:
  - a job whose payload says `{"fail":true}` → `failed`, then `POST /api/executions/{id}/rerun` → 201 → it runs again;
  - a normal job: submit → running (progress visible via `GET /api/executions`) → success with `result`;
  - rerun of the successful job → **409** (P18);
  - remove via `DELETE`; prune via `SetPruneIntervalForTest(50ms)` with retention changed through `UpdateSettings`. `Close` leaks nothing (goleak).
- `cmd/server/dispatch_test.go` gains four tests.
  - `TestDispatcher_TaskmasterAndUtuberShareOneEngine`: routes both modules. `len(dispatch.closers) == 1`. A `POST /enqueue` through the utuber host returns 204. `GET /api/tasks` through the taskmaster host lists a task with `kind == "utuber.download"`. Loop 5 times (`for i := 0; i < 5; i++` inside the test), to exercise random map order. **Each iteration** builds a fresh config from `utuberTestConfig`, which gives a new `t.TempDir()` for the download dir, static dir and `taskmaster.db`, then builds its own dispatcher via `buildDispatcher`, and calls `dispatch.Close()` at the end of the iteration (not via `t.Cleanup`), so no two engines share a DB file or outlive their iteration.
  - `TestDispatcher_UtuberWithoutTaskmasterRoute_Headless`: utuber alone. `/jobs.json` returns 200 `[]`, and the engine closer is registered (`len(dispatch.closers) == 1`). Headless worker scoping (P16) is pinned by `TestOpen_OwnedLanesOnlySkipsShellLanes`.
  - `TestDispatcher_EngineOpenFailure_BothModules503`: `cfg.Taskmaster.DBPath` points under a regular file. The utuber and taskmaster hosts both return 503 naming their module, and grocery stays healthy.
  - The existing `TestUtuberTwoHostnamesShareOneInstance` (`:367-397`) is kept as is. Its `queued` assertion stays valid under R7.

### 7.3 E2E-equivalent (manual; **owner sign-off**, see A12; there is no browser test harness in `make check`)
- **E1, the taskmaster board after Phase 4.** Run `make build && ./unified-webapp -config local-test/config.json`, open `http://taskmaster.test:8080`, then check:
  1. Lanes render Running / Up next / Recent runs.
  2. Drag-reorder and cross-lane move work.
  3. Pid, ↗ output, ⏸/▶ and ✖ work on a `sleep 60` task.
  4. Rerun appears on recent runs and creates a new execution.
  5. Up-next button and cooldown label unchanged.
  6. Lane pause dialog unchanged.
  
  Optionally, `tools/baseline-shots` gives a before/after screenshot diff (`node shoot.js pre-lane` before Phase 4, `node shoot.js post-lane` after).
- **E2, utuber after Phase 5,** with a real yt-dlp on a short public-domain clip:
  1. Submit, then watch the progress bar advance at least every 2 s.
  2. Cancel mid-download → `canceled`, and the yt-dlp process is gone (`pgrep yt-dlp` prints nothing).
  3. Re-run → success, Play/Download links work.
  4. Remove. Duplicate submit → banner.
  5. ☰ width 2 with two jobs → both run.
  6. Toggle "Show in taskmaster" off → the TM board, `/api/lanes` and Metrics show no `utuber` lane. On again → the lane reappears.
  7. `kill -9` the server mid-download and restart → that job shows `failed` ("interrupted…"), and queued jobs are still queued and then run.
- **E3, retention.** Set age-out to 1 day, then with the server stopped run `sqlite3 local-test/data/taskmaster/taskmaster.db "UPDATE task_executions SET finished_at = finished_at - 2*86400000"`. Restart: the finished jobs are gone and the log shows the prune line.

### 7.4 Observability
New log lines (exact prefixes):
- `taskmaster: lane "<l>" registered by "<o>" …` (RegisterLane).
- `taskmaster: pruned N job(s) from lane …`.
- `taskmaster: func task <name> (exec <id>) panicked: …` plus the stack.
- `taskmaster: func task <name> (exec <id>): invalid payload: …`.
- `taskmaster: no function registered for kind "<k>"; <n> task(s) waiting`, logged once per kind per process, not per poll: `poll` records kinds it has already logged in a `map[string]bool` on the Worker.
- The config clamp logs from §4.8.

`GET /api/executions` exposes `progress_pct`/`progress_label`/`result`, so a stuck job is diagnosable over the API. There are no new metrics endpoints.

### 7.5 Existing tests edited (the only allowed edits, R3)
- `build_test.go:37` (utuber) → `buildWith(cfg, engine, exec)` via the new helper.
- `internal/utuber/build_test.go:86-123` → replaced by `TestJobsWireFormat` (N5).
- `internal/utuber/build_test.go:174-202` → ported (R7).
- `internal/utuber/handler_test.go:55-212, 226-545, 628-652` → ported to the lane-based helpers. The assertions keep their intent: enqueue validation, duplicate block and force, delete 204/409/404/405.
- `coordinator.New` call sites (§4.12) get `nil, nil`.
- `cmd/server/dispatch_test.go:309-329` (`utuberTestConfig`) gains the Taskmaster paths.
- `cmd/server/dispatcher_auth_test.go:88-108` gets the `moduleDeps` threading.

---

## 8. Acceptance criteria (exact commands)

| # | Command | Expected |
|---|---------|----------|
| A1 | `make check` | exit 0 |
| A2 | `go test -race -count=1 ./internal/taskmaster/... ./internal/utuber/... ./cmd/server/ ./internal/platform/config/` | `ok` for every package, no `FAIL`, no goleak report |
| A3 | `test ! -e internal/utuber/jobs && echo gone` | `gone` |
| A4 | `grep -rn "utuber/jobs" --include='*.go' . \| wc -l` | `0` |
| A5 | `grep -rni utuber internal/taskmaster \| wc -l` | `0` (R4, Q5) |
| A6 | `grep -n "metricsIncludeHiddenLanes" internal/taskmaster/coordinator/*.go \| wc -l` | `≥ 2` (the const and its use), and `go test -run TestMetrics_HiddenLaneExcludedByPolicy ./internal/taskmaster/coordinator/` → `ok` |
| A7 | `grep -rn "sh\", \"-c\"\|/bin/sh" internal/utuber --include='*.go' \| grep -v _test.go \| wc -l` | `0` (D1: no shell in utuber production code) |
| A8 | `go test -race -count=20 -run 'TestCancelExecution_\|TestFuncTask_BrakeVsPendingCancel' ./internal/taskmaster/worker/` | `ok` (race protocol stable under repetition) |
| A9 | `go test -race -count=1 -run 'TestProgressReporter_' ./internal/taskmaster/worker/` | `ok` (Q3 floor; deterministic clock) |
| A10 | `node scripts/check-shared-barrel.mjs && node scripts/check-shared-css.mjs` | both print their PASS lines, exit 0 |
| A11 | After the phase commit: `npm run build && git status --porcelain web/shared/dist web/taskmaster/js/bundle.js web/utuber/js/bundle.js` | empty output (the committed artifacts equal a fresh build) |
| A12 | E1–E3 checklists (§7.3) | **Signed off by the owner**, who needs a browser, network access and yt-dlp. The autonomous executor cannot run these; it records A12 as `PENDING — owner sign-off` in its completion report, and it must not mark the plan complete on its own authority. |

---

## 9. Pre-mortem (deliberate mode)

1. **"utuber shows 503 on half the boots."** The cause would be build-order dependence: utuber built before taskmaster, or a second engine opened on the same DB by a second path. That produces two workers racing on the same lanes, with locks preventing double runs but width being exceeded. Prevention: the single memoised `moduleDeps.taskmasterEngine()`, the explicit error path, `TestDispatcher_TaskmasterAndUtuberShareOneEngine` looped 5×, the headless test, and `len(closers)==1`.
2. **"A canceled download started anyway" or "Remove raced the worker."** The cause would be a TOCTOU between the HTTP cancel/remove and `poll`→`runTask`. Prevention: registration happens before the claim (`worker.go:185`); pending→canceled and pending→running are single conditional UPDATEs on one connection; `CancelExecution` is DB-first (the pending→canceled UPDATE, then the registry cancel); `runFunc` checks `execCtx.Err()` right after the claim and never calls Run on a dead context; the brake path finishes func rows only when they are still pending; the four race tests run at `-count=20` (A8); `RemoveIdleTask` checks for a running execution inside its transaction, so a claim affects 0 rows after a delete.
3. **"The server hangs at shutdown or tests hang under `-race`."** The cause would be a deadlock from a `(*DB)` call inside `withTx` (R1), the progress reporter writing while the pruner holds a tx, or yt-dlp grandchildren holding pipes. Prevention: R1 plus code review of every `withTx` body; the pruner tx has no callbacks; the reporter never runs inside a tx; `OSExecutor` uses Setpgid, group kill and `WaitDelay` 10 s (P15); goleak in taskmaster, cmd/server and (new) utuber; `TestOSExecutorCancelKillsGroup`.
4. **"The taskmaster board regressed after D10."** The cause would be lost drag/drop, pid controls or empty notes in the extraction. Prevention: the adapter preserves legacy classes and `decorate`; E1 checklist; baseline screenshots; `patchList` behaviour unchanged except for the attribute name (unit tested).
5. **"The Raspberry Pi pegs its CPU while downloading."** The cause would be progress events triggering a full board refetch every interval for every open TM tab, or a DB write per yt-dlp line. Prevention: the throttle (≤ 1 write per interval per execution); `task-progress` is patched locally in `board.ts` without a refetch; hidden lanes publish nothing.

---

## 10. Risks and mitigations

| ID | Risk | Mitigation |
|----|------|-----------|
| K1 | Existing shell semantics drift through the SQL/scan changes | R3; the existing suite runs unmodified; the pins `TestGetEligibleTasks_ShellCanceledQuirkUnchanged` and `…FuncTaskOnlyWithPending` |
| K2 | Board's global `listExecutions(undefined, 300)` (`board.ts:122`) is crowded out by many visible utuber executions | Retention bounds the growth; the hidden lane is excluded at the SQL level. Follow-up: per-lane execution fetch |
| K3 | Payload exposed via `GET /api/tasks` (URLs) | Same trust boundary as the TM module itself (it is RCE by design, `docs/taskmaster.md` Security). Hidden lanes hide it. |
| K4 | A migration 5 failure on an existing on-disk DB | Transactional, with a rollback test; the error surfaces at `Open` → 503 for TM/utuber only; other modules are unaffected (`main.go:320-323`) |
| K5 | yt-dlp survives a hard crash (a separate process group) | Accepted. It is the same class as a shell task's orphan. The reconcile marks the job failed; the orphan finishes or dies on its own. Documented. |
| K6 | Partial `<stem>.*` files left after cancel or failure | Documented. The stem is `<jobID>-<execID>` (§4.10), so a re-run writes to a **new** stem, and it neither resumes nor collides with the previous attempt's partial files, which remain in `download_dir` as litter. Follow-up: best-effort removal of `<jobID>-<execID>.*` on cancel or failure. |
| K7 | `settings.json` partial apply (lane ok, python_bin write fails) | Validate-all-first. The 500 message says "partially saved". Documented (§4.10). |
| K9 | **Downgrade hazard.** An older binary opened on a v5 DB ignores the new columns, which SQLite permits. Its eligibility SQL (`db.go:534-538`) treats every pending func execution as eligible and runs it through `TaskExecutor.Execute` with an empty command, and that returns `task has no command` (`worker/executor.go:41-43`). So every queued download is consumed as **`failed`**, not `success`. Hidden lanes also reappear, and the owned-lane guards vanish. | Documented as **"no downgrade past schema v5"** in `docs/taskmaster.md` (Phase 6) and in the Phase 1 commit message. Recovery after an accidental downgrade: upgrade again, then re-run the failed jobs from utuber (they are `failed`, so P18 allows it). There is no code guard, since an old binary cannot be changed retroactively. |
| K8 | Late registration: TM boots with pending utuber jobs, and utuber fails to build | Jobs stay pending (visible in TM when not hidden). A once-per-kind log line (§7.4) points at the missing kind. |

---

## 11. ADR

- **Decision.** Implement utuber downloads as one-shot **Go-function tasks** on a TM-owned lane (Option A). The engine is shared in-process through an explicit `golane.Host` interface injected by `cmd/server` (A1). Payloads are versioned JSON on the task row. Progress is kept unthrottled in memory and persisted and streamed at most once per `progress_interval_ms` (default 2000, clamped to ≤ 2000). Retention, visibility and width are lane columns. One shared `QueuePanel` in `web/shared` is adopted by taskmaster and then utuber. `internal/utuber/jobs` is deleted. Cancel is DB-first. When taskmaster is not routed, the engine runs headless and schedules owned lanes only (P16). Func-task metrics aggregate per (lane, kind) (P17). Config lanes never touch owned lanes (P19).
- **Drivers.** Minimal risk to TM's per-task lock/eligibility scheduler; restart safety and leak-free shutdown under goleak/`-race`; order-independent module wiring; FRD D1–D10; owner decisions Q3–Q5.
- **Alternatives considered.**
  - B, execution-per-job on one durable task: rejected because it requires rewriting per-task locking and pending reuse (`worker.go:144-167`).
  - C, utuber keeps its own worker with TM storage: invalid under FR-U1/D1.
  - D, HTTP loopback: invalid under D1.
  - A2, global init-time registry: rejected for test isolation and hidden coupling.
  - For progress, throttling in-memory updates too was rejected, because it would be slower than today (Q3 floor).
- **Why chosen.** A reuses width, pause, brake, cancel, reconcile, heartbeat and metrics unchanged for the new task kind. The only scheduler change is a single eligibility clause scoped to `kind != ''`. Explicit injection makes build order irrelevant and keeps tests hermetic.
- **Consequences.**
  - One task row per download, bounded by retention.
  - TM's API gains additive fields, one route and owned-lane guards (N1–N4).
  - utuber's wire format and statuses change (N5, P6), and `utuber.workers` becomes a seed (N7).
  - Queued downloads survive restarts, and in-flight ones become `failed` and can be re-run.
  - Per-execution SIGSTOP pause does not exist for Go tasks (P5).
  - The utuber page still polls every 2 s.
- **Follow-ups.**
  1. **Metrics reversal (Q4):** set `metricsIncludeHiddenLanes = true` in `internal/taskmaster/coordinator/handlers_metrics.go`, invert `TestMetrics_HiddenLaneExcludedByPolicy`, and update `docs/taskmaster.md`. No schema or API change is needed.
  2. **A second module lane (Q5):** call `host.RegisterLane(golane.LaneSpec{…}, golane.NewKind[…](…))` from that module's `Build`, and add a `moduleDeps.taskmasterEngine()` call in its `buildModule` case. No TM change is needed. Do not generalise further until that consumer exists.
  3. A shell-task progress reporter (a stdout marker line) to complete D3 for shell tasks (P12).
  4. Fix or document the shell canceled-eligibility quirk (P3), as its own change with an owner decision.
  5. Payload upgrade functions when a Kind first bumps its `Version`.
  6. Per-lane execution fetch on the TM board (K2).
  7. Cleanup of partial downloads on cancel (K6).
  8. `seedLanes` overwriting a UI-edited width at every boot (fact 6): an owner decision on whether config or DB should win.
  9. If the owner wants hidden lanes fully untouched by the hand brake (G3), exempt `owner != ''` lanes in `handleEngageBrake` (`handlers_control.go:121-136`). That is a brake-semantics change.
  10. A shell-task "run while taskmaster unrouted" mode, if ever wanted, would be an explicit config flag. P16 deliberately keeps today's "unrouted means nothing runs".

---

## 12. Open questions carried forward
(Not written to `.omc/plans/open-questions.md`, because this planning pass was restricted to this file. The orchestrator should append them there.)
- [ ] P9 default "Show in taskmaster" = shown. The owner may prefer hidden by default.
- [ ] P12: is deferring the shell progress reporter acceptable, or is D3 "any task" required in this change?
- [ ] Fact 6 / follow-up 8: should configured lane widths override UI edits at every boot (today's code) or seed once (today's doc)?
- [ ] Follow-up 4: fix the shell canceled-eligibility quirk?

---

## Changelog (v1 → v2)

This revision synthesises round 1 (Architect SOUND-WITH-CHANGES, Critic ITERATE). Every point below was re-verified against the code before writing.

**Blocking fixes**
1. **Cancel protocol is now DB-first** (§4.4 `cancelexec.go`). v1 was wrong: `runTask` registers the cancel func (`worker.go:185`) before the claim, and `CancelRegistry.Cancel` returns true once an ID is registered (`cancel.go:43-55`). So a still-pending job would be reported "canceling", left pending, claimed, and then run on a dead context. The order is now (a) the conditional pending→canceled UPDATE plus a registry cancel, (b) the registry cancel, (c) not_running.
   - `runFunc` gains step 3b: after the claim, it checks `execCtx.Err()` and never calls Run on a dead context.
   - `cancelStepHook` and `TestCancelExecution_ClaimWinsRace` are removed. `afterFuncClaimHook` is added.
   - The race tests are rewritten as four direct-`runTask` internal tests: pending-cancel-before-claim, claim-then-cancel, canceled-before-registration, and brake-vs-pending-cancel.
2. **The unregistered-kind filter moves to candidate building** (`worker.go:134-139`), before `toRun` (`:141`), so it no longer burns slots.
3. **Config and owned lane collision** is resolved by P19. `seedLanes` skips owned lanes, and `EnsureOwnedLane` names the conflict and the fix. Tests: `TestSeedLanes_SkipsOwnedLane` and `TestRegisterLane_ConfigLaneCollisionError`.
4. **Metrics blow-up** is resolved by P17: func tasks aggregate per (lane, kind), `MetricSummary.kind` is added, the `metrics.ts:80` key is fixed, and `TestGetMetrics_FuncTasksAggregatedByKind` pins it.
5. **Guard gaps:** task pause and resume (`handlers_tasks.go:152,165`) now return 409 for func tasks. `funcTaskGuard` is the single helper, and `TestOwnedLaneGuards` covers every row.
6. **The progress test is deterministic.** `progressReporter.now` gives an injected clock, and the exact write count is 5. A label change persists immediately (`TestProgressReporter_LabelChangePersistsImmediately`).
7. **Brake double-finish:** the func brake path uses the conditional `CancelPendingFuncExecution`, so there is exactly one metric. Test: `TestFuncTask_BrakeVsPendingCancel_OneMetric`.
8. **Barrel:** `ExecStatus`, `statusSymbol` and `effectiveStatus` all move and are all exported. The exact lists are 4 values and 7 types, and the citation is fixed to `check-shared-barrel.mjs:37-38`. Why deep imports are not an option: `build-web.mjs:82` externalises `@shared/*` to the barrel bundle.
9. **Headless engine** is resolved by P16, `OpenOptions{OwnedLanesOnly}`. The shell worker never runs when taskmaster is unrouted, and `TestOpen_OwnedLanesOnlySkipsShellLanes` pins it.

**Non-blocking fixes**
- The FK test is replaced by `TestDSNFor_SetsWALAndForeignKeys`, using the extracted `dsnFor`. The v1 behavioural test would already pass via the pool PRAGMA at `db.go:30`.
- Downgrade hazard: K9, plus the Phase 6 "no downgrade past schema v5" warning.
- The `UpdateTask` allowlist runs before the `updated_at` injection (`db.go:459`).
- `UpdateTaskLabel` is added to §4.2. `SetLabel`, `Progress` and `Log` are all no-ops after Run returns.
- §4.12 adds `internal/utuber/build_test.go:156`.
- The hook lifecycle rule is added to R6.
- The dispatcher share test uses a fresh `TempDir` and a `Close` per iteration.
- The download stem is `<jobID>-<execID>`.
- TM rerun of a successful func job is refused (P18, `golane.ErrSucceeded`).
- N1 is labelled as added scope.
- P12, P5, the brake-while-hidden case and P9 are moved into §2.5 as owner-visible narrowings G1–G4. Utuber now surfaces `queue_paused_by`, so a braked lane explains itself.
- A12 is owner sign-off, and the executor records it as pending.
- The Architect synthesis (hide func tasks from `/api/tasks`) is evaluated and **not adopted** (P20, with evidence).

**Points where the evidence differs from the review**
- K9 downgrade: the review said an older binary would record pending func tasks as **"success"**. The evidence says **"failed"**. `TaskExecutor.Execute` returns `errors.New("task has no command")` for an empty command (`internal/taskmaster/worker/executor.go:41-43`), and `runTask` records any non-nil error as `failed` (`worker.go:325-336`). The hazard is still real (queued downloads get consumed), so it is documented as such.
- The Architect synthesis (P20) is declined: func task names remain discoverable via `GET /api/executions` `task_name` (`db.go:637-650`), so the per-name guards cannot be removed, and the board builds lanes from `listTasks` (`board.ts:116-131`).

---

## Changelog (v2 → v3, post-consensus improvements)

Consensus was reached in round 2 (Architect SOUND-WITH-CHANGES, Critic APPROVE). These accepted improvements were applied and verified against the code.

1. **`runFunc` step 2a (pre-claim shutdown check).** On a worker shutdown (context dead, not `WasCanceled`), `runFunc` releases the lock and returns, leaving the row `pending`, so queued jobs survive a restart (FR-U2). New test: `TestFuncTask_ShutdownBeforeClaimStaysPending`.
2. **Step 3b also covers the brake.** If `brake.Engaged()` after the claim, the job finishes `canceled` without calling Run. This closes the window between `worker.go:201` and the claim, which the brake sweep (`handlers_control.go:142-149`) cannot see. New test: `TestFuncTask_BrakeAfterPreCheck_CanceledWithoutRun`.
3. **`CancelExecution` path (a)** resolves the lane via `ExecutionLane` and the task name via `GetExecution`, outside any tx, and publishes through the hidden-lane filter.
4. **Metrics:** `GetMetricsExcludingLanes` is the single P17 implementation, and it uses `ORDER BY t.lane_name, task_name` (the alias). The test now asserts that the hidden func lane is absent under kind aggregation and covers the task-detail path. The task-detail behaviour (`taskdetail.ts:168,174` falls back to `rows[0]`) is documented, with no code change.
5. **Race-test spec:** the task is loaded via `GetTask` after `SubmitFuncTask`. `BrakeVsPendingCancel` subscribes to the broker before acting.
6. **`brake_engaged`** is exposed in `LaneSettings` and utuber's `/settings.json`, and the utuber panel explains a persisted brake, including in headless mode.
7. **K6** is corrected for the `<jobID>-<execID>` stem.
8. **§4.9:** an explicit `persistedOnce` flag replaces the "lastPersist is zero" reasoning.

