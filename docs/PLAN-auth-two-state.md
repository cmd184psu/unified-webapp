# PLAN: Two-state module auth (open / protected)

Status: APPROVED 2026-09-11 (Chris) — execution started 2026-09-11 (ralph, branch `security-fix`).
Date: 2026-09-11
FRD: docs/FRD-admin-identity.md — section "The auth model (settled 2026-09-11)" + "Decided" list
Branch: work continues directly on `security-fix` (operator decision 2026-09-11; the
gate/auth code this round modifies exists only there). No new branch.
Mode: RALPLAN-DR, deliberate.

---

## 1. RALPLAN-DR summary

### Principles

1. **Server-side enforcement only.** Cookie host-scoping and login-page tab logic are UX;
   every authorization decision is made in the gate against the session's grants. A
   `pin:todo` grant must be rejected on obsidianoid by code, not by cookie accident.
2. **Two states, one mechanism.** A module is open (absent from `auth.modules`) or
   protected (present). No third state, no per-module method lists, no module special-cased
   in code (multissh included). Admin is the single, explicit exception.
3. **Hard break over compatibility shims.** One operator, one config file. A targeted boot
   error that names the fix beats dual-format parsing that lives forever.
4. **Broken features over hypothetical hardening.** The menuserver `["key"]` dead-end
   (login page with zero workable methods) is the bug that motivates this; the plan is
   judged by the regression matrix, not by threat-model completeness.
5. **Fail loudly at boot.** Misconfiguration (legacy method lists, unreadable pin_file,
   protected modules with no LDAP configured) is a fatal, self-explanatory boot error —
   never a silently-open or silently-bricked module.

### Decision Drivers (top 3)

1. **Fix the method-based authorization holes now**: any-PIN-reaches-every-PIN-module,
   register-passkey-from-any-session escalation, and the `["key"]` browser dead-end.
2. **Minimal diff on a working gate.** gate.go's structure (route order, no
   ResponseWriter wrapping for multissh's Hijacker) is load-bearing and stays; only the
   authorization decision inside step 5 and the claims vocabulary change.
3. **One-operator home lab**: no fleet migration, no config back-compat obligations, no
   session-continuity guarantees across the deploy (re-login once is acceptable).

### Viable Options

**Decision A — config migration posture**

- **A1 (chosen): hard break with targeted boot error.** `auth.modules` values become
  objects; a custom unmarshaller detects a legacy array value (or a legacy `auth.pins`
  list) and fails boot with a message naming the module and the exact new syntax.
  - Pros: one parser, one schema, zero permanent complexity; misconfig is impossible to
    miss; migration is a 2-minute edit of one file.
  - Cons: old config will not boot until edited; old README/USERGUIDE snippets go stale
    (fixed in step S5).
- **A2: dual-format tolerance** (`json.RawMessage` per module; arrays interpreted as
  "protected, pin_file only if 'pin' was listed and a global pin exists"…).
  - Pros: old config boots.
  - Cons: the interpretation is inherently lossy (old `pins` are identity PINs, new
    pin_files are door codes — there is no faithful mapping), two code paths forever,
    and the operator never learns the new schema. **Invalidated**: the semantics changed,
    not just the syntax; silent reinterpretation would be wrong, not merely ugly.

**Decision B — grant encoding in the session token**

- **B1 (chosen): scoped grant strings in the session claim.** Values become
  `"ldap"`, `"passkey"`, `"admin_pin"`, plus `"pin:<module>"` (e.g. `"pin:todo"`).
  Unknown/legacy values (bare `"pin"`, `"key"`) parse fine and grant nothing.
  **Deliberate addition (pre-deploy token cutover):** the claim's JSON key is renamed
  `methods` → `grants`. Without this, a token minted under the old semantics carrying
  `"ldap"` (which reached only modules listing `"ldap"`) would silently *widen* at deploy
  to every protected module — exactly the silent-legacy-tolerance pattern principle 3
  condemns. The rename makes every pre-deploy token inert (empty `grants` ⇒ no access ⇒
  one forced re-login for everyone, which Driver 3 already declares acceptable) with zero
  runtime version-checking code. The `/api/auth/login` and `/api/auth/session` *response*
  bodies keep their external `"methods"` key (login.html only checks `res.ok`; minimal
  churn).
  - Pros: token format, cookie, JWT plumbing, `accumulate()` shape all survive; one
    cookie holds identity + several module grants; pre-deploy tokens are invalidated by
    construction, not by a guard that can be forgotten.
  - Cons: stringly-typed scoping (mitigated by tiny helpers + tests); one forced
    re-login at deploy. Note: *browser-real* cross-host grant accumulation only happens
    when `cookie_domain` is set to a shared parent domain — with today's host-only
    cookies each host carries its own jar; the union semantics are still enforced and
    tested server-side (S3, e2e case 9).
- **B2: structured claim** (`{identity_methods: [...], modules: [...]}`).
  - Pros: self-describing. Cons: new claim schema, all existing sessions invalidated,
    more churn in session.go/tests for zero behavioral gain. Rejected: B1 delivers the
    same enforcement with a fraction of the diff.
- **B3: one cookie per module** (scope by cookie name/path).
  - Rejected outright: relies on client-side scoping, which principle 1 forbids as the
    enforcement mechanism, and breaks the accumulate-in-one-browser requirement.

**Decision C — hard session expiry at fixed local time (`"session": {"expires_at": "00:00"}`)**

- **C1 (chosen): explicitly DEFER to a small follow-up round.** It is fully orthogonal to
  grant scoping (expiry policy touches `exp` computation and sliding refresh; grants live
  in the claim body regardless), nothing is broken today about TTL, and it drags in
  timezone/clock testing that would dilute this round's regression focus. Deferring keeps
  this round's diff reviewable and its test matrix honest.
- **C2: include it now.** Pros: one deploy instead of two; it is on the FRD Decided list.
  Cons: unrelated failure modes (DST, server timezone) mixed into an auth-semantics
  deploy; if midnight-expiry misbehaves it would be indistinguishable from a scoping bug
  during shakeout. Rejected for this round; it is first in Follow-ups.

---

## 2. Pre-mortem: three ways this ships broken, and the mitigations baked in

**PM1 — Admin lockout.** The schema/claims rework accidentally breaks the `admin_pin`
path (e.g. admin falls into the generic protected branch and starts demanding LDAP, or
the exactly-one-PIN-source validation rejects the current config) and the operator cannot
reach the admin page — the break-glass path is the casualty.
*Mitigations baked in:* legacy top-level `admin_pin`/`admin_pin_file` keys remain honored
unchanged (S1); admin gets its own explicit, first branch in the gate's authorization
step, written before the generic branch (S3); "admin unchanged" is a named regression in
the unit suite (S3) **and** the curl e2e checklist (S6), including admin login with the
existing `local-test/admin.pin` and API-key-rejected-on-admin.

**PM2 — Stale-session poisoning.** Browsers hold pre-deploy cookies whose claims say
`"pin"` or `"ldap"`. A grant parser that assumes the `pin:` prefix (`strings.Split` and
index without checking) panics; a leftover `intersects()` call treats legacy `"pin"` as
satisfying some module; or a pre-deploy `"ldap"` token — which under old semantics
reached only modules listing `"ldap"` — silently widens to every protected module.
*Mitigations baked in:* the claim key rename `methods`→`grants` (Decision B1) makes
every pre-deploy token inert by construction — old tokens parse to an empty grant set
and 401 into a re-login; grant helpers are additionally written total — any string that
is not exactly `"ldap"`, `"passkey"`, `"admin_pin"`, or `"pin:"+module grants nothing
(S2); a dedicated unit test feeds a legacy-shaped token (old `methods` claim carrying
`["pin"]`, `["ldap"]`, `["key"]`) through the new gate and asserts 401, no panic
(S2/S3).

**PM3 — A surviving method-intersection path leaks scope.** The old `EffectiveMethods` /
`intersects` / `containsMethod` machinery lingers somewhere (passkey ceremony guards, the
mode endpoint, a test helper used by production code) and one path still authorizes by
method intersection — `pin:todo` sails through on obsidianoid, or a bearer key satisfies
admin because admin has "a matrix entry" like any other module.
*Mitigations baked in:* S2 **deletes** `Policy.EffectiveMethods` and the slice-intersection
authorization helpers rather than deprecating them, so any remaining caller is a compile
error; the API-key check is structurally placed inside the non-admin branch only (S3);
the cross-module cases (todo PIN on obsidianoid, bearer on admin) are named acceptance
criteria with both unit and curl coverage (S6).

---

## 3. Implementation steps

All work directly on branch `security-fix`.

**Green-tree cadence (honest version):** retyping `AuthConfig.Modules` and deleting
`PINs` in S1 necessarily breaks compilation outside the config package
(`internal/platform/auth/policy.go` copies both fields; `internal/admin/handler.go`
round-trips them), and S2's deletions (`EffectiveMethods`, `checkPIN`, the retyped
`Policy.Modules`) are still consumed by gate.go and handlers.go — same package, but S3
files — so the auth package cannot compile at an S2-only boundary either. The invariant
is therefore: **S1+S2+S3 land as one verifiable unit**, with the first full `make test`
(go test -race ./...) green at the **end-of-S3** boundary. S2 and S3 remain separate
work descriptions inside that unit. Per-step checkpoints inside the unit:
S1 — `go test ./internal/platform/config/...` green;
S2 — the auth package's full file set **minus gate.go and handlers.go (and their
tests)** builds; the policy/validate/session/pin unit tests are written in S2 but first
*run* green at the S3 boundary; the mechanical `internal/admin` fixes and test
retargeting (see S2 Files) are likewise **written in S2 and first compile/pass at the
end-of-S3 (unit) boundary**, because all three admin files import
`internal/platform/auth` (apply.go, build.go, handler.go) and auth stays red until S3.
From the end of S3 onward, every step boundary (S3, S4, S5, S6) leaves the full
`make test` green.

### S1 — Config schema: two-state `auth.modules`, per-module `pin_file`, hard-break errors

**Files:** `internal/platform/config/config.go`, `internal/platform/config/config_test.go`
(and any config fixtures under the config package).

**Changes:**
- `AuthConfig.Modules` becomes `map[string]ModuleAuthConfig` with
  `type ModuleAuthConfig struct { PinFile string \`json:"pin_file"\` }`. Present (even
  `{}`) = protected; absent = open.
- Custom `UnmarshalJSON` on `ModuleAuthConfig` (or on the map type): a legacy JSON array
  value produces a targeted error, e.g.
  `auth.modules.todo: per-module method lists were removed; use {} (protected) or {"pin_file": "./todo.pin"} — see docs/FRD-admin-identity.md`.
- Remove `AuthConfig.PINs` (`auth.pins`). Detect the legacy key at decode time and fail
  boot with: `auth.pins was removed; identity comes from LDAP, door codes are per-module pin_file`.
  **The detector must be marshal-invisible**: the admin live-apply path
  (internal/admin/apply.go) marshals the whole `AuthConfig` back into config.json via
  `json.MarshalIndent`, so a plain tombstone field would write `"pins": null` on the
  first admin save and brick the *next* boot on this very error. Use
  `PINs json.RawMessage \`json:"pins,omitempty"\`` (a nil RawMessage has len 0 and is
  omitted from output) — or, equivalently, a shadow-struct decode pass inside Load that
  the persisted type never carries. Named round-trip test required (see verification).
  **Explicit `"pins": null` edge (decided):** a literal `"pins": null` in the file
  decodes to the RawMessage bytes `null` (len > 0) and therefore fires the removal
  error. **Accepted as-is** — fail-loud and self-explanatory: `null` only appears when
  a human or tool wrote the legacy key, and the error names the fix; treating it as
  absent would be a small silent-tolerance carve-out for no operator benefit. (The
  live-save path can never produce it: nil marshals to omitted, per above.)
  `AuthConfig.APIKeys`, `AdminPIN`, `AdminPINFile`, `Session`, `LDAP`, `Passkey`,
  cookie fields: unchanged.
- `expandAuthPaths` additionally walks `Modules` and resolves each non-empty `PinFile`
  relative to the config file dir with `~` expansion (same treatment as
  `AdminPINFile`/`DataDir` today). While config.go is open here, also **export
  `expandRelativeTo` (or add an exported wrapper)** — the live-apply pipeline in
  `internal/admin` needs it in S4 and cannot call the unexported helper.
- `modules.admin.pin_file` is accepted as the new spelling of the admin PIN file: config
  load treats it as an admin-PIN *source* alongside the legacy top-level keys (the
  exactly-one-source rule moves to validation in S2).

**Verification:** `go test ./internal/platform/config/...` — new cases: two-state parse;
legacy array value → exact error text; legacy `pins` key → exact error text; pin_file
relative-path expansion; `modules.admin.pin_file` populates the admin source;
explicit `"pins": null` → fires the removal error (decided above);
**marshal-invisibility round-trip**: unmarshal a valid (pins-free) config, marshal it
back, assert the output contains no `"pins"` key and re-loads cleanly. (The full
admin-live-save → reload-written-config → boots round-trip is asserted again at the
admin tier in S4.)

### S2 — Policy, validation, and the grant vocabulary

**Files:** `internal/platform/auth/policy.go`, `validate.go`, `session.go`, `pin.go`
(+ their `_test.go` files); plus the **mechanical** type-shape fixes in
`internal/admin/handler.go`/`apply.go` (Modules map type, PINs field/route plumbing
deletion — including the `POST /api/pins` / `DELETE /api/pins/{name}` handlers, which
reference `cur.PINs` and cannot survive the field deletion; behavioral editor work is
S4) per the green-tree cadence above. This mechanical license explicitly extends to
`internal/admin`'s **test assertions**: retarget them to the new shapes so the admin
package's tests *pass* at the unit boundary, not merely compile — mechanical
retargeting only, no new behavioral coverage (that is S4's job).

**Changes:**
- `Policy.Modules` becomes `map[string]ModulePolicy` (`PinFile string`, absolute).
  Drop `Policy.PINs`. **Delete** `EffectiveMethods`, `modulesUseMethod`, and the
  authorization uses of `intersects`/`containsMethod` (compile errors flush every caller;
  gate/handlers are fixed in S3, tests alongside).
- New `Policy.OfferedMethods(module) []string` — drives *only* `/api/auth/mode` and
  login-handler method enforcement, never request authorization:
  admin → `["admin_pin"]` (plus `"pin"`? no — keep exactly `["admin_pin"]`, which
  login.html already maps to the PIN tab); protected non-admin → `["ldap"]`, plus
  `"passkey"` when the policy's passkey service is configured, plus `"pin"` when the
  module has a `PinFile`; open module → `[]`. `"key"` never appears.
- `BuildPolicy`: passkey service is constructed when `Passkey.RPID != ""` and at least
  one protected non-admin module exists (passkey is offered wherever protected once
  globally configured).
- `ValidatePolicy` rewrite:
  - unknown-module check unchanged;
  - any protected **non-admin** module ⇒ `auth.ldap.url` must be set (LDAP is the
    identity backbone; error names the module);
  - every configured `pin_file` is stat'ed at boot: must exist and pass the `&0077 == 0`
    permission check (same rule and message style as `admin_pin_file` today) — fail loud;
  - admin PIN source: exactly one of top-level `admin_pin`, top-level `admin_pin_file`,
    or `modules.admin.pin_file` may be set; if admin is routed, at least one must be;
  - `api_keys` may be empty (keys simply don't authenticate); passkey config optional.
- `session.go`: grant vocabulary. The claim's JSON key is renamed `methods` → `grants`
  (Decision B1's cutover: every pre-deploy token parses to an empty grant set and is
  inert; no version-check code). Constants/helpers:
  `pinGrant(module) = "pin:" + module`; `isIdentityGrant(g)` true for `"ldap"`/`"passkey"`;
  `grantsAllow(claims, module)` (used by the gate in S3): admin ⇔ `"admin_pin"` present;
  otherwise identity grant present or `pinGrant(module)` present. Unknown strings
  (including legacy `"pin"`, `"key"`) grant nothing — total functions, no parsing panics.
- `accumulate()` rework for scoped grants:
  - door-code login (`pin:<m>`): folds into **any** existing valid session without
    touching its subject (a door code adds a module grant, never an identity); with no
    prior session, creates an anonymous session (`Subject == ""`) holding just that grant;
  - identity login (`ldap`/`passkey`) onto an anonymous (pin-only) session: adopts the
    subject, keeps the pin grants;
  - identity login with a *different* existing subject: replaces the session outright
    (current behavior, grants dropped);
  - same subject: union without duplicates (current behavior);
  - `admin_pin` is classified as **interpretation A — its own identity, not a door
    grant**: admin PIN login authenticates as subject `admin` and follows the identity
    rules above (replaces a different-subject session, unions with an existing `admin`
    session); it never folds into an identity session the way `pin:<m>` does. This
    matches today's behavior exactly ("admin unchanged").
- `pin.go`: extract the file branch of `checkAdminPIN` into
  `checkPINFile(path, pin) (bool, error)` — re-read per attempt, permission re-check,
  constant-time compare, error text never contains the PIN. `checkAdminPIN` keeps its
  signature and delegates; the named-PIN table path (`checkPIN` over `NamedHash`) is
  deleted with `Policy.PINs`.

**Verification (per the green-tree cadence — S2 sits inside the S1+S2+S3 unit, so
`go test ./internal/platform/auth/...` does not compile until S3's gate.go/handlers.go
rework lands):** S2's checkpoint is that the auth package's full file set **minus
gate.go and handlers.go (and their tests)** builds. `go test ./internal/admin/...`
with its mechanically-retargeted assertions first passes at the S3 (unit) boundary
alongside the auth package tests — internal/admin imports internal/platform/auth, so
it cannot compile before S3 either. The policy/validate/session/pin unit tests are
**written in S2** and first run green at the S3 boundary — new cases: OfferedMethods per state; accumulate matrix (door onto
anonymous, door onto identity, identity onto anonymous, identity replace); **named
stale-subject case: door-code login onto a pre-deploy token (old-format `methods`
claim, non-empty subject) — the subject is preserved but confers no access: resulting
grants are exactly `["pin:<m>"]`, identity-gated routes still 403, and
`/api/auth/session` reports the stale subject (cosmetic, accepted)**; `grantsAllow`
including legacy `["pin"]`/`["key"]` claims → deny; validation errors
(protected-without-ldap, bad pin_file perms, admin source multiplicity).

### S3 — Gate authorization + login/passkey handlers + login page tab logic

**Files:** `internal/platform/auth/gate.go`, `handlers.go`, `login.html`
(+ `gate_test.go`, `handlers_test.go`, `login_page_test.go`,
`cmd/server/dispatcher_auth_test.go`, `cmd/server/dispatch_test.go` as needed).

**Changes:**
- `Gate` step 2 (mode): respond with `p.OfferedMethods(module)` — shape stays
  `{"methods":[...]}`.
- `Gate` step 3 unchanged (`protected := hasEntry || module == "admin"`; open modules
  pass everything through — a bearer header on an open module is thereby ignored, no code
  needed).
- `Gate` step 5 rewritten, ordered and explicit:
  1. `module == "admin"`: **no API-key check ever**; session cookie accepted iff
     `grantsAllow(claims, "admin")` (i.e. `admin_pin` grant). Sliding refresh preserved.
  2. otherwise (protected non-admin): `checkAPIKey(r)` first, unconditionally — a valid
     bearer satisfies any protected non-admin module with no per-module opt-in. An
     invalid/absent key falls through silently (unchanged helper); then the session
     cookie is accepted iff `grantsAllow(claims, module)`; sliding refresh preserved.
  3. nothing authenticates → existing 401/login-page behavior (step 6) unchanged.
  Invariants preserved: no ResponseWriter wrapping anywhere (multissh Hijacker), refresh
  via plain `http.SetCookie` before `next.ServeHTTP`.
- Gate-owned route table: session-required passkey routes
  (`/api/auth/passkeys` GET, `register/begin`, `register/finish`, `passkeys/{id}` DELETE)
  gain a requirement beyond "valid session": claims must contain the `"ldap"` grant.
  Enforced in `Gate` (extend the `rt.session` precondition to a per-route required-grant
  field) so handlers keep assuming it holds. A valid session without `"ldap"` gets 403
  `"passkey management requires a full (LDAP) login"` — distinguishable from the plain
  401. This closes the door-code→passkey escalation server-side.
- `handleLogin` rewrite:
  - method enforcement against `OfferedMethods`, with one explicit mapping:
    **login.html posts `method:"pin"` on every module, including admin**, while
    `OfferedMethods("admin")` is `["admin_pin"]` — so the enforcement rule is: posted
    `"pin"` is acceptable iff the module offers `"pin"` **or** `"admin_pin"` (i.e. on
    admin, posted `"pin"` selects the admin-PIN path, exactly as today's special case at
    handlers.go:88-94). Literal set-membership on the posted string alone would break
    admin login; this mapping is the guard. A module without a pin_file rejects `"pin"`
    (open modules are unreachable here anyway);
  - `"pin"` on admin: `checkAdminPIN` exactly as today → grant `admin_pin`, subject
    `admin`; the secondary named-PIN fallback on admin is deleted with the PIN table;
  - `"pin"` on a module with `PinFile`: `checkPINFile` → grant `pinGrant(module)`,
    anonymous subject (accumulate handles folding). **Error path mirrors admin FR-M2
    (handlers.go:120-130):** a `checkPINFile` *config* error (file deleted after boot,
    permissions drifted, unreadable) is a loud 500 carrying the fix-naming error text,
    logged with a new distinct reason `pin_file_config`, and does not feed the throttle
    — never a generic 401. Module pin_files are the FRD's designated break-glass path
    and must not fail quietly;
  - `"ldap"` unchanged (grant `"ldap"`); throttle and auth-event logging behavior
    preserved; the event log line keeps module/method/identity fields (identity is
    `"unknown"` for door-code logins — nothing to name);
  - success response `{"identity": ..., "methods": [...]}` now carries grants; identity
    may be `""` for pure door-code sessions (login.html only checks res.ok).
- Passkey login begin/finish guard becomes: module is protected, non-admin, and the
  policy has a passkey service; otherwise 400 as today. Accumulate with grant
  `"passkey"`.
- Logout semantics (stated decision): one cookie may carry an identity plus several
  door-code grants; `handleLogout` stays all-or-nothing — it clears the cookie and
  destroys **every** grant at once. No per-grant logout this round.
- `login.html`: minimal adaptation only — current logic (`pin` tab when `pin`/`admin_pin`
  present, ldap tab when `ldap`, passkey button when `passkey`) already matches the new
  mode output, so expected change is nil-to-tiny; verify the "no methods" empty state
  is impossible now (protected always offers at least ldap or admin_pin) and drop any
  dead handling if trivially removable. go:embed means restart-to-see; no visual
  redesign.

**Verification:** `go test ./internal/platform/auth/... ./cmd/server/...` — new/updated
cases mapping 1:1 to the regression list: todo-PIN session 401 on obsidianoid; LDAP
session 200 on menuserver/obsidianoid/multissh; bearer 200 on menuserver, 401-path on
admin; legacy-claims token denied; door-code session 403 on passkey register/list/delete;
LDAP session may register; admin flow byte-for-byte behavior (mode shows `admin_pin`,
PIN login works, key never works); mode outputs per state.

### S4 — Admin live-edit surface: minimal adaptation to the new shape

**Files:** `internal/admin/handler.go`, `internal/admin/apply.go`,
`internal/admin/build.go` (comments), `web/admin/app.js`, `web/admin/index.html`
(+ admin tests).

**Changes:**
- `authConfigView`/PUT `/api/config/modules` body becomes
  `map[string]{"pin_file": string}` mirroring the config schema; **apply pipeline shape
  unchanged** (apply.go's deliberately-documented ordering: Validate → Build → persist
  → swap) and it now enforces the new rules on live edits too (including pin_file stat
  — a live edit naming a missing file is a 400 with the validator's message).
- **Path-expansion parity with boot (divergence fix):** `expandAuthPaths` runs only in
  `config.Load`, but the apply pipeline validates the operator-submitted `AuthConfig`
  directly — so a relative `pin_file` would be stat'ed against the process CWD at
  live-apply but against the config dir at boot. The apply pipeline must expand each
  submitted `pin_file` relative to the config file's directory — using the exported
  form of `expandRelativeTo` (config.go ~487-496) that **S1 already provides** —
  **before** ValidatePolicy runs.
  Read/write rule for the editor: GET views return the expanded absolute paths (what
  boot produced); the operator may type a relative or `~` path, which is expanded on
  save — the persisted config stores what the operator typed only if load-time
  expansion reproduces the same result; simplest is to persist the expanded form, same
  as validation saw.
- The Pins panel (list editor for `auth.pins`) is removed here in the UI; its
  server-side routes — `POST /api/pins` and `DELETE /api/pins/{name}`
  (internal/admin/handler.go:73-74) plus their handlers/tests — were **already removed
  in S2** (they reference `cur.PINs` and cannot survive the field deletion; nothing to
  hunt for here). The API-keys, LDAP, session, operator-PIN panels stay as-is.
- Matrix editor: replace the 4-method checkbox grid with one row per known module:
  a "Protected" checkbox + a `pin_file` text input (enabled when protected). Admin row
  shows its pin_file (new spelling) or the legacy top-level source read-only.
  **Beware the seeding inversion:** the current editor seeds *all* known modules into
  its working matrix (app.js:75-76) and drops empty entries at save — under
  present=protected those exact semantics would flip meaning (seeding = protecting
  everything; dropping empties = un-protecting `{}` modules). The rework must seed
  "protected" only from actual config presence and persist `{}` entries. **Data-shape
  adaptation only — explicitly not the React/desktop-first redesign.**

**Verification:** `go test ./internal/admin/...` — named cases: (a) **seeding
inversion**: saving with only menuserver marked protected yields a persisted
`auth.modules` containing exactly `{"menuserver": {}}` — nothing else protected — and a
bare `{}` entry round-trips as protected-LDAP-only; (b) **live-save round-trip** (closes
the A1 trap end-to-end): admin live-save → reload the *written* config file via
`config.Load` → boots, with no `"pins"` key in the written file; (c) relative `pin_file`
submitted via PUT is validated against the config dir, not CWD. Manual: load admin page
against the local profile, toggle menuserver protected off/on, save, observe SwapPolicy
take effect on next request (curl menuserver before/after).

### S5 — Local-test profile and docs

**Files:** `local-test/config.json`, `local-test/setup.sh`, `docs/USERGUIDE.md`
(the "Logging in: the auth model" section and the module table where auth is mentioned).

**Changes:**
- `local-test/config.json` auth block:
  ```json
  "modules": {
    "todo":        { "pin_file": "./todo.pin" },
    "slideshow":   { "pin_file": "./slideshow.pin" },
    "menuserver":  {},
    "obsidianoid": {},
    "multissh":    {},
    "admin":       { "pin_file": "./admin.pin" }
  }
  ```
  `pins` list deleted; `admin_pin_file` top-level key dropped in favor of the new
  spelling (exercises it); `api_keys`, `ldap`, `session`, cookie settings unchanged.
- `setup.sh`: also write `local-test/todo.pin` (`111111`) and `local-test/slideshow.pin`
  (`222222`), chmod 0400, same leave-alone-if-exists pattern as admin.pin; update the
  banner text describing how to log in to each module.
- `USERGUIDE.md`: rewrite the auth-model section to two-state semantics (open vs
  protected; LDAP always; door codes; orthogonal API keys; admin PIN-only), update the
  per-module login instructions and the glauth note (LDAP now needed for menuserver too).

**Verification:** fresh `setup.sh` run creates all three pin files 0400; server boots on
the new config; boot intentionally fails (with the S1 error) when pointed at a copy of
the *old* config — checked once manually to confirm the migration error reads well.

### S6 — Full regression sweep

**Files:** none new (test-only fixes if the sweep finds any).

**Changes:** none planned; this step is the gate for calling the round done.

**Verification:** `make test` green (all 16 packages, -race); the complete curl e2e
checklist from section 4 executed against the local profile (glauth running); auth event
log lines spot-checked for the observability items in section 4.

---

## 4. Expanded test plan

### Unit (go test, per package)

- **config**: two-state decode; legacy array → targeted error; legacy `pins` → targeted
  error; pin_file path expansion (relative + `~`); `modules.admin.pin_file` as admin
  source.
- **auth/policy+validate**: OfferedMethods for open / protected / protected+pin_file /
  admin; passkey-service construction rule; protected-without-LDAP error; pin_file
  missing/world-readable errors; admin-source exactly-one rule (all three spellings).
- **auth/session**: accumulate matrix (5 cases in S2, incl. the named stale-subject case); grantsAllow total-function
  behavior incl. legacy `"pin"`/`"key"`/garbage grants; anonymous-subject token
  issue/parse round-trip.
- **auth/pin**: checkPINFile (match, mismatch, missing file, 0644 file, trailing
  newline); checkAdminPIN delegation unchanged.
- **auth/gate+handlers**: every case listed under S3 verification.
- **admin**: PUT modules new shape; live-apply validation failure surfaces 400.

### Integration (httptest via cmd/server dispatcher tests)

- Full dispatcher with the new-schema config: request routing × auth outcome for each
  module state; sliding refresh still re-issues cookies; multissh WebSocket upgrade path
  still sees an unwrapped ResponseWriter (existing Hijacker test keeps passing).

### E2E (curl against the local profile, glauth up) — the named regressions

| # | Case | Command sketch | Expect |
|---|------|----------------|--------|
| 1 | menuserver browser login now works via LDAP | `curl -c j -X POST menu.test:8080/api/auth/login -d '{"method":"ldap","username":"chris","password":"ldap-test-1"}'`; then GET / | 200 login; 200 page (was: unreachable dead-end) |
| 2 | todo PIN session rejected on obsidianoid | PIN-login on todo.test (111111), extract the `uw_session` token from the jar, replay it explicitly: `curl -H "Cookie: uw_session=<token>" obsidianoid.test:8080/api/...` | todo 200; obsidianoid 401 |
| 3 | API key rejected on admin | `curl -H "Authorization: Bearer <local-test key>" admin.test:8080/api/config` | 401 |
| 4 | bearer ignored on grocery | same header against grocery.test/ | 200 (open), identical to no header |
| 5 | door-code session cannot register a passkey | todo PIN cookie → POST /api/auth/passkey/register/begin | 403 |
| 6 | admin unchanged | mode shows admin_pin; PIN 424242 logs in; LDAP login attempt on admin rejected; key rejected (case 3) | all as today |
| 7 | bearer works on any protected non-admin module | key against menuserver.test and multissh.test API paths | 200 both (menuserver previously opt-in only) |
| 8 | invalid key on protected | garbage bearer, no cookie, menuserver.test | 401 |
| 9 | one browser, several grants (accumulate union) | LDAP login on obsidianoid.test, extract token; PIN login on todo.test **sending that token explicitly** (`curl -H "Cookie: uw_session=<token>" -X POST todo.test:8080/api/auth/login ...`), extract the re-issued token; GET /api/auth/session and both modules' APIs with the new token via explicit header | session shows "ldap" and "pin:todo"; obsidianoid 200 and todo 200 with the same token |

*Note on cases 2 and 9:* the local profile uses host-only cookies
(`cookie_domain: ""`), so a plain `curl -b jar` never sends a cookie cross-host — a
naive jar replay would 401 (case 2) or accumulate nothing (case 9) even if server-side
scoping were completely broken. The explicit `Cookie:` header replay **deliberately
bypasses cookie host-scoping to prove the enforcement is server-side**, which is exactly
principle 1's claim. The same cases run at the httptest tier (S3) with direct cookie
injection, independent of curl mechanics.

### Observability (what gets logged)

- `event=auth_login` lines preserved: door-code success logs
  `ok=true module="todo" method="pin:todo" identity="unknown"` (the qualified grant
  string, consistent with the grants vocabulary; verified in S6); disallowed method (PIN on a
  pin_file-less module) logs `reason="disallowed_method"`; throttle and
  `admin_pin_config` reasons unchanged; new reason `pin_file_config` for a module
  pin_file config error (loud 500 path, S3), mirroring `admin_pin_config`.
- New: the 403 on passkey management without an LDAP grant logs one line (e.g.
  `event=auth_passkey_denied module=... reason="requires_ldap"`), so the closed
  escalation is visible in the log, not silent.
- `event=auth_ldap_error` (directory down vs bad credential) unchanged.
- Boot: migration errors (legacy array / legacy pins) and pin_file validation errors are
  the fatal boot messages — verified once by pointing the server at an old-format config.

---

## 5. Acceptance criteria (mapped to FRD decisions)

1. **Two-state config** (FRD: auth model): a module absent from `auth.modules` is fully
   open (all requests pass, bearer headers ignored); present = protected. Legacy
   method-list config fails boot with an error naming the module and the new syntax.
2. **LDAP backbone** (FRD: LDAP always offered): every protected non-admin module accepts
   an LDAP session and offers `ldap` in `/api/auth/mode`; config with a protected module
   and no LDAP URL fails boot.
3. **Scoped door codes** (FRD: PIN = module door code): a `pin:<m>` grant authorizes
   module m only, enforced in the gate (e2e case 2); PIN tab appears only for modules
   with a `pin_file`; the identity-PIN table (`auth.pins`) is gone.
4. **Orthogonal API keys** (FRD: keys are service auth): a valid bearer satisfies every
   protected non-admin module with no opt-in (case 7); is ignored on open modules
   (case 4); never satisfies admin (case 3); invalid key with no session on protected =
   401 (case 8); `key` never appears in mode output.
5. **Admin unchanged** (FRD: admin PIN-only): admin PIN (either spelling) is required and
   the only way in; LDAP and keys never grant admin (case 6).
6. **Enrollment tightening** (FRD Decided: passkey enrollment): passkey
   register/list/delete require a session holding the `ldap` grant; a door-code session
   gets 403 (case 5); passkey *login* still works on protected modules when configured.
7. **Union sessions** (FRD: session semantics): one cookie accumulates identity +
   multiple module grants correctly (case 9); every pre-deploy token is inert by the
   `methods`→`grants` claim rename (deny, no panic, one forced re-login).
8. **Login page** (FRD goal 3): tabs driven by mode; no protected module can present
   zero workable browser methods; no visual redesign shipped.
9. `make test` (go test -race, 16 packages) green at the S3, S4, S5, and S6 step
   boundaries and at the end. S1+S2+S3 land as one verifiable unit whose first full
   green tree is the end-of-S3 boundary; inside the unit, S1 is checked at the
   config-package level and S2 at the compiles-except-gate/handlers checkpoint — see
   the green-tree cadence in section 3.

---

## 6. ADR

- **Decision:** Replace per-module accepted-method lists with the two-state model:
  `auth.modules` maps module → `{pin_file?}`; authorization moves from method
  intersection to scoped grants (`ldap`/`passkey` identity-wide, `pin:<module>`
  module-only, `admin_pin` admin-only) carried in the session claim, whose JSON key is
  renamed `methods`→`grants` so every pre-deploy token is inert by construction; API
  keys become orthogonal service auth on protected non-admin modules; passkey management
  requires the `ldap` grant. Hard config break with targeted boot errors; fixed-time
  session expiry deferred. Per FRD open question 3, door codes are stored as `pin_file`
  (0400 plaintext file, generalized admin.pin loader) rather than inline bcrypt hashes —
  confirmed 2026-09-11 (operator delegated the call; files win: one loader shared with
  admin.pin, the secret stays out of config.json — which the admin live-apply rewrites —
  a bcrypt hash of a short PIN is offline-crackable in seconds anyway, and a 0400 file
  can be set or rotated on the box with no hashing tool, preserving the break-glass
  property).
- **Drivers:** close the method-based authorization holes (cross-module PINs, passkey
  enrollment escalation, `["key"]` browser dead-end); keep the diff minimal on a
  load-bearing gate; one-operator home lab tolerates a hard break and favors loud simple
  failure over compatibility machinery.
- **Alternatives considered:** dual-format config tolerance (A2 — rejected: semantics
  changed, faithful auto-mapping impossible); structured session claim (B2 — rejected:
  more churn, same enforcement); per-module cookies (B3 — rejected: client-side scoping
  as enforcement); shipping midnight expiry in-round (C2 — rejected: orthogonal risk
  mixed into a semantics deploy).
- **Why chosen:** the chosen combination is the smallest change that makes every FRD
  regression testable and true, preserves the two invariants that must not move (gate
  never wraps the ResponseWriter; admin PIN path byte-compatible), and deletes more
  concept-count (method lists, identity PINs, per-module key opt-in) than it adds
  (one grant prefix, one boolean state).
- **Consequences:** old configs must be hand-migrated once (guided by the boot error);
  every pre-deploy session dies at deploy (the `grants` claim rename — one re-login for
  every browser, deliberately chosen over silently widening old `"ldap"` tokens to all
  protected modules); admin UI matrix panel becomes a
  protected/pin_file editor and the pins panel disappears; menuserver/obsidianoid/
  multissh become unreachable when LDAP is down and no passkey is enrolled — the FRD's
  accepted failure mode, with per-module `pin_file` as the documented break-glass;
  passkey-only sessions cannot manage passkeys (must full-login first) — accepted per
  task directive.
- **Follow-ups:** (1) hard session expiry at fixed local time (`expires_at`) — first
  candidate next round; (2) LDAP-groups access matrix; (3) module instances schema;
  (4) React admin UI v2 (the S4 editor is a placeholder); (5) optional admin-page
  warning when a sensitive module (multissh) carries a door code; (6) LDAP metadata
  caching for passkey-login-while-directory-down — an FRD-Decided item, safe to defer
  because nothing this round depends on the cache: no passkeys are enrollable in the
  local environment yet (enrollment now requires an LDAP session first anyway), and
  door codes are entirely local, so the only LDAP-down casualty is full login itself,
  which the FRD already accepts as honestly failing.

---

## 7. Out of scope (explicit)

- LDAP-groups × modules access matrix (later round).
- Module *instances* schema / host_routing rework (later round).
- React admin UI v2 / desktop-first redesign (S4 is data-shape adaptation only).
- Login-page visual redesign (tab logic adaptation only).
- Hard session expiry at fixed local time (`session.expires_at`) — deferred, see ADR C1.
- LDAP metadata caching for offline passkey login.
- Per-person PINs, service-principal matrix rows, any new identity storage.
- Pushing/merging/deploying — user merges themselves. Work lands directly on
  `security-fix` (no new branch, operator decision 2026-09-11).
