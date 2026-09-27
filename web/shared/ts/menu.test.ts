// HamburgerMenu unit tests: eager construction with verbatim slot mounting,
// a11y wiring (aria-expanded, focus trap, Escape), per-open when()
// re-evaluation, id-addressed add/remove/update, textContent-only labels,
// destroy() listener accounting, shared focusable predicate, barrel exports.
//
// No jsdom, no @types/node. Two stubs:
//   - ./test-dom's installFakeDom() for ThemeManager globals (the
//     themePicker:true case mounts a real ThemeManager)
//   - a hand-rolled element stub for the DOM tree, with createElementNS,
//     getElementById, and a listener ledger to observe destroy()'s cleanup
//
// installFakeDom() runs first; the element stub layers over the document
// it installs, sharing the same documentElement. Layout-dependent checks
// (drawer slide, reduced-motion, swatch fills) are CSS facts verified
// by check-shared-css.mjs, not here.

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
  value = "";
  disabled = false;
  tabIndex = 0;
  textContent: string | null = "";
  dataset: Record<string, string> = {};
  attrs: Record<string, string> = {};
  children: FakeElement[] = [];
  listeners: Record<string, Array<(e: unknown) => void>> = {};
  parent: FakeElement | null = null;
  _rectLeft: number | undefined;
  _rectRight: number | undefined;

  constructor(tagName: string) {
    this.tagName = tagName;
  }

  /** Only side:"auto" ever calls this — a hand-set stand-in for real layout. */
  getBoundingClientRect(): { left: number; right: number; width: number } {
    const left = this._rectLeft ?? 0;
    const right = this._rectRight ?? 100;
    return { left, right, width: right - left };
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

  /** Attached to some parent — enough for mountSignOut's "is it mounted yet". */
  get isConnected(): boolean {
    return this.parent !== null;
  }

  /** Inserts node just before this one, as session.ts places its button. */
  before(node: FakeElement): void {
    if (!this.parent) return;
    node.remove();
    node.parent = this.parent;
    this.parent.children.splice(this.parent.children.indexOf(this), 0, node);
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

// Only side:"auto" ever reads this — mutable so that one test can move it.
const fakeWindow = { innerWidth: 800 };
Object.defineProperty(globalThis, "window", {
  value: fakeWindow,
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

/**
 * The drawer's children with the always-present .ui-menu-header excluded —
 * that header is chrome C0 adds, not a registered item, and sync() never
 * touches it. Recomputed fresh on every call, never cached, since children
 * mutate in place under addItem/removeItem/updateItem.
 */
function itemChildren(menu: HamburgerMenu): FakeElement[] {
  return el(menu.drawer).children.filter((c) => c.className !== "ui-menu-header");
}

/** The drawer's visible items, as class names — sync()'s observable result. */
function classes(menu: HamburgerMenu): string[] {
  return itemChildren(menu).map((c) => c.className);
}

/** The drawer's visible items, as text — what the order assertions read. */
function labels(menu: HamburgerMenu): string[] {
  return itemChildren(menu).map((c) => c.textContent ?? "");
}

// --- eager construction, and a slot mounted verbatim ------------------------
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

  // …and it is still the same node after a close/open cycle.
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

// --- aria-expanded, aria-controls, Escape, focus, Tab trap -----------------

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
    "aria-controls names the drawer's real id",
    drawer.id !== "" && trigger.attrs["aria-controls"] === drawer.id,
    `aria-controls="${trigger.attrs["aria-controls"]}" vs drawer id "${drawer.id}"`,
  );
  check(
    "aria-controls resolves through document.getElementById to the drawer itself",
    pick(trigger.attrs["aria-controls"]) === drawer,
    "aria-controls names an id nothing in the document carries",
  );
  check(
    'the drawer is role="dialog" aria-modal="true", labeled by the title option',
    drawer.attrs.role === "dialog" && drawer.attrs["aria-modal"] === "true" && drawer.attrs["aria-label"] === "A11y",
    JSON.stringify(drawer.attrs),
  );

  // Opened through the trigger's own click listener, not the method: the flip
  // has to happen on the interaction a user actually performs.
  fire(trigger, "click");
  check('B4.1: aria-expanded flips to "true" on open', trigger.attrs["aria-expanded"] === "true", `got "${trigger.attrs["aria-expanded"]}"`);
  check("the drawer gains .is-open", drawer.className === "ui-menu-drawer is-open", `got "${drawer.className}"`);
  check("the backdrop gains .is-open", el(menu.backdrop).className === "ui-menu-backdrop is-open", `got "${el(menu.backdrop).className}"`);
  // C0: the header's close button is now the first focusable descendant, so
  // it — not the first registered item — is what the trap wraps around.
  const focusableEls = getFocusable(drawer as unknown as HTMLElement).map(el);
  check(
    "B4.1: focus moves to the first focusable item in the drawer on open",
    fakeDocument.activeElement === focusableEls[0],
    `activeElement is "${fakeDocument.activeElement?.className ?? "null"}"`,
  );
  check("onOpen fired exactly once", fired.filter((f) => f === "open").length === 1, JSON.stringify(fired));

  // The trap: forward from the last wraps to the first, back from the first
  // wraps to the last, and both wraps preventDefault.
  const last = focusableEls[focusableEls.length - 1];
  last.focus();
  const before = prevented;
  press("Tab");
  check(
    "B4.1: Tab from the last focusable item wraps to the first",
    fakeDocument.activeElement === focusableEls[0] && prevented === before + 1,
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
  fire(itemChildren(menu)[1], "click");
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

// --- the trap predicate has one definition ----------------------------------
//
// The menu's trap and the shared focusable module use the same code path:
// the drawer's focusable set, read back through the shared helper, is
// exactly the set the trap wrapped around above.

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
    "B4.8: the shared predicate sees the header's close button, the action and the link, and skips the separator and the heading",
    focusable.length === 3,
    `got ${focusable.length} of ${JSON.stringify(classes(menu))}`,
  );
  check(
    "B4.8: and it returns them in document order",
    el(focusable[0]).className === "ui-menu-close" &&
      el(focusable[1]).className === "ui-menu-item" &&
      el(focusable[2]).className === "ui-menu-link",
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

  const children = itemChildren(menu);
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

// --- every label arrives via textContent ------------------------------------

{
  const raw = "<b>evil</b> & <i>x</i>";
  const menu = new HamburgerMenu({
    items: [
      { id: "a", label: raw, onSelect: () => {} },
      { id: "l", label: raw, href: "/x" },
      { section: raw },
    ],
  });
  const children = itemChildren(menu);
  check("B4.6: an action's label goes through textContent verbatim, unparsed", children[0].textContent === raw, `got "${children[0].textContent}"`);
  check("B4.6: a link's label goes through textContent verbatim, unparsed", children[1].textContent === raw, `got "${children[1].textContent}"`);
  check("B4.6: a section heading's text goes through textContent verbatim, unparsed", children[2].textContent === raw, `got "${children[2].textContent}"`);
  check("B4.6: and no label produced a child element", children.every((c) => c.children.length === 0), "a label was parsed into nodes");
  menu.destroy();
}

// --- when() is re-evaluated on every open -----------------------------------

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

// --- addItem/removeItem/updateItem resolve by id, no-op on unknown ---------

{
  const menu = new HamburgerMenu({
    items: [
      { id: "keep", label: "Keep", onSelect: () => {} },
      { id: "drop", label: "Drop", onSelect: () => {} },
      { id: "patch", label: "Before", href: "/before" },
    ],
  });

  const extra: MenuItem = { id: "added", label: "Added", onSelect: () => {} };
  menu.addItem(extra);
  check("B4.5: addItem appends the item and it is visible immediately", labels(menu).join("|") === "Keep|Drop|Before|Added", JSON.stringify(labels(menu)));

  menu.removeItem("drop");
  check("B4.5: removeItem resolves by id", labels(menu).join("|") === "Keep|Before|Added", JSON.stringify(labels(menu)));

  menu.updateItem("patch", { label: "After", href: "/after" });
  check(
    "B4.5: updateItem resolves by id and patches in place, keeping its position",
    labels(menu).join("|") === "Keep|After|Added" && itemChildren(menu)[1].href === "/after",
    `${JSON.stringify(labels(menu))} href "${itemChildren(menu)[1].href}"`,
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
    itemChildren(menu).length === 4 && itemChildren(menu)[3].className === "ui-menu-separator",
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

// --- destroy() removes every listener it added -----------------------------
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
    "B4.7: construction adds exactly 7 listeners — trigger click, backdrop mousedown, document keydown, header close-button click, one click per action item — and none for the separator, the heading, the link or the slot",
    added === 7,
    `got ${added}`,
  );

  menu.open();
  menu.close();
  menu.open();
  check("B4.7: open/close cycles add and remove nothing", liveCount() - baseline === 7, `got ${liveCount() - baseline}`);

  menu.addItem({ id: "a4", label: "Four", onSelect: () => {} });
  check("B4.7: addItem's action item registers one more", liveCount() - baseline === 8, `got ${liveCount() - baseline}`);
  menu.removeItem("a4");
  check("B4.7: removeItem takes that one back off", liveCount() - baseline === 7, `got ${liveCount() - baseline}`);

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
    // C0: the header's close button is always present, so an item-less drawer
    // is never focusable-empty — focus lands there, not on the drawer itself.
    "with no items, focus lands on the header's close button rather than falling back to the drawer",
    fakeDocument.activeElement === el(getFocusable(menu.drawer)[0]),
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
  // The picker alone in the drawer: the full swatch list.
  const menu = new HamburgerMenu({
    title: "Picker",
    items: [],
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
    "no color value reaches the menu — each swatch carries only its data-theme",
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

// --- themePicker sharing the drawer: the compact dropdown -------------------

{
  const themes = new ThemeManager({ module: "menu-suite-select", default: "dark" });
  const menu = new HamburgerMenu({
    title: "Picker",
    items: [{ id: "a", label: "A", onSelect: () => {} }],
    themePicker: true,
    themes,
  });

  const children = el(menu.drawer).children;
  const section = children[children.length - 1];
  const select = section.children[1];
  check(
    "a picker sharing the drawer with other items renders as a .ui-theme-select dropdown",
    select.tagName === "select" && select.className === "ui-theme-select",
    `got <${select.tagName} class="${select.className}">`,
  );
  check(
    "the dropdown offers one option per theme",
    select.children.length === themes.list.length && select.children.every((o, i) => o.tagName === "option" && o.value === themes.list[i]),
    JSON.stringify(select.children.map((o) => o.value)),
  );

  select.value = themes.list[2];
  fire(select, "change");
  check(
    "choosing an option stamps data-theme through the shared ThemeManager",
    dom.documentElement.dataset.theme === themes.list[2],
    `got "${dom.documentElement.dataset.theme}" for "${themes.list[2]}"`,
  );
  check(
    "and it persists under the module's own storage key",
    dom.storage.get("ui-theme:menu-suite-select") === themes.list[2],
    `storage holds ${JSON.stringify([...dom.storage.entries()])}`,
  );

  themes.set(themes.list[5]);
  check(
    "a theme set from elsewhere moves the dropdown's selection with it",
    select.value === themes.list[5],
    `got "${select.value}" for "${themes.list[5]}"`,
  );
  menu.destroy();
}

{
  const themes = new ThemeManager({ module: "menu-suite-headings", default: "dark" });
  const menu = new HamburgerMenu({
    items: [{ section: "Only a heading" }, { separator: true }],
    themePicker: true,
    themes,
  });
  const children = el(menu.drawer).children;
  const section = children[children.length - 1];
  check(
    "headings and separators alone don't count as sharing the drawer — the list stays",
    section.children[1].className === "ui-theme-picker",
    JSON.stringify(section.children.map((c) => c.className)),
  );
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

// --- C0: side option — explicit "right" --------------------------------------

{
  const menu = new HamburgerMenu({
    title: "Right",
    side: "right",
    items: [{ id: "a", label: "A", onSelect: () => {} }],
  });
  check(
    "C0: side:'right' stamps data-side on the drawer at construction",
    el(menu.drawer).dataset.side === "right",
    JSON.stringify(el(menu.drawer).dataset),
  );
  menu.destroy();
}

// --- C0: side option — "auto", resolved against the trigger on first open ---

{
  const menu = new HamburgerMenu({
    title: "Auto",
    side: "auto",
    items: [{ id: "a", label: "A", onSelect: () => {} }],
  });

  check(
    "C0: side:'auto' sets no data-side before the first open",
    el(menu.drawer).dataset.side === undefined,
    JSON.stringify(el(menu.drawer).dataset),
  );

  // Put the trigger on the right half of an 800px-wide viewport.
  el(menu.trigger)._rectLeft = 600;
  el(menu.trigger)._rectRight = 700;
  fakeWindow.innerWidth = 800;

  menu.open();
  check(
    "C0: side:'auto' resolves to 'right' when the trigger sits on the right half",
    el(menu.drawer).dataset.side === "right",
    JSON.stringify(el(menu.drawer).dataset),
  );

  // Move the trigger's rect after resolution — the cached choice must stick.
  menu.close();
  el(menu.trigger)._rectLeft = 0;
  el(menu.trigger)._rectRight = 50;
  menu.open();
  check(
    "C0: side:'auto' caches its resolution — a later open does not re-query",
    el(menu.drawer).dataset.side === "right",
    JSON.stringify(el(menu.drawer).dataset),
  );

  menu.close();
  menu.destroy();
}

// --- C0: close button and title, in a header before any items ---------------

{
  const menu = new HamburgerMenu({
    title: "Settings",
    items: [{ id: "a", label: "A", onSelect: () => {} }],
  });

  const header = el(menu.drawer).children[0];
  check("C0: the drawer's first child is the .ui-menu-header", header.className === "ui-menu-header", JSON.stringify(el(menu.drawer).children.map((c) => c.className)));

  const closeButton = header.children[0];
  const titleEl = header.children[1];
  check(
    "C0: the header's first child is the .ui-menu-close button",
    closeButton.tagName === "button" && closeButton.className === "ui-menu-close",
    `got tag "${closeButton.tagName}" class "${closeButton.className}"`,
  );
  check(
    "C0: the header carries a .ui-menu-title span with the title text",
    titleEl.className === "ui-menu-title" && titleEl.textContent === "Settings",
    `got class "${titleEl.className}" text "${titleEl.textContent}"`,
  );

  menu.open();
  check("C0: the drawer is open before the close button is clicked", el(menu.drawer).className === "ui-menu-drawer is-open", `got "${el(menu.drawer).className}"`);
  fire(closeButton, "click");
  check("C0: clicking the close button closes the drawer", el(menu.drawer).className === "ui-menu-drawer", `got "${el(menu.drawer).className}"`);

  menu.destroy();
}

// --- C0: default side sets no data-side attribute ----------------------------

{
  const menu = new HamburgerMenu({ items: [{ id: "a", label: "A", onSelect: () => {} }] });
  check(
    "C0: with no side option, the drawer carries no data-side attribute — left is CSS's positional default",
    el(menu.drawer).dataset.side === undefined,
    JSON.stringify(el(menu.drawer).dataset),
  );
  menu.destroy();
}

// --- barrel export shape ----------------------------------------------------

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

// --- sign-out: shown before the trigger only when there is a session ---------

async function signOutCase(sessionStatus: number, body: unknown): Promise<FakeElement[]> {
  const g = globalThis as unknown as { fetch?: unknown; window: Record<string, unknown> };
  const saved = g.fetch;
  const requests: string[] = [];
  g.fetch = async (url: string) => {
    requests.push(url);
    return { ok: sessionStatus === 200, status: sessionStatus, json: async () => body };
  };
  // session.ts listens on window for real use (clicks, typing, ...).
  const windowListeners = new Set<string>();
  g.window.addEventListener = (type: string) => windowListeners.add(type);
  g.window.removeEventListener = (type: string) => windowListeners.delete(type);
  try {
    const bar = new FakeElement("div");
    const trigger = new FakeElement("button");
    bar.append(trigger);
    const menu = new HamburgerMenu({ items: [], mountTrigger: trigger as unknown as HTMLElement });
    for (let i = 0; i < 5; i++) await Promise.resolve();
    const children = bar.children.length === 1 && bar.children[0] !== trigger ? [...bar.children[0].children] : [...bar.children];
    const watching = windowListeners.has("pointerdown") && windowListeners.has("keydown");
    check(
      "sign-out: the idle watch listens for real use exactly when there is a session",
      watching === (children.length === 2),
      `listening=${watching} with ${children.length} children`,
    );
    check("sign-out: only the session check was requested up front", requests.join() === "/api/auth/session", `got ${requests.join()}`);
    menu.destroy();
    check("sign-out: destroy() stops the idle watch", windowListeners.size === 0, `still listening for ${[...windowListeners].join()}`);
    check(
      "sign-out: destroy() removes the button and restores the trigger",
      bar.children.length === 1 && bar.children[0] === trigger,
      `left ${bar.children.length} children`,
    );
    return children;
  } finally {
    g.fetch = saved;
  }
}

void (async () => {
  const withSession = await signOutCase(200, { identity: "", methods: ["pin:todo:ab"], idleSeconds: 3600 });
  check(
    "sign-out: with a session, the icon and the trigger share one group, icon first",
    withSession.length === 2 && withSession[0].className === "ui-menu-trigger ui-signout" && withSession[0].attrs["aria-label"] === "Sign out" && withSession[1].tagName === "button",
    `bar children ${JSON.stringify(withSession.map((c) => c.className))}`,
  );
  const noSession = await signOutCase(401, { error: "unauthorized" });
  check("sign-out: without a session, nothing is added", noSession.length === 1, `got ${noSession.length} children`);
  const spaFallback = await signOutCase(200, "<html>");
  check("sign-out: a non-session 200 (an open module's page) adds nothing", spaFallback.length === 1, `got ${spaFallback.length} children`);
})();
