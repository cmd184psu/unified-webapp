# FRD: haproxy editor module

Status: **Approved** by the owner on 2026-09-30 (draft v7; nothing is built yet). Build order: the CertMachine extension (section 13) first, then the editor.
Module name: `haproxy` (a new module in `unified-webapp`, its own hostname via `host_routing`)
Scope: `internal/haproxy`, `web/haproxy`, config section `haproxy`, `docs/guides/haproxy.md`; **and the CertMachine API extension in section 13** (`internal/certmachine` handlers and tests, `docs/guides/certmachine.md`)
Sibling: `smbedit` (`internal/smbedit`, `docs/guides/smbedit.md`). This module is smbedit's near
twin and may merge with it later (section 10). It must not modify smbedit now.

`CMD>` lines are the owner's and are binding. They are kept verbatim where they apply; the text around
them has been brought into line.

## 1. Goal

Manage the machine's HAProxy from a browser, the way smbedit manages Samba, and **make a change take
effect from the UI**, with no command line:

- a small **collection of global settings** (`global` and `defaults`), like smbedit's globals;
- a list of **services** (the "shares" equivalent): each one maps a **FQDN** on an exposed port
  (normally 443) to a **port on this machine**, with TLS provided by HAProxy. Services are **any**
  local service, not only unified-webapp modules; unified-webapp is just the **default service**;
- the **certificates** HAProxy serves, all **pulled from a CertMachine service** by the editor, named by
  the editor, each one **disable-able** while still tracked. Nobody uploads a cert;
- a read-only **Stats tab** to see how HAProxy is faring.

The behaviour and the user experience are **identical on Ubuntu, Rocky Linux 10 and macOS Tahoe
(Apple silicon)**; the user cannot tell which one they are touching (D13).

Why: today hero's `haproxy.cfg` is hand-edited as root: a `global`, a `defaults`, a stats listener, one
443 frontend with host ACLs routing a few services, a second frontend on 8443, certs dropped into
`/etc/haproxy/certs` by hand. A wrong edit or mis-named cert means an outage or a silently un-served
hostname. The editor validates before it applies, keeps backups, and shows what each certificate covers.

CMD> it should be similar to smbedit.  There's a collection of global settings and then there should be a processor of services that match up with a particulr FQDN for port 443 (exposed port) and whatever the internal port happens to be on localhost.  haproxy provides TLS for more than just unified-webapp modules.  The default backend should be unified, as it exists today in the haproxy.cfg file.  So use hero's haproxy.cfg as a model.   

## 2. Non-goals (this release)

- Not a general text editor. (A read-only **Raw** view of the file the editor would write is in scope.)
- Not the whole of HAProxy. The model is globals plus services (section 5); other HAProxy features are
  reached only through a per-service **extra directives** escape hatch, or are preserved raw.
- Not a metrics dashboard. The Stats tab is a simple read-only table (section 7), nothing more.
- **No certificate work of any kind.** The editor does not issue, design, edit, parse or inspect certificates, and does not use or depend on `openssl`. CertMachine does all of that. The editor only downloads the HAProxy-format PEM files from CertMachine, verifies them, tracks which are enabled, and references them correctly (section 6).
- **No uploads and no hand-placed certs.** Every cert comes from CertMachine; the editor owns the certs directory and its file names (D16).
- No changes to smbedit and no shared-code refactor now (section 10 records the seams). CertMachine changes **only** by the extension in section 13.
- No multi-host management: it edits the HAProxy on the host the app runs on.
- **No support for hand edits or out-of-turn edits** (D3): the editor owns the file.
- **No Intel Macs** (owner): macOS support is Apple silicon only.

## 3. Decisions

Owner answers of 2026-09-30 are marked **(owner)**.

| # | Decision |
|---|----------|
| D1 | **One OS "driver" per platform** behind one interface (section 4). Everything above it (model, render, validate, apply, UI) is OS-agnostic and tested with a fake driver. |
| D2 | **State on disk in the module's `data_dir`, not the main config.** `state.json` holds the model and `certs.json` a minimal cert tracking list (section 6), both written atomically, mode 0600, like smbedit's `state.json`. The main app config only carries the module's small settings block. **(owner)** |
| D3 | **The editor owns `haproxy.cfg` and the crt-list.** It writes its own generated files and overwrites the existing ones; **comments are not preserved** and hand edits or out-of-turn edits are simply replaced. There is **no drift detection**. The only requirement on the output is that it is **valid** HAProxy config. The owner backs up the original before first use, and the editor also keeps its own backup of whatever it overwrites (D4). **(owner)** |
| D4 | **Nothing touches the live config until Apply,** and **Apply only acts when something changed.** Edits change only `state.json`. The editor compares what it would write against what is live; if they match, Apply says there is **nothing to apply** and does nothing. Otherwise Apply = render, validate, back up, install atomically, reload, verify, roll back on failure (section 7). **(owner)** |
| D5 | **The UI makes changes take effect.** When there are pending changes the UI says so and offers the action that applies them (a **Pending changes** bar with **Review changes** and **Apply changes**, which validates, installs and reloads). Reload, Restart and Start are also buttons. Nothing is ever left for the operator to do on the CLI. **(owner)** |
| D6 | **Validate with HAProxy itself, in a staging directory.** Every Apply and a separate **Check** render the candidate into a staging directory (paths substituted so it sees the candidate crt-list and certs) and run `haproxy -c -f` on it. The editor's own field validation is only for fast feedback. **(owner)** |
| D7 | **A managed `crt-list` file** lists the enabled certs; the TLS bind references it with `crt-list`. A disabled cert stays on disk and in `certs.json` but is left out of the list. Enabling puts it back; nothing is renamed or deleted to disable. **(owner)** |
| D8 | **Private keys are write-only** in the editor's own API and UI: certificate details (from CertMachine) are shown, key material is never returned. |
| D9 | **sudo for all privileged reads and writes.** The service runs as the owner's account, which has passwordless sudo for a documented set of commands (section 8). **(owner)** |
| D10 | **CertMachine is a separate service, even if it is also a module on this host.** The editor is configured with its **URL and an API key** and talks to it only over HTTP, so it works when CertMachine is on another machine. Any API key issued by that instance works; the editor **does not enforce** that it is dedicated. CertMachine's API **is extended** where the editor needs it, and that extension is specified in this FRD (section 13). **(owner)** |
| D11 | **Follow the module conventions** (section 9): login by PIN and/or LDAP is **encouraged but not required**, like any other module; each module has its own login, session and timeout (no shared cookie; a login on another module grants nothing here); ☰ menu on the right (side from config only); boolean settings as toggle switches; roomy text fields; no autosave on keystroke; every outcome a toast. **(owner)** |
| D12 | **Each service has its own certificate, and a certificate may carry SANs for the same service on several systems.** **(owner)** A service names its cert, the editor checks that the service's FQDN(s) are covered by that cert's SANs, and flags any that are not. **Included in this FRD; the owner needs section 6.5's explanation before accepting it.** |
| D13 | **Identical behaviour and UX on every supported OS.** Ubuntu, Rocky Linux 10 and macOS Tahoe on **Apple silicon** (Homebrew `haproxy`) behave the same; the OS-specific detail is hidden entirely behind the driver, and where that needs sudo or extra effort, the driver does it. The user cannot tell which OS they are on. The macOS-specific setup (launchd job, file limit) is one-time and automatic; the owner sees it as set and forget. **(owner)** |
| D14 | **A simple read-only Stats tab,** fed by HAProxy's local stats socket (section 7). Kept deliberately small. **(owner)** |
| D15 | **CertMachine provides every cert detail, over its REST API.** Anything the UI shows about a cert (FQDN, SANs, issuer, validity, status, whether a newer one exists) is fetched from CertMachine. The editor does not parse certs, compute fingerprints, or call `openssl`, and it keeps no copy of those details. It only ensures the HAProxy-format PEM is in place and referenced correctly. If CertMachine's API lacks something the editor needs, **the API is extended** (section 6). **(owner)** |
| D16 | **The editor owns the certs directory and names the files.** Certs are mandated to come from CertMachine, so the editor does not inherit anyone's naming. It names each file from CertMachine's data by a fixed convention (section 6.2). Nobody works with the files by hand; the owner works in the UI. Files that do not follow the convention are never touched. **(owner)** |
| D17 | **Downloads are verified.** Each `haproxy.pem` is pulled from CertMachine and checked with a simple ETag/hash test before it is written, so the editor knows it got exactly what it asked for (section 6.4). This needs a small, additive **CertMachine API extension, which is part of this FRD** (section 13). **(owner)** |

## 4. OS support and the driver

The driver supplies defaults (all overridable in module settings) and the operations that differ. The
UI and API never expose these differences (D13).

CMD> correction: we are not support Intel-based macs; only Apple Silicon

| Concern | Ubuntu | Rocky Linux 10 | macOS Tahoe (Apple silicon, Homebrew) |
|---|---|---|---|
| Config file | `/etc/haproxy/haproxy.cfg` | `/etc/haproxy/haproxy.cfg` | `/opt/homebrew/etc/haproxy.cfg` |
| Certs dir / crt-list | `/etc/haproxy/certs`, `/etc/haproxy/crt-list.txt` | same | `/opt/homebrew/etc/haproxy/certs`, `.../crt-list.txt` |
| Service management | systemd, unit `haproxy` | systemd, unit `haproxy` | a **launchd job the driver writes and owns** running `haproxy -W -f <cfg> -p <pidfile>`, with a raised open-files limit (see below); the Homebrew formula supplies the binary only |
| Reload (graceful) | `systemctl reload haproxy` | `systemctl reload haproxy` | `SIGUSR2` to the master process (master-worker mode via `-W`; verified) |
| Restart / start | `systemctl restart/start haproxy` | same | `launchctl kickstart -k` / `bootstrap` of the driver's job |
| Validate | `haproxy -c -f <file>` | same | same |
| Runs as / file owner | `haproxy` user; certs `0640 root:haproxy` | same | the logged-in user; certs `0600` owner |
| Privilege | passwordless sudo for the listed commands | same, plus SELinux (below) | none needed to bind `*:443` (verified: a non-root process can bind `0.0.0.0:443`, not a specific address, so the driver always emits `bind *:443`); sudo only for anything that needs it |
| Extra checks | none | **SELinux:** backends on non-standard ports need `haproxy_connect_any`; cert files need a readable context. Detected and reported, never changed. | local-network privacy: **not yet tested** (below) |
| Log source | `journalctl -u haproxy` | `journalctl -u haproxy` | the driver's job log |

- **Detection:** `haproxy.os` in settings, defaulting to auto-detect (`/etc/os-release` ID `ubuntu` /
  `rocky`, `uname` = Darwin on `arm64`). Anything else (including Intel macOS) refuses to build the
  module with a clear 503 reason (the smbedit pattern).
- **Rocky 9** behaves like Rocky 10 and is what hero runs today; treated as a supported variant.
- **Driver baseline directives.** Some `global` settings are not portable, so each driver supplies a
  baseline the editor applies to a new model and the UI marks as OS-managed: on Ubuntu and Rocky,
  `user`/`group haproxy`, `chroot /var/lib/haproxy`, a `log` target, and the stats socket path (D14);
  on macOS there is no `/dev/log`, no `haproxy` user and no chroot, so the baseline uses
  `log stdout format raw local0` (verified) and a `pidfile` under `/opt/homebrew/var`. The operator may
  change baseline values, but the driver refuses ones that cannot work on that OS.
- **HAProxy version** differs by OS and release; the driver reports `haproxy -v`. `haproxy -c` is the
  arbiter of validity.
- **Verified on the owner's Tahoe machine (macOS 26.5, arm64, Homebrew `/opt/homebrew`, HAProxy 3.4.6,
  2026-09-30), using a throwaway instance:** `log stdout format raw local0` and `pidfile` validate;
  master-worker mode plus `kill -USR2 <master>` reloads gracefully (master stays up, new worker, listener
  keeps answering); the **`master-worker` global keyword is deprecated in 3.3 and removed in 3.5**, so the
  editor starts HAProxy with `-W` rather than the keyword; without a raised open-files limit HAProxy
  auto-sets `maxconn` to 100, so the driver's launchd job raises it; Homebrew's own service was registered
  but not running and no `haproxy.cfg` existed yet, so first run on a Mac is a bootstrap case the driver
  handles. **Not yet tested:** whether macOS's local-network privacy controls need to allow HAProxy to
  reach LAN backends (everything the owner's services need is on localhost, so this may not matter).

CMD> as long as it is feature parity with the Linux version, I honestly don't care.  The detail should be hidden from the user to the point that they can't tell they are touching haproxy on a mac versus Linux.  It should "just work".  If that means sudo or additional effort.. so be it.  But the user experience between OSs should be identical.

## 5. Data model

The model follows hero's `haproxy.cfg`: a `global`, a `defaults`, and per-hostname routing on a TLS
frontend with a default backend. `state.json` (in `data_dir`), simplified:

```
{
  "version": 1,
  "global":   [ {"key": "maxconn", "value": "2000"}, {"key": "log", "value": "127.0.0.1 local0 notice"}, ... ],
  "defaults": [ {"key": "mode", "value": "http"}, {"key": "timeout server", "value": "1h"}, ... ],
  "defaultService": { "name": "unified", "upstream": {"host": "127.0.0.1", "port": 8787}, "check": true },
  "ports": [ {"port": 8443, "defaultService": "fakes3_ui"} ],      // extra exposed ports; 443 is implicit
  "services": [ {
      "id": "s1",
      "name": "brandx",
      "enabled": true,
      "fqdns": ["brandx.cmdhome.net"],
      "exposedPort": 443,                         // default 443
      "upstream": {"host": "hero.cmdhome.net", "port": 8181},   // host defaults to 127.0.0.1
      "check": true,
      "cert": {"fqdn": "brandx.cmdhome.net"},     // the CertMachine cert (its active one) that serves it (D12)
      "extra": []                                 // escape hatch: raw directives for this service's backend
  } ],
  "rawSections": [ {"header": "frontend stats", "lines": ["bind *:1936", "stats enable", "..."]} ]
}
```

- **Globals** are an ordered key/value list, like smbedit's globals: one group for `global`, one for
  `defaults`, editable as text rows, with a pick-list of common keys for convenience. Anything valid is
  allowed; `haproxy -c` is the check. (This replaces the earlier idea of a typed keyword catalogue.)
- **Services** are what the generator expands into HAProxy. For each exposed port it emits one TLS
  frontend (`bind *:<port> ssl crt-list <path>`); for each enabled service it emits a host ACL matching
  the service's FQDN(s), with or without a port in the `Host` header, a `use_backend` rule, and a
  backend with a single `server <name> <host>:<port> [check]`. Services that share an exposed port share
  a frontend.
- **Exposed ports are first-class.** Port 443 always exists. Adding another exposed port is an explicit,
  occasional action (**Add port**), and a port can then hold **many services** under it, each matched by
  its FQDN (the owner's use case). The UI groups services by exposed port.
- **The default service** is always present: it is the `default_backend` of the 443 frontend, so any host
  no service claims reaches it. It ships as `unified` at `127.0.0.1:8787`, as in hero's config; its port is
  editable, it cannot be deleted. Every other port has its own **default service**, chosen from the
  services under it, or none (HAProxy then answers unmatched hosts with a 503). On import, a port with a
  single service (hero's 8443 frontend) gets that service as its default, so nothing changes.
- The **upstream host** of a service defaults to `127.0.0.1` and is always editable, because it is not
  always local (hero's `brandx` points at `hero.cmdhome.net:8181`).
- A service may be **disabled** (like a smbedit share or a cert): it stays listed and tracked and is left
  out of the generated config.
- **Import** (first run, and on demand) reads the existing `haproxy.cfg` and maps it into this model.
  hero's file maps to: `global`, `defaults`, the `unified` default (`unified_webapp` at 127.0.0.1:8787),
  services `brandx` and `s3-hero` from the host ACLs and `use_backend` rules, a second-port service for the
  8443 frontend, and the `frontend stats` block kept as a **raw section**. **Comments are dropped (D3).**
  Anything that does not fit (other sections, unknown directives) is kept as raw so the result stays
  valid. The result is checked with `haproxy -c` before it is accepted, and the import shows what it could
  not map so nothing is silently lost.
- The generated file starts with a header saying it is managed by the editor and will be overwritten (as
  smbedit's does).
- A **names index** (service names, FQDNs, certs) drives referential checks (FR-H6).

## 6. Certificates

HAProxy expects one PEM bundle per certificate (the certificate, any intermediates, then the private key),
which is exactly CertMachine's `haproxy.pem`. **The editor does not create, edit, parse or inspect certs
(D15), and nobody uploads one.** It downloads the HAProxy-format PEM from CertMachine, verifies it, names the
file, tracks whether it is enabled, and references it in the crt-list.

CMD> probably too much to track the metadata of the cert locally; I question it: necessary?  certmachine should be the source of truth on that.

### 6.1 Tracking file

Everything the UI shows about a cert (FQDN, SANs, issuer, validity, expiry, status) is fetched from
CertMachine over REST. The editor's own tracking file holds only what the editor itself owns:

`certs.json` (in `data_dir`):

```
{
  "version": 1,
  "dir": "/etc/haproxy/certs",
  "certs": [ {
      "name": "brandx.cmdhome.net-3fa91c07b2de.pem",   // the file, named by the convention in 6.2
      "enabled": true,
      "note": "",
      "certmachine": { "id": 12, "fqdn": "brandx.cmdhome.net" },   // where it came from
      "sha256": "3fa91c07b2de..."                      // hash of the exact bytes installed (the verified ETag)
  } ]
}
```

`id` and `fqdn` say where a file came from; `sha256` identifies the exact bytes installed. None of it is a
copy of cert details.

### 6.2 File names (D16)

The editor names every file itself, from CertMachine's data:

`<safe-fqdn>-<first 12 hex of the bundle's SHA-256>.pem`

- `<safe-fqdn>` is the cert's FQDN made filesystem-safe by CertMachine's own rule (a leading `*.` becomes
  `_wildcard.`, anything unsafe becomes `_`).
- The hash part makes a name identify its **contents**: a new or changed bundle is a **new file**, and the
  old file is removed only after the new one is applied and verified. That makes Update and rollback safe
  (the old file is still there until the new config is live) and removes any need to overwrite in place.
- The owner never types or sees these names in daily use; the UI shows the FQDN. The convention can change
  later because nothing outside the editor depends on it.
- The certs directory belongs to the editor. A file that does not follow the convention is **left alone,
  never deleted**, and shown only as a count of unmanaged files.

### 6.3 Pulling a cert for a service

1. On a service, choose its cert (section 6.5). The picker is fed by CertMachine's list.
2. The editor downloads that cert's `haproxy.pem` from CertMachine and **verifies it (6.4)**.
3. It writes the file under the 6.2 name with the OS's ownership and mode, through sudo, and records it in
   `certs.json`.
4. It adds the file to the crt-list; the change is then pending until **Apply** (D4).

### 6.4 Integrity and freshness (D17)

"Did I get exactly what I asked for, and is it still current?" is answered with an ETag and the cert's id.
(The CertMachine side is specified in section 13.)

- **Integrity.** CertMachine serves each `haproxy.pem` with a strong `ETag: "sha256-<hex>"`, the SHA-256 of the
  exact bytes it sends, plus `X-Cert-Id` and `X-Cert-Fingerprint` headers naming which cert the bytes are. The
  editor hashes the bytes it received (a hash over bytes, not any reading of the cert) and requires **all of
  these to agree**: the hash of the bytes equals the `ETag`; `X-Cert-Id` is the id it asked for; and
  `X-Cert-Fingerprint` equals the fingerprint CertMachine's list gave for that cert. On any mismatch, or a
  missing `ETag`, it rejects the download, writes nothing, and shows an error toast.
- **Freshness.** For a bundle, CertMachine builds `haproxy.pem` from the cert's **own** certificate, its **own**
  signing CA and its own key, so the bytes for a given cert id **never change**. A cert is therefore out of
  date exactly when CertMachine's **active** cert for that FQDN has a **different id** (a re-issue, an Edit,
  or a CA replacement all create a new cert row). The editor asks CertMachine for the active cert for the FQDN
  and compares ids; **Update available** means they differ. No private key material is downloaded just to
  find out.
- **Update.** One click downloads and verifies the new bundle, writes the new file, swaps it in the crt-list,
  and is applied with the normal Apply; the old file is deleted afterwards.
- A conditional request (`If-None-Match`, answered 304) and `HEAD` are supported by CertMachine for cheap
  re-checks, but the id comparison is what the editor relies on.

### 6.5 Choosing a cert for a service, and the coverage check (D12): the plan, awaiting owner acceptance

**The problem it prevents.** HAProxy picks a cert from the crt-list by the hostname the browser asked for
(TLS SNI). If no cert in the list covers that hostname, HAProxy does not refuse: it quietly serves the
default (first) cert and the browser shows a certificate warning. That is exactly what happened with
`utuber.hero.cmdhome.net` before its cert had that name as a SAN. The coverage check exists so this is caught
in the editor, before it reaches a browser.

**The plan, in order:**

1. **Each service has a cert field** that points at a CertMachine cert by its FQDN (the active cert for that
   FQDN). That is the "link". It is what lets the editor know which cert is meant to serve which service.
2. **The picker is fed by CertMachine.** It lists CertMachine's active certs and uses CertMachine's own SAN
   list to mark which ones cover the service's FQDN(s). By default it shows only the ones that do. Choosing
   one that does not needs an explicit confirmation.
3. **The check is repeated on every Apply**, using CertMachine's current SANs (one list call; the editor
   still reads nothing from the cert files). Anything not covered appears in the Apply summary and as a badge
   on the service ("cert does not cover utuber.hero.cmdhome.net"). These are **warnings, not blockers**: the
   only things that block Apply are a config HAProxy itself rejects, or a service whose cert is missing.
4. **Several services may share one cert,** which is the "SANs spanning systems" case: one cert lists
   `utuber.cmdhome.net` and `utuber.hero.cmdhome.net`, and both services point at it. The file is written once.
5. **If CertMachine is unreachable,** coverage shows "unknown" instead of failing, and Apply still works.
6. **The fix for a missing name is in CertMachine,** where the cert is edited and re-issued with the extra
   SAN (the Edit dialog fixed this session). The editor then shows **Update available** (6.4) and one click
   pulls the new bundle. The editor never changes SANs itself.

**Why include it now:** earlier it was deferred because some certs would not come from CertMachine. Now every
cert does, so the SANs are always available and the check costs one REST call. **What it does not do:**
parse certs, fix SANs, or block Apply over a coverage warning.

### 6.6 CertMachine integration (D10, D15, D17)

CertMachine is treated as a completely separate service reached by URL, even on the same machine, and as the
only provider of certs and cert information.

- **Settings:** `haproxy.certmachine.url` (for example `https://certmachine.cmdhome.net`), an **API key**, and
  optionally a CA file to trust (CertMachine's root CA) or a documented, off-by-default insecure toggle. The
  key is stored in the module settings, redacted in every response (shown as set/not set, like the LDAP bind
  password) and never logged. It is sent as `Authorization: Bearer <key>`, which CertMachine's platform
  already accepts on any protected module. Any key that instance has issued works; the editor does not check
  or require that it is dedicated or read-only.
- **Transport:** HTTPS is required for a non-local URL, because a bundle includes the private key. Plain HTTP
  is refused unless the URL is loopback.
- **Already in CertMachine and used as is:** `GET /api/certs` (list with FQDN, SANs, status, expiry,
  fingerprint, stale flag), `GET /api/certs/{id}` (one cert), `GET /api/certs/{id}/files/haproxy.pem` (the
  ready-made bundle).
- **CertMachine API extension (in scope, section 13).** The list filters (`fqdn`, `status`) and the
  `ETag` / `X-Cert-Id` / `X-Cert-Fingerprint` / conditional-request support on the file downloads are specified
  and built as part of this work. They are additive: nothing existing changes. Archived, quarantined or
  expired entries are shown by the picker but cannot be chosen without an explicit confirmation, and a
  CertMachine refusal to export a cert (a quarantined cert, or one that does not chain to its CA) is shown
  with CertMachine's own message.
- **Failure handling:** CertMachine unreachable, wrong key, TLS error, a 4xx/5xx, or an integrity failure each
  produce a distinct, visible message; the rest of the module keeps working without it, and certs known only
  by their record show "details unavailable" instead of failing the page.

### 6.7 Enable, disable, remove

- **Disable / enable** (FR-H13, D7): a toggle switch per cert. Disabled = left out of the crt-list; the file
  and tracking row remain, marked disabled. Applying reloads HAProxy so it stops being served. A service
  whose cert is disabled is flagged.
- **Remove** (FR-H14): a separate, confirmed action that deletes the file and its tracking row, refused while
  any service uses it.

## 7. Apply, status, stats and the UI actions (D4, D5, D6, D14)

**Pending changes.** The editor compares the files it would write (config and crt-list, plus any cert
files queued for install) with what is live. The UI shows **Pending changes** with a summary and **Review
changes** (a diff). When nothing differs, the bar shows **No changes** and Apply is disabled (D4).

**Apply changes** does, in order:

1. Render the candidate config and crt-list into a **staging directory** with the paths substituted (D6),
   so validation sees the candidate crt-list and certs; render again with the final paths for install.
2. **Validate:** `haproxy -c -f <staged candidate>`. On failure nothing is written and the errors are shown
   against the offending setting or service.
3. Back up the live config and crt-list (timestamped, keep the last N).
4. Install cert files, crt-list, then config, each atomically (write beside, then rename) with the OS's
   ownership and mode, through sudo.
5. **Reload** via the driver, then **verify**: the service is active and, through the stats socket, the new
   worker generation is serving.
6. On any failure after step 4: restore the backups, reload again, and report exactly what happened. The UI
   always states which config is live.

**Other actions, all in the UI:** **Check** (steps 1 and 2 only), **Reload**, **Restart**, **Start**
(confirmed where they drop connections), a **service status** panel (active/inactive, HAProxy version,
uptime, last apply and its result), a **Backups** list with view and restore (each restore goes through
validate and reload), and an **ops log** plus service log over SSE.

**Stats tab (D14).** A tab beside the editor that shows how HAProxy is faring, read-only and simple:

CMD>If it's easy to do, we could have a stats tab to flip over and see how haproxy is faring.  I think that's a good idea, but I don't want to blow up the FRD -- keep it simple.   

- The generated `global` includes a **stats socket** at the driver's path, owned by the app's user and at
  the read-only `level user`, so the app reads it directly with no sudo and can never change HAProxy
  through it. (The same socket serves the Apply verify in step 5.)
- The tab shows a table of frontends, backends and servers with status (UP/DOWN), current sessions, session
  rate, bytes in/out, and last check result, plus HAProxy's version, uptime and current connections. It
  refreshes on a button and every few seconds while visible. Nothing else: no graphs, no history, no
  controls.
- If the socket is missing (HAProxy not applied yet, or stopped), the tab says so plainly.

## 8. Privileges (D9)

Like smbedit, the service user (the owner's account) gets passwordless sudo for a short listed set of
commands per OS, documented in `docs/guides/haproxy.md`: install and read the config, crt-list and cert
files in their directories, `haproxy -c`, `systemctl reload|restart|start haproxy` (or `launchctl` for the
driver's job on macOS), and read the journal. The guide notes the standard sudoers caveat that a wildcard
path grant can match `..`; the editor only ever passes paths it built from a validated name under the
configured directory, and exact-path grants are preferred where the path is fixed. No credentials are
placed on a command line.

## 9. Module conventions (must follow)

- **Registration:** module `haproxy`, its own `host_routing` hostname(s), config block `haproxy`
  (`static_dir`, `data_dir`, and the settings below). Build failures are scoped to this module (503 with
  the reason; other modules keep serving).
- **Auth:** opt-in per module through `auth.modules.haproxy`: a PIN file and/or LDAP on the platform's
  login page, with **its own login, session and timeout cycle**. A login on another module grants nothing
  here and vice versa; no shared/global cookie (`auth.cookie_domain` stays empty). **Auth is encouraged but
  not required, like any other module**; the guide says plainly that without it anyone who can reach the
  hostname can rewrite the proxy.

CMD> like any other module, auth is encouraged but not required

- **UI:** ☰ hamburger menu on the right (side from config only); shared `web/shared` components (theme,
  menu, toasts, modals); boolean settings are toggle switches, never checkboxes; text fields wide enough
  for real values (FQDNs, upstreams, directive lines); **no autosave on keystroke**, edits are explicit and
  applied on **Apply changes**; every outcome (saved, checked, applied, failed, rolled back, installed from
  CertMachine) produces a toast, never a silent no-op.
- **Module settings** (`haproxy` block): `os` (auto), `config_path`, `certs_dir`, `crt_list_path`,
  `stats_socket_path`, `service_name`, `backup_keep`, `expiry_warn_days`,
  `certmachine.{url, api_key, ca_file}`, and the sudo command paths. Every default comes from the driver.
- **Tests and docs:** Go tests alongside the code; `docs/guides/haproxy.md` (before you start, how it fits
  together, privileges per OS, CertMachine setup, troubleshooting, owner acceptance checklist, HTTP API),
  mirroring `docs/guides/smbedit.md`. `make test` is the standard full test.

## 10. Relationship to smbedit (future merge)

smbedit and this module share a shape: parse a system config into a model, edit it in a UI, render, back
up, install, act on a service, stream an ops log. Design this module so that shape can be lifted out later,
without doing it now:

- keep the **pipeline** (render, diff, validate, backup, install, service action, ops log, rollback)
  behind a small interface with no HAProxy types in it;
- keep the **OS driver** interface generic (paths, service name, reload/restart/start, ownership,
  privileged read/write);
- keep the **editor UI** (globals list, services list, diff view, backups list, pending-changes bar) in
  reusable front-end pieces written against a generic globals-plus-items shape.

If the merge happens, the likely outcome is one "system config editor" module with per-service plug-ins
(Samba, HAProxy, others); that is for a later FRD.

## 11. Requirements

### Model and import
- **FR-H1** Import an existing `haproxy.cfg` into the model (section 5); comments are dropped (D3);
  anything unmapped is kept as raw and reported; the import is checked with `haproxy -c`.
- **FR-H2** Edit `global` and `defaults` as ordered key/value settings with a pick-list of common keys
  (smbedit-style globals).
- **FR-H3** Edit **services**: name, enabled, FQDN(s), exposed port (default 443), upstream host (default
  127.0.0.1, editable) and port, health check on/off, cert (see D12), and extra directives.
- **FR-H8** **Add port**: define another exposed port that can hold many services, each with its own default
  service or none; the UI groups services by port.
- **FR-H4** Edit the **default service** (its upstream; not deletable); the 443 frontend always has it as
  `default_backend`; every other port has its own chosen default or none.
- **FR-H5** Generate the frontends, host rules and backends from the model (section 5), a TLS bind per
  exposed port using the crt-list, and keep unmapped sections as raw.
- **FR-H6** Referential checks: a duplicate service name or FQDN, an FQDN claimed by two services on one
  port, a service naming a cert that is missing or disabled, a port default that is not one of its services.
- **FR-H7** A read-only **Raw** view of exactly what Apply would write.

### Certificates
- **FR-H10** List the managed certs with enabled state and which services use each; details and expiry come
  from CertMachine (D15). Files that do not follow the naming convention are never touched and appear only as
  a count.
- **FR-H11** Show FQDN, SANs, issuer, validity window and status for each cert, fetched from CertMachine.
- **FR-H12** Name every cert file by the 6.2 convention (D16) and write it with the OS's ownership and mode;
  never overwrite in place.
- **FR-H13** Disable / enable a cert without touching its file; a disabled cert stays listed and tracked, and
  is left out of the crt-list (D7).
- **FR-H14** Remove a cert (confirmed), refused while a service uses it.
- **FR-H15** Warn on expired and soon-to-expire certs, using CertMachine's expiry.
- **FR-H16** Private keys are never returned by the editor's API or shown in its UI (D8).
- **FR-H17** Generate and maintain the managed crt-list file (D7).
- **FR-H18** Per-service coverage check of FQDN(s) against the chosen cert's SANs, as in 6.5: picker, check on
  every Apply, warnings not blockers (D12; pending owner acceptance of 6.5).

### CertMachine
- **FR-H40** Settings for the CertMachine URL, API key (redacted, never logged) and an optional CA file;
  HTTPS required for non-loopback URLs.
- **FR-H41** List CertMachine's certs and fetch one cert's details, all over REST.
- **FR-H42** Pull a chosen cert's `haproxy.pem` for a service, verify it (FR-H45), write it, record it, and add
  it to the crt-list (6.3).
- **FR-H43** Offer one-click **Update** when an update is available (FR-H46, 6.4).
- **FR-H44** Distinct, visible errors for CertMachine unreachable / unauthorized / TLS / server error /
  integrity failure, with the rest of the module unaffected.
- **FR-H45** Verify every download: the SHA-256 of the received bytes must equal the `ETag`, and `X-Cert-Id` and
  `X-Cert-Fingerprint` must match what was asked for, or nothing is written (D17).
- **FR-H46** Detect **Update available** by asking CertMachine for the active cert for the recorded FQDN and
  comparing its id with the recorded one (6.4).
- **FR-H47** Require the CertMachine extension of section 13; if a CertMachine does not provide the `ETag`, say
  so plainly ("this CertMachine needs updating") and write nothing, rather than skipping the check.

### Apply, status, stats and safety
- **FR-H20** Pending-changes detection and **Review changes** (diff); Apply is disabled and says "No
  changes" when nothing differs (D4).
- **FR-H21** Apply pipeline exactly as section 7, including validate in staging, backup, atomic install,
  reload, verify, rollback.
- **FR-H22** **Check**, **Reload**, **Restart** and **Start** as UI actions, with confirmation where they
  can drop connections, so no change ever requires the CLI (D5).
- **FR-H23** Service status panel; backups list with view and restore.
- **FR-H24** Ops log and service log streams over SSE.
- **FR-H25** **Stats tab** (D14): read-only table of frontends, backends and servers plus version, uptime and
  connections, from a read-only stats socket; plain message when the socket is absent.

### Platform
- **FR-H30** Three drivers (Ubuntu, Rocky Linux, macOS Tahoe on Apple silicon) behind one interface (D1);
  auto-detect with a settings override; an unsupported OS (including Intel macOS) is a scoped 503 with the
  reason.
- **FR-H31** Identical behaviour and UX on all three OSs; no OS-specific control, label or option appears in
  the UI (D13).
- **FR-H32** macOS: HAProxy runs as a driver-owned launchd job with `-W`, a pidfile, a raised file limit and
  `bind *:443`, with graceful `SIGUSR2` reload (verified on Tahoe).
- **FR-H33** Rocky: detect and report the SELinux conditions in section 4 without changing policy.
- **FR-H34** All privileged operations go through sudo (D9), limited to the documented commands.
- **FR-H35** Module conventions of section 9 (auth optional, ☰ right, toggles, no autosave, toasts).

## 12. Testing and acceptance

`make test` is the standard full test; web code also needs `make test-web`.

- **Import and render:** import a sanitized copy of hero's `haproxy.cfg` and assert (a) the model has the
  default service, the `brandx` and `s3-hero` services, the 8443 service and the raw stats section; (b) the
  rendered output passes `haproxy -c` (stub, or the real binary where installed); (c) rendering is stable:
  import(render(import(x))) equals import(x).
- **Change detection (red first):** an unedited model reports no changes and Apply does nothing and says so;
  any edit reports changes; toggling a service's or cert's enabled flag reports changes.
- **Apply:** table tests with a fake driver for validate-fail, install-fail, reload-fail and rollback,
  including that a failed Apply leaves the previous config live.
- **Services:** a disabled service is absent from the generated config but stays in state; two services
  sharing a port share one frontend; the default service is always the 443 default backend; a service
  cannot be created that shadows another's FQDN.
- **Certificates:** file names follow the convention exactly and are deterministic; a changed bundle gets a new
  file name and the old one is removed only after a successful Apply; files that do not follow the convention
  are never modified or deleted (red first); no cert detail is ever written to `certs.json` (red first); disable
  removes the cert from the crt-list but keeps the file and tracking row, enable restores it; keys never appear
  in any API response (red first); the code has no x509, `openssl` or upload path (a test that fails if one
  is imported).
- **Integrity (red first):** a download whose body hash differs from its `ETag`, whose `X-Cert-Id` or
  `X-Cert-Fingerprint` is not the one asked for, or that has no `ETag` is rejected and writes nothing; a
  matching one is written byte for byte.
- **Freshness:** a different active id for the recorded FQDN reports Update available without downloading a
  key; the same id reports up to date; no active cert for the FQDN is reported plainly.
- **Coverage (red first):** a service whose FQDN is not in its chosen cert's SANs is flagged and does not
  block Apply; a cert whose SANs span two systems covers both services and is written once; CertMachine
  unreachable shows "unknown" and Apply still works.
- **Ports and services:** two ports each holding several services render one frontend per port; a service
  never appears under another port's frontend; a port's default is one of its services or none; hero's 8443
  frontend imports as a port with one default service.
- **CertMachine client:** against a fake server: list, fetch by id, each failure class, "details unavailable"
  without breaking the page; the API key never appears in a response or log (red first); non-loopback HTTP is
  refused. The CertMachine-side tests are in section 13 (FR-C5).
- **Drivers:** each OS's paths, commands, baseline directives and ownership rules asserted against a fake
  exec layer, including that macOS never emits a non-wildcard bind, that `-W` is used rather than the
  deprecated keyword, and that no OS-specific field appears in the API shape.
- **Stats:** parse a recorded `show stat` CSV into the table; a missing socket yields the plain message.
- **Auth:** the module's login is its own: a session from another module must not open it.
- **Owner acceptance (browser, not self-certified):** import hero's config and review the generated file;
  add a service for a new FQDN and local port, install its cert from CertMachine, and Apply; add a second exposed port with several services under it; disable a cert
  and see it stop being served after Apply while still listed; make a deliberately broken edit and see Check
  and Apply refuse it with the old config still live; see a pending-changes bar appear after an edit and
  clear after Apply with no CLI step; open the Stats tab and see servers UP/DOWN. Repeat the core flow on
  the Mac and confirm it looks and behaves identically.

## 13. CertMachine changes required by this module (in scope)

CertMachine is its own service and stays that way (D10), but this module needs a few things from it that it
does not offer today. They are small, **additive** and **in scope for this FRD**: they are specified here,
built and tested as part of this work, and **done first** because they stand alone and can be checked with
`curl` before any editor code exists.

### 13.1 What the editor needs, and what CertMachine has today

| The editor needs to | CertMachine today | Change |
|---|---|---|
| List certs with SANs, status, expiry, fingerprint, stale flag | `GET /api/certs` returns all of it | none |
| Fetch one cert's details | `GET /api/certs/{id}` | none |
| Download the HAProxy bundle | `GET /api/certs/{id}/files/haproxy.pem` | none to the body |
| Find the **active** cert for an FQDN (to learn what is current) | only by fetching everything and filtering | **FR-C1** |
| Know a download is **uncorrupted and is the cert it asked for** | no `ETag`, no identity headers | **FR-C2, FR-C3** |
| Re-check cheaply without re-downloading a key | no conditional requests, no `HEAD` | **FR-C4** |
| Authenticate server to server | an API key works on any protected module | none (setup only, FR-C7) |
| Show why an export was refused | quarantined and root-mismatch exports already return 409 with a message | none (pinned by FR-C5) |

The bytes of a given cert id's `haproxy.pem` never change (it is that cert, its own signing CA and its own
key), so a stable per-id `ETag` is both cheap and meaningful.

### 13.2 Specification

**FR-C1: list filters.** `GET /api/certs` accepts optional query parameters:
- `fqdn=<name>`: exact, case-insensitive match on the cert's primary FQDN (a wildcard name is matched
  literally);
- `status=<active|archived|quarantined>`: any other value is `400`.
Both can be combined, for example `?fqdn=utuber.cmdhome.net&status=active` returns at most one cert (CertMachine
allows one active cert per FQDN). With no parameters the response is exactly today's. The response shape is
unchanged: `{"certs": [...]}`.

**FR-C2: strong `ETag` on downloads.** Every response from `GET /api/certs/{id}/files/{name}` (`cert.pem`,
`key.pem`, `haproxy.pem`) carries `ETag: "sha256-<64 hex>"`, the SHA-256 of the exact response body. All file
downloads already pass through one function (`writeDownload`), so this is added in one place. The existing
`Content-Type`, `Content-Disposition` and `Cache-Control: no-store` stay as they are.

**FR-C3: identity headers.** The same responses carry `X-Cert-Id: <id>` and
`X-Cert-Fingerprint: <the cert's fingerprint, exactly as GET /api/certs reports it>`, so a client can confirm
the bytes belong to the cert it requested without reading the PEM.

**FR-C4: conditional requests and HEAD.** `If-None-Match` carrying the current `ETag` returns `304 Not
Modified` with no body (the `ETag` and identity headers still sent); `HEAD` returns the headers with no body.
Both build the bundle server-side but transmit no key material.

**Unchanged on purpose.** Refusals still return what they do today: a quarantined cert or one that does not
chain to its CA returns `409` with its message and **no** `ETag`; an unknown id returns `404`. There is no
schema change, no change to issuing, renewing, editing or deleting, and no change to CertMachine's UI.

### 13.3 What the editor does with it

It requires the `ETag` (FR-H47) and treats its absence as "this CertMachine needs updating", not as a reason to
skip verification. It uses FR-C1 to find the active cert for an FQDN (the picker and Update available), and
FR-C2/FR-C3 to verify each download (6.4).

### 13.4 Requirements

- **FR-C1** `fqdn` and `status` filters on `GET /api/certs` as above; unfiltered behaviour unchanged.
- **FR-C2** Strong `ETag` (`"sha256-<hex of body>"`) on every file download.
- **FR-C3** `X-Cert-Id` and `X-Cert-Fingerprint` on every file download.
- **FR-C4** `If-None-Match` to `304`, and `HEAD`, on file downloads.
- **FR-C5** Tests in `internal/certmachine` (run by `make test`), each written failing first:
  the filters (each alone, combined, no match, `400` on a bad `status`, unfiltered list identical to today's);
  the `ETag` equals the SHA-256 of the body for all three file names; the identity headers are present and the
  fingerprint equals the list's; `If-None-Match` gives `304` with no body and a wrong tag gives `200`; `HEAD`
  has headers and no body; a quarantined cert and a root-mismatch cert still return `409` with their message
  and no `ETag`; an unknown id is `404`; the existing download tests still pass unchanged.
- **FR-C6** `docs/guides/certmachine.md` documents the filters and the download headers, with a `curl`
  example that downloads a bundle and checks its hash.
- **FR-C7** Setup, no code: how to create an API key on the CertMachine instance and list it in that
  instance's `auth.api_keys` (the existing mechanism), documented in both guides. Any key works; a dedicated
  one is recommended and not enforced (D10).

### 13.5 Order and acceptance

Build FR-C1 to FR-C4 first, with FR-C5 to FR-C7. Acceptance, by `curl` with an API key, before editor work
starts: the filtered list returns the one active cert; a bundle download has all three headers and
`sha256sum` of the body equals the `ETag`; repeating it with `If-None-Match` returns `304`; `HEAD` returns
headers only; an Edit that re-issues a cert (new SANs) makes the filtered list return the new id. Then
`make test` passes. Compatibility: additive, so existing clients and the CertMachine UI are unaffected.

## 14. Approval record

**Approved by the owner on 2026-09-30.** All items that were open are confirmed:

1. The service-to-cert link and coverage check (D12), as explained in section 6.5: warnings, not blockers, and
   the picker shows only covering certs by default.
2. The CertMachine extension (section 13), including the `X-Cert-Id` / `X-Cert-Fingerprint` header names and
   the `ETag` on all three file downloads.
3. The file naming convention (D16): `<safe-fqdn>-<first 12 hex of the bundle SHA-256>.pem`.

Everything settled earlier in this document stands. **Next:** build the CertMachine extension (FR-C1 to FR-C7)
and accept it by `curl` (section 13.5), then the editor, with `make test` (and `make test-web` for web code)
as the standard full test.
