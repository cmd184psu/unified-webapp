# FRD: certmachine module for unified-webapp

**Status:** Draft for planning (ralplan)
**Date:** 2026-09-11
**Branch:** certmachine-addition

---

## 1. Overview

Add **certmachine** as the 8th module of unified-webapp: a private-CA certificate
manager that mints, tracks, and serves TLS certificates for hosts on the local
network. It replaces the standalone certmachine webapp
(`reference/certmachine`, v0.0.3) and must inherit that deployment's existing
root CA and certificate inventory so that every client already trusting the CA
keeps working.

The legacy app is functionally correct but has three core problems this module
fixes:

1. **Untenable storage** — a flat `certs/<fqdn>/` directory tree with per-cert
   `meta.json` files that drift from reality, no history, no status, no cleanup.
2. **Hideous, unusable UI** — every cert ever minted renders into one giant
   undifferentiated table.
3. **Broken/awkward downloads** — the bundle download lands under an unhelpful
   filename; downloading is the *deployment mechanism* (certs are manually
   copied to HAProxy, which lives on another box), so this matters.

## 2. Background

### 2.1 Legacy certmachine (reference/certmachine)

Single 219-line `main.go`, static HTML/JS frontend, listening on :9443.

- Data root: `/opt/certmachine/pki/`
  - `rootCA.crt`, `rootCA.key` — 4096-bit RSA root CA, 10-year validity, CN
    "CertMachine Root CA"
  - `certs/<fqdn>/cert.pem` — leaf cert (RSA 2048, 1-year, single DNS SAN,
    ServerAuth EKU)
  - `certs/<fqdn>/key.pem` — PKCS#1 private key
  - `certs/<fqdn>/haproxy.pem` — concatenation: leaf + root + key (the format
    HAProxy consumes)
  - `certs/<fqdn>/meta.json` — `{fqdn, created, expires}` written at gen time
- Endpoints: `/api/init` (create root CA), `/api/gen?fqdn=` (mint leaf),
  `/api/list` (walk tree for meta.json), `/api/download?fqdn=&file=`,
  `/api/root` (rootCA.crt), `/api/bundle?fqdn=` (zip of cert/key/haproxy pem).
- Known defects: `meta.json` can be missing/stale; re-generating an FQDN
  silently overwrites; `/api/download` joins query params directly into a
  filesystem path (path traversal); errors ignored throughout; no
  confirmation/feedback in UI.

### 2.2 unified-webapp module architecture

- One Go binary; `cmd/server/main.go` dispatches on the `Host` header via
  `host_routing` config (hostname → module name).
- Each module: `internal/<module>/` exposing `Build(cfg) (http.Handler, error)`,
  registered in `buildModule()`. A module that fails to build serves 503 with
  the reason; the rest of the binary keeps serving.
- Per-module config struct in `internal/platform/config/config.go`, populated
  from `~/.unified-webapp.json`; `WriteDefault` emits all keys.
- Shared helpers: `internal/platform/response` (JSON responses),
  `internal/platform/middleware` (CORS), `internal/platform/broker` (SSE sync —
  not needed here).
- Frontend: static files under `web/<module>/`, served by the module with SPA
  fallback. Newer modules (multissh, slideshow, obsidianoid) use TypeScript
  compiled by esbuild (`npm run build`); certmachine follows that pattern.
- Tests: per-module `*_test.go` (handler + store), run via `make test`.

## 3. Decisions already made (do not re-litigate in planning)

| Decision | Choice |
|---|---|
| Storage | **SQLite**, full store: metadata *and* PEM blobs in the DB. Driver: `modernc.org/sqlite` (pure Go, no CGO — preserves single-binary build). |
| Legacy data | **One-time import/migration** into SQLite. Legacy dir is read-only and left untouched as the rollback path. |
| Import trigger | **UI import wizard**: offered when the DB has no certs and config points at a legacy dir. Dry-run preview first, then one-click import, then a human-readable report. |
| Cruft handling | **Import everything, badge it.** Expired certs come in with expired status; unparseable/incomplete entries become `quarantined` with the error recorded. Nothing silently dropped. |
| API | Clean new JSON API; the legacy endpoints are **not** preserved (nothing scripted consumes them). Design routes so per-key auth can be added later (see §8). |
| Lifecycle scope | **Renew, Delete, Multi-SAN/wildcard** all in scope, plus parity features. |
| Direct file consumers | None. HAProxy has its own cert directory on a (possibly) different box; certs travel by browser download. No materialize-to-disk feature needed. |

## 4. Functional requirements

### FR-1: Module integration
- New module `certmachine` following the standard pattern:
  `internal/certmachine/Build(cfg config.CertmachineConfig) (http.Handler, error)`,
  case added to `buildModule()`, frontend under `web/certmachine/`.
- New config struct (all keys emitted by `WriteDefault`, documented in README):

```json
"certmachine": {
  "static_dir":            "/opt/unified-webapp/web/certmachine",
  "db_path":               "/data/certmachine/certmachine.db",
  "legacy_import_dir":     "/opt/certmachine/pki",
  "default_validity_days": 365,
  "expiry_warn_days":      30
}
```

- `legacy_import_dir` may be empty (fresh installs skip the wizard).
- Build fails (→ 503 handler) if the DB directory cannot be created or the DB
  cannot be opened/migrated to the current schema.

### FR-2: SQLite store
- Single DB file at `db_path`. Schema (indicative — planner may refine):

```sql
ca    (id INTEGER PK, cert_pem TEXT, key_pem TEXT, created TEXT);
certs (id INTEGER PK,
       fqdn TEXT,                 -- CN / primary name
       serial TEXT,
       not_before TEXT, not_after TEXT,
       sans TEXT,                 -- JSON array: DNS names + IPs
       status TEXT,               -- active | archived | quarantined
       cert_pem TEXT, key_pem TEXT,
       imported_from TEXT,        -- legacy path, NULL for native certs
       quarantine_reason TEXT,
       created TEXT);
```

- **Parsed cert is the source of truth**: metadata columns are always derived
  by parsing `cert_pem` at insert time, never trusted from external metadata.
- "Expired" is **not** a stored status — it is computed from `not_after` at
  query/render time (a cert expires while sitting in the DB).
- One `active` row per FQDN at most; renew archives the predecessor in the same
  transaction.
- `haproxy.pem` is not stored; it is assembled on demand (leaf + root CA + key).
- Schema versioning via `PRAGMA user_version` so future migrations are possible.

### FR-3: CA management
- Import carries the legacy root CA over **byte-for-byte** — this is the heart
  of backward compatibility.
- "Init CA" creates a root only when none exists (4096-bit RSA, 10-year, as
  legacy); the UI never offers re-init while a CA is present.
- Root CA cert downloadable as `rootCA.crt`. The UI keeps the per-OS trust
  instructions from the legacy help text.

### FR-4: Certificate generation
- Inputs: primary FQDN (CN), optional additional DNS SANs, optional IP SANs.
  Wildcard names (`*.example.local`) accepted as CN or SAN.
- Parity defaults: RSA 2048 key, ServerAuth EKU, validity =
  `default_validity_days`, serial from a random/unique source (not
  `UnixNano`).
- Validation: reject empty/duplicate-active FQDNs with a clear error; normalize
  case; validate IP SANs parse as IPs.
- Generation is transactional: a failed generation leaves no partial row.

### FR-5: Renew
- One click on any cert (active or expired): fresh key, same
  CN/SANs, new validity window. Old row → `archived` and new row inserted in
  one transaction. Archived certs remain listed (badged) and downloadable
  until deleted.

### FR-6: Delete
- Any cert row (active, archived, or quarantined) can be deleted after an
  explicit confirmation naming the FQDN. This is the primary cleanup tool for
  the inherited mess. Deleting is permanent (the legacy dir remains as the
  cold backup). The root CA is **not** deletable from the UI.

### FR-7: Downloads (first-class requirement)
- Downloads are how certs get deployed (manually copied to HAProxy, possibly on
  another machine). Every download must carry a correct, predictable
  `Content-Disposition` filename. For wildcard names, `*` is sanitized in
  filenames (e.g. `_wildcard.example.local`).
- **Single-file haproxy.pem download is a first-class action** — in many cases
  it is the *only* file needed. Offered directly on each cert's list row (and
  in the detail view), downloading `<fqdn>.haproxy.pem` as a plain file: no
  archive, no unpacking step.
- Bundle download: `<fqdn>.tgz` (gzipped tar) containing `cert.pem`,
  `key.pem`, `haproxy.pem`, and `rootCA.crt` for convenience. Rationale for
  .tgz over .zip: tar preserves Unix permission bits, so `key.pem` and
  `haproxy.pem` are stored `0600` and arrive that way on the HAProxy box; and
  it sidesteps OS "helpfully" auto-expanding .zip downloads. Certs are
  deployed to Linux hosts, where .tgz is native.
- Individual files from the detail view: `<fqdn>.cert.pem`, `<fqdn>.key.pem`,
  `<fqdn>.haproxy.pem`, `rootCA.crt`.
- All download routes are parameterized by **cert row id**, not by
  filesystem-derived names — this eliminates the legacy path-traversal defect
  structurally.
- Copy-to-clipboard for PEM contents in the detail view.

### FR-8: Import wizard
- Shown when the DB contains zero certs and `legacy_import_dir` is set and
  exists. (Also reachable later from a settings/tools corner of the UI in case
  the first import was declined — planner's discretion on placement.)
- Step 1 — dry run: scan the legacy tree, parse every `cert.pem`, and present a
  preview: N importable (valid), M already expired, K broken/incomplete (with
  per-item reasons: unparseable PEM, missing key, key/cert mismatch).
- Step 2 — import: root CA first, then all leaf certs. Expired certs import as
  normal rows (their computed status shows expired); broken entries import as
  `quarantined` with `quarantine_reason`. Duplicate FQDNs in the legacy tree
  cannot occur (dir-per-fqdn), but defensively: newest `not_after` wins
  `active`, others import as `archived`.
- Step 3 — report: what was imported, what was quarantined and why. The legacy
  directory is never written to.
- Import is idempotent-safe: re-running against a non-empty DB is refused
  unless explicitly confirmed, and already-imported serials are skipped.

### FR-9: UI
- Complete rebuild under `web/certmachine/` — TypeScript + esbuild, matching
  the conventions of the newer modules; responsive; no external CDN
  dependencies (consistent with the rest of the repo).
- Cert list improvements (all four):
  1. **Search + sort** — filter-as-you-type on FQDN/SANs; sortable by name,
     created, expiry.
  2. **Expiry status badges** — valid / expiring soon (≤ `expiry_warn_days`) /
     expired / archived / quarantined, color-coded; expired+archived
     de-emphasized and collapsible so the default view shows the living certs.
  3. **Group by domain** — certs grouped under their parent domain
     (`foo.cmdhome.net`, `bar.cmdhome.net` → `cmdhome.net`), collapsible
     groups, toggleable with the flat view.
  4. **Cert detail view** — full parsed details (CN, SANs, serial, SHA-256
     fingerprint, validity dates, imported-from), per-file downloads, copy-PEM,
     renew/delete actions.
- Each list row carries the two common actions inline: download
  `haproxy.pem` (single file) and download bundle (`.tgz`) — the 90% workflow
  never requires opening the detail view.
- Action feedback: generation/renew/delete/import show success or the actual
  error — no `alert()`-and-hope.

### FR-10: API
- JSON REST under `/api/` (route naming per existing module conventions),
  covering: CA status/init/root-download, cert list, cert detail, generate,
  renew, delete, bundle/file downloads, import dry-run/execute.
- Proper methods (mutations are POST/DELETE, not GET), JSON errors via
  `internal/platform/response`, correct status codes.
- The API surface should be clean enough for scripted use later; **no
  authentication in this branch** (see §8).

## 5. Non-functional requirements

- Go tests for store (schema, CRUD, import parsing/quarantine, renew/archive
  transitions) and handlers (routes, methods, download headers, traversal
  attempts), consistent in style with existing modules; `make test` green.
- Frontend builds via `npm run build` (esbuild entry added); `tsc --noEmit`
  clean.
- Single static binary preserved: no CGO (hence `modernc.org/sqlite`).
- Import of a few hundred legacy certs completes in seconds; list view stays
  responsive with 500+ rows.

## 6. Explicit non-goals (this branch)

- Authentication, authorization, API keys, LDAP, encryption-at-rest — a
  parallel security effort covers unified-webapp; certmachine will adopt it
  when it lands.
- Automated push/deploy of certs to HAProxy or remote hosts (future, §8).
- ACME/Let's Encrypt, CSR import, intermediate CAs, revocation/CRL/OCSP.
- ECDSA or configurable key types/sizes (RSA-2048 parity for now).
- Editing or deleting anything in the legacy directory.

## 7. Security notes (recorded, deferred to the security branch)

- All endpoints — including private-key downloads and CA init — are
  unauthenticated; anyone who can reach the hostname owns the PKI. Inherited
  from legacy; the parallel auth effort is the fix.
- Root CA key stored unencrypted in the DB (as it was on disk). Same posture.
- Fixed **in this branch** because they are certmachine-specific and structural:
  path traversal in downloads (FR-7), CA re-init clobbering (FR-3), silent
  error swallowing, GET-mutations (FR-10).
- DB file should be created 0600 in a 0700 directory.

## 8. Future directions (not in scope, keep the door open)

- **API keys**: a per-key auth capability is under development on another
  branch; the certmachine API should be shaped so keys can gate it later
  without route changes.
- **Proxy editor (stretch goal)**: certmachine grows an HAProxy management
  panel — view and edit `haproxy.cfg` (frontends/binds and which cert serves
  which hostname), see at a glance which of the machine's certs are deployed
  to the proxy vs. only sitting in the store, and push certs + config to the
  HAProxy host (which may be a different box, so transport is SSH/SCP —
  potentially reusing multissh's plumbing). Replaces today's fully manual
  copy-certs-around workflow. Deferred until the auth/API-key effort lands:
  a tool that can rewrite the proxy config and place private keys on remote
  hosts must not ship unauthenticated.
- Expiry notifications; ECDSA keys; configurable validity per cert.
