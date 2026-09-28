# Project status — which documents to trust

**Written:** 2026-09-28 · **Branch:** `ui-upgrade` @ `2d2523a` · **76** commits ahead of
`origin/main`, **0** behind
**Purpose:** the doc set does not agree with itself about what phase the project is in, and —
as of this rewrite — most of its file paths no longer resolve. This file records what is
actually true today. Every claim carries the command that verifies it.

**Supersedes** the 2026-09-27 version of this file, which was wrong in three ways: every
document path it cited had moved, its branch count was stale, and it reported certmachine as
awaiting sign-off when the sign-off was already recorded.

---

## 1. The doc set was reorganised. Old paths are dead.

`docs/` is now sorted into subdirectories. **A `docs/X.md` path from any pre-2026-09-28
document is almost certainly wrong.** 38 of the 50 documents moved to `docs/archived/`.

| Old path (as cited by older docs) | Where it is now |
|---|---|
| `docs/PLAN-utuber-taskmaster-lane.md` | `docs/plans/PLAN-utuber-taskmaster-lane.md` |
| `docs/PLAN-ui-unification-phase7-punchlist.md` | `docs/plans/PLAN-ui-unification-phase7-punchlist.md` |
| `docs/FRD-utuber-taskmaster-lane.md` | `docs/frd/FRD-utuber-taskmaster-lane.md` |
| `docs/utuber.md` | `docs/guides/utuber.md` |
| `docs/taskmaster.md` | `docs/guides/taskmaster.md` — **never archived** (see below) |
| `docs/USERGUIDE.md` | `docs/guides/USERGUIDE.md` |
| `docs/certmachine.md` · `docs/multissh.md` · `docs/smbedit.md` | `docs/guides/…` |
| `session.md` (root) | `session-26Sept2026.md` (root) |
| `progress.txt` · `taskmaster-progress.txt` (root) | **deleted** |

Additional older documents — superseded FRDs, plans, per-module specs, and punch lists — are in
`docs/archived/`, though some were renamed with a date stamp and some were deleted outright, so
there is no complete old-path → new-path mapping. **Anything in `docs/archived/` is either done or
abandoned — never "not started."** Their old `docs/*.md` paths are dead and their internal
cross-references are left exactly as written; treat them as history, not as a path index.

Verify: `find docs -name '*.md' | sort` · `ls docs/guides/`

### 1a. One live reference still points at an old path

| Where | Points at | Should be |
|---|---|---|
| `internal/taskmaster/db/db.go:245` (code comment) | `docs/taskmaster.md` | `docs/guides/taskmaster.md` |

The reorg moved files but did not update inbound links. All of them are **fixed in the working
tree** except this one: it is a code comment, deliberately left alone because repairing it needs a
code change. It is the only broken reference left in the repo.

Verify: `grep -rnoE '\]\(docs/[a-zA-Z0-9/._-]+\.md\)' --include=*.md . | while read _ p; do
test -e "$p" || echo "BROKEN $p"; done`

**`docs/guides/taskmaster.md` is a live document — it was briefly archived by mistake.** The
2026-09-27 reorg moved it to `docs/archived/`; that was wrong, and it has been moved back. It is
the *live* taskmaster reference (476 lines, full API and security guidance) that Phase 7's D9
section updates. **Do not re-archive it.** One consequence: it still contains **no** mention of
func tasks, metrics retention, or hidden lanes, because it predates the lane migration — that is
exactly the D9 gap, not an archiving error.

---

## 2. One phase vocabulary now exists: **D0–D9**

This is the answer to the collision that made "Phase 5" and "Phase 6" unreadable.

| Namespace | Where | Status |
|---|---|---|
| **Phase 7** | `docs/plans/PLAN-ui-unification-phase7-punchlist.md`, sections **D0–D9** | **authoritative for current work** |
| UI unification (older) | superseded punch lists, in `docs/archived/` | shipped, historical |
| utuber→taskmaster lane | `docs/plans/PLAN-utuber-taskmaster-lane.md` §6 "Phase 1–6" | **its own numbering — not Phase 7** |

**The lane plan's "Phase 5" and "Phase 6" are steps 5 and 6 of that one plan.** They are not
project phases. They are now **D8** and **D9** in Phase 7. If a line in the lane plan says
"Phase 5", it means D8.

The old `C1`…`C14` commit labels remain ambiguous across four dated sequences (2026-09-18,
09-21, 09-22, and the archived punch lists). **Never cite a `C<n>` label without its commit
hash.** `git log --format='%h %ad %s' --date=format:'%m-%d'` resolves them.

---

## 3. Where the work actually is

### Phase 7 sections D0–D9 — sequenced, with one hard ordering constraint

Full detail in `docs/plans/PLAN-ui-unification-phase7-punchlist.md`. In order:

| # | Section | State |
|---|---|---|
| 1 | **D0** hamburger `side` from config, **no auto-detection** | teardown is the only live work; the option itself shipped in `f7e9157` |
| 2 | **D1** menuserver ☰ anchored far right | already done — `margin-left: auto` at `app.css:87`; no work |
| 3 | **D2** certmachine | **no action** — see §4 |
| 4 | **D3** multissh ☰ right; sign-out control already exists | not started; MS-2 is verify-only — a build would duplicate shared code |
| 5 | **D4** smbedit scroll past footer | not started |
| 6 | **D7** login page theme | **verify first** — the mechanism already exists |
| 7 | **D8** utuber → taskmaster lane; delete `internal/utuber/jobs` | not started |
| 8 | **D5** utuber error text + cookies | **must run after D8** |
| 9 | **D6** taskmaster | **verify only** — queue panel already done |
| 10 | **D9** lane docs + `make check` | runs last |

**D5 cannot run before D8.** D8 rewrites the same five files D5 edits
(`build.go`, `handler.go`, `settings.go`, `media/exec.go`, `web/utuber/js/main.ts`) and pins the
`/settings.json` response shape. Two fixes also appeared in both plans and now have one owner
each: **`media/exec.go` → D8** (P15, which *deletes* the scanner goroutines) and **the failure
text → D5** (which the lane plan never fixes).

### Lane plan progress: steps 1–4 done, 5–6 are now D8/D9

```sh
grep -rni utuber internal/taskmaster | wc -l        # → 0        (R4)
grep -c QueuePanel web/shared/ts/index.ts          # → 3        (exported)
grep -c QueuePanel web/taskmaster/js/board.ts      # → 8        (adopted)
test -d internal/taskmaster/golane && echo ok       # → ok
test -e internal/utuber/jobs && echo exists        # → exists   (step 5 not done)
grep -c lineWriter internal/utuber/media/exec.go   # → 0        (P15 not done)
git status --porcelain web/                        # → clean
```

### certmachine: **signed off — do not reopen**

The certmachine manual-verification record in `docs/archived/` ends with the owner's verdict:
*"all 12: Approved and VERIFIED! Stop asking about this!"* Its own `Status:` header still reads
**PENDING OWNER SIGN-OFF**, which is older than that line and is now wrong. Phase 7 D2 is
therefore a no-op.

---

## 4. Owner sign-off, and one abandoned item

- **A-2 passkey registration — abandoned, not pending.** It was untestable without an HTTPS host
  to register against, and its only record is the post-phase-5 punch list, which is itself
  archived. Under the rule below that makes it *abandoned*, not "still open": there is nothing
  live tracking it. If passkey registration is wanted again it has to be re-raised against a
  real HTTPS origin — it will not be picked up by re-reading the archive.
- The lane plan's §7.3 **E1–E3** browser runs are owner sign-off (item A12) and must be listed
  as pending, not self-certified. These are genuinely live: they are carried by the current
  Phase 7 plan, not by anything archived.

---

## 5. Still accurate from the 2026-09-27 assessment

These were re-verified and hold:

- **Everything is green.** `go build`, `go vet`, `gofmt -l`, `go test -race ./...`,
  `npx tsc --noEmit`, `npm run test:web`, `web-verify` (22 artifacts byte-identical),
  `bundle-shape`, `token-overlap`, `check-shared-css`, `check-shared-barrel`.
- **Anything in `docs/archived/` is either done or abandoned — never "not started."** Some carry
  stale `Status:` headers that say "pending approval" or `PENDING OWNER SIGN-OFF`; treat those as
  noise, not as a signal that work is outstanding. Read the status line's date, and where a
  document claims something is pending, check whether a later document (or the code) already
  landed it.
- **No CI.** `README.md`: *"There is no CI on this repository."* `make check` is one command.
- **Test gap:** `internal/issuetracker/{api,db,models,store}` have no test files;
  `cmd/taskmasterctl` has none.
- **Known flake:** `TestProcessRegistry_SuspendResume_RealProcess`
  (`internal/taskmaster/worker/process_test.go:53`).
- **File dates are useless for freshness** — bulk commits reset mtimes. Read status lines, and
  even those lie.
- **The 8 root strays are gone** (`security-plan.md`, `taskmaster-FRD.md`, `progress.txt`, …).
  Only `README.md` and `session-26Sept2026.md` remain at the root.

---

## 6. Corrections to the previous version of this file

| It said | Reality |
|---|---|
| `ui-upgrade` 73 commits ahead | **76** |
| 45 files in `docs/` | 50 `.md` in 4 subdirectories; 38 in `docs/archived/`, 7 in `docs/guides/` |
| certmachine "awaiting owner sign-off" | **signed off**; the archive file's header is stale |
| every cited doc path | **all moved** — see §1 |
| obsidianoid file tree is commit `89d89e5` | **that commit does not exist** — claim unverifiable |
| `progress.txt` is a live document | **deleted** |
| "Phase 6 scope was never written down" | partly resolved: the lane plan's steps 5–6 are now D8/D9 |
| §11: *which "Phase 5" is intended?* | **answered** — namespace C, the lane plan. Its step 5 is D8 |

---

## 7. If you only read one thing

`docs/plans/PLAN-ui-unification-phase7-punchlist.md` is the only document describing work that
has not happened. Read its "Read this first: execution order and ownership" section before any
of D0–D9 — the ordering is a correctness constraint, and two fixes have a single owner each.
