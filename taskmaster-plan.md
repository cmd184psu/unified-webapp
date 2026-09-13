# PLAN: Taskmaster module integration into unified-webapp

**Status: pending approval**
**Date:** 2026-09-12
**Mode:** RALPLAN consensus, DELIBERATE
**Source of truth:** `taskmaster-FRD.md` (decisions D1–D6 are settled; this plan implements them)
**Reference (READ-ONLY):** `reference/continuous-task-runner-queue/` — only `cmd/` + `internal/` + `web/` are live; `ctq/`, `ctq-example/`, and the committed `ctrqctl` binary are excluded and never touched.
**Model policy (D6):** Fable for planning/critic-tier oversight; Sonnet for all workers/builders. **Never Opus 5.** Execution: ralph sequential executor with Sonnet workers.

---

## RALPLAN-DR summary

### Principles (5)

1. **Verbatim-first port.** Ported code changes only where the FRD requires it: auth deletion (D1), lifecycle correctness (FR-B7), security gating (FR-X2/X3, D3/D4), and build-convention conformance (D5). Everything else stays byte-for-byte recognizable against the reference.
2. **Platform posture by contract, zero auth code in the module.** Protection, body limits, origin checks, headers, and the login page all come from the dispatcher wrapping (`docs/adding-a-module.md`); taskmaster contains not one line of authentication.
3. **Deterministic shutdown.** Every goroutine `Build` starts is owned by the returned handler and stopped by `Close()`; no `signal.Notify`, no unstoppable tickers, no `time.Sleep(time.Hour)` orphans. The goleak gate is a hard invariant, not a test to appease.
4. **Small, reversible, green steps.** Every phase ends with the tree compiling and a single named verification command passing — sized for a Sonnet builder to execute mechanically.
5. **Security honesty.** v1 ships the gate + config-gated sudo + `allowed_types` enforcement and *documents* the residual risk (FRD §8) instead of inventing half-sandboxing.

### Decision Drivers (top 3)

1. **The FRD's minimal-modification mandate** (FRD §1, FR-B2, FR-B4): the reference works; every rewrite is regression surface.
2. **Hard platform invariants:** the `Build(cfg) (http.Handler, error)` contract, `io.Closer` shutdown, the `cmd/server` goleak gate, `platform/static` off-disk serving, esbuild/committed-JS frontend conventions.
3. **Executor reliability:** the plan runs on Sonnet builders; steps must be mechanical, bounded, and independently verifiable.

### Viable Options (2)

**Option A — Verbatim port with adapters (CHOSEN).** Keep chi, keep the reference's subpackage layout under `internal/taskmaster/`, keep taskmaster's own SSE fan-out; adapt only the edges (config in, lifecycle out, auth deleted, validation added).
- Pros: smallest diff against a working reference; ported tests survive nearly unchanged; handler code (`chi.URLParam`, route table) untouched; lowest regression risk; fastest for Sonnet workers (copy → mechanical edit → verify).
- Cons: adds `go-chi/chi/v5` as a dependency; two router styles now coexist in the repo. (Response helpers are NOT duplicated: Phase 4 converges on `platform/response`, per `docs/adding-a-module.md`'s don't-reimplement rule.)

**Option B — Restructure to platform idioms.** Convert to Go 1.26 `http.ServeMux` patterns, flatten to one package, replace the SSE code with `platform/broker`. (`platform/response` adoption is not a differentiator — Option A does it too, in Phase 4.)
- Pros: repo uniformity; one fewer dependency; shared broker cap logic for free.
- Cons: rewrites every handler and every ported test; `platform/broker` is a broadcast/snapshot model that structurally mismatches taskmaster's per-execution *replay-then-live, done-terminated* streams (it would need per-exec room lifecycle management bolted on — more new code than it saves); directly contradicts driver 1; the highest-risk work lands in the least-verifiable place (SSE timing).

Both options are viable; **A wins on drivers 1 and 3**. B's only real payoff (uniformity) is recoverable later as an incremental refactor and is recorded as an ADR follow-up.

---

## Planner's-choice resolutions (FRD call-outs)

| # | Choice point | Resolution | Justification |
|---|---|---|---|
| P1 | chi vs Go 1.26 ServeMux | **Keep chi** (`github.com/go-chi/chi/v5`) | FR-B2 explicitly blesses it; every handler uses `chi.URLParam`; conversion touches all 5 handler files + tests for zero functional gain. Drop only `chi/middleware` Logger/Recoverer (the platform supplies posture; Recoverer would mask panics from the goleak-gated tests). |
| P2 | `/api/health` vs platform `/healthz` | **Keep `/api/health`** (behind the gate); CLI `health` uses it with the API key | Platform `/healthz` is process-liveness only; `/api/health` does `db.Ping()` — DB-aware health the CLI actually wants. Cost is one already-written handler. It now sits behind the gate like every other route; `taskmasterctl health` authenticates like every other subcommand (acceptable: an unauthenticated caller can still use `/healthz` for liveness). |
| P3 | SSE fan-out: own vs `platform/broker` | **Keep taskmaster's own fan-out**, add an instance-scoped subscriber counter honoring `cfg.SSEMaxSubscribers` (503 before headers when at cap, mirroring broker semantics) | The stream is per-execution ring-buffer replay → live subscribe → `event: done` termination. `platform/broker`'s Notify/Publish/snapshot model doesn't express replay-then-live-then-done per dynamic room; forcing it costs more new code than the ~100 lines kept. FR-B6 explicitly permits this given the cap is honored. |
| P4 | Package layout | **Keep subpackages:** `internal/taskmaster/{models,db,worker,coordinator}` + `internal/taskmaster/build.go` (package `taskmaster`) | Preserves the reference's import graph and package names — porting becomes a mechanical import-path rewrite (`github.com/cmd184psu/ctrq/internal/X` → `cmd184psu/unified-webapp/internal/taskmaster/X`). Nothing outside `internal/taskmaster` imports the subpackages except `build.go`; FR-B1's isolation requirement holds by construction (verified in Phase 5). |
| P5 | How the UI learns `allow_sudo` | **New `GET /api/capabilities` → `{"allow_sudo": bool}`**, fetched once at UI startup | One tiny read-only handler; doesn't pollute every task/group response; naturally extensible (future capability flags). UI caches it in a module-level variable; `tasks.ts` hides the sudo control when false. |
| P6 | CLI base-URL/key configuration | **Precedence: flags `-url`/`-key` > env `TASKMASTER_URL`/`TASKMASTER_KEY` > optional config file `~/.taskmasterctl.json`** (`{"url": "...", "api_key": "..."}`, created by the operator, must be 0600) | Flags for one-offs, env for CI, file for daily driving without leaking the key into shell history/process lists. No token cache (no `~/.ctrq-token` analog — the API key *is* the credential). Docs state: mint the key in the admin panel; taskmaster must have an `auth.modules` entry or API keys are not consulted. |
| P7 | Per-execution unregister goroutine (FR-B7) | **Delete it; GC-driven expiry is the sole reaper, keyed on a completion timestamp** | The reference double-covers cleanup (1 h `time.Sleep` goroutine *and* GC). Removing the goroutine entirely is simpler than making it cancellable and removes a whole goroutine class from the goleak surface. The GC expires on `FinishedAt` (stamped when the execution completes, see 3.2), preserving the reference's finish+1h replay window even for tasks that run longer than the TTL. FRD explicitly offers "or GC-driven expiry". |
| P8 | Body limit (FR-M7) | **Platform default 1 MiB; no `limitFor` override** | All taskmaster payloads are small JSON (task args are a JSON string in a row, not file uploads). If porting surprises us, the override is a 3-line follow-up with a comment. |

---

## Architecture at a glance (target state)

```
cmd/server/main.go            knownModules += "taskmaster"; buildModule case
cmd/taskmasterctl/main.go     ported CLI (API-key bearer auth)
internal/platform/config/     TaskmasterConfig + TaskmasterGroup + expandTaskmasterPaths
                              + applyServerDefaults copies SSEMaxSubscribers
internal/taskmaster/
  build.go                    Build(cfg) → handler + io.Closer; owns DB, registry, worker ctx
  build_test.go               goleak-verified Build/Close cycle + isolation checks
  models/models.go            ported verbatim (auth types deleted)
  db/db.go, db_test.go        ported verbatim (import rewrite only)
  worker/
    worker.go                 instance Registry; WaitGroup-joined runTask; no unregister goroutine
    executor.go               allowSudo refusal + exec.CommandContext + 10s WaitDelay
    output.go                 OutputRegistry: no global; stoppable GC; FinishedAt-keyed expiry;
                              Subscribe-after-done returns closed channel
    worker_test.go            ported (global→instance registry) + sudo/ctx/GC/subscribe tests
  coordinator/
    coordinator.go            chi routes minus auth (Routes → *chi.Mux); /api/capabilities;
                              SSE cap; platform/response envelopes
    handlers_*.go             ported; + sudo & allowed_types validation in tasks handlers
    sse.go                    ported; subscriber-cap check before headers
    coordinator_test.go       ported + validation tests
web/taskmaster/
  index.html, style.css       ported (script src → /js/bundle.js)
  js/*.ts                     ported TS, login/JWT stripped, EventSource SSE
  js/bundle.js                committed esbuild output
local-test/config.json        taskmaster section + host_routing + auth.modules entry
local-test/setup.sh           taskmaster.test hostname + data dir
docs/taskmaster.md            module deep-dive incl. §8 security reference
docs/USERGUIDE.md             updated
```

FR coverage map: FR-M1–M7 → Phases 1, 5, 8 · FR-B1–B8 → Phases 2–5 · FR-X1–X4 → Phases 3–4 · FR-W1–W4 → Phase 6 · FR-C1–C3 → Phase 7.

---

## Phase 1 — Config plumbing (FR-M1, FR-M2)

**Files modified:** `internal/platform/config/config.go` (+ its test file if present, else new `config_taskmaster_test.go`)

Steps:

1.1. Add to `config.go`:
```go
// TaskmasterGroup seeds one concurrency group into the taskmaster DB at startup.
type TaskmasterGroup struct {
    Name         string   `json:"name"`
    PoolLimit    int      `json:"pool_limit"`
    AllowedTypes []string `json:"allowed_types"`
}

// TaskmasterConfig holds configuration specific to the taskmaster module.
type TaskmasterConfig struct {
    StaticDir string            `json:"static_dir"`
    DBPath    string            `json:"db_path"`
    Groups    []TaskmasterGroup `json:"groups"`
    AllowSudo bool              `json:"allow_sudo"` // default false; see FRD §8
    // SSEMaxSubscribers is the effective SSE subscriber cap, copied from
    // Config.Server.SSEMaxSubscribers by Load. Not read from the config file.
    SSEMaxSubscribers int `json:"-"`
}
```
Add `Taskmaster TaskmasterConfig \`json:"taskmaster"\`` to `Config` (after `Multissh`, before `Auth`).

1.2. `expandTaskmasterPaths(t *TaskmasterConfig) error` expanding `StaticDir` and `DBPath` via `ExpandPath`, called from `Load` alongside the other expanders (after `expandMultisshPaths`).

1.3. `applyServerDefaults`: add `cfg.Taskmaster.SSEMaxSubscribers = max`.

1.4. `DefaultConfig()`: add
```go
Taskmaster: TaskmasterConfig{
    StaticDir: "./web/taskmaster",
    DBPath:    "./data/taskmaster/taskmaster.db",
    Groups:    []TaskmasterGroup{},
    AllowSudo: false,
},
```

1.5. Unit test: `Load` of a JSON blob with a `taskmaster` section round-trips fields, `~` in `db_path` expands, `SSEMaxSubscribers` is populated (both set and default-64 cases), and `DefaultConfig().Taskmaster.AllowSudo == false`.

**Done-check:** `go build ./... && go test -race ./internal/platform/config/`

---

## Phase 2 — Backend port: models + db (FR-B1, FR-B5, deps)

**Files created:** `internal/taskmaster/models/models.go`, `internal/taskmaster/db/db.go`, `internal/taskmaster/db/db_test.go`
**Files modified:** `go.mod`/`go.sum`

Steps:

2.1. `go get github.com/go-chi/chi/v5 modernc.org/sqlite github.com/stretchr/testify` (reference pins: chi v5.2.5, testify v1.11.1; use those or newer). goleak is already present.

2.2. Copy `reference/.../internal/models/models.go` → `internal/taskmaster/models/models.go`. Delete only the auth types (`AuthRequest`, `AuthResponse`, `Passcode`/JWT-related config fields if present in `models.Config`). Keep `models.Config` **only if** worker/db compile against it; otherwise trim to the fields they use — prefer keeping it verbatim minus auth fields (worker takes `*models.Config` today; Phase 3 changes what worker needs, so revisit there — the invariant for this phase is: package compiles).

2.3. Copy `internal/db/db.go` and `db_test.go` → `internal/taskmaster/db/`, rewriting imports `github.com/cmd184psu/ctrq/internal/models` → `cmd184psu/unified-webapp/internal/taskmaster/models`. **No logic changes**: WAL DSN, `SetMaxOpenConns(1)`, `PRAGMA foreign_keys`, migration mechanism, `task_metrics` all preserved verbatim (FR-B5). Ported tests keep the reference's `:memory:` DBs (already immune to cross-package file locking); any **new** file-backed test (e.g. Phase 5's build_test) uses `t.TempDir()`.

**Done-check:** `go build ./... && go test -race ./internal/taskmaster/...`

---

## Phase 3 — Worker port with lifecycle + sudo enforcement (FR-B4, FR-B7, FR-B8, FR-X1, FR-X2-executor)

**Files created:** `internal/taskmaster/worker/{worker.go,executor.go,output.go,worker_test.go}`

Steps:

3.1. Copy the three worker files + test, rewrite imports as in 2.3.

3.2. **`output.go` — de-globalize, make GC stoppable, fix two latent lifecycle bugs (FR-B7):**
- Delete `var Registry = ...`. Add `func NewRegistry() *OutputRegistry`.
- **Subscribe/MarkDone race fix:** the reference's `Subscribe` appends unconditionally while `MarkDone` closes only the subscribers present at that moment — a subscriber arriving after `MarkDone` gets a never-closed channel (hung SSE handler, hung httptest suite). Fix inside `Subscribe`, under the existing lock:
```go
func (oc *OutputCapture) Subscribe() <-chan OutputLine {
    ch := make(chan OutputLine, 256)
    oc.mu.Lock()
    defer oc.mu.Unlock()
    if oc.done {
        close(ch)   // late subscriber: signal completion immediately
        return ch
    }
    oc.subscribers = append(oc.subscribers, ch)
    return ch
}
```
This is FR-B7 lifecycle correctness, not verbatim creep. (sse.go's check-then-subscribe pattern needs no change once this holds.)
- **GC expiry keyed on completion, not registration:** `captureSet` gains `FinishedAt time.Time` (zero while running); add `func (r *OutputRegistry) MarkFinished(execID int64)` stamping it `time.Now()` (called by `runTask` right after both `MarkDone`s, see 3.3). `gc` deletes only sets with a non-zero `FinishedAt` older than the TTL — preserving the reference's *finish*+1h replay window for executions that run longer than the TTL (the reference's sleep-unregister timed from finish; keying on `CreatedAt` would have narrowed it). `Stdout.Done()/Stderr.Done()` checks in `gc` become redundant but harmless; keep the non-zero-`FinishedAt` check as the sole condition.
- Replace `StartGC(ttl)` with a stoppable form:
```go
// StartGC runs the reaper until Stop is called. Returns a stop func that
// blocks until the goroutine has exited.
func (r *OutputRegistry) StartGC(ttl time.Duration) (stop func()) {
    done := make(chan struct{})
    stopped := make(chan struct{})
    go func() {
        defer close(stopped)
        ticker := time.NewTicker(5 * time.Minute)
        defer ticker.Stop()
        for {
            select {
            case <-ticker.C:
                r.gc(ttl)
            case <-done:
                return
            }
        }
    }()
    return func() { close(done); <-stopped }
}
```

3.3. **`worker.go` — instance registry, tracked goroutines, no orphan reaper, ctx-aware execution (FR-B7, P7, A4):**
- `Worker` gains `registry *OutputRegistry`, `wg sync.WaitGroup`, and `runCtx context.Context` (the module lifetime context, passed to `Start` and stored for `runTask`); `New`/`NewWithExecutor` take the registry.
- **allowSudo wiring:** `New(database, registry, workerID, allowSudo)` constructs `executor: &TaskExecutor{AllowSudo: allowSudo}` — the config flag actually feeds 3.4's enforcement. `NewWithExecutor` keeps injecting mocks for tests.
- In `poll`: `w.wg.Add(1)` before `go w.runTask(...)`; `runTask` does `defer w.wg.Done()`.
- In `runTask`: **delete** the `defer go { time.Sleep(time.Hour); Unregister }` block entirely; the GC is the sole reaper. `Registry.Register` → `w.registry.Register`. Call the executor as `w.executor.Execute(w.runCtx, task, stdoutW, stderrW)`; after both `MarkDone`s, call `w.registry.MarkFinished(execID)` (3.2). A ctx-cancelled execution records status `failed` with the context error message — acceptable and documented (8.3).
- Add `func (w *Worker) Wait() { w.wg.Wait() }` for Close to join in-flight `runTask` goroutines. Note honestly: **this join is NEW behavior** — the reference never joins `runTask` goroutines (its `wg` covers only the poll loop and coordinator); the join is what makes the goleak gate meaningful. It is *bounded* because `Close` cancels the ctx first and `CommandContext` + `WaitDelay` (3.4) guarantee `Wait` returns within ~10 s of cancel even for unkillable or pipe-holding processes, so drain completes promptly instead of waiting out arbitrary commands.
- `Start(ctx)` scheduling semantics unchanged (5 s ticker, lock TTL 10 min, pending-execution reuse — FR-B4 verbatim). Make the poll interval an unexported package var (`var pollInterval = 5 * time.Second`) so tests can shorten it (A7/5.3); production value untouched.
- `output_file` teeing with `{task}`/`{exec_id}` substitution preserved verbatim (FR-B8; no fspath confinement in v1 per FRD).
- Replace `cfg *models.Config` with the two things worker actually needs: nothing from config except sudo policy → the `allowSudo` parameter above; delete the `cfg` field (trim `models.Config` accordingly in 2.2 if now unused).

3.4. **`executor.go` — defense-in-depth sudo refusal + ctx-aware commands (FR-X2, A4):**
- The `Executor` interface becomes `Execute(ctx context.Context, task *models.Task, stdout, stderr io.Writer) error` (interface is already being touched for sudo; one signature change covers both).
- `TaskExecutor` gains `AllowSudo bool`; `buildCmd` becomes a ctx-taking method:
```go
const waitDelay = 10 * time.Second

func (te *TaskExecutor) buildCmd(ctx context.Context, sudo bool, name string, args ...string) (*exec.Cmd, error) {
    if sudo && !te.AllowSudo {
        return nil, errors.New("task requests sudo but allow_sudo is disabled in taskmaster config")
    }
    var cmd *exec.Cmd
    if sudo {
        cmd = exec.CommandContext(ctx, "sudo", append([]string{name}, args...)...)
    } else {
        cmd = exec.CommandContext(ctx, name, args...)
    }
    // WaitDelay bounds cmd.Wait after ctx cancel even when (a) a backgrounded
    // grandchild inherits the stdout/stderr pipes and never EOFs them, or
    // (b) a sudo child is root-owned and our unprivileged kill gets EPERM.
    // After cancel+waitDelay, Wait force-closes the pipes and returns.
    cmd.WaitDelay = waitDelay
    return cmd, nil
}
```
- **Why `CommandContext` + `WaitDelay` (A4 choice a, R1):** ctx cancel alone does not bound the drain — `cmd.Stdout/Stderr` are `io.Writer`s, so os/exec creates OS pipes with copy goroutines and `Wait` blocks until pipe EOF; a `shell` task running `sh -c 'something &'` leaves a grandchild holding the write end after the child dies, and a sudo child is root-owned so the cancel's SIGKILL fails with EPERM. `WaitDelay` covers both: after cancel + 10 s, `Wait` force-closes the pipes and returns. Together they make `Close()` deterministic — the 3.3 drain is bounded rather than hostage to a wedged or unkillable task — upholding principle 3 at the cost of shutdown killing (or, for sudo/grandchildren, abandoning) in-flight tasks, which 8.3 documents.
- All four task types (`exec`, `shell`, `script`, `migration`) preserved (FR-X1); each `execute*` threads ctx and propagates the buildCmd error so a stale `sudo: true` task **fails** with a clear message written to its execution error, rather than silently dropping sudo.

3.5. Tests: port `worker_test.go` (mock executor). **Named mechanical rewrites (A7):** the reference test file uses the package-global `worker.Registry` (worker_test.go:203–215) — rewrite those references to a per-test `NewRegistry()` instance passed into `New`/`NewWithExecutor`; mock executors gain the ctx parameter. Then add:
- sudo-gating: `AllowSudo:false` + `sudo:true` task → execution fails with the config error; `AllowSudo:true` → command is prepended with `sudo` (assert on `buildCmd` output, don't actually run sudo).
- ctx cancellation: a long-running `shell` task's execution finishes `failed` promptly after ctx cancel; **and** the pipe-inheriting-grandchild case — a `shell` task of `sh -c 'sleep 300 & wait'` — still returns within cancel + `WaitDelay` (~10 s bound), proving `Wait` doesn't hang on the orphan's open pipe (R1).
- GC stop: `StartGC` then `stop()` returns and (under goleak in this package's `TestMain`) leaks nothing.
- registry expiry: **finished** captures with `FinishedAt` past TTL are removed by `gc`; a still-running set registered before the TTL window survives (A3 semantics).
- subscribe-after-done: `MarkDone` then `Subscribe` returns an already-closed channel (A2 regression test).
- Add `TestMain` with `goleak.VerifyTestMain(m)` to this package.

**Done-check:** `go test -race ./internal/taskmaster/worker/`

---

## Phase 4 — Coordinator port: routes minus auth, validation, SSE cap (FR-B3, FR-B6, FR-X2, FR-X3, D4, P2, P3, P5)

**Files created:** `internal/taskmaster/coordinator/{coordinator.go,handlers_groups.go,handlers_tasks.go,handlers_executions.go,handlers_metrics.go,sse.go,coordinator_test.go}`
**Not ported:** `auth.go` (deleted per D1), `Coordinator.Start`/`http.Server` (module contract replaces it).

Steps:

4.1. Copy coordinator files (minus `auth.go`), rewrite imports. In `coordinator.go`:
- `Coordinator` fields: `db *db.DB`, `registry *worker.OutputRegistry`, `allowSudo bool`, `sseMax int`, plus an `atomic.Int64` SSE-subscriber counter.
- `New(database *db.DB, registry *worker.OutputRegistry, allowSudo bool, sseMax int) *Coordinator`.
- **`handlers_groups.go` also carries auth code:** when copying it, **delete the `handleAuthToken` function** (reference handlers_groups.go:12–26) — it references the deleted `models.AuthRequest`/`AuthResponse`, `c.cfg.Passcode`, and `c.generateToken()`; a verbatim copy does not compile. Grep the copied coordinator files for `AuthRequest\|AuthResponse\|Passcode\|generateToken` → must be empty.
- `routes()`: delete `r.Post("/api/auth/token", ...)`, the `r.Group`/`authMiddleware` wrapper (all routes now flat — the platform gate protects them), and `middleware.Logger`/`middleware.Recoverer` (platform posture; Recoverer would hide panics from tests) — also drop the now-unused `chi/v5/middleware` import. Keep `r.Get("/api/health", ...)` (P2). Add `r.Get("/api/capabilities", c.handleCapabilities)` (P5) returning `{"allow_sudo": c.allowSudo}`. Static serving is **not** mounted here — `build.go` composes it, so `Routes`/`routes` return **`*chi.Mux`** (not `http.Handler`) so `build.go` can call `r.Handle("/*", ...)` (A1).
- **Response helpers converge on the platform (A6):** delete the local `writeJSON`/`writeError`; use `platform/response.WriteJSON`/`response.WriteError` throughout (byte-identical `{"error": ...}` envelope; mechanical find-replace), and `response.WriteDecodeError(w, err)` in every `json.Decoder.Decode` error branch (correct 413-vs-400 mapping under the platform BodyLimit for free). `docs/adding-a-module.md` explicitly forbids reimplementing these. The one exception: `sse.go`'s inline `event:`/`data:` writes stay hand-rolled (they are SSE framing, not JSON envelopes).

4.2. **`handlers_tasks.go` — validation (FR-X2 create/update, FR-X3/D4):** add a shared helper used by both `handleAddTask` and `handleUpdateTask`:
```go
// validateTaskPolicy enforces allow_sudo and the group's allowed_types.
func (c *Coordinator) validateTaskPolicy(taskType, groupName string, sudo bool) (status int, msg string)
```
- sudo: `sudo && !c.allowSudo` → `403 "sudo tasks are disabled (allow_sudo is false in taskmaster config)"`.
- allowed_types: fetch group; non-empty `AllowedTypes` not containing `taskType` → `400 "task_type %q not permitted in group %q (allowed: %v)"`. Empty/absent list = all types allowed.
- `handleAddTask`: call after the existing group-exists check.
- `handleUpdateTask`: compute the *effective* post-update `task_type`, `group_name`, `sudo` (updates map merged over `existing`) and validate that triple — covers moving a task into a stricter group and flipping sudo on. **Type discipline (C7):** extract each field from the `map[string]any` with comma-ok assertions (`v, ok := updates["sudo"].(bool)`); a present-but-wrong-typed value → `400 "field %q must be a %s"` — never a naive `.(bool)` that panics into a 500.

4.3. **`sse.go` — subscriber cap (FR-B6, P3):** at the top of `handleExecutionOutput`, before any header is written:
```go
if c.sseMax > 0 {
    if n := c.sseSubs.Add(1); n > int64(c.sseMax) {
        c.sseSubs.Add(-1)
        response.WriteError(w, http.StatusServiceUnavailable, "sse subscriber limit reached")
        return
    }
    defer c.sseSubs.Add(-1)
}
```
The 503-before-headers path is plain JSON, so it falls under the A6 convergence rule, not the hand-rolled-SSE-framing exception — likewise the reference sse.go's two pre-stream error call sites (sse.go:18 invalid-id, sse.go:39 not-found) convert to `response.WriteError` as part of the 4.1 sweep (R3). Everything else (replay, live subscribe, `event: output|status|done` framing) verbatim.

4.4. Port `coordinator_test.go`: strip auth-token setup (call `Routes(c)` directly), add tests: capabilities endpoint reflects allowSudo; create-with-sudo 403 when disabled; create/update violating `allowed_types` → 400 with clear message; update that moves a task into a restrictive group → 400; wrong-typed update field → 400; SSE 503 at cap (sseMax=1, two concurrent requests). **Message expectations (R4a):** ported tests asserting the reference's exact `"invalid request body"` error text must have expectations updated wherever `response.WriteDecodeError` now speaks (its 400/413 messages differ). **The SSE-cap test must `MarkDone` its registered captures before asserting/returning** so the in-cap streaming handler's goroutine exits (via the closed subscriber channels → `event: done`) ahead of the goleak check (A7). `TestMain` with goleak here too.

**Done-check:** `go test -race ./internal/taskmaster/...`

---

## Phase 5 — Build/Close + server registration (FR-M3, FR-M4, FR-M5, FR-M7, FR-B7)

**Files created:** `internal/taskmaster/build.go`, `internal/taskmaster/build_test.go`
**Files modified:** `cmd/server/main.go`

Steps:

5.1. `internal/taskmaster/build.go` (package `taskmaster`), modeled on `internal/slideshow/build.go`:
```go
func Build(cfg config.TaskmasterConfig) (http.Handler, error) {
    if err := os.MkdirAll(filepath.Dir(cfg.DBPath), 0o755); err != nil { return nil, ... }
    database, err := db.Open(cfg.DBPath)                     // WAL, single conn
    if err != nil { return nil, err }
    if err := seedGroups(database, cfg.Groups); err != nil { database.Close(); return nil, err }

    registry := worker.NewRegistry()
    stopGC := registry.StartGC(time.Hour)

    workerID, _ := os.Hostname()
    w := worker.New(database, registry, workerID, cfg.AllowSudo)
    ctx, cancel := context.WithCancel(context.Background())
    workerDone := make(chan struct{})
    go func() { defer close(workerDone); w.Start(ctx) }()
    log.Printf("taskmaster: worker started (poll 5s, db %s, allow_sudo=%v)", cfg.DBPath, cfg.AllowSudo)

    c := coordinator.New(database, registry, cfg.AllowSudo, cfg.SSEMaxSubscribers)
    r := coordinator.Routes(c)                               // *chi.Mux with /api/* (A1)
    r.Handle("/*", static.NewHandler(cfg.StaticDir))         // FR-M5: module owns "/"

    return &closer{Handler: r, close: func() error {
        cancel()                 // stops poll loop; kills running commands (CommandContext+WaitDelay)
        <-workerDone
        w.Wait()                 // join runTask goroutines — bounded, commands are dying
        stopGC()                 // stop registry GC
        return database.Close()  // then close DB
    }}, nil
}
```
`seedGroups` ported from `reference/.../cmd/ctrq.go:76–89` (upsert name/pool_limit/allowed_types; nil→`[]string{}`; DB authoritative thereafter; note the FRD §2 mis-cites this as `cmd/run.go` — run.go is only argv[0] dispatch). `closer` is the slideshow `stoppableHandler` pattern. **No `signal.Notify` anywhere in the module** (grep-verified in 5.4).

Note on the drain (A4): joining `runTask` goroutines is **new** behavior — the reference never joins them (its WaitGroup covers only the poll loop and coordinator). The join is what makes goleak meaningful, and it is deterministic because ctx cancellation reaches running commands via `exec.CommandContext`, with `WaitDelay` bounding `Wait` even against root-owned sudo children or pipe-inheriting grandchildren (3.4): `Close` kills or abandons in-flight tasks (recorded as `failed`) rather than waiting them out. Documented in `docs/taskmaster.md` (Phase 8).

5.2. `cmd/server/main.go`: add `"taskmaster"` to `knownModules`; add `case "taskmaster": return taskmaster.Build(cfg.Taskmaster)` to `buildModule`. **No `limitFor` change** (P8 — default 1 MiB).

5.3. `internal/taskmaster/build_test.go` with `goleak.VerifyTestMain`:
- Build with `t.TempDir()` DB + static dir and seeded groups → hit `/api/groups` and `/api/capabilities` via `httptest`, enqueue a trivial `shell` task (`{"shell":"echo hi"}`), poll `/api/executions` until success, stream its output SSE, then `Close()` → goleak clean. To keep this off the `go test -race ./...` critical path, shorten the worker poll interval via an exported `worker.SetPollIntervalForTest` helper (the only route — build_test.go is package `taskmaster`, so it cannot touch the unexported 3.3 var directly), called once from `TestMain` before any `Build` so the var is never mutated while a worker goroutine is live under `-race` — FR-B4 governs production semantics, not test wiring (A7/R4b).
- Build error path: unwritable DB path returns error and leaks nothing.

5.4. Isolation + hygiene checks (one-shot, part of done-check):
`grep -rn "signal.Notify" internal/taskmaster/` → empty; `grep -rln "internal/taskmaster" --include=*.go internal/ cmd/ | grep -v "^internal/taskmaster/\|^cmd/server/main.go\|^cmd/taskmasterctl/"` → empty (FR-B1 isolation both directions); `grep -rn "golang-jwt" internal/taskmaster/ cmd/taskmasterctl/` → empty (D1).

**Done-check:** `go build ./... && go test -race ./...` (full-repo — includes the existing `cmd/server` goleak gate) + the three greps in 5.4.

---

## Phase 6 — Frontend port to unified conventions (FR-W1–W4, D5)

**Files created:** `web/taskmaster/index.html`, `web/taskmaster/style.css`, `web/taskmaster/js/{api,main,groups,tasks,executions,metrics,output}.ts`, committed `web/taskmaster/js/bundle.js`
**Files modified:** `package.json`, `tsconfig.json`

Steps:

6.1. Copy `web/static/ts/*.ts` → `web/taskmaster/js/`, `index.html` + `style.css` → `web/taskmaster/`. Change `index.html`'s script tag to `<script type="module" src="/js/bundle.js"></script>` (multissh bundle precedent; absolute paths stay valid because the module owns `/` on its vhost — FR-M5). Delete the reference `js/*.js` outputs (not copied).

6.2. **`api.ts` — strip auth (FR-W3):** delete `_token`, `setToken`, `clearToken`, `hasToken`, the `Authorization` header injection, and `api.login`. Replace the 401 branch with `window.location.reload()` (the platform gate then serves its login page for the browser GET). Keep every endpoint wrapper; add `capabilities(): Promise<{allow_sudo: boolean}>` → `GET /api/capabilities`.

6.3. **`main.ts`:** remove the `#login` route/page and any `hasToken()` boot guard; on startup, `await api.capabilities()` into an exported `caps` object (default `{allow_sudo:false}` on fetch failure), then route as before. **`buildNav()` (C2):** it imports and branches on `hasToken`/`clearToken` (nav-link gating + logout click handler), both deleted in 6.2 — rewrite it to render the nav unconditionally (the platform gate already blocked unauthenticated users before `index.html` was ever served); replace the logout handler's `clearToken()` + `#login` hop with `fetch('/api/auth/logout', {method:'POST'})` (the gate-owned logout route present on every protected module) then `window.location.reload()`.

6.4. **`tasks.ts` (FR-X2 UI):** render the sudo checkbox only when `caps.allow_sudo`; when hidden, never send `sudo: true`.

6.5. **`output.ts` — EventSource (FR-W3):** replace the fetch-ReadableStream reader with
```ts
const es = new EventSource(`/api/executions/${id}/output`);
es.addEventListener('output', e => appendLine(JSON.parse(e.data)));
es.addEventListener('status', e => showStatus(e.data));
es.addEventListener('done', () => { es.close(); markDone(); });
es.onerror = ...  // close + show disconnect notice
```
Close the EventSource on page navigation (hook the existing hash-router teardown).

6.6. Build wiring (FR-W2): in `package.json` `build` and `build:dev`, append `esbuild web/taskmaster/js/main.ts --bundle --target=es2020 --outfile=web/taskmaster/js/bundle.js` (with `--sourcemap` in `build:dev`); add `"web/taskmaster/js/*.ts"` to `tsconfig.json` `include`. Run `npm run build` and **commit `bundle.js`** (committed-JS convention).

**Done-check:** `npm run build && npm run typecheck` clean; `grep -rn "sessionStorage\|Bearer\|auth/token\|#login" web/taskmaster/js/*.ts` → empty.

---

## Phase 7 — taskmasterctl CLI (FR-C1, FR-C2, FR-C3, D2)

**Files created:** `cmd/taskmasterctl/main.go` (single file, ported from `reference/.../cmd/ctrqctl.go`)
**Files modified:** `Makefile`

Steps:

7.1. Port `ctrqctl.go` → `cmd/taskmasterctl/main.go` (`package main`, `func main()` replaces `RunCLI`). Keep the full command surface and output formatting: `group|task|executions|output|metrics|health` with all verbs/flags/tabwriter output.

7.2. **Auth replacement (FR-C2, P6):** delete `getToken`, the JWT parsing, `~/.ctrq-token`, and the `golang-jwt` + reference-config imports. Add `resolveConfig()` implementing flag > env > `~/.taskmasterctl.json` (0600; warn to stderr if looser) for `url` (no default port guessing — empty url is a fatal usage error naming all three mechanisms) and `key`. Every request sets `Authorization: Bearer <key>`; `health` too (P2). On 401/403, print: `authentication failed: check the API key (minted in the admin panel) and that taskmaster has an auth.modules entry`.

7.3. **SSE follow (FR-C3):** the existing `runOutput` bufio line-scan over the response body already works for SSE from a plain `http.Get`-style request; keep it, just issue the request with the bearer header and no client timeout on that call.

7.4. Models: the CLI needs the response shapes; import `cmd184psu/unified-webapp/internal/taskmaster/models` (allowed: `cmd/taskmasterctl` is taskmaster's own binary; add it to the 5.4 grep allowlist — already done there).

7.5. Makefile: `build` gains `go build -o taskmasterctl ./cmd/taskmasterctl`; `build-rpi` gains the `GOOS=linux GOARCH=arm64` equivalent (`taskmasterctl-arm64-linux`); `clean` removes both.

**Done-check:** `go build ./cmd/taskmasterctl && make build && make build-rpi` succeed; `./taskmasterctl` with no args prints usage naming url/key mechanisms; `./taskmasterctl -url http://x health` without key fails with the auth guidance message (against no server: connection error is acceptable for the smoke; full e2e in Phase 8).

---

## Phase 8 — local-test, docs, acceptance sweep (FR-M6, FRD §10)

**Files modified:** `local-test/config.json`, `local-test/setup.sh`, `docs/USERGUIDE.md`
**Files created:** `docs/taskmaster.md`

Steps:

8.1. `local-test/config.json`: add `"taskmaster.test": "taskmaster"` to `host_routing`; add section
```json
"taskmaster": {
  "static_dir": "./web/taskmaster",
  "db_path": "./local-test/data/taskmaster/taskmaster.db",
  "groups": [
    { "name": "default", "pool_limit": 2, "allowed_types": [] },
    { "name": "shell-only", "pool_limit": 1, "allowed_types": ["shell"] }
  ],
  "allow_sudo": false
}
```
and `"taskmaster": { "pin_file": "" }` under `auth.modules` (LDAP + API key, matching multissh — this is what makes the API-key e2e and gate-login acceptance checks testable).

8.2. `local-test/setup.sh`: add `taskmaster.test` to `HOSTNAMES` (covers both the `127.0.0.1` and `::1` loops); add `"$LT/data/taskmaster"` to the `mkdir -p` list; add a line to the credentials header comment (`taskmaster.test: LDAP or API key`).

8.3. `docs/taskmaster.md`: module deep-dive — config reference (every field incl. `allow_sudo` default-false), scheduling semantics summary (5 s poll, pool limits, locks, repeat/cooldown), API table, SSE format, `output_file` teeing caveat (writes as the service user, outside the DB, no path confinement in v1), **shutdown semantics** (server shutdown cancels in-flight commands via `CommandContext` + 10 s `WaitDelay`; interrupted executions are recorded `failed` with the context error; a sudo task's root-owned process may outlive the server, since the unprivileged service cannot kill it — A4/R1/C5), **output replay-window semantics** (in-memory replay is available until ~1h *after an execution finishes*, reaped by a 5-minute-tick GC — completion-stamped expiry, so long-running tasks keep their full post-finish window; A3), manual `task_metrics` retention guidance (the `DELETE` from the reference README), CLI usage (key minting flow, url/key precedence), and a **Security** section reproducing/deep-linking the FRD §8 write-up (sudo coarseness, sudoers delegation, API-key flattening, inherent arbitrary exec, intended setuid-helper direction).

8.4. `docs/USERGUIDE.md`: add taskmaster to the module list, local-test table (hostname, auth), and CLI mention. Config exemplar: `local-test/config.json` is the designated current exemplar (per FRD §10 note) — 8.1 already updated it; do **not** touch stale root `unified-webapp.json`/`unified-webapp-example.json`.

8.5. **Acceptance sweep** (manual, recorded as a checklist in the executor's output):
1. `make build && make web && npm run typecheck && go test -race ./...` all green.
2. `bash local-test/setup.sh`; start server; browser → `http://taskmaster.test:8080` shows the **platform** login (LDAP `chris`/`ldap-test-1`), never a taskmaster-native login.
3. UI flow: create group, create task (`shell`, `{"shell":"for i in 1 2 3; do echo tick $i; sleep 1; done"}`), enqueue, watch live SSE output ticks, executions list shows success, metrics page populates. Sudo control absent (allow_sudo=false).
4. API checks: create task with `sudo:true` → 403 with clear error; create `exec`-type task in `shell-only` group → 400 with clear error.
5. CLI e2e: `taskmasterctl -url http://taskmaster.test:8080 -key varOO_vuQyged_rklN3ujsy2tgQAcEs-9Ln13hDIyh0 group list` / `task add` / `task enqueue` / `output <id>` (follows SSE) / `metrics` / `health` all succeed; wrong key → the guidance error.
6. Flip `allow_sudo: true`, restart: sudo checkbox appears; API accepts `sudo:true`.

**Done-check:** every item in 8.5 recorded pass; step 2–6 evidence (status codes / observed behavior) noted in the execution log.

---

## Pre-mortem (deliberate mode): 3 failure scenarios, mitigations baked in

**PM-1: The goleak gate fails on ported goroutines.**
*How it happens:* the reference leaks by design — package-global registry, unstoppable GC ticker, 1 h sleep-unregister goroutines, `runTask` goroutines nobody joins; any one of them survives `Close()` and the whole `cmd/server` test package turns red at Phase 5, with the cause buried three packages deep.
*Mitigations in the plan:* the goroutine inventory is enumerated and each has a named owner in `build.go`'s `Close` (3.2 stoppable GC, 3.3 WaitGroup + `Wait()` bounded by `CommandContext`+`WaitDelay`, P7 deletes the sleep-goroutine class outright); the Subscribe/MarkDone race — the one way an SSE handler goroutine could hang forever — is closed in 3.2 with a regression test; goleak `TestMain`s are added **per ported package** in Phases 3–4 so leaks are caught in the package that created them, before the Phase 5 integration gate; 5.3's Build/Close test exercises the full cycle including an in-flight execution.

**PM-2: SQLite locking/WAL misbehavior under the unified process and `-race` test parallelism.**
*How it happens:* `go test -race ./...` runs packages in parallel; a shared/fixed DB path or a second connection to the same file yields `SQLITE_BUSY`/locked-database flakes; in production, WAL sidecar files land wherever `db_path` points and the directory may not exist.
*Mitigations in the plan:* `SetMaxOpenConns(1)` + WAL preserved verbatim (single-writer by construction, FR-B5); ported tests keep the reference's `:memory:` DBs (2.3 — already immune to this failure mode) and every new file-backed test uses `t.TempDir()` (5.3); 5.1 `MkdirAll`s the DB directory before open; local-test uses its own `./local-test/data/taskmaster/` path; exactly one `db.Open` per built module and `Close` is the only closer.

**PM-3: Auth-stripping breaks the frontend's fetch/SSE layer.**
*How it happens:* the reference UI is built around token state — 401→`#login` redirect in `apiFetch`, Authorization headers on fetch-stream SSE; naive deletion leaves dead `#login` routes, an SSE reader that no longer authenticates (cookie vs header), or a UI that silently hangs when the platform session expires mid-use.
*Mitigations in the plan:* 6.2 defines the exact replacement 401 behavior (`location.reload()` → platform gate login); 6.5 replaces fetch-streaming with native `EventSource` (cookies ride along automatically) including explicit `output|status|done` listeners, error handler, and teardown-on-navigation; 6.6's grep done-check proves no token/`#login`/Bearer residue; acceptance step 8.5.2/3 exercises the gate-login → UI → live SSE path end-to-end in a real browser; the CLI keeps header-based SSE (FR-C3) which never had the browser limitation.

---

## Expanded test plan (deliberate mode, four tiers)

**Tier 1 — Unit** (`go test -race ./internal/taskmaster/...`, goleak TestMain in every package)
- Ported: db CRUD/locks/enqueue/metrics tests; worker poll/pool-limit/repeat-cooldown tests (mock executor); coordinator handler tests.
- New: sudo gating at executor level (false→execution fails with config error; true→`sudo` prepended); sudo gating at handler level (create 403, update-to-sudo 403); `allowed_types` enforcement (create 400, update-type 400, update-group-move 400, wrong-typed update field 400, empty list allows all — the D4 latent-bug regression test); ctx-cancel kills a running command promptly (bounded drain); OutputRegistry `FinishedAt`-keyed GC expiry + `StartGC` stop + Subscribe-after-MarkDone returns closed channel; SSE subscriber cap 503; capabilities endpoint; config round-trip (Phase 1.5).

**Tier 2 — Integration**
- `internal/taskmaster/build_test.go`: full Build → seed → enqueue → execute → SSE stream → Close cycle under goleak; build-failure path leak-free (5.3).
- `cmd/server` dispatcher tests: extend the existing goleak-gated `dispatcher_auth_test.go` config with a routed+protected taskmaster: unauthenticated `/api/groups` → gate 401/login; API-key request passes; `/healthz` open; module handler receives requests only through `svc.Gate` + `BodyLimit`.

**Tier 3 — End-to-end** (Phase 8.5)
- Browser: platform login → groups/tasks CRUD → enqueue → live SSE output → executions/metrics; sudo control hidden; protected module never shows a taskmaster-native login.
- CLI: `taskmasterctl` full command surface against `taskmaster.test` with the admin-panel-minted API key, including SSE `output` follow and the wrong-key error path.
- Negative API: sudo-while-disabled 403, disallowed-type 400 (curl, exact messages recorded).

**Tier 4 — Observability**
- Log surface (all prefixed `taskmaster:` where added): startup line with db path, poll interval, `allow_sudo` (5.1); worker poll errors; per-execution start/finish already logged via DB errors — add one info line in `runTask` finish: `taskmaster: exec %d task %q %s in %dms`; GC needs no logging.
- Operator runbook (in `docs/taskmaster.md`): confirm the worker loop is alive by (a) the startup log line, (b) `taskmasterctl task enqueue <t>` then `taskmasterctl executions <t>` showing pending→running within ~5 s, (c) `GET /api/health` returning `{"status":"ok"}` (DB-aware, authenticated) vs platform `/healthz` (process-only, open). A stuck loop shows enqueued executions pinned at `pending` past 2 poll intervals — the documented alarm condition.

---

## ADR

**Decision.** Port ctrq into `internal/taskmaster` as a **verbatim-first port with adapters** (Option A): chi router and `modernc.org/sqlite` retained, reference subpackage layout retained, taskmaster's own SSE fan-out retained with a platform-cap adapter (but JSON envelopes converged on `platform/response` — the how-to forbids duplicating platform helpers); all auth deleted in favor of the platform gate; lifecycle rebuilt around `Build`/`io.Closer` with instance-scoped state, a stoppable GC as the sole capture reaper (completion-stamped expiry), WaitGroup-joined executions, and `exec.CommandContext` + 10 s `WaitDelay` so `Close` deterministically kills (or abandons, for unkillable processes) in-flight commands; frontend rebuilt onto esbuild/EventSource/committed-JS conventions; CLI reborn as `taskmasterctl` with API-key bearer auth (flag>env>0600-file); `allow_sudo` (default false) enforced at handler and executor plus a `/api/capabilities` endpoint for the UI; `allowed_types` enforced at task create/update.

**Drivers.** (1) FRD minimal-modification mandate over a working reference; (2) hard platform invariants — module contract, goleak gate, zero-per-module-auth design; (3) Sonnet-builder executability of small mechanical steps.

**Alternatives considered.** Option B (restructure to ServeMux/flat-package/platform-broker/platform-response) — viable but rejected: rewrites every handler and test for uniformity alone, and `platform/broker`'s broadcast/snapshot model structurally mismatches per-execution replay-then-live-then-done streams, so it would *add* net new code in the highest-risk area. Sub-alternatives rejected: cancellable sleep-timer unregister (P7 — GC-only expiry deletes the goroutine class instead of taming it); unbounded `Close` drain without `CommandContext` (A4 option b — a wedged task would turn the goleak gate's failure mode into a suite hang and violate principle 3; killing in-flight commands on shutdown is the deterministic trade), and `CommandContext` without `WaitDelay` (R1 — pipe-inheriting grandchildren and unkillable root-owned sudo children would re-open the same suite-hang hole cancel was meant to close); `allow_sudo` piggybacked on task/group responses (P5 — pollutes every payload); `/healthz`-only health (P2 — loses DB-awareness the CLI wants); CLI token-cache file (P6 — API keys need no refresh, a cache is pure liability); keeping local `writeJSON/writeError` duplicates (A6 — would contradict `docs/adding-a-module.md`'s explicit don't-reimplement rule for an identical envelope).

**Why chosen.** It is the only option whose risk profile matches the FRD's own stance ("modifications minimal except where required"), it converts nearly all work into mechanical copy+edit steps with grep-able done-checks, and every required deviation (auth, lifecycle, gating, conventions) lands at an edge with a dedicated test.

**Consequences.** Positive: smallest reviewable diff; ported tests keep their value; regression risk concentrated in the four edges that get dedicated tests; deterministic shutdown even with wedged tasks. Negative (accepted): `go-chi/chi/v5` + `modernc.org/sqlite` + `testify` join `go.mod`; two router styles exist in-tree; server shutdown/restart kills in-flight task commands (recorded `failed`; operators of long tasks must expect re-runs after restart, and root-owned sudo processes may briefly outlive the server — R1); SSE cap logic is hand-rolled (mirroring, not sharing, broker semantics).

**Follow-ups.**
1. **Curated setuid-root helper** replacing the sudo flag with a fixed, audited allow-list of privileged operations — removes the sudoers dependency entirely (FRD §8, out of v1 scope).
2. **Scoped/per-module API keys** platform-wide, ending the key-flattening risk where any fleet key is taskmaster-grade (FRD §8.3).
3. `platform/fspath` confinement (or an allow-listed root) for `output_file` paths (FRD FR-B8 note).
4. Automatic `task_metrics` retention/pruning (documented-manual in v1).
5. Opportunistic convergence: evaluate chi→ServeMux once stable, as an isolated refactor (`platform/response` convergence already happens in Phase 4).

---

## Execution guidance for ralph

- **Model policy (D6):** this plan and critic review run on **Fable**; every builder/worker step runs on **Sonnet**; **never Opus 5**.
- Execute phases **strictly in order 1→8**; within a phase, steps in order. Each phase's **Done-check is the gate** — do not start phase N+1 with phase N's check failing.
- Never modify anything under `reference/` (read-only source); never add auth code, `signal.Notify`, or `go:embed` to `internal/taskmaster`; never touch root `unified-webapp.json`/`unified-webapp-example.json` (stale legacy exemplars).
- When a ported file needs an edit this plan doesn't name, prefer *no edit*; if compilation forces one, keep it minimal and note it in the step output for critic review.
- Phase 8.5 is manual verification: record concrete evidence (commands, status codes, observed UI behavior) — assertions without evidence don't close the phase.
- Open questions for the owner are tracked in `.omc/plans/open-questions.md`; none are blocking (all FRD choice points are resolved above, subject to consensus review).
