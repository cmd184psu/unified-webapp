# baseline-shots

Re-runnable screenshot baseline harness for the UI unification work.
Self-contained on purpose: its `package.json` keeps Playwright out of the
repo root's deliberately small devDependency footprint.

## Run

```sh
cd tools/baseline-shots
npm install                      # once
npx playwright install chromium  # once, needs internet
node shoot.js [label]            # label defaults to "pre-phase1"
```

Output: `baselines/<label>/<module>/<variant>.png` (full-page, 1440x900
viewport, light color-scheme emulation). Run it again at a commit boundary
with a new label (e.g. `node shoot.js post-c6`) and diff the two trees.

## How it works

- Derives a config from `local-test/config.json`: port 18080, every
  already-protected module gets a locally generated PIN file (`.pins/`,
  PIN 142536), so login is scriptable without the glauth LDAP server.
  Unprotected modules stay unprotected. Written to `.derived-config.json`.
- Starts `../../unified-webapp` (run `make build` first) with repo root as
  cwd, waits for it, kills it when done.
- Launches Chromium with
  `--host-resolver-rules=MAP *.test 127.0.0.1, MAP *.cmdhome.net 127.0.0.1`
  so the real host_routing hostnames resolve without /etc/hosts or DNS.
- Logs in via a same-origin `fetch("/api/auth/login", {method:"pin"})`
  executed inside the page. (Not `context.request` — that runs in Node and
  bypasses the resolver rules, which would leak `*.cmdhome.net` lookups to
  real DNS.)
- Theme variants: todo light/dark via `todo-theme` localStorage; obsidianoid
  currently just `dark` (being renamed `obsidian` — after the rename, update
  `VARIANTS.obsidianoid` and compare against the old `dark` shot). Also one
  shot of the shared login gate (`_login-gate/`).

## Prereqs / seed data

`bash local-test/setup.sh` seeds the data the screenshots show (slideshow
PNGs, obsidianoid vault, menuserver links).

## Interaction scenes

`interactions.js` holds per-module scripted scenes (drawer open, modal up,
nav dropdown hovered) that run after each module's plain variant shots, in
the same authenticated context. Each scene is `{name, ls?, fullPage?, run}`;
shots land next to the variants as `<module>/<name>.png`. Scenes default to
viewport (not full-page) capture, since drawers/modals are fixed overlays.
A failing scene is reported and sets exit code 1 but doesn't abort the run.
Current coverage: every live menu/drawer implementation (certmachine Tools,
menuserver dropdown+panel, obsidianoid theme panel, slideshow settings,
smbedit drawer, taskmaster menu + confirm modal, todo sidebar, utuber
settings) — selectors documented in docs/INVENTORY-hamburger-menus.md.

## Known gaps

- Screenshots are at one viewport; no pixel-diff step yet
  (pixelmatch/odiff could be added on top for a mechanical zero-regression
  gate).
