# Phase 3 Plan — UI Unification: Adopt in Remaining Vanilla Modules

Status: **APPROVED — Architect APPROVE + Critic ACCEPT-WITH-RESERVATIONS, consensus round 1**
FRD: docs/FRD-ui-unification.md §6 row 3
Branch: ui-upgrade (never push, never touch main)
Predecessor: Phase 2 COMPLETE (10 commits 063f594..0726794)

---

## 1. Scope

Adopt the shared components (ThemeManager, HamburgerMenu, Toast, shared.css
tokens/themes) in the 6 modules not yet touched:

| Module | Current state | Token state | Adoption scope |
|---|---|---|---|
| taskmaster | TS/ESM, sharedConsumer, shared.css linked, own hamburger, own Obsidian-style tokens | 17 local tokens in `:root`, 141 `var(--` refs | Retire local tokens, replace hamburger with HamburgerMenu, switch modal import to @shared direct |
| certmachine | TS/esbuild IIFE, own toast.ts, short-name tokens (--bg, --panel, --text, etc.) | 12 local tokens in `:root`, 125 `var(--` refs, 14 hex literals | ESM conversion, sharedConsumer, shared.css, ThemeManager + HamburgerMenu, replace local toast, map local tokens to shared |
| multissh | TS/esbuild IIFE, short-name tokens, xterm theme hardcoded | 11 local tokens in `:root`, 127 `var(--` refs, 12 hex literals | ESM conversion, sharedConsumer, shared.css, ThemeManager + HamburgerMenu, map local tokens to shared, wire xterm to ThemeManager |
| admin | Plain JS (776 lines), no CSS tokens, 32 hex literals | 0 tokens, 32 hex literals in style.css | TS conversion, build entry, sharedConsumer, shared.css, ThemeManager + HamburgerMenu, introduce tokens for hex literals |
| timetracker | Plain JS (736+245 lines), broken FA, no CSS tokens | 0 tokens, 6 hex literals in index.css | TS conversion, build entry, sharedConsumer, shared.css, ThemeManager + HamburgerMenu, remove FA, introduce tokens |
| grocery | Plain JS (1997 lines, IIFE-wrapped), Google Fonts CDN, own `--color-*` / structural tokens | ~45 local tokens in `:root`, 372 `var(--` refs, 58 hex literals | TS conversion, IIFE unwrap, build entry, sharedConsumer, shared.css, ThemeManager + HamburgerMenu, align tokens to shared, remove CDN |

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
5. **Test preservation** — grocery's 1277-line app.test.js and certmachine's 3 test suites must keep passing

### Decision Drivers

1. **Module ordering** — already-TS modules first (less risk), then TS-conversion modules
2. **Token migration strategy** — for modules WITH local tokens (taskmaster, certmachine, multissh, grocery): mechanical rename. For modules WITHOUT tokens (admin, timetracker): introduce `var(--color-*)` for key hex literals, guided by visual role
3. **Bundle format** — IIFE to ESM conversion for certmachine/multissh (required for @shared external)

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

## 3. Build-System Progression

### 3.1 EXPECTED_ARTIFACT_COUNT

Current: 16 (11 descriptors, some emitting both .js and .css).

certmachine and multissh already have descriptors (IIFE, emitsCss). Changing
format to "esm" and adding sharedConsumer does NOT change their artifact count
(they already emit bundle.js + bundle.css each). The new descriptors are:

| Commit | Descriptor added | emitsCss? | New count |
|---|---|---|---|
| C1 (taskmaster) | — (already exists) | no | 16 |
| C2 (certmachine) | — (modified, not new) | yes (already) | 16 |
| C3 (multissh) | — (modified, not new) | yes (already) | 16 |
| C4 (admin) | admin: bundle.js | no (standalone `<link>`) | 17 |
| C5 (timetracker) | timetracker: bundle.js | no (standalone `<link>`) | 18 |
| C6 (grocery) | grocery: bundle.js | no (standalone `<link>`) | 19 |

Decision: admin, timetracker, and grocery keep CSS as standalone `<link>` tags
(no emitsCss). Rationale: these modules have standalone CSS files already loaded
via `<link>`, and converting to esbuild CSS imports is unnecessary churn. This
gives deterministic artifact counts: C4=17, C5=18, C6=19.

### 3.2 bundle-shape --require progression

Current Makefile line:
```
node scripts/gates/bundle-shape.mjs --require=sampler,taskmaster,obsidianoid,todo
```

Progression:
| After commit | --require value |
|---|---|
| C2 | sampler,taskmaster,obsidianoid,todo,certmachine |
| C3 | sampler,taskmaster,obsidianoid,todo,certmachine,multissh |
| C4 | + admin |
| C5 | + timetracker |
| C6 | + grocery |

Each commit that adds `sharedConsumer: true` to a descriptor also appends
that module to the `--require` list in the Makefile `gates` target.

### 3.3 tsconfig include progression

Current includes (from tsconfig.json):
```
web/obsidianoid/js/*.ts, web/slideshow/js/*.ts, web/multissh/js/*.ts,
web/certmachine/js/*.ts, web/taskmaster/js/**/*.ts, web/smbedit/src/**/*.ts,
web/smbedit/src/**/*.tsx, web/issuetracker/src/**/*.ts,
web/issuetracker/src/**/*.tsx, web/shared/**/*.ts, web/sampler/js/*.ts,
web/todo/js/*.ts
```

Additions:
| Commit | Include added |
|---|---|
| C4 | `web/admin/js/**/*.ts` |
| C5 | `web/timetracker/js/**/*.ts` |
| C6 | `web/grocery/js/**/*.ts` |

---

## 4. Per-Module Adoption Specifications

**"Standard adoption"** — shorthand used in C3-C6 specifications. It means
applying ALL of the following to a module's index.html and entry script:

(a) Add `<link href="/shared/dist/shared.css">` before the module's own CSS
    `<link>` tag in index.html
(b) Add `data-theme="dark"` on the `<html>` element
(c) Add the theme bootstrap inline script (pre-paint, same pattern as
    obsidianoid/todo) in `<head>`
(d) Construct `ThemeManager` with the module name and default theme:
    `new ThemeManager({ module: "<name>", default: "dark" })`
(e) Construct `HamburgerMenu` with `themePicker: true` and migrate existing
    nav/actions into hamburger items
(f) Change the main `<script>` tag to `type="module"` pointing to the
    esbuild-produced bundle

### 4.1 C1: taskmaster — ThemeManager + HamburgerMenu + token retirement

**Current state:** 399-line main.ts has a hand-built hamburger (buildMenu() at
lines 153-257: dropdown panel with hidden toggle). Uses 17 local Obsidian-style
tokens in style.css `:root` block (--bg-primary, --bg-secondary, etc.) with
141 var(--) references. Already has shared.css linked, data-theme="obsidian",
sharedConsumer flag. Local ui/modal.ts is a 2-line re-export shim from @shared.

**Changes:**

1. **Token migration in style.css** — mechanical rename of 141 var() references:
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
   | --status-blue | keep as local (no shared equivalent; used for info state) |
   | --font-mono | --font-mono |
   | --font-ui | --font-body |
   | --radius | --radius-md |

   Delete the `:root { }` block defining these tokens (shared.css provides them).
   Keep `--status-blue` as a local definition if still referenced.

2. **ThemeManager in main.ts** — add:
   ```ts
   import { ThemeManager, HamburgerMenu } from "@shared";
   const themes = new ThemeManager({ module: "taskmaster", default: "obsidian" });
   themes.apply();
   ```

3. **HamburgerMenu replaces buildMenu()** (lines 153-257) — migrate contents:
   - Nav links section (Metrics) -> link items
   - Live toggle -> render slot
   - Fallback interval select -> render slot
   - Allow-sudo toggle -> render slot with when() guard (auth-dependent)
   - Server status line -> render slot
   - Logout icon -> action item with when() guard
   - themePicker: true

4. **Delete local hamburger code** — buildMenu(), panel, btn, ICON_MENU,
   old menu section builders in main.ts.

5. **Switch modal import** — main.ts line 16: change from `'./ui/modal.js'` to
   `"@shared"`. Delete `web/taskmaster/js/ui/modal.ts` (the 2-line shim).

6. **Theme bootstrap in index.html** — add inline pre-paint script, same
   pattern as obsidianoid/todo.

**Descriptor change:** none (taskmaster is already ESM + sharedConsumer).

**Touches:** web/taskmaster/style.css, web/taskmaster/js/main.ts,
web/taskmaster/index.html, web/taskmaster/js/ui/modal.ts (delete),
web/taskmaster/js/bundle.js (rebuild)

**Gate criteria:**
- `npm run build` — bundle rebuilds, 16 artifacts
- `npm run typecheck` — no type errors
- `npm run test:web` — 9 suites pass
- `make test` — Go tests pass
- `node scripts/gates/bundle-shape.mjs --require=sampler,taskmaster,obsidianoid,todo` — pass
- Visual: board renders with obsidian theme, hamburger opens with all controls

---

### 4.2 C2: certmachine — ESM + sharedConsumer + adoption

**Current state:** TS/esbuild IIFE bundle. Has its own toast.ts (81 lines,
the original donor for the shared toast). Uses 12 short-name local tokens
(--bg, --panel, --panel-2, --border, --text, --muted, --accent, --accent-hover,
--green, --amber, --red, --purple) defined in cert.css `:root` block with
125 var(--) references and 14 residual hex literals.

**Changes:**

1. **Build descriptor** — in scripts/descriptors.mjs, change certmachine:
   ```js
   {
     name: "certmachine",
     entry: ["web/certmachine/js/main.ts"],
     mode: "bundle",
     out: "web/certmachine/js/bundle.js",
     bundle: true,
     format: "esm",         // was: "iife"
     target: "es2020",
     emitsCss: true,
     sharedConsumer: true,   // NEW
   }
   ```

2. **Token migration in cert.css** — rename 125 var() references:
   | Local token | Shared token |
   |---|---|
   | --bg | --color-bg |
   | --panel | --color-surface-1 |
   | --panel-2 | --color-surface-2 |
   | --border | --color-border |
   | --text | --color-text |
   | --muted | --color-text-muted |
   | --accent | --color-primary |
   | --accent-hover | --color-primary-hover |
   | --green | --color-success |
   | --red | --color-danger |
   | --amber | --color-warning |
   | --purple | keep as local (no shared equivalent) |

   Delete the `:root { }` block. Keep --purple as a local definition if needed.
   For the 14 residual hex literals: replace those with visual-role-appropriate
   shared tokens where the role is clear (e.g. a background hex matching the
   --bg value becomes `var(--color-bg)`); leave ambiguous ones as-is for now.

3. **ThemeManager + HamburgerMenu in main.ts** — add imports from @shared,
   construct ThemeManager and HamburgerMenu with themePicker.

4. **Replace local toast.ts** — change imports from `"./toast"` to `"@shared"`
   (showToast). Before deleting `web/certmachine/js/toast.ts`, verify that the
   3 test files (generate.test.ts, listmodel.test.ts, status.test.ts) do NOT
   import from `./toast`. If they do, update those imports to `@shared` as well.
   Then delete `web/certmachine/js/toast.ts`.

5. **index.html** — add `<link href="/shared/dist/shared.css">` before module
   CSS, add `data-theme="dark"`, add theme bootstrap, change script to
   `type="module"`.

6. **Makefile** — append `certmachine` to `--require` list.

**Touches:** scripts/descriptors.mjs, web/certmachine/js/main.ts,
web/certmachine/js/cert.css, web/certmachine/js/toast.ts (delete),
web/certmachine/index.html, web/certmachine/js/bundle.js +
bundle.css (rebuild), Makefile

**Gate criteria:**
- `npm run build` — 16 artifacts (count unchanged)
- `npm run typecheck` — no type errors
- `npm run test:web` — 9 suites pass (certmachine has generate, listmodel, status tests)
- `make test` — Go tests pass
- `--require=sampler,taskmaster,obsidianoid,todo,certmachine` — pass
- Visual: cert list renders, toasts fire on actions, theme picker works

---

### 4.3 C3: multissh — ESM + sharedConsumer + xterm theme wiring

**Current state:** TS/esbuild IIFE bundle. Uses 11 short-name local tokens
(--bg, --panel, --panel-2, --border, --text, --muted, --accent, --accent-hover,
--green, --amber, --red) defined in ssh.css `:root` block with 127 var(--)
references and 12 residual hex literals. terminal.ts line 33 has a hardcoded
xterm theme: `theme: { background: "#0b0f17" }` — this does NOT read CSS
custom properties; it is a static hex literal.

**Changes:**

1. **Build descriptor** — in scripts/descriptors.mjs, change multissh:
   ```js
   {
     name: "multissh",
     entry: ["web/multissh/js/main.ts"],
     mode: "bundle",
     out: "web/multissh/js/bundle.js",
     bundle: true,
     format: "esm",         // was: "iife"
     target: "es2020",
     emitsCss: true,
     sharedConsumer: true,   // NEW
   }
   ```

2. **Token migration in ssh.css** — rename 127 var() references (same-origin
   short-name scheme as certmachine):
   | Local token | Shared token |
   |---|---|
   | --bg | --color-bg |
   | --panel | --color-surface-1 |
   | --panel-2 | --color-surface-2 |
   | --border | --color-border |
   | --text | --color-text |
   | --muted | --color-text-muted |
   | --accent | --color-primary |
   | --accent-hover | --color-primary-hover |
   | --green | --color-success |
   | --red | --color-danger |
   | --amber | --color-warning |

   Delete the `:root { }` block. Handle 12 residual hex literals same as C2.

3. **ThemeManager + HamburgerMenu** — standard adoption.

4. **xterm theme wiring in terminal.ts** — the current code at line 33 is:
   ```ts
   theme: { background: "#0b0f17" }
   ```
   This is a hardcoded hex, NOT a CSS variable read. Replace with a helper
   that reads shared tokens from the computed style:
   ```ts
   function resolveXtermTheme(): ITheme {
     const s = getComputedStyle(document.documentElement);
     return {
       background: s.getPropertyValue("--color-bg").trim(),
       foreground: s.getPropertyValue("--color-text").trim(),
       cursor: s.getPropertyValue("--color-primary").trim(),
       selectionBackground: s.getPropertyValue("--color-primary-tint").trim(),
     };
   }
   ```
   Wire theme changes via the ThemeManager constructor's `onChange` option
   (note: `onChange` is a constructor option on `ThemeManagerOptions`
   (theme.ts:47), NOT a method on the ThemeManager class):
   ```ts
   const themes = new ThemeManager({
     module: "multissh",
     default: "dark",
     onChange: () => { term.options.theme = resolveXtermTheme(); },
   });
   ```

5. **index.html** — shared.css link, data-theme="dark", theme bootstrap, type="module".

6. **Makefile** — append `multissh` to `--require` list.

**Touches:** scripts/descriptors.mjs, web/multissh/js/main.ts,
web/multissh/js/ssh.css, web/multissh/js/terminal.ts,
web/multissh/index.html, web/multissh/js/bundle.js + bundle.css (rebuild),
Makefile

**Gate criteria:**
- `npm run build` — 16 artifacts
- `npm run typecheck` — no type errors
- `npm run test:web` — 9 suites pass (sshcommand.test.ts)
- `make test` — Go tests pass
- `--require=sampler,taskmaster,obsidianoid,todo,certmachine,multissh` — pass
- Visual: SSH terminal renders correctly, terminal colors update on theme change

---

### 4.4 C4: admin — TS conversion + adoption

**Current state:** Plain JS (app.js, 776 lines). style.css has 305 lines with
32 hex color literals and ZERO CSS custom properties. No build descriptor,
not in tsconfig. HTML has a 148-line topbar with h1 + logout and 6 panels.

**Changes:**

1. **TS conversion** — mechanical:
   - Create `web/admin/js/` directory
   - Move `web/admin/app.js` -> `web/admin/js/main.ts` with minimal typing
     (add parameter types, fix any `any` that tsc catches)

2. **Build descriptor** — add to scripts/descriptors.mjs:
   ```js
   {
     name: "admin",
     entry: ["web/admin/js/main.ts"],
     mode: "bundle",
     out: "web/admin/js/bundle.js",
     bundle: true,
     format: "esm",
     target: "es2020",
     sharedConsumer: true,
   }
   ```
   Update `EXPECTED_ARTIFACT_COUNT` to 17.

3. **tsconfig** — add `web/admin/js/**/*.ts` to include array.

4. **Token introduction in style.css** — admin has 0 CSS custom properties and
   32 hex color literals. This is NOT a rename; it is introducing var()
   references for the first time. Strategy: inspect each hex literal's visual
   role (background, text, border, accent) and replace with the closest shared
   token. Judgment-based per value — the executor maps by role, not by
   mechanical hex matching. Expected coverage: ~20-25 of 32 hex values
   replaceable; a few (box-shadow colors, gradient stops) may stay as hex.

5. **ThemeManager + HamburgerMenu** — standard adoption. Replace the existing
   topbar h1 + logout with HamburgerMenu (move logout to hamburger action).

6. **index.html** — shared.css link, data-theme="dark", theme bootstrap,
   `<script type="module" src="js/bundle.js">`, delete old `<script src="app.js">`.

7. **Makefile** — append `admin` to `--require` list.

**Touches:** web/admin/app.js (delete), web/admin/js/main.ts (new),
web/admin/style.css (edit in place), web/admin/index.html,
scripts/descriptors.mjs, tsconfig.json, Makefile,
web/admin/js/bundle.js (new artifact)

**Gate criteria:**
- `npm run build` — 17 artifacts
- `npm run typecheck` — no type errors (admin now in tsconfig)
- `npm run test:web` — 9 suites pass
- `make test` — Go tests pass (admin handler_test.go)
- `--require` list includes admin — pass

---

### 4.5 C5: timetracker — TS conversion + FA removal + adoption

**Current state:** Plain JS (index.js 736 lines + TimeSelector.js 245 lines).
Broken FontAwesome (fontawesome.min.css 59841 bytes + webfonts/fa-solid-900.ttf
426KB — far glyphs 404). Has marked.min.js (markdown library). index.css has
211 lines with only 6 hex literals and 0 CSS custom properties. HTML already
uses type="module".

**Changes:**

1. **TS conversion** — mechanical:
   - Create `web/timetracker/js/` directory
   - Move `web/timetracker/index.js` -> `web/timetracker/js/main.ts`
   - Move `web/timetracker/TimeSelector.js` -> `web/timetracker/js/time-selector.ts`
   - Handle marked.min.js — keep as external `<script>` tag (bundling a
     vendored minified file adds complexity for no gain)

2. **FontAwesome removal** — delete:
   - `web/timetracker/fontawesome.min.css` (59841 bytes)
   - `web/timetracker/webfonts/fa-solid-900.ttf` (426KB)
   - Replace 8 FA icon references with inline SVG (fas fa-edit, fas fa-download,
     fas fa-copy, and others). Use simple path-only SVGs, same pattern as
     shared menu.ts icons.

3. **Build descriptor** — add to scripts/descriptors.mjs:
   ```js
   {
     name: "timetracker",
     entry: ["web/timetracker/js/main.ts"],
     mode: "bundle",
     out: "web/timetracker/js/bundle.js",
     bundle: true,
     format: "esm",
     target: "es2020",
     sharedConsumer: true,
   }
   ```
   Update `EXPECTED_ARTIFACT_COUNT` to 18.

4. **tsconfig** — add `web/timetracker/js/**/*.ts` to include array.

5. **Token introduction in CSS** — only 6 hex literals. Replace with shared
   tokens where role is clear. The CSS files are light (211 + TimeSelector.css)
   so this is straightforward.

6. **ThemeManager + HamburgerMenu** — standard adoption.

7. **index.html** — shared.css link, data-theme="dark", theme bootstrap,
   remove FA CSS link, update script src to bundle.js.

8. **Makefile** — append `timetracker` to `--require` list.

**Touches:** web/timetracker/index.js (delete), web/timetracker/TimeSelector.js
(delete), web/timetracker/js/main.ts (new), web/timetracker/js/time-selector.ts
(new), web/timetracker/fontawesome.min.css (delete), web/timetracker/webfonts/
(delete), web/timetracker/index.html, scripts/descriptors.mjs, tsconfig.json,
Makefile, web/timetracker/js/bundle.js (new artifact)

**Gate criteria:**
- `npm run build` — artifact count updated
- `npm run typecheck` — no type errors (timetracker now in tsconfig)
- `npm run test:web` — 9 suites pass
- `make test` — Go tests pass
- `--require` list includes timetracker — pass
- Visual: FA icons replaced with working SVG alternatives

---

### 4.6 C6: grocery — TS conversion + adoption + CDN removal + test adaptation

**Current state:** Plain JS (app.js, 1997 lines). style.css has 1406 lines
with ~45 local CSS custom properties defined in `:root` (including --color-bg,
--color-surface, --color-border, --color-text, --color-primary, --font-body,
structural tokens, and module-specific tokens like --needed-bg, --check-bg,
--color-nogroup-*) with 372 var(--) references and 58 hex literals. Uses
Google Fonts CDN for DM Sans. app.test.js (1277 lines) uses Node built-in
test runner with `readFileSync` to read the shipped `app.js` source and
performs mirror-integrity checks against function bodies.

**CRITICAL: Test adaptation strategy**

The grocery test suite (`app.test.js`, 1277 lines) has a specific fragility:

```js
const APP_SRC = readFileSync(join(import.meta.dirname, 'app.js'), 'utf8');
const CSS_SRC = readFileSync(join(import.meta.dirname, 'style.css'), 'utf8');
```

The mirror-integrity block (lines 788-823) reads the shipped `app.js` source
text, normalizes it, and asserts that inline copies of 14 helper functions
match the shipped versions. When `app.js` is renamed to `js/main.ts` and
bundled to `js/bundle.js`, two things break: (1) the `readFileSync('app.js')`
path, and (2) the `norm()` comparator — Node's type stripping replaces type
annotations with whitespace padding in `fn.toString()` output, while
`readFileSync` returns raw TS with annotations intact (see ADR-017).

**Adaptation approach:**
1. The test file moves to `web/grocery/js/main.test.ts`
2. The `readFileSync` path must be updated to read the NEW source file:
   `readFileSync(join(import.meta.dirname, 'main.ts'), 'utf8')` — the test
   inspects source, not the bundle
3. The CSS path similarly: `readFileSync(join(import.meta.dirname, '..', 'style.css'), 'utf8')`
   (or wherever the CSS lives post-move)
4. The `norm()` function must be updated to handle the type-stripping
   mismatch. After stripping leading indent, also strip TS type annotations
   and collapse whitespace runs so both `fn.toString()` output (whitespace-
   padded) and raw TS source (with annotations) normalize to the same text:
   ```js
   function norm(s) {
     return s
       .replace(/^[ \t]+/gm, '')                                      // strip leading indent
       .replace(/:\s*[A-Za-z_][\w\[\]|&<>, ]*(?=\s*[,)={}])/g, '')   // strip type annotations
       .replace(/\s+/g, ' ')                                          // collapse whitespace
       .trim();
   }
   ```
5. The test runner entry in `scripts/test-web.mjs` must update:
   - Change `entry: "web/grocery/app.test.js"` to `entry: "web/grocery/js/main.test.ts"`
   - Runner stays `"node-test"` (the test uses import.meta.dirname which
     cannot go through esbuild-cjs)
6. The `APP_SRC.match(/mirrored in app\.test\.js ::/g)` tag-counting assertion
   (line 820) must update the regex to match the new test filename

**Changes:**

**C6 INTERNAL CHECKPOINT PROTOCOL** (3-phase execution within this commit):

   **Checkpoint 1 — File moves + path updates (still JS):**
   Create `web/grocery/js/`, move app.js → js/main.js (NOT .ts yet),
   move app.test.js → js/main.test.js, update readFileSync paths in test,
   update test-web.mjs entry, run `npm run test:web` to confirm all grocery
   tests pass on the STILL-JS source. This isolates path breakage from TS
   conversion breakage.

   **Checkpoint 2 — TS conversion + norm() update:**
   Rename main.js → main.ts, main.test.js → main.test.ts, remove IIFE
   wrapper, add minimal type annotations, update norm() with type-stripping
   regex, run each of the 14 mirror-integrity assertions individually to
   confirm norm() handles all function signatures. Also verify the 3
   non-mirror-integrity norm() call sites (lines ~1092, ~1096, ~1109 in
   the moved test) remain compatible.

   **Checkpoint 3 — Token alignment + adoption + CDN removal:**
   Token migration (~45 defs, 372 var refs, 58 hex), ThemeManager +
   HamburgerMenu adoption, Google Fonts CDN removal, visual verification
   under all 8 themes.

1. **TS conversion + IIFE unwrapping** — `web/grocery/app.js` is wrapped in
   an IIFE (`(() => {` at line 1, `})();` at line 1997). Converting to ESM
   requires removing this wrapper and promoting all closure-scoped `let`/
   `const`/`function` declarations to module scope. This is safe because the
   file is bundled as a single ESM entry point (module scope provides the
   same encapsulation the IIFE provided).
   - Create `web/grocery/js/` directory
   - Move `web/grocery/app.js` -> `web/grocery/js/main.ts`
   - Remove the IIFE wrapper (opening `(() => {` and closing `})();`)
   - Move `web/grocery/app.test.js` -> `web/grocery/js/main.test.ts`
   - style.css stays at `web/grocery/style.css` (simplest; CSS import via
     `<link>` in HTML, not esbuild)

2. **Token alignment** — grocery's tokens overlap significantly with shared:
   | Local token | Shared token | Action |
   |---|---|---|
   | --color-bg | --color-bg | exact match, delete local def |
   | --color-surface | --color-surface-1 | rename refs |
   | --color-surface-off | --color-surface-2 | rename refs |
   | --color-border | --color-border | exact match |
   | --color-divider | --color-divider | exact match |
   | --color-text | --color-text | exact match |
   | --color-text-muted | --color-text-muted | exact match |
   | --color-text-faint | --color-text-faint | exact match |
   | --color-primary | --color-primary | exact match |
   | --color-primary-hov | --color-primary-hover | rename refs |
   | --color-primary-lite | --color-primary-tint | rename refs |
   | --color-danger | --color-danger | exact match |
   | --font-body | --font-body | exact match (DM Sans def removed; shared's Inter takes over) |

   **Keep as local** (module-specific semantics):
   --color-danger-hov, --color-danger-lite, --color-warning-lite,
   --color-warning-bdr, --color-nogroup-bg/bdr/txt, --needed-bg/txt,
   --check-bg/txt, --notneed-bg/txt, --header-h, --footer-h

   **Structural tokens** — grocery defines its own --radius-*, --shadow-*,
   --space-*, --text-*, --t. Where names match shared exactly (most do),
   delete the local def and let shared.css provide. Where grocery uses a
   value shared doesn't have (--radius-xl, --shadow-lg), keep as local.

   Delete the `:root { }` block's shared-matching definitions. Keep
   module-specific definitions in a local `:root` block.

   For the 58 hex literals: replace those with visual-role-appropriate shared
   tokens where clear; leave ambiguous ones.

3. **Build descriptor** — add to scripts/descriptors.mjs:
   ```js
   {
     name: "grocery",
     entry: ["web/grocery/js/main.ts"],
     mode: "bundle",
     out: "web/grocery/js/bundle.js",
     bundle: true,
     format: "esm",
     target: "es2020",
     sharedConsumer: true,
   }
   ```
   Update `EXPECTED_ARTIFACT_COUNT` to 19.

4. **tsconfig** — add `web/grocery/js/**/*.ts` to include array.

5. **Test adaptation** — per the strategy above. Update paths in the test file
   and in `scripts/test-web.mjs`. Run the test suite to confirm mirror
   integrity passes against the renamed source.

6. **ThemeManager + HamburgerMenu** — standard adoption. Grocery has a complex
   header with tabs, progress bar, icon buttons, and footer. The hamburger
   supplements rather than replaces the tab bar — it provides theme picker
   and any settings/actions that don't fit the existing header.

7. **Google Fonts CDN removal** — delete the `<link>` tags for
   fonts.googleapis.com/fonts.gstatic.com in index.html. DM Sans is replaced
   by shared's Inter (--font-body). If DM Sans is strongly preferred, self-host
   under web/shared/public/fonts/ as a follow-up.

8. **index.html** — shared.css link, data-theme="dark", theme bootstrap,
   remove CDN links, type="module" script pointing to bundle.js.

9. **Makefile** — append `grocery` to `--require` list.

**Touches:** web/grocery/app.js (delete), web/grocery/app.test.js (delete),
web/grocery/js/main.ts (new), web/grocery/js/main.test.ts (new),
web/grocery/style.css (edit), web/grocery/index.html,
scripts/descriptors.mjs, scripts/test-web.mjs, tsconfig.json, Makefile,
web/grocery/js/bundle.js (new artifact)

**Gate criteria:**
- `npm run build` — artifact count updated
- `npm run typecheck` — no type errors (grocery now in tsconfig)
- `npm run test:web` — ALL suites pass, especially grocery mirror-integrity
- `make test` — Go tests pass
- `--require` list includes grocery — pass
- Visual: grocery list renders, drag-and-drop works, sections expand/collapse

---

### 4.7 C7: D-5 fix — obsidianoid thread-mode hamburger

**User directive (2026-09-16):** hamburger must be reachable in obsidianoid
thread mode. Currently the trigger (`#btn-hamburger`) sits inside
`#topbar-actions`, which is `display:none` in thread mode.

**Current DOM problem:** `<button id="btn-hamburger">` (index.html:54) is
INSIDE `<div id="topbar-actions">` (line 32). Thread mode hides
`#topbar-actions` via `display:none` (app.css:441), which makes the hamburger
unreachable. Note: app.ts:490 has a stale comment claiming the trigger
"lives OUTSIDE #topbar-actions" — this is wrong and must be corrected.

**Fix — exact DOM change:**
1. Move `<button id="btn-hamburger">` in index.html to be a direct child of
   `#topbar` (sibling of `#topbar-actions`), NOT inside `#topbar-actions`
2. The existing `mountTrigger` wiring at app.ts:498 already adopts the button
   in place, so NO JavaScript change is needed for the mount logic
3. Correct the stale comment at app.ts:490 to accurately describe the trigger's
   new position
4. Add CSS for `#btn-hamburger` positioning as a direct child of `#topbar` if
   the layout requires adjustment (flexbox order or margin)

**Touches:** web/obsidianoid/index.html (move button element),
web/obsidianoid/js/app.ts (fix stale comment at line 490),
web/obsidianoid/css/app.css (positioning if needed),
web/obsidianoid/js/app.js + bundle.css (rebuild)

**Gate criteria:**
- `npm run build` — pass
- `npm run typecheck` — pass
- `npm run test:web` — pass
- `make test` — pass (obsidianoid handler_test.go)
- Visual: hamburger visible and functional in BOTH normal and thread mode
- Verify: switching to thread mode does NOT hide the hamburger button

---

### 4.8 C8: deslop — clean plan references in shared .ts sources

**Carry-forward from Phase 2:** ~45 plan-specific references (B-criteria,
S-sections, ADR numbers, FRD line references, "Phase 2" mentions,
PLAN-ui-unification references) in production .ts source files (focusable.ts,
menu.ts, theme.ts, toast.ts, modal.ts) could not be cleaned because editing
them would re-stamp the shared bundle. Phase 3 commits new bundles for
adopting modules, so the shared bundle stamp has already changed.

**Changes:** Strip plan provenance from web/shared/ts/*.ts source files —
keep useful architectural comments; remove only plan-specific references
(B3.x, B4.x, section numbers, ADR cross-refs, FRD line numbers).

**Touches:** web/shared/ts/focusable.ts, web/shared/ts/menu.ts,
web/shared/ts/theme.ts, web/shared/ts/toast.ts, web/shared/ts/modal.ts,
web/shared/dist/shared.mjs (rebuild)

**Cascade:** Since all sharedConsumer modules hash shared sources in their
content digests, editing web/shared/ts/*.ts re-stamps ALL consumer bundles:
taskmaster (bundle.js), sampler (bundle.js), obsidianoid (app.js + threads.js),
todo (shell.js), certmachine (bundle.js + bundle.css), multissh (bundle.js +
bundle.css), admin (bundle.js), timetracker (bundle.js), grocery (bundle.js).
`npm run build` at C8 re-stamps every sharedConsumer bundle. Stage ALL rebuilt
artifacts before running the artifact gate.

**Gate criteria:**
- `npm run build` — pass (all consumer bundles re-stamped)
- `npm run typecheck` — pass
- `npm run test:web` — all suites pass
- `make test` — pass
- bundle-shape gate passes with new stamps for ALL consumer bundles

---

## 5. Commit Sequence

| Commit | Summary | Key descriptor/build changes | EXPECTED_ARTIFACT_COUNT |
|---|---|---|---|
| C1 | taskmaster: ThemeManager + HamburgerMenu + retire local tokens | none | 16 |
| C2 | certmachine: ESM + sharedConsumer + adoption + toast->shared | format:"esm", +sharedConsumer, +Makefile --require | 16 |
| C3 | multissh: ESM + sharedConsumer + adoption + xterm wiring | format:"esm", +sharedConsumer, +Makefile --require | 16 |
| C4 | admin: TS conversion + build entry + adoption | +descriptor, +tsconfig, +Makefile --require | 17 |
| C5 | timetracker: TS conversion + FA removal + adoption | +descriptor, +tsconfig, +Makefile --require | 18 |
| C6 | grocery: TS conversion + IIFE unwrap + adoption + CDN removal + test adapt | +descriptor, +tsconfig, +test-web.mjs, +Makefile --require | 19 |
| C7 | D-5: obsidianoid thread-mode hamburger reachability | none | same |
| C8 | deslop: clean plan references in shared .ts sources | none (shared.mjs re-stamped) | same |

Each commit leaves ALL gates green:
- `npm run build`
- `npm run typecheck`
- `npm run test:web`
- `make test`
- `gofmt -l .` (no output)
- `go vet ./...` (no errors)
- `node scripts/gates/bundle-shape.mjs --require=<accumulated list>`

Rollback: any single commit can be reverted independently. C8 (deslop) is
optional and can be dropped without affecting any module's functionality.

---

## 6. Per-Step Verification Protocol

For each commit Cn:

1. **Pre-edit snapshot** — note current git status
2. **Make changes** per S4 specification
3. **Build** — `npm run build` (rebuilds all bundles)
4. **Typecheck** — `npm run typecheck` (tsc --noEmit)
5. **Web tests** — `npm run test:web` (all suites)
6. **Go tests** — `make test` (go test -race ./...)
7. **Go lint** — `gofmt -l .` (no output) + `go vet ./...` (no errors)
8. **Bundle-shape gate** — `node scripts/gates/bundle-shape.mjs --require=<list>`
9. **Token-overlap gate** — `node scripts/gates/token-overlap.mjs`
10. **Stage and commit** — stage rebuilt artifacts BEFORE running artifact gate
    (`scripts/gates/artifacts.mjs` diffs worktree-vs-index)

---

## 7. FR-4 Preservation Checklist

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
- [ ] Terminal colors update on theme switch (xterm re-theme via ThemeManager onChange constructor option)
- [ ] Tab bar functional
- [ ] File/dir/key pickers functional
- [ ] Theme picker in hamburger

### admin
- [ ] Admin panel renders
- [ ] All 6 panels accessible and functional
- [ ] Logout moved to hamburger, still works
- [ ] Theme picker in hamburger, all 8 themes apply

### timetracker
- [ ] Time entries render
- [ ] Edit buttons work (was FA fa-edit, now inline SVG)
- [ ] Download button works (was FA fa-download, now inline SVG)
- [ ] Copy button works (was FA fa-copy, now inline SVG)
- [ ] TimeSelector component functional
- [ ] Markdown rendering works (marked.min.js kept as external script)
- [ ] Theme picker in hamburger

### grocery
- [ ] Grocery list renders with sections
- [ ] Drag-and-drop reordering works
- [ ] Section expand/collapse works
- [ ] Add/edit/delete items functional
- [ ] Check/uncheck items functional
- [ ] Progress indicators render correctly
- [ ] Recipes tab functional (if present)
- [ ] app.test.js (now main.test.ts) passes — 1277 lines of coverage
- [ ] Mirror-integrity block passes against renamed source
- [ ] Theme picker in hamburger

---

## 8. Hard Constraints

1. Zero functionality or visual regression in modules NOT named above
2. `npm run build`, `npm run typecheck`, `npm run test:web`, `make test`
   all pass at every commit
3. `gofmt -l .` + `go vet ./...` clean at every commit
4. No CDN/external assets (apps run on closed LANs)
5. Generated artifacts (web/*/js/bundle.js, web/shared/dist/*) remain
   byte-identity gated via scripts/gates/bundle-shape.mjs
6. FRD S7 Resolved Decisions are SETTLED — never reopen
7. Never push, never touch main
8. Q11 rider: good effects propagate program-wide
9. Q12: any theme any module — all 8 themes must work in all adopted modules
10. BX.10 byte-identity BEST EFFORT for generated artifacts

---

## 9. Risk Assessment

| Risk | Likelihood | Mitigation |
|---|---|---|
| Token rename misses a var() reference -> visual glitch | Medium | grep for all local token names post-migration; visual spot-check per theme |
| Token introduction (admin/timetracker) maps hex to wrong role | Medium | Each hex replacement is judgment-based; executor verifies visually per theme |
| IIFE->ESM conversion breaks certmachine/multissh at runtime | Low | Both already use `import` statements; IIFE is esbuild output format only |
| Grocery test suite breaks during TS conversion | HIGH | The readFileSync path, norm() comparator (type-stripping mismatch), and mirror-integrity block must be adapted first; test-first approach — adapt paths + norm(), verify pass, then do the conversion |
| Grocery mirror-integrity tag regex breaks on rename | Medium | Update the regex pattern in the test to match the new filename |
| Grocery IIFE unwrapping promotes names to module scope | Low | ESM module scope provides the same encapsulation; esbuild bundles as a single entry point |
| xterm theme hardcoded hex not replaced | Medium | The hex `#0b0f17` at terminal.ts:33 must become a CSS variable read; visual terminal check under all 8 themes |
| timetracker marked.min.js bundling breaks | Low | Keep as separate `<script>` tag (simplest) |
| D-5 fix breaks obsidianoid normal mode | Low | Test both modes; trigger move is DOM-only |
| DM Sans removal changes grocery appearance | Medium | Accepted: shared Inter replaces DM Sans; both are clean sans-serifs. Self-host DM Sans as follow-up if needed |

---

## 10. ADRs

Continuing Phase 2's ADR-008 through ADR-015.

**ADR-016 — Token migration strategy varies by module state.**
*Decision:* Modules that already define local CSS custom properties (taskmaster,
certmachine, multissh, grocery) use mechanical rename of var() references and
deletion of local `:root` definitions. Modules with no custom properties (admin,
timetracker) introduce `var(--color-*)` references for key hex literals, guided
by visual role mapping.
*Drivers:* (1) taskmaster/certmachine/multissh/grocery all have well-organized
local tokens that map cleanly to shared tokens — a rename is safe and
mechanical. (2) admin/timetracker have only raw hex — introducing tokens requires
judgment about which hex serves which visual role. (3) grocery has the most
complex token landscape (~45 definitions) with module-specific semantics that
must stay local.
*Alternatives:* (A) one-size-fits-all mechanical rename (fails for hex-only
modules); (B) introduce a per-module token mapping JSON file consumed by a
build-time transform (over-engineered for 2 modules).
*Why chosen:* matches each module's actual state without added infrastructure.
*Consequences:* hex-only modules require visual verification per theme since
role assignment is judgment-based. Accepted: admin has only 32 hex values,
timetracker only 6.
*Follow-ups:* none.

**ADR-017 — Grocery test mirror-integrity with TS type stripping.**
*Decision:* Grocery converts to TS like the other modules. The test's `norm()`
comparator is updated to strip type annotations and collapse whitespace, making
it agnostic to Node's type-stripping behavior. When Node runs `.ts` files, it
replaces type annotations with whitespace padding; `fn.toString()` returns this
whitespace-padded text, while `readFileSync('main.ts')` returns raw TS with
annotations. The updated `norm()` normalizes BOTH sides of the comparison:
```js
function norm(s) {
  return s
    .replace(/^[ \t]+/gm, '')                                      // strip leading indent (existing)
    .replace(/:\s*[A-Za-z_][\w\[\]|&<>, ]*(?=\s*[,)={}])/g, '')   // strip type annotations
    .replace(/\s+/g, ' ')                                          // collapse whitespace
    .trim();
}
```
The test file moves alongside its source (`web/grocery/js/main.test.ts`) and
its `readFileSync` calls are updated to point to the new source path
(`main.ts`). The test continues using the `node-test` runner.
*Drivers:* (1) The mirror-integrity pattern is a deliberate design — it catches
drift between test copies and shipped code. (2) `import.meta.dirname` requires
the `node-test` runner. (3) Node's type stripping makes `fn.toString()`
incompatible with `readFileSync` of raw TS source unless the comparator
normalizes both representations.
*Alternatives:* (A) Keep grocery as JS to avoid the type-stripping issue
entirely (rejected: inconsistent with the project's TS-first direction and
defers necessary conversion work); (B) rewrite tests to import functions
directly (breaks the "inspect what ships" principle); (C) read bundle.js
instead of source (bundle is minified/transformed, mirror check would fail).
*Why chosen:* preserves the mirror-integrity guarantee while allowing full TS
conversion; the norm() fix is a small, targeted change.
*Consequences:* the norm() regex for type annotation stripping is a heuristic
— it handles common patterns (`: string`, `: Recipe[]`, `: boolean`) but may
need adjustment for complex generic types. Executor must verify mirror-integrity
passes after conversion. The tag-counting regex must update to match the new
test filename.
*Follow-ups:* none.

**ADR-018 — xterm theme reads shared CSS custom properties at runtime.**
*Decision:* Replace the hardcoded `theme: { background: "#0b0f17" }` in
terminal.ts with a helper that reads `getComputedStyle` for shared token
values. Wire the ThemeManager constructor's `onChange` option (a constructor
option on `ThemeManagerOptions`, NOT a class method) to re-apply the xterm
theme on theme switch.
*Drivers:* (1) Q12 requires all 8 themes to work in all adopted modules.
(2) The xterm Terminal constructor accepts an `ITheme` object that must
contain resolved color strings, not CSS var() references. (3) Theme switches
happen at runtime and must update the terminal without reconnecting.
*Alternatives:* (A) hardcode 8 theme objects as a static map (duplicates
themes.css values, drifts on any theme update); (B) set terminal background
via CSS only (xterm's canvas rendering ignores external CSS for its own
background/foreground).
*Why chosen:* single source of truth (themes.css) with runtime resolution.
*Consequences:* terminal theme update has a brief flash as the computed style
is read and applied. Accepted: negligible for a theme switch action.
*Follow-ups:* none.

---

## 11. CSS Token Introduction Strategy (hex-only modules)

For admin (32 hex literals) and timetracker (6 hex literals), tokens must be
INTRODUCED where none exist. This is fundamentally different from the rename
strategy used for modules that already have tokens.

**Approach:**
1. For each hex literal in the CSS, identify its visual role:
   - Background colors -> `var(--color-bg)`, `var(--color-surface-1)`, etc.
   - Text colors -> `var(--color-text)`, `var(--color-text-muted)`, etc.
   - Border colors -> `var(--color-border)`
   - Accent/interactive colors -> `var(--color-primary)`, `var(--color-primary-hover)`
   - Status colors -> `var(--color-success)`, `var(--color-danger)`, `var(--color-warning)`
2. Replace hex with `var(--token-name)` in the CSS
3. Do NOT add a local `:root` fallback block — shared.css provides all values
4. For hex values that don't clearly map to a shared token (unusual gradients,
   decorative colors), leave as-is
5. Verify all 8 themes visually after replacement

**Expected coverage:**
- admin: ~20-25 of 32 hex values replaceable
- timetracker: ~4-5 of 6 hex values replaceable

This is judgment-based work that cannot be fully specified in advance. The
executor makes per-value decisions and verifies visually.

---

## 12. Open Questions

1. ~~**admin CSS import strategy**~~ — RESOLVED: admin, timetracker, and grocery
   keep CSS as standalone `<link>` tags (no emitsCss). See Section 3.1.

2. **grocery DM Sans** — removing the CDN `<link>` means DM Sans is gone and
   Inter takes over. If the visual difference is unacceptable, DM Sans can be
   self-hosted under web/shared/public/fonts/ as a follow-up. Not blocking.

3. **timetracker marked.min.js** — keep as external `<script>` or bundle via
   esbuild? External is simpler and avoids bundling a vendored minified file.
   Executor decides.

4. **grocery test runner** — the test currently uses `node-test` runner because
   of `import.meta.dirname`. After TS conversion, evaluate whether esbuild-cjs
   runner is viable (it would need a different way to locate the source file).
   Default: keep `node-test`.

5. **grocery norm() regex coverage** — the type-annotation stripping regex in
   the updated `norm()` function handles common patterns (`: string`,
   `: Recipe[]`, `: boolean`) but may need adjustment for complex generic types
   if grocery adds any during TS conversion. Executor should verify all 14
   mirror-integrity function comparisons pass after conversion.
