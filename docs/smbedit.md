# smbedit

Edit Samba shares from a browser: shares, globals, an smb.conf preview, an
import from an existing file, and a Save & Restart button that writes
`/etc/samba/smb.conf` and bounces the smbd service.

smbedit is the port of the standalone `smbed` tool into `unified-webapp`. It is
not a separate service: it runs inside the same binary, on the same port, and
is reached by its own hostname through the `host_routing` table. Configuration
lives in the `smbedit` section of `~/.unified-webapp.json` — see
[the README](../README.md) for the field reference. This document covers
running and operating it.

---

## Contents

1. [Before you start](#1-before-you-start)
2. [How it fits together](#2-how-it-fits-together)
3. [Privileges: the sudoers grants](#3-privileges-the-sudoers-grants)
4. [Putting it behind a proxy](#4-putting-it-behind-a-proxy)
5. [Quick start](#5-quick-start)
6. [Migrating from standalone smbed](#6-migrating-from-standalone-smbed)
7. [When something breaks](#7-when-something-breaks)
8. [Owner acceptance checklist](#8-owner-acceptance-checklist)
9. [HTTP API](#9-http-api)

---

## 1. Before you start

**There is no login.** Anyone who can reach the smbedit hostname can rewrite
`/etc/samba/smb.conf` on this machine and restart the Samba service. Reaching
the hostname is the entire access-control story in this release. Application
authentication is a separate, later piece of work; until it lands, the network
is the boundary. Do not expose the smbedit hostname beyond the network segment
you would trust with root-adjacent control of the file server. And note the
boundary is really the **browser** of anyone who can resolve the hostname, not
just the network segment itself: the shared platform middleware answers with
`Access-Control-Allow-Origin: *` and there is no CSRF token, so any web page
open in such a browser can POST to smbedit's API cross-origin (including
`save-and-restart` and `import`) without the user doing anything.

Two other things worth knowing up front:

- **Saving the UI and rewriting smb.conf are two different actions.** Every
  edit in the UI persists only to smbedit's own `state.json` until you press
  **Save & Restart**, which renders smb.conf, writes it (after taking a
  timestamped `.bak` backup of the existing file), and restarts smbd.
- **A healthy process is not proof smbedit came up.** If the module's
  `static_dir` is missing or its `state.json` is corrupt, the module fails to
  build at boot and its hostnames answer 503 with the reason in the body,
  while every other module keeps serving. Check the boot log or just load the
  page.

---

## 2. How it fits together

```
  Browser                      unified-webapp                    host
  ────────                     ─────────────                     ────
  edit UI ──── JSON API ─────▶ state.json  (data_dir, 0600)
  Save & Restart ────────────▶ render ───▶ /etc/samba/smb.conf (+ .bak)
                               └─────────▶ sudo systemctl restart smbd
  ops log ──── SSE ──────────▶ in-memory ring buffer
  samba log ── SSE ──────────▶ sudo tail -n 200 -F <samba_log_path>
```

State lives in `<data_dir>/state.json` (created on first boot, written
atomically, mode 0600). The rendered smb.conf goes wherever `smb_conf_path` in
the module's settings points — `/etc/samba/smb.conf` in production.

Shares whose `path` does not exist on disk are automatically disabled — on
load, on save, and on import — and the ops log says so. This is deliberate:
Samba refuses cleanly instead of exporting a dangling share.

---

## 3. Privileges: the sudoers grants

The service user (see `unified.service`) needs passwordless sudo for exactly
three operations — writing the conf, restarting Samba, tailing the log — and
nothing else:

```
sudo install -m 0644 <tmp> <smb_conf_path>     # write smb.conf when not directly writable
sudo systemctl restart smbd                    # and fallbacks: smb, service smbd restart
sudo tail -n 200 -F <samba_log_path>           # Samba log stream
```

A copy-pasteable snippet for `/etc/sudoers.d/smbedit` (adjust the user, the
conf path, and the log path to your host; edit with `visudo -f`):

```
# smbedit: write smb.conf (and its timestamped .bak backup), restart Samba,
# tail the Samba log — nothing else. The .bak rule is required: Save & Restart
# backs up the existing file through the same "sudo install" path before
# writing, and fails before writing anything if the backup is denied.
cdelezenski ALL=(root) NOPASSWD: /usr/bin/install -m 0644 * /etc/samba/smb.conf
cdelezenski ALL=(root) NOPASSWD: /usr/bin/install -m 0644 * /etc/samba/smb.conf.*
cdelezenski ALL=(root) NOPASSWD: /usr/bin/systemctl restart smbd
cdelezenski ALL=(root) NOPASSWD: /usr/bin/systemctl restart smb
cdelezenski ALL=(root) NOPASSWD: /usr/sbin/service smbd restart
cdelezenski ALL=(root) NOPASSWD: /usr/bin/tail -n 200 -F /var/log/samba/log.smbd
```

**Without these grants nothing crashes.** On a host where sudo is not
configured (a Mac, a dev box), Save & Restart still writes the conf file if the
path is directly writable, and the restart failure comes back in-band:
HTTP 200 with `restart.success=false`, plus ops-log entries naming each failed
command. The module treats "this host can't restart Samba" as a result to
report, not an error to die on.

---

## 4. Putting it behind a proxy

Same two rules as every module, and they matter more here because smbedit has
two Server-Sent-Events endpoints (`/api/logs/ops/stream` and
`/api/logs/samba/stream`):

- **Preserve the `Host` header.** The dispatcher routes on it; a proxy that
  rewrites `Host` sends the request to the wrong module (or a 503).
- **Do not buffer responses.** SSE is a long-lived, incremental response. A
  proxy that buffers it (nginx's default `proxy_buffering on`, or HAProxy
  without tune options) turns the live log view into a page that loads
  forever. For nginx: `proxy_buffering off;` and `proxy_read_timeout` high
  enough for an idle stream. For HAProxy, the defaults pass SSE through; just
  keep `timeout server`/`timeout client` generous on the smbedit backend.

---

## 5. Quick start

1. Add the hostname(s) to `host_routing` in `~/.unified-webapp.json`:

   ```json
   "smbedit-test.cmdhome.net": "smbedit",
   "smbedit.cmdhome.net":      "smbedit"
   ```

2. Fill in the `smbedit` section:

   ```json
   "smbedit": {
     "static_dir": "./web/smbedit",
     "data_dir": "./data/smbedit",
     "picker_root": "/opt"
   }
   ```

3. Point DNS (or HAProxy, or `/etc/hosts`) at the box and restart
   `unified-webapp`. The boot log prints one `registered` line per hostname.

4. Load the page. Set the smb.conf output path and the Samba log path under
   **Settings** (they are state, not server config — they live in
   `state.json` and are editable from the UI). Import your existing smb.conf
   from the Settings page if you have one, review the staged result, save.

The folder picker offers the immediate subdirectories of `picker_root`
(default `/opt`), one level deep, hidden entries excluded.

---

## 6. Migrating from standalone smbed

There is no automatic migration. The manual procedure:

1. **Copy the state file:** `cp ~/.smbed.json <data_dir>/state.json` (with
   `data_dir` as configured, e.g. `./data/smbedit/state.json`, owned by the
   service user). smbedit creates a default `state.json` on first boot if you
   skip this — you would just be starting from an empty share list.
2. **Remove the `listen_addr` key** from the copied file. smbedit has no
   per-module listener — the unified binary owns the port. A leftover
   `listen_addr` is ignored on load, but remove it anyway so the file matches
   what the module writes back.
3. **Add the hostname** to `host_routing` (step 1 of the quick start) and to
   DNS/HAProxy/`/etc/hosts`.
4. **Move the sudoers grants** to the unified service user
   ([section 3](#3-privileges-the-sudoers-grants)) if smbed ran as a
   different account.
5. **Stop and disable the old service:**
   `sudo systemctl disable --now smbed` (or however the standalone binary was
   supervised).

---

## 7. When something breaks

- **The smbedit hostname answers 503, everything else works.** The module
  failed to build at boot: missing/unreadable `static_dir`, unset `data_dir`,
  or a `state.json` that no longer parses. The 503 body and the boot log name
  the cause. A corrupt `state.json` is never silently overwritten — fix or
  remove it yourself.
- **Save & Restart says written but restart failed.** Expected on any host
  without the sudoers grants. The response carries
  `restart.success=false` and the ops log (Logs page, left stream) shows each
  command attempted: `systemctl restart smbd`, then `smb`, then
  `service smbd restart`.
- **The Samba log stream shows nothing / errors immediately.** The stream
  needs `samba_log_path` set in Settings (400 without it) and the `tail`
  sudoers grant. Also check the proxy-buffering rule in
  [section 4](#4-putting-it-behind-a-proxy).
- **A share vanished from Samba after a save.** Check the ops log for an
  auto-disable entry: shares whose `path` is missing on disk are disabled
  rather than exported dangling.

---

## 8. Owner acceptance checklist

Two acceptance items cannot be verified from this repository's test suite and
remain with the owner. Tick them on a real deployment:

- [ ] **Browser pass (FRD §9.3):** load the UI through the routed hostname;
  create/edit/disable a share; use the folder picker; import an existing
  smb.conf and confirm it stages without persisting; watch both log streams
  live; confirm the theme toggle persists across a reload.
- [ ] **Linux/Samba pass (FRD §9.4):** on a Linux host with Samba and the
  sudoers grants from [section 3](#3-privileges-the-sudoers-grants):
  Save & Restart writes `/etc/samba/smb.conf` with a timestamped `.bak`
  alongside, smbd restarts, and the Samba log stream tails. Then remove the
  grants and confirm the same action reports the failure in the UI
  (`restart.success=false`, ops-log entries) without crashing the module.

---

## 9. HTTP API

All under the smbedit hostname. Same shapes as standalone smbed, minus
`listen_addr` in the config payloads.

| Method & path | What it does |
|---|---|
| `GET /api/config` | Full state: settings, globals, shares |
| `PUT /api/config` | Patch settings (smb_conf_path, samba_log_path, share_owner, theme) |
| `GET /api/shares` / `PUT /api/shares` | Read / replace the share list (missing paths auto-disable, ops-logged) |
| `GET /api/globals` / `PUT /api/globals` | Read / replace the [global] entries |
| `GET /api/folders` | Folder-picker listing of `picker_root` |
| `POST /api/import` | Parse an smb.conf into staged globals/shares — does not persist |
| `GET /api/preview` | The rendered smb.conf as text/plain |
| `POST /api/save-and-restart` | Backup, write smb.conf, restart smbd; failures reported in-band |
| `GET /api/logs/ops/stream` | SSE: ops-log backlog, then live entries |
| `GET /api/logs/samba/stream` | SSE: `sudo tail -F` of `samba_log_path` |
| `GET /api/version` | Module version string |
