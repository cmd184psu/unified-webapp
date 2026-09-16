# Open Questions — UI Unification

Tracked here rather than under `.omc/` because `.gitignore` ignores `.omc/`, so a
mirror there would not survive a clone. Owner: whoever lands the phase named in
"Needed by".

## Phase 1 plan (`docs/PLAN-ui-unification-phase1.md`) — 2026-09-15

- [ ] **Q1 — Does `light` get its own `--overlay-scrim`?** Phase 1 pins one
  structural value (`rgba(0,0,0,0.55)`, the donor's literal from
  `web/taskmaster/js/ui/modal.ts:28`). A 55%-black scrim over a light page is
  heavier than intended. *Needed by: Phase 2.*
- [ ] **Q2 — Does `--color-surface-dynamic` need a home in FR-1's vocabulary?**
  It exists only in `web/obsidianoid/css/themes.css` and Phase 1 drops it from
  the remap. obsidianoid's migration needs somewhere to put it. *Needed by:
  Phase 3.*
- [ ] **Q3 — Do forest/ocean/ember/rose need `--color-primary-fg` values other
  than `#fff`?** Accent luminance varies across the four; Phase 1 sets `#fff`
  everywhere and relies on the 8-theme checklist to catch a bad pairing.
  *Needed by: Phase 2.*
- [ ] **Q4 — When does FR-7.3's literal tsconfig `include`
  (`["web/*/src/**/*", "web/shared/**/*"]`) land, and in the same commit as the
  `js/` → `src/` entry renames?** Coupling them keeps `tsc --noEmit` coverage
  constant; decoupling them silently drops 5 modules from typecheck. *Needed by:
  Phase 2.*
- [ ] **Q5 — Should `make web-verify`'s artifact list be generated from the
  build descriptor list instead of hand-maintained?** A hand-maintained list
  drifts as modules are added, and a missing entry makes G2 quietly weaker.
  *Needed by: Phase 2.*
- [ ] **Q7 — Do the remaining 12 modules adopt `shared.css` via `<link>` or via
  their own bundled CSS?** Determines whether `web/shared/dist/shared.css` stays
  a separate committed artifact. *Needed by: Phase 2.*
- [ ] **Q8 — `/shared/` sits inside `svc.Gate`, so the login page structurally
  cannot link `shared.css`.** `internal/platform/auth/gate.go:28` embeds and
  serves `login.html` before any module handler runs, yet the FRD's scope
  includes styling it. Options: **(a)** a narrow unauthenticated allowlist for
  exactly `GET /shared/dist/shared.css`, added beside `Gate`'s existing
  carve-outs (`GET /healthz`, `GET /api/auth/mode`, `GET /api/auth/whoami`, all
  evaluated before `protected := hasEntry || module == "admin"`) — makes one
  stylesheet world-readable on a closed LAN, a small but real posture change; or
  **(b)** permanent inline styles in the login page — keeps the gate absolute
  but permanently forks login styling from the token system. *Needed by: decide
  before Phase 2 touches the login page. Not a Phase-1 blocker.*

### Closed

- **Q6 — Should `web/grocery/app.test.js` be wired into `test:web`?** Closed
  2026-09-15: yes, unconditionally, via `scripts/test-web.mjs` (Phase 1 Step 2,
  driver rule 7). Verified green standalone at 189 pass / 0 fail.
