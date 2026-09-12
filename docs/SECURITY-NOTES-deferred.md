# Security notes — deferred to a later branch

**Status:** backlog only. Do NOT action any item here on the `grocerymod` branch.

These were observed while planning the Recipes tab. The user scoped that work to the
grocery module and explicitly deferred auth, CORS, and TLS to a separate future branch
that will also move to the latest Go and revisit `go.mod` dependencies. Recorded here so
the findings are not lost and are not re-derived from scratch.

Every claim below was checked against the tree on branch `grocerymod` (2026-09-10).
Line numbers are from that state.

---

## Findings

### S-1 — No authentication or authorization anywhere
`grep -rn "Authorization|BasicAuth|Bearer|session"` across `internal/` and `cmd/` returns
nothing outside tests. Every route in every module is unauthenticated and unauthorized.
The server binds all interfaces: `cmd/server/main.go:78` (`0.0.0.0:%d`, default port 8080
at `internal/platform/config/config.go:91`).

Anyone who can reach the port can read, mutate, and delete all data.

This is the root finding. S-2, S-3 and S-5 are only interesting because of it.

### S-2 — Permissive CORS on every module
`internal/platform/middleware/cors.go:8-10` sets `Access-Control-Allow-Origin: *` with
`GET, POST, PATCH, DELETE, OPTIONS`, and `cmd/server/main.go:79` wraps the whole dispatcher
in it.

Precise exposure: because the origin is the `*` wildcard, browsers will not attach
credentials, so this is not classic cookie-CSRF. The real consequence is that any web page
in any browser that can route to a reachable instance may drive the API *and read the
responses back*. With S-1 there is nothing else standing in the way.

### S-3 — No request body size limits
No `http.MaxBytesReader` anywhere in the repo. The grocery handler alone has seven
unbounded `json.NewDecoder(r.Body)` sites: `internal/grocery/handler.go:148, 173, 209,
244, 267, 286, 328`.

`handler.go:286` is the sharp one — `POST /api/sync` decodes a caller-supplied array into
memory and then persists it, so one request converts directly into heap and disk.

### S-4 — No server timeouts
`cmd/server/main.go:84` and `:87` call `http.ListenAndServeTLS` / `http.ListenAndServe`
directly. There is no `http.Server{}` literal, so `ReadTimeout`, `WriteTimeout`,
`IdleTimeout` and `MaxHeaderBytes` are all zero — slow-client connections are held open
indefinitely.

Fix with care: a naive `WriteTimeout` would kill the SSE stream
(`internal/platform/broker/broker.go:57+`), which is a long-lived response by design. The
correct shape is `ReadHeaderTimeout` + `ReadTimeout` + `IdleTimeout` with `WriteTimeout`
left at zero, or a per-route override for the events endpoint. This may well be why the
timeouts are absent rather than an oversight.

### S-5 — Unbounded SSE subscriptions
`broker.go:34-38` adds one map entry per connection with no cap on concurrent subscribers
and no per-client limit. Combined with S-1 and S-2, connections are free to open and each
one costs a goroutine plus a channel.

### S-6 — TLS is optional and off by default
`cmd/server/main.go:80` enables TLS only when both `TLSCert` and `TLSKey` are non-empty;
otherwise it serves plain HTTP on `0.0.0.0`. The comment at `main.go:20` says HAProxy is
expected to forward the original `Host` unchanged, which suggests TLS terminates upstream
in the real deployment. Confirm that before treating this as a live finding — if the proxy
terminates TLS, this is configuration hygiene, not an exposure.

### S-7 — Host-header routing
`cmd/server/main.go:34` selects the module from `r.Host`, a client-controlled header. Safe
behind a proxy that normalizes `Host`; a module-isolation problem if the app is ever
exposed directly, since a client could choose which module it talks to.

### S-8 — Permissive data-file modes (low)
`internal/grocery/store.go:281` writes the data file `0644` and `store.go:101` /
`internal/grocery/build.go:15` create directories `0755`. Grocery data is world-readable on
the host. Severity depends entirely on who else has an account there.

---

## Verified non-findings

Recorded so the later branch does not re-chase them:

- **Static-file path traversal — not present.** `internal/grocery/build.go:47` uses
  `filepath.Join(sh.dir, filepath.Clean("/"+r.URL.Path))`. Rooting the path before cleaning
  is the idiom that neutralizes `../`; it is correct as written.
- **Partial-write window on save — not present.** `internal/grocery/store.go:281-288`
  writes a `.tmp` file and then `os.Rename`s it, which is atomic on the same filesystem.

---

## What the Recipes tab adds to this surface

Nothing new in kind. The seven new routes (`GET/POST /api/recipes`,
`PATCH/DELETE /api/recipes/{id}`, `POST /api/recipes/{id}/ingredients`,
`DELETE /api/recipes/{id}/ingredients/{item_id}`, `POST /api/recipes/reorder`) inherit
S-1 through S-4 exactly as the existing item routes do. The two new body-decoding sites
inherit S-3.

**Not deferred:** recipe names are user-controlled strings that reach `innerHTML` in the
new rendering paths. `docs/PLAN-recipes-tab.md` mandates `esc()` (`web/grocery/app.js:151`)
at every such site plus `CSS.escape` for selectors built from a recipe id, with an
acceptance criterion behind it. That is handled in the Recipes work as output-encoding
consistency with what `app.js` already does at `:718, :721, :725` — it is not part of this
backlog.

---

## Suggested order for the later branch

S-1 first; it is the finding that makes the others exploitable, and adding auth changes the
severity calculus for S-2 and S-5. Then S-3 and S-4 together (both are `http.Server` /
middleware-level and touch the same files). S-2 becomes a narrow allow-list question once
S-1 exists. S-6 and S-7 are deployment questions to settle with whoever owns the HAProxy
config. S-8 is a one-line change whenever the file is next touched.

---

## Deferred UX notes (not security findings)

### U-1 — multissh key picker lists every file in ssh_dir, not just private keys
Deferred by Chris on 2026-09-11 ("more than private keys are showing.. which can be
confusing, but I don't want to fix it right now"). Observed on branch `security-fix`
after `1040bfe` pointed the local profile's `ssh_dir` at `~/.ssh`.

`GET /api/ssh/keys` (`internal/multissh/server.go:54` → `handleSSHKeys` →
`sshproxy.ListKeys`, `internal/multissh/sshproxy/keys.go:32`) returns ALL regular files
in `ssh_dir`, sorted by name — subdirectories are greyed out, nothing else is filtered.
Against a real `~/.ssh` the picker therefore offers `authorized_keys`, `config`,
`known_hosts`, `known_hosts.old`, and `*.pub` alongside the actual private keys.
Selecting a non-key file just fails at connect time; the harm is confusion, not exposure
(the file contents are not sent to the browser).

Future fix: filter (or at least annotate) server-side in `ListKeys` — per the standing
"don't rely on the front end for security" directive — e.g. skip `*.pub`, `config`,
`known_hosts*`, `authorized_keys`, or positively match files whose first line looks like
a PEM/OpenSSH private-key header. Do NOT action this until asked.
