// theme.test.ts — Phase-2 C3 coverage (docs/PLAN-ui-unification-phase2.md §5
// Step 3.4): the resolution order as three cases plus a construction assertion
// (B3.1 as amended in v5 FINAL), the unknown-stored-name fall-through (B3.2),
// the storageKey() override (B3.3), the live matchMedia `change` listener
// (B3.4), themes.list's roster (B3.5), reresolve()'s two halves (B3.10), and
// the barrel's export shape (BX.2).
//
// No jsdom, no @types/node (ADR-005). The three globals ThemeManager touches
// come from ./test-dom's installFakeDom(), which the harness cannot supply and
// modal.test.ts's five-member stub does not overlap.

import { THEMES, ThemeManager } from "./theme";
import type { ThemeManagerOptions } from "./theme";
import { installFakeDom } from "./test-dom";
import * as barrel from "./index";

function check(name: string, cond: boolean, detail: string): void {
  if (!cond) throw new Error(`${name}: ${detail}`);
  console.log(`ok - ${name}`);
}

const names: readonly string[] = THEMES;

// A `default` OUTSIDE THEMES. Step 4 is unreachable, so every case below can
// use a sentinel floor: if resolution ever reached it, the stamp would be a
// string no theme block declares and the failure would be unmistakable.
const FLOOR = "floor-never-reached";

// A value no resolution step can produce, stamped by hand to observe whether
// the change listener writes the attribute AT ALL.
const UNTOUCHED = "untouched-by-the-listener";

// --- B3.1 case 1 — step 1: a validated stored name wins -----------------------

{
  const dom = installFakeDom();
  dom.storage.set("ui-theme:sampler", "forest");
  const themes = new ThemeManager({ module: "sampler", default: FLOOR, serverDefault: () => "ocean" });
  themes.apply();
  check(
    "B3.1 step 1: a validated stored name wins over a live serverDefault()",
    dom.documentElement.dataset.theme === "forest",
    `got "${dom.documentElement.dataset.theme}"`,
  );
}

// --- B3.1 case 2 — step 2: storage empty, serverDefault() answers -------------

{
  const dom = installFakeDom();
  const themes = new ThemeManager({ module: "sampler", default: FLOOR, serverDefault: () => "ocean" });
  themes.apply();
  check(
    "B3.1 step 2: with storage empty, serverDefault() answers",
    dom.documentElement.dataset.theme === "ocean",
    `got "${dom.documentElement.dataset.theme}"`,
  );
}

// --- B3.1 case 3 — step 3: both matchMedia polarities -------------------------

{
  const dom = installFakeDom();
  const themes = new ThemeManager({ module: "sampler", default: FLOOR, serverDefault: () => undefined });

  dom.media.matches = true;
  themes.apply();
  check(
    "B3.1 step 3: storage empty and serverDefault() silent, matches:true resolves dark",
    dom.documentElement.dataset.theme === "dark",
    `got "${dom.documentElement.dataset.theme}"`,
  );

  dom.media.matches = false;
  themes.apply();
  check(
    "B3.1 step 3: matches:false — no-preference included — resolves light",
    dom.documentElement.dataset.theme === "light",
    `got "${dom.documentElement.dataset.theme}"`,
  );
}

// --- B3.1's construction assertion for step 4 --------------------------------
//
// There is no fourth runtime case and there cannot be one: the system step
// resolves under EVERY matchMedia outcome, so step 4 is unreachable at runtime
// (§5 Step 3.1, §18 Critic finding 1). `default` is asserted where it actually
// lives — in ThemeManagerOptions and in the construction — and the resolver is
// asserted never to reach it. A case that tried to fall past the system step
// could only do so by deleting matchMedia from the stub, which would assert the
// behaviour of a browser this repo does not serve.

{
  const dom = installFakeDom();
  const options: ThemeManagerOptions = { module: "floor", default: FLOOR, serverDefault: () => undefined };

  check(
    "B3.1 step 4: the construction carries a `default` — step 4 is typed config, present by construction",
    typeof options.default === "string" && options.default === FLOOR,
    `got ${JSON.stringify(options.default)}`,
  );
  check(
    "B3.1 step 4: this construction's floor is not a THEMES member, so reaching step 4 would be visible",
    !names.includes(options.default),
    `"${options.default}" is in THEMES, which would make the next assertion vacuous`,
  );

  const themes = new ThemeManager(options);
  for (const matches of [true, false]) {
    dom.media.matches = matches;
    themes.apply();
    check(
      `B3.1 step 4 is unreachable: with storage empty and serverDefault() silent (matches:${matches}) the stamp is the system polarity, never \`default\``,
      dom.documentElement.dataset.theme === (matches ? "dark" : "light"),
      `got "${dom.documentElement.dataset.theme}"`,
    );
  }
}

// --- B3.2 — an unknown stored name falls through, never gets stamped ---------

{
  const dom = installFakeDom();
  // `system` is the instructive unknown: it is the resolver's implicit step and
  // must not become selectable through a hand-written storage entry (§10 row 22).
  dom.storage.set("ui-theme:sampler", "system");
  const themes = new ThemeManager({ module: "sampler", default: FLOOR, serverDefault: () => "ocean" });
  themes.apply();
  check(
    'B3.2: an unknown stored name ("system") falls through to step 2',
    dom.documentElement.dataset.theme === "ocean",
    `got "${dom.documentElement.dataset.theme}"`,
  );
  check(
    "B3.2: the unknown stored name is not stamped",
    dom.documentElement.dataset.theme !== "system",
    "an unvalidated stored string reached the DOM",
  );
}

{
  const dom = installFakeDom();
  dom.storage.set("ui-theme:sampler", "chartreuse");
  const themes = new ThemeManager({ module: "sampler", default: FLOOR });
  dom.media.matches = true;
  themes.apply();
  check(
    "B3.2: with no serverDefault, an unknown stored name falls all the way through to the system step",
    dom.documentElement.dataset.theme === "dark",
    `got "${dom.documentElement.dataset.theme}"`,
  );
  check(
    "B3.2: the unknown name is left in storage untouched — reading validates, it does not clean up",
    dom.storage.get("ui-theme:sampler") === "chartreuse",
    `got "${dom.storage.get("ui-theme:sampler")}"`,
  );
}

// --- B3.3 — storageKey() overrides ui-theme:<module> -------------------------
//
// Proved with obsidianoid's per-vault template, which is the only client of the
// override in the phase.

{
  const dom = installFakeDom();
  const vault = 0;
  dom.storage.set("obsidianoid-theme-0", "ember");
  dom.storage.set("ui-theme:obsidianoid", "rose"); // the default key must be ignored
  const themes = new ThemeManager({
    module: "obsidianoid",
    default: FLOOR,
    storageKey: () => `obsidianoid-theme-${vault}`,
  });

  themes.apply();
  check(
    "B3.3: storageKey() overrides ui-theme:<module> on read",
    dom.documentElement.dataset.theme === "ember",
    `got "${dom.documentElement.dataset.theme}"`,
  );

  themes.set("puma");
  check(
    "B3.3: set() writes the overridden key",
    dom.storage.get("obsidianoid-theme-0") === "puma",
    `got "${dom.storage.get("obsidianoid-theme-0")}"`,
  );
  check(
    "B3.3: set() leaves ui-theme:<module> untouched",
    dom.storage.get("ui-theme:obsidianoid") === "rose",
    `got "${dom.storage.get("ui-theme:obsidianoid")}"`,
  );
}

{
  const dom = installFakeDom();
  const themes = new ThemeManager({ module: "sampler", default: FLOOR });
  themes.set("ocean");
  check(
    "B3.3: without an override the key is exactly ui-theme:<module>",
    dom.storage.get("ui-theme:sampler") === "ocean",
    `got keys ${JSON.stringify([...dom.storage.keys()])}`,
  );
  check(
    "B3.3: and it is the only key written",
    dom.storage.size === 1,
    `got keys ${JSON.stringify([...dom.storage.keys()])}`,
  );
}

// --- B3.4 — `system` is the implicit resolution step, and its `change`
// listener is live ------------------------------------------------------------

{
  const dom = installFakeDom();
  const themes = new ThemeManager({ module: "sampler", default: FLOOR, serverDefault: () => undefined });
  themes.apply();
  check(
    "B3.4: while resolution reaches the system step, the initial stamp is the OS polarity",
    dom.documentElement.dataset.theme === "light",
    `got "${dom.documentElement.dataset.theme}"`,
  );

  dom.media.fireChange(true);
  check(
    "B3.4: a matchMedia change event re-applies while the resolution is still reaching the system step",
    dom.documentElement.dataset.theme === "dark",
    `got "${dom.documentElement.dataset.theme}"`,
  );

  dom.media.fireChange(false);
  check(
    "B3.4: and it follows the OS back the other way",
    dom.documentElement.dataset.theme === "light",
    `got "${dom.documentElement.dataset.theme}"`,
  );
}

{
  const dom = installFakeDom();
  dom.storage.set("ui-theme:sampler", "forest");
  const themes = new ThemeManager({ module: "sampler", default: FLOOR, serverDefault: () => undefined });
  themes.apply();
  dom.documentElement.dataset.theme = UNTOUCHED;
  dom.media.fireChange(true);
  check(
    "B3.4: the change listener is ignored once storage answers — it does not write the attribute at all",
    dom.documentElement.dataset.theme === UNTOUCHED,
    `got "${dom.documentElement.dataset.theme}"`,
  );
}

{
  const dom = installFakeDom();
  const themes = new ThemeManager({ module: "sampler", default: FLOOR, serverDefault: () => "ocean" });
  themes.apply();
  dom.documentElement.dataset.theme = UNTOUCHED;
  dom.media.fireChange(true);
  check(
    "B3.4: the change listener is ignored once serverDefault() answers",
    dom.documentElement.dataset.theme === UNTOUCHED,
    `got "${dom.documentElement.dataset.theme}"`,
  );
}

// --- B3.5 — themes.list has 8 entries and equals THEMES' name set ------------

{
  installFakeDom();
  const themes = new ThemeManager({ module: "sampler", default: FLOOR });
  check("B3.5: themes.list has 8 entries", themes.list.length === 8, `got ${themes.list.length}`);
  check(
    "B3.5: themes.list equals THEMES' name set, both directions",
    themes.list.every((n) => names.includes(n)) && names.every((n) => themes.list.includes(n)),
    `list ${JSON.stringify(themes.list)} vs THEMES ${JSON.stringify(names)}`,
  );
  check(
    "B3.5: `system` is not in themes.list — it is the implicit step, not a choice",
    !themes.list.includes("system"),
    "`system` leaked into the picker roster",
  );
}

// --- onChange fires on set() and on nothing else (§5 Step 3.4) ---------------

{
  const dom = installFakeDom();
  const fired: string[] = [];
  const themes = new ThemeManager({ module: "sampler", default: FLOOR, onChange: (name) => fired.push(name) });

  themes.apply();
  check("onChange does not fire on the initial apply()", fired.length === 0, `got ${JSON.stringify(fired)}`);
  themes.reresolve();
  check("onChange does not fire on reresolve() either", fired.length === 0, `got ${JSON.stringify(fired)}`);

  themes.set("rose");
  check(
    "onChange fires on set(), once, with the chosen name",
    fired.length === 1 && fired[0] === "rose",
    `got ${JSON.stringify(fired)}`,
  );
  check("set() applies the chosen theme", dom.documentElement.dataset.theme === "rose", `got "${dom.documentElement.dataset.theme}"`);
}

// --- apply() is idempotent and writes no storage -----------------------------

{
  const dom = installFakeDom();
  dom.storage.set("ui-theme:sampler", "forest");
  const themes = new ThemeManager({ module: "sampler", default: FLOOR });
  themes.apply();
  themes.apply();
  themes.apply();
  check(
    "apply() is idempotent — three calls leave the same stamp",
    dom.documentElement.dataset.theme === "forest",
    `got "${dom.documentElement.dataset.theme}"`,
  );
  check(
    "apply() writes no storage",
    dom.storage.size === 1 && dom.storage.get("ui-theme:sampler") === "forest",
    `got ${JSON.stringify([...dom.storage.entries()])}`,
  );
}

// --- B3.10 — reresolve() exists, is public, follows a changed storageKey(),
// and does not write storage --------------------------------------------------

{
  const dom = installFakeDom();
  let vault = 0;
  dom.storage.set("obsidianoid-theme-0", "ember");
  dom.storage.set("obsidianoid-theme-1", "puma");
  const themes = new ThemeManager({
    module: "obsidianoid",
    default: FLOOR,
    storageKey: () => `obsidianoid-theme-${vault}`,
  });

  check("B3.10: reresolve() is public on the instance", typeof themes.reresolve === "function", `got ${typeof themes.reresolve}`);

  themes.apply();
  check(
    "B3.10: the first resolution follows the closure's current key",
    dom.documentElement.dataset.theme === "ember",
    `got "${dom.documentElement.dataset.theme}"`,
  );

  vault = 1;
  themes.reresolve();
  check(
    "B3.10: reresolve() re-reads a changed storageKey() and stamps the NEW key's stored value",
    dom.documentElement.dataset.theme === "puma",
    `got "${dom.documentElement.dataset.theme}"`,
  );
  check(
    "B3.10: reresolve() does not write storage — the destination vault's stored choice is unchanged",
    dom.storage.get("obsidianoid-theme-1") === "puma",
    `got "${dom.storage.get("obsidianoid-theme-1")}"`,
  );
  check(
    "B3.10: reresolve() did not copy the source vault's choice across either",
    dom.storage.get("obsidianoid-theme-0") === "ember",
    `got "${dom.storage.get("obsidianoid-theme-0")}"`,
  );
  check("B3.10: reresolve() added no key", dom.storage.size === 2, `got keys ${JSON.stringify([...dom.storage.keys()])}`);
}

// --- BX.2 — the barrel's export shape ---------------------------------------

check("barrel exposes ThemeManager", typeof barrel.ThemeManager === "function", `got ${typeof barrel.ThemeManager}`);
check("barrel's ThemeManager is this module's class", barrel.ThemeManager === ThemeManager, "the barrel re-exports a different binding");
check("barrel still exposes THEMES and setTheme beside it (ADR-008)", barrel.THEMES === THEMES && typeof barrel.setTheme === "function", "the primitive was displaced");
