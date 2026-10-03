# Phase 8: Punch List Fixes

Status: `In progress`. P8-1 and P8-4 are done; P8-2, P8-3, P8-5 and P8-6 are open and are to be fixed in this phase.
Branch: `bugfixes`
Sibling docs: `docs/plans/PLAN-ui-unification-phase7-punchlist.md` (Phase 7, D0–D9).
Section labels here are **P8-1, P8-2, …** so they never collide with Phase 7's D-numbers.

## Working rules for this phase

- Guidance that starts with `CMD>` is the owner's and is binding, as in the Phase 7 plan. The `CMD>` lines
  in this file are kept verbatim; the text around them has been brought into line with them.
- **`make test` is the standard full test** (it runs `go test -race ./...`). Run it before calling work done.
  It does not run the web tests: `make test-web` (typecheck plus the web suites) does, and `make check`
  runs everything (web-verify, test-web, gates, test).
- **Defer means "until Phase 8", and this is Phase 8.** Nothing here is parked for a later phase.
- Modules do not share sessions: each has its own login, session and timeout cycle, and no global cookie.

## Related Phase 8 work not tracked in this list

Phase 8 also carries the utuber headless-browser cookie-auth effort (IP-bound cookies make
pasted cookie files useless across networks). It gets its **own FRD** and is not a punch-list item.
Do not fold it in here.

## Punch list

| # | Module | Item | Severity | Status |
|---|---|---|---|---|
| P8-1 | certmachine | Edit dialog **Save** button does nothing | High: blocked SAN re-issue | **Done and signed off** (commit 79bf28f) |
| P8-2 | admin | LDAP form corrupts `required_groups` (DNs split on commas); admin "Test user login" disagrees with the real login | High | Open (autosave removed, Test configuration added; comma split and the test-box discrepancy remain) |
| P8-3 | auth / admin | Passkeys cannot be registered or used | High | **Done**: register in admin and passkey login on todo confirmed by the owner (2026-10-02) |
| P8-4 | auth | An LDAP/passkey login on one module was accepted by every module | High: contradicted the per-module session rule | **Done** (red/green test); owner confirmed it works |
| P8-5 | certmachine | "Show/Hide N expired/archived certificates" button does not hide or show | Medium | **Fixed in code** (measured in headless Chrome); awaiting owner browser check |
| P8-6 | certmachine | Delete should be a button on each cert card, behind an "Are you sure?" modal | Medium | **Built**: Delete on every card with an "Are you sure?" dialog; awaiting owner browser check |
| P8-7 | auth | API keys are global: one key is accepted on every protected non-admin module (not module-scoped) | Medium: contradicts the per-module isolation rule | Open; owner to circle back |
| P8-8 | smbedit | With many shares the list falls off the bottom of the screen with no scrollbar; the footer is pushed out of view | Medium | **Fixed in code**, measured in headless Chrome; awaiting owner browser check |
| P8-9 | haproxy | The haproxy module has no settings UI: CertMachine URL/API key/CA file, config/certs/crt-list/stats-socket paths, backup count, expiry warning days, OS override can only be set by editing the config file | Medium: violates "always a UI to do it for you" | **Built** (Settings tab, live apply); enabling the module itself and its hostname still needs host_routing, tracked separately as the general "Modules" admin control |

---

## P8-1: certmachine, Edit dialog Save button is inert (done)

Open a cert, click **Edit…**, change its SANs, click **Save**: nothing happened. The Save button sat in
the modal footer, a sibling of the `<form>` instead of a descendant, so a `type="submit"` button with no
form owner submitted nothing (`web/certmachine/js/detail.ts`, `renderEditForm()`). The backend
(`POST /api/certs/{id}/edit`, `Store.Edit`) was never the problem.

Fix: give the form an id and point the button at it with the `form` attribute, and rebuild the bundle.
Commit **79bf28f**. Signed off by the owner in a browser on 2026-09-30.

CMD> I would consider P8-1 already completely done and signed off.  

Not part of P8-1 and not requested: the Edit form pre-fills the server's default validity rather than the
cert's current validity.

---

## P8-2: admin, LDAP form splits group DNs on commas

**Reported behavior.** LDAP configured in the admin module "looks correct" but login never works.

**Verified root cause.** `web/admin/js/main.ts` `saveLdap()` builds `required_groups` with
`value.split(",")`. A group DN such as `cn=household,ou=groups,dc=cmdhome,dc=net` is stored as four
entries (`cn=household`, `ou=groups`, `dc=cmdhome`, `dc=net`), none of which can ever match. The form
also autosaves on every keystroke (`autoSaveLdap`), so a half-typed value is persisted and can
overwrite hand-edited config.

**Related facts, not bugs in themselves.**
- `internal/platform/auth/ldap.go` `findGroups` searches under `base_dn` and compares group **cn**
  values, so `required_groups` must be plain names (`household`) and `base_dn` must be an ancestor of
  both people and groups (`dc=cmdhome,dc=net`). Neither the form nor `unified-webapp-example.json` says so.
- 389-ds Directory Manager's DN is `cn=Directory Manager` with no suffix.
- Passkey routes need an `ldap:admin` grant on the session (`gate.go`); an admin-PIN-only session gets
  `requires_ldap` by design, which read like "LDAP is broken". The passkey card now opens a sign-in dialog
  for this (see P8-3).

**Fix.** Use a separator that cannot appear in a name (one group per line) or validate that each entry
parses as a plain cn; stop autosaving on keystroke (save on blur or explicit button); document the
`base_dn`/group-name rules next to the fields and in the example config; make the passkey card explain
that an LDAP login is needed instead of implying LDAP is down. **Acceptance:** save `household` and a
DN-shaped value through the form and read the config back unchanged; the passkey card states why it is
disabled for a PIN-only session.

**Open observation (owner, 2026-09-30).** In admin, **Test configuration** passes, but **Test user login**
fails for the owner's credentials, while the owner's real LDAP login on todo works (confirmed by logging out
of todo and back in). So LDAP and the login path are fine and this one admin control is wrong. Checked so
far: run against the saved config, the handler behind it (`handlePostLDAPTest`) returns `ok` for `chris` and
`katie` and `bad_credentials` for `cdelezenski`, which has no LDAP entry. Still needed: the exact username
typed and the message shown. Suspects, none confirmed: the running service's in-memory admin copy of the LDAP
settings differing from the file after a form save; the browser autofilling the wrong credential, because the
test box's inputs use `autocomplete="off"` (the sign-in dialog added for passkeys uses
`autocomplete="username"` / `"current-password"` and worked). To be fixed in this phase.

Status of P8-2 as of 2026-09-29: the LDAP form no longer autosaves (`web/admin/js/main.ts`), and an
admin-side **Test configuration** button (`POST /api/ldap/check`) verifies connect, service bind, base-DN
search and required groups with no user account. The comma-splitting of group DNs and the missing
guidance on `base_dn` / group-name rules are still open. The session form and the security matrix still autosave.

---

## P8-3: passkeys (done, confirmed 2026-10-02)

CMD> to be clear: this is the plan where we should fix it and stop deferring the fix

**Owner clarification (2026-09-30).** "Defer" meant defer until Phase 8, and this is Phase 8. Passkeys are
to be designed and fixed here, not carried to a later phase. Do not paper over it and do not hide it in the
UI in the meantime: the Passkeys card keeps showing its real failure states until the feature works.

**Why it does not work (code and config; the sign-in half confirmed in a browser).**
- Passkeys are only enabled when `auth.passkey.rp_id` is set; it is empty. Registration answers 400
  "passkeys not available" and no module login page offers a passkey option. The owner confirmed in a
  browser that signing in through the new LDAP dialog works and registration then fails with a 400 and an
  error toast.
- Browsers only allow WebAuthn on HTTPS or `localhost`. The app serves plain HTTP (no `tls_cert` /
  `tls_key`); the admin page already warns about this. (hero is reached over HTTPS through haproxy, so the
  origin the browser sees may already be HTTPS; the app behind it does not know that.)
- `rp_origins` must list each module's exact origin, and `rp_id` must be a parent of every one.
- Session cookies are host-only (`cookie_domain` empty) and identity grants are module-scoped, so a
  passkey login is per module host, as the design requires. A shared `cookie_domain` is not a fix: it is
  one global value, it is against the design, and certmachine is on a different domain
  (`certmachine.cmdhome.net`), so its cookie would be rejected by browsers.

**What exists today (keep unless the design replaces it).**
- Backend step-up: `POST /api/auth/login` with `method: "ldap"` is accepted on admin only when the request
  already carries a valid admin-PIN session; it keeps `admin_pin` and adds `ldap:admin` under the LDAP
  user's identity (`internal/platform/auth/handlers.go`, test `TestHandleLoginLDAPStepUpOnAdminKeepsAdminPIN`).
- Admin UI: the Passkeys card opens a modal LDAP sign-in when the session has no LDAP identity, then
  continues into registration with a toast for each outcome (`web/admin/js/main.ts`, `askLdapLogin` /
  `registerPasskey`). The sign-in half works; the registration half fails for the reasons above.

**Design decisions to settle first, then build.**
1. Who uses passkeys, on which hosts, over what transport. HTTPS is a prerequisite: either terminate it in
   the app (`tls_cert` / `tls_key`, likely a CertMachine certificate with a SAN per module host) or tell the
   app it is behind a TLS-terminating proxy so the origin it checks is the HTTPS one.
2. `rp_id` and the exact `rp_origins` list, including `certmachine.cmdhome.net`.
3. ~~How sessions span modules~~ **Settled by the owner (2026-09-30): they don't.** Each module has its own
   login, session and timeout cycle; no shared or global cookie (`auth.cookie_domain` stays empty). A
   passkey is registered and used per module host.
4. Whether admin may ever accept a passkey; today `OfferedMethods("admin")` is `["admin_pin"]` only.
5. What the card shows when passkeys are not configured. Owner preference: show the truth of the current
   behaviour; do not hide or disable the feature as a workaround.

**Progress 2026-10-02.** The owner signed in through the new dialog and got "Signed in as chris, but passkeys
are still unavailable (HTTP 400)": the server answers 400 "passkeys not available" while `auth.passkey.rp_id` is
empty. Most of this item is configuration, not code: HTTPS is already terminated by haproxy (go-webauthn checks
the browser's origin string, not the request scheme), so the app does not need `tls_cert`/`tls_key`. Set in the
owner's live config: `rp_id: "cmdhome.net"` (a parent of every origin, including `certmachine.cmdhome.net`) and
`rp_origins` for the four hosts that have haproxy certs (`https://admin.hero.cmdhome.net`,
`https://todo.hero.cmdhome.net`, `https://utuber.hero.cmdhome.net`, `https://certmachine.cmdhome.net`). After the
restart, `/api/auth/mode` offers `passkey` on todo and certmachine and stays PIN-only on admin. **Awaiting the
owner's browser test of registration (admin) and passkey login (another module).** Modules without an HTTPS cert
(grocery, smbedit, ...) cannot use passkeys until haproxy serves them over TLS. Fixed: when passkeys
are not configured every passkey route answers 409 with a plain message (and `code: passkeys_not_configured`),
checked before the LDAP requirement; the admin card explains it, disables registration and offers no pointless
sign-in.

**Gap found 2026-10-02 (owner): there is no way to configure passkeys in the UI. FIXED 2026-10-02** (panel `#panel-passkey-settings`, route `PUT /api/config/passkey`, server-side validation, hot apply; Go tests in `internal/admin/passkey_config_test.go`, web tests in `web/admin/js/passkeyform.test.ts`). The owner's live config already has `rp_id` `cmdhome.net` and four https origins set by hand earlier; the panel now shows them. Original gap text follows. The admin module has panels
for LDAP, session, API keys and the security matrix, but `auth.passkey.rp_id` / `rp_origins` can only be set by
editing the config file, and the "not configured" message used to point there. Fix (to build, owner confirmed):
a **Passkey settings** panel in admin next to LDAP: `rp_id`, an allowed-origins list with add/remove and a "use
this page's address" helper, explicit Save (no autosave), `PUT /api/config/passkey` hot-applied through the same
`mutateAuth`/`applyAuth` path as LDAP (the policy swap rebuilds the passkey service), with server-side
validation (origins https, or http only for localhost; each origin's host is `rp_id` or a subdomain of it) and
plain-text errors. The "not configured" 409 message and the Passkeys card then point at this panel, not at the
config file.

**Passkey login could never work. FIXED 2026-10-02 (awaiting the owner's browser test).** Cause: the login
page and the server disagreed. The page's passkey button posted `{}` (no username) to
`/api/auth/passkey/login/begin`, but the server only implemented the targeted flow and demanded a username
(`ErrPasskeyIdentityRequired`), so it answered a generic 400 (logged as `bad_credential`) and the page hid it
behind "Passkey sign-in isn't available." Registration was never the problem. Fix, in `internal/platform/auth`:
(1) `BeginPasskeyLogin("")` now runs the discoverable flow (`BeginDiscoverableLogin`), and `FinishPasskeyLogin`
resolves the identity from the credential ID and checks the returned userHandle against the enrolment; a typed
username still runs the targeted flow. (2) Registration requests a resident key (it already did: the new test
pins it). (3) Plain, specific answers: no passkey for the given account 404 "No passkey is registered for that
account. Sign in another way, then register one in Admin > Passkeys."; failed assertion 401 "That passkey was
not accepted."; expired/unknown/replayed challenge 400 "The passkey sign-in timed out. Try again."; the 409
not-configured answer is unchanged. The log carries `passkey_begin_failed`, `passkey_no_credentials`,
`passkey_verification_failed`, `passkey_challenge_expired`, `passkey_not_authorized`. (4) The finish handler now
also applies the global login throttle and re-checks the LDAP directory (`Authorize`, required groups) before
issuing the session, failing closed; neither existed before. (5) `login.html` sends the typed username only when
there is one and shows the server's message. Tests: `passkey_ceremony_test.go` drives register then login through
the real handlers with a software ES256 authenticator. **Owner action:** if the existing fingerprint passkey was
not created as a discoverable credential, discovery-style login will not offer it: delete it in Admin >
Passkeys and register it again (new enrolments are discoverable), or type the username before pressing the
passkey button to use the targeted flow.

**Acceptance.** Register a passkey for an LDAP user over HTTPS and see it listed; log in to a PIN-protected
module with it; confirm removing the user from the required group revokes it; certmachine unaffected.

CMD> not "eventual" .. fix it during this phase.  This was a misunderstanding.  I meant defer until phase 8 and this is phase 8.

---

## P8-4: identity grants are not module-scoped

**Owner rule (2026-09-30).** Each module has its own login, session and timeout cycle. Logging into
todo does not grant grocery, or any other module. No global cookie.

**Reported/verified behavior.** Logged in through todo's LDAP login and presented that same session
cookie to other modules' hosts; the gate let it through on every one:

| Module host | no cookie | todo-login cookie |
|---|---|---|
| todo / grocery / utuber / smbedit / multissh | 401 | 200 |

**Root cause (verified).** `authorizingGrant` (`internal/platform/auth/session_scope.go`) returns the
session's identity grant (`ldap` / `passkey`) as authorizing *any* non-admin module. Isolation today is
an accident of cookie scope: `cookie_domain` is empty, so each host's cookie is host-only and the browser
never sends todo's cookie to grocery. Anything that presents the cookie to another host (a shared
`cookie_domain`, the same host serving several modules, a hand-built request) bypasses it.

**Fix (done, 2026-09-30; owner stated the rule as a design fact).** Scope grants per module: an LDAP or passkey
login records the module it happened on (as door-code grants already do via `pinGrant(module)`), and the
gate accepts an identity grant only for that module. Keep the admin step-up as the one deliberate
exception, scoped to admin. Idle and maximum-session clocks are already per module (`ModuleSeen`).
**Implemented as:** identity grants are `ldap:<module>` / `passkey:<module>` (`identityGrant`,
`identityGrantFor` in `session.go`); `grantsAllow`, `authorizingGrant` and `signedOutOf` use only the
requested module's grant; a bare `ldap` / `passkey` from an older token authorizes nothing, so existing
sessions must log in again; logout drops only that module's grants; the passkey routes require
`ldap:<module>`. Tests: `internal/platform/auth/session_isolation_test.go` (written failing first, then
green), plus updated fixtures in the existing auth and dispatcher tests. Live check: a todo login gets 200
on todo and 401 on grocery, utuber, smbedit and multissh.

**Acceptance (met):** a todo login cookie gets 401 on every other module host, in a test that sends it
explicitly; per-module login still works; logging out of one module leaves others untouched; existing
gate tests updated to the scoped behavior. Owner confirmed in a browser (2026-09-30): logging into one module
has no effect on the others.

CMD> appears to be working now

---

## P8-5: certmachine, the expired/archived show/hide button does nothing

**Reported behavior (owner, 2026-09-30).** Show/hide of stale certs does not work.

**Suspected root cause (read from the code and CSS; not yet confirmed in a browser).**
`web/certmachine/js/render.ts` (`appendRowGroup`) hides the de-emphasized list with
`deemphasizedList.hidden = true` and flips it on click, and relabels the button
"Show/Hide N expired/archived certificate(s)". But the list is an `ul.cert-list`, and
`web/certmachine/js/cert.css` gives `.cert-list` `display: flex`. An author `display` rule overrides the
browser's built-in `[hidden] { display: none }`, so the list is never actually hidden: the button's text
toggles and nothing else changes. `web/shared/css/components.css` already documents this exact trap and
fixes it for `.ui-queue-progress[hidden]`; certmachine's `.cert-list-deemphasized[hidden]` has no such rule
(only `.cert-trust-ssh[hidden]` does).

**Which control.** The toolbar also has a separate **Stale only** toggle (`ui.ts`, a client-side filter
by CA staleness). Its logic reads correct. Owner to confirm whether it is the expired/archived button or
the Stale-only toggle that misbehaves; this entry assumes the button.

**Fix.** Add `.cert-list[hidden] { display: none; }` (or scope it to `.cert-list-deemphasized[hidden]`).
**Acceptance:** with expired or archived certs present, the list starts collapsed; Show expands it, Hide
collapses it, and the count and label stay right in both the flat and the grouped-by-domain views. Add a
test that fails while the list stays visible after `hidden = true` is set (a DOM or CSS assertion), then
passes after the fix.

---

## P8-6: certmachine, Delete on each cert card with an "Are you sure?" modal

**Request (owner, 2026-09-30).** The delete button belongs on each cert card, and it should open a modal
dialog asking "Are you sure?".

**Today.** Delete exists only inside the cert's detail dialog (`detail.ts`, **Delete...**), and it asks
for the cert's FQDN to be typed before **Delete permanently** enables (`renderDeleteConfirm`).

**Design to settle (owner).** Whether the new modal replaces the typed-FQDN confirmation or keeps it for
the cases where it matters (an active cert, or one that signs other things). A plain "Are you sure?" is
quicker; typed confirmation is the stronger guard against deleting the wrong cert from a long list, which
is the case a per-card button makes more likely. Recommendation: plain confirm for archived, expired and
stale certs; typed confirmation for an **active** cert.

**Work.** A **Delete** button on every card in `render.ts` (not hidden behind Details), wired to the
shared `confirmDialog` from `web/shared/ts/modal.ts`. The dialog names the cert (FQDN, serial, expiry) and
says what deleting does, including the existing "previous CA no longer signs anything" cleanup
(`previousDropped`). Success and failure both produce a toast. The detail dialog's Delete stays or is
folded into the same flow, not duplicated with different behavior.
**Acceptance:** Delete on a card opens the modal; Cancel and Escape do nothing; Confirm deletes, the list
refreshes, and a toast reports it; deleting the wrong row by a stray click is not possible without
confirming.

---

## P8-7: API keys are not module-scoped

**Owner expectation (2026-10-02).** API keys were thought to be per module. Same principle as P8-4: each
module has its own login and credentials, and one module's credential grants nothing on another.

**Verified behaviour.** `internal/platform/auth/gate.go` (~line 203): every protected non-admin module
accepts a valid bearer API key unconditionally, "no per-module opt-in"; admin never accepts one. The config
shape is `auth.api_keys: [{name, hash}]` (`config.NamedHash`), with no module field. So a key created for
one client (for example the haproxy editor talking to CertMachine) is also valid on todo, grocery, utuber,
smbedit and every other protected module. The `-gen-api-key -name <name>` flag and the admin "Generate key"
action store only a name and a hash; `-name` is a label, not a scope.

**Fix direction (needs owner decision; not started).** Add an optional module scope to a key entry (for
example `modules: ["certmachine"]`; empty could mean all, to keep existing keys working, or could be
refused), have the gate accept a key only on its listed modules, show and set the scope in the admin Generate
key action and in `-gen-api-key` (a `-modules` flag), and add a red-first test like P8-4's
`session_isolation_test.go`: a key scoped to certmachine gets 401 on every other module host. Decide what an
unscoped existing key does (breaking vs. compatible).

---

## P8-8: smbedit, many shares fall off the bottom with no scrollbar

**Reported (owner, 2026-10-02).** Add enough shares and they fall off the bottom of the screen; no vertical
scrollbar appears. The page should scroll, with the footer staying at the footer. (A follow-on to Phase 7 D4,
"smbedit page scrolls below the footer": that fix left the opposite symptom.)

**Root cause (verified by measurement).** `web/smbedit/src/styles.css`: `.layout { height: 100% }` inside
`#root`, which had no height, so the percentage resolved to `auto` and the grid grew to its content (3727px in a
633px window with 40 shares); `body { height: 100vh; overflow: hidden }` clipped it; `.main-content`
(`overflow-y: auto`) never overflowed because it had grown to fit, so it never scrolled; the footer sat at
3727px, off screen.

**Fix.** `#root { height: 100% }`. Re-measured in headless Chrome with 40 shares: layout 633px (= viewport),
main area scrolls (536px visible of 3630px), footer flush at the bottom, no page scroll range; with 2 shares the
footer is still at the bottom. Rebuilt `web/smbedit/js/bundle.css` (the only tracked artifact that changed).
Regression guard `web/smbedit/src/layout.test.ts` (suite `smbedit-layout`) asserts the height chain and the
scrolling region in the stylesheet; shown to FAIL with the fix removed and PASS with it.

**Acceptance.** Owner confirms in a browser: with many shares the list scrolls, the header and the Save & Restart
footer stay put, and the footer is flush with the bottom of the window.

---

## P8-9: haproxy module settings have no UI

**Owner rule (2026-10-02).** Except in extreme circumstances there should always be a UI component to do it for
you; no feature may require hand-editing a file (memory: feedback-no-hand-editing-features).

**Gap (found while closing the rule out, not yet raised by the owner on this module).** Every setting of the
haproxy module is only editable in `unified-webapp.json`: `certmachine.url`, `certmachine.api_key`,
`certmachine.ca_file`, `os`, `config_path`, `certs_dir`, `crt_list_path`, `stats_socket_path`, `service_name`,
`backup_keep`, `expiry_warn_days`. The editor itself is a UI, but the editor's own settings are not, and the
module cannot be turned on at all without a `host_routing` entry and a `haproxy` block added by hand (the
FRD 9 module-settings list was written without a UI home for them).

**Fix direction (needs a short design and owner approval before building).** A Settings tab in the haproxy
module: CertMachine URL, API key (write-only field, shown as set/not set), CA file, a "Test connection" button
(list call plus an ETag-verified pull of a harmless cert, reporting plainly), and the path/backup/expiry
settings with the driver's defaults shown as placeholders; explicit Save, no autosave; applied without a
restart where the module can rebuild its driver/client live, otherwise the UI says exactly that and offers a
restart control rather than telling the owner to run a command. Also decide how the module gets enabled without
hand editing (an admin "Modules" control for host routing and the settings block is the general fix and would
help every module).

**Built (2026-10-02).** Settings tab in the haproxy module: grouped fields, write-only API key with Set/Not set
and Clear, driver defaults as placeholders, explicit Save with Discard and an unsaved indicator, Test connection
(list call, plain failure classes, no key leakage). Stored in `<data_dir>/settings.json` (0600, only changed
fields), the config block is optional starting values. Save rebuilds driver, CertMachine client, cert store,
applier and stats verifier live under an RWMutex (including `os`; an unsupported OS keeps the scoped 503 with
the reason shown). Nothing needs a restart. Not covered here: turning the module on and its hostname still
needs a `host_routing` entry (the general "Modules" admin control is a separate item), and the test is a list
call only, not an ETag-verified pull.

**Acceptance.** Every `haproxy` setting can be viewed and changed from the UI with validation errors in plain
text, the API key is never returned, and nothing in the guide tells the operator to edit a file except as an
explicitly labelled convenience.

---

## Adding to this list

Append new items as **P8-n** rows in the table plus a section below, each with: reported
behavior, verified root cause, fix, test gap, acceptance. Record the root cause only once it is
verified, and mark it "suspected" otherwise.
