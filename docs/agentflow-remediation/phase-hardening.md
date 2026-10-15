# Phase H: harden agentflow against weak models (planned 2026-10-15)

Source: the two webcalc trials on the live service (efforts 3 and 4, same one-sentence spec, same engine).

| | Free models (effort 3) | Local models (effort 4) |
|---|---|---|
| Result | works, matches the spec | marked complete, does not work |
| Port | 7070 | 8080 (spec said 7070) |
| Routes | `/api/add` … `/api/history` | `/arithmetic/add`, `/history` |
| `GET /` | page | 404 |
| Time, stages, tokens | 16 min, 20, 0.55M | 148 min, 48, 6.0M |

The engine's gates (build, vet, tests, reviewer) all passed on the broken build. The unit tests were written by the same weak model that wrote the code, so they agree with it.

## Findings

1. **The port was lost at the first step.** The owner's sentence says port 7070. The local FRD draft (qwen3.6) has no requirement mentioning it (0 occurrences in FRD and plan). The planner then invented 8080. The free-model FRD kept it. Nothing in the engine notices that a stated fact vanished.
2. **The compiler compresses literals away.** The plan for effort 4 still had `POST /api/add` and friends. The compiled task prompts say "mapping POST endpoints for add subtract multiply divide", and the criteria are paraphrases ("Arithmetic endpoints return HTTP 200"). `compiler.md` rule 3 asks for "two to five short sentences", which squeezes out exact strings. Acceptance criteria are not required to quote anything.
3. **The reviewer cannot catch what the criteria do not say.** qwen3-coder approved the wrong routes and the wrong port because the criteria it was given do not contain them.
4. **Reviewer independence is partial.** `skipReviewer` (engine/dispatch.go) only rejects a reviewer whose model name equals the worker's. Same-family models (two qwens) still review each other.
5. **No model classes.** A candidate list is just an ordered list per role. A weak local model can land on planner or reviewer, and nothing says a reviewer should be at least as strong as the worker.

## Decisions (defaults; change before building if wrong)

- **Independence is soft, then loud.** Prefer a reviewer from a different family than the worker; if none is eligible, accept a different model of the same family; if none, wait as today. The attempt records which level was used and the UI shows it on the task. Not a hard block, because with only local models a hard family rule could stall a run.
- **Tiers are owner-set, with evidence beside them.** Each backend model has a tier: 1 strong, 2 capable, 3 light. A role has a floor. The Backends page also shows per-model results from past runs (stages, repairs, infra failures, time per stage) so the owner can see why a tier is wrong. No automatic reassignment.
- **Role floors:** drafter, planner, architect, critic, compiler: tier 1 or 2. Reviewer: its tier must be at or better than the worker's tier for that attempt. Worker: any tier. A candidate under the floor is skipped with a stated reason, exactly like the paid and hosted rules.
- **Family** is derived from the model name (`qwen*`, `mistral*`, `llama*`, `gpt-oss*`, `deepseek*`, …) and can be overridden per model; unknown names are their own family.

## Stories

**H1 Literal-facts guard.** (root cause for findings 1 to 3)
- Engine extracts literals from the owner's description deterministically: ports (`port N`, `:N`), URL paths, HTTP status codes, backticked and quoted strings, JSON field names in braces, file names. Stored with the effort.
- After the FRD draft: every literal must appear in the FRD text. A miss gets one corrective follow-up with the missing list named, then the draft fails visibly. Same check on the approved plan and on the compiled queue (each literal must appear in at least one task prompt or criterion).
- Prompt changes: drafter, planner and compiler copy literals verbatim; compiler rule 3 loosened so a prompt may be longer when it carries literals; each interface criterion must quote its literal (`POST /api/divide` with `b=0` returns 400 `{"error":"division by zero"}`).
- Reviewer prompt carries the FRD requirement text for the task's criteria, verbatim.
- Tests: a draft dropping the port fails then repairs; a compiled queue dropping a path is rejected.

**H2 Reviewer independence.** `family` on backend models (probe fills it, override in the UI); `reviewerFor` prefers a different family, then a different model; the attempt records the level; the task row shows it; tests for each of the three outcomes and for the sticky reviewer.

**H3 Model tiers.** `tier` on backend models (migration), role floors in the dispatcher with stated skip reasons, reviewer-not-weaker-than-worker rule, tier control on the Backends model rows, a per-model results table fed from the stages table, docs.

**H4 More hardware.** Add the two Ollama hosts (both answered on 11434 on 2026-10-15) and set slot widths conservatively.
- m4, `http://192.168.1.11:11434`, unified memory smaller than the m5: qwen3.6:27b-q4_K_M (16 GB), qwen2.5-coder:14b (8 GB), qwen2.5-coder-32k (8 GB). Width 1.
- windows, `http://windows.cmdhome.net:11434` (192.168.1.90), large system RAM plus an AMD eGPU: qwen3-coder:30b, gpt-oss:20b, mistral-small3.2, qwen3:8b, llama3.2:3b. Width 1 until throughput is measured; a model that spills out of the eGPU's memory into system RAM will be slow, so rate it by measurement, not size.
- Suggested starting tiers (owner to confirm): tier 1 qwen3.6 on the m5 and the other 24B+ instruct models; tier 2 qwen3-coder:30b, gpt-oss:20b, mistral-small3.2; tier 3 qwen2.5-coder:14b, qwen3:8b, llama3.2:3b.

**H5 Trial harness and re-run.** Turn the webcalc trial into a scored check: a probe script that starts the built branch on the port from the spec (first confirming the port is free, which would have caught my stale-server mistake) and tests every route in the spec, the page, the history cap and the 400 cases. Re-run webcalc with local-only models and with the mixed set, and record the results in progress.md. Success: the local-only run passes the probe, or fails at a gate with a named reason instead of completing.

## Order

H1 first (it is the actual defect), then H2, H3, H4 (adds hardware once tiers exist), H5 last. Each story: tests first, `make gates`, one commit.

## Not in this phase

A behavioral smoke stage that starts the built program (the sandbox forbids network and the engine runs no model-authored commands); noted as the next step if H1 is not enough. Backend delete (see the notes in progress.md).
