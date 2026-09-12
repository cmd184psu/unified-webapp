# Round two — auth, passkeys, and cross-module origin control

Split out on 2026-09-10 so round one is just the multissh port. **This is scheduled work, not cancelled work** — the operator will pick up auth, crypto and security as the next round.

The active plan (`../plan.md`) ports the module and leaves this untouched.

## What's here

| File | What it is |
|---|---|
| `plan-with-platform-auth.md` | The full v8 plan — 836 lines, five consensus iterations. Contains **all** the auth/passkey/origin design work, reviewed and (nearly) converged. |
| `multissh-FRD.md` | Verbatim copy of the FRD as it stood when the auth work was in scope. The live `../multissh-FRD.md` is unchanged; this copy exists so the shelved plan's citations stay resolvable if the FRD is later edited. |

## What's in round two, and where it lives in the shelved plan

| Item | Shelved plan sections |")
|---|---|
| LDAP auth as a platform capability for all six modules (**O-1**) | §0 O-1, tasks 3.1, 3.1b, **3.1c** (the `auth.Gate` type), 3.8, 4.9, 1.2c |
| WebAuthn / passkeys (**O-2**) | §0 O-2, task 3.8's passkey block, pre-mortem scenario 4 |
| Same-origin CORS binary-wide + cross-origin write rejection for all six modules (**D-A / O-3**) | §0 O-3, tasks 4.6, 4.6a, 4.6b, risk R9 |
| The per-module route inventory that the origin middleware and auth gate both needed | task 0.2b |
| `secure_mode`, `protected_modules`, `auth.*` config surface | task 1.1's auth section, 1.2c |
| FRD items FR-A3, FR-A4, FR-A5 | §5 traceability, "Auth & security (FR-A)" |

## Start here, don't regenerate

The design in here is expensive and mostly correct. Five iterations of Architect + Critic review closed, among other things:

- **C4** — a multissh-shaped public-path predicate (`non-/api/ GET == static asset`) promoted to a platform address would have served `GET /items` and `GET /config` unauthenticated on menuserver, todo and slideshow, because every module in this repo is a catch-all-at-`/` SPA with data routes inside that prefix space. The fix is the two-list `PublicPrefixes`/`PrivatePrefixes` gate in task 4.9.
- **C1** — folding `GET /api/auth/mode` into the auth gate makes it vanish under the default config, breaking the SPA's own bootstrap.
- The `auth.Gate` type itself (task 3.1c), including the `Service != nil` mounting guard that keeps `POST /api/auth/login` off unprotected modules.
- `auth.FromConfig` must return `(Provider, *Service, error)` — the two-value form cannot feed the handlers.

**Start from 3.1c and 4.9**, not from the FRD. Add **4.6b** — it closes R9, the one open item round one leaves behind (`../plan.md` §3).

## Known open items at time of shelving

The v8 plan was `pending approval` and had completed four of five permitted consensus iterations. Iteration 5 (Architect on v8) was launched and cancelled when scope was cut, so **v8 itself is unreviewed**. Its status line describes iteration 4's findings, which it applied. Treat v8 as "synthesis complete, verification not done".
