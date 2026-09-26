# Phase 6: Post-Phase 5 Punch List Fixes

Status: `pending approval`
Branch: `ui-upgrade`
Source: `docs/PUNCH-LIST-post-phase5.md`

## RALPLAN-DR Summary

### Principles
1. **Fix regressions before features** — restore broken functionality first, then improve UX
2. **Cross-cutting before per-module** — fix shared components so per-module fixes inherit them
3. **No scope creep** — fix what's on the punch list, nothing more
4. **Preserve existing behavior** — the fix for a bug must not break something else
5. **One hamburger per module** — consolidate duplicate menus, don't add new ones

### Decision Drivers
1. 5 modules rated BROKEN need to become functional — this is the top priority
2. Shared HamburgerMenu side-mismatch affects every module — fixing it once fixes many items
3. Character-disappearing bug (CC-2) affects obsidianoid + taskmaster and is user-visible

### Viable Options

**Option A: Layered commits by scope (cross-cutting → per-module)** — Fix shared component issues first (CC-1 side agreement, CC-2 text entry), then sweep per-module fixes grouped by severity. ~8-10 commits.
- Pros: Cross-cutting fixes cascade to modules; clear dependency ordering; easier to review
- Cons: More commits; later modules wait for shared fixes

**Option B: Module-by-module** — Fix each module completely in its own commit, including any shared component changes.
- Pros: Each commit is self-contained
- Cons: Shared fixes get duplicated or scattered; hard to review shared changes; risk of conflicts between module commits

**Chosen: Option A** — cross-cutting first, then per-module. Option B rejected because shared component changes (HamburgerMenu side option, CSS fixes) would be scattered across commits with conflict risk.

## Commit Sequence

### C0: HamburgerMenu side option + close button + title (CC-1, CC-3 prep)
**Files:** `web/shared/ts/menu.ts`, `web/shared/css/components.css`, `web/shared/ts/menu.test.ts`

Add `side?: "left" | "right"` option to `HamburgerMenuOptions`. Default `"left"`. When `"right"`:
- CSS: drawer anchors `right: 0` instead of `left: 0`, `border-left` instead of `border-right`, `translateX(100%)` for closed state
- Add `data-side="right"` on `.ui-menu-drawer` to select CSS variant

Add visible close button (X) at top of drawer — currently only closes via backdrop click or Escape.

Add title rendering in drawer header (already has `aria-label` but no visible title text).

Auto-detect is **opt-in only** via `side: "auto"`. When `"auto"`, detection is deferred to the first `open()` call (not the constructor) where layout is guaranteed settled — `getBoundingClientRect()` on the trigger determines left/right based on viewport midpoint. If no `side` is set, the default remains `"left"` for backward compatibility. This avoids timing-fragile constructor-time layout queries.

**Acceptance criteria:**
- `side: "right"` produces a right-anchored drawer with correct animation direction
- Auto-detection works when trigger is on the right side of the viewport
- Close button (X) visible at top of drawer, closes on click
- Title text visible in drawer header
- Existing left-side behavior unchanged when `side` is omitted and trigger is on the left
- All existing menu.test.ts tests pass + new tests for side option
- Auto-detect locks after first detection (caches result, does not re-query on subsequent opens)

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

Per-module commits (C4-C12) MUST include the `side` wiring from this table.

### C1: Fix text-entry character disappearing bug (CC-2)
**Files:** `web/shared/css/components.css` or per-module CSS — investigation needed

This is a **spike-then-fix** commit. Investigation first, then fix.

Investigate the CSS leak causing `#` (obsidianoid) and `&` (taskmaster) to visually disappear during text input. Likely candidates:
- A CSS rule styling content that matches typed characters (e.g., a rule targeting `#` as a heading marker)
- An overly broad selector from shared CSS bleeding into contenteditable/textarea/input contexts

**Decision point:** If root cause is in shared CSS (`web/shared/css/`), the fix stays here in C1 (before per-module commits). If root cause is per-module (e.g., obsidianoid's own markdown rendering or taskmaster's own CSS), move the fix into the respective module commit (C12 for taskmaster, and add an obsidianoid fix commit). Update commit ordering accordingly.

**Acceptance criteria:**
- Root cause identified with evidence
- Typing `#` in obsidianoid editor does not cause character to disappear
- Typing `&` in taskmaster text input does not cause character to disappear
- Characters are visible continuously during typing, not just after Enter
- No regression in other text inputs across modules

### C2: Todo quick fixes (TD-1 ban icon, TD-2 theme picker)
**Files:** `web/todo/js/todo-utils.js`, `web/todo/js/shell.ts`, `web/todo/js/shell.js` (rebuild)

- TD-1: Replace pause SVG at line 260 with correct ban/prohibition icon (circle with diagonal line)
- TD-2: Replace hardcoded `["dark", "light"]` theme picker in `shell.ts:164` with `themePicker: true` on the HamburgerMenu constructor (like other modules use), removing the custom `render()` theme block entirely

**Acceptance criteria:**
- Blocker icon renders as circle with diagonal line (ban symbol), not pause
- All 8 themes appear in todo's hamburger menu theme picker
- Theme selection works and persists across reloads
- Build + typecheck green

### C3: Sampler spelling fix (SA-1)
**Files:** `web/sampler/` — find and fix "colour" → "color"

**Acceptance criteria:**
- Zero occurrences of "colour" in web/sampler/
- US English spelling used throughout

### C4: Obsidianoid fixes (O-1, O-2)
**Files:** `web/obsidianoid/js/app.ts`, `web/obsidianoid/js/app.js` (rebuild)

- O-2: Pass `side: "right"` to HamburgerMenu constructor (trigger is on the right)
- O-1: If C1 spike determines CC-2 is per-module, fix the `#` character disappearing bug here. If C1 fixed it in shared CSS, verify the fix works in obsidianoid.

**Acceptance criteria:**
- Hamburger drawer slides from the right (matching trigger position)
- Typing `#` in the markdown editor does not cause characters to disappear (verified post-C1)
- Build + typecheck green

### C5: Menuserver auth + UI fix (M-1, M-2)
*Previously C4.*
**Files:** Investigate auth routing for menuserver, compare with working modules. Fix menuserver UI to match production (lab.cmdhome.net) layout.

- M-1: Determine why menuserver gets raw 401 JSON while other modules get the login page. Fix auth routing.
- M-2: Compare current menuserver UI against production and fix layout/styling issues.

**Acceptance criteria:**
- `menuserver.test:8080` shows the login page (not raw 401 JSON)
- After login, hamburger menu visible and functional
- Page navigation works through hamburger menu items
- Layout comparable to production at lab.cmdhome.net

### C6: Timetracker CSS restoration (T-1)
**Files:** `web/timetracker/css/app.css`, `web/timetracker/index.html`

Timetracker has no styling at all. Restore/create tokenized CSS using the shared design token system (`var(--color-*)`, `var(--space-*)`, etc.).

**Acceptance criteria:**
- Timetracker renders with proper styling (backgrounds, colors, spacing, typography)
- Theme picker works through hamburger menu with all 8 themes
- Timer start/stop UI is functional and styled
- Build succeeds

### C7: Certmachine header integration + side fix (CM-1)
**Files:** `web/certmachine/index.html`, `web/certmachine/css/app.css`

Move hamburger trigger into the certmachine app header instead of a separate nav bar above it.

**Acceptance criteria:**
- Hamburger icon is part of the certmachine header row, not a separate bar
- Drawer opens from the right (explicit `side: "right"` per C0 wiring table)
- No visual regression in cert list or other certmachine UI

### C8: Multissh fixes (MS-1 through MS-4)
**Files:** `web/multissh/`

- MS-1: Add hamburger menu (with theme picker)
- MS-2: Fix terminal card hiding current input line
- MS-3: Make host cards collapsible
- MS-4: Remove horizontal scrollbar — fix layout to fit without scrolling

**Acceptance criteria:**
- Hamburger menu present with theme picker (all 8 themes)
- Terminal shows current input line
- Host cards can be collapsed/expanded
- No horizontal scrollbar at normal viewport widths
- Build succeeds

### C9: Smbedit consolidation (SB-1 through SB-6)
**Files:** `web/smbedit/`

- SB-1/SB-2: Consolidate two hamburger menus into one shared HamburgerMenu with theme dropdown
- SB-3: Close button + title already handled by C0
- SB-4: Preview tab auto-renders — remove separate "Render Preview" button
- SB-5: Fix SMB log file path
- SB-6: Add SMB restart button

**Acceptance criteria:**
- Single hamburger menu with theme picker, close button, title
- Clicking Preview tab immediately renders preview
- SMB log file path is correct
- SMB restart button present and functional
- Build succeeds

### C10: Utuber consolidation (U-1 through U-3)
**Files:** `web/utuber/`

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
