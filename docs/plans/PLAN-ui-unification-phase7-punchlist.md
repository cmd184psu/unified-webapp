# Phase 7: Punch List Fixes

Status: `Approved`
Branch: `ui-upgrade`
Source: `docs/plans/PLAN-ui-unification-phase7-punchlist.md`

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
2. Shared HamburgerMenu side-mismatch affects every module — fixing it once fixes many items
3. login screen theme mismatched; login screen should take on current theme per module


### Viable Options

**Chosen Methodology: Layered commits by scope (cross-cutting → per-module)** — Fix shared component issues first (CC-1 side agreement), then sweep per-module fixes grouped by severity.
- Pros: Cross-cutting fixes cascade to modules; clear dependency ordering; easier to review
- Cons: More commits; later modules wait for shared fixes

## Commit Sequence

### C0: HamburgerMenu side option + close button + title (CC-1, CC-3 prep)
**Files:** `web/shared/ts/menu.ts`, `web/shared/css/components.css`, `web/shared/ts/menu.test.ts`

Add `side?: "left" | "right"` option to `HamburgerMenuOptions`. Default `"left"`. When `"right"`:
- CSS: drawer anchors `right: 0` instead of `left: 0`, `border-left` instead of `border-right`, `translateX(100%)` for closed state
- Add `data-side="right"` on `.ui-menu-drawer` to select CSS variant

Add visible close button (X) at top of drawer — currently only closes via backdrop click or Escape.

~~~Auto-detect is **opt-in only** via `side: "auto"`. When `"auto"`, detection is deferred to the first `open()` call (not the constructor) where layout is guaranteed settled — `getBoundingClientRect()` on the trigger determines left/right based on viewport midpoint. If no `side` is set, the default remains `"left"` for backward compatibility. This avoids timing-fragile constructor-time layout queries.~~~

CMD> This feels more complex than it should be.. and that's why it's still broken.  some modules still have the hamburger trigger on the opposite side of the screen.  Let's make it more deterministic:  Set the hamburger side in configuration, per module in admin menu and the per module json.  So each module gets to chose the location instead of expensive "auto detection".  This guidance is approved and trumps all reviewers.

**Acceptance criteria:**
- `side: "right"` produces a right-anchored drawer with correct animation direction
- Auto-detection works when trigger is on the right side of the viewport
- Close button (X) visible at top of drawer, closes on click
- Title text visible in drawer header
- Existing left-side behavior unchanged when `side` is omitted and trigger is on the left
- All existing menu.test.ts tests pass + new tests for side option
- ~~~Auto-detect locks after first detection (caches result, does not re-query on subsequent opens)~~~ 
- CMD>position is decided on configuration; default is left side. NO AUTODETECTION

**Side-wiring table — modules that need `side` set after C0:**

| Module | Trigger position | Action |
|--------|-----------------|--------|
| todo | left (mountTrigger) | no change needed (left is default) |
| obsidianoid | right | pass `side: "right"` in app.ts HamburgerMenu constructor |
| grocery | right | pass `side: "right"` in app.ts |
| certmachine | right | pass `side: "right"` in app.ts |
| taskmaster | right | pass `side: "right"` in app.ts |
| menuserver | left (auto-created) | no change needed |
| multissh | C7 adds hamburger — use `side: "left"` |
| smbedit | C8 consolidates — set side per trigger position |
| utuber | C9 consolidates — `side: "left"` per U-2 |
| slideshow | ADR-020: no HamburgerMenu |
| admin | left | no change needed |
| issuetracker | left | no change needed |
| timetracker | C5 restores — set side per trigger position |


CMD> The above table may be inaccurate; please double check it.  I'm looking for consistency, but some pages may look better with left instead of right, so it should be deterministic in the configuration.  Tear down the autodetection for this - it does not work.


### C5: Menuserver auth + UI fix (M-1, M-2)

CMD> this section needs to be rewritten, but the only issue with menuserver is that the hamburger trigger is floating on the header, it should be anchored far right, not just hanging out to the right of the last menu.

### C7: Certmachine

CMD> no action items for this module; certmachine is tested, verified and complete.


### C8: Multissh fixes (MS-1 through MS-2)
**Files:** `web/multissh/`

- MS-1: Move hamburger menu to right side (via configuration and shared code)
- MS-2: Move logout button to the right, just left of the hamburger

CMD> Once the hamburger placement bug is fixed in shared code, this should be simple enough to do in config.

### C9: Smbedit 
**Files:** `web/smbedit/`

CMD> the only issue is a strange page scrolling issue.  It is possible to scroll too far down and you wind up below the footer

### C10: Utuber
**Files:** `web/utuber/`

CMD> Downloads appear to be broken

### C13: Taskmaster 
**Files:** `web/taskmaster/`

CMD> see taskmaster/utuber lane plan - already approved!

### C14: Login page redesign fix
**Files:** `web/shared/` or auth module templates

CMD> login page looks great, except that it does not keep the theme for the module.  I suspect that we're using a global theme for all login pages -- if so, that's incorrect.  Test: log in: change theme, log out.  Attempt to log in again, the log in should reflect the theme in use for that module.  Try to log in to another module and it should take on the theme for that module.


## ADR

**Decision:** Fix all punch list items in a cross-cutting-first commit sequence.
