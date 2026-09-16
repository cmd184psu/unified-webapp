# Work Plan — UI Unification, Phase 1 (Foundation)

**Status: v5 FINAL (amended per iteration-5 reviews) — APPROVED, ready for execution**

*Iteration 5 closed the consensus loop: the Critic returned **APPROVE** with five mandatory pre-execution amendments (A–E) and the Architect, reviewing the same snapshot independently, returned two blockers (B1, B2) with specified fixes plus five mediums (M1–M5) and five optional polish items. All of them are applied; the record is the change log's subsection 7, "Amendments applied (iteration-5 close-out)". No section was renumbered and no verified fact was re-derived — this revision executes the approval's attached conditions, it is not a sixth iteration.*
**Scope:** Phase 1 of `docs/FRD-ui-unification.md` only. FRD §7 "Resolved Decisions" is settled input, not a subject of this plan.
**Branch:** `ui-upgrade`

**Verification baseline.** Every empirical claim below was run against this tree at `3ea95a5` (branch `ui-upgrade`) with go1.27.1, node v26.0.0, esbuild 0.28.0, tsc 5.9.3. Where a reviewer's finding rested on a stale or incorrect premise, this plan says so explicitly instead of silently adopting it (§8, and the tables below).

**The two rules this revision was written under.**

1. *One definition per fact* — a key, artifact, file set, or count appears in exactly one place. **Operational test before deleting an occurrence:** confirm that another occurrence still defines the *same* fact. v4 deleted occurrences that were the only definition of their fact; that is how the platform half of the plan disappeared without a ledger row.
2. *A gate is a command that was run* — and where a gate provably cannot run before execution it is labelled **[deferred]** and states what was verified in its place. §5's preamble states the census once.

**What Phase 1 delivers is fixed.** FRD `:457` lists six deliverables for Phase 1. All six are in scope here; §12's self-audit clause (a) maps each to the plan sections that execute it. Shrinking the deliverable set is not an option this revision weighed, because the FRD settled it.

---

## Changes from v4

Revision 5 resolves the iteration-4 Architect review (NOT SOUND — 8 blocking B1–B8, 13 medium, 6 precision nits) and the iteration-4 Critic review (ITERATE — a 10-item "to APPROVE" list plus 4 unscored questions). Both reviewers reached the same diagnosis and recommended the same remedy, and this revision does exactly that:

> **v4's data layer is verified exact and is kept verbatim. v4's compression (845 → 648 lines) deleted the platform half of the plan with no ledger record; that deletion is the defect, and it is reverted from v3.**

So v5 grows back past v4 and past v3. Length is not the variable being optimised; *one definition per fact* is, and a fact with zero definitions fails that rule as surely as one with two.

### 1 — Restorations from v3 (each was the only definition of its fact)

| Restored | Where it lives in v5 | Why its deletion was a defect |
|---|---|---|
| `web/shared/css/components.css` | D1, Step 1's Creates, A1.6, A1.10, A5.2 | It is FR-5's CSS home. Without it the modal lift has nowhere to put the ~80 lines `ensureStyles()` currently injects, and `shared.css` has no Phase-1 consumer at all — which is what made v4's D6 cascade claim and its `--color-primary-fg` justification circular (Critic M-1). |
| FR-5 modal lift: `web/shared/ts/modal.ts`, the re-export shim, the five untouched call sites, A5.1–A5.7 | Step 5, Step 7, §5 A5.x, ADR-003 | FRD `:457` deliverable. v4 mentioned "shared TS entry" and never lifted anything. **A5.5 — taskmaster's four dialogs exercised through the shim — is the only behavioural gate on C6** and was absent from v4 entirely. |
| The `@shared` `onResolve` externalization, the ESM-format driver assertion, and the two-sided bundle-shape criterion | Step 3 driver rule 10, Step 5, §5 A9.x, ADR-001 | Without it `@shared` either inlines (violating FRD §7.1) or silently downgrades to iife and emits `__require(…)` **with exit 0** — a runtime-only failure no other gate in the plan can see. |
| The FR-8 Go API — `SharedHandler` / `WithShared` / `MountShared` / `Muxer`, `shared_test.go`, the closer-ordering invariant, A8.1–A8.8, R7–R12, the `dispatcher_auth_test.go:100` mirror edit, and §8's FR-8.1 / FR-8.2 rows | Step 4, §5 A8.x, §6, §8, ADR-002 | FRD `:457` deliverable, and the subject of iteration-2's blocker 1 (a wrapper that erases `io.Closer`). v4 asserted A8.x against an API it no longer specified — Architect B2. |
| The sampler's Go and config half: `internal/sampler/build.go`, `knownModules` 13 → 14, `SamplerConfig.StaticDir`, `host_routing`, `local-test/config.json`, the `auth.modules` entry | Step 6, §5 A10.1 | v4's A10.1–A10.4 asserted properties of a page **no server route ever served** — Architect B6. A gate that cannot run at its own commit boundary is not a gate. |
| `docs/sampler-checklist.md` and `docs/adding-a-module.md` | Step 6's Creates/Modifies, A10.6, A10.8 | Three of v4's own follow-ups defer decisions *to the checklist* (§9 items 10, 11, and the legibility pass). A document that other rows depend on cannot be uncommitted. |
| Driver rule "no absolute path on any esbuild field" | Step 3 driver rule 3, R2 | Highest-probability cause of a G2 false failure: esbuild embeds **relative** source paths as bundle comments, so one `path.resolve` rewrites every comment in every artifact at once. |
| The `comm -12` token-overlap gate | Step 7, A9.5 | It is the mechanism behind R1 and R13. v4 kept the risk rows and deleted the mechanism. |
| `make check`, `make test-web`, `.PHONY` maintenance | Step 3, A7.15 | "Green at every commit" is human-run (no CI); these three targets are what make it one command. |
| ADR-001 and ADR-002 decision bodies with their rejected alternatives; ADR-003's real subject; ADR-005's live question | §10 | v4 compressed four ADRs to one sentence each, which is not an ADR — it is a decision with its reasoning removed. |

### 2 — Corrections (facts v4 got wrong; all re-read line by line for v5)

| # | v4 said | This tree says | Sites fixed |
|---|---|---|---|
| **B1** | `cmd/unified-webapp/main.go` | **`cmd/server/main.go`** (`Makefile:2` is `CMD := ./cmd/server`; `cmd/unified-webapp/` does not exist) | 6 explicit + 3 bare `main.go:303` references. §12 clause (e) greps the plan body for the phantom path and requires zero hits. |
| **B1** | `internal/platform/…` (an ellipsis inside a commit manifest) | The real list: `internal/platform/static/shared.go`, `internal/platform/static/shared_test.go`, `internal/platform/config/config.go` | Step 4, §4 C3 row. An ellipsis in a Touches column is unexecutable — §12 clause (b) now requires every manifest path to exist or be created by a prior commit. |
| **B4** | Delta 4's before-value is a "6px UA default" and its mechanism is `--radius-md` | `web/taskmaster/js/ui/modal.ts:39` is `var(--radius, 6px)` **and** `web/taskmaster/style.css:23` declares `--radius: 6px`, so the fallback never fires. The 6px is a *declared* value; the delta is real but its mechanism is `components.css` authoring `--radius-md` in the lifted rules | T7 row 4 |
| **B8 / roster** | T5a's header reads "(dark, forest, ocean, ember, rose)" | Its five themes are **obsidian, forest, ocean, ember, rose**. FRD FR-2 renames obsidianoid's `:root, [data-theme="dark"]` block to **`obsidian`** (`:190-191`); canonical `dark` comes from `web/todo/css/todo.css:3-17` | T5a header, A2.2, Step 1.3. The rename is stated **once**, in Step 1.3's preamble. |
| **M3** | T2's obsidianoid citations `:20-36` mapped token-by-token | Re-read `web/obsidianoid/css/themes.css`: radii `:20-23`, `--shadow-sm` `:24`, `--shadow-md` `:25`, `--space-1/2/3` all on `:26`, `--space-4/6/8` all on `:27`, `--text-xs/sm/base/lg` `:28-31`, `--font-body` `:32`, `--font-mono` `:33`, `--sidebar-width` `:34`, `--topbar-height` `:35`, `--transition` `:36`. **There is no `--space-5`, no `--space-7` and no `--text-xl` in that file** | T2, every donor cell |
| **M4** | T2 has 25 tokens | **27.** FRD `:171-174` mandates the full `--space-1..8` scale and `--text-xl`; `--space-7` and `--text-xl` have no donor and are **authored** | T2, A1.4, Step 1.2 |
| **M7** | `--font-body-fallback` / `--font-mono-fallback` are "obsidianoid's stack minus the first family" | Partly authored: obsidianoid `:32` is `'Inter', 'Segoe UI', sans-serif` and `:33` is `'JetBrains Mono', 'Fira Code', monospace`, so `system-ui` and `ui-monospace` are **additions** | T2's provenance column |
| **M13 / T7** | Six deltas, all inside modal DOM | Eleven, and one is **outside** modal DOM: C1b's `@font-face` registration makes `style.css:21`'s first-named `'JetBrains Mono'` actually resolve at five existing consumers | T7 rows 1–11, §11 principle 3 |
| **B7** | Step 6's byte-identity recipe and AX.1/P2's clean-tree gates, written as `[ -z "$(git diff …)" ]` inside a make recipe | **Proven always-pass:** make expands `$(git diff …)` as a *make* variable reference, which is empty, so the shell sees `[ -z "" ]` and exits 0 | G11 + `scripts/gates/*.mjs` (Step 3). No gate is a shell one-liner in a recipe any more. |
| **M-6 (Critic)** | `web-verify` wrote the expected artifact count twice in one recipe and read the generator through `$(shell …)`, which discards its exit status | Both fixed by the same move: `EXPECTED_ARTIFACT_COUNT` is exported once from `scripts/descriptors.mjs`, and `scripts/gates/artifacts.mjs` is one process that reads it, so a generator crash is a non-zero exit rather than an empty string | Steps 1.8 and 3, A7.3, A7.7 |
| nit | `Makefile:42` is the `web:` recipe body | `:42` is the `web:` target line; **`:43`** is the `@if [ ! -d node_modules ]` line being replaced | Step 3 |
| nit | `main.ts:217` "Backend build" / `:218` "Frontend build" | **`:218`** and **`:219`** (`:216` is the `Server` heading, `:217` is the Status row) | 4 cites: §2, Step 7, §8 row 8, ADR-004 |
| nit | tsconfig ships 10 `include` globs | **11** — 9 today + `web/shared/**/*.ts` + `web/sampler/js/*.ts` | 4 places: A7.8, Step 5, §8 row 1, and the companion doc's Q4 |
| nit | FRD `:210` carries puma's `--color-primary`; FRD §7 decision 6 is `:483-485` | **`:211`** and **`:482-485`** respectively (the Architect's suggested `:483-487` is also wrong) | Step 1.4, ADR-007, §7 Q3, companion doc |
| nit | `dependencies` is empty | It holds **five** runtime deps (`@xterm/addon-fit`, `@xterm/xterm`, `react`, `react-dom`, `react-router-dom`). G4's real claim is *unchanged*, not *empty* | G4 (the corrected claim lives in G4's text; no criterion asserts a dependency count) |
| nit | grocery has `--color-success` only in `-lite`/`-bdr` variants | It has **no `--color-success` token of any kind**; it has `--color-warning-lite` `:29` and `--color-warning-bdr` `:30` | T5c |
| nit | `dispatcher_auth_test.go:29-30` denies a goleak `TestMain` | That comment is **stale**: `func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }` is at **`:1241-1243`** | A8.3 |

### 3 — Additions original to v5 (neither reviewer raised them)

- **C2's Touches must include `web/taskmaster/js/bundle.js`.** ADR-004 changes how the frontend stamp is computed, so the commit that lands the driver necessarily re-stamps that artifact. A C2 whose manifest omits it cannot pass its own byte-identity gate.
- **G6 and v4's AX.4 — "no path-scoping exceptions" — were false at C1 and C1b.** Until C2 lands, `package.json:5-6` still embeds `$(date -u …)`, so **any** rebuild dirties `web/taskmaster/js/bundle.js` unconditionally. C1 and C1b therefore carry exactly v3's one documented byte-identity exemption, which **disappears at C2** and is asserted gone by A7.4. Stating "no exceptions" while shipping a mechanism that guarantees one is worse than stating the exception.
- **Every gate is a file.** Beyond fixing B7, this is what makes R19's mutation rule affordable: a gate you can run as one process (`node scripts/gates/artifacts.mjs`) can be run against a deliberately broken input in one command.
- **The `[deferred]` census is honest and appears once.** v4 claimed four; the true figure is most of them, because almost every gate asserts a property of code this phase has not written yet. §5's preamble states the census once and by number — it is the only place in this document where those numbers appear — and every other passage, this one included, refers to it by name as "the §5 census".
- **`tsconfig.json` is edited in exactly one commit.** v4's manifests had C4 and C5 both touching it (one glob each). Both globs now land in **C4**; `web/sampler/js/*.ts` matches nothing for one commit, which is harmless because `tsc` only errors when the *whole* `include` is empty. A7.8 is therefore `[deferred → C4]`, not `C4/C5`, and the C5 row no longer lists the file — one fact, one commit, one gate boundary.
- **The companion document is reconciled in the same revision.** `docs/OPEN-QUESTIONS-ui-unification.md` is the tracked, clone-surviving copy of §7 and had drifted with v4: it was stamped "reconciled against plan v4", cited the theme-stamp gate as A10.5, cited FRD decision 6 as `:483-485`, said the tsconfig union was "10 globs", and still described Q5's count guard in the `$(words …)`/`$(shell …)` form that Architect B7 proved cannot fail. All five are corrected there, so the two documents agree line for line.

### 4 — Self-audit result (§12, run before this revision was finished)

| Clause | Result |
|---|---|
| (a) Six FRD `:457` deliverables each map to named sections | **Pass** — table in §12. |
| (b) Every commit-manifest path exists in the tree or is created by a prior commit | **Pass** — §4's 7 rows name **57 path entries, 46 distinct** (one of them the `web/shared/public/fonts/**` group of 20 files); all resolve: **16** name files that exist in the tree today and **30** name files a commit at or before that boundary creates. Two ellipses eliminated. *(Recounted after the iteration-5 amendments. C1 gains three entries — `descriptors.mjs`, `list-artifacts.mjs`, `Makefile` — C2 loses the first two, and C4 gains `Makefile` for the one barrel-gate line: 10/7/9 → 13/5/10, so **55 → 57**. Distinct (46), existing (16) and created (30) are all unchanged, because every moved path was already counted somewhere and `Makefile` was already a repeat.)* |
| (c) Every gate is a command that was run, or carries `[deferred]` + substitute evidence | **Pass** — 0 unlabelled; the split is the §5 census, and every deferred criterion names the boundary where it first runs. |
| (d) Every list, set, and count is defined exactly once | **Pass** with one stated and unavoidable exception: the artifact count moves during the sequence (12 → 14 → 15), so §4 states the trajectory and `EXPECTED_ARTIFACT_COUNT` is the single executable definition. |
| (e) Neither phantom reference survives in the plan body | **Pass** — both greps return zero over §0–§11, extracted by pattern rather than by line number so the check cannot drift as the document is edited (rc=1 each). Each string survives exactly three times in the whole file, all of them records of the correction rather than live references: its row B1 above, §12 clause (b), and the command block in §12 clause (e). |

### 5 — Corrections to the reviews themselves

| Review claim | This tree | Consequence |
|---|---|---|
| Critic: "v3 → v4 is 1244 → 648 lines". | v3 is **845** lines. The Architect's 845 → 648 is the correct figure. | The compression was 23%, not 48%. The finding stands regardless — what it deleted is what matters, not how much. |
| Architect nit: FRD §7 decision 6 is `:483-487`. | It is **`:482-485`**. | Both v4's cite and the suggested correction were wrong; v5 cites the re-read range. |
| Architect B4 concludes delta 4 should be dropped. | The delta is **real** — `components.css` authors `--radius-md: 0.5rem` = 8px where the donor rule resolved `--radius` to 6px. | Only v4's *reasoning* was wrong (it invoked a UA default that `style.css:23` pre-empts). The row stays, with the correct mechanism and the correct before-value.

### 6 — Carried forward from v4 unchanged

v4's entire data layer: T1's 17 keys, T2's structure (extended by 2 FRD-mandated tokens), T5a/b/c's remaps, Step 1.4's contrast matrix and six derived cells, Step 1.5's scrim, the fonts arithmetic (18 staged − 3 = 15 shipped) and its digest-file mechanism, ADR-004's metafile-derived input set and its `c2c572876987` digest, ADR-006, ADR-007, and the generated `WEB_ARTIFACTS`. Both reviewers verified these independently and asked for no change to any of them. Preconditions stay P1–P6.

### 7 — Amendments applied (iteration-5 close-out)

The iteration-5 reviews closed the loop: the **Critic returned APPROVE** with five mandatory pre-execution amendments, and the **Architect** — reviewing the same snapshot independently — returned two blockers with specified fixes plus five mediums. This subsection records where each landed. **Nothing else was reopened**: no section was renumbered, no verified fact was re-derived, and the census, the token arithmetic and the donor tables are untouched.

| Item | Source | What it required | Where it landed |
|---|---|---|---|
| **A** | Critic | `make gates` must not invoke `check-shared-barrel.mjs` before C4 creates it; the Makefile line belongs with the script; not a C1 no-op stub; `bundle-shape.mjs`'s input set must be pinned to a descriptor field | Step 3's gate-target blocks and driver rule 9; the new `sharedConsumer` paragraph in Step 3; Step 1.8's third bullet; Step 5's "Why the barrel gate's Makefile line lands here" paragraph; §4's **Gate composition by boundary** table (row C4 carries the reasoning) |
| **B** | Critic | A9.4 relabelled `[deferred → C5]` and removed from C4's Gates column; A9.2 annotated and moved | §5's A9 preamble (input set = `sharedConsumer: true`, empty until C5); A9.1/A9.2 → `[deferred → C5/C6]`; A9.4 → `[deferred → C5]`; §4's C4 row ("**Not A9.x**"), C5 row (+A9.1, A9.2, A9.4) |
| **C** | Critic | Resolve the default-theme contradiction one way and propagate it | **`:root` is dark.** Decided and argued once in **ADR-007's Decision** (three ordered reasons) with the arithmetic-invariance paragraph beside it; propagated to ADR-007's Consequences and **R5**. The gate spec (`check-shared-css.mjs` clause 4, A1.3, A2.2) always encoded `dark` and is unchanged — verified, not assumed |
| **D** | Critic | A8.5 becomes the utuber-dedup criterion; `.mjs` Content-Type folds into A8.6; census preserved | §5 **A8.5** (three clauses, two guarded greps + the `:58` route) and **A8.6** (three-clause response-header contract); **R10**'s mitigation re-pointed at A8.6; Step 4's contract clauses 8 and 9; Step 4's Verification block. Census unchanged at 74 = 8 + 66, because the content was rewritten rather than a bullet added |
| **E** | Critic | Three citation errors | `style.css:20` → **`:21`** in **R1** and A9.5; `utuber/build.go:58-72` → **`:68-79`** harmonized to one location description across **§8 row 16**, **R14**, and Step 4's utuber paragraph |
| **B1** | Architect | The barrel is the declared **complete public surface**: re-export `THEMES`/`setTheme`; `check-shared-barrel.mjs` asserts an explicit allowlist; the shim stays narrower | Step 5's barrel body, its "Why `theme.js` is re-exported here" paragraph, and its **allowlist table** (6 named values + 4 types); **A5.2**'s barrel half; **A5.4** ("narrower than the barrel's allowlist by design"); **A5.7**; **A10.3**; **ADR-003**'s "Where the narrow surface is enforced"; **ADR-007**'s Consequences; Step 3's **driver rule 10** (barrel-completeness is the precondition the `@shared` rewrite rests on) |
| **B2** | Architect | Pull `descriptors.mjs` + `EXPECTED_ARTIFACT_COUNT` and the `web-verify`/`gates` targets into C1; record the `descriptors.mjs` edit trail; add Step 1's missing Verification block | New **Step 1.8** with its Verification block; Step 1's Creates/Modifies; Step 3's "What this step does *not* create" note and rescoped manifest; §4's C1 and C2 rows; §4's ordering-rationale bullets (C1-before-everything, and the four-commit `descriptors.mjs` trail); §7 Q5. `list-artifacts.mjs` moved with it, because A7.7 requires the gate to run the generator as a child process |
| **M1** | Architect | Name the confinement mechanism for A8.4 clause 5; reconcile the contract ↔ A8.4 mapping | Step 4's contract is now **nine clauses with a per-clause criterion table**; clause 4 names `os.OpenRoot(dir)` / `root.Open(rel)` on **go1.27.1** (verified present) and the `http.ServeContent` consequence, and states why `filepath.EvalSymlinks` was rejected; **A8.1** counts nine; **A8.4**'s six clauses each carry their contract-bullet tag |
| **M2** | Architect | — | Subsumed by Critic **D** |
| **M3** | Architect | Scope the "no `$(shell …)`" claim to recipes; say `Makefile:9` survives by design | **§2**'s build paragraph; **G11** and **A7.17** (recipe-scoped); **ADR-001**'s Consequences; **§7 Q5** |
| **M4** | Architect | Pick one direction for `gofmt`/`go vet` and be consistent | Direction chosen: **not** folded into `check`. **AX.7** says so explicitly and **A7.16** asserts `check`'s four prerequisites by name; the two commands stay their own line in Steps 4 and 6 |
| **M5** | Architect | Fix ADR-002's self-contradiction on router-agnosticism | **ADR-002**'s Consequences: agnosticism is a property of the `WithShared` dispatcher wrap; `MountShared` is deliberately ServeMux-only (A8.7, R8); "the interface is a seam, not a promise of portability" |

**Optional items — all ten applied**, each a one-line fix as the close-out's own rule requires; none needed new design, so none was skipped.

- Architect **M6** — §12(a)'s D1 row now names where the emitted `shared.css` is produced (Step 5 / C4 / A6.5), not only its authored sources.
- Architect **M7** — the same citation harmonization Critic **E** mandates: `internal/utuber/build.go` is described by **one** location everywhere (`:68-79` deleted, `:64-67` comment, `:58` rewritten).
- Architect **M8** / Critic **note 3** — Step 1.7 states **11** clauses, exactly, because Step 2 and A1.9 refer to "clause 10" by number.
- Architect **M9** — §8 row 7 now ledgers the filename deviation (`web/sampler/style.css` vs FRD `:405-406`'s `styles.css`) alongside the `js/` vs `src/` one. The FRD range was re-read while applying it: the sampler clause is `:405-406`, not the `:405-408` M9 cited nor the `:404` v5's row carried.
- Architect **M10** — the companion doc's four v4-era prose stamps are corrected: Q1's and Q2's "Plan v4 §3 …" citations, Q3's "corrected in v4" / "v4 makes the premise true", and Q5's Step-3-only pointer, which now reads "§3 Steps 1 and 3" because B2 moved `EXPECTED_ARTIFACT_COUNT` and `artifacts.mjs` into Step 1.8.
- Critic **note 1** — subsumed by Architect **M1**: Step 4's contract now carries a per-clause criterion table instead of the false blanket claim.
- Critic **note 2** — §12(a)'s D3 row cites **A7.1–A7.17**, so the tsconfig half (A7.8–A7.10) is no longer omitted.
- Critic **note 4** — clause 10 carries A1.9's **exactly 15** `@font-face` count, so gate and criterion match as every other A1.x pair does.
- Critic **note 5** — A9.5 states the gate is **live from C1** (not C2, since B2 moved the `gates` target there) and *load-bearing* at C6, so the deferral cannot be read as "not running yet".
- One consequential one-liner neither review raised: **R11** no longer names `http.ServeFile`, because M1's clause 4 changed which `net/http` helper serves the bytes.

**Amendment C did not touch the companion document.** Q3 concerns `--color-primary-fg`'s values and the obsidian-stamp premise; it never asserts a direction for the unstamped `:root` default, so the resolution propagates entirely inside this plan.

**Two places where an amendment was applied in a form the review did not write**, both stated rather than absorbed, because a synthesis that silently edits its own instructions is not auditable:

1. **B2 moved one more file than it named.** The Architect asked for `scripts/descriptors.mjs` and the two Makefile targets; `scripts/list-artifacts.mjs` moved with them. Its own rationale forces it: **A7.7** requires `artifacts.mjs` to run the generator **as a child process**, so a C1 that held the gate and the data but not the generator would still fail to run A7.7 at its own boundary. Two files, not one.
2. **The `descriptors.mjs` trail is four commits, not the Architect's "five".** C1 creates it; C4, C5 and C6 edit it; C2 and C3 do not touch it. Counted row by row against §4 — the recount is what turned the "five-commit" phrasing into "one create and three edits", and it is also what confirmed §12(b)'s entry total moves 55 → 57 rather than further.

**One reconciliation the two reviews needed from each other.** Critic **A** requires `check-shared-barrel.mjs` to be added to `make gates` **at C4**; Architect **B2** requires the `gates` *target itself* to exist **at C1**. Both hold simultaneously — the target moves, the line inside it does not — but only if the recipe's composition is stated at every boundary where it differs, which is what **G6** demands. §4's new **Gate composition by boundary** table is that statement: three scripts from C1 through C3, four from C4, with the `--allow` artifact-gate form named for C1/C1b. Without it the two amendments would read as contradictory.

---

## §0 — Scope

FRD `:457` is the Phase-1 row of the phase table, and it fixes this phase's deliverable set. All of it is here; D7 is the user's 2026-09-15 decision to fold FR-6 forward into this phase.

**How `:457`'s wording becomes D1–D6.** The line carries five semicolon-delimited clauses, and they cross-cut rather than map one-to-one, so the plan numbers **six** deliverables — the framing both iteration-4 reviews used:

| FRD `:457` clause | Deliverables |
|---|---|
| "`web/shared/` skeleton: tokens.css, themes.css (8 themes incl. puma), components.css base" | **D1** (the sheets) + **D5** (8 themes *as data*, which is what the sampler consumes) |
| "shared bundle build + `MountShared` + `/shared/` route" | **D1** (the bundle is a committed artifact) + **D2** (the route, FR-8) |
| "build script + tsconfig rework" | **D3** + **D4** — the stamp is not a separate FRD item but an unavoidable consequence: a descriptor-driven driver cannot satisfy G2 while `package.json:5-6` embeds `$(date -u …)` (ADR-004) |
| "Modal lifted from taskmaster `ui/modal.ts` into shared" | **D5** + **D6** — a lift with no consumer is unproven, so adoption on the donor is tracked separately |
| "**sampler module skeleton (FR-10) as the dev harness**" | **D5** |

No clause is unassigned, and no deliverable lacks a clause. §12 clause (a) maps each of the six onward to the sections that execute it.

| | Deliverable |
|---|---|
| **D1** | Create `web/shared/` — token CSS, theme CSS, **component CSS**, font CSS, and a shared TS bundle — and commit its built artifacts alongside the existing 12. |
| **D2** | Serve it from one route, `/shared/`, reachable on every one of the 13 module hosts, inside auth, via a new tested platform handler (FR-8). |
| **D3** | Replace `package.json`'s two `&&`-chained esbuild command strings with a descriptor-driven Node build driver, and give the test runner the same treatment. |
| **D4** | Replace taskmaster's `$(date -u …)` frontend build stamp with a content digest (ADR-004), so a rebuild with no source change produces a byte-identical artifact. |
| **D5** | **Lift `web/taskmaster/js/ui/modal.ts` into `web/shared/ts/modal.ts`** (FR-5) — tokenized, with its CSS moved into `components.css` — and ship all 8 themes of FR-1's vocabulary as data, plus `web/sampler/`, a page that renders every token in every theme and exercises all four dialogs. The sampler is the defect oracle for Phase 2+. |
| **D6** | Adopt `shared.css` **and the shared modal** on exactly **one** surface (taskmaster, via ADR-003's re-export shim) to prove the route, the cascade order, the `@shared` externalization, and the byte-identity gates end to end. |
| **D7** | **Self-host the font faces** under `/shared/public/fonts/` (FR-6, folded forward by user decision 2026-09-15), with license text and digests travelling alongside. *v3's wording — "no external font requests" — overstated this: the deliverable is that `web/shared/` requests nothing external. See G10 and §8 row 11.* |

**Not in scope:** migrating any module's own CSS to the tokens (Phase 3+); the login page (Q8); removing the 5 surviving Google Fonts `<link>`s (§9 item 4); rewriting taskmaster's five call sites off the shim (§9 item 13); `--color-surface-dynamic` and `--radius-xl` (Q2 — each deferred to the phase that migrates its one consumer).

---

## §0.1 — Preconditions

| | Precondition | State |
|---|---|---|
| **P1** | `gofmt -l .` is empty. | **Satisfied** — re-run for v5: **0 lines, repo-wide.** The four former offenders were fixed in `9e7eff9`, so v3's scoped-gofmt argument is obsolete and AX's gate is repo-wide (user-settled). |
| **P2** | **The cleanliness mechanism.** `scripts/gates/clean-tree.mjs <paths…>` runs `git status --porcelain --untracked-files=no -- <paths>` and exits non-zero if the output is non-empty, **paired** with a `git ls-files --error-unmatch` assertion over that commit's artifacts. Untracked files are ignored deliberately — `fonts-staging/` and verification screenshots are permanently untracked — so a `??` line is never a gate failure. | **Mechanism unchanged from v3/v4; implementation moved into a file (G11).** v4 wrote it as `test -z "$(git status …)"` inside a make recipe, which make evaluates as an empty variable reference — Architect B7. The gate is **per-commit and path-scoped to that commit's paths**, not "the tree is clean before Phase 1 starts". |
| **P3** | The 12 pre-existing committed build artifacts are byte-reproducible from a clean checkout. | **Verified for all 12** (8 JS + 4 `bundle.css`; taskmaster has no `bundle.css`), by rebuilding into a scratch directory and `cmp`-ing each. (Byte-valid only because esbuild embeds source-path comments relative to cwd, not to `outfile` — verified, and the reason for driver rule 3.) taskmaster reproduces *at a fixed instant*; it is not reproducible **across** instants, which is what D4 fixes and why C1/C1b carry the exemption in §4. Machine-checked from C2 onward by `make web-verify`. |
| **P4** | `web/shared/` must be written **idempotently** against a possible concurrent font-integration session. | **Unchanged.** C1b creates files; it never rewrites a file it did not create. |
| **P5** | `npx tsc --noEmit` exits 0. | **Satisfied** — verified: rc=0, `grep -c 'error TS'` = **0**. This is why there is no baseline file (Critic iteration-3 missing-item 6); v3's `docs/typecheck-baseline-phase1.txt` stays deleted. |
| **P6** | The `tools/baseline-shots/` side-car workstream (interaction scenes) is independent of Phase 1 and may be in flight. | **In flight** — `tools/baseline-shots/README.md` and `shoot.js` are modified in the working tree right now. Every commit in §4 names its paths and P2's gate is path-scoped, so these cannot be swept in. Risk R20. |

---

## §1 — Guardrails

- **G1 — No source file outside the listed paths.** Each commit in §4 names every path it touches. A commit touching anything else is wrong, not merely surprising.
- **G2 — Committed build artifacts stay byte-identical unless the commit's purpose is to change them.** Enforced by `make web-verify` over the generated `WEB_ARTIFACTS`. At C1 and C1b the same gate is invoked directly as `node scripts/gates/artifacts.mjs --allow web/taskmaster/js/bundle.js` — the flag *is* the exemption's mechanism, stated in §4 and retired by C2.
- **G3 — Phase 1 does not edit a module's own CSS, HTML, or TS**, with exactly **two** hand-edited exceptions, both in C6 and both enumerated as sanctioned deltas: `web/taskmaster/index.html` (two edits) and `web/taskmaster/js/ui/modal.ts` (390 lines → a 2-line shim). **`web/taskmaster/js/main.ts` is *not* edited** — the frontend stamp is consumed through `web/taskmaster/js/buildinfo.ts:11,13`, which declares `__TM_BUILD_TIME__` and re-exports it as `FRONTEND_BUILD_TIME`, so D4 changes how the define is *computed* and touches no module TS (Architect M1, verified). `web/taskmaster/js/bundle.js` is a generated artifact governed by G2, not a hand edit.
- **G4 — No new dependency.** `dependencies` and `devDependencies` are **unchanged** — not empty: `dependencies` holds 5 runtime packages and `devDependencies` holds 4 (esbuild ^0.28.0, typescript ^5.4.0, @types/react, @types/react-dom; **no `@types/node`**, which is ADR-005 driver 1).
- **G5 — Every route claim is proved by a two-sided probe:** the thing that should be served is served, *and* the thing that should not be reachable is not.
- **G6 — Gate composition is uniform.** Every commit runs the full gate set applicable to the tree at that commit. Composition differs only where the artifact a gate reads does not exist yet, and every such point is named in §4's Gates column. The **one** substantive exception is C1/C1b's byte-identity exemption (§4), which exists because `package.json` still carries `$(date -u)` until C2 — not because a gate was waived. See §4's exemption and A7.4.
- **G7 — No `.omc/` path is ever committed.** Already covered by `.gitignore`.
- **G8 — Reversibility.** Every commit in §4 is revertible in isolation except for the two stated ordering constraints: C1 → C1b (`index.css` cannot import a file that does not exist) and **C6 before C4** on the revert path (R16).
- **G9 — Binary assets carry provenance.** Any committed binary must have (1) a digest line in the tracked `web/shared/public/fonts/SHA256SUMS` — the only file in that tree allowlisted besides `*.woff2` and `OFL.txt` — and (2) its license text travelling alongside it.
- **G10 — `web/shared/` references no host but the origin serving it.** This is narrower than "the app makes no external requests": 5 Google Fonts `<link>`s in 4 module HTML files survive Phase 1 untouched, because removing them is an FR-6 edit to files G3 forbids this phase from touching. They are enumerated in §7 and removed by §9 item 4.
- **G11 — No gate is a shell one-liner inside a make recipe.** *(new in v5; closes Architect B7.)* Every gate is a script file invoked as one process, so its exit code is the gate. Two mechanisms make the one-liner form unsafe, both observed in v4: make expands `$(…)` in a recipe as a *make* variable, so `[ -z "$(git diff …)" ]` reaches the shell as `[ -z "" ]` and always passes; and `$(shell …)` discards the child's exit status, so a crashed generator reads as an empty list. `scripts/gates/` holds `artifacts.mjs`, `bundle-shape.mjs`, `token-overlap.mjs`, and `clean-tree.mjs`; `scripts/check-shared-css.mjs` and `scripts/check-shared-barrel.mjs` complete the set. A7.17 asserts the form.

---
## §2 — Current reality (verified, not assumed)

**Build.** `package.json:5` (`build`) and `:6` (`build:dev`) are single command strings chaining 7 esbuild invocations with `&&`. Both embed `$(date -u +%Y-%m-%dT%H:%M:%SZ)`; `grep -c 'date -u' package.json` = **2**. `Makefile:9`'s `BUILD_TIME := $(shell date -u …)` feeds Go ldflags at `:10` and is a **different** stamp, surfaced on its own UI row (`web/taskmaster/js/main.ts:218` "Backend build", `:219` "Frontend build"; `:216` is the `Server` menu heading and `:217` is the Status row). D4 changes only the frontend one: **`Makefile:9`'s backend stamp survives Phase 1 unchanged, by design**, and G11's "no `$(shell …)`" claim is scoped to make **recipes** on the gate path, not to variable assignments elsewhere in the file (Architect M3; ADR-001's consequences, §7 Q5).

**Build warnings today.** `npm run build 2>&1 | grep -iv 'log-level=warning' | grep -i 'warn'` → **rc=1**: zero real esbuild warnings. The inverse filter is required because npm echoes the script text, which itself contains `--log-level=warning` — the naive form matches npm's own echo and reports a false positive. This answers iteration-3's unscored question about whether warnings-as-failures breaks smbedit or issuetracker on day one: it does not.

**Test runner.** `package.json:8`'s `test:web` runs exactly **4** suites — `web/multissh/js/sshcommand.test.ts` and `web/certmachine/js/{status,listmodel,generate}.test.ts` — each as `esbuild --bundle --platform=node --format=cjs --target=node18 --log-level=warning | node -`. `web/grocery/app.test.js` exists, uses `node:test` and `import.meta.dirname` (`:16-17`), is wired to nothing, and is green when run directly (189 pass, 0 fail). Phase 1 wires it and adds `web/shared/ts/modal.test.ts`, for **five** suites (A7.13).

**Typecheck.** `tsconfig.json:11` lists **9** literal `include` globs, with `moduleResolution: "bundler"` (`:5`), `strict` (`:7`), `noEmit` (`:8`), and no `baseUrl`/`paths`. None of the 9 matches `web/shared/` or `web/sampler/`, neither of which exists yet. `npx tsc --noEmit` exits 0 with zero errors.

**Artifacts.** **12** built files are committed today — 8 JS (`web/obsidianoid/js/{app,threads}.js`, `web/slideshow/js/app.js`, `web/{multissh,certmachine,taskmaster,smbedit,issuetracker}/js/bundle.js`) and 4 CSS (`web/{multissh,certmachine,smbedit,issuetracker}/js/bundle.css`). taskmaster has no `bundle.css` because it imports no CSS. All 12 are tracked. Phase 1 adds 3 — `web/shared/dist/shared.mjs`, `web/shared/dist/shared.css`, `web/sampler/js/bundle.js` — for **15**.

**Serving.** 13 modules are dispatched by `Host` header; taskmaster uses chi, the other 12 use `*http.ServeMux` (9 mount static from their `build.go`, 3 own the mux inside a constructor). `knownModules` is a 13-name slice at `cmd/server/main.go:337`. The dispatcher loop is `buildDispatcher` at `:289-311`:

```go
hh, err := buildModule(module, cfg, svc)            // :295
if err != nil { log.Printf(…); h = unavailableHandler(module, err) }   // :297-298
else {
    if c, ok := hh.(io.Closer); ok { dispatch.closers = append(dispatch.closers, c) }   // :300-302
    h = middleware.BodyLimit(limitFor(module, cfg), svc.Gate(module, hh))               // :303
}
```

Two facts this plan depends on: the `io.Closer` assertion is made on the **direct** `buildModule` return and happens **before** the wrap at `:303`, and `unavailableHandler` is installed **outside** the per-module wrap. The first is what makes Step 4's mount unable to erase a closer; the second is what makes A8.2 assertable. `cmd/server/dispatcher_auth_test.go` replicates the loop at `:87-107` (closers at `:97-99`, the mirror `BodyLimit` line at **`:100`**, without `Gate`) and runs `goleak.VerifyTestMain` from `TestMain` at **`:1241-1243`** — note that the file's own header comment at `:29-30` denies having one and is **stale**.

**Existing static handler.** `internal/platform/static/static.go` is 30 lines: `Handler` `:14-16`, `NewHandler` `:19-21`, `ServeHTTP` `:23-30`. It joins `filepath.Clean("/"+r.URL.Path)` onto its dir, stats, and falls back to `index.html` on `IsNotExist`. It does no prefix stripping and suppresses no directory listing, so a typo under `/shared/` would return someone else's `index.html` with status 200. `SharedHandler` is therefore a new type in a new file, not a reuse. `internal/utuber/build.go:58` mounts a **byte-identical private copy** of it (`type staticHandler` `:68-70`, `ServeHTTP` `:72-79`, with a duplicate-acknowledging comment at `:64-67`) — FR-8.2's dedup target.

**No CI exists.** There is no `.github/`. "Green at every commit" is a human-run obligation; Step 3's three `make` targets make it one command.

**taskmaster's page.** `web/taskmaster/index.html` is 14 lines: `:2` is `<html lang="en">`, `grep -c 'data-theme' …` = **0** (rc=1); `:7` links `/style.css`; `:12` loads `/js/bundle.js` as `type="module"` — so the iife → esm switch in C6 needs no HTML change. C6 makes **two** edits here.

**taskmaster's tokens.** `web/taskmaster/style.css` (820 lines) declares exactly 17 custom properties on `:root` at `:6-24`, including `--font-mono: 'JetBrains Mono', 'Fira Code', 'Cascadia Code', monospace` `:21`, `--font-ui` `:22`, and **`--radius: 6px` `:23`**. There is **no `html {` rule** (count 0), so the root size is the 16px UA default; `body { font-size: 14px }` is at `:32`. `var(--font-mono)` has **five** consumers (`:288`, `:499`, `:613`, `:671`, `:736`) — v3 said one. The string `Inter` appears nowhere in the file. `grep -c 'tm-modal' web/taskmaster/style.css` = **0**, and the file declares no `.ui-`-prefixed class, so C6's `tm-` → `ui-` rename creates zero collisions (R17).

**Palette donors — all three are renames.** There is no file in the tree that already speaks FR-1's vocabulary:

- `web/obsidianoid/css/themes.css` — 5 theme blocks (`:2` `:root, [data-theme="dark"]`, `:40` forest, `:78` ocean, `:116` ember, `:154` rose), **17** `--color-*` keys each, plus structural tokens at `:20-36`. Its names include `--color-surface`, `--color-surface-offset`, `--color-primary-highlight`, `--color-error`, `--color-surface-dynamic` — none of which is canonical — and it declares **no `--color-primary-fg`**.
- `web/grocery/style.css` — near-canonical literals at `:15-33`, `--space-5: 1.25rem` at `:12` (the sole donor for it, as FRD `:171` says), radii `:39-43`, shadows in `rgb()` `:44-46`, text clamps `:6-9`.
- `web/todo/css/todo.css` — `[data-theme="dark"]` `:3-17` and `[data-theme="light"]` `:19-33`, 13 declarations each (12 colours + `--shadow`), using **no `--color-*` name at all**.

**Fonts.** `fonts-staging/` is untracked (ADR-006) and holds **24** non-`.omc` files: 18 woff2 across 4 SIL-OFL-1.1 families (Inter 5, JetBrains Mono 4, Sora 5, IBM Plex Mono 4), 4 `OFL.txt`, `NOTES.md`, and a `SHA256SUMS` covering all 18.

**Surviving external requests (G10).** 5 Google Fonts `<link>` groups across 4 modules: `web/grocery/index.html:8-10`, `web/utuber/index.html:7-9`, `web/todo/index.html:8-10`, `web/todo/compare.html:8-10`, `web/smbedit/index.html:7-9`. **FRD FR-6's affected-module list at `:338-341` names only grocery, smbedit, and todo** — it omits utuber and `todo/compare.html`. §7 records the correction.

---

## §3 — Steps

One step per commit, in landing order: Step 1 → C1, Step 2 → C1b, Step 3 → C2, Step 4 → C3, Step 5 → C4, Step 6 → C5, Step 7 → C6. §4 states the ordering rationale and the dependencies.

### Step 1 — `web/shared/css/`: tokens, themes, components (C1)

**Creates:** `web/shared/css/tokens.css`, `web/shared/css/themes.css`, `web/shared/css/components.css`, `web/shared/css/index.css`, `scripts/check-shared-css.mjs`, `scripts/gates/{artifacts,bundle-shape,token-overlap,clean-tree}.mjs`, `scripts/descriptors.mjs`, `scripts/list-artifacts.mjs`
**Modifies:** `.gitignore`, `Makefile` (adds the `web-verify` and `gates` targets; `.PHONY` 9 → 11)

`index.css` `@import`s in cascade order: `tokens.css` → `themes.css` → `components.css` (and, from C1b, `fonts.css` first). `components.css` carries **only** `.ui-modal-*` rules in Phase 1 — the retokenized bodies of what `web/taskmaster/js/ui/modal.ts`'s `ensureStyles()` (`:20-106`) injects today, which Step 5 deletes. It is the **Phase-1 consumer of `shared.css`**: without it, `shared.css` is a sheet of custom properties no rule reads, and every claim about cascade order and about `--color-primary-fg` having a consumer is circular (Critic M-1).

#### 1.0 How a page selects a theme

Stated once, here.

All tokens are declared on `:root`. A theme is selected by a `data-theme` attribute **on the `<html>` element**. It must be `<html>`, not a wrapper: `:root` *is* `html`, so a `[data-theme="x"]` rule targeting a container would create a second, lower-specificity origin for the same custom properties, and `:root`'s values would win everywhere outside that container. The sampler therefore sets `document.documentElement.dataset.theme`; C6 stamps a literal attribute.

**Which theme governs which surface is that surface's own decision, in that surface's own file.** Phase 1 decides it for exactly one surface: after C6, taskmaster renders under `[data-theme="obsidian"]`. This implements FRD §7 decision 6 (`:482-485`) — "Its `ui/modal.ts` is the donor… the violet look preserved by the `obsidian` theme" — so it executes a settled decision rather than making a new one. The other 12 modules are untouched and continue rendering however they do today. ADR-007.

**Passages this propagates through** (the complete list, so a reviewer can check that none still assumes the old premise): (1) this subsection; (2) Step 1.4's obsidian contrast exemption; (3) §2's "taskmaster's page" paragraph; (4) Step 7's C6 description; (5) T7's rows that cite obsidian literals (1, 2, 3, 6, 7, 8, 9, 10); (6) §4's C6 row; (7) gate **A10.7**; (8) §7's Q3 closure; (9) the companion doc's Q3 closure; (10) §8 row 5; (11) §9 item 1; (12) §6 risk R21; (13) ADR-007.

#### 1.1 Table T1 — the canonical `--color-*` set (defined once)

These 17 keys are the complete per-theme colour vocabulary. **No other section re-enumerates them.**

`--color-bg`, `--color-surface-1`, `--color-surface-2`, `--color-surface-3`, `--color-border`, `--color-divider`, `--color-text`, `--color-text-muted`, `--color-text-faint`, `--color-primary`, `--color-primary-hover`, `--color-primary-active`, `--color-primary-tint`, `--color-primary-fg`, `--color-danger`, `--color-success`, `--color-warning`.

**Provenance of `--color-primary-fg`: authored by this phase, no donor.** `grep -rn -- '--color-primary-fg' web/` → **rc=1**. It appears in no donor file and in no FRD code block, the puma block included. Every claim in this plan about the shipped CSS matching the FRD or a donor must therefore exempt it — A2.1 and A2.4 do. **Its Phase-1 consumer is real:** `components.css`'s `.ui-modal-btn-primary { color: var(--color-primary-fg) }`, which is the retokenization of `modal.ts:99`'s `color: #fff`. That is why the key is authored now rather than deferred, and why Step 1.4's measurement is load-bearing rather than theoretical.

#### 1.2 Table T2 — structural tokens (27, defined once)

Declared on `:root` in `tokens.css`. `themes.css` may override only the three marked ★ (A1.2). All obsidianoid citations below were re-read for v5; v4's were off (Architect M3).

| Token | Value | Donor / note |
|---|---|---|
| `--radius-sm` | `0.25rem` | obsidianoid `:20`. **Conflict:** grocery `:39` says `0.35rem`. |
| `--radius-md` | `0.5rem` | obsidianoid `:21` = grocery `:40`. **= 8px at the 16px root** — the fact behind sanctioned delta 4. |
| `--radius-lg` | `0.75rem` | obsidianoid `:22` = grocery `:41` |
| `--radius-full` | `9999px` | obsidianoid `:23` |
| `--shadow-sm` | `0 1px 3px oklch(0 0 0 / 0.3)` | obsidianoid `:24`. **Conflict:** grocery `:44` uses `rgb()`. |
| `--shadow-md` | `0 8px 32px rgba(0, 0, 0, 0.4)` | **`web/taskmaster/js/ui/modal.ts:44`** — deliberate divergence from obsidianoid `:25` (`0 4px 16px oklch(0 0 0 / 0.4)`), because the donor modal's own shadow must be reproducible from the token or C6 changes it. §8 row 12. |
| `--space-1`, `--space-2`, `--space-3` | `0.25rem`, `0.5rem`, `0.75rem` | obsidianoid — **all three on `:26`** |
| `--space-4`, `--space-6`, `--space-8` | `1rem`, `1.5rem`, `2rem` | obsidianoid — **all three on `:27`** |
| `--space-5` | `1.25rem` | **`web/grocery/style.css:12`** — obsidianoid has no `--space-5`; FRD `:171` names this exact gap. |
| `--space-7` | `1.75rem` | **Authored — no donor.** FRD `:171-174` mandates the full `--space-1..8` scale; neither donor declares `--space-7`. Value continues the scale's `0.25rem` step. *(New in v5 — Architect M4.)* |
| `--text-xs`, `--text-sm`, `--text-base`, `--text-lg` | obsidianoid `:28`, `:29`, `:30`, `:31` clamps | obsidianoid. **Conflict:** grocery `:6-9` clamps differ. |
| `--text-xl` | `clamp(1.5rem, 1.32rem + 0.75vw, 2rem)` | **Authored — no donor.** FRD `:172` names it; neither donor declares it. Continues obsidianoid's clamp geometry one rung up. *(New in v5 — Architect M4.)* |
| `--font-body` | `"Inter", var(--font-body-fallback)` | restructured from obsidianoid `:32` (`'Inter', 'Segoe UI', sans-serif`); §8 row 13 |
| `--font-mono` | `"JetBrains Mono", var(--font-mono-fallback)` | restructured from obsidianoid `:33` (`'JetBrains Mono', 'Fira Code', monospace`) |
| `--font-body-fallback` | `"Segoe UI", system-ui, sans-serif` | **Part donor, part authored:** `"Segoe UI"` and `sans-serif` are obsidianoid `:32` minus Inter; **`system-ui` is added by this plan.** **Mandated** as a token — FRD `:222` writes `var(--font-body-fallback)`. |
| `--font-mono-fallback` | `"Fira Code", ui-monospace, monospace` | Same split: `"Fira Code"`/`monospace` from obsidianoid `:33`, **`ui-monospace` added here.** Mandated by FRD `:223`. |
| `--sidebar-width` | `280px` | obsidianoid `:34` |
| `--topbar-height` | `48px` | obsidianoid `:35` |
| `--transition` | `160ms cubic-bezier(0.16, 1, 0.3, 1)` | obsidianoid `:36` |
| `--overlay-scrim` ★ | `rgba(0, 0, 0, 0.55)` | `modal.ts:28` literal (settled) |

★ = overridable in `themes.css`. The three overridable tokens are `--font-body`, `--font-mono` (puma, FRD `:222-223`) and `--overlay-scrim` (light, Q1); A1.2 allowlists exactly these and fails on a fourth.

**Conflict rule, stated once.** `web/obsidianoid/css/themes.css` is the **structural donor of record**. Three documented exceptions: `--shadow-md` and `--overlay-scrim` come from `modal.ts` literals so the donor modal stays reproducible; `--space-5` comes from grocery because obsidianoid lacks it and FRD `:171` names it. Two tokens (`--space-7`, `--text-xl`) have no donor at all and are authored because FRD `:171-174` mandates the complete scales. Where grocery and obsidianoid disagree on a token both define (`--radius-sm`, `--shadow-sm`, `--text-*`), **obsidianoid wins** and grocery's value becomes a Phase-3 migration delta, not a Phase-1 decision. §8 row 9.

**Deliberately not authored:** `--color-surface-dynamic` (obsidianoid-only, Q2) and `--radius-xl` (grocery-only). Identical reasoning for both: one consumer each, in a module this phase does not migrate, and `--color-surface-dynamic` would additionally mean inventing 7 donor-less theme values that nothing reads. Each lands with its module's migration (§9 item 5). *Note the asymmetry with `--space-7`/`--text-xl`, which are authored: those two are named by FR-1's own vocabulary, so omitting them would be a deviation from the FRD, whereas authoring the other two would be an addition to it.*

#### 1.3 Tables T5a/b/c — donor → canonical remaps

**The `dark` / `obsidian` rename, stated once.** FRD FR-2 (`:185-196`) makes `obsidianoid`'s `:root, [data-theme="dark"]` block **canonical `obsidian`** (`:190-191`), and sources canonical **`dark`** from `web/todo/css/todo.css:3-17`. So the violet palette below is obsidian's, not dark's; v4's T5a header said "dark" and was wrong (Architect B8).

**T5a — obsidianoid → canonical** (obsidian, forest, ocean, ember, rose). 17 source keys per block.
*Identity:* `--color-bg`, `--color-surface-2`, `--color-divider`, `--color-border`, `--color-text`, `--color-text-muted`, `--color-text-faint`, `--color-primary`, `--color-primary-hover`, `--color-primary-active`, `--color-success`, `--color-warning`.
*Renamed:* `--color-surface` → `--color-surface-1`; `--color-surface-offset` → `--color-surface-3`; `--color-primary-highlight` → `--color-primary-tint`; `--color-error` → `--color-danger`.
*Dropped:* `--color-surface-dynamic` (Q2).
*Gap:* `--color-primary-fg`, authored per 1.4.
**Arithmetic: 17 − 1 dropped = 16 remapped, + 1 authored = 17 canonical.** This is the whole of iteration-3's Architect B7 / Critic C-2.

**T5b — todo → canonical** (dark, light). 13 source declarations.
`--bg`→`--color-bg`, `--surface`→`--color-surface-1`, `--surface-raised`→`--color-surface-2`, `--border`→`--color-border`, `--text`→`--color-text`, `--text-muted`→`--color-text-muted`, `--accent`→`--color-primary`, `--accent-hover`→`--color-primary-hover`, `--accent-lite`→`--color-primary-tint`, `--danger`→`--color-danger`, `--success`→`--color-success`, `--warning`→`--color-warning`.
`--shadow` is **not** a colour key — it informs T2's `--shadow-*` and leaves the colour matrix.
**Gaps: 5 per theme** — `--color-surface-3`, `--color-divider`, `--color-text-faint`, `--color-primary-active`, `--color-primary-fg`.

**T5c — grocery → canonical** (contributes to `light` only).
*Fills 2 of light's gaps:* `--color-divider: #ebe8e3`, `--color-text-faint: #b0ada8`.
*Corroborates 10* keys todo-light already supplies identically: `#f4f2ee`, `#ffffff`, `#f0ede8`, `#dcd9d5`, `#28251d`, `#6b6862`, `#01696f`, `#0c4e54`, `#dff0f0`, `#b91c1c`.
**Grocery declares no `--color-success` token at all** — it has `--color-warning-lite` (`:29`) and `--color-warning-bdr` (`:30`) but no `--color-success` in any form — so light's success and warning both come from todo-light (`#2d8a4e`, `#c9860a`). *(v4 said "only `-lite`/`-bdr` variants", which implied a base success token exists. It does not.)*

#### 1.4 Non-donor cells — 14 of 136

The 8 × 17 matrix has 136 cells; 122 come from a donor or from the FRD's puma block (`:202-217`). The other 14 are authored, and each derivation is written down so a reviewer can check the arithmetic rather than trust it.

**8 measured cells — `--color-primary-fg`, one per theme.** `#ffffff` fails WCAG AA (4.5:1) on **six of eight**:

| Theme | `--color-primary` | vs `#ffffff` | vs `#0b0f14` | Ships |
|---|---|---|---|---|
| light | `#01696f` | 6.46 ✓ | 2.98 | `#ffffff` |
| dark | `#7c3aed` | 5.70 ✓ | 3.37 | `#ffffff` |
| obsidian | `#7c6af7` | **3.99 ✗** | 4.82 | `#ffffff` — see below |
| forest | `#4dbb6e` | 2.43 ✗ | 7.92 | `#0b0f14` |
| ocean | `#5b9cf6` | 2.79 ✗ | 6.89 | `#0b0f14` |
| ember | `#f0a04a` | 2.14 ✗ | 9.00 | `#0b0f14` |
| rose | `#e05c7a` | 3.51 ✗ | 5.47 | `#0b0f14` |
| puma | `#3fbf9c` (FRD `:211`) | 2.30 ✗ | 8.37 | `#0b0f14` |

**obsidian ships `#ffffff` at 3.99:1 deliberately.** taskmaster ships `color:#fff` on `var(--interactive-accent, #7f6df2)` today (`modal.ts:97-99`) at **3.91:1**, and after C6 taskmaster *is* the surface rendering under `[data-theme="obsidian"]` — through `components.css`'s `.ui-modal-btn-primary`, which is the rule that reads the token. Raising it would be a visible restyle of a shipping button inside a foundation phase. Recorded as an inherited pre-existing condition, §9 item 1. **This reasoning is sound only because C6 stamps the attribute and because `components.css` exists to consume the token** — v4 asserted the first and deleted the second.

**6 derived cells.**

| Theme | Key | Value | Derivation |
|---|---|---|---|
| dark | `--color-surface-3` | `#2c3138` | Continues todo-dark's own step: `#161b22` → `#21262d` is `+0x0b` per channel, so the next rung is `#2c3138`. **Flag:** it lands only `0x04` below `--color-border` `#30363d`, tighter than obsidian's equivalent gap. §9 item 10. |
| dark | `--color-divider` | `#232930` | **A chosen value, not a computed one.** Constraint it satisfies: above `--color-surface-2` `#21262d` and well below `--color-border` `#30363d`, so a divider reads quieter than a border. The midpoint (`#282e35`) would read as a second border. |
| dark | `--color-text-faint` | `#6e7681` | GitHub Primer `fg.subtle` — the family todo-dark's other greys come from (`#8b949e` is Primer `fg.muted`). |
| dark | `--color-primary-active` | `#5b21b6` | Tailwind violet-800, one rung below todo-dark's `--accent-hover` `#6d28d9` (violet-700). Independently present in this tree at `web/grocery/style.css:33`. |
| light | `--color-surface-3` | `#e6e3df` | Midway between `--color-surface-2` `#f0ede8` and `--color-border` `#dcd9d5`; the exact midpoint is `#e6e3de`/`#e6e3df` and the blue channel rounds up. |
| light | `--color-primary-active` | `#173339` | todo-light's own `--accent` → `--accent-hover` delta (`#01696f` → `#0c4e54`, i.e. `+0x0b, −0x1b, −0x1b`) applied once more. **Exact.** |

#### 1.5 The `light` scrim override (user answer Q1 = yes)

`--overlay-scrim`'s base is the donor's 55% black — correct on the 7 dark themes and far too heavy over a light page. `themes.css`'s `[data-theme="light"]` block therefore overrides it to `rgba(40, 37, 29, 0.35)`: light's own `--color-text` `#28251d` at 0.35 alpha, so the scrim is the theme's own ink rather than a foreign black, at an alpha where the page reads dimmed rather than blacked out. Same mechanism as puma's `--font-*` override, and the third entry in A1.2's allowlist. **The alpha is a judgement, not a derivation** — the sampler pass confirms or adjusts it (§9 item 11, via `docs/sampler-checklist.md`).

#### 1.6 `components.css` — the `.ui-modal-*` rules

The retokenized body of `modal.ts`'s `ensureStyles()` (`:20-106`), moved here in Step 5 and authored here in Step 1 so `shared.css` has a consumer from C4 onward. Rules for the eight classes the donor defines — `ui-modal-overlay`, `ui-modal-panel`, `ui-modal-title`, `ui-modal-message`, `ui-modal-input`, `ui-modal-actions`, `ui-modal-btn`, `ui-modal-btn-primary` (verified: those are exactly the 8 `tm-modal-*` literals in the donor, renamed per Step 5).

**Two authoring constraints, both gated:**

- **No colour literal** (A1.6). Every colour comes from a token. **Carve-out, restored from v3:** the ban is on colour *literals* — `#hex`, `rgb(`, `rgba(`, `hsl(` — outside comments, and it applies to **`components.css` only**, not to `themes.css` (which is nothing but literals by definition) and not to module CSS (§8 row 14). `box-shadow` and the scrim consume `var(--shadow-md)` and `var(--overlay-scrim)`, whose literals live in `tokens.css`, so the carve-out costs nothing: there is no rule in `components.css` that needs a literal.
- **Selector discipline** (A1.10): every selector is `.ui-*`, or a state/descendant of one. No bare element selectors — this is what keeps `shared.css` inert on the 12 non-adopting modules if one ever links it, and it is why `check-shared-css.mjs` parses selector lists instead of pattern-matching lines (v1's grep missed `a:hover {` and `.ui-card > button {`).

#### 1.7 `scripts/check-shared-css.mjs` — the specification

*(Answers the Critic's unscored question 3, which asked how A1.3's 136-declaration check and A1.6's carve-out are actually implemented.)*

Lints each file in `web/shared/css/` **individually, and does not resolve `@import` targets.** Two structural reasons, both load-bearing: at C1 `fonts.css` does not exist yet, and a linter that followed imports would make the C1/C1b split unlandable.

Implementation, **11 clauses**, no CSS parser dependency (G4):

1. **Strip comments first** (`/\/\*[\s\S]*?\*\//g`), before any other clause, so every "outside comments" qualifier below is structural rather than a regex afterthought.
2. **Block scanner.** Walk the file tracking brace depth; collect `{selector, declarations[], line}` for every depth-1 block, skipping blocks whose selector starts with `@` (so `@media`/`@supports` wrappers do not masquerade as rules). Declarations are split on `;` and each parsed as `prop: value` with the property trimmed.
3. **`tokens.css`:** exactly one depth-1 block, selector `:root`; its property set must **equal** T2 (27 names) — missing and extra are both failures, each reported by name. → A1.4.
4. **`themes.css`:** the depth-1 selector roster must equal the 8 expected strings — `:root, [data-theme="dark"]` plus `[data-theme="light"|"obsidian"|"forest"|"ocean"|"ember"|"rose"|"puma"]`. → A1.3 clause 1.
5. **Per theme block:** the set of `--color-*` properties must equal T1 (17), **and then** the running total of `--color-*` declarations across all 8 blocks must equal **136**. Both checks are needed: the set test alone passes when a block declares one key twice and omits another, because a set has no multiplicity. → A1.1, A1.3.
6. **Non-`--color-*` properties in a theme block** must be members of `ALLOW = {--font-body, --font-mono, --overlay-scrim}`; a fourth is a failure naming the offending property and line. → A1.2.
7. **`components.css`:** the colour-literal regex (`#[0-9a-f]{3,8}\b`, `rgba?\(`, `hsla?\(`) runs **only over this file**, and every selector must match `^\.ui-[a-z0-9-]+` allowing descendant/pseudo/attribute continuations. → A1.6, A1.10.
8. **`index.css`:** every `@import` is a relative same-directory path (no `/`, no `..`, no scheme, no `url(http…)`), and the set of imported names is a subset of the files present. → A1.7. No external-URL `@import` anywhere under `web/shared/css/`. → A1.5.
9. **`!important` appears in no file** under `web/shared/css/`. → A1.8. This is load-bearing for R17: `web/taskmaster/style.css:4`'s `[hidden] { display: none !important; }` must keep winning over any `.ui-modal-*` display rule, and it does precisely because shared CSS may not use `!important`.
10. **From C1b, `fonts.css`:** the file declares **exactly 15 `@font-face` rules**; each `url("…")` resolves — mapping the served prefix `/shared/` back to the repo path `web/shared/` — and each `*.woff2` under `web/shared/public/fonts/` is referenced by exactly one `url()`. → A1.9, all three halves (count, forward resolution, reverse coverage) per G5. *(The count half was in A1.9 but missing from this clause — Critic note 4.)*
11. **`--color-error` appears nowhere** under `web/shared/`; `--color-danger` is the canonical name. → A1.11.

Exit 1 on the **first** failure, printing `file:line: clause: message`. Invoked by `scripts/build-web.mjs` before the first esbuild call (driver rule 8), by `make gates`, and directly in each commit's verification block.

#### 1.8 `scripts/descriptors.mjs` and the two Makefile gate targets land here

*(Architect blocker B2. v5 put these in C2 while making C1 assert against them.)*

C1 is the commit that introduces the **gate harness**, and a harness is not shippable in halves. Four of C1's own gates — **A7.2** (`WEB_ARTIFACTS` is generated, not hand-written), **A7.3** (the artifact list is tracked and byte-identical), **A7.7** (`artifacts.mjs` runs the generator as a child process and compares against `EXPECTED_ARTIFACT_COUNT`) and **A7.17** (no gate is a shell one-liner in a make recipe) — plus **AX.3** and **AX.4** (count 12 at this boundary) are assertions *about these files*. Deferring them to C2 would mean C1's boundary named six gates that could not be invoked until the next commit.

So C1 additionally lands:

- **`scripts/descriptors.mjs`** — the seven existing descriptors transcribed from today's npm chain, plus `EXPECTED_ARTIFACT_COUNT = 12`. It is a **static data module**: it imports nothing, executes nothing, and depends on neither driver. Its full contents and schema are specified once, in **Step 3**, alongside the drivers that consume it; only its *timing* is decided here.
- **`scripts/list-artifacts.mjs`** — prints one artifact path per line, derived from that array. It moves here with `descriptors.mjs` for the same reason and one more: A7.7 requires `artifacts.mjs` to run the **generator** as a child process, so the generator must exist wherever the gate does.
- **`Makefile`** — the `web-verify` and `gates` targets, and `.PHONY` grows 9 → 11. `gates` runs **three** scripts at this boundary: `check-shared-css.mjs`, `bundle-shape.mjs`, `token-overlap.mjs`. It does **not** name `check-shared-barrel.mjs`, which C4 creates and C4 adds to the recipe (Critic amendment A; §4's gate-composition table; Step 5).

`bundle-shape.mjs` therefore ships **live but with an empty input set** — its input is the `sharedConsumer: true` descriptors, of which there are none until C5. That is a vacuous pass, recorded as such in §4's composition table and in A9.5, not counted as evidence of anything. It is not a stub: the same code path that passes on zero inputs is the one that fails on a bad input at C6.

**Verification**
```sh
node scripts/check-shared-css.mjs                                    # A1.1-A1.8, A1.10, A1.11 (clause 10 is inert: no fonts.css yet)
node scripts/gates/artifacts.mjs --allow web/taskmaster/js/bundle.js # A7.2, A7.3, A7.7, AX.3, AX.4: count 12; the C1/C1b exemption (§4)
make gates                                                           # A7.17: three scripts at this boundary (§4's composition table)
make -n web-verify gates >/dev/null                                   # A7.15: both targets exist and are phony
node scripts/gates/token-overlap.mjs                                 # A2.1-A2.5
node scripts/gates/clean-tree.mjs web/shared/css/ scripts/ Makefile .gitignore   # AX.1, P2-scoped so P6's side-car files are untouched
grep -rn -- '--color-error' web/shared/; rc=$?; [ "$rc" -eq 1 ]      # A1.11, guarded per AX.5
```

`make check` is **not** run here: it does not exist until C2, which adds it together with `test-web`. `make web-verify` is likewise not used at this boundary — the byte-identity exemption's mechanism is the `--allow` flag on the gate file itself (§4).

---
### Step 2 — Fonts (C1b, deliverable D7)

**Creates:** `web/shared/public/fonts/**` (15 woff2, 4 `OFL.txt`, 1 `SHA256SUMS`), `web/shared/css/fonts.css`
**Modifies:** `web/shared/css/index.css` (adds `@import "fonts.css";` as the first import)

**Shipping rule, stated once so the count follows mechanically:** ship exactly the staged woff2 files for which `fonts.css` declares an `@font-face`, preserving the family subdirectory layout. Inter's three static weights are **not** shipped — `InterVariable.woff2` plus `InterVariable-Italic.woff2` cover 100–900, so the statics would be dead bytes. 18 staged − 3 = **15**: Inter 2, JetBrains Mono 4, Sora 5, IBM Plex Mono 4.

**Why Sora and IBM Plex Mono ship in Phase 1** rather than later: the puma theme block ships in Phase 1, and FRD `:222-223` names those two families in its `--font-*` override. Shipping the block without the faces means puma silently resolves to `var(--font-body-fallback)` — and C6's screenshot pass would bless the wrong rendering as correct. Risk R22.

**`@font-face` form for the variable faces:** `font-family: "Inter"` with `font-weight: 100 900`. **Not** `"Inter var"` — nothing in T2 references that family name, so the descriptor would be inert. A6.4.

**`url()` form and the esbuild policy that makes the artifact count hold.** Every `url()` in `fonts.css` is **server-absolute**: `url("/shared/public/fonts/inter/InterVariable.woff2")`. The `shared-css` descriptor sets `external: ["*.woff2"]`, so esbuild leaves those urls untouched and emits **no** hashed side-artifacts — which is what keeps the Phase-1 artifact total at 15 rather than 15 + 15 content-hashed copies. The alternative (`loader: { ".woff2": "file" }`) would copy and rename every face into `web/shared/dist/`, doubling the committed bytes and making `SHA256SUMS` describe files no page requests. **Consequence, stated because it is a real constraint:** `fonts.css` is only correct when the tree is served at `/shared/` — which D2 guarantees on all 13 hosts and A8.1 asserts. A6.5.

**Provenance (G9).** `web/shared/public/fonts/SHA256SUMS` is `fonts-staging/SHA256SUMS` minus the three `inter/Inter-{Regular,Medium,SemiBold}.woff2` lines. Because the family subdirectory layout is preserved, the remaining 15 lines transplant **verbatim** and `shasum -a 256 -c SHA256SUMS`, run with the fonts directory as cwd, resolves them — which is why the digest file lives beside the fonts rather than in `docs/`. Mechanism verified end to end on scratch files: rc=0 intact; **rc=1 with a `FAILED` line on byte mutation; rc=1 with `FAILED open or read` on a missing file.** It fails loud in both directions, which is what makes it a gate rather than a comment. Each family's `OFL.txt` travels beside its faces. The file is HTTP-reachable at `/shared/public/fonts/SHA256SUMS`; it lists digests of world-readable font files, so it discloses nothing.

**Weight trimming is deferred, and the reason is quoted rather than cited.** `fonts-staging/NOTES.md:27-28` says, verbatim: *"**Sora** — 400/500/600/700/800 (puma theme headings; trim once the theme CSS pins its weights)."* Phase 1 ships all five because the puma block does not yet pin weights; §9 item 8 trims them when it does. The quote is reproduced here because `NOTES.md` is **untracked** (ADR-006), so a reviewer of this plan cannot open the citation.

**Verification**
```sh
cd web/shared/public/fonts && shasum -a 256 -c SHA256SUMS      # A6.1, rc=0
node scripts/check-shared-css.mjs                              # now includes clause 10 → A1.9
node scripts/gates/clean-tree.mjs web/shared/ web/taskmaster/js/bundle.js   # see the C1b exemption in §4
```

### Step 3 — Build and test drivers, Makefile, and the generated artifact list (C2)

**Creates:** `scripts/build-web.mjs`, `scripts/test-web.mjs`
**Modifies:** `package.json` (`build`, `build:dev`, `test:web`, `engines`), `Makefile` (`web:` at `:42-44`, `typecheck:` at `:46-47`, new `test-web` and `check`, `.PHONY` extended 11 → 13), `web/taskmaster/js/bundle.js` (regenerated — see below)

**What this step does *not* create, stated because v5 had it wrong** *(Architect blocker B2)*. `scripts/descriptors.mjs`, `scripts/list-artifacts.mjs`, `scripts/gates/artifacts.mjs` and the `web-verify` / `gates` Makefile targets all land at **C1** (Step 1.8), not here. Step 1's own gates A7.2/A7.3/A7.7/A7.17 and AX.3 assert against them, and a gate cannot assert against a file three commits in its future. The dependency runs the other way round: `descriptors.mjs` is a static data module that depends on nothing the drivers create, while the drivers import it. C2 does not touch `descriptors.mjs` at all — see §4's `descriptors.mjs` edit-trail bullet.

One descriptor array in `scripts/descriptors.mjs` is the single source for the build driver, the test driver, **and** `WEB_ARTIFACTS`. The artifact list, the build, and the test runner therefore cannot disagree. Each descriptor: `{ name, entry, out, mode, bundle, format, jsx, define, loader, external, runner, sharedConsumer }`.

**`sharedConsumer` is one field doing three jobs, deliberately.** A descriptor with `sharedConsumer: true` (a) gets the `@shared` `onResolve` plugin of rule 10, (b) is subject to the driver's `format: "esm"` assertion, and (c) is a member of `scripts/gates/bundle-shape.mjs`'s input set (A9.4). Deriving all three from one field is why they cannot drift apart: v5 described the plugin's applicability as "bundled browser descriptors" and left `bundle-shape.mjs`'s input set unstated, so the plugin could apply to a bundle the gate never inspected — Critic amendment A. The field is **absent on all seven descriptors here and on `shared`/`shared-css` at C4**: the barrel is not a consumer of itself. It first appears at C5 (`sampler`) and again at C6 (`taskmaster`), which is why A9.1/A9.2/A9.4 are deferred to those commits and not to C4.

**The seven existing descriptors** — authored at **C1** (Step 1.8) and consumed unchanged here; `shared` and `shared-css` are added by C4, `sampler` by C5. None carries `sharedConsumer`:

| name | entry | mode | format | extra |
|---|---|---|---|---|
| obsidianoid | `web/obsidianoid/js/threads.ts`, `web/obsidianoid/js/app.ts` | transpile (`outdir`) | (default) | — |
| slideshow | `web/slideshow/js/app.ts` | transpile (`outdir`) | (default) | — |
| multissh | `web/multissh/js/main.ts` | bundle → `js/bundle.js` | iife | — |
| certmachine | `web/certmachine/js/main.ts` | bundle → `js/bundle.js` | iife | — |
| taskmaster | `web/taskmaster/js/main.ts` | bundle → `js/bundle.js` | iife *(→ esm in Step 7)* | `define: __TM_BUILD_TIME__` |
| smbedit | `web/smbedit/src/main.tsx` | bundle → `js/bundle.js` | iife | `jsx: automatic`, `define: process.env.NODE_ENV`, `logLevel: warning` |
| issuetracker | `web/issuetracker/src/main.tsx` | bundle → `js/bundle.js` | iife | same as smbedit |

**Driver rules** — each exists because violating it breaks a specific gate.

1. **esbuild JS API, one `build()` per descriptor, sequential.** Shared descriptors run first so a later module could import their output.
2. **Entry paths stay exactly as they are today.** No module is relocated in Phase 1. The schema carries `mode: "transpile" | "bundle"` precisely so `web/obsidianoid/js/app.ts` (transpile-only, `outdir`) and `web/smbedit/src/main.tsx` (bundled, `outfile`) coexist without either being forced into the other's shape. The target layout (`web/<m>/src/`) is a Phase-2 rename touching only the `entry` field.
3. **All paths relative to the repo root; never absolute; no `absWorkingDir`; no `path.resolve` on any esbuild input or output field.** *(Restored from v3; deleted by v4.)* esbuild embeds relative source paths as comments in bundles — `web/taskmaster/js/bundle.js:3` is literally `// web/taskmaster/js/ui/modal.ts`. Any absolute path rewrites every one of those comments and fails G2 everywhere at once. This is the single highest-probability cause of a G2 false failure. The driver asserts `!path.isAbsolute(v)` on every descriptor path field before calling esbuild. R2.
4. **`define: { __TM_BUILD_TIME__: JSON.stringify(digest()) }`**, per-descriptor so no other module sees the flag, where `digest()` is ADR-004's content digest computed in the two-pass build. There is no `$(date -u)` and no `1970` pin anywhere.
5. **`--sourcemap` via one flag.** `node scripts/build-web.mjs --dev` sets `sourcemap: true` and flips `process.env.NODE_ENV` to `"development"` for the two React modules. `build:dev` becomes that one command.
6. **`target: "es2020"`, `platform: "browser"`, `logLevel`** mirrored per descriptor from the current chain — including `logLevel: "warning"` on exactly the two React modules, because a different default surfaces different stderr and invites someone to "fix" it by changing flags.
7. **Warnings are failures.** Both drivers set `logLevel: "warning"` and exit non-zero when `result.warnings.length > 0`. Verified safe to turn on immediately: the tree produces zero real esbuild warnings today (§2). This is the mechanism that would have caught iteration-1's `import.meta` downgrade, which esbuild reported as a warning **with exit 0**.
8. **`check-shared-css.mjs` runs from the driver**, before the first esbuild call; a failure aborts the build non-zero. Without this the linter is a gate nobody invokes.
9. **`check-shared-barrel.mjs` runs alongside it from C4**, same placement in the driver, same abort semantics. Its **`make gates` line is added by C4**, not by this commit — the script does not exist yet, and a recipe naming it would fail C2 and C3 outright while a stub that exits 0 would be a gate that has never been able to fail (R19, AX.6). Step 5 states this; §4's gate-composition table records that `make gates` runs three scripts through C3 and four from C4. *(Critic amendment A.)*
10. **`@shared` is externalized, not inlined.** *(Restored from v3.)* An `onResolve` plugin, applied to exactly the descriptors carrying **`sharedConsumer: true`** — not "bundled browser descriptors", which was v5's imprecise phrasing and would have swept in `shared` itself:
    ```js
    build.onResolve({ filter: /^@shared(\/.*)?$/ }, () => ({ path: "/shared/dist/shared.mjs", external: true }));
    ```
    Every `@shared/...` specifier — `@shared`, `@shared/modal`, `@shared/theme` alike — collapses to the single barrel URL. That is correct **only because the barrel is the complete public surface**: a deep specifier that resolved to something the barrel does not re-export would collapse to a URL from which the name is genuinely absent, and the failure would be a browser-console `SyntaxError`, not a build error. So barrel-completeness is not a stylistic preference — it is the precondition this rewrite rests on, and it is enforced by **A5.2's allowlist half** (`check-shared-barrel.mjs` asserts the exported-name set *equals* the six named values plus four types). `tsconfig`'s `paths` makes `tsc --noEmit` resolve the same specifiers to source, so types are checked while bytes stay external. **A descriptor with `sharedConsumer: true` must declare `format: "esm"`, and the driver asserts it** — with iife, esbuild emits `__require("/shared/dist/shared.mjs")` and a `Dynamic require of` shim, exits 0 with zero warnings, and the failure appears only in a browser console. A9.1/A9.2 are the two-sided gate; R3.
11. **Test runner is the same driver, same flags.** `scripts/test-web.mjs` bundles each test entry with `bundle: true, platform: "node", format: "cjs", target: "node18", logLevel: "warning"` — the exact four flags `test:web` uses today — and pipes to `node`. The `runner` field selects the mode: `"esbuild-cjs"` for the four existing `.test.ts` suites plus `web/shared/ts/modal.test.ts` (C4); `"node-test"` for `web/grocery/app.test.js`, which uses `node:test` and `import.meta.dirname` (`:16-17`) and must run as `spawnSync(process.execPath, ["--test", <file>], { cwd: repoRoot })`. **Five suites after C4**; A7.13.

**`package.json`**
```
"build":     "node scripts/build-web.mjs",
"build:dev": "node scripts/build-web.mjs --dev",
"typecheck": "tsc --noEmit",
"test:web":  "node scripts/test-web.mjs",
"engines":   { "node": ">=22" }
```
`engines` is declared because the drivers use `import.meta.dirname` and `node:test`; without it a Node 18 host fails obscurely. No `@types/node` is added (G4, ADR-005 driver 1).

**Why `web/taskmaster/js/bundle.js` is in C2's manifest.** *(Original to v5.)* ADR-004 changes how `__TM_BUILD_TIME__` is computed, so the commit that lands the driver necessarily re-stamps that artifact from a `$(date -u)` string to the digest `c2c572876987`. A C2 that did not commit the regenerated bundle would fail its own byte-identity gate at its own boundary. This is the commit where G2 becomes true for taskmaster, and it is why the C1/C1b exemption ends here.

**The generated artifact list (user answer Q5 = "sure, generate the list").**

`scripts/descriptors.mjs` exports both the descriptor array and `EXPECTED_ARTIFACT_COUNT`. `scripts/list-artifacts.mjs` prints the artifact paths derived from the array, one per line. The count is **defined once, in `descriptors.mjs`**, and both the generator and the gate import it — v4 wrote the number twice inside one make recipe (Critic M-6). **All three files, and the `web-verify` and `gates` targets that drive them, land at C1** (Step 1.8); this section specifies them once, here, because this is where the build driver that shares the same data module is described.

**This commit's two new targets:**

```make
test-web:
	npm ci
	npm run typecheck
	npm run test:web

check: web-verify test-web gates test
```

**For reference, the two targets C1 already added** (Step 1.8; reproduced so the gate path is readable in one place — C2 does not edit them):

```make
web-verify:
	node scripts/gates/artifacts.mjs

gates:
	node scripts/check-shared-css.mjs
	node scripts/gates/bundle-shape.mjs
	node scripts/gates/token-overlap.mjs
```

`gates` is **three** scripts here. C4 adds a fourth line, `node scripts/check-shared-barrel.mjs`, in the same commit that creates the script (driver rule 9, Step 5, §4's gate-composition table). `bundle-shape.mjs` exists from C1 and is **vacuous until C5** — its input set is the `sharedConsumer: true` descriptors, which is empty until then; A9.5 records that it is live from C1 and load-bearing from C6.

`scripts/gates/artifacts.mjs` is **one process** that does all four things v4 tried to do in four recipe lines:

1. **imports `EXPECTED_ARTIFACT_COUNT` from `descriptors.mjs`** and **runs `scripts/list-artifacts.mjs` as a child process**, then fails if the printed line count and the imported constant disagree (so a truncated list is a failure, not a silent pass). Running the generator rather than re-deriving the list in-process is what A7.7 requires: it is the generator's own output that `WEB_ARTIFACTS` consumes, so it is the generator's own output that must be counted.
2. runs `git ls-files --error-unmatch -- <paths>` with an **explicit, non-empty** argv — verified asymmetry: **zero-argument `git ls-files --error-unmatch` lists the whole repo and exits 0**, a silent pass, whereas zero-path `git diff --exit-code --` diffs the whole tree and fails loud, so only the `ls-files` half needed the guard;
3. runs `npm run build` and fails on non-zero;
4. runs `git diff --exit-code -- <paths>` and reports the differing paths.

**Why this is a file and not a recipe (G11).** Architect B7 proved v4's form always passes: in `@[ -z "$(git diff --name-only -- $(WEB_ARTIFACTS))" ]`, make expands `$(git diff …)` as a *make variable named "git diff …"*, which is empty, so the shell receives `[ -z "" ]` and exits 0 **whatever the tree contains**. The same hazard applied to v4's AX.1 and P2. Inside a `.mjs` file there is no make expansion layer: `execFileSync("git", ["diff", "--exit-code", "--", ...paths])` throws on non-zero and the process exits non-zero. Additionally, no `$(shell …)` survives anywhere in the Makefile for the gate path, because `$(shell …)` discards the child's exit status — a crashed generator would read as an empty list. *(v4's finding that a `:=` assignment runs `$(shell …)` at parse time, breaking `make build` on a node-free host, is retained as recorded evidence in ADR-001's consequences; the mechanism it describes is superseded by having no `$(shell)` at all, which satisfies ADR-001 driver 3 unconditionally rather than by careful assignment choice.)*

`Makefile:42`'s `web:` target keeps its name; **`:43`**'s `@if [ ! -d node_modules ]; then npm install; fi` is replaced by `npm ci`, so a stale or partial `node_modules` cannot make a verification pass, and `npm install`'s lockfile rewrite cannot dirty `git status` under P2. `.PHONY` at `:12` today lists exactly the 9 targets the file defines (`run build build-rpi test clean clean-local-test-db init-config web typecheck`), and it grows **in two stages**: **9 → 11 at C1** (`web-verify`, `gates`) and **11 → 13 here** (`test-web`, `check`). A7.15 is therefore `[deferred → C1/C2]` and asserts membership at both boundaries, because a non-phony target named after a file that happens to exist silently never runs.

`.gitignore` gains `fonts-staging/` (ADR-006) and keeps `certmachine.pin`; that edit lands at **C1** with the rest of the harness (§4's C1 row), because `fonts-staging/` must already be ignored when C1b stages faces. It does **not** gain `screenshots/` — verified: `tools/baseline-shots/baselines/` is already covered by a nested `.gitignore`, and nothing in the tree writes to `screenshots/`.

**Verification**
```sh
npm ci && npm run build
make web-verify                    # A7.3 + AX.4: count 12, tracked, byte-identical — no `--allow`, the exemption ended here (A7.4)
make gates                         # A7.17: still three scripts at this boundary (§4's composition table)
npm run typecheck && npm run test:web && make test
make check                         # A7.16: exactly these four prerequisites, in this order
grep -c 'date -u' package.json; rc=$?; [ "$rc" -eq 1 ]   # A7.5: no match, so grep -c prints 0 and exits 1
node scripts/gates/clean-tree.mjs scripts/ package.json Makefile web/taskmaster/js/bundle.js   # AX.1; `.gitignore` belongs to C1's path set
```

---
### Step 4 — Go: `/shared/` on every module host (C3, deliverable D2 / FR-8)

**Creates:** `internal/platform/static/shared.go`, `internal/platform/static/shared_test.go`
**Modifies:** `internal/platform/config/config.go` (one field + one default line), `cmd/server/main.go` (one line, `:303`), `cmd/server/dispatcher_auth_test.go` (one line, `:100`), `internal/utuber/build.go` (delete the duplicate), `unified-webapp-example.json`, `unified-webapp.json`, `README.md`

*(Restored from v3 in full. v4 deleted this entire specification while keeping gates that assert against it — Architect B2.)*

**Three functions, one mount point.**

```go
// SharedHandler serves the shared asset tree at dir under the /shared/ prefix.
// It strips the prefix itself, so every caller passes unmodified request paths.
// Only the dist/ and public/ subtrees are reachable; anything else is 404.
// There is no SPA fallback and no directory listing: a typo under /shared/ is a
// 404, never someone else's index.html with status 200.
func SharedHandler(dir string) http.Handler

// WithShared returns next with /shared/* diverted to SharedHandler(dir).
// dir == "" returns next unchanged.
func WithShared(next http.Handler, dir string) http.Handler

// MountShared registers SharedHandler(dir) at /shared/ on m.
// It is the per-module path, for modules that own a mux and want the subtree
// inside their own middleware.
func MountShared(m Muxer, dir string) error

type Muxer interface { Handle(pattern string, h http.Handler) }
```

**Handler contract, pinned — nine clauses, each a `shared_test.go` case, each mapped to its criterion.** *(v5 had seven bullets and claimed each was "a clause of A8.4". That was false for four of them — prefix stripping, caching and Content-Type are not A8.4 clauses — and it left A8.4's symlink and dotfile clauses with no bullet at all, i.e. two security clauses with no specified mechanism: Architect M1, Critic note 1. The mapping is now stated per bullet and A8.1 counts nine.)*

| # | Clause | Criterion |
|---|---|---|
| 1 | Prefix stripping | A8.1 |
| 2 | First-segment allowlist | A8.4 clause 2 (R9) |
| 3 | Lexical path confinement | A8.4 clause 1 (R12) |
| 4 | Root confinement (symlinks) | A8.4 clause 5 |
| 5 | Dotfile refusal | A8.4 clause 6 |
| 6 | Directory 404 | A8.4 clause 3 |
| 7 | Method allowlist | A8.4 clause 4 |
| 8 | Caching contract | A8.6 (R11) |
| 9 | `.mjs` Content-Type | A8.6 (R10) |

1. **Prefix stripping happens inside `SharedHandler`.** All three entry points therefore agree, and there is no "who strips it" ambiguity between `WithShared` and `MountShared`. A test asserts the resolved on-disk path for `/shared/dist/shared.css` is `<dir>/dist/shared.css`.
2. **First-segment allowlist.** After stripping, the first path segment must be exactly `dist` or `public`; anything else → 404. This is not deferred hardening — it is the initial behaviour, so `web/shared/ts/*.ts` and `web/shared/css/*.css` are **never** HTTP-reachable. Every smoke URL and acceptance criterion in this plan therefore names **`/shared/dist/shared.css`**, never `/shared/css/tokens.css`. R9.
3. **Lexical path confinement** via `path.Clean("/"+rest)` before joining, matching certmachine's `staticFileExists` discipline. A test drives `/shared/../../etc/passwd`, `/shared/dist/../../../etc/passwd`, and the `%2e%2e%2f` form. R12.
4. **Root confinement, by mechanism rather than by lexical care — this is the named answer for A8.4 clause 5.** `path.Clean` cannot see a symlink: `dist/evil → /etc` survives cleaning and then escapes on the join. So the handler holds a `*os.Root` obtained once, at construction, from **`os.OpenRoot(dir)`** (available on this tree's **go1.27.1**), and every request opens through **`root.Open(rel)`**, which refuses any path that leaves the root — symlinked or not — with an error the handler answers as **404**. A test plants a symlink under the fixture's `dist/` pointing outside it and asserts 404, and a second asserts the handler still serves a regular file through the same path. **One consequence, stated because it changes a named API:** serving is then `http.ServeContent(w, r, rel, fi.ModTime(), f)` on the opened file rather than `http.ServeFile` on a joined path. `ServeContent` supplies `Last-Modified` from the passed modtime, honours `If-Modified-Since` with a bodyless 304, and derives `Content-Type` from `rel`'s extension via `mime.TypeByExtension` — the same three behaviours clauses 8 and 9 assert of `ServeFile`, so the caching and Content-Type contracts are unchanged in substance. *(The rejected alternative is `filepath.EvalSymlinks` plus a prefix comparison: it needs the target to exist, so a legitimate 404 becomes an error path, and it leaves a TOCTOU window between the check and the open. `os.Root` closes both.)*
5. **Dotfile refusal — A8.4 clause 6.** After cleaning, any path segment beginning with `.` → 404. This is a separate clause from confinement because a dotfile *inside* the root is reachable without escaping it.
6. **Directory requests 404.** No listings, ever — `fi.IsDir()` on the opened file is a 404, not a redirect.
7. **Methods:** GET and HEAD only; anything else 405 with `Allow: GET, HEAD`.
8. **Caching contract:** `Last-Modified` present on both GET and HEAD, `If-Modified-Since` yields 304 with no body, and no `ETag`. Tested explicitly so a future handler change cannot silently drop conditional requests. R11, A8.6.
9. **Content-Type for `.mjs`:** a test asserts the response `Content-Type` starts with `text/javascript`. Go's builtin table maps `.mjs`, but `mime.TypeByExtension` consults the **system** table first, so a host with a stale `mime.types` could serve something a browser refuses for a module script. This must fail in a test run, not in someone's browser. R10, A8.6 *(folded in with clause 8 per Critic amendment D — one `ServeContent` call decides both, so they are one criterion)*.

**Route wiring — one line.** `cmd/server/main.go:303` becomes:

```go
h = middleware.BodyLimit(limitFor(module, cfg), svc.Gate(module, static.WithShared(hh, cfg.Server.SharedStaticDir)))
```

and the mirror line at `cmd/server/dispatcher_auth_test.go:100` (which has no `Gate`) becomes:

```go
h = middleware.BodyLimit(limitFor(module, cfg), static.WithShared(hh, cfg.Server.SharedStaticDir))
```

**The invariant this placement establishes, stated as an invariant:** `static.WithShared` sits **inside** `svc.Gate`, so `/shared/` is authenticated like everything else, and **after** the `io.Closer` assertion at `:300-302`, so **no module's closer can be erased — not certmachine's today, and not a module added tomorrow.** That is a property of where the wrapper goes, not of care taken while writing it. **A8.3 asserts it** (a certmachine-routed dispatcher has a non-empty `closers` slice, and `make test` passes goleak via `dispatcher_auth_test.go:1241-1243`), because an invariant nobody checks is a comment. This was iteration-2's blocker 1; ADR-002 records the rejected per-module alternative that reintroduces it.

**Config.** `ServerConfig` (`internal/platform/config/config.go:203-212`, which today holds only `OriginCheck` `:208` and `SSEMaxSubscribers` `:211`) gains exactly one field:

```go
// SharedStaticDir is the directory holding the shared asset tree served at
// /shared/ on every module host. Empty disables the mount.
SharedStaticDir string `json:"shared_static_dir"`
```

`applyServerDefaults` (`:659-669`) gains one line defaulting it to `./web/shared` when empty. **No `json:"-"` fan-out into 13 module configs and no 13 default lines** — that cost belongs to the per-module alternative, and ADR-002 does not pay it.

**Boot-time warning.** `cmd/server` logs a warning (not a failure) when `cfg.Server.SharedStaticDir` does not stat as a readable directory, matching the posture of the existing per-module `checkStaticDir` helpers (`internal/certmachine/build.go:168`, `internal/multissh/build.go:87`, `internal/smbedit/build.go:56`) while keeping the degradation non-fatal — an operator who has not yet deployed `web/shared/` should still get a running binary in Phase 1.

**utuber de-duplication (FR-8.2).** Delete the verbatim `staticHandler` type and its `ServeHTTP` (`internal/utuber/build.go:68-79`, plus the duplicate-acknowledging comment at `:64-67`); change `:58` to `mux.Handle("/", static.NewHandler(cfg.StaticDir))`. Behaviour is identical — the deleted code is a byte-for-byte copy of `internal/platform/static`. A8.5.

**Ops artifacts.**
- `unified-webapp-example.json` gains `"shared_static_dir": "./web/shared"` in its existing `server` block (`:24-27`).
- `unified-webapp.json` — add a `server` block containing only `shared_static_dir`. (Omitting it would work by default, but the sample configs are the deployment documentation.)
- `README.md` deploy section: `web/shared/` and `web/sampler/` are new required directories alongside the per-module `web/<m>/`.

**Intra-step ordering, for mid-step rollback.** Land in this order so the tree compiles at each sub-point: (a) `ServerConfig` field + `applyServerDefaults` line + sample configs; (b) `shared.go` + `shared_test.go`; (c) the two call sites + boot warning; (d) utuber deletion. (a) and (b) are independently harmless; a failure at (c) reverts one line in each of two files.

**Verification**
```sh
test -z "$(gofmt -l .)"                     # A7.14, repo-wide: 0 lines today
go vet ./...                                # A8.8, rc=0 today
go test -race ./internal/platform/static/... -run Shared -v
make test                                   # goleak-gated; proves no closer was erased (A8.3)
grep -rn 'staticHandler' internal/utuber/; rc=$?; [ "$rc" -eq 1 ]                        # A8.5, guarded per AX.5
grep -rn 'type staticHandler' internal/ --include=*.go; rc=$?; [ "$rc" -eq 1 ]           # A8.5, guarded per AX.5
grep -n 'static.NewHandler' internal/utuber/build.go                                     # A8.5's third clause: `:58` routes through the platform handler
node scripts/gates/clean-tree.mjs internal/ cmd/ unified-webapp.json unified-webapp-example.json README.md
```

Plus the table-driven test behind A8.2: for every name in `knownModules` (`cmd/server/main.go:337`, 13 names), build a dispatcher via `buildDispatcher` with that module routed to a host, then assert `GET http://<host>/shared/dist/shared.css` is **200 or 503, never 404** — 503 being `unavailableHandler`'s response for a module that is configured off, which still proves the route exists — and that `GET /shared/ts/modal.ts` is 404. And the companion test asserting `dispatch.closers` is non-empty for a certmachine route (A8.3), the direct regression guard for iteration-2's blocker 1.

**Why the gates in this step pass at this boundary.** `/shared/dist/shared.css` does not exist in the tree until C4, so A8.1's 200 assertion is exercised against a test fixture directory in `shared_test.go`, and A8.2's dispatcher test asserts **200-or-503-never-404** — which is satisfiable with or without the real artifact present. Nothing in this step asserts a property of a file a later commit creates.

### Step 5 — `web/shared/ts/`: the modal lift and the shared bundle (C4, deliverable D5 part 1)

**Creates:** `web/shared/ts/modal.ts`, `web/shared/ts/index.ts`, `web/shared/ts/theme.ts`, `web/shared/ts/modal.test.ts`, `scripts/check-shared-barrel.mjs`, `web/shared/dist/shared.mjs`, `web/shared/dist/shared.css`
**Modifies:** `scripts/descriptors.mjs` (two descriptors + the `@shared` plugin + `EXPECTED_ARTIFACT_COUNT` → 14), `web/shared/css/components.css` (the `.ui-modal-*` rule bodies), `Makefile` (one line: `node scripts/check-shared-barrel.mjs` added to the `gates` recipe), `tsconfig.json`

**Why the barrel gate's Makefile line lands here and not at C1** *(Critic amendment A)*. `make gates` must not name a script that does not exist: a `gates` recipe invoking `check-shared-barrel.mjs` from C1 would fail C1, C1b, C2 and C3 outright, and a stub that exits 0 until C4 would be a gate that has never been capable of failing — exactly what R19 and AX.6 forbid. So the recipe line and the script land in the same commit, and §4's gate-composition table records that `make gates` runs three scripts through C3 and four from C4.

**The two new descriptors:**

| name | entry | mode | format | outfile | extra |
|---|---|---|---|---|---|
| shared | `web/shared/ts/index.ts` | bundle | **esm** | `web/shared/dist/shared.mjs` | — |
| shared-css | `web/shared/css/index.css` | bundle | — | `web/shared/dist/shared.css` | `external: ["*.woff2"]` (Step 2) |

Both entries exist as of this step; driver rule 1 puts them first.

**The lift.** `web/taskmaster/js/ui/modal.ts` (390 lines) becomes `web/shared/ts/modal.ts` with three mechanical changes and nothing else:

1. **`ensureStyles()` is deleted entirely** — the injected CSS block (`:20-106`), its `STYLE_ATTR = "data-tm-ui-modal-styles"` sentinel (`:17`), and its call site (`:130`) all go. Those rules live in `web/shared/css/components.css` (Step 1.6), retokenized.
2. **Class prefix `tm-modal-*` → `ui-modal-*`** throughout — all 8 literals: `tm-modal-overlay`, `tm-modal-panel`, `tm-modal-title`, `tm-modal-message`, `tm-modal-input`, `tm-modal-actions`, `tm-modal-btn`, `tm-modal-btn-primary`.
3. **Every colour/metric literal and every Obsidian-era token resolves to a shared token.** Complete map:

| donor (`web/taskmaster/js/ui/modal.ts`) | shared token |
|---|---|
| `var(--bg-secondary, #252525)` `:36` — panel bg | `var(--color-surface-2)` |
| `var(--bg-primary, #1e1e1e)` `:62` — input bg | `var(--color-bg)` |
| `var(--bg-tertiary, #2d2d2d)` `:82` — secondary button bg | `var(--color-surface-3)` |
| `var(--text-normal, #dcddde)` `:37`, `:63`, `:83` | `var(--color-text)` |
| `var(--bg-modifier-border, #3a3a3a)` `:38`, `:64`, `:81` | `var(--color-border)` |
| `var(--radius, 6px)` `:39`, `:65`, `:80` | `var(--radius-md)` |
| `var(--font-ui, -apple-system, …)` `:33` | `var(--font-body)` |
| `var(--interactive-accent, #7f6df2)` `:71`, `:88-89`, `:93`, `:97-98` | `var(--color-primary)` |
| `var(--interactive-accent-hover, #9d8fff)` `:94`, `:102` | `var(--color-primary-hover)` |
| `rgba(0, 0, 0, 0.55)` `:28` — overlay scrim | `var(--overlay-scrim)` |
| `0 8px 32px rgba(0, 0, 0, 0.4)` `:44` — panel shadow | `var(--shadow-md)` |
| `color: #fff` `:99` — primary button text | `var(--color-primary-fg)` |

**Focus ring — one answer.** The shared focus ring is **`--color-primary`** (`:71` and `:93` are `:focus-visible` rules); `:94`'s `box-shadow` spread colour is the one `-hover` site inside a focus rule and maps to `--color-primary-hover`, matching its donor literal `#9d8fff`. T7 rows 8 and 9 carry the resulting deltas.

**What is preserved byte-for-byte:** the focus trap (`getFocusable` + the Tab-wrapping `onKeydown`, `:181-187`), `onOverlayClick` (`:191-195`), `close()` and focus return via `previouslyFocused.focus()` (`:197-207`), and the four exported signatures — `openModal` (`:129`), `confirmDialog` (`:226`), `alertDialog` (`:277`), `promptDialog` (`:324`) — plus `ModalOptions`, `ModalHandle`, `DialogOptions`, `PromptOptions`. FR-5's behavioural requirements are met because they are the donor's existing behaviour, unmodified. **Reviewers should diff those ranges specifically**: the lift will *not* read as a rename (≈80 CSS lines deleted, every class renamed, 12 values retokenized), so rename detection is unlikely to fire, and the narrow checkable claim is byte-identity of the focus trap and the four signatures.

**Security invariants carried forward unchanged:** all user-supplied text goes through `textContent`; no `innerHTML` of an interpolated string anywhere; no `alert()`/`confirm()`/`prompt()`. A5.4.

**`web/shared/ts/index.ts`** — explicit named re-export barrel, and **the library's complete public surface**:
```ts
export { openModal, confirmDialog, alertDialog, promptDialog } from "./modal.js";
export type { ModalOptions, ModalHandle, DialogOptions, PromptOptions } from "./modal.js";
export { THEMES, setTheme } from "./theme.js";
```

**Why `theme.js` is re-exported here, stated because it was the Architect's blocker B1.** Driver rule 10 collapses *every* `@shared/...` specifier to the single URL `/shared/dist/shared.mjs`, and that bundle's entry point is this file. A symbol reachable only by importing `./theme.js` directly is therefore reachable by **no** consumer: the sampler's `import { THEMES } from "@shared/theme"` would typecheck against source through `tsconfig`'s `paths` and then, at runtime, request a named export the emitted bundle does not have. v5 asserted both "`shared.mjs` **is** the barrel" and "`theme.ts` exposes `THEMES` … for the sampler" while the barrel re-exported only `modal.js`; the two could not both be true. Re-exporting `theme.js` is the fix that keeps the collapse — the alternative, a second bundle and a second URL, would make `@shared` two entry points and defeat FRD §7.1's one-shared-bundle requirement.

**The barrel allowlist** — `scripts/check-shared-barrel.mjs` asserts that the file's exported-name set **equals**:

| | Allowlisted exports |
|---|---|
| **6 named values** | `openModal`, `confirmDialog`, `alertDialog`, `promptDialog`, `THEMES`, `setTheme` |
| **4 types** | `ModalOptions`, `ModalHandle`, `DialogOptions`, `PromptOptions` |

Missing and extra are both failures, reported by name — so a symbol added to `modal.ts` or `theme.ts` and forgotten in the barrel fails the gate here rather than producing an `undefined` import in a browser, and a symbol added to the barrel without a decision fails it too. The gate also bans `export *` outright: a star export makes the public surface implicit, which is what an allowlist exists to prevent, and it would make A5.2's shape assertion meaningless. **An allowlist, not a hard-coded "four and four"** — that literal form was what made B1's fix impossible to express. A5.2 is the criterion that reads this table.

**`web/shared/ts/theme.ts`** exports `THEMES` — the canonical 8-name array — and `setTheme(name)`, which writes `document.documentElement.dataset.theme` (Step 1.0). FRD `:226-228` makes `THEMES` the one JS-side source of the theme list — "Adding a theme = adding one CSS block + one entry to a single `THEMES` array" — which is what §9 item 12 later uses to de-duplicate obsidianoid's CSS/TS theme list, and what A10.3 makes load-bearing by requiring the sampler's `<select>` to iterate it rather than carry a second copy. Both symbols are re-exported by the barrel above; nothing in Phase 1 imports `./theme.js` by path.

**`tsconfig.json`.** `include` goes from **9** globs to **11**, adding `web/shared/**/*.ts` and `web/sampler/js/*.ts`. `paths` gains exactly two keys — `"@shared": ["./web/shared/ts/index.ts"]` and `"@shared/*": ["./web/shared/ts/*"]` — with **no `baseUrl`**, which works because `moduleResolution: "bundler"` resolves `paths` against the tsconfig's own directory (verified: the one-key map alone gives TS2307 on a bare `@shared` import). `exclude` gains `web/shared/dist` so the emitted `.mjs` never enters the program. The esbuild plugin filter, the tsconfig `paths`, Step 6's sampler import, and Step 7's shim all use identical specifiers. A7.8, A7.10.

**Both new globs land here, in C4 — C5 does not touch `tsconfig.json`.** `web/sampler/js/*.ts` therefore matches nothing for one commit, which is harmless (tsc's "No inputs were found" fires only when the *whole* `include` is empty, and the other 10 globs supply inputs) and it means the typecheck configuration is settled in one place rather than edited twice.

**Tests.** `web/shared/ts/modal.test.ts`, run through `scripts/test-web.mjs` with the same cjs/node18 flags. Phase-1 coverage: option normalization and defaults, the `textContent`-only escaping path, and the barrel's export shape — driven against a ~20-line hand-rolled element stub defined in the test file. Focus-trap, Escape, focus-return and backdrop-click are verified per theme through `docs/sampler-checklist.md`. **ADR-005** records why there is no jsdom, and this test file is the thing that makes that ADR a live decision rather than a historical note.

**Verification**
```sh
node scripts/check-shared-barrel.mjs        # A5.2's allowlist half: 6 named values + 4 types, no `export *`
node scripts/check-shared-css.mjs
make gates                                  # four scripts from this commit onward (§4's composition table)
npm run build
git ls-files --error-unmatch web/shared/dist/shared.mjs web/shared/dist/shared.css   # after add
grep -rEn 'innerHTML' web/shared/ts/; rc=$?; [ "$rc" -eq 1 ]                          # A5.4
grep -rEn '\b(alert|confirm|prompt)\s*\(' web/shared/ts/; rc=$?; [ "$rc" -eq 1 ]      # A5.4
grep -rEn '(createElement\(["'\'']style|style\.textContent)' web/shared/ts/modal.ts; rc=$?; [ "$rc" -eq 1 ]   # A5.6
make web-verify                             # count 14
npm run typecheck && npm run test:web       # five suites
```

**A5.6 exists because the obvious criterion is vacuous.** "`modal.ts` contains no CSS" cannot fail once `ensureStyles()` is deleted — it is true by construction. The criterion above asserts the *mechanism* instead: no `createElement("style")` and no `style.textContent`, so the file cannot inject styles even if someone reintroduces a rule string.

---
### Step 6 — `sampler`, module 14 (C5, deliverable D5 part 2 / FR-10)

**Creates:** `web/sampler/index.html`, `web/sampler/style.css`, `web/sampler/js/main.ts`, `web/sampler/js/bundle.js`, `internal/sampler/build.go` (+ `build_test.go`), `docs/sampler-checklist.md`
**Modifies:** `internal/platform/config/config.go` (`SamplerConfig` + expander + default), `cmd/server/main.go` (`buildModule` case + `knownModules` 13 → 14), `scripts/descriptors.mjs` (one descriptor), `local-test/config.json`, `unified-webapp-example.json`, `docs/adding-a-module.md`

*(The Go and config half was deleted by v4, which left A10.1–A10.4 asserting properties of a page no route served — Architect B6.)*

Follows `docs/adding-a-module.md`'s five-step checklist exactly (`:52`, `:62`, `:65`, `:70`, `:76`): (1) `SamplerConfig{StaticDir string}` modelled on `TimetrackerConfig` (`config.go:267-268`), with an `expandSamplerPaths` call beside `expandTimetrackerPaths` (`:603`, def `:684`) and a `DefaultConfig` entry defaulting to `./web/sampler` (beside `:476`); (2) a `buildModule` case; (3) a `knownModules` entry (`cmd/server/main.go:337`, 13 → **14**); (4) a `host_routing` entry in `local-test/config.json` (`:5-20`) and in `unified-webapp-example.json`; (5) static assets under `web/sampler/`. `docs/adding-a-module.md` gains the shared-asset line: a new module gets `/shared/` for free from the dispatcher mount, and may opt into `MountShared` if it wants the subtree inside its own middleware.

`internal/sampler/build.go` is the **documented per-module `MountShared` example**:

```go
mux := http.NewServeMux()
if err := static.MountShared(mux, cfg.SharedStaticDir); err != nil { return nil, err }
mux.Handle("/", static.NewHandler(cfg.StaticDir))
```

The dispatcher-level `WithShared` from C3 already covers `/shared/` for every host including sampler's, so this mount is redundant at runtime — **deliberately.** It keeps `MountShared` exercised by a real module, gives `docs/adding-a-module.md` something to point at, and is the migration path for any module that later wants the subtree inside its own middleware. `MountShared` returns an error (rather than panicking) and validates that it received an `*http.ServeMux`, so a chi router passed by mistake is reported at boot rather than silently registering a wrong exact-match pattern — see R8.

**Auth-gated like every other module** — one `auth.modules` entry in the sample configs. No special-casing. (`internal/platform/auth/gate.go:157` computes `protected := hasEntry || module == "admin"`, so a module with an entry is protected; the sampler renders shared CSS and nothing private, but consistency is cheaper than an exception.)

**Page content (Phase 1):** `index.html` links `/shared/dist/shared.css` **and its own `style.css`, and nothing else colour-bearing**; a theme `<select>` **populated by iterating `THEMES` from `@shared`** and calling **`setTheme`** on change — which writes `document.documentElement.dataset.theme` — through all 8 values; a swatch grid rendered from T1's 17 keys and a specimen block for each of T2's 27 tokens; a modal section with buttons for `openModal`, `confirmDialog`, `alertDialog`, `promptDialog`; and a source snippet beside each, rendered via `textContent` into a `<pre>` (never `innerHTML`). **The theme half of the page is why the barrel exports `THEMES` and `setTheme`** (B1): the sampler is the only Phase-1 consumer that switches themes at runtime, `@shared` collapses to one bundle URL, and a hard-coded second theme list in the sampler would violate FRD `:226-228`'s single-source requirement outright. `web/sampler/js/main.ts` imports from `@shared` and bundles to `web/sampler/js/bundle.js` with **`format: "esm"`** and **`sharedConsumer: true`** (driver rule 10 asserts the format, A9.1/A9.2 gate the result, and this descriptor is the first member of `bundle-shape.mjs`'s input set).

**`docs/sampler-checklist.md`** is the manual verification surface for the behaviour Phase-1 automated tests do not cover (ADR-005). For each of the 8 themes: open each of the four dialogs; Tab cycles within the panel and wraps; Shift-Tab wraps backwards; Escape closes; focus returns to the invoking button; backdrop mousedown closes; primary-button text is legible against `--color-primary` (the `--color-primary-fg` check); no element is unreadable. Checked once per theme before C5 is considered done, and re-run in every later phase that touches shared CSS. **Three follow-ups defer decisions to this document** (§9 items 10, 11, and the obsidian legibility note in item 1), which is why it is a committed artifact rather than a paragraph in this plan.

**Verification**
```sh
test -z "$(gofmt -l .)" && go vet ./... && make test
npm run build && npm run typecheck && npm run test:web
make web-verify                              # count 15
node scripts/gates/bundle-shape.mjs          # A9.1/A9.2 over web/sampler/js/bundle.js — first non-empty input set
# then seed format:"iife" on the sampler descriptor and re-run: must exit non-zero   # A9.4
grep -c '"sampler"' cmd/server/main.go       # buildModule case + knownModules
curl -sI http://sampler.local:PORT/shared/dist/shared.css                   # 200 + Last-Modified, via local-test
curl -so /dev/null -w '%{http_code}' http://sampler.local:PORT/shared/ts/modal.ts   # 404
# then: docs/sampler-checklist.md, all 8 themes
```

**Why the gates in this step pass at this boundary.** A10.1's route exists because C3 landed the handler and this commit lands the module; A10.2–A10.6 are properties of files this commit creates and a server this commit can start. This is the ordering decision §4 records: the sampler's Go half cannot come after its gates.

### Step 7 — taskmaster consumes the shared modal (C6, deliverable D6)

This is the **only** commit in Phase 1 that changes a rendered pixel, and only on taskmaster's page.

**Modifies:** `web/taskmaster/index.html` (two edits), `web/taskmaster/js/ui/modal.ts` (390 lines → a 2-line re-export shim), `scripts/descriptors.mjs` (taskmaster descriptor: `format: "esm"` and `sharedConsumer: true`), `web/taskmaster/js/bundle.js` (regenerated)

**Adoption via re-export shim** (ADR-003). `web/taskmaster/js/ui/modal.ts` becomes:

```ts
export { openModal, confirmDialog, alertDialog, promptDialog } from "@shared/modal";
export type { ModalOptions, ModalHandle, DialogOptions, PromptOptions } from "@shared/modal";
```

All five importing call sites — `api.ts:10`, `designer.ts:23`, `outputmodal.ts:13`, `board.ts:27`, `main.ts:16`, each importing `'./ui/modal.js'` — are **untouched**.

**The shim names four functions, the barrel exports six.** That asymmetry is the point, not an inconsistency: the barrel is the library's complete public surface (Step 5, A5.2's allowlist) because `@shared` collapses to one URL and a symbol absent from the barrel is unreachable by *any* consumer; the shim is taskmaster's surface, and ADR-003's "keep C6 small" constraint is enforced **there**. `THEMES` and `setTheme` exist in `shared.mjs` and stay unreachable through `./ui/modal.js`, which is correct — taskmaster does not pick a theme at runtime in Phase 1; its `<html>` attribute does.

Two further consequences the shim buys:

- **Two prose comments stay true.** `api.ts:6` ("UI code surfaces failures via `ui/modal.ts`'s …") and `designer.ts:20` ("the only dialog used for validation errors is `ui/modal.ts`'s `alertDialog`") remain accurate through the shim. A naive `grep -rn 'ui/modal' web/taskmaster/` would match these prose lines and report a false failure; with the shim there is nothing to grep for. The correct narrow form, if one is ever wanted, is `grep -rn "from '\./ui/modal" web/taskmaster/js/ --include=*.ts` — and under the shim it is simply not applicable, by design.
- **C6 stays a small revert.** Two hand-edited files, one descriptor field, and one regenerated artifact.

**`web/taskmaster/index.html` — exactly two edits:**

1. `:2` becomes `<html lang="en" data-theme="obsidian">` (Step 1.0, ADR-007, A10.7).
2. `<link rel="stylesheet" href="/shared/dist/shared.css">` is inserted **before** `:7`'s `/style.css`, so taskmaster's own rules keep winning on equal specificity.

The attribute is not cosmetic. Without it the page inherits `:root`'s canonical **`dark`** values (ADR-007), the modal renders in a generic dark palette instead of obsidian's violet, and every obsidian-specific claim in Step 1.4 and T7 is unfounded — a small enough shift that only A10.7 reliably catches it, which is why A10.7 is a gate and not a note. `:12`'s `<script type="module">` needs **no change** — the page is already a module script, so the iife → esm switch is invisible to the HTML.

**`web/taskmaster/js/main.ts` is not edited.** The frontend build stamp reaches the UI through `web/taskmaster/js/buildinfo.ts:11` (`declare const __TM_BUILD_TIME__: string;`) and `:13` (`export const FRONTEND_BUILD_TIME: string = __TM_BUILD_TIME__;`), so ADR-004 changes only how the driver computes the define. `main.ts:218`'s "Backend build" row (Go ldflags) and `:219`'s "Frontend build" row (the digest) both keep working with no module TS edit. G3's exception budget is therefore **two** hand-edited files, not three (Architect M1, verified).

**Token-collision analysis — complete and mechanically verified.** `web/taskmaster/style.css:6-24` declares exactly 17 custom properties: `--bg-modifier-border`, `--bg-primary`, `--bg-secondary`, `--bg-tertiary`, `--font-mono`, `--font-ui`, `--interactive-accent`, `--interactive-accent-hover`, `--radius`, `--status-blue`, `--status-green`, `--status-red`, `--status-yellow`, `--text-accent`, `--text-faint`, `--text-muted`, `--text-normal`. Intersected against the whole shared vocabulary (T1's 17 + T2's 27): **`--font-mono` is the only overlap.** Two near-misses that are *not* collisions: taskmaster's `--radius` does not collide with `--radius-md`, and its `--text-accent`/`--text-faint`/`--text-muted`/`--text-normal` do not collide with the `--text-xs..xl` size scale — different names, so no cascade interaction. Both declarations sit on `:root` at specificity (0,1,0), so source order decides; `shared.css` is linked **first**, therefore taskmaster's `'JetBrains Mono', 'Fira Code', 'Cascadia Code', monospace` wins at all **five** of its consumers (`style.css:288`, `:499`, `:613`, `:671`, `:736`). **No live regression** — and it is documented here rather than discovered later.

The mechanical check is `scripts/gates/token-overlap.mjs`, run from `make gates` (A9.5). It computes:

```
comm -12 <(shared token names, sorted -u) <(web/taskmaster/style.css token names, sorted -u)
```

with an expected output of exactly `--font-mono`. If that set ever grows, the gate fails and the new overlap must be analysed before landing. *(This is a gate file, not a shell one-liner in a recipe: G11, and process substitution is not portable inside a make recipe anyway.)*

**Table T7 — sanctioned visual deltas, re-derived for v5.** This is the complete set of pixel changes C6 may produce; anything else is a defect (§11 principle 3).

**How this table was built** (v4's six rows and v3's six rows are both superseded): every `var(--x, fallback)` in the donor's `ensureStyles()` block was read against `web/taskmaster/style.css:6-24`, and **every fallback turned out to equal the declared value** — `--radius: 6px` at `:23`, `--bg-secondary`, `--text-normal`, and the rest. So the fallbacks never fire, and each row's "Before" is the value `style.css` actually supplies today. "After" is the obsidian value the retokenized `components.css` rule resolves to under `[data-theme="obsidian"]`.

| # | Site in `modal.ts` | Before (from `style.css`) | After (obsidian) | Net |
|---|---|---|---|---|
| 1 | `:36` panel background | `#252525` | `--color-surface-2` `#1f1f2e` | small shift, slightly cooler |
| 2 | `:37`, `:63`, `:83` text | `#dcddde` | `--color-text` `#d4d4e8` | small shift, slightly cooler |
| 3 | `:38`, `:64`, `:81` borders | `#3a3a3a` | `--color-border` `#333348` | small shift, slightly cooler |
| 4 | `:39`, `:65`, `:80` radius | **`6px`** (declared at `style.css:23`, *not* a UA default) | `--radius-md` `0.5rem` = **8px** at the 16px root (`style.css` has no `html {` rule) | **a real 6px → 8px change** on the modal panel, input, and button corners. *v3 called this a no-op; v4 got the direction right and the mechanism wrong (Architect B4). The mechanism is that `components.css` authors `--radius-md`, which the lifted rule reads in place of `--radius`.* |
| 5 | `:33` modal font | `--font-ui` → `-apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif` | `--font-body` → `"Inter", …`, and **Inter now actually resolves** because C1b registers the face | a new face inside modal DOM |
| 6 | `:62` input background | `#1e1e1e` | `--color-bg` `#13131a` | small shift |
| 7 | `:82` secondary button background | `#2d2d2d` | `--color-surface-3` `#252535` | small shift |
| 8 | `:71`, `:88-89`, `:93`, `:97-98` accent (input focus ring, button hover border, button focus ring, primary button bg+border) — **four sites** | `#7f6df2` | `--color-primary` `#7c6af7` | near-no-op; 1–2 units per channel |
| 9 | `:94`, `:102` accent-hover (focus `box-shadow` spread, primary button hover) | `#9d8fff` | `--color-primary-hover` `#9580ff` | near-no-op |
| 10 | `:99` primary button text | `#fff` | `--color-primary-fg` `#ffffff` | **value unchanged**; the measurable change is the contrast ratio against row 8's new background: **3.91:1 → 3.99:1** (§9 item 1) |
| 11 | **Outside modal DOM** — `style.css:288`, `:499`, `:613`, `:671`, `:736` (`var(--font-mono)` consumers) | `'JetBrains Mono'` named but **not resolvable** — no face registered, so the stack fell through to `'Fira Code'`/`'Cascadia Code'`/`monospace` | `'JetBrains Mono'` **resolves**, because C1b registers the face and C6's `<link>` brings `fonts.css` onto the page | **the one delta that occurs regardless of the lift** (Architect M13): it follows from C1b + C6's `<link>` alone. Named so the screenshot pass has a correct oracle for the five code/`textarea` surfaces. |

**Asserted-zero rows** (reproduced exactly, so they must *not* move): `:28`'s scrim `rgba(0,0,0,0.55)` → `--overlay-scrim`, same literal; `:44`'s `0 8px 32px rgba(0,0,0,0.4)` → `--shadow-md`, same literal. Those two tokens diverge from obsidianoid (§8 row 12) precisely so these rows are zero.

**Blast radius outside modal DOM is row 11 and nothing else,** and that is provable rather than hopeful: `components.css` contains only `.ui-modal-*` selectors (A1.10), so no rule in `shared.css` matches a non-modal element on taskmaster's page; and the only token-name overlap between the two sheets is `--font-mono` (A9.5), on which taskmaster wins by source order. Row 11 is a *font-availability* change, not a cascade change — which is why it survives the selector argument.

**Byte-identity at this boundary.** `scripts/gates/artifacts.mjs --allow web/taskmaster/js/bundle.js` rebuilds and asserts that the only artifact whose bytes moved is taskmaster's bundle. The allowance is an explicit argv flag on a gate file, not a `grep -v` inside a recipe — v4's recipe form was the always-pass hazard (Architect B7).

**Verification**
```sh
npm run build && npm run typecheck && npm run test:web && make test
node scripts/check-shared-css.mjs && node scripts/check-shared-barrel.mjs
node scripts/gates/bundle-shape.mjs                 # A9.1-A9.3 over web/sampler/ and web/taskmaster/js/bundle.js
node scripts/gates/token-overlap.mjs                # A9.5: exactly --font-mono
node scripts/gates/artifacts.mjs --allow web/taskmaster/js/bundle.js
grep -c 'data-theme="obsidian"' web/taskmaster/index.html    # A10.7: 1, on line 2
# manual, in local-test: load taskmaster, exercise all four dialogs through the shim (A5.5),
# confirm Tab/Shift-Tab/Escape/focus-return/backdrop-close, and confirm nothing outside a modal
# moved except T7 row 11's five --font-mono surfaces (side-by-side against the pre-C6 build)
```

`scripts/gates/bundle-shape.mjs` **derives its input set from the descriptors, not from a pattern**: it imports `scripts/descriptors.mjs` and inspects the `out` artifact of every descriptor declaring `sharedConsumer: true` — the same field driver rule 10 uses to select the `@shared` plugin and to assert `format: "esm"`, so gate and build cannot range over different sets *(Critic amendment B.4: "every bundle that consumes `@shared`" named no mechanism, and a gate whose input set is a guess can silently inspect nothing)*. It **fails if that set is empty at a boundary where §4's composition table says it should not be**, so the vacuous pass at C1–C4 is a stated condition rather than an unnoticed one. For each bundle in the set it asserts, two-sided (G5):
- **positive:** a top-level `import … from "/shared/dist/shared.mjs"` statement is present;
- **negative:** neither `__require(` nor `Dynamic require of` appears anywhere in the file;
- and for taskmaster specifically: `var FRONTEND_BUILD_TIME` appears **exactly once** (the define was applied, once).

v3 also grepped for `^\s*this\b` as an iife-residue check. **That is deleted, not restored:** it has 22 legitimate matches in taskmaster's committed bundle today, so it was never a gate.

---

## §4 — Commit sequence

7 commits, one per step, pixels last. Each names every path it touches; P2's gate is `scripts/gates/clean-tree.mjs <those paths>`, which leaves P6's side-car files alone.

| | Commit | Touches | Gates at this boundary | Artifacts |
|---|---|---|---|---|
| **C1** | shared CSS foundation **+ the gate harness** | `web/shared/css/{tokens,themes,components,index}.css`, `scripts/check-shared-css.mjs`, `scripts/gates/{artifacts,bundle-shape,token-overlap,clean-tree}.mjs`, `scripts/descriptors.mjs`, `scripts/list-artifacts.mjs`, `Makefile` (adds `web-verify` and `gates`), `.gitignore` | A1.1–A1.8, A1.10, A1.11, A2.1–A2.5, **A7.2, A7.3, A7.7, A7.15 (the two targets this commit defines), A7.17**, AX.1, AX.3, AX.4, AX.5 **+ the byte-identity exemption below**. Not A1.9 (no `fonts.css`), not A2.6 (no sampler), not A5.x/A6.x/A9.x/A10.x (no consumer, no faces, no `sharedConsumer` descriptor) | 12 |
| **C1b** | fonts | `web/shared/public/fonts/**` (20 files), `web/shared/css/fonts.css`, `web/shared/css/index.css` | every C1 gate re-run, **+ A1.9, A6.1, A6.2 + the exemption** | 12 |
| **C2** | build + test drivers, `package.json`, the remaining Makefile targets | `scripts/{build-web,test-web}.mjs`, `package.json`, `Makefile` (`web:`, `typecheck:`, `test-web`, `check`), `web/taskmaster/js/bundle.js` | A7.1, A7.4–A7.6, A7.11, A7.12, A7.14, A7.15 (roster now complete), A7.16, AX.3–AX.5, plus every C1 gate re-run (A7.2, A7.3, A7.7, A7.17). **The exemption ends here**, and A7.4 asserts it is gone | 12 |
| **C3** | Go `/shared/` route | `internal/platform/static/{shared.go,shared_test.go}`, `internal/platform/config/config.go`, `cmd/server/main.go`, `cmd/server/dispatcher_auth_test.go`, `internal/utuber/build.go`, `unified-webapp.json`, `unified-webapp-example.json`, `README.md` | A8.1–A8.8, A7.14, AX.7, plus every C2 gate | 12 |
| **C4** | shared TS + modal lift + emitted bundle | `web/shared/ts/{modal,index,theme,modal.test}.ts`, `web/shared/css/components.css`, `web/shared/dist/**`, `scripts/{descriptors.mjs,check-shared-barrel.mjs}`, `Makefile` (one line added to `gates`), `tsconfig.json` | A5.1, A5.2 (both halves — the barrel allowlist gate lands with the script that implements it), A5.6, A6.5, A7.8, A7.10, A7.13, AX.4 (count **14**). **Not A9.x:** the `sharedConsumer` set is still empty at this boundary | **14** |
| **C5** | sampler, module 14 | `web/sampler/{index.html,style.css,js/main.ts,js/bundle.js}`, `internal/sampler/{build.go,build_test.go}`, `internal/platform/config/config.go`, `cmd/server/main.go`, `scripts/descriptors.mjs`, `local-test/config.json`, `unified-webapp-example.json`, `docs/{adding-a-module.md,sampler-checklist.md}` | A10.1–A10.6, A10.8, A2.6, A6.3, A6.4, **A9.1, A9.2, A9.4** (first boundary with a `sharedConsumer` descriptor), AX.4 (count **15**) | **15** |
| **C6** | taskmaster adoption | `web/taskmaster/index.html`, `web/taskmaster/js/ui/modal.ts`, `web/taskmaster/js/bundle.js`, `scripts/descriptors.mjs` | A5.4, A5.5, A5.7, A9.1–A9.3, A9.5, A10.7, AX.6, T7's byte-identity assert with `--allow` | 15 |

**Ordering decision, and why every gate is passable at its own boundary.** *(This is the question v4 never answered, and the reason Architect B6 exists.)*

- **C1 before everything:** `components.css` must exist before C4's lift has a destination, and the gate scripts must exist before any boundary runs them. **This is why `scripts/descriptors.mjs`, `scripts/list-artifacts.mjs` and the `web-verify`/`gates` Makefile targets are in C1's manifest and not C2's** *(Architect B2)*: `artifacts.mjs` reads `EXPECTED_ARTIFACT_COUNT` and the generated path list, so a C1 that ships the gate without its data cannot run AX.4's "12 at C1" or A7.2/A7.3/A7.7 at all — the gate would first be invokable one commit after the boundary it is supposed to guard. The dependency runs the other way round from the obvious reading: `descriptors.mjs` is **data** and needs nothing the drivers create, while `build-web.mjs` and `test-web.mjs` (C2) are what need `descriptors.mjs`.
- **The `descriptors.mjs` trail is four commits — one create and three edits — by design.** Created at **C1** with the seven existing descriptors and `EXPECTED_ARTIFACT_COUNT = 12`; **C4** adds the `shared` and `shared-css` descriptors, the `@shared` `onResolve` plugin and the count 14; **C5** adds the `sampler` descriptor with `sharedConsumer: true` and the count 15; **C6** flips the taskmaster descriptor to `format: "esm"` and sets `sharedConsumer: true`. **C2 and C3 do not touch it at all** — this is the record B2 asked for, and it is also why C2's manifest no longer lists the file. Each of the three edits is the single definition of a fact that changes at that boundary, which is what §12 clause (d) requires — not four copies of one fact.
- **C1b before C4:** `shared.css` is first emitted at C4, and it must contain the `@font-face` rules, or T7 row 5 and row 11 are wrong at C6.
- **C2 before C3, C4, C5, C6:** every later commit's verification runs `npm run build`, which is the driver. (C3 is the one commit that does not otherwise depend on C2's output; it depends on it only through the verification block.)
- **C3 before C5:** the sampler's `build.go` calls `MountShared`, and A10.1's route must exist for the sampler's own gates to run. This is the fix for Architect B6: v4 ordered the sampler before any server route existed, so A10.x could not pass at C5.
- **C4 before C5 and C6:** both consume `@shared`, which resolves to `web/shared/dist/shared.mjs`.
- **C6 last:** it is the only commit that changes a pixel.

**Gate composition by boundary** (G6 requires every point where composition differs to be named here, and three of them differ):

| Boundary | `make gates` runs | Artifact gate | Notes |
|---|---|---|---|
| C1, C1b | `check-shared-css.mjs`, `bundle-shape.mjs`, `token-overlap.mjs` | `node scripts/gates/artifacts.mjs --allow web/taskmaster/js/bundle.js`, invoked **directly** | `make web-verify` (no `--allow`) is not used at these two boundaries — the flag *is* the byte-identity exemption's mechanism, and an argv flag on a gate file is auditable where a `grep -v` in a recipe is not. `bundle-shape.mjs` inspects zero files (empty `sharedConsumer` set) and passes vacuously; that is recorded rather than counted as evidence. `make check` does not exist yet. |
| C2, C3 | same three | `make web-verify` | The exemption is gone, so the plain target is clean. `make check` exists from C2 and runs all four targets (A7.16). |
| C4 | **four** — `check-shared-barrel.mjs` is added to the `gates` recipe by this commit, in the same commit that creates the script | `make web-verify` (count 14) | The one line of Makefile edit that C4's manifest carries. It is **not** a no-op stub placed at C1: a `gates` recipe invoking a script that does not exist would fail every boundary from C1 to C3, and a stub that exits 0 would be a gate that has never been able to fail, which R19 and AX.6 exist to forbid — *Critic amendment A*. |
| C5, C6 | same four | `make web-verify` (count 15); C6 adds `artifacts.mjs --allow` for T7 | C5 is the first boundary at which `bundle-shape.mjs` has an input, so A9.1/A9.2/A9.4 are gates there rather than vacuous passes. |

**The one byte-identity exemption, stated rather than denied.** At C1 and C1b, `package.json:5-6` still embeds `$(date -u …)`, so **any** rebuild re-stamps `web/taskmaster/js/bundle.js` unconditionally. Those two boundaries therefore run `clean-tree.mjs` with `web/taskmaster/js/bundle.js` excluded and `artifacts.mjs` with `--allow web/taskmaster/js/bundle.js`, and do not assert byte-identity for it. **The exemption disappears at C2**, where the digest replaces the timestamp and the artifact is committed in its new form; A7.4 (two consecutive builds produce identical bytes) is the assertion that it is gone. v4's G6 said "there are no path-scoping exceptions" while shipping the mechanism that guarantees one — this states the exception, bounds it to two commits, and names the gate that retires it.

**Revert order is C6-then-C4** (R16): reverting C4 while C6 is in place would leave the shim importing a `@shared` that no longer exists. Recorded in both commit messages.

**If C1b has to defer** — a license question, a digest mismatch — it defers **past C4 and C6 rather than blocking them.** `index.css` simply carries no `@import "fonts.css";` line, `check-shared-css.mjs` does not resolve imports (Step 1.7), and C4's `shared.css` is emitted without `@font-face` rules. The cost is **one extra artifact commit** to re-emit `shared.css` when C1b lands, plus T7 rows 5 and 11 becoming *deferred* deltas rather than sanctioned ones. The sequence does not pause.

**Artifact count by commit:** 12 through C3, **14** after C4, **15** after C5 and C6. This is the one place a count appears more than once, and it is unavoidable — the quantity moves during the sequence. All three values are assertions made by the same gate reading `EXPECTED_ARTIFACT_COUNT`, not three independent lists; §12 clause (d) records this as the single stated exception to *one definition per fact*.

---
## §5 — Acceptance criteria

**What the labels mean, and the census.** A criterion **without** a `[deferred]` label was executed against this tree and its output is recorded with it. A criterion **with** `[deferred]` asserts a property of code Phase 1 has not written yet: it cannot be executed before the commit that creates its subject, so it names the boundary where it first runs and states what was verified in its place. **8 of the 74 criteria below were executed against this tree; 66 carry `[deferred]`.** That is the honest shape of a foundation plan — almost every gate here asserts a property of a file this phase is about to create — and v4's claim that only four were deferred was false for roughly 26 of its 46. The census is stated **once, here, by number**; every other passage in this document refers to it by name ("the §5 census"), never by figure.

Gate numbering is **A*n* = FR-*n*** (A1 ↔ FR-1, A5 ↔ FR-5, …), plus A9 for ADR-001's bundle shape and AX for cross-cutting. v4 used a different scheme; the mapping table at the end of this section shows where each v4 criterion went, so nothing is silently dropped.

### A1 — FR-1: the token vocabulary and the shared-CSS linter

*Each criterion names the `check-shared-css.mjs` clause that implements it (Step 1.7), so the gate and the criterion cannot drift.*

- **A1.1** Every theme block's `--color-*` property set **equals** T1's 17 names — missing and extra are both failures, reported by name. `[deferred → C1]` *In place:* T1 was derived cell-by-cell from the three donors (Step 1.1). *(clause 5)*
- **A1.2** The only non-`--color-*` properties permitted inside a theme block are the three-member allowlist `{--font-body, --font-mono, --overlay-scrim}` — puma's two `--font-*` overrides (FRD `:222-223`) and light's scrim (Q1). A fourth fails, naming the property and line. `[deferred → C1]` *(clause 6)*
- **A1.3** `themes.css`'s depth-1 selector roster equals the 8 expected strings, **and** the running total of `--color-*` declarations across those 8 blocks is exactly **136**. `[deferred → C1]` *(clauses 4, 5)*
- **A1.4** `tokens.css` is exactly one depth-1 `:root` block whose property set **equals** T2's 27 names. The two marked ★ (`--space-7`, `--text-xl`) are authored and ledgered (§8); every other one cites a donor line. `[deferred → C1]` *(clause 3)*
- **A1.5** No `@import` anywhere under `web/shared/css/` names an external URL (no scheme, no `url(http…)`) — the shared sheet has no network dependency beyond the fonts it self-hosts. `[deferred → C1]` *(clause 8)*
- **A1.6** No colour literal (`#[0-9a-f]{3,8}`, `rgba?(`, `hsla?(`) appears in `components.css`. **The ban is scoped to that one file** — `themes.css` is nothing but literals by definition, and module CSS is out of scope (§8 row 14). The carve-out costs nothing: `box-shadow` and the scrim consume `var(--shadow-md)` and `var(--overlay-scrim)`, whose literals live in `tokens.css`. *(v4 stated the ban unscoped, which forbids the token definitions themselves; v3's scoping is restored.)* `[deferred → C1]` *(clause 7)*
- **A1.7** Every `@import` in `index.css` is a relative same-directory path (no `/`, no `..`), and the set of imported names is a subset of the files actually present. `[deferred → C1]` *(clause 8)*
- **A1.8** No `!important` anywhere under `web/shared/css/`. This is load-bearing for R17: it is why `web/taskmaster/style.css:4`'s `[hidden] { display: none !important; }` keeps winning over any `.ui-modal-*` display rule. `[deferred → C1]` *(clause 9)*
- **A1.9** `fonts.css` declares exactly **15** `@font-face` rules; each `url()` resolves to a file under `web/shared/public/fonts/` (mapping the served `/shared/` prefix back to the repo path), and each `*.woff2` present is referenced by exactly one `url()` — two-sided per G5, so neither an orphan file nor a dangling reference survives. `[deferred → C1b]` *(clause 10)*
- **A1.10** Every selector in `components.css` matches `^\.ui-[a-z0-9-]+`, allowing descendant, pseudo, and attribute continuations. No bare element, id, or universal selector that could match a host page's DOM. This is the proof behind T7's "blast radius is row 11 only", and it is why the linter parses selector lists instead of pattern-matching lines. `[deferred → C1]` *(clause 7)*
- **A1.11** `--color-error` appears nowhere under `web/shared/`; `--color-danger` is the canonical name. `[deferred → C1]` *(clause 11)*

Two properties of the gate itself, rather than of the CSS, are asserted elsewhere: that `make gates` sees the linter's real exit code is **A7.17**, and that the linter has been observed to fail against a seeded violation is **AX.6**. Both halves of A1.3 are necessary and AX.6 proves it: set-equality alone passes a block that declares one key twice and omits another (a set has no multiplicity), and the 136-count alone passes a block of 17 wrong names.

### A2 — FR-2: the eight themes

- **A2.1** Each theme's 17 values match its donor of record — T5a/T5b/T5c, or the FRD's puma block (`:202-217`) — **except** the 14 authored cells enumerated in Step 1.4, of which `--color-primary-fg` is one (it has no donor anywhere: `grep -rn -- '--color-primary-fg' web/` → rc=1). `[deferred → C1]` *(The roster of 8 blocks is A1.3's first clause; this criterion is about the values inside them.)*
- **A2.2** The five obsidianoid-derived themes are named **obsidian, forest, ocean, ember, rose** — obsidianoid's canonical `[data-theme="dark"]` block becomes **obsidian** (FRD FR-2's rename), and canonical `dark` comes from `web/todo/css/todo.css:3-17`. `[deferred → C1]`
- **A2.3** obsidian's 17 values are byte-equal to T5a's obsidian column. `[deferred → C1]`
- **A2.4** Every theme's `--color-primary-fg` clears 4.5:1 against its `--color-primary`, except obsidian's inherited **3.99:1**, which is an inherited condition, ledgered in §8 and carried as §9 item 1. `[deferred → C1]`
- **A2.5** light's `--overlay-scrim` is `rgba(40,37,29,0.35)` (Q1, user-settled). `[deferred → C1]`
- **A2.6** All 8 themes render the sampler with no unreadable pair — `docs/sampler-checklist.md`, manual, once per theme. `[deferred → C5]`

### A5 — FR-5: the modal lift

- **A5.1** `web/shared/ts/modal.ts` is behaviourally identical to the donor: focus trap with Tab/Shift-Tab wrap, Escape closes, backdrop mousedown closes, focus returns to the invoking element, `[hidden]` toggling, and no `innerHTML` anywhere. `[deferred → C4]` *In place:* the donor's behaviour was read line-by-line and the preserved regions are listed byte-range by byte-range in Step 5.
- **A5.2** Two halves, one criterion, because one commit decides both. **CSS:** `shared.css` contains the `.ui-modal-*` rules and `shared.mjs` contains **no** CSS string, no `createElement("style")`, and no `STYLE_ATTR`. **Barrel surface:** `web/shared/ts/index.ts`'s exported-name set **equals** the barrel allowlist — the 6 named values `openModal`, `confirmDialog`, `alertDialog`, `promptDialog`, `THEMES`, `setTheme` and the 4 types `ModalOptions`, `ModalHandle`, `DialogOptions`, `PromptOptions` — with **no `export *`**; missing and extra are both failures, reported by name. `scripts/check-shared-barrel.mjs` implements both directions. `[deferred → C4]` *(Architect B1: an allowlist rather than a hard-coded "four and four", because the barrel must be the library's complete public surface — `@shared` collapses to one URL, so a symbol not in the barrel is unreachable by any consumer. Taskmaster's narrower four-function surface is enforced at the **shim** instead: A5.4, ADR-003.)*
- **A5.3** taskmaster's `style.css` contains zero `tm-modal` references, and taskmaster's CSS/HTML contains zero `.ui-` class references — so the 8-class rename can collide with nothing. **Executed:** `grep -c 'tm-modal' web/taskmaster/style.css` → `0` (exit 1); `grep -rn '\.ui-' web/taskmaster --include=*.css --include=*.html` → 0 lines.
- **A5.4** The shim re-exports exactly the four dialog functions and the four types — **narrower than the barrel's allowlist by design**, so `THEMES` and `setTheme` stay unreachable from taskmaster's `./ui/modal.js` specifier (ADR-003) — and `git diff` shows **no change** to `api.ts`, `designer.ts`, `outputmodal.ts`, `board.ts`, or `main.ts`. `[deferred → C6]`
- **A5.5** **Manual, and mandatory:** all four of taskmaster's dialogs are opened through the shim in the running app under `[data-theme="obsidian"]`, and each one traps focus, closes on Escape, closes on backdrop mousedown, and returns focus. This is the **only behavioural gate on C6**; without it the adoption is asserted, not verified. `[deferred → C6]`
- **A5.6** `modal.ts` contains no `createElement("style")` and no `style.textContent`. *This criterion exists because the obvious one is vacuous:* "`modal.ts` contains no CSS" is true by construction once `ensureStyles()` is deleted and can never fail. Asserting the **mechanism** means the file cannot inject styles even if someone reintroduces a rule string. `[deferred → C4]`
- **A5.7** `web/taskmaster/js/ui/modal.ts` is the 2-line shim — the 390-line donor body is **gone**, not duplicated — and every name it re-exports is a member of the barrel allowlist A5.2 asserts (Step 5's table). The containment direction is what matters here: the shim may be narrower than the allowlist, never wider, because a name the barrel does not export is unreachable through `@shared`'s single-URL collapse. `[deferred → C6]` *(Architect B1 asked that A5.2 and A5.7 both read the allowlist rather than a hard-coded "four and four"; A5.4 asserts the shim's exact four.)*

### A6 — FR-6: self-hosted fonts

- **A6.1** Exactly 15 `.woff2` files ship, `shasum -c SHA256SUMS` passes, and SHA256SUMS has exactly 15 lines. `[deferred → C1b]` *In place:* the 18 staged digests were verified and the 3 dropped lines identified (Step 2).
- **A6.2** Each shipped family carries its license text alongside it (SIL OFL 1.1), per G9. `[deferred → C1b]`
- **A6.3** Loading the sampler issues **no** request to `fonts.googleapis.com` or `fonts.gstatic.com` (DevTools network panel, or a grep of the served HTML/CSS). `[deferred → C5]`
- **A6.4** `--font-body`, `--font-mono`, and `--font-display` each resolve to a registered face (not a fallback) in the browser's computed style. `[deferred → C5]`
- **A6.5** The `shared-css` descriptor sets `external: ["*.woff2"]`, so `shared.css` ships the server-absolute `url("/shared/public/fonts/…")` strings **unrewritten** and esbuild emits **no hashed side-artifacts** — which is what keeps the artifact count at 15 rather than 15 + 15. `[deferred → C4]`

### A7 — FR-7: build driver, typecheck, hygiene

- **A7.1** `npm run build` exits 0 and writes exactly the generated artifact list — nothing extra, nothing missing. `[deferred → C2]`
- **A7.2** The artifact list is **generated** from `scripts/descriptors.mjs` by `scripts/list-artifacts.mjs`; no hand-maintained copy exists (Q5, user-settled). `[deferred → C1]`
- **A7.3** `EXPECTED_ARTIFACT_COUNT` is defined **once**, in `scripts/descriptors.mjs`, and every count assertion reads it. `[deferred → C1]` *(Critic M-6: v4's recipe wrote the expected count twice in one line. Architect B2: the file lands at C1, with the gates that read it, not at C2 — otherwise AX.4's "12 at C1" asserts against a file C1 does not contain.)*
- **A7.4** Two consecutive `npm run build` runs produce byte-identical artifacts. This is the assertion that the `$(date -u)` define is gone and the C1/C1b byte-identity exemption has been retired. `[deferred → C2]`
- **A7.5** `package.json` contains no `date -u`: `grep -c 'date -u' package.json; rc=$?; [ "$rc" -eq 1 ]` — no match, so `grep -c` prints `0` **and exits 1**. `[deferred → C2]`
- **A7.6** The build stamp is a content digest over the metafile input set ∪ `EXTRA`, 15 files, per ADR-004, and re-running the two-pass procedure reproduces `c2c572876987`. `[deferred → C2]`
- **A7.7** A crashing generator **fails** the gate: `scripts/gates/artifacts.mjs` runs `scripts/list-artifacts.mjs` as a child process and propagates a non-zero exit. `[deferred → C1]` *(Critic M-6: `$(shell …)` swallows exit status. C1 per Architect B2, with the generator and the gate it feeds.)*
- **A7.8** `tsconfig.json`'s `include` lists **11** globs — today's 9 plus `web/shared/**/*.ts` and `web/sampler/js/*.ts`. Both globs are added by **C4** (Step 5); C5 does not touch the file. `[deferred → C4]`
- **A7.9** `package.json` declares no `@types/node`, so the shared TS must stay DOM-only. **Executed:** `grep -c '@types/node' package.json` → `0` (exit 1). *(This is ADR-005's first driver, verified rather than assumed.)*
- **A7.10** `tsconfig.json` gains `paths` entries for `@shared` and `@shared/*` with **no** `baseUrl`, keeps `moduleResolution: "bundler"`, and adds `web/shared/dist` to `exclude` so emitted output is never typechecked. `[deferred → C4]`
- **A7.11** `npm run typecheck` exits 0. **Executed:** rc=0 on this tree (9 globs). Re-run at every boundary, and from C4 onward against the 11 globs of A7.8.
- **A7.12** `npm run test:web` exits 0. **Executed:** rc=0 on this tree, 4 suites, all assertions `ok`.
- **A7.13** `npm run test:web` reports **five** suites — the four existing plus `web/shared/ts/modal.test.ts`. `[deferred → C4]` *In place:* A7.12 establishes the runner works; the fifth suite is the file C4 creates.
- **A7.14** `gofmt -l .` outputs nothing, **repo-wide**. **Executed:** 0 lines on this tree, so the repo is already clean and the plan's Go commits inherit a repo-wide standard rather than arguing for a scoped one.
- **A7.15** `.PHONY` lists all 13 targets: today's 9 plus `test-web`, `web-verify`, `gates`, `check`. A non-phony target named after a file that happens to exist silently never runs. The roster is complete after C2 and grows in **two stages** — 9 → 11 at **C1** (`web-verify`, `gates`; Step 1.8) and 11 → 13 at **C2** (`test-web`, `check`; Step 3's `.PHONY` sentence, §4's C1 and C2 rows) — so the criterion is checked at both boundaries against the targets defined at each. `[deferred → C1/C2]`
- **A7.16** `make check` runs `web-verify`, `test-web`, `gates`, and `test`, and fails if any of them fails. Exactly those four: the Go hygiene commands are AX.7's separate mandate. `[deferred → C2]`
- **A7.17** **G11's form is real:** every multi-command gate is a file under `scripts/` invoked as a single process, and no Makefile **recipe** contains `$(shell …)` or a `$(git …)` substitution. *(Recipe-scoped: `Makefile:9`'s backend `BUILD_TIME := $(shell date -u …)` is a variable assignment, not a gate, and is untouched by Phase 1 — Architect M3.)* `[deferred → C1]` *In place:* Architect B7 proved v4's recipe form always passed — make expanded `$(git diff …)` before the shell ran, so the test was `[ -z "" ]`. The same hazard was present in v4's AX.1 and P2. Seeding a byte difference must make this gate fail (R19).

### A8 — FR-8: `/shared/` on every module host

- **A8.1** `internal/platform/static/shared_test.go` covers all **nine** clauses of the pinned handler contract in Step 4, which states for each clause the criterion it maps to. `[deferred → C3]` *(Architect M1 / Critic note 1: v5's contract had seven bullets and claimed each was "a clause of A8.4", which was false for four of them and left A8.4's symlink and dotfile clauses with no contract bullet at all. The contract is now nine bullets with an explicit mapping, and A8.4's six clauses are each named by one.)*
- **A8.2** A table test over all **13** `knownModules` host names asserts `GET /shared/dist/shared.css` answers **200 or 503, never 404** — 503 being the legitimate answer when a module is configured off. The test **iterates the array** rather than hard-coding a count, so C5's 14th entry (A10.6) extends it with no edit. `[deferred → C3]`
- **A8.3** **The invariant:** the `/shared/` wrap at `cmd/server/main.go:303` sits *inside* `svc.Gate` and *after* the `io.Closer` assertion at `:300-302`. The criterion that asserts it: the closers slice is non-empty at the assertion point, and `goleak` in `TestMain` (`cmd/server/dispatcher_auth_test.go:1241-1243`) reports no leaked goroutine. Reordering the wrap above `:300` drops a closer and goleak fails. `[deferred → C3]` *(Note: the stale comment at `dispatcher_auth_test.go:29-30` predates goleak's introduction and is corrected in this commit.)*
- **A8.4** Six security clauses, each with its own test and each named by one bullet of Step 4's contract: (1) `../` and encoded traversal are confined lexically by `path.Clean("/"+rest)` **(contract bullet 3)**; (2) only `dist` and `public` pass the first-segment allowlist **(bullet 2)**; (3) a directory path returns 404, never a listing **(bullet 6)**; (4) a non-GET/HEAD method returns 405 with an `Allow` header **(bullet 7)**; (5) a symlink escaping the root is refused — the named mechanism is `os.OpenRoot(dir)` at construction and `root.Open(rel)` per request, whose error the handler answers with 404 **(bullet 4)**; (6) a path with a dot-prefixed segment is not served **(bullet 5)**. `[deferred → C3]` *(R9–R12 record the failure each clause prevents. Clause 5 previously named no mechanism, so it was untestable as written — Architect M1; `os.Root` is available on this tree's go1.27.1.)*
- **A8.5** utuber's private static handler is **gone**: `grep -rn 'type staticHandler' internal/ --include=*.go` → rc=1, `grep -rn 'staticHandler' internal/utuber/` → rc=1, and `internal/utuber/build.go:58` routes through `static.NewHandler(cfg.StaticDir)`. `[deferred → C3]` *(FR-8.2, R14, §8 row 16. Step 4's utuber paragraph and its two guarded greps already cited A8.5 for exactly this; the criterion now says what they assert — Critic amendment D.)*
- **A8.6** The response-header contract, all three clauses in one criterion because one `http.ServeContent` call decides all three: `Last-Modified` is set on GET **and** HEAD; a matching `If-Modified-Since` returns 304 with no body; and `.mjs` is served with a `Content-Type` starting `text/javascript`. `[deferred → C3]` *(R10, R11. The `.mjs` clause is folded in here rather than carried separately: `mime.TypeByExtension` consults the **system** table before Go's builtin one, so a host with a stale `mime.types` serves a type a browser refuses for a module script, and that must fail in a test run — Critic amendment D.)*
- **A8.7** `MountShared` handed something other than an `*http.ServeMux` returns an **error at boot**, not a panic and not a silently-wrong exact-match pattern. `[deferred → C3]` *(R8.)*
- **A8.8** `go vet ./...` exits 0 and `go test -race ./...` (via `make test`) exits 0. **Executed:** vet rc=0; `make test` rc=0 with every package `ok`.

### A9 — ADR-001: bundle shape

**The input set these three criteria range over is a descriptor field, not a guess.** `scripts/gates/bundle-shape.mjs` inspects exactly the outputs of descriptors declaring **`sharedConsumer: true`** (Step 3, driver rule 10) — the same field that selects the `@shared` `onResolve` plugin and triggers the driver's `format: "esm"` assertion, so the gate, the plugin and the assertion cannot range over different sets. **That set is empty until C5** (`sampler` at C5, `taskmaster` at C6; the `shared` descriptor is the barrel, not a consumer of it), which is why A9.1, A9.2 and A9.4 first run at **C5** and not at C4 — *Critic amendment B: v5 listed A9.2 and A9.4 in §4's C4 row, where they would have passed over zero files and been recorded as green.*

- **A9.1** Every bundle whose descriptor declares `sharedConsumer: true` contains a **top-level** `import … from "/shared/dist/shared.mjs"` statement. `[deferred → C5/C6]`
- **A9.2** No such bundle contains `__require(` or `Dynamic require of`. Both sides are required, because esbuild downgrades an ESM-only construct under `format: "iife"` **silently, with exit 0** — the positive check alone cannot see it, and the negative check alone passes an empty file. `[deferred → C5/C6]` *(G5.)*
- **A9.3** taskmaster's bundle contains `var FRONTEND_BUILD_TIME` **exactly once** — the define was applied, and applied once. `[deferred → C6]`
- **A9.4** Seeding `format: "iife"` on a `sharedConsumer` descriptor makes `scripts/gates/bundle-shape.mjs` exit non-zero. `[deferred → C5]` *(R19: a gate never observed to fail is not known to be a gate. C5 is the first boundary at which there is a consumer to seed — at C4 the seed would have nothing to act on, so the mutation test could not distinguish a working gate from an empty one.)*
- **A9.5** `scripts/gates/token-overlap.mjs` reports the shared ∩ taskmaster token-name intersection as exactly `--font-mono`. `[deferred → C6]` *(The gate itself is live from **C1**, where it already computes exactly this intersection and passes — `tokens.css` declares `--font-mono` from C1 and `style.css:21` always has. C6 is where it becomes **load-bearing**, because C6 is the commit that puts both sheets on one page, and it is therefore the criterion's boundary of record — Critic note 5.)* *In place:* the intersection was computed by hand in Step 7 from `web/taskmaster/style.css:6-24`'s 17 declared properties against T1's 17 + T2's 27, and `--font-mono` is the only member; taskmaster wins it on source order at all five of its consumers.

### A10 — FR-10: the sampler, and adoption

- **A10.1** `GET /shared/dist/shared.css` on the sampler's host returns 200 with a `Last-Modified` header. `[deferred → C5]`
- **A10.2** `web/sampler/index.html` links `/shared/dist/shared.css` and its own `style.css`, and nothing else colour-bearing. `[deferred → C5]`
- **A10.3** The theme `<select>` is populated by iterating `THEMES` imported from `@shared` — **not** a second hard-coded list (FRD `:226-228`) — and selecting an option calls `setTheme`, which writes `document.documentElement.dataset.theme`; all 8 values cycle with no reload. `[deferred → C5]` *(This is the criterion that makes the barrel's theme half load-bearing: B1.)*
- **A10.4** The swatch grid renders T1's 17 colour tokens and T2's 27 structural tokens from a single in-page source, so a token added to `tokens.css` and not to the sampler is visible as a gap. `[deferred → C5]`
- **A10.5** All four dialogs open from the sampler, through `@shared`, with no taskmaster code in the page. `[deferred → C5]`
- **A10.6** `knownModules` has **14** entries, and the sampler has an `auth.modules` entry in both sample configs — gated like every other module, not special-cased. `[deferred → C5]`
- **A10.7** `web/taskmaster/index.html:2` carries `data-theme="obsidian"` (mechanism (a), ADR-007). `[deferred → C6]` *In place:* the propagation survey found 13 passages that the attribute mechanism affects, all enumerated in Step 1.0.
- **A10.8** All five steps of `docs/adding-a-module.md` (`:52`, `:62`, `:65`, `:70`, `:76`) are done for the sampler, and the document gains its shared-asset line. `[deferred → C5]`

### AX — cross-cutting

- **AX.1** At each commit boundary, `node scripts/gates/clean-tree.mjs <that commit's paths>` exits 0. Untracked files are ignored by design (P6's side-car artifacts and `fonts-staging/` must not fail a gate). `[deferred → C1]`
- **AX.2** Every `path:line` citation in this document resolves to the content it claims. **Executed:** every citation in v5 was verified by reading the cited line; §12 clause (b) records the sweep, and the corrections table in "Changes from v4" records the 9 citations that moved.
- **AX.3** Every artifact the build generates today is tracked by git. **Executed:** `git ls-files --error-unmatch` over all 12 → rc=0.
- **AX.4** After each commit, the artifact count equals `EXPECTED_ARTIFACT_COUNT`: 12 at C1, C1b, C2, C3; **14** at C4; **15** at C5 and C6. `[deferred → C1]`
- **AX.5** No gate uses a bare `! grep`. Every grep assertion uses the guarded form (`grep …; rc=$?; [ "$rc" -eq 1 ]`), because `! grep` treats exit 2 — a missing file, an unreadable directory, a bad pattern — as success. `-s` is not sufficient: it silences the message but still returns 2. `[deferred → C1]`
- **AX.6** Every gate in this plan has been observed to fail at least once, against a deliberately seeded violation. `[deferred → C6]` *(R19. This is the criterion v4 came closest to needing and did not have: B7's always-pass recipe would have been caught by it.)*
- **AX.7** `README.md` documents `make check` as the required pre-commit gate, since the repo has no CI and nothing else runs these checks — **and states that `gofmt -l .` and `go vet ./...` are mandated separately, by A7.14 and A8.8, rather than folded into `check`.** `[deferred → C3]` *(Architect M4 asked for one of the two directions, consistently: `check` keeps exactly the four targets A7.16 asserts (`web-verify test-web gates test`), the two Go hygiene commands stay their own step in every Verification block that runs them (Steps 4 and 6), and the README says so. Adding them to `check` was the alternative; it was not taken, because `make test` already runs the Go suite and A7.16's composition is asserted by name.)*

### v4 → v5 gate mapping

Numbering changed to **A*n* = FR-*n***. Every one of v4's 46 criteria has a v5 home; nothing was dropped in the renumbering.

| v4 | v5 | Note |
|---|---|---|
| A1.1–A1.9 | A1.1–A1.9 | all nine survive, re-ordered to match `check-shared-css.mjs`'s clause order (Step 1.7) so gate and criterion cannot drift; A1.6 regains v3's `components.css` scoping (v4's unscoped literal ban forbade the token definitions themselves); **A1.10**, **A1.11** are new |
| A2.1–A2.6 | A2.1–A2.6 | unchanged except A2.1's authored-cell exemption and A2.2's roster fix (obsidian, not dark) |
| A5.1–A5.4 *(fonts)* | A6.1–A6.4 | renumbered to FR-6; **A6.5** (woff2 externalization policy) is new |
| A6.1–A6.5 *(build driver)* | A7.1–A7.7 | merged into one FR-7 family; **A7.3** and **A7.7** are new and answer Critic M-6 |
| A7.1–A7.7 *(typecheck/hygiene)* | A7.8–A7.16 | v4 A7.4's "`dependencies` is empty" was **false** (5 runtime deps) and is restated as "unchanged"; **A7.13**, **A7.15** are new |
| A8.1–A8.4 *(Go)* | A8.1–A8.6 | A8.3 and A8.4 regain v3's closer invariant and six security clauses; **A8.7**, **A8.8** are new |
| A10.1–A10.5 | A10.1–A10.5 + **A10.7** | v4's deferred `data-theme` gate is now A10.7; **A10.6**, **A10.8** are new |
| AX.1–AX.6 | AX.1–AX.4 + **A7.17** | v4's byte-identity recipe becomes A7.17 (gate files, per Architect B7); **AX.5–AX.7** are new |
| — | **A5.5–A5.7** | restored/new behavioural gates on the lift; A5.5 is the only behavioural gate on C6 |
| — | **A9.1–A9.5** | **restored from v3** — the bundle-shape and token-overlap family v4 deleted wholesale |

---
## §6 — Risks

Each row names the mechanism that catches it, not an intention. A risk whose mitigation is "be careful" is not mitigated.

| | Risk | Mitigation |
|---|---|---|
| **R1** | A shared token name collides with a module's own token of the same name, silently changing that module's rendering. **Live today:** `--font-mono` is declared by both `tokens.css` and `web/taskmaster/style.css:21`. | `shared.css` is linked **first**, so the module wins on source order at all five consumers; `scripts/gates/token-overlap.mjs` (A9.5) pins the intersection at exactly `--font-mono` and fails if it grows. |
| **R2** | An absolute path on any esbuild input/output field rewrites the source-path comments esbuild embeds in every bundle (`web/taskmaster/js/bundle.js:3` is literally `// web/taskmaster/js/ui/modal.ts`), failing G2 on every artifact at once. **Highest-probability cause of a G2 false failure.** | Driver rule 3: no absolute path, no `absWorkingDir`, no `path.resolve`; the driver asserts `!path.isAbsolute(v)` on every descriptor path field before calling esbuild. |
| **R3** | esbuild **silently** downgrades an ESM-only construct under `format: "iife"`, emitting `__require(…)` and exiting **0**. The failure appears only as a "Dynamic require" error in a browser console. | A9.1/A9.2 are two-sided (positive import present **and** negative `__require(` absent); driver rule 10 asserts `format: "esm"` for every `@shared` consumer; A9.4 mutation-tests the gate. |
| **R4** | A gate that always passes. Make expands `$(…)` inside a recipe before the shell sees it, so `[ -z "$(git diff …)" ]` becomes `[ -z "" ]`; `$(shell …)` discards a child's exit status. Both were live in v4 (Architect B7, Critic M-6). | G11: every gate is a file invoked as one process. A7.17 asserts the form (no `$(shell …)`, no `$(git …)` in any recipe); AX.6 requires each gate to have been observed failing. |
| **R5** | The `data-theme` attribute is never stamped, so taskmaster's modal renders on `:root`'s canonical **`dark`** defaults — a generic dark palette, not obsidian's violet — and every obsidian-specific claim in Step 1.4 and T7 is unfounded. The risk is *sharper* for `:root` being dark than light: a light-on-dark flip would be noticed on sight, whereas dark-instead-of-obsidian is a small palette shift that a screenshot pass could bless (ADR-007). | A10.7 checks the attribute at `web/taskmaster/index.html:2`; Step 1.0 enumerates the 13 passages that depend on it, so the premise cannot rot unnoticed. |
| **R6** | esbuild rewrites `fonts.css`'s `url()`s into content-hashed copies under `web/shared/dist/`, inflating the artifact count, doubling committed bytes, and orphaning every line of `SHA256SUMS`. | A6.5: the `shared-css` descriptor sets `external: ["*.woff2"]` and the urls are server-absolute, so nothing is copied or renamed. |
| **R7** | The `/shared/` wrapper is placed **outside** `svc.Gate` (shared assets become unauthenticated) or **above** the `io.Closer` assertion at `cmd/server/main.go:300-302` (a module's closer is erased and its goroutine leaks). This was iteration-2's blocking defect. | The invariant is stated in Step 4 and asserted by A8.3: a certmachine-routed dispatcher has a non-empty `closers` slice, and goleak (`dispatcher_auth_test.go:1241-1243`) fails on a leak. ADR-002 records the rejected per-module alternative that reintroduces it. |
| **R8** | `MountShared` is handed a chi router or another `Muxer` whose `Handle` has different pattern semantics, registering a subtree as an exact match — a 404 that looks like a config error. | `MountShared` returns an `error` (never panics) and validates that it received an `*http.ServeMux`, so the failure surfaces at boot. A8.7. |
| **R9** | Source disclosure: `/shared/ts/modal.ts` or `/shared/css/tokens.css` becomes fetchable, publishing the phase's un-bundled source. | First-segment allowlist (`dist`, `public`) as **initial** behaviour, not deferred hardening. Every smoke URL in this plan names `/shared/dist/shared.css`; Step 6's two-sided probe asserts `/shared/ts/modal.ts` → 404. A8.4 clause 2. |
| **R10** | `.mjs` is served with a Content-Type a browser refuses for a module script, because `mime.TypeByExtension` consults the **system** mime table before Go's builtin one — so a host with a stale `mime.types` breaks the page while CI is green. | **A8.6**: a test asserts the response Content-Type starts with `text/javascript`. It fails in a test run rather than in someone's browser. *(Folded into A8.6 with the other response-header clauses per Critic amendment D, which frees A8.5 for the utuber de-duplication it was already cited for in Step 4.)* |
| **R11** | A future handler change silently drops conditional requests, turning every page load into a full re-download of `shared.css`. | A8.6 tests `Last-Modified` on GET and HEAD and `If-Modified-Since` → 304 explicitly, so the behaviour is pinned rather than incidental to whichever `net/http` helper serves the bytes (`http.ServeContent`, per Step 4's contract clause 4). |
| **R12** | Path traversal escapes the shared root and serves arbitrary files. | `path.Clean("/"+rest)` before joining, matching certmachine's `staticFileExists` discipline; tests drive `/shared/../../etc/passwd`, `/shared/dist/../../../etc/passwd`, and the `%2e%2e%2f` form. A8.4 clause 1. |
| **R13** | A **future** token added to `tokens.css` creates a new overlap with a module's vocabulary, and nobody notices because the current overlap was analysed once by hand. | `token-overlap.mjs` is a standing gate in `make gates`, not a one-time analysis: it fails the moment the intersection differs from `--font-mono`. A9.5. |
| **R14** | utuber's private copy of the static handler (`internal/utuber/build.go:68-79`) drifts from the platform one, so a security fix lands in one and not the other. | C3 deletes the copy and routes utuber through `static.NewHandler` — §8 row 16, FR-8.2, A8.5. |
| **R15** | A committed font binary has no provenance, or its license text is separated from it, so nobody can establish redistribution rights later. | G9: a tracked `SHA256SUMS` line plus `OFL.txt` beside each family; `shasum -c` is a gate (A6.1) verified to fail loud in both directions (mutated byte → `FAILED`; missing file → `FAILED open or read`). |
| **R16** | Reverting C4 while C6 is in place leaves taskmaster's shim importing a `@shared` that no longer exists — a broken build from a "safe" revert. | Revert order is **C6 then C4**, recorded in both commit messages and in G8. |
| **R17** | The `tm-modal-*` → `ui-modal-*` rename collides with taskmaster's own CSS, or a shared rule overrides a module rule it should not. | A5.3 (executed): zero `tm-modal` references in `style.css`, zero `.ui-` class references in taskmaster. Element-level selectors are (0,0,1) against `.ui-modal-*`'s (0,1,0), and `style.css:4`'s `[hidden] { display: none !important; }` always wins — guaranteed by A1.8's `!important` ban in shared CSS. |
| **R18** | The manual gates — the 8-theme checklist and C6's four-dialog pass — get skipped under time pressure, and the phase ships with its only behavioural evidence unexecuted. | `docs/sampler-checklist.md` is a **committed artifact** with per-theme checkboxes, and three follow-ups depend on it (§9 items 10, 11, and item 1's legibility note). A5.5 is named in §4's C6 row as the only behavioural gate on that commit. |
| **R19** | **A gate never observed to fail is not known to be a gate.** v4 shipped three always-passing gates and called them enforcement. | AX.6: every gate must have failed once against a seeded violation. Specific instances: A9.4 (seed `format: "iife"`), A1.3 (seed a duplicated key), A7.17 (seed a byte difference), A6.1 (mutate a font byte — already demonstrated). |
| **R20** | The in-flight `tools/baseline-shots/` side-car work (P6) gets swept into a Phase-1 commit, or its untracked screenshots fail a cleanliness gate. | Every commit in §4 names its paths, and P2's gate is `clean-tree.mjs <that commit's paths>` — path-scoped, and ignoring untracked files by design. |
| **R21** | obsidian's 3.99:1 `--color-primary-fg` is read as a regression this phase introduced, and someone "fixes" it inside Phase 1 by changing a token that T5a pins to the donor. | It is ledgered as an **inherited** condition (§8 row 5); T7 row 10 records the direction as 3.91:1 → **3.99:1** (an improvement, not a regression); §9 item 1 owns the real fix in the phase that owns taskmaster's visuals. |
| **R22** | The puma theme block ships without its font faces, puma silently resolves to `var(--font-body-fallback)`, and C6's screenshot pass blesses the wrong rendering as correct. | Sora and IBM Plex Mono ship in C1b precisely because the puma block ships in Phase 1 (FRD `:222-223`); A6.4 asserts the families resolve to registered faces rather than fallbacks. |

### §6.5 — Pre-mortem

*Three ways this phase could be reported "done" while being wrong. Each names what would have to change.*

1. **"Every gate is green" while three gates never ran.** This is not hypothetical — it is what v4 shipped: a byte-identity recipe that make reduced to `[ -z "" ]`, a count guard whose `$(shell)` swallowed a generator crash, and A10.x asserting properties of a page no route served. The tell is a gate that has never failed. **What changes it:** G11 makes every gate a process with a real exit code, AX.6 requires each to have been observed failing, and §4 places every gate at a boundary where its subject exists.
2. **The pixels move and nobody notices, because the oracle was wrong.** T7 is the oracle for C6. If T7 omits a delta — as v4 did for the `@font-face` registration that makes `JetBrains Mono` resolve at five non-modal surfaces (Architect M13) — then the screenshot pass either blesses an unsanctioned change or chases a sanctioned one as a bug. **What changes it:** T7 was re-derived cell by cell against `style.css:6-24` rather than copied from v3 or v4, and it includes the one delta that occurs regardless of the lift, plus two asserted-**zero** rows that must not move.
3. **Phase 1 quietly becomes Phase 3.** The tokens exist, so retokenizing one module's CSS looks like a five-minute win; then the diff is 400 lines across four modules, the screenshot oracle is gone, and the foundation is unlandable. **What changes it:** G1 and G3 name every touched path, §0's "not in scope" list is explicit, A1.10 keeps `components.css` incapable of matching a non-adopting page, and §8 row 14 records module CSS as an accepted deviation rather than an oversight.

---

## §7 — Open questions

Two questions remain open, plus one FRD correction. Six are closed by user decision (2026-09-15) and one by measurement. The companion document `docs/OPEN-QUESTIONS-ui-unification.md` is the tracked, clone-surviving copy; this section and that document must agree, and v5 reconciles them.

**Open**

- **Q3 — Do forest/ocean/ember/rose need a `--color-primary-fg` other than `#fff`?** The **values** are settled by measurement (Step 1.4): `#ffffff` on light, dark, and obsidian; `#0b0f14` on forest, ocean, ember, rose, and puma. What is deliberately left open is the *justification's* premise: obsidian keeping `#ffffff` at **3.99:1** is defensible only because obsidian is the theme taskmaster's modal actually renders in — and that becomes true only when **C6** stamps `data-theme="obsidian"` (FRD §7 decision 6, `:482-485`; ADR-007; gate **A10.7**). Until C6 lands, the justification is a promise, not a fact. *Needed by: C6.*
- **Q9 — Does Q8's unauthenticated allowlist extend to `/shared/dist/shared.mjs`?** The **`.woff2` half is settled: yes** — the user, asked directly, said "fonts and css do not require token protections", which covers `/shared/public/fonts/*.woff2` alongside `GET /shared/dist/shared.css`, so a login page styled with shared tokens also loads its faces. What remains open is **only** the `.mjs`: executable JavaScript is a different posture question, and the login page may not need the script at all. *Needed by: Phase 2, before the login page is touched.* (§9 item 6.)

**FRD correction (not a question — a defect in the FRD)**

- **FR-6's affected-module list is incomplete.** `docs/FRD-ui-unification.md:338-341` names only grocery, smbedit, and todo. The tree has **5 Google Fonts `<link>` groups across 4 modules**: `web/grocery/index.html:8-10`, `web/utuber/index.html:7-9`, `web/todo/index.html:8-10`, `web/todo/compare.html:8-10`, `web/smbedit/index.html:7-9`. **utuber** and **`web/todo/compare.html`** are missing, so an implementer working from FR-6 alone would leave two external requests in place and believe FR-6 complete. All five survive Phase 1 untouched by design (G3, G10, §8 row 11) and are removed by §9 item 4. *Needed by: whoever lands FR-6.*

**Closed**

- **Q1 — Does `light` get its own `--overlay-scrim`?** **Yes** (user). `rgba(40, 37, 29, 0.35)` — light's own `--color-text` at 0.35 alpha, so the scrim is the theme's ink rather than a foreign black. The alpha is a judgement, confirmed or adjusted by the sampler pass (§9 item 11). Mechanically it is the third entry in A1.2's three-key allowlist, which is why that gate allowlists rather than bans.
- **Q2 — Does `--color-surface-dynamic` join FR-1's vocabulary?** **Yes, but in Phase 3.** It exists only in obsidianoid's themes and has exactly one consumer, in the module Phase 1 does not migrate; an 18th canonical key now would mean authoring 7 donor-less values nothing reads. **`--radius-xl` is deferred on identical grounds** (grocery-only, one consumer). §8 rows 3 and 6; the FRD amendment is §9 item 5.
- **Q4 — When does FR-7.3's tsconfig `include` land, and in the same commit as the `js/` → `src/` renames?** **Coupled, same commit** (user) — decoupling silently drops 5 modules from typecheck. Phase 1 ships today's 9 enumerated globs plus `web/shared/**/*.ts` **and** `web/sampler/js/*.ts` = **11**. §8 rows 1 and 7; the FRD's literal two-glob form lands with the Phase-2 renames (§9 item 3).
- **Q5 — Should the artifact list be generated from the descriptors?** **Yes** (user). `scripts/list-artifacts.mjs` derives it from `scripts/descriptors.mjs`, and `EXPECTED_ARTIFACT_COUNT` is single-sourced there (A7.2, A7.3). v4's two mechanical findings — recursive `=` rather than `:=`, and a mandatory count guard because zero-argument `git ls-files --error-unmatch` lists the whole repo and exits 0 — are **retained as recorded evidence in ADR-001's consequences**, though the mechanism they describe is superseded: under G11 no make **recipe** uses `$(shell …)` or a `$(git …)` substitution. *(Scoped to recipes, per Architect M3: `Makefile:9`'s `BUILD_TIME := $(shell date -u …)` is a variable assignment feeding Go ldflags, not a gate, and it survives Phase 1 unchanged by design — §2, §8 row 8.)* The gate targets themselves land at **C1** (Step 1.8), not C2, because a gate that cannot be invoked at its own boundary is not a gate — Architect B2.
- **Q6 — Should `web/grocery/app.test.js` be wired into `test:web`?** **Yes, unconditionally, but not through the `esbuild --format=cjs | node -` pipeline:** it uses `node:test` and `import.meta.dirname` (`:16-17`), esbuild downgrades `import.meta` with a **warning and exit 0**, and node then throws. `scripts/test-web.mjs` carries a per-descriptor `runner` field — `"esbuild-cjs"` for the four existing suites, `"node-test"` for grocery via `spawnSync(process.execPath, ["--test", file])`. Warnings are promoted to failures in both drivers (driver rule 7), verified safe: the tree produces zero real esbuild warnings today.
- **Q7 — `<link>` or bundled CSS for the remaining 12 modules?** **Via `<link>`** (user). Two consequences, recorded rather than absorbed: `web/shared/dist/shared.css` stays a separate committed artifact, and **ADR-006's third reason** for keeping `@font-face` out of `tokens.css` (separate importability) **weakens** — its reasons 1 and 2 stand on their own and are sufficient. §9 item 2.
- **Q8 — `/shared/` sits inside `svc.Gate`, so the login page structurally cannot link `shared.css`.** **Direction (a) authorised** (user): a narrow unauthenticated allowlist for exactly `GET /shared/dist/shared.css`, beside `Gate`'s existing carve-outs (`/healthz` `:129`, `/api/auth/mode` `:135`, `/api/auth/whoami` `:148`, all evaluated before `protected := hasEntry || module == "admin"` at `:157`) — chosen over permanent inline styles in the login page. **Implementation is Phase 2; Phase 1 does not touch `gate.go`.** The scope beyond CSS is Q9. §9 item 7.

---

## §8 — Deviation ledger

Every place this plan knowingly differs from the FRD, from a donor, or from a reviewer's suggestion. A deviation that is not in this table is a defect.

| # | Subject | Deviation | Why, and when it resolves |
|---|---|---|---|
| 1 | FR-7.3 tsconfig `include` | Phase 1 ships **11** enumerated globs, not the FRD's literal `["web/*/src/**/*", "web/shared/**/*"]` | The literal form presumes the `js/` → `src/` renames, which are Phase 2. Coupling them (Q4) keeps `tsc` coverage constant. Resolves: §9 item 3. |
| 2 | FR-1 vocabulary | `--color-danger` is canonical; `--color-error` appears nowhere | One name per concept; the donors disagree. A1.11 enforces it. Resolves: never — this is the decision. |
| 3 | FR-1 vocabulary | `--color-surface-dynamic` is **not** among the 17 keys | Q2: one consumer, in a module Phase 1 does not migrate. Resolves: Phase 3, with obsidianoid's migration. |
| 4 | FR-1 vocabulary | `--color-primary-fg` is **authored by this phase** — it exists in no donor and in no FRD code block (`grep -rn -- '--color-primary-fg' web/` → rc=1) | The lifted primary button needs a foreground token, and hard-coding `#fff` in `components.css` would violate A1.6. Its Phase-1 consumer is real: `.ui-modal-btn-primary`. Resolves: never; A2.1 exempts it from donor matching. |
| 5 | FR-2 / WCAG AA | obsidian ships `--color-primary-fg: #ffffff` at **3.99:1**, below 4.5:1 | An **inherited** condition, not a new one: taskmaster ships 3.91:1 on that same surface today (`modal.ts:97-99`), so C6 improves it. Changing it would mean deviating from T5a's donor column inside a foundation commit. Resolves: §9 item 1, in the phase that owns taskmaster's visuals. |
| 6 | FR-1 vocabulary | `--radius-xl` is **not** among the 27 structural tokens | Q2, identical grounds to row 3: grocery-only, one consumer. Resolves: Phase 3. |
| 7 | FR-10 layout | The sampler lives at `web/sampler/js/`, not `web/sampler/src/`; and its sheet is `web/sampler/style.css`, not FRD `:405-406`'s `styles.css` (re-read: the sampler's shape is stated at `:405-406`, `web/sampler/src/main.ts` + `styles.css`; the Architect's M9 cited `:405-408`, which runs past the clause) | It matches every other module's present layout **and every other module's filename** — 12 of 13 modules ship `style.css` — so adopting the FRD's two spellings for one new module would make it the only exception until Phase 2 renames the rest. This is why the tsconfig glob is `web/sampler/js/*.ts`, and why `style.css` is the spelling used in Step 6's manifest, §4's C5 row, and A10.2 *(Architect M9: the FRD's `styles.css` is not adopted anywhere in this plan, so the ledger row is the only place the divergence is recorded)*. Resolves: §9 item 3. |
| 8 | FR-7 build stamp | The frontend stamp is a **content digest** over 15 files, not a wall-clock timestamp | Byte-identity (G2) is impossible against `$(date -u)`. ADR-004. The UI is unaffected: `main.ts:218`'s "Backend build" row and `:219`'s "Frontend build" row read through `buildinfo.ts:11,13`, so no module TS is edited. **And it is only the frontend stamp:** `Makefile:9`'s `BUILD_TIME := $(shell date -u …)`, which feeds the Go ldflags at `:10` and the "Backend build" row, survives Phase 1 unchanged by design — a variable assignment, not a gate, so G11's recipe-scoped ban does not reach it *(Architect M3)*. Resolves: never. |
| 9 | FR-1 donors | `web/obsidianoid/css/themes.css` is the **structural donor of record**; where grocery disagrees on a token both define (`--radius-sm`, `--shadow-sm`, `--text-*`), obsidianoid wins | Three documented exceptions: `--shadow-md` and `--overlay-scrim` (row 12) and `--space-5` (grocery, because obsidianoid lacks it and FRD `:171` names it). Grocery's differing values become Phase-3 migration deltas. |
| 10 | FR-1 scales | `--space-7` and `--text-xl` are **authored** — no donor declares them | FRD `:171-174` mandates the complete `--space-1..8` and `--text-xs..xl` scales. Marked ★ in T2. Resolves: never. |
| 11 | FR-6 | **5 Google Fonts `<link>` groups in 4 module HTML files survive Phase 1 untouched** | Removing them is an FR-6 edit to module HTML, which G3 forbids this phase from making. G10 is therefore scoped to "`web/shared/` references no host but the origin", not "the app makes no external request". Enumerated in §7. Resolves: §9 item 4. |
| 12 | FR-1 donors | `--shadow-md` and `--overlay-scrim` take `modal.ts`'s literals (`0 8px 32px rgba(0,0,0,0.4)`, `rgba(0,0,0,0.55)`), **not** obsidianoid's values | Deliberate: it is what makes T7's two asserted-**zero** rows true, so the lift reproduces the donor's scrim and shadow exactly. Resolves: never. |
| 13 | FR-1 donors | `--font-body-fallback` and `--font-mono-fallback` are **part-authored**: `system-ui` and `ui-monospace` are additions, not inheritance | obsidianoid `:32` is `'Inter', 'Segoe UI', sans-serif` and `:33` is `'JetBrains Mono', 'Fira Code', monospace`. Labelling them "the donor's stack minus the first family" (v4) was wrong — Architect M7. Resolves: never. |
| 14 | FR-1 | The colour-literal ban applies to `components.css` only; **module CSS keeps its literals** | Phase 1 does not migrate any module's CSS (G1, G3). An unscoped ban would also forbid `themes.css`, which is nothing but literals by definition. Resolves: Phase 3+. |
| 15 | FR-8.1 | `/shared/` is mounted **once at the dispatcher**, not per-module | ADR-002: per-module mounting reintroduces iteration-2's blocker (13 wrappers, each an opportunity to erase an `io.Closer`) and would need 13 edits for one route. `MountShared` exists for modules that later want the subtree inside their own middleware, and the sampler demonstrates it. |
| 16 | FR-8.2 | C3 **deletes** utuber's private static handler (`internal/utuber/build.go:68-79`, `:58` rewritten to `static.NewHandler`) | A duplicate handler is where a security fix lands in one copy and not the other (R14). This edits a module's **Go** file, which G3 permits — G3 covers module CSS, HTML, and TS. |
| 17 | FR-5 adoption | taskmaster adopts via a **2-line re-export shim**; the five call sites are not rewritten | ADR-003. It keeps C6 a two-file revert and keeps two prose comments (`api.ts:6`, `designer.ts:20`) true. Resolves: §9 item 13. |
| 18 | FR-6 | **15** of the 18 staged faces ship; Inter's 3 static weights are dropped | `InterVariable.woff2` + `InterVariable-Italic.woff2` cover 100–900, so the statics are dead bytes. SHA256SUMS is the staged file minus exactly those 3 lines. |
| 19 | FR-6 | woff2 files are `external` to esbuild and referenced by **server-absolute** urls | Keeps the artifact count at 15 rather than 15 + 15 hashed copies (R6). **Consequence:** `fonts.css` is correct only when the tree is served at `/shared/` — which D2 guarantees on all 13 hosts. A6.5. |
| 20 | G2 / G6 | **C1 and C1b carry a byte-identity exemption** for `web/taskmaster/js/bundle.js` | Until C2 lands the digest, `package.json:5-6`'s `$(date -u)` re-stamps that artifact on **any** rebuild, so the exemption is forced by a mechanism, not chosen. Bounded to two commits; retired at C2 and asserted gone by A7.4. v4 claimed no exceptions while shipping the mechanism that guarantees one. |
| 21 | FR-2 | Two per-theme **structural** overrides are allowlisted: puma's `--font-body`/`--font-mono` (FRD `:222-223`) and light's `--overlay-scrim` (Q1) | Hence A1.2 is a three-key allowlist rather than an absolute ban. A fourth override fails the gate. |
| 22 | FR-10 | The sampler is **auth-gated** like every other module, though it renders nothing private | Consistency is cheaper than an exception in `gate.go`; one `auth.modules` entry, no special-casing. (The unauthenticated carve-out for `shared.css` itself is Q8, Phase 2.) |

---
## §9 — Follow-ups (explicitly out of Phase 1)

Each item names the phase that owns it. An item with no owner is a wish, not a follow-up.

1. **obsidian's `--color-primary-fg` contrast.** 3.99:1 is an inherited condition (§8 row 5), improved from taskmaster's current 3.91:1 but still under AA. The fix — darkening `--color-primary` or moving to a dark foreground — belongs to the phase that owns taskmaster's visuals, because it changes the look of a shipped module. The legibility judgement is recorded per theme in `docs/sampler-checklist.md`. *Phase 2.*
2. **Adopt `shared.css` on the remaining 12 modules, via `<link>`** (Q7). Phase 1 proves the mechanism on exactly one surface; this is the rollout. *Phase 2.*
3. **`js/` → `src/` entry renames, and FR-7.3's literal tsconfig `include`.** Coupled into one commit per Q4, which is what keeps `tsc --noEmit` coverage constant. Note the layouts this must reconcile: `web/obsidianoid/js/`, `web/taskmaster/js/`, `web/sampler/js/` (§8 row 7) on one side; `web/smbedit/src/`, `web/issuetracker/src/` already on the other. *Phase 2.*
4. **Remove the 5 Google Fonts `<link>` groups** in `web/grocery/index.html:8-10`, `web/utuber/index.html:7-9`, `web/todo/index.html:8-10`, `web/todo/compare.html:8-10`, `web/smbedit/index.html:7-9`, and amend FRD `:338-341`, which names only three of the four modules (§7). *Phase 2, with item 2.*
5. **Amend FRD FR-1's vocabulary when each deferred key lands** — `--color-surface-dynamic` and `--radius-xl` (Q2, §8 rows 3 and 6) — so the plan and the FRD do not diverge silently. *Phase 3.*
6. **Settle Q9's `.mjs` half** before the login page links anything executable. *Phase 2.*
7. **Implement Q8's unauthenticated carve-out** for `GET /shared/dist/shared.css` in `internal/platform/auth/gate.go`, beside the existing `/healthz`, `/api/auth/mode`, and `/api/auth/whoami` carve-outs. Phase 1 does not touch `gate.go`. *Phase 2.*
8. **Trim Sora's weights.** `fonts-staging/NOTES.md:27-28` says, verbatim: *"**Sora** — 400/500/600/700/800 (puma theme headings; trim once the theme CSS pins its weights)."* Phase 1 ships all five because the puma block does not pin weights yet; trimming is mechanical once it does (delete faces, delete `SHA256SUMS` lines, re-run A6.1). The sentence is quoted rather than cited because `NOTES.md` is untracked and a reviewer cannot open it. *Phase 2 or 3, whichever pins puma's weights.*
9. **Introduce CI.** There is none today, which is why `make check` exists and why AX.7 makes the README say so. Every gate in §5 is a command, so wiring them into a runner is configuration, not redesign. *Phase 2.*
10. **Reconcile forest/ocean/ember/rose surface-to-border contrast.** Those donors use a tighter surface/border gap than obsidian (e.g. `--color-border` `#30363d` against their surfaces), which is legible but flatter than obsidian's. Confirm or adjust from the sampler pass rather than by eye in a diff. *Phase 2, via `docs/sampler-checklist.md`.*
11. **Confirm light's `--overlay-scrim` alpha.** `rgba(40, 37, 29, 0.35)` is a judgement, not a derivation (Q1). The sampler's modal section over a light page is the test. *Phase 2, via `docs/sampler-checklist.md`.*
12. **De-duplicate obsidianoid's theme list.** `web/shared/ts/theme.ts`'s `THEMES` array plus `themes.css` makes "add a theme" a two-edit operation; obsidianoid currently carries its own CSS and TS theme lists, which this can replace. *Phase 3.*
13. **Rewrite taskmaster's five call sites off the shim** (`api.ts:10`, `designer.ts:23`, `outputmodal.ts:13`, `board.ts:27`, `main.ts:16`) to import `@shared/modal` directly, and delete `web/taskmaster/js/ui/modal.ts`. Also update the two prose comments at `api.ts:6` and `designer.ts:20`, which are true through the shim and would become stale. *Phase 2 — deliberately not Phase 1 (ADR-003, §8 row 17).*

---

## §10 — Decision records

### ADR-001 — `@shared` is externalized at build time, and every consumer bundle is ESM

**Decision.** `web/shared/ts/` builds once to `web/shared/dist/shared.mjs`. Consumer bundles do **not** inline it: an esbuild `onResolve` plugin rewrites the specifier to the served URL and marks it external —

```js
build.onResolve({ filter: /^@shared(\/.*)?$/ }, () => ({ path: "/shared/dist/shared.mjs", external: true }));
```

— and every descriptor that consumes `@shared` sets `format: "esm"`.

**Drivers.** (1) FRD §7.1 requires one shared bundle, not one copy per module — inlining would ship 13 copies and defeat the route entirely. (2) A bare-specifier external is meaningless to a browser; the import must be a URL the server answers, which is exactly what D2 provides. (3) The build driver must run on a host with no network and no extra dependency (G4), so the rewrite has to be a local plugin rather than an import map or a bundler feature.

**Alternatives rejected.**
- *Inline `@shared` into each consumer.* Simplest to build; defeats FR-8's purpose, makes every artifact churn whenever shared TS changes, and makes 13 artifacts re-stamp on a one-line shared edit.
- *An import map in each module's HTML.* Needs an HTML edit per module — forbidden this phase by G3 — and leaves the bundler unable to see the dependency at all.
- *`external: ["@shared", "@shared/*"]` without the resolve rewrite.* esbuild emits the bare specifier verbatim; the browser then throws on a non-URL module specifier. This is the failure mode the `onResolve` form exists to prevent.

**Consequences.** `format: "esm"` becomes mandatory for `@shared` consumers, which is why taskmaster's descriptor flips from iife at C6. esbuild does **not** error on an ESM-only construct under iife — it silently downgrades and emits `__require(…)` with exit **0** — so A9.1/A9.2 must be two-sided and A9.4 mutation-tests them (R3, R19). Recorded evidence from v4, retained because it documents make's behaviour even though G11 supersedes the mechanism: a `$(shell …)` artifact list must be assigned with recursive `=`, never `:=`, or it runs at parse time and breaks `make build` on a node-free host; and a zero-argument `git ls-files --error-unmatch` lists the whole repo and **exits 0**, so a count guard is mandatory. Under G11 **no make recipe** uses `$(shell …)` or a `$(git …)` substitution, so both hazards are structurally absent from the gate path. *(Architect M3: the claim is scoped to recipes, not to the file. `Makefile:9`'s `BUILD_TIME := $(shell date -u …)` is a variable assignment feeding Go ldflags at `:10` — the backend stamp, a different stamp on a different UI row (§2), untouched by Phase 1 by design. D4 changes only the frontend one.)*

**Follow-ups.** Item 2 (the other 12 modules), item 13 (call sites off the shim).

### ADR-002 — `/shared/` is mounted once at the dispatcher, not per module

**Decision.** `static.WithShared` wraps the per-module handler in `cmd/server/main.go:303`, inside `svc.Gate` and after the `io.Closer` assertion at `:300-302`. `MountShared(m Muxer, dir string) error` also ships, for modules that later want the subtree inside their own middleware; the sampler uses it as the documented example.

**Drivers.** (1) One route must be reachable on all 13 module hosts (FRD FR-8) — the dispatcher is the only place that sees all 13. (2) Iteration-2's blocking defect was a wrapper that erased a module's `io.Closer`; one wrapper in one place is one opportunity for that mistake, not 13. (3) Adding a 14th module must not require remembering a mount step (`docs/adding-a-module.md`).

**Alternatives rejected.**
- *Per-module mount in each `build.go`.* 13 edits for one route, 13 chances to drop a closer, and a 14th module silently missing `/shared/`.
- *A separate `shared.` vhost.* A new host entry, a new TLS name, and a cross-origin font fetch — for assets that must be same-origin to stay inside `svc.Gate`.
- *Serving from each module's existing static handler.* Would need `StaticDir` to contain the shared tree, i.e. a copy per module — the problem FR-8 exists to remove.

**Consequences.** `/shared/` is authenticated everywhere, which is what forces Q8's separate carve-out for the login page. **Router-agnosticism is a property of the dispatcher path, and only of it** *(Architect M5: this passage previously claimed both halves of a contradiction)*: `WithShared(next http.Handler, dir string) http.Handler` wraps an `http.Handler`, so it composes with whatever router any host uses and needs to know nothing about it — which is why one wrapper covers all 13 hosts. `MountShared` is deliberately **not** router-agnostic. It takes a `Muxer` so the behaviour is testable against a fake, but it validates that it actually received an `*http.ServeMux` and returns an error otherwise, because `Handle`'s subtree-versus-exact-match semantics are ServeMux's and a chi router given `"/shared/"` would register an exact match — a 404 that reads as a config error (R8, A8.7). The interface is a seam, not a promise of portability. *(v4 carried a separate one-line ADR about a chi shim; it is moot — `WithShared` needs no shim and `MountShared` refuses the case at boot — and is deleted rather than renumbered.)* utuber's duplicate static handler becomes indefensible once the platform one is the single path, and C3 deletes it (§8 row 16).

**Follow-ups.** Item 7 (Q8's carve-out in `gate.go`).

### ADR-003 — taskmaster adopts the shared modal through a re-export shim

**Decision.** `web/taskmaster/js/ui/modal.ts` becomes two lines that re-export the four functions and four types from `@shared/modal`. The five importing call sites are not touched.

**Drivers.** (1) C6 must stay a small, revertible diff — it is the only commit that moves a pixel. (2) Two prose comments (`api.ts:6`, `designer.ts:20`) name `ui/modal.ts` by path and stay true through the shim. (3) Phase 1's job is to prove the mechanism, not to finish the migration.

**Alternatives costed.**
- *Rewrite all five call sites to `@shared/modal` and delete the file.* Five more edited files in the one commit that changes rendering, two prose comments to update, and a larger revert — for no Phase-1 benefit. Deferred to item 13.
- *Keep the donor and add a second implementation in `web/shared/`.* Two modals, guaranteed to diverge; the lift would never actually happen.
- *`export * from "@shared/modal";`* One line shorter, and it re-exports whatever the barrel gains later, including symbols taskmaster should not see. `check-shared-barrel.mjs` bans `export *` in the barrel for the same reason.

**Consequences.** `grep -rn 'ui/modal' web/taskmaster/` matches the two prose comments and would read as a false failure, so no gate greps for it; A5.4 asserts the five call sites are unmodified instead. The shim's existence is a ledgered deviation (§8 row 17), not an oversight.

**Where the narrow surface is enforced, stated because B1 moved it.** The barrel `web/shared/ts/index.ts` is the shared library's **complete** public surface — six named values and four types (Step 5, A5.2's allowlist) — because a consumer that cannot reach `THEMES` through `@shared` has no legal way to reach it at all. Taskmaster's narrow surface is therefore enforced **at the shim**, not at the barrel: the shim names exactly the four dialog functions and four types, so nothing else the barrel exports becomes reachable from taskmaster's `./ui/modal.js` specifier, and A5.4 asserts that list. The barrel growing a symbol is a library change; the shim growing one would be a taskmaster change, and it is the shim that this ADR keeps small.

**Follow-ups.** Item 13.

### ADR-004 — the frontend build stamp is a content digest

**Decision.** `__TM_BUILD_TIME__` is defined as a 12-character digest over the union of taskmaster's esbuild **metafile inputs** and an explicit `EXTRA` set (`web/taskmaster/index.html`, `web/taskmaster/style.css`) — 15 files, digest `c2c572876987` at this baseline — computed in a two-pass build: pass 1 produces the metafile, pass 2 rebuilds with the define.

**Drivers.** (1) G2 requires a rebuild with no source change to produce byte-identical artifacts; `$(date -u)` makes that impossible by construction. (2) The stamp must still tell a human *which* code is deployed, so it has to be derived from content, not pinned to a constant. (3) The input set must be derived, not hand-listed, or it goes stale the first time a file is added.

**Alternatives rejected.**
- *Pin to `1970-01-01T00:00:00Z`.* Byte-identical and useless — the UI row would lie in every deployment.
- *Use `git rev-parse HEAD`.* Unavailable from a tarball export, and wrong in the common case: it changes on commits that do not touch taskmaster, and does **not** change on uncommitted edits.
- *Hand-list the input files.* Goes stale silently; a new module file would simply not be covered.

**Consequences.** Two esbuild passes per stamped module (measurably ~200 ms here). **The `EXTRA` set's cost, stated rather than discovered:** `web/taskmaster/style.css` is in the digest input set, so a CSS-only edit re-stamps the id and therefore rewrites `bundle.js`. **That is desired** — the stamp answers "which frontend is this?", and a style-only change is a different frontend — but it means a CSS-only commit legitimately shows a bundle diff, and a reviewer must not read that as churn. The digest is not a git identity and must not be presented as one.

**Follow-ups.** None. This is complete in Phase 1.

### ADR-005 — the shared modal's test is DOM-level and narrow

**Decision.** `web/shared/ts/modal.test.ts` ships as the fifth suite in `npm run test:web`, exercising the pure and DOM-observable behaviour that can be asserted without a browser engine: the focus-order computation, the `[hidden]` toggling, the escape/backdrop handler wiring, and the promise resolution of `confirmDialog`/`promptDialog`. Full interaction fidelity — real focus movement, real key events, real paint — stays manual, in `docs/sampler-checklist.md` and A5.5.

**Drivers.** (1) `package.json` declares **no `@types/node`** (verified: `grep -c '@types/node' package.json` → 0, exit 1), and G4 forbids adding one, so the suite must be DOM-typed and dependency-free. (2) The existing runner is `esbuild --bundle --platform=node --format=cjs | node -`, which has no DOM. (3) A jsdom or Playwright dependency would be the largest new dependency in the repo, added in a foundation phase, to test 390 lines.

**The live question this ADR leaves open.** A DOM-free suite cannot assert that Tab actually moves focus — only that the handler computes the right next element. That is a real coverage gap, and it is why A5.5 is mandatory rather than nice-to-have and why the checklist is a committed artifact (R18). If Phase 2 adds a browser-driven runner for other reasons, this suite should grow into it; that is a deliberate open question, not an answered one.

**Alternatives rejected.** *jsdom* (a dependency, and its focus model is not the browser's, so passing tests would overstate confidence); *Playwright* (correct fidelity, wrong phase — it needs CI, which item 9 owns); *no test at all* (the lift would then have zero automated evidence).

**Follow-ups.** Item 9 (CI), and revisiting this suite when a browser runner exists.

### ADR-006 — `@font-face` lives in `fonts.css`, not `tokens.css`

**Decision.** Font registration is its own file, `@import`-ed by `index.css`, and lands in its own commit (C1b).

**Drivers.** (1) A consumer can take the token layer without downloading 15 font faces. (2) The font layer changes on a different cadence than the token vocabulary. (3) Separate importability for per-module adoption — **this third reason weakened when Q7 closed on `<link>`**, because every module now takes the same combined sheet; reasons 1 and 2 stand on their own and are sufficient.

**Alternatives rejected.** *`@font-face` in `tokens.css`* (couples the vocabulary to 15 binaries); *inline `@font-face` per module* (13 copies of the same registration).

**Consequences.** C1b is independently revertible, and if it has to defer it defers **past** C4 and C6 rather than blocking them (§4). `fonts-staging/` stays untracked, which is why §9 item 8 quotes `NOTES.md` rather than citing it.

**Follow-ups.** Item 8 (weight trimming).

### ADR-007 — theme selection is a `data-theme` attribute on `<html>`

**Decision.** A theme is selected by stamping `data-theme="<name>"` on the root element; `:root` **is** `html`. C6 stamps `data-theme="obsidian"` on `web/taskmaster/index.html:2` (mechanism (a)), implementing FRD §7 decision 6 (`:482-485`).

**The unstamped default is `dark`, stated here once.** `themes.css`'s first depth-1 block is the compound selector `:root, [data-theme="dark"]`, so canonical **dark** is both the named theme and the default an unstamped page inherits; the other **seven** themes are `[data-theme="…"]` blocks that override it. Three reasons, in the order they decide it: (1) the donor structure — `web/obsidianoid/css/themes.css:1-2` is literally `/* ─── Design Tokens: dark (default) ─── */` followed by `:root, [data-theme="dark"] {`, and Step 1.3's remap preserves that block's *form* while FRD FR-2 (`:190-191`) renames its *palette* to `obsidian`, with canonical `dark`'s values coming from `web/todo/css/todo.css:3-17`; (2) FRD FR-2's roster is 7 dark themes and one light one, so a light default would make the exception the default; (3) Phase-2 adoption (§9 item 2) links `shared.css` onto 12 module pages one at a time, and every one of them renders dark today (taskmaster `#1e1e1e`, obsidianoid `#13131a`, todo's dark-first block) — a dark `:root` is therefore the value that makes an unstamped page's *unadopted* rendering closest to its current one, and A1.10's inertness argument is about selectors, not about which palette the tokens carry.

**Arithmetic and rosters are unchanged by this, and that is checkable.** The block count is **8** either way — the compound `:root, [data-theme="dark"]` is one depth-1 block, not two — so A1.3's 8-string roster (`check-shared-css.mjs` clause 4) and its running total of 8 × 17 = **136** `--color-*` declarations both hold verbatim, and A2.2's five-name obsidianoid roster is untouched. The direction is therefore a prose fact only: v5's gate specification always encoded `dark`, and three prose passages (R5, and this ADR's decision and consequences) said `light`. Those three are corrected, not the mechanism.

**Drivers.** (1) It must work with **zero** JavaScript, before first paint, with no flash of the wrong theme. (2) It must be one attribute per page, so adoption is a one-line HTML edit (item 2). (3) Tokens must cascade to every element including `<body>`'s own background.

**Alternatives rejected.**
- *A wrapper element with a class (`<div class="theme-obsidian">`).* Tokens declared on a wrapper do not reach `html` or `body`, so the page background stays untokenized — a cascade-origin problem, not a styling preference.
- *A JS call at boot (`setTheme()`).* Guarantees a flash of the default theme on every load and makes the theme unavailable to CSS before hydration.
- *A per-module stylesheet build.* 13 builds, 13 artifacts, and no runtime switching — which the sampler requires.

**Consequences.** Without the attribute, taskmaster renders on the canonical **`dark`** defaults `:root` carries — a generic dark palette, not obsidian's violet — and every obsidian claim in Step 1.4 and T7 is unfounded. The failure is quieter than a light-on-dark flip, which is precisely why it needs a gate: A10.7 checks the attribute, and R5 names the failure. `web/shared/ts/theme.ts` exposes `THEMES` and `setTheme`, **both re-exported by the barrel** so a consumer reaches them through `@shared` (B1, A5.2's allowlist), making "add a theme" a two-edit operation (one CSS block, one `THEMES` entry) and giving item 12 something to de-duplicate obsidianoid against.

**Follow-ups.** Item 12.

---

## §11 — RALPLAN-DR

**Mode: SHORT** — with the pre-mortem (§6.5) and the expanded test plan (§5's nine gate families) present anyway, because this is the final iteration of the consensus loop and both reviewers' remaining blockers were about *missing* verification, not excess.

**Principles**

1. **One definition per fact.** A key, artifact, file set, or count is defined in exactly one place. **The operational test before deleting an occurrence:** confirm another occurrence still defines the *same* fact. v4 deleted occurrences that were the only definition of theirs; that is how half the plan vanished without a ledger row.
2. **A gate is a command that was run.** Anything else carries `[deferred]`, names the boundary where it first runs, and states what was verified in its place. The split is the §5 census — stated there once, by number, and referred to by name everywhere else, this principle included.
3. **Every pixel that moves is sanctioned in advance.** T7 is the complete set for C6, including two rows asserted to be **zero**. A delta not in T7 is a defect, and a T7 row that does not appear is equally a defect.
4. **Scope is fixed by the FRD, not by convenience.** FRD `:457` names six Phase-1 deliverables; all six are here. Shrinking the deliverable set was available and was not taken.
5. **A deviation is ledgered or it is a defect.** §8 has 22 rows; each names why and when it resolves.

**Decision drivers (top 3)**

1. **Reversibility over elegance.** Seven commits, each independently revertible, pixels last, with one stated revert-order constraint (C6 before C4).
2. **Verifiability over completeness.** A gate that cannot run at its own commit boundary is worth less than a smaller gate that can — which is why the sampler's Go half is restored and why the ordering in §4 is derived rather than assumed.
3. **Inheritance is not regression.** obsidian's 3.99:1 and the five surviving Google Fonts `<link>`s are ledgered as inherited, with owners, rather than fixed inside a foundation phase that has no visual oracle for them.

**Options weighed for this revision**

| | Option | Verdict |
|---|---|---|
| **A** | **Restore the platform half from v3; keep v4's data layer verbatim.** | **Chosen.** Both reviewers independently recommended it. v4's data layer was verified exact (Critic 36/36, Architect 8/8 on the iteration-3 findings), and the 845 → 648 compression is what deleted FR-5, FR-8, the sampler's server half, and the bundle-shape family. Restoring is additive: no verified fact is disturbed. |
| **B** | Shrink Phase 1's scope to what v4 actually specified, and ledger the dropped deliverables. | **Rejected — not available.** FRD `:457` fixes the six deliverables and the loop's charter treats §7 as settled input. Dropping FR-5 and FR-8 would also strand the gates that depend on them (A5.x, A8.x, A9.x) and leave `shared.css` with no Phase-1 consumer, which is the circularity Critic M-1 identified. |
| **C** | Re-derive the whole plan from the FRD, discarding both v3 and v4. | **Rejected.** It would discard v4's contrast matrix, donor tables, fonts arithmetic, and the ADR-004 digest — all independently verified this iteration — and reintroduce the risk of new arithmetic errors at the last iteration, with no reviewer left to catch them. |

---

## §12 — Self-audit

Run against the finished document before this revision was submitted, as five explicit clauses. The results are part of the change log (see "Changes from v4", §4).

**Re-run after the iteration-5 amendments** (the close-out subsection of the change log lists them). Results: **(a)** pass, two rows re-worded, no row unassigned; **(b)** pass, **57** entries (was 55), 46 distinct / 16 existing / 30 created all unchanged — the arithmetic is in the change log's clause-(b) row; **(c)** pass, the census is still **74 = 8 executed + 66 deferred**, unchanged, because amendment D rewrote A8.5's content instead of adding a bullet and B1 extended A5.2 instead of adding a criterion; **(d)** pass, with three sets added and one exception still stated; **(e)** pass, both greps return 0 over §0–§11 (rc=1) and each string still appears exactly three times in the whole file. Cross-reference sweep re-run over the amended file: **74** criteria defined, every `A*`/`AX*` reference resolves to a definition, **11** guardrails, **22** risks, **6** preconditions, **7** ADRs, no dangling identifier, and every `[deferred → Cn]` label agrees with the commit whose Gates column names it.

**(a) Each of the six deliverables derived from FRD `:457` maps to named plan sections.** (§0 shows the derivation from the line's five clauses; no clause is unassigned.) The table has **seven** rows: the six FRD deliverables plus **D7**, the self-hosted fonts folded forward by user decision, which is marked as the addition it is rather than smuggled into the six *(Architect M6's second half — the row count now reads as intended)*.

| Deliverable | Plan sections that execute it |
|---|---|
| Shared CSS foundation (tokens, themes, components, index) | §0 D1; Step 1 (1.0–1.8); §4 C1; A1.x, A2.x — **and its emitted artifact:** the `shared-css` descriptor and `web/shared/dist/shared.css` are Step 5; §4 C4; A6.5 *(Architect M6: the row previously stopped at the authored sources and omitted where the served sheet is produced)* |
| `/shared/` served on all 13 module hosts, inside auth (FR-8) | §0 D2; Step 4; §4 C3; A8.x; ADR-002 |
| Descriptor-driven build and test drivers (FR-7) | §0 D3; Steps 1.8 and 3; §4 C1, C2; A7.1–A7.17 *(Critic note 2: A7.8–A7.10, the tsconfig half, were omitted from this row)*; ADR-001 |
| Reproducible build stamp | §0 D4; Step 3 (rule 4); §4 C2; A7.4, A7.6; ADR-004 |
| Shared modal lift + all 8 themes as data + the sampler (FR-5, FR-10) | §0 D5; Steps 1.6, 5, 6; §4 C4, C5; A5.1–A5.2, A5.6, A10.x; ADR-003, ADR-005 |
| Adoption on exactly one surface (FR-5 / FR-10 proof) | §0 D6; Step 7; §4 C6; A5.3–A5.5, A5.7, A9.x, A10.7; T7 |
| *(+ D7)* Self-hosted fonts, folded forward by user decision | §0 D7; Step 2; §4 C1b; A1.9, A6.x; ADR-006 |

**(b) Every path in every commit manifest exists in the tree, or is created by an earlier commit.** Checked row by row across §4's seven commits: 57 entries, 46 distinct; **16** exist in the tree today and **30** are created at or before the boundary that names them. Four create-then-modify trails: `components.css` at C1 then modified at C4; `index.css` at C1 then C1b; `scripts/descriptors.mjs` at C1 then C4/C5/C6; `Makefile` (existing) at C1 (the two gate targets), C2 (`web:`/`typecheck:`/`test-web`/`check`), C4 (one line added to `gates`). Two ellipses (`internal/platform/…`) were replaced with the real three-path list; the phantom `cmd/unified-webapp/main.go` was corrected to `cmd/server/main.go` at all 9 sites. Every cited line number was re-read: `Makefile:42` → **`:43`** for the `npm install` line, `main.ts:217/:218` → **`:218/:219`**, FRD `:210` → **`:211`**, FRD decision 6 `:483-485` → **`:482-485`**, obsidianoid's T2 citations re-derived from the file, goleak at **`:1241-1243`** (with `dispatcher_auth_test.go:29-30` flagged stale), `main.go:302` → **`:303`**, `dispatcher_auth_test.go:97-102` → **`:100`**.

**(c) Every gate is a command that was run, or carries `[deferred]` plus substitute evidence.** No criterion is unlabelled. The split is the §5 census; each deferred criterion names the boundary where it first runs and what was verified in its place.

**(d) Every list, set, and count is defined exactly once.** T1 (17 colour keys), T2 (27 structural tokens), the 136-cell matrix, T5a/b/c, the 8 theme names, the 15 fonts, the 11 tsconfig globs, `knownModules` (13 today, 14 after C5 — one definition, in Step 6, asserted by A10.6), the 5 call sites, the 8 renamed classes, the §5 census, the `.PHONY` roster, and the 7-commit sequence each have one definition. **Three sets added by the iteration-5 amendments, each likewise defined once:** the **barrel allowlist** (6 named values + 4 types) in Step 5's table, read by A5.2 and A5.7 and deliberately *not* restated at A5.4, which asserts the shim's narrower four; the **handler contract** (9 clauses) in Step 4's table, which is also the single place the clause → criterion mapping lives, so A8.1's "nine clauses" and A8.4's six are one statement with a cross-reference rather than two rosters; and `bundle-shape.mjs`'s **input set**, which is not written down as a list at all but *derived* from the `sharedConsumer: true` descriptor field — the strongest form of one-definition, since the gate and the plugin read the same field. **One stated exception:** the artifact count legitimately moves during the sequence (12 → 14 → 15), so §4 states the trajectory while `EXPECTED_ARTIFACT_COUNT` in `scripts/descriptors.mjs` remains the single executable definition, read by every assertion. The `.PHONY` roster's **two-stage growth** (9 → 11 at C1, 11 → 13 at C2) is not a second exception: the roster is stated once, in Step 3, and the two boundaries at which it is asserted are a property of A7.15's label, not a second list.

**(e) Neither phantom reference survives in the plan body.** Run against the final file over §0–§11 — everything except the change log and this section. The range is taken **by pattern, not by line number**, so re-running it after any edit needs no arithmetic:

```sh
sed -n '/^## §0 —/,/^## §12 —/p' docs/PLAN-ui-unification-phase1.md > /tmp/body.md
grep -c 'cmd/unified-webapp'   /tmp/body.md   # 0, rc=1
grep -c 'internal/platform/…'  /tmp/body.md   # 0, rc=1
```

Both return zero. Each string does still appear **three** times in the whole file — in its row B1 of the corrections table, in clause (b) above, and in the command block just here — because those passages are the record that the correction happened. Excluding them from the grep is the point of the range, not a way around the check: a reviewer can re-run the same two greps over the whole file and confirm that every hit sits in one of those three passages, none of which is a path a commit manifest or a gate would follow.
