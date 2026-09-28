# PLAN — timetracker module integration

Status: APPROVED (owner, 2026-09-15)
Date: 2026-09-12
Source of truth: `docs/timetracker-FRD.md` (requirements contract), `docs/adding-a-module.md` (integration checklist)
Reference (READ-ONLY, never modified): `reference/pshelper/` (`serve.go` + `public/`)
Architectural reference: `internal/grocery/` (single-JSON-file store pattern)
Executor: ralph (sequential autonomous Sonnet builders); each slice below is independently verifiable and leaves the tree green.

---

## RALPLAN-DR Summary

### Principles

1. **Feature parity, minimal change.** Port the reference app; do not redesign it. Known quirks in FRD §3.4 are preserved, not fixed.
2. **Platform contract fidelity.** Follow `docs/adding-a-module.md` step-for-step. Zero platform code changes beyond the sanctioned touchpoints (config struct/expander, one `buildModule` case, one `knownModules` entry, config file routing entries).
3. **Mutable state never lives in `web/`; `reference/` is never touched.** `data.json` → `cfg.DataFile`; `tunnellist.txt` is dead; `tooltips.json` stays static content.
4. **The server absorbs the fixes so the frontend ships verbatim.** FR-F1 (newCustomer) is a server-side fix precisely so `index.js` needs no edits beyond FR-F6 tunnel removal.
5. **No new Go dependencies, no goroutines, no `io.Closer`, no broker.** Stdlib only (`encoding/json`, `encoding/csv`, `mime/multipart` via `net/http`); auth is config-only (IR-8).

### Decision Drivers (top 3)

1. **The FRD is a contract**: API shapes byte-compatible with §3.2, required fixes FR-F1..FR-F6 exactly, complete tunnel removal (`grep -ri tunnel` clean).
2. **Autonomous Sonnet execution**: slices must be dependency-ordered, small, with explicit file lists, concrete verification commands, and testable acceptance criteria — no judgment calls left to builders.
3. **Safety fixes without observable drift**: concurrency (FR-F3), bounds (FR-F2), and CSV atomicity (FR-F4) must land without changing anything the copied frontend can observe.

### Viable Options

**Option A — Minimal literal transplant.** Port `serve.go` handlers nearly verbatim into one file: per-request `loadData()`/`saveData()`, fixes patched inline, no store type.

- Pros: smallest textual diff from reference; fastest to write; easy to diff-audit against `serve.go`.
- Cons: violates FR-F3 as specified (FRD mandates "grocery-style single-file store: `sync.RWMutex` + temp-file + rename"); violates IR-6 house file split (`model.go`/`store.go`/`handler.go`/`build.go`); per-request disk IO; store-level tests impossible without HTTP scaffolding; plain-text errors would need rework anyway (FR-F5).

**Option B — Port with platform store (grocery-style). CHOSEN.** House file split; a `Store` owning `sync.RWMutex`-guarded in-memory `Data`, atomic tmp+rename saves; handlers thin, using `platform/response`; static via `platform/static`.

- Pros: directly satisfies FR-F3, IR-6, FR-F5; store logic unit-testable independent of HTTP; matches every other module the maintainers read.
- Cons: more new code than a literal transplant; the builder must consciously preserve reference response shapes (mitigated by the handler test suite in Slice 3).
- Rationale: the FRD *names* the grocery pattern as the target (FR-F3, IR-6). Option A cannot meet the FRD without converging on Option B anyway.

**Sub-decision — where the FR-F1 fix lives.** (a) Server-side: `/update` accepts `field: "newCustomer"` and appends the decoded `Customer` — CHOSEN, mandated by FR-F1 ("fix on the server so the copied frontend works untouched"). (b) Frontend-side: edit `index.js` to call `/create-customer` — REJECTED: violates the "only FR-F6 edits to JS" rule and widens the verbatim-copy audit surface. `/create-customer` is still implemented for contract parity (§3.2).

**Sub-decision — CSV import validation.** Parse the entire multipart CSV into a `[]Customer` in the handler first; only on full success call `store.ReplaceCustomers` (FR-F4). Per-row field counts: 8 columns = current format, 9 columns = legacy pshelper export (drop index 6), anything else → 400 with existing data untouched. (FR-F4's literal "≠9 fields → 400" predates the FR-F6 column removal; §3.2 is authoritative: {8, 9} accepted. Recorded under Open Questions.) Edge semantics, decided here so no builder improvises: a zero-byte upload or a failed header read → 400 (reference parity); a header-only file with zero data rows → **400, data untouched** — FR-F4's "must not destroy data" framing wins over reference parity (which would silently replace the dataset with an empty list). Recorded under Open Questions.

### The parity rule (settles every quirk question)

The parity line is **frontend observability**: anything the shipped `index.js` parses and acts on stays byte-compatible with the reference — the sorted `GET /data` shape, full-`Data` mutation response bodies, the CSV header row and column order. Anything the shipped frontend *cannot* observe adopts house style — JSON error envelopes (FR-F5), 405s on wrong methods, 400 on non-string `/update` values, 400 on CSV edge cases. This rule, not taste, decides current and future quirk questions. Corollary: the store's `Raw()` (insertion-order mutation bodies) is a diff-auditing nicety rather than a fidelity obligation — optional in principle, but **mandated in this plan** (Slice 2 specifies it; builders implement it as written). The frontend reads mutation responses as `updatedCustomer[target]` and never observes customer ordering in them.

---

## ADR

- **Decision:** Implement `internal/timetracker` as a grocery-style module — `model.go` (verbatim structs minus `supportTunnel`), `store.go` (`sync.RWMutex`, in-memory `Data`, atomic tmp+rename persistence to `cfg.DataFile`), `handler.go` (six API routes, `platform/response` envelopes, method-tagged mux patterns with 405 fallbacks), `build.go` (store + routes + `static.NewHandler(cfg.StaticDir)` at `/`; no goroutines, no `io.Closer`). Frontend copied from `reference/pshelper/public/` with only FR-F6 edits; `data.json`, `tunnellist.txt`, `index2.html` excluded. Config/wiring per the meld checklist (IR-1..IR-4). Support tunnels removed entirely (FR-F6).
- **Drivers:** FRD contract (shapes, FR-F1..F6); autonomous sliced execution; safety fixes (race, panic, data-wipe) without frontend-observable drift; platform invariant that module N+1 costs only config + one Build case.
- **Alternatives considered:** (1) Literal transplant with per-request file IO — rejected, fails FR-F3/IR-6/FR-F5. (2) Fixing Add-Customer in the frontend instead of FR-F1 server-side — rejected, FRD forbids JS edits beyond FR-F6. (3) Keeping tunnels behind a config flag — rejected, explicit owner decision 2026-09-12 to drop entirely. (4) Adding SSE/revision counters while in there — rejected, out of scope (§5: last-write-wins accepted).
- **Why chosen:** It is the only option that satisfies every FR verbatim, and it is the pattern the codebase already teaches (grocery/menuserver), minimizing review cost and drift.
- **Consequences:** Mutation semantics remain index-based and last-write-wins (accepted, §5). Production migration = copy old `public/data.json` to the configured `DataFile`; legacy `supportTunnel` values are ignored on load and dropped on first save. Mutation responses return full `Data` in file (insertion) order — only `GET /data` and `/export-csv` sort — preserving reference behavior. The stale-link cosmetic quirk (§3.4) ships as-is.
- **Follow-ups (out of scope):** optional SSE/revision for multi-tab freshness; ID-based addressing instead of indices; production `auth.modules.timetracker` entry (operator's call, IR-8); fixing the stale-link quirk upstream someday.

---

## Constraints (binding on every slice)

- `reference/pshelper/` is READ-ONLY **and stays untracked in git** (it contains real customer data — see Slice 0 and Open Question 10). The read-only invariant is protected by a checksum manifest, not git: every slice ends by running the manifest verify command **REFCHECK** defined in Slice 0. Adding `reference/` to `.gitignore` is **FORBIDDEN** — do not "fix" untracked-dir noise that way. **NEVER run `git clean` in any variant (`-fd`, `-fdx`, `-ffd`)** — `reference/` is the untracked sole working copy and `git clean` would delete it. `.omc/` is likewise left untracked as-is.
- **Commit discipline:** EVERY slice (0–6) makes a git commit of exactly that slice's file list. Each slice STARTS from a clean tree; run Verify, then commit; after the commit, `git status --porcelain | grep -v '^?? '` must be empty (Slice 0 makes two commits). Do not assume the executor harness auto-commits.
- No new entries in `go.mod`; Go toolchain unchanged.
- Frontend files are byte-identical to reference except `index.js` and `tooltips.json` (FR-F6 edits only).
- `web/timetracker/` never contains `data.json`, `tunnellist.txt`, or `index2.html`.
- Errors via `response.WriteError` / `response.WriteDecodeError` / `response.WriteJSON` only.
- Module has no goroutines and does not implement `io.Closer`.
- Default 1 MiB body limit; **no** `limitFor` change (NFR-3).
- Every slice ends with `go build ./... && go vet ./... && go test -race ./...` green.

---

## Slices

### Slice 0 — Pre-flight: baseline commit + reference checksum manifest (required first action)

**Context (verified against live git state):** at the start of the run, `reference/`, `docs/timetracker-FRD.md`, and `docs/PLAN-timetracker.md` are UNTRACKED (`HEAD == main == de232eb`). An untracked `reference/` gives the read-only invariant zero git protection, and no diff baseline exists. This slice fixes both — **without** committing `reference/`, because `reference/pshelper/public/data.json` and `tunnellist.txt` contain real customer data (customer names, Slack channel IDs, bucket names) that must not enter git history on a GitHub-hosted repo (Open Question 10).

**Files:** `docs/timetracker-FRD.md` (commit as-is), `docs/PLAN-timetracker.md` (commit as-is), `docs/reference-pshelper.sha256` (new).

**Work:**
1. Commit `docs/timetracker-FRD.md` and `docs/PLAN-timetracker.md` (they contain no customer data).
2. `reference/` stays UNTRACKED. Generate the checksum manifest over every file in `reference/pshelper/` (excluding `.git/` — `reference/pshelper/` contains a nested `.git`, verified, so this exclusion is mandatory, not cosmetic — and excluding `.DS_Store`):
   ```
   (cd reference/pshelper && find . -type f ! -path './.git/*' ! -name .DS_Store -print0 | sort -z | xargs -0 shasum -a 256) > docs/reference-pshelper.sha256
   ```
3. Define the per-slice verify command — referred to as **REFCHECK** throughout this plan. The `-c` check catches modification and deletion of listed files but is blind to ADDITIONS, so REFCHECK also asserts count equality over the same file population as the generation command:
   ```
   (cd reference/pshelper && shasum -a 256 --quiet -c ../../docs/reference-pshelper.sha256) \
     && test "$(cd reference/pshelper && find . -type f ! -path './.git/*' ! -name .DS_Store | wc -l | tr -d ' ')" = "$(grep -c . docs/reference-pshelper.sha256)" \
     && echo reference-ok
   ```
4. Commit the manifest. This commit is the **Slice 0 baseline** for the Slice 6 diff gate — record its hash (e.g. `git rev-parse --short HEAD` noted in the commit message or session notes).
5. **FORBIDDEN:** do NOT add `reference/` (or anything) to `.gitignore` to silence untracked-dir noise — that would be a plausible "fix" and it is explicitly prohibited. **NEVER run `git clean` in any variant (`-fd`, `-fdx`, `-ffd`)** — `reference/` is the untracked sole working copy and would be deleted. `.omc/` also stays untracked as-is. Safety note: the nested `reference/pshelper/.git` means git refuses single-force cleans of that directory, and an accidental `git add reference/` would produce only a gitlink (submodule pointer), never the file contents — both strengthen the untracked-plus-manifest story, but neither replaces the prohibitions.

**Verify:**
```
git log --oneline -2                          # two new commits (docs, manifest) on top of de232eb
git ls-files reference/ | wc -l               # 0 — reference/ untracked
git ls-files docs/reference-pshelper.sha256   # tracked
REFCHECK                                      # (the command from step 3) → reference-ok
git status --porcelain | grep -v '^?? ' ; test $? -eq 1 && echo tree-clean
test "$(cd reference/pshelper && find . -type f ! -path './.git/*' ! -name .DS_Store | wc -l | tr -d ' ')" = "$(grep -c . docs/reference-pshelper.sha256)" && echo count-ok
```

**Acceptance:** FRD + plan + manifest committed; `reference/` untracked and manifest-verifiable; `.gitignore` untouched; REFCHECK passes.

---

### Slice 1 — Platform config: `TimetrackerConfig` (IR-1)

**Files:** `internal/platform/config/config.go`, `internal/platform/config/config_test.go`

**Work:**
1. Add to `config.go`, following the `GroceryConfig`/`MenuserverConfig` pattern:
   ```go
   // TimetrackerConfig holds configuration specific to the timetracker module.
   type TimetrackerConfig struct {
       StaticDir string `json:"static_dir"` // e.g. ./web/timetracker
       DataFile  string `json:"data_file"`  // e.g. ./data/timetracker.json
   }
   ```
   No `SSEMaxSubscribers` field (no broker), no `applyServerDefaults` change.
2. Add `Timetracker TimetrackerConfig \`json:"timetracker"\`` to `Config` (alongside `Grocery`, `Menuserver`, …).
3. Add `expandTimetrackerPaths(t *TimetrackerConfig) error` expanding `StaticDir` and `DataFile` via `config.ExpandPath`, modeled on `expandGroceryPaths`; call it from `Load` alongside the existing expander calls.
4. Defaults in `DefaultConfig()`: `StaticDir: "./web/timetracker"`, `DataFile: "./data/timetracker.json"`.
5. `config_test.go`: extend in house style — defaults present in `DefaultConfig()`; a config file with `~`-prefixed timetracker paths gets them expanded by `Load`; a config file omitting the section still yields the defaults.

**Verify:**
```
go build ./... && go vet ./... && go test -race ./...
go test -race ./internal/platform/config/ -run Timetracker -v
REFCHECK   # manifest verify, defined in Slice 0 → reference-ok
```
Commit the slice (config.go + config_test.go).

**Acceptance:** `TimetrackerConfig` exists with exactly `static_dir`/`data_file`; expander wired into `Load`; defaults boot-clean; all tests green. No other file touched.

---

### Slice 2 — Module core: `model.go` + `store.go` + `store_test.go`

**Files (new):** `internal/timetracker/model.go`, `internal/timetracker/store.go`, `internal/timetracker/store_test.go`

**Work:**
1. `model.go` — structs ported from `reference/pshelper/serve.go` with `SupportTunnel` removed (FR-F6). JSON tags are the API contract, verbatim:
   ```go
   type Customer struct {
       CustomerName   string `json:"customerName"`
       SlackChannel   string `json:"slackChannel"`
       SlackChannelId string `json:"slackChannelId"`
       InsightUrl     string `json:"insightUrl"`
       WorkLoadType   string `json:"workLoadType"`
       SfdcUrl        string `json:"sfdcUrl"`
       CumulusBucket  string `json:"cumulusBucket"`
       Jira           string `json:"jira"`
   }
   type Data struct {
       CompanyName string     `json:"companyName"`
       ProjectName string     `json:"projectName"`
       Author      string     `json:"author"`
       Version     string     `json:"version"`
       Customers   []Customer `json:"customers"`
   }
   ```
   Sentinel errors in the package: `ErrInvalidIndex`, `ErrInvalidField` (grocery-style sentinels so the handler maps them to 400).
2. `store.go` — grocery-pattern single-file store (FR-F3):
   - `type Store struct { mu sync.RWMutex; data Data; filePath string }`
   - `New(filePath string) (*Store, error)`: load the file if present; on `os.IsNotExist`, start empty-but-valid (`Customers: []Customer{}`) without failing (§3.5 fresh-boot). Unknown JSON fields (legacy `supportTunnel`) are ignored by decoding into the new structs.
   - `Snapshot() Data`: deep copy; customers sorted case-insensitively by `CustomerName` (`strings.ToLower` comparison, matching reference). Used by `GET /data` and CSV export.
   - `Raw() Data`: deep copy in file (insertion) order. Used as the mutation-response body — the reference returns the *unsorted* loaded `Data` from `/update`, `/delete`, `/create-customer`, `/import-csv`; only `GET /data` and export sort. Per the parity rule, this is a diff-auditing nicety, not a fidelity obligation: the frontend never observes customer ordering in mutation bodies.
   - Mutators, each `mu.Lock()`, one `save()` on success, returning the post-mutation `Raw()`-equivalent copy:
     - `SetAuthor(value string) (Data, error)`
     - `UpdateField(index int, field, value string) (Data, error)` — field switch over `customerName`, `slackChannel`, `insightUrl`, `workLoadType`, `sfdcUrl`, `cumulusBucket`, `jira` (NOT `slackChannelId`, NOT `supportTunnel`); unknown field → `ErrInvalidField`; `index < 0 || index >= len` → `ErrInvalidIndex` (FR-F2).
     - `AppendCustomer(c Customer) (Data, error)` — FR-F1 and `/create-customer` both land here.
     - `DeleteCustomer(index int) (Data, error)` — bounds-checked → `ErrInvalidIndex`.
     - `ReplaceCustomers(customers []Customer) (Data, error)` — used by CSV import after full validation (FR-F4).
   - `save()` (lock held): `os.MkdirAll(filepath.Dir(filePath), 0750)`, marshal, write `filePath + ".tmp"` with 0600, `os.Rename` (FR-F3 atomicity). File keeps insertion order (never sorted on disk).
3. `store_test.go` — cases enumerated in the Test Plan below (store section), using `t.TempDir()`.

**Verify:**
```
go build ./... && go vet ./... && go test -race ./internal/timetracker/ -v
go test -race ./...
REFCHECK   # manifest verify (Slice 0)
```
Commit the slice (model.go, store.go, store_test.go).

**Acceptance:** package compiles standalone; every store test in the Test Plan passes under `-race`; no `supportTunnel` identifier in non-test code (`grep -ril tunnel internal/timetracker/ --include='*.go' --exclude='*_test.go'` → empty); no goroutines in non-test code. **`_test.go` files are exempt from the tunnel and goroutine greps by design** — the Test Plan mandates tunnel-string fixtures (`TestLoadIgnoresLegacySupportTunnel`, `TestImportCSV9ColumnLegacy`, `TestNoTunnelMappingEndpoint`) and concurrent `go func` tests (`TestConcurrentReadersAndWriters`); see Open Questions.

---

### Slice 3 — HTTP layer: `handler.go` + `build.go` + `handler_test.go`

**Files (new):** `internal/timetracker/handler.go`, `internal/timetracker/build.go`, `internal/timetracker/handler_test.go`

**Work:**
1. `handler.go` — `type Handler struct { store *Store }`, `NewHandler(s *Store) *Handler`, `Register(mux *http.ServeMux)` using grocery's method-tagged pattern + bare 405 fallback:
   - `GET /data` → `response.WriteJSON(w, 200, h.store.Snapshot())` (sorted).
   - `POST /update` → decode `{Index int; Field string; Value json.RawMessage}` (decode failure → `response.WriteDecodeError`). Dispatch order (matches FRD §3.2 literally — author check first):
     1. `Index == -1` (any field): unmarshal `Value` into `string` (failure → 400); `SetAuthor`; return full `Data`. (Corner case `{index:-1, field:"newCustomer"}` therefore takes the author path; the shipped frontend never sends it — recorded under Open Questions.)
     2. `Field == "newCustomer"` (FR-F1): unmarshal `Value` into `Customer` (failure → 400); `AppendCustomer`; the request's `Index` is otherwise ignored (frontend sends `len(customers)`). Return full `Data`, 200.
     3. Otherwise: unmarshal `Value` into `string` (failure → 400); `UpdateField`; map `ErrInvalidIndex`/`ErrInvalidField` → `response.WriteError(w, 400, …)` (FR-F2, FR-F5); return full `Data`.
     - Note: reference silently ignored non-string values (200, no-op). We answer 400 instead — the frontend only ever sends strings/objects, so this is unobservable; recorded under Open Questions.
   - `POST /delete` → decode `{Index int}`; `DeleteCustomer`; `ErrInvalidIndex` → 400; return full `Data`.
   - `POST /create-customer` → no body read; `AppendCustomer(Customer{CustomerName: "New Customer"})`; return full `Data`. (Contract parity, §3.2 — nothing in the shipped frontend calls it.)
   - `GET /export-csv` → `Content-Type: text/csv`, `Content-Disposition: attachment; filename=customers.csv`; `csv.Writer`; header row exactly `CustomerName,SlackChannel,SlackChannelId,InsightUrl,WorkLoadType,SfdcUrl,CumulusBucket,Jira` (8 columns, reference order minus `SupportTunnel`); rows from `Snapshot()` (sorted).
   - `POST /import-csv` → `r.FormFile("file")` (failure → 400); `csv.NewReader` with `FieldsPerRecord = -1`; read all rows into memory first (FR-F4): skip row 0 (header); each subsequent row must have 8 fields (map positionally) or 9 fields (legacy — skip index 6); any other width → `response.WriteError(w, 400, …)` and **no store mutation**. Edge cases (decided, not builder's choice): zero-byte upload or failed header read → 400 (reference parity); header-only file with zero data rows → 400, data untouched (FR-F4 spirit; see the CSV sub-decision above). Only after every row parses call `ReplaceCustomers`; return full `Data`. Note `/import-csv` and `/create-customer` are **API-only** endpoints — the shipped frontend has no import UI (only export, `index.js:163`) and nothing calls `/create-customer`; both exist for contract parity and curl/tooling use.
   - Bare-path `methodNotAllowed` fallbacks for `/data`, `/update`, `/delete`, `/create-customer`, `/export-csv`, `/import-csv` → 405 with the JSON envelope (FR-F5; supersedes reference's plain-text `http.Error`).
   - No `/tunnel-mapping` registration of any kind (FR-F6).
2. `build.go`:
   ```go
   func Build(cfg config.TimetrackerConfig) (http.Handler, error) {
       if err := os.MkdirAll(filepath.Dir(cfg.DataFile), 0750); err != nil { return nil, err }
       s, err := New(cfg.DataFile)
       if err != nil { return nil, err }
       h := NewHandler(s)
       mux := http.NewServeMux()
       h.Register(mux)
       mux.Handle("/", static.NewHandler(cfg.StaticDir))
       return mux, nil
   }
   ```
   No goroutines, no `io.Closer`, no broker.
3. `handler_test.go` — cases enumerated in the Test Plan (handler section), via `httptest` against a `Build`-produced handler or a mux + `Register`, with `t.TempDir()` data files. For `TestStaticFallback` (and any test exercising the static mount), create a temp static dir containing a one-line `index.html` fixture (e.g. `<html><body>timetracker-test-index</body></html>`) written by the test into `t.TempDir()`, and assert the fallback response body contains that marker.

**Verify:**
```
go build ./... && go vet ./... && go test -race ./internal/timetracker/ -v
go test -race ./...
grep -rn "http.Error" internal/timetracker/ --include='*.go' --exclude='*_test.go'   # must be empty (FR-F5)
grep -ri tunnel internal/timetracker/ --include='*.go' --exclude='*_test.go'         # must be empty (FR-F6)
grep -rn "go func\|io.Closer" internal/timetracker/ --include='*.go' --exclude='*_test.go'  # must be empty
REFCHECK                                           # manifest verify (Slice 0)
```
(`_test.go` files MAY contain tunnel-string fixtures/test names and `go func` — mandated by the Test Plan; see Open Questions.)
Commit the slice (handler.go, build.go, handler_test.go).

**Acceptance:** all six endpoints answer with §3.2 shapes; FR-F1/F2/F4/F5 handler tests pass; 405s use the JSON envelope; grep checks empty; tree green under `-race`.

---

### Slice 4 — Frontend port: `web/timetracker/` (IR-5, §3.4, FR-F6)

**Files (new):** everything under `web/timetracker/`, copied from `reference/pshelper/public/`:
- Copy verbatim: `index.html`, `index.css`, `TimeSelector.js`, `TimeSelector.css`, `marked.min.js`, `fontawesome.min.css`, `webfonts/` (incl. `fa-solid-900.ttf`).
- Copy then edit (FR-F6 only): `index.js`, `tooltips.json`.
- Do NOT copy: `data.json`, `tunnellist.txt`, `index2.html`.

**FR-F6 edits, exhaustively (reference line numbers from `reference/pshelper/public/index.js`):**
1. Remove `supportTunnel: ''` from the new-customer object literal (~line 72).
2. Remove the `let tunnelMapping = {};` declaration (~line 118).
3. Remove the entire `fetch('/tunnel-mapping')…` block (~lines 127–131).
4. Remove the Support Tunnel form group markup: the entire `<div class="form-group">…</div>` block at reference `index.js` lines ~231–234 — the wrapper div, the `<label data-tooltip="supportTunnel">Support Tunnel:</label>` line, the `<select id="supportTunnelSelect" …>` line, and the closing `</div>`. Remove all four lines; the adjacent `cumulusBucket` form-group (starting ~line 235) is untouched.
5. Remove the `supportTunnelSelect` lookup, the `populateSupportTunnelOptions(...)` call and function definition, and the `change` listener (~lines 388–417).
6. `tooltips.json`: delete the `"supportTunnel"` entry (keep valid JSON — mind the trailing comma).

No other character in `index.js` changes (FR-F1 exists so Add Customer works server-side).

**Verify:**
```
# verbatim files are byte-identical:
for f in index.html index.css TimeSelector.js TimeSelector.css marked.min.js fontawesome.min.css; do
  cmp reference/pshelper/public/$f web/timetracker/$f || echo "DIFFERS: $f"; done
diff -r reference/pshelper/public/webfonts web/timetracker/webfonts
# exclusions:
test ! -e web/timetracker/data.json && test ! -e web/timetracker/tunnellist.txt && test ! -e web/timetracker/index2.html && echo exclusions-ok
# FR-F6 clean:
grep -ri tunnel web/timetracker/ ; test $? -eq 1 && echo tunnel-free
# tooltips still valid JSON:
python3 -m json.tool web/timetracker/tooltips.json > /dev/null && echo tooltips-valid
# edited files still reference no removed ids:
grep -n "supportTunnel\|tunnelMapping\|populateSupportTunnelOptions" web/timetracker/index.js ; test $? -eq 1 && echo js-clean
go build ./... && go test -race ./...
REFCHECK   # manifest verify (Slice 0) — the copy must not have disturbed reference/
```
Commit the slice (everything under web/timetracker/).

**Acceptance:** verbatim files byte-identical to reference; three exclusions absent; `grep -ri tunnel web/timetracker/` empty; `tooltips.json` parses; Go tree still green (no Go changes in this slice).

---

### Slice 5 — Platform wiring: `buildModule`, `knownModules`, config files (IR-2, IR-3, IR-4)

**Files:** `cmd/server/main.go`, `local-test/config.json`, `unified-webapp-example.json`

**Work:**
1. `cmd/server/main.go`:
   - Add `"timetracker"` to `knownModules` (line ~270): `[]string{"grocery", "todo", "slideshow", "menuserver", "obsidianoid", "multissh", "admin", "timetracker"}`.
   - Add to `buildModule`'s switch: `case "timetracker": return timetracker.Build(cfg.Timetracker)` with the `cmd184psu/unified-webapp/internal/timetracker` import.
   - NO `limitFor` change (NFR-3: default 1 MiB suffices).
2. `local-test/config.json`: add `"timetracker.test": "timetracker"` under `host_routing` (matching the `.test` convention) and a module section:
   ```json
   "timetracker": {
     "static_dir": "./web/timetracker",
     "data_file": "./local-test/data/timetracker.json"
   }
   ```
3. `unified-webapp-example.json`: add `"timetracker.cmdhome.net": "timetracker"` and `"timetracker-test.cmdhome.net": "timetracker"` under `host_routing`, plus:
   ```json
   "timetracker": {
     "static_dir": "./web/timetracker",
     "data_file": "./data/timetracker.json"
   }
   ```
4. `local-test/setup.sh`: append `timetracker.test` to the `HOSTNAMES=` list (`local-test/setup.sh:122`) so the `/etc/hosts` check covers it, and extend the test-credentials header comment (`setup.sh:7-13`) with a `timetracker.test: open, no login` line, matching the existing format. No other setup.sh changes.

**Verify:**
```
go build ./... && go vet ./... && go test -race ./...
go test -race ./cmd/... -v
python3 -m json.tool local-test/config.json > /dev/null && python3 -m json.tool unified-webapp-example.json > /dev/null && echo json-ok
grep -n timetracker cmd/server/main.go            # exactly the knownModules entry + buildModule case (+ import)
grep -rn timetracker local-test/config.json unified-webapp-example.json
grep -ri tunnel local-test/config.json unified-webapp-example.json local-test/setup.sh ; test $? -eq 1 && echo config-tunnel-free
```
**Mandatory smoke test** (the only pre-Slice-6 end-to-end proof of host routing + fresh boot — this is the AC-1/§3.5 evidence): make it idempotent, start the server in the background with `local-test/config.json`, then
```
rm -f local-test/data/timetracker.json    # idempotence: fresh-boot expectation survives re-runs
# (start server, then:)
curl -s -H 'Host: timetracker.test' http://localhost:8080/data
# expect: {"companyName":"","projectName":"","author":"","version":"","customers":[]}
```
then kill the server. Caveat: `local-test/config.json`'s auth section points at absolute paths in the sibling `/opt/unified-webapp` checkout — boot depends on that environment; a boot failure there is a pre-existing local-test concern, not a timetracker regression (fall back to a minimal config with only `host_routing` + `timetracker` sections if needed, and note it).

End of slice: `REFCHECK` (manifest verify, Slice 0), then commit (cmd/server/main.go, local-test/config.json, unified-webapp-example.json, local-test/setup.sh).

**Acceptance:** binary boots with `timetracker` routed and all pre-existing modules unaffected (`go test -race ./...` green, incl. `cmd/server` tests); smoke curl returns the empty-valid `Data` envelope; both config files parse and route the module; setup.sh hosts list and header updated. The claim that `auth.ValidatePolicy` accepts an `auth.modules.timetracker` entry (because the name is in `knownModules`) has no exercising command in this slice — it is verified by **manual check 8** in the Test Plan.

---

### Slice 6 — Docs + final acceptance sweep (NFR-4, NFR-5, §6)

**Files:** `README.md` (module list), `docs/USERGUIDE.md` (short timetracker section), `session.md` (Phase entry), no code changes.

**Work:**
1. `README.md`: add `timetracker` to the module list in house style (one line: what it is, host-routing name).
2. `docs/USERGUIDE.md`: short section — what the module does (customer list, per-customer deep links, 15-minute time selector, markdown report composer, CSV export/import), config keys (`timetracker.static_dir`, `timetracker.data_file`), migration note worded **without tunnel strings** (the docs grep gate below covers `docs/`): "copy the old standalone app's `public/data.json` to the configured data file; removed legacy per-customer fields are ignored on load and dropped on first save", auth note (add `auth.modules.timetracker` in production, IR-8).
3. `session.md`: append the Phase entry for this work per its existing format — same wording caution: no tunnel strings (describe the removal as "legacy remote-access feature removed per FRD FR-F6" or reference the FRD by section, not by the feature's name).
4. Final sweep (the FRD §6 gates that are scriptable):

**Verify:**
```
go build ./... && go vet ./... && go test -race ./...
# FR-F6 global gate. The FRD's literal exemption list is only docs/timetracker-FRD.md;
# this run adds two recorded exceptions (see Open Questions): docs/PLAN-timetracker.md
# (this plan must name what it removes) and *_test.go files (mandated fixtures/test names).
grep -ril tunnel internal/timetracker web/timetracker local-test/config.json unified-webapp-example.json local-test/setup.sh session.md README.md \
  | grep -v '_test\.go$' ; test $? -eq 1 && echo AC5-code-ok
grep -ril tunnel docs/ | grep -v -e 'docs/timetracker-FRD\.md' -e 'docs/PLAN-timetracker\.md' -e 'docs/reference-pshelper\.sha256' ; test $? -eq 1 && echo AC5-docs-ok
REFCHECK                              # manifest verify (Slice 0) — reference never modified
```
(The manifest is exempt from the docs tunnel sweep because it is a machine-generated listing of reference-tree file NAMES — its `./public/tunnellist.txt` line is inventory, not a tunnel-feature reference. See Open Question 6.)

Commit the slice (README.md, docs/USERGUIDE.md, session.md). **Then, AFTER the commit** (mirroring Slice 0's verify-after-commit pattern — running it pre-commit would omit this slice's own files from the diff):
```
git diff --stat <slice0-commit>..HEAD   # baseline = the Slice 0 manifest commit recorded there
git status --porcelain | grep -v '^?? ' ; test $? -eq 1 && echo tree-clean
```
The diff must contain **exactly** the union of the Slice 1–6 file lists: `internal/platform/config/config.go` + `config_test.go`; `internal/timetracker/{model,store,handler,build}.go` + the two test files; everything under `web/timetracker/`; `cmd/server/main.go`; `local-test/config.json`; `unified-webapp-example.json`; `local-test/setup.sh`; `README.md`; `docs/USERGUIDE.md`; `session.md`. Nothing else (the FRD, this plan, and the manifest are already in the Slice 0 baseline; `reference/` and `.omc/` are untracked and appear nowhere).

**Acceptance:** docs updated; every automated acceptance criterion in FRD §6 (1, 4-curl-part, 5, 6-automatable-part) demonstrably true; manual checklist below handed to the operator.

---

## Test Plan

All automated tests live in `internal/timetracker/store_test.go` and `internal/timetracker/handler_test.go`, run under `go test -race`. Shapes below are the FRD §3.2 contract.

### store_test.go (Slice 2)

| Test | Covers |
|---|---|
| `TestNewMissingFileStartsEmpty` — `New` on a nonexistent path succeeds; `Snapshot().Customers` is empty non-nil; first mutation creates the file | §3.5 fresh boot |
| `TestNewCorruptFileFails` — `New` on a present-but-invalid-JSON data file returns an error (the resulting per-host 503 half is dispatcher behavior, already covered there) | OQ 9 |
| `TestNewLoadsExistingFile` / `TestPersistAcrossReopen` — write, reopen, same data | §3.1 |
| `TestLoadIgnoresLegacySupportTunnel` — data file containing `"supportTunnel":"x"` loads cleanly; after first save the file contains no `supportTunnel` substring | §2 migration, FR-F6 |
| `TestSnapshotSortsCaseInsensitively` — {"zeta","Alpha","beta"} → Alpha, beta, zeta; on-disk file keeps insertion order after save | §3.2 GET /data |
| `TestSnapshotIsDeepCopy` — mutating the returned slice/structs does not affect a subsequent `Snapshot` | §3.2 |
| `TestSetAuthor` — persists; other fields untouched | §3.2 index==-1 |
| `TestUpdateFieldEveryField` — table-driven over the 7 updatable fields | §3.2 |
| `TestUpdateFieldRejectsSlackChannelIdAndUnknown` — `slackChannelId`, `supportTunnel`, `bogus` → `ErrInvalidField` | §3.2, FR-F6 |
| `TestUpdateFieldBounds` — index -1 (as field update path), -5, `len`, `len+10` → `ErrInvalidIndex`; store unchanged | FR-F2 |
| `TestAppendCustomer` — appended at end (insertion order), persisted | FR-F1 core, §3.2 |
| `TestDeleteCustomer` + `TestDeleteCustomerBounds` — removes the right row; -1/`len` → `ErrInvalidIndex` | §3.2, FR-F2 |
| `TestReplaceCustomers` — wholesale replacement, persisted | §3.2 import |
| `TestSaveIsAtomic` — after a save, `filePath+".tmp"` does not exist and the file parses as valid JSON | FR-F3 |
| `TestConcurrentReadersAndWriters` — N goroutines mixing `Snapshot`/`UpdateField`/`AppendCustomer`/`DeleteCustomer` (indices clamped); no race under `-race`, file valid afterwards | FR-F3 |

### handler_test.go (Slice 3)

| Test | Covers |
|---|---|
| `TestGetData` — 200, `application/json`, all five `Data` keys present, customers sorted case-insensitively | §3.2 |
| `TestUpdateAuthor` — `{"index":-1,"field":"anything","value":"Chris"}` → 200, full `Data`, author set | §3.2 |
| `TestUpdateEachField` — table-driven over 7 fields; response is the **full updated `Data`** (not a customer) | §3.2 |
| `TestUpdateNewCustomer` — `{"index":<len>,"field":"newCustomer","value":{…customer object…}}` → 200, customer appended, full `Data` returned; exact reproduction of the frontend's Add-Customer payload | **FR-F1** |
| `TestUpdateUnknownField` → 400, body `{"error":"..."}` | §3.2, FR-F5 |
| `TestUpdateIndexOutOfRange` — index 99 and -5 with a real field → 400, no panic, data unchanged | **FR-F2**, NFR-2 |
| `TestUpdateMalformedBody` — invalid JSON → 400 via `WriteDecodeError` envelope | FR-F5 |
| `TestDelete` — valid index removes row, returns full `Data` | §3.2 |
| `TestDeleteInvalidIndex` — -1, `len` → 400, not a crash | §3.2, FR-F2 |
| `TestCreateCustomer` — POST → appends `{customerName:"New Customer"}`, full `Data` | §3.2 |
| `TestExportCSV` — 200, `text/csv`, `Content-Disposition: attachment; filename=customers.csv`, header row exactly the 8 columns in order, rows sorted case-insensitively | §3.2, FR-F6 |
| `TestImportCSV8Columns` — multipart field `file`; replaces entire list; returns full `Data` | §3.2 |
| `TestImportCSV9ColumnLegacy` — 9-column pshelper export accepted, column 6 (`SupportTunnel`) dropped, other 8 mapped correctly | §3.2, **FR-F4/F6** |
| `TestImportCSVMalformedLeavesDataUntouched` — a file whose row 3 has 5 fields → 400; a follow-up `GET /data` returns the pre-import dataset byte-for-byte | **FR-F4** |
| `TestImportCSVHeaderOnly` — header row only, zero data rows → 400 AND follow-up `GET /data` returns the pre-import dataset | **FR-F4**, OQ 7 |
| `TestImportCSVEmptyUpload` — zero-byte `file` part → 400, data untouched | **FR-F4**, OQ 7 |
| `TestImportCSVRoundTrip` — seed the store with ≥1 customer first, then export and import the produced CSV → equal customer set (an empty dataset no longer round-trips: its export is header-only, which imports as 400 — deliberate, OQ 7) | §6.6 |
| `TestImportCSVMissingFileField` — multipart without `file` → 400 envelope | FR-F5 |
| `TestMethodNotAllowed` — GET `/update`, GET `/delete`, POST `/export-csv`, GET `/import-csv`, POST `/data`, etc. → 405 with `{"error":…}` envelope (POST `/data` → 405 is a recorded micro-deviation — reference `dataHandler` had no method check; unobservable, frontend only GETs) | FR-F5 |
| `TestNoTunnelMappingEndpoint` — GET `/tunnel-mapping` is not an API route (falls through to static SPA fallback; response is not a JSON mapping) | **FR-F6** |
| `TestStaticFallback` — GET `/` and GET `/nonexistent-page` serve the static handler's index fallback | IR-5 |

### Structural / grep gates (Slices 3–6)

- `grep -ri tunnel internal/timetracker --include='*.go' --exclude='*_test.go'` and `grep -ri tunnel web/timetracker local-test/config.json unified-webapp-example.json local-test/setup.sh` → empty; docs sweep per Slice 6 with recorded exceptions {`docs/timetracker-FRD.md`, `docs/PLAN-timetracker.md`, `docs/reference-pshelper.sha256`, `*_test.go`} (FR-F6, AC-5).
- `grep -rn "http.Error" internal/timetracker/ --include='*.go' --exclude='*_test.go'` → empty (FR-F5).
- `grep -rn "go func\|io.Closer" internal/timetracker/ --include='*.go' --exclude='*_test.go'` → empty (§3.5). `_test.go` files MAY contain tunnel-string fixtures/test names and goroutines — the Test Plan requires them.
- `REFCHECK` (reference checksum-manifest verify, defined in Slice 0) → `reference-ok`, every slice. `reference/` is untracked, so git gates cannot protect it; the manifest does.
- `cmp`/`diff -r` verbatim-copy checks (Slice 4).
- No `go.mod`/`go.sum` diff at the end (`git diff --stat go.mod go.sum` empty) (IR-7).

### Manual checks (browser, operator — not automated)

Via `local-test` at `http://timetracker.test:<port>/`:
1. Customer list renders sorted; selecting a customer shows the detail panel + 15-minute time selector (AC-2).
2. Edit a field → Submit → restart server → value persisted in `local-test/data/timetracker.json` (AC-2).
3. Add Customer end-to-end — new row appears after reload (AC-3, FR-F1).
4. Delete Customer via modal YES (AC-4).
5. Detail panel has no Support Tunnel row; network tab shows no `/tunnel-mapping` request (AC-5).
6. CSV export downloads the 8-column file via the UI button (AC-6). **Import checks are API-only** — the shipped frontend has no import UI (only export, `index.js:163`). Exercise via curl with the server running:
   `curl -F 'file=@customers.csv' -H 'Host: timetracker.test' http://localhost:8080/import-csv`
   Round-trip the exported file, then a legacy 9-column export, then a malformed file (expect 400, data unchanged), then a header-only file (`printf 'CustomerName,SlackChannel,SlackChannelId,InsightUrl,WorkLoadType,SfdcUrl,CumulusBucket,Jira\n' > header-only.csv` — expect 400, data unchanged, OQ 7). `/create-customer` is likewise API-only.
7. Report composer renders markdown preview (marked.js) with date/author/total-time framing; **clipboard copy** works (AC-7).
8. With `auth.modules.timetracker` set in a test config, the login gate fronts the module with zero module-code change (AC-8, IR-8).

---

## Open Questions (resolved by planner — flag to reviewers)

1. **FR-F4 wording vs §3.2**: FR-F4 says "rows with ≠9 fields → 400" but §3.2 (and FR-F6) accept 8-column current-format rows and 9-column legacy rows. Resolution: accepted widths are **{8, 9}**; anything else → 400 with data untouched. §3.2 is treated as authoritative (the "9" in FR-F4 predates the column removal). *Iteration 1: ENDORSED by both reviewers.*
2. **Non-string `value` for a string field on `/update`**: reference silently no-ops (200). Plan answers 400 instead — unobservable per the parity rule (frontend only sends strings/objects). *Iteration 1: ENDORSED by both reviewers.*
3. **`/update` dispatch corner case**: `index == -1` is checked before `field == "newCustomer"` (FRD §3.2 literal order), so `{index:-1, field:"newCustomer"}` takes the author path. The shipped frontend never sends that combination (it always sends `index = len(customers)` with `newCustomer`). For the newCustomer path itself, the request's index is otherwise ignored — bounds-checking it against pre-append length would only ever be `== len`, a special case with no value.
4. **`local-test` hosts entry**: resolved concretely — `timetracker.test` is appended to `HOSTNAMES=` at `local-test/setup.sh:122` and to the credentials header comment at `setup.sh:7-13` (Slice 5 step 4).
5. **Grep gates vs mandated tests (AC-5 deviation)**: FRD AC-5's literal `grep -ri tunnel` over `internal/timetracker` cannot be satisfied verbatim — the FRD's own NFR-1/§3.2 test obligations require tunnel-string fixtures and names (`TestLoadIgnoresLegacySupportTunnel`, `TestImportCSV9ColumnLegacy`, `TestNoTunnelMappingEndpoint`) and `go func` concurrency tests (`TestConcurrentReadersAndWriters`). Resolution: every code-side gate excludes `_test.go` (`--include='*.go' --exclude='*_test.go'`); `_test.go` files MAY contain tunnel strings and goroutines. Non-test code remains at zero occurrences.
6. **FR-F6 docs grep exemption list**: the FRD exempts only `docs/timetracker-FRD.md`. This run records three additional exceptions: `docs/PLAN-timetracker.md` (the plan must name what it removes), `*_test.go` (item 5), and `docs/reference-pshelper.sha256` — the Slice 0 manifest is a machine-generated listing of reference-tree file NAMES (its `./public/tunnellist.txt` line is inventory, not a tunnel-feature reference); exempting it is a smaller change than relocating the manifest out of `docs/`. USERGUIDE and session.md are deliberately worded without tunnel strings so they need no exemption.
7. **CSV import edge cases**: zero-byte upload or failed header read → 400 (reference parity). Header-only file (zero data rows) → **400, data untouched** — FR-F4's "must not destroy data" framing wins over reference parity, which would silently replace the dataset with an empty list. Deliberate, recorded deviation. Consequence, also deliberate: export/import is asymmetric for an empty dataset — an empty store exports a header-only CSV that will not re-import (400); there is no way to empty the dataset via import, only via repeated `/delete`. Enforced by `TestImportCSVHeaderOnly`, `TestImportCSVEmptyUpload`, and the seeded `TestImportCSVRoundTrip`.
8. **`POST /data` → 405**: reference `dataHandler` had no method check (a POST returned the data). Method-tagged mux patterns make it 405 here. Unobservable per the parity rule (frontend only GETs `/data`).
9. **Corrupt-but-present data file at boot**: `New` returns the parse error, `Build` fails, and the dispatcher serves that host a 503 with the reason (grocery parity; missing file ≠ corrupt file — missing initializes empty per §3.5). Chosen behavior: fail loud rather than silently starting empty over unreadable data. Enforced by `TestNewCorruptFileFails` (store side; the 503 half is dispatcher-covered).
10. **`reference/` deliberately NOT committed to git** (coordinator decision, iteration 2): `reference/pshelper/public/data.json` and `tunnellist.txt` contain real customer data (customer names, Slack channel IDs, bucket names, session owners); committing them would put that data permanently into git history on a GitHub-hosted repo. The reviewers' default alternative — commit `reference/` wholesale for git-based read-only protection — is rejected **pending owner sign-off**; the owner may opt in later (e.g. after scrubbing or with a private-history decision). Until then the read-only invariant is protected by the Slice 0 checksum manifest (`docs/reference-pshelper.sha256` + REFCHECK), `reference/` stays untracked, and adding it to `.gitignore` — or running any variant of `git clean` — is forbidden. Two recorded consequences and one boundary note: (a) **reproducibility cost** — fresh clones lack `reference/` and cannot run REFCHECK or re-audit port parity until the owner supplies the reference tree out-of-band; (b) **nested-git safety** — `reference/pshelper/.git` exists, so git refuses single-force cleans of that directory and an accidental `git add reference/` yields only a gitlink, never file contents; (c) **the privacy boundary is data files, not org identifiers** — the org identifiers embedded in the shipped frontend (Slack team `T12DX4MJR`, Cloudian loginsight/Jira URLs in `index.js`) ARE committed per FRD §3.4; only the customer data in `reference/pshelper/public/data.json` and `tunnellist.txt` stays out of history.

---

## Definition of Done

- All six slices complete; `go build ./... && go vet ./... && go test -race ./...` green.
- FRD §6 automated criteria (1, 4-curl, 5, 6) pass via the commands above; manual checklist delivered.
- `reference/pshelper/`: no manifest-listed file modified or deleted, and file count unchanged — REFCHECK (checksums + count equality) green at every slice; `reference/` still untracked; `.gitignore` untouched; no `git clean` ever run. No new dependencies; no `limitFor`/toolchain changes.
- One commit per slice (0–6) on top of the Slice 0 baseline; `git diff --stat <slice0-commit>..HEAD` matches the stated file set exactly.
- Docs updated (README, USERGUIDE, session.md Phase entry).

---

## Revision log — iteration 1

Architect: SOUND-WITH-CHANGES. Critic: ITERATE. All required and minor items accepted; nothing rejected.

**Accepted changes:**
1. (CRITICAL) Rescoped every code-side grep gate (tunnel, `http.Error`, `go func`/`io.Closer`) to `--include='*.go' --exclude='*_test.go'` in Slice 2 acceptance, Slice 3 Verify, the structural-gates table, and the Slice 6 sweep — the gates were unsatisfiable against the plan's own mandated tests. Stated explicitly that `_test.go` MAY contain tunnel fixtures/names and goroutines. Recorded as Open Question 5 (AC-5 deviation).
2. (MAJOR) Fixed the FR-F6 docs conflict three ways: USERGUIDE migration note reworded without tunnel strings; `docs/` added to the Slice 6 sweep with an explicit exception list {`timetracker-FRD.md`, `PLAN-timetracker.md`}; the misquoted FRD exemption corrected (FRD exempts only itself — this plan is a *recorded* additional exception, Open Question 6); same wording caution applied to session.md.
3. `/update` dispatch reordered: `index == -1` (author) checked before `field == "newCustomer"`, matching FRD §3.2 literally; the never-sent `{index:-1, field:"newCustomer"}` corner case recorded (Open Question 3).
4. CSV edge semantics decided and written down: zero-byte/failed-header → 400 (reference parity); header-only file → 400, data untouched (FR-F4 spirit over reference parity, per Critic's recommendation). Open Question 7; encoded in the Slice 3 handler spec and the CSV sub-decision.
5. Manual check 6 corrected: import is API-only (no import UI in the shipped frontend; only export at `index.js:163`); exact curl command given; `/import-csv` and `/create-customer` marked API-only in Slice 3 too.
6. Slice 6 diff gate given an explicit baseline: `git diff --stat main...HEAD`.
7. Slice 5: `auth.ValidatePolicy` acceptance claim explicitly tied to manual check 8 (no exercising command in-slice); setup.sh cited concretely (`HOSTNAMES=` at `local-test/setup.sh:122`, header comment `setup.sh:7-13`); curl smoke promoted from optional to **mandatory** (sole pre-Slice-6 end-to-end proof of routing + fresh boot; AC-1/§3.5 evidence).
8. Slice 3: `TestStaticFallback` fixture specified (test-written one-line `index.html` marker in `t.TempDir()`).
9. Open Questions added: 8 (`POST /data` → 405 micro-deviation), 9 (corrupt data file at boot → Build error → per-host 503, grocery parity); Slice 4 tunnel form-group removal pinned to reference `index.js` lines ~231–234 including the wrapper div.
10. Architect's parity rule encoded as a stated section ("The parity rule"): frontend-observable behavior stays byte-compatible; unobservable behavior adopts house style; `Raw()` reclassified as a diff-auditing nicety.

**Rejected feedback:** none.

**Endorsements carried forward unchanged:** non-string `/update` values → 400; CSV widths {8, 9-legacy, index 6 dropped}; slice ordering; platform-contract fidelity; FR-F1 server-side fix.

---

## Revision log — iteration 2

Architect: SOUND-WITH-CHANGES. Critic: ITERATE. Both confirmed all iteration-1 fixes resolved (grep gates and docs sweep empirically verified on this host). All items accepted, with the blocker resolved per an explicit coordinator decision that modifies the reviewers' default remedy.

**Accepted changes:**
1. (BLOCKER — git baseline) Added **Slice 0 (pre-flight)**: commit the FRD and this plan (no customer data); generate and commit a checksum manifest `docs/reference-pshelper.sha256` over `reference/pshelper/` (excl. `.git/`); define the **REFCHECK** verify command. `reference/` deliberately stays UNTRACKED — the reviewers' default "commit reference/ wholesale" is NOT adopted because `data.json`/`tunnellist.txt` contain real customer data that must not enter git history (coordinator decision; recorded as Open Question 10, owner may opt in later). Adding `reference/` to `.gitignore` is explicitly FORBIDDEN (named as a plausible builder improvisation); `.omc/` stays untracked as-is.
2. (BLOCKER cont.) Every `git status --porcelain reference/` gate (Constraints, Slices 1–4, 6, structural-gates table) replaced with REFCHECK; a REFCHECK gate added to Slice 5, which previously had none.
3. (BLOCKER cont.) Commit discipline stated in Constraints: every slice (0–6) ends with a git commit of exactly its file list; Verify runs against a clean tree; no assumption of harness auto-commits. Per-slice "Commit the slice (…)" lines added.
4. (BLOCKER cont.) Slice 6 diff gate rebased to `git diff --stat <slice0-commit>..HEAD` with the expected file set enumerated exactly (Slice 1–6 union; FRD/plan/manifest live in the Slice 0 baseline; `reference/` and `.omc/` untracked, appear nowhere) — the "exactly" claim is now true.
5. (MAJOR — Critic) CSV edge semantics now enforced: added `TestImportCSVHeaderOnly` (400 + `GET /data` returns pre-import dataset) and `TestImportCSVEmptyUpload` (400, data untouched) to the handler test table; manual check 6 extended with the header-only curl case including the printf one-liner.
6. (Architect) `TestImportCSVRoundTrip` now specifies seeding ≥1 customer before export; OQ 7 records the deliberate empty-dataset export/import asymmetry (empty store exports header-only CSV that will not re-import).
7. (Architect) Added store test `TestNewCorruptFileFails` covering OQ 9's decided behavior (503 half remains dispatcher-covered).
8. (Architect) Slice 5 smoke made idempotent: `rm -f local-test/data/timetracker.json` prepended.
9. (Critic) Slice 5 smoke caveat added: local-test auth config depends on absolute paths in the sibling `/opt/unified-webapp` checkout; a boot failure there is a pre-existing local-test concern, not a timetracker regression (minimal-config fallback named).
10. (Critic) Parity-rule/Slice-2 phrasing reconciled: `Raw()` is "optional in principle, mandated in this plan".
11. Definition of Done extended: REFCHECK green every slice, one commit per slice, diff-set match against the Slice 0 baseline.

**Rejected feedback:** the reviewers' default remedy for the blocker — committing `reference/` wholesale — rejected per coordinator decision (customer data in git history on a GitHub-hosted repo); replaced with the untracked-plus-manifest variant, recorded in OQ 10 pending owner sign-off.

---

## Revision log — iteration 3

Architect: SOUND-WITH-CHANGES. Critic: ITERATE. Both confirmed all iteration-2 changes genuinely resolved and the Slice 0/REFCHECK/commit-discipline architecture sound; the Critic re-simulated Slices 0–6 under the corrected ordering and found no further wall. Five-item convergence set, all accepted.

**Accepted changes:**
1. (CRITICAL — both, dry-run confirmed) The Slice 0 manifest necessarily lists `./public/tunnellist.txt`, so the Slice 6 docs tunnel sweep would deterministically fail. Added `-e 'docs/reference-pshelper\.sha256'` to that gate's exemptions, added the manifest to the structural-gates exception set, and recorded the rationale in Open Question 6: the manifest is a machine-generated listing of reference-tree file NAMES, not a tunnel-feature reference. The manifest stays in `docs/` (exemption is the smaller change than relocation).
2. (MAJOR — Critic; wording half flagged by Architect) Verify/commit ordering contradiction fixed both halves: (a) the Constraints clause now reads "each slice STARTS from a clean tree; run Verify, then commit; after the commit, `git status --porcelain | grep -v '^?? '` must be empty (Slice 0 makes two commits)"; (b) the Slice 6 `git diff --stat <slice0-commit>..HEAD` gate (plus the tree-clean check) now runs explicitly AFTER the Slice 6 commit, mirroring Slice 0's verify-after-commit pattern — pre-commit it would omit README.md/USERGUIDE.md/session.md and the "exactly" set could never match.
3. (MAJOR Architect / MINOR Critic — both empirically confirmed) REFCHECK was blind to file additions. The canonical REFCHECK now appends a count-equality clause (`find … ! -path './.git/*' ! -name .DS_Store | wc -l` vs `grep -c . docs/reference-pshelper.sha256`), and the Slice 0 generation command carries the identical `! -path './.git/*' ! -name .DS_Store` filters so both sides count the same population (the nested `reference/pshelper/.git` is verified present — the exclusion is mandatory). Slice 0's count check is now an executable `test … && echo count-ok` command instead of an eyeballed comment. Definition of Done softened to "no manifest-listed file modified or deleted; file count unchanged".
4. (MINOR — both) `git clean` prohibition added beside the `.gitignore` prohibition (Constraints and Slice 0 step 5): never run `git clean` in any variant (`-fd`, `-fdx`, `-ffd`) — `reference/` is the untracked sole working copy. Supporting note added (Slice 0 step 5 + OQ 10): the nested `.git` makes git refuse single-force cleans there, and `git add reference/` would yield only a gitlink — strengthening, not replacing, the prohibitions.
5. (MINOR — Architect) OQ 10 expanded with (a) the reproducibility cost: fresh clones lack `reference/` and cannot run REFCHECK or re-audit parity until the owner supplies the tree out-of-band; and (b) the privacy boundary: org identifiers in the shipped frontend (Slack team `T12DX4MJR`, Cloudian loginsight/Jira URLs) ARE committed per FRD §3.4 — only `data.json`/`tunnellist.txt` customer data stays out of history.

**Rejected feedback:** none.
