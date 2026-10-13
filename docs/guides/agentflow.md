# agentflow

The agentflow module is unified-webapp's built-in orchestrator UI + API for running
models in a Ralph-like loop: **FRD → plan/review → task queue → reviewed execution**.
It is the product — not a dashboard over an external system. The old
`/opt/agentflow/` shell/Python tree is a temporary migration source; nothing in this
module reads it at runtime. Design source of truth: `docs/agentflow-design.md`.

---

## Concepts

The pipeline moves through explicit stages with human gates between them:

```
no-frd → frd-draft → frd-approved → plan-draft → plan-approved → queued → running → done
```

- **FRD** — one objective, a list of requirements (`R1…`, must/should/could), acceptance
  criteria (`AC1…`, checkable), constraints, and context. Authored in the FRD Wizard tab,
  stored as `frd.json` (`status: draft`, then `approved`).
- **Plan** — approach, assumptions, risks, and phases generated from the approved FRD,
  then passed through a reviewer loop (max 3 rounds) before a human approves it.
  Stored as `plan.json` (`status: draft`, then `approved`).
- **Queue** — atomic tasks compiled from the approved plan. Each task carries `id`
  (`T001…`), `phase`, `title`, `status`, `prompt`, `working_dir`, `verify`, `acceptance`,
  `files_touched`, `attempts`/`max_attempts`, plus runner-managed `repair_prompt`,
  `last_run`, and `last_review`.
- **Task lifecycle** — `blocked → ready → running → verifying → reviewing →
  passed | repair | failed`. One task runs at a time (lock file); infrastructure
  failures re-queue without consuming an attempt.
- **Progress** — derived at runtime from `queue.json` (total, passed, ready, blocked,
  review, repair, failed). Live per-task output streams over SSE while a task runs.

---

## Config reference

Section `agentflow` in the server config (`internal/platform/config.AgentflowConfig`):

```json
"agentflow": {
    "static_dir":           "./web/agentflow",
    "project_path":         "./data/agentflow",
    "prompt_dir":           "./prompts",
    "default_profile":      "m5",
    "default_timeout_secs": 300,
    "profiles": [
        {
            "name":          "m5",
            "ollama_host":   "http://192.168.1.239:11434",
            "worker_model":  "qwen3.6:35b-a3b-mxfp8",
            "planner_model": "qwen3.6:35b-a3b-mxfp8",
            "review_model":  "mistral-small3.2",
            "timeout_secs":  300
        }
    ]
}
```

| Field | Type | Meaning |
|---|---|---|
| `static_dir` | string | Directory serving the module's frontend; mounted at `/` via `platform/static`. |
| `project_path` | string | Project root — the directory containing the `.agentflow` subdirectory. The parent is created on first boot if absent, at mode `0750`. Default when unset: `./data/agentflow`. |
| `prompt_dir` | string | Directory holding prompt templates (`planner.md`, `worker.md`, `reviewer.md`, `plan-reviewer.md`, `task-compiler.md`). Loaded at startup; a missing file fails the build loudly. |
| `default_profile` | string | Name of the active model profile (the knife switch). |
| `default_timeout_secs` | int | Fallback task timeout in seconds when a profile leaves `timeout_secs` unset. Default `300`. |
| `profiles` | array | Named model profiles: `name`, `ollama_host`, `worker_model`, `planner_model`, `review_model`, optional `timeout_secs` and per-profile `prompt_dir`. Explicit per-field values win over the profile. |

Prompt and profile paths accept a leading `~` (expanded at load).

---

## API route table

All routes live under the module's own hostname (e.g. `agentflow.cmdhome.net`) and are
wrapped by platform middleware like any other module.

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/queue` | Full task queue. 404 when `queue.json` does not exist; 503 when malformed. |
| `GET` | `/progress` | Counts derived from `queue.json` (total, passed, ready, blocked, review, repair, failed). Returns zero counts (not an error) when the queue file is missing or malformed. |
| `GET` | `/frd` | Current FRD. 404 when absent. |
| `POST` | `/frd` | Create/update the FRD draft. |
| `GET` | `/plan` | Current plan. 404 when absent. |
| `POST` | `/plan` | Create/update the plan draft. |
| `POST` | `/plan/review` | Run the plan reviewer loop against the current plan. |
| `POST` | `/tasks/compile` | Compile the approved plan into the task queue. |
| `POST` | `/tasks/run` | Enqueue a task for execution (`{ task_id }`, or empty for first ready). |
| `POST` | `/tasks/{id}/approve` | Mark task `approve` (human gate). |
| `POST` | `/tasks/{id}/repair` | Mark task `repair` (rework with `repair_prompt`). |
| `POST` | `/tasks/{id}/fail` | Mark task `failed`. |
| `POST` | `/run-next` | Advance the first `ready` task to `in-progress`. |
| `GET` | `/tasks/{id}/stream` | SSE stream of live task output (worker log, verify output, review verdict). |
| `GET` | `/settings` | Current profiles, prompt dir, and timeouts. |
| `POST` | `/settings` | Update profiles, prompt dir, or timeouts. |

---

## Data files

All state lives under `<project_path>/.agentflow/` (JSON on disk is the source of truth):

```
<project_path>/.agentflow/
    frd.json        ← FRD (UI writes via POST /frd)
    plan.json       ← plan (engine writes via /plan/*)
    queue.json      ← task queue (engine writes via /tasks/compile + worker)
    lock            ← single-task lock { pid, task_id, started } (worker only)
    logs/           ← append-only per-run stdout/stderr (worker only)
```

Files are replaced atomically (write temp, rename) so readers always see a consistent
snapshot. A stale `lock` (dead PID) is recovered automatically on the next run; a live
PID blocks with a clear message.

---

## The interface

Five tabs, one pipeline segment each (see `docs/agentflow-design.md` §4):

1. **FRD Wizard** — stepper (Objective → Requirements → Acceptance → Constraints →
   Review). One objective; requirements/AC/constraints are typed lists with add/remove
   and per-field tooltips. Validation runs before each advance; submit persists the draft.
2. **Plan Gen** — generate from the approved FRD, run the reviewer loop, approve.
   Disabled until an FRD is approved (enforced server-side).
3. **Compile** — compile the approved plan into atomic tasks; review the list.
4. **Queue** — progress bar, task list, detail pane (prompt, acceptance, verify command,
   last review), Approve/Repair/Fail/Run-next actions, and a live SSE output panel.
5. **Settings** — model profiles (Ollama host, worker/planner/reviewer per profile),
   prompt directory, and timeouts.

The hamburger menu slides from the right (trigger in `nav-right`); the theme picker is
built into the shared menu component.

---

## Integration guide

### 1. Add to `host_routing`

```json
"host_routing": {
    "agentflow.cmdhome.net":       "agentflow",
    "agentflow-test.cmdhome.net":  "agentflow"
}
```

### 2. Add the module section

```json
"agentflow": {
    "static_dir":      "/opt/unified-webapp/web/agentflow",
    "project_path":    "/data/agentflow",
    "prompt_dir":      "/opt/unified-webapp/prompts",
    "default_profile": "m5",
    "profiles": [
        {
            "name":          "m5",
            "ollama_host":   "http://192.168.1.239:11434",
            "worker_model":  "qwen3.6:35b-a3b-mxfp8",
            "planner_model": "qwen3.6:35b-a3b-mxfp8",
            "review_model":  "mistral-small3.2",
            "timeout_secs":  300
        }
    ]
}
```

### 3. Provide prompt templates

`prompt_dir` must contain `planner.md`, `worker.md`, `reviewer.md`, `plan-reviewer.md`,
and `task-compiler.md`. The module fails to build without them.

### 4. Author FRD → plan → queue in the UI

No seeding required: open the module host, complete the FRD Wizard, generate and approve
the plan, compile tasks. The queue tab then drives execution.

### 5. TLS / proxy

Follow the same HAProxy or nginx configuration pattern as any other module — see
[Production: HAProxy Configuration](../README.md#production-haproxy-configuration) in the
main README for SNI cert selection and host-preservation rules. AgentFlow has no special
proxy requirements beyond standard HTTP/HTTPS; a reverse proxy that rewrites the `Host`
header breaks dispatch, like every other module.

---

## Security notes

- No server-side authentication logic exists within the module itself (it inherits
  platform-level auth). If this module appears in `auth.modules`, callers need a valid
  session or API key; otherwise any reachable operator can read and mutate pipeline state.
- The worker shells out to the model runner and task verify commands on the server host
  with the service user's privileges. Treat `verify` commands and prompt templates as
  privileged content: only approved plans produce them, and plan approval is a human gate.
- Model API keys (if any) live in the profile/Ollama configuration, never in task,
  FRD, or plan JSON.
