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
- **Tiers are owner-set, with evidence on its own tab.** Each backend model has a tier: 1 strong, 2 capable, 3 light. A role has a floor. Per-model results from past runs (stages, repairs, infra failures, time and tokens per stage) get their own top-level **Stats** tab beside Overview, Project and Backends (owner, 2026-10-15: it must be handy, not buried low on a nearly unrelated page). No automatic reassignment.
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

**H3 Model tiers.** `tier` on backend models (migration), role floors in the dispatcher with stated skip reasons, reviewer-not-weaker-than-worker rule, tier control on the Backends model rows, docs.

**H3b Stats tab.** A top-level Stats tab (`web/agentflow/js/stats.ts`, `GET /api/stats`): one row per backend and model with stages run, passes, repairs, infra failures, median and total time, tokens, and for workers the attempts-to-pass per task, filterable by role and by effort. Computed from the stages and attempts tables, no new storage. Each row shows the model's tier so a wrong tier is easy to see. Tests for the aggregation and the tab.

**H4 More hardware.** Add the two Ollama hosts (both answered on 11434 on 2026-10-15) and set slot widths conservatively.
- m4, `http://192.168.1.11:11434`, unified memory smaller than the m5: qwen3.6:27b-q4_K_M (16 GB), qwen2.5-coder:14b (8 GB), qwen2.5-coder-32k (8 GB). Width 1.
- windows, `http://windows.cmdhome.net:11434` (192.168.1.90), large system RAM plus an AMD eGPU: qwen3-coder:30b, gpt-oss:20b, mistral-small3.2, qwen3:8b, llama3.2:3b. Width 1 until throughput is measured; a model that spills out of the eGPU's memory into system RAM will be slow, so rate it by measurement, not size.
- Suggested starting tiers (owner to confirm): tier 1 qwen3.6 on the m5 and the other 24B+ instruct models; tier 2 qwen3-coder:30b, gpt-oss:20b, mistral-small3.2; tier 3 qwen2.5-coder:14b, qwen3:8b, llama3.2:3b.

**H4a Backend keys in the UI.** (owner, 2026-10-15) Today a key exists only as an `AGENTFLOW_KEY_<BACKEND>` environment variable read once at boot, which means a service restart and a hand-edited unit file. Add a key field to the backend form. The key is write-only: stored in a `0600` file under `data_dir` (not in the repositories, not in the database dumps), never returned by any API (the backend shows only "key set"), never logged, and forwarded to the sandbox per stage from the store, so a new key works on the next stage with no restart. The env variable keeps working and wins if both exist. Tests: write, replace, clear, never echoed, reaches the runner env, file mode. Owner OK'd that I read `GATE_API_KEY` from `/etc/waste/serve.env` (group-readable by the service user) and enter it through this mechanism, without printing it.

**H4b Kimi k3 through WASTE (experiment).** On builder, `~/wastegate.py` (running, port 8011, `--model-id kimi-k3`) is a keepalive proxy in front of WASTE's OpenAI-compatible server on 8010. WASTE streams a 2.78T-parameter MoE off NVMe at a fraction of a token per second, and the proxy serialises requests, so there is exactly one job at a time. The proxy requires a bearer key (I did not look for it); add the backend as `openai_compatible`, `http://127.0.0.1:8011`, with the key set through H4a, width 1, hosted off (it is local), tier 1.
- Honest expectation: at under one token per second a worker or compiler stage that writes a few thousand tokens would take hours, so it cannot be in the worker lane. The useful experiment is a single short, high-value stage: reviewer (a verdict is a few hundred tokens) or critic, on a small effort, with a long per-stage timeout set for that candidate only.
- Needs: a per-candidate stage timeout (today one project-wide limit) so a slow model is not killed or cause a retry storm; the job API of wastegate (asks longer than a socket survives) is a possible later adapter if SSE keepalive alone is not enough.
- The experiment: run the webcalc plan with Kimi as the only reviewer over the free-model workers, then compare in the Stats tab.

**H5 Trial harness and re-run.** Turn the webcalc trial into a scored check: a probe script that starts the built branch on the port from the spec (first confirming the port is free, which would have caught my stale-server mistake) and tests every route in the spec, the page, the history cap and the 400 cases. Re-run webcalc with local-only models and with the mixed set, and record the results in progress.md. Success: the local-only run passes the probe, or fails at a gate with a named reason instead of completing.

**H6 Install a model from the UI.** (owner, 2026-10-15) On the Backends page: pick an Ollama backend, name a model, press Install. The service calls the host's `POST /api/pull` (no ssh needed; it is the same API I used by hand on 2026-10-15 for devstral-small-2 and gemma3:27b), streams the progress (layer, bytes done of total) to a progress bar through a job record in SQLite so a page reload or service restart still shows it, and on success adds the model to the backend with a tier chosen at install time. Errors are shown as the host reports them (an unknown tag answers `pull model manifest: file does not exist`). Also: a host-side delete (`DELETE /api/delete`) behind the same "Are you sure?" box as the planned backend delete, and the host's loaded/installed lists (`/api/ps`, `/api/tags`) to warn when a model will not fit. Tests: progress parsing, the job surviving a restart, unknown tag, a cancelled pull. Not for non-Ollama backends.

**H7 Repeat failures change the model; three strikes bench it.** (owner, 2026-10-15, after effort 5 stalled on T003: qwen3.6:35b wrote a result file with one stray backtick three attempts running, and the task was declared stuck with other models idle.) The strict check stays strict; nothing tolerates junk.
- A model that has failed a task's latest attempts the same way (same fingerprint, same worker) from `stuck_threshold - 1` of them is passed over for the next attempt; if no other model could ever serve, the repeat runs so the task still reaches stuck.
- A strike is derived from the stages and attempts tables: unparseable output, a git policy violation, or the repeat above. Strikes are a streak (owner, 2026-10-15: like failed password attempts): a success by the model in that role resets the count to zero, and three in a row bench it, whether local, free or paid; the dispatcher skips it with the reason. The history is never deleted (owner: keep failures and metrics to track over time): `GET /api/strikes` returns every strike with kind, where, when and whether it still counts, plus per-day counts, and the Stats tab shows it with the streak, the lifetime total, a benched badge and a Clear button. Migration 13 stores only when the owner last cleared.

**H8 An unreachable backend is suspended, not retried, and blames nobody.** (owner, 2026-10-15, after switching off the m5.) A stage that ends `infra:unreachable` suspends its backend until a plain HTTP check of its address answers (checked on the reconcile tick); no task infra-retry strike, no backoff, no halt, no model strike. Backends without an address keep the timed cooldown. Repeat strikes count only verify and review fingerprints, so timeouts and merge conflicts are not held against a model.

**H9 A plan for merge conflicts (to come back to).** (owner, 2026-10-15: avoiding them is best, but some cannot be helped, so there must be a plan.) Today a conflict ends the merge stage `repair` and the next worker in the list gets an ordinary repair attempt, with no say over which model, and a weak model may keep one side, drop the other, or leave markers. Proposed, cheapest first:
1. **Prevention.** Reject or flag an attempt whose diff touches files outside the task's `files_touched`, and serialize tasks that share a file. (T003 of the 2026-10-15 webcalc run conflicted in `main_test.go`, which belonged to T002 running beside it.)
2. **A deterministic ladder** (owner, 2026-10-15: deterministic code first, an agent only for what is left). Tried in order on a conflicted merge, each rung followed by the normal verify (build, vet, tests) before it is accepted, because a clean merge is not a correct one:
   - a better textual merge: git's patience or histogram diff;
   - formatting-only differences: `gofmt`, then merge again;
   - union (keep both sides) for files where both sides only added independent things at the same spot: appended test functions, imports, route registrations, list entries, docs. A duplicate definition or a failing build after a union hands the conflict to the next rung instead of being accepted;
   - generated or derived files are regenerated, not merged (`go.sum` by `go mod tidy`);
   - a structure-aware merge (Mergiraf-style, by syntax tree) if one that supports the project's language is installed; check Go support before relying on it.
3. **An agent for what is left.** A `resolver` role with its own candidate list and a floor of tier 1 or 2 (never tier 3). Its prompt carries both tasks' prompts and acceptance criteria, the conflicting files with both sides, and the instruction to keep both intents; it works in the task worktree after the integration branch is merged into it, so the resolution is an ordinary commit that goes through verify and review again.
4. **Escalate, don't loop.** A conflict that survives one resolver attempt goes to the next tier up, and then halts for the owner with the two diffs shown, never silently retried. A conflict is never a strike against the worker that caused it; a resolver gets strikes like any model if it leaves markers or an unreadable result.
5. **Evidence.** Record which rung resolved each conflict (and for the agent, which model), and show conflicts per task, per file and per rung on the Stats tab, so it is measured how much the deterministic rungs absorb and whether the agent is needed often.

**H10 What stalled in effort 6 (2026-10-16) and the mitigations.**
- *The overnight stall was a gate, not compute.* Planning ran 45 minutes (22:54 to 23:37; the critic's two rounds on the m4 took 33 of them) and then waited at plan approval for 13.5 hours, because the external kickoff script gave up after 30 minutes and nothing else approves gates. Fixed in the script (6-hour waits, `--resume`). Still open: (a) an unattended trial should use the engine's own YOLO mode instead of an external poller; (b) a gate that has waited a long time should show how long on the project card; (c) order candidates within a tier by observed speed (Stats data), so a slow host is not the default critic.
- *A model that cannot do tool calls was listed.* `gemma3:27b` answers `does not support tools`; the runner needs tools. Five instant crashes burned T003's whole infrastructure budget in three minutes, and T003 (needs attention) blocked T004 and T006. To build: (a) read the host's reported capabilities (`/api/show` lists `tools`) when a model is probed or installed, store them, and skip a model without `tools` for every runner role with a stated reason; (b) a deterministic refusal (`does not support tools`, unknown model) is permanent, not transient: take the model out at once instead of retrying it, and spend nothing from the task's budget; (c) an infra retry should avoid the model that just crashed, not only the backend.
- *The server is not the bottleneck.* The service itself idles (0.1% CPU); load average 18 comes from the VM guests and one OMC rate-limit daemon; verify stages take about 0.1 minutes.

## Order

H1 first (it is the actual defect), then H2, H3, H3b (Stats), H4 (adds hardware once tiers exist), H4a (keys), H4b (Kimi), H5 last. Each story: tests first, `make gates`, one commit.

## Not in this phase

A behavioral smoke stage that starts the built program (the sandbox forbids network and the engine runs no model-authored commands); noted as the next step if H1 is not enough. Backend delete (see the notes in progress.md).

## Status (updated 2026-10-15)

- **H1, H2, H3, H3b done and committed.** H1: `model/literals.go`, checks in `engine/draft.go` and `engine/compile.go`, `engine/frdlink.go`. H2: `model/family.go`, `reviewerFor`. H3: migration 12, floors in `skipReason`, reviewer-not-weaker in `skipReviewer`. H3b: `web/agentflow/js/stats.ts`, `GET /api/stats/models?project=`.
- **H4 done on the live service.** `ollama-m4` (3 models) and `ollama-win` (5 models) added, one slot each; every local model rated (qwen3.6 tier 1; mistral-small3.2, gpt-oss:20b, qwen3-coder:30b tier 2; the 14B coder, qwen3:8b, llama3.2:3b tier 3). They are appended to the end of the global role lists (tier 3 only to worker and reviewer), so they take work when the earlier candidates are busy or down.
- Next: H4a (backend keys in the UI), H4b (Kimi), H5 (trial harness and re-run).
- **H4a, H5 done; devstral-small-2 (m5, tier 1) and gemma3:27b (windows, tier 2) added 2026-10-15.** Re-run with local models: `scripts/agentflow-webcalc-local.py` (fresh project and repo, candidate lists built from tiers by `--lineup local`, then the probe). `--dry-run` on the kickoff script prints the lists without starting anything.
- **H7 built 2026-10-15** (store/strikes.go, `repeatFailers` and the bench in engine/dispatch.go, stats rows and `POST /api/strikes/clear`). Known and not fixed: the attempt's independence label reads the worker model at attempt start, so a retry on another model can show `same_model` wrongly.
- **H6 built 2026-10-15** (migration 14 `model_installs`, `store/installs.go`, `api/installs.go`, `web/agentflow/js/installs.ts` on each Ollama backend card). Not built from the plan: host-side delete and the "will it fit" warning. Held back from deploy until the running trial (webcalc-local3, effort 6) ends, because a service restart interrupts a run.
- **H10 tool-support check built 2026-10-16** (migration 15 `backend_models.tools`; `probeInfo` reads `/api/show` capabilities; refused at backend save, probe warning, install, candidate lists and dispatch; start-up refresh). Still open from H10: treat a permanent runtime refusal (`does not support tools`, unknown model) as permanent rather than transient, make an infra retry avoid the model that just crashed, speed-aware candidate order, gate-waiting age on the project card. Not deployed until the running trial ends.
- **H11 built 2026-10-16**, from the T002 post-mortem (nine attempts that never saw why `TestHistory` failed): gibberish short-circuit with a model change, failing-test output in the verify report, tier escalation after two failed attempts, scope enforcement for files another task owns, and a compiler rule naming test files.
- **H12 built 2026-10-16**, from effort 6 T004: mistral-small3.2 told itself to edit `internal/api/main.go`, a path nobody gave it, declared the task blocked, and a retry hit the same model and the same invention. A first worker block now goes to another model; only a second halts for the owner.
