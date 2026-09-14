# Implementation Plan: smbedit — port of smbed into unified-webapp

**Status:** pending approval — ralplan consensus draft v4
**Date:** 2026-09-12
**Mode:** DELIBERATE (pre-mortem + expanded test plan included)
**Source FRD:** [`docs/smbedit-frd.md`](./smbedit-frd.md) — approved, decisions D1–D6 fixed
**Executor:** ralph (sequential, autonomous). Every task below is independently verifiable by a runnable command.
**Revision:** v4 incorporates three review rounds — see §10. v2 answered the Architect/Critic reviews of v1
(structure and coverage); v3 answered the iteration-2 reviews, which were entirely about the *pinned shell*
v2 introduced; v4 answers iteration 3, where both reviewers independently converged on the same two defects —
both of them, again, in shell that v3 newly introduced and reasoned about rather than ran. Every changed
command in v4 was **executed before being pinned**, and the two that reviewers reproduced were reproduced
here first: the failing `tsc` gate (rc=1) and the vacuous blast-radius gate (rc=0 with a committed rogue file).
**Shell contract:** all verification commands are written for **bash** (several use process substitution
`<(…)`, which `sh` does not provide). ralph must run them under `bash`.

---

## 1. Objective

Port the standalone `smbed` application (vendored read-only at `reference/smbed/`) into unified-webapp as a
new module named **smbedit**, following the existing module contract: a `Build(cfg) (http.Handler, error)`
package at `internal/smbedit/`, a static frontend at `web/smbedit/`, dispatch by `Host` header, operator
configuration in `~/.unified-webapp.json`, mutable state in `<data_dir>/state.json`.

This is a **straight port**. Every behavior delta is declared here; there are nine, not four.

**Deltas the FRD mandates:**

| # | Delta | FRD |
|---|---|---|
| D-1 | chi router → `http.ServeMux` method patterns; embedded FS → on-disk `staticHandler` | §5.4 |
| D-2 | `~/.smbed.json` split into operator config + `<data_dir>/state.json`; `listen_addr` removed | §5.3 |
| D-3 | All state mutation mutex-guarded; `state.json` written atomically at mode 0600 | §5.5 |
| D-4 | Vite → esbuild (React 18 sources unchanged); jest suite dropped | §5.7, D1, D3 |
| D-9 | chi's `Recoverer` → a **module-local panic-recovery wrapper** whose `ResponseWriter` forwards `Flush()` and `Unwrap()` | §5.4 |

**Further deltas — declared, not incidental. D-5…D-8 are downstream consequences of D-1…D-4:**

| # | Delta | Why | Task |
|---|---|---|---|
| D-5 | New **streaming** command seam alongside `runCommand`. smbed's `runCommand` is batch — `func(name string, args ...string) (string, error)` — and cannot express a tailed stream, so the Samba log path needs a second seam: `var runStreamingCommand = func(ctx context.Context, name string, args ...string) (io.ReadCloser, func() error, error)`. | FR-11 requires everything exec-ing external commands to sit behind a swappable var; the batch seam physically cannot cover `tail -F`. | T3.4 |
| D-6 | `cmd.WaitDelay` set on the tail process, and the child started in its **own process group** (`SysProcAttr{Setpgid: true}`, killed by negative PID). | Killing the direct child (`sudo`) orphans the `tail` grandchild; on a long-lived shared server those accumulate. smbed ended at process exit and never had to care. | T3.4 |
| D-7 | Ops-log hook wired **per-server** at construction instead of via the process-global `samba.Logf`. | A process-global mutated in a constructor is a latent conflict in a multi-module binary. | T3.3 |
| D-8 | On a `state.json` save failure the in-memory mutation is **rolled back** and the request 500s, so memory and disk never diverge. smbed kept the mutation in memory and 500'd, leaving the two out of sync until the next successful save silently persisted the earlier edit. **Ordering, pinned:** the `auto-disabled share(s)` ops-log entry is emitted **after** a successful save, not before it as the reference does (`reference/smbed/internal/api/server.go:152-157`) — otherwise a rolled-back mutation leaves the ops log asserting an auto-disable that never persisted. | §5.5's whole premise: state handling that was tolerable single-user is not tolerable on a shared long-lived server. | T2.2, T3.2 |

**One divergence accepted rather than fixed:** wrong-method requests to an API path return **404** in
production (the static handler owns `/` and claims the request before `ServeMux` can emit 405), where chi
returned 405. This matches the house convention already asserted in `internal/multissh/static_test.go:47-49`.
See §5 T3.1 and ADR consequences.

**What D-9 actually buys, stated precisely (corrected in v3).** An earlier draft claimed that without D-9 a
single handler panic would "kill all seven modules". That is **false**, and was measured: `net/http` installs
its own per-connection `recover()` (`net/http.(*conn).serve.func1`), so the process survives a handler panic
and subsequent requests are served normally — the *connection* is dropped mid-response and the client sees a
bare transport EOF with no status and no body. chi's `Recoverer` instead logged the stack and returned a clean
**500**. D-9 restores that 500. It is a client-visible fidelity item, not outage prevention, and FRD §5.4 now
mandates it directly. The subtle part is the implementation, not the motivation: see T3.1.

Requirements covered: **FR-1 … FR-18**, **TR-1 … TR-4**, acceptance criteria **§9.1 … §9.5**.

---

## 2. RALPLAN-DR summary

### 2.1 Principles

1. **Fidelity over improvement.** This is a port, not a redesign. Any behavior difference from smbed must be
   traceable to a named FRD clause and listed in §1's delta tables. No opportunistic refactors, no "while I'm
   here" fixes, no feature adds.
2. **Convention over invention.** Every structural choice copies the nearest existing precedent in this repo —
   `internal/multissh` for `Build()`/static serving/`ServeMux` routing/goleak style, `expandMultisshPaths` for
   `~`-expansion. Zero new dependencies; `go.mod` stays router-free.
3. **Green at every task boundary.** ralph runs unattended and sequentially: no task may leave the tree
   unbuildable, a test red, or a repo-wide command (`npm run build`, `make web`) broken — because the next
   task inherits that breakage with no human to notice.
4. **Testability without privilege.** Nothing in the test path shells out, mutates `$HOME`, requires `sudo`,
   or assumes Linux. External commands stay behind the swappable `runCommand` / `runStreamingCommand` vars;
   all filesystem state lives under `t.TempDir()`.
5. **Blast radius is bounded and enumerated.** Module code is new; shared files are touched only at the
   enumerated wiring points, and two of those edits are genuinely **mutating**, not additive: T5.4 changes
   root `tsconfig.json` compiler options that all three existing TS modules compile under, and T5.3 rewrites
   the existing `build` / `build:dev` script strings. Both are verified safe by re-running `make typecheck`
   and `npm run build` against the untouched modules. The must-not-touch list is in §9.

### 2.2 Decision drivers (top 3)

1. **Autonomous sequential execution.** ralph cannot backtrack cheaply. Ordering must guarantee that each task
   compiles and tests green on its own, that no task leaves a repo-wide command broken for later tasks, and
   that the riskiest work happens where a failure is diagnosable in isolation rather than tangled with five
   other unverified changes.
2. **The FRD already fixed the design.** D1–D6 settle the frontend stack, persistence model, test scope,
   `reference/` handling, and toolchain. The only remaining degrees of freedom are **package structure** and
   **sequencing** — so those are what this plan's options are about.
3. **Risk is concentrated in two places, not spread evenly.** ~80% of the work is mechanical transcription with
   ported tests that either pass or don't. The genuinely novel parts are (a) React under esbuild instead of
   Vite and (b) SSE + concurrency under a long-lived shared server. Sequencing must isolate those two so a
   failure in either cannot block or contaminate the mechanical bulk.

### 2.3 Viable options

#### Option A — Single package `internal/smbedit`, backend-first, frontend as one late phase, de-risked by an up-front throwaway spike  ✅ **CHOSEN**

All ported Go code lands in one package (`build.go`, `handler.go`, `state.go`, `samba.go`, `oplog.go`,
`picker.go` + `*_test.go`). Phases run: repo hygiene **+ frontend-toolchain spike** → platform config →
backend core → HTTP layer → dispatch wiring → frontend → docs → final verification.

- **Pros:** No cross-package export surface has to be invented — smbed's `samba` package takes
  `*config.Config` parameters throughout, and collapsing them removes that coupling. Internal tests that swap
  unexported vars (`reference/smbed/internal/samba/write_internal_test.go` overrides `runCommand`) stay
  in-package and port almost verbatim. Backend-first means the frontend phase lands against a backend whose
  API contract is already test-locked. The two risky areas (SSE/concurrency in Phase 3, esbuild in Phase 5)
  are in separate phases with separate verification gates. `package.json` / `tsconfig.json` / the committed
  bundle are touched exactly once each.
- **Cons:** One package of ~1,100 LOC + ~900 LOC of tests is larger than any single existing module file set —
  navigability rests on file naming discipline. **More importantly, the phase with the highest uncertainty
  (Phase 5's React-under-esbuild toolchain, the only part of this port with no working precedent in either
  repo) sits last**, so a fundamental toolchain problem would surface after all backend work is committed.
  This is mitigated directly by T0.2, a throwaway spike that runs the exact esbuild and `tsc` invocations
  against the real sources before any other work begins, and commits nothing. The residual late-phase risk —
  a UI-visible API mismatch — is small by construction: the API shape is byte-compatible with smbed's, and
  the frontend is a near-verbatim copy of a UI that already works against it.

#### Option B — Mirror smbed's sub-packages (`internal/smbedit/{config,samba,oplog,api}`), backend-first

Same phase order, but the four smbed packages are preserved as sub-packages under `internal/smbedit/`.

- **Pros:** Diff against `reference/smbed/` is nearly 1:1, making review trivial. Package boundaries document
  responsibility. Closest to a literal transcription, so the lowest chance of an accidental semantic change.
- **Cons:** Four packages for ~900 LOC, each needing an exported surface that exists only to be reachable from
  its sibling (`config.Config`, `samba.Render`, `oplog.Log`, …), plus a `build.go` at the root that has to
  thread state through all of them. The FRD (§5.1) explicitly calls this "acceptable but not required" and
  notes "the smbed sub-packages are small". No other module in this repo splits below one package except
  `multissh/sshproxy`, which exists because it is genuinely swappable behind an interface. Critically, the
  sub-package layout makes the in-package tests that swap unexported `runCommand` awkward to distribute — the
  seam and its test must stay in the same package, which pins `samba` and its 356 LOC of tests together while
  the handler tests that also need to control it live elsewhere.

#### Option C — Vertical slices (one page end-to-end per phase: Shares, Globals, Preview, Logs, Settings)

Each phase delivers one UI surface with its backend endpoints, tests, and bundle rebuild.

- **Pros:** Each phase produces something demonstrable in a browser. Frontend/backend mismatches surface
  immediately, per-feature. Attractive when requirements are uncertain.
- **Cons:** Wrong shape for a port — requirements are not uncertain, the destination behavior is already
  running in `reference/smbed/`, and no slice has independent user value because the app is useless until all
  of it works. Cross-cutting foundations (state store + mutex, oplog, samba render/parse, static handler,
  esbuild setup) would each have to be built partially in slice 1 and extended in every later slice. The
  committed `bundle.js` would be regenerated and recommitted five times, producing five large spurious diffs.
  Worst of all it **interleaves the two riskiest areas** (Phase 3 SSE and Phase 5 esbuild) into every slice,
  which is exactly what driver #3 says not to do.

### 2.4 Choice and rationale

**Option A.** It is the only option that satisfies all three drivers simultaneously. Driver 2 (design already
fixed) removes Option C's raison d'être — there is nothing to learn from early feedback that the reference
implementation does not already tell us. Driver 3 (risk concentration) rules Option C out affirmatively:
vertical slicing spreads the two hard problems across all five phases. Between A and B, driver 1 decides:
Option B buys 1:1 diffability at the cost of an export surface that exists only for internal reachability,
and it complicates the placement of the unexported command seams that FR-11 depends on. The FRD already
pre-blessed the single-package layout. Option A also keeps in-package tests — which is what lets TR-1's
"port every Go test" be mostly copy-paste rather than a rewrite, directly protecting the "coverage must not
regress" clause.

Structure discipline replaces package boundaries: one file per former smbed package, same names, plus
`build.go` and `handler.go`. If any file exceeds ~400 LOC during execution, split by concern within the
package (e.g. `handler_logs.go`) — never introduce a sub-package without re-opening this decision.

---

## 3. Pre-mortem — it is three weeks later and the port failed. Why?

### Scenario 1 — The React bundle builds clean and the page is blank

**Failure story.** `npm run build` exits 0, `bundle.js` is committed, `make typecheck` is green, and the browser
shows an empty `<div id="root">` with `ReferenceError: process is not defined` in the console. Three distinct
Vite-isms are being relied on without anyone noticing:

- **`process.env.NODE_ENV`.** `react-dom` branches on it in ~40 places. Vite defines it; bare esbuild does not,
  leaving a literal `process.env.NODE_ENV` in the browser bundle. This throws at module-evaluation time — before
  any React code runs — so the failure mode is a blank page with no React error boundary and no partial render.
  **And the obvious fix has a silent-failure mode of its own:** the define's value must reach esbuild as a
  *JSON string*, not a bare token. Get the shell quoting wrong and esbuild substitutes the *identifier*
  `production`, producing a bundle that contains no `process.env` (so a naive grep passes) and dies at load
  with `ReferenceError: production is not defined`. Exact quoting is specified in T5.3.
- **JSX runtime selection.** `App.tsx` and every other component use JSX **without** `import React`. esbuild
  defaults to the classic `React.createElement` transform unless it reads `"jsx": "react-jsx"` from a tsconfig
  it discovers by walking up from the source file, or is told `--jsx=automatic`. If discovery does not resolve
  the way it is assumed to, the build still succeeds and the page dies at runtime with `React is not defined`.
- **tsconfig collision.** The root `tsconfig.json` currently serves three vanilla-TS modules with
  `"moduleResolution": "bundler"` and `lib: ["ES2020","DOM"]`. Adding TSX needs `"jsx"`, `@types/react`, and
  `DOM.Iterable` for React 18's iterator usage. (CSS imports are *not* a new problem here: `web/multissh/js/css.d.ts`
  already declares `*.css` ambiently and is already inside the root `include` glob, so the declaration is
  project-wide today. A local `web/smbedit/src/css.d.ts` is therefore **redundant in-repo on the pinned
  TypeScript `^5.4.0` — but required out-of-tree, and required on TypeScript 7 regardless** (v4 correction:
  TS 7 raises `TS2882` on the side-effect `import './styles.css'`, and the ambient declaration next door is out
  of scope once you leave the repo's `include` set — which is exactly what broke T0.2's spike gate). Created
  either way, so the directory is self-contained if the include list is ever narrowed and the module survives a
  TypeScript major bump. Duplicate shorthand ambient module declarations were verified harmless.)

**Mitigations (all are plan tasks, not hopes).**
- **T0.2 runs the whole toolchain as a throwaway spike before any other work**, against the real sources in a
  scratch directory with its own `node_modules`, asserting all four bundle properties and a clean `tsc`
  (expected error set: empty). If React-under-esbuild is going to fail, it fails on day one with nothing
  committed — not in Phase 5 after the backend is done.
- Exact define quoting specified in T5.3, and verified by its **effect** rather than its presence: a correct
  string define makes esbuild constant-fold React's `NODE_ENV` comparisons and drop every dev-only
  `Warning: …` string, so `grep -c 'Warning: ' bundle.js` → `0` distinguishes a correct define from an
  identifier define. An identifier define leaves the dev build in.
- `--jsx=automatic` passed explicitly **in addition to** `"jsx": "react-jsx"` in the root tsconfig, so the
  bundle never depends on tsconfig discovery (T5.3, T5.4). Verified by
  `grep -cE 'jsx_runtime|react/jsx-runtime' bundle.js` → `> 0`.
- Ordering: sources land (T5.2) before the build script that consumes them (T5.3), and `make typecheck` gates
  T5.4 before the bundle is committed in T5.5 — so a tsconfig problem surfaces in a task whose only change is
  tsconfig.
- Residual accepted: `react` + `react-dom` add **~299 KB of JS and ~16 KB of CSS** to the committed tree
  (measured, unminified — `--minify` is deliberately not used, matching the multissh precedent). This is the
  price of D1 and is recorded, not mitigated.

### Scenario 2 — SSE works in tests, stalls in production, and leaks `tail` processes

**Failure story.** `GET /api/logs/ops/stream` delivers the backlog and then goes silent forever; the Logs page
looks frozen. Meanwhile `ps` on the Linux host shows a growing pile of `tail -F` processes, one per page
load, each holding a file descriptor. Four contributing causes:

- Something in the response chain buffers. `middleware.Wrap` is a verified pass-through, but a fronting
  proxy (nginx `proxy_buffering on` is the default) swallows the stream invisibly — the connection stays open,
  so nothing errors.
- The `http.Flusher` type assertion silently fails because a wrapper in the chain does not implement it, and
  the handler returns a JSON 500 the EventSource client reports only as a generic `onerror`. **This is no
  longer hypothetical: D-9's own recovery wrapper is exactly such a wrapper.** Tracking `wroteHeader` — which
  it must, to avoid writing a 500 over a committed stream — means embedding the `http.ResponseWriter`
  *interface*, and Go promotes only that interface's methods, so `Flush()` is lost unless written out by hand.
  Measured: `wrapped-rw flusher=false`, `passthrough-rw flusher=true`. The plan's own required fix would
  therefore break FR-8 **completely and in production only**, since tests that mount the bare mux never see the
  wrapper. Hence two hard requirements: T3.1's wrapper forwards `Flush()`/`Unwrap()` and asserts it directly,
  and T3.4's SSE tests mount `srv.Handler()`.
- **The grandchild problem.** `handleSambaLogStream` runs `sudo tail -n 200 -F <path>`. `cmd.Process.Kill()`
  signals `sudo` — the direct child — and `sudo` does **not** forward SIGKILL to `tail`. The grandchild is
  orphaned, keeps the file open, and keeps running. In a single-user binary that ended at process exit; in a
  long-lived shared server it accumulates one per Logs-page visit.
- If `scanner.Scan()` blocks on a live `-F` stream, the goroutine parks until the write fails — which on a
  dead-but-unclosed socket may be never.

**Mitigations.**
- Start the child in its own process group (`SysProcAttr{Setpgid: true}` in a `//go:build unix` file — works
  on both darwin and linux) and kill the **negative PID** so the whole group including `tail` dies; set
  `cmd.WaitDelay` so a child that survives is force-reaped on a bounded timer (T3.4, D-6).
  **Honest residual:** this cannot be verified in CI without actually shelling out, which principle 4 forbids.
  What *is* tested is that the handler returns and its goroutine exits on disconnect; that the process group
  is correctly torn down is asserted by construction and by the manual Linux pass (§9.4), not by an
  automated test. The plan does not claim otherwise.
- Port smbed's `TestOpsLogStream_SendsBacklogThenReturnsOnDisconnect` and extend it to assert the handler
  **returns** when the request context is cancelled. **Harness is mandated, not incidental:** an
  `httptest.ResponseRecorder` read from the test goroutine while the handler writes from another races on
  `rr.Body` and will trip `-race`. Use `httptest.NewServer` + a real `http.Client` with a cancellable request
  (T3.4).
- Adopt the repo's per-test goleak style — `defer goleak.VerifyNone(t)`, as in
  `internal/multissh/build_test.go:41` — from T2.1 onward on every test that starts a goroutine, and
  explicitly on the SSE tests and the `Build()` test. Note: the ported `TestSubscribe_*` cases **must** call
  their `cancel()` or they will fail their own goleak check.
- Keep the `http.Flusher` check and the exact SSE headers unchanged (FR-8), and document in `docs/smbedit.md`
  that a proxy must disable buffering and preserve the `Host` header for these two endpoints — mirroring the
  warning already in `docs/multissh.md` (T6.2, FR-17, §5.6).
- Manual verification is an explicit, *operable* acceptance step (T7.1 step 7): a fresh instance's ops log is
  **empty**, so a bare `curl -N` would hang forever. The check opens the stream in the background, triggers a
  real ops-log entry with a `PUT`, and asserts a `data:` line arrives **after** the trigger.

### Scenario 3 — `-race` trips in CI, or `state.json` is found truncated after a crash

**Failure story.** `make test` fails intermittently with a data race between two `PUT /api/shares` goroutines,
or — worse, and silently — an operator finds `state.json` as a zero-byte file after the host lost power
mid-save, and every share definition is gone. smbed's handlers mutate `s.cfg` with no lock and call
`os.WriteFile` directly (truncate-then-write); that was tolerable for a single-user binary launched on demand
and is not tolerable here (§5.5). Specific ways a naive port reproduces it:

- The mutex is added but handlers still read a field, compute, and write it back across three separate
  critical sections — the lock is present and the lost update happens anyway.
- `DisableMissingPaths` mutates the shares slice **in place** while another request holds a reference to the
  same backing array.
- The atomic write creates its temp file in `os.TempDir()` rather than `data_dir`, so `os.Rename` crosses a
  filesystem boundary, fails with `EXDEV` on exactly the deployment hosts where `/data` is a separate mount,
  and the save silently falls back or errors only in production.
- The rename is durable but the **directory entry** is not: `os.Rename` alone does not guarantee the new name
  survives power loss without an fsync of the *parent directory*. This scenario is explicitly about power
  loss, so that gap matters.
- A leftover `state.json.tmp` from a crashed save confuses the next load, and repeated crashes accumulate
  `state-*.json.tmp` files forever.
- A save fails (disk full, permissions) and the in-memory state keeps the mutation — memory and disk diverge,
  and the *next* successful save silently persists the edit the operator was told had failed.

**Mitigations.**
- A single unexported `store` type owns the mutex, the in-memory state, and the save path. Handlers never
  touch state fields directly — they call store methods that take the lock once for the whole
  read-modify-write (T2.2). Any method returning a slice returns a **deep copy**, so no caller ever shares a
  backing array with the store.
- Atomic write: `os.CreateTemp(dataDir, "state-*.json.tmp")` (already 0600 by contract — the explicit
  `Chmod(0o600)` is belt-and-braces and is kept) → write → `Sync()` → `Close()` → `os.Rename` **within
  `data_dir`**, guaranteeing a same-filesystem rename → **fsync the parent directory** so the rename itself is
  durable. The temp file is removed on every error path, and `load()` sweeps any stale `state-*.json.tmp`
  left by an earlier crash (T2.2, FR-9, S9).
- **Save failure rolls back the in-memory mutation** and returns 500 with an ops-log entry, so memory and disk
  never disagree (D-8, T2.2/T3.2). This is a declared divergence from smbed, justified by §5.5.
- Load ignores stray `*.tmp` files and treats a missing `state.json` as first-run defaults; a malformed
  `state.json` is a hard `Build()` error, not a silent reset to defaults — losing an operator's shares
  silently is the one failure worth failing loudly over (T2.2, T3.5).
- TR-2 concurrency test: N goroutines issuing interleaved `PUT /api/shares` + `PUT /api/globals` +
  `GET /api/config` against one handler, asserting no race under `-race` and that the final `state.json`
  parses (T3.2). `make test` already runs `-race` (verified in the Makefile), so this is enforced, not advisory.

---

## 4. Expanded test plan

Mapped to FRD §7 (TR-1…TR-4) and §9 acceptance criteria.

**Standing convention (supersedes v1's `-run` filters).** Per-task verification runs the **full package**:
`go test -race -count=1 ./internal/smbedit/`. v1 used `-run` name filters that matched none of the actual
ported test names (`TestAddAndSnapshot`, `TestSnapshot_BoundedByMax`, `TestSubscribe_ReceivesNewEntries`,
`TestSubscribe_CancelStopsDelivery`, `TestLoadDefaults_NoFile`, `TestSaveAndReload`, `TestLoadCorrupt`,
`TestDisableMissingPaths`, `TestListOptFolders_ReturnsDirectories`) — `go test -run 'Oplog|OpLog|Log'` exits
**0** with "no tests to run", so those gates were vacuous. Where a task needs to prove its *own* tests ran and
not merely that the package is green, it asserts a **test-function count**:

```bash
[ "$(go test -list '.*' ./internal/smbedit/ | grep -c '^Test')" -ge N ]
```

**Why `-list` and not `grep -c '^=== RUN'` (v3 change).** `=== RUN` lines include **subtests**
(`=== RUN   TestX/case`), so one table-driven test with N cases satisfies a floor of N while porting nothing —
the metric could be inflated by exactly the vacuousness it exists to prevent. Measured on `internal/multissh`:
38 top-level `func Test`, but 52 `=== RUN` lines. `go test -list` enumerates **top-level test functions only**,
which is what "the tests were actually ported" means. The gate is also written as a runnable `[ … -ge N ]`
test rather than prose, so ralph can execute it rather than interpret it. The floors are cumulative over the
package and are stated self-consistently in §5 (4 → 13 → 27 → 30); measured reference inventory is oplog 4,
config 6, samba 12, write_internal 2, picker 1, so realistic cumulative counts land at 4 / 17 / 31 / 34 and
every floor clears with headroom. The paired plain `go test -race -count=1` run — which `-list` does **not**
replace, since `-list` compiles but runs nothing — is what proves they pass.

### 4.1 Unit — `go test -race -count=1 ./internal/smbedit/`

| Area | Cases | Ported from | Req |
|---|---|---|---|
| oplog | ring-buffer bound at 500, `Add`/`Addf`, `Snapshot` returns a copy, `Subscribe` fan-out + `cancel` idempotence (each subscriber test **must** `cancel()` — goleak is on from this task), concurrent add/subscribe | `internal/oplog/oplog_test.go` (70 LOC, 4 tests) | TR-1 |
| samba render | golden `smb.conf` for a known state; global ordering preserved; disabled shares omitted; `share_owner` applied | `internal/samba/samba_test.go` (276 LOC) | TR-1, FR-3 |
| samba parse | case-insensitive keys, whitespace collapsing, line continuations, `read only`/`writable`/`writeable`, `#`/`;` comment skipping, `ReadConf` on a missing file | same | TR-1, FR-5 |
| samba write/restart | backup naming, direct write, sudo-`install` fallback, restart chain `systemctl smbd` → `systemctl smb` → `service smbd restart`, `Logf` emission — all via the in-package `runCommand` seam, **never** shelling out | `internal/samba/write_internal_test.go` (80 LOC) | TR-1, FR-4, FR-11 |
| state store | defaults on first run (17 globals, empty shares); JSON round-trip; `DisableMissingPaths` on load; atomic write leaves mode 0600, no `.tmp` residue, and a parent-dir fsync; stale `state-*.json.tmp` swept on load; malformed file errors; deep-copy isolation of returned slices; **save-failure rollback** leaves memory == disk (D-8) | `internal/config/config_test.go` (138 LOC, **6** tests) + new | TR-1, TR-2, FR-6, FR-9 |
| picker | immediate subdirs only, hidden dirs skipped, files skipped, never recurses, configurable root | `internal/api/picker_test.go` (47 LOC) | TR-1, FR-7 |
| platform config | `SmbeditConfig` defaults; `~`-expansion of `static_dir`/`data_dir`/`picker_root`; empty `picker_root` → `/opt`; `WriteDefault` output contains a `smbedit` section | new | TR-2, FR-10 |

### 4.2 Integration — in-process `httptest` against the real handler

| Area | Cases | Req |
|---|---|---|
| All 13 route registrations | Ported `server_test.go` (269 LOC): config GET/PUT, shares round-trip + auto-disable, globals round-trip, preview `text/plain`, version, import (success / missing file / auto-disable), bad-JSON 400 | TR-1, FR-2 |
| Config API delta | `GET /api/config` response contains **no** `listen_addr` key; `PUT /api/config` with a `listen_addr` field is accepted and ignored (not a 400) | TR-2, FR-2, §5.3 |
| Persistence | Every mutating endpoint leaves `<data_dir>/state.json` updated and re-loadable by a fresh store | TR-2, FR-9 |
| Save-failure rollback | A store whose save path is made unwritable returns 500 **and** leaves the in-memory state unchanged (D-8) | FR-9 |
| Concurrency | N=16 goroutines × interleaved PUT/GET; no race under `-race`; final `state.json` parses | TR-2, FR-9 |
| Panic recovery | A handler forced to panic returns **status 500 with a body**, and the stack is written to the module's logger. The "process survives" half of v2's criterion is **dropped as vacuous** — `net/http` recovers handler panics on its own, so that clause passes identically with D-9 absent and proves nothing (D-9) | §1 D-9, §5.4 |
| Flusher survives the D-9 wrapper | `Handler()`'s response writer still satisfies `http.Flusher` — asserted directly, not only implied by the SSE tests, because a `wroteHeader`-tracking wrapper silently loses the promotion | FR-8, §5.4 |
| SSE ops stream | backlog delivered first, then a live entry, then handler returns on context cancel — via `httptest.NewServer` + real client mounting **`srv.Handler()`** (the full wrapped chain, **not** the bare mux), **never** a shared `ResponseRecorder`; `goleak.VerifyNone(t)` | TR-1, FR-8 |
| SSE samba stream | 400 when `samba_log_path` empty; command construction exercised through `runStreamingCommand`; handler returns and its goroutine exits on disconnect — also mounted via `srv.Handler()` | TR-1, FR-8 |
| Static handler | real file served; `/api/nope` → 404 JSON (**not** index.html); unknown non-API GET → index.html; directory path → no listing; non-GET miss on a non-API path → 405 | TR-2, FR-15 |
| Wrong-method on API paths — **both servers** | *No static dir* (test construction): `ServeMux` emits **405**. *With static dir* (production): the `/` pattern claims the request first and the static handler emits **404 JSON**. Both asserted, so the divergence is pinned rather than discovered | FR-2, FR-15 |
| `Build()` | fails on empty/missing/non-directory/unreadable `static_dir`; creates `data_dir` when absent; fails when `data_dir` cannot be created; initialises a default `state.json`; starts no goroutines (`goleak.VerifyNone`) | TR-2, FR-1 |
| Dispatch | `cmd/server/dispatch_test.go` extended: `"smbedit"` builds behind a routing entry and serves; a broken smbedit config yields a 503 for its hostname only; `"not-a-module"` behavior unchanged | TR-3, §9.5 |

### 4.3 Frontend / build

| Check | Command | Req |
|---|---|---|
| Type safety across all TS + TSX | `make typecheck` | TR-4, §9.2 |
| Bundle produced deterministically without Vite | `npm run build && git status --porcelain web/smbedit/js/` | §9.2 |
| No Vite-isms survive | `grep -c 'process\.env' web/smbedit/js/bundle.js` → `0`; `grep -c 'import\.meta' web/smbedit/js/bundle.js` → `0` | Pre-mortem 1 |
| `NODE_ENV` define resolved to a **string**, not an identifier | `grep -c 'Warning: ' web/smbedit/js/bundle.js` → `0` (dev-only React code constant-folded away) | Pre-mortem 1, T5.3 |
| Automatic JSX runtime in use | `grep -cE 'jsx_runtime\|react/jsx-runtime' web/smbedit/js/bundle.js` → `> 0` | Pre-mortem 1 |
| Existing modules still compile and bundle | `make typecheck` + `npm run build` after the T5.3/T5.4 shared-file edits | Principle 5 |
| npm-free build | `make build` (it invokes only `go build`; no npm dependency to park) | G4, §9.2 |

### 4.4 e2e smoke — live binary, `Host`-header routed (no `/etc/hosts` needed)

**Prerequisites:** **bash** (not `sh`); `jq`, `curl`, `lsof` installed; TCP port free (the script picks one and
fails loudly if it is taken).
Full operable script is T7.1 step 7 — it backgrounds the binary, polls for readiness, bounds every `curl`
with `--max-time`, generates a live ops-log entry before asserting on the stream, and tears down via `trap`.

1. `HEAD /` → `200`, `text/html`.
2. `GET /js/bundle.js` → non-empty JS.
3. `GET /api/config | jq -e 'has("listen_addr") | not'` → true.
4. `GET /api/version` → `{"version":…}`.
5. `GET /api/preview` → rendered `smb.conf` text.
6. `GET /api/definitely-not-a-route` → `404`.
7. **SSE liveness:** open `/api/logs/ops/stream` in the background, then `PUT /api/shares` to generate an ops
   entry, and assert a `data:` line appears **after** the trigger — proving nothing in the chain buffers.
8. Browser pass (§9.3, manual): add share via folder picker → edit a global → Preview shows both → Save →
   `state.json` on disk reflects both → Import a sample `smb.conf` → staged data appears → the ops-log stream
   shows the import entry live → toggle theme → reload persists it.

### 4.5 Observability

| Signal | Where | Purpose |
|---|---|---|
| Boot log line `registered ( http://smbedit-test… ) → smbedit` | dispatcher, existing | Confirms routing took effect |
| `Build()` error → 503 body naming the module and cause | `unavailableHandler`, existing | A misconfigured smbedit is visible in the browser, not just the log |
| Ops log entries: save & restart requested, write failure, backup path, restart attempt outcomes, import summary, auto-disabled share names, **state save failure** | `oplog`, streamed to the Logs page | The in-band failure channel required by §5.8 — "reports failure in the UI without crashing the module" (§9.4) |
| Samba daemon log tail | `/api/logs/samba/stream` | Post-restart diagnosis on Linux |
| Module-local panic recovery logs the stack and returns 500 (D-9) | `smbedit.Handler()` | Without it `net/http` recovers silently and drops the connection: the operator sees a client-side EOF and **nothing in the log**. D-9 makes the panic both diagnosable server-side and reportable client-side |
| `goleak.VerifyNone(t)` per test | test-time only | Turns an SSE goroutine leak into a red build |

### 4.6 Explicitly not tested

- The jest/testing-library suite (`App.test.tsx`, `test-setup.ts`) is **not** ported — decision D3. The frontend
  safety net is `make typecheck` plus the integration tests that lock the API contract the UI consumes.
- **Process-group teardown of the `sudo tail` grandchild (D-6)** cannot be asserted without shelling out, which
  principle 4 forbids. Covered by construction + the manual Linux pass (§9.4), not by CI.
- **The verification harness itself.** Three consecutive review rounds found defects not in the plan's design
  but in the plan's own shell — a `kill 0` that signalled the caller's process group, a `head -c` that killed
  `curl` with EPIPE under `pipefail`, a `git status` allow-list that could never match, an unpinned `tsc` gate
  that could not exit 0, and a blast-radius gate that could not fail. There is no test suite for this document,
  and the pattern is consistent: **in every round the defects were in the shell that round had just
  introduced**, never in shell that had already been executed once. The mitigation is T0.2 step 6: **dry-run
  the plan's novel shell constructs against a stub on day one** — now three dry-runs, widened in v4 to cover
  the blast-radius gate — so they fail in a five-minute throwaway task rather than at task 22. The standing
  rule this implies: a gate with no escape hatch must be executed before it is pinned.

---

## 5. Ordered task list

Conventions for every task: paths are repo-relative to `/opt/unified-webapp-smbedit`; **verification** commands
run from the repo root **under bash** and must exit 0; a task is done only when its verification passes **and**
`go build ./...` still succeeds. Verification commands are **idempotent and re-runnable** — a failed task can be
retried without manual cleanup. Several use process substitution `<(…)`, which is a bash feature: running them
under `sh` produces a syntax error, not a failed assertion.

### Phase 0 — Repo hygiene and up-front de-risking

**T0.1 — Untrack the reference checkout, fingerprint it, and record the run's baseline commit**
- Edit: `.gitignore`
- Add a `reference/` entry with a one-line comment explaining it is a read-only vendored source checkout that
  is never built or imported. Place it near the existing `data` / `node_modules/` entries. **In the same edit
  add `/.smbedit-reference.sha` and `/.smbedit-base.sha`**, the two baseline files created below.
- **Then capture a content fingerprint**, because once `reference/` is gitignored a `git diff -- reference/`
  guard is vacuous — it is empty by construction and would pass even if the directory were deleted:
  ```bash
  find reference -type f ! -name .DS_Store -print0 | sort -z | xargs -0 shasum | shasum > .smbedit-reference.sha
  ```
  T7.1 step 8 re-computes and compares. Also record the resulting hash in the task's completion note, as a
  second copy independent of the file.
- **Then record the commit this run starts from (v4 addition):**
  ```bash
  git rev-parse HEAD > .smbedit-base.sha
  ```
  T7.1 step 8 diffs against it. Record the hash in the completion note too, for the same reason as the
  fingerprint — a second copy independent of the file.
- **Why a baseline commit is needed at all (v4).** T7.1 step 8's blast-radius gate is the enforcement point for
  §9.5, and until v4 it could not fail once ralph committed anything. `git diff HEAD` compares the working tree
  to `HEAD`, so a commit moves `HEAD` and empties the diff. This plan **instructs** mid-run commits — T5.5 says
  to commit Phase 5 per repo convention — so the normal path made the gate vacuous, not some unlikely one.
  Reproduced in a scratch repo: with an unauthorized edit committed, v3's two-source gate reported
  `UNEXPECTED=[]` and **exited 0**; the v4 three-source gate exits 1 and names the file. A ref captured at the
  start is the only source that sees committed work, because it is the only one that does not move.
- **Why this exact form (v3 change).** The file lives **in the repo** and is gitignored, not in `/tmp`: a
  multi-hour unattended run can outlive `/tmp` reaping, and a *missing* baseline would make T7.1's `diff` fail
  for the wrong reason. `! -name .DS_Store` stops a Finder visit to `reference/` from forging a mismatch on
  darwin. `-print0 | sort -z | xargs -0` keeps the pipeline correct for paths containing spaces (there are none
  today — 67 files — but the guard costs nothing and its absence is a silent wrong answer, not an error).
  Verified on this machine: stable across runs, and it genuinely discriminates — with `reference/` absent it
  yields the SHA-1 of empty input rather than hanging or passing.
- Verify: `git check-ignore -v reference/smbed/go.mod` prints a `.gitignore` match;
  `git check-ignore -v .smbedit-reference.sha` and `git check-ignore -v .smbedit-base.sha` likewise;
  `git status --porcelain` lists neither `reference/` nor either `.sha` file;
  `test -s .smbedit-reference.sha && test -s .smbedit-base.sha`; and
  `git cat-file -e "$(cat .smbedit-base.sha)^{commit}"` — the recorded ref resolves to a real commit.
- Covers: D4, §3.

**T0.2 — Throwaway spike: prove the toolchain *and this plan's own shell* run on this box**
- Creates and deletes: `/tmp/smbedit-spike/` only. **No repo file is added or modified by this task**, and —
  unlike v2's version — nothing in it reaches outside that directory, so no cleanup command needs to touch the
  repo. The task is read-only with respect to `/opt/unified-webapp-smbedit`.
- Two things have no working precedent: React-under-esbuild (the toolchain), and the non-trivial shell this
  plan pins for its own gates. Option A puts the first of those last, and three review rounds found defects in
  the second. This task proves both on day one, at a cost of minutes.
- **The spike is fully self-contained (v3 change).** v2 copied sources to `/tmp` but ran `npm install` with no
  `cd`, so it installed into the *repo's* `node_modules`. Node, esbuild, and `tsc` resolve from the importing
  file's directory upward — `/tmp/smbedit-spike/src` → `/tmp/smbedit-spike` → `/tmp` → `/` — which never
  reaches the repo. Measured result of v2's form: `Could not resolve "react"` from esbuild and ~10
  `TS2307: Cannot find module 'react'` from tsc, i.e. the gate failed for a reason unrelated to what it tests,
  at task 2 of 22, behind a stop-and-report instruction. It also forced a `git checkout -- package.json
  package-lock.json` that contradicted the task's own no-repo-mutation contract.

  ```bash
  set -euo pipefail
  SPIKE=/tmp/smbedit-spike
  rm -rf "$SPIKE" && mkdir -p "$SPIKE/src"      # rm -rf first: re-runnable after a failure
  cp /opt/unified-webapp-smbedit/reference/smbed/web/src/*.tsx \
     /opt/unified-webapp-smbedit/reference/smbed/web/src/*.ts \
     /opt/unified-webapp-smbedit/reference/smbed/web/src/*.css "$SPIKE/src/"
  rm -f "$SPIKE/src/App.test.tsx" "$SPIKE/src/test-setup.ts"
  printf 'declare module "*.css";\n' > "$SPIKE/src/css.d.ts"   # v4: see "the CSS shim" below
  cd "$SPIKE"
  npm init -y >/dev/null
  npm install react@^18.2.0 react-dom@^18.2.0 @types/react@^18.2.0 @types/react-dom@^18.2.0 \
    typescript@^5.4.0 esbuild@^0.28.0
  ```
- **Every version is pinned to mirror the repo's `package.json` (v4 change).** v3 left `typescript` and
  `esbuild` bare. On this box that resolved to **TypeScript 7.0.2** while the repo pins `^5.4.0` — so the gate
  gated a compiler the project will never run, free to fail where the repo passes and to pass where the repo
  fails. It also made step 5's size comparison meaningless: drift was guaranteed by construction when the
  dependency set was unpinned, which is precisely what that assertion is supposed to detect. Measured after
  pinning: `typescript@^5.4.0` → 5.9.3, `esbuild@^0.28.0` → 0.28.2.
- **The CSS shim (v4 change).** `App.tsx:10` does a side-effect `import './styles.css'`, and
  `reference/smbed/web/` contains **no `.d.ts` of any kind** — Vite supplied that declaration at build time;
  bare `tsc` does not. The shim mirrors exactly what T5.1 creates in the real tree, so the spike typechecks the
  same file set Phase 5 will produce and proves the shim works before T5.1 commits to it. **No change to the
  `tsc` file list is needed:** the existing `src/*.ts` glob already matches `src/css.d.ts` — verified, since
  every `.d.ts` is a `.ts`. (The iteration-3 review suggested adding `src/*.d.ts` explicitly; that was measured
  to be redundant — it passes the same file twice — so the shim is created but the command is left alone.)
- **Step 3 — bundle.** Run the **shell form** of the define. It differs from the form that appears in
  `package.json`, and the difference is not cosmetic:

  ```bash
  npx esbuild src/main.tsx --bundle --target=es2020 --jsx=automatic --log-level=warning \
    --define:process.env.NODE_ENV='"production"' --outfile=bundle.js
  ```

  T5.3 specifies the same define **as raw JSON text** (`'\"production\"'`), where `\"` is a JSON escape that
  npm unescapes back to `"` before the shell ever sees it. Pasting T5.3's raw-file text into a shell leaves the
  backslashes intact and esbuild rejects it outright — measured:
  `✘ [ERROR] Invalid define value (must be an entity name or JS literal): \"production\"`, rc=1. Two contexts,
  two quotings, same final bytes reaching esbuild. Do not copy one into the other.
- **Step 4 — typecheck.**
  ```bash
  npx tsc --noEmit --jsx react-jsx --target ES2020 --lib ES2020,DOM,DOM.Iterable \
    --moduleResolution bundler --strict --skipLibCheck src/*.tsx src/*.ts
  ```
  **The expected error set is EMPTY — this must exit 0** (v3 correction, and it now actually does). v2 said
  "a small known set is expected" because the `listen_addr` and `~/.smbed.json` deltas are not applied in the
  spike. That reasoning is backwards: the deltas produce errors when *applied*, not when *omitted*, and the
  un-delta'd reference sources typecheck clean. The old wording licensed ralph to wave a genuine error through
  as "expected"; any error here is a real finding that must reshape Phase 5 before it is written.

  **v3 pinned this gate without running it in its own new form, and it failed — measured matrix (v4):**

  | toolchain | `css.d.ts` shim | rc |
  |---|---|---|
  | TS 7.0.2 (what v3's bare `typescript` resolved to) | absent | **1** — `App.tsx(10,8): error TS2882` |
  | TS 7.0.2 | present | 0 |
  | TS 5.9.3 (`^5.4.0`, the repo's pin) | absent | 0 |
  | TS 5.9.3 (`^5.4.0`) | **present — the pinned v4 form** | **0** |

  Two independent defects on one line, and either alone was enough to halt the plan at task 2 of 22 behind a
  stop-and-report instruction. Note the diagonal: the shim is what makes it pass on TS 7, and the pin is what
  makes it representative — v4 applies both, so the gate is correct *and* correct for the right reason.
- **Scope note (v4).** The spike deliberately omits the `listen_addr` and `state.json` deltas, so it typechecks
  the **reference** sources, not the ported ones. That is the right call for a day-one toolchain probe — the
  point is to prove React-under-esbuild works on this box — but it means the delta edits themselves are first
  typechecked at T5.4's `make typecheck`. Do not read T0.2 as broader coverage than it is.
- **Step 5 — assert the four bundle properties** (each must hold, not merely be recorded): `process.env` count
  `0`; `import.meta` count `0`; `Warning: ` count `0`; `jsx_runtime|react/jsx-runtime` count `> 0`.

  Then compare sizes — **and the metric is pinned, because the two available ones differ by 3% and that is the
  same order as the drift being looked for** (v4). Use **esbuild's own summary line**, which is what the ADR's
  figures are quoted in; `wc -c` reports raw bytes and will look like drift on a correct build:

  | artifact | esbuild summary (the metric to use) | `wc -c` (informational) |
  |---|---|---|
  | `bundle.js` | `301.1kb` | 308,315 B |
  | `bundle.css` | `15.5kb` | 15,875 B |

  esbuild's `kb` is bytes ÷ 1024, so the two columns agree exactly — they are one measurement in two units, not
  a discrepancy. Against the ADR's stated ~299 KB / ~16 KB this is a fraction of a percent, i.e. no drift.
  Treat a divergence beyond roughly ±10% as a real signal that the dependency set changed, and investigate
  before proceeding. This assertion only became meaningful in v4: while `typescript` and `esbuild` were
  unpinned, drift was guaranteed by construction and the comparison could not mean anything.
- **Step 6 — dry-run this plan's own novel shell constructs (v3 addition; widened to three in v4).** No repo
  state involved; a stub suffices. Each of these was wrong in some earlier draft and each was caught only by a
  reviewer running it — which is the argument for running them here, on day one, before anything depends on
  them. The rule this step enforces: **a gate with no escape hatch must be executed before it is pinned.**
  1. **The guarded trap + port guard from T7.1 step 7.** Run the trap skeleton with a forced early exit
     *before* `PID` is assigned, and confirm (a) the calling shell survives and (b) the temp dir is removed.
     v2's `trap 'kill "${PID:-0}" …'` expanded to `kill 0` on that path, which signals the **entire process
     group** — measured: it terminated the invoking shell and leaked the temp dir because the trap killed
     itself before reaching its own `rm -rf`.
  2. **The allow-list primitives from T5.5 and T7.1.** In a scratch repo pinned to `"$SPIKE"/gitprobe` — so
     step 7's `rm -rf` actually reclaims it, and so an executor cannot create it under the repo root — with a
     few known files under `web/smbedit/`:
     - **T5.5's form** (`find web/smbedit -type f ! -name .DS_Store | sort | diff - <(…)`): confirm rc=0 on a
       correct tree, rc=1 on a stray file, and rc=1 on a missing file. *This is the form T5.5 actually uses*;
       v3 left 6.2 exercising the `git status` form that T5.5 had already stopped using — the dry-run and the
       gate it exists to protect had drifted apart (v4 fix).
     - **T7.1's form** (`git status --porcelain -uall -- web/smbedit/ | cut -c4- | sort`): confirm it emits
       **one line per file** and that the `diff` against the expected list returns 0. Plain
       `git status --porcelain` collapses an untracked tree to the single line `?? web/smbedit/`, which is why
       v2's gate could never pass.
  3. **The blast-radius gate from T7.1 step 8 (v4 addition) — against a *committed* rogue file.** This is the
     case that made the §9.5 gate vacuous through two drafts, and it is cheap to pin here. In the same scratch
     repo: record `git rev-parse HEAD > .smbedit-base.sha`, then commit both an authorized change and an
     unauthorized one, and run step 8's three-source `CHANGED` pipeline. **It must exit 1 and name the
     unauthorized file.** Then revert the rogue, commit, and confirm it exits 0 — a gate that only ever fails
     is no better than one that only ever passes. Measured both ways while drafting v4: v3's two-source form
     returned `UNEXPECTED=[]` rc=0 against a committed rogue; the three-source form returns rc=1 naming
     `internal/multissh/server.go`, and rc=0 on the clean case.
- **Step 7 — clean up.** `rm -rf /tmp/smbedit-spike`. There is nothing else to undo: no repo file was written,
  so no `git checkout` is needed or permitted.
- If any assertion fails, **stop and report** rather than proceeding — Phase 5's design, or this plan's
  verification shell, needs revisiting before three phases of backend work are built on top of it.
- Verify: steps 3–6 all exit 0; and the repo is provably untouched — scripted, not asserted in prose (v4).
  Capture before the task and compare after:
  ```bash
  # before any spike work:
  git -C /opt/unified-webapp-smbedit status --porcelain -uall > /tmp/smbedit-porcelain-before.txt
  # after step 7:
  git -C /opt/unified-webapp-smbedit status --porcelain -uall | diff /tmp/smbedit-porcelain-before.txt -
  ```
  `diff` exits non-zero on any drift, so this is a gate rather than a note. It catches the whole failure class
  v2 fell into — a stray `node_modules/`, a modified `package.json` or `package-lock.json` — without needing to
  enumerate the artifacts in advance. (`-uall` for the usual reason: an untracked directory otherwise collapses
  to one line and hides what is inside it.)
- Covers: pre-mortem 1, §4.6's harness gap, de-risks §2.3 Option A's stated con.

### Phase 1 — Platform configuration

**T1.1 — `SmbeditConfig` in platform config**
- Edit: `internal/platform/config/config.go`
- Create: `internal/platform/config/smbedit_config_test.go` (or extend the existing config test file)
- Add `SmbeditConfig` with `static_dir`, `data_dir`, `picker_root` (FRD §5.3 table); add
  `Smbedit SmbeditConfig \`json:"smbedit"\`` to `Config`; add defaults to `DefaultConfig()`
  (`./web/smbedit`, `./data/smbedit`, `/opt`); add `expandSmbeditPaths` modelled exactly on
  `expandMultisshPaths` (config.go:305) covering all three fields, and call it from `Load()` alongside the
  other expanders. No validation beyond `~`-expansion — `Build()` owns the rest (FR-1).
- Tests: defaults present; `~/x` expands for each of the three fields; a config file omitting the `smbedit`
  section still yields the defaults; `WriteDefault` output unmarshals with a populated `smbedit` section.
- Verify: `go test -race -count=1 ./internal/platform/config/` and `go build ./...`
- Covers: FR-10, TR-2.

### Phase 2 — Backend core (no HTTP yet)

Each task in this phase creates one file plus its test file inside the new package
`cmd184psu/unified-webapp/internal/smbedit`. Source is `reference/smbed/` — **read only**, never imported.

**T2.1 — Ops log**
- Create: `internal/smbedit/oplog.go`, `internal/smbedit/oplog_test.go`
- Port `reference/smbed/internal/oplog/oplog.go` verbatim into package `smbedit`, unexporting types that no
  longer need to cross a package boundary (`opLog`, `opEntry`) while keeping the JSON field names `time` and
  `message` exactly as smbed emits them (FR-8 wire compatibility). Port `oplog_test.go` (4 tests:
  `TestAddAndSnapshot`, `TestSnapshot_BoundedByMax`, `TestSubscribe_ReceivesNewEntries`,
  `TestSubscribe_CancelStopsDelivery`).
- **Adopt the repo's goleak convention from this first task**, not later: `defer goleak.VerifyNone(t)` in every
  test that starts a goroutine, matching `internal/multissh/build_test.go:41`. Both `TestSubscribe_*` cases
  must call their `cancel()` — without it they fail their own goleak check.
- Verify: `go test -race -count=1 ./internal/smbedit/` **and**
  `[ "$(go test -list '.*' ./internal/smbedit/ | grep -c '^Test')" -ge 4 ]`
- Covers: TR-1.

**T2.2 — State store**
- Create: `internal/smbedit/state.go`, `internal/smbedit/state_test.go`
- Port the type definitions from `reference/smbed/internal/config/config.go` (`Share`, `GlobalEntry`,
  and `Config` → `State`) with `listen_addr` **removed** and all other JSON tags unchanged (§5.3).
  Port `defaults()` including all 17 global entries verbatim, and `DisableMissingPaths` verbatim.
  Add an unexported `store` type owning `sync.Mutex` + `*State` + the `state.json` path, exposing
  read-modify-write methods that take the lock exactly once per operation and return **deep copies** of any
  slice (pre-mortem 3).
- `load(dataDir)`: sweep any stale `state-*.json.tmp`; missing `state.json` → defaults (persisted); present →
  unmarshal onto defaults then `DisableMissingPaths`; malformed → error.
- `save()`: `os.CreateTemp(dataDir, "state-*.json.tmp")` (already 0600; the explicit `Chmod(0o600)` is kept as
  belt-and-braces) → write → `Sync()` → `Close()` → `os.Rename` within `data_dir` → **fsync the parent
  directory** so the rename survives power loss → remove the temp file on every error path.
- **Save-failure semantics (D-8):** a mutating store method that fails to save **rolls the in-memory mutation
  back** and returns the error. smbed kept the mutation and returned 500, leaving memory and disk divergent
  until the next successful save silently persisted it. Pick this side and test it.
- Tests: port `config_test.go` — **exactly 6 tests, and these are all of them**: `TestLoadDefaults_NoFile`,
  `TestSaveAndReload`, `TestDisableMissingPaths`, `TestDisableMissingPaths_NoChange`,
  `TestLoad_DisablesMissingSharePathOnReload`, `TestLoadCorrupt` — adapted to `t.TempDir()` with **no
  `t.Setenv("HOME", …)`**; plus new cases for atomic-write residue, 0600 mode, parent-dir durability, stale
  `.tmp` sweep, malformed-file error, deep-copy isolation, and rollback-on-save-failure.
  *(v3 correction: v2 said "7 tests … plus the picker-adjacent one". `config_test.go` contains 6 `func Test`,
  verified. The seventh, `TestListOptFolders_ReturnsDirectories`, lives in `internal/api/picker_test.go` and
  belongs to **T2.4** — do not look for it here and do not duplicate it into `state_test.go`.)*
- Verify: `go test -race -count=1 ./internal/smbedit/` **and**
  `[ "$(go test -list '.*' ./internal/smbedit/ | grep -c '^Test')" -ge 13 ]`
  (4 from T2.1 + 6 ported + ≥3 new; the realistic count is ~17)
- Covers: TR-1, TR-2, FR-6, FR-9, D-8.

**T2.3 — Samba render / parse / write / restart**
- Create: `internal/smbedit/samba.go`, `internal/smbedit/samba_test.go`, `internal/smbedit/samba_internal_test.go`
- Port `reference/smbed/internal/samba/samba.go` into package `smbedit`, switching its `*config.Config`
  parameters to the local `*State`. Keep the `confTemplate` string **byte-for-byte**, including the
  `# Generated by smbed — do not edit by hand.` header line (see §8 ambiguity A2). Keep `runCommand` as a
  package-level swappable `var` with its existing batch signature
  `func(name string, args ...string) (string, error)` (FR-11). Port `ParseConf`/`ReadConf`,
  `backupExisting`, `writeFile`, `writeFileWithSudo`, `Restart`, `joinContinuations`, `normalizeKey`,
  `isYes` unchanged in semantics.
- Tests: port `samba_test.go` (276 LOC) and `write_internal_test.go` (80 LOC); both now live in the same
  package, so merge carefully and keep every assertion. The golden-render assertions must pass **unmodified**
  — that is the FR-3 check.
- Verify: `go test -race -count=1 ./internal/smbedit/` **and**
  `[ "$(go test -list '.*' ./internal/smbedit/ | grep -c '^Test')" -ge 27 ]`
  (13 + 12 from `samba_test.go` + 2 from `write_internal_test.go`; realistic count ~31. **v3 reconciles the
  floors** — v2's ≥13 and ≥28 were mutually unsatisfiable: a package landing at exactly 13 could only reach 27
  after adding all 14 samba tests, so the ≥28 gate would fail a correct port.)
- Covers: TR-1, FR-3, FR-4, FR-5, FR-11.

**T2.4 — Folder picker**
- Create: `internal/smbedit/picker.go`, `internal/smbedit/picker_test.go`
- Port `reference/smbed/internal/api/picker.go` into package `smbedit`. Replace the hardcoded
  `const pickerRoot = "/opt"` with a root passed in from config; keep `"/opt"` as the fallback when the
  configured value is empty (§5.3). Keep `FolderEntry`'s `name`/`path` JSON tags. Keep the one-level-only
  behavior and hidden-directory skipping.
- Tests: port `TestListOptFolders_ReturnsDirectories` (from `reference/smbed/internal/api/picker_test.go` —
  this is the test T2.2 deliberately does **not** own) against a `t.TempDir()` root; add a case asserting an
  empty configured root resolves to `/opt`; add a case asserting nested subdirectories are never returned.
- Verify: `go test -race -count=1 ./internal/smbedit/` **and**
  `[ "$(go test -list '.*' ./internal/smbedit/ | grep -c '^Test')" -ge 30 ]`
  (27 + 1 ported + 2 new; realistic count ~34)
- Covers: TR-1, FR-7.

### Phase 3 — HTTP layer

**T3.1 — Server skeleton, mux, helpers, static handler, panic recovery**
- Create: `internal/smbedit/handler.go`, `internal/smbedit/handler_test.go`
- Define an unexported `server` struct (store, oplog, picker root, static dir, version) and an unexported
  `newServer(opts) *server` constructor plus `Handler() http.Handler` — mirroring `multissh.New` /
  `Server.Handler`, so tests can build a handler **without** a static dir the way smbed's
  `NewServer(cfg, "test", nil)` did (§8 ambiguity A3).
- Register **13 handlers across 10 distinct paths** on `http.ServeMux` using method patterns, exactly as
  `internal/multissh/server.go` does. The enumeration is normative — an incomplete mux must not be able to
  satisfy the test:

  | # | Registration | # | Registration |
  |---|---|---|---|
  | 1 | `GET /api/config` | 8 | `POST /api/import` |
  | 2 | `PUT /api/config` | 9 | `POST /api/save-and-restart` |
  | 3 | `GET /api/shares` | 10 | `GET /api/preview` |
  | 4 | `PUT /api/shares` | 11 | `GET /api/logs/ops/stream` |
  | 5 | `GET /api/globals` | 12 | `GET /api/logs/samba/stream` |
  | 6 | `PUT /api/globals` | 13 | `GET /api/version` |
  | 7 | `GET /api/folders` | | |

  Add **no** chi, **no** new `go.mod` entries, and **no** per-route `OPTIONS` handlers (the platform's
  `middleware.Wrap` answers preflight for the whole dispatcher).
- Port `jsonOK`/`jsonErr`/`decodeBody` from `reference/smbed/internal/api/server.go:390-407`. Copy the
  `staticHandler` / `staticFileExists` / `noDirList` / `onlyGet` pattern from
  `internal/multissh/server.go:174-215`, registering it only when `static_dir` is non-empty.
- **Add a module-local panic-recovery wrapper (D-9)** around the mux in `Handler()`: `recover()`, log the
  stack, respond 500 **if nothing has been written yet**. FRD §5.4 mandates it. The motivation is precise and
  narrower than v2 claimed: `net/http` already recovers handler panics itself, so the process survives either
  way — but it drops the connection with **no status and no body**, where chi's `Recoverer` returned a clean
  500 and a logged stack. D-9 restores the 500. See §1.

  **Two implementation requirements, both mandatory — this is where the wrapper goes wrong (v3 addition):**

  1. **It must track whether the header was already written**, and only synthesize the 500 when it was not.
     Writing a 500 header over an already-committed streaming response is a `superfluous WriteHeader` at best
     and a corrupted response at worst.
  2. **Its `ResponseWriter` must explicitly forward `Flush()` — and `Unwrap() http.ResponseWriter` — to the
     underlying writer.** Go promotes only the methods of an embedded `http.ResponseWriter` *interface*, so the
     `wroteHeader`-tracking wrapper requirement (1) forces **does not satisfy `http.Flusher`** unless `Flush`
     is written out by hand. Measured: `wrapped-rw flusher=false`, `passthrough-rw flusher=true`. Both SSE
     handlers begin with a `w.(http.Flusher)` assertion and return `500 "streaming unsupported"` when it fails
     (`reference/smbed/internal/api/server.go:297-301` and `:331-335`), so omitting the forward silently breaks
     **FR-8 in full** — and does so in production only, since nothing in the plan would catch it unless the SSE
     tests exercise the wrapped chain. Which is why T3.4 mounts `Handler()`, not the bare mux.
- Handlers may be stubs at this task's end **only if** the package still compiles and the routing tests pass;
  stubs must be filled in by T3.2–T3.4.
- Tests: every one of the 13 registrations reaches a handler. Static handler rules — real file served,
  `/api/nope` → 404 JSON not index.html, unknown non-API GET → index.html, directory path → no listing,
  non-GET miss on a non-API path → 405. **Wrong-method behavior asserted for both server shapes:** with no
  static dir, `DELETE /api/shares` → `ServeMux` 405; with a static dir, the `/` pattern claims it first and
  the static handler returns **404 JSON** (house convention, `internal/multissh/static_test.go:47-49`).
  **Panic recovery (D-9), two assertions:** (a) a deliberately panicking route served through `Handler()`
  returns **status 500 with a body**, and the stack reaches the module's logger — *not* "the process survives",
  which `net/http` guarantees with or without D-9 and which therefore proves nothing; (b) the response writer
  that reaches a handler through `Handler()` still satisfies `http.Flusher` — assert it directly here, so a
  lost `Flush` promotion fails in the task that introduced the wrapper rather than in T3.4 or, worse, only in
  production.
- Verify: `go test -race -count=1 ./internal/smbedit/` and `go build ./...`
- Covers: FR-2, FR-15, D-9, §5.4.

**T3.2 — Config, shares, globals, folders handlers**
- Edit: `internal/smbedit/handler.go`; Create: `internal/smbedit/handler_state_test.go`
- Port `handleGetConfig`, `handlePutConfig`, `handleGetShares`, `handlePutShares`, `handleGetGlobals`,
  `handlePutGlobals`, `handleGetFolders`, and `disabledByMissingPath`
  (`reference/smbed/internal/api/server.go:100-208`), routing every mutation through the `store` methods from
  T2.2 so the lock is taken once per request. Remove `ListenAddr` from `patchConfig`; an inbound `listen_addr`
  field is ignored, not rejected (§5.3). Ops-log auto-disabled share names on `PUT /api/shares` (FR-6).
  A save failure surfaces as 500 with the in-memory state rolled back (D-8).
- **Ops-log ordering under D-8 — pinned, not left to the executor (v3 addition).** The reference emits the
  `auto-disabled share(s)` entry **before** it saves (`reference/smbed/internal/api/server.go:152-157`). That
  ordering is incompatible with rollback: a failed save would leave the ops log — the operator's only in-band
  failure channel (§5.8) — asserting an auto-disable that was rolled back and never persisted. **Emit the entry
  after the save succeeds.** On the failure path the only ops-log output is the save-failure entry. This is a
  micro-delta from smbed, declared in §1's D-8 row, and it is asserted by a test: a store whose save path is
  unwritable produces a 500, unchanged in-memory state, and **no** auto-disable entry in the ops log.
- Tests: port the relevant cases from `server_test.go`; add the `listen_addr`-absent assertion, the
  persistence assertion (`state.json` re-loadable after each PUT), the **save-failure rollback** assertion
  (unwritable save path → 500 **and** unchanged in-memory state), and the **TR-2 concurrency test** (16
  goroutines interleaving `PUT /api/shares`, `PUT /api/globals`, `GET /api/config`).
- Verify: `go test -race -count=1 ./internal/smbedit/`
- Covers: FR-2, FR-6, FR-7, FR-9, D-8, TR-1, TR-2.

**T3.3 — Import, preview, save-and-restart, version**
- Edit: `internal/smbedit/handler.go`; Create: `internal/smbedit/handler_conf_test.go`
- Port `handleImportConf`, `handlePreview`, `handleSaveAndRestart` (`server.go:210-289`) and the version
  handler — which is the **inline closure at `server.go:80-82`**, not part of that range — preserving
  response shapes exactly: `{globals, shares, share_owner}` for import (staged, **not** persisted — FR-5),
  `text/plain` with `Content-Length` for preview, `{written, restart:{success,output}, path}` for
  save-and-restart.
- Define `var Version = "dev"` at package level as the `/api/version` source (§8 ambiguity A1). **Absent a
  Makefile `-ldflags` stamp, `/api/version` will always report `"dev"`** — intended, and recorded as ADR
  follow-up #6.
- Wire the ops-log hook **per-server at construction** rather than through the process-global `samba.Logf`
  (D-7).
- Tests: port `TestImportConf`, `TestImportConf_DisablesMissingSharePath`, `TestImportConf_MissingFile`,
  `TestPreview`, `TestVersion`, `TestPutConfig_BadJSON`; add a save-and-restart test using a swapped
  `runCommand` that asserts failure is reported in-band (`restart.success=false` + ops-log entries) and the
  handler does not panic (§9.4).
- Verify: `go test -race -count=1 ./internal/smbedit/`
- Covers: FR-2, FR-4, FR-5, D-7, TR-1.

**T3.4 — SSE streams**
- Edit: `internal/smbedit/handler.go`; Create: `internal/smbedit/handler_sse_test.go`,
  `internal/smbedit/samba_unix.go`
- Port `handleOpsLogStream`, `handleSambaLogStream`, `setSSEHeaders`, `writeSSEEntry`
  (`server.go:291-388`) unchanged in wire format. Keep the `http.Flusher` assertion and the headers. Keep the
  `sudo tail -n 200 -F <path>` invocation and the 400-when-empty behavior.
- **Add the streaming command seam (D-5).** `runCommand`'s batch signature
  `func(name string, args ...string) (string, error)` cannot express a tailed stream, so add a second
  package-level var:
  ```go
  var runStreamingCommand = func(ctx context.Context, name string, args ...string) (io.ReadCloser, func() error, error)
  ```
  returning the stdout pipe, a wait/cleanup func, and an error. Tests swap this; the handler never touches
  `exec` directly (FR-11).
- **Process-group teardown (D-6).** In `samba_unix.go` (`//go:build unix` — covers both darwin and linux),
  set `SysProcAttr{Setpgid: true}` on the command and kill the **negative PID** so `tail` dies with `sudo`;
  set `cmd.WaitDelay` so a survivor is force-reaped on a bounded timer. Record honestly that this is **not**
  CI-verifiable without shelling out (§4.6).
- Tests: ops stream sends backlog, then a live entry, then **returns** on context cancel. **Harness is
  mandated, on two axes:**
  - *Transport:* `httptest.NewServer` + a real `http.Client` with a cancellable request. Reading an
    `httptest.ResponseRecorder`'s `Body` from the test goroutine while the handler writes to it from another
    races under `-race` and will fail intermittently.
  - *What is mounted (v3 addition):* `httptest.NewServer(srv.Handler())` — **the full wrapped chain, never the
    bare `mux`**. The D-9 recovery wrapper sits in that chain and is exactly the thing most likely to break
    SSE, by losing the `http.Flusher` promotion (see T3.1). Mounting the mux directly would let every SSE test
    in this task pass against a handler chain that returns `500 "streaming unsupported"` in production, and the
    breakage would not surface until T7.1 step 7 — four phases later.

  Samba stream: 400 without a configured path; with a swapped `runStreamingCommand`, the handler returns and
  its goroutine exits on disconnect (`defer goleak.VerifyNone(t)`). Same mounting rule.
- Verify: `go test -race -count=1 ./internal/smbedit/` (run it **three times** — an SSE race that appears once
  in three runs is still a bug)
- Covers: FR-8, FR-11, D-5, D-6, TR-1.

**T3.5 — `Build()`**
- Create: `internal/smbedit/build.go`, `internal/smbedit/build_test.go`
- `Build(cfg config.SmbeditConfig) (http.Handler, error)` following `internal/multissh/build.go`: trim
  `static_dir` and run a `checkStaticDir` equivalent (empty / missing / not-a-directory / unreadable all
  error); `os.MkdirAll(data_dir, 0o755)` and error if it fails; load or default-initialise `state.json`,
  erroring on a malformed file; resolve `picker_root` (empty → `/opt`); construct the server from T3.1 and
  return its handler. **Fails, never degrades** — the dispatcher turns the error into a 503 handler.
- Tests: each failure mode returns an error and a nil handler; success creates `data_dir` and a default
  `state.json`; an existing `state.json` is loaded rather than overwritten; `Build` starts no goroutines
  (`defer goleak.VerifyNone(t)`, mirroring `internal/multissh/build_test.go:41`).
- Verify: `go test -race -count=1 ./internal/smbedit/` and `go vet ./internal/smbedit/`
- Covers: FR-1, TR-2.

### Phase 4 — Dispatch wiring

**T4.1 — Register the module**
- Edit: `cmd/server/main.go`, `cmd/server/dispatch_test.go`
- Add `case "smbedit": return smbedit.Build(cfg.Smbedit)` to `buildModule` (main.go:116-132) and the import.
  Extend `dispatch_test.go` with a `smbeditTestConfig` helper modelled on `multisshTestConfig`
  (dispatch_test.go:19) providing a real static dir with an `index.html` and a `t.TempDir()` data dir; assert
  a `smbedit` routing entry builds and serves, that a broken smbedit config 503s only its own hostname while
  other modules stay healthy, and that the unknown-module path is unchanged.
- Verify: `go test -race -count=1 ./cmd/server/` and `make test`
- Covers: FR-10, TR-3, §9.5.

### Phase 5 — Frontend port

> **Ordering note (v2 change).** v1 put the esbuild script edit before the sources existed, which both broke
> `npm run build` / `make web` repo-wide for the duration of three tasks and made the script task's own
> verification unrunnable. Sources now land first.

**T5.1 — Frontend dependencies and the CSS shim**
- Edit: `package.json` (root); Create: `web/smbedit/src/css.d.ts`
- Add `react` `^18.2.0` and `react-dom` `^18.2.0` to `dependencies`; `@types/react` `^18.2.0` and
  `@types/react-dom` `^18.2.0` to `devDependencies`. Add **nothing else** — no Vite, no jest, no
  `@vitejs/plugin-react` (D1, D3).
- Create `web/smbedit/src/css.d.ts` containing `declare module "*.css";` with a comment mirroring
  `web/multissh/js/css.d.ts`. **Whether this is required is version- and scope-conditional (restated in v4 —
  the behavior is unchanged, the file is created either way):**
  - *In-repo, on the pinned TypeScript `^5.4.0`:* **not** required. `web/multissh/js/css.d.ts` already declares
    `*.css` ambiently and is matched by the root `include` glob `web/multissh/js/*.ts`, so the declaration is
    project-wide today.
  - *Out-of-tree (T0.2's spike), or on TypeScript 7:* **required.** TS 7 reports
    `TS2882: Cannot find module or type declarations for side-effect import` for `import './styles.css'`, and
    the ambient declaration next door is out of scope anyway once you leave the repo's `include` set.

  So the v3 flat claim "defensive, not required" was true only under assumptions it did not state. The file is
  kept regardless: it makes `web/smbedit/src/` self-contained if the include list is ever narrowed, and it is
  what makes the module survive a TypeScript major bump. Duplicate shorthand ambient declarations were verified
  harmless.
- Verify: `npm install && node -e "require.resolve('react-dom/client')"`; and a scripted allow-list diff
  rather than a human eyeball —
  `{ git diff HEAD --name-only -- package.json package-lock.json; git diff --name-only "$(cat .smbedit-base.sha)" -- package.json package-lock.json; } | sort -u | diff - <(printf 'package-lock.json\npackage.json\n')`
  (**not a bare `git diff`** — that form is index-relative, so staging the two files empties the output and
  silently inverts the gate from "exactly these two changed" to "nothing changed". The `$BASE` source is the
  same v4 fix as T7.1 step 8, for the same reason: `git diff HEAD` goes quiet once the files are *committed*,
  and this gate is bounded in practice only because the adjacent `node -e` assertion is commit-agnostic and
  catches the real failure. Fixing it costs one clause now that T0.1 records the ref.)
  plus `node -e "const d=require('./package.json'); for (const k of ['react','react-dom']) if(!d.dependencies[k]) throw new Error('missing dep '+k); for (const k of ['@types/react','@types/react-dom']) if(!d.devDependencies[k]) throw new Error('missing devDep '+k); if (JSON.stringify(d).match(/vite|jest/i)) throw new Error('forbidden toolchain dep added'); console.log('ok')"`
- Covers: §5.7, D1.

**T5.2 — Copy the React sources and apply the authorized deltas**
- Create: `web/smbedit/src/{App,SharesPage,GlobalsPage,PreviewPage,SettingsPage,LogsPage,FolderPicker,Toast,theme,main}.tsx`,
  `web/smbedit/src/api.ts`, `web/smbedit/src/styles.css`
- Copy from `reference/smbed/web/src/` **unchanged** except for the deltas below. Do **not** copy
  `App.test.tsx`, `test-setup.ts`, `vite.config.ts`, the per-app `package.json`/`tsconfig.json`, or anything
  under `web/dist/`. The sources contain no `import.meta` or `process.env` references (verified against the
  reference), so nothing else needs adapting (§5.7).
- **Authorized delta 1 — remove `listen_addr` (five sites across three files).** v1 listed only two files and
  would have left two TS2339 compile errors and an unsatisfiable grep:

  | File | Line (in reference) | What |
  |---|---|---|
  | `api.ts` | 19 | `listen_addr: string` in `AppConfig` (and the config-patch type) |
  | `SettingsPage.tsx` | 62 | `value={config.listen_addr}` |
  | `SettingsPage.tsx` | 64 | `onChange={e => onChange({ listen_addr: e.target.value })}` — remove the whole field: input, label, handler |
  | `App.tsx` | 83 | `listen_addr: config.listen_addr,` passed into `api.putConfig` |
  | `App.tsx` | 136 | `listen_addr: config.listen_addr,` passed into `api.putConfig` |

- **Authorized delta 2 — the three `~/.smbed.json` UI strings.** They become false after the port (state now
  lives in `<data_dir>/state.json`) and would contradict the migration doc written in T6.2:

  | File | Line (in reference) | Current text |
  |---|---|---|
  | `App.tsx` | 92 | `toast('Saved', 'Configuration written to ~/.smbed.json', 'success')` |
  | `App.tsx` | 241 | `'✓ All changes saved to ~/.smbed.json.'` |
  | `SettingsPage.tsx` | 27 | `Saved to <code>~/.smbed.json</code>.` |

  Replace each with wording referring to `state.json` (e.g. "Configuration written to state.json",
  "✓ All changes saved to state.json.", "Saved to <code>state.json</code>."). Do not embed an absolute path —
  `data_dir` is operator-configurable.

- Verify: the source set is exactly these 13 files — **scripted against a fixed list, not eyeballed** (v4; this
  was the last unscripted expectation left in the plan, and the list it referred to was never written down).
  `css.d.ts` is present because T5.1 runs before this task; the absence of `*.test.*` follows from the list
  being exact, so it needs no separate assertion:
  ```bash
  ls web/smbedit/src/ | sort | diff - <(printf '%s\n' \
    api.ts App.tsx css.d.ts FolderPicker.tsx GlobalsPage.tsx LogsPage.tsx main.tsx PreviewPage.tsx \
    SettingsPage.tsx SharesPage.tsx styles.css theme.tsx Toast.tsx | sort)
  ```
  These are the same 13 `web/smbedit/src/` entries T5.5's whole-directory gate expects, so the two lists are
  consistent by construction — if one is ever edited, edit both. Then:
  `grep -rn 'listen_addr\|listenAddr' web/smbedit/src/` returns nothing;
  `grep -rn '\.smbed\.json' web/smbedit/src/` returns nothing;
  `grep -rn 'import\.meta' web/smbedit/src/` returns nothing.
- Covers: FR-12, FR-13, FR-14, §5.7, D3.

**T5.3 — esbuild build scripts**
- Edit: `package.json` (root)
- Append to the `build` script (` && ` after the existing multissh entry). **This is the exact text as it
  appears in the raw `package.json` file** — the `\"` sequences are JSON escapes for literal double quotes,
  wrapped in shell single quotes:

  ```
  esbuild web/smbedit/src/main.tsx --bundle --target=es2020 --jsx=automatic --log-level=warning --define:process.env.NODE_ENV='\"production\"' --outfile=web/smbedit/js/bundle.js
  ```

  and the mirrored entry in `build:dev` with `--sourcemap` and `'\"development\"'`.

  **Two contexts, two quotings — do not copy one into the other.** The block above is **JSON source**: npm
  unescapes `\"` to `"` before handing the script to the shell, which then strips the single quotes, so esbuild
  finally receives `production` wrapped in real double quotes — a JSON string. The **shell** form of the same
  define, used by T0.2 when running esbuild directly from a terminal, is `--define:process.env.NODE_ENV='"production"'`
  with no backslashes. Pasting this JSON text into a shell leaves the backslashes intact and esbuild rejects it
  outright: `✘ [ERROR] Invalid define value (must be an entity name or JS literal): \"production\"` (measured).

  **Quoting matters and one wrong form fails *silently*.** The shorter `--define:process.env.NODE_ENV=\"production\"`
  (no surrounding single quotes) is **wrong**: the shell strips the quotes, esbuild receives the bare token
  `production`, treats it as an *identifier* rather than a string, and substitutes it into `react-dom` —
  producing a bundle that contains no `process.env` (so a naive grep passes) and dies at load with
  `ReferenceError: production is not defined`. That is why the verification below checks the define's
  **effect**, not just its presence.
- `--jsx=automatic` and the `--define` are both **required** — see pre-mortem 1. Do not rely on tsconfig
  discovery, and do not omit the define because the build succeeds without it. `--log-level=warning` quiets
  informational output so that any esbuild warning stands out in the build log; warnings are **surfaced for
  review, not auto-failed** — a benign React warning must not be able to block ralph.
- Verify:
  1. `node -e "const s=require('./package.json').scripts; for (const k of ['build','build:dev']) { if (!s[k].includes('web/smbedit/src/main.tsx')) throw new Error(k+': missing smbedit entry'); if (!s[k].includes('--jsx=automatic')) throw new Error(k+': missing --jsx=automatic'); if (!/--define:process\.env\.NODE_ENV='\"[a-z]+\"'/.test(s[k])) throw new Error(k+': NODE_ENV define missing or wrongly quoted'); } console.log('ok')"`
  2. `npm run build` exits 0 — **and the three pre-existing module bundles still build**, which this command
     covers since they share the script (principle 5).
  3. **Define took effect as a string:** `node -e "const s=require('fs').readFileSync('web/smbedit/js/bundle.js','utf8'); if (/\bprocess\.env\b/.test(s)) throw new Error('NODE_ENV not defined away'); if (/Warning: /.test(s)) throw new Error('dev build of react-dom bundled: define resolved to an identifier, not the string production'); console.log('ok')"`
- Covers: §5.7, G4.

**T5.4 — Root tsconfig covers the TSX**
- Edit: `tsconfig.json` (root)
- Add `"jsx": "react-jsx"` to `compilerOptions`, add `"DOM.Iterable"` to `lib`, and add
  `"web/smbedit/src/**/*.ts"` + `"web/smbedit/src/**/*.tsx"` to `include`.
- **This mutates shared compiler options** that all three existing TS modules compile under — it is not an
  additive edit (principle 5). It is safe because those modules contain no JSX syntax, so a project-wide
  `jsx` setting is inert for them, and `DOM.Iterable` only widens the available lib. Both facts are proven by
  the verification, not assumed. Fall back to a referenced `web/smbedit/tsconfig.json` only if `tsc` actually
  reports a conflict (§5.7 allows either).
- Verify: `make typecheck` (covers old and new modules in one pass — a regression in obsidianoid, slideshow,
  or multissh fails here)
- Covers: TR-4, §5.7.

**T5.5 — index.html and the committed bundle**
- Create: `web/smbedit/index.html`, `web/smbedit/js/bundle.js`, `web/smbedit/js/bundle.css`
- Copy `reference/smbed/web/index.html`, keep the `<title>` and the Google-Fonts links, and replace the Vite
  module script with the multissh-style relative references: `<link rel="stylesheet" href="js/bundle.css">`
  and `<script src="js/bundle.js"></script>`. No asset hashing. Run the build and commit the output per repo
  convention (`make build` must never need npm).
- Verify: `npm run build` exits 0; `test -s web/smbedit/js/bundle.js && test -s web/smbedit/js/bundle.css`;
  the T5.3 verification step 3 (no `process.env`, no dev-only `Warning: ` strings);
  `grep -c 'import\.meta' web/smbedit/js/bundle.js` → `0`;
  `grep -cE 'jsx_runtime|react/jsx-runtime' web/smbedit/js/bundle.js` → greater than `0`;
  `make typecheck`; and a **scripted, fixed-expectation** file-set check rather than an eyeball:

  ```bash
  find web/smbedit -type f ! -name .DS_Store | sort | diff - <(printf '%s\n' \
    web/smbedit/index.html \
    web/smbedit/js/bundle.css \
    web/smbedit/js/bundle.js \
    web/smbedit/src/App.tsx web/smbedit/src/FolderPicker.tsx web/smbedit/src/GlobalsPage.tsx \
    web/smbedit/src/LogsPage.tsx web/smbedit/src/PreviewPage.tsx web/smbedit/src/SettingsPage.tsx \
    web/smbedit/src/SharesPage.tsx web/smbedit/src/Toast.tsx web/smbedit/src/api.ts \
    web/smbedit/src/css.d.ts web/smbedit/src/main.tsx web/smbedit/src/styles.css \
    web/smbedit/src/theme.tsx | sort)
  ```

  **Three v3 corrections. The first two were reproduced by reviewers rather than reasoned about:**
  - **v2's `git status --porcelain web/smbedit/ | awk '{print $2}'` could never pass.** `git status --porcelain`
    collapses an entirely-untracked tree to the single line `?? web/smbedit/`, so the four-line expectation
    failed unconditionally, on every run, by construction. (`-uall` fixes that, and `cut -c4-` would be the
    right field extractor — `awk` splits on whitespace, truncating any path containing a space and mangling the
    `R  old -> new` rename form.)
  - **But the check should not be asking git at all.** The claim under test is "`web/smbedit/` contains exactly
    these files and nothing else", which is a statement about the working tree, not about the index. `find` is
    commit-agnostic: it gives the same answer whether or not ralph has committed Phase 5, and it catches a
    stray file even if that file is already tracked. Both git-based forms go quiet in exactly those cases.
  - **The expected list is fixed here, at authoring time, and v2's hedge is deleted.** v2 told the executor to
    *"adjust the expected list to the actual collapsing `git status` performs"* — under which ralph sets the
    expectation to `web/smbedit/` and the gate then passes regardless of what the directory contains. A gate
    whose documented remedy is to weaken it until green is not a gate. If this list is wrong, that is a plan
    bug to report, **not** an expectation to edit.
- Covers: G4, §9.2, pre-mortem 1.

### Phase 6 — Configuration example and documentation

**T6.1 — Example configuration**
- Edit: `unified-webapp-example.json`
- Add `"smbedit-test.cmdhome.net": "smbedit"` and `"smbedit.cmdhome.net": "smbedit"` to `host_routing`, and a
  filled-in `smbedit` section (`static_dir`, `data_dir`, `picker_root`) matching the FRD §5.3 shape and the
  formatting of the neighbouring sections.
- Verify — note this checks **the example file itself**, not just `DefaultConfig()` (v1's check exercised only
  the latter), and is idempotent (v1's was not: `config.WriteDefault` no-ops when the target exists, so a
  retry re-grepped stale content):
  1. `python3 -m json.tool unified-webapp-example.json > /dev/null`
  2. The example file unmarshals into the real config struct with a populated `smbedit` section — a throwaway
     Go test or `go run` that calls `config.Load("unified-webapp-example.json")` and asserts
     `cfg.Smbedit.StaticDir != "" && cfg.Smbedit.DataDir != ""`, plus that `host_routing` contains a
     `smbedit` value.
  3. `rm -f /tmp/smbedit-example-check.json && go run ./cmd/server -init-config -config /tmp/smbedit-example-check.json && grep -q '"smbedit"' /tmp/smbedit-example-check.json` (the `rm -f` is what makes this re-runnable)
- Covers: FR-18, FR-10.

**T6.2 — Operator documentation**
- Create: `docs/smbedit.md`
- Cover, following the structure and tone of `docs/multissh.md`: what the module does; the **no-authentication**
  warning made prominent given this module rewrites `/etc/samba/smb.conf` and restarts services; the
  privilege model with a copy-pasteable sudoers snippet for exactly the three commands in FRD §5.8; the
  behavior on a host **without** those grants (in-band failure via `restart.success=false` and the ops log, no
  crash); the proxy/SSE caveat (no response buffering, preserve the `Host` header) for the two stream
  endpoints; and the manual migration procedure from standalone smbed (FRD §8) — copy `~/.smbed.json` to
  `<data_dir>/state.json`, remove `listen_addr`, add the hostname to `host_routing` and DNS/HAProxy/`/etc/hosts`,
  configure sudoers for the unified service user, stop and disable the old smbed service.
- **Also add a named "Owner acceptance checklist" section** capturing the two acceptance items that cannot be
  automated from this repo — §9.3's browser pass and §9.4's Linux/Samba pass with and without sudoers — as an
  explicit tick-list deliverable rather than a line buried in a plan.
- Verify: `test -s docs/smbedit.md`; `grep -qi 'sudoers' docs/smbedit.md`; `grep -qi 'buffer' docs/smbedit.md`;
  `grep -qi 'state.json' docs/smbedit.md`; `grep -qi 'no login\|no authentication' docs/smbedit.md`;
  `grep -qi 'acceptance checklist' docs/smbedit.md`
- Covers: FR-17, §5.6, §5.8, §8, §9.3, §9.4.

**T6.3 — README**
- Edit: `README.md`
- Add smbedit to the module list; add the two `host_routing` example entries; add the `smbedit` config section
  block and its field table (mirroring the existing `#### The multissh section` pattern at README.md:100); add
  the `./data/smbedit` entry to the data-directory layout; add the no-auth note and a pointer to
  `docs/smbedit.md`. **Documentation only — create no `./data/smbedit` directory.** `.gitignore` carries an
  unanchored `data` entry, so such a path would be silently untracked; the layout section describes what the
  running binary creates at `data_dir`, it does not seed it.
- Verify: `grep -c smbedit README.md` → at least `6`; `grep -q 'docs/smbedit.md' README.md`
- Covers: FR-16.

### Phase 7 — Final verification

**T7.1 — Full verification pass**
- Edit: nothing (verification only; if a check fails, fix it in the task that owns the file)
- Run in order:
  1. `go build ./...`
  2. `go vet ./...`
  3. `make test` (`go test -race ./...`) — acceptance §9.1
  4. `make typecheck` — acceptance §9.2, TR-4
  5. **Bundle is reproducible and adds nothing.** v2 wrote
     `npm run build && git status --porcelain web/smbedit/js/` — which has **no `diff` attached at all**, so it
     exits 0 on any output whatsoever despite the prose demanding a non-zero exit on an unexpected path. The
     gate is the comparison, so write the comparison. Phrased as a **subset** test, because whether the bundle
     shows as dirty depends on whether Phase 5 was committed — a deterministic rebuild of an already-committed
     bundle correctly produces *no* output, and an exact-match expectation would fail that good case:
     ```bash
     npm run build
     UNEXPECTED=$(git status --porcelain -uall -- web/smbedit/js/ | cut -c4- | sort \
       | grep -vx -e 'web/smbedit/js/bundle.css' -e 'web/smbedit/js/bundle.js' || true)
     [ -z "$UNEXPECTED" ] || { printf 'unexpected paths under web/smbedit/js/:\n%s\n' "$UNEXPECTED"; exit 1; }
     ```
     (`|| true` is required: `grep` exits 1 when it matches nothing, which is the *success* case here, and
     `set -e` would abort on it.)

     **Backstop the subset test by re-running T5.5's `find` gate here (v4 addition).** The subset test above is
     honest but incomplete: being git-relative, it goes quiet on anything committed, so on its own it cannot
     confirm the "adds nothing" half of this step's own title. T5.5's fixed-list `find` form is the one gate in
     this plan that is commit-agnostic — verified to still return rc=1 on a stray file *after* that file has
     been committed, exactly where `git status -uall` returns nothing. Re-running it costs one command and
     closes the hole without weakening either check. Acceptance §9.2
  6. `make build` — acceptance §9.2's "no npm" clause. **No `node_modules` parking:** v1 moved `node_modules`
     aside and moved it back, which never restored on failure and could not be re-run. `make build` invokes
     only `go build` (verified in the Makefile), so there is nothing to park — running it *is* the check.
  7. **Operable e2e smoke** (v1's version hung forever: a fresh instance's ops log is empty, so `curl -N`
     never returns; and the binary was started in the foreground with no readiness wait or teardown).
     Prerequisites: **bash**; `jq`, `curl`, `lsof`; a free TCP port. Run as a script with `set -euo pipefail`:
     ```bash
     set -euo pipefail
     PORT=18080; H='Host: smbedit-test.cmdhome.net'; B="http://127.0.0.1:$PORT"
     DATA=$(mktemp -d)
     trap 'if [ -n "${PID:-}" ]; then kill "$PID" 2>/dev/null || true; fi; rm -rf "$DATA"' EXIT
     lsof -ti "tcp:$PORT" >/dev/null && { echo "port $PORT busy"; exit 1; }
     cat > "$DATA/cfg.json" <<EOF
     {"port":$PORT,"host_routing":{"smbedit-test.cmdhome.net":"smbedit"},
      "smbedit":{"static_dir":"./web/smbedit","data_dir":"$DATA/state","picker_root":"$DATA"}}
     EOF
     ./unified-webapp -config "$DATA/cfg.json" & PID=$!
     for i in $(seq 1 50); do curl -sf --max-time 1 -H "$H" "$B/api/version" >/dev/null && break; sleep 0.2; done
     curl -sf --max-time 5 -o /dev/null -H "$H" "$B/"                                  # 1 index
     curl -sf --max-time 5 -o "$DATA/bundle.js" -H "$H" "$B/js/bundle.js"              # 2 bundle
     test -s "$DATA/bundle.js"                                                         #   …non-empty
     curl -sf --max-time 5 -H "$H" "$B/api/config" | jq -e 'has("listen_addr")|not'     # 3
     curl -sf --max-time 5 -H "$H" "$B/api/version" | jq -e '.version'                  # 4
     curl -sf --max-time 5 -o "$DATA/preview.txt" -H "$H" "$B/api/preview"              # 5 preview
     grep -q '\[global\]' "$DATA/preview.txt"                                           #   …renders
     [ "$(curl -s --max-time 5 -o /dev/null -w '%{http_code}' -H "$H" "$B/api/nope")" = 404 ] # 6
     # 7 SSE liveness: stream in background, THEN generate a live ops entry
     curl -sN --max-time 10 -H "$H" "$B/api/logs/ops/stream" > "$DATA/sse.txt" & SSE=$!
     sleep 1
     curl -sf --max-time 5 -X PUT -H "$H" -H 'Content-Type: application/json' \
          -d '[{"name":"smoke","path":"/nonexistent","writable":true,"public":false,"browseable":true,"enabled":true}]' \
          "$B/api/shares" >/dev/null
     sleep 2; kill "$SSE" 2>/dev/null || true
     grep -q '^data: ' "$DATA/sse.txt" || { echo "SSE produced no events"; exit 1; }
     ```

     **Three v3 fixes, each reproduced by a reviewer against real tools rather than argued:**
     - **The trap ran `kill 0`.** v2 installed `trap 'kill "${PID:-0}" …'` *before* `PID` was assigned, and the
       port-busy guard on the very next line exits between the two. `kill 0` signals **every process in the
       caller's process group** — measured: it terminated the invoking shell (rc=143), and it leaked the temp
       dir because the trap killed itself before reaching its own `rm -rf`. If ralph does not isolate this
       script in its own process group, that SIGTERM reaches ralph. The guard on non-empty `PID` plus `|| true`
       fixes both halves.
     - **Step 2 killed `curl` with EPIPE.** `curl … | head -c 100 | grep -q .` under `pipefail`: `head` closes
       the pipe after 100 bytes, `curl` dies writing to it (rc=23), and `pipefail` propagates that as the
       pipeline's status. The bundle is 299 KB, so this was deterministic, not a race — every step after it,
       **including the entire SSE assertion this step exists for**, was unreachable. Fixed with `-o` + `test -s`.
       Step 5 had the same shape and survived only because the preview fits in the 64 KB pipe buffer; it is
       given the same treatment rather than left to luck, since `picker_root` fixtures can grow.
     - **`kill "$SSE"` aborted before the assertion.** `kill` on an already-exited PID returns 1 (the
       `--max-time 10` curl may well have finished), and under `set -e` that killed the script one line before
       `grep -q '^data: '` — turning the meaningful failure into a silent one. `|| true`.

     *Verified sound and deliberately unchanged:* `lsof … && { …; exit 1; }` does **not** trip `set -e` when
     `lsof` finds nothing (non-final command of an AND-list); `sleep 0.2` is fine on BSD `sleep`; and the SSE
     trigger is chosen, not lucky — `enabled:true` with `path:"/nonexistent"` drives `DisableMissingPaths`,
     which fires the `auto-disabled share(s)` ops entry (`reference/smbed/internal/api/server.go:152-154`), the
     only ops-log output a fresh instance can produce without sudo. Under D-8 that entry is emitted after the
     save succeeds (T3.2); the save succeeds here, so the entry still appears.
  8. **Provenance and blast-radius checks.** No file outside the FRD §5.1/§5.2 set plus the enumerated wiring
     points may have been created or modified — as a **scripted allow-list that exits non-zero** on any extra
     path, not a human read-through. **Three** commands are needed, because they answer different questions and
     each is blind to the others' cases: `git status --porcelain -uall` covers untracked-and-modified relative
     to the index; `git diff HEAD --name-only` covers what is **staged**; and `git diff --name-only "$BASE"`
     covers everything **committed since the run began**.
     ```bash
     BASE=$(cat .smbedit-base.sha)     # recorded by T0.1
     ALLOWED=$(printf '%s\n' \
       .gitignore README.md package.json package-lock.json tsconfig.json \
       unified-webapp-example.json docs/smbedit-frd.md docs/smbedit-plan.md docs/smbedit.md \
       cmd/server/main.go cmd/server/dispatch_test.go internal/platform/config/config.go | sort)
     CHANGED=$( { git status --porcelain -uall | cut -c4-; git diff HEAD --name-only; \
                  git diff --name-only "$BASE"; } | sort -u )
     UNEXPECTED=$(printf '%s\n' "$CHANGED" | grep -v -e '^internal/smbedit/' -e '^web/smbedit/' \
       -e '^internal/platform/config/.*_test\.go$' \
       | grep -vxF "$ALLOWED" || true)
     [ -z "$UNEXPECTED" ] || { printf 'files outside the authorized set:\n%s\n' "$UNEXPECTED"; exit 1; }
     ```
     (`|| true` on the final `grep`: no unexpected paths means no matches means exit 1, which is the success
     case. `-uall` is what stops an untracked directory from collapsing to one line and hiding its contents.
     `.smbedit-reference.sha`, `.smbedit-base.sha` and `reference/` are gitignored by T0.1, so none appears.)

     **The `$BASE` source is what makes this gate able to fail at all (v4 fix).** v3 claimed `git diff HEAD`
     covered "anything already staged **or committed** during the run". The staged half is true; the committed
     half is false — `git diff HEAD` compares the working tree to `HEAD`, and a commit *moves* `HEAD`. Since
     T5.5 tells ralph to commit Phase 5, the expected path was the vacuous one: reproduced in a scratch repo,
     an unauthorized file committed mid-run yielded `UNEXPECTED=[]` and rc=0. With the `$BASE` source added the
     same scenario exits 1 and names the file, and the clean case still exits 0. A ref captured at the start is
     the only one of the three that sees committed work, because it is the only one that does not move.

     *Known latent caveat:* `cut -c4-` mangles the `R  old -> new` rename form into a single bogus path, the
     same defect correctly diagnosed for `awk` in T5.5. No task in this plan renames a tracked file, so this is
     latent rather than live — recorded so nobody reads `cut` as having solved that problem.

     Then verify `reference/` is genuinely untouched by **re-computing the T0.1 fingerprint**, since
     `git diff -- reference/` is vacuous once the directory is gitignored — empty by construction, and it would
     pass even if `reference/` had been deleted:
     ```bash
     find reference -type f ! -name .DS_Store -print0 | sort -z | xargs -0 shasum | shasum \
       | diff - .smbedit-reference.sha
     ```
     Fail-safe by construction: a missing or unreadable baseline makes `diff` **error**, not pass. Acceptance §9.5.
- Verify: all eight steps exit 0 / match expectations.
- Covers: §9.1, §9.2, §9.3 (automated portion), §9.5, G3, G4.

**Remaining manual acceptance (cannot be automated from this repo, for the owner):** §9.3's browser pass and
§9.4's Linux-host Samba pass with and without sudoers — delivered as the named "Owner acceptance checklist"
section of `docs/smbedit.md` (T6.2).

---

## 6. Task and phase summary

| Phase | Tasks | Theme | Gate |
|---|---|---|---|
| 0 | T0.1 – T0.2 | Repo hygiene + reference fingerprint + **baseline commit ref**; throwaway spike of the pinned toolchain **and this plan's own shell** | `git check-ignore`; four bundle assertions + clean `tsc` (rc=0 on pinned TS5 with the CSS shim); **three** dry-runs — trap, allow-lists, blast-radius vs a committed rogue; repo porcelain byte-identical before/after |
| 1 | T1.1 | Platform config | `go test -race -count=1 ./internal/platform/config/` |
| 2 | T2.1 – T2.4 | Backend core (oplog, state, samba, picker) | full-package `go test -race -count=1` + `go test -list` count floor (4 → 13 → 27 → 30) |
| 3 | T3.1 – T3.5 | HTTP layer (13 routes, handlers, SSE, `Build`, panic recovery) | full-package `go test -race -count=1` + per-test goleak + Flusher-survives-D-9 assertion |
| 4 | T4.1 | Dispatch wiring | `make test` |
| 5 | T5.1 – T5.5 | Frontend port (deps, **sources**, **scripts**, tsconfig, bundle) | `make typecheck` + scripted bundle assertions |
| 6 | T6.1 – T6.3 | Example config, operator docs + owner checklist, README | config unmarshal + content greps |
| 7 | T7.1 | Full verification | 8-step sweep incl. operable SSE e2e + reference fingerprint |

**8 phases, 22 tasks.**

---

## 7. ADR — Architecture Decision Record

**Decision.** Port smbed into unified-webapp as a **single Go package** `internal/smbedit`, executed
**backend-first in eight sequenced phases fronted by a throwaway frontend-toolchain spike**, preserving
smbed's REST contract, `smb.conf` template, and React frontend; changing only what the platform forces
(stdlib routing, on-disk static serving, operator/state config split, mutex-guarded atomic state writes,
esbuild instead of Vite, a module-local panic-recovery wrapper) plus the four downstream deltas D-5…D-8
enumerated in §1.

**Drivers.**
1. ralph executes autonomously and sequentially — every task must be independently verifiable, must leave the
   tree green, and must not leave a repo-wide command broken for later tasks.
2. FRD decisions D1–D6 are fixed; only package structure and sequencing remain open.
3. Risk is concentrated in two areas (React-under-esbuild; SSE + concurrency) that must be isolated from the
   mechanical bulk of the port.

**Alternatives considered.**
- *Sub-package mirror of smbed's layout* (Option B) — 1:1 diffability, at the cost of an export surface that
  exists only for internal reachability and awkward placement of the unexported command seams FR-11 depends on.
- *Vertical feature slices* (Option C) — per-slice demoability, at the cost of repeatedly rebuilding shared
  foundations, five recommits of the generated bundle, and interleaving both high-risk areas into every slice.
- *Keep chi* — rejected by FRD §5.4: the repo has zero router dependencies and stays that way; `ServeMux`
  method patterns (Go 1.22+, and `go.mod` targets 1.26) cover every route smbed uses.
- *Drop chi's `Recoverer` without replacement* — rejected, and FRD §5.4 now rejects it explicitly. **The reason
  is narrower than an earlier draft of this plan claimed** (which asserted, wrongly, that a panic would take all
  seven modules down): `net/http` installs its own per-connection `recover()`, so the process survives. What is
  lost without a replacement is the *response* — the client gets a bare transport EOF with no status and no
  body, and nothing is logged by the module. Replaced module-locally (D-9), with the `Flush()`-forwarding
  requirement that makes the replacement safe for SSE.
- *smb.conf as the single source of truth instead of `state.json`* — evaluated and declined in FRD D5.
- *Port the jest suite* — declined in FRD D3.
- *Rewrite the frontend in vanilla TS to match the older modules* — declined in FRD D1.
- *Frontend-first or frontend-parallel sequencing* — rejected: the frontend's only meaningful runtime
  verification is against a working backend. The toolchain risk that motivated considering it is addressed
  more cheaply by T0.2's throwaway spike.

**Why chosen.** Option A is the only candidate satisfying all three drivers at once. Driver 2 removes Option C's
justification. Driver 3 rules Option C out affirmatively. Between A and B, driver 1 decides: a single package
keeps the in-package tests that swap unexported vars portable nearly verbatim, which is what makes TR-1's
"coverage must not regress" achievable by transcription rather than rewrite. FRD §5.1 pre-blessed the
single-package layout. Option A's one real weakness — the highest-uncertainty phase sits last — is bought off
directly by T0.2 rather than by reordering phases into a worse shape.

**Consequences.**
- One package of roughly 1,100 LOC plus ~900 LOC of tests. Navigability depends on file-per-former-package
  naming discipline; a file exceeding ~400 LOC is split by concern *within* the package, never by adding a
  sub-package without re-opening this decision.
- `go.mod` is unchanged — no new Go dependencies. `package.json` gains exactly four entries
  (`react`, `react-dom`, `@types/react`, `@types/react-dom`).
- The committed tree grows by roughly **299 KB of JS and 16 KB of CSS** (measured, unminified — these are
  esbuild's own summary figures in KiB, the metric T0.2 step 5 pins; the same build reads 308,315 and 15,875
  raw bytes under `wc -c`). `--minify` is
  deliberately not used: it would deviate from the multissh precedent and make the committed artifact harder to
  diff. Accepted as the price of D1 and of the "`make build` needs no npm" convention.
- **Wrong-method requests to API paths return 404 in production, not 405.** The static handler owns `/` and
  claims the request before `ServeMux` can emit a 405, so chi's 405 becomes the house 404 (already the
  asserted convention in `internal/multissh/static_test.go:47-49`). Pinned by tests for both server shapes so
  it cannot drift unnoticed.
- **Two shared-file edits are mutating, not additive** (root `tsconfig.json` compiler options; the existing
  `build`/`build:dev` script strings). Verified safe — all three existing TS modules still typecheck and
  bundle — but the claim is proven per-task, not assumed.
- Process-group teardown of the `sudo tail` grandchild (D-6) is correct by construction but **not CI-verifiable**
  without shelling out. Covered by the manual Linux acceptance pass only.
- **The D-9 wrapper puts a second `ResponseWriter` implementation in every request path.** It must forward
  `Flush()` and `Unwrap()` or it silently disables both SSE endpoints (FR-8). This is pinned by a direct
  assertion in T3.1 and by T3.4 mounting `Handler()` rather than the mux — but it is a standing hazard for
  anyone who later adds a second wrapper (`Hijack`, `ReadFrom`, and `Push` would need the same treatment if a
  future handler needs them).
- **This plan's verification shell is itself unverified code.** Three rounds of review found more defects in
  the plan's own commands than in its design — and in every round the defects were in the shell that round had
  just introduced, never in shell that had already been executed. T0.2 step 6 dry-runs the novel constructs on
  day one, and §4.6 lists the harness as explicitly untested — but the honest statement is that a pinned
  command is a liability as well as an asset, and the mitigation is running it early, not writing it more
  carefully. The rule v4 adopts and the reviewers converged on: **a gate with no escape hatch must be executed
  before it is pinned**, since removing the hedges (correctly) converts every soft-wrong gate into a hard halt.
- **Two of this plan's gates depend on baseline files created in T0.1** (`.smbedit-reference.sha`,
  `.smbedit-base.sha`). Both are gitignored and repo-local, and both fail *safe*: a missing or unreadable
  baseline makes the consuming command error rather than pass. But they are state carried across a multi-hour
  run, and a `git clean -x` between Phase 0 and Phase 7 would destroy them — which is why T0.1 also records
  both values in its completion note as a second, independent copy.
- `/api/version` reports `"dev"` on every build until someone adds a `-ldflags` stamp (follow-up #6).
- The frontend is not visually exercised until Phase 5; the API contract is test-locked from Phase 3 and the
  toolchain is proven in Phase 0, so a Phase 5 failure is narrowly attributable.
- `internal/smbedit` becomes the first React-bearing module. It establishes the esbuild+React pattern
  (`--jsx=automatic`, `--define:process.env.NODE_ENV`, root-tsconfig inclusion) that a future migration of the
  older modules would follow.
- `reference/` becomes gitignored, so the source of truth for the port lives only on the developer's machine.
  Anything that must survive is captured in the FRD, this plan, or the ported code — and its integrity during
  execution is guarded by a checksum fingerprint rather than by a git diff.

**Follow-ups (explicitly not in this plan).**
1. Rename the `smb.conf` header line from "Generated by smbed" to "smbedit" — FRD FR-3 permits it provided the
   template and its golden tests change together; deferred so the ported tests stay copy-paste (see §8 A2).
2. Reintroduce a frontend test layer if a concrete regression justifies it — revisit D3.
3. Migrate the vanilla-TS modules to the React+esbuild pattern this module establishes — separate round per D1.
4. Revisit the "smb.conf as source of truth" persistence model — prerequisites in D5.
5. `middleware.Wrap`'s `Access-Control-Allow-Methods` omits `PUT`. Harmless here (the UI is same-origin, so
   CORS never applies), but it would bite any future cross-origin client. Shared-code change, out of scope per
   FRD §11 — **do not edit it during this work**.
6. Stamp a real version via `-ldflags -X` in the Makefile so `/api/version` reports something other than
   `"dev"`. Needs a repo-wide versioning convention that does not exist yet.
7. Consider hoisting the module-local panic recovery (D-9) into the platform middleware chain so all seven
   modules get it. Shared-code change, out of scope here.

---

## 8. FRD ambiguities resolved

| # | Ambiguity | Resolution |
|---|---|---|
| **A1** | `GET /api/version` must keep working (FR-2), but unified-webapp has **no version concept anywhere** — no version var in `cmd/server/main.go` or `internal/platform/`, and smbed's came from its own `main`. | Declare `var Version = "dev"` at package level in `internal/smbedit`, overridable at build time via `-ldflags`. `GET /api/version` returns `{"version": Version}`, preserving the endpoint shape with no platform-wide change. Absent a Makefile stamp it always reports `"dev"` — intended; ADR follow-up #6. (T3.3) |
| **A2** | FR-3 demands byte-identical `smb.conf` rendering while also permitting the header line to be changed from "smbed" to "smbedit" "in template + tests together". | **Keep `# Generated by smbed — do not edit by hand.` verbatim.** FR-3's primary clause is byte-identity; keeping it lets the 276-LOC golden render test port unmodified, which is the strongest available FR-3 check. A coordinated rename is ADR follow-up #1. |
| **A3** | FR-1 requires `Build()` to **fail** without a valid `static_dir`, but smbed's 269-LOC API test suite constructs the server with `NewServer(cfg, "test", nil)` — no static serving at all. Porting those tests through `Build()` would force a fake static dir into every one. | Split construction the way multissh already does: an unexported `newServer(opts)`/`Handler()` pair that registers static routes only when `static_dir` is non-empty, and a `Build()` that validates config and then calls it. Both FR-1 and TR-1's copy-paste-ability are satisfied. **Consequence made explicit in v2:** the two shapes differ on wrong-method behavior (405 without static, 404 with), so both are asserted. (T3.1, T3.5) |
| **A4** | TR-1 says "adapt to the new state-file location" without saying how. smbed's tests do `t.Setenv("HOME", t.TempDir())` because `~/.smbed.json` was hardcoded. | State lives at `<data_dir>/state.json` with `data_dir` injected, so tests pass a `t.TempDir()` directly. **No test may mutate `HOME`** — that pattern does not port and would be fragile under `-race` with parallel tests. (T2.2) |
| **A5** | §5.3 says an empty `picker_root` means the default `/opt`, but `/opt` frequently does not exist on the darwin dev machine, and smbed's handler 500s when `ReadDir` fails. | Preserve smbed's behavior exactly — a missing picker root yields a 500 with the error text, and the UI surfaces it. Do **not** add a "return empty list" softening: that is a feature change, and FR-7 says the picker lists `picker_root`'s subdirectories, full stop. Tests always pass an explicit `t.TempDir()` root, so darwin is unaffected. (T2.4) |
| **A6** | §5.7 leaves CSS handling open: "let esbuild emit `bundle.css` from the CSS import, or link `style.css` directly from index.html — either, consistently." | Follow the multissh precedent: keep `import './styles.css'` in `App.tsx`, let `--bundle` emit `web/smbedit/js/bundle.css`, and link it from `index.html` as `js/bundle.css`. One artifact pair, one build command, identical to the module next door. A local `css.d.ts` shim is added — **required out-of-tree and on TypeScript 7, redundant in-repo on the pinned `^5.4.0`** (v4 correction: the v3 flat "not required" held only in-repo and on TS 5; `web/multissh/js/css.d.ts` declares `*.css` ambiently inside the root include glob, but that scope does not reach T0.2's spike, where its absence produces `TS2882` and halts the gate). Created unconditionally either way. (T5.1, T5.5, T0.2) |
| **A7** *(premise corrected in v3)* | §5.4 drops chi's middleware in favour of the platform's `middleware.Wrap`, but does not say whether routes need their own `OPTIONS` handling. | `middleware.Wrap` (verified: `internal/platform/middleware`) answers every `OPTIONS` with 204 before the dispatcher is reached, so **no per-route `OPTIONS` handlers**. (T3.1) **`Recoverer` is no longer part of this ambiguity.** v2 claimed the FRD "does not say what replaces chi's `Recoverer`" and booked D-9 as an ambiguity resolution. That premise was false — §5.4 addressed `Recoverer` explicitly — and the divergence should have gone through the FRD-amendment channel that A10/A11 used, not been resolved unilaterally here. It since has: FRD §5.4 now mandates the module-local recovery wrapper *and* its `Flush()`/`Unwrap()` forwarding, so D-9 is FRD-mandated (§1, first delta table) rather than plan-introduced. |
| **A8** | §5.7's "no other source changes beyond what compilation outside Vite requires (e.g. env/`import.meta` references, if any)" left it unknown whether any exist. | Verified against `reference/smbed/web/src/`: **zero** `import.meta`, `process.env`, or `NODE_ENV` references in the application sources. The Vite dependency is entirely in `vite.config.ts` and is simply not ported. The `process.env.NODE_ENV` risk is real but lives inside `react-dom`, hence the mandatory `--define` in T5.3 rather than a source edit. |
| **A9** | §5.1's layout sketch lists `state.go` and `samba.go` without saying who owns the ops-log hook. smbed uses a **process-global** `samba.Logf`, mutated in `NewServer`. | The dispatcher builds each module exactly once (verified in `buildDispatcher`), so a single smbedit instance is guaranteed today. Still, wire the log hook per-server at construction rather than as a package-global side effect, so the invariant is not silently depended upon. (D-7, T3.3) |
| **A10** *(raised in v2; **resolved at source** in v3)* | §5.7 originally said to port the sources "minus the `listen_addr` field from `SettingsPage` and from `api.ts`" — naming two files, where the reference references `listen_addr` at **five sites across three files**, including two in `App.tsx` that pass it into `api.putConfig`. | No longer an ambiguity: **FRD §5.7 now names `App.tsx` lines 83 and 136 directly** (`smbedit-frd.md:147`), so T5.2's five-site table is FRD-mandated rather than plan-authorized. Retained here as the record of how the omission was found — without it the port leaves two TS2339 errors and the task's own grep can never pass. |
| **A11** *(raised in v2; **resolved at source** in v3)* | The FRD was silent on the three user-visible `~/.smbed.json` strings in the UI (`App.tsx:92`, `App.tsx:241`, `SettingsPage.tsx:27`), which become false after the config split and contradict the migration doc FR-17 mandates. | No longer an ambiguity: **FRD §5.7 now mandates updating all three to `state.json`** (`smbedit-frd.md:148`). T5.2 implements it with no absolute path baked in, since `data_dir` is operator-configurable. Grep-verified. |
| **A12** *(raised in v2; **resolved at source** in v3)* | FR-2 and §4 said "12 API endpoints"; the reference router registers **13 handlers across 10 paths**. | No longer a discrepancy: **FR-2 now enumerates all 13 method+path registrations across 10 distinct paths** (`smbedit-frd.md:170`), matching T3.1's normative table exactly. Nothing in this plan is left asserting "12", and nothing is left flagged as pending on this point. |

---

## 9. Out of scope

From FRD §3 and §11 — ralph must not do any of these:

- **Anything under `reference/`.** Never modified, never imported, never built. Integrity is proven at T7.1 by
  re-computing the T0.1 checksum fingerprint stored at the gitignored repo-local path
  `.smbedit-reference.sha` — **not** by `git diff -- reference/`, which is vacuous once the directory is
  gitignored.
- **`unified-webapp.json`** — the tracked *live* operator config, distinct from `unified-webapp-example.json`.
  Only the example file is edited (T6.1).
- **Other modules' code.** The only shared files touched are the wiring points: `cmd/server/main.go`,
  `internal/platform/config/config.go`, root `package.json` / `package-lock.json` / `tsconfig.json`,
  `.gitignore`, `README.md`, `unified-webapp-example.json`. `Makefile` only if genuinely required (it is not
  expected to be — `web`, `typecheck`, `test`, and `build` already cover the new module).
- **`internal/platform/middleware`** — neither the missing `PUT` in `Access-Control-Allow-Methods` (ADR
  follow-up #5) nor hoisting panic recovery into the chain (follow-up #7) is in scope.
- **Authentication or authorization.** Reaching the hostname is the access boundary, consistent with the rest
  of the platform. Restated as a warning in the docs, not implemented.
- **macOS/Windows Samba operation.** The module must compile and unit-test on darwin; `sudo` write and restart
  behavior is only expected to function on Linux.
- **New features.** No multi-file `smb.conf` includes, no per-share owners, no user management, no validation
  of Samba option names, no UI redesign.
- **A standalone smbed binary in this repo.**
- **Automatic migration code** from `~/.smbed.json`. Migration is manual and documented (FRD §8).
- **The jest/testing-library suite** (D3), **Vite or any Vite plugin** (D1), **chi or any HTTP router**
  (§5.4), and **any Go toolchain upgrade** (D6 — authorized but not required, and not expected to be needed).

---

## 10. Review responses

Three review rounds, kept separately so a claim can be traced to the round that produced it. **Where an earlier
disposition was later found wrong, its row says so and points at the item that corrects it** — the appendix is
a record, not a scoreboard, and a superseded row left unqualified is worse than no row.

### 10.A — Iteration 3 (v3 → v4)

Both reviewers independently converged on the **same two defects**, and both executed the shell rather than
reading it. Both are in commands v3 newly introduced and reasoned about rather than ran — the third consecutive
round where that was the failure mode, and the reason T0.2 step 6 is widened rather than the reviews being
answered one command at a time. The Critic stated exact flip-to-APPROVE conditions; both were applied, and
**both were reproduced and then re-verified here before being pinned**, using the same commands the reviewers
ran. Nothing in v4 changes a phase, a task boundary, or a design decision.

| ID | Source | Change |
|---|---|---|
| **W-R1** | Arch R1 + Crit B1 | **[T0.2 steps 2 and 4]** The day-one `tsc` gate could not pass. Root-caused to **two independent defects on one line**, both fixed. (a) *Unpinned install:* bare `typescript` resolved to **7.0.2** while the repo pins `^5.4.0`, so the gate tested a compiler this project will never run — now pinned to `typescript@^5.4.0 esbuild@^0.28.0`, mirroring `package.json` (measured: 5.9.3 / 0.28.2). (b) *No ambient CSS declaration:* `reference/smbed/web/` contains **no `.d.ts` of any kind** — Vite supplied it at build time — so `App.tsx:10`'s `import './styles.css'` raises `TS2882` under TS 7. A `src/css.d.ts` mirroring T5.1's is now written after the `cp`. Reproduced and verified here as a 2×2 matrix (unpinned+no-shim → rc=1; every other cell → rc=0), pinned in the task. *Adopted in substance but not in form:* the suggested `src/*.d.ts` addition to the `tsc` file list was measured **redundant** — the existing `src/*.ts` glob already matches `css.d.ts`, since every `.d.ts` is a `.ts` — so the shim is created and the command is left unchanged. |
| **W-R2** | Arch R2 + Crit B2 | **[T0.1, T7.1 step 8]** The §9.5 blast-radius gate — the plan's strongest — **could not fail once ralph committed**, and its prose claimed the opposite. `git diff HEAD` compares the working tree to `HEAD`, and a commit moves `HEAD`; since T5.5 instructs a mid-run commit, the *expected* path was the vacuous one. T0.1 now records `git rev-parse HEAD > .smbedit-base.sha` (gitignored beside the fingerprint, and duplicated into the completion note for the same reason), and step 8's `CHANGED` gains a third source, `git diff --name-only "$BASE"`. Reproduced here in a scratch repo: v3's two-source form returned `UNEXPECTED=[]` rc=**0** against a committed rogue file; the v4 form returns rc=**1** naming it, and rc=0 on the clean case. The false prose claim is corrected in place rather than deleted. This is the same commit-blindness argument v3 used to reject `git ls-files -o` for T5.5 (V-R3) — v3 fixed the narrow instance and left the broad one. |

| ID | Source | Disposition |
|---|---|---|
| W-S1 | Arch synthesis | **Adopted.** T0.2 step 6 becomes **three** dry-runs, not two: the guarded trap, the allow-list primitives, and the blast-radius gate against a scratch repo with a **committed** rogue file. Both W-R1 and W-R2 would have been caught on day one by a step that already exists for exactly this purpose and is already budgeted. Also pinned: the gate must exit 0 on the clean case, since a gate that only ever fails is no better than one that only ever passes. |
| W-S2 | Arch 5 | **Adopted.** Step 6.2's scratch `git init` is pinned to `"$SPIKE"/gitprobe` — as written it was unlocated, so step 7's `rm -rf /tmp/smbedit-spike` reclaimed it only by luck, and an executor could have created it under the repo root. |
| W-S3 | Crit NB2 | **Adopted.** Step 6.2 had drifted from the gate it protects: it exercised the `git status` form that T5.5 stopped using at V-R3. It now dry-runs **T5.5's actual `find` form** (rc=0 clean, rc=1 on a stray, rc=1 on a missing file) alongside T7.1's `git status` form. |
| W-S4 | Arch 3 + Crit NB3 | **Adopted.** T0.2 step 5 pins **which** size metric: esbuild's own summary line (KiB), the unit the ADR's figures are quoted in. Measured `bundle.js` 301.1kb / 308,315 B and `bundle.css` 15.5kb / 15,875 B — esbuild's `kb` is bytes ÷ 1024, so the two are one measurement in two units, not a discrepancy. Without this a correct build reads as 3% drift, the same order as the signal. The assertion only became meaningful once W-R1 pinned the dependency set. |
| W-S5 | Arch 4 | **Adopted.** T5.1's `git diff HEAD --name-only -- package.json package-lock.json` gets the same `$BASE` source as step 8 — identical bug, free to fix now that T0.1 records the ref. Bounded in practice because the adjacent `node -e` assertion is commit-agnostic, but bounded-by-luck is not a reason to leave it. |
| W-S6 | Crit NB1 | **Adopted.** T7.1 step 5 is backstopped by re-running T5.5's `find` gate. The subset test is honest but git-relative, so it cannot confirm the "adds nothing" half of its own title once work is committed; the `find` form is the only commit-agnostic gate in the plan. |
| W-S7 | Crit NB5 | **Adopted.** T5.2's `ls web/smbedit/src/ | sort` "matches the expected file list" was the **last unscripted expectation in the plan**, and the list it referred to was never written down. Now a `diff` against an inline, fixed 13-file list, cross-checked here against both T5.5's whole-directory list and the real reference inventory (12 ported sources + `css.d.ts`); the no-`*.test.*` clause follows from exactness and is dropped as redundant. |
| W-S8 | Crit NB6 + Arch 1 | **Adopted.** §8 A6 and T5.1 restated as **version- and scope-conditional**: the `css.d.ts` shim is redundant in-repo on TS `^5.4.0` and **required** out-of-tree or on TS 7. v3's flat "defensive, not required" was true only under assumptions it never stated — and that unstated assumption is exactly what hid W-R1. Behavior unchanged: T5.1 creates the file either way. |
| W-S9 | Crit NB4 | **Adopted.** T0.2's verify clause — "porcelain byte-identical … capture it before and `diff`" — was prose surrounded by commands. Now two scripted lines with `-uall`, which gates the whole v2 failure class (stray `node_modules/`, mutated `package.json`) without enumerating artifacts in advance. |
| W-S10 | Arch 6 | **Adopted as a note, not a change.** `cut -c4-` inherits the `R  old -> new` rename mangling correctly diagnosed for `awk` in T5.5. No task in this plan renames a tracked file, so it is latent; recorded at step 8 so nobody reads `cut` as having solved that problem. |
| W-S11 | Crit open question | **Adopted.** T0.2 gains a scope note: the spike omits the `listen_addr` / `state.json` deltas, so it typechecks the **reference** sources. That is right for a toolchain probe, but it means the delta edits are first typechecked at T5.4 — stated so nobody reads T0.2 as broader coverage than it is. |

**Nothing from iteration 3 was rejected.** One item (W-R1's `src/*.d.ts`) was adopted in substance but not in
the suggested form, on measurement; the reasoning is recorded in T0.2 rather than only here.

### 10.B — Iteration 2 (v2 → v3)

Both reviewers confirmed every iteration-1 item is genuinely fixed **in task text**, not merely claimed here;
no item from round 1 was re-litigated. Round 2's findings are almost entirely about the *pinned shell* v2
introduced: turning prose gates into executable ones was right, but four of the resulting commands did not run
or did not discriminate on a real machine. Every finding below was reproduced by a reviewer against real tools,
and **independently re-verified here before being acted on** — `net/http` panic recovery, `git ls-files` vs.
`git status -uall` collapsing, the guarded-trap behavior, `kill` on an exited PID, `sort -z`, and
`go test -list` counts were all re-run in this repo rather than taken on trust.

#### Required (Architect) / Blocking (Critic) — all adopted

| ID | Source | Change |
|---|---|---|
| **V-R1** | Arch N3 + Crit B2 | **[T7.1 step 7]** `trap 'kill "${PID:-0}" …'` → `trap 'if [ -n "${PID:-}" ]; then kill "$PID" 2>/dev/null \|\| true; fi; rm -rf "$DATA"'`. On the port-busy early-exit path the old form expanded to `kill 0`, signalling the caller's entire process group — it terminated the invoking shell *and* leaked the temp dir, because the trap killed itself before reaching its own `rm -rf`. Re-verified here: the guarded form exits 1 cleanly, the outer shell survives, the temp dir is removed. |
| **V-R2** | Arch N2a/b/c + Crit B4, NB2, NB3 | **[T0.2]** Rewritten. The spike is now fully self-contained (`cd /tmp/smbedit-spike && npm init -y && npm install …`), so module resolution works and the repo is never touched; the contract-violating `git checkout -- package.json package-lock.json` is deleted; step 3 spells the **shell** form of the define literally (`--define:process.env.NODE_ENV='"production"'`) instead of cross-referencing T5.3's JSON-escaped text, with T5.3 noting the two contexts need different quoting; and step 4's expected error set is corrected to **empty — it must exit 0** (v2's "a small known set is expected" was backwards and licensed waving a genuine error through). |
| **V-R3** | Arch N5 + Crit B3, NB7 | **[T5.5, T7.1 steps 5 and 8]** All three allow-list gates rebuilt, and the *"adjust the expected list"* hedge **deleted** — a gate whose documented remedy is to weaken it is not a gate. T5.5 now uses `find web/smbedit -type f`, which is commit-agnostic and catches a stray file even if tracked; `git status` is asked only where the question really is about the index, and then with `-uall` (untracked trees no longer collapse to one line) and `cut -c4-` (no `awk` whitespace/rename mangling). T7.1 step 5, which had **no `diff` attached at all**, gets a real subset assertion. Step 8 gets the same, plus `git diff HEAD --name-only` to cover anything staged or committed mid-run. **Superseded in part by W-R2:** the "or committed" half of that claim was false — `git diff HEAD` goes quiet once `HEAD` moves — so v3 fixed the narrow instance of commit-blindness (T5.5) while leaving the broad one (step 8) in place, behind prose asserting the opposite. v4 adds the `$BASE` source. *Reconciliation note:* the Architect's `git ls-files -o --exclude-standard` was the suggested primitive; it was **not** adopted, because it lists only *untracked* files — verified here that it goes silent on a modified tracked file, so it would pass vacuously if ralph commits Phase 5 before Phase 7, which is the exact failure class these gates exist to prevent. |
| **V-R4** | Crit B1, NB1 | **[T7.1 step 7]** Step 2's `curl … \| head -c 100 \| grep -q .` died on EPIPE under `pipefail` against the 299 KB bundle (rc=23, deterministic), making every later step — including the SSE assertion the step exists for — unreachable; replaced with `-o "$DATA/bundle.js" && test -s`. Step 5 had the identical shape and survived only because the preview fits the 64 KB pipe buffer; given the same treatment rather than left to luck. And `kill "$SSE" 2>/dev/null` gets `\|\| true`: `kill` on an already-exited PID returns 1 and `set -e` aborted one line *before* the real `grep -q '^data: '` assertion. |
| **V-R5** | Arch N1 + Crit B5 | **[D-9, everywhere]** Two complementary findings, both adopted. *(a) The rationale was false.* v2 asserted four times — once stamped `(verified)` — that dropping chi's `Recoverer` would "kill all seven modules". `net/http` recovers handler panics itself; re-verified here (panic → client EOF, next request → 200, process alive). Restated in §1, §2.3 ADR, §4.2, §4.5, §8 A7 and §10 S14 as what it actually is: `net/http` drops the connection with no status or body, chi returned a clean 500, D-9 restores the 500. §4.2's acceptance criterion drops the vacuous "does not take the process down" clause — it passes identically with D-9 absent — in favour of asserting the 500 and the logged stack. *(b) The implementation can silently break FR-8.* A `wroteHeader`-tracking wrapper does not satisfy `http.Flusher`, because Go promotes only the embedded interface's methods; both SSE handlers assert `w.(http.Flusher)` and 500 with "streaming unsupported" when it fails. T3.1 now requires the wrapper's `ResponseWriter` to forward `Flush()` and `Unwrap()` and asserts it directly; T3.4 requires the SSE tests to mount `srv.Handler()`, not the bare mux, so the wrapper is in the path under test rather than discovered broken at T7.1. |
| **V-R6** | Arch N4 | **[T2.2, §4.1]** "7 tests" corrected to **6** — re-counted in `reference/smbed/internal/config/config_test.go`, and the six named were all of them. The phantom "picker-adjacent one" is `TestListOptFolders_ReturnsDirectories`, which lives in `internal/api/picker_test.go` and belongs to **T2.4**; T2.2 now says so explicitly and T2.4 claims it, so an autonomous executor neither hunts for a phantom nor duplicates the test. |

#### Suggested / non-blocking

| ID | Source | Disposition |
|---|---|---|
| V-S1 | Arch N6, Crit NB4 | **Adopted, and went further than suggested.** Floors switch from `grep -c '^=== RUN'` to `[ "$(go test -list '.*' … \| grep -c '^Test')" -ge N ]` — runnable rather than prose, and **not inflatable**: `=== RUN` counts subtests, so one table-driven test with N cases could satisfy a floor of N while porting nothing (verified: multissh has 38 top-level tests but 52 `=== RUN` lines). The ≥13/≥28 inconsistency is also real and fixed — a package landing at exactly 13 could only reach 27 after all 14 samba tests, so ≥28 would have failed a *correct* port. Floors are now self-consistent at 4 → 13 → 27 → 30 against realistic counts of 4 / 17 / 31 / 34. |
| V-S2 | Arch | **Adopted.** T0.1's fingerprint gains `! -name .DS_Store` (a Finder visit to `reference/` would otherwise forge a mismatch on darwin) and `-print0 \| sort -z \| xargs -0` (space-safe; no such paths today, but its absence is a silent wrong answer rather than an error). Re-verified on this box: `sort -z` is supported and the hash is stable. |
| V-S3 | Arch N8 | **Adopted.** The FRD was corrected at source between rounds, so the stale "FRD says 12 endpoints / out of scope" notes are **deleted** from §1, A12, and the orchestrator-flag list, and A10/A11 are reframed as FRD-mandated (`smbedit-frd.md:147-148, 170`) rather than plan-authorized. D-9 likewise moves into §1's *FRD-mandated* delta table, since §5.4 now requires both it and its `Flush()`/`Unwrap()` forwarding. |
| V-S4 | Arch N7 | **Adopted.** §8 A7's premise was false — FRD §5.4 addressed `Recoverer` explicitly, so D-9 was not filling a silence. A7 now says so plainly and records that the divergence should have gone through the FRD-amendment channel A10/A11 used. It since has. |
| V-S5 | Arch, Crit NB6 | **Adopted, with a side picked.** Both reviewers flagged that the reference emits the `auto-disabled share(s)` ops entry *before* saving (`server.go:152-157`), which under D-8 rollback would leave the log asserting an auto-disable that never persisted. **Pinned in T3.2: emit after a successful save**; on the failure path the only output is the save-failure entry. Declared in §1's D-8 row and asserted by a test. |
| V-S6 | Crit NB5 | **Adopted; extended in v4 by W-S5.** T5.1's `git diff --name-only` → `git diff HEAD --name-only`. The bare form is index-relative, so staging the two files would empty the output and silently invert the gate from "exactly these two changed" to "nothing changed". That fixed the *staged* case only; the *committed* case needed the `$BASE` source added in W-S5. |
| V-S7 | Crit NB8 | **Adopted.** The bash requirement is stated three times where it matters: the header, §5's task conventions, and §4.4's prerequisites. Several gates use process substitution, which under `sh` is a syntax error rather than a failed assertion. |
| V-S8 | Crit NB9 | **Adopted.** The reference fingerprint moves from `/tmp/smbedit-reference.sha` to the repo-local, gitignored `.smbedit-reference.sha` (T0.1 adds the `.gitignore` entry in the same edit). A multi-hour unattended run can outlive `/tmp` reaping, and a missing baseline would fail T7.1 for the wrong reason. |
| V-S9 | Arch synthesis | **Adopted.** T0.2 is extended past the toolchain to dry-run **this plan's own novel shell** — the guarded trap with a forced early exit, and the allow-list primitive against a scratch `git init` — on day one, for the cost the spike was already budgeted. Both v2 defects would have been caught by it. §4.6 now lists the verification harness as explicitly untested, and the ADR records that a pinned command is a liability as well as an asset. |
| V-S10 | Crit open question 3 | **Adopted.** T6.3 states that the README's data-directory layout entry is documentation only and must not seed a `./data/smbedit` directory — `.gitignore`'s unanchored `data` entry would make it silently untracked. |

**Nothing was rejected this round.** One suggestion was adopted in a different form than proposed (V-R3's
`git ls-files -o` → `find` / `git status -uall`), for a reason verified rather than asserted: the suggested
primitive lists only untracked files and goes silent on a modified tracked file.

### 10.C — Iteration 1 (v1 → v2)

#### Required (Architect) — all adopted

| ID | Change |
|---|---|
| A-R1 | Phase 5 reordered: sources (now T5.2) precede build scripts (now T5.3). Renumbered T5.2↔T5.3; added an ordering note explaining why. |
| A-R2 | Removed the incorrect `'\\\"production\\\"'` parenthetical from T5.3. The raw-file form `--define:process.env.NODE_ENV='\"production\"'` now stands alone, with the regex check unchanged. Also corrected in pre-mortem 1. |
| A-R3 | T5.2 delta table now lists **five** `listen_addr` sites across **three** files, including `App.tsx:83` and `App.tsx:136`. Recorded as new ambiguity A10. |
| A-R4 | T0.1 captures a `find … | shasum | shasum` fingerprint; T7.1 step 8 re-computes and diffs it. The vacuous `git diff -- reference/` guard is called out as vacuous in both §9 and T7.1. |
| A-R5 | Every "12 endpoints" replaced by the normative 13-registration / 10-path table in T3.1; §4.2 and §6 updated. FRD's own wording flagged in §1 and A12. |
| A-R6 | The three `~/.smbed.json` UI strings updated to `state.json` as an authorized delta (table in T5.2), grep-verified. Recorded as ambiguity A11. Adopted the recommended option, not the accept-and-document fallback. |
| A-R7 | T3.4 mandates `httptest.NewServer` + a real client for the SSE test and explains the `rr.Body` race it avoids; also stated in §4.2 and pre-mortem 2. |

#### Blocking (Critic) — all adopted

| ID | Change |
|---|---|
| C-B1 | All `-run` filters removed. §4 opens with a standing convention: full-package `go test -race -count=1 ./internal/smbedit/`, plus run-count floors on T2.1–T2.4. *(The `grep -c '^=== RUN'` metric v2 chose was **superseded in v3** by `go test -list` — see V-S1.)* |
| C-B2 | = A-R1. |
| C-B3 | = A-R3. |
| C-B4 | T7.1 step 7 rewritten as a complete `set -euo pipefail` script: background start + PID capture, readiness poll on `/api/version`, `--max-time` on every curl, SSE opened in background *then* a `PUT /api/shares` to generate a live entry, assertion that a `data:` line arrived, `trap`-based teardown, `jq` and free-port prerequisites stated. |
| C-B5 | (a) `node_modules` parking dropped entirely — `make build` invokes only `go build`, so running it *is* the check. (b) T6.1 prefixed with `rm -f`, and its verification now unmarshals `unified-webapp-example.json` itself into `config.Config` rather than only exercising `DefaultConfig()`. |

#### Suggested / non-blocking — 17 adopted, 1 adopted with modification, 0 rejected

| ID | Disposition |
|---|---|
| S1 | **Adopted.** Added T0.2 throwaway spike (nothing committed) before Phase 1; rewrote §2.3 Option A's Cons to name the real late-phase risk as *toolchain*, not API mismatch. |
| S2 | **Adopted.** §1 now declares nine deltas in two tables. D-5 gives the streaming seam an explicit shape: `func(ctx, name string, args ...string) (io.ReadCloser, func() error, error)`. D-6 and D-7 declared. |
| S3 | **Adopted, both halves.** Process-group teardown implemented in `samba_unix.go` (`//go:build unix`, Setpgid + negative-PID kill, `WaitDelay`) **and** the un-testable claim downgraded: §4.6 and the ADR state plainly that this is not CI-verifiable; the test asserts only handler return + goroutine exit. |
| S4 | **Adopted.** goleak moved to T2.1 and switched to the repo's per-test `defer goleak.VerifyNone(t)` style (`internal/multissh/build_test.go:41`) instead of `VerifyTestMain`. The `TestSubscribe_*` cancel() requirement is called out in T2.1 and pre-mortem 2. |
| S5 | **Adopted.** T3.1 and §4.2 assert wrong-method behavior for **both** server shapes — 405 without a static dir, 404 with one — so the divergence is pinned. |
| S6 | **Adopted.** Pre-mortem 1's TS2307 rationale corrected: `web/multissh/js/css.d.ts` already declares `*.css` project-wide inside the root include glob. The smbedit shim is kept but labelled defensive in T5.1 and A6. |
| S7 | **Adopted.** Corrected to the measured **299 KB JS + 16 KB CSS, unminified**, in pre-mortem 1 and the ADR; `--minify` explicitly declined to match the multissh precedent. |
| S8 | **Adopted.** The obsidianoid precedent claim was dropped entirely rather than qualified — it is `os.WriteFile` 0o644 and non-atomic (`internal/obsidianoid/state.go:79`), so citing it risked ralph copying the wrong mechanism. T2.2 now specifies the mechanism directly. |
| S9 | **Adopted.** T2.2 adds parent-directory fsync after rename, a stale `state-*.json.tmp` sweep on load, and notes `CreateTemp` is already 0600 with the `Chmod` kept as belt-and-braces. Pre-mortem 3 updated. |
| S10 | **Adopted.** Picked a side: save failure **rolls back** the in-memory mutation and 500s (D-8), diverging from smbed under §5.5's mandate. Tested in T2.2 and T3.2. |
| S11 | **Adopted with modification.** `--log-level=warning` added to the esbuild invocation, but warnings are **surfaced, not auto-failed** — a benign React warning must not be able to block an autonomous executor. The `Warning:`-string bundle grep (the actual define guard) is kept. |
| S12 | **Adopted.** Principle 5 rewritten to state plainly that T5.3 and T5.4 mutate shared files; each task now proves the existing modules survive. `unified-webapp.json` added to the must-not-touch list in §9. |
| S13 | **Adopted.** The false import-cycle argument is struck from Option B's cons (verified acyclic) and replaced with the real one: sub-packaging complicates placement of the unexported command seams FR-11 depends on. |
| S14 | **Adopted, both halves** — *but the rationale v2 gave was wrong and is **corrected in v3** (see V-R5).* Panic recovery is restored module-locally as D-9. v2 justified it by claiming a handler panic would "make one handler panic an outage for all seven modules"; `net/http` recovers handler panics itself, so that is false. The real, narrower justification — chi returned a clean 500 where bare `net/http` drops the connection with no response — is now stated in §1, T3.1, §4.5, and the ADR. The 405→404 divergence is recorded in §1, T3.1, §4.2, and the ADR. |
| S15 | **Adopted in letter; the v2 commands did not work and are **rewritten in v3** (see V-R3).** T5.1, T5.5, and T7.1 steps 5 and 8 use scripted allow-lists that exit non-zero — but v2's `git status --porcelain` form collapsed untracked trees, step 5 had no `diff` attached at all, and a hedge sentence invited the executor to weaken the expectation until it passed. |
| S16 | **Adopted.** T3.4's range corrected to `server.go:291-388`; the version handler correctly cited as the inline closure at `server.go:80-82`. |
| S17 | **Adopted.** T6.2 now requires a named "Owner acceptance checklist" section in `docs/smbedit.md`, grep-verified. |
| S18 | **Adopted.** Stated in T3.3 and A1; ADR follow-up #6 added. |

### 10.D — Flagged for the orchestrator

**Resolved since v2 — verified present in the FRD, nothing outstanding:**

1. ~~FR-2 / §4's "12 API endpoints"~~ — corrected at source. `smbedit-frd.md:170` now enumerates all 13
   method+path registrations across 10 distinct paths, matching T3.1's normative table.
2. ~~§5.7's two-file `listen_addr` list~~ — corrected at source. `smbedit-frd.md:147` now names `App.tsx`
   lines 83 and 136.
3. ~~§5.7's silence on the three `~/.smbed.json` UI strings~~ — corrected at source. `smbedit-frd.md:148` now
   mandates updating all three to `state.json`.
4. ~~§5.4's dropping of chi's `Recoverer`~~ — amended at source. `smbedit-frd.md:128` now specifies that
   `Recoverer` is *replaced, not dropped*, and — going further than this plan asked — mandates the
   `Flush()`/`Unwrap()` forwarding that keeps the replacement SSE-safe.

**Open — no further FRD gaps found in iterations 2 or 3.** Both rounds were entirely about this plan's own
verification shell, not about requirements coverage. Two items are worth the orchestrator's awareness rather
than an amendment:

5. **FRD §5.4's amended text now embeds an implementation detail** (the `Flush()`/`Unwrap()` forwarding
   requirement) that is genuinely load-bearing but unusually specific for a requirements document. No action
   needed — this plan implements and tests it either way (T3.1, T3.4) — but if the FRD is ever trimmed back
   toward pure requirements, that clause must not be dropped silently: it is the single sentence standing
   between a correct-looking recovery wrapper and both SSE endpoints returning
   `500 "streaming unsupported"` in production only.
6. **The repo's TypeScript pin is load-bearing in a way the FRD does not state** (v4). The port typechecks
   clean on `typescript@^5.4.0`, which is what `package.json` pins today. On TypeScript 7 — measured 7.0.2 —
   `App.tsx`'s side-effect `import './styles.css'` raises `TS2882` unless an ambient `*.css` declaration is in
   scope. This plan makes smbedit safe either way by creating `web/smbedit/src/css.d.ts` unconditionally
   (T5.1), so **no FRD amendment is needed for this port**. Flagged because the exposure is not smbedit's: the
   three existing vanilla-TS modules rely on the same single ambient declaration in `web/multissh/js/css.d.ts`
   reaching them through the root `include` glob, and a future TypeScript major bump is a platform-wide
   question rather than a module one. Out of scope here; worth an owner's decision separately.
