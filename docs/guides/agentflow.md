# agentflow

AgentFlow is unified-webapp's orchestrator for coding models. You give it a git repository
and a requirements document (FRD). Models plan the work, you approve the plan and the task
queue, and models then implement each task in an isolated git worktree. The engine verifies
and reviews every change and merges it into an integration branch. You merge that branch
into your own, on your terms.

It is a local, single-user tool. How it works inside is in
[docs/agentflow-design.md](../agentflow-design.md).

---

## Requirements

| Needs | Why |
|---|---|
| Linux with `bwrap` (bubblewrap) and unprivileged user namespaces | Every model stage runs in a bwrap sandbox. There is no unsandboxed mode. |
| `git` on the server's `PATH` | The engine runs all git work. |
| The real `opencode` binary | The model runner. Install it so the binary is at `~/.opencode/bin/opencode`, or on `PATH`. |
| A project repository that is already a git repository | See [Projects](#projects). |

**Use the real opencode binary, not a shim.** At startup AgentFlow looks for
`~/.opencode/bin/opencode`, then for `opencode` on `PATH`. A path that contains `/.agents/`
is refused, because wrapper shims there break once the sandbox replaces `HOME`. If no binary
is found, `GET /api/health` reports `degraded` and the opencode error. The module still
serves its data, but no stage can run.

The server user runs the models, so run the server as the account that owns the
repositories you will point it at.

---

## Configuration

The `agentflow` section of the server config has two keys
(`internal/platform/config.AgentflowConfig`):

```json
"agentflow": {
    "static_dir": "./web/agentflow",
    "data_dir":   "./data/agentflow"
}
```

| Key | Default | Meaning |
|---|---|---|
| `static_dir` | `./web/agentflow` | Directory with the built frontend, served at `/`. |
| `data_dir` | `./data/agentflow` | Everything AgentFlow writes: the SQLite database, worktrees, stage directories. Created (mode `0750`) at startup. Use an absolute path in production. |

Everything else (backends, models, role candidates, limits, projects) is stored in the
database under `data_dir` and edited in the UI or API. Back up `data_dir/agentflow.db` to
keep it.

Layout under `data_dir`:

```
agentflow.db                              SQLite database (all state)
worktrees/<project>/effort-<id>/<task>    task and integration worktrees
stages/<stage-id>/                        prompt, frozen runner config, events, private home
tasks/<task-id>/                          small per-task bookkeeping markers
```

**Migration from the old module.** Older builds had `project_path`, `prompt_dir`,
`default_profile`, `default_timeout_secs` and `profiles` keys. They are ignored now, so you
can delete them. The old `.agentflow/` JSON files and the `/opt/agentflow/` scripts are not
read by anything.

### Route a hostname

Like every module, AgentFlow is reached by `Host` header. Add it to `host_routing`:

```json
"host_routing": {
    "agentflow.example.net": "agentflow"
}
```

Reverse-proxy and TLS notes are the same as for other modules; see
[Production: HAProxy Configuration](../../README.md#production-haproxy-configuration).
A proxy that rewrites `Host` breaks dispatch.

### Authentication

The module has no login of its own. List `agentflow` in `auth.modules` to require a session
or API key; otherwise anyone who can reach the host can use it. Writes are recorded with the
authenticated name (`anonymous` when auth is off).

### API keys for hosted backends

A backend that needs a key reads it from the **server's environment**, never from the
database or the UI. The variable name is `AGENTFLOW_KEY_` plus the backend name in upper
case, with `-` replaced by `_`:

| Backend name | Environment variable |
|---|---|
| `openrouter` | `AGENTFLOW_KEY_OPENROUTER` |
| `my-host` | `AGENTFLOW_KEY_MY_HOST` |

With systemd, add it to the unit (or an `EnvironmentFile`) and restart:

```ini
[Service]
Environment=AGENTFLOW_KEY_OPENROUTER=sk-...
```

The set of keys is read **once at boot**. Adding or changing a key needs a restart. Only
`AGENTFLOW_KEY_*` variables are passed into the sandbox, and the generated config refers to
them as `{env:NAME}` instead of holding the value.

---

## First run

1. Start the server. The `agentflow` tabs are **Overview**, **Project**, **Settings** and
   **Backends**; inside a project: **FRD**, **Plan**, **Queue**, **Board**, **History**.
2. Check `GET /api/health`. `{"status":"ok"}` means opencode was found.
3. Add a backend and role candidates ([next section](#backends-and-role-candidates)).
4. Create a project ([Projects](#projects)).
5. Create an effort and work it through the steps in [Efforts](#efforts).

Every project starts **stopped** after every restart. Nothing runs until you press Start.

---

## Backends and role candidates

A **backend** is a model host: a name, a host URL, an API type, how many stages may run on it
at once (`slots`) and which models it offers.

| Field | Values |
|---|---|
| `name` | Lower-case id (`[a-z0-9][a-z0-9_-]*`). Not `verify` or `merge`. |
| `host` | Base URL of the model server. |
| `api_type` | `ollama`, `openai_compatible` or `anthropic`. |
| `hosted` | `true` for a remote service. Hosted backends are never used under model scope `local`. |
| `paid` | `true` if the backend bills per use. |
| `slots` | Concurrent stages, at least 1. |
| `models` | List of `{model, paid, max_concurrent}`. `max_concurrent` is a per-model cap below the slots (0 = no extra cap). |

Create and manage them in the **Backends** tab or with `POST /api/backends`,
`PATCH /api/backends/{name}`, `POST /api/backends/{name}/retire` (optionally cancelling
running stages) and `POST /api/backends/{name}/enable`.

A **role candidate list** names the backend and model to try, in order, for each role:
`worker`, `reviewer`, `planner`, `architect`, `critic`, `compiler`. Set the global lists with
`PUT /api/candidates/{role}` and override them for one project with
`PUT /api/projects/{slug}/candidates/{role}`.

Rules the dispatcher applies:

- **Paid models are opt-in.** A model flagged paid is used only if you list it as a
  candidate for the role. The Backends tab shows this as an opt-in checkbox.
- **Model scope.** A project is `local` (default) or `any`. Under `local`, no stage is sent
  to a hosted backend.
- **Reviewer differs from worker.** A task is not started unless a reviewer on a different
  model is available. Planner, architect and critic also prefer different models; where that
  is impossible the plan shows a warning.
- **Unreachable backends are skipped** for `backend_cooldown` (default 2 minutes).
- If nothing is eligible, the task shows a *waiting* reason instead of failing.

Model statistics are at `GET /api/stats/models`.

---

## Projects

A project links a name (slug) to a repository on the server's disk.

- `repo_path` must be an **absolute path to an existing git repository**.
- **Protected paths.** A repository that is, contains, or sits inside the server's working
  directory, the server executable's directory or `data_dir` is refused. In particular you
  cannot register unified-webapp's own checkout.
- **Not a git repository yet?** The UI offers to initialise it. `POST /api/projects/git-init`
  with `{"repo_path": "...", "confirm": true}` runs `git init --initial-branch=main` and makes
  an empty first commit, then you can create the project. It refuses a directory that is
  already inside a repository.
- AgentFlow needs at least one commit, because worktrees branch from it.

Project settings (`PATCH /api/projects/{slug}`):

| Setting | Meaning |
|---|---|
| `model_scope` | `local` (default) or `any`. |
| `feedback_cmds` | Commands that verify each task, as strings split on whitespace. No quoting, no shell, no expansion: wrap anything fancy in a script in your repo. When empty, AgentFlow detects: `go build/vet/test ./...` for `go.mod`, `python -m pytest -q` for `pyproject.toml`, `npm test --silent` when `package.json` has a test script. |
| `limits` | Per-project overrides of the global limits (below). Blank inherits. |
| `cleanup_enabled` | Turn on the [cleanup pass](#cleanup). |
| `plugins` | [Opencode plugins](#plugins). |

Feedback commands are the only commands run to verify work. They come from you, never from a
model: compiled queues containing a `verify` field are rejected.

### Limits

Global settings are at `GET/PUT /api/settings`; a project overrides eight of them at
`GET/PUT /api/projects/{slug}/limits`.

| Key | Default | Meaning |
|---|---|---|
| `max_attempts` | 5 | Counting attempts per task before it is `exhausted`. |
| `stuck_threshold` | 3 | Consecutive failures with the same fingerprint before `stuck`. Must be at most `max_attempts`. |
| `max_infra_retries` | 5 | Infrastructure failures before `needs_attention`. 0 means unlimited. |
| `plan_max_rounds` | 5 | Planner, architect, critic rounds. |
| `worker_timeout` | 30m | Per worker stage. |
| `review_timeout` | 15m | Per review stage. |
| `plan_timeout` | 30m | Per planning stage. |
| `verify_timeout` | 15m | Per verification (and each feedback command). |

Global only: `verify_slots` (default 2), `backend_cooldown` (default 2m). Durations are Go
strings such as `30m0s`.

The settings response also lists `opencode_path`, but the binary is chosen at startup as
described under [Requirements](#requirements); changing `opencode_path` does not move it.

---

## Efforts

An **effort** is one run from FRD to merged code in a project. A project has one active
effort at a time. Create one in the **Project** tab or with
`POST /api/projects/{slug}/efforts`.

```
FRD -> plan -> approve plan -> compile -> approve queue -> run controls -> complete
```

### 1. FRD

Write the requirements in the **FRD** sub-tab (`PUT /api/projects/{slug}/efforts/{id}/frd`),
or import an existing one with `POST /api/projects/{slug}/efforts/import` (the body is FRD
JSON; it is strictly validated). Every save is a numbered revision
(`GET /api/projects/{slug}/efforts/{id}/frd/revisions`). Approve it with
`POST /api/projects/{slug}/efforts/{id}/frd/approve`. **You always approve the FRD yourself.**

### 2. Plan

`POST /api/projects/{slug}/efforts/{id}/plan/generate` starts the planning loop: a planner
drafts, an architect and a critic review (the critic never sees the architect's review), and
the planner revises, for up to `plan_max_rounds`. Read the rounds with
`GET /api/projects/{slug}/efforts/{id}/plan/rounds`. When the loop ends the effort is
**plan_ready**. If the reviewers did not both approve, the plan is flagged and you decide
whether to edit it (`PUT .../plan`), regenerate it, or approve it anyway.

Approve with `POST /api/projects/{slug}/efforts/{id}/plan/approve`.

### 3. Compile and approve the queue

Plan approval does not compile. Press **Compile** (or
`POST /api/projects/{slug}/efforts/{id}/compile`). The compiler turns the plan into tasks with
dependencies, acceptance criteria and touched files. Review them in the **Queue** sub-tab
(`GET /api/projects/{slug}/efforts/{id}/queue`).

The queue has a hash. Approving pins that hash:
`POST /api/projects/{slug}/efforts/{id}/queue/approve` with `{"hash": "<hash>"}`. If the queue
changed since you looked, the approval is refused. Editing any task
(`PUT /api/projects/{slug}/efforts/{id}/tasks/{key}`) returns the queue to draft for another
approval. After approval, no stage runs against a different queue.

### 4. Run controls

Controls are on the **Board** sub-tab and apply to the project:

| Control | Route | Effect |
|---|---|---|
| Start | `POST /api/projects/{slug}/run/start` | Dispatch tasks. Needs an approved queue. Resumes a paused run. |
| Pause | `POST /api/projects/{slug}/run/pause` | Start nothing new; becomes paused when in-flight work finishes. |
| Stop | `POST /api/projects/{slug}/run/stop` | Cancel running stages now. Interrupted attempts do not count against the task. |
| Run one | `POST /api/projects/{slug}/run/run-one` | Run one task attempt, then pause. From stopped or paused. |

Watch progress on the Board, in live stage output
(`GET /api/stages/{id}/stream`, Server-Sent Events), and in History. The overview stream is
`GET /api/events`.

### Task states and halted tasks

| Status | Meaning |
|---|---|
| `blocked` | Waiting on a dependency. |
| `ready` / `running` / `repair` | Normal flow. A failed attempt becomes `repair` and is retried. |
| `passed` | Reviewed and merged. |
| `overridden` | You accepted it by hand. |
| `stuck` | Same failure repeated `stuck_threshold` times. |
| `exhausted` | `max_attempts` used. |
| `needs_input` | The worker or reviewer says it needs a human decision. |
| `needs_attention` | Too many infrastructure failures or git policy violations. |

The last four are **halted**. Only that task and its dependents wait; the rest of the queue
keeps going. Act on a halted task from the Board:

| Action | Route | Effect |
|---|---|---|
| Retry | `POST /api/projects/{slug}/efforts/{id}/tasks/{key}/retry` `{"clean": false}` | Fresh attempt counters. `clean: true` also throws away the task's worktree and branch. |
| Edit | `PUT /api/projects/{slug}/efforts/{id}/tasks/{key}` | Change the task, then re-approve the queue. |
| Override | `POST /api/projects/{slug}/efforts/{id}/tasks/{key}/override` `{"reason": "..."}` | Mark it done without a pass so dependents can run. |
| Abandon effort | `POST /api/projects/{slug}/efforts/{id}/abandon` | End the whole effort. |

Retry and override only work on halted tasks.

### 5. Finish: merge it yourself

When every task is done the effort is **complete**. Its integration branch is
`agentflow/effort-<id>` (task branches are `agentflow-task/<effort>/<task>`). AgentFlow
never pushes or merges into your branch. The UI and
`GET /api/projects/{slug}/efforts/{id}/git-commands` give you commands to review and merge,
roughly:

```
cd '/path/to/repo'
git log --oneline 'main'..'agentflow/effort-1'
git switch 'main'
git merge --no-ff 'agentflow/effort-1'
git push origin 'main'
```

Read the log first. Run the commands yourself.

---

## Optional features

### YOLO

Turn on per effort (`POST /api/projects/{slug}/efforts/{id}/yolo` with
`{"on": true, "confirm": true}`). It clicks the gates after the FRD for you: it approves the
plan when both reviewers approved it, compiles, approves the queue and starts the run. Auto
steps are recorded as actor `yolo`. It also gives each stuck or exhausted task one automatic
clean retry.

YOLO relaxes nothing else. Sandboxing, the git rules, local model scope and paid opt-in all
stay on. `GET /api/yolo/invariants` lists them. Turning YOLO off applies at the next gate. A
restart never starts a run, YOLO or not.

### Budgets

Set per effort with `PATCH /api/projects/{slug}/efforts/{id}` using `budget_tokens` and
`budget_seconds` (0 = none). At 90% of either limit no new task attempt starts. At 100% the
run is stopped with the reason `budget`. Tokens are as reported by the model runner; seconds
add up stage runtime across parallel stages.

### Cleanup

Set `cleanup_enabled` on the project. After a task passes review, a model tidies the change
(removes dead code, debug leftovers and redundancy, without changing behavior) and verification runs again. If the cleanup or its
verification fails, the reviewed version is kept. It cannot cost a task its pass.

### Plugins

A project can opt into opencode plugins: `plugins` is a list of `{"name": "...",
"version": "1.2.3"}`. The version must be exact, with no `latest` and no ranges. A project
with no plugins always runs opencode with `--pure`, so no plugin from your own opencode
configuration is ever loaded.

### Deliberate mode

Create an effort with `"deliberate": true` (or `PATCH` it) for risky work. The planner must
add three pre-mortem scenarios and a test plan, and the critic checks them. The UI suggests it
when the FRD text mentions things like auth, migration or PII.

### Export and import

| Route | Result |
|---|---|
| `GET /api/efforts/{id}/export?format=json` | The whole effort. `format=md` renders Markdown. |
| `GET /api/efforts/{id}/frd/export` | The FRD (`?format=`, `?revision=`). |
| `GET /api/efforts/{id}/plan/export` | The plan (`?format=`, `?revision=`). |
| `POST /api/projects/{slug}/efforts/import` | Create an effort from FRD JSON. |

---

## What agentflow never does

- **Push.** There is no push code path. You get commands as text.
- **Edit your checked-out branch or main working tree.** All changes happen on `agentflow/`
  and `agentflow-task/` branches in worktrees under `data_dir`.
- **Store state in your repository.** No dotfiles, no JSON, nothing committed except the
  work itself on agentflow branches.
- **Run model-written commands outside the sandbox.** Models run in bwrap with no SSH
  agent, no `~/.ssh`, no git credentials and no access to your home or the server's data.
- **Run unverified feedback commands.** Verification commands come only from your project
  settings or file-based detection. A model can propose nothing executable that the engine
  will run.
- **Start a run by itself.** Every project is stopped after every restart.

---

## Troubleshooting

| Symptom | Check |
|---|---|
| `GET /api/health` says `degraded` | The `opencode` field gives the error. Install the real binary at `~/.opencode/bin/opencode` or on `PATH`, then restart. |
| Stages fail immediately as `infra:crash` | Run `bwrap --version`. Check that unprivileged user namespaces are enabled. |
| Project creation says the path is protected | The repo is the server's working directory, executable directory or `data_dir` (or contains/is inside one). Use another repository. |
| Project creation says it is not a git repository | Run `git init` and make a first commit, or use the UI offer to initialise it. |
| A hosted backend fails with an auth error | Export `AGENTFLOW_KEY_<NAME>` for the server process and restart. Names use `_` for `-`. |
| Task shows a waiting reason | No eligible candidate: check role candidates, model scope (`local` skips hosted backends), paid opt-in, and whether a second model exists for review. |
| A backend was marked unreachable | It is skipped for `backend_cooldown` (default 2 minutes). Check the host URL; retire or re-enable it on the Backends tab. |
| Run won't start | The queue must be approved, and the effort's budget not used up. |
| Run stopped by itself with reason `budget` | Raise the effort's budgets and start again. |
| Task is `needs_attention` | Infrastructure failures or git policy violations: read the task's History, fix the cause, then Retry. |
| `409` on retry or override | The task is not halted. |
| `409` on git commands | The effort is not complete yet. |
| After a restart, a project shows "was running" | That is a memory aid. Press Start. |
| Verification fails on a clean checkout | The baseline is recorded before the first task. Failures that already existed there are not blamed on a task; fix or remove the failing feedback command. |
