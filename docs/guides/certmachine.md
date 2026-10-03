# certmachine

A self-signed certificate authority and leaf-certificate manager for internal
services: initialize (or import) a root CA, issue and renew server
certificates, and download them in the shapes HAProxy and clients actually
need.

certmachine is the seventh module of `unified-webapp`. It is not a separate
service: it runs inside the same binary, on the same port, and is reached by
its own hostname through the `host_routing` table. Configuration lives in the
`certmachine` section of `~/.unified-webapp.json` — see
[the README](../README.md#the-certmachine-section) for the field reference.
This document covers running and using it.

---

## Contents

1. [Before you start](#1-before-you-start)
2. [How it fits together](#2-how-it-fits-together)
3. [Quick start](#3-quick-start)
4. [The interface](#4-the-interface)
5. [Status badges](#5-status-badges)
6. [Trusting the root CA](#6-trusting-the-root-ca)
   - [Automatic device trust](#automatic-device-trust)
7. [Downloads and the HAProxy workflow](#7-downloads-and-the-haproxy-workflow)
8. [The import wizard](#8-the-import-wizard)
9. [Backup](#9-backup)
10. [Replacing the root CA](#10-replacing-the-root-ca)
11. [Performance](#11-performance)
12. [Security posture](#12-security-posture)
13. [Real-PKI rehearsal runbook](#13-real-pki-rehearsal-runbook)
14. [HTTP API](#14-http-api)

---

## 1. Before you start

**Gate this module.** certmachine has no login of its own; it relies on the
platform auth gate. List it in `auth.modules` (the example config ships
`"certmachine": ["pin"]`) or anyone who can reach the certmachine hostname
can initialize or import the certificate authority, issue certificates, and
download every private key the module holds, including the root CA's own key.
An unlisted module is served ungated — reaching the hostname becomes the
entire access-control story. See [Security posture](#12-security-posture).

Two other things worth knowing up front:

- **The current CA can never be deleted through the API, but it can be
  replaced.** There is still no "delete the CA" button — the only route that
  removes a `ca` row is the automatic cleanup that follows a replace or
  switch-back, and it refuses anything but the *previous* CA. See
  [Replacing the root CA](#10-replacing-the-root-ca) for the UI procedure.
- **A healthy process is not proof certmachine came up.** An unwritable
  `db_path` directory makes the module fail to build; the process stays up,
  every other module keeps serving, and every certmachine hostname returns
  **503** naming the module. The build error itself — which includes the
  offending path — appears only in the boot log, never in the response body
  (the 503 surface sits outside the auth gate). Check the boot log before
  assuming the module is broken in some other way.

---

## 2. How it fits together

```
  Legacy PKI dir           certmachine                    Consumers
  ───────────────          ───────────                    ─────────
  rootCA.crt/.key  ──┐
  certs/<fqdn>/    ──┤──▶ import wizard ──▶ SQLite (certmachine.db)
                     │                           │
                     │                           ├──▶ haproxy.pem  ──▶ HAProxy
  Init Root CA  ─────┘                           ├──▶ .tgz bundle  ──▶ manual copy
                                                  ├──▶ <CA name>.crt ─▶ client trust stores
                                                  └──▶ cert.pem/key.pem
```

A single SQLite database (`db_path`) holds exactly one CA row and every leaf
certificate row, active or archived. There is no `meta.json`: every field the
UI shows — CN, SANs, serial, fingerprint, validity window — is parsed back
out of the stored `cert_pem`, never trusted from a sidecar file. The legacy
directory, when `legacy_import_dir` is set, is read from and never written
to; nothing about running certmachine touches it.

---

## 3. Quick start

1. Add a `certmachine` entry to `host_routing` and fill in the `certmachine`
   config section (see [the README](../README.md#the-certmachine-section)).
   If you are migrating from the standalone `certmachine` tool, also set
   `legacy_import_dir` to its PKI directory (the one containing `rootCA.crt`,
   `rootCA.key`, and a `certs/` subdirectory).
2. Build and run:
   ```bash
   make build
   make run
   ```
3. Open the certmachine hostname, e.g. `http://certmachine-test.cmdhome.net:8080`.
4. If `legacy_import_dir` points at a real PKI, the **import wizard**
   appears automatically (see [§8](#8-the-import-wizard)). Otherwise, click
   **Init Root CA**, give the CA a name (its Common Name; blank means
   "CertMachine Root CA"), and generate it. The name tells CAs, and versions
   of one CA, apart: the root download, the bundle entry and the trust-store
   anchor are all named after it (e.g. "Home Lab CA 2026" →
   `Home-Lab-CA-2026.crt`).
5. Click **New certificate**, fill in an FQDN (and optional DNS/IP SANs), and
   submit. Download `haproxy.pem` straight from the list row.

---

## 4. The interface

The page has a CA panel at the top (root CA metadata, download, trust
instructions, **Replace CA…**, and — when a previous CA exists — its own
summary block and a **Switch back to previous CA** button; see
[§6](#6-trusting-the-root-ca) and [§10](#10-replacing-the-root-ca)) and a
certificate list below it, with a toolbar: search box, sort
(name / created / expiry) with direction toggle, a group-by-domain toggle, a
**Stale only** filter, **New certificate**, and an **Import** button (re-run
the import wizard; disabled when there is nothing to import, with the reason
as its tooltip).

Each row shows the FQDN, its status badge (plus a separate **Stale** badge
when it applies — see [§10](#10-replacing-the-root-ca)), and two inline
actions — `haproxy.pem` and `.tgz` bundle downloads — because that pair
covers the common deployment workflow without opening the detail view.
Opening a row's **Details** button gets you the full parsed record (CN,
SANs, serial, SHA-256 fingerprint, validity window, signing CA,
`importedFrom` when applicable), per-file downloads, a copy button for
`cert.pem`, **Re-issue**, **Edit…**, and **Delete…** (which requires typing
the FQDN, case-insensitively, before it unlocks).

Expired and archived certificates are collapsed out of the default view
behind a "Show N expired/archived certificate(s)" toggle, so the list you see
on load is the living certs.

---

## 5. Status badges

Computed client-side from the server's three-value `status` column
(`active` / `archived` / `quarantined`) plus `notAfter` and
`expiry_warn_days` — the server never sends `"expired"` or an
`expiringSoon` boolean, so this is the *only* place the computation happens,
in `web/certmachine/js/status.ts`. Precedence, highest first, and a row is
never shown with more than one badge:

1. **Quarantined** — the stored cert or key did not parse cleanly (usually
   an import-time defect). See `quarantineReason` in the detail view.
2. **Archived** — superseded by a renewal; kept for history and still
   downloadable.
3. **Expired** — `notAfter` has already passed.
4. **Expiring soon** — `notAfter` is within `expiry_warn_days` of now
   (inclusive of the boundary).
5. **Valid** — none of the above.

The same `expiry_warn_days` value gates the certificate authority's own
expiry check (the 409 described in [§10](#10-replacing-the-root-ca)), so
raising or lowering it changes both the badge threshold and how much runway
you get before generate/renew starts refusing. See the README's field table
for the one surprising edge case: `expiry_warn_days: 0` does **not** mean
"never warn."

---

## 6. Trusting the root CA

Certificates issued by this CA are only trusted by clients that have been
told to trust the root. **Trust this CA…** in the CA panel does it for you,
either on this server or on another machine over SSH (see below). To do it by
hand, download the root from the CA panel (named after the CA, e.g.
`Home-Lab-CA-2026.crt`; written `<CA name>.crt` below) and install it:

| OS | Procedure |
|---|---|
| macOS | Open the downloaded `<CA name>.crt` in Keychain Access, then set it to "Always Trust". |
| Linux | Copy `<CA name>.crt` to `/usr/local/share/ca-certificates/` and run `update-ca-certificates`. |
| Windows | Import `<CA name>.crt` into the "Trusted Root Certification Authorities" store. |
| iOS | Install the configuration profile for `<CA name>.crt`, then enable full trust under Settings > General > About > Certificate Trust Settings. |

Every client that needs to trust certificates issued by this CA needs this
done once. When the root CA is ever replaced (see [§10](#10-replacing-the-root-ca)),
every one of those clients needs it done again for the new root.

### Automatic device trust

**Trust this CA…** opens a dialog with two targets: **This server** and
**Another machine over SSH** ([below](#trusting-another-machine-over-ssh)).
"This server" is offered when `certmachine.trust_device_enabled` is `true`
in config and the server's OS is supported. Choosing it runs the
detected platform's native trust-install procedure, via `sudo`, against the
host **the unified-webapp process itself is running on** — not the browser's
machine. This only does something useful when certmachine and the services
whose certificates it issues live on the same box (the intended case: a lab
host running both), and it is off by default.

| Detected platform | What runs |
|---|---|
| macOS (`darwin`) | `sudo -n security add-trusted-cert -d -r trustRoot -k /Library/Keychains/System.keychain <CA file>` |
| RHEL family (`ID`/`ID_LIKE` matches `rhel`/`rocky`/`centos`/`fedora`/`almalinux`) | `sudo -n cp <CA file> /etc/pki/ca-trust/source/anchors/certmachine-<CA name>.pem` then `sudo -n update-ca-trust extract` |
| Debian family (`ID`/`ID_LIKE` matches `debian`/`ubuntu`) | `sudo -n cp <CA file> /usr/local/share/ca-certificates/certmachine-<CA name>.crt` then `sudo -n update-ca-certificates` |
| Anything else (Windows, an unrecognized Linux distro) | Not automated — "This server" is unavailable (`GET /api/config`'s `trustPlatform` comes back empty); use SSH from elsewhere, or the manual procedure in [§6](#6-trusting-the-root-ca) above. |

The anchor is named after the CA (`certmachine-<CA name>`), so trusting a
new CA adds an anchor beside the old one rather than overwriting it.

**Every command runs `sudo -n`** (non-interactive): an HTTP handler has no
TTY to answer a password prompt on, so without `-n` a host that needs a
password would hang the request until the client gave up, rather than fail
immediately with a readable error. This means the operator must grant
passwordless sudo for these specific commands ahead of time, e.g. via a
`sudoers.d` drop-in:

```
# /etc/sudoers.d/certmachine-trust — only as specific as InstallTrust's own commands
<service-user> ALL=(root) NOPASSWD: /usr/bin/security add-trusted-cert *, /usr/bin/cp * /etc/pki/ca-trust/source/anchors/certmachine-*.pem, /usr/bin/update-ca-trust extract, /usr/bin/cp * /usr/local/share/ca-certificates/certmachine-*.crt, /usr/sbin/update-ca-certificates
```

(Adjust binary paths and pick only the lines for your platform.) Whatever the
button ran — success or failure — its full combined output is returned to
the browser and shown under the button, not just a pass/fail toast, so a
`sudo: a password is required` refusal or a missing `update-ca-trust` binary
is immediately visible.

**Accept the risk deliberately before turning this on.** `trust_device_enabled`
hands a web-facing button the ability to run `sudo` on the host — anyone who
can reach this hostname and click the button (subject to whatever
`auth.modules.certmachine` gate is configured, see [§1](#1-before-you-start))
triggers it. This is a deliberate, scoped-down version of "give the webapp
sudo": it is not a general command channel, only ever runs the fixed command
list above against the current root CA, and is meant for a single-operator
lab box, not a shared or internet-facing deployment. A platform-wide,
better-scoped mechanism for actions like this is expected eventually; until
then, this is the whole safety story, and the default is `false` for exactly
that reason.

### Trusting another machine over SSH

"Another machine over SSH" takes a hostname, port, username, and either an
SSH key (picked by name from the server's key folder) or a password. The
server connects, works out the remote OS, and installs the CA there:

- **macOS, RHEL family (Rocky), Debian family (Ubuntu):** the CA is uploaded
  to a `mktemp` file over the SSH session's input (never on a command line),
  then the same commands as the table above run, through `sudo -n` unless
  the login is root. The SSH user therefore needs passwordless sudo, or log
  in as root; no sudo password is ever asked for or sent.
- **Windows (OpenSSH):** the CA is added to the machine's Trusted Root store
  with `certutil -addstore -f Root`; the SSH user must be an administrator.
- **Anything else:** nothing is installed, and the dialog says so.

Every command run, and its output, is shown in the dialog. SSH itself uses
the same settings as multissh — its `ssh_dir` key folder and host-key policy
(`known_hosts_path`, `strict_host_key`) — so there is one SSH configuration
for the whole app. If multissh's SSH settings are missing or invalid, the
option is disabled with the reason as its tooltip.

---

## 7. Downloads and the HAProxy workflow

- **`haproxy.pem`** (single file, offered inline on every row and in the
  detail view) is the certificate, its full chain, and its private key
  concatenated into the one combined PEM HAProxy expects for `bind ... ssl
  crt`. In most deployments this is the only file you need.
- **`.tgz` bundle** contains `cert.pem`, `key.pem`, `haproxy.pem`, and
  `<CA name>.crt`. Tar (not zip) is deliberate: it preserves Unix permission
  bits, so `key.pem` and `haproxy.pem` arrive `0600` and `cert.pem` /
  `<CA name>.crt` arrive `0644`, without your having to `chmod` anything after
  extraction — see the note below.
- Individual files are also available from the detail view:
  `<fqdn>.cert.pem`, `<fqdn>.key.pem`, `<fqdn>.haproxy.pem`, `<CA name>.crt`.
  Wildcard FQDNs have their `*` sanitized in filenames (e.g.
  `_wildcard.example.local`).
- All download routes are parameterized by the certificate's database row
  id, never by a filesystem path built from the FQDN — this is what makes
  the legacy tool's path-traversal defect structurally impossible here
  rather than merely filtered.

**Cross-reference with [README § Production: HAProxy Configuration](../README.md#production-haproxy-configuration):**
that section's `chmod 600 /etc/haproxy/certs/*.pem` step is **unnecessary**
for certs extracted from a certmachine `.tgz` — the tar archive already
carries the correct `0600` permission on `key.pem` and `haproxy.pem`, so
extracting it into `/etc/haproxy/certs/` (or copying `haproxy.pem` there
directly) leaves the permissions already correct. The `chmod` step in that
section exists for certs obtained some other way (a CA vendor's zip, a
manually assembled PEM) where nothing already set the mode bit.

---

## 8. The import wizard

Shown automatically when the database holds zero certificates and
`legacy_import_dir` is set and readable; also reachable later from the
**Import** button (e.g. if you declined it the first time, or a second legacy
tree needs importing).

1. **Preview** (`GET /api/import/preview`) — a dry run. Scans the legacy
   tree and reports counts: importable, already expired, broken/incomplete
   (each with a reason: unparseable PEM, missing key, key/cert mismatch),
   and already-imported (`skipped`, on a re-run against a populated
   database).
2. **Confirm** — shown only when the database *already holds certificates*
   (the server refuses `POST /api/import` without the confirmation flag in
   that case). On a first import into an empty database there is nothing to
   confirm and this step is skipped entirely. It is not gated on the preview's
   counts: **Import now** is offered whenever the scan found anything at all
   to import — including a tree of nothing but broken leaves, which import as
   quarantined rows and are exactly the inventory you most want.
3. **Execute** (`POST /api/import`) — imports the root CA first (verbatim,
   byte-for-byte, never re-encoded), then every leaf. Expired certs import
   as normal rows with an expired status; broken entries import
   **quarantined** with a reason. If the legacy tree has duplicate FQDNs
   (which its own directory-per-fqdn layout should prevent, but the importer
   does not trust that), the newest `not_after` wins `active` and the rest
   import `archived`. The legacy directory itself is never modified.

Re-running the wizard is safe: already-imported leaves (matched by serial or
by their recorded `importedFrom` path) are reported `skipped`, not
re-inserted or quarantined again.

**Fixing a quarantined row.** A quarantined row is a record of a legacy
directory certmachine could not make sense of, and it is not editable in
place — but it is not a dead end either:

1. Fix the source files in the legacy directory (or discard them).
2. Delete the quarantined row (**Details → Delete…**, typing the FQDN to
   confirm). This is a hard delete: the row and its stored PEM bytes are gone
   from the database, and the legacy directory on disk is untouched.
3. Re-run the wizard. With the row deleted, its `importedFrom` path and serial
   are no longer in the skip set, so the directory is scanned and imported
   afresh — a fixed directory now imports as a normal row.

This round trip is covered by
`TestDeletingAnImportedRowLeavesTheLegacyTreeUntouched`
(`internal/certmachine/importer_test.go`), which asserts both halves: the
legacy tree is byte-identical after the delete, and the re-run re-imports
exactly the deleted directory while still skipping everything else.

**A key-format note for anyone reproducing a legacy tree by hand for
testing:** the importer's key parser (`internal/certmachine/pki.go`,
`ParseKey`) accepts only PKCS#1 (`RSA PRIVATE KEY`) PEM blocks, matching what
the original standalone `certmachine` tool actually wrote. A PKCS#8
(`PRIVATE KEY`) block — which is what `openssl req -newkey rsa -keyout ...`
produces by default on modern OpenSSL — is rejected with `x509: failed to
parse private key (use ParsePKCS8PrivateKey instead...)`. This is not a
defect to work around: it is the importer correctly refusing a format the
legacy tool never emitted. If you hit this against a *real* legacy tree
(rather than a hand-built test fixture), something upstream of certmachine
re-encoded the key, which is worth investigating in its own right. If you
need to convert a PKCS#8 key to PKCS#1 for a test fixture:
```bash
openssl rsa -in rootCA.key -out rootCA.key
```

---

## 9. Backup

There is exactly one thing to back up, and its identity changes once,
early:

- **Before the first certificate is generated or renewed against an
  imported (or freshly initialized) CA**, the original legacy PKI directory
  *is* your rollback path — it still holds the untouched root and leaf
  certs the standalone tool wrote, and nothing in certmachine has changed
  since. Keep it.
- **After the first new certificate is minted**, that rollback story
  expires: `certmachine.db` now holds state (the new cert, possibly a
  renewal chain) that does not exist anywhere in the legacy directory, and
  reverting to the legacy tree would silently lose it. From that point on,
  **`certmachine.db` is the backup.** Stop the binary (SQLite here is
  single-writer; copying a live database file is not safe) and copy the
  file:
  ```bash
  systemctl stop unified-webapp   # or however the binary is stopped
  cp /data/certmachine/certmachine.db /data/certmachine/certmachine.db.bak
  ```
  Restart, and you have a point-in-time snapshot. There is no live/hot
  backup mechanism and none is planned — the module has no continuous
  replication story, so "stop, copy, restart" on whatever schedule matters
  to you is the whole procedure.

---

## 10. Replacing the root CA

Replacing the CA — because it is expiring, because a legacy import chose the
wrong one, or because a fingerprint mismatch left you stuck — is a UI
operation: **Replace CA…** in the CA panel. Three server error messages point
here (see `internal/certmachine/importer.go`'s `caReplacementRemedy`
constant): the `ErrImportPending` refusal from `POST /api/ca/init`, the
fingerprint-mismatch refusal from the import wizard, and the expiring-CA 409
from generate/renew.

The database keeps at most two CA rows: the **current** one, signing new
issuance, and at most one **previous** one, kept only so certificates it
already signed keep working. There is no unbounded history — as soon as
nothing active is signed by the previous CA, it (and its *archived*
certificates) is deleted automatically, whether that happens right after a
replace, or later because a renew, edit, or delete emptied it out.

### Replace CA…

1. Click **Replace CA…** in the CA panel, confirm the "are you sure" step,
   and give the new CA a name (its Common Name). The name must not collide,
   case-insensitively, with the current or previous CA's download filename.
2. **Choose what happens to certificates currently signed by the outgoing
   CA** (this includes any certificate whose signer is unknown, e.g. an
   unresolved legacy import):
   - **Re-issue under the new CA** — same FQDN and SANs, freshly signed.
   - **Delete** — removed outright.
   - **Keep as-is (marked stale)** — left signed by the old CA; the download
     routes still serve them chained to that CA, not the new one, and the
     list marks them **Stale**.
3. **If the previous CA (the one already being retired to make room) still
   signs any active certificate**, a second choice appears: **re-issue** or
   **delete** those certificates too. This choice has no "keep" option —
   either way, that CA's archived certificates are discarded in the same
   step, along with the CA row itself. (With re-issue, the new copies are the
   only ones kept.) This choice is required whenever it appears; the dialog
   only shows it when there is something to decide.
4. If the blanket choice in step 2 is **re-issue** or **delete**, the dialog
   warns that the current CA will be removed as soon as nothing active uses
   it any more, which means **Switch back** will not be available
   afterwards. Only **keep** preserves the ability to switch back.
5. Submit. The success message reports counts (re-issued / deleted / kept),
   and if the previous CA dropped out as a result, an added sentence says so:
   *"The previous CA no longer signed any active certificate and was removed,
   along with its archived certificates."*
6. **The D6 reminder always follows a successful replace:** *"Machines that
   trusted the old CA are unchanged. Use 'Trust this CA…' for the new CA and
   redeploy the re-issued certificates."* Machines that trusted the retired
   CA keep trusting it — replacing the CA never touches anything outside this
   database. Use the reminder's button (or [§6](#6-trusting-the-root-ca)
   again) for the new CA, and redistribute every re-issued certificate's
   files the same way the originals were.

### Switch back to previous CA

Visible whenever a previous CA exists. Behind an "are you sure" confirm, it
swaps the two roles: the CA that was previous becomes current again, and the
one that was current becomes previous. It refuses (409) if there is no
previous CA, or if the previous CA has itself drifted within
`expiry_warn_days` of expiring — you could not issue under it anyway. The D6
reminder above follows a successful switch-back too, and it can itself cause
the (new) previous CA to drop if nothing active still needs it.

### Per-certificate: re-issue, edit, delete

From a certificate's detail view:

- **Re-issue** (the existing renew action) re-signs the same FQDN/SANs under
  whichever CA is current now. This is the per-certificate way to clear a
  **Stale** badge.
- **Edit…** is available on every non-quarantined certificate — stale or not
  — and opens a form pre-filled with the current FQDN, SANs, and validity
  (defaulting to the server's `default_validity_days`). Submitting re-signs
  under the current CA with whatever was changed; a duplicate active FQDN or
  an out-of-range validity is refused before anything is written.
- **Delete…** works as before (type the FQDN to confirm).

Any of Re-issue, Edit, or Delete can be the operation that finally drops the
previous CA — its success toast gets the same added sentence as a Replace
that drops it.

### The CA panel's previous-CA block

When a previous CA exists, the CA panel shows its subject, validity window,
and how many active certificates still depend on it. This count is exactly
what decides whether Replace's second (previous-CA) choice appears at all,
and it is also visible ahead of time via `GET /api/ca`'s `previous.activeCount`.

### Last resort: editing the database by hand

There is still no supported way to edit `ca` rows while the server is
running — SQLite here is single-writer, and the schema's own invariants (at
most one current CA, at most one previous CA, the current CA never deleted)
are enforced in application code around each operation, not by the database
alone. If the UI procedure above cannot get you where you need to go:

1. **Stop the binary first.** Editing rows while the server holds the file
   open is unsupported and can corrupt the file.
2. **Back it up before touching anything:**
   ```bash
   cp certmachine.db certmachine.db.bak
   ```
3. **Never delete the current CA row.** Every code path that removes a `ca`
   row refuses to touch anything but a row already marked `previous` — there
   is no equivalent guard on raw SQL run by hand.

Prefer the UI procedure above. This section exists for the case where it
cannot be reached at all (for example, a database so damaged the server
won't boot).

---

## 11. Performance

The list view runs its search/sort/group pipeline (`filterCerts` →
`sortCerts` → `groupCerts`, `web/certmachine/js/listmodel.ts`) entirely
client-side, in memory, on every keystroke — no re-fetch. Measured against a
synthetic 500-row fixture on this development machine (200 samples of the
full filter+sort+group pipeline after warm-up): **p50 0.22ms, p95 0.25ms,
max 0.39ms** — comfortably inside the 50ms re-render budget the module was
designed against. The list is expected to stay well under 500 rows in
practice; this number exists to confirm the pure-function design does not
need optimizing before it does.

---

## 12. Security posture

Per the plan for this module, no new authentication, authorization, or
encryption-at-rest work was built inside certmachine — the platform-wide
auth work (PIN/LDAP/passkey/API-key gate, same-origin CORS, origin check,
body limits) has since landed and certmachine sits behind it like every
other module rather than growing a parallel mechanism. What follows is
**not a punch list of things this module got wrong**; it is a record of
what an operator should know before exposing this hostname:

- **Authentication is opt-in per module.** The platform gate protects
  certmachine only when it is listed in `auth.modules` (the example config
  ships `"certmachine": ["pin"]`). An unlisted module is served ungated:
  certificate-authority initialization, private-key downloads (`cert.pem`,
  `key.pem`, `haproxy.pem`, `.tgz` bundles), and every mutating route become
  reachable by anyone who can reach the hostname — the inherited legacy
  tool's posture. Do not deploy this hostname ungated.
- **Key material is stored unencrypted in the SQLite database**
  (`certmachine.db`), exactly as it was stored unencrypted on disk by the
  legacy tool. Anyone with filesystem read access to `db_path` has every
  private key certmachine has ever issued or imported, including the root
  CA's.
- **No TLS of its own.** Like every other module in this server, certmachine
  serves plain HTTP; TLS termination is the front proxy's job.
- **`trust_device_enabled` (default `false`) runs `sudo` on the host from a
  button click.** See [Automatic device trust](#automatic-device-trust) under
  §6 for exactly what runs and the sudoers entry it requires — this is an
  explicit, opt-in tradeoff for a single-operator lab box, not something to
  turn on in a shared or internet-facing deployment.
- **What *was* addressed in this branch, deliberately, because it is
  certmachine-specific and structural rather than a platform-wide auth
  concern:** path traversal in downloads (routes are keyed by database row
  id, not filesystem-derived names), the CA re-initialization race (a
  second `POST /api/ca/init` gets a deterministic 409, never a silent
  clobber), silent error-swallowing (every failing path returns and logs an
  error rather than discarding it), and GET requests that mutate state
  (every mutation in this module is POST or DELETE).

The platform-wide auth/API-key/LDAP effort has landed, and this module's
routes sit behind its gate without route-shape changes, exactly as
intended. Two module-scoped hardening details on top of it: certmachine
strips the platform's CORS response headers off its own responses
(`stripCORS`, `internal/certmachine/build.go`) — a key-serving module has
no cross-origin caller at all — and its build-failure 503 never echoes
filesystem paths.

---

## 13. Real-PKI rehearsal runbook

Before this module is used against the real production PKI, run the
following checklist against a **copy** of it — never the original — on the
target host. This is not a per-slice automated gate; it is a manual release
gate, and item 8 below (the last one) is the single check that proves the
whole exercise achieved its purpose.

1. Point `legacy_import_dir` at a **copy** of the real `/opt/certmachine/pki`.
   With an empty database, the import wizard should appear, and **Init Root
   CA should be demoted** (Import leads) rather than offered as the primary
   action.
2. Confirm `POST /api/ca/init` is refused with `ErrImportPending` while the
   import is still pending — initializing a fresh CA at this point would
   permanently orphan the legacy root.
3. **Check the real root's remaining life:**
   ```bash
   openssl x509 -noout -enddate -in <copy>/rootCA.crt
   ```
   If it has less than `expiry_warn_days` left, the expiring-CA 409 fires
   on the very first generate call, and [Replacing the root CA](#10-replacing-the-root-ca)
   is needed *before* the module is otherwise useful. This single command
   is what decides whether that is a day-one problem or a distant one — as
   of 2026-09-11 the real root has roughly a year of validity left, so this
   is expected to be a distant-event check, not an immediate blocker, but
   re-run it against whatever copy you actually have.
4. Confirm the wizard's **preview counts** reconcile against
   `ls /opt/certmachine/pki/certs | wc -l`.
5. **Execute** the import and confirm the report lists imported / expired /
   quarantined / skipped counts with reasons, plus any stray root-level
   files the legacy tree had.
6. **Re-run Preview** against the now-populated database and confirm every
   leaf reports `skipped`, with the same count Execute's own `skipped`
   total reported.
7. Check every **quarantined** entry's reason by hand against the actual
   file on disk — confirm the reason is accurate, not just present.
8. `diff -r` the legacy copy against the pristine original directory —
   confirm there are **no differences**. The import must never write to the
   source tree.
9. Generate a new certificate after the import and confirm
   `openssl verify -CAfile <original rootCA.crt>` succeeds against it.
10. If the real root turned out to be short-lived (item 3), confirm the
    generate response actually carries the clamped-validity notice, and
    that the shorter `not_after` it reports matches what `openssl` reports
    on the downloaded certificate.
11. **Load the new certificate into a scratch HAProxy frontend and confirm
    a browser that already trusts the old root accepts it with no
    warning.** This is the backward-compatibility gate. Nothing ships
    without it passing.

Record the results of all eleven items in the pull request before merging.

---

## 14. HTTP API

For automation. Client-facing errors carry a human-readable message; 500s
are the exception by design (generic to the client, detailed in the log).

| Method & path | Purpose | Notes |
|---|---|---|
| `GET /api/config` | Runtime config for the SPA | `defaultValidityDays`, `expiryWarnDays`, `certCount`, `legacyImportAvailable`, `legacyImportDir`, `legacyImportReason`, `trustDeviceAvailable`, `trustPlatform`, `trustRemoteAvailable`, `trustRemoteReason` |
| `GET /api/ca` | Current CA status | `{"exists": false}` if none yet. Otherwise `id`, `subject`, `serial`, `notBefore`, `notAfter`, `fingerprint`, `importedFrom`, `unknownSignerActiveCount` (always present, even 0), and `previous` (omitted if there is no previous CA — see below) |
| `POST /api/ca/init` | Initialize a new root CA | Optional body `{name}` (the CA's Common Name; blank = "CertMachine Root CA"); 201 with none stored, 409 if one exists, 409 `ErrImportPending` if an import is pending |
| `GET /api/ca/root.crt` | Download the root CA certificate | `Content-Disposition: attachment; filename="<CA name>.crt"`. No `ETag` or identity headers |
| `POST /api/ca/replace` | Replace the current CA (see [§10](#10-replacing-the-root-ca)) | Body `{"name", "existing": "reissue"\|"delete"\|"keep", "previousStale": "reissue"\|"delete"}` (`previousStale` omitted unless the previous CA still signs an active row). 200 `{"ca": <GET /api/ca shape>, "reissued", "deleted", "kept", "clamped", "previousDropped"}`. 400 on a bad `existing`/`previousStale` value or a name collision; 409 `ErrPreviousStaleChoiceRequired` if `previousStale` is required but missing, 409 `ErrConcurrentChange` if the certificate set moved between the read and the write. **Progress mode:** send `Accept: application/x-ndjson` to get a streamed alternative to the plain 200 above instead — see [below](#post-apicareplace-progress-mode-accept-applicationx-ndjson) |
| `POST /api/ca/switch-back` | Swap the current and previous CA (see [§10](#10-replacing-the-root-ca)) | Empty body. 200 `{"ca": <GET /api/ca shape>}`. 409 `ErrNoPreviousCA` if there is none, or if the previous CA itself fails its own expiry check |
| `POST /api/ca/trust` | Run this host's device-trust install (see [Automatic device trust](#automatic-device-trust)) | 409 if `trust_device_enabled` is false or no CA exists; body always carries `output` (the ran commands' combined stdout+stderr) alongside `platform` and, on failure, `error` |
| `POST /api/ca/trust/remote` | Install the CA on another machine over SSH (see [above](#trusting-another-machine-over-ssh)) | `{host, port?, user, key \| password}`; 200 with `platform` and `output`; 400 bad request; 409 SSH unavailable, or the install failed on that machine (with `output`); 422 unsupported OS, nothing installed (with `output`); 502 couldn't connect |
| `GET /api/ssh/keys` | The SSH key folder's files, for the dialog | `{keys: [...]}`: names only, never key material |
| `GET /api/certs` | List certificates | Optional `fqdn` and `status` filters (see [below](#download-integrity-headers-and-api-key-access)). Metadata only — no PEM in the response. Each row now carries `caId` (the signing CA's row id, or `null` for an unresolved/unknown signer), `caSubject` (omitted when unknown), and `stale` (`true` exactly when `caId` is non-null and differs from the current CA's id) |
| `POST /api/certs` | Generate a certificate | `{fqdn, dnsSans[], ipSans[]}` → 201; no validity field, it is always `default_validity_days` (possibly clamped). Response gains `previousDropped` (bool) alongside the existing `cert`/`validityClamped`/`requestedNotAfter` fields |
| `GET /api/certs/{id}` | Certificate detail | Includes `certPem`; never `keyPem`. Also carries `caId`, `caSubject`, `stale` |
| `DELETE /api/certs/{id}` | Delete a row | Requires `?confirm=<fqdn>` (case-insensitive); 400 without it. **200** `{"previousDropped": bool}` on success (previously 204 with no body — deleting the last active certificate under the previous CA can now trigger its automatic removal, which the response reports) |
| `POST /api/certs/{id}/renew` | Renew | 201 with the new row; the predecessor is archived. Response gains `previousDropped` (bool), same reason as Delete above |
| `POST /api/certs/{id}/edit` | Edit FQDN, SANs, and/or validity in place (see [§10](#10-replacing-the-root-ca)) | Body `{fqdn, dnsSans[], ipSans[], validityDays}` — the only issuance route that accepts a validity directly, still clamped to the current CA's own expiry. 201, same shape as `POST /api/certs`'s response plus `previousDropped`. 409 `ErrDuplicateActive` if another row is already active for the new FQDN; 409 `ErrQuarantined` for a quarantined source |
| `GET /api/certs/{id}/files/{name}` | Individual file download | `name` is a closed enum: `cert.pem`, `key.pem`, `haproxy.pem`. 409 `ErrUnknownSigner` if the certificate's signing CA cannot be resolved (`caId` is `null`) — re-issue it first. Success carries `ETag`, `X-Cert-Id`, `X-Cert-Fingerprint` and honours `If-None-Match` and `HEAD` (see [below](#download-integrity-headers-and-api-key-access)) |
| `GET /api/certs/{id}/bundle` | `.tgz` bundle download | `cert.pem`, `key.pem`, `haproxy.pem`, `<CA name>.crt`. Same `ErrUnknownSigner` 409 as above. No `ETag` or identity headers |
| `GET /api/import/preview` | Dry-run the legacy import | Counts and per-item reasons; never writes |
| `POST /api/import` | Execute the legacy import | Idempotent-safe; already-imported leaves report `skipped` |

### Download integrity headers and API-key access

**Filters on `GET /api/certs`.** Two optional query parameters narrow the
list; with neither, the response is exactly the unfiltered list.

- `fqdn=<name>` keeps rows whose FQDN equals the value, case-insensitively
  (exact match, not a substring).
- `status=<value>` keeps rows with that status: `active`, `archived` or
  `quarantined` (lowercase). Any other non-empty value is a **400**.
- Both together are ANDed. No match is `{"certs": []}` with 200.

**Headers on `GET /api/certs/{id}/files/{cert.pem|key.pem|haproxy.pem}`.**
A successful response carries:

- `ETag: "sha256-<hex>"`, a strong ETag: the SHA-256 of the exact response body.
- `X-Cert-Id`: the certificate's row id.
- `X-Cert-Fingerprint`: the certificate's fingerprint as stored (the same
  value `GET /api/certs` reports).

`If-None-Match` is honoured: a header that is `*`, equals the ETag, equals it
with a `W/` prefix, or is a comma-separated list containing such an entry
gets **304** with no body (the `ETag` and identity headers are still sent).
`HEAD` returns the same headers as `GET` with no body.

Refusals carry none of these headers: a **409** (quarantined row, or a
root/signer mismatch such as `ErrUnknownSigner`) and a **404** (unknown id or
file name) send no `ETag`, `X-Cert-Id` or `X-Cert-Fingerprint`.
`GET /api/ca/root.crt` and `GET /api/certs/{id}/bundle` are unchanged and
carry no such headers (the bundle embeds a timestamp, so it has no stable
ETag).

**Example with an API key** (see [API key setup](#api-key-setup-for-automation)):

```bash
BASE=https://certs.example.local   # your CertMachine instance
KEY=...                            # the API key (not its hash)
ID=7                               # the cert's row id from GET /api/certs

# Download and capture headers.
curl -sS -D hdr.txt -o haproxy.pem \
  -H "Authorization: Bearer $KEY" \
  "$BASE/api/certs/$ID/files/haproxy.pem"

# The ETag is "sha256-<hex of the body>"; the body must hash to it.
etag=$(awk 'tolower($1)=="etag:" {gsub(/[\r"]/,"",$2); print $2}' hdr.txt)
[ "$etag" = "sha256-$(sha256sum haproxy.pem | cut -d' ' -f1)" ] && echo OK

# Conditional re-request: 304 and no body when nothing changed.
curl -sS -o /dev/null -w '%{http_code}\n' \
  -H "Authorization: Bearer $KEY" \
  -H "If-None-Match: \"$etag\"" \
  "$BASE/api/certs/$ID/files/haproxy.pem"      # prints 304
```

### API key setup for automation

Non-browser clients authenticate to a CertMachine instance with an API key,
using the existing mechanism described in
[README § API keys for automation](../../README.md#api-keys-for-automation):

1. Generate a key and store its hash in one step:
   `go run ./cmd/server -gen-api-key -name haproxy-editor -config ./unified-webapp.json`.
   It prints the key once (store it where your client reads secrets) and adds
   the hash to that instance's `auth.api_keys`; restart or reload as you
   normally do for config changes. The admin module's Generate key does the
   same live, without a restart.
2. Alternative, manual: `go run ./cmd/server -gen-api-key` (no `-name`) only
   prints the key and its `sha256:...` hash; paste the hash into
   `auth.api_keys` yourself.
3. Send the key as `Authorization: Bearer <key>` (or `X-API-Key: <key>`).

Any key listed in `auth.api_keys` works. A dedicated key for each client
(for example the HAProxy editor) is recommended so it can be rotated or
removed on its own, but this is not enforced.

**Errors, in general:** `ErrValidation` and a malformed confirm/body are
**400**; a missing row is **404**; everything else this package returns —
including every new sentinel above (`ErrNoPreviousCA`,
`ErrPreviousStaleChoiceRequired`, `ErrConcurrentChange`, `ErrUnknownSigner`)
plus the existing ones (`ErrDuplicateActive`, `ErrCAExists`, `ErrQuarantined`,
`ErrNewerActiveExists`, `ErrCAFingerprintMismatch`, …) — is a **409**: a
well-formed request that conflicts with the database's current state.

**Schema note.** The database schema is version 2. An existing (version-1)
database is migrated automatically, inside one transaction, the first time
the server opens it — there is nothing to run by hand. Once a database has
been migrated, it holds the version-2 shape permanently; going back to an
older binary against that same file afterward is not a supported path.

### `POST /api/ca/replace` progress mode (`Accept: application/x-ndjson`)

A blanket re-issue of dozens of certificates has to generate a fresh RSA-4096
CA key plus one fresh RSA-2048 key per re-issued certificate before the
short database transaction that actually applies the change (R2 of the
CA-replacement design: all crypto runs before the transaction opens) — real
time an operator would otherwise wait through with no feedback at all. The
UI's "Replace CA…" dialog now asks for this by sending
`Accept: application/x-ndjson` on the same request; the request body and
every other rule above is unchanged.

- **Before any crypto runs** (a 400 validation failure, the 409
  `ErrPreviousStaleChoiceRequired` check, a name collision, etc.), the
  response is byte-for-byte the same plain JSON error envelope the
  non-streaming path returns, at the same status code — no line has been
  written yet, so nothing about the failure mode changes.
- **Once the first event is ready to send**, the response commits to 200
  with `Content-Type: application/x-ndjson` and `Cache-Control: no-store`,
  and streams one JSON object per line (newline-delimited, not a JSON
  array), flushed immediately as each one is produced:
  - `{"type":"progress","phase":"ca-key","total":<n>}` — once, before the
    new CA's key is generated.
  - `{"type":"progress","phase":"leaf-keys","done":<i>,"total":<n>}` — once
    per re-issued certificate's key, `done` running from 1 to `n`, where `n`
    is every certificate being re-issued in this call (the outgoing
    current CA's own set, plus the outgoing previous CA's set if that
    choice is also "reissue").
  - `{"type":"progress","phase":"saving","total":<n>}` — once, immediately
    before the (short) database transaction opens.

  `total` is `n` — the same overall leaf-key count as the `leaf-keys`
  events, 0 when none will be re-issued — on *every* progress event,
  including `ca-key` and `saving`, not only `leaf-keys` (`omitempty`, so it
  is simply absent when 0). This lets a client compute an overall
  percentage from the very first event, rather than only once `leaf-keys`
  events begin: the UI's progress bar counts up from 0% to 100% across the
  whole operation, never showing an indeterminate state.
  - A final line, exactly one of:
    - `{"type":"result", "ca": <GET /api/ca shape>, "reissued", "deleted", "kept", "clamped", "previousDropped"}` — the same fields `POST /api/ca/replace`'s plain 200 body carries, on success.
    - `{"type":"error","status":<int>,"error":"<message>"}` — on a failure
      that happened after streaming had already started (e.g. the request
      context was canceled mid-run, or the transaction itself failed).
      `status` and `error` are exactly what the non-streaming path's HTTP
      status and body would have carried for the same failure; `error`
      never leaks internal detail (SQL text, file paths, key material) any
      more than a plain 500 body does.
- If the request's context is canceled while certificate keys are still
  being generated, no partial work is applied: the database transaction has
  not opened yet (R2 again), so cancellation between keys is exactly as safe
  as cancellation before the request started.
