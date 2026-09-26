# Mini-FRD: certmachine CA replacement and certificate editing

Status: **draft, not yet planned** (decisions recorded 2026-09-26)
Scope: `internal/certmachine`, `web/certmachine`

## 1. Goal

Let the operator **replace the root CA** from the UI (today it's a manual
`sqlite3 "DELETE FROM ca;"` procedure), choosing what happens to the
certificates the old CA issued, and let them **edit** any certificate's
SANs without re-entering its details.

Already done (2026-09-26): a CA gets a **name** at initialization (its Common
Name), and its files are named after it (root download, bundle entry, trust
anchor).

## 2. Decisions

| # | Decision |
|---|----------|
| D1 | **Replace CA…** in the CA panel: an "are you sure" dialog, a name for the new CA, and a blanket choice for the existing certificates (D2). |
| D2 | Blanket choice, applied to all existing certificates at once: **(1) Re-issue** under the new CA (same FQDN and SANs, new key and dates); **(2) Delete**; **(3) Keep as stale** (listed, flagged "issued by a previous CA"). |
| D3 | **Stale certificates** get per-certificate actions: **Re-issue** (under the current CA), **Edit** (D4, then re-issue), or **Delete**. |
| D4 | **Edit** is available on every certificate, stale or current, pre-filled with its current details: change the **FQDN**, add/remove/change **SANs**, and set the **validity length**, then re-issue with the current CA. Effectively a re-issue with new details, without re-entering everything. |
| D5 | **Only n-1 is kept:** at most one retired (previous) CA, alongside the current one, while any certificate it signed remains, so stale certificates stay downloadable and verifiable. No unending history. |
| D6 | Replacing the CA does **not** touch machines that trusted the old CA; the UI reminds the operator to trust the new one (Trust this CA…) and redeploy re-issued files. |
| D7 | **The previous CA drops off at the next replacement.** If certificates are still stale under it then, the replace dialog requires choosing, for those, re-issue or delete (keeping them isn't possible once their CA is gone). The previous CA is also removed as soon as no certificate uses it. |
| D8 | A plain **re-issue** (blanket on replace, or per certificate) uses the **current default validity**, not the certificate's original length; choosing a different length is what Edit is for. |
| D9 | **Switch back:** while a previous CA exists, a "Switch back to the previous CA" action makes it current again. Certificates signed by it become valid; those signed by the (now previous) CA become stale; nothing is re-issued or deleted automatically. The swapped-out CA then follows the n-1 rules (D5, D7). Confirmed with an "are you sure" dialog. |

## 3. Requirements

- **FR-R1 Schema:** the `ca` singleton (`CHECK (id = 1)`) holds at most **two** CAs: the **current** one and, optionally, the **previous** one (D5). Each certificate records the CA that signed it (`ca_id`). Migration maps the existing CA and all certificates onto this, with no behavior change until a replacement happens.
- **FR-R2** `POST /api/ca/replace` with `{name, existing: "reissue" | "delete" | "keep", previousStale?: "reissue" | "delete"}`, atomic: on any failure nothing changes (the old CA stays current). `previousStale` is required when certificates are still stale under the previous CA (D7).
- **FR-R3** Downloads (`cert.pem`, `haproxy.pem`, bundle) and chain checks use the certificate's **own** CA, so stale certificates still produce correct files.
- **FR-R4** List and detail views show a certificate's CA and a **stale** badge when it isn't the current one; the list can filter to stale certificates.
- **FR-R5** Per-certificate **Re-issue** under the current CA (a renew that also switches CA).
- **FR-R6** Per-certificate **Edit** (FQDN, SANs, validity length) then re-issue (FR-R5), for any certificate. A changed FQDN must not collide with another certificate's.
- **FR-R7** The previous CA is shown (name, dates, how many certificates it still signs) and removed automatically once it signs none.
- **FR-R8** The import wizard and legacy-import rules keep working against the current CA.
- **FR-R9** `POST /api/ca/switch-back`: swaps current and previous CAs atomically (D9); 409 when there is no previous CA.

## 3a. Testing (Go, no real machines)
Everything above is server-side and verified with Go tests against temp databases and freshly generated CAs:
- Each replace path (reissue / delete / keep; forced `previousStale` choice) and switch-back, checking the resulting CAs and each certificate's signer.
- Real chain checks with `x509.Verify`: every certificate, and every download (`cert.pem`, `haproxy.pem`, bundle), chains to the CA the database says signed it.
- Randomized sequences of replace / keep / reissue / edit / delete / switch-back, asserting after every step: at most two CAs; every certificate's CA exists; every certificate verifies against its own CA; "stale" is exactly "not signed by the current CA".
- Atomicity: a failure injected mid-replace leaves everything unchanged.
- HTTP endpoint tests, like remote trust's.
The UI is then checked in the browser against the local test database; replacement never touches any machine's trust store.

## 4. Open questions (for planning)
None at the moment (undo was decided as D9, switch back).

## 5. Out of scope
- Intermediate CAs, CRLs/OCSP, and multiple *simultaneously active* CAs.
