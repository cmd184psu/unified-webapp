# FRD: UI Unification — Shared Components, Theming, and CSS Consolidation

Status: DRAFT v2 — open questions resolved 2026-09-15
Date: 2026-09-15
Scope: all 13 existing web modules (`admin`, `certmachine`, `grocery`,
`issuetracker`, `menuserver`, `multissh`, `obsidianoid`, `slideshow`,
`smbedit`, `taskmaster`, `timetracker`, `todo`, `utuber`), one new module
(`sampler`, FR-10), plus the platform login page and the web build pipeline.

**Precondition (SATISFIED 2026-09-15):** the `taskmaster` branch merged to
`main` (PR #12, `6eb4e95`) and into this branch. Verified post-merge: the
web-side diff is strictly additive (`web/taskmaster/*` only, no other module
touched); `ui/modal.ts` exports match FR-5's lift plan; taskmaster is
already in the package.json build chain and tsconfig include; its
`build.go` uses `platform/static`. This FRD accounts for taskmaster below.

---

## 1. Purpose

The 13 module UIs were ported from independent apps and share almost nothing.
Today the tree contains:

- **10 independent design-token palettes** in 4 naming conventions
  (obsidianoid `--color-*`, todo/multissh/certmachine bare `--bg`/`--accent`,
  smbedit `--bg-panel`/`--text-subtle`, taskmaster Obsidian.md-style
  `--bg-primary`/`--text-normal`/`--interactive-accent`), and 3 modules
  (admin, timetracker, menuserver) with no CSS variables at all.
- **Modals implemented 11 times in 7 incompatible class vocabularies**; toasts
  4 times; `.btn` families 6 times; tabs 5 ways; hamburgers/drawers 5 ways with
  5 different glyphs (FA `fa-bars`, inline SVG, `&#9776;`, literal `☰`).
- **Three byte-identical copies of the legacy pshelper tree**
  (todo/slideshow/menuserver): jQuery + jQuery-UI + Bootstrap + Popper + axios
  vendored ×3, ~90k duplicated vendor lines, 244 KB of webfonts ×3. For todo
  and slideshow the copies are entirely dead weight.
- **FontAwesome 4 copies across 2 major versions**, one of them broken
  (timetracker ships the FA6 CSS but only `fa-solid-900.ttf`, so `far` glyphs
  and the preferred woff2 404).
- **3 different theme-persistence strategies**: obsidianoid localStorage
  per-vault, todo localStorage global + `prefers-color-scheme`, smbedit
  server-side via `patchConfig`. slideshow persists theme server-side too.
- A heterogeneous build: 2 modules TS-transpile-only, 4 esbuild-bundled
  (2 React), 6 plain static JS with no build step, 1 (utuber) a single
  816-line HTML file.

This FRD defines a shared, TypeScript-level component library and a unified
theme system so that UI behavior, look, and code are DRY across modules.

**Prime directive: no functionality is lost.** Every existing menu item,
setting, shortcut, and behavior survives migration. Shared components are
containers/primitives that module code feeds via hooks — they never dictate
*what* a module's UI does, only *how it looks and behaves as chrome*.

## 2. Goals / Non-Goals

Goals:

1. One shared token vocabulary and one `themes.css` consumed by every module.
2. A consistent set of selectable color themes, including a **Puma** brand
   theme derived from pumamesh.com.
3. Every module has a hamburger menu with identical behavior, populated
   through a hook API; theme selection lives in that menu in every module.
4. A shared TS component library (`web/shared/`) for the recurring widgets:
   hamburger/drawer, modal, toast, confirm, tabs, buttons/forms, table/list
   helpers, loading states, icons.
5. One build pipeline shape for all modules (TS + esbuild bundle).
6. Delete duplicated/dead CSS and JS; one icon strategy; one FontAwesome
   story (or none).

Non-Goals (this FRD):

- No feature changes inside modules (no new pages, no API changes beyond a
  small shared-static route and optional theme-default plumbing).
- No framework migration: React modules stay React; vanilla modules stay
  vanilla. The shared library is framework-agnostic (see FR-2).
- menuserver's jQuery UI rewrite is scoped as its own final phase (Phase 5)
  and may be split into a separate FRD.

## 3. Architecture Overview

### 3.1 `web/shared/` layout

```
web/shared/
  css/
    tokens.css        # structural tokens: spacing, radius, type scale, motion
    themes.css        # [data-theme=…] color blocks — the single theme source
    components.css    # chrome + widget styles (topbar, drawer, modal, toast…)
  ts/
    theme.ts          # ThemeManager
    menu.ts           # HamburgerMenu
    modal.ts          # Modal / ConfirmDialog
    toast.ts          # Toast (lifted from certmachine's — best in repo)
    tabs.ts           # TabBar
    dom.ts            # el() helper (currently copy-pasted in 5+ files)
    icons.ts          # SVG icon registry
    index.ts          # barrel export
  react/
    theme.tsx         # thin React bindings over theme.ts (smbedit, issuetracker)
    Toast.tsx         # wrapper over toast.ts
  public/
    fonts/            # self-hosted webfonts (see FR-8)
    icons/            # any raster/shared images
```

Layering:

- **Layer 1 — CSS (all 14 modules):** tokens.css + themes.css + components.css.
  Even a module that adopts nothing else gets the token vocabulary and themes.
- **Layer 2 — vanilla TS classes (the 12 non-React modules):** HamburgerMenu,
  Modal, Toast, etc. Plain DOM, no framework.
- **Layer 3 — React bindings (smbedit, issuetracker):** thin wrappers that
  delegate to Layer 2 logic and Layer 1 classes, replacing their bespoke
  `theme.tsx` / `Toast.tsx` internals while keeping their APIs.

### 3.2 How shared code reaches the browser — one shared bundle, not N copies

Each module's Go handler serves only its own `web/<module>/` directory at `/`,
so `web/shared/` is not HTTP-reachable today. Decision (resolved): shared
code is **not** compiled into every module bundle — it is built **once** and
served from a shared route, so the browser caches it across modules and the
repo doesn't commit N copies of the same compiled code.

1. **Shared static route.** `internal/platform/static` gains
   `static.MountShared(mux)`, serving `web/shared/dist/` and
   `web/shared/public/` under `/shared/` with the same Clean/no-dirlist
   discipline as `NewHandler`. Every module's `build.go` calls it (one line
   each); platform config gets `shared_static_dir` defaulting to
   `./web/shared`.
2. **One shared runtime bundle.** The build compiles `web/shared/ts/index.ts`
   → `web/shared/dist/shared.mjs` (ESM) and the shared CSS →
   `web/shared/dist/shared.css`, once.
3. **Modules import it as an external.** Module code imports
   `@shared/...`; tsconfig `paths` maps `@shared/*` → `web/shared/ts/*` for
   typechecking, and each module's esbuild invocation aliases/externals
   `@shared` to the runtime URL `/shared/dist/shared.mjs`. All module entries
   are `<script type="module">` (most already are), so plain ESM imports
   resolve at runtime with no loader. Each `index.html` links
   `/shared/dist/shared.css` before its module CSS.
4. **No version skew:** one repo, one deploy artifact — the shared bundle
   and all module bundles ship together, rebuilt by the same
   `npm run build`. Cache-correctness relies on normal HTTP caching
   (ETag/Last-Modified from the file server); if that ever proves
   insufficient, a content-hash in the filename is the escape hatch.
5. **Exceptions:** React itself stays bundled per React module (2 modules,
   deliberate); tiny pre-paint theme bootstrap (FR-3.1) is inlined per page
   since it must run before any module script loads.

## 4. Functional Requirements

### FR-1: Unified design tokens

One vocabulary, defined once. Naming adopts the obsidianoid/grocery
`--color-*` convention (richest existing set, already used by the two most
idiomatic modules), with reconciliations:

- Status color naming standardizes on **`--color-danger`** (obsidianoid's
  `--color-error` and grocery's `--color-danger*` collide; `danger` wins —
  it matches todo, smbedit's usage, and pumamesh.com's own `--danger`).
- Surfaces become an ordered scale: `--color-bg`, `--color-surface-1`,
  `--color-surface-2`, `--color-surface-3` (replacing the opaque
  `-offset`/`-dynamic`/`-raised`/`-off` names; migration is mechanical).
- Text: `--color-text`, `--color-text-muted`, `--color-text-faint` (3 tiers).
- Accent: `--color-primary`, `-hover`, `-active`, `-tint` (unifying
  obsidianoid `-highlight`, todo `--accent-lite`, grocery `-lite`).
- Borders: `--color-border`, `--color-divider`.

Structural tokens live in `tokens.css` on `:root` **once** (not per theme —
obsidianoid currently restates all 37 tokens in each of 5 theme blocks):

- Spacing `--space-1..8` (full scale incl. `--space-5`, which grocery has and
  obsidianoid lacks), radius `--radius-sm/md/lg/full`, fluid type scale
  `--text-xs/sm/base/lg/xl`, `--font-body`, `--font-mono`,
  `--shadow-sm/md`, `--transition`, `--topbar-height`, `--sidebar-width`.

Theme blocks in `themes.css` define **colors and shadows only**.

Acceptance: `themes.css` + `tokens.css` are the only files defining these
custom properties; a grep for `--color-error`, `--accent-lite`,
`--surface-raised`, `--bg-panel` etc. across `web/` returns only vendored
files slated for deletion.

### FR-2: Theme roster

`[data-theme="…"]` blocks, one palette each. Initial roster (8):

| Theme | Mode | Source |
|---|---|---|
| `light` | light | todo-light / grocery palette (already identical: bg `#f4f2ee`, teal accent `#01696f`) |
| `dark` | dark | todo-dark palette (neutral GitHub-ish: bg `#0d1117`, accent `#7c3aed`) |
| `obsidian` | dark | obsidianoid's current `dark` (bg `#13131a`, violet `#7c6af7`) — **renamed** (decision confirmed): the violet identity moves with the theme rather than squatting on the generic `dark` name. Precedent: grocery's dark rendering problems trace to exactly this kind of palette-identity mixing. taskmaster's near-identical violet (`#7f6df2` on `#1e1e1e`) folds into this theme rather than becoming an 11th palette |
| `forest` | dark | obsidianoid |
| `ocean` | dark | obsidianoid |
| `ember` | dark | obsidianoid |
| `rose` | dark | obsidianoid |
| `puma` | dark | pumamesh.com (below) |

**Puma theme** (captured 2026-09-15 from `pumamesh.com/assets/css/styles.css`):

```css
[data-theme="puma"] {
  --color-bg: #081119;
  --color-surface-1: #0d1823;
  --color-surface-2: #0e1925;          /* site: rgba(14,25,37,.84) card */
  --color-surface-3: #13202e;
  --color-border: rgba(255, 255, 255, 0.11);
  --color-divider: rgba(255, 255, 255, 0.2);
  --color-text: #ecf3f6;
  --color-text-muted: #9fb2bd;
  --color-text-faint: #728796;
  --color-primary: #3fbf9c;            /* mint teal accent */
  --color-primary-hover: #6cd7b4;      /* accent-strong */
  --color-primary-active: #35a888;
  --color-primary-tint: rgba(63, 191, 156, 0.12);
  --color-danger: #ff8a8a;
  --color-success: #3fbf9c;
  --color-warning: #fbbf24;            /* amber from the PumaMesh logo mark */
}
```

The puma theme also carries brand fonts as a per-theme override:
`--font-body: "Sora", var(--font-body-fallback)` and
`--font-mono: "IBM Plex Mono", var(--font-mono-fallback)` — self-hosted (FR-8),
never loaded from Google's CDN (these apps must work on closed LANs).

Adding a theme = adding one CSS block + one entry to a single `THEMES` array
in `theme.ts` (today obsidianoid duplicates its theme list between CSS and TS;
the array remains the one JS-side source and is exported for pickers).

### FR-3: ThemeManager (`web/shared/ts/theme.ts`)

One class, three existing strategies unified via hooks:

```ts
const themes = new ThemeManager({
  module: "todo",                    // namespaces the localStorage key
  default: "dark",                   // module's shipped default
  serverDefault: () => cfg.theme,    // optional: server-provided default
  storageKey: () => `obsidianoid-theme-${state.activeVault}`, // optional override
  onChange: (name) => { ... },       // optional: e.g. smbedit patchConfig({theme})
});
```

Behavior (all mandatory):

1. Sets `document.documentElement.dataset.theme`; must run **before first
   paint** (a small inline bootstrap or head-loaded script — todo's
   `theme.js` pattern, generalized). Default `data-theme` remains in the
   HTML markup as FOUC insurance.
2. Resolution order: localStorage → `serverDefault()` → `system`
   (via `prefers-color-scheme`, mapping to `light`/`dark`) → `default`.
3. Persistence: localStorage under `ui-theme:<module>` unless `storageKey`
   overrides (obsidianoid keeps per-vault persistence — behavior preserved).
   `onChange` lets smbedit/slideshow keep their server-side persistence.
4. `system` is a selectable pseudo-theme (smbedit's tri-state, promoted to
   everyone), live-updating on `matchMedia` change.
5. Exposes `themes.list` for pickers; picker UI itself is rendered by
   HamburgerMenu (FR-4), styled from `components.css` with swatches
   (obsidianoid's `.theme-btn`/`.theme-swatch` pattern, with the hard-coded
   `rgba(255,255,255,.15)` swatch border replaced by a token so it works on
   light themes).

### FR-4: HamburgerMenu (`web/shared/ts/menu.ts`) — hook-fed

Every module gets the same hamburger: same glyph (one shared inline-SVG
`fa-bars`-equivalent from `icons.ts`), same placement (topbar, left), same
open/close behavior (todo's slide-in drawer + backdrop pattern — the most
complete implementation in the repo), same a11y (none of the current six
implementations has `aria-expanded`, Escape handling, or a focus trap; the
shared one has all three).

**The class owns the chrome; modules own the contents via hooks:**

```ts
const menu = new HamburgerMenu({
  title: "TimeTracker",
  items: [
    { id: "export", label: "Export CSV", icon: "download", onSelect: exportCsv },
    { id: "rewind", label: "Rewind", onSelect: openRewind, when: () => hasHistory() },
    { separator: true },
    { id: "cols", render: (host) => mountColumnToggles(host) },  // custom DOM slot
  ],
  themePicker: true,          // standard ThemeManager-backed picker section
  onOpen, onClose,            // lifecycle hooks
});
menu.addItem(...); menu.removeItem(id); menu.updateItem(id, patch);
```

Item kinds: action (label/icon/onSelect), link (href), separator, section
header, and **`render` slots** for arbitrary module DOM (selects, checkbox
groups, number inputs). The `render` slot is what guarantees zero
functionality loss — anything that doesn't fit a standard kind is mounted
verbatim.

**Preservation inventory** — existing menu/drawer contents that MUST be
re-registered one-for-one:

| Module | Current hamburger contents → migration |
|---|---|
| todo | Subject select, List select, Move-to-subject select + button, Columns checkbox group (votes/period/next_due/cooldown), Vote Cooldown input → `render` slots + items. Theme toggle currently *outside* the menu becomes the standard themePicker section; the topbar quick-toggle button may remain as a bonus, backed by the same ThemeManager. |
| obsidianoid | Theme swatch panel → the standard themePicker section (its only current content). Topbar actions (autosave, mode, save, new note, git sync) stay in the topbar — unchanged. |
| slideshow | Settings drawer: mode/interval/shuffle/controls-pos selects + checkboxes, theme select → `render` slots; theme select becomes themePicker (server persistence kept via `onChange`). |
| smbedit | Settings drawer contents → items/slots via the React binding; tri-state theme switcher becomes themePicker with `system`. |
| utuber | Settings panel contents → slots; mode tabs stay in-page. |
| menuserver | Nav dropdowns are *navigation*, not settings — the hamburger gains its nav links as link items (Phase 5). |
| admin, certmachine, grocery, issuetracker, multissh, taskmaster, timetracker | Net-new hamburger: themePicker + whatever module actions naturally belong there (e.g. admin logout, certmachine Tools menu entries may move or stay — moving is optional, not required). |

Acceptance: after migration, a checklist per module confirms every
pre-existing control is reachable and functional; the drawer passes
keyboard-only operation (open, navigate, Escape-close, focus return).

### FR-5: Shared widget library

All lifted from the best existing implementation, generalized:

| Component | Base implementation | Replaces |
|---|---|---|
| `Modal` / dialogs | **taskmaster `js/ui/modal.ts`** (390 lines), which already exports promise-based `openModal`/`confirmDialog`/`alertDialog`/`promptDialog` — exactly the intended API. Generalized: theme-token styling, focus trap, Escape/focus-return; optionally re-based on native `<dialog>` (obsidianoid's pattern) as an internal detail | 7 incompatible modal vocabularies across 11 modules; native `confirm()` in issuetracker (×3) and smbedit (×1) — certmachine's documented ban on `alert()/confirm()/prompt()` becomes repo-wide policy, satisfied by these dialogs |
| `Toast` | certmachine `toast.ts` (aria-live, per-tone auto-dismiss, sticky errors, textContent-only) | smbedit Toast.tsx internals, obsidianoid `#toast`, utuber `#err-toast`, admin/timetracker ad-hoc status lines, issuetracker's unimplemented `.toast` |
| `TabBar` | grocery's semantic `role=tablist` markup + multissh `tabs.ts` logic | 5 tab patterns |
| Buttons/forms (CSS-only) | smbedit's `.btn` family + multissh `.field` naming | 6+ `.btn` reimplementations, 4 form vocabularies |
| Table/list styles (CSS-only) | todo `.item-table` + certmachine `.cert-row` card-list | per-module table CSS |
| Loading | obsidianoid skeletons + a standard `.loading` line + spinner | 8 duplicated "Loading…" literals in issuetracker alone |
| `el()` DOM helper | certmachine/multissh's (currently copy-pasted in ≥5 files) | all copies |
| `Icon` registry | inline SVG, `stroke="currentColor"` (admin/grocery/obsidianoid style) | 5 icon strategies (FA classes, emoji, entities, SVG, none) |

Escaping discipline: shared components render text via `textContent` only —
no `innerHTML` of interpolated strings (certmachine's rule, repo-wide).

### FR-6: Icon and font consolidation

- **Icons: one shared SVG registry** (`icons.ts`), tree-shaken into each
  bundle. FontAwesome is **removed** once todo and timetracker (the only
  modern FA consumers) are migrated — that deletes 4 copies of
  `fontawesome.min.css`, ~730 KB of webfonts, and both FA generations, and
  fixes timetracker's broken glyph set. Emoji-as-icons (smbedit,
  issuetracker nav) migrate to the registry.
- **Fonts: self-hosted** under `web/shared/public/fonts/`, served via the
  `/shared/` route: Inter (current body font), JetBrains Mono, plus Sora and
  IBM Plex Mono for the puma theme. Google Fonts CDN `<link>`s (grocery,
  smbedit, todo) are removed — LAN-deployable, no external dependency.

### FR-7: Build standardization

Every module converges on the same shape:

1. **Entry:** `web/<module>/src/main.ts` (or `.tsx`), `--bundle`,
   `--target=es2020`, `--format=esm`, output `web/<module>/js/bundle.js` +
   `bundle.css`, with `@shared` externalized per §3.2. Module CSS covers
   module-specific styles only; shared tokens/themes/components come from
   the separately linked `/shared/dist/shared.css`. The transpile-only mode
   (obsidianoid, slideshow) is retired. taskmaster already matches this
   shape (`web/taskmaster/js/main.ts`, esbuild `--bundle`, ESM script tag)
   and needs only the `src/` layout normalization and `@shared` adoption.
2. **package.json:** the per-module `&&` chain becomes a small
   `scripts/build-web.mjs` driving the esbuild JS API over a module list
   (shared bundle first, then modules) — adding a module is a one-line
   change; `--sourcemap` dev mode and the test bundling reuse the same
   list. taskmaster's `--define:__TM_BUILD_TIME__` style per-module flags
   are supported via per-entry options in the list.
3. **tsconfig:** `include` becomes `["web/*/src/**/*", "web/shared/**/*"]`
   with `paths: {"@shared/*": ["web/shared/ts/*"]}` — no more per-module
   enumeration.
4. **Plain-JS modules (admin, grocery, timetracker, todo, menuserver,
   utuber) move into the pipeline**: existing JS is moved to `src/` and
   converted file-by-file to TS (mechanical rename + minimal typing first;
   deep typing is follow-up). utuber's 816-line single file is split into
   `index.html` + `src/main.ts` + `src/styles.css`.
5. Committed bundle artifacts: **unchanged policy** (bundles stay in git so
   `make build` needs no node) — but `make build` gains a dependency note
   and CI check that bundles are not stale (`npm run build && git diff
   --exit-code web/*/js/`).

### FR-8: Go-side changes (minimal)

1. `internal/platform/static`: add `MountShared(mux)` serving
   `web/shared/public/` at `/shared/` (same handler discipline); called from
   each module's `build.go`.
2. utuber's verbatim duplicate of `platform/static` in
   `internal/utuber/build.go` is replaced with the platform handler.
3. Optional per-module `theme` config default (obsidianoid vaults and
   smbedit already have server-side theme state; the pattern generalizes to
   a `Theme string` field with default `""` = client decides).
4. Confirmed: no other Go-side module hygiene needed — one Go module,
   no replaces, no external `github.com/puma/*` imports remain.

### FR-9: Dead-weight deletion

- Delete the legacy pshelper trees from `web/todo/` and `web/slideshow/`
  (byte-identical dead copies of menuserver's: 19 CSS files, 17 JS files
  incl. jQuery/jQuery-UI/Bootstrap/Popper/axios, 8 HTML stubs, webfonts,
  images — ×2). menuserver keeps its copy until Phase 5.
- Delete todo's third dead hamburger (`base.html` `#rightToggle` +
  `panels.js` + `responsivenav.css` usage).
- Delete `web/timetracker/marked.min.js` duplicate once bundling imports it
  (or moves it to a shared vendored location).

### FR-10: `sampler` — module 14, the component gallery

A new module whose single scrollable page visually showcases every shared
UI element, so the whole library can be validated in one place (per theme)
instead of visiting 13 modules — and whose source demonstrates how easy (or
hard) the library is to consume.

1. **Standard module shape**, deliberately: `web/sampler/src/main.ts` +
   `styles.css`, a minimal `internal/sampler/build.go` mounting
   `platform/static` + `MountShared`, a `static_dir` config entry defaulting
   to `./web/sampler`, behind the same auth gate as everything else. Building
   it *is* the test of the "adding a module" workflow and of the net-new
   hamburger adoption path (no legacy menu to preserve).
2. **Coverage: every shared element, one after another** — token/color
   swatch grid for the active theme; type scale; buttons (all variants and
   states incl. disabled); form fields; tabs; table and card-list; loading
   states (text, skeleton, spinner); toasts (one trigger per tone, incl.
   sticky error); modal, confirm, alert, prompt triggers; icon registry
   grid; and the hamburger itself, configured with the standard themePicker
   plus example action items, a `when()`-guarded item, and a `render` slot —
   i.e. every hook kind exercised.
3. **Each section shows the component next to the source snippet that
   produced it** (escaped `<pre>` of the actual call). The sampler doubles
   as the library's living documentation; future modules copy from it.
4. **Role in phasing: dev harness first.** The sampler skeleton lands in
   Phase 1 and each shared component is developed *against* it in Phases
   1–2, before any real module adopts. It also end-to-end exercises the
   `/shared/` route, the external shared bundle, and the build-script
   module list before real modules depend on them.
5. **Ships enabled** in the production binary (static, harmless,
   auth-gated); a config flag to disable it is optional, not required.
6. Acceptance: no shared component or theme exists that the sampler does
   not display; switching any of the 8 themes restyles every section with
   no hard-coded stragglers; a keyboard-only pass works end to end.

## 5. Non-Functional Requirements

- **A11y floor for shared components:** `aria-expanded`/`aria-controls` on
  the hamburger, focus trap + Escape + focus-return on drawer and modals,
  `role=status aria-live=polite` on toasts, `role=tablist` semantics on tabs,
  visible focus rings from tokens.
- **No FOUC:** theme applied pre-paint in every module (FR-3.1).
- **No external network dependencies** at runtime (fonts self-hosted, FR-6).
- **Light-theme correctness:** all hard-coded `rgba(0,0,0,…)`/
  `rgba(255,255,255,…)` shadows and borders in migrated CSS become tokens;
  known offenders: obsidianoid swatch border + `#vault-selector` SVG chevron
  (`%237878a0`), todo winner-row `#111`/`#fff !important` overrides,
  multissh's xterm theme object (`terminal.ts:33`) which must read resolved
  CSS variables on theme change.
- **Type safety:** `tsc --noEmit --strict` stays green as modules convert.
- **Tests:** shared `theme.ts`, `menu.ts`, `toast.ts`, `modal.ts` get unit
  tests in the existing esbuild-bundle-to-node harness; grocery's
  `app.test.js` keeps passing.

## 6. Phasing

| Phase | Deliverable | Modules touched |
|---|---|---|
| 0 | **Precondition:** taskmaster branch → main → this branch — **DONE** (PR #12, `6eb4e95`) | merge only |
| 1 | `web/shared/` skeleton: tokens.css, themes.css (8 themes incl. puma), components.css base; shared bundle build + `MountShared` + `/shared/` route; build script + tsconfig rework; Modal lifted from taskmaster `ui/modal.ts` into shared; **sampler module skeleton (FR-10) as the dev harness** | build/platform + taskmaster (modal donation) + sampler (new) |
| 2 | ThemeManager + HamburgerMenu + Toast, each developed against the sampler as it lands; then adopt in the two references: **obsidianoid** (its dark→`obsidian`, drop duplicated theme list) and **todo** (drawer re-homed onto shared class, dead tree deleted) | sampler, obsidianoid, todo |
| 3 | Adopt in the modern vanilla modules: taskmaster, certmachine, multissh, grocery, admin, timetracker (incl. TS conversion for grocery/admin/timetracker; timetracker loses FA; taskmaster retires its local tokens for `obsidian`+roster) | 6 modules |
| 4 | React bindings; smbedit + issuetracker migrate tokens/theme/toast/confirm; slideshow modern app migrates; utuber split into files + migrated | 4 modules |
| 5 | menuserver rewrite off jQuery onto the shared library; delete its legacy tree; FA fully removed repo-wide | menuserver |

Each phase ends with the preservation checklist (FR-4 acceptance) for the
modules it touched, plus `npm run typecheck`, `npm run test:web`, `make test`.

## 7. Resolved Decisions (2026-09-15 review)

1. **Build uniformity + no repeated bundling** — all modules build the same
   way, and shared content is NOT compiled into each module bundle. Resolved
   by the single shared runtime bundle served at `/shared/` (§3.2).
   Committed-bundle policy unchanged (bundles stay in git); the shared
   bundle is one more committed artifact, and module bundles shrink.
2. **`dark` identity** — the violet palette moves with the theme: it becomes
   `obsidian`, and `dark` is the neutral palette. (Grocery's dark-theme
   problems are remembered fallout from mixing palette identities.)
3. **Light variants** of forest/ocean/ember/rose/puma — out of scope for v1.
4. **certmachine/multissh look shift** onto unified tokens — accepted.
5. **menuserver Phase 5** — in scope in this FRD. Reaffirmed requirement:
   reusable, theme-respecting modal dialogs everywhere; zero calls to
   `alert()`/`confirm()`/`prompt()` repo-wide (FR-5's dialog set is the
   replacement).
6. **taskmaster is the 13th module** — merged in before planning (Phase 0).
   Its `ui/modal.ts` is the donor for the shared Modal; its Obsidian-style
   local tokens are retired in favor of the shared vocabulary, with its
   violet look preserved by the `obsidian` theme.
7. **`sampler` is the 14th module** (FR-10) — a net-new component gallery
   built in Phase 1 as the shared library's dev harness, kept forever as
   the one-page theme/component validation surface and living docs.
