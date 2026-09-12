# FRD: Admin UI v2, unified identity, and module auth

Status: DRAFT, round 2. The **auth model** below (two-state modules, module
PINs, orthogonal API keys) was settled in discussion on 2026-09-11 and is the
next implementation round. The access matrix and module instances remain a
later round. Nothing here is implemented yet. Mark up freely.

## Problem

Today authorization is **method-based, not identity-based**. A session records
*who* logged in and *how* (`subject` + `methods[]`), but the gate only checks
whether the session's method intersects the module's accepted-method list.
Consequences:

- Any PIN holder can reach every PIN-gated module.
- Any LDAP user (in `required_groups`) can reach every LDAP-gated module.
- Any API key works on every `key` module.
- Any logged-in identity can register a passkey (the register routes only
  require *a* valid session), and that passkey then signs them into every
  passkey-enabled module — a cross-method escalation.

The method vocabulary also treats `"key"` as a peer of the interactive
methods, which it is not. Surfaced by menuserver: a module configured
`["key"]` presents a login page with **zero workable methods** — browser
sign-in is impossible. API keys are service authentication and belong in a
separate category from pin / username+password / passkey.

Identities are also entered by hand (PIN `name` fields in config) rather than
coming from a directory. And the admin UI is a single vertical column tuned
for phones, while the primary client is a 14–16" laptop.

## The auth model (settled 2026-09-11)

Each module is in one of **two states**:

1. **Open** — absent from `auth.modules`. Requests pass straight through;
   going to the page gets you in, exactly the pre-`security-fix` behavior.
   grocery stays here.
2. **Protected** — present in `auth.modules`. Login required.

When a module is protected:

- **LDAP is always offered.** It is the identity backbone; an LDAP session
  reaches every protected module (the matrix narrows this in a later round).
- **Passkey is offered when globally configured** (secure context + enrolled
  credential). A passkey is an alternate proof of the *same LDAP identity*,
  never a standalone principal.
- **PIN is offered only if the module has its own `pin_file`.** A module PIN
  is a **door code owned by the module**, not an identity credential. A PIN
  session grants access to *that module only*. This generalizes the existing
  `admin_pin` mechanism rather than inventing anything new.
- **API keys are orthogonal.** A valid `Authorization: Bearer` key works on
  any protected module **except admin** — no per-module opt-in needed, no
  login-page surface ever. A bearer header sent to an open module is simply
  ignored (open is open); an invalid key on a protected module is a 401.

Config sketch (replaces the accepted-methods lists):

```json
"auth": {
  "modules": {
    "todo":        { "pin_file": "./todo.pin" },
    "slideshow":   { "pin_file": "./slideshow.pin" },
    "menuserver":  {},
    "obsidianoid": {},
    "multissh":    {},
    "admin":       { "pin_file": "./admin.pin" }
  }
}
```

(admin is the one exception to the table above: it remains a special case
with its own notion of a PIN — the PIN is *required* and is the *only* way
in. LDAP does not grant admin, and API keys never satisfy it.)

### Consequences, deliberately accepted

- **PIN identity is dropped.** A door code identifies a module, not a person
  ("someone who knows todo's code"). Shared PINs never identified anyone
  anyway — two people with `1234` were indistinguishable — so this trades
  fake identity for real blast-radius scoping: knowing todo's PIN gets you
  todo and nothing else. Per-person PINs can return in the matrix round if
  ever wanted. Identity, when it matters, comes from LDAP only.
- **multissh is not special in code.** Uniform mechanism, deliberate config:
  the right policy is simply *no `pin_file` on multissh*, making it
  LDAP-identity-only. (An admin-page warning when a sensitive module has a
  door code is possible later polish, not architecture.)
- **Admin stays PIN-only this round.** LDAP does not grant admin; only the
  admin PIN does — until the matrix delivers a real admins group. This keeps
  today's behavior exactly. Admin is also the one protected module API keys
  must never satisfy.
- **LDAP is a single point of entry for pin-less modules.** With
  `ldap.cmdhome.net` down and passkeys not yet enrolled, menuserver /
  obsidianoid / multissh are unreachable (full login honestly fails, per the
  LDAP-down decision). Known failure mode; the escape hatch is built in — a
  module `pin_file` doubles as break-glass.
- **Passkey enrollment requires an LDAP-authenticated session.** A door code
  must never mint an identity credential. This closes the
  register-anywhere/use-anywhere escalation from the audit as a natural
  consequence of the model rather than a patch.

Session semantics: session grants become scoped — an LDAP/passkey session is
identity-scoped (all protected modules, pending matrix); a PIN session is
module-scoped (its module only). Cookies are host-only today, which scopes
PIN sessions almost automatically, but the gate enforces the scope
server-side regardless — never rely on the front end (or cookie scoping) for
security.

## Goals

1. **One identity = one LDAP account.** Display name and groups come from
   LDAP, never data entry. Passkeys are local credentials *bound to an LDAP
   identity*. Module PINs are not identity credentials at all (see above).
2. **Access matrix** (later round). Authorization becomes LDAP-groups ×
   module-instances, edited on the admin page, deny-by-default. Holding a
   credential is necessary but not sufficient.
3. **Login page modes.** The login screen flips between full login
   (username+password), PIN, and passkey — each tab shown only when actually
   available for that module (PIN only with a `pin_file`; passkey only in a
   secure context with an enrolled credential).
4. **API keys stay off the login page — and out of the method vocabulary.**
   Keys are service authentication, header-only, their own admin section
   (service principals). They are available *only* on auth-enabled modules.
5. **Admin UI v2 in React, desktop-first.** Cards lay out in a responsive
   grid that spreads horizontally on laptop/iPad widths instead of stacking
   into one column. Phone layout still works but is not the design target —
   grocery and todo are the only modules with a real phone use case.

## Non-goals

- No change to grocery remaining open — open modules bypass auth entirely.
- No self-service user management; the admin page is the only writer.
- No re-plumbing of the other modules' frontends to React in this pass.

## Sketch

### Identity & credential model

```
identity (key = LDAP username, e.g. "chris")
├── display name / groups   ← from LDAP (cached; refreshed on login/admin view)
└── passkeys[]              ← WebAuthn credentials, stored locally, optional

module (protected)
└── pin_file                ← optional door code, grants this module only

api_keys are separate: service principals, valid on protected non-admin modules.
```

### Access matrix: LDAP groups → modules (later round)

Decision (Chris): access is driven by **LDAP group membership**, against the
real lab directory at `ldaps://ldap.cmdhome.net` (currently missing groups
and users — populating it is part of rollout). The matrix maps groups to
module instances:

```json
"access": {
  "home": ["grocery", "todo", "slideshow", "obsidianoid-home"],
  "work": ["obsidianoid-work", "multissh"],
  "admins": ["admin"]
}
```

A user in both `home` and `work` gets the union — both obsidianoids.
Gate check becomes: module protected AND some group of the session's
identity grants the module. The two-state model above composes cleanly:
"protected" stays the on/off switch, the matrix narrows *which identities*;
API keys become service principals with their own matrix rows.

### Module *instances*, not hardcoded modules (later round)

The admin page must list modules **from the config**, not a hardcoded set.
Motivating case: two obsidianoids — one home, one work-restricted — with
different vault directories and different group access.

Honest correction: today's config does *not* quite allow this. Multiple
hostnames can route to the same module name, but they deliberately share one
handler and one config section (`buildDispatcher` builds each module name
once; `buildModule` switches on the seven fixed names). Two obsidianoids
need a schema change — named instances of a module *type*:

```json
"instances": {
  "obsidianoid-home": { "type": "obsidianoid", "vaults": [ ... ] },
  "obsidianoid-work": { "type": "obsidianoid", "vaults": [ ... ] }
},
"host_routing": {
  "notes.test":     "obsidianoid-home",
  "worknotes.test": "obsidianoid-work"
}
```

Existing single-instance sections keep working (a bare module name is an
implicit instance of its own type). The auth config, host routing, and the
admin page all key on instance names. This is the biggest schema change in
the FRD and should land before or with the matrix.

### Session TTL

Decision (Chris): replace the long sliding TTL with a **hard expiry at a
fixed local time of day** (default midnight): however late you log in, the
session dies at the next cutoff. Sliding refresh goes away or only operates
within the same day. Config: `"session": {"expires_at": "00:00"}` plus a
timezone (server-local by default).

### Login page

Tabbed selector (Full login / PIN / Passkey) driven by `/api/auth/mode` plus
client-side availability checks. Visual redesign is still open — Chris is
noodling on the look; don't start it from this FRD alone.

## Decided

- **Auth model**: two-state per module (open / protected); LDAP always when
  protected; passkey when configured; optional per-module `pin_file` door
  code scoped to its module; API keys orthogonal, valid on protected
  non-admin modules only. (2026-09-11; supersedes per-module method lists.)
- **PIN semantics**: module door codes, not identity credentials. Identity
  comes from LDAP only. Supersedes the earlier "PIN name references an LDAP
  username" idea; the "ambiguous PINs tolerated" caveat dissolves — there is
  nothing to be ambiguous about.
- **Passkey enrollment**: requires an LDAP-authenticated session, never a
  PIN (door-code) session.
- **Admin**: PIN-only until the matrix round; never satisfiable by API key.
- **LDAP-down behavior**: server-side metadata caching. Identity metadata
  (display name, groups) is cached in the auth data dir at last successful
  login; passkey login keeps working from cache, full login honestly fails
  while the directory is unreachable. Module PINs are unaffected (local).
- **Session TTL**: hard expiry at a fixed local time of day (see above).
- **Access driver** (later round): LDAP groups → module instances, against
  `ldaps://ldap.cmdhome.net`.
- **Admin module list**: enumerated from config (instances), never hardcoded.
- **Matrix default** (later round): deny-by-default. An identity whose groups
  grant nothing gets nothing on protected modules.

## Open questions

1. **Admin access under the matrix.** Does "admin" become a matrix column
   like any other (operator PIN remains the break-glass path regardless), so
   a trusted LDAP identity can also administer? (This round: PIN-only, see
   Decided.)
2. **React build.** web/admin gains a build step (esbuild or vite) with
   committed bundle vs. built-at-make-time. Repo currently embeds static
   assets; decide whether bundles are checked in (like multissh) or built.
3. **`pin_file` vs inline hash — RESOLVED 2026-09-11: `pin_file`.** Chris
   delegated the call ("bcrypt unless individual files are considered a
   better choice"); files are the better choice: one loader shared with
   admin.pin, the secret stays out of config.json (which the admin live-apply
   rewrites and which lives in the repo for the local profile), a bcrypt hash
   of a 4–6 digit PIN is offline-crackable in seconds so hashing buys almost
   nothing here, and a 0400 file can be set or rotated on the box with
   `echo`+`chmod` — no hashing tool — which preserves the break-glass
   property.
