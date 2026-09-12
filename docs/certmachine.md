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

**There is no login.** Anyone who can reach the certmachine hostname can
initialize or import the certificate authority, issue certificates, and
download every private key the module holds, including the root CA's own key.
Reaching the hostname is the entire access-control story in this release —
see [Security posture](#12-security-posture) for the full list and why this
is deliberate for now, not an oversight.

Two other things worth knowing up front:

- **There is no CA rotation or deletion route through the API.** FR-6
  deliberately ships no "delete the CA" button — see
  [Replacing the root CA](#10-replacing-the-root-ca) for why, and for the
  manual procedure that exists instead.
- **A healthy process is not proof certmachine came up.** An unwritable
  `db_path` directory makes the module fail to build; the process stays up,
  every other module keeps serving, and every certmachine hostname returns
  **503** with the offending path in the body. Check the 503 body or the boot
  log before assuming the module is broken in some other way.

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
                                                  ├──▶ rootCA.crt   ──▶ client trust stores
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
   **Init Root CA** to generate a fresh one.
5. Click **New certificate**, fill in an FQDN (and optional DNS/IP SANs), and
   submit. Download `haproxy.pem` straight from the list row.

---

## 4. The interface

The page has a CA panel at the top (root CA metadata, download, trust
instructions — see [§6](#6-trusting-the-root-ca)) and a certificate list
below it, with a toolbar: search box, sort (name / created / expiry) with
direction toggle, a group-by-domain toggle, **New certificate**, and a
**Tools** menu (re-run the import wizard, when available).

Each row shows the FQDN, its status badge, and two inline actions —
`haproxy.pem` and `.tgz` bundle downloads — because that pair covers the
common deployment workflow without opening the detail view. Opening a row's
**Details** button gets you the full parsed record (CN, SANs, serial,
SHA-256 fingerprint, validity window, `importedFrom` when applicable),
per-file downloads, copy-to-clipboard for any PEM, **Renew**, and **Delete**
(which requires typing the FQDN, case-insensitively, before it unlocks).

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
told to trust the root. Download `rootCA.crt` from the CA panel and install
it:

| OS | Procedure |
|---|---|
| macOS | Open the downloaded `rootCA.crt` in Keychain Access, then set it to "Always Trust". |
| Linux | Copy `rootCA.crt` to `/usr/local/share/ca-certificates/` and run `update-ca-certificates`. |
| Windows | Import `rootCA.crt` into the "Trusted Root Certification Authorities" store. |
| iOS | Install the configuration profile for `rootCA.crt`, then enable full trust under Settings > General > About > Certificate Trust Settings. |

Every client that needs to trust certificates issued by this CA needs this
done once. When the root CA is ever replaced (see [§10](#10-replacing-the-root-ca)),
every one of those clients needs it done again for the new root.

---

## 7. Downloads and the HAProxy workflow

- **`haproxy.pem`** (single file, offered inline on every row and in the
  detail view) is the certificate, its full chain, and its private key
  concatenated into the one combined PEM HAProxy expects for `bind ... ssl
  crt`. In most deployments this is the only file you need.
- **`.tgz` bundle** contains `cert.pem`, `key.pem`, `haproxy.pem`, and
  `rootCA.crt`. Tar (not zip) is deliberate: it preserves Unix permission
  bits, so `key.pem` and `haproxy.pem` arrive `0600` and `cert.pem` /
  `rootCA.crt` arrive `0644`, without your having to `chmod` anything after
  extraction — see the note below.
- Individual files are also available from the detail view:
  `<fqdn>.cert.pem`, `<fqdn>.key.pem`, `<fqdn>.haproxy.pem`, `rootCA.crt`.
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
**Tools** menu (e.g. if you declined it the first time, or a second legacy
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

FR-6 deliberately provides **no** API route to delete or rotate the
certificate authority: doing that safely (re-chaining every leaf, telling
every already-deployed client to trust a new root) is an operational
decision, not something a button should do silently. Instead, replacing the
CA — because it is expiring, because a legacy import chose the wrong one, or
because a fingerprint mismatch left you stuck — is a **documented manual
procedure**. Three server error messages point at exactly this procedure by
quoting its literal text (see `internal/certmachine/importer.go`'s
`caReplacementRemedy` constant): the `ErrImportPending` refusal from
`POST /api/ca/init`, the fingerprint-mismatch refusal from the import
wizard, and the expiring-CA 409 from generate/renew. The steps below expand
on that quoted text; they do not contradict it.

1. **Stop the binary.** The database is single-writer; editing `ca` rows
   while the server holds the file open is unsupported and can corrupt the
   file.
2. **Back it up first:**
   ```bash
   cp certmachine.db certmachine.db.bak
   ```
3. **Delete the CA row:**
   ```bash
   sqlite3 certmachine.db "DELETE FROM ca;"
   ```
4. **Restart, then either click Init Root CA** (generates a fresh
   RSA-4096, ~10-year root) **or re-run the import wizard** against a
   legacy tree if the replacement root should come from there instead.
5. **Renew every existing certificate.** This is the expensive step, and
   the reason the CA's remaining lifetime is worth watching before it
   becomes urgent: every leaf still on disk was signed by the *old* root,
   which the `ca` row no longer holds. The root-match guard
   (`checkDownloadable`, `ErrRootMismatch` in `bundle.go`) refuses to
   assemble `haproxy.pem` or a `.tgz` bundle for any leaf that no longer
   chains to the currently-stored CA, with a 409 whose message literally
   names the remedy: renew it. Renewing re-issues the same CN and SAN set
   fresh under the new root and clears the mismatch in one action — it is
   not blocked by the guard itself, since Renew always signs under
   whatever CA is currently stored. There is no shortcut here; every
   deployed certificate's replacement has to be distributed the same way
   the original was.
6. **Re-distribute the new `rootCA.crt`** to every client that trusted the
   old one, following [§6](#6-trusting-the-root-ca) again for each. Until a
   client has done this, it will show a certificate-trust error for
   *every* certificate issued after step 4, even ones that renewed
   cleanly — the leaf changed roots, and the client's trust store did not
   move with it.

Step 5 is why the CA's remaining lifetime matters even though nothing
appears broken day to day: the closer it gets to `expiry_warn_days` of
remaining life, the more certificates step 5 will eventually mean
re-issuing, and the 409 from `checkCAExpiry` starts refusing new
generate/renew calls before you necessarily notice the calendar.

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
encryption-at-rest work is in scope here — that work is already underway,
platform-wide, on a separate branch, and certmachine is expected to sit
behind it once it lands rather than grow its own parallel mechanism. What
follows is **not a punch list of things this module got wrong**; it is a
record of what an operator should know before exposing this hostname, so
the gap is documented rather than discovered:

- **No authentication on any endpoint**, including certificate-authority
  initialization, private-key downloads (`cert.pem`, `key.pem`,
  `haproxy.pem`, `.tgz` bundles, and the CA's own key implicitly via any
  operation that needs it), and every mutating route. Network reachability
  of the hostname is the entire access boundary. This matches the inherited
  legacy tool's posture; it is not a regression introduced by this module.
- **Key material is stored unencrypted in the SQLite database**
  (`certmachine.db`), exactly as it was stored unencrypted on disk by the
  legacy tool. Anyone with filesystem read access to `db_path` has every
  private key certmachine has ever issued or imported, including the root
  CA's.
- **No TLS of its own.** Like every other module in this server, certmachine
  serves plain HTTP; TLS termination is the front proxy's job.
- **What *was* addressed in this branch, deliberately, because it is
  certmachine-specific and structural rather than a platform-wide auth
  concern:** path traversal in downloads (routes are keyed by database row
  id, not filesystem-derived names), the CA re-initialization race (a
  second `POST /api/ca/init` gets a deterministic 409, never a silent
  clobber), silent error-swallowing (every failing path returns and logs an
  error rather than discarding it), and GET requests that mutate state
  (every mutation in this module is POST or DELETE).

When the platform-wide auth/API-key/LDAP effort lands, the intent is for
this module's routes to sit behind it without route-shape changes — nothing
here was designed to make that harder.

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
| `GET /api/config` | Runtime config for the SPA | `defaultValidityDays`, `expiryWarnDays`, `certCount`, `legacyImportAvailable`, `legacyImportDir`, `legacyImportReason` |
| `GET /api/ca` | Current CA status | `{"exists": false}` if none yet |
| `POST /api/ca/init` | Initialize a new root CA | 201 with none stored, 409 if one exists, 409 `ErrImportPending` if an import is pending |
| `GET /api/ca/root.crt` | Download the root CA certificate | `Content-Disposition: attachment; filename="rootCA.crt"` |
| `GET /api/certs` | List certificates | Metadata only — no PEM in the response |
| `POST /api/certs` | Generate a certificate | `{fqdn, dnsSans[], ipSans[]}` → 201; no validity field, it is always `default_validity_days` (possibly clamped) |
| `GET /api/certs/{id}` | Certificate detail | Includes `certPem`; never `keyPem` |
| `DELETE /api/certs/{id}` | Delete a row | Requires `?confirm=<fqdn>` (case-insensitive); 400 without it, 204 on success |
| `POST /api/certs/{id}/renew` | Renew | 201 with the new row; the predecessor is archived |
| `GET /api/certs/{id}/files/{name}` | Individual file download | `name` is a closed enum: `cert.pem`, `key.pem`, `haproxy.pem` |
| `GET /api/certs/{id}/bundle` | `.tgz` bundle download | `cert.pem`, `key.pem`, `haproxy.pem`, `rootCA.crt` |
| `GET /api/import/preview` | Dry-run the legacy import | Counts and per-item reasons; never writes |
| `POST /api/import` | Execute the legacy import | Idempotent-safe; already-imported leaves report `skipped` |
