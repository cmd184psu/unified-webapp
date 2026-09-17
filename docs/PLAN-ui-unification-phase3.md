# Phase 3 Plan — UI Unification: Adopt in Remaining Vanilla Modules

Status: **pending approval**
FRD: docs/FRD-ui-unification.md §6 row 3
Branch: ui-upgrade (never push, never touch main)
Predecessor: Phase 2 COMPLETE (10 commits 063f594..0726794)

---

## 1. Scope

Adopt the shared components (ThemeManager, HamburgerMenu, Toast, shared.css
tokens/themes) in the 6 modules not yet touched:

| Module | Current state | Adoption scope |
|---|---|---|
| taskmaster | TS/ESM, sharedConsumer, shared.css linked, own hamburger, own Obsidian-style tokens | Retire local tokens, replace hamburger with HamburgerMenu, switch modal import to @shared direct |
| certmachine | TS/esbuild IIFE, own toast.ts, short-name tokens | ESM conversion, sharedConsumer, shared.css, ThemeManager + HamburgerMenu, replace local toast |
| multissh | TS/esbuild IIFE, short-name tokens, xterm theme | ESM conversion, sharedConsumer, shared.css, ThemeManager + HamburgerMenu, wire xterm to ThemeManager |
| grocery | Plain JS (1997 lines), Google Fonts CDN, `--color-*` tokens (close to shared) | TS conversion, build entry, sharedConsumer, shared.css, ThemeManager + HamburgerMenu, remove CDN |
| admin | Plain JS (776 lines), no tokens, no theme | TS conversion, build entry, sharedConsumer, shared.css, ThemeManager + HamburgerMenu |
| timetracker | Plain JS (736+245 lines), broken FA, no tokens | TS conversion, build entry, sharedConsumer, shared.css, ThemeManager + HamburgerMenu, remove FA |

**Carry-forward items from Phase 2:**
- D-5 user directive: hamburger reachable in obsidianoid thread mode
- Deslop: ~45 plan-specific references in web/shared/ts/*.ts source files

**Out of scope:** Phase 4 (React modules: smbedit, issuetracker, slideshow,
utuber), Phase 5 (menuserver jQuery rewrite). No feature changes beyond
theme/menu adoption. No Go-side changes needed — the dispatcher already wraps
all module handlers with `static.WithShared(handler, cfg.Server.SharedStaticDir)`
(cmd/server/main.go:326) and the auth gate carve-out (Phase 2 C8) already
permits unauthenticated GET of shared.css/shared.mjs/woff2.

---

## 2. RALPLAN-DR Summary

### Principles

1. **Zero regression** — every existing control, shortcut, and behavior survives
2. **Mechanical-first** — prefer renames/moves over rewrites; deep typing is follow-up
3. **One commit per module** — rollback-friendly, each commit leaves all gates green
4. **Shared tokens replace local tokens** — modules consume, never redefine
5. **Test preservation** — grocery's 1277-line app.test.js and certmachine's tests must keep passing

### Decision Drivers

1. **Module ordering** — already-TS modules first (less risk), then TS-conversion modules
2. **Token migration strategy** — search-and-replace local tokens with shared equivalents
3. **Bundle format** — IIFE→ESM conversion for certmachine/multissh (required for @shared external)

### Viable Options

**Option A: Per-module commits (CHOSEN)**
One commit per module adoption. Each commit is self-contained and independently
revertable. 8 commits total (6 modules + D-5 fix + deslop cleanup).
- Pro: Clean rollback, clear blame, reviewable diffs
- Con: More commits to manage

**Option B: Grouped commits (REJECTED)**
Group by type (all token migrations, then all hamburger adoptions, etc.)
- Pro: Smaller per-commit scope
- Con: Cross-module commits make rollback destructive; a token migration
  commit touching 6 modules can't be reverted for one module

---

## 3. Per-Module Adoption Specifications

### 3.1 C1: taskmaster — ThemeManager + HamburgerMenu + token retirement

**Current state:** 400-line main.ts has a hand-built hamburger (nav-menu
pattern with panel, toggles, sections). Uses local Obsidian-style tokens
(--bg-primary, --text-normal, etc.) in style.css (104 custom property refs).
Already has shared.css linked, data-theme="obsidian", sharedConsumer flag.
Local ui/modal.ts is a 2-line re-export shim from @shared already.

**Changes:**

1. **Token migration in style.css** — mechanical rename:
   | Local token | Shared token |
   |---|---|
   | --bg-primary | --color-bg |
   | --bg-secondary | --color-surface-1 |
   | --bg-tertiary | --color-surface-2 |
   | --bg-modifier-border | --color-border |
   | --text-normal | --color-text |
   | --text-muted | --color-text-muted |
   | --text-faint | --color-text-faint |
   | --text-accent | --color-primary |
   | --interactive-accent | --color-primary |
   | --interactive-accent-hover | --color-primary-hover |
   | --status-green | --color-success |
   | --status-red | --color-danger |
   | --status-yellow | --color-warning |
   | --status-blue | (keep as local --status-blue or map to --color-primary) |
   | --font-mono | --font-mono |
   | --font-ui | --font-body |
   | --radius | --radius-md |

   Delete the `:root { }` block defining these tokens (shared.css provides them).

2. **ThemeManager in main.ts** — add:
   ```ts
   import { ThemeManager, HamburgerMenu } from "@shared";
   const themes = new ThemeManager({ module: "taskmaster", default: "obsidian" });
   themes.apply();
   ```

3. **HamburgerMenu replaces hand-built nav-menu** — migrate the existing
   hamburger contents:
   - Nav links section (Metrics) → link items
   - Live toggle → render slot
   - Fallback interval select → render slot
   - Allow-sudo toggle → render slot with when() guard (auth-dependent)
   - Server status line → render slot
   - Logout icon → action item with when() guard
   - themePicker: true

4. **Delete local hamburger code** — the nav-menu build logic, panel, btn,
   ICON_MENU, the old menu section builders in main.ts.

5. **Switch modal import** — `main.ts:16` change from `'./ui/modal.js'` to
   `"@shared"`. Delete `web/taskmaster/js/ui/modal.ts` (the 2-line shim).

6. **Theme bootstrap in index.html** — add inline pre-paint script, same
   pattern as obsidianoid/todo.

7. **index.html updates** — add `<script type="module" src="js/shell.js">`
   OR integrate into existing main.ts (taskmaster already has a module entry).

**Touches:** web/taskmaster/style.css, web/taskmaster/js/main.ts,
web/taskmaster/index.html, web/taskmaster/js/ui/modal.ts (delete),
web/taskmaster/js/bundle.js (rebuild)

**Verification:**
- `npm run build` — bundle rebuilds
- `npm run typecheck` — no type errors
- `make test` — Go tests pass
- Visual: board renders correctly with obsidian theme, hamburger opens with
  all controls functional, theme picker works

---

### 3.2 C2: certmachine — ESM + sharedConsumer + adoption

**Current state:** TS/esbuild IIFE bundle. Has its own toast.ts (81 lines,
the original donor for the shared toast). Uses short-name tokens (--accent,
--bg, --border, --muted, --panel, --text). 159 CSS custom property refs in
cert.css. Imports CSS via esbuild.

**Changes:**

1. **Build descriptor** — in scripts/descriptors.mjs, change certmachine from
   `format: "iife"` to `format: "esm"`, add `sharedConsumer: true`.

2. **Token migration in cert.css** — map short names to shared tokens:
   | Local token | Shared token |
   |---|---|
   | --bg | --color-bg |
   | --panel | --color-surface-1 |
   | --panel-* variants | --color-surface-2/3 as appropriate |
   | --border | --color-border |
   | --text | --color-text |
   | --muted | --color-text-muted |
   | --accent | --color-primary |
   | --accent-hover | --color-primary-hover |
   | --green | --color-success |
   | --red | --color-danger |
   | --amber | --color-warning |
   | --purple | (keep as local or map) |

   Delete the `:root { }` / `[data-theme]` blocks defining these tokens.

3. **ThemeManager + HamburgerMenu in main.ts** — add imports from @shared,
   construct ThemeManager and HamburgerMenu with themePicker.

4. **Replace local toast.ts** — change imports from `"./toast"` to `"@shared"`
   (showToast). Delete `web/certmachine/js/toast.ts`.

5. **index.html** — add `<link href="/shared/dist/shared.css">` before module
   CSS, add `data-theme="dark"`, add theme bootstrap, change script to
   `type="module"`.

**Touches:** scripts/descriptors.mjs, web/certmachine/js/main.ts,
web/certmachine/js/cert.css, web/certmachine/js/toast.ts (delete),
web/certmachine/index.html, web/certmachine/js/bundle.js +
bundle.css (rebuild)

**Verification:**
- `npm run build` + `npm run typecheck` + `npm run test:web`
  (certmachine has generate.test.ts, listmodel.test.ts, status.test.ts)
- `make test` — Go tests pass
- Visual: cert list renders, toasts fire on actions, theme picker works

---

### 3.3 C3: multissh — ESM + sharedConsumer + xterm theme wiring

**Current state:** TS/esbuild IIFE bundle. Uses same short-name tokens as
certmachine (146 CSS refs). Has terminal.ts with xterm theme object that
reads CSS custom properties at runtime.

**Changes:**

1. **Build descriptor** — format:"esm", sharedConsumer:true.

2. **Token migration in ssh.css** — same mapping as certmachine (shared origin).

3. **ThemeManager + HamburgerMenu** — standard adoption.

4. **xterm theme wiring** — terminal.ts builds an xterm `ITheme` object from
   CSS custom properties. Update to read the shared token names. Wire
   ThemeManager.onChange to re-read and apply the xterm theme on theme switch.

5. **index.html** — shared.css link, data-theme, theme bootstrap, type="module".

**Touches:** scripts/descriptors.mjs, web/multissh/js/main.ts,
web/multissh/js/ssh.css, web/multissh/js/terminal.ts,
web/multissh/index.html, web/multissh/js/bundle.js + bundle.css (rebuild)

**Verification:**
- Build/typecheck/test gates
- Visual: SSH terminal renders correctly, terminal colors update on theme change
- sshcommand.test.ts passes

---

### 3.4 C4: admin — TS conversion + adoption

**Current state:** Plain JS (app.js, 776 lines). No CSS custom properties.
No theme. SPA with static handler + API routes.

**Changes:**

1. **TS conversion** — mechanical:
   - Create `web/admin/js/` directory
   - Move `web/admin/app.js` → `web/admin/js/main.ts` with minimal typing
     (add parameter types, fix any `any` that tsc catches)
   - Move `web/admin/style.css` → `web/admin/css/admin.css` (or leave in
     place and import via esbuild if simpler)

2. **Build descriptor** — add admin to descriptors.mjs:
   ```js
   { name: "admin", entry: ["web/admin/js/main.ts"], mode: "bundle",
     out: "web/admin/js/bundle.js", bundle: true, format: "esm",
     target: "es2020", sharedConsumer: true }
   ```

3. **tsconfig** — add `web/admin/js/**/*` to include if needed (check if
   existing globs already cover it).

4. **ThemeManager + HamburgerMenu** — standard adoption in main.ts.

5. **Token introduction** — admin currently uses hardcoded colors in CSS.
   Replace key colors with `var(--color-*)` references so themes apply.

6. **index.html** — shared.css link, data-theme="dark", theme bootstrap,
   `<script type="module" src="js/bundle.js">`.

**Touches:** web/admin/app.js (delete), web/admin/js/main.ts (new),
web/admin/style.css → web/admin/css/admin.css, web/admin/index.html,
scripts/descriptors.mjs, web/admin/js/bundle.js (new artifact)

**Verification:**
- Build/typecheck gates
- `make test` — admin Go tests pass
- Visual: admin pages render, theme picker works

---

### 3.5 C5: timetracker — TS conversion + FA removal + adoption

**Current state:** Plain JS (index.js 736 lines + TimeSelector.js 245 lines).
Broken FontAwesome (CSS file + ttf only, no woff2 — far glyphs 404).
Has marked.min.js (markdown library). No CSS custom properties.

**Changes:**

1. **TS conversion** — mechanical:
   - Create `web/timetracker/js/` directory
   - Move `web/timetracker/index.js` → `web/timetracker/js/main.ts`
   - Move `web/timetracker/TimeSelector.js` → `web/timetracker/js/time-selector.ts`
   - Move `web/timetracker/index.css` → `web/timetracker/css/timetracker.css`
   - Move `web/timetracker/TimeSelector.css` → `web/timetracker/css/time-selector.css`
   - Import CSS via esbuild (import statements in TS)
   - Handle marked.min.js — either bundle it or keep as external script

2. **FontAwesome removal** — delete:
   - `web/timetracker/fontawesome.min.css`
   - `web/timetracker/webfonts/fa-solid-900.ttf`
   - Replace 8 FA icon references in index.js with inline SVG or shared
     icon registry entries (fas fa-edit, fas fa-download, fas fa-copy)

3. **Build descriptor** — add timetracker to descriptors.mjs.

4. **Token introduction** — replace hardcoded colors with shared tokens.

5. **ThemeManager + HamburgerMenu** — standard adoption.

6. **index.html** — shared.css link, data-theme="dark", theme bootstrap,
   remove FA CSS link, type="module" script.

**Touches:** web/timetracker/index.js (delete), web/timetracker/TimeSelector.js
(delete), web/timetracker/js/main.ts (new), web/timetracker/js/time-selector.ts
(new), web/timetracker/fontawesome.min.css (delete), web/timetracker/webfonts/
(delete), web/timetracker/index.html, scripts/descriptors.mjs,
web/timetracker/js/bundle.js (new artifact)

**Verification:**
- Build/typecheck gates
- `make test` — timetracker Go tests pass
- Visual: time entries render, FA icons replaced with working alternatives,
  edit/download/copy buttons functional, theme picker works

---

### 3.6 C6: grocery — TS conversion + adoption + CDN removal

**Current state:** Plain JS (app.js, 1997 lines). Has app.test.js (1277
lines, run via esbuild-cjs harness). Uses `--color-*` tokens that closely
match the shared vocabulary (422 CSS refs). Uses Google Fonts CDN for DM Sans.

**Changes:**

1. **TS conversion** — mechanical:
   - Create `web/grocery/js/` directory
   - Move `web/grocery/app.js` → `web/grocery/js/main.ts`
   - Move `web/grocery/app.test.js` → `web/grocery/js/main.test.ts`
   - Move `web/grocery/style.css` → `web/grocery/css/grocery.css`
     (or `web/grocery/js/grocery.css` for esbuild CSS import)

2. **Token alignment** — grocery's tokens are already close to shared:
   | Local token | Shared token | Action |
   |---|---|---|
   | --color-bg | --color-bg | exact match, delete local def |
   | --color-border | --color-border | exact match |
   | --color-primary | --color-primary | exact match |
   | --color-text | --color-text | exact match |
   | --color-text-muted | --color-text-muted | exact match |
   | --color-text-faint | --color-text-faint | exact match |
   | --color-danger | --color-danger | exact match |
   | --color-surface | --color-surface-1 | rename |
   | --color-surface-off | --color-surface-2 | rename |
   | --color-surface-offset | --color-surface-3 | rename |
   | --color-primary-hov | --color-primary-hover | rename |
   | --color-primary-lite | --color-primary-tint | rename |
   | --color-danger-hov | (local, keep or extend shared) | evaluate |
   | --color-divider | --color-divider | exact match |
   | --font-body | --font-body | exact match |
   Module-specific tokens (--check-bg, --needed-bg, --color-nogroup-*,
   --footer-h, --header-h, etc.) stay local.

3. **Build descriptor** — add grocery with test runner descriptor.

4. **ThemeManager + HamburgerMenu** — standard adoption.

5. **Google Fonts CDN removal** — delete the `<link>` tags for
   fonts.googleapis.com / fonts.gstatic.com in index.html. DM Sans is not
   in the shared font set; if still needed, self-host under
   web/shared/public/fonts/ or replace with Inter (shared body font).

6. **index.html** — shared.css link, data-theme="dark", theme bootstrap,
   remove CDN links, type="module" script.

**Touches:** web/grocery/app.js (delete), web/grocery/app.test.js (move),
web/grocery/js/main.ts (new), web/grocery/js/main.test.ts (new),
web/grocery/style.css (move/merge), web/grocery/index.html,
scripts/descriptors.mjs, web/grocery/js/bundle.js (new artifact)

**Verification:**
- Build/typecheck gates
- `npm run test:web` — grocery test suite passes (critical: 1277-line test)
- `make test` — Go tests pass
- Visual: grocery list renders, drag-and-drop works, sections expand/collapse,
  theme picker works

---

### 3.7 C7: D-5 fix — obsidianoid thread-mode hamburger

**User directive (2026-09-16):** hamburger must be reachable in obsidianoid
thread mode. Currently the trigger (`#btn-hamburger`) sits inside
`#topbar-actions`, which is `display:none` in thread mode.

**Fix:** Move `#btn-hamburger` outside `#topbar-actions` in the thread-mode
DOM, or add a separate trigger mount point visible in thread mode. The
HamburgerMenu's `mountTrigger` option supports this — mount to a button
that's always visible regardless of mode.

**Touches:** web/obsidianoid/index.html (move trigger placement),
possibly web/obsidianoid/js/app.ts (adjust mount logic),
web/obsidianoid/js/app.js (rebuild)

**Verification:**
- Visual: hamburger visible and functional in both normal and thread mode
- Build/typecheck gates
- obsidianoid handler_test.go passes

---

### 3.8 C8: deslop — clean plan references in shared .ts sources

**Carry-forward from Phase 2:** ~45 plan-specific references (B-criteria,
§-sections, ADR numbers, FRD line references) in production .ts source files
(focusable.ts, menu.ts, theme.ts, toast.ts, modal.ts) could not be cleaned
because editing them would re-stamp the shared bundle. Phase 3 commits new
bundles for adopting modules, so the shared bundle stamp has already changed.

**Changes:** Strip plan provenance from web/shared/ts/*.ts source files —
same pattern as the Phase 2 test-file deslop but now applied to the
production sources. Keep useful architectural comments; remove only
plan-specific references.

**Touches:** web/shared/ts/focusable.ts, web/shared/ts/menu.ts,
web/shared/ts/theme.ts, web/shared/ts/toast.ts, web/shared/ts/modal.ts,
web/shared/dist/shared.mjs (rebuild)

**Verification:**
- All gates green (build/typecheck/test)
- Bundle-shape gate passes with new stamp

---

## 4. Commit Sequence

| Commit | Summary | Modules touched | Gate check |
|---|---|---|---|
| C1 | taskmaster: ThemeManager + HamburgerMenu + retire local tokens | taskmaster | full |
| C2 | certmachine: ESM + sharedConsumer + adoption + toast→shared | certmachine | full |
| C3 | multissh: ESM + sharedConsumer + adoption + xterm wiring | multissh | full |
| C4 | admin: TS conversion + build entry + adoption | admin | full |
| C5 | timetracker: TS conversion + FA removal + adoption | timetracker | full |
| C6 | grocery: TS conversion + adoption + CDN removal | grocery | full |
| C7 | D-5: obsidianoid thread-mode hamburger reachability | obsidianoid | full |
| C8 | deslop: clean plan references in shared .ts sources | shared | full |

Each commit leaves `npm run build`, `npm run typecheck`, `npm run test:web`,
`make test`, `gofmt -l .`, and `go vet ./...` all green.

Rollback: any single commit can be reverted independently (one module per
commit). C8 (deslop) is optional and can be dropped without affecting any
module's functionality.

---

## 5. Per-Step Verification Protocol

For each commit Cn:

1. **Pre-edit snapshot** — note current git status
2. **Make changes** per §3 specification
3. **Build** — `npm run build` (rebuilds all bundles)
4. **Typecheck** — `npm run typecheck` (tsc --noEmit)
5. **Web tests** — `npm run test:web` (all suites including module-specific)
6. **Go tests** — `make test` (go test -race ./...)
7. **Go lint** — `gofmt -l .` (no output) + `go vet ./...` (no errors)
8. **Bundle-shape gate** — `node scripts/gates/bundle-shape.mjs`
9. **Stage and commit** — stage rebuilt artifacts BEFORE running gates
   (scripts/gates/artifacts.mjs diffs worktree-vs-index)

---

## 6. FR-4 Preservation Checklist

For each adopted module, verify that EVERY pre-existing control is reachable
and functional after migration:

### taskmaster
- [ ] Board view renders with correct colors (obsidian theme)
- [ ] Lane headers, task cards, status badges styled correctly
- [ ] Live/pause toggle functional in hamburger
- [ ] Fallback interval select functional in hamburger
- [ ] Allow-sudo toggle functional (when auth enabled)
- [ ] Server status line visible in hamburger
- [ ] Logout action functional (when auth enabled)
- [ ] Metrics page accessible via nav link
- [ ] confirmDialog still works (modal import switched)
- [ ] Theme picker in hamburger, all 8 themes apply correctly

### certmachine
- [ ] Certificate list renders
- [ ] Certificate detail view works
- [ ] Generate wizard functional
- [ ] Toast notifications fire on actions (success/error)
- [ ] Tools menu entries accessible (if moved to hamburger)
- [ ] Theme picker in hamburger, themes apply

### multissh
- [ ] Host list renders
- [ ] SSH terminal opens and connects
- [ ] Terminal colors correct under each theme
- [ ] Terminal colors update on theme switch (xterm re-theme)
- [ ] Tab bar functional
- [ ] File/dir/key pickers functional
- [ ] Theme picker in hamburger

### admin
- [ ] Admin panel renders
- [ ] All existing controls functional
- [ ] Theme picker in hamburger

### timetracker
- [ ] Time entries render
- [ ] Edit buttons work (was FA fa-edit, now SVG)
- [ ] Download button works (was FA fa-download)
- [ ] Copy button works (was FA fa-copy)
- [ ] TimeSelector component functional
- [ ] Markdown rendering works (marked.min.js)
- [ ] Theme picker in hamburger

### grocery
- [ ] Grocery list renders with sections
- [ ] Drag-and-drop reordering works
- [ ] Section expand/collapse works
- [ ] Add/edit/delete items functional
- [ ] Check/uncheck items functional
- [ ] Progress indicators render correctly
- [ ] app.test.js passes (1277 lines of coverage)
- [ ] Theme picker in hamburger

---

## 7. Hard Constraints

1. Zero functionality or visual regression in modules NOT named above
2. `npm run build`, `npm run typecheck`, `npm run test:web`, `make test`
   all pass at every commit
3. `gofmt -l .` + `go vet ./...` clean at every commit
4. No CDN/external assets (apps run on closed LANs)
5. Generated artifacts (web/*/js/bundle.js, web/shared/dist/*) remain
   byte-identity gated via scripts/gates/bundle-shape.mjs
6. FRD §7 Resolved Decisions are SETTLED — never reopen
7. Never push, never touch main

---

## 8. Risk Assessment

| Risk | Likelihood | Mitigation |
|---|---|---|
| Token migration misses a color reference → visual glitch | Medium | grep for all local token names post-migration; visual spot-check per theme |
| IIFE→ESM conversion breaks certmachine/multissh at runtime | Low | Both already use `import` statements; the IIFE wrapping is esbuild's output format only |
| grocery test suite breaks during TS conversion | Medium | Mechanical rename first; keep test in CJS runner format; add test descriptor |
| xterm theme object reads wrong CSS property names | Medium | Unit test the property name mapping; visual terminal check |
| timetracker marked.min.js import breaks | Low | Keep as separate `<script>` tag if bundling is complex; bundle if straightforward |
| D-5 fix breaks obsidianoid normal mode | Low | Test both modes; the trigger move is DOM-only, no logic change |
