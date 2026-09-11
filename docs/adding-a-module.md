# Adding a module

This is the checklist for adding module N+1 to unified-webapp. Follow it and the new
module inherits the whole platform posture — static serving, SSE, JSON error shapes,
path confinement, origin checks, body limits, security headers, timeouts, and the auth
gate — with zero platform code changes.

## The contract

Every module is a function with this shape:

```go
func Build(cfg config.<Module>Config) (http.Handler, error)
```

For example, `internal/todo/build.go`:

```go
func Build(cfg config.TodoConfig) (http.Handler, error) {
	store, err := NewStore(cfg.DataDir)
	if err != nil {
		return nil, err
	}
	mbr := broker.NewMultiRoomBroker(cfg.SyncIntervalSeconds * 1000)
	mbr.SetMaxSubscribers(cfg.SSEMaxSubscribers)
	h := NewHandler(store, mbr, cfg)

	mux := http.NewServeMux()
	h.Register(mux)
	mux.Handle("/", static.NewHandler(cfg.StaticDir))

	return mux, nil
}
```

`Build` takes its own config section (not the whole `*config.Config`) and returns an
`http.Handler` that owns every route behind its host — an `*http.ServeMux` is the usual
choice. Return a non-nil error and `cmd/server/main.go` serves that hostname a 503 with
the reason instead of crashing the rest of the binary (`unavailableHandler` in
`cmd/server/main.go`). `admin` is the one sanctioned exception to this signature — see
**The admin exception**, below.

If `Build` starts a background goroutine (a ticker loop, a filesystem watcher), the
handler it returns must also implement `io.Closer`, with `Close` stopping everything
`Build` started. The dispatcher detects the interface and calls `Close` on shutdown;
this is what lets `cmd/server`'s tests run under a `goleak` gate. Slideshow (conductor
tick loop) and obsidianoid (per-vault fsnotify watchers) are the two existing examples.
A module with no background work returns its mux as-is.

## Step-by-step checklist

1. **Config struct + expander.** Add a `<Module>Config` struct to
   `internal/platform/config/config.go` (see `GroceryConfig`, `TodoConfig`, etc. for the
   pattern — `static_dir`, `data_dir`/`data_file`, and whatever else your module needs,
   each with a `json:"..."` tag). Add a field for it on `Config` and give it a
   `json:"..."` tag too (e.g. `Grocery GroceryConfig \`json:"grocery"\``). If any path
   fields need `~`/env expansion, write an `expand<Module>Paths` function (see
   `expandGroceryPaths`, `expandTodoPaths`) using `config.ExpandPath`, and call it from
   `Load` alongside the existing expander calls. Give it a sane zero-value default in
   `DefaultConfig` if a fresh config should still boot cleanly.

2. **`buildModule` case.** Add a `case "<module>":` to the switch in
   `cmd/server/main.go`'s `buildModule` function, calling your `Build(cfg.<Module>)`.

3. **`knownModules`.** Add the module's name to the `knownModules` slice in
   `cmd/server/main.go` — this is the universe `auth.FromConfig`/`auth.ValidatePolicy`
   check `auth.modules` entries against, so a module missing from this list can never be
   named there.

4. **`host_routing` entry.** In the config file, map a hostname to your module's name
   under `host_routing` (`Config.Routing`, tagged `json:"host_routing"`) — this is what
   actually turns the module on. Multiple hostnames may point at the same module name;
   `buildDispatcher` builds each module exactly once and shares the handler across every
   hostname that routes to it.

5. **Static assets**, if any, under `web/<module>/` — point your config's `static_dir`
   at it and serve it with `platform/static` (see below). No build-pipeline change is
   needed unless the frontend itself requires one.

That's the whole list for full security posture on a module with no login. Steps 1–4 are
required; step 5 only if the module serves its own static frontend.

## Platform packages to reach for

Don't reimplement any of these — they're shared for a reason, and duplicating them is
exactly the kind of drift this project consolidated away from:

- **`internal/platform/static`** — `static.NewHandler(dir string) *static.Handler`
  serves files from `dir` with an `index.html` SPA fallback, path-rooted so a request
  can't escape `dir`. Mount it at `"/"` on your mux.
- **`internal/platform/broker`** — `broker.NewBroker(retryMs int) *Broker` for a single
  SSE stream, or `broker.NewMultiRoomBroker(retryMs int)` for per-room streams. Both
  have `SetMaxSubscribers(n int)`, which caps concurrent subscribers and answers `503`
  to anyone over the cap before any SSE headers are written. Wire your module's slice of
  `Config.Server.SSEMaxSubscribers` through: `Load` already copies the effective cap
  (`0` → 64) down into each broker-owning module's config as an unexported-from-JSON
  `SSEMaxSubscribers int \`json:"-"\`` field (see `GroceryConfig`, `TodoConfig`,
  `SlideshowConfig`, `ObsidianoidConfig`) — add the same field to your config struct, add
  one line to `applyServerDefaults` in `config.go` to copy it down, and pass
  `cfg.SSEMaxSubscribers` to `SetMaxSubscribers`. `Broker.Notify()` sends a bare refresh
  signal; `Broker.Publish(data string)` / `ServeSSE(eventName string, snapshot func() string)`
  carry a payload and optionally snapshot current state to a newly-connected client.
- **`internal/platform/response`** — `response.WriteJSON(w, status, v)` and
  `response.WriteError(w, status, msg)` for the `{"error": "..."}` envelope every module
  uses; `response.WriteDecodeError(w, err)` maps a failed `json.Decoder.Decode` to `413`
  (body over a `middleware.BodyLimit`) or `400` (anything else malformed) — call it from
  every decode-error branch instead of writing your own status-code logic.
- **`internal/platform/fspath`** — `fspath.ValidName(name string) bool` checks a single
  path component is non-empty, not dot-prefixed, and has no `/` or `\`;
  `fspath.ConfineTo(root, rel string) (string, error)` joins `root` and `rel` and
  rejects the result if it would resolve outside `root`. Use these anywhere a request
  supplies a filename or relative path that reaches the filesystem.

## What the dispatcher gives you for free

`buildDispatcher` in `cmd/server/main.go` wraps every successfully-built module's
handler the same way, before your code ever sees a request:

```go
h = middleware.BodyLimit(limitFor(module, cfg), svc.Gate(module, hh))
```

...and the whole dispatcher is itself wrapped once, outside host dispatch:

```go
handler := middleware.Wrap(middleware.OriginCheck(cfg.Server.OriginCheck, dispatch))
```

That gets your module, with no code of its own:

- **Same-origin CORS posture** (`middleware.Wrap`, `internal/platform/middleware/cors.go`)
  — `Access-Control-Allow-Origin` reflected only for same-origin requests, never a
  wildcard; `X-Content-Type-Options: nosniff` on every response.
- **Cross-origin write rejection** (`middleware.OriginCheck`,
  `internal/platform/middleware/origincheck.go`) — any non-`GET`/`HEAD` request whose
  `Origin` doesn't match `Host` gets a `403` before your handler, or even your body, is
  ever read (`server.origin_check`, default `"enforce"`; `"log"`/`"off"` are the escape
  hatches).
- **A body-size ceiling** (`middleware.BodyLimit`, wired via `limitFor` in
  `cmd/server/main.go`) — a default 1 MiB per request unless your module needs a
  different figure (see how `multissh` and `admin` override it in `limitFor` and
  `AdminConfig.MaxBodyBytes`); an oversized body surfaces as an `*http.MaxBytesError`
  from your next `Body` read, which `response.WriteDecodeError` turns into `413`.
- **Server timeouts** (`newServer` in `cmd/server/main.go`) — `ReadHeaderTimeout` and
  `IdleTimeout` are set once on the shared `http.Server`; `ReadTimeout`/`WriteTimeout`
  are deliberately left at zero so long-lived SSE/WebSocket connections aren't killed
  mid-stream.
- **The auth gate** (`svc.Gate(module, hh)`, `internal/platform/auth/gate.go`) — always
  applied, even when your module has no `auth.modules` entry; an unprotected module
  passes straight through. See below for what protecting it costs.

None of this requires your `Build` function, or anything inside it, to know these
middlewares exist.

## Auth opt-in: one `auth.modules` entry

To put your module behind the login gate, add one entry to `auth.modules` in the config
naming the methods it accepts:

```json
"auth": {
  "modules": {
    "<module>": ["pin", "ldap"]
  }
}
```

No code change in the module is required — the gate, the login page, session cookies,
API keys, and every authenticator all live in `internal/platform/auth` and are applied
by `svc.Gate(module, hh)` in `buildDispatcher`, which every module already flows through
per the previous section. This is the design invariant this whole platform is built to
preserve (`security-plan.md`):

> adding module N+1 with full security posture must require only (a) a config section
> + `host_routing` entry, (b) the standard `Build(cfg) (http.Handler, error)` + one
> `buildModule` case, and (c) optionally one `auth.modules` entry naming its accepted
> methods. Everything else — static serving, broker cap, response helpers, path
> confinement, origin checks, body limits, headers, the auth gate, the login page — is
> inherited from `internal/platform/*` and the dispatcher with zero per-module code.

## The admin exception

`internal/admin` is built exactly per this checklist — it has a config section, a
`buildModule` case, a `host_routing` keyword (`admin`), and serves its SPA out of
`web/admin` via `platform/static` like any other module. But it knowingly deviates from
the pattern in three ways, and only these three (FR-M6, `security-FRD.md`):

1. **It is the only module that writes the config file.** Its live-apply feature
   (`internal/admin/apply.go`) edits `auth.*` on disk from the admin UI, which is why its
   `Build` signature is different (below) — every other module only ever reads config.
2. **It is the only module with an always-on auth method outside the assignment
   matrix.** The operator PIN (`admin_pin`) authenticates into `admin` whether or not
   `"admin_pin"` — or anything at all — appears in `auth.modules["admin"]`, so a live
   matrix edit can never lock the operator out.
3. **It is the only module that cannot exist unprotected.** Every other module with no
   `auth.modules` entry is wide open; `admin`, when routed, is always gated (`gate.go`:
   `protected := (module has an auth.modules entry) || module == "admin"`), regardless
   of whether the matrix names it.

The mechanism for (1) is the sanctioned signature deviation:

```go
func Build(cfg *config.Config, deps admin.Deps) (http.Handler, error)
```

where `Deps{Service *auth.Service, ConfigPath string, KnownModules []string, AdminRouted bool}`
carries the running auth service to swap a new policy into, the on-disk config path to
splice the auth section into, and the exact `knownModules`/`adminRouted` arguments boot
passed to `auth.ValidatePolicy` — so a live save is validated by the identical code path
and inputs boot used. `buildModule`'s `"admin"` case is the one place that builds a
module with extra arguments; no other module gets this shape, and nothing about `admin`
beyond these three points is special — it still goes through `platform/static`,
`platform/response`, and `svc.Gate` like everything else.
