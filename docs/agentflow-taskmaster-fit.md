# Agentflow → Taskmaster Lane Fit Investigation

> **Status (2026-10-05):** point-in-time investigation, body preserved as written. The
> module has since grown into a full orchestrator (own worker engine, FRD/plan/queue
> pipeline) per `docs/agentflow-design.md`, which is the current source of truth. The
> conclusion below stands: agentflow keeps its own orchestration state and does not run
> on taskmaster lanes.

**Task:** T020 — Investigate fitting agentflow runs onto taskmaster lanes without changing any behavior.

---

## 1. What Maps Cleanly

### 1.1 Core Queue Operations — Direct golane.Lane mapping

Agentflow's task queue is a simple JSON-file CRUD backed by an HTTP handler (`internal/agentflow/handler.go`). Taskmaster's `golane.Lane` interface (`internal/taskmaster/golane/golane.go:149-165`) provides exactly the right operations. Every agentflow workflow step maps directly to one or more lane operations.

**Subtask → `lane.Submit(kind, label, payload)`**

| Agentflow (handler.go) | Taskmaster (golane.go / lane.go) |
|---|---|
| `flipTaskStatus(id, action)` reads/writes `queue.json` to set status (`approve`, `repair`, `fail`) | `lane.Submit()` validates payload against a kind's Decode, inserts a func task + pending execution into the DB |
| `runNext()` finds first `"ready"` task, flips to `"in-progress"` | No direct equivalent — see §3.1 below |

**Mapping:** An agentflow task becomes a func-task submitted via `lane.Submit()`. The kind would be something like `agentflow.run` with a payload containing the original QueueTask fields (id, title, status, attempts) plus an orchestration context. This is clean — the submission path already exists for utuber's `utuber.download` kind (`internal/utuber/build.go:72`).

**List → `lane.List()`** and **Get → `lane.Get(id)`** map 1:1. Agentflow's `GET /queue` reads all tasks; taskmaster's `lane.List()` returns `[]golane.Job` ordered by position ASC (FIFO) — exactly what the agentflow frontend expects.

### 1.2 Progress Streaming

Agentflow has a `GET /progress` endpoint (`handler.go:128-132`) that computes counts from queue.json. Taskmaster's progress infrastructure is more capable:

| Agentflow | Taskmaster |
|---|---|
| Server-side tally of task statuses on each GET | Per-execution `ProgressRegistry` with pct/label streaming via SSE (`internal/taskmaster/worker/progress.go`) |
| No real-time updates | Real-time broadcasts via BoardEvent broker when status changes (`worker.go:430-431`, `lane.go:68,188,202,257,260`) |

The agentflow progress panel could ride taskmaster's SSE transport (`golane.Lane.StreamOutput` at `golane.go:159-165`, implemented in `lane.go:280-293`). The shared `SSECap` ensures process-wide subscriber limits are respected (`engine.go:146-153`).

File-level evidence:
- `handler.go:77-96` — `flipTaskStatus`: read queue.json, mutate, write back (no concurrency guard)
- `handler.go:99-118` — `runNext`: same pattern, find ready → in-progress
- `lane.go:33-69` — `Submit`: validates payload against kind's Decode, serial DB insert, 3x retry on name collision (P1)
- `worker/progress.go` — per-execution progress with configurable interval (`FuncOptions.ProgressInterval`, set at `engine.go:162-164` or defaulted to 2000ms)

### 1.3 Cancellation

Agentflow's `POST /tasks/{id}/fail` sets status to `"failed"` (a terminal state). Taskmaster has two cancellation paths:
- Per-execution cancel via the shared `CancelRegistry` → writes "canceled" status (`worker/cancelexec.go`, referenced in `lane.go:164`)
- Hand-brake cancel sweeps all running executions (`worker.go:283-319`)

File-level evidence:
- `lane.go:149-165` — `Cancel()` checks if task has executions; calls `worker.CancelExecution()` which writes board event "task-finished" with status "canceled"
- `worker.go:410-425` — execution finish path: `cancels.WasCanceled()` determines whether status is "canceled" vs "failed"

### 1.4 Module coexistence — Engine sharing works correctly

From `main.go`:
- `agentflow` builds independently via `agentflow.Build(cfg.Agentflow)` at line 467 — no dependency on taskmaster
- `utuber` explicitly shares the engine: `e, err := deps.taskmasterEngine()` then `utuber.Build(cfg.Utuber, e)` (lines 499-503)
- The shared engine is opened exactly once per dispatcher via memoisation (`moduleDeps.tmTried` at `engine.go:450-462`)

The critical path for agentflow using taskmaster lanes already exists structurally: a second call to `deps.taskmasterEngine()` + `RegisterLane()` would work. File-level evidence:
- `main.go:431-442` — `moduleDeps` memoises the single Engine per dispatcher build
- `main.go:450-462` — `taskmasterEngine()`: opens once, memoises result, appends to closers on success

---

## 2. What Would Duplicate

### 2.1 Verification Gates (approve / repair / fail)

Agentflow's workflow has three distinct terminal gates:
- `approve` → marks task `"passed"` (`handler.go:89`)
- `repair` → marks task `"repair"` (`handler.go:89`)
- `fail` → marks task `"failed"` (`handler.go:89`)

Taskmaster's terminal states are: `"pending"`, `"running"`, `"succeeded"`, `"failed"`, `"canceled"`. There is no concept of a review/repair gate. The lane's worker polls for eligible tasks (pending) and runs them automatically — it does not wait for human approval between execution steps.

If agentflow were to use taskmaster lanes, each agentflow step would need its own task in the queue, plus the "approve/repair/fail" logic would need to live outside taskmaster (back in an orchestration layer) — so you'd still have **both** a taskmaster scheduler and an agentflow orchestrator, with the orchestrator driving task submission and status transitions. This creates a dual-state system where:
- Taskmaster tracks execution state (`pending` → `running` → `succeeded/failed`)
- Agentflow tracks orchestration state (`ready` → `in-progress` → `review|repair→passed|failed`)

File-level evidence:
- `handler.go:77-96`: `flipTaskStatus` writes the action string directly as the new status — there is no guard against arbitrary transitions (e.g., `"ready"` can jump to `"failed"` without going through `"in-progress"` first)
- `worker/funcs.go:38-45`: `runFunc()` in taskmaster has no approval step — once a kind is registered and a task is pending, the worker polls and starts it immediately

### 2.2 Model Review Flow

Agentflow's `"review"` status represents a model evaluation pass (the code-review cycle described in the wider agentflow specification). Taskmaster has no concept of review — its execution path is raw: submit → poll claims → run → finish. There is no "pause for review" state.

### 2.3 RALPH Completion

The RALPH completion check is an agentflow-specific decision gate that runs after an agent completes work and before the orchestrator advances to the next step. Taskmaster's execution model is blind to application-level business logic — it just runs `Kind.Run()` callbacks and returns success/failure. Implementing RALPH would require a custom kind with a Run function that embeds the RALPH logic, **or** an external orchestrator layer between agentflow and taskmaster. Either way, you're adding duplication: either embedded in the kind (mixing concerns) or a separate orchestrator wrapping the lane operations (adding indirection without eliminating the underlying duality).

### 2.4 State File Semantics Divergence

Agentflow's queue.json has simple status strings (`ready`, `in-progress`, `review`, `repair`, `failed`, `passed`, `blocked`). Taskmaster's task table stores `status` as one of three execution-phase values plus `position` for ordering within a lane. The conversion happens in `lane.go:99-147` (the `toJob` helper), which maps DB `"pending"` → golane `"queued"` and passes through other statuses verbatim — but the mapping is incomplete because taskmaster has no `"ready"`, `"review"`, `"repair"`, or `"passed"` values.

File-level evidence:
- `store.go:40-45`: QueueTask status field holds agentflow-specific values
- `lane.go:119-136`: toJob maps `"pending"` → `"queued"` and otherwise passes through — but there is no mapping for agentflow's richer state machine

---

## 3. What Would Break

### 3.1 State Ownership — Dual Source of Truth

The critical structural mismatch is this: **agentflow owns the queue.json lifecycle, and taskmaster would own the lane execution lifecycle.** These are two separate persistence layers. If an agentflow task were submitted as a taskmaster func task to complete work, then:

1. Agentflow's `"passed"` (from an approve action) does not exist on the taskmaster side — it records `"succeeded"`. The frontend would receive the wrong status.
   - File evidence: `lane.go:99-147` — `toJob()` maps DB `"pending"` → golane `"queued"`, but there is no code that translates taskmaster `"succeeded"` back to agentflow `"passed"`.

2. Agentflow's `"blocked"` status has no taskmaster equivalent — a task cannot be manually set to "blocked" in the lane model; tasks are auto-scheduled when eligible, or explicitly canceled.

3. The `runNext()` operation — "find first ready task and flip to in-progress" — has no direct analog. Taskmaster's worker polls every 5 seconds (`pollInterval = 5 * time.Second` at `worker.go:26`) and auto-claims pending tasks. There is no manual run-next operation; the worker is autonomous. File evidence: `store.go:146-171`: RunNext reads queue.json, finds first `"ready"` task, flips to `"in-progress"`, writes back. Taskmaster has none of this — its claim path is at `worker.go:210` where `poll()` calls `db.AcquireLock(task.ID, w.workerID, lockTTL)` and then spawns `runTask`.

**Conclusion:** This is not a "map the operations" problem — it is a **two different systems** problem. Agentflow is an interactive orchestration tool with human-in-the-loop gates; taskmaster is an autonomous execution engine with polling workers. Making them talk at the lane level without changes would produce conflicting state views.

### 3.2 OwnedLanesOnly Interplay

From `engine.go:87-91` and `engine.go:219-224`:
- When `OwnedLanesOnly: true`, taskmaster skips `seedLanes()` (config lanes) and returns `nil` from its own `Handler()`. The coordinator routes (`/api/queue`, `/api/board/events`, etc.) are never mounted.
- The agentflow module builds completely independently at `main.go:467` — it has no reference to taskmaster whatsoever, so enabling OwnedLanesOnly for agentflow would not affect agentflow directly.

However, if agentflow were to use a lane on the shared engine:
- When `taskmaster` is **not** routed (OwnedLanesOnly = true), the coordinator's routes are nil (`engine.go:219-224`). A lane handle from `RegisterLane()` still works because it only touches the DB and worker — not HTTP routes. So **lane operations would work** but the taskmaster WebUI would be unavailable (which is the intent).
- The agentflow's SSE streaming (`lane.StreamOutput` at `lane.go:280-293`) draws on `l.engine.sseCap`, which is always available regardless of OwnedLanesOnly. So progress streaming works in both modes.

File-level evidence:
- `engine.go:455-456`: `Opts.OwnedLanesOnly` is set based on whether "taskmaster" is routed: `!moduleIsRouted(d.cfg.Routing, "taskmaster")` — this means utuber (which runs alongside agentflow conceptually) also gets OwnedLanesOnly when taskmaster isn't mounted
- `lane.go:219-224`: When !OwnedLanesOnly, coordinator routes + static dir are mounted. Otherwise `e.router` stays nil

**Conclusion:** OwnedLanesOnly itself does not block the integration; it only disables taskmaster's HTTP surface while keeping lane operations functional. The real blocker is the conceptual fit — see §3.1.

### 3.3 Cancel Semantics Mismatch

Agentflow has three terminal status changes:
- `"approve"` → `"passed"` (positive completion)
- `"repair"` → `"repair"` (return for rework, non-terminal in practice because repair loops back to ready)
- `"fail"` → `"failed"` (negative completion)

Taskmaster's cancel (`lane.go:149-165`) writes status `"canceled"`, but agentflow has no `"canceled"` state. There is also taskmaster's hand-brake cancel which sweeps all running executions (`worker.go:283-319`), and a per-execution explicit cancel via `CancelRegistry`. If the hand brake engages during an agentflow execution, taskmaster records it as `"canceled"` but agentflow expects one of its seven statuses — creating a status value that neither the frontend nor the backend recognizes.

File-level evidence:
- `worker.go:413-425`: `w.cancels.WasCanceled(execID)` → `"canceled"`, else `"failed"`
- `golane/golane.go:118`: Job.Status documented as `"queued | running | success | failed | canceled"` — `"success"` not `"succeeded"` (note the verb form difference)
- `lane.go:134`: `j.Status = e.Status` — the DB status is passed through verbatim; no translation happens

### 3.4 Worker Auto-Scheduling vs Manual Run-Next

Agentflow's `runNext()` (`store.go:146-171`) is a **manual trigger**: an operator clicks "run next" and the orchestrator advances one task. In contrast, taskmaster's worker (`worker.go:127-141`, polling every 5 seconds at line 26) is **autonomous**: it continuously polls for eligible tasks and runs them concurrently up to the lane width.

Making agentflow submit to a taskmaster lane would mean:
1. Task submission → taskmaster queue (ok)
2. Agent clicks "run next" → no equivalent action; the worker will pick it up on the next poll cycle automatically
3. Agent clicks "approve" → writes `"passed"` to agentflow's own record, but taskmaster already marked it `"succeeded"` or `"failed"` — they drift apart

The `flipTaskStatus` function at `handler.go:77-96` operates on a separate DB (`queue.json`) from any taskmaster lane. If tasks lived only in taskmaster, agentflow would need additional lane-based operations (e.g., a custom kind with an "approve" action) to stay in sync — which is an implementation change, and the task specifies "without changing any behavior."

---

## Recommendation

**Do not fit agentflow onto taskmaster lanes at this time.** The two systems solve different problems:

- **Agentflow** is a multi-step orchestration tool: it manages dependencies between tasks, runs human-in-the-loop gates (review, repair), and tracks an agent's progress through a complex workflow. It is synchronous, interactive, and stateful in ways that taskmaster does not model.
- **Taskmaster** is a queue engine: it receives discrete units of work and executes them autonomously via polling workers with bounded concurrency. It has no concept of approval gates, repair loops, or orchestration ordering beyond FIFO within a lane.

Forcing agentflow into taskmaster lanes would require:
1. A custom `agentflow.run` kind whose Run function wraps an external command or Go callback — but then agentflow's own status transitions (approve/repair/fail) become disconnected from taskmaster's execution states
2. An additional orchestration layer to bridge the two persistence models (queue.json ↔ SQLite lane table) — which duplicates what the existing `handler.go:flipTaskStatus` and `store.go:RunNext` already do, just with more indirection
3. Resolution of status name conflicts (`passed` vs `succeeded`, absent `blocked`/`review`/`repair`) requiring code changes on both sides

The clean path forward would be a design-phase study of whether agentflow and taskmaster should remain separate (each solving its own layer) or whether one should absorb the other, rather than bolting them together at the lane submission level where they do not naturally fit.
