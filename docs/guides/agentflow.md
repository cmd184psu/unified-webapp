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
| Linux with `bwrap` (bubblewrap) and unprivileged user namespaces, **or macOS** (uses the built-in `/usr/bin/sandbox-exec`) | Every model stage and verify command runs sandboxed. There is no unsandboxed mode. See the design doc, [Sandbox](../agentflow-design.md#7-sandbox), for how the macOS sandbox differs. |
| `git` on the server's `PATH` | The engine runs all git work. |
| The real `opencode` binary | The model runner. Install it so the binary is at `~/.opencode/bin/opencode`, or on `PATH`. |
| A project repository that is already a git repository | See [Projects](#projects). |

**Use the real opencode binary, not a shim.** With `opencode_path` at its default, AgentFlow looks (at every stage and probe) for
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

A hosted backend's key is stored in the agentflow database and applies to the next stage
without a restart. Set it in the **Backends** tab or with the write-only `api_key` field of
`POST /api/backends` and `PATCH /api/backends/{name}`: a string sets it, an empty string clears
it, leaving the field out keeps the stored key. Responses never contain the key, only
`has_key`.

At each stage the key is resolved as the stored key, else the server's environment variable
`AGENTFLOW_KEY_` plus the backend name in upper case with `-` replaced by `_`. The environment
variable is read when the stage starts, not at boot:

| Backend name | Environment variable |
|---|---|
| `openrouter` | `AGENTFLOW_KEY_OPENROUTER` |
| `my-host` | `AGENTFLOW_KEY_MY_HOST` |

The stage's sandbox gets only that one variable, for its own backend. The generated config
refers to it as `{env:NAME}` and never holds the value; the key does not appear in prompts,
logs, exports, events or API responses. Keys are stored in plain text in
`data_dir/agentflow.db`, so protect that file like the environment file it replaces.

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
| `api_type` | `ollama`, `openai_compatible`, `anthropic` or `opencode`. `opencode` is opencode's own built-in provider: no host, no key, models addressed as `opencode/<model>` (Probe lists them, the free ones among them). Hosted: prompts leave the machine. |
| `hosted` | `true` for a remote service. Hosted backends are never used under model scope `local`. |
| `paid` | `true` if the backend bills per use. |
| `slots` | Concurrent stages, at least 1. |
| `models` | List of `{model, paid, max_concurrent, context_tokens, tier}`. `tier` rates the model: 1 strong, 2 capable, 3 light, 0 not rated (counts as 2). A light model is never picked for the planner, architect, critic or compiler role (the skip reason says so), and a reviewer is never weaker than the worker whose work it checks. A reviewer from another model family than the worker is preferred, then another model of the same family; an attempt reviewed within the worker's family is flagged in the task view. `tools` says whether the host reports that the model supports tool calls, which every role needs: `yes`, `no`, or empty when not known. The server sets it from the host's own answer (Ollama's model details list a `tools` capability) and ignores any value you send. A backend cannot be saved with a model whose host says `no` (the error names the model), the probe reports it so the form can warn before you save, a model that turns out to lack it after an install is not added, a model stored as `no` cannot be put in a candidate list, and the dispatcher skips one with the reason. A host that cannot be reached leaves the value empty rather than blocking a save, and the service asks its Ollama hosts about empty values once at start-up. `max_concurrent` is a per-model cap below the slots (0 = no extra cap). `context_tokens` is the model's context window (0 = unknown); it feeds the [size check](#size-and-splitting-a-large-frd). |

Create and manage them in the **Backends** tab or with `POST /api/backends`,
`POST /api/backends/probe` (lists the models a host serves, so the form can offer them; for an
Ollama host the answer also has `contexts`, a map of model name to the context window the host
states, leaving out any model that does not answer),
`PATCH /api/backends/{name}`, `POST /api/backends/{name}/retire` (optionally cancelling
running stages) and `POST /api/backends/{name}/enable`, and `DELETE /api/backends/{name}`: a backend is deleted only after it is disabled and no candidate list, global or project, still names it (otherwise 409 with the reason); its models and model checks go with it and stage history keeps the name.

A **role candidate list** names the backend and model to try, in order, for each role:
`worker`, `reviewer`, `planner`, `architect`, `critic`, `compiler`. Set the global lists with
`PUT /api/candidates/{role}` and override them for one project with
`PUT /api/projects/{slug}/candidates/{role}`.

Rules the dispatcher applies:

- **Paid models are opt-in.** A model flagged paid is used only if you list it as a
  candidate for the role. The Backends tab shows this as an opt-in checkbox.
- **Model scope.** A project is `local` (default) or `any`. Under `local`, no stage is sent
  to a hosted backend.
- **Minor findings never block.** A review that asks for repair but lists only minor issues
  counts as a pass; the notes stay in the stored review.
- **Reviewer differs from worker.** A task is not started unless a reviewer on a different
  model is available. Planner, architect and critic also prefer different models; where that
  is impossible the plan shows a warning.
- **Unreachable backends are skipped** for `backend_cooldown` (default 2 minutes).
- If nothing is eligible, the task shows a *waiting* reason instead of failing.

Model statistics have their own **Stats** tab and are at `GET /api/stats/models` (add `?project=<slug>` to limit them to one project). Each row is a backend, model and role: stages, passes, failures, infra failures (left out of the pass rate), average and total time, tokens, the model's tier and family, and for workers the attempts that passed, and its strikes. Use it to decide a model's tier.

**Strikes and the bench.** A model gets a strike for output the engine could not read (`infra:unparseable`), for breaking the git policy, and for failing one task the same way twice in a row (the same fingerprint, same worker). Strikes count against a model in one role, and it makes no difference whether the backend is local, free or paid. They are a streak, like failed password attempts: a success by that model in that role (a worker's attempt that passed, or a planning or review stage that passed) resets the count to zero, and only three in a row bench it. Separately, a model that has failed a task the same way twice in a row is passed over for that task's next attempt, so another model gets a turn before the task can be declared stuck; if no other model could ever serve, the repeat is allowed so the task still reaches stuck. A benched model is skipped by the dispatcher with the reason "benched"; the Stats tab shows the streak (and the lifetime total), a benched badge and a **Clear** button (`POST /api/strikes/clear` with `{backend, model, role}`), which starts the streak again. A host that does not answer is never a model's fault: an `infra:unreachable` stage makes no strike, and a timeout, a merge conflict or a refused commit never counts as a repeat (only a repeated verify or review failure does). Nothing is ever deleted: `GET /api/strikes` (optional `project`, `model`, `limit`) returns the history newest first, each strike with its kind, project, effort, task, time, whether it still counts and why not (`success` or `cleared`), plus per-day counts per model, role and kind, and the Stats tab shows the same under Strike history.

**Configuration file.** The Backends tab ends with a *Configuration file* section. **Export configuration** downloads `GET /api/config/export`: the backends with their models, tiers and tool flags, the global candidate list of each role, and the limit settings. The file holds no projects, no efforts and no API keys. To import one, choose the file: the panel asks the server for a dry run and lists what would change (backends created or updated with their model lists replaced, candidate lists replaced, settings set), and nothing is written until you click **Apply import** (`POST /api/config/import` with `{config, apply}`; `apply: false` is the dry run). The change is live; no restart is needed.

An unreadable reply from a worker (no JSON object, or JSON the schema rejects after the one corrective follow-up) fails that attempt, counts as a strike and sends the task's next attempt to another model; it does not use the task's infrastructure-retry budget. Only incoherent text (empty, a few words, symbols, a loop) skips the follow-up.

---

### The Grok CLI backend

A backend of api type `grok` runs stages through the `grok` command on your own Grok plan, so no key is stored anywhere in agentflow and no login is copied: Grok uses its own login (`grok login`). It has no host. Add the backend, press the probe button to list the models your login offers (`grok models`), then name them on the candidate lists. Mark it hosted and paid only if your plan counts that way; a paid model runs only where a candidate list names it.

Each stage runs `grok --prompt-file ... --output-format streaming-json --sandbox <profile>` in the task worktree, with sub-agents and web search off. Grok's own sandbox confines it: roles that write (worker, cleanup) use the profile `agentflow` (write access to the worktree), every other role `agentflow-ro` (read-only). Both profiles must exist in `~/.grok/sandbox.toml`; agentflow does not write that file. Use:

```toml
[profiles.agentflow]
extends = "workspace"
deny = ["~/.ssh", "~/.local/share/opencode/auth.json", "~/.unified_common.pin", "**/.env"]

[profiles.agentflow-ro]
extends = "read-only"
deny = ["~/.ssh", "~/.local/share/opencode/auth.json", "~/.unified_common.pin", "**/.env"]
```

Grok's own `auth.json` has to stay readable inside its sandbox, so a model that went looking could read it; that is the same exposure as using `grok` yourself. Every Grok call carries a large built-in prompt (about 50,000 input tokens for one word), so it uses plan usage quickly: put it on roles where quality matters (reviewers, hard tasks) before cheap per-task work.

### The Codex CLI backend

A backend of api type `codex` runs stages through `codex exec` on your own ChatGPT login (`codex login`), the same way as the Grok CLI backend: no host, no key, no credential copied. The probe button lists the models your login offers. Stages run with `--json`, the prompt on stdin, and Codex's own sandbox through a permissions profile built for each call: `:workspace` for roles that write (worker, cleanup), `:read-only` for the rest, never asking for approval. The profile denies the model's tools any read of `~/.ssh`, opencode's `auth.json`, the service PIN file, Grok's and Codex's own `auth.json` (Codex reads its login outside the sandbox, so this costs nothing) and every `.env` file under the workspace. Only paths that exist are listed, and a symlinked one is denied at its real path. Codex also loads your global instructions into every call, which adds to the usage each stage costs.

### The Claude Code and Muse CLI backends

A backend of api type `claude` runs stages through `claude -p` on your own Claude login (no host, no key, no credential copied). The probe lists the models agentflow offers on it (Haiku, Sonnet, Opus 5.5); add only the ones you want, and put a paid-plan model on a candidate list only when you mean to use it. Each call gets Claude Code's own sandbox, turned on and not escapable, with `~/.ssh`, opencode's `auth.json`, the service PIN, the Grok, Codex and Claude logins and every `.env` denied to its shell commands and to its Read tool. A path that exists is hidden (it looks empty), so a model cannot tell what is there. Workers get the edit tools and may write the worktree; every other role gets read-only tools and a sandbox that cannot write the worktree. Your own Claude settings, hooks and CLAUDE.md are not loaded (`--setting-sources local`), so a stage costs about 3,000 tokens of fixed prompt instead of 24,000.

A backend of api type `muse` runs stages through `muse exec` on your own Muse login, with `--approval-mode never` and Muse's own shell sandbox, and `--disable-write` for every role but worker and cleanup. **Known gap:** Muse offers no setting to deny reads of chosen paths (its own documentation and the CLI show none), so a model on this backend can read anything you can read, including `~/.ssh` and `.env` files. Use it for work that does not touch secrets, or not at all if that bothers you. The backend lists one model, `muse-spark-1.3-contributor`.

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
| `definition_of_done` | Optional list of lines that replaces the [derived definition of done](#definition-of-done). `null` (or an empty list) goes back to the derived one. |
| `cleanup_enabled` | Turn on the [cleanup pass](#cleanup). |
| `plugins` | [Opencode plugins](#plugins). |

### Unregister a project

`DELETE /api/projects/{slug}` (the trash button in Edit project and on each Overview row)
unregisters a project. It is allowed only when the run is **stopped** and the project has
**no active effort**; otherwise it answers 409 and says which ("stop the run", or "finish or
abandon the active effort first").

- **Removed:** the project's AgentFlow records, in one transaction: all its efforts and
  everything under them (FRDs, plans and approvals, queues, tasks, attempts, stages,
  baselines, resets, retries, progress notes, FRD parts), its limit overrides, plugins and
  role candidates. Afterwards its working files under `data_dir` are removed too
  (`worktrees/<slug>`, the stage, task and baseline folders), and stale worktree
  registrations in the repository are pruned. Audit events stay; one `project.delete` event
  is added. The slug can be registered again later with a clean slate.
- **Not touched:** your repository, its files and its branches. AgentFlow never deletes
  your branches, so the answer lists the ones still there:
  `{"deleted": "slug", "branches": [...], "commands": ["git -C <repo> branch -D <branch>", ...]}`.
  The UI shows the commands with copy buttons; run them yourself if you want the branches gone.

### Definition of done

Every task is held to one standard, shown by `GET /api/projects/{slug}/definition-of-done` as
`{"items": [...], "source": "detected" | "project", "toolchains": [...]}`. Detected items are the
commands the engine runs after every task (your `feedback_cmds`, else the commands found for the
repository's toolchain: Go, Python, npm) plus three fixed lines: no files outside the task's scope
change, new code has tests, no secrets are added. A `definition_of_done` set on the project replaces
them. The worker and reviewer prompts carry the list, and the reviewer is told to judge against it.
Because the engine already enforces it, nobody writes "the tests pass" as an acceptance criterion
and the drafter below never does.

Feedback commands are the only commands run to verify work. They come from you, never from a
model: compiled queues containing a `verify` field are rejected.

### Resting models

A hosted model has no usage meter to read. The provider answers "rate limit exceeded" or goes
quiet, so agentflow rests a model when that happens: a rate limit rests it for 5 minutes, a
stall (a hosted model silent past the stall limit) for 3, and each failure in a row doubles the
rest, up to 60 and 20 minutes. Until the rest ends the model gets no new stage and the work goes
to the next candidate; when every candidate for a role is resting, the task waits and says until
when, then carries on by itself. Any stage the model answers clears its rest. A rate limit is not
the task's fault, so it costs no infrastructure retry. A model on a host of your own is never
rested for being slow.

The Backends tab shows a resting model with the time it comes back, and **Wake** ends the rest
now. `GET /api/rests` lists them, and `DELETE /api/rests?backend=&model=` wakes one (204, or 404
when it is not resting). Rests are kept in the database, so a restart does not forget them.

**Hold** (a button on each hosted model) says the model's work should wait for it. Normally a
resting model is passed over and the stage goes to the next candidate; a held model that is resting
ends the candidate list instead, so its stages wait with the reason "resting until ..." and carry on
when the rest ends (the dispatcher's next tick) or you press Wake. Release puts the model back to
being passed over. `GET /api/holds` lists the held models, and
`PUT /api/holds?backend=&model=&hold=true|false` holds or releases one (204). Holds are kept in the
database (migration 28).

### The Antigravity CLI backend (agy)

A backend of api type `agy` runs stages through Google's `agy` in print mode on your own Google login (no host, no key, no credential copied). The probe lists what `agy models` offers (Gemini Flash in three effort levels, Claude Sonnet and Opus 4.6, GPT-OSS 120B); add only the ones you want. Each call gets a per-session `--settings` object that agy never saves: its terminal sandbox on (`--sandbox`, commands that would need to leave it ask, and in headless mode that is a refusal), reads denied for `~/.ssh`, opencode's `auth.json`, the service PIN, the Grok, Codex, Claude and agy logins, and every `.env` or `.env.*` file in the worktree (agy's rules take paths, not globs, so they are listed at the start of each call), and for every role but worker and cleanup writes denied to the worktree. A prompt over 100 KB is too big for an argument, so the model is told to read it from its file, which the settings allow. Anything the model creates later, such as a new `.env`, is not covered.

### BotChat relay

With the **BotChat address** and **BotChat key file** settings both filled in (the settings menu, BotChat section; empty means off, and a change applies at once), agentflow posts what happens into a BotChat service. There is one room per effort, `agentflow-effort-<id>`, made on first use, and events with no effort go to `global`. Agentflow's own code does all the posting, so no model holds the key; the key file is read on every post.

- **Stages** speak under their own callsign, `af-<effort>-<role>[-<task>]` (for example `af-23-worker-T004`): one message when each stage is handed to its model ("T004 attempt 2 · assigned to worker big-pickle (opencode-free)") and one when it ends, with the attempt, model and backend, outcome, minutes and tokens. The ending message carries what the stage said: a reviewer's verdict, summary and issues (severity, text, file), a worker's summary or the reason it is blocked, and the words of a verify or merge stage.
- **The coordinator** (`af-coord`) posts the events worth telling: effort created, run started, paused, stopped or complete, queue approved, a task edited, compile or planning halted, stalled, acceptance passed, effort abandoned.

- **The task board.** When a queue is approved the coordinator makes a BotChat plan for the effort and a task per agentflow task (title, prompt as objective, acceptance criteria, files as exclusions, dependencies), assigned to `af-<effort>-worker-<task>` and promoted to ready. Tasks then move in step with the engine: claimed and started when their worker stage is handed to a model, done when an attempt passes verify and review, then accepted and closed by the coordinator; blocked on the coordinator when an attempt reports it is blocked; the plan is closed when the run completes. Agentflow stays the source of truth: a move BotChat refuses (the task is already there, or someone moved it by hand) is ignored, and a failed move is logged without stopping the chat messages.

**BotChat mode.** In the settings it is the switch "Send work through BotChat", off by default; off is `mirror` and on is `dispatch` (stored as `botchat_mode`). `mirror` (the default) only posts what happens, as above. `dispatch` also hands each stage to an agent through BotChat. The engine still decides what runs when. For each model call it pitches the job to an agent's callsign, one per role, backend and model (`af-worker-grok-grok-4-7`), as a directed message `[job:<stage>] ...`. That agent is a loop of agentflow's own code, not a model: it keeps its callsign alive on BotChat, reads its inbox, and when a job arrives it posts "picked up", reads the latest messages in the effort's room that were sent to it or to everyone, runs the same one-shot model call as before with those messages put in front of the prompt (saved as `prompt.botchat.md` in the stage directory), then posts how the job ended. Because the loop does the reading and the reporting, no model can skip either. An agent stays up while work keeps coming and gives its callsign back after three idle minutes. If BotChat cannot be reached, or nobody picks a job up within 45 seconds, the call runs directly, so a chat service that is down does not stop a run. Model checks and other calls that belong to no stage always run directly.

Switching the relay on starts at the present, so it does not replay history, and it remembers how far it has posted (the `botchat_state` table, migration 30), so a restart carries on. A failed post is logged once and tried again on the next tick (every 5 seconds). The relay only writes to BotChat; it reads nothing back yet.

### Limits

Global settings are at `GET/PUT /api/settings`; a project overrides most of them at
`GET/PUT /api/projects/{slug}/limits`.

| Key | Default | Meaning |
|---|---|---|
| `max_attempts` | 5 | Counting attempts per task before it is `exhausted`. |
| `stuck_threshold` | 3 | Consecutive failures with the same fingerprint before `stuck`. Must be at most `max_attempts`. |
| `max_infra_retries` | 5 | Infrastructure failures before `needs_attention`. 0 means unlimited. |
| `plan_max_rounds` | 5 | Planner, architect, critic rounds. |
| `review_panel` | 1 | Different reviewer models that must each pass a task before it merges, 1 to 3. The first dissent sends the task back. A panel is best effort: when no further reviewer model could serve, the reviews that passed stand. |
| `worker_timeout` | 30m | Per worker stage. |
| `review_timeout` | 15m | Per review stage. |
| `plan_timeout` | 30m | Per planning stage. |
| `verify_timeout` | 15m | Per verification (and each feedback command). |

Global only: `verify_slots` (default 2), `backend_cooldown` (default 2m), `opencode_path`
and `browse_roots`. Durations are Go strings such as `30m0s`.

Every setting is live: saving applies to the next stage or request, with no restart. A new
`verify_slots` resizes the verify lane at once, and a new `opencode_path` re-probes the
version for `GET /api/health` and `GET /api/opencode/version`. `opencode_path` left at
`opencode` finds the binary as described under [Requirements](#requirements); any other value
is used as given.

`browse_roots` is an optional list of absolute directories that confines the directory picker
(default: empty, so any folder can be browsed; the picker starts at the server user's home). `GET /api/fs/dirs?path=/abs/dir` returns
`{"path", "parent", "dirs": [{"name", "path", "git"}]}` (`parent` is empty at a browse root): subdirectories only, sorted by name,
hidden ones left out, `git` true when the directory has a `.git` entry. Without `path` it
starts at the first root. A path that resolves (after symlinks) outside every root is a 400
naming `browse_roots`.

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

You do not have to type the structure. The ids (`R1`, `AC1`, ...), the links from criteria to
requirements (a criterion with none covers every requirement), the status (`draft`), the created date and
the FRD id (`FRD-<effort id>`) are filled in on every save, so an FRD with only an objective,
requirements and criteria is valid (an import takes the same shortcuts as a save). New ids are numbered above
the highest one in the FRD and never reused, so deleting R1 does not let a new requirement take its place. A link
to a requirement that no longer exists is dropped on save; a criterion left with no links covers every requirement.

**Draft from a description.** `POST /api/projects/{slug}/efforts/{id}/frd/draft` with
`{"description": "what you want, in plain words", "polish": false}` runs a planner-role stage and
answers 202. The result is saved as a new, unapproved FRD revision. With `"polish": true` the stage
tidies the existing FRD instead (shorter wording, duplicates merged, the id and constraints kept).
Criteria that only restate the [definition of done](#definition-of-done) (generic wording such as "tests
continue to pass", "go test -race ./... passes", "gofmt reports no diff", "no unrelated files changed") are removed by
code after the reply is read; specific ones such as "no files outside internal/x are changed" stay, and if every
criterion would go they are all kept (the event records `kept_all_restated`). `GET .../frd/draft` gives `{"state": "idle" | "running" | "failed", "started": ms, "error": "..."}`;
a failed draft leaves the FRD as it was. The effort must be a draft with no stage in flight: a second
request while one runs, or a request after the plan has started, is 409.

### Size and splitting a large FRD

A model with a short context cannot plan a large FRD, and a long context costs a lot on a paid model.
`GET /api/projects/{slug}/efforts/{id}/size` estimates the FRD and the plan in tokens (a quarter of
their JSON length) and sets them against a budget: half of the smallest known `context_tokens`
among the models the planner, architect, critic and compiler roles may use for the project (global
lists, or the project's own). It answers `{"frd_tokens", "plan_tokens", "budget_tokens",
"context_tokens_min", "over", "models": [{"backend", "model", "context_tokens"}],
"unknown_models": [{"backend", "model"}], "advice"}`; `over` is only ever true when a context size is known and
is computed from the known sizes only, so `unknown_models` (models with `context_tokens` 0) is there to warn
that the check does not cover them; `advice` names them.

`POST .../frd/split` with an optional `{"max_parts": n}` asks the drafter to cut the FRD into the fewest
ordered, self-contained parts that fit the budget. Part 1 becomes this effort's FRD (a new revision);
parts 2 and up are stored (a later split keeps the parts already stored, started or not, and appends its own after them): `GET .../frd/parts` lists them as
`[{"n", "title", "summary", "tokens", "started_effort"}]`, and `POST .../frd/parts/{n}/start` creates a
new effort from part n, the way an import does. That is a 409 while the project has an active effort,
so finish or abandon this one first, and for a part that was started already.

### 2. Plan

`POST /api/projects/{slug}/efforts/{id}/plan/generate` starts the planning loop: a planner
drafts, an architect and a critic review (the critic never sees the architect's review), and
the planner revises, for up to `plan_max_rounds`. Read the rounds with
`GET /api/projects/{slug}/efforts/{id}/plan/rounds`. When the loop ends the effort is
**plan_ready**. If the reviewers did not both approve, the plan is flagged and you decide
whether to edit it (`PUT .../plan`), regenerate it, or approve it anyway.

Approve with `POST /api/projects/{slug}/efforts/{id}/plan/approve`.

### 3. Compile and approve the queue

Approving the plan starts the compile by itself: the effort goes plan approved, compiling,
queue draft without a second click. If no compiler candidate is available the approval still
stands and the effort waits with a reason (shown in the Plan tab) until one is. A compile that
never started (for example across a restart) is started by the next tick; one that ran and halted
is not retried, so the effort stays at plan approved with the reason and **Compile**
(`POST /api/projects/{slug}/efforts/{id}/compile`) starts it again; it also recompiles. The compiler turns the plan into tasks with
dependencies, acceptance criteria and touched files. Review them in the **Queue** sub-tab
(`GET /api/projects/{slug}/efforts/{id}/queue`).

Tasks that do not depend on each other, directly or through others, run at the same time (up to the
backends' free slots); a task waits only for the tasks it depends on. Two tasks that list the same
file must be ordered by a dependency, and the compiler's reply is rejected when they are not, so
tasks that run side by side never touch the same file and their merges do not conflict. Each task
still works in its own worktree.

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

A worker may find that an earlier task already did the work. It then changes nothing and sets
`no_changes_reason` naming the evidence. If the tree really is unchanged, verify passes with a
warning, the reviewer judges whether the files already meet every acceptance criterion, and a
passing review completes the attempt as passed with no commit and no merge (event
`attempt.no_changes`). Without a reason an unchanged tree still fails `no_changes`.

The last four are **halted**. Only that task and its dependents wait; the rest of the queue
keeps going. Act on a halted task from the Board:

| Action | Route | Effect |
|---|---|---|
| Retry | `POST /api/projects/{slug}/efforts/{id}/tasks/{key}/retry` `{"clean": false}` | Fresh attempt counters. `clean: true` also throws away the task's worktree and branch. |
| Edit | `PUT /api/projects/{slug}/efforts/{id}/tasks/{key}` | Change the task. The queue returns to draft and needs approving again; add `?approve=true` (the board does) and a queue that was approved is approved again under the new hash, and a run that was going carries on. |
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

The same response carries what is being merged, so you can review it before you run
anything: `branch` (the integration branch), `base` (the short sha the effort started from),
`commits` (`[{sha, subject}]` for base..branch, oldest first), `diffstat` (the
`git diff --stat` text) and `files` (the number of changed files). Tool state (`.omc`,
`.claude`) is left out of the diffstat. Read the log first. Run the commands yourself.

---

## Optional features

### YOLO

Turn on per effort (`POST /api/projects/{slug}/efforts/{id}/yolo` with
`{"on": true, "confirm": true}`). It clicks the gates after the FRD for you: it approves the
plan when both reviewers approved it, approves the queue and starts the run. Auto
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
| `GET /api/health` says `degraded` | The `opencode` field gives the error. Install the real binary at `~/.opencode/bin/opencode` or on `PATH`, or set `opencode_path`; saving the setting re-probes at once. |
| Stages fail immediately as `infra:crash` | Run `bwrap --version`. Check that unprivileged user namespaces are enabled. |
| Project creation says the path is protected | The repo is the server's working directory, executable directory or `data_dir` (or contains/is inside one). Use another repository. |
| Project creation says it is not a git repository | Run `git init` and make a first commit, or use the UI offer to initialise it. |
| A hosted backend fails with an auth error | Set the backend's API key in the Backends tab (`api_key`), or export `AGENTFLOW_KEY_<NAME>` for the server process; no restart is needed. Names use `_` for `-`. |
| Task shows a waiting reason | No eligible candidate: check role candidates, model scope (`local` skips hosted backends), paid opt-in, and whether a second model exists for review. |
| A backend was marked unreachable | It is skipped for `backend_cooldown` (default 2 minutes). Check the host URL; retire or re-enable it on the Backends tab. |
| Run won't start | The queue must be approved, and the effort's budget not used up. |
| Run stopped by itself with reason `budget` | Raise the effort's budgets and start again. |
| Task is `needs_attention` | Infrastructure failures or git policy violations: read the task's History, fix the cause, then Retry. |
| `409` on retry or override | The task is not halted. |
| `409` on git commands | The effort is not complete yet. |
| After a restart, a project shows "was running" | That is a memory aid. Press Start. |
| Verification fails on a clean checkout | The baseline is recorded before the first task. Failures that already existed there are not blamed on a task; fix or remove the failing feedback command. |

## Auto-save

Editing something that already exists never needs a Save button: a backend, a role's candidate order, the settings drawer, an effort's settings, a project (including its definition of done and plugins), a project's limits, the FRD and a halted task. After five seconds without a change the card compares its fields with what was last saved and saves only if they differ. A refused save marks the field it names and is not retried until something changes again. A small mark shows the state: `…` pending, `↻` saving, `✓` saved, `⚠` not saved. **Done** (✓) saves anything pending and closes the card. One mark in the top corner, before the menu, rolls them all up: `✓` everything is saved, `…` something is waiting for its pause (click to save now), `⚠` something was refused (click to scroll to it).

Two things behave differently because of what a save means. Each FRD save stores a new revision (so a long editing session leaves several), and Done then shows the saved revision for approval. Editing a halted task of an approved queue approves the edited queue with it and a run carries on; stages built on the old task are discarded. A repository that is not a git repository is reported as an error on a project edit instead of offering git init.

Creating something (Add backend, Create project, Create effort) stays one explicit ✓, because saving on a pause would create half-typed entities.

## What is happening now

A strip above a project's tabs says what the project is doing, whichever tab is open: a pulsing dot with a clock while the engine or a model is working (drafting the FRD, planning, turning the plan into tasks, or running a task with its stage and model), a yellow dot when it is your turn (review the FRD, plan or queue; a halted task), and a grey dot when nothing is going on. The arrow opens the tab that shows it. The Overview row shows the run state ("stopped", Start) only once a queue is approved; before that it is not a decision you have, so it is left out.

**Unreachable backends.** When a stage ends `infra:unreachable` the backend is suspended (shown as "suspended: not answering" on the Backends tab and `suspended_since` in `GET /api/backends`). Nothing is dispatched to it and no stage is wasted retrying it. The engine checks its address on every reconcile tick (about a minute; any HTTP reply counts as answering) and brings it back by itself, then wakes the dispatcher and the stages that were waiting. An outage does not count against the task's infrastructure-retry limit or against any model, so shutting a machine down does not halt tasks; they go to another backend or wait. A backend without an address (opencode's built-in provider) cannot be checked and keeps the timed `backend_cooldown`.

**Installing a model on an Ollama host.** An Ollama backend's card on the Backends tab has an install row: type the model's name (as Ollama knows it, such as `devstral-small-2` or `gemma3:27b`), choose its tier, and press Install. The service asks the host to pull it through Ollama's own pull call on that host, so no ssh is needed, and shows a progress bar with the layer being downloaded. It carries on if you close the page, because the progress is stored. When the host says it is done, the model is added to the backend with the tier you chose and its context window, ready to put in a candidate list (it is not added to any list for you). If the host refuses, you see its own words (an unknown name answers `pull model manifest: file does not exist`). Cancel stops a pull; Ollama keeps the layers it has, so installing the same model again resumes. A service restart ends a running install as failed ("interrupted by a restart"); install it again to resume. API: `POST /api/backends/{name}/install` with `{model, tier}` (202, the install row; 409 when the same model is already being installed on that backend; 400 for a backend that is not Ollama or is retired), `GET /api/backends/{name}/installs`, `POST /api/installs/{id}/cancel`, and `install.update` events on the stream.

**Unreadable replies, repair evidence, escalation and scope.** (H11)
- *A full context window is the host's fault.* When a model's last step ends with finish reason `length`, the host's context window filled up and cut it off mid-task. The stage ends `infra:context_full`, naming the window size (for example 16384 tokens), with no strike. The same attempt goes to another model at once, without spending the infrastructure retries, at most twice; after that the normal infrastructure budget applies. The fix is on the host: raise Ollama's context length (`OLLAMA_CONTEXT_LENGTH`, for example 32768), keeping the memory it costs in mind.
- *Context windows.* A model is served with a context window that is often far smaller than the maximum it supports (Ollama commonly serves 16384). Each backend model has a **window** (Backends tab; blank = not known): your own value, or the size agentflow learned when a run was cut off (shown as "window 16.4k (learned)"). Where it is not known the **Context floor** setting applies (default 16384). Before a queue is approved, each task's context need is estimated (the prompt, the files it writes, repair attempts carrying the previous diff) and compared with the largest worker window; a task that cannot fit is sent back to the compiler to be split. At run time a worker whose window is too small for a task is passed over. The model maximum (`max 262.1k`) is shown for information only. To raise windows, give an Ollama backend a **Memory budget** (GB one loaded model may use, weights plus context) and optionally a **Window cap**, then press **Extend windows** on its card: for each model agentflow computes the largest window that fits the budget and re-creates the model on the host with that context size (same name, weights shared). Without a budget nothing is extended.
- *Gibberish gets one try.* A reply with no JSON object in it at all ends the stage `infra:unparseable` at once, with no corrective follow-up, and the retry is sent to a different model rather than the same one again (it falls back to the same model only when no other could serve). A reply that is an object with a wrong or missing field still gets its single follow-up.
- *A repair sees why a test failed.* The verify report carries an `evidence` list: for each new failing test, the lines it printed (its assertion message or panic, capped at 8 lines and 800 bytes). The failure signatures that fingerprints are built from are unchanged, so stuck detection works as before.
- *Repairs escalate by tier.* After two failed attempts in a row on a task, the next worker must be a stronger tier than the one that failed last (tier 1 stays tier 1). If no stronger model could ever serve, the same tier goes on trying, so the task can still reach stuck.
- *A task may not take another task's files.* The scope check now fails (not just warns) when a worker changes a file listed in `files_touched` by a task it is not built on, and does not list it itself; the repair report says it "belongs to another task". Dependencies' files are exempt, since they are already in the worker's tree, and a new file nobody owns is still only a warning. The compiler is told to name a task's test files in `files_touched` and to keep tests independent of one another.

**A worker that says it is blocked is checked before you are asked.** (H12) A first claim of being blocked ("the file does not exist", "I need an API key") ends the attempt as an ordinary failure with the fingerprint `blocked`, and the next attempt goes to a different model, with the claim in its prompt and a note that it may be a mistake (a weak model can invent a path or misread the task). Only a second claim, on the very next attempt, halts the task as `needs_input` with the worker's reason. A reviewer's block is not changed.

**Starting a clean test run.** The Overview tab's **Drop all projects** button (`POST /api/projects/_all_` with `{"confirm": true}`; without the confirmation it is a 400) removes every project, with its efforts, tasks, attempts, stages, strikes and statistics, so a full trial starts from empty Projects, Board and Stats screens. The button asks first and says what goes and what stays. Active efforts are abandoned and runs are stopped; no manual stopping is needed. It keeps the configuration (backends, models, tiers, tool flags, API keys, candidate lists, settings) and leaves your repositories and their `agentflow/*` branches alone (the response lists the leftover branches and the commands that delete them). Before removing anything it saves a copy of the whole database, failure history and strikes included, under `<data_dir>/archive/` and returns its path as `archive`. Deleting the database file instead would also delete the configuration.
