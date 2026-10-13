# Agentflow remediation — progress log

Resume point for the ralplan → ralph run of [FRD-AF-REM-001](frd.md). Newest entries at the bottom.

## Run parameters
- Branch: `agentflow` (commit per passing story, never push)
- Scope: all priorities (must, should, could)
- Builders: every implementation (executor) agent runs on Sonnet 5.5 (owner, 2026-10-13); reviewers keep their tiers
- Plan: `.omc/plans/ralplan-agentflow-remediation.md` (iteration 1); ralph PRD in `.omc/state/sessions/<session>/prd.json`
- Plan approval: automatic after critic approval; stop and ask only if the critic never approves within 5 rounds
- Pre-change snapshot: `~/pre-remediation-tracked.patch`, `~/pre-remediation-modified.tgz`, `~/pre-remediation-untracked.tgz`

## Phase status
| Phase | Status |
|---|---|
| FRD | done (2026-10-12) |
| ralplan | done (2026-10-13, iteration 1; owner stopped review loop) |
| ralph | pending |


## Spike findings (SP0, 2026-10-13)
Full notes, fixtures, the opencode stub server and the bwrap template are kept outside the repo in `~/agentflow-spikes/` (`findings-{opencode,bwrap,taskmaster}.md`, `fixtures/*.jsonl`, `stub/`, `sb_ro.sh`). Stories copy what they need into `testdata/`.

- **Runner binary (all):** `which opencode` is a bash shim (`~/.agents/.cache/shims/opencode`) that fails with a replaced HOME. Use the real binary `~/.opencode/bin/opencode` (v1.18.19), resolved before sandboxing and ro-bound into the sandbox. The server env carries `OPENCODE_CONFIG_DIR` (leaks the user's config) — the child env must be an allowlist.
- **S1 prompt delivery — FALLBACK met:** `-f file` truncates at ~50 KB; argv caps at 128 KB. Piping the prompt on **stdin with no message argument** delivered 500 KB (3/3) and 2 MB intact. Always set Stdin explicitly (an open never-closed pipe hangs `opencode run`).
- **S2 permissions — MET:** last-match-wins. Emit `"*": allow`, then `"git *": deny`, then each read-only git subcommand allow; plus `external_directory: deny`. Denies hold with/without `--auto`, catch compound commands, `$()`, `git -C`. `edit: deny` removes edit/write tools but not shell writes. Not caught: `/usr/bin/git`, `env git`, `sh -c` → GIT5 layers 2-3 (read-only gitdir + detect-and-revert, no creds) are required.
- **S3 isolation — MET:** `--pure` + `XDG_CONFIG_HOME=<stage>/xdg` (config at `<stage>/xdg/opencode/opencode.json`) + fresh HOME + scrubbed env + `OPENCODE_DISABLE_PROJECT_CONFIG=1`. Then a marker plugin is not loaded and generated agents are. `OPENCODE_CONFIG` is additive (insufficient alone); `OPENCODE_CONFIG_DIR` must never be passed. Provider keys must come via generated config or `OPENCODE_AUTH_CONTENT` (not tested with real keys).
- **S4 sandbox — MET:** bwrap 0.10.0, userns unrestricted. Read-only git works with `.git` AND `.git/worktrees/<id>` ro-bound; writable set = worktree + stage home only. commit/branch/update-ref/add/stash fail; `~/.ssh` etc. unreadable; no SSH agent. `--clearenv` must precede `--setenv`. Env allowlist: PATH, HOME, LANG, TERM=dumb, GIT_TERMINAL_PROMPT=0, GIT_CONFIG_GLOBAL=/dev/null, GIT_CONFIG_NOSYSTEM=1, GIT_OPTIONAL_LOCKS=0, XDG_*, GOCACHE, GOMODCACHE, GOPATH, GOTOOLCHAIN=local. Go deps: network or ro host GOMODCACHE + GOPROXY=off (0.27 s). **Extra:** the plan's `--ro-bind / /` leaves `/opt/unified-webapp/data/*`, `.git`, source and sibling stage dirs readable → B8 uses a deny-by-default root (ro-bind /usr, /etc, libs; tmpfs /home, /run, /tmp; bind only repo .git ro, worktree, stage home, runner binary) with tests that data dir, sibling stage and config are unreadable (this is within B8's own "no credentials reachable" scope).
- **S5 events — MET:** JSONL on stdout: `step_start`, `text`, `tool_use`, `step_finish`, `error`. Final text = last `text`; usage = sum of `step_finish.part.tokens` (per-step vs cumulative unverified). Errors: one `error` event + exit 1 — 429 (`statusCode` 429 + retry-after) → rate_limit; 500 → crash; no statusCode "Cannot connect" → unreachable; zero events → timeout. opencode retries internally (up to ~76 s) → adapter timeout must be well above 90 s. Rejected `ask` exits 0 with last step_finish reason `tool-calls` and no text → incomplete. SO2 corrective: `--session <id>` with the same HOME/XDG/cwd, new prompt on stdin.
- **S6 taskmaster — MET:** ctx cancel < 10 ms for Cancel, brake and Close. Cancel: queued → canceled; running → canceling → canceled. Pause keeps queued queued, leaves running alone. Brake cancels running jobs and pauses only unpaused lanes; release unpauses only those. Close: running → `failed: context canceled` (treat as infra:lost), queued stays pending. Width 0/-1 rejected by UpdateSettings; `InitialWidth:0` allowed. Pending jobs with unregistered kinds stay pending; RegisterLane after Open works (job starts next poll). Rule: if `BrakeEngaged` do nothing; pause only if not paused; unpause only if `PausedBy=="owner:agentflow"`. Reroute = Cancel + Submit. Second RegisterLane same name → ErrLaneRegistered.
- **Baselines:** `~/agentflow-gates-baseline.txt` (make gates exit 0, 5 gates pass); UI screenshots in `~/agentflow-ui-baseline/` (desktop tabs, settings, hamburger desktop/mobile; server run against an empty temp project_path). Playwright installed only in the session scratchpad; it launched the cached chromium-1234 via `executablePath`.


## Plan corrections found during execution
Later stories must follow these, not the original plan text.
- **Migrations:** v1 = events (B1), v2 = config tables (B2), v3 = effort/task tables (B3); append-only.
- **Task branch name:** git cannot hold `agentflow/<effort>` and `agentflow/<effort>/<task>` at once (`cannot lock ref`). Integration branch stays `agentflow/<effort>` (or user-named); task branches are `agentflow-task/<effort>/<task>` (`gitsvc.TaskBranch`). Applies to GIT2/PX2, B10, C6, UI.
- **opencode config location:** `<stage>/xdg/opencode/opencode.json` with `XDG_CONFIG_HOME=<stage>/xdg`, `--pure`, fresh HOME `<stage>/home`, `OPENCODE_DISABLE_PROJECT_CONFIG=1`; never set `OPENCODE_CONFIG`/`OPENCODE_CONFIG_DIR` (S3: OPENCODE_CONFIG is additive). Provider keys only as `{env:AGENTFLOW_KEY_<NAME>}`.
- **Prompt delivery:** stdin, no message argument (S1).
- **Sandbox:** deny-by-default root; `.git/worktrees/<id>` read-only; `Command` returns `(*exec.Cmd, error)`; env = allowlist + bwrap's `PWD`.
- **worktree prune:** only when every stale entry is under `<data_dir>/worktrees`.
- **Root `prompts/`:** copied into `internal/agentflow/prompts` (B4); D1 deletes the root copy.

## Log
- 2026-10-12 — FRD written (frd.json canonical, frd.md generated). Owner added UI6: keep existing look and feel.
- 2026-10-12 — OMC 5.7.1 installed mid-session; owner chose to run ralplan/ralph from a fresh session so OMC hooks, state tools and agent types are active.
- 2026-10-13 — ralplan iteration 1: plan `.omc/plans/ralplan-agentflow-remediation.md` (46 stories: R0, SP0, A1-A4, B1-B12, C1-C12, D1-D4, E1-E5, F1-F5, G1-G2). Architect: SOUND WITH CHANGES (R1-R9). Critic: ITERATE (8 blockers). Owner stopped the review loop and approved iteration 1 for ralph; review items deferred to the next phase — backlog in `.omc/plans/reviews/iter1-architect.md` and `iter1-critic.md`.
- 2026-10-13 — ralph started on the iteration-1 plan. Feedback baseline recorded (go vet, go test, tsc, make gates all green).
- 2026-10-13 — R0: pre-remediation baseline commit (excludes .agentflow/ and draft-frd.json).
- 2026-10-13 — SP0: spikes S1-S6 run (S1 via stdin fallback; S2-S6 met); findings above; baselines captured.
- 2026-10-13 — A1: unsafe execution half removed (Worker, runTask/runVerify, lock file, SSE stream, run-next/tasks/run/approve/repair/fail routes, LockFile/TaskExecution, broker SubscribePayload/UnsubscribePayload); `.agentflow/` deleted, draft-frd.json sha256 unchanged; TestBuildStartsNoWorker + TestRemovedExecutionRoutes404 added.
- 2026-10-13 — A2: gofmt sweep (12 files, formatting-only) + gofmt gate. Ralph feedback commands switched to quiet-on-success wrappers and baseline re-recorded (verify counted new PASS lines as failures).
- 2026-10-13 — A3: gate repairs: bundle-shape PASS + derived set, strict hamburger gate (no 'unknown'); fixed grocery drawer side (right) found by the strict gate.
- 2026-10-13 — B1: store core package (migrations v1=events; B2 appends v2, B3 v3).
- 2026-10-13 — A4: taskmaster RegisterLane: same-owner shared kinds, check->persist->register order, no residue on failure.
- 2026-10-13 — B5: runner interface + scriptable fake (Match, Partial, EditBeforeBlock).
- 2026-10-13 — B4: embedded role prompts + strict schema parser (last-object rule, blocked reviews, contract drift test); root prompts/ kept until D1.
- 2026-10-13 — B2: store config tables (migration v2 + CHECK constraints), validation, snapshot, repo-path safety incl. RecheckRepoPath.

## Handoff to the fresh session
1. `/oh-my-claudecode:ralplan --deliberate docs/agentflow-remediation/frd.md`
   - Deliberate mode: the FRD covers sandboxing, credentials, git policy and a data-model rewrite.
   - Execution is pre-approved by the owner: on critic APPROVE, proceed to ralph without asking; if 5 rounds pass without APPROVE, stop and ask.
2. `/oh-my-claudecode:ralph <approved plan path>` with these run parameters:
   - Implement ALL priorities (must, should, could) in frd.json.
   - Branch `agentflow`, on top of the uncommitted files; commit per passing story; never push.
   - Keep the existing agentflow look and feel (UI6).
   - Run the planning spikes S1-S6 first; record findings here.
   - Append a dated entry to this log after every story.
