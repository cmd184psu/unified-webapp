# AF integration release notes: `af/af-next`

Base: `af/af17` @ `67e1f96` (production binary). Integration branch is **local only**
(`af/af-next`); it is not pushed to GitHub.

## Merge order

1. `af/af18` (merger role)
2. `af/af15b` (planning-loop cap, stage-home pruning, disk watchdog)
3. `af/af16` (seats page and seat registry)
4. `uw/botchat` (internal/botchat port; cutover left OFF)
5. `af/af19` (coordinator role, partial)

## What each piece adds

- **AF-18 (merger):** a `merger` role candidate and stage role that resolves a
  merge too hairy for the deterministic merge; `merger_timeout` setting
  (default `30m0s`); rebuilds `role_candidates` and `stages`.
- **AF-15b:** stage-home pruning (`stage_home_keep_days`, default `1`) and a
  disk watchdog (`min_free_space_gb`, default `20`) that stops new stages
  starting when free space is low; planning-loop cap from AF-15.
- **AF-16 (seats):** seats page and the seat registry (`internal/agentflow/coord`),
  plus the `botchat_seat`/`freebuff_seat` backend types.
- **uw/botchat:** ports `internal/botchat` (module + config) into the unified
  webapp. Cutover is **not** enabled; the relay stays off unless configured.
- **AF-19 (coordinator, partial):** a `coordinator` role candidate and stage
  role that runs the coordination self-check on a schedule;
  `coordinator_check_minutes` (default `60`), `coordinator_runs_per_day`
  (default `24`), `coordinator_timeout` (default `30m0s`); rebuilds
  `role_candidates` and `stages`. Scheduler, fallthrough, guarded tools and the
  ladder are **not** included (backlog B81).

## New settings and defaults

| Setting | Default | Source |
|---|---|---|
| `merger_timeout` | `30m0s` | AF-18 |
| `stage_home_keep_days` | `1` | AF-15b |
| `min_free_space_gb` | `20` | AF-15b |
| `coordinator_check_minutes` | `60` | AF-19 |
| `coordinator_runs_per_day` | `24` | AF-19 |
| `coordinator_timeout` | `30m0s` | AF-19 |

All are inserted with `INSERT OR IGNORE`, so an existing value is preserved.

## Migration order

Existing migrations `1`..`45` are unchanged. The integration adds three new
steps, renumbered into one gapless sequence:

| Version | Source | What it does |
|---|---|---|
| 46 | AF-18 | Rebuilds `role_candidates` and `stages` to add the `merger` role; adds `merger_timeout` setting. |
| 47 | AF-15b | Adds `stage_home_keep_days` and `min_free_space_gb` settings. |
| 48 | AF-19 | Rebuilds `role_candidates` and `stages` to add the `coordinator` role; adds `coordinator_check_minutes`, `coordinator_runs_per_day`, `coordinator_timeout` settings. |

Every migration is idempotent for a database already at `af/af17` (version 45):
table rebuilds recreate and repopulate from the old table, settings use
`INSERT OR IGNORE`.

## Migration proof (production copy)

A copy of the production database was taken with
`sqlite3 ... ".backup /tmp/prodcopy.db"` and opened with `store.Open`:

- before: schema version 45; after: schema version 48
- `PRAGMA integrity_check` = `ok`
- `PRAGMA foreign_key_check` = empty
- row counts identical for every table except `schema_version` (+3) and
  `settings` (+6, the new settings above)

## Rollback

1. Stop the new binary.
2. Restore the database backup taken before upgrade
   (`cp /tmp/prodcopy.db ...` or the pre-upgrade backup) in place of
   `agentflow.db` (and remove any `-wal`/`-shm`).
3. Run the previous binary (`af/af17`).

Migrations are forward-only: a database at version 48 opened by an older binary
is rejected as too new, so the database backup must be restored together with
the binary.

## Test plan (dev)

- AF-18: create an effort whose merge conflicts; confirm a `merger` stage is
  queued and resolves it; check `merger_timeout` setting.
- AF-15b: set `stage_home_keep_days` to 0 and confirm finished stage homes are
  pruned; fill disk below `min_free_space_gb` and confirm new stages do not
  start.
- AF-16: open the seats page, register a seat, assign it to a backend.
- uw/botchat: leave `botchat.url` empty and confirm the relay stays off; with a
  URL set, confirm messages post.
- AF-19: enable the coordinator schedule and confirm `coordinator` stages run
  within `coordinator_check_minutes`.

## Gates

- `gofmt -l` empty
- `go vet ./internal/agentflow/... ./internal/botchat/...` clean
- `go test ./internal/agentflow/... ./internal/botchat/...` green
- `node scripts/test-web.mjs` green (47 suites)
