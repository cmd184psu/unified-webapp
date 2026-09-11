# unified-webapp User Guide

One Go binary serves seven modules. The `Host` header of each request picks
the module: `host_routing` in the config maps a hostname (port ignored) to a
module name, so `todo.local:8080` and `todo.local` both route to whatever
`"todo.local"` maps to. Each module can independently require login (PIN,
LDAP, passkey, or API key) — or require nothing at all.

Modules:

| Module | What it is |
|---|---|
| [grocery](#grocery) | Shared real-time grocery list with a recipe system |
| [todo](#todo) | Multi-subject collaborative to-do lists with votes and recurrence |
| [slideshow](#slideshow) | Synced fullscreen photo slideshow / kiosk with music |
| [menuserver](#menuserver) | Read-only bookmark/notes/credentials pages |
| [obsidianoid](#obsidianoid) | Web viewer/editor for Obsidian-style markdown vaults |
| [multissh](#multissh) | Browser SSH console for many hosts + file broadcast |
| [admin](#admin) | Web UI for editing the auth config live |

---

## Local testing quick start

A ready-made profile lives in `local-test/`. It routes every module to a
`.local` hostname and demonstrates every auth style at once.

**1. Add the hostnames** (one line in `/etc/hosts`, needs sudo):

```
127.0.0.1 grocery.local todo.local slideshow.local menu.local menuserver.local obsidianoid.local multissh.local admin.local
```

**2. Seed data dirs and the admin PIN file** (idempotent):

```
bash local-test/setup.sh
```

**3. Start the server** (from the repo root — module paths in the config are
relative to the working directory):

```
go run ./cmd/server -config local-test/config.json
```

**4. Browse.** Every module is at `http://<name>.local:8080`:

| URL | Module | Auth | Credential |
|---|---|---|---|
| http://grocery.local:8080 | grocery | none | — |
| http://todo.local:8080 | todo | PIN | `1234` |
| http://slideshow.local:8080 | slideshow | PIN | `1234` |
| http://menuserver.local:8080 (or menu.local) | menuserver | API key | see below |
| http://obsidianoid.local:8080 | obsidianoid | LDAP | `chris` / `ldap-test-1` |
| http://multissh.local:8080 | multissh | LDAP **or** API key | same as above |
| http://admin.local:8080 | admin | admin PIN | `424242` |

These are throwaway test credentials, published in this repo on purpose.
Never reuse them outside local testing.

The test API key (menuserver, multissh):

```
varOO_vuQyged_rklN3ujsy2tgQAcEs-9Ln13hDIyh0
```

API keys are for scripted access, not browsers — send them as a header:

```
curl -H "Authorization: Bearer varOO_vuQyged_rklN3ujsy2tgQAcEs-9Ln13hDIyh0" \
     http://menuserver.local:8080/items
```

### Local LDAP with glauth

The profile points LDAP at `ldap://127.0.0.1:3893`, which matches the
included [glauth](https://github.com/glauth/glauth) test config (single
static binary, or docker):

```
glauth -c local-test/glauth.cfg
```

That serves user `chris` (password `ldap-test-1`, member of `household`,
which the profile requires) and a read-only bind account. Verify it answers
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
  each `<module>.local` gets its own host-only session cookie; logging into
  todo.local doesn't log you into slideshow.local. (In production, siblings
  under one parent domain plus `cookie_domain: ".example.net"` give you the
  accumulating multi-module session.)
- **Passkeys are deliberately absent.** WebAuthn requires a secure context,
  and `http://anything.local` is not one (only `localhost` gets that
  exemption). Testing passkeys needs TLS or a `localhost` route.
- **Admin PIN lives in `local-test/admin.pin`** — a plaintext PIN in a file
  that must be `chmod 0400` (the server refuses more-open modes on every
  read). Edit the file to change the PIN; it takes effect on the next login
  attempt, no restart.
- Runtime state (data dirs, the PIN file, the auth session key) is
  gitignored; the profile itself (`config.json`, `setup.sh`, `glauth.cfg`)
  is tracked.

---

## Logging in: the auth model

Auth is configured centrally (`auth` section) and enforced server-side per
module by a gate in front of the module's routes. `auth.modules` maps each
module to the list of methods it accepts:

- **`pin`** — a shared numeric PIN checked against bcrypt hashes in
  `auth.pins` (several named PINs allowed; the matching name becomes your
  identity). Generate a hash with `go run ./cmd/server -hash-pin`.
- **`ldap`** — username + password verified by bind against your directory,
  with optional required-group membership.
- **`passkey`** — WebAuthn. Requires a configured `rp_id`/`rp_origins` and a
  secure context (HTTPS, or localhost). Register keys from a module page
  once logged in by another method.
- **`key`** — API keys for scripts: `Authorization: Bearer <key>` (no
  cookies involved). Keys are stored as SHA-256 hashes in `auth.api_keys`;
  generate a pair with `go run ./cmd/server -gen-api-key`.

A module with an empty/absent method list is open — no login. The **admin**
module is special: routed admin always requires the operator PIN
(`auth.admin_pin` bcrypt hash in config, or `auth.admin_pin_file` — exactly
one of the two), independent of the matrix.

Sessions are cookies (`uw_session`), TTL and sliding refresh configurable
under `auth.session`. Every module answers `GET /api/auth/mode` with its
accepted methods, and login/logout are `POST /api/auth/login` /
`POST /api/auth/logout` on the module's own hostname. Wrong PINs and
passwords are throttled server-side.

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
- Controls bar: previous/next image, previous/next subject, play/pause,
  plus music stop/play/next-collection.
- Settings panel (hamburger): display mode (**Ken Burns** / Pan & Scan /
  Static), seconds per image (1–300), shuffle, dark/light theme, controls
  at top or bottom.
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

Because pages can hold passwords, the local profile gates this module with
an API key; in production put it behind whatever auth you trust. **Don't
rely on the hidden-password toggle for security** — the password is in the
page's HTML; the auth gate is the protection.

## Obsidianoid

A lightweight web front-end for one or more Obsidian-style markdown vaults:
browse the file tree, edit notes, preview rendered markdown, and keep a
handful of always-there scratch "threads."

**Using it:**

- **Notes view:** vault selector (multi-vault), search box filtering the
  file tree, resizable sidebar. Open a note, toggle edit/preview, save
  manually or let the debounced autosave do it. New notes via the +
  button (`folder/My Note.md` paths allowed). Per-vault theme picker.
- **Threads view:** a fixed grid (default 4 slots) of scratch notes
  (`Threads/Thread01.md`, …) you can enable/disable independently — the
  disabled flag lives outside the vault so the markdown files stay clean.
- **Live refresh:** the server watches each vault with fsnotify; edit a
  file in the real Obsidian (or any editor) and open browsers refresh.
- **Git sync:** if the vault is a git repo, a sync button appears —
  commit-message dialog, then `git add -A && git commit && git push`.

**Storage:** the vault directories themselves (notes are plain `.md`
files); thread enable-flags in `{data_dir}/state.json`.

## Multissh

A browser-based operations console: up to `max_sessions` SSH terminals to
different hosts side by side, a broadcast bar that sends one command to all
of them, and a file-broadcast workflow that uploads a file once and pushes
it to many hosts. See `docs/multissh.md` for the full operator guide.

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

In the local profile multissh accepts LDAP (browser login) **or** an API
key (scripted access) — an example of stacking methods on one module.

## Admin

A web UI for operating the auth system without touching the server: edit
which methods each module requires, manage PINs and API keys, and apply the
result **live** — enforcement changes on the very next request, no restart.

**Using it:**

- Log in with the operator PIN. This is separate from the auth matrix: a
  routed admin module always requires the admin PIN, configured as exactly
  one of `auth.admin_pin` (bcrypt hash in the config) or
  `auth.admin_pin_file` (plaintext PIN in a root-owned `0400` file, re-read
  every login so edits apply immediately). The server refuses to boot with
  admin routed and neither — or both — configured.
- Edit the per-module method matrix, add/remove named PINs, add/remove API
  keys. New API keys are shown **once** at creation; only their hashes are
  stored. Sensitive values are redacted in every view.
- Apply validates the new policy first (same rules as boot) and rejects
  anything inconsistent — e.g. a module listing `pin` with no PINs defined.
  On success the config file is rewritten surgically (only the auth
  section; your comments-free JSON formatting elsewhere is preserved) via
  an atomic temp-file + rename, and the running policy is swapped in
  memory. The result is exactly what a restart would load.
- Wrong-PIN attempts get 401s and are throttled.

**Don't rely on front-end behavior for security** — all enforcement
(gates, validation, redaction, throttling) is server-side; the UI is just a
convenience over the admin API.

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
