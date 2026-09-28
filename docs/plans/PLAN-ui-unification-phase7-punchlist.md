# Phase 7: Punch List Fixes

Status: `Approved`
Branch: `ui-upgrade`
Source: `docs/archived/PLAN-ui-unification-phase6-punchlist.md` (section labels renumbered D0–D7)
Absorbed: the utuber→taskmaster lane plan (`docs/plans/PLAN-utuber-taskmaster-lane.md`). Its
internal `Phase 1`–`Phase 6` are **not** Phase 7 phases and are not called "phase" anywhere in
this document. Steps 1–4 of that plan are done; its steps 5 and 6 are now **D8** and **D9** below.

## Read this first: execution order and ownership

**One phase vocabulary: D0–D9.** Every section here is a Phase 7 section. The lane plan keeps
its own `§6` step numbering for internal cross-reference only — if a line there says
"Phase 5", it means step 5 *of that plan*, which is D8 here.

**Run them in this order.** The order is not alphabetical and not arbitrary; three sections
rewrite files another section also edits.

| # | Section | What it does | Must run after |
|---|---|---|---|
| 1 | **D0** | Tear down hamburger auto-detection; `side` comes from config only | — |
| 2 | **D1** | Anchor the menuserver ☰ far right of the header | — |
| 3 | **D2** | *No action.* certmachine is tested, verified, complete | — |
| 4 | **D3** | multissh: move ☰ right; sign-out control already exists — verify | D0 |
| 5 | **D4** | smbedit: stop scrolling below the footer | — |
| 6 | **D7** | Login page wears the module's own theme — **verify before building** | — |
| 7 | **D8** | Migrate utuber onto the taskmaster lane; delete `internal/utuber/jobs` | D0 |
| 8 | **D5** | utuber: surface yt-dlp's real reason + optional cookies | **D8** |
| 9 | **D6** | taskmaster: queue panel + headless owned-lanes mode — *verify, mostly done* | D8 |
| 10 | **D9** | Lane docs + full `make check` gate | **D5, D6** |

**Why D5 comes after D8, and this is not negotiable.** D8 rewrites `internal/utuber/build.go`,
`handler.go`, `settings.go`, `media/exec.go` and `web/utuber/js/main.ts` — the exact five files
D5 edits. D8 also pins the `/settings.json` response to a fixed key set that has no cookie
field. Build D5 first and D8 overwrites it; build D8 first and D5's cookie keys have to be added
to the *new* response shape. So D5 is written against post-D8 code, in `rc.Progress` / `rc.Log`
terms, not against today's `progressCallback`. See D5's "Rebased onto D8" note.

**Two fixes exist in two plans. Do not do either twice.**

- `internal/utuber/media/exec.go` scanner goroutines — **D8 owns this** (lane plan step P15).
  D5 does not touch `exec.go`. P15 *deletes* the two scanner goroutines and replaces them with a
  single shared `lineWriter`, so "join the goroutines before `Run` returns" is not merely
  redundant here — after D8 there are no goroutines left to join, and D5's old test for it
  cannot even compile.
- The failure-reason text — **D5 owns this.** P15 sends raw yt-dlp lines to `rc.Log()`, but
  nothing in the lane plan ever puts the `ERROR:` line into the job's error field. That is the
  live bug and it survives D8 untouched.

## RALPLAN-DR Summary

### Principles
1. **Fix regressions before features** — restore broken functionality first, then improve UX
2. **Cross-cutting before per-module** — fix shared components so per-module fixes inherit them
3. **No scope creep** — fix what's on the punch list, nothing more
4. **Preserve existing behavior** — the fix for a bug must not break something else
5. **One hamburger per module** — consolidate duplicate menus, don't add new ones
6. **hamburger should popout on the side that it's triggered from**
7. **Guidance that starts with "CMD>" is immune to reviewer disapproval.**  In other words, CMD> statements in this document are hard law and must be implemented as stated.

### Decision Drivers
1. Shared HamburgerMenu side-mismatch affects every module — fixing it once fixes many items
2. login screen theme mismatched; login screen should take on current theme per module

### Viable Options

**Chosen Methodology: Layered commits by scope (cross-cutting → per-module)** — Fix shared component issues first (CC-1 side agreement), then sweep per-module fixes grouped by severity.
- Pros: Cross-cutting fixes cascade to modules; clear dependency ordering; easier to review
- Cons: More commits; later modules wait for shared fixes

## Commit Sequence

### D0: HamburgerMenu side option + close button + title (CC-1, CC-3 prep)
**Files:** `web/shared/ts/menu.ts`, `web/shared/css/components.css`, `web/shared/ts/menu.test.ts`

Add `side?: "left" | "right"` option to `HamburgerMenuOptions`. Default `"left"`. When `"right"`:
- CSS: drawer anchors `right: 0` instead of `left: 0`, `border-left` instead of `border-right`, `translateX(100%)` for closed state
- Add `data-side="right"` on `.ui-menu-drawer` to select CSS variant

Add visible close button (X) at top of drawer — currently only closes via backdrop click or Escape.

~~Auto-detect is **opt-in only** via `side: "auto"`. When `"auto"`, detection is deferred to the first `open()` call (not the constructor) where layout is guaranteed settled — `getBoundingClientRect()` on the trigger determines left/right based on viewport midpoint. If no `side` is set, the default remains `"left"` for backward compatibility. This avoids timing-fragile constructor-time layout queries.~~

CMD> This feels more complex than it should be.. and that's why it's still broken.  some modules still have the hamburger trigger on the opposite side of the screen.  Let's make it more deterministic:  Set the hamburger side in configuration, per module in admin menu and the per module json.  So each module gets to chose the location instead of expensive "auto detection".  This guidance is approved and trumps all reviewers.

**What is already done — do not rebuild it.** The `side` option, the `data-side` CSS variant, the
close button and the title all shipped in `f7e9157` ("C0: HamburgerMenu side option + close
button + title"). The `side-wiring table` below confirms every module already passes `side`
explicitly or accepts the deliberate `left` default.

**The only live work in D0 is the teardown.** `"auto"` is still implemented and still reachable.
Measured from source, 2026-09-28 — every site below is real, but the teardown is **eight** edits
across two files, not four:

| File:line | What it does | Action |
|---|---|---|
| `web/shared/ts/menu.ts:102` | doc comment: `"auto" defers the choice to the first open()` | rewrite to describe the configured edge |
| `web/shared/ts/menu.ts:106` | `side?: "left" \| "right" \| "auto"` — the type still permits `"auto"` | narrow to `"left" \| "right"` |
| `web/shared/ts/menu.ts:212-213` | comment + `private resolvedSide: "left" \| "right" \| null` cache field | delete both |
| `web/shared/ts/menu.ts:227-234` | the constructor resolver, **three** branches: `left`/`right` → set, `auto` → null, else → `left` | collapse to two: configured value, else `left` |
| `web/shared/ts/menu.ts:287-292` | `open()`'s `side === "auto" && resolvedSide === null` guard, then `getBoundingClientRect()` midpoint and the `dataset.side` write | delete the whole block; `dataset.side` is already set in the constructor |
| `web/shared/ts/menu.test.ts:77-82` | `getBoundingClientRect()` test double — its own comment reads *"Only side:\"auto\" ever calls this"* | delete the double |
| `web/shared/ts/menu.test.ts:207` | `const fakeWindow = { innerWidth: 800 }` — *"Only side:\"auto\" ever reads this"* | delete |
| `web/shared/ts/menu.test.ts:872-914` | the `side: "auto"` test block: 3 checks (no `data-side` before first open; resolves right on the right half; caches the resolution) | delete the block |

Those last three are why this cannot be a pure "delete dead code" pass: three tests assert the
behaviour being removed, and the two fakes exist only to feed it. **Delete the tests; do not
re-point them at `left`/`right`.** They encode the guess the CMD> calls expensive and wrong, and
keeping them would keep `getBoundingClientRect` alive in the test harness for no reason.

`getBoundingClientRect` is a layout-forcing call on a value the config already knows, which is why
it is unreliable rather than merely unnecessary.

**Acceptance criteria:**
- `side` is typed `"left" | "right"`; passing `"auto"` is a **compile-time** error, not a runtime
  fallthrough to the default
- No `getBoundingClientRect` and no `resolvedSide` remain in `menu.ts`
  (`grep -nE 'getBoundingClientRect|resolvedSide|"auto"' web/shared/ts/menu.ts` → no output)
- The same grep over `web/shared/ts/menu.test.ts` → no output; the two autodetection fakes and the
  three `side: "auto"` checks are **gone**, not re-pointed at `left`/`right`
- `side: "right"` still produces a right-anchored drawer with the correct animation direction
- Close button (X) visible at top of drawer, closes on click
- Title text visible in drawer header
- A module that omits `side` still gets `left` — the default is unchanged
- `npm run typecheck` and `npm run test:web` pass; every *remaining* `menu.test.ts` test passes
- `make web-verify` reports the committed `bundle.js`/`bundle.css` byte-identical after
  `npm run build` — D0 is shared code, so **every** module's artifact changes
- CMD> position is decided on configuration; default is left side. NO AUTODETECTION

**Side-wiring table — measured from source, 2026-09-28.** `side` is what each module's
`new HamburgerMenu({...})` call passes today. Absent means the shared default `left` applies.
Every module is therefore already **deterministic**; what varies is whether `left` was chosen
deliberately or arrived by omission.

| Module | call site | `side` passed | Note |
|--------|-----------|---------------|------|
| admin | `web/admin/js/main.ts:931` | `"right"` | explicit |
| menuserver | `web/menuserver/js/main.ts:252` | `"right"` | explicit |
| slideshow | `web/slideshow/js/app.ts:385` | `"right"` | explicit |
| taskmaster | `web/taskmaster/js/main.ts:216` | `'right'` | explicit |
| certmachine | `web/certmachine/js/main.ts:8` | `'right'` | explicit |
| timetracker | `web/timetracker/js/main.ts:18` | `'right'` | explicit |
| issuetracker | `web/issuetracker/src/main.tsx:15` | `"right"` | explicit; `.tsx` |
| smbedit | `web/smbedit/src/main.tsx:29` | `'right'` | explicit; `.tsx` |
| grocery | `web/grocery/js/main.ts:7` | *absent* → `left` | trigger `.prepend`ed into `.header-right` |
| todo | `web/todo/js/shell.ts:55` | *absent* → `left` | passes `mountTrigger` |
| multissh | `web/multissh/js/main.ts:32` | *absent* → `left` | **D3 adds `side: "right"`** |
| obsidianoid | `web/obsidianoid/js/app.ts:856` | *absent* → `left` | needs a look |
| utuber | `web/utuber/js/main.ts:6` | *absent* → `left` | needs a look |
| sampler | `web/sampler/js/main.ts:428` | *absent* → `left` | needs a look |

8 modules pass `side` explicitly; 6 take the default. The 6 default rows are the ones worth a
human look, and D3 settles exactly one of them (multissh). CMD> is explicit that `left` can be the
right answer for a page — the requirement is that it be **chosen**, so each of obsidianoid, utuber,
sampler, grocery and todo should be eyeballed once and, if the default is not what looks right,
given an explicit `side`.

> The first version of this table was wrong in five places and has been replaced. It reported
> `obsidianoid` and `utuber` as passing `"right"` when neither passes any `side` at all; it
> reported `smbedit` as absent when `smbedit/src/main.tsx:29` passes `'right'`; it called
> issuetracker "no `HamburgerMenu` call" when `issuetracker/src/main.tsx:15` constructs one with
> `side: "right"`; it omitted `sampler` entirely; and it marked multissh "default is correct"
> while **D3 changes it to `right`**. Every row now carries its call site so it can be re-checked
> with one grep. `make web-verify` is green (22 artifacts, byte-identical), so the emitted `.js`
> matches every `.ts` and none of this is build drift.

CMD> The above table may be inaccurate; please double check it.  I'm looking for consistency, but some pages may look better with left instead of right, so it should be deterministic in the configuration.  Tear down the autodetection for this - it does not work.

> The table above *was* inaccurate — gold mind of wrong info.  It claimed eight modules still
> needed `side` wired when seven of them already pass it explicitly, called a module with no
> `HamburgerMenu` call "no HamburgerMenu" for the wrong reason, pointed five rows at Phase-6
> commit labels that had nothing to do with the sections, and had a three-column header over
> two-column rows.  All of it was confident, specific, and wrong, which is the failure mode this
> punch list is most exposed to: a plan that reads authoritative invites implementation on trust.
> The table above is now measured from source instead of remembered.  Treat the rest of this
> document the same way — where it names a file, a line, or an existing symbol, check it.


### D1: Menuserver — anchor the ☰ at the far right of the header (M-1, M-2)
**Files:** `web/menuserver/js/main.ts` (and its CSS, wherever `.topnav` is defined)

CMD> this section needs to be rewritten, but the only issue with menuserver is that the hamburger trigger is floating on the header, it should be anchored far right, not just hanging out to the right of the last menu.

**Scope: trigger placement only.** Auth is already correct here — the module's `side` is
`"right"` (`main.ts:256`), which D0's table confirms. Do not revisit its auth, and do not change
the drawer side; the drawer is already on the right. The bug is that the *button* is not pushed
to the edge, so it sits floating after the last nav item.

**The bug, from source.** The trigger is `document.createElement("button")` with class
`topnav-btn topnav-settings`, then `nav.append(settingsTrigger)` — the last child of the nav, so
it lands wherever the previous item ends:

```ts
const settingsTrigger = document.createElement("button");
settingsTrigger.className = "topnav-btn topnav-settings";
...
nav.append(settingsTrigger);
```

Appending as the final child gives it `width: auto` and no claim on the free space at the end of
the bar. The fix is layout, not markup: the nav row must distribute its space so the trigger is
pinned to the trailing edge — `margin-left: auto` on `.topnav-settings` (the standard one-line
answer), or `justify-content: space-between` with the item group and the trigger as two flex
children. Whichever you pick, the trigger ends flush to the right edge at every viewport width.

**Acceptance criteria:**
- The ☰ button's right edge sits flush against the nav container's right edge, with no gap and
  no dead space to its right, at wide and narrow viewports
- The button is still the last tabbable element and still opens the same `HamburgerMenu` drawer
  with `side: "right"`
- The other `.topnav-item` entries keep their current order and spacing
- `npm run typecheck` and `npm run test:web` pass; `make web-verify` byte-identical after build

### D2: Certmachine

CMD> no action items for this module; certmachine is tested, verified and complete.

**No work.** Do not open this module. Its plan is at
`docs/archived/PLAN-certmachine-ca-replacement.md`, its manual checks at
`docs/archived/MANUAL-VERIFICATION-certmachine-ca-replacement.md`, and the owner's verdict is
recorded in that file: *"all 12: Approved and VERIFIED! Stop asking about this!"* The file's
`PENDING OWNER SIGN-OFF` header is older than that line and has not been updated.

### D3: Multissh — move ☰ right; the logout button already exists (MS-1, MS-2)
**Files:** `web/multissh/js/main.ts`, plus `.app-header` in **both** `web/multissh/js/ssh.css:29`
and `web/multissh/js/bundle.css:30`

CMD> Once the hamburger placement bug is fixed in shared code, this should be simple enough to do in config.

**MS-1 is two edits, not one. MS-2 is already built — verify, do not rebuild.**

- **MS-1a (config)** — multissh constructs `new HamburgerMenu({ title: "MultiSSH", items, themePicker: true, themes, mountTrigger: trigger })`
  at `main.ts:31-38` and passes **no `side`**, so it silently takes the `left` default. Add
  `side: "right"` to that call. This is the "simple enough to do in config" half, and it depends
  on nothing but D0 confirming the default is `left`.
- **MS-1b (layout)** — **`side: "right"` alone does not move the trigger.** It only sets which edge
  the *drawer* slides from (`data-side` on `.ui-menu-drawer`); it says nothing about where the ☰
  sits. The trigger is built by multissh itself (`main.ts:20-23`, a hand-written `☰` SVG) and
  placed with `header.append(trigger, title)` at `main.ts:29` — **trigger first**, so it renders at
  the *leading* edge. `.app-header` is `display: flex; align-items: center; gap: …` with **no
  `justify-content`**, so its children pack to the start. MS-1a on its own therefore leaves a
  right-opening drawer behind a left-hand ☰, which is the same mismatch in a new place.

  Land the ☰ at the trailing edge by changing the flex behaviour, not by reordering the append:
  either `justify-content: space-between` on `.app-header`, or `margin-inline-start: auto` on the
  trigger. `.app-header` is declared **twice, identically** — `ssh.css:29` and `bundle.css:30` — and
  both are tracked and hand-maintained (neither is generated), so **both must be edited or the
  module will render differently depending on which stylesheet the page loads first.** There is no
  `.topnav` in multissh; an earlier draft of this section said there was.

  The sign-out rides along for free. `mountSignOut` wraps `[ Sign out ][ ☰ ]` into one
  `.ui-menu-actions` group and inserts it *at the trigger's position*, so wherever MS-1b puts the ☰,
  the sign-out lands immediately to its left — the order the CMD> asks for either way.
- **MS-2** — **the logout button already exists, and multissh already has it.** It comes from
  shared code, not from a multissh source file:

  - `web/shared/ts/menu.ts:275` — `if (options.signOut !== false) this.unmountSignOut = mountSignOut(this.trigger)`.
    The behaviour is **on by default**; a module opts out by passing `signOut: false`
    (`menu.ts:112`). multissh passes no `signOut`, so it gets the button.
  - `web/shared/ts/session.ts:201` `mountSignOut` places it in the required order,
    `[ …nav items… ] [ Sign out ] [ ☰ ]`: it wraps both in one `.ui-menu-actions` group and calls
    `trigger.before(group)`, then `group.append(buildSignOutButton(), trigger)`.
  - `buildSignOutButton()` (`session.ts:47`) is a real control that posts to
    **`POST /api/auth/logout`** — the same `handleLogout` at
    `internal/platform/auth/handlers.go:217` — disables itself, then reloads onto the login page.

  An earlier draft of this section claimed the button did not exist, on the strength of a grep for
  `logout`/`signout`/`sign-out`/`log-out` across `web/multissh/**` that returned zero matches. That
  grep only covered multissh's *own* files; the control lives in `web/shared/`, which is why it
  found nothing. **Do not build a second logout control.** The correct work is to confirm the
  shared one renders, and to confirm D0's teardown of `side: "auto"` does not disturb it.

  It already handles the case the old draft got wrong: `mountSignOut` returns early unless
  `GET /api/auth/session` reports `signed-in`, so an open multissh shows no sign-out control
  rather than one that silently does nothing. An open module answers `unknown`, not `signed-in`,
  because it has no auth routes at all.

**Acceptance criteria:**
- `new HamburgerMenu({ …, side: "right" })` at `main.ts:31-38`; the drawer opens from the right
- multissh's ☰ sits flush at the **trailing** edge of its `.app-header` (there is no `.topnav` in
  this module) — and it is genuinely at that edge, not merely the last child of a left-packed flex row
- `.app-header` carries the trailing-edge rule in **both** `ssh.css:29` and `bundle.css:30`, so the
  page renders the same whichever stylesheet loads first
- The ☰ and the drawer agree: a right-side `side` with a right-hand ☰, never one without the other
- On a multissh listed in `auth.modules`, the shared **Sign out** control renders immediately left
  of the ☰, inside a single `.ui-menu-actions` group
- Clicking it ends this module's session and lands on the login page; after signing out, loading
  any protected multissh URL does not restore the session
- On an *open* multissh (not in `auth.modules`), no sign-out control renders — never one that
  appears and does nothing
- `npm run typecheck` and `npm run test:web` pass; `make web-verify` byte-identical after build

### D4: Smbedit — page scrolls below the footer
**Files:** `web/smbedit/src/styles.css`, `web/smbedit/src/App.tsx`

CMD> the only issue is a strange page scrolling issue.  It is possible to scroll too far down and you wind up below the footer

**Scope: the scroll bug and nothing else.** smbedit is otherwise working correctly. Do not audit
this module, do not restyle it, and do not touch its `left` hamburger — the D0 table already
settled that. One bug, one fix.

**Where the bug lives.** smbedit is a React app, so its layout CSS is
`web/smbedit/src/styles.css` — compiled to `web/smbedit/js/bundle.css` and `bundle.js`. The
obvious-looking path `web/smbedit/style.css` **does not exist**; an earlier draft of this section
named it. Two rules in that stylesheet are the candidates, and they are read together:

| Line | Rule | Role |
|---|---|---|
| 25 | `min-height: 100vh` on the page root | makes the document exactly one viewport tall at minimum |
| 39 | `height: 100vh` on the app shell | fixes the shell to a viewport, while the footer sits *inside* it |

`min-height: 100vh` on the root plus a footer inside a `height: 100vh` shell is the classic
overscroll shape: the root can never be shorter than the viewport, so there is always leftover
scroll range, and the content that should sit at the bottom of the shell ends up above it.

**Acceptance criteria:**
- The page cannot be scrolled such that any part of the footer is above the bottom edge of the
  viewport, and there is no empty scroll range past the footer
- Scrolling to the bottom shows the footer flush with the bottom of the window
- All smbedit pages still scroll internally where they are meant to (`overflow-y: auto` regions
  at styles.css:96 and :138 must keep working — they are not the bug)
- `npm run typecheck` and `npm run test:web` pass
- `npm run build` then `make web-verify` is byte-identical for `web/smbedit/js/bundle.{js,css}`

### D5: Utuber — age-restricted downloads fail, and failures are undiagnosable
**Files:** `internal/utuber/media/downloader.go`, `internal/utuber/settings.go`, `web/utuber/js/main.ts`, `web/utuber/index.html`, `internal/platform/config/config.go`, `docs/guides/utuber.md`
**Runs after D8.** Do not start this section until D8 has landed and the gate below is green.

CMD> Downloads appear to be broken

### Rebased onto D8 — read this before writing any code

D8 migrates utuber onto the taskmaster lane and **rewrites the files this section used to list**.
Three consequences, all of which change the implementation:

1. **`media/exec.go` is not D5's file.** D8's item P15 rewrites `OSExecutor.Run` outright: it
   deletes `StdoutPipe` and both scanner goroutines, replacing them with a single shared
   `lineWriter` plus `Setpgid`, process-group kill and `WaitDelay: 10s`. D5 must not touch
   `exec.go`, and the old D5 test that asserted "both scanner goroutines are finished when `Run`
   returns" is deleted by D8 — there are no goroutines left to join.
2. **Progress reporting is now `rc.Progress` / `rc.SetLabel` / `rc.Log`,** not `progressCallback`.
   D8 already sends every raw yt-dlp line to `rc.Log()`. So the diagnostic channel below becomes
   "capture the `ERROR:` line as well as logging it", not "build a channel from scratch".
3. **The `/settings.json` response shape is fixed by D8** to
   `{python_bin, age_out_days, concurrent_downloads, show_in_taskmaster, queue_paused,
   queue_paused_by, brake_engaged}`. D5's `cookies_configured` and cookie-mtime fields are
   **added to that set** — extend the documented contract, do not replace it, and update
   `docs/guides/utuber.md` (and D8's own §4.10 table) so the two agree.

**D8's gate for D5 to start:**

```sh
test ! -e internal/utuber/jobs && echo gone                    # → gone
grep -rn "utuber/jobs" --include='*.go' . | wc -l              # → 0
grep -rni utuber internal/taskmaster | wc -l                   # → 0
grep -n 'lineWriter' internal/utuber/media/exec.go             # → present
go test -race -count=1 ./internal/utuber/... ./cmd/server/
```

**Diagnosis (verified on hero, 2026-09-28, against the pre-D8 tree).** Two independent defects.
Neither is a regression, and neither has anything to do with the taskmaster lane migration —
`build.go`'s `Build(cfg, host)` is still the forgetful shim (`_ = host`), so utuber runs its own
`internal/utuber/jobs` pool exactly as before.

1. **Age-restricted videos cannot authenticate.** `media.Download` hardcodes its argv and
   passes no credential of any kind — `grep -rniE 'cookie|netrc|password' internal/utuber/`
   returns nothing. For both reported URLs yt-dlp answered
   `ERROR: [youtube] <id>: Sign in to confirm your age. Use --cookies-from-browser or --cookies`
   and exited 1. The toolchain is healthy: yt-dlp 2026.08.19, ffmpeg 5.1.8, ffprobe and
   python3.12 all present, and an 80 MB file completed through this identical code path at
   22:02 on 2026-09-27 (`history.json`). The content is gated, not the pipeline.
2. **The failure reason never reaches the UI.** `downloader.go`'s progress handling tests
   every line against `\[download\]\s+([\d.]+)%` and discards the rest, including yt-dlp's
   `ERROR:` lines. `processor.Process` therefore sees only `cmd.Wait()`, and the job records
   `err.Error()` — the literal string `exit status 1`. With no progress line ever arriving, the
   job sits at `"downloading"` and then flips to failed with no stated reason.

   **This one survives D8 untouched.** P15 gets every raw line into `rc.Log()`, but nothing in
   the lane plan ever puts the `ERROR:` line into the job's *error field*. Fix 1 below is the
   only thing in either plan that does, and it is the reason D5 still exists.

**Fix 1 — surface yt-dlp's output. Land this even if the cookie work is deferred; without it
every future utuber failure costs a manual investigation like this one.** This is D5's
unshared work; nothing in the lane plan does it.
- Keep progress parsing regex-scoped. Every non-progress line, stderr especially, is retained
  rather than discarded.
- Every such line goes to `rc.Log()` — D8 already wires this, so confirm it rather than
  re-adding it.
- **The new part:** capture the last non-empty `ERROR:` line and return it as the error, so the
  execution's error field carries yt-dlp's actual reason instead of `exit status 1`. The
  mechanism is whatever the lane exposes for setting a failure reason on a func execution —
  read D8's landed `golane`/`worker` code for the exact call and use it. Do not invent a
  parallel error channel, and do not change the wire format of `/jobs.json` to carry it.
- **Do not touch `media/exec.go`.** The old bullet here read *"Join both scanner goroutines in
  `exec.go` before `Run` returns. Closes P15."* — that is **P15, and P15 is D8's work**, done by
  deleting both goroutines. Implementing it in D5 would mean writing code that D8 then deletes.
  D8 also removes the old D5 test `exec_test.go: both scanner goroutines are finished when Run
  returns`, which cannot exist once `lineWriter` is in place.

**Fix 2 — cookie support, with a UI affordance.**
- yt-dlp cannot generate YouTube cookies; there is no programmatic login. The file has to be
  produced on a machine that already holds a signed-in YouTube browser session, in Netscape
  format. So the UI **accepts** a cookie file, it does not create one, and the Settings copy
  must say so plainly rather than implying the module can sign in. Two ways to produce it:
  - a browser extension that exports Netscape format ("Get cookies.txt LOCALLY"), or
  - on the signed-in machine: `yt-dlp --cookies-from-browser chrome --cookies cookies.txt --skip-download <any-url>`
- New config key `utuber.cookies_file`. Empty means no cookies, which is today's behaviour —
  yt-dlp's `--no-cookies` is already the default, so omitting the flag changes nothing.
- `media.Download` appends `--cookies <cookies_file>` only when the file exists, so deleting
  the cookie degrades to today's behaviour instead of an error. (Verified: yt-dlp tolerates a
  missing `--cookies` path, but being explicit keeps the logged argv honest.)
- The UI posts the cookie text to the existing `POST /settings.json`. `settingsFile` gains a
  `cookies_txt` field and `settingsStore` gains the cookie path, so this reuses the mechanism
  the `python_bin` control already sits on rather than adding an endpoint. The file is written
  `0600`. Hardening the cookie file beyond that mode is deliberately **out of scope** here —
  utuber is login-protected on the LAN, and tighter handling is a separate, later decision.
- `handleSettings`' GET response **adds** `cookies_configured` and the file's mtime to the seven
  keys D8 already defines, and never returns the contents, so the UI can show presence and age
  without round-tripping the secret. Extend D8's contract; do not replace or reorder it.
- `web/utuber`: in the existing Settings section (`web/utuber/js/main.ts`, beside the
  `python-bin` control) add a textarea with Save and Clear, plus copy explaining the file must
  be exported from a signed-in browser on another machine. Surface the cookie file's age —
  YouTube rotates these, and a stale jar fails identically to no jar, so staleness has to be
  visible in the UI.
- `docs/guides/utuber.md` documents the workflow, and drops the now-false implication that
  yt-dlp needs no credentials.

**Acceptance criteria:**
- An age-restricted URL downloads when a valid cookie file is configured.
- The same URL without a cookie file fails with a `j.Error` that names the yt-dlp reason, not
  `exit status 1`.
- A stale or wrong cookie file produces the same informative error, not a silent failure.
- `POST /settings.json` with valid Netscape text writes the cookie file `0600`; with
  unparseable text it returns 400 and leaves any existing file untouched.
- Deleting the cookie file returns the module to today's behaviour with no config error.
- `media_test.go`: stderr-only failure surfaces the yt-dlp `ERROR:` line; `[download] 45.2%`
  still parses as progress; `--cookies` absent when unconfigured and present when configured.
- `GET /settings.json` returns D8's seven keys **plus** `cookies_configured` and the mtime —
  nine in total, with none of D8's dropped or renamed.
- The old `exec_test.go` assertion that both scanner goroutines are finished when `Run` returns
  is **not** reintroduced. It was D8's to delete, and it cannot compile against `lineWriter`.

### D6: Taskmaster — close out the queue-panel work, then the lane takes over
**Files:** `web/taskmaster/js/board.ts` (verify only)

CMD> see taskmaster/utuber lane plan - already approved!

**This section is a verification gate, not a work item.** The lane plan's steps 1–4 are done;
D8 and D9 are steps 5 and 6. Confirm that before starting anything:

```sh
grep -rni utuber internal/taskmaster | wc -l              # → 0        (lane plan rule R4)
grep -c QueuePanel web/shared/ts/index.ts                # → 3        (exported from the barrel)
grep -c QueuePanel web/taskmaster/js/board.ts            # → 8        (adopted as the first consumer)
test -d internal/taskmaster/golane && echo golane-ok     # → golane-ok
git status --porcelain web/                              # → empty     (artifacts committed)
```

If all five hold, D6 is **done** — record that and move on. Do not re-adopt the panel, and do
not begin taskmaster work here: the remaining taskmaster behaviour is headless owned-lanes mode
(item P16) and func-metric aggregation (P17), both of which ship inside D8 with the code they
depend on. Splitting them from D8 would mean editing files D8 is about to rewrite.

### D7: Login page wears the module's own theme — **verify before building**
**Files:** `internal/platform/auth/login.html` (only if the verification fails)

CMD> login page looks great, except that it does not keep the theme for the module.  I suspect that we're using a global theme for all login pages -- if so, that's incorrect.  Test: log in: change theme, log out.  Attempt to log in again, the log in should reflect the theme in use for that module.  Try to log in to another module and it should take on the theme for that module.

**The mechanism is already implemented. Do not rebuild it.** The owner's report predates it, or
describes a case it does not cover. From source, verified 2026-09-28:

| Piece | Where | What it does |
|---|---|---|
| Template placeholder | `login.html:14` | `var m = "__UW_MODULE__"` |
| Server substitution | `internal/platform/auth/gate.go:340` | replaced with the routed module name, HTML-escaped |
| Primary lookup | `login.html:16` | `localStorage.getItem("ui-theme:" + m)` |
| Per-module fallback | `login.html:16` | `localStorage.getItem(m + "-theme")` |
| Per-vault fallback | `login.html:17-20` | scans for a key prefixed `m + "-theme-"` (obsidianoid's vault-scoped keys) |
| System fallback | `login.html:22-24` | `prefers-color-scheme` when nothing is saved |
| Applies it | `login.html:25` | `document.documentElement.dataset.theme = t` |
| Key it reads | `web/shared/ts/theme.ts:194` | `storageKey()` returns `` `ui-theme:${this.options.module}` `` |

`ThemeManager` writes `ui-theme:<module>`, and every module's `ThemeManager` name **equals its
routing name** — checked for todo, grocery, slideshow, obsidianoid, multissh, smbedit, utuber,
certmachine, menuserver, taskmaster and admin. The primary lookup therefore resolves for all of
them, and there is no global theme: nothing is stored globally, so nothing global can be read.

**The one real residual:** `<html data-theme="dark">` is hardcoded at `login.html:2` and only
corrected by the inline script. The script is synchronous in `<head>`, so this does not visibly
flash today — but it is the fragile part, and it is what to look at first if the theme ever
looks wrong.

**Do this first — the owner's own procedure, unchanged:**

1. Log into a module, change its theme, log out.
2. Log in again. The login page must show that module's theme.
3. Log into a *different* module. Its login page must show *its* theme.

**If step 2 or 3 fails, the bug is not "a global theme" — it is one of exactly three things.**
Check in this order, and fix only what you find:

1. The module's `ThemeManager({ module: ... })` name does not equal its `host_routing` name, so
   it writes a key the login page never looks up.
2. The module stores its theme under a bespoke key that none of the three lookups cover
   (`theme.ts:194` also honours an explicit `storageKey` override at `theme.ts:111`).
3. `localStorage` is unavailable or partitioned, so the script silently falls through to the
   system preference — note this is already handled by the `try/catch` at `login.html:15`, so
   if this is the cause, the fix belongs in the module, not the login page.

**Acceptance criteria:**
- Steps 1–3 above pass for at least two different modules with different saved themes
- `grep -n 'data-theme' internal/platform/auth/login.html` shows the hardcoded default is either
  gone or provably applied before first paint, and the module's saved theme still wins
- A module with no saved theme still follows `prefers-color-scheme`
- `go test -race -count=1 ./internal/platform/auth/` passes

### D8: Migrate utuber onto the taskmaster lane; delete `internal/utuber/jobs`
**Authority:** `docs/plans/PLAN-utuber-taskmaster-lane.md` §4.10 and §6 step 5. That plan is
owner-approved and is the specification. This section does not restate it — it states the
boundary, the invariants that survive into Phase 7, and the gate. **Read §0 of the lane plan
first**; rules R1–R8 there are binding on this section, especially R1 (no `db.conn` or
`(*DB)` call inside a transaction — `Open` sets `SetMaxOpenConns(1)` in
`internal/taskmaster/db/db.go`, so such a call deadlocks) and R4 (no `utuber` name in
taskmaster core).

**Files:** `internal/utuber/{build,handler,settings}.go`, `internal/utuber/media/exec.go`,
`internal/utuber/media/exec_test.go`, `internal/utuber/{build,handler,settings}_test.go`,
new `internal/utuber/main_test.go`, `web/utuber/js/main.ts`, `web/utuber/style.css` (dead
`.progress-*` rules only, where no selector remains), `web/utuber/js/bundle.js`.
**Delete:** the whole `internal/utuber/jobs/` directory.

**Item P15 is the part that matters to the rest of Phase 7.** `OSExecutor.Run` is rewritten
outright: `cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}`, `cmd.Cancel` killing the
process *group* with `SIGKILL`, `cmd.WaitDelay = 10 * time.Second`, and `cmd.Stdout`/`cmd.Stderr`
bound to **one shared `lineWriter`** that splits on `\n`, buffers partial lines, serialises
`onLine` under a mutex and flushes the remainder after `Wait`. `StdoutPipe` and both scanner
goroutines are **deleted**. D5 depends on this landing first.

**Invariants D8 must not break:** R4 (`grep -rni utuber internal/taskmaster` → 0), R7 (the
`Workers: 0` test-safety contract — a lane width of 0 means the lane starts nothing, and no
test may invoke real yt-dlp), and the `/settings.json` key set D5 will extend.

**Gate — all five must hold before D5 starts:**

```sh
test ! -e internal/utuber/jobs && echo gone         # → gone
grep -rn "utuber/jobs" --include='*.go' . | wc -l   # → 0
grep -rni utuber internal/taskmaster | wc -l        # → 0
grep -n 'lineWriter' internal/utuber/media/exec.go  # → present
go test -race -count=1 ./internal/utuber/... ./cmd/server/
npm run typecheck && npm run build && make gates
```

### D9: Lane documentation and the full gate
**Runs after D5 and D6.** Authority: lane plan §6 step 6.

**Use these paths.** The lane plan names all three of its documentation targets with paths that
**no longer exist** after the docs were reorganised. Corrected:

| Lane plan says | The real file |
|---|---|
| `docs/taskmaster.md` | `docs/guides/taskmaster.md` |
| `docs/utuber.md` | `docs/guides/utuber.md` |
| `docs/FRD-utuber-taskmaster-lane.md` | `docs/frd/FRD-utuber-taskmaster-lane.md` |

D9 records in `docs/guides/utuber.md`: func tasks, progress, retention, the rerun route,
`progress_interval_ms`, headless owned-lanes mode (P16), the config-lane vs owned-lane rule
(P19), func-metric aggregation (P17), the fact-6 corrections, and a **"Schema v5: no
downgrade"** warning with the K9 consequence and recovery. It also removes that document's
"Known limitation" section, since P15 fixes it, and adds D5's cookie workflow. The FRD status
line becomes *"implemented; see `docs/plans/PLAN-utuber-taskmaster-lane.md`"*.

Then the full gate:

```sh
make check
```

The lane plan's §7.3 E1–E3 browser runs are **owner sign-off (item A12)** — list them as
pending, do not self-certify them.

## ADR

**Decision:** one phase vocabulary, D0–D9, in a fixed order with an explicit ownership
boundary. D0–D4 and D7 are UI fixes; D8 absorbs the lane plan's step 5, D9 its step 6, and
D5 is deliberately sequenced *after* D8 because D8 rewrites all five files D5 would otherwise
edit. Two items that appeared in both plans — the `media/exec.go` scanner goroutines and the
`/settings.json` contract — are assigned to one owner each, so neither is implemented twice and
neither is silently discarded.

**Ordering is a correctness constraint, not a preference.** Running D5 before D8 means writing
cookie fields that D8's fixed response shape does not contain, and writing a scanner-goroutine
join that D8 deletes. Running D8 before D5 is the only order where both survive.

**This supersedes the earlier "cross-cutting first" sequencing rule**, which was a layering
preference and did not account for two plans editing the same files.
