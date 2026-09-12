# FRD: utuber Module for unified-webapp

**Status:** Approved (2026-09-11) — OQ resolutions folded in
**Date:** 2026-09-11
**Source of truth:** `reference/utuber-11Sept2026/` (standalone utuber, virtually unchanged)
**Target:** `unified-webapp` module, dispatched by Host header alongside grocery, todo, slideshow, menuserver, obsidianoid, multissh.

---

## 1. Background

utuber is a standalone Go web app that downloads media via `yt-dlp`, optionally
extracts audio via `ffmpeg`, names outputs in Plex/TV-style
`Show - SxxEyy - Title.{m4v,mp3}` format, tracks an in-memory job queue with
worker goroutines, and records completed downloads in a JSON history file used
for duplicate detection.

unified-webapp is a single Go binary hosting multiple modules. Each module
exposes `Build(cfg config.<Module>Config) (http.Handler, error)`, is registered
in `cmd/server/main.go:buildModule`, gets its config struct in
`internal/platform/config/config.go`, and is routed by hostname via the
`host_routing` config map. A module that fails to build serves a 503 with the
reason; the rest of the binary keeps serving.

**Why this is low-risk:** the utuber frontend calls root-relative endpoints
(`/enqueue`, `/jobs.json`, `/downloads/…`, `/ytdlp-update`). Because
unified-webapp dispatches on Host (not path prefix), the module owns the entire
path space for its hostnames. The UI and handler routes work unchanged.

## 2. Goal

Integrate utuber as a unified-webapp module with **behavior identical to the
standalone version**, restructured only as much as the module convention
requires (Build signature, config struct, import paths).

## 3. Non-Goals

- No UI redesign, no feature additions, no endpoint changes — with one
  approved exception: a hamburger settings menu for the Python interpreter
  (FR-8), which also adds `python_bin` config (FR-2) and a settings endpoint
  (FR-4).
- No integration of `reference/utuber-11Sept2026/atomicparsley/` (vendored C++
  project; not referenced by any Go code).
- No migration of `reference/.../junk/`, `downloads/` sample data, `.gz`
  artifacts (`main.go.gz`, `downloader_test.go.gz`), or `coverage.out`.
- No authentication, rate limiting, or multi-user isolation (standalone had none).
- No change to how yt-dlp/ffmpeg are invoked (still exec'd from `$PATH`).

## 4. Functional Requirements

### FR-1: Module registration
- New module name `"utuber"`, buildable via `utuber.Build(cfg.Utuber)` from
  `internal/utuber`, added to `buildModule` in `cmd/server/main.go`.
- Routable from `host_routing` like any other module; multiple hostnames map
  to one shared instance (existing dispatcher rule).
- If `Build` fails (e.g. download dir uncreatable), the standard 503
  `unavailableHandler` applies. Missing `yt-dlp`/`ffmpeg` binaries are NOT a
  build failure — they surface as job errors at runtime, matching standalone
  behavior.

### FR-2: Configuration
- New `UtuberConfig` in `internal/platform/config`:

  ```json
  "utuber": {
    "static_dir": "./web/utuber",
    "download_dir": "./data/utuber/downloads",
    "workers": 1,
    "python_bin": "python3.12"
  }
  ```

- `static_dir` and `download_dir` participate in `~` expansion via
  `ExpandPath`, same as every other module (add `expandUtuberPaths` to `Load`).
- `workers`: 0 means unset → default 1; negative rejected at Load (mirror the
  multissh `normalizeMultissh` single-validation-point pattern).
- `python_bin`: the Python interpreter used by the yt-dlp self-update
  (FR-4/FR-8). Empty means unset → default `python3.12`. This is the
  *default*; a value saved through the UI settings menu (FR-8) overrides it.
- Defaults added to `DefaultConfig()` and therefore to `--init-config` output
  and `unified-webapp-example.json`.
- The standalone `internal/config` package (hardcoded `:8080`, `./downloads`,
  1 worker) is **dropped**; the unified config replaces it.

### FR-3: Code migration (virtually unchanged)
- `reference/.../internal/jobs`    → `internal/utuber/jobs`    (unchanged except module path)
- `reference/.../internal/media`   → `internal/utuber/media`   (unchanged except module path)
- `reference/.../internal/history` → `internal/utuber/history` (unchanged except module path)
- `reference/.../cmd/server/main.go` logic (processor, handleEnqueue,
  handleJobs, handleYtdlpUpdate, randID, safe) → `internal/utuber/`
  (e.g. `build.go` + `handler.go`), minus the standalone server/lifecycle code
  (flag parsing, signal handling, ListenAndServe).
- `reference/.../web/static/index.html` → `web/utuber/index.html`, byte-identical.
- All ported tests come along and pass under `go test ./...`.

### FR-4: HTTP surface (identical to standalone)
Registered on the module's own mux:

| Route | Behavior |
|---|---|
| `GET /` | Serve `static_dir` (index.html) |
| `POST /enqueue` | Multipart form: `url` (required), `show`, `title`, `season`, `episode`, `mode` (`audio` else `video`), `force`. Returns 204 on enqueue; 400 on missing url; **409 + JSON** `{error:"duplicate", output_file, show_name, mode}` when history already has the URL and `force != "1"`. |
| `GET /jobs.json` | JSON array of all jobs (current queue state). |
| `GET /ytdlp-update` | SSE stream of `<python_bin> -m pip install -U yt-dlp` output; `__done__` sentinel on success, `ERROR: …` line on failure. Interpreter resolved per FR-8 (saved setting → config `python_bin` → `python3.12`). |
| `GET /downloads/*` | File server over `download_dir`. |
| `GET /settings.json` | Returns effective UI-editable settings, currently `{"python_bin": "…"}`. |
| `POST /settings.json` | Persists UI-editable settings to `<download_dir>/settings.json`. Rejects a `python_bin` that fails validation (FR-8) with 400. |

### FR-5: Job processing (identical to standalone)
- FIFO in-memory queue (capacity 100), `workers` goroutines, jobs lost on
  process restart (accepted, as standalone).
- Missing show/title auto-filled from `yt-dlp` metadata (uploader/title).
- Progress strings: `fetching metadata` → `downloading` / `download <line>` →
  (`converting` / `convert <line>` for audio) → `done`.
- Output naming: `Show - SxxEyy - Title.m4v` (video, rename) or `.mp3` (audio,
  ffmpeg extract then temp file removed). `safe()` sanitization of `/` and `:`
  preserved as-is.
- Completed jobs recorded to `<download_dir>/history.json` (append-style JSON
  log, opened at Build time; open failure is a Build failure → 503 module).

### FR-6: Lifecycle
- Worker goroutines start inside `Build` with a background context, matching
  the precedent of `slideshow`'s `conductor.Run()`. The unified server has no
  per-module shutdown hook; in-flight downloads die with the process
  (same effective behavior as standalone SIGTERM).

### FR-7: Ops artifacts
- `unified-webapp-example.json`: add a `utuber` section and a
  `utuber-test.cmdhome.net → utuber` routing example.
- `Makefile check`-style warnings for missing `yt-dlp`/`ffmpeg` are **not**
  ported into the unified Makefile (runtime concern; optional follow-up).
- `docs/`: this FRD serves as module doc seed.

### FR-8: Hamburger settings menu (approved addition)
The reference UI has no settings surface (the yt-dlp update is a bare button),
so this is the one deliberate deviation from "virtually unchanged":

- Add a hamburger (☰) button to the topbar opening a small settings panel,
  styled to match the existing UI.
- The panel contains one setting: **Python interpreter** (text field), used by
  the yt-dlp self-update. Shows the effective value from `GET /settings.json`;
  saving `POST`s it back. Blank resets to the config default.
- Persistence is server-side (`<download_dir>/settings.json`) so the setting
  survives across browsers and devices; resolution order for `/ytdlp-update`
  is saved setting → config `python_bin` → `python3.12`.
- Validation: the value must match `^[A-Za-z0-9._/-]+$` (a bare command name
  or path — no spaces, no shell metacharacters). It is passed as argv[0] to
  the executor, never through a shell.

## 5. External Runtime Dependencies

Present on the host, invoked via `$PATH`: `yt-dlp`, `ffmpeg`, and the
configured Python interpreter (default `python3.12`; only for the self-update
endpoint). Absence degrades at runtime per-feature; it never blocks the binary
or other modules.

## 6. Acceptance Criteria

1. `go build ./...` and `make test` pass; all ported utuber tests green.
2. With `host_routing` mapping a hostname to `utuber`, the standalone UI loads
   and a video and an audio download complete end-to-end with correct
   `Show - SxxEyy - Title` naming and history dedup (409 then `force=1` retry).
3. Every other module builds and routes exactly as before (existing
   `dispatch_test.go` extended for `utuber`).
4. Diff between ported Go packages and `reference/` versions is limited to
   package/import paths and the config-struct seam (FR-8's UI/settings
   additions are the sole approved exception).
5. Removing `yt-dlp` from PATH fails the job with an error status but does not
   affect module build or other modules.
6. Hamburger menu: setting the Python interpreter to a bogus value is rejected
   by validation; setting it to a valid alternate interpreter is used by the
   next `/ytdlp-update` run and survives a server restart.

## 7. Resolved Questions (approved 2026-09-11)

- **OQ-1 — resolved:** do NOT hardcode `python3.12`. The interpreter is
  configurable from the UI via a hamburger settings menu (FR-8), backed by a
  `python_bin` config default (FR-2) and server-side persistence.
- **OQ-2 — resolved:** keep subpackages `internal/utuber/{jobs,media,history}`.
- **OQ-3 — resolved:** `download_dir` defaults to `./data/utuber/downloads`.
