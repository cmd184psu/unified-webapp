# menuserver — Present-State Inventory (Phase-5 Pre-Work)

> **Requirements-capture pre-work for FRD-ui-unification Phase 5** (menuserver
> rewrite off jQuery/jQuery-UI/Bootstrap onto the shared vanilla-TS library).
> Generated **2026-09-15** on branch **`ui-upgrade`**. This document describes
> **current behavior only** — facts, quirks, and must-survive behavior. No
> redesign proposals except the final "Open questions" section.

---

## 1. Module purpose and runtime topology

menuserver is a **read-only personal link/credential portal** ("home lab
launchpad"). The backend scans a data directory of per-subject folders of
small JSON "menu" files; the frontend renders one nav-bar dropdown per
subject, each entry being either an external link or an in-page "page" (a
panel showing links, usernames, masked passwords with hide/show + copy
buttons, and free-form notes). It is a Go port of an older Node.js
multipurpose server ("pshelper"), and its static tree still carries that
server's *other* apps (todo, slideshow, on/off switch, exercise timer) as
dead weight.

Runtime wiring (all paths absolute):

- Module build: `/opt/unified-webapp-ui-upgrade/internal/menuserver/build.go`
  — API routes on a `ServeMux`, everything else falls through to the shared
  static handler over `cfg.StaticDir`.
- Dispatch: **Host-header routing** in
  `/opt/unified-webapp-ui-upgrade/cmd/server/main.go` (`Dispatcher`, line ~44;
  `buildModule` case `"menuserver"` line 360-361). Live config
  `/opt/unified-webapp-ui-upgrade/unified-webapp.json` routes
  `menuserver.cmdhome.net` → `menuserver`. All frontend fetches are
  **relative URLs** (`config/`, `items`, `menus/...`) and therefore assume
  the app is served at the **root path of its host** — there is no path
  prefix support.
- Middleware (main.go line 137 and buildDispatcher): `middleware.Wrap(middleware.OriginCheck(...))`
  around the dispatcher; per-module `middleware.BodyLimit` = **1 MiB**
  (`defaultBodyLimit`, main.go ~line 316) and `auth.Service.Gate("menuserver", …)`.
- Auth: menuserver is **protected iff `auth.modules.menuserver` exists in
  config** (`internal/platform/auth/gate.go` step 3, line ~160). The example
  config (`unified-webapp-example.json` line 123) protects it with a PIN file
  (`/etc/unified-webapp/menuserver.pin`); the checked-in live
  `unified-webapp.json` has **no `auth` block at all → menuserver currently
  runs unprotected**. Whether protected or not, the gate itself answers
  `GET /healthz`, `GET /api/auth/mode`, and `GET /api/auth/whoami` on the
  menuserver host before the module ever sees them.
- Config type: `MenuserverConfig{StaticDir, DataDir, ShowAllPages}` —
  `/opt/unified-webapp-ui-upgrade/internal/platform/config/config.go` lines
  259-264; defaults `./web/menuserver` and `./data/menuserver` (lines
  472-475); `~` expansion via `expandMenuserverPaths` (line 671).

## 2. Backend HTTP endpoint inventory

Handler: `/opt/unified-webapp-ui-upgrade/internal/menuserver/handler.go`;
store: `/opt/unified-webapp-ui-upgrade/internal/menuserver/store.go`; tests:
`handler_test.go`, `store_test.go` in the same directory.

| # | Method/Path | Params | Response | Notes | Frontend caller |
|---|---|---|---|---|---|
| 1 | `GET /config` and `GET /config/` | none | `200` JSON `{"showAllPages": bool}` | Both spellings registered (handler.go:24-25). Value comes from config `menuserver.show_all_pages`. | `js/menuserver.js:362` (`ajaxGetJSON("config/")`) — also called by the dead pages' JS |
| 2 | `GET /items` | none | `200` JSON array of `{"age":0, "timestamp":0, "subject":"<dir>", "entries":["<dir>/<file>.json", …]}` | `age`/`timestamp` are **hard-coded 0** legacy fields (store.go:59-60). Empty/absent data ⇒ `[]` (never `null`, handler.go:46-48). Subjects = subdirectories of DataDir containing ≥1 `*.json`; subdirs with zero `.json` files are omitted (store.go:55-57). Ordering = `os.ReadDir` ⇒ **alphabetical by filename**, both levels. `500` JSON `{"error": "..."}` if DataDir unreadable. | `js/menuserver.js:367` |
| 3 | `GET /menus/{subject}/{item}` | path segments | `200`, `Content-Type: application/json`, **raw file bytes** of `DataDir/{subject}/{item}` | `404` if either segment fails `fspath.ValidName` (empty, dot-prefixed, or containing `/`/`\` — fspath.go:15-20), if the path escapes DataDir, or if the file is missing (handler.go:52-63). No `.json`-suffix check on read — any readable file in a subject dir is served. | `js/menuserver.js:389` (`"menus/"+entry`, where `entry` is the `subject/file.json` string from `/items`) |
| 4 | `GET /*` (everything else) | any path | Static file, or **`index.html` fallback with `200`** for any path that does not exist on disk | Shared SPA static handler `/opt/unified-webapp-ui-upgrade/internal/platform/static/static.go:23-30`. Consequence: there is **no real 404** for pages — `/nonsense` returns the portal. Non-GET verbs fall here too (nothing else registers POST/PUT/DELETE). | browser |
| 5 | `GET /healthz` | — | `200` `{"ok":true}` | Served by the auth gate (gate.go:129-133), not the module. | none |
| 6 | `GET /api/auth/mode` | — | `200` `{"methods":[…]}` | Gate-owned (gate.go:136-143). | none (menuserver JS never calls it) |
| 7 | `GET /api/auth/whoami` | — | `200` principal info | Gate-owned (gate.go:149-152). | none |

**There are no write endpoints.** The Go menuserver module is read-only. The
frontend still contains a `POST items/{filename}` save path
(`js/menuserver.js:46-67 SaveList`) inherited from the Node.js version; if it
were ever invoked, the POST would fall through to the static handler and get
the `index.html` fallback (HTML, 200), which `$.ajax(dataType:'json')` would
report as an error. On the live page nothing ever calls it (see §6 quirks).

## 3. Data model

- **Location:** `DataDir` (default `./data/menuserver`; live deployment
  example at `/opt/unified-webapp/data/menuserver`). Auto-created with mode
  `0750` at module build (store.go:23-28) — a fresh install boots with an
  empty portal (empty `[]` items ⇒ nav bar containing only the hamburger
  icon).
- **Layout:** `DataDir/<subject>/<item>.json` — one directory per nav
  dropdown, one JSON file per menu entry. No index/manifest file; the
  directory listing *is* the menu. No DB, no metadata files.
- **Menu file format** (observed superset across the 52 live files:
  subjects `coding`, `computers`, `contacts`, `internal`, `sites`, `splash`,
  `work`):

```jsonc
{
  "id": "unique-page-id",       // required in practice; DOM id of the page div
  "title": "Display Title",     // required; Title-Cased by the client
  "notes": "free text/HTML",    // optional; rendered as raw HTML
  "url": "https://…",           // optional; makes the NAV entry a direct external link
  "sites": [                    // optional; rows of the page's table
    {
      "label": "Name",          // optional; falls back to the link
      "url": "https://… | ssh://… | vnc://…",
      "port": 5001,             // optional; rendered as url:port (see quirk Q14)
      "username": "…",          // optional; enables the credential rows
      "password": "…"           // optional; plaintext in the file
    }
  ],
  "gdoc": "…"                   // referenced by code but buggy (Q8); not present in live data
}
```

- **Sensitive data:** live menu files store **plaintext passwords** (16 of
  the 52 files carry `username`+`password` pairs, including the splash
  page). These are served verbatim by `GET /menus/...`, embedded into DOM
  attributes, and dumped to the browser console (Q12). Any rewrite must
  treat menu JSON as secret-bearing.
- **Special subject `splash`:** excluded from the nav; the page whose JSON
  `id` is literally `"splash"` is auto-shown at startup (menuserver.js:397,
  415-418).

## 4. Frontend inventory

### 4.1 HTML pages (9 files)

| File | Status | What it is |
|---|---|---|
| `web/menuserver/index.html` | **LIVE** — the only real page | Portal shell: empty `div.topnav#myTopnav`, empty `div#lowerSection`, hidden `#hiddentext`/`#copytext` input for clipboard copy (line 46). Loads jquery, jquery-ui, navcontrols, utils, menuserver.js; inline `$(document).ready → startMenuserver()` (lines 28-32) and inline `<style> body{position:relative} </style>` (lines 33-37). |
| `web/menuserver/menuserver.html` | LIVE alias — **byte-identical to index.html** | Same page reachable at a second URL. |
| `web/menuserver/todo.html` | DEAD (legacy pshelper) | Old todo app UI. Calls endpoints (`POST items/…`, etc.) that don't exist in the Go menuserver. Loads Bootstrap 3.4.1 **from maxcdn CDN** (line 18). |
| `web/menuserver/slideshow.html` | DEAD | Old slideshow UI (incl. `<ken-burns-carousel>` custom element from kbc.js, Escape-key handler). Backend endpoints absent. |
| `web/menuserver/onoff.html` | DEAD | Toggle-switch demo; calls `getstate`/`togglestate` (absent). CDN Bootstrap (line 19). |
| `web/menuserver/exercise.html` | DEAD | Exercise-timer app; calls `data/data.json` (absent). CDN Bootstrap (line 18). |
| `web/menuserver/base.html` | DEAD | Unrelated "reactive app" scaffold (login panel with default passcode `11111`, side panels, task tables) using axios + base-util.js + panels.js — a taskmaster-lineage template, never linked from the portal. |
| `web/menuserver/404.html` | DEAD | Stock nginx/Fedora error page (references `/nginx-logo.png`, `/poweredby.png` which don't exist). Never served: the static handler's fallback is index.html, and API 404s are Go plain-text/JSON. |
| `web/menuserver/50x.html` | DEAD | Same, for 50x. |

All dead pages remain reachable by direct URL (`/todo.html` etc.) on the
menuserver host, but are broken to varying degrees.

### 4.2 JavaScript (17 files)

| File | Lines/size | Status | Purpose / notes |
|---|---|---|---|
| `js/menuserver.js` | 420 | **LIVE** | The app. Live call graph from `startMenuserver()` (line 360): fetch config → fetch items → per subject fetch each menu JSON → `addPage()` renders a hidden page div per unique id → `addMajorMenu()` renders a hover dropdown per subject (except "Splash") → `addMajorMenuHamburger()` appends the ☰ icon → auto-show splash. Also `showPage()`, `renderSiteRow/Table()`, `arrayToDropDown()`, `dropDownButton()`. Roughly half the file is **vestigial todo-lineage code never reachable on the live page**: `SaveList/saveit/SelectNewFile/SaveAndLoad/changeItem/changeSubject/SelectNewSubject/revertList/addIt` (lines 19-207) plus two functions literally named `…DONTUSE` (lines 209-235). |
| `js/utils.js` | 161 | **LIVE** | Shared helpers: `ajaxGetJSON` (fetch-based, **no error/reject path**, lines 12-19), `copyToClipBoard` (deprecated `document.execCommand('copy')` via the hidden input, lines 21-37), `toggle(id)` show/hide pair (lines 125-139), `titleCase` (108-123), `makeID` (141-150), plus todo-only helpers (`DaysToMS`, `EpocMStoISODate`, `embedURL`, `genTableHeader/Footer`), a broken `href()` emitting `<href=…>` (line 152-156), and a jQuery plugin stub `$.fn.checkUtils` (158-161). |
| `js/navcontrols.js` | 8 | **LIVE** | Single function `openMenu()` — toggles class `responsive` on `#myTopnav` (mobile hamburger). |
| `js/jquery.js` | 11,008 (288 KB) | **LIVE vendor** | **jQuery 1.12.4** (2016; known XSS advisories). Used only for: `$(document).ready`, `$(sel).append/hide/show/prop/val`, `$.ajax/$.get` (vestigial paths). No plugins beyond jQuery-UI are ever initialized. |
| `js/jquery-ui.js` | 18,705 (512 KB) | **LIVE-loaded, functionally unused** | jQuery UI 1.12.1 full build. **Zero widget calls anywhere in the tree** (no `.dialog(`, `.sortable(`, `.datepicker(`, etc. — verified by grep across all JS and HTML). Pure dead weight on the wire. |
| `js/bootstrap.js` | 60 KB min | DEAD | Bootstrap **4.5.2** bundle — present on disk but **no HTML file references it** (the dead pages load Bootstrap **3.4.1 from CDN** instead). |
| `js/popper.js` | ~20 KB min | DEAD | Popper v1; referenced by nothing. |
| `js/axios.min.js` | 2 lines min | DEAD (for menuserver) | axios **0.21.1**; loaded only by dead `base.html`. |
| `js/marked.min.js` | min | DEAD | marked **v15.0.7**; referenced by nothing in this tree. |
| `js/todo.js` | 489 | DEAD | Old todo app logic (defines the `lists` global that menuserver.js's vestigial functions lean on). |
| `js/todo-utils.js` | 926 | DEAD | Old todo rendering/drag-drop/voting utils; defines `dropVars()` (line 54) which `menuserver.js:41` calls in the (unreachable) save path. |
| `js/slideshow.js` | 445 | DEAD | Old slideshow logic. |
| `js/kbc.js` | 79 (min) | DEAD | `ken-burns-carousel` web component (used only by dead slideshow.html). |
| `js/onoff.js` | 36 | DEAD | Toggle-switch demo logic. |
| `js/exercise.js` | 93 | DEAD | Exercise timer logic. |
| `js/base-util.js` | 587 | DEAD | base.html scaffold logic (fetches `/status`, `/taggedcsv`, `/config`, `/task` — endpoints of a different app). |
| `js/panels.js` | 222 | DEAD | base.html slide-in panels + `/login`, `/logout` fetches. |

### 4.3 CSS (19 files)

| File | Loaded by | Status / notes |
|---|---|---|
| `css/responsivenav.css` | index/menuserver.html | **LIVE — the app's entire look.** W3Schools "responsive topnav with dropdown" pattern: `.topnav` (#333 bar), `.dropdown:hover .dropdown-content {display:block}` (hover-open menus), `.dropbtn`, `@media (max-width:600px)` collapse + `.topnav.responsive` expanded state. Hard-coded colors (#333/#555/#f9f9f9/#04AA6D); `font-family: Arial`. **No CSS variables, no dark theme.** |
| `css/fontawall.css` | index/menuserver + dead pages | **LIVE vendor** — Font Awesome Free **5.7.0** all.css referencing `../webfonts/fa-{brands,regular,solid}-*`. Only regular-400 (ttf/woff/woff2) and solid-900 (woff/woff2) files exist; **brands and all .eot/.svg files are missing** (harmless: only solid/regular glyphs are used). |
| `css/fontawsme.css` | index/menuserver + dead pages | LIVE-loaded, **broken** — Font Awesome **4.6.3** referencing `../fonts/fontawesome-webfont.*`; **no `fonts/` directory exists**, so FA4 never renders; the FA5 sheet's `.fa` alias is what actually draws icons. |
| `css/jquery-ui.css` | all jQuery pages | LIVE-loaded, functionally unused (no jQuery-UI widgets/markup). 1,312 lines. |
| `css/modal.css` | index/menuserver + dead pages | LIVE-loaded, unused on the live page (`.modal/.modal-content/.close` markup exists only in dead pages' flows). |
| `css/nav.css` | dead pages only (commented out in index.html:9) | DEAD for menuserver (`.sidenav` slide-out pattern). |
| `css/progress.css` | todo/onoff/exercise.html | DEAD (progress bars for old todo). |
| `css/toggle.css` | onoff.html | DEAD (switch styling). |
| `css/kbc.css` | slideshow.html | DEAD. |
| `css/style-base.css`, `css/style-base-tr.css`, `css/progressbar-base.css`, `css/arrow.css`, `css/panels.css` | base.html | DEAD (scaffold styling; style-base.css:36 references `/images/divider.png` by absolute path). |
| `css/fontawesome.min.css` | **nothing** | DEAD — FA **5.15.4** with `/webfonts/...` absolute URLs; never linked. |
| `css/pshelper-index.css` | nothing | DEAD (old pshelper landing styling). |
| `css/buttons.css` | nothing | DEAD. |
| `css/base.css` | nothing | DEAD. |
| `css/slideshow.css` | nothing (slideshow.html doesn't even link it) | DEAD. |

### 4.4 Fonts and images

| File | Status |
|---|---|
| `webfonts/fa-solid-900.woff2` / `.woff` | **LIVE** — the glyphs actually rendered (`fa-caret-down` in every dropdown button, `fas fa-arrow-up` back-to-top in showAllPages mode). 244 KB total webfonts. |
| `webfonts/fa-regular-400.woff2` / `.woff` / `.ttf` | Loaded by FA5 css if a `far` glyph is used — the live page uses none (dead todo.html uses `far fa-copy`). |
| `images/fvw.png` | **LIVE** — favicon of every page. |
| `images/logo.svg` | DEAD (base.html only, and via absolute `/images/logo.svg`). |
| `images/user.png`, `images/divider.png` | DEAD (divider.png referenced only by dead style-base.css). |

Whole static tree: **54 files, ~1.6 MB**; the live page actually exercises
~11 of them (index.html + 3 CSS live/loaded-broken + jquery-ui.css/modal.css
dead-loaded + 5 JS + favicon + fa-solid woff2).

### 4.5 Vendor usage summary (used vs shipped)

| Vendor asset | Shipped | Actually used by live page |
|---|---|---|
| jQuery 1.12.4 | yes | yes — DOM append/show/hide/val/prop, `$(document).ready`; `$.ajax/$.get` only in unreachable code |
| jQuery-UI 1.12.1 (js+css) | yes (800 KB) | **no** — zero widget instantiations |
| Bootstrap | 4.5.2 local (unreferenced) + 3.4.1 CDN (dead pages) | **no** |
| Popper v1 | yes | **no** |
| axios 0.21.1 | yes | **no** (base.html only) |
| marked 15.0.7 | yes | **no** |
| Font Awesome | 4.6.3 css (broken), 5.7.0 css+partial webfonts, 5.15.4 css (unlinked) | FA 5.7.0 only, two glyphs: `fa-caret-down`, `fa-arrow-up` (plus dead-page icons) |

## 5. Nav / dropdown structure (current DOM)

Built entirely by string-concatenated `innerHTML` appends into
`div.topnav#myTopnav`:

```html
<div class="topnav" id="myTopnav">
  <div class="dropdown">                          <!-- one per subject, alphabetical -->
    <button class="dropbtn">Subject <i class="fa fa-caret-down"></i></button>
    <div class="dropdown-content">
      <!-- entry with url: external link -->
      <a href="https://…" target="_blank">Title-or-URL</a>
      <!-- entry without url: in-page page switch -->
      <a href="javascript:showPage('pageid')">Title Cased</a>
    </div>
  </div>
  …
  <a href="javascript:void(0);" class="icon" onclick="openMenu()">&#9776;</a>  <!-- last child -->
</div>
<div id="lowerSection" style="padding-left:16px">
  <div id="<json.id>" class="pageClass" style="display:none">…</div> …
</div>
```

- Dropdowns open on **CSS `:hover`** (responsivenav.css:71-73) — no JS, no
  click-to-open, no keyboard access, no aria attributes anywhere.
- ≤600 px: dropbtns hidden, ☰ shown; clicking ☰ toggles `.responsive`
  (navcontrols.js:1-8), which stacks everything vertically; dropdown-content
  becomes `position:relative` inline blocks (still hover-driven).
- These dropdowns are **navigation** (per the FRD): each item either leaves
  the site (`target="_blank"`, including `ssh://` / `vnc://` scheme links
  handled by the OS) or switches the visible page div.
- **Keyboard behavior: none.** No key handlers on the live page (the only
  Escape handler in the tree is in dead slideshow.html:52-64). Tab reaches
  links/buttons in DOM order; hover-only dropdowns make items unreachable
  without a pointer.
- **Forms/dialogs/tables on the live page:** no forms and no dialogs
  (modal.css is loaded but unused). One layout `<table>` per page div
  (border-collapse:separate; 15px/20px spacing) holding link rows,
  username/password rows, a nested button table (Hide/Show + Copy), and a
  notes row.

## 6. Quirks and traps (things a rewriter would break by accident)

Numbered for reference; paths relative to `/opt/unified-webapp-ui-upgrade/`.

- **Q1 — Two identical entry points.** `web/menuserver/index.html` and
  `web/menuserver/menuserver.html` are byte-identical; bookmarks may point
  at either. The SPA fallback additionally makes *every* unknown path serve
  the portal with HTTP 200 (`internal/platform/static/static.go:23-30`).
- **Q2 — Everything is `innerHTML` string concatenation with unescaped
  data.** Menu JSON values (titles, labels, notes, usernames, passwords, ids)
  are interpolated raw (`js/menuserver.js:242-347`). `notes` **relies** on
  this: live data contains prose that is rendered as HTML. A quote character
  in a password breaks the DOM (`value="…"` at menuserver.js:289-290 and the
  inline `onclick="copyToClipBoard('<password>')"` at line 294). Escaping
  everything uniformly would change notes rendering; escaping nothing keeps
  the injection quirk.
- **Q3 — Clipboard copy is `document.execCommand('copy')`** via the hidden
  `#copytext` input that must exist in the page (`web/menuserver/index.html:46`,
  `js/utils.js:21-37`), followed by a blocking `alert('Copied!')`. Deprecated
  API; behavior (alert included) is user-visible.
- **Q4 — Title-casing is applied to data, then compared.**
  `topMenus[i].subject = titleCase(subject)` (menuserver.js:382) before the
  `subject != "Splash"` check (line 397) — so the splash exclusion works for
  any capitalization of the directory name, but display names are forcibly
  Title Cased (`titleCase` also **lowercases** the rest of each word:
  `utils.js:108-123`, so a directory `AWS` renders as `Aws`). Entry fetch
  paths use the *original* strings from `/items`, so only display is
  affected.
- **Q5 — Splash convention is two-layered**: the *subject directory* named
  `splash` is excluded from the nav (Q4), but the auto-shown page is the one
  whose JSON **`id` is literally `"splash"`** (`showPage('splash')`,
  menuserver.js:415-418). A splash file with a different `id` renders but
  never auto-shows.
- **Q6 — Duplicate page ids render once.** `renderedPageSet` dedupes page
  divs by JSON `id` across all subjects (menuserver.js:377, 392-395), so the
  same page can be reachable from several dropdowns. Distinct files sharing
  an id silently collapse to whichever loads first (alphabetical).
- **Q7 — Top-level `url` affects the nav only.** An entry with a top-level
  `url` becomes a direct external `<a target="_blank">` in the dropdown
  (menuserver.js:247-251), but its page div is *still rendered* (empty —
  the page-body `url` line is commented out at menuserver.js:319).
- **Q8 — `gdoc` branch is buggy**: it renders `json.notes`, not `json.gdoc`
  (menuserver.js:331-337). No live data uses `gdoc`, so faithfully porting
  the bug vs. fixing it is a decision, not an accident.
- **Q9 — Vestigial globals cross file boundaries.** menuserver.js's
  unreachable functions reference `lists` (defined only in dead
  `js/todo.js`) at menuserver.js:83 etc., `tj` (defined nowhere) at line 138,
  and `dropVars()` (defined only in dead `js/todo-utils.js:54`) at line 41.
  They only avoid throwing because nothing calls them on the live page. Any
  "port the whole file" approach imports latent ReferenceErrors.
- **Q10 — Implicit globals**: `label` in `renderSiteRow`
  (menuserver.js:277-278) and `submenu_index` (line 388) leak to `window` —
  harmless today, breaks under `"use strict"`/modules.
- **Q11 — Load-order dependency**: `js/utils.js` must load after jQuery
  (it registers `$.fn.checkUtils` at top level, utils.js:158) and before
  `js/menuserver.js`; `startMenuserver()` is called from an inline
  `$(document).ready` in the HTML (index.html:28-32). All scripts are
  synchronous `<script src>` tags in `<head>`.
- **Q12 — Passwords go to the browser console.** `startMenuserver` logs the
  entire fetched menu hierarchy (`JSON.stringify(topMenus)`,
  menuserver.js:407-408) and each submenu (line 391), credentials included;
  ~30 other `console.log`s fire on every load.
- **Q13 — Password toggle relies on paired ids** `"<prefix>pwd<i>_inner"` /
  `"<prefix>pwd<i>_hidden"` with a **random 5-char prefix generated at render
  time when `site.prefix` is undefined** (menuserver.js:275,
  `makeID(5)+"_"`), consumed by `toggle()` in utils.js:125-139. The masked
  placeholder is the literal string `xxxxxxxxxx` (menuserver.js:290).
- **Q14 — `port` rendering oddity**: if `site.port` is set the link becomes
  `site.url + ":" + port` — appended blindly, so a URL with a path would get
  `:port` in the wrong place (menuserver.js:273-274). Live data uses ports
  only in host-style URLs.
- **Q15 — `ajaxGetJSON` never rejects** (utils.js:12-19): a non-JSON
  response (e.g., an auth-gate HTML login page, or the index.html SPA
  fallback) surfaces as an unhandled promise rejection and the portal
  silently renders nothing past that point. There is no user-visible error
  state anywhere.
- **Q16 — showAllPages mode changes navigation semantics.** With
  `show_all_pages: true`: pages render visible, stacked, each followed by an
  up-arrow anchor to `#top` (an anchor that **does not exist** — browsers
  scroll to top anyway) and an `<HR>`; `showPage` sets
  `window.location = "#"+id` (hash jump, adds history entries) instead of
  hide/show (menuserver.js:311-358). Config default is `false`
  (live unified-webapp.json sets it explicitly false).
- **Q17 — FA4 sheet is loaded but broken** (`css/fontawsme.css` →
  missing `fonts/` dir) and FA 5.7.0's brands/eot/svg font files are missing;
  icons render only because FA5's `.fa` alias maps to fa-solid-900. A
  rewriter tidying "duplicate FontAwesome" must keep caret-down and arrow-up
  glyphs working.
- **Q18 — External CDN scripts exist in the tree** (dead pages only):
  `https://maxcdn.bootstrapcdn.com/bootstrap/3.4.1/js/bootstrap.min.js` at
  `web/menuserver/todo.html:18`, `onoff.html:19`, `exercise.html:18` —
  violates the FRD's no-CDN rule but only on dead pages.
- **Q19 — Backend list semantics**: `GET /items` hides dot-directories?
  No — it hides nothing at the directory level except non-dirs; it *does*
  skip subject dirs with no `*.json` and never lists non-`.json` files, but
  `GET /menus/...` will happily serve a non-`.json` file if asked
  (store.go:50, 70-77). Dot-prefixed names are blocked at the handler by
  `fspath.ValidName` (handler.go:55).
- **Q20 — `age`/`timestamp` fields**: always `0` (store.go:59-60), kept only
  for wire-shape compatibility with the Node.js original. Nothing reads them.
- **Q21 — Relative-URL assumption** (see §1): `config/`, `items`,
  `menus/…` resolve against the page URL; the portal works from `/` and
  `/index.html`/`/menuserver.html` but would break if ever mounted under a
  sub-path.
- **Q22 — The dead pages are still URLs.** `/todo.html`, `/slideshow.html`,
  `/onoff.html`, `/exercise.html`, `/base.html`, `/404.html`, `/50x.html`
  load today (broken to various degrees) on the menuserver host. Deleting
  them changes those URLs from "broken page" to "portal via SPA fallback".

## 7. Cross-module duplication (dead copies)

All **54 files** of `web/menuserver/` also exist in `web/todo/` and
`web/slideshow/`, **byte-identical** except:

| File | Difference |
|---|---|
| `todo/index.html`, `slideshow/index.html` | each module's own live entry page |
| `todo/js/todo.js`, `todo/js/todo-utils.js` | todo's live (newer) versions differ from menuserver's copies |

Everything else — including `js/menuserver.js`, `menuserver.html`, all 19
CSS files, jQuery/jQuery-UI/Bootstrap/Popper/axios/marked, webfonts, and
images — is a byte-identical triplicate (verified by `cmp` across the three
trees). `web/obsidianoid/` shares **zero** files. This matches FRD FR-9
("byte-identical dead copies of menuserver's: 19 CSS files, 17 JS files,
8 HTML stubs, webfonts, images — ×2"); FR-9 deletes the todo/slideshow
copies in an earlier phase, and **menuserver's own copy is the Phase-5
deletion target**. Note the FA webfonts are also slated for repo-wide
removal (FRD FR-6) once no module needs FA glyphs — menuserver is one of
the "FA fully removed repo-wide" gates in the Phase 5 row (FRD line 461).

## 8. Behavior checklist (must survive the rewrite)

User-visible behaviors of the **live** menuserver portal, current defaults
(`show_all_pages: false`) unless noted:

1. `GET http://<menuserver-host>/`, `/index.html`, and `/menuserver.html` all serve the portal; page title "NodeJS Menuserver"; favicon `images/fvw.png`.
2. On load the portal fetches `config/` then `items`, then every `menus/{subject}/{item}` listed, before any UI appears; an empty data dir yields a bar containing only the (mobile-hidden) hamburger icon and no error.
3. The top nav bar shows one dropdown button per subject directory, in alphabetical directory order, labeled with the Title-Cased directory name plus a caret-down icon.
4. Dropdowns open on mouse hover and close when the pointer leaves; items appear in alphabetical file order.
5. A menu entry whose JSON has a top-level `url` renders as a direct link that opens in a new tab (label = `title` if present, else the URL).
6. A menu entry without a top-level `url` renders as an item that, when clicked, hides all page panels and shows that entry's panel below the nav.
7. Non-http schemes in links (`ssh://`, `vnc://`) pass through to the OS handler.
8. Each page panel shows: the entry's Title-Cased `title` as an `<h2>`, then a spaced table of its `sites`.
9. A site row with only a `label` renders as plain text; with a `url` it renders as a new-tab link; when `label` and `url` differ the row shows "label : url"; when `port` is present the link target is `url:port`.
10. A site with a `username` additionally renders: a "Username:" row, a "Password:" row showing the mask `xxxxxxxxxx`, a "Hide/Show" button that toggles between the mask and a text input containing the real password, and a "Copy" button that copies the password to the clipboard and pops an alert ("Copied!" / "Falied to copy." on failure — note the existing typo).
11. A `notes` field renders below the sites as a paragraph, **interpreted as HTML** (existing notes may contain markup).
12. The same page `id` referenced from multiple subjects yields one shared panel reachable from each dropdown.
13. If a `splash` subject exists, it does not appear in the nav, and the page with id `splash` is displayed automatically on load.
14. At viewport width ≤600 px the dropdown buttons disappear and a ☰ icon appears at the right of the bar; tapping it expands/collapses the nav into a vertical list (dropdown items indented, still hover/tap-to-open); tapping again collapses it.
15. With `show_all_pages: true`: every panel renders visible in sequence, separated by horizontal rules, each followed by an up-arrow link that scrolls to the top; nav items become `#id` hash jumps (browser history gains entries; back button walks anchors).
16. `GET /config` and `GET /config/` return `{"showAllPages":<bool>}` as JSON.
17. `GET /items` returns the subject/entry catalog as a JSON array with fields `age` (0), `timestamp` (0), `subject`, `entries` (`["subject/file.json", …]`), `[]` when empty, alphabetically ordered, omitting subject dirs with no `.json` files.
18. `GET /menus/{subject}/{item}` returns the raw menu file with `Content-Type: application/json`; returns 404 for missing files, dot-prefixed or slash/backslash-containing names, and traversal attempts.
19. Requests for unknown static paths return `index.html` with 200 (SPA fallback); existing files under the static dir (including today's legacy pages) are served as-is.
20. `GET /healthz` returns `{"ok":true}`; `GET /api/auth/mode` and `/api/auth/whoami` respond on the menuserver host (gate-owned).
21. When `auth.modules.menuserver` is configured, every route above (except healthz/mode/whoami) requires a PIN session or API key exactly as the shared gate dictates; when absent, everything is anonymous.
22. The module boots even when the data dir doesn't exist (it is created, mode 0750), and a module build failure turns every request into a 503 JSON envelope without naming paths.
23. Request bodies over 1 MiB are rejected by shared middleware (no live flow sends a body today).
24. Data remains plain per-subject directories of individual JSON files — adding/removing a file or directory on disk changes the portal on next page load with no restart.

## 9. Open questions for the rewrite

1. **Fate of the seven dead pages** (`todo.html`, `slideshow.html`, `onoff.html`, `exercise.html`, `base.html`, `404.html`, `50x.html`) and their asset closure on the menuserver host: FRD FR-9 deletes the todo/slideshow *copies* and says "menuserver keeps its copy until Phase 5" — confirm Phase 5 deletes them all here and that no bookmark/automation depends on any of them.
2. **Read-only vs. editing**: the Go backend is read-only, but the JS still carries a full save path (`SaveList` → `POST items/{filename}`). Is Phase 5's contract "portal stays read-only, edit files on disk", or is a write API in scope?
3. **`notes` as raw HTML** (Q2/behavior 11): the shared-library escaping rule is `textContent`-only (FRD FR-5). Live notes are prose today, but the current renderer would honor embedded HTML. Decide: sanitize/render markdown, escape plainly, or grandfather HTML.
4. **Password handling**: plaintext credentials in menu JSON are served, embedded in `onclick` attributes, and console-logged (Q2, Q12). Does Phase 5 merely reproduce hide/show + copy with the modern Clipboard API, or is the credential UX (and the console logging) allowed to change?
5. **`gdoc` field** (Q8): buggy render path, unused in live data — port, fix, or drop?
6. **showAllPages mode** (Q16): still wanted? It's config-gated, currently off in the live config, and doubles the navigation semantics the shared HamburgerMenu must reproduce.
7. **Config surface**: old-Node config keys the JS still reads (`autosave`, `ext`, `defaultSubject`, `defaultItem` — menuserver.js:111, 131, 137) are never served by the Go `/config`. Confirm they are officially dead so the rewrite can drop them.
8. **`fvw.png` favicon and "NodeJS Menuserver" title**: keep, or rebrand under the puma theme? (Title currently names a runtime the app no longer uses.)
9. **Hover-only dropdown parity**: FR-4 requires keyboard-operable navigation; the current UI has zero keyboard support (Q/§5). The acceptance checklist should state explicitly that hover-open behavior is *replaced*, not preserved, so "zero functionality loss" is measured in reachable destinations, not input gestures.
10. **Alphabetical-only ordering** (Q of store.go): directory order is the only ordering mechanism (users prefix filenames to sort). Confirm the rewrite keeps filename ordering rather than introducing manifest-driven order.
