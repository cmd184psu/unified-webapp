# unified-webapp

Unified Go server for todo-list, slideshow, menuserver, grocery-list, and obsidianoid (Obsidian-vault viewer/editor). A single binary replaces separate services. HAProxy (or `/etc/hosts`) routes hostnames to the correct module; the Go server dispatches on the `Host` header.

---

## How It Works

Every request carries a `Host` header. The server reads that header and looks up the matching module in `host_routing`. If no entry matches, it returns 404. This means the server does nothing useful until `host_routing` is configured.

---

## Quick Start

### 1. Generate the default config

```bash
make init-config
```

This writes `~/.unified-webapp.json` with all keys present and empty `host_routing`.

### 2. Edit the config

Open `~/.unified-webapp.json` and fill in:

- `host_routing` — map hostname → module name
- Module `static_dir` and `data_dir` / `data_file` paths (use absolute paths in production)

Minimal example with all seven modules. Multiple hostnames can map to the same module — useful for adding `-test` aliases that won't collide with live services on your network:

```json
{
  "port": 8080,
  "tls_cert": "",
  "tls_key": "",
  "host_routing": {
    "grocery.cmdhome.net":        "grocery",
    "grocery-test.cmdhome.net":   "grocery",
    "todolist.cmdhome.net":       "todo",
    "todo-test.cmdhome.net":      "todo",
    "slideshow.cmdhome.net":      "slideshow",
    "slideshow-test.cmdhome.net": "slideshow",
    "menu.cmdhome.net":               "menuserver",
    "menu-test.cmdhome.net":          "menuserver",
    "obsidianoid.cmdhome.net":        "obsidianoid",
    "obsidianoid-test.cmdhome.net":   "obsidianoid",
    "multissh.cmdhome.net":           "multissh",
    "multissh-test.cmdhome.net":      "multissh",
    "certmachine.cmdhome.net":        "certmachine",
    "certmachine-test.cmdhome.net":   "certmachine"
  },
  "grocery": {
    "static_dir": "/opt/unified-webapp/web/grocery",
    "data_file":  "/data/grocery.json",
    "groups": ["Produce", "Meats", "mid store", "back wall", "frozen", "deli area near front"],
    "title": "Grocery List",
    "sync_interval_seconds": 1
  },
  "todo": {
    "static_dir": "/opt/unified-webapp/web/todo",
    "data_dir":   "/data/todo",
    "ext": "json",
    "default_subject": "home",
    "sync_interval_seconds": 1
  },
  "slideshow": {
    "static_dir": "/opt/unified-webapp/web/slideshow",
    "image_dir":  "/data/slideshow",
    "prefix":     "slides",
    "default_subject": ""
  },
  "menuserver": {
    "static_dir": "/opt/unified-webapp/web/menuserver",
    "data_dir":   "/data/menuserver",
    "show_all_pages": false
  },
  "obsidianoid": {
    "static_dir": "/opt/unified-webapp/web/obsidianoid",
    "data_dir":   "/data/obsidianoid",
    "vaults": [
      { "path": "/path/to/vault", "name": "My Vault", "theme": "dark" }
    ],
    "threads_folder": "Threads",
    "thread_count": 4,
    "autosave_disabled": false
  },
  "multissh": {
    "static_dir": "/opt/unified-webapp/web/multissh",
    "ssh_dir": "",
    "upload_dir": "",
    "hosts_path": "/data/multissh/multissh-hosts.json",
    "browse_root": "",
    "max_sessions": 3,
    "max_upload_bytes": 8589934592,
    "strict_host_key": false,
    "known_hosts_path": ""
  },
  "certmachine": {
    "static_dir": "/opt/unified-webapp/web/certmachine",
    "db_path": "/data/certmachine/certmachine.db",
    "legacy_import_dir": "",
    "default_validity_days": 365,
    "expiry_warn_days": 30
  }
}
```

#### The `multissh` section

| Field | Meaning |
|---|---|
| `static_dir` | Built frontend for the module. Must exist and be readable, or multissh fails to build. |
| `ssh_dir` | Directory backing the SSH key picker. Empty resolves to the server user's `~/.ssh` at startup. |
| `upload_dir` | Staging area for broadcast uploads. Empty resolves to `$TMPDIR/multissh-uploads`. |
| `hosts_path` | JSON file of saved host presets. Passwords are never written here. |
| `browse_root` | Sandbox root for the server-side file browser. Empty resolves to `upload_dir`. |
| `max_sessions` | Number of concurrent terminal panels, 1–16. `0` means "unset" and takes the default of 3; values above 16 are clamped with a warning. |
| `max_upload_bytes` | Hard cap per upload. Default 8 GiB. |
| `strict_host_key` | `true` verifies SSH host keys against `known_hosts_path` and fails closed if that file is missing. |
| `known_hosts_path` | Empty resolves to `<ssh_dir>/known_hosts`. |

Running and using the module — host cards, terminals, broadcasts, the proxy requirements, and the audit log — is documented separately in **[docs/multissh.md](docs/multissh.md)**. Read the [proxy section](docs/multissh.md#3-putting-it-behind-a-proxy) before putting it behind nginx: a front end that rewrites the `Host` header breaks every terminal while leaving the page looking fine. Note also that this module has **no login** — reaching its hostname is the whole access boundary.

**Empty strings are meaningful, not omissions.** `ssh_dir`, `upload_dir`, `browse_root` and `known_hosts_path` are resolved at startup from the environment, so `make init-config` writes them as present-but-empty strings. An empty value reads as "resolve this for me"; leaving the key out entirely would be indistinguishable from a typo'd key name. Keep them present.

#### The `certmachine` section

| Field | Meaning |
|---|---|
| `static_dir` | Built frontend for the module. Must exist and be readable, or certmachine fails to build. |
| `db_path` | Path to the module's SQLite database. Created (along with its parent directory, `0700`) on first run if missing. |
| `legacy_import_dir` | Directory holding a standalone `certmachine` installation's PKI (`rootCA.crt`, `rootCA.key`, `certs/`). Empty means no import is offered. If set but unreadable, the module still builds and serves — the UI shows the reason instead of the import wizard. |
| `default_validity_days` | Validity period for newly generated and renewed leaf certificates, in days. `0` means "unset" and takes the default of **365**. |
| `expiry_warn_days` | How many days before a certificate's (or the CA's own) expiry the UI shows an "expiring soon" badge, and the threshold below which `POST /api/certs` and renew refuse with 409 rather than mint something a client would soon distrust along with its issuer. `0` means "unset" and takes the default of **30** — **`expiry_warn_days` cannot express "never warn."** Following `max_sessions`'s convention above, `0` normalizes to the default rather than disabling the check, so the smallest effective warning horizon is `1` day, not `0`. Set it to `1` if you want the closest thing to "only warn when it's actually about to expire," never `0` expecting silence — you'll get the 30-day default instead, and since this same value also gates the CA's own expiring-CA 409, the surprise would not be confined to badge colors. |

Running and using the module — the download-to-HAProxy workflow, the import wizard, trusting the root CA, the status badge vocabulary, backup, and the manual procedure for replacing the root CA — is documented separately in **[docs/certmachine.md](docs/certmachine.md)**. Note also that this module has **no login** — reaching its hostname is the whole access boundary, same as multissh above.

### 3. Run

```bash
make run
```

---

## Local Testing (No DNS, No HAProxy)

The server dispatches on the `Host` header, so a plain `http://localhost:8080` in a browser sends `Host: localhost` — which won't match any module unless you add a `"localhost"` entry. Three options:

### Option A — `/etc/hosts` (recommended for browser testing)

Use `-test` hostnames so you don't shadow live services already on your network. Add to `/etc/hosts`:

```
127.0.0.1  grocery-test.cmdhome.net  todo-test.cmdhome.net  slideshow-test.cmdhome.net  menu-test.cmdhome.net
```

Make sure those same names are in `host_routing` in your config (see the example above). Then open any of these in a browser:

```
http://grocery-test.cmdhome.net:8080
http://todo-test.cmdhome.net:8080
http://slideshow-test.cmdhome.net:8080
http://menu-test.cmdhome.net:8080
```

No HAProxy needed. Undo by removing the line from `/etc/hosts`. The production hostnames (`grocery.cmdhome.net`, etc.) continue to work via DNS as normal — the `-test` entries only resolve to localhost on this machine.

### Option B — curl with explicit Host header

No config changes needed. Useful for API testing:

```bash
curl -s -H "Host: todo-test.cmdhome.net"      http://localhost:8080/config/
curl -s -H "Host: grocery-test.cmdhome.net"   http://localhost:8080/items
curl -s -H "Host: slideshow-test.cmdhome.net" http://localhost:8080/items
curl -s -H "Host: menu-test.cmdhome.net"      http://localhost:8080/items
```

### Option C — localhost fallback in config

Add one entry to `host_routing` to make `http://localhost:8080` open a specific module directly in any browser, no `/etc/hosts` required:

```json
"host_routing": {
  "localhost":                  "todo",
  "todo-test.cmdhome.net":      "todo",
  "todolist.cmdhome.net":       "todo",
  ...
}
```

Only one module can be the `localhost` fallback at a time. Change it to switch which module opens at `http://localhost:8080`.

---

## Grocery: the Recipes tab

The Grocery module has two tabs over one shared list. **Grocery** organises food by where it
sits in the store; **Recipes** organises the same food by the meal it belongs to. There is one
set of items underneath — a recipe ingredient *is* a grocery item, not a copy of one.

### Enabling a recipe

Each recipe has a switch. Enabling it flips all of its ingredients to **needed** on the Grocery
tab; disabling it flips them back to **not needed**. That is a starting point, not a lock — you
can still cycle an individual item to "not needed" by clicking it if you already have some, and
that override survives reloads. Re-enabling the recipe resets it.

Recipes do not share ingredients. If chili and tacos both need beef, you get two rows, `beef
(Chili)` and `beef (Tacos)`. There is no notion of quantity, so two near-duplicate rows is how
"twice the beef" gets expressed.

### New ingredients land in Unallocated

Adding an ingredient to a recipe does not make you choose a store section first. It appears at
the bottom of the Grocery tab under **Unallocated**, and you can drag it into Meats or Produce
whenever you like. The move sticks.

### Why recipe ingredients have no trash icon

On the Grocery tab, the delete (trash) control appears only on **free items** — ones that
belong to no recipe. An ingredient owned by a recipe deliberately has no delete button, so that
you cannot quietly break a recipe from the shopping view. Hovering such a row shows which
recipe owns it, e.g. *"Belongs to recipe Chili"*.

To remove an ingredient, go to the Recipes tab and delete it there, behind a confirmation. Since
both tabs read the same list, it disappears from both. Deleting a whole recipe deletes all of
its ingredients, and the confirmation names the recipe and states how many items go with it.

The rule is enforced server-side too, not just hidden in the UI: `DELETE /api/items/{id}` on a
recipe-owned item returns **409 Conflict**.

### Jumping between the two views

Clicking the `(Chili)` chip on a Grocery row switches to the Recipes tab, expands that recipe's
card and scrolls it into view. Clicking the item *name* cycles its state instead — the chip and
the name are separate controls on the same row.

## Data Directory Layout

### Grocery

Single JSON file:

```
/data/grocery.json
```

### Todo

One JSON file per list item, grouped by subject directory:

```
/data/todo/
  home/
    shopping.json
    tasks.json
  work/
    standup.json
```

`/items/{subject}/index.json` is generated dynamically — do not create it on disk.

### Slideshow

One subdirectory per subject, images inside:

```
/data/slideshow/
  vacation/
    beach.jpg
    sunset.png
  family/
    birthday.jpg
```

Supported formats: `.jpg`, `.jpeg`, `.png`, `.gif`

### Obsidianoid

Reads an existing Obsidian vault directory (plain Markdown files). The vault directory must already exist; the module will not create it.

```
/opt/Vaults/MyVault/
  Projects/
    Note A.md
    Note B.md
  Journal.md
  Threads/          ← created automatically on first thread save
    Thread01.md
    Thread02.md
    Thread03.md
    Thread04.md
```

Thread disabled-state is persisted separately in the module's data directory (not inside the vault):

```
/data/obsidianoid/state.json
```

To migrate existing thread states from the standalone app, copy the `thread_states` array from `~/.obsidianoid.json` into `state.json` under the key `thread_states`.

**Git sync** (`POST /api/git/sync`): shells out to `git add -A && git commit && git push` inside the vault directory. The service user must have git installed and push credentials configured for the vault's remote.

**TypeScript sources**: the frontend is compiled TypeScript. After editing `web/obsidianoid/js/*.ts`, run `make web` then commit the regenerated `.js` files. `make typecheck` runs `tsc --noEmit` for type safety. `make build` / `make build-rpi` do not require npm — compiled JS is committed.

### Menuserver

One subdirectory per subject, JSON menu files inside:

```
/data/menuserver/
  home/
    networking.json
    streaming.json
```

Each menu file follows this shape:

```json
{
  "id": "networking",
  "title": "Networking",
  "sites": [
    { "label": "Router", "url": "http://192.168.1.1" }
  ],
  "notes": ""
}
```

### Certmachine

A single SQLite database, created (with its parent directory at `0700`) on first run if missing:

```
/data/certmachine/certmachine.db
```

There is no `meta.json` and no other on-disk state — the certificate authority row, every leaf certificate row, and everything the UI displays about them (CN, SANs, serial, fingerprint, validity window) live in this one file and are derived from the stored PEM data, not a sidecar. See [docs/certmachine.md](docs/certmachine.md#9-backup) for the backup story: stop the binary and copy this file.

---

## Production: HAProxy Configuration

HAProxy terminates TLS, selects the correct certificate via SNI, and forwards requests to the Go server over plain HTTP on localhost. The Go server reads the `Host` header (preserved by HAProxy by default) to dispatch to the correct module. No ACLs or Host-rewriting rules are needed.

### PEM file format

HAProxy expects a single combined PEM per certificate: the full certificate chain followed by the private key, all concatenated into one file.

```bash
# If you have separate files:
cat fullchain.pem privkey.pem > /etc/haproxy/certs/grocery.cmdhome.net.pem

# Repeat for each hostname:
cat fullchain.pem privkey.pem > /etc/haproxy/certs/todo.cmdhome.net.pem
cat fullchain.pem privkey.pem > /etc/haproxy/certs/slideshow.cmdhome.net.pem
cat fullchain.pem privkey.pem > /etc/haproxy/certs/menu.cmdhome.net.pem

chmod 600 /etc/haproxy/certs/*.pem
```

If your CA provides a single combined file (cert + chain + key), you can use it directly.

**If certs come from the certmachine module**, skip the `cat`/`chmod` steps above entirely: download `haproxy.pem` straight from a cert's row (or extract it from the `.tgz` bundle) and drop it into `/etc/haproxy/certs/` as-is. It is already the correct combined-PEM shape, and the file already carries mode `0600` — the tar archive preserves that bit, so nothing needs re-chmodding after extraction. See [docs/certmachine.md § Downloads and the HAProxy workflow](docs/certmachine.md#7-downloads-and-the-haproxy-workflow).

### /etc/haproxy/haproxy.cfg

```haproxy
global
    log /dev/log local0
    maxconn 4096
    user haproxy
    group haproxy
    daemon

defaults
    log     global
    mode    http
    option  httplog
    option  dontlognull
    timeout connect 5s
    timeout client  30s
    timeout server  30s

#
# Redirect plain HTTP to HTTPS
#
frontend http_front
    bind *:80
    redirect scheme https code 301

#
# HTTPS frontend — HAProxy picks the certificate by SNI.
# Point crt at the directory; HAProxy loads every .pem it finds there.
#
frontend https_front
    bind *:443 ssl crt /etc/haproxy/certs/

    option forwardfor          # adds X-Forwarded-For with real client IP
    default_backend unified_webapp

#
# All four hostnames share the same Go backend.
# HAProxy preserves the Host header — the Go server uses it to dispatch.
#
backend unified_webapp
    server app 127.0.0.1:8080 check
```

HAProxy does not support backslash line continuation, so multiple `crt` entries must either go on one long line or — more maintainably — point at a directory. The directory form above loads every `.pem` file in `/etc/haproxy/certs/` automatically; adding a new cert later just means dropping a new file there and reloading.

If you prefer to list certs explicitly, it must be a single unbroken line:

```haproxy
    bind *:443 ssl crt /etc/haproxy/certs/grocery.cmdhome.net.pem crt /etc/haproxy/certs/todo.cmdhome.net.pem crt /etc/haproxy/certs/slideshow.cmdhome.net.pem crt /etc/haproxy/certs/menu.cmdhome.net.pem
```

### How SNI selection works

When a browser connects to `https://todo.cmdhome.net`, TLS negotiation happens before any HTTP is sent. HAProxy reads the SNI hostname from the TLS handshake and picks the matching certificate automatically. No ACL rules are needed. All four hostnames end up at the same backend; the Go server then reads the `Host` HTTP header to decide which module to invoke.

**Do not add** `http-request set-header Host` or `reqset-header` — those would overwrite the Host header and break dispatch.

### DNS

Point all four A records at the HAProxy host:

```
grocery.cmdhome.net    A  <haproxy-ip>
todo.cmdhome.net       A  <haproxy-ip>
slideshow.cmdhome.net  A  <haproxy-ip>
menu.cmdhome.net       A  <haproxy-ip>
```

### Verify

```bash
# Check HAProxy config is valid before reloading
haproxy -c -f /etc/haproxy/haproxy.cfg

# Reload without dropping connections
systemctl reload haproxy

# Confirm the correct cert is served for each hostname
echo | openssl s_client -connect <haproxy-ip>:443 -servername todo.cmdhome.net 2>/dev/null | openssl x509 -noout -subject
echo | openssl s_client -connect <haproxy-ip>:443 -servername grocery.cmdhome.net 2>/dev/null | openssl x509 -noout -subject
```

---

## Authentication (optional)

Auth is off by default: a config with no `auth` section, or an empty one (what `make init-config` writes), behaves exactly like the server did before auth existed — every module wide open, no login, no cookies. Everything below only matters once you start filling in `auth` in your config. See `unified-webapp-example.json` for a fully-populated example (per-module matrix, PINs, an API key, LDAP, and passkeys).

### The model: per-module method lists are literal

`auth.modules` maps a module name to the list of methods that unlock it — `"pin"`, `"key"`, `"ldap"`, `"passkey"`. There is no strength ranking between them: listing `["pin", "ldap"]` on a module means *either* a matching PIN *or* a successful LDAP bind opens it, full stop. If you want a module protected only by something strong, only list that one method — don't rely on the list being read as "at least this secure."

A module with no entry in `auth.modules` at all is unprotected, same as if `auth` weren't configured. The one exception is `admin`: when it's routed (appears in `host_routing`), it is *always* protected — with or without a matrix entry — using the operator PIN described below. A live matrix save can add methods to `admin`'s entry, or even delete the entry outright, but it can never remove the operator PIN, because the operator PIN isn't a member of the matrix in the first place.

### `origin_check`: `enforce` is the default, `log` is the escape hatch

`server.origin_check` rejects cross-origin state-changing requests (any method other than GET/HEAD whose `Origin` header doesn't match `Host`) with a 403, before the request body is even read. `"enforce"` (the default — both an unset field and the literal string mean the same thing) is what you want for anything reachable from a browser. If something legitimate is tripping the check — a reverse-proxy setup where `Origin` and `Host` genuinely differ for a reason you've verified is safe — set it to `"log"` to see the rejections in the server log without actually blocking them, diagnose, then either fix the mismatch or move on. `"off"` disables the check entirely; there's rarely a good reason for that outside of local testing with `curl -H Host: ...`, which never sends `Origin` anyway and so isn't affected by this setting either way.

### The reverse proxy must preserve the `Host` header

This is load-bearing, not a nicety: the Go server dispatches every request by `Host` (`host_routing`) and the origin check above compares `Origin` against that same `Host`. A proxy that rewrites `Host` — even to "fix" something — breaks module dispatch and can make legitimate same-origin requests look cross-origin. HAProxy already preserves `Host` by default (see the section above; don't add a `set-header Host` rule). If you're fronting with nginx instead, make sure your `location` block has:

```nginx
proxy_set_header Host $host;
```

nginx's default `Host` behavior varies by version/config, so set this explicitly rather than assuming it's already correct.

### Session key file: rotate or delete it to force a global logout

Sessions are signed JWTs (HMAC-SHA256) using a key generated on first boot and stored at `<auth.data_dir>/session.key`. Every currently-issued session is validated against that one key. If you ever need to invalidate every session at once — a suspected leak, or just "log everyone out" — stop the server, delete (or move aside) `session.key`, and restart; a fresh key is generated and every existing session cookie stops verifying. There's no per-session revocation list; this file is the only lever.

### `cookie_domain` and `passkey.rp_id`: share them across modules on a common parent domain

If your modules live under a shared parent domain (e.g. `grocery.cmdhome.net`, `todo.cmdhome.net`), set `auth.cookie_domain` to the parent (`.cmdhome.net`) so one login session is valid across all of them — no separate login per module. Passkeys work the same way via `auth.passkey.rp_id`: set it to the shared parent domain and a passkey registered on one module's hostname is usable to log into any other module under that same `rp_id`, as long as each module's origin is also listed in `auth.passkey.rp_origins`. Leave `cookie_domain` empty (host-only cookie) and set `rp_id` per-hostname if you'd rather keep each module's login fully separate.

### First-run bootstrap for the admin operator PIN

The `admin` module (when routed) always requires an operator PIN, configured as exactly one of `auth.admin_pin` (a bcrypt hash, generated with `-hash-pin`) or `auth.admin_pin_file` (a plaintext file read fresh on every login attempt — no caching, so editing it takes effect on the very next attempt). The file form is the easiest way to get started:

```bash
echo "$PIN" > admin.pin && chmod 0400 admin.pin
```

then point `auth.admin_pin_file` at that path. The file's permissions are checked on every read; anything looser than `0400` is refused with an error telling you to `chmod` it.

### Bind address stays `0.0.0.0`

The server still binds `0.0.0.0:<port>` regardless of auth configuration — that hasn't changed and isn't going to. The expected deployment posture is the one described above: a reverse proxy (HAProxy/nginx) on the same host or network handles TLS and is the only thing actually reachable from outside, forwarding to the Go server over plain HTTP on localhost or an internal address. Auth is defense for a proxy or an internal network you don't fully trust, not a substitute for keeping the Go server's port off the public internet.

### API keys for automation

Scripts and other non-browser clients can authenticate with an API key instead of logging in interactively. Generate one with:

```bash
go run ./cmd/server -gen-api-key
```

which prints the key once (put it wherever your script reads secrets from) and its `sha256:...` hash (paste that into `auth.api_keys`). Send the key as either header — `Authorization: Bearer <key>` is checked first, falling back to `X-API-Key: <key>` if `Authorization` is absent or isn't `Bearer`-shaped. A module only accepts API keys if `"key"` appears in its `auth.modules` entry.

---

## Make Targets

| Target | Description |
|---|---|
| `make run` | Run the server with `~/.unified-webapp.json` |
| `make build` | Compile to `./unified-webapp` binary |
| `make test` | Run all tests with race detector |
| `make init-config` | Write default config to `~/.unified-webapp.json` |
| `make clean` | Remove compiled binary |
