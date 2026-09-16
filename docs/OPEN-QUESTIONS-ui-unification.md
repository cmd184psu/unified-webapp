# Open Questions — UI Unification

Tracked here rather than under `.omc/` because `.gitignore` ignores `.omc/`, so a
mirror there would not survive a clone. Owner: whoever lands the phase named in
"Needed by".

## Phase 1 plan (`docs/PLAN-ui-unification-phase1.md`) — 2026-09-15

*Last reconciled against plan **v5 FINAL (amended per iteration-5 reviews)**. The
iteration-5 amendments touched this document in exactly two ways: four v4-era
prose stamps were corrected (Architect M10), and Q5's pointer now reads "§3
Steps 1 and 3", because the Architect's blocker B2 moved `descriptors.mjs`,
`list-artifacts.mjs`, `EXPECTED_ARTIFACT_COUNT` and the two Makefile gate
targets from commit C2 to C1. Nothing else changed: in particular the Critic's
amendment C — which settled the unstamped `:root` default as **dark** — does not
reach Q3, whose subject is `--color-primary-fg`'s measured values and the
obsidian-stamp premise, never a default direction.*

*Reconciled again on **2026-09-16** against the **Phase-2** plan
(`docs/PLAN-ui-unification-phase2.md`, v5) after three user rulings the same
day. Three changes, all confined to this section's own questions: **Q9** moved
from open to Closed (the `.mjs` half of Q8's carve-out is authorised, and the
part it does not authorise is written down); **Q2** gained a dated amendment
overriding its Phase-3 timing for `--color-surface-dynamic` and correcting two
of its premises against the tree; and **Q3** is untouched — still open, still
gated on C6 landing the attribute, and unaffected by either ruling. Section
numbers in the entries below refer to the **Phase-1** plan unless a citation
says otherwise; the Phase-2 entries added on 2026-09-16 name their plan
explicitly, because the two plans number their sections differently.*

- [ ] **Q3 — Do forest/ocean/ember/rose need `--color-primary-fg` values other
  than `#fff`?** The **values** are settled by measurement and unchanged from
  v3 (plan v5, §3 Step 1.4): `#ffffff` on light, dark, and obsidian; `#0b0f14`
  on forest, ocean, ember, rose, and puma. **The reasoning was not sound and
  was corrected from v4 onward.** v3's closure rested on "obsidian is the theme
  taskmaster's modal actually renders in" — false:
  `grep -c 'data-theme' web/taskmaster/index.html` = **0** (rc=1) and `:2` is
  exactly `<html lang="en">`. The plan makes the premise true rather than
  abandoning the conclusion: commit **C6** stamps `data-theme="obsidian"` on
  `web/taskmaster/index.html:2`, implementing FRD §7 decision 6 (`:482-485` — v4 cited `:483-485`, which
  starts mid-sentence; the decision begins at `:482`),
  which already settled that taskmaster's violet look "is preserved by the
  `obsidian` theme". With the attribute in place, obsidian keeping `#ffffff` at
  **3.99:1** is a defensible inherited condition — taskmaster ships 3.91:1 on
  that same surface today (`web/taskmaster/js/ui/modal.ts:97-99`) — rather than
  an assertion about a theme that was never applied. New **ADR-007** records the
  mechanism; gate **A10.7** checks it (v4 numbered it A10.5; v5 renumbered when
  the A10 family grew to eight criteria); plan §9 item 1 fixes the ratio when
  Phase 2 owns taskmaster's visuals. *Deliberately left open until C6 actually
  lands the attribute: until then the justification is a promise, not a fact.*
  *Needed by: C6.*
- [ ] **FRD correction — FR-6's affected-module list is incomplete.**
  *(new — opened by v4.)* `docs/FRD-ui-unification.md:338-341` names only
  grocery, smbedit, and todo as carrying Google Fonts `<link>`s. The tree has
  **5 link groups across 4 modules**: `web/grocery/index.html:8-10`,
  `web/utuber/index.html:7-9`, `web/todo/index.html:8-10`,
  `web/todo/compare.html:8-10`, `web/smbedit/index.html:7-9`. **utuber** and
  **`web/todo/compare.html`** are missing from the FRD, so an implementer
  working from FR-6 alone would leave two external requests in place and
  believe FR-6 complete. All five survive Phase 1 untouched by design — removing
  them is an FR-6 edit to module HTML that plan guardrail G3 forbids this phase
  from making (plan §8 row 11, G10). *Needed by: whoever lands FR-6.*
  (Plan §9 item 4.)

### Closed

- **Q1 — Does `light` get its own `--overlay-scrim`?** **Closed: yes** (user,
  2026-09-15). Phase 1 pins one structural value, `rgba(0,0,0,0.55)` — the
  donor's literal from `web/taskmaster/js/ui/modal.ts:28` — which is correct on
  the 7 dark themes and far too heavy over a light page. Resolution:
  `themes.css`'s `[data-theme="light"]` block overrides it to
  `rgba(40, 37, 29, 0.35)` — light's own `--color-text` `#28251d` at 0.35 alpha,
  so the scrim is the theme's own ink rather than a foreign black. Mechanically
  this is the same allowlisted per-theme structural override that FR-2 already
  requires for puma's `--font-*`, which is why plan gate **A1.2** carries a
  3-key allowlist instead of an absolute ban. The alpha is a judgement, not a
  derivation; plan §9 item 11 confirms or adjusts it from the sampler pass.
  (Plan v5 §3 Step 1.5.)
- **Q2 — Does `--color-surface-dynamic` need a home in FR-1's vocabulary?**
  **Closed: yes, but it lands in Phase 3**, not Phase 1 (user said yes; v4
  decides the timing). It exists only in `web/obsidianoid/css/themes.css`
  (`#2e2e42`, one of that file's 17 keys) and has exactly one consumer, in the
  module Phase 1 does not migrate. Adding an 18th canonical key now would mean
  authoring 7 donor-less values for 7 themes that nothing reads — so it lands
  with obsidianoid's own migration, which is also when its value is testable.
  **`--radius-xl` is deferred on identical grounds** (grocery-only, one
  consumer). Plan §9 item 5 carries the FRD FR-1 amendment so the vocabulary is
  updated when each key lands, rather than the plan and the FRD diverging
  silently. (Plan v5 §3 Step 1.2, §8 rows 3 and 6.)

  > **Amended 2026-09-16 — the timing half of this answer is overridden, and
  > the override is recorded rather than the original silently rewritten.**
  > Asked about todo's inability to use the full theme roster, the user ruled:
  > *"…In order for todo to enjoy the bredth of theming options, that means we
  > need color choices to extend each theme in a way that works for todo. I'm
  > in favor of doing exactly that: extend the themes so that any theme can be
  > used with any module."* So `--color-surface-dynamic` lands as the **18th
  > shared key in Phase 2, at commit C1** — not Phase 3.
  >
  > Both of this answer's premises were true when written and both were
  > amended by the ruling, not refuted by it. (i) "7 donor-less values for 7
  > themes that nothing reads" over-counts: **5** of the 8 shared themes take
  > a value donated **verbatim** from `web/obsidianoid/css/themes.css` —
  > `obsidian` `#2e2e42` (`:7`), `forest` (`:45`), `ocean` (`:83`), `ember`
  > (`:121`), `rose` (`:159`) — the same donor of record obsidianoid's other
  > five themes already use, so only **3** cells (`dark`, `light`, `puma`) are
  > authored, by the same method `themes.css` already used for 14 of its
  > existing cells. (ii) "exactly one
  > consumer" is **wrong on the tree**: `grep -c 'var(--color-surface-dynamic)'
  > web/obsidianoid/css/app.css` is **2** (`app.css:202`, `:455`). The
  > inherited record therefore did not settle the question on accurate facts,
  > which is part of why the user was asked again.
  >
  > **`--radius-xl` is not swept along.** It stays deferred on its original
  > grounds — grocery-only, one consumer, and no module in Phase 2's manifest
  > reads it. The ruling widens the *theme colour* vocabulary so any theme
  > works with any module; it does not open the structural token set.
  >
  > Phase-2 plan: §9 **Q12** carries the full disposition, guardrail **G12**
  > carries a closed one-item carve for exactly this token, §5 Step 1.5 is the
  > derivation and the eight resolved values, **B1.1 part B** machine-checks
  > the diff shape (8 added declarations, 0 other additions, 0 deletions), and
  > deviation ledger row 14 is reversed. The broader mandate — retokenising
  > `todo.css` and widening todo's picker past two themes — is **Phase 3+**
  > (Phase-2 §11 items 1 and 2) and is in no Phase-2 commit manifest.
- **Q4 — When does FR-7.3's literal tsconfig `include`
  (`["web/*/src/**/*", "web/shared/**/*"]`) land, and in the same commit as the
  `js/` → `src/` entry renames?** **Closed: coupled, same commit** — "I'm fine
  with that (doing two things in the same commit and avoiding risk)". Coupling
  keeps `tsc --noEmit` coverage constant; decoupling silently drops 5 modules
  from typecheck. Phase 1 ships the union of today's enumerated globs plus
  `web/shared/**/*.ts` **and** `web/sampler/js/*.ts` — **11 globs**, not v3's 9 (v4 said "10", which
  miscounted the union); both new globs land in **C4** per plan v5 §3 Step 5,
  so `tsconfig.json` is edited in one commit, not two;
  without the sampler glob, the one thing Phase 1 adds would be invisible to
  the typechecker. Recorded as plan §8 rows 1 and 7; landed in Phase 2 per plan
  §9 item 3. *Needed by: Phase 2.*
- **Q5 — Should `make web-verify`'s artifact list be generated from the build
  descriptor list instead of hand-maintained?** **Closed: yes** — "sure,
  generate the list". v3 reduced two hand-written lists to one make variable but
  left it hand-written, and the iteration-3 reviews found the remaining drift
  hazard. v4 generates `WEB_ARTIFACTS` from `scripts/descriptors.mjs` via
  `scripts/list-artifacts.mjs`. Two mechanics were verified before being written
  down: the assignment must be **recursive (`=`)**, because a simply-expanded
  (`:=`) `$(shell …)` runs at parse time and would break `make build` on a
  node-free host (ADR-001 driver 3); and a **count guard is
  mandatory**, because zero-argument `git ls-files --error-unmatch` lists the
  whole repo and **exits 0** — an empty list would silently disable half of
  guardrail G2 while `web-verify` stayed green. **v5 moves that guard out of the
  Makefile**: v4 expressed it as a `$(words …)` test around `$(shell …)`, which
  cannot fail correctly because `$(shell)` discards the generator's exit status
  and make expands the comparison before the recipe runs. In v5 the expected
  count lives once in `scripts/descriptors.mjs` as `EXPECTED_ARTIFACT_COUNT` and
  the assertion is a real process, `scripts/gates/artifacts.mjs`, that exits
  non-zero (plan v5 §1 guardrail G11, §3 Steps 1 and 3, gates A7.2/A7.7/AX.4).
  **All three files land in commit C1**, with the gate harness rather than with
  the build drivers, because C1's own gates assert against them; the descriptor
  schema itself is specified in Step 3, alongside the drivers that consume it.
  (Plan v5 §3 Steps 1.8 and 3.)
- **Q6 — Should `web/grocery/app.test.js` be wired into `test:web`?**
  **Closed in v3, re-verified in v4.** Yes, unconditionally — but not through
  the `esbuild --format=cjs | node -` pipeline: the file uses `node:test` plus
  `import.meta.dirname` (`:16-17`), esbuild downgrades `import.meta` with a
  **warning and exit 0**, and node then throws a TypeError and exits 1.
  Resolution: `scripts/test-web.mjs` carries a per-descriptor `runner` field —
  `"esbuild-cjs"` for the four existing suites, `"node-test"` for grocery, the
  latter running
  `spawnSync(process.execPath, ["--test", <file>], {cwd: repoRoot})`. In
  addition, **esbuild warnings are promoted to failures** in both drivers, so
  the class of warning that masked this cannot hide the next case. v4 confirmed
  that promotion is safe to switch on immediately:
  `npm run build 2>&1 | grep -iv 'log-level=warning' | grep -i 'warn'` → rc=1,
  i.e. **zero real esbuild warnings in the tree today**. (The inverse filter is
  required because npm echoes the script text, which itself contains
  `--log-level=warning` — the naive grep matches npm's own echo.)
- **Q7 — Do the remaining 12 modules adopt `shared.css` via `<link>` or via
  their own bundled CSS?** **Closed: via `<link>`** — "ideally, via link". Two
  consequences, both recorded rather than absorbed silently:
  `web/shared/dist/shared.css` **stays a separate committed artifact**; and
  **ADR-006's third reason** for keeping `@font-face` out of `tokens.css` —
  separate importability for per-module adoption — **weakens**, because every
  module now takes the same combined sheet and nothing in Phase 2 exercises
  separate importability. ADR-006's reasons 1 (a consumer can take tokens
  without downloading 15 font faces) and 2 (the font layer changes on a
  different cadence) stand on their own and are sufficient. Plan §9 item 2
  carries the adoption work. *Needed by: Phase 2.*
- **Q8 — `/shared/` sits inside `svc.Gate`, so the login page structurally
  cannot link `shared.css`.** **Closed: direction (a) authorised** — "there's no
  need to protect css files like that". `internal/platform/auth/gate.go:28`
  embeds and serves `login.html` before any module handler runs, yet the FRD's
  scope includes styling it. Of the two options — **(a)** a narrow
  unauthenticated allowlist for exactly `GET /shared/dist/shared.css`, added
  beside `Gate`'s existing carve-outs (`GET /healthz` at `:129`,
  `GET /api/auth/mode` at `:135`, `GET /api/auth/whoami` at `:148`, all
  evaluated before `protected := hasEntry || module == "admin"` at `:157`), or
  **(b)** permanent inline styles in the login page — the user chose the posture
  change over the permanent fork. **Implementation is Phase 2; Phase 1 does not
  touch `gate.go`.** The scope of the carve-out beyond CSS is **not** settled by
  this answer and is tracked as **Q9** below. Plan §9 item 7.
- **Q9 — Does Q8's allowlist carve-out extend to `.mjs`?** *(opened by the
  Phase-1 plan's v4; narrowed same day.)* **Closed: yes, both halves**
  (user, **2026-09-16**).

  The `.woff2` half was settled on 2026-09-15 — "fonts and css do not require
  token protections" — covering `/shared/public/fonts/*.woff2` alongside
  `GET /shared/dist/shared.css`. The `.mjs` half was deliberately left open
  because executable JavaScript is a different posture question and the login
  page might not need the script at all. Asked directly, the user closed it:

  > *"yes, the login page should use the theme previously selected."*

  A login page that restores a previously selected theme must **run** the code
  that reads the store, and that code ships only in the shared barrel — so
  `GET /shared/dist/shared.mjs` is the ruling's minimum, not a convenience.
  The carve-out is therefore **three exact GET-only path shapes**, not two.
  What bounds it: the `.mjs` match is exact, so `/shared/dist/` does not become
  a readable prefix; the method guard is unchanged, so `POST`/`HEAD` still
  401; the handler is the same `static.SharedHandler` the authenticated
  dispatcher already uses; and the barrel is already world-readable on every
  unprotected module, so the change narrows an asymmetry rather than exposing
  new content.

  Implementation is **Phase 2, commit C8** — `docs/PLAN-ui-unification-phase2.md`
  §5 Step 8, guardrails and deviation ledger row 20, criteria **B6.3**
  (which **flips from a negative 401 probe to a positive 200 + Content-Type
  probe** under this ruling and keeps its number), **B6.4** and **B6.5**.
  Phase-2 §9 Q9 carries the full disposition.

  **What this answer does *not* authorise.** *Styling* the login page — the FRD
  scope item that motivated Q8 in the first place — is **still not in Phase 2**.
  C8 makes the three assets reachable without a session; it does not add a
  `<link>`, a pre-paint block or a theme control to `login.html`, and
  `internal/platform/auth/gate.go`'s embedded page is otherwise untouched. That
  remains a follow-up, tracked as Phase-2 §11 item 11. *Needed by: whoever
  styles the login page.*
