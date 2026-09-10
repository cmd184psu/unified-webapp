# multissh

Drive several SSH terminals from one browser page, broadcast a command to all of
them at once, and fan a file out to them over SFTP.

multissh is the sixth module of `unified-webapp`. It is not a separate service:
it runs inside the same binary, on the same port, and is reached by its own
hostname through the `host_routing` table. Configuration lives in the `multissh`
section of `~/.unified-webapp.json` — see [the README](../README.md#the-multissh-section)
for the field reference. This document covers running and using it.

---

## Contents

1. [Before you start](#1-before-you-start)
2. [How it fits together](#2-how-it-fits-together)
3. [Putting it behind a proxy](#3-putting-it-behind-a-proxy)
4. [Quick start](#4-quick-start)
5. [The interface](#5-the-interface)
6. [Configuring hosts](#6-configuring-hosts)
7. [Working with terminals](#7-working-with-terminals)
8. [Broadcasting a file](#8-broadcasting-a-file)
9. [Host-key verification](#9-host-key-verification)
10. [When something breaks](#10-when-something-breaks)
11. [Audit log](#11-audit-log)
12. [What this module does not do](#12-what-this-module-does-not-do)
13. [HTTP API](#13-http-api)

---

## 1. Before you start

**There is no login.** Anyone who can reach the multissh hostname can open a
root shell on every host you have configured and push files to them. Reaching
the hostname is the entire access-control story in this release. Application
authentication is a separate, later piece of work; until it lands, the network
is the boundary.

Two other things worth knowing up front:

- **Passwords are memory-only.** A password typed into a host card lives in the
  browser tab and, for the duration of a connection, in the server process. It
  is never written to `hosts_path`, never logged, and is gone the moment you
  reload the page or restart the server. Re-entering it on every session is the
  intended behavior, not an oversight.
- **A healthy process is not proof multissh came up.** See
  [multissh alone can be down](#multissh-alone-can-be-down).

---

## 2. How it fits together

```
  Browser                      unified-webapp                 targets
  ────────                     ─────────────                  ───────
  panel 1 ──── WebSocket ────▶ ssh bridge ──── SSH PTY ─────▶ host A
  panel 2 ──── WebSocket ────▶ ssh bridge ──── SSH PTY ─────▶ host B
  panel N ──── WebSocket ────▶ ssh bridge ──── SSH PTY ─────▶ host N
  upload  ──── HTTP POST ────▶ staging dir ─── SFTP ────────▶ A, B, N
```

Each panel opens its own WebSocket and its own SSH connection, so the sessions
run fully in parallel and one hanging host does not stall the others. File
broadcasts use SFTP, one goroutine per target, each reporting progress
independently.

**Private keys never leave the server.** The browser picks a key by *base name*
from a listing of `ssh_dir`; the server resolves the name to a path and reads
the file itself. A saved host preset stores the key name, never key material.

---

## 3. Putting it behind a proxy

**The proxy must pass the original `Host` header through.** The WebSocket
upgrade handlers compare the request's `Origin` against its `Host` and reject
anything that does not match. A proxy that rewrites `Host` to `localhost:8080`
(the default in several nginx snippets) makes that comparison fail for every
upgrade: the page loads, the host rail works, the key picker works, and not one
terminal will connect.

For nginx, the multissh vhost needs all four lines:

```nginx
location / {
    proxy_pass http://127.0.0.1:8080;
    proxy_set_header Host $host;              # required, see above
    proxy_http_version 1.1;                   # required for WebSockets
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection "upgrade";
}
```

Long-running terminals also outlive the default proxy read timeout. Raise it on
this vhost (`proxy_read_timeout 1d;`) or idle sessions will drop.

If terminals fail to connect, the server log says exactly what happened:

```
multissh: audit ws upgrade rejected origin="https://multissh.cmdhome.net" host="localhost:8080"
```

The two values not matching *is* the diagnosis.

---

## 4. Quick start

1. Put the private keys for your targets in the server's SSH directory (the
   `ssh_dir` setting; empty means the server user's `~/.ssh`). They must be
   **unencrypted** — there is no passphrase prompt.
2. Add a `multissh` entry to `host_routing` and fill in the `multissh` config
   section (see [the README](../README.md#the-multissh-section)).
3. Build and run:
   ```bash
   make build
   make run
   ```
4. Open the multissh hostname, e.g. `http://multissh-test.cmdhome.net:8080`.
5. In the **host rail** on the left, fill in Host 1: IP/hostname, user, port,
   then either pick an **SSH key** or switch that card to **Password**.
6. On the **SSH Console** tab, click **Connect** on Host 1's panel.
7. Type into the panel, or use the **Broadcast** bar at the top to send the
   same line to every connected host.

---

## 5. The interface

A persistent **host rail** on the left, and a **tabbed pane** on the right with
two tabs: **SSH Console** and **Upload to Host**. Host configuration is shared
between both tabs, and switching tabs never tears down a live session.

The number of host cards and terminal panels is `max_sessions` (default 3,
maximum 16). The page asks the server for it at load time via
`GET /api/config` — it is not baked into the bundle, so changing
`max_sessions` and restarting is enough. Above four hosts the rail widens and
lays the cards out in two columns so they stay reachable without scrolling;
below 900px viewport width it moves above the tabs instead.

---

## 6. Configuring hosts

Each card holds:

- **IP / hostname**, **User**, **Port** (default 22).
- **Auth**: **SSH key** or **Password**.
  - **SSH key** — click the key field to open a picker listing the non-hidden
    files in `ssh_dir`. Pick the *private* key.
  - **Password** — a password field on the card. It is not saved (see below),
    and it is cleared when the page reloads. The placeholder says so.
- **Remote directory** (default `/tmp`) — the default broadcast destination for
  that host. For key hosts, clicking it opens a live SFTP directory picker
  (IP, user, and key must be set first). For password hosts the picker is not
  available and the field is a plain text box — type the path.
- **Copy ssh command** — puts a ready-to-paste invocation on your clipboard:

  ```bash
  ssh -i ~/.ssh/id_lab -p 2222 root@10.0.0.5
  ```

  `-p` is omitted for port 22, and `-i` is omitted when the card has no key —
  including when it is set to **Password**, where the command is just
  `ssh user@host` and your own client prompts you. The command never contains a
  password, and switching a card to Password drops a previously chosen key name
  from it.

**What gets saved.** Card edits are written to the `hosts_path` JSON file
automatically, so presets survive a reload and a restart. Only IP, port, user,
key *name*, and remote directory are written. The password is not part of the
saved record — not as an empty string, not as a redacted placeholder. The type
that gets serialized has no password field at all, so there is no code path by
which one could reach the file.

To confirm on your own install:

```bash
grep -ci password /data/multissh/multissh-hosts.json   # 0
```

---

## 7. Working with terminals

Each panel header has:

- **▾ / ▸** — collapse or expand the panel. Collapsing is display only: the
  WebSocket stays open, the session stays connected, output keeps arriving, and
  it is all there when you expand again. Useful for parking six connected hosts
  while you work in the seventh.
- **Status** — `disconnected`, `connecting`, `connected`, or `error: <reason>`.
- **Pause** — that panel ignores broadcast input. You can still type into it
  directly, and **Ctrl-C** still works.
- **Ctrl-C** — sends an interrupt to that host alone, even while paused. This
  is the escape hatch for one console stuck on a prompt.
- **Connect / Disconnect**.

### The broadcast bar

What you type goes to every **connected, non-paused** panel. Enter or **Send to
all** sends it with a trailing newline.

- **Up / Down arrows** walk back through the lines you have sent this browser
  session, shell-style. The line you were typing is preserved: arrow back up
  through history, then back down past the newest entry, and your unfinished
  line returns. History is in memory only and is not persisted.
- **`key:<name>`** sends a control sequence instead of text. Currently
  `key:ctrl+c` (interrupt every connected, non-paused terminal at once).
  An unrecognised name sends **nothing at all** and shows a hint listing the
  known ones — a typo'd `key:ctlr+c` will not be blasted at your hosts as
  literal text. The line stays in the field so you can fix it.

### Reconnecting

A failed connect reports the error and leaves the socket open. Fix the card and
click **Connect** again.

---

## 8. Broadcasting a file

On the **Upload to Host** tab:

**Stage 1 — choose a source.** Either drag a file onto the drop zone (streamed
straight to disk in `upload_dir`, so multi-GB files do not buffer in memory), or
click *"Or choose a server-resident file…"* to browse a sandbox rooted at
`browse_root` and pick something already on the server.

**Stage 2 — pick targets.** Configured hosts appear as checkboxes. A host is
selectable once it has an IP, a user, and a credential — a key *or* a password.
Tick from 1 to `max_sessions` of them and click **Broadcast**.

Each target gets its own row: host, state, progress bar, `transferred / total`.
States run `pending → transferring → done`, or `error: <reason>`. Transfers are
parallel and independent — one failing target does not stop the others. The file
lands in that host's **Remote directory** under its original name.

Re-broadcasting a server-resident file needs no re-upload: pick it from the
browser and go.

---

## 9. Host-key verification

`strict_host_key` is **off by default**, because lab VMs get rebuilt and their
host keys legitimately churn. Off means host keys are not verified at all.

Turning it on (`"strict_host_key": true`) verifies against `known_hosts_path`
(empty resolves to `<ssh_dir>/known_hosts`) for both terminals and SFTP, and it
fails closed in both directions:

- A missing or unreadable `known_hosts` is a **build-time failure** for the
  module — see below.
- An unknown or changed host key is **rejected at connect time**.

Pre-populate the file the usual way:

```bash
ssh-keyscan -H 10.0.0.5 >> ~/.ssh/known_hosts
```

After a legitimate VM rebuild, remove the stale entry before adding the new key.

---

## 10. When something breaks

Client-facing messages are deliberately generic; the detail is in the server
log. If the UI is vague, read the log.

### multissh alone can be down

A misconfigured multissh — unreadable `static_dir`, or `strict_host_key: true`
with a `known_hosts` that is missing — **fails to build at startup**. The other
five modules build and serve normally, the process stays up, and the port stays
green. Every multissh hostname returns **503** with the reason in the body:

```bash
$ curl -s -H 'Host: multissh.cmdhome.net' http://localhost:8080/
{"error":"module unavailable","module":"multissh","reason":"multissh: static_dir /opt/unified-webapp/web/multissh: stat /opt/unified-webapp/web/multissh: no such file or directory"}
```

So: **a running process is not evidence that multissh came up.** Check the 503
body, or the boot log, which names the offending path:

```
ERROR: module "multissh" failed to build and will return 503 on every request: multissh: known_hosts /home/ops/.ssh/known_hosts: sshproxy: secure mode requires a readable known_hosts at "/home/ops/.ssh/known_hosts": open /home/ops/.ssh/known_hosts: no such file or directory
```

### Quick reference

| Symptom | Likely cause | What to do |
|---|---|---|
| Every multissh URL returns 503, other modules fine | Module failed to build | Read the 503 body / boot log; fix `static_dir` or `known_hosts` |
| Page loads, no terminal ever connects | Proxy rewrote `Host` | Add `proxy_set_header Host $host;` ([§3](#3-putting-it-behind-a-proxy)); confirm with the `ws upgrade rejected` log line |
| Terminals drop after a few minutes idle | Proxy read timeout | Raise `proxy_read_timeout` on this vhost |
| `error: connection failed: check host, user, and key` | Host down, wrong user/port, key not authorized, or an encrypted key | Check the card, then the server log |
| Connect refused only with `strict_host_key: true` | Host key unknown or changed | Update `known_hosts` |
| `error: transfer failed` on one broadcast row | That target unreachable or remote dir not writable | Fix that host; re-broadcast it alone |
| Upload rejected, "exceeds maximum allowed size" | Over `max_upload_bytes` (413; the partial file is removed) | Raise the cap |
| "invalid file path" / "path escapes browse root" | Source outside `browse_root` | Move it under `browse_root` |
| Directory picker: "unable to list remote directory" (502) | SFTP unreachable, path missing, or a password host | Check host/user/key and the path; password hosts have no picker — type the path |
| An `uploadId` stops working after a restart | The upload registry is in memory | Re-select the staged file via the server-side browser; it is still on disk |
| Saved hosts vanish | `hosts_path` not writable | Check the path and the log |
| Password gone after a reload | Working as designed | Re-enter it; passwords are never persisted |

### Encrypted keys

Not supported. The log says *"parse key (encrypted keys are not supported)"* and
the UI shows the generic connect failure. Use an unencrypted key.

---

## 11. Audit log

Every connect, disconnect, and broadcast target is logged through the standard
logger, which on a systemd install lands in journald:

```bash
journalctl -u unified | grep 'multissh: audit'
```

```
multissh: audit connect host="10.0.0.5" port=22 user="ops" auth=key outcome=ok
multissh: audit broadcast host="10.0.0.5" user="ops" remote="/opt/staging/patch.sh" bytes=418 outcome=start
multissh: audit broadcast host="10.0.0.5" user="ops" remote="/opt/staging/patch.sh" bytes=418 outcome=ok
multissh: audit disconnect host="10.0.0.5" user="ops" reason=client
```

These lines record **what was reached and by whom, never what it was reached
with**. No password, no key path, and no credential of any kind is an argument
to the audit logger; tests assert it and fail if that changes. `auth=password`
tells you a password was used — the password itself is not there and cannot be
recovered from the log.

`outcome` is `ok`, `failed`, or `rejected`; `reason=client` versus
`reason=remote` distinguishes a disconnect you asked for from one the far end
initiated.

---

## 12. What this module does not do

- **No authentication.** No login, no sessions, no per-user anything. Network
  reachability is the access boundary in this release.
- **No TLS of its own.** The server terminates plain HTTP; TLS belongs to the
  front proxy. (`tls_cert` / `tls_key` at the top of the config are the
  process-wide setting, not a multissh feature.)
- **No environment-variable overrides.** The standalone tool had a `MULTISSH_*`
  variable per field; here, JSON config is the only input. Anything you
  remember as `MULTISSH_SSH_DIR` is `multissh.ssh_dir`.
- **Unencrypted private keys only.**
- **SFTP only** for transfers; the legacy `scp` protocol is not used.
- **The upload registry is in memory** — staged files outlive a restart, their
  IDs do not.

---

## 13. HTTP API

For automation. Client-facing errors are generic; detail goes to the log.

| Method & path | Purpose | Notes |
|---|---|---|
| `GET /api/config` | Runtime config for the SPA | `{"maxSessions":N}` — what the rail sizes itself from |
| `GET /api/ssh/keys` | Selectable key names in `ssh_dir` | Names only, never paths or material |
| `GET /api/ssh/ws` | Per-terminal WebSocket bridge | Same-origin; JSON text control frames, binary keystrokes/output |
| `GET /api/hosts` | Read saved presets | `{"hosts":[…]}` |
| `PUT /api/hosts` | Save presets | At most `max_sessions`; safe fields only; atomic write |
| `POST /api/sftp/listdir` | List a directory on a target | `{host,port,user,key,path}`; 502 on failure |
| `GET /api/files?path=<rel>` | Browse under `browse_root` | Sandboxed; directories first |
| `POST /api/upload` | Stream-upload a file | `multipart/form-data` field `file`; 413 over `max_upload_bytes` |
| `GET /api/uploads` | List staged uploads | In-memory registry |
| `DELETE /api/uploads/{id}` | Remove a staged upload | 204 on success |
| `POST /api/broadcast` | Start an SFTP broadcast | `{uploadId? \| filePath?, targets:[{host,port,user,key,password,remoteDir}]}` → `{jobId}` |
| `GET /api/broadcast/ws?job=<id>` | Live per-target progress | Same-origin; `pending/transferring/done/error` frames, then `complete` |

Two rules the server enforces on every credential-carrying request, so a client
must satisfy both:

- **Exactly one source.** `POST /api/broadcast` takes `uploadId` *or*
  `filePath`, never both and never neither (400).
- **Exactly one credential.** Each target, and each terminal connect frame,
  carries a `key` or a `password` — never both, never neither (400 / rejected
  connect). Send the unused one as `""`.
