# Phase 4 — React bindings + smbedit / issuetracker / slideshow / utuber migration

**Status:** `approved` (consensus: Architect APPROVE + Critic APPROVE, iteration 3, 2026-09-20)
**Branch:** `ui-upgrade`
**Predecessor:** Phase 3 (C1-C8, commits `73936e7..31935dd`)

## 1. Scope

FRD §6 row 4: "React bindings; smbedit + issuetracker migrate tokens/theme/
toast/confirm; slideshow modern app migrates; utuber split into files + migrated."

Four modules, each with distinct migration shapes:

| Module | Framework | Current state | Migration shape |
|--------|-----------|---------------|-----------------|
| smbedit | React (TSX) | IIFE bundle, local ThemeProvider + Toast, Google Fonts CDN, `window.confirm()` | ESM+sharedConsumer, delete local theme/toast, replace native dialogs, CDN removal |
| issuetracker | React (TSX) + react-router | IIFE bundle, dark-only CSS tokens, `alert()`/`confirm()` in 4 files | ESM+sharedConsumer, token migration, replace native dialogs |
| slideshow | Vanilla TS (transpile mode) | Multi-file transpile, 20 CSS files, FA webfonts, jQuery/jQuery-UI, hand-built settings panel | Token migration in app.css (24 var refs), ThemeManager+HamburgerMenu, FA removal |
| utuber | Vanilla (single 816-line HTML) | Everything inline in one file, Google Fonts CDN, dark-only | Split into files (main.ts + style.css), ESM+sharedConsumer, token migration, CDN removal |

## 2. RALPLAN-DR Summary

### Principles
1. **Mechanical over creative** — token renames and import swaps are deterministic; minimize judgment calls
2. **React modules keep React** — don't rewrite React components; adapt the bridging layer only
3. **Shared library is the source of truth** — delete local theme/toast/dialog implementations in favour of @shared
4. **One commit per module** — each module is independently testable and revertible
5. **utuber file split is Phase 4's only structural change** — everything else is adoption

### Decision Drivers
1. **React bridging strategy** — how do React components consume the imperative shared library (ThemeManager, showToast, confirmDialog)?
2. **utuber split granularity** — how many files to create from 816 lines of inline HTML+CSS+JS?
3. **slideshow's jQuery dependency** — migrate settings panel to shared HamburgerMenu, or just add theme wiring alongside existing panel?

### Viable Options

**Option A: Thin imperative bridge (CHOSEN)**
- React modules import `ThemeManager` and call `themes.apply()` at the module level (outside React), same as vanilla modules
- Delete local ThemeProvider/ToastProvider React components
- Replace `window.confirm()` / `alert()` with `confirmDialog()` / `alertDialog()` from `@shared` (returns Promise — wrap in async handlers)
- HamburgerMenu mounted imperatively in main.tsx before React root, trigger prepended to a top-level container
- Pros: consistent with all other modules, no new React wrapper code, shared library stays framework-agnostic
- Cons: theme state lives outside React tree (acceptable — it's a CSS custom property, not React state)

**Option B: React wrapper components**
- Create `useThemeManager()` hook and `<SharedToastProvider>` wrapping the imperative API
- Pros: idiomatic React
- Cons: new code to maintain, framework coupling in shared library, inconsistent with vanilla modules
- **Rejected:** adds code and maintenance burden for no functional benefit; ThemeManager is CSS-driven, not state-driven

### Slideshow decision
Slideshow already has a hand-built settings panel (`#settings-panel`) with theme select, interval, shuffle, etc. The HamburgerMenu would duplicate this. **Decision:** keep the existing settings panel, add ThemeManager wiring only (expand the theme select from dark/light to all 8 themes), add shared.css link. Do NOT add HamburgerMenu — it would conflict with the existing UI.

### utuber split
Split into 3 files: `web/utuber/js/main.ts` (JS logic), `web/utuber/style.css` (CSS), `web/utuber/index.html` (markup only). This is the minimum viable split — the file is well-structured with clear CSS/JS/HTML sections.

## 3. Build system progression

| Commit | EXPECTED_ARTIFACT_COUNT | --require additions |
|--------|------------------------|-------------------|
| C0 (gate fix) | 19 → 20 (fix Phase 3 off-by-one) | (none) |
| C1 (smbedit) | 20 (unchanged) | +smbedit |
| C2 (issuetracker) | 20 (unchanged) | +issuetracker |
| C3 (slideshow) | 20 (unchanged, transpile mode) | (slideshow is transpile, not sharedConsumer) |
| C4 (utuber) | 21 (+1, new descriptor) | +utuber |

Note: Phase 3 left EXPECTED_ARTIFACT_COUNT=19, but 14 descriptors produce 20 artifacts. C0 corrects this before module work begins.

smbedit and issuetracker already have build descriptors (IIFE→ESM change needed).
slideshow has a transpile descriptor (no sharedConsumer — it loads shared.css via HTML link).
utuber has NO descriptor — needs one created.

## 4. Commit sequence

### 4.0 C0: fix EXPECTED_ARTIFACT_COUNT (Phase 3 off-by-one)

**Current state:** EXPECTED_ARTIFACT_COUNT=19 in scripts/descriptors.mjs, but 14 descriptors produce 20 artifacts (obsidianoid emits 2 files from 1 descriptor).

**Changes:**
1. scripts/descriptors.mjs — change EXPECTED_ARTIFACT_COUNT from 19 to 20

**Gates:** npm run build (verifies artifact gate passes with corrected count)

**Touches:** scripts/descriptors.mjs

### 4.1 C1: smbedit — ESM + sharedConsumer + delete local theme/toast + dialog migration

**Current state:** React app with local ThemeProvider (3 modes: dark/light/system), local ToastProvider, Google Fonts CDN (JetBrains Mono + Syne), `window.confirm()` in SettingsPage.tsx. IIFE bundle. 129 var() refs in styles.css across dark/light themes with local tokens (--bg, --bg-panel, --bg-card, etc.).

**Changes:**

1. **Descriptor** — change format:"iife" to format:"esm", add sharedConsumer:true
2. **Delete local theme.tsx** — remove ThemeProvider, useTheme hook. Replace with imperative ThemeManager at module level in main.tsx
3. **Delete local Toast.tsx** — remove ToastProvider, useToast hook. Replace all `toast(title, message?, kind?)` calls with `showToast(message, tone?)` from @shared — note the signature difference: showToast takes (message, tone) where tone is "success"|"error"|"notice", so adapt each call site in App.tsx (lines ~28, 55, 91, 93, 112, 118, 147, 149, 152). Tone mapping: 'success'→'success', 'error'→'error', 'warning'→'notice'. Where toast was called with (title, message), concatenate or use message only depending on context. The `warnAutoDisabled(before, after, toast)` helper (App.tsx:23) takes `toast` as a parameter — refactor to call `showToast` directly and remove the parameter. Update App.tsx to remove ToastProvider wrapper
4. **Dialog migration** — replace `window.confirm()` in SettingsPage.tsx with `confirmDialog()` from @shared (async)
5. **Token migration** in styles.css — map local tokens to shared:
   - --bg → --color-bg
   - --bg-panel → --color-surface-1
   - --bg-card → --color-surface-2
   - --bg-input → --color-surface-1 (closest match)
   - --bg-hover → --color-surface-2
   - --border → --color-border
   - --border-focus → --color-primary
   - --text → --color-text
   - --text-muted → --color-text-muted
   - --text-subtle → --color-text-faint
   - --accent / --accent-hi → --color-primary / --color-primary-hover
   - --green → --color-success
   - --red → --color-danger
   - --yellow → --color-warning
   Delete [data-theme="dark"] and [data-theme="light"] blocks (shared themes.css provides all palettes). Keep module-specific tokens (--scrollbar, --scrollbar-thumb, --shadow, --shadow-sm, --radius, --radius-lg, --accent-glow) as local.
6. **HamburgerMenu** — construct imperatively in main.tsx with themePicker:true, title:'SMBEdit'. Prepend trigger to the app's topbar. The existing settings drawer (App.tsx:258-276) and its hamburger button (App.tsx:178-186) are KEPT — they host SettingsPage content (server config, import, share ownership) which is app-specific. The shared HamburgerMenu provides the theme picker. **Remove the Appearance card from SettingsPage (lines 31-52) and the dead useTheme/themes/mode/setMode references above it (lines 3, 13, 16-20)** — the theme-switcher duplicates the HamburgerMenu's themePicker, and after theme.tsx deletion these imports become dead code.
7. **Google Fonts CDN** — remove preconnect + stylesheet links from index.html. JetBrains Mono replaced by shared's --font-mono; Syne replaced by --font-body.
8. **index.html** — add shared.css link, data-theme="dark", theme bootstrap, type="module"
9. **Makefile** — append smbedit to --require

**Inline var() migration in TSX:**
   - App.tsx:160 `var(--text-muted)` → `var(--color-text-muted)` (loading spinner)
   - App.tsx:223 `var(--green)` → `var(--color-success)`, `var(--red)` → `var(--color-danger)` (restart output card border)
   - PreviewPage.tsx:67 `var(--red)` → `var(--color-danger)` (error card border)

**Touches:** web/smbedit/src/main.tsx, App.tsx, PreviewPage.tsx, theme.tsx (delete), Toast.tsx (delete), SettingsPage.tsx, styles.css, index.html, scripts/descriptors.mjs, Makefile

### 4.2 C2: issuetracker — ESM + sharedConsumer + token migration + dialog migration

**Current state:** React app with react-router-dom, dark-only CSS (no theme switching). IIFE bundle. 80 var() refs with local tokens. `alert()` in IssueModal.tsx, `confirm()` in ProjectsPage, TagsPage, IssueDetailPage.

**Changes:**

1. **Descriptor** — format:"iife" → "esm", add sharedConsumer:true
2. **Token migration** in styles.css:
   - --bg → --color-bg
   - --bg-elevated → --color-surface-1
   - --bg-hover → --color-surface-2
   - --bg-input → --color-surface-1
   - --border → --color-border
   - --border-strong → --color-border (keep as local if visually distinct)
   - --text → --color-text
   - --text-dim → --color-text-muted
   - --text-faint → --color-text-faint
   - --accent → --color-primary
   - --accent-hover → --color-primary-hover
   - --radius → --radius-md
   Delete :root block's shared-matching defs, keep --sidebar-w as local
3. **Dialog migration** — replace native dialogs:
   - IssueModal.tsx:72 `alert(String(e))` → `alertDialog(String(e))`
   - ProjectsPage.tsx:71 `confirm(...)` → `await confirmDialog(...)`
   - TagsPage.tsx:31 `confirm("Delete this tag?")` → `await confirmDialog("Delete this tag?")`
   - IssueDetailPage.tsx:86 `confirm("Delete this issue?")` → `await confirmDialog("Delete this issue?")`
   Import `alertDialog, confirmDialog` from `@shared` in each file
4. **ThemeManager + HamburgerMenu** — construct in main.tsx before ReactDOM.createRoot. Module:'issuetracker', default:'dark'. HamburgerMenu with themePicker.
5. **index.html** — add shared.css link, data-theme="dark", theme bootstrap, type="module"
6. **Makefile** — append issuetracker to --require

**Inline var() migration in TSX** (21 refs across 11 files — these use local tokens in inline `style={}` props):
   - common.tsx:31 `var(--text-dim)` → `var(--color-text-muted)`, :41 `var(--border-strong)` → `var(--color-border)`
   - IssueModal.tsx:184 `var(--text-faint)` → `var(--color-text-faint)`
   - IssuesPage.tsx:54 `var(--text-faint)` → `var(--color-text-faint)`
   - EpicsPage.tsx:24 `var(--text-faint)` → `var(--color-text-faint)`
   - StoriesPage.tsx:24 `var(--text-faint)` → `var(--color-text-faint)`
   - EpicDetailPage.tsx:34 `var(--text-dim)` → `var(--color-text-muted)`
   - StoryDetailPage.tsx:35 `var(--text-dim)` → `var(--color-text-muted)`, :39 `var(--accent)` → `var(--color-primary)`
   - ApiPage.tsx:19 `var(--bg-input)` → `var(--color-surface-1)`, :20 `var(--border)` → `var(--color-border)`, :39/:60/:76 `var(--text-dim)` → `var(--color-text-muted)`
   - ProjectsPage.tsx:142/:206 `var(--border)` → `var(--color-border)`
   - TagsPage.tsx:93 `var(--border)` → `var(--color-border)`
   - IssueDetailPage.tsx:105/:235/:243 `var(--text-faint)` → `var(--color-text-faint)`, :183 `var(--accent)` → `var(--color-primary)`

**Touches:** web/issuetracker/src/main.tsx, styles.css, common.tsx, IssueModal.tsx, IssuesPage.tsx, EpicsPage.tsx, StoriesPage.tsx, EpicDetailPage.tsx, StoryDetailPage.tsx, ApiPage.tsx, ProjectsPage.tsx, TagsPage.tsx, IssueDetailPage.tsx, index.html, scripts/descriptors.mjs, Makefile

### 4.3 C3: slideshow — token migration + ThemeManager wiring (keep existing panel)

**Current state:** Vanilla TS in transpile mode (not bundled). 20+ CSS files, most with zero var() refs. app.css has 24 var() refs. Has its own settings panel with dark/light theme select. jQuery/jQuery-UI loaded via script tags. FontAwesome webfonts present. Already has `type="module"` on script tag.

**Changes:**

1. **DO NOT change descriptor** — slideshow is transpile mode, not bundle mode. It cannot use sharedConsumer (no single bundle to inject the import into). shared.css loaded via HTML link instead.
2. **Token migration** in css/app.css — map 24 var() refs from local tokens to shared tokens. The :root block in app.css defines 9 local tokens in :root + [data-theme="light"]; delete shared-matching defs, keep module-specific.

   | Local token | Shared token | Refs | Classification |
   |---|---|---|---|
   | --bg | --color-bg | 2 (lines 31, 55) | shared-matching → delete from :root |
   | --fg | --color-text | 5 (lines 32, 174, 242, 280, 287) | shared-matching → delete from :root |
   | --panel-bg | --color-surface-1 | 1 (line 235) | shared-matching → delete from :root |
   | --panel-border | --color-border | 2 (lines 237, 295) | shared-matching → delete from :root |
   | --label-fg | --color-text-muted | 1 (line 273) | shared-matching → delete from :root |
   | --overlay-bg | (keep local) | 4 (lines 109, 146, 211, 217) | module-specific (semi-transparent gradient overlays) |
   | --btn-bg | (keep local) | 3 (lines 172, 285, 335) | module-specific (translucent button fill) |
   | --btn-border | (keep local) | 3 (lines 173, 286, 336) | module-specific (translucent button border) |
   | --btn-hover | (keep local) | 1 (line 183) | module-specific (translucent hover) |
   | --interval | (keep local) | 2 (lines 79, 82) | module-specific (CSS animation duration fallback, not a theme token) |

   Delete the [data-theme="light"] block entirely — shared themes.css provides all light-mode values for shared tokens; module-specific tokens (--overlay-bg, --btn-bg, --btn-border, --btn-hover) need light-mode values moved to a `[data-theme="light"]` block that defines only those 4.
3. **ThemeManager wiring** in js/app.ts:
   - Import ThemeManager, THEMES from `/shared/dist/shared.mjs` (direct path, since no sharedConsumer)
   - Construct ThemeManager with module:'slideshow', default:'dark'
   - Wire the existing `#sel-theme` select to ThemeManager.set() and populate with all 8 THEMES
   - ThemeManager onChange updates the select's displayed value
   - **SSE coordination:** the existing `applyState` function (app.ts:~299) sets `document.documentElement.dataset.theme` directly — update it to call `themes.set(state.theme)` instead so ThemeManager and the SSE state don't fight. ThemeManager's onChange should call `control("set-theme", name)` to persist to the server.
4. **index.html** — add shared.css link before /css/app.css
5. **FontAwesome removal** — delete web/slideshow/webfonts/ directory and all FA CSS files: css/fontawesome.min.css, css/fontawsme.css (FA 4.6.3), css/fontawall.css (FA 5.7.0). Update any HTML/CSS referencing FA icons.
6. **DO NOT add HamburgerMenu** — slideshow has its own settings panel that controls slideshow-specific settings (mode, interval, shuffle, debug). HamburgerMenu would be redundant.

**Touches:** web/slideshow/css/app.css, js/app.ts, index.html, webfonts/ (delete), css/fontawesome.min.css (delete), css/fontawsme.css (delete), css/fontawall.css (delete)

### 4.4 C4: utuber — file split + ESM + sharedConsumer + token migration + CDN removal

**Current state:** Single 816-line index.html with ALL CSS in `<style>`, ALL JS inline in `<script>`. Google Fonts CDN (Inter). 94 var() refs with local tokens. No build descriptor.

**Changes:**

1. **File split:**
   - Extract `<style>` content → `web/utuber/style.css`
   - Extract `<script>` content → `web/utuber/js/main.ts` (add minimal type annotations)
   - Reduce index.html to markup + link/script tags
2. **Build descriptor** — add to scripts/descriptors.mjs:
      ```js
      { name: "utuber", entry: ["web/utuber/js/main.ts"], mode: "bundle", out: "web/utuber/js/bundle.js",
        format: "esm", bundle: true, target: "es2020", sharedConsumer: true, emitsCss: false }
      ```
      Update EXPECTED_ARTIFACT_COUNT from 20 → 21.
3. **Token migration** in style.css — map 94 var() refs:
   - --bg → --color-bg
   - --surface → --color-surface-1
   - --surface-2 → --color-surface-2
   - --border → --color-border
   - --text → --color-text
   - --text-muted → --color-text-muted
   - --text-faint → --color-text-faint
   - --primary → --color-primary
   - --primary-hov → --color-primary-hover
   - --primary-dim → --color-primary-tint
   - --success → --color-success
   - --danger → --color-danger
   - --radius → --radius-md
   - --radius-sm → --radius-sm
   Keep module-specific: --audio, --audio-hov, --audio-dim, --dl-blue, --conv-orange, --shadow, --t, --font
4. **ThemeManager + HamburgerMenu** in main.ts — module:'utuber', default:'dark', themePicker:true
5. **Google Fonts CDN** — remove from index.html. Inter replaced by shared's --font-body.
6. **index.html** — shared.css link, data-theme="dark", theme bootstrap, type="module" to js/bundle.js
7. **Makefile** — append utuber to --require

8. **tsconfig.json** — add `"web/utuber/js/*.ts"` to the include array

**Touches:** web/utuber/index.html (rewrite), web/utuber/style.css (new), web/utuber/js/main.ts (new), scripts/descriptors.mjs, tsconfig.json, Makefile

## 5. Gates

Each commit must pass:
- `npm run build`
- `npm run typecheck`
- `npm run test:web`
- `node scripts/gates/bundle-shape.mjs --require=<cumulative list>`

Final `make test` after C4 (Go tests unaffected by CSS/TS but run as Phase gate).

## 6. Token mapping summary

| Local token pattern | Shared token | Modules affected |
|---|---|---|
| --bg (smbedit, issuetracker, utuber, slideshow) | --color-bg | all 4 |
| --bg-panel / --bg-elevated / --surface / --panel-bg | --color-surface-1 | smbedit, issuetracker, utuber, slideshow |
| --bg-card / --surface-2 | --color-surface-2 | smbedit, utuber |
| --border / --panel-border | --color-border | all 4 |
| --text / --fg | --color-text | all 4 |
| --text-muted / --text-dim | --color-text-muted | smbedit, issuetracker, utuber |
| --text-subtle / --text-faint | --color-text-faint | smbedit, issuetracker, utuber |
| --label-fg | --color-text-muted | slideshow |
| --accent / --primary | --color-primary | smbedit, issuetracker, utuber |
| --accent-hover / --primary-hov | --color-primary-hover | smbedit, issuetracker, utuber |
| --green / --success | --color-success | smbedit, utuber |
| --red / --danger | --color-danger | smbedit, issuetracker, utuber |
| --yellow | --color-warning | smbedit |
| --radius | --radius-md | issuetracker, utuber |
| --overlay-bg, --btn-bg, --btn-border, --btn-hover | (module-specific, kept local) | slideshow |

## 7. Risk register

| Risk | Severity | Mitigation |
|---|---|---|
| smbedit ThemeProvider removal breaks React re-render of theme-dependent components | Medium | ThemeManager drives CSS custom properties via data-theme attribute; React components read via var() in CSS, no re-render needed |
| smbedit server-side theme persistence under imperative model | Medium | ThemeManager's onChange callback must call patchConfig({theme}) (currently at App.tsx:167). Pattern: export a module-level `let persistTheme: (t: string) => void = () => {}` in main.tsx; ThemeManager's onChange sets it. Inside App, a `useEffect` registers `persistTheme = (t) => patchConfig({theme: t})`. This bridges imperative→React without adding a wrapper component. |
| smbedit ToastProvider removal breaks toast calls in deeply nested components | Medium | Replace useToast() hook calls with direct showToast() imports — showToast is imperative, no context needed. Note signature difference: toast(title, msg?, kind?) → showToast(msg, tone?) |
| issuetracker confirm()→confirmDialog() breaks synchronous control flow | Medium | confirmDialog returns Promise; wrap calling functions in async, add await. Test each call site. |
| slideshow app.ts imports from /shared/dist/shared.mjs directly (no sharedConsumer) | Low | Transpile mode doesn't bundle; the import resolves at runtime via the /shared/ route. Not checked by bundle-shape gate. |
| utuber file split introduces path errors | Low | Simple mechanical extraction; test by loading the page |

## 8. ADR

**ADR-019 — React modules use imperative shared library, not React wrappers.**

*Decision:* smbedit and issuetracker consume ThemeManager, showToast, confirmDialog, and HamburgerMenu imperatively (same pattern as vanilla modules), deleting their local React ThemeProvider and ToastProvider.

*Drivers:* (1) Consistency — all 14 modules use the same imperative API. (2) No new code — React wrappers would be net-new maintenance surface. (3) The shared library is intentionally framework-agnostic.

*Alternatives considered:* React wrapper hooks (useThemeManager, useSharedToast) — rejected because ThemeManager is CSS-driven (data-theme attribute), not React-state-driven, and showToast is a fire-and-forget imperative call that needs no React context.

*Why chosen:* Simpler, less code, consistent with the 10 vanilla modules already adopted.

*Consequences:* Theme changes don't trigger React re-renders (acceptable — CSS custom properties update automatically). Toast rendering is handled by the shared library's DOM, not React's virtual DOM (acceptable — the toast stack is a separate DOM tree).

**ADR-020 — slideshow keeps its settings panel, gains ThemeManager only.**

*Decision:* Slideshow does not get a HamburgerMenu. Its existing settings panel is wired to ThemeManager for expanded theme support (8 themes instead of 2).

*Drivers:* The settings panel controls slideshow-specific settings (mode, interval, shuffle, controls position) that have no equivalent in HamburgerMenu's generic item model. Adding both would create two competing settings surfaces.

*Consequences:* Slideshow is the one module without a HamburgerMenu. Its settings panel gains theme selection via ThemeManager integration.
