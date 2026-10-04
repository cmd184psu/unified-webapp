# unified-webapp User Guide

One Go binary serves eight modules. The `Host` header of each request picks
the module: `host_routing` in the config maps a hostname (port ignored) to a
module name, so `todo.test:8080` and `todo.test` both route to whatever
`"todo.test"` maps to. Each module is independently either open (no login)
or protected (LDAP, an optional PIN door code, passkey, and API keys all
work against a protected module) — see
[Logging in: the auth model](#logging-in-the-auth-model).

Modules:

| Module | What it is |
|---|---|
| [grocery](#grocery) | Shared real-time grocery list with a recipe system |
| [todo](#todo) | Multi-subject collaborative to-do lists with votes and recurrence |
| [slideshow](#slideshow) | Synced fullscreen photo slideshow / kiosk with music |
| [menuserver](#menuserver) | Read-only bookmark/notes/credentials pages |
| [obsidianoid](#obsidianoid) | Web viewer/editor for Obsidian-style markdown vaults |
| [multissh](#multissh) | Browser SSH console for many hosts + file broadcast |
| [taskmaster](#taskmaster) | Scheduled/on-demand command runner with live output |
| [admin](#admin) | Web UI for editing the auth config live |
| [smbedit](../docs/guides/smbedit.md) | Edit Samba shares (shares, globals, smb.conf preview, save & restart) |

---

## Local testing quick start

A ready-made profile lives in `local-test/`. It routes every module to a
`.test` hostname and demonstrates every auth style at once. (`.test` is
reserved for exactly this — RFC 6761. Don't use `.local`: that domain
belongs to Bonjour/mDNS on macOS, and every page load stalls ~5 seconds
waiting on multicast lookups.)

**1. Add the hostnames** (two lines in `/etc/hosts`, needs sudo):

```
127.0.0.1 grocery.test todo.test slideshow.test menu.test menuserver.test obsidianoid.test multissh.test admin.test taskmaster.test smbedit.test
::1 grocery.test todo.test slideshow.test menu.test menuserver.test obsidianoid.test multissh.test admin.test taskmaster.test smbedit.test
```

Add both so IPv4 and IPv6 lookups resolve straight from `/etc/hosts`.

**2. Seed data dirs and the admin PIN file** (idempotent):

```
bash local-test/setup.sh
```

**3. Start the server** (from the repo root — module paths in the config are
relative to the working directory):

```
go run ./cmd/server -config local-test/config.json
```

**4. Browse.** Every module is at `http://<name>.test:8080`:

| URL | Module | Protected? | Credential |
|---|---|---|---|
| http://grocery.test:8080 | grocery | open | — |
| http://todo.test:8080 | todo | yes | PIN `111111`, or LDAP |
| http://slideshow.test:8080 | slideshow | yes | PIN `222222`, or LDAP |
| http://menuserver.test:8080 (or menu.test) | menuserver | yes | LDAP (or API key) |
| http://obsidianoid.test:8080 | obsidianoid | yes | LDAP (or API key) |
| http://multissh.test:8080 | multissh | yes | LDAP (or API key) |
| http://taskmaster.test:8080 | taskmaster | yes | LDAP (or API key) |
| http://admin.test:8080 | admin | yes | admin PIN `424242` only |
| http://smbedit.test:8080 | smbedit | open | — (no auth wired in yet) |

These are throwaway test credentials, published in this repo on purpose.
Never reuse them outside local testing. LDAP credentials for all protected
modules other than admin: `chris` / `ldap-test-1` (see below).

The test API key (works on any protected non-admin module — todo,
slideshow, menuserver, obsidianoid, multissh, taskmaster):

```
varOO_vuQyged_rklN3ujsy2tgQAcEs-9Ln13hDIyh0
```

API keys are for scripted access, not browsers — send them as a header:

```
curl -H "Authorization: Bearer varOO_vuQyged_rklN3ujsy2tgQAcEs-9Ln13hDIyh0" \
     http://menuserver.test:8080/items
```

### Local LDAP with glauth

The profile points LDAP at `ldap://127.0.0.1:3893`, which matches the
included [glauth](https://github.com/glauth/glauth) test config (single
static binary, or docker):

```
glauth -c local-test/glauth.cfg
```

That serves user `chris` (password `ldap-test-1`, member of `household`,
which the profile requires) and a read-only bind account. LDAP is offered on
every protected module in this profile — including menuserver, which used
to be API-key-only — so glauth needs to be running to log into todo,
slideshow, menuserver, obsidianoid, multissh, or taskmaster via the browser
(todo and slideshow also accept their PIN instead). Verify it answers
before blaming the webapp:

```
ldapsearch -H ldap://127.0.0.1:3893 -x \
  -D "cn=serviceuser,dc=glauth,dc=com" -w svc-secret-1 \
  -b dc=glauth,dc=com "(cn=chris)"
```

**If glauth isn't running, LDAP logins fail with the same generic "login
failed" a wrong password gets** (deliberately — the login page never reveals
infrastructure detail). The server log tells them apart: a directory that
can't be reached logs `event=auth_ldap_error ... connection refused`, while
a wrong password logs `reason="bad_credential"`.

To use a real LDAP server instead, edit the `auth.ldap` block in
`local-test/config.json`. Note `required_groups` entries are matched against
group **cn values** (e.g. `"household"`), not full DNs — the server resolves
groups by searching `base_dn` for entries whose `member`/`memberUid`/
`uniqueMember` points at the user and collecting their `cn`s.

### Things to know about this profile

- **Plain HTTP**, so `cookie_secure` is `false` here. The example/production
  config keeps it `true` — don't copy this profile's auth block to a real
  deployment.
- **Sessions don't carry across hostnames.** `cookie_domain` is empty, so
  each `<module>.test` gets its own host-only session cookie; logging into
  todo.test doesn't log you into slideshow.test. (In production, siblings
  under one parent domain plus `cookie_domain: ".example.net"` give you the
  accumulating multi-module session.)
- **Passkeys are deliberately absent.** WebAuthn requires a secure context,
  and `http://anything.test` is not one (only `localhost` gets that
  exemption). Testing passkeys needs TLS or a `localhost` route.
- **PINs live in plaintext files** — `local-test/admin.pin` (admin),
  `local-test/todo.pin` (todo), `local-test/slideshow.pin` (slideshow) —
  each of which must be `chmod 0400` (the server refuses more-open modes on
  every read). Edit a file to change its PIN; it takes effect on the next
  login attempt, no restart.
- Runtime state (data dirs, the PIN files, the auth session key) is
  gitignored; the profile itself (`config.json`, `setup.sh`, `glauth.cfg`)
  is tracked.

---

## Logging in: the auth model

Auth is configured centrally (`auth` section) and enforced server-side per
module by a gate in front of the module's routes. It's a **two-state**
model: `auth.modules` maps a module name to its protection — a module *not*
listed there is **open** (no login at all); a module listed there — even as
a bare `{}` — is **protected**, and every protected module accepts the same
set of methods, gated only by what's configured:

- **LDAP** — always offered on a protected non-admin module (it's the
  identity backbone every protected module relies on, which is why
  `auth.ldap.url` is required as soon as any non-admin module is
  protected). Username + password verified by bind against your directory,
  with optional required-group membership.
- **A PIN door code** — offered only when the module's entry sets
  `pin_file`, e.g. `"todo": { "pin_file": "./todo.pin" }`. This is a single
  shared numeric PIN read from a plaintext file that must be `chmod 0400`;
  it doesn't carry an identity the way LDAP does — it's just a door code
  for that one module. Edit the file to change the PIN; no restart needed.
- **Passkey** — WebAuthn, offered when `auth.passkey.rp_id` (the domain)
  is set, in Admin > Passkey settings. Every host in `host_routing` that sits
  under that domain is allowed automatically, so a new module needs nothing
  more; `rp_origins` only lists extra addresses that are not routed. Requires
  a secure context (HTTPS, or `localhost`). Register keys from a module page
  once logged in by another method. A passkey is tied to the domain, so one
  registered on any allowed host works on all of them, and each module still
  keeps its own session. If sign-in says "Passkeys are not enabled for this
  address", the address you opened is not routed under the domain.
- **API key** — orthogonal to the modules matrix. Each key works on the
  modules you chose for it, or on **all modules**; a key is never unscoped.
  It is ignored on open modules (nothing to authenticate into), never accepted
  for admin, and gets a 401 on any module outside its list. Sent as
  `Authorization: Bearer <key>` (no cookies involved); keys are stored as
  SHA-256 hashes. In Admin > API Keys each key shows its modules, **Scope…**
  changes them without rotating the key, and the create form needs a name and
  at least one module (or All modules) before **Generate key** turns on. From
  the command line, `go run ./cmd/server -gen-api-key -name <name> -modules
  todo,grocery` (or `-modules all`); `-modules` is required with `-name`. In
  the config, `auth.api_keys` entries carry `"modules": [...]` (`"*"` = all);
  an older entry with no `modules` is treated as all.

The **admin** module is the one exception to all of this: it is **PIN-only**
— never LDAP, passkey, or API key — using the operator PIN configured
through exactly one of `auth.admin_pin` (bcrypt hash in config),
`auth.admin_pin_file`, or `auth.modules.admin.pin_file` (the last is just
the module matrix's normal `pin_file` field, reused for admin's own entry).

Sessions are cookies (`uw_session`), TTL and sliding refresh configurable
under `auth.session`. Every module answers `GET /api/auth/mode` with its
accepted methods, and login/logout are `POST /api/auth/login` /
`POST /api/auth/logout` on the module's own hostname. Wrong PINs and
passwords are throttled server-side.

Each module signs you out on its own idle time, even when modules share a
PIN file or a cookie domain. Set it per module with
`auth.modules.<module>.idle_minutes` (or the "Idle sign-out" column in
admin's Module Access); unset means 60 minutes. Only real use counts:
clicks, typing, touches, scrolling, page loads and saves. A page's
background polling and live updates keep working but don't keep you signed
in, so an untouched tab idles out, and an open page drops to the login screen
when its time is up. `auth.session.ttl_hours` is the maximum session length:
sign in again after that long however active (default 720 hours = 30 days).
Signing out never stops server-side work: taskmaster tasks and utuber
downloads keep running.

A PIN login is tied to the PIN it used, so changing a PIN file signs out
every session made with the old PIN on the modules that use that file. Every
protected module shows a sign-out icon beside ☰; it signs out of that module
only (signing out of an LDAP or passkey login ends the whole session, since
it covers every module). Sessions issued before this change must sign in
once more.

---

## Grocery

A shared, real-time grocery list. Every open browser stays in sync (SSE),
so two phones in two aisles see each other's checkmarks within a second.

**Using it:**

- Items live in **groups** (aisles). Tap an item to cycle its state:
  needed → check → not needed. A progress bar up top shows
  completed/needed/check/not-needed; tap a segment for percentages.
- Header buttons: hide not-needed items (eye), collapse/expand all groups,
  reset the list (with confirmation), manage groups (add/remove/reorder —
  items from a removed group land in "Unallocated"), and edit the list
  title. Groups can also be drag-reordered.
- Add items from the footer form, picking the group.
- **Recipes tab**: each recipe card owns a set of ingredient items. Toggle
  a recipe on and all its ingredients flip to "needed" in one tap; toggle
  off to clear them. Add/remove ingredients per recipe, reorder recipes,
  delete a recipe (removes its owned items too).
- Works offline: with sync off or the network gone, changes queue locally
  and replay when the connection returns.

**Storage:** one JSON file (`grocery.data_file`), written atomically.

## Todo

Multi-subject to-do lists. A **subject** is a folder; each list in it is a
JSON file of entries with votes, optional recurrence, and
blocked/completed states. Everything syncs live per subject (SSE).

**Using it:**

- Pick a subject and a list from the two selectors. "Copy Link" / "Open in
  New Tab" deep-link the current list; "Move List to New Subject" relocates
  the file.
- Each entry can be voted up (vote button), marked completed or blocked,
  and hidden via the Hide Completed / Hide Blocked toggles. A segmented
  progress bar shows completed / in-progress / blocked / todo.
- **Periodic items:** give an item a period in days when adding it and the
  server computes its next-due timestamp — good for recurring chores. A
  cooldown (default 10 minutes, configurable in settings) paces re-votes.
- Drag-and-drop reordering and a read-only mode are toggleable; column
  visibility (Votes, Period, NextDue, Cooldown) is configurable and saved.
- Refresh / Save / Add buttons do what they say; "Go Back" reverts unsaved
  changes.

**Storage:** `{data_dir}/{subject}/{list}.json` per list, plus per-subject
`columns.json`/`settings.json`.

## Slideshow

A fullscreen ambient slideshow for a wall display or TV. The server — not
the browser — runs the show: a "conductor" advances slides on a timer and
broadcasts state over SSE, so **every connected screen shows the same image
at the same moment**. Optional background music.

**Using it:**

- Subjects are subdirectories of `slideshow.image_dir` (jpg/jpeg/png/gif;
  a leading `_` hides a directory). Music collections are subdirectories of
  the audio dir (mp3/flac/ogg/m4a/wav/aac); no audio dir, no music controls.
- Controls are two translucent cards over the image: **images**
  (previous/next image, previous/next subject, play/pause) and **audio**
  (stop/play/next collection; only when there's music). Each card has a grip
  to drag it anywhere, and a minimize button that shrinks it to its grip and
  icon. In ☰ you snap each card to one of 8 positions around the edge.
- Click the image to skip to the next one.
- Settings (☰): display mode (**Ken Burns** / Pan & Scan / Static), seconds
  per image (1–300), shuffle, card positions, theme, and **Image age limit
  (days)**: images whose file is older than that are skipped (0 = no limit;
  the image set is rechecked hourly).
- Keyboard: `←`/`→` previous/next, `Space` play/pause, `Enter` music
  play/pause, `Esc` closes settings.
- There is no upload UI — populate the image directory server-side (rsync,
  scp, a mount, …).

Any control change from any client applies to all of them — it's one
show, many screens.

## Menuserver

A read-only "menu" of personal reference pages: links, notes, and saved
credentials, organized by subject into a dropdown top navigation. Think of
it as a self-hosted start page for your home infrastructure.

**Using it:**

- Each subject (subdirectory of `menuserver.data_dir`) holds JSON page
  files. A page has an `id`, `title`, optional `notes` and `gdoc` link, and
  a `sites` list; each site row shows a link (`label` + `url` + optional
  `port`) and, if present, a username with a hidden password —
  Hide/Show and Copy buttons per row.
- `show_all_pages: true` renders every page on one scrollable screen with
  anchor navigation; `false` shows one page at a time via the dropdown.
- There is no editing UI — edit the JSON files on disk. The server never
  writes.

Because pages can hold passwords, the local profile protects this module
(LDAP, or the API key for scripted access); in production put it behind
whatever auth you trust. **Don't rely on the hidden-password toggle for
security** — the password is in the page's HTML; the auth gate is the
protection.

## Obsidianoid

A lightweight web front-end for one or more Obsidian-style markdown vaults:
browse the file tree, edit notes, preview rendered markdown, and keep a
handful of always-there scratch "threads."

**Using it:**

- **Notes view:** vault selector (multi-vault) and a resizable sidebar with
  the file tree. Sort it by **Name** or **Recent** (last modified). The
  search box is a filter: it searches note *contents* (grep) and shows only
  the notes that match. Open a note, toggle edit/preview, save manually or
  let the debounced autosave do it. New notes via the + button
  (`folder/My Note.md` paths allowed).
- **Organizing:** drag a note by its grip onto a folder to move it (onto
  another note: into that note's folder; onto empty space: the vault root).
  Notes and folders have rename and delete buttons (delete asks first), and
  there's a new-folder button. The tree works from the keyboard too: arrows
  move and open/close folders, Enter opens a note. Only empty folders can be
  deleted. If the target name already exists, the move or rename is refused
  unless **Allow overwrites** (☰) is on, and then it asks first. **Lock
  file tree** (☰) turns off moving, renaming and deleting (editing notes
  still works).
- **Threads view:** a grid of scratch notes (`Threads/Thread01.md`, …; the
  count is set in ☰, 1–24, default 4) you can enable/disable independently.
  Each thread can have a title; untitled ones show the file name. Titles and
  enable flags live outside the vault, so the markdown files stay clean and
  keep their names. The threads folder itself can't be renamed. Moving a
  thread file out of the folder (to keep it) frees its slot, which starts
  again as a fresh empty thread; with overwrites allowed, you can move a
  file back into a slot.
- **Live refresh:** the server watches each vault with fsnotify; edit a
  file in the real Obsidian (or any editor) and open browsers refresh.
- **Git sync:** if the vault is a git repo, a sync button appears —
  commit-message dialog, then `git add -A && git commit && git push`.
- **Settings (☰):** theme (per vault), sort, thread count, lock file tree,
  allow overwrites.

**Storage:** the vault directories themselves (notes are plain `.md`
files); thread titles, enable flags and the thread count in
`{data_dir}/state.json`.

## Multissh

A browser-based operations console: up to `max_sessions` SSH terminals to
different hosts side by side, a broadcast bar that sends one command to all
of them, and a file-broadcast workflow that uploads a file once and pushes
it to many hosts. See `docs/guides/multissh.md` for the full operator guide.

**Using it:**

- **Host rail** (left): one card per session slot — host, user, port, and
  auth by SSH key (picked from `~/.ssh` via the key browser) or password.
  Passwords are never saved server-side; key-auth hosts also get an SFTP
  remote-directory browser. Config auto-saves as you type (except
  passwords). "Copy ssh command" gives you the equivalent CLI.
- **SSH Console tab:** per-slot Connect/Disconnect/Interrupt and a live
  terminal (WebSocket). The master bar broadcasts a typed command — or
  `key:ctrl+c` — to every connected terminal at once.
- **Upload to Host tab:** stage a file (drag-drop, file picker, or pick a
  server-resident file), then check target hosts and hit Broadcast;
  per-host progress rows stream live.
- Encrypted (passphrase-protected) private keys are not supported.
  `strict_host_key: true` enforces `known_hosts` checking. Connections,
  disconnections, broadcasts, and rejected WebSocket upgrades are audit-
  logged server-side (never with credential values).

In the local profile multissh is protected like any other module: LDAP for
browser login, or the API key for scripted access.

## Taskmaster

A command runner: define tasks — each a single shell command — that live
in ordered, width-limited **lanes**; run them on demand or let them repeat
after a cooldown rest; watch a live lane board and each task's output over
SSE. See `docs/guides/taskmaster.md` for the full config/API/scheduling reference
and a security write-up you should read before enabling sudo.

**Using it:**

- **Lanes** replace the old "groups." A lane is an ordered playlist of
  tasks with a **width**: width 1 runs tasks one at a time in order; width
  N runs up to N concurrently, pulled off the top in position order. There
  is no per-task priority — order within a lane is drag-reorder (or
  `PUT /api/lanes/{name}/order`).
- **Tasks** belong to one lane and run a single **command** through a
  shell — there is no task "type" selector and no JSON args blob. A task
  can be `repeat`-scheduled with a `cooldown_seconds` (a minimum rest
  between runs, not a cron-style cadence), or triggered on demand via
  "Up next," which puts it at the front of its lane's queue.
- **Live output:** triggering a task (or opening a running one) streams its
  stdout/stderr line-by-line over SSE as it happens; the executions list
  shows status (pending/running/success/failed/canceled) and duration; the
  metrics page aggregates success/failed/canceled counts and durations per
  task. The lane board itself updates live over a separate board-events SSE
  stream rather than polling or full-page refresh.
- **Cancel and the hand brake:** a running execution can be force-killed
  individually (recorded as `canceled`, distinct from `failed`), or the
  global **hand brake** can pause every lane and kill every running
  execution in one action — it latches until explicitly released, and
  persists across a restart. A `sudo`-run task's child process is
  root-owned and may survive either kind of cancel; see
  `docs/guides/taskmaster.md`.
- **Sudo control:** a sudo toggle on task creation appears only when the
  server's `taskmaster.allow_sudo` is `true` (checked via
  `GET /api/capabilities`); when hidden, tasks can never be created with
  `sudo: true` from the UI, and the API rejects `sudo: true` with a 403 if
  `allow_sudo` is false regardless. `allow_sudo` is runtime-togglable
  (`POST /api/capabilities`) and DB-authoritative once seeded from config.
- **`taskmasterctl`** is a companion CLI for the same API (lane/task/
  executions/output/cancel/brake/metrics/health), authenticating with a
  platform API key — see `docs/guides/taskmaster.md`.

In the local profile taskmaster is protected like multissh: LDAP for
browser login, or the API key for scripted access.

## Admin

A web UI for operating the auth system without touching the server: toggle
which modules are protected, set or clear each module's `pin_file` door
code, manage API keys, and apply the result **live** — enforcement changes
on the very next request, no restart.

**Using it:**

- Log in with the operator PIN. This is separate from the modules matrix: a
  routed admin module always requires the admin PIN, configured as exactly
  one of `auth.admin_pin` (bcrypt hash in the config), `auth.admin_pin_file`,
  or `auth.modules.admin.pin_file` (plaintext PIN in a `0400` file, re-read
  every login so edits apply immediately) — exactly one of the three. The
  server refuses to boot with admin routed and none configured.
- Toggle each module protected/open and set its `pin_file`, add/remove API
  keys. New API keys are shown **once** at creation; only their hashes are
  stored. Sensitive values are redacted in every view.
- Apply validates the new policy first (same rules as boot) and rejects
  anything inconsistent — e.g. an unknown module name, or a protected
  non-admin module when `auth.ldap.url` isn't set. On success the config
  file is rewritten surgically (only the auth section; your comments-free
  JSON formatting elsewhere is preserved) via an atomic temp-file + rename,
  and the running policy is swapped in memory. The result is exactly what a
  restart would load.
- Wrong-PIN attempts get 401s and are throttled.

**Don't rely on front-end behavior for security** — all enforcement
(gates, validation, redaction, throttling) is server-side; the UI is just a
convenience over the admin API.

---

## Timetracker

A PS/customer helper: a customer list with per-customer editable fields and
deep links (Slack, CMS, Jira), a 15-minute-block time selector, a markdown
report composer with clipboard copy, and CSV export. CSV import and
`/create-customer` exist as API endpoints (curl/tooling); the UI has no
import button. One shared dataset, last write wins.

The customer list is kept sorted case-insensitively by name — in memory, on
disk, in `GET /data`, and in mutation responses — so the row indexes the
update/delete API uses always address the customer the client saw. Adding a
customer requires a name (blank names are rejected with a 400).

**Reports persist per customer per day** in a SQLite database. Selecting a
customer shows today's report; edits to the report text or the time
selector auto-save after a short pause (and on customer switch, date
change, and page close). The ◀/▶ arrows beside the date picker rewind
through the dates that actually have a stored report for that customer —
the date picker, markdown preview, and time selector all swing together —
and the Today button jumps back to the current day. Picking any date in
the date picker loads that day's report. Clearing a report (empty text, no
time blocks) removes its stored row. Renaming a customer migrates their
report history to the new name; deleting a customer keeps the history
(reachable again by re-adding the same name).

**Config keys:**

- `timetracker.static_dir` — built frontend (default `./web/timetracker`)
- `timetracker.data_file` — the single JSON data file (default
  `./data/timetracker.json`); created empty-but-valid on first boot
- `timetracker.report_db` — the SQLite report database (default:
  `timetracker-reports.db` next to `data_file`); created on first boot

**Migrating from the standalone app:** copy the old app's `public/data.json`
to the configured `data_file`. The legacy `sfdcUrl` and `cumulusBucket`
keys are mapped to `cmsUrl` and `supportBucket` on load; other removed
legacy per-customer fields (`insightUrl` and the remote-access field) are
ignored on load. The file is rewritten under the current keys on the first
save. CSV import likewise accepts the legacy 8- and 9-column exports in
addition to the current 7-column format.

**Auth:** the module ships open. Customer names, Slack IDs, and Jira
numbers are mildly sensitive, so production configs SHOULD add an
`auth.modules.timetracker` entry — the login gate is inherited from the
dispatcher with no module-code change.

---

## Server CLI helpers

```
go run ./cmd/server -config <file>     # run with a config
go run ./cmd/server -init-config       # write a starter config
go run ./cmd/server -hash-pin          # prompt for a PIN, print its bcrypt hash
go run ./cmd/server -gen-api-key       # print a fresh API key + its sha256 hash
```

`-hash-pin` also accepts a piped value (`echo 1234 | go run ./cmd/server
-hash-pin`) for scripting.
