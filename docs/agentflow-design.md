# AgentFlow design

Status: **implemented** under [FRD-AF-REM-001](agentflow-remediation/frd.md). This document
describes the code as it is. The operator view is in [guides/agentflow.md](guides/agentflow.md).

AgentFlow runs coding models against a git repository you already have. You write a
requirements document (FRD), models plan it, you approve the plan and the compiled task
queue, and then models work each task in an isolated git worktree. Engine-run checks and a
second model review every change before the engine merges it into an integration branch.
You decide when to merge that branch into your own.

Scope: AgentFlow is a **local, single-user tool**. Safety requirements in the FRD are
enforced. Extra hardening beyond them is not done and is listed in
[Security posture](#12-security-posture).

## 1. Principles

| Principle | What it means in code |
|---|---|
| The engine owns git, verification and state | Models never commit, merge or change refs. The engine does, through one package (`gitsvc`). |
| Models are untrusted | Every model call runs in a bwrap sandbox with an allowlisted environment. |
| State lives in one SQLite database | Nothing is stored in your repository or in JSON files. A test (`TestNoStateFiles`) fails on new file-based state. |
| Status is derived | A task's status is computed from attempts, stages, overrides and resets. It is never a stored column. |
| Humans approve the gates | FRD, plan and queue are approved by a person. YOLO only stands in for that click. |
| Nothing starts by itself after a restart | Every project boots `stopped`. |

## 2. Packages

All packages live under `internal/agentflow/`.

| Package | Responsibility |
|---|---|
| `agentflow` (`build.go`) | Composition root. Opens the database, builds the git service, sandbox template and opencode adapter, starts the engine, registers the API and the static UI. Returns a handler that implements `io.Closer`. |
| `engine` | The orchestrator: lane and kind registration, dispatcher, attempt chain, stage runtime, failure policy, planning loop, compile, run control, backend health, halted-task actions, YOLO, budgets, cleanup, reconciliation. |
| `store` | SQLite access (`modernc`-style pure Go driver via `database/sql`), migrations v1 to v8, settings, backends, projects, efforts, documents, queues, tasks, attempts, stages, events. Validation of configuration lives here. |
| `model` | Pure types and rules: queue validation and hashing, `DeriveTaskStatus`, effort and run states. No I/O. |
| `gitsvc` | The only place that runs git. Hardened argv calls, worktrees, task and integration branches, commit, serialized merge, policy detect-and-revert, push instructions as text. No push code path exists. |
| `sandbox` | Builds the bwrap command: deny-by-default root, allowlisted environment, read-only worktree option. |
| `verify` | Structural checks on a diff, feedback commands (sandboxed), failure signatures, baseline comparison. |
| `runner` | Runner-agnostic interface (`Runner`, `Request`, error classes), a scriptable fake for tests, and plugin pin validation. |
| `runner/opencode` | The only opencode-specific code: generated config, agent definitions, ordered permission rules, isolation environment, JSONL event parsing, version probe. |
| `api` | HTTP handlers under `/api/`, the SSE event bus, export and import. |
| `prompts` | Embedded role prompts (`planner`, `architect`, `critic`, `compiler`, `worker`, `reviewer`, `cleanup`). |
| `schema` | Strict parsers for model replies (last JSON object rule), per-role output types. |

The engine does not import `runner/opencode` (enforced by `TestImportBoundary`); the
composition root hands it a config generator instead.

## 3. Taskmaster lanes and kinds

AgentFlow owns no worker loop. It registers lanes on the shared taskmaster engine
(owner `agentflow`) and submits jobs to them.

| Lane | Width | Kinds | Runs |
|---|---|---|---|
| `agentflow-<backend>` | the backend's `slots` | `agentflow.work`, `agentflow.review`, `agentflow.plan` | Model stages. One lane per backend. |
| `agentflow_verify` | setting `verify_slots` (default 2) | `agentflow.verify`, `agentflow.merge` | Engine-run verification. |
| `agentflow_merge` | 1 | `agentflow.verify`, `agentflow.merge` | Serialized merges plus post-merge verify. |

- Backend lanes use a hyphen and the engine lanes an underscore, so a backend name can never
  collide with them. The names `verify` and `merge` are reserved as stage backends.
- `agentflow.work` carries the worker (and cleanup) role, `agentflow.review` the reviewer,
  `agentflow.plan` the planner, architect, critic and compiler.
- The database is authoritative for slots. `SyncLanes` re-asserts width, hidden and paused
  state. A retired backend's lane is paused and hidden.
- Lanes are paused until the engine finishes starting (the "hold"). If the taskmaster brake is
  engaged, AgentFlow leaves pause state alone, and it only unpauses lanes it paused itself.
- A lane job ends in some state, but the stage outcome is always written to the AgentFlow
  database first (a panic becomes `infra:panic`).

## 4. The stage chain

An attempt is one pass at a task. The engine drives it with an idempotent `Advance` that
derives the next step from stored stages, so a restart repeats nothing.

```
work -> verify -> review -> [cleanup -> verify] -> commit -> merge + post-merge verify
```

| Step | Who runs it | Notes |
|---|---|---|
| work | model, worker role, read-write worktree | The only stage that may write files. Starts from the integration commit. |
| verify | engine | Git policy check, structural checks, then feedback commands. Gates review. |
| review | model, reviewer role, read-only worktree | Sees the verify report. A distinct model from the worker is required. |
| cleanup | model, optional | Only if the project enables it. See [Cleanup](#9-budgets-cleanup-and-plugins). |
| commit | engine | Commits on the task branch with an engine identity. |
| merge | engine, `agentflow_merge` lane | Merges the task branch into the integration worktree, then verifies the integrated tree in the same stage. |

A conflict or a post-merge verify failure resets the integration branch to where it was and
ends the stage as `repair`, which opens a repair attempt in the same worktree.

Before any model stage runs, the engine takes a git `Snapshot` of the worktree. Afterwards it
runs `DetectAndRevert` (see [Git policy](#6-git-policy)).

### Dispatch

One dispatcher goroutine creates attempts and submits their first stage. It runs on a kick
(an outcome or a settings change) and on a safety tick (30 s). Reconciliation also runs on a
60 s tick. Per pass the dispatcher:

- serves projects round-robin;
- dispatches only tasks whose derived status is `ready` or `repair`;
- walks the role's candidate list for a backend and model, honouring model scope,
  paid opt-in, backend health and per-model concurrency hints;
- records a `waiting_reason` on the task when no candidate is eligible;
- starts a worker only if a distinct reviewer model is eligible.

## 5. State model

### Efforts

An effort is one FRD-to-merge run for a project. A project has at most one active effort
(unique index).

`draft -> planning -> plan_ready -> plan_approved -> compiling -> queue_draft -> queue_approved -> running <-> paused -> complete`
and `abandoned` from any active state.

### Task statuses (derived)

Statuses are computed by `model.DeriveTaskStatus` and never stored.

| Status | Meaning |
|---|---|
| `blocked` | A dependency is not `passed` or `overridden`. |
| `ready` | Dependencies done, no counting attempt yet. |
| `running` | An attempt is open or a stage is queued or running. |
| `repair` | A counting attempt failed and attempts remain. |
| `passed` | The last attempt passed (merged). |
| `overridden` | A human accepted the task (`override`) without a pass. |
| `stuck` | The last `stuck_threshold` attempts failed with the same fingerprint. Halted. |
| `exhausted` | `max_attempts` counting attempts used. Halted. |
| `needs_input` | The worker or reviewer reported it is blocked. Halted. |
| `needs_attention` | `max_infra_retries` infrastructure failures, or 3 git policy violations. Halted. |

A halted task blocks only its own dependents. Other tasks continue.

### Attempts

An attempt has an outcome: `passed`, `failed`, `blocked`, `policy_violation` or
`abandoned`. **`abandoned` never counts** toward `max_attempts`, stuck detection or any
limit. Stop, cancel, restart and reset all close the open attempt as `abandoned`, so
interrupting a run costs nothing. At most one attempt per task is open (unique index).

### Resets are epochs

`retry` writes a `task_resets` row holding the highest attempt id and infra-retry id at that
moment. Only attempts and infra retries with a greater id count afterwards. History is never
deleted.

### Infrastructure versus attempt failures

| Kind | Outcomes | Effect |
|---|---|---|
| Attempt failure | `repair`, `blocked`, `timeout`, `policy_violation` | Closes the attempt; counts toward limits. |
| Infrastructure | `infra:rate_limit`, `infra:unreachable`, `infra:crash`, `infra:timeout`, `infra:unparseable`, `infra:panic` | Does not consume an attempt. The stage is retried with backoff (10 s, doubling, capped at 5 min) and each strike counts toward `max_infra_retries`. `0` means unlimited. |
| Not counted at all | `infra:cancel`, `infra:lost`, `infra:rerouted`, `discarded` | Cancel by a person, a lost stage after a restart, a reroute after a backend retire, or a stage made stale by a changed queue. |

`infra:unreachable` also marks the backend down for `backend_cooldown` (default 2 minutes).

## 6. Git policy

All git runs through `gitsvc`: fixed argv (never a shell), a scrubbed environment and an
explicit directory.

- **Hardened calls.** Every engine git call uses `gitsvc.Hardened`: an empty-tree attribute
  source, no attributes files, every config filter neutralised, and no `ssh` or `file`
  protocol. Consequence: engine commits store raw bytes (no LFS, git-crypt or eol
  conversion).
- **AdminGitDir.** The engine never trusts `$WORKTREE/.git`, which a model can rewrite. It
  resolves the worktree's admin directory from the main repository and refuses a symlinked
  gitfile.
- **Branches.**
  - Integration branch: `agentflow/effort-<id>` (or a name you choose when creating the effort;
    a pre-existing branch that AgentFlow did not create is refused).
  - Task branches: `agentflow-task/<effort>/<task>`. Git cannot hold `agentflow/<effort>` and
    `agentflow/<effort>/<task>` together, hence the separate prefix.
  - AgentFlow-created override branches carry a `refs/agentflow/owned/<branch>` marker.
- **Worktrees.** Under `<data_dir>/worktrees/<project>/effort-<id>/<task>`. Your checked-out
  branch and main working tree are never modified.
- **Models get read-only git.** Layer 1 is opencode's ordered permission rules (last match
  wins: allow all, deny `git *`, allow read-only subcommands). Layer 1 is a convenience only:
  grouped redirections, `/usr/bin/git`, `env git` and `sh -c` get past it. Layer 2 is the
  worktree-scoped **detect-and-revert**: after each model stage the engine compares HEAD, the
  task branch, the index and merge markers with the snapshot and restores them, records an
  audit event and ends the attempt as `policy_violation`. It looks only at that worktree, so
  concurrent engine writes elsewhere are never mistaken for violations. Layer 3 is the
  sandbox: no credentials are reachable and, for non-worker roles, the worktree is read-only.
  Three policy violations on a task halt it as `needs_attention`.
- **No push.** There is no push code path. When an effort is `complete`,
  `GET /api/projects/{slug}/efforts/{id}/git-commands` returns the commands to merge the
  integration branch into your branch and push. You run them.
- **Worktree prune** runs only when every stale entry is under `<data_dir>/worktrees`.

## 7. Sandbox

Every model stage and every feedback command runs under bwrap.

- **Deny-by-default root.** Read-only: `/usr` and the merged-`/usr` links, plus an allowlist of
  `/etc` entries (loader, passwd, resolver, TLS roots, timezone). Tmpfs for `/home`, `/run`,
  `/tmp`. Bound in: the repository's `.git` and `.git/worktrees/<id>` read-only, the worktree
  (read-write for a worker, read-only otherwise), the stage home read-write at
  `/home/agent`, and the resolved opencode binary read-only. The server's working directory,
  data directory, config and sibling stage directories do not exist inside.
- **Environment allowlist.** `PATH`, `HOME`, `LANG`, `TERM=dumb`, `GIT_TERMINAL_PROMPT=0`,
  `GIT_CONFIG_GLOBAL=/dev/null`, `GIT_CONFIG_NOSYSTEM=1`, `GIT_OPTIONAL_LOCKS=0`, `XDG_*`,
  `GOCACHE`, `GOMODCACHE`, `GOPATH`, `GOTOOLCHAIN=local`. Nothing is inherited from the
  server. The runner adds only the `AGENTFLOW_KEY_*` variables.
- **Process control.** Own process group, `--unshare-pid` and `--die-with-parent`: when bwrap
  dies the kernel kills everything in the sandbox.
- **Read-only worktree for non-workers.** Planner, architect, critic, compiler and reviewer
  cannot write regardless of what the permission layer lets through. Plan stages run in the
  project repository itself, bound read-only.
- **Feedback commands** run with the worktree read-only, `GOPROXY=off` and the host Go module
  cache bound read-only at `/sandbox/gomod`.
- **Project toolchains (AF-6).** A project may set `toolchains`; `dotnet` is the first. It mounts the host
  `~/.dotnet` read-only at `/sandbox/dotnet` (on `PATH`, as `DOTNET_ROOT`) and the host `~/.nuget/packages`
  read-only at `/sandbox/nuget` (`NUGET_PACKAGES`), with `DOTNET_CLI_HOME` in the writable stage home and
  `DOTNET_NOLOGO`, `DOTNET_SKIP_FIRST_TIME_EXPERIENCE` and `DOTNET_CLI_TELEMETRY_OPTOUT` set, so a C# project
  builds and tests offline with no wrapper script. A project may also set `feedback_env`, extra variables for
  its feedback commands (never a secret), and `verify_in_copy`: verify then copies the worktree into the stage
  home without `.git`, `bin` and `obj` and runs the feedback commands there read-write, because `dotnet build`
  writes build output next to the sources and the worktree itself stays read-only. A C# feedback command passes
  `-m:1`: MSBuild's parallel worker nodes deadlock when process spawning is confined, so a fresh copy stalls for
  five minutes, while the same build with `-m:1` finishes in seconds.
- **Docker projects (AF-12).** A project may set `sandbox: "docker"` and everything above runs
  in its image instead: the worktree at its host path (read-write for a worker, read-only
  otherwise), the git dirs read-only, the stage home at `/home/agent`, the same environment
  allowlist, and no network unless the project asks for it. The engine starts the container
  runtime before the first stage and stops it when idle; a runtime that cannot start fails the
  stage with the reason rather than falling back to the host. Details and the image rebuild:
  [agentflow-docker.md](agentflow-docker.md).
- **Network.** Model stages share the host network namespace (they must reach their backend).
  A container stage has no network by default.
  See [Security posture](#12-security-posture).

### macOS (sandbox-exec)

macOS has no bwrap; there the same `sandbox.Command` builds a Seatbelt profile and runs the
stage under `/usr/bin/sandbox-exec` (`sandbox/seatbelt_darwin.go`). Same rules where macOS
allows them: writes only to the stage home (which is `HOME` and holds `TMPDIR`) and, for a
worker, the worktree; git admin dirs and the worktree's gitfile read-only; the server user's
credentials (`~/.ssh`, `~/.gnupg`, `~/.aws`, API key dotfiles, the opencode auth file, the
server config) and the server's data dir (except the stage's own home and worktree)
unreadable; the same allowlisted environment; loopback only without `Net`; own process group.

Differences, by design of the host:

- **No path remapping.** `SandboxHome` and every `ExtraROBinds` destination are rewritten to
  their host paths in the environment and argv.
- **Reads are allowed by default.** Toolchains read system paths; only the paths above are
  denied. bwrap's empty root has no macOS equivalent.
- **No PID namespace.** Cancel signals the process group; a descendant that starts its own
  process group can outlive a cancelled stage.

### Docker (container stages)

`Spec.Docker` selects the container backend, so `sandbox.Command` is the only entry point on
both paths. The container gets the worktree, the trusted git directories and the stage home
at the same absolute paths (a worktree's `.git` file holds absolute paths), plus the
project's declared mounts. Two differences from the host sandbox are deliberate: the image
supplies the toolchain, and the stage's network is `none` unless the project opts into
`provider` (a proxy) or `full`. Cancelling a stage closes the docker CLI *and* kills the
container by name, because the container is not a child of the server.

The runtime is behind `dockervm.Driver`: Colima on macOS, the native daemon on Linux. The
manager holds one start/stop at a time, reuses a runtime that already answers, counts running
`agentflow=1` containers before stopping, and never starts or stops a host daemon on Linux.

### opencode isolation

The stage directory `<data_dir>/stages/<id>/` holds `prompt.md`, the frozen config at
`xdg/opencode/opencode.json`, `events.jsonl` and a fresh `home/`. The adapter runs the real
opencode binary with `--pure`, `XDG_CONFIG_HOME=<stage>/xdg`, `OPENCODE_DISABLE_PROJECT_CONFIG=1`
and a scrubbed environment. `OPENCODE_CONFIG` and `OPENCODE_CONFIG_DIR` are never set. The
prompt goes in on **stdin** with no message argument (file and argv delivery truncate large
prompts). Provider keys appear in the config only as `{env:AGENTFLOW_KEY_<NAME>}`; the value
is never written to disk. A stage keeps the config it started with.

## 8. Planning loop and queue approval

```
FRD (approved) -> planner -> architect -> critic -> [revise round] -> plan_ready -> human approves
                                                                        |
                                                  compile (compiler) -> queue_draft -> human approves hash
```

- **Roles.** Each is its own `agentflow.plan` stage with its own candidate list. Pairwise
  distinct models are preferred; where that is impossible a warning is recorded.
- **Critic is blind to the architect.** The critic's prompt contains only the FRD and the plan
  and tells it that it has not seen another review. A revising planner sees the previous plan
  and both reviews.
- **Rounds.** The loop runs up to `plan_max_rounds` (default 5). If it ends without both
  reviewers approving, the effort is still `plan_ready` and flagged `not_critic_approved`, and
  the UI shows the best revision. Every plan and review is an immutable document revision.
- **Deliberate mode** (per effort): the planner must add exactly three pre-mortem scenarios
  and a test plan. A plan without them fails validation and goes to the one corrective
  follow-up. The critic checks both.
- **Compile.** Approving the plan starts the compiler by itself; `POST .../compile` recompiles. Replies are strictly parsed: unknown
  fields (including `verify`), cycles, dangling dependencies and absolute paths are rejected.
  The compiler emits no executable content; verification commands come from the project.
- **Hash pinning.** The queue gets a SHA-256 hash over its canonical form. Approving takes the
  hash you reviewed (`POST .../queue/approve {"hash": ...}`); a different hash is a conflict.
  Every task stage records the approved hash, and a stage whose hash no longer matches is
  closed `discarded` rather than run. Editing a task returns the effort to `queue_draft`.
- **Corrective follow-up.** A model reply that fails strict parsing gets one corrective
  message in the same session. A second failure is `infra:unparseable`.

## 9. Budgets, cleanup and plugins

- **Budgets** are per effort: `budget_tokens` and `budget_seconds` (0 means none), set through
  `PATCH /api/projects/{slug}/efforts/{id}`. Tokens come from runner usage; seconds are summed
  stage runtime. At 90% of either limit the dispatcher starts no new attempt (in-flight
  attempts finish). At 100% the project run is stopped with reason `budget`. That stop
  abandons open attempts, which never count against a task. Starting a run on an effort whose
  budget is used up is refused.
- **Cleanup** (per project, `cleanup_enabled`): after a passing review the engine commits the
  reviewed tree as a checkpoint, a `cleanup` stage edits the worktree and verify runs again.
  If the cleanup stage or that verify fails, the engine resets to the checkpoint and carries
  on with the reviewed version. Cleanup cannot cost a task its pass.
- **Plugins** (per project): a list of `{name, version}` pinned to an exact semantic version
  (no `latest`, no ranges). A project with plugins runs its stages without `--pure`; a
  project without them always runs with `--pure`.

## 10. Run control, YOLO and backend health

### Run control

Run state is in memory per project: `stopped`, `running`, `pausing`, `paused`. It is never
restored at boot (RC1): every project starts `stopped`, and the last persisted state is shown
only as "was running before restart".

| Control | Effect |
|---|---|
| `start` | Dispatcher may run the project. Needs an approved queue. Resumes a pausing or paused project. |
| `pause` | No new dispatch; becomes `paused` once in-flight work drains. |
| `stop` | Cancels running stages (waits up to 15 s, then closes them), abandons open attempts. |
| `run-one` | Starts one task's next attempt, then pauses. Only from `stopped` or `paused`. |

Routes: `POST /api/projects/{slug}/run/start`, `/pause`, `/stop`, `/run-one`.

### YOLO

YOLO is a per-effort switch that stands in for the human clicks after the FRD: plan approval
(only when both reviewers approved the plan), starting compile, queue approval, and starting
the run. You still approve the FRD. Each automatic step records the actor `yolo`. Enabling it needs `confirm: true`. It adds no
capability: sandbox, git policy, model scope and paid opt-in code never consult it (a test
checks the YOLO sources do not call them). It takes effect at the next gate. It gives a task
that becomes `stuck` or `exhausted` one automatic clean-worktree retry. It never starts a run
after a restart. The list of rules it cannot relax is `GET /api/yolo/invariants`.

### Backend health and retire

An unreachable backend is skipped for `backend_cooldown` (in memory; a restart forgets it).
Retiring a backend (`POST /api/backends/{name}/retire`) pauses and hides its lane, closes its
queued stages `infra:rerouted` (no strike) and resubmits them elsewhere. With
`cancel_running` it also cancels running stages, abandons the attempts and redispatches the
tasks. Responses carry warnings when a running project is affected.

### Halted-task actions

| Action | Route | Effect |
|---|---|---|
| retry | `POST .../tasks/{key}/retry` `{clean}` | Resets the task (new epoch). `clean` also deletes the task worktree and branch. |
| edit | `PUT .../tasks/{key}` | Changes the task definition; effort returns to `queue_draft`. |
| override | `POST .../tasks/{key}/override` `{reason}` | Marks the task `overridden`; dependents may proceed. |
| abandon effort | `POST .../efforts/{id}/abandon` | Ends the effort. |

Retry and override require a halted task (409 otherwise). Every action is audited.

## 11. Persistence and events

### SQLite

One database at `<data_dir>/agentflow.db`. Migrations are append-only; opening a database
newer than the build is refused.

| Migration | Adds |
|---|---|
| v1 | `events` (audit log: actor, action, subject, detail) |
| v2 | `settings`, `backends`, `backend_models`, `projects`, `project_limits`, `role_candidates` |
| v3 | `efforts`, `documents` (FRD, plan and review revisions), `approvals`, `queues`, `tasks`, `task_deps`, `attempts`, `stages`, `infra_retries`, `overrides`, `task_resets`, `progress_log`, `baselines` |
| v4 | setting `backend_cooldown` |
| v5 | `backend_models.max_concurrent` |
| v6 | `cleanup` stage role (rebuilds `stages`), `projects.cleanup_enabled` |
| v7 | `stages.tokens`, `efforts.budget_tokens`, `efforts.budget_seconds` |
| v8 | `project_plugins` |

Settings with defaults: `max_attempts` 5, `stuck_threshold` 3, `max_infra_retries` 5,
`plan_max_rounds` 5, `worker_timeout` 30m, `review_timeout` 15m, `plan_timeout` 30m,
`verify_timeout` 15m, `verify_slots` 2, `backend_cooldown` 2m. A project may override the
first eight (not `verify_slots` or `backend_cooldown`). Every write records its actor in the
same transaction. The only files outside the database are stage artifacts
(`<data_dir>/stages/<id>/`), task markers (`<data_dir>/tasks/<id>/`) and worktrees.

### Event stream

`GET /api/events` is Server-Sent Events with event name `agentflow`. The first frame is
`{"type":"snapshot","projects":[...]}`. Later frames are notices: `project.run_state`,
`project.update`, `project.delete`, `effort.update`, `task.update`, `backend.update`,
`candidates.update`, `settings.update` and `state.changed`. They are hints to refetch, not
data. The snapshot is computed before subscribing, so an event in that gap can be missed;
clients refetch on `state.changed`. `GET /api/stages/{id}/stream` proxies one stage's live
output from the lane (409 if the stage has not started). Subscribers are capped.

## 12. Security posture

AgentFlow is for one trusted user on a trusted machine. The FRD's safety requirements are
implemented and tested: sandboxed model stages, no credentials in reach, engine-owned git,
no push, protected paths, secrets only in the environment. Beyond that, reviewers logged
these findings and they were **noted, not fixed**:

- The sandbox shares the host network namespace, so a sandboxed agent can reach the server's
  loopback API.
- `sandbox.Spec.Env` blocks nothing case-insensitively and does not stop `GIT_CONFIG_*`
  overrides; callers are trusted.
- opencode permission rules are layer 1 only. `git grep -n TODO` is denied as a side effect
  of the `-O` rule.
- gitsvc: the owner-marker ref for integration overrides is not written atomically with the
  branch (it fails safe); a gitlink-path `reset` error is ignored; each hardened call spawns
  two probe git processes.
- opencode adapter: `Incomplete` is reported only when the final text is empty (a rejected
  ask after earlier text returns stale text); per-step token sums are unverified on
  multi-step streams; the frozen config is copied into the stage home on each run.
- verify: the 5000-file cap is applied after git lists everything; files over 8 MiB pass
  `parse` with a warning; there is a time-of-check gap between the containment check and
  the read.
- API: `PUT .../plan` with body `null` is accepted; project limits `PATCH` accepts strings
  only; task detail embeds full raw stage output for every attempt; the git-init audit row is
  a separate transaction; FRD import is two transactions with a compensating abandon.
- Known races: a task retry resets then removes the worktree (a dispatcher pass in between
  could start an attempt in a doomed worktree); queue approval is read-then-write; one global
  mutex is held across a baseline run.
- macOS sandbox: reads outside the denied paths are allowed, and there is no PID namespace
  (see [macOS](#macos-sandbox-exec)).
- The module has no authentication of its own. It inherits platform auth: list `agentflow`
  in `auth.modules` to require a session or API key.
