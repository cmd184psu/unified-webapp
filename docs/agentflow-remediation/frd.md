# Agentflow remediation: execution engine, state, safety, and multi-project orchestration

> Generated from `frd.json` — edit the JSON, not this file.

**ID:** FRD-AF-REM-001 · **Status:** approved · **Created:** 2026-10-12 · **Approved:** 2026-10-12

## Objective

Replace agentflow's broken execution half with a safe, deterministic orchestrator that emulates oh-my-claudecode's ralph (verify + reviewer gates, persistent repair loop, stuck detection, progress log) and ralplan (planner -> architect -> critic consensus planning), runs every model stage on taskmaster lanes across many model backends at once, keeps all state in SQLite outside the project repos, manages multiple projects, and never lets a model execute model-authored scripts, write git state, or reach credentials.

## Context

A code review of the uncommitted agentflow module on the `agentflow` branch produced 31 findings (see `findings`). The FRD/plan/compile half was reasonable; the execution half was scaffolded and never ran end to end: it auto-started on boot and ran model-written shell (`bash -c task.Verify`), enqueued empty tasks, never promoted blocked tasks, looped forever on timeouts, ran the agent in the server's own directory, called a non-existent binary, could not write state on a fresh install, lost queue metadata on every write, raced on queue.json, could crash the server from POST /settings, and rendered model output as raw HTML. Decisions taken while walking the findings are recorded in `decisions`; every requirement traces to findings and decisions.

## Constraints

- Work lands on the existing `agentflow` git branch on top of the uncommitted agentflow files; no new branch, no stash. One commit per passing story; never push.
- Execution reuses taskmaster via golane.Host exactly as utuber does; agentflow owns no worker loop, lock file or SSE broker of its own.
- No task, plan or model output may contain executable content that agentflow runs; verification is engine-owned Go code with fixed argv commands.
- Models never write git state (add, commit, branch, tag, stash, push, reset, checkout of other refs); the engine owns all git writes; agentflow never pushes.
- Agentflow writes no state into project repositories; all state lives in data_dir (SQLite + worktrees).
- Users interact with agentflow state only through the UI/API, never by editing files.
- Use modernc.org/sqlite and follow taskmaster's versioned-migration pattern.
- Follow existing module conventions: Build(cfg, host) -> http.Handler, static.NewHandler, buildModule's unavailableHandler on error, shared frontend barrel (@shared), descriptors.mjs bundle build, `make gates`.
- No legacy migration: the old `.agentflow/` directory is never read.
- All Go code gofmt-clean; `go vet`, `go test ./...`, `npx tsc --noEmit -p .`, and `make gates` pass after every story.
- Keep the existing agentflow look and feel: web/agentflow/style.css, the nav bar with hamburger and logout, the tab bar, the console layout (progress header, task list, detail pane, slide-in stream panel), status chips and shared theme. New views extend this visual language rather than replacing it.

## Requirements

Must: 113, Should: 11, Could: 5, Won't: 2

### RC — Run controller

_Explicit per-project start/pause/stop/run-one; always boots stopped._

- **RC1** (Must) Each project's run controller boots in `stopped` regardless of its state before shutdown; no stage is dispatched for a project until an operator (or YOLO, YO2) starts it. _(findings #1; AC-RC1)_
- **RC2** (Must) Run states are stopped | running | pausing | paused, driven by start, pause (finish in-flight stages, dispatch nothing new), stop, and run-one (dispatch exactly one task's next attempt, then pause). These replace POST /run-next and POST /tasks/run. _(findings #1, #2, #11; AC-RC3)_
- **RC3** (Must) `stop` cancels in-flight stages through lane.Cancel, killing the stage's whole process tree; the cancellation is recorded as an infra retry (not an attempt) and the task returns to eligible. _(findings #1; AC-RC2, AC-TM2)_
- **RC4** (Must) On module start, agentflow pauses each of its lanes before the dispatcher runs (taskmaster persists lane pause state across restarts) and reconciles in-flight stages (DBS5) before resuming dispatch for running projects. _(findings #1, #15; AC-RC1)_
- **RC5** (Must) Each project's run state is visible in the UI and pushed over the agentflow event stream. _(findings #1)_

### QA — Queue approval

_A compiled queue is a draft until approved; approval is hash-pinned._

- **QA1** (Must) Compilation produces a queue in `queue_draft`; the dispatcher never dispatches from an effort whose queue is not `queue_approved`. _(findings #1; AC-QA1)_
- **QA2** (Must) Approving a queue records a hash of its task definitions; any task edit or recompile returns it to draft. Every dispatched stage carries the queue hash and is discarded (not counted) if the hash is stale. _(findings #1; AC-QA2, AC-DBS3)_
- **QA3** (Must) The approval view shows every task's prompt, files_touched, acceptance criteria and dependencies; approval is for the whole queue at once. _(findings #1)_

### WC — Worker containment

_Every stage runs in its worktree, own process group, minimal env, inside bwrap._

- **WC1** (Must) Every stage runs with its working directory set to its worktree, in its own process group (killed as a group on cancel or timeout), with an allowlisted environment rather than the server's. _(findings #1, #6; AC-RC2)_
- **WC2** (Must) Every model stage runs inside bwrap: the worktree is read-write; the main repository's git metadata is read-only except what the worktree strictly needs; the user's home is not visible beyond what the runner needs; no SSH agent socket, ~/.ssh, git credential helpers/stores, or gh/glab configuration is reachable. _(findings #1; AC-WC1)_
- **WC3** (Must) Agentflow refuses to register a project whose repo_path equals, contains, or is inside the server's working directory or data_dir. _(findings #1; AC-WC2)_

### V — Engine-owned verify

_Deterministic Go verify stage: structural checks + project feedback commands vs a baseline._

- **V1** (Must) Tasks carry no executable content: Task.Verify is removed, the task-compiler prompt no longer produces it, and queue validation rejects a verify field. _(findings #1; AC-V1)_
- **V2** (Must) Verify is an engine-owned Go stage run after the worker stage; it returns a structured VerifyReport stored on the stage. _(findings #1, #5; AC-V2)_
- **V3** (Must) In-process structural checks: at least one file changed; changed files compared with files_touched (out-of-scope edits are warnings passed to the reviewer); no changes outside the worktree and no symlinks escaping it; every changed file parses by extension (go/parser for .go, JSON, YAML, TOML); no conflict markers. _(findings #1; AC-V3)_
- **V4** (Must) Project feedback commands (stored in project settings; auto-detected from go.mod, pyproject.toml or package.json when unset) run as fixed argv, never through a shell, inside the stage sandbox, concurrently where independent, each with a timeout. _(findings #1; AC-V2)_
- **V5** (Must) A feedback baseline is recorded when a project's run starts and refreshed after each merged pass; a task fails verify only on new failures or on baseline failures in files it touched. Baseline failures are reported as warnings. _(findings #5; AC-V4)_
- **V6** (Must) A verify failure ends the attempt as a repair without calling the reviewer; a verify pass submits the review stage with the VerifyReport as evidence. _(findings #1)_

### TM — Taskmaster execution

_Stage jobs on per-backend taskmaster lanes, dispatched just in time._

- **TM1** (Must) Agentflow builds with the shared taskmaster engine (golane.Host) like utuber and registers its lanes on it; the agentflow package contains no worker goroutine loop, lock file, or SSE broker of its own. _(findings #14, #15, #16; AC-TM1)_
- **TM2** (Must) Each attempt is a chain of stage jobs: agentflow.work on the worker backend's lane, engine verify, agentflow.review on the reviewer backend's lane; planning roles use agentflow.plan. Payload: {project, effort, task, stage, attempt, queue_hash}. Each stage has its own timeout. _(findings #2, #17)_
- **TM3** (Must) Per-backend concurrency equals that backend lane's width, which equals the backend's slots. _(findings #15)_
- **TM4** (Must) A dispatcher submits stages just in time: only when the target backend has a free slot and the effort's queue is approved, the project is running, the task's dependencies have passed, and no stage for the task is in flight. The next project is chosen round-robin. _(findings #2, #3)_
- **TM5** (Must) Stage output and progress flow through rc.Log()/rc.Progress() and are served by lane.StreamOutput; the custom SSE handler and the broker.go additions are removed. _(findings #14, #16; AC-TM1)_
- **TM6** (Must) Tests use a fake AgentRunner and/or lanes at width 0; `make test` never starts a real agent.
- **TM7** (Must) docs/agentflow-design.md and docs/guides/agentflow.md describe the new architecture; docs/agentflow-taskmaster-fit.md is marked superseded by this FRD.

### TMX — Taskmaster changes

_Shared-kind lane registration and clean failure of RegisterLane._

- **TMX1** (Must) taskmaster's RegisterLane lets a lane reuse kinds already registered by the same owner; registering a kind owned by a different owner still fails. _(AC-TMX1)_
- **TMX2** (Must) A failed RegisterLane leaves no residual state (lane name not stuck in e.lanes, no partial kind registration); the same name can be registered again. _(AC-TMX1)_
- **TMX3** (Must) utuber behaviour and tests are unchanged by TMX1/TMX2. _(AC-TMX1)_

### PX — Worktrees and parallelism

_Worktree per task, serialized merge, safe co-scheduling._

- **PX1** (Must) The data model and dispatcher do not assume a single concurrent attempt; default backend slots is 1.
- **PX2** (Must) Each task's attempts run in an agentflow-created git worktree (under data_dir) on branch agentflow/<effort>/<task>, created from the current integration commit; repair attempts continue in the previous attempt's worktree. _(findings #6; AC-PX1)_
- **PX3** (Must) A passing attempt is merged into the effort's integration branch by a serialized merge step, then verify runs again on the integrated tree; a merge conflict or post-merge verify failure sends the task to repair. _(AC-PX1)_
- **PX4** (Should) With more than one slot available, the dispatcher co-schedules only tasks with no dependency path between them; overlapping files_touched is allowed (the merge step resolves it).
- **PX5** (Could) Per-backend concurrency hints per model (e.g. a host that can run two small models at once).

### MB — Model backends

_Many backends (Ollama hosts, free hosted tiers, opt-in paid) with slots and role candidate lists._

- **MB1** (Must) All model calls go through the AgentRunner interface (AR1); there is no direct use of OLLAMA_HOST. _(findings #7)_
- **MB2** (Must) Ollama backends are rendered as providers in agentflow's generated runner configuration; the user's global opencode configuration is never modified or loaded by jobs. _(findings #7)_
- **MB3** (Must) Each backend declares slots; its lane is `agentflow-<name>` with width = slots.
- **MB4** (Must) Each role (worker, reviewer, planner, architect, critic, compiler) has an ordered list of candidate models; the dispatcher picks the first candidate whose backend has a free slot and is allowed by the project's scope. _(AC-MB1)_
- **MB5** (Must) Paid providers are disabled by default; only local backends and free hosted tiers are eligible unless a paid model is explicitly listed for a role. _(AC-MB2)_
- **MB6** (Must) A task's reviewer model differs from its worker model; planner, architect and critic are pairwise different when the pool allows (otherwise a visible warning). _(AC-MB3)_
- **MB7** (Should) Backend health: an unreachable backend is marked down for a cooldown; rate limits and connection errors are infra retries that move to the next candidate. _(AC-MB1)_
- **MB8** (Must) Every stage records the backend and model that ran it; the UI can show pass rates per model.

### BK — Dynamic backends

_Add/edit/retire backends at runtime without restart._

- **BK1** (Must) Backends can be added, edited (host, API type, slots, models), retired and re-enabled at runtime from the UI with no restart. Backends are global across projects. _(findings #13, #25; AC-BK1)_
- **BK2** (Must) The backend list is persisted; on startup all backends are re-registered and retired ones come back retired. _(findings #25)_
- **BK3** (Must) Add registers the backend's lane; a slots change updates lane width; retire pauses and hides the lane and keeps the handle; re-enable reverses it. _(AC-BK1)_
- **BK4** (Must) Retiring a backend reroutes its queued stages; running stages finish unless the operator chooses 'cancel running', in which case they are infra retries. _(AC-BK2)_
- **BK5** (Must) Any backend change regenerates the runner configuration; running stages keep the configuration they started with.
- **BK6** (Must) Backend names satisfy taskmaster's lane-name rule; with the `agentflow-` prefix they are at most 22 characters of [a-z0-9_-] starting with [a-z0-9].

### PP — Privacy scope

_Per-project local-only model scope._

- **PP1** (Must) Each project has model_scope `local` (only operator-run backends) or `any`; under `local` the dispatcher never dispatches to a hosted backend and the task waits with a visible reason. _(AC-PP1)_

### ST — State storage

_All state under data_dir; nothing in repos; embedded prompts._

- **ST1** (Must) All agentflow state lives under data_dir (default ./data/agentflow). Agentflow writes nothing into project repositories except worker code changes inside agentflow worktrees and engine git operations on agentflow branches. _(findings #8, #31; AC-ST1, AC-ST2)_
- **ST2** (Must) data_dir is created on module start; if it cannot be, the module returns 503 with a message naming the path. _(findings #8; AC-ST1)_
- **ST3** (Must) Prompts and agent definitions are embedded with go:embed; the module never fails to start because prompt files are missing. _(findings #8; AC-ST1)_
- **ST4** (Must) Worktrees live under data_dir and are removed after merge or abandonment.

### DBS — SQLite state

_SQLite schema, migrations, transactions, revisions, reconciliation, import/export._

- **DBS1** (Must) All agentflow state lives in data_dir/agentflow.db (modernc SQLite, WAL, foreign keys on, busy timeout). Agentflow reads and writes no JSON state files. _(findings #9, #10; AC-DBS1)_
- **DBS2** (Must) The schema is created and migrated by numbered migrations, each applied with its version bump in one transaction (taskmaster's pattern); a database newer than the code is refused.
- **DBS3** (Must) Every state change goes through the API inside a transaction; each human action writes an events row with the authenticated user. _(findings #9, #10)_
- **DBS4** (Must) FRDs and plans are stored as JSON document revisions; approval records the approved revision; later edits create new revisions without altering it. _(AC-DBS3)_
- **DBS5** (Must) Each stage writes its outcome before returning; on start and periodically, reconciliation compares stages marked in flight with lane.Get() and closes cancelled, crashed or lost stages as infra failures. _(findings #15; AC-DBS2)_
- **DBS6** (Should) Any FRD, plan or whole effort can be exported as JSON and as rendered markdown; an FRD can be imported from JSON through the UI.

### PJ — Projects

_Multi-project registry, overview, round-robin dispatch._

- **PJ1** (Must) A project registry managed in the UI: slug, absolute repo_path (an existing git repository; the UI offers `git init` otherwise), model_scope, feedback commands, role preferences, limit overrides. The config key project_path is removed. _(findings #8; AC-ST1)_
- **PJ2** (Must) Each project has its own run controller (RC); a stopped project is 'resting' and still shown and monitored. _(AC-PJ1)_
- **PJ3** (Must) One dispatcher serves all running projects from the shared backend pool, choosing projects round-robin. _(AC-PJ1)_
- **PJ4** (Must) A project has at most one active effort; a repo_path can belong to only one project.
- **PJ5** (Must) An overview page shows every project: run state, active effort, progress, current stage and backend.
- **PJ6** (Must) All agentflow API routes live under /api/ (project-scoped ones under /api/projects/{slug}/...). _(findings #29)_

### MG — Migration

_No legacy migration; remove stray state from this repo._

- **MG1** (Won't) No in-app migration; agentflow never reads a `.agentflow/` directory.
- **MG2** (Must) Remove the stray `.agentflow/` directory from this repository. `draft-frd.json` is left untouched. _(findings #31)_

### AT — Attempts and failure handling

_ralph-style persistent repair loop, stuck detection, halts, progress log._

- **AT1** (Must) A task receives attempts until it passes or halts; a pass requires verify green against the baseline and a reviewer verdict of `pass`. _(findings #4)_
- **AT2** (Must) A verify failure or reviewer rejection is non-terminal: the next attempt is a repair attempt that receives the previous verify report, reviewer issues and repair prompt, and the previous attempt's diff, and continues in the previous attempt's worktree. _(findings #4; AC-AT3)_
- **AT3** (Must) The reviewer model is sticky for a task across attempts (re-verify with the same reviewer) and differs from the worker model (MB6).
- **AT4** (Must) Stuck detection: each failure gets a deterministic fingerprint (failing check + file + normalized message, or the reviewer's normalized issue set); stuck_threshold consecutive identical fingerprints halt the task as `stuck`. _(findings #5; AC-AT1)_
- **AT5** (Must) After max_attempts attempts the task halts as `exhausted`. _(findings #5)_
- **AT6** (Must) Worker or reviewer may report a human-needed blocker via structured output ({"blocked": "reason"}); the task halts as `needs_input` with the reason shown.
- **AT7** (Must) Infra failures (backend unreachable, rate limit, runner crash, cancel/stop, server restart, unparseable output after the corrective retry) do not consume attempts; they retry on another eligible backend with backoff up to max_infra_retries, then halt as `needs_attention`. A worker timeout counts as an attempt. _(findings #5; AC-AT2)_
- **AT8** (Must) A halted task blocks only its dependents; independent branches continue. An effort is complete when every task is passed or overridden. _(findings #3)_
- **AT9** (Must) Halted tasks offer: retry (resets counters; optional clean worktree), edit task (queue returns to draft), override as passed, abandon effort. Each is audited. _(findings #11)_
- **AT10** (Must) Progress log: after each attempt the engine appends what changed, files touched, and learnings (from the worker's structured output) to the effort's progress log; each attempt is a fresh runner session whose prompt includes the recent progress log. _(findings #5; AC-AT3)_
- **AT11** (Could) Optional cleanup ('deslop') stage after a pass, followed by verify; if cleanup breaks verify its changes are rolled back.
- **AT12** (Could) Per-effort token and time budgets from runner usage events: at 90% no new tasks start, at 100% the run stops; exhaustion is a stop, not a failure.

### RP — Planning (ralplan)

_Planner -> architect -> critic consensus loop with human approval._

- **RP1** (Must) Plan generation runs planner, then architect, then critic as separate stages with their own candidate models; the critic never receives the architect's review. _(AC-RP1)_
- **RP2** (Must) Any non-approve verdict triggers a revision round in which the planner synthesizes both reviews; at most plan_max_rounds rounds (default 5); on exhaustion the best revision is presented marked 'not critic-approved'. _(AC-RP1)_
- **RP3** (Must) A human always approves the plan (unless YOLO, YO2); neither critic approval nor round exhaustion approves it automatically. _(AC-RP1)_
- **RP4** (Should) The plan schema includes principles, decision drivers, at least two options considered, and an ADR (decision, drivers, alternatives, why chosen, consequences, follow-ups); each round's reviews are kept as revisions.
- **RP5** (Could) Deliberate mode adds a three-scenario pre-mortem and an expanded test plan; toggled per effort and suggested for auth, migration or PII work.

### CF — Settings

_UI-editable limits persisted in the DB; config file is bootstrap only._

- **CF1** (Must) Limits are edited in the UI and persisted in agentflow's settings table: max_attempts (default 5), stuck_threshold (3), max_infra_retries (5), plan_max_rounds (5), worker_timeout, review_timeout, plan_timeout, verify_timeout. _(findings #5, #13, #25; AC-CF1)_
- **CF2** (Should) Projects may override any limit; blank means the global value.
- **CF3** (Must) The JSON config file holds only bootstrap settings for agentflow (static_dir, data_dir); everything UI-editable (including the opencode path) lives in the database. _(findings #25)_

### OC — Opencode invocation

_Correct binary, prompt files, isolated config, fixed flags._

- **OC1** (Must) The opencode adapter runs `opencode run` from a configurable path (default `opencode` on PATH); a version check runs at start and on demand and its result is shown in Settings. _(findings #7)_
- **OC2** (Must) Each stage's full prompt is written to a file in the stage's directory under data_dir and passed to the runner as an attachment with a short instruction message, never as a single large argument; the file is kept as the record of what the model saw. _(findings #7; AC-OC1)_
- **OC3** (Must) Runner sessions are isolated from the user's opencode configuration: only agentflow's generated configuration is loaded and external plugins are disabled. _(AC-OC2)_
- **OC4** (Must) Runs always use the stage worktree as directory, JSON event output, and non-interactive auto-approval of permissions not explicitly denied. _(findings #6)_

### SV — Settings validation and crash safety

_Validated settings, snapshot reads, no panics._

- **SV1** (Must) Every settings, backend and project write is validated server-side (names, URLs, slots >= 1, non-empty role lists, max_attempts 1-50, stuck_threshold 2..max_attempts, timeouts 30s-4h); invalid writes return 400 naming the field. _(findings #13; AC-SV1)_
- **SV2** (Must) Settings changes are transactional and the dispatcher works from a consistent snapshot per pass; no engine fields are shared and mutated in place. _(findings #13)_
- **SV3** (Must) 'No eligible backend for a role' is a visible waiting state with a reason, never an index error; retiring the last backend for a role is allowed with a warning naming affected running projects. _(findings #13; AC-SV1)_
- **SV4** (Must) A panic inside a stage becomes an infra failure and never crashes the server; a test proves it. _(findings #13; AC-SV1)_

### SO — Structured output

_One schema-validated parser for every role's output._

- **SO1** (Must) One shared parser takes the final assistant message from the runner's events, extracts the JSON object, and validates it against the role's Go schema (required fields, enums, unknown fields rejected). _(findings #18; AC-SO1)_
- **SO2** (Must) On parse or validation failure the same model gets one corrective follow-up; a second failure is an infra failure of the stage (AT7), never an attempt failure or a verdict. _(findings #18; AC-SO1)_
- **SO3** (Must) Each stage stores raw output and parsed result. _(findings #18)_

### GIT — Controlled git

_Engine-owned git service; models read-only; no push; no credentials._

- **GIT1** (Must) Only the engine changes git state, through an internal Go git service that runs fixed git argv, never a shell.
- **GIT2** (Must) Agentflow never modifies the repository's main working tree or the user's checked-out branch. Work happens on agentflow/<effort> (integration; overridable per effort with a user-named branch) and agentflow/<effort>/<task> branches in agentflow worktrees. _(AC-PX1)_
- **GIT3** (Must) At the end of each attempt the engine commits the worktree as `agentflow <noreply@agentflow.local>` with a message from the progress-log entry plus Agentflow-Task/Attempt trailers; passing tasks merge via PX3.
- **GIT4** (Must) Agentflow never pushes. The UI shows the git commands to merge and push the integration branch for the human to run.
- **GIT5** (Must) Models get read-only git only (status, diff, log, show, blame, grep, ls-files, ls-tree, rev-parse, cat-file, branch --list/--show-current), enforced by three layers: runner deny rules; engine detect-and-revert of any ref/HEAD/index/stash change after each stage (policy violation, counts as an attempt, flagged, audited); and no credentials (GIT6, WC2). _(AC-GIT1)_
- **GIT6** (Must) Stage jobs run with no git or forge credentials reachable. _(AC-WC1)_

### OA — Agent definitions

_Agentflow's own embedded opencode agents replace oh-my-openagent._

- **OA1** (Must) Agentflow ships embedded agent definitions (worker, reviewer, planner, architect, critic, compiler) with per-agent permissions: the worker may edit and run allowed commands; reviewer, architect, critic and planner have no edit permission and read-only bash.
- **OA2** (Must) Orchestration (loop, planning, review, retries, learnings) is agentflow's alone; no runner plugin participates.
- **OA3** (Could) Per-project opt-in of specific runner plugins, pinned to a version (never @latest).

### YO — YOLO mode

_Per-effort auto-approval of human gates only._

- **YO1** (Should) YOLO is a per-effort UI toggle, off by default; enabling it requires confirmation, is audited, and shows a persistent badge on the effort and the overview.
- **YO2** (Should) In YOLO the human gates auto-approve: plan on critic approval (human only on round exhaustion), queue after compile, run start on approval, and one automatic clean-worktree retry of a stuck or exhausted task. _(AC-YO1)_
- **YO3** (Should) YOLO cannot relax GIT2, GIT4-GIT6, WC1-WC3, MB5 or PP1; the UI lists them as still enforced beside the toggle. _(AC-YO1)_
- **YO4** (Should) Turning YOLO off mid-run takes effect at the next gate; prior auto-approvals stand.
- **YO5** (Should) Auto-approvals are audited with actor `yolo`.

### AR — Agent runner abstraction

_Opencode behind an interface so it can be replaced._

- **AR1** (Must) Every model call goes through a Go AgentRunner interface (role, model, workdir, prompt -> events, result); opencode is the first adapter. _(findings #7; AC-AR1)_
- **AR2** (Must) Everything opencode-specific (flags, generated config, providers, agent definitions, permission rules, event parsing) lives inside the adapter package. _(AC-AR1)_
- **AR3** (Must) Backends and models are stored in agentflow terms (host, API type, model name); the adapter translates them. _(findings #24)_
- **AR4** (Won't) Native runner (agentflow-owned tool loop against Ollama/OpenAI-compatible APIs) — direction only, not this release.

### UI — Frontend

_Rewritten multi-project UI: escaped output, one fetch path, SSE._

- **UI1** (Must) No model- or user-supplied text reaches the DOM as HTML: textContent or one shared escape helper; a gate fails on unescaped interpolation inside innerHTML templates. _(findings #19; AC-UI1)_
- **UI2** (Must) Every API call goes through one apiFetch; errors are surfaced, never swallowed; one endpoint per operation. _(findings #20)_
- **UI3** (Must) Live updates via SSE: taskmaster stream for stage output and an agentflow event stream for project/effort/task state; no fixed-interval polling.
- **UI4** (Must) No circular imports; web/agentflow/main.js and other leftovers deleted. _(findings #21, #22)_
- **UI5** (Must) Views: project overview; per project FRD editor, plan with ralplan rounds, queue approval, task board with halted-task actions and live stream, effort history/export; global Settings (limits, opencode) and Backends.
- **UI6** (Must) The rewritten UI preserves the current aesthetic: existing style.css tokens and classes, nav/hamburger/logout, tab bar, console layout (progress header, task list, detail pane, slide-in stream panel) and status chips. New views (project overview, backends, ralplan rounds, queue approval) reuse these components and the shared theme; visual changes beyond what new features require are out of scope. _(AC-UI2)_

### CL — Code hygiene

_Typed errors, no duplicate/dead types, gofmt, broker revert._

- **CL1** (Must) Store and runner errors are typed sentinels (ErrNotFound, ErrConflict, ErrInvalid) mapped to HTTP codes with errors.Is; no os.IsNotExist on wrapped errors and no error-string matching. _(findings #12)_
- **CL2** (Must) Each type is defined once; config.ProfileConfig, the duplicate DefaultAgentflowProjectPath, ResolvedProfile, TaskExecution, and ExpandPath on URLs are removed. _(findings #23, #24)_
- **CL3** (Must) All Go is gofmt-clean and a gate fails on unformatted files. _(findings #30; AC-GT1)_
- **CL4** (Must) broker.go's exported SubscribePayload/UnsubscribePayload are removed. _(findings #16)_

### CI — Config and deployment

_No personal infra in defaults; bootstrap-only config; explicit unit-file path._

- **CI1** (Must) Defaults and the example config contain no personal IPs, hostnames or model names; with no backends the UI prompts to add one and nothing dispatches. _(findings #26)_
- **CI2** (Must) The agentflow config section is bootstrap-only (static_dir, data_dir) and the example JSON is correctly indented. _(findings #26)_
- **CI3** (Must) unified.service keeps reading the home-directory config, written as the explicit path /home/cdelezenski/.unified-webapp.json. _(findings #27)_

### GT — Gates

_Restore/strengthen bundle-shape and hamburger gates; add escape and gofmt gates._

- **GT1** (Must) bundle-shape.mjs prints its PASS summary again. _(findings #28; AC-GT1)_
- **GT2** (Must) hamburger-alignment.mjs fails on modules whose hamburger side it cannot determine, unless they are on an explicit not-applicable list. _(findings #28; AC-GT1)_
- **GT3** (Should) bundle-shape's bundle-mode module list is derived from descriptors.mjs. _(findings #28)_
- **GT4** (Must) `make gates` includes the UI1 escape gate and the CL3 gofmt gate. _(findings #19, #30; AC-GT1)_

## State model

| Task status | Meaning |
|---|---|
| `blocked` | At least one dependency has not passed (derived). |
| `ready` | Dependencies passed, no attempt in flight, attempts remain (derived). |
| `running` | A stage of the current attempt is queued or running (derived). |
| `repair` | Last attempt failed verify or review; attempts remain; next attempt is a repair attempt (derived). |
| `passed` | Last attempt passed verify and review and was merged into the integration branch (derived). |
| `overridden` | A human (or YOLO) override marked the task passed (from overrides). |
| `stuck` | Halted: same failure fingerprint on stuck_threshold consecutive attempts. |
| `exhausted` | Halted: max_attempts reached. |
| `needs_input` | Halted: worker or reviewer reported a blocker that needs a human. |
| `needs_attention` | Halted: max_infra_retries reached or a policy violation threshold was hit. |

**Effort statuses:** `draft` → `planning` → `plan_ready` → `plan_approved` → `compiling` → `queue_draft` → `queue_approved` → `running` → `paused` → `complete` → `abandoned`

**Run states:** `stopped`, `running`, `pausing`, `paused`

Task status is computed from tasks, task_deps, attempts, stages and overrides; it is never stored as a mutable field.

## Acceptance criteria

| ID | Criterion | Requirements |
|---|---|---|
| AC-RC1 | Restarting the server with an approved queue and a project that was running dispatches no stage within 60 seconds. | RC1, RC4 |
| AC-RC2 | `stop` during a stage leaves no child processes and the task is eligible again with its attempt count unchanged. | RC3, WC1 |
| AC-RC3 | run-one dispatches exactly one task's attempt and the project ends in `paused`. | RC2 |
| AC-QA1 | start on an effort whose queue is in draft returns 409. | QA1 |
| AC-QA2 | Editing a task in an approved queue returns it to draft and in-flight stages with the old hash are discarded without counting. | QA2 |
| AC-WC1 | Inside a stage, reading ~/.ssh fails and `git push` fails for lack of credentials. | WC2, GIT6 |
| AC-WC2 | Registering /opt/unified-webapp (or a path inside it, or data_dir) as a project is rejected. | WC3 |
| AC-V1 | A compiled queue contains no verify field; queue validation rejects one. | V1 |
| AC-V2 | No code path in verify invokes bash, sh, or -c; a test enforces it. | V2, V4 |
| AC-V3 | A worker stage that changes nothing fails verify; an edit outside files_touched appears as a warning in the VerifyReport. | V3 |
| AC-V4 | A test failing before the run started does not fail an unrelated task. | V5 |
| AC-TM1 | Agentflow stages appear in the taskmaster UI and stream output; the agentflow package has no lock file or worker loop. | TM1, TM5 |
| AC-TM2 | Engaging taskmaster's hand-brake cancels a running agentflow stage and the task returns to eligible. | RC3 |
| AC-TMX1 | Two lanes share agentflow.work; a failed registration followed by a corrected retry succeeds; utuber tests pass unchanged. | TMX1, TMX2, TMX3 |
| AC-PX1 | After an effort completes, the user's checked-out branch and working tree are byte-identical to before, and agentflow/<effort> contains the merged work. | PX2, PX3, GIT2 |
| AC-MB1 | With backend m5 busy or unreachable, the next eligible worker stage runs on another backend and no attempt is counted for the outage. | MB4, MB7 |
| AC-MB2 | On a default configuration no stage ever uses a paid-provider model. | MB5 |
| AC-MB3 | No attempt has the same model as worker and reviewer. | MB6 |
| AC-BK1 | Adding a backend at runtime creates its lane and it receives work without a restart. | BK1, BK3 |
| AC-BK2 | Retiring a backend with queued stages reroutes them to other backends. | BK4 |
| AC-PP1 | A `local` project never produces a stage on a hosted backend. | PP1 |
| AC-ST1 | From a clean clone with empty data/ and no prompts/ directory, the server starts, agentflow is not 503, a project can be added, and an FRD can be saved; a test reproduces this in a temp directory. | ST1, ST2, ST3, PJ1 |
| AC-ST2 | After an effort completes, `git status` in the project repo shows no agentflow files. | ST1 |
| AC-DBS1 | internal/agentflow contains no state-file writes (os.WriteFile/json.MarshalIndent to state paths). | DBS1 |
| AC-DBS2 | Killing the server during a stage and restarting reconciles the stage as an infra failure and the task becomes eligible. | DBS5 |
| AC-DBS3 | Editing an approved plan creates a new revision; the approved revision remains viewable. | DBS4, QA2 |
| AC-PJ1 | With project A running and B stopped, only A dispatches and B is on the overview; with both running on a one-slot backend, their attempts alternate. | PJ2, PJ3 |
| AC-AT1 | A task failing verify on the same check three consecutive times halts as `stuck` and is never marked failed before that. | AT4 |
| AC-AT2 | Killing a backend mid-stage does not increment attempts. | AT7 |
| AC-AT3 | A repair attempt's prompt contains the previous reviewer issues, verify report and recent progress log. | AT2, AT10 |
| AC-RP1 | The critic's input never contains the architect's review; the loop stops after plan_max_rounds and requests human approval without auto-approving. | RP1, RP2, RP3 |
| AC-CF1 | Changing max_attempts in the UI survives a restart. | CF1 |
| AC-OC1 | A 500 KB repair prompt runs successfully. | OC2 |
| AC-OC2 | With a plugin listed in the global opencode config, an agentflow stage shows the plugin was not loaded. | OC3 |
| AC-SV1 | POSTing an empty backend list or empty role list never panics; invalid settings return 400 naming the field; an injected panic in a stage is an infra failure. | SV1, SV3, SV4 |
| AC-SO1 | A fenced reviewer reply parses; a reply with verdict "maybe" gets one corrective retry then becomes an infra failure without changing attempts. | SO1, SO2 |
| AC-GIT1 | A test worker running `sh -c 'git commit -am x'` triggers a policy violation; the commit is reverted and the task branch contains only engine commits. | GIT5 |
| AC-YO1 | A YOLO effort goes from saved FRD to running tasks with no further clicks, and its worker still cannot push or read ~/.ssh. | YO2, YO3 |
| AC-AR1 | A recording fake runner drives the full dispatcher/stage/review test suite; no package outside the adapter imports opencode-specific code. | AR1, AR2 |
| AC-UI1 | An FRD title `<img src=x onerror=alert(1)>` and a reviewer summary containing `<script>` render as literal text. | UI1 |
| AC-GT1 | `make gates` prints PASS lines for every gate, fails on an unformatted Go file, and fails on an unescaped innerHTML interpolation. | GT1, GT2, GT4, CL3 |
| AC-UI2 | Side-by-side screenshots of the queue/console view before and after show the same layout, typography, colours and chip styles; new views use the same nav, tab bar and component classes. | UI6 |

## Decisions

| ID | Decision | Rationale |
|---|---|---|
| D1 | Verify is engine-owned deterministic Go, not model-authored (shell or structured). | Mirrors `omc ralph verify`; the compiler prompt's many shell-pitfall rules showed model-written verify was fragile and unsafe. |
| D2 | Execution runs on taskmaster lanes via golane.Host (supersedes agentflow-taskmaster-fit.md). | DRY with utuber; agentflow keeps the task graph and dispatcher so taskmaster never needs dependencies or approval gates. |
| D3 | Stage jobs on per-backend lanes; taskmaster enforces per-host concurrency. | Many hosts with different capacity; visibility in the taskmaster UI. |
| D4 | Backends are dynamic at runtime and global; retire instead of unregister. | Hosts come and go; taskmaster has no lane unregister. |
| D5 | Taskmaster gains shared-kind registration and clean RegisterLane failure. | Needed for per-backend lanes; fixes an existing taskmaster bug; benefits taskmaster. |
| D6 | Multiplex Ollama hosts, free hosted tiers, and opt-in paid models. | Use all available capacity; paid models off by default to avoid surprise cost. |
| D7 | Per-project local-only scope. | Free hosted tiers may retain prompts; some projects must stay local. |
| D8 | No hidden per-repo .agentflow; all state server-side under data_dir. | Keep repos clean; agentflow owns its state. |
| D9 | SQLite for all state; UI is the only way to change it. | Transactions replace hand-rolled file writes; supports multi-project, revisions, audit. |
| D10 | Multi-project with per-project run controllers. | Several projects monitored at once, some resting, some active. |
| D11 | No migration of legacy .agentflow. | No rollout yet; legacy state came from a failed self-hosting experiment. |
| D12 | Failure handling and planning emulate ralph and ralplan. | Proven loop: non-terminal rejection, stuck detection, progress log, blind critic, max 5 rounds. |
| D13 | Limits are UI-configurable and persisted in the DB; config file is bootstrap-only. | UI-first; avoid the UI rewriting the user's config file. |
| D14 | Repair attempts build on the previous attempt's worktree. | ralph's 'fix the issues' rather than redo. |
| D15 | Models get read-only git; the engine owns all git writes; never push; no credentials in stages. | Pushing is irreversible; policy rules alone are bypassable from a shell. |
| D16 | Sandbox with bwrap (installed at /usr/bin/bwrap). | Only reliable way to hide credentials from a shell-capable agent without a separate OS user. |
| D17 | Replace oh-my-openagent; jobs never load the user's opencode plugins or config. | Agentflow is the orchestrator; reproducible runs; no private tools leaking into jobs. |
| D18 | YOLO auto-approves human gates only. | Gates cost time; safety invariants protect against irreversible outcomes. |
| D19 | Opencode behind an AgentRunner interface. | Long-term goal to distance agentflow from opencode. |
| D20 | Refuse projects inside the server's own directory. | Self-hosting caused the original failure; revisit once sandbox and worktrees are proven. |
| D21 | Integration branch agentflow/<effort> by default, overridable per effort. | The human merges and pushes. |
| D22 | unified.service reads /home/cdelezenski/.unified-webapp.json explicitly. | Keep the home config without relying on in-app ~ expansion. |
| D23 | Implement all priorities (must, should, could); commit per passing story on `agentflow`. | Owner's choice for the ralph run. |
| D24 | Keep the existing agentflow look and feel. | Owner likes the current aesthetic; the rewrite is functional, not visual. |

## Findings → requirements

| # | Finding | Covered by |
|---|---|---|
| 1 | Worker auto-starts and runs model-authored shell unattended | RC1, RC2, RC3, QA1, QA2, QA3, WC1, WC2, WC3, V1, V2, V3, V4, V6 |
| 2 | POST /tasks/run enqueues an empty task | RC2, TM2, TM4 |
| 3 | Blocked tasks are never promoted | TM4, AT8 |
| 4 | Repair is a dead end | AT1, AT2 |
| 5 | Timeouts retry forever; counters never persisted | V5, AT4, AT5, AT7, AT10, CF1 |
| 6 | Agent runs in the server's working directory | WC1, PX2, OC4 |
| 7 | opencode-run binary missing; prompt as argv | MB1, MB2, OC1, OC2, AR1 |
| 8 | Fresh install cannot write state | ST1, ST2, ST3, PJ1 |
| 9 | Status writes drop queue metadata | DBS1, DBS3 |
| 10 | No locking, non-atomic writes | DBS1, DBS3 |
| 11 | Action endpoints write invalid statuses | RC2, AT9 |
| 12 | os.IsNotExist on wrapped errors | CL1 |
| 13 | POST /settings can panic the server; races | SV1, SV2, SV3, SV4, BK1, CF1 |
| 14 | Data races on currentExec | TM1, TM5 |
| 15 | Worker.Stop deadlock; lock recovery wrong | TM1, TM3, RC4, DBS5 |
| 16 | SSE bypasses subscriber cap | TM1, TM5, CL4 |
| 17 | Worker and reviewer share one timeout | TM2 |
| 18 | Task review parsing stricter than plan review | SO1, SO2, SO3 |
| 19 | XSS via innerHTML | UI1, GT4 |
| 20 | Frontend swallows errors; duplicate endpoints | UI2 |
| 21 | Circular import | UI4 |
| 22 | Stale web/agentflow/main.js | UI4 |
| 23 | Duplicate/dead types | CL2 |
| 24 | ExpandPath applied to OllamaHost | CL2, AR3 |
| 25 | Settings not persisted; prompt_dir ineffective | BK1, BK2, CF1, CF3 |
| 26 | Personal LAN IPs in defaults; broken example JSON | CI1, CI2 |
| 27 | unified.service ~ path | CI3 |
| 28 | Gates: missing PASS line; vacuous hamburger gate | GT1, GT2, GT3 |
| 29 | Routes not under /api | PJ6 |
| 30 | gofmt failures | CL3, GT4 |
| 31 | Untracked state files in the repo | ST1, MG2 |

## Out of scope

- Native agent runner (AR4).
- Automatic or in-app migration of legacy .agentflow state (MG1); an out-of-band script may be written separately if ever wanted.
- Extracting taskmaster's SQLite open/migrate code into internal/platform/sqlitedb.
- Pushing, creating merge requests, or any forge integration.
- Allowing projects inside the server's own directory (revisit after WC2/PX are proven).
- Keeping oh-my-openagent or any plugin by default.

## Planning spikes

- **S1** Does `opencode run -f <file>` with a short message reliably deliver a large prompt (>=500 KB)? If not, what is the fallback (stdin, --attach server, session API)?
- **S2** How does opencode match permission rules (ordering, wildcards) and does --auto honour explicit denies for bash patterns?
- **S3** How is opencode isolated from the user's config: OPENCODE_CONFIG, XDG_CONFIG_HOME override, --pure, or a combination? Which agent/permission keys does the generated config need?
- **S4** Under bwrap with the main .git read-only, which paths must stay writable for read-only git commands in a worktree (worktree index under .git/worktrees/<id>, GIT_OPTIONAL_LOCKS=0)? Can opencode reach model APIs (network) while ~ is hidden?
- **S5** What is the schema of `opencode run --format json` events: final assistant message, token usage, errors, rate-limit signals?
- **S6** How do taskmaster lane pause, width 0, Cancel and hand-brake interact with func-task cancellation (does ctx cancel reach Run promptly)?
