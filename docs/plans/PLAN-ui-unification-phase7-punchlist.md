# Phase 7: Punch List Fixes

Status: `Approval`
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


CMD> The above table may be inaccurate; please double check it


Per-module commits (C4-C12) MUST include the `side` wiring from this table.



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

### C10: Utuber consolidation (U-1 through U-3)
**Files:** `web/utuber/`

CMD> 

- U-1/U-2: Remove old hamburger, use shared HamburgerMenu with trigger integrated into existing header on the left
- U-3: Move python interpreter field and yt-dlp update into the shared hamburger menu + theme picker

**Acceptance criteria:**
- Single hamburger menu with trigger in existing header (left side)
- Menu contains: python interpreter, yt-dlp update, theme picker
- Old custom hamburger menu removed
- Build succeeds

### C11: Slideshow theme picker + menu reconciliation (SL-1, SL-2)
**Files:** `web/slideshow/`

Per ADR-020, slideshow keeps its settings panel. Add theme picker dropdown to the existing settings panel. Remove any hidden/unused shared HamburgerMenu if mounted.

**Acceptance criteria:**
- Theme picker with all 8 themes in slideshow settings panel
- No duplicate hidden hamburger menu
- Settings panel continues to work as before
- Build succeeds

### C12: Admin fixes (A-1 through A-7)
**Files:** `web/admin/`

- A-1: Fix token copy to clipboard
- A-2: Investigate passkey registration failure
- A-3/A-4: Clarify operator PIN UI — improve explanatory text
- A-5: Add tooltip/help text for cookie domain field
- A-6: Auto-save settings per action
- A-7: Improve button/theme styling

**Acceptance criteria:**
- API token copy works (toast confirms)
- Passkey registration functional (or clear error if server-side limitation)
- Operator PIN section has clear, non-confusing text
- Cookie domain has tooltip explaining purpose
- Settings save on each individual change
- Buttons and theme styling consistent with other modules
- Build succeeds

### C13: Taskmaster fixes (TM-1 through TM-7)
**Files:** `web/taskmaster/`

- TM-1: Add toast on copy
- TM-3: Allow cancel of running task when lane is paused
- TM-4: Hide "again in ..." countdown during handbrake
- TM-6: Use `themePicker: true` on the HamburgerMenu (shared button-style picker via `ThemeManager.renderPicker()`). The user asked for a dropdown, but `renderPicker` produces swatch buttons which are the established pattern across all other modules. Use the existing button-style picker for consistency; a dropdown variant would be a separate enhancement.
- TM-7: Move metrics out of hamburger menu into main content area

**Acceptance criteria:**
- Copy button shows toast feedback
- Can cancel running task when lane is paused
- No countdown timers visible during handbrake
- Theme picker functional in hamburger menu
- Metrics visible in main content, not hamburger menu
- Build succeeds

### C14: Login page redesign (CC-4) — NON-REGRESSION ENHANCEMENT
**Files:** `web/shared/` or auth module templates

**Scope gate:** This is a UX enhancement, not a regression fix. It is intentionally last in the sequence and may be deferred to a follow-up phase if the regression fixes (C0-C12) consume the available scope. The user explicitly requested it in the punch list.

Redesign the login page to visually separate PIN and LDAP login. Make it look less generic/boring. PIN should be the primary/prominent method with LDAP as a secondary option (e.g., collapsible section or separate tab).

**Acceptance criteria:**
- PIN and LDAP login are visually separated (not in same form space)
- PIN is the primary/prominent login method
- Page looks polished, not generic
- Works across all themes
- Responsive at mobile widths

## ADR

**Decision:** Fix all punch list items in a cross-cutting-first commit sequence.

**Drivers:** 5 BROKEN modules must be restored; shared component fixes (hamburger side, text entry bug) cascade to many modules; user explicitly flagged these as regressions.

**Alternatives considered:** Module-by-module commits — rejected because shared component changes would scatter across commits.

**Why chosen:** Cross-cutting fixes first means each subsequent per-module commit inherits the shared improvements. Clear dependency ordering reduces risk.

**Consequences:** ~15 commits (C0-C14) on ui-upgrade. Shared component changes (C0, C1) must be carefully tested since they affect all modules. Login page redesign (C14) is the most subjective item and may need iteration.

**Follow-ups:** Module-level API token management (issuetracker) noted but explicitly out of scope.
