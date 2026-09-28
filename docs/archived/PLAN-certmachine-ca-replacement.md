# Plan: certmachine CA replacement and certificate editing

Source FRD: `docs/FRD-certmachine-ca-replacement.md`
Mode: RALPLAN-DR consensus, **deliberate** (schema migration + new destructive delete paths)
Status: **APPROVED by owner 2026-09-27**, queued for ralph (Sonnet builder) after the utuber/taskmaster-lane plan. Consensus was reached in round 5 (Architect SOUND, Critic APPROVE on v5). **Gate P2 resolved: delete archived rows when the previous CA drops.**

All paths are relative to `internal/certmachine/` unless otherwise stated. Line numbers were verified against the tree at commit `29ac6c7`.

NOTICE: this plan may be carried out already.  Please verify that this plan was already done before touching code.  If so, just mark this plan complete near the top (remove this notice).


---

## 0. Binding rules for the executor (read first)

These rules apply to every task. Breaking any of them is a defect even if the tests pass.

- **R1. No `s.db` inside `WithTx`, and no `db.*` inside `migrate`'s transactions.** `store.go:65` sets `SetMaxOpenConns(1)` before `migrate` runs (`store.go:74-78`), so any `s.db.*` or `db.*` call made while a transaction is open blocks forever. Inside a transaction, use only `tx`-scoped helpers (§3.2). When the code reads rows and then updates them inside one transaction, it must **read everything into a slice, close the cursor, and only then run the UPDATEs**. Never interleave writes with an open cursor on the single connection. Every new tx-using test runs under `context.WithTimeout(…, 10*time.Second)` so a regression fails instead of hanging.
- **R6. Never use `x509.Verify` in production code.** Follow the repo's own rule (`importer.go:231-233`, `bundle.go:86`): signer checks use `CheckSignatureFrom`, because an expired leaf or CA is a routine stored state and not a chain problem. `x509.Verify` appears only in test assertions on freshly issued certificates, with `CurrentTime` pinned inside the validity window, `KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageAny}`, and a roots pool containing only the expected CA.
- **R7. One test seam per concern, all named here.** These are the only test seams:
  - `Store.hook func(step string) error`: nil in production; called by the replace, switch-back, edit, delete, and drop paths through `s.runHook(step)`.
  - `Store.genCA func(name string) (certPEM, keyPEM []byte, err error)`: defaults to `GenerateNamedCA`. Tests set it to a 2048-bit generator, the same trick `setupCA` uses at `lifecycle_test.go:14-17`.
  - `migrateV1toV2(tx *sql.Tx, hook func(step int) error)`: `migrate` passes a nil hook; the rollback test calls it directly on a `ddlV1` database.
- **R2. Crypto happens before the transaction opens.** `lifecycle.go:5-7,152-158` states this as a binding rule. All RSA generation (the new CA, every re-issued leaf) completes before `WithTx` is called. The transaction only reads and writes rows.
- **R3. Foreign keys stay off.** `schema.go:21-22,89` records that the module deliberately uses no FKs and no `foreign_keys` pragma. This plan keeps that decision. `certs.ca_id` is a plain `INTEGER` with no `REFERENCES` clause. Integrity is enforced in code by `checkInvariants` (§3.3), which runs after migration and inside every CA-changing test.
- **R4. The current CA is never deleted.** The only function that may delete a `ca` row is `dropPreviousCATx` (§3.4). It deletes only a row whose `role = 'previous'`, and it re-checks that role inside the same transaction.
- **R5. Compile-forced call-site migration.** `GetCA` is renamed to `GetCurrentCA`. There is no alias, so every call site fails to build until someone has decided which CA it means.

---

## 1. RALPLAN-DR Summary

### Principles
1. **Every CA-changing operation keeps one invariant set true.** The invariants:
   - at most one `current` and one `previous` CA;
   - every non-NULL `ca_id` resolves to an existing CA;
   - every certificate whose `ca_id` is non-NULL has a signature that checks out against that CA (`CheckSignatureFrom`; R6);
   - "stale" holds exactly when `ca_id IS NOT NULL AND ca_id != current.id`. `checkInvariants` (§3.3) encodes these, and it is the oracle for the §3a randomized test.
2. **The FR-6 and FR-4 narrowings are explicit, not silent.** FR-6 ("the CA is non-deletable", `lifecycle.go:354-356`) is narrowed to "the *current* CA is non-deletable; a *previous* CA is deleted automatically under D5/D7". FR-4 ("no per-cert validity override", `pki.go:416-417`) is narrowed to "except Edit (D4)". Both narrowings are written into the code comments and tested.
3. **Atomic or nothing.** Replace, switch-back, edit, renew, delete, and the auto-drop run as one short immediate transaction each. Their crypto runs first (R2). An injected failure at any step leaves the full `ca` + `certs` dump byte-identical.
4. **Reuse existing lifecycle code.** FR-R5 re-issue *is* `Renew` (`lifecycle.go:303-346`, route `handler.go:50`). Edit reuses `ValidateRequest` and `GenerateLeaf`. Downloads keep going through `certAndCA` (`handler.go:664-675`).
5. **No behavior change until the first replacement.** After migration, every existing test passes, with only the edits listed in §6.

### Decision Drivers
1. **Root-of-trust safety.** A wrong delete or a wrong signer attribution breaks every deployed certificate.
2. **Migration on real, populated databases.** SQLite cannot drop a CHECK in place, and the current `migrate()` (`schema.go:97-111`) is neither versioned nor transactional.
3. **Mechanically checkable** without real machines (FRD §3a).

### Viable options
- **Option A: keep `ca` as a strict singleton for the current CA and add a `ca_previous` singleton table.** The current CA moves into `ca_previous` at replace time.
  - Pros: every existing `GetCA` caller stays correct unchanged; FR-6's "the `ca` row is never deleted" survives literally; no CHECK-dropping rebuild.
  - Cons: `certs.ca_id` has to carry a signer across two tables. It would be stored as a fingerprint, or as a discriminator plus id, so every signer lookup becomes a two-table `CASE` or `UNION`. Switch-back copies whole rows between tables instead of flipping one column. A replace step moves key material between tables, which is a larger failure surface than a column update.
- **Option A′: keep `ca` and add an append-only `ca_history` keyed by fingerprint.**
  - Rejected: D5 forbids an unbounded history ("No unending history"), so the history table would need the same n-1 pruning as B with none of B's single-table lookups.
- **Option B (chosen): one `ca` table holding ≤2 rows, distinguished by `role` (`'current'` | `'previous'`).** A partial UNIQUE index enforces one row per role, and `id` is `AUTOINCREMENT` so an id is never reused. `certs.ca_id` is nullable and has no FK (R3).
  - Pros: one lookup path (`GetCAByID`); switch-back is three role UPDATEs; matches FR-R1's wording.
  - Cons: requires a `ca` table rebuild and a compile-forced rename across 11 call sites, both bounded and listed in §3.5.

**Why B over A:** once FKs are off (R3), B's advantage is not referential integrity. It is that there is one table and one lookup for "the CA that signed row X", and that switch-back and replace move a single column instead of whole rows carrying private keys. R5 reduces A's "no call-site churn" advantage to a compile-guided mechanical change.

---

## 2. Decisions made in this revision (flagged for owner review)

| # | Decision | Rationale |
|---|---|---|
| P1 | **`certs.ca_id` is nullable. NULL means "unknown signer"**, which is neither stale nor current. Quarantined rows, and rows whose certificate does not verify against any stored CA (for example an `import_warning` "does not chain" row), get NULL. | Blanket backfill would make invariant 1 false on real data (quarantined rows have no parseable cert; `importer.go:310` rows don't chain). |
| P2 | **A CA "signs" a certificate only through an `active` row.** The previous CA drops off (D5, FR-R7) when no `active` row has its `ca_id`. **When it drops, the archived rows with its `ca_id` are deleted in the same transaction.** Renew, Edit, and Delete can trigger the drop as well as Replace, so every one of those responses reports `previousDropped` (§4). | Otherwise Renew's archive-then-insert (`lifecycle.go:333-340`) leaves archived rows pinning the previous CA forever. Deleting them follows D5's "no unending history". **Owner confirmed 2026-09-27: delete (not NULL-out).** |
| P3 | **The blanket choice (D2) applies to every `active` row not signed by the new CA:** rows under the outgoing CA and active rows with NULL `ca_id`. Archived and quarantined rows are never re-issued or deleted by the blanket choice. | Matches "existing certificates" in D2 without resurrecting history. |
| P4 | **`previousStale` (D7) applies to `active` rows whose `ca_id` is the outgoing *previous* CA.** Omitting it when such rows exist → **409** `ErrPreviousStaleChoiceRequired`. The value `"keep"` → **400**. | Follows the package rule "well-formed request that conflicts with current state → 409" (`handler.go:107-117`). The UI never hits the 409 because `GET /api/ca` exposes the count. |
| P5 | **Edit is `POST /api/certs/{id}/edit`**, body `{fqdn, dnsSans, ipSans, validityDays}`. The body is validated with `ValidateRequest`. `validityDays` is an integer in 1..3650 (`ErrValidation` otherwise) and is still clamped to the CA's NotAfter by `GenerateLeaf`. **FQDN change:** if another row is active for the new FQDN, the answer is 409 `ErrDuplicateActive`. Otherwise the transaction archives the *source* row (by id, if active) and inserts the new row. The old FQDN is then left with no active row. Quarantined sources are refused with `ErrQuarantined`. An archived source whose FQDN has a newer active row, with the FQDN unchanged, is refused with `ErrNewerActiveExists` (same rule as Renew). | Pins every branch an executor would otherwise have to guess. |
| P6 | **Replace name rules:** `NormalizeCAName` (`pki.go:333`). `CAFileStem(name)` must differ, case-insensitively, from both the current and the previous CA's stem, otherwise 400. | Prevents download filename collisions via `CAFileName` (`pki.go:375`). |
| P7 | **Switch-back refuses (409) when the previous CA fails `checkCAExpiry`** (`lifecycle.go:143`). | You could not issue under it anyway; one notion of "about to expire". |
| P8 | **Replace is allowed even when the current CA is expiring or expired.** | That is the main use case. |
| P9 | **One audit `log.Printf` line** per replace and per switch-back: old and new fingerprint, reissued/deleted/kept counts, and whether a previous CA was dropped. | Cheap. This is the root of trust. |

---

## 3. Backend design

### 3.1 Schema v2 and migration (`schema.go`)

`schemaVersion` becomes `2`. `migrate()` is rewritten as **versioned steps, each in its own transaction, with `PRAGMA user_version` set inside that transaction.** SQLite rolls the pragma back with the transaction.

- **Fresh database** (`user_version = 0`): apply the full **v2 DDL** (`ddlV2`) in one transaction and set `user_version = 2`.
- **`user_version = 1`**: run `migrateV1toV2(tx, hook)` in one transaction (R7). It calls `hook(n)` after each numbered step when the hook is non-nil:
  1. `CREATE TABLE ca_new (id INTEGER PRIMARY KEY AUTOINCREMENT, role TEXT CHECK (role IN ('current','previous')), cert_pem …, key_pem …, subject …, serial …, not_before …, not_after …, fingerprint …, imported_from …, created …)`. **`role` is nullable in the schema on purpose:** switch-back needs a transient NULL (§3.4), and a NULL passes the CHECK. The only guard against a leftover NULL is structural invariant 1 (§3.3), which `Open` enforces.
  2. `INSERT INTO ca_new (id, role, …) SELECT id, 'current', … FROM ca` (0 or 1 rows; the id is kept, so it stays 1).
  3. `DROP TABLE ca`, then `ALTER TABLE ca_new RENAME TO ca`. This is the documented create/copy/drop/rename order. There are no FKs to rewrite (R3).
  4. `CREATE UNIQUE INDEX ca_one_per_role ON ca(role) WHERE role IS NOT NULL`.
  5. `ALTER TABLE certs ADD COLUMN ca_id INTEGER` (nullable; P1), then `CREATE INDEX certs_ca_id ON certs(ca_id)`.
  6. **Backfill in Go, inside the same transaction, `tx.*` only (R1):**
     - If a CA row exists, parse it once.
     - Read every `(id, status, cert_pem)` row into a slice, then close the rows cursor.
     - Then, for each non-quarantined row with non-NULL `cert_pem`, parse it and run `CheckSignatureFrom(caCert)` (R6). On success, `UPDATE certs SET ca_id = ? WHERE id = ?`.
     - Leave `ca_id` NULL on failure, on a parse error, or for quarantined rows. Unparseable certs are not an error.
  7. `PRAGMA user_version = 2`.
- `user_version ≥ 2`: no-op.
- After migration, `Open` runs **`checkStructuralInvariants`** (§3.3, invariants 1–3 only; these are cheap SQL with no crypto) and fails loudly if one is violated. The signature and staleness invariants (4–5) are **never** run at boot: an expired or oddly-EKU'd legacy row must not block the service. They run in tests.

`ddlV2` is the v1 DDL with the new `ca` shape, `ca_one_per_role`, `certs.ca_id`, and `certs_ca_id`. The v1 `ddl` constant is kept as `ddlV1` only for test fixtures (§5).

### 3.2 Store layer (`store.go`, `model.go`)

- `model.go`:
  - `CA` gains `Role string`, serialized as `json:"role"`.
  - `Cert` gains `CAID *int64` (`json:"caId"`), `CASubject *string` (`json:"caSubject,omitempty"`), and `Stale bool` (`json:"stale"`).
  - The cert list and detail queries fill these three fields with a `LEFT JOIN ca`.
- **Renames, compile-forced (R5):**
  - `GetCA` → `GetCurrentCA(ctx)`: `WHERE role = 'current'`.
  - New `GetCAByID(ctx, id)`.
  - New `GetPreviousCA(ctx)`, which returns `(nil, nil)` when there is no previous CA.
- **New tx-scoped helpers.** These are the only reads and writes allowed inside `WithTx` (R1):
  - `getCurrentCATx`, `getPreviousCATx`, `getCertTx`
  - `countActiveByCATx(tx, caID)`
  - `deleteCertTx`, `archiveCertTx(tx, id)`
  - `insertCATx(tx, ca, role) (id int64, err error)`: returns the new row's id
  - `setCARoleTx(tx, id int64, role *string)`: **always addresses the row by `id`, never by role**
  - `dropPreviousCATx(tx, id)`: the **only** function that deletes a `ca` row (R4, §3.4). There is no separate `deleteCATx`.
  - `setCertCAIDTx`
- `InsertCert` writes `ca_id` (new column in the INSERT at `store.go:239-243`).
- `InsertCA` (`store.go:363`) becomes an init/import-only helper. It refuses with `ErrCAExists` if **any** `ca` row exists, then inserts with `role = 'current'`.
- **Test seams:** `Store.hook` and `Store.genCA` (R7). Each Replace sub-step calls `s.runHook` with a **unique** name. `<i>` is the 0-based position in that sub-step's id-sorted row list.

  | Replace step (§3.4) | Hook name(s) |
  |---|---|
  | 1: per-row delete of old-previous active rows | `prev-delete:<i>` |
  | 1: per-row archive of old-previous active rows | `prev-archive:<i>` |
  | 1: archived-row delete + `dropPreviousCATx` | `retire-previous`, once, after these |
  | 2 | `demote` |
  | 3 | `insert-ca` |
  | 4: insert of the leaves re-issued under `previousStale` | `prev-reissue:<i>` |
  | 5 | `reissue:<i>` / `delete:<i>` |
  | 6 | `drop-previous`, emitted **only** when the drop actually happens |

  Switch-back emits `swap-null`, `swap-promote`, `swap-demote`, and `drop-previous`. Edit, Renew, and Delete emit `write` after their row changes and `drop-previous` when the drop happens.

  **`TestReplace_Atomicity`:**
  - **Two configurations,** each starting from a store that already has a previous CA with ≥2 active rows, plus ≥2 active rows under the current CA:
    - (a) `previousStale = delete`, `existing = reissue`
    - (b) `previousStale = reissue`, `existing = delete`
  - **Recording pass first:** run the configuration once with a hook that only records the emitted names. Assert that the recorded set covers every name in the table that is reachable in that configuration.
  - **Then, for each recorded name,** rebuild the same fixture, set the hook to fail at that name, run Replace, and assert an error plus a full `ca`+`certs` dump identical to the pre-call dump.

### 3.3 Invariant checkers (new, in `carotate.go`)

Both checkers use `s.db` and are never called inside a transaction.

**`checkStructuralInvariants(ctx) error`** is run by `Open` and by tests:
1. At most one `current` and one `previous` row, and no NULL-role row outside a transaction.
2. If any `ca` row exists, exactly one is `current`.
3. Every non-NULL `certs.ca_id` resolves to a `ca` row.

**`checkInvariants(ctx) error`** runs in tests only. It runs the structural checks, then:

4. Every row with non-NULL `ca_id` and parseable `cert_pem` passes `leaf.CheckSignatureFrom(ca)` (R6). This is expiry-insensitive and EKU-insensitive, and agrees with the backfill.
5. For every row returned by **`ListCerts`** (the production list path), `Stale == (CAID != nil && *CAID != current.ID)`.

### 3.4 Operations (`carotate.go`, new; `lifecycle.go`)

**Leaf issuance refactor (`lifecycle.go:159`).** `issueLeaf` is split:
- `issueLeafWith(ca *CA, fqdn, req, validityDays, expiryWarnDays)` does the pure crypto and never touches the DB.
- `issueLeaf` becomes `GetCurrentCA` + `issueLeafWith`.
- `Generate` and `Renew` set `ca_id = current.id` on the new row.
- **D8:** Renew keeps passing `defaultValidityDays`, and a test asserts it (§5).

**`dropPreviousIfUnusedTx(tx)`.** Called at the end of every transaction that archives, deletes, or re-signs rows. If a previous CA exists and `countActiveByCATx` returns 0 for it, it deletes the archived rows with that `ca_id` (P2), then calls `dropPreviousCATx(tx, id)`. `dropPreviousCATx` re-reads the row in the same transaction and returns `errRefuseDropCurrent` unless `role = 'previous'` (R4).

**Renew (FR-R5)** keeps its existing flow and adds `dropPreviousIfUnusedTx` inside its transaction. Renewing a stale certificate is the per-certificate "Re-issue under the current CA".

**Delete** (`lifecycle.go:357`) is rewritten to run inside `WithTx`: `getCertTx`, then `deleteCertTx`, then `dropPreviousIfUnusedTx`. The confirmation check is unchanged.

**Edit (FR-R6, P5).** `Store.Edit(ctx, id, req CertRequest, validityDays, expiryWarnDays)`:
1. Pre-checks run outside the transaction: the source exists and is not quarantined; the collision check. These give early, friendly errors. **The real guard is `InsertCert`'s own in-transaction duplicate-active check (`store.go:210-221`)**, so a concurrent collision still surfaces as `ErrDuplicateActive` → 409.
2. `issueLeafWith(current, …, validityDays)`.
3. `WithTx`:
   - **FQDN unchanged:** `ArchiveAllForFQDN(fqdn)` alone. It already covers the source when the source is active, so `archiveCertTx` is **not** called.
   - **FQDN changed:** `archiveCertTx(source.id)` if the source is active. The new FQDN is not archived, since a collision there is a 409.
   - Then `InsertCert` with `ca_id = current.id`, then `dropPreviousIfUnusedTx`.
4. Return `IssueResult` plus `PreviousDropped bool`.

**Replace (FR-R2, D1/D2/D5/D7), `Store.ReplaceCA(ctx, name, existing, previousStale *string, defaultValidityDays, expiryWarnDays)`:**

1. Validation, outside the transaction:
   - a current CA exists;
   - the name follows P6;
   - `existing` ∈ {reissue, delete, keep};
   - `previousStale` follows P4.
2. Crypto, outside the transaction:
   - `s.genCA(name)`, which is `GenerateNamedCA` in production (R7);
   - one `issueLeafWith(newCA, …, defaultValidityDays, expiryWarnDays)` per row that will be re-issued. Those rows are:
     - the P3 set when `existing = reissue`;
     - the old-previous stale set when `previousStale = reissue`.
   - The rows are read with `s.db` before the transaction.
3. `WithTx`, re-read inside the transaction. **Concurrency key:** the pre-read and the in-transaction read each build `snapshot{currentID, previousID (0 if none), p3 []rowKey, prevStale []rowKey}`, where `rowKey{id, status, caID}` and each slice is sorted by id. If the two snapshots are not `reflect.DeepEqual`, the call returns `ErrConcurrentChange` (409) and writes nothing. Otherwise:
   These steps run in exactly this order:
   1. **Retire the old previous CA** (only if one exists). For its active rows:
      - `previousStale = delete`: `deleteCertTx`;
      - `previousStale = reissue`: `archiveCertTx`, and the replacement leaves are held in memory for step 4.
      Then delete its archived rows (P2) and call `dropPreviousCATx(oldPrev.id)`. The `previous` role slot is now free.
   2. **Demote:** `setCARoleTx(current.id, 'previous')`.
   3. **Promote:** `newID := insertCATx(newCA, 'current')`. **Every re-issued row gets `ca_id = newID`, the return value, never a field of the `newCA` struct.**
   4. **Insert the leaves re-issued in step 1** with `ca_id = newID`.
   5. **Apply `existing` to the P3 set:**
      - reissue: `ArchiveAllForFQDN` + `InsertCert` with `ca_id = newID`;
      - delete: `deleteCertTx`;
      - keep: no-op.
   6. **`dropPreviousIfUnusedTx`.** After a blanket reissue or delete, nothing active is left under the outgoing CA, so it drops immediately and switch-back is no longer possible (the dialog warns about this; §5).
4. Audit log line (P9). The call returns `ReplaceResult{Reissued, Deleted, Kept, Clamped int, PreviousDropped bool}`.

**Switch-back (FR-R9, D9), `Store.SwitchBack(ctx, expiryWarnDays)`:**
- No previous CA → `ErrNoPreviousCA` (409).
- The previous CA fails `checkCAExpiry` → 409 (P7).
- Inside the transaction:
  1. `setCARoleTx(currentID, NULL)`;
  2. `setCARoleTx(previousID, 'current')`;
  3. `setCARoleTx(currentID, 'previous')`. All three address rows by id, using ids read at the start of the transaction.
  4. `dropPreviousIfUnusedTx`, so the swapped-out CA drops immediately if it signs nothing active (D9 "follows the n-1 rules").
- The NULL step avoids the partial unique index firing mid-swap. SQLite checks UNIQUE constraints per row.
- Audit log line.

### 3.5 Call-site inventory (verified; R5 makes the compiler enforce it)

| Site | Becomes |
|---|---|
| `handler.go:82` `logBoot` | `GetCurrentCA`. The log line gains `previous_ca=present/absent`. |
| `handler.go:282` `handleCAGet` | `GetCurrentCA` + `GetPreviousCA` + `countActiveByCA` (see §4 response shape). |
| `handler.go:316` `handleCAInit` (re-read after init) | `GetCurrentCA`. |
| `handler.go:325` `handleCARootGet` | `GetCurrentCA`. Root download and trust always serve the current CA (D6). |
| `handler.go:354` `handleCATrust`, `handler.go:425` `handleCATrustRemote` | `GetCurrentCA`. |
| `handler.go:669` `certAndCA` | **`GetCAByID(*c.CAID)`**. If `CAID` is NULL, return a new `ErrUnknownSigner` (409, "certificate's signing CA is unknown; re-issue it"). **This single change is FR-R3** for `haproxy.pem` and the bundle. `bundle.go:100,123` already take the CA as a parameter and are unchanged. |
| `handler.go` `handleCertFileGet` (`cert.pem`/`key.pem`) | Unchanged: serves the stored leaf PEM, which needs no CA. |
| `lifecycle.go:77` `InitNamedCA` | `GetCurrentCA`. With a previous CA present, `ErrCAExists` still applies. |
| `lifecycle.go:160` `issueLeaf` | `GetCurrentCA` + `issueLeafWith`. |
| `importer.go:84` `ImportCA` | **FR-R8:** match `legacyFP` against the current **or** previous fingerprint. On a match it is a no-op. On a mismatch it returns `ErrCAFingerprintMismatch` with the new remedy text (§3.6). |
| `lifecycle.go:113`, `importer.go:108` `InsertCA` | `InsertCA` (init/import-only, `role = 'current'`). |
| Importer leaf insert | `ca_id` is set to the id of whichever stored CA the leaf's `CheckSignatureFrom` succeeds against; otherwise NULL (the existing `import_warning` is kept). |
| Tests: 17 `GetCA` call sites (`importer_test.go`, `store_test.go`, `lifecycle_test.go`, `bundle_test.go`) | Mechanical rename to `GetCurrentCA`. |

### 3.6 Remedy text (`importer.go:32`)

`caReplacementRemedy` becomes:

> `use "Replace CA…" in the certmachine CA panel (POST /api/ca/replace) to install a new certificate authority and re-issue or retire existing certificates`

It is surfaced by:
- `lifecycle.go:89-90` (`ErrImportPending`);
- `lifecycle.go:146-147` (`ErrCAExpiringSoon`; also reword the comment at `:140-142`, since a rotation route now exists);
- `importer.go:90`.

Update in the same task:
- `lifecycle_test.go:85,196`, which currently assert the string `"DELETE FROM ca"`, to assert `"Replace CA"`;
- `handler_test.go:799-818`;
- `docs/certmachine.md:391-405` (replace the manual `sqlite3` procedure with the UI procedure; keep a "last resort" note that stresses stopping the binary).

Also update the comments at `lifecycle.go:27-33,348-356` for the FR-6 narrowing (Principle 2).

---

## 4. HTTP API (`handler.go`)

Routes added in `mountRoutes` (`handler.go:36-69`):
```
POST /api/ca/replace
POST /api/ca/switch-back
POST /api/certs/{id}/edit
```
Method-less fallthroughs:
- `/api/ca/replace` → `methodNotAllowed("POST")`
- `/api/ca/switch-back` → `methodNotAllowed("POST")`
- `/api/certs/{id}/edit` → `methodNotAllowed("POST")`

Update `TestAllowHeadersMatchRegisteredRoutes` (`handler_test.go:163`).

New sentinels go in `store.go`'s error block. `writeStoreError` already maps any package sentinel that is not validation/not-found to 409 (`handler.go:107-140`); add the new ones to that list:
- `ErrNoPreviousCA`
- `ErrPreviousStaleChoiceRequired`
- `ErrConcurrentChange`
- `ErrUnknownSigner`

Bad enum values and name-rule violations are `ErrValidation` → 400.

**`GET /api/ca`** (the existing `caResponse`, `handler.go:256-279`, extended; the existing fields are unchanged for compatibility):
```json
{ "exists": true, "id": 3, "subject": "...", "serial": "...", "notBefore": "...", "notAfter": "...",
  "fingerprint": "...", "importedFrom": null,
  "previous": { "id": 1, "subject": "...", "notBefore": "...", "notAfter": "...",
                "fingerprint": "...", "activeCount": 4 } }
```
`previous` is omitted when there is no previous CA. `activeCount` drives both the FR-R7 display and the D7 forced choice in the Replace dialog.

**`POST /api/ca/replace`**:
- Request: `{"name": "...", "existing": "reissue|delete|keep", "previousStale": "reissue|delete"}`, with `previousStale` optional (decoded as `*string`).
- Response 200: `{"ca": <GET /api/ca shape>, "reissued": n, "deleted": n, "kept": n, "clamped": n, "previousDropped": bool}`.

**`POST /api/ca/switch-back`**:
- Request: empty body.
- Response 200: `{"ca": <GET /api/ca shape>}`.

**`POST /api/certs/{id}/edit`**:
- Request: `{"fqdn": "...", "dnsSans": [...], "ipSans": [...], "validityDays": 397}`.
- Response 201: the same shape `POST /api/certs` returns, including `validityClamped`, **plus `"previousDropped": bool`**.

**Existing `POST /api/certs/{id}/renew` and `DELETE /api/certs/{id}`**: their responses gain `"previousDropped": bool`, since either can trigger the P2 drop.
- Renew: add the field to `issueResponseFrom`'s body (`handler.go:562`).
- Delete: currently 204 with no body (`handler.go:548`). It becomes **200 `{"previousDropped": bool}`**. Update the existing assertion at `handler_test.go:443`, which is listed in §6's allowed edits.

When `previousDropped` is true, the UI appends a notice to the operation's existing success toast (§5, `detail.ts`).

**Cert list/detail**: each cert gains `caId`, `caSubject`, and `stale` (§3.2).

---

## 5. Frontend (`web/certmachine/js/`)

- `types.ts`:
  - add `caId: number | null`, `caSubject?: string`, and `stale: boolean` to the cert type;
  - add `previous?` to the CA type;
  - add the `ReplaceResult` type.
- `api.ts`:
  - add `replaceCA(req)`, `switchBackCA()`, and `editCert(id, req)`, following the existing `renewCert` shape (`api.ts:104-110`);
  - **change `deleteCert` (`api.ts:118-124`)** to return `Promise<{ previousDropped: boolean }>`. It parses the JSON body, and the `res.status !== 204` special case is removed.
- `types.ts`: add `previousDropped?: boolean` to `CertMutationResponse` (`types.ts:73-78`). Renew and Edit return this type.
- `detail.ts`: at the renew call site (`:216-228`), the delete call site (`:270-278`), and the new edit call site, when `previousDropped` is true, **append to the existing single success toast** rather than firing a second one, which `@shared`'s `showToast` might overwrite. The appended sentence: "The previous CA no longer signed any active certificate and was removed, along with its archived certificates."
- **CA panel** (`ui.ts`, `render.ts`):
  - Show the previous CA (name, dates, "signs N active certificates"), per FR-R7.
  - **"Replace CA…"** dialog, built on the `trustdialog.ts` pattern (D1):
    - an "are you sure" step;
    - a name field;
    - a blanket radio (reissue/delete/keep);
    - a second radio (reissue/delete) shown **only** when `previous.activeCount > 0`. Its text says the previous CA is about to be removed (D7) and that **either choice also discards all archived certificates under that CA**; with "reissue", the new copies are the only ones kept (P2);
    - when the blanket choice is **reissue** or **delete**, a warning: "The current CA will be removed once no certificate uses it, so *Switch back* will not be available afterwards." Only **keep** preserves switch-back.
    - when active rows with an unknown signer exist, the text "N certificates with an unknown signer are included in this choice." For this, `GET /api/ca` gains `"unknownSignerActiveCount": n` at the top level.
  - After a successful replace or switch-back, show the **D6 reminder**: "Machines that trusted the old CA are unchanged. Use *Trust this CA…* for the new CA and redeploy the re-issued certificates." It includes a button that opens the existing trust dialog.
  - **"Switch back to previous CA"** button, visible only when `previous` exists, behind an "are you sure" confirm (D9).
- **List** (`listmodel.ts`, `render.ts`):
  - a **stale** badge, and an "unknown signer" badge for `caId === null` on non-quarantined rows;
  - a **"Stale only"** filter toggle, following the existing status filter pattern (FR-R4).
- **Detail** (`detail.ts`):
  - show the signing CA name and the stale badge (FR-R4);
  - **Re-issue** (calls the existing renew), **Edit**, and **Delete** actions. Stale certs show all three (D3). **Edit** is available on every non-quarantined cert (D4).
  - Edit opens a form pre-filled with the FQDN, SANs, and validity (defaulting to `default_validity_days` from `GET /api/config`), then posts to `editCert`.
- `listmodel.test.ts`: tests for the stale filter and the unknown-signer classification (run by `npm run test:web`).
- `make web` rebuilds and commits `js/bundle.js` and `bundle.css`, as the repo's other modules do.

---

## 6. Tests

All tests run under `go test -race ./internal/certmachine/...`, and every new tx-path test uses a context with a 10s deadline (R1).

**Unit / store**
- `TestMigrateV1toV2_Populated`:
  - build a v1 database from `ddlV1` plus fixture rows: 1 CA, active, archived, quarantined, one `import_warning` row that doesn't chain, and a mixed-case legacy FQDN;
  - run `migrate`;
  - assert `user_version = 2`, the CA has `role = current` and id 1, each row's `ca_id` follows P1, all 5 old indexes plus the 2 new ones exist, and `checkInvariants` passes;
  - assert a second `ca` row with `role = 'previous'` inserts OK, and a second `current` fails (the partial unique index).
- `TestMigrate_FreshIsV2` and `TestMigrate_Idempotent` (reopening does not re-run).
- `TestMigrate_FreshEqualsMigrated`: compares **structure, not `sqlite_master.sql` text**. Raw text can never match: after `RENAME`, SQLite stores the table as `CREATE TABLE "ca"`, and `ADD COLUMN` splices into the v1 text while keeping its inline `--` comments. For a fresh v2 database and a migrated v1 database, assert equality of:
  - the table-name set;
  - for each of `ca` and `certs`, `PRAGMA table_info` rows (name, type, notnull, dflt_value, pk), compared **as a set keyed by name**, since `ADD COLUMN` places `ca_id` last while `ddlV2` may place it elsewhere;
  - the index-name set, and for each index its `PRAGMA index_list` row (unique, partial) and `PRAGMA index_xinfo` key columns plus collation;
  - each index's `sqlite_master.sql` with whitespace collapsed. Index SQL is stored per statement, so partial-index `WHERE` clauses compare cleanly.
  - **`AUTOINCREMENT` (id-reuse guard):** `table_info` does not reveal `AUTOINCREMENT`, and `sqlite_sequence` already exists in v1 because of `certs`, so neither is a real check. The real check runs on **both** a fresh v2 database and a migrated v1 database, each starting with exactly one `current` CA at id 1 (the migrated fixture has one; the fresh one gets it through `setupCA`):
    1. insert a `previous` row, and assert id 2;
    2. drop it through `dropPreviousCATx`;
    3. insert another `previous` row, and assert id **3**, not 2.
- `TestMigrateV1toV2_FailureRollsBack`: on a `ddlV1` database, begin a transaction and call `migrateV1toV2(tx, hook)`, with the hook returning an error at step 3. Roll back, then assert `user_version = 1` and a full dump of `ca` and `certs` equal to the pre-migration dump (R7 seam).
- `TestOpen_ExpiredRowsStillBoot` (R6), required:
  - Build a v1 fixture containing an **expired leaf** (NotAfter in the past, signed by the fixture CA) and a leaf with **no ServerAuth EKU**. Then migrate and `Open`.
  - Assert that `Open` succeeds, both rows get `ca_id = 1` through the `CheckSignatureFrom` backfill, and `checkInvariants` (including invariant 4) passes.
- `TestReplace_KeepUnderExpiredOutgoingCA`:
  - **Building the fixture:** `Generate` and `Renew` refuse under an expired CA (`checkCAExpiry`, `lifecycle.go:143-149`), so the fixture is built directly:
    1. `newThrowawayCA(t, pastNotAfter)` (`pki_test.go:26`, the helper `setupCA` calls) provides the expired CA's cert and key; insert it as `current` the way `setupCA` does.
    2. Sign 2 leaves directly with `GenerateLeaf(caCert, caKey, req, days)`, which skips `checkCAExpiry`. `GenerateLeaf` clamps NotAfter to the expired CA's NotAfter, so these leaves have NotAfter before NotBefore. **That is expected and harmless:** the staleness and `CheckSignatureFrom` checks ignore time.
    3. Write each through `InsertCert` with `Status = active` and `CAID` set to the CA's id.
  - Then run `ReplaceCA(keep)` with `genCA` = 2048-bit, and reopen the store.
  - Assert that `Open` and `checkInvariants` both pass and that both kept rows show `stale = true`.
- `TestDropPreviousCATx_RefusesCurrent`: returns an error and deletes nothing (R4, Principle 2). This replaces any appeal to an "existing FR-6 test".
- `TestRenew_UsesDefaultValidity` (D8) and `TestEdit_UsesRequestedValidity` (P5, the FR-4 narrowing), both including the clamp to the CA's NotAfter.
- `TestEdit_*`, covering every P5 branch: FQDN unchanged; FQDN changed; collision → `ErrDuplicateActive`; quarantined; archived with a newer active row; validity out of range.

**Operations**
All operation tests set `Store.genCA` to a 2048-bit generator (R7).

- `TestReplace_{Reissue,Delete,Keep}`: counts and signers; a test-only `x509.Verify` of every freshly issued cert, following R6's pinned options; `checkInvariants`. `TestReplace_Reissue` uses 50 certs and **logs** the transaction duration. It asserts only a generous 3s bound, because `-race` on slow hardware (the repo has a `build-rpi` target) would make a 1s bound flaky.
- `TestReplace_PreviousStale{Required,Reissue,Delete}`: the 409 when required and omitted; the 400 for `"keep"`; each path drops the old previous CA together with its archived rows (P2).
- `TestReplace_NameCollision` (P6).
- `TestReplace_Atomicity`: fully specified in §3.2 (the hook-name table, two configurations, recording pass, then per-name injection). "Unchanged" means a full dump of every column of `ca` and `certs`, ordered by id, identical before and after.
- `TestSwitchBack_{NoPrevious409,Expiring409,Swap,DropsUnused}`.
- `TestAutoDrop_ArchivedRowsDeleted` (P2).
- `TestRandomizedSequence`:
  - Runs 100 random operations from {replace(keep|reissue|delete × previousStale), renew, edit, delete, switch-back, generate}, with fixed seeds 1..3, logged on failure.
  - `checkInvariants` runs after every step.
  - The 2048-bit `genCA` seam keeps it within `-timeout 300s` under `-race`.
  - Skipped under `testing.Short()`.

**HTTP** (`handler_test.go` pattern)
- Round trips for the 3 new routes.
- Each download of a stale cert chains to its own CA and not to the current one; this is FR-R3. The downloads are `cert.pem` and `haproxy.pem`, plus the bundle's CA entry.
  - The check is `CheckSignatureFrom` on the downloaded leaf against the downloaded or own CA.
  - A test-only pinned `x509.Verify` runs as well.
- `certAndCA` with a NULL signer → 409.
- `GET /api/ca` returns the `previous` block and `activeCount`.
- `TestAllowHeadersMatchRegisteredRoutes` is updated.

**Existing tests**
- Allowed edits only:
  - the `GetCA` → `GetCurrentCA` rename in 17 call sites;
  - the remedy-text assertions (§3.6);
  - **`store_test.go:42`**: its raw `INSERT INTO ca(...)` gains `role` with the value `'current'`. Without it the row's role is NULL, and the reopen that follows fails structural invariant 1;
  - **`handler_test.go:443`**: the delete status changes from 204 to 200 with a `previousDropped` body (§4);
  - `lifecycle_test.go:491`'s direct `DELETE FROM ca` keeps working unchanged (it is test-only raw SQL).
- No other existing assertion may change.

**Frontend**
- `npm run typecheck`
- `npm run test:web` (the new `listmodel.test.ts` cases)

**Observability**
- One test captures `log` output and asserts the P9 audit line on replace and switch-back.

---

## 7. Pre-mortem (deliberate mode)

1. **Deadlock through `s.db` inside a transaction (most likely).**
   - Scenario: the executor calls `Renew`, `GetCert`, or `DeleteCert` from inside `ReplaceCA`'s `WithTx`, and the tests hang.
   - Mitigation: R1, the tx-scoped helper set (§3.2), and 10s context deadlines on every tx-path test. Also, `go test -timeout 300s` is the verification command.
2. **The previous CA never drops, or drops too early.**
   - Scenario: archived rows pin it (the v1 plan's bug), or rows with a NULL `ca_id` are counted.
   - Mitigation: P2 counts active rows only; `TestAutoDrop_ArchivedRowsDeleted`; `checkInvariants`'s staleness equality.
3. **Migration corrupts a populated v1 database** (rebuild order, non-transactional steps, or a backfill that misattributes non-chaining rows).
   - Mitigation: §3.1's single-transaction versioned step; the `Populated`, `FailureRollsBack`, and `FreshEqualsMigrated` tests; `checkStructuralInvariants` on `Open`.
5. **The service refuses to boot after the upgrade.**
   - Scenario: a boot-time integrity check trips on an expired leaf, an expired kept-stale CA, or a legacy leaf without an EKU.
   - Mitigation: R6 (`CheckSignatureFrom`, never `Verify`, in production); `Open` enforces only the cheap structural invariants 1–3; `TestOpen_ExpiredRowsStillBoot`.
4. **The current CA gets deleted.**
   - Mitigation: R4, `dropPreviousCATx`'s in-transaction role re-check, and `TestDropPreviousCATx_RefusesCurrent`.

## 8. Risks and mitigations

| Risk | Mitigation |
|---|---|
| The replace transaction holds the IMMEDIATE lock too long (busy_timeout is 5s). | R2: all crypto runs before the transaction; the transaction only writes rows. Measure in `TestReplace_Reissue` with 50 certs and assert the transaction takes under 1s. |
| Rows change between the pre-transaction read and the transaction. | The row set is re-read inside the transaction; on a mismatch the call returns `ErrConcurrentChange` (409). |
| P2 deletes archive history the owner wanted. | Flagged for owner review (§2). The alternative (NULL out `ca_id`) is a one-line change in `dropPreviousIfUnusedTx`. |
| The stale manual remedy text tells operators to `DELETE FROM ca` on a two-CA database. | §3.6 rewrites the text everywhere, and the tests assert the new text. |

## 9. Acceptance criteria

1. `go test -race -timeout 300s ./internal/certmachine/...` passes, including every test named in §6.
2. `make test` passes repo-wide.
3. `npm run typecheck` and `npm run test:web` pass.
4. `grep -rn "GetCA(" internal/certmachine` finds no matches, meaning no un-migrated call sites remain.
5. The only production `DELETE FROM ca` statement is inside `dropPreviousCATx`: `grep -rnw "DELETE FROM ca" internal/certmachine --include=*.go | grep -v _test.go | wc -l` prints `1`, and that single match lies inside `dropPreviousCATx`. `deleteCATx` is folded into `dropPreviousCATx`, so it is not a second statement.
    **Executor rule:** do not quote the literal SQL `DELETE FROM ca` in any production comment. That includes the rewritten FR-6 comment at `lifecycle.go:348-356`; write "deletes the ca row" instead.
5a. `x509.Verify` is not called in production code: `grep -rn "\.Verify(" internal/certmachine --include=*.go | grep -v _test.go` prints nothing (R6).
6. `grep -n "REFERENCES" internal/certmachine/schema.go` finds no matches (R3).
7. The committed bundle is rebuilt: `make web-verify` passes.
8. **Manual browser check** against the local test DB (FRD §3a), recorded in `docs/MANUAL-VERIFICATION-certmachine-ca-replacement.md`:
   - replace with each blanket choice;
   - the stale badge and filter;
   - the edit form, pre-filled;
   - switch-back and its confirm;
   - the D6 reminder;
   - the previous CA panel;
   - the D7 second radio appearing only when needed.
   - Replacement never touches any machine's trust store.

## 10. ADR

- **Decision:** Option B. A two-row `ca` table with a `role` column; a partial unique index per role; `AUTOINCREMENT` ids; nullable `certs.ca_id` with no FK. All CA-changing operations live in a new `carotate.go`, run as short immediate transactions with crypto done beforehand, and are guarded by a `checkInvariants` oracle.
- **Drivers:** root-of-trust safety; safe migration of populated databases; mechanical testability.
- **Alternatives considered:**
  - A (a `ca_previous` singleton): cross-table signer lookup and row-copy of key material.
  - A′ (`ca_history`): contradicts D5.
  - Enabling FK enforcement: rejected because it reverses a recorded binding decision (`schema.go:21-22`) and changes every existing write path's behaviour for no benefit over `checkInvariants`.
- **Why chosen:** one lookup path for "who signed this"; switch-back is a role flip; the compiler enforces the call-site migration.
- **Consequences:**
  - FR-6 and FR-4 are narrowed explicitly.
  - Archive history under a dropped CA is deleted (P2).
  - `migrate()` becomes versioned, which also benefits future schema changes.
  - The legacy manual remedy is retired.
- **Pre-execution gate:** P2 confirmed by the owner (delete archived rows).
- **Follow-ups:**
  - Consider exposing the audit log in the UI.
  - Intermediate CAs and CRL/OCSP stay out of scope (FRD §5).

## Changelog (v4 → v5)

- **`TestReplace_Atomicity`:** a unique hook name for every Replace sub-step (`prev-delete`, `prev-archive`, `retire-previous`, `demote`, `insert-ca`, `prev-reissue`, `reissue`, `delete`, `drop-previous`), plus hook names for switch-back, edit, renew, and delete. It runs two configurations, uses a recording pass to find reachable steps, then injects a failure at each one.
- **`AUTOINCREMENT` check:** the vacuous `sqlite_sequence` existence check is replaced by the id-reuse guard, run on both fresh and migrated databases, each starting from one CA at id 1.
- **`TestReplace_KeepUnderExpiredOutgoingCA`:** the fixture is built directly (`newThrowawayCA` + direct signing + `InsertCert` with `CAID`), because `Generate` refuses under an expired CA.
- **`previousDropped` notice:** folded into the single success toast.

## Changelog (v3 → v4)

- **`TestMigrate_FreshEqualsMigrated`:** rewritten to compare structure (the `PRAGMA table_info`, `index_list`, and `index_xinfo` output, the index SQL, and `sqlite_sequence`), because raw table-SQL text cannot match after `RENAME`/`ADD COLUMN`. The id-reuse guard now inserts a previous row explicitly.
- **Delete 204→200 carried through the frontend:** `deleteCert` returns `{previousDropped}`, `CertMutationResponse` gains `previousDropped?`, and the toast is wired at the `detail.ts` renew, delete, and edit call sites.
- **Atomicity coverage:** added the `"retire-previous"` hook step and made the test's coverage of every step explicit.
- **Replace dialog:** now states that the D7 choice discards the old previous CA's archived rows (P2).
- **Expired-CA test:** `TestOpen_ExpiredRowsStillBoot` narrowed to the meaningful v1-fixture case. The expired-CA case moved to its own test, `TestReplace_KeepUnderExpiredOutgoingCA`.
- **Executor rule:** no literal `DELETE FROM ca` in production comments, which keeps AC5 exact.

## Changelog (v2 → v3)

- **Boot safety (R6):** production signer checks use `CheckSignatureFrom`, never `Verify`, following `importer.go:231-233`. `Open` enforces only structural invariants 1–3. Added `TestOpen_ExpiredRowsStillBoot` and pre-mortem 5.
- **Migration:** R1 now covers `migrate`, and the backfill reads everything before updating. There is an explicit `migrateV1toV2(tx, hook)` seam, plus the `FreshEqualsMigrated` schema-equality test and an `AUTOINCREMENT` sequence check.
- **Seams:** all consolidated in R7 (`Store.hook`, `Store.genCA`, `migrateV1toV2`'s hook). The randomized test is reduced to 3 seeds × 100 operations using 2048-bit keys.
- **Replace:** the steps are re-ordered and numbered unambiguously. There is an exact `ErrConcurrentChange` snapshot key, and re-issued rows take `ca_id` from `insertCATx`'s return value.
- **Switch-back:** `setCARoleTx` addresses rows by id; `role` is documented as nullable.
- **Edit:** the same-FQDN path uses `ArchiveAllForFQDN` only; `InsertCert`'s in-transaction check is named as the real collision guard.
- **`previousDropped`:** surfaced from Renew, Edit, and Delete. Delete changes from 204 to 200 with a body.
- **Replace dialog:** warns that reissue/delete removes switch-back and shows the unknown-signer count.
- **Allowed test edits:** added `store_test.go:42` (raw CA insert gains `role`) and `handler_test.go:443` (delete status).
- **AC5:** rewritten to a satisfiable single-`DELETE FROM ca` check; added AC5a (no production `Verify`).
- **Timing:** the replace transaction timing is logged, with a 3s bound.
- **P2:** raised from a follow-up to an **owner gate before execution**.

## Changelog (v1 → v2)

- **FK / migration**
  - Dropped the "real FK" claim; FKs stay off (R3); added `checkInvariants`.
  - Rewrote the migration as versioned, transactional steps with the correct rebuild order; `ca_id` is nullable and backfilled by signature check (P1).
- **Call sites**
  - Replaced the wrong call-site list with the verified inventory; `bundle.go` is unchanged, and FR-R3 is the one fix in `certAndCA`.
- **Transactions**
  - Added R1 (no `s.db` in a transaction) and R2 (crypto before the transaction), plus tx-scoped helpers and deadline-bounded tests.
- **Lifecycle**
  - FR-R5 now maps to the existing `Renew`.
  - Added the FR-4 narrowing for Edit validity and D8 tests.
  - Decided how archived and quarantined rows count (P2, P3) and pinned the Edit semantics (P5).
- **Schema**
  - Added the partial unique `role` index, `AUTOINCREMENT`, and the NULL-step switch-back swap.
- **API and UI**
  - Added the FR-R7 API and UI, the D6 reminder, the D9 confirm plus expiry check, the D1 name rules, the FR-R8 importer rules, and the `previousStale` status contract.
  - Specified all request and response shapes.
  - Added the 405 fallthroughs and the route-test update.
- **Tests and observability**
  - Named the atomicity seam and defined "unchanged" as a full table dump.
  - Retired `caReplacementRemedy`'s `DELETE FROM ca` text everywhere.
  - Added the frontend unit tests, an audit log line, and the pre-mortems for deadlock and archive pinning.
