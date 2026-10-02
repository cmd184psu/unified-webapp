# haproxy

Edit the HAProxy that fronts this network from a browser: services, exposed
ports, the default service, global and defaults settings, certificates pulled
from CertMachine, a pending-changes review with a one-click Apply, backups with
restore, and a read-only live Stats tab.

The module is a port of what you would otherwise do by hand-editing
`haproxy.cfg`. It is not a separate service: it runs inside the same
`unified-webapp` binary, on the same port, and is reached by its own hostname
through the `host_routing` table. The specification is
[docs/frd/FRD-haproxy-editor.md](../frd/FRD-haproxy-editor.md); this document
covers running and operating what shipped. Where the two differ, this guide
describes the code.

---

## Contents

1. [What it is, and what it is not](#1-what-it-is-and-what-it-is-not)
2. [How it fits together](#2-how-it-fits-together)
3. [Supported platforms](#3-supported-platforms)
4. [Module settings](#4-module-settings)
5. [Privileges](#5-privileges)
6. [CertMachine setup](#6-certmachine-setup)
7. [The model](#7-the-model)
8. [Apply, and what each outcome means](#8-apply-and-what-each-outcome-means)
9. [Backups and restore](#9-backups-and-restore)
10. [Certificate lifecycle](#10-certificate-lifecycle)
11. [The Stats tab](#11-the-stats-tab)
12. [Authentication](#12-authentication)
13. [Per-OS notes](#13-per-os-notes)
14. [Putting it behind a proxy](#14-putting-it-behind-a-proxy)
15. [Quick start](#15-quick-start)
16. [When something breaks](#16-when-something-breaks)
17. [Owner acceptance checklist](#17-owner-acceptance-checklist)
18. [HTTP API](#18-http-api)

---

## 1. What it is, and what it is not

**It owns two files.** After you Apply, the module owns `haproxy.cfg` and the
managed crt-list (the file the TLS `bind` lines point at with `crt-list`).
Every Apply regenerates both from the module's model and writes them out.

- **Comments are not preserved.** The generated `haproxy.cfg` starts with
  `# managed by the haproxy editor - manual edits will be overwritten`. Import
  drops comments, and anything the model does not understand is kept verbatim
  as a raw section (and listed in the import report), but your comments do not
  survive.
- **Hand edits are overwritten.** Edit the file on disk and the next Apply
  replaces it. Edit through the browser instead.
- **Back up your original before first use.** The module also keeps its own
  never-pruned first-adoption backup (see [section 9](#9-backups-and-restore)),
  but take your own copy of `haproxy.cfg` first. It costs nothing.
- **No certificate work happens in this module.** It does not generate, parse,
  convert or validate certificates, and it has no upload. Certificates come
  only from [CertMachine](certmachine.md): the module lists them, downloads
  the `haproxy.pem` bundle, verifies it, and files it. Details (SANs, issuer,
  validity, status) are always read live from CertMachine, never from the file.
- **The outage risk is real.** This HAProxy fronts every module. A wrong
  config is an outage. That is why Apply validates with the real `haproxy -c`
  first and rolls back automatically ([section 8](#8-apply-and-what-each-outcome-means)).

Two things worth knowing up front:

- **Saving and applying are different actions.** Edits are held in the browser
  until you press **Save**, which stores the model in the module's own
  `state.json`. Nothing is live until you press **Apply changes**.
- **A healthy process is not proof the module came up.** If `static_dir` is
  missing or `data_dir` cannot be created, the module fails to build at boot
  and its hostnames answer 503 with the reason, while every other module keeps
  serving.

---

## 2. How it fits together

```
  Browser                 unified-webapp (haproxy module)            host
  ───────                 ───────────────────────────────            ────
  edit UI ── JSON API ──▶ state.json   (model, in data_dir)
                          certs.json   (tracking rows, in data_dir, 0600)
                          staging/     (candidate files, validated here)

  Apply ──────────────▶ render ─▶ haproxy -c (staged) ─▶ driver ─▶ sudo ─▶ crt-list, haproxy.cfg,
                                                                           backups/, certs/
                                                         driver ─▶ sudo systemctl reload haproxy
                                                                   (macOS: launchd job, no sudo)
  Stats tab ── GET ───▶ reads the stats socket directly as the app user
                        (`show stat`, `show info` only; no sudo)
  Log tab ──── SSE ───▶ the module's own in-memory operations log

  certificates ◀──── REST + Bearer key ──── CertMachine   (list, details, verified haproxy.pem)
```

All platform differences live behind one driver. The browser and the API never
see an OS-specific value.

`state.json` and `certs.json` live in `<data_dir>`. `certs.json` records only
what the module itself owns: file name, enabled flag, note, the CertMachine id
and FQDN the file came from, and the SHA-256 of the installed bytes. It holds
no certificate details.

---

## 3. Supported platforms

| Platform | Notes |
|---|---|
| **Ubuntu** | systemd, unit `haproxy`. Auto-detect accepts `/etc/os-release` `ID` of `ubuntu` or `debian`. |
| **Rocky Linux 10** | systemd, unit `haproxy`. Rocky 9 works. Auto-detect accepts `ID` of `rocky`, `rhel`, `almalinux`, `centos` or `fedora`. |
| **macOS Tahoe, Apple silicon only** | Homebrew HAProxy under `/opt/homebrew`, run by a launchd job the driver writes and owns. |

The user experience is identical on all three: same tabs, same actions, same
API. Anything else is refused. **Intel macOS** is refused whatever the `os`
setting says, including an explicit `macos`
(`Intel macOS is not supported (Apple silicon only)`), as is any other Linux
or an unknown `os` value. An explicit `os: "macos"` on a non-Mac host is still
accepted (development and tests). A refused module still builds, and every request to
its hostname answers a scoped **503**:

```json
{"error": "module unavailable", "module": "haproxy", "reason": "unsupported haproxy os ..."}
```

Every other module keeps serving. (Setting `"os": "macos"` explicitly skips the
architecture probe; the code does not check the CPU in that case.)

---

## 4. Module settings

The `haproxy` section of the server config (`~/.unified-webapp.json`; see
`unified-webapp-example.json`). A leading `~` in any path is expanded. Path
settings left empty take the driver's default for the detected OS.

| Setting | Default | Meaning |
|---|---|---|
| `static_dir` | `./web/haproxy` | Directory the UI is served from. Must exist and be readable or the module fails to build (503). |
| `data_dir` | `./data/haproxy` | Holds `state.json`, `certs.json` and `staging/`. Created at boot; unset is a build failure. |
| `os` | `auto` | `auto`, `ubuntu`, `rocky`, or `macos` (`darwin` is accepted too). Case-insensitive. |
| `config_path` | Ubuntu/Rocky `/etc/haproxy/haproxy.cfg`; macOS `/opt/homebrew/etc/haproxy.cfg` | The live config the module owns. |
| `certs_dir` | Ubuntu/Rocky `/etc/haproxy/certs`; macOS `/opt/homebrew/etc/haproxy/certs` | Where certificate files are written. |
| `crt_list_path` | Ubuntu/Rocky `/etc/haproxy/crt-list.txt`; macOS `/opt/homebrew/etc/haproxy/crt-list.txt` | The managed crt-list. |
| `stats_socket_path` | Ubuntu `/run/haproxy/admin.sock`; Rocky `/var/lib/haproxy/stats`; macOS `/opt/homebrew/var/run/haproxy.sock` | The stats socket the generated config creates and the Stats tab reads. |
| `service_name` | Ubuntu/Rocky `haproxy`; macOS `net.cmdhome.unified.haproxy` | The systemd unit, or the launchd job label. |
| `backup_keep` | `10` | Timestamped backups kept per kind (config, crt-list). Zero or negative means 10. |
| `expiry_warn_days` | `30` | A certificate expiring within this many days is flagged "expires soon". Zero or negative means 30. |
| `certmachine.url` | empty | CertMachine base URL. Empty means CertMachine is not configured (the certificate routes answer 409). |
| `certmachine.api_key` | empty | The API key, sent as `Authorization: Bearer <key>`. A secret; it is redacted wherever the config struct is printed and never appears in an API response, error or log. |
| `certmachine.ca_file` | empty | PEM file of an extra root to trust when talking to CertMachine. Added to the system roots. |

The backup directory is not a setting: it is `/etc/haproxy/backups` on Linux
and `/opt/homebrew/etc/haproxy/backups` on macOS.

**The config, certs and backups directories are created on first write.** If
the directory a file is going into is missing, the driver creates it (through
sudo on Linux) before the write: mode `0750` for the certs directory and `0755`
for the config and backups directories on Linux, owned `root:haproxy`; `0700`
and `0755` on macOS, owned by the running user. An existing directory is never
touched. You can still create them yourself, for example
`sudo mkdir -p /etc/haproxy/certs /etc/haproxy/backups`.

Route the hostname in `host_routing` and, optionally, protect it
([section 12](#12-authentication)):

```json
"host_routing": { "haproxy.cmdhome.net": "haproxy" },
"auth": { "modules": { "haproxy": { "pin_file": "/etc/unified-webapp/haproxy.pin" } } }
```

---

## 5. Privileges

### Linux (Ubuntu and Rocky)

Every file operation and service action runs through `sudo -n` (non-interactive:
a missing sudoers rule fails immediately instead of waiting on a password).
File contents travel over stdin, never on a command line. Writes are atomic: a
temporary file `<target>.tmp-<12 hex>` is created `0600`, filled, `chown`ed
`root:haproxy`, `chmod`ed (`0644` for config, crt-list and backups; `0640` for
certificate files) and renamed over the target.

The exact commands, derived from the driver:

| Purpose | Command run through sudo |
|---|---|
| Read a file | `cat <path>` |
| Check the directory exists | `test -d <dir>` |
| Create a missing directory | `mkdir -p -m 0750\|0755 <dir>`, then `chown root:haproxy <dir>` |
| Create temp file | `install -m 0600 /dev/null <target>.tmp-<hex>` |
| Fill temp file | `tee <target>.tmp-<hex>` |
| Set owner | `chown root:haproxy <target>.tmp-<hex>` |
| Set mode | `chmod 0644\|0640 <target>.tmp-<hex>` |
| Install | `mv -f <target>.tmp-<hex> <target>` |
| Delete (superseded cert, pruned backup, rollback) | `rm -f <path>` |
| List a directory (certs, backups) | `find <dir> -maxdepth 1 -type f -printf '%p\t%s\t%T@\n'` |
| Validate | `haproxy -c -f <data_dir>/staging/haproxy-stage-*/haproxy.cfg` |
| Reload / restart / start | `systemctl reload\|restart\|start haproxy` |
| Status | `systemctl is-active haproxy` |

Not through sudo: `haproxy -v` (version) and the stats socket (the generated
`stats socket` line hands the socket to the app user at `level user`, so the
module connects directly).

A starting point for `/etc/sudoers.d/haproxy-editor`. Replace `cdelezenski`
with the service user, replace `haproxy` in the `systemctl` lines if you changed
`service_name`, adjust the paths if you changed them in the settings, confirm
each binary with `command -v`, and edit with `visudo -f`:

```
# haproxy editor: read/write the config, crt-list, certs and backups; validate;
# reload/restart/start haproxy. Nothing else.
Cmnd_Alias HAPROXY_FILES = \
  /usr/bin/test -d /etc/haproxy, \
  /usr/bin/test -d /etc/haproxy/certs, \
  /usr/bin/test -d /etc/haproxy/backups, \
  /usr/bin/mkdir -p -m 0755 /etc/haproxy, \
  /usr/bin/mkdir -p -m 0750 /etc/haproxy/certs, \
  /usr/bin/mkdir -p -m 0755 /etc/haproxy/backups, \
  /usr/bin/chown root\:haproxy /etc/haproxy, \
  /usr/bin/chown root\:haproxy /etc/haproxy/certs, \
  /usr/bin/chown root\:haproxy /etc/haproxy/backups, \
  /usr/bin/cat /etc/haproxy/haproxy.cfg, \
  /usr/bin/cat /etc/haproxy/crt-list.txt, \
  /usr/bin/cat /etc/haproxy/backups/*, \
  /usr/bin/find /etc/haproxy/certs -maxdepth 1 -type f -printf %p\\t%s\\t%T@\\n, \
  /usr/bin/find /etc/haproxy/backups -maxdepth 1 -type f -printf %p\\t%s\\t%T@\\n, \
  /usr/bin/install -m 0600 /dev/null /etc/haproxy/haproxy.cfg.tmp-*, \
  /usr/bin/install -m 0600 /dev/null /etc/haproxy/crt-list.txt.tmp-*, \
  /usr/bin/install -m 0600 /dev/null /etc/haproxy/certs/*.tmp-*, \
  /usr/bin/install -m 0600 /dev/null /etc/haproxy/backups/*.tmp-*, \
  /usr/bin/tee /etc/haproxy/haproxy.cfg.tmp-*, \
  /usr/bin/tee /etc/haproxy/crt-list.txt.tmp-*, \
  /usr/bin/tee /etc/haproxy/certs/*.tmp-*, \
  /usr/bin/tee /etc/haproxy/backups/*.tmp-*, \
  /usr/bin/chown root\:haproxy /etc/haproxy/haproxy.cfg.tmp-*, \
  /usr/bin/chown root\:haproxy /etc/haproxy/crt-list.txt.tmp-*, \
  /usr/bin/chown root\:haproxy /etc/haproxy/certs/*.tmp-*, \
  /usr/bin/chown root\:haproxy /etc/haproxy/backups/*.tmp-*, \
  /usr/bin/chmod 0644 /etc/haproxy/haproxy.cfg.tmp-*, \
  /usr/bin/chmod 0644 /etc/haproxy/crt-list.txt.tmp-*, \
  /usr/bin/chmod 0640 /etc/haproxy/certs/*.tmp-*, \
  /usr/bin/chmod 0644 /etc/haproxy/backups/*.tmp-*, \
  /usr/bin/mv -f /etc/haproxy/haproxy.cfg.tmp-* /etc/haproxy/haproxy.cfg, \
  /usr/bin/mv -f /etc/haproxy/crt-list.txt.tmp-* /etc/haproxy/crt-list.txt, \
  /usr/bin/mv -f /etc/haproxy/certs/*.tmp-* /etc/haproxy/certs/*.pem, \
  /usr/bin/mv -f /etc/haproxy/backups/*.tmp-* /etc/haproxy/backups/*, \
  /usr/bin/rm -f /etc/haproxy/haproxy.cfg, \
  /usr/bin/rm -f /etc/haproxy/crt-list.txt, \
  /usr/bin/rm -f /etc/haproxy/*.tmp-*, \
  /usr/bin/rm -f /etc/haproxy/certs/*, \
  /usr/bin/rm -f /etc/haproxy/backups/*

Cmnd_Alias HAPROXY_SERVICE = \
  /usr/bin/systemctl reload haproxy, \
  /usr/bin/systemctl restart haproxy, \
  /usr/bin/systemctl start haproxy, \
  /usr/bin/systemctl is-active haproxy, \
  /usr/sbin/haproxy -c -f /opt/unified-webapp/data/haproxy/staging/*/haproxy.cfg

cdelezenski ALL=(root) NOPASSWD: HAPROXY_FILES, HAPROXY_SERVICE
```

Notes on this file:

- **Wildcards.** A `*` in a sudoers argument matches any characters, including
  `/` and `..`, so a wildcard path grant can be steered outside its directory.
  The editor only passes paths it built from a validated name (no separators,
  no `..`) under the configured directory, but a grant is only as tight as its
  pattern. Exact-path grants are preferred, and the config, crt-list, `systemctl`
  and `cat` lines above are exact. The wildcards that remain exist because the
  temporary-file suffix is random and certificate and backup names are chosen
  at run time. If you want them tighter, narrow the patterns to the shape
  `<name>-<12 hex>.pem` and to `20060102T150405.000000000Z` timestamps.
- **`<data_dir>/staging`.** The validation line must name your real `data_dir`
  (the example assumes `/opt/unified-webapp/data/haproxy`). The staged files
  are owned by the app user at mode `0600`; root can read them.
- **Rollback.** `rm -f` of the config and crt-list is only used when a failed
  Apply must remove a file that did not exist before it.
- **Check it.** After installing, run `sudo -n -l` as the service user and
  exercise the module once; a denied command shows up in the Log tab with the
  exact command line that failed.

### macOS

The driver runs everything as the logged-in user: no `sudo`, no `chown`, files
`0644` (config, crt-list, backups) and `0600` (certificates). The launchd job is
a per-user LaunchAgent bootstrapped into the `gui/<uid>` domain, so **no sudoers
entries are needed** unless you deliberately run that job as root, which the
driver does not do. The files the driver touches must be writable by that user:
make the Homebrew `etc` and `var` paths above writable for the account the
service runs as. A non-root process can bind `0.0.0.0:443` on this macOS
version (verified on the owner's Mac for the FRD), which is why the generated
config always uses a wildcard bind.

---

## 6. CertMachine setup

The editor fetches certificates from a CertMachine instance with an API key,
using the existing mechanism
([README, API keys for automation](../../README.md#api-keys-for-automation)):

1. Generate a key: `go run ./cmd/server -gen-api-key`. It prints the key once
   and its `sha256:...` hash.
2. Paste the hash into the CertMachine instance's `auth.api_keys`.
3. Put the key itself in `haproxy.certmachine.api_key`; it is sent as
   `Authorization: Bearer <key>`.

Any key in `auth.api_keys` works. A dedicated key for the haproxy editor is
recommended but not enforced.

Then set `haproxy.certmachine.url` (and, if CertMachine uses a private CA,
`haproxy.certmachine.ca_file`).

- **HTTPS is required for any non-loopback address.** Plain `http://` is
  accepted only for `localhost` or a loopback IP, because a bundle carries a
  private key. A plain-http URL elsewhere, or a URL scheme other than http or
  https, fails the module's build at boot (scoped 503 with the reason).
- **`ca_file`** adds a root on top of the system roots, for example CertMachine's
  own CA when it runs on another machine. An unreadable file or one with no
  usable certificate also fails the build.
- **The API key is never in an API response, an error message or a log line.**
  The status endpoint only reports `certmachineConfigured: true|false`.
- **Integrity check.** Every download is verified before a byte is written:
  the SHA-256 of the body must equal the `ETag` (`"sha256-<hex>"`), `X-Cert-Id`
  must be the id asked for, and `X-Cert-Fingerprint` must equal the fingerprint
  CertMachine listed. Any mismatch is a 502 and nothing is written. **A download
  with no `ETag` is reported as "this CertMachine needs updating"**: it means
  the CertMachine in front of you predates the download-integrity extension
  and must be upgraded; the editor does not skip the check. The extension
  (list filters and the three headers) is described in
  [certmachine.md](certmachine.md#download-integrity-headers-and-api-key-access).

---

## 7. The model

The model is what you edit; it is stored in `state.json` and rendered to
`haproxy.cfg` on Apply.

- **Globals and defaults.** Ordered key/value lists. Each OS driver contributes
  baseline `global` directives (on Linux `log`, `chroot /var/lib/haproxy`, the
  stats socket, `user haproxy`, `group haproxy`; on macOS `log`, the stats
  socket and a `pidfile`). The UI marks them **OS-managed**. A row you add with
  the same key overrides the driver's value (except repeatable keys such as
  `log`). The generated `stats socket` line is always rendered by the module at
  `level user` (read-only) and is authoritative: the stats socket is always
  read-only, and adding a `stats socket` directive on the Globals page is
  rejected (Apply returns 409 with that issue).
- **Services.** Each service has a name, an enabled toggle, one or more FQDNs,
  an exposed port, an upstream (host and port), an optional health check, a
  certificate (by FQDN, linked to a CertMachine cert) and optional extra
  directives for its backend. The **upstream host defaults to `127.0.0.1`**
  (the UI placeholder and default). A disabled service is left out of the
  generated config.
- **Exposed ports.** Port 443 is always present. Further TLS ports can be
  added, each with an optional default service for traffic that matches no
  host rule. Each port becomes a `frontend fe_<port>` with
  `bind *:<port> ssl crt-list <crt-list path>`, `option httplog` and
  `option forwardfor`. Routing is by `hdr(host)` against each service's FQDNs.
- **The default service.** Unmatched `:443` traffic goes to the default
  service. A fresh model names it **`unified`** and points it at
  `127.0.0.1:8787` with a health check: that is this web server. Port 443
  cannot be listed as an extra port.
- **Raw sections.** Anything in an imported file the model does not map
  (for example `frontend stats`) is kept verbatim and written back. The Raw tab
  shows exactly what Apply would write; it is read-only.
- **Import.** The first time you open the module with no stored model, you are
  offered **Import live config** (read through the driver, previewed with a
  report of raw sections kept, legacy per-bind `crt` arguments, and unmapped
  lines, and stored only when you confirm) or **Start with defaults** (offered
  when there is no live config to import).
- **Checks before Apply.** The stored model is checked for: duplicate service
  names, names that are not safe HAProxy identifiers (lowercase letters, digits,
  `-`, `_`), invalid ports, the same FQDN claimed twice, a port listed twice,
  port 443 listed as an extra port, a port default that is not one of its
  services, and a service naming a certificate with **no tracked row**.
  These are **errors and block Apply** (409). A service naming a certificate
  that is **disabled** is a **warning** and does not block.

The tabs: **Services**, **Globals**, **Certificates**, **Backups**, **Raw**,
**Log** (the module's operations log, streamed live) and **Stats**. A bar above
them shows pending changes, **Review changes** (diffs of `haproxy.cfg` and the
crt-list), **Check**, **Apply changes**, the service status (active, detail,
HAProxy version, last apply) and **Reload / Restart / Start**.

---

## 8. Apply, and what each outcome means

Nothing is live until Apply, and Apply does nothing when nothing changed. The
order is:

1. **Stage and validate.** The candidate config and crt-list are written to a
   fresh directory under `<data_dir>/staging` and checked with the real
   `haproxy -c -f`. The live files are not touched. **Check** runs just this
   step.
2. **Back up** the live config and crt-list ([section 9](#9-backups-and-restore)),
   then prune to `backup_keep`.
3. **Install the crt-list, then the config**, each atomically.
4. **Reload** gracefully.
5. **Verify.** The service must be active and the stats socket must answer
   `show info` with a version. If the socket is not available yet (first run,
   macOS before the job is bootstrapped), it falls back to the active check
   alone and says so in the Log.
6. **Roll back** on any failure after the first install: the previous files are
   restored (or removed if they did not exist) and HAProxy is reloaded again.
   A rollback never deletes a certificate file.
7. **Clean up** (success only): certificate files superseded by an update are
   removed.

Apply and restore answer HTTP 200 for these outcomes, in `outcome`:

| `outcome` | Meaning |
|---|---|
| `applied` | Installed, reloaded and verified. The new config is live. |
| `no_changes` | Nothing differed from the live files; nothing was done. |
| `validation_failed` | `haproxy -c` rejected the candidate. The live files were never touched. The message carries HAProxy's own text. |
| `rolled_back` | A step after validation failed (installing the crt-list or the config, reload, or verify) and the previous config is live again. `rolledBack` is `true`. |

`rollback_failed` is the one outcome that is an error: the rollback itself
failed, the live state may be inconsistent, and the API answers **HTTP 500**
with the message. Check the Log tab and the Backups tab and restore by hand if
needed. An infrastructure failure (for example the backup directory is missing
or unreadable) is also a 500.

An error-severity check issue ([section 7](#7-the-model)) refuses Apply with a
**409** and the issue list before any of the above starts. Coverage warnings
never block.

---

## 9. Backups and restore

Before every install, Apply writes timestamped copies of the live files into
the backup directory: `haproxy.cfg.<UTC timestamp>` and
`crt-list.txt.<UTC timestamp>`.

- **First adoption.** The first time a backup is taken (the backup directory is
  empty), a second copy named `haproxy.cfg.orig-<timestamp>` (and likewise for
  the crt-list) is written too. **`.orig-` backups are never pruned**, so your
  original is always recoverable.
- **Keep-N.** Only the newest `backup_keep` timestamped backups of each kind
  are kept.
- **Restore** (Backups tab, or `POST /api/backups/{name}/restore`) goes through
  the same validate, back up, install, reload, verify and rollback pipeline as
  Apply. Restoring a config backup also restores the crt-list backup with the
  same timestamp when one exists; otherwise the live crt-list is kept.
  Restoring a crt-list backup keeps the live config. A restore is itself backed
  up first.
- **View.** A backup's content can be viewed from the Backups tab.

---

## 10. Certificate lifecycle

**Pull.** On the Certificates tab, **+ Add from CertMachine** (or **Pick from
CertMachine** on a service) lists CertMachine's active certificates, marking
which cover the service's FQDNs using CertMachine's own SAN list. By default
only covering certificates are shown; "show all" lists any status, flagged.
Pulling downloads `haproxy.pem`, verifies it ([section 6](#6-certmachine-setup)),
and writes it with the driver's mode (`0640 root:haproxy` on Linux, `0600` on
macOS). Only an **active** CertMachine certificate can be installed (409
otherwise). The pull is **pending until you Apply**: the file is on disk and
tracked, but the crt-list only references it once Apply installs the crt-list.

**File naming.** `<safe-fqdn>-<first 12 hex of the bundle's SHA-256>.pem`, where
`*.` becomes `_wildcard.` and unsafe characters become `_`. A name identifies
its contents, so a changed bundle is a **new file**; nothing is overwritten in
place. A file in the directory that does not follow this convention is never
touched, only counted ("N unmanaged file(s) not tracked here").

**Tracking.** `certs.json` records name, enabled, note, the CertMachine id and
FQDN, and the SHA-256. The crt-list holds one absolute path per **enabled**
row, sorted by name.

**Update available.** The tab compares the CertMachine id recorded for each
enabled certificate with the id of CertMachine's current **active** certificate
for the same FQDN (one list call, no key downloaded). Statuses: `up to date`,
`update available`, `no active cert`, and `unknown` (CertMachine unreachable).
**Update** pulls the new bundle: the new file is enabled, the old same-FQDN row
is disabled and marked superseded so the crt-list switches to the new file only,
and **the old file is removed only after a successful Apply**. A certificate you
disabled on purpose is left alone. Re-issuing a certificate in CertMachine (for
example with an extra SAN) is what produces an update; the editor never
changes SANs.

**Disable and enable.** A toggle on the row. Disabling leaves the file and row in
place and drops the line from the crt-list; enabling puts it back. Takes effect
at the next Apply.

**Remove.** Deletes the file and the row. Refused with **409** while the
certificate is enabled and a service names its FQDN, and also refused while
the live crt-list (what HAProxy loads on its next restart or host reboot) still
references it, so disable the certificate and Apply first, then Remove. A
disabled certificate the live crt-list no longer lists may be removed.

While a certificate is in use its **Remove button is grayed out** and the
reason is shown next to it (and as the button tooltip). The reasons are:

- "Listed in the live configuration. Disable it, then Apply, before removing it."
- "Enabled and used by service <name>."
- "Could not read the live configuration." (the safe default when the live
  crt-list cannot be read; the button stays disabled)

The button re-enables after the next successful Apply once the certificate is
no longer listed. The server still refuses with 409 as a backstop, for example
from a stale page.

**Details and expiry.** The details panel shows FQDN, SANs, issuer (when
CertMachine supplies it), validity window and status, all fetched live from
CertMachine; if CertMachine is unreachable the row says details are
unavailable. Badges: `valid`, `expires soon` (within `expiry_warn_days`),
`expired`, and `missing` when the tracked file is not on disk.

**Checks and warnings.**

- A service that names a certificate with **no tracked row blocks Apply**.
- A certificate that is disabled, or that does not cover a service's FQDN
  (coverage), is a **warning only**. HAProxy would serve the first certificate
  in the list for an uncovered name and browsers would show a warning, which is
  what coverage is there to catch. Coverage is checked against CertMachine's
  current SANs; with CertMachine down it shows `unknown` rather than failing,
  and Apply still works.

---

## 11. The Stats tab

Read-only live statistics: HAProxy version, uptime, current connections, and one
row per frontend, backend, server and listener (status, current sessions,
session rate, bytes in and out, last health check). The tab refreshes about
every five seconds while visible, and has a **Refresh** button.

The module only ever sends `show stat` and `show info` on the stats socket; any
other command is refused before it touches the socket. The generated `stats
socket` line is `<path> mode 660 level user user <app user>` on Linux and
`<path> mode 600 level user` on macOS, so the socket is read-only for the app
and needs no sudo. If the socket is missing, refuses, denies permission or does
not answer, the API still answers 200 with `available: false` and a plain
message ("HAProxy statistics are not available: ... HAProxy may not have been
applied yet, or it is stopped.").

---

## 12. Authentication

**Authentication is encouraged, not required.** The module works with no `auth`
entry and then has **no login at all**: anyone who can reach the hostname can
rewrite the proxy that fronts everything else, and restart it. Treat that as
root on the network edge. Do not leave it open on a network you do not fully
trust.

Protect it by listing it in `auth.modules` (a door-code PIN file, or any other
method the platform offers; see
[README, Authentication](../../README.md#authentication-optional)):

```json
"auth": { "modules": { "haproxy": { "pin_file": "/etc/unified-webapp/haproxy.pin" } } }
```

It has **its own login, session and timeout**. A login on another module grants
nothing here: sessions are scoped to the one module they were made on and the
gate refuses them everywhere else. A valid API key is accepted like on any
protected module.

---

## 13. Per-OS notes

### macOS

- **The launchd job.** The driver writes
  `~/Library/LaunchAgents/net.cmdhome.unified.haproxy.plist` and owns it. It
  runs `/opt/homebrew/bin/haproxy -W -f <config_path> -p /opt/homebrew/var/run/haproxy.pid`
  with `RunAtLoad` and `KeepAlive`, log to `/opt/homebrew/var/log/haproxy.log`.
  The Homebrew formula supplies the binary only; Homebrew's own service is not
  used. The **Start** button bootstraps the job on first run.
- **`-W`.** HAProxy is started with `-W` (master-worker). The deprecated and, in
  3.5, removed `master-worker` global keyword is never emitted.
- **Wildcard bind.** The generated config always binds `*:<port>`; a non-root
  process on macOS can bind the wildcard address but not a specific one.
- **Raised file limit.** The job sets `NumberOfFiles` (soft and hard) to 65536.
  Without it HAProxy auto-lowers `maxconn` to 100.
- **Reload and restart.** Reload sends `SIGUSR2` to the master process named in
  the pidfile; restart is `launchctl kickstart -k`.
- **Baseline `global`.** `log stdout format raw local0`, the stats socket and
  the `pidfile`; no `user`, `group` or `chroot`.
- **Not tested.** Whether macOS local-network privacy needs to allow HAProxy to
  reach LAN backends is unknown; backends on `127.0.0.1` should not be
  affected.

### Rocky Linux (SELinux)

On Rocky, backends on non-standard ports need the SELinux boolean
`haproxy_connect_any`, and certificate files need a readable context. **The
editor never changes SELinux; it only reports it.** The Rocky driver reads
`getenforce` and `getsebool haproxy_connect_any` (read-only). When SELinux is
enforcing and the boolean is off, the status panel shows a notice and
`GET /api/status` lists it under `diagnostics`; nothing is shown on a healthy
system. If HAProxy still cannot reach a backend after an Apply on Rocky, check
the audit log yourself.

### Ubuntu and Rocky

The Linux baseline `global` directives are `log /dev/log local0`,
`chroot /var/lib/haproxy`, the stats socket, `user haproxy` and `group haproxy`.

---

## 14. Putting it behind a proxy

Same two rules as every module, and they matter because `/api/ops/stream` is a
Server-Sent-Events endpoint (capped at `server.sse_max_subscribers`; past that it
answers 503 "too many log streams"):

- **Preserve the `Host` header.** The dispatcher routes on it.
- **Do not buffer responses.** A proxy that buffers SSE turns the Log tab into a
  page that never fills. For nginx: `proxy_buffering off;` and a high
  `proxy_read_timeout`. HAProxy passes SSE through by default.

---

## 15. Quick start

1. Install the sudoers file ([section 5](#5-privileges)) on Linux. On macOS,
   install Homebrew HAProxy. The directories are created on first write.
2. Add the hostname to `host_routing`:

   ```json
   "haproxy-test.cmdhome.net": "haproxy",
   "haproxy.cmdhome.net":      "haproxy"
   ```

3. Fill in the `haproxy` section. The minimum:

   ```json
   "haproxy": {
     "static_dir": "./web/haproxy",
     "data_dir": "./data/haproxy",
     "certmachine": {
       "url": "https://certmachine.cmdhome.net",
       "api_key": "<the key from -gen-api-key>"
     }
   }
   ```

4. Protect the hostname ([section 12](#12-authentication)), point DNS at the
   box and restart `unified-webapp`. The boot log prints one `registered` line
   per hostname.
5. **Back up your original `haproxy.cfg` by hand.**
6. Load the page. Choose **Import live config**, review the report, **Confirm
   import**. Check the Services and Globals tabs, then look at **Review
   changes**: the first diff will show your comments disappearing and the
   `# managed by` header appearing. Press **Check**, then **Apply changes**.
7. Add a service (FQDN, exposed port, upstream), **Pick from CertMachine** for its
   certificate, **Save**, then **Apply changes**. Open **Stats** and confirm the
   server is UP.

---

## 16. When something breaks

| Symptom | Cause and fix |
|---|---|
| The haproxy hostname answers **503** with `module unavailable` and a `reason` | Unsupported OS (Intel macOS, whatever the `os` setting, a Linux other than the supported families, or a bad `os` value). Set `os` explicitly or run on a supported platform. |
| 503 with another message, everything else works | The module failed to build at boot: missing `static_dir`, unset `data_dir`, a corrupt `state.json`, or a bad CertMachine `url` or `ca_file`. The 503 body and the boot log name it. |
| Apply says **validation failed** | `haproxy -c` rejected the candidate; the message is HAProxy's own. Nothing live changed. Fix the model (often a raw section or an extra directive) and Check again. |
| Apply says **rolled back** | A post-validation step failed (install, reload or verify). The previous config is live. Read the message and the Log tab; on Linux a denied `sudo` command is the usual cause. |
| **rollback failed** (HTTP 500) | Both the step and the rollback failed. Inspect the files and use the Backups tab or restore by hand from the backup directory. |
| Apply or pull fails with a backup, list or write error | The sudoers grant is missing (it must include the directory commands, so the first write can create the directories) ([section 4](#4-module-settings), [section 5](#5-privileges)). |
| Apply refused with 409 and a list of issues | An error-severity check ([section 7](#7-the-model)); the list says which service or port. |
| Certificate routes answer 409 "CertMachine is not configured" | `haproxy.certmachine.url` is empty. |
| Pull or details say **CertMachine is unreachable** (502) | Network, DNS or CertMachine is down. |
| **TLS error** (502) talking to CertMachine | CertMachine's certificate is not trusted. Set `certmachine.ca_file` to its CA. |
| **CertMachine rejected the API key** (502) | The key is wrong or its hash is not in CertMachine's `auth.api_keys`. |
| **Integrity check failed** (502) | The body hash did not match the ETag, or `X-Cert-Id` or `X-Cert-Fingerprint` did not match. Nothing was written. Retry; if it persists, investigate the path to CertMachine. |
| "**this CertMachine needs updating**" | The download carried no `ETag`: CertMachine lacks the integrity extension. Upgrade it ([certmachine.md](certmachine.md#download-integrity-headers-and-api-key-access)). |
| Stats tab says "statistics are not available" | The stats socket is missing, refused, denied or timed out. HAProxy may not have been applied yet or is stopped; Apply once, or use Start. Check `stats_socket_path` and that HAProxy created it. |
| Log tab empty | A proxy is buffering SSE ([section 14](#14-putting-it-behind-a-proxy)), or the stream cap was reached (503). |
| A backend is unreachable on Rocky after Apply | Check SELinux `haproxy_connect_any` ([section 13](#13-per-os-notes)). |

---

## 17. Owner acceptance checklist

This guide, the Go code and its tests were written without the owner's hands on
a real system. **Three items are PENDING OWNER SIGN-OFF and are not done.**

- [ ] **A-OG: curl acceptance of the CertMachine extension** (FRD section 13.5),
  against a real CertMachine with an API key: the filtered list returns the one
  active certificate; a bundle download carries `ETag`, `X-Cert-Id` and
  `X-Cert-Fingerprint` and `sha256sum` of the body equals the ETag; repeating with
  `If-None-Match` gives 304; `HEAD` returns headers only; an Edit that re-issues
  with new SANs makes the filtered list return the new id.
- [ ] **B-OG: browser sign-off of the full flow**, repeated on the owner's Tahoe
  Mac and on real Linux, confirming identical look and behaviour: import the
  existing config and review the generated file; add a service (new FQDN and
  local port) and install its certificate from CertMachine; Apply; add a second
  exposed port with several services; disable a certificate and see it stop
  being served after Apply while still listed; make a deliberately broken edit
  and see Check and Apply refuse it with the old config still live; see the
  pending bar appear and clear with no CLI step; open Stats and see servers
  UP/DOWN. On the Mac, confirm the launchd job and file limit are "set and
  forget".
- [ ] **B5b: swap the client from the fake to the real CertMachine endpoint** and
  re-verify. The client's integrity logic has only ever run against a fake
  that replays the byte-level vectors; B5b re-runs it against a real
  CertMachine after A-OG passes.

**What has and has not been tested.** Against a **real HAProxy** and a **real
CertMachine**: nothing in this repository's tests. The module's behaviour has
been exercised through unit tests over an in-package fake driver and fake
command layer, handler tests, a fake CertMachine server, and web unit and type
tests. The only real-system evidence is the set of macOS experiments recorded in
FRD section 4 (owner's Tahoe Mac, macOS 26.5, arm64, HAProxy 3.4.6, 2026-09-30,
a throwaway instance): `log stdout format raw local0` and `pidfile` validate;
master-worker mode with `SIGUSR2` reloads gracefully; the `master-worker` keyword
is removed in 3.5 so `-W` is used; without a raised file limit `maxconn` drops to
100; a non-root process can bind `0.0.0.0:443`.

**Not browser-tested.** The UI has not been driven in a real browser by the
agent that built it. Not exercised anywhere: the real sudoers set on Ubuntu or
Rocky, real `systemctl reload` and a real rollback after a failed reload, the
stats-socket path and ownership on real Linux, SELinux behaviour on Rocky, the
macOS launchd bootstrap by this driver, and CertMachine downloads over a real
network. The sudoers file in section 5 is derived from reading the driver code,
not from running it under `sudo`.

---

## 18. HTTP API

All under the haproxy hostname, behind the platform auth gate (nothing here
re-checks auth). JSON unless noted. A method not listed for a path answers 405
with an `Allow` header. Error bodies are `{"error": "<message>"}`. No response
carries key material or the CertMachine API key.

| Method and path | What it does |
|---|---|
| `GET /api/status` | `{module, moduleVersion, service:{active,detail}, version, lastApply:{time,outcome,message} or null, pending, certmachineConfigured, diagnostics:[string]}` |
| `GET /api/model` | The model plus `imported` (false until a model is stored; defaults when there is no `state.json`) |
| `PUT /api/model` | Replace the stored model. 400 with a message when structurally invalid; body limited to 1 MiB |
| `GET /api/model/check` | `{issues:[{severity:"error" or "warning", where, message}]}` from the stored model and tracked certificates |
| `POST /api/import` `{commit}` | Read the live config and import it, returning `{model, report}`; stored only when `commit` is true. 404 when there is no live config; 422 when it cannot be parsed |
| `GET /api/changes` | `{hasChanges, configDiff, crtListDiff, summary}` |
| `GET /api/raw` | `{config, crtList}`: exactly what Apply would write |
| `POST /api/check` | `{ok, message}`: `haproxy -c` on the staged candidate |
| `POST /api/apply` | `{applied, rolledBack, outcome, message}`. 409 `{error, issues}` when any error-severity issue exists (warnings never block) |
| `POST /api/reload`, `POST /api/restart`, `POST /api/start` | `{ok:true}`; 500 on failure |
| `GET /api/backups` | List of backups (`name`, `kind`, `timestamp`, `orig`) |
| `GET /api/backups/{name}` | `{name, content}`. 400 for a bad name, 404 for no such backup |
| `POST /api/backups/{name}/restore` | Same result shape as apply. 400 / 404 as above |
| `GET /api/stats` | `{available, message, info:{version,uptimeSec,currConns,pid}, rows:[...]}`. Read-only; a missing or unreachable socket is 200 with `available:false` |
| `GET /api/ops` | Snapshot of the operations log |
| `GET /api/ops/stream` | SSE of operations-log entries; 503 past `sse_max_subscribers` |
| `GET /api/certs` | `{certs:[tracking row, missing, details or detailsError, removable, inUseReason], unmanaged}`; `removable` is false while the cert is in use and `inUseReason` says why (empty when removable) |
| `GET /api/certmachine/certs` | `?forFqdn=<name>[&all=1]`: CertMachine's active certificates with `covers`; only covering ones unless `all=1` |
| `POST /api/certs/pull` `{certmachineId, note}` | Verified download and stage; returns `{name, superseded}` |
| `PUT /api/certs/{name}/enabled` `{enabled}` | Toggle the tracking flag only |
| `DELETE /api/certs/{name}` | Remove the file and row; 409 while a service uses it or the live crt-list references it |
| `GET /api/coverage` | Per-service coverage, warnings only; `unknown` when CertMachine is down |
| `GET /api/certs/freshness` | `[{name, fqdn, status}]` where status is `up to date`, `update available`, `no active cert` or `unknown` |

**Status mapping.** Apply and restore answer **200** for `applied`,
`no_changes`, `validation_failed` and `rolled_back` (the body carries `outcome`
and `message`), and **500** `{"error"}` only for `rollback_failed` or an
infrastructure failure. The CertMachine routes answer **409** "CertMachine is
not configured" when no `certmachine.url` is set. Pull failures: unreachable,
TLS, unauthorized, integrity and server errors are **502**; a CertMachine
refusal is **409** with CertMachine's own message.
