# Session handoff (written 2026-10-16 19:05 EDT, before a Claude Code restart)

Read this first in a new session. Branch `agentflow` (never pushed). Live service: `unified.service` (port 8787, host `agentflow.builder.cmdhome.net`, PIN in `~/.unified_common.pin`). Work is committed; `git log --oneline | head -30` shows it. Phase H is described in `docs/agentflow-remediation/phase-hardening.md`, results in `progress.md`, user-facing behaviour in `docs/guides/agentflow.md`.

## 1. A trial is running right now (it survives the restart)

Project `webcalc-local3`, effort 6, repo `~/af-webcalc-local3`, branch `agentflow/effort-6`. It runs inside the systemd service, not in Claude, so restarting Claude does not stop it. At 19:03: T001 to T005 passed, **T006 (serve the page at GET /) running** on ollama-win gpt-oss:20b. Check: `python3 ~/agentflow-session-tools/afapi.py`-style calls (see section 5), or the UI.

When it finishes (status `complete`) or a task halts:
1. `python3 scripts/agentflow-webcalc-probe.py --repo ~/af-webcalc-local3 --branch agentflow/effort-6` (17 checks; port 7070 must be free). At 19:03 the API checks passed (13/17); the four page checks waited on T006.
2. Compare with efforts 3 (free models, 17/17), 4 (old local, 2/17, listens on 8080) and 5 (hardened, halted at T003); record stages, repairs, infra failures, tokens by model, strikes (Stats tab, `GET /api/stats/models`, `GET /api/strikes`) in `progress.md` (section "Webcalc trials").
3. Then: `python3 scripts/agentflow-reset.py` (dry run), then with `--yes` for a clean slate (it archives stats and a DB copy to `~/agentflow-archive/` and keeps configuration), then the full fresh trial (local-only, then the mixed set) as several projects/efforts so the project/effort UX can be judged.

The old watcher processes die with the Claude session; start a new one if wanted (`~/agentflow-session-tools/watch6.py`, background).

## 2. Uncommitted / unfinished at hand-off

Committed just before this note: H13 configuration export/import API (`internal/agentflow/api/config.go`, tests in `config_test.go`, route-walk entries) and the UI module `web/agentflow/js/configfile.ts` + `api.ts` types.
**Not done for H13:** wire `ConfigPanel` (configfile.ts) into the Backends tab (`backends.ts`, e.g. a section under the backend list, `new ConfigPanel(defaultConfigDeps(), () => void this.load())`), write `configfile.test.ts` (register in `scripts/test-web.mjs`), run `npm run build`, `npm run test:web`, `make gates`, document in the guide, commit. **Not deployed** (a restart interrupts a running stage): deploy with the drain procedure below.

## 3. What has been built this long session (all committed, live unless noted)

H1 literal-facts guard; H2 reviewer independence; H3 model tiers; H3b Stats tab; H4 hosts (m4 `ollama-m4`, windows `ollama-win`, m5 `ollama-m5`); H4a backend keys in UI; H5 probe script; H6 install a model from the UI (pull via Ollama API, progress stored); H7 per-task model avoidance + strikes as a streak (3 in a row bench a model, a success resets, history permanent, Clear button); H8 unreachable backend suspended until it answers, never blames models or tasks; H10 tool-support check (server reads `/api/show` capabilities; refused at backend save, probe warning, install, candidate lists, dispatch; startup refresh); H11 gibberish gets one try then another model, failing-test output in the repair report, tier escalation after two failed attempts, scope check fails for files another (non-ancestor) task owns, compiler names test files; H12 a worker's first "blocked" goes to another model; auth fix (gate takes the newest valid `uw_session` cookie when several are sent); Project dropdown marks the running project; `scripts/agentflow-webcalc-local.py` (local-lineup trial), `-kickoff.py` (`--resume`, `--lineup local`, `--dry-run`), `-probe.py`, `agentflow-reset.py`.
Planned, not built: H9 merge-conflict plan (prevention, deterministic ladder, then a resolver agent with a tier floor); UX proposals in `progress.md` ("projects and efforts are easy to confuse": header line, Overview card wording, active effort in the dropdown, trial number in effort titles); runtime permanent-refusal classification; speed-aware candidate order; gate-waiting age on the project card; backend trash-can delete; skip the `/frd` poll; folder-picker horizontal scroll.

## 4. Owner decisions and preferences (do not re-ask)

- Do not tolerate junk output anywhere (strict parsing stays strict); fix it by changing the model, not by loosening the parser.
- Gibberish (no JSON object at all) is given up on at the first try; strikes are a streak like failed passwords: a success resets, three in a row bench the model; keep the full failure history and metrics for tracking over time.
- An unreachable host is infrastructure, never a model's fault; suspend the backend, don't retry it.
- Local, free and paid models are treated the same by the strike rules.
- Kimi (WASTE) is **tabled**; `waste-serve.service` stays off; do not start it.
- Do not change auth settings; the PIN file is `~/.unified_common.pin`. (An OMC rate-limit daemon, PID 3560197, uses ~80% of a core; the owner may want it killed; I never touched it.)
- Prefer live changes; **a service restart interrupts a running stage**, so use the drain procedure: `POST /api/projects/<slug>/run/pause`, wait for no open stages (`select count(*) from stages where state<>'done'` on `data/agentflow/agentflow.db`), `make build`, `sudo systemctl restart unified.service`, then `POST .../run/start` (restart sets run state to stopped). `~/agentflow-session-tools/deploy.sh` does this for webcalc-local3.
- For a full test run: clean slate via `agentflow-reset.py`, not deleting the DB file (that would lose backends, tiers, keys, candidate lists).
- Projects and efforts were conflated by the owner (they looked at the old project `webcalc-local`, effort 4, which is complete at 100%); the UX note covers it.
- Merge conflicts: avoid first, but plan for them (H9); the owner asked to keep the idea of a deterministic ladder before any agent.

## 5. Working notes and gotchas

- API from scripts: POST `/api/auth/login` `{"method":"pin","pin":...}`, send header `Host: agentflow.builder.cmdhome.net`, keep the cookie; re-login on 401 (idle sign-out is 60 minutes without real use). Helper: `~/agentflow-session-tools/afapi.py` (exec it, then `api(method, path, body)`).
- Tests: `go test ./internal/agentflow/...` (engine takes ~4.5 minutes), `npm run test:web`, `npx tsc --noEmit`, `npm run build` (bundle `web/agentflow/js/bundle.js` is committed byte-identical), `make gates`. Every story: tests first, gates, one commit. Commit trailer: `Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>`.
- Real-browser check without Playwright: `~/agentflow-session-tools/cdp.mjs` drives the Chromium in `~/.cache/ms-playwright` over DevTools (signs in with the PIN, prints console errors and page text).
- Models: m5 `192.168.1.239:11434` (devstral-small-2 tier 1, qwen3.6:35b tier 1, gpt-oss, mistral-small3.2, qwen3-coder), m4 `192.168.1.11:11434` (qwen3.6:27b tier 1, slow), windows `windows.cmdhome.net:11434` (gpt-oss, mistral-small3.2, qwen3-coder, qwen3:8b, llama3.2:3b; slow, ~15 min per worker stage). gemma3:27b has no tool support and was removed.
- `draft-frd.json` in the repo root is an untracked leftover; ignore it.
- Stale OMC state: an `autopilot` state file for this session may be reported stale; `/oh-my-claudecode:cancel` clears it. Harmless.
