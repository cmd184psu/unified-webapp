// web/shared/ts/focusable.ts
var FOCUSABLE_SELECTOR = 'a[href], button:not([disabled]), textarea:not([disabled]), input:not([disabled]), select:not([disabled]), [tabindex]:not([tabindex="-1"])';
function getFocusable(root) {
  return Array.from(root.querySelectorAll(FOCUSABLE_SELECTOR));
}

// web/shared/ts/modal.ts
function openModal(contentEl, opts = {}) {
  const closeOnEscape = opts.closeOnEscape !== false;
  const closeOnOverlayClick = opts.closeOnOverlayClick !== false;
  const previouslyFocused = document.activeElement;
  const overlay = document.createElement("div");
  overlay.className = "ui-modal-overlay";
  const panel = document.createElement("div");
  panel.className = "ui-modal-panel";
  panel.setAttribute("role", "dialog");
  panel.setAttribute("aria-modal", "true");
  panel.tabIndex = -1;
  if (opts.title) {
    const titleEl = document.createElement("h2");
    titleEl.className = "ui-modal-title";
    titleEl.textContent = opts.title;
    panel.appendChild(titleEl);
  }
  panel.appendChild(contentEl);
  overlay.appendChild(panel);
  document.body.appendChild(overlay);
  let closed = false;
  function onKeydown(e) {
    if (e.key === "Escape" && closeOnEscape) {
      e.preventDefault();
      close();
      return;
    }
    if (e.key === "Tab") {
      const focusable2 = getFocusable(panel);
      if (focusable2.length === 0) {
        e.preventDefault();
        panel.focus();
        return;
      }
      const first = focusable2[0];
      const last = focusable2[focusable2.length - 1];
      if (e.shiftKey && document.activeElement === first) {
        e.preventDefault();
        last.focus();
      } else if (!e.shiftKey && document.activeElement === last) {
        e.preventDefault();
        first.focus();
      }
    }
  }
  function onOverlayClick(e) {
    if (closeOnOverlayClick && e.target === overlay) {
      close();
    }
  }
  function close() {
    if (closed) return;
    closed = true;
    document.removeEventListener("keydown", onKeydown, true);
    overlay.removeEventListener("mousedown", onOverlayClick);
    overlay.remove();
    if (previouslyFocused && typeof previouslyFocused.focus === "function") {
      previouslyFocused.focus();
    }
    opts.onClose?.();
  }
  document.addEventListener("keydown", onKeydown, true);
  overlay.addEventListener("mousedown", onOverlayClick);
  const focusable = getFocusable(panel);
  (focusable[0] ?? panel).focus();
  return { overlay, panel, close };
}
function confirmDialog(message, opts = {}) {
  return new Promise((resolve) => {
    let settled = false;
    const content = document.createElement("div");
    const msg = document.createElement("p");
    msg.className = "ui-modal-message";
    msg.textContent = message;
    content.appendChild(msg);
    const actions = document.createElement("div");
    actions.className = "ui-modal-actions";
    const cancelBtn = document.createElement("button");
    cancelBtn.type = "button";
    cancelBtn.className = "ui-modal-btn";
    cancelBtn.textContent = opts.cancelLabel ?? "Cancel";
    const okBtn = document.createElement("button");
    okBtn.type = "button";
    okBtn.className = "ui-modal-btn ui-modal-btn-primary";
    okBtn.textContent = opts.confirmLabel ?? "OK";
    actions.appendChild(cancelBtn);
    actions.appendChild(okBtn);
    content.appendChild(actions);
    const handle = openModal(content, {
      title: opts.title,
      onClose: () => {
        if (!settled) {
          settled = true;
          resolve(false);
        }
      }
    });
    cancelBtn.addEventListener("click", () => {
      settled = true;
      resolve(false);
      handle.close();
    });
    okBtn.addEventListener("click", () => {
      settled = true;
      resolve(true);
      handle.close();
    });
  });
}
function alertDialog(message, opts = {}) {
  return new Promise((resolve) => {
    let settled = false;
    const content = document.createElement("div");
    const msg = document.createElement("p");
    msg.className = "ui-modal-message";
    msg.textContent = message;
    content.appendChild(msg);
    const actions = document.createElement("div");
    actions.className = "ui-modal-actions";
    const okBtn = document.createElement("button");
    okBtn.type = "button";
    okBtn.className = "ui-modal-btn ui-modal-btn-primary";
    okBtn.textContent = opts.confirmLabel ?? "OK";
    actions.appendChild(okBtn);
    content.appendChild(actions);
    const handle = openModal(content, {
      title: opts.title,
      onClose: () => {
        if (!settled) {
          settled = true;
          resolve();
        }
      }
    });
    okBtn.addEventListener("click", () => {
      settled = true;
      resolve();
      handle.close();
    });
  });
}
function promptDialog(message, opts = {}) {
  return new Promise((resolve) => {
    let settled = false;
    const content = document.createElement("div");
    const msg = document.createElement("p");
    msg.className = "ui-modal-message";
    msg.textContent = message;
    content.appendChild(msg);
    const input = document.createElement("input");
    input.type = "text";
    input.className = "ui-modal-input";
    input.value = opts.defaultValue ?? "";
    if (opts.placeholder) input.placeholder = opts.placeholder;
    content.appendChild(input);
    const actions = document.createElement("div");
    actions.className = "ui-modal-actions";
    const cancelBtn = document.createElement("button");
    cancelBtn.type = "button";
    cancelBtn.className = "ui-modal-btn";
    cancelBtn.textContent = opts.cancelLabel ?? "Cancel";
    const okBtn = document.createElement("button");
    okBtn.type = "button";
    okBtn.className = "ui-modal-btn ui-modal-btn-primary";
    okBtn.textContent = opts.confirmLabel ?? "OK";
    actions.appendChild(cancelBtn);
    actions.appendChild(okBtn);
    content.appendChild(actions);
    const handle = openModal(content, {
      title: opts.title,
      onClose: () => {
        if (!settled) {
          settled = true;
          resolve(null);
        }
      }
    });
    function submit() {
      settled = true;
      resolve(input.value);
      handle.close();
    }
    input.addEventListener("keydown", (e) => {
      if (e.key === "Enter") {
        e.preventDefault();
        submit();
      }
    });
    cancelBtn.addEventListener("click", () => {
      settled = true;
      resolve(null);
      handle.close();
    });
    okBtn.addEventListener("click", submit);
    input.focus();
    input.select();
  });
}

// web/shared/ts/theme.ts
var THEMES = [
  "dark",
  "light",
  "obsidian",
  "forest",
  "ocean",
  "ember",
  "rose",
  "puma"
];
function setTheme(name) {
  document.documentElement.dataset.theme = name;
}
var ThemeManager = class {
  constructor(options) {
    /** The roster pickers enumerate. Deliberately the same set as THEMES and
     * nothing more: `system` is the resolver's implicit step, not an offerable
     * name — absent here, absent from THEMES, and rejected on read-back. */
    this.list = THEMES;
    /**
     * One row set per picker this instance has rendered, because one instance
     * may render several and they are views of one state. Kept on the instance
     * because the active mark has to follow every theme change, including ones
     * made from outside a picker — set() from another picker, reresolve() once
     * serverDefault() becomes answerable, and the system `change` listener.
     */
    this.pickers = [];
    this.options = options;
    this.media = matchMedia("(prefers-color-scheme: dark)");
    this.media.addEventListener("change", () => {
      if (this.storedTheme() !== void 0) return;
      if (this.options.serverDefault?.() !== void 0) return;
      this.apply();
    });
  }
  /** Applies the resolved theme. Callable pre-paint, idempotent, and silent. */
  apply() {
    this.stamp(this.resolve());
  }
  /**
   * Re-runs the resolution order against the closures' current values and
   * applies the result, without writing storage. The separate name is the
   * entry point obsidianoid's fetchVaults/switchVault call when the per-vault
   * storage key changes under a live instance. Not writing is the load-bearing
   * half — a writing reresolve() would overwrite the destination vault's saved
   * choice with the source vault's.
   */
  reresolve() {
    this.apply();
  }
  /** Writes storage, applies, and fires onChange. */
  set(name) {
    localStorage.setItem(this.storageKey(), name);
    this.stamp(name);
    this.options.onChange?.(name);
  }
  /**
   * The one place a theme becomes the applied theme: apply() and set() both
   * route through here, so a change made anywhere re-marks every rendered
   * picker. setTheme stays the single definition of the DOM write; this adds
   * the mark beside it, and nothing else.
   */
  stamp(name) {
    setTheme(name);
    this.mark(name);
  }
  mark(name) {
    for (const rows of this.pickers) {
      for (const row of rows) {
        row.button.className = row.name === name ? "ui-theme-btn is-active" : "ui-theme-btn";
      }
    }
  }
  /**
   * Renders the shared swatch picker into `host`. Every node is built through
   * createElement and every label is a text node — no markup string is assigned
   * anywhere in this file. No colour value reaches this file either: each
   * swatch carries `data-theme`, and themes.css keys every palette on a bare
   * attribute selector.
   */
  renderPicker(host) {
    const picker = document.createElement("div");
    picker.className = "ui-theme-picker";
    const rows = [];
    for (const name of this.list) {
      const button = document.createElement("button");
      button.type = "button";
      button.className = "ui-theme-btn";
      const swatch = document.createElement("span");
      swatch.className = "ui-theme-swatch";
      swatch.dataset.theme = name;
      button.append(swatch, name);
      button.addEventListener("click", () => this.set(name));
      rows.push({ name, button });
      picker.append(button);
    }
    host.append(picker);
    this.pickers.push(rows);
    this.mark(this.resolve());
  }
  storageKey() {
    const override = this.options.storageKey;
    return override ? override() : `ui-theme:${this.options.module}`;
  }
  /**
   * Step 1. Every value read from storage is validated against THEMES before
   * use, so an unknown stored string falls through to the next step rather
   * than stamping a nonexistent theme.
   */
  storedTheme() {
    const raw = localStorage.getItem(this.storageKey());
    if (raw === null) return void 0;
    return THEMES.includes(raw) ? raw : void 0;
  }
  /** Step 3. `matches` → dark; everything else → light. Always resolves. */
  systemTheme() {
    return this.media.matches ? "dark" : "light";
  }
  /**
   * Resolution order: localStorage → serverDefault() → system → default.
   * Step 3 always answers, which makes the trailing `?? this.options.default`
   * a typed-config floor — present, in order, and unreachable at runtime.
   */
  resolve() {
    const stored = this.storedTheme();
    if (stored !== void 0) return stored;
    const server = this.options.serverDefault?.();
    if (server !== void 0) return server;
    return this.systemTheme() ?? this.options.default;
  }
};

// web/shared/ts/toast.ts
var DEFAULT_DURATION_MS = {
  success: 6e3,
  notice: 8e3,
  error: 0
};
var stack = null;
function ensureStack() {
  if (stack && document.body.contains(stack)) return stack;
  stack = document.createElement("div");
  stack.className = "ui-toast-stack";
  stack.setAttribute("role", "status");
  stack.setAttribute("aria-live", "polite");
  document.body.append(stack);
  return stack;
}
function showToast(message, tone = "notice", durationMs = DEFAULT_DURATION_MS[tone]) {
  const container = ensureStack();
  const node = document.createElement("div");
  node.className = "ui-toast";
  node.dataset.tone = tone;
  const text = document.createElement("p");
  text.className = "ui-toast-text";
  text.textContent = message;
  const close = document.createElement("button");
  close.type = "button";
  close.className = "ui-toast-close";
  close.textContent = "\xD7";
  close.setAttribute("aria-label", "Dismiss");
  node.append(text, close);
  container.append(node);
  let timer = null;
  let dismissed = false;
  const dismiss = () => {
    if (dismissed) return;
    dismissed = true;
    if (timer !== null) clearTimeout(timer);
    node.remove();
  };
  close.addEventListener("click", dismiss);
  if (durationMs > 0) timer = setTimeout(dismiss, durationMs);
  return { dismiss };
}

// web/shared/ts/menu.ts
var SVG_NS = "http://www.w3.org/2000/svg";
var instanceCount = 0;
function isSeparator(item) {
  return "separator" in item;
}
function isSection(item) {
  return "section" in item;
}
function isRender(item) {
  return "render" in item;
}
function isLink(item) {
  return "href" in item;
}
function itemId(item) {
  return "id" in item ? item.id : void 0;
}
function barsGlyph() {
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
var HamburgerMenu = class {
  constructor(options) {
    this.records = [];
    this.bindings = [];
    this.opened = false;
    this.destroyed = false;
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
    this.bind(document, "keydown", (e) => this.onKeydown(e), true);
    this.sync();
  }
  open() {
    if (this.opened || this.destroyed) return;
    this.opened = true;
    this.sync();
    this.paint();
    const focusable = getFocusable(this.drawer);
    (focusable[0] ?? this.drawer).focus();
    this.options.onOpen?.();
  }
  close() {
    if (!this.opened) return;
    this.opened = false;
    this.paint();
    this.trigger.focus();
    this.options.onClose?.();
  }
  toggle() {
    if (this.opened) this.close();
    else this.open();
  }
  addItem(item) {
    this.records.push(this.buildRecord(item));
    this.sync();
  }
  removeItem(id) {
    const index = this.records.findIndex((record2) => itemId(record2.item) === id);
    if (index < 0) return;
    const [record] = this.records.splice(index, 1);
    this.unbindWithin(record.el);
    record.el.remove();
  }
  updateItem(id, patch) {
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
  destroy() {
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
  bind(target, type, fn, capture = false) {
    target.addEventListener(type, fn, capture);
    this.bindings.push({ target, type, fn, capture });
  }
  unbindWithin(el) {
    for (let i = this.bindings.length - 1; i >= 0; i--) {
      const binding = this.bindings[i];
      if (binding.target !== el) continue;
      binding.target.removeEventListener(binding.type, binding.fn, binding.capture);
      this.bindings.splice(i, 1);
    }
  }
  buildRecord(item) {
    if (isSeparator(item)) {
      const el2 = document.createElement("div");
      el2.className = "ui-menu-separator";
      el2.setAttribute("role", "separator");
      return { item, el: el2 };
    }
    if (isSection(item)) {
      const el2 = document.createElement("div");
      el2.className = "ui-menu-label";
      el2.textContent = item.section;
      return { item, el: el2 };
    }
    if (isRender(item)) {
      const el2 = document.createElement("div");
      el2.className = "ui-menu-slot";
      el2.id = item.id;
      this.drawer.append(el2);
      item.render(el2);
      return { item, el: el2 };
    }
    if (isLink(item)) {
      const el2 = document.createElement("a");
      el2.className = "ui-menu-link";
      el2.href = item.href;
      el2.textContent = item.label;
      return { item, el: el2 };
    }
    const el = document.createElement("button");
    el.type = "button";
    el.className = "ui-menu-item";
    el.textContent = item.label;
    if (item.icon !== void 0) el.dataset.icon = item.icon;
    this.bind(el, "click", () => {
      item.onSelect();
      this.close();
    });
    return { item, el };
  }
  refresh(record) {
    const item = record.item;
    if (isSeparator(item) || isRender(item)) return;
    if (isSection(item)) {
      record.el.textContent = item.section;
      return;
    }
    record.el.textContent = item.label;
    if (isLink(item)) {
      record.el.href = item.href;
      return;
    }
    if (item.icon !== void 0) record.el.dataset.icon = item.icon;
  }
  buildPicker() {
    const themes = this.options.themes;
    if (this.options.themePicker !== true || themes === void 0) return null;
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
  sync() {
    for (const record of this.records) {
      const guard = record.item.when;
      if (guard === void 0 || guard() === true) this.drawer.append(record.el);
      else record.el.remove();
    }
    if (this.picker) this.drawer.append(this.picker);
  }
  paint() {
    this.drawer.className = this.opened ? "ui-menu-drawer is-open" : "ui-menu-drawer";
    this.backdrop.className = this.opened ? "ui-menu-backdrop is-open" : "ui-menu-backdrop";
    this.trigger.setAttribute("aria-expanded", this.opened ? "true" : "false");
  }
  onKeydown(e) {
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
};
export {
  HamburgerMenu,
  THEMES,
  ThemeManager,
  alertDialog,
  confirmDialog,
  openModal,
  promptDialog,
  setTheme,
  showToast
};
