# Manual verification — certmachine CA replacement and certificate editing

Status: **PENDING OWNER SIGN-OFF**

Everything that could be gated by a command **has** been (`go test -race`,
`npm run typecheck`, `npm run test:web`, the grep-based acceptance checks in
`.omc/plans/certmachine-ca-replacement.md` §9). This file holds only what is
left: the checks that need a live server and a real browser. AC8 of that plan
discharges to this file and nowhere else.

**None of these has been run.** They are not simulated, inferred, or claimed
anywhere else.

## Setup

This exercises `POST /api/ca/replace`, `POST /api/ca/switch-back`, and
`POST /api/certs/{id}/edit` against a real SQLite file, so use a throwaway
copy of the local test config rather than the one under version control —
`local-test/config.json` and `local-test/test.pin` are not touched by this
checklist.

```sh
cd /opt/unified-webapp

# A private copy of the local-test profile, pointed at its own database.
cp local-test/config.json /tmp/certmachine-verify.json
mkdir -p /tmp/certmachine-verify-data
sed -i 's#./local-test/data/certmachine/certmachine.db#/tmp/certmachine-verify-data/certmachine.db#' \
  /tmp/certmachine-verify.json

# Reuse the existing certmachine PIN (333333) that local-test/setup.sh
# already wrote to local-test/certmachine.pin -- config.json's certmachine
# entry actually points at local-test/test.pin, so point the copy at
# whichever PIN file you already have. If neither exists yet:
#   umask 077 && printf '333333\n' > /tmp/certmachine-verify.pin
# then also update the "certmachine" entry under "auth.modules" in
# /tmp/certmachine-verify.json to point at it.

make run CONFIG=/tmp/certmachine-verify.json
```

Then browse to the `certmachine.test` hostname (add it to `/etc/hosts` first
if `local-test/setup.sh` hasn't already, per its own instructions) and log in
with the PIN. Seed a handful of certificates through **New certificate**
before starting the checklist below — several steps need certificates
already signed by a CA that is about to become "previous."

`BASE=http://certmachine.test:8080` for any `curl` spot-checks against
`GET /api/ca`.

---

## 1 — Replace, blanket "re-issue"

With at least two active certificates signed by the current CA and no
previous CA yet, **Replace CA…** → name the new CA → blanket choice
**Re-issue under the new CA** → submit.

While the request is in flight, required: the progress bar is a single
determinate bar (not a spinner) that runs from 0% up to 100% over the whole
operation, never jumping backwards or sitting indeterminate at any point;
the status line next to it names the current phase and shows the percent
(e.g. "Generating the new certificate authority key… 0%", "Generating
certificate keys 3 / 12 — 34%", "Saving… 98%", "Done — 100%").

Required: every previously-active certificate reappears in the list signed
by the new CA (no **Stale** badge), the success toast reports the reissued
count, and — because a blanket reissue leaves nothing signed by the outgoing
CA — the toast's added sentence ("The previous CA no longer signed any
active certificate and was removed…") appears, and the CA panel shows **no**
previous-CA block afterward.

## 2 — Replace, blanket "delete"

Seed fresh certificates, then **Replace CA…** with blanket choice **Delete**.

Required: those certificates are gone from the list entirely (not archived,
not stale), the toast reports the deleted count, and the same "previous CA
… removed" sentence appears for the same reason as step 1.

## 3 — Replace, blanket "keep"

Seed fresh certificates, then **Replace CA…** with blanket choice
**Keep as-is (marked stale)**.

Required: those certificates keep their FQDN/SANs unchanged but now show a
**Stale** badge; the CA panel now shows a previous-CA block (subject,
validity window, and an active-certificate count matching what was kept);
**no** "previous CA … removed" sentence this time, since something active
still depends on it.

## 4 — Stale badge and "Stale only" filter

Following on from step 3: on the certificate list, confirm the **Stale**
badge is visually distinct from (and can appear alongside) the expiry-based
badges (valid / expiring soon / expired) — it is not a replacement for them.
Toggle **Stale only** in the toolbar: only stale rows remain; toggle it off
and they return.

## 5 — Edit form, pre-filled, and re-issue clears staleness

Open a stale certificate's detail view. Click **Edit…**. Required: the form
opens pre-filled with that certificate's current FQDN, SANs, and a validity
defaulting to the server's configured `default_validity_days` — not the
validity the certificate was originally issued with, if different. Change
something small (e.g. add a SAN) and submit.

Required: the new row is signed by the *current* CA (no **Stale** badge).

Then, on a **different** stale certificate, click **Re-issue** instead
(no edit). Required: same result — it comes back signed by the current CA,
stale badge gone — confirming Re-issue alone (the existing renew action)
is a valid way to clear staleness, without going through Edit.

## 6 — Second (previous-CA) choice appears only when needed

Set up two cases and confirm the dialog differs between them:

- **With** an active certificate still signed by the previous CA (e.g. right
  after step 3's "keep"): **Replace CA…** must show the second radio group
  ("re-issue" / "delete", no "keep" option), with text stating the count of
  affected certificates and that either choice also discards that CA's
  archived certificates.
- **Without** one (e.g. right after step 1's blanket reissue, before any new
  "keep" replace): **Replace CA…** must **not** show that second radio group
  at all.

## 7 — The "switch back will not be available" warning

Open **Replace CA…** with a previous CA present. Toggle the blanket radio
through all three choices and confirm the warning line ("The current CA will
be removed once no certificate uses it, so Switch back will not be available
afterwards.") is visible for **Re-issue** and **Delete**, and hidden for
**Keep as-is**.

## 8 — Switch back, with confirmation

With a previous CA present (from a "keep" replace), click
**Switch back to previous CA**. Required: an "are you sure" confirmation
appears first, naming what will happen (the current CA becomes previous,
the previous becomes current). Confirm it.

Required: the CA panel's current-CA identity and the previous-CA block swap;
certificates that were stale under the (old) previous CA are no longer
stale, and any certificate that was current under the (old) current CA now
shows **Stale** if it's still signed by that CA and something else is
current.

Then try Switch back again with **no** previous CA present at all (e.g.
right after a blanket-reissue replace dropped it) and confirm it is refused
with a clear error rather than silently doing nothing.

## 9 — The D6 trust reminder, after both replace and switch-back

Immediately after a successful **Replace CA…** (any blanket choice) and,
separately, after a successful **Switch back**, confirm the same reminder
dialog appears: *"Machines that trusted the old CA are unchanged. Use 'Trust
this CA…' for the new CA and redeploy the re-issued certificates."* Its
button opens the existing **Trust this CA…** dialog directly, without
needing to close this one and hunt for the button in the CA panel first.

## 10 — The previous-CA block in the CA panel

With a previous CA present, confirm the CA panel shows its subject, its
validity window, and how many active certificates still depend on it — and
that this number matches what `GET /api/ca` reports under `previous.activeCount`:

```sh
curl -s "$BASE/api/ca" | python3 -m json.tool
```

Cross-check: the count shown in the panel, the count used to decide whether
step 6's second choice appears, and the count in this JSON must all agree.

## 11 — The previousDropped notice appears from every operation that can cause it

Beyond Replace and Switch-back (already covered above), confirm that
**Re-issue**, **Edit…**, and **Delete…** on the *last* remaining active
certificate signed by the previous CA each independently trigger the same
added sentence in their own success toast. Do this at least once for each of
the three actions, starting fresh each time (a "keep" replace, then use only
that one action to remove the last active dependency on the previous CA).

## 12 — Replacement never touches any machine's trust store

Before doing any replace or switch-back in this checklist, install the
*original* CA's certificate into a real client's trust store (a browser or
system trust store on a separate machine, per [§6](../docs/certmachine.md#6-trusting-the-root-ca)).
After every replace and switch-back performed above, confirm on that same
client:

- the original CA's entry in its trust store is **still there**, unchanged
  — replacing certmachine's stored CA never reaches out to any other
  machine;
- a certificate freshly re-issued under a *new* CA is **not** trusted by
  that client until you separately run **Trust this CA…** for the new CA on
  it (this is the D6 reminder's whole point — confirm the failure mode it
  warns about actually occurs if you skip it).

---

Record the results of all twelve items in the pull request before merging.

all 12: Approved

