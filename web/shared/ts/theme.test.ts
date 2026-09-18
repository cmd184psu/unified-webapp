// ThemeManager unit tests: resolution order (storage → server → system →
// default), unknown-name fallback, storageKey override, live matchMedia
// listener, theme roster, reresolve(), and barrel export shape.
//
// No jsdom, no @types/node. The three globals ThemeManager touches
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

// A `default` OUTSIDE THEMES. The system step always resolves, so every case below can
// use a sentinel floor: if resolution ever reached it, the stamp would be a
// string no theme block declares and the failure would be unmistakable.
const FLOOR = "floor-never-reached";

// A value no resolution step can produce, stamped by hand to observe whether
// the change listener writes the attribute AT ALL.
const UNTOUCHED = "untouched-by-the-listener";

// --- resolution step 1: a validated stored name wins -------------------------

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

// --- resolution step 2: storage empty, serverDefault() answers ---------------

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

// --- resolution step 3: both matchMedia polarities ---------------------------

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

// --- construction assertion for the default floor ----------------------------
//
// The system step resolves under every matchMedia outcome, so the `default`
// floor is unreachable at runtime. It is asserted where it actually lives —
// in ThemeManagerOptions and in the construction — and the resolver is
// asserted never to reach it.

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

// --- an unknown stored name falls through, never gets stamped ---------------

{
  const dom = installFakeDom();
  // `system` is the instructive unknown: it is the resolver's implicit step and
  // must not become selectable through a hand-written storage entry.
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

// --- storageKey() overrides ui-theme:<module> --------------------------------

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

// --- the system resolution step, and its live change listener ---------------

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

// --- themes.list has 8 entries and equals THEMES' name set ------------------

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

// --- onChange fires on set() and on nothing else ----------------------------

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

// --- reresolve() follows a changed storageKey() without writing storage -----

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

// --- C5b — the active mark follows the applied theme, from every path --------
//
// Regression for the defect C5's browser leg found: renderPicker() marked the
// swatch the resolution reached at RENDER time and nothing moved it again, so
// on obsidianoid — whose drawer is built eagerly, before the vault roster
// arrives and therefore before serverDefault() can answer — the mark read
// `light` while the root carried `obsidian`, and it stayed on the source
// vault's theme across a vault switch. A mark on the wrong swatch is worse
// than no mark, and C4's sampler leg could not catch it: no serverDefault and
// no vault switch there.
//
// installFakeDom() is reused exactly as landed at C3. It deliberately supplies
// no element tree, so these blocks layer a minimal element stub over the
// `document` it installs — menu.test.ts's idiom — carrying the same
// documentElement object through, so setTheme() still writes what is read back.

class FakeElement {
  tagName: string;
  className = "";
  type = "";
  textContent = "";
  dataset: Record<string, string> = {};
  children: FakeElement[] = [];
  listeners: Array<() => void> = [];

  constructor(tagName: string) {
    this.tagName = tagName;
  }

  /** A string argument is a TEXT node — renderPicker passes the label as one. */
  append(...nodes: Array<FakeElement | string>): void {
    for (const node of nodes) {
      if (typeof node === "string") {
        this.textContent += node;
        continue;
      }
      this.children.push(node);
    }
  }

  addEventListener(type: string, fn: () => void): void {
    if (type === "click") this.listeners.push(fn);
  }

  click(): void {
    for (const fn of [...this.listeners]) fn();
  }
}

/** installFakeDom() plus the element factory it deliberately does not supply. */
function installPickerDom(): ReturnType<typeof installFakeDom> {
  const dom = installFakeDom();
  Object.defineProperty(globalThis, "document", {
    value: {
      documentElement: dom.documentElement,
      createElement: (tag: string): FakeElement => new FakeElement(tag),
    },
    writable: true,
    configurable: true,
  });
  return dom;
}

/** Renders one picker into a fresh host and returns its swatch buttons. */
function renderInto(themes: ThemeManager): FakeElement[] {
  const host = new FakeElement("div");
  themes.renderPicker(host as unknown as HTMLElement);
  return host.children[0].children;
}

/** The names of the swatches carrying .is-active — one, or the mark is broken. */
function marked(buttons: FakeElement[]): string[] {
  return buttons.filter((b) => b.className.split(" ").includes("is-active")).map((b) => b.textContent);
}

{
  const dom = installPickerDom();
  let vault = 0;
  const roster: Array<{ theme: string } | undefined> = [];
  const themes = new ThemeManager({
    module: "obsidianoid",
    default: "obsidian",
    storageKey: () => `obsidianoid-theme-${vault}`,
    serverDefault: () => roster[vault]?.theme,
  });

  const buttons = renderInto(themes);
  check(
    "C5b: the render-time mark is the resolution in force at render time — here the system step, the roster being absent",
    marked(buttons).join(",") === "light",
    `got ${JSON.stringify(marked(buttons))}`,
  );
  check(
    "C5b: renderPicker() marks only — it stamps no attribute, so a picker may be built before its module applies anything",
    !("theme" in dom.documentElement.dataset),
    `got "${dom.documentElement.dataset.theme}"`,
  );

  // Case (a) as observed in the browser: the roster lands, serverDefault()
  // becomes answerable, fetchVaults() reresolve()s. Before this commit the
  // root read "obsidian" while the mark still read "light".
  roster[0] = { theme: "obsidian" };
  roster[1] = { theme: "forest" };
  themes.reresolve();
  check(
    "C5b case (a): reresolve() moves the mark onto a newly answerable serverDefault(), so the mark matches the root",
    dom.documentElement.dataset.theme === "obsidian" && marked(buttons).join(",") === "obsidian",
    `root "${dom.documentElement.dataset.theme}" vs marked ${JSON.stringify(marked(buttons))}`,
  );
  check(
    "C5b case (a): exactly one swatch is marked",
    marked(buttons).length === 1,
    `got ${JSON.stringify(marked(buttons))}`,
  );

  // Case (b): a set() made AFTER render — the path that used to work only
  // because the click handler moved the mark itself.
  themes.set("rose");
  check(
    "C5b case (b): a set() made after render moves the mark to the chosen theme",
    dom.documentElement.dataset.theme === "rose" && marked(buttons).join(",") === "rose",
    `root "${dom.documentElement.dataset.theme}" vs marked ${JSON.stringify(marked(buttons))}`,
  );

  // Case (c): switchVault() re-runs both closures. The mark must leave the
  // source vault's stored choice, which is exactly what it used not to do.
  vault = 1;
  themes.reresolve();
  check(
    "C5b case (c): a vault switch moves the mark off the source vault's stored choice and onto the destination's resolution",
    dom.documentElement.dataset.theme === "forest" && marked(buttons).join(",") === "forest",
    `root "${dom.documentElement.dataset.theme}" vs marked ${JSON.stringify(marked(buttons))}`,
  );
  check(
    "C5b case (c): still exactly one swatch marked — no second mark left behind",
    marked(buttons).length === 1,
    `got ${JSON.stringify(marked(buttons))}`,
  );
}

{
  const dom = installPickerDom();
  const themes = new ThemeManager({ module: "sampler", default: FLOOR, serverDefault: () => undefined });
  themes.apply();
  const buttons = renderInto(themes);

  dom.media.fireChange(true);
  check(
    "C5b: a matchMedia change event moves the mark while the resolution is still reaching the system step",
    dom.documentElement.dataset.theme === "dark" && marked(buttons).join(",") === "dark",
    `root "${dom.documentElement.dataset.theme}" vs marked ${JSON.stringify(marked(buttons))}`,
  );

  dom.media.fireChange(false);
  check(
    "C5b: and the mark follows the OS back the other way",
    dom.documentElement.dataset.theme === "light" && marked(buttons).join(",") === "light",
    `root "${dom.documentElement.dataset.theme}" vs marked ${JSON.stringify(marked(buttons))}`,
  );

  // Once storage answers, the listener is ignored — and so the mark must not
  // move either, since the applied theme did not.
  themes.set("ember");
  dom.media.fireChange(true);
  check(
    "C5b: an ignored change event moves no mark, because it applies nothing",
    dom.documentElement.dataset.theme === "ember" && marked(buttons).join(",") === "ember",
    `root "${dom.documentElement.dataset.theme}" vs marked ${JSON.stringify(marked(buttons))}`,
  );
}

{
  const dom = installPickerDom();
  const themes = new ThemeManager({ module: "sampler", default: FLOOR });
  // One instance, two rendered pickers — the sampler's page section and its
  // drawer, three views of one state.
  const section = renderInto(themes);
  const drawer = renderInto(themes);

  section[themes.list.indexOf("ember")].click();
  check(
    "C5b: choosing a swatch in one picker re-marks the other rendered picker too",
    marked(section).join(",") === "ember" && marked(drawer).join(",") === "ember",
    `section ${JSON.stringify(marked(section))} vs drawer ${JSON.stringify(marked(drawer))}`,
  );

  drawer[themes.list.indexOf("ocean")].click();
  check(
    "C5b: and it works in the other direction, with one mark per picker",
    marked(section).join(",") === "ocean" &&
      marked(drawer).join(",") === "ocean" &&
      marked(section).length === 1 &&
      marked(drawer).length === 1,
    `section ${JSON.stringify(marked(section))} vs drawer ${JSON.stringify(marked(drawer))}`,
  );

  dom.storage.set("ui-theme:sampler", "puma");
  themes.reresolve();
  check(
    "C5b: an outside-the-picker reresolve() re-marks every rendered picker",
    dom.documentElement.dataset.theme === "puma" &&
      marked(section).join(",") === "puma" &&
      marked(drawer).join(",") === "puma",
    `root "${dom.documentElement.dataset.theme}", section ${JSON.stringify(marked(section))}, drawer ${JSON.stringify(marked(drawer))}`,
  );
}

// --- barrel export shape ----------------------------------------------------

check("barrel exposes ThemeManager", typeof barrel.ThemeManager === "function", `got ${typeof barrel.ThemeManager}`);
check("barrel's ThemeManager is this module's class", barrel.ThemeManager === ThemeManager, "the barrel re-exports a different binding");
check("barrel still exposes THEMES and setTheme beside it", barrel.THEMES === THEMES && typeof barrel.setTheme === "function", "the primitive was displaced");
