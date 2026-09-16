# PLAN — UI unification, Phase 2: the three shared components and their two reference modules

**v5 FINAL (amended per iteration-5 reviews) — APPROVED, ready for
execution.** The iteration-5 Architect review returned **SOUND** with three
mandatory one-line amendments (A-1, A-2, A-3) and five polish notes; the
iteration-5 Critic review returned **APPROVE WITH MANDATORY PRE-EXECUTION
AMENDMENTS** (1 critical, 6 major, 15 minors, three unconditional-approve
rows). Both verdicts are exit verdicts, so **iteration 5 closes the consensus
loop** and no iteration 6 is convened. Every amendment from both reviews has
been applied to this document by the Planner — the only party that edits the
plan — and **§18** maps each one to its edit and republishes the full §13
suite. Hand-off is to `/oh-my-claudecode:start-work`, at C1, on branch
`ui-upgrade`.

Planner output for the ralplan consensus loop
(Planner → Architect → Critic), **iteration 5 — the last**: the synthesis of
iteration-4's Architect review (**SOUND** — narrow margin, conditional on A-1
and B-2, plus five polish notes N-1…N-5) and Critic review (ITERATE: F-A and
F-B critical, F-C and F-D major, an eight-item to-approve list), **together
with seven user rulings delivered 2026-09-16** that close Q9, Q11 and Q12 and
settle four open tolerances.
**Both reviewers independently reproduced all six of §13's published integers
exactly**, so the audit layer is trusted and every remaining defect in v4 was
semantic: *a criterion asserting a property of a gate's output, or of a pixel
diff, that the step which writes that gate or makes that edit never
specified.* That class has run at one defect per iteration for three
iterations, so v5 adds the structural remedy both reviewers pointed at — a
**bidirectional "guaranteed by" back-pointer** between every criterion that
pins a gate's output and the step that makes it true — plus **§13(a6)**,
which greps the owning file for every symbol whose definition the plan
deletes. The two reviews **converge on one item** (Architect A-1 ≡ Critic F-A,
item 4: Step 2.3(b) never pinned the descent's line coordinates), and it is
fixed at both ends.
**The largest change in v5 is not a review item but user ruling 3**: Q12 is
answered with **option B**, so `--color-surface-dynamic` lands as the **18th
shared token** — engineered in full below, with the G12/§1/B1.1/gate-roster
carves it requires, the three authored values, and a seeded-failure
demonstration of the carved gate edit run before and after.
The nine-commit sequence, ADR-015/4D and ADR-016 are unchanged. Nothing has
been implemented; no source file outside this document and
`docs/OPEN-QUESTIONS-ui-unification.md` has been touched. **§14 is iteration
1's fix-by-fix ledger, §15 is iteration 2's, §16 is iteration 3's and §17 is
iteration 4's plus the user rulings**, each mapping every finding to its edit
or to a reasoned, tree-verified rebuttal.

> **The lesson both reviewers stated, now a standing rule of the pass.** Three
> consecutive iterations have introduced a fresh citation error *on the
> surface the pass just edited* — v2's `todo.css:102-143`, v3's
> `bundle-shape.mjs:100`, and v4's own first draft of §5 Step 5.6, which cited
> a **plan** line number in a document whose `:NNN` convention resolves
> against the last-named *file*. The third was caught inside this pass rather
> than shipped, by grepping the citation immediately after writing it — which
> is the rule, not luck. The property that predicts an error is **recency of
> edit**, not citation density, so §13(a4) gains a **third, diff-scoped half**:
> every `path:N` citation this revision added or changed is re-derived against
> the tree before the revision is published — exhaustively, not sampled. That
> set is small by construction and shrinks as the plan stabilises.
> **§13 also gains (a5)**, which closes the last audit blind spot the
> reviewers found: pointers *inside* this document. Three of iteration-3's
> findings were stale cross-references (`§4.2` for `§4.4`, `§5.5` for Step
> 6.2a, Step 5.1 for Step 1.3) and one was §7's census contradicting §13's own
> table — none of which any existing clause looked at. *(Architect's
> iteration-3 synthesis and M-3; Critic F-4, F-5.)*
>
> ***v5 adds the second standing rule, for the semantic class.*** Recency of
> edit predicts *citation* errors; it does **not** predict the defect class
> that survived four iterations. Iteration 4's four fresh defects split two
> ways: F-A landed on a surface v4 had just rewritten, while **F-B, F-C and
> F-D all sat on text v4 never touched** — each one a criterion or an ADR
> asserting a property of a gate's output, of a deletion's blast radius, or of
> a pixel diff, that the *step* responsible never specified. So the rule is
> not "re-read what you edited" but **"re-derive what you asserted"**: every
> criterion that pins a gate's printed output, a diff's composition, or a
> symbol's disappearance now carries a **"guaranteed by (step)"**
> back-pointer, and the named step names the criterion in return. The sweep is
> the same `grep` shape §13(d2) already runs over the label ⇄ schedule
> directions, and §13(a6) adds the deleted-symbol half. Under that sweep of
> v4, **B1.3's C4-leg part 2 was the only criterion with no pair** — which is
> exactly the defect A-1/F-A reported, found a second way.
>
> *(**v5 FINAL**, Critic finding 4: read precisely, B1.3 was the only
> criterion with no leg in **either** direction. Four of the six pairs v5
> DRAFT went on to announce were nevertheless **one-legged** — BX.2's and
> BX.11's reverse legs absent, B9.1 named back by one of its three steps, and
> B2.7 carrying no step leg at all. All four are paired in v5 FINAL; §17 holds
> the itemised correction and the live pointer counts.)*

**Phase base SHA — stated once, referenced everywhere:** `b73d31c`
(`b73d31cb31dfdcf897fe1eb685fddd3e4cd0ffa9`), the tip of Phase 1. Every ranged
`git diff` in §7 ranges against this SHA; every boundary uses the same one.

Phase 2 of `docs/FRD-ui-unification.md` (phase table `:454-461`, Phase-2 row
`:458`). Phase 1 is `docs/PLAN-ui-unification-phase1.md`; this plan **inherits**
its guardrails, its two governing rules, and its ADR numbering, and does not
re-derive any of them.

The two inherited governing rules, restated because every section below leans
on them:

1. **One definition per fact.** Before an occurrence of a fact is deleted,
   the surviving definition is named. Before a fact is written twice, the
   duplication is declared as an exception with a reason.
2. **A gate is a command that was run.** A criterion with no recorded output
   carries `[deferred → Cn]` and names the boundary where it first runs, plus
   the substitute evidence gathered in its place. The census is stated **once,
   by number**, in §7.

---

## §0 — RALPLAN-DR summary

**Mode: SHORT.** Phase 2 writes ~900 lines of new TypeScript and CSS into an
existing, fully gated harness and changes pixels in two modules. It is not a
platform-posture change: no new dependency, no new token, no new build driver,
one narrow auth carve-out that the user has already authorised in principle
(Q8). Deliberate mode is not warranted; §8.5 still carries a three-scenario
pre-mortem because two of the commits are coupled.

### Principles

- **P-I — The component owns the chrome; the module owns the contents.**
  Every hook in FR-4 exists so that a module's odd control (a number input, a
  four-checkbox group, a pair of selects) is mounted *verbatim* rather than
  re-modelled. Zero functionality loss is achieved by refusing to model, not
  by modelling harder.
- **P-II — Adoption is proved by a preservation checklist, not by a diff.**
  FRD `:308-310` makes the per-module checklist the acceptance condition for
  FR-4. §7's B4 family instantiates it for all three modules; a control that
  cannot be pointed at post-migration is a failure even if the build is green.
- **P-III — A gate that cannot fail is not a gate.** Phase 1 shipped
  `bundle-shape.mjs` with a vacuous-pass path and recorded it as vacuous
  rather than counting it. Phase 2 inherits that honesty: the sampler's
  focus-trap checklist is vacuous today for one of four dialogs (§4.5), and
  the G9 fonts allowlist is currently unenforced (§4.6). Both are closed
  before any new component lands.
- **P-IV — Pixels last, and only in the two named modules.** sampler is a
  gallery with no users; obsidianoid and todo are the two reference
  adopters. Every other module's bytes are held constant by G2 and the
  artifact gate, and the unnamed modules are not in any commit manifest.
- **P-V — Rollback is a property of the sequence, not of a hope.** Each
  commit names every path it touches, revert order is stated for the one
  provider/adopter pair, and the one coupled commit (C5) states why coupling
  reduces risk rather than adding it.

### Decision drivers (top 3)

1. **D1 — `@shared` is only resolvable through a bundling esbuild pass.**
   `build-web.mjs:78-88`'s `onResolve` plugin is an esbuild *bundle* plugin;
   with `bundle: false` esbuild does not resolve import specifiers at all, so
   a transpile-only descriptor would emit the literal string `"@shared"` into
   the browser. This single mechanical fact decides obsidianoid's descriptor
   shape (ADR-011), todo's entry shape (ADR-013), and why
   `scripts/gates/bundle-shape.mjs` must learn about multi-output descriptors
   before C5.
2. **D2 — `scripts/check-shared-css.mjs` clause 7 has three independent holes,
   and Phase 2 is the phase that would fall through all three.** The clause
   body is `:300-319`. (a) It is keyed to the literal filename
   `components.css` (`:301-302`), so any new sheet escapes it. (b) Its
   *selector* rule — the loop at `:310-318`, whose `!UI_SELECTOR.test(one)`
   at `:314` fails any depth-1 selector not matching
   `UI_SELECTOR = /^\.ui-[a-z0-9-]+/` (`:102`) — is a **separate check from**
   its colour-literal rule (the per-line scan at `:304-308`, `COLOUR_LITERAL`
   at `:101`); a mechanism can be legal under one and illegal under the other,
   which is exactly how v1's swatch proposal passed review as "gate-legal".
   (c) `parseBlocks()` ends `return blocks.filter((b) => !b.selector.startsWith("@"))`
   (`:182`), so the **selector** rule — and *only* the selector rule — is
   blind inside an at-rule. *(v2 said "nothing inside an at-rule is checked by
   either rule". That is **false** and is corrected here: the colour-literal
   rule at `:304-308` iterates `text.split("\n")` over the **whole file** and
   is indifferent to nesting, so a colour literal inside an `@media` block is
   caught today. The asymmetry is the point — the two rules do not share a
   parse.)* The selector blindness is latent today
   (`grep -c '@' web/shared/css/components.css` = **0**) and opened by Step
   4.2's `@media`. This single driver decides that all Phase-2 CSS lands in the
   one existing file, that clause 7 is re-keyed to fail closed, that
   `parseBlocks` gains an opt-in at-rule descent, and that the re-key ships
   with a **negative** test (ADR-012, B1.4) — a test which, *because* of the
   asymmetry just stated, must be a **selector** escape rather than a colour
   literal: a colour literal inside `@media` is caught by the existing
   whole-file scan whether the new descent parameter works or not, so it
   probes nothing.
3. **D3 — The `dark` → `obsidian` rename is a data migration, not only a
   string edit.** `web/obsidianoid/js/app.ts:425` persists the *chosen theme
   name* under `obsidianoid-theme-<vaultIndex>`. After the rename, a stored
   `"dark"` still resolves to a valid shared theme — the wrong one
   (`#0d1117`, not `#13131a`). **Authority split, stated once here:** the
   rename *decision* is FRD `:191` (FR-2's roster table, "**renamed**
   (decision confirmed)"); the *coupling principle* — do the two edits in one
   commit rather than two — is the user ruling quoted at
   `docs/OPEN-QUESTIONS-ui-unification.md:126-129` (Q4, "I'm fine with that
   (doing two things in the same commit and avoiding risk)"), which closes a
   `tsconfig` question but states the principle in general terms. Q4 is **not**
   an authority on theme names and is not cited as one anywhere in this plan.
   D3 is what makes the coupling correct rather than merely convenient
   (ADR-014).

### Viable options for the four shape decisions

**Decision 1 — ThemeManager's API surface vs. Phase-1's `theme.ts`.**
Today `web/shared/ts/theme.ts` is 21 lines: `THEMES` (`:8-17`, 8 names,
`as const`) and `setTheme(name: string): void` (`:19-21`, one DOM write).
FRD `:230-261` specifies a configured class.

| | Option | Pros | Cons |
|---|---|---|---|
| **1A** *(chosen)* | Add `class ThemeManager` to `theme.ts`; `setTheme` stays exported as the primitive the class calls; `THEMES` stays the single roster | No consumer breaks (`web/sampler/js/main.ts:9` destructures `setTheme` today; taskmaster is a barrel consumer from Phase-1 C6); the class is the only new public name; `THEMES` remains one definition feeding both `themes.list` and the CSS matrix | Two ways to set a theme exist in the public surface; a module could bypass persistence by calling `setTheme` directly |
| 1B | Replace `setTheme` with `ThemeManager` only | One way to do it; persistence cannot be bypassed | Breaks the sampler and the Phase-1 barrel allowlist in the same commit that introduces an unproven class; deletes a working primitive before its replacement has a single adopter — fails the one-definition rule's operational test (name the survivor *before* deleting) |
| 1C | Free functions (`initTheme`, `currentTheme`, `onThemeChange`) | No class ceremony; trivially tree-shaken | Cannot hold per-instance config (`storageKey()`, `serverDefault()`, `onChange()`); obsidianoid's per-vault key is re-derived on every call, which is exactly the `app.ts:425`/`:493` duplication this phase is deleting |

**Decision 2 — HamburgerMenu as a class vs. a web component.**

| | Option | Pros | Cons |
|---|---|---|---|
| **2A** *(chosen)* | Plain exported class rendering into light DOM with `.ui-menu-*` classes | `components.css` clause 7 can lint every selector it emits; the `render(host)` slot hands a module a real `HTMLElement` in the page's own DOM, so todo's existing `<select>`s and checkboxes keep working with their existing `document.getElementById` handlers (`web/todo/index.html:52-76`) unchanged; same authoring idiom as `modal.ts` | Global class names can collide with a module's own CSS; nothing prevents a module from reaching into the drawer's internals |
| 2B | Custom element + Shadow DOM | True encapsulation; no class-name collisions | Shadow DOM breaks `document.getElementById` for every slotted control, which is precisely todo's 9 controls; shared tokens would need explicit piercing; and `check-shared-css.mjs` lints files, not shadow trees, so clause 7 would stop covering the component's styles |
| 2C | Custom element, no shadow root | Declarative `<ui-hamburger>` markup | Buys the registration ceremony without the encapsulation; adds a custom-elements registry as a new global contract for zero gate coverage gained |

**Decision 3 — Toast's API.**
Donor of record: `web/certmachine/js/toast.ts` (81 lines), named by FRD `:319`.

| | Option | Pros | Cons |
|---|---|---|---|
| **3A** *(chosen)* | Lift the donor's surface verbatim: `showToast(message, tone?, durationMs?) → ToastHandle`, module-level lazily-created singleton stack (`ensureStack()`, donor `:28-36`) | Zero API invention; the donor's three load-bearing properties survive by construction — `textContent` only (`:56`), sticky errors (`:20-24`, `error: 0`), one `role="status" aria-live="polite"` live region (`:32-33`); certmachine can adopt in a later phase with a two-line shim, exactly as taskmaster's modal did at Phase-1 C6 | A module-level singleton is process-global state in a library; two pages in one document would share the stack (not a real case — one page per document) |
| 3B | `class ToastHost` with per-instance stacks | Testable without globals; multiple regions possible | Multiple `aria-live` regions is an a11y regression, not a feature; and obsidianoid's 16 call sites (`app.ts:160,173,267,270,272,335,338,340,402,403,405`; `threads.ts:163,180,181,204,205`) all expect a free function — a class forces an instance to be threaded through two files that share no module scope |
| 3C | Keep obsidianoid's `showToast(msg, type)` shape (2 args, 2800 ms fixed) | Smallest diff in obsidianoid | Throws away sticky errors, which is the donor's entire reason for existing (`toast.ts:17-19`); FRD `:319` names certmachine as the base, and §7 of the FRD is settled |

**Decision 4 — where the theme swatch's per-theme colour comes from.**
*(v1 left this as an "open detail for review" and recommended an option that
both reviewers refuted. It is now decided here, with the refutations recorded,
and it is ADR-015.)* Four constraints must hold **simultaneously**, and v1's
analysis satisfied at most two of them at once:

- **gate-legal** — survives clause 7's *selector* rule (`:310-316`,
  `UI_SELECTOR = /^\.ui-[a-z0-9-]+/` at `:102`) **and** its colour-literal
  rule (`:304-307`). These are two checks, not one;
- **functionally correct** — the eight swatches in **one open picker** render
  eight **distinct** fills at the same instant;
- **G12-compatible** — declares no new design token, and does not edit
  `tokens.css` or `themes.css`;
- **barrel-stable** — `THEMES` keeps its `readonly string[]` shape, so
  `web/sampler/js/main.ts:66-79` (`option.value = theme` at `:72`) keeps
  working. Note the gate's reach here, because it is narrower than it looks:
  `check-shared-barrel.mjs` diffs exported *names* only — `ALLOWED_VALUES`
  (`:25`, which already lists `THEMES` and `setTheme`) and `ALLOWED_TYPES`
  (`:26`, populated from `export type` / `export interface` declarations at
  `:89-91`). **It does not inspect `THEMES`' type.** So a reshape of `THEMES`
  is invisible to every gate in `make check` and surfaces only as wrong pixels
  — which is an argument against reshaping, not for it.

| | Option | Gate-legal | Correct | G12 | Barrel | Verdict |
|---|---|---|---|---|---|---|
| 4A | `getComputedStyle` probe: stamp a hidden element with each theme in turn and read `--color-primary` | ✅ | ✅ | ✅ | ✅ | **Viable, rejected.** Unreliable before first paint — exactly when a pre-paint picker may build — and forces a layout read per swatch. Kept as the documented fallback if 4D ever fails |
| 4B | `THEMES` → `{name,label,color}[]`, colour literals emitted per theme in `components.css` *(v1's recommendation)* | ❌ | ❌ | ✅ | ⚠️ | **Refuted.** The eight literals survive clause 7 only through the colour rule's literal-only blind spot — in the very phase whose ADR-012 exists to close blind spots. And `option.value = theme` at `main.ts:72` silently becomes the string `"[object Object]"`, breaking sampler's picker **with every gate still green** (see the reach note above). A silent break is worse than a caught one |
| 4C | `[data-theme="x"] .ui-theme-swatch { background: var(--color-primary) }`, one rule per theme *(Architect's iteration-1 recommendation)* | ❌ | ❌ | ✅ | ✅ | **Refuted.** Eight depth-1 selectors starting with `[` → **eight hard clause-7 failures** at `:314`. And the selector keys on the **ancestor's** active theme, which is one value at any instant, so all eight swatches in one open picker render **identically** |
| 4E | Inline `style="background:${color}"` per swatch — **what the donor does today**, `web/obsidianoid/js/app.ts:438` | ✅ | ✅ | ✅ | ⚠️ | **Refuted.** Gate-legal only by scope (clause 7 reads CSS files, not `.ts`), but the colours must come from somewhere: the donor's literals live in its own `THEMES` at `app.ts:46-52` and cover **5** themes, not 8. Adopting it therefore *requires* 4B's reshape and inherits 4B's silent break. It also re-introduces inline style, which G12's spirit and the shared-CSS gate exist to remove |
| **4D** *(chosen)* | Stamp `data-theme` on **each swatch element itself**; **one** rule in `components.css`: `.ui-theme-swatch { background: var(--color-primary); }` | ✅ | ✅ | ✅ | ✅ | **Chosen.** See the four proofs below |

**Why 4D satisfies all four, each verified in this tree:**

1. **Gate-legal — selector rule.** The one new selector is `.ui-theme-swatch`,
   which matches `UI_SELECTOR` at `:102` outright. No `[` appears at depth 1.
   *(Note the mechanics: `UI_SELECTOR` is applied with `.test()`, an unanchored-
   at-the-right prefix match, so even `.ui-theme-swatch[data-theme="x"]` would
   pass — but 4D needs no such form. One rule, not eight.)*
2. **Gate-legal — colour rule.** `components.css` gains **zero** colour
   literals; the fill is `var(--color-primary)`. `COLOUR_LITERAL` (`:101`) has
   nothing to match. The `data-theme` value is written by TypeScript
   (`el.dataset.theme = name`) and never appears in CSS.
3. **Functionally correct.** `web/shared/css/themes.css` keys every palette on
   a **bare attribute selector** — `:root, [data-theme="dark"]` `:15`,
   `[data-theme="light"]` `:38`, `obsidian` `:61`, `forest` `:82`, `ocean`
   `:103`, `ember` `:124`, `rose` `:145`, `puma` `:167` — **not**
   `:root[data-theme=…]`. So the block applies to *any* element carrying the
   attribute, setting that element's own `--color-primary`, which its own
   `background` then resolves. Eight swatches carrying eight different
   `data-theme` values resolve eight different fills **simultaneously**, under
   any page theme. Verified distinct: `#7c3aed`, `#01696f`, `#7c6af7`,
   `#4dbb6e`, `#5b9cf6`, `#f0a04a`, `#e05c7a`, `#3fbf9c` — **8 distinct
   values**, which is what makes B4.2's `Set(...).size === 8` assertion
   satisfiable rather than aspirational. *Cross-check on the rename:* the
   donor's own `dark` swatch colour is `#7c6af7` (`app.ts:47`), which is
   byte-identical to shared **`obsidian`**'s `--color-primary`
   (`themes.css:61` block) and **not** to shared `dark`'s `#7c3aed`. The donor
   palette the user sees today is the one FRD `:191` renames to `obsidian`, so
   4D reproduces the donor's five fills exactly under their new names — a
   pixel-parity fact B2 can lean on.
4. **G12 and barrel.** No token is declared; `tokens.css` and `themes.css` are
   not edited (B1.1 still passes ranged). `THEMES` stays
   `readonly string[]`, so `main.ts:72` is untouched and neither
   `ALLOWED_VALUES` (`:25`) nor `ALLOWED_TYPES` (`:26`) moves — the barrel
   trajectory becomes **7/6 → 8/7 → 9/9**: v1's `ThemeName` export existed
   only to type option 4B's reshaped `THEMES`, so 4D removes it and one type
   drops out of C3 and C4's counts. This is a *consequence* of the decision,
   propagated to every place the plan states the counts (§5 Steps 3.3 and 4.3,
   §6's composition table, BX.2, §13(d)) rather than left to drift. Labels are
   derived in the
   picker by title-casing the name; the donor's five labels (`app.ts:47-51`:
   `Dark`, `Forest`, `Ocean`, `Ember`, `Rose`) are each exactly the title-case
   of their name, so title-casing loses **no** label data and the donor's
   `label` field becomes genuinely redundant rather than dropped.

**The one consequence 4D carries, declared rather than discovered.** Theme
blocks are pure custom-property blocks — `check-shared-css.mjs` clause 6
(`:287-295`) permits only `--color-*` plus
`THEME_ALLOW = ["--font-body", "--font-mono", "--overlay-scrim"]` (`:96`), and
`grep -c '@' web/shared/css/themes.css` = 0 — so stamping `data-theme` on a
small empty `<span>` has exactly one effect: **every** colour token resolves to
the previewed theme inside that span's subtree. The swatch's own
`border: 1px solid var(--color-border)` (FRD `:257-261`'s requirement,
replacing the donor's `rgba(255,255,255,.15)` at `app.css:810`) therefore
follows the **previewed** theme rather than the page theme. This is recorded as
a deliberate micro-preview, not a defect: it is the reason the border reads
correctly on light *and* dark swatches, which is the FRD's stated goal. It is
ledger row 15 and **D-row D-15** (§5 Step 5.5 — *not* D-12, which is todo's
drawer `box-shadow`; an earlier draft mis-numbered it).

**Invalidation rationale where only one option survives.** Two Phase-2
decisions have no second viable option, and both are mechanical rather than
architectural:

- **Where Phase-2 CSS lives.** `check-shared-css.mjs:298` keys clause 7 to
  the filename `components.css`. A new `menu.css`/`toast.css` would be linted
  by clauses 8, 9 and 11 but **not** by clause 7's colour-literal ban or its
  `^\.ui-` selector rule. Splitting therefore *reduces* gate coverage over
  the code this phase writes. The only alternative — split the files *and*
  extend clause 7's key to a set — is strictly more work for an identical
  guarantee, so the chosen shape is: one file, and clause 7 re-keyed to
  "every sheet that is not `tokens`/`themes`/`fonts`/`index`" so a future
  sheet is linted by default (ADR-012).
- **obsidianoid must bundle.** D1 above. There is no non-bundling path to
  `@shared`, and hand-writing the literal URL `/shared/dist/shared.mjs` in
  module source would need a `tsconfig` `paths` entry mapping a URL to a file
  — which defeats `@shared`'s single mapping (`tsconfig.json:10-13`) and
  makes `bundle-shape.mjs`'s A9.1 marker unfalsifiable.

---

## §1 — Scope

**In scope — the five Phase-2 deliverables** (FRD `:458`):

1. **ThemeManager** — FR-3, `web/shared/ts/theme.ts` (FRD `:230-261`).
2. **HamburgerMenu** — FR-4, `web/shared/ts/menu.ts`, hook-fed (FRD `:263-310`).
3. **Toast** — FR-5's widget table, row 2 (FRD `:319`), `web/shared/ts/toast.ts`.
4. **obsidianoid adoption**, with the `dark` → `obsidian` rename **coupled in
   the same commit** and its duplicated theme roster dropped in favour of the
   shared source. Two distinct authorities, kept distinct (D3): the **rename
   decision** is FRD `:191` ("**renamed** (decision confirmed)"); the
   **same-commit coupling principle** is the user ruling at
   `docs/OPEN-QUESTIONS-ui-unification.md:126-129` (Q4 — "I'm fine with that
   (doing two things in the same commit and avoiding risk)"), which is a
   ruling about coupling, *not* about theme names.
5. **todo adoption** — drawer re-homed onto the shared `HamburgerMenu`; the
   verified-dead pshelper tree deleted (FR-9, FRD `:387-396`). Lands as **two
   commits, C6a then C6b** (ADR-016), not one.

Each of 1–3 lands **with its sampler showcase in the same commit**, so every
component has a rendered, keyboard-operable demo at its own boundary.

Two inherited debts are closed first because they make later gates real
rather than nominal:

6. The sampler's `openModal` demo gains a focusable control (§4.5).
7. **G9's fonts-tree allowlist becomes enforced** (§4.6) — today it is prose
   only.

One settled platform item lands last, isolated:

8. **Q8(a)** — the narrow unauthenticated allowlist for `GET
   /shared/dist/shared.css` and the woff2 faces
   (`docs/OPEN-QUESTIONS-ui-unification.md:187-199`, "Implementation is Phase
   2").

**Out of scope, explicitly:**

- **Modules other than sampler, obsidianoid, todo.** taskmaster, timetracker,
  issuetracker, utuber, slideshow, smbedit, menuserver, admin, grocery,
  certmachine, multissh are not in any commit manifest in §6.
- **FR-6 (icons/fonts consolidation).** `HamburgerMenu` renders its own
  private inline `fa-bars`-equivalent SVG; there is **no** `icons.ts` and no
  icon registry in this phase. todo keeps its 19 FontAwesome glyphs and both
  Google-Fonts `<link>` groups (`web/todo/index.html:8-10`,
  `web/todo/compare.html:8-10`). Deviation-ledger row 4.
- **New design tokens — one narrow exception, added in v5 by user ruling.**
  `--radius-xl` stays deferred to Phase 3 on Q2's original grounds
  (grocery-only, one consumer, no module in this phase reads it), and
  `tokens.css` (43 lines) is **not edited by any Phase-2 commit** — its
  freeze is absolute. **`--color-surface-dynamic` is the exception:** Q12 is
  answered *option B* (user, 2026-09-16 — "extend the themes so that any
  theme can be used with any module"), so it lands as the **18th key of
  T1** in **C1**, and `themes.css` goes 187 → **195 lines** in that one
  commit and is frozen for the other eight. `check-shared-css.mjs` clauses
  3–6 are unchanged *in logic*; clause 5's roster gains one entry at C1 (two
  lines — see §5 Step 1.5) and `gates/token-overlap.mjs`'s expected
  intersection `{--font-mono}` (`:27`) is unchanged by construction, because
  taskmaster's sheet declares no `--color-surface-*` name. **Out of scope
  and staying so:** retokenising todo, widening todo's picker past
  `{dark, light}`, and any further vocabulary growth — the user's broader
  mandate is recorded as Phase-3+ program direction in §11 item 2.
- **jQuery removal from todo**, and the deletion of
  `web/todo/js/jquery-ui.js` (520,714 bytes, verified-unused). §11 item 5.
- **todo's three hand-rolled modals** (`index.html:307-334`, `:337-359`,
  `:362-382`) → shared dialogs. §11 item 6.
- **`compare.html` gaining a hamburger.** It has none today — verified: zero
  occurrences of `menu-toggle`, `id="sidebar"`, or `sidebar-backdrop` in all
  105 lines — so FR-4 there is net-new adoption, not migration. §11 item 7.
  **This is a scope boundary, not a "don't touch" boundary, and the two were
  conflated in v1.** `compare.html` **is** edited at C6a, for one reason: it
  loads `js/theme.js` (`:18`), the file C6a deletes, and its
  `#theme-toggle` button carries an inline `onclick="toggleTheme()"` (`:41`)
  bound to that file's global. Deleting `theme.js` without editing
  `compare.html` leaves a live button calling an undefined function. C6a
  therefore re-homes `compare.html`'s toggle onto `ThemeManager` and leaves
  its markup otherwise untouched; it gains no drawer, no backdrop, no
  `#menu-toggle`. Full treatment in §4.8; the path is named in C6a's Touches
  cell in §6.
- **Styling the login page — but not, any longer, the `.mjs` carve-out
  itself.** *(Rewritten in v5; the bullet read "**Q9's `.mjs` half** — still
  open; see §9.")* The user closed Q9 on 2026-09-16 — *"yes, the login page
  should use the theme previously selected."* — so the **capability** moves
  **into** scope: `/shared/dist/shared.mjs` joins C8's unauthenticated
  allowlist as a third exact path shape (§5 Step 8.1) and **B6.3 flips from a
  negative probe to a positive one**. What stays out of scope is the
  **appearance**: no login-page markup is re-classed onto `.ui-*`, no token
  vocabulary is applied to it, and it appears in no P5/P5a baseline. C8
  remains a pure server-side allowlist commit with no pixel surface. §9 Q9
  records the ruling; §11 item 11 carries the follow-up.

---

## §2 — Preconditions

| | Precondition | How it is checked |
|---|---|---|
| **P1** | Branch is `ui-upgrade`; `main` is never touched; nothing is pushed | `git rev-parse --abbrev-ref HEAD` = `ui-upgrade` before the first commit; no `git push` appears in any step |
| **P2** | Per-commit path-scoped clean tree, side-cars untouched | `node scripts/gates/clean-tree.mjs <that commit's paths>` (`:55-57`); the gate is path-scoped and ignores untracked files by design (`:8-10`), so `tools/baseline-shots/*` and `docs/INVENTORY-*.md` can never be swept in. **v2 — this is a hand-run gate, deliberately, and iteration 1 was right to ask.** `clean-tree.mjs` is invoked by **nothing** in the repo: `grep -rn clean-tree` outside its own source returns no hit in `Makefile`, in any other script, or in any config. That is not an oversight to fix here — the gate **requires path arguments** (`:53` fails with "no paths given"), and the paths differ per commit, so it cannot be a `make gates` line without inventing a default that would be wrong at eight of nine boundaries. It stays operator-invoked, once per commit, with that commit's own Touches cell as its argument list. What v2 changes is that the plan no longer *relies* on it: **BX.4** asserts the same property from the other side, per commit, by inspecting what the commit actually contains (`git show --stat --name-only`), which needs no prior discipline at all. G16's principle applied to a gate that legitimately cannot live in a recipe |
| **P3** | The working tree's **current** dirty state is user side-car only | `git status --short` at plan time shows only modified `tools/baseline-shots/*` and untracked `docs/INVENTORY-*.md` + `tools/baseline-shots/interactions.js`. **No Phase-2 commit may contain any of these paths.** *(**v5 FINAL** — re-derived at the amendment pass, 2026-09-16, and published as an **operator note** because it is the one premise an executor inherits from the environment rather than from this document. `git status --short` returns exactly **7** lines: `M docs/OPEN-QUESTIONS-ui-unification.md`, `M tools/baseline-shots/README.md`, `M tools/baseline-shots/shoot.js`, `?? docs/INVENTORY-hamburger-menus.md`, `?? docs/INVENTORY-menuserver.md`, `?? docs/PLAN-ui-unification-phase2.md`, `?? tools/baseline-shots/interactions.js`. **Five of the seven are the exact paths BX.4 forbids in any commit** — they are user side-cars: never staged, never committed, never cleaned up by a step. The other two are this planning pass's own output (this plan, still untracked, and the companion question log). So the executor starts C1 on a tree that is *dirty by design*, and the discipline that makes that safe is per-commit and path-scoped, not global: `git add` only the paths in that commit's **Touches** cell, never `git add -A`, never `git commit -a`. **P2**'s hand-run `clean-tree.mjs` and **BX.4**'s `git show --stat --name-only` are the two sides of this; BX.4 is the one that cannot be forgotten, since it inspects the commit after the fact.)* |
| **P4** | Phase 1 is landed: barrel, `/shared/` route, sampler, 15 artifacts | `node scripts/gates/artifacts.mjs` passes and `scripts/descriptors.mjs:162` reads `EXPECTED_ARTIFACT_COUNT = 15` |
| **P5** | Baseline pixels captured before C5 and C6a | `tools/baseline-shots/` + its `interactions.js` scene harness, run against obsidianoid (5 themes × notes/threads mode) and todo (dark + light, drawer open and closed) **before** C5. Output stays untracked (P3). |
| **P5a** | **obsidianoid's baseline is re-captured *inside* C5, at the first point in C5 at which obsidianoid is renderable on its own vocabulary** — i.e. after **Step 5.3 items 1–4** (the shared link replaces the `css/themes.css` link in place, the local `themes.css` is deleted, the four renames are applied, and item 4 confirms `--color-surface-dynamic`'s two consumers need no edit — **v5**, user ruling on Q12) **plus** Step 5.4's first bullet, `index.html:2` `data-theme="dark"` → `"obsidian"`. Those five edits land as **one working-tree sub-step** and the capture follows them. Step 5.1 may land on either side (pixel-inert); **Steps 5.2 and 5.3 item 5 land after the capture** | The single-baseline model is wrong for obsidianoid and v1 missed it. `web/obsidianoid/index.html` links only `css/themes.css` (`:7`) and `css/app.css` (`:8`) — **no shared CSS, and therefore none of `web/shared/css/fonts.css`'s 15 `@font-face` rules** (2 Inter, 4 JetBrains Mono, 5 Sora, 4 IBM Plex Mono; the gate pins this exact number at `check-shared-css.mjs:99`, `EXPECTED_FONT_FACES = 15`, checked at `:365-366`. Beware `grep -c "@font-face" web/shared/css/fonts.css`, which returns **16** because the file's header comment at `:1` contains the literal string — the 15 real rules start at `:11`). Adding the `shared.css` link changes `--font-body`/`--font-mono` from the OS fallback stack to self-hosted Inter/JetBrains Mono, so **every glyph on the page reflows**. A pre-C5 baseline cannot be compared to a post-C5 render at the pixel level; comparing them would either fail spuriously or, worse, be waved off and hide a real regression. C5 therefore captures a second baseline once the module is rendering the shared matrix under its own theme name, and compares its own later steps against **that**. The font shift itself is a declared visual delta (ledger row 13, D-2), asserted by B2.6 as an intended change, not a preserved pixel. **v4 — the capture point is specified by *state*, not by edit order, and this is the third specification rather than the third reword** (Architect B-1/A1, Critic F-6; §5 Step 5.6 was unsatisfiable in v2 and again in v3, by two different mechanisms). "After the link lands and before any other C5 edit" names a tree in which obsidianoid **does not render**: the link alone, with `css/themes.css` already unlinked and the renames not yet applied, leaves **32** `var()` references undefined (R3's exact failure mode — 15 + 8 + 6 + 3 across the four renamed names; `--color-surface-dynamic`'s two are **not** among them, because post-C1 the shared sheet defines that name and the link has already landed) and — because `web/obsidianoid/css/themes.css:2` and `web/shared/css/themes.css:15` are **both** `:root, [data-theme="dark"]` at identical specificity, while `index.html:2` still says `dark` — recolours every panel on the page from obsidianoid's 12 colliding token values to shared `dark`'s. Naming the capture by the *state* obsidianoid must be in ("renderable on its own vocabulary") resolves the ordering ambiguity v3's wording created against §5's own numbering — 5.1 and 5.2 are edits and both precede 5.3 — and makes §5 Step 5.6's expected-residue list exact and small (ledger row 13/D-2 and D-1 — **v5**: D-3 is retired, so the list is shorter still and item 3 of that composition is now a zero-delta assertion) instead of a scope claim that was false at every candidate capture point. |
| **P6** | Q4, Q8 and **Q9** are settled; Q3 is not | `docs/OPEN-QUESTIONS-ui-unification.md:126-129` (Q4 — closed, and note it adjudicates the `tsconfig` glob, supplying only the *coupling principle* this plan borrows), `:187-199` (Q8 — closed), `:31` (Q3 — `--color-primary-fg`; **its checkbox is unticked `- [ ]` while its body states the values as settled by measurement**, so the plan treats the values as usable and the question as formally open; this asymmetry is itself recorded because a reviewer reading only the checkbox would conclude the values are unknown), `:200` (**Q9 — closed 2026-09-16 by user ruling**, moved into that file's `### Closed` section in the same pass that produced v5: the `.mjs` half is carved out and B6.3 is a positive probe). *(**v5**: all four coordinates moved, because closing Q9 and amending Q2 grew that file **160 → 235** lines — the committed baseline is `git show HEAD:docs/OPEN-QUESTIONS-ui-unification.md | wc -l` = **160**, and v5 published 161 by counting a trailing newline as a line; the parenthetical claimed to be grep-derived, so the one-unit error is the clause's own kind of defect and is corrected rather than annotated. v1–v4 read `:87-90`, `:148-160`, `:19`, `:40`, and the last of those read "Q9 — **open**". Re-derived by `grep -n`, not adjusted by arithmetic. Q3 is the only one of the four still in the open list.)* |

---

## §3 — Guardrails

Phase-1 G1–G11 apply unchanged. Phase 2 adds **five** (G12–G16) and narrows
two. G16 is new in v2.

- **G1 — No source file outside the listed paths.** Each commit in §6 names
  every path it touches.
- **G2 — Committed build artifacts stay byte-identical unless the commit's
  purpose is to change them.** Enforced by `make web-verify`
  (`Makefile:57-58`). Phase 2 has **no** byte-identity exemption: the
  `$(date -u)` stamp was retired at Phase-1 C2, so `--allow` is used at no
  boundary here.
- **G3 — Phase 2 edits module CSS/HTML/TS in exactly three modules**:
  `web/sampler/`, `web/obsidianoid/`, `web/todo/`. Every such edit appears in
  §6's Touches column and in §5's sanctioned-delta table.
- **G4 — No new dependency.** `package.json:13-18` `devDependencies` (4
  packages, still no `@types/node`) and `:19-25` `dependencies` (5 packages)
  are unchanged. The new tests use the hand-rolled element stub idiom of
  `web/shared/ts/modal.test.ts:15-53` — no jsdom (Phase-1 ADR-005).
- **G5 — Every route claim is proved by a two-sided probe.** Applies to C8's
  carve-out: the allowlisted path is reachable unauthenticated **and** a
  sibling path under the same prefix is not.
- **G6 — Gate composition is uniform**, and every point where it differs is
  named in §6's composition table.
- **G7 — No `.omc/` path is ever committed.** Covered by `.gitignore`.
- **G8 — Reversibility.** §6 states the one revert-order constraint.
- **G9 — Binary assets carry provenance — and from C1 this is *enforced*.**
  Phase 1 stated G9 as prose: `web/shared/public/fonts/` may contain only
  `*.woff2`, `OFL.txt`, and the tracked `SHA256SUMS`. No gate checks it.
  `check-shared-css.mjs` clause 10 (`:358-401`) checks that every `url()`
  resolves and that every `.woff2` on disk is referenced exactly once — it
  says nothing about a stray `.zip`, `.ttf`, or an unlisted digest. C1 adds
  **clause 12** (§4.6).
- **G10 — `web/shared/` references no host but the origin serving it.**
  Unchanged: the 5 Google-Fonts `<link>` groups across 4 module HTML files
  survive Phase 2 too, because FR-6 is out of scope (§1). Two of the five are
  in todo (`index.html:8-10`, `compare.html:8-10`) — a module this phase
  *does* touch — so this is a deliberate non-removal, ledger row 4, not an
  oversight.
- **G11 — No gate is a shell one-liner inside a make recipe.** Every gate
  Phase 2 adds or extends is a script file invoked as one process
  (`Makefile:49-55`).
- **G12 — Phase 2 declares no new design token, with exactly one named
  exception.** *(Amended in **v5** on the user's Q12 ruling, 2026-09-16:
  *"extend the themes so that any theme can be used with any module."* The
  amendment is deliberately written as a **closed enumeration**, not as a
  relaxation: the unamended rule is what made B1.1 a one-line ranged diff
  rather than a token audit, and it keeps doing that everywhere outside the
  one carve.)*
  - `web/shared/css/tokens.css` is in **no** commit manifest. Its freeze is
    absolute; `--radius-xl` stays in Phase 3.
  - `web/shared/css/themes.css` is in **exactly one** commit manifest, **C1**,
    for **exactly one** addition: `--color-surface-dynamic` as T1's **18th**
    key, 8 declarations plus a header-comment correction (§5 Step 1.5). At
    C2…C8 it is in no manifest and B1.1's ranged diff over it must be empty.
  - Any colour a new component needs must already exist among the **18**
    `--color-*` keys or the 27 structural names. No *second* token may be
    added in this phase on this precedent: a further addition is a plan
    revision, and **B1.1 part C**'s ranged diff from C1's SHA fails at the
    boundary of any later commit that touches the file, so a second addition
    cannot pass as "the same carve". *(**v5 FINAL**, Architect A-2. This bullet
    read "§13(e) counts `themes.css` edits per commit", which is false in the
    plainest way: §13(e) contains **zero** occurrences of `themes.css` — it
    checks §6's Touches tokens against a prefix allowlist and would admit a
    `web/shared/css/themes.css` token at any boundary, because `web/shared/` is
    the first entry in that allowlist. B1.1 part C is the real assertor, and it
    is the one that actually bites.)*
  - The carve's limit is machine-checked from both sides: **B1.1** pins the
    diff's *shape* at C1 (8 added declarations, one per theme block, all the
    same property name) and its *emptiness* everywhere else, and clause 5 then
    constrains 8 × 18 cells instead of 8 × 17 — so after C1 the vocabulary is
    more tightly pinned than it was before, which is the test a carve has to
    pass to be a carve.
- **G13 — Every new shared CSS rule lands in
  `web/shared/css/components.css`.** D2. No new file under
  `web/shared/css/`, so `index.css` (8 lines) is unchanged and clause 8's
  import check has nothing new to validate.
- **G14 — No shared component reads or writes `innerHTML` of an interpolated
  string.** FRD `:327-328`. Note that the *donor* pattern being replaced
  violates this: `web/obsidianoid/js/app.ts:438` builds the theme swatch with
  ``btn.innerHTML = `<span class="theme-swatch" style="background:${t.color}"></span>${t.label}` ``. The shared theme picker builds the
  same node with `createElement` + `textContent`. Asserted by B3.6.
- **G15 — `EXPECTED_ARTIFACT_COUNT` moves exactly once in Phase 2**, at
  **C6a**, 15 → 16, as a **two-line** edit to `scripts/descriptors.mjs`: the
  constant at `:162`, **and** the one value-bearing line of the trajectory
  comment that narrates it. That comment spans `:157-161`, and the sentence
  carrying the numbers is on **`:160`** — "*sequence — 12 through C3, 14 after
  C4, 15 after C5 — and each move is an*" — so the second edited line is
  `:160`, not a range. *(v2 cited the comment as `:157-158`; verified wrong in
  the tree — `:157-158` is the comment's opening sentence about defining the
  count once, and contains no numbers. Corrected in all three places v2 stated
  it: here, §5 Step 6.1 and §14's M6 row.)* That comment
  is Phase 1's own record of the count's history; leaving it stale would
  contradict the one-definition rule in the very file that exists to enforce
  it, so the comment gains `, 16 after Phase-2 C6a`. Every other commit leaves
  both lines alone. §6's artifact column is the record.
- **G16 — a gate flag the plan relies on must live in the Makefile recipe, not
  only in the gate's own argument parser.** New in v2, and it exists because
  v1 broke it twice: the plan's correctness arguments leaned on
  `bundle-shape.mjs`'s `--expect-nonempty` (parsed at `:45`) while
  `Makefile:62` invokes the script **bare**, so the flag was never passed and
  the argument was about code that does not run. P-III ("a gate that cannot
  fail is not a gate") has a corollary: *a gate that is not invoked with the
  flag the plan cites is not the gate the plan describes.* Concretely, for
  every gate Phase 2 adds or extends, the commit that relies on a flag also
  edits `Makefile`'s recipe line to pass it, and `Makefile` appears in that
  commit's Touches cell in §6 — which is why `Makefile` now joins the manifests
  of **C1, C5, and C6a**.

---

## §4 — Current reality (verified, not assumed)

Every line number in this section was read in this tree.

### 4.1 The shared library as Phase 1 left it

| File | Lines | Contents |
|---|---|---|
| `web/shared/ts/index.ts` | 12 | The barrel: 6 named values + 4 types, explicit re-exports only |
| `web/shared/ts/modal.ts` | 297 | `openModal` `:38-123`, focus trap `:65-96`, focus return `:110-112`, initial focus `:119-121`; `confirmDialog`/`alertDialog`/`promptDialog` |
| `web/shared/ts/theme.ts` | 21 | `THEMES` `:8-17` (8 names, `as const`); `setTheme` `:19-21` writing `document.documentElement.dataset.theme` |
| `web/shared/ts/modal.test.ts` | 137 | Hand-rolled `FakeElement` `:15-38`, `document` stub `:47-53`, barrel-shape checks `:128-137` |
| `web/shared/css/components.css` | 101 | `.ui-modal-*` only, `:12-101` |
| `web/shared/css/tokens.css` | 43 | 27 structural names on `:root` `:9-43`; `--overlay-scrim: rgba(0,0,0,0.55)` `:42` |
| `web/shared/css/themes.css` | 187 | 8 blocks; `:root, [data-theme="dark"]` first at `:15`; `obsidian` at `:61-79` |
| `web/shared/css/index.css` | 8 | four `@import`s |

`scripts/check-shared-barrel.mjs:25-26` holds the allowlist as two literal
arrays — **not derived**, so every new export is a deliberate two-place edit
(barrel + allowlist) in one commit:

```js
const ALLOWED_VALUES = ["openModal", "confirmDialog", "alertDialog", "promptDialog", "THEMES", "setTheme"];
const ALLOWED_TYPES = ["ModalOptions", "ModalHandle", "DialogOptions", "PromptOptions"];
```

### 4.2 The harness

`Makefile:60-64` `gates` runs four scripts; `:71` `check` is
`web-verify test-web gates test`. `scripts/test-web.mjs:39-46` is a fixed
six-suite list, `shared-modal` among them. `scripts/descriptors.mjs` holds 10
descriptors; `:162` is `EXPECTED_ARTIFACT_COUNT = 15`.
`tsconfig.json:15` has **11** `include` globs; `web/shared/**/*.ts` already
covers anything added under `web/shared/ts/`, so C1–C4 need no `tsconfig`
edit.

#### 4.2.1 `bundle-shape.mjs` — three defects, and one of them invalidated a v1 argument

`scripts/gates/bundle-shape.mjs:52` selects its inputs as
`descriptors.filter(d => d.sharedConsumer === true)` and `:73` does
`fs.readFileSync(d.out, "utf8")`.

**(a) `--expect-nonempty` is a no-op today, and v1 leaned on it anyway.** The
flag is parsed at `:45` and consulted at exactly one place, `:55`, inside the
`if (inputs.length === 0)` branch at `:54`. But **the input set is not empty
and has not been since Phase 1**: `sampler` (`descriptors.mjs:70-77`) and
`taskmaster` (`:116-123`) both carry `sharedConsumer: true`. Verified by
running it:

```
$ node scripts/gates/bundle-shape.mjs
bundle-shape: PASS — 2 sharedConsumer bundle(s) inspected      exit=0
$ node scripts/gates/bundle-shape.mjs --expect-nonempty
bundle-shape: PASS — 2 sharedConsumer bundle(s) inspected      exit=0
```

Two consequences, both corrections to v1:

1. The flag's guarded branch is **unreachable**, so `--expect-nonempty`
   **cannot fail**. v1 cited it as the mechanism ensuring obsidianoid is
   actually inspected at C5; it cannot do that, because *global* non-emptiness
   was already satisfied by two other descriptors. A flag that cannot fail is
   P-III's own target. *(Architect M9 is correct on this and v1's C1 note is
   corrected accordingly; the framing "the vacuous path is what will fail at
   C6" was describing a path that no longer executes.)*
2. `Makefile:62` invokes the script **bare** — no flag at all — so even the
   no-op was not being passed. This is the defect that motivated **G16**.

The gate's own header compounds it: `:19-20` still reads "*The set is EMPTY
until C5 (sampler at C5, taskmaster at C6)*", which described Phase 1
mid-sequence and is now stale by two descriptors. C1 corrects the comment in
the same edit that adds the flag, because a stale comment about which set is
empty is exactly what let v1's error survive review.

**The fix — `--require=<name>,…`, landing at C1.** Per-descriptor, so it can
fail for the reason the plan actually cares about: *this named descriptor is in
the inspected set*. `--expect-nonempty` is kept as-is (harmless, and still
meaningful if every consumer were ever removed). `Makefile:62` becomes
`node scripts/gates/bundle-shape.mjs --require=sampler,taskmaster`, extended to
`+obsidianoid` at C5 and `+todo` at C6a. That is three `Makefile` edits across
three commits — G16's manifest requirement, and why `Makefile` appears in the
Touches cell of C1, C5 and C6a in §6.

**(b) `d.out` is a directory for a `mode: "transpile"` descriptor, and the gate
crashes rather than failing.** `scripts/descriptors.mjs:83` is
`out: "web/obsidianoid/js/"`. At `:69` `fs.existsSync(d.out)` returns **true**
for a directory, so the guard does not catch it, and `:73`'s
`fs.readFileSync(d.out, "utf8")` then throws an uncaught `EISDIR` — a stack
trace, not a gate failure with a message. The moment obsidianoid carries
`sharedConsumer: true`, `make gates` breaks this way.

> **Rebuttal — Critic M-?/"fix obsidianoid's descriptor `out` to a file
> path".** That fix is mechanically impossible and would break the artifact
> gate. obsidianoid is a **2-entry transpile**, and
> `scripts/list-artifacts.mjs:38-45` derives its outputs by joining `d.out`
> with a per-entry basename (`path.posix.join(d.out, jsName(entry))`). `d.out`
> **must** stay a directory for a multi-entry transpile, or two artifacts
> collapse onto one path and `EXPECTED_ARTIFACT_COUNT` can never reconcile.
> The correct fix is the one Step 5.1 already specified — have `bundle-shape`
> iterate the *derived artifact paths* rather than `d.out` — so what needed
> repair was §4.2's and B9.3's framing, which implied `d.out` was itself the
> file to read. Framing corrected here; the step stands.

**(c) The derivation to extract is wider than v1's range.** v1 cited
`scripts/list-artifacts.mjs:38-42` as holding the descriptor→paths logic. The
mode dispatch is `:38-45`, and it is **not self-contained**: the transpile
branch calls `jsName(entry)`, defined at **`:25-27`**
(`return path.basename(entry).replace(/\.tsx?$/, ".js");` at `:26`). Extracting
`:38-45` alone yields a module with an undefined reference. The extraction is
therefore **`:25-27` + `:38-45`** → a shared `artifactPaths(d)` helper that
both `list-artifacts.mjs` and `bundle-shape.mjs` import. *(Architect M1 is
correct.)* §5 Step 5.1 carries the corrected range.

### 4.3 obsidianoid

7 web files, all tracked, including the two emitted `.js`:
`index.html` 134, `css/app.css` 812, `css/themes.css` 189, `js/app.ts` 507,
`js/app.js` 472, `js/threads.ts` 241, `js/threads.js` 174.

- **Theme roster is stated three times.** `css/themes.css` has 5 depth-1
  blocks — `:root, [data-theme="dark"]` `:2-37`, `forest` `:40-75`, `ocean`
  `:78-113`, `ember` `:116-151`, `rose` `:154-189` — each declaring the same
  **38** properties (17 colour + 21 structural). `js/app.ts:46-52` holds a
  5-entry `THEMES` array whose `color:` field is a **third** copy of each
  theme's `--color-primary` hex. There is **no light theme**.

  > **Count note — 38 declarations on 34 lines, and both reviewers read the
  > lines.** Iteration 1's Architect (M7/B2) and Critic (M-10) independently
  > reported 34 properties and asked for the plan's 38 to be corrected. **The
  > plan's 38 is right and is retained.** The file declares two `--space-*`
  > tokens per line at `:26` and three at `:27`, so lines and declarations
  > diverge by exactly four per block. Arithmetic over the whole file, which
  > divides cleanly by 5 and settles it either way:
  >
  > ```
  > grep -o -- "--[a-z0-9-]*:" web/obsidianoid/css/themes.css | wc -l   → 190   (= 5 × 38 declarations)
  > grep -c  -- "--[a-z0-9-]*:" web/obsidianoid/css/themes.css          → 170   (= 5 × 34 lines)
  > grep -o -- "--[a-z0-9-]*:" web/obsidianoid/css/themes.css | sort -u | wc -l → 38   (17 --color-* + 21 structural)
  > ```
  >
  > This is recorded rather than silently kept because two reviewers will
  > re-check it: the number 34 is a correct count of a different thing.

- **The blocks are *not* identical to each other — 16 of the 38 tokens vary.**
  v1 said "identical", which was wrong and is corrected here. Varying: **14
  `--color-*`** (the palette proper — `bg`, `border`, `divider`, `primary`,
  `primary-active`, `primary-highlight`, `primary-hover`, `surface`,
  `surface-2`, `surface-dynamic`, `surface-offset`, `text`, `text-faint`,
  `text-muted`) plus **2 structural**, which are the interesting ones because
  structural tokens are supposed to be theme-invariant:

  | token | `dark` | `forest` / `ocean` / `ember` / `rose` | consumers in obsidianoid | consequence |
  |---|---|---|---|---|
  | `--shadow-sm` | `0 1px 3px oklch(0 0 0 / 0.3)` | `… / 0.35` | **0** (`grep -c 'var(--shadow-sm)'` = 0) | **None.** Declared five times, read zero times. Deleting it is invisible by construction — no pixel can change |
  | `--shadow-md` | `0 4px 16px oklch(0 0 0 / 0.4)` | `0 4px 16px oklch(0 0 0 / 0.45)` | **3** | Real. Shared `tokens.css:16` is `0 8px 32px rgba(0, 0, 0, 0.4)` — a **different geometry**, not just a different alpha, so all **five** themes shift, not the four with `/0.45`. Sanctioned delta; see the structural-delta bullet below |

  The other **22** tokens are genuinely invariant across all five blocks.

- **All five donor themes already exist in the shared matrix with
  hex-identical values — not just `obsidian`.** v1 verified only the
  `obsidian` block and left the other four as an assumption; verified here
  across every mapped colour name in all five blocks, after applying the four
  renames in the table below. *(v2–v4 also had to set `--color-surface-dynamic`
  aside, because it had no shared counterpart. **In v5 nothing is set aside:**
  the user's Q12 ruling makes it T1's 18th key at C1 (§5 Step 1.5), donated
  verbatim from these same five blocks, so the parity check now covers **every
  one** of obsidianoid's 17 colour names.)*

  | donor block | shared block | names compared | colour mismatches |
  |---|---|---|---|
  | `:root, [data-theme="dark"]` `:2-37` | `[data-theme="obsidian"]` `:61-79` | 17 | **0** |
  | `[data-theme="forest"]` `:40-75` | `[data-theme="forest"]` `:82-100` | 17 | **0** |
  | `[data-theme="ocean"]` `:78-113` | `[data-theme="ocean"]` `:103-121` | 17 | **0** |
  | `[data-theme="ember"]` `:116-151` | `[data-theme="ember"]` `:124-142` | 17 | **0** |
  | `[data-theme="rose"]` `:154-189` | `[data-theme="rose"]` `:145-163` | 17 | **0** |

  **85 comparisons, 0 mismatches, 0 unmapped names** — re-derived by machine in
  v5 against the post-Step-1.5 shared file, applying the four renames and
  mapping `--color-surface-dynamic` to itself. The only shared colour key with
  **no** obsidianoid donor is `--color-primary-fg`, in all five blocks; it is
  authored by Step 1.4's contrast matrix and is reported by the same script, so
  its absence is a measured result rather than an omission. *(Shared-block line
  numbers here are today's pre-C1 tree — Step 1.5's coordinate table carries the
  post-C1 values.)*

  This is the single most load-bearing fact in C5: it is what makes the
  commit a *rename plus delete* rather than a re-authoring, and it is what
  makes B2's pixel-parity claim provable for **five** themes instead of one.
  The 16 varying colour values are exactly the palette differences, and every
  one of them is already reproduced in the shared file.
- **Token-name deltas obsidianoid must absorb.** The two rosters are both 17
  colour names, but **only 12 names overlap** — so five must be resolved, not
  four (v1's prose said "four" above a five-row table; the table was right).
  All **21** structural names obsidianoid declares exist in
  `web/shared/css/tokens.css` under the same spelling (verified: zero absent;
  tokens.css declares 27, so obsidianoid uses a 21-name subset).

  Counts below are `var()` consumption sites, split by file kind, because v1
  censused `web/obsidianoid/css/*.css` only and thereby under-reported
  `--color-error` by a factor of three — its other sites are interpolated
  inline styles inside **TypeScript**:

  | obsidianoid name | CSS | TS | shared name | note |
  |---|---|---|---|---|
  | `--color-surface` | 6 | 0 | `--color-surface-1` | value-preserving (`#1a1a24`) |
  | `--color-surface-offset` | 15 | 0 | `--color-surface-3` (**all 15**) | value-preserving at **all 15** (`#252535` on both sides). *v5: v4 split this 12/3 because option A shifted 3 occurrences onto `--color-surface-2`; the user's Q12 ruling removed the pair shift, so the split is gone and the rename is uniform* |
  | `--color-primary-highlight` | 8 | 0 | `--color-primary-tint` | value-preserving (`#2a2548`) |
  | `--color-error` | 3 | **3** | `--color-danger` | value-preserving (`#e05c7a`). `check-shared-css.mjs` clause 11 (`:403-425`) already bans `--color-error` under `web/shared/`. The three TS sites are `js/threads.ts:68`, `js/app.ts:142`, `js/app.ts:188`, all inside `innerHTML` template literals — **G14 territory**, and each is mirrored in the tracked emitted `js/threads.js` and `js/app.js`, so C5's commit necessarily carries the rebuilt `.js` for `make web-verify` to stay green |
  | `--color-surface-dynamic` | 2 | 0 | **`--color-surface-dynamic`** — the same name, now shared | **0 authored edits.** §5 **Step 1.5** lands it as T1's 18th key at **C1** on the user's Q12 ruling, so `css/app.css:202` (the `linear-gradient` skeleton shimmer) and `:455` keep the name they already have and simply resolve from `web/shared/css/themes.css` instead of the deleted local one. Value-preserving on `obsidian` by construction — the shared value **is** obsidianoid's donated `#2e2e42` |

  **Rename surface: 32 CSS sites + 3 TS sites = 35 authored edits, all 35
  value-preserving**, plus 3 mechanically-regenerated JS mirrors. Re-derived by
  machine under option B: `6 + 15 + 8 + 3 = 32` CSS `var()` sites across the
  four renamed names, `3` TS sites (`--color-error` only), and **zero** edits
  for `--color-surface-dynamic`. *(v5 — this repairs a number rather than
  moving one. v4 wrote the same "32 CSS sites" while also rewriting the two
  `--color-surface-dynamic` sites, which made the true CSS edit count 34; and
  its "3 shift one surface step" counted the pair-shift occurrences, not the
  renames. Option B makes 32 correct and makes the value-preserving count
  total.)*

  > **The `--color-surface-dynamic` conflict — new in v2, and user-owned.**
  > Q2 (`docs/OPEN-QUESTIONS-ui-unification.md:79-90`) closed this token as
  > "**yes, but it lands in Phase 3**" *and*, two sentences later, as landing
  > "**with obsidianoid's own migration, which is also when its value is
  > testable**". Those are now the same event: obsidianoid's migration is
  > Phase-2 C5. Q2 also states the token "has exactly one consumer"; there are
  > **two** (`app.css:202`, `:455`). *(**v5:** the next sentence was v2–v4's
  > binding premise and is now historical — it is kept because the resolution
  > below is only intelligible as an answer to it.)* "The plan cannot declare
  > an 18th token either way, because **G12 forbids it** and B1.1 asserts
  > `tokens.css`/`themes.css` are untouched." Both halves have since been
  > amended by ruling rather than by argument: G12 now carves exactly this one
  > token (§3) and B1.1 is carved with it at exactly one commit.
  >
  > **The naive remap is a functional regression, and this is why the token
  > needs a decision rather than a rename.** In obsidian the four surfaces run
  > `surface #1a1a24` < `surface-2 #1f1f2e` < `surface-offset #252535` <
  > `surface-dynamic #2e2e42`. Shared has three, and `--color-surface-3`
  > (`#252535`) is the **lightest** — there is no "one step above
  > `surface-3`". Both `surface-dynamic` sites exist precisely to sit one step
  > *above* `surface-offset`, so collapsing both onto `surface-3` erases the
  > contrast that carries the meaning:
  >
  > | site | rule | what collapsing breaks |
  > |---|---|---|
  > | `app.css:202` | `.skeleton-text` shimmer, `linear-gradient(90deg, surface-offset 25%, surface-dynamic 50%, surface-offset 75%)` | all three stops become `#252535` → a **flat** gradient, and the `shimmer` animation at `:204` becomes **invisible**. The loading state stops reading as loading |
  > | `app.css:455` | `#mode-switcher button.active` background, against the container's own `surface-offset` at `:437` | active fill equals container fill → the **selected tab becomes indistinguishable**. `:458`'s `:hover:not(.active)` changes only `color`, so nothing else marks the selection |
  >
  > ***Resolved in v5 — the user chose the 18th token.***
  >
  > **Q12 was the user's call and the user made it (2026-09-16):** *"…extend
  > the themes so that any theme can be used with any module."* That is
  > **option B**. Everything above this line is preserved because it is the
  > derivation of *why* the question was worth asking — the collapse-breaks-it
  > analysis is what rules out the cheap answer — but the **plan of record is
  > no longer the pair shift.** v4's three-row remap table is deleted, not
  > amended, and with it:
  >
  > | v4 artefact | v5 disposition |
  > |---|---|
  > | the three rule-local `app.css` edits (`:202`, `:437`'s shift, `:455`) | **deleted.** The **`--color-surface-dynamic` name is unchanged at both of its sites**, `:202`'s middle gradient stop and `:455`'s background (§5 Step 5.3 **item 4**), and `:455` is therefore not edited at all; `:202` and `:437` keep only the plain, value-preserving `--color-surface-offset` → `--color-surface-3` rename that **item 3** applies everywhere (`:202` carries **two** such references, `:437` one) |
  > | **D-3** ("shift one surface step at 3 edits") | **retired to zero delta** (§5 Step 5.5) |
  > | ledger row **14** | rewritten to record option B and the user's ruling |
  > | **B2.8** ("the pair shift preserved one step of contrast") | rewritten to assert two *distinct* computed colours on **8** themes |
  > | `--color-surface-offset`'s 12/3 split | gone; uniform, value-preserving at 15 |
  >
  > **Why this is a better answer and not merely the user's answer.** The pair
  > shift bought G12 compliance with a permanent pixel debt in the one module
  > whose identity C5 exists to preserve: obsidian's lit element would have
  > gone `#2e2e42` → `#252535` and its ground `#252535` → `#1f1f2e`, forever,
  > to avoid authoring three hex values. Option B authors those three values
  > once (§5 **Step 1.5**: `dark #373c43`, `light #d3d0cc`, `puma #182737`,
  > derived from each palette's own ladder), donates the other five verbatim
  > from obsidianoid itself, and leaves obsidianoid's rendering **byte-exact**
  > where v4 declared a regression. The 8-values-for-8-themes problem this
  > blockquote used as the argument against option B is real and is simply
  > **solved** rather than avoided — with the user explicitly taking the
  > visual-validation step: *"I'll need to visually validate each one
  > anyway."*
  >
  > **What option B costs, stated plainly:** G12 is amended to a closed
  > one-item enumeration (§3), B1.1's freeze is carved at C1 and re-closed for
  > C2…C8 with a machine-checked diff shape, and `themes.css` +
  > `web/shared/dist/shared.css` join C1's manifest. Q2's Phase-3 deferral in
  > `docs/OPEN-QUESTIONS-ui-unification.md:79-90` is **overridden** by this
  > ruling; the override and its reasoning are recorded **at `:91-125` of that
  > file, as a dated amendment appended under Q2's original answer** — the
  > original text is left standing rather than rewritten, so the two documents
  > disagree on purpose and visibly instead of quietly. That amendment also
  > carries the two premise corrections below and records that **`--radius-xl`
  > is not swept along**: it stays deferred on its own grounds (grocery-only,
  > one consumer, unread by any module in Phase 2's manifest), because the
  > ruling widens the theme *colour* vocabulary, not the structural token set.
  > Q2's other two errors are unaffected and still stand
  > corrected above: this token has **two** consumers, not one, and "Phase 3"
  > and "with obsidianoid's migration" were never two different events.

- **Structural values that change when shared tokens take over**, all
  declared as sanctioned deltas in §5 Step 5.6 *(v1 mis-pointed this at "Step
  6.5"; obsidianoid is Step 5)*:
  - `--shadow-md`: `0 4px 16px oklch(0 0 0 / 0.4)` in `css/themes.css:25` vs
    `0 8px 32px rgba(0, 0, 0, 0.4)` in `web/shared/css/tokens.css:16` (a
    documented Phase-1 exception, `tokens.css:3-4`). The offset and blur both
    double, so this is a **geometry** change and it lands on **all five**
    themes — including the four whose donor alpha was `/0.45`. **3**
    consumption sites.
  - `--shadow-sm`: differs across donor blocks (`/0.3` vs `/0.35`) and the
    shared value is `/0.3` (`tokens.css:15`), but it has **0** consumers in
    obsidianoid, so this one is a no-op with no pixel to assert. Recorded only
    so a reviewer re-deriving the variance table does not read its absence
    from the delta list as an omission.
  - `--font-body` gains `system-ui` in its fallback chain
    (`tokens.css:33,35`) — and, far more visibly, adding the `shared.css` link
    brings the 15 self-hosted `@font-face` rules into a page that has none
    today, reflowing every glyph. That is P5a's whole subject, ledger row 13,
    asserted by B2.6.
- **Hamburger without a drawer.** `index.html:54-56` is `#btn-hamburger`
  (inline 3-line SVG); it toggles the theme popover and nothing else
  (`js/app.ts:449` trigger, `:444-447` toggle, `:450-455` outside-click
  dismiss). No backdrop, no Escape handler for the panel, no focus trap, no
  `aria-expanded`/`aria-controls`. `grep -rni "drawer\|off-canvas\|nav-toggle"
  web/obsidianoid/` → no matches. It sits inside `#topbar-actions`
  (`index.html:32-57`), which `css/app.css:463` hides entirely in threads
  mode — **so the theme picker is currently unreachable from the Threads
  view.** C5 fixes that as a side effect and it is recorded as a sanctioned
  delta.
- **Toast is a single-slot `className` assignment.** `index.html:102` is
  `<div id="toast" role="status" aria-live="polite">`; `js/app.ts:54-61`
  `showToast(msg, type = 'success')` with a fixed 2800 ms timer and only
  `success`/`error` tones. 16 call sites; 5 of them are in `threads.ts`
  reaching a **cross-file global** declared at `threads.ts:15`.
- **The other cross-file global is `window.ThreadsView`**, assigned at
  `threads.ts:22` and consumed at `app.ts:3` (`declare const`), `:413`,
  `:418`, `:504`. Two separate questions hang on it, and v2 conflated them:
  - *At **runtime** it survives the move to ESM unchanged.* It is a real
    property on the real `window` object, so module scoping does not hide it;
    only the execution order matters, and `type="module"` preserves document
    order.
  - *At **compile time** it does **not**.* `threads.ts:17-20` declares
    `interface Window { ThreadsView: ThreadsViewAPI }` at top level, and that
    merges into the **global** `Window` only while the file is a *script* —
    which it is today: `grep -cE '^(import|export)' web/obsidianoid/js/threads.ts`
    is **0**. The moment Step 5.2 adds `import { showToast } from "@shared"`
    the file becomes a module, the interface becomes module-local, and the
    assignment at `:22` fails `tsc --noEmit` with **TS2339 —
    `Property 'ThreadsView' does not exist on type 'Window & typeof
    globalThis'`**. Verified by reducing both files to a probe and running
    `npx tsc --noEmit --strict`: the error reproduces, and it disappears when
    the declaration is rewritten as
    `declare global { interface Window { ThreadsView: ThreadsViewAPI } }`.
    The same probe in *script* form rejects `declare global` with **TS2669**
    ("*Augmentations for the global scope can only be directly nested in
    external modules*"), so the rewrite is legal **only** in the commit that
    adds the import — it cannot be pre-landed. Step 5.2 carries it as an
    explicit sub-edit and C5 carries a `npm run typecheck` acceptance
    criterion (**B2.9**) for it.
- **Light-theme correctness defects** named by FRD `:433-450`:
  `css/app.css:740`'s `#vault-selector` chevron data URI with
  `stroke='%237878a0'` (URL-encoded `#7878a0` = obsidian's
  `--color-text-muted`); `:810` `.theme-swatch { border: 1px solid
  rgba(255,255,255,0.15) }`; `:634` `.disabled-overlay { background:
  rgba(19,19,26,0.72) }` with a comment admitting it hand-copies
  `--color-bg`; three `color: #fff` on primary-filled buttons at `:90`,
  `:404`, `:540`.
- **Descriptor** `scripts/descriptors.mjs:79-86`: `mode: "transpile"`,
  `bundle: false`, two entries, `out: "web/obsidianoid/js/"`, **no
  `format`**, **no `sharedConsumer`**.
- **Server-side default** `internal/obsidianoid/build.go:40-41` sets
  `cfg.Vaults[i].Theme = "dark"` when empty. `"theme": "dark"` also appears
  at `unified-webapp.json:53`, `unified-webapp-example.json:67`,
  `local-test/config.json:63`, and `README.md:88`.
  *(`internal/platform/config/config.go:307,486`'s `DefaultTheme: "dark"`
  belongs to **SlideshowConfig** — out of scope.)*

### 4.4 todo

59 files, 1.7M. `web/todo/index.html` 388, `compare.html` 105,
`css/todo.css` 619, `js/theme.js` 27, `js/todo.js` 529,
`js/todo-utils.js` 961, `js/compare.js` 392, `js/utils.js` 161.
**todo is not in the build pipeline**: `grep -i todo scripts/descriptors.mjs`
→ no matches; there is no `.ts` file under `web/todo/`.

- **The drawer is the repo's most complete one, and has no a11y at all.**
  `#menu-toggle` `index.html:172-174`; `#sidebar-backdrop` `:195`;
  `<aside id="sidebar">` `:201-252`; `toggleSidebar`/`closeSidebar` at
  `:78-85` — **inside the 144-line inline `<script>` at `:21-164`**, not in
  any `.js` file. CSS: `.sidebar` `todo.css:103-120`,
  `.sidebar.sidebar-open` `:122`, `.sidebar-section` `:124`,
  `.sidebar-divider` `:126-130`, `.sidebar-backdrop` `:134-144`, mobile width
  `:617-619`. *(**v4** — `.sidebar-section` `:124`, `padding: .5rem 0`, was
  missing from this inventory. §5 Step 6.2's class list and Step 6.2a's
  per-declaration table both carry it (→ `.ui-menu-section`, `var(--space-2)
  0` = the same `0.5rem`, no D-row), so this list was the one place in the
  document that itemised the `:103-144` block and left one of its six rules
  out. Architect A9; no touch list and no D-row changes.)* There is **no
  `aria-*` attribute of any kind** in the live
  pages (the sole exception is `role="progressbar"` at `index.html:260-263`);
  `:149-151`'s document Escape handler closes the three modals and **not**
  the drawer; there is no focus trap and no focus return.
- **Nine controls plus three pieces of chrome** must be preserved — the full
  table is §7's B4.2.
- **Theme system** `js/theme.js` (27 lines, IIFE): key `'todo-theme'` `:2`;
  `applyTheme` `:4-10` (also swaps the `#theme-icon` FA class at `:8`);
  `getPreferred` `:12-16` with **inverted-polarity** `matchMedia('(prefers-color-scheme: light)')` at `:15`; pre-paint call at `:19` (works
  only because it is a `<head>` script, `index.html:20`); `window.toggleTheme`
  `:21-26`. Two themes only (`'dark'`/`'light'`), **bare unprefixed token
  names** (`--bg`, `--surface`, `--accent`, …, 13 of them) declared at
  `todo.css:3-17` and `:19-33`. No `storage` listener, no `matchMedia` change
  listener. The toggle is duplicated at `compare.html:41-43` with the same
  `id`.

  > **The inverted polarity is a real behavioural delta at exactly one input,
  > and C6a resolves it toward the shared manager.** `theme.js:15` probes
  > `'(prefers-color-scheme: light)'` and falls back to `dark`; the shared
  > `ThemeManager` probes `dark` and maps the negative case to `light`. FRD
  > `:250-251` fixes the *order* — "localStorage → `serverDefault()` →
  > `system` (via `prefers-color-scheme`, mapping to `light`/`dark`) →
  > `default`" — but is silent on the probe's **polarity**, so this is the
  > plan's call and not a quotation. The two agree whenever the OS expresses a
  > preference. They differ
  > only under **`no-preference`**, where neither query matches: today's todo
  > lands on `dark`, the shared manager lands on `light`. **Decision: adopt
  > the shared polarity**, because the alternative is a per-module override of
  > the one resolution rule FRD `:250-251` defines — a second definition of
  > "what the system preference means" — to protect a case that has no
  > persisted-choice path leading into it (any user who has ever toggled has a
  > `localStorage` value, which wins first). The exposure is therefore
  > first-visit-only, on a browser reporting `no-preference`, and it is a
  > **one-click** difference. Recorded here rather than raised as a question
  > because FRD §7 is settled and this follows from it.
- **The winner-row overrides FRD `:433-450` names**: `todo.css:411-427`, two
  theme-forked blocks, `color: #111 !important` at `:419` on `#fef08a`, and
  `color: #fff !important` at `:427` on `#374151`. Two more hard-coded
  `!important` pairs are *not* theme-forked and are broken in one theme by
  construction: `.inProgressClass` `:358-361`, `.blockedClass` `:364-367`.
- **The dead tree.** 51 of the 59 files are byte-identical to
  `web/menuserver/`'s. **40 of those 51 are also referenced by nothing** in
  the live closure (8 HTML stubs, 17 CSS, 12 JS, 3 images; 377,727 bytes).
  The 11 pshelper files that are still live are `css/fontawsme.css`,
  `css/fontawall.css`, `js/jquery.js`, `js/jquery-ui.js`, `js/utils.js`,
  `images/fvw.png`, and the 5 webfonts. The third dead hamburger is
  `base.html:26-32` (`#rightToggle`) + `js/panels.js:20-22` (the click
  listener) calling `toggleRightPanel()` at **`:39-53`**, whose icon-class
  toggle is `:42` (loaded only from `base.html:222`) +
  **`css/panels.css:57-101`** — *not* `responsivenav.css`,
  which contains no hamburger and belongs to a fourth dead menu
  (`js/menuserver.js:266-268` + `js/navcontrols.js:3-6`). `grep -rn
  'base\.html' web/ internal/ scripts/ cmd/` → zero matches repo-wide.
- **"Dead" means unreferenced, not unroutable.**
  `internal/platform/static/static.go:23-30` stats the requested path and
  serves any file that exists, and `internal/todo/build.go:23` mounts the
  whole directory at `/`. So `GET /base.html` returns 200 today. Deleting the
  40 files cannot break the live app; it turns those URLs into the SPA
  index fallback.
- **todo has no toast and no modal abstraction.** `grep -iE
  'toast|snackbar|notify'` over the four live JS files finds only
  `compare.js:280`'s 900 ms row flash. Three bespoke modals, each with its
  own open/close pair and its own `setTimeout(…focus(), 50)` workaround
  (`index.html:94`, `:123`, `:144`). No `alert()`/`confirm()`/`prompt()`.

### 4.5 The sampler's vacuous focus-trap checks

`web/sampler/js/main.ts:137-150` — the `openModal` demo's content is a single
`<p>`:

```ts
run: () => {
  const content = document.createElement("p");
  content.textContent = "Hello from openModal.";
  openModal(content, { title: "openModal" });
},
```

`modal.ts:65-71`'s `getFocusable()` therefore returns `[]` for this demo;
`:81-84` then preventDefaults Tab and parks focus on the panel, and `:119-121`
focuses the panel rather than a control. `docs/sampler-checklist.md:16-19` asks
the verifier to confirm "Tab cycles" and "Shift-Tab wraps back" for **each of
the four dialogs in each of the 8 themes** — for `openModal` that is a
tautology in all 8 sections. The three dialog helpers each build real buttons,
so only the `openModal` row is affected: **8 vacuous boolean pairs.** C1 gives
the demo a focusable control.

Also verified: `web/sampler/index.html:2` is `<html lang="en">` with **no
`data-theme`**, which is correct — `web/shared/css/themes.css:15` makes
`:root` carry dark, so the unstamped default renders dark (Phase-1 ADR-007).

### 4.6 G9 is unenforced

`web/shared/public/fonts/` contains `inter/`, `jetbrains-mono/`,
`ibm-plex-mono/`, `sora/` and a tracked `SHA256SUMS`.
`check-shared-css.mjs`'s `FONT_DIR` is declared at `:28` and used only by
clause 10's woff2 walk (`:389-401`). Nothing asserts the **file-type
allowlist** G9 states, and nothing asserts that `SHA256SUMS` lists every
`.woff2` present. Both are one clause.

### 4.7 The Q8 carve-out's surface

`cmd/server/main.go:326` wraps as
`svc.Gate(module, static.WithShared(hh, cfg.Server.SharedStaticDir))` — the
gate is **outside**, so it decides before `/shared/` is ever diverted.
`internal/platform/auth/gate.go:28` is `//go:embed login.html`; the three
existing unauthenticated carve-outs are `GET /healthz` `:129`,
`GET /api/auth/mode` `:135`, `GET /api/auth/whoami` `:148`, all evaluated
before `protected := hasEntry || module == "admin"` at `:157`. Q8's own line
citations were checked against this tree and all four are correct.

**Phase 2 does not *need* C8.** obsidianoid and todo pages are only reached
with a session, so their `shared.css` link and `shared.mjs` import carry the
cookie and pass step 5. C8 exists because Q8 names Phase 2 as the
implementation phase, and it is sequenced last precisely so that it is
revertible without touching any UI work.

### 4.8 `web/todo/compare.html` — the page v1 assumed away

New in v2. Both reviewers landed on this independently (Architect B1/B5,
Critic M-1) and they were right: v1's §1 declared compare.html out of scope
*for a hamburger*, then Step 6.3 quietly edited it, and its acceptance
criteria never mentioned it. Full inventory, all 105 lines read:

| | | |
|---|---|---|
| `:2` | `<html lang="en" data-theme="dark">` | FOUC insurance, same as index.html |
| `:16-19` | `js/jquery.js`, `js/utils.js`, **`js/theme.js`**, `js/compare.js?v=1` | classic scripts; **`theme.js` is the file C6a deletes** |
| `:20-27` | inline `<script>` | page-local helpers |
| `:41` | `<button class="icon-btn" id="theme-toggle" title="Toggle dark / light mode" onclick="toggleTheme()">` | the inline `onclick` binds to `theme.js:21`'s `window.toggleTheme` |
| `:42` | `<i class="fas fa-moon" id="theme-icon">` | the glyph `theme.js:6-9` swaps |
| — | `menu-toggle`, `id="sidebar"`, `sidebar-backdrop`, `toggleSidebar` | **zero occurrences** |
| — | `js/todo.js`, `js/todo-utils.js` | **not loaded** |

**The joint unsatisfiability in v1, stated plainly.** Three of v1's claims
could not all hold: (1) `theme.js` is deleted at C6; (2) `compare.html` is
out of scope; (3) no page is left calling an undefined global. Deleting
`theme.js` removes `window.toggleTheme`, and `compare.html:41`'s inline
`onclick` is a live reference to it — so leaving compare.html untouched
produces a button that throws `ReferenceError` on every click, on a page that
is reachable in normal use. v2 resolves it by dropping claim (2) in favour of
a precise statement: compare.html is **out of scope for the drawer** and **in
scope for the theme system**, it is edited at **C6a only**, and it is named in
C6a's Touches cell and its own acceptance criterion (B4.12).

**What C6a does to it — three edits, mirroring index.html exactly:**
1. `:18`'s `<script src="js/theme.js">` → the inline pre-paint bootstrap;
2. `:41`'s `onclick="toggleTheme()"` attribute removed (the listener is bound
   in `shell.ts`);
3. a `<script type="module" src="js/shell.js">` tag added after the classic
   scripts.

`:2`'s `data-theme="dark"` and `:42`'s `#theme-icon` are **unchanged** — the
icon is driven by `ThemeManager`'s `onChange`, which writes the same two class
strings `theme.js:8` wrote.

**What it does *not* get, and why that costs nothing.** No `#menu-toggle`, no
drawer, no backdrop — so `shell.ts`'s single `if (document.getElementById("menu-toggle"))`
guard makes C6b's drawer code inert here without a second code path. And it
links **no shared CSS**: its theming comes from `css/todo.css`'s own
`[data-theme]` blocks, and it renders none of the shared components, so
`shared.css` would be dead weight. Only `index.html` links `shared.css` —
written by **§5 Step 6.2** at C6b, positioned before `index.html:8`'s
Google-Fonts group, and asserted by **B4.13**. *(**v5**: this sentence was in
the plan from v1 and the **edit that makes it true was never written down** —
no step added the `<link>` and no criterion checked it, while B4.10 and B4.11
both assume todo renders shared `.ui-menu-*` classes, which is impossible
without it. Found while applying the user's cache-buster ruling; §13(a5)
could not have caught it, because there was no pointer to dangle.)*
*(This is why B7.4's "only `shell.js` is referenced" is scoped to
`index.html`: compare.html legitimately keeps `jquery.js`, `utils.js` and
`compare.js`.)*

---

## §5 — Steps

### Step 1 → C1: make three existing gates real

**1.1 — Give the sampler's `openModal` demo a focusable control.**
`web/sampler/js/main.ts:137-150`: the content element becomes a `<div>`
holding the `<p>` plus one `<button type="button" class="ui-modal-btn">Close</button>` wired to the returned handle's `close()`. The demo's
`source` string is updated to match — it is displayed verbatim in a `<pre>`
(`:184-186`), so a stale string is a lie on the page.

**1.2 —** `docs/sampler-checklist.md` gains a note under its bullet list
recording that the `openModal` row's Tab/Shift-Tab columns were vacuous
before C1 and are meaningful after it.

**1.3 — `check-shared-css.mjs` clause 12: the fonts-tree allowlist (G9).**
A new clause, structured like clause 10's walk:

- Walk `FONT_DIR` (`:28`). Every regular file must match `*.woff2` or be
  named `OFL.txt` or `SHA256SUMS`; anything else fails by name.
- Every `.woff2` found must appear in `SHA256SUMS`, and every `SHA256SUMS`
  entry must exist on disk. Both directions, per G5.
- `SHA256SUMS` itself must exist and be non-empty.

**Where the clause body lands, because Step 1.5's coordinate table depends on
it.** *(New in **v5 FINAL**, Critic "what's missing".)* The new block is
**appended after clause 11** — whose `}` closes at `:424`, with `:425` blank —
and therefore **above** the PASS line at `:426` and **below** every coordinate
Step 1.5's table names (the lowest-lying of which is clause 7's body at
`:300-319`). Consequence, stated so it is not assumed: clause 12's insertion
moves **no line that table cites**, which is why that table's shift is a
uniform **+1** attributable entirely to `T1` gaining an entry. An executor who
instead interleaves clause 12 among the earlier clauses invalidates the table,
which is reason enough to pin the position here rather than leave it to taste.

The clause count moves from 11 to 12 in **two** places, and both are part of
this commit: the PASS line at `:426`
(`check-shared-css: 11 clauses pass over …`) **and** the file's header comment
at `:2` (`// Linter for web/shared/css/ — the 11 clauses of
PLAN-ui-unification-phase1.md`). v1 named only the PASS line. Updating one and
not the other would leave the gate's own documentation contradicting its
output — in the file whose entire purpose is to enforce that facts have one
definition. **B10.2** asserts the PASS line; the header is verified by
`grep -c '11 clauses' scripts/check-shared-css.mjs` returning **0** after the
commit. *(A third mention exists — Step 4's clause-7 at-rule descent, ADR-012
— but that changes a clause's *behaviour*, not their count, so neither string
moves again.)*

**1.4 — `bundle-shape.mjs` gets a `--require` flag, and the `Makefile`
actually passes it.** New in v2, and the third vacuous gate: §4.2.1(a)
records that `--expect-nonempty` is a **no-op** — its only effect is the
`inputs.length === 0` branch at `:54-66`, which is dead while any descriptor
carries `sharedConsumer: true`, and `Makefile:62` does not pass the flag
anyway. So the gate can neither detect a descriptor that *silently stopped*
being a shared consumer nor one that never became one. Two edits:

- **Add `--require=<name>,…`** alongside the existing flag (`:45`'s parse,
  `:26`'s usage line). Semantics: every named descriptor must exist in
  `descriptors` **and** have `sharedConsumer === true`; a name that is absent
  or whose flag is false is a **per-descriptor failure reported by name**.
  This is strictly stronger than `--expect-nonempty`: "the set is non-empty"
  cannot fail while any one member remains, whereas "these *n* are in it"
  fails the moment one leaves. `--expect-nonempty` and its `:54-66` branch
  are **left in place** — unreachable on today's descriptor set but not
  wrong, and deleting a branch is a change this commit does not need to make
  to get the property it is after. The stale header at `:19-20` is corrected
  in the same edit.
- **`Makefile:62` becomes `node scripts/gates/bundle-shape.mjs
  --require=sampler,taskmaster`** — the two descriptors carrying the flag
  today (`descriptors.mjs:77`, `:123`). The list grows at C5 (`+obsidianoid`,
  Step 5.1) and C6a (`+todo`, Step 6.1). This is the edit **G16** exists for:
  a gate not invoked with the flag the plan cites is not the gate the plan
  describes, and v1 cited a flag no invocation passed.

**Negative test, run before the commit closes:** flip `sharedConsumer` to
`false` on `descriptors.mjs:77` in the working tree, run `make gates`, and
confirm it now **fails naming `sampler`** where it previously printed
`PASS — 2 sharedConsumer bundle(s) inspected` and exited 0 both with and
without `--expect-nonempty`. Revert the flip. **BX.8.**

*Guaranteed by (criteria): **B9.1**, **BX.11**.*

**1.5 — `--color-surface-dynamic` becomes the 18th key of T1.** *(New in
**v5**, by user ruling on **Q12**, 2026-09-16, quoted because it is the
authority for every carve below:*

> *"…In order for todo to enjoy the bredth of theming options, that means we
> need color choices to extend each theme in a way that works for todo. I'm in
> favor of doing exactly that: extend the themes so that any theme can be used
> with any module."*

*The user chose **option B** from §9 Q12's table. §4.3's rule-local pair-shift
— v4's "plan of record" under option A — is **superseded**, and **Step 5.3
item 4 is rewritten to a no-op rather than deleted**: its two `app.css`
consumers stop being edits and become facts about what C5 leaves alone. The
number is kept deliberately. P5a (§2) is specified as "after Step 5.3 items
1–4", and §5 Step 5.6 partitions C5's edits around "item 5"; renumbering five
sub-steps to save one line of prose would move four live pointers for no gain,
and §13(a5) would have to re-resolve all of them. The **broader** half of the
mandate (todo
retokenised, any theme usable with any module, todo's picker widened past two)
is **not** taken into Phase 2 and is recorded as program direction in §11
item 2.)*

**What lands, in five files, all already in C1's manifest except the two
shared-CSS paths:**

1. **`web/shared/css/themes.css`** gains one `--color-surface-dynamic`
   declaration in each of its 8 blocks, immediately after
   `--color-surface-3`, and its 10-line header comment (`:1-10`) is corrected
   in place — `8 × 17` → `8 × 18`, authored cells **14 → 17**. Re-derived
   against the tree: the file goes **187 → 195 lines**, the `[data-theme="puma"]`
   block moves from `:167-187` to **`:174-195`**, and the whole-file
   `--color-*` declaration count moves **136 → 144**.
2. **`scripts/check-shared-css.mjs`** — a **two-line** edit: one new entry in
   `T1` (`:31-49` → **`:31-50`**) and the trailing comment on
   `EXPECTED_COLOR_DECLARATIONS`. The constant itself is **derived**
   (`THEME_SELECTORS.length * T1.length`), so no integer is hand-maintained —
   only the `// 136` comment beside it becomes `// 144`. Two citations shift
   by one and are restated here so every later reference is to the post-C1
   file: `THEME_ALLOW` `:96` → **`:97`**, `EXPECTED_COLOR_DECLARATIONS`
   `:98` → **`:99`**.
3. **`web/sampler/js/main.ts`** — `COLOR_TOKENS` gains the same entry, and its
   header comment is corrected. Coordinates, re-derived because v5's first
   draft of this line mis-cited them: the declaration spans **`:15-33`**
   (`const COLOR_TOKENS = [` at `:15`, `] as const;` at `:33`) and its 17
   quoted names are **`:16-32`**; the new `"--color-surface-dynamic"` goes
   immediately after `--color-surface-3` (`:19`), matching `themes.css`'s
   insertion point, and the comment at **`:11`** — "the 17 `--color-*` keys" —
   reads **18**. Insertion *position* is cosmetic (B10.6 compares the two
   rosters with `sort -u`); the count in the comment is not. **This is a
   second definition of T1's roster and v4 did not know it existed**: the
   sampler's specimen page iterates its own array (`:85`), so without this
   edit the 18th token is missing from the one page whose purpose is to show
   every token. It is declared as duplication **5** in §13(b) and asserted by
   the new **B10.6**.
4. **`web/sampler/js/bundle.js`** — the emitted mirror (`:4`), regenerated by
   `npm run build`, not hand-edited.
5. **`web/shared/dist/shared.css`** — regenerated: the `shared-css`
   descriptor (`descriptors.mjs:58-64`) bundles `web/shared/css/index.css`,
   which `@import`s `themes.css`, so the committed sheet changes bytes. The
   artifact **count** does not move (G15): `shared.css` was already one of
   the 15.

**The eight values, and the rule that produced the three with no donor.** Five
are donated **verbatim** from `web/obsidianoid/css/themes.css` — the same
donor of record (T5a) the other five obsidianoid themes already use, so these
five cells need no judgement at all:

| theme | value | source |
|---|---|---|
| `obsidian` | `#2e2e42` | donor `web/obsidianoid/css/themes.css:7` |
| `forest` | `#1f3d26` | donor `:45` |
| `ocean` | `#1e3450` | donor `:83` |
| `ember` | `#402c14` | donor `:121` |
| `rose` | `#3d1e30` | donor `:159` |
| `dark` | `#373c43` | **authored** |
| `light` | `#d3d0cc` | **authored** |
| `puma` | `#182737` | **authored** |

The authoring rule is read *off the donors* rather than invented, so the three
new cells are the same kind of thing as the five given ones:
**`--color-surface-dynamic` is one ladder step beyond `--color-surface-3`, in
the direction that palette's own surface ladder already travels.** Measured
across all five donors, that step is a uniform per-channel lightening of
`--color-surface-3` by **×1.19–1.25** (obsidian ×1.243/1.243/1.245, forest
×1.192/1.196/1.188, ocean ×1.25/1.238/1.25, ember ×1.208/1.189/1.25, rose
×1.196/1.25/1.2) — hue-preserving, not a grey wash. Applied per theme:

- **`dark`** — its surface ladder is *arithmetic*: `#0d1117` → `#161b22` →
  `#21262d` → `#2c3138`, the last two steps both exactly `+0x0b` per channel.
  One more step is **`#373c43`**, which the multiplicative rule independently
  puts at `#363c44` — the two agree to within one unit per channel, so the
  value is over-determined rather than chosen. It sits just above `dark`'s
  `--color-border` `#30363d`, the same relation `ember` and `rose` already
  have.
- **`puma`** — ladder step `#0e1925` → `#13202e` is `+0x05/+0x07/+0x09`; one
  more step is **`#182737`**, and the multiplicative rule gives `#172738`.
  Again within one unit. puma's `--color-border` is translucent
  (`rgba(255, 255, 255, 0.11)`), so there is nothing to collide with.
- **`light`** is the one theme where the rule needs a stated second step, and
  this is the single judgement in the set. `light`'s ladder descends
  (`#ffffff` → `#f0ede8` → `#e6e3df`, step ×0.957/0.958/0.961), so "beyond
  `--color-surface-3`" means *darker*. One step lands on **`#dcd9d6`** — and
  `light`'s `--color-border` is `#dcd9d5`, i.e. the first step **is** the
  border, to within one unit of blue. A highlight token whose value equals the
  border it renders beside is not a usable highlight, so `light` takes a
  second step of the same ratio: **`#d3d0cc`**. Recorded as a judgement, not a
  derivation, exactly as Q1's light scrim alpha was.

  *Why darker is correct on light rather than a defect of the rule.* Both
  consumers are "distinguished from the container" states — the skeleton
  shimmer's lit stop (`app.css:202`) and `#mode-switcher button.active`
  (`:455`). On the seven dark themes "distinguished" reads lighter; on light
  it reads recessed, which is also what every other light-theme selected-tab
  convention does. `light`'s `--color-surface-1` is already `#ffffff`, so
  there is no headroom in the other direction: `+0x0b` upward from
  `--color-surface-3` gives `#f1eeea`, which collides with
  `--color-surface-2` `#f0ede8`. The palette decides this one.

**The user will validate all eight visually** — *"I'll need to visually
validate each one anyway"* (2026-09-16) — so these are values proposed with
their derivation shown, not values asserted to be final. A change to any of
the three authored cells after visual review is a **correction inside C1**, not
a plan revision: it moves no line count, no criterion and no commit boundary.

**The gate edit is carved, and the carve is demonstrated by seeded failure
rather than asserted.** §3's G12 forbade a new token, and G12 as rewritten in
§3 is amended for exactly this one addition. *(**v5 FINAL**, Architect N-2: v5
also claimed here that "§6's ordering forbade editing
`scripts/check-shared-css.mjs` outside C2". That was false — §6's C1 row
already lists `scripts/check-shared-css.mjs`, for Step 1.3's clause 12, and
§6's ordering text carries no such rule. Only `themes.css` needed a carve, and
G12 supplies it. The clause is dropped rather than repaired.)* Because clause 5 checks **set equality** per block *and* a derived
total, the roster edit and the `themes.css` edit are **inseparable** — either
one alone fails the gate, in opposite directions and by name. Run at plan time
against a copy of `scripts/` + `web/shared/` outside the repo (the gate
`chdir`s to its own parent, so a copy is lintable and the tracked tree was
never touched):

```
# 1. baseline, today's tree
$ node scripts/check-shared-css.mjs
check-shared-css: 11 clauses pass over 5 file(s) in web/shared/css/          rc=0

# 2. themes.css has 18 keys, T1 still 17  →  fails as "extra"
$ node scripts/check-shared-css.mjs
web/shared/css/themes.css:15: clause 5: :root, [data-theme="dark"]: 1 key(s) not in T1: --color-surface-dynamic
                                                                              rc=1

# 3. T1 has 18, themes.css still 17  →  fails as "missing"
$ node scripts/check-shared-css.mjs
web/shared/css/themes.css:15: clause 5: :root, [data-theme="dark"]: missing 1 T1 key(s): --color-surface-dynamic
                                                                              rc=1

# 4. both  →  PASS, and the total is 144
$ node scripts/check-shared-css.mjs
check-shared-css: 11 clauses pass over 5 file(s) in web/shared/css/          rc=0
$ grep -c -- '^\s*--color-[a-z0-9-]*:' web/shared/css/themes.css
144
```

**Case 4's banner is a plan-time reading, not a C1 post-condition.** *(**v5
FINAL**, Architect N-1.)* The sandbox carries Step 1.5's two edits alone, so it
still runs **11** clauses. The committed C1 tree prints `check-shared-css: 12
clauses pass over 5 file(s) in web/shared/css/` — Step 1.3 lands clause 12 in
the same commit, and **B10.2** is the criterion that asserts the 12-clause
banner at the C1 boundary. An executor comparing case 4's line against a real C1
run should expect `12`, with the rest of the line byte-identical.

Cases 2 and 3 are the whole justification for landing both halves in one
commit, and they are the reason this is a *carve* and not a *hole*: after C1
the gate constrains 8 × 18 cells instead of 8 × 17, so the vocabulary is more
tightly pinned than before, not less. `gates/token-overlap.mjs` was re-run in
the same sandbox and still prints
`token-overlap: PASS — intersection is exactly --font-mono`, rc 0; only its
informational line moves (**44 → 45** shared names), and no criterion pins it —
taskmaster's sheet declares no `--color-surface-*` name of any kind (`grep -c`
= 0), which is why B1.2 is unaffected.

**Why C1 and not C5, where the token is consumed.** The brief's own default was
C5; C1 is strictly better and the reasons are checkable:

1. **C1's manifest already carries both editable files.** C1 edits
   `scripts/check-shared-css.mjs` (clause 12) *and* `web/sampler/js/main.ts` +
   `bundle.js` (Step 1.1) *and* `docs/sampler-checklist.md`. It gains only
   `web/shared/css/themes.css` and `web/shared/dist/shared.css`. At C5 the
   same edit would pull the shared-CSS gate, the sampler entry point and the
   sampler bundle into a commit that today touches none of them.
2. **B1.1's freeze stays unbroken where it matters.** Carving the freeze at C1
   leaves it absolute across C2…C8 — the seven commits where cascade-order and
   palette defects are the actual risk. Carving it at C5 would open it in the
   single commit whose whole subject is a palette migration, i.e. the one
   place a spurious `themes.css` diff would be least distinguishable from the
   work.
3. **It is the plan's own C1 rule.** "A gate added after the work it should
   have guarded has never been able to fail" (above): the 18th key is
   *guarded* by clause 5 from C1 onward, so C5's adoption is checked by a gate
   that predates it.
4. **It is pixel-inert until C5, verifiably.** The only `var()` consumers of
   the name anywhere in the tree are `web/obsidianoid/css/app.css:202` and
   `:455` (`grep -rn 'var(--color-surface-dynamic)' web/ --include='*.css'` →
   exactly those two), and until C5 obsidianoid links **no** shared CSS
   (`index.html:7-8` are its own two sheets), so both continue to resolve from
   `web/obsidianoid/css/themes.css`. Nothing else in the repo reads the name;
   `components.css` does not consume it in this phase. The one visible change
   at C1 is the sampler's specimen page, which gains an 18th swatch — a page
   whose entire purpose is to show the vocabulary, with its own checklist
   (`docs/sampler-checklist.md`, already in C1's manifest) noting the row.

**What C5 then does with it: nothing.** That is the point of landing it here.
`app.css:202`'s and `:455`'s `var(--color-surface-dynamic)` references are
**not edited by C5 at all** — the name they already use is now a shared name
carrying obsidian's own donated hex — which is what collapses v4's three-site
"declared visual delta" (old **D-3**, ledger row 14, B2.8) into **zero** delta.
See §5 Step 5.3 **item 4** (rewritten to a no-op), the retired **D-3** row in
Step 5.5, and **B2.8** as rewritten.

**The one coordinate shift this step causes, declared once here.** Adding an
entry to `T1` (`scripts/check-shared-css.mjs:31-49` today, `:31-50` after) moves
**every line of that file below `:49` down by exactly one**, and adding one
declaration to each theme block moves `web/shared/css/themes.css`'s later
blocks down by one each. *Every `check-shared-css.mjs` and `themes.css` line
citation in this plan is to **today's tree**, pre-C1* — that is the baseline
§13(a4) part 1 resolves them against, and re-numbering them to post-C1
coordinates would make the audit unrunnable while the plan is still a plan. The
post-C1 values are therefore stated **once, here**, and criteria that must quote
a post-C1 coordinate (only **B1.4** probe 2 does) cite it from this list rather
than restating the arithmetic:

| fact | today | after C1 |
| --- | --- | --- |
| `T1` array | `check-shared-css.mjs:31-49` | `:31-50` |
| `THEME_ALLOW` | `:96` | `:97` |
| `EXPECTED_COLOR_DECLARATIONS` (and its comment) | `:98`, `// 136` | `:99`, `// 144` |
| `EXPECTED_FONT_FACES` | `:99` | `:100` |
| `parseBlocks` / its `:182` at-rule filter | `:141` / `:182` | `:142` / `:183` |
| `parseDeclarations` def / its call | `:187` / `:163` | `:188` / `:164` |
| clause 7 body / its `parseBlocks` call | `:300-319` / `:310` | `:301-320` / `:311` |
| `themes.css` length | 187 lines | 195 lines |
| `[data-theme="puma"]` block | `themes.css:167-187` | `:174-195` |
| the 8 theme selectors | `:15, :38, :61, :82, :103, :124, :145, :167` | `:15, :39, :63, :85, :107, :129, :151, :174` |
| clause 5's expected total | 136 | 144 |

*(Measured in the sandbox, not predicted: `wc -l` 195,
`grep -n 'data-theme="puma"'` → 174, and the four constants read back at
`:97`/`:99`/`:100`. The two `--font-*` lines still close puma's block, now at
`:193-194`.)*

*Guaranteed by (this step): **B1.1**'s C1 carve, **B2.8**, **B10.6**, and
§13(b) duplication 5.*

**Why this is C1 and not later:** all three original items assert properties of
a tree no later commit changes, so they can only ever pass from here on — and a
gate added at the end of a phase has never been able to fail during it.
`--require` in particular must precede C5 and C6a, which are the commits that
*add* names to its list; if it arrived with them, the boundary that introduced
a shared consumer would be the same boundary that introduced its only check.
Step 1.5 joins them on the reasons enumerated above.

**Acceptance (C1):** B1.1, B1.2, B9.1, B10.1, B10.2, **B10.6**, BX.1, BX.4,
BX.6, BX.8, BX.11.

### Step 2 → C2: Toast

**2.1 —** New `web/shared/ts/toast.ts`, lifting
`web/certmachine/js/toast.ts`'s surface verbatim (ADR-010): `ToastTone`
(`success | error | notice`), `ToastHandle { dismiss(): void }`,
`DEFAULT_DURATION_MS` with `error: 0`, the lazily-created singleton stack
carrying one `role="status" aria-live="polite"`, and
`showToast(message, tone?, durationMs?): ToastHandle`. Changes from the donor,
all three mechanical:

- class names `cert-toast*` → `ui-toast*` (G13/clause 7's `^\.ui-` rule);
- the dismiss button's `×` glyph stays a text node via `textContent`
  (donor `:61`), and gains `aria-label="Dismiss"` (donor `:62`) unchanged;
- no colour literal anywhere — tones map to `data-tone` and the CSS selects
  on it.

**2.2 —** `web/shared/css/components.css` gains `.ui-toast-stack`,
`.ui-toast`, `.ui-toast[data-tone="success"|"error"|"notice"]`,
`.ui-toast-text`, `.ui-toast-close` (+ `:hover`, `:focus-visible`). Donor
geometry: `web/certmachine/js/cert.css:916-966`. Colours come from
`--color-surface-1`, `--color-border`, `--color-text`, `--color-success`,
`--color-danger`, `--color-primary`; spacing/radius/shadow from `tokens.css`.
No `!important` (clause 9).

**2.3 — Clause 7 is re-keyed to fail closed, and taught to descend into
at-rules (ADR-012).** Two edits to `check-shared-css.mjs`, both landing here
because C4 adds the phase's first `@media` block to `components.css` and this
clause must be hardened *before* that arrives, not after.

*(a) Re-key.* The clause body `:300-319` selects `components.css` by name at
`:301-302`. It becomes: *every* sheet in `web/shared/css/` except
`tokens.css`, `themes.css`, `fonts.css`, `index.css`. Today that set is
exactly `{components.css}`, so behaviour is unchanged at this boundary —
which is the point: the extension is verified by the existing suite still
passing, and a future sheet is linted without anyone remembering to add it.

*(b) Opt-in at-rule descent.* `parseBlocks()` ends
`return blocks.filter((b) => !b.selector.startsWith("@"))` (`:182`), so
**nothing nested inside an at-rule reaches the selector rule**. Today that is
latent — `grep -c '@' web/shared/css/components.css` = **0** — and C4's
`@media` opens it: a `.sidebar { … }` written inside a media query would pass
clause 7 silently. `parseBlocks` therefore gains a second parameter,
`parseBlocks(text, { intoAtRules: true })`, and **only clause 7 passes it**.

***The descent's post-condition, which v4 left unstated (Architect A-1 ≡ Critic
F-A item 4 — the one place the two iteration-4 reviews converged).*** A nested
block's `line` **must be its absolute line in the file**, not a line relative to
the at-rule body the descent is walking. This is not a nicety: **B1.3**'s C4-leg
part 2 asserts that clause 7's failure line is *greater than* the line of the
enclosing `@media`, and both reviewers, implementing the natural descent
independently, produced body-relative numbers — a selector at absolute `:104`
inside an `@media` opened at `:103` reported as `:2`. That turns B1.3 part 2 red
on a **correct** implementation, at C4, where §6's manifest does not permit
touching `check-shared-css.mjs`; the defect would be unfixable at the boundary
that found it.

The fix is to reuse the idiom the function already has rather than invent one.
`parseBlocks` tracks `line` as it scans, captures `bodyLine = line` at the
moment a depth-1 `{` opens (`:155`), and hands that start line to
`parseDeclarations(bodyBuf, bodyLine)` (`:163`); the declaration parser
(`:187`) then reconstructs each absolute coordinate as
`bodyLine + countNewlines(before) + countNewlines(chunk.slice(0, lead))`
(`:201`). **The descent does the same thing one level up:** when it re-parses an
at-rule's body it passes that body's own start line as an offset, and every
`selector`/`line` pair it returns is expressed in whole-file coordinates. Stated
as an invariant an implementer can check without reading B1.3: *for every block
`parseBlocks` returns at any depth, `text.split("\n")[b.line - 1]` contains
`b.selector`'s first component.* One line of throwaway assertion in the C2
working tree proves it for `components.css` and for probe 2's wrapped
`themes.css`, and it is exactly what **B1.4** probe 1 pins as a committed
criterion (below): the printed failure line is the inner selector's line **in
the file**. That places the pairing correctly — a wrong descent fails at **C2**,
in the commit that writes the descent, instead of at C4 in a commit that may not
touch the gate.

*Guaranteed by (this step): **B1.4** probe 1's in-file line assertion and
**B1.3**'s C4-leg part 2, both of which name this step in return.*

> **Why opt-in and not simply recursive.** `parseBlocks` has exactly three
> callers — `:236` (tokens.css, clause 3), `:252` (themes.css, clauses 4, 5
> and 6) and `:310` (clause 7) — and the first two are **set-equality**
> assertions over a file modelled as flat:
>
> - clause 3 `:237` requires `blocks.length === 1` and `:240` requires that
>   block's selector to be exactly `:root`;
> - clause 4 `:255-266` requires the returned selector roster to **equal** the
>   8 strings in `THEME_SELECTORS`, with an "unexpected depth-1 selector"
>   failure for any extra;
> - clause 5 `:283-285` requires the running `--color-*` total across those
>   blocks to equal `EXPECTED_COLOR_DECLARATIONS` (`:98` today, `8 × 17 = 136`;
>   `8 × 18 = 144` from C1 onward, which is the value in force when C2 lands —
>   Step 1.5's coordinate table is the one definition of both).
>
> Under unconditional descent, one `@media (prefers-reduced-motion)` or
> `@media print` block added to either file — which is what a routine
> accessibility or print refinement looks like — would have its nested
> selector counted as a roster member and fail clause 4 with a message that
> names the wrong problem, or bump clause 3's block count, or double-count
> `--color-*` into clause 5's total. The `:182` filter is precisely what makes
> the flat model true, so removing it globally silently re-specifies three
> clauses. Opt-in changes exactly one caller's behaviour, which is the one we
> intend to change.
>
> **The honest caveat, stated because it is the reason (c) exists:**
> unconditional descent would **pass** on today's tree —
> `grep -c '@' web/shared/css/{tokens,themes}.css` is `0` and `0`, so there is
> nothing for clauses 3–6 to trip over yet. The coupling is latent, not
> visible, which means "the suite still passes" is no evidence either way.
> *(Note `fonts.css` is **not** part of this argument: `parseBlocks` is never
> called on it — its 15 `@font-face` rules are counted inside **clause 10** by
> a raw-text `text.match(/@font-face\b/g)` at `:364`, compared against
> `EXPECTED_FONT_FACES` (`:99`, `15`) at `:365-367` — and clause 6 reads
> `themes.css` only. An earlier draft of this rebuttal cited fonts.css as the
> blocker; that was wrong and is withdrawn. The clause 3/4/5 argument above is
> the real one.)*

*(c) It ships with a negative test — B1.4.* Per the second governing rule
inherited from Phase 1 ("a gate is a command that was run"), and P-III, both
halves are proved by making the gate **fail on purpose** before it is trusted:
write a `@media` block containing a non-`.ui-` selector into `components.css`,
run `node scripts/check-shared-css.mjs`, observe a clause-7 failure naming the
inner selector and a non-zero exit, then revert. The same probe under the
pre-C2 gate exits 0 — which is the evidence that the hole was real. Recorded
as executed evidence in §7, not as a deferred criterion, because a re-key
whose only witness is "the suite still passes" has proved only the re-key's
*(a)* half. **B1.4 probe 1 additionally requires the printed line to be the
inner selector's line in the file**, which is (b)'s post-condition made
falsifiable; **B1.4 probe 2** supplies the descent-off/descent-on contrast on
`themes.css`.

The PASS line at `:426` keeps its clause count of 12 (set by C1); this step
changes clause 7's reach, not the number of clauses.

**2.4 —** `web/shared/ts/index.ts` exports `showToast` + the two types;
`scripts/check-shared-barrel.mjs:25-26` gains them in the same commit
(7 values / 6 types).

*Guaranteed by (criterion): **BX.2**.*

**2.5 —** New `web/shared/ts/toast.test.ts` in the `modal.test.ts` idiom
(hand-rolled stub, no jsdom), added to `scripts/test-web.mjs:39-46` as suite
`shared-toast`. Asserts: `textContent` carries markup verbatim and
unescaped; `error` gets `durationMs === 0` and no timer; `success`/`notice`
get their defaults; one stack element is created for N toasts; the stack
carries `role="status"` and `aria-live="polite"`; `dismiss()` is idempotent;
the barrel exposes `showToast`.

**2.6 — Sampler showcase.** `web/sampler/index.html` gains a "Toasts"
section; `web/sampler/js/main.ts` gains `buildToastDemos()` with one button
per tone plus a "3 at once" stacking button, each with its `source` string,
mirroring `buildModalDemos()`'s row shape (`:167-186`).
`docs/sampler-checklist.md` gains a per-theme toast row (legibility per tone,
error stickiness, dismiss).

**Artifacts re-emitted:** `web/shared/dist/shared.mjs`,
`web/shared/dist/shared.css`, `web/sampler/js/bundle.js`. Count stays 15.

**Acceptance (C2):** B1.1–B1.4, B5.1–B5.7, B10.3, B10.5, BX.2–BX.4, BX.6.

### Step 3 → C3: ThemeManager

**3.1 —** `web/shared/ts/theme.ts` gains `ThemeManager` (ADR-008).
`THEMES` `:8-17` and `setTheme` `:19-21` stay exactly as they are; the class
calls `setTheme` rather than re-implementing the DOM write — one definition
of "how a theme is applied".

Config, per FRD `:230-244`:
`{ module: string; default: string; serverDefault?: () => string | undefined;
storageKey?: () => string; onChange?: (name: string) => void }`.

Behaviour, per FRD `:245-261`:

- resolution order **localStorage → `serverDefault()` → `system` → `default`**.
  **Step 4 is a typed-config floor, not a reachable branch: the `system` step
  resolves under `no-preference` too (§15 row 9), so `default` is unreachable
  at runtime and is asserted by construction, not by resolution;**
- persistence under `ui-theme:<module>` unless `storageKey()` overrides;
- `system` is the **implicit resolution step**, not a selectable name. It
  resolves through `matchMedia('(prefers-color-scheme: dark)')` — `matches`
  → `dark`, everything else (including `no-preference`) → `light` — and
  installs a live `change` listener so an OS flip mid-session is followed.
  **It is not offerable as a choice and deliberately so:** `system` is in
  neither `THEMES` (`web/shared/ts/theme.ts:8-17`, 8 names) nor the
  `themes.list` those names feed (B3.5), and the storage validation two
  paragraphs below rejects it on read-back like any other unknown string. A
  picker therefore cannot show it and storage cannot hold it; what `system`
  governs is the value the order produces when nothing earlier in the order
  answers. *(**v5 FINAL**, Critic CRITICAL 1. This bullet read "`system` is a
  selectable pseudo-theme", which contradicted three other places in this
  document at once — `THEMES`, `themes.list`, and the validation rule — and
  was the premise under B3.4's "while `system` is selected". Deviation-ledger
  row **22** records what the correction costs against FRD `:254-255`, and §11
  item **17** carries the delivery. The Critic's amendment cited FRD `:258`;
  re-derived in the tree, `docs/FRD-ui-unification.md:254-255` is item 4, "*
  `system` is a selectable pseudo-theme (smbedit's tri-state, promoted to
  everyone), live-updating on `matchMedia` change*", while `:258` sits inside
  item 5's swatch-border sentence. The corrected coordinate is published.)*
- `themes.list` exposes the roster for pickers;
- `apply()` is callable pre-paint and is idempotent;
- `set(name)` writes storage, applies, and fires `onChange`.

Every value read from storage is validated against `THEMES` before use — an
unknown stored string falls through to the next resolution step rather than
stamping a nonexistent theme.

**3.2 — The theme-picker CSS** lands in `components.css`:
`.ui-theme-picker`, `.ui-theme-btn` (+ `.is-active`, `:hover`,
`:focus-visible`), `.ui-theme-swatch`. Donor:
`web/obsidianoid/css/app.css:787-812`. **The hard-coded
`rgba(255,255,255,0.15)` swatch border at `:810` becomes
`var(--color-border)`** (FRD `:257-261`).

**The swatch fill — decided, not flagged (ADR-015, §0 Decision 4).** v1 left
this open and recommended the one option that fails on three counts; both
reviewers rejected it, and the Architect's counter-proposal fails too. The
mechanism is:

```css
/* components.css — the entire addition for per-theme swatch fills. */
.ui-theme-swatch { background: var(--color-primary); }
```

```ts
// the picker, per theme — no colour value appears in TypeScript either.
const sw = document.createElement("span");
sw.className = "ui-theme-swatch";
sw.dataset.theme = name;          // <- this is what makes the fill differ
```

It works because `web/shared/css/themes.css` keys all 8 palettes on **bare
attribute selectors** (`[data-theme="light"]` at `:38`, `obsidian` `:61`,
`forest` `:82`, `ocean` `:103`, `ember` `:124`, `rose` `:145`, `puma` `:167`,
and `:root, [data-theme="dark"]` `:15`) rather than on `:root[data-theme=…]`.
Each block therefore applies to *any* element carrying the attribute and sets
**that element's own** `--color-primary`, which its own `background` resolves.
Eight swatches carrying eight different `data-theme` values render eight
different fills **at the same instant, in one open picker** — which is what
B4.2 asserts, and what v1's and the Architect's proposals could not deliver.
All 8 `--color-primary` values are verified distinct (`#7c3aed`, `#01696f`,
`#7c6af7`, `#4dbb6e`, `#5b9cf6`, `#f0a04a`, `#e05c7a`, `#3fbf9c`).

Gate legality, stated against the code rather than by assertion.
`check-shared-css.mjs` clause 7 applies **two independent** rules to
`components.css`, and v1's analysis addressed only the second:
- the **selector** rule, `:310-316`, rejects any depth-1 selector not matching
  `UI_SELECTOR = /^\.ui-[a-z0-9-]+/` (`:102`) — `.ui-theme-swatch` matches,
  and there is **one** such selector, not eight;
- the **colour-literal** rule, `:304-308`, keyed on `COLOUR_LITERAL` (`:101`)
  — `var(--color-primary)` is not a literal, and no hex, `rgb()` or `hsl()`
  enters the file.

No inline `style.background` is written at all, so the G14 note about the
donor's `innerHTML` at `app.ts:438` still holds and the picker additionally
stops carrying colour values in JavaScript. **Declared consequence:** because
a theme block sets *every* colour token on the stamped element, the swatch's
own `border: 1px solid var(--color-border)` also follows the **previewed**
theme — a deliberate micro-preview, which is what makes the border legible on
light and dark swatches alike (FRD `:257-261`'s actual goal). **Ledger row
15 / D-15.** *(v2 pointed this at "ledger row 13", which is obsidianoid's
typography change. Corrected in v3 — Critic minor.)*

**3.3 —** Barrel + allowlist gain `ThemeManager` and the type
`ThemeManagerOptions` — **8 values / 7 types**. *(v1 said 8/8, counting a
`ThemeName` export that existed only to type option 4B's reshaped `THEMES`.
ADR-015 keeps `THEMES` as `readonly string[]`, so `ThemeName` is not
exported and `web/sampler/js/main.ts:72`'s `option.value = theme` is
untouched.)*

*Guaranteed by (criterion): **BX.2**.*

**3.4 —** `web/shared/ts/theme.test.ts`, added to `test-web.mjs` as
`shared-theme`. Asserts the full resolution order with a stubbed
`localStorage`/`matchMedia`/`document.documentElement`; that an unknown
stored name is rejected; that `storageKey()` overrides the default key; that
`onChange` fires on `set` and not on initial `apply`; that the `matchMedia`
`change` listener re-applies **only while the resolution is still reaching
the `system` step** — i.e. with storage empty and `serverDefault()` returning
`undefined` — and is ignored once storage or the server answers. *(**v5
FINAL**, Critic CRITICAL 1: this clause read "only while `system` is
selected", a state the product cannot be in — `system` is un-selectable, per
Step 3.1. The *test* is unchanged in substance; what changes is the condition
it names, from a selection that cannot exist to the resolution state that
can. **B3.4** is retitled to match.)*

**3.5 — Sampler showcase.** `web/sampler/index.html:15`'s `#theme-select` is
re-backed by a `ThemeManager` instance (`module: "sampler"`), and
`buildThemeSelect()` (`main.ts:66-79`) reads `themes.list` instead of
`THEMES` directly. A "Theme picker" section shows the shared
`.ui-theme-picker` swatch grid — the same widget `HamburgerMenu` will mount
at C4. The sampler thereby also becomes the first module with persisted
theme selection, which is a visible behaviour change in a gallery with no
users (sanctioned delta).

  **A second sanctioned delta at C3, stated because it is the one place
  `system` becomes observable in this phase.** *(New in **v5 FINAL**, Critic
  "what's missing".)* `web/sampler/index.html:2` is `<html lang="en">` — it
  carries **no** `data-theme` attribute, so today the page renders `dark`
  unconditionally from `web/shared/css/themes.css:15`'s `:root, [data-theme="dark"]`
  half. Backing `#theme-select` with a `ThemeManager` gives the sampler that
  manager's resolution order, and on a fresh profile the order falls through
  storage and an absent `serverDefault()` to the **`system`** step — so from
  C3 the sampler's first render is **OS-dependent**: `light` under
  `prefers-color-scheme: light` or `no-preference`, `dark` only when the OS
  asks for dark. This is deliberate and it is the only module where the
  `system` step reaches a pixel in Phase 2 (obsidianoid's `serverDefault()`
  always answers, and todo's picker is pinned to `{dark, light}` with a stored
  key). It is a sanctioned delta on the same grounds as the persistence
  change — a specimen gallery with no users — and it is recorded rather than
  left to surface as an unexplained screenshot: B10.5's `:root` premise is
  about the **stylesheet**, which does not move, while what moves is which
  attribute the page stamps on itself.

**Acceptance (C3):** B1.1–B1.3, B3.1–B3.7, B3.10, B10.3, B10.5, BX.2–BX.4,
BX.6. Artifact count stays 15. *(v3: B3.8 moves to C6a — it asserts todo's
pre-paint blocks, which do not exist until C6a — and B3.10, added in v2, is
scheduled here for the first time.)*

### Step 4 → C4: HamburgerMenu

**4.1 —** New `web/shared/ts/menu.ts` exporting `class HamburgerMenu`
(ADR-009) and the types `HamburgerMenuOptions`, `MenuItem`.

Item kinds, per FRD `:289-293`: `action` (`{id, label, icon?, onSelect,
when?}`), `link` (`{id, label, href, when?}`), `separator`, `section`
(header), and **`render`** (`{id, render: (host: HTMLElement) => void}`).
`when()` is re-evaluated on every open, so a guarded item appears and
disappears without `addItem`/`removeItem` churn.

Options: `{ title?, items, themePicker?: boolean, themes?: ThemeManager,
onOpen?, onClose?, mountTrigger?: HTMLElement }`. Methods: `open()`,
`close()`, `toggle()`, `addItem()`, `removeItem(id)`, `updateItem(id, patch)`,
`destroy()`.

Chrome, modelled on todo's drawer (FRD `:265-269` names it "the most complete
implementation in the repo" — structurally; §4.4 records that it has no a11y
at all, so the three a11y features below are **net-new, not ported**):

- trigger `<button class="ui-menu-trigger">` with a private inline `fa-bars`
  equivalent SVG, `aria-expanded`, `aria-controls`, `aria-haspopup="true"`;
- `<div class="ui-menu-backdrop">` using `var(--overlay-scrim)`
  (`tokens.css:42`, with light's override already in `themes.css`);
- `<aside class="ui-menu-drawer" role="dialog" aria-modal="true"
  aria-label=…>` sliding on `transform` with `var(--transition)`;
- Escape closes; focus moves to the first focusable item on open and returns
  to the trigger on close; Tab is trapped inside the drawer while open —
  reusing `modal.ts:65-96`'s proven predicate. **`getFocusable` is
  extracted from `modal.ts` into a private shared helper module rather than
  copied** (rule 1). `modal.ts`'s public surface is unchanged, and the helper
  is *not* exported from the barrel.
- `prefers-reduced-motion` short-circuits the transition.

**4.2 —** `components.css` gains `.ui-menu-trigger`, `.ui-menu-backdrop`,
`.ui-menu-drawer` (+ `.is-open`), `.ui-menu-section`, `.ui-menu-label`,
`.ui-menu-item`, `.ui-menu-link`, `.ui-menu-separator`, `.ui-menu-slot`, and
a `@media (max-width: 640px)` width rule mirroring `todo.css:617-619`.

**4.3 —** Barrel + allowlist gain `HamburgerMenu`, `HamburgerMenuOptions`,
`MenuItem` — **9 values / 9 types**, the Phase-2 end state. *(v1 said 9/10; the
difference is ADR-015 dropping `ThemeName` at C3, not a change here.)*

*Guaranteed by (criterion): **BX.2**.*

**4.4 —** `web/shared/ts/menu.test.ts` → `test-web.mjs` suite `shared-menu`.
Asserts: `aria-expanded` flips on open/close; `aria-controls` names the
drawer's real `id`; a `render` slot receives a host element and its child
survives a close/open cycle; `when() === false` omits the item and
`when()` is re-evaluated per open; `separator`/`section` emit the right
classes; every label arrives via `textContent`; `removeItem`/`updateItem`
resolve by `id`; `destroy()` removes every listener it added.

**4.5 — Sampler showcase.** A `HamburgerMenu` is mounted in
`web/sampler/index.html`'s header with: two action items, one link item, a
separator, a section header, one `render` slot (a `<select>` proving a
verbatim mount), an item guarded by `when()` toggled from a page button, and
`themePicker: true` bound to C3's instance. `docs/sampler-checklist.md` gains
a per-theme drawer row (open/close, Escape, focus return, Tab trapped,
backdrop legibility, slot control usable).

**Acceptance (C4):** B1.1–B1.3, B4.1–B4.9, B10.3, B10.4, B10.5, BX.2–BX.4,
BX.6, BX.7. Artifact count stays 15. *(v3: **B4.2** is included here, not
only at C5/C6b — v2's label already read `C4/C5/C6b`, and C4 is the commit
that builds the widget whose preservation rows it checks.)*

### Step 5 → C5: obsidianoid adoption + the coupled `dark` → `obsidian` rename

The single largest commit, and the one whose two halves ship together on
**Q4's coupling *principle*** — not on Q4's subject matter, which is
`tsconfig` globs (§0's "Authority split": the rename *decision* is FRD
`:191`; Q4 supplies only "doing two things in the same commit and avoiding
risk"). **Rename and
adoption in one commit** because the rename's correctness depends on the
shared matrix being the source of truth in the same tree state: if the two
were split, the intermediate commit either has `[data-theme="obsidian"]`
stamped with no block defining it (a blank page) or a renamed local block
that the next commit deletes (two edits to one fact).

**5.0 — The parity pre-check, run and recorded BEFORE any edit in this
commit.** New in v2. §8.5's Scenario 1 (the pre-mortem's "the five themes were
not actually identical" failure) had no scheduled check in v1 — it described
a risk and then trusted a claim. It is now a step with a criterion (**B2.7**),
and it is the first thing C5 does, because every later step in C5 is
predicated on its result:

```
node -e '…'   # full-property comparison, not a spot-check:
              # for each of the 5 donor blocks in web/obsidianoid/css/themes.css,
              # apply the 4 renames, map --color-surface-dynamic to itself, and
              # diff EVERY declaration against its shared counterpart
              # (dark→obsidian, forest, ocean, ember, rose).
              # Report unmapped names and shared-only names too, so that a
              # name falling out of the comparison cannot read as a pass.
```

Expected output, and the plan's claim of record: **85 comparisons, 0
mismatches, 0 unmapped names in all five blocks**, with `--color-primary-fg`
reported as the one shared-only key per block (authored by Step 1.4, no donor
exists). This was executed at plan time and is reproduced in §4.3's parity
table — so B2.7 is *executed evidence*, not a deferred criterion. *(v5: the
comparison widened from 16 names to 17 per block, i.e. 80 → 85, because the
user's Q12 ruling gives `--color-surface-dynamic` a shared counterpart to be
compared **against** instead of a carve-out to be set aside. The "0" did not
change; the number of things it is a zero **about** did. Reporting unmapped and
shared-only names is new in v5 for the same reason the widening was possible at
all — v2–v4's script could have dropped a name silently and still printed 0.)*
It is
re-run inside C5 because the tree can move between planning and execution,
and because a "spot-check `--color-bg` and `--color-primary`" is precisely the
shape of check that Scenario 1 describes slipping through: v1 verified 2 of 17
keys in 1 of 5 blocks and generalized.

If it ever returns non-zero, C5 **stops** — the rename becomes a
re-authoring, which is a different commit with different acceptance criteria,
and the plan would need revision rather than improvisation.

**5.1 — Descriptor and gate plumbing.**
`scripts/descriptors.mjs:79-86` becomes `mode: "transpile"`, **`bundle:
true`**, `format: "esm"`, `target: "es2020"`, `sharedConsumer: true`, entries
and `out` unchanged (ADR-011). This satisfies `build-web.mjs:182-190`'s
`format === "esm"` assertion and selects the `onResolve` plugin `:78-88`;
`list-artifacts.mjs:38-39` still derives the same two paths, so
`EXPECTED_ARTIFACT_COUNT` does **not** move.

New `scripts/artifact-paths.mjs` exporting `artifactPaths(descriptor)` — the
derivation currently private to `list-artifacts.mjs`. **The extraction range
is `:25-27` + `:38-45`**, not v1's `:31-46`: the mode dispatch at `:38-45`
calls `jsName(entry)`, which is defined separately at `:25-27`
(`return path.basename(entry).replace(/\.tsx?$/, ".js");` at `:26`), so
lifting the dispatch alone produces a module with an undefined reference.
*(Architect M1.)* Both `list-artifacts.mjs` and `gates/bundle-shape.mjs`
import it; `bundle-shape.mjs:68-94` iterates `artifactPaths(d)` instead of
reading `d.out`, fixing the `EISDIR` throw (§4.2.1(b)) and extending A9.1/A9.2
to both of obsidianoid's outputs. `list-artifacts.mjs` keeps its
absolute-path assertion (`:34-36`), which is why the helper must also stay
relative-path-only.

> **The PASS line is part of this edit, and v2 missed it (Architect N5).**
> `bundle-shape.mjs:101` is
> ``process.stdout.write(`bundle-shape: PASS — ${inputs.length} sharedConsumer bundle(s) inspected\n`)``
> — `inputs.length` is the **descriptor** count (`:52`
> `descriptors.filter((d) => d.sharedConsumer === true)`), and `:101` is
> *outside* the `:68-94` range this step rewrites. Through C4 descriptors and
> artifacts coincide, so the two readings are indistinguishable; at C5 they
> diverge, because obsidianoid is **one** descriptor with **two** outputs. Left
> alone, the gate would iterate 4 artifacts and then print `3`, and B9.1's
> "Outputs inspected: 4" row would be describing a number the gate never
> emits. So this step also changes `:101` to count the artifacts actually
> inspected, and B9.1 pins the resulting string per boundary. Verified by
> running the gate in this tree: it prints exactly
> `bundle-shape: PASS — 2 sharedConsumer bundle(s) inspected`, rc 0.
> *(**v4 — the line is `:101`, not `:100`, and both reviewers caught it**
> (Architect A4 / Critic F-3). The file is exactly 101 lines: `:96-99` is the
> failure block, **`:100` is blank**, and `:101` is the PASS write. Six sites
> carried `:100`; all six are corrected. The argument is unaffected — `:101`
> is still outside `:68-94` — but the defect landed on the very line the
> executor is sent to edit, in the fix for the finding that was **about** a
> mis-cited line. §13(a4)'s new diff-scoped half exists because of this.)*

`Makefile:62` gains `obsidianoid` to `--require=` in this same commit (G16) —
without it, obsidianoid entering the `sharedConsumer` set is not actually
*asserted* by anything, only relied upon. This is the edit that makes B9.1 a
real criterion for this boundary rather than a restatement of the gate's
default behaviour.

**5.2 — TypeScript.**

- `js/app.ts:46-52`'s local `THEMES` is **deleted**; the picker is the shared
  `.ui-theme-picker`, fed by `themes.list`. Surviving definition:
  `web/shared/ts/theme.ts:8-17`.
- `js/app.ts:425` `themeStorageKey()` and `:427-431` `setTheme()` and
  `:433-442` `buildThemePanel()` and `:444-447` `toggleThemePanel()` are
  **deleted**, replaced by one `ThemeManager` **constructed at `:503`**, in
  the `/* ─── Init ─── */` block, where `buildThemePanel()` is called today:

  ```ts
  const themes = new ThemeManager({
    module: 'obsidianoid',
    default: 'obsidian',
    storageKey: () => `obsidianoid-theme-${state.activeVault}`,
    serverDefault: () => state.vaults[state.activeVault]?.theme,
  });
  ```

  This also removes the inline duplicate of the key template at `:493-494` —
  the template string was stated twice and is now stated once. What replaces
  that call site is the `reresolve()` bullet below.

  > ***Where the instance lives, stated once (v5, Critic F-D item 7).***
  > Through v4 the plan described the `ThemeManager` **config** in four places
  > and its **construction** in none: `new ThemeManager` appeared **zero**
  > times across the whole document, so an executor had a config object with
  > no constructor call and no binding to call `reresolve()` on. The binding is
  > named here — `themes`, module scope, `js/app.ts:503` — because three later
  > bullets depend on it existing: `reresolve()` in `switchVault`, the
  > `HamburgerMenu`'s `themePicker: true` (which reads `themes.list`), and
  > `js/app.ts:46-52`'s deleted local `THEMES`, whose replacement is
  > `themes.list`. Module scope, not inside a function, for the same reason:
  > `switchVault` (`:484`) and the hamburger wiring are both module-scope and
  > must see the same instance. **`default: 'obsidian'`, not `'dark'`** — this
  > is where the C5 rename lands the fallback that `js/app.ts`'s **two**
  > `|| 'dark'` tails used to express (`:479` and `:494`, both deleted with
  > their statements), and `default` is a **required** config key per §5 Step
  > 3.1, so omitting it is a compile error rather than a silent shared-`dark`
  > fallback. *(**v5 FINAL**, Critic minor 14: v5 called this "the last of the
  > **three** `|| 'dark'` tails in the file" and then named two.
  > `grep -n "|| 'dark'" web/obsidianoid/js/app.ts` returns exactly those
  > two — the construction site is where the fallback *moves to*, not a third
  > instance of it, so counting it as one made the sentence contradict its own
  > parenthesis.)*
  >
  > *Guaranteed by (criteria): **B2.10** asserts `new ThemeManager` appears
  > exactly once and that no reference to the four deleted functions survives;
  > **B2.11** asserts the `serverDefault()` path at runtime and
  > `default: 'obsidian'` by construction (its part 3 is a grep over this
  > site — **v5 FINAL**, Critic finding 1); **B2.9** covers the
  > `declare global` half of the same step.*

- **The three surviving call sites of the four deleted functions, and what
  each becomes (v5, Critic F-B items 1–3 — without this, C5 does not
  compile).** Deleting four functions leaves seven references in `app.ts`.
  Four die with their own definitions or are claimed by other bullets:
  `:430` (`themeStorageKey()` inside `setTheme`) and `:439`
  (`setTheme(t.name)` inside `buildThemePanel`) are **internal** to deleted
  bodies and vanish with them; `:449` (`toggleThemePanel()`) is claimed by the
  `HamburgerMenu` bullet below; `:494` (`setTheme(…)` in `switchVault`) is
  claimed by the `reresolve()` bullet below. **Three are not claimed anywhere
  in v4**, and each would be a `TS2304: Cannot find name` at `tsc --noEmit`:

  | Site | Today | After |
  |---|---|---|
  | `js/app.ts:478` | `const stored = localStorage.getItem(themeStorageKey());` | **deleted** |
  | `js/app.ts:479` | `setTheme(stored \|\| state.vaults[0].theme \|\| 'dark', false);` | `themes.reresolve();` |
  | `js/app.ts:503` | `buildThemePanel(); btnAutoSave.classList.add('active');` | the `new ThemeManager({…})` above — **only the first statement** |

  `:503` holds **two statements on one line**. Only `buildThemePanel();` is
  replaced; `btnAutoSave.classList.add('active');` is unrelated to theming and
  must survive verbatim. Called out because a line-granular edit here silently
  drops the autosave button's initial active state — a defect with no gate: it
  compiles, no criterion reads that class, and it shows only as a wrong-looking
  toggle in the browser pass.

  `:478-479` sit inside `fetchVaults()` (`:465`), guarded by
  `if (state.vaults.length > 0)` at `:477`. They are the **initial per-vault
  resolution**: the first moment `state.vaults` is populated and therefore the
  first moment `serverDefault()` can return anything. Both lines collapse to
  one, and the guard can stay or go — `reresolve()` is safe on an empty roster
  because `serverDefault()` returns `undefined` and resolution falls through.

  > ***`reresolve()`, not `set()` — and this is not a style preference.***
  > `set(name)` "writes storage, applies, and fires `onChange`" (§5 Step 3.1).
  > The line it would replace is
  > `setTheme(stored || state.vaults[0].theme || 'dark', **false**)` — the
  > `false` is `persist`, and it is `false` deliberately. Calling `set()` here
  > would take whatever the **server** sent for this vault and write it into
  > `localStorage` under `obsidianoid-theme-<i>` on the very first load, which
  > (a) makes a server-side default indistinguishable from a user's explicit
  > choice for ever after, so a later change to the vault's configured theme
  > would never reach that browser again, and (b) inverts the resolution order
  > the class exists to own — position 2 would permanently occupy position 1.
  > `reresolve()` "re-runs the full resolution order against the *current*
  > closure values and applies the result, **without writing storage**", which
  > is exactly `persist = false`. The same argument is why `switchVault` calls
  > `reresolve()` and not `set()`; this bullet and that one are the same fact
  > applied at the two moments `state.activeVault`'s meaning changes — once
  > when the roster first arrives, once on every switch.
  >
  > One behaviour difference is **intended** and is why `:479` is an
  > improvement rather than a translation: today's line reads
  > `state.vaults[**0**].theme` while its key came from
  > `themeStorageKey()` → `state.activeVault`. Those agree only because
  > `activeVault` is 0 at boot. `serverDefault: () => state.vaults[state.activeVault]?.theme`
  > reads the same index as the key on both paths, so the latent mismatch is
  > removed rather than carried across.
  >
  > **And the third path is the one that *does* write.** Two of the three
  > callers of the old `setTheme` passed `persist = false`
  > (`:478-479`'s boot resolution, `:494`'s vault switch) and one passed the
  > default `true` — `:439`, the swatch's own
  > `btn.addEventListener('click', () => setTheme(t.name))` inside
  > `buildThemePanel()`. That third path is not translated by this bullet: it
  > dies with the panel and is replaced by the shared
  > `.ui-theme-picker`, whose click handler calls **`set()`** — the writing
  > path — because a user choosing a theme from the menu is exactly the event
  > that should occupy resolution position 1. So after C5 the three paths are
  > `set()` on user choice, `reresolve()` on boot, `reresolve()` on vault
  > switch, and the `persist` parameter disappears from obsidianoid's code
  > entirely: the distinction it encoded is now carried by *which method is
  > called*, which is the whole reason `ThemeManager` has two. *(Critic's
  > unscored open question for iteration 4 — "does the shared picker persist
  > while `reresolve()` deliberately does not write?" Answer: yes, and the
  > contrast is stated here rather than left to be inferred from two adjacent
  > methods.)*
- `js/app.ts:54-61` `showToast` is **deleted**; both files import `showToast`
  from `@shared`. The `declare function showToast` at `threads.ts:15` goes
  with it, and the cross-file global contract dissolves.
  `css/app.css:351-371`'s `#toast` rules are deleted (the shared stack
  creates its own live region and styles it from `components.css`); the
  markup and its handle are deleted by the orphan-cleanup bullet below, which
  owns that fact for all three orphaned ids at once.
  Tone mapping: `'success'` → `success`, `'error'` → `error`. **The
  16 call sites keep their two-argument shape**, so the tone becomes sticky
  for errors — a deliberate behaviour change, sanctioned delta.
- `js/app.ts:449-455` (`#btn-hamburger` wiring) becomes a `HamburgerMenu`
  with `themePicker: true`, mounted on the existing `index.html:54-56`
  button via `mountTrigger`. Its only content is the theme picker, per FRD
  `:301`.
- **`switchVault` needs a re-resolution entry point — `ThemeManager` gains
  `reresolve()` (Architect M5).** v1 deleted `setTheme()` and
  `themeStorageKey()` and gave `ThemeManager` a `storageKey` closure over
  `state.activeVault`, but left `switchVault`'s `:493-494`
  (`localStorage.getItem(\`obsidianoid-theme-${idx}\`)` then
  `setTheme(storedTheme || state.vaults[idx]?.theme || 'dark', false)`) with
  nothing to call. A `storageKey` **closure** does not re-run itself when the
  value it closes over changes, so without an entry point switching vaults
  would silently keep the previous vault's theme — a regression in the one
  behaviour FRD `:253` explicitly says to preserve ("obsidianoid keeps
  per-vault persistence — behavior preserved"). `ThemeManager` therefore
  exposes **`reresolve(): void`** — re-runs the full resolution order
  (localStorage → `serverDefault()` → `system` → `default`) against the
  *current* closure values and applies the result, without writing storage.
  `switchVault:493-494` collapses to a single `themes.reresolve()` call after
  `state.activeVault = idx`. This is a **new public method on the class** and
  is therefore part of Step 3.1's API and asserted by **B3.10**, not an
  obsidianoid-local helper — it lands at C3 so C5 only calls it.
  *(Note it is not a new barrel **export**: it is a method, so the 8/7 counts
  at C3 are unaffected.)*
- **Orphaned markup and its lying handles are removed together (Architect
  M4).** Deleting `#toast` (`index.html:102`) and the theme popover
  (`index.html:60-64`: the comment at `:60`, `#theme-panel` `:61`, its
  `<p>Theme</p>` `:62`, `#theme-options` `:63`, the close at `:64`) leaves
  three module-level `const`s resolving to `null`:
  `toastEl` `:31`, `themePanel` `:41`, `themeOptions` `:42`. Each is written
  with a **non-null assertion** (`document.getElementById('toast')!`), so
  `tsc --noEmit` stays green and the failure surfaces only as a runtime
  `TypeError` at the first property access. The `!` defeats the only
  compile-time protection available, which is exactly why the markup deletion
  and the handle deletion must be **one edit**: all three `const`s are deleted
  in the same commit as their markup. `btnHamburger` `:40` **survives** — the
  button at `index.html:54-56` is kept and handed to `HamburgerMenu` via
  `mountTrigger`. Asserted by **BX.9**, which is written cross-cutting rather
  than obsidianoid-local because C6a creates the same hazard in todo: no
  `getElementById`/`querySelector('#…')` in any adopter's TypeScript may name
  an id absent from the HTML that loads it.
- **Both `<script>` tags gain `type="module"` — `index.html:131`
  (`/js/threads.js`) and `:132` (`/js/app.js`).** *(New as a **prescription**
  in v5 — Critic F-C. The edit was named in ADR-011's Consequences from v1
  onward and in no step, while ledger row 16 and BX.10 asserted the opposite;
  ADR-011's v5 amendment carries the reconciliation.)* This is the edit
  without which C5 does not boot at all, and it is **forced**, not chosen:

  1. flipping obsidianoid's descriptor to `sharedConsumer: true` (Step 5.1)
     makes `format: "esm"` mandatory — `scripts/build-web.mjs:183-186` throws
     `sharedConsumer:true requires format "esm"` otherwise;
  2. driver **rule 10 externalizes `@shared` rather than inlining it** —
     `build-web.mjs:82-85` returns
     `{ path: "/shared/dist/shared.mjs", external: true }` under the header
     "rule 10: `@shared` is externalized, never inlined" — so `bundle: true`
     does *not* absorb the barrel;
  3. the emitted `js/app.js` and `js/threads.js` therefore keep a literal
     top-level `import { … } from "/shared/dist/shared.mjs"`, exactly as
     `web/taskmaster/js/bundle.js:2` does today;
  4. a classic `<script src>` cannot parse a top-level `import` — it is
     `SyntaxError: Cannot use import statement outside a module`, a blank page
     rather than a degraded one.

  The repo's convention already matches this exactly, on both sides: the two
  `format: "esm"` consumers carry module tags (`web/sampler/index.html:36`,
  `web/taskmaster/index.html:13`) and the two `format: "iife"` bundles carry
  classic ones (`web/multissh/index.html:11`,
  `web/certmachine/index.html:11`).

  **Order within the commit matters and is fixed by P5a, not by taste.** These
  two tag edits are part of Step 5.2 and therefore land **after** the P5a
  capture (§2, P5a), together with the TypeScript. That is the safe order: the
  tags and the artifacts they load change in one working-tree sub-step, so
  there is no intermediate tree in which ESM artifacts are loaded classically.

  > *Guaranteed by (criterion): **B2.12**.*
- **`window.ThreadsView`'s *runtime* behaviour is untouched, but its
  *declaration* must be rewritten in this same commit (Architect N1, v3).**
  At runtime nothing changes: `threads.js` executes before `app.js` under
  `type="module"` document order, exactly as today (§4.3, R6). At compile
  time the ESM conversion breaks it. `threads.ts` has **zero** top-level
  `import`/`export` today, so its `interface Window { ThreadsView:
  ThreadsViewAPI }` (`:17-20`) is a *global* declaration merge. Adding
  `import { showToast } from "@shared"` makes the file a module, the
  interface becomes module-local, and the assignment at `threads.ts:22` fails
  with **TS2339 — `Property 'ThreadsView' does not exist on type 'Window &
  typeof globalThis'`**. So this step includes a **third sub-edit** to
  `threads.ts`, alongside deleting `declare function showToast` (`:15`) and
  adding the import:

  ```ts
  // replaces threads.ts:17-20
  declare global {
    interface Window {
      ThreadsView: ThreadsViewAPI;
    }
  }
  ```

  Both halves were verified with `npx tsc --noEmit --strict` on a reduced
  probe: the TS2339 reproduces on import-plus-bare-`interface Window`, and
  vanishes with the `declare global` wrapper. The wrapper is **illegal until
  the import lands** — in script form `tsc` rejects it with **TS2669**
  ("*Augmentations for the global scope can only be directly nested in
  external modules or ambient module declarations*") — so the two edits are
  one atomic change and cannot be split across commits. `app.ts:3`'s
  `declare const ThreadsView` is unaffected (it is a module-local ambient
  declaration and `app.ts` already imports nothing from `threads.ts`).
  Asserted by **B2.9**, a `npm run typecheck` criterion at C5, because no
  other C5 criterion would catch this: the `make gates` scripts do not run
  `tsc`, and `make check`'s typecheck step lives in `test-web`
  (`Makefile:66-69`).

**5.3 — CSS. Items 1–4 are an *ordered* sub-step, not a bag of bullets.**
*(v4 — Architect A1, Critic F-6.)* Items 1–4 below, together with Step 5.4's
first bullet, are **one working-tree sub-step**: the tree between any two of
them does not render, so no pixel capture and no review may be taken inside
the group. Item 5 is order-independent and may land before or after. §5 Step 5.6
states the capture point that depends on this; the two must not drift.

1. **The shared link *replaces* the `css/themes.css` link in place, and this
   pins the cascade order rather than leaving it to be inferred.**
   `web/obsidianoid/index.html:7` is `<link rel="stylesheet"
   href="/css/themes.css" />` and `:8` is `<link rel="stylesheet"
   href="/css/app.css" />`. Item 1 rewrites `:7` to
   `<link rel="stylesheet" href="/shared/dist/shared.css" />` and leaves `:8`
   untouched (Q7 closed: adopt via `<link>`). So the shared sheet is **before
   `css/app.css`** — which Step 5.3a item 1's `--sidebar-width` cascade
   argument depends on — and it is **in the slot the local `themes.css`
   vacated**, which is the fact v3 left unstated and which matters for
   exactly one reason: `web/obsidianoid/css/themes.css:2` and
   `web/shared/css/themes.css:15` are **both** `:root, [data-theme="dark"] {`,
   the same selector list at the same specificity, so while both sheets are
   linked **link order alone decides all 12 colliding token values** and the
   page silently recolours. Replacing in place means the two are never linked
   together in the first place, which is why this is a replacement and not an
   addition. *(The 12 names and the 12 value pairs are §4.3's parity row
   read the other way round: obsidianoid's `dark` is parity-equal to
   shared's **`obsidian`** block `:61-79`, not to shared's `dark` `:15-33`.)*
2. `web/obsidianoid/css/themes.css` (189 lines) is **deleted in full.**
   Surviving definitions: the **18** colour keys × 5 themes at
   `web/shared/css/themes.css:63-170` (obsidian/forest/ocean/ember/rose), and
   the 21 structural names at `web/shared/css/tokens.css:9-43`. Verified
   hex-for-hex for obsidian in §4.3; forest/ocean/ember/rose were donated from
   this very file by Phase 1 (`themes.css:81,102,123,144` name the donor
   ranges). *(**v5 FINAL**, Critic minor 2. v5 published "17 colour keys ×
   5 themes at `:61-164`", which was wrong twice over. **Today**, before C1,
   the five non-`dark`/`light` blocks are 17 keys × 5 = **85** declarations at
   `:61-163` — `:163` is rose's closing brace and `:164` is blank, so the
   published range over-reached by one line. **After C1** the file is the
   195-line, 18-key form: `--color-surface-dynamic` joins T1, every block
   grows by one declaration and the two leading blocks push the five down by
   two lines, giving 18 × 5 = **90** declarations at `:63-170`. This step
   runs at C5, after C1, so the post-C1 coordinates are the ones that belong
   here; both integers are `grep -c` derived from the built sheet, not
   arithmetic.)*
3. The four token renames from §4.3's table are applied across
   `css/app.css` — 6 + 15 + 8 + 3 = **32 `var()` references**, mechanical.
   **This is why items 1–2 cannot be left standing on their own:** until the
   renames land, `app.css` still names **four** tokens no linked sheet defines —
   `--color-surface-offset` (15 refs), `--color-primary-highlight` (8),
   `--color-surface` (6), `--color-error` (3), **32 references** — and the
   affected rules lose their surfaces. *(**v5 FINAL**, Critic minor 1. v5
   listed `--color-surface-dynamic` (2 refs) as a fifth undefined name and
   published 34. It is **not** undefined in that window: C1 added the name to
   the shared sheet's T1 vocabulary and item 1 has already replaced the local
   link with `/shared/dist/shared.css`, so both of its references resolve
   throughout. The count of tokens item 3 renames and the count of names left
   dangling are the same four, and 15 + 8 + 6 + 3 = **32** is the same
   arithmetic the item's own first sentence publishes — v5 disagreed with
   itself in adjacent sentences.)* That is
   **R3 verbatim**, whose stated mitigation is the post-C5 pixel diff, so a
   baseline captured in that state would be R3's failure mode serving as
   R3's reference.
4. `--color-surface-dynamic`'s two consumers are **not edited** — this item is
   a no-op in v5, kept as a numbered sub-step because P5a and §5 Step 5.6 are
   specified against this numbering. *(Rewritten by the user's **Q12** ruling;
   v4 rewrote both sites onto `--color-surface-2`/`-3` because the name had no
   shared counterpart. It has one now: §5 **Step 1.5** lands it as T1's 18th
   key at **C1**, four commits earlier, carrying obsidian's own donated hex
   `#2e2e42` on the `obsidian` theme.)*
   - `css/app.css:202` (`.skeleton-text` shimmer) keeps
     `linear-gradient(90deg, var(--color-surface-offset) 25%, var(--color-surface-dynamic) 50%, var(--color-surface-offset) 75%)`
     apart from the `--color-surface-offset` → `--color-surface-3` **rename**,
     which is item 3's table, not this item's, and which is now
     **value-preserving here too** (`#252535` on both sides — §4.3). The two
     gradient stops stay *distinct* on all 8 themes because Step 1.5 authored
     the three donor-less values one ladder step off each palette's
     `--color-surface-3`; **B2.8** asserts exactly that, now on 8 themes
     instead of asserting a shift.
   - `css/app.css:455` (`#mode-switcher button.active`) keeps
     `var(--color-surface-dynamic)` verbatim — zero edits.
   - `css/app.css:437` is item 3's plain `--color-surface-offset` rename and
     was only ever in this item's count because v4's pair shift touched it. It
     too lands on `--color-surface-3`, value-preserving.

   **The check that this is a no-op and not an omission:** after C5,
   `grep -c 'var(--color-surface-dynamic)' web/obsidianoid/css/app.css` is
   still **2**, and both resolve — from `web/shared/css/themes.css` now that
   the module's own `themes.css` is deleted. That is the whole of what changed:
   the *definition* moved modules, the *references* did not move at all. It is
   also why v4's D-3 retires to zero delta rather than being re-scoped.
5. The four light-theme-correctness defects are fixed:
   `:810` swatch border → the shared `.ui-theme-swatch` rule (the local rule
   is deleted with the panel CSS `:766-812`); `:634` `.disabled-overlay`
   → `var(--overlay-scrim)`; `:90`, `:404`, `:540` `color: #fff` →
   `var(--color-primary-fg)`; `:740`'s `stroke='%237878a0'` data URI →
   a `mask-image` + `background-color: currentColor` rewrite so the chevron
   follows `--color-text-muted`. *(`dialog::backdrop` at `:383` keeps
   `oklch(0 0 0 / 0.6)` — it is theme-independent by intent and FRD `:433-450`
   does not name it. Ledger row 7.)*

**5.3a — three adjacent facts that deliberately do not move, written down so
no reviewer has to re-derive them.** All three were raised in iteration 1 as
"worth one sentence"; here they are.

1. **`css/app.css:417`'s `:root { --sidebar-width: 200px; }` stays, and the
   cascade after C5 is identical to today's.** It sits inside
   `@media (max-width: 640px)` and is the only custom-property declaration in
   the whole 812-line file. After C5 it co-exists with shared
   `tokens.css:38`'s `280px`. Both declarations match `:root` with the same
   specificity, so order decides: `shared.css` is linked **before**
   `css/app.css` (Step 5.3), therefore app.css wins wherever its media query
   applies and the shared value wins outside it — which is exactly the
   arrangement the module has today against its own `themes.css`. Nothing to
   change, and **G12 is not engaged**: redeclaring an existing token's value
   in module CSS is not declaring a new token. *(It is also why the at-rule
   question in ADR-012 matters only for `web/shared/css/`: this declaration
   is in module CSS, which `check-shared-css.mjs` never reads.)*
2. **`.ui-menu-trigger` mounts *outside* `#topbar-actions`.** `app.css:463`
   is `#app[data-mode="threads"] #topbar-actions { display: none; }`, so
   anything inside that container disappears in Threads mode — which is
   precisely why the existing theme panel is unreachable there today. The
   shared trigger is therefore handed `#btn-hamburger` (`index.html:54-56`),
   which lives outside the container, and the rule at `:463` is left
   untouched. This is what makes **D-5** true rather than accidental; put
   another way, D-5 is a *fix* delivered by placement, not by a CSS edit.
3. **Two colour values *are at issue* outside the token vocabulary, and both
   are accounted for.** `:383`'s `dialog::backdrop` keeps its literal (ledger
   row 7, §11 item 8). `:740`'s chevron data URI does **not** survive — the
   `stroke='%237878a0'` literal is removed by the `mask-image` rewrite above,
   which is **D-7**. So after C5 the file contains exactly **one**
   out-of-vocabulary colour, deliberately, and a later token audit will find
   it already documented instead of rediscovering it. *(**v5**: the heading
   said "survive C5", which its own second sentence contradicts — only one
   survives. Architect **N-1**.)*

   **The inventory this claim rests on, by command.** The whole
   out-of-vocabulary colour surface of the 812-line `web/obsidianoid/css/app.css`
   is six declarations plus one escaped literal inside a data URI, and every
   one of them now has an owner:

   ```sh
   grep -nE -- '#[0-9a-fA-F]{3,8}\b|rgba?\(|hsla?\(' web/obsidianoid/css/app.css
   grep -n -- '%23' web/obsidianoid/css/app.css
   ```

   | site | value today | disposition at C5 |
   |---|---|---|
   | `:90` | `color: #fff` | → `var(--color-primary-fg)` — **D-17** |
   | `:383` | `dialog::backdrop { background: oklch(0 0 0 / 0.6) }` | **kept** — ledger row 7, §11 item 8 |
   | `:404` | `color: #fff` | → `var(--color-primary-fg)` — **D-17** |
   | `:540` | `color: #fff !important` | → `var(--color-primary-fg)` — **D-17** |
   | `:634` | `.disabled-overlay { background: rgba(19, 19, 26, 0.72) }` | → `var(--overlay-scrim)` — **D-16** |
   | `:740` | chevron `stroke='%237878a0'` | literal removed by the `mask-image` rewrite — **D-7** |
   | `:810` | `.theme-swatch { border: 1px solid rgba(255,255,255,0.15) }` | rule deleted with the panel CSS `:766-812`; the shared `.ui-theme-swatch` rule takes over — **D-15** |

   > **Why the first grep does not find `:383` or `:740`, and why that is the
   > point of running two.** `oklch()` is not in the first pattern's
   > alternation, and the chevron's `#` is percent-encoded as `%23` inside the
   > URI, so a single hex/`rgb` sweep reports **five** sites and silently
   > misses the two that are most easily forgotten — the one deliberate
   > survivor and the one that only D-7 removes. Two commands, seven sites,
   > one leftover. Stated because a future audit will reach for the one-liner
   > first.

**5.4 — The rename, and the migration it requires (ADR-014).**

- `index.html:2` `data-theme="dark"` → `data-theme="obsidian"`. **This bullet
  closes Step 5.3's ordered sub-step and is the last edit before P5a's
  capture** *(v4 — Architect A1)*: until it lands, the document asks for
  `dark` and the shared matrix answers with shared `dark` (`:15-33`), not
  with obsidian (`:61-79`) — so the page renders in GitHub-dark rather than
  in obsidianoid's palette. After it lands, B2.7's 0-mismatch parity result
  guarantees the obsidian palette equals the old local `dark` palette hex for
  hex, which is precisely what makes §5 Step 5.6's scope claim true.
- `internal/obsidianoid/build.go:41` default `"dark"` → `"obsidian"`.
- `unified-webapp.json:53`, `unified-webapp-example.json:67`,
  `local-test/config.json:63`, `README.md:88`: `"theme": "dark"` →
  `"theme": "obsidian"`. *(`unified-webapp.json:52`'s `"forest"` is
  unchanged — forest keeps its name.)*
- **One-time localStorage migration**, run **once**, at module scope, before
  the `ThemeManager` is constructed at `js/app.ts:503`: every
  `localStorage` key beginning `obsidianoid-theme-` whose value is exactly
  `"dark"` is rewritten to `"obsidian"`. Without it, a user who had explicitly
  chosen obsidianoid's dark now silently gets shared `dark` (`#0d1117`)
  instead of `#13131a` — a visual regression in the one module whose identity
  this commit is preserving. The migration is guarded so it runs at most once
  (a `ui-theme-migrated:obsidianoid` flag) and is deleted by §11 item 3 in a
  later phase.

  > ***The mechanism is a prefix scan, and v5 has to say so because the v4
  > wording was unsatisfiable (Critic F-D item 6).*** v4 said "run once per
  > vault key on boot before the `ThemeManager` resolves". There is no list of
  > vault keys at that moment: the roster arrives from
  > `Promise.all([fetchConfig(), fetchVaults()])` at `js/app.ts:507`, so
  > `state.vaults` is `[]` for the whole of module evaluation, and "once per
  > vault key" names an empty iteration. Deferring the migration until after
  > the fetch is worse, not better — `fetchVaults` is exactly where the
  > **initial** resolution happens (`:478-479`, now `themes.reresolve()`), so a
  > migration running after it would rewrite storage the resolver had already
  > read, and the first paint of the session would still be shared `dark`.
  >
  > The keys are therefore discovered from **storage**, not from the roster,
  > which needs no network and no roster:
  >
  > ```ts
  > const MIGRATED = 'ui-theme-migrated:obsidianoid';
  > if (!localStorage.getItem(MIGRATED)) {
  >   for (const k of Object.keys(localStorage)) {
  >     if (k.startsWith('obsidianoid-theme-') && localStorage.getItem(k) === 'dark') {
  >       localStorage.setItem(k, 'obsidian');
  >     }
  >   }
  >   localStorage.setItem(MIGRATED, '1');
  > }
  > ```
  >
  > Three properties this shape has and "once per vault key" did not:
  > **(1)** it covers keys for vaults that are not in the current config at
  > all — a vault removed from `unified-webapp.json` still has its key, and a
  > roster-driven loop would skip it and leave a `"dark"` landmine for whenever
  > it comes back; **(2)** `Object.keys` is snapshotted before the writes, so
  > mutating values mid-loop is safe (only values change, never the key set);
  > **(3)** the prefix test is on the **key**, not the value, so `ui-theme:todo`
  > and every other module's key are untouchable by construction — which is
  > the property B2.6's foreign-key seed exists to prove. The `=== 'dark'`
  > equality is deliberate too: it migrates only the exact legacy value and
  > leaves `"forest"`, `"ocean"`, `"ember"`, `"rose"` and an already-migrated
  > `"obsidian"` alone.
  >
  > *Guaranteed by (criterion): **B2.6**, which seeds two obsidianoid keys, one
  > non-`dark` obsidianoid key and one foreign key, and asserts the flag and
  > the second-boot no-op.*
- **The Go side is exactly two lines, and v1 cited a file that does not
  exist.** v1 wrote "`handler_test.go` / `build_test.go` … to be confirmed at
  implementation time". There is **no `internal/obsidianoid/build_test.go`**
  — the package's test files are `events_test.go`, `git_test.go`,
  `handler_test.go`, `state_test.go`, `threads_test.go`, `vault_test.go`.
  The command v1 deferred has now been run:

  ```
  $ grep -rn '"dark"' internal/obsidianoid/
  internal/obsidianoid/build.go:41:			cfg.Vaults[i].Theme = "dark"
  internal/obsidianoid/handler_test.go:40:			{Path: v0, Name: "Vault0", Theme: "dark"},
  ```

  Two occurrences, and they are **not the same kind of thing**:
  - `build.go:41` is the **default** applied to a zero-value `Theme`
    (`:39-43`). This one *must* change to `"obsidian"`; it is the server-side
    half of the rename and leaving it makes a freshly-configured vault render
    shared `dark` (`#0d1117`) instead of obsidianoid's palette.
  - `handler_test.go:40` is **fixture data**, not an assertion about the
    default — it sets `Vault0`'s theme explicitly, and `"dark"` remains a
    valid shared theme name afterwards, so the test **still passes
    unchanged**. Changing it is *discretionary hygiene*: it keeps the fixture
    meaning "obsidianoid's own palette" rather than silently becoming "the
    shared dark theme". **Decision: change it**, in the same commit, because
    the adjacent `:41` fixture is `"forest"` — a name the rename leaves alone
    — and a reader comparing the two would otherwise have no way to tell that
    one is post-rename and the other is pre-rename.

  Because neither line is load-bearing for `go vet` or compilation, the Go
  half of C5 cannot fail silently: `make test` exercises `handler_test.go`
  and **B2.5** covers `build.go:41` by asserting `grep -c '"dark"' internal/obsidianoid/build.go` = 0, rc=1.

**5.5 — Sanctioned visual/behavioural deltas in obsidianoid** (nothing else
may move):

| | Delta | Why |
|---|---|---|
| D-1 | `--shadow-md` becomes `0 8px 32px rgba(0,0,0,0.4)` at 3 sites | shared `tokens.css:16`, a documented Phase-1 exception (`:3-4`) |
| D-2 | `--font-body` fallback chain gains `system-ui` | shared `tokens.css:33,35`; invisible while Inter loads |
| ~~D-3~~ | ***Retired in v5 — zero delta.*** The row read "skeleton shimmer and `#mode-switcher button.active` shift one surface step (3 edits: `app.css:202`, `:437`, `:455`)" | The user's **Q12** ruling makes `--color-surface-dynamic` the 18th shared token at **C1** (§5 Step 1.5), carrying obsidianoid's own donated `#2e2e42` on `obsidian`. The **`--color-surface-dynamic` name is untouched at both of its sites** — `:202`'s middle gradient stop and `:455`'s background (§5 Step 5.3 **item 4**, a no-op) — so `:455` is not edited at all, and `:202` and `:437` take only the plain, value-preserving `--color-surface-offset` → `--color-surface-3` rename that **item 3** applies module-wide (`:202` carries **two** of those references, `:437` one). The one-surface-step *shift* this row declared therefore **does not happen** at any of the three lines. *(**v5 FINAL**, Critic finding 7. v5 wrote "`:202` and `:455` are therefore **not edited**", which is false at `:202`: item 3's rename is a text edit to that line, and item 3's own grep requires it. The distinction the row needs is between the **token name** `--color-surface-dynamic`, which item 4 leaves alone, and the **line**, which item 3 rewrites — a zero *value* delta, not a zero *byte* delta.)* The row number is retired rather than reused — D-numbers are unique across the plan and a reader meeting "D-3" in a review, a commit message or ledger row 14 must land here and not on a different delta. **B2.8** now asserts the zero-delta shape instead |
| D-4 | Errors become sticky toasts; toast geometry becomes the shared stack | ADR-010; FRD `:319` |
| D-5 | The theme picker is now reachable in Threads mode | it was unreachable via `css/app.css:463`; the shared trigger mounts outside `#topbar-actions` |
| D-6 | 8 themes offered where 5 were | FRD `:183-196`'s roster is the single definition |
| D-7 | The vault-selector chevron now follows the theme | `:740` was frozen at obsidian's muted grey |
| **D-15** | Each swatch's 1px border now follows the **previewed** theme, not the page theme — replacing the donor's fixed `rgba(255,255,255,0.15)` (`css/app.css:810`) with `var(--color-border)` | the unavoidable consequence of ADR-015: stamping `data-theme` on the swatch re-resolves **every** colour token in that element's subtree, the border included. Deliberate, and the reason the border reads correctly on light *and* dark swatches (FRD `:257-261`). §0 Decision 4, ledger row 15. **One theme behaves unlike the other seven and it is worth knowing before the review:** of the eight `--color-border` values in `web/shared/css/themes.css` (`:20, :43, :66, :87, :108, :129, :150, :172`) seven are opaque hexes and **puma's (`:172`) is `rgba(255, 255, 255, 0.11)` — translucent**. So the puma swatch's border composites against the swatch's own `--color-primary` fill (`#3fbf9c`) rather than replacing it, giving a subtly lighter edge than the other seven; that is the *closest* behaviour to the donor's fixed `rgba(255,255,255,0.15)` and is the one swatch a pixel diff will show moving least. Not a defect and not worth special-casing — recorded so a reviewer comparing the eight swatches side by side does not read the odd one out as a bug *(v3, Critic minor)* |

| **D-16** | `.disabled-overlay`'s scrim stops being obsidian-specific: `css/app.css:634`'s `rgba(19, 19, 26, 0.72)` becomes `var(--overlay-scrim)`. On **7** themes that resolves to `rgba(0, 0, 0, 0.55)` (`web/shared/css/tokens.css:42`); on **light** the theme overrides it to `rgba(40, 37, 29, 0.35)` (`web/shared/css/themes.css:56`), one of `THEME_ALLOW`'s three permitted non-colour props (`check-shared-css.mjs:96`) | ***New in v5 — Architect B-2.*** This is one of the four edits §5 Step 5.3 **item 5** already prescribed, and v4 gave it no D-row, so §5 Step 5.6's attribution rule made the only compliant executor response a **scheduled stop**. Two directions of change, both intended: on obsidian the overlay loses its purple tint and drops from 0.72 to 0.55 alpha (the literal's own comment says it "matches `--color-bg` at 72% opacity", i.e. `#13131a` — a value that is *wrong by construction* on the seven themes obsidianoid did not have), and on light it becomes a warm ink at 0.35 instead of near-black at 0.72, which is the light-theme-correctness defect item 5 exists to fix. The same token and the same two-direction behaviour are already documented for todo's backdrop at **D-14**, so this is the second consumer of one decision, not a second decision |
| **D-17** | The three `color: #fff` literals become `var(--color-primary-fg)`: `css/app.css:90` (`#btn-save:not(:disabled):hover`), `:404` (`.dialog-actions .btn-primary`) and `:540` (`.btn-save:hover:not(:disabled)`, where the `!important` is **preserved** — module CSS, so clause 9 does not apply). On `dark`, `light` and `obsidian` the value is `#ffffff` (`themes.css:29`, `:52`, `:75`) — **byte-identical, zero pixel delta**; on `forest`, `ocean`, `ember`, `rose` and `puma` it is `#0b0f14` (`:96`, `:117`, `:138`, `:159`, `:181`) | ***New in v5 — Architect B-2***, same defect as D-16: prescribed by §5 Step 5.3 item 5, unattributable in v4. All three sites set foreground text on a `var(--color-primary)` **background** declared on the line immediately above (`:89`, `:403`, `:539`), which is precisely what `--color-primary-fg` is for — Step 1.4 authored its 8 values as a contrast matrix against those primaries. The delta is a **fix on the five themes where it appears**: ember's primary is `#f0a04a` and rose's `#e05c7a`, on which white text was failing contrast; `#0b0f14` is the value Step 1.4 chose for exactly those grounds. Three themes show no change at all, which is why this row's pixel diff is expected to be *empty* on the P5a reference theme (`obsidian`) and non-empty on five of the other seven |

> **Numbering:** D-rows are unique across the whole plan, so the swatch-border
> row is **D-15** and not D-8 — `D-8…D-14` are todo's drawer deltas (§5 Step
> 6.2a). D-15 is filed here because obsidianoid is where it is *measurable*: it
> has a pre-existing swatch with a pre-existing border to diff against, whereas
> the sampler's picker is net-new and has no baseline. It applies to both hosts.
> **D-16 and D-17 continue the sequence past todo's block** for the same
> reason: the numbers are global, so obsidianoid's rows are not contiguous and
> are not meant to be.
>
> ***Why D-16 and D-17 had to be added, and what it says about the D-table as a
> whole*** (Architect B-2). §5 Step 5.3 item 5 prescribes **four**
> pixel-visible edits — the swatch border, `.disabled-overlay`, the three
> `color: #fff` sites, and the chevron — of which v4 gave D-rows to exactly
> **two** (D-15 and D-7). §5 Step 5.6 then requires every non-empty region of
> the P5a → end-of-C5 diff to be attributable to a D-row and forbids widening
> the rule in flight, so an executor who performed item 5 correctly would reach
> two unattributable diff regions and, following the plan exactly, **stop the
> commit**. That is the plan defeating itself, not a gap in the executor's
> judgement. The generalisation, which is the Architect's back-pointer rule
> applied to pixels rather than to gate output: **every prescribed edit that
> changes a rendered colour needs a D-row, and every D-row needs a prescribing
> step** — §5 Step 5.6's composition lists are now the both-ways check, and the
> four edits of item 5 map to D-15, D-16, D-17 and D-7 with nothing left over.

**5.6 — Pixel evidence.** *(Re-specified in **v4** — Architect B-1/A1, Critic
F-6. v2's version was unsatisfiable one way and v3's rewrite was unsatisfiable
another, so **v4 changes the kind of specification** rather than the wording
for a third time: the capture point is named by the **state the tree must be
in**, not by which edit it follows. What v3 got right — the two-part
attribution rule and the ledger-row-13 half — is kept intact; only the anchor
moves.)*

Two baselines exist for obsidianoid and this step uses the **second** one:

1. **P5's** pre-C5 capture (5 themes × notes/threads) is the *reference for
   the typography change itself* and for nothing else. **P5a** rules it out as
   a pixel reference for the rest of C5: adding the `/shared/dist/shared.css`
   link pulls in `fonts.css`'s 15 `@font-face` rules, so every glyph on the
   page reflows and a P5-versus-post-C5 diff is non-empty essentially
   everywhere. v2's "baseline shots from P5 are re-taken after C5 and diffed"
   therefore prescribed the one comparison P5a exists to forbid.
2. **P5a's** capture is the reference of record, and it is taken at the
   **first point in C5 at which obsidianoid is renderable on its own
   vocabulary**: after Step 5.3's items 1–4 *and* Step 5.4's
   `data-theme="obsidian"` stamp, which together are one working-tree
   sub-step (Step 5.3's lead paragraph). Every later C5 step is diffed against
   **this**, per theme × mode. **Step 5.1 may land on either side** — it edits
   `scripts/` and `Makefile` only and is pixel-inert. **Steps 5.2 and 5.3
   item 5 land after the capture**, which is the one ordering constraint v4
   adds beyond Architect A1's wording: both are pixel-visible (5.2 deletes
   `app.css:351-371`'s `#toast` rules and the toast/popover markup; item 5
   rewrites the chevron and three `color: #fff` sites), and pinning them after
   the shutter is what makes the P5 → P5a diff's composition **exact** instead
   of conditional on the executor's sequencing. Nothing in items 1–4 depends
   on either.

**Why by state and not by edit order.** Both iteration-3 reviewers proved that
every *edit-ordered* trigger names a tree in which the page is broken, and
each broken state fails in a way the step would then have to excuse:

| Candidate trigger | The state it names | Why it is not a reference |
|---|---|---|
| after 5.3 item 1 alone | shared linked, local `themes.css` gone, no renames | **32** `var()` references undefined (R3) — panels lose their surfaces. *(**v5 FINAL**, Critic minor 1: 15 + 8 + 6 + 3 across the four renamed names; `--color-surface-dynamic`'s two references resolve, because post-C1 the shared sheet defines that name and item 1's link has already landed. §2 P5a and §5 Step 5.3 item 3 publish the same 32.)* |
| after 5.3 items 1–4, before 5.4 | vocabulary complete, document still says `data-theme="dark"` | shared `themes.css:15` is `:root, [data-theme="dark"]`, so the page resolves shared **dark**, not **obsidian** — all 12 colliding names differ in value (§4.3's parity table) and `web/obsidianoid/css/app.css` consumes them **121** times (measured `var(--name)` occurrences; `--color-primary` and `--color-text` 21 each, `--color-border` 20, `--color-text-faint` 14, `--color-text-muted` 13, `--color-divider` and `--color-surface-2` 8 each, `--color-primary-active` 6, `--color-bg` 5, `--color-primary-hover` 3, `--color-success` 2, `--color-warning` 0). Every panel recolours |
| "after the link, before any other C5 edit" (v3) | both of the above at once | and it contradicts §5's own numbering, since 5.1 and 5.2 are edits that precede 5.3 |
| **after 5.3 items 1–4 + 5.4's stamp (v4)** | obsidianoid rendering obsidian from the shared matrix | **the reference.** B2.7's 0-mismatch parity makes this palette hex-for-hex the old local `dark` palette |

The attribution rule stays in two parts, because the typography change is
**ledger row 13**, not a D-row — v2 required every non-empty diff to map to
"a row of the D-table above", and §5 Step 5.5's D-table (D-1, D-2, D-4…D-7,
D-15…D-17 — D-3 retired) has no row for it, so the largest diff in the commit
was unattributable by construction:

- **P5 → P5a.** The diff is expected to be **large and global**, and v4 states
  its full expected composition rather than a scope claim that was false at
  every candidate capture point:
  1. a whole-page glyph reflow — **ledger row 13**, with **D-2** covering the
     narrower `--font-body` fallback-chain half, and **B2.6** asserting it as
     an intended change;
  2. **D-1** — `--shadow-md` goes from the local `0 4px 16px oklch(0 0 0 /
     0.4)` (`web/obsidianoid/css/themes.css:25`) to shared
     `tokens.css:16`'s `0 8px 32px rgba(0, 0, 0, 0.4)`, at all three
     `box-shadow: var(--shadow-md)` sites (`css/app.css:362`, `:381`, `:775`);
  3. **nothing from the surface tokens** — and in v5 that is an *assertion*,
     not an omission. v4's item 3 here was **D-3**, the one-surface-step shift
     in the shimmer (`app.css:202`) and `#mode-switcher button.active`
     (`:455`). D-3 is retired: the `--color-surface-dynamic` **name** is
     untouched at both of its sites (item 4 is a no-op), `:455` is not edited
     at all, and the `--color-surface-offset` → `--color-surface-3` rename
     item 3 applies at `:202` (twice) and `:437` (once) is
     value-preserving (`#252535` both sides, §4.3's parity table) — so every
     one of the three lines contributes **zero pixels**, two of them by not
     being edited and `:202`/`:437` by being edited to the same value.
     *(**v5 FINAL**, Critic finding 7: v5 said "`:202` and `:455` are not
     edited at all", which is false at `:202`.)* So the
     shimmer, the mode switcher and every other surface in the module must be
     **pixel-identical** across this window except for the glyph reflow that
     moves their text. A non-empty surface-colour region here is now a
     **finding**, where in v4 it was expected.

  That is the whole list. **D-4, D-5, D-6, D-7, D-15, D-16 and D-17 fall on
  the other side of the shutter** — D-4 and D-5 with Step 5.2, D-7, D-16 and
  D-17 with Step 5.3 item 5, and D-6/D-15 with the shared picker that Step 5.2
  mounts — so they belong to the P5a → end-of-C5 window, not here.

  *(v4 deletes v3's "no element gains or loses a box, **no panel changes
  colour**" claim. The second half was false — and it was false **with a
  mechanism attached** ("the theme matrix is not yet being consumed by any
  *renamed* token"), which is the shape that gets an executor to wave away the
  exact diff the step exists to catch. The premise was true and the conclusion
  did not follow: the shared matrix is consumed at the instant the link lands
  through the **12 token names that are identical in both matrices**, not
  through the renamed ones. The first half was true but is now redundant —
  D-1 and D-7 are box-preserving (as was D-3, before v5 retired it), so "no
  element gains or loses a box" is the residual property and it is stated below
  as the check. D-16 and D-17 are box-preserving too — both change only a
  colour value — so the property holds for the whole commit, not just for this
  window.)*

  **What is checked:** every non-empty region of the P5 → P5a diff is
  accounted for by **ledger row 13 / D-2** (item 1) or **D-1** (item 2) and by
  nothing else, and **no element gains or loses a box** — layout geometry moves
  only by text metrics. An unaccounted region here stops C5 exactly as it does
  below. *(v5: v4 wrote "one of the four rows above" over a three-item list,
  counting row 13, D-2, D-1 and D-3 as four rows across three items. With D-3
  retired the list is two attributing rows plus item 3's explicit
  zero-delta assertion, so the rows are named instead of counted.)* Colour
  equality is *not* asserted at this boundary and does not need to be:
  **B2.7** asserts it directly and far more precisely, by comparing all 17
  keys × 5 blocks for 0 mismatches.

  *Guaranteed by (steps): item 1's reflow is caused by §5 **Step 5.3 item 1**
  (the `shared.css` link, which is what pulls in `fonts.css`); item 2's
  `--shadow-md` change is caused by §5 **Step 5.3 items 1–2** together (the
  shared sheet arrives, the local `themes.css` that overrode the token is
  deleted); item 3's zero-delta assertion is guaranteed by §5 **Step 5.3
  item 4** being a **no-op** and by the `--color-surface-offset` →
  `--color-surface-3` rename in **item 3** being value-preserving (§4.3's
  parity table, `#252535` both sides). **v5** — added by the back-pointer
  sweep: this window asserted a three-item composition and named no step for
  any item, and item 3 in particular asserts that a step does **nothing**,
  which is unverifiable unless the step that does nothing is named.*
- **P5a → end of C5** (everything else). Every non-empty diff must be
  attributable to a row of §5 Step 5.5's D-table — **D-1, D-2, D-4, D-5, D-6,
  D-7, D-15, D-16, D-17** (D-3 retired; `D-8…D-14` are todo's and cannot
  appear in an obsidianoid diff). An unattributable diff here stops C5. The
  window's expected composition, so that "attributable" is checkable rather
  than arguable: Step 5.2 contributes **D-4** (toast), **D-5** and **D-6**
  (picker reachability and roster) and **D-15** (swatch border); Step 5.3
  item 5 contributes **D-7** (chevron), **D-16** (`.disabled-overlay`) and
  **D-17** (the three `color: #fff` sites); **D-1** and **D-2** land before
  the shutter and must not reappear after it.

  *Guaranteed by (steps): §5 **Step 5.2** and §5 **Step 5.3 item 5**, whose
  four pixel-visible edits map onto D-15, D-16, D-17 and D-7 with nothing left
  over — the both-ways check §5 Step 5.5's closing note states as a rule.*

**The attribution rule may not be widened in flight.** If a diff at either
boundary cannot be attributed, C5 stops and the plan is revised — adding a
"palette churn" row to absorb an unexplained global recolour would delete the
only instrument in Phase 2 pointed at **cascade-order** defects. C5's other
guards (B2.7 parity, B1.1's ranged diff, B2.8's computed-colour assertion,
B2.6's both-ways migration) are all **value** checks; none of them sees a
stylesheet linked in the wrong position. *(v4 — Architect B-1's stated repair
hazard, recorded as a rule so the repair is not available.)*

Diffs are inspected, not committed (P3).

> **User ruling, 2026-09-16 — the pixel windows are best-effort evidence, not
> a release gate.** Asked whether P5a's two-window attribution was worth its
> cost, the user answered:
>
> > "again, best effort. If I have to make corrections later, that's ok."
>
> What that changes: **an unattributable diff no longer stops C5 by itself.**
> It is recorded — named region, theme, mode, and the executor's reading of
> the cause — and C5 proceeds. What it does **not** change: the composition
> lists above stay exactly as written, because their value was never the
> stopping power. They are what makes "I looked and it seemed fine" into a
> statement with content, and they are how a correction *later* gets pointed
> at the right edit instead of at the whole commit. The two-part rule above
> ("may not be widened in flight") also stands unchanged, and is now the
> sharper of the two instruments: widening the **D-table** to absorb a
> surprise is still forbidden, because the D-table is what C6's and C7's
> windows are read against as well. The distinction is: *the diff is advisory,
> the attribution vocabulary is not.*
>
> The one place this ruling is load-bearing rather than permissive is the
> **font reflow**. P5 → P5a is expected to be non-empty essentially
> everywhere, and no realistic reviewer can confirm by eye that a global
> glyph reflow hides nothing. v4 wrote that window as if it could be
> discharged; under this ruling it is honestly labelled as the window whose
> colour evidence comes from **B2.7** (17 keys × 5 blocks, 0 mismatches) and
> whose pixel evidence is corroborating at best.

**Acceptance (C5):** B1.1, B1.2, B2.1–B2.12, B3.9, B4.2, B5.8, B9.1–B9.4,
BX.4, BX.5, BX.6, BX.9, BX.11.
**B3.10** (`reresolve()`) lands at C3 but is first *exercised* here, by
`switchVault`. Artifact count stays 15.

### Step 6 → C6a + C6b: todo adoption, split in two

> **v2 change — C6 becomes two commits (ADR-016).** Both reviewers converged
> on this and it is **adopted**. v1's single C6 bundled two unrelated
> migrations (theme system, drawer) whose only shared artifact is `shell.ts`,
> and the bundling created the one *jointly unsatisfiable* claim in the plan:
> C6 asserted pixel-parity for todo's drawer while simultaneously deleting and
> rebuilding it. Split:
>
> | | Contents | Why it stands alone |
> |---|---|---|
> | **C6a** | build entry + `ThemeManager` + `theme.js` deletion + `#theme-toggle`/`#theme-icon` on **both** pages + `compare.html` | The drawer is **not touched**, so "drawer pixels unchanged" is true *by construction* rather than by assertion, and `compare.html` — which has no drawer and never gets one — is fully resolved here instead of hanging off a drawer commit |
> | **C6b** | the drawer onto `HamburgerMenu`, index.html only | Pixel-compares against a tree where the theme system is already settled, so a drawer regression cannot be confused with a theming regression |
>
> Revert order is C6b before C6a (C6b's drawer code imports the shell C6a
> creates). Nine commits total: C1–C5, C6a, C6b, C7, C8.

#### The one deliberate boundary in todo — stated once, here

`index.html` and `compare.html` are **not** the same page, and v1 treated them
as one. Verified inventory:

| | `index.html` | `compare.html` |
|---|---|---|
| `#theme-toggle` + `#theme-icon` | `:179`, `:180` | `:41`, `:42` |
| inline `onclick="toggleTheme()"` | `:179` | `:41` |
| `<script src="js/theme.js">` | `:20` | `:18` |
| static `data-theme="dark"` (FOUC insurance) | `:2` | `:2` |
| `#menu-toggle` | `:172` | **absent** |
| `#sidebar` / `#sidebar-backdrop` | `:201`, `:195` | **absent** |
| `toggleSidebar`/`closeSidebar` | `:78`, `:82` | **absent** |
| loads `js/todo.js`, `js/todo-utils.js` | yes (`:19`, `:18`) | **no** |

So the two pages share the **theme** surface exactly and share **nothing** of
the drawer surface. `shell.ts` is therefore loaded by both pages and does two
independent things, **each guarded on its own anchor element** — one `if`, not
a page-sniffing branch, not two bundles, and not a `window` façade:

```ts
// shell.ts — the whole boundary, in two guards.
initTheme();                                     // both pages: #theme-toggle + #theme-icon exist on both
if (document.getElementById("menu-toggle")) {    // index.html only
  initDrawer();                                  // C6b adds this call's body
}
```

This is what makes C6a's `compare.html` edit safe and C6b's drawer code
inert on `compare.html`. The guard lands in **C6a** (with `initDrawer` a
no-op), so C6b adds only the body — the boundary is never in flux.

**6.1 — todo enters the build pipeline with one new entry (ADR-013).**
New `web/todo/js/shell.ts`, a `sharedConsumer` **bundle** descriptor
(`mode: "bundle"`, `out: "web/todo/js/shell.js"`, `format: "esm"`,
`bundle: true`). `scripts/descriptors.mjs:162`
`EXPECTED_ARTIFACT_COUNT` **15 → 16** at **C6a** — the phase's only move, and
a two-line edit per G15 (`:162` plus `:160`, the value-bearing line of the
`:157-161` trajectory comment).
`tsconfig.json:15` gains `"web/todo/js/*.ts"` — 11 globs → **12**, in this
same commit (the Q4 coupling principle: a glob that lands later means a file
that is unchecked in between). `Makefile:62` gains `todo` to
`--require=` (G16).

`shell.ts` is the *only* TypeScript in todo. **19** `.js` files exist under
`web/todo/js/` today *(v1 said 20; Critic M-10 is correct — verified `ls
web/todo/js/ | wc -l` = 19)*. C6a deletes `theme.js` and adds the emitted
`shell.js`, so the directory holds **18 legacy + `shell.js` = 19** after C6a,
and C7 deletes 12 more of the legacy 18, leaving **6 legacy + `shell.js` = 7**.
jQuery stays. `shell.js` is loaded as a `<script type="module">` **after** the
legacy scripts on both pages — so the inline handlers in `index.html`
(`onchange="changeSubject()"` etc.) keep resolving against the globals
`js/todo.js` already defines, and `type="module"`'s deferral guarantees the
ordering rather than merely hoping for it.

*Guaranteed by (criteria): **B9.1**, **BX.11**.*

**6.2 — The drawer is re-homed.**

> **The coordinate shift C6a causes in `web/todo/index.html`, declared once
> here.** *(**New in v5 FINAL**, Critic finding 3. The plan's only other shift
> declaration — Step 1.5's uniform **+1** over `check-shared-css.mjs` — is
> explicitly scoped to that file and `themes.css`, so this file's shift was
> undeclared while every citation below depended on it.)*
>
> **6.3 item 3 lands at C6a and replaces one line with a block.**
> `index.html:20` today is the single line `<script src="js/theme.js">`;
> item 3 replaces it with the sentinel-wrapped pre-paint bootstrap plus the
> `shell.js` module tag — as specified there, **6 lines** (`<!-- theme-bootstrap:start -->`,
> `<script>`, the resolution statement, `</script>`, `<!-- theme-bootstrap:end -->`,
> `<script type="module" src="js/shell.js"></script>`). One line out, six in:
> the net shift is **+5**, not +6.
>
> **Therefore every `index.html:NN` citation in this step, in §5 Step 6.5 and
> in §13(b)'s todo rows is to the *pre-C6a* tree**, because that is the tree
> the plan was written against and the tree a reviewer can check today. At
> C6b, any of them **below `:20`** is read with **+5** applied: `:78-85` →
> `:83-90`, `:149-151` → `:154-156`, `:172-174` → `:177-179`, `:195` →
> `:200`, `:201-252` → `:206-257`. Citations **above** `:20` — `:2`, `:8`,
> `:13` — are unmoved by C6a, since the block lands below them.
>
> **The binding rule, and it is a rule about re-derivation rather than about
> the constant.** If the landed block is not 6 lines, the shift is
> *(lines in) − 1*, re-derived from C6a's own diff — the executor reads the
> number out of `git show <C6a-sha> -- web/todo/index.html`, never out of this
> paragraph. And the shift is a convenience, not a dependency: **every target
> in this step is addressed by `id`** (`#menu-toggle`, `#sidebar-backdrop`,
> `#sidebar`) or by function name (`toggleSidebar`/`closeSidebar`), so a
> mis-added constant produces a citation a reviewer must re-derive, never a
> wrong element edited. The line numbers are here to make the diff reviewable,
> and `id`s are what make the edit correct.
>
> `compare.html` takes the same treatment at `:18` and is subject to the same
> rule, but no C6b citation in this step falls below it — its only C6b edit is
> the `?v=` bump at `:13`, above the block.

- `index.html:172-174` (`#menu-toggle`) is kept as the trigger element and
  handed to `HamburgerMenu` via `mountTrigger`; its inline
  `onclick="toggleSidebar()"` is removed.
- `index.html:195` (`#sidebar-backdrop`) and `:201-252` (`<aside
  id="sidebar">`) are **deleted** — the shared class owns both.
- `index.html:78-85`'s `toggleSidebar`/`closeSidebar` are **deleted** from
  the inline `<script>`. Surviving definition: `menu.ts`'s `toggle()`/
  `close()`.
- `todo.css:103-144` (`.sidebar`, `.sidebar.sidebar-open`,
  `.sidebar-section`, `.sidebar-divider`, `.sidebar-backdrop`,
  `.sidebar-backdrop.sidebar-open`) and **`:615-619`** — the
  `/* ── Responsive ── */` heading, its blank line and `:617-619`'s mobile
  width rule — are **deleted**. The block runs `.sidebar {` at **`:103`** through
  `.sidebar-backdrop.sidebar-open { display: block; }` at **`:144`**.
  *(**v3 correction, Architect N2.** v2 "corrected" this to `:102-143` and
  claimed v1 was off by one at both ends. **v1 was right and v2 was wrong.**
  Re-verified by printing `web/todo/css/todo.css:99-146` with line offsets:
  `:101` is the `/* ── Sidebar (slide-in drawer) ── */` section comment,
  `:102` is blank, `:103` is `.sidebar {`, `:143` is the `}` closing
  `.sidebar-backdrop`, and `:144` is the live rule
  `.sidebar-backdrop.sidebar-open { display: block; }`. Deleting `:102-143`
  would therefore have **stranded `:144`** — a rule whose only companion is
  gone, so the backdrop would be `display: none` forever and the drawer would
  open with no scrim. Every line citation in the 6.2a D-table below is shifted
  **+1** by the same correction, and §13(b)'s row already read `:103-144`,
  which is how the contradiction was detectable at all.)*
  **The section comment at `:101` is deliberately kept out of the deletion
  range** so that the two blank lines and the comment collapse in one
  follow-on whitespace tidy rather than making the range look like it removes
  a heading it does not own; the executor deletes `:101-102` as well if no
  `.sidebar*` rule remains, which after this step is the case — stated here so
  the range is unambiguous rather than silently either/or.
  **The same tidy applies at the file's tail, and v3 stated it only at the
  head** *(v4 — Architect A10)*: `todo.css` is **619** lines, `:617-619` is
  its last content, and `:615` is the `/* ── Responsive ── */` section comment
  with `:616` blank. `:617-619` is the **only** rule that heading owns, so
  deleting the rule alone leaves a 616-line file ending in a heading with
  nothing under it. The executor therefore deletes **`:615-619`** — heading,
  blank and rule — by the identical reasoning that keeps `:101` out of the
  drawer range and then removes it: a section comment is deleted with its last
  member, never before it. (This is the tail, so no renumbering follows; the
  head tidy and this one are independent.)
  Surviving definitions: `.ui-menu-drawer`, `.ui-menu-backdrop` and the shared
  media query in `components.css`.
- **`web/todo/index.html` links `/shared/dist/shared.css`, and the link is
  positioned.** *(**New in v5.** This edit was **missing from the plan
  entirely** — §4.8 asserted "Only `index.html` links `shared.css`" and
  **B4.10**/**B4.11** assert the drawer renders from shared classes, but no
  step ever wrote the `<link>`. Found while applying the user's cache-buster
  ruling. It is the same defect class as the `type="module"` omission in C5:
  the single most load-bearing edit of the commit existed only as a
  consequence other text depended on.)* The new line goes **immediately
  before `index.html:8`**, i.e. ahead of the Google-Fonts group at `:8-10`:

  ```html
  <link rel="stylesheet" href="/shared/dist/shared.css">
  <link rel="preconnect" href="https://fonts.googleapis.com">          <!-- :8 -->
  ```

  > **Why *before* the fonts group and not last. (v5.)** Three orderings were
  > considered and only one is correct.
  >
  > 1. **After `css/todo.css?v=19` (`:13`)** — wrong. `shared.css` would then
  >    be the last stylesheet, and while the two token vocabularies are
  >    disjoint (**R9**: `--bg` versus `--color-bg`, so nothing is
  >    overridden), `components.css`'s `.ui-*` rules would win any future
  >    collision over todo's own, inverting the intended precedence for a
  >    module that keeps its own look.
  > 2. **After the fonts group, before `css/fontawsme.css` (`:11`)** — also
  >    wrong, and this is the one that bites. `todo.css:42` sets
  >    `font-family: 'Inter', system-ui, sans-serif`, and **both** sheets
  >    supply an `Inter` face: Google's static Inter via `:10`, and shared
  >    `fonts.css:12`/`:20`'s self-hosted faces, which arrive **inlined** in
  >    `shared.css`. *(**v5 FINAL**, Critic minor 5. v5 said `shared.css`
  >    "`@import`s" them, which describes the source and not the artifact.
  >    The importer is `web/shared/css/index.css:5`, `@import "fonts.css";`,
  >    and esbuild's bundling descriptor — `shared-css`, `bundle: true`,
  >    `external: ["*.woff2"]` (`scripts/descriptors.mjs:57-64`) — resolves
  >    every `@import` at build time: `grep -c '@import' web/shared/dist/shared.css`
  >    = **0** and `grep -c '@font-face'` = **15**. The hazard is identical
  >    either way — what matters is that the 15 faces are **in** the linked
  >    file — but "@import" would send a reader looking for a directive the
  >    served sheet does not contain, and it would also imply a second network
  >    fetch whose ordering could be argued about.)*
  >    With two `@font-face` rules matching the same family and weight, **the
  >    later declaration wins**, so linking `shared.css` after `:10` silently
  >    re-faces every glyph on todo's busiest page — a whole-page reflow with
  >    no D-row, in the commit whose pixel window is supposed to show only the
  >    drawer. `todo.css` declares **no** `@font-face` of its own
  >    (`grep -c '@font-face'` = 0), so Google's link is todo's only font
  >    source today and must stay the winner.
  > 3. **Before `:8`** ✅ — shared tokens and `.ui-*` classes are available,
  >    todo's own sheet still cascades last, and Google's Inter is still
  >    declared after shared `fonts.css` and still wins. This is the same
  >    reasoning obsidianoid's Step 5.3 item 1 applies in the opposite
  >    direction: there the font change **is** the intended delta (ledger row
  >    13) and the link replaces a local sheet in place; here it must be a
  >    non-event, so position is the whole specification.
  >
  > **Residual, declared — option 3 is a non-event for every weight
  > `todo.css` asks for, and not for the one weight its *markup* asks for.**
  > *(**New in v5 FINAL**, Architect A-3, which also answers the Critic's
  > open question on todo's weight coverage.)* `index.html:10` requests
  > `family=Inter:wght@400;500;600`, and `todo.css` sets `font-weight` at
  > exactly eight sites — **400 × 1** (`:576`), **500 × 3** (`:85`, `:201`,
  > `:310`) and **600 × 4** (`:74`, `:161`, `:278`, `:543`) — with **no**
  > `font-style: italic` anywhere (`grep -c` = 0). Google's three faces
  > therefore cover every weight the stylesheet requests exactly, and at those
  > weights Google's faces are declared **later** than shared `fonts.css`'s
  > and keep winning. That is the whole of the non-event, and it is now
  > *measured* rather than assumed.
  >
  > What changes is **700**. Before the link, a 700 request had no exact face
  > and CSS font matching fell to Google's closest available, **600**; after
  > it, shared `fonts.css:13`/`:21`'s `font-weight: 100 900` InterVariable
  > face matches 700 **exactly** and therefore wins over any closest-match, so
  > those glyphs render in the self-hosted variable face at true 700 instead
  > of Google's static 600. Exactly **one live consumer** exists:
  > `web/todo/js/todo-utils.js:950`'s `prepend="<strong><b>"` in
  > `renderItem()`, applied to periodic items that are due now — on
  > `index.html`'s main list, i.e. **inside C6b's pixel window**. No CSS rule
  > reaches it; `<strong>`/`<b>`'s UA `bold` is the only requester of 700 on
  > either page.
  >
  > This is recorded as **D-19** in §5 Step 6.2a's D-table so that **B4.11**'s
  > "exactly D-8…D-14 and nothing else" has somewhere to attribute it — the
  > alternative is a correct, predicted, one-element delta that stops C6b.
  > It is the plan's only **data-dependent** D-row: whether it appears in a
  > given capture depends on whether the seeded fixture has a periodic item
  > that is due, which is why it is stated as a *predicted best-effort* delta
  > and why B4.11 asserts its **absence** as acceptable and its presence as
  > attributable, rather than requiring it.
  >
  > **This is why `compare.html` gets no shared link** (§4.8): it renders no
  > `.ui-*` component, so the sheet would be dead weight *and* a font hazard
  > for nothing.
- **`css/todo.css?v=19` → `?v=20`, on both pages** — `index.html:13` and
  `compare.html:13`. *(**v5 FINAL**, Critic finding 3: within C6b the
  `shared.css` insert of the preceding bullet lands **first**, immediately
  before `:8`, so by the time the bump is applied `todo.css`'s link is at
  **`:14`** in `index.html`. `compare.html` gets no shared link, so its link
  stays at `:13`. Both are above C6a's block, so the +5 above does not apply
  to either.)* `todo.css` goes from **619** lines to **570** — the two
  named ranges are 42 (`:103-144`) and 5 (`:615-619`), plus the `:101-102`
  head tidy — and the
  existing `?v=` query is todo's own cache-busting convention (`todo.js?v=4`,
  `todo-utils.js?v=17`, `compare.css?v=1`), so not bumping it serves the
  pre-drawer stylesheet to every returning browser.

  > **User ruling, 2026-09-16.** Asked whether the cache-buster bump should
  > be its own commit or ride along:
  >
  > > "yeah, it can land in the same commit.. that's fine. I'll need to
  > > visually validate each one anyway."
  >
  > Read as: *place* the bump in C6b, alongside the edits that make it
  > necessary — **not** as authorisation to remove the `?v=` mechanism. The
  > mechanism stays; only the number moves, and it moves once.
  >
  > **`compare.html` is therefore on C6b's touch list**, which it was not in
  > v4 (§6's C6b row is corrected). It is the *only* reason that page is
  > touched at C6b — it gains no drawer, no trigger and no shared link — and
  > the bump is needed there because both pages link the **same** file under
  > the **same** query string, so leaving `compare.html` at `?v=19` would
  > have one page of todo pinned to a cached stylesheet the other page has
  > already replaced. Harmless in appearance today (compare.html renders no
  > `.sidebar*` rule), and exactly the kind of divergence that is free to
  > prevent and annoying to diagnose later.

**6.2a — todo's drawer D-table.** Architect asked for this and v1 omitted it;
obsidianoid got one and todo did not, which is why todo's drawer migration was
the least-evidenced part of the plan. Every declaration being deleted, and
where it goes. `D` = deliberate delta.

| todo property (`todo.css`) | value | `.ui-menu-*` disposition |
|---|---|---|
| `.sidebar` `position` `:104` | `fixed` | same |
| `top` `:105` / `height` `:108` | `52px` / `calc(100vh - 52px)` | **D-8** — the shared drawer is full-height (`top: 0`), so it spans under todo's 52px topbar instead of starting below it. Visible change at the top edge |
| `left` `:106` | `0` | same |
| `width` `:107` | `240px` | same — `components.css` uses `240px` |
| `background` `:109` | `var(--surface)` | **D-9** — becomes `var(--color-surface-1)`. todo's `--surface` is `#161b22` (dark) / `#ffffff` (light); shared `--color-surface-1` on the `dark` theme is a different hex. The drawer stops matching todo's own panels exactly. This is the *one* place todo's two-vocabulary split (6.3's last bullet) becomes visible, and it is the price of not re-skinning todo this phase |
| `border-right` `:110` | `1px solid var(--border)` | same shape, `var(--color-border)` |
| `padding` `:111` | `1rem .875rem` | **D-10** — shared uses `var(--space-4) var(--space-3)` = `1rem 0.75rem`. Horizontal padding narrows by 2px a side |
| `display`/`flex-direction`/`gap`/`overflow-y` `:112-115` | `flex` / `column` / `0` / `auto` | same |
| `z-index` `:116` | `200` | same |
| `transform` `:117` + `.sidebar-open` `:122` | `translateX(-100%)` → `translateX(0)` | same mechanism, driven by `.ui-menu-drawer[data-open]` instead of `.sidebar-open` |
| `transition` `:118` | `transform 220ms ease, background 150ms ease, border-color 150ms ease` | **D-11** — shared uses `transform var(--transition)` = `160ms cubic-bezier(0.16, 1, 0.3, 1)` (`tokens.css:40`). The slide gets **60ms faster** with a different easing curve, and the `background`/`border-color` transitions are dropped, so a theme switch while the drawer is open recolours it instantly rather than easing |
| `box-shadow` `:119` | `4px 0 24px rgba(0,0,0,.35)` | **D-12** — becomes `var(--shadow-md)` = `0 8px 32px rgba(0,0,0,0.4)`. The shadow stops being directional (rightward) and becomes downward — the most visible single delta in C6b, and the reason B4.11 captures a drawer-open shot rather than trusting the D-table |
| `.sidebar-section` `:124` | `padding: .5rem 0` | `.ui-menu-section`, `var(--space-2) 0` = same `0.5rem` |
| `.sidebar-divider` `:126-130` | `height:1px; background:var(--border); margin:.375rem 0` | `.ui-menu-divider`; margin becomes `var(--space-1) 0` = `0.25rem`, **D-13** (−2px a side) |
| `.sidebar-backdrop` `:134-143` + `.sidebar-backdrop.sidebar-open` `:144` | `display:none`→`block`, `inset:0; top:52px; background:rgba(0,0,0,.45); z-index:199; backdrop-filter:blur(1px)` + `-webkit-` | `.ui-menu-backdrop`. `top:52px` → `0` (follows D-8); `rgba(0,0,0,.45)` → `var(--overlay-scrim)`, **D-14** — on `dark` that is `rgba(0, 0, 0, 0.55)` (`tokens.css:42`), i.e. 10 points more opaque; on `light` the theme overrides it (one of `THEME_ALLOW`'s three permitted non-colour props, `check-shared-css.mjs:96`) to light's own ink at 0.35, so the light-theme scrim gets **lighter** while dark's gets darker. Both directions are intended; B4.11 captures both themes for this reason. `blur(1px)` **preserved**, both prefixes, because dropping it is a visible change with no upside |
| `@media (max-width:640px) { .sidebar { width:85vw } }` `:617-619` | `85vw` | shared media query, same breakpoint and value |

**Two further todo D-rows, neither of them the drawer's.** *(New in
**v5 FINAL**. They live in this table because it is todo's only D-table and
D-numbers must have exactly one home; each names its own commit, because
neither is caused by the drawer edits above.)*

| todo delta | commit | disposition |
|---|---|---|
| **D-18 — first-visit theme under `prefers-color-scheme: no-preference` flips `dark` → `light`.** *Behavioural, and the reason the "none behavioural" claim below is qualified rather than restated.* `web/todo/js/theme.js:15` reads `window.matchMedia('(prefers-color-scheme: light)').matches ? 'light' : 'dark'` — it asks about **light** and defaults to **dark**, so a profile expressing no preference gets dark. `ThemeManager`'s `system` step asks the opposite question, `(prefers-color-scheme: dark)`, and defaults to **light** (§5 Step 3.1) | **C6a** (Step 6.3's `theme.js` deletion) | **Accepted, declared, and deliberately not papered over.** The two OS cases that are *expressed* — `light` and `dark` — resolve identically before and after; only `no-preference` moves, and it moves because the shared resolver's polarity is the shared resolver's, used by eight modules, not todo's to invert. It is invisible to any user who has ever toggled the theme (storage wins at step 1) and invisible on any profile that expresses a preference, which is nearly all of them: `no-preference` is what a headless or freshly-provisioned profile reports. It is nonetheless a **first-paint behaviour change on a real page**, so it gets a row rather than a footnote, and it is the one todo delta that is not a pixel measurement — a capture taken under either explicit OS setting will not show it |
| **D-19 — glyphs at weight 700 re-face from Google's closest-match 600 to shared `fonts.css`'s exact-match InterVariable.** Derivation, consumer and why it cannot be designed away: §5 Step 6.2's option-3 residual note | **C6b** (Step 6.2's `shared.css` link) | **Predicted, best-effort, data-dependent.** The single consumer is `web/todo/js/todo-utils.js:950`'s `<strong><b>` on periodic-and-due items, so whether it appears in a capture depends on the fixture's clock and data. **B4.11** therefore treats its presence as attributable and its absence as acceptable — the only D-row in the plan with that shape, and it is stated here so that shape is a decision rather than a loophole |

Nine todo D-rows in all. The seven drawer deltas (**D-8…D-14**) are cosmetic
and none of them is behavioural; **D-18** is behavioural and belongs to a
different commit; **D-19** is cosmetic and data-dependent. *(**v5 FINAL**,
Critic's unconditional-approve row: v5 wrote "Seven deltas (D-8…D-14), all
cosmetic, none behavioural" over a step whose sibling deleted `theme.js` and
changed what a first visit renders. The claim was true of the drawer and false
of the commit pair, and a reader checking "none behavioural" against C6a would
have found the counter-example the plan itself states at Step 6.3.)* The two a
reviewer should actually look at are **D-8** (drawer now spans the topbar) and
**D-12** (shadow direction), which is what B4.11's drawer-open capture exists
to catch.

> **All 20 line citations in this table were shifted +1 in v3** (Architect
> N2). v2 derived them from its own wrong deletion range `:102-143`; the
> block's true extent is `:103-144`, so every declaration sat one line lower
> than v2 said. Re-derived by printing `todo.css:99-146` with explicit
> offsets, declaration by declaration. `:617-619` is the one citation that was
> already right, because it comes from a different part of the file and was
> never touched by v2's "correction". **§4.4**'s drawer inventory
> (`.sidebar` `:103-120`, `.sidebar.sidebar-open` `:122`, `.sidebar-section`
> `:124`, `.sidebar-divider` `:126-130`, `.sidebar-backdrop` `:134-144`) and
> §13(b)'s survivor row
> (`:103-144`) were **both** already correct — which is precisely why the
> defect was findable: three places stated the same fact and one of them
> disagreed. *(**v4** — two corrections to this note itself: it said "§4.2",
> but the drawer inventory is in **§4.4**; and it listed four rules where the
> block holds six, because `.sidebar-section` `:124` was absent from §4.4.
> Every citation it quotes is still right — the omission was of a rule, not a
> misstatement of a line. Critic minor, Architect A9.)*
- All nine controls are re-registered through `render` slots and items — the
  one-for-one table is §7's B4.2. The three `<select>`s and four checkboxes
  keep their existing `id`s and their existing inline `onchange` attributes,
  because a `render` slot mounts real DOM into the live document (Decision 2,
  option 2A): `applyColConfig()` (`index.html:52-60`) and
  `loadColConfig()` (`:72-76`) continue to find them by `id` with **zero**
  changes to `js/todo.js` or `js/todo-utils.js`.

**6.3 — The theme system is replaced.**

- `web/todo/js/theme.js` (27 lines) is **deleted.** Surviving definition:
  `ThemeManager` with `module: "todo"`.
- **Key handling — `storageKey`, not a migration.** todo persists under the
  bare key `'todo-theme'` (`theme.js:2`); `ThemeManager`'s default would be
  `ui-theme:todo`. v1 specified a read-write-delete migration. **Corrected:
  FRD `:253` provides `storageKey` precisely for this** ("localStorage under
  `ui-theme:<module>` **unless `storageKey` overrides**"), so C6a passes
  `storageKey: "todo-theme"` and **no migration code exists**. Rationale: a
  migration is a one-shot side effect that is unobservable after it runs,
  cannot be re-tested without hand-editing `localStorage`, and silently does
  nothing on a fresh profile — it is untestable by B-criteria, whereas
  `storageKey` is a one-line assertion. It also cannot lose a user's
  preference, because nothing is read or deleted. *(This is the opposite
  choice from obsidianoid's, and deliberately so: obsidianoid's key
  `obsidianoid-theme-${idx}` holds the **value** `dark`, which FRD `:191`
  renames to `obsidian` — a value migration that `storageKey` cannot express.
  todo's values are `dark`/`light`, both unchanged in the shared roster, so
  todo needs no value translation at all.)*
- **Pre-paint is preserved** by a *tiny inline* bootstrap in `<head>`
  (FRD `:246-249` explicitly sanctions this shape: "*a small inline bootstrap
  or head-loaded script — todo's `theme.js` pattern, generalized*"), replacing
  the `<script src="js/theme.js">` at `index.html:20` and `compare.html:18`. It
  reads the resolved name and stamps `data-theme` **before first paint**; the
  full `ThemeManager` takes over from `shell.ts`.
  *(**v4** — this read "before the stylesheet parses", which is false and was
  load-bearing nowhere but still wrong: `todo.css` is linked at
  `index.html:13` and `compare.html:13`, seven lines **above** the script slot
  at `:20`/`:18`, so the stylesheet has already been requested and parsed by
  the time the bootstrap runs. What the bootstrap beats is **paint**, not
  parse — the attribute lands while the parser is still inside `<head>`, so
  the first paint already carries the resolved palette. Critic minor; the same
  error produced BX.10's broken extraction rule, corrected there too.)*
  `index.html:2` and `compare.html:2` keep their static `data-theme="dark"`
  as FOUC insurance (FRD `:249`).

  **Why an inline bootstrap is structurally required, not merely preferred.**
  `theme.js` is a *classic, non-deferred* `<script>` in `<head>`, so its
  `applyTheme(getPreferred())` call at `:19` runs during parse, before first
  paint. `shell.js` ships as `type="module"`, which is **deferred by
  specification** — it cannot run before first paint. So replacing `theme.js`
  with `shell.js` alone converts a correct pre-paint stamp into a guaranteed
  flash of the markup default on every load where the stored theme is
  `light`. The inline bootstrap is the only thing that can occupy that slot,
  which is why FRD `:246-249` names the shape. It duplicates ~4 lines of
  resolution logic with `ThemeManager`; that duplication is **declared**
  (ledger row 16, and §13(b) records it as a sanctioned second definition)
  rather than being an accidental violation of the one-definition rule —
  the alternative is a visible regression on both pages.
- `#theme-toggle` **stays** as a topbar quick-toggle on **both** pages
  (`index.html:179-181`, `compare.html:41-43`), now driven by `ThemeManager` —
  FRD `:300` allows the quick button to remain "as a bonus, backed by the same
  ThemeManager". Three sub-edits per page, and v1 named only the first:
  1. the inline `onclick="toggleTheme()"` attribute is **removed** and
     replaced by a listener bound in `shell.ts`. v1 left this attribute in
     place while deleting the `window.toggleTheme` that `theme.js:21` defines
     — which would leave a live button calling an undefined global on both
     pages. *(Architect B5 / Critic M-1.)*
  2. the `#theme-icon` FontAwesome class swap from `theme.js:6-9`
     (`fas fa-moon` ↔ `fas fa-sun`) moves into `shell.ts` as the
     `ThemeManager` `onChange` callback — this is the concrete consumer that
     justifies Decision 1A's `onChange()` config. **But `onChange` alone is
     not sufficient, and v1 implied it was.** `theme.js` sets the attribute
     *and* the icon class in the same pre-paint call (`:5` and `:8` inside
     one `applyTheme`); `shell.ts` is deferred, so an `onChange`-only
     implementation stamps the palette pre-paint and the icon after, leaving
     the icon as the visibly flashing element — the same defect as before,
     relocated. So the **initial** icon class is set by the inline bootstrap,
     in the same statement that stamps the attribute, and `onChange` owns
     only *subsequent* swaps. Both pages define the handle
     (`index.html:180`, `compare.html:42`), so the bootstrap can assume it.
  3. `<script src="js/theme.js">` (`index.html:20`, `compare.html:18`) is
     replaced by the inline pre-paint bootstrap plus the `shell.js` module tag.
     **The bootstrap is written wrapped in a sentinel comment pair** —
     `<!-- theme-bootstrap:start -->` above it and
     `<!-- theme-bootstrap:end -->` below it, on both pages — because
     **BX.10** extracts the block by those sentinels and uses
     `grep -rl 'theme-bootstrap:start' web/ --include='*.html'` as its
     detector that no third copy has appeared. *(**v4** — BX.10 previously
     tried to locate the block positionally and could not; both todo pages
     already carry an unrelated inline src-less `<script>` in `<head>`
     (`index.html:21-164`, `compare.html:20-27`), so the sentinel is what
     makes the block addressable and the copy set enumerable. Critic F-1.)*

- **The drawer's theme picker ships `dark` and `light` only — and this is a
  correctness constraint, not a taste call.** v1 wrote that the drawer "carries
  the standard `themePicker` section, so 8 themes become selectable where 2
  were — sanctioned delta", and flagged the alternative to Architect as
  "ship all 8 and accept mixed chrome". **That framing is wrong and is
  corrected here.** `web/todo/css/todo.css` declares its 13 bare tokens in
  exactly **two** blocks — `[data-theme="dark"]` `:3-17` and
  `[data-theme="light"]` `:19-33` — and there are **74** `var(--…)`
  consumption sites across the sheet. Stamping any of the other six shared
  names on `<html>` matches **neither** block, so all 74 resolve to their
  guaranteed-invalid initial value: todo's backgrounds, borders, text and
  accents all drop out at once. The `!important` winner-row contrast fix at
  `:414-427` is keyed to the same two selectors and stops applying too. The
  outcome is not "mixed chrome" — it is an unstyled page. So the picker is
  restricted to `{dark, light}` for this phase (B4.10), the restriction is
  enforced in `shell.ts` rather than left to the shared default, and widening
  it is gated on todo adopting the shared vocabulary (§11 item 1). No Architect
  decision is needed; the tree decides this one.
- `todo.css:3-17` and `:19-33`'s 13 bare token names are **not** renamed in
  this phase. todo keeps its own vocabulary; `shared.css` is linked for the
  component styles only — that link is **written by §5 Step 6.2** (immediately
  before `index.html:8`, so `todo.css` keeps precedence and the Google-Fonts
  group keeps the last `@font-face` word) and asserted by **B4.13**; before v5
  no step wrote it, and §4.8 asserted it as already true. **This is a declared
  duplication**, not an
  oversight: mapping 13 bare names onto the shared 17 across a 619-line sheet
  with **12** `!important` declarations (`:359`, `:361`, `:365`, `:367`,
  `:373`, `:416`, `:419`, `:424`, `:427`, `:454`, `:455`, `:480` — v1 said 6
  and cited three ranges that hold 8 of them, missing `:373`, `:454`, `:455`
  and `:480` entirely) is a re-skin of todo, not an adoption of three
  components, and
  it would put a second definition of every colour in play mid-phase. §11
  item 1. **Consequence to state plainly:** todo's own chrome stays on
  `--bg`/`--surface`/`--accent` while the drawer, toasts and dialogs render
  from `--color-*`. Because both vocabularies' `dark`/`light` values were
  donated from *this file* (`web/shared/css/themes.css:12-14` and `:35-37`
  name todo `:3-17` and `:19-33` as donor of record for dark and light, 12 of
  17 keys each, the other 5 authored), the two agree closely on those two
  themes and diverge on the other six — which is exactly why B4.10 restricts
  todo's shipped picker to `dark` and `light` for this phase. **No longer
  flagged for Architect:** v1 offered "ship all 8 and accept mixed chrome" as
  the alternative, and the bullet above shows there is no such alternative —
  the other six names match no block in `todo.css` and take all 74 `var()`
  sites down with them. The restriction is forced, so there is nothing to
  decide.

**6.4 — `shell.ts`'s contents**, in full, so each commit's surface is bounded.

*Landing in **C6a**:*
- `ThemeManager` construction with `module: "todo"`, `storageKey:
  "todo-theme"`, `themes: ["dark", "light"]`, and an `onChange` that performs
  the `#theme-icon` class swap;
- the `#theme-toggle` listener binding (both pages);
- the `if (document.getElementById("menu-toggle"))` guard, with `initDrawer`
  present as a no-op.

*Landing in **C6b**:*
- `initDrawer`'s body — the `HamburgerMenu` construction with the nine
  `render`/item registrations.

**Not landing at all — `window.showToast` is not exposed.** v1 offered it as
optional forward wiring for §11 item 6. **Architect answered NO and the answer
is adopted**, on the evidence in §4.4: **zero** legacy todo `.js` files call
`showToast` today, so the global would have no caller in this phase — it would
be a new permanent entry in the global namespace, justified entirely by a
follow-up item, and G14's neighbourhood is exactly where this phase is
*removing* ambient globals rather than adding them. Recorded as the governing
rule for the future case, in Architect's own terms: **if legacy todo JS ever
needs a boundary, it is one deliberate façade (`window.todoShell`) applied to
all legacy call sites at once, or nothing** — never a drip of individual
functions onto `window`. That decision belongs to §11 item 6, not here.

**6.5 — Escape now closes the drawer** *(C6b)*. `index.html:149-151`'s handler
is left alone (it closes the three modals); `HamburgerMenu` installs its own.
This is net-new behaviour, sanctioned. *(`:149-151` is a **pre-C6a**
coordinate, like every `index.html:NN` citation in Step 6.2 — read it as
`:154-156` in the C6b tree, per the shift declared once at the top of Step 6.2.)*

**6.6 — Pixel evidence.** Split with the commits, which is half the reason for
splitting them:

- **C6a** — dark and light, drawer **closed**, both pages. The drawer's own
  markup and CSS are untouched by C6a, so the expected result is
  **pixel-identical except the `#theme-icon` glyph**, and any other diff is a
  defect rather than a judgement call. `compare.html` gets its own pair of
  shots, since C6a is the only commit that touches it.
  *(**v5 FINAL.** "dark and light" is not a stylistic choice here: both shots
  must be taken with the OS preference **explicitly** set, because **D-18** —
  the `no-preference` polarity flip — would otherwise land inside this
  window and make the "pixel-identical" claim false for a reason the harness
  could not distinguish from a defect. Under an explicit `light` or `dark`
  preference D-18 does not fire at all, so pinning the setting is what makes
  this bullet's exactness real rather than conditional. `tools/baseline-shots`
  drives the preference, so this is a harness argument, not a hope.)*
- **C6b** — dark and light, drawer **open** and closed, against the *C6a*
  tree. Expected diffs are exactly D-8…D-14, **plus D-19 if and only if the
  fixture carries a periodic item that is due** (§5 Step 6.2a; its absence is
  equally acceptable), and nothing else. The winner-row
  section (`:411-427`, whose four rules are `:414-427`) must be byte-untouched
  in CSS and unchanged in pixels in both themes — it is the one place in todo
  where an `!important` background fights the token system, so a drawer change
  that perturbed it would be a real regression.

**Acceptance — C6a:** B1.1, B1.2, B3.8, B4.12, B7.1–B7.5, B9.1, B9.2, B9.4,
BX.4–BX.7, BX.9, BX.10, BX.11. Artifact count **16**.

> *v3 (Critic C-1): three corrections here.* **B3.9** and **B9.3** are
> removed — both are obsidianoid-only (`B3.9` is per-vault persistence;
> `B9.3` is the `EISDIR` fix for `out: "web/obsidianoid/js/"`, and todo's
> descriptor is `mode: "bundle"` with `out: "web/todo/js/shell.js"`, a file
> path, so the directory hazard cannot recur here) and their labels say
> `→ C5`. **B3.8, B7.5, BX.7, BX.9 and BX.10** are added — each labels C6a
> and no cell scheduled it. **B9.4** stays, and its label is widened to say
> so.

**Acceptance — C6b:** B1.1, B1.2, B4.2, B4.10, B4.11, **B4.13**, BX.4, BX.6. Artifact
count stays **16**.

### Step 7 → C7: FR-9 dead-weight deletion in todo

**7.1 —** Delete the **40 verified-dead files** enumerated by §4.4: 8 HTML
stubs (`404.html`, `50x.html`, `base.html`, `exercise.html`,
`menuserver.html`, `onoff.html`, `slideshow.html`, `todo.html`), 17 CSS
(`arrow.css`, `base.css`, `buttons.css`, `fontawesome.min.css`,
`jquery-ui.css`, `kbc.css`, `modal.css`, `nav.css`, `panels.css`,
`progress.css`, `progressbar-base.css`, `pshelper-index.css`,
`responsivenav.css`, `slideshow.css`, `style-base-tr.css`, `style-base.css`,
`toggle.css`), 12 JS (`axios.min.js`, `base-util.js`, `bootstrap.js`,
`exercise.js`, `kbc.js`, `marked.min.js`, `menuserver.js`, `navcontrols.js`,
`onoff.js`, `panels.js`, `popper.js`, `slideshow.js`), 3 images
(`divider.png`, `logo.svg`, `user.png`). This includes the third dead
hamburger (`base.html` `#rightToggle` + `panels.js` + `panels.css`) and the
fourth (`menuserver.js:266-268` + `navcontrols.js`).

**7.2 — The 11 pshelper files that are still live are NOT deleted**:
`css/fontawsme.css`, `css/fontawall.css`, `js/jquery.js`, `js/jquery-ui.js`,
`js/utils.js`, `images/fvw.png`, and the 5 webfonts. FR-9's "delete the
legacy pshelper tree" is therefore satisfied **partially and deliberately**;
ledger row 3.

**7.3 — Deletion proof, run at the commit boundary** (two-sided, G5):

```sh
# Two-sided, and the accumulator is the point — see below.
hits=0
for f in <the 40 paths relative to web/todo/>; do
  # Match the last two path segments (e.g. "js/onoff.js"), not the bare
  # basename: "utils.js" is a substring of the LIVE "todo-utils.js", and a
  # basename grep would report a phantom reference and block a correct delete.
  if grep -rn -e "$f" web/todo/index.html web/todo/compare.html; then
    hits=$((hits + 1))
    echo "STILL REFERENCED: $f"
  else
    rc=$?
    [ "$rc" -eq 1 ] || { echo "grep error rc=$rc on $f"; exit 2; }
  fi
done
[ "$hits" -eq 0 ]                 # no live page names any of the 40

grep -rn 'base\.html' web/ internal/ scripts/ cmd/; rc=$?; [ "$rc" -eq 1 ]
```

**Two defects in v1's form, both fixed above — this is B8.2.**

1. **The loop had no accumulator.** v1 wrote `done; rc=$?`, which captures
   the status of the **last iteration only**. If the 1st of the 40 files were
   still referenced (`grep` rc=0) and the 40th were not (rc=1), `rc` would be
   1 and the proof would pass while a live file was deleted. `hits` makes the
   check hold over the whole set. *(Critic — this is the one that could have
   shipped a broken todo.)*
2. **Basename matching was too loose in one direction.** `utils.js` is live
   (`index.html:17`) and is a substring of nothing in the delete set, but
   `todo-utils.js` (also live, `:18`) *contains* `utils.js`, and
   `responsivenav.css` contains `nav.css` — both members of the 40. A
   basename grep therefore reports references that do not exist and blocks a
   correct deletion. Matching `<dir>/<file>` is precise against how the
   pages actually spell their references (`src="js/…"`, `href="css/…"`).

The **guarded**-`grep` form remains mandatory (Phase-1 AX.5): `! grep` maps
exit **2** (a real grep error — unreadable file, bad pattern) onto "success",
so a mistyped path would read as proof of absence. The `rc -eq 1` check in
the `else` branch is what distinguishes "no match" from "grep broke".

**7.4 —** `web/menuserver/` and `web/slideshow/` keep their copies; FRD
`:387-390` assigns menuserver's to Phase 5 and slideshow's deletion is
out of this phase's module scope. Ledger row 3.

**Acceptance (C7):** B1.1, B1.2, B8.1–B8.4, BX.4, BX.6. Artifact count 16 (no artifact deleted —
none of the 40 files is in the generated list).

### Step 8 → C8: Q8(a) — the narrow unauthenticated allowlist

**8.1 —** `internal/platform/auth/gate.go` gains a fourth carve-out beside
`:129` (`/healthz`), `:135` (`/api/auth/mode`) and `:148`
(`/api/auth/whoami`), before `protected` is computed at `:157`. It admits
**exactly three** path shapes, all GET-only:

| # | Shape | Match | Q9 half |
|---|-------|-------|---------|
| 1 | `/shared/dist/shared.css` | exact | settled yes since v1 |
| 2 | `/shared/public/fonts/…/*.woff2` | prefix + suffix | settled yes since v1 |
| 3 | `/shared/dist/shared.mjs` | exact | **added in v5** — user ruling, 2026-09-16 |

> ***Shape 3 is new in v5.*** Every version through v4 excluded it and said so
> — "Q9's `.woff2` half is settled yes; the `.mjs` half is **not** and is
> **not** included". The user closed Q9 on 2026-09-16:
>
> > *"yes, the login page should use the theme previously selected."*
>
> The stored theme is read by code that, **from C3 onward**, ships only in the
> barrel, so shape 3 is not an extra convenience — it is the ruling's minimum.
> *(**v5 FINAL**, Critic minor 14: "ships only in the barrel" is a fact about
> the tree **at and after C3**, where `ThemeManager` is added to
> `web/shared/ts/theme.ts` and reaches a browser only through
> `/shared/dist/shared.mjs`. Before C3 the statement is false — todo's
> `js/theme.js` reads the same class of value from its own file. C8 is five
> boundaries after C3, so the claim holds where it is used; it was the tense
> that was wrong, and the tense is what a reader checks it in. The one
> non-barrel copy that does survive C3 — todo's inline pre-paint bootstrap,
> ledger row 16 — is per-page markup on todo's two pages and is not available
> to a login template, so it is not an alternative route to the same
> capability.)* It is an **exact**
> match, not a prefix: `/shared/dist/` does not become readable, only that one
> file, which is why B6.4 keeps a 401 probe for `/shared/ts/theme.ts` and
> gains one for `POST` on shape 3. §9 Q9 and ledger row 20 record the change;
> §11 item 11 records what the ruling does **not** authorise (restyling the
> login page, which stays Phase 3+).
>
> *Guaranteed by (criteria): B6.1 asserts shape 1, B6.2 shape 2, B6.3 shape 3,
> B6.4 the two exclusions, B6.5 the no-regression side.*

**8.2 —** The allowlist delegates to the same `static.SharedHandler` the
dispatcher already uses (`cmd/server/main.go:326`), so path confinement,
method handling and content-type behaviour have exactly one definition. This
is what makes shape 3 cheap: `SharedHandler` already confines to `dist/` and
`public/` (`internal/platform/static/shared.go:57`), already refuses dotfile
segments (`:65`), already refuses anything but GET/HEAD (`:40`), and already
serves `.mjs` as `text/javascript` through `http.ServeContent` (`:91`), under
a Phase-1 test of record (`internal/platform/static/shared_test.go:232-244`).
C8 adds a gate bypass, not a second static server.

Two precisions on that delegation, both pre-existing and both deliberate:

- **The 401s in probes 5 and 6 come from the gate, not the handler.** The
  carve-out condition is `r.Method == http.MethodGet && <one of the three
  shapes>`, exactly like the three carve-outs above it. A `POST` fails that
  condition, so it never reaches `SharedHandler` by this route; it falls
  through to the `protected` check at `:157` and 401s there. `SharedHandler`'s
  own `:40` guard answers **405**, which is the right answer for an
  *authenticated* POST and is not what probes 5 and 6 measure. The handler
  guard is defence in depth behind the gate, not the source of the expectation.
- **GET-only means `HEAD` on a carved path also 401s** when unauthenticated,
  because `http.MethodHead` is not in the condition. That is intentional
  narrowness carried unchanged from v1: no stylesheet `<link>`, font fetch or
  `import` issues a HEAD, so widening the condition would buy nothing and cost
  the "exactly three shapes, exactly one method" property that makes R15
  reviewable. Recorded here so it reads as a decision rather than an oversight.

**8.3 — Two-sided probes** in `cmd/server/dispatcher_auth_test.go`, on a
protected module with no session:

| Probe | Request | Expected | Criterion |
|-------|---------|----------|-----------|
| 1 | `GET /shared/dist/shared.css` | 200, `Content-Type: text/css` | B6.1 |
| 2 | `GET /shared/public/fonts/inter/<a real face>.woff2` | 200 | B6.2 |
| 3 | `GET /shared/dist/shared.mjs` | **200**, `Content-Type` prefix `text/javascript` | B6.3 |
| 4 | `GET /shared/ts/theme.ts` | 401 | B6.4 |
| 5 | `POST /shared/dist/shared.css` | 401 | B6.4 |
| 6 | `POST /shared/dist/shared.mjs` | 401 | B6.4 |
| 7 | `GET /shared/public/fonts/inter/OFL.txt` | **401** | B6.4 |

…and the same **seven** paths **with** a session behave exactly as they do
before C8 (B6.5). Probe 3's expectation is the one row v5 changed — it read
**401** through v4. Probe 6 is net-new in v5 and exists precisely because
probe 3 flipped: without it, nothing would catch a carve-out written as a path
match that forgot the method guard. `goleak` stays as
`dispatcher_auth_test.go` already has it.

> **Probe 7 is new in v5 FINAL, and it is the probe that makes shape 2 a
> shape.** *(Critic finding 5. The pre-mortem's Scenario 3 at §8.5 already
> pre-committed to this probe — "an `OFL.txt` → 401 probe … is part of B6.4
> from the start" — and it was not: B6.4 had three probes and this table had
> six, none of them `OFL.txt`. A pre-mortem that names a guard the plan does
> not contain is worse than one that names none, because it retires the risk
> in the reader's mind.)*
>
> The defect it catches: shape 2 is specified as **prefix + suffix**
> (`/shared/public/fonts/` … `.woff2`), but the three carve-outs it sits
> beside are written as plain matches, and the cheapest way to write a fourth
> is `strings.HasPrefix(r.URL.Path, "/shared/public/")` — dropping the suffix
> half. That implementation would **pass every criterion in §7**: probes 1–6
> all still answer as specified (none of them is under `/shared/public/`
> except probe 2, which the over-wide form still serves 200), so the gate
> would read green while the entire `public/` subtree had become
> unauthenticated. `web/shared/public/fonts/inter/OFL.txt` is a real,
> committed, non-`.woff2` file under exactly that prefix, so it is the
> minimum witness that distinguishes the two implementations — P-III's rule
> that a gate which cannot fail is not a gate, applied to the one criterion
> whose failure mode is an over-wide match rather than a missing one.

**Acceptance (C8):** B1.1, B1.2, B6.1–B6.5, BX.4, BX.5, BX.6. Artifact count
16. *(v3: BX.5 is restored here — it is the full-phase run of record, the
only boundary whose `<head>` covers all nine commits, and v2's C8 cell had
dropped it while C5's and C6a's had kept it.)*

---

## §6 — Commit sequence

**9 commits** (v2: v1's C6 splits into C6a + C6b, ADR-016), one per step,
pixels last, auth last of all. Each names every path it touches; P2's gate is
`node scripts/gates/clean-tree.mjs <those paths>`, which leaves the P3
side-cars alone. **`Makefile` appears in three cells** (C1, C5, C6a) because
G16 requires the flag the plan cites to be in the recipe; **`compare.html`
appears in exactly one** (C6a), because that is the only commit that touches
it.

> **v3 — the gate column is now derived from the labels, not written
> alongside them (Critic C-1).** Through v2 this table's "Gates at this
> boundary" cells and §7's `[deferred → Cn]` labels were two independent
> statements of one fact, and they had drifted at **every one of the nine
> boundaries**. Measured against the labels as they now stand, v2's cells
> **under-scheduled 19 distinct criteria** — B1.1, B1.2, B1.3, B1.4, B2.9,
> B3.8, B3.10, B4.2, B7.5, B10.3, B10.4, B10.5, BX.4, BX.5, BX.6, BX.7,
> BX.8, BX.9, BX.10 — and **over-scheduled 4**: B3.8 at C3, B3.9 at C6a,
> B9.3 at C6a, BX.7 at C1. B3.8 and BX.7 are in both lists, which is the
> signature of the underlying error — each was scheduled at the wrong
> boundary and absent from its right one. Two were worse than misplaced:
> **B1.3** was labelled `→ C2` and appeared in no cell, and **B10.5** had
> neither a label nor a cell, so both were criteria with **no boundary
> anywhere** — stated, agreed, and unrunnable. Six carried no label at all
> (B1.1, B1.2, B2.7, B10.5, BX.1, BX.3) while four of those six were being
> enforced by a cell, which is the same fact-with-two-owners problem pointing
> the other way.
>
> A labelled criterion that no cell schedules is a criterion that never runs
> — the same defect class as G16's never-passed flag, one level up — and the
> reason it survived two reviews is that **§13 audited names, definitions and
> counts but never scheduling**. So three things changed together and the
> third is the one that matters: the cells below were re-derived from §7's
> labels; the labels §6 had been enforcing tacitly are now written down
> (B10.5, BX.1, BX.3 gained one; B9.4 and BX.5 widened; B1.1, B1.2, B2.7
> gained one earlier in the v3 pass); and **§13 gained clause (d2)**, which
> compares the two directions mechanically and is run below. Per the
> inherited rule *one definition per fact*, §7's label is the definition and
> this column is its derivation; when they disagree, §7 wins and (d2) fails.

| | Commit | Touches | Gates at this boundary | Artifacts |
|---|---|---|---|---|
| **C1** | make three gates real **+ land T1's 18th key** | **`web/shared/css/themes.css`**, **`web/shared/dist/shared.css`**, `web/sampler/js/main.ts`, `web/sampler/js/bundle.js`, `scripts/check-shared-css.mjs`, `scripts/gates/bundle-shape.mjs`, **`Makefile`**, `docs/sampler-checklist.md` | `make check`; B1.1, B1.2, B9.1, B10.1, B10.2, **B10.6**, BX.1, BX.4, BX.6, BX.8, BX.11 | 15 |
| **C2** | Toast | `web/shared/ts/{toast,toast.test,index}.ts`, `web/shared/css/components.css`, `web/shared/dist/{shared.mjs,shared.css}`, `scripts/{check-shared-barrel,check-shared-css,test-web}.mjs`, `web/sampler/{index.html,js/main.ts,js/bundle.js}`, `docs/sampler-checklist.md` | `make check`; B1.1–B1.4, B5.1–B5.7, B10.3, B10.5, BX.2–BX.4, BX.6 | 15 |
| **C3** | ThemeManager | `web/shared/ts/{theme,theme.test,index}.ts`, **new** `web/shared/ts/test-dom.ts` (B3.1's stub), `web/shared/css/components.css`, `web/shared/dist/**`, `scripts/{check-shared-barrel,test-web}.mjs`, `web/sampler/{index.html,js/main.ts,js/bundle.js}`, `docs/sampler-checklist.md` | `make check`; B1.1–B1.3, B3.1–B3.7, B3.10, B10.3, B10.5, BX.2–BX.4, BX.6 | 15 |
| **C4** | HamburgerMenu | `web/shared/ts/{menu,menu.test,index}.ts` + the private focusable helper, `web/shared/ts/modal.ts` (helper extraction only), `web/shared/css/components.css`, `web/shared/dist/**`, `scripts/{check-shared-barrel,test-web}.mjs`, `web/sampler/{index.html,js/main.ts,js/bundle.js}`, `docs/sampler-checklist.md` | `make check`; B1.1–B1.3, B4.1–B4.9, B10.3, B10.4, B10.5, BX.2–BX.4, BX.6, BX.7 | 15 |
| **C5** | obsidianoid adoption **+ `dark`→`obsidian`** (coupled per Q4's *principle*; the rename decision is FRD `:191` — §0) | `web/obsidianoid/{index.html,css/app.css,js/app.ts,js/threads.ts,js/app.js,js/threads.js}`, **delete** `web/obsidianoid/css/themes.css`, `scripts/{descriptors,list-artifacts}.mjs`, **new** `scripts/artifact-paths.mjs`, `scripts/gates/bundle-shape.mjs`, **`Makefile`**, `internal/obsidianoid/{build.go,handler_test.go}` (the only two `"dark"` occurrences in the package — Step 5.4), `unified-webapp.json`, `unified-webapp-example.json`, `local-test/config.json`, `README.md` | `make check` + `make test` + `gofmt -l` + `go vet`; B1.1, B1.2, B2.1–B2.12, B3.9, B4.2, B5.8, B9.1–B9.4, BX.4, BX.5, BX.6, BX.9, BX.11 | 15 |
| **C6a** | todo: build entry + theme system | `web/todo/index.html`, **`web/todo/compare.html`**, **new** `web/todo/js/shell.ts`, **new artifact** `web/todo/js/shell.js`, **delete** `web/todo/js/theme.js`, `scripts/descriptors.mjs`, `tsconfig.json`, **`Makefile`** | `make check`; B1.1, B1.2, B3.8, B4.12, B7.1–B7.5, B9.1, B9.2, B9.4, BX.4–BX.7, BX.9, BX.10, BX.11 | **16** |
| **C6b** | todo: drawer onto `HamburgerMenu` | `web/todo/index.html`, **`web/todo/compare.html`**, `web/todo/js/shell.ts`, `web/todo/js/shell.js`, `web/todo/css/todo.css` | `make check`; B1.1, B1.2, B4.2, B4.10, B4.11, **B4.13**, BX.4, BX.6 | 16 |
| **C7** | FR-9 deletion | **delete** 40 files under `web/todo/` | `make check`; B1.1, B1.2, B8.1–B8.4, BX.4, BX.6 | 16 |
| **C8** | Q8(a) carve-out | `internal/platform/auth/gate.go`, `cmd/server/dispatcher_auth_test.go` | `make test` + `make check` + `gofmt -l` + `go vet`; B1.1, B1.2, B6.1–B6.5, BX.4, BX.5, BX.6 | 16 |

### Ordering, and why every gate is passable at its own boundary

- **C1 first** because clause 12 and the sampler's focusable control assert
  properties of a tree no later commit changes; a gate added after the work
  it should have guarded has never been able to fail. `--require` has a
  second, stronger reason to be here: C5 and C6a are the commits that *add*
  names to its list, so if the flag arrived with them, the boundary that
  introduced a shared consumer would also be the boundary that introduced its
  only check — and the check would have nothing to have caught.
- **C2 before C3 before C4.** `HamburgerMenu`'s `themePicker: true` consumes
  a `ThemeManager` (FRD `:291-293`), so C3 precedes C4. Toast is independent of
  both and goes first because it is the smallest and proves the
  barrel-plus-allowlist-plus-suite rhythm that C3 and C4 repeat.
- **C2–C4 before C5 and C6a/C6b.** Both adopters import all three components.
  C3 additionally owes C5 the `reresolve()` method (Step 5.2, B3.10).
- **C5 before C6a** only for evidence sequencing: C5 is the commit that first
  exercises `bundle-shape.mjs` over a multi-output descriptor, so C6a's new
  single-output descriptor lands into a gate already proven on the harder
  case. There is no code dependency between them.
- **C6a before C6b**, and this is a real dependency, not sequencing: C6b's
  `initDrawer` body lives in the `shell.ts` that C6a creates, and C6b's pixel
  comparison is taken *against the C6a tree* so a drawer regression cannot be
  mistaken for a theming one. C6a is the commit that makes the drawer's
  "pixels unchanged" claim true by construction — it does not touch the
  drawer at all — which is the whole reason for the split.
- **C7 after C6b** so todo's live closure is settled before anything is
  deleted from it. C7 is independent of both C6 halves in both directions —
  none of the 40 files is referenced by todo's pre-C6a pages either.
- **C8 last.** It is the only commit touching the auth surface and the only
  one outside the three named modules' own trees; sequencing it last means a
  revert of the auth posture leaves every UI commit intact.

### Gate composition by boundary

G6 requires every point where composition differs to be named. Three differ.

**Output counts, reconciled once — v1's §6 and §13(c) disagreed and §13(c)
was right.** `bundle-shape.mjs:52` selects descriptors by `sharedConsumer`,
and the number that matters is **derived artifact paths**, not descriptors.
Today: sampler's `bundle.js` + taskmaster's `bundle.js` = **2**. C5 adds
obsidianoid, a **2-entry transpile** → `app.js` + `threads.js` = **+2 → 4**
(v1's §6 said "three", having counted the descriptor as one output). C6a adds
todo's single-entry `shell.js` → **5** (v1's §6 said "four"). So the
trajectory is **2 → 4 → 5**, it is stated here and nowhere else, and it is
the reason `artifact-paths.mjs` has to exist: descriptor-count and
output-count diverge the moment a transpile descriptor joins the set.

| Boundary | `make gates` runs | Artifact gate | Notes |
|---|---|---|---|
| C1 | the four existing scripts | `make web-verify` (15) | `check-shared-css.mjs` now reports **12** clauses; `bundle-shape.mjs` is invoked `--require=sampler,taskmaster` over **2** outputs and is a real gate — it can now fail by name, which it could not before (§4.2.1(a)) |
| C2–C4 | same four | `make web-verify` (15) | `check-shared-barrel.mjs`'s PASS line counts move 7/6 → 8/7 → 9/9. `reresolve()` is a method, not an export, so C3's counts are unaffected by it |
| C5 | same four; `bundle-shape.mjs` now iterates `artifactPaths(d)` over **4** outputs and is invoked `--require=sampler,taskmaster,obsidianoid` | `make web-verify` (15) | the gate script and the descriptor that would have crashed it (`EISDIR`, §4.2.1(b)) change in the **same** commit. `EXPECTED_ARTIFACT_COUNT` does not move: obsidianoid's two outputs were already counted |
| C6a–C8 | same four; **5** outputs, `--require=…,todo` | `make web-verify` (**16**) | `EXPECTED_ARTIFACT_COUNT` 15 → 16 at C6a for `shell.js`. C6b, C7 and C8 add no descriptor and no output |

**No byte-identity exemption exists in Phase 2.** Phase 1's `--allow` was the
mechanism for `package.json`'s `$(date -u)`, retired at Phase-1 C2. If any
boundary here needs `--allow`, that is a finding, not a plan step.

**Revert order.** Adopters before providers: **C6b, then C6a, then C5, must
be reverted before C4, C3 or C2.** Reverting C4/C3/C2 while an adopter stands
leaves it importing a symbol the barrel no longer exports — a build failure at
`check-shared-barrel.mjs`, not a silent break. Two orderings inside that:

- **C6b before C6a.** C6b's drawer code is a body inside the `shell.ts` C6a
  creates; reverting C6a first would delete the file C6b edits.
- **C4 before C3** among the providers (the menu's `themePicker` holds a
  `ThemeManager`), and C3 cannot be reverted while C5 stands for a second
  reason beyond the barrel: obsidianoid calls `reresolve()` from **two**
  sites — `js/app.ts:479` (`fetchVaults`, the initial per-vault resolution)
  and `:494` (`switchVault`) — and constructs the class at `:503`. *(v5: this
  sentence said "`switchVault` calls `reresolve()`", naming one of the two;
  the count matters here because reverting C3 leaves **three** unresolved
  references in `app.ts`, not one — Critic F-B item 3.)*

**C7 and C8 are independently revertible in any order. C1 is not — it is
last-revertable of all nine** *(Architect N4, v3; v2 wrongly listed C1
alongside C7 and C8)*. C1 owns **both halves** of the `--require` mechanism:
the argv parser it adds to `scripts/gates/bundle-shape.mjs` and the
`--require=sampler,taskmaster` it adds to `Makefile:62`. The parser's
unknown-argument branch (`bundle-shape.mjs:42-50`) calls `process.exit(1)` —
verified by running `node scripts/gates/bundle-shape.mjs --require=sampler`
against today's tree, which prints
`bundle-shape: FAIL unknown argument --require=sampler` and exits **1**. So a
revert of C1 that leaves any later `--require=` in the recipe breaks
`make gates` immediately and by name. Two consequences:

- **C5, C6a and any other commit that edits `Makefile:62` must be reverted
  before C1.** After C5 and C6a that line reads
  `--require=sampler,taskmaster,obsidianoid,todo`, so reverting C1's hunk
  either conflicts or (if forced) strips the parser while leaving the flag —
  the exit-1 case above. The full revert order is therefore strict LIFO:
  **C8 or C7 (either, any time) → C6b → C6a → C5 → C4 → C3 → C2 → C1.**
- Reverting C5 or C6a **must** also drop the corresponding name from
  `Makefile:62`, because `--require=` names descriptors that only ever *gain*
  members and a name with no descriptor fails by name. That is the intended
  behaviour — it is what makes the flag a gate — but it means the `Makefile`
  line is part of the revert, not collateral. Recorded in the C1, C5, C6a and
  C6b commit messages.

**If C5 has to defer** — an unresolved pixel diff, a migration concern, or a
non-zero result from Step 5.0's parity pre-check — it defers **past C6a, C6b,
C7 and C8 rather than blocking them.** todo's adoption does not depend on
obsidianoid's, and C6a's descriptor addition is independent of C5's descriptor
flip. Two things move with it: `artifact-paths.mjs` plus the `bundle-shape`
multi-output change migrate to whichever of C5/C6a lands first (a
single-entry `mode: "bundle"` descriptor like todo's does not strictly need
the helper, but the `EISDIR` fix is cheap and the helper is where the
derivation belongs either way), and the `--require` list gains `todo` before
`obsidianoid` instead of after. Stated here so the sequence does not pause on
a judgement call.

---

## §7 — Acceptance criteria

**Labels and the census.** A criterion **without** `[deferred]` was executed
against this tree and its result is recorded with it. A criterion **with**
`[deferred → Cn]` asserts a property of code Phase 2 has not written yet; it
names the boundary where it first runs and states the substitute evidence
gathered in its place. **There are 82 criteria below. 6 were executed against
this tree; 76 carry `[deferred]`, and 31 of those 76 carry executed
substitute evidence** (marked *Executed for the premise / defect / contrast*,
or *Substitute:* — an assertion about today's tree that makes the deferred
criterion falsifiable rather than aspirational). A "substitute" is never
counted as a pass. **§13(a2) is the definition of record for these four
numbers, and §13(a5) asserts that this sentence agrees with it** — the
restatement is hand-typed but machine-checked, so a stale copy fails the
audit instead of being published. They are restated here once for the reader
and nowhere else.

> ***v4 — the numbers above were stale for the third consecutive revision***
> (Critic F-5). They read **75 / 6 / 69 / 26** in v3 while §13's own result
> table — one screen further down the same document, published in the same
> pass — already read **77 / 6 / 71 / 28**, the figures (a2) actually emits.
> v3 added **B2.9** and **BX.11** and updated the table without updating the
> prose. The deeper defect was the claim that followed: "these four numbers
> are **not maintained by hand**". That was false as written — nothing read
> (a2)'s output and wrote it here, and nothing compared the two — which is
> why the same drift recurred in v2, v3 and v4's inbox. Stating "derived, not
> hand-maintained" about a hand-typed number is the one-definition rule
> inverted: it *licenses* the reader to trust the copy. **v4 stops asserting
> the derivation and starts checking it**: §13 gains **(a5)**, which parses
> this paragraph's four integers and fails unless they equal (a2)'s. The
> numbers stay in the prose, where a reader wants them, and the guarantee
> moves from a promise to a command — which is the same correction P-III
> makes to a gate.

> ***v5 — the census moved, and every figure above came out of (a2).*** The
> four numbers went **77 / 6 / 71 / 28 → 82 / 6 / 76 / 31**, and the whole of
> the movement decomposes:
>
> - **Five criteria are new**, all deferred: **B2.10**, **B2.11**, **B2.12**
>   (§5 Step 5.2's three surviving `app.ts` call sites, the first-load
>   `serverDefault()` path on empty `localStorage`, and the `type="module"`
>   post-condition — Critic F-B), **B4.13** (§5 Step 6.2's `shared.css` link,
>   its position and the `?v=` bump — user ruling 4, and a step that had no
>   criterion because v1–v4 had no step) and **B10.6** (the sampler's second
>   copy of Table T1 — user ruling 3, §13(b) duplication 5). Two of the five
>   carry substitutes (B4.13, B10.6); B2.10–B2.12 assert properties of code
>   that exists in no form yet, so they honestly have none.
> - **One inherited criterion gained a substitute: BX.10.** Hardening it for
>   N-3 (`[ -s … ]` non-emptiness) and N-4 (the storage-key-literal detector)
>   meant running both new parts against today's tree, and an executed premise
>   is substitute evidence whether or not it was collected for that purpose.
>   That is the +3 against +5 new: 28 + 2 + 1 = **31**.
> - **The executed count does not move.** It is still exactly the six
>   `[re-asserted → …]` criteria (B1.1, B1.2, B2.7, B10.5, BX.1, BX.3).
>   v5 rewrote B1.1's parts A/B/C wholesale for the 18th token and re-ran
>   them, but a re-run of an already-executed criterion is not a new one.
>
> No integer in this paragraph was typed from arithmetic: the split, the
> five-name list, the substitute delta and the BX.10 finding were all produced
> by diffing (a2)'s parse of this revision against its parse of the v4
> snapshot §13(a4) part 3 keeps as its baseline of record. **(a5) then fails
> the document if this restatement and (a2) disagree**, which is the only
> reason a hand-typed copy is allowed to exist here at all.

> **v2 — what changed in the census, and what v1 got wrong.** v1's **total
> was right**: 65 criteria, 5 executed, 60 deferred. Its **substitute** count
> was not: v1 asserted 20, the Critic counted 21 in v1's own text (§14,
> Critic minor 1), and §13(a)'s clause under-reported besides (Critic M-9).
> Every one of those figures was hand-maintained — precisely the failure mode
> this plan's own first governing rule exists to prevent — so v2 stops
> asserting them and derives all four from the document.
>
> **Ten criteria are new in v2** — B1.4, B2.7, B2.8, B3.10, B4.11, B4.12,
> B7.5, BX.8, BX.9 and **BX.10** — taking the total 65 → **75**. One is
> *executed* rather than deferred: **B2.7**, whose five-block parity
> comparison was actually run at plan time (§4.3's table), which moves the
> executed count 5 → 6. The other nine are deferred (60 → 69), and **six**
> of them carry substitutes: B1.4, B2.8, B4.12, B7.5, BX.8, BX.9. Only
> **B3.10**, **B4.11** and **BX.10** assert properties of code that exists in
> no form yet, so they honestly have none.
>
> **Derivation, run against this revision:** 75 total = 6 executed + 69
> deferred; **26** substitutes = the 20 inherited criteria that still carry
> one, + the 6 new ones above. *(Two accounting notes, because the number
> moved twice during v2. First: **B2.5 gained a substitute it did not have**
> — widening it from `build.go` to the whole package meant actually running
> the package-wide grep, which is executed evidence. Second: v1's own
> substitute figure of 21 is **no longer verifiable** — this file is
> untracked (`git status` reports `??`) and v1 was revised in place, so no v1
> text survives to count. The plan therefore states the derived figure and
> declines to state a delta against a document that no longer exists; the
> command is the definition of record precisely so that this kind of
> arithmetic never has to be trusted. Third: an earlier v2 draft of this very
> paragraph published 74 / 6 / 68 / 28, arrived at by hand-arithmetic —
> 21 + 6 + 1 — before BX.10 existed and while mis-attributing substitutes to
> B3.10 and B4.11; a later one published 25 by the same method. The command
> disagreed with the arithmetic twice, and the command won twice, which is
> why the census now has exactly one definition, in §13(a), per governing
> rule 1.)*

Numbering is **B*n* = FR-*n*** (B1↔FR-1, B3↔FR-3, B4↔FR-4, B5↔FR-5,
B6↔FR-8/Go, B7↔FR-7/build, B8↔FR-9, B9 for ADR-001's bundle shape,
B10 for FR-10/sampler, B2 for FR-2's roster, BX for cross-cutting).

### B1 — FR-1: the token vocabulary is not touched

- **B1.1 — Phase 2 declares no new custom property, except the one G12 carves.**
  *Executed (part A).* `[re-asserted → C1…C8]` `web/shared/css/tokens.css` is
  43 lines (27 declarations) and `themes.css` 187 lines (8 blocks) today.
  `tokens.css` appears in no §6 manifest; `themes.css` appears in exactly one,
  **C1** (§5 Step 1.5, the 18th T1 key — G12's single carve).

  **Part A — `tokens.css` is frozen absolutely, at all nine boundaries,
  ranged against the phase base:**

  ```
  git diff --exit-code b73d31c..HEAD -- web/shared/css/tokens.css
  ```

  **The range is the whole criterion.** v1 wrote the bare
  `git diff --exit-code -- <paths>` form, which compares the *working tree*
  to `HEAD` and therefore exits 0 at every commit boundary by construction —
  a clean tree is a precondition of committing at all (BX.4), so the check
  could never fail no matter what the commit contained. P-III: a gate that
  cannot fail is not a gate. Against `b73d31c` it fails the moment any commit
  in the phase has touched the file. *(Architect B3 / Critic M-3.)*

  **Part B — at the C1 boundary only, `themes.css`'s diff must have the
  carve's exact shape.** A freeze with a hole in it is not a freeze unless the
  hole is measured, so C1 replaces "must be empty" with three integers and a
  roster, all read out of the same diff:

  ```
  git diff b73d31c..HEAD -- web/shared/css/themes.css > t.diff
  D='^\+[[:space:]]+--[a-z-]+:.*;$'
  grep -E "$D" t.diff | grep -c  -- '--color-surface-dynamic'   # 8
  grep -E "$D" t.diff | grep -vc -- '--color-surface-dynamic'   # 0
  grep -cE '^-[[:space:]]+--[a-z-]+:.*;$' t.diff                # 0
  ```

  8 / 0 / 0: eight added declarations, all of the one carved name, and **no
  declaration removed or altered** — the only other permitted change is prose
  inside the header comment, which the `--[a-z-]+:` … `;$` anchors exclude.
  ***Executed (v5).*** Run over a real 18-key diff (`diff -u` between the
  tracked file and Step 1.5's sandbox result, 86 lines): **8 / 0 / 0**, and the
  eight matched lines are exactly Step 1.5's eight values. The anchors are
  load-bearing and were found so by being wrong first: the looser
  `^-[[:space:]]+--` returned **1**, matching the header comment's
  continuation line `   --font-mono overrides and light's --overlay-scrim. */`
  — a comment line that begins with a token name. Declarations in this file are
  indented two spaces and end in `;`; comment continuations are indented three
  and do not. *(This is the §13-style near-miss the Architect's back-pointer
  sweep is for: the criterion pinned three integers that the step's actual diff
  did not produce until the pattern was re-derived against it.)* That
  the eight land **one per theme block** is not re-derived here: clause 5's
  set-equality already requires every theme selector to declare exactly the
  T1 set, so 8 × 18 = 144 cells with zero surplus and zero shortfall is the
  per-block distribution check, and Step 1.5's seeded-failure demonstration
  shows both halves of it failing. *(A shape check is also why the carve
  cannot be widened silently: a ninth added declaration, or one added name
  that is not `--color-surface-dynamic`, fails part B at the boundary where it
  lands.)*

  **Part C — from C1 onward `themes.css` is frozen again, ranged against
  C1.** Once C1 is committed, record its SHA in the commit trailer and use it:

  ```
  git diff --exit-code <C1-sha>..HEAD -- \
      web/shared/css/tokens.css web/shared/css/themes.css
  ```

  Run at the C2, C3, C4, C5, C6a, C6b, C7 and C8 boundaries. It is empty at
  the C1 boundary by identity (`HEAD` *is* C1) and is therefore **not** the
  check that authorises C1 — part B is; but at every later boundary it fails
  the moment a commit whose subject is not "make three gates real" touches
  either file. That is the property G12's enumeration needs: the carve is open
  for exactly one commit and closed in the other eight.

  **Not runnable while the plan is a plan; runnable from C1 onward.**
  *(**v5 FINAL**, Critic minor 12.)* `<C1-sha>` is a literal placeholder — the
  commit it names does not exist yet, so this command cannot be executed
  against today's tree and carries no *Executed* note by design. It becomes
  live the moment C1 is committed, which is also when its first boundary (C2)
  arrives, so nothing is ever asserted by an unrunnable command: the sequence
  supplies the SHA one boundary before the first check needs it. The recording
  step is the commit trailer, named above, and it is the only manual link in
  the chain. §13(a4) part 3's `BASE` snapshot has the same shape and the same
  note.

  *Guaranteed by (steps): §5 **Step 1.5** (which names B1.1's C1 carve in
  return) and **G12**, whose enumeration part B and part C mechanise —
  **v5 FINAL**, Architect A-2: part C is also what §3's G12 bullet now cites
  as the assertor that a second `themes.css` addition cannot pass as "the same
  carve", replacing a false claim about §13(e).*
- **B1.4 — clause 7's two C2 edits each fail on purpose before being
  trusted.** `[deferred → C2]` Step 2.3(c) in full, as two probes, because
  the re-key and the descent are independent changes and "the suite still
  passes" is evidence for neither:
  1. *Descent.* Write a `@media` block containing a non-`.ui-` selector into
     `components.css`; the gate must fail at clause 7 **naming the inner
     selector**, non-zero exit. Under the pre-C2 gate the same probe exits 0
     — that contrast is the evidence the hole was real.

     ***v5 — probe 1 also pins the coordinate*** (Architect A-1 ≡ Critic F-A
     item 4). The failure string must be
     `web/shared/css/components.css:<L>: clause 7: selector "<name>" does not
     start with .ui-*`, where **`<L>` is the inner selector's own line number
     in `components.css`** — not its offset within the `@media` body. Check it
     by construction, not by eye: place the probe's `@media` so that the inner
     selector sits at a line the author knows (e.g. append the block at the
     end of the file, so `<L>` = the file's pre-probe line count + 2), and
     require the printed integer to equal it. Rationale and the offset idiom
     the descent must reuse are in §5 Step 2.3(b); the reason this belongs on
     probe 1 rather than only on B1.3 is that **C2 is the commit that writes
     the descent** — a body-relative implementation must fail here, in the
     commit that can fix it, and not two commits later at C4 where §6 does not
     permit editing the gate. *(The two assertions are deliberately
     complementary, not duplicated: probe 1 pins an **exact** line on
     synthetic content whose geometry the probe controls, while B1.3's C4 leg
     pins the weaker **`> @media` line** relation on C4's real blocks, whose
     line numbers move as the widget's CSS is written.)*
  2. *Re-key + the flat model — the opt-in default is still **off** for the
     other two callers, proved by a probe that can fail.* **Rewritten in v4
     (Architect A3, Critic F-2); v2/v3's form was permanently vacuous.**
     Give the descent something to descend into, in the one file whose
     clauses would notice:

     - In the working tree, wrap `web/shared/css/themes.css`'s **last theme
       block** — `[data-theme="puma"]`, at `:174-195` when C2 runs (`:167-187`
       in today's pre-C1 tree; Step 1.5's coordinate table is the one
       definition of the shift) — in `@media (min-width: 1px) { … }`, changing
       nothing inside it.
     - **The falsifying observation.** Re-run the gate with the opt-in as C2
       ships it (off for clauses 3/4/5). It **must fail**, at
       `web/shared/css/themes.css:1: clause 4: missing theme block(s):
       [data-theme="puma"]`, rc 1 — `parseBlocks` now returns **7** depth-1
       blocks where `THEME_SELECTORS` expects 8. **If instead the gate
       passes, the descent is global** and this criterion has caught it:
       passing requires having walked into the at-rule, which is exactly the
       behaviour clauses 3/4/5 must not have.
     - **The positive control.** Force the opt-in **on** for this caller and
       re-run: the gate returns to PASS, clause 4 seeing the full 8-selector
       roster and clause 5's total back at `144`
       (`EXPECTED_COLOR_DECLARATIONS`, `check-shared-css.mjs:99` at C2 —
       `:98`/`136` pre-C1, per Step 1.5's coordinate table). This is
       what establishes that the wrapper is a *descent* discriminator and not
       a file the gate simply cannot read.
     - Revert the wrapper and the forced flag.

     The descent-off/descent-on **contrast** is the evidence, in the same
     positive/negative shape probe 1 already uses. *(v2 and v3 instead asked
     for "clause 3 still reports one `:root` block, clause 4 still the
     8-selector roster, clause 5's total still `136`" and named its purpose
     as catching an accidental global descent. Measured: `tokens.css` and
     `themes.css` contain **zero `@` characters**, so `parseBlocks` returns
     byte-identical output whether the `:182` filter descends or not — all
     three numbers hold under the exact fault named, and the gate stays
     green. B1.1 freezes both files for the whole phase, so the vacuity was
     permanent, not incidental. The old prose also had the logic inverted: it
     said a global descent "would otherwise pass unnoticed on today's
     at-rule-free tree", when the at-rule-free tree is precisely **why** the
     old probe could not see it. P-III — a gate that cannot fail is not a
     gate — is cited by name two bullets above, and this is the second time
     the plan has had to apply it to its own clause-7 probes (the first was
     Architect N6).)*

  Both probes are reverted; neither is committed, and neither is a
  counter-example to **B1.1**: B1.1 is a `git diff b73d31c..HEAD` over
  *commits*, so a reverted working-tree edit cannot make it fail. *Substitute:*
  the filter is unconditional today (`:182`); `components.css` has zero `@`
  characters, so probe 1's "before" state is directly observable; and
  `themes.css` likewise has zero, which is both why probe 2 has to create its
  own at-rule and why the old form of probe 2 could not fail.
  ***Executed for the contrast (v4):*** probe 2 was run at plan time against a
  **copy** of `scripts/` + `web/shared/` outside the repo — the gate `chdir`s
  to its own parent, so a copy is lintable and the tracked tree was never
  touched. Baseline: `check-shared-css: 11 clauses pass over 5 file(s) in
  web/shared/css/`, rc 0. With puma wrapped and the filter as it ships today
  (descent **off**): `web/shared/css/themes.css:1: clause 4: missing theme
  block(s): [data-theme="puma"]`, rc **1** — character for character the
  string the criterion pins. With the same wrapper and `parseBlocks` patched
  to recurse into at-rule bodies (descent **on**): back to `11 clauses pass
  over 5 file(s)`, rc 0, which is only reachable if clause 4 saw 8 selectors
  and clause 5's total was **136 — the pre-C1 value, because that run was made
  against the pre-C1 sandbox**. At C2 the same run must show **144**; the
  contrast the criterion rests on is *rc 1 vs rc 0*, and the clause-5 total is
  the positive control's corroborating number, so the shift changes the
  expected integer and nothing about the probe's logic. *(v5: the executed
  evidence is left at the value it was measured at, and labelled, rather than
  edited to 144 — a re-labelled measurement is honest, a re-written one is
  not. Step 1.5's demonstration is the executed evidence for 144.)* So the
  contrast is measured, not predicted, and probe 2 is now a probe that
  demonstrably fails in the descent-off direction.

  *Guaranteed by (steps): §5 **Step 2.3(b)** for probe 1's in-file coordinate
  and §5 **Step 2.3(c)** for the two probes themselves — both name B1.4 in
  return; §5 **Step 1.5** for the `144` / `:99` / `:174-195` values probe 2
  quotes.*
- **B1.2 — `gates/token-overlap.mjs`'s intersection stays `{--font-mono}`.**
  *Executed (reading `:27`).* `[re-asserted → C1…C8]` Re-run at every
  boundary; a non-zero exit means a new shared name collided with
  taskmaster's sheet.
- **B1.3 — every colour in `components.css` is a `var(--color-*)`
  reference.** `[deferred → C2/C3/C4]` — enforced by clause 7's existing
  `COLOUR_LITERAL` check, whose current scope was read at
  `check-shared-css.mjs:298-320`. Substitute evidence: the clause exists and
  passes on the 101-line file today. *(v3: v2 labelled this `→ C2` and then
  scheduled it at **no** boundary at all. Both halves are fixed — it is now in
  C2's, C3's and C4's Acceptance lines and §6 cells, and the label reads
  C2/C3/C4 because **all three** commits write new rules into
  `components.css`, so all three can introduce the first colour literal.
  Critic C-1.)*

  **C4's leg carries one extra assertion, because C4 is where clause 7 meets
  its first *real* at-rule.** *(v3 — answering the Critic's unscored
  question (c): yes, C4's real `@media` selectors get a post-C4 assertion,
  and here it is.)* Clause 7 is two independent rules (§11 item 1): the
  colour scan at `check-shared-css.mjs:304-308` is a whole-file per-line
  loop and is therefore depth-blind — it already sees inside at-rules today,
  and covers C4's new blocks with no change. The **selector** loop at
  `:310-318` is the one that depends on C2's descent, because `parseBlocks`
  discards at-rules at `:182` and yields only depth-0 blocks. Until C4 there
  is nothing for the descent to descend into: `components.css` has zero `@`
  characters, so **B1.4's probe is synthetic by necessity** — it writes the
  `@media` block it then detects. C4 adds the first at-rules the plan ships
  (B4.9's `prefers-reduced-motion` suppression and the drawer's responsive
  width), and a descent that works on a hand-written probe but not on real
  content is exactly the gap a synthetic probe cannot close. So at the C4
  boundary:

  1. *Positive.* Clause 7 passes with C4's real at-rules present — `node
     scripts/check-shared-css.mjs`, rc 0, printing its `N clauses pass over M
     file(s)` line.
  2. *Negative, on real content.* Retitle one selector **inside** C4's
     `@media (prefers-reduced-motion: reduce)` block to a non-`.ui-` name in
     the working tree; clause 7 must fail **naming that inner selector**, and
     the **line number in the failure must be greater than the line of the
     `@media` that encloses it** — `fail()` writes
     `web/shared/css/components.css:<line>: clause 7: selector "<name>" does
     not start with .ui-*` to **stderr** and exits 1
     (`check-shared-css.mjs:106-109`), and the line it is handed is
     `block.line`, the *selector's* own line (`:315`, inside the clause-7
     block loop at `:310-318`). A line inside the at-rule body is reachable only by
     having walked in. Revert; nothing is committed.

  Part 2 is the P-III probe; part 1 is what stops it from being satisfied by a
  gate that fails on everything. Both run against the same real block B4.9
  depends on, which is why this lives on B1.3's C4 leg rather than becoming a
  criterion of its own — it asserts a property of clause 7's scope, not a new
  fact about the widget.

  *Guaranteed by (step): §5 **Step 2.3(b)**, whose v5 post-condition requires
  the descent to express nested blocks' `line` in **absolute file
  coordinates**, and which names this part in return.* ***This pointer is the
  whole of Architect A-1 / Critic F-A item 4.*** v4 asserted part 2's
  inequality here and specified nothing about coordinates there; both reviewers
  built the natural descent, got body-relative lines (a selector at absolute
  `:104` inside an `@media` opened at `:103` reported as `:2`), and part 2 went
  red on a correct implementation — at C4, the one boundary where §6 forbids
  editing `check-shared-css.mjs`. Under the Architect's back-pointer sweep of
  v4 this was **the only criterion pinning a gate's output with no step
  guaranteeing it**, which is why the same defect was found twice from
  different directions. The stronger, exact-coordinate form of the same
  assertion now also runs at **C2** (B1.4 probe 1), so C4's leg is the
  confirmation on real content rather than the first place a body-relative
  descent can be detected.

  > ***v4 — part 1's "inspected-block count" assertion is deleted, and part 2
  > absorbs the work it was meant to do*** (Architect A2/M-1, Critic F-7).
  > v3's part 1 required "the selector loop's inspected-block count is
  > **strictly greater** than the depth-0 block count". `check-shared-css.mjs`
  > writes **exactly one** line to stdout on success — `:426`,
  > `check-shared-css: 11 clauses pass over 5 file(s) in web/shared/css/` — a
  > **file** count, not a block count; it emits no per-clause counts, and the
  > script has **zero exports**, so `parseBlocks` cannot be imported by a
  > scratch harness either. Discharging it as written would have required
  > instrumenting the gate, which no step schedules. **Both reviewers offered
  > a fix and v4 takes both**, because they are complementary rather than
  > alternative: the Architect's (delete the unobservable half; part 1's
  > remaining half already excludes the fails-on-everything gate) removes an
  > assertion no output can settle, and the Critic's (require part 2's FAIL to
  > name a line ≥ the `@media` line) puts the *descent* evidence back where it
  > is free — `fail()` already prints the line. The alternative the Architect
  > named — have C2's descent print `clause 7: N block(s) inspected (M at
  > depth 0)` and pin the string per boundary the way B9.1 pins
  > `bundle-shape`'s — is declined: it adds a stdout contract to a gate this
  > phase otherwise only re-scopes, and the line-number assertion buys the
  > same proof with no code change at all.

### B2 — FR-2: the roster and the `obsidian` rename

- **B2.1 — `grep -c 'data-theme="dark"' web/obsidianoid/index.html` = 0,
  rc=1.** `[deferred → C5]` Substitute: it is `1` today, at `:2`.
- **B2.2 — `grep -c 'data-theme="obsidian"' web/obsidianoid/index.html` = 1.**
  `[deferred → C5]`
- **B2.3 — `web/obsidianoid/css/themes.css` does not exist.**
  `[deferred → C5]` Substitute: 189 lines today, 5 depth-1 blocks.
- **B2.4 — `grep -rn '"theme": "dark"'` over `unified-webapp.json`,
  `unified-webapp-example.json`, `local-test/config.json`, `README.md`
  returns nothing, rc=1.** `[deferred → C5]` *Executed as the negative's
  complement:* all four sites carry it today (`:53`, `:67`, `:63`, `:88`).
- **B2.5 — `grep -rc '"dark"' internal/obsidianoid/` reports 0 in every file,
  rc=1** — widened in v2 from `build.go` alone to the whole package, because
  the package has exactly **two** occurrences and the plan changes both
  (Step 5.4). `[deferred → C5]` *Executed for the premise:* today the same
  command returns `build.go:41` (the default) and `handler_test.go:40` (a
  fixture), and nothing else. The widened form is what makes the criterion
  match the edit; the narrow form would have passed with `handler_test.go`
  left half-renamed.
- **B2.6 — the migration is proved both ways, over more than one vault key.**
  ***Widened in v5 (Critic F-D item 6).*** `[deferred → C5]` Seed
  `localStorage` with **two** obsidianoid keys at once —
  `obsidianoid-theme-0 = "dark"` and `obsidianoid-theme-3 = "dark"` — plus one
  key the migration must not touch (`obsidianoid-theme-1 = "forest"`) and one
  foreign key (`ui-theme:todo = "dark"`). After one boot: both `"dark"` values
  are `"obsidian"`, `"forest"` is untouched, `ui-theme:todo` is **untouched**
  (the scan is prefix-scoped, not a global `"dark"` sweep), the
  `ui-theme-migrated:obsidianoid` flag is set, and a second boot rewrites
  nothing. Also: a stored `"obsidian"` is untouched, and a browser with **no**
  obsidianoid key migrates cleanly (zero writes, flag still set).

  > **Why two keys and a foreign key. (v5.)** Through v4 this criterion seeded
  > exactly one key, `obsidianoid-theme-0`. That is satisfiable by an
  > implementation that migrates only vault 0 — the single most likely way to
  > write this wrong, because vault 0 is the only key present in any developer's
  > own browser and because `state.vaults` is `[]` at the moment the migration
  > runs, so "iterate the vaults" is not available (§5 Step 5.4's migration
  > bullet). A user with four vaults and a `"dark"` choice on vault 3 would
  > have got shared `dark` on that vault only — a per-vault regression that the
  > v4 criterion passes green. The foreign key is there because the migration's
  > mechanism is a prefix scan over `Object.keys(localStorage)`, and a scan
  > written against the *value* `"dark"` rather than the *key prefix* would also
  > pass the v4 criterion while corrupting todo's stored theme.
  >
  > *Guaranteed by (step): §5 Step 5.4's migration bullet specifies the
  > prefix-scan mechanism and the guard flag this criterion probes.*
- **B2.7 — all five donor blocks are declaration-for-declaration identical to
  their shared counterparts, every property, not a spot-check.** *Executed.*
  `[re-asserted → C5]` Step 5.0's comparison was run at plan time over
  `web/obsidianoid/css/themes.css`'s 5 blocks (`:2-37`, `:40-75`, `:78-113`,
  `:116-151`, `:154-189`) against `web/shared/css/themes.css`'s `obsidian`,
  `forest`, `ocean`, `ember` and `rose` blocks, applying the four renames and
  mapping `--color-surface-dynamic` to itself: **85 comparisons, 0 colour
  mismatches and 0 unmapped names across all five**, with `--color-primary-fg`
  the only shared-only key in each block. *(v5 — re-run by machine after the
  Q12 ruling; the comparison widened 16 → 17 names per block, 80 → 85 pairs,
  and the script now also reports unmapped and shared-only names so a dropped
  name cannot read as a pass. §4.3 carries the result table.)* Re-run as the
  first action inside C5, because
  the tree can move between planning and execution; a non-zero result **stops
  C5** rather than being worked around (§8.5 Scenario 1).

  > **Why this is a criterion and not a remark.** v1 verified `--color-bg` and
  > `--color-primary` in **one** block and generalized to all five — 2 keys of
  > 17 in 1 file of 5 — then built C5's "rename plus delete" shape on the
  > generalization. That is the exact shape of check §8.5 Scenario 1 describes
  > slipping through. The full comparison is cheap and it either licenses the
  > delete or forbids it.

  > **What "Executed" means here, stated because the 85 is not yet runnable
  > against the tree.** *(**v5 FINAL**, Critic minor 9.)* The 17-names-per-block
  > form of this comparison requires `--color-surface-dynamic` to exist on the
  > shared side, and it does not **until C1** lands §5 Step 1.5's eight
  > declarations. Run against the tree **today** the comparison is
  > **80 pairs** (16 mapped names × 5 blocks, `--color-surface-dynamic`
  > unmapped on the shared side and reported as such); the **85** published
  > above was produced in Step 1.5's sandbox, against the built post-C1 sheet
  > — which is legitimate, because this criterion is
  > `[re-asserted → C5]` and C5 is four boundaries after C1, but it is a
  > different thing from "reproduces in the tree right now" and v5 wrote a
  > bare *Executed* over both. From C1 onward the 85 is the live number and
  > the 80 is history. The two related §13 notes take the same treatment:
  > §13(a4)'s two embedded scripts are **published indented inside this
  > document** and must be de-indented before they run, and part 3 of that
  > clause needs a `BASE` snapshot held **outside** the tree; and §13(a6)'s
  > "53 hits" `showToast` aside was measured and never published as a
  > reproducible command, so it is an observation and not a gate.

  *Guaranteed by (step): §5 **Step 5.0**, the literal first action of C5, which
  runs this comparison and stops C5 on a non-zero result.* *(**v5 FINAL**,
  Critic finding 4: §17 claimed six bidirectional pointer pairs, and this was
  the criterion with **no** `Guaranteed by (step)` line at all while Step 5.6
  and §11 both cited it as the colour evidence of record.)*
- **B2.8 — `--color-surface-dynamic` is a shared token on all 8 themes, both
  of its obsidianoid consumers are carried unedited, and both effects that
  depend on it still work.** ***Rewritten in v5 on the user's Q12 ruling; the
  pair shift it used to assert no longer exists.*** `[deferred → C5]` §5 Step
  1.5 owns the values and the derivation; §4.3 owns the rename table; this
  criterion owns the proof, in four parts:

  1. `grep -rc 'surface-offset\|--color-surface:' web/obsidianoid/` = 0, rc=1
     — no obsidianoid-**only** surface name survives. *(v5: `surface-dynamic`
     is deliberately **not** in this pattern any more. It is now a shared
     name, so its survival is required, not forbidden — which is exactly the
     kind of inversion a hand-edited grep gets wrong, and the reason part 2
     asserts the count from the other side.)*
  2. `grep -c 'var(--color-surface-dynamic)' web/obsidianoid/css/app.css` =
     **2** — `:202`'s middle stop and `:455`'s background are the two
     references, and **the `--color-surface-dynamic` name at both is unchanged
     by C5** (§5 Step 5.3 **item 4**, which is a no-op); `:202`'s **two
     `surface-offset` references are renamed by item 3**, so `:455` is
     byte-unchanged and `:202` is not. Both resolve, from
     `web/shared/css/themes.css`, once the module's own `themes.css` is
     deleted. *(**v5 FINAL**, Critic finding 7 — the one internal
     unsatisfiability in v5's §7. The part said both lines were
     "byte-unchanged by C5", which makes this criterion contradict **part 1**
     of itself: part 1 requires `grep -rc 'surface-offset…' web/obsidianoid/`
     = 0, which is only achievable by editing `:202`, and part 3 says so
     outright. What is invariant across C5 is the **token name this criterion
     is about**; what changes at `:202` is a *different* token's name on the
     same line. The grep in this part counts `--color-surface-dynamic` and is
     unaffected either way, so the assertion was always runnable — it was the
     stated reason for it that could not be true.)*
  3. `.skeleton-text`'s three gradient stops resolve to **two distinct**
     computed colours in all 8 themes, never one — the stops being
     `--color-surface-3` / `--color-surface-dynamic` / `--color-surface-3`
     after `surface-offset`'s uniform rename. This is still the assertion that
     catches a flattened gradient: one name for both stops makes the `shimmer`
     animation at `:204` invisible while every gate stays green.
  4. `#mode-switcher button.active`'s computed background differs from
     `#mode-switcher`'s own in all 8 themes. `:458`'s `:hover:not(.active)`
     changes only `color`, so this background *is* the selection indicator —
     if it equals its container the selected tab disappears.

  Parts 3 and 4 are now the **same** pair of tokens (`--color-surface-3` vs
  `--color-surface-dynamic`), where under v4's pair shift they were
  `surface-2` vs `surface-3`. The pair is verified distinct in all 8 themes,
  which is what licenses both effects — measured in Step 1.5's sandbox:

  | theme | `--color-surface-3` | `--color-surface-dynamic` |
  |---|---|---|
  | dark | `#2c3138` | `#373c43` |
  | light | `#e6e3df` | `#d3d0cc` |
  | obsidian | `#252535` | `#2e2e42` |
  | forest | `#1a3320` | `#1f3d26` |
  | ocean | `#182a40` | `#1e3450` |
  | ember | `#352510` | `#402c14` |
  | rose | `#331828` | `#3d1e30` |
  | puma | `#13202e` | `#182737` |

  Eight pairs, **zero collisions**. Note `light`'s pair moves *darker* rather
  than lighter — the ladder step is away from the page ground on every theme,
  which is what makes one assertion correct for all eight instead of needing a
  light-theme special case. *Substitute:* both consumer sites, and the
  colour-only `:hover:not(.active)` rule, are confirmed in today's tree; the
  eight-pair distinctness above is measured, not deferred.

  *Guaranteed by (step): §5 **Step 1.5**, which authors the eight values and
  names B2.8 in return, and §5 **Step 5.3 item 4**, which is the "unedited"
  half of part 2.*

  > **Q12 is closed** (user ruling, 2026-09-16 — §9). What was "the plan of
  > record, not a settled fact" in v2–v4 is now settled the other way: the
  > 18th token lands, at **C1** rather than at C5, and every obsidianoid
  > surface name stays 1:1. Ledger row 14.
- **B2.9 — `npm run typecheck` is clean after obsidianoid becomes ESM, and
  `threads.ts`'s `Window` augmentation is a `declare global` block.** *(New in
  v3 — Architect N1.)* `[deferred → C5]` Two parts:
  1. `npm run typecheck` exits 0 at the C5 boundary. This is the criterion's
     substance, and no other C5 criterion covers it: `make gates`
     (`Makefile:60-64`) runs four scripts and none of them invokes `tsc`;
     typecheck lives in `test-web` (`Makefile:66-69`, the `npm run typecheck`
     line at `:68`), which `make check` does reach — so the gate exists, but
     until now nothing in §7 *named* it, and a plan whose criteria do not name
     the command that catches its own hardest compile-time change is relying
     on `make check` incidentally rather than asserting it.
  2. `grep -c 'declare global' web/obsidianoid/js/threads.ts` = 1, and
     `grep -cE '^interface Window' web/obsidianoid/js/threads.ts` = 0 — the
     augmentation is wrapped, not left at top level.

  *Executed for the defect, as a reduced probe rather than a tree edit (no
  source file may be modified at planning time):* a probe reproducing
  `threads.ts`'s shape was run through `npx tsc --noEmit --strict`. With an
  `import` plus a bare top-level `interface Window { … }`, tsc reports
  **`TS2339: Property 'ThreadsView' does not exist on type 'Window & typeof
  globalThis'`**. Wrapping the interface in `declare global { … }` clears it.
  The same wrapper in a *script* (no import) is rejected with **`TS2669:
  Augmentations for the global scope can only be directly nested in external
  modules or ambient module declarations`** — which is why the two edits are
  atomic and why this criterion cannot be moved to an earlier boundary.
  `threads.ts` has **0** top-level `import`/`export` lines today, the premise
  the whole finding rests on, and that too is one command:
  `grep -cE '^(import|export)' web/obsidianoid/js/threads.ts`.
- **B2.10 — the four deleted theme functions leave no reference behind, and
  the `ThemeManager` instance exists.** *(New in v5 — Critic F-B items 1–2.)*
  `[deferred → C5]` One guarded grep for the four retired names across both
  TS files, expected **0 matching lines, rc=1**:

  ```
  P='themeStorageKey|buildThemePanel|toggleThemePanel|setTheme\('
  if grep -nE "$P" web/obsidianoid/js/app.ts web/obsidianoid/js/threads.ts; then
    echo "FAIL: retired theme symbol still referenced"; exit 1
  else
    rc=$?; [ "$rc" -eq 1 ] || { echo "FAIL: grep error rc=$rc"; exit 1; }
  fi
  ```

  …plus one expected **1**:
  `grep -c 'new ThemeManager' web/obsidianoid/js/app.ts`.

  The **guarded** form is mandatory here for the reason §5 Step 7.3 gives:
  bare `! grep` maps grep's exit **2** (unreadable file, bad pattern) onto
  "success", so a mistyped path would read as proof that every reference is
  gone. `grep -nE` rather than `-c` so a failure prints the surviving line and
  its number instead of an integer the reader then has to go hunting for.

  > **Why this is a criterion and not left to `tsc`.** B2.9 already runs
  > `npm run typecheck`, which *does* catch the three dangling references as
  > `TS2304`. This criterion earns its place on two grounds `tsc` cannot
  > cover:
  >
  > 1. **`setTheme(` names a symbol that also exists in the shared barrel, so
  >    the obvious "minimal fix" for the compile error is itself the
  >    regression.** `web/shared/ts/theme.ts:19-21` exports
  >    `setTheme(name: string): void` — a one-line `dataset.theme` write with
  >    **no** persistence and **no** resolution order. Deleting the local
  >    `setTheme` and leaving `:479`/`:494` does fail the build, but not with
  >    `TS2304` once `setTheme` is imported: both call sites pass **two**
  >    arguments (`…, false`) against a one-parameter signature, so tsc reports
  >    **`TS2554: Expected 1 arguments, but got 2`**. The dangerous repair is
  >    to drop the second argument —
  >    `setTheme(stored || state.vaults[0].theme || 'obsidian')` — which
  >    **typechecks cleanly**, stamps a plausible theme, and bypasses
  >    `ThemeManager` entirely: no storage validation against `THEMES`, no
  >    `system` step, no `reresolve()` on vault switch, and `onChange` never
  >    fires. That is a green build with the resolution order gone. `tsc`
  >    cannot distinguish it from correct code; `grep -c 'setTheme('` = 0 can.
  > 2. **`new ThemeManager` = 1 is the only check that the instance was ever
  >    constructed.** A config object described in prose and never instantiated
  >    is exactly the v4 defect this criterion closes (`new ThemeManager`
  >    appeared **zero** times in the v4 plan).
  >
  > *Guaranteed by (step): §5 Step 5.2 — the construction block at
  > `js/app.ts:503` and the three-call-site table.*
- **B2.11 — first load with empty storage resolves through `serverDefault()`,
  and does not write storage.** *(New in v5 — Critic F-B item 2.)*
  `[deferred → C5]` With `localStorage` **empty** for the obsidianoid origin
  and a config whose vault 0 carries `"theme": "ocean"`, boot obsidianoid and
  assert three things after the roster arrives:
  1. `document.documentElement.dataset.theme` is `ocean` — the server default
     was honoured, so resolution step 2 is live;
  2. `localStorage.getItem('obsidianoid-theme-0')` is **`null`** — the server
     default was **not** persisted;
  3. `grep -c "default: 'obsidian'" web/obsidianoid/js/app.ts` = **1** — the
     constructed config carries the post-rename floor, not `'dark'`.

  > **Part 3 is a grep in v5 FINAL, and it used to be a boot scenario that
  > could not fail.** *(Critic findings 1 and 2, which are the same defect
  > seen from two sides.)* v5's part 3 read: "with the same empty storage and a
  > vault carrying **no** `theme`, the stamp is `obsidian` — `default:
  > 'obsidian'` is what the order falls through to". Two independent reasons
  > that scenario proves nothing:
  >
  > 1. **A themeless vault does not exist by the time the browser sees it.**
  >    `internal/obsidianoid/build.go:38-43` **backfills** an empty vault
  >    theme — `cfg.Vaults[i].Theme = "dark"` at `:41` today, `"obsidian"`
  >    after §5 Step 5.4's coupled rename (**B2.5**) — and this plan states
  >    the backfill itself at §4.3 `:905`. So `serverDefault()` returns a
  >    value, resolution stops at **step 2**, and the assertion passes without
  >    step 4 ever being reached. Worse, it passes *with the exact string the
  >    part is trying to prove*: post-C5 the backfill hands back `obsidian`,
  >    so a `ThemeManager` constructed with `default: 'dark'` — the defect —
  >    would still stamp `obsidian` and still pass. The part was parts 1 and 2
  >    run a second time under a different name.
  > 2. **Step 4 is not reachable at runtime at all.** The `system` step
  >    resolves under every `matchMedia` outcome including `no-preference`
  >    (§5 Step 3.1, §15 row 9), so nothing ever falls through it. `default`
  >    is a **typed-config floor**: its value is asserted by *construction*,
  >    not by *resolution*, and a grep over the construction site is the only
  >    honest form of the assertion. B3.1 takes the same correction.
  >
  > The defect part 3 exists to catch is unchanged and is still caught: a
  > `ThemeManager` constructed with `'dark'` carried over from the pre-rename
  > file would render a themed-looking page in the wrong palette, and no pixel
  > baseline covers it because P5a captures obsidian. The grep fails on
  > exactly that, and fails on a missing `default` too, where the boot
  > scenario failed on neither.

  > **Why this needs its own criterion. (v5, Critic F-B item 2.)** B3.1
  > asserts the resolution order on the `ThemeManager` **unit**, with a stubbed
  > storage and a stubbed `serverDefault`. It says nothing about obsidianoid's
  > *wiring* of that order, and obsidianoid is the only module in the phase
  > whose `serverDefault` is non-trivial: it closes over `state.activeVault`
  > and reads a roster that does not exist at construction time. Part 2 is the
  > half that cannot be obtained any other way — it is the single assertion
  > that distinguishes `themes.reresolve()` from `themes.set(…)` at
  > `js/app.ts:479`, and `set()` passes parts 1 and 3 while failing part 2.
  > Without part 2, the wrong call is green. Part 3 catches the other silent
  > substitution: a `ThemeManager` constructed without `default` (or with
  > `'dark'` carried over from the pre-rename file) renders a themed-looking
  > page in the wrong palette, and no pixel baseline covers it because P5a
  > captures obsidian.
  >
  > *Guaranteed by (step): §5 Step 5.2 — the `reresolve()`-not-`set()`
  > argument and `default: 'obsidian'` in the construction block.*
- **B2.12 — obsidianoid's two `<script>` tags are modules, and the artifacts
  they load really are ESM.** *(New in v5 — Critic F-C.)* `[deferred → C5]`
  Two sides, because either alone is satisfiable by a half-done C5:

  ```
  # markup side: both end-of-body tags carry type="module", and no classic
  # <script src> to a local /js/ path survives on the page
  grep -c 'script type="module"' web/obsidianoid/index.html          # 2
  grep -cE '<script src="/js/' web/obsidianoid/index.html            # 0, rc=1

  # artifact side: each emitted file imports the barrel by URL
  grep -c 'from "/shared/dist/shared.mjs"' web/obsidianoid/js/app.js     # >= 1
  grep -c 'from "/shared/dist/shared.mjs"' web/obsidianoid/js/threads.js # >= 1
  ```

  > **Why both sides, and why this is not a duplicate of B9.1.** B9.1 asserts
  > the **gate** (`bundle-shape.mjs` with `--require=…,obsidianoid`) sees the
  > barrel marker in obsidianoid's artifacts — it is about the gate's coverage
  > set. B2.12's artifact side is the same fact read directly, which matters
  > here for a different reason: it is the **premise** of the markup side. The
  > two halves can fail independently and each failure mode is distinct and
  > silent to the other:
  >
  > - **modules without ESM** (tags flipped, descriptor not) — everything
  >   loads, `type="module"` on a classic script is harmless, and the only
  >   symptom is that the shared code was never adopted. Nothing else at C5
  >   catches the tag half in isolation.
  > - **ESM without modules** (descriptor flipped, tags not) — a blank page
  >   and one console `SyntaxError`. `make check` is fully green: `tsc` is
  >   happy, the build emits, the artifact count is right, `bundle-shape.mjs`
  >   passes because the marker *is* there. **No other criterion in the phase
  >   fails on this**, which is exactly why it needs one. It would be found in
  >   P5a's browser pass — after the commit, by eye.
  >
  > The second grep on the markup side (0 classic local `<script src>` tags)
  > is what makes "2 modules" mean "both of them" rather than "at least two
  > somewhere": a partially-converted page with one module tag and one classic
  > tag returns 1 and 1, failing both halves.
  >
  > *Guaranteed by (step): §5 Step 5.2's `type="module"` bullet (markup side)
  > and Step 5.1's descriptor flip (artifact side).*

### B3 — FR-3: ThemeManager

- **B3.1 — resolution order is localStorage → `serverDefault()` → `system` →
  `default`,** proved by **three unit cases isolating steps 1–3, plus a
  construction assertion for step 4** (unreachable at runtime — §5 Step 3.1).
  `[deferred → C3]`

  > **Why three and not four.** *(**v5 FINAL**, Critic finding 1 — the one
  > CRITICAL of iteration 5, and it was a contradiction the plan held in three
  > places at once.)* v5 required "four unit cases each isolating one step".
  > No such fourth case can be written: the `system` step resolves under
  > **every** `matchMedia` outcome — `matches` → `dark`, and everything else
  > including `no-preference` → `light` — so resolution never falls out of
  > step 3 and step 4 is **unreachable at runtime**. §15 row 9 already said
  > so, and §5 Step 3.1 now says it in the resolution order itself.
  >
  > A test claiming to isolate step 4 could only do it by removing
  > `matchMedia` from the stub, which asserts the behaviour of a browser this
  > repo does not serve and contradicts this criterion's own "Runtime guard —
  > deliberately none" note below. So step 4 is asserted where it actually
  > lives: `default` is a **typed-config floor**, present in the
  > `ThemeManagerOptions` type and in each adopter's construction site, and
  > the assertion is that the construction carries the right value —
  > **B2.11 part 3** for obsidianoid, the same shape for todo. The three
  > runtime cases are: storage hit (step 1 wins over a live
  > `serverDefault()`), storage empty and `serverDefault()` answering
  > (step 2), storage empty and `serverDefault()` returning `undefined`
  > (step 3, both `matchMedia` polarities).
  >
  > This is not a weakening. Four cases over a three-branch resolver meant
  > one case that either could not be written or, written anyway, would pass
  > against a resolver with **no** step 4 at all — a gate that cannot fail
  > (P-III). Three cases plus a construction grep fail on every defect the
  > four were meant to catch, including a missing or stale `default`.

  > **Precondition, and it is not optional: `theme.test.ts` must stub three
  > globals the harness does not provide.** *(Critic M-11.)*
  > `scripts/test-web.mjs:51-59` bundles each suite with
  > `platform: "node"` `:54`, `format: "cjs"` `:55`, `target: "node18"` `:56`
  > and pipes the output to a fresh `node` via
  > `spawnSync(process.execPath, ["-"], { input: code })` `:65-69` —
  > **there is no DOM, no jsdom, and no `window`**, which is why
  > `modal.test.ts:47-52` hand-rolls `globalThis.document` with exactly the
  > five members `modal.ts` touches (`body`, `activeElement`,
  > `createElement`, `addEventListener`, `removeEventListener`) and nothing
  > more. `ThemeManager` touches a **disjoint** set, none of which that stub
  > provides:
  >
  > | needed | for | present in `modal.test.ts`'s stub? |
  > |---|---|---|
  > | `document.documentElement.dataset` | stamping `data-theme` | **no** — the stub has `body` but no `documentElement` |
  > | `localStorage.getItem`/`setItem` | step **1** of the order, and `set()`'s write-back | **no** — not a `document` member at all |
  > | `matchMedia(q)` → `{ matches, addEventListener }` | the `system` step and B3.4's live `change` | **no** |
  >
  > So B3.1 and B3.4 are **unwritable** until the stub exists, and each suite
  > is its own esbuild bundle in its own `node` process (`:65-69`, driven by
  > the loop at `:82-91`), so nothing is inherited from `modal.test.ts`.
  > C3 therefore adds a small
  > `web/shared/ts/test-dom.ts` helper exporting `installFakeDom()` —
  > `documentElement` with a plain-object `dataset`, a `Map`-backed
  > `localStorage`, and a `matchMedia` whose returned object exposes a
  > `fireChange(matches)` for B3.4 — and both `theme.test.ts` and
  > `menu.test.ts` import it. `modal.test.ts` is **not** retrofitted onto it
  > in this phase (that is a refactor of a passing suite for no gate benefit;
  > §11).
  >
  > Two gate consequences, both checked: the helper is **not** a barrel export
  > and is not imported by `index.ts`, so `check-shared-barrel.mjs`'s counts
  > are unaffected (it reads `web/shared/ts/index.ts` only, `:23`) and the
  > shared bundle does not grow; and it **is** matched by `tsconfig.json:15`'s
  > include globs, so `tsc --noEmit` type-checks it like any other file.
  >
  > **Runtime guard — deliberately none.** `ThemeManager` does *not*
  > feature-detect `matchMedia`. It is universally available in every browser
  > this repo serves, and a guard would be an untested branch that the stub
  > above makes unnecessary. Stated so a reviewer does not read its absence as
  > an oversight.
- **B3.2 — an unknown stored name falls through** rather than being stamped.
  `[deferred → C3]`
- **B3.3 — `storageKey()` overrides the `ui-theme:<module>` default**, proved
  with obsidianoid's per-vault template. `[deferred → C3]`
- **B3.4 — `system` is the implicit resolution step and its `change` listener
  is live**: a `matchMedia` `change` event re-applies **while the resolution is
  still reaching the `system` step** — storage empty and `serverDefault()`
  returning `undefined` — and is ignored once storage or the server answers.
  `[deferred → C3]`

  > **Retitled in v5 FINAL** *(Critic finding 1, secondary)*. The assertion is
  > unchanged; the title was un-offerable. `system` is **not a selectable
  > name** in this product: it is absent from `THEMES`
  > (`web/shared/ts/theme.ts:8-17`, 8 names), absent from `themes.list`
  > (**B3.5** asserts the two are the same set), and §5 Step 3.1's validation
  > rule **rejects** it on read-back from storage. So "while `system` is
  > selected" named a state no user can enter, and a test written against the
  > old title would have had to reach it by writing an invalid value into
  > storage — where B3.2 requires it to be ignored. §5 Step 3.1 carries the
  > resolution semantics, §10 ledger row 22 records the deviation from FRD
  > `:254-255`, and §11 item 17 carries it to Phase 3.
- **B3.5 — `themes.list` has 8 entries and equals `THEMES`' name set.**
  `[deferred → C3]` *Executed for the premise:* `theme.ts:8-17` holds 8 names
  and `modal.test.ts:134-136` already asserts
  `Array.isArray(barrel.THEMES) && barrel.THEMES.length === 8`.
- **B3.6 — the picker contains no `innerHTML`.**
  `grep -c 'innerHTML' web/shared/ts/theme.ts` = 0, rc=1. `[deferred → C3]`
  *Executed for the contrast:* the donor does it at
  `web/obsidianoid/js/app.ts:438`.
- **B3.7 — no `rgba(255,255,255` appears in `components.css`.**
  `[deferred → C3]` — the FRD `:258-261` requirement; also caught
  independently by clause 7.
- **B3.8 — pre-paint.** todo `index.html` **and** todo `compare.html` stamp
  `data-theme` before the first paint; verified in a browser with the page's
  own `document.documentElement.dataset.theme` read from a `<head>`-position
  script. `[deferred → C6a]` The `shell.js` module tag **cannot** satisfy
  this: `type="module"` is deferred by specification, so the inline bootstrap
  is load-bearing, not belt-and-braces (Step 6.3, ledger row 16).

  > **Corrected in v2: the sampler is deliberately *not* in this set**, and
  > v1 put it here by reflex. Two facts decide it. (1) The sampler has no
  > persisted theme to restore — `main.ts:77` *reads*
  > `document.documentElement.dataset.theme` and never writes it, and
  > `buildThemeSelect` calls `setTheme` only from the `change` handler
  > (`:78`), so there is nothing to stamp early. (2) The unstamped page is
  > not unstyled: `web/shared/css/themes.css:15` is the two-selector list
  > `:root, [data-theme="dark"]`, so an unstamped document resolves the full
  > `dark` palette from its `:root` half. Demonstrating exactly that default
  > is B10.5's purpose, so stamping the sampler would *delete* a covered
  > behaviour.
  >
  > **todo is different on both counts, and precisely how matters.** It
  > persists a choice (`theme.js:13`, key `'todo-theme'`), and `todo.css`
  > declares its 13 names only under `[data-theme="dark"]` `:3-17` and
  > `[data-theme="light"]` `:19-33` with **no** `:root` fallback — so on todo
  > an *unstamped* document is genuinely unstyled, not defaulted. Both pages
  > therefore carry a **static** `data-theme="dark"` in markup
  > (`index.html:2`, `compare.html:2`) as the floor, and `theme.js` — a
  > head-position **classic** script (`index.html:20`) whose
  > `applyTheme(getPreferred())` runs at `:19` — corrects it to the stored
  > choice before paint. C6a must preserve *both* halves: keep the static
  > attribute (removing it and trusting the inline block would make a script
  > error render an unstyled page) **and** keep a pre-paint corrector. The
  > deferred `shell.js` can only be the second half's replacement if it is
  > not deferred, which it is — hence the inline block.
  >
  > One consequence v1 missed: `theme.js:6-9` also sets `#theme-icon`'s
  > FontAwesome class inside the same pre-paint call. If the inline block
  > stamps only the attribute and leaves the icon to `shell.js`, the icon —
  > not the palette — becomes the flashing element. **B4.12** owns the icon
  > handle on both pages; the inline block is the natural place for its
  > initial class, and Step 6.3 says so.
- **B3.9 — obsidianoid's per-vault persistence still works.**
  `[deferred → C5]` Switch vault, pick a theme, switch away and back: the
  per-vault choice is remembered. This is the behaviour FRD `:250-252`
  requires be preserved.
- **B3.10 — `reresolve()` exists, is public, and re-reads a changed
  `storageKey()`.** `[deferred → C3]` Unit case: construct a `ThemeManager`
  whose `storageKey` closure reads a mutable variable, seed two different
  stored values under the two keys, flip the variable, call `reresolve()`,
  and assert the stamped `data-theme` follows the *new* key. Second case:
  `reresolve()` **does not write** storage — the stored value under the new
  key is unchanged afterwards, so re-resolution cannot silently overwrite a
  user's choice for the vault being switched to.

  > **Why this criterion exists at all.** Without it the method is untested
  > API, and **both** its callers are in a different module with no unit
  > suite — so a regression would surface only as "the theme didn't change
  > when I switched vaults", which is precisely the class of bug FRD `:253`
  > singles out as must-not-regress. The first case is also the only test in
  > the plan that exercises a `storageKey` **closure** rather than a constant,
  > which is the distinction B3.3 does not draw.
  >
  > ***v5 — "its one caller" was wrong, and the second caller is the one this
  > criterion's second case is really for (Critic F-B item 3).*** v4 named
  > `app.ts`'s `switchVault` as the sole caller. There are **two**, and they
  > are the two moments `state.activeVault`'s meaning changes:
  >
  > | Caller | Site | What it replaces |
  > |---|---|---|
  > | `fetchVaults()` | `js/app.ts:479` | the **initial** per-vault resolution, `setTheme(…, false)` |
  > | `switchVault()` | `js/app.ts:494` | the per-switch resolution, `setTheme(…, false)` |
  >
  > Both original calls pass `persist = false`, which is what case 2 above
  > pins. The `fetchVaults` caller is the *stricter* client of the two: it runs
  > on **first load**, when storage may be empty and `serverDefault()` is the
  > step that answers — so a `reresolve()` that wrote storage would convert a
  > server-side default into a permanent user choice on every new browser, in
  > the one code path a developer's own already-populated `localStorage` never
  > exercises. §5 Step 5.2 carries that argument; **B2.11** asserts it at C5
  > against the real module. This criterion is its C3 half: the method must
  > *be* non-writing before anything relies on it being non-writing.

### B4 — FR-4: HamburgerMenu, and the preservation checklist

- **B4.1 — a11y floor, all three, asserted in the unit suite and re-checked
  in the browser:** `aria-expanded` flips; Escape closes; focus moves in on
  open and returns to the trigger on close; Tab is trapped.
  `[deferred → C4]` *Executed for the premise:* `modal.ts:65-96` already
  implements the trap being reused, and §4.4 records that **none** of todo's
  or obsidianoid's current implementations has any of the three.

- **B4.2 — the preservation checklist, instantiated.** Every row must be
  reachable and functional after its commit. `[deferred → C4/C5/C6b]` —
  the sampler rows at C4, obsidianoid's at C5, todo's at C6b, except todo
  row **c3**, whose topbar half lands at C6a (B4.12) and whose drawer half
  lands at C6b.

  **sampler** (net-new; the row exists so the gallery exercises every item
  kind):

  | Item kind | Registered as | Check |
  |---|---|---|
  | action ×2 | `{id,label,onSelect}` | click fires the handler, drawer closes |
  | link ×1 | `{id,label,href}` | renders an `<a href>`, keyboard-activatable |
  | separator | `{separator:true}` | `.ui-menu-separator` present, not focusable |
  | section header | `{section:"…"}` | `.ui-menu-label`, not focusable |
  | `render` slot | `{id,render}` | a `<select>` mounted verbatim; still the same node after close/open |
  | `when()` guard | `{…,when}` | item disappears when the guard flips, on the next open |
  | themePicker | `themePicker:true` | **8 swatches with 8 DISTINCT computed `background-color` values, read simultaneously from one open picker**; selection stamps `data-theme` |

  > **The swatch assertion is `getComputedStyle`, not markup inspection, and
  > it is the criterion ADR-015 lives or dies by.** The mechanism is one CSS
  > rule (`.ui-theme-swatch { background: var(--color-primary); }`) plus
  > `data-theme` stamped on each swatch element, which works *only* because
  > `web/shared/css/themes.css`'s eight blocks are keyed on **bare**
  > `[data-theme="x"]` attribute selectors rather than `:root`-anchored
  > `:root[data-theme="x"]` ones — seven of them exactly so (`:38`, `:61`,
  > `:82`, `:103`, `:124`, `:145`, `:167`) and the eighth as the two-selector
  > list `:root, [data-theme="dark"]` (`:15`), whose attribute half is equally
  > bare and whose `:root` half is what supplies the unstamped default. So a
  > stamped descendant re-resolves `--color-primary` for its own subtree. A
  > markup-level check ("eight `<span class="ui-theme-swatch">` elements
  > exist") would pass on the broken variant where all eight render the
  > *active* theme's colour, which is the exact failure mode of the rejected
  > option 4C. The test is therefore: open the picker once, collect
  > `getComputedStyle(el).backgroundColor` for all eight, and assert the set
  > has **cardinality 8**. The eight authored values are pairwise distinct
  > (`#7c3aed`, `#01696f`, `#7c6af7`, `#4dbb6e`, `#5b9cf6`, `#f0a04a`,
  > `#e05c7a`, `#3fbf9c`), so cardinality 8 is achievable and any collapse is
  > a real defect. Repeated per host: sampler (C4), obsidianoid (C5). todo is
  > exempt — its picker ships 2 (B4.10), and the assertion there is
  > cardinality **2**.

  **obsidianoid** (FRD `:301` — the theme panel is its *only* current
  content):

  | Current control | Today | Registered as | Check |
  |---|---|---|---|
  | Theme swatch panel | `index.html:61-64` + `js/app.ts:433-442` | standard `themePicker` section | 8 swatches, **8 distinct computed backgrounds** (above), active state marked, persists per vault (B3.9), survives a vault switch (B3.10) |
  | `#btn-hamburger` | `index.html:54-56`, `js/app.ts:449` | `mountTrigger` | same glyph position; now `aria-expanded` |
  | Topbar actions: autosave, mode, save, new note, git sync | `index.html:34,38,42,46,50` | **unchanged, stay in the topbar** | all five still present and functional; FRD `:301` requires exactly this |
  | Vault selector | `index.html:71` | **unchanged** | still populates and switches |

  **todo** (FRD `:300`):

  | # | Current control | Today | Registered as | Check |
  |---|---|---|---|---|
  | 1 | Subject `<select>` | `index.html:205`, `onchange="changeSubject()"` → `js/todo.js:241` | `render` slot, same `id` | options populate; changing it switches subject |
  | 2 | List `<select>` | `:210`, → `js/todo.js:236` | `render` slot, same `id` | changing it switches list |
  | 3 | Move-to-subject `<select>` | `:215` | `render` slot, same `id` | options populate |
  | 4 | Move List button | `:216`, → `js/todo.js:453` | same slot as #3 | moves the list |
  | 5–8 | Columns checkboxes votes/period/next_due/cooldown | `:224,:228,:232,:236`, → `:62-70` | one `render` slot holding all four | each toggles its column via `todo.css:443-446`; state round-trips through `POST /config/columns` (`:65-69`) and survives reload (`:72-76`) |
  | 9 | Vote Cooldown number input | `:246-247`, → `:32-40`; `min=1 max=120` | `render` slot | value persists via `POST /config/settings`; bounds preserved |
  | c1 | "Columns" group label | `:222` | `section` header | present |
  | c2 | Two dividers | `:219`, `:241` | `separator` ×2 | present |
  | c3 | Theme toggle (currently **outside** the menu) | `:179-181` | stays in the topbar **and** the drawer gains `themePicker` | both work, both backed by one `ThemeManager` (FRD `:300`) |

- **B4.3 — `render` slots mount verbatim**: the node handed to the host is
  `===` the node retrieved by `document.getElementById` afterwards. **The
  assertion runs before any `open()` has been called on the instance, so a
  drawer built lazily on first open fails here.** `[deferred → C4]`

  > **The ordering clause is new in v5 FINAL** *(Critic finding 6)*. §8.5's
  > pre-mortem Scenario 2 pre-committed to it — it claims this criterion is
  > what "makes it structural" that construction mounts eagerly — and the
  > clause did not exist: B4.3 was one sentence with no ordering constraint,
  > and the words "eager" and "lazy" appeared nowhere in the plan outside
  > Scenario 2 itself. A `HamburgerMenu` that deferred all DOM work to the
  > first `open()` passed B4.3 as written, because the test would simply have
  > opened the drawer first. Why it matters: todo's nine controls keep their
  > inline `onchange` handlers and are found by `id` from `js/todo.js`
  > (§5 Step 6.2), so `applyColConfig()` and `loadColConfig()` run against the
  > live document **whether or not the drawer has ever been opened**. Lazy
  > construction turns those into silent no-ops until the user opens the
  > drawer once — a defect with no visible symptom and no other criterion
  > pointed at it.
- **B4.4 — `when()` is re-evaluated per open**, not cached at construction.
  `[deferred → C4]`
- **B4.5 — `addItem`/`removeItem`/`updateItem` resolve by `id`** and are
  no-ops on an unknown `id` rather than throwing. `[deferred → C4]`
- **B4.6 — every label arrives via `textContent`.**
  `grep -c 'innerHTML' web/shared/ts/menu.ts` = 0, rc=1. `[deferred → C4]`
- **B4.7 — `destroy()` removes every listener it added**, proved by a
  counting stub. `[deferred → C4]`
- **B4.8 — the trap predicate has one definition.**
  `grep -rn 'a\[href\], button:not' web/shared/ts/ | wc -l` = 1, and the one
  hit is the extracted private helper. `[deferred → C4]` *Executed:* it is 1
  today, at `modal.ts:68`.

  > **v2 — the command form was wrong and the criterion was unsatisfiable.**
  > v1 wrote `grep -c '…' web/shared/ts/` — a **`-c` against a directory**.
  > Run in this environment it recurses and prints one `path:count` line *per
  > file* (`modal.test.ts:0`, `modal.ts:1`, `theme.ts:0`, `index.ts:0`, rc=0),
  > and under GNU grep it errors `Is a directory` with rc=2. Neither output is
  > ever the string `1`, so "= 1" could not be satisfied by a correct tree.
  > `-rn … | wc -l` counts matching **lines across the tree**, which is the
  > quantity the criterion is about. *(Critic M-12.)* The same defect shape is
  > why B9.4 specifies `-rF` explicitly.
- **B4.9 — `prefers-reduced-motion` suppresses the slide.** `[deferred → C4]`
- **B4.10 — todo's shipped picker offers `dark` and `light` only** this
  phase. `[deferred → C6b]` This is **forced, not stylistic**: `todo.css`
  declares its 13 bare tokens in only `[data-theme="dark"]` `:3-17` and
  `[data-theme="light"]` `:19-33`, and there are **74** `var(--…)` consumers
  across the sheet, so stamping any of the other six shared names matches
  neither block and all 74 fall back to their invalid initial values — an
  unstyled page, not "mixed chrome". Asserted by enumerating the picker's
  rendered options (exactly 2) rather than by reading `shell.ts`. §5 Step 6.3
  carries the full argument and the §11 item that widens it.
- **B4.11 — C6b's drawer diffs are exactly D-8…D-14, plus D-19 where the
  fixture triggers it, and nothing else.**
  `[deferred → C6b]` Drawer **open** and closed, `dark` and `light`, captured
  against the **C6a** tree (not the phase base) so a theming change cannot be
  read as a drawer change — which is the whole point of the split. Two
  explicit sub-assertions, because they are the two places a diff would be
  easy to wave through:
  1. the winner-row section `todo.css:411-427` (whose four rules are
     `:414-427`) is **byte-untouched** in CSS and **pixel-unchanged** in both
     themes. It is the one place in todo where an `!important` background
     fights the token system, so a drawer edit that perturbed it is a real
     regression;
  2. the `box-shadow` direction change (D-12: `4px 0 24px rgba(0,0,0,.35)`
     → `var(--shadow-md)`, i.e. rightward-directional → downward-diffuse) is
     **present and expected**; it is listed so the capture is not filed as
     "no diff" when a diff is in fact required.
  3. **D-19 is the one attributable delta whose *absence* is also a pass.**
     *(New in **v5 FINAL**, Architect A-3.)* Weight-700 glyphs re-face from
     Google's closest-match 600 to shared `fonts.css`'s exact-match
     InterVariable, and their **only** consumer is
     `web/todo/js/todo-utils.js:950`'s `<strong><b>` on periodic items that
     are due now — so whether any pixel moves depends on the fixture's data
     and clock, not on the code. This criterion therefore accepts either
     outcome: if bold text in the main list changes weight, it is attributed
     to D-19 and C6b continues; if the fixture has no due periodic item,
     nothing changes and that is equally correct. No other D-row has this
     shape, and it is written here so the asymmetry is a stated decision
     rather than an argument at the boundary. §5 Step 6.2's option-3 residual
     note carries the derivation; §5 Step 6.2a carries the row.
- **B4.12 — `compare.html`'s theme toggle works and no undefined global
  survives on either page.** `[deferred → C6a]` Three assertions, all on
  `compare.html`, which v1 never named:
  1. neither page carries the attribute any more:

     ```sh
     ! grep -qF 'onclick="toggleTheme()"' web/todo/index.html web/todo/compare.html
     ```

     *Substitute:* it is present **once in each** today — `index.html:179`,
     `compare.html:41`;
  2. the name has no surviving occurrence anywhere under `web/todo/`,
     definition or call:

     ```sh
     n=$(grep -rhoE '\btoggleTheme\b' web/todo/ | wc -l)
     [ "$n" -eq 0 ] || { echo "toggleTheme survives: $n occurrence(s)"; \
                         grep -rnE '\btoggleTheme\b' web/todo/; exit 1; }
     ```

     `theme.js` is deleted at C6a and `window.toggleTheme` (`theme.js:21-26`)
     goes with it, so leaving either `onclick` in place would be a live
     button invoking an undefined global on **both** pages.
     *(Architect B5 / Critic M-1 — the convergent blocker.)* ***v5 — the
     command was replaced, not the assertion.*** v4 wrote
     `grep -rc 'toggleTheme' web/todo/`, which prints **one count per file
     including the zeros** and therefore reports a wall of `file:0` lines
     that a reader has to scan for a non-zero — it states nothing an
     automated boundary check can act on, and `grep -c`'s per-file output
     order is not argv order on this platform (demonstrated under **B5.8**).
     The `-rho` form totals occurrences instead, and the failure branch
     prints the surviving lines. *Executed for the premise:* **3** today
     (`index.html:179`, `compare.html:41`, `theme.js:21`) — matching
     §13(a6)'s count for this symbol exactly.
  3. clicking `#theme-toggle` on `compare.html` flips `data-theme` between
     `dark` and `light`, swaps `#theme-icon`'s class (`fas fa-moon` ↔
     `fas fa-sun`), and the choice survives a reload — i.e. `compare.html`
     has its **own owned path** through `shell.ts`, not a borrowed one.
     `compare.html` loads neither `todo.js` nor `todo-utils.js` (§4.8), so
     nothing else on that page can supply the behaviour.
- **B4.13 — the shared stylesheet is linked on exactly one todo page, in the
  right position, and the cache-buster moved.** `[deferred → C6b]` *(New in
  v5 — the criterion for §5 Step 6.2's two new bullets, which v4 had no step
  and therefore no criterion for.)*

  ```sh
  # 1. exactly one shared link, on index.html only
  [ "$(grep -c '/shared/dist/shared.css' web/todo/index.html)"   = 1 ] || exit 1
  [ "$(grep -c '/shared/dist/shared.css' web/todo/compare.html)" = 0 ] || exit 1

  # 2. it precedes the Google-Fonts group — the font-cascade post-condition
  s=$(grep -n '/shared/dist/shared.css' web/todo/index.html | cut -d: -f1)
  g=$(grep -n 'fonts.googleapis.com' web/todo/index.html | head -1 | cut -d: -f1)
  [ "$s" -lt "$g" ] || { echo "shared.css at :$s is not before fonts at :$g"; exit 1; }

  # 3. …and still precedes todo's own sheet, so todo.css cascades last
  t=$(grep -n 'css/todo.css' web/todo/index.html | cut -d: -f1)
  [ "$s" -lt "$t" ] || exit 1

  # 4. the cache-buster moved on both pages, and no ?v=19 survives
  [ "$(grep -c 'css/todo.css?v=20' web/todo/index.html)"   = 1 ] || exit 1
  [ "$(grep -c 'css/todo.css?v=20' web/todo/compare.html)" = 1 ] || exit 1
  ! grep -rqF 'todo.css?v=19' web/todo/
  ```

  *Executed for the premise, on today's pre-C6b tree:* part 1 gives **0** on
  `index.html` (so the criterion fails, correctly — the link does not exist
  yet); part 4's last line fails because `?v=19` is present twice
  (`index.html:13`, `compare.html:13`); and the landmarks parts 2 and 3
  compare against are already in place — `head -1` on the
  `fonts.googleapis.com` grep returns **`:8`**, the `preconnect`, not `:10`'s
  stylesheet, which is the stricter of the two and the right one: the new
  link must precede the whole Google group, not merely its stylesheet. And
  `css/todo.css` is at **`:13`**.

  > **Why the ordering is asserted by line number rather than by rendering.**
  > The property that matters is "Google's `Inter` face is declared after
  > shared `fonts.css`'s", and the honest way to check *that* is to read a
  > computed `font-family` and the resolved font file in a browser — which is
  > **B4.11**'s pixel window, and under the user's best-effort ruling on
  > pixel evidence it is corroborating rather than decisive. Line order in
  > the `<head>` is the same fact one level up, it is checkable by a command
  > at the boundary, and it is the thing an executor can get wrong by
  > appending the link to the end of the group without thinking about fonts
  > at all. Parts 2 and 3 together pin the link into the **one** slot that
  > satisfies both constraints: after nothing, before the fonts, before
  > `todo.css`.
  >
  > **And the line-order check is not a complete statement of the font
  > outcome, which is why the residual is declared rather than asserted
  > here.** *(**v5 FINAL**, Architect A-3.)* Correct line order makes Google's
  > faces win at every weight **both** sheets declare — and `todo.css`
  > requests only 400, 500 and 600, all three of which Google's
  > `wght@400;500;600` supplies exactly. At **700** Google has no face and the
  > shared variable face (`font-weight: 100 900`) does, so the exact match
  > wins regardless of declaration order: line position cannot prevent it, and
  > this criterion does not pretend to. That residual is **D-19**, derived in
  > §5 Step 6.2's option-3 note and attributed by **B4.11** part 3. Stated
  > here so a reader who checks parts 2 and 3 green does not conclude the font
  > rendering is unchanged — it is unchanged for every weight the *stylesheet*
  > asks for, and changed for the one weight the *markup* asks for.

### B5 — FR-5: Toast

- **B5.1 — the donor's three properties survive.** One `aria-live` region
  for N toasts; `error` is sticky (`durationMs === 0`); `message` is rendered
  by `textContent`. `[deferred → C2]` *Executed for the donor:*
  `web/certmachine/js/toast.ts:20-24`, `:32-33`, `:56`.
- **B5.2 — the live region's attributes are exactly `role="status"` and
  `aria-live="polite"`.** `[deferred → C2]`
- **B5.3 — markup in a message is not interpreted**, asserted with the
  `"<b>evil</b> & <script>…"` string `modal.test.ts:85` already uses.
  `[deferred → C2]`
- **B5.4 — `dismiss()` is idempotent** and clears its timer.
  `[deferred → C2]`
- **B5.5 — exactly one stack element exists** after three `showToast` calls.
  `[deferred → C2]`
- **B5.6 — the close button carries `aria-label="Dismiss"`** and its glyph
  is a text node. `[deferred → C2]`
- **B5.7 — no colour literal in the toast CSS**; caught by clause 7.
  `[deferred → C2]`
- **B5.8 — obsidianoid's 16 call sites all resolve, and neither file still
  declares the name.** `[deferred → C5]`

  ```sh
  a=$(grep -oE 'showToast\(' web/obsidianoid/js/app.ts     | wc -l)   # 11
  t=$(grep -oE 'showToast\(' web/obsidianoid/js/threads.ts | wc -l)   # 5
  [ $((a + t)) -eq 16 ] || { echo "call sites moved: $a + $t"; exit 1; }
  ! grep -qE '(^|[^a-zA-Z])function showToast\(' \
        web/obsidianoid/js/app.ts web/obsidianoid/js/threads.ts
  grep -c 'import {[^}]*showToast' web/obsidianoid/js/app.ts          # 1
  grep -c 'import {[^}]*showToast' web/obsidianoid/js/threads.ts      # 1
  ```

  > ***v5 — this criterion's command could not produce the number it
  > claimed.*** v4 wrote `grep -c 'showToast' <two files>` and said it
  > "totals 16 call sites + 1 import each". `grep -c` over two files prints
  > **one count per file and no total** — and, run on this tree, prints them
  > **in the opposite order to argv** (`threads.ts:7` then `app.ts:12`), so
  > even reading the two numbers by position is unsafe. It also counted
  > lines rather than occurrences, and counted the comment at
  > `threads.ts:14` and the `declare` at `:15`. *Executed for the premise,
  > on today's pre-C5 tree:* `showToast(` occurs **12** times in `app.ts`
  > (11 calls + the definition at `:56`) and **6** in `threads.ts` (5 calls
  > + the `declare` at `:15`), so the total is **18** today and **16** after
  > C5 — v4's form, by contrast, expected 16 and would have read 12 or 7
  > depending on which line of output was believed. The deletions that make
  > 18 into 16 are exactly the two definitions, which is why the `! grep -q`
  > line asserts both are gone in the same breath.

  *Guaranteed by (step): §5 Step 5.2 — both the `@shared` imports and the
  deletion of `threads.ts:15`'s `declare function showToast` are sub-edits of
  that step (its `window.ThreadsView` bullet names the `declare` deletion
  explicitly, since making `threads.ts` a module is what forces the
  `declare global` rewrite).*

### B6 — FR-8: the Q8(a) carve-out

- **B6.1 — positive.** Unauthenticated `GET /shared/dist/shared.css` on a
  protected module → 200, `Content-Type: text/css`. `[deferred → C8]`
- **B6.2 — positive.** Unauthenticated `GET` of a real
  `/shared/public/fonts/**/*.woff2` → 200. `[deferred → C8]`
- **B6.3 — positive. *(Flipped in v5.)*** Unauthenticated
  `GET /shared/dist/shared.mjs` on a protected module → 200, and
  `Content-Type` starts with `text/javascript`.
  `[deferred → C8]`

  > ***Why this flipped.*** Through v4 this was a **negative** probe asserting
  > **401**, on the stated ground that "Q9 is open; the `.mjs` half is
  > deliberately not carved out". The user closed Q9 on 2026-09-16 — *"yes,
  > the login page should use the theme previously selected."* A login page
  > that restores a stored theme has to run the code that reads the store,
  > and that code ships in the barrel, so the `.mjs` path shape joins the
  > allowlist and the same request that had to 401 now has to 200. The probe
  > is not deleted and not weakened: it keeps its number, keeps its slot in
  > C8's acceptance list, and asserts the opposite outcome. §9 Q9 records the
  > ruling; §5 Step 8 carries the allowlist edit.
  >
  > *Guaranteed by (step): §5 Step 8 — 8.1 lists the three allowed path
  > shapes and 8.3 lists the probe outcomes; this criterion asserts 8.3's
  > third line.*
  >
  > **Why the `Content-Type` half is an assertion and not a new definition.**
  > The carve-out delegates to the same `static.SharedHandler` the dispatcher
  > already uses (Step 8.2), and that handler's final act is
  > `http.ServeContent(w, r, rel, …)` (`internal/platform/static/shared.go:91`),
  > which derives the type from the extension. `.mjs` → `text/javascript` is
  > therefore a **Phase-1 fact with a Phase-1 test of record**:
  > `TestSharedMjsContentType` in
  > `internal/platform/static/shared_test.go:232-244` already asserts 200 plus
  > the `text/javascript` prefix against the handler directly. B6.3 asserts the
  > same two things **through the gate**, which is the part C8 changes — it
  > does not re-establish the MIME mapping, and it must not grow its own
  > expected-type table.
- **B6.4 — negative.** Unauthenticated `GET /shared/ts/theme.ts` → 401 (a
  source path, not `dist/`), `POST /shared/dist/shared.css` → 401,
  `POST /shared/dist/shared.mjs` → 401 (the carve-out is GET-only, and v5's
  flip must not turn the `.mjs` shape into a method-agnostic hole), and
  **`GET /shared/public/fonts/inter/OFL.txt` → 401** — a real committed file
  under shape 2's prefix that does **not** carry shape 2's `.woff2` suffix, so
  it is the one probe that fails if the carve-out is written as
  `HasPrefix("/shared/public/")` and drops the suffix half. `[deferred → C8]`
  *(The fourth probe is new in **v5 FINAL**, Critic finding 5; §5 Step 8.3
  probe 7 carries the derivation and the reason every other criterion in §7
  is blind to that implementation.)*
- **B6.5 — no regression.** With a valid session all **seven** probed paths
  behave exactly as they do before C8, and the three existing carve-outs at
  `gate.go:129`, `:135`, `:148` are unchanged. `[deferred → C8]`
  *Executed:* all three line citations verified in this tree —
  `:129` is the `/healthz` guard, `:135` `/api/auth/mode`, `:148`
  `/api/auth/whoami`, with `protected :=` at `:157`, so the fourth carve-out
  has an unambiguous insertion point.

### B7 — FR-7: build standardization for todo

- **B7.1 — `EXPECTED_ARTIFACT_COUNT` is 16 and the generator prints 16
  paths**, one of them `web/todo/js/shell.js`, and it is tracked by git.
  `[deferred → C6a]` *Executed:* `scripts/descriptors.mjs:162` reads 15 today
  and `gates/artifacts.mjs:83-86` is the comparison.
- **B7.2 — `tsconfig.json:15` has 12 globs** and `tsc --noEmit` covers
  `web/todo/js/shell.ts`. `[deferred → C6a]` *Executed:* 11 globs today.
- **B7.3 — two consecutive `npm run build` runs produce identical bytes**
  for all 16 artifacts. `[deferred → C6a]`
- **B7.4 — no `.js` file under `web/todo/js/` other than `shell.js` is
  touched by the build**: the other **18** are not descriptor entries and
  their bytes are unchanged. `[deferred → C6a]`

  > **v2 — the count.** `web/todo/js/` holds **19** files today
  > (axios.min.js, base-util.js, bootstrap.js, compare.js, exercise.js,
  > jquery-ui.js, jquery.js, kbc.js, marked.min.js, menuserver.js,
  > navcontrols.js, onoff.js, panels.js, popper.js, slideshow.js, theme.js,
  > todo-utils.js, todo.js, utils.js). C6a **deletes** `theme.js` and **adds**
  > `shell.js`, so at the C6a boundary the directory still holds 19, of which
  > `shell.js` is the one build output and **18** are untouched. v1 said "the
  > other 19", which would only be right if nothing had been deleted. C7 then
  > removes 12 of the 18, leaving 6 legacy files plus `shell.js`.
- **B7.5 — `shell.js` is the only artifact under `web/todo/`, and it is
  committed, not gitignored.** `[deferred → C6a]` Three parts, because todo
  is the first module in the repo where a build output lands in a directory
  full of hand-written, git-tracked `.js`:
  1. `node scripts/list-artifacts.mjs | grep -c '^web/todo/'` = 1, and the
     one line is `web/todo/js/shell.js`;
  2. `git check-ignore -q web/todo/js/shell.js` exits **1** (not ignored) and
     `git ls-files --error-unmatch web/todo/js/shell.js` exits 0 — the repo
     commits its build outputs (that is what `EXPECTED_ARTIFACT_COUNT` and
     the clean-tree gate are built on), so an accidentally-ignored artifact
     would make `make web-verify` pass while the served tree is stale;
  3. `shell.js` contains a **column-0** `import … from
     "/shared/dist/shared.mjs"`, i.e. `TOP_LEVEL_IMPORT`
     (`bundle-shape.mjs:38`) matches it — which is a `/m` regex, so the
     assertion is *"some line, anchored at column 0"*, not *"line 1"*. In
     practice the import is on **line 2**: esbuild writes a `// <input path>`
     banner as line 1 of every output, verified in both existing bundles
     (`web/sampler/js/bundle.js:1` is `// web/sampler/js/main.ts`,
     `web/taskmaster/js/bundle.js:1` is `// web/taskmaster/js/ui/modal.ts`,
     and the import is `:2` in each). So `todo` really is in the `--require`
     set and inspected, which B9.1 asserts and `Makefile:62` enables.

     > **v3 correction (Architect N3).** v2 wrote "`shell.js`'s **first
     > line** matches …". That criterion **cannot pass** while the gate it
     > cites **does** pass — the inverse of P-III: not a gate that cannot
     > fail, but a criterion that cannot succeed. The `/m` flag is the whole
     > difference, and v2's wording quietly promoted the gate's line-anchored
     > search into a first-line requirement that esbuild's banner makes
     > impossible. Reworded to the regex's own semantics; a `grep -n` for the
     > import line is the executable form, and `head -1` is **not**.

  *Substitute:* today `list-artifacts.mjs` prints **0** lines under
  `web/todo/`, and all 19 files there are hand-written and tracked — so the
  "before" state is unambiguous.

### B8 — FR-9: deletion

- **B8.1 — exactly 40 files are deleted**, and the deleted set equals §5 Step
  7.1's enumeration. `[deferred → C7]` *Executed for the premise:* the set
  was derived two independent ways — byte-identity against
  `web/menuserver/` and reference-closure from the two live pages.
- **B8.2 — the two-sided deletion proof** of §5 Step 7.3 passes in the
  guarded-`grep` form, **with the loop accumulator and the two-segment path
  match**. `[deferred → C7]` The accumulator is not a style preference: v1's
  `done; rc=$?` read the last iteration's status only, so a live reference in
  any of the first 39 files was invisible. Step 7.3 carries both fixes and
  the reasoning. *Substitute:* the 40-path set itself was derived two
  independent ways and intersected (B8.1), so the proof is checking a set
  that already has independent support — which is why a silent accumulator
  bug would have been so easy to miss.
- **B8.3 — the 11 live pshelper files still exist** and the live pages still
  reference them. `[deferred → C7]`
- **B8.4 — the live app is unchanged.** todo's index loads, the drawer
  opens, the table renders, `compare.html` round-trips. `[deferred → C7]`

### B9 — ADR-001: bundle shape

- **B9.1 — A9.1 holds for every `sharedConsumer` output, and the set is
  asserted by name rather than by size.** Each artifact **contains** a
  column-0 `import … from "/shared/dist/shared.mjs"` — `bundle-shape.mjs:38`'s
  `TOP_LEVEL_IMPORT` is a `/m` regex, so "anchored at column 0 on *some*
  line", not "on line 1"; esbuild's `// <input path>` banner occupies line 1
  of every output and the import is on line 2 (N3, B7.5 item 3) — **and** the
  gate is invoked `--require=` with every descriptor the boundary expects, so
  a descriptor that quietly lost the flag fails by name.
  `[deferred → C1/C5/C6a]` Per-boundary lists, which is the whole content of
  **G16**, together with the **exact PASS string** each boundary must print:

  | Boundary | `--require=` | Descriptors | Outputs inspected | Expected PASS line, verbatim |
  |---|---|---|---|---|
  | C1 | `sampler,taskmaster` | 2 | 2 | `bundle-shape: PASS — 2 sharedConsumer bundle(s) inspected` |
  | C5 | `sampler,taskmaster,obsidianoid` | **3** | **4** | `bundle-shape: PASS — 4 sharedConsumer bundle(s) inspected` |
  | C6a–C8 | `sampler,taskmaster,obsidianoid,todo` | **4** | **5** | `bundle-shape: PASS — 5 sharedConsumer bundle(s) inspected` |

  > **The PASS line is pinned, not inferred (Architect N5, v3).** The number in
  > that string switches *what it counts* at C5. Today `bundle-shape.mjs:101`
  > prints `inputs.length` — the **descriptor** count from `:52` — and through
  > C4 that is indistinguishable from the artifact count, because every
  > `sharedConsumer` is single-output. From C5 they diverge: 3 descriptors, 4
  > artifacts. Step 5.1 therefore changes `:101` to count inspected artifacts
  > **in the same commit** that makes the loop iterate `artifactPaths(d)`;
  > without that, the gate would check 4 outputs and print `3`, exit 0, and
  > satisfy every other word of this criterion while the plan's own table said
  > 4. Pinning the string is what makes the divergence assertable. This mirrors
  > **BX.2**, which pins the barrel gate's PASS line
  > (`check-shared-barrel.mjs:117-119`) for exactly the same reason. The C1
  > row is not a prediction: `node scripts/gates/bundle-shape.mjs` was run in
  > this tree and printed that line, rc 0.

  *Guaranteed by (step): the `--require=` list on each row is written by **§5
  Step 1.4** (the flag itself, plus C1's two names into `Makefile:62`), **§5
  Step 5.1**
  (obsidianoid added, plus the `:101` count change and `artifactPaths(d)`),
  and **§5 Step 6.1** (todo added). The PASS-line wording itself is fixed by
  `scripts/gates/bundle-shape.mjs:101`, which only Step 5.1 edits. **v5** —
  this was the criterion the Architect's back-pointer sweep found asserting
  three boundary states with no step named for any of them.*

  *Executed for the defect:* the gate today prints
  `PASS — 2 sharedConsumer bundle(s) inspected` and exits 0 **both** bare and
  with `--expect-nonempty` (§4.2.1(a) reproduces both runs), so v1's claim
  that `--expect-nonempty` made obsidianoid's inspection checkable was
  false — nothing in the repo passed the flag, and the flag could not fail
  anyway.
- **B9.2 — A9.2 holds:** no `__require(` and no `Dynamic require of` in any
  of them. `[deferred → C5/C6a]`
- **B9.3 — the gate reads derived artifact paths, not `d.out`, and so cannot
  throw on a directory.** `[deferred → C5]` Two assertions: `make gates`
  completes with obsidianoid in the `sharedConsumer` set; and the failure
  mode is a **gate failure with a message** rather than a stack trace, proved
  by pointing one descriptor's entry at a non-existent file and observing a
  named failure.

  *Executed for the defect:* `bundle-shape.mjs:69` is `existsSync(d.out)`,
  which returns **true** for a directory and therefore does not guard, and
  `:73` is `readFileSync(d.out, "utf8")`, which throws an uncaught `EISDIR`;
  `descriptors.mjs:83` is `out: "web/obsidianoid/js/"`.

  > **v2 — reframed.** v1 worded this as "the gate no longer throws on a
  > directory `out`", which reads as though `d.out` should become a file
  > path. It must not: `list-artifacts.mjs:38-39` derives a multi-entry
  > transpile's outputs by joining `d.out` with each entry's basename, so a
  > file path there collapses obsidianoid's two artifacts onto one and
  > `EXPECTED_ARTIFACT_COUNT` can never reconcile. The fix is on the *gate*
  > side — iterate `artifactPaths(d)` (§4.2.1(c), Step 5.1) — and the
  > criterion now says so. *(The Critic proposed the descriptor-side fix;
  > §4.2.1(b) carries the rebuttal with the tree evidence.)*
- **B9.4 — the derivation has one definition.**
  `grep -rF 'replace(/\.tsx?$/' scripts/ | wc -l` = 1, and the one hit is in
  `scripts/artifact-paths.mjs`. *(Fixed-string `-F`: the pattern contains
  `?`, `$` and `/`, and a BRE/ERE version of this grep silently matches
  nothing — which would read as "zero duplicates" instead of "no
  definition".)* `[deferred → C5/C6a]` — **widened from `C5` in v3, and the
  C6a run is deliberate**: C6a adds a fourth `sharedConsumer` descriptor, and
  a new descriptor is exactly the occasion on which someone re-inlines the
  derivation "just for this one case". The invariant is one definition
  *forever*, so it is re-asserted at the only later boundary that adds a
  descriptor. *(Critic C-1 asked whether §6's scheduling of B9.4 at C6a was
  deliberate. Answer: **yes**, and the label now says so.)* *Executed:* it is
  1 today, at
  `scripts/list-artifacts.mjs:26`; `:11` is the prose comment describing it.

### B10 — FR-10: the sampler

- **B10.1 — the `openModal` demo has a focusable control**, so
  `modal.ts:65-71`'s `getFocusable()` returns a non-empty array for it and
  the checklist's Tab/Shift-Tab columns are meaningful in all 8 sections.
  `[deferred → C1]` *Executed for the defect:* `main.ts:137-150` builds only
  a `<p>`.
- **B10.2 — `check-shared-css.mjs` reports 12 clauses** and clause 12 fails
  on an injected stray file under `web/shared/public/fonts/` (proved by
  creating one, running the gate, and deleting it — never committed).
  `[deferred → C1]` *Executed for the premise:* the PASS line says 11 today.
- **B10.3 — every component has a sampler section**: Toasts (C2), Theme
  picker (C3), Hamburger (C4). `[deferred → C2/C3/C4]`
- **B10.4 — `docs/sampler-checklist.md` has a row per new widget per theme**,
  8 themes × {toast tones, drawer keyboard pass}. `[deferred → C4]`
- **B10.5 — the sampler still carries no `data-theme` on `<html>`**, so the
  `:root` default remains the thing being demonstrated.
  `[re-asserted → C2/C3/C4]` — the three commits that edit
  `web/sampler/index.html`; adding a `data-theme` there while wiring a new
  section is the specific way this invariant would be lost. *(New label in
  v3 — Critic C-1: it was scheduled at C2/C3/C4 in §6 with no label.)*
  *Executed:* `web/sampler/index.html:2` is `<html lang="en">`.
- **B10.6 — the sampler's second copy of Table T1 grows with the first.**
  *(New in v5 — required by §5 Step 1.5; §13(b) duplication 5.)*
  `[deferred → C1]` After C1:

  ```sh
  # the declared source of truth — declarations only, never comment prose
  node scripts/check-shared-css.mjs                                    # rc 0
  grep -cE -- '^[[:space:]]+--color-[a-z0-9-]+:' \
      web/shared/css/themes.css                                        # 144 (8 × 18)
  grep -cE -- '^[[:space:]]+--color-surface-dynamic:' \
      web/shared/css/themes.css                                        # 8

  # the sampler's mirror — same 18 names, both copies
  grep -c -- '"--color-' web/sampler/js/main.ts                        # 18
  grep -c -- '--color-surface-dynamic' web/sampler/js/main.ts          # 1
  grep -c -- '--color-surface-dynamic' web/sampler/js/bundle.js        # 1
  ```

  > **Why the `themes.css` count is anchored, and why that anchor is not
  > cosmetic. (v5.)** The obvious form of the first check is
  > `grep -c -- '--color-' web/shared/css/themes.css`, and its **expected
  > post-C1 value of 144 is what that command already prints on today's
  > pre-C1 tree** — because `grep -c` counts *lines*, and the file's header
  > (`:1-10`) plus its eight per-theme block comments (`:12-14` and its
  > siblings) name `--color-*` in prose on exactly 8 lines beyond the 136
  > declarations. A reviewer running the unanchored command before C1 sees
  > 144, reads "pass", and the criterion has told them nothing. Anchored to
  > `^[[:space:]]+--color-…:` the two states are **136 → 144**, which is the
  > number Step 1.5 actually moves. *(Found while deriving this criterion;
  > the same class of near-miss as B1.1 part B's diff anchors.)*

  …and the set-equality that makes the copies *agree* rather than merely
  count the same — the check that would still fail if one name were added and
  a different one dropped:

  ```sh
  a=$(grep -oE -- '"--color-[a-z0-9-]+' web/sampler/js/main.ts \
      | tr -d '"' | sort -u)
  b=$(grep -oE -- '^[[:space:]]+--color-[a-z0-9-]+:' web/shared/css/themes.css \
      | tr -d ' :' | sort -u)
  [ "$a" = "$b" ] || { echo "T1 copies diverged"; diff <(echo "$a") <(echo "$b"); exit 1; }
  ```

  ***Executed (v5), on today's pre-C1 tree:*** both sides yield **17** names
  and the sets are **equal**. Re-run with the `themes.css` side replaced by
  Step 1.5's sandboxed 18-key file and the sampler side left at today's 17,
  it reports **DIVERGED** and `diff` names the single missing entry,
  `--color-surface-dynamic` — i.e. **the exact one-sided landing this
  criterion exists to catch was reproduced, not assumed.** C1 must therefore
  move both sides at once; the count checks above would also catch the
  one-sided landing, while a name-swap (one added, a different one dropped)
  is caught only here.

  > **On the anchors in the set-equality form. (v5.)** Both sides are
  > anchored to the *syntax that makes a name a definition* — a quoted
  > string literal in the sampler's array, a property declaration in
  > `themes.css` — rather than to the bare name. On today's tree the
  > unanchored `themes.css` form happens to yield the same 17 names, because
  > every token the comments mention is also declared; that coincidence is
  > not a property the file is obliged to keep (`tokens.css:1-7` is the
  > counter-example in the same directory: its header names `--shadow-md`,
  > `--overlay-scrim`, `--space-5`, `--space-7` and `--text-xl` while
  > discussing files it does not declare). Anchoring means the check keeps
  > measuring definitions no matter what the prose later says.

  > **Why the sampler needs its own criterion at C1. (v5.)**
  > `web/sampler/js/main.ts:15-33` is a **second definition of Table T1** —
  > 17 entries today, iterated at `:85` to build the swatch grid, mirrored
  > into the tracked artifact `web/sampler/js/bundle.js:4`. Its own comment
  > (`:11-14`) says so: "Table T1 -- the 17 `--color-*` keys every theme in
  > `web/shared/css/themes.css` declares." The duplication is deliberate and
  > declared (§13(b) duplication 5), but it is **not gate-checked**: nothing
  > in `check-shared-css.mjs` reads the sampler's TypeScript, so landing the
  > 18th key in `themes.css` and the gate while leaving the sampler at 17
  > produces a fully green `make check` and a specimen page that silently
  > **omits the new token** — the one page in the repo whose entire job is to
  > show every token. That is a P-III failure in the other direction: a
  > correct-looking system with no criterion able to notice. Three files move
  > together at C1 or none do.
  >
  > The `bundle.js` half is asserted separately because it is a **tracked
  > artifact**: `make web-verify` would catch a stale bundle, but only if the
  > rebuild happens — and an executor editing `main.ts` without rebuilding is
  > the ordinary mistake, not an exotic one.
  >
  > *Guaranteed by (step): §5 Step 1.5, which lists all three files and the
  > eight authored values.*

### BX — cross-cutting

- **BX.1 — clause 12 is a file, not a recipe** (G11). `[re-asserted → C1]`
  — C1 is the commit that *introduces* clause 12 (**§5 Step 1.3**), so it is
  the boundary at which its form is decided; no later commit adds a clause.
  *(New label in v3 — Critic C-1: scheduled at C1 in §6 with no label.
  **v4** — the parenthetical said "Step 5.1", which is C5's descriptor
  plumbing; clause 12 is introduced by **Step 1.3**, the step C1 owns.
  Architect A6. The pointer resolved to a step that exists, which is why
  §13(a5)'s dangling-pointer half could not have caught it — see (a5)'s stated
  limit.)*
  *Executed for the form:* `Makefile:60-64` invokes script files only.
- **BX.2 — the barrel's counts move exactly as §6 says**: 7/6 at C2, **8/7** at
  C3, **9/9** at C4, and `check-shared-barrel.mjs`'s PASS line prints them
  (`:117-119`). `[deferred → C2/C3/C4]`
  *Guaranteed by (step): one allowlist edit per commit, at
  `check-shared-barrel.mjs:25-26` — **§5 Step 2.4** (`showToast` + its two
  types: 6/4 → **7/6**), **§5 Step 3.3** (`ThemeManager` + its config type:
  → **8/7**), **§5 Step 4.3** (`HamburgerMenu` + its two types: → **9/9**).
  §13(b) duplication 3 declares the three intermediate states as deliberate.
  **v5** — added by the back-pointer sweep: the criterion named three numbers
  and no step, which is how a correct implementation could still fail it by
  landing an allowlist edit in the wrong commit.*
- **BX.3 — `export *` never appears in the barrel** (`:45-47`).
  `[re-asserted → C2/C3/C4]` — the three commits that add an export to
  `web/shared/ts/index.ts`; a wildcard is the shortcut someone reaches for
  *while* adding the third one. *(New label in v3 — Critic C-1: scheduled at
  C2/C3/C4 in §6 with no label.)*
  *Executed:* `web/shared/ts/index.ts` is 12 lines of named re-exports.
- **BX.4 — path-scoped clean tree at every boundary**, and **no commit
  contains `tools/baseline-shots/` or `docs/INVENTORY-`.**
  `git show --stat --name-only <sha> | grep -c -e 'tools/baseline-shots' -e 'docs/INVENTORY-'`
  = 0, rc=1, for all **9** commits (C1–C5, C6a, C6b, C7, C8).
  `[deferred → C1…C8]` *Executed for the risk:* those paths are
  dirty/untracked right now (P3), which is exactly why the criterion counts
  commits rather than trusting the sequence.
- **BX.5 — nothing under `web/` changes outside the four exempted prefixes.**
  `git diff --name-only <base>..<head> -- web/ | grep -v -e '^web/sampler/'
  -e '^web/obsidianoid/' -e '^web/todo/' -e '^web/shared/'` = empty, rc=1.
  *(**v5 FINAL**, Critic minor 11: the headline read "outside the **three**
  named ones" over a command that exempts **four** prefixes. `web/shared/` is
  the fourth and is not a module at all — it is the library all three adopt,
  and every one of the nine commits touches it, so a three-prefix reading of
  this criterion would fail at C1. §11 item 16 already counted four; this
  headline was the outlier. The scope limit worth knowing is the **other**
  one, stated there: the command is `-- web/`, so `scripts/`, `Makefile`,
  `tsconfig.json`, the two Go files and the three JSON configs are outside
  its reach entirely.)*
  `[deferred → C5/C6a/C8]` — **widened from `C8` in v3, and the C5/C6a runs
  are deliberate**: the range is `b73d31c..<boundary>`, so each run is a
  *prefix* check over everything committed so far, and the two adoption
  commits are the only ones that touch adopter trees at all — catching a
  stray fourth module at C5 or C6a is worth three commits more than catching
  it at C8. C8 stays as the full-phase run of record, because only there does
  `<head>` cover all nine commits. *(Critic C-1 asked whether §6's
  scheduling of BX.5 at C5/C6a was deliberate. Answer: **yes**; the label now
  says so, and C8 — which §6 had omitted — is restored to the cell.)*
- **BX.6 — `make check` + `gofmt -l` + `go vet ./...` are clean at every
  commit boundary**, per README. `gofmt -l` prints nothing; `go vet` exits 0;
  `make check` (`Makefile:71`) runs `web-verify test-web gates test` and
  exits 0. `[deferred → C1…C8]`
- **BX.7 — no `alert()`/`confirm()`/`prompt()` is added.**
  `grep -rnE '\b(alert|confirm|prompt)\(' web/shared/ts/ web/todo/js/shell.ts`
  = 0, rc=1. `[deferred → C4/C6a]` *Executed:* todo's live JS has none today.
- **BX.8 — `bundle-shape.mjs`'s `--require` is proved by making it fail.**
  `[deferred → C1]` Flip `sharedConsumer` to `false` on
  `descriptors.mjs:77` in the working tree, run `make gates`, and confirm it
  fails **naming `sampler`** and exits non-zero. Revert the flip; nothing is
  committed. *Executed for the contrast:* against today's gate the identical
  flip still exits **0** — `taskmaster` alone keeps the set non-empty — which
  is the demonstration that `--expect-nonempty` never guarded anything.

  > This is the P-III probe for G16, and it is the reason G16 exists as a
  > guardrail rather than a remark: v1 named a flag as its mechanism, the
  > `Makefile` never passed it, and the flag could not have failed if it had.
- **BX.9 — no adopter's TypeScript references an id its HTML does not
  define.** `[deferred → C5/C6a]` For each adopter, extract every id literal
  from `getElementById('…')` and `querySelector('#…')` in its `.ts` sources
  and confirm each appears as an `id="…"` in every HTML file that loads the
  resulting module. `[C5]` covers `web/obsidianoid/js/{app,threads}.ts`
  against `index.html`; `[C6a]` covers `web/todo/js/shell.ts` against **both**
  `index.html` and `compare.html` — and that pair is the point, because
  `compare.html` lacks `#menu-toggle`, `#sidebar` and `#sidebar-backdrop`
  entirely (§4.8), so an unguarded drawer handle would be exactly this defect.

  > **Why a criterion and not a code review note.** Both adopters use
  > **non-null assertions** on their handles — `app.ts:31`, `:40`, `:41`,
  > `:42` are all `document.getElementById('…')!`. The `!` tells `tsc` the
  > value is non-null, so deleting the markup leaves `tsc --noEmit` green and
  > the failure appears only as a runtime `TypeError` on first property
  > access. There is no compile-time protection left to rely on, which makes
  > this the one class of C5/C6a regression that every gate in the repo would
  > pass. *Executed for the defect:* all four assertions exist today, and C5
  > deletes the markup behind three of them (Step 5.2).
- **BX.10 — todo's two inline pre-paint blocks are byte-identical, and there
  is no third copy.** `[deferred → C6a]` *(Re-specified in **v4** — Critic
  F-1 CRITICAL. The v3 form was unsatisfiable; the post-mortem is below.)*
  The block is deliberately duplicated rather than shared (ledger row 16), so
  what can rot is **divergence**, not existence — existence and pre-paint
  *timing* are **B3.8**'s, browser-verified, and are not restated here.
  Step 6.3 item 3 writes each copy wrapped in a sentinel comment pair, and
  this criterion is two `grep`/`sed` assertions over those sentinels:

  ```sh
  # part 1 — the set is exactly todo's two pages
  set -- $(grep -rl 'theme-bootstrap:start' web/ --include='*.html' | sort)
  [ "$*" = "web/todo/compare.html web/todo/index.html" ] \
    || { echo "bootstrap copy set drifted: [$*]"; exit 1; }

  # part 2 — the two copies are byte-identical after indent normalisation
  a=$(mktemp); b=$(mktemp)
  extract() { sed -n '/theme-bootstrap:start/,/theme-bootstrap:end/p' "$1" \
              | sed 's/^[[:space:]]*//'; }
  extract web/todo/index.html   > "$a"
  extract web/todo/compare.html > "$b"
  [ -s "$a" ] && [ -s "$b" ] \
    || { echo "extraction produced nothing — sentinels missing or malformed"; exit 1; }
  diff -u "$a" "$b" || { echo "bootstrap copies diverged"; exit 1; }

  # part 3 — comment-stripped-paste detector: the storage-key literal may
  # appear only inside a sentinel-bearing page.
  bad=$(grep -rl "todo-theme" web/ --include='*.html' \
        | while read -r f; do grep -q 'theme-bootstrap:start' "$f" || echo "$f"; done)
  [ -z "$bad" ] || { echo "storage key outside a sentinel-bearing page:"; \
                     echo "$bad"; exit 1; }
  ```

  Part 1 is what makes the "a fourth copy must join the set" intent a
  **command** rather than a hope: the sentinel is the detector, and `grep -rl`
  over `web/` fails the moment a third page grows one, because the set is
  compared for **equality**, not membership. Part 2 is the divergence check.
  All three parts run at C6a over files already on **§6's C6a touch list**, so
  nothing is widened.

  > **Part 2's `[ -s ]` guard, and why an empty extraction is the dangerous
  > case. (v5, Architect N-3.)** `sed -n '/start/,/end/p'` over a file with no
  > sentinels prints **nothing** and exits **0**. `diff -u` over two empty
  > files also succeeds. So the v4 form's happy path and its
  > sentinels-were-never-written path are **the same green output** — the
  > criterion would certify that two blocks match when neither exists. That
  > is P-III's dual: a criterion that cannot fail in the one state it most
  > needs to catch, since forgetting the sentinel wrapper is a far more
  > likely executor slip than mistyping one of the two copies. Existence of
  > the *behaviour* is **B3.8**'s, browser-verified — but existence of the
  > **sentinels** is nobody's but this criterion's, because nothing else in
  > the plan mentions them.
  >
  > **Part 3 — the comment-stripped paste. (v5, Architect N-4.)** Part 1's
  > detector is the sentinel comment, which makes part 1 exactly as good as
  > the assumption that a third copy would arrive *with* its comments. A
  > minifier, a copy-paste that grabs only the `<script>` body, or an
  > editor's "remove comments" action all produce a third live bootstrap that
  > part 1 cannot see. Part 3 closes it from the other side, by keying on the
  > one string the behaviour cannot work without: the storage-key literal
  > `todo-theme` (`theme.js:2` today; §5 Step 6.3 keeps the bare key and
  > ADR-014's migration deliberately does **not** apply to todo, so the
  > literal is stable across the phase). Any HTML page under `web/` holding
  > that literal without a sentinel is a copy that escaped the mechanism.
  > *Executed on today's tree:* **zero** HTML files contain `todo-theme` (the
  > only occurrence in the repo is `web/todo/js/theme.js:2`), so part 3 is
  > green today and stays green only if C6a writes both copies inside
  > sentinels — it fails on the stripped-paste state it was written for, and
  > it is a genuine guard rather than a tautology because part 1 and part 2
  > cannot fail on that state at all.

  > **User ruling, 2026-09-16 — byte-identity is best-effort.** Asked whether
  > exact byte-identity between the two blocks was worth holding C6a for, the
  > user answered:
  >
  > > "best effort. I have no illusions that there's going to be some modules
  > > that are a bit off.. and we'll correct them after changes are applied."
  >
  > What changes: a part-2 `diff` that is non-empty is **reported and
  > recorded, not fatal** — C6a may land with a noted divergence, and the
  > correction is a follow-up. What does **not** change: the mechanism, all
  > three parts of it. The sentinels still get written, part 1's
  > set-equality still runs, part 3 still runs, and part 2 still runs and
  > still prints its diff. The ruling relaxes the **consequence**, not the
  > **evidence** — and parts 1 and 3 are the two that stay fatal by their
  > nature, because "there is a third copy" and "there is a copy outside the
  > mechanism" are not conditions a later visual pass can discover. A
  > divergence between two blocks shows up as a flash on one page and can be
  > corrected on sight; an undetected third copy cannot.

  > ***v4 post-mortem — the v3 form blocked C6a or could not fail, and there
  > was no third alternative.*** v3 named the file set as
  > `web/sampler/index.html`, `web/todo/index.html`, `web/todo/compare.html`
  > and asserted the three extracted blocks are identical strings. **The
  > sampler has no inline block and C6a does not give it one.** Its
  > `<head>` (`web/sampler/index.html:3-9`) is two `<meta>`s, a `<title>`
  > and two `<link>`s — no script of any kind — and its only script is
  > `<script type="module" src="js/bundle.js">` at **`:36`, in `<body>`** —
  > src-ful and deferred. So v3's BX.10 had exactly two readings, both bad:
  > either the extraction yields an empty string for the sampler and the
  > criterion **fails at C6a no matter what the executor does** — it would
  > block the commit — or the empty string is silently skipped and the
  > criterion degenerates into "the two todo copies match", i.e. the sampler
  > clause could never fail, which is **P-III**. It also contradicted four
  > places that already had this right: **B3.8**'s excluded-sampler
  > blockquote, **B10.5**, ledger row 16 ("Not the sampler"), and §14 **M3**
  > ("the set is todo's two pages, not three"). BX.10 was the straggler from
  > the v2 pass that fixed the others. The fix is a **narrowing**, not a
  > widening: nothing is added to any touch list, and adding a bootstrap to
  > the sampler is what was rejected — doing so would delete B10.5's covered
  > behaviour, because `themes.css:15`'s `:root, [data-theme="dark"]` already
  > defaults an unstamped sampler to the full `dark` palette.
  >
  > ***And v3's extraction rule was independently broken on the two pages it
  > did get right.*** "Each `<script>` that appears **before the first
  > stylesheet `<link>`**" selects **nothing** on either todo page: the first
  > stylesheet link is `index.html:10` / `compare.html:10` and the first
  > `<script>` of any kind is `index.html:15` / `compare.html:16`, five to six
  > lines **after** it. *(**v5**, Architect N-2: v3 and v4 both wrote `:11`
  > here. `:11` is the first line matching the literal string
  > `<link rel="stylesheet"`, but `:10` — the Google Fonts link — is a
  > stylesheet link too; it simply spells the attribute last,
  > `…&display=swap" rel="stylesheet">`. The correction widens the gap the
  > paragraph is making a point about rather than narrowing it, and it is
  > load-bearing elsewhere: the same `:8-10` Google-Fonts group is the
  > landmark §5 **Step 6.2**'s `shared.css` `<link>` is positioned **before**,
  > so that Google's static Inter keeps winning the `--font-body` cascade
  > over shared `fonts.css`'s variable Inter at the three weights `todo.css`
  > requests — **v5 FINAL**, Critic minor 14: this read "Step 6.3", but the
  > link bullet is Step **6.2**'s, 6.3 being the theme system; and the
  > weight-700 residual the cascade cannot prevent is **D-19**.)* That rule came from the same false premise as
  > Step 6.3's "before the stylesheet parses" (corrected in §5) — the
  > bootstrap beats **paint**, not the stylesheet. The Critic's proposed
  > repair, "the `<script>` with no `src` attribute in `<head>`", is correct
  > in spirit but **not unique in this tree**: both pages *already* carry a
  > large inline src-less `<script>` in `<head>` — `index.html:21-164`
  > (144 lines: `loadSettings`, `toggleSidebar`/`closeSidebar` at `:78-85`,
  > the Escape handler at `:149-151`) and `compare.html:20-27` — and those
  > survive C6a untouched. "The **first** src-less `<script>`" would in fact
  > be unique and correct, since the bootstrap replaces `theme.js` at
  > `index.html:20` / `compare.html:18`, one line above each existing inline
  > block. Sentinels are chosen over that anyway for one reason: a positional
  > rule cannot detect a fourth copy **somewhere else**, and the sentinel can.
  > One mechanism discharges both halves.
  >
  > **Why this and not "just share it".** Sharing is impossible by
  > specification — `type="module"` defers, so nothing bundled can run before
  > paint — and a *nearly* identical copy is worse than a duplicate, because
  > the storage-key or `THEMES`-membership rule drifting on one page yields a
  > flash on that page only, which is the hardest class of theme bug to
  > reproduce (**R21**). This criterion converts "we remembered to keep them
  > in sync" into a command.
  >
  > **obsidianoid is not in the set either — and v5 restates *why*, because
  > v4's reason was false (Critic F-C).** v4 said obsidianoid needs no inline
  > block "because `app.ts`'s pre-paint stamp is already its own module-level
  > side effect on a classic end-of-body tag". There is **no module-level
  > stamp**. The only `document.documentElement.dataset.theme` write in the
  > module is `web/obsidianoid/js/app.ts:428`, inside `setTheme`, and every
  > path to it is either a click handler (`:439`), `switchVault` (`:494`) or
  > `fetchVaults` (`:479`) — and `fetchVaults` is `async` and sits behind
  > `await fetch('/api/vaults')` (`:467`). Nothing obsidianoid ships stamps a
  > theme before paint; it stamps one a network round-trip *after* paint.
  >
  > The real reason obsidianoid needs no bootstrap is that **it does not need
  > JS to be correct at first paint**: `web/obsidianoid/index.html:2` carries
  > a **static** `data-theme` attribute in the markup the server sends —
  > `data-theme="dark"` today, `"obsidian"` after Step 5.4's first bullet — so
  > the correct palette is in effect before a single script is fetched. Two
  > further facts make that sufficient rather than merely lucky: **B2.7**'s
  > 0-mismatch parity result means shared `obsidian` equals the retiring local
  > `dark` block hex for hex, so the statically stamped theme renders the same
  > pixels the module renders today; and Step 5.4 renames the attribute and the
  > server-side default (`internal/obsidianoid/build.go:41`) in the **same
  > commit**, so there is no boundary at which the markup says one name and the
  > config says the other. todo has no equivalent — its pages ship unstamped
  > and its theme is a per-user localStorage value — which is exactly why todo
  > needs an inline block and obsidianoid does not.
  >
  > **And the second half of v4's claim was wrong too, in the other
  > direction.** v4 said "C5 does not change the tag". C5 **must** change both
  > tags: `web/obsidianoid/index.html:131-132` gain `type="module"`, because
  > driver rule 10 externalizes `@shared` rather than inlining it
  > (`scripts/build-web.mjs:82-85`), so the emitted artifacts carry a real
  > top-level `import` that a classic `<script>` cannot parse. §5 Step 5.2
  > prescribes the edit, **B2.12** asserts it, and ADR-011's v5 amendment
  > carries the full forcing chain. This does not weaken obsidianoid's
  > exclusion from BX.10's set — it strengthens it: obsidianoid's scripts are
  > about to become *more* deferred, not less, which is precisely why its first
  > paint has to be correct from static markup and cannot be made correct by
  > anything it ships in JS.
  >
  > **Indent normalisation is an allowance, not a requirement here.** Both
  > slots sit at the same two-space depth today (`index.html:20`,
  > `compare.html:18` are both `  <script src="js/theme.js"></script>`), so
  > the copies should be byte-identical even before the `s/^[[:space:]]*//`
  > pass. The pass is kept so that a future re-indent of one page is not a
  > false failure; it is deliberately the *only* normalisation, because
  > anything more — collapsing internal whitespace, stripping comments —
  > would start hiding the drift the criterion exists to find.
- **BX.11 — `--require`'s list is complete, not merely present.**
  `[deferred → C1/C5/C6a]` *(New in v3 — Critic unscored question (b).)*
  At each of the three boundaries that edit the recipe, the comma list in
  `Makefile`'s `bundle-shape.mjs` line, sorted, must **equal** the sorted
  names of every descriptor carrying `sharedConsumer: true` in
  `scripts/descriptors.mjs` at that commit — not be a subset of it:

  ```sh
  flag=$(grep -o -- '--require=[A-Za-z0-9,_-]*' Makefile | head -1 \
         | cut -d= -f2 | tr ',' '\n' | sort | tr '\n' ' ')
  live=$(node -e 'import("./scripts/descriptors.mjs").then(m=>
         console.log(m.descriptors.filter(d=>d.sharedConsumer===true)
         .map(d=>d.name).sort().join("\n")))' | sort | tr '\n' ' ')
  [ "$flag" = "$live" ] || { echo "require list drifted: [$flag] != [$live]"; exit 1; }
  ```

  Expected: `sampler taskmaster` at C1, `+obsidianoid` at C5,
  `+todo` at C6a — the same three states §13(c) tabulates.

  > **Why this is added rather than declined.** The Critic asked whether
  > anything catches a `--require` list that is *too short* — the shape a
  > deferred C5 would leave behind, and the shape a forgotten recipe edit
  > leaves behind even when C5 lands. Nothing did. **BX.8** proves the flag
  > can fail, which is G16's requirement, but it proves it for the names that
  > are *in* the list; a name that was never added is a name whose bundle
  > shape is simply not required, and every gate in the repo stays green. That
  > is the same failure mode as v1's `--expect-nonempty` one level in: not an
  > absent mechanism, but a mechanism with nothing behind it. **B9.1** states
  > the per-boundary lists in prose, so this criterion is deliberately not a
  > second statement of them — it derives the expected set from
  > `descriptors.mjs`, which makes the descriptor file the single definition
  > (the inherited *one definition per fact* rule) and makes the criterion
  > correct without edit even if C5 is deferred out of the phase: with
  > obsidianoid's flip reverted, `live` loses the name and the flag must lose
  > it too. *Executed for the premise:* `Makefile:62` is
  > `node scripts/gates/bundle-shape.mjs` with **no flag at all** today, so
  > `flag` is empty, `live` is `sampler taskmaster`, and the check fails — the
  > correct result before C1.

  *Guaranteed by (step): both sides of the equality are written by the same
  three steps, which is the property that makes the criterion satisfiable —
  **§5 Step 1.4** (`Makefile:62` gains the flag with `sampler,taskmaster`;
  `descriptors.mjs` already carries both flags today — `sharedConsumer: true`
  at `:77` for `sampler` (`:70`) and at `:123` for `taskmaster` (`:116`)),
  **§5 Step 5.1** (`descriptors.mjs` gains
  `sharedConsumer: true` on obsidianoid **and** `Makefile:62` gains the name,
  stated in that step as one commit, G16), **§5 Step 6.1** (the same pairing
  for todo). **v5** — added by the back-pointer sweep. The pairing is the
  whole criterion: either edit alone leaves the sets unequal, which is
  exactly the drift BX.11 exists to catch, so a step that prescribed only one
  of the two would make a correct-looking commit fail.*

---

## §8 — Risks

| | Risk | Mitigation |
|---|---|---|
| **R1** | `bundle-shape.mjs` crashes with `EISDIR` the moment obsidianoid carries `sharedConsumer` | The gate extension and the descriptor flip are in the **same** commit (C5); B9.3 asserts it |
| **R2** | A stored `"dark"` silently becomes shared `dark` for existing obsidianoid users | ADR-014's one-time migration, discovering keys by **prefix scan over `Object.keys(localStorage)`** rather than from the vault roster — which is `[]` when the migration runs (§5 Step 5.4). **B2.6** proves it both ways *and across more than one vault*. ***v5 correction (Critic F-D item 6): this row's coverage claim was overstated through v4.*** "B2.6 proves it both ways" was true of the *direction* (migrated / not migrated) but not of the *population*: the v4 criterion seeded one key, `obsidianoid-theme-0`, so a vault-0-only implementation passed green and a user's vault 3 kept its regression. B2.6 now seeds two obsidianoid keys, a non-`dark` obsidianoid key and a foreign module's key, and asserts the guard flag plus a second-boot no-op |
| **R3** | Deleting `web/obsidianoid/css/themes.css` leaves a `var()` undefined and a rule renders transparent | The **38**-name census in §4.3 is the complete per-block vocabulary (17 colour + 21 structural); **34** names match shared exactly (13 colour + all 21 structural), **4** are renamed by table (`--color-surface`, `--color-surface-offset`, `--color-primary-highlight`, `--color-error`), and **0** are rewritten at their consumers. B1.1's ranged `git diff` plus the post-C5 pixel diff catch a miss. *v1 said 36 and 21 — it counted 34 lines as 34 declarations and then mis-split the remainder; §4.3 carries the corrected derivation (190 declarations / 5 blocks = 38, on 170 lines / 5 = 34).* ***v5:*** *the split moved from 33/4/1 to **34/4/0** — the user's Q12 ruling lands `--color-surface-dynamic` as T1's 18th key at C1 (§5 Step 1.5), so the one name that used to need rewriting now matches shared by name and by value. 34 + 4 + 0 = 38 still.* |
| **R4** | `--color-surface-dynamic`'s two consumers break on the 3 new themes | **Removed at the source in v5, not mitigated.** The risk existed only while the token was module-local: after the user's Q12 ruling, §5 **Step 1.5** declares `--color-surface-dynamic` in `web/shared/css/themes.css` for **all 8** themes at C1, so `app.css:202` and `:455` resolve everywhere and there is no theme on which they can fail to resolve. **B2.8** asserts the resolved values; **B1.1 part B** asserts the 8 declarations landed and that nothing else in `themes.css` moved. *(v1 pointed this at "Step 6.3" — that is todo; obsidianoid is Step 5. v2–v4 mitigated it by rewriting both sites onto other tokens, which is the plan that ruling 3 superseded.)* |
| **R5** | Mapping both surface names onto one token would flatten the skeleton shimmer to a solid bar | Explicitly avoided and recorded (§4.3). Under option B the two gradient stops are **different shared names** — `--color-surface-3` and `--color-surface-dynamic` — so the flattening would now require merging two shared tokens rather than mis-mapping one local one. **B2.8** still asserts the gradient resolves to **two distinct** computed colours in all 8 themes, and Step 1.5's value table shows all 8 pairs are distinct by construction (light's moves darker rather than lighter, which is why one assertion covers all eight), so the collapse cannot land green |
| **R6** | Under ESM, `window.ThreadsView` breaks | **Split in v3, because the two halves have opposite answers.** *Runtime:* it does not break — it is a real global property (`threads.ts:22`) read through the scope chain (`app.ts:3`), and `type="module"` preserves document order. Verified by reading both files; re-verified in the browser at C5. *Compile time:* it **does** break, and v2 wrongly asserted otherwise. `threads.ts`'s `interface Window` (`:17-20`) merges globally only while the file is a script (0 top-level `import`/`export` today); Step 5.2's import makes it module-local and `:22` fails with **TS2339**. Mitigated by Step 5.2's `declare global` sub-edit — legal only once the import lands (script form is rejected with **TS2669**) — and asserted by **B2.9** |
| **R7** | todo's 9 drawer controls lose their inline `onchange` wiring | Decision 2A: `render` slots mount into the live document, so `document.getElementById` still resolves. B4.3 asserts node identity |
| **R8** | Moving `toggleSidebar` out of todo's inline `<script>` disturbs the rest of it | The block is `web/todo/index.html:21-164` — **144 lines** including both `<script>` tags (v2 said "143-line", off by one at the open tag). Only `:78-85` is removed; the other **136** lines (settings, column config, three modals, `$(document).ready`) are untouched and named as such |
| **R9** | todo's bare token vocabulary clashes with `--color-*` once `shared.css` is linked | The two name-spaces are disjoint (`--bg` vs `--color-bg`), so nothing is overridden; `token-overlap.mjs` covers the taskmaster case and B1.2 re-runs it |
| **R10** | Mixed chrome in todo: shared components on `--color-*`, todo's own UI on `--accent` | B4.10 restricts todo's shipped picker to `dark`/`light`, the two themes whose values were donated *from* `todo.css`. **Not a judgement call:** the other six names match neither of `todo.css`'s two token blocks, so all **74** `var()` sites fall back to invalid initial values — an unstyled page, not mixed chrome |
| **R11** | A new shared CSS file escapes clause 7 | G13 forbids new files; ADR-012 re-keys the clause to fail closed anyway |
| **R12** | `EXPECTED_ARTIFACT_COUNT` is edited in more than one commit | G15; §6's artifact column is the record; `artifacts.mjs:83-86` fails on any mismatch |
| **R13** | The 40-file deletion takes a live file with it | The set was derived two independent ways and intersected; B8.2's guarded grep runs at the boundary; B8.3 asserts the 11 live pshelper files survive |
| **R14** | `GET /base.html` returning 404-via-SPA-fallback breaks a bookmark | `static.go:23-30`'s fallback serves `index.html`, so the URL degrades to the app rather than an error. Recorded, accepted |
| **R15** | C8 widens the auth surface more than Q8 authorised | GET-only; **three** path shapes — two exact (`shared.css`, `shared.mjs`) and one prefix+suffix (`public/fonts/…/*.woff2`); `HEAD` excluded with the reason recorded (§5 Step 8.2); five two-sided criteria over **seven** probes (B6.1–B6.5). *(**v5 FINAL**, Critic amendment 5: six through v5 DRAFT; the seventh is `GET /shared/public/fonts/inter/OFL.txt` → **401**, which is the only probe that can distinguish shape 2's prefix-**plus-suffix** match from a bare `HasPrefix("/shared/public/")`. Without it an over-wide handler passes every §7 criterion.)* *v5: the `.mjs` shape was excluded "pending Q9" through v4; the user closed Q9 on 2026-09-16 and it is now in. The widening is one exact file, and it is **authorised**, which is the distinction this row exists to police — B6.4 gains `POST /shared/dist/shared.mjs` → 401 so the new shape cannot silently become method-agnostic, and `/shared/ts/theme.ts` → 401 still proves `dist/` did not become a readable prefix* |
| **R16** | A side-car file is swept into a commit | P2's path-scoped `clean-tree.mjs`, plus BX.4 as a per-commit assertion |
| **R17** | `--shadow-md`'s change makes obsidianoid's panels look wrong | Declared as sanctioned delta D-1 with the pixel diff as the check. **The fallback is now closed off by ruling, not left open:** the user answered Q11 with *"accept for now"* on 2026-09-16, so D-1 stands and a module-local override is **not** the escape hatch — §9 Q11. What §11 item 4 now records instead is the opposite direction the same ruling set: decide the shadow ladder centrally in a later phase so every module gets the better value, rather than letting obsidianoid keep it privately. *(v1–v4 read: "reverting means a module-local override, which §11 item 4 records as the fallback".)* |
| **R18** | ~~The theme-swatch colour source forces a `THEMES` shape change late~~ | **RESOLVED in v2 — ADR-015.** Option 4D needs no `THEMES` change at all: the swatch carries `data-theme` and one CSS rule reads `var(--color-primary)`. `THEMES` stays `readonly string[]`, `main.ts:72` is untouched, and `ThemeName` is no longer exported (barrel trajectory 7/6 → 8/7 → **9/9**). Nothing is flagged for Architect |
| **R23** | `compare.html` keeps a live `onclick="toggleTheme()"` after `theme.js` is deleted | The convergent iteration-1 blocker. §4.8 inventories the page, §1 and Step 6 name it, it is in **C6a's Touches cell**, and **B4.12** asserts zero `onclick="toggleTheme()"` on *both* pages plus zero `toggleTheme` definition anywhere under `web/todo/` |
| **R24** | The plan cites a gate flag that `make gates` never passes | **G16**, and it is a *recorded* failure rather than a hypothetical: v1 did exactly this with `--expect-nonempty`. `Makefile` is now in the Touches cell of C1, C5 and C6a; **BX.8** proves the flag can fail by flipping a `sharedConsumer` and watching it fail by name |
| **R25** | Deleting orphaned markup leaves a non-null-asserted handle resolving to `null` | The `!` at `app.ts:31`/`:41`/`:42` keeps `tsc --noEmit` green, so no build gate catches it. Step 5.2 makes markup and handle **one edit**; **BX.9** asserts no adopter TS names an id its HTML lacks, for both adopters |
| **R26** | All eight swatches render the active theme's colour and the picker looks broken while every gate passes | The rejected option 4C's exact failure. **B4.2** asserts **8 DISTINCT computed `background-color`s** read simultaneously from one open picker, not the presence of 8 elements — the only form of the assertion that can fail on this defect |
| **R19** | `HamburgerMenu` and `modal.ts` both open, and the two focus traps fight | The menu closes on open of a modal it hosts; the trap predicate has one definition (B4.8) and the drawer's `role="dialog"` nesting is exercised in the sampler at C4 |
| **R20** | obsidianoid's errors becoming sticky floods the screen | The shared stack dismisses on click and the tone mapping is explicit; D-4 records the change and the sampler's "3 at once" demo is where stacking is judged |
| **R21** | todo's pre-paint inline bootstrap diverges from `ThemeManager`'s resolution, or the two pages' copies diverge from each other | The bootstrap reads the same key and the same `THEMES` membership rule. **BX.10** is the mitigation that matters: it asserts the two inline blocks are **byte-identical**, which is the only divergence a command can catch — `index.html` versus `compare.html` drift is the failure mode that shows on one page only and is hardest to reproduce. **B3.8** additionally checks that the stamp is present pre-paint at C6a. Bootstrap-versus-`ThemeManager` drift is not command-checkable (the bootstrap is inline text and the manager is bundled TS) and shows as a visible flash rather than a silent error. *(v2's row leaned on B3.8 alone; BX.10 is the stronger half and v2 omitted it here — v3, Architect minor.)* |
| **R22** | Phase 2's plan cites a line that has moved | §13's phantom-citation clause; every citation in §4 was read in this tree at plan time |

### §8.5 — Pre-mortem

Three scenarios, each with the earliest signal and the response.

**Scenario 1 — C5 lands and obsidianoid's `forest` theme is subtly wrong.**
The `forest`/`ocean`/`ember`/`rose` blocks in `web/shared/css/themes.css`
carry 17 colour keys each, and so do obsidianoid's — but **the two sets
overlap rather than nest**, and Phase 1's donor comments
(`themes.css:81,102,123,144`) cite the colour runs `:40-57` etc., not the
full blocks. If any of the four shared blocks silently dropped a key during
Phase 1's transcription, C5 makes it visible in a non-default theme that no
Phase-1 gate exercised. **Signal:** the post-C5 pixel diff for forest/ocean/
ember/rose shows a change that no D-row explains. **Response:** the fix is in
`web/shared/css/themes.css`, which G12 says Phase 2 does not touch — so this
becomes a **blocking finding**, parked with the diff, not patched inside C5.

> **v5 FINAL — the key arithmetic, corrected** *(Critic minor 8).* v5 wrote
> that obsidianoid's blocks carry "17 colour keys *plus*
> `--color-surface-dynamic`", i.e. 18, with shared's 17 a subset. Both halves
> are wrong, and the real shape is the reason Step 5.0 compares *mapped pairs*
> rather than key counts. Machine-derived over the two sheets as they stand:
>
> - obsidianoid's `forest` block: **34** declarations, of which **17** are
>   `--color-*`. `--color-surface-dynamic` is one of those 17, not an 18th.
> - shared's `forest` block: **17** declarations, all **17** `--color-*`.
> - The intersection is **12** names. Each side has **5** of its own:
>   obsidianoid `--color-error`, `--color-primary-highlight`,
>   `--color-surface`, `--color-surface-offset`, `--color-surface-dynamic`;
>   shared `--color-danger`, `--color-primary-tint`, `--color-surface-1`,
>   `--color-surface-3`, `--color-primary-fg`.
>
> T5a's rename maps four of obsidianoid's five onto four of shared's five
> (`error`→`danger`, `primary-highlight`→`primary-tint`,
> `surface`→`surface-1`, `surface-offset`→`surface-3`). The two that do **not**
> pair are `--color-surface-dynamic`, which has no shared counterpart until
> C1 lands it, and `--color-primary-fg`, which has no obsidianoid counterpart
> at all — so post-rename each block yields **16 mapped pairs** today,
> **16 × 5 = 80**, and **17 × 5 = 85** from C1 onward. Those are exactly
> **B2.7**'s two numbers and the reason its *Executed* note reads 80 while its
> published expectation reads 85. The "silently dropped a key" risk is
> unchanged: it just shows up as a pair whose values differ or a name with no
> partner, never as a block-size mismatch.

**v2 — the pre-check is now scheduled, and it has already been run once.**
v1 ended this scenario with a recommendation ("it belongs in Step 5 as its
first action") and then went ahead on an unverified claim, which is a
pre-mortem that describes a risk without discharging it. It is now **§5 Step
5.0**, the literal first action of C5, with **B2.7** as its criterion — and
it is a **full-property** comparison of **all five** blocks (not v1's "4×17"
key-set diff, which would have compared *names* and missed a wrong *value*).
Executed at plan time: **0 mismatches in all five blocks**, table in §4.3. A
non-zero result stops C5 outright rather than being absorbed, because a
rename-plus-delete and a re-authoring are different commits with different
acceptance criteria.

**What this scenario still covers, now that the pre-check exists:** drift
between plan time and execution time. The pre-check is re-run inside C5 for
exactly that reason.

**Scenario 2 — C6b's `render` slots work in the sampler and break in todo.**
The sampler's slot is a freshly created `<select>`; todo's slots are existing
elements moved out of `<aside id="sidebar">` into the drawer. If
`HamburgerMenu` renders its drawer lazily on first open, the nine controls do
not exist in the DOM at `$(document).ready` (`index.html:147-163`), and
`loadColConfig()` (`:72-76`) and `loadSettings()` (`:23-31`) silently find
nothing — the drawer looks perfect and the saved column state is lost.
**Signal:** column visibility and cooldown do not survive a reload, with no
console error. **Response:** `HamburgerMenu` must build its drawer DOM
eagerly at construction and only *animate* on open. **Make it structural:**
B4.3's node-identity assertion is written to run *before* any `open()` call,
so a lazy implementation fails the unit suite at C4 rather than the browser
at C6b. **The C6 split helps here too:** C6a lands `shell.ts` with
`initDrawer` a no-op, so `$(document).ready`'s interaction with the drawer is
a single-variable change at C6b rather than one of two simultaneous
migrations.

**Scenario 3 — C8's carve-out is wider than intended.**
`gate.go`'s existing carve-outs compare `r.URL.Path` by equality (`:129`,
`:135`, `:148`). A prefix test for the woff2 half (B6.2 needs one — the face
filenames are not a fixed list) is the first prefix carve-out in the
unauthenticated set, and `/shared/public/fonts/` as a prefix plus a
`.woff2` suffix test is easy to write as "starts with `/shared/public/`",
which would serve anything under `public/` unauthenticated.
**Signal:** B6.4's negative probes pass but a probe for
`/shared/public/<anything else>` also returns 200. **Response:** the suffix
test is mandatory and is asserted negatively: add a probe for
`/shared/public/fonts/inter/OFL.txt` → 401. **Pre-commitment:** that probe is
part of B6.4 from the start, so the wide form cannot land green.

---

## §9 — Open questions

Only genuinely user-owned questions appear here. Everything else is either
settled in FRD §7 (`:466-487`), settled in
`docs/OPEN-QUESTIONS-ui-unification.md`, or is a Planner/Architect decision
recorded as an ADR.

- **Q9 — CLOSED in v5 by user ruling (2026-09-16). Yes: the carve-out extends
  to `/shared/dist/shared.mjs`.**
  `docs/OPEN-QUESTIONS-ui-unification.md:200-235` — **moved out of that file's
  open list and into its `### Closed` section** in the same pass that produced
  v5, with the answer, the user's words and the date. (Closing there is a
  *move*, not a tick: that document's closed entries are plain bullets, not
  `- [x]` checkboxes, which is why Q9 no longer carries a box at all. Its
  header note at `:19-30` records the reconciliation.) The user's words and
  the reasoning they encode:

  > *"yes, the login page should use the theme previously selected."*

  A login page that restores the visitor's stored theme must run the shared
  theme code before the visitor has a session, so the bundle has to be
  reachable unauthenticated — the `.mjs` half of the allowlist is not a
  posture preference, it is a consequence of the feature. **What this changes
  in Phase 2:** C8's allowlist gains the `.mjs` path shape alongside the CSS
  and `.woff2` shapes, and **B6.3 flips from a negative to a positive** —
  unauthenticated `GET /shared/dist/shared.mjs` must now return **200** with
  the correct `Content-Type`, not 401. **What it does not change:** *styling
  the login page itself stays out of Phase 2.* The ruling authorises the
  transport, not the page; no login-page markup, CSS or theme-restore script
  is in any commit manifest. That work is recorded as a follow-up (§11 item
  11), which is also the one place that will need the pre-paint question
  answered for a page with no session. *Needed by: C8.*
- **Q10 — CLOSED in v2. Not a user question; the tree answers it.** v1 asked
  whether todo's picker should offer 8 themes or 2. It can only offer 2:
  `web/todo/css/todo.css` declares its 13 bare tokens in exactly two blocks
  (`[data-theme="dark"]` `:3-17`, `[data-theme="light"]` `:19-33`) and has
  **74** `var(--…)` consumers, so any of the other six shared names matches
  no block and takes all 74 sites down to their invalid initial values. v1
  framed the alternative as "ship all 8 and accept mixed chrome"; that
  alternative does not exist — the outcome is an unstyled page. Recorded as
  B4.10 and §5 Step 6.3; widening it is §11 item 1. **No decision required.**
- **Q11 (new, widened in v2) — is obsidianoid's `--shadow-md` change (D-1)
  acceptable, or should the module keep a local override?** The shared value
  is `0 8px 32px rgba(0,0,0,0.4)` (taskmaster's literal, a documented Phase-1
  exception at `tokens.css:3-4`); obsidianoid's `dark` block is
  `0 4px 16px oklch(0 0 0 / 0.4)` (`css/themes.css:25`). Both offset and blur
  **double**, so this is a geometry change, not an alpha nudge, and it lands
  at **3** consumption sites.

  **What v1 missed and this version states:** the four non-`dark` donor
  blocks carry `--shadow-md` at alpha **`/0.45`**, not `/0.4`, so the change
  is not uniform across the five themes — `forest`, `ocean`, `ember` and
  `rose` also lose 0.05 of alpha on top of the geometry change. And
  `--shadow-sm` varies the same way (`dark` `/0.3`, the other four `/0.35`)
  but has **0** consumers in `css/app.css`, so it is invisible either way and
  needs no decision. The question is therefore about `--shadow-md` at 3
  sites across 5 themes.

  **CLOSED in v5 by user ruling (2026-09-16): accept, and D-1 stands.**

  > *"accept for now; but the goal is that all the modules should adopt the
  > same capability for look and feel. that's the purpose of this exercise..
  > so if obsidianoid does this neat thing with shadow drop, probably want
  > that same effect in all the modules."*

  The answer to the question as asked is "accept" — no module-local override,
  D-1 stands at C5, and §8's R17 fallback stays a fallback. The **rider** is
  not a Phase-2 change and is recorded as program direction in §11 item 12:
  the convergence target is that a good effect in one module becomes
  available to all of them, which is an argument for *more* shared tokens over
  time and against module-local shadow overrides in particular. Nothing in
  Phase 2 acts on it; it is recorded in **§11 item 4**. *Needed by: C5 —
  answered.*
- **Q12 — CLOSED in v5 by user ruling (2026-09-16): option B, land the 18th
  shared token.** The question was: accept the one-step surface shift, or land
  an 18th shared token? `--color-surface-dynamic` was the one obsidianoid
  colour name with no shared counterpart, and **G12** forbade Phase 2 from
  declaring a new token. The two answers as put to the user:

  | | What it means | Cost |
  |---|---|---|
  | ~~**A — accept the shift**~~ (v2–v4's plan of record, **superseded**) | §4.3's three rule-local edits: `app.css:202` and `:437` drop to `--color-surface-2`, `:202`'s middle stop and `:455` land on `--color-surface-3`. Both effects keep one step of contrast | obsidianoid's skeleton and mode-switcher shift one surface level darker — a declared visual delta (D-3, ledger row 14), asserted by **B2.8** |
  | **B — land the 18th token** ✅ **chosen** | `--color-surface-dynamic` enters `T1` and `web/shared/css/themes.css` | **8** authored values needed where only **5** have donors; edits `themes.css` (**carving B1.1**) and `T1` (`check-shared-css.mjs:31-49`), moving `EXPECTED_COLOR_DECLARATIONS` 136 → 144; **amends G12** |

  > *"…In order for todo to enjoy the bredth of theming options, that means we
  > need color choices to extend each theme in a way that works for todo. I'm
  > in favor of doing exactly that: extend the themes so that any theme can be
  > used with any module."*

  **Where option B is engineered:** §5 **Step 1.5** (the whole of it — values,
  derivation, gate edit, seeded-failure demonstration, coordinate shift),
  **G12** (§3, amended to a closed one-item enumeration), **B1.1** (carved at
  C1 with a machine-checked diff shape and re-closed for C2…C8), **B2.8**
  (rewritten to assert eight distinct token pairs), **B10.6** (new, the
  sampler's second T1), §4.3 (the rename table, the 32/3/35 arithmetic and
  the 85-comparison parity result), §5 Step 5.3 **item 4** (now a no-op) and
  ledger row **14**. It lands at **C1**, not at C5 — four reasons, in Step 1.5.

  **Why it was the user's call and not the Planner's** (recorded, because the
  reasoning is what makes the override legitimate):
  `docs/OPEN-QUESTIONS-ui-unification.md:79-90` (Q2) said two incompatible
  things — "Closed: yes, but it lands in Phase 3" *and* "lands with
  obsidianoid's own migration", which C5 **is** — and it undercounted the
  token's consumers as "exactly one" when there are **two** (`app.css:202`,
  `:455`). So the inherited record did not settle it. Q2's Phase-3 deferral is
  now **overridden**, with the override and its reasoning written into
  `docs/OPEN-QUESTIONS-ui-unification.md:91-125` — a dated amendment appended
  beneath Q2's original answer, which is left standing rather than rewritten —
  instead of left as a silent contradiction between two documents. That
  amendment also records that **`--radius-xl` is not swept along** by the
  ruling: deferred on its own grounds, since this ruling widens the theme
  colour vocabulary and not the structural token set.

  **The part of the ruling Phase 2 does *not* act on**, stated here so the
  scope line is visible next to the mandate: "any theme can be used with any
  module" also requires retokenising `web/todo/css/todo.css` and widening
  todo's picker past two themes. That is **Phase 3+** (§11 items 1 and 2) and
  no part of it is in a Phase-2 commit manifest. Phase 2 lands the one token
  the tree actually needs today and the program direction that explains why.
- **Q13 (v5) — the artifact-count and DRY posture, ANSWERED IN ADVANCE by
  user ruling (2026-09-16).** Not a question the plan asked, so it is
  numbered and recorded rather than left in a review transcript:

  > *"as long as all modules can use all themes and we practice DRY as much as
  > possible (best effort), I approve all 19+ artifacts…"*

  `EXPECTED_ARTIFACT_COUNT` **15 → 16** at C6a stands (G15, `descriptors.mjs:162`
  and `:160`, asserted by BX.9), and **future growth is pre-approved** — a
  later phase adding build entries does not need to re-ask. The two conditions
  the user attached are the ones the plan already enforces: *all modules can
  use all themes* is what Q12's ruling starts and §11 items 1–2 finish, and
  *DRY as much as possible* is rule 1, whose only declared exceptions are
  §13(b)'s enumerated duplications and ledger row 16. Also carried in **§17**,
  since the count is one of the numbers the §13 audit re-derives.

*(Per the Planner's standing convention the live questions were appended to
`.omc/plans/open-questions.md` when this plan was adopted. **As of v5 none of
§9's questions is live:** Q9, Q11 and Q12 are closed by the 2026-09-16
rulings, Q10 was closed in v2 by the tree, and Q13 is an answer recorded
without a question. **This section is the record of account.** The revision
that closed them was scoped by its brief to this file and
`docs/OPEN-QUESTIONS-ui-unification.md` and to nothing else, so the Planner's
`.omc/plans/open-questions.md` side-car is reconciled against §9 on adoption
rather than edited here — keeping each closed entry with its answer rather
than deleting it. Recorded so the two files' disagreement, while it lasts, is
a stated lag and not a second definition.)*

---

## §10 — Deviation ledger

Places where this plan departs from the FRD, from the Phase-1 plan, or from
the brief's framing — each stated once, with the reason.

| | Deviation | Reason |
|---|---|---|
| 1 | **FR-9's JS count is wrong.** FRD `:390-391` says "17 JS files"; the verified pshelper-identical count is **15** (19 JS total under `web/todo/js/` minus todo's own 4: `compare.js`, `theme.js`, `todo.js`, `todo-utils.js`). CSS 19 ✅ and HTML 8 ✅ are correct | Counted in the tree. FRD §7 is settled but §FR-9's arithmetic is not a §7 decision; recorded, not reopened |
| 2 | **FR-9 names the wrong CSS file.** FRD `:393-394` lists `responsivenav.css` as part of the `base.html` `#rightToggle` hamburger. It is not: that hamburger's CSS is `web/todo/css/panels.css:57-101` (`#rightToggle` at `:57` through the last `.hamburger-icon.open` rule, closing at `:101` — v1 stopped at `:89` and cut three `nth-child` rules off the range). `responsivenav.css` belongs to a *fourth* dead menu (`js/menuserver.js:266-268` + `js/navcontrols.js:3-6`), and the sheet's only `<link>` is `web/todo/menuserver.html:16` — the page, not `base.html` *(that link was missing from v1's evidence; iteration 1 supplied it and it is verified here)* | Verified by grep; `responsivenav.css`, `menuserver.js`, `navcontrols.js` and `menuserver.html` are all in C7's 40-file deletion set, so the FRD's *intent* is satisfied and nothing is left dangling |
| 3 | **FR-9's "delete the legacy pshelper tree" is satisfied partially.** 40 of the 51 pshelper-identical files go; 11 stay because the live pages still load them | Deleting a referenced file is a regression, not a cleanup. §11 item 5 carries the rest |
| 4 | **FR-6 is untouched**, including todo's two Google-Fonts `<link>` groups and its three FontAwesome sheets, and `HamburgerMenu` carries a private SVG instead of using an `icons.ts` | FR-6 is not one of the five Phase-2 deliverables. G10 already scoped the `<link>`s as survivors |
| 5 | **Phase-1 §9 item 12** ("de-duplicate obsidianoid's theme list") is labelled *Phase 3* there; the Phase-2 brief assigns it to Phase 2 | The brief is the later instruction; landing it with the rename avoids editing the same file in two phases |
| 6 | **`todo.css`'s 13 bare token names are not retokenised**, so todo carries two colour vocabularies after C6, and its picker offers 2 of the 8 themes | §5 Step 6.3 + B4.10: **forced**, not chosen — the sheet declares its 13 names in only two blocks (`:3-17`, `:19-33`) and has 74 `var(--…)` consumers, so a third theme name unstyles the page. Q10 is closed in v2 for this reason; survivor plan is §11 item 1 |
| 7 | **`web/obsidianoid/css/app.css:383`'s `dialog::backdrop { oklch(0 0 0 / 0.6) }` is left alone** | Theme-independent by intent; FRD `:433-450` does not name it. §11 item 8 |
| 8 | **`web/shared/ts/modal.ts` is edited in C4** (extracting the focusable predicate), although Phase 2's deliverables do not include the modal | Rule 1: the alternative is a second definition of the trap predicate. The public surface is unchanged and `modal.test.ts` proves it |
| 9 | **`internal/obsidianoid/build.go` and four config/doc files are edited**, although the brief says "modules touched: sampler, obsidianoid, todo ONLY" | They *are* obsidianoid: its server-side theme default and the config values that feed it. The rename is incomplete without them |
| 10 | **`scripts/check-shared-css.mjs` clause 7 is re-keyed** although no new sheet is added | D2; the change is verified by the existing suite passing, and it makes G13 enforceable rather than aspirational |
| 11 | **C8 is in this phase at all**, although no Phase-2 deliverable needs it | Q8 (`:157-158`) says "Implementation is Phase 2". Sequenced last and independently revertible |
| 12 | **`docs/INVENTORY-hamburger-menus.md` and `docs/INVENTORY-menuserver.md` are cited but never committed or edited** | User side-cars, read-only. Every `[side-car]` claim in this plan was independently re-verified against the tree |
| 13 | **obsidianoid's typography changes wholesale at C5, and this is accepted as a delta rather than avoided.** Today `index.html` links only `css/themes.css:7` and `css/app.css:8` — no shared CSS, so none of `web/shared/css/fonts.css`'s 15 `@font-face` rules. C5 adds the `/shared/dist/shared.css` link (Step 5.3), which switches `--font-body`/`--font-mono` from an OS fallback stack to self-hosted Inter/JetBrains Mono: **every glyph on the page reflows** | Q7 closed the delivery question as "adopt via `<link>`", and the shared bundle is one sheet — `index.css`'s four `@import`s are not separable, so there is no way to take `themes.css` without `fonts.css`. Avoiding it would mean a module-local copy of the theme matrix, i.e. the duplication this phase exists to remove. Consequences: the font shift is a declared delta (**D-2** for the fallback chain, asserted by B2.6 as an *intended* change), and it forces **P5a** — obsidianoid's pixel baseline is re-captured *inside* C5 after the link lands, because no pre-C5 shot is comparable to a post-C5 render |
| 14 | **`--color-surface-dynamic` is resolved by landing an 18th shared token, not by a pair shift.** `web/shared/css/themes.css` gains 8 declarations at **C1** (§5 Step 1.5); the **`--color-surface-dynamic` name is unchanged at both of its sites**, `web/obsidianoid/css/app.css:202`'s middle gradient stop and `:455`'s background, so `:455` is **not edited at all**, while `:202` and `:437` take only the value-preserving `--color-surface-offset` → `--color-surface-3` rename they were always taking (`:202` carries **two** such references, `:437` one). *(**v5 FINAL**, Critic finding 7: v5 wrote "`:202` and `:455` are **not edited at all**", which is false at `:202` — §5 Step 5.3 item 3's rename is a byte edit to that line and item 3's own grep requires it. The zero delta here is a zero **value** delta. Same correction at §4.3, D-3, Step 5.6's item 3 and **B2.8**.)* | ***Reversed in v5 by user ruling (2026-09-16), and this is the one deviation the user chose rather than accepted.*** The row read: "resolved by a one-step pair shift, not by a new token. Three rule-local edits in `web/obsidianoid/css/app.css` (`:202`, `:437`, `:455`) — G12 forbids an 18th token this phase, and only 5 of the 8 shared themes have a donor value for it." Both premises were true and both were **amended by the ruling**: *"…extend the themes so that any theme can be used with any module."* G12 now carries a closed one-item carve for exactly this token (§3), and the 3 donor-less values are **authored** from each palette's own surface ladder rather than treated as a blocker — the same method `themes.css` already used for its 14 authored cells. Consequences: **D-3 is retired to zero delta** (§5 Step 5.5), the visual delta this row used to declare **does not happen**, **B2.8** now asserts the zero-delta shape and the 8 resolved values, and **B1.1 part B** machine-checks the diff shape (8 added declarations, 0 other additions, 0 deletions). The cost is one commit's edit to a frozen file; the gain is that `--color-surface-dynamic` works on all 8 themes instead of on 5, which is what the ruling asked for. §9 **Q12** carries the full disposition and §11 item 2 the program direction |
| 15 | **The theme swatch is coloured by a stamped `data-theme` attribute plus one `var(--color-primary)` rule, not by an inline colour or a per-theme rule set** | **ADR-015.** The three alternatives each break something: an inline literal re-introduces 8 colour literals in TS, a per-theme rule block fails `check-shared-css.mjs` clause 7's selector loop (`:314`) 8 times, and an ancestor-keyed rule renders all 8 swatches identically while every gate passes (**R26**). Declared consequence: the swatch border follows the previewed theme, not the page theme — **D-15** |
| 16 | **todo's two pages each gain a small inline pre-paint `<script>`, duplicated, rather than importing a shared function** | `type="module"` is deferred by specification, so no bundled code can run before first paint, and `theme.js` — the head-position **classic** script doing the job today (`index.html:20`, `compare.html:18`) — is deleted at C6a. The duplication is **2 pages × ~5 lines** and is a deliberate exception to rule 1, bounded by **BX.10**, which asserts the two blocks are byte-identical. Not the sampler (no persisted choice, and `themes.css:15`'s `:root` half already defaults it — B3.8, B10.5) and not obsidianoid, whose first paint is correct from **static markup**: `index.html:2` ships a `data-theme` attribute (`"dark"` today, `"obsidian"` after Step 5.4), B2.7 proves shared `obsidian` equals the retiring local `dark` block hex for hex, and Step 5.4 renames markup and server default in one commit. ***v5 correction (Critic F-C) — this cell was false in both halves.*** *It read "not obsidianoid (its `js/app.js` is a classic end-of-body tag at `index.html:132`, so its existing stamp is already pre-paint and C5 does not change the tag)". (a) **There is no module-level stamp in `app.ts`** — the only `dataset.theme` write is `:428` inside `setTheme`, reachable only from a click handler, `switchVault`, or `fetchVaults` behind `await fetch('/api/vaults')` (`:467`), so obsidianoid stamps a theme a network round-trip after paint, never before it. (b) **C5 does change the tags:** `index.html:131-132` both gain `type="module"`, because driver rule 10 externalizes `@shared` (`scripts/build-web.mjs:82-85`) and the emitted artifacts therefore carry a real top-level `import` — §5 Step 5.2, **B2.12**, ADR-011's v5 amendment. The correction cuts the same way the row does: obsidianoid's scripts become **more** deferred at C5, which is why its first paint must come from static markup and not from anything it ships in JS.* |
| 17 | **`Makefile:62` is edited three times** — at C1, C5 and C6a — rather than once | **G16**: a gate not invoked with the flag the plan cites is not the gate the plan describes. `--require=<name>,…` can only name boundaries that already exist, so the argument grows with the tree. Each edit is one line and is asserted at its own boundary by **B9.1** |
| 18 | **C6 is two commits, C6a and C6b**, so the sequence is 9 commits rather than 8 | **ADR-016.** C6a's claim ("todo renders pixel-identically") is true *by construction* only if the drawer is untouched in the same commit; bundling them makes the claim unverifiable and the revert all-or-nothing. Cost: one extra boundary where `initDrawer` is a no-op |
| 19 | **`web/todo/compare.html` is migrated in C6a even though FR-4 gives it nothing.** It gets the build entry, the pre-paint block and the `#theme-toggle`/`#theme-icon` handles, but no hamburger | It loads `js/theme.js` at **`:18`** and calls `toggleTheme()` from a live `onclick` at `:41`; deleting `theme.js` without touching the page leaves a hard `ReferenceError` on every click (**R23**). v1 assumed the page away. Its hamburger is §11 item 7. *(v2's row said `:20`, which is `compare.html`'s inline `<script>`; `:16` is `jquery.js`, `:17` `utils.js`, **`:18` `js/theme.js`**, `:19` `compare.js`. Corrected in v3 — Architect minor. §4.8's page table and ledger row 16 already read `:18`, so this row was the outlier.)* |
| 20 | **C8's unauthenticated allowlist admits `/shared/dist/shared.mjs` — executable code, not just a stylesheet and fonts.** *(New in v5.)* Three GET-only path shapes instead of two (§5 Step 8.1) | Q8(a) authorised the carve-out in principle and left the `.mjs` half to Q9, which v1–v4 recorded as open and deliberately excluded. The user closed it on 2026-09-16: *"yes, the login page should use the theme previously selected."* A login page that restores a stored theme must run the code that reads the store, and that code ships only in the barrel — so the shape is the ruling's **minimum**, not a convenience. What bounds the deviation: the match is **exact**, so `/shared/dist/` does not become a readable prefix (**B6.4** keeps `GET /shared/ts/theme.ts` → 401); the method guard is unchanged, so `POST` and `HEAD` on the new shape 401 (**B6.4** gains the `POST` probe); the handler is the same `static.SharedHandler` the dispatcher already uses, so path confinement and MIME behaviour gain no second definition; and the barrel is already world-readable on every *unprotected* module, so the carve-out narrows an asymmetry rather than opening new content. **B6.3 flips from negative to positive** and keeps its number. §9 **Q9**, **R15**, §11 item 11 |
| 21 | **C6b writes a `<link>` that v1–v4 asserted was already there, and touches `web/todo/compare.html` for a one-line cache-buster and nothing else.** *(New in v5.)* §5 Step 6.2 now adds `<link rel="stylesheet" href="/shared/dist/shared.css">` immediately before `web/todo/index.html:8`, and bumps `css/todo.css?v=19` → `?v=20` on **both** `index.html:13` and `compare.html:13` | Two separate declarations, landed together because they share a commit. (i) §4.8 read "only `index.html` links `shared.css`" from v1 onward, and **no step ever wrote the element** — yet B4.10 and B4.11 both presuppose todo rendering shared `.ui-menu-*` rules. §13(a5) could not catch it because there was no pointer to dangle; §13(a6) does not catch it either, since (a6) walks *deleted* symbols, not absent elements. The position is forced, not chosen: after `todo.css` it inverts precedence, and after `index.html:8-10`'s font group it silently re-faces every glyph on the page (both sheets supply `Inter`, `todo.css:42` requests it, `todo.css` has **0** `@font-face`, and the later `@font-face` wins) — a whole-page reflow with no D-row to account for it. Before `:8` satisfies both. *(**v5 FINAL**, Architect A-1: this row said `:8-12`. The Google group is `:8-10` — `preconnect`, `preconnect`, stylesheet — and `:11-12` are todo's **local** `css/fontawsme.css` and `css/fontawall.css`, which declare **no** `Inter` face and are therefore irrelevant to the hazard this row is about. §5 Step 6.2 already published `:8-10`, so the row was the only site disagreeing.)* (ii) `compare.html` gets no FR-4 behaviour at C6b (ledger row 19 covers its C6a migration); it is on the touch list solely because the `?v=` query it shares with `index.html` must move when the sheet's bytes move. The user authorised the placement on 2026-09-16: *"yeah, it can land in the same commit.. that's fine. I'll need to visually validate each one anyway."* Read as authority to **place** the bump in C6b, **not** to remove the `?v=` mechanism. **B4.13** asserts the link, its line ordering and both bumps. §4.8, §5 Step 6.2, §6's C6b row |
| 22 | **`system` is *not* a selectable theme name in Phase 2.** FRD `:254-255` specifies "4. `system` is a selectable pseudo-theme (smbedit's tri-state, promoted to everyone), live-updating on `matchMedia` change." Phase 2 delivers the **live-updating** half and **not** the **selectable** half: `system` is the resolver's implicit third step, never an entry a user can choose | *(New in **v5 FINAL**, Critic finding 1 secondary.)* The tree decides this one, in three independent places: `system` is absent from `THEMES` (`web/shared/ts/theme.ts:8-17`, 8 names), it is therefore absent from `themes.list` (**B3.5** asserts those two are the same set), and §5 Step 3.1's validation rule **rejects** it on read-back from storage — so even a hand-written `localStorage` entry cannot select it, and **B3.2** requires exactly that rejection. What Phase 2 *does* deliver is the behaviour the FRD item is actually about: with storage empty and `serverDefault()` silent, the OS preference decides the theme and a `matchMedia` `change` re-applies live (**B3.4**). What it does not deliver is a ninth option in the picker. The deviation is bounded and cheap to close — a name in `THEMES`, a `themes.list` entry, a validation carve and a picker row — and it is **§11 item 17**. Why it is not closed here: adding `system` to `THEMES` widens the set every module's picker enumerates (**B3.5**, **B4.10**, **B10.6**), and `THEMES` is also the set `check-shared-css.mjs` counts declarations against — a pseudo-theme with no `themes.css` block would have to be carved out of the gate that Step 1 exists to make real. **B3.4**'s title was corrected to match this row; §5 Step 3.1 is the single definition of the resolution semantics |
| 23 | **todo's first visit under `prefers-color-scheme: no-preference` renders `light` where it rendered `dark`.** The one *behavioural* delta in the todo commits | *(New in **v5 FINAL**.)* `web/todo/js/theme.js:15` asks `(prefers-color-scheme: light)` and falls back to `dark`; the shared resolver asks `(prefers-color-scheme: dark)` and falls back to `light`. Both are defensible; they are opposite. The shared polarity wins because it is shared — inverting it for todo would give the phase two resolution orders and put a module-specific branch inside the component the phase exists to unify. Bounded: invisible to anyone who has ever toggled the theme (storage is resolution step 1), and invisible on any profile that expresses a preference. **D-18** in §5 Step 6.2a carries it, §5 Step 6.6's C6a bullet pins the OS preference explicitly so the capture is not confounded by it, and §5 Step 6.2a's "none behavioural" claim is qualified rather than restated |
| 24 | **Weight-700 glyphs on `web/todo/index.html` re-face to the shared InterVariable file.** The only font delta the `<head>` ordering of §5 Step 6.2 cannot prevent | *(New in **v5 FINAL**, Architect A-3.)* `index.html:10` supplies `wght@400;500;600` and `todo.css` requests exactly those three weights (400 × 1, 500 × 3, 600 × 4, no italics), so at every weight the **stylesheet** asks for, Google's later-declared faces keep winning. At 700 Google has no face; shared `fonts.css:13`/`:21`'s `font-weight: 100 900` face matches exactly, and an exact match beats a closest-match regardless of declaration order. One live consumer: `web/todo/js/todo-utils.js:950`'s `<strong><b>` on periodic-and-due items, inside C6b's pixel window. **D-19** carries it; **B4.11** part 3 accepts its presence as attributed and its absence as equally correct, because whether it renders depends on the fixture's data rather than on the code — the only D-row in the plan with that shape |

---

## §11 — Follow-ups

Deliberately **not** in Phase 2.

1. **Retokenise `web/todo/css/todo.css`** — map the 13 bare names onto the
   shared 17, delete `:3-17` and `:19-33`, and tokenise the hard-coded
   `!important` overrides. **The count: the three ranges v2 named
   (`:358-361`, `:364-367`, `:411-427`) span *eight* `!important`
   declarations, not six** — `:359`, `:361`, `:365`, `:367`, `:416`, `:419`,
   `:424`, `:427` — and the **file holds twelve**, the other four being
   `:373` (`padding`), `:454`, `:455` (drag-indicator borders) and `:480`
   (`.ctrl-delete:hover i`). Only the eight carry colour literals and are
   therefore in scope for retokenising; the remaining four are specificity
   overrides against jQuery-UI and are a separate question. *(v2 said "six";
   corrected in v3 by `grep -n '!important' web/todo/css/todo.css` — Architect
   minor.)* Unblocks todo's full 8-theme picker (Q10). *Phase 3.*
2. **"Any theme with any module" — the program direction the user set when
   answering Q12, in the user's own words.** *(Rewritten in **v5**. The item
   used to be "`--color-surface-dynamic` and `--radius-xl` (Q2/Q12): restore
   obsidianoid's exact skeleton-shimmer and `#mode-switcher` values once the
   token exists." **`--color-surface-dynamic` is done** — it lands at C1, §5
   Step 1.5, so there is nothing left to restore and nothing for a later phase
   to pick up. What replaces it is the larger mandate the same ruling carried,
   which is deliberately **not** in Phase 2.)*

   > *"…In order for todo to enjoy the bredth of theming options, that means we
   > need color choices to extend each theme in a way that works for todo. I'm
   > in favor of doing exactly that: extend the themes so that any theme can be
   > used with any module."*

   Read as a specification, that is three pieces of work, none of them in a
   Phase-2 commit manifest:
   - **Retokenise `web/todo/css/todo.css`** — item 1 above, and the
     precondition for the other two. Today its 13 bare names are declared in
     exactly two blocks with 74 consumers, which is *why* todo's picker can
     only offer 2 themes (Q10, closed by the tree, not by preference).
   - **Widen todo's picker from 2 themes to 8** — B4.10 and §5 Step 6.3 pin it
     at 2 for Phase 2 because a third name unstyles the page. Once item 1
     lands, the pin comes out.
   - **Extend the shared themes with whatever further keys the other modules
     genuinely need**, by the same method Step 1.5 used: donate where a donor
     exists, author from the palette's own ladder where none does, and carve
     G12 for exactly the named addition. `--radius-xl` is the next known
     candidate and stays out of Phase 2 (§1).

   The **method** is the transferable part: Step 1.5 is the worked example of
   adding one shared key safely — 8 values, 5 donated and 3 authored, a
   two-line gate edit, a four-case seeded-failure demonstration, and a
   machine-checked diff shape. A later phase adding three keys should look
   exactly like Step 1.5 three times, not like a token refactor. *Phase 3+.*

   **One rename candidate this phase deliberately leaves alone:
   `--color-surface-dynamic` itself.** *(New in **v5 FINAL**, Architect N-5.)*
   The name is obsidianoid's, and it describes a *usage* — the moving stop of
   a shimmer gradient — where every other T1 key describes a *position* in a
   ladder (`--color-surface-1`, `-2`, `-3`). Having landed it verbatim at C1
   to avoid a value change, the shared vocabulary now carries one key whose
   name does not follow its own convention, and the obvious Phase-3 tidy is
   `--color-surface-4` or similar. The **price is stated here so the trade is
   visible before someone pays it accidentally**: the rename re-opens
   `web/obsidianoid/css/app.css` at `:202` and `:455` — the two sites §5 Step
   5.3 item 4 exists in order *not* to touch — and it revives the **D-3**
   shape that Q12 retired, because a name change at `:455` is a byte change to
   a line the plan currently certifies as untouched. That is a deliberate,
   well-understood, three-line commit in a later phase; it is not a tidy to
   fold into anything. *Phase 3+.*
3. **Delete the two one-time migrations** — obsidianoid's `"dark"` →
   `"obsidian"` and todo's `'todo-theme'` → `ui-theme:todo` — once enough
   time has passed. Both are guarded by a flag so the deletion is a pure
   removal. *Phase 4+.*
4. **Propagate a good effect once, not per module** (Q11's rider). The Q11
   question itself — does obsidianoid keep the shared `--shadow-md` or take a
   module-local override — is **answered**: it keeps the shared value, D-1
   stands, and §9 Q11 is closed. What is deferred is the direction the user
   attached to that answer:

   > *"accept for now; but the goal is that all the modules should adopt the
   > same capability for look and feel. that's the purpose of this exercise..
   > so if obsidianoid does this neat thing with shadow drop, probably want
   > that same effect in all the modules."*

   Concretely, for a later phase: decide the shadow ladder **centrally** and
   let every module inherit it, rather than letting whichever module has the
   nicest value keep it privately. Three machine-derived inputs, all measured
   in today's tree —
   - the donor varies **by theme**, which the shared vocabulary currently
     cannot express: `web/obsidianoid/css/themes.css` declares `--shadow-md`
     five times, at `:25` (`/0.4`, the `dark` block) and `:63`, `:101`,
     `:139`, `:177` (all `/0.45`). `--shadow-sm` splits the same way — `:24`
     is `/0.3`, the other four `/0.35`;
   - the shared value matches **neither** obsidianoid block on geometry, and
     deliberately so: `web/shared/css/tokens.css:16` is
     `0 8px 32px rgba(0, 0, 0, 0.4)` because Phase 1 took `--shadow-md` from
     taskmaster's donor modal rather than from the structural donor, so that
     modal stayed reproducible — `web/shared/css/tokens.css:3-4` says exactly
     this. Every obsidianoid block is `0 4px 16px oklch(0 0 0 / …)`. That
     whole delta — blur, offset, colour space and, for four themes, alpha — is
     what D-1 accepts;
   - **a stale comment in `tokens.css`, found while deriving the above, that
     Phase 2 must not fix.** `tokens.css:3-4` cites the donor as
     "`web/taskmaster/js/ui/modal.ts`'s literals (`:44`, `:28`)". Those
     coordinates resolved when the comment was written and do not resolve in
     HEAD: Phase-1 C6 collapsed that file to a two-line re-export shim, so it
     is 3 lines long today. The provenance is still true and still checkable —
     at the pre-C6 blob `3edfad7`, `:28` is
     `background: rgba(0, 0, 0, 0.55);` (→ `--overlay-scrim`) and `:44` is
     `box-shadow: 0 8px 32px rgba(0, 0, 0, 0.4);` (→ `--shadow-md`), both
     byte-matching the shared values. Only the *pointer* rotted. Correcting it
     is a one-line comment edit to `tokens.css`, and **G12 plus B1.1 part A
     forbid touching that file at every one of the nine Phase-2 boundaries**,
     comments included — B1.1 part A is `git diff --exit-code`, which does not
     distinguish a comment from a declaration. That is the guardrail working
     as designed, not a gap: the fix waits. *Phase 3, in the same commit that
     next legitimately opens `tokens.css`.*
   - `tokens.css` forbids the per-theme form outright: its header closes with
     "themes.css may override only `--font-body`, `--font-mono`,
     `--overlay-scrim`" (`tokens.css:7`), and clause 7 enforces that list. So
     a per-theme shadow is not a value tweak — it must widen that permitted
     set, which makes it a Step-1.5-shaped change (8 authored values, one G12
     carve, a gate edit).

   This is the same "any module, any theme" mandate as item 2, applied to a
   structural token instead of a colour. *Phase 3.*
5. **Delete `web/todo/js/jquery-ui.js`** (520,714 bytes — re-verified by
   `wc -c` in this tree, since iteration 1 flagged the figure as taken on
   faith; `index.html:16`) and
   the remaining live pshelper files as their consumers go. The negative
   probe — no `.sortable/.draggable/.datepicker/…` call anywhere in the four
   live JS files — is strong but jQuery permits dynamic dispatch, so this
   wants its own commit and its own browser pass. *Phase 3.*
6. **Migrate todo's three hand-rolled modals** (`index.html:307-334`,
   `:337-359`, `:362-382`) onto the shared dialogs, deleting the three
   `setTimeout(…focus(), 50)` workarounds and `todo.css:511-563`'s modal CSS.
   *Phase 3.*
7. **Give `web/todo/compare.html` a hamburger** — it has none today, so FR-4
   there is net-new. Its four pane selects (`:71-72`, `:88-89`) are the
   natural slot contents. *Phase 3.*
8. **`dialog::backdrop`** across the repo → `var(--overlay-scrim)`
   (obsidianoid `app.css:383` is one instance). *Phase 3.*
9. **Adopt the shared Toast in certmachine** — a two-line shim over
   `web/certmachine/js/toast.ts`, exactly as taskmaster's modal became one at
   Phase-1 C6 — and then in smbedit, utuber, admin, timetracker,
   issuetracker per FRD `:319`. *Phase 3+.*
10. **Extend clause 12** to cover `OFL.txt` presence per family, not just per
    tree. *Phase 3.*
11. **Styling the login page** — and only that. *(Rewritten in **v5**. The
    item was "Q9's `.mjs` half once the user rules on it, together with
    styling the login page." The user ruled on 2026-09-16: "yes, the login
    page should use the theme previously selected." So the `.mjs` half is
    **not** a follow-up any more — it lands in Phase 2 at C8, the allowlist
    gains the `/shared/dist/shared.mjs` path shape, and **B6.3 flips from a
    negative probe to a positive one**. See §9 Q9 and §5 Step 8.)*

    What stays deferred is the visual work the ruling makes *possible* but did
    not ask for. After C8 the login page can read the stored theme and the
    shared sheet and bundle will load unauthenticated; nothing yet restyles
    its markup onto `.ui-*` classes or the token vocabulary. Phase 2
    deliberately ships the **capability** and not the **appearance** — which
    keeps C8 a pure server-side allowlist commit with two-sided HTTP probes and
    no pixel surface, and keeps the login page out of P5/P5a entirely. *Phase
    3+.*

    **And the ruling's observable outcome lands in Phase 3, not in Phase 2 —
    stated plainly, because the ruling is quoted in five places in this
    document and none of them said so.** *(New in **v5 FINAL**, Critic's
    unconditional-approve row.)* The user asked for a login page that *uses*
    the previously selected theme. What C8 delivers is the **open door**:
    **B6.3**'s unauthenticated 200 on `/shared/dist/shared.mjs`, so the code
    that reads the store is fetchable before a session exists. What C8 does
    **not** deliver is any markup on the login page that constructs a
    `ThemeManager`, reads the key, or stamps `data-theme` — no step in §5
    edits a login template, and no criterion in §7 asserts a login-page
    render. So a reader who tests the ruling by loading the login page after
    C8 will see **no change**, correctly, and that must not read as a C8
    defect. This is the phase's cleanest capability/appearance split and it is
    deliberate: the allowlist commit is the one commit in the sequence with a
    security surface, and keeping it free of template edits is what makes it
    reviewable in isolation.
12. **`check-shared-barrel.mjs` is still a hand-maintained allowlist.** After
    three Phase-2 edits it will hold **18** names — 9 values and 9 types, per
    BX.2's 7/6 → 8/7 → 9/9 trajectory. *(v1 said 19; corrected.)* Consider
    generating the expected set from a declaration inside the barrel itself.
    *Phase 4+.*
13. **Phase-1 §9's remaining items** not claimed above stay where Phase 1 put
    them; this plan claims items 1, 2, 3, 6, 7, 12 and leaves the rest.
14. **Retrofit `web/shared/ts/modal.test.ts` onto `test-dom.ts`.** C3 adds
    `web/shared/ts/test-dom.ts` with `installFakeDom()` because the harness
    has no DOM at all (`scripts/test-web.mjs:51-59` bundles
    `platform: "node"`/`format: "cjs"` and pipes to a bare `node`) and
    `modal.test.ts`'s existing stub is a five-member object literal
    (`:47-52`: `body`, `activeElement`, `createElement`, `addEventListener`,
    `removeEventListener`) with no `documentElement`, no `localStorage` and
    no `matchMedia` — disjoint from everything `ThemeManager` touches. That
    makes the new helper **additive**, not a replacement: `modal.test.ts` is
    deliberately left alone in Phase 2 so that no Phase-1 test changes in a
    commit that is also adding new behaviour. Collapsing the two stubs into
    one is a pure test refactor with no product change. *Phase 3.*
15. **Pass `--build` to `artifacts.mjs` in the `web-verify` recipe.** *(New in
    **v5**, and it is the follow-up §13(b) duplication 5's blockquote names.)*
    `Makefile:57-58` invokes the gate bare, and that script's rebuild is
    optional (`scripts/gates/artifacts.mjs:105`, gated on `--build`), so what
    `make check` proves about every emitted artifact is **tracked and
    committed-clean, not provably rebuilt** — a stale `bundle.js` committed
    beside a changed `main.ts` is green. It is a one-line recipe change that
    would close the whole class, and it is deliberately *not* taken here:
    G16 confines Phase-2 `Makefile` edits to the enumerated `--require`
    lines at C1/C5/C6a, and adding a rebuild to `check` changes what every
    boundary's `make check` does, which is a change reviewers should see on
    its own rather than folded into a gate commit. **B10.6**'s **one** grep
    over `web/sampler/js/bundle.js` is the Phase-2 stand-in, and it covers
    exactly one artifact and one token. *(**v5 FINAL**, Critic minor 10: this
    read "two greps over `bundle.js`". B10.6 runs one —
    `grep -c -- '--color-surface-dynamic' web/sampler/js/bundle.js` = 1; its
    other greps target `web/shared/css/themes.css` and
    `web/sampler/js/main.ts`. The understatement matters in the direction that
    hurts: the freshness evidence is thinner than the item claimed, which
    strengthens rather than weakens the case for the `--build` flag.)*

    **And the same gap applies to C1's own bundle freshness, which this item
    is the recipe-level fix for.** *(New in **v5 FINAL**, Architect N-4.)* C1
    edits `web/sampler/js/main.ts` (§5 Step 1.1) and the committed
    `web/sampler/js/bundle.js` must be its rebuilt mirror. **Machine-checked:
    exactly one token** — B10.6's single `bundle.js` grep asserts
    `--color-surface-dynamic` reached the artifact, which is enough to prove
    *a* rebuild happened after *that* edit. **Not machine-checked: everything
    else Step 1.1 changes in `main.ts`.** Those rest on the step's manual
    checklist plus `artifacts.mjs`'s committed-clean check, which a stale
    artifact passes. One token is a sound freshness *witness* for a commit
    whose only `main.ts` change is that token — and C1's is not quite that, so
    the residual is real and is this item. *Phase 3.*
16. **Widen BX.5 beyond `web/`, or stop calling it the scope assertor.**
    *(New in **v5**, found by §13(e)'s own new derivation.)* BX.5's command is
    scoped `-- web/`, so §13(e)'s allowlist is asserted at commit time only
    for the four module prefixes; `scripts/`, `Makefile`, `tsconfig.json`, the
    two Go files and the three JSON configs are covered plan-time by §13(e)
    and §6's cells, and at commit time only by BX.4's narrower side-car ban.
    Widening the grep to the whole tree is the obvious fix and has a real
    cost: every `docs/` edit in the phase — and this plan is one of them —
    becomes a diff the criterion has to allowlist, which is how an
    over-broad scope gate turns into a noisy one that gets weakened. The
    trade belongs to a reviewer. *Phase 3.*
17. **Make `system` selectable, closing the second half of FRD `:254-255`.**
    *(New in **v5 FINAL**, Critic finding 1 secondary; deviation-ledger row
    22.)* Phase 2 ships `system` as the resolver's implicit third step with a
    live `matchMedia` `change` listener (**B3.4**) and ships **no way to
    choose it**: the name is absent from `THEMES`
    (`web/shared/ts/theme.ts:8-17`) and from `themes.list`, and §5 Step 3.1's
    validation rejects it on read-back. Closing the gap is four small edits —
    a name in `THEMES`, a `themes.list` entry, a carve in the read-back
    validation so the stored value `system` means "keep resolving", and a
    picker row whose swatch has no single colour to show. What makes it a
    follow-up rather than a line item is the **third** edit and the swatch:
    `THEMES` is also the set `check-shared-css.mjs` counts per-theme
    declarations against (§5 Step 1.1's clause 12, `EXPECTED_COLOR_DECLARATIONS`),
    so a pseudo-theme with no `themes.css` block has to be carved out of the
    gate this phase exists to make real — and carving the gate to admit a
    non-theme is the opposite of the phase's direction. Doing it in a phase of
    its own means the carve arrives with its own criterion instead of riding
    in on a theme commit. *Phase 3.*

---

## §12 — ADRs

Continuing Phase-1's ADR-001…ADR-007.

**ADR-008 — `ThemeManager` is added beside `setTheme`, not instead of it.**
*Decision:* `web/shared/ts/theme.ts` keeps `THEMES` and `setTheme` and gains
`class ThemeManager`, which calls `setTheme` internally.
*Drivers:* D1 is irrelevant here; the drivers are that `web/sampler/js/main.ts:9`
destructures `setTheme` today, that the Phase-1 barrel allowlist names it, and
that rule 1's operational test forbids deleting an occurrence before naming
the survivor with an adopter in hand.
*Alternatives:* replace `setTheme` (Decision 1B); free functions (1C).
*Why chosen:* the class is the only new public name; "how a theme is applied"
keeps exactly one definition; no existing consumer changes in the commit that
introduces an unproven class.
*Consequences:* two entry points exist, and a module can bypass persistence by
calling `setTheme`. Accepted: `setTheme` is the primitive, documented as such.
*Follow-ups:* §11 item 12.

> **v2 amendment — the class gains one method v1 did not have: `reresolve()`.**
> *Driver:* obsidianoid's storage key is **per vault**
> (`app.ts:425` `themeStorageKey()`), and `switchVault` (`:484-498`) changes
> which key is current *without* changing the theme. A `ThemeManager` that
> resolves storage only in its constructor therefore keeps showing the
> previous vault's theme — the exact behaviour FRD `:250-253` requires be
> preserved, and the one obsidianoid feature that has no analogue in the
> sampler or todo. *Alternatives:* construct a new `ThemeManager` per vault
> switch (leaks the old instance's listeners, and `destroy()` on the theme
> manager is not in FR-3's surface); have the caller read storage and call
> `setTheme` (moves resolution logic back out of the class, i.e. a second
> definition of "how a stored theme becomes an applied theme"). *Why chosen:*
> one idempotent method keeps resolution in one place and makes the vault
> case a one-line call at `app.ts:493-494`. *Consequence and its guard:*
> `reresolve()` must **not write** storage, or switching vaults would
> overwrite the destination vault's saved choice with the source vault's;
> **B3.10** asserts both halves (it follows the new key, and the stored value
> is unchanged afterwards). The method lands at C3 and is first exercised at
> C5.

**ADR-009 — `HamburgerMenu` is a plain class rendering into light DOM.**
*Decision:* an exported class, `.ui-menu-*` classes, no custom element, no
shadow root.
*Drivers:* todo's nine controls are reached by `document.getElementById` from
inline `onchange` attributes and from `js/todo.js`/`js/todo-utils.js`; clause
7 lints files, not shadow trees; `modal.ts` already establishes the idiom.
*Alternatives:* custom element + Shadow DOM (2B); custom element, no shadow
(2C).
*Why chosen:* Shadow DOM would break the exact thing FR-4's `render` slot
exists to guarantee.
*Consequences:* global class names; nothing prevents a module reaching into
the drawer. Mitigated by clause 7's `^\.ui-` rule and by B4.7's `destroy()`.
*Follow-ups:* none.

**ADR-010 — Toast lifts certmachine's surface verbatim.**
*Decision:* `showToast(message, tone?, durationMs?) → ToastHandle`, a
module-level lazily-created singleton stack, `error` sticky at 0 ms.
*Drivers:* FRD `:319` names the donor; obsidianoid's 16 call sites expect a
free function across two files that share no module scope; one `aria-live`
region is an a11y requirement, not a style choice.
*Alternatives:* a `ToastHost` class (3B); keep obsidianoid's 2-arg/2800 ms
shape (3C).
*Why chosen:* zero API invention, and the donor's three load-bearing
properties survive by construction.
*Consequences:* process-global state in a library; obsidianoid's errors become
sticky (D-4).
*Follow-ups:* §11 item 9.

**ADR-011 — obsidianoid becomes a bundling, multi-output `sharedConsumer`
descriptor, and the artifact-path derivation gets its own module.**
*Decision:* `mode: "transpile"`, `bundle: true`, `format: "esm"`,
`sharedConsumer: true`, two entries, one **`out`**. *(**v5 FINAL**, Critic
minor 14: the field is spelled `out` — `scripts/descriptors.mjs:83`,
`out: "web/obsidianoid/js/"`, documented at `:18` as "the outdir for
`transpile`, the outfile for `bundle`". "outdir" is esbuild's option name, not
the descriptor's, and this ADR is about the descriptor.)* New
`scripts/artifact-paths.mjs` is imported by both `list-artifacts.mjs` and
`gates/bundle-shape.mjs`.
*Drivers:* D1 — `@shared` is only resolvable in a bundling pass
(`build-web.mjs:78-88`); `bundle-shape.mjs:73` reads `d.out` as a file and
would throw `EISDIR`; rule 1 forbids a second copy of the derivation.
*Alternatives:* two single-output `bundle` descriptors named
`obsidianoid-app`/`obsidianoid-threads` (breaks "name = module identity" and
doubles the descriptor count for no gain); one merged entry `main.ts`
(requires converting `window.ThreadsView` and `showToast` into real imports
across two files and changes the artifact set from 2 files to 1, moving
`EXPECTED_ARTIFACT_COUNT` twice in the phase); hand-writing the URL
`/shared/dist/shared.mjs` in module source (needs a `tsconfig` path mapping a
URL to a file and makes B9.1's marker unfalsifiable).
*Why chosen:* it is the only shape that keeps one descriptor per module, keeps
the artifact count still, and makes the gate correct for every future
multi-output consumer.
*Consequences:* both `.js` files become ES modules and `index.html:131-132`'s
two `<script>` tags gain `type="module"`; the gate script changes in the same
commit as the descriptor.
*Follow-ups:* none.

> ***v5 — `type="module"` is mandatory, and two other sites said the
> opposite (Critic F-C).*** Ledger row 16 and BX.10 both asserted that "C5
> does not change the tag". ADR-011 was right and they were wrong, and the
> reason is a single line of the driver: **rule 10 externalizes `@shared`
> rather than inlining it.** `scripts/build-web.mjs:82-85` resolves every
> `@shared…` specifier to `{ path: "/shared/dist/shared.mjs", external: true }`
> under the section header "rule 10: `@shared` is externalized, never
> inlined". So `bundle: true` does **not** absorb the barrel: the emitted
> artifact keeps a literal top-level
> `import { … } from "/shared/dist/shared.mjs"`, exactly as
> `web/taskmaster/js/bundle.js:2` does today. A classic `<script src>` cannot
> parse that — it is `SyntaxError: Cannot use import statement outside a
> module`, a blank page, not a degradation. The forcing chain has no slack in
> it:
>
> `sharedConsumer: true` → `format: "esm"` is **enforced**, not conventional
> (`build-web.mjs:183-186` throws `sharedConsumer:true requires format "esm"`)
> → rule 10 emits a real `import` → the tag must be `type="module"`.
>
> The tree already shows the convention holding perfectly, which is how the
> contradiction could have been caught earlier: the two `format: "esm"`
> consumers carry module tags — `web/sampler/index.html:36` and
> `web/taskmaster/index.html:13` — and the two `format: "iife"` bundles carry
> classic ones — `web/multissh/index.html:11`,
> `web/certmachine/index.html:11`. obsidianoid moves from
> `bundle: false`/`mode: "transpile"` into the first column at C5, so its tags
> move with it.
>
> **This also exposed a missing prescription, not just a contradiction.**
> ADR-011 was the *only* place in v4 that mentioned the edit, and an ADR
> records a decision — it is not an instruction an executor works from. No
> step listed the tags and no criterion asserted them, so the single most
> load-bearing edit in C5 (without it obsidianoid does not boot) existed in
> the plan only as a consequence clause. §5 **Step 5.2** now prescribes it and
> **B2.12** asserts it.

> **v2 amendment — rebuttal: `out` stays a directory, and the fix cannot move
> to the descriptor.** The Critic proposed avoiding the new module by giving
> obsidianoid a file-valued `out` like every other descriptor, so
> `bundle-shape.mjs:73`'s `readFileSync(d.out)` would just work. That is
> **mechanically impossible**, and the tree says so in one function:
> `scripts/list-artifacts.mjs:38-45` dispatches on mode, and the
> `transpile` arm is `path.posix.join(d.out, jsName(entry))` (`:39`) — it
> *joins* `out` with a per-entry basename. A file-valued `out` would make the
> two derived artifact paths `web/obsidianoid/js/app.js/app.js` and
> `…/app.js/threads.js`. Obsidianoid has **two** entries and therefore two
> outputs; one file-valued `out` cannot name two files. So the gate is the
> side that must learn to iterate, which is what `artifact-paths.mjs` is for.
> *Also corrected:* the extraction range is `:25-27` **+** `:38-45`, not the
> dispatch alone — `jsName` is defined outside it (`:26`), so lifting `:38-45`
> in isolation yields a module with an undefined reference (Step 5.1).
>
> *One clarification iteration 1 asked for:* `mode: "transpile"` with
> `bundle: true` looks contradictory but is not. `mode` is **this repo's**
> descriptor vocabulary, not esbuild's — it selects the artifact *shape*
> (one output per entry, written into a directory) while `bundle` is
> esbuild's own flag and is what makes `@shared` resolvable via the
> `onResolve` plugin (`build-web.mjs:78-88`). esbuild bundles each entry
> point independently and writes one output per entry, so the combination is
> ordinary esbuild usage. It keeps obsidianoid's two artifact paths exactly
> where they are today, which is why `EXPECTED_ARTIFACT_COUNT`
> (`descriptors.mjs:162`, `= 15`) does not move at C5.

**ADR-012 — all Phase-2 CSS lands in `components.css`, and clause 7 is
re-keyed to fail closed.**
*Decision:* no new file under `web/shared/css/`; clause 7's selector becomes
"every sheet that is not `tokens`/`themes`/`fonts`/`index`".
*Drivers:* D2 — `check-shared-css.mjs:298` keys clause 7 to the literal
filename, so a new sheet escapes the colour-literal ban and the `^\.ui-`
rule.
*Alternatives:* split into `toast.css`/`menu.css` and extend clause 7's key
to a set (strictly more work, identical guarantee, plus `index.css` and
clause 8 churn); split without extending (loses coverage over exactly the
code this phase writes).
*Why chosen:* the gate coverage is the point, and one file preserves it with
zero new plumbing.
*Consequences:* `components.css` grows from 101 lines to roughly 300; the
re-key is a behaviour-preserving change at its own boundary, verified by the
existing suite passing.
*Follow-ups:* §11 item 10.

> **v2 amendment — at-rule descent in `parseBlocks` is opt-in, and the v1
> rationale for that was wrong.** Phase 2's components live partly inside
> `@media` blocks (the drawer's breakpoint behaviour), and
> `check-shared-css.mjs:182`'s filter makes `parseBlocks` skip at-rules
> entirely — so clause 7's **selector loop** (`:310-318`) never sees a rule
> nested in a media query, and a `body { display: block }` hidden in one would
> pass. The fix adds an opt-in descent parameter, used by clause 7 only.
>
> **v3 correction — the hole is selector-only, and v2's illustration named the
> wrong escape (Architect N6).** v2 wrote the example as
> `body { background: #fff }`, which is *not* an escape: clause 7's other,
> independent rule is a per-line scan of the **whole file**
> (`:304-308`, `text.split("\n")`), so a colour literal inside an at-rule is
> caught today. Proved by copying `web/shared/css/` and
> `scripts/check-shared-css.mjs` into a scratch tree and running the gate
> three times — baseline `11 clauses pass`, rc 0; with
> `@media (…) { body { background: #fff } }` appended to `components.css`,
> **rc 1**, `clause 7: colour literal "#fff"`; with
> `@media (…) { body { display: block } }` appended instead, **rc 0 — the
> escape**. The same `body { display: block }` at top level is caught
> (`clause 7: selector "body" does not start with .ui-*`, rc 1). The probe
> below is rewritten accordingly, and the declaration inside it must be
> **colourless** or the colour scan catches it first and the result says
> nothing about the descent.
>
> *Corrected rationale.* v1 argued descent must be opt-in because making it
> global would break clause 6 via `fonts.css`'s 15 `@font-face` rules.
> **That claim is false and is withdrawn** (see the note at Step 2.3):
> `parseBlocks` has exactly three callers — `:236`, `:252`, `:310` — none of
> which is ever passed `fonts.css`, and clause 10's font count is a raw-text
> `text.match(/@font-face\b/g)` at `:364`, not a block parse. The true reason
> is **clauses 3, 4 and 5**: each is a *set-equality* assertion over a file
> modelled as flat — clause 3 fails unless `tokens.css` yields exactly one
> block (`blocks.length !== 1`, `:237`), and clauses 4/5 compare
> `themes.css`'s block roster and its total declaration count
> (`EXPECTED_COLOR_DECLARATIONS = 136`, `:98`) against fixed expectations. Any
> future at-rule in either file would, under global descent, add blocks and
> declarations to those totals and fail three clauses for a legal edit.
> Opt-in keeps each clause's model of its own file intact.
>
> *Honest caveat:* on **today's** tree global descent would also pass —
> `grep -c '@' ` is **0** for both `tokens.css` and `themes.css`. So this is
> a future-proofing argument, not a bug-avoidance one, and it is recorded as
> such rather than overstated. *Negative test:* the descent is proved by
> making it fail — append
> `@media (max-width: 640px) { body { display: block; } }` to
> `components.css`, confirm clause 7 now fails **naming the inner selector**
> (`selector "body" does not start with .ui-*`) with a non-zero exit, and
> revert (nothing committed). It must be a **selector** escape carrying a
> **colourless** declaration: a colour literal in the same position is caught
> by the pre-existing whole-file scan whether the descent works or not, so it
> probes nothing (v3, N6 — measured both ways above). Under the pre-C2 gate
> the identical probe exits 0, which is the "before" half of the proof.
> Without it the new parameter is untested code in a gate, which P-III
> forbids. This is **B1.4** item 1, which already had the probe in its correct
> selector form — the defect was that this ADR and §14's M-6 row described a
> different, non-probing test for the same criterion, i.e. a one-definition
> violation with two disagreeing definitions.

**ADR-013 — todo enters the build pipeline with exactly one TS entry.**
*Decision:* new `web/todo/js/shell.ts`, a `bundle` `sharedConsumer`
descriptor emitting `web/todo/js/shell.js`;
`EXPECTED_ARTIFACT_COUNT` 15 → 16; `tsconfig.json` 11 globs → 12 in the same
commit. The remaining legacy `.js` files, jQuery included, are untouched —
**18** of them: `web/todo/js/` holds **19** tracked `.js` files today, C6a
deletes exactly one (`theme.js`) and adds one artifact (`shell.js`). *(v1 said
20; corrected, and the same correction lands in B7.4.)*
*Drivers:* D1; todo has no TypeScript and no descriptor today; the inline
`<script>` at `index.html:21-164` holds real application logic that must keep
working; Q4's coupling principle says the `tsconfig` glob lands with the file
it covers.
*Alternatives:* a hand-written ESM file importing the literal
`/shared/dist/shared.mjs` with no descriptor (no typecheck, no
`bundle-shape.mjs` coverage — a shared consumer invisible to both gates);
migrating `js/todo.js` + `js/todo-utils.js` to TS in this phase (1,490 lines
of jQuery-flavoured JS; a re-write, not an adoption).
*Why chosen:* one new file, one new artifact, full typecheck and gate
coverage, and zero risk to 1,490 lines of working code.
*Consequences:* todo runs a module script alongside six classic scripts;
`shell.js` must execute after them, which `type="module"`'s deferral
guarantees.
*Follow-ups:* §11 items 1, 5, 6.

**ADR-014 — the `dark` → `obsidian` rename ships with a one-time localStorage
migration.**
*Decision:* before the `ThemeManager` resolves, a stored
`obsidianoid-theme-<i>` value of exactly `"dark"` is rewritten to
`"obsidian"`, once, under a guard flag.
*Drivers:* D3 — `app.ts:425-430` persists the chosen *name*, and after the
rename `"dark"` is a valid-but-different shared theme (`#0d1117` vs
`#13131a`). Q4's coupling principle puts rename and adoption in one commit
(§0's authority split; Q4 itself adjudicates `tsconfig`, not theme names);
without the
migration that coupling would ship a silent visual regression in the very
module whose identity it preserves.
*Alternatives:* no migration (users who chose dark silently get a different
palette); a server-side rewrite of persisted vault themes (the client value
is per-browser, so the server cannot see it); mapping shared `dark` to
obsidian's palette (would corrupt the shared matrix for every other module).
*Why chosen:* it is the only fix at the layer where the stale value lives.
*Consequences:* obsidianoid carries migration code and a guard key until §11
item 3 removes them.
*Follow-ups:* §11 item 3. Note that todo needs the same treatment for a
different reason — a *key* rename, not a value rename — and both are written
in the same idiom (§5 Step 6.3).

**ADR-015 — the theme swatch is coloured by a stamped `data-theme` attribute
resolving one `var(--color-primary)` rule.** *(New in v2. This is the decision
both reviewers identified as the plan's central unresolved conflict.)*
*Decision:* `components.css` gains exactly one rule —
`.ui-theme-swatch { background: var(--color-primary); }` — and the picker's
TypeScript stamps `el.dataset.theme = name` on each swatch element. No
per-theme CSS, no inline colour, no colour literal anywhere in TypeScript.

*Prior art in this very module, so the idiom is adopted rather than invented:*
`web/obsidianoid/js/app.ts:437` already writes `btn.dataset.theme = t.name;`
when building its theme panel, and `:429` already **reads** it back —
`document.querySelectorAll('.theme-btn').forEach(b => b.classList.toggle('active', (b as HTMLElement).dataset.theme === name))`
inside `setTheme` (`:427-431`). 4D changes what the stamp *does* (it now also
resolves a theme block) without changing who writes it or how. This matters
for review: the donor already round-trips `dataset.theme` on exactly these
elements, so 4D adds no new lifecycle, no new attribute and no new naming
convention — only a CSS consequence the donor did not exploit.

*The one load-bearing dependency, stated explicitly because 4D is silently
wrong without it:* **no sheet in the shared bundle may key a rule on a
`[data-theme]` *ancestor*.** 4D works because `themes.css`'s eight blocks are
**bare** attribute selectors, so a stamped swatch matches one of them
*itself* and re-resolves the tokens on its own element. A rule written as
`[data-theme="x"] .something` would instead match by *inheritance from the
page*, which is option 4C's failure and **R26**. Verified in this tree:
`grep -rn 'data-theme' web/shared/css/` returns hits **only** in
`themes.css` — the eight block selectors at `:15, :38, :61, :82, :103, :124,
:145, :167` plus two comments — and **nothing** in `components.css`,
`tokens.css`, `index.css` or `fonts.css`. The dependency is also *structurally
enforced* rather than merely observed: clause 7 requires every top-level
selector in `components.css` to match `/^\.ui-[a-z0-9-]+/` (`:314`), so a
`[data-theme="…"] .ui-theme-swatch` rule added to the one file Phase-2 CSS
lands in fails the gate by name. That is the same check that rejects 4C, which
is why the two facts are one fact. Consequence (3) below — "no other rule in
`components.css` may key off `.ui-theme-swatch`'s ancestors" — is the
forward-looking half of this same dependency.
*Drivers:* (1) **clause 7**, which has two independent rules — a
colour-literal scan (`check-shared-css.mjs:304-308`) *and* a selector loop
requiring every top-level selector in the file to match
`UI_SELECTOR = /^\.ui-[a-z0-9-]+/` (`:102`, enforced at `:314`); (2) the
functional requirement that one open picker shows **8 different** colours
simultaneously (FRD `:257-261`); (3) **G12** — no token may be declared this
phase; (4) barrel stability — `THEMES` must keep its
`readonly string[]`-compatible shape so `web/sampler/js/main.ts:9`'s
destructuring and `check-shared-barrel.mjs`'s 6-value allowlist are unaffected.
*Alternatives, all four rejected with the reason each fails:*

| | Option | Fails on |
|---|---|---|
| 4A | `getComputedStyle` probe: stamp a hidden element per theme and read the colour back | Viable and gate-legal, but unreliable before first paint — exactly when a picker may build — and costs a layout read per swatch. Retained as the documented fallback if 4D ever fails |
| 4B | v1's reshape of `THEMES` into objects carrying a `color` literal | Re-introduces 8 colour literals in TypeScript (a second definition of every palette's primary, which the shared matrix exists to be), changes the barrel's exported *shape*, and breaks `main.ts:72` **silently** — `check-shared-barrel.mjs` never inspects `THEMES`' type (`:25`, `:103-104`), so every gate stays green |
| 4C | a per-theme rule set keyed on an ancestor, e.g. `[data-theme="ocean"] .ui-theme-swatch` | **8 clause-7 failures** at `:314`: those selectors do not start with `.ui-`. And even if the gate allowed it, every swatch in an open picker would render the *page's* colour, so all 8 look identical while a markup-level test passes — **R26** |
| 4E | keep the donor's inline `style="background:${t.color}"` (`app.ts:438`) | Keeps the colour literals in TS (4B's defect) and leaves the shared picker unable to render swatches at all without per-module data |

*Why chosen:* 4D is the only option that satisfies all four drivers at once,
and it does so because of a property of the tree rather than a new mechanism.
`web/shared/css/themes.css` keys its eight blocks on **bare** attribute
selectors — seven exactly so (`:38`, `:61`, `:82`, `:103`, `:124`, `:145`,
`:167`) and the eighth as the list `:root, [data-theme="dark"]` (`:15`) — not
on `:root[data-theme=…]`. A CSS custom property declared by a block that
matches an element is set **on that element**, and a directly-matching
declaration always beats an inherited value, so a stamped swatch re-resolves
`--color-primary` for its own subtree regardless of the page theme. The eight
values are verified distinct: `#7c3aed`, `#01696f`, `#7c6af7`, `#4dbb6e`,
`#5b9cf6`, `#f0a04a`, `#e05c7a`, `#3fbf9c`.
*Consequences:* (1) **D-15** — stamping re-resolves *every* token in the
swatch's subtree, so its 1px border follows the previewed theme rather than
the page; this replaces the donor's fixed `rgba(255,255,255,0.15)`
(`css/app.css:810`) and is the reason the border reads correctly against both
light and dark swatch fills. (2) The mechanism is invisible to markup
inspection, so **B4.2** must assert eight **distinct computed** backgrounds —
`getComputedStyle(el).backgroundColor` collected across one open picker,
`Set(...).size === 8` — which is the criterion this ADR lives or dies by.
(3) A future `.ui-theme-swatch` descendant inherits the previewed palette too;
that is intended, and it is why no other rule in `components.css` may key off
`.ui-theme-swatch`'s ancestors.
*Follow-ups:* none. If 4D is ever invalidated, 4A is the pre-analysed
replacement and only `components.css` plus the picker's build function change.

**ADR-016 — C6 is split into C6a (theme system) and C6b (drawer).**
*(New in v2. Both reviewers proposed the split independently; **adopted**.)*
*Decision:* nine commits, not eight: C1–C5, **C6a**, **C6b**, C7, C8. C6a
lands todo's build entry, `shell.ts`, the `ThemeManager` wiring, the
`theme.js` deletion and the `#theme-toggle`/`#theme-icon` work on **both**
`index.html` and `compare.html`, with `initDrawer` present but a no-op. C6b
puts todo's existing drawer onto `HamburgerMenu`, touching `index.html` only.
*Drivers:* v1's single C6 contained the plan's one **jointly unsatisfiable**
claim — it asserted todo renders pixel-identically while in the same commit
deleting and re-implementing the drawer. The claim is only true *by
construction* if the drawer is untouched in the commit that makes the pixel
claim. Second driver: revertability. A single C6 makes "back out the drawer
migration but keep the theme adoption" impossible, and those are the two
halves most likely to fail for unrelated reasons (`$(document).ready` timing
versus FontAwesome icon state).
*Alternatives:* keep one C6 and weaken the pixel criterion to "no
*unintended* diff" (unfalsifiable — every diff becomes arguable); split the
other way, drawer first (worse: the drawer migration needs `shell.ts`, which
only exists after the build entry lands, so it would require a throwaway
scaffold).
*Why chosen:* it makes an existing criterion true instead of rewording it,
and it costs exactly one extra boundary.
*Consequences:* (1) one commit ships a no-op `initDrawer`, which
`make check` passes and a browser cannot observe. (2) **The split adds one
more mandatory step to the revert chain**: C6b must be reverted **before**
C6a, because C6b's `initDrawer` body lives in the `shell.ts` that C6a creates
— reverting C6a first orphans it. *(v2 called this "revert order is reversed
relative to the sequence", which is misleading: strict LIFO is the normal
order for a dependency chain and every other pair in §6 obeys it too. What is
notable about C6a/C6b is not the direction but that the ordering is
**not optional** — the two commits are not independently revertible, which is
the one cost the split buys. §6's revert paragraph carries the full chain,
including C1's last-revertable position.)* (3) `compare.html` belongs
to C6a only, which is what lets C6b's Touches cell be a single file.
*Follow-ups:* §11 item 7 (compare.html's own hamburger, Phase 3).

---

## §13 — Self-audit clauses

Run before this plan is handed to Architect, and again before it is handed to
Critic.

**(a) Phantom-citation and census derivation — hardened in v2, and it is the
derivation of record for §7's four numbers.** Phase-1's reviewers punished
phantom citations. v1's clause had a **regex-ordering bug** that made it
unusable: in `\.(ts|tsx|js|mjs|css|html|go|md|json)` the alternative `js`
precedes `json`, and regex alternation is ordered, so every `.json` citation
was truncated to a `.js` path and reported as a phantom. Running v1's clause
verbatim yields spurious hits like `local-test/config.js`. The alternation is
now **longest-first with a trailing boundary**, and the clause classifies
rather than merely testing:

```sh
python3 - <<'PY'
import re, io, os
s = io.open('docs/PLAN-ui-unification-phase2.md', encoding='utf-8').read()
# The audited body is §0-§12 PLUS §14. §13 itself is excluded because it quotes
# the patterns it searches for — a clause that scanned its own text would
# report its own examples as findings, which is how v2's first run of (a3)
# "failed" with 2 hits, both of them this script and its own result row.
body = (re.search(r'^## §0 —.*?(?=^## §13 —)', s, re.S | re.M).group(0)
        + re.search(r'^## §14 —.*\Z', s, re.S | re.M).group(0))

# (a1) every full path cited in the body is classified, not just existence-tested.
pat = re.compile(r'(?:web|scripts|internal|cmd|docs|tools|local-test)/'
                 r'[A-Za-z0-9_./-]+\.(?:tsx|ts|mjs|json|js|css|html|go|md|txt)(?![A-Za-z0-9])')
paths = sorted(set(pat.findall(body)))
CREATES = {  # the 9 paths this plan brings into existence; each is marked "new" at every mention
  'web/shared/ts/toast.ts', 'web/shared/ts/toast.test.ts', 'web/shared/ts/theme.test.ts',
  'web/shared/ts/menu.ts',  'web/shared/ts/menu.test.ts',  'web/shared/ts/test-dom.ts',
  'web/todo/js/shell.ts',   'web/todo/js/shell.js',         'scripts/artifact-paths.mjs' }
print('paths cited      :', len(paths))
print('  existing       :', sum(1 for p in paths if os.path.exists(p)))
print('  declared new   :', len([p for p in paths if p in CREATES]))
COUNTERFACTUAL = {  # cited to be refuted, not to be read — see the note below
  'web/obsidianoid/js/app.js/app.js', 'internal/obsidianoid/build_test.go' }
print('  counterfactual :', len([p for p in paths if p in COUNTERFACTUAL]))
print('  PHANTOM        :', [p for p in paths
                             if not os.path.exists(p) and p not in CREATES | COUNTERFACTUAL])
print('  new-but-present:', [p for p in CREATES if os.path.exists(p)])   # a contradiction if non-empty

# (a2) the §7 census — the definition of record for its four numbers.
sec = re.search(r'^## §7 —.*?(?=^## §8 —)', s, re.S | re.M).group(0)
blocks = re.findall(r'^- \*\*(B(?:\d+|X)\.\d+) —(.*?)(?=^- \*\*B|\Z)', sec, re.M | re.S)
ids  = [i for i, _ in blocks]
dfr  = [i for i, b in blocks if '[deferred' in b]
sub  = [i for i, b in blocks if '[deferred' in b and re.search(r'\*Executed|\*Substitute', b)]
print('criteria         :', len(ids), '| duplicate ids:',
      sorted({i for i in ids if ids.count(i) > 1}) or 'none')
print('  executed       :', len(ids) - len(dfr))
print('  deferred       :', len(dfr), '| of those, with substitute:', len(sub))

# (a3) names Phase 1 retired must not reappear as live. Scoped to the SAME
# body as (a1) — see the comment at the top of this script.
for name in ('cmd/unified-webapp',):
    print(name, sum(1 for line in body.splitlines() if name in line))
PY
```

`--color-error` / `--color-primary-highlight` / `--color-surface-offset` are
deliberately **not** in (a3)'s ban list: they are obsidianoid's *pre-rename*
names, and a plan that specifies a rename has to be able to write the old name
down. What must stay true is narrower and now **machine-checked instead of
hand-counted** — the mentions may sit only in sections that *describe* the
rename, and never in §7, where a criterion naming an old token would be
asserting the old vocabulary:

```sh
python3 - <<'PY'
import re, io
s = io.open('docs/PLAN-ui-unification-phase2.md', encoding='utf-8').read()
lines = s.splitlines()
b13 = s[:re.search(r'^## §13 —', s, re.M).start()].count('\n')
b14 = s[:re.search(r'^## §14 —', s, re.M).start()].count('\n')
heads = [(m.start(), m.group(0).split('—')[0].strip()) for m in
         re.finditer(r'^## §\d+[^\n]*', s, re.M)]
def sec(ln):
    off = sum(len(l) + 1 for l in lines[:ln - 1]); cur = '?'
    for st, h in heads:
        if st <= off: cur = h
    return cur
ALLOWED = {'## §4', '## §5', '## §8', '## §10', '## §14'}
bad = []
for nm in ('--color-error', '--color-primary-highlight', '--color-surface-offset'):
    hits = [(i, sec(i), l.count(nm)) for i, l in enumerate(lines, 1)
            if nm in l and (i <= b13 or i > b14)]
    print(f'{nm:28s} {sum(h[2] for h in hits):3d} mentions  '
          f'sections: {sorted({h[1] for h in hits})}')
    bad += [h for h in hits if h[1] not in ALLOWED]
print('OUT OF ALLOWED SECTIONS:', bad or 'none')
PY
```

**Published at this revision:** `--color-error` **6**, `--color-primary-highlight`
**3**, `--color-surface-offset` **15** — **24** mentions in total, all inside
§4, §5, §8, §10 and §14, **0** outside. *(v2–v4 asserted "4 + 1 + 2 = 7
mentions, all in §4.3's mapping table, its cross-reference and §14's note,
verified by hand". The hand count was correct when written and is wrong now:
v5's Q12 work added twelve `--color-surface-offset` mentions in §5 alone. It
was also the wrong shape of check — a total that has to be re-counted by hand
every revision is the class of hand-maintained integer this pass is removing.
Architect/Critic convergence on machine-derived numbers; the coincidence that
`--color-surface-offset`'s 15 mentions equal its 15 `app.css` references is
just that, a coincidence.)*

**Result, run against this revision (v5). Every integer below is this
revision's own run, not v4's carried forward:**

| Clause | Expected | Actual |
|---|---|---|
| a1 paths cited | — | **73** (v4: 65, v2: 62) — the growth is eight paths, all pre-existing files, itemised below; one of the 73 is this document itself, cited by §15's own `grep` example |
| a1 existing | all but declared-new and counterfactual | **62** (v4: 54) — 73 − 9 − 2, and the identity holds by construction |
| a1 declared new | 9 | **9** |
| a1 counterfactual | — | **2** — both deliberate, see below |
| a1 phantom | 0 | **0** |
| a1 new-but-present | 0 | **0** |
| a2 criteria | — | **82** (v4: 77 — **B2.10**, **B2.11**, **B2.12**, **B4.13**, **B10.6** are new), no duplicate ids |
| a2 executed | — | **6** (B1.1, B1.2, B2.7, B10.5, BX.1, BX.3) — unmoved |
| a2 deferred | — | **76**, of which **31** carry substitutes |
| a3 `cmd/unified-webapp` | 0 | **0** body-scoped (2 unscoped — both this clause) |
| a3 retired token names | 0 outside §4/§5/§8/§10/§14 | **24** mentions, **0** outside — machine-classified in v5, was a hand count |
| **a4** line-citation range | 0 out of range | **89 found, 89 checked, 0 skipped, 0 out of range** (v4: 61; v3 published 0 of 53) |
| **a4** citation diff | 0 defects | **0 of 39** added or changed by the v5 pass — and v5 is the first pass with a real snapshot to diff |
| **a4** content spot-check | 0 wrong anchors | **0 of 17** re-derived |
| **a5** intra-doc pointers | 0 dangling | **0** (first run over v3's text: **12** over 5 targets) |
| **a5** §7 census restatement | equals (a2) | **PASS** — 82/6/76/31 both sides |
| **a6** deleted-symbol references | complete + reconciled | **13** symbols, **36** remaining references — new in v5, below |
| **d2** label ⇄ schedule | equal in both directions, ×9 | **PASS**, 9/9 — **136** (criterion, boundary) pairs |
| **(e)** §6 Touches ⊆ allowlist | 0 outside | **79** path tokens, **0** outside — derived in v5, and it failed on first run |

**The eight paths (a1) gained since v4**, every one of them a file that exists
today and none of them a new citation target the plan intends to create:
`scripts/build-web.mjs` (driver rule 10 — the `@shared` externalisation chain
behind `type="module"`), `internal/platform/static/shared.go` and
`internal/platform/static/shared_test.go` (Q9's carve-out, §5 Step 8),
`web/certmachine/index.html`, `web/multissh/index.html` and
`web/taskmaster/index.html` (the three comparators in the stylesheet-order
argument), and `web/obsidianoid/js/{app.js,threads.js}` (C5's tracked emitted
mirrors, which §13(b) duplication 5's reasoning had to name explicitly).
Derived by diffing (a1)'s path set against its run over the v4 snapshot, which
is also part 3's baseline of record — **added 8, removed 0**.

The six "executed" criteria are exactly the six that carry
`[re-asserted → …]` rather than `[deferred → …]`: they hold in the tree
today and the label says where they are re-checked. That identity is not a
coincidence the reader has to take on faith — (a2) derives the split from the
label, so the two can only disagree by a visible edit.

**The two counterfactual paths, and why they are a named category rather than
a silenced result.** Both are cited *in order to be refuted*, and a clause
that cannot tell a refutation from a claim would have to be satisfied by
deleting the argument — so v2 gives them a set of their own,
`COUNTERFACTUAL`, printed on its own line rather than folded into the pass:

1. `web/obsidianoid/js/app.js/app.js` — ADR-011's v2 amendment: the absurd
   path a file-valued descriptor `out` would produce through
   `list-artifacts.mjs:38-45`. It is the evidence for a rebuttal (§14,
   Critic M-3).
2. `internal/obsidianoid/build_test.go` — **v1's own phantom, now quoted as
   the defect it was.** Step 5.4, §13(d) and §14's Critic-M-9 row each name it
   to record that no such file exists; the package's test files are
   `events_test.go`, `git_test.go`, `handler_test.go`, `state_test.go`,
   `threads_test.go` and `vault_test.go`. A plan that documents its own
   phantom necessarily contains the phantom's name, which is the whole reason
   this category has to exist.

With both classified, **the phantom count at this revision is 0** — and the
clause still fails loudly if a genuinely new phantom appears, because
widening the exception set is a visible edit to this document rather than a
silent change in a script.

Every path cited in §4 was read in this tree during planning.

**(a4) Line-citation check — new in v3 (Architect N2).** (a1) proves a cited
*file* exists. It says nothing about the `:NNN` after it, and that is where
the one substantive citation defect in v2 lived: `web/todo/css/todo.css`'s
drawer block was cited as `:102-143` when the tree has `.sidebar {` at
**`:103`** and the block's last line at **`:144`** — a uniform off-by-one
that propagated through 20 rows of §5 Step 6.2a's table because the rows were
written from the first citation rather than re-derived. A file-existence
check could never see it. So (a4) adds three dimensions — two bounded so they
stay runnable, and one exhaustive because the diff is small by construction:

1. **Range (mechanical, exhaustive).** Every fully-qualified `path:N` or
   `path:A-B` citation in the audited body must satisfy `1 ≤ A ≤ B ≤ wc -l
   path`. This is cheap, total, and catches the whole class of citations left
   behind by a file that shrank.

   ```sh
   python3 - <<'PY'
   import re, io, os
   s = io.open('docs/PLAN-ui-unification-phase2.md', encoding='utf-8').read()
   body = (re.search(r'^## §0 —.*?(?=^## §13 —)', s, re.S|re.M).group(0)
           + re.search(r'^## §14 —.*\Z', s, re.S|re.M).group(0))
   cit = re.compile(r'((?:web|scripts|internal|cmd|docs|tools|local-test)/'
                    r'[A-Za-z0-9_./-]+\.(?:tsx|ts|mjs|json|js|css|html|go|md|txt)):(\d+)(?:-(\d+))?')
   bad = []; seen = set(); skipped = set()
   for p, a, b in cit.findall(body):
       k = (p, a, b)
       if k in seen: continue
       if not os.path.exists(p):
           skipped.add(k); continue
       seen.add(k)
       total = sum(1 for _ in io.open(p, encoding='utf-8', errors='replace'))
       lo, hi = int(a), int(b or a)
       if lo < 1 or hi > total or hi < lo: bad.append((p, lo, hi, total))
   print('found:', len({(p, a, b) for p, a, b in cit.findall(body)}),
         '| checked:', len(seen), '| skipped:', sorted(skipped) or 'none')
   print('OUT OF RANGE:', bad or 'none')
   PY
   ```

   **Result: 89 found, 89 checked, 0 skipped, 0 out of range.**
   *(**v5** — v4 published **61** and v3 **53**. The jump to 89 is the cost of
   v5's two largest additions: Q9's carve-out reaches into
   `internal/platform/static/` and `scripts/build-web.mjs`, and the Q12 option-B
   work cites `themes.css` donors and `main.ts`'s table by line. All 89 resolve
   to files that exist, so `found` and `checked` still coincide and `skipped` is
   still empty — which is the property the v4 reporting change was made to keep
   visible.)* *(**v4** — v3 published **53**; v4's edits added qualified
   citations to the audited body and the count was then 61, all of them
   resolving to files that exist. Two changes to the clause's *reporting*, both prompted by the Critic
   reporting "checks 49, publishes 53" for the v3 text: the script now prints
   **found**, **checked** and the **skipped** list as three separate numbers
   rather than one, because v3's `len(seen)` silently excluded any citation
   whose path does not resolve — a citation can therefore be *found* and never
   *checked*, which is precisely the shape of gap this clause exists to
   forbid. I could not reproduce a 49/53 split from the v3 text and do not
   have a v3 snapshot to diff against, so no explanation for the gap is
   offered here; what v4 does instead is make the two quantities visible so
   they cannot diverge unremarked again. At **that** revision they coincided at
   **61 found, 61 checked, 0 skipped**.)*

2. **Content (bounded, by hand-with-a-script).** Range cannot catch an
   off-by-one — `:102` is in range — so a sample of anchors is re-derived by
   printing the cited line and confirming the construct the prose claims is
   there. The sample is deliberately not "all 61": it is the places where a
   wrong anchor does the most damage, which is **§5 Step 6.2a's D-table** (the row
   that carried N2's defect) **plus the ten tables with the highest citation
   density in the document** — 16 distinct qualified citations, plus
   `web/obsidianoid/css/app.css:810`, the one bare-path citation in the
   D-table.

   **Result: 17 of 17 re-derive correctly.** *(**v4** — v3's sentence here
   added "including every citation the v3 pass rewrote", which was false and
   is deleted. The v3 pass also wrote `scripts/gates/bundle-shape.mjs:100`,
   which is not in this sample and was wrong; the sample is chosen by *damage
   and density*, and the pass's own new writes are a different axis
   entirely. Part 3 below is that axis. Architect A7, Critic F-4.)*

   | Citation | Re-derived first line |
   |---|---|
   | `web/todo/css/todo.css:103-144` | `.sidebar {` … `.sidebar-backdrop.sidebar-open { display: block; }` — **the N2 fix, confirmed** |
   | `web/obsidianoid/css/app.css:810` | `border: 1px solid rgba(255,255,255,0.15);` — D-15's donor value |
   | `web/shared/css/themes.css:20,43,66,87,108,129,150,172` | eight `--color-border:` declarations; `:172` is `rgba(255, 255, 255, 0.11)` — the one translucent value, as D-15 says |
   | `web/obsidianoid/js/app.ts:425` | `function themeStorageKey() { return \`obsidianoid-theme-${state.activeVault}\`; }` |
   | `web/obsidianoid/js/app.ts:433-447` | `function buildThemePanel() {` … `}` |
   | `web/obsidianoid/js/app.ts:46-52` / `:54-61` | `const THEMES = [` … / the `Toast` block |
   | `web/shared/ts/theme.ts:8-17` | `export const THEMES = [` … `] as const;` |
   | `web/shared/css/themes.css:61-163` | `[data-theme="obsidian"] {` … rose's closing `}` — **corrected from `:61-164` in v5 FINAL** (Critic minor 2): `:61` is right, but `:164` is the **blank line** after rose's `}` at `:163`. Re-derived by `sed -n '59,63p;160,166p'`. The same off-by-one stood at (b)'s obsidianoid-`themes.css` row and at §5 Step 5.3 item 2, and all three are fixed |
   | `web/todo/index.html:78-85` / `:21-164` | `function toggleSidebar() {` … / `<script>` … `</script>` (144 lines) |
   | `web/todo/compare.html:41-43` | the `id="theme-toggle"` button |
   | `web/todo/js/theme.js:6-9` | the `#theme-icon` class swap |
   | `web/todo/css/panels.css:57-101` | `#rightToggle {` … `}` |
   | `web/todo/menuserver.html:16` | `<link rel="stylesheet" href="css/responsivenav.css">` |
   | `web/obsidianoid/css/app.css:383` | `dialog::backdrop { background: oklch(0 0 0 / 0.6); }` |
   | `web/obsidianoid/css/app.css:202` / `:437` / `:455` | the three `--color-surface-dynamic` / `--color-surface-offset` rules (ledger row 14, B2.8) |
   | `scripts/descriptors.mjs:162` | `export const EXPECTED_ARTIFACT_COUNT = 15;` |
   | `scripts/check-shared-css.mjs:314` | `if (!UI_SELECTOR.test(one)) {` — clause 7's selector loop (ADR-015) |
   | `docs/OPEN-QUESTIONS-ui-unification.md:126-129` | Q4's text |

   **Why bounded and not exhaustive.** Re-deriving all 61 by hand is the kind
   of clause that gets skipped on the next revision, and a clause that is
   skipped is worse than a smaller clause that is run (P-III's shape, applied
   to the audit itself). The range half is exhaustive and free; the content
   half is scoped to the tables where the v2 defect actually occurred and
   where density makes a single stale anchor spread.

3. **Diff (exhaustive over the pass's own writes) — new in v4, and it is now
   the standing rule for every future revision.** *(Architect's consensus
   addendum, Critic F-4.)* **Every `path:N` citation that this revision added
   or changed is re-derived against the tree before the revision is
   published** — not sampled, not spot-checked: all of them.

   **Why this axis and not a bigger sample.** Three consecutive iterations
   introduced exactly one fresh citation defect each, and in all three cases
   the defect was on the surface the pass had just edited: v2's
   `todo.css:102-143`, v3's `bundle-shape.mjs:100`, and — caught inside this
   pass rather than shipped — v4's first draft of §5 Step 5.6, which cited a
   *plan* line number (`:603`) in a document whose `:NNN` convention resolves
   against the last-named **file**. Part 2 samples by damage and density and
   would have caught **none** of the three: `bundle-shape.mjs` is cited six
   times in prose, not in a dense table, and a brand-new paragraph has no
   density history at all. **The property that predicts a citation error is
   recency of edit, not citation density** — so the right sample is the diff,
   and the diff is small by construction, which is what makes an exhaustive
   rule affordable. Parts 2 and 3 are therefore complements, not a wide and a
   narrow version of the same check: part 2 guards the *inherited* document
   against tree drift, part 3 guards the *new* text against the author.

   **Mechanism.** Before its first edit, the pass snapshots the revision it
   was handed (this document is untracked, so `git diff` is unavailable — a
   scratch copy is the baseline of record), and at the end diffs the two,
   extracts every `path:N` / `path:A-B` token appearing in an added or changed
   line, and re-derives each by printing the cited line and checking it
   against what the surrounding prose claims. Range-checking is not enough:
   every one of the three historical defects was **in range**.

   **v5's run — 39 distinct qualified citations in added or changed lines,
   39 re-derived correct, 0 unresolvable, 0 out of range, 0 defects**, over
   **2,756** added or changed lines as measured at the close of the pass.

   > **One of those two numbers is stable and one cannot be.** 39/39/0 is
   > stable under any edit that changes only integers, which is what the rest
   > of this refresh does. The added-line total is **self-referential** —
   > publishing it changes it, because the line that publishes it is itself
   > an added line — so it is stated as *measured at the close of the pass*
   > and is the one figure in this document that a reviewer re-running the
   > clause should expect to differ by a handful. The load-bearing assertion
   > is the defect count, not the diff volume; the volume is published only
   > to show the clause was exhaustive over a large diff rather than a
   > convenient one. Recorded rather than quietly rounded, because "no
   > hand-maintained integers" and "a number that changes when you write it
   > down" are in genuine tension here and the tension should be visible.

   *(v5 is the first pass to run this clause as written: a scratch
   copy of the v4 text was taken **before the first edit** and the changed set
   comes from `difflib.ndiff` over the two, not from the author's memory of
   what he edited. The mechanism paragraph above is therefore now a
   description of something that happened rather than an instruction for a
   future pass.)*

   ```sh
   python3 - <<'PY'
   import re, io, os, difflib, collections
   BASE = '<scratch copy of the revision this pass was handed>'
   new = io.open('docs/PLAN-ui-unification-phase2.md', encoding='utf-8').read().split('\n')
   old = io.open(BASE, encoding='utf-8').read().split('\n')
   added = [l for l in difflib.ndiff(old, new) if l.startswith('+ ')]
   cit = re.compile(r'((?:web|scripts|internal|cmd|docs|tools|local-test)/'
                    r'[A-Za-z0-9_./-]+\.(?:tsx|ts|mjs|json|js|css|html|go|md|txt)):(\d+)(?:-(\d+))?')
   c = collections.Counter()
   for l in added:
       for p, a, b in cit.findall(l): c[(p, a, b)] += 1
   print('added/changed lines:', len(added), '| distinct citations:', len(c))
   bad = []
   for (p, a, b) in sorted(c):
       if not os.path.exists(p): bad.append(('MISSING', p, a, b)); continue
       tot = sum(1 for _ in io.open(p, encoding='utf-8', errors='replace'))
       lo, hi = int(a), int(b or a)
       if lo < 1 or hi > tot or hi < lo: bad.append(('RANGE', p, lo, hi, tot))
   print('unresolvable or out of range:', bad or 'none')
   for (p, a, b), n in sorted(c.items()):
       print(f'  {p}:{a}' + (f'-{b}' if b else '') + f'  x{n}')
   PY
   ```

   > **The script is half the clause; the other half is not mechanisable and
   > was done by hand over all 39.** The loop proves *resolvable and in
   > range* — the property every one of the three historical defects
   > satisfied. What the pass then did for each of the 39 is print the cited
   > line and read it against the claim the new prose makes about it. Two of
   > the 39 are worth naming because they are the evidence for a defect this
   > pass found in its own new audit clause: `web/obsidianoid/js/app.ts:478`
   > is `const stored = localStorage.getItem(themeStorageKey());` and
   > `web/todo/js/theme.js:13` is `var stored = localStorage.getItem(STORAGE_KEY);`
   > — the two lines §13(a6)'s first exclusion regex silently swallowed.
   > Neither would have been caught by range-checking, and one of them is a
   > call site Critic F-B rated CRITICAL.

   **v4's run — 62 citations, 62 correct, 0 defects.** The v4 pass did not
   snapshot v3 before starting (the rule was new in that very pass), so the
   changed set was enumerated from the pass's own edit list instead of from a
   diff — a weaker derivation, stated as such, and the reason the snapshot
   step is written into the mechanism above. The set spans
   the twelve surfaces v4 edited: the six `bundle-shape.mjs:101` sites; §3's
   P5a cell; §5 Steps 5.3, 5.4, 5.6, 6.2, 6.3 and the pre-paint bullet; §4.4's
   drawer inventory; B1.3's C4 leg; B1.4's probe 2; and BX.10. Every citation
   resolved in range **and** matched the construct the prose names — e.g.
   `web/todo/css/todo.css:124` is `.sidebar-section { padding: .5rem 0; }`,
   `:615` is the `/* ── Responsive ── */` heading in a 619-line file,
   `web/sampler/index.html:36` is the module tag and `:3`/`:9` bracket a
   script-free `<head>`, `scripts/check-shared-css.mjs:315` is the clause-7
   `fail(file, block.line, 7, …)` call, and `web/obsidianoid/css/themes.css`
   is **189** lines with `--shadow-md` at `:25`.

   **What part 3 cannot catch, stated so it is not assumed.** It checks that a
   citation *resolves to what the prose says is there*. It cannot tell that a
   correctly-resolving pointer is aimed at the **wrong** thing — BX.1's
   "(Step 5.1)" named a step that exists and was still the wrong step
   (Architect A6). That class needs a human reading the claim, or (a5)'s
   narrower structural version of it.

   **And a second limit, stated in v5 because the script now exists to be
   run by someone else.** The regex matches **fully-qualified** citations
   only. This document's convention lets a citation be **bare** — `:810`,
   `:2205` — resolving against the last-named file in the surrounding prose,
   and the `app.css` colour inventory in §5 Step 5.3a is written that way on
   purpose because seven rows of the same path would be noise. Those bare
   citations are **outside** part 3's automated half; they are covered by the
   by-hand half (the pass re-derived every row of that inventory by `grep`,
   which is how `:383`'s `oklch` and `:740`'s `%23` were found to escape a
   naive colour sweep) and by part 2's sample, which includes the D-table's
   one bare citation explicitly. Resolving bare citations mechanically needs
   a last-named-file tracker across the whole body, which is a real piece of
   machinery and not one this pass built. Recorded as a limit rather than
   left as an assumption — the number **39** is a count of *qualified* new
   citations, not of all new citations.

   **A third limit, which is a convention rather than a gap: short-form
   `file.ext:NN` citations are module-scoped.** *(New in **v5 FINAL**, Critic
   "what's missing".)* **Fourteen** modules under `web/` own a file named
   `index.html` (`ls web/*/index.html | wc -l` = 14 — every module directory
   except `web/shared/`), and this document cites **three** of them in short
   form — the sampler's, obsidianoid's and todo's — because a step that has
   already named its module does not repeat the prefix on every line. The same
   shape recurs in three more stems, each machine-checked here rather than
   assumed: `app.css` has **two** owners (`web/obsidianoid/css/app.css`, cited
   throughout §5 Step 5, and `web/slideshow/css/app.css`, which this phase
   never touches); `main.ts` has **four** (certmachine, multissh, sampler,
   taskmaster — only the sampler's is cited); and `theme.ts` / `theme.js` are
   **different files with the same stem** in different languages —
   `web/shared/ts/theme.ts`, which gains `ThemeManager` at C3, and
   `web/todo/js/theme.js`, which C6a deletes.
   **The rule, stated once: a short-form citation resolves against the module
   the enclosing step, criterion or ledger row is about, and never against a
   same-named file in another module.** In practice the enclosing §5 step
   heading or §7 criterion headline always names the module within a few
   lines. This is worth writing down because a reviewer's mechanical range
   sweep over short-form citations cannot know the scope and will
   over-report: the Critic's first pass over v5 DRAFT produced **22 false
   positives** from exactly this, a candidate `index.html` from the wrong
   module being tried against a short-form `index.html:NN` whose enclosing
   prose had already fixed its module. The convention is not changed
   — expanding 135 short-form citations to full paths would trade one kind of
   noise for another — but a reader or a future audit script now has the
   scoping rule in the document instead of having to infer it, and any citation
   whose module is *not* obvious from its surroundings is a defect to be fixed
   at the citation, not at the convention.

**(a5) Intra-document pointer check, and the §7 census restatement — new in
v4 (Architect M-3, Critic F-5).** (a1) and (a4) audit pointers *out* of this
document, into the tree. Nothing audited pointers *within* it, and that was
the third audit blind spot: v3 shipped "§4.2's drawer inventory" for a list
that lives in §4.4, "§5.5's table" for a table that lives in §5 Step 6.2a,
and a §7 census paragraph contradicting §13's own result table one screen
below it. Every one of those is a cross-reference defect, and every one was
found by a reviewer reading, not by a command.

**The pointer convention this clause enforces**, stated because v3 used two
spellings interchangeably: `§N` and `§N.M` are legal only where a **heading**
of that number exists (`## §7 —`, `### 4.4`, `#### 4.2.1`, `### §8.5`). §5's
sub-steps are **not** subsections — they are bold labels inside §5 — so they
are cited as **`§5 Step 5.6`**, never `§5.6`. §13's clauses are cited as
`§13(x)` where `x` is either a `**(x)**` clause header or one of the `(a1)`
… `(a3)` labels inside (a)'s script.

```sh
python3 - <<'PY'
import re, io
s = io.open('docs/PLAN-ui-unification-phase2.md', encoding='utf-8').read()
body = (re.search(r'^## §0 —.*?(?=^## §13 —)', s, re.S|re.M).group(0)
        + re.search(r'^## §14 —.*\Z', s, re.S|re.M).group(0))
secs    = set(re.findall(r'^## §(\d+) —',            s, re.M))
subs    = set(re.findall(r'^#{3,4} §?(\d+(?:\.\d+)+)', s, re.M))
steps   = set(re.findall(r'^### Step (\d+) →',       s, re.M))
substep = set(re.findall(r'^\*\*(\d+\.\d+[a-z]?) —', s, re.M))
clauses = set(re.findall(r'^\*\*\(([a-z]\d?)\)',     s, re.M)) \
        | set(re.findall(r'^# \((a\d)\)',            s, re.M))
bad = []
for m in re.finditer(r'§(\d+(?:\.\d+)*)', body):
    t = m.group(1)
    if not (t in subs if '.' in t else t in secs): bad.append(('section', t))
for m in re.finditer(r'Step (\d+(?:\.\d+[a-z]?)?)', body):
    t = m.group(1)
    if not (t in substep if '.' in t else t in steps): bad.append(('step', t))
for m in re.finditer(r'§13\(([a-z]\d?)\)', body):
    if m.group(1) not in clauses: bad.append(('clause', m.group(1)))
print('DANGLING POINTERS:', len(bad), sorted(set(bad)) or 'none')

# the §7 census restatement must equal (a2)'s derivation
sec7 = re.search(r'^## §7 —.*?(?=^## §8 —)', s, re.S|re.M).group(0)
blk  = re.findall(r'^- \*\*(B(?:\d+|X)\.\d+) —(.*?)(?=^- \*\*B|\Z)', sec7, re.M|re.S)
ids  = [i for i, _ in blk]
dfr  = [i for i, b in blk if '[deferred' in b]
sub  = [i for i, b in blk if '[deferred' in b and re.search(r'\*Executed|\*Substitute', b)]
derived = (len(ids), len(ids)-len(dfr), len(dfr), len(sub))
m = re.search(r'There are (\d+) criteria below\. (\d+) were executed against\s+'
              r'this tree; (\d+) carry `\[deferred\]`, and (\d+) of those', sec7)
stated = tuple(int(x) for x in m.groups()) if m else None
print('census derived:', derived, '| §7 states:', stated,
      '|', 'PASS' if stated == derived else 'FAIL')
PY
```

**Result at this revision: 0 dangling pointers; census PASS — derived
`(82, 6, 76, 31)`, §7 states `(82, 6, 76, 31)`.** *(**v5.** v4 published
`(77, 6, 71, 28)` on both sides. The two sides were re-derived independently
this pass — §7's restatement was rewritten from (a2)'s output, not adjusted
by arithmetic from the old numbers — and the known clause labels the resolver
accepts now include **`a6`** and **`e`**, which is why §17's dozen
`§13(x)` pointers resolve. The clause was also run **twice**: once before §17
existed, when it correctly reported **1 dangling pointer (`§17`)**, and again
after, when it reported 0. A clause that reports the section you have not
written yet is behaving correctly, and publishing only the second run would
have hidden that it did.)* The first run of this clause
against the v3 text it was written to audit reported **12 dangling pointers
over 5 distinct targets** — `§5.6` (six sites), `§5.5` (two), `§16` (one, the
section this revision adds), and the clause vocabulary gaps `(a2)`/`(a5)` that
made the clause's own resolver incomplete. All are fixed above; the resolver
now admits `(a1)`–`(a3)` because (a)'s script labels them.

**What (a5) does not do.** It resolves pointers; it does not verify **aim**. A
reference to a step that exists but is the wrong step passes — which is
exactly what BX.1's "(Step 5.1)" did. That is the residual class, and it is
declared rather than papered over: the honest coverage claim for
cross-references in this document is "no dangling pointers, and a reviewer for
mis-aimed ones."

**(a6) Deleted-symbol reference completeness — new in v5 (Critic item 8,
Architect synthesis).** (b) below checks that every deleted *fact* has a named
survivor. It does **not** check that every *caller* of a deleted symbol has
somewhere to go, and that is a different question with a different failure
mode: (b) is satisfied by naming `ThemeManager` as the survivor of
`themeStorageKey`, while the commit still fails to compile because a call site
nobody enumerated still says `themeStorageKey()`. That is exactly the shape of
iteration-4's CRITICAL (F-B): three surviving `app.ts` call sites, a plan that
named the deletion and the survivor, and no enumeration in between. So the
clause is mechanical: **for every symbol whose definition this plan deletes,
list every remaining reference in the authored sources of the owning module,
and give each one a disposition.**

```sh
# scope: authored sources only — *.ts and *.html per module, *.mjs under
# scripts/, plus web/todo/js/*.js (todo has no TS build until C6a). The
# tracked emitted mirrors (web/*/js/*.js next to a .ts) are deliberately
# excluded: they are rebuilt by the same commit, never hand-edited, and
# including them doubles every count with no new information.
ROSTER='
THEMES|web/obsidianoid/js/app.ts|web/obsidianoid|C5
showToast|web/obsidianoid/js/app.ts|web/obsidianoid|C5
themeStorageKey|web/obsidianoid/js/app.ts|web/obsidianoid|C5
setTheme|web/obsidianoid/js/app.ts|web/obsidianoid|C5
buildThemePanel|web/obsidianoid/js/app.ts|web/obsidianoid|C5
toggleThemePanel|web/obsidianoid/js/app.ts|web/obsidianoid|C5
STORAGE_KEY|web/todo/js/theme.js|web/todo|C6a
applyTheme|web/todo/js/theme.js|web/todo|C6a
getPreferred|web/todo/js/theme.js|web/todo|C6a
toggleTheme|web/todo/js/theme.js|web/todo|C6a
toggleSidebar|web/todo/index.html|web/todo|C6b
closeSidebar|web/todo/index.html|web/todo|C6b
jsName|scripts/list-artifacts.mjs|scripts|C1
'
scan() {
  if [ "$1" = "scripts" ]; then find scripts -name '*.mjs' -not -path '*/node_modules/*'
  else find "$1" \( -name '*.ts' -o -name '*.html' \) -not -name '*.test.ts'
       [ "$1" = "web/todo" ] && find web/todo -name '*.js'; fi
}
echo "$ROSTER" | while IFS='|' read -r sym file dir at; do
  [ -z "$sym" ] && continue
  # Exclude ONLY the owning definition line. The symbol name must follow the
  # keyword — see the blockquote below for why a bare keyword list is unsafe.
  DEF="^${file}:[0-9]+:[[:space:]]*(export[[:space:]]+)?(function[[:space:]]+${sym}\b\
|(const|let|var)[[:space:]]+${sym}[[:space:]]*=\
|window\.${sym}[[:space:]]*=)"
  refs=$(scan "$dir" | sort | xargs grep -nE "\b${sym}\b" 2>/dev/null | grep -vE "$DEF")
  printf '%-17s %-4s n=%s\n' "$sym" "$at" "$(printf '%s' "$refs" | grep -c .)"
  printf '%s\n' "$refs" | sed 's/^/    /'
done
```

> **The clause's own first draft was broken, and it failed in the one
> direction an audit clause must never fail. (v5, caught inside this pass.)**
> The exclusion regex started life as a bare keyword list —
> `(function|const|let|var|window\.${sym}|export function)` — with no
> requirement that the symbol **follow** the keyword. Run verbatim against
> this tree it prints `themeStorageKey n=1`, `STORAGE_KEY n=1`,
> `toggleTheme n=2`, total **34**, because it discards any line that merely
> *begins* with a declaration keyword:
>
> - `web/obsidianoid/js/app.ts:478` — `const stored = localStorage.getItem(themeStorageKey());`
> - `web/todo/js/theme.js:13` — `var stored = localStorage.getItem(STORAGE_KEY);`
>
> The first of those is **one of the three call sites iteration-4's CRITICAL
> (F-B) was about**. A clause written to make F-B's class impossible would
> have hidden F-B's own evidence — a false negative, silently, with a green
> total. P-III's dual in its sharpest form: not a gate that cannot fail, but
> a gate that fails to see. The fix is to anchor the exclusion to a real
> definition (the name must follow the keyword), and the discipline it
> illustrates is the one this pass adopted for gates generally — **run the
> published command, compare its output to the published number, and treat a
> disagreement as a defect in whichever of the two is wrong.** Here it was
> the script. Total moves **34 → 36**, and the two suppressed references
> reappear.

**Result at this revision: 13 symbols, 36 remaining references, every one
with a named disposition.** The per-symbol counts, as printed:

| Symbol | Owning file | At | Refs | Disposition of every reference |
|---|---|---|---|---|
| `THEMES` (local) | `app.ts:46-52` | C5 | **1** | `:434`, inside `buildThemePanel`'s body — dies with the function it is in. Survivor for the *data*: `web/shared/ts/theme.ts:8-17`. |
| `showToast` | `app.ts:56` | C5 | **18** | 11 calls in `app.ts` + 5 in `threads.ts` become `@shared` calls at the same signature (ADR-010 lifted the donor verbatim); the remaining 2 are `threads.ts:14`'s comment and `:15`'s `declare`, both deleted by §5 Step 5.2. Asserted by **B5.8**. |
| `themeStorageKey` | `app.ts:425` | C5 | **2** | `:430` dies with `setTheme`'s body; `:478` is one of F-B's three call sites, rewritten by §5 Step 5.2. Survivor: `ThemeManager`'s `storageKey()`. |
| `setTheme` (local, 2-arg) | `app.ts:427` | C5 | **3** | `:439` dies with `buildThemePanel`; `:479` and `:494` are F-B's other two, both becoming `reresolve()` — **not** `set()` — per §5 Step 5.2. Asserted by **B2.10**, **B2.11**. |
| `buildThemePanel` | `app.ts:433` | C5 | **1** | `:503`, the module-level boot line — the two-statement precision in §5 Step 5.2's call-site table. |
| `toggleThemePanel` | `app.ts:444` | C5 | **1** | `:449`, the `btnHamburger` listener — claimed by `HamburgerMenu`'s `mountTrigger` (§5 Step 5.2). |
| `STORAGE_KEY` | `theme.js:2` | C6a | **2** | `:13`, `:24`, both inside the IIFE being deleted whole. Survivor: `ThemeManager`'s `storageKey()`. |
| `applyTheme` | `theme.js:4` | C6a | **2** | `:19`, `:25`, both inside the same IIFE. |
| `getPreferred` | `theme.js:12` | C6a | **1** | `:19`, same IIFE. Survivor for the *logic*: `ThemeManager`'s resolution order, and the inline pre-paint block for the first-paint half. |
| `toggleTheme` | `theme.js:21` | C6a | **2** | `index.html:179` and `compare.html:41`, both **inline `onclick` attributes**, removed by §5 Step 6.3 and asserted by **B4.12** on both pages. *(The `window.toggleTheme` assignment at `theme.js:21` is the definition, so the clause excludes it — which is why this row reads 2 while **B4.12** asserts **0 of 3**: B4.12 counts every occurrence in `web/todo/`, definition included, because its job is that nothing of the name survives anywhere. Two measures, two purposes, stated so the numbers are not read as a contradiction.)* |
| `toggleSidebar` | `index.html:78` | C6b | **1** | `index.html:172`'s inline `onclick`, removed by §5 Step 6.2 (the element itself is kept as `mountTrigger`'s target). |
| `closeSidebar` | `index.html:82` | C6b | **1** | `index.html:195`'s inline `onclick` — the whole `#sidebar-backdrop` element is deleted by §5 Step 6.2, so the reference goes with its host. |
| `jsName` | `list-artifacts.mjs:25` | C1 | **1** | `:39`. Moves with the derivation into `scripts/artifact-paths.mjs`, imported twice. Asserted by **B9.4**. |

> **What the first run of this clause found, and why the scope is
> module-and-authored rather than repo-wide. (v5.)** Two findings, one of
> each kind.
>
> **1. The inline `onclick` class — the reason this clause cannot be a
> TypeScript question.** **Four** of the 36 references — `index.html:179`,
> `compare.html:41` (both `toggleTheme`), `index.html:172`
> (`toggleSidebar`) and `index.html:195` (`closeSidebar`) — live in **HTML
> attributes**, where no compiler and
> no `tsc` pass will ever see them. A reference sweep restricted to `*.ts`
> finds nothing wrong with deleting `window.toggleTheme`; the page then
> throws `ReferenceError` on the first click. The plan already handles all
> four (§5 Step 6.2, Step 6.3, R23 — R23 *is* this class, found by a
> reviewer in iteration 1), which is the point: the clause reproduces by
> command a defect that previously needed a human to notice.
>
> **2. A repo-wide first pass over-reported, but it over-reported something
> real.** Run without the module scope, `showToast` returns **53** hits,
> because `web/certmachine/js/toast.ts:43` exports a `showToast` of its own
> — it is the **donor of record** — and four certmachine modules import it.
> Those are not references to the symbol being deleted, so they do not
> belong in the table. But running the wide form is what made the
> **duplication** visible: Phase 2 creates `web/shared/ts/toast.ts` and
> leaves the donor in place, and until v5 that was declared only as
> *future work* (§11 item 9) and not as a duplication existing during the
> phase. It is now **(b) duplication 6**. The narrow scope is therefore the
> clause, and the wide scan is worth running once per revision as a
> reviewer aid.

**What (a6) does not do.** It takes the deletion roster as given. A symbol
this plan deletes but never *says* it deletes is invisible to the clause, in
the same way (a5) cannot see a pointer that was never written. The roster is
derived from (b)'s left-hand column plus §5's explicit deletions, and that
derivation is a reading, not a command — the declared residual.

**(b) One-definition check.** Every fact this plan deletes has a named
survivor, and every deliberate duplication is declared:

| Deleted occurrence | Surviving definition |
|---|---|
| `web/obsidianoid/js/app.ts:46-52` local `THEMES` | `web/shared/ts/theme.ts:8-17` |
| `web/obsidianoid/css/themes.css` (189 lines) | `web/shared/css/themes.css:61-163` + `tokens.css:9-43` (**v5 FINAL**: `:61-164` through v5 DRAFT; `:164` is blank, rose's `}` is `:163`. Post-C1 the same span is `:63-170`) |
| `web/obsidianoid/js/app.ts:54-61` `showToast` | `web/shared/ts/toast.ts` (new) |
| `web/obsidianoid/js/app.ts:425` + `:493-494` duplicated key template | `ThemeManager`'s `storageKey()`, once |
| `web/obsidianoid/js/app.ts:433-447` panel build/toggle | `HamburgerMenu` + `.ui-theme-picker` |
| `web/todo/js/theme.js` (27 lines) | `ThemeManager` |
| `web/todo/index.html:78-85` toggle/close | `HamburgerMenu.toggle()/close()` |
| `web/todo/css/todo.css:103-144`, `:615-619` | `.ui-menu-drawer`/`.ui-menu-backdrop` + shared media query. *(**v4** — the second range read `:617-619` in v2 and v3, the media query alone; `:615` is the `/* ── Responsive ── */` heading and `:616` is blank, and `:617-619` is the only rule that heading owns, so the deletion takes `:615-619` or leaves a 616-line file ending in an empty heading. Architect A10; §5 Step 6.2 states the same range, and the `:101-102` head tidy is the precedent.)* |
| `web/todo/compare.html:41-43` duplicate toggle | same `ThemeManager`, one construction site in `shell.ts` — and the construction site is **one** across both todo pages, which is why `compare.html` is in C6a's Touches rather than deferred (§4.8, B4.12) |
| `web/todo/js/theme.js:6-9`'s `#theme-icon` class swap | `ThemeManager`'s `onChange` callback for *subsequent* swaps, and the inline bootstrap for the *initial* class (Step 6.3 item 2) |
| the focusable predicate, if copied | extracted from `modal.ts:68` into one private helper (B4.8) |
| `list-artifacts.mjs:38-42`'s private derivation | `scripts/artifact-paths.mjs` (new), imported twice (B9.4) |

**Declared duplications, each with a reason:**

1. `todo.css`'s 13 bare token names alongside the shared 17 — §5 Step 6.3
   (forced, not chosen), ledger row 6, §11 item 1.
2. `EXPECTED_ARTIFACT_COUNT`'s two values 15 and 16 — G15; the quantity
   genuinely moves during the sequence, and both values are assertions by the
   same gate reading one constant.
3. The barrel allowlist's three intermediate states — **7/6 → 8/7 → 9/9**
   (values/types), one deliberate edit per commit at
   `check-shared-barrel.mjs:25-26`, asserted by BX.2 against the gate's own
   PASS line (`:117-119`). Final surface: **18** names.
4. **The inline pre-paint bootstrap, duplicated across todo's two pages** —
   ledger row 16. This is the only duplication in the plan that is
   *unavoidable* rather than merely cheapest: `type="module"` is deferred by
   specification, so the resolution logic cannot be imported by the code that
   needs to run before first paint. It duplicates ~5 lines with
   `ThemeManager`'s own resolution and duplicates itself once more across
   `index.html` and `compare.html`. **BX.10** converts "we kept them in sync"
   into a command by asserting the two blocks are byte-identical, and — since
   **v4** — also asserts the *set* is exactly those two pages, so a third copy
   appearing anywhere under `web/` fails the criterion instead of quietly
   becoming an undeclared duplication.
5. **Table T1's second copy, in the sampler** — `web/sampler/js/main.ts:15-33`
   (`COLOR_TOKENS`, 17 entries today, 18 after C1), mirrored into the tracked
   emitted artifact `web/sampler/js/bundle.js:4`. *(New in v5, and this is a
   **pre-existing** duplication the plan inherits rather than one it creates
   — what is new is that Phase 2 now **moves** it, so it stops being
   harmless.)* The reason it exists: the sampler renders a live swatch for
   every colour token, which means it needs the token *names* as data at
   runtime, and CSS custom properties are not enumerable from
   `getComputedStyle` — so the list cannot be derived from
   `web/shared/css/themes.css` in the browser. `main.ts:11-14` declares the
   duplication in place and states which table it copies. Why it is only now
   a risk: nothing in `check-shared-css.mjs` reads the sampler's TypeScript,
   so before Step 1.5 the copy never had to change and the drift could not
   occur; C1 changes the original, and a one-sided landing yields a green
   `make check` over a specimen page that omits the very token it was built
   to display. **B10.6** converts the agreement into two commands — an
   anchored count on each side and a set-equality over the two name sets —
   and the second of those was demonstrated to fail on a seeded one-sided
   tree.

   > **The `bundle.js` half is not a third definition — but it is not
   > gate-covered either, and v5 had to check. (v5.)** `bundle.js:4-22` is
   > `main.ts:15-33` compiled, so it holds no independent decision. The
   > tempting conclusion is that B10.6 need not grep it, because
   > `make web-verify` would catch a stale emit. **It would not.**
   > `web-verify` is `node scripts/gates/artifacts.mjs` (`Makefile:57-58`),
   > with **no `--build` flag** — and that script's rebuild step is
   > explicitly optional (`scripts/gates/artifacts.mjs:105`, "optional
   > rebuild", gated on `--build`). What it asserts without the flag is that
   > every one of the `EXPECTED_ARTIFACT_COUNT` paths is **tracked** (`:97`)
   > and **committed-clean** (`git diff --exit-code`, `:118`). A `bundle.js`
   > left at 17 entries and committed alongside an 18-entry `main.ts` is
   > tracked, clean, and green. So B10.6's third and fourth greps are
   > load-bearing, and the coverage claim for emitted artifacts in this plan
   > is "committed-clean, not provably rebuilt." *(Adding `--build` to the
   > recipe is a one-line change that would close the class — but the
   > recipe is a `Makefile` edit, and G16 confines those to C1's enumerated
   > set. Recorded as a Phase-3 follow-up in §11 rather than smuggled in.)*
6. **The Toast surface, in two places, for the duration of the phase** —
   `web/shared/ts/toast.ts` (new, §5 Step 2) and `web/certmachine/js/toast.ts`
   (81 lines, the **donor of record**, FRD `:319`, ADR-010). *(New in v5,
   surfaced by (a6).)* ADR-010 lifts the donor's surface **verbatim**, which
   is what makes the copy safe — but it also means that from C2 onward two
   files export a `showToast` of the same name and signature, and four
   certmachine modules keep importing the local one (`detail.ts:16`,
   `generate.ts:16`, `ui.ts:9`, `wizard.ts:16`, all `from "./toast"`).
   **Phase 2 does not collapse it**, because certmachine appears in no commit
   manifest in §6 (§1). §11 item 9 already carries the follow-up — "a
   two-line shim over `web/certmachine/js/toast.ts`, exactly as taskmaster's
   modal became one" — so the *work* was declared; what was missing until v5
   was the declaration that a **duplication exists in the meantime**.
   Recorded here so (b) is honest rather than silent: the phase ends with the
   Toast surface defined twice, deliberately, with the collapse named and
   deferred. *The end state is not hypothetical —
   `web/taskmaster/js/ui/modal.ts` is today the **two-line** re-export shim
   this item's Phase-3 form describes — a value re-export and a type
   re-export, both `from "@shared/modal"`, and `wc -l` on it returns `2`,
   which is the same number §11 item 9 uses for the Toast shim it proposes.*

**(c) Artifact count, and gate input composition — reconciled in v2.** Two
different trajectories were stated inconsistently in v1 (§6 said the gate
inspects "three, then four" bundles; this clause said 2 → 4 → 5). The clause
was right; §6 was wrong, and §6 is corrected. *(**v4** — the third column was
labelled "`bundle-shape.mjs` inputs" through v2 and v3 while holding artifact
counts; B9.1 calls the same quantity "Outputs inspected" and the gate's own
PASS line says `N sharedConsumer bundle(s) inspected`. Relabelled to match, so
the two places naming this column agree. Architect A8 — benign, since the cell
text disambiguated, but it is the same one-definition rule.)*

| Boundary | `EXPECTED_ARTIFACT_COUNT` | Outputs inspected by `bundle-shape.mjs` | `--require=` in `Makefile:62` |
|---|---|---|---|
| C1 | 15 | **2** — sampler, taskmaster | `sampler,taskmaster` |
| C2–C4 | 15 | 2 | `sampler,taskmaster` |
| C5 | 15 | **4** — + obsidianoid ×2 | `+obsidianoid` |
| C6a | **16** | **5** — + todo `shell.js` | `+todo` |
| C6b–C8 | 16 | 5 | unchanged |

`EXPECTED_ARTIFACT_COUNT` moves exactly once, at C6a
(`scripts/descriptors.mjs:162`) — G15. The **composition** also shifts at C5
without the count moving: obsidianoid's two outputs change from non-module
transpiled `.js` to ESM bundles (ADR-011), which is why C5 needs B9.1's
`--require` entry even though the artifact total is still 15. The input count
grows 2 → 4 (not 3) at C5 because obsidianoid has **two** entries, which is
the same fact that forces `artifact-paths.mjs` to exist.

**(d) Gate-census check.** §7 states the executed/deferred split once, by
number, and those numbers are **derived by (a2) above** rather than
maintained here — so this clause no longer restates them. What it asserts is
the *shape*: every `[deferred]` criterion names its boundary and states its
substitute evidence or explicitly has none; no criterion is phrased as a
property with no command behind it; and every gate names a script or a shell
command plus its expected exit status. Two v2 corrections belong to this
clause specifically:

- **A gate must be invoked as the plan describes it (G16).** Three criteria
  in v1 relied on `bundle-shape.mjs --expect-nonempty`, a flag `Makefile:62`
  never passed — and one that could not have failed if it had, since it only
  checks the set is non-empty and `taskmaster` alone satisfies that. C1 adds
  `--require=<name>,…`, the `Makefile` recipe is edited at C1/C5/C6a to name
  the boundaries that exist, and **BX.8** proves the flag by making it fail.
- **A guarded `grep` must not launder its exit code.** `! grep …` maps
  grep's exit **2** (a usage or read error) onto shell success, and
  `for … done; rc=$?` reads only the final iteration. B4.8 and Step 7.3 are
  rewritten with an explicit accumulator and an `rc -eq 1` guard, so a
  malformed pattern fails the criterion instead of passing it.

**(d2) Label ⇄ schedule check — new in v3, and it is the enabling-gap fix
for Critic C-1.** Everything above audits what a criterion *says*. Nothing
audited **when it runs**, and that is precisely how v2 shipped 19
criteria that no commit boundary scheduled and 4 scheduled at a boundary
their label did not name (§6's v3 note has the enumeration). A criterion that
never runs is indistinguishable, at review time, from a criterion that does —
which makes this the same defect class as G16's never-passed flag, and the
reason a *shape* clause could not catch it: the shape was perfect. So (d2)
compares the three places the same fact is written — §7's label, §6's "Gates
at this boundary" cell, §5's `**Acceptance**` line — and requires **set
equality in both directions at all nine boundaries**. Subset in either
direction fails:

```sh
python3 - <<'PY'
import re, io, collections
s = io.open('docs/PLAN-ui-unification-phase2.md', encoding='utf-8').read()
L = s.split('\n'); B9 = ['C1','C2','C3','C4','C5','C6a','C6b','C7','C8']

def bounds(t):
    t = t.replace('…', '...')
    return list(B9) if 'C1...C8' in t else re.findall(r'C\d[ab]?', t)

def crits(t):                      # expands "B5.1-B5.7" and strips markup
    t = re.sub(r'\*\*|`', '', t); out = []
    for tok in t.split(','):
        tok = tok.strip().rstrip('.').strip()
        m = re.fullmatch(r'(B(?:\d+|X))\.(\d+)\s*[-–—]\s*B(?:\d+|X)\.(\d+)', tok)
        if m: out += [f'{m.group(1)}.{i}' for i in range(int(m.group(2)), int(m.group(3))+1)]
        elif re.fullmatch(r'B(?:\d+|X)\.\d+', tok): out.append(tok)
    return out

labels, i = {}, 0                  # §7 bullets: the DEFINITION
while i < len(L):
    m = re.match(r'- \*\*(B(?:\d+|X)\.\d+) —', L[i])
    if not m: i += 1; continue
    j = i + 1
    while j < len(L) and not re.match(r'- \*\*B(?:\d+|X)\.\d+ —|#{2,4} ', L[j]): j += 1
    lm = re.search(r'\[(?:deferred|re-asserted) → ([^\]]+)\]', '\n'.join(L[i:j]))
    labels[m.group(1)] = bounds(lm.group(1)) if lm else []
    i = j

cells = {}                         # §6 table: derivation 1
for ln in L:
    m = re.match(r'\| \*\*(C\d[ab]?)\*\* \|', ln)
    if m:
        g = [c.strip() for c in ln.strip().strip('|').split('|')][3]
        cells[m.group(1)] = crits(g.split(';', 1)[1] if ';' in g else g)

acc = {}                           # §5 Acceptance lines: derivation 2
for k, ln in enumerate(L):
    m = re.match(r'\*\*Acceptance(?: \((C\d[ab]?)\)| — (C\d[ab]?))?:?\*\*(.*)', ln)
    if not m or not (m.group(1) or m.group(2)): continue
    body, n = m.group(3), k + 1
    while n < len(L) and L[n].strip() and not L[n].startswith(('**', '>', '#', '|')):
        body += ' ' + L[n]; n += 1
    acc[m.group(1) or m.group(2)] = crits(body.split('Artifact count')[0].split('*(')[0])

lab = collections.defaultdict(set)
for c, bs in labels.items():
    for b in bs: lab[b].add(c)
print('criteria:', len(labels), '| unlabelled:',
      sorted(c for c, v in labels.items() if not v) or 'none')
fail = pairs = 0
for b in B9:
    Lb, S6, S5 = lab[b], set(cells.get(b, [])), set(acc.get(b, []))
    pairs += len(Lb)               # the prose figure below, derived not typed
    d = [sorted(Lb-S6), sorted(S6-Lb), sorted(Lb-S5), sorted(S5-Lb)]
    ok = not any(d); fail += (not ok)
    print(f'{b:4s} label={len(Lb):2d} §6={len(S6):2d} §5={len(S5):2d} '
          + ('OK' if ok else f'FAIL {d}'))
print('(d2):', 'PASS' if not fail else f'FAIL at {fail} boundary/boundaries')
print('pairs:', pairs)
PY
```

**Result, run against this revision:**

```
criteria: 82 | unlabelled: none
C1   label=11 §6=11 §5=11 OK
C2   label=17 §6=17 §5=17 OK
C3   label=17 §6=17 §5=17 OK
C4   label=20 §6=20 §5=20 OK
C5   label=26 §6=26 §5=26 OK
C6a  label=19 §6=19 §5=19 OK
C6b  label= 8 §6= 8 §5= 8 OK
C7   label= 8 §6= 8 §5= 8 OK
C8   label=10 §6=10 §5=10 OK
(d2): PASS
pairs: 136
```

**136 (criterion, boundary) pairs across 82 criteria.** *(v3 and v4 published
131 across 77. The five criteria v5 adds — B2.10, B2.11, B2.12, B4.13, B10.6
— account for the whole delta: C1 +1 (B10.6), C5 +3 (B2.10–B2.12), C6b +1
(B4.13).)*

> **This clause failed once during the v5 pass, and the failure was real.**
> After **B4.13** was authored it reached §7's label and §6's C6b cell but not
> §5's `**Acceptance — C6b:**` line, and (d2) reported
> `C6b label= 8 §6= 8 §5= 7 FAIL [[], [], ['B4.13'], []]` — the third
> position, meaning §5 is the side that is short. One line was added and the
> re-run is the output above. Worth recording for two reasons: it is the
> defect class (d2) was written for, reproduced by the clause on its author
> rather than on a reviewer's tree; and it shows why the three-way comparison
> is not redundant — a two-way check against §6 alone would have been green,
> because §6 was one of the two places that were already right.

Set equality at every
boundary implies the weaker property v2 violated outright — **no labelled
criterion is scheduled at none of its boundaries** — which is the check
B1.3 and B10.5 failed.

Three notes on what (d2) deliberately does **not** do. It does not invent a
schedule: §7's label is the single definition, §6 and §5 are derivations, and
a disagreement fails rather than being auto-reconciled — which is what keeps
widening a label an explicit, reviewable decision (B9.4 and BX.5 were widened
in v3 *with* stated reasons, not silently). It does not check that a
criterion is *satisfiable* at the boundary it names; that is (d)'s shape
clause and the §6 ordering argument, and the two C6a removals in v3 (B3.9,
B9.3 — both obsidianoid-only) were judgements of that kind, not of this one.
And it does not read the three `C1…C8` criteria as a special case: the label
expands to all nine boundaries and every cell must carry them, which is why
seven cells grew.

**(e) Scope check — and since v5 the allowlist is *derived* from §6 rather
than restated beside it.** `git diff --name-only` across the whole phase must
touch only: `web/shared/`, `web/sampler/`, `web/obsidianoid/`, `web/todo/`,
`scripts/`, **`Makefile`**, `tsconfig.json`, `internal/obsidianoid/`,
`internal/platform/auth/gate.go`, `cmd/server/dispatcher_auth_test.go`,
`unified-webapp.json`, `unified-webapp-example.json`, `local-test/config.json`,
`README.md`, `docs/`. **Nothing under `tools/baseline-shots/` or matching
`docs/INVENTORY-*` may appear in any commit** — BX.4, per commit, not per
phase.

The allowlist above is not an independent statement of scope; §6's Touches
cells are, and this clause checks that the two agree in the direction that
matters — every path any commit claims it will touch falls inside the list:

```sh
python3 - <<'PY'
import re, io
s = io.open('docs/PLAN-ui-unification-phase2.md', encoding='utf-8').read()
sec = re.search(r'^## §6 —.*?(?=^## §7 —)', s, re.S | re.M).group(0)
rows = re.findall(r'^\| \*\*(C\d[ab]?)\*\* \|([^|]*)\|([^|]*)\|', sec, re.M)
ALLOW = ['web/shared/', 'web/sampler/', 'web/obsidianoid/', 'web/todo/',
         'scripts/', 'Makefile', 'tsconfig.json', 'internal/obsidianoid/',
         'internal/platform/auth/gate.go', 'cmd/server/dispatcher_auth_test.go',
         'unified-webapp.json', 'unified-webapp-example.json',
         'local-test/config.json', 'README.md', 'docs/']
def expand(tok):                      # `web/shared/ts/{toast,index}.ts` -> two paths
    m = re.match(r'^(.*)\{([^}]*)\}(.*)$', tok)
    return [m.group(1) + p.strip() + m.group(3) for p in m.group(2).split(',')] \
           if m else [tok]
pairs, bad = set(), set()
for c, _title, touches in rows:
    for tok in re.findall(r'`([^`]+)`', touches):
        for p in (x.strip() for x in expand(tok)):
            # Path-shaped tokens only. This drops prose fragments and quoted
            # source literals such as C5's `"dark"`, which is a string in the
            # Go package, not a file. Getting this filter wrong in the lax
            # direction is safe (a non-path is reported as out-of-allowlist
            # and inspected); getting it wrong in the strict direction would
            # hide a real path, so the pattern admits every character that
            # appears in a repo path and nothing else.
            if not re.fullmatch(r'[A-Za-z0-9_./*-]+', p or ''):
                continue
            pairs.add((c, p))
            if not any(p == a or p.startswith(a) for a in ALLOW):
                bad.add((c, p))
print('path tokens in Touches cells:', len(pairs))
print('outside the allowlist       :', sorted(bad) or 'none')
PY
```

**Result at this revision: 79 path tokens across the nine cells, 0 outside
the allowlist.**

> **`Makefile` was missing from this list for four revisions, and it is in
> three Touches cells. (v5, found by running the clause above rather than
> reading the list.)** G16 — a gate flag the plan relies on must live in the
> recipe — is what puts `Makefile` in C1, C5 and C6a (§1 G16; §5 Step 1.4;
> §6's note at the head of the table), and (e) simply never gained the entry.
> A *correct* implementation of C1 would therefore have contradicted this
> clause on its first commit, which is the same defect shape as a phantom
> citation with the sign reversed: not a claim about a file that does not
> exist, but a scope statement that excludes a file the plan requires. The
> fix is the entry **and** the derivation, because a hand-kept second copy of
> §6's path set was always going to drift — that is the inherited rule *one
> definition per fact* applied to this clause's own body.
>
> **The other half of the finding is that this clause named the wrong
> assertor.** v1–v4 read "Asserted by BX.5". **BX.5's command is scoped
> `-- web/`** and filters to the four in-scope module prefixes, so what it
> actually asserts is the narrower property *no module outside the three
> adopters plus `shared/` changes* — it is silent about `scripts/`,
> `Makefile`, `tsconfig.json`, the Go files and the JSON configs, and it
> could not have caught the omission above. The honest attribution:
> **BX.5** covers the `web/` prefixes, **BX.4** covers the two forbidden
> side-car classes per commit, and the remainder of this allowlist is
> covered by the check in this clause plus §6's cells — plan-time, not
> commit-time. Recorded rather than papered over: widening BX.5 to the whole
> tree would be a better gate, but it would also make every `docs/` edit in
> the phase a diff BX.5 has to allowlist, and that trade belongs to a
> reviewer, not to a synthesis pass. Carried as **§11 item 16**.

---

## §14 — v2: iteration-1 synthesis

A fix-by-fix ledger. Every Architect blocker, medium and minor and every
Critic critical, major and minor is listed with **what changed** or, where the
reviewer was factually wrong, **the rebuttal and the tree evidence for it**.
Nothing is silently dropped, and nothing is accepted on a reviewer's word: the
rule for this iteration was to re-verify each finding in the tree first, which
is how four of them turned out to need rebuttals and how two of my *own*
earlier fixes turned out to be wrong and were withdrawn in-document.

### Architect — blockers

| | Finding | Disposition |
|---|---|---|
| **B1** | `compare.html` jointly unsatisfiable at C6 | **Fixed, and it drove the largest structural change in v2.** §4.8 inventories the page; **ADR-016** splits C6 into C6a/C6b; `compare.html` is in C6a's Touches and **only** C6a's; §6 is a 9-commit table; **B4.12** asserts the toggle works and no undefined global survives; ledger row 19 records why the page is migrated despite FR-4 giving it nothing; **R23** carries the risk |
| **B2** | deleting obsidianoid's `themes.css` collapses two distinct shadow values, and §8.5's pre-check structurally cannot catch it | **Fixed on both halves.** The pre-check is now **Step 5.0**, a numbered sub-step that runs *before any edit in C5*, compares **every declaration** in all **five** blocks (not v1's names-only 4×17), and stops C5 outright on a non-zero result — **B2.7**, executed at plan time with 0 mismatches. The shadow finding itself is widened in **Q11**: the four non-`dark` donors carry `--shadow-md` at `/0.45`, not `/0.4`, so the delta is non-uniform across the five themes — a fact v1 did not state |
| **B3** | `--expect-nonempty` requires a `Makefile` edit no commit manifest contains | **Fixed, and generalised into a guardrail.** The flag was proven a no-op by running the gate. **Step 1.4** adds `--require=<name>,…`; `Makefile:62` is wired at **C1** (`sampler,taskmaster`), **C5** (`+obsidianoid`) and **C6a** (`+todo`), each in that boundary's Touches (ledger row 17); **BX.8** proves the flag by making it fail; **G16** states the principle — "a gate not invoked with the flag the plan cites is not the gate the plan describes"; **R24** carries the class |
| **B4** | C6's pixel acceptance invokes a todo D-table that does not exist | **Fixed.** §5 Step 6.2a now carries the table, **D-8…D-14**, and D-15 is filed under obsidianoid with an explicit numbering note (v1's §0 mis-cited it as "D-12", which is todo's drawer `box-shadow`) |
| **B5** | `#theme-toggle`'s inline `onclick` survives C6 into ES-module scope | **Fixed.** Step 6.3 now names **three** sub-edits per page where v1 named one: remove the `onclick` attribute, move the icon swap, replace the `<script src>`. **B4.12** asserts zero `onclick="toggleTheme()"` on both pages and zero `toggleTheme` definition under `web/todo/` |

### Architect — mediums

| | Finding | Disposition |
|---|---|---|
| **M1** | B9.4's extraction range excludes the symbol it tests | **Fixed.** The range is `:25-27` **+** `:38-45`, not `:31-46`: `jsName` is defined at `:26`, outside the mode dispatch, so lifting the dispatch alone yields an undefined reference. Stated in Step 5.1 and in ADR-011's amendment |
| **M2** | phantom path, and §13(a)'s grep is structurally blind to its form | **Fixed, and the clause was worse than reported.** v1's regex had an **ordering bug** — `js` precedes `json` in the alternation, so every `.json` citation was truncated to a `.js` path and reported as a phantom. §13(a) is rewritten with a longest-first alternation, a trailing boundary, and *classification* rather than bare existence-testing. Re-run against v2 **as the document now writes it**: 62 paths, 51 existing, 9 declared-new, 2 **counterfactual**, **0** phantom. The counterfactual category is new and named (`COUNTERFACTUAL` in the script): ADR-011's `app.js/app.js` and v1's own `build_test.go`, both cited in order to be refuted. The clause was also re-scoped to audit **§14 as well** and to exclude §13 itself — its first v2 run reported 2 hits for the retired-name check, and both were this clause quoting its own search pattern |
| **M3** | undeclared fourth duplication: the pre-paint bootstrap | **Fixed.** Ledger row 16, §13(b) declared-duplication item 4, and **BX.10** — a new criterion asserting the two blocks are byte-identical. Also corrected along the way: the set is **todo's two pages**, not three — the sampler has no persisted choice and `themes.css:15`'s `:root` half already defaults it, and obsidianoid's stamp is already pre-paint via a classic end-of-body tag *(**Historical, and left as written.** v5 FINAL, Critic minor 7: that last clause is the premise iteration-4's **F-C / §17 row 5** found **false in the tree** — obsidianoid has no end-of-body theme tag; `web/obsidianoid/index.html:2` carries a **static** `data-theme` attribute, which is why the module needs no bootstrap. The row's *conclusion* — the copy set is todo's two pages, not three — was and is correct. This row records what the iteration-2 pass decided; §14, §15 and §16 are not rewritten when later findings move the facts they record. The live statement of the exclusion and its real grounds is **§17 row 5**, which owns it.)* |
| **M4** | orphaned obsidianoid markup and element handles | **Fixed.** Step 5.2 deletes markup and handles as **one edit**, naming `index.html:60-64` and `:102` against `toastEl:31`, `themePanel:41`, `themeOptions:42`, and recording that `btnHamburger:40` **survives**. **BX.9** generalises it cross-cutting, because C6a creates the same hazard in todo. The reason it needs a criterion at all is that all four handles use **non-null assertions**, so `tsc --noEmit` stays green and the failure is runtime-only — **R25** |
| **M5** | `ThemeManager` has no re-resolution entry point for the vault switch | **Fixed.** `reresolve()` is added in Step 5.2 and specified in **ADR-008's v2 amendment**; **B3.10** asserts both halves — it follows a changed `storageKey()`, and it does **not write** storage (or switching vaults would overwrite the destination's saved choice) |
| **M6** | G15's single-line edit leaves the adjacent comment lying | **Fixed.** G15 is now a **two-line** edit: the constant at `descriptors.mjs:162` and the value-bearing line of the trajectory comment that narrates it. *(v2 cited that comment as `:157-158`; the comment is `:157-161` and the numbers are on `:160`. Corrected in v3 — §15, M-1.)* |
| **M7** | §4.3's property census is wrong in three ways | **REBUTTED, with arithmetic.** `web/obsidianoid/css/themes.css` has **190** declarations across **5** blocks = **38 per block**, and 170 content lines / 5 = 34 *lines* per block. The reviewer's "34" is the line count, not the declaration count. 38 = 17 colour + 21 structural. The census stands; §4.3 now shows the derivation so the next reader does not have to redo it |
| **M8** | obsidianoid's popover→drawer transformation is absent from D-1..D-7 | **Fixed.** **D-5** records that the theme picker becomes reachable in Threads mode, and Step 5.3a item 2 explains *why* it is a fix delivered by placement — the trigger mounts outside `#topbar-actions`, so `app.css:463` no longer hides it |
| **M9** | P-III's bundle-shape framing and C1's gate-composition note are both wrong | **Fixed.** See B3 above for the flag; §13(c) is now a per-boundary table reconciling artifact count, gate input count (**2 → 4 → 5**) and `--require=` contents, and §6's contradicting "three, then four" is corrected. The input count grows 2 → **4** at C5 because obsidianoid has two entries — the same fact that forces `artifact-paths.mjs` to exist |

### Architect — minors, and the two flagged decisions

| | Finding | Disposition |
|---|---|---|
| FRD ±1 drift ×5 | quick-toggle `:300` not `:301`; topbar-actions `:301` not `:302`; Item kinds `:289-293` not `:288-292`; FOUC `:249`; §6's `:293` | **All five verified in `docs/FRD-ui-unification.md` and corrected** (the FOUC cite was already `:249`; §6's now reads `:291-293`, the render-slot sentence, rather than landing on the bare word "verbatim") |
| `panels.css:57-89` understated | — | **Fixed → `:57-101`.** `#rightToggle` starts at `:57`; the `.hamburger-icon.open` `nth-child` rules run to `:101`. v1's range cut three rules off |
| `panels.js:39-53` mis-cited | — | **Fixed.** The click listener is `:20-22`; it calls `toggleRightPanel()` at `:39-53`, whose icon-class toggle is `:42`. Both citations now appear |
| `jquery-ui.js` 520,714 bytes "unverified" | — | **Verified:** `wc -c` = **520714**. §11 item 5 now says so explicitly |
| **4a** — swatch colour source | — | **Answered: option 4D, ADR-015.** See "the swatch resolution" below |
| **4b** — `window.showToast` from `shell.ts`? | — | **Answered NO**, as directed. No `window.*` surface is added. If legacy todo JS ever needs one it is a single deliberate façade (`window.todoShell`) applied to all legacy call sites at once, or nothing — never one incidental global |

### Critic — critical and majors

| | Finding | Disposition |
|---|---|---|
| **C-1** | Step 3.2's recommended swatch option is gate-infeasible **and** cannot render 8 distinct swatches; B4.2 cannot detect either failure | **Fixed by choosing a fifth option, ADR-015/4D**, and by rewriting **B4.2** to assert **8 distinct computed backgrounds** (`getComputedStyle`, `Set(...).size === 8`) rather than inspecting markup. The Critic's own proposed reshape (`.ui-theme-btn[data-theme-name] .ui-theme-swatch`) was **rejected with evidence** — it is a descendant selector whose top-level form still starts `.ui-`, so it survives clause 7, but it re-introduces the per-theme rule set that clause 7's `:314` loop rejects in the 8-block form, and more importantly it does not solve the *rendering* problem 4D solves for free |
| **M-1** | `compare.html` on both horns of an unresolved fork | **Fixed** — see Architect B1. Additionally: `shell.ts`'s `HamburgerMenu` construction is guarded on `#menu-toggle` being present, and **BX.9** asserts no TS handle names an id `compare.html` lacks (`#menu-toggle`, `#sidebar`, `#sidebar-backdrop` are all absent there) |
| **M-2** | B1.1's gate cannot fail | **Fixed.** **B1.1** is ranged `b73d31c..HEAD` instead of comparing the tree to itself, and **B1.4** is added |
| **M-3** | `--expect-nonempty` never wired, and cannot fail for the reason B9.1 needs | **Fixed** — see Architect B3. **Partially rebutted:** the Critic's remedy included "fix obsidianoid's descriptor `out` to a file path", which is **mechanically impossible** — `list-artifacts.mjs:38-45`'s transpile arm is `path.posix.join(d.out, jsName(entry))`, so a file-valued `out` yields `web/obsidianoid/js/app.js/app.js`, and obsidianoid has **two** entries that one file path cannot name. The gate is the side that must iterate; ADR-011's amendment records this |
| **M-4** | §6's gate-composition table contradicts §13(c) | **Fixed; §13(c) was right and §6 was wrong.** Both now read **2 → 4 → 5** from one per-boundary table |
| **M-5** | clause 7 fails open inside at-rules; C4 is the first commit to open that hole | **Fixed** — opt-in at-rule descent for clause 7, ADR-012's v2 amendment |
| **M-6** | the clause-7 re-key has no negative test | **Fixed.** A negative probe injects a non-`.ui-` **selector** carrying a colourless declaration inside an `@media` block, confirms clause 7 now fails naming the inner selector, and reverts (B1.4 item 1). Also fixed here: the clause count string moves in **two** places (`:426` and the header at `:2`), where v1 named only the PASS line. *(v2's row described the probe as a **colour literal** — wrong, and it would have proved nothing, since the colour scan is a whole-file per-line scan that already catches that case. Corrected in v3; §15, N6.)* |
| **M-7** | Step 7.3's deletion proof validates only the last of 40 files | **Fixed, and a second defect was found in the same command.** `done; rc=$?` reads only the final iteration — now a `hits` accumulator with an `rc -eq 1` guard so a malformed pattern fails instead of passing. The second defect: **basename matching produces phantom references** (`utils.js` is a substring of the live `todo-utils.js`; `nav.css` of `responsivenav.css`), which would have *blocked a correct deletion*. The command now matches the last **two** path segments |
| **M-8** | Q4 misattributed as the rename-coupling authority | **Fixed.** §0 states the authority split explicitly (rename decision = FRD `:191`; coupling *principle* = Q4's user ruling, which adjudicates `tsconfig`), and the three loose "Q4 settled" phrasings in §5, §6 and ADR-014 are tightened. **P6** additionally records Q3's asymmetry: its checkbox is unticked while its body states the values as settled by measurement |
| **M-9** | §13(a) under-reports by 2× and misses a real phantom | **Fixed** — see Architect M2 for the regex. The `build_test.go` phantom is real and worse than reported: **there is no `internal/obsidianoid/build_test.go` at all.** Step 5.4 now runs the command v1 deferred and records its exact output — **two** `"dark"` occurrences in the package, `build.go:41` (the default, must change) and `handler_test.go:40` (fixture data, still passes unchanged, changed anyway as declared hygiene) — and **B2.5** is widened from `build.go` to the whole package so the criterion matches the edit |
| **M-10** | obsidianoid's theme-block census is wrong (38/21 claimed, 34/17 actual) | **REBUTTED** — see Architect M7. 190/5 = 38 declarations; 170/5 = 34 lines. Both reviewers reached the same wrong number from the same line count, which is why the derivation is now shown |
| **M-11** | B3.1's fourth resolution case is unreachable as written | **Fixed, and the underlying problem is bigger than the criterion.** `scripts/test-web.mjs:51-59` bundles `platform:"node"`/`format:"cjs"` and pipes to a bare `node`: **the harness has no DOM at all**, and `modal.test.ts`'s stub is a five-member object literal (`:47-52`) with no `documentElement`, no `localStorage` and no `matchMedia` — disjoint from everything `ThemeManager` touches. C3 therefore adds `web/shared/ts/test-dom.ts` with `installFakeDom()` (documentElement + dataset, Map-backed `localStorage`, `matchMedia` with `fireChange`). It is barrel-neutral and type-checked by `tsconfig.json:15`. `modal.test.ts` is deliberately **not** retrofitted (§11 item 14) |
| **M-12** | B4.8's verification command is not executable as written | **Fixed.** The guarded-`grep` form is corrected: `! grep …` maps grep's exit **2** onto shell success, so a malformed pattern would pass. Now explicit about rc |
| **M-13** | obsidianoid gains real webfonts at C5; the only declared font delta describes something else | **Fixed.** **Ledger row 13** is the wholesale typography change (the `shared.css` link brings 15 `@font-face` rules into a page that has none, reflowing every glyph); **D-2** stays as the narrower fallback-chain delta; **P5a** re-captures obsidianoid's pixel baseline *inside* C5 after the link lands, because no pre-C5 shot is comparable to a post-C5 render. *(An earlier v2 draft of row 13 claimed the opposite — that obsidianoid adds no shared `<link>` — which contradicted Step 5.3. Caught and replaced.)* |
| **M-14** | Scenario 1's prescribed pre-check is never wired into any step or criterion | **Fixed** — see Architect B2. It is **Step 5.0** with criterion **B2.7** |

### Critic — minors

| | Finding | Disposition |
|---|---|---|
| 1 | §7's census says 20 substitutes; tree count is 21 | **Accepted, and then superseded by deriving all four numbers.** The census is now the output of §13(a2), which reports **75 / 6 executed / 69 deferred / 26 substitutes** for v2. Two earlier v2 drafts published 74 / 6 / 68 / 28 and then 25 substitutes, both by hand-arithmetic; the command disagreed both times and the command won both times. v1's own figure is no longer checkable — the file is untracked and was revised in place — so §7 now states the derived number and asserts no delta against a document that no longer exists |
| 2 | the gate's clause count lives in two strings | **Fixed** — Step 1.3 now moves both `:426` and the header at `:2` |
| 3 | `responsivenav.css` is linked from `menuserver.html:16` | **Accepted and verified.** Ledger row 2 now carries the link, and notes that all four files (`responsivenav.css`, `menuserver.js`, `navcontrols.js`, `menuserver.html`) are in C7's 40-file deletion set, so nothing dangles |
| 4 | `app.css:417`'s `:root { --sidebar-width: 200px; }` co-exists with shared `280px` | **Written down** — Step 5.3a item 1. Verified as the file's **only** custom-property declaration; `shared.css` links before `app.css`, so app.css wins inside its media query and shared wins outside, identical to today. G12 is not engaged: redeclaring an existing token's value in module CSS is not declaring a new token |
| 5 | `clean-tree.mjs` is wired into no `make` target | **Accepted, verified, and answered rather than patched.** `grep -rn clean-tree` finds no invocation anywhere outside the script itself. It **cannot** be a `make gates` line — it requires path arguments (`:53` fails without them) and the paths differ per commit. It stays operator-invoked; what changed is that the plan no longer *relies* on it, because **BX.4** asserts the same property from the other side by inspecting what each commit actually contains. Recorded on **P2** |
| 6 | `dialog::backdrop` and the chevron data URI are untokenised colours absent from the ledger | **Both accounted for** — Step 5.3a item 3. The chevron does **not** survive: `:740`'s `stroke='%237878a0'` is removed by the `mask-image` rewrite (**D-7**). `dialog::backdrop` survives deliberately (ledger row 7, §11 item 8). After C5 exactly **one** out-of-vocabulary colour remains, documented |
| 7 | `:463` interacts with the C4 trigger's placement | **Written down** — Step 5.3a item 2; the trigger mounts outside `#topbar-actions`, which is what makes **D-5** true |
| 8 | ADR-011's `transpile` + `bundle` shape deserves a legality note | **Added.** `mode` is this repo's descriptor vocabulary, not esbuild's; esbuild bundles each entry independently and writes one output per entry, so the combination is ordinary usage and `EXPECTED_ARTIFACT_COUNT` (`descriptors.mjs:162`) does not move at C5 |
| 9 | `theme.js:15`'s `matchMedia` polarity is inverted relative to the shared manager | **Decided, not deferred.** §4.4 now records that the two agree whenever the OS expresses a preference and differ only under **`no-preference`** (todo → `dark`, shared → `light`). The plan **adopts the shared polarity**: the alternative is a per-module override of the one resolution rule, to protect a first-visit-only, one-click difference. FRD `:250-251` fixes the resolution *order* and is silent on polarity, so this is the plan's call and is labelled as such rather than quoted |

### Rebuttals, collected

Five findings were checked in the tree and **not** adopted. Each is recorded
where it matters, not only here.

1. **obsidianoid's 38 declarations per theme block** (Architect M7, Critic
   M-10). 190 declarations / 5 blocks = 38; the reviewers' 34 is 170 content
   lines / 5. Derivation now shown in §4.3.
2. **obsidianoid's descriptor `out` must stay a directory** (Critic M-3's
   remedy). `list-artifacts.mjs:38-45` joins `out` with a per-entry basename,
   and the module has two entries. ADR-011 amendment.
3. **`parseBlocks` descent must be opt-in — but not for v1's reason.** The
   reviewers were right that descent is needed; v1's *rationale* was false and
   is **withdrawn in-document**. `fonts.css` is never passed to
   `parseBlocks` (three callers: `:236`, `:252`, `:310`) and clause 10 counts
   faces by raw `text.match` at `:364`. The real reason is clauses 3/4/5, each
   a set-equality assertion over a file modelled as flat. Honest caveat also
   recorded: on today's tree global descent would pass, since
   `grep -c '@'` is 0 for both files — so this is future-proofing, not
   bug-avoidance.
4. **Q3 does not block C5.** Its values are settled by measurement in its own
   body; only its checkbox is unticked. Recorded on P6 rather than treated as
   a blocker.
5. **B4.10's two-theme picker is forced, not stylistic.** `todo.css` declares
   its 13 names in exactly two blocks with **74** `var(--…)` consumers, so a
   third theme name unstyles the page. This retires **Q10** as a user
   question entirely — the tree answers it.

### Corrections to my own v2 work, made before publishing

Recorded because a synthesis pass that only lists the reviewers' errors is not
an honest one.

1. The **fonts.css rebuttal was false** and is withdrawn in both places it
   appeared (item 3 above).
2. **B2.8 was drafted wrong twice** (3 occurrences then 2, both remapping
   `--color-surface-offset` onto `surface-2`). The tree shows
   `--color-surface-offset` is `#252535`, byte-identical to shared
   `--color-surface-3`, so §4.3's existing pair-shift was already correct.
   B2.8 now defers the mapping to §4.3 and owns only the proof.
3. **Two criterion-id collisions** (BX.7 and B4.11) were created and resolved;
   the orphan-handle check became cross-cutting **BX.9**, which is stronger
   than the obsidianoid-local form it replaced.
4. **A D-row collision** — §0 cited "D-12" for the swatch consequence, which
   is todo's drawer `box-shadow`. It is **D-15**, with a numbering note.
5. **An over-correction was reverted:** `#theme-panel` really is
   `index.html:61-64`, not `:60-63`.
6. **v1's §7 total of 65 was right**; only its substitute count was wrong. An
   earlier v2 draft claimed the total was also wrong.
7. **Ledger row 13's first draft contradicted Step 5.3** (claiming obsidianoid
   adds no shared `<link>`) and was replaced with the true typography
   deviation.
8. **B10.5 was nearly "fixed" into a defect.** It asserts the sampler carries
   no `data-theme`, and I initially read that as a latent bug — `tokens.css`
   declares **zero** `--color-*` names, so an unstamped page has no colour
   fallback. Then `web/shared/css/themes.css:15` turned out to be the
   two-selector list `:root, [data-theme="dark"]`, so an unstamped document
   resolves the full `dark` palette and B10.5 is correct as written. What did
   change is **B3.8**, which wrongly included the sampler in the pre-paint
   set; the sampler has no persisted choice to restore
   (`main.ts:77` reads `dataset.theme` and never writes it). The near-miss is
   why every "bare selector" claim in the plan now names `:15` as the
   two-selector list instead of rounding it to "all eight are bare".

---

## §15 — v3: iteration-2 synthesis

> **Superseded in places by §16.** Iteration 3 found that three of this
> section's fixes were incomplete and one of its own claims was overstated:
> N2's "the class is closed" (softened in the row below), N5's
> `bundle-shape.mjs:100` (wrong line — `:101`), M-2's §5 Step 5.6 rewrite
> (unsatisfiable again, by a second mechanism), and §7's census (restated
> stale for the third time). Where a row here and a row in **§16** disagree,
> **§16 is current.** Rows are left as written rather than edited in place,
> because the sequence of attempts is the evidence that the *class* of defect
> is real and not three unlucky instances.

Iteration 2 of the consensus loop. Both reviewers opened with the same
instruction — **do not re-plan; the structure is sound; land targeted
edits** — so the nine-commit sequence, ADR-015/4D, ADR-016 and every verified
fact in v2 are unchanged. What follows is the ledger of what moved, one row
per finding, and every fix in it was checked against this tree before it was
written. Two structural additions are the exception to "targeted": **§13(a4)**
and **§13(d2)**, both of which exist because a v2 defect was invisible to
every clause the plan had.

**Scope discipline.** Only this document was edited. `git status --short` at
the close of the pass shows two modified and two untracked files, all four of
them the P3 side-cars (`tools/baseline-shots/**`, `docs/INVENTORY-*`) plus
this plan; **no source file was touched and nothing was committed.** The one
place that required running code — N1's TypeScript question — was answered
with reduced probes in a scratch directory rather than an edit to
`web/obsidianoid/js/threads.ts`.

### Architect — blockers

| | Finding | Disposition |
|---|---|---|
| **N1** | `threads.ts`'s `interface Window` stops merging globally the moment Step 5.2 adds an import; the plan asserted the opposite | **Fixed, and the plan was wrong in the direction that matters.** `grep -cE '^(import\|export)' web/obsidianoid/js/threads.ts` = **0**, so the file is a *script* today and `:17-20`'s bare `interface Window` does merge into the global scope. Step 5.2's `import` makes it a *module* and `:22`'s `window.ThreadsView = …` fails with **TS2339**. Both halves were measured with reduced `tsc` probes: the module form fails TS2339, and the naive repair — wrapping in `declare global` *before* the import lands — fails **TS2669** (`declare global` is legal only in a module), so the two edits are order-coupled and must ship in one commit. Step 5.2 now names the replacement verbatim (`declare global { interface Window { ThreadsView: ThreadsViewAPI; } }` for `:17-20`), §4 states the script-vs-module rule, **R6 is split** into a runtime half (does not break — the global property is real and `type="module"` preserves document order) and a compile half (does break; v2 asserted it did not), and **B2.9** is the new criterion that asserts both the clean `npm run typecheck` and the `declare global` form |
| **N2** | `web/todo/css/todo.css`'s drawer block is cited as `:102-143`; the tree says `:103-144` | **Fixed, and the blast radius was larger than the report.** Re-derived line by line: `:101` is the section comment, `:102` blank, **`:103` `.sidebar {`**, and the block's last line is **`:144` `.sidebar-backdrop.sidebar-open { display: block; }`**. The off-by-one had propagated to **all 20** line citations in §5 Step 6.2's property table, because the rows were written from the first citation instead of re-derived; all 20 are shifted +1 and the table now carries a note saying so. §13(b)'s deleted-occurrence row and §4's "144-line" reference are corrected to match. **The instance is closed, and the class is narrowed:** §13 gained **(a4)**, which range-checks every qualified `path:N` citation exhaustively (53, none out of range) and re-derives by content the D-table plus the ten densest tables (17 of 17 correct). (a1) could never have caught this — the *file* existed. *(**v4 correction to this row's own claim** — Critic F-4, Architect A7. v3 wrote "the class, not just the instance, is closed", which overstated what (a4) covered. Only **fully-qualified** `path:N` citations reach its range half, and only **17** reach its content half; the Critic's own sweep of the v3 body counted **135 short-form `file.ext:N`** and **659 bare `:NNN`** citations that inherit their path from context and are seen by neither half — it put content coverage at roughly **6%**, and reported that a range sweep over all 135 short-form citations found 0 genuine out-of-range hits, so the gap is the content dimension and not the range one. v3 then proved the point against itself by writing `bundle-shape.mjs:100`, in range and wrong, in the same pass. **v4 adds (a4) part 3**, which is exhaustive over the one axis that actually predicted all three defects — the citations the pass itself wrote — so the honest claim is that the class is *narrowed to a stated residue*, not closed.)* |

### Architect — mediums

| | Finding | Disposition |
|---|---|---|
| **N3** | B7.5 and B9.1 say "first line", but esbuild writes a `// <input path>` banner as line 1 | **Fixed, sharpened to what the gate actually tests.** `web/sampler/js/bundle.js:1` is `// web/sampler/js/main.ts` and the `/shared/dist/shared.mjs` import is on line **2**; `web/taskmaster/js/bundle.js` is the same shape. The gate never cared about line 1: `bundle-shape.mjs:38`'s `TOP_LEVEL_IMPORT` carries the `/m` flag, so it matches a **column-0** `import` anywhere in the file. B7.5 item 3 and B9.1's lead now say "a column-0 `import … from` statement", which is both true and the property the gate enforces |
| **N4** | C1 is named as the last-revertable commit; the dependency graph says otherwise | **Fixed.** The revert-order paragraph is rewritten and ADR-016's consequence 2 with it: **C1 is the *least*-revertable of the nine**, because every later boundary's `make check` runs the clause 12 and `--require` machinery C1 introduces. The correct last-revertable commits are the leaves — C8, then C7, then C6b |
| **N5** | Step 5.1's `bundle-shape.mjs` rewrite leaves the PASS line printing the wrong number | **Fixed, and the finding is stronger than reported.** `bundle-shape.mjs:101` prints `inputs.length`, which is the **descriptor** count, not the artifact count — and `:101` sits *outside* Step 5.1's `:68-94` rewrite range, so without an explicit edit the gate at C5 would inspect 4 artifacts and print `3`. Step 5.1 now names `:101` as part of the edit, and B9.1's per-boundary table **pins the expected PASS-line text** at each boundary rather than leaving it inferred. *(**v4:** this row said `:100` in v3 — the off-by-one the v3 fix itself introduced. Corrected at all six sites; see §16.)* |
| **N6** | ADR-012's negative test injects a colour literal, which proves nothing about the descent | **Fixed, after measuring it three ways.** A sandbox experiment confirmed the asymmetry the report implies: clause 7 is **two independent rules**, a whole-file per-line colour scan (`check-shared-css.mjs:304-308`) that is depth-blind and already catches a literal inside an `@media`, and a `parseBlocks`-driven selector loop (`:310-318`) that never sees inside at-rules because `parseBlocks` discards them at `:182`. So the colour half of v2's probe was a **no-op** and the selector half is the real hole. §0's Decision 2(c), ADR-012's amendment, B1.4 item 1 and §14's own M-6 row are all rewritten to inject a **non-`.ui-` selector carrying a colourless declaration**; §14's row carries a pointer here because it was describing the wrong probe |

### Architect — minors (four, matching the review)

| | Finding | Disposition |
|---|---|---|
| ADR-015's prior art unstated | — | **Fixed.** ADR-015 now cites the two places obsidianoid already does exactly this — `app.ts:437` (`btn.dataset.theme = t.name;`) and `:429` (`(b as HTMLElement).dataset.theme === name`) — and adds the paragraph on 4D's one real dependency: it works **only** because all eight blocks in `web/shared/css/themes.css` use bare attribute selectors (`:15, :38, :61, :82, :103, :124, :145, :167`), so a future `html[data-theme=…]` re-key would silently flatten all eight swatches |
| **R21** leaned on B3.8 alone | — | **Fixed.** R21's mitigation is **BX.10** — byte-identical inline blocks — with B3.8 as the secondary check. The `index.html`-versus-`compare.html` divergence is the failure mode that shows on one page only |
| ledger row 19 cites `compare.html:20` | — | **Fixed → `:18`.** `:16` is `jquery.js`, `:17` `utils.js`, **`:18` `js/theme.js`**, `:19` `compare.js`, `:20` the inline `<script>`. §4.8's page table and ledger row 16 already read `:18`, so this row was the outlier |
| §11 item 1's `!important` count | — | **Fixed by measurement.** `grep -n '!important' web/todo/css/todo.css` gives **twelve** in the file (`:359, :361, :365, :367, :373, :416, :419, :424, :427, :454, :455, :480`), of which **eight** fall inside the three ranges §11 spans — all eight colour literals. The item now states both numbers |

### Critic — critical and majors

| | Finding | Disposition |
|---|---|---|
| **C-1** | §7's `[deferred → Cn]` labels and §6's per-boundary gate cells disagree; some criteria are scheduled nowhere | **Fixed at all three levels, and this was the largest edit in the pass.** *The instance:* measured against the labels as they now stand, v2's cells **under-scheduled 19** distinct criteria and **over-scheduled 4** (B3.8 at C3, B3.9 at C6a, B9.3 at C6a, BX.7 at C1); drift existed at **all nine** boundaries; **B1.3** was labelled `→ C2` and appeared in no cell, and **B10.5** had neither a label nor a cell, so both were criteria with no boundary anywhere. *The reconciliation:* all nine §6 cells and all nine §5 `**Acceptance**` lines are re-derived from §7's labels, which are now the single definition; B10.5, BX.1 and BX.3 gained the labels §6 had been enforcing tacitly; B9.4 (`→ C5/C6a`) and BX.5 (`→ C5/C6a/C8`) were **widened with stated reasons** rather than trimmed; BX.7's C1 cell entry is corrected to **BX.8**; B3.8 moves C3 → C6a; B3.9 and B9.3 leave C6a as obsidianoid-only. *The enabling gap:* §13 audited names, definitions, counts and shape but **never scheduling**, which is why two reviews passed over it — so §13 gained **(d2)**, which requires set equality between label, §6 cell and §5 Acceptance in **both directions at all nine boundaries**. It was run; it reports **PASS, 9/9, 77 criteria, 131 (criterion, boundary) pairs, 0 unlabelled**, and the published script reproduces the published output when extracted verbatim from this document |
| **M-1** | G15's "two-line edit" cites the trajectory comment as `:157-158` | **Fixed → `:157-161`, with the value-bearing numbers on `:160`.** Corrected in all three places it appeared: G15 at `:410`, §5 Step 6.1, and §14's own M6 row |
| **M-2** | §5 Step 5.6's pixel-evidence step is unsatisfiable | **Fixed by rewriting §5 Step 5.6 whole.** Two independent defects: (1) it prescribed a P5-versus-post-C5 diff, the exact comparison **P5a** exists to forbid — adding the `/shared/dist/shared.css` link pulls in `fonts.css`'s 15 `@font-face` rules, so every glyph reflows and the diff is non-empty essentially everywhere; the reference of record is now **P5a's** in-C5 capture, taken after the link lands and before any other C5 edit. (2) It required every non-empty diff to map to "a row of the D-table above", but the typography change is **ledger row 13**, not a D-row, so a correct run could not be attributed. The attribution rule is now explicitly two-part |

### Critic — minors (eight; the two with their own rows, then the rest)

| | Finding | Disposition |
|---|---|---|
| ADR-015's consequence cites "ledger row 13" | **Fixed** to **Ledger row 15 / D-15** at `:1217`. Row 13 is obsidianoid's *typography* deviation, a different fact entirely; clause 7's colour scan is cited at `:304-308` in the same passage |
| D-15 does not mention that one theme's border is translucent | **Fixed.** Of the eight `--color-border` values in `web/shared/css/themes.css` (`:20, :43, :66, :87, :108, :129, :150, :172`), seven are opaque hexes and **puma's `:172` is `rgba(255, 255, 255, 0.11)`**. D-15 now records that the puma swatch's border composites against its own `--color-primary` fill rather than replacing it — the closest behaviour to the donor's fixed `rgba(255,255,255,0.15)`, and the swatch a pixel diff will show moving least. Not a defect; recorded so a reviewer does not read the odd one out as a bug |
| the remaining minors | — | Each is folded into the row of §14 or §7 it corrects rather than restated here; every one is marked in place with a `*(… v3 …)*` note, and `grep -n 'v3' docs/PLAN-ui-unification-phase2.md` enumerates them |

### The three unscored questions, answered in place

| | Question | Answer |
|---|---|---|
| **(a)** | Is §6's scheduling of BX.5, B3.9, B9.3 and B9.4 at boundaries their labels do not name deliberate? | **Two yes, two no, and each answer is now written into the label.** **B9.4 at C6a — deliberate:** C6a adds the fourth `sharedConsumer` descriptor, and a new descriptor is exactly the occasion on which someone re-inlines the `.tsx?$` derivation "just for this one case"; the label reads `C5/C6a`. **BX.5 at C5/C6a — deliberate:** the range is `b73d31c..<boundary>`, so each run is a *prefix* check, and catching a stray fourth module at C5 is worth three commits more than catching it at C8; the label reads `C5/C6a/C8`, and C8 — which v2's cell had dropped — is restored as the full-phase run of record. **B3.9 at C6a — not deliberate, removed:** it is obsidianoid's per-vault persistence. **B9.3 at C6a — not deliberate, removed:** it is the `EISDIR` fix for `out: "web/obsidianoid/js/"`, and todo's descriptor is `mode: "bundle"` with `out: "web/todo/js/shell.js"`, a *file* path, so the directory hazard cannot recur there |
| **(b)** | Add or decline a criterion catching a `--require` list left too short by a C5 deferral? | **Added — BX.11**, `[deferred → C1/C5/C6a]`. Nothing covered it: BX.8 proves the flag can fail, but only for names that are *in* the list; a name never added is a name whose bundle shape is simply not required, and every gate stays green — the same failure mode as v1's `--expect-nonempty`, one level in. BX.11 asserts **set equality**, not subset, between the recipe's comma list and the `sharedConsumer` descriptors that exist at that commit, and it derives the expected set from `scripts/descriptors.mjs` rather than restating B9.1's prose lists, so it stays correct without edit even if C5 is deferred out of the phase. *Executed for the premise:* `Makefile:62` is `node scripts/gates/bundle-shape.mjs` with no flag at all today, so the check fails — the correct result before C1 |
| **(c)** | Do C4's real `@media` selectors get a post-C4 assertion? | **Yes, and it is now stated** on B1.3's C4 leg. C4 adds the first at-rules the plan ships (`components.css` has **zero** `@` characters today, which is why **B1.4's probe is synthetic by necessity**), and a descent that works on a hand-written probe but not on real content is the gap a synthetic probe cannot close. Clause 7's colour scan (`:305-308`) is depth-blind and already covers the new blocks; its selector loop (`:310-318`) is the half that depends on C2's descent, because `parseBlocks` discards at-rules at `:182`. So C4 adds a positive (clause 7 passes, rc 0) and a negative on real content (retitle a selector inside C4's `prefers-reduced-motion` block; clause 7 must fail naming that inner selector **at a line greater than the enclosing `@media`'s line**, which is the descent evidence; revert). *(**v4** — this row said the positive leg also asserts "the selector loop's inspected-block count exceeds the depth-0 block count". The gate emits no block counts and exports nothing, so that was unobservable; the descent proof moved to the negative leg's line number, which `fail()` already prints. Architect A2/M-1, Critic F-7.)* |

### New in v3, and why each is new rather than a rewording

| | What | Why it could not be a note |
|---|---|---|
| **B2.9** | `npm run typecheck` is clean after obsidianoid becomes ESM, and `threads.ts`'s `Window` augmentation is a `declare global` block | N1's defect is invisible to `make gates`: `Makefile:60-64` runs four scripts and **none of them invokes `tsc`**. Typecheck lives in `test-web` (`:66-69`, the `npm run typecheck` line at `:68`), so the criterion has to name it |
| **BX.11** | `--require`'s list is complete, not merely present | See unscored question (b) |
| **§13(a4)** | line-citation range check (exhaustive) + content spot-check (bounded) | (a1) proves the cited *file* exists and is structurally blind to the `:NNN` after it — the dimension N2's defect lived in |
| **§13(d2)** | label ⇄ schedule equality, both directions, nine boundaries | Every other clause audits what a criterion *says*. None audited **when it runs**, which is how C-1's defect passed two reviews with a perfect shape |

### Self-audit, run against this revision

Full outputs are published in §13 beside the clause that produced them. Summary:

| Clause | Result |
|---|---|
| a1 | 65 paths cited, 54 existing, 9 declared-new, 2 counterfactual, **0 phantom**, 0 new-but-present |
| a2 | **77** criteria (v2: 75), no duplicate ids; 6 executed, 71 deferred, 28 with substitutes |
| a3 | the Phase-1 retired-name ban — **0** body-scoped. *(The banned string is deliberately not written out here: §15 is inside (a3)'s own audited body, unlike §13, and quoting the name would make this row report itself — the identical self-reference §13(a)'s header comment already records for its first v2 run.)* |
| **a4** | **53** qualified citations, **0 out of range**; content spot-check **17 of 17** re-derived correctly, including `todo.css:103-144` |
| b | one-definition table extended by one row; 4 declared duplications unchanged |
| c | artifact/gate-input trajectory unchanged: 15/15/15/**16**, inputs 2 → 4 → 5 |
| d | shape clause unchanged; its two v2 corrections stand |
| **d2** | **PASS, 9/9** — label-set == §6 cell == §5 Acceptance at every boundary; 131 pairs; 0 unlabelled; 0 criteria scheduled at none of their boundaries |
| e | scope list unchanged; BX.5's label widened, so the clause is now asserted at three boundaries rather than one |

### Rebuttals

**None.** Every blocker, Critical, Major, medium and minor from both
iteration-2 reviews was checked against the tree and every one was confirmed
as stated. Three were confirmed *and strengthened* — N2's blast radius was 20
citations rather than 1, N5's wrong number is printed from a line outside the
range the plan proposed to rewrite, and N6's colour-literal probe turned out
to be a strict no-op rather than merely a weak test — and in each case the
plan records the stronger version, not the reported one. v2's two standing
rebuttals (Architect M7 / Critic M-10 on the 38-per-block census, and Critic
M-3's descriptor-side `out` fix) are untouched and still stand on the same
evidence.

### Corrections to my own v3 work, made before publishing

1. **The Step 5.2 N1 edit was first labelled "Architect N2"** and corrected in
   place. N2 is the `todo.css` citation.
2. **BX.1's first rationale claimed C1 is the only commit touching
   `Makefile`/`scripts/gates/`** — false: B9.3 edits
   `scripts/gates/bundle-shape.mjs` at C5. Narrowed to the defensible claim,
   that C1 is the commit which *introduces* clause 12 and no later commit adds
   one.
3. **BX.11's first draft called the export `m.DESCRIPTORS`.**
   `scripts/descriptors.mjs` exports lowercase `descriptors`; the snippet was
   corrected and then run.
4. **(d2)'s prose first claimed the script prints the
   never-scheduled-anywhere check.** It does not — set equality *implies* that
   property, so the prose now says so instead of describing output that is not
   there.
5. **Both new clauses were verified by extraction, not by transcription.** The
   `(a4)` and `(d2)` scripts were pulled back out of this document
   programmatically and executed; each reproduced its published output
   character for character. A published result that was retyped from a
   different script than the one printed would be exactly the class of defect
   this plan spent two iterations removing.

---

## §16 — v4: iteration-3 synthesis

Iteration 3 of the consensus loop. The Architect voted **SOUND** on a narrow
margin — blocker B-1, mandatory amendments A1–A3, mediums M-1…M-3, seven
minors, with N1–N6 and C-1 all confirmed resolved — and the Critic voted
**ITERATE** with F-1 critical, F-2…F-7 majors and three minors, having
re-extracted and re-run every published §13 clause and reproduced every
output character-for-character. **Both reviewers were correct on every item.**
This pass raises no finding-level rebuttal; the only disagreements recorded
below are two places where a reviewer's *suggested repair* does not work in
this tree, and in both the finding stands and a different repair is used.

**Scope discipline.** Only this document was edited. `git status --short` at
the close of the pass shows two modified and two untracked files — all four
the P3 side-cars (`tools/baseline-shots/**`, `docs/INVENTORY-*`) — plus this
plan. **No source file was touched and nothing was committed.** The two items
that required running code (B1.4's probe 2 and the new §13(a5) clause) were
executed against a **copy** of `scripts/` + `web/shared/` outside the repo
and against this document respectively.

### The four convergent items

Both reviewers independently reached the same four conclusions. Each is a
*structural* fix, not a reword — the third attempt at two of them.

| # | Item | Architect | Critic | Resolution |
|---|---|---|---|---|
| 1 | §5 Step 5.6 / P5a's capture point is unsatisfiable — again, by a second mechanism | B-1 + A1 | F-6 | **Re-specified by state.** P5a is now "the first renderable, vocabulary-consistent state": Step 5.3 items 1–4 **plus** Step 5.4's first bullet, as **one** working-tree sub-step, capture after. Step 5.3's link is pinned as an **in-place replacement of `index.html:7`**; Steps 5.2 and 5.3 item 5 are pinned **after** the capture; the false "no panel changes colour" claim is deleted and the expected residue is enumerated exactly |
| 2 | B1.4's probe 2 cannot fail | A3 + M-2 | F-2 | **Replaced with a measured probe.** Wrap `web/shared/css/themes.css:167-187` in `@media (min-width: 1px)`; descent-off must fail with the exact `fail()` string, descent-on must return to PASS. Both directions were **run** at plan time |
| 3 | B1.3's C4 positive leg asserts an unobservable quantity | A2 + M-1 | F-7 | **Both reviewers' fixes taken together**, because they are complementary: the unobservable count is **deleted** (Architect) and the descent proof moves to part 2's **line number**, which `fail()` already prints (Critic). No instrumentation is added |
| 4 | `bundle-shape.mjs`'s PASS line is `:101`, not `:100` | A4 | F-3 | Fixed at all six sites, with the line map recorded once |

### Architect — blocker and mandatory amendments

| Item | Finding | Disposition |
|---|---|---|
| **B-1** | §5 Step 5.6's reference-of-record is a state in which obsidianoid does not render, and its scope claim is false at **every** candidate capture point | **Accepted in full; convergent item 1.** §5 Step 5.6 is re-specified whole, with a table of the four candidate triggers and why each broken one fails, an exact expected-residue list, and a new rule forbidding the attribution rule from being widened in flight |
| **A1** | Re-specify P5a by **state**, not edit order; delete the "no panel changes colour" claim; name D-1/D-2/D-3 + ledger row 13 as expected residue | **Accepted, with one ordering constraint added beyond the amendment's wording.** A1 lists **D-7** in the residue; D-7's chevron `mask-image` rewrite is Step 5.3 **item 5**, so its presence in the diff would depend on executor sequencing. Pinning Steps 5.2 and 5.3 item 5 **after** the capture makes the residue exactly ledger row 13/D-2, D-1 and D-3, and D-4/D-5/D-6/D-7/D-15 fall on the other side of the shutter. Stated as an addition rather than folded in silently. *(**Historical, and left as written.** v5's Q12 ruling retired **D-3** to zero delta and added **D-16**/**D-17**, so the live residue list is no longer this one — it is **§5 Step 5.6**'s, and only that. This row records what v4 decided in response to iteration-3's A1; §15 and §16 are not rewritten when later rulings move the facts they record. The retired D-3 row is §5 Step 5.5, so the label still resolves.)* |
| **A2** | Make B1.3's C4-leg part 1 observable, or drop it | **Accepted; convergent item 3.** Dropped, and the observable half substituted — see the rebuttal-free rationale in B1.3 for why the print-and-pin alternative was declined |
| **A3** | Restate B1.4 item 2 so it can fail | **Accepted; convergent item 2.** The old form was permanently vacuous (`tokens.css` and `themes.css` have zero `@` characters and B1.1 freezes both for the phase) *and* stated the logic inverted. Both defects are recorded in the criterion |

### Architect — mediums

| Item | Finding | Disposition |
|---|---|---|
| **M-1** | B1.3's C4-leg part 1 asserts a number the gate never emits | **Accepted** — same edit as A2 |
| **M-2** | B1.4 item 2 cannot fail, for the same structural reason N6's probe could not | **Accepted** — same edit as A3 |
| **M-3** | **Nothing audits intra-document `§`/`Step` cross-references, and the v3 pass introduced three errors of exactly that kind** | **Accepted, and this is the pass's second structural addition.** §13 gains **(a5)**, which resolves every `§N`, `§N.M`, `Step N`, `Step N.M` and §13-clause pointer in the audited body against the document's actual headings, bold sub-step labels and clause labels. Its **first run over v3's text reported 12 dangling pointers across 5 distinct targets.** It also states the pointer convention v3 used two ways, and it declares what it cannot do: resolve **aim** |

### Architect — minors

| Item | Finding | Disposition |
|---|---|---|
| **A4** | `bundle-shape.mjs` PASS line is `:101`; `:100` is blank; six sites | **Fixed;** convergent item 4 |
| **A5** | §13(a4) attributes the N2 defect's 20-row table to Step 5.5 (obsidianoid's D-table) | **Fixed** — the table is §5 **Step 6.2a**'s, at both sites. The pointer *resolved*, which is why only a reader could catch it — recorded as (a5)'s stated limit |
| **A6** | BX.1 says C1 introduces clause 12 "(Step 5.1)"; it is **Step 1.3** | **Fixed.** Step 5.1 is C5's descriptor plumbing. Same class as A5 |
| **A7** | §13(a4)'s "including every citation the v3 pass rewrote" is falsified by A4 | **Fixed** — the clause deleted, and (a4) **part 3** added as the axis that claim was reaching for |
| **A8** | §13(c)'s column is labelled "`bundle-shape.mjs` inputs" while holding artifact counts; B9.1 calls it "Outputs inspected" | **Fixed** — relabelled to match the gate's own PASS wording |
| **A9** | §4.4's drawer inventory omits `.sidebar-section` `:124` | **Fixed** — added, and the note that cross-references the inventory is corrected too (it said "§4.2" and listed four of the block's six rules) |
| **A10** | Deleting `todo.css:617-619` strands `:615`'s `/* ── Responsive ── */` heading as a 616-line file's tail | **Fixed** — the deletion is **`:615-619`**, stated in §5 Step 6.2 and in §13(b)'s row, by the same reasoning as the `:101-102` head tidy |

### Critic — critical and majors

| Item | Finding | Disposition |
|---|---|---|
| **F-1** | **CRITICAL.** BX.10's file set names `web/sampler/index.html`, which has no inline bootstrap and gets none — so BX.10 either **blocks C6a** or its sampler clause **cannot fail** | **Accepted in full; BX.10 redefined over todo's two pages only.** It was the straggler from the v2 pass that fixed B3.8, B10.5, ledger row 16 and §14 M3. The fix is a **narrowing** — no touch list is widened. The extraction rule is replaced with **sentinel comments** written by Step 6.3 item 3, which also gives the fourth-copy clause a real `grep -rl` detector, so part 1 compares the copy set for **equality** rather than hoping. The touch list is unchanged |
| **F-2** | B1.4's probe 2 cannot fail; `:2345` states the logic inverted | **Accepted;** convergent item 2 |
| **F-3** | `:100` → `:101` at six sites | **Accepted;** convergent item 4 |
| **F-4** | §15's "the class, not just the instance, is closed" is overstated — (a4) covers ~6% by content | **Accepted, and the Architect's synthesis implemented.** §15's row is softened to "the instance is closed, and the class is narrowed", quoting the Critic's own 135-short-form / 659-bare sweep and its finding that the *range* dimension is broadly sound and the **content** dimension is the gap. (a4) gains **part 3**, exhaustive over the citations the pass itself wrote — the axis that predicted all three historical defects, and the **standing rule for future passes** |
| **F-5** | §7's census prose is stale for the third time, and its "not maintained by hand" claim is false | **Accepted, both halves.** Numbers corrected to **77 / 6 / 71 / 28**, and the claim replaced by a **check**: (a5)'s second half parses §7's four integers and fails unless they equal (a2)'s. Asserting a derivation that nothing performed is the one-definition rule inverted — it licenses the reader to trust the copy |
| **F-6** | §5 Step 5.6 unsatisfiable on both horns: Step 5.3 never pins `shared.css`'s position relative to `css/themes.css`, and 5.1/5.2 precede 5.3 in the document's own numbering | **Accepted;** convergent item 1. The link is pinned as an in-place replacement of `index.html:7`, with the identical-specificity collision (`web/obsidianoid/css/themes.css:2` and `web/shared/css/themes.css:15` are **both** `:root, [data-theme="dark"]`) written down as the reason link order alone decides the palette. Step 5.3 becomes an **ordered** numbered list with an explicit atomic-group statement, and the "before any other C5 edit" phrasing is gone |
| **F-7** | B1.3's C4 positive leg asserts an unobservable quantity | **Accepted;** convergent item 3 |

### Critic — minors

| Item | Finding | Disposition |
|---|---|---|
| 1 | "§4.2's drawer inventory" — it is in **§4.4** | **Fixed** (with A9, in the same note) |
| 2 | "before the stylesheet parses" is false | **Fixed** → "before **first paint**", with the reason recorded: `todo.css` is linked at `index.html:13` / `compare.html:13`, seven lines above the script slot. This was the **root cause** of BX.10's broken extraction rule as well, so the two fixes are cross-referenced |
| 3 | BX.10's "a fourth copy must join the set" has no detector | **Fixed** — resolved naturally by F-1's sentinel mechanism, which turns the intent into `grep -rl` plus a set-equality comparison |

### The Critic's residual-(d2) observation, answered

The Critic's duty-3 answer was that **(d2) does not close the C-1 class**: it
closes the *drift* sub-class provably, but the residual hole is a criterion
whose label and cells **agree** while the criterion is unrunnable at that
boundary — and it named **three live instances, all on v3's edited surface**:
BX.10 (guaranteed red or vacuous), B1.4 probe 2 (cannot fail), B1.3's C4 leg
(part 1 unobservable). That is exactly right, and §13 conceded the *gap*
without conceding that anything was sitting in it. **All three instances are
closed by this pass** — items F-1, F-2/A3 and F-7/A2 respectively. The gap
itself is not closed and is not claimed to be: no clause in this document can
decide whether a criterion is *runnable*, only whether it is scheduled
consistently. What changed is that the three known occupants are gone and the
concession now says so.

### New in v4, and why each is new rather than a reword

| Addition | Why it is structural |
|---|---|
| **§13(a4) part 3** — diff-scoped citation re-derivation, exhaustive | Parts 1 and 2 sample by *range* and by *density*. Neither would have caught any of the three historical defects, because all three were in range and none was in a dense table. Recency of edit is the predictor, so the diff is the right sample — and it is small by construction, which is what makes "exhaustive" affordable |
| **§13(a5)** — intra-document pointer resolution + the §7 census restatement | The third audit blind spot. (a1) and (a4) audit pointers *out* of the document; nothing audited pointers *within* it, and four of iteration-3's findings were of exactly that kind. Its first run found 12 |
| **(a4) part 1 now reports found / checked / skipped separately** | v3's script folded them into one number, so a citation could be *found* and never *checked*. That is the same shape as the gaps P-III exists to forbid, inside the audit itself |
| **The atomic-sub-step concept in §5 Step 5.3** | Two iterations tried to fix §5 Step 5.6 by choosing a better *edit* to capture after. The defect was the frame: some adjacent edits do not have a renderable tree between them, so the unit of pixel evidence is a **state**, not an edit |

### Self-audit, run against this revision

Every clause in §13 was re-run after the last edit, including both new/widened
ones. Outputs are published in §13 next to their clauses; the summary:

> **Every integer in the table below is v4's, and is left as written.** §15
> and §16 record what those passes measured; §13 carries the *current* run and
> §17 carries v5's, and the two sets differ because five criteria were added
> and the audited body grew. Reading a v4 number here as a claim about this
> revision is the one misreading this note exists to prevent. *(Added in v5.)*

| Clause | Result |
|---|---|
| a1 | **65** paths cited · **54** existing · **9** declared-new · **2** counterfactual · **0** phantom · **0** new-but-present — unchanged from v3; §16 cites no path v3 did not |
| a2 | **77** criteria, no duplicate ids · **6** executed · **71** deferred, **28** with substitutes — and §7 now states the same four numbers, checked by (a5) |
| a3 | **0** body-scoped occurrences of the Phase-1 retired name |
| a4 part 1 | **61 found, 61 checked, 0 skipped, 0 out of range** (v3 published 53) |
| a4 part 2 | **0 of 17** wrong anchors |
| a4 part 3 | **0 defects of 62** citations this pass added or changed — the first run of the new half |
| a5 pointers | **0** dangling (first run, over v3's text: **12** across 5 targets) |
| a5 census | **PASS** — (a2) derives `(77, 6, 71, 28)`, §7 states `(77, 6, 71, 28)` |
| b | unchanged in structure; one row re-derived (`todo.css` `:615-619`) |
| c | unchanged in content; third column relabelled (A8) |
| d / d2 | (d2) **PASS**, 9/9 boundaries, 131 label⇄schedule pairs — unchanged |
| e | unchanged |

### Rebuttals

No finding is rebutted. Two **suggested repairs** are declined, with the
finding accepted in both cases and verified in the tree first:

1. **The Critic's BX.10 anchor — "the `<script>` with no `src` attribute in
   `<head>`" — is not unique in this tree.** Both todo pages already carry a
   large inline src-less `<script>` in `<head>`: `web/todo/index.html:21-164`
   (144 lines) and `web/todo/compare.html:20-27`, and both survive C6a. "The
   **first** src-less `<script>`" *would* be unique and correct, since the
   bootstrap replaces `theme.js` one line above each. Sentinel comments are
   used instead for a reason the positional rule cannot satisfy: a position
   cannot detect a fourth copy **elsewhere**, and Critic minor 3 asks for
   exactly that detector. One mechanism discharges both halves.
2. **The Architect's alternative for A2 — have C2's descent print
   `clause 7: N block(s) inspected` and pin the string per boundary as B9.1
   pins `bundle-shape`'s — is declined** in favour of its own cheaper form
   plus the Critic's line-number assertion. It would add a stdout contract to
   a gate this phase otherwise only re-scopes, and the line number `fail()`
   already prints buys the same descent proof with no code change.

### Corrections to my own v4 work, made before publishing

1. **I wrote a plan-internal line citation into §5 Step 5.6 and Step 5.3 —
   `(§4.3 :603)` — which is the exact failure mode this iteration was warned
   about.** Bare `:NNN` in this document resolves against the last-named
   **file**, and my own header edit had already shifted the plan's line
   numbers besides. Both were replaced with "§4.3's parity table". Caught by
   grepping the citation immediately after writing it; it is the third
   consecutive iteration to introduce a citation error on the surface it had
   just edited, and the reason (a4) part 3 exists.
2. **D-7 was wrongly carried into the P5a residue list**, following A1's
   wording. It is Step 5.3 item 5, not items 1–4. Corrected by pinning the
   ordering (see A1's row) rather than by leaving the residue list
   approximate.
3. **Three stale sentences survived in the P5a cell** after that ordering
   decision — "Steps 5.1, 5.2 and 5.3 item 5 may precede or follow",
   "captures a second baseline *after* the link lands", and "independent of
   how the executor sequences 5.1/5.2". All three were rewritten to match §5
   Step 5.6; a cell and a step disagreeing about the same fact is the
   one-definition rule's own failure mode.
4. **My first `parseBlocks` descent patch for B1.4's probe 2 silently did
   nothing**, because the block objects carry no raw body to recurse into. The
   probe was only publishable once the patched run actually returned PASS —
   an unpatched "expected PASS" would have been a predicted contrast
   published as a measured one.
5. **"Scratch copy" is not literally possible for `check-shared-css.mjs`**:
   `CSS_DIR` is a hard-coded const and the script `chdir`s to its own parent.
   The probe was run against a **copy of `scripts/` + `web/shared/`** outside
   the repo, which works *because* of that `chdir`, and B1.4 now says so
   rather than implying a `--dir` flag the gate does not have.
6. **I nearly published an explanation for the Critic's "checks 49, publishes
   53" gap that I could not reproduce.** The explanation was deleted; (a4)
   part 1 now prints found/checked/skipped as three numbers instead, so the
   quantity cannot diverge unremarked again. An invented mechanism in an audit
   clause is worse than an acknowledged unknown.

---

## §17 — v5: iteration-4 synthesis, and the seven user rulings

Iteration 4 of the consensus loop, and — under the 5-iteration budget — the
last. The Architect voted **SOUND on a narrow margin, conditional on A-1 and
B-2**; the Critic voted **ITERATE** with F-A critical, F-B critical, F-C and
F-D major, two minors, four "missing" items and three unscored open
questions, having re-extracted and re-run every published §13 clause and
**reproduced all six integers exactly**. **Both reviewers were correct on
every scored item. This pass raises no rebuttal.**

That last sentence is now four iterations old, and it is the reason this
pass spent its effort differently: when the audit layer is reproducible by a
third party and the reviewers are never wrong, the remaining defects are all
*semantic*, and the only durable answer is to make the semantic class
machine-visible too. Hence §13(a6), hence (e)'s new derivation, hence the
back-pointers.

**Where iteration 4's fresh defects sat, which is the finding behind the
finding.** F-A landed on a surface **v4 had just rewritten** (B1.3's
replacement C4 leg). F-B, F-C and F-D all landed on text **v4 never
touched** — Step 5.2's deletion census, ADR-011's *Consequences*, Step 5.4's
migration bullet, each of them carried unexamined since v1 or v2. Four
rounds of review have therefore been progressively clearing the *edited*
surface while the *inherited* surface was only ever sampled. Part 3 of (a4)
is the guard for the first class and it works. §13(a6) is the first guard
this document has ever had for the second.

**Scope discipline.** Two documents were edited: this plan and
`docs/OPEN-QUESTIONS-ui-unification.md` (Q9 closed, Q2 amended, a
reconciliation note added — **160 → 235** lines; *(**v5 FINAL**, Critic minor 13: v5 DRAFT published 161 here and at P6, by counting the trailing newline as a line. `git show HEAD:docs/OPEN-QUESTIONS-ui-unification.md | wc -l` = **160**; the 235 is exact. Corrected at both sites, because the surrounding clause claims to be grep-derived.)*). **No source file was touched
and nothing was committed.** The `.omc/plans/open-questions.md` side-car was
deliberately left alone because it is outside this pass's brief; §9 records
that as a stated lag and names itself the record of account.

### Convergent item — the one both reviewers found independently

| Item | Architect | Critic | Resolution |
|---|---|---|---|
| The at-rule descent must be specified to preserve **absolute** file coordinates, or B1.3's C4-leg part 2 is red on a *correct* implementation | **A-1 (blocker)**, reproduced by implementing the descent two ways in a scratch copy | **F-A (critical)**, reproduced as `components.css:2: clause 7: … ".sidebar-thing"` for a block at absolute line 104 | **Accepted in full.** §5 Step 2.3(b) now pins the post-condition — an inner block's `line` is absolute and file-relative, the at-rule body's start line is added back, with the `parseDeclarations(bodyBuf, bodyLine)` idiom named — and **B1.4 probe 1** is extended to assert the printed line is the inner selector's line **in the file**, which is what makes the contract fail at **C2**, where the code is written, instead of at C4, where §6 forbids touching the gate |

The two reviewers reached it from opposite ends — the Architect from the
implementation, the Critic from the criterion — and that is the clearest
statement of the root cause both named: **a criterion asserted a property of
a gate's output that the step writing the gate never specified.**

### Architect — the two blocking amendments

| Item | Finding | Disposition |
|---|---|---|
| **A-1** | Descent must preserve absolute line numbers; pin it in Step 2.3(b) and make it fail at C2 | **Accepted; convergent item above.** |
| **B-2** | Step 5.3 item 5 lands *after* the P5a shutter with two of its four edits undeclared in the D-table — `.disabled-overlay`'s `rgba(19, 19, 26, 0.72)` and the three `color: #fff` sites | **Accepted in full, and the D-table is now the single roster.** **D-16** (`app.css:634` → `var(--overlay-scrim)`) and **D-17** (`color:#fff` → `var(--color-primary-fg)` at `:90`, `:404`, `:540`) are added; the residue list and Step 5.6's attribution roster read **D-1…D-7, D-15…D-17**. The pass then went further than the amendment, because the amendment's own logic demanded it: §5 Step 5.3a item 3 now carries a **machine inventory** of every out-of-vocabulary colour in `app.css`, since a hand-built roster is exactly what B-2 caught being incomplete. That inventory found the two values a single naive `grep` misses — `:383`'s `oklch(0 0 0 / 0.6)` (not in the usual `#`/`rgb`/`hsl` alternation) and `:740`'s `stroke='%237878a0'` (the `#` percent-encoded inside a data URI) — which are also the two most easily forgotten |

**The Architect's iteration-5 proposal was adopted as specified.** Rather
than a new clause for the back-pointer class, each criterion that pins a
gate's output or a pixel diff's composition now names the step that
guarantees it, and the step names the criterion back: **B9.1**, **BX.2**,
**BX.11**, **B1.4**, **B1.3**'s C4 leg, and **§5 Step 5.6**'s two attribution
windows. Six pairs, in the shape B9.1 has used informally since v1 and the
only one of the six that has never broken.

> ***"Adopted as specified" was an over-claim in v5 DRAFT, and this is the
> correction.*** *(**v5 FINAL**, Critic finding 4.)* Measured against the
> document rather than against the intention, **four of the six pairs were
> one-legged** when the claim was written:
>
> - **BX.2** — criterion → step present; **no step named it back**. Its three
>   owning steps (§5 **Step 2.4**, **Step 3.3**, **Step 4.3**) now each carry
>   `*Guaranteed by (criterion): **BX.2**.*`
> - **BX.11** — same shape, reverse leg absent. §5 **Step 1.4** and **Step
>   6.1** now name it (each alongside **B9.1**, which they guarantee jointly).
> - **B9.1** — **one of its three** owning steps named it; the other two
>   (Step 1.4, Step 6.1) did not. Both now do.
> - **B2.7** — worse than one-legged in the other direction: it had **no
>   `Guaranteed by (step)` line at all**, while §5 Step 5.6 and §11 both cited
>   it as the colour evidence of record. Step 5.0 is now named on the
>   criterion and B2.7 on the step.
>
> **B1.4** and **B1.3**'s C4 leg were genuinely two-legged as claimed, and
> Step 5.6's two attribution windows were the pass's own addition, so two of
> the six stood. The live inventory, machine-derived: **13** `Guaranteed by`
> lines in §5 pointing at criteria and **15** in §7 pointing at steps — the
> asymmetry is expected, since one step can guarantee several criteria and
> one criterion can be guaranteed by several steps. The lesson recorded rather
> than the count: a structural remedy announced in a summary section is not a
> structural remedy until the sweep that installs it has been **run and
> counted**, which is why §13 audits pointers and §17's prose does not.

### Critic — the eight to-approve items

| # | Finding | Severity | Disposition |
|---|---|---|---|
| 1 | Step 5.2's deletion census accounts for **one of three** surviving call sites — `app.ts:478-479` and `:503` are unaccounted; **C5 does not compile as written** | **CRITICAL (F-B)** | **Accepted.** Step 5.2 gains a three-row call-site table covering all three, with `:477-480`'s guarded block collapsing to **`themes.reresolve()`** and the `reresolve()`-not-`set()` rationale stated **inline** rather than by reference — plus, in v5, the third path's contrast: the shared `.ui-theme-picker` *does* call `set()`, and the `persist` parameter disappears from obsidianoid entirely |
| 2 | No criterion exercises the first-load `serverDefault()` path on an empty `localStorage`, in the only adopter that has one | **CRITICAL (F-B)** | **Accepted.** New **B2.11**: fresh profile, vault configured `forest`, renders forest — not `system`, not `default`. B3.9's existing scenario writes storage first and so could never reach position 2 |
| 3 | B3.10 and §6 both say `reresolve()` has "one caller"; after item 1 there are two | **minor** | **Accepted.** B3.10's blockquote becomes a two-caller table (boot, switch) and §6's C5 revert text is corrected. Recorded as more than a typo: that sentence is where F-B's blind spot was *written down* |
| 4 | Step 2.3(b) must pin the descent's coordinate contract | **CRITICAL (F-A)** | **Accepted; convergent item above** |
| 5 | ADR-011's *Consequences* contradicts ledger row 16 and BX.10, and the premise both rely on ("its existing stamp is already pre-paint") is **false in the tree** | **MAJOR (F-C)** | **Accepted.** The false premise is deleted from ADR-011's *Consequences*, ledger row 16 and **BX.10** — **three** live sites — and **annotated** at the one historical site that also carries it, §14's **M3** row, which is left as written per the §15/§16 convention. *(**v5 FINAL**, Critic minor 7: v4 wrote "all three places" while the string lived at **four**; the fourth is a historical record, so it is labelled rather than edited, and the count is now stated as 3 live + 1 annotated instead of a bare "all".)* The exclusion is restated on its real grounds: `index.html:2`'s **static** `data-theme` is obsidianoid's only pre-paint signal, Step 5.4's first bullet keeps it correct through the coupled rename, and **B2.7**'s 0-mismatch parity leaves the pre-paint appearance unchanged. The *conclusion* (no inline bootstrap for obsidianoid) was always right, which is why this was a major and not a critical — but it was right for a reason the document did not contain |
| 6 | Step 5.4's migration is unsatisfiable under one of its two readings; B2.6 cannot distinguish them; **R2 claims coverage B2.6 does not provide** | **MAJOR (F-D)** | **Accepted.** The migration bullet now states the key-discovery mechanism — a prefix-scan of `Object.keys(localStorage)` for `obsidianoid-theme-` — and where it runs relative to construction; **B2.6** is widened to **two** vault keys so a per-vault loop is distinguishable from a single-key special case; R2's coverage sentence is rewritten to what B2.6 now actually proves |
| 7 | The construction point of `ThemeManager` is unstated in all three adopters — `new ThemeManager` appeared **zero** times in 5,110 lines | **MAJOR (F-D)** | **Accepted, and it is the shared root of items 1, 2 and 6.** Each adopter's construction site is now written out, and **B2.10** asserts `new ThemeManager` appears exactly once in `app.ts` — because "the instance exists" was the unstated premise under three separate findings |
| 8 | No §13 clause checks **reference-completeness for deleted symbols**; (b) is definition-side only and (a4) checks cited lines, never uncited ones. This is the clause that would have caught item 1 mechanically, and item 1 survived three iterations because it did not exist | **structural** | **Accepted as proposed, and it immediately earned its place — see below.** New **§13(a6)**: 13 symbols across four boundaries, **36** remaining authored references, each reconciled against the plan text |

**Critic minor 2** — ADR-011's *Consequences* was the only place the
`type="module"` conversion of `index.html:131-132` was stated as a
consequence rather than as an edit — is accepted: Step 5.2 now carries the
bullet, so the edit appears in the step that performs it. **The two remaining
unscored open questions** are answered where they arise: the persisting-picker
contrast in Step 5.2's blockquote, and the document-order/`ThreadsView.init()`
question in §4.3's ADR-011 discussion.

### The Architect's five polish items

| # | Disposition |
|---|---|
| **N-1** | Step 5.3a item 3's heading now reads "Two colour values **are at issue**", and the item carries the machine inventory described under B-2 |
| **N-2** | `:11` → **`:10`**: the first stylesheet link on both todo pages is the Google-Fonts one, whose `rel="stylesheet"` trails its `href`. Conclusion unaffected |
| **N-3** | BX.10 part 2 gains `[ -s … ]` non-emptiness on both extracted blocks, so two files with sentinels around nothing no longer `diff` clean |
| **N-4** | BX.10 gains a **third** part: a grep for the bootstrap's storage-key **literal** across `web/ --include='*.html'`, requiring the same two-file set. This is the comment-stripped-paste detector; part 1 finds only sentinel-bearing copies |
| **N-5** | Not a plan defect but a real gap, and the user ruled on it — see ruling 4 |

### The seven user rulings, 2026-09-16

| # | Ruling | What it moved |
|---|---|---|
| **1 — Q9** | *"yes, the login page should use the theme previously selected."* | The C8 carve-out extends to **`/shared/dist/shared.mjs`**, and **B6.3 flips from a negative probe (expect 401) to a positive one (expect 200 + correct `Content-Type`)** — a criterion changing sign, which is why the census had to be re-derived rather than adjusted. §5 Step 8 is rewritten around **three GET-only path shapes — two exact (`/shared/dist/shared.css`, `/shared/dist/shared.mjs`) and one prefix-plus-suffix (`/shared/public/fonts/…/*.woff2`)** — with four bounding conditions; *(**v5 FINAL**, Critic minor 4: this row said "three **exact** … shapes", which contradicts Step 8.1's own table — shape 1 exact, **shape 2 prefix + suffix**, shape 3 exact — and would read as authorising an exact-match-only allowlist that the font shape cannot satisfy. The exactness that bounds the Q9 widening is **shape 3**'s, and that is what B6.4's `GET /shared/ts/theme.ts` → 401 probe polices.)* ledger row 20, B6.4 and B6.5 follow. Q9 is **moved into the Closed section** of `OPEN-QUESTIONS-ui-unification.md` (`:200-235`). **Styling the login page stays out of Phase 2** — §11 item 11 records the capability/appearance split explicitly |
| **2 — Q11** | *"accept for now; but the goal is that all the modules should adopt the same capability for look and feel… so if obsidianoid does this neat thing with shadow drop, probably want that same effect in all the modules."* | **D-1 stands at C5** as planned, and the rider is recorded as **program direction** in §11 item 4 — propagate a good effect once, in the shared layer, not per module. No Phase-2 scope change |
| **3 — Q12** | *"In order for todo to enjoy the bredth of theming options, that means we need color choices to extend each theme in a way that works for todo. I'm in favor of doing exactly that: extend the themes so that any theme can be used with any module."* | **Option B**, and it is the largest structural change in v5. `--color-surface-dynamic` lands as the **18th shared token** at **C1**, superseding §4.3's three rule-local `app.css` edits (option A). Consequences: new **§5 Step 1.5**; **G12** amended to a closed one-item enumeration; **B1.1** split into parts A/B/C; `EXPECTED_COLOR_DECLARATIONS` 136 → **144** (8×18); ledger row 14 **reversed**; Q2's answer in the open-questions document amended in place at `:91-125` with two of its premises corrected against the tree (**5** donors, not 3; **2** `app.css` references, not 1) and `--radius-xl` explicitly **not** swept along; new **B10.6** for the sampler's second copy of table T1. The **broader** mandate — every theme usable with every module — is Phase-3+ and is recorded in **§11 item 2 in the user's own words**, deliberately without widening Phase 2 |
| **4 — todo's `?v=19`** | *"yeah, it can land in the same commit.. that's fine. I'll need to visually validate each one anyway."* | Read as authority to **place** the bump, not to remove the mechanism: **C6b** bumps `css/todo.css?v=19` → **`?v=20`** on `index.html:13` **and** `compare.html:13`. Chasing it down found a **structural omission inherited from v1**: `web/todo/index.html` has **no `shared.css` link at all**, and §4.8 asserted it as already true while no step ever wrote it. C6b now writes the `<link>` immediately **before `index.html:8`** — a position forced by a three-way cascade (after `todo.css` it inverts precedence; after the `:8-10` font group it silently re-faces every glyph, because both sheets supply `Inter`, `todo.css:42` requests it, and `todo.css` has **zero** `@font-face`; `:11-12` are todo's own FontAwesome sheets and declare no `Inter` face — **v5 FINAL**, Architect A-1) — asserted by new **B4.13**, with `compare.html` added to C6b's touch list and **deviation-ledger row 21** recording both declarations |
| **5 — BX.10 byte-identity** | *"best effort. I have no illusions that there's going to be some modules that are a bit off.. and we'll correct them after changes are applied."* | BX.10 stays a hard byte-identity assertion — it is cheap and it is a *self*-consistency check within one module, not a cross-module appearance claim — and the ruling is recorded as the disposition for **appearance** deltas, which is where it bites: P5/P5a |
| **6 — P5a screenshots** | *"again, best effort. If I have to make corrections later, that's ok."* | §3's **P5a** and **P6** rows are re-stated as best-effort with the user's words attached, so a pixel delta the plan did not predict is a **finding to record**, not a boundary that blocks the sequence. The attribution windows in §5 Step 5.6 stay exact, because *those* are what make a recorded finding interpretable |
| **7 — artifact count** | *"as long as all modules can use all themes and we practice DRY as much as possible (best effort), I approve all 19+ artifacts…"* | The 16-artifact end state and the 18-name barrel are **pre-approved**; G15's single move (15 → 16 at C6a) and BX.2's 7/6 → 8/7 → 9/9 trajectory need no further ruling. Forward-referenced from **§9 Q13** |

### What this pass found in its own work

Recorded because a synthesis that only lists a reviewer's findings is
claiming its own edits were flawless, and four iterations have now shown that
no pass's edits are.

1. **§13(a6)'s first published script did not reproduce its own published
   output, and it failed in the one direction an audit clause must never
   fail — silently, by seeing less.** The exclusion regex was a bare keyword
   list, `(function|const|let|var|window\.${sym}|export function)`, which
   discarded **any** line beginning with a declaration keyword rather than
   only the owning definition. It suppressed `web/obsidianoid/js/app.ts:478`
   — one of **F-B's own three critical call sites** — and
   `web/todo/js/theme.js:13`, printing 34 where the text claimed 37. A
   clause introduced to catch F-B would have hidden F-B's evidence. Fixed by
   anchoring the exclusion to a real definition (the symbol name must follow
   the keyword); the published script now emits the published 13-row output
   totalling **36**. This is P-III's sharpest dual so far: not a gate that
   cannot fail, but **a gate that fails to see** — and the discipline it
   forces is the one written into (a6) itself: *run the published command,
   compare its output to the published number, and treat a disagreement as a
   defect in whichever of the two is wrong.*
2. **§13(d2) failed once, at C6b, and the failure was real.** `B4.13` reached
   §7's label and §6's cell but not §5's `**Acceptance — C6b:**` line. The
   clause printed `C6b label= 8 §6= 8 §5= 7 FAIL`, one line was added, and
   the re-run is the published output. The three-way comparison earned itself
   here: a two-way check against §6 alone would have been green.
3. **§13(a3)'s "4 + 1 + 2 = 7 mentions, verified by hand" had gone stale** —
   it is 6 + 3 + 15 = **24** after Q12's work. Replaced with a machine clause
   that asserts **section confinement** rather than a total, because a total
   needing a re-count every revision is precisely the hand-maintained integer
   this pass was told to remove.
4. **§13(e) omitted `Makefile`, which is in three Touches cells.** Found by
   *deriving* the clause instead of reading it — a correct C1 would have
   contradicted the plan's own scope statement on the first commit. The
   allowlist gained the entry **and** a derivation from §6, and the clause's
   false attribution ("Asserted by BX.5") was corrected: BX.5's command is
   scoped `-- web/` and could not have caught it. Carried as §11 item 16.
5. **§13(b) duplication 5's coverage claim needed checking, and the obvious
   answer was wrong.** `make web-verify` is `artifacts.mjs` with **no
   `--build`**, and that script's rebuild is optional, so a stale `bundle.js`
   committed beside a changed `main.ts` is tracked, clean and green. The
   honest coverage claim for emitted artifacts in this plan is
   **"committed-clean, not provably rebuilt"**; B10.6's greps are
   load-bearing because of it, and §11 item 15 carries the one-line recipe
   fix that would close the class.
6. **(a6)'s repo-wide pass over-reported `showToast` at 53 hits**, because
   certmachine owns the donor — and that over-report surfaced **§13(b)
   duplication 6**: from C2 onward two files export a `showToast` of the same
   name and signature, four certmachine modules keep importing the local one,
   and Phase 2 does not collapse it. The *work* was declared in §11 item 9
   since v1; what was missing was the declaration that **a duplication exists
   in the meantime**.
7. **§16's A1 row was annotated, not rewritten.** Q12 retired D-3 to a zero
   delta and added D-16/D-17, so v4's residue list is no longer live — but
   §15 and §16 are historical records of what those passes decided, and
   rewriting them to match later rulings would destroy the only account of
   how the plan got here. The row is labelled *historical* and the live fact
   is routed to §5 Step 5.6, which owns it.
8. **`type="module"` is load-bearing in a way v4 stated only as an ADR
   consequence.** Driver rule 10 — `@shared` is externalised, never inlined
   (`scripts/build-web.mjs:82-85`) — is what forces the conversion, and the
   forcing chain is now written where the edits are (Step 5.2) as well as
   where the decision is (ADR-011).

### Published §13 results at this revision

Every integer is this revision's own run. The census **moved**, as predicted
by the brief, and it moved for two reasons that were themselves derived
rather than assumed: five criteria were added (**B2.10**, **B2.11**,
**B2.12** from F-B; **B4.13** from ruling 4; **B10.6** from ruling 3), and
**BX.10** gained substitute evidence when N-3's and N-4's premises were
executed. The decomposition was produced by diffing (a2)'s parse of this
revision against its parse of the v4 snapshot — added five, removed none,
one changed from `(def, False)` to `(def, True)` — not by arithmetic on the
previous numbers.

| Clause | v4 | v5 | Why it moved |
|---|---|---|---|
| a1 paths cited / existing | 65 / 54 | **73 / 62** | Q9's carve-out, Q12's donors, the three comparator pages, C5's emitted mirrors |
| a2 criteria / executed / deferred / substitute | 77 / 6 / 71 / 28 | **82 / 6 / 76 / 31** | five new criteria; BX.10 gains a substitute; executed unmoved |
| a3 retired names | 7, by hand | **24, machine-classified, 0 outside the allowed sections** | the hand count was stale *and* the wrong shape |
| a4 part 1 range | 61 / 61 / 0 | **89 / 89 / 0 skipped / 0 out of range** | the same two additions that moved (a1) |
| a4 part 3 diff | 62, from an edit list | **39 of 39 correct, 0 unresolvable, 0 out of range, 0 defects** | first pass with a real pre-edit snapshot to diff |
| a5 pointers / census | 0 dangling; PASS | **0 dangling; PASS — 82/6/76/31 both sides** | §7's restatement re-derived, not patched |
| a6 deleted symbols | — | **13 symbols, 36 references** | new clause; see finding 1 above |
| d2 label ⇄ schedule | PASS, 131 pairs | **PASS, 9/9, 136 pairs** | five new criteria across C1/C5/C6b |
| (e) scope | declarative | **79 path tokens, 0 outside** | new derivation; failed on first run |

**Verdict this pass asks the reviewers to test, not to accept.** Every number
above is reproducible from the scripts printed in §13 against this file and
this tree, which is the only property that has held for four consecutive
iterations and the one this revision leans on hardest.

## §18 — v5 FINAL: iteration-5 amendment pass

**This section closes the loop.** Iteration 5 was declared the last, and both
iteration-5 reviews returned exit verdicts: the Architect **SOUND**, with
three mandatory one-line amendments and five polish notes; the Critic
**APPROVE WITH MANDATORY PRE-EXECUTION AMENDMENTS**, with one critical, six
majors, fifteen minors and three rows it approved unconditionally. Neither
verdict asks for another round, so no iteration 6 is convened. What follows is
the amendment pass itself: **the Planner combined both reviews into this one
document** — the reviewers are read-only by construction and never edit the
plan — and the pass was scoped as *surgical*: apply the named amendments,
sweep the named minors, introduce nothing else, and re-derive every citation
touched. Where a review's own premise proved false in the tree, the rebuttal
is recorded below with the command that settled it, because a silent
correction is indistinguishable from an unapplied amendment.

The previous four synthesis sections (§14, §15, §16, §17) map findings to
edits for iterations 1–4. This one does the same for iteration 5, and then
republishes the whole of §13 so the numbers in this document are this
revision's own.

### Critic — the critical finding and the six majors

| # | Finding | Where it landed |
|---|---|---|
| **1** | **CRITICAL.** `system` / `default` is a three-way contradiction: Step 3.1's resolution order plus §15 row 9's `no-preference` ruling make the `system` step always resolve, so step 4 `default` is unreachable — yet **B3.1** demanded "four unit cases each isolating one step" and **B2.11** part 3 required step 4 to be *reached* | **Accepted; §15 row 9's reading adopted, at five sites.** (a) **Step 3.1**'s order bullet now states that step 4 is a *typed-config floor, not a reachable branch*, asserted by construction. (b) **B3.1** reads "**three unit cases isolating steps 1–3, plus a construction assertion for step 4**", with a "Why three and not four" paragraph naming the three. (c) **B3.1**'s stub table no longer says `localStorage` is needed "for steps 1 and 4"; it says step **1** and `set()`'s write-back. (d) **B2.11 part 3** is replaced by a grep (see row 2). (e) The secondary half — `system` is not *selectable* — is recorded as **§10 ledger row 22** against `docs/FRD-ui-unification.md:254-255`, as **§11 item 17** (the four edits that would deliver it, and why they are gated), and by retitling **B3.4** |
| **2** | **B2.11 part 3 cannot fail**, and passing proves the opposite of its claim: `internal/obsidianoid/build.go:38-43` backfills an empty vault theme, so `serverDefault()` answers at step 2 and the stamp is right for an unrelated reason | **Accepted; the part is replaced, not deleted.** It is now `grep -c "default: 'obsidian'" web/obsidianoid/js/app.ts` = **1** — a construction assertion over the site **Step 5.2** writes. The criterion also now states **both** reasons the old form could not fail: the Go backfill, and (from row 1) step 4's unreachability — including the sharper point that post-C5 the backfill returns *the very string* the old part tried to prove |
| **3** | **Undeclared C6a→C6b coordinate shift in todo's `index.html`**; every C6b citation sits below the C6a insertion and is unqualified | **Accepted, with the arithmetic re-derived.** A coordinate-shift declaration now opens **Step 6.2**: Step 6.3 item 3 replaces one line with a six-line block, so the net insertion is **+5, not +6**; all `index.html:NN` citations in Step 6.2, Step 6.5 and §13(b) are **pre-C6a**; the five affected ranges are mapped explicitly; `:2`, `:8` and `:13` do not move; the binding rule is *(lines in) − 1*, re-derivable from `git show <C6a-sha> -- web/todo/index.html`; and every C6b target is addressed by `id` or function name, not by line. Separately, the `?v=19` → `?v=20` bullet now pins the edit order inside C6b: the `shared.css` insert lands first, so `todo.css` is at **`:14`** in `index.html` and stays at `:13` in `compare.html` |
| **4** | **§17's back-pointer sweep publishes a false result** — four of the six claimed pairs are one-legged | **Accepted in full; the four pairs are completed and the claim is corrected.** Four `Guaranteed by` lines added: **BX.2** is now named back by **Step 2.4**, **Step 3.3** and **Step 4.3**; **BX.11** and **B9.1** by **Step 1.4** and **Step 6.1**; **B2.7** gains the `Guaranteed by (step)` line it never had, naming **Step 5.0**. §17's paragraph now itemises which four were broken and how, and publishes the live counts; the plan's header carries the same correction in one sentence, since it made the same claim first |
| **5** | **Pre-mortem Scenario 3's pre-commitment is false**: no probe distinguishes the fonts allowlist's suffix match from a bare directory prefix, so an over-wide handler passes every criterion in §7 | **Accepted; the probe exists now.** **Step 8.3** gains probe **7**, `GET /shared/public/fonts/inter/OFL.txt` → **401**, and its "six paths" becomes **seven**; **B6.4** gains the same probe with its rationale; **B6.5** asserts all **seven**; **R15** in §8 is corrected from six probes to seven. A blockquote at Step 8.3 states the defect in the terms that make it memorable: the over-wide form passes every §7 criterion, and the pre-mortem's claim that the probe "is part of B6.4 from the start" was the plan asserting its own fix into existence |
| **6** | **Pre-mortem Scenario 2's "make it structural" claim is false**: B4.3 is one sentence with no ordering clause, so a lazily-built drawer passes it | **Accepted.** **B4.3** gains the clause that makes it structural: *"The assertion runs before any `open()` has been called on the instance, so a drawer built lazily on first open fails here."* A blockquote records that the words "eager" and "lazy" appeared nowhere in the plan outside the scenario that relied on them |
| **7** | **B2.8 is internally unsatisfiable on `app.css:202`**: part 2 called the line "byte-unchanged by C5" while parts 1 and 3 require the item-3 rename that edits it | **Accepted, and the false claim was at five sites, not the three reported.** The distinction the plan needed is between the **token name** `--color-surface-dynamic`, which Step 5.3 item 4 leaves alone, and the **line**, which item 3 rewrites — a zero *value* delta, not a zero *byte* delta. Corrected at **B2.8** part 2, the retired **D-3** row in Step 5.5, **§4.3**'s rename table, **Step 5.6**'s P5→P5a item 3, and **§10 ledger row 14**. `:455` is the only one of the three lines that is genuinely untouched |

### Critic — the three rows approved unconditionally, and the "What's missing" list

| Item | Where it landed |
|---|---|
| A deviation row for todo's `no-preference` polarity flip, qualifying Step 6.2a's "none behavioural" | **D-18** (C6a, behavioural) in a new "Two further todo D-rows" table in **Step 6.2a**; its closing sentence now reads "Nine todo D-rows in all…" and qualifies the "none behavioural" claim instead of contradicting it. **Step 6.6**'s C6a bullet now requires the OS preference be set **explicitly** during capture, so D-18 cannot confound "pixel-identical" |
| The FRD `system`-un-selectability row | **§10 ledger row 22**, plus §11 item 17 and B3.4's retitle (Critic row 1(e)) |
| §11 item 11 gains a sentence saying Q9's *observable* outcome lands in Phase 3 | Done: Phase 2 ships **B6.3**'s open door; no §5 step edits a login template; the appearance half is named as Phase 3 work |
| The sampler's default render becomes OS-dependent at C3 | **Step 3.5** gains a second sanctioned delta, stated as the one place the `system` step reaches a pixel in this phase, with `web/sampler/index.html:2` (no `data-theme`) as the reason and B10.5's stylesheet premise explicitly untouched |
| Step 1.3 never named clause 12's insertion coordinate | **Step 1.3** now pins the block **after clause 11**, above the PASS line and below every coordinate Step 1.5's table cites — which is what makes that table's uniform **+1** attributable to T1 alone |
| A one-time note that short-form citations are module-scoped | **§13(a4)**'s limits, as a third limit: fourteen modules under `web/` own an `index.html`, three are cited in short form, and the scoping rule is now written down rather than inferred. The note also records that this convention produced **22 false positives** in the Critic's first mechanical sweep |
| The eager-drawer and non-`.woff2` gaps | Critic rows 6 and 5 above |
| The coordinate-shift convention | Critic row 3 above |

### Critic — the fifteen minors

| # | Minor | Disposition |
|---|---|---|
| 1 | Option-A arithmetic residue: "five tokens … 34 references" | **Fixed at three sites** — Step 5.3 item 3 now reads **four** tokens / **32** references (15+8+6+3); Step 5.6's candidate-trigger table and the P5a residue note carried the same 34. The Critic named two of the three; the third was found by grepping the integer |
| 2 | "17 colour keys × 5 themes at `themes.css:61-164`" | **Fixed, with a tree-verified rebuttal on the post-C1 half** — today it is **17 × 5 = 85** declarations over `web/shared/css/themes.css:61-163` (`:163` is rose's `}`, `:164` is blank); post-C1 it is **18 × 5 = 90** over **`:63-170`**, not the `:63-171` the review proposed. Both derived from a built sheet, not from arithmetic. The same off-by-one stood at §13(a4)'s re-derivation table and §13(b)'s survivor row; both corrected |
| 3 | B6.5 says five probes, Step 8.3 has six | **Fixed and superseded** — B6.5 now says **seven**, because amendment 5 added probe 7 in the same pass |
| 4 | §17 says "three **exact** path shapes" | **Fixed** — three shapes, **two exact and one prefix + suffix**, matching Step 8.1's own table; the sentence now also names which exactness bounds the Q9 widening (shape 3's) |
| 5 | "`shared.css` `@import`s the self-hosted faces" | **Fixed** — the faces arrive **inlined**; `web/shared/css/index.css:5` is the importer, the descriptor is `scripts/descriptors.mjs:57-64`, and `grep -c '@import' web/shared/dist/shared.css` = **0** while `grep -c '@font-face'` = **15**. The conclusion survived; the mechanism was wrong |
| 6 | Google-Fonts group cited as `:8-12` | **Fixed at both sites** (§10 ledger row 21 and the §17 user-ruling row) → **`:8-10`**; `:11-12` are todo's two local FontAwesome sheets, which declare only `font-family:'FontAwesome'` and no Inter face. Same item as Architect **A-1** |
| 7 | §14's M3 row still carries the retracted pre-paint premise, unannotated, while §17 row 5 claims it was deleted "from all three places" | **Fixed on both sides** — §14's M3 row is labelled **Historical, and left as written**, in the same style §15 and §16 use, with the live statement routed to §17 row 5; and §17 row 5 now says the premise is deleted from **three live sites** and **annotated** at the one historical site, instead of "all three" |
| 8 | Pre-mortem Scenario 1's key arithmetic | **Fixed with the real shape published.** The two `forest` blocks each carry **17** colour declarations, and the sets **overlap rather than nest**: 12 names in common, five unique to each side. The rename maps four of five, leaving `--color-surface-dynamic` (no shared counterpart until C1) and `--color-primary-fg` (no obsidianoid counterpart) unpaired — hence **16 × 5 = 80** mapped pairs today and **17 × 5 = 85** from C1, which are exactly B2.7's two numbers |
| 9 | B2.7's "85 comparisons, *Executed*" is not reproducible today | **Fixed by qualifying the label, not by moving the number.** The tree yields **80** pairs today; the published **85** came from Step 1.5's sandbox and is live from C1. The two related §13 notes are qualified the same way: (a4)'s embedded scripts are published **indented** and must be de-indented before they run, and part 3 needs a `BASE` snapshot held outside the tree; (a6)'s "53 hits" aside is an observation, never a published command |
| 10 | §11 item 15 says "B10.6's **two** greps over `bundle.js`" | **Fixed → one.** Verified from the criterion: B10.6 runs exactly one `bundle.js` grep; its others target `themes.css` and `main.ts`. The item also gained Architect **N-4**'s point in the same edit |
| 11 | BX.5's headline says "the three named" while its command exempts four prefixes | **Fixed** — the headline now reads "nothing under `web/` changes outside the four exempted prefixes"; `web/shared/` is the fourth, and every commit in the phase touches it |
| 12 | B1.1 part C is deferred-runnable only | **Fixed with one sentence** — "Not runnable while the plan is a plan; runnable from C1 onward." The `<C1-sha>` placeholder is a literal by design and carries no *Executed* note; §13(a4) part 3's `BASE` has the same shape and the same note |
| 13 | The companion document's committed baseline is **160** lines, not 161 | **Fixed at both sites** (§2's P6 row and §17's scope-discipline paragraph) → **160 → 235**, re-derived by `git show HEAD:docs/OPEN-QUESTIONS-ui-unification.md \| wc -l`. The clause claimed to be grep-derived, so a one-unit error was its own kind of defect |
| 14 | Four one-token slips | **All four fixed.** todo's `shared.css` link is **Step 6.2**, not 6.3; "ships only in the barrel" is now dated **from C3 onward**, with the one surviving non-barrel copy named; `web/obsidianoid/js/app.ts` has **two** `\|\| 'dark'` tails (`:479`, `:494`), not three; ADR-011's Decision line says **`out`**, the descriptor field, citing `scripts/descriptors.mjs:83` and the schema comment at `:18` |
| 15 | Unchecked contingency: C6b's "position before `:8` is a non-event" holds only if `todo.css` uses no other weights and no italics | **Measured, and it does not hold.** `web/todo/css/todo.css` sets `font-weight` at eight sites — 400 × 1, 500 × 3, 600 × 4 — and declares no italics, so the Google link at `web/todo/index.html:10` keeps winning at exactly those three weights. But `<strong><b>` at `web/todo/js/todo-utils.js:950` asks for **700**, which no Google face supplies and which therefore re-faces to the self-hosted variable font. Recorded as **D-19** rather than argued away. This is also Architect **A-3** |

### Architect — three mandatory amendments and four polish notes

| # | Item | Disposition |
|---|---|---|
| **A-1** | Ledger row 21's Google-Fonts group is `:8-10`, not `:8-12` | **Accepted; fixed at both sites.** Same as Critic minor 6 |
| **A-2** | §3's G12 bullet claims §13(e) counts `themes.css` edits; it does not | **Accepted.** The bullet now cites **B1.1 part C** — the assertor that a second `themes.css` addition cannot pass as "the same carve" — and part C's *Guaranteed by* line records that it is what G12 now leans on |
| **A-3** | Step 6.2's option-3 font-weight residual must be a declared D-row, or B4.11 is unsatisfiable | **Accepted, and measured rather than assumed.** **D-19** (C6b, data-dependent) is added in Step 6.2a; **B4.11**'s headline is widened and it gains a third sub-assertion making D-19's *absence* also a pass; **Step 6.6**'s C6b bullet reads "exactly D-8…D-14, plus D-19 if and only if the fixture carries a periodic item that is due"; and **B4.13** gains a cross-reference saying line order cannot prevent the residual, because exact weight match beats closest-match regardless of declaration order |
| **N-1** | Step 1.5's seeded transcript, case 4, prints 11 clauses | **Accepted.** Case 4 is now explicitly a **plan-time** reading — the sandbox carries Step 1.5's two edits alone — and the committed C1 tree prints **12**, which is **B10.2**'s assertion. An executor comparing case 4 against a real C1 run is told to expect 12 with the rest of the line byte-identical |
| **N-2** | Step 1.5 over-states what needed carving: §6's C1 row already lists the gate script | **Accepted; the clause is dropped rather than repaired.** §6's C1 row does list `scripts/check-shared-css.mjs`, for Step 1.3's clause 12, and §6's ordering text carries no such rule. Only `themes.css` needed a carve, and G12 supplies it |
| **N-3** | `light`'s authored value is two units off its own stated rule | **Left as written, as the Architect proposed.** The cell is explicitly declared a judgement, the user validates all eight themes visually, and a change after visual review is a correction inside C1 — it moves no line count, no criterion and no commit boundary |
| **N-4** | Bundle freshness at C1 is covered for the Q12 edit only | **Accepted**, folded into §11 item 15's edit: the C1 bundle is machine-checked fresh **for the Q12 token and nothing else**, and the recipe fix that would close the class is named there |
| **N-5** | The 18th key's *name* is the part the Q12 ruling locks in | **Accepted.** §11 item 2 now carries `--color-surface-dynamic` as a Phase-3 **rename candidate**, with its price stated: renaming it re-opens `web/obsidianoid/css/app.css:202` and `:455` and revives the shape D-3 was retired from |

### Rebuttals — four, all tree-verified

Zero rebuttals were expected from this pass; four were produced, each because
the amendment's own premise did not survive the command that checks it. In
every case the *finding* was accepted and only its integer changed.

| Review's figure | Published figure | Command that settled it |
|---|---|---|
| post-C1 span `:63-171` (Critic minor 2) | **`:63-170`** | the post-C1 sheet was **built** in a sandbox and counted: 195 lines, 18 × 5 = 90 declarations over `:63-170` |
| FRD `:258` for the selectable-`system` item (Critic row 1) | **`docs/FRD-ui-unification.md:254-255`** | `:254-255` is item 4, the selectable-pseudo-theme sentence; `:258` sits inside item 5's swatch-border sentence |
| companion baseline 161 lines (Critic minor 13) | **160** | `git show HEAD:docs/OPEN-QUESTIONS-ui-unification.md \| wc -l` = 160, trailing newline present |
| C6a insertion **+6** (Critic row 3) | **+5** | a six-line block replacing one line is a net +5; the derivation is published with the range map, plus the fallback that every target is addressed by `id` |

Two further corrections were made that neither review named, on the
one-definition-per-fact rule: a **third** site of minor 1's "34" (Step 5.6's
candidate-trigger table), and **two further** sites of finding 7's false
"not edited at all" (Step 5.6's item 3 and §10 ledger row 14).

### §13 re-run at v5 FINAL

Every clause was re-extracted from this document and run against this tree at
the end of the pass. The extraction is mechanical, so it also re-confirms the
limit §13 declares about itself: the scripts are published **indented** inside
this document and must be de-indented before they run, and (a4) part 3 needs
its `BASE` snapshot — here the 7,548-line v5 DRAFT — held outside the tree.

Two of the numbers below are **self-referential**: (a4) part 1 counts
citations in a body that includes this section, and part 3 diffs a file that
contains the table publishing part 3's result. They were therefore re-run
after this section was written and after the table was filled in, and the
published values are the **fixed point** — the run that reproduces the numbers
already printed. That is the only honest way to publish an audit of a document
inside that document, and it is stated rather than left for a reviewer to
notice.

| Clause | v5 DRAFT | v5 FINAL | Why it moved |
|---|---|---|---|
| a1 paths cited / existing / declared-new / counterfactual | 73 / 62 / 9 / 2 | **77 / 66 / 9 / 2**, PHANTOM **none**, new-but-present **none** | four new paths, all existing: `tools/baseline-shots/README.md` and `shoot.js` (the operator side-car note), `web/shared/public/fonts/inter/OFL.txt` (probe 7), `web/todo/js/todo-utils.js` (D-19's consumer) |
| a2 criteria / executed / deferred / with-substitute | 82 / 6 / 76 / 31 | **82 / 6 / 76 / 31**, duplicate ids **none** | **unmoved, as the brief required** — this pass added no criterion. New ledger rows, new D-rows and B3.4's retitle do not enter the census |
| a3 retired names | 24 mentions, 0 outside allowed | **27 mentions, 0 outside allowed** | the three amendment notes that discuss the rename. One genuine regression was caught **by this clause and fixed**: amendment 7's first draft wrote the full retired token name inside §7, where the clause forbids it; the criterion now uses the bare stem, as B2.8 part 1's own grep pattern already did |
| a4 part 1 range | 89 / 89 / 0 skipped | **97 found / 97 checked / 0 skipped / 0 out of range** | eight new qualified citations, six from the amendments and two from this section |
| a4 part 3 diff | 39 of 39 | **917 added-or-changed lines, 18 distinct citations, 0 unresolvable, 0 out of range** | the pass is larger than v5 DRAFT's, and every citation it wrote resolves |
| a5 pointers / census | 0 dangling; PASS | **0 dangling; PASS — derived (82, 6, 76, 31) = stated (82, 6, 76, 31)** | §7's restatement still re-derives; no pointer this pass wrote dangles |
| a6 deleted symbols | 13 symbols, 36 references | **13 symbols, 36 references** | unmoved; no deletion roster changed |
| b one-definition | declared | **declared, and one row corrected** | §13(b)'s obsidianoid-`themes.css` survivor row carried minor 2's off-by-one |
| c / d artifact count and gate census | 15 → 16 at C6a; census by label | **unchanged** | no commit boundary and no artifact count moved in this pass |
| d2 label ⇄ schedule | PASS, 9/9, 136 pairs | **PASS, 9/9, 136 pairs, 0 unlabelled** | unmoved, and this is the mechanical confirmation that the census did not move: 11 / 17 / 17 / 20 / 26 / 19 / 8 / 8 / 10 across C1…C8, identical on all three sides |
| e scope | 79 path tokens, 0 outside | **79 path tokens, 0 outside the allowlist** | unmoved; no Touches cell changed |

**The census confirmation, stated as its own claim because the brief asked for
it.** §7 holds **82** criteria — 6 executed, 76 deferred, 31 of those carrying
substitute evidence — before and after this pass, with no duplicate ids. Three
independent derivations agree: (a2) parses §7 directly, (a5) compares that
parse against §7's own restatement, and (d2) reproduces the same 82 from the
label ⇄ schedule ⇄ acceptance triangle at all nine boundaries. Amendments that
retitle a criterion (**B3.4**), widen one (**B4.11**, **B6.5**), add a part to
one (**B2.11**, **B4.3**, **B6.4**) or add ledger and delta rows (**22**,
**23**, **24**; **D-18**, **D-19**) do not move it, and none was expected to.

### What changed for the executor

Everything below is a change to what an executor does or checks, as opposed to
a change in how the plan explains itself. Nothing else in this pass alters
execution.

1. **B2.11 part 3 is a different command.** It is now
   `grep -c "default: 'obsidian'" web/obsidianoid/js/app.ts` = **1**, asserted
   over the construction site Step 5.2 writes. The old form — stamp a
   themeless vault and expect the config floor to answer — must not be
   attempted; it cannot fail, and post-C5 the Go backfill returns the very
   string it tried to prove.
2. **B3.1 is three cases plus a construction assertion, not four cases.** Do
   not try to write a unit case in which the resolution order falls past the
   `system` step: `installFakeDom()` installs `matchMedia`, the plan forbids
   feature-detecting it, and step 4 is a typed-config floor. Step 4 is
   discharged by B2.11 part 3's grep.
3. **B3.4 asserts a different thing than its old title implied.** It is now
   "`system` is the implicit resolution step and its `change` listener is
   live". There is no UI state in which a user has "selected `system`" —
   `system` is in neither the roster constant nor `themes.list`, and storage
   rejects it on read-back. Test the listener, not a selection.
4. **The C6b coordinate convention.** Every `index.html:NN` citation in Step
   6.2, Step 6.5 and §13(b) is **pre-C6a**. C6a's net insertion is **+5** at
   one point in the file, and the declaration at the head of Step 6.2 maps
   each affected range. Prefer the `id` or function name over the line number,
   and re-derive the offset from
   `git show <C6a-sha> -- web/todo/index.html` rather than trusting +5 if the
   commit came out differently. Inside C6b the `shared.css` `<link>` insert
   lands **first**, which puts `todo.css` at `:14` for the cache-bust bump.
5. **Probe 7 exists.** `GET /shared/public/fonts/inter/OFL.txt` must return
   **401**. A handler that matches `/shared/public/` as a bare prefix passes
   every other criterion in §7 and fails only this one, so do not simplify the
   suffix check away.
6. **Two new delta rows are expected in pixel captures.** **D-18**: todo's
   first render under an OS `no-preference` setting flips `dark` → `light` at
   C6a, which is why Step 6.6 now requires the capture profile to set an OS
   preference **explicitly**. **D-19**: at C6b, `<strong><b>` text inside a
   due periodic item re-faces from a Google Inter weight to the self-hosted
   variable font, so it appears in the C6b diff **only if** the fixture
   carries a periodic item that is due — and B4.11's third sub-assertion
   accepts either outcome, provided the fixture state matches.
7. **B4.3 constrains construction order.** `HamburgerMenu` must build its
   drawer DOM eagerly in the constructor and only *animate* on open; B4.3's
   node-identity assertion runs before any `open()`, so a lazy implementation
   fails the unit suite at C4 rather than the browser at C6b.
8. **The working tree is dirty by design.** §2's P3 row publishes the exact
   `git status --short` this pass was run on: five of its seven entries are
   the user side-cars **BX.4** forbids in any commit. Stage only the paths in
   each commit's **Touches** cell — never `git add -A`, never `git commit -a`.

**Hand-off.** This plan is approved and complete at v5 FINAL. Execution begins
at **C1** via `/oh-my-claudecode:start-work`, on branch `ui-upgrade`, against
phase base SHA `b73d31c`. Nothing in this document has been implemented; no
source file outside this plan and `docs/OPEN-QUESTIONS-ui-unification.md` has
been touched by any planning pass, and nothing has been committed.
