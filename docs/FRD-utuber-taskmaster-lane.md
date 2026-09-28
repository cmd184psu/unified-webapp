# Mini-FRD: utuber on a taskmaster lane

Status: **draft, not yet planned** (decisions recorded 2026-09-26)
Scope: `internal/utuber`, `web/utuber`, `internal/taskmaster`, `web/taskmaster`, `web/shared`

## 1. Goal

utuber stops running its own download queue and becomes a thin front end
that submits one-shot jobs to a dedicated **utuber lane** in taskmaster.
Taskmaster gains the ability to run registered **Go functions** (not only
shell commands) and to show an optional **progress bar**. Both modules
render their queues with one **shared queue/lane panel**.


Why: this is one binary by design (DRY, consistency, fewer bugs, lower
CPU/memory on small hardware). A second queue engine is duplication, and
the queue features utuber is missing are the ones taskmaster already has:
state that survives restarts, re-running failed work, cancellation, and
concurrency limits.

This is expected to be **destructive** to utuber's current queue code
(`internal/utuber/jobs`). That is accepted.

## 2. Decisions

| # | Decision |
|---|----------|
| D1 | **Go-function tasks, not shell.** Taskmaster runs registered Go callbacks for lanes that opt in, restoring taskmaster's original intent (in-process calls). utuber registers its download/transcode function with taskmaster, **in-process through a Go interface, not over HTTP**. User input (URL, titles) never reaches a shell. |
| D2 | **No sudo for the utuber lane.** Go-function tasks never use taskmaster's sudo gate. Updating yt-dlp remains utuber's own action (its existing Settings button), outside the lane. |
| D3 | **Optional progress for any taskmaster task.** A task can report progress (0–100 plus a label); taskmaster shows a progress bar when a task reports it. Go-function tasks report through the callback's context; utuber reports yt-dlp's progress this way. |
| D4 | **Post-processing (tagging, filing) runs in the utuber callback**, in Go, as part of the job. |
| D5 | **Duplicate detection stays in utuber**, checked before submitting. |
| D6 | **Age-out:** finished utuber jobs are removed after a configurable number of days, set in utuber's ☰ menu. Default **10 days**. |
| D7 | **Concurrency:** utuber's lane width (downloads at once) is configurable in utuber's ☰ menu. The hand brake is not required for utuber. |
| D8 | **No legacy migration.** utuber keeps no history across restarts today; the new lane starts empty. |
| D9 | **utuber controls its visibility in taskmaster.** A utuber setting decides whether taskmaster's UI shows the utuber lane and its functionality at all. Hidden, taskmaster's board and menus show no trace of it; the lane still runs. |
| D10 | **Shared queue/lane panel** in `web/shared`: Running / Up next / Recent sections, status badges, optional progress bar, and cancel / pause / re-run / remove actions, fed by a per-module adapter. Taskmaster adopts it first, then utuber. |

## 3. Requirements

### Taskmaster
- **FR-T1** Register Go-function task kinds by name (e.g. `utuber.download`) with a typed payload; a lane declares which kinds it runs.
- **FR-T2** Run Go-function tasks under the lane's width, pause, cancel (context cancellation), and restart reconcile, like shell tasks.
- **FR-T3** Optional task progress (percent + label), persisted with the execution and streamed to the UI; progress bar shown only when reported.
- **FR-T4** One-shot tasks (Repeat off) with retention: finished executions are pruned after the owning lane's age-out setting.
- **FR-T5** Re-run a finished or failed execution with the same payload.
- **FR-T6** Hide/show a lane and its related UI on request from the owning module (D9).

### utuber
- **FR-U1** Submit downloads to the utuber lane through the in-process interface; delete `internal/utuber/jobs`.
- **FR-U2** Queue survives restarts (via taskmaster's storage).
- **FR-U3** Re-run a failed download; cancel a queued or running one; remove finished ones.
- **FR-U4** ☰ settings: age-out days (default 10), concurrent downloads (lane width), and "show in taskmaster" (D9).
- **FR-U5** Render the queue with the shared panel (D10).

## 4. Open questions (for planning)
1. The exact Go interface between utuber and taskmaster: registration at build time vs. runtime; how the payload is typed and versioned.

That will have be determined by the agent.  I would suggest creating a generic paylaod handler structure in taskmaster (or shared) and inheriting it in utuber.

2. Where Go-task payloads are stored (a JSON column on the task/execution?) and how they are validated on restart.

How about a break out badge that says 'payload' and when you click the badge, you get a modal dialog with pretty-printed json.  The utuber side would have to validate.  Perhaps there's a validate() function abstracted upstream and utuber must implement it.

3. Progress update rate limiting (yt-dlp emits many lines).

The progress update simply has to match what utuber does today.  it does not have to reinvent something new or better, necessarily.

4. What "hidden" means for metrics: are utuber runs excluded from taskmaster's Metrics page too?

Yes, otherwise it would be confusing.  Again, hidden is optional.. so there should be an option of including downloads in the metrics, when not hidden.

5. Whether other modules should later get lanes the same way (a general "module lane" pattern).

Yes, eventually, but not at this time.

## 5. Out of scope
- Moving utuber's yt-dlp update into taskmaster.

Disagree.. there's no reason why this couldn't be thrown at taskmaster as a run-once shell task.  Upgrading yt-dlp would use a generic shell-task lane, could use sudo optionally (if taskmaster currently permits it) and upgrading yt-dlp would be included in the metrics.

- Hand brake integration for the utuber lane (not required; may fall out for free).
- Importing utuber's current in-memory history.

## 6. Interim (done before this FRD is planned)
utuber keeps its own queue for now, with one addition: **queued or finished
jobs can be removed** (`POST /jobs/delete?id=`, with an "are you sure?"
dialog). Running jobs can't be removed until FR-U3's cancellation exists.
