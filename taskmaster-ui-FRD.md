# FRD (WORKING DRAFT): Taskmaster UI & Model Rework

**Status:** Ready for ralplan (2026-09-13). Premise and structure fully hammered
out with the owner; sections are marked **[SETTLED]** / **[OPEN]** (remaining
OPEN items are wording/syntax detail left to ralplan/ralph, not premise-level).
**Relationship to `taskmaster-FRD.md`:** that document specced the *port* of
`continuous-task-runner-queue` into the module and is considered done/settled.
This document reworks the **UI and the user-facing object model** on top of it.
It will feed ralplan → ralph once hammered out. The backend impact is noted
where the new model diverges from the ported one (it is not purely frontend).

---

## 1. Why this exists [SETTLED]

The ported UI is a **CRUD panel over the four DB tables** (groups, tasks,
executions, metrics), each a flat co-equal tab, originally behind a login. That
premise is wrong for this tool ("so bad it's not even wrong"). Symptoms:
- Navigation mirrors the database, not the work.
- The hierarchy the backend supports (group→tasks→executions→output) exists in
  code but is unreachable — no drill-down links.
- Default landing is **Groups** (config), not the work.
- "Run now" (the primary act of a task runner) is buried in a table row.
- Manual **Refresh** buttons everywhere = a no-live-updates hangover.
- Task creation makes you hand-write a **raw JSON** args blob.
- Config gaps: `allowed_types` is enforced by the backend but uneditable in the UI.

We are redesigning the premise, not patching the tabs.

## 2. What taskmaster *is* [SETTLED]

It runs commands — on demand, or repeating after a rest period — through
**width-limited lanes**, and lets you watch output live and review history.
It is **not** an orchestrator (no conditional/branching logic) and **not** a
scheduler (see §4).

## 3. Core object model [SETTLED]

- **Task** — one command. Durable, named, reusable (so repeat / history /
  metrics are meaningful). Runs on demand or repeats on a cooldown. Lives in
  exactly one lane.
- **Lane** — an ordered playlist of tasks with a **width**. One noun replaces
  both the old "group" **and** any separate "sequence" concept:
  - **width 1** → one at a time, in order (the old "sequence" / "single-threaded").
  - **width N** → up to N run concurrently, pulled off the top in order (the
    old "pool" / group).
  - Plays straight through: a task's pass/fail **never** gates the next one.
    No conditionals — "only if the last succeeded" is the user's cue to write a
    script instead.
  - Ordered by **what's ready next**, not a frozen list. A task in cooldown is
    simply "not ready yet," so the lane runs the next *ready* task and slots the
    cooled-off task back in when it becomes due (decision (b) below). A pure
    one-shot sequence still plays top-to-bottom because everything is ready
    immediately; repeats interleave by their timers.

**"Groups" is dead as a term and as a first-class managed entity.** There are
only lanes. A lane's identity is its role (a place tasks run), not a pool-size
dial.

## 4. Scheduling axiom — "We're not cron" [SETTLED]

- **Cooldown is a minimum *rest*, not a cadence.** After a task finishes it must
  cool off *at least* its cooldown before becoming eligible again — but it runs
  when its lane is free and it's its turn, which may be far later. If a task
  doesn't run for 10 minutes because a lane-mate was running, that is correct
  and acceptable.
- **Hard non-goals:** no wall-clock scheduling, no cron expressions, no
  time-of-day, no guaranteed interval/cadence. The only clock that matters is
  *finished-plus-rest*.
- **Wall-clock triggers are out of scope by design.** "Run nightly at 2am" is
  not a taskmaster feature. The seam is the CLI: cron (or anything) calls
  `enqueue` on a task, and taskmaster runs it through its lanes from there.
  `enqueue` is the boundary between the world's clock and our lanes.

## 5. The lane board (home screen) [SETTLED premise, OPEN details]

- The app opens on a **lane board**: vertical lanes (columns), each a playlist
  showing **ran (above) / running (now) / up-next (below)**.
- Repeating tasks appear as up-next entries **with a cooldown timer**
  (e.g. "X — again in 8:00").
- **Drag a task between lanes** to move it; **drag within a lane** to reorder
  (this ordering replaces per-task priority — see §6).
- **"+ add task" sits at the top of each lane** — opens the Task Designer with
  that lane pre-selected, so it's clear where the task is headed (§6).
- **Manually triggering an existing task is "Up next"** (not "Run now"): it puts
  that task at the **front of its lane's up-next**, so it runs as soon as a slot
  frees. Same playlist vocabulary as everything else.
- **Cancel a running task.** A running task has a **stop/cancel** control that
  **force-kills it (SIGKILL / `kill -9`)**. The execution is recorded with a
  distinct **`canceled`** status (not `failed`, so history/metrics can tell an
  operator kill apart from a real failure). **Caveat:** a task running via `sudo`
  is a root-owned child the unprivileged service cannot force-kill — same
  limitation as shutdown (documented in `docs/taskmaster.md`); it may survive the
  cancel.
- **Emergency stop / hand brake.** A single, **prominent, always-visible** global
  control (not buried in the hamburger) that in one action: **pauses every lane**
  (no new tasks start anywhere) **and force-kills every running task under our
  control** (SIGKILL; root-owned `sudo` children excepted, per the caveat above).
  It **latches** — everything stays paused until the operator explicitly
  **releases** it (resume). Killed executions are recorded `canceled`. This is the
  panic button for "a command is doing something I didn't want."
- **Per-lane width** lives on the lane's own header/menu (click the lane, set
  its width). *[OPEN: exact placement.]*
- **Create/delete lane** — tentatively in the hamburger. *[OPEN.]*
- The board should feel **live** (running/next update on their own), which
  largely removes the need for the manual Refresh buttons and possibly the
  bolted-on auto-refresh. *[OPEN: live via SSE vs polling.]*

## 6. Task creation & execution [SETTLED premise, OPEN details]

**There is no task "type" in the UI.** The old selector
(shell/exec/script/migration) is removed. History: the *original* intent was a
curated set of internal Go operations (friendly-labeled) to invoke; those were
never built; `exec` was a time-pressure stopgap; `shell`/`script`/`migration`
were AI-misinterpretations that leaked through. None of that should ever face
the user.

- **The user provides a command line ("a CLI"). One field. That is the task.**
- **Taskmaster interprets and dispatches it.** For **now**, "interpret" means
  simply: **run the command line through a shell** — the shell is the natural
  interpreter of a CLI (pipes, redirects, quoting, globs all work). Deliberately
  **no** clever metacharacter-sniffing / shell-vs-argv heuristic now; dumb and
  predictable over surprising. *[OPEN: confirm shell-execute is the whole of
  "interpret" for now.]*
- **Future (captured, not built):** a curated **registry of internal Go
  operations** with friendly labels — what `type` was originally meant to be.
  The dispatcher then routes a recognized command to its Go function and falls
  back to shell for everything else. Same user mental model ("give a command"),
  no UI change. Deferred with the other future items.
- **The "Task Designer" is the creation surface.** Opened from a **"+ add task"
  button at the top of each lane**, so the destination lane is obvious and
  pre-selected (still changeable in the form). This is the chosen placement over
  a single global "new task" button.
- **Progressive disclosure:** primary = **name + command**. **Advanced** (folded):
  repeat + cooldown, output-file tee, sudo, **workdir/env**. *[SETTLED: keep
  advanced fields including workdir/env.]*
- **Primary action label is "Add to lane"** — not "Run now." It enqueues the
  task into that lane's up-next; it runs when its turn comes (matches the
  playlist model). (Replaces the old "Run now"/enqueue wording.)
- **[SETTLED, new] Export instead of add.** The Designer can **export the
  equivalent command** rather than adding directly, so a task can be scripted/CI'd:
  - a **`taskmasterctl` export** (the CLI invocation that creates/enqueues it), and
  - a **`curl` export** (the raw HTTP equivalent).
  Both reflect the current form state and are **collapsed by default** to save
  space. The `curl` export must carry the `Host` header and, on a protected
  deployment, an API-key placeholder; the `taskmasterctl` export uses its flags.
  Exact syntax is left to ralplan/ralph.
- **[SETTLED] Per-task `priority` is removed** — drag-order within a lane is the
  only ordering. (Backend impact: replace priority-weighted slot selection with
  an explicit persisted per-lane position.)
- Tasks are **durable / named / reusable** (assumed).
- Security posture is unchanged by hiding type: a shell-executed command line is
  the same arbitrary-exec-as-service-user reality already documented; the sudo
  gate still governs. (No-production context — noted once.)

## 7. Sudo [SETTLED — already built this branch]

Runtime `allow_sudo` toggle, DB-persisted (config seeds once, DB authoritative),
shared gate enforced at task-validation and executor. UI exposes it. The task
form shows a sudo control only when enabled. Keep as built.

## 8. Views & navigation [SETTLED premise]

Resolved from the design conversation:
- **Task type** — gone from the UI (§6).
- **Priority** — removed; drag-order only (§6).
- **History** — **no standalone tab.** It is the lane's "ran" section plus each
  task's own drill-in (full run list + live output).
- **Metrics** — per-task metrics live in the task drill-in, **and a global
  metrics overview becomes the Metrics tab**, replacing the old per-task-cards
  metrics tab entirely. It's the cross-task "how's everything doing" view.
- **Naming:** **"lane"** replaces "group" (confirmed). The task action is
  **"Add to lane"** (not "Run now").
- **Task creation** starts from a **"+ add task" at the top of each lane**
  (destination pre-selected) — see §6.
- **Resulting nav:** **lane board (home) + Metrics tab (global) + hamburger.**

Open: nothing premise- or structure-level remains. Detail/wording polish and
export syntax are left for ralplan/ralph.

## 8a. UI conventions [SETTLED]

- **Toggle switches, not checkboxes.** Every binary on/off control renders as a
  toggle switch. **Global UI policy** (all modules, all future work). The
  already-built hamburger (sudo, auto-refresh) currently uses checkboxes and
  must be converted.
- **In-app lightbox/modal dialogs, never native `alert()`/`prompt()`/`confirm()`.**
  Errors, confirmations, and prompts all use styled in-page modals. **Global UI
  policy.** The current UI uses `alert()` (error paths in `tasks.ts`) and
  `confirm()` (deletes in `groups.ts`/`tasks.ts`) — replace in the rework.
- **Live/pause instead of Refresh buttons; conditional, surgical updates.**
  **Global UI policy.** No manual "Refresh" buttons — a **live/pause toggle**,
  with the interval configurable in the hamburger. Each tick must (a) **only
  update if something actually changed** (revision/version check or server
  push), and (b) **patch only what changed** — never repaint the whole
  page/table. Over-aggressive or full-repaint refreshes cause "jitter" and
  ruin usability. The current auto-refresh calls `renderPage()` (full repaint) —
  the exact anti-pattern to remove. This resolves the "reconcile hamburger with
  a live board" open item: the lane board is the primary live view, and
  SSE/push is the natural fit for "only when changed."

## 8b. Future: shared platform UI layer [OUT OF SCOPE — noted]

There is **no** platform-wide UI system today: the legacy trio
(todo/slideshow/menuserver) copy-paste identical CSS three ways, and the modern
modules (admin/taskmaster/obsidianoid/multissh/grocery) are each bespoke with
their own tokens. The four global UI policies above (toggles, modal dialogs,
live/pause + surgical refresh) plus the parked themeable-skin are effectively the
*content* of a future **shared design system** — a real DRY concern (UI being
reinvented ~8x). **That overhaul is out of scope for this branch.** Guardrail for
the taskmaster rework: build the toggle / modal / live-pause pieces as **clean,
self-contained, extractable components** so the future layer can lift them
wholesale rather than rewrite. (Tracked in memory: platform-ui-layer-future.)

## 9. Backend implications (flag for ralplan) [OPEN]

Not purely a frontend change. The new model touches the backend:
- **Rename** group → lane through the API/DB/config (or map lane→existing group
  internally). Width = existing `pool_limit`.
- **Ordering:** replace `priority` with a persisted per-lane task **position**
  (drag-order); the worker's "fill free slots in order" logic keys off position
  + readiness instead of priority.
- The "ready-next / cooldown-as-rest" behavior largely matches the ported
  eligibility model already (5s poll, becomes-eligible-after-cooldown) — confirm
  no cron-like assumptions crept in.
- **Per-execution cancel + global hand brake (new).** Each running execution
  needs its **own cancelable context** and a registry mapping `execution_id →
  cancel func`, so a single task can be force-killed on demand
  (`cancel()` → `CommandContext` SIGKILL + `WaitDelay`, the machinery that
  already exists for shutdown). New endpoints: cancel one execution; and an
  **emergency-stop** op that **pauses all lanes + cancels every running
  execution** atomically, plus a **release** to resume. Killed executions are
  written with a distinct **`canceled`** status (new terminal state alongside
  `success`/`failed`). Root-owned `sudo` children remain unkillable by the
  unprivileged service (documented caveat). The hand-brake latched state is
  **durably persisted in the DB** (like allow_sudo) and **survives an unclean /
  violent reboot** — on boot the worker starts in the braked (all-paused) state
  and will not launch anything until the operator explicitly releases it. A
  restart never silently un-pauses.
- **Executor rewritten clean.** No longer bound by `alfredo`'s execute
  structure/methodology (that constraint is gone). Collapse the four
  type-branches + JSON `args` into a **single path**: stored command string →
  `exec.CommandContext(ctx, "/bin/sh", "-c", cmd)`. Stdlib only (`os`,
  `os/exec`, `filepath` — no `alfredo`). **Keep** the port's lifecycle
  correctness, which is unrelated to alfredo and already right: `CommandContext`
  + 10s `WaitDelay` + context-cancel-on-shutdown + goleak-clean `Close()`, plus
  the line-by-line stdout/stderr streaming into the output registry/SSE. Sudo
  stays gated (`sudo sh -c "…"` when open). Drop `task_type`, the `args` JSON
  schema, and the `migration`/`exec`/`script`/`shell` distinctions from
  models/DB/executor.

## 10. Process [SETTLED]

FRD (this doc) → **ralplan** (produces a plan like `taskmaster-plan.md`) →
**ralph** (loops to correct the existing code).

**Keep vs rewrite — decided:**
- **Rewrite `web/taskmaster/` (the frontend) from scratch.** The premise change
  (CRUD tabs → lane board) is total; the existing TS/CSS/HTML is not worth
  salvaging. The four UI edicts (§8a) and extractable-component guardrail (§8b)
  govern the rewrite.
- **Keep the Go backend** (the port: coordinator/worker/db/models, the clean
  lifecycle, SSE, metrics) and the lane/sudo backend work done on this branch,
  **modified** per §9: group→lane rename, priority→persisted position, single
  shell exec path (no type/JSON/alfredo), per-execution cancel + hand brake +
  `canceled` status, durable brake flag.

**Status: ready for ralplan.**
