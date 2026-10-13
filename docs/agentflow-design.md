# AgentFlow Module Design

Status: **draft for review** — no implementation lands against this doc until approved.
Goal: make the `agentflow` module of unified-webapp the complete orchestrator UI + API for
FRD → plan/review → task queue → reviewed execution, deprecating `/opt/agentflow/`.

## 0. Constraints (non-negotiable)

1. **`/opt/agentflow/` is temporary.** The unified-webapp module (UI + API) is the product.
   No code may assume that directory exists. Prompt templates and JSON schemas currently
   seeded from there are owned copies inside this repo (`prompts/`, validated against
   `schemas/`); the old tree is a migration source, not a runtime dependency.
2. **DRY inside the module.** Requirements, acceptance criteria, and constraints are one
   pattern (typed list: add / remove / validate / reorder). The wizard stepper is one
   pattern (progress → content → Back/Next + per-step validation). Both get exactly one
   implementation. A second copy is a bug.
3. **Shared components first.** Toasts, dialogs, theme, hamburger, toggles come from
   `@shared`. Module code owns only what is genuinely agentflow-specific (wizard content,
   plan review display, queue rows, settings forms).
4. **TDD, and regressions get locked-in tests.** New behavior ships with a failing test
   first. Every regression we have already hit gets a permanent gate or unit test:
   tab-init early-return, bundle-vs-source script reference, hamburger side alignment,
   wizard-step navigation presence.

## 1. Pipeline state machine

One linear pipeline with human gates. The backend owns the state; tabs are views over it.

```
no-frd ──► frd-draft ──(submit)──► frd-approved ──(generate)──► plan-draft
   ▲                                                        │  ▲
   │ edit                                                   │  │ repair loop (max 3)
   │                                                        ▼  │
   │                                              plan-approved ──(compile)──► queued
   │                                                                                │
   │                                                          ◄── repair/fail/pass ──┘
   │                                                          │  (per-task reviewer)
   │                                                          ▼
   │                                                    running ──► done
   └── abandon (any stage, explicit human action) ◄───────────────┘
```

Rules:
- Each stage has exactly one writer. UI writes drafts; the worker engine writes execution
  state; the reviewer writes verdicts. Nobody else mutates those files.
- Stage transitions are explicit API calls (`POST /frd`, `POST /plan/review`,
  `POST /tasks/compile`, `POST /tasks/run`), never side effects of rendering.
- `plan` and `compile` tabs are disabled until an approved FRD exists; `queue` run
  controls are disabled until an approved plan exists. Gating is enforced server-side
  (API rejects illegal transitions), the tab bar merely reflects it.

## 2. API contracts (mirror the schemas)

Frontend TypeScript types mirror `schemas/*.schema.json` field-for-field. Backend validates
incoming payloads against the same schemas and rejects with `400 + { error }` on mismatch.

| Endpoint | Input | Output | Transition |
|---|---|---|---|
| `GET /frd` | — | FRD or 404 | read |
| `POST /frd` | FRD (`status: draft`) | saved FRD | → `frd-draft` |
| `POST /frd/approve` | — | FRD (`status: approved`) | → `frd-approved` |
| `GET /plan` | — | Plan or 404 | read |
| `POST /plan/generate` | — (server reads approved FRD) | Plan draft | → `plan-draft` |
| `POST /plan/review` | — (server runs reviewer loop) | Plan + review notes | → `plan-approved` or stays `plan-draft` |
| `POST /tasks/compile` | — (server reads approved plan) | queue summary | → `queued` |
| `GET /queue`, `GET /progress` | — | queue / counts | read |
| `POST /tasks/run` | `{ task_id }` or empty (first ready) | `{ status }` | enqueue |
| `GET /tasks/{id}/stream` | SSE | worker log / verify / review events | stream |
| `GET /settings`, `POST /settings` | profiles, prompt dir, timeouts | settings | configure |

Existing `POST /tasks/{id}/approve|repair|fail` and `POST /run-next` remain as manual
overrides on top of this pipeline.

## 3. Persistence and file ownership

All state lives under the module's `project_path/.agentflow/`:

| File | Writer | Content |
|---|---|---|
| `frd.json` | UI (via `POST /frd`, `POST /frd/approve`) | FRD per `frd.schema.json` |
| `plan.json` | engine (via `/plan/*`) | Plan per `plan.schema.json` |
| `queue.json` | engine (via `/tasks/compile`, worker) | Queue per `task-queue.schema.json` |
| `lock` | worker only | single-task enforcement `{ pid, task_id, started }` |
| `logs/` | worker only | append-only per-run stdout/stderr |

`static_dir`, `project_path`, `prompt_dir`, profiles, and timeouts come from the
unified-webapp config (`agentflow` section). Nothing reads `/opt/agentflow/` at runtime.

## 4. Tab responsibilities

Five tabs, each owning one pipeline segment. `main.ts` is shell only: tab bar, panels,
hamburger, logout — no business logic.

| Tab | Owns | Entry condition | Exit artifact |
|---|---|---|---|
| FRD Wizard | stepwise FRD authoring | always open | `frd.json` (`draft`, then `approved`) |
| Plan Gen | generate → reviewer loop (≤3) → human approve | approved FRD | `plan.json` (`approved`) |
| Compile | plan → atomic tasks, human review of list | approved plan | `queue.json` |
| Queue | progress, task detail, run/approve/repair/fail, live SSE output | queue exists | task verdicts |
| Settings | profiles (Ollama host, worker/planner/reviewer models), prompt dir, timeouts | always open | config update |

Cross-tab flow uses explicit navigation (submit → plan tab; compile → queue tab), not
implicit side effects.

## 5. Reusable wizard framework (the DRY core)

One step definition, one stepper — not five hand-wired steps:

```ts
interface WizardStep<T> {
  id: string;
  title: string;
  render(host: HTMLElement, data: T): void;   // content only, no nav buttons
  validate(data: T): string | null;           // error message or null
  next?: string;                              // next step id
}
```

The framework renders: progress indicator → `step.render()` → standard
Back/Next (or Back/Submit on the final step) → runs `validate()` before advancing.
Steps supply content and validation only.

One typed-list component serves requirements, acceptance criteria, and constraints:

```ts
listField<T>(host, items, {
  add(): T;                    // e.g. next R-id / AC-id / blank string
  renderRow(el, item, i): void;
  validate(items): string | null;
})
```

Every text field takes an optional tooltip (`title` + `field-hint` line) explaining what
belongs there — FRD objective, requirement descriptions (`R1…`, must/should/could),
criteria (`AC1…`, checkable), constraints (one per line). Tooltips are part of the field
component's props, not ad-hoc markup.

## 6. Shared-component usage

- Feedback: `showToast` for transient results, `confirmDialog`/`alertDialog` for gates
  and errors. No native `alert()`/`confirm()`, no bespoke modal.
- Chrome: `ThemeManager` + `HamburgerMenu(side: 'right')` with the trigger in `nav-right`
  (locked by the hamburger-alignment gate).
- Forms: shared toggle/select/input styling via design tokens; no color literals, families
  via `font` shorthand so theme overrides win.

## 7. Execution engine (backend)

Single worker, one task at a time, enforced by `lock` file with stale-PID recovery:
`ready → running → verifying → reviewing → passed | repair | failed`.
Worker prompt → model (via `opencode-run` wrapper against the profile's Ollama host) →
verify command → reviewer model → verdict applied. Progress and live output stream over
SSE (`GET /tasks/{id}/stream`). Infrastructure failures (timeout, model unreachable)
re-queue without consuming an attempt; max review follow-ups cap repair loops.

## 8. Test plan

- **Unit (Go):** store CRUD for FRD/plan/queue; lock acquire/release + stale recovery;
  state-transition guards (reject illegal transitions); prompt template loading.
- **Unit (TS):** wizard framework (validate-before-advance, Back/Next presence on every
  step — regression lock for the missing-Next bug); typed-list add/remove/validate;
  tab gating (plan/compile disabled without approved FRD).
- **Gates (existing + kept):** `make check` — artifacts byte-identity, bundle-shape
  (incl. A9.4 bundle-vs-source), hamburger-alignment, shared barrel, CSS tokens.
- **Contract:** frontend types and backend validation both trace to `schemas/*.json`;
  a schema change requires both sides updated in the same change.

## 9. Migration off `/opt/agentflow/`

1. Vendor prompts + schemas into this repo (done: `prompts/`); backend reads only these.
2. Land this module behind its own hostname; run old and new side by side.
3. Port remaining behaviors (plan loop, task loop) into the engine; delete nothing in
   the old tree until parity is demonstrated by the engine's own tests.
4. Old tree becomes unreferenced; removal is a separate, explicitly approved change.

## 10. Open questions

1. Draft autosave: persist wizard drafts per-step to `frd.json` (`status: draft`), or
   keep drafts in memory until submit? (Recommendation: persist — refresh-safe.)
2. Plan reviewer loop: fully automatic up to 3 rounds, or pause for human eyes each round?
3. Task execution host: worker shells out on the server host only, or dispatch to the
   profile's Ollama host? (Current: server host runs, Ollama host serves models.)
