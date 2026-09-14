# utuber — media download module

utuber is a small web frontend over `yt-dlp`: paste a URL, pick video or
audio, and the server downloads it into a Plex-friendly
`Show - S01E02 - Title.m4v` / `.mp3` filename. It was ported into
unified-webapp virtually unchanged from the standalone app; the FRD lives at
[docs/utuber-frd.md](utuber-frd.md).

The module owns its hostname's whole path space (host-header dispatch), so
every endpoint below is root-relative. Like multissh, it has **no login** —
reaching its hostname is the whole access boundary.

## Endpoints

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/` | The single-page frontend (`static_dir/index.html`; unknown paths fall back to it). |
| `POST` | `/enqueue` | Queue a download. Form or query fields: `url` (required), `show`, `title`, `season`, `episode`, `mode` (`video` default, `audio`), `force=1` to override the duplicate check. `204` on success, `409` with the prior entry on a duplicate URL, `400` without `url`. |
| `GET` | `/jobs.json` | The job list the page polls. Each job carries exactly these keys: `ID`, `URL`, `ShowName`, `EpisodeTitle`, `Season`, `Episode`, `Mode`, `Status`, `Progress`, `OutputFile`, `Error`. |
| `GET` | `/ytdlp-update` | Server-sent-events stream of `<python_bin> -m pip install -U yt-dlp`; ends with `__done__` or an `ERROR:` line. |
| `GET`/`POST` | `/settings.json` | Read / save UI settings (see below). Other methods get `405`. |
| `GET` | `/downloads/…` | File server over `download_dir` (finished outputs — and the housekeeping files, see below). |

## Config keys

| Field | Meaning |
|---|---|
| `static_dir` | Frontend directory (a single `index.html`). |
| `download_dir` | Output directory; created at startup, and a build failure (503 for this module only) if it cannot be. Also holds `history.json` and `settings.json`. |
| `workers` | Concurrent download workers. `0` → default 1; clamped to 8 with a warning; negative is a config error. |
| `python_bin` | Interpreter for "Update yt-dlp". Default `python3.12`. Bare command name or path; no spaces or shell metacharacters (it is executed as `argv[0]`, never through a shell). |

## Runtime dependencies

The server shells out to external tools; none are bundled:

- **`yt-dlp`** on `PATH` — metadata fetch and download.
- **`ffmpeg`** on `PATH` — audio (MP3) extraction.
- **A Python with `pip`** — only for the "Update yt-dlp" button; configurable
  via `python_bin` and the settings menu.

## Settings menu (FR-8)

The ☰ button in the topbar opens a settings panel with one field: the Python
interpreter used by "Update yt-dlp". Saving persists it server-side in
`<download_dir>/settings.json` (atomic tmp+rename), so it survives restarts
and applies across browsers and devices. A blank save deletes the file and
falls back to the configured `python_bin`. Values are validated against
`^[A-Za-z0-9._/-]+$` — a rejected value returns `400` and leaves the saved
setting untouched.

Resolution order: saved override → `python_bin` from the config → `python3.12`.

## Shutdown behavior (FR-6)

Workers are deliberately un-stoppable: the unified server has no per-module
shutdown hook, so in-flight downloads die with the process — the same
effective behavior as the standalone app under SIGTERM. Queued jobs are held
in memory only and do not survive a restart; `history.json` (the duplicate
log) does.

## Known limitation

`OSExecutor.Run` launches stdout/stderr scanner goroutines that it does not
join before returning. For the SSE update stream this means late writes to the
response can race the handler's return, and a job's `Progress` text can be
cosmetically stale ("download 99.8%" on a job already `done`). Job state
itself is not at risk — all job writes serialize through the queue. Deferred
deliberately to keep the port virtually unchanged; tracked in the integration
plan (§0.2 / R11).

## Note on `/downloads/`

`history.json` and `settings.json` live inside `download_dir`, so both are
reachable via `/downloads/history.json` and `/downloads/settings.json`.
Neither holds secrets (URLs, filenames, an interpreter name), but be aware of
it before exposing the hostname beyond your LAN.
