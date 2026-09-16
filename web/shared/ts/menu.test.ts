// menu.test.ts — Phase-2 C4 coverage (docs/PLAN-ui-unification-phase2.md §5
// Step 4.4): eager construction and verbatim slot mounting (B4.3), the three
// net-new a11y features (B4.1), per-open `when()` re-evaluation (B4.4),
// id-addressed add/remove/update with no-op-on-unknown (B4.5), the
// textContent-only path (B4.6), destroy()'s listener accounting (B4.7), the
// single trap predicate (B4.8), and the barrel's export shape (BX.2/BX.3).
//
// No jsdom, no @types/node (ADR-005). Two stubs, for two disjoint reasons:
//
//   - ./test-dom's installFakeDom() supplies the three globals ThemeManager
//     touches (documentElement.dataset, localStorage, matchMedia), because the
//     `themePicker: true` case mounts a real ThemeManager. It is reused exactly
//     as landed at C3 and is not extended.
//   - a hand-rolled element stub, in modal.test.ts's idiom, supplies the
//     element tree — installFakeDom() deliberately does not, and this suite
//     needs three things no earlier suite did: createElementNS (the trigger's
//     glyph), getElementById (B4.3's node identity is stated in exactly those
//     terms), and an add/remove listener LEDGER, because "removes every
//     listener it added" is a claim about listeners that must no longer exist,
//     and there is no other way to observe their absence.
//
// installFakeDom() runs FIRST and the element stub is layered over the
// `document` it installs, carrying the same documentElement object through, so
// ThemeManager keeps writing the object this suite reads back.
//
// There is no browser here; a non-zero exit is the whole report. The three
// checks that need a real layout engine — the drawer slide, the reduced-motion
// suppression (B4.9) and the eight distinct swatch fills (B4.2) — are CSS
// facts, checked by docs/sampler-checklist.md and check-shared-css.mjs clause
// 7, not here.

import { HamburgerMenu } from "./menu";
import type { MenuItem } from "./menu";
import { getFocusable } from "./focusable";
import { ThemeManager } from "./theme";
import { installFakeDom } from "./test-dom";
import * as barrel from "./index";

// --- the listener ledger ----------------------------------------------------

interface LedgerRow {
  target: unknown;
  type: string;
  fn: unknown;
  capture: boolean;
  live: boolean;
}

const ledger: LedgerRow[] = [];

function recordAdd(target: unknown, type: string, fn: unknown, capture: boolean): void {
  ledger.push({ target, type, fn, capture, live: true });
}

function recordRemove(target: unknown, type: string, fn: unknown, capture: boolean): void {
  const row = ledger.find(
    (r) => r.live && r.target === target && r.type === type && r.fn === fn && r.capture === capture,
  );
  if (row) row.live = false;
}

function liveCount(): number {
  return ledger.filter((r) => r.live).length;
}

// --- a hand-rolled element stub, no jsdom -----------------------------------

const FOCUSABLE_TAGS = ["button", "input", "select", "textarea"];

class FakeElement {
  tagName: string;
  className = "";
  id = "";
  type = "";
  href = "";
  disabled = false;
  tabIndex = 0;
  textContent: string | null = "";
  dataset: Record<string, string> = {};
  attrs: Record<string, string> = {};
  children: FakeElement[] = [];
  listeners: Record<string, Array<(e: unknown) => void>> = {};
  parent: FakeElement | null = null;

  constructor(tagName: string) {
    this.tagName = tagName;
  }

  /** A string argument is a TEXT node — ThemeManager.renderPicker passes one. */
  append(...nodes: Array<FakeElement | string>): void {
    for (const node of nodes) {
      if (typeof node === "string") {
        this.textContent = (this.textContent ?? "") + node;
        continue;
      }
      // Real append() MOVES a node that already has a parent, and sync()'s
      // re-ordering depends on that, so the stub must do it too.
      node.remove();
      node.parent = this;
      this.children.push(node);
    }
  }

  remove(): void {
    if (!this.parent) return;
    const i = this.parent.children.indexOf(this);
    if (i >= 0) this.parent.children.splice(i, 1);
    this.parent = null;
  }

  setAttribute(name: string, value: string): void {
    this.attrs[name] = value;
  }

  removeAttribute(name: string): void {
    delete this.attrs[name];
  }

  addEventListener(type: string, fn: (e: unknown) => void, capture = false): void {
    (this.listeners[type] ??= []).push(fn);
    recordAdd(this, type, fn, capture);
  }

  removeEventListener(type: string, fn: (e: unknown) => void, capture = false): void {
    const list = this.listeners[type] ?? [];
    const i = list.indexOf(fn);
    if (i >= 0) list.splice(i, 1);
    recordRemove(this, type, fn, capture);
  }

  focus(): void {
    fakeDocument.activeElement = this;
  }

  contains(node: FakeElement): boolean {
    return this.children.includes(node) || this.children.some((c) => c.contains(node));
  }

  /** Only ever called with focusable.ts's selector, so it answers only that. */
  querySelectorAll(_selector: string): FakeElement[] {
    const out: FakeElement[] = [];
    const walk = (node: FakeElement): void => {
      for (const child of node.children) {
        if (child.isTabReachable()) out.push(child);
        walk(child);
      }
    };
    walk(this);
    return out;
  }

  isTabReachable(): boolean {
    if (this.attrs.tabindex === "-1") return false;
    if (this.tagName === "a") return this.href !== "";
    if (FOCUSABLE_TAGS.includes(this.tagName)) return !this.disabled;
    return this.attrs.tabindex !== undefined;
  }
}

const dom = installFakeDom();
const fakeBody = new FakeElement("body");

function byId(root: FakeElement, id: string): FakeElement | null {
  for (const child of root.children) {
    if (child.id === id) return child;
    const hit = byId(child, id);
    if (hit) return hit;
  }
  return null;
}

const fakeDocument = {
  body: fakeBody,
  documentElement: dom.documentElement,
  activeElement: null as FakeElement | null,
  createElement: (tag: string): FakeElement => new FakeElement(tag),
  createElementNS: (_ns: string, tag: string): FakeElement => new FakeElement(tag),
  getElementById: (id: string): FakeElement | null => byId(fakeBody, id),
  listeners: {} as Record<string, Array<(e: unknown) => void>>,
  addEventListener(type: string, fn: (e: unknown) => void, capture = false): void {
    (fakeDocument.listeners[type] ??= []).push(fn);
    recordAdd(fakeDocument, type, fn, capture);
  },
  removeEventListener(type: string, fn: (e: unknown) => void, capture = false): void {
    const list = fakeDocument.listeners[type] ?? [];
    const i = list.indexOf(fn);
    if (i >= 0) list.splice(i, 1);
    recordRemove(fakeDocument, type, fn, capture);
  },
};

// Layered over installFakeDom()'s `document`, same documentElement object.
Object.defineProperty(globalThis, "document", {
  value: fakeDocument,
  writable: true,
  configurable: true,
});

function check(name: string, cond: boolean, detail: string): void {
  if (!cond) throw new Error(`${name}: ${detail}`);
  console.log(`ok - ${name}`);
}

/** The production code sees lib.dom's types; the assertions see the stub's. */
function el(node: unknown): FakeElement {
  return node as unknown as FakeElement;
}

/** document.getElementById, typed as what it actually returns here. */
function pick(id: string): FakeElement | null {
  return fakeDocument.getElementById(id);
}

/** Fires one event at an element's own listeners. */
function fire(target: FakeElement, type: string, event: Record<string, unknown> = {}): void {
  for (const fn of [...(target.listeners[type] ?? [])]) fn({ preventDefault: () => {}, ...event });
}

let prevented = 0;

/** Fires a keydown at the document-level listeners, the way a browser would. */
function press(key: string, shiftKey = false): void {
  for (const fn of [...(fakeDocument.listeners.keydown ?? [])]) {
    fn({
      key,
      shiftKey,
      preventDefault: () => {
        prevented++;
      },
    });
  }
}

/** The drawer's visible children, as class names — sync()'s observable result. */
function classes(menu: HamburgerMenu): string[] {
  return el(menu.drawer).children.map((c) => c.className);
}

/** The drawer's visible children, as text — what the order assertions read. */
function labels(menu: HamburgerMenu): string[] {
  return el(menu.drawer).children.map((c) => c.textContent ?? "");
}

// --- B4.3 — eager construction, and a slot mounted verbatim -----------------
//
// Every assertion in this block runs with NO open() ever called on the
// instance. A drawer built lazily on first open fails all five.

{
  const seen = { renders: 0, host: null as FakeElement | null, mounted: null as FakeElement | null };

  const menu = new HamburgerMenu({
    title: "Eager",
    items: [
      {
        id: "cols",
        render: (host) => {
          seen.renders++;
          seen.host = el(host);
          const select = document.createElement("select");
          select.id = "slot-select";
          host.append(select);
          seen.mounted = el(select);
        },
      },
    ],
  });

  check("B4.3: render() ran during construction, before any open()", seen.renders === 1, `got ${seen.renders} call(s)`);
  check(
    "B4.3: the host handed to render() is the .ui-menu-slot element",
    seen.host?.className === "ui-menu-slot",
    `got "${seen.host?.className ?? "null"}"`,
  );
  check(
    "B4.3: the drawer is attached to document.body before any open()",
    fakeBody.contains(el(menu.drawer)),
    "the drawer was not in the document",
  );
  check(
    "B4.3: the node the slot mounted is === document.getElementById, with no open() yet",
    seen.mounted !== null && pick("slot-select") === seen.mounted,
    "getElementById returned a different node, or nothing",
  );
  check(
    "B4.3: the slot host itself is addressable by its item id before any open()",
    seen.host !== null && pick("cols") === seen.host,
    "the slot host was not reachable by id",
  );

  // …and it is still the same node after a close/open cycle (§5 Step 4.4).
  const before = pick("slot-select");
  menu.open();
  menu.close();
  menu.open();
  check(
    "B4.3: the mounted node survives a close/open cycle as the SAME node",
    before !== null && pick("slot-select") === before,
    "the slot was rebuilt across the cycle",
  );
  check("B4.3: no open() called render() again", seen.renders === 1, `got ${seen.renders} call(s)`);
  menu.destroy();
}

// --- B4.1 — aria-expanded, aria-controls, Escape, focus, Tab trap -----------

{
  const fired: string[] = [];
  const menu = new HamburgerMenu({
    title: "A11y",
    items: [
      { id: "one", label: "One", onSelect: () => fired.push("one") },
      { id: "two", label: "Two", onSelect: () => fired.push("two") },
    ],
    onOpen: () => fired.push("open"),
    onClose: () => fired.push("close"),
  });

  const trigger = el(menu.trigger);
  const drawer = el(menu.drawer);

  check(
    'the created trigger is a <button type="button">',
    trigger.tagName === "button" && trigger.type === "button",
    `got "${trigger.tagName}" type "${trigger.type}"`,
  );
  check("the created trigger carries the ui-menu-trigger class", trigger.className === "ui-menu-trigger", `got "${trigger.className}"`);
  check(
    "the trigger's glyph is an assembled <svg> of three bars, not a markup string",
    trigger.children.length === 1 && trigger.children[0].tagName === "svg" && trigger.children[0].children.length === 3,
    JSON.stringify(trigger.children.map((c) => c.tagName)),
  );
  check('B4.1: aria-expanded starts "false"', trigger.attrs["aria-expanded"] === "false", `got "${trigger.attrs["aria-expanded"]}"`);
  check('the trigger carries aria-haspopup="true"', trigger.attrs["aria-haspopup"] === "true", JSON.stringify(trigger.attrs));
  check(
    "aria-controls names the drawer's real id (§5 Step 4.4)",
    drawer.id !== "" && trigger.attrs["aria-controls"] === drawer.id,
    `aria-controls="${trigger.attrs["aria-controls"]}" vs drawer id "${drawer.id}"`,
  );
  check(
    "aria-controls resolves through document.getElementById to the drawer itself",
    pick(trigger.attrs["aria-controls"]) === drawer,
    "aria-controls names an id nothing in the document carries",
  );
  check(
    'the drawer is role="dialog" aria-modal="true", labelled by the title option',
    drawer.attrs.role === "dialog" && drawer.attrs["aria-modal"] === "true" && drawer.attrs["aria-label"] === "A11y",
    JSON.stringify(drawer.attrs),
  );

  // Opened through the trigger's own click listener, not the method: the flip
  // has to happen on the interaction a user actually performs.
  fire(trigger, "click");
  check('B4.1: aria-expanded flips to "true" on open', trigger.attrs["aria-expanded"] === "true", `got "${trigger.attrs["aria-expanded"]}"`);
  check("the drawer gains .is-open", drawer.className === "ui-menu-drawer is-open", `got "${drawer.className}"`);
  check("the backdrop gains .is-open", el(menu.backdrop).className === "ui-menu-backdrop is-open", `got "${el(menu.backdrop).className}"`);
  check(
    "B4.1: focus moves to the first focusable item in the drawer on open",
    fakeDocument.activeElement === drawer.children[0],
    `activeElement is "${fakeDocument.activeElement?.className ?? "null"}"`,
  );
  check("onOpen fired exactly once", fired.filter((f) => f === "open").length === 1, JSON.stringify(fired));

  // The trap: forward from the last wraps to the first, back from the first
  // wraps to the last, and both wraps preventDefault.
  const items = drawer.children;
  const last = items[items.length - 1];
  last.focus();
  const before = prevented;
  press("Tab");
  check(
    "B4.1: Tab from the last focusable item wraps to the first",
    fakeDocument.activeElement === items[0] && prevented === before + 1,
    `activeElement "${fakeDocument.activeElement?.textContent ?? "null"}", prevented ${prevented - before}`,
  );
  press("Tab", true);
  check(
    "B4.1: Shift-Tab from the first focusable item wraps to the last",
    fakeDocument.activeElement === last && prevented === before + 2,
    `activeElement "${fakeDocument.activeElement?.textContent ?? "null"}", prevented ${prevented - before}`,
  );

  press("Escape");
  check("B4.1: Escape closes the drawer", drawer.className === "ui-menu-drawer", `got "${drawer.className}"`);
  check('B4.1: aria-expanded flips back to "false" on close', trigger.attrs["aria-expanded"] === "false", `got "${trigger.attrs["aria-expanded"]}"`);
  check("B4.1: focus returns to the trigger on close", fakeDocument.activeElement === trigger, "focus was left inside the closed drawer");
  check("onClose fired exactly once", fired.filter((f) => f === "close").length === 1, JSON.stringify(fired));

  // The one document-level listener is inert while closed: a stray Escape or
  // Tab elsewhere on the page must not act.
  const afterClose = prevented;
  press("Escape");
  press("Tab");
  check(
    "the document keydown listener is a no-op while the drawer is closed",
    prevented === afterClose && drawer.className === "ui-menu-drawer",
    `it prevented ${prevented - afterClose} event(s) with the drawer shut`,
  );

  // An action item runs its handler and closes the drawer.
  menu.open();
  fire(drawer.children[1], "click");
  check("an action item's onSelect fires on click", fired.includes("two"), JSON.stringify(fired));
  check("an action item closes the drawer after selecting", drawer.className === "ui-menu-drawer", `got "${drawer.className}"`);

  // The backdrop closes on mousedown, the way modal.ts's overlay does.
  menu.open();
  fire(el(menu.backdrop), "mousedown");
  check("a mousedown on the backdrop closes the drawer", drawer.className === "ui-menu-drawer", `got "${drawer.className}"`);

  menu.toggle();
  check("toggle() opens from closed", drawer.className === "ui-menu-drawer is-open", `got "${drawer.className}"`);
  menu.toggle();
  check("toggle() closes from open", drawer.className === "ui-menu-drawer", `got "${drawer.className}"`);

  menu.destroy();
}

// --- B4.8 — the trap predicate has one definition ---------------------------
//
// The criterion itself is a grep over web/shared/ts/ (= 1 hit, focusable.ts).
// What is assertable here is that the menu's trap and that module's predicate
// are the same code path: the drawer's focusable set, read back through the
// shared helper, is exactly the set the trap wrapped around above.

{
  const menu = new HamburgerMenu({
    items: [
      { id: "a", label: "A", onSelect: () => {} },
      { section: "Group" },
      { separator: true },
      { id: "l", label: "L", href: "#l" },
    ],
  });
  const focusable = getFocusable(menu.drawer);
  check(
    "B4.8: the shared predicate sees the action and the link, and skips the separator and the heading",
    focusable.length === 2,
    `got ${focusable.length} of ${JSON.stringify(classes(menu))}`,
  );
  check(
    "B4.8: and it returns them in document order",
    el(focusable[0]).className === "ui-menu-item" && el(focusable[1]).className === "ui-menu-link",
    JSON.stringify(focusable.map((f) => el(f).className)),
  );
  menu.destroy();
}

// --- the item kinds, and the classes each one emits -------------------------

{
  const menu = new HamburgerMenu({
    items: [
      { id: "a1", label: "Action one", onSelect: () => {} },
      { id: "a2", label: "Action two", icon: "download", onSelect: () => {} },
      { id: "lnk", label: "Docs", href: "/docs" },
      { separator: true },
      { section: "Columns" },
      { id: "slot", render: (host) => host.append(document.createElement("input")) },
    ],
  });

  check(
    "the five item kinds emit their classes in registration order",
    classes(menu).join(" ") === "ui-menu-item ui-menu-item ui-menu-link ui-menu-separator ui-menu-label ui-menu-slot",
    JSON.stringify(classes(menu)),
  );

  const children = el(menu.drawer).children;
  check('an action item is a <button type="button">', children[0].tagName === "button" && children[0].type === "button", `got "${children[0].tagName}"`);
  check("an icon name reaches CSS as data-icon and goes nowhere else", children[1].dataset.icon === "download", JSON.stringify(children[1].dataset));
  check("a link item is an <a> carrying its href", children[2].tagName === "a" && children[2].href === "/docs", `got "${children[2].tagName}" href "${children[2].href}"`);
  check('a separator carries role="separator" and is not focusable', children[3].attrs.role === "separator" && !children[3].isTabReachable(), JSON.stringify(children[3].attrs));
  check("a section heading is a .ui-menu-label and is not focusable", !children[4].isTabReachable(), "the heading was Tab-reachable");
  check("a render slot's host is a <div> carrying the item id", children[5].tagName === "div" && children[5].id === "slot", `got "${children[5].tagName}" id "${children[5].id}"`);
  check(
    "the slot holds exactly the node the module mounted",
    children[5].children.length === 1 && children[5].children[0].tagName === "input",
    JSON.stringify(children[5].children.map((c) => c.tagName)),
  );

  menu.destroy();
}

// --- B4.6 — every label arrives via textContent -----------------------------

{
  const raw = "<b>evil</b> & <i>x</i>";
  const menu = new HamburgerMenu({
    items: [
      { id: "a", label: raw, onSelect: () => {} },
      { id: "l", label: raw, href: "/x" },
      { section: raw },
    ],
  });
  const children = el(menu.drawer).children;
  check("B4.6: an action's label goes through textContent verbatim, unparsed", children[0].textContent === raw, `got "${children[0].textContent}"`);
  check("B4.6: a link's label goes through textContent verbatim, unparsed", children[1].textContent === raw, `got "${children[1].textContent}"`);
  check("B4.6: a section heading's text goes through textContent verbatim, unparsed", children[2].textContent === raw, `got "${children[2].textContent}"`);
  check("B4.6: and no label produced a child element", children.every((c) => c.children.length === 0), "a label was parsed into nodes");
  menu.destroy();
}

// --- B4.4 — when() is re-evaluated on every open ----------------------------

{
  const state = { visible: false, evaluations: 0 };
  const menu = new HamburgerMenu({
    items: [
      { id: "always", label: "Always", onSelect: () => {} },
      {
        id: "guarded",
        label: "Guarded",
        onSelect: () => {},
        when: () => {
          state.evaluations++;
          return state.visible;
        },
      },
    ],
  });

  check("B4.4: a guard that is false at construction omits its item", classes(menu).length === 1, JSON.stringify(labels(menu)));
  const atConstruction = state.evaluations;
  check("B4.4: the guard was evaluated at construction, so the eager tree is correct too", atConstruction >= 1, `got ${atConstruction}`);

  // The guard flips while the drawer is SHUT — the only way to tell a per-open
  // re-evaluation from a value cached at construction.
  state.visible = true;
  check("B4.4: flipping the guard while closed changes nothing on its own", classes(menu).length === 1, JSON.stringify(labels(menu)));
  menu.open();
  check("B4.4: the item appears on the next open, with no addItem call", classes(menu).length === 2, JSON.stringify(labels(menu)));
  check("B4.4: and the guard was re-evaluated to do it", state.evaluations > atConstruction, `still ${state.evaluations}`);
  menu.close();

  state.visible = false;
  menu.open();
  check("B4.4: and it disappears again on the open after the guard flips back", classes(menu).length === 1, JSON.stringify(labels(menu)));
  menu.close();

  // Order is restored, not appended to: the item returns to its registered
  // position rather than to the end.
  state.visible = true;
  menu.open();
  check("B4.4: a re-appearing item returns to its registered position", labels(menu).join("|") === "Always|Guarded", JSON.stringify(labels(menu)));
  menu.close();
  menu.destroy();
}

// --- B4.5 — addItem/removeItem/updateItem resolve by id, no-op on unknown ---

{
  const menu = new HamburgerMenu({
    items: [
      { id: "keep", label: "Keep", onSelect: () => {} },
      { id: "drop", label: "Drop", onSelect: () => {} },
      { id: "patch", label: "Before", href: "/before" },
    ],
  });
  const children = el(menu.drawer).children;

  const extra: MenuItem = { id: "added", label: "Added", onSelect: () => {} };
  menu.addItem(extra);
  check("B4.5: addItem appends the item and it is visible immediately", labels(menu).join("|") === "Keep|Drop|Before|Added", JSON.stringify(labels(menu)));

  menu.removeItem("drop");
  check("B4.5: removeItem resolves by id", labels(menu).join("|") === "Keep|Before|Added", JSON.stringify(labels(menu)));

  menu.updateItem("patch", { label: "After", href: "/after" });
  check(
    "B4.5: updateItem resolves by id and patches in place, keeping its position",
    labels(menu).join("|") === "Keep|After|Added" && children[1].href === "/after",
    `${JSON.stringify(labels(menu))} href "${children[1].href}"`,
  );

  const settled = labels(menu).join("|");
  menu.removeItem("no-such-id");
  menu.updateItem("no-such-id", { label: "nope" });
  check("B4.5: removeItem and updateItem are no-ops on an unknown id, not throws", labels(menu).join("|") === settled, JSON.stringify(labels(menu)));

  // A separator carries no id, so nothing can address it — the no-op path
  // again, from the other direction.
  menu.addItem({ separator: true });
  menu.removeItem("undefined");
  check(
    "B4.5: an id-less item cannot be addressed away by accident",
    children.length === 4 && children[3].className === "ui-menu-separator",
    JSON.stringify(classes(menu)),
  );

  // updateItem on a render slot patches the item, never the mounted DOM.
  menu.addItem({
    id: "slot2",
    render: (host) => {
      const input = document.createElement("input");
      input.id = "slot2-input";
      host.append(input);
    },
  });
  const mounted = pick("slot2-input");
  menu.updateItem("slot2", { when: () => true });
  check(
    "B4.5: updateItem on a render slot leaves the mounted node identical",
    mounted !== null && pick("slot2-input") === mounted,
    "a patch rebuilt the slot's contents",
  );

  menu.destroy();
}

// --- B4.7 — destroy() removes every listener it added -----------------------
//
// Measured on the ledger, which every add and every remove in this suite's stub
// passes through. themePicker is deliberately absent from this instance:
// ThemeManager.renderPicker() adds its own click listeners to its own buttons,
// and destroy()'s claim is about the listeners THIS class added.

{
  const baseline = liveCount();
  const menu = new HamburgerMenu({
    title: "Accounting",
    items: [
      { id: "a1", label: "One", onSelect: () => {} },
      { id: "a2", label: "Two", onSelect: () => {} },
      { id: "a3", label: "Three", onSelect: () => {} },
      { separator: true },
      { section: "Heading" },
      { id: "l1", label: "Link", href: "/l" },
      { id: "s1", render: () => {} },
    ],
  });

  const added = liveCount() - baseline;
  check(
    "B4.7: construction adds exactly 6 listeners — trigger click, backdrop mousedown, document keydown, one click per action item — and none for the separator, the heading, the link or the slot",
    added === 6,
    `got ${added}`,
  );

  menu.open();
  menu.close();
  menu.open();
  check("B4.7: open/close cycles add and remove nothing", liveCount() - baseline === 6, `got ${liveCount() - baseline}`);

  menu.addItem({ id: "a4", label: "Four", onSelect: () => {} });
  check("B4.7: addItem's action item registers one more", liveCount() - baseline === 7, `got ${liveCount() - baseline}`);
  menu.removeItem("a4");
  check("B4.7: removeItem takes that one back off", liveCount() - baseline === 6, `got ${liveCount() - baseline}`);

  menu.destroy();
  check("B4.7: destroy() leaves zero of its listeners live", liveCount() === baseline, `${liveCount() - baseline} listener(s) survived destroy()`);
  check("B4.7: destroy() detaches the drawer", !fakeBody.contains(el(menu.drawer)), "the drawer was left in the document");
  check("B4.7: destroy() detaches the backdrop", !fakeBody.contains(el(menu.backdrop)), "the backdrop was left in the document");
  check("B4.7: destroy() detaches a trigger it created", !fakeBody.contains(el(menu.trigger)), "the created trigger was left in the document");
  menu.destroy();
  check("B4.7: destroy() is idempotent", liveCount() === baseline, `got ${liveCount() - baseline}`);
}

// --- mountTrigger: adopt an existing button, and hand it back on destroy ----

{
  const host = document.createElement("button");
  host.id = "btn-hamburger";
  host.className = "topbar-btn";
  fakeBody.append(el(host));

  const baseline = liveCount();
  const menu = new HamburgerMenu({ title: "Adopted", items: [], mountTrigger: host });

  check("mountTrigger is used as the trigger rather than a new button", el(menu.trigger) === el(host), "a second trigger was created");
  check("an adopted trigger keeps its own class, so its glyph and position do not move", el(host).className === "topbar-btn", `got "${el(host).className}"`);
  check("an adopted trigger gains no glyph of ours", el(host).children.length === 0, `got ${el(host).children.length} child node(s)`);
  check("an adopted trigger gains aria-expanded", el(host).attrs["aria-expanded"] === "false", JSON.stringify(el(host).attrs));

  fire(el(host), "click");
  check("an adopted trigger opens the drawer", el(menu.drawer).className === "ui-menu-drawer is-open", `got "${el(menu.drawer).className}"`);
  check(
    "an empty drawer falls back to focusing the drawer itself, as modal.ts does for its panel",
    fakeDocument.activeElement === el(menu.drawer),
    "focus went somewhere else",
  );
  menu.close();

  menu.destroy();
  check("destroy() leaves an adopted trigger in the page", fakeBody.contains(el(host)), "the module's own button was removed");
  check(
    "destroy() removes the three attributes it set on an adopted trigger",
    el(host).attrs["aria-expanded"] === undefined && el(host).attrs["aria-controls"] === undefined && el(host).attrs["aria-haspopup"] === undefined,
    JSON.stringify(el(host).attrs),
  );
  check("destroy() leaves no listener on an adopted trigger", liveCount() === baseline, `got ${liveCount() - baseline}`);
  el(host).remove();
}

// --- themePicker: the standard section, built by the one ThemeManager -------

{
  const themes = new ThemeManager({ module: "menu-suite", default: "dark" });
  const menu = new HamburgerMenu({
    title: "Picker",
    items: [{ id: "a", label: "A", onSelect: () => {} }],
    themePicker: true,
    themes,
  });

  const children = el(menu.drawer).children;
  const section = children[children.length - 1];
  check("themePicker appends a .ui-menu-section last", section.className === "ui-menu-section", JSON.stringify(classes(menu)));
  check(
    "the section is headed by a .ui-menu-label text node",
    section.children[0].className === "ui-menu-label" && section.children[0].textContent === "Theme",
    JSON.stringify(section.children.map((c) => c.className)),
  );
  check(
    "the picker itself is ThemeManager.renderPicker's .ui-theme-picker — one definition, mounted here",
    section.children[1].className === "ui-theme-picker",
    JSON.stringify(section.children.map((c) => c.className)),
  );
  const picker = section.children[1];
  check("the picker offers one button per theme", picker.children.length === themes.list.length, `got ${picker.children.length} for ${themes.list.length} theme(s)`);
  check(
    "no colour value reaches the menu — each swatch carries only its data-theme",
    picker.children.every((b) => b.children[0].className === "ui-theme-swatch" && b.children[0].dataset.theme !== undefined),
    "a swatch was built some other way",
  );

  // The picker is built once and stays last across opens.
  menu.open();
  menu.close();
  menu.open();
  const after = children[children.length - 1];
  check("the picker section is not rebuilt by an open, and stays last", after === section && section.children[1] === picker, JSON.stringify(classes(menu)));

  // Picking a theme goes through the one ThemeManager, so it persists.
  fire(picker.children[3], "click");
  check(
    "picking a swatch in the drawer stamps data-theme through the shared ThemeManager",
    dom.documentElement.dataset.theme === themes.list[3],
    `got "${dom.documentElement.dataset.theme}" for "${themes.list[3]}"`,
  );
  check(
    "and it persists under the module's own storage key",
    dom.storage.get("ui-theme:menu-suite") === themes.list[3],
    `storage holds ${JSON.stringify([...dom.storage.entries()])}`,
  );

  menu.close();
  menu.destroy();
}

{
  const menu = new HamburgerMenu({ items: [{ id: "a", label: "A", onSelect: () => {} }], themePicker: true });
  check(
    "themePicker without a ThemeManager mounts no section rather than throwing",
    classes(menu).join(" ") === "ui-menu-item",
    JSON.stringify(classes(menu)),
  );
  menu.destroy();
}

// --- BX.2 / BX.3 — the barrel's export shape --------------------------------

check("the barrel exposes HamburgerMenu", typeof barrel.HamburgerMenu === "function", `got ${typeof barrel.HamburgerMenu}`);
check("the barrel's HamburgerMenu is this module's class", barrel.HamburgerMenu === HamburgerMenu, "the barrel re-exports a different binding");
check(
  "the barrel still exposes the eight values C1-C3 landed beside it",
  typeof barrel.openModal === "function" &&
    typeof barrel.confirmDialog === "function" &&
    typeof barrel.alertDialog === "function" &&
    typeof barrel.promptDialog === "function" &&
    typeof barrel.setTheme === "function" &&
    typeof barrel.ThemeManager === "function" &&
    typeof barrel.showToast === "function" &&
    Array.isArray(barrel.THEMES),
  "a value export was displaced by this commit",
);
check(
  "focusable.ts is NOT part of the public surface",
  !Object.keys(barrel).includes("getFocusable"),
  `the barrel exports ${JSON.stringify(Object.keys(barrel))}`,
);
