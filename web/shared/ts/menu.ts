// menu.ts — the one hamburger every module gets. The class owns the chrome;
// modules own the contents through hooks.
//
// Chrome is modelled on todo's slide-in drawer, structurally: trigger →
// backdrop → left-edge drawer sliding on `transform`. Three a11y features
// are provided: `aria-expanded`, Escape handling, and a focus trap. The
// trap's element predicate is imported from ./focusable.js.
//
// Three properties worth stating because they are load-bearing elsewhere:
//
//   1. CONSTRUCTION IS EAGER. The trigger, the backdrop, the drawer and every
//      item — `render` slots included — are built and attached to the live
//      document in the constructor, and open() only animates. Controls keep
//      their inline `onchange` attributes and are found by `id`, so they must
//      resolve whether or not the drawer has ever been opened.
//   2. `when()` IS RE-EVALUATED ON EVERY OPEN, never cached at
//      construction, so a guarded item appears and disappears with no
//      addItem/removeItem churn. Each item's element is built once and kept:
//      sync() attaches and detaches the SAME node, so a slot's mounted DOM
//      survives any number of close/open cycles.
//   3. NO MARKUP STRING IS ASSIGNED ANYWHERE IN THIS FILE. Every element
//      comes from createElement/createElementNS and every label is a text
//      node, which is also why the trigger's glyph is an assembled
//      inline SVG rather than a pasted icon-font fragment.
//
// No colour, size or motion value reaches this file either: the eight .ui-menu-*
// classes in web/shared/css/components.css carry all of it, including the
// reduced-motion suppression — so a module that wants a different
// drawer width restyles one class rather than passing an option.

import type { ThemeManager } from "./theme.js";
import { getFocusable } from "./focusable.js";

/** An action row: runs `onSelect` and closes the drawer. */
interface MenuActionItem {
  id: string;
  label: string;
  /** An opaque icon name, surfaced to CSS as `data-icon` and nothing more. */
  icon?: string;
  onSelect: () => void;
  when?: () => boolean;
}

/** A navigation row: a real anchor, so it is keyboard-activatable natively. */
interface MenuLinkItem {
  id: string;
  label: string;
  href: string;
  when?: () => boolean;
}

/** A horizontal rule between groups. Carries no id and is not focusable. */
interface MenuSeparatorItem {
  separator: true;
  when?: () => boolean;
}

/** A group heading. Carries no id and is not focusable. */
interface MenuSectionItem {
  section: string;
  when?: () => boolean;
}

/**
 * An arbitrary-DOM slot — the kind that guarantees zero functionality loss.
 * `render` is called ONCE, during construction, with a host element already
 * attached to the document, and is never called again: the nodes a module
 * mounts are its own and this module does not rebuild them.
 */
interface MenuRenderItem {
  id: string;
  render: (host: HTMLElement) => void;
  when?: () => boolean;
}

export type MenuItem =
  | MenuActionItem
  | MenuLinkItem
  | MenuSeparatorItem
  | MenuSectionItem
  | MenuRenderItem;

export interface HamburgerMenuOptions {
  /** Names the drawer for assistive tech via `aria-label`. */
  title?: string;
  items: MenuItem[];
  /** Appends the standard ThemeManager-backed picker section. Needs `themes`. */
  themePicker?: boolean;
  /** The module's one ThemeManager — the same instance its topbar uses. */
  themes?: ThemeManager;
  onOpen?: () => void;
  onClose?: () => void;
  /**
   * Adopt an existing button as the trigger instead of creating one, so a
   * module that already ships a hamburger keeps its glyph and its position
   * and gains only the a11y wiring.
   * An adopted trigger is left in place by destroy(); a created one is not.
   */
  mountTrigger?: HTMLElement;
}

const SVG_NS = "http://www.w3.org/2000/svg";

/** Distinct drawer ids, so `aria-controls` is unambiguous with N instances. */
let instanceCount = 0;

function isSeparator(item: MenuItem): item is MenuSeparatorItem {
  return "separator" in item;
}

function isSection(item: MenuItem): item is MenuSectionItem {
  return "section" in item;
}

function isRender(item: MenuItem): item is MenuRenderItem {
  return "render" in item;
}

function isLink(item: MenuItem): item is MenuLinkItem {
  return "href" in item;
}

/** Separators and section headings are unaddressable by design — no id. */
function itemId(item: MenuItem): string | undefined {
  return "id" in item ? item.id : undefined;
}

/** The three-bar glyph, assembled node by node. */
function barsGlyph(): SVGElement {
  const svg = document.createElementNS(SVG_NS, "svg");
  svg.setAttribute("viewBox", "0 0 448 512");
  svg.setAttribute("width", "1em");
  svg.setAttribute("height", "1em");
  svg.setAttribute("fill", "currentColor");
  svg.setAttribute("aria-hidden", "true");
  svg.setAttribute("focusable", "false");
  for (const y of [64, 224, 384]) {
    const bar = document.createElementNS(SVG_NS, "rect");
    bar.setAttribute("x", "0");
    bar.setAttribute("y", String(y));
    bar.setAttribute("width", "448");
    bar.setAttribute("height", "64");
    bar.setAttribute("rx", "32");
    svg.append(bar);
  }
  return svg;
}

/** One item, its element, and the label node the element writes text into. */
interface ItemRecord {
  item: MenuItem;
  el: HTMLElement;
}

/** Every listener this instance added, so destroy() can undo exactly them. */
interface Binding {
  target: EventTarget;
  type: string;
  fn: EventListener;
  capture: boolean;
}

export class HamburgerMenu {
  /** The button that opens the drawer — created here unless adopted. */
  readonly trigger: HTMLElement;
  /** The scrim behind the drawer. Attached in the constructor. */
  readonly backdrop: HTMLElement;
  /** The drawer itself. Attached in the constructor; `aria-controls` names it. */
  readonly drawer: HTMLElement;

  private readonly options: HamburgerMenuOptions;
  private readonly records: ItemRecord[] = [];
  private readonly bindings: Binding[] = [];
  private readonly picker: HTMLElement | null;
  private opened = false;
  private destroyed = false;

  constructor(options: HamburgerMenuOptions) {
    this.options = options;
    const drawerId = `ui-menu-drawer-${++instanceCount}`;

    this.drawer = document.createElement("aside");
    this.drawer.className = "ui-menu-drawer";
    this.drawer.id = drawerId;
    this.drawer.setAttribute("role", "dialog");
    this.drawer.setAttribute("aria-modal", "true");
    this.drawer.setAttribute("aria-label", options.title ?? "Menu");
    this.drawer.tabIndex = -1;

    this.backdrop = document.createElement("div");
    this.backdrop.className = "ui-menu-backdrop";

    if (options.mountTrigger) {
      this.trigger = options.mountTrigger;
    } else {
      const button = document.createElement("button");
      button.type = "button";
      button.className = "ui-menu-trigger";
      button.setAttribute("aria-label", options.title ?? "Menu");
      button.append(barsGlyph());
      this.trigger = button;
    }
    this.trigger.setAttribute("aria-expanded", "false");
    this.trigger.setAttribute("aria-controls", drawerId);
    this.trigger.setAttribute("aria-haspopup", "true");

    document.body.append(this.backdrop);
    document.body.append(this.drawer);

    for (const item of options.items) this.records.push(this.buildRecord(item));
    this.picker = this.buildPicker();

    this.bind(this.trigger, "click", () => this.toggle());
    this.bind(this.backdrop, "mousedown", () => this.close());
    this.bind(document, "keydown", (e) => this.onKeydown(e as KeyboardEvent), true);

    this.sync();
  }

  open(): void {
    if (this.opened || this.destroyed) return;
    this.opened = true;
    this.sync();
    this.paint();
    const focusable = getFocusable(this.drawer);
    (focusable[0] ?? this.drawer).focus();
    this.options.onOpen?.();
  }

  close(): void {
    if (!this.opened) return;
    this.opened = false;
    this.paint();
    this.trigger.focus();
    this.options.onClose?.();
  }

  toggle(): void {
    if (this.opened) this.close();
    else this.open();
  }

  addItem(item: MenuItem): void {
    this.records.push(this.buildRecord(item));
    this.sync();
  }

  removeItem(id: string): void {
    const index = this.records.findIndex((record) => itemId(record.item) === id);
    if (index < 0) return;
    const [record] = this.records.splice(index, 1);
    this.unbindWithin(record.el);
    record.el.remove();
  }

  updateItem(id: string, patch: Partial<MenuItem>): void {
    const record = this.records.find((r) => itemId(r.item) === id);
    if (!record) return;
    Object.assign(record.item, patch);
    this.refresh(record);
    this.sync();
  }

  /**
   * Removes every listener this instance added and detaches its chrome. An
   * adopted `mountTrigger` is left in the page with the three attributes this
   * class set removed; a trigger this class created is detached with the rest.
   */
  destroy(): void {
    if (this.destroyed) return;
    this.destroyed = true;
    this.opened = false;
    for (const binding of this.bindings) {
      binding.target.removeEventListener(binding.type, binding.fn, binding.capture);
    }
    this.bindings.length = 0;
    this.drawer.remove();
    this.backdrop.remove();
    if (this.options.mountTrigger) {
      this.trigger.removeAttribute("aria-expanded");
      this.trigger.removeAttribute("aria-controls");
      this.trigger.removeAttribute("aria-haspopup");
    } else {
      this.trigger.remove();
    }
  }

  // --- internals -------------------------------------------------------------

  private bind(target: EventTarget, type: string, fn: EventListener, capture = false): void {
    target.addEventListener(type, fn, capture);
    this.bindings.push({ target, type, fn, capture });
  }

  private unbindWithin(el: EventTarget): void {
    for (let i = this.bindings.length - 1; i >= 0; i--) {
      const binding = this.bindings[i];
      if (binding.target !== el) continue;
      binding.target.removeEventListener(binding.type, binding.fn, binding.capture);
      this.bindings.splice(i, 1);
    }
  }

  private buildRecord(item: MenuItem): ItemRecord {
    if (isSeparator(item)) {
      const el = document.createElement("div");
      el.className = "ui-menu-separator";
      el.setAttribute("role", "separator");
      return { item, el };
    }

    if (isSection(item)) {
      const el = document.createElement("div");
      el.className = "ui-menu-label";
      el.textContent = item.section;
      return { item, el };
    }

    if (isRender(item)) {
      const el = document.createElement("div");
      el.className = "ui-menu-slot";
      el.id = item.id;
      this.drawer.append(el);
      item.render(el);
      return { item, el };
    }

    if (isLink(item)) {
      const el = document.createElement("a");
      el.className = "ui-menu-link";
      el.href = item.href;
      el.textContent = item.label;
      return { item, el };
    }

    const el = document.createElement("button");
    el.type = "button";
    el.className = "ui-menu-item";
    el.textContent = item.label;
    if (item.icon !== undefined) el.dataset.icon = item.icon;
    this.bind(el, "click", () => {
      item.onSelect();
      this.close();
    });
    return { item, el };
  }

  private refresh(record: ItemRecord): void {
    const item = record.item;
    if (isSeparator(item) || isRender(item)) return;
    if (isSection(item)) {
      record.el.textContent = item.section;
      return;
    }
    record.el.textContent = item.label;
    if (isLink(item)) {
      (record.el as HTMLAnchorElement).href = item.href;
      return;
    }
    if (item.icon !== undefined) record.el.dataset.icon = item.icon;
  }

  private buildPicker(): HTMLElement | null {
    const themes = this.options.themes;
    if (this.options.themePicker !== true || themes === undefined) return null;
    const section = document.createElement("div");
    section.className = "ui-menu-section";
    const label = document.createElement("div");
    label.className = "ui-menu-label";
    label.textContent = "Theme";
    section.append(label);
    themes.renderPicker(section);
    return section;
  }

  /**
   * Re-evaluates every `when()` and re-attaches the visible items in order.
   * Appending a node that is already a child MOVES it, so ordering is restored
   * without detaching anything that stays visible — and the nodes themselves
   * are never recreated, which is what makes a slot's contents survive any
   * number of close/open cycles.
   */
  private sync(): void {
    for (const record of this.records) {
      const guard = record.item.when;
      if (guard === undefined || guard() === true) this.drawer.append(record.el);
      else record.el.remove();
    }
    if (this.picker) this.drawer.append(this.picker);
  }

  private paint(): void {
    this.drawer.className = this.opened ? "ui-menu-drawer is-open" : "ui-menu-drawer";
    this.backdrop.className = this.opened ? "ui-menu-backdrop is-open" : "ui-menu-backdrop";
    this.trigger.setAttribute("aria-expanded", this.opened ? "true" : "false");
  }

  private onKeydown(e: KeyboardEvent): void {
    if (!this.opened) return;
    if (e.key === "Escape") {
      e.preventDefault();
      this.close();
      return;
    }
    if (e.key !== "Tab") return;
    const focusable = getFocusable(this.drawer);
    if (focusable.length === 0) {
      e.preventDefault();
      this.drawer.focus();
      return;
    }
    const first = focusable[0];
    const last = focusable[focusable.length - 1];
    if (e.shiftKey && document.activeElement === first) {
      e.preventDefault();
      last.focus();
    } else if (!e.shiftKey && document.activeElement === last) {
      e.preventDefault();
      first.focus();
    }
  }
}
