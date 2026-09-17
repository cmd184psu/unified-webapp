# Hamburger / Drawer / Settings-Menu Inventory (Phases 2–4 pre-work)

> **Purpose:** Definitive, verified census of every existing hamburger, drawer,
> and settings-menu implementation across the 13 web modules, expanding the
> coarse preservation-inventory table in `docs/FRD-ui-unification.md` (FR-4).
> The migration goal is **zero functionality loss** when each module is moved
> onto the shared `HamburgerMenu` class (`web/shared/ts/menu.ts`).
>
> **Generated:** 2026-09-15. Describes current behavior on branch `ui-upgrade`.
> All paths are relative to the repo root; line numbers refer to the working
> tree at generation time.

**Proposed shared-menu kinds** referenced below: `action` (id/label/icon/onSelect),
`link` (href), `separator`, `section` (header), `render` (arbitrary module DOM
slot), `themePicker` (standard ThemeManager-backed picker section).

---

## admin

**Existing hamburger/drawer:** **None.** The UI is a single page of stacked
panels (`web/admin/index.html`) under a static topbar (`header.topbar`,
index.html:10–15). No theme UI anywhere (no `data-theme`, no
`prefers-color-scheme` handling, no localStorage).

**Net-new hamburger candidates** (FRD lists admin as net-new; candidates only,
no moves prescribed):

| Order | Label | Control type | Handler (file:line) | Persistence | Proposed shared-menu kind |
|---|---|---|---|---|---|
| 1 | Sign out (icon-only logout button, inline-SVG glyph) | button | `web/admin/index.html:12–14`; handler `web/admin/app.js:770–772` (`POST /api/auth/logout`) | server session | `action` (candidate; may stay in topbar) |
| — | Theme switching | — | none today | — | `themePicker` (net-new) |

The six content panels (Module Access matrix, API Keys, LDAP, Session,
Passkeys, Operator PIN — index.html:19–130) are page content, not menu
candidates.

**Dead/duplicate menu code:** none found.

---

## certmachine

**Existing hamburger/drawer:** **None.** Entire UI is TS-rendered into
`#cert-app` (`web/certmachine/index.html:10`). There **is** a dropdown menu:
the **Tools menu**, a native `<details>`/`<summary>` element in the toolbar —
`buildToolsMenu()`, `web/certmachine/js/ui.ts:191–229`, appended at
`ui.ts:350–352`. Open/close is native `<details>` disclosure (no backdrop, no
Escape handling, no aria-expanded management beyond what `<details>` gives
natively, no focus trap). No theme UI anywhere in the module.

**Tools menu contents (in order):**

| Order | Label | Control type | Handler (file:line) | Persistence | Proposed shared-menu kind |
|---|---|---|---|---|---|
| 1 | Tools (trigger) | `<summary>` text | `web/certmachine/js/ui.ts:192–195` | — | (becomes hamburger trigger or stays a toolbar menu) |
| 2 | Download root CA | link (`<a href>`) | `ui.ts:199–204` → `GET /api/ca/root.crt` (shown only when CA exists) | — | `link` (with `when:` guard) |
| 3 | Re-import legacy certificates | button | `ui.ts:206–214` → `openImportWizard()` (shown only when `legacyImportAvailable`) | — | `action` (with `when:` guard) |
| 3b | "Legacy import unavailable: …" | disabled text (shown instead of 3 when `legacyImportDir` set but unavailable) | `ui.ts:215–219` | — | `render` slot (disabled text has no standard kind) |
| 3c | "No tools available." | disabled text (empty-menu fallback) | `ui.ts:221–225` | — | handled by shared empty-state or `render` |

**Other toolbar/menu-like candidates for a net-new hamburger** (all in
`renderChrome()`, `ui.ts:283–353`): search input (:297–305), sort select
(:307–319), sort-direction button (:321–329), "Group by domain" checkbox
(:331–339), "New certificate" button (:341–348). These are working-surface
controls and most naturally stay in the toolbar; the Tools entries are the
strongest hamburger candidates.

**Dead/duplicate menu code:** none found.

---

## grocery

**Existing hamburger/drawer:** **None.** All actions live in the app header
(`web/grocery/index.html:15–93`) as icon buttons (inline SVG,
`stroke="currentColor"`). No theme UI anywhere (no `data-theme`, DM Sans from
Google Fonts CDN, single palette).

**Net-new hamburger candidates** (header controls, on-screen order):

| Order | Label | Control type | Handler (file:line) | Persistence | Proposed shared-menu kind |
|---|---|---|---|---|---|
| 1 | Edit list title (title text is the button) | button | `index.html:24`; `app.js:124` → `openTitleModal` → `POST /api/config/title` (`app.js:108`) | server | `action` (likely stays as title affordance) |
| 2 | Grocery / Recipes tabs | `role=tablist` buttons | `index.html:27–30`; tab state saved `app.js:795`, restored `app.js:1986` | localStorage `grocery.activeTab` | stays in-page (FR-5 TabBar) |
| 3 | Hide 'Not Needed' items (3-state cycle: show_all → hide_not_needed → hide_completed) | button (aria-pressed, cycling) | `index.html:43–49`; `app.js:896–900`, state names `app.js:139` | none (session only) | `action` or `render` (3-state cycle is awkward as a plain action — flag) |
| 4 | Collapse all sections | button | `index.html:52–59`; `app.js:871` | none (deliberately unpersisted, `app.js:32–35`) | `action` |
| 5 | Expand all sections | button | `index.html:60–67`; `app.js:872` | none | `action` |
| 6 | Reset list to Check state | button (opens confirm modal) | `index.html:70–76`; `app.js:924` → `POST /api/reset` (`app.js:917`) | server | `action` |
| 7 | Manage groups | button (opens groups modal) | `index.html:79–84`; `app.js:1140` → `openGroupsModal` (`app.js:1077`) | server (`/api/config/groups*`) | `action` |
| 8 | Sync toggle | checkbox styled as switch | `index.html:87–91`; `app.js:1923–…` | none (in-memory `syncEnabled`) | `render` slot (toggle switch) or `action` with state — flag: it is a stateful switch, not a one-shot action |
| — | Theme switching | — | none today | — | `themePicker` (net-new) |

Buttons 3, 6, 7 are grocery-tab-only (hidden on Recipes tab, `app.js:306`,
`app.js:814–816`) — the shared menu needs `when:`/`updateItem` support for
this per-tab visibility.

**Dead/duplicate menu code:** none found.

---

## issuetracker

**Existing hamburger/drawer:** **None.** Navigation is a permanent left
sidebar (`<nav className="sidebar">`, `web/issuetracker/src/App.tsx:35–64`)
with emoji icons. `AccountBar` (`src/components/AccountBar.tsx`) shows
identity only — **no logout control exists**. No theme UI anywhere.

**Net-new hamburger candidates** (sidebar nav links, on-screen order — all
React Router `NavLink`s rendered from arrays at `App.tsx:17–33`):

| Order | Label | Control type | Handler (file:line) | Persistence | Proposed shared-menu kind |
|---|---|---|---|---|---|
| 1 | Workspace (section) | section label | `App.tsx:40` | — | `section` |
| 2 | Issues | link → `/issues` | `App.tsx:18` | — | `link` |
| 3 | Board | link → `/board` | `App.tsx:19` | — | `link` |
| 4 | My Issues (auth only) | link → `/my-issues` | `App.tsx:22` | — | `link` with `when:` |
| 5 | My Board (auth only) | link → `/my-board` | `App.tsx:23` | — | `link` with `when:` |
| 6 | Stories | link → `/stories` | `App.tsx:26` | — | `link` |
| 7 | Epics | link → `/epics` | `App.tsx:27` | — | `link` |
| 8 | Settings (section) | section label | `App.tsx:51` | — | `section` |
| 9 | Projects | link → `/projects` | `App.tsx:30` | — | `link` |
| 10 | Tags | link → `/tags` | `App.tsx:31` | — | `link` |
| 11 | API | link → `/api` | `App.tsx:32` | — | `link` |
| — | Theme switching | — | none today | — | `themePicker` (net-new) |

Note: the sidebar itself presumably stays (desktop nav); a hamburger would
duplicate these as links on narrow screens (taskmaster's `menu-nav` section
pattern). React module — needs the React binding like smbedit.

**Dead/duplicate menu code:** none found.

---

## menuserver

**Existing hamburger:** **Yes — a responsive-topnav collapse hamburger**, not
a drawer.

- **Trigger:** an `<a class="icon">` appended last into `#myTopnav`,
  glyph `&#9776;` (HTML entity, no icon font) —
  `addMajorMenuHamburger()`, `web/menuserver/js/menuserver.js:266–268`,
  called at `menuserver.js:411`.
- **Panel markup:** none — the nav itself (`<div class="topnav" id="myTopnav">`,
  `web/menuserver/index.html:41–42`) is populated dynamically with
  `.dropdown` blocks (`addMajorMenu()`, `menuserver.js:259–263`;
  `dropDownButton()` :237–239 uses FontAwesome `fa fa-caret-down`;
  `arrayToDropDown()` :241–257 builds `.dropdown-content` links).
- **Open/close:** `openMenu()` (`web/menuserver/js/navcontrols.js:1–8`)
  toggles the `responsive` class on `#myTopnav` (mobile collapse only,
  styled by `css/responsivenav.css`). Desktop dropdowns are pure CSS hover.
- **Dismiss:** none — no backdrop, no Escape, no outside-click.
- **A11y:** none — no `aria-expanded`, no focus trap, no focus return.

**Contents:** entirely **data-driven navigation** — top-level dropdown
subjects and entries come from `menus/*.json` fetched at runtime
(`menuserver.js:380–417`). Each entry is either an external URL link
(`target="_blank"`, `menuserver.js:250`) or `showPage('<id>')`
(`menuserver.js:253`, showPage at :349) switching in-page sections rendered
into `#lowerSection`. Item labels/count are deployment data, not code.

| Order | Label | Control type | Handler (file:line) | Persistence | Proposed shared-menu kind |
|---|---|---|---|---|---|
| 1..N | (data-driven subjects) | CSS-hover dropdown button | `menuserver.js:259–263` | — | `section` per subject (Phase 5) |
| 1..M per subject | (data-driven entries) | links (`showPage` or external URL) | `menuserver.js:241–257` | — | `link` / `action` (showPage) |
| last | ☰ (mobile collapse) | `<a class="icon">` | `menuserver.js:266–268`; `navcontrols.js:1–8` | — | replaced by shared hamburger chrome |
| — | Theme switching | — | none today | — | `themePicker` (net-new) |

Per FRD, nav dropdowns are *navigation* and move into the shared hamburger as
link items in **Phase 5**. Awkward bits to flag: two-level hierarchy
(subject → entries) needs `section` + items; `showPage()` entries are
`action`s, not `link`s; the whole structure must be built after an async
fetch (menu must support late `addItem`).

**Dead/duplicate menu code in this tree:**
- `web/menuserver/base.html:26–32` — the dead `#rightToggle` three-`<span>`
  CSS hamburger + `#leftToggle` arrow panel, driven by
  `web/menuserver/js/panels.js` (right-panel toggle :39–53; placeholder
  "Option 1/2/3" list in base.html:50–54). `base.html` is reachable as a
  static file but linked from nowhere.
- `web/menuserver/menuserver.html` is **byte-identical** to
  `web/menuserver/index.html` (verified by diff) — duplicate dead entry page.
- `css/nav.css` is linked only from a commented-out line
  (`index.html:9`) — dead.
- The tree also carries `js/todo.js`, `js/todo-utils.js`, `js/slideshow.js`,
  `js/kbc.js`, `js/exercise.js`, `js/onoff.js` + matching HTML pages —
  legacy standalone pages, outside menu scope here.

---

## multissh

**Existing hamburger/drawer:** **None.** UI is TS-rendered into `#ssh-app`
(`web/multissh/index.html:10`): a persistent left host rail
(`js/hosts.ts`, mounted `js/tabs.ts:26`) plus a two-tab pane
(`js/tabs.ts:33–39`). No theme UI anywhere; no localStorage use at all
(deliberate — `js/types.ts:28`).

**Net-new hamburger candidates:**

| Order | Label | Control type | Handler (file:line) | Persistence | Proposed shared-menu kind |
|---|---|---|---|---|---|
| 1 | SSH Console (tab) | button | `web/multissh/js/tabs.ts:37`, activation :59–71 | — | `link`/`action` (likely stays as FR-5 TabBar) |
| 2 | Upload to Host (tab) | button | `js/tabs.ts:38` | — | `link`/`action` (likely stays as TabBar) |
| — | Theme switching | — | none today | — | `themePicker` (net-new) |

Everything else (per-host "Copy ssh command" `js/hosts.ts:292–…`, per-terminal
Connect/Disconnect/Interrupt `js/terminal.ts:237–247`, upload broadcast
`js/upload.ts:100–102`) is per-widget working surface, not menu material.
multissh is the thinnest net-new case: realistically themePicker only.

**Dead/duplicate menu code:** none found.

---

## obsidianoid

**Existing hamburger:** **Yes — a theme-panel popover** (its only content).

- **Trigger:** `#btn-hamburger` in the topbar actions cluster, inline-SVG
  three-line glyph (`stroke="currentColor"`) —
  `web/obsidianoid/index.html:54–56`.
- **Panel markup:** `#theme-panel` (`index.html:61–64`), a fixed popover
  below the topbar; options built at runtime by `buildThemePanel()`
  (`web/obsidianoid/js/app.ts:433–442`).
- **Open/close:** `toggleThemePanel()` toggles the `hidden` attribute
  (`app.ts:444–447`); trigger listener `app.ts:449`.
- **Dismiss:** outside click closes (document click handler,
  `app.ts:450–453`). **No Escape**, no backdrop/scrim.
- **A11y:** none of the three — no `aria-expanded`, no focus trap, no focus
  return. Trigger has `title="Settings"` only.

**Contents (in order):**

| Order | Label | Control type | Handler (file:line) | Persistence | Proposed shared-menu kind |
|---|---|---|---|---|---|
| 1 | "Theme" heading | static `<p>` | `index.html:62` | — | `section` (subsumed by themePicker) |
| 2–6 | Dark / Forest / Ocean / Ember / Rose | `.theme-btn` buttons with `.theme-swatch` color chip | built `app.ts:433–442`; `setTheme()` `app.ts:427–431` sets `documentElement.dataset.theme` | localStorage `obsidianoid-theme-<vaultIndex>` (`app.ts:425`), **per-vault**; vault default from server `VaultInfo.theme` (`app.ts:478–479`, :493–494) | `themePicker` (the FRD's designated base pattern; keep per-vault `storageKey` override) |

**Theme facts:** 5 themes — `dark, forest, ocean, ember, rose`
(`THEMES`, `app.ts:46–52`); **no light theme**; swatch colors are per-theme
accent hexes. Default markup `data-theme="dark"` (`index.html:2`).

**Topbar actions that stay in the topbar** (per FRD, unchanged — listed for
completeness): mode switcher Notes/Threads (`index.html:21–30`), Auto-save
toggle (`index.html:34`, handler `app.ts:310`), Edit/Preview toggle
(`index.html:38`), Save (`index.html:42`, `app.ts:308`), New note
(`index.html:46`, `app.ts:316`), Git sync (`index.html:50`, `app.ts:380`);
vault selector in sidebar header (`index.html:71`, `app.ts:500`).

**Dead/duplicate menu code:** none (`js/app.js` / `js/threads.js` are the
transpiled outputs of the `.ts` sources — build artifacts, not dead menus).

---

## slideshow

**Existing hamburger:** **Yes — the settings panel drawer.**

- **Trigger:** `#btn-hamburger` in the top overlay bar, glyph `&#9776;`
  (HTML entity), with `aria-label="Settings"` —
  `web/slideshow/index.html:24`.
- **Panel markup:** `#settings-panel` (`index.html:61–121`), right-side
  panel; scrim `#settings-scrim` (`index.html:58`).
- **Open/close:** `openSettings()`/`closeSettings()` toggle the `hidden`
  attribute on panel + scrim (`web/slideshow/js/app.ts:436–441`).
- **Dismiss:** scrim click (`app.ts:441`), close button `#btn-close-settings`
  (`index.html:64`, `app.ts:440`), **Escape closes** (`app.ts:471`).
- **A11y:** `aria-label` on trigger and close button, **Escape handling
  present**; but no `aria-expanded`, no focus trap, no focus return.

**Contents (in order):** all persistence is **server-side** via
`POST /api/control` (`control()`, `app.ts:418–426`); values echo back over
SSE `/api/events` and are re-applied to the controls (`applyState`,
`app.ts:333–337`).

| Order | Label | Control type | Handler (file:line) | Persistence | Proposed shared-menu kind |
|---|---|---|---|---|---|
| 1 | Settings (header) + ✕ close | heading + button | `index.html:62–65`; `app.ts:440` | — | shared chrome |
| 2 | Mode (Ken Burns / Pan & Scan / Static) | select | `index.html:69–73`; `app.ts:445` → `set-mode` | server | `render` slot |
| 3 | Seconds per image | number input (1–300) | `index.html:78`; `app.ts:449–452` → `set-interval` | server | `render` slot |
| 4 | Shuffle subjects | checkbox | `index.html:82`; `app.ts:447` → `set-shuffle` | server | `render` slot |
| 5 | Theme (Dark / Light) | select | `index.html:87–91`; `app.ts:446` → `set-theme`; applied via SSE at `app.ts:299` | **server** (no localStorage) | `themePicker` with `onChange` server persistence (per FRD) |
| 6 | Controls position (Bottom / Top) | select | `index.html:95–98`; `app.ts:448` → `set-controls-position` | server | `render` slot |
| 7 | Debug timer | checkbox | `index.html:102`; `app.ts:443` → `debugTimer.show()` | none (client-only) | `render` slot |
| 8 | Server stamp ("started …") | read-only text | `index.html:107`; filled `app.ts:341–343` | — | `render` slot |
| 9 | Keyboard shortcuts help | static `<dl>` | `index.html:111–119` | — | `render` slot |

**Theme facts:** two themes (dark/light) via `data-theme` on `<html>`
(`index.html:2` default dark; runtime `app.ts:299`), value owned by the
server state — every connected screen follows.

**Dead/duplicate menu code in this tree:** the full dead menuserver asset
tree — `web/slideshow/base.html` (#rightToggle hamburger :26–32),
`js/panels.js`, `js/menuserver.js` (topnav hamburger :266), `js/navcontrols.js`,
plus jquery/todo/kbc/exercise files and matching HTML pages. Verified
**byte-identical** to menuserver's copies (md5: `base-util.js`,
`menuserver.js`, `panels.js`, `todo-utils.js`, `slideshow.js`, `utils.js`,
`style-base.css` all match). The live app uses only `index.html` +
`css/app.css` + `js/app.ts→app.js`.

---

## smbedit

**Existing hamburger:** **Yes — a right-side settings drawer (React).**

- **Trigger:** `.hamburger-btn` in the topbar, glyph `☰` (text character),
  `title="Settings"`, `aria-label="Open settings"` —
  `web/smbedit/src/App.tsx:178–185`.
- **Panel markup:** `.settings-drawer` + `.settings-backdrop`
  (`App.tsx:257–276`), body renders `<SettingsPage>` (`App.tsx:274`).
- **Open/close:** React state `settingsOpen`; CSS slide-in from the right
  (`transform: translateX(100%) → 0`, `web/smbedit/src/styles.css:717–734`)
  with fading backdrop (`styles.css:702–715`).
- **Dismiss:** backdrop click (`App.tsx:258–261`), ✕ close button
  (`App.tsx:265–271`). **No Escape**, no outside-Esc handling.
- **A11y:** `aria-label` on trigger and close; no `aria-expanded`, no focus
  trap, no focus return.

**Contents (in order — all of `SettingsPage`,
`web/smbedit/src/SettingsPage.tsx`):** persistence model: edits call
`onChange`→`patchConfig` (dirty state); written to server `state.json` only
when the user hits Save / Save & Restart (`App.tsx:75–97`, :125–156 via
`api.putConfig`). No localStorage.

| Order | Label | Control type | Handler (file:line) | Persistence | Proposed shared-menu kind |
|---|---|---|---|---|---|
| 1 | 🎨 Appearance / Theme: ☀️ Light · 🌑 Dark · 💻 System | 3-button tri-state switcher | `SettingsPage.tsx:37–50`; `ThemeProvider` resolves `system` via `matchMedia` (`src/theme.tsx:24–30`) | server config `theme` (saved with Save) | `themePicker` with `system` (FRD promotes this tri-state repo-wide); keep server persistence via `onChange` |
| 2 | smb.conf output path | text input | `SettingsPage.tsx:58–66` | server config (on Save) | `render` slot |
| 3 | Samba log path | text input | `SettingsPage.tsx:68–76` | server config (on Save) | `render` slot |
| 4 | 📥 Import existing configuration (section) | section + text input + button | input `SettingsPage.tsx:88–96`; Import button :98–111 → `window.confirm` then `onImport` (`App.tsx:100–122`, `api.importConf`) | server (explicit action) | `section` + `render` slot; **flag:** uses native `confirm()` (banned repo-wide by FR-5) |
| 5 | 👤 Share ownership / Share owner | text input | `SettingsPage.tsx:121–129` | server config (on Save) | `render` slot — **flag:** duplicates the sidebar's share-owner input (`App.tsx:203–211`); pick one home during migration |

**Theme facts:** tri-state `dark | light | system` (`theme.tsx:3`), resolved
to `data-theme` dark/light (`theme.tsx:28–30`); initial value from server
config (`App.tsx:167`); **no live `matchMedia` change listener** (system
changes apply on next render only) — the shared ThemeManager's live
`matchMedia` updating is an upgrade, not a regression.

**Dead/duplicate menu code:** none found.

---

## taskmaster

**Existing hamburger:** **Yes — a full dropdown menu, already the closest
thing in the repo to FR-4's target.** (The FRD's net-new list predates the
taskmaster merge — see Discrepancies.)

- **Trigger:** `#nav-menu-btn` in the top nav, inline-SVG three-line glyph
  (`ICON_MENU`, `web/taskmaster/js/main.ts:30–31`), with
  `aria-label="Menu"`, `aria-haspopup="true"`, `aria-expanded` maintained —
  built at `main.ts:158–165`, toggled at `main.ts:247–253`.
- **Panel markup:** `#nav-menu-panel` dropdown (`main.ts:167–170`), a
  popover panel, **not** a slide-in drawer; no backdrop.
- **Open/close:** `hidden` attribute toggle (`main.ts:249–251`); opening
  refreshes the server status line (`main.ts:252`, `refreshStatusLine`
  :65–81).
- **Dismiss:** outside click (`main.ts:325–328`), **Escape**
  (`main.ts:329–331`, `closeMenu` :58–63 also resets `aria-expanded`).
- **A11y:** `aria-expanded` ✓, Escape ✓; **no focus trap, no focus return**.

**Contents (in order):**

| Order | Label | Control type | Handler (file:line) | Persistence | Proposed shared-menu kind |
|---|---|---|---|---|---|
| 1 | Metrics (nav link; menu copy shown only on narrow screens via `.menu-nav` CSS) | link `#metrics` | `main.ts:172–184` (from `NAV_LINKS` :37) | — | `link` |
| 2 | "Live updates" heading | section header | `main.ts:188–190` | — | `section` |
| 3 | Fallback poll interval (5s/10s/30s/60s) | select | `main.ts:192–207` → `live.setInterval` | localStorage `tm.live.interval` (`js/ui/live.ts:23`, write :138) | `render` slot |
| 4 | "Server" heading | section header | `main.ts:215–216` | — | `section` |
| 5 | Status (healthy/unreachable) | read-only row | `main.ts:217`, refreshed `main.ts:65–81` (`GET /api/health`) | — | `render` slot |
| 6 | Backend build | read-only row | `main.ts:218`, filled :75 | — | `render` slot |
| 7 | Frontend build | read-only row | `main.ts:219` (`FRONTEND_BUILD_TIME` from `js/buildinfo.ts`) | — | `render` slot |
| 8 | Allow sudo | custom toggle (`ui/toggle.ts` handle, never a checkbox) | `main.ts:221–244` → `api.setCapabilities` with optimistic revert | server `/api/capabilities` | `render` slot (module-styled toggle) |

**Topbar controls that stay outside the menu** (deliberate per taskmaster's
own FRD comment, `main.ts:1–11`): Live/pause toggle (`buildLiveControl`,
`main.ts:135–151`; persisted localStorage `tm.live.enabled`,
`ui/live.ts:22`), Hand brake button (`main.ts:261–278`, confirm via shared
`confirmDialog` `ui/modal.ts`), auth-aware Logout icon button
(`main.ts:113–124`, inline-SVG `ICON_LOGOUT`).

**Theme:** none — single palette in `web/taskmaster/style.css`, no
`data-theme`, no picker. `themePicker` is net-new here.

**Dead/duplicate menu code:** none found.

---

## timetracker

**Existing hamburger/drawer:** **None.** Layout is a customer sidebar + main
report area, all built by `web/timetracker/index.js` (DOM-injected HTML
strings). Icons are FontAwesome classes (`fa-edit`, `fa-download`,
`fa-copy` — the module ships `fontawesome.min.css` with a broken/partial
webfont set per FRD FR-6). No theme UI anywhere; no localStorage.

**Net-new hamburger candidates:**

| Order | Label | Control type | Handler (file:line) | Persistence | Proposed shared-menu kind |
|---|---|---|---|---|---|
| 1 | Export data | icon button (`fa-download`, `#exportBtn`) | markup `index.js:187–189`; handler `index.js:193` | — | `action` (strongest candidate — FRD's own FR-4 example is "Export CSV") |
| 2 | Add customer | sidebar `+` button opening add-modal | `index.js:53–80`, submit :135 | server | `action` (candidate) |
| 3 | Delete customer | per-row button + confirm modal | `index.js:36–48` | server | stays per-row (content) |
| 4 | Report navigation ◀ / Today / ▶ | buttons | markup `index.js:308–311`; handlers `index.js:426–428` | server-stored reports | stays in-page (working surface) |
| 5 | Copy report | icon button (`fa-copy`, `#copyReportBtn`) | `index.js:319` | — | stays in-page or `action` |
| — | Theme switching | — | none today | — | `themePicker` (net-new) |

**Dead/duplicate menu code:** none found (but note the FRD-flagged broken FA
glyph set affects any icon reuse).

---

## todo

**Existing hamburger:** **Yes — the repo's most complete implementation and
the FRD's designated base pattern** (slide-in drawer + backdrop).

- **Trigger:** `#menu-toggle` in the topbar, FontAwesome glyph
  `<i class="fas fa-bars">` — `web/todo/index.html:172–174`.
- **Drawer markup:** `<aside class="sidebar" id="sidebar">`
  (`index.html:201–252`); backdrop `#sidebar-backdrop` (`index.html:195`).
- **Open/close:** `toggleSidebar()`/`closeSidebar()` toggle the
  `sidebar-open` class on both (`index.html:78–85`); CSS slide-in
  `transform: translateX(-100%) → 0` (`web/todo/css/todo.css:117–122`),
  backdrop display toggle (`todo.css:134–144`).
- **Dismiss:** backdrop click (`index.html:195`). **No Escape for the
  sidebar** (the page-level Escape handler `index.html:149–151` closes
  modals only), no outside-click beyond the backdrop.
- **A11y:** none of the three — no `aria-expanded`, no focus trap, no focus
  return. Trigger has `title="Menu"` only.

**Contents (in order):**

| Order | Label | Control type | Handler (file:line) | Persistence | Proposed shared-menu kind |
|---|---|---|---|---|---|
| 1 | Subject | select `#subject-list-selector` | `index.html:204–206` → `changeSubject()` `web/todo/js/todo.js:241` | selection drives loaded list (URL/server data) | `render` slot |
| 2 | List | select `#item-list-selector` | `index.html:208–211` → `changeItem()` `todo.js:236` | — | `render` slot |
| 3 | Move to subject + **Move List** button | select `#new-subject-list-selector` + button | `index.html:213–217` → `moveListToNewSubject()` `todo.js:453` | server (list move) | `render` slot |
| 4 | (divider) | `.sidebar-divider` | `index.html:219` | — | `separator` |
| 5 | Columns (label) | section label | `index.html:222` | — | `section` |
| 6 | Votes | checkbox `#col-votes` | `index.html:223–226` → `setColVisible('votes', …)` `index.html:62–70` | server `POST /config/columns` (loaded `index.html:72–76`) | `render` slot (checkbox group) |
| 7 | Period | checkbox `#col-period` | `index.html:227–230` | server `/config/columns` | `render` slot |
| 8 | Next Due Date | checkbox `#col-next_due` | `index.html:231–234` | server `/config/columns` | `render` slot |
| 9 | Cooldown | checkbox `#col-cooldown` | `index.html:235–238` | server `/config/columns` | `render` slot |
| 10 | (divider) | `.sidebar-divider` | `index.html:241` | — | `separator` |
| 11 | Vote Cooldown (min) | number input `#cooldown-input` (1–120) | `index.html:243–250` → `saveCooldown()` `index.html:32–40` | server `POST /config/settings` (loaded :23–31) | `render` slot |

**Theme switching (outside the menu):** topbar `#theme-toggle` button
(`index.html:179–181`, FA moon/sun icon swap) → `toggleTheme()`
(`web/todo/js/theme.js:21–26`); applied pre-paint at script load
(`theme.js:19` — the FRD's generalization source); resolution order
localStorage → `prefers-color-scheme` (`theme.js:12–16`).
**Storage key:** localStorage `todo-theme`; **themes:** `dark`, `light`
(default markup `data-theme="dark"`, `index.html:2`).
→ becomes standard `themePicker`; the topbar quick-toggle may remain as a
bonus backed by the same ThemeManager (per FRD).

**Topbar/toolbar context (stays in-page):** brand/title editor
(`index.html:175–176`), copy link (:182–184, `todo.js:435`), open in new
tab (:185–187, `calcNewHREF` `todo.js:424`), compare (:188–190,
`openCompare` `index.html:134–139`); toolbar: back (`revertList`
`todo.js:286`), New List (`openNewListDialog` `index.html:89–95`), Add
(`showAdd` :142–145), Completed (`hideShowCompleted` `todo.js:502`),
Blocked (`hideShowBlocked` `todo.js:506`), Read-only checkbox (`roToggled`
`web/todo/js/todo-utils.js:887`), Vote (`vote` `todo-utils.js:468`).

**Dead/duplicate menu code — the todo tree contains THREE hamburger
implementations, two dead:**
1. **Live:** `#menu-toggle` sidebar above.
2. **Dead:** `web/todo/base.html:26–32` — `#rightToggle` three-`<span>` CSS
   hamburger (`.hamburger-icon`) + `#leftToggle` arrow, driven by
   `web/todo/js/panels.js` (right-panel width toggle :39–53, login-gated
   display :141–190). `base.html` is linked from nowhere; verified
   byte-identical to menuserver's and slideshow's copies.
3. **Dead:** `web/todo/js/menuserver.js:266–268` topnav `&#9776;` hamburger
   (plus `js/navcontrols.js`) — the menuserver implementation, unused by any
   live todo page (`index.html` does not load it). Byte-identical to
   menuserver's copy.

Most of `web/todo/css/` + `web/todo/js/` beyond
`todo.css/todo.js/todo-utils.js/theme.js/utils.js/compare.*/jquery*` is the
dead menuserver asset tree (verified by md5 above; only `todo-utils.js`
has diverged from the menuserver/slideshow copies). `compare.html` /
`compare.js` are **live** (reached via the topbar Compare button).

---

## utuber

**Existing hamburger:** **Yes — a small settings dropdown panel** (single
816-line `index.html`).

- **Trigger:** `#settings-btn` in the topbar, glyph `☰` (text character),
  with `aria-label="Settings"`, `aria-controls="settings-panel"`, and
  **`aria-expanded` maintained** — `web/utuber/index.html:446–448`,
  toggled `index.html:764–771` (`btn.setAttribute('aria-expanded', …)`
  :769).
- **Panel markup:** `#settings-panel` (`index.html:451–462`), shown/hidden
  via the `hidden` attribute (`.settings-panel[hidden]{display:none}`,
  `index.html:98`). Inline panel under the topbar — no slide animation.
- **Dismiss:** trigger re-click only. **No backdrop, no Escape, no
  outside click.**
- **A11y:** `aria-expanded` ✓ (+ `aria-controls`); no Escape, no focus
  trap, no focus return.

**Contents (in order):** opening the panel re-fetches current values
(`loadSettings()`, `index.html:770`, :773–784).

| Order | Label | Control type | Handler (file:line) | Persistence | Proposed shared-menu kind |
|---|---|---|---|---|---|
| 1 | Python interpreter | text input `#python-bin` (+ hint text) | `index.html:452–457`; loaded `index.html:773–784` | server `GET/POST /settings.json` | `render` slot |
| 2 | Save | button | `index.html:459` → `saveSettings()` `index.html:786–809` | server `POST /settings.json` | `render` slot (paired with 1) or `action` |
| 3 | (status text) | inline status span `#settings-status` | `index.html:460`; written :779–807 | — | part of the same `render` slot |

**Theme:** hard-coded `data-theme="dark"` (`index.html:2`); no switching UI,
no localStorage. `themePicker` is net-new. Mode tabs (Video / Audio,
`index.html:466–480`) stay in-page per FRD. Update yt-dlp button
(`index.html:525`) and queue are page content.

**Dead/duplicate menu code:** none found.

---

# Cross-module summary

"A11y features" columns: **E**=Escape closes, **X**=aria-expanded on trigger,
**T**=focus trap/focus return. No module has T.

| Module | Has menu today? | Items inside it | A11y present | Migration complexity (1–5) | Notes |
|---|---|---|---|---|---|
| admin | No | 0 | — | 1 | themePicker + optional logout action |
| certmachine | No hamburger; Tools `<details>` dropdown | 2 (+2 conditional texts) | native `<details>` only | 2 | Tools entries map cleanly to link/action |
| grocery | No | 0 (7 header controls are candidates) | — | 2 | per-tab item visibility needed; 3-state visibility button awkward |
| issuetracker | No | 0 (10 sidebar links are candidates) | — | 2 | React binding; sidebar likely stays on desktop |
| menuserver | Yes (responsive topnav collapse) | data-driven (N subjects × M links) | none | 3 | nav-as-menu is Phase 5; async, two-level structure |
| multissh | No | 0 | — | 1 | themePicker only, realistically |
| obsidianoid | Yes (theme popover) | 1 section / 5 theme buttons | outside-click only | 2 | themePicker replaces its only content; keep per-vault key + 5 custom themes (no light) |
| slideshow | Yes (settings panel + scrim) | 9 (6 interactive) | E | 3 | all persistence server-side via `/api/control`; theme select → themePicker w/ `onChange` |
| smbedit | Yes (right slide-in drawer + backdrop) | 5 groups (7 controls) | none (labels only) | 3 | React binding; native `confirm()` in Import must become shared dialog; duplicate share-owner input |
| taskmaster | **Yes** (dropdown menu — FRD says net-new) | 8 (1 link, 2 sections, 1 select, 3 read-only rows, 1 toggle) | E, X | 3 | closest to FR-4 already; popover→drawer chrome change; keep `tm.live.*` keys |
| timetracker | No | 0 (export/add-customer are candidates) | — | 2 | FA glyphs broken; icons must come from shared registry |
| todo | Yes (slide-in drawer + backdrop — FR-4 base) | 11 rows (3 selects, 1 button, 4 checkboxes, 1 number input, 2 dividers/1 section) | none | 4 | most items; theme toggle outside menu → themePicker; 2 extra dead hamburgers in tree |
| utuber | Yes (inline settings dropdown) | 3 (input + save + status) | X | 1 | smallest live menu |

**Totals:** 7 modules have a live hamburger/menu-like control today
(menuserver, obsidianoid, slideshow, smbedit, taskmaster, todo, utuber —
counting certmachine's Tools `<details>` would make 8 menu-like surfaces).
Fixed (non-data-driven) items to preserve one-for-one: **todo 9 controls,
slideshow 6 controls + 3 info blocks, smbedit 7 controls, taskmaster 8
items, obsidianoid 5 theme buttons, utuber 2 controls (+status) — ≈ 37
control-level items**, plus menuserver's data-driven nav links and
certmachine's 2 Tools entries.

---

# Discrepancies vs the FRD's FR-4 preservation table

1. **taskmaster already has a hamburger menu** — the FRD lists taskmaster in
   the net-new row, but the merged taskmaster ships a complete dropdown menu
   (`web/taskmaster/js/main.ts:153–257`) with a nav-link section, poll-interval
   select, server-status rows, and an allow-sudo toggle. These items are
   **missing from the preservation inventory** and must be re-registered
   one-for-one (plus its localStorage keys `tm.live.enabled` /
   `tm.live.interval`). The FRD's "six implementations" count is also stale —
   with taskmaster it is seven.
2. **"None of the current six implementations has aria-expanded, Escape
   handling, or a focus trap"** is only true on the strict reading "none has
   all three." Individually: **utuber maintains `aria-expanded`**
   (`web/utuber/index.html:769`) plus `aria-controls`; **slideshow closes on
   Escape** (`web/slideshow/js/app.ts:471`); **taskmaster has both
   `aria-expanded` and Escape** (`main.ts:251, 329–331`). No module has a
   focus trap or focus return, so the shared class is still a strict upgrade
   everywhere — but the migration checklist should verify utuber/taskmaster
   don't *lose* their existing aria wiring during any interim state.
3. **The dead `base.html` `#rightToggle` hamburger is not todo-specific** —
   byte-identical copies exist in all three trees:
   `web/todo/base.html`, `web/slideshow/base.html`,
   `web/menuserver/base.html` (each with `js/panels.js`). The FRD mentions
   only todo's third dead hamburger.
4. **`web/menuserver/menuserver.html` is a byte-identical duplicate of
   `web/menuserver/index.html`** — an additional dead duplicate the FRD does
   not mention; `css/nav.css` is likewise dead (linked only from a
   commented-out line, `index.html:9`).
5. **obsidianoid has no light theme** — its 5 themes are
   dark/forest/ocean/ember/rose. The FRD's themePicker default set must not
   silently replace these; the module needs a custom `themes` list, not just
   the standard picker (the FRD's `ThemeManager` config supports this, but
   the preservation row says only "theme swatch panel → themePicker").
6. **smbedit's Import flow uses native `window.confirm`**
   (`web/smbedit/src/SettingsPage.tsx:103–105`) — the FRD's FR-5 table counts
   smbedit's `confirm()` (×1); confirmed, and it sits *inside* the drawer
   being migrated, so the FR-4 migration will collide with the FR-5
   dialog-replacement work — sequence deliberately.
7. **smbedit's theme has no live `matchMedia` listener** for `system`
   (`src/theme.tsx:24–26` reads it once per render) — the FRD's FR-3 mandate
   of live-updating `system` is an intentional behavior *change* (upgrade),
   worth calling out in the acceptance checklist rather than treating as
   1:1 preservation.
8. **Minor:** slideshow's drawer also contains non-control content the FRD
   row doesn't list — the read-only server-start stamp
   (`index.html:107`) and the keyboard-shortcuts help block
   (`index.html:111–119`); both need `render` slots to avoid silent loss.
   Similarly, utuber's settings panel (FRD: "settings panel contents →
   slots") is exactly one input + save button + status line, and its
   trigger currently carries `aria-controls`, which the shared chrome
   should reproduce.
