# Post-Phase 5 UI Unification Punch List

Date: 2026-09-22
Branch: ui-upgrade
Source: manual testing session against local-test config

## Status Summary

| Rating | Modules |
|--------|---------|
| BROKEN (5) | menuserver, timetracker, admin, multissh, smbedit |
| Major issues (4) | utuber, taskmaster, obsidianoid, todo |
| Minor issues (3) | certmachine, slideshow, sampler |
| Good (1) | issuetracker |

## Cross-Cutting Issues

### CC-1: HamburgerMenu drawer/trigger side mismatch
The shared HamburgerMenu component always slides from the left regardless of where the trigger button is placed. If the trigger is on the right, the drawer must slide from the right. Either auto-detect trigger position or accept a `side` option in the constructor. Both sides valid but must match.

### CC-2: Text entry characters disappearing
Characters like `#` (obsidianoid) and `&` (taskmaster) disappear while typing in editor/input fields, reappear on Enter. Likely a shared CSS leak causing live markdown/HTML rendering during input.

### CC-3: Duplicate hamburger menus
smbedit, utuber, and slideshow each have both an old custom menu AND the new shared HamburgerMenu. Must consolidate to one per module, migrating module-specific settings into the shared component.

### CC-4: Login page redesign
PIN and LDAP login are in the same space, which is confusing. Needs better visual separation and less boring design.

## Per-Module Issues

### menuserver — BROKEN
- M-1: Gets raw 401 JSON instead of login page (other modules show login form correctly)
- M-2: UI completely broken vs production (lab.cmdhome.net) — no visible hamburger, layout trashed

### timetracker — BROKEN
- T-1: No CSS at all — completely unstyled, untestable

### admin — BROKEN
- A-1: Copy-paste of newly generated API token doesn't work
- A-2: Passkey registration doesn't work
- A-3: Operator PIN should be special/separate — shouldn't default to same pin as modules
- A-4: Operator PIN shows "Not configured" with confusing explanatory text
- A-5: Cookie domain field has no tooltip or context
- A-6: Session and module access settings should auto-save per action, not batch-save
- A-7: Theming off / buttons look intentionally plain — styling incomplete

### multissh — BROKEN
- MS-1: No hamburger menu at all
- MS-2: Terminal card hides the current line — can't see what you're typing
- MS-3: Host cards on the left should be collapsible
- MS-4: Horizontal scrollbar — layout should fit without scrolling

### smbedit — BROKEN
- SB-1: Two hamburger menus — consolidate into one settings menu (see CC-3)
- SB-2: No theme selection — add as dropdown in merged menu
- SB-3: Hamburger menu missing X close button and title
- SB-4: Clicking Preview tab should immediately render — "Render Preview" button is superfluous
- SB-5: SMB log file path is wrong
- SB-6: No way to restart SMB — regression from original

### utuber — Major
- U-1: Two hamburger menus, both wrong (see CC-3)
- U-2: Trigger should be on the left, integrated into existing header, not centered above it
- U-3: Consolidated menu should include: python interpreter field, update yt-dlp option, theme selection
- U-4: Mechanically works

### taskmaster — Major
- TM-1: Copy button rigid/flat — no feedback that copy worked. Needs toast
- TM-2: `&` characters disappear while typing (see CC-2)
- TM-3: Pausing a lane should allow canceling the running task
- TM-4: Handbrake engaged should NOT show "again in ..." countdown — hide cooldown timers
- TM-5: Hamburger menu on wrong side (see CC-1)
- TM-6: Theme picker should be a dropdown, not buttons
- TM-7: "Metrics" in hamburger menu makes no sense — should be visible content, not buried in nav

### obsidianoid — Major
- O-1: Characters disappear while typing in markdown editor — `#` etc. (see CC-2)
- O-2: Hamburger menu slides from wrong side (see CC-1)
- O-3: Themes work correctly

### todo — Major
- TD-1: Blocker icon wrong (`todo-utils.js:260`) — renders as pause (circle + two bars) instead of ban/prohibition (circle + diagonal line)
- TD-2: Only 2 themes in picker (`shell.ts:164`) — hardcoded to dark/light, should be all 8
- TD-3: Hamburger menu mechanically OK, buttons work

### certmachine — Minor
- CM-1: Hamburger icon in separate nav header — should be visually integrated into app header (see CC-3 pattern)
- CM-2: Mechanically works, themes work

### slideshow — Minor
- SL-1: No theme selection dropdown in settings panel
- SL-2: Likely running old custom hamburger with shared one hidden — reconcile (see CC-3)

### sampler — Minor
- SA-1: "colour" should be "color" — US English spelling

### issuetracker — Good
- No issues found. API token creation UI could be future work.

## Config Fixes Already Applied
- `local-test/config.json`: added `shared_static_dir`, normalized hostnames to `<name>.test`, added utuber route + config, all modules on pin auth
- `local-test/test.pin`: created with value `1234`, permissions `0400`
