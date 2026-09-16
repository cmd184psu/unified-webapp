# Work Plan — UI Unification, Phase 1 (Foundation)

**Status: DRAFT v2 — pending approval**
**Scope:** Phase 1 of `docs/FRD-ui-unification.md` only. FRD §7 "Resolved Decisions" is settled input, not a subject of this plan.
**Branch:** `ui-upgrade`

---

## Changes from v1

Revision 2 synthesizes the Architect review (SOUND-WITH-AMENDMENTS) and the Critic review (ITERATE). Each converged blocker and the one major are resolved as follows.

| # | Finding | Resolution in v2 |
|---|---|---|
| **B1** | `static.WithShared(...)` wrapped *outside* certmachine's `closableHandler` erases its `io.Closer`, so `Dispatcher.Close` never closes the cert store → `database/sql` connectionOpener leaks → goleak in `cmd/server/dispatcher_auth_test.go:1242` fails → `make test` red at C3. Also puts `/shared/` outside `stripCORS`. | **Fixed by construction.** Adopted **option B2**: `/shared/` is mounted once in the dispatcher at `cmd/server/main.go:302`, *after* the `io.Closer` type assertion at :300 and *inside* `svc.Gate`. No module's return value is touched, so no closer can be erased and `stripCORS` keeps covering everything certmachine returns. v1's incorrect claim that certmachine's mux is unreachable from `build.go` is corrected in §2 (`s.mux.Handle("/", …)` is at `internal/certmachine/build.go:73`). The B1-style per-module fix is recorded in ADR-002 as the rejected alternative, with the correct `closableHandler{Handler: static.WithShared(srv.Handler(), dir), srv: srv}` form noted for the record. |
| **B2** | Step 2's module list named `shared` (entry `web/shared/ts/index.ts`, which Step 4 creates) → esbuild 0.28.0 exits 1 on a missing entry → `npm run build` red at C2. `shared-css` *would* build and emit an artifact C2 did not list → G6's `git status --porcelain` clause trips. | **Fixed.** Both shared descriptors (`shared` and `shared-css`) move from Step 2 to **Step 4**, where their inputs exist. Step 2 is now a strict zero-output-change refactor of the existing 7 invocations. `web/shared/dist/` is added to C4's Touches column. No skip-on-missing-entry rule is introduced — a missing entry stays a hard build failure. |
| **B3** | Pinning `TM_BUILD_TIME=1970-01-01T00:00:00Z` for reproducible diffs permanently ships "Frontend build: 1970-01-01T00:00:00Z" in taskmaster's hamburger (`web/taskmaster/js/main.ts:219`) and destroys the stale-bundle-detection capability `Makefile:5-8` and `web/taskmaster/js/buildinfo.ts` exist to provide. | **Fixed with the Critic's option.** `scripts/build-web.mjs` resolves the value as `process.env.TM_BUILD_TIME ?? git log -1 --format=%cI -- web/taskmaster ?? new Date().toISOString()`. This is simultaneously **reproducible** (same commit → same bytes, from any checkout, with no pin) and **meaningful** (it dates the frontend source, which is exactly the staleness signal the feature wants). The 1970 pin is deleted from `make web-verify` and from Steps 2 and 6. Chosen over the Architect's normalized-diff alternative because a normalized diff weakens the G2 gate for *every* future taskmaster change in order to tolerate one field, whereas the git-log source makes the field genuinely deterministic and lets G2 stay a byte-identity check across all seven bundles. See ADR-004. |
| **B4** | ADR section was a skeleton — three `Why chosen: _(pending)_`, an undefined "Option 1", a dangling "see §6 Option 4" pointing at the Risks section. | **Fixed.** §10 now carries five written ADRs (001 shared-bundle delivery, 002 route wiring, 003 taskmaster modal adoption, 004 build-time determinism, 005 DOM test harness) with real drivers, honest bounded pros/cons, and filled *Why chosen*. ADR-003 weighs the re-export-shim option the Critic raised and **adopts it**. ADR-001's driver is corrected: dispatch is by `Host` header (`cmd/server/main.go:292`; `local-test/config.json` maps distinct hostnames per module), so each module is a separate browser origin and there is **no** cross-module HTTP cache sharing. The driver that holds is FRD §7.1's "one committed artifact in git instead of N". A10.4 now reads "fetched once per origin per load". FRD §7 itself is untouched. |
| **MAJOR** | v1 chose per-module mounting (B1) after mispricing B2. | **B2 adopted.** ~4 edits instead of ~39, router-agnostic, dissolves the chi `Muxer` miswiring hazard, and makes A8.2 a table-driven Go test over `buildDispatcher` × `knownModules` instead of grep+curl. `MountShared` is retained as tested platform API and is the documented per-module path, used by the new `internal/sampler/build.go` so `docs/adding-a-module.md` has a concrete example. The FR-8.1 wording deviation is recorded in §8. |

Other reviewer findings applied throughout: `SharedHandler` first-segment allowlist (§3.1, A8.4); DOM tests deferred with a hand-rolled stub (ADR-005 — avoids the `@types/node`/`@types/jsdom` gap that would break `npm run typecheck`); G5 replaced with a positive ESM-import assertion plus a `Dynamic require of` negative (v1's sentinel probe was unsound in both directions); build-driver rules gain the no-absolute-paths rule, the cjs/node18 test flags, `check-shared-css.mjs` placement, `npm ci`, and `.PHONY`; theme key-set invariant restated so puma passes, `--color-primary-fg` added, token map completed, `--font-body` pinned, focus-ring contradiction resolved; Step 6 gains the `--font-mono` collision analysis and a corrected grep; R14 mitigation fixed and R17 added; ops items (config samples, README, boot warning, caching contract, `.mjs` Content-Type) added; Q6 deleted (grocery's test is wired unconditionally), Q8 added; verification hygiene corrected repo-wide (`gofmt` via `test -z`, `!`-prefixed negative greps, `git ls-files --error-unmatch`); §2 prose corrected; open questions moved to a tracked doc (`docs/OPEN-QUESTIONS-ui-unification.md`) because `.omc/` is gitignored.

Two findings are original to v2, from running the reviewers' own proposed commands against this tree:

- **The `gofmt` gate must be scoped, not repo-wide.** `gofmt -l internal/ cmd/` is already red on four untouched files (`internal/slideshow/conductor.go`, `internal/todo/model.go`, `internal/utuber/history/history.go`, `internal/utuber/media/media_test.go`). Both reviewers proposed a repo-wide `test -z "$(gofmt -l internal/ cmd/)"`, which would have failed at **every** Phase-1 commit boundary for unrelated reasons. AX.5 is now scoped to Phase-1-touched paths, with the cleanup split out as follow-up 10.
- **The `--font-mono` collision analysis is confirmed by extraction, not by inspection.** `web/taskmaster/style.css` declares exactly 17 custom properties; `--font-mono` is the sole intersection with the shared vocabulary. Two near-misses (`--radius` vs `--radius-md`, `--text-accent/-faint/-muted/-normal` vs `--text-xs..xl`) are *not* collisions. Step 6 records the full list.

One further de-risking fact, verified in the tree: **`web/taskmaster/index.html:12` is already `<script type="module">`**, so Step 6's iife → esm switch requires no HTML change at all — v1 had assumed otherwise.

---

## 0. Scope

### Deliverables

| ID | Deliverable | FRD slice |
|----|-------------|-----------|
| D1 | `web/shared/css/{tokens,themes,components}.css` — structural tokens once on `:root`, all 8 theme palettes, component base | FR-1, FR-2 |
| D2 | One shared runtime bundle: `web/shared/ts/index.ts` → `web/shared/dist/shared.mjs` + `shared.css`, `@shared` externalized from every module bundle | §3.2, FR-7 |
| D3 | `internal/platform/static`: `SharedHandler`, `WithShared`, `MountShared`; `/shared/` reachable on every module host; `internal/utuber`'s duplicate handler deleted | FR-8.1, FR-8.2 |
| D4 | `scripts/build-web.mjs` replaces the `&&` chain; tsconfig gains `paths` for `@shared/*`; `make test-web` / `web-verify` / `check` targets | FR-7 |
| D5 | `web/shared/ts/modal.ts` — taskmaster's `ui/modal.ts` generalized onto theme tokens; taskmaster consumes it via `@shared` | FR-5 |
| D6 | `web/sampler` + `internal/sampler/build.go` + config wiring — module 14, the shared library's dev harness | FR-10 |

### Explicitly out of scope for Phase 1

- Migrating any module other than taskmaster onto shared CSS or shared TS (Phases 2–6).
- Retiring taskmaster's `web/taskmaster/style.css` local tokens (FRD §7.6 — Phase 3).
- `theme.ts`, `menu.ts`, `toast.ts`, `tabs.ts`, `icons.ts`, `dom.ts`, `web/shared/react/` (Phase 2+). Phase 1 ships `modal.ts` only; `index.ts` is the barrel it will grow into.
- Self-hosted fonts under `web/shared/public/fonts/` (FR-6). Phase 1 pins `--font-body`/`--font-mono` to system fallback stacks; see Step 1 note 4.
- Light variants of forest/ocean/ember/rose/puma (FRD §7.3).
- Replacing `alert()`/`confirm()`/`prompt()` outside taskmaster (FRD §7.5 — later phases).
- Entry-layout normalization (`web/<m>/js/` → `web/<m>/src/`). Step 2 keeps every module's current entry path; see Step 2 note 2.

---

## 1. Guardrails

These are mechanisms, not intentions. Each is a command someone can run.

**G1 — Shared CSS is inert for non-adopting pages.**
`web/shared/css/*.css` may contain **no** bare element selectors and no selectors outside the `--`-prefixed custom-property declarations on `:root`/`[data-theme=…]` and the `.ui-*` class namespace. Enforced by `scripts/check-shared-css.mjs` (Step 1), not by grep.
*Softened from v1:* the claim is that shared CSS **cannot change rendering through selector matching**. Property-**name** collisions with a page's own tokens are possible and are resolved by `<link>` order; every adopting page enumerates its overlaps (Step 6 does this for taskmaster).

**G2 — Byte-identity of existing bundles.**
After Step 2, `npm run build` must reproduce all seven existing committed artifacts byte-for-byte:
```
git stash list >/dev/null && npm ci && npm run build && git diff --stat --exit-code -- \
  web/obsidianoid/js/app.js web/obsidianoid/js/threads.js web/slideshow/js/app.js \
  web/multissh/js/bundle.js web/certmachine/js/bundle.js \
  web/taskmaster/js/bundle.js web/smbedit/js/bundle.js web/issuetracker/js/bundle.js
```
Deterministic because B3's git-log build time makes taskmaster's bundle a pure function of the commit.

**G3 — Only taskmaster's page changes.**
No `web/*/index.html` or `web/*/*.html` other than `web/taskmaster/index.html` and the new `web/sampler/` files may be modified in Phase 1. `git diff --name-only main... -- 'web/**/*.html'` must list only taskmaster and sampler.

**G4 — Barrel completeness.**
Every `.ts` file in `web/shared/ts/` other than `index.ts` is re-exported by `index.ts`. Asserted by `scripts/check-shared-barrel.mjs` (Step 4).

**G5 — `@shared` is external, and externalization actually worked.**
Replaces v1's sentinel probe, which was unsound in both directions: with `external: true` esbuild never inlines, so it could not fail for the stated reason; and an exported string const is precisely what esbuild constant-folds.
Positive assertion, per adopting bundle:
```
grep -Eq '^\s*import\s.*from\s*["'\'']/shared/dist/shared\.mjs["'\'']' web/taskmaster/js/bundle.js
```
Negative assertion, repo-wide — catches the silent iife downgrade:
```
! grep -rl 'Dynamic require of' web/*/js/bundle.js
```
*Why the negative matters (verified empirically):* `esbuild --bundle --external:@shared` with **iife** output exits 0 with zero warnings and emits `var import_shared = __require("@shared")`, which throws at runtime. With the specifier rewrite in place, that iife output even **contains** the literal `shared.mjs` inside `__require("/shared/dist/shared.mjs")`, so a naive substring grep passes on a broken bundle. Any bundle importing `@shared` must therefore be `format: "esm"`, and Step 6 item 4's v1 claim that "esbuild refuses" is corrected to "esbuild silently downgrades".

**G6 — Four gates green at every commit boundary.**
`npm ci && npm run build && npm run typecheck && npm run test:web && make test`, then `git status --porcelain` empty.
*Scoping exception at C1 only:* the pre-Step-2 `&&` chain embeds `$(date -u …)`, so running `npm run build` on the old chain makes `web/taskmaster/js/bundle.js` unconditionally dirty. At C1 the `git status --porcelain` clause is evaluated with that one path excluded; from C2 onward it applies with no exclusions.

**G7 — No external host.**
`! grep -rEn '(https?:)?//(fonts\.googleapis|fonts\.gstatic|cdn|unpkg|jsdelivr|cdnjs)' web/shared/ web/sampler/`
No `@import url(...)` with a scheme or `//` prefix anywhere under `web/shared/`.

**G8 — Committed artifacts stay tracked.**
```
git ls-files --error-unmatch web/shared/dist/shared.mjs web/shared/dist/shared.css
```
(`git status --porcelain` cannot distinguish a staged file from an untracked one; `ls-files --error-unmatch` can.)

---

## 2. Current reality (corrected)

Facts this plan depends on, each verified first-hand in this tree.

**Static mounting.** 12 of 13 modules use `*http.ServeMux`; taskmaster uses chi. Of the 12, **9 mount the static handler from `build.go`** (`internal/admin/build.go:62`, `grocery:38`, `issuetracker:45`, `menuserver:20`, `obsidianoid:78`, `slideshow:40`, `timetracker:34`, `todo:23`, `utuber:58`) and **3 own their mux inside a constructor** (`certmachine`, `multissh`, `smbedit`). taskmaster mounts at `internal/taskmaster/build.go:135` via `r.Handle("/*", …)`.
*v1 said "11 of 13 ServeMux" and claimed certmachine's mux was unreachable from `build.go`. Both wrong:* `internal/certmachine/build.go:63` creates the mux and `:73` mounts the static handler, both inside `New`. This divergence is why per-module mounting was expensive — and why B2 sidesteps it.

**Dispatcher shape** (`cmd/server/main.go:288-306`):
```go
for host, module := range cfg.Routing {
    h, ok := built[module]
    if !ok {
        hh, err := buildModule(module, cfg, svc)
        if err != nil { … h = unavailableHandler(module, err) } else {
            if c, ok := hh.(io.Closer); ok { dispatch.closers = append(dispatch.closers, c) }  // :300
            h = middleware.BodyLimit(limitFor(module, cfg), svc.Gate(module, hh))               // :302
        }
        built[module] = h
    }
    dispatch.register(host, h)
}
```
The `io.Closer` assertion is on the **direct** `buildModule` return and happens **before** the wrap. That ordering is what makes B2 safe: inserting a wrapper at :302 cannot erase any closer.
`knownModules` is a 13-name slice at `cmd/server/main.go:337`. `cmd/server/dispatcher_auth_test.go:97-102` replicates the loop as `h = middleware.BodyLimit(limitFor(module, cfg), hh)` (no `Gate`) with the same closer append; `newGateServer` registers `t.Cleanup(dispatch.Close)`.

**Existing static handler** (`internal/platform/static/static.go`, 30 lines) joins `filepath.Clean("/"+r.URL.Path)` onto its dir, stats, and falls back to `index.html` on `IsNotExist`. It does no prefix stripping and no directory-listing suppression. Both behaviours are wrong for an asset subtree: a typo under `/shared/` would return taskmaster's `index.html` with status 200. `SharedHandler` is therefore a new type, not a reuse.

**Build.** `package.json`'s `build` is one 7-invocation `esbuild … && esbuild …` chain; `build:dev` repeats it with `--sourcemap`. `test:web` is 4 `esbuild --bundle --platform=node --format=cjs --target=node18 --log-level=warning | node -` pipes covering `multissh/js/sshcommand.test.ts` and `certmachine/js/{status,listmodel,generate}.test.ts`. `web/grocery/app.test.js` exists and is wired to nothing; run directly it is green (189 pass, 0 fail). devDependencies: `esbuild ^0.28.0`, `typescript ^5.4.0`, `@types/react`, `@types/react-dom` — **no `@types/node`**.

**No CI exists.** There is no `.github/`. "Green at every commit" is a human-run obligation today; Step 2 gives it three `make` targets so it is one command.

**Committed artifacts** (`git ls-files`): `web/{certmachine,issuetracker,multissh,smbedit}/js/bundle.{js,css}`, `web/taskmaster/js/bundle.js` (no `bundle.css`), `web/obsidianoid/js/{app,threads}.js`, `web/slideshow/js/app.js`. `.gitignore` ignores no build output — and ignores `.omc/`, which is why open questions live in a tracked doc (§7).

**taskmaster's page** already uses `<script type="module" src="/js/bundle.js">` (`web/taskmaster/index.html:12`). Switching its bundle from esbuild's default iife to `format: "esm"` therefore needs **no HTML change** — a material de-risking of Step 6 relative to v1's assumption.

---

## 3. Implementation steps

### Step 1 — `web/shared/css/` foundation (D1)

**Creates**
- `web/shared/css/tokens.css` — structural tokens, declared **once** on `:root`, never repeated per theme: `--space-1..8`, `--radius-sm/md/lg/full`, `--text-xs/sm/base/lg/xl`, `--shadow-sm/md`, `--transition`, `--topbar-height`, `--sidebar-width`, `--font-body`, `--font-mono`, plus two Phase-1 scrim/elevation primitives (`--overlay-scrim`, see note 3).
- `web/shared/css/themes.css` — 8 blocks: `:root, [data-theme="dark"]`, then `[data-theme="light"|"obsidian"|"forest"|"ocean"|"ember"|"rose"|"puma"]`.
- `web/shared/css/components.css` — `.ui-modal-*` base only in Phase 1 (the classes Step 4 needs).
- `web/shared/css/index.css` — `@import` of the three, in order tokens → themes → components.
- `scripts/check-shared-css.mjs` — the G1/A1.x gate.

**Palette sources** (read from the tree, transcribed, not invented):
- `obsidian` ← `web/obsidianoid/css/themes.css:2-37` (the violet `:root, [data-theme="dark"]` block), remapped: `--color-surface` → `--color-surface-1`, `--color-surface-2` → `--color-surface-2`, `--color-surface-offset` → `--color-surface-3`, `--color-error` → `--color-danger`, `--color-primary-highlight` → `--color-primary-tint`. `--color-surface-dynamic` has no FR-1 counterpart and is dropped (Phase 3 concern for obsidianoid itself, recorded as a follow-up).
- `forest`/`ocean`/`ember`/`rose` ← `web/obsidianoid/css/themes.css:40,78,116,154`, same remap.
- `dark` ← `web/todo/css/todo.css:3-17` (the neutral GitHub-ish palette; FRD §7.2 makes this the `dark` identity).
- `light` ← `web/todo/css/todo.css:19-33`, with the grocery `:root` scale (`web/grocery/style.css:4+`) supplying nothing colour-wise — grocery contributes only to the structural scale sanity-check.
- `puma` ← FR-2's exact 16-declaration block, transcribed verbatim.

**The key-set invariant** (restated so puma passes — v1's "exactly 17 keys, no more and no less" rejected puma's block and would have failed this step's own gate):
> The canonical key set is the set of `--color-*` properties declared by the `:root, [data-theme="dark"]` block. Every other theme block must declare **exactly** that set — no additions, no omissions. `--font-body` and `--font-mono` are allowlisted **additional** declarations inside a theme block (puma overrides both per FR-2) and are excluded from the equality test. Any property in `themes.css` that is neither a `--color-*` key nor an allowlisted `--font-*` override is a gate failure.

Canonical set = FR-2's 16 puma colour keys **plus `--color-primary-fg`** (17):
`--color-bg`, `--color-surface-1`, `--color-surface-2`, `--color-surface-3`, `--color-border`, `--color-divider`, `--color-text`, `--color-text-muted`, `--color-text-faint`, `--color-primary`, `--color-primary-hover`, `--color-primary-active`, `--color-primary-tint`, `--color-primary-fg`, `--color-danger`, `--color-success`, `--color-warning`.

**Notes**

1. **`--color-primary-fg` is the 18th…17th key, and it is load-bearing.** The donor's `.tm-modal-btn-primary { color: #fff }` (`web/taskmaster/js/ui/modal.ts:99`) has no target in FR-1's vocabulary. White on the light theme's `--color-primary: #01696f` is the risk case that makes a hardcoded `#fff` wrong. Every theme sets it; dark-ish themes set `#fff`, light sets `#fff` (teal `#01696f` at 4.5:1+ against white — verified acceptable), and the token exists so a future theme can differ.
2. **Focus ring — one answer, stated in both places.** The shared focus ring is **`--color-primary`**. v1 said `--color-primary` in Step 1 and mapped the donor's `#9d8fff` ring to `--color-primary-hover` in Step 4; that contradiction is resolved in favour of `--color-primary`, and Step 4's token map below says so. (`obsidian`'s `--color-primary: #7c6af7` is within a shade of the donor's literal, so the visual delta is imperceptible and is listed as a sanctioned delta in Step 6.)
3. **Scrim and elevation are structural, not theme-scoped.** `--overlay-scrim: rgba(0, 0, 0, 0.55)` and `--shadow-md: 0 8px 32px rgba(0, 0, 0, 0.4)` live in `tokens.css` on `:root` with a single Phase-1 value each, carrying the donor's exact literals (`modal.ts:28` and `:44`). This keeps colour literals out of `components.css` (A1.5) without inflating the theme key set. Light-theme scrim tuning is a Phase-2 follow-up.
4. **`--font-body` is pinned to the fallback stack, deliberately.** Phase 1 ships no webfonts (FR-6 is Phase 6), so `--font-body: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif` and `--font-mono: ui-monospace, SFMono-Regular, 'JetBrains Mono', Menlo, monospace`. v1's implied `'Inter', <fallback>` would reference a face Phase 1 does not ship, making rendering machine-dependent in a way no gate can see. The `puma` block's Sora / IBM Plex Mono overrides are transcribed per FR-2 but likewise resolve to their fallbacks until FR-6 lands — recorded as a known Phase-1 limitation, not a defect.

**Verification**
```
node scripts/check-shared-css.mjs
# asserts, and exits non-zero with the offending line on failure:
#  A1.1 tokens.css declares each structural token exactly once, all on :root
#  A1.2 themes.css declares no structural token
#  A1.3 all 8 theme blocks present; key sets equal per the invariant above
#  A1.5 components.css contains no #hex / rgb( / rgba( / hsl( outside comments
#  G1   no bare element selectors; every non-:root/[data-theme] selector is .ui-*
! grep -rEn '(https?:)?//(fonts\.googleapis|fonts\.gstatic|cdn|unpkg|jsdelivr|cdnjs)' web/shared/
npm run build && npm run typecheck && npm run test:web && make test
```
`check-shared-css.mjs` is the gate rather than a grep because v1's element-selector grep missed `a:hover {` and `.ui-card > button {`. The script parses selector lists; it does not pattern-match lines.

---

### Step 2 — `scripts/build-web.mjs` replaces the `&&` chain (D4, part 1)

**Creates** `scripts/build-web.mjs`. **Modifies** `package.json` (scripts), `tsconfig.json` (`paths`, `exclude`), `Makefile` (targets + `.PHONY`).

**This step changes zero build output.** Its only job is to move the same seven invocations behind a data-driven driver so Steps 4–6 can extend a list instead of editing a shell chain.

**Module descriptor list** (exactly the seven that exist today; `shared` and `shared-css` are **not** here — see B2 above):

| name | entry | mode | format | extra |
|---|---|---|---|---|
| obsidianoid | `web/obsidianoid/js/threads.ts`, `web/obsidianoid/js/app.ts` | transpile (`outdir`) | (default) | — |
| slideshow | `web/slideshow/js/app.ts` | transpile (`outdir`) | (default) | — |
| multissh | `web/multissh/js/main.ts` | bundle → `js/bundle.js` | iife | — |
| certmachine | `web/certmachine/js/main.ts` | bundle → `js/bundle.js` | iife | — |
| taskmaster | `web/taskmaster/js/main.ts` | bundle → `js/bundle.js` | iife *(→ esm in Step 6)* | `define: __TM_BUILD_TIME__` |
| smbedit | `web/smbedit/src/main.tsx` | bundle → `js/bundle.js` | iife | `jsx: automatic`, `define: process.env.NODE_ENV`, `logLevel: warning` |
| issuetracker | `web/issuetracker/src/main.tsx` | bundle → `js/bundle.js` | iife | same as smbedit |

**Driver rules** — each exists because violating it breaks a specific gate.

1. **esbuild JS API, one `build()` per descriptor, sequential.** Shared descriptors (from Step 4) run first so a later module could import their output.
2. **Entry paths stay exactly as they are today.** No module is relocated in Phase 1. The descriptor schema carries `mode: "transpile" | "bundle"` precisely so `web/obsidianoid/js/app.ts` (transpile-only, `outdir`) and `web/smbedit/src/main.tsx` (bundled, `outfile`) coexist without either being forced into the other's shape. The target layout (`web/<m>/src/`) is a Phase-2 rename that touches only the `entry` field.
3. **All paths relative to the repo root; never absolute; no `absWorkingDir`; no `path.resolve` on any esbuild input or output field.** esbuild embeds relative source paths as comments in bundles — `web/taskmaster/js/bundle.js:3` is literally `// web/taskmaster/js/ui/modal.ts`. Any absolute path rewrites every one of those comments and fails G2 everywhere at once. This is the single highest-probability cause of a G2 false failure and v1's R2 did not mention it. The driver asserts `!path.isAbsolute(v)` on every descriptor path field before calling esbuild.
4. **`define: { __TM_BUILD_TIME__: JSON.stringify(buildTime()) }`** where
   ```js
   function buildTime() {
     if (process.env.TM_BUILD_TIME) return process.env.TM_BUILD_TIME;
     const r = spawnSync("git", ["log", "-1", "--format=%cI", "--", "web/taskmaster"], {encoding: "utf8"});
     const t = r.status === 0 ? r.stdout.trim() : "";
     return t || new Date().toISOString();
   }
   ```
   Per-descriptor `define`, so no other module sees the flag. This is B3's fix; there is no 1970 pin anywhere.
5. **`--sourcemap` via one flag.** `node scripts/build-web.mjs --dev` sets `sourcemap: true` and flips `process.env.NODE_ENV` to `"development"` for the two React modules. `build:dev` becomes that one command.
6. **`target: "es2020"`, `platform: "browser"`, `logLevel`** mirrored per descriptor from the current chain — including `logLevel: "warning"` on exactly the two React modules, because a different default surfaces different stderr and invites someone to "fix" it by changing flags.
7. **Test runner is the same driver, same flags.** `scripts/test-web.mjs` bundles each test entry with `bundle: true, platform: "node", format: "cjs", target: "node18", logLevel: "warning"` — the exact four flags `test:web` uses today — and pipes to `node`. Test entries: the existing four, **plus `web/grocery/app.test.js` unconditionally** (verified green: 189 pass, 0 fail). v1's Q6 asking whether to wire it is deleted; it is wired.
8. **`check-shared-css.mjs` runs from the driver**, before the first esbuild call, and a failure aborts the build with a non-zero exit. v1's rules 1–8 never said where this ran, which would have left it a gate nobody invokes.
9. **Barrel check** (`check-shared-barrel.mjs`, Step 4) runs alongside it, same placement, same abort semantics.

**`package.json`**
```
"build":     "node scripts/build-web.mjs",
"build:dev": "node scripts/build-web.mjs --dev",
"typecheck": "tsc --noEmit",
"test:web":  "node scripts/test-web.mjs"
```

**`tsconfig.json`**
- `compilerOptions.paths`: `{ "@shared/*": ["web/shared/ts/*"] }` (with `baseUrl: "."`).
- `include`: the **union** of the current 7 enumerated globs **and** `web/shared/**/*`. FR-7.3's literal `["web/*/src/**/*", "web/shared/**/*"]` is **deferred to Phase 2**, because 5 modules still keep sources in `js/` and adopting the literal form now would silently drop them from `tsc --noEmit` — a coverage regression invisible to every gate. Recorded in §8.
- `exclude`: `["node_modules", "web/shared/dist"]`. v1's exclude listed transpiled `.js` outputs that `allowJs: false` could never have included anyway; only `web/shared/dist` does real work (it keeps the emitted `.mjs` out of the program).

**`Makefile`** — add three targets and extend `.PHONY` (currently line 12):
```make
test-web:
	@if [ ! -d node_modules ]; then npm ci; fi
	npm run typecheck
	npm run test:web

web-verify: web
	git diff --stat --exit-code -- \
	  web/obsidianoid/js/app.js web/obsidianoid/js/threads.js web/slideshow/js/app.js \
	  web/multissh/js/bundle.js web/certmachine/js/bundle.js \
	  web/taskmaster/js/bundle.js web/smbedit/js/bundle.js web/issuetracker/js/bundle.js
	! grep -rl 'Dynamic require of' web/*/js/bundle.js

check: web-verify test-web test
```
Also change the existing `web:` target (lines 42-44) from `npm install` to **`npm ci`**. `npm install` may rewrite `package-lock.json`, which both dirties `git status` under G6 and mutates the lockfile this plan treats as the authoritative esbuild pin (`^0.28.0`).

**Verification**
```
npm ci && npm run build
git diff --stat --exit-code -- web/obsidianoid/js/app.js web/obsidianoid/js/threads.js \
  web/slideshow/js/app.js web/multissh/js/bundle.js web/certmachine/js/bundle.js \
  web/taskmaster/js/bundle.js web/smbedit/js/bundle.js web/issuetracker/js/bundle.js   # G2: empty
npm run typecheck && npm run test:web && make test
make check
git status --porcelain    # empty
```

---

### Step 3 — Go: `/shared/` on every module host (D3)

**Creates** `internal/platform/static/shared.go` + `shared_test.go`.
**Modifies** `internal/platform/config/config.go` (one field + one default), `cmd/server/main.go` (one line), `cmd/server/dispatcher_auth_test.go` (one line), `internal/utuber/build.go` (delete the duplicate), `unified-webapp-example.json`, `unified-webapp.json`, `README.md`.

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

**Handler contract, pinned:**
- **Prefix stripping happens inside `SharedHandler`.** All three entry points therefore agree, and there is no "who strips it" ambiguity between `WithShared` and `MountShared`. A test asserts the resolved on-disk path for `/shared/dist/shared.css` is `<dir>/dist/shared.css`.
- **First-segment allowlist.** After stripping, the first path segment must be exactly `dist` or `public`; anything else → 404. This is not deferred hardening — it is the initial behaviour, so `web/shared/ts/*.ts` and `web/shared/css/*.css` are **never** HTTP-reachable. All smoke URLs and acceptance criteria therefore name **`/shared/dist/shared.css`**, never `/shared/css/tokens.css`. (v1's A8.2 canonized the latter, which would have foreclosed exactly this.)
- **Path confinement** via `path.Clean("/"+rest)` before joining, matching `certmachine`'s `staticFileExists` discipline. Traversal cannot escape `dir`. A test drives `/shared/../../etc/passwd`, `/shared/dist/../../../etc/passwd`, and `%2e%2e%2f` forms.
- **Directory requests 404.** No listings, ever.
- **Methods:** GET and HEAD only; anything else 405 with `Allow: GET, HEAD`.
- **Caching contract:** served via `http.ServeFile`, so `Last-Modified` is present on both GET and HEAD and `If-Modified-Since` yields 304. No `ETag` (`ServeFile` supplies none for a plain file). Tested explicitly so a future change of handler cannot silently drop conditional requests.
- **Content-Type for `.mjs`:** a test asserts the response `Content-Type` starts with `text/javascript`. Go 1.26's builtin table maps `.mjs`, but `mime.TypeByExtension` consults the **system** table first, so a host with a stale `mime.types` could serve something a browser refuses for a module script. This must fail in CI-equivalent runs, not in someone's browser.

**Route wiring — B2.** One line in `cmd/server/main.go`, at :302:
```go
h = middleware.BodyLimit(limitFor(module, cfg), svc.Gate(module, static.WithShared(hh, cfg.Server.SharedStaticDir)))
```
and the mirror line in `cmd/server/dispatcher_auth_test.go:97-102`:
```go
h = middleware.BodyLimit(limitFor(module, cfg), static.WithShared(hh, cfg.Server.SharedStaticDir))
```
`static.WithShared` sits **inside** `svc.Gate`, so `/shared/` is authenticated like everything else, and **after** the `io.Closer` assertion at :300, so no module's closer is erased. See ADR-002 for why this beats per-module mounting and §8 for the FR-8.1 wording deviation.

**Config.** `ServerConfig` (`internal/platform/config/config.go:205`) gains exactly one field:
```go
// SharedStaticDir is the directory holding the shared asset tree served at
// /shared/ on every module host. Empty disables the mount.
SharedStaticDir string `json:"shared_static_dir"`
```
`applyServerDefaults` (`:659`) gains one line defaulting it to `./web/shared` when empty. **No `json:"-"` fan-out into 13 module configs and no 13 default lines** — that cost was B1's, and B2 does not pay it.

**Boot-time warning.** `cmd/server` logs a warning (not a failure) when `cfg.Server.SharedStaticDir` does not stat as a readable directory, matching the posture of the existing per-module `checkStaticDir` helpers (`internal/certmachine/build.go:168`, `multissh:87`, `smbedit:56`) while keeping the degradation non-fatal — an operator who has not yet deployed `web/shared/` should still get a running binary in Phase 1.

**utuber de-duplication (FR-8.2).** Delete the verbatim `staticHandler` type and its `ServeHTTP` (`internal/utuber/build.go:68-79`); change `:58` to `mux.Handle("/", static.NewHandler(cfg.StaticDir))`. Behaviour is identical — the deleted code is a byte-for-byte copy of `internal/platform/static`.

**Ops artifacts.**
- `unified-webapp-example.json` gains `"shared_static_dir": "./web/shared"` in its existing `server` block.
- `unified-webapp.json` has **no** `server` key today; add one containing only `shared_static_dir`. (Omitting it would work by default, but the sample configs are the deployment documentation.)
- `README.md` deploy section: `web/shared/` and `web/sampler/` are new required directories alongside the per-module `web/<m>/`.

**Intra-step ordering, for mid-step rollback.** Land in this order so the tree compiles at each sub-point: (a) `ServerConfig` field + `applyServerDefaults` line + sample configs; (b) `shared.go` + `shared_test.go`; (c) the two call sites + boot warning; (d) utuber deletion. (a) and (b) are independently harmless; a failure at (c) reverts one line in each of two files.

**Verification**
```
# gofmt is scoped to the files this phase touches -- see the note below.
test -z "$(gofmt -l internal/platform/static/ internal/platform/config/config.go \
                 cmd/server/main.go cmd/server/dispatcher_auth_test.go internal/utuber/build.go)"
go vet ./...
go test -race ./internal/platform/static/... -run Shared -v
make test                                    # goleak-gated; proves no closer was erased
! grep -rn 'staticHandler' internal/utuber/
! grep -rn 'type staticHandler' internal/ --include=*.go   # only platform/static defines one now
npm run build && npm run typecheck && npm run test:web
git status --porcelain    # empty
```

> **`gofmt` is scoped, not repo-wide — verified necessity.** `gofmt -l internal/ cmd/`
> is **not clean in this tree today**: it lists `internal/slideshow/conductor.go`,
> `internal/todo/model.go`, `internal/utuber/history/history.go`, and
> `internal/utuber/media/media_test.go`. A repo-wide gate would therefore fail at
> every Phase-1 commit boundary through no fault of this work, and the predictable
> reaction — reformatting four untouched files — would add unrelated churn to a
> phase whose entire premise is a minimal diff. The gate is scoped to the paths
> Phase 1 edits (none of which are among those four; note `internal/utuber/build.go`
> is a different file from the two dirty `internal/utuber/**` ones). Reformatting
> those four is a separate, independently revertable commit, recorded as follow-up 10.
> The `test -z "$(...)"` form is still required: bare `gofmt -l` exits 0 while
> listing offenders.
Plus the new table-driven test (A8.2): for every name in `knownModules`, build a `Dispatcher` via `buildDispatcher` with that module routed to a host, then assert `GET http://<host>/shared/dist/shared.css` reaches `SharedHandler` (200 with the tree present) and `GET /shared/ts/modal.ts` is 404. And a companion test asserting `dispatch.closers` is non-empty for a certmachine route — the direct regression guard for what was blocker 1.

---

### Step 4 — Shared bundle + `modal.ts` (D2, D5)

**Creates** `web/shared/ts/modal.ts`, `web/shared/ts/index.ts`, `web/shared/ts/modal.test.ts`, `scripts/check-shared-barrel.mjs`, `web/shared/dist/shared.mjs`, `web/shared/dist/shared.css`.
**Modifies** `scripts/build-web.mjs` (two descriptors), `web/shared/css/components.css` (the `.ui-modal-*` rules).

**The two descriptors that B2 moved here from Step 2:**

| name | entry | mode | format | outfile |
|---|---|---|---|---|
| shared | `web/shared/ts/index.ts` | bundle | **esm** | `web/shared/dist/shared.mjs` |
| shared-css | `web/shared/css/index.css` | bundle | — | `web/shared/dist/shared.css` |

Both entries exist as of this step. The driver's ordering rule puts them first.

**The lift.** `web/taskmaster/js/ui/modal.ts` (390 lines) becomes `web/shared/ts/modal.ts` with three mechanical changes and nothing else:

1. **`ensureStyles()` is deleted entirely** — the ~80 injected CSS lines and the `STYLE_ATTR = "data-tm-ui-modal-styles"` sentinel both go. Those rules move into `web/shared/css/components.css`, retokenized.
2. **Class prefix `tm-modal-*` → `ui-modal-*`** throughout, matching G1's `.ui-*` namespace.
3. **Every colour/metric literal and every Obsidian-era token resolves to a shared token.** Complete map — v1's was missing four rows:

| donor (`web/taskmaster/js/ui/modal.ts`) | shared token |
|---|---|
| `var(--bg-secondary, #252525)` — panel bg | `var(--color-surface-2)` |
| `var(--bg-primary, #1e1e1e)` :62 — input bg | `var(--color-bg)` |
| `var(--bg-tertiary, #2d2d2d)` :82 — secondary button bg | `var(--color-surface-3)` |
| `var(--text-normal, #dcddde)` | `var(--color-text)` |
| `var(--bg-modifier-border, #3a3a3a)` | `var(--color-border)` |
| `var(--radius, 6px)` | `var(--radius-md)` |
| `var(--interactive-accent, #7f6df2)` | `var(--color-primary)` |
| `var(--interactive-accent-hover, #9d8fff)` | `var(--color-primary-hover)` |
| `var(--font-ui, -apple-system, …)` :33 | `var(--font-body)` |
| `rgba(0, 0, 0, 0.55)` :28 — overlay | `var(--overlay-scrim)` |
| `0 8px 32px rgba(0, 0, 0, 0.4)` :44 — panel shadow | `var(--shadow-md)` |
| `#9d8fff` :94 — focus ring | `var(--color-primary)` *(Step 1 note 2)* |
| `color: #fff` :99 — primary button text | `var(--color-primary-fg)` |

**What is preserved byte-for-byte:** the focus trap (`getFocusable` + the Tab-wrapping `onKeydown`, `modal.ts:181-209`), Escape-to-close, focus return via `previouslyFocused.focus()`, overlay-mousedown-to-close, and the four exported signatures — `openModal` (:129), `confirmDialog` (:226), `alertDialog` (:277), `promptDialog` (:324) — plus `ModalOptions`, `ModalHandle`, `DialogOptions`, `PromptOptions`. FR-5's behavioural requirements are met because they are the donor's existing behaviour, unmodified.

**Security invariants carried forward unchanged:** all user-supplied text goes through `textContent`; no `innerHTML` of an interpolated string anywhere in `modal.ts`; no `alert()`/`confirm()`/`prompt()`.

**`web/shared/ts/index.ts`** — explicit named re-export barrel:
```ts
export { openModal, confirmDialog, alertDialog, promptDialog } from "./modal.js";
export type { ModalOptions, ModalHandle, DialogOptions, PromptOptions } from "./modal.js";
```

**`@shared` resolution.** An esbuild `onResolve` plugin in the driver, applied only to bundled browser descriptors:
```js
build.onResolve({ filter: /^@shared(\/.*)?$/ }, () => ({ path: "/shared/dist/shared.mjs", external: true }));
```
Every `@shared/...` specifier collapses to the single barrel URL, which is correct because `shared.mjs` **is** the barrel. `tsconfig`'s `paths` (Step 2) makes `tsc --noEmit` resolve the same specifiers to source, so types are checked while bytes stay external.

**Tests.** `web/shared/ts/modal.test.ts`, run through `scripts/test-web.mjs` with the same cjs/node18 flags. Phase-1 coverage: option normalization/defaults, the `textContent`-only escaping path, and the barrel's export shape — driven against a ~20-line hand-rolled element stub in the test file. Focus-trap, Escape, focus-return and backdrop-click move to the Step-5 sampler keyboard checklist. See **ADR-005** for why no jsdom.

**Verification**
```
node scripts/check-shared-barrel.mjs            # G4
npm run build
git ls-files --error-unmatch web/shared/dist/shared.mjs web/shared/dist/shared.css   # G8 (after add)
grep -Eq '^\s*import\s' web/shared/dist/shared.mjs || true      # barrel has no external imports; informational
! grep -rEn 'innerHTML' web/shared/ts/
! grep -rEn '\b(alert|confirm|prompt)\s*\(' web/shared/ts/
! grep -rEn '(createElement\(["'\'']style|style\.textContent)' web/shared/ts/modal.ts   # A1.4, see below
node scripts/check-shared-css.mjs
npm run typecheck && npm run test:web && make test
git status --porcelain    # empty
```
**A1.4 was vacuous in v1** ("modal.ts contains no CSS") — once `ensureStyles()` is deleted, `modal.ts` contains no CSS *by construction*, so the criterion could not fail. The replacement above asserts the mechanism instead: no `createElement("style")` and no `style.textContent`, i.e. the file cannot inject styles even if someone reintroduces a rule string.

---

### Step 5 — `sampler`, module 14 (D6)

**Creates** `web/sampler/{index.html,style.css,js/main.ts}`, `internal/sampler/build.go` (+ test), `docs/sampler-checklist.md`.
**Modifies** `internal/platform/config/config.go`, `cmd/server/main.go` (`buildModule` case + `knownModules`), `scripts/build-web.mjs` (one descriptor), `local-test/config.json`, `unified-webapp-example.json`, `docs/adding-a-module.md`.

Follows `docs/adding-a-module.md` exactly: (1) `SamplerConfig{StaticDir string}` + expander + `DefaultConfig` default `./web/sampler`; (2) `buildModule` case; (3) `knownModules` entry (13 → 14); (4) a `host_routing` entry in the sample/local configs; (5) static assets under `web/sampler/`.

`internal/sampler/build.go` is the **documented per-module `MountShared` example**:
```go
mux := http.NewServeMux()
if err := static.MountShared(mux, cfg.SharedStaticDir); err != nil { return nil, err }
mux.Handle("/", static.NewHandler(cfg.StaticDir))
```
B2's dispatcher-level `WithShared` already covers `/shared/` for every host including sampler's, so this mount is redundant at runtime — deliberately. It keeps `MountShared` exercised by a real module, gives `docs/adding-a-module.md` something to point at, and is the migration path for any module that later wants the subtree inside its own middleware. `MountShared` returns an error (rather than panicking) so a chi router passed by mistake is reported rather than silently registering a wrong exact-match pattern — see the `Muxer` hazard in R8.

**Auth-gated like every other module** — one `auth.modules` entry in the sample configs. No special-casing.

**Page content (Phase 1):** `index.html` links `/shared/dist/shared.css` **and nothing else colour-bearing**; a theme `<select>` writing `document.documentElement.dataset.theme` through all 8 values; a token swatch grid rendered from the canonical key list; a modal section with buttons for `openModal`, `confirmDialog`, `alertDialog`, `promptDialog`; and a source snippet beside each, rendered via `textContent` into a `<pre>` (never `innerHTML`). `web/sampler/js/main.ts` imports from `@shared` and bundles to `web/sampler/js/bundle.js` with `format: "esm"`.

**`docs/sampler-checklist.md`** is the manual verification surface for the behaviour Phase-1 automated tests do not cover (ADR-005): for each of the 8 themes — open each dialog; Tab cycles within the panel and wraps; Shift-Tab wraps backwards; Escape closes; focus returns to the invoking button; backdrop mousedown closes; primary-button text is legible against `--color-primary` (the `--color-primary-fg` check); no element is unreadable. Checked once per theme before C5 is considered done, and re-run in every later phase that touches shared CSS.

**Verification**
```
test -z "$(gofmt -l internal/sampler/ internal/platform/config/config.go cmd/server/main.go)"
go vet ./... && make test
npm run build && npm run typecheck && npm run test:web
grep -c '"sampler"' cmd/server/main.go        # buildModule case + knownModules
curl -sI http://sampler.local:PORT/shared/dist/shared.css   # 200 + Last-Modified, via local-test
curl -so /dev/null -w '%{http_code}' http://sampler.local:PORT/shared/ts/modal.ts   # 404
# then: docs/sampler-checklist.md, all 8 themes
git status --porcelain    # empty
```

---

### Step 6 — taskmaster consumes the shared modal (D5, part 2)

This is the **only** step in Phase 1 that changes a rendered pixel, and only on taskmaster's page.

**Modifies** `web/taskmaster/index.html` (one `<link>`), `web/taskmaster/js/ui/modal.ts` (390 lines → a 2-line re-export shim), `scripts/build-web.mjs` (taskmaster descriptor: `format: "esm"`), `web/taskmaster/js/bundle.js` (regenerated).

**Adoption via re-export shim** (ADR-003). `web/taskmaster/js/ui/modal.ts` becomes:
```ts
export { openModal, confirmDialog, alertDialog, promptDialog } from "@shared/modal";
export type { ModalOptions, ModalHandle, DialogOptions, PromptOptions } from "@shared/modal";
```
All five importing call sites — `api.ts:10`, `designer.ts:23`, `outputmodal.ts:13`, `board.ts:27`, `main.ts:16`, each importing `'./ui/modal.js'` — are **untouched**. Two further consequences the shim buys:

- The stale-comment problem disappears. `api.ts:6` ("UI code surfaces failures via `ui/modal.ts`'s …") and `designer.ts:20` ("the only dialog used for validation errors is `ui/modal.ts`'s `alertDialog`") remain **true** through the shim. v1's `grep -rn 'ui/modal' web/taskmaster/` would have matched these prose lines and reported a false failure; with the shim there is nothing to grep for. Where a negative grep is still wanted, the correct narrow form is `! grep -rn "from '\./ui/modal" web/taskmaster/js/ --include=*.ts`, and under the shim it is simply not applicable.
- C6 shrinks to `index.html` + one file + one descriptor field + the bundle, making it a genuinely single-file revert.

**`index.html`** gains `<link rel="stylesheet" href="/shared/dist/shared.css">` **before** the existing `<link rel="stylesheet" href="/style.css">` (line 7). The `<script type="module" src="/js/bundle.js">` at line 12 needs **no change** — taskmaster's page is already a module script, so the iife → esm switch is invisible to the HTML.

**Token-collision analysis — complete and mechanically verified.** `web/taskmaster/style.css` declares exactly 17 custom properties (extracted, not estimated): `--bg-modifier-border`, `--bg-primary`, `--bg-secondary`, `--bg-tertiary`, `--font-mono`, `--font-ui`, `--interactive-accent`, `--interactive-accent-hover`, `--radius`, `--status-blue`, `--status-green`, `--status-red`, `--status-yellow`, `--text-accent`, `--text-faint`, `--text-muted`, `--text-normal`. Intersected against the entire shared vocabulary (`--color-*`, `--space-1..8`, `--radius-sm/md/lg/full`, `--text-xs/sm/base/lg/xl`, `--font-body`, `--font-mono`, `--shadow-sm/md`, `--transition`, `--topbar-height`, `--sidebar-width`, `--overlay-scrim`): **`--font-mono` is the only overlap.** Note the two near-misses that are *not* collisions: taskmaster's `--radius` does not collide with `--radius-md`, and its `--text-accent`/`--text-faint`/`--text-muted`/`--text-normal` do not collide with the `--text-xs..xl` size scale — different names, so no cascade interaction. Both declarations have specificity (0,1,0) on `:root`, so source order decides; `shared.css` is linked **first**, therefore taskmaster's `'JetBrains Mono', 'Fira Code', 'Cascadia Code', monospace` wins at its single consumer (`style.css:288`, `textarea { font-family: var(--font-mono) }`). **No live regression** — and it is documented here rather than discovered later. A mechanical check runs in `web-verify`:
```
comm -12 <(grep -oE '^\s*--[a-z0-9-]+' web/shared/css/*.css | grep -oE '\--[a-z0-9-]+' | sort -u) \
         <(grep -oE '^\s*--[a-z0-9-]+' web/taskmaster/style.css | grep -oE '\--[a-z0-9-]+' | sort -u)
# expected output: exactly "--font-mono"
```
If that set ever grows, the check fails and the new overlap must be analysed before landing.

**Sanctioned visual deltas** — the complete list (v1 listed four; two were missing):
1. Panel/input/button surfaces move from the donor's Obsidian fallbacks to `obsidian`-theme values (near-identical by construction — the `obsidian` palette *is* obsidianoid's violet set).
2. Accent moves `#7f6df2` → `--color-primary` (`obsidian`: `#7c6af7`).
3. Focus ring moves `#9d8fff` → `--color-primary` (`#7c6af7`).
4. Border radius moves `var(--radius, 6px)` → `--radius-md`; `--radius-md` is set to `6px` so this is a no-op by design.
5. **Dialog font stack moves `var(--font-ui, -apple-system, …)` → `var(--font-body)`.** Values are equivalent stacks; the token name differs. *(New in v2.)*
6. **Panel shadow moves the inline `0 8px 32px rgba(0,0,0,0.4)` → `var(--shadow-md)`,** which carries that exact value. *(New in v2.)*

Every delta is confined to modal DOM. Nothing outside a modal on taskmaster's page changes.

**iife → esm sanity checks** (v2 additions — the format switch is the one mechanically risky part of this step):
```
grep -Eq '^\s*import\s.*from\s*["'\'']/shared/dist/shared\.mjs["'\'']' web/taskmaster/js/bundle.js   # G5 positive
! grep -l 'Dynamic require of' web/taskmaster/js/bundle.js                                            # G5 negative
[ "$(grep -c 'var FRONTEND_BUILD_TIME' web/taskmaster/js/bundle.js)" = 1 ]                            # define still applied once
! grep -nE '^\s*this\b' web/taskmaster/js/bundle.js                                                   # no top-level `this` in module scope
```

**Verification**
```
npm run build && npm run typecheck && npm run test:web && make test
node scripts/check-shared-css.mjs && node scripts/check-shared-barrel.mjs
# the four iife→esm checks above
comm -12 ...   # exactly --font-mono
git diff --name-only -- web/ | grep -v '^web/taskmaster/\|^web/sampler/\|^web/shared/'   # no output
# manual, in local-test: load taskmaster, exercise all four dialogs, confirm Tab/Escape/focus-return,
# confirm nothing outside a modal shifted (side-by-side against the pre-C6 build)
git status --porcelain    # empty
```

---

## 4. Commit sequence

Each commit is independently revertable and leaves all four gates green. Ordered so that partial landing is safe and only the last commit changes pixels.

| # | Commit | Touches | Gate at boundary | Revert impact |
|---|---|---|---|---|
| **C1** | `web/shared: add token, theme, and component CSS foundation` | `web/shared/css/*`, `scripts/check-shared-css.mjs` | four gates + `check-shared-css`; G6 git-status clause **excluding** `web/taskmaster/js/bundle.js` (the old chain's `$(date -u)` makes it unconditionally dirty — see G6) | Deletes inert CSS nothing links yet. Zero runtime effect. |
| **C2** | `build: replace the esbuild chain with scripts/build-web.mjs` | `scripts/build-web.mjs`, `scripts/test-web.mjs`, `package.json`, `tsconfig.json`, `Makefile` | four gates + **G2 byte-identity** + `make check`; git-status clause now unexcepted | Restores the `&&` chain. Output was byte-identical, so nothing rebuilt changes. |
| **C3** | `platform/static: serve the shared asset tree at /shared/` | `internal/platform/static/shared.go`(+test), `internal/platform/config/config.go`, `cmd/server/main.go`, `cmd/server/dispatcher_auth_test.go`, `internal/utuber/build.go`, `unified-webapp*.json`, `README.md` | four gates + `go vet` + `gofmt` + the A8.2 table test + the certmachine-closer test | `/shared/` 404s everywhere. No page links it yet (C1's CSS is unlinked, C6 has not landed), so no module regresses. |
| **C4** | `web/shared: lift taskmaster's modal into the shared bundle` | `web/shared/ts/*`, `web/shared/css/components.css`, **`web/shared/dist/`** *(added in v2 — B2's fix)*, `scripts/check-shared-barrel.mjs`, `scripts/build-web.mjs` | four gates + G4 + G5 negative + G8 | Removes `shared.mjs`/`shared.css` and the shared TS. taskmaster still has its own `ui/modal.ts` (untouched until C6), so it keeps working. |
| **C5** | `sampler: add the shared-library dev harness as module 14` | `web/sampler/*`, `internal/sampler/*`, config/main.go/`local-test`, `docs/adding-a-module.md`, `docs/sampler-checklist.md` | four gates + `go vet` + `gofmt` + the sampler build test + the 8-theme checklist | Removes a net-new module. Nothing else references it. |
| **C6** | `taskmaster: consume the shared modal via @shared` | `web/taskmaster/index.html`, `web/taskmaster/js/ui/modal.ts`, `web/taskmaster/js/bundle.js`, `scripts/build-web.mjs` | four gates + the four iife→esm checks + `comm -12` + manual dialog pass | taskmaster returns to its own modal implementation. **The only revert that changes rendering, and it reverts to today's exact rendering.** |

**Dependencies, stated rather than implied:**
- C2's G2 gate is self-contained (it compares against artifacts already in the tree).
- C3's verification includes `npm run build`, which requires C2 to have landed. C3 therefore **depends on C2** — v1 left this implicit. C3 does not depend on C1.
- C4 depends on C1 (components.css) and C2 (the driver). C6 depends on C4.
- C5 depends on C3 (`MountShared`) and C2 (the driver).

**On C4's diff shape.** v1 claimed the modal lift would read as a `git mv`-equivalent rename. It will not: ~80 lines of CSS are deleted, every class is renamed, and 13 values are retokenized, so rename detection is unlikely to fire. The reviewable claim is narrower and checkable: the **focus trap (`modal.ts:181-209`) and the four exported signatures (`:129`, `:226`, `:277`, `:324`) are byte-identical** between donor and lift. Reviewers should diff those ranges specifically. If a cleaner history is wanted, C4 may be split into C4a (copy `ui/modal.ts` → `web/shared/ts/modal.ts` verbatim, so rename detection *does* fire) and C4b (delete `ensureStyles`, rename classes, retokenize) — both leave the tree green, and C4b is then a pure-transformation diff.

---

## 5. Acceptance criteria

Mapped to FRD requirements. Every one is a command or a named test, not a judgement.

**FR-1 — token vocabulary**
- **A1.1** `tokens.css` declares each structural token exactly once, all on `:root`. — `check-shared-css.mjs`
- **A1.2** `themes.css` declares zero structural tokens. — `check-shared-css.mjs`
- **A1.3** All 8 theme blocks present; each declares exactly the canonical `--color-*` key set (17 keys), with `--font-body`/`--font-mono` allowlisted as extras. — `check-shared-css.mjs`
- **A1.4** `web/shared/ts/modal.ts` contains no `createElement("style")` and no `style.textContent`. *(Replaces v1's vacuous "contains no CSS".)*
- **A1.5** `components.css` contains no `#hex`, `rgb(`, `rgba(`, or `hsl(` outside comments. *(New.)*
- **A1.6** `--color-danger` is the canonical name; `--color-error` appears nowhere in `web/shared/`.

**FR-2 — theme roster**
- **A2.1** The `puma` block matches FR-2's 16 declarations exactly, plus its two `--font-*` overrides. — byte comparison in `check-shared-css.mjs`
- **A2.2** `obsidian` reproduces `web/obsidianoid/css/themes.css:2-37`'s violet values under the remap in Step 1.
- **A2.3** `dark` reproduces `web/todo/css/todo.css:3-17`; `light` reproduces `:19-33`.
- **A2.4** All 8 themes render the sampler with no unreadable element. — `docs/sampler-checklist.md`

**FR-8 — Go**
- **A8.1** `SharedHandler`, `WithShared`, `MountShared` exist with the Step-3 signatures and are covered by `internal/platform/static/shared_test.go`.
- **A8.2** For every name in `knownModules`, a dispatcher built by `buildDispatcher` serves `/shared/dist/shared.css` (200) and 404s `/shared/ts/modal.ts`. — table-driven Go test. *(v1 had grep+curl; B2 makes this a real test.)*
- **A8.3** A certmachine-routed dispatcher has a non-empty `closers` slice, and `make test` passes goleak. — direct guard for blocker 1.
- **A8.4** `SharedHandler` 404s anything whose first post-strip segment is not `dist` or `public`; 404s directory requests; 405s non-GET/HEAD; confines traversal; serves `Last-Modified` and honours `If-Modified-Since`; serves `.mjs` as `text/javascript`.
- **A8.5** `internal/utuber` defines no `staticHandler`; `type staticHandler` exists nowhere outside `internal/platform/static`.

**FR-7 — build**
- **A7.1** `npm run build` is `node scripts/build-web.mjs`; the `&&` chain is gone from `package.json`.
- **A7.2** G2 holds: all eight pre-existing artifacts are byte-identical after C2.
- **A7.3** `tsconfig.json` has `paths: {"@shared/*": ["web/shared/ts/*"]}`; `npm run typecheck` passes.
- **A7.4** `make test-web`, `make web-verify`, `make check` exist and are in `.PHONY`.
- **A7.5** `make web:` uses `npm ci`, not `npm install`.
- **A7.6** `tsc --noEmit --listFiles` covers every `.ts`/`.tsx` covered before C2, plus `web/shared/**`. Baseline captured before C2 and stored at `docs/typecheck-baseline-phase1.txt` (tracked, so the comparison is reproducible by a reviewer). *(v1 never said where the baseline lived.)*
- **A7.7** `TM_BUILD_TIME` resolves from `git log -1 --format=%cI -- web/taskmaster` absent an env override; two clean builds of the same commit produce identical bundles; no `1970` literal appears in `scripts/` or `Makefile`.

**FR-5 — modal**
- **A5.1** `web/shared/ts/modal.ts` exports `openModal`, `confirmDialog`, `alertDialog`, `promptDialog` with the donor's signatures.
- **A5.2** Its CSS lives only in `components.css` and references only tokens (A1.5).
- **A5.3** Focus trap, Escape-close, focus-return, backdrop-close verified per theme. — `docs/sampler-checklist.md`
- **A5.4** No `innerHTML` of interpolated content, and no `alert`/`confirm`/`prompt`, anywhere in `web/shared/ts/`.
- **A5.5** taskmaster's four dialogs work post-C6, through the shim, with all five call sites unmodified.

**FR-10 — sampler**
- **A10.1** `sampler` is in `knownModules` and has a `buildModule` case, a config struct with a `./web/sampler` default, and a `host_routing` entry in the sample configs.
- **A10.2** It is auth-gated by a single `auth.modules` entry, like every other module.
- **A10.3** The page demonstrates all 8 themes, the token grid, and all four dialogs, with source snippets rendered via `textContent`.
- **A10.4** The page's only stylesheet is `/shared/dist/shared.css` plus its own `style.css`; `shared.mjs` is fetched **once per origin per load**. *(Corrected from v1's "exactly once" — Host-header dispatch means one origin per module, so there is no cross-module cache sharing. See ADR-001.)*

**Cross-cutting**
- **AX.1** G3: `git diff --name-only main... -- 'web/**/*.html'` lists only `web/taskmaster/index.html` and `web/sampler/index.html`.
- **AX.2** G7: no external host referenced from `web/shared/` or `web/sampler/`.
- **AX.3** G8: `shared.mjs` and `shared.css` are tracked.
- **AX.4** G6 at all six commit boundaries (with C1's documented exception).
- **AX.5** `gofmt -l <Phase-1-touched Go paths>` is empty, asserted via `test -z "$(…)"`. **Scoped deliberately:** the repo-wide form is already red on four untouched files (see the note in Step 3), so a repo-wide gate would fail every commit boundary for unrelated reasons.

---

## 6. Risks

| ID | Risk | Mitigation |
|---|---|---|
| R1 | G2 fails because the driver's esbuild options differ subtly from the chain's flags. | Transcribe flags per descriptor from the chain verbatim, including `logLevel` on exactly the two React modules. C2 does nothing else, so a G2 failure is a one-commit investigation. |
| R2 | **G2 fails because a path became absolute.** esbuild embeds relative source paths as bundle comments (`web/taskmaster/js/bundle.js:3`). One `path.resolve` rewrites every comment in every bundle. | Driver rule 3 forbids absolute paths, `absWorkingDir`, and `path.resolve` on esbuild fields, and asserts `!path.isAbsolute()` on each before building. *(Highest-probability G2 failure mode; absent from v1.)* |
| R3 | A module bundle silently downgrades to iife while importing `@shared`, emitting `__require("@shared")` that throws only at runtime. | G5's positive ESM-import assertion plus the `Dynamic require of` negative, both in `web-verify`. Verified empirically that esbuild exits 0 with zero warnings in this case. |
| R4 | taskmaster's bundle changes non-reproducibly, making G2 unusable. | B3: build time comes from `git log`, so the bundle is a pure function of the commit. |
| R5 | A theme block drifts out of key-set agreement, producing an unset custom property and an invisible element. | A1.3's equality test, gated in the build (driver rule 8). |
| R6 | `--color-primary-fg` reads badly on some theme. | It is a per-theme token precisely so it can be tuned; the 8-theme checklist has an explicit legibility check on primary buttons. |
| R7 | `/shared/` shadows a real module route. | No module currently registers `/shared/`; the A8.2 table test asserts the mount for all 14 and would fail on a collision. `WithShared` diverts only the `/shared/` prefix. |
| R8 | **`Muxer` accepts chi.** `chi.Mux` satisfies `Handle(string, http.Handler)`, so `MountShared(r, dir)` would compile while registering a wrong exact-match pattern → silent 404. | B2 means no production path calls `MountShared` on chi. `MountShared` returns an error and validates that it received an `*http.ServeMux`, so a mistake is reported at boot rather than at request time. *(B1 would have left this a live hazard on taskmaster.)* |
| R9 | `web/shared/ts/*.ts` becomes HTTP-reachable. | The first-segment allowlist is initial behaviour, not deferred hardening; A8.4 tests the 404. |
| R10 | A stale system `mime.types` makes `.mjs` unservable as a module script. | A8.4's Content-Type test. |
| R11 | Conditional-request behaviour silently regresses on a future handler change. | A8.4 tests `Last-Modified` + 304. |
| R12 | Path traversal under `/shared/`. | `path.Clean("/"+rest)` before join; three traversal forms tested. |
| R13 | A future phase links a shared stylesheet into a page whose own tokens collide by name. | G1's softened claim requires every adopting page to enumerate its overlaps; Step 6 does this for taskmaster and the `comm -12` check makes new overlaps fail the build. |
| R14 | Shared CSS visually affects a page that links it. | *Fixed from v1, which said "G3 keeps shared.css off those pages" — wrong, because taskmaster **is** the Phase-1 adopter.* G3 keeps shared.css off the **twelve non-adopting** modules. For taskmaster, safety comes from G1 (no element selectors, `.ui-*` only) plus the enumerated `--font-mono` overlap, not from absence. |
| R15 | `tsc` coverage silently shrinks when `include` is rewritten. | A7.6's `--listFiles` comparison against the tracked `docs/typecheck-baseline-phase1.txt`; FR-7.3's literal include is deferred (§8). |
| R16 | A revert of C4 or C6 leaves a dangling import. | C6's shim means reverting C6 restores the full `ui/modal.ts` and removes the only `@shared` importer on taskmaster; reverting C4 with C6 still in place would break the build, so the revert order is C6-then-C4 and is recorded in the commit messages. |
| R17 | **Modal style precedence inverts.** Today `ensureStyles()` appends a `<style>` **after** the page's `<link>`, so modal rules win ties. After the lift, `shared.css` is linked **first**, so modal rules **lose** ties against `style.css`. | *(New in v2.)* No live collision exists: taskmaster's competing rules are element-level (lower specificity than `.ui-modal-*`) and the `tm-` → `ui-` rename removes every class-name tie. Named here so a future `style.css` addition is recognized as the cause rather than rediscovered. |
| R18 | The login page cannot link shared CSS because `/shared/` is inside the gate. | Open question Q8 — decide or defer explicitly before Phase 2 touches the login page. Phase 1 does not touch it, so this is not a Phase-1 blocker. |

---

## 7. Open questions

Tracked in `docs/OPEN-QUESTIONS-ui-unification.md` (a **tracked** doc — `.gitignore` ignores `.omc/`, so v1's `.omc/plans/open-questions.md` mirror would not have survived a clone).

| ID | Question | Why it matters | Needed by |
|---|---|---|---|
| Q1 | Does `light` get its own `--overlay-scrim`? | A 55%-black scrim over a light page is heavier than intended. | Phase 2 |
| Q2 | Does `--color-surface-dynamic` (obsidianoid-only) need an FR-1 home? | obsidianoid's Phase-3 migration needs somewhere to put it. | Phase 3 |
| Q3 | Do forest/ocean/ember/rose need `--color-primary-fg` values other than `#fff`? | Accent luminance varies across the four. | Phase 2 |
| Q4 | When does FR-7.3's literal tsconfig `include` land, and in the same commit as the `js/` → `src/` renames? | Coupling the two keeps typecheck coverage constant; decoupling risks a gap. | Phase 2 |
| Q5 | Should `web-verify`'s artifact list be generated from the descriptor list rather than hand-maintained? | A hand list drifts as modules are added. | Phase 2 |
| Q7 | Do the remaining 12 modules adopt `shared.css` via `<link>` or via their own bundled CSS? | Determines whether `shared-css` stays a separate artifact. | Phase 2 |
| **Q8** | **`/shared/` sits inside `svc.Gate`, so the login page cannot link `shared.css`** — `internal/platform/auth/gate.go:28` embeds and serves `login.html` before any module handler runs, yet the FRD's scope includes styling it. Options: **(a)** a narrow unauthenticated allowlist for exactly `GET /shared/dist/shared.css`, added beside `Gate`'s existing carve-outs (`GET /healthz`, `GET /api/auth/mode`, `GET /api/auth/whoami`, all before `protected := hasEntry \|\| module == "admin"`); **(b)** permanent inline styles in the login page. | (a) makes one stylesheet world-readable on a closed LAN — small but real posture change. (b) keeps the gate absolute but permanently forks the login page's styling from the token system. | **Decide before Phase 2 touches the login page.** Not a Phase-1 blocker. |

*(v1's Q6 — "should `web/grocery/app.test.js` be wired into `test:web`?" — is deleted. It is wired, unconditionally, in Step 2 driver rule 7; verified green at 189 pass, 0 fail.)*

---

## 8. Deviations from FRD wording

Recorded explicitly so review can accept or reject each one. None of these reopen FRD §7.

| FRD text | Deviation | Rationale |
|---|---|---|
| **FR-8.1** implies `MountShared` is called per module to mount `/shared/`. | `/shared/` is mounted **once** in the dispatcher via `static.WithShared` at `cmd/server/main.go:302`. `MountShared` is still built, tested, documented, and exercised by `internal/sampler/build.go`. | 4 edits instead of ~39; router-agnostic, so the chi and constructor-owned-mux divergences (§2) never have to be special-cased; removes the `Muxer`-accepts-chi hazard (R8) from every production path; and makes the mount impossible to erase a module's `io.Closer` (blocker 1). The FRD's requirement — `/shared/` reachable on every module host, inside auth — is met more completely, since a module added tomorrow gets it with zero work. See ADR-002. |
| **FR-7.3**'s tsconfig `include` is `["web/*/src/**/*", "web/shared/**/*"]`. | Phase 1 uses the **union** of today's 7 enumerated globs plus `web/shared/**/*`. | 5 modules still keep sources in `js/`. Adopting the literal form now drops them from `tsc --noEmit` — a coverage loss no gate would catch. Lands in Phase 2 with the entry renames (Q4). |
| **FR-6** self-hosted Sora / IBM Plex Mono. | Phase 1 transcribes puma's `--font-*` overrides but ships no font files, so they resolve to fallbacks. | FR-6 is Phase 6. Shipping a token that references an absent face would make rendering machine-dependent and invisible to every gate (Step 1 note 4). |
| **FR-1** vocabulary. | Adds `--color-primary-fg` (theme-scoped) and `--overlay-scrim` (structural). | `modal.ts:99`'s `color:#fff` and `:28`'s scrim literal have no FR-1 home, and A1.5 forbids literals in `components.css`. Both are additive. |

---

## 9. Follow-ups (Phase 2+)

1. Migrate the remaining 12 modules onto shared CSS/TS (FRD Phases 2–6), one commit per module, each with its own token-overlap enumeration per G1.
2. Retire taskmaster's local Obsidian tokens (FRD §7.6) and **delete the C6 re-export shim**, rewriting the five call sites to `@shared/modal` directly (ADR-003's deferred cost).
3. Normalize entry layouts to `web/<m>/src/` and adopt FR-7.3's literal tsconfig `include` in the same commit (Q4).
4. Grow `web/shared/ts/`: `theme.ts`, `menu.ts`, `toast.ts`, `tabs.ts`, `dom.ts`, `icons.ts`; `web/shared/react/`.
5. Self-host fonts under `web/shared/public/fonts/` (FR-6) and promote puma's `--font-*` overrides from fallback to real.
6. Replace every `alert()`/`confirm()`/`prompt()` repo-wide with the shared dialog set (FRD §7.5).
7. Add CI. Today "green at every commit" is human-run; `make check` makes it one command, but nothing enforces it.
8. Decide Q8 (login-page styling) before Phase 2 touches the login page.
9. Revisit ADR-005: adopt a DOM harness (jsdom or linkedom, with the `@types/node` cost priced) when shared components outgrow a hand-rolled stub.
10. **Reformat the four pre-existing `gofmt` offenders** — `internal/slideshow/conductor.go`, `internal/todo/model.go`, `internal/utuber/history/history.go`, `internal/utuber/media/media_test.go` — in one independent commit, then widen AX.5's gate to repo-wide. Deliberately **not** part of Phase 1: it is unrelated churn in a phase premised on a minimal diff, and folding it in would make a Phase-1 revert also revert formatting fixes.

---

## 10. Architecture Decision Records

### ADR-001 — Shared runtime ships as one bundle served at `/shared/`

**Decision.** Build `web/shared/ts/index.ts` into a single `web/shared/dist/shared.mjs` plus `shared.css`, commit both, serve them at `/shared/`, and have every module import `@shared/...` as an **external** ESM specifier rewritten to `/shared/dist/shared.mjs`.

**Drivers.**
1. FRD §7.1 resolved that shared content must **not** be recompiled into each module bundle — one committed artifact in git instead of N copies, and module bundles shrink.
2. No version skew: every module executes the same bytes, so a shared-code fix cannot land in 9 modules and miss 4.
3. Bundles are committed and `make build` must not require node, so the artifact has to be a real file in the tree.

**Correction to v1.** v1 (echoing FRD §3.2's prose) cited browser cache sharing across modules as a driver. **It does not exist.** Dispatch is by `Host` header (`cmd/server/main.go:292`; `local-test/config.json` maps a distinct hostname per module), so each module is a separate browser origin with a separate HTTP cache. `shared.mjs` is fetched once **per origin per load**. Driver 1 is the one that holds; A10.4 is worded accordingly. FRD §3.2's own text is not being amended — only this plan's reasoning is corrected.

**Alternatives considered.**
- *Per-module inlining (esbuild resolves `@shared` to source).* Pros: no new HTTP dependency, no format constraint, G5 unnecessary. Cons: violates §7.1 directly; N copies in git; version skew becomes possible the moment one module is rebuilt and another is not. **Rejected** — §7.1 is settled.
- *npm workspace package consumed as a normal dependency.* Pros: idiomatic; `tsc` resolution comes free. Cons: still inlines into each bundle (so §7.1 again), and adds a workspace layer for one internal package.
- *Import map in each HTML page instead of an esbuild specifier rewrite.* Pros: no build-time rewrite; specifiers stay `@shared/...` in the shipped bundle. Cons: requires editing 14 HTML pages (G3 forbids 12 of them in Phase 1) and gives no typecheck-time resolution. **Rejected for Phase 1**, viable later.

**Why chosen.** It is the only option satisfying §7.1 that keeps `make build` node-free, and the externalization is verifiable (G5) rather than assumed. The cost — every adopting bundle must be `format: "esm"` — is real but bounded, and taskmaster's page already uses `type="module"`, so the first adopter pays nothing in HTML.

**Consequences.** Adopting bundles must be esm; G5's two assertions become permanent gates. `web/shared/dist/` is a new committed artifact under G8. A page that fails to link `shared.css` gets unstyled `.ui-*` components — which is why the sampler exists.

**Follow-ups.** Q7 (link vs bundled CSS for the other 12); follow-up 4 (grow the barrel).

---

### ADR-002 — `/shared/` is mounted once in the dispatcher, not per module

**Decision.** Insert `static.WithShared(hh, cfg.Server.SharedStaticDir)` at `cmd/server/main.go:302`, inside `svc.Gate` and after the `io.Closer` assertion at :300, plus the mirror line in `cmd/server/dispatcher_auth_test.go:97-102`. Keep `SharedHandler`, `WithShared`, and `MountShared` as tested platform API; use `MountShared` in `internal/sampler/build.go` as the documented per-module path.

**Drivers.**
1. Zero regression: the mount must not perturb any module's existing handler chain, and in particular must not erase an `io.Closer`.
2. Cost and blast radius: fewer edits across fewer files is fewer chances to break one module.
3. Router-agnosticism: §2 shows three distinct mounting shapes (9 `build.go` ServeMux, 3 constructor-owned, 1 chi).

**Alternatives considered.**
- *B1 — per-module mounting: call `MountShared` in each `build.go`, or wrap each returned handler.* Pros: literal reading of FR-8.1; each module's `/shared/` sits inside that module's own middleware (e.g. certmachine's `stripCORS`). Cons, all verified: **(i)** wrapping `certmachine`'s return erases `closableHandler`'s `Close` (`internal/certmachine/build.go:162`, `:144`), which the dispatcher type-asserts at `main.go:300`, so the cert store never closes, the `database/sql` connectionOpener goroutine leaks, and goleak at `dispatcher_auth_test.go:1242` fails `make test` — this was blocker 1; **(ii)** it needs a `SharedStaticDir` on all 13 module configs (`json:"-"`) plus 13 `applyServerDefaults` lines, ~39 edits versus ~4; **(iii)** it special-cases chi and the three constructor-owned muxes; **(iv)** it leaves R8 (`Muxer` accepts chi, compiles, silently 404s) live on taskmaster. **Rejected.** For the record, the correct B1 form would have been `return closableHandler{Handler: static.WithShared(srv.Handler(), dir), srv: srv}, nil` — mounting *inside* the closer and inside `stripCORS` — plus a test asserting `dispatch.closers` is non-empty for a certmachine route.
- *Mount `/shared/` outside host dispatch, before `middleware.Wrap` selects a module.* Pros: one mount, no per-host anything. Cons: it lands **outside `svc.Gate`**, making the shared tree unauthenticated on every host — a security-posture regression on a system where every module is gated. **Rejected.**
- *`http.FileServer` with `http.StripPrefix` at :302 instead of a new handler.* Pros: no new code. Cons: directory listings, no first-segment allowlist, no 405 discipline — exactly the holes §2 identifies in `static.NewHandler`. **Rejected.**

**Why chosen.** It satisfies driver 1 *by construction* rather than by care: because the wrapper goes on the already-asserted handler, there is no module for which a closer can be lost, so blocker 1 cannot recur through a future module either. It costs ~4 edits, needs no knowledge of any module's router, and converts A8.2 from a grep-plus-curl into a table-driven Go test over `buildDispatcher` × `knownModules` — which is both stronger evidence and cheaper to maintain.

**Consequences.** FR-8.1's wording is deviated from; recorded in §8. `MountShared` is redundant at runtime for sampler — deliberately, to keep it exercised and documented. `/shared/` is inside the gate, which surfaces Q8 (login page). A module that wants `/shared/` inside its own middleware must opt in with `MountShared`; the dispatcher mount is then shadowed for that host, which is the intended precedence.

**Follow-ups.** Q8; R8's `*http.ServeMux` validation inside `MountShared`.

---

### ADR-003 — taskmaster adopts the shared modal through a re-export shim

**Decision.** In C6, reduce `web/taskmaster/js/ui/modal.ts` to a two-line named re-export from `@shared/modal` and leave all five importing call sites (`api.ts:10`, `designer.ts:23`, `outputmodal.ts:13`, `board.ts:27`, `main.ts:16`) untouched. Rewriting them to `@shared/modal` directly is deferred to Phase 3, alongside retiring taskmaster's local tokens.

**Drivers.**
1. C6 is the only commit in Phase 1 that changes rendering; its diff should be as small and as revertable as possible.
2. Zero regression in taskmaster's four dialogs.
3. Phase 3 is already going to open all of taskmaster's UI files to retire its local tokens.

**Alternatives considered.**
- *Direct adoption: delete `ui/modal.ts`, rewrite 5 specifiers to `@shared/modal`.* Pros: no shim to remove later; the dependency is visible at every call site; arguably the "finished" state. Cons: C6 touches 6 files instead of 2, so a revert is a 6-file revert; it strands the prose comments at `api.ts:6` and `designer.ts:20` (which name `ui/modal.ts`) as stale, requiring two more edits or a false-positive grep; and it front-loads churn into the one commit that is already the riskiest.
- *Keep both implementations, feature-flag the switch.* Pros: instant rollback without a revert. Cons: two modal code paths in one bundle, a flag nobody will remove, and the sanctioned-delta list becomes conditional. **Rejected** — a single-file `git revert` is already the faster rollback.
- *Defer taskmaster adoption entirely; ship the shared modal unused in Phase 1.* Pros: Phase 1 changes zero pixels. Cons: the shared modal ships with no production consumer, so the lift's correctness is untested against real call sites until Phase 3, and the coordinator's Phase-1 scope explicitly wants taskmaster consuming `@shared`. **Rejected.**

**Why chosen.** The shim makes C6 a genuine single-file revert — the property the whole commit sequence is organized around — and it keeps two existing prose comments **true** rather than stale, which removes a verification false-positive rather than adding a cleanup task. The deferred cost (5 specifiers) is real but lands in a phase that is already editing those files for the token retirement, so it is nearly free later and would be pure risk now.

**Consequences.** `web/taskmaster/js/ui/modal.ts` survives Phase 1 as a one-line indirection; follow-up 2 removes it. The negative grep `! grep -rn "from '\./ui/modal"` is *not* applicable in Phase 1 (by design). G5's positive import assertion is what proves the shim actually reaches `@shared`.

**Follow-ups.** Follow-up 2.

---

### ADR-004 — `__TM_BUILD_TIME__` comes from git, not from a pin

**Decision.** `scripts/build-web.mjs` resolves the value as `process.env.TM_BUILD_TIME ?? git log -1 --format=%cI -- web/taskmaster ?? new Date().toISOString()`. No pinned constant anywhere; `make web-verify` sets no override.

**Drivers.**
1. G2 (byte-identity of committed bundles) requires the build to be deterministic.
2. `web/taskmaster/js/buildinfo.ts` and `Makefile:5-8` exist to make a stale frontend bundle visible; `web/taskmaster/js/main.ts:219` renders the value as "Frontend build" in the hamburger's Server section. That capability must survive.
3. Whatever mechanism is chosen must not weaken G2 for unrelated future changes.

**Alternatives considered.**
- *Pin `TM_BUILD_TIME=1970-01-01T00:00:00Z` in `web-verify` (v1).* Pros: trivially deterministic. Cons: `make web` and `make web-verify` share the `web` prerequisite, so the pinned value is what gets **committed** — taskmaster permanently displays "Frontend build: 1970-01-01T00:00:00Z" and the staleness feature is dead. **Rejected** — this was blocker 3.
- *Normalized diff: drop taskmaster's bundle from `web-verify`'s literal set and compare content modulo the timestamp line (Architect's option).* Pros: leaves `$(date -u)` semantics untouched. Cons: it permanently downgrades G2 for taskmaster's bundle from byte-identity to fuzzy-match — the one bundle with the most Phase-1 churn — and every future reviewer has to trust the normalizer. **Rejected.**
- *Drop the field.* Cons: deletes a deliberate capability documented in two places. **Rejected.**
- *Backend-stamped only (reuse the Go `BUILD_TIME` LDFLAG).* Cons: the field exists precisely because the bundle and the binary are independent artifacts; sourcing both from one stamp defeats its purpose.

**Why chosen.** It is the only option that makes the value simultaneously deterministic *and* meaningful. Because the timestamp is a function of the commit, G2 stays a strict byte-identity check across all eight artifacts with no normalizer and no pin, and the displayed value dates the frontend **source** — which is a better staleness signal than wall-clock build time, since a rebuild with no source change should not look like new frontend code.

**Consequences.** Building from a shallow clone or a tarball with no git history falls through to `new Date()`, which is non-deterministic — acceptable, because G2 is only asserted in a git checkout, and `TM_BUILD_TIME` remains available as an explicit override. A commit that changes nothing under `web/taskmaster/` leaves the value unchanged, which is correct.

**Follow-ups.** None.

---

### ADR-005 — Phase 1 ships no DOM test harness

**Decision.** `web/shared/ts/modal.test.ts` tests option normalization, the `textContent`-only escaping path, and the barrel's export shape against a ~20-line hand-rolled element stub defined in the test file. Focus-trap, Escape, focus-return and backdrop-click are verified through `docs/sampler-checklist.md`, per theme. No jsdom, no linkedom, no new devDependency.

**Drivers.**
1. `npm run typecheck` must stay green. There is **no `@types/node`** in `devDependencies`; adding jsdom requires `@types/jsdom`, which requires `@types/node`, which changes what `tsc --noEmit --strict` sees across the whole program.
2. The four gates must be green at every commit, so a harness cannot be a half-landed thing.
3. FR-5's behavioural requirements are the donor's existing, shipping behaviour — Phase 1 preserves it byte-for-byte (`modal.ts:181-209`) rather than newly implementing it.

**Alternatives considered.**
- *Adopt jsdom + `@types/jsdom` + `@types/node`.* Pros: real focus/keyboard semantics; automated coverage of FR-5's behavioural clauses. Cons: three new devDependencies and a program-wide `tsc` surface change landing inside the same phase as the lift, so a typecheck failure would be ambiguous between the two; and jsdom's focus model diverges from browsers precisely around focus traps, so a green test is weaker evidence than the manual checklist for the thing being tested.
- *linkedom instead of jsdom.* Pros: much lighter, no `@types/node` chain in practice. Cons: **no layout and no focus management at all**, so it cannot test the focus trap either — it buys the dependency cost without the capability that motivated it. Not weighed in v1; weighed and rejected here.
- *No tests at all for `modal.ts` in Phase 1.* Cons: the escaping path and the option defaults are pure logic and cheap to cover; leaving them untested wastes the one thing a stub *can* verify well.

**Why chosen.** The behaviour a DOM harness would test is behaviour Phase 1 is preserving verbatim, and the harness that could test it best (jsdom) is least trustworthy exactly there. Spending three dependencies and a `tsc`-surface change on weak evidence, inside the phase that most needs unambiguous gates, is the wrong trade. The stub covers what is actually new (tokenized rendering, barrel shape, option handling); the sampler covers what is actually risky (keyboard and focus, across 8 themes) with a human who can see it.

**Consequences.** Part of FR-5's verification is manual and must be re-run whenever shared CSS or `modal.ts` changes — which is why `docs/sampler-checklist.md` is a committed document rather than a note in this plan. Phase 2, adding components with real interaction logic, should revisit this (follow-up 9).

**Follow-ups.** Follow-up 9.

---

## 11. RALPLAN-DR summary (short mode, v2)

### Principles

1. **Mechanism over intention.** Every guardrail is a command that fails, not a sentence asking for care. Where v1 had a guardrail that could not fail (A1.4) or could not detect what it claimed (G5), v2 replaced it.
2. **Only the last commit changes a pixel.** The commit sequence is ordered so that C1–C5 are behaviour-preserving and C6 is a single-file revert.
3. **Zero regression is proven, not asserted.** Byte-identity of eight committed artifacts (G2), a table-driven mount test across all 14 modules (A8.2), and a goleak-gated closer test (A8.3).
4. **Fix by construction where possible.** The dispatcher-level mount cannot erase a closer for *any* module, present or future — a stronger property than fixing certmachine.
5. **Defer honestly.** Deviations from FRD wording (§8) and deferred work (§7, §9) are enumerated with rationale rather than quietly skipped.

### Decision drivers (top 3)

1. **Zero functionality or visual regression in the 12 non-adopting modules**, with taskmaster's modal the single sanctioned exception.
2. **All four gates green at every commit boundary** — `npm run build`, `npm run typecheck`, `npm run test:web`, `make test` — with no CI to catch a miss.
3. **Lowest blast radius per unit of foundation delivered**, because Phase 1 is load-bearing for five later phases.

### Viable options on the genuine open micro-decisions

*(None of these reopen FRD §7.)*

**1. Route wiring — RESOLVED to B2.**
- **B2, dispatcher-level `WithShared` at `main.go:302`.** Pros: ~4 edits; router-agnostic; cannot erase an `io.Closer`; A8.2 becomes a real test; new modules get `/shared/` free. Cons: deviates from FR-8.1's per-module wording; `/shared/` sits outside each module's own middleware (certmachine's `stripCORS` no longer covers it — acceptable, since the shared tree has no private content and `middleware.Wrap`'s permissive CORS on a public stylesheet is not the exposure `stripCORS` exists to prevent).
- **B1, per-module `MountShared`/wrap.** Pros: literal FR-8.1; subtree inside each module's middleware. Cons: erases certmachine's closer unless mounted inside `closableHandler`; ~39 edits; special-cases chi and 3 constructor-owned muxes; leaves the `Muxer`-accepts-chi hazard live.

**2. Build-time determinism — RESOLVED to git-log sourcing.**
- **git-log `%cI` with env override.** Pros: deterministic *and* meaningful; G2 stays strict byte-identity for all eight artifacts; no pin to forget. Cons: falls back to wall-clock outside a git checkout.
- **Normalized diff for taskmaster's bundle.** Pros: no change to how the value is produced. Cons: permanently weakens G2 for the most-churned bundle; requires trusting a normalizer.

**3. taskmaster modal adoption — RESOLVED to the re-export shim.**
- **Shim.** Pros: C6 is a 2-file diff and a single-file revert; keeps two existing prose comments true; removes a grep false-positive. Cons: a one-line indirection survives Phase 1; 5 specifiers still to rewrite in Phase 3.
- **Direct rewrite of 5 call sites.** Pros: finished state now; dependency visible at each call site. Cons: 6-file revert on the riskiest commit; strands two comments as stale.

**4. DOM test harness — RESOLVED to no harness in Phase 1.**
- **Hand-rolled stub + sampler checklist.** Pros: no new dependency, no `tsc`-surface change inside the lift commit; tests what is new, humans verify what is risky. Cons: FR-5's behavioural clauses are manually verified.
- **jsdom + `@types/jsdom` + `@types/node`.** Pros: automated keyboard/focus coverage. Cons: three dependencies and a program-wide typecheck surface change landing in the same phase; jsdom's focus model is least faithful exactly at focus traps.
- **linkedom.** *Invalidated:* no focus management at all, so it cannot test the behaviour that motivates a harness — cost without capability.

**5. tsconfig `include` shape — one option survives.**
- **Union of today's globs + `web/shared/**/*`.** The literal FR-7.3 form (`web/*/src/**/*`) is **invalidated for Phase 1**, not merely deprioritized: 5 modules keep sources in `js/`, so adopting it silently removes them from `tsc --noEmit`, and no gate in this plan or the repo would detect the loss. It becomes viable only once the entry renames land (Q4, Phase 2). Recorded in §8.

**6. Shared-asset exposure — one option survives.**
- **First-segment allowlist (`dist`, `public`) inside `SharedHandler`.** The alternative — serve the whole `web/shared/` tree and harden later — is **invalidated** because it makes `web/shared/ts/*.ts` HTTP-reachable from day one and, worse, would let an acceptance criterion canonize `/shared/css/tokens.css` as the smoke URL (as v1's A8.2 did), after which the allowlist is a breaking change rather than a one-line hardening.

### Mode

**SHORT.** No pre-mortem or expanded test-plan section is included. Phase 1 touches no data path, no auth decision, and no external interface; its risk is concentrated in build reproducibility and CSS inertness, both of which are covered by mechanical gates (G1–G8) rather than by scenario analysis.
