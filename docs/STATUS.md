# Project status — which documents to trust

**Written:** 2026-09-27 · **Branch at time of writing:** `ui-upgrade` @ `5725f5c`
**Purpose:** the doc set does not agree with itself about what phase the project is in.
This file records what is actually true, and which of the other 45 files in `docs/` can be
believed. Ordered most important → least.

Every claim below carries the command that verifies it, so nothing here has to be taken on
faith.

---

## 1. Read this first: the same labels mean different things in different files

This is the root problem. Three independent numbering schemes are in active use, and none of
the documents disambiguate them. A reader who trusts a filename will draw the wrong conclusion.

### 1a. "Phase 5" / "Phase 6" — three different projects

| Namespace | Defined in | Its "Phase 5" | Its "Phase 6" |
|---|---|---|---|
| **A. Consolidation** (original 4-app merge) | `session.md` | Timetracker module | does not exist — stops at 5 |



| **B. UI unification** | `docs/FRD-ui-unification.md` | menuserver jQuery rewrite | the punch-list doc — **menu/CSS fixes only** |
| **C. utuber-taskmaster lane** (internal to that one plan) | `docs/PLAN-utuber-taskmaster-lane.md` §6 | migrate utuber, **delete `internal/utuber/jobs`** | docs + `make check` + owner sign-off |

**The Phase 6 described as "certmachine CA regeneration + utuber/taskmaster improvements +
file tree" is not written down anywhere.** `FRD-ui-unification.md` stops at Phase 5. The only
document titled "Phase 6" covers hamburger menus and CSS. That scope exists only in
conversation, and it fragmented — see §5.

> This is what caused a wrong reading during the 2026-09-27 assessment: "Phase 5 already
> happened" (namespace B — true) was initially read as namespace C's Phase 5 (not started).

- `grep -n 'Phase 6' docs/*.md *.md` → 7 documents use "Phase 6" for 7 unrelated meanings.

### 1b. `C1`…`C14` commit labels — reused across four sequences

| Label | 2026-09-18 (`progress.txt`) | 2026-09-21 | 2026-09-22 (punch list) |
|---|---|---|---|
| C1 | taskmaster | delete legacy trees | *(CC-2 text-entry bug — **no such commit**)* |
| C2 | **certmachine** | todo FA/CDN | todo ban icon |
| C3 | **multissh** | shared dialogs | sampler spelling |
| C4 | **admin** | — | **obsidianoid** |
| C7 | D-5 fix | — | **certmachine** |
| C8 | deslop | — | multissh |
| C9–C14 | — | — | smbedit → login page |

`C2` has three meanings. `C3` has three. `C4` and `C7` have two each. **Never cite a `C<n>`
label without its date and commit hash.**

- `git log --format='%h %ad %s' --date=format:'%m-%d' 5f3e035~4..5725f5c | cat`


***progress.txt has been deleted***

---

## 2. Where the project actually is (verified 2026-09-27)

**Everything is green.** Full `make check` equivalent, run directly:

| Gate | Result |
|---|---|
| `go build ./...` | clean |
| `go vet ./...` | clean |
| `gofmt -l .` | empty |
| `go test -race ./...` | all 36 packages pass |
| `npx tsc --noEmit` | clean |
| `npm run test:web` | 189/189, 12 suites |
| `web-verify` / `artifacts.mjs` | 22 artifacts, byte-identical |
| `bundle-shape` | 19 sharedConsumer bundles pass |
| `token-overlap` | empty intersection |
| `check-shared-css` / `check-shared-barrel` | pass |

**Branches are clean.** `ui-upgrade` is 73 commits ahead of `origin/main`, 0 behind — a
straight fast-forward, no merge risk.
- `git rev-list --count origin/main..HEAD` → 73 · `git rev-list --count HEAD..origin/main` → 0

~~~**Toolchain is current.** go.mod `go 1.26.2`, local toolchain go1.26.3.~~~

***toolchain is go 1.26 or later; not particularly critical***

**Two live workstreams:**

1. **utuber-on-a-taskmaster-lane** — approved 2026-09-27, namespace C above. Internal Phases
   1–4 **done**, Phases 5–6 **not started**. Evidence:
   - R4 invariant: `grep -rni utuber internal/taskmaster | wc -l` → **0**
   - Phase 4 done: `QueuePanel` exported from `web/shared/ts/index.ts` and adopted in
     `web/taskmaster/js/board.ts` (8 references); artifacts committed, `git status web/` clean
   - Phase 5 not done: `test -e internal/utuber/jobs` → **still exists**
2. **certmachine CA replacement** — approved 2026-09-27, code committed, **awaiting owner
   sign-off**. `MANUAL-VERIFICATION-certmachine-ca-replacement.md` states plainly:
   *"None of these has been run."* 9 checks need a live server and a real browser.

**Two items only the owner can unblock:** the certmachine 9-point browser checklist, and
**A-2 passkey registration** — untestable because it needs an HTTPS host.

---

## 3. Trust these four (authoritative for current state)

All dated 2026-09-27.

| Document | Claims | Note |
|---|---|---|
| `docs/PLAN-utuber-taskmaster-lane.md` (120K) | APPROVED, v3, 6 internal phases | Verified independently against §2 |
| `docs/PLAN-certmachine-ca-replacement.md` (48K) | APPROVED, queued for ralph | Current |
| `docs/MANUAL-VERIFICATION-certmachine-ca-replacement.md` | PENDING OWNER SIGN-OFF | Honest about its own gap | 

***docs/MANUAL-VERIFICATION-certmachine-ca-relacement.md and 


| `docs/FRD-admin-identity.md` | CLOSED, not pursuing further | **The only file in the set with an accurate terminal status** |

**One caveat inside a trusted doc:** `FRD-utuber-taskmaster-lane.md` still says *"draft, not yet
planned."* The status line is stale — its plan is approved and four phases are done. Its
*decisions* (D1–D10) are still good; the lane plan's own Phase 6 will fix the pointer.

---

## 4. Do not trust these nine (they assert false state)

Each claims a project state that is demonstrably false. **These will cause someone to redo
work that already shipped.**

| Document | Its claim | Reality |
|---|---|---|
| `docs/SECURITY-NOTES-deferred.md` | "backlog only, do NOT action" · S-1…S-8 all open | **S-1, S-2, S-3, S-4, S-5 all shipped.** Only S-6/S-7 (deployment questions) and S-8 (file modes) are live |
| `docs/PLAN-ui-unification-phase6-punchlist.md` | "Status: `pending approval`" | All its commits landed — `f7e9157` (C0) … `aed2c30` (C14), plus `5447058` (C12a). 12 `C*` commits on 09-22 |
| `progress.txt` (root) | "All 8 commits landed (C1–C8). Architect verification in progress." | 12 more `C*` commits landed after C8, on a later date, with colliding labels (§1b) |
| `shelved/plan-with-platform-auth.md` (152K) | "**Do not execute**" | Its decision O-1 — auth belongs at `internal/platform/auth` — is exactly what shipped |
| `docs/plan.md` (88K) | "pending approval. Do not execute without approval" | multissh shipped: `internal/multissh` exists, 71 test funcs |
| `docs/smbedit-plan.md` (144K) | "pending approval" | smbedit shipped: `internal/smbedit`, 65 test funcs |
| `docs/PLAN-recipes-tab.md` (236K) | "pending approval" | Recipes tab shipped, documented in `README.md` |
| `docs/plan-issue-tracker.md` (116K) + `docs/plan-issuetracker-port.md` (20K) | "pending approval" | issuetracker shipped: `internal/issuetracker` with db + graphql + actor |
| `docs/plan-round-two.md` (188K) | "pending consensus review" | The auth/security round shipped: `internal/platform/auth` |

**~940KB of plan documents say "pending approval" for work that is in the tree.** A literal
reading of the doc set implies almost nothing has shipped.

Verify the security claims: `internal/platform/auth` exists · `middleware/bodylimit.go`
(`http.MaxBytesReader`) · `newServer()` in `cmd/server/main.go:273` sets `ReadHeaderTimeout`
and `IdleTimeout` · `DefaultMaxSubscribers = 64` in `platform/broker/broker.go` · `cors.go`
reflects `Origin` only when same-origin.

---

## 5. The biggest gap: "Phase 6" was never written down

The scope — certmachine CA regeneration, utuber/taskmaster improvements, obsidianoid file
tree — survives only in conversation. It shipped as three fragments, none of them a
"Phase 6" document:

| Fragment | Where it lives | State |
|---|---|---|
| certmachine CA replacement | `FRD-` + `PLAN-certmachine-ca-replacement.md`, both approved 09-27 | Code committed; owner sign-off pending |
| utuber + taskmaster improvements | `FRD-` + `PLAN-utuber-taskmaster-lane.md`, both approved 09-27 | Phases 1–4 of 6 done |
| obsidianoid file tree + search | **No plan document.** Commit `89d89e5` only | Shipped, undocumented as a phase |

Consequence: there is no single place that states "this is what Phase 6 was and how much of
it is left." Reconstructing that cost a full pass over git history, artifact inspection and
invariant greps.

---

## 6. Accurate history — safe to read, useless for current state

- **`session.md`** — clean and authoritative **for its scope**, but it stops at Phase 5
  (timetracker) and has never heard of multissh, smbedit, utuber, certmachine, issuetracker,
  obsidianoid or taskmaster. Reads as a complete project log; is not.
- **`docs/PUNCH-LIST-post-phase5.md`** — the resolution at the *top* says everything is fixed
  except A-2; the "Status Summary (as found on 2026-09-22)" table at the *bottom* lists 5
  BROKEN modules. The truth is at the top. Most readers hit the table first.
- `docs/PLAN-ui-unification-phase1/3/4/5.md` — approved, historical, complete.
- `docs/PLAN-timetracker.md` — approved 2026-09-15, shipped.
- `docs/PLAN-auth-two-state.md` — approved 2026-09-11, shipped.
- `docs/utuber-frd.md`, `docs/smbedit-frd.md`, `docs/multissh-FRD.md`, `docs/certmachine-frd.md`,
  `docs/timetracker-FRD.md`, `docs/frd-issue-tracker.md`, `docs/FRD-recipes-tab.md` — all
  pre-date or accompany shipped work. Treat as design history, not status.
- `taskmaster-progress.txt` (root) — detailed and honest, but ends 2026-09-14. Also logs a
  **COORDINATION ANOMALY**: two agent sessions committing the same branch with 17 files
  uncommitted at risk of clobber. See §9.

---

## 7. Reference docs — make no state claim, so they do not lie

Accurate as reference. Several are stale in *content*: the lane plan §147 already catalogues
four specific inaccuracies in `docs/taskmaster.md` (route table, `?group=` vs `?lane=`, lane
width authority) and `docs/utuber.md` (missing `POST /jobs/delete`).

`certmachine.md` · `multissh.md` · `smbedit.md` · `taskmaster.md` · `utuber.md` ·
`USERGUIDE.md` · `adding-a-module.md` · `INVENTORY-hamburger-menus.md` ·
`INVENTORY-menuserver.md` · `sampler-checklist.md` · `OPEN-QUESTIONS-ui-unification.md` ·
`MANUAL-VERIFICATION-recipes-tab.md`

---

## 8. Root-level strays

Outside `docs/`, dated 2026-09-11…14, no status discipline:
`security-plan.md` (96K) · `security-FRD.md` · `taskmaster-plan.md` (52K) ·
`taskmaster-ui-plan.md` · `taskmaster-ui-FRD.md` · `taskmaster-FRD.md` ·
`taskmaster-progress.txt` · `progress.txt` · `session.md`

---

## 9. Two meta-problems

**File dates are useless for judging freshness.** ~40 files show a 2026-09-26/27 mtime from a
bulk commit while containing content from 2026-09-09…15. The doc set cannot be triaged by
date — only by reading status lines, which is exactly where it lies.

**No CI.** `README.md` states it: *"There is no CI on this repository; the gate suite only runs
when you run it."* For a 14-module monorepo with a five-part gate suite, nothing prevents a
red tree from being committed. `make check` is already one composable command, so CI is close
to free. Note `internal/certmachine` takes **262s** under `-race` (190 test funcs) and
`taskmaster/worker` 96s — those want sharding or a scheduled run.

**Test gap:** `internal/issuetracker/{api,db,models,store}` have **no test files at all**;
only `actor` and `graphql` are covered. `cmd/taskmasterctl` has none. For a DB-backed module
planned at 116K, an untested data layer is the one place a bug reaches production unnoticed.

**Known flake:** `TestProcessRegistry_SuspendResume_RealProcess`
(`internal/taskmaster/worker/process_test.go:53`) — timing-sensitive, drives a real process
group. Flagged pre-existing in `progress.txt`. It passed during the 09-27 run.

---

## 10. Suggested cleanup, in priority order

1. Decide the **Phase 6 scope question in §5** and write it down, or accept that the three
   fragments are the record.
2. Fix the nine lying status lines in §4 — one line each. Cheapest high-value edit in the repo.
3. Resolve "Phase 5" ambiguity: state in §1 of this file which namespace is authoritative, or
   rename the lane plan's internal phases to something that cannot collide (e.g. `L1…L6`).
4. Add CI (§9).
5. Add `issuetracker` data-layer tests.
6. Move the root strays (§8) into `docs/`, or delete them.
7. Retire `shelved/` — its central decision is implemented.
8. Quarantine or fix the worker flake.

## 11. Open question for the owner

The 2026-09-27 assessment read "Phase 5" as namespace C and reported the utuber migration as
not started, on the evidence that `internal/utuber/jobs` still exists. The owner may have meant
namespace B (menuserver + the C0–C14 punch list), which **is** complete. Confirming which is
intended decides whether "finish the lane" is 2 phases of work or 6.
