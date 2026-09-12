# PLAN — Recipes Tab for the Grocery Module

**Status: pending approval**
**Mode:** RALPLAN consensus, **DELIBERATE** (high-risk) — **revision r5**.
**Consensus reached at r4:** the Architect returned **SOUND** and an independent Critic returned
**APPROVE**, each having verified its own prior blocking items against the tree rather than against
the revision log. r5 applies the four pre-execution corrections they raised (three of which both
found independently) plus one contradiction found in review of the r4 output itself. See the
r4 → r5 and r3b → r4 revision logs. Nothing was restructured at either revision.
**Source of requirements:** `docs/FRD-recipes-tab.md` (§3 decisions are LOCKED — not reopened here)
**Branch:** `grocerymod` — **no git operations are planned or authorized** (no commits, branches,
PRs, stashes, `git diff` gates, or any other `git` invocation anywhere in this plan)
**Date:** 2026-09-10

---

## 0. Scope fence — file allowlist (READ THIS FIRST)

This work runs alongside another active branch. To avoid clashing with it, the executor may create
or modify **only** these paths:

| Allowed |
|---|
| `internal/grocery/*.go` |
| `web/grocery/*` |
| `docs/*.md` |
| `.omc/plans/*.md` |

`.omc/plans/*.md` is untracked planning state (`.omc/` is the only entry in `git status` on this
branch and holds `plans/`, `sessions/`, `state/`, `project-memory.json`). It is listed explicitly
because §8 directs open questions there; without the entry the allowlist gate would fail on a
correct execution. It is **not** a licence to write source or config anywhere under `.omc/`.

**Explicitly off-limits — do not edit, even to "fix" something obviously wrong:**

`internal/platform/**` · `internal/todo/**` · `internal/obsidianoid/**` · `internal/slideshow/**` ·
`internal/menuserver/**` · `cmd/**` · `go.mod` · `go.sum` · `package.json` · `tsconfig.json` ·
`Makefile` · any file under `.github/`, `scripts/`, or the repo root (other than the
`.omc/plans/*.md` exception named above).

Reading and *running* off-limits files is fine (`make test` shells out to `go test -race ./...`;
`internal/platform/config/config_test.go` is run as a regression check and never edited). Editing is
not.

**Every task in §4 carries this as a done-when check, and it has a real mechanism** (r2 stated the
rule but gave the executor no way to run it, and `git status` is not available):

> *Done-when (allowlist).* At the **start** of the task:
> ```sh
> touch /tmp/plan-task-marker
> ```
> At the **end**, **both** commands:
> ```sh
> find internal cmd web docs -newer /tmp/plan-task-marker -type f -not -path '*/.git/*'
> find . -maxdepth 1 -type f -newer /tmp/plan-task-marker
> ```
> Every path printed must match `internal/grocery/*.go`, `web/grocery/*`, `docs/*.md`, or
> `.omc/plans/*.md`. Anything else means the task edited outside the fence: STOP and report rather
> than editing further. (The first `find` is scoped to `internal cmd web docs` deliberately — it
> catches the realistic slips, `internal/platform/**` and `cmd/**`, without walking build output.)
>
> **The second `find` is new in r4 (R4-18) and it closes a structural blind spot.** The scoped scan
> cannot see repo-root files — yet `go.mod`, `go.sum`, `package.json` and `Makefile` are **exactly**
> what §0 and §5.2 declare off-limits, so the single most consequential violation this fence exists
> to catch was the one violation it could not detect. `-maxdepth 1 -type f` prints nothing on a
> clean run and every root-level edit on a dirty one.
>
> **Known limit, stated rather than papered over:** `-newer` detects creation and modification, not
> **deletion**. A task that deletes an out-of-fence file passes both scans. Nothing in T1-T10 deletes
> a file outside `web/grocery/index.html`'s own duplicate-modal ranges (T5), so this is recorded as a
> boundary of the mechanism, not a gap to be plugged with a `git` command — §0 forbids that anyway.

### 0.1 Topics that are closed

- **No security work.** Auth, CORS, TLS, CSRF, rate limiting, request body-size caps, and header
  policy are out of scope by explicit direction. This plan proposes no changes to any of them and
  opens no section on them. (B17's `esc()` mandate in T9 is **not** a security task — it is ordinary
  output-encoding consistency with what `app.js` already does on every rendered string today, and it
  is required because unescaped `<` in a recipe name breaks the markup regardless of intent.)
- **No Go version or dependency work.** `go.mod` declares `go 1.26.2` and lists two direct
  dependencies. Leave all of it alone; a separate future branch owns that.
- **No git.** Not even read-only `git diff` as a verification gate (r1 had one; it is gone — see
  T6).

---

## Revision log (r4 → r5)

r4 was reviewed independently by Architect and Critic. **Both returned a passing verdict** — SOUND
and APPROVE respectively — and neither asked for another round. Four of the five items below are
pre-execution corrections they raised; **three of those four were found independently by both
reviewers**, which is the strongest convergence signal of the effort. The fifth was found while
verifying the r4 output and was missed by both.

Every item is docs-only. No task was added, removed or re-scoped; no §3 decision was reopened; no
gate design changed; no source file was touched.

| Item | Change | Why |
|---|---|---|
| **R5-1** | **Rebase 20 FRD citations by +4** — `FRD:445-446`→`449-450` (7 sites), `447-450`→`451-454` (5), `437-450`→`437-454` (1), `453`→`457` (1), `469`→`473` (3), `486`→`490` (2). | *Both reviewers.* r4's own AC-9.4 amendment inserted four lines at FRD:444-447 and shifted everything below it, but r4 did not rebase its own citations — while claiming at the top of its revision log that every citation had been re-verified. The sharpest instance: seven sites sent the executor to `FRD:445-446` to confirm **the chip is required**, and FRD:445-446 now lands *inside AC-9.4's struck-clause parenthetical* — text explaining that a requirement was **removed**. Citations at or below FRD:443 were verified unaffected. `FRD:438-444` at §6 F-3 is deliberately **not** rebased: it describes the pre-amendment state historically. |
| **R5-2** | **Delete the "clicking it still cycles state" sentence in T9** and replace it with the opposite. | *Found in verification of r4; missed by both reviewers.* T9 contained two sentences thirteen lines apart saying opposite things: one correctly warned that reaching `cycleState` on a chip click is the bug the arm ordering prevents, the other — a leftover from r3b, when the suffix was a passive `<span>` and cycling *was* correct — instructed that clicking it "still cycles state". R4-9's merge inverted the truth and this sentence was not updated. An executor reading it would skip the `.recipe-chip` arm and ship a chip that flips the row's needed/not-needed state on click. |
| **R5-3** | **The line-through does not inherit** — say so, and add `text-decoration: inherit;` to the `.recipe-chip` reset. | *Both reviewers.* r4 asserted the suffix "must inherit the line-through … so this works with no change to 641 — verify visually." False: R4-9 made the element a `<button>`, an atomic inline-level box, and text decorations are not propagated into atomic inlines. A completed owned row would strike `beef` and leave `(Chili)` unstruck. The plan now also names the trap: the natural fix — adding the class to `style.css:641` — is forbidden by T6's prefix hash *and* by the appended-rules rule, so an executor would hit a wrong UI and a gate refusing their fix. |
| **R5-4** | **Move the separator space outside the `<button>`**: emit it in the template and `trimStart()` the button's text. `recipeSuffix` is unchanged. | *Both reviewers.* `recipeSuffix` returns `" (Chili)"` with a leading space, and r4 interpolated the whole string inside the button. A `<button>` establishes its own inline formatting context, and collapsible white space at the start of one is removed — so the row rendered **`beef(Chili)`**, violating AC-9.1. No unit gate would have caught it: T10 asserts on the string, which was always correct; only the DOM was wrong. |
| **R5-5** | **Retarget the appended-rules rule from "every selector" to "the subject of every selector."** | *Both reviewers.* The strict reading forbade `.item-row.completed .recipe-chip { … }` — subject is a new class, re-declares nothing — which is a legitimate way to scope a new-family rule to an existing row state, and was the only in-fence remedy for R5-3 before `text-decoration: inherit` was chosen instead. |

> **What this round says about the process.** Three of the five items trace to a single r4 change —
> R4-9's `<span>`→`<button>` merge, itself the fix for an r3b blocker. That is the third consecutive
> round in which a fix introduced the next round's finding, and it is the empirical case for R4-24's
> gate-integrity discipline: every one of R5-2, R5-3 and R5-4 is invisible to the automated suite and
> visible within seconds of a human running the §5.3 steps against the tree.

## Revision log (r3b → r4)

r3b was reviewed independently by Architect and Critic. Both found the architecture sound; every
finding was localized. **Nothing was restructured, no §3 decision was reopened, and no source file
was touched** — §0, §0.2, both earlier revision logs, the RALPLAN-DR summary, the pre-mortem, the
four-level test plan, the T1-T10 decomposition, §6, the §7 ADR and §8 all stand. Every line and
citation below was re-verified against the tree before it was changed.

**The theme of this round: five of this plan's own gates failed on a correct implementation.** That
is a defect class, not five defects — see **R4-24**, which adds the rule that catches the next one.

**Blocking — both reviewers, convergent:**

| # | Change | Why |
|---|---|---|
| R4-1 | **The footer `<select>` clause is deleted, not implemented** (T9, §5.3 step 17, §6 F-4, §7 Consequences, FRD AC-9.4). `rebuildGroupSelect()` is now an explicit **do-not-touch**. There are **two** Unallocated-facing strings, not three. | `rebuildGroupSelect()` (`app.js:209-221`) excludes NO_GROUP **by design** — `app.js:212` is the comment `// Never offer NO_GROUP as an add target` and the loop at `:213` iterates `groups`, documented at `app.js:9` as `// real groups only; NO_GROUP is virtual`. `index.html:112` is an empty `<select>`. So r3b prescribed relabelling an option that has never existed, and the only way to discharge it was to **add** one — reversing a deliberate exclusion and letting users create items directly into Unallocated, which no AC requests. The `<select>` is the add-form's destination picker (`app.js:875`), not a move control; moves go through drag. **Product decision, made and closed: drop the clause.** |
| R4-2 | **T7 must stub `updateRecipeControlsDisabled()` as well as `renderRecipesTab()`**, with a matching grep gate. | T7's `setActiveTab` calls `updateRecipeControlsDisabled()` (plan:1242) whose only definition is in T8 (plan:1511). Every tab switch would throw `ReferenceError` *before* reaching `render()`, so T7's own done-when ("both tabs switch instantly") could not pass on either tab. R3-31 fixed one call site and missed this one. |

**Blocking — Architect:**

| # | Change | Why |
|---|---|---|
| R4-3 | **The init IIFE must call `setActiveTab(activeTab)`** after `await fetchItems()`, not merely seed the variable. §5.3 step 7 extended to check the header chrome. | Seeding `activeTab` makes `render()` dispatch correctly, but **nothing applies the tab chrome**: the header buttons carry no `hidden` attribute in markup (`index.html:38, 65, 74`) and every reconciliation lives in `setActiveTab`. Reloading onto Recipes therefore ships Grocery chrome — `#tab-grocery` still `.active`/`aria-selected="true"`, eye/reset/groups still visible. **AC-8.1 and AC-8.4 fail on the reload path**, and both their gates (steps 14, 15) exercise only the *click* path. |
| R4-4 | **The mirror-integrity gate normalizes leading whitespace** before comparing, and its two `import`s are pinned to **file top level** (T10). | `app.js` helpers sit at 2-space indent inside the IIFE (`app.js:141, 151, 162`); the `app.test.js` mirrors sit at column 0 (`app.test.js:22, 42, 50`). `Function.prototype.toString()` returns source verbatim, so `APP_SRC.includes(fn.toString())` is `false` for every multi-line helper. Reproduced empirically: dedenting `app.js`'s `esc()` and re-testing gives `includes → false` raw, `true` normalized. An `import` inside the `describe` callback is a `SyntaxError` that takes all 52 baseline tests with it. |
| R4-5 | **T8's `25 + N` census replaced with an itemized table in the T7 style; the hard number is 39.** T9 gains a census line it did not have. | `N` was defined as the mutation count, but plan:1463 specifies every mutation "in the style of `addItem`", and `addItem` (`app.js:226-243`) contains **two** `render()` lines (`:234`, `:240`). Seven mutations ⇒ 14, not 7. Worse, `25 + N` is **unfalsifiable**: any result satisfies it. The table, not the mutation count, is the gate. |

**Blocking — Critic:**

| # | Change | Why |
|---|---|---|
| R4-6 | **AC-8.8's gate moves from §5.1's T7 row to its T8 row**; T10's table reads "implemented T7 / gated at T8, step 19"; T7's AC line and done-when say so. | T7 ships `renderRecipesTab() { /* T8 */ }` (plan:1197), so `#recipes-container` is empty at T7 and step 19's "every card body collapses" has no card bodies. The gate failed on a correct implementation. |
| R4-7 | **"The three universal gates" is now defined**, once, immediately after §5.1's table. | The phrase is a done-when on **ten** occurrences across nine tasks (plan:637, 763, 799, 923, 999, 1075, 1340, 1524, 1596, 1699) and a case-insensitive search of all 2163 lines found **no definition**. Nine of ten tasks terminated in a clause the executor could not discharge. |
| R4-8 | **§5.3 step 9 rewritten self-contained** around a throwaway *Ghost* recipe and an id-specific assertion. | The old assertion `curl -s "$BASE/api/items" \| grep -c '"recipe_id"'   # → 0` reads **1** on a fully correct build: at step 9 two live recipes own items (Chili Verde, and Tacos' beef from step 4), step 10 requires both to survive, and `grep -c` counts matching **lines** while `/api/items` returns a single JSON line. The step also never named which recipe to delete, and step 10 foreclosed both candidates. |
| R4-9 | **The suffix *is* the chip — one element, not two** (T9, T6 CSS, §5.3 step 18). Plus a dangling-recipe guard and a reachable replacement for step 18's race clause. | r3b gave the chip an opening tag (plan:1564) and **never said what it displays**, while the suffix was separately rendered at plan:1537-1539; step 18 called it "the `(Tacos)` chip", which would render `beef (Chili) (Chili)`. One `<button class="recipe-chip item-recipe-suffix">` discharges AC-9.1 and AC-9.6 together. **Product decision, made and closed.** The race clause was unreproducible: `refreshAll()` updates `items` and `recipes` under one `Promise.all` + one render, so before the SSE tick the card exists and after it the chip is gone. |

**Advisable — all applied:** R4-10 (D-4's "if the chip is cut for time" rewritten — F-3 made it
required and AC-9.6 gates it) · R4-11 (D-6's "untested-by-AC" withdrawn, as it already was in T7) ·
R4-12 (T2's rollback test given a root-proof unwritable path) · R4-13 (the `DeleteRecipe` log line
and the `"log"` import both land in T3, so T2 compiles standalone) · R4-14
(`updateRecipeControlsDisabled` must not show the Recipes hint while the user is on Grocery) ·
R4-15 (`focusRecipeId = null` runs unconditionally; `setSelectionRange` guarded on a present draft) ·
R4-16 (step 19 names **Collapse all** and **Expand all** — they are two separate buttons,
`app.js:29-30` / `index.html:47, 55`; `setAllCollapsed`'s Recipes-branch iteration source named) ·
R4-17 (AC-9.7's static half turned into source-text assertions over the shipped `app.js`) ·
R4-18 (§0's fence scan extended to repo-root files, which it structurally could not see) ·
R4-19 (T6: re-declaring an existing selector in an appended rule is forbidden) ·
R4-20 (the three `rc` listeners bind **once at module scope**, never inside a render) ·
R4-21 (`Broker.Subscribe` is `broker.go:48`, `Unsubscribe` `:51`; `24-33` is `Notify()`) ·
R4-22 (`handleRevision` is `handler.go:315-321`, not 315-322) ·
R4-23 (collapsing a selection to a caret on focus restore is recorded as a decision).

**Synthesis:**

| # | Change | Why |
|---|---|---|
| R4-24 | **Gate integrity is now a universal done-when** (§4 preamble): every gate must be run once against the tree *as it stood at the start of the task* and the result recorded, before the task's own gates are treated as passing. A gate that fails before the work begins is wrong or drifted — fix the gate, never the implementation. | **No gate in this plan was itself checked.** Each was written once from an authoring-time measurement and then trusted; three review rounds have served as the gate-checker at one full cycle each, and this round alone found five gates that fail on correct work. Under this rule R4-4 surfaces in T10's first minute, R4-5 in T8's, R4-2 the instant T7's tab switch is tried, and R4-3's step-7 gap becomes visible the moment the executor states what "restores the tab" means. |

Nothing else in this document changed.

---

## Revision log (r3 → r3b)

A surgical pass on 2026-09-10, after the product owner resolved the six pending FRD amendments and
they were applied to `docs/FRD-recipes-tab.md`. **Nothing was restructured and no decision was
re-opened** — r3's architecture, option choices, pre-mortem, ADR and ten-task shape stand unchanged.
Exactly four changes:

| # | Change | Why |
|---|---|---|
| R3b-1 | **AC-9.6 assigned to T8.** New `revealRecipe(recipeId)`: `delete collapsedRecipes[id]` → `setActiveTab('recipes')` (which renders) → `CSS.escape`'d `querySelector` → `scrollIntoView({block:'nearest'})` behind a mandatory `if (card)` guard. **T9**'s chip arm collapses to a one-line call. T8's AC list gains AC-9.6; T9's gains AC-9.6 (call site) and AC-9.7. | F-3 was resolved in favour of the full-polish chip, so expand-and-scroll is **genuine new client work**, not a missing gate — r3's T8/T9 built the chip and the tab switch and implemented neither. It lands in T8 because `collapsedRecipes` does. The ordering is load-bearing: the card is not in the DOM until after the render, and a deleted recipe must be a clean no-op rather than a `null.scrollIntoView` throw inside a delegated listener. |
| R3b-2 | **Three gates added.** §5.3 e2e **step 18** (AC-9.6), **step 19** (AC-8.8), and **step 13 promoted** from an R-17 aside to AC-9.7's named two-site `Chili <b>` assertion, covering the Recipes-tab card title **and** the Grocery-tab suffix. T7 gains AC-8.8 and its `setAllCollapsed` bullet drops the now-false "untested-by-AC per D-6". | AC-8.8 and AC-9.7 were already specified as behavior; only the gates were missing. D-6 is precisely what F-9 closed, so the r3 parenthetical had become wrong. |
| R3b-3 | **T10's AC walk made dischargeable** — a three-row table mapping AC-8.8 / AC-9.6 / AC-9.7 to a numbered step, with the R-19 rationale for why none of the three gets a unit gate. §5.1's T7/T8/T9/T10 rows updated to match. | T10's own done-when requires every FRD §9 line to map to a named assertion or a numbered step. Without this it could not pass, which is a done-when that fails on a correct implementation. |
| R3b-4 | **Every FRD line citation in this document re-verified against the current file; 23 distinct citations (38 occurrences) were stale or short and are corrected.** | The FRD grew when the nine amendments landed and no citation was re-based. Both prior review rounds were burned by bad citations, so leaving them in place was not acceptable. The corrections: AC-8.4 424-426→**429-431**, AC-9.4 435-437→**441-443**, AC-8.8 434→**435**, AC-9.6 440-441→**445-446**, AC-9.7 442-445→**447-450**, AC-10.1 437→**453**, AC-7 417-418→**422-423**, AC-1.5 378→**383**, AC-2.6 386→**391**, AC-9 group 429-434→**437-450** and 430-434→**438-444**, §5 asymmetry note 212-215→**214-217**, §5.1 reset row 226→**230**, "empty name → 400" 218→**222**, §6.1.3 240-241→**244-245**, §6.5 274-276→**276-285** and 277-280→**278-281**, §7.1 table 294-302→**299-307**, §7.1 Collapse/Expand row 298→**303**, §7.3 330-331→**330-336**, §7.4.2 337-338→**342-343**, §10 mirroring convention 453→**469**, §11 drag bullet 474→**486**. Historical "was FRD:nnn" values in §6 are left as written, since they record the pre-amendment file. |

Nothing else in this document changed.

---

## Revision log (r2 → r3)

r2 was reviewed independently by Architect (**SOUND WITH CONCERNS**, 9 mandatory items) and Critic
(**ITERATE**, 6 required items). Both found the architecture sound and every remaining defect
localized to material r2 itself introduced. The r1 → r2 log is retired; what follows is r2 → r3.

**Convergent findings (both reviewers) — all applied:**

| # | Change | Why |
|---|---|---|
| R3-1 | **`fetchRecipes()` wrapper deleted** (T7, T8). R-3 retitled "recipe-fetch call sites enumerated". | r2 defined `fetchRecipes()` and then never called it: the three enumerated sites use `fetchRecipesData()` and `refreshAll()`. It was also a 24th `render()` line, so r2's `== 23` gate failed on a *correct* implementation — and the natural way out is to delete a legitimate `render()` call, which destroys Q2 Option B's entire zero-churn justification. |
| R3-2 | **`opts` dissolved. Per-card draft state moves to module scope** (`recipeDrafts`, `focusRecipeId`); `render()` keeps its zero-argument signature (T7, T8, PM-3). | r2 gated the focus guard on `opts.source === 'sse'`, but T8 specifies every recipe mutation "in the style of `addItem`", and `addItem` calls a bare `render()` (`app.js:234`). Adding an ingredient would wipe `#recipes-container` unguarded on the local path too — contradicting FRD §1.1. This also restores this plan's own Principle "module scope, never re-derived from the DOM", and makes the SSE and local paths identical. |
| R3-3 | **Gate arithmetic recomputed and itemized: post-T7 `grep -c 'render()'` = 25**, not 23 or 24. The obsolete `grep -c 'function render(opts'` gate is dropped. | A count with no itemization behind it cannot be audited, and both r2 and the reviewers got it wrong. See T7 for the full census. |
| R3-4 | **AC-8.1 and AC-8.4 given real checks**; `progressBarVisible(showProgress, itemCount, tab)` extracted from `renderProgressBar` (`app.js:583-584`) and mirrored/tested; `#progress-bar` removed from `tabControlVisible`'s table (T7, T10, §5.3). | AC-8.1 had no check at all. AC-8.4's only check was the `tabControlVisible` unit test that R-19 itself disqualifies as non-shipped — and worse, its table returned `true` for `#progress-bar` on Grocery while the shipped `setActiveTab` deliberately does not touch it (R-11). The test asserted the opposite of the design. |
| R3-5 | **`ErrInvalidName`'s rationale restated** around `PATCH /api/recipes/{id}` (T2, T4). | r2's justification was false: both `POST /api/recipes` and `POST /api/recipes/{id}/ingredients` route through `decodeName`, which trims and 400s at `handler.go:332-336` before any store call. The sentinel's only reachable path is PATCH with `{"name":"  "}` — a pointer struct that bypasses `decodeName` — and FRD:222 requires 400 there. The `ErrInvalidName → 400` mapper arm was completely untested; it now has the test that is its sole coverage. |
| R3-6 | **T6's eight CSS gates replaced with one**: `sed -n '1,1076p' web/grocery/style.css \| shasum -a 256` must still equal the recorded baseline, `wc -l` must be strictly greater than 1076. | `wc -l → 1076 + N` was self-fulfilling, and `grep -c` counts lines *containing* a string — so editing `style.css:507` in place to `.group-section, .recipe-card { … }` passed all seven selector gates while breaking the additive-only rule the gates existed to enforce. A prefix hash cannot be fooled by an in-place edit. |
| R3-7 | **The `.group-section` scoping claim removed and its reasoning rewritten** (T6). | The claim that drag queries are `gc`-scoped was false and its citations fabricated. The *directive* — recipe cards must not carry `class="group-section"` — is correct and now stated on true grounds: drag only runs on Grocery where `#recipes-container` is hidden, but `closest()` from `document.elementFromPoint` is not container-scoped, so the isolation is circumstantial and must not be leaned on. |
| R3-8 | **`loadConfigData()`'s split boundary made exact** (T7): `loadConfigData()` = fetch + field assignment only (`app.js:179-190`); `loadConfig()` = `loadConfigData(); rebuildGroupSelect(); renderProgressBar();`; `refreshAll()` calls `loadConfigData()` + `rebuildGroupSelect()` and lets `render()` own the progress bar. | r2 said "same split as `fetchItems`", but `fetchItems` ends in `render()` while `loadConfig` ends in `rebuildGroupSelect()` (`app.js:191`) + `renderProgressBar()` (`app.js:192`). The analogy was ambiguous in exactly the place that decides whether R-11 holds. |
| R3-9 | **`AddRecipe` uses `max(Order)+1`, not `len(s.recipes)`** (T2). | `len()` collides after any delete — two recipes end up with the same `Order` — and it was gratuitously inconsistent with the max-order idiom this same task mandates verbatim for `AddIngredient`. |
| R3-10 | **`buildRow`'s delete-button range corrected to `app.js:725-730`**, with "make conditional, do not delete" (T9). | :730 terminates the template literal and :731 is blank. Deleting 725-731 literally produces a syntax error. |
| R3-11 | **Ten citation errors corrected throughout** (see the table below). | Both reviewers independently verified them; I re-verified all ten against the tree and confirmed every one. |

**Architect-only, blocking — all applied:**

| # | Change | Why |
|---|---|---|
| R3-12 | **`Add` gets a rollback**: `if err := s.save(); err != nil { delete(s.items, item.ID); return nil, err }` (T1). | A regression **r2 introduced**. Today `Add` runs `os.MkdirAll` at `store.go:101` *before* constructing (104-112) and inserting (:113) the item, so a failure returns `nil, err` with the map untouched. Folding `MkdirAll` into `save()` (D-13) moves the failure *after* the insert, so `Add` would return `item, err` with the item live in `s.items` and absent from disk — PM-2c's exact mechanism, imported into the oldest code path in the store. The fold is still right; it just now carries its rollback. |
| R3-13 | **PATCH body-decode failure path specified**: mirror `handler.go:209-212` — 400 `"invalid body"` (T4). | Unspecified, a malformed body leaves both pointers nil and is indistinguishable from the legitimate neither-field no-op, so garbage would return 200. |
| R3-14 | **Missing imports named** (T1, T4): `model.go` imports only `"time"` (`model.go:3`) and needs `errors`; `handler.go` imports encoding/json, net/http, strings, broker, response (`handler.go:3-10`) and needs `errors` for the `errors.Is` mapper. | Both files fail to compile without them, and neither task said so. |
| R3-15 | **`activeTab` is seeded before `await loadConfig()` (`app.js:933`)**, not merely "before the first render" (T7). | `loadConfig` calls `renderProgressBar()` at `app.js:192`, which now consults `activeTab` via `progressBarVisible`. |

**Critic-only, blocking — all applied:**

| # | Change | Why |
|---|---|---|
| R3-16 | **The dispatcher is rewritten against cached element handles**; `rc` / `rEmptyEl` / `rHintEl` added alongside the existing handles at `app.js:20-31`, and `setHidden(el, hidden)` is defined (and marked NOT mirrored) (T7). | r2's dispatcher called `setHidden('#groups-container', …)`. **`setHidden` does not exist anywhere in `app.js`**, and the file's idiom is a cached handle plus `classList.toggle('hidden', cond)` (`app.js:640`). The dispatcher would have thrown `ReferenceError` on the first render. |
| R3-17 | **`.omc/plans/*.md` added to the §0 allowlist** (§0). | §8 directs open questions to `.omc/plans/open-questions.md`, which r2's own exhaustive allowlist forbade. The plan contradicted itself. |
| R3-18 | **Two empty-state risk gates added to T7**: with zero items *and* zero recipes, switching to Recipes hides `#empty-state` and shows `#recipes-empty-state`; switching back reverses it. | This is precisely the leak Q2 Option B names in its cons column, in the one state where both leaks are visible at once. |

**Advisables folded in (both reviewers):**

| # | Change |
|---|---|
| R3-19 | Mirror drift is now **enforced**, not conventional: a ~20-line `describe('mirror integrity')` block in `app.test.js` reads `app.js` with `readFileSync` + `import.meta.dirname` and asserts each mirrored helper's source text appears verbatim. Q3 is upgraded to "Option A **with** Option C's guarantee". |
| R3-20 | T2's rollback criterion is stated as **user-recoverability**, not record count, and anchored on `save()`'s `.tmp` + `os.Rename` (`store.go:281-286`) meaning the on-disk file is never torn. |
| R3-21 | `PatchRecipe`'s **validate-before-mutate** invariant stated explicitly (T2). |
| R3-22 | New test: `{}` PATCH on a **known** id returns **200** with both `recipe` and `items` (T4, §5.3). |
| R3-23 | T1's AC-1.3 fixture mutation named concretely: `Add("beef", "Produce")`. |
| R3-24 | Fourth T5 gate: `<div` count must equal `</div>` count (deleting 189-294 instead of 190-294 passes all three of r2's gates). |
| R3-25 | T8 requires delegated **`keydown`/Enter** and **`input`** handling, not just `click`. |
| R3-26 | §5.3's `-race` fork resolved to **one** addressee: a ~20-line concurrent store test (option (b)); removed from §8's open items. |
| R3-27 | §7 Consequences notes that `refreshAll()` on the sync-toggle path adds a config refetch today's `await fetchItems()` (`app.js:885`) does not. |
| R3-28 | `#recipes-offline-hint`'s position in the DOM specified (T5). |
| R3-29 | `DELETE /api/recipes/` (empty id) → `DeleteRecipe("")` → 404, versus `handleItem`'s explicit 400 (`handler.go:202-204`); recorded and resolved in T4. |
| R3-30 | ADR Consequences names two tensions honestly: additive-only CSS vs FRD §7.2's "styled consistently with `.group-section`", and the mirroring convention's four prior failures. |
| R3-31 | **T7 must define a `renderRecipesTab()` stub.** Not raised by either reviewer: r2's T7 builds a dispatcher that calls `renderRecipesTab()`, which T8 does not define until the next task, while T7's done-when already claims "both tabs switch instantly". T7 was not independently executable — a direct violation of this plan's own sequential-executability driver. |

**FRD amendments applied directly to `docs/FRD-recipes-tab.md`** (`docs/*.md` is in-allowlist and no
other branch owns the file). Exact lines changed:

| F | FRD location before | What changed | FRD location after |
|---|---|---|---|
| **F-4** | AC-9.4, **FRD:433** (one line) | Broadened from "the section heading" to every user-visible occurrence — Grocery section heading **and the Groups-modal hint** (`index.html:156`); stored value stays `"No Group"`, renaming it stays deferred to §11. **r4 (R4-1) struck the third item, "group `<select>` option": it is a defect, not a requirement.** The `<select>` has no NO_GROUP option to relabel — `rebuildGroupSelect()` (`app.js:209-221`) excludes it by design (`app.js:212` `// Never offer NO_GROUP as an add target`; `app.js:9` `// real groups only; NO_GROUP is virtual`), and `index.html:112` ships it empty. r3b broadened F-4 to a site that does not exist, and the only way to discharge it would have been to **add** the option — reversing a deliberate product exclusion. FRD:441-442 is amended to match. | **FRD:441-443** |
| **F-7** | §6.1.3, **FRD:244-245** | "editable by drag (mirrors the existing groups-modal drag implementation)" → "editable via per-card up/down controls; drag-reorder of recipe cards is deferred to §11", matching R-14. A companion bullet was added to §11. | **FRD:244-245**, plus new **FRD:490** |
| **F-8** | AC-8.4, **FRD:424** (one line) | Added: on the Grocery tab the progress meter's visibility continues to follow config (`progress` enabled **and** at least one item); the Recipes tab never shows it. Makes R-11 an acceptance criterion instead of a plan-local decision. | **FRD:429-431** |

Note: r2's §6 cited F-7's target as "FRD:242-243". That is §6.1.4 (Delete) — a citation error neither
reviewer caught. §6.1.3 is FRD:244-245, which is what was actually amended.

**F-1 and F-3 remain listed in §6, unapplied.** Both need a product-owner call (D-1's response-shape
asymmetry and D-4's chip-without-an-AC), and are surfaced at approval rather than decided here.

**Verified citation corrections (r2 → r3).** All ten reviewer-reported rows were re-checked against
the tree and all ten were correct:

| r2 said | Verified actual |
|---|---|
| `<main id="main">` at `index.html:95`, `#groups-container` :96, `#empty-state` :97 | `<main>` **:96**, `#groups-container` **:97**, `#empty-state` **:98**; :95 is the `<!-- ── List ── -->` comment |
| `reorderGroups(newOrder)` at `app.js:466` | **`app.js:467`**; :466 is `newOrder.splice(insertAt, 0, dragged)` |
| `esc(item.name)` at `app.js:718, 721, 725` | **718, 720, 725**; :721 is the `.state-badge` span, which has no `esc` |
| `removeGroup` "has no counterpart in `app.js` at all" | **`app.js:541` — `async function removeGroup(name)` exists.** A *worse* mismatch than r2 described: same name, different arity, an async network mutation versus `app.test.js:58`'s pure `removeGroup(groups, items, name)` |
| `Register` `handler.go:28-43` | **28-44** |
| `handleItem` DELETE branch `handler.go:220-226` | **221-227** |
| `BulkSync` existing-item branch `store.go:190-194` | **191-195** |
| `Add` `store.go:87-113` | **92-115** |
| `store.go` import block 3-11 | **3-12** |
| `buildRow` `app.js:702-735` | **702-734** |

**Six further citation errors neither reviewer caught, also corrected:**

| r2 said | Verified actual |
|---|---|
| `save()` is `store.go:270-288` (D-13, Principle 2) | **270-289** |
| `Add`'s max-order idiom loop is `store.go:91-97` (T2) | **95-100** |
| the pure-helper block is `app.js:111-169` | **111-165**; :166 is blank and 167-169 is the "API" comment box |
| the sync-toggle `change` handler is `app.js:881-887` | **881-892**; :887 is `} else {` |
| F-7 targets "§6.1.3 (FRD:242-243)" | §6.1.3 is **FRD:244-245**; 242-243 is §6.1.4 (Delete) |
| `List()` `:84`, `sortedUnsafe` `:220`, `Reorder` `:174`, `BulkSync` `:186` | **:85, :221, :175, :187** — each r2 citation pointed at the doc comment, not the `func` line |

### 0.2 Review items I did **not** apply, and why

**From the r1 review — all three since independently confirmed by both r2 reviewers.** Three
"citation fixes" in that review were themselves wrong; r1 was right. Kept here so the executor is
not sent to the wrong lines:

- **`Broker.Notify` is `internal/platform/broker/broker.go:24-33`, not `broker.go:26-35`.** `func (b
  *Broker) Notify()` opens at :24 and closes at :33. r1's `24-33` was correct. (The file also lives
  under `internal/platform/broker/`, not `internal/grocery/`; it is off-limits for editing.)
- **`.sync-toggle` rules are `style.css:461-486`, not `461-484`.** The block's last rule,
  `.sync-toggle input:checked + .toggle-track .toggle-thumb`, is at :486. r1's `461-486` was correct.
- **`esc()` is `app.js:151`, not `:150`.** Off by one.

**From the r2 review — three items adopted only in corrected form.** The directives are right; the
supporting detail is not, and the detail is what an executor follows.

- **The reviewers' post-T7 `render()` count of 24 is one short. The correct count is 25.** The
  convergent finding derives "22 baseline + 1 (`setActiveTab`) + 1 (definition) = 24". That omits
  `refreshAll()`, whose tail is a bare `render()` — a 25th matching line — and which both reviewers
  otherwise endorse. Full itemization is in T7; the gate asserts **25**. A gate built on the
  reviewers' arithmetic would fail on a correct implementation, which is the exact defect R3-1 was
  raised to fix.
- **The `.group-section` re-enumeration is itself incomplete.** The finding replaces r2's fabricated
  citations with "the real queries are `app.js:760` and `app.js:796`". It omits **`app.js:770-771`**
  — ``gc.querySelector(`.group-section[data-group="${CSS.escape(targetGroup)}"]`)`` — which is a
  third `.group-section` query and the *only* genuinely `gc`-scoped one in the file. The directive
  and the corrected reasoning stand; T6 carries all three sites.
- **Draft *value* is module state; *focus* is not.** The synthesis says to store `{ value, caret }`
  "rather than reading state back off `document.activeElement` at render time". That is right for
  the value — reading `activeElement` loses card A's text when the render was triggered by clicking
  ✕ on card B, because `activeElement` is then the button. But whether to focus at all cannot come
  from module state alone, or removing an ingredient from card B would *steal* focus into card A.
  T8 therefore keeps exactly one `activeElement` read, immediately before the wipe, purely to decide
  **whether** to restore; the value and caret come from `recipeDrafts`.

Everything else in both reviews was verified against the tree and applied.

---

## 1. Discrepancies found

Recorded rather than silently patched. Each needs an explicit call from the product owner or a
conscious choice by the executor; the recommended resolution is stated but is **not** a decision.

| # | Where | Finding | Recommendation |
|---|---|---|---|
| **D-1** | FRD §5 | `PATCH /api/recipes/{id}` returns `{recipe, items}` but `DELETE /api/recipes/{id}` returns `{items}` only. Asymmetric — after a delete the client must remove the recipe from local state itself, whereas after a patch it is handed back. | Implement the FRD shapes literally (that is what AC-2/AC-5 assert). Client compensates in T8. **FRD amendment listed in §6.** |
| **D-2** | FRD §5.1 vs §9 AC-7 | §5.1 says `POST /api/reset` "sets every recipe `enabled=false`", but AC-7 and the existing wire contract return a bare `[]Item`. `app.js:341` does `items = data`. If the executor "helpfully" changes the response to `{items, recipes}`, `items` becomes a non-array object and the very next `render()` throws inside `items.some(...)` at `groupsForRender`. **Silent client breakage, no test catches it.** | **Resolved:** keep the response a bare `[]Item`. Combined with D-3's resolution, `Reset()`'s signature is unchanged and `TestHandlerReset` (`handler_test.go:156`) is untouched. Recipe changes reach clients via the existing `Notify()` → SSE → `refreshAll()` path. |
| **D-3** | `internal/grocery/store.go:207` | `Reset()` returns `([]*Item, error)`. §5.1 requires it to also clear `Enabled` on every recipe. | **Resolved without a signature change** (r1 proposed one; dropped). Clear `r.Enabled` inside the existing `Reset()` body. The `[]*Recipe` return r1 wanted was never consumed by anything. Keeping `([]*Item, error)` means `handleReset` (`handler.go:300-312`) needs no edit at all and `TestHandlerReset` is untouched — which also resolves D-2. |
| **D-4** | FRD §7.4.1 vs §7.4.2 | §7.4.1 renders the recipe name as a text suffix; §7.4.2 *also* adds a clickable recipe chip. Partly redundant, and **§7.4.2 had no acceptance criterion** — AC-9 tested only the suffix. | **Resolved by F-3 (2026-09-10), and rewritten in r4 (R4-10).** The chip is **required** and is gated by **AC-9.6** (FRD:449-450), executed as §5.3 step 18. r3b left this cell reading "if the chip is cut for time, no AC regresses" — that is now false and it is the sentence an executor under time pressure would act on. **The chip may not be cut.** The redundancy the row noticed is also resolved, in r4, by **merging the two elements (R4-9): the suffix *is* the chip** — one `<button class="recipe-chip item-recipe-suffix">(Chili)</button>`, not a text suffix plus a separate chip rendering the same string twice. |
| **D-5** | `web/grocery/index.html:156` | §7.4.4/AC-9.4 relabel the `"No Group"` heading to "Unallocated", but the Groups modal hard-codes *"Removing a group moves its items to **No Group** until reassigned."* No AC covers this string, so the UI would ship with two different names for the same bucket. | Update the modal hint to "Unallocated" in T5. Stored value stays `"No Group"` (§3.2). **FRD amendment listed in §6.** |
| **D-6** | FRD §7.1 vs AC-8.4 | §7.1's table says Collapse/Expand-all is shown on both tabs and collapses recipe cards, but no AC covers recipe-card collapse. AC-8.4 enumerates only eye/reset/groups/progress as hidden. | **Closed by F-9 (2026-09-10); the recommendation is corrected in r4 (R4-11).** Recipe-card collapse is **no longer untested-by-AC** — F-9 added **AC-8.8** (FRD:435), implemented by T7's `setAllCollapsed` branch and gated by §5.3 **step 19** (run at T8, per R4-6, since T7 renders no card bodies). r3b withdrew the identical "untested-by-AC" parenthetical in T7 but left it standing here, so the two halves of the document disagreed about the one thing D-6 was recorded to track. Implement per §7.1 **and** gate it. |
| **D-7** | `internal/grocery/store.go:105` | **Code contradicts what AC-3.4 needs.** IDs are `fmt.Sprintf("%d", time.Now().UnixNano())`. AC-3.4 requires two recipes each adding "beef" to produce **distinct IDs**. Two `Add`/`AddIngredient` calls in a tight test loop can land on the same nanosecond on a coarse-clock platform; the second silently overwrites the first in `s.items` and the test flakes (or worse, passes locally and fails on the Pi). Pre-existing, but this feature makes it load-bearing. | Add an unexported `nextIDUnsafe()` that **increments the candidate** on collision (see T1 — it must not re-poll the clock under the write lock). |
| **D-8** | ~~`store.go:248-257`~~ | **r1 got this wrong; recorded so it is not re-discovered.** r1 claimed the legacy plain-array branch of `load()` (early return at `store.go:256`) leaves `s.recipes` nil. It does not matter: T1 initializes `recipes: make(map[string]*Recipe)` in `New` (`store.go:33`), exactly as `items` is today, so the map is non-nil on every path before `load()` runs. r1's prescribed fix — `append([]*Recipe{}, s.recipes...)` — **does not compile**, because `s.recipes` is a map and cannot be spread. `Groups()` can do this only because `s.groups` is a slice. | **No action.** The real requirement (never emit JSON `null` from `GET /api/recipes`) is met by `sortedRecipesUnsafe()` allocating with `make([]*Recipe, 0, len(s.recipes))` — see T1. **Do not add a parallel `recipes []*Recipe` field to `Store` to make r1's snippet compile**: that is the dual-source-of-truth bug FRD §4.1 explicitly forbids. |
| **D-9** | FRD §6.5 vs AC-10.1 | §6.5 lists the disabled controls as "enable switches, add, rename, delete, and reorder" — it does not mention the **shared footer form**, which on the Recipes tab creates recipes (§7.3). AC-10.1 says "all Recipes-tab mutation controls". Ambiguous. | Read AC-10.1 broadly: disable the footer submit too when `activeTab === 'recipes'` and sync is off. **FRD amendment listed in §6.** |
| **D-10** | FRD §10 (not a defect — recorded so it is not "fixed") | `package.json` has no `"type": "module"`, yet `web/grocery/app.test.js` uses `import` (`app.test.js:7-8`). **Verified working:** `node --test web/grocery/app.test.js` passes 52/52 via ESM syntax detection. | Do **not** add `"type": "module"`. It is off-limits per §0 anyway, and it would change module resolution for the esbuild-built obsidianoid/slideshow sources. |
| **D-11** | `internal/grocery/handler.go:15` | `Handler.groups` is a plain field mutated by `handleConfigGroupsAdd`/`Remove`/`Reorder` with no lock — a pre-existing data race under concurrent requests. Recipes live in the `Store` (mutex-guarded), so this feature does not worsen it. | Out of scope. Noted because `make test` runs `-race` and a future *concurrent* handler test would trip it. |
| **D-12** | `web/grocery/app.js:201` | **`syncToServer()` is dead code.** `grep -rn 'syncToServer\|setInterval' web/grocery/` returns only the definition. Nothing calls it; `syncIntervalSeconds` (`app.js:15`, set at `:182`) schedules nothing; the client never issues `POST /api/sync`. So `/api/sync` is currently reachable only by a manual or scripted POST. | Leave it. Do **not** delete it (out of scope) and do **not** wire it up (that is a behavior change no AC asks for). Its consequences for PM-1, Q4 and the e2e plan are corrected below. AC-6 hardening stays **mandatory** regardless — hardening a currently-unreachable endpoint before a future client reaches it is exactly the right order. |
| **D-13** | `store.go:92-115`, `store.go:270-289` | `os.MkdirAll(filepath.Dir(s.filePath))` lives in `Add` (`store.go:101`) and **nowhere else** — `save()` has none. On a fresh install whose first mutation is a recipe create, `save()` fails with `ENOENT`. | Move the `MkdirAll` from `Add` into the top of `save()` (T1). Contained, in-allowlist, and it makes every mutation path safe rather than one. |
| **D-14** | `handler.go:166, 218, 275, 295, 310` | Store methods return **live pointers** into `s.items`, which handlers `json.Marshal` *after* the lock is released. Pre-existing for `*Item`; this feature extends the same pattern to `*Recipe`. A concurrent mutation during marshalling is a data race. | **Defer — same treatment as D-11.** Do not fix here: deep-copying every return value is a store-wide refactor with no AC behind it. Recorded so the next `-race` failure is not a surprise. |

---

## 2. RALPLAN-DR summary

### 2.1 Principles

1. **The store is the only place that knows the rules.** Handlers translate HTTP; they never
   compute ingredient sets, cascade deletes, or toggle semantics. Everything the AC list calls
   "semantics" is testable without an `httptest.Server`.
2. **One mutation = one lock = one `save()` = one revision = one `Notify()`.** This is the
   invariant the existing store already holds (`store.go:270-289`) and the one AC-1.5 pins down.
   Any recipe operation that would need two `save()` calls is designed wrong — which is why
   `PatchRecipe` is one store method and not a composition of two (R-1).
3. **The server is authoritative for `recipe_id`.** No client input path — `BulkSync`, `Move`,
   `Reorder`, `Patch` — may ever write it. Ownership changes only via the recipe endpoints.
4. **Additive, not transformative, on the client.** `web/grocery/app.js` is a 938-line IIFE with
   **22** `render()` call sites. The plan adds a dispatcher and a second render path; it does not
   restructure, modularize, bundle, or TypeScript-ify anything (FRD §12 explicitly withholds
   authorization for a rewrite).
5. **Stay inside the fence.** §0's allowlist is a hard constraint, not a preference. A change that
   appears to need a file outside it is a signal to stop and report, not to widen the diff.

### 2.2 Decision drivers (top 3)

1. **Data-loss blast radius.** `DELETE /api/recipes/{id}` is the only endpoint in this codebase that
   destroys N records from one request, and `save()` is a full-file atomic rewrite — a wrong
   cascade is written to `grocery.json` immediately and irreversibly. This dominates sequencing:
   the cascade must be store-tested before an HTTP route can reach it.
2. **`app.js` render entanglement.** A second view sharing one 938-line IIFE, one `render()`
   symbol, and one SSE refresh path is where regressions in *existing* grocery behavior will come
   from — not from the Go side.
3. **Scope confinement + sequential executability.** Every task must compile, pass `make test`, be
   verifiable on its own, **and stay inside §0's allowlist**, so a failed task never leaves a
   half-migrated store, a broken page, or a diff that collides with the other branch.

### 2.3 Viable options for the genuinely open questions

FRD §3 is locked. These are the *how*, not the *what*.

---

#### Q1 — Route registration style for the seven new recipe endpoints

| | **Option A — old-style prefix + manual segment parsing** *(recommended)* | **Option B — Go 1.22+ method/wildcard patterns** |
|---|---|---|
| Shape | `mux.HandleFunc("/api/recipes", h.handleRecipes)` + `mux.HandleFunc("/api/recipes/", h.handleRecipePath)`, splitting the suffix on `/` | `mux.HandleFunc("POST /api/recipes/{id}/ingredients", …)` etc., with `r.PathValue("id")` |
| Pros | Identical to `handleItem` (`handler.go:200`) and to the other twelve routes in the **same file**; wrong-method responses stay in the module's `{"error":…}` envelope; one place to read the whole recipe URL grammar; **smallest possible diff to a file the other branch may also touch** | ~40 fewer lines; `r.PathValue("id")` removes hand-rolled parsing; `/api/recipes/reorder` vs `/api/recipes/{id}` precedence handled by the mux; matches the rest of the repo |
| Cons | Must explicitly match the literal `reorder` segment **before** treating segment 0 as an ID, or `POST /api/recipes/reorder` is dispatched as an unknown-recipe request | Mux-generated 405s carry an `Allow` header and an **empty body**, not the module's JSON envelope — a silent inconsistency no existing test would catch. Also invites the "while we're here, restyle the other twelve routes" churn that §0 forbids. |
| Verdict | **Chosen** — on **scope confinement and smallest diff** (driver 3) plus the envelope-consistency argument. | Legitimate. If ever chosen, add an explicit 405 test per route asserting body shape. |

> **Corrected fact (r1 was backwards).** r1 rejected Option B partly because it would be "the first
> new-style patterns in the repo, next to five old-style ones." That is false. `internal/todo/handler.go:28-39`,
> `internal/obsidianoid/handler.go:29-39`, `internal/slideshow/handler.go:29-42` and
> `internal/menuserver/handler.go:24-27` **all** use method/wildcard patterns with `PathValue`, and
> `go.mod` declares `go 1.26.2`. `internal/grocery/handler.go` is the **sole old-style holdout** in
> the repo. The verdict is unchanged, but it now rests on the right reasons: matching the file we are
> editing, keeping the diff minimal on a shared-risk file, and preserving the error envelope.
> Restyling grocery's routes to match the repo is a legitimate future cleanup — and explicitly
> **not** this branch's job.

#### Q2 — Client render-path split

| | **Option A — one parameterized `render(tab)`** | **Option B — `render()` becomes a thin dispatcher over `renderGroceryTab()` / `renderRecipesTab()`** *(recommended)* |
|---|---|---|
| Shape | Single function branching internally on `activeTab` | Rename the current `render()` body (`app.js:638-697`) to `renderGroceryTab()`; new `render()` is a short dispatcher on `activeTab` |
| Pros | One entry point; no risk of a caller invoking the wrong one | **Zero call-site churn** — all **22** existing `render()` calls in mutations, drag handlers and modals keep working untouched; the two views never share a `gc.innerHTML = ''`; matches the FRD §12 mitigation verbatim |
| Cons | The single function grows past 120 lines with two unrelated DOM trees interleaved; every existing `render()` call now runs a branch it does not care about | Four containers (`#groups-container`, `#empty-state`, `#recipes-container`, `#recipes-empty-state`) must all be hidden/shown correctly, or the grocery empty-state (`app.js:640`) leaks onto the Recipes tab. **Owned by T7's dispatcher bullet — see R-13.** |
| Verdict | Rejected — it maximizes exactly the entanglement driver 2 warns about. | **Chosen.** |

> **`render()` keeps its zero-argument signature.** r2 introduced `render(opts = {})` so the SSE
> path could pass `{source:'sse'}` and PM-3's focus guard could key off it. That is withdrawn
> (R3-2): every recipe mutation calls a bare `render()` "in the style of `addItem`" (`app.js:234`),
> so an SSE-only guard leaves the *local* path wiping `#recipes-container` on every mutation the
> user triggers by hand. PM-3's guard now lives in module-scope state (`recipeDrafts`,
> `focusRecipeId`), which makes the SSE and local paths identical, keeps all 22 existing bare calls
> valid **and unmodified** — Option B's whole win — and restores Principle 4's "module scope, never
> re-derived from the DOM".

#### Q3 — Keeping `app.test.js` mirrored helpers in sync with `app.js`

| | **Option A — keep manual inline mirroring** *(recommended, with C)* | **Option B — extract to a shared `helpers.js`** | **Option C — a mechanical drift check** |
|---|---|---|---|
| Pros | Zero new tooling; exactly what FRD §10 (FRD:473) mandates ("helpers are mirrored inline… kept in sync"); tests stay runnable with the single documented command; no change to how `app.js` loads | One source of truth; drift becomes structurally impossible | Catches drift mechanically without changing either file's structure |
| Cons | Drift is a discipline problem, caught only by review — **and it has already happened four times**, see the note below | Technically achievable without `type="module"` (a second plain `<script src="helpers.js">` exposing a global, or `node:vm` in the test), so r1's "structurally impossible" framing was **overstated**. The real costs stand: a second script tag and a load-order dependency in `index.html`, a second global on a one-script page, and a restructure FRD §10 does not ask for. | Needs no new tooling *if* it lives inside the test file rather than in a script the `Makefile` would have to invoke — and `Makefile` is off-limits (§0) |
| Verdict | **Chosen — Option A *with* Option C's guarantee.** | **Rejected** — viable but unauthorized restructuring, and §0 argues for the smallest diff. r1's "invalidated" label is downgraded to "rejected". | **Folded into A**, not deferred: a ~20-line `describe('mirror integrity')` block in `app.test.js` that reads `app.js` via `readFileSync` + `import.meta.dirname` and asserts each mirrored helper's source text appears verbatim. Runs under the existing `node --test` command; adds no file, no script tag, no `Makefile` hook. See T10. |

> **The mirror convention is already partly fictional — it has failed four times.**
> `app.test.js:50` (`applyReset`) has **no counterpart in `app.js` at all**. `app.test.js:58`'s
> `removeGroup(groups, items, name)` is worse than "missing": **`app.js:541` defines
> `async function removeGroup(name)`** — same name, different arity, and an async network mutation
> rather than a pure function, so the test asserts behavior the shipped symbol does not have. Two
> more have different signatures in the two files: `itemsForGroup(items, group)` in the test vs
> `itemsForGroup(group)` at `app.js:141`; `groupsForRender(groups, items)` in the test vs
> `groupsForRender()` at `app.js:162` (both read module-scope state in `app.js`). Consequence for
> this plan: **every new helper must be written closure-free with an identical explicit parameter
> list in both files**, so its mirror is genuinely exact. Existing mismatches are recorded, not
> fixed — that is churn in a file the other branch may touch.

#### Q4 — How to guard `BulkSync` (`store.go:187-204`)

| | **Option A — field-level allowlist inside the existing loop** *(recommended)* | **Option B — reject the whole request if any item is suspect** | **Option C — a separate sanitizing pass before the merge loop** |
|---|---|---|---|
| Shape | The existing "update" branch already copies only `State/Completed/Group/Order` — leave it and add a comment that `RecipeID` is deliberately absent; in the `else` (new-item) branch, `continue` when `inc.RecipeID != ""` | Scan first; `400` on any incoming item whose `RecipeID` differs from the stored one | Build a cleaned `[]*Item` first, then run the untouched merge loop over it |
| Pros | **Smallest diff — the decisive criterion under §0.** Satisfies AC-6.1/6.2/6.3 exactly; the update branch needs *zero* code change, only a comment; partial sync still succeeds | Loud failure, easy to reason about | Merge loop stays free of policy |
| Cons | Silent drop — a rejected new item vanishes with no client-visible signal (mitigated by the observability log line in §5.3) | Loses the "partial sync still succeeds" property. **Note: r1's stated invalidation was wrong** — see below | Extra allocation and a second pass for no behavioral gain; two places now encode the same policy |
| Verdict | **Chosen**, on smallest-diff grounds. | Rejected — not invalidated, just larger and less forgiving. | Rejected — strictly more code, identical behavior. |

> **Corrected rationale (r1 was wrong).** r1 marked Option B "**invalidating** — the user loses every
> offline grocery edit." That is false. When Sync is re-enabled, `syncTog`'s `change` handler
> (`app.js:881-892`) calls `await fetchItems()` at `:885`, which does `items = data || []` (`app.js:197`) — it
> **already discards local state wholesale**. And per D-12 the client never POSTs `/api/sync` at all,
> so no offline edit is uploaded today under any option. Option B loses nothing a user has. It is
> rejected purely because Option A is smaller and preserves partial-success semantics for a future
> client that *does* sync.

Note the existing `else` branch stores the *caller's pointer* (`s.items[inc.ID] = inc`). That
aliasing is pre-existing and out of scope (see D-14), but the guard must sit **before** that line.

#### Q5 — Sequencing store vs handler vs client work

| | **Option A — vertical slices (one AC group end-to-end at a time)** | **Option B — strict horizontal layers: store → handlers → markup → styles → client → tests** *(recommended)* |
|---|---|---|
| Pros | Each slice is demoable early | Cascade delete and toggle semantics are proven by `go test -race` before any HTTP route can invoke them (driver 1); the client is written once against a settled API instead of chasing a moving one; every task's verification is a command that already exists |
| Cons | Each slice re-opens `store.go`, `handler.go` and `app.js`, causing repeated churn in the same functions; a wrong `save()` in an early slice reaches disk before the store tests exist | **Nothing is demoable until T5.** And the real, unstated cost: **all manual-only client work (T7-T9) lands last, under maximum schedule pressure** — precisely the tasks with no automated gate. Mitigation: §5.3's e2e checklist is written up front, not improvised at T9. |
| Verdict | Rejected. | **Chosen.** |

*(r1 had these two Pros/Cons cells transposed — "nothing is demoable until T5" was printed as a cost
of vertical slicing. Corrected.)*

---

## 3. Pre-mortem — three concrete failure scenarios

> *It is three weeks later. This feature caused a problem. What happened?*

### PM-1 — A stale bulk sync resurrects ghost ingredients that cannot be deleted

**What happened.** Someone (or a future version of the client, or a `curl` in a shell-history
one-liner) POSTs a day-old snapshot of the item array to `/api/sync`. Meanwhile the laptop had
deleted the *Chili* recipe, cascading away its 8 ingredients. Those 8 items are no longer in
`s.items`, so `BulkSync` takes the `else` branch at `store.go:197` and **recreates them**,
`recipe_id` and all, pointing at a recipe that no longer exists. They render as `beef ()` or crash
the suffix lookup, and — because of the §3.5 guard — `DELETE /api/items/{id}` answers **409
Conflict** for every one of them. The user has eight permanently undeletable rows in the middle of a
shopping list, and the only fix is hand-editing `grocery.json` over SSH.

**Severity, corrected (R-5).** r1 headlined this as an offline-phone scenario driven by a periodic
`syncToServer()`. **That mechanism does not exist** — `syncToServer` (`app.js:201`) is dead code and
nothing schedules it (D-12). The realistic trigger today is a manual or scripted POST, or a future
client that wires sync up. So this is **hardening a currently-unreachable endpoint**, not a live
bug. It stays in the plan and the guard stays mandatory (AC-6 requires it, and the whole point is to
land the guard *before* anything reaches the endpoint), but it is no longer the headline risk.

**Guards in this plan.** AC-6.2's reject lands in **T3**, *before* the recipe routes exist in T4.
The store test must reconstruct this exact sequence — create ingredient → snapshot → `DeleteRecipe`
→ `BulkSync(snapshot)` → assert the item is **not** resurrected — rather than the easier "sync an
item with a bogus recipe_id" case. T9's client render must additionally tolerate a dangling
`recipe_id` (unknown recipe → render the clean name, no suffix, no crash) so a corrupted file is
survivable rather than fatal.

### PM-2 — Deleting one recipe deadlocks the server, or quietly eats a week of curation

**Variant (a) — the deadlock.** `DeleteRecipe` is written the obvious way: take `s.mu.Lock()`, find
the owned items, and call `s.Delete(id)` for each. `Delete` (`store.go:141`) takes `s.mu.Lock()`
itself. `sync.RWMutex` is **not reentrant**, so the first iteration blocks forever holding the write
lock. Every subsequent grocery request — including `GET /api/items` from both phones — blocks on
`RLock`. The whole module hangs. There is no ops tooling on a home LAN; the symptom is "the grocery
page spins forever" and the fix is a restart that loses nothing but takes an evening to diagnose. A
near-miss variant that *doesn't* deadlock — delegating to an unexported helper that still calls
`save()` per item — writes the file 8 times, bumps `Revision()` by 8, fires 8 SSE storms, and breaks
AC-1.5 silently.

**Variant (b) — the curation loss.** Over three weeks the user dragged Chili's ingredients out of
Unallocated into Produce, Meats and mid-store, and started treating "beef" as a general staple. They
then delete the Chili recipe to tidy up. §3.5 says the cascade is correct, and it is — but the
confirmation modal said only *"Delete Chili?"*. Eight curated rows vanish mid-trip.

**Variant (c) — the half-committed cascade (new in r2, R-15).** `DeleteRecipe` removes 8 items and
the recipe from the maps, then `save()` fails (disk full, permissions, the `os.Rename` at
`store.go:284` failing across a filesystem boundary). The handler returns 500 and the client shows
an error — but **memory and disk are now divergent**. The next unrelated mutation from any client
calls `save()`, which succeeds, and silently commits the deletion the user was told had failed.

**Guards in this plan.** T2 implements `DeleteRecipe` as a **single** lock/scan/delete/`save()`
with an explicit comment naming the reentrancy trap, and its test asserts
`rev(after) == rev(before) + 1` — the revision counter is what actually detects variant (a)'s
near-miss. For variant (c), `DeleteRecipe` (and **only** it) snapshots the removed `*Item`s and
`*Recipe` before mutating and **restores them into the maps under the same lock** if `save()`
returns non-nil. No second write; the one-save invariant holds. For variant (b), T8's modal text
must name the recipe **and** the exact count **and** state that items moved into other groups are
included (FRD §6.1.4 requires the count; the group caveat is this plan's addition).

### PM-3 — Every SSE tick wipes the Recipes tab out from under the user

**What happened.** Two phones are in the kitchen. Phone A is on the Recipes tab, halfway through
typing `kidney bea` into Chili's ingredient input. Phone B ticks a checkbox. `handleItem` calls
`h.broker.Notify()`; the broker fans out to **every** subscriber including Phone A
(`internal/platform/broker/broker.go:24-33`). Phone A's `message` handler (`app.js:908`) refetches
and re-renders, which does `container.innerHTML = ''`. The half-typed input is destroyed, focus is
lost, and the soft keyboard closes. Because `Notify()` fires on *every* mutation from *any* client,
this happens constantly during a two-person shop, and the Recipes tab becomes unusable for entry.

**Why it is new.** The existing grocery view has no persistent text input inside
`#groups-container` — the footer form lives outside it and survives `render()`. The Recipes tab
introduces one input *per card*, inside the wiped container.

**Why r1's "simpler and preferred" alternative does not work (R-6).** r1 offered, as an alternative
to focus capture, "skip the wipe when the SSE payload changed nothing the Recipes tab displays."
But PM-3's scenario **is a checkbox tick mutating `items`**, and `renderRecipesTab()` derives its
rows from `items` via `ingredientsForRecipe`. "Nothing changed" is false in exactly the case that
matters. Worse, offering two mechanisms with no mandate hands the executor a fork.

**Why r2's mechanism does not work either (R3-2).** r2 mandated `render(opts = {})` with the guard
gated on `opts.source === 'sse'`. But T8 specifies every recipe mutation "in the style of
`addItem`", and `addItem` calls a **bare** `render()` (`app.js:234`). So adding an ingredient —
the single most common Recipes-tab action — wipes `#recipes-container` with the guard switched off,
every time. The footer form survives that today only by accident: `newInput` is a stable element
outside `gc` and is explicitly re-focused at `app.js:878`. Nothing gives the per-card inputs that
protection. r2's guard therefore covered the *rarer* of the two paths and left the common one
broken, contradicting FRD §1.1.

**Guards in this plan — one mechanism, mandatory, path-independent:**
1. **`render()` takes no arguments.** `opts` is gone. All 22 existing bare `render()` calls are
   untouched (Q2 Option B's win is preserved intact) and no caller has to remember to opt in.
2. **Draft text lives in module scope, not in the DOM.** Two new module-scope bindings sit beside
   the existing `collapsedRecipes`:
   ```js
   let recipeDrafts  = {};    // recipeId -> { value, caret }
   let focusRecipeId = null;  // which card's input should hold focus after the next render
   ```
   `recipeDrafts` is maintained by a **delegated `input` listener on `#recipes-container`**, so it
   is current before any wipe, whoever triggered it. This is Principle 4's "module scope, never
   re-derived from the DOM" applied to the one piece of state r2 left in the DOM.
3. **`renderRecipesTab()` writes drafts back as `.value` properties**, never interpolated into
   `innerHTML` — the value is user text and must not go through the markup path at all (this is
   also why it needs no `esc()`).
4. **Focus is decided separately from value.** Exactly one read of `document.activeElement`, taken
   immediately **before** the wipe: if it is a `.recipe-ingredient-input`, its `data-recipe-id`
   seeds `focusRecipeId`. Focus is *not* module state — if it were, removing an ingredient from
   card B would steal focus into card A. `addIngredient` sets `focusRecipeId` explicitly on success
   so the input the user was typing in ends up **cleared and focused**, ready for the next
   ingredient.
5. The per-card ingredient input **must carry `data-recipe-id`** — without it there is nothing to
   key either the draft or the restore on. (r1 mandated the restore and omitted the attribute that
   makes it possible.)
6. The lookup after re-render uses
   ``rc.querySelector(`.recipe-ingredient-input[data-recipe-id="${CSS.escape(id)}"]`)``.
   **If the recipe was deleted by the other client, that returns `null` and the restore is a
   no-op** — the input is simply gone along with its card. That is correct and intended behavior;
   it is stated here so it is not mistaken for a bug. `deleteRecipe` also deletes the stale
   `recipeDrafts` key so the map cannot grow unboundedly.
7. New `refreshAll()` (T7): the SSE handler fires **one** `await Promise.all([...])` followed by
   **one** `render()`. r1's SSE path fired three unordered promises each triggering its own
   render — tripling PM-3's blast radius and opening a window in which `items` references a recipe
   already removed from `recipes`.

The e2e steps in §5.3 exercise exactly this with two browser windows; it is the one bug in this plan
that unit tests structurally cannot catch.

---

## 4. Task breakdown

Ten tasks, strictly sequential, layered store → handler → markup → styles → client → tests
(Q5 Option B). Every task ends green on `make test`.

**Legend:** `Dep` = must be complete first. `AC` = FRD §9 criteria satisfied.

**Applies to every task without exception:**
- *Done-when (allowlist):* files touched ⊆ `internal/grocery/*.go` ∪ `web/grocery/*` ∪ `docs/*.md`
  ∪ `.omc/plans/*.md`, **verified with the `touch`/`find -newer` mechanism in §0** rather than by
  recollection.
- *Done-when (no git):* no `git` command was run for any purpose, including verification.
- *Done-when (gate):* `make test` is green.
- **_Done-when (gate integrity)._ New in r4 (R4-24).** Before a task's own gates are treated as
  passing, each gate must have been **run once against the tree as it stood at the start of the
  task** and its result recorded. A gate that fails *before* the work begins is either measuring a
  drifted baseline or is simply wrong; in either case **fix the gate and record the correction — do
  NOT adjust the implementation to satisfy it.**

  > **Why this rule exists.** No gate in this plan is itself checked. Every one was written once from
  > a measurement taken at authoring time and then trusted, and three review rounds have functioned
  > as the gate-checker at a cost of one full cycle each. The r3b round alone found **five** gates
  > that fail on a correct implementation (R4-2, R4-4, R4-5, R4-6, R4-8). Under this rule R4-4
  > surfaces in T10's first minute, R4-5 in T8's, R4-2 the instant T7's tab switch is tried, and
  > R4-3's step-7 gap becomes visible the moment the executor has to state what "restores the tab"
  > actually means. A pre-run costs seconds; a review round costs a day.
  >
  > This applies to **every** gate form the plan uses: `grep -c` censuses, the T6 prefix hash, the
  > §0 allowlist scans, and the §5.3 manual steps (for a manual step, "run it first" means read it
  > against the pre-task tree and confirm it is *reachable* — a step that references markup or a
  > function the task has not built yet is a gate on the wrong task, which is precisely R4-6).

---

### T1 — Data model and persistence
**Files:** `internal/grocery/model.go`, `internal/grocery/store.go`, `internal/grocery/store_test.go`
**Dep:** none · **AC:** AC-1.1, AC-1.2, AC-1.3, AC-1.4

- `model.go`: add `RecipeID string \`json:"recipe_id,omitempty"\`` as the **last** field of `Item`
  (after `CreatedAt`). Add the `Recipe` struct exactly as FRD §4.1 specifies.
- `model.go`: **add `errors` to the import list.** It is currently `import "time"` on a single line
  (`model.go:3`) — a bare single import, not a block. It becomes:
  ```go
  import (
      "errors"
      "time"
  )
  ```
  Without this the four sentinels below do not compile, and r2 did not say so.
- `model.go`: add **four** sentinel errors (`errors.New`), used by every later task:
  `ErrRecipeNotFound`, `ErrDuplicateRecipe`, `ErrRecipeOwned`, **`ErrInvalidName`**. The existing
  store returns `fmt.Errorf("item not found: %s", id)` and `handleItem` maps *all* errors to 404;
  recipes need 400/404/409 discrimination, which requires `errors.Is`-able sentinels.
  **`ErrInvalidName`'s reachable path is `PATCH /api/recipes/{id}`, and only that (R3-5).** r2
  justified the sentinel with `POST /api/recipes/{id}/ingredients` receiving `{"name":"  "}`. That
  is wrong: both that route and `POST /api/recipes` read their body through `decodeName`, which
  trims and returns 400 `"name is required"` at `handler.go:332-336` before any store method is
  called — the store never sees the blank name. PATCH is different: its body is a pointer struct
  (`{name *string, enabled *bool}`) decoded directly, precisely so "field absent" and "field empty"
  stay distinguishable, so it **bypasses `decodeName` by design**. `PATCH {"name":"  "}` therefore
  reaches `PatchRecipe`, and FRD:222 requires 400 there. The sentinel is still not optional — the
  justification is. T2 and T4 carry the test that is this mapper arm's **sole** coverage.
- `store.go` `storeData` (lines 14-19): add `Recipes []*Recipe \`json:"recipes,omitempty"\``.
- `store.go` `Store` (lines 22-29): add `recipes map[string]*Recipe`. Initialize it in `New`
  (`store.go:33`) alongside `items`: `s := &Store{items: make(map[string]*Item), recipes:
  make(map[string]*Recipe), filePath: filePath}`. **This is what makes D-8 a non-issue** — the map is
  non-nil on every load path.
- `store.go` `load()` (line 260 region, the `storeData` branch): populate `s.recipes` from
  `sd.Recipes`. **Leave the legacy `[` branch (lines 247-257) completely untouched** — it correctly
  returns early, and per D-8 that is harmless.
- `store.go` `save()` (line 270): set `Recipes: s.sortedRecipesUnsafe()`.
- Add `sortedRecipesUnsafe()` mirroring `sortedUnsafe()` (`store.go:221`), allocating with
  `make([]*Recipe, 0, len(s.recipes))` so it is **never nil**; sort by `Order`, tie-break on
  `CreatedAt`.
- Add the exported accessor, mirroring `List()` (`store.go:85`) rather than `Groups()`:
  ```go
  // Recipes returns all recipes in stable sort order. Never nil.
  func (s *Store) Recipes() []*Recipe {
      s.mu.RLock()
      defer s.mu.RUnlock()
      return s.sortedRecipesUnsafe()
  }
  ```
  **Do not write `append([]*Recipe{}, s.recipes...)`** — `s.recipes` is a map and that does not
  compile (D-8). If you hit a compile error here, the fix is the `make(...)` in
  `sortedRecipesUnsafe`, **never** adding a parallel `recipes []*Recipe` field to `Store`.
- **D-13:** move `os.MkdirAll(filepath.Dir(s.filePath), 0755)` out of `Add` (`store.go:101-103`) and
  into the **top of `save()`** (`store.go:270`), returning its error. `save()` has none today, so a
  fresh install whose first mutation is `AddRecipe` fails with `ENOENT`.
- **`Add` must gain a rollback in the same edit (R3-12). This is not optional and r2 got it wrong.**
  `Add` does **not** keep working identically. Today the `MkdirAll` is at `store.go:101-103`, which
  is *before* the item is constructed (104-112) and inserted (:113), so a directory failure returns
  `nil, err` with `s.items` untouched. Moving it into `save()` moves the failure to **after** the
  insert, and `Add`'s tail is `return item, s.save()` (`store.go:114`) — so on failure it hands the
  caller a live `*Item` that is in memory, absent from disk, and will be silently committed by the
  next unrelated successful `save()`. That is PM-2c's exact mechanism, imported into the oldest
  write path in the store. Change the tail to:
  ```go
  s.items[item.ID] = item
  if err := s.save(); err != nil {
      delete(s.items, item.ID)
      return nil, err
  }
  return item, nil
  ```
  The fold is still correct — it makes every mutation path safe instead of one — it just has to
  carry its rollback with it. See T2 for which other mutations need this and which deliberately do
  not.
- Add `nextIDUnsafe() string` per D-7 and use it in `Add` (replacing `store.go:105`); T2's recipe and
  ingredient creators use it too. **It must increment, not re-poll the clock** — re-polling under
  `s.mu.Lock()` on a coarse-clock platform busy-waits while holding the write lock:
  ```go
  // nextIDUnsafe must be called with the write lock held.
  func (s *Store) nextIDUnsafe() string {
      n := time.Now().UnixNano()
      for {
          id := strconv.FormatInt(n, 10)
          if _, ok := s.items[id]; !ok {
              if _, ok := s.recipes[id]; !ok {
                  return id
              }
          }
          n++
      }
  }
  ```
  Note the collision check spans **both** maps — items and recipes share one ID namespace, and
  `nextIDUnsafe` is the only place that fact is enforced. Adds `strconv` to `store.go`'s imports.
- Tests, matching `store_test.go`'s hand-written style and its `newTempStore` helper:
  - back-compat load of an object file with no `recipes` key → `Recipes()` returns a non-nil,
    length-0 slice (AC-1.1);
  - legacy array still loads (extend/assert alongside `TestPersistence_LegacyArrayFormat`) (AC-1.2);
  - a recipe round-trips all five fields through `save()` → `grocery.New` (AC-1.4);
  - **AC-1.3, replacing r1's two contradictory manual gates (R-12):**
    ```go
    // TestPersistence_NoRecipeKeysForFreeItems
    // build a store from a fixture with no "recipes" key, run exactly one free-item mutation
    // — Add("beef", "Produce") — then read the file back off disk and assert:
    //   if strings.Contains(string(data), `"recipes"`)   { t.Error(...) }
    //   if strings.Contains(string(data), `"recipe_id"`) { t.Error(...) }
    ```
    Unambiguous, automated, needs no real `grocery.json`, and covers exactly what AC-1.3 asks.
    (`omitempty` on an empty non-nil `[]*Recipe` omits the key, so this holds.)

**Done when:** the above tests pass under `go test -race ./internal/grocery/`, plus the three
universal gates.

---

### T2 — Recipe and ingredient store mutations
**Files:** `internal/grocery/store.go`, `internal/grocery/store_test.go`
**Dep:** T1 · **AC:** AC-1.5 (revision half), AC-2.6 (store half), AC-3.1–3.5, AC-4.1–4.7, AC-5.3, AC-5.4, AC-5.5

Every method below: **one** `s.mu.Lock()`, **one** `s.save()` on the return path, **no calls to any
other locking `Store` method**.

- `AddRecipe(name string) (*Recipe, error)` — trim; **empty → `ErrInvalidName`** (handler maps to
  400); case-insensitive duplicate check via `strings.EqualFold` → `ErrDuplicateRecipe`;
  `Enabled: false`; **`Order = max(existing Order) + 1`**, using the same loop shape `Add` uses at
  `store.go:95-100` (scan `s.recipes`, `if r.Order >= maxOrder { maxOrder = r.Order + 1 }`); ID from
  `nextIDUnsafe()`.
  > **Not `len(s.recipes)` (R3-9).** r2 specified `len()`. Create three recipes (Orders 0,1,2),
  > delete the middle one, create a fourth: `len()` is 2, so the new recipe collides with the
  > existing Order-2 recipe and `sortedRecipesUnsafe`'s tie-break silently decides the display
  > order. It was also gratuitously inconsistent with the max-order idiom this same task mandates
  > verbatim for `AddIngredient` two bullets down.

- **`PatchRecipe(id string, name *string, enabled *bool) (*Recipe, []*Item, error)` — ONE method
  (R-1).** r1 told T4 to "apply rename and/or enable" by composing `RenameRecipe` +
  `SetRecipeEnabled`; that is two locks, two `save()`s, `Revision()` **+2**, and two `Notify()`s —
  violating Principle 2 and **failing AC-1.5** (FRD:383) on the one body shape FRD §5 (FRD:202)
  explicitly permits. Pointer-optional fields mirror `PatchPayload` at `store.go:118-121`.
  All four cases are specified:

  | `name` | `enabled` | Behavior |
  |---|---|---|
  | nil | nil | Existence check only. Return `(recipe, s.sortedUnsafe(), nil)`. **No mutation, no `save()`, no revision bump** — a no-op PATCH is not a mutation, so AC-1.5 must not see +1. Handler skips `Notify()`. |
  | set | nil | Trim; empty → `ErrInvalidName`; case-insensitive duplicate check **excluding self** → `ErrDuplicateRecipe`; set `r.Name`. **Must not touch any item** (AC-2.4). **Still returns the full item list** — the response shape is `{recipe, items}` for every PATCH per FRD §5, rename-only included. |
  | nil | set | FRD §6.3 verbatim: for every item with `RecipeID == id`, set `State` to `needed`/`not_needed` **and** `Completed = false` (assumption A-1). Set `r.Enabled`. |
  | set | set | Both of the above, then **one** `save()`. |

  Unknown `id` → `ErrRecipeNotFound` (404) **in all four cases, including neither-field** — there is
  currently no existence check on that path and one must be added.

  > **Validate before you mutate.** When both fields are set, run the trim, the empty check and the
  > duplicate-name check to completion **first**, and only then write `r.Name`, the item states and
  > `r.Enabled`. Written in the obvious order — rename, then toggle — a `ErrDuplicateRecipe` return
  > leaves `r.Enabled` and every owned item's `State` already mutated in memory with **no `save()`**
  > and **no revision bump**, so the store now disagrees with disk and nothing signals it. This is
  > the same class of bug as PM-2c, reached without any I/O failure at all.

  Items owned by other recipes and free items are untouched by construction: the loop condition
  `item.RecipeID == id` is the whole guard (AC-4.3, AC-4.4).

- `ReorderRecipes(ids []string) ([]*Recipe, error)` — mirror `Reorder` (`store.go:175`): set
  `Order = i` for each known id, ignore unknown ids, one `save()`.

- `DeleteRecipe(id string) ([]*Item, error)` — **the dangerous one (PM-2).** Under the single lock:
  1. verify the recipe exists → else `ErrRecipeNotFound`;
  2. collect the owned `*Item` pointers **and their IDs**, plus the `*Recipe` pointer, into local
     slices — this is the rollback snapshot;
  3. `delete(s.items, …)` each **inline**; `delete(s.recipes, id)`;
  4. **one** `save()`;
  5. **if `save()` returns non-nil, restore the snapshot into `s.items` / `s.recipes` under the same
     still-held lock and return the error** (PM-2c, R-15). No second write; the one-save invariant
     holds.

  > **Which mutations need a rollback, and why — the criterion is user-recoverability, not record
  > count (R3-20).** `save()` writes `s.filePath + ".tmp"` and then `os.Rename`s it into place
  > (`store.go:281-286`), so a failed save never leaves a *torn* file: disk still holds the last
  > good state. The damage is purely that memory has moved on, and the next successful `save()` from
  > any unrelated request commits it. So the question is not "how many records diverged" but **"can
  > the user get back to where they were once they see the error?"**
  > - **Rollback required** — `DeleteRecipe` (the data is gone; there is no UI to retype eight
  >   curated rows), and the three creators `Add`, `AddRecipe`, `AddIngredient` (the user is told
  >   the create failed, so a phantom record they believe does not exist will surface later, and
  >   they have no handle to delete it).
  > - **Rollback not required** — `PatchRecipe`, `Patch`, `Move`, `Reorder`, `Reset`, `SaveGroups`,
  >   `BulkSync`. Every one of these is a state the user can simply set again: they see the error,
  >   they toggle it again, and the retry converges. Adding rollback to them is unrequested churn in
  >   code shared with the other branch, for no recoverability gain.

  Add a comment stating (a) that calling `s.Delete` here **deadlocks** (`sync.RWMutex` is not
  reentrant) and (b) that a per-item `save()` breaks AC-1.5. Returns the surviving items.

- `AddIngredient(recipeID, name string) (*Item, error)` — `ErrRecipeNotFound` if unknown; trim,
  **empty → `ErrInvalidName`**; `Group: NoGroup`; `State` = `StateNeeded` if the recipe is enabled
  else `StateNotNeeded`; `RecipeID: recipeID`; `Completed: false`; ID from `nextIDUnsafe()`;
  `Order` computed with the **same max-order idiom as `Add` (`store.go:95-100`), scoped to `NoGroup`**.
  > That idiom (`maxOrder := 0; if item.Group == group && item.Order >= maxOrder { maxOrder =
  > item.Order + 1 }`) *looks* order-dependent under random map iteration but is in fact equivalent
  > to `max(Order)+1`. **Copy it verbatim; do not "improve" it** — a rewrite here is unrelated churn
  > in a shared file, and any behavior change would be invisible to the tests.

  Duplicate ingredient names are explicitly legal (§6.2.1).

- `DeleteIngredient(recipeID, itemID string) error` — `ErrRecipeNotFound` when the item is missing
  **or** when `item.RecipeID != recipeID` (AC-5.4 wants 404 for the mismatch, not 409). Delete
  inline under the single lock and `save()` once.
  > **Add an explicit comment: do NOT delegate to `s.Delete`.** After T3's guard, `s.Delete` returns
  > `ErrRecipeOwned` for **exactly** the items `DeleteIngredient` exists to remove — delegating is a
  > silent AC-5.3 failure that looks like a tidy refactor.

**Tests to write** (`store_test.go`, new `── Recipes ──` section):
duplicate name case-insensitive; empty name on `AddRecipe`, `AddIngredient` **and `PatchRecipe`** →
`ErrInvalidName` (the `PatchRecipe` case is the store half of the only HTTP path that can actually
reach this sentinel — see T4);
`PatchRecipe(id, &"Chili", &true)` where "chili" already exists → `ErrDuplicateRecipe` **and**
`r.Enabled` is still `false` and no owned item changed state (the validate-before-mutate invariant);
`Add`/`AddRecipe`/`AddIngredient` rollback: the create returns an error **and** `List()`/`Recipes()`
do not contain the record.
> **The unwritable path must be root-proof and container-proof (R4-12).** r3b said only "with the
> store's `filePath` pointed at an unwritable location", which an executor will implement as
> `chmod 0500` on a directory — **defeated by root**, which is exactly who tests run as inside most
> containers and CI images, so the test would silently pass by never failing the save. Specify the
> mechanism instead: create a **regular file** `F` inside `t.TempDir()` and set
> `filePath = F + "/grocery.json"`. Then `filepath.Dir(s.filePath)` is a *file*, `os.MkdirAll`
> returns `ENOTDIR`, and `save()` fails **deterministically for every user including root**, on
> every platform, with no permission bits involved.
> This also has the useful side effect of exercising **D-13's newly introduced failure point
> directly**: the `MkdirAll` that T1 moves out of `Add` and into the top of `save()` is precisely the
> call that returns `ENOTDIR` here, so the test hits the new code path rather than a generic write
> error further down.
**rename-to-an-existing-name → `ErrDuplicateRecipe`** (AC-2.2 covers create only — this is new in r2);
rename-to-own-name-different-case succeeds (self-exclusion); unknown-id on patch/delete;
enable then assert every owned item is `needed` + `!completed`; disable likewise;
**two-recipe isolation** (toggling A leaves B's items untouched); free-item isolation;
the beef(Chili)/beef(Tacos) scenario from AC-4.5; manual `Patch` to `not_needed` survives, then
re-enable restores `needed` (AC-4.6, AC-4.7); `AddIngredient` state depends on the recipe's
`Enabled`; **two recipes adding "beef" yield distinct IDs** (AC-3.4 — this is the test D-7 protects);
ingredient/recipe-mismatch delete → `ErrRecipeNotFound`; `DeleteRecipe` removes exactly the owned
items and leaves free items + other recipes' items.

**Plus, for AC-1.5 — the revision-delta suite:** capture `Revision()` before and after *each* recipe
mutation and assert a delta of exactly **1**, including:
- `DeleteRecipe` with **three** ingredients (delta 1, not 3 — the PM-2a near-miss canary);
- **`PatchRecipe` carrying BOTH `name` and `enabled` in one call (delta 1, not 2)** — this is the
  case r1's design failed and r1's test suite structurally could not catch, because it tested every
  mutation in isolation (R-1);
- `PatchRecipe` with **neither** field on a known id → delta **0**.

**Done when:** `go test -race ./internal/grocery/ -run 'Recipe|Ingredient' -v` is green, the
revision-delta test passes with both the 3-ingredient cascade and the both-fields patch, plus the
three universal gates.

---

### T3 — Store-level guards: delete refusal, BulkSync hardening, reset
**Files:** `internal/grocery/store.go`, `internal/grocery/store_test.go`
**Dep:** T2 · **AC:** AC-5.1 (store half), AC-5.2, AC-6.1, AC-6.2, AC-6.3, AC-7.1, AC-7.2

- `Delete` (`store.go:141`): after the existence check, `if item.RecipeID != "" { return
  ErrRecipeOwned }`. Free items keep the current behavior exactly.
- `BulkSync` (`store.go:187`): per Q4 Option A.
  - In the **existing-item** branch (`store.go:191-195`), change **no code** — add a comment that
    `RecipeID` is deliberately not copied because the server is authoritative (AC-6.1). AC-6.3
    already holds: `State/Completed/Group/Order` continue to merge.
  - In the **`else`** branch, **before** `s.items[inc.ID] = inc`:
    `if inc.RecipeID != "" { log.Printf("grocery: bulksync rejected new item id=%s recipe_id=%s", inc.ID, inc.RecipeID); continue }`
- `Reset` (`store.go:207`): additionally set `r.Enabled = false` for every recipe. **Keep the
  signature `([]*Item, error)`** — r1 proposed `([]*Item, []*Recipe, error)`; dropped per D-3. The
  `[]*Recipe` return was never consumed, so the change bought nothing and forced edits to
  `handleReset` and `TestHandlerReset` (`handler_test.go:156`).
- **Import note, corrected in r4 (R4-13):** `store.go`'s import block (`store.go:3-12`) currently has
  **no `log`**. Add `"log"` **here in T3**, together with **both** log lines — including the
  `grocery: recipe delete …` line inside `DeleteRecipe`, which T2 built.
  > r3b said `"log"` must be added in T3 "because T2's `DeleteRecipe` logs too" — but **T2's body
  > never instructs adding that log line**, and if it had, T2 would not compile: `log` would be used
  > with no import, or imported with no use. Either way T2 ends red, and this plan's driver 3
  > requires **every** task to end green and be independently executable. So: **T2 leaves
  > `DeleteRecipe` log-free** and compiles standalone; T3 adds the import and both call sites in one
  > edit. Adding the `log.Printf` to `DeleteRecipe` here does not disturb T2's revision-delta suite —
  > a log line changes neither `save()` count nor `Revision()`.
- Observability log lines (see §5.3) — exactly two, both on destructive or rejected paths, **both
  added in this task (R4-13)**:
  - `grocery: recipe delete id=%s name=%q items_deleted=%d` (added here, into T2's `DeleteRecipe`)
  - `grocery: bulksync rejected new item id=%s recipe_id=%s` (here)

  Format follows the repo's existing `"<module>: …"` convention (`internal/obsidianoid/build.go:43`).

**Tests:** delete-owned returns `ErrRecipeOwned` **and the item still exists** in `List()`;
delete-free still succeeds; the **PM-1 resurrection scenario** (add ingredient → snapshot the item
slice → `DeleteRecipe` → `BulkSync(snapshot)` → assert the item is absent from `List()`); mutated
`recipe_id` on an existing item is ignored while `state/completed/group/order` still merge (AC-6.1 +
AC-6.3 in one test); reset clears `completed`/`state` on owned items **and** disables every recipe.

**Done when:** `go test -race ./internal/grocery/ -run 'BulkSync|Delete|Reset' -v` is green;
deleting an owned item is impossible through the store API; plus the three universal gates.

---

### T4 — HTTP routes and status codes
**Files:** `internal/grocery/handler.go`, `internal/grocery/handler_test.go`
**Dep:** T3 · **AC:** AC-1.5 (Notify half), AC-2.1–2.6, AC-3.1–3.5 (HTTP), AC-5.1 (409), AC-5.3, AC-5.4, AC-5.5, AC-7.1, AC-7.2

- `Register` (`handler.go:28-44`): add two patterns, Q1 Option A, **after** the `/api/items` pair:
  `mux.HandleFunc("/api/recipes", h.handleRecipes)` and
  `mux.HandleFunc("/api/recipes/", h.handleRecipePath)`.
- `handleRecipes`:
  - `GET` → `response.WriteJSON(w, 200, h.store.Recipes())`.
  - `POST` → `decodeName(w, r)` (`handler.go:324-338` already handles decode + trim + empty → 400),
    then `AddRecipe`; `201` + recipe; `Notify()` after the write. `ErrDuplicateRecipe` → 409.
  - default → 405 via `response.WriteError`.
- `handleRecipePath`: `rest := strings.TrimPrefix(r.URL.Path, "/api/recipes/")`,
  `seg := strings.Split(rest, "/")`.
  **Match the literal `"reorder"` first** (Q1's stated trap), then:
  - `[id]` + `PATCH` → decode `struct { Name *string \`json:"name"\`; Enabled *bool
    \`json:"enabled"\` }`. **On a decode error, return 400 `"invalid body"` and stop**, mirroring
    `handleItem`'s PATCH branch verbatim (`handler.go:209-212`):
    ```go
    if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
        response.WriteError(w, http.StatusBadRequest, "invalid body")
        return
    }
    ```
    > **This is not optional (R3-13).** Both fields are pointers, so a failed decode leaves both
    > `nil` — byte-for-byte indistinguishable from the legitimate neither-field no-op. Without the
    > guard, `PATCH {"enabled": "yes"}` or `PATCH garbage` returns **200** with the unchanged recipe
    > and a full item list, telling the client the write succeeded.

    Then call **`PatchRecipe(id, body.Name, body.Enabled)`** (one call — R-1);
    respond `{"recipe":…, "items":…}` per FRD §5, **including for a rename-only patch and for a
    valid neither-field body**. `Notify()` only when at least one field was present.
  - `[id]` + `DELETE` → `DeleteRecipe`; respond `{"items": …}` (D-1: no `recipe` key).
  - `[id, "ingredients"]` + `POST` → **`decodeName(w, r)`** (explicitly: this branch uses
    `decodeName`, which is what makes `{"name":"  "}` a 400 rather than reaching the store), then
    `AddIngredient`; `201` + item.
  - `[id, "ingredients", itemID]` + `DELETE` → `DeleteIngredient`; `204`, no body.
  - anything else → 404 via `response.WriteError`.
  - **Empty id.** `DELETE /api/recipes/` trims to `rest == ""`, so `seg` is `[""]` and the code
    would call `DeleteRecipe("")` → `ErrRecipeNotFound` → **404**. `handleItem` returns an explicit
    **400 `"id required"`** for the analogous case (`handler.go:202-204`). Resolved deliberately:
    **keep the 404.** No AC covers it, both are defensible, and a 400 here would need an explicit
    empty-`seg[0]` check whose only effect is to disagree with the store's own answer. Recorded so
    it reads as a decision rather than an oversight (R3-29).
- **Import note:** `handler.go`'s import block is `handler.go:3-10` — encoding/json, net/http,
  strings, and the two internal packages. **Add `"errors"`**; the mapper below does not compile
  without it, and r2 did not say so (R3-14).
- **Error-mapping helper** (one function, used by every recipe branch):
  `errors.Is(err, ErrRecipeNotFound)` → **404**; `ErrDuplicateRecipe` → **409**;
  **`ErrInvalidName` → 400**; `ErrRecipeOwned` → **409**; else **500**.
  > *Corrected 2026-09-10: duplicate was specified as 400 here and at the `handleRecipes`
  > POST bullet, inherited from the same mistake in FRD §5. It is 409.*
  > **The `ErrInvalidName` arm's only reachable caller is the PATCH branch (R3-5).** r2 justified it
  > with the ingredient route, which is wrong — that route goes through `decodeName`, which already
  > 400s on a blank name at `handler.go:332-336` before `AddIngredient` is called, as does `POST
  > /api/recipes`. PATCH decodes its own pointer struct and bypasses `decodeName` by design, so
  > `PATCH /api/recipes/{id}` with `{"name":"  "}` is the **sole** path that reaches this arm, and
  > FRD:222 requires 400 for it. The test named below is therefore this arm's only coverage; without
  > it the arm ships untested and a regression to 500 would pass the whole suite.
- `handleItem`'s `DELETE` branch (`handler.go:221-227`): map `ErrRecipeOwned` → **409** with the
  message "item belongs to a recipe; delete it from the recipe instead"; everything else stays 404.
- `handleReset` (`handler.go:300-312`): **no change** — `Reset()`'s signature is unchanged (D-3) and
  it keeps writing the bare item array (D-2).
- Every mutating branch calls `h.broker.Notify()` **after** `response.WriteJSON`, matching every
  existing handler.

**Tests** (`handler_test.go`, reusing `newHarness` / `hh.do` / `decodeJSON`):
201 on create with `enabled:false`; 400 duplicate (case-insensitive); 400 empty/whitespace on
`POST /api/recipes` and `POST /api/recipes/{id}/ingredients` (these two assert `decodeName`'s
behavior, not the mapper's — see above);
**400 on `PATCH /api/recipes/{id}` with `{"name":"  "}` — the sole test of the mapper's
`ErrInvalidName → 400` arm; label it as such in a comment so it is not deleted as redundant with
the two `decodeName` cases**;
**400 `"invalid body"` on `PATCH /api/recipes/{id}` with a malformed body, asserting it is not
mistaken for a no-op 200** (R3-13);
**200 on a `{}` PATCH to a *known* id, with both a `recipe` object and an `items` array present in
the response** — the no-op path still owes the client the full FRD §5 shape, and pairing it with the
`want == 0` Notify assertion below is what proves "no-op" means no write rather than no response;
404 on unknown patch/delete;
404 on a **neither-field** patch to an unknown id; rename leaves items untouched **and still returns
an `items` array**; rename-to-duplicate → 400; reorder sets `Order`; ingredient create returns
`group == grocery.NoGroup`, `recipe_id` set, `name` with **no** suffix; 404 ingredient on unknown
recipe; **409 on `DELETE /api/items/{id}` for an owned item plus a follow-up `GET /api/items`
proving it survived**; 204 for a free item; ingredient delete removes it from `GET /api/items`; 404
on recipe/item mismatch; recipe delete leaves free items and the other recipe's items; 405 on a
wrong method for each of the two new patterns, asserting the `{"error":…}` envelope.

**Notify counting — AC-1.5's second half (R-10).** r1 declared "exactly one `Notify()`"
**unverifiable**, claiming "the broker channel is buffered at depth 1 and cannot count duplicates."
That premise is wrong: the depth-1 buffer is `notifyC`'s own choice at `handler_test.go:70`
(`ch := make(chan struct{}, 1)`), not the broker's. `Broker.Subscribe(ch chan struct{})`
(**`internal/platform/broker/broker.go:48`**; `Unsubscribe` is **`:51`**) takes a
**caller-supplied** channel, and `Notify()` performs one non-blocking send per subscriber
(`internal/platform/broker/broker.go:24-33`). A deeper channel therefore counts exactly.
> **Citation correction (R4-21).** `broker.go:24-33` is `Notify()` — correct where it stands above,
> and §0.2 already defends it against an earlier bad "fix". But r3b attached **no** citation to
> `Subscribe`, and §5.3 attached `24-33` to `Subscribe` outright. `Subscribe` is a one-line method at
> **:48** and `Unsubscribe` at **:51**. The argument was right; the pointer was on the wrong
> function. Both sites now carry the true line.

Note also
that the revision-delta assertions do **not** cover this half — a handler that calls `Notify()`
twice still leaves `Revision()` at +1.

Add, in `handler_test.go` (the broker package itself is off-limits per §0 — none of this needs it):
```go
// notifyCountC subscribes with a deep buffer, fires f, and asserts the exact
// number of Notify signals. Notify() is a non-blocking send per subscriber and
// handlers call it synchronously before returning, so len(ch) is deterministic
// once f() has returned.
func notifyCountC(t *testing.T, b *broker.Broker, label string, want int, f func()) {
    t.Helper()
    ch := make(chan struct{}, 8)
    b.Subscribe(ch)
    defer b.Unsubscribe(ch)
    f()
    if got := len(ch); got != want {
        t.Errorf("%s: got %d Notify signals, want %d", label, got, want)
    }
}
```
Apply `notifyCountC(..., 1, ...)` to **at least one recipe route per verb**: `POST /api/recipes`,
`PATCH /api/recipes/{id}` (both-fields body — the R-1 regression guard),
`DELETE /api/recipes/{id}`, `POST /api/recipes/{id}/ingredients`,
`DELETE /api/recipes/{id}/ingredients/{item_id}`, `POST /api/recipes/reorder`. Also assert
`want == 0` for a neither-field patch. Keep the existing `notifyC` for the eleven pre-existing call
sites (`handler_test.go:405, 413, 424, 435, 444, 451, 459, 467, 475, 484, 495`) — **eleven**, not the
six r1 claimed. Do not rewrite them.

**Done when:** `make test` is fully green — including every pre-existing handler test unchanged,
since `Reset()`'s signature no longer moves — plus the three universal gates.

> **No `curl` gates (R-19).** r1 gated T4 on `curl localhost:8080/...`. The port is
> user-configurable, r1 wrote it two different ways (`localhost:8080` and `localhost:PORT`), and no
> step specified how to obtain an owned item's id. Every one of those checks is already an
> `httptest`/`newHarness` test in the list above, which needs no running server.

---

### T5 — `index.html`: housekeeping, tab bar, recipes container
**Files:** `web/grocery/index.html`
**Dep:** T4 (not technically, but keeps the client tasks contiguous) · **AC:** AC-11.1, AC-11.2, plus the markup AC-8/AC-10 depend on

**Recorded baseline:** the file is **298 lines**. Six identical `<div id="title-modal">` blocks open
at lines **171, 192, 213, 234, 255, 276**.

- **Delete the five duplicate title-modal blocks — exact ranges (R-2).**
  - **Keep** lines **170-189**: line 170 is the `<!-- ══ Title edit modal ══ -->` comment; the first
    block runs **171 through 189** (its closing `</div>` is at 189, **not** 190).
  - **Delete** lines **190-294** inclusive (a blank line, then the five repeated comment+block
    pairs, ending at the sixth block's closing `</div>` at 294).
  - **Never touch lines 295-298**: 295 is blank, **296 is `<script src="app.js"></script>`**, 297 is
    `</body>`, 298 is `</html>`.

  > r1 said the duplicates span "171-296" and told the executor to keep "171-190". An agent following
  > that literally **deletes the page's only script tag** — and r1's sole gate,
  > `grep -c 'id="title-modal"' == 1`, still passes in that state. Hence the paired gate below.

  This also removes five duplicate copies each of `title-modal-heading`, `-form`, `-input`,
  `-error`, `-cancel`, `-save`.
- Add the two-tab control in `.app-header`, between the `.header-left` block (which closes at line
  **25**) and the progress bar (line **28**):
  ```html
  <nav class="tab-bar" role="tablist">
    <button id="tab-grocery" class="tab-btn active" role="tab" aria-selected="true">Grocery</button>
    <button id="tab-recipes" class="tab-btn"        role="tab" aria-selected="false">Recipes</button>
  </nav>
  ```
- **Corrected markup coordinates (R3-11).** r2 cited `<main id="main">` at 95, `#groups-container`
  at 96 and `#empty-state` at 97. Measured: line **95** is the `<!-- ── List ── -->` comment,
  `<main id="main">` opens at **96**, `#groups-container` is **97**, `#empty-state` opens at **98**
  and closes at **106**, and `</main>` is **107**. An executor inserting "after line 97" using r2's
  numbers lands *inside* `#empty-state`.
- Inside `<main id="main">`, add three siblings **after `#empty-state`'s closing `</div>` (line
  106) and before `</main>` (line 107)**, in this order:
  ```html
  <p   id="recipes-offline-hint"  class="offline-hint hidden">Recipe editing requires sync.</p>
  <div id="recipes-container"     class="hidden"></div>
  <div id="recipes-empty-state"   class="empty-state hidden">…</div>
  ```
  > **Order matters and r2 left it unspecified (R3-28).** The hint goes **first**, above the cards,
  > because it explains why every control below it is disabled — placing it last puts the
  > explanation below a scrollable list of recipes the user cannot interact with. It sits inside
  > `<main>` rather than in the header so it scrolls away once read, and it is a sibling of
  > `#recipes-container` rather than a child so `rc.innerHTML = ''` cannot destroy it.
  > All three start `hidden`; the dispatcher (T7) and the offline gate (T8) own their visibility
  > from the first render onward.
- **D-5:** change the Groups-modal hint at line **156** from `<strong>No Group</strong>` to
  `<strong>Unallocated</strong>`.

**Done when (T5-specific), all four gates required:**
```
grep -c 'id="title-modal"'          web/grocery/index.html   # → 1   (AC-11.1)
grep -c '<script src="app.js">'     web/grocery/index.html   # → 1   (R-2 guard)
grep -c 'id="title-modal-save"'     web/grocery/index.html   # → 1
grep -o '<div'  web/grocery/index.html | wc -l               # must EQUAL the next line
grep -o '</div>' web/grocery/index.html | wc -l
```
> **The fourth gate is new (R3-24) and it is the one that catches an off-by-one deletion.** Deleting
> **189**-294 instead of 190-294 removes the *first* block's closing `</div>` and passes all three of
> r2's greps — the surviving markup still contains exactly one `id="title-modal"`, one script tag and
> one `id="title-modal-save"`, but every element after the modal is now nested inside it and the
> whole page renders inside a hidden container. Assert the two counts are **equal to each other**,
> not to a fixed number: the baseline is 35/35, the deletion removes 15 matched pairs and the new
> markup adds 2, so any hard-coded expectation would be wrong.
Plus, manually: the page loads, clicking the title opens the modal, and saving persists the new
title across a reload (AC-11.2). Plus the three universal gates.

---

### T6 — `style.css`: tab bar, recipe cards, switch, suffix, chip
**Files:** `web/grocery/style.css`
**Dep:** T5 · **AC:** presentation support for AC-8, AC-9, AC-10 (no AC is gated on this task alone)

**Hard constraint — additive only.** Add **standalone new rules**. **Never** modify an existing
selector list (do not turn `.group-section { … }` into `.group-section, .recipe-card { … }`) and
never modify an existing declaration block. New rules may *duplicate* a few declarations from an
existing block; that is preferred over coupling.

**Recorded baseline (measured for r3):**

| | Value |
|---|---|
| `wc -l web/grocery/style.css` | **1076** |
| `sed -n '1,1076p' web/grocery/style.css \| shasum -a 256` | **`cdc3bb1f404164d8f87211a7e3e50b151447f79791bbf4fb53a3045c7ce6cea9`** |

> **Why r2's eight gates became one (R3-6).** r2 recorded seven `grep -c` selector counts plus
> `wc -l → 1076 + N`. The `wc` gate is self-fulfilling — any N satisfies it. The seven counts are
> worse than useless: `grep -c` counts **lines containing** a string, so the single edit the rule
> exists to forbid — changing `style.css:507` in place from `.group-section {` to
> `.group-section, .recipe-card {` — leaves all seven counts **identical** and sails through. A hash
> over the original 1076 lines cannot be fooled by an in-place edit, and it needs no table.

Rules to add:
- `.tab-bar` / `.tab-btn` / `.tab-btn.active`, reusing `--color-primary` and the `.header-icon-btn`
  sizing at line 432.
- `.recipe-card` / `.recipe-header` / `.recipe-body` / `.recipe-body.collapsed`, visually mirroring
  `.group-section` (507), `.group-header` (509-540, incl. the `.open` chevron rotation at 540),
  `.group-body` (562) and `.group-body.collapsed` (574) — **by duplicating the needed declarations
  into the new selectors, not by adding classes to the existing rules.**
  > **Recipe cards must not carry `class="group-section"` — and r2's reason for that was false
  > (R3-7).** r2 claimed "every `.group-section` query in `app.js` is scoped to `gc`
  > (`app.js:741, 743, 768, 770-771`)". Measured, :741 and :743 are
  > `gc.querySelectorAll('.drag-over-above,.drag-over-below')` and `('.drag-target')` — not
  > `.group-section` queries at all — and :768 is `gc.querySelector([data-id=…])`. The three real
  > `.group-section` sites are **`app.js:760`** and **`app.js:796`**, both
  > `el.closest('.group-section')` walking **up** from `document.elementFromPoint`, and
  > **`app.js:770-771`**, ``gc.querySelector(`.group-section[data-group="${CSS.escape(targetGroup)}"]`)``
  > — the only genuinely `gc`-scoped one of the three.
  >
  > `closest()` is not container-scoped: it walks ancestors from wherever the pointer happens to be.
  > What actually keeps drag off the Recipes tab is circumstance — drag only runs on the Grocery
  > tab, where `#recipes-container` is `hidden` and `elementFromPoint` cannot return anything inside
  > it. That is a **weaker** guarantee than r2 described, which makes the directive *more*
  > necessary, not less: the isolation is incidental and must not be leaned on. A few duplicated
  > declarations are cheaper than a coupling whose only defence is that one container currently
  > happens to be invisible.
- `.recipe-switch` reusing the `.sync-toggle` track/thumb pattern at **461-486** (FRD §7.2) — again
  as a new standalone rule set.
- `.recipe-card--enabled` — accent border or tinted header, following the `.group-header--nogroup`
  precedent at 550-560.
- **`.item-recipe-suffix` and `.recipe-chip` co-occur on ONE element (R4-9) — the two rule sets must
  not fight.** r4 merged the suffix and the chip into a single
  `<button class="recipe-chip item-recipe-suffix">(Chili)</button>` (T9), so both classes apply to
  the same `<button>` and the appended rules are read together, in source order, on one node.
  - `.item-recipe-suffix` — subdued (`--color-text-faint`). It must show the line-through when the
    row is `.item-row.completed`, and **this does not happen by itself (R5-3).** `style.css:641`
    (`.item-row.completed .item-name { text-decoration: line-through; … }`) decorates `.item-name`,
    and through r3b the suffix was a `<span>`, which inherited it. R4-9 made it a `<button>` — an
    **atomic inline-level box** — and text decorations are *not* propagated into atomic inlines, so
    a completed owned row would strike `beef` and leave `(Chili)` unstruck. The fix is one
    declaration on the new class, `text-decoration: inherit;`, listed in the `.recipe-chip` reset
    below.
    > **Do not fix this by editing `style.css:641`** to add `.item-recipe-suffix` to its selector
    > list. That is the natural remedy and it is forbidden twice over — by T6's `shasum` prefix gate
    > over lines 1-1076, and by the appended-rules rule below. The `text-decoration: inherit`
    > declaration is a bare new-class rule, which that rule plainly permits.
  - `.recipe-chip` — because the element is now a `<button>` rather than a `<span>`, it arrives
    carrying the UA's button defaults (grey background, border, padding, `font: 400 13.333px Arial`,
    `color: buttontext`). Left alone, `beef (Chili)` would render as a grey box in a different
    typeface mid-row. **The `.recipe-chip` rule must therefore reset them:**
    `background: none; border: 0; padding: 0; font: inherit; color: inherit; cursor: pointer;
    text-decoration: inherit;` (the last one per R5-3 — an atomic inline inherits no decoration)
    (add `-webkit-appearance: none` if the phone build needs it). `font: inherit` and
    `color: inherit` are what hand the styling back to `.item-recipe-suffix`, so **order the two
    rules with `.recipe-chip` first and `.item-recipe-suffix` second** — equal specificity, so
    source order decides, and the subdued colour must win over the reset's `inherit`.
  - The element still needs to be a real `<button>`, not a styled `<span>`: it is keyboard-focusable
    and Enter/Space-activatable for free, which a `<span>` would need `tabindex` and a `keydown` arm
    to fake. AC-9.6 says "clicking"; the free keyboard path costs nothing and is not a behaviour
    change anyone has to test.
- `.offline-hint`, `.recipe-ingredient-row` and its remove `✕` (following `.groups-modal-item` at
  823 / `.groups-modal-delete` at 835), `.recipe-ingredient-input`, and the up/down reorder buttons
  (R-14).

**Done when (T6-specific) — one gate, no git, per §0 (R-7):**
```sh
# 1. The original 1076 lines must be byte-identical.
sed -n '1,1076p' web/grocery/style.css | shasum -a 256
# → cdc3bb1f404164d8f87211a7e3e50b151447f79791bbf4fb53a3045c7ce6cea9

# 2. The file must have grown.
wc -l web/grocery/style.css        # → strictly greater than 1076
```
Both must hold, **and every new rule must be appended after original line 1076** — which is exactly
what the two together prove: the prefix is untouched and everything added is beyond it. An in-place
edit anywhere in 1-1076 changes the hash; appending nothing fails the line count. This replaces r2's
eight gates, none of which could detect the one edit they were written to forbid.

**Third rule — cascade non-interference, new in r4 (R4-19).** The two gates above prove **prefix
immutability**. They do **not** prove that the appended block leaves the existing cascade alone: an
appended rule that *re-declares an existing selector* — `.group-section { padding: 0 }` at line 1200,
say — passes the hash (the prefix is byte-identical) and passes `wc -l` (the file grew), while
silently overriding the original rule by source order. That is the same class of defeat R3-6 caught
in r2's greps, relocated from editing to appending.

> **Appended rules may declare only new selectors.** The **subject** of every selector in the
> appended block — its rightmost compound — must be one of `.recipe-*`, `.tab-*`,
> `.item-recipe-suffix`, or `.offline-hint`, the four families this plan introduces. **Retargeted
> from "every selector" to "the subject of every selector" in r5 (R5-5):** the strict reading
> forbade `.item-row.completed .recipe-chip { … }`, whose subject is a new class and which
> re-declares nothing, and that construction is a legitimate way to scope a new-family rule to an
> existing row state. A compound or descendant selector is permitted when its subject is one of the
> four families and no simple selector anywhere in it is *re-declared*. **Re-declaring any selector that already appears in lines 1-1076 is forbidden**,
> including inside a grouped selector list (`.group-section, .recipe-card { … }` is a violation) and
> including bare element selectors (`button`, `input`, `:root`). If a shared value is genuinely
> needed, **duplicate the declaration into a `.recipe-*` rule** — that is the drift liability §7's
> Consequences already books on credit, and it is the price the fence charges. Do not reach for a
> `:root` custom property here; defining one is fine, but *reading* it from the original rules would
> require editing them. See §7 Follow-up 1.
>
> Checkable by eye in one pass over the appended block, since the block is the only thing that grew.
> Under R4-24 this is one of the gates to run **before** appending anything, against the untouched
> file: it must print a clean bill on the pre-task tree, or the baseline itself has drifted.

Plus: both tabs render without layout breakage at phone width and desktop. Plus the three universal
gates.

> r1 gated this task on `git diff --stat web/grocery/style.css`, violating this plan's own line 6 and
> the standing no-git constraint. Removed.

---

### T7 — `app.js`: pure helpers, `activeTab` state, dispatcher, tab-scoped header
**Files:** `web/grocery/app.js`
**Dep:** T6 · **AC:** AC-8.1, AC-8.2, AC-8.3, AC-8.4, **AC-8.8 (implemented here, gated at T8 — R4-6)**

**Recorded baseline:** `app.js` is 938 lines. `grep -n 'render()' web/grocery/app.js` matches **23
lines**: 198, 203, 234, 240, 249, 257, 263, 275, 284, 293, 324, 338, 341, 513, 520, 529, 536, 547,
556, **638 (the definition)**, 673, 767, 818. That is **22 call sites**, not the "~40" r1 asserted
throughout (R-8). One of the 22 (line 203) sits inside the dead `syncToServer` (D-12).

**Post-T7 target: `grep -c 'render()' web/grocery/app.js` → 25.** Itemized, because an unitemized
count is exactly how r2 and both r2 reviewers each arrived at a different wrong number:

| | Lines | Note |
|---|---|---|
| Existing call sites, unmodified | 22 | Q2 Option B's entire justification is that these do not move |
| The definition | 1 | `function render() {` still contains the substring `render()` |
| New, in `setActiveTab` | +1 | |
| New, in `refreshAll` | +1 | its tail is a bare `render()` |
| `fetchItems` refactor | ±0 | `async function fetchItems() { await fetchItemsData(); render(); }` replaces the call at :198 one-for-one |
| `loadConfig` refactor | ±0 | contains no `render()` at all — see the split below |
| Init-IIFE `setActiveTab(activeTab)` (R4-3) | **±0** | see the note below — this adds a *runtime* render but **no matching source line** |
| Two T8 stubs (R4-2) | ±0 | neither `renderRecipesTab` nor `updateRecipeControlsDisabled` contains the substring `render()` |
| **Total** | **25** | |

> **The R4-3 row is ±0, and stating why is the point of the table (R4-5's lesson applied here).** The
> review that raised R4-3 asked for "the one extra `render()`" to be counted. There is an extra
> render **at runtime** — `setActiveTab` ends in `render()`, and `fetchItems` already rendered — but
> the gate is `grep -c 'render()'`, a count of **source lines containing that substring**, and the
> line the IIFE gains is `setActiveTab(activeTab);`, which contains no `render()`. So the census
> stays at **25**. Recording the row as an explicit ±0 rather than omitting it is deliberate: an
> omitted row is indistinguishable from an overlooked one, which is how r2 and both r2 reviewers each
> produced a different wrong total.

`renderProgressBar()`, `renderGroceryTab()` and `renderRecipesTab()` do **not** match: the character
after `render` is `P`/`G`/`R`, not `(`.

> **Two counts to distrust.** r2 asserted **23**, which was its own baseline unchanged — impossible,
> since T7 adds calls by construction. Both r2 reviewers asserted **24**, deriving "22 + 1 + 1"; that
> omits `refreshAll()`'s tail call, which the same reviewers separately endorsed. See §0.2. A gate
> built on either number fails on a correct implementation, and the tempting way out — deleting a
> legitimate `render()` call to make the number fit — destroys Q2 Option B.
>
> **The obsolete `grep -c 'function render(opts'` gate is dropped**, since `opts` no longer exists
> (R3-2).

- **New module-scope state** near `app.js:8-16`: `let recipes = []`, `let activeTab = 'grocery'`,
  `let collapsedRecipes = {}`, `const TAB_KEY = 'grocery.activeTab'`. Module scope, **never
  re-derived from the DOM** — PM-3's guard. **`collapsedRecipes` is NOT persisted to
  `localStorage`** — `collapsedGroups` (`app.js:11`) is not persisted today either, and persisting
  one and not the other is new behavior behind no AC (R-19). Only `activeTab` persists (AC-8.2
  requires it).

- **New pure helpers**, added to the existing `Pure helpers (mirrored in app.test.js — keep in sync)`
  block (comment at `app.js:106`; the helpers themselves run **111-165** — :166 is blank and 167-169
  is the `── API ──` comment box, so do not append inside those). **All parameters explicit, no
  module-scope reads**, so the `app.test.js` mirror is exact (Q3's note):
  - `recipeById(recipes, id)`
  - `recipesForRender(recipes)` — sort by `order`, tie-break `created_at`; returns a new array
  - `ingredientsForRecipe(items, recipeId)` — filter + sort by `created_at` (§4.1 / A-6); new array
  - `isOwned(item)` — `!!item.recipe_id`
  - **`recipeSuffix(item, recipes)`** → `" (Chili)"` when owned by a **known** recipe, `""` otherwise
    (free item **or** dangling `recipe_id` — PM-1's guard). **This is the helper `buildRow` renders
    from** (R-16).
  - `displayName(item, recipes)` → **defined in terms of it**: `item.name + recipeSuffix(item, recipes)`
  - `groupLabel(group)` — `NO_GROUP → 'Unallocated'`, else `group`
  - `groupEmptyHint(group)` — `'Nothing unallocated'` / `'No items'`
  - `tabControlVisible(controlId, tab)` — AC-8.4's truth table for **`#hide-not-needed-btn`,
    `#reset-btn`, `#groups-btn` only**. **`#progress-bar` is removed from this table (R3-4).** r2
    kept it in and had T10 assert `tabControlVisible('#progress-bar', 'grocery') === true`, while
    r2's own R-11 forbids `setActiveTab` from applying it — so the unit test asserted the opposite of
    the shipped design and would have passed forever regardless of the bar's actual behavior.
  - **`progressBarVisible(showProgress, itemCount, tab)`** → `!!showProgress && itemCount > 0 && tab
    === 'grocery'`. **New in r3 (R3-4).** This is the extracted form of the guard that actually
    ships, so the test and the code are the same logic rather than two descriptions of it — see the
    ownership note below.
  - `applyRecipeToggle(items, recipeId, enabled)` — pure mirror of FRD §6.3 for the optimistic
    client update; returns a new array, never mutates the input

- **Mirror convention — ONE rule, stated once and used everywhere** (r1 stated it three different
  ways across T7, T10 and Q3):
  > Every helper mirrored between `app.js` and `app.test.js` carries, as the line immediately above
  > its `function` keyword, the comment `// mirrors app.js :: <fnName>` (in `app.test.js`) or
  > `// mirrored in app.test.js :: <fnName>` (in `app.js`), and the two definitions have **identical
  > parameter lists**.

  This rule applies to the helpers **added by this plan** only. The four pre-existing mismatches
  (Q3's note) are recorded, not retrofitted.

- **Q2 Option B — the dispatcher.** Rename the body of `render()` (`app.js:638-697`) to
  `renderGroceryTab()`.

  **First, three new element handles**, added to the existing cached-handle block at
  `app.js:20-31` — r2 omitted them and its dispatcher could not have worked:
  ```js
  const rc       = document.getElementById('recipes-container');
  const rEmptyEl = document.getElementById('recipes-empty-state');
  const rHintEl  = document.getElementById('recipes-offline-hint');
  ```
  **And a two-line helper**, because the file has no such function today:
  ```js
  // NOT mirrored in app.test.js: touches the DOM, so it is not a pure helper.
  function setHidden(el, hidden) { if (el) el.classList.toggle('hidden', hidden); }
  ```
  > **r2's dispatcher would have thrown on the very first render (R3-16).** It called
  > `setHidden('#groups-container', …)` — but **`setHidden` is not defined anywhere in `app.js`**,
  > and it passed *selector strings* where this file's idiom throughout is a cached handle plus
  > `classList.toggle('hidden', cond)` (e.g. `app.js:640`). Handles are cached once at
  > `app.js:20-31`; `#recipes-container` and `#recipes-empty-state` were never added to that block.
  > `setHidden` therefore takes an **element**, never a selector.

  The new `render` is:
  ```js
  function render() {
    // Container visibility is owned here, before dispatch (R-13 / Q2's named risk).
    setHidden(gc,       activeTab !== 'grocery');
    setHidden(emptyEl,  activeTab !== 'grocery' || items.length > 0);
    setHidden(rc,       activeTab !== 'recipes');
    setHidden(rEmptyEl, activeTab !== 'recipes' || recipes.length > 0);
    renderProgressBar();                       // sole owner of #progress-bar (R-11)
    if (activeTab === 'recipes') renderRecipesTab();
    else                         renderGroceryTab();
  }
  ```
  - **All 22 existing bare `render()` calls stay untouched** — and now trivially so, since the
    signature did not change at all (R3-2).
  - **T7 must add TWO stubs, not one (R4-2).** Both, verbatim:
    ```js
    function renderRecipesTab()            { /* T8 */ }
    function updateRecipeControlsDisabled() { /* T8 */ }
    ```
    - Without the **first**, `render()` throws the moment the Recipes tab is selected (R3-31).
    - Without the **second**, **every** tab switch throws `ReferenceError` — on *both* tabs, and
      *before* `render()` is ever reached — because `setActiveTab` calls
      `updateRecipeControlsDisabled()` (see its bullet below) and the function's only definition is
      in T8 (T8's "Offline gating" bullet). T7's own done-when, "both tabs switch instantly", cannot
      pass; neither can AC-8.1, AC-8.2, AC-8.3 or AC-8.4, all of which are exercised by switching.

    > R3-31 caught the `renderRecipesTab` call site and **missed this one**, so r3b still left T7
    > non-executable in isolation — the exact defect R3-31 was raised to fix, one call site over.
    > This is also R4-24's canonical example: the failure appears in the first second of manual
    > checking, but only if someone actually tries the gate before declaring the task done.

    T8 replaces both bodies. **Neither stub moves the `render()` census** (`renderRecipesTab` is
    followed by `T`, not `(`; `updateRecipeControlsDisabled` contains no `render` at all) — see the
    ±0 row in the table above.
  - **The empty-state leak (R-13).** Q2's cons column named this exact risk and r1 gave it no owner:
    `renderGroceryTab()` owns `emptyEl.classList.toggle(...)` at `app.js:640` and is not called on
    the Recipes tab, so a visible `#empty-state` persists across the switch. Setting all four
    containers **here, before dispatch**, is the fix.
  - `renderGroceryTab()` keeps its own `emptyEl.classList.toggle` at :640 (harmless — the dispatcher
    already agreed with it on the grocery path).
  - **Move the `renderProgressBar()` call from line 641 up into `render()`** (one line). This is what
    makes `renderProgressBar` the single owner on *both* tabs and on every SSE re-render.

- **`#progress-bar` has exactly ONE owner (R-11), and the guard is now extracted and tested
  (R3-4).** r1 both applied `tabControlVisible` to
  `#progress-bar` in `setActiveTab` **and** extended `renderProgressBar()`'s guard. Since
  `tabControlVisible('#progress-bar', 'grocery')` is true, `setActiveTab('grocery')` would
  **un-hide the bar unconditionally**, overriding the `!showProgress` early return at `app.js:584`
  that keeps it hidden when config sets `progress:false` — which is the **default**
  (`app.js:181`: `showProgress = cfg?.progress || false`). That silently regresses existing behavior
  with no AC covering it.
  - **Remove `#progress-bar` from `setActiveTab`'s set**, and from `tabControlVisible`'s table.
  - Extract the shipped predicate as a pure helper, mirrored and tested:
    ```js
    // mirrored in app.test.js :: progressBarVisible
    function progressBarVisible(showProgress, itemCount, tab) {
      return !!showProgress && itemCount > 0 && tab === 'grocery';
    }
    ```
  - Rewrite `renderProgressBar`'s guard (`app.js:583-584`) to **call it**:
    ```js
    function renderProgressBar() {
      if (!progressBarVisible(showProgress, items.length, activeTab)) {
        progressBar.classList.add('hidden');
        return;
      }
      …
    ```
    Writing the condition inline a second time is what let r2 ship an AC-8.4 unit test that
    contradicted the shipped code. The helper is the AC-8.4 / R-11 check (T10, §5.3).
  - `render()` calls `renderProgressBar()`; `setActiveTab` calls `render()`. Done.

- `setActiveTab(tab)`: writes `localStorage[TAB_KEY]`, updates `.tab-btn.active` + `aria-selected`,
  applies `tabControlVisible` to **`#hide-not-needed-btn`, `#reset-btn`, `#groups-btn` only**,
  toggles the footer form's group `<select>` and placeholder (§7.3), calls
  `updateRecipeControlsDisabled()`, then `render()`.
  **Must not call `fetchItems()`, `fetchItemsData()`, `fetchRecipesData()`, `loadConfig()`,
  `loadConfigData()`, `refreshAll()`, `connectSSE()` or `disconnectSSE()`** (AC-8.3). Switching tabs
  renders from state already in memory; that is the whole criterion.

- `setAllCollapsed` (`app.js:291`): branch on `activeTab` — collapse recipe cards via
  `collapsedRecipes` on the Recipes tab (§7.1). **This is AC-8.8's implementation** (FRD:435, added
  by F-9). r3's parenthetical "untested-by-AC per D-6" is **withdrawn (r3b)**: D-6's gap is exactly
  what F-9 closed (D-6's own row is corrected to match in r4, R4-11).
  - **Iteration source on the Recipes branch (R4-16):** iterate **`recipes`** — the module-scope
    array — and write `collapsedRecipes[r.id] = collapsed` for each, mirroring what the Grocery
    branch does over its own groups. Do **not** iterate `groupsForRender()` (wrong domain entirely)
    and do **not** iterate the DOM (`rc.querySelectorAll('.recipe-card')`): this plan's Principle is
    module scope, **never re-derived from the DOM** (PM-3's guard), and a DOM walk would also miss
    cards that a filter had not rendered. `recipesForRender(recipes)` is equally correct here but
    buys nothing — order is irrelevant when the operation is "set them all" — so use the plain array.
  - **Written in T7, first observable at T8 (R4-6).** The branch is authored here, but T7 ships
    `renderRecipesTab() { /* T8 */ }`, so `#recipes-container` is **empty at T7** and there are no
    card bodies for a collapse to act on. **AC-8.8's gate is §5.3 e2e step 19, and step 19 runs at
    T8**, where the cards exist. §5.1's rows and T10's AC-walk table say the same thing. Do not try
    to discharge step 19 during T7: it fails on a correct implementation, which is exactly what
    R4-24 exists to catch.

- **Recipe-fetch call sites — all three enumerated (R-3, retitled in r3).** r1 left these
  unspecified, which fails AC-8.2 and AC-8.5. Mirror the data half of `fetchItems`
  (`app.js:195-199`) exactly, including the `|| []` at `app.js:197`:
  ```js
  async function fetchRecipesData() {
    const data = await api('GET', '/api/recipes').catch(() => []);
    recipes = data || [];              // mirrors items = data || []  (app.js:197)
  }
  ```
  > **There is no `fetchRecipes()` wrapper (R3-1).** r2 defined
  > `async function fetchRecipes() { await fetchRecipesData(); render(); }` and then **never called
  > it** — all three call sites below use `fetchRecipesData()` or `refreshAll()`. Worse, it was a
  > 24th `render()` line, so r2's own `== 23` gate failed on a *correct* implementation, and the
  > obvious way to make the number fit is to delete a legitimate `render()` call — which is Q2
  > Option B's entire justification. Do not add it. (`fetchItems` keeps its wrapper only because 
  > existing call sites already use that symbol; nothing calls `fetchRecipes`, so nothing needs it.)

  **Split `fetchItems` and `loadConfig` — and the two splits are NOT symmetric (R3-8).** r2 said
  "the same way", but the two functions do not end the same way: `fetchItems` ends in `render()`
  (`app.js:198`) while `loadConfig` ends in `rebuildGroupSelect()` (`app.js:191`) **and**
  `renderProgressBar()` (`app.js:192`). Exactly:
  ```js
  async function fetchItemsData() {
    const data = await api('GET', '/api/items').catch(() => []);
    items = data || [];                        // app.js:196-197 verbatim
  }
  async function fetchItems() { await fetchItemsData(); render(); }

  // loadConfigData is the fetch and the field assignments ONLY — app.js:179-190,
  // through the end of the `if (cfg?.title)` block. No rebuild, no render.
  async function loadConfigData() { /* app.js:179-190 */ }

  async function loadConfig() {
    await loadConfigData();
    rebuildGroupSelect();                      // app.js:191
    renderProgressBar();                       // app.js:192
  }
  ```
  Both existing symbols keep their exact current behavior, so **no existing call site changes**.
  Then:
  ```js
  async function refreshAll() {
    await Promise.all([fetchItemsData(), fetchRecipesData(), loadConfigData()]);
    rebuildGroupSelect();
    render();                                  // owns the progress bar via renderProgressBar()
  }
  ```
  > `refreshAll` deliberately calls `loadConfigData()` and `rebuildGroupSelect()` rather than
  > `loadConfig()`: calling `loadConfig()` would run `renderProgressBar()` a second time, once
  > before `render()` and once inside it. Letting `render()` own the bar is what keeps R-11's
  > single-owner rule true on the SSE path too.
  Call sites:

  | # | Where | Change |
  |---|---|---|
  | 1 | **Init IIFE**, `app.js:932-936` (`await loadConfig(); await fetchItems(); connectSSE();`) | Rewritten in full below (R4-3). Seed `activeTab`, fetch recipes, **and call `setActiveTab(activeTab)`** — seeding the variable alone is not enough. |

  **Call site #1 in full (R4-3).** The IIFE becomes exactly:
  ```js
  (async () => {
    activeTab = localStorage.getItem(TAB_KEY) === 'recipes' ? 'recipes' : 'grocery';
    await loadConfig();
    await fetchRecipesData();
    await fetchItems();
    setActiveTab(activeTab);      // R4-3 — applies the chrome, not just the data
    connectSSE();
  })();
  ```
  - **`await fetchRecipesData()` before `await fetchItems()`.** Without it, reloading onto the
    Recipes tab renders an empty container (AC-8.2 + AC-8.5).
  - **Seeding `activeTab` is the *first* statement, before `await loadConfig()` on `app.js:933`** —
    not merely "before the first render" as r2 said: `loadConfig` calls `renderProgressBar()` at
    `app.js:192`, which now reads `activeTab` through `progressBarVisible`, so an unseeded
    `activeTab` decides the bar's visibility on load (R3-15). Default to `'grocery'` on any
    unrecognized value — the ternary above does that by construction.
  - **`setActiveTab(activeTab)` after `await fetchItems()` is mandatory and is new in r4 (R4-3).**

  > **Why seeding the variable is not enough.** The init IIFE (`app.js:932-936`) contains **no**
  > `setActiveTab` call today, and **everything that reconciles the header with `activeTab` lives
  > inside `setActiveTab`**: the `.tab-btn.active` class, `aria-selected`, and `tabControlVisible`
  > over `#hide-not-needed-btn` / `#reset-btn` / `#groups-btn`. Those three buttons carry **no
  > `hidden` attribute in the markup** (`index.html:38`, `:65`, `:74`), and T5's tab bar ships
  > `#tab-grocery` with `class="tab-btn active"` and `aria-selected="true"` as the static default.
  >
  > So after a reload with `activeTab === 'recipes'`: `render()` dispatches to `renderRecipesTab()`
  > correctly and `progressBarVisible` hides the bar correctly — but the header still shows
  > **Grocery** as the selected tab and still shows the eye, reset and groups buttons, floating over
  > a Recipes view they do not control. **AC-8.1 and AC-8.4 both fail on the reload path.** Neither
  > is caught today: §5.3 steps 14 and 15 exercise only the *click* path, where `setActiveTab` runs
  > by definition. Calling `setActiveTab(activeTab)` once at the end of init makes the two paths
  > identical, which is the same argument R3-2 used to delete `opts`.
  >
  > **Two consequences, both accepted and both stated so they are not "discovered" later:**
  > 1. **A redundant `localStorage` write.** `setActiveTab` writes `localStorage[TAB_KEY]`, and we
  >    just read the value from it. Writing back the value we read is a no-op with no observable
  >    effect. Harmless; do not add a guard for it.
  > 2. **A second render on load.** `fetchItems()` ends in `render()` and `setActiveTab` ends in
  >    `render()`, so the page renders twice during init. Both renders are synchronous `innerHTML`
  >    writes over data already in memory, on a list the user has not seen yet — invisible, and
  >    cheaper than the alternative. **The `render()` census is unmoved: it stays at 25**, because
  >    the line added is `setActiveTab(activeTab);`, which does not contain the substring `render()`.
  >    The census table above carries this as an explicit **±0** row.
  >
  > **The rejected alternative, recorded so it is not re-proposed.** Factoring the chrome into an
  > `applyTabChrome()` called from both `setActiveTab` and init would avoid the second render. It is
  > rejected: it adds a third function and a second place where header state is decided, to save one
  > invisible synchronous render during page load. **Pick one — this plan picks `setActiveTab`.**
  | 2 | **Sync-toggle `change` handler**, `app.js:881-892`, at the `await fetchItems()` on **:885** | Replace with `await refreshAll()` so recipes are refreshed on re-enable alongside items (AC-8.5, AC-10.2). Note this adds a config refetch the current code does not perform — see §7 Consequences. |
  | 3 | **SSE `message` handler**, `app.js:908-912` (currently `fetchItems(); loadConfig();`) | Replace both with a single `refreshAll()` (AC-8.7 + PM-3 guard 7). No argument — the PM-3 guard no longer depends on knowing the caller. |

  > **Why `refreshAll` (R-6).** r1's SSE path would have fired three unordered promises
  > (`fetchItems`, `fetchRecipes`, `loadConfig`), each triggering its own `render()` — tripling
  > PM-3's blast radius and creating a window in which `items` references a recipe already removed
  > from `recipes`, i.e. exactly PM-1's `beef ()` render, transiently, on every delete.

**Done when (T7-specific):**
- `grep -c 'function renderGroceryTab' web/grocery/app.js` → **1**
- `grep -c 'function renderRecipesTab' web/grocery/app.js` → **1** (the T8 stub)
- `grep -c 'function updateRecipeControlsDisabled' web/grocery/app.js` → **1** (the second T8 stub,
  **R4-2** — without it every tab switch throws `ReferenceError` before `render()` is reached)
- `grep -c 'function setHidden' web/grocery/app.js` → **1**
- `grep -c 'fetchRecipes()' web/grocery/app.js` → **0** (R3-1: the wrapper must not exist)
- `grep -c 'render()' web/grocery/app.js` → **25**, per the itemized table at the top of this task.
  The 22 baseline call sites must **all** be preserved — none deleted, none renamed to
  `renderGroceryTab()`/`renderRecipesTab()`; that preservation *is* Q2 Option B's entire benefit.
  Any later task adding calls states its own delta the same way.
  > r1's gate — "`grep -c` before and after" — cannot hold, since T7 adds calls by construction
  > (R-8). r2's `== 23` and the r2 reviewers' `== 24` both fail on a correct implementation. If your
  > count is 24, you probably dropped `refreshAll`'s tail call; if it is 26, you probably kept
  > `fetchRecipes()`.
- Both tabs switch instantly. **"Restores the tab" (AC-8.2) means data *and* chrome (R4-3):** reload
  while on Recipes and the recipe view is shown **and** `#tab-recipes` carries `.active` and
  `aria-selected="true"` **and** the eye, reset and groups buttons are hidden — checked in the DOM
  inspector, per §5.3 step 7. A reload that restores only the view is call site #1 missing its
  `setActiveTab(activeTab)`. DevTools Network filtered to XHR/EventSource shows **zero** requests and
  no `EventSource` reconnect on a tab switch (AC-8.3).
- With config `progress:false` (the default), the progress bar stays hidden on **both** tabs
  (R-11 regression check). With `progress:true` and at least one item, it is visible on Grocery and
  hidden on Recipes, and **survives a switch away and back** (AC-8.4 as amended by F-8).
- **Empty-state isolation, both directions, with zero items AND zero recipes (R3-18)** — the state
  where both leaks are visible at once:
  - switch Grocery → Recipes: `#empty-state` becomes hidden and `#recipes-empty-state` becomes
    visible;
  - switch Recipes → Grocery: the reverse.
  This is precisely the risk Q2 Option B names in its cons column; the dispatcher is its only owner.
- **AC-8.8 is written here but NOT gated here (R4-6).** `setAllCollapsed`'s `activeTab` branch is
  authored in this task; its gate, §5.3 **step 19**, requires "every card body collapses" and T7
  ships `renderRecipesTab() { /* T8 */ }`, so there are no card bodies yet. **Step 19 is discharged
  at T8**, and §5.1's T8 row and T10's AC-walk table both say so. T7's obligation here is only that
  the branch exists and reads `activeTab` — confirm by inspection, not by pressing the buttons.
- Plus the three universal gates.

---

### T8 — `app.js`: Recipes tab render, mutations, offline gating
**Files:** `web/grocery/app.js`
**Dep:** T7 · **AC:** AC-8.5, AC-8.6, AC-8.7, AC-9.5, **AC-9.6**, AC-10.1, AC-10.2

- **`renderRecipesTab()`** — no parameters (R3-2) — writing into `rc` (`#recipes-container`): one card per
  `recipesForRender(recipes)` with name (click → rename), ingredient count, `.recipe-switch`, delete
  button, collapse chevron, **up/down reorder buttons** (R-14), the ingredient list from
  `ingredientsForRecipe(items, r.id)` each with a remove `✕`, and a per-card
  `<input class="recipe-ingredient-input" data-recipe-id="…" placeholder="Add an ingredient…">`.
  Empty state via `#recipes-empty-state`, **never** the grocery `#empty-state` (`app.js:640`).

- **Every recipe name written into `innerHTML` goes through `esc()` (`app.js:151`) (R-17/B17.)**
  This is ordinary output-encoding consistency — `buildRow` already does it for `item.name` at
  `app.js:718, 720, 725` — not a security workstream (§0.1). Applies to the card title, the delete
  button's `aria-label`, the confirmation-modal text, and T9's suffix. Any `querySelector` built
  from a recipe id uses **`CSS.escape`**, mirroring the existing group-selector handling at
  `app.js:770-771`.

- **PM-3 guard — mandatory, one mechanism, path-independent (R-6 as corrected by R3-2).** r2 gated
  this on `opts.source === 'sse'`. That is withdrawn: every mutation below calls a bare `render()`
  "in the style of `addItem`" (`app.js:234`), so an SSE-only guard leaves the local path — the
  common one — wiping the container on every add. The guard is now unconditional and lives in
  module scope.

  **Module-scope state**, declared beside `collapsedRecipes` (T7):
  ```js
  let recipeDrafts  = {};    // recipeId -> { value, caret }
  let focusRecipeId = null;  // which card's input should hold focus after the next render
  ```

  **A delegated `input` listener on `rc`** keeps `recipeDrafts` current, so the draft is captured
  as the user types rather than scraped at wipe time. **It is bound ONCE at module scope (R4-20) —
  see the binding rule under "Three delegated listeners" below, which governs all three:**
  ```js
  rc.addEventListener('input', (e) => {
    const el = e.target.closest('.recipe-ingredient-input');
    if (!el) return;
    recipeDrafts[el.dataset.recipeId] = { value: el.value, caret: el.selectionStart };
  });
  ```

  **`renderRecipesTab()` then, in order:**
  1. Read `document.activeElement` **once**, immediately before the wipe: if it matches
     `.recipe-ingredient-input`, set `focusRecipeId = el.dataset.recipeId`.
  2. `rc.innerHTML = ''` and rebuild every card, with the ingredient inputs **empty in the markup**.
  3. For each card, write the draft back as a **property**:
     `input.value = (recipeDrafts[r.id] || {}).value || ''`. **Never interpolate a draft into
     `innerHTML`** — it is raw user text, and as a property it needs no `esc()` at all.
  4. If `focusRecipeId` is set, locate the input with
     ``rc.querySelector(`.recipe-ingredient-input[data-recipe-id="${CSS.escape(focusRecipeId)}"]`)``.
     If it is present, call `focus()`; then, **only if `recipeDrafts[focusRecipeId]` exists**, call
     `setSelectionRange(caret, caret)` with that entry's `caret`.
     **Then set `focusRecipeId = null` — unconditionally, as the last statement of step 4, outside
     every `if` above it.**

  > **Two corrections to r3b's step 4 (R4-15).**
  > 1. **The null-out is unconditional.** r3b said "finally set `focusRecipeId = null`" and then, one
  >    line later, "if the element is gone… step 4 is a **no-op**". Read literally — and an executor
  >    reads literally — the whole of step 4 including the null-out is skipped when the element is
  >    missing, leaving a **stale `focusRecipeId`**. The next unrelated render then finds that id
  >    still set and yanks focus into whichever card now answers to it, or into nothing. Only the
  >    *DOM work* is a no-op; the bookkeeping always runs. Structure it so `focusRecipeId = null` is
  >    the last statement of the step, not the last statement of the success branch.
  > 2. **`setSelectionRange` needs its own guard.** `caret` comes from `recipeDrafts[focusRecipeId]`,
  >    but a user who **focused an input and never typed** has no `recipeDrafts` entry at all — the
  >    `input` listener only fires on input. `(recipeDrafts[id] || {}).caret` is then `undefined`, and
  >    `setSelectionRange(undefined, undefined)` coerces to `(0, 0)`, silently jumping the caret to
  >    the start of an input the user may have clicked into the middle of. Skip the call when there is
  >    no draft entry and let the browser's own focus placement stand.
  >
  > **Recorded decision, not an oversight (R4-23): a selection collapses to a caret.** `recipeDrafts`
  > stores `selectionStart` only, and restore replays `setSelectionRange(caret, caret)`. A user who
  > had an active *selection* in an ingredient input when a render fired gets a caret at the
  > selection's start instead of the selection back. This is deliberate: storing
  > `{ start, end, direction }` and replaying all three is more state and more restore logic to
  > preserve a selection inside a short single-line "Add an ingredient…" field, across a re-render the
  > user did not initiate. Nothing in FRD §1.1 or AC-10 asks for it. If it ever matters, the change is
  > local to the `input` listener and this step.

  > **Why value and focus are tracked separately (see §0.2).** The value cannot be read off
  > `document.activeElement` at render time: when the render was triggered by clicking ✕ on card B,
  > `activeElement` is that button and card A's half-typed text would be lost. But *whether to
  > focus* cannot come from module state either, or that same ✕ click would yank focus into card A.
  > Hence: value from `recipeDrafts` (always), focus from the one pre-wipe `activeElement` read
  > (plus an explicit set by `addIngredient`).
  >
  > **r1's "Alternatively, skip the wipe when nothing changed" stays deleted**: it does not work for
  > PM-3's actual scenario (a checkbox tick mutates `items`, from which this tab derives its rows),
  > and offering two mechanisms leaves the executor without a mandate.

- **`revealRecipe(recipeId)` — AC-9.6's implementation lives here (r3b).** FRD:449-450 requires that
  clicking an owned row's recipe chip switches to the Recipes tab, **expands that recipe's card if it
  is collapsed, and scrolls it into view**. T9 owns the chip's markup and the `gc` listener arm; the
  behavior owns `collapsedRecipes`, which is T8's state — so the function is defined here and T9
  calls it. One function, one home for the state.

  ```js
  function revealRecipe(recipeId) {
    delete collapsedRecipes[recipeId];                 // 1. expand BEFORE the render
    setActiveTab('recipes');                           // 2. persists tab, re-chromes header, render()s
    const card = rc.querySelector(
      `.recipe-card[data-recipe-id="${CSS.escape(recipeId)}"]`);
    if (card) card.scrollIntoView({ block: 'nearest' });  // 3. AFTER the render, null-guarded
  }
  ```

  **The ordering is the specification, not a stylistic preference:**
  1. `delete collapsedRecipes[recipeId]` must come **first**. `renderRecipesTab()` reads that map while
     building each card body; clearing it afterwards leaves the card collapsed until some later,
     unrelated render — the user clicks the chip and lands on a collapsed card, which is AC-9.6 failing
     while looking like it passed. `delete` rather than `= false`: absent-means-expanded is the same
     default `collapsedGroups` (`app.js:11`) already uses, and deleting keeps the map from growing
     across a session, exactly as `deleteRecipe` and `addIngredient` do for `recipeDrafts`.
  2. `setActiveTab('recipes')` (T7) already ends in `render()`, which dispatches to `renderRecipesTab()`.
     **Do not add a second `render()` here** — T7's `grep -c 'render()'` census is a gate, and this
     function must not move it.
  3. `scrollIntoView` must come **after** step 2 and cannot be hoisted above it: until that render
     returns, `#recipes-container` still holds whatever the previous render left there and **the target
     card does not exist in the DOM**, so the query returns `null` and the scroll silently does nothing.
     `render()` is synchronous — plain `innerHTML` writes, no `await` — so the element is queryable on
     the very next statement: **no `requestAnimationFrame`, no `setTimeout`, no `await`**.
     `block: 'nearest'` so a card already on screen does not jump the viewport.
  - **Selector.** `.recipe-card[data-recipe-id="…"]`, with **`CSS.escape` on the id** per AC-9.7
    (FRD:451-454), mirroring the existing group-selector handling at `app.js:770-771`. This means T8's
    card markup must carry `data-recipe-id` on the `.recipe-card` element itself, not only on the inner
    controls — worth stating because r3 only ever required it on `.recipe-ingredient-input`.
  - **A recipe id that no longer resolves is a clean no-op, never a throw.** If `recipes` holds no
    such recipe, `recipesForRender` builds no card and `rc.querySelector` returns `null`. The
    **`if (card)` guard is mandatory and stays regardless of how the id got here**:
    `null.scrollIntoView` throws inside the `gc` click handler, which is delegated, so the exception
    is invisible on screen and kills the rest of that handler. `delete collapsedRecipes[id]` on an
    absent key is a no-op, and `CSS.escape` accepts any string, so nothing else on the path can fail.
    **The tab switch still happens** — the user asked to go look at recipes, and the one they clicked
    simply is not there any more; swallowing the whole gesture would be the more confusing behavior.

    > **The delete-race story r3b told for this guard does not hold up (R4-9c), and the guard is
    > correct anyway.** r3b justified `if (card)` with "another window can delete the recipe between
    > this client's last render and the click". Trace it: `refreshAll()` (T7) updates `items` and
    > `recipes` under **one** `Promise.all` followed by **one** `render()`. *Before* the SSE tick,
    > window A still holds Tacos in `recipes` **and** still holds the beef row, so the card is built
    > and `card` is non-null. *After* the tick, the cascade delete has removed the beef row too, so
    > **the chip no longer exists to be clicked** — and under R4-9 the chip *is* the suffix, so the
    > row simply renders as `beef`. There is no window in which the chip is clickable and the card is
    > absent. Keeping a defensive guard whose stated trigger is unreachable is how a plan
    > accumulates folklore.
    >
    > **The genuinely reachable `card === null` path, recorded so the guard has a true reason to
    > exist:** window B creates a recipe *and* an ingredient, and A's `fetchItems()` (`app.js:910`)
    > lands the **item** before A has the **recipe** — the two arrive on different fetches. In that
    > interval A holds an item whose `recipe_id` resolves to nothing. Under R4-9b that row renders
    > with **no chip**, so it is not clickable either — but the same shape is reachable through any
    > future caller of `revealRecipe`, and a delegated handler that can throw is not worth the two
    > characters saved. **Keep `if (card)`.** Its gate is §5.3 step 18's console check, which r4
    > rewrites to call `revealRecipe('no-such-id')` directly rather than staging an unreproducible
    > race.
  - **No interaction with the `recipeDrafts` / `focusRecipeId` machinery.** At click time
    `document.activeElement` is the chip `<button>` inside `#grocery-container`, not a
    `.recipe-ingredient-input`, so `renderRecipesTab()`'s step-1 pre-wipe read leaves `focusRecipeId`
    **null** and no focus is stolen into a card. `revealRecipe` must **not** set `focusRecipeId` itself:
    scrolling is the affordance AC-9.6 asks for, and focusing an ingredient input would pop the soft
    keyboard on a phone for a user who only wanted to look. Drafts are untouched — they live in module
    scope and survive the render — so a half-typed ingredient on another card is still there when the
    user scrolls back to it.

- **Mutations**, each optimistic-then-confirm in the style of `addItem` (`app.js:226`) and gated on
  `syncEnabled`:
  - `createRecipe(name)`
  - `renameRecipe(id, name)` → `PATCH {name}`; adopt the returned `{recipe, items}` (the response
    carries `items` even for a rename — see T4)
  - `toggleRecipe(id, enabled)` → apply `applyRecipeToggle` locally, then `PATCH {enabled}`, then
    adopt the returned `{recipe, items}`. This is AC-9.5's "without a manual reload".
  - `deleteRecipe(id)` behind a confirmation modal that names the recipe (escaped), states the
    **exact ingredient count**, and warns that **items the user moved into other groups are
    included** (FRD §6.1.4 requires the count; the group caveat is this plan's addition, per PM-2b).
    On success, remove the recipe from local `recipes` **client-side** — the response is `{items}`
    only, with no `recipe` key (D-1) — and `delete recipeDrafts[id]` so the map cannot grow
    unboundedly across a long session.
  - `addIngredient(recipeId, name)` — on success it **must** `delete recipeDrafts[recipeId]` and set
    `focusRecipeId = recipeId` **before** calling `render()`, so the input the user was typing in
    ends up **cleared and focused**, ready for the next ingredient. This is the behavior the e2e
    "three ingredients in a row without re-tapping" step checks.
  - `removeIngredient(recipeId, itemId)`
  - `moveRecipe(id, delta)` → recompute the id order locally, then `POST /api/recipes/reorder {ids}`

- **Client-side recipe drag-reorder is DESCOPED (R-14).** r1 said to reorder recipes by "reusing
  `attachGroupDrag`'s pointer logic (`app.js:409-496`)". That function is **not reusable**: it
  hard-codes `groupsList` (`app.js:40`, and again at :411, :424, :440, :444), the
  `.groups-modal-item` selector, `dataset.group`, and it terminates in `reorderGroups(newOrder)` at
  **:467**. "Reusing" it means copying ~88 lines with four substitutions — a second full drag
  implementation inside the same IIFE, with no AC and no test behind it.
  - **Keep** `POST /api/recipes/reorder` (AC-2.6 requires only that the *endpoint* sets `Order`, and
    T2/T4 test it).
  - **Ship** up/down buttons on each card, which hit the same endpoint.
  - **No AC regresses.** FRD §6.1.3's "editable by drag (mirrors the existing groups-modal drag
    implementation)" is the only thing left unimplemented — **FRD amendment listed in §6**.

- **Three delegated listeners on `rc`, not one (R3-25) — and all three are bound EXACTLY ONCE, at
  module scope (R4-20).**

  > **Where they are bound is as load-bearing as what they do, and r3b never said (R4-20).** Bind all
  > three **beside the existing `gc` click listener at `app.js:859`**, at module scope inside the
  > IIFE, evaluated once when the script runs. **Never inside `renderRecipesTab()` or any other
  > render function.** A listener registered inside a render **accumulates on every render**:
  > `rc.innerHTML = ''` destroys the *children*, but the listeners are attached to `rc` itself, which
  > survives — so after N renders there are N identical handlers and one click fires the click arm N
  > times. That means N `POST`s for one delete, N `revealRecipe` calls, N `render()`s each of which
  > adds another listener. The failure is silent at N=1 (the first render after load) and gets
  > geometrically worse the longer the tab is open — the worst possible shape for a bug to have.
  > Delegation is what makes binding-once safe: the handlers match on `e.target.closest(...)`, so
  > they keep working against markup that did not exist when they were bound.
  >
  > `rc` is a cached handle from T7 (`app.js:20-31` block), so it is non-null at module-scope
  > evaluation time and needs no guard.

  r2 specified only `click`, mirroring the
  `gc` listener at `app.js:859-868` — which is correct as far as it goes (that listener is bound to
  `#groups-container` and will **not** fire for recipe cards), but a text input needs more:
  - **`click`** — switch, delete, collapse chevron, up/down, ingredient ✕, rename.
  - **`input`** — maintains `recipeDrafts`, above. Without it there is no draft to restore.
  - **`keydown`** — **Enter inside a `.recipe-ingredient-input` submits that ingredient**
    (`e.preventDefault()` first). This is the only way to add an ingredient without leaving the
    keyboard, which is the entire point of a per-card input on a phone; with `click` alone the user
    must dismiss the soft keyboard to reach a button. The inputs are deliberately **not** wrapped in
    a `<form>` — a nested form inside the page's existing `addForm` flow is not something `app.js`
    does anywhere, and `keydown` costs one handler.

- **`addForm` submit** (`app.js:871-879`): branch on `activeTab` — create a recipe on Recipes, an
  item on Grocery (AC-8.6). `setActiveTab` (T7) already hides `#group-select` and swaps the
  placeholder to "Add a recipe…" (§7.3).

- **Offline gating:** `updateRecipeControlsDisabled()` sets `disabled` on every recipe control
  **and** — per D-9 — on the footer submit when `activeTab === 'recipes'`. Call it from the end of
  `renderRecipesTab()`, from `setActiveTab`, and from the sync-toggle `change` handler
  (`app.js:881`) so AC-10.2 is immediate. **T7 ships it as a stub** (`function
  updateRecipeControlsDisabled() { /* T8 */ }`, R4-2); this task supplies the body.

  - **The hint is tab-scoped, not sync-scoped (R4-14).** `#recipes-offline-hint` must be shown only
    when sync is off **and** the user is on the Recipes tab. Put it in the same `setHidden`
    dispatcher line-up as the other containers, with the same shape:
    ```js
    setHidden(rHintEl, syncEnabled || activeTab !== 'recipes');
    ```
    > **Why this is not a nicety.** `updateRecipeControlsDisabled()` is called from the sync-toggle
    > `change` handler at `app.js:881`, which fires **regardless of which tab is active**. Gated on
    > `!syncEnabled` alone, turning sync off while sitting on the **Grocery** tab would show
    > *"Recipe editing requires sync."* inside `<main>`, directly above the grocery list — a message
    > about a view the user is not looking at, explaining a restriction that does not apply to
    > anything on screen. T5 deliberately placed the hint inside `<main>` as a sibling of
    > `#recipes-container`, which is what puts it in the Grocery flow when unhidden.
    >
    > This also gives **`rHintEl`** (T7, `app.js:20-31` block) an actual job. r3b added the handle
    > and then never used it — the dispatcher owns `gc`, `emptyEl`, `rc` and `rEmptyEl`, and nothing
    > owned `rHintEl`. An unused cached handle is a sign the design forgot a container, and here it
    > was.
    >
    > The `disabled` attributes themselves stay sync-scoped: a disabled control on a hidden tab is
    > invisible and correct either way, and gating them on the tab too would mean re-enabling work on
    > every switch for no gain.

- **`doReset()`** (`app.js:335-343`): also set `r.enabled = false` locally for every recipe (A-2).
  **Keep `items = data`** at `app.js:341` — the response is still a bare array (D-2/D-3).

**Done when:** every AC-8/AC-10 criterion is manually demonstrable; **AC-9.6 is demonstrable via
`revealRecipe` — collapse a card, switch to Grocery, click that row's chip, and the tab switches, the
card is expanded and it is scrolled into view, in one gesture (§5.3 step 18)**; typing in an
ingredient input survives a mutation fired from a second browser window **and** a mutation fired
locally on another card; **the `render()` census is 39, per the itemized table below**; plus the
three universal gates.

**Post-T8 target: `grep -c 'render()' web/grocery/app.js` → 39.** Itemized in the T7 style, because
**"25 + N" was both wrong and unfalsifiable (R4-5)**:

| | Lines | Note |
|---|---|---|
| Post-T7 total, unchanged | 25 | T7's itemized table; every one of the 22 baseline call sites still preserved |
| `createRecipe(name)` | +2 | |
| `renameRecipe(id, name)` | +2 | |
| `toggleRecipe(id, enabled)` | +2 | |
| `deleteRecipe(id)` | +2 | |
| `addIngredient(recipeId, name)` | +2 | the second is the one that clears the draft and restores focus |
| `removeIngredient(recipeId, itemId)` | +2 | |
| `moveRecipe(id, delta)` | +2 | |
| `revealRecipe(recipeId)` | **±0** | it delegates to `setActiveTab`, which already renders — adding one here would be the bug the function's own step 2 forbids |
| `renderRecipesTab()` body | ±0 | the character after `render` is `R`, not `(` |
| `updateRecipeControlsDisabled()` body | ±0 | contains no `render` at all |
| `addForm` submit branch | ±0 | branches to `createRecipe` / `addItem`; neither is a new `render()` line |
| `doReset()` edit | ±0 | adds a `r.enabled = false` loop to an existing function; its `render()` at `app.js:341`-region is untouched |
| `rc` `input` / `keydown` listeners | ±0 | `keydown` calls `addIngredient`, already counted |
| **Total** | **39** | |

> **Why `25 + N` failed (R4-5).** r3b defined `N` as "the number of mutations this task added" — but
> the same task specifies every mutation as **optimistic-then-confirm in the style of `addItem`**
> (see the Mutations bullet), and `addItem` (`app.js:226-243`) contains **two** `render()` lines:
> `:234` for the optimistic insert and `:240` inside the `if (saved)` confirm. Seven mutations
> therefore contribute **14**, not 7, and a correct build lands at 39 while the gate expects 32.
>
> The deeper defect is that **`25 + N` is unfalsifiable**: `N` is whatever the executor's mutation
> count turns out to be, so *every* result satisfies it, including a build that dropped a confirm
> render or duplicated one. A gate that cannot fail is not a gate.
>
> **The table is the gate, not the number.** If a mutation legitimately deviates from the two-render
> shape — a mutation with nothing to confirm, say — **change its row and restate the total**; do not
> reconcile by adjusting the code or by shrugging at the difference. And per **R4-24**, run
> `grep -c 'render()'` against the tree **before** starting T8: it must print exactly **25**. If it
> does not, T7 drifted and this table is measuring the wrong baseline.

---

### T9 — `app.js`: Grocery tab changes
**Files:** `web/grocery/app.js`
**Dep:** T8 · **AC:** AC-9.1, AC-9.2, AC-9.3, AC-9.4, **AC-9.6** (call site), **AC-9.7**

- **`buildRow` (`app.js:702-734`), item-name span at line 720.** Render the clean name and the
  suffix as two spans, the suffix produced **exclusively by `recipeSuffix(item, recipes)`** (R-16):
  **The suffix element IS the chip — one element, not two (R4-9), and the separator space sits
  OUTSIDE the button (R5-4).**
  ```js
  const suffix = recipeSuffix(item, recipes);
  // …
  <span class="item-name" data-id="${item.id}">${esc(item.name)}${
    suffix
      ? ` <button class="recipe-chip item-recipe-suffix" data-recipe-id="${esc(item.recipe_id)}">${esc(suffix.trimStart())}</button>`
      : ''
  }</span>
  ```

  > **Why the space is outside the button (R5-4).** `recipeSuffix` returns `" (Chili)"`, leading
  > space included (see T7's helper list), and r4 interpolated that whole string *inside* the
  > `<button>`. A `<button>` is `display: inline-block` by UA default, so it establishes its own
  > inline formatting context — and collapsible white space at the **start** of an inline formatting
  > context is removed. The row would have rendered **`beef(Chili)`**, violating AC-9.1's
  > `name (RecipeName)` (FRD:438). So `buildRow` emits the space in the template, outside the tag,
  > and `trimStart()`s it off the button's text. `recipeSuffix` itself is **unchanged** — it still
  > returns the leading space, because `displayName = item.name + recipeSuffix(...)` depends on it
  > and T10 unit-tests that concatenation.
  > > Note that no unit gate would have caught this: T10 asserts on the **string** `recipeSuffix`
  > > returns, which was always correct. Only the DOM was wrong. §5.3 steps 1-2, which name
  > > `beef (Chili)` verbatim, were the sole gate standing between it and shipping — which is
  > > exactly the argument for R4-24's pre-run discipline.
  >
  > **Why one element and not two (R4-9a) — a product decision, made and closed.** r3b rendered the
  > suffix here as `<span class="item-recipe-suffix">` and then, in a separate bullet below, added
  > `<button class="recipe-chip" data-recipe-id="…">` — **and never said what the chip displays.**
  > Neither this plan nor the FRD gives the chip any content. §5.3 step 18 calls it "the `(Tacos)`
  > chip", which is the only content anyone has proposed for it — and that is the string the suffix
  > already renders, so the row would read **`beef (Chili) (Chili)`**: two adjacent elements showing
  > the same text, one of them clickable, with no way for the user to tell which. That is the
  > redundancy D-4 flagged in the first place, shipped rather than resolved.
  >
  > **One element discharges both criteria.** `beef (Chili)` is AC-9.1's derived display name
  > (FRD:438); clicking it is AC-9.6's reveal (FRD:449-450). The `<button>` sits **inside**
  > `.item-name`, exactly where the `<span>` did, so §7.4.1's "not part of the item name" holds in
  > the sense that matters — it is not part of `Item.Name`, which is never written (AC-9.2) — and the
  > hit target is the one the user would aim at anyway.
  >
  > **Its click arm must run before the `.item-name` arm** in the `gc` delegated listener, since the
  > button is a descendant of `.item-name` and `closest('.item-name')` will match from it. Without
  > that ordering, clicking the chip cycles the row's state instead of revealing the recipe. See the
  > listener bullet below.
  >
  > **T6 must style the two classes as one element** — a `<button>` carries UA defaults that would
  > otherwise render the suffix as a grey box in Arial. See T6's `.recipe-chip` bullet for the exact
  > reset and rule ordering.
  > **Why the helper is mandatory here.** r1 had T10 unit-test `displayName(item, recipes) → "beef
  > (Chili)"` while T9's `buildRow` needed the two halves separately to wrap the suffix in its own
  > span — so `buildRow` would have re-implemented the concatenation, and AC-9.1's unit test would
  > not have tested what ships. That is precisely the drift Q3 warns about. `recipeSuffix` is the
  > single shipped implementation; `displayName` is defined in terms of it and exists for the test
  > and for any future consumer.

  **The suffix is now the chip, so clicking it must NOT cycle state (R5-2).** This paragraph said
  the opposite through r4 — a leftover from r3b, when the suffix was a passive `<span>` and cycling
  *was* the correct outcome. R4-9 merged the two elements and inverted it: a click on
  `(Chili)` reveals the recipe card, and reaching `cycleState` is the bug that the arm ordering
  above exists to prevent. §7.4.1's "not part of the item name" remains true in the sense that
  matters — the suffix is never written to `Item.Name` (AC-9.2). Leave the checkbox `aria-label`
  (`app.js:718`) on the **clean** name — §3.3's whole point.
  > An executor who read the old sentence would skip the `.recipe-chip` arm entirely and ship a
  > chip that flips the row's needed/not-needed state on click. Two sentences in the same task said
  > opposite things thirteen lines apart; this is the surviving one.

- **`buildRow`, delete button (`app.js:725-730`)**: make the button **conditional — do not delete
  those lines.** Wrap them so the `<button class="delete-btn">…</button>` is emitted only when
  `item.recipe_id` is empty, and set
  `li.title = 'Delete this ingredient from its recipe'` (§7.4.3, AC-9.3). This is the client half of
  the §3.5 guard; T3/T4's 409 is the server half.
  > **Range correction (R3-10).** r2 cited `app.js:725-731`. The button is **725-730**: :730 is
  > `` </button>`; `` — which *terminates the template literal* — and :731 is blank. An executor
  > deleting 725-731 literally leaves an unterminated template literal and `app.js` stops parsing,
  > taking the whole page with it. Line 725 also contains one of the three `esc(item.name)` calls,
  > which is why the instruction is "conditional", not "delete".

- **D-4 chip — gated by AC-9.6 (FRD:449-450), required, and merged with the suffix (r3b + R4-9).**
  The chip's **markup is the suffix element above** — r3b's separate "add a `<button
  class="recipe-chip">` outside `.item-content`" is **withdrawn**; there is one element, rendered in
  `buildRow` as shown. What remains in this bullet is the **handler**: handle
  `.recipe-chip` in the `gc` delegated listener (`app.js:859-868`) **before** the `.item-name` /
  `.state-badge` checks — mandatory, since the chip is now a descendant of `.item-name` and
  `closest('.item-name')` matches from it, so a later arm would cycle the row's state instead. The
  arm is **one line** — `revealRecipe(chip.dataset.recipeId)` — because T8 owns the tab switch, the
  expand and the scroll together (see T8's `revealRecipe`).

  - **Dangling-`recipe_id` guard: gate the chip on the SAME predicate `recipeSuffix` uses (R4-9b).**
    `isOwned(item)` is `!!item.recipe_id`, which is **true for a dangling id** — an item whose
    `recipe_id` names a recipe this client does not have (yet, or any more). Gating the chip on
    `isOwned` would render a clickable chip while `recipeSuffix` correctly returns `""` — PM-1's
    `beef ()` symptom relocated into a button, and a button leading nowhere. **Gate on
    `recipeById(recipes, item.recipe_id)` resolving**, exactly as `recipeSuffix` does internally.
    Under the merged-element decision above this **falls out for free**: the element is emitted only
    when `suffix` is non-empty, and `recipeSuffix` returns `""` precisely when the id does not
    resolve. So a dangling id renders a clean `beef` with **no chip and no suffix**, and there is
    nothing to click. State it explicitly anyway, because it is only true *because* the two elements
    were merged — split them again and the guard silently disappears.
    **`revealRecipe`'s `if (card)` guard stays regardless** (T8); it is correct defensive code and
    its removal is not licensed by this.
  **Do not re-implement `setActiveTab` + `scrollIntoView` inline here.** `collapsedRecipes` is T8's
  state; an inline version in T9 would reach across the task boundary, and the expand step — the half
  F-3 was actually resolved to add — is the easy one to omit. r3 specified only the switch and the
  scroll, so this is the one place where AC-9.6 is genuinely new work rather than a missing gate.

- **`renderGroceryTab()` header** (the `group-title` span, `app.js:662`): render
  `esc(groupLabel(group))` instead of `esc(group)` — `"No Group"` displays as **"Unallocated"**
  (AC-9.4) while `section.dataset.group` (**:657**), `body.dataset.group` (**:678**) and
  `ul.dataset.group` (**:688**) keep the **stored** value, because `attachDragToHandle` and
  `moveItem` read those datasets to compute the destination group. Getting this wrong silently
  breaks drag-and-drop into Unallocated.

- **Empty hint** (`app.js:683`): use `groupEmptyHint(group)` → "Nothing unallocated" (§7.4.4).

- **`rebuildGroupSelect()` — make NO CHANGE to it, and do NOT add a "No Group" option (R4-1).**
  `rebuildGroupSelect()` (`app.js:209-221`) **already excludes NO_GROUP, deliberately**:
  `app.js:212` is the comment `// Never offer NO_GROUP as an add target`, and the loop on
  `app.js:213` iterates `groups`, which `app.js:9` documents as
  `// real groups only; NO_GROUP is virtual`. The markup itself (`index.html:112`) is an empty
  `<select id="group-select">`. **There is no "No Group" option in the footer select and there never
  has been.** Leave the function untouched.

  > **r3b's clause here was a defect, and r4 removes it rather than implementing it.** r3b said the
  > option "must also read Unallocated" — prescribing a **relabel of an option that does not
  > exist**, which an executor can only discharge by *adding* one. That would be an unrequested
  > behaviour change reversing a deliberate exclusion: the footer `<select>` is the add-form's
  > **destination picker** (`app.js:875`: `const group = groupSel.value || groups[0] || NO_GROUP;`),
  > so adding the option would let users **create items directly into Unallocated**. No AC asks for
  > that. The `<select>` cannot move an existing item in any case — moves go through the drag path
  > (`attachDragToHandle` / `moveItem`), which is why §5.3 step 17 now says *drag*.
  >
  > **There are two user-visible Unallocated strings, not three:** the Grocery **section heading**
  > (the bullet above) and the **Groups-modal hint** (`index.html:156`, shipped in T5). AC-9.4 is
  > amended to match (FRD:441-442), §6's F-4 row records the removal, and §7's Consequences now
  > counts two.

- **AC-9.2 needs no code here**: renaming a recipe changes `recipes`, and the suffix is derived per
  render. Its gate is a test — see T10.

**Done when:** owned rows show the suffix and no trash icon; free rows are unchanged with a trash
icon; an item whose `recipe_id` does not resolve shows **neither** suffix nor chip (R4-9b); the
`"No Group"` heading reads **Unallocated**; dragging an item into Unallocated lands it there and it
persists across reload (proves the `dataset.group` stored-value split); clicking an owned row's chip
reveals its card (AC-9.6, §5.3 step 18);
`grep -c 'render()' web/grocery/app.js` → **39, unchanged from T8 (R4-5)** — this task edits
`buildRow` and `renderGroceryTab` and adds one `gc` listener arm, none of which is a `render()` line:
the chip arm is `revealRecipe(...)`, which delegates to `setActiveTab`. A count other than 39 means
a `render()` was added or lost here; reconcile against T8's table before proceeding. Per **R4-24**,
run this grep **before** starting T9 and confirm it already prints 39;
plus the three universal gates.

**AC-9.7's gate (r3b) — the `<`-in-name check, promoted from an aside to a named two-site assertion.**
r3 already carried "a recipe name containing `<` renders literally rather than breaking the row
(R-17)", but it checked **one** render site. AC-9.7 (FRD:451-454) requires both. Named assertion:
*create a recipe named `Chili <b>`, give it an ingredient, and confirm the literal text `Chili <b>`
appears in **both** places a recipe name reaches markup — the **Recipes-tab card title** (T8
`renderRecipesTab`) **and** the **Grocery-tab suffix** on the owned row (this task's `buildRow`) — with
no bold rendering and no broken row in either.* One site passing is **not** a pass: these are separate
`esc()` call sites in separate functions, and a miss in `renderRecipesTab` is exactly what a
row-only check cannot see. Executed as §5.3 e2e **step 13**, which now carries the AC-9.7 tag.

**AC-9.7's static half is now automated, not eyeballed (R4-17).** r3b left it as prose — "no recipe
name is interpolated into a template literal without `esc()`, and every selector built from a recipe
id is wrapped in `CSS.escape`" — with nothing running it. R3-19 already reads the **shipped** `app.js`
into `app.test.js` with `readFileSync`, so the material is there; add assertions over `APP_SRC` in
the same block (T10):

```js
// AC-9.7 static half — assertions over the SHIPPED source, not a mirrored copy.
assert.ok(!/\$\{\s*(r|recipe)\.name\s*\}/.test(APP_SRC),
  'recipe name interpolated without esc()');
assert.ok(/CSS\.escape\(\s*recipeId\s*\)/.test(APP_SRC),   // revealRecipe
  'revealRecipe selector missing CSS.escape');
assert.ok(/CSS\.escape\(\s*focusRecipeId\s*\)/.test(APP_SRC), // renderRecipesTab focus restore
  'focus-restore selector missing CSS.escape');
```

> **R-19's objection does not apply here, and the distinction matters.** R-19 disqualifies unit tests
> that exercise an **inline-mirrored copy** of a function, because the copy is not what ships. These
> assertions inspect the **source text of the shipped file**. There is no copy involved, so a
> regression in `app.js` fails the suite directly. This is the same mechanism R3-19 uses for mirror
> integrity, pointed at a different property.
>
> **They are a floor, not a proof.** A regex cannot enumerate every way a name might reach markup;
> what it *can* do is fail loudly on the specific shapes an executor would actually write
> (`${r.name}`, `${recipe.name}`) and on a `CSS.escape` that was dropped during a refactor. **Step 13
> remains AC-9.7's gate**; these assertions are what stop a later edit from silently undoing it
> between manual runs. Adjust the patterns if the shipped identifiers differ — an assertion that
> cannot match the code it guards is a dead gate, which is precisely R4-24's failure mode.

**Add the delete-button `aria-label` to step 13's inspection list.** T8 escapes the recipe name into
the card's delete-button `aria-label` (the `esc()` bullet in T8 names it), and an attribute value is
**invisible to a visual check** — `Chili <b>` in an `aria-label` shows nothing on screen whether it
was escaped or not, and a broken quote there silently mangles the surrounding tag. Inspect it in the
DOM inspector alongside the two rendered sites and the confirmation modal.

The `CSS.escape` sites this plan adds are exactly two: `revealRecipe` and `renderRecipesTab`'s focus
restore, both in T8.

---

### T10 — `app.test.js` mirrors and full-suite verification
**Files:** `web/grocery/app.test.js`
**Dep:** T9 · **AC:** verification layer for AC-8.4, AC-9.1, AC-9.4; supporting evidence for AC-9.2; **AC walk for AC-8.8, AC-9.6, AC-9.7 (r3b)**; final gate on all 11 groups

**Recorded baseline:** 52 `it(...)` tests; mirror block at `app.test.js:10-102`; imports at :7-8.

- Following the file's existing convention (Q3 Option A), **inline-copy** each new pure helper from
  T7 into the mirror block, each carrying the one mirror tag defined in T7. **Identical parameter
  lists** in both files. Do **not** introduce imports, modules, a bundler, or a second script tag.
- Extend the existing `applyReset` mirror (`app.test.js:50`) to take and clear `recipes` (A-2), and
  update its three existing tests.
  > Note `applyReset` and `removeGroup` (`:58`) have **no `app.js` counterpart** — they are
  > test-local models, not mirrors. Extending `applyReset` is therefore extending a model of
  > `doReset()`, not mirroring a function. Recorded so the tag is not read as a false promise.
- New `describe` blocks in the file's style:
  - **`recipeSuffix`** — owned by a known recipe → `" (Chili)"`; free item → `""`; **unknown
    `recipe_id` → `""`, no throw** (PM-1's dangling guard); source not mutated. *This is AC-9.1's
    real unit gate, because `buildRow` renders from this function (R-16).*
  - `displayName` — `"beef (Chili)"` / `"beef"`, and that it equals `item.name + recipeSuffix(...)`
  - `ingredientsForRecipe` — filters by `recipe_id`, sorts by `created_at`, empty for unknown, does
    not mutate the source (mirroring the four `itemsForGroup` tests at `app.test.js:118-146`)
  - `groupLabel` / `groupEmptyHint` — the Unallocated relabel (AC-9.4)
  - `groupsForRender` — unchanged behavior with owned items present
  - `tabControlVisible` — the truth table for the three controls it actually governs
    (`#hide-not-needed-btn`, `#reset-btn`, `#groups-btn`). **`#progress-bar` is NOT in it (R3-4).**
    r2 kept `#progress-bar` in the table and asserted it visible on Grocery, while r2's own R-11
    forbids `setActiveTab` from applying the helper to that element — so the test asserted the
    opposite of the shipped design and could never fail on a real regression. Per R-19 this test is
    **supporting evidence**, not a gate: it exercises a test-local copy.
  - **`progressBarVisible` — this one IS AC-8.4's gate for the progress bar (R3-4)**, because
    `renderProgressBar` (`app.js:583-584`) now calls the shipped helper rather than repeating the
    condition. Cover: `progress:false` → hidden on both tabs (the default, and R-11's regression
    case); `progress:true, items>0, 'grocery'` → visible; `progress:true, items>0, 'recipes'` →
    hidden; `progress:true, items===0` → hidden on both. That is AC-8.4 as amended by F-8
    (FRD:429-431).
  - `applyRecipeToggle` — owned → toggled + `completed:false`; other recipe untouched; free
    untouched; source not mutated
  - `isOwned`; `recipesForRender` (order, tie-break, no mutation)
- **Mirror-integrity test — Option C folded into Option A (R3-19).** Add one `describe('mirror
  integrity')` block, ~20 lines, at the end of the file:
  ```js
  // ── FILE TOP LEVEL, beside the existing imports at app.test.js:7-8 ──
  import { readFileSync } from 'node:fs';
  import { join } from 'node:path';
  const APP_SRC = readFileSync(join(import.meta.dirname, 'app.js'), 'utf8');

  // Indentation differs by design: app.js helpers live inside the IIFE at 2-space
  // indent; the app.test.js mirrors sit at column 0. Compare on content, not layout.
  const norm  = s => s.replace(/^[ \t]+/gm, '').trim();
  const APP_N = norm(APP_SRC);
  ```
  For every helper introduced by T7 (`recipeById`, `recipesForRender`, `ingredientsForRecipe`,
  `isOwned`, `recipeSuffix`, `displayName`, `groupLabel`, `groupEmptyHint`, `tabControlVisible`,
  `progressBarVisible`, `applyRecipeToggle`), assert that the **normalized** `app.js` source contains
  the mirrored function's **normalized** own source:
  ```js
  assert.ok(APP_N.includes(norm(recipeSuffix.toString())), 'recipeSuffix drifted from app.js');
  ```
  so an edit to either copy fails the suite. `import.meta.dirname` keeps it independent of the
  working directory, and the whole thing runs under the existing
  `node --test web/grocery/app.test.js` command: no new file, no script tag, no `Makefile` hook
  (which §0 forbids anyway).

  > **Two corrections to r3b, both of which broke this gate outright (R4-4).**
  >
  > **1. The raw `includes` comparison is `false` for every multi-line helper.** r3b specified
  > `assert.ok(APP_SRC.includes(recipeSuffix.toString()))`. `Function.prototype.toString()` returns
  > the source **verbatim, including leading whitespace on every line**. In `app.js` the pure helpers
  > sit inside the IIFE at **2-space indent** (`app.js:141` `  function itemsForGroup(group) {`,
  > `:151` `  function esc(str) {`, `:162` `  function groupsForRender() {`); in `app.test.js` the
  > mirrors sit at **column 0** (`app.test.js:22`, `:42`, `:50`). Every continuation line therefore
  > differs, and the substring is never found. **Verified empirically**: taking `app.js`'s own `esc()`
  > text, dedenting it, and re-testing gives `APP_SRC.includes(...) → false` while the normalized
  > comparison gives `true`. Shipped as written, this gate fails on a **correct** implementation for
  > all eleven helpers — a red suite with nothing wrong, whose only obvious "fix" is to delete the
  > test that R3-19 added precisely because the convention had a measured 0% success rate. The
  > baseline suite is 52/52 green today under `node --test web/grocery/app.test.js`, so the failure
  > would be unambiguously self-inflicted.
  >
  > `norm` strips leading indentation per line and trims the ends. It is deliberately **not** a
  > whitespace-insensitive comparison — internal spacing, line breaks, names and punctuation must
  > still match exactly, which is the whole point of the gate.
  >
  > **2. The two `import` statements go at FILE TOP LEVEL**, beside the existing imports at
  > `app.test.js:7-8` — **not** inside the `describe('mirror integrity')` callback where r3b's
  > snippet visually places them. `import` is a static declaration; inside a function body it is a
  > **`SyntaxError`**, which is a parse-time failure: the module never executes and **all 52 baseline
  > tests die with it**. Only `const APP_SRC`, `norm` and `APP_N` may live wherever is convenient;
  > top level is simplest for those too.
  >
  > Per **R4-24**, write the assertion for one helper and run it **before** mirroring the other ten.
  > If it is red on the first helper, the gate is wrong — not the helper.
  > This is what makes Q3's verdict defensible rather than merely traditional. The convention has
  > already failed four times in this very file (see Q3's note, including `removeGroup` at
  > `app.test.js:58` versus `app.js:541`); "keep them in sync" as a discipline has a measured 0%
  > success rate here. The pre-existing four are **not** retrofitted into this test — that is churn
  > in a file the other branch may touch — but nothing this plan adds may drift silently.
- **AC-9.2 — the real gate is server-side (R-19).** r1 proposed asserting in `app.test.js` that
  `applyRecipeToggle`/`displayName` never write `item.name`. Keep that assertion, but **demote it to
  supporting evidence**: it tests a *test-local copy*, so it cannot prove what ships. **AC-9.2's
  gate is T4's handler test "rename leaves items untouched"**, which exercises the real store and
  the real route.

- **AC walk — the three criteria the 2026-09-10 amendments added (r3b).** T10's done-when requires
  that *every* AC-1…AC-11 line map to a named green assertion or a numbered completed manual step.
  These three arrived after r3 and are the ones an executor would otherwise reach the end of the walk
  without a home for, so their mapping is fixed here:

  | AC | FRD | Implemented by | Gate |
  |---|---|---|---|
  | **AC-8.8** | FRD:435 | **Implemented T7** (`setAllCollapsed`'s `activeTab` branch) / **gated at T8, step 19** (R4-6) | §5.3 e2e **step 19** — Collapse all collapses **and** Expand all expands every recipe card, and the two tabs keep independent collapse state. Run at **T8**, not T7: T7's `renderRecipesTab()` is a stub, so there are no card bodies for the branch to act on |
  | **AC-9.6** | FRD:449-450 | T8's `revealRecipe`, called from T9's `gc` chip arm | §5.3 e2e **step 18** — switch + expand + scroll in one gesture, plus the already-expanded and deleted-recipe cases |
  | **AC-9.7** | FRD:451-454 | `esc()` at every recipe-name site (T8 card title, T9 suffix) and `CSS.escape` at every id-derived selector (T8 ×2) | The **named two-site `Chili <b>` assertion** in T9's done-when, executed as §5.3 e2e **step 13** |

  **None of the three gets a unit gate, and that is deliberate (R-19).** `setAllCollapsed`,
  `revealRecipe` and `buildRow` all read module-scope state and write the DOM; none is a pure helper,
  so none can be inline-mirrored into `app.test.js` without testing a copy that is not what ships —
  the precise failure R-19 disqualified for AC-8.4 and AC-9.2. A mirrored `revealRecipe` would also
  need a DOM, which this suite does not have and which §0 forbids adding. Manual steps are the honest
  gate here; the mirror-integrity test below covers only the pure helpers.

**Done when:** `node --test web/grocery/app.test.js` passes with the new count (52 baseline → ~90+);
`make test` passes; and every AC-1…AC-11 line in FRD §9 maps to a **named** green assertion or a
**numbered** completed manual step in §5.3 — **including AC-8.8 → step 19, AC-9.6 → step 18 and
AC-9.7 → step 13 per the table above**, which are the three lines the walk could not discharge before
r3b. Plus the three universal gates.

---

## 5. Verification

### 5.1 Per-task commands

| Task | Command | What proves success |
|---|---|---|
| T1 | `go build ./... && go test -race ./internal/grocery/` | Compiles; back-compat + round-trip green. **AC-1.3 gate is `TestPersistence_NoRecipeKeysForFreeItems`** — the on-disk bytes contain neither `"recipes"` nor `"recipe_id"` (R-12; replaces r1's two mutually exclusive manual gates, neither of which was runnable without a `grocery.json` the repo does not ship). |
| T2 | `go test -race ./internal/grocery/ -run 'Recipe\|Ingredient' -v` | Every AC-3/AC-4/AC-5.3-5.5 test named and passing. **The revision-delta suite is the AC-1.5 proof and the PM-2a canary**: delta 1 for a 3-ingredient `DeleteRecipe` (delta 3 ⇒ per-item `save()`; a hang ⇒ the reentrancy deadlock), delta 1 for a both-fields `PatchRecipe` (delta 2 ⇒ r1's compose-of-two design regressed in), delta 0 for a neither-field patch. |
| T3 | `go test -race ./internal/grocery/ -run 'BulkSync\|Delete\|Reset' -v` | The PM-1 resurrection test proves a deleted ingredient stays deleted across a stale bulk sync. |
| T4 | `make test` | Fully green, **including every pre-existing handler test unchanged** — `Reset()`'s signature no longer moves (D-3), so `TestHandlerReset` (`handler_test.go:156`) needs no edit. `notifyCountC` asserts exactly one `Notify()` per mutating recipe route and zero for a no-op patch (AC-1.5's second half). **No `curl`, no running server** (R-19). |
| T5 | The four gates in T5 | `id="title-modal"` → 1 (AC-11.1), `<script src="app.js">` → 1 (R-2), and `<div` count == `</div>` count (R3-24, the off-by-one catcher). Then load the page, click the title, rename, reload — the new title persists (AC-11.2). |
| T6 | `sed -n '1,1076p' … \| shasum -a 256` + `wc -l` | The prefix hash still equals `cdc3bb1f…cea9` ⇒ not one byte of the original 1076 lines changed, and the line count grew ⇒ the new rules were appended. One gate replaces r2's eight, none of which could detect an in-place selector edit (R3-6). Visual check at phone width + desktop. **No `git`** (R-7). |
| T7 | `node --test web/grocery/app.test.js` + the five greps in T7 + DevTools | Existing 52 tests still pass. `grep -c 'render()'` is **25**, reconciling against T7's itemized table (R-8, R3-3), and `grep -c 'fetchRecipes()'` is **0** (R3-1). Tab switches produce **zero** XHR and no EventSource reconnect (AC-8.3); reload restores the tab (AC-8.2); with `progress:false` the bar stays hidden on both tabs (R-11); with zero items and zero recipes neither empty state leaks across a switch (R3-18). **AC-8.8 is implemented here but not gated here (R4-6)** — T7 ships `renderRecipesTab()` as a `/* T8 */` stub, so `#recipes-container` is empty and step 19 has no card bodies to collapse; the gate is T8's. |
| T8 | Manual, two browser windows | Create/rename/toggle/delete a recipe and its ingredients; window B updates within one SSE tick. Sync off → every recipe control `disabled`, hint shown, footer submit inert; on → immediately live (AC-10). **PM-3 check, both paths:** (a) type in an ingredient input in window A, tick a checkbox in window B, confirm A keeps its text, caret and focus; (b) with no second window at all, add three ingredients in a row to one recipe without re-tapping the input, then remove an ingredient from a second card while mid-typing in the first. Path (b) is the one r2's SSE-gated guard failed (R3-2). **AC-9.6 check:** collapse a card, switch to Grocery, click that row's chip — tab switches, card expands, card scrolls into view; then delete the recipe in window B and click A's stale chip before its SSE tick — A switches tabs and does nothing else, **with a clean console** (step 18). **AC-8.8 check (moved here from T7 by R4-6):** on the Recipes tab, Collapse all collapses **every** card body and Expand all expands them all again, and the Grocery tab's group collapse state is unaffected in both directions (step 19). T8 is the first task at which real card bodies exist, so it is the first task at which this gate can be run at all. |
| T9 | Manual + `node --test` | Owned rows read `beef (Chili)` with no trash icon; free rows unchanged; the heading reads **Unallocated**; drag into Unallocated persists across reload; **a recipe named `Chili <b>` renders as literal text in *both* the Recipes-tab card title and the Grocery-tab suffix (AC-9.7, step 13)**; the chip reveals its recipe (AC-9.6, step 18). |
| T10 | `make test && node --test web/grocery/app.test.js` | Both suites green. Walk FRD §9 top to bottom and tick every numbered line against a named test or a numbered §5.3 manual step. **Steps 13, 18 and 19 are the r3b additions: AC-9.7, AC-9.6 and AC-8.8 tick against those and nothing else.** |

#### "The three universal gates" — definition (R4-7)

Every task's **Done when** block in §4 ends with the phrase *"Plus the three universal gates."* Until
r4 that phrase was never defined anywhere in this document, so it discharged to whatever the reader
guessed. It means exactly these three commands, run from the repository root, in this order, after
**every** task T1-T10 — not only after T10:

| # | Command | Passes when |
|---|---|---|
| **1** | `make test` | Exit 0. This is `go test -race ./...` across the whole module — not just `./internal/grocery/`. A task that only touched `web/` still runs it, because the point is to prove it did not touch Go. |
| **2** | `node --test web/grocery/app.test.js` | Exit 0, with **at least** the 52 baseline tests passing and **zero** skipped. The count only ever grows: 52 through T6, then upward as T10 lands mirrors. A drop in the count is a failure even if the run is green. |
| **3** | The **§0 allowlist scan** — **both** `find` commands from §0, the scoped one and the `-maxdepth 1` root scan (R4-18) | Every path either one prints matches `internal/grocery/*.go`, `web/grocery/*`, `docs/*.md`, or `.omc/plans/*.md`. Anything else — another package, a test outside the fence, `Makefile`, `package.json`, `go.mod`, a stray scratch file — is an out-of-scope edit and the task is **not** done. Create the marker (`touch /tmp/plan-task-marker`) as the *first* action of each task so the scan has a baseline. |

These are gates, not smoke tests: a task is **not** done while any of the three is red, regardless of
how complete its own feature work looks. Gates 1 and 2 together are §5.2's full-suite gate; gate 3 is
what makes §0's fence enforceable rather than aspirational. None of the three requires `git`, which
§0 forbids (R-7).

### 5.2 Full-suite gate (run after **every** task, not just T10)

```
make test                              # go test -race ./...
node --test web/grocery/app.test.js
```

`make test` is the only Go gate; there is no linter or typecheck target covering `web/grocery/`
(`npm run typecheck` covers only the TypeScript modules, and both `Makefile` and `package.json` are
off-limits per §0). Running these is fine; editing them is not.

### 5.3 Test plan by level

#### Unit

- **Go store** (`internal/grocery/store_test.go`, no HTTP): AC-1 (persistence, back-compat,
  `omitempty`, revision), AC-3 (ingredient creation rules), AC-4 (toggle semantics **including**
  two-recipe isolation and free-item isolation), AC-5.3-5.5 (cascade + mismatch), AC-6 (BulkSync
  hardening), AC-7 (reset). Style: hand-written `func TestX(t *testing.T)` using the existing
  `newTempStore` helper — **no table-driven rewrite of the file** (churn in a shared file).
- **JS pure logic** (`web/grocery/app.test.js`, `node --test`, no DOM): `recipeSuffix` including the
  dangling-`recipe_id` case, `displayName` defined in terms of it, ingredient filtering by
  `recipe_id`, `tabControlVisible`'s truth table for the three header buttons, **`progressBarVisible`
  (AC-8.4's gate, R3-4)**, `groupsForRender` + the Unallocated relabel, `applyRecipeToggle`, extended
  `applyReset`. Helpers inline-mirrored per FRD §10 (FRD:473) under T7's single mirror rule, **and
  the mirror-integrity block (R3-19) makes that rule mechanical rather than aspirational.**

#### Integration

- **Go handler** (`internal/grocery/handler_test.go`, `httptest` + a real `Store` on `t.TempDir()` +
  a real `broker`): all of AC-2, every status code (201/400/404/409/204/405), the exact payload
  shapes from FRD §5 (`{recipe, items}` on **every** PATCH including rename-only, `{items}` on
  DELETE — D-1), and the 409 guard with a follow-up `GET` proving survival. This is also where
  **AC-9.2's real gate lives**: "rename leaves items untouched" against the real store (T10's JS
  assertion is supporting evidence only, since it exercises a test-local copy).
- **Broker fan-out:** keep the eleven existing `notifyC` tests (`handler_test.go:405, 413, 424, 435,
  444, 451, 459, 467, 475, 484, 495`) untouched and add `notifyCountC` coverage for every new
  mutating route. **AC-1.5's "exactly one `Notify()`" is verifiable and is verified** — r1's claim
  that the depth-1 buffer made it impossible was wrong; that depth is `notifyC`'s own choice at
  `handler_test.go:70`, and `Broker.Subscribe` takes a caller-supplied channel
  (`internal/platform/broker/broker.go:48`; the matching `Unsubscribe` is `:51`). **Citation
  corrected in r4 (R4-21):** this sentence is about `Subscribe`, but cited `broker.go:24-33`, which
  is `Notify()` — the right range attached to the wrong function. The `24-33` citation is correct
  and must stay wherever it is attached to `Notify()` (see §0.2, which defends it against an earlier
  bad "fix"); it is wrong only here. Note the revision counter does **not** cover this:
  a handler calling `Notify()` twice still leaves `Revision()` at +1.
- **Config** (`internal/platform/config/config_test.go`): **run, never edited** (off-limits per §0) —
  asserts no regression. `GroceryConfig` gains nothing (FRD §4.3).

#### Concurrency — an honest note about `-race`

`Makefile:17` runs `go test -race ./...`, but **every existing test in `internal/grocery` is
sequential**, so `-race` currently catches nothing for this feature: a race detector with no
concurrent goroutines is a no-op guarantee.

**Resolved (R3-26): the executor adds one small concurrent store test in T2.** r2 offered two
options and told nobody to choose — an open fork in a plan is a defect, not a decision. The test:
N goroutines (8 is plenty) interleaving `AddIngredient`, `PatchRecipe(enabled)` and `Recipes()`
against one `Store` on `t.TempDir()` for a fixed iteration count, asserting only that it terminates
with no race report. ~20 lines, no new dependency, and it runs inside the `make test` gate the plan
already mandates after every task.

It is worth exactly one thing, and that thing is worth having: it is the **only** check in this plan
that would catch `sortedRecipesUnsafe()` — or any other `…Unsafe` helper — being called without the
lock held. That is a mistake this feature makes newly easy, because T1 and T2 add two more unexported
lock-required methods to a file that previously had one. The alternative, accepting that `-race` is
decorative here, was the honest reading of r2's status quo; it stops being honest once the plan
doubles the number of ways to get the locking wrong.

This test is **not** a substitute for `-race` coverage elsewhere and does not pretend to be.

Note this test would **not** cover D-11 (`Handler.groups`, unsynchronized) or D-14 (live pointers
marshalled after the lock is released) — both are pre-existing, both are deliberately deferred, and
a *concurrent handler* test would trip them. Do not add one.

#### End-to-end (manual — there is no browser harness in this repo, and this plan does not add one)

1. Two browsers side by side on the LAN. Create *Chili*, add beef / kidney beans / cumin. Enable it.
   Confirm the Grocery tab on **both** shows three `needed` rows in Unallocated with the `(Chili)`
   suffix and no trash icon. *(AC-3, AC-4.1, AC-8.7, AC-9.1, AC-9.3, AC-9.4)*
2. Drag `beef (Chili)` into Meats. Reload. It stays in Meats and keeps its suffix.
   *(AC-9.1, §6.2.3, §5.1 move-unchanged)*
3. Manually cycle `beef` to `not_needed`. Reload — it sticks. Disable then re-enable Chili — it
   returns to `needed`. *(AC-4.6, AC-4.7)*
4. Create *Tacos*, add beef. Disable Chili. Confirm `beef (Tacos)` stays `needed`. *(AC-4.5)*
5. Rename Chili → *Chili Verde*. Every suffix updates on the next render, on both browsers.
   *(AC-9.2, AC-2.4)*
6. **PM-3, remote path:** with browser A mid-typing in Chili Verde's ingredient input, tick a
   checkbox in browser B. A's input keeps its text, caret position and focus.
6a. **PM-3, LOCAL path — the case r2's guard missed entirely (R3-2).** In one browser, with no
    second window involved: add three ingredients to one recipe in a row **without re-tapping the
    input** — after each add the input must be cleared and still focused. Then start typing a fourth
    ingredient into that card and, mid-word, click the ✕ on an ingredient in a **different** recipe
    card. The first card's text and caret must survive, and focus must **not** jump into it.
    Under r2's `opts.source === 'sse'` gate every one of these renders ran unguarded, so this step
    failed on the first add. *(FRD §1.1)*
7. Reload browser A **while it is on the Recipes tab.** Three things must all be true, and r3b only
   checked the middle one:
   - **The tab chrome matches the restored tab (R4-3).** `#tab-recipes` carries `.active` and
     `aria-selected="true"`, `#tab-grocery` carries neither, and the eye / reset / groups header
     buttons are hidden. Seeding `activeTab` from `localStorage` sets a *variable*; only
     `setActiveTab(activeTab)` applies the chrome, so T7's init IIFE must call it after
     `await fetchItems()`. Without that call the page restores the Recipes *content* under the
     Grocery *header* — the bug this step exists to catch. Check it in DevTools, not by eye:
     `document.getElementById('tab-recipes').className` and `.getAttribute('aria-selected')`.
   - **The tab itself is restored** — the Recipes tab is showing, not Grocery. *(AC-8.2)*
   - **The recipe cards are populated** — the AC-8.5 check that the init-IIFE `fetchRecipesData()`
     call (T7 call site #1) exists.
8. Turn Sync off in A. Every Recipes control disables, the hint shows, the footer submit is inert.
   Turn it on — immediately live. *(AC-10)*
9. **PM-1, replacing r1's vacuous step (R-5), and self-contained as of r4 (R4-8).** r1's step read
   "with A offline, delete a recipe from B, then bring A back online; the deleted ingredients must
   not reappear" — which passes **unconditionally**, because the client never POSTs `/api/sync`
   (D-12), so nothing could resurrect them. r3b replaced it with an explicit exercise of the
   endpoint, but ended on `grep -c '"recipe_id"' → 0`, **an assertion that cannot pass**: by step 9
   *Tacos* still owns `beef`, and steps 1-8 leave several other owned items alive, so the count is
   non-zero on a **correct** implementation. It also silently depended on whatever earlier steps had
   left in the store. Both problems go away if the step brings its own recipe and asserts on *that
   recipe's id* rather than on the string `"recipe_id"`:
   ```sh
   BASE=http://<host>:<port>          # the configured server URL

   # 1. A throwaway recipe nothing else in this plan touches.
   GHOST=$(curl -s -X POST -H 'Content-Type: application/json' \
             -d '{"name":"Ghost"}' "$BASE/api/recipes" | sed -n 's/.*"id":"\([^"]*\)".*/\1/p')
   curl -s -X POST -H 'Content-Type: application/json' \
        -d '{"name":"ectoplasm"}' "$BASE/api/recipes/$GHOST/ingredients" > /dev/null
   curl -s "$BASE/api/items" | grep -c "$GHOST"        # → 1  (precondition: it is there)

   # 2. Snapshot WHILE the ghost still exists — this is the stale client state.
   curl -s "$BASE/api/items" > /tmp/snapshot.json

   # 3. Delete the recipe (browser B, or the API directly).
   curl -s -X DELETE "$BASE/api/recipes/$GHOST" > /dev/null

   # 4. Replay the stale snapshot. This is the resurrection attempt.
   curl -s -X POST -H 'Content-Type: application/json' \
        --data @/tmp/snapshot.json "$BASE/api/sync" > /dev/null

   # 5. THE ASSERTION: the ghost's id appears nowhere. Other recipes' items are untouched.
   curl -s "$BASE/api/items" | grep -c "$GHOST"        # → 0
   ```
   Step 1's `grep -c "$GHOST" → 1` is not decoration: it fails loudly if the id capture returned an
   empty string, which would make step 5's `→ 0` pass vacuously — the exact failure mode r1's
   version had. Assert on `$GHOST`, **never** on `"recipe_id"`. *(AC-6.2)*
10. Delete Chili Verde. The modal names it, states its exact item count, and warns that items moved
    into other groups are included. Confirm — its items vanish; Tacos' beef and all free items
    survive. *(AC-5.5, §6.1.4, PM-2b)*
11. `POST /api/reset`. All items `check`/uncompleted; every recipe switch is off. *(AC-7)*
12. Click the title on both tabs, rename, save. *(AC-11.2)*
13. **AC-9.7 — output encoding at *both* render sites (FRD:451-454, r3b).** Create a recipe named
    `Chili <b>` and add an ingredient to it. Confirm the literal text `Chili <b>` renders in **both**
    places a recipe name reaches markup — the **Recipes-tab card title** and the **Grocery-tab suffix**
    on the owned row — with no bold text and no broken row in either, and that the delete-confirmation
    modal names it literally too. **One site passing is not a pass**; they are separate `esc()` call
    sites in separate functions (T8 `renderRecipesTab`, T9 `buildRow`), and r3's version of this step
    checked only the row. **A fourth site to inspect, added in r4 (R4-17):** the ingredient
    delete button's `aria-label` — it interpolates the ingredient name into an attribute, so read it
    in the DOM inspector and confirm it holds the literal characters rather than a truncated or
    markup-broken value. Attributes are the encoding site people forget precisely because a broken
    one still *looks* fine on screen. The remaining static half of AC-9.7 (no un-`esc()`-ed
    interpolation, `CSS.escape` at every id-derived selector) is automated in T10's `APP_SRC`
    assertions and is not eyeballed here. This step is AC-9.7's manual gate. *(AC-9.7, R-17)*
14. **AC-8.1 — tab affordance.** Both tab buttons are present in the header; exactly one carries
    `.active` and `aria-selected="true"` at any moment, and clicking the other swaps **both**
    attributes in the same gesture. Check it in the DOM inspector, not by eye — a styled-but-not-
    `aria-selected` tab passes a visual check and fails the criterion. r2 had **no check of any kind**
    for AC-8.1 (R3-4). *(AC-8.1)*
15. **AC-8.4 + R-11 — header controls and the progress meter.** With config `progress:true` and at
    least one item: on Grocery the eye, reset and groups buttons and the progress bar are visible;
    switch to Recipes and all four are hidden; switch back and all four return. Then set config
    `progress:false` (the default) and repeat: the three buttons still behave as above, and the
    progress bar stays hidden on **both** tabs and after every switch. That second half is R-11's
    regression check — r2's only AC-8.4 evidence was a unit test asserting the opposite. *(AC-8.4 as
    amended by F-8, FRD:429-431)*
16. **Empty-state isolation with nothing in the list (R3-18).** Starting from zero items **and** zero
    recipes: on Grocery, `#empty-state` is visible and `#recipes-empty-state` is not; switch to
    Recipes and the two swap; switch back and they swap again. Then add one recipe and repeat — on
    Recipes neither empty state shows. This is the leak Q2 Option B names in its own cons column.
17. **AC-9.4 as amended (F-4, FRD:441-443), corrected in r4 (R4-1).** With at least one unallocated
    item, confirm the string "Unallocated" — never "No Group" — appears in **two** places: the
    **Grocery section heading** and the **Groups-modal hint**. That is the complete list.
    **The footer `<select>` is not a third site and never was.** `rebuildGroupSelect()`
    (`app.js:209-221`) populates the dropdown from `groups` only, and `app.js:9` declares
    `let groups = [];   // real groups only; NO_GROUP is virtual` with an explicit
    `// Never offer NO_GROUP as an add target` at `app.js:212`. There is no "No Group" option to
    relabel, so r3b's instruction to check the `<select>` — and to "move an item into it via the
    `<select>`" — was a step that fails on a correct implementation. **Do not add the option** to
    make the old step pass; suppressing NO_GROUP as an add target is deliberate behavior that §0
    puts out of scope.
    To prove the display/data split instead, use the mechanism that actually exists: **drag** an
    item into the Unallocated section, then reload. It is still there, and the stored group value is
    still the literal `"No Group"` — confirm with
    `curl -s "$BASE/api/items" | grep -c '"No Group"'` → non-zero while nothing on screen says
    "No Group". *(AC-9.4)*
18. **AC-9.6 — the recipe chip reveals its card (FRD:449-450, r3b).** On the Recipes tab, collapse
    *Tacos* with its chevron, and make sure there are enough recipes above it that it sits below the
    fold — otherwise the scroll is unobservable and the step proves nothing. Switch to Grocery. Click
    the `(Tacos)` chip on the `beef` row. **All three halves must happen in one gesture:** the Recipes
    tab becomes active, the Tacos card is **expanded**, and it is **scrolled into view** with no
    further input. Then, with that card already expanded and on screen, click the chip again — the
    viewport must **not** jump (`block: 'nearest'`).
    Note that "the chip" and "the `(Tacos)` suffix" are **the same element** (R4-9): the suffix is
    rendered as the clickable chip, not as plain text with a separate widget beside it. There is
    nothing else on the row to click.
    Finally, the **missing-card case**, restated in r4 (R4-9c). r3b asked for a *race*: delete Tacos
    in browser B and click A's stale chip "before its SSE tick lands." That is not a runnable manual
    step — the tick arrives in milliseconds over a LAN, the tester has no way to widen the window,
    and a step that cannot be performed on demand cannot gate anything. Exercise the same code path
    deterministically instead, with **no second browser and no deletion**:

    > **Do not do this by typing `revealRecipe('no-such-id')` into the console.** `app.js` is a
    > single IIFE — `(() => { 'use strict'; … })()` at `app.js:1` and `:938` — and it assigns
    > **nothing** to `window` (`grep -n 'window\.' web/grocery/app.js` → no matches). Every function
    > this plan adds is module-private, so a console call is a `ReferenceError` and tests nothing.
    > This is also why §0 must not be relaxed to "just export it for testing."

    Use the DOM instead, which needs no globals. In A's **Elements** panel, find the `beef` row's
    chip and change its `data-recipe-id` attribute to `no-such-id`. Then click it. The required
    result: the app switches to the Recipes tab and does nothing else — **no thrown exception, and a
    clean console**. That is the identical `null`-card branch the race was trying to reach, reached
    on purpose and repeatably. It matters because the chip is served by the delegated `gc` listener
    (`app.js:859`): a throw on a `null` card is invisible on screen while silently killing every
    other arm of that handler for the rest of the session. *(AC-9.6)*
19. **AC-8.8 — Collapse all / Expand all drive recipe cards (FRD:435, r3b; wording corrected in r4,
    R4-16).** **Run this step at T8, not T7** (R4-6): T7 ships `renderRecipesTab()` as a `/* T8 */`
    stub, so `#recipes-container` is empty and there are no card bodies for the branch to act on —
    the step would fail on correct T7 work.
    These are **two separate buttons**, not one toggle. r3b's "press it again" reads as a single
    control that alternates, which would send a tester looking for a button that does not exist.
    With at least three recipes, on the Recipes tab press **Collapse all**: **every** card body
    collapses. Then press **Expand all**: **every** card body expands — both directions, since
    AC-8.8 names both. Then switch to Grocery and press **Collapse all** there: the *group* sections
    collapse and the recipe cards are untouched. Switch back and confirm the Recipes tab still holds
    its own collapse state, in both directions. This is the gap D-6 recorded and F-9 closed; r3
    shipped the `setAllCollapsed` branch in T7 with nothing checking it. *(AC-8.8)*

#### Observability

Deliberately modest — this is a single stdlib Go binary with SSE on a home LAN. **No metrics
backend, no tracing, no Prometheus endpoint is proposed or needed.** What is actually available:

1. **The revision counter is the primary signal.** `Store.Revision()` is already exported over HTTP
   at `GET /api/revision` (`handler.go:315-321` — corrected from `315-322` in r4, R4-22; line 322 is
   blank, and an off-by-one citation is exactly the class of error that burned two review rounds).
   Every recipe test asserts an exact delta, and the
   manual check `curl -s "$BASE/api/revision"` before and after a recipe toggle must show `+1`. A
   delta > 1 means a mutation called `save()` more than once — the PM-2a near-miss and the R-1
   two-method regression, and the only cheap detector for either.
2. **Structured log lines on destructive and rejected paths only** — exactly two, using stdlib `log`
   with the repo's `"<module>: …"` prefix convention (`internal/obsidianoid/build.go:43`):
   - `grocery: recipe delete id=%s name=%q items_deleted=%d` — the one irreversible operation; the
     post-hoc answer to "where did my eight items go" (PM-2b).
   - `grocery: bulksync rejected new item id=%s recipe_id=%s` — makes Q4 Option A's silent drop
     visible in the journal (PM-1).

   Nothing else logs. Per-request logging would drown a Raspberry Pi's journal.
3. **SSE fan-out is observable through the existing tests**: `TestSSE_DataEventDelivered` already
   asserts a `refresh` reaches a live stream after a mutation; add the recipe-toggle equivalent.
   Cross-client delivery itself is only verifiable manually (e2e step 1) — accept that.
4. **The data file is the audit log.** `grocery.json` is a full-file atomic rewrite; `cp` it before a
   risky manual test and `diff` afterwards. Cheapest and most reliable observability this system has.
   *(`diff`, not `git diff` — §0.)*

---

## 6. FRD amendments — all nine applied

`docs/FRD-recipes-tab.md` contradicts this plan in the places below.

**All nine are now APPLIED.** F-4, F-7 and F-8 landed with r3 as pure corrections describing what
this branch ships. The remaining six were applied after r3, following a product-owner decision on
2026-09-10:

| Ref | Decision | Resulting FRD text |
|---|---|---|
| F-1 | Keep the asymmetry; document the client obligation. | New bullet in §5 Notes (**FRD:214-217**): the shapes are deliberately asymmetric, and after a `DELETE` the client drops the recipe from local state itself. |
| F-2 | Confirm the plan's resolution (D-3). | `POST /api/reset` row (**FRD:230**) now states the response shape is unchanged — a bare `[]Item` — with recipe changes reaching clients via SSE. |
| F-3 | **Keep the chip and gate it in full**, scroll-into-view included. | New **AC-9.6** (**FRD:449-450**): clicking an owned row's recipe chip switches to the Recipes tab, expands that recipe's card if collapsed, and scrolls it into view. |
| F-5 | Confirm the plan's resolution (T8). | §6.5 (**FRD:278-281**) now names the shared footer form and the per-card ingredient input among the offline-disabled controls. |
| F-6 | Confirm the plan's resolution (R-17). | New **AC-9.7** (**FRD:451-454**): recipe names are output-encoded everywhere they render; no name reaches markup without `esc()`, and id-derived selectors use `CSS.escape`. |
| F-9 | **Add the criterion** rather than marking the row best-effort. | New **AC-8.8** (**FRD:435**): on the Recipes tab, Collapse/Expand collapses and expands all recipe cards. |

**Three of these added gates that no r3 task owned. All three are assigned as of r3b.** AC-8.8 →
T7's `setAllCollapsed`, gated by §5.3 step 19. AC-9.6 → T8's new `revealRecipe`, called from T9's chip
arm, gated by §5.3 step 18 — this was the one that carried **new client work**, since r3's T8/T9 built
the chip and the tab switch but implemented neither the expand nor the `scrollIntoView`. AC-9.7 → the
existing `esc()`/`CSS.escape` requirements, gated by the named two-site assertion in T9's done-when and
§5.3 step 13. T10's AC walk carries the mapping table. Nothing here remains unassigned.

The table below is retained as the record of what each contradiction was.

| # | FRD location | Contradiction | Proposed amendment |
|---|---|---|---|
| **F-1** ✅ **APPLIED** | §5 table, rows 3-4 (FRD:202-203) | `PATCH` returns `{recipe, items}` but `DELETE` returns `{items}` only — asymmetric, so the client must delete the recipe from local state itself after a DELETE while a PATCH hands it back (D-1). | Either add a note making the asymmetry deliberate and stating the client obligation, **or** change DELETE to `{recipe, items}` for symmetry. This plan implements the FRD literally (AC-2/AC-5 assert the current shapes) and compensates in T8. |
| **F-2** ✅ **APPLIED** | §5.1, `POST /api/reset` row (FRD:230) | Says reset "sets every recipe `enabled=false`" without stating the **response shape**, and AC-7 (FRD:422-423) plus the live contract return a bare `[]Item`. An executor reading §5.1 alone may return `{items, recipes}`, which makes `app.js:341`'s `items = data` a non-array and throws on the next render (D-2). | Add to the row: *"Response shape is unchanged — a bare `[]Item`. Recipe changes reach clients via `Notify()` → SSE."* **Resolved in this plan** by keeping `Reset()`'s signature (D-3). |
| **F-3** ✅ **APPLIED** | §7.4.2 (FRD:342-343) | The clickable recipe chip has **no acceptance criterion** — AC-9 (FRD:438-444) tests only the suffix (D-4). | Either add `AC-9.6 — clicking an owned row's recipe chip switches to the Recipes tab and scrolls that recipe into view`, **or** move §7.4.2 to §11 (deferred). This plan builds it but gates nothing on it. |
| **F-4** ✅ **APPLIED** | §7.4.4 / AC-9.4 (was FRD:433, now **FRD:441-443**) vs `web/grocery/index.html:156` | AC-9.4 relabels the section heading to "Unallocated", but the Groups modal hard-codes *"Removing a group moves its items to **No Group** until reassigned."* No AC covers that string, so the UI would ship with two names for one bucket (D-5). | **Applied, and narrowed in r4 (R4-1).** AC-9.4 now reads: every user-visible occurrence of the `"No Group"` string reads "Unallocated" — the Grocery section heading **and the Groups-modal hint** — while the stored value remains `"No Group"` (renaming it stays deferred to §11). **The "group `<select>` option" clause is removed as a defect r4 corrected, not as a scope cut.** r3b's rationale ("the `<select>` was added because r2 relabelled only two of the three") was itself the error: there is no third site. `rebuildGroupSelect()` (`app.js:209-221`) never offers NO_GROUP as an add target (`app.js:212`), `groups` is documented as real groups only (`app.js:9`), and `index.html:112` is an empty `<select>` the code fills. The `<select>` is the add-form's destination picker (`app.js:875`), not a move control — moves go through drag — so relabelling it would have been a feature change, not a rename. T5 and T9 ship the two real sites. |
| **F-5** ✅ **APPLIED** | §6.5 (FRD:276-285) vs AC-10.1 (FRD:457) | §6.5 enumerates the offline-disabled controls as "enable switches, add, rename, delete, and reorder" and **omits the shared footer form**, which on the Recipes tab creates recipes (§7.3, FRD:330-336). AC-10.1 says "all Recipes-tab mutation controls" (D-9). | Add the footer form to §6.5's list explicitly. This plan reads AC-10.1 broadly and disables it (T8). |
| **F-6** ✅ **APPLIED** | §9, AC-9 group (FRD:437-454) | No criterion covers **output encoding of recipe names**, which now reach `innerHTML` in two new places (the Grocery-tab suffix and the Recipes-tab cards). `app.js` escapes every other rendered string via `esc()` (`app.js:151`), so this is a consistency gap, not a new policy (B17/R-17). | Add `AC-9.6/9.7 — a recipe named "Chili <b>" renders as literal text in both the Recipes-tab card and the Grocery-tab suffix; no recipe name is interpolated into markup without esc().` |
| **F-7** ✅ **APPLIED** | §6.1.3 (**FRD:244-245** — r2 cited 242-243, which is §6.1.4 *Delete*; a citation error neither r2 reviewer caught) | Says recipes are "editable by drag (mirrors the existing groups-modal drag implementation)". `attachGroupDrag` (`app.js:409-496`) is not reusable — it hard-codes `groupsList`, `.groups-modal-item`, `dataset.group`, and ends in `reorderGroups()` at :467 (R-14). **AC-2.6 (FRD:391) requires only that the endpoint set `Order`**, so nothing breaks. | **Applied.** §6.1.3 now reads *"Recipes render in `Order`, editable via per-card up/down controls. Drag-reorder of recipe cards is deferred to §11."*, and a matching bullet was added to §11 (**FRD:490**). |
| **F-8** ✅ **APPLIED** | §7.1 table (FRD:299-307) vs AC-8.4 (was FRD:424, now **FRD:429-431**) | §7.1 correctly says the progress meter is shown "per config", but AC-8.4 phrases it as a flat "hidden on the Recipes tab", which reads as though it should be *shown* on Grocery — it must not be when config sets `progress:false` (the default). r1's plan regressed exactly this (R-11). | **Applied.** AC-8.4 now adds: *"On the Grocery tab the progress meter's visibility continues to follow config (`progress` enabled **and** at least one item); the Recipes tab never shows it."* This turns R-11 from a plan-local decision into an acceptance criterion, and it is what `progressBarVisible` (T7/T10) is tested against. |
| **F-9** *(minor)* ✅ **APPLIED** | §7.1 table, Collapse/Expand row (FRD:303) | Says collapse-all "collapses recipe cards" on the Recipes tab; no AC covers it (D-6). | Either add an AC or mark the row *(no AC — best effort)*. |

---

## 7. ADR — Recipes as an owning layer over the existing item engine

**Status:** proposed, pending approval

### Decision

Implement the Recipes tab as a **second view over the one existing item store**: `Item` gains a
single `recipe_id` field; `Recipe` is a new record persisted in the **same** `grocery.json`; the
ingredient list is **derived** (`items where recipe_id == r.id`), never stored. All recipe semantics
live in `Store` methods that each take one lock, call `save()` exactly once, and are wrapped by thin
handlers that `Notify()` exactly once — including `PatchRecipe`, which handles `name` and `enabled`
in **one** method precisely so that a both-fields PATCH stays one mutation. The client gains one
`activeTab` variable and a zero-argument `render()` **dispatcher** over two tab render functions
inside the existing IIFE — no framework, no bundler, no TypeScript, no module extraction. Transient
per-card draft state lives in module scope (`recipeDrafts`, `focusRecipeId`), never re-derived from
the DOM. Routes use the
existing old-style `ServeMux` prefix + manual segment parsing. Work lands store → handlers → markup →
styles → client → tests, in ten independently verifiable tasks, **entirely within
`internal/grocery/*.go`, `web/grocery/*` and `docs/*.md`**.

### Drivers

1. **Data-loss blast radius.** Recipe deletion is the only N-record destructive operation in the
   codebase, over a full-file atomic rewrite. Semantics must be provable by `go test -race` before
   any HTTP route can reach them.
2. **`app.js` render entanglement.** 938 lines, one IIFE, **22** `render()` call sites, no module
   boundaries. Regressions in *existing* grocery behavior are the likeliest damage. *(r1 said "~40";
   measured, it is 22 — the verdict is unaffected, but the figure is now honest.)*
3. **Scope confinement.** A parallel branch is live. Every design choice that reduces the diff inside
   `internal/grocery/handler.go`, `web/grocery/app.js` and `web/grocery/style.css` is worth real
   points, and no choice may reach outside §0's allowlist.

### Alternatives considered

- **Store the ingredient list on `Recipe` (`ingredient_ids []string`).** Rejected by FRD §4.1: a
  stored list can diverge from the items map, needing reconciliation on every load, delete and bulk
  sync. Deriving makes dangling IDs unrepresentable at O(n) filter cost that is irrelevant here.
  *(Note: r1's D-8 fix, if "repaired" by adding a parallel `recipes []*Recipe` field to `Store`,
  would have reintroduced this exact bug — hence D-8's explicit warning.)*
- **A separate `recipes.json` / a separate `/recipes.html` page.** Rejected by FRD §3.6: creating an
  ingredient writes a Recipe row and an Item row; one file makes that a single atomic `save()`, two
  files make it a torn-state window.
- **Composing `RenameRecipe` + `SetRecipeEnabled` for PATCH** (r1's design). **Invalidated:** two
  locks, two `save()`s, `Revision()` +2, two `Notify()`s — it fails AC-1.5 (FRD:383) on the one body
  shape FRD §5 explicitly permits, and violates Principle 2. Replaced by a single
  `PatchRecipe(id, *string, *bool)` mirroring `PatchPayload`'s pointer-optional idiom
  (`store.go:118-121`).
- **Go 1.22+ method/wildcard route patterns** (Q1 Option B). Viable and, in fact, the **repo-wide
  convention** — `internal/todo`, `internal/obsidianoid`, `internal/slideshow` and
  `internal/menuserver` all use them, and `internal/grocery/handler.go` is the sole old-style
  holdout. Rejected here on **scope confinement and smallest diff** (restyling twelve existing routes
  in a file another branch may touch is exactly the churn §0 forbids) plus the `{"error":…}`
  envelope, which mux-generated 405s do not produce. *(r1 rejected it on the false ground that it
  would be "the first new-style patterns in the repo".)*
- **One parameterized `render(tab)`** (Q2 Option A). Rejected: it forces every existing call site
  through a branch it does not need and interleaves two DOM trees in one function.
- **Extracting shared pure helpers to an importable `helpers.js`** (Q3 Option B). **Rejected, not
  invalidated:** achievable without `type="module"` (a second plain `<script>` exposing a global, or
  `node:vm` in the test), so r1's structural argument was overstated. It is rejected because FRD §10
  (FRD:473) mandates inline mirroring and because a second script tag plus a new global is a larger
  diff than §0 tolerates.
- **Rejecting an entire `BulkSync` batch on any `recipe_id` anomaly** (Q4 Option B). Rejected on
  size, not on data loss. *(r1 called it invalidating because "the user loses every offline grocery
  edit"; that is false — `fetchItems()` at `app.js:885` already discards local state wholesale on
  sync re-enable, and per D-12 the client never uploads offline edits at all.)*
- **Client-side drag-reorder of recipe cards.** Descoped: `attachGroupDrag` is not reusable, so
  "reuse" meant an untested ~88-line copy behind no AC. Up/down buttons hit the same endpoint.
- **Vertical feature slices** (Q5 Option A). Rejected: it would ship the cascade-delete route before
  the cascade had store tests, directly against driver 1.
- **Changing `Reset()`'s signature to return `[]*Recipe`** (r1's D-3). Rejected: the return was never
  consumed, clearing `r.Enabled` needs no signature change, and keeping `([]*Item, error)` leaves
  `handleReset` and `TestHandlerReset` untouched — which also resolves D-2.

### Why chosen

It is the smallest change that satisfies all 11 AC groups **without leaving `internal/grocery/` and
`web/grocery/`**. Deriving ingredients removes an entire bug class instead of testing around it. The
single-lock/single-`save()`/single-`Notify()` invariant is already the store's contract, so recipe
operations inherit atomicity and the `Revision()` counter becomes a free correctness signal (AC-1.5)
that also detects both the PM-2a near-miss and any regression back toward r1's two-method PATCH.
Making `render()` a dispatcher costs a handful of lines and **zero** call-site churn — the cheapest
possible way to add a second view to an entangled IIFE. Keeping its signature at zero arguments
(r3) is what makes that true unconditionally: PM-3's focus guard needs no cooperation from the
caller, so the SSE path and the twenty-two local mutation paths are the same path.

### Consequences

**Accepted:**

- Near-duplicate rows are permanent and by design (§3.1). The suffix and chip are the only mitigation.
- Recipe editing is **unavailable offline** (§6.5). Real functional loss on a flaky LAN, deferred
  deliberately.
- **Recipes are reordered by up/down buttons, not drag** (R-14). FRD §6.1.3 is amended, not met
  (F-7). No AC regresses.
- `DELETE /api/items/{id}` gains a 409 the client can now receive. The client's optimistic
  `deleteItem` (`app.js:261`) removes the row locally *before* the request; if the button is ever
  rendered for an owned row, the row vanishes locally and reappears on the next SSE refresh. T9's
  button suppression is the only thing preventing that confusing flicker.
- **Two** Unallocated-facing strings now exist — the Grocery **section heading** and the
  **Groups-modal hint** (`index.html:156`) — that must be kept in agreement while the stored value
  stays `"No Group"` (D-5, F-4 as applied). Both are a display/data split: the label changes,
  `dataset.group` does not. **Corrected from "three" in r4 (R4-1):** the third string r3b counted
  was a footer `<select>` option that does not exist and never has — `rebuildGroupSelect()`
  (`app.js:209-221`) populates the dropdown from `groups` only, excluding NO_GROUP by design
  (`app.js:212`, `app.js:9`). Counting it made the consequence unfalsifiable and pushed the
  executor toward *adding* an option to satisfy it.
- `app.test.js` mirroring is now **mechanical for the helpers this plan adds** (R3-19) and remains a
  discipline for the four pre-existing ones, which are not retrofitted.
- `app.js` grows by roughly 300 lines in the same IIFE. Accepted now; it moves a future
  modularization from "nice" to "eventually necessary".
- **`internal/grocery/handler.go` stays the repo's sole old-style-routing file** — a deliberate,
  recorded inconsistency bought with a smaller diff on a shared-risk file.
- **`-race` stops being decorative**, but only barely: §5.3 mandates one ~20-line concurrent store
  test whose entire job is to catch an `…Unsafe` helper called without the lock. Every other
  grocery test remains sequential, and D-11/D-14 stay uncovered by deliberate choice.
- **The sync-toggle path now refetches config.** Replacing `await fetchItems()` (`app.js:885`) with
  `await refreshAll()` adds a `GET /api/config` that today's code does not perform on that path
  (R3-27). Deliberate and cheap — one request when the user flips a switch — and it means re-enabling
  sync picks up a server-side `groups` or `title` change too. Recorded because it is a behavior
  change no AC asked for.
- **`PATCH /api/recipes/{id}` is the only route that decodes its own body instead of using
  `decodeName`**, because it must distinguish "field absent" from "field empty". The cost is that it
  needs its own 400 `"invalid body"` arm (R3-13) and is the only path that can reach
  `ErrInvalidName` (R3-5) — two special cases that exist solely to buy PATCH's four-way semantics.
- **`DELETE /api/recipes/` (empty id) answers 404 where `handleItem` answers 400** (R3-29). A
  deliberate, recorded inconsistency: no AC covers it and adding the check would only contradict the
  store's own answer.

**Bought on credit — two tensions worth naming rather than burying:**

1. **Additive-only CSS versus FRD §7.2's "styled consistently with `.group-section`".** T6's hash
   gate forbids editing any of the original 1076 lines, so "consistent with" can only be achieved by
   **duplicating declarations** out of `style.css:507` (`.group-section`), `509-540`
   (`.group-header`, incl. the chevron rotation at 540), `562` (`.group-body`), `574`
   (`.group-body.collapsed`) and `461-486` (`.sync-toggle`) into the new `.recipe-*` selectors. That
   guarantees **undetectable drift**: change a group card's padding next month and the recipe cards
   silently stop matching, with no test, no gate and no compiler to notice. The genuinely correct
   answer — lift the shared values into `:root` custom properties and have both rule sets read them
   — requires editing existing blocks, which the fence forbids for good reason while a parallel
   branch is live. So this plan knowingly buys a smaller, safer diff **now** with a maintenance
   liability **later**, and it is the fence, not the design, that makes that trade. See Follow-ups.
2. **Rejecting `helpers.js` on FRD §10's authority is weak, and the mirror test is what saves it.**
   This plan proposes nine FRD amendments; treating that same document as binding on Q3 and nowhere
   else would be selective. The verdict stands, but the honest grounds are the concrete ones: a
   second `<script>` creates a load-order dependency and a second global on a page that has exactly
   one script today, and that is a larger diff than §0 tolerates on a shared file. What actually
   makes the verdict *defensible* rather than merely conventional is R3-19's mirror-integrity test —
   without it, this plan would be doubling down on a convention with a measured 0% success rate in
   this very file (four pre-existing drifts, including `removeGroup` at `app.test.js:58` versus
   `app.js:541`).
- **D-11 and D-14 are left in place**: `Handler.groups` is unsynchronized, and store methods return
  live pointers that handlers marshal after the lock is released (`handler.go:166, 218, 275, 295,
  310` — pre-existing for `*Item`, extended to `*Recipe` here). Both are pre-existing patterns; both
  would need a concurrent *handler* test to surface.
- **`os.MkdirAll` moves from `Add` into `save()`** (D-13) — a behavior *improvement* on every other
  mutation path, but it is a change to an existing function, recorded as such. It also forces a
  rollback into `Add` (R3-12), because the fold moves the failure point from before the map insert
  to after it. So D-13 is not the free win r2 presented: it buys universal directory safety at the
  price of touching the oldest write path in the store.

**Rejected side effects avoided:** no new dependency, no `go.mod` change, no build step for
`web/grocery/`, no data migration, no change to `GroceryConfig`, no change to any other module, no
security-surface change, and no git operation.

### Follow-ups

1. **Lift the shared card/toggle values into `:root` custom properties** and have `.group-section`,
   `.group-header`, `.group-body`, `.sync-toggle` **and** the new `.recipe-*` rules read them, so the
   two card styles cannot drift. This is the real answer to Consequences tension 1; it is impossible
   under T6's additive-only fence and should be the **first** CSS task once the parallel branch has
   merged. Until then, every change to a group card's appearance must be hand-mirrored onto the
   recipe card, with nothing to catch a miss.
2. **Reconcile the four pre-existing `app.test.js` mirror mismatches** — `applyReset` has no `app.js`
   counterpart at all; `removeGroup` collides with an unrelated async `app.js:541`; `itemsForGroup`
   and `groupsForRender` have different signatures in the two files. Then extend R3-19's
   mirror-integrity test to cover them, which is what makes the reconciliation stick.
3. **Fix `Handler.groups` unsynchronized access** (D-11) and **stop returning live store pointers**
   (D-14). Pre-existing; the next concurrent handler test trips both.
4. **Restyle `internal/grocery/handler.go` to Go 1.22+ route patterns**, matching the other four
   modules. Deliberately deferred to avoid a large diff on a shared-risk file (Q1).
5. **Replace nanosecond IDs with a monotonic counter** — D-7's `nextIDUnsafe()` is a contained patch,
   not a fix for the underlying scheme.
6. **Decide the fate of `syncToServer()`** (D-12) — either wire it up (and then AC-6's hardening
   becomes load-bearing rather than pre-emptive) or delete it. Out of scope here either way.
7. **Everything in FRD §11**: quantities/units, adopting an existing free item into a recipe (A-4),
   offline recipe editing with replay (§6.5), meal planning by date, recipe instructions/photos/
   import-export, drag-reorder of ingredients (A-6), drag-reorder of recipes (F-7), and migrating the
   stored `"No Group"` value to `"Unallocated"`.
8. **A real browser harness.** §5.3's e2e level is entirely manual, and PM-3 is the one defect in this
   plan that unit tests structurally cannot catch.

---

## 8. Confirmation

**Does this plan capture your intent?**

- `proceed` — hand off to the `ralph` skill, executing T1 → T10 in order
- `adjust [X]` — revise a task, an option choice, a discrepancy resolution, or an FRD amendment
- `restart` — discard and re-plan

**Open items needing a call before or during execution.** On approval these are copied to
`.omc/plans/open-questions.md` (an explicitly allowed path per §0); r2 asserted they had already
been written there, which was both untrue and outside its own allowlist.

~~1-4. F-1, F-3, D-6/F-9, F-2/F-5/F-6~~ — **all resolved by the product owner on 2026-09-10 and
applied to the FRD.** See §6 for each decision and the resulting FRD line numbers. One consequence
is carried forward as new work rather than as an open question:

~~1. AC-9.6 needs an owner. 2. AC-8.8 and AC-9.7 need checks.~~ — **both closed in r3b.** AC-9.6 is
assigned to T8's new `revealRecipe` and called from T9's chip arm; AC-8.8, AC-9.6 and AC-9.7 are gated
by §5.3 e2e steps 19, 18 and 13 respectively, and T10's AC walk carries the mapping table. See the
r3 → r3b revision log.

**No open items remain.** Every FRD amendment is applied, every acceptance criterion has an owning
task and a named assertion or numbered step, and nothing in this plan is waiting on a product-owner
call. `.omc/plans/open-questions.md` therefore records nothing for this plan beyond the resolved
history above.

*(§5.3's `-race` fork is no longer an open item — it is resolved in favour of the concurrent store
test, per R3-26.)*
