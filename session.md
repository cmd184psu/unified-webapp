# Unified Webapp — Session Log

## Goal

Consolidate four Go web apps (grocery-list, todo-list, slideshow, menuserver) into one Go service. HAProxy handles hostname routing; Go reads `r.Host` to dispatch to the correct module. Grocery-list is the architectural reference. Migration is incremental.

Key decisions:
- Dispatch: Host header (`r.Host` → module)
- Todo storage: File-per-subject, per-subject `sync.RWMutex`, atomic revision — SSE uses `MultiRoomBroker` (Google Docs model: clients viewing the same list see each other's changes in real time)

Full plan: `/Users/cdelezenski/.claude/plans/you-are-helping-me-cozy-waterfall.md`

---

## Migration Phases

### Phase 0 — Platform Foundation ✅ DONE

**Goal:** Shared SSE infrastructure with full test coverage. No runnable binary.

Files created:
- `internal/platform/broker/broker.go` — SSE broker ported from grocery-list reference (package renamed)
- `internal/platform/broker/broker_test.go` — 9 tests (subscribe/unsubscribe, fan-out, non-blocking, ServeHTTP lifecycle)
- `internal/platform/broker/multirouter.go` — `MultiRoomBroker`: lazy per-room broker map for todo's multi-list SSE
- `internal/platform/broker/multirouter_test.go` — 8 tests (isolation, lazy creation, concurrent access, no-op on missing room)

Result: `go test ./internal/platform/broker/...` → 17/17 PASS

---

### Phase 1 — Grocery Module + Runnable Server ✅ DONE

**Goal:** `grocery.cmdhome.net` served live from the unified binary.

Files created:
- `internal/platform/config/config.go` + `config_test.go` — unified config loader, `ExpandPath`, `GroceryConfig`, `WriteDefault`
- `internal/platform/response/response.go` — `WriteJSON`, `WriteError`
- `internal/platform/middleware/cors.go` — `Wrap` (CORS + OPTIONS)
- `internal/grocery/model.go` — `Item`, `ItemState`, `NoGroup` constants
- `internal/grocery/store.go` — thread-safe single-file JSON store with atomic revision
- `internal/grocery/handler.go` — all HTTP handlers, imports `platform/broker` and `platform/response`
- `internal/grocery/build.go` — `Build(GroceryConfig) http.Handler`, `staticHandler` type (SPA fallback)
- `internal/grocery/store_test.go` + `handler_test.go` — ported from reference
- `cmd/server/main.go` — `Dispatcher` type (Host-header routing), `buildModule` switch, TLS support
- `web/grocery/` — copied from reference

Result: `go test ./...` → **75/75 PASS** across grocery, broker, config packages

Run: `go run ./cmd/server -config ~/.unified-webapp.json`

---

### Phase 2 — Todo Module ✅ DONE

**Goal:** `todolist.cmdhome.net` live with SSE multi-list sync.

Files created:
- `internal/todo/model.go` — `Subject`, `IndexItem`, `IndexFile` types
- `internal/todo/store.go` — per-file store, per-subject `sync.RWMutex` map, atomic writes (tmp+rename), `MoveFile` with deadlock-safe sorted lock acquisition
- `internal/todo/handler.go` — REST API matching jQuery frontend shapes + SSE via `MultiRoomBroker`
- `internal/todo/build.go` — `Build(TodoConfig) http.Handler`, `staticHandler`
- `internal/todo/store_test.go` + `handler_test.go` — 20 tests (concurrent access, persistence, path validation, response shapes)
- `web/todo/` — copied from `reference/gotodolist/static/`, `index.html` entry point created
- `web/todo/js/todo.js` — SSE snippet appended (~20 lines): `connectSSE(subject)`, `sseReload()`
- `web/todo/index.html` — `startTodo(params).then(connectSSE)` wired in
- `internal/platform/config/config.go` — `TodoConfig` added, path expansion hooked in
- `cmd/server/main.go` — `case "todo": todo.Build(cfg.Todo)` added
- Fixed pre-existing broker test race (`TestServeHTTPDeliversRefreshOnNotify` — missing `<-done` sync)

API contract (jQuery frontend preserved exactly):
- `GET /config/` → `{ext, defaultSubject, defaultItem, autosave}`
- `GET /items` → `[{age, timestamp, subject, entries:[...]}]`
- `GET /items/{subject}/index.json` → generated `{title, list:[{json, name, skip}]}`
- `GET /items/{subject}/{item}` → raw file content (creates empty if missing)
- `POST /items/{subject}/{item}` → saves body, notifies SSE room → `{"msg":"saved"}`
- `POST /items/{subject}/{item}/{newSubject}` → moves file → `{"msg":"moved"}`
- `GET /api/events?subject={subject}` → SSE stream via `MultiRoomBroker`

Result: `go test -race ./...` → all PASS

---

### Phase 3 — Slideshow Module ✅ DONE

**Goal:** `slideshow.cmdhome.net` live, path traversal fixed.

Files created:
- `internal/slideshow/store.go` — `Store`: scans `imageDir` for subject subdirectories; entries formatted as `"{subject}/{file}"`; `ImagePath` validates extension allowlist + `filepath.Rel` escape check
- `internal/slideshow/handler.go` — `GET /config`, `GET /config/`, `POST /config` (in-memory `defaultSubject`), `GET /items`, `GET /{prefix}/{subject}/{item}` (validated image serving)
- `internal/slideshow/build.go` — `Build(SlideshowConfig) http.Handler`
- `internal/slideshow/store_test.go` + `handler_test.go` — 16 tests
- `web/slideshow/` — copied from reference; `index.html` → `slideshow.html`
- `internal/platform/config/config.go` — `SlideshowConfig` added with path expansion
- `cmd/server/main.go` — `case "slideshow":` added

Path traversal fix: reference used `fmt.Sprintf(".%s", r.RequestURI)` (vulnerable).
New code: `validName` rejects dot-prefix/slashes; `ImagePath` uses `filepath.Rel` to confirm path stays inside `imageDir`; extension allowlist (jpg/jpeg/png/gif) rejects non-image requests.

Result: `go test -race ./...` → all PASS

---

### Phase 4 — Menuserver Module ✅ DONE

**Goal:** `menu.cmdhome.net` live.

Files created:
- `internal/menuserver/store.go` — `Store`: scans `dataDir` for subject subdirectories of `.json` menu files; `ReadMenu` with `filepath.Rel` escape check
- `internal/menuserver/handler.go` — `GET /config`, `GET /config/`, `GET /items`, `GET /menus/{subject}/{item}` (read-only; no POST routes)
- `internal/menuserver/build.go` — `Build(MenuserverConfig) http.Handler`
- `internal/menuserver/store_test.go` + `handler_test.go` — 14 tests
- `web/menuserver/` — copied from reference; `index.html` → `menuserver.html`
- `internal/platform/config/config.go` — `MenuserverConfig` added with path expansion
- `cmd/server/main.go` — `case "menuserver":` added

JS audit findings — `startMenuserver()` makes exactly three API calls:
1. `GET /config/` → `{showAllPages}`
2. `GET /items` → `[{subject, entries:["home/networking.json",...]}]`
3. `GET /menus/{subject}/{item}` → raw page JSON `{id, title, sites:[], notes}`

`SaveList`, `SelectNewFile`, and other todo.js remnants in menuserver.js are dead code — never called from `startMenuserver()`. No POST endpoints were implemented.

Result: `go test -race ./...` → all PASS

---

## Target Directory Structure

```
/opt/unified-webapp/
├── cmd/server/main.go
├── internal/
│   ├── platform/
│   │   ├── broker/          ✅ broker.go, multirouter.go (+ tests)
│   │   ├── response/        ✅ response.go
│   │   ├── middleware/      ✅ cors.go
│   │   └── config/          ✅ config.go (+ test)
│   ├── grocery/             ✅ model.go, store.go, handler.go, build.go (+ tests)
│   ├── todo/                ✅ model.go, store.go, handler.go, build.go (+ tests)
│   ├── slideshow/           ✅ store.go, handler.go, build.go (+ tests)
│   └── menuserver/          ✅ store.go, handler.go, build.go (+ tests)
└── web/
    ├── grocery/             ✅ copied from reference
    ├── todo/                ✅ copied from reference (+ SSE snippet)
    ├── slideshow/           ✅ copied from reference
    └── menuserver/          ✅ copied from reference
```

## Known Risks / Unknowns

1. **alfredo library** — used in gotodolist for `FileExistsEasy` and `ExpandTilde`; both trivially inlineable
2. **Todo data directory layout** — must confirm real deployment layout before writing store.go so existing data is readable without conversion
3. **Slideshow image layout** — need to know real directory structure
4. **jQuery API response shapes** — todo/slideshow/menuserver JS expects exact JSON; audit JS before writing handlers
5. **Menuserver dynamic endpoints** — most routes commented out; audit menuserver.js first
6. **HAProxy Host header** — confirm HAProxy forwards `Host` header unchanged (standard behavior)
