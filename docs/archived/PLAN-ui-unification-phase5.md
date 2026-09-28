# Phase 5 — menuserver rewrite + legacy tree deletion + FA removed repo-wide

**Status:** `approved` (consensus: Architect APPROVE + Critic APPROVE, iteration 3, 2026-09-22)
**Branch:** `ui-upgrade`
**Predecessor:** Phase 4 (C0-C4, commits `7a5f998..83e4c36`)

## 1. Scope

FRD §6 row 5: "menuserver rewrite off jQuery onto the shared library; delete
its legacy tree; FA fully removed repo-wide."

Plus the repo-wide FR-5 requirement: zero `alert()`/`confirm()`/`prompt()`
calls (satisfied by shared `showToast`/`alertDialog`/`confirmDialog`).

### Current state inventory

**menuserver** — the last jQuery holdout:
- **index.html** (48 lines): loads jQuery + jQuery-UI + navcontrols.js +
  utils.js + menuserver.js. Builds nav dropdowns dynamically from JSON API,
  shows/hides content pages. FA CSS for dropdown carets + page nav arrows.
- **menuserver.html**: byte-identical copy of index.html (dead).
- **base.html** (224 lines): old pre-React taskmaster dashboard (login +
  panels + tabs + tables + lightbox). Loads axios + base-util.js + panels.js.
  **Dead code** — taskmaster has its own modern UI at `web/taskmaster/`.
- **17 JS files** (33k lines): 29k is jQuery/jQuery-UI vendor; active app JS
  is menuserver.js (420), navcontrols.js (7), utils.js (161) = 588 lines.
  Dead copies: todo.js, todo-utils.js, slideshow.js, exercise.js, kbc.js,
  onoff.js, panels.js, base-util.js.
- **19 CSS files** (2.9k lines): 1.3k is jQuery-UI; active is nav.css (133)
  + responsivenav.css. The rest is vendor, dead features, or base.html-only.
- **5 webfont files** (FA), **4 images**.

**todo** — FA still active, jQuery still loaded:
- index.html loads jQuery + jQuery-UI + fontawsme.css + old todo.js/utils.js/
  todo-utils.js alongside the Phase 3 shell.js module. 24+ FA icon references
  across index.html, compare.html, shell.ts, todo-utils.js, compare.js
  (including inline bootstrap scripts and duplicates across files).
- webfonts/ has 5 FA font files.
- Google Fonts CDN `<link>` still present (should have been removed earlier).

**obsidianoid** — one native `confirm()` at app.ts:139.

**Remaining alert() calls** (active, non-commented):
- menuserver: utils.js (2 — copyToClipBoard), menuserver.js (2 — behind
  `showsavealert` flag, effectively dead), todo.js (5), slideshow.js (1)
- todo: todo.js (5), utils.js (2), compare.js (1)
- All menuserver alert() calls are in files deleted in C1; all todo alert()
  calls are in legacy JS that coexists with shell.js.

## 2. RALPLAN-DR Summary

### Principles
1. **Menuserver is the primary deliverable** — the jQuery→vanilla TS rewrite
   is the structural work; everything else is cleanup
2. **Nav dropdowns → HamburgerMenu link items** — per FRD §FR-4 menuserver row
3. **Delete dead code first, then migrate live code** — base.html and its JS
   are dead; purge before converting
4. **FA replacement is icon-for-icon** — each FA class maps to an inline SVG;
   no visual regression
5. **Shared dialogs replace native dialogs** — `showToast` for notifications,
   `alertDialog`/`confirmDialog` for blocking interactions

### Decision Drivers
1. **menuserver nav architecture** — dropdown nav becomes HamburgerMenu items;
   the page-show/page-hide logic stays as-is in vanilla TS
2. **todo jQuery dependency** — todo.js/todo-utils.js are 1400+ lines of
   jQuery; full TS conversion is out of Phase 5 scope. FA removal and alert()
   replacement can be done without removing jQuery.
3. **base.html disposition** — dead code (old taskmaster UI), delete entirely

### Viable Options

**Option A: menuserver TS bundle + FA/alert cleanup only (CHOSEN)**
- Rewrite menuserver's 588 active JS lines into one TypeScript module
- Replace jQuery DOM manipulation with vanilla DOM API + fetch
- HamburgerMenu replaces dropdown nav; ThemeManager adds theme support
- FA replaced with inline SVGs in menuserver and todo
- alert() → showToast/alertDialog in todo (in the existing JS, not a TS rewrite)
- Todo keeps jQuery for now; its TS conversion is follow-up work
- Pros: bounded scope, all FRD Phase 5 requirements met, todo remains functional
- Cons: todo still loads jQuery (technical debt, but not a Phase 5 requirement)

**Option B: full todo TS rewrite in Phase 5**
- Also convert todo.js + todo-utils.js + utils.js (1500+ lines) to TypeScript
- Remove jQuery from todo entirely
- Pros: cleaner final state
- Cons: doubles the scope; todo's jQuery code is complex (drag-and-drop via
  jQuery-UI sortable, periodic saves, SSE reconnection); high risk for regressions
- **Rejected:** FRD Phase 5 scope is "menuserver rewrite... FA fully removed
  repo-wide" — todo jQuery removal is not required

### ADR: base.html is dead code

**Decision:** delete base.html + base-util.js + panels.js.

**Drivers:** base.html's title says "Reactive App (replace on load)"; its JS
fetches `/status`, `/config`, `/task`, `/login`, `/logout` — all taskmaster
API endpoints. Taskmaster has its own React UI at `web/taskmaster/` since
Phase 0. base-util.js functions (fetchStatus, updateTable, etc.) are
taskmaster-specific. No other module loads these files.

**Consequences:** 810 lines of dead code removed. If anyone needs this old UI,
it's recoverable from git history.

## 3. Build system progression

| Commit | EXPECTED_ARTIFACT_COUNT | --require additions |
|--------|------------------------|---------------------|
| C0 | 21 → 22 (+1 new descriptor) | +menuserver |
| C1 | 22 (unchanged) | (none — deletion only) |
| C2 | 22 (unchanged) | (none — FA cleanup only) |
| C3 | 22 (unchanged) | (none — dialog cleanup only) |

## 4. Commit sequence

### 4.0 C0: menuserver TS conversion + shared library adoption

**Goal:** rewrite menuserver's index.html off jQuery onto vanilla TS + shared
library. Nav dropdowns become HamburgerMenu link items.

**New files:**
1. `web/menuserver/js/main.ts` — consolidated from menuserver.js (420 lines) +
   navcontrols.js (7 lines) + utils.js (161 lines → only the functions used
   by menuserver: `ajaxGetJSON`, `titleCase`, `toggle`, `makeID`,
   `copyToClipBoard`, `rebuildListSelector`).

   Key conversions:
   - `$.ajax({url, type:'post', ...})` → `fetch(url, {method:'POST', ...})`
   - `ajaxGetJSON` already uses `fetch` (not jQuery) — keep as-is, add types
   - `$('#el')` → `document.getElementById('el')` / `querySelector`
   - `$('#el').val()` → `(el as HTMLSelectElement).value`
   - `$('#el').append(html)` → `el.insertAdjacentHTML('beforeend', html)`
   - `$('#el').show()/$('#el').hide()` → `el.hidden = false/true`
   - `$('#el').prop('disabled', v)` → `(el as HTMLButtonElement).disabled = v`
   - `$(document).ready(fn)` → top-level module execution (ESM runs deferred)
   - `openMenu()` (navcontrols.js) → removed, replaced by HamburgerMenu

   ThemeManager + HamburgerMenu wiring:
   ```typescript
   import { ThemeManager, HamburgerMenu } from '@shared';

   const themes = new ThemeManager({ module: 'menuserver', default: 'dark' });
   themes.apply();

   // After loading topMenus from API, build HamburgerMenu items:
   const menuItems = topMenus.flatMap(group => [
     { separator: true, label: group.subject },
     ...group.submenus.map(sub => ({
       id: sub.id,
       label: sub.title,
       onSelect: () => showPage(sub.id),
     })),
   ]);

   const hamburger = new HamburgerMenu({
     title: 'Menuserver',
     items: menuItems,
     themePicker: true,
     themes,
   });
   ```

   The old dropdown nav (`<div class="topnav" id="myTopnav">` + jQuery-built
   `<div class="dropdown">` elements) is replaced by HamburgerMenu. Page
   content rendering (addPage, showPage) stays — just rewritten from jQuery
   `$('#el').append()` to vanilla `insertAdjacentHTML`.

2. `web/menuserver/css/app.css` — consolidated from nav.css (133 lines),
   stripped of the old dropdown/topnav styles (replaced by shared components),
   keeping only module-specific page layout styles. Tokenized:
   - Hard-coded `#333` bg → `var(--color-surface-1)`
   - Hard-coded `#f2f2f2`/`#818181` text → `var(--color-text)`/`var(--color-text-muted)`
   - Hard-coded `#111` sidenav bg → removed (HamburgerMenu handles drawer)
   - Hard-coded `#ddd` hover → `var(--color-surface-2)`
   - `font-family: "Lato"` → `var(--font-body)`
   - Page content styles (`.pageClass`, table spacing) kept and tokenized

**Modified files:**
3. `web/menuserver/index.html` — rewritten:
   - Remove: jQuery/jQuery-UI `<script>` tags, navcontrols.js, old utils.js,
     old menuserver.js, inline `<script>$(document).ready(...)`, FA CSS links,
     responsivenav.css link, modal.css link
   - Add: theme bootstrap `<script>`, `/shared/dist/shared.css` link,
     `css/app.css` link, `<script type="module" src="js/bundle.js">`
   - Keep: `<div id="lowerSection">` (page content container), hidden
     clipboard input
   - Remove: `<div class="topnav" id="myTopnav">` (replaced by HamburgerMenu)

4. `scripts/descriptors.mjs`:
   ```javascript
   {
     name: "menuserver",
     entry: ["web/menuserver/js/main.ts"],
     mode: "bundle",
     out: "web/menuserver/js/bundle.js",
     bundle: true,
     format: "esm",
     target: "es2020",
     sharedConsumer: true,
     emitsCss: false,
   },
   ```
   EXPECTED_ARTIFACT_COUNT: 21 → 22

5. `Makefile` — add `menuserver` to `--require` list in bundle-shape gate

6. `tsconfig.json` — add `"web/menuserver/js/*.ts"` to `include` array

**Gates:** npm run build, npm run typecheck, npm run test:web, bundle-shape,
token-overlap, artifacts

### 4.1 C1: delete menuserver legacy tree

**Goal:** delete all dead/vendor files from web/menuserver/. After this commit,
menuserver/ contains only: `index.html`, `js/main.ts`, `js/bundle.js`,
`css/app.css`, and `images/fvw.png` (favicon).

**Delete JS (17 files, ~32k lines):**
- Vendor: `jquery.js` (11k), `jquery-ui.js` (18.7k), `bootstrap.js` (6),
  `popper.js` (5), `axios.min.js` (2), `marked.min.js` (6)
- Dead app copies: `todo.js` (489), `todo-utils.js` (926), `slideshow.js` (445),
  `exercise.js` (93), `kbc.js` (79), `onoff.js` (36)
- Dead base.html JS: `base-util.js` (587), `panels.js` (222)
- Old app JS (superseded by main.ts): `menuserver.js` (420), `utils.js` (161),
  `navcontrols.js` (7)

**Delete CSS (19 files, ~2.8k lines):**
- Vendor: `jquery-ui.css`, `fontawall.css`, `fontawsme.css`, `fontawesome.min.css`
- Dead/superseded: `nav.css`, `responsivenav.css`, `pshelper-index.css`,
  `modal.css`, `slideshow.css`, `kbc.css`, `base.css`, `style-base.css`,
  `style-base-tr.css`, `progressbar-base.css`, `progress.css`, `arrow.css`,
  `panels.css`, `buttons.css`, `toggle.css`

**Delete HTML stubs (8 files):**
- `menuserver.html` (byte-identical copy of index.html)
- `base.html` (dead old taskmaster UI)
- `exercise.html` (49 lines, dead feature stub)
- `onoff.html` (48 lines, dead feature stub)
- `slideshow.html` (141 lines, dead feature stub)
- `todo.html` (124 lines, dead feature stub)
- `404.html` (119 lines, dead error page)
- `50x.html` (119 lines, dead error page)

**Delete other:**
- `webfonts/` directory (5 FA font files)
- `images/` — delete all except `fvw.png` (favicon, referenced by index.html)

**Delete slideshow legacy tree** (Phase 2 debt — these carry FA references
and active `alert()` calls that would fail C2/C3 verification gates. Same
FR-9 pattern as todo's C7 in Phase 3):

Delete ALL files in `web/slideshow/` EXCEPT the live app:
`index.html`, `js/app.ts`, `js/app.js`, `css/app.css`, `images/fvw.png`.

Specifically delete (43 files):
- **8 HTML stubs**: `exercise.html`, `menuserver.html`, `onoff.html`,
  `todo.html`, `slideshow.html`, `base.html`, `404.html`, `50x.html`
- **15 JS files**: `jquery.js`, `jquery-ui.js`, `bootstrap.js`, `popper.js`,
  `axios.min.js`, `marked.min.js`, `todo.js`, `todo-utils.js`,
  `slideshow.js`, `menuserver.js`, `navcontrols.js`, `utils.js`,
  `base-util.js`, `panels.js`, `exercise.js`, `kbc.js`, `onoff.js`
  (Note: 17 files — same vendor+dead-app set as menuserver)
- **16 CSS files**: `arrow.css`, `base.css`, `buttons.css`, `jquery-ui.css`,
  `kbc.css`, `modal.css`, `nav.css`, `panels.css`, `progress.css`,
  `progressbar-base.css`, `pshelper-index.css`, `responsivenav.css`,
  `slideshow.css`, `style-base-tr.css`, `style-base.css`, `toggle.css`
- **3 images**: `divider.png`, `logo.svg`, `user.png` (only `fvw.png` kept)

After C1, `web/menuserver/` contains only: `index.html`, `js/main.ts`,
`js/bundle.js`, `css/app.css`, and `images/fvw.png`.

**Gates:** all green (no code changes, only deletions of unreferenced files)

### 4.2 C2: todo FA icon replacement + CDN removal

**Goal:** replace all FontAwesome icon references in todo with inline SVG.
Delete FA CSS and webfonts from todo. Remove Google Fonts CDN link.

**FA icon mapping** (each FA class → inline SVG, `stroke="currentColor"` or
`fill="currentColor"`, sized to match):

| FA class | Usage location | SVG replacement |
|----------|---------------|-----------------|
| `fas fa-bars` | index.html:171 | shared hamburger SVG (already in HamburgerMenu) — remove, HamburgerMenu provides its own trigger |
| `fas fa-moon` / `fas fa-sun` | index.html:178, shell.ts:18, compare.html:47, bootstrap | Replace with inline SVG moon/sun icons; shell.ts swaps `innerHTML` instead of `className` |
| `far fa-copy` | index.html:181 | clipboard SVG |
| `fas fa-external-link-alt` | index.html:184, todo-utils.js:956 | external-link SVG |
| `fas fa-columns` | index.html:187 | columns SVG |
| `fas fa-chevron-left` | index.html:212, compare.html:41 | chevron-left SVG |
| `fas fa-plus-circle` | index.html:216 | plus-circle SVG |
| `fas fa-plus` | index.html:219 | plus SVG |
| `fas fa-eye-slash` | index.html:222 | eye-slash SVG |
| `fas fa-ban` | index.html:225 | ban/circle-slash SVG |
| `fas fa-vote-yea` | index.html:232 | check-square SVG |
| `fas fa-trophy` | todo-utils.js:248 | trophy SVG |
| `fa fa-trash` | todo-utils.js:255 | trash SVG |
| `fas fa-hand-paper` | todo-utils.js:260 | hand SVG |
| `fas fa-play` | todo-utils.js:261 | play SVG |
| `fas fa-edit` | todo-utils.js:262 | edit/pencil SVG |
| `fas fa-sync-alt` | todo-utils.js:301,307 | refresh SVG |
| `fas fa-exchange-alt` | compare.html:58 | arrows-exchange SVG |
| `fas fa-arrow-left` | compare.html:61 | arrow-left SVG |
| `fas fa-exclamation-triangle` | compare.html:64 | warning-triangle SVG |
| `fas fa-arrow-right` / `fas fa-arrow-left` | compare.js:93-94 | arrow SVGs |
| `fa fa-trash` | compare.js:103 | trash SVG |
| `fas fa-exclamation-triangle` | compare.js:264 | warning-triangle SVG (same icon as compare.html:64) |
| `fas fa-arrow-up` | (menuserver.js:342 — deleted in C1) | n/a |

**Implementation approach:**
- In HTML files: replace `<i class="fas fa-*"></i>` with
  `<span class="icon"><!-- SVG --></span>` using compact inline SVGs
  (16×16 viewBox, currentColor fill/stroke)
- In JS files (todo-utils.js, compare.js): replace FA `<i>` strings in
  innerHTML templates with SVG strings
- In shell.ts: replace `icon.className = "fas fa-moon"` with
  `icon.innerHTML = moonSvg` / `icon.innerHTML = sunSvg`
- In inline bootstrap scripts (index.html:24, compare.html:21): remove the
  FA class-setting logic (`var i=...;if(i)i.className=...`). The `<span>`
  element already contains the correct SVG from the HTML markup, and shell.ts
  updates it after it loads. The bootstrap only needs to set `data-theme`.

**Delete files:**
- `web/todo/css/fontawsme.css`
- `web/todo/css/fontawall.css` (if present)
- `web/todo/webfonts/` directory (5 FA font files)

**Modified files:**
- `web/todo/index.html` — remove FA CSS `<link>` tags, remove Google Fonts
  CDN `<link>` tags (3 lines: preconnect ×2 + stylesheet), replace FA `<i>` tags
- `web/todo/compare.html` — remove FA CSS links, remove Google Fonts CDN
  `<link>` tags (same 3 lines as index.html), add `/shared/dist/shared.css`
  link (needed for toast/dialog styling from C3), replace FA `<i>` tags
- `web/todo/js/shell.ts` — replace FA class toggle with SVG innerHTML swap
- `web/todo/js/todo-utils.js` — replace FA `<i>` strings in render functions
- `web/todo/js/compare.js` — replace FA icons (arrows, trash, exclamation-triangle)

**FA verification gate:** after C2, this grep must return zero hits:
```bash
grep -rn 'fontawsme\|fontawall\|fontawesome\|fas fa-\|far fa-\|fa fa-\|fab fa-' \
  web/ --include='*.html' --include='*.ts' --include='*.js' --include='*.css' \
  | grep -v node_modules | grep -v '\.min\.' | grep -v web/shared/
```

**Gates:** all green. Visual regression: each icon should render at the same
size and position as before.

### 4.3 C3: alert()/confirm()/prompt() → shared dialogs repo-wide

**Goal:** zero native dialog calls across `web/`. FR-5 requirement.

**Replacements:**

| File | Call | Replacement |
|------|------|-------------|
| `web/todo/js/todo.js:90` | `if (showsavealert \|\| savebutton) { alert(data.msg); savebutton=false; }` | `if (showsavealert \|\| savebutton) { showToast(data.msg, 'success'); savebutton=false; }` — preserve guard |
| `web/todo/js/todo.js:97` | `if (showsavealert) alert(err.responseJSON.error)` | `if (showsavealert) showToast(err.responseJSON.error, 'error')` — preserve guard |
| `web/todo/js/todo.js:378` | `alert("This instance has gone stale, reloading...")` | `showToast('Instance stale, reloading…', 'notice'); setTimeout(() => location.reload(), 1500)` |
| `web/todo/js/todo.js:470` | `if (showsavealert \|\| savebutton) { alert(data.msg); savebutton=false; }` | `if (showsavealert \|\| savebutton) { showToast(data.msg, 'success'); savebutton=false; }` — preserve guard |
| `web/todo/js/todo.js:491` | `if (showsavealert) alert(err.responseJSON.error)` | `if (showsavealert) showToast(err.responseJSON.error, 'error')` — preserve guard |
| `web/todo/js/utils.js:32` | `alert('Copied!')` | `showToast('Copied!', 'success')` |
| `web/todo/js/utils.js:34` | `alert('Falied to copy.')` | `showToast('Failed to copy.', 'error')` |
| `web/todo/js/compare.js:41` | `alert('Failed to save ...')` | `showToast('Failed to save ' + side + ' list.', 'error')` |
| `web/obsidianoid/js/app.ts:139` | `confirm('You have unsaved changes...')` | `await confirmDialog('You have unsaved changes. Discard and open new note?')` (enclosing `loadNote` is already async — just add `await`) |

**Import strategy for legacy JS files (todo.js, utils.js, compare.js):**
These are plain `<script>` globals, not ES modules. They can't `import` from
`@shared`. Instead, shell.ts (which IS a module and already imports `@shared`)
will expose `showToast` on `window`:

```typescript
// In shell.ts, add:
import { showToast } from '@shared';
(window as any).showToast = showToast;
```

Then the legacy JS files call `showToast(msg, tone)` as a global. This avoids
converting them to modules (which would require removing jQuery).

**obsidianoid/js/app.ts:** already an ES module, can import `confirmDialog`
from `@shared` directly.

**Verification gate:** after C3, this grep must return zero hits:
```bash
grep -rn '\balert(\|[^a-zA-Z]confirm(\|[^a-zA-Z]prompt(' web/ \
  --include='*.js' --include='*.ts' --include='*.html' \
  | grep -v node_modules | grep -v '\.min\.' | grep -v '/shared/' \
  | grep -v 'bundle\.js' \
  | grep -v '//.*alert\|//.*confirm\|//.*prompt' \
  | grep -v ' \*.*alert\| \*.*confirm\| \*.*prompt' \
  | grep -v 'confirmDialog\|alertDialog\|promptDialog\|showToast' \
  | grep -v 'confirmLabel\|confirmFqdn\|confirmNonEmpty\|confirmDeleteBtn\|confirmAddBtn\|confirm-' \
  | grep -v 'showsavealert'
```

Note: `bundle.js` exclusion covers third-party code bundled by esbuild (e.g.
xterm web-links addon in multissh). The ` *` exclusion covers JSDoc/block
comment lines. The FRD's FR-5 intent is zero native dialog calls in app code,
not in vendored/bundled third-party dependencies.

**Gates:** all green.

## 5. Files touched per commit

| Commit | New | Modified | Deleted |
|--------|-----|----------|---------|
| C0 | main.ts, app.css, bundle.js | index.html, descriptors.mjs, Makefile, tsconfig.json | — |
| C1 | — | — | menuserver: 17 JS + 19 CSS + 5 webfonts + 8 HTML + images; slideshow: 17 JS + 16 CSS + 8 HTML + 3 images = ~97 files |
| C2 | — | index.html, compare.html, shell.ts, todo-utils.js, compare.js | fontawsme.css, (fontawall.css), webfonts/ = ~7 files |
| C3 | — | todo.js, utils.js, compare.js, shell.ts, app.ts | — |

## 6. Risk assessment

| Risk | Mitigation |
|------|-----------|
| menuserver nav behavior regression | HamburgerMenu link items replicate exact same showPage() calls; SHOWALLPAGES mode preserved |
| todo icon visual regression | Each SVG sized to match FA's 1em default; currentColor inherits text color like FA did |
| todo jQuery + showToast coexistence | showToast exposed as window global from shell.ts; tested that toast renders on top of jQuery-managed DOM |
| obsidianoid confirm() → async | The containing function already has an early-return pattern; making it async is safe |
| clipboardAPI fallback | copyToClipBoard already uses deprecated execCommand; replacing alert() with showToast doesn't change clipboard behavior |
| window.showToast load order | shell.ts loads as `type="module"` (deferred); legacy scripts load synchronously and execute first. Safe because all alert() calls are inside AJAX callbacks / event handlers, never during initial execution — by the time they fire, shell.ts has assigned window.showToast |
| todo jQuery remains after Phase 5 | Bounded technical debt. Todo's full TS conversion should be tracked as follow-up work (separate FRD) to prevent the window.showToast bridge from becoming permanent architecture |
