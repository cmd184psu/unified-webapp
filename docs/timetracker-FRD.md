# FRD — timetracker module for unified-webapp

Integrate **pshelper** (reference copy at `reference/pshelper/`, read-only) into
unified-webapp as the **timetracker** module. Reference is a standalone Go binary
(`serve.go`, stdlib only, ~440 lines) serving a vanilla-JS SPA from `public/`.
Expectation: **minimal change** — feature parity with the reference, ported onto the
platform contract per `docs/adding-a-module.md`.

---

## 1. Goal

One new module name, `timetracker`, buildable via the standard contract:

```go
func Build(cfg config.TimetrackerConfig) (http.Handler, error)
```

routed by Host header like every other module. The reference app is a PS/customer
helper: a customer list (single JSON file), per-customer editable fields with deep
links (Slack, Insight, SFDC, Jira), a 15-minute-block time selector, a markdown
report composer (marked.js), and CSV export/import. No SSE, no websockets, no
background goroutines.

**Support tunnels are dropped entirely** (owner decision, 2026-09-12): the
`/tunnel-mapping` endpoint, `tunnellist.txt`, the `supportTunnel` customer field,
and the Support Tunnel UI do not carry over. See FR-F6.

## 2. Integration requirements (the platform "meld" pattern)

Per `docs/adding-a-module.md` — steps are the checklist; zero platform code changes:

- **IR-1** `TimetrackerConfig` in `internal/platform/config/config.go`:
  ```go
  type TimetrackerConfig struct {
      StaticDir string `json:"static_dir"` // ./web/timetracker
      DataFile  string `json:"data_file"`  // ./data/timetracker.json
  }
  ```
  Plus `Timetracker TimetrackerConfig \`json:"timetracker"\`` on `Config`, an
  `expandTimetrackerPaths` expander called from `Load`, and sane defaults in
  `DefaultConfig`. No `SSEMaxSubscribers` (module has no broker).
- **IR-2** `case "timetracker":` in `buildModule` (`cmd/server/main.go`).
- **IR-3** `"timetracker"` appended to `knownModules`.
- **IR-4** `host_routing` examples updated: `unified-webapp-example.json`
  (`timetracker.cmdhome.net`, `timetracker-test.cmdhome.net`) and
  `local-test/config.json` (`timetracker.test`, matching the `.test` convention).
- **IR-5** Static assets under `web/timetracker/`, served via
  `static.NewHandler(cfg.StaticDir)` mounted at `"/"`.
- **IR-6** Module package `internal/timetracker/` with the house file split:
  `model.go`, `store.go`, `handler.go`, `build.go` + `store_test.go`,
  `handler_test.go`. Handlers use `platform/response` (`WriteJSON`, `WriteError`,
  `WriteDecodeError`); no bespoke error shapes.
- **IR-7** No new Go dependencies expected (reference is stdlib-only: `encoding/json`,
  `encoding/csv`, `bufio`). go.mod / Go toolchain bumps are permitted if something
  requires one, but nothing identified does.
- **IR-8** Auth: **no module code**. Production config SHOULD add
  `auth.modules.timetracker` (customer names, Slack IDs, Jira numbers are mildly
  sensitive); the gate is inherited from the dispatcher. Config-only, operator's call.

### Data placement (deliberate deviation from reference)

The reference keeps its mutable data **inside** the static dir
(`public/data.json`, `public/tunnellist.txt`) — writable state served as static
files. In unified-webapp, mutable state never lives in `web/`:

- `data.json` → `cfg.DataFile` (default `./data/timetracker.json`). Same JSON shape
  (any legacy `supportTunnel` values are ignored on load and dropped on first save),
  so a production `public/data.json` is migrated by copying the file. **Not** copied
  into `web/timetracker/`.
- `tunnellist.txt` — dead (FR-F6). Not copied anywhere, no config key.
- `tooltips.json` **stays static** (frontend fetches `/tooltips.json` as a plain
  file) — it is content, not state. Its `supportTunnel` entry is removed.

## 3. Functional requirements (feature parity)

### 3.1 Data model

`model.go` ports the reference structs verbatim — JSON tags are the API contract:

- `Customer`: `customerName`, `slackChannel`, `slackChannelId`, `insightUrl`,
  `workLoadType`, `sfdcUrl`, `cumulusBucket`, `jira` (all strings; reference's
  `supportTunnel` removed per FR-F6).
- `Data`: `companyName`, `projectName`, `author`, `version`, `customers []Customer`.

### 3.2 API endpoints (shapes preserved exactly)

| Method | Path | Behavior |
|---|---|---|
| GET | `/data` | Full `Data`, customers deep-copied and sorted case-insensitively by name. File on disk keeps insertion order. |
| POST | `/update` | Body `{index, field, value}`. `index == -1` + any field → sets `Author`. Otherwise sets the named field on `customers[index]`. Returns the **full updated `Data`** (reference behavior — the frontend tolerates it). Unknown field → 400. |
| POST | `/delete` | Body `{index}`. Bounds-checked; removes customer; returns full `Data`. |
| POST | `/create-customer` | Appends `{customerName: "New Customer"}`; returns full `Data`. |
| GET | `/export-csv` | `text/csv`, `Content-Disposition: attachment; filename=customers.csv`, header row + customers sorted case-insensitively, 8 columns (`CustomerName, SlackChannel, SlackChannelId, InsightUrl, WorkLoadType, SfdcUrl, CumulusBucket, Jira` — reference order minus `SupportTunnel`, FR-F6). |
| POST | `/import-csv` | Multipart field `file`. Skips header row, **replaces** the entire customer list, returns full `Data`. Accepts 8-column rows; 9-column rows from a legacy pshelper export are also accepted, with the `SupportTunnel` column (index 6) dropped. |
| GET | `/` (everything else) | `platform/static` file server over `web/timetracker/` with SPA fallback. |

The `/update` field switch covers: `customerName`, `slackChannel`, `insightUrl`,
`workLoadType`, `sfdcUrl`, `cumulusBucket`, `jira`, plus `author` via `index == -1`
(`supportTunnel` removed, FR-F6). `slackChannelId` is intentionally **not**
updatable via `/update` (reference behavior; CSV import is its only write path).

### 3.3 Required fixes (deviations from reference, each justified)

- **FR-F1 — `/update` accepts `field: "newCustomer"`.** The reference frontend's
  *Add Customer* button POSTs `/update` with `{index: len(customers), field:
  "newCustomer", value: {…empty customer…}}`, which the reference server rejects
  (400, no such case) — **Add Customer is broken in the reference as shipped**
  (`/create-customer` exists server-side but nothing calls it). Fix on the server so
  the copied frontend works untouched: when `field == "newCustomer"`, decode `value`
  as a `Customer` and append it. `/create-customer` is still implemented (3.2) for
  contract parity.
- **FR-F2 — `/update` bounds-checks `index`.** Reference indexes
  `data.Customers[update.Index]` unvalidated → out-of-range panic kills the
  process. Invalid index (`< -1` or `>= len` for field updates) → 400.
- **FR-F3 — concurrency safety.** Reference has zero locking and non-atomic saves
  (`os.Create` + encode). Port to the grocery-style single-file store:
  `sync.RWMutex` around all access, writes via temp-file + rename.
- **FR-F4 — CSV import must not destroy data on malformed input.** Reference clears
  `data.Customers` *then* parses, silently stopping at the first bad row — a short
  row can wipe the dataset. Parse the whole CSV into memory first; only on success
  replace and save. Rows with ≠9 fields → 400, existing data untouched.
- **FR-F5 — error envelope.** Errors use `response.WriteError` /
  `response.WriteDecodeError` (`{"error": "..."}`) instead of `http.Error` plain
  text. The frontend only `console.error`s error bodies, so this is a safe
  house-style alignment.
- **FR-F6 — support tunnels removed** (owner decision). No `/tunnel-mapping`
  endpoint, no tunnel file, no `supportTunnel` on `Customer`, no `SupportTunnel`
  CSV column (legacy 9-column imports tolerated, §3.2). Frontend edits in
  `index.js`: remove the `/tunnel-mapping` fetch and `tunnelMapping` state, the
  Support Tunnel form group, `populateSupportTunnelOptions`, and the
  `supportTunnelSelect` change listener; remove the `supportTunnel` entry from
  `tooltips.json`. No other tunnel references may remain anywhere in the module
  (`grep -ri tunnel` clean across `internal/timetracker`, `web/timetracker`,
  config, and docs except this FRD).

### 3.4 Frontend (`web/timetracker/`)

Copied from `reference/pshelper/public/`, minus `data.json`, `tunnellist.txt`
(see §2), and minus `index2.html` (dead standalone layout experiment, referenced by
nothing). Keep: `index.html`, `index.js`, `index.css`, `TimeSelector.js`,
`TimeSelector.css`, `tooltips.json`, `marked.min.js`, `fontawesome.min.css`,
`webfonts/`. The **only** JS edits are the FR-F6 tunnel removals — everything else
ships verbatim (FR-F1 exists precisely so the add-customer path needs no frontend
change).

Known reference quirks preserved (not bugs to fix in this effort):

- Mutation responses are full `Data`, but the frontend reads them as a customer
  (`updatedCustomer[target]`) — e.g. a Jira edit shows a stale link text until the
  next interaction/reload. Cosmetic; reference behavior.
- Hard-coded external URLs in `index.js` (Slack team `T12DX4MJR`, Cloudian
  loginsight/Jira URLs) stay as-is.
- Time selector state is ephemeral (never persisted server-side); the commented-out
  `timeUsage` field in the reference stays out.

### 3.5 Module wiring

`build.go`: construct store from `cfg.DataFile`, register the six API routes on a
fresh mux, mount `static.NewHandler(cfg.StaticDir)` at `/`. No goroutines → no
`io.Closer`. If `DataFile` is missing at boot, initialize it empty-but-valid
(`{"customers": []}` semantics) rather than failing the module — matches grocery's
fresh-boot behavior.

## 4. Non-functional requirements

- **NFR-1** `go test -race ./...` green; new tests follow the house pattern
  (store tests: load/save/atomicity/concurrent access; handler tests: every
  endpoint, response shapes byte-compatible with §3.2, the FR-F1..F4 cases).
- **NFR-2** No panics reachable from request input (FR-F2/F4 close the two known).
- **NFR-3** Default 1 MiB body limit is sufficient (CSV of ~hundreds of customers
  is KBs); no `limitFor` override.
- **NFR-4** `go vet` clean; no new lint debt.
- **NFR-5** Docs touched: README module list, `docs/USERGUIDE.md` (short section),
  this FRD. `session.md` gets a Phase entry when the work lands.

## 5. Known limitations (accepted, carried over)

- Single shared dataset; no multi-user isolation, no revision/ETag, no SSE — last
  write wins on concurrent edits (now safely serialized, but still last-write-wins
  semantically).
- Index-based addressing (`/update`, `/delete` by array position against a
  name-sorted UI list) relies on the frontend reloading after list-shape changes;
  two browsers editing simultaneously can target the wrong row. Reference
  behavior; out of scope.
- `companyName`, `projectName`, `version` are data-file-driven and have no write
  endpoint (author is the only mutable `Data` scalar).

## 6. Acceptance criteria

1. `make build` / `go build ./...` succeeds; binary boots with `timetracker` routed
   and all previously existing modules unaffected (`go test -race ./...` all green).
2. Via `local-test` at `http://timetracker.test:<port>/`: customer list renders
   sorted; selecting a customer shows detail panel + time selector; edit → Submit
   persists across restart (field lands in `data/timetracker.json`).
3. Add Customer works end-to-end (FR-F1) — new row appears after reload.
4. Delete Customer (modal YES) removes the row; invalid index via curl → 400, not
   a crash.
5. No tunnel remnants: the detail panel has no Support Tunnel row, no request to
   `/tunnel-mapping` is made, and `grep -ri tunnel` over `internal/timetracker`,
   `web/timetracker`, and config files comes back empty.
6. CSV export downloads the 8-column file; re-importing it round-trips the data;
   a legacy 9-column pshelper export imports with `SupportTunnel` dropped;
   importing a malformed CSV → 400 and the dataset is unchanged.
7. Report composer renders markdown preview with date/author/total-time framing and
   copies to clipboard (manual check).
8. With `auth.modules.timetracker` set in a test config, the login gate fronts the
   module with no module-code change.

---

*Next step: `/oh-my-claudecode:ralplan` turns this FRD into an executable sliced
plan; ralph executes. Fable runs opus-tier roles; Sonnet builds. Never Opus 5.*
