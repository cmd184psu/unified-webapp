# utuber — media download module

utuber is a small web frontend over `yt-dlp`: paste a URL, pick video or
audio, and the server downloads it into a Plex-friendly
`Show - S01E02 - Title.mp4` / `.mp3` filename. It was ported into
unified-webapp virtually unchanged from the standalone app; the FRD lives at
[docs/frd/FRD-utuber-taskmaster-lane.md](../frd/FRD-utuber-taskmaster-lane.md).

The module owns its hostname's whole path space (host-header dispatch), so
every endpoint below is root-relative. Like multissh, it has **no login** —
reaching its hostname is the whole access boundary.

utuber no longer runs its own download queue. It submits one-shot **func
tasks** (Go-function tasks, kind `utuber.download`) to a dedicated `utuber`
lane on the shared taskmaster engine (`internal/taskmaster`,
`internal/taskmaster/golane`) — see
[docs/guides/taskmaster.md](taskmaster.md#func-go-function-tasks) for the
general func-task model. utuber owns that lane (`owner: "utuber"`): its
width, retention and visibility live in the taskmaster DB, and it is
manageable from the taskmaster UI/API like any lane (subject to the
owned-lane guards documented there), but every per-job action still goes
through utuber's own `/jobs.json`/`/jobs/cancel`/`/jobs/rerun`/`/jobs/delete`
routes below.

**utuber can run standalone, without taskmaster routed at all
("headless" mode).** If only utuber's hostname is routed, `cmd/server`
still opens the shared taskmaster engine underneath it, just with no
taskmaster HTTP surface mounted and the worker scheduling only utuber's own
(owned) lane — see [Headless "owned lanes only"
mode](taskmaster.md#headless-owned-lanes-only-mode) in the taskmaster guide.
Downloads still queue, run, retry and prune exactly as documented below; the
only thing missing is the taskmaster board/API for cross-module visibility.

## Endpoints

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/` | The single-page frontend (`static_dir/index.html`; unknown paths fall back to it). |
| `POST` | `/enqueue` | Queue a download. Form or query fields: `url` (required), `show`, `title`, `season`, `episode`, `mode` (`video` default, `audio`), `force=1` to override the duplicate check. Builds a `utuber.download` payload and calls `Lane.Submit`. `204` on success, `409` with the prior entry on a duplicate URL, `400` without `url` (or on a payload-validation error). |
| `GET` | `/jobs.json` | The job list the page polls, in lane (FIFO) order. See [`/jobs.json` wire format](#jobsjson-wire-format) below for the exact 15 keys. |
| `POST` | `/jobs/cancel?id=` | Cancel a queued or running download. `200 {"status":"canceled"}` if it was still queued (the pending job is cancelled outright), `200 {"status":"canceling"}` if it was running (SIGKILL is sent; the row finishes `canceled` once the process actually exits), `404` `job not found`, `409` `that download already finished` if it's already terminal, `405` (non-POST). |
| `POST` | `/jobs/rerun?id=` | Re-queue a finished download with the **same payload** (a fresh execution of the same job). `204` on success, `404` `job not found`, `409` `that download is queued or running`, `409` `that download already succeeded` (a succeeded one-shot job refuses re-run — see [Rerun refuses success](#rerun-refuses-a-succeeded-download) below), `405` (non-POST). |
| `POST` | `/jobs/delete?id=` | Remove a queued or finished job (its downloaded file is left on disk). `204` on success, `409` `that job is running and can't be removed`, `404` `job not found`, `405` (non-POST). |
| `GET` | `/ytdlp-update` | Server-sent-events stream of `<python_bin> -m pip install -U yt-dlp`; ends with `__done__` or an `ERROR:` line. |
| `GET`/`POST` | `/settings.json` | Read / save UI settings (see below). Other methods get `405`. |
| `GET` | `/downloads/…` | File server over `download_dir` (finished outputs — and the housekeeping files, see below). |

### `/jobs.json` wire format

Each element of the `GET /jobs.json` array has **exactly** these 15
snake_case keys (this replaced an earlier 11-key PascalCase shape with a
`completed` status — if you have an old client or a saved fixture using
`ID`/`Status: "completed"` etc., it is stale):

```json
{
  "id": "utuber-0a1b2c3d4e5f",
  "status": "running",
  "label": "Show — Episode Title",
  "url": "https://…",
  "show_name": "Show",
  "episode_title": "Episode Title",
  "season": 1,
  "episode": 2,
  "mode": "video",
  "progress": { "pct": 42, "label": "Downloading" },
  "output_file": "",
  "error": "",
  "created_at": "2026-09-28T10:00:00Z",
  "started_at": "2026-09-28T10:00:01Z",
  "finished_at": null
}
```

- `status` is one of `queued | running | success | failed | canceled` — note
  **`success`**, not the old `completed`.
- `progress` is `null` until the first progress report; otherwise
  `{"pct": int|null, "label": string}`, where `pct: null` means
  indeterminate (e.g. "Fetching metadata", "Converting" — anything that
  isn't the percent-driven download phase). It updates live while the job is
  `running` (utuber's page keeps polling every 2000 ms, so no SSE is used
  here).
- `show_name`/`episode_title`/`output_file` come from the job's **result**
  once it succeeds (overriding the submitted payload's values, in case
  metadata lookup filled in something better); before that they reflect the
  submitted payload (`output_file` is `""` until `success`).
- `started_at`/`finished_at` are RFC 3339 timestamps or `null`.

## Config keys

| Field | Meaning |
|---|---|
| `static_dir` | Frontend directory (a single `index.html`). |
| `download_dir` | Output directory; created at startup, and a build failure (503 for this module only) if it cannot be. Also holds `history.json` and `settings.json`. |
| `workers` | **Initial seed only** for the utuber lane's width, applied the first time the lane is created. `0` → default 1; clamped to 8 with a warning; negative is a config error. After the lane exists, the taskmaster DB is authoritative — change concurrency via the ☰ "Downloads at once" field (or the taskmaster UI/API) and it survives restarts; editing this config value afterward has no effect. |
| `python_bin` | Interpreter for "Update yt-dlp". Default `python3.12`. Bare command name or path; no spaces or shell metacharacters (it is executed as `argv[0]`, never through a shell). |
| `cookies_file` | Path to a Netscape-format cookie jar yt-dlp uses to authenticate age-restricted downloads (see "Age-restricted downloads" below). Empty defaults to `<parent of download_dir>/utuber-cookies.txt` — deliberately *outside* `download_dir`, which `/downloads/` serves unauthenticated (see "Note on `/downloads/`"). Setting it to a path inside `download_dir` yourself exposes the cookie file over that route; don't. |

## Runtime dependencies

The server shells out to external tools; none are bundled:

- **`yt-dlp`** on `PATH` — metadata fetch and download.
- **`ffmpeg`** on `PATH` — audio (MP3) extraction.
- **A Python with `pip`** — only for the "Update yt-dlp" button; configurable
  via `python_bin` and the settings menu.

**Age-restricted content needs credentials.** yt-dlp cannot sign in to
YouTube on its own — it needs a Netscape-format cookie jar exported from a
browser session that is already signed in, or every age-gated URL fails with
`ERROR: ... Sign in to confirm your age ...`. See "Age-restricted downloads"
below.

## Progress reporting

While a download runs, utuber reports progress through the taskmaster
`golane.RunContext` handed to its `utuber.download` kind (see
[Func (Go-function) tasks](taskmaster.md#func-go-function-tasks) in the
taskmaster guide for the general mechanism):

- `rc.Progress(-1, "Fetching metadata")` — indeterminate, while resolving
  show/title from yt-dlp's metadata (only when either wasn't already
  supplied on `/enqueue`).
- `rc.SetLabel(show + " — " + title)` — retitles the job as soon as
  show/title are known, so the queue list shows a real name instead of the
  bare URL.
- `rc.Progress(-1, "Downloading")` right before the download starts, then
  `rc.Progress(pct, "Downloading")` on every `[download] N%` line yt-dlp
  emits (parsed with `strconv.ParseFloat`) — the same per-line cadence the
  old queue had, now unthrottled in memory and throttled only for DB
  persistence/board events (`taskmaster.progress_interval_ms`, default
  2000 ms — see the taskmaster guide's [Progress
  reporting](taskmaster.md#progress-reporting)).
- `rc.Progress(-1, "Converting")` during ffmpeg audio extraction (audio mode
  only).
- Every raw yt-dlp/ffmpeg output line also goes to `rc.Log()` regardless of
  whether it matched a progress pattern, so the full raw output is available
  via taskmaster's execution-output SSE stream even though utuber's own page
  doesn't expose that stream itself.

The `/jobs.json` `progress` field (above) is this state as utuber's page
polls it every 2000 ms; `pct: null` means indeterminate (a labeled phase
with no percent), matching the phases above.

## Settings menu (FR-8)

The ☰ button in the topbar opens a settings panel with the Python interpreter
used by "Update yt-dlp", the queue controls (below), plus the age-restricted
cookie jar (further below). Saving the Python interpreter persists it
server-side in `<download_dir>/settings.json` (atomic tmp+rename), so it
survives restarts and applies across browsers and devices. A blank save
deletes the file and falls back to the configured `python_bin`. Values are
validated against `^[A-Za-z0-9._/-]+$` — a rejected value returns `400` and
leaves the saved setting untouched.

Resolution order: saved override → `python_bin` from the config → `python3.12`.

### Queue settings

Three more fields save individually on change, each via `POST
/settings.json` with only that field set (every field in the POST body is
an optional pointer — an absent field is left untouched):

- **"Delete finished downloads after (days)"** (`age_out_days`, 1–365) —
  the utuber lane's retention: a finished download older than this many
  days is pruned (task, executions, and metrics all removed together) by
  taskmaster's hourly retention pruner. Default **10 days**, seeded when the
  lane is first created; changeable at any time via this field (or the
  taskmaster UI, since it's the same lane-DB value).
- **"Downloads at once"** (`concurrent_downloads`, 1–`config.MaxUtuberWorkers`
  i.e. 1–8) — the lane's width (how many downloads run concurrently). Same
  value as `workers` in config, but DB-authoritative after the lane's first
  creation (see the `workers` config row above).
- **"Show in taskmaster"** (`show_in_taskmaster`, bool) — toggles the lane's
  `hidden` flag (inverted: unchecked = hidden). Defaults **on** (shown).
  When hidden, the utuber lane and its jobs are invisible to every
  taskmaster HTTP route and the board (404/filtered), and excluded from
  `GET /api/metrics`. **The lane keeps running either way** — hiding it from
  taskmaster does not pause it; use the queue-pause toggle in utuber's own
  page for that. If the taskmaster hand brake is engaged while the lane is
  hidden, utuber still reports it (`queue_paused_by: "brake"` /
  `brake_engaged: true` in `GET /settings.json`) even though nothing about
  it is visible in the taskmaster UI — releasing the brake still requires
  the taskmaster module to be routed (`DELETE /api/brake`).

`GET /settings.json` returns all of the above (9 keys total, including the
cookie fields below):

```json
{
  "python_bin": "python3.12",
  "age_out_days": 10,
  "concurrent_downloads": 1,
  "show_in_taskmaster": true,
  "queue_paused": false,
  "queue_paused_by": "",
  "brake_engaged": false,
  "cookies_configured": false,
  "cookies_updated_at": null
}
```

`queue_paused_by`, `brake_engaged`, `cookies_configured` and
`cookies_updated_at` are read-only — a POST cannot set them directly;
`queue_paused` is set through the pause toggle (or the taskmaster UI, same
lane). `queue_paused_by` is `""` when running normally, `"brake"` when the
taskmaster hand brake paused it, or `"owner:utuber"` when utuber's own pause
toggle paused it.

## Age-restricted downloads

yt-dlp has no way to log in to YouTube by itself — there is no programmatic
sign-in. To download age-restricted content, you export a Netscape-format
cookie jar from a machine where a browser is already signed into YouTube, and
paste it into utuber's ☰ menu ("Age-restricted cookies" field). Two ways to
produce the export:

- a browser extension that writes Netscape format, e.g. "Get cookies.txt
  LOCALLY"; or
- on the signed-in machine, with yt-dlp itself:
  `yt-dlp --cookies-from-browser chrome --cookies cookies.txt --skip-download <any-url>`

Paste the resulting file's contents into the textarea and click Save. The
server validates it looks like a Netscape cookie file (every non-comment line
has the expected 7 tab-separated fields), writes it to `cookies_file` (or its
default, `<parent of download_dir>/utuber-cookies.txt` — deliberately
*outside* `download_dir`) at permissions `0600`, and never
returns the contents back to the browser — `GET /settings.json` only reports
`cookies_configured` (bool) and `cookies_updated_at` (the file's mtime, or
`null` if unset). Click Clear (or save a blank textarea) to delete the jar
and return to today's no-cookies behavior; that is not an error.

**Cookies go stale.** YouTube rotates session cookies, and an expired jar
fails identically to no jar at all — the ☰ menu shows the jar's age so a
run of failures can be diagnosed as "re-export the cookies" rather than "the
pipeline is broken." When a download fails, the job's `error` field now
carries yt-dlp's own stated reason (e.g. the `Sign in to confirm your age`
message) instead of a bare `exit status 1`, which is usually enough to tell
the two cases apart without re-running anything.

## Shutdown behavior

utuber's queue is now taskmaster's, so shutdown follows taskmaster's
[Shutdown semantics](taskmaster.md#shutdown-semantics): the worker's context
is cancelled, every in-flight download's `exec.CommandContext` receives a
kill signal (bounded by a 10-second `WaitDelay`), the worker joins every
in-flight execution's goroutine before the process exits, and an
in-flight download interrupted this way is recorded **`failed`** (not
`canceled` — that's reserved for an explicit operator cancel). **Queued
(not-yet-started) downloads survive a restart** — they're rows in the
taskmaster DB, not in-memory state, and the worker picks them back up on
the next boot. `history.json` (the duplicate-URL log) also survives, as
before.

### Rerun refuses a succeeded download

`POST /jobs/rerun` (and the equivalent `POST
/api/executions/{id}/rerun` from the taskmaster side) refuses to re-run a
download whose latest attempt already **succeeded** — `409 {"that download
already succeeded"}` (utuber's message) / the taskmaster route's own 409.
A successful one-shot download is done; if you want the file again, submit
a new `/enqueue` (past the duplicate-history check with `force=1` if
needed) rather than re-running the same job. Rerun **is** allowed from
`failed` or `canceled` — it creates a new execution of the same job with
the same payload (URL, show/title, season/episode, mode), so a transient
yt-dlp failure or an operator cancel can be retried without re-entering the
form.

## Note on `/downloads/`

`history.json` and `settings.json` live inside `download_dir`, so both are
reachable via `/downloads/history.json` and `/downloads/settings.json`.
Neither holds secrets (URLs, filenames, an interpreter name), but be aware of
it before exposing the hostname beyond your LAN. The cookie jar is different
— it is a real credential — so its default location
(`<parent of download_dir>/utuber-cookies.txt`) is deliberately outside
`download_dir` and therefore never served by this route.
