// web/shared/ts/focusable.ts
var FOCUSABLE_SELECTOR = 'a[href], button:not([disabled]), textarea:not([disabled]), input:not([disabled]), select:not([disabled]), [tabindex]:not([tabindex="-1"])';
function getFocusable(root) {
  return Array.from(root.querySelectorAll(FOCUSABLE_SELECTOR));
}

// web/shared/ts/modal.ts
function openModal(contentEl, opts = {}) {
  const defaultCloseOnEscape = opts.closeOnEscape !== false;
  const defaultCloseOnOverlayClick = opts.closeOnOverlayClick !== false;
  let closeOnEscape = defaultCloseOnEscape;
  let closeOnOverlayClick = defaultCloseOnOverlayClick;
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
  function setClosable(closable) {
    if (closable) {
      closeOnEscape = defaultCloseOnEscape;
      closeOnOverlayClick = defaultCloseOnOverlayClick;
    } else {
      closeOnEscape = false;
      closeOnOverlayClick = false;
    }
  }
  return { overlay, panel, close, setClosable };
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
    this.adopt(name);
    this.options.onChange?.(name);
  }
  /**
   * Writes storage and applies, WITHOUT firing onChange: for a theme that
   * arrives from elsewhere (slideshow's server state). Using set() there
   * would echo the theme back to its source, which answers with the same
   * state again -- an endless loop.
   */
  adopt(name) {
    localStorage.setItem(this.storageKey(), name);
    this.stamp(name);
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
    for (const marker of this.pickers) marker(name);
  }
  /**
   * Renders the shared theme picker into `host`. Every node is built through
   * createElement and every label is a text node — no markup string is assigned
   * anywhere in this file. No color value reaches this file either: each
   * swatch carries `data-theme`, and themes.css keys every palette on a bare
   * attribute selector.
   *
   * `mode` is a hint from the caller about the space it has: "list" (the
   * default) is one swatch button per theme, for a picker that stands alone;
   * "select" is a compact dropdown, for a picker sharing its space with other
   * settings. HamburgerMenu chooses it from what else the drawer holds.
   */
  renderPicker(host, mode = "list") {
    if (mode === "select") {
      const select = document.createElement("select");
      select.className = "ui-theme-select";
      select.setAttribute("aria-label", "Theme");
      for (const name of this.list) {
        const option = document.createElement("option");
        option.value = name;
        option.append(name);
        select.append(option);
      }
      select.addEventListener("change", () => this.set(select.value));
      host.append(select);
      this.pickers.push((name) => {
        select.value = name;
      });
      this.mark(this.resolve());
      return;
    }
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
    this.pickers.push((name) => {
      for (const row of rows) {
        row.button.className = row.name === name ? "ui-theme-btn is-active" : "ui-theme-btn";
      }
    });
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

// web/shared/ts/session.ts
var SVG_NS = "http://www.w3.org/2000/svg";
function doorGlyph() {
  const svg = document.createElementNS(SVG_NS, "svg");
  svg.setAttribute("viewBox", "0 0 24 24");
  svg.setAttribute("width", "1em");
  svg.setAttribute("height", "1em");
  svg.setAttribute("fill", "none");
  svg.setAttribute("stroke", "currentColor");
  svg.setAttribute("stroke-width", "2");
  svg.setAttribute("stroke-linecap", "round");
  svg.setAttribute("stroke-linejoin", "round");
  svg.setAttribute("aria-hidden", "true");
  svg.setAttribute("focusable", "false");
  const door = document.createElementNS(SVG_NS, "path");
  door.setAttribute("d", "M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4");
  const head = document.createElementNS(SVG_NS, "polyline");
  head.setAttribute("points", "16 17 21 12 16 7");
  const shaft = document.createElementNS(SVG_NS, "line");
  shaft.setAttribute("x1", "21");
  shaft.setAttribute("y1", "12");
  shaft.setAttribute("x2", "9");
  shaft.setAttribute("y2", "12");
  svg.append(door, head, shaft);
  return svg;
}
function buildSignOutButton() {
  const btn = document.createElement("button");
  btn.type = "button";
  btn.className = "ui-menu-trigger ui-signout";
  btn.title = "Sign out";
  btn.setAttribute("aria-label", "Sign out");
  btn.append(doorGlyph());
  btn.addEventListener("click", () => {
    btn.disabled = true;
    void fetch("/api/auth/logout", { method: "POST" }).catch(() => void 0).then(() => window.location.reload());
  });
  return btn;
}
async function sessionState(method) {
  if (typeof fetch !== "function") return { kind: "unknown" };
  const url = method === "GET" ? "/api/auth/session" : "/api/auth/activity";
  try {
    const res = await fetch(url, { method, headers: { Accept: "application/json" } });
    if (res.status === 401) return { kind: "signed-out" };
    if (!res.ok) return { kind: "unknown" };
    const body = await res.json();
    if (!Array.isArray(body.methods)) return { kind: "unknown" };
    return { kind: "signed-in", idleSeconds: typeof body.idleSeconds === "number" ? body.idleSeconds : 0 };
  } catch {
    return { kind: "unknown" };
  }
}
var REPORT_EVERY_MS = 6e4;
var RECHECK_CAP_MS = 5 * 6e4;
var ACTIVITY_EVENTS = ["pointerdown", "keydown", "wheel", "touchstart"];
function watchFetch(onUnauthorized) {
  if (typeof window.fetch !== "function") return () => void 0;
  const original = window.fetch;
  const wrapped = async (input, init) => {
    const res = await original.call(window, input, init);
    if (res.status === 401) {
      const raw = typeof input === "string" ? input : input instanceof URL ? input.href : input.url;
      const url = new URL(raw, window.location.href);
      if (url.origin === window.location.origin && !url.pathname.startsWith("/api/auth/")) onUnauthorized();
    }
    return res;
  };
  window.fetch = wrapped;
  return () => {
    if (window.fetch === wrapped) window.fetch = original;
  };
}
function watchIdle(initialIdleSeconds) {
  let stopped = false;
  let timer;
  let lastReport = Date.now();
  let pendingUse = false;
  const signedOut = () => {
    if (stopped) return;
    stop();
    showToast("Your session has ended. Taking you to sign in\u2026", "notice");
    setTimeout(() => window.location.reload(), 1500);
  };
  const unwatchFetch = watchFetch(() => signedOut());
  const schedule = (idleSeconds) => {
    if (stopped) return;
    clearTimeout(timer);
    const due = Math.min(Math.max(idleSeconds * 1e3 + 1e3, 5e3), RECHECK_CAP_MS);
    timer = setTimeout(() => void check(), due);
  };
  const apply = (state) => {
    if (state.kind === "signed-out") signedOut();
    else if (state.kind === "signed-in") schedule(state.idleSeconds);
    else schedule(60);
  };
  const check = async () => {
    const report = pendingUse;
    pendingUse = false;
    if (report) lastReport = Date.now();
    apply(await sessionState(report ? "POST" : "GET"));
  };
  const onUse = () => {
    if (Date.now() - lastReport >= REPORT_EVERY_MS) {
      lastReport = Date.now();
      pendingUse = false;
      void sessionState("POST").then(apply);
    } else {
      pendingUse = true;
    }
  };
  const onVisible = () => {
    if (document.visibilityState === "visible") void check();
  };
  for (const type of ACTIVITY_EVENTS) window.addEventListener(type, onUse, { capture: true, passive: true });
  document.addEventListener("visibilitychange", onVisible);
  schedule(initialIdleSeconds);
  function stop() {
    stopped = true;
    unwatchFetch();
    clearTimeout(timer);
    for (const type of ACTIVITY_EVENTS) window.removeEventListener(type, onUse, { capture: true });
    document.removeEventListener("visibilitychange", onVisible);
  }
  return stop;
}
function mountSignOut(trigger) {
  let canceled = false;
  let group = null;
  let observer = null;
  const place = () => {
    if (canceled || !trigger.isConnected) return false;
    group = document.createElement("span");
    group.className = "ui-menu-actions";
    trigger.before(group);
    group.append(buildSignOutButton(), trigger);
    return true;
  };
  let stopWatching = null;
  void sessionState("GET").then((state) => {
    if (state.kind !== "signed-in" || canceled) return;
    stopWatching = watchIdle(state.idleSeconds);
    if (place() || typeof MutationObserver !== "function") return;
    observer = new MutationObserver(() => {
      if (place()) observer?.disconnect();
    });
    observer.observe(document.body, { childList: true, subtree: true });
  });
  return () => {
    canceled = true;
    observer?.disconnect();
    stopWatching?.();
    if (group) {
      group.before(trigger);
      group.remove();
    }
  };
}

// web/shared/ts/menu.ts
var SVG_NS2 = "http://www.w3.org/2000/svg";
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
function closeGlyph() {
  const svg = document.createElementNS(SVG_NS2, "svg");
  svg.setAttribute("viewBox", "0 0 24 24");
  svg.setAttribute("width", "1em");
  svg.setAttribute("height", "1em");
  svg.setAttribute("fill", "none");
  svg.setAttribute("aria-hidden", "true");
  svg.setAttribute("focusable", "false");
  for (const d of ["M6 6L18 18", "M6 18L18 6"]) {
    const line = document.createElementNS(SVG_NS2, "path");
    line.setAttribute("d", d);
    line.setAttribute("stroke", "currentColor");
    line.setAttribute("stroke-width", "2");
    line.setAttribute("fill", "none");
    svg.append(line);
  }
  return svg;
}
function barsGlyph() {
  const svg = document.createElementNS(SVG_NS2, "svg");
  svg.setAttribute("viewBox", "0 0 448 512");
  svg.setAttribute("width", "1em");
  svg.setAttribute("height", "1em");
  svg.setAttribute("fill", "currentColor");
  svg.setAttribute("aria-hidden", "true");
  svg.setAttribute("focusable", "false");
  for (const y of [64, 224, 384]) {
    const bar = document.createElementNS(SVG_NS2, "rect");
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
    /** Removes the sign-out button (or cancels its pending mount). */
    this.unmountSignOut = null;
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
    if (options.side !== void 0) this.drawer.dataset.side = options.side;
    const header = document.createElement("div");
    header.className = "ui-menu-header";
    const closeButton = document.createElement("button");
    closeButton.type = "button";
    closeButton.className = "ui-menu-close";
    closeButton.setAttribute("aria-label", "Close menu");
    closeButton.append(closeGlyph());
    header.append(closeButton);
    if (options.title !== void 0) {
      const titleEl = document.createElement("span");
      titleEl.className = "ui-menu-title";
      titleEl.textContent = options.title;
      header.append(titleEl);
    }
    this.drawer.append(header);
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
    if (options.signOut !== false) this.unmountSignOut = mountSignOut(this.trigger);
    this.bind(this.trigger, "click", () => this.toggle());
    this.bind(this.backdrop, "mousedown", () => this.close());
    this.bind(document, "keydown", (e) => this.onKeydown(e), true);
    this.bind(closeButton, "click", () => this.close());
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
    this.unmountSignOut?.();
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
  unbindWithin(el2) {
    for (let i = this.bindings.length - 1; i >= 0; i--) {
      const binding = this.bindings[i];
      if (binding.target !== el2) continue;
      binding.target.removeEventListener(binding.type, binding.fn, binding.capture);
      this.bindings.splice(i, 1);
    }
  }
  buildRecord(item) {
    if (isSeparator(item)) {
      const el3 = document.createElement("div");
      el3.className = "ui-menu-separator";
      el3.setAttribute("role", "separator");
      return { item, el: el3 };
    }
    if (isSection(item)) {
      const el3 = document.createElement("div");
      el3.className = "ui-menu-label";
      el3.textContent = item.section;
      return { item, el: el3 };
    }
    if (isRender(item)) {
      const el3 = document.createElement("div");
      el3.className = "ui-menu-slot";
      el3.id = item.id;
      this.drawer.append(el3);
      item.render(el3);
      return { item, el: el3 };
    }
    if (isLink(item)) {
      const el3 = document.createElement("a");
      el3.className = "ui-menu-link";
      el3.href = item.href;
      el3.textContent = item.label;
      return { item, el: el3 };
    }
    const el2 = document.createElement("button");
    el2.type = "button";
    el2.className = "ui-menu-item";
    el2.textContent = item.label;
    if (item.icon !== void 0) el2.dataset.icon = item.icon;
    this.bind(el2, "click", () => {
      item.onSelect();
      this.close();
    });
    return { item, el: el2 };
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
    const shared = this.options.items.some((item) => !isSeparator(item) && !isSection(item));
    themes.renderPicker(section, shared ? "select" : "list");
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

// web/shared/ts/icons.ts
var SVG_NS3 = "http://www.w3.org/2000/svg";
function icon(shapes, filled = false) {
  const svg = document.createElementNS(SVG_NS3, "svg");
  const base = {
    viewBox: "0 0 24 24",
    width: "1em",
    height: "1em",
    "aria-hidden": "true",
    focusable: "false"
  };
  const paint = filled ? { fill: "currentColor" } : { fill: "none", stroke: "currentColor", "stroke-width": "2", "stroke-linecap": "round", "stroke-linejoin": "round" };
  for (const [k, v] of Object.entries({ ...base, ...paint })) svg.setAttribute(k, v);
  for (const [tag, attrs] of shapes) {
    const node = document.createElementNS(SVG_NS3, tag);
    for (const [k, v] of Object.entries(attrs)) node.setAttribute(k, v);
    svg.append(node);
  }
  return svg;
}
var copyIcon = () => icon([
  ["rect", { x: "9", y: "9", width: "13", height: "13", rx: "2" }],
  ["path", { d: "M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1" }]
]);
var checkIcon = () => icon([["polyline", { points: "20 6 9 17 4 12" }]]);
var chevronIcon = () => icon([["polyline", { points: "9 18 15 12 9 6" }]]);
var folderIcon = () => icon([["path", { d: "M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z" }]]);
var fileIcon = () => icon([
  ["path", { d: "M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z" }],
  ["polyline", { points: "14 2 14 8 20 8" }]
]);
var editIcon = () => icon([["path", { d: "M17 3a2.83 2.83 0 1 1 4 4L7.5 20.5 2 22l1.5-5.5Z" }]]);
var trashIcon = () => icon([
  ["polyline", { points: "3 6 5 6 21 6" }],
  ["path", { d: "M19 6l-1 14a2 2 0 0 1-2 2H8a2 2 0 0 1-2-2L5 6" }],
  ["path", { d: "M10 11v6" }],
  ["path", { d: "M14 11v6" }],
  ["path", { d: "M9 6V4a1 1 0 0 1 1-1h4a1 1 0 0 1 1 1v2" }]
]);
var gripIcon = () => icon(
  [
    [7, 5],
    [17, 5],
    [7, 12],
    [17, 12],
    [7, 19],
    [17, 19]
  ].map(([cx, cy]) => ["circle", { cx: String(cx), cy: String(cy), r: "2" }]),
  true
);

// web/shared/ts/clipboard.ts
async function copyText(text) {
  try {
    if (navigator.clipboard && window.isSecureContext) {
      await navigator.clipboard.writeText(text);
      return true;
    }
  } catch {
  }
  try {
    const ta = document.createElement("textarea");
    ta.value = text;
    ta.setAttribute("readonly", "");
    ta.style.position = "fixed";
    ta.style.opacity = "0";
    document.body.append(ta);
    ta.select();
    const ok = document.execCommand("copy");
    ta.remove();
    return ok;
  } catch {
    return false;
  }
}
function createCopyButton(options) {
  const btn = document.createElement("button");
  btn.type = "button";
  btn.className = options.className ? `ui-copy-btn ${options.className}` : "ui-copy-btn";
  btn.title = `Copy ${options.label}`;
  btn.setAttribute("aria-label", `Copy ${options.label}`);
  btn.append(copyIcon());
  let reset;
  btn.addEventListener("click", () => {
    void copyText(options.text()).then((ok) => {
      showToast(ok ? "Copied!" : "Copy failed. Select the text and copy it by hand.", ok ? "success" : "error");
      if (!ok) return;
      btn.replaceChildren(checkIcon());
      clearTimeout(reset);
      reset = setTimeout(() => btn.replaceChildren(copyIcon()), 1200);
    });
  });
  return btn;
}

// web/shared/ts/filetree.ts
var byName = (a, b) => a.name.localeCompare(b.name, void 0, { sensitivity: "base", numeric: true });
function el(tag, className) {
  const node = document.createElement(tag);
  node.className = className;
  return node;
}
var FileTree = class {
  constructor(options) {
    this.entries = [];
    /** Folders opened or closed by the user, overriding openFolders. */
    this.open = /* @__PURE__ */ new Map();
    this.byPath = /* @__PURE__ */ new Map();
    this.parentOf = /* @__PURE__ */ new Map();
    this.loading = /* @__PURE__ */ new Set();
    this.errors = /* @__PURE__ */ new Map();
    this.selectedPath = null;
    this.topLoaded = false;
    /** The last row click, to spot a double-click even if the row was redrawn in between. */
    this.lastClick = { path: "", at: 0 };
    /** Open folders met while rendering that still need loading; started after. */
    this.queued = [];
    this.options = options;
    this.el = el("div", "ui-tree");
    this.el.setAttribute("role", "tree");
    this.el.setAttribute("aria-label", options.label);
    this.el.addEventListener("click", (e) => this.onClick(e));
    this.el.addEventListener("keydown", (e) => this.onKeydown(e));
    if (options.entries) {
      this.entries = options.entries;
      this.topLoaded = true;
      this.render();
    } else {
      void this.loadTop();
    }
  }
  /** Replaces the top-level entries (open/closed folders are kept). */
  setEntries(entries) {
    this.entries = entries;
    this.topLoaded = true;
    this.render();
  }
  /** Changes options (sort, filter, activePath, locked, …) and redraws. */
  update(changes) {
    this.options = { ...this.options, ...changes };
    this.render();
  }
  /** The selected entry, in a picker. */
  get selected() {
    return this.selectedPath === null ? null : this.byPath.get(this.selectedPath) ?? null;
  }
  /** Reloads a folder's contents (or, with no path, the top level). */
  async reload(path) {
    if (path === void 0) {
      await this.loadTop();
      return;
    }
    const entry = this.byPath.get(path);
    if (entry && entry.isDir) {
      entry.children = void 0;
      await this.loadChildren(entry);
    }
  }
  /**
   * Opens the folders down to `path`, loading them as needed, then selects
   * it (in a picker) and scrolls it into view. Paths nest by prefix: a
   * folder "a/b" (or "/a") contains "a/b/c" (or "/a/b").
   */
  async reveal(path) {
    if (!this.topLoaded) await this.loadTop();
    let level = this.entries;
    for (; ; ) {
      const hit = level.find((e) => e.path === path || e.isDir && contains(e.path, path));
      if (!hit) break;
      if (hit.path === path) {
        this.choose(hit, false);
        break;
      }
      this.open.set(hit.path, true);
      if (hit.children === void 0) await this.loadChildren(hit);
      level = hit.children ?? [];
    }
    this.render();
    this.rowFor(path)?.scrollIntoView({ block: "nearest" });
  }
  /** Focuses the selected, active or first row. */
  focus() {
    const row = this.selectedPath && this.rowFor(this.selectedPath) || this.options.activePath && this.rowFor(this.options.activePath) || this.rows()[0];
    row?.focus();
  }
  // --- loading ---------------------------------------------------------------
  async loadTop() {
    if (!this.options.load) return;
    this.loading.add("");
    this.render();
    try {
      this.entries = await this.options.load(null);
      this.errors.delete("");
    } catch (err) {
      this.errors.set("", err instanceof Error ? err.message : String(err));
    } finally {
      this.loading.delete("");
      this.topLoaded = true;
      this.render();
    }
  }
  async loadChildren(entry) {
    if (!this.options.load || this.loading.has(entry.path)) return;
    this.loading.add(entry.path);
    this.render();
    try {
      entry.children = await this.options.load(entry);
      this.errors.delete(entry.path);
    } catch (err) {
      this.errors.set(entry.path, err instanceof Error ? err.message : String(err));
    } finally {
      this.loading.delete(entry.path);
      this.render();
    }
  }
  // --- rendering ---------------------------------------------------------------
  isOpen(entry) {
    if (entry.leaf) return false;
    if (this.options.filter) return true;
    const set = this.open.get(entry.path);
    if (set !== void 0) return set;
    const all = this.options.openFolders ?? (this.options.load ? "none" : "all");
    return all === "all";
  }
  visible(entry) {
    const filter = this.options.filter;
    if (!filter) return true;
    if (!entry.isDir) return filter(entry);
    return entry.children === void 0 || entry.children.some((c) => this.visible(c));
  }
  sorted(entries) {
    const recent = this.options.sort === "recent";
    return [...entries].sort((a, b) => {
      if (recent) return (b.mtime ?? 0) - (a.mtime ?? 0) || byName(a, b);
      if (a.isDir !== b.isDir) return a.isDir ? -1 : 1;
      return byName(a, b);
    });
  }
  render() {
    const active = document.activeElement;
    const focusedPath = active && this.el.contains(active) ? active.closest(".ui-tree-row")?.dataset.path : void 0;
    this.byPath.clear();
    this.parentOf.clear();
    const frag = document.createDocumentFragment();
    if (this.loading.has("")) {
      frag.append(this.noteRow("Loading\u2026", 0));
    } else if (this.errors.has("")) {
      frag.append(this.noteRow(this.errors.get(""), 0, true));
    } else {
      const shown = this.appendLevel(frag, this.entries, 0, this.options.rootPath ?? "");
      if (!shown && this.topLoaded) frag.append(this.noteRow(this.options.emptyText ?? "Nothing here.", 0));
    }
    this.el.replaceChildren(frag);
    this.el.classList.toggle("is-locked", this.isLocked());
    const tabRow = focusedPath && this.rowFor(focusedPath) || this.selectedPath && this.rowFor(this.selectedPath) || this.options.activePath && this.rowFor(this.options.activePath) || this.rows()[0];
    if (tabRow) tabRow.tabIndex = 0;
    if (focusedPath && tabRow && tabRow.dataset.path === focusedPath) tabRow.focus();
    for (const entry of this.queued.splice(0)) void this.loadChildren(entry);
  }
  /** Appends one level's rows; returns how many were shown. */
  appendLevel(parent, entries, depth, parentPath) {
    let shown = 0;
    for (const entry of this.sorted(entries)) {
      this.byPath.set(entry.path, entry);
      this.parentOf.set(entry.path, parentPath);
      if (!this.visible(entry)) continue;
      shown++;
      const row = this.row(entry, depth);
      if (!entry.isDir) {
        parent.appendChild(row);
        continue;
      }
      const wrap = el("div", "ui-tree-branch");
      wrap.append(row);
      if (this.isOpen(entry)) {
        const group = el("div", "ui-tree-group");
        group.setAttribute("role", "group");
        if (this.loading.has(entry.path) || entry.children === void 0 && this.options.load && !this.errors.has(entry.path)) {
          group.append(this.noteRow("Loading\u2026", depth + 1));
          if (!this.loading.has(entry.path)) this.queued.push(entry);
        } else if (this.errors.has(entry.path)) {
          group.append(this.noteRow(this.errors.get(entry.path), depth + 1, true));
        } else if (entry.children === void 0) {
          if (!this.options.filter) group.append(this.noteRow("Empty", depth + 1));
        } else if (entry.children.length === 0) {
          if (!this.options.filter) group.append(this.noteRow("Empty", depth + 1));
        } else {
          this.appendLevel(group, entry.children, depth + 1, entry.path);
        }
        wrap.append(group);
      }
      parent.appendChild(wrap);
    }
    return shown;
  }
  row(entry, depth) {
    const row = el("div", "ui-tree-row");
    row.setAttribute("role", "treeitem");
    row.setAttribute("aria-level", String(depth + 1));
    row.tabIndex = -1;
    row.dataset.path = entry.path;
    row.style.setProperty("--ui-tree-depth", String(depth));
    row.title = entry.title ?? entry.path;
    if (entry.isDir) {
      const open = this.isOpen(entry);
      row.classList.add("is-dir");
      row.classList.toggle("is-open", open);
      if (!entry.leaf) row.setAttribute("aria-expanded", String(open));
    }
    if (entry.path === this.options.activePath) row.classList.add("is-active");
    if (this.options.select) {
      const selected = entry.path === this.selectedPath;
      row.classList.toggle("is-selected", selected);
      row.setAttribute("aria-selected", String(selected));
    }
    if (entry.disabled) {
      row.classList.add("is-disabled");
      row.setAttribute("aria-disabled", "true");
    }
    if (this.canDo("move", entry)) {
      const grip = el("span", "ui-tree-grip");
      grip.title = "Drag to move into a folder";
      grip.append(gripIcon());
      grip.addEventListener("pointerdown", (e) => this.startDrag(e, grip, row, entry));
      row.append(grip);
    }
    const twisty = el("span", "ui-tree-twisty");
    if (entry.isDir && !entry.leaf) twisty.append(chevronIcon());
    const iconEl = el("span", "ui-tree-icon");
    iconEl.append(entry.isDir ? folderIcon() : fileIcon());
    const name = el("span", "ui-tree-name");
    name.textContent = entry.name;
    row.append(twisty, iconEl, name);
    if (entry.meta) {
      const meta = el("span", "ui-tree-meta");
      meta.textContent = entry.meta;
      row.append(meta);
    }
    const tools = el("span", "ui-tree-tools");
    const kind = entry.isDir ? "folder" : "file";
    if (this.canDo("rename", entry)) tools.append(this.tool("rename", editIcon(), `Rename ${kind} ${entry.name}`, `Rename ${kind}`));
    if (this.canDo("remove", entry)) tools.append(this.tool("remove", trashIcon(), `Delete ${kind} ${entry.name}`, `Delete ${kind}`));
    if (tools.childElementCount) row.append(tools);
    return row;
  }
  tool(action, icon2, label, tip) {
    const btn = el("button", `ui-tree-tool ui-tree-${action}`);
    btn.type = "button";
    btn.dataset.action = action;
    btn.title = tip;
    btn.setAttribute("aria-label", label);
    btn.tabIndex = -1;
    btn.append(icon2);
    return btn;
  }
  noteRow(text, depth, error = false) {
    const note = el("div", error ? "ui-tree-note is-error" : "ui-tree-note");
    note.style.setProperty("--ui-tree-depth", String(depth));
    note.textContent = text;
    return note;
  }
  isLocked() {
    return !!this.options.locked;
  }
  canDo(action, entry) {
    if (this.isLocked()) return false;
    const o = this.options;
    if (action === "rename") return !!o.rename && (o.canRename?.(entry) ?? true);
    if (action === "remove") return !!o.remove && (o.canRemove?.(entry) ?? true);
    return !!o.move && (o.canMove?.(entry) ?? !entry.isDir);
  }
  // --- interaction ---------------------------------------------------------------
  rows() {
    return Array.from(this.el.querySelectorAll(".ui-tree-row"));
  }
  rowFor(path) {
    return this.el.querySelector(`.ui-tree-row[data-path="${CSS.escape(path)}"]`);
  }
  entryOf(target) {
    const row = target?.closest?.(".ui-tree-row");
    return row?.dataset.path !== void 0 ? this.byPath.get(row.dataset.path) ?? null : null;
  }
  toggle(entry, open = !this.isOpen(entry)) {
    this.open.set(entry.path, open);
    if (open) this.errors.delete(entry.path);
    this.render();
  }
  /** Selects (picker) an entry; with `open`, also treats it as chosen. */
  choose(entry, open) {
    if (entry.disabled) return;
    const selectable = this.options.select === (entry.isDir ? "dir" : "file");
    if (selectable) {
      this.selectedPath = entry.path;
      this.options.onSelect?.(entry);
    }
    if (open && (selectable || !this.options.select)) this.options.onOpen?.(entry);
  }
  /** The row's main action: a folder opens/closes (and selects, in a folder
   * picker); a file opens, or is selected in a picker. */
  activate(entry) {
    if (entry.isDir) {
      if (this.options.select === "dir") {
        this.choose(entry, false);
        if (entry.leaf) this.markSelection();
        else this.toggle(entry, true);
      } else {
        this.toggle(entry);
      }
      return;
    }
    if (this.options.select) {
      this.choose(entry, false);
      this.markSelection();
    } else {
      this.choose(entry, true);
    }
  }
  onClick(e) {
    const target = e.target;
    if (target.closest(".ui-tree-grip")) return;
    const entry = this.entryOf(target);
    if (!entry) return;
    const tool = target.closest(".ui-tree-tool");
    if (tool) {
      e.stopPropagation();
      if (tool.dataset.action === "rename") this.options.rename?.(entry);
      if (tool.dataset.action === "remove") this.options.remove?.(entry);
      return;
    }
    if (entry.isDir && !entry.leaf && target.closest(".ui-tree-twisty")) {
      this.toggle(entry);
      return;
    }
    const now = Date.now();
    const again = this.lastClick.path === entry.path && now - this.lastClick.at < 450;
    this.lastClick = again ? { path: "", at: 0 } : { path: entry.path, at: now };
    if (again && this.options.select === (entry.isDir ? "dir" : "file")) {
      this.choose(entry, true);
      return;
    }
    this.activate(entry);
  }
  /** Moves the selection mark without redrawing the rows. */
  markSelection() {
    for (const row of this.rows()) {
      const selected = row.dataset.path === this.selectedPath;
      row.classList.toggle("is-selected", selected);
      row.setAttribute("aria-selected", String(selected));
    }
  }
  onKeydown(e) {
    const entry = this.entryOf(e.target);
    if (!entry) return;
    const rows = this.rows();
    const at = rows.indexOf(e.target);
    const focusRow = (row) => {
      if (!row) return;
      for (const r of rows) r.tabIndex = -1;
      row.tabIndex = 0;
      row.focus();
    };
    switch (e.key) {
      case "ArrowDown":
        focusRow(rows[at + 1]);
        break;
      case "ArrowUp":
        focusRow(rows[at - 1]);
        break;
      case "Home":
        focusRow(rows[0]);
        break;
      case "End":
        focusRow(rows[rows.length - 1]);
        break;
      case "ArrowRight":
        if (!entry.isDir || entry.leaf) return;
        if (!this.isOpen(entry)) this.toggle(entry, true);
        else focusRow(rows[at + 1]);
        break;
      case "ArrowLeft":
        if (entry.isDir && !entry.leaf && this.isOpen(entry)) {
          this.toggle(entry, false);
        } else {
          const parent = this.parentOf.get(entry.path);
          if (parent !== void 0) focusRow(this.rowFor(parent) ?? void 0);
        }
        break;
      case "Enter":
        if (this.options.select && this.options.select === (entry.isDir ? "dir" : "file")) this.choose(entry, true);
        else this.activate(entry);
        break;
      case " ":
        this.activate(entry);
        break;
      default:
        return;
    }
    e.preventDefault();
  }
  // --- drag to move (grocery/todo-style grip, pointer-driven) -------------------
  dropFolderAt(x, y, dragged) {
    const hit = document.elementFromPoint(x, y);
    if (!hit || !this.el.contains(hit)) return null;
    const root = this.options.rootPath ?? "";
    const target = this.entryOf(hit);
    let folder;
    if (!target) folder = root;
    else if (target.isDir) folder = target.path;
    else folder = this.parentOf.get(target.path) ?? root;
    if (folder === this.parentOf.get(dragged.path)) return null;
    if (dragged.isDir && (folder === dragged.path || contains(dragged.path, folder))) return null;
    const mark = folder === root ? this.el : this.rowFor(folder) ?? this.el;
    return { folder, mark };
  }
  startDrag(e, grip, row, entry) {
    if (e.button !== 0) return;
    e.preventDefault();
    e.stopPropagation();
    grip.setPointerCapture(e.pointerId);
    row.classList.add("is-dragging");
    let target = null;
    const clear = () => {
      this.el.classList.remove("is-drop-target");
      this.el.querySelectorAll(".is-drop-target").forEach((n) => n.classList.remove("is-drop-target"));
    };
    const onMove = (ev) => {
      clear();
      target = this.dropFolderAt(ev.clientX, ev.clientY, entry);
      target?.mark.classList.add("is-drop-target");
    };
    const onEnd = () => {
      grip.removeEventListener("pointermove", onMove);
      grip.removeEventListener("pointerup", onEnd);
      grip.removeEventListener("pointercancel", onEnd);
      row.classList.remove("is-dragging");
      clear();
      if (target) this.options.move?.(entry, target.folder);
    };
    grip.addEventListener("pointermove", onMove);
    grip.addEventListener("pointerup", onEnd);
    grip.addEventListener("pointercancel", onEnd);
  }
};
function contains(dir, path) {
  if (dir === "" || dir === "/") return path !== dir;
  return path.startsWith(dir.endsWith("/") ? dir : dir + "/");
}
function openTreePicker(options) {
  return new Promise((resolve) => {
    let settled = false;
    const settle = (value) => {
      if (settled) return;
      settled = true;
      resolve(value);
    };
    const content = el("div", "ui-tree-picker");
    if (options.note) {
      const note = el("p", "ui-modal-message");
      note.textContent = options.note;
      content.append(note);
    }
    const pathBar = el("div", "ui-tree-picker-path");
    pathBar.textContent = "Nothing selected";
    const actions = el("div", "ui-modal-actions");
    const cancel = el("button", "ui-modal-btn");
    cancel.type = "button";
    cancel.textContent = "Cancel";
    const ok = el("button", "ui-modal-btn ui-modal-btn-primary");
    ok.type = "button";
    ok.textContent = options.confirmLabel ?? "Choose";
    ok.disabled = true;
    actions.append(cancel, ok);
    let modal = null;
    const finish = (entry) => {
      settle(entry);
      modal?.close();
    };
    const tree = new FileTree({
      label: options.title,
      select: options.select,
      load: options.load,
      entries: options.entries,
      emptyText: options.emptyText,
      onSelect: (entry) => {
        pathBar.textContent = entry.path;
        ok.disabled = false;
      },
      onOpen: (entry) => finish(entry)
    });
    tree.el.classList.add("ui-tree-picker-tree");
    content.append(pathBar, tree.el, actions);
    modal = openModal(content, { title: options.title, onClose: () => settle(null) });
    cancel.addEventListener("click", () => finish(null));
    ok.addEventListener("click", () => finish(tree.selected));
    if (options.startPath !== void 0) {
      void tree.reveal(options.startPath).then(() => tree.focus());
    }
  });
}

// web/shared/ts/outputmodal.ts
var STYLE_ATTR = "data-tm-output-modal-styles";
function ensureStyles() {
  if (document.head.querySelector("style[" + STYLE_ATTR + "]")) return;
  const style = document.createElement("style");
  style.setAttribute(STYLE_ATTR, "");
  style.textContent = ".output-modal-panel { max-width: min(90vw, 900px); width: 90vw; }\n.output-modal-box { height: 70vh; max-height: 70vh; overflow: auto; margin: 0; white-space: pre-wrap; word-break: break-word; font-family: var(--font-mono, monospace); font-size: 13px; }\n";
  document.head.appendChild(style);
}
function parseOutputLine(raw) {
  try {
    const data = JSON.parse(raw);
    return data.line ?? "";
  } catch {
    return null;
  }
}
function appendOutputLine(box, ev) {
  const line = parseOutputLine(ev.data);
  if (line === null) return;
  box.textContent += line + "\n";
  box.scrollTop = box.scrollHeight;
}
function showStatusIfEmpty(box, statusText) {
  if (box.textContent === "") box.textContent = "(execution " + statusText + ")";
}
function markDoneIfEmpty(box) {
  if (box.textContent === "") box.textContent = "(no output captured for this run)";
}
function markErrorIfEmpty(box) {
  if (box.textContent === "") box.textContent = "(could not load output \u2014 the job may have been removed)";
}
function openOutputModal(source, title) {
  ensureStyles();
  const box = document.createElement("pre");
  box.className = "output-modal-box";
  box.textContent = "";
  wireOutputSource(source, box);
  const handle = openModal(box, { title, onClose: makeCloseSource(source) });
  handle.panel.classList.add("output-modal-panel");
  return handle;
}
function wireOutputSource(source, box) {
  source.addEventListener("output", makeAppendHandler(box));
  source.addEventListener("status", makeStatusHandler(box));
  source.addEventListener("done", makeDoneHandler(box, source));
  source.addEventListener("error", makeErrorHandler(box, source));
}
function makeAppendHandler(box) {
  function onOutput(ev) {
    appendOutputLine(box, ev);
  }
  return onOutput;
}
function makeStatusHandler(box) {
  function onStatus(ev) {
    showStatusIfEmpty(box, ev.data);
  }
  return onStatus;
}
function makeDoneHandler(box, source) {
  function onDone() {
    markDoneIfEmpty(box);
    source.close();
  }
  return onDone;
}
function makeErrorHandler(box, source) {
  function onError() {
    if (source.readyState === EventSource.CLOSED) markErrorIfEmpty(box);
  }
  return onError;
}
function makeCloseSource(source) {
  function closeSource() {
    source.close();
  }
  return closeSource;
}

// web/shared/ts/patchlist.ts
var KEY_ATTR = "data-ui-key";
function patchList(container, items, opts) {
  const existingByKey = /* @__PURE__ */ new Map();
  for (const child of Array.from(container.children)) {
    const el2 = child;
    const k = el2.getAttribute(KEY_ATTR);
    if (k !== null) existingByKey.set(k, el2);
  }
  const seenKeys = /* @__PURE__ */ new Set();
  let cursor = container.firstChild;
  for (const item of items) {
    const key = String(opts.key(item));
    seenKeys.add(key);
    let el2 = existingByKey.get(key);
    if (el2) {
      opts.update(el2, item);
    } else {
      el2 = opts.create(item);
      el2.setAttribute(KEY_ATTR, key);
    }
    if (cursor !== el2) {
      container.insertBefore(el2, cursor);
    } else {
      cursor = cursor.nextSibling;
      continue;
    }
    cursor = el2.nextSibling;
  }
  for (const [key, el2] of existingByKey) {
    if (!seenKeys.has(key)) {
      el2.remove();
    }
  }
}

// web/shared/ts/status.ts
function statusSymbol(status) {
  if (status === "success") return "\u2713";
  if (status === "failed") return "\u2715";
  if (status === "canceled") return "\u2298";
  if (status === "suspended") return "\u23F8";
  if (status === "running") return "\u25CF";
  if (status === "pending") return "\u2026";
  return "\u2022";
}
function effectiveStatus(status, suspended) {
  return status === "running" && suspended ? "suspended" : status;
}

// web/shared/ts/toggle.ts
function createToggle(options) {
  const wrap = document.createElement("label");
  wrap.className = "ui-toggle";
  const input = document.createElement("input");
  input.type = "checkbox";
  input.className = "ui-toggle-input";
  if (options.id) input.id = options.id;
  input.checked = options.checked;
  input.addEventListener("change", () => void options.onChange(input.checked));
  const track = document.createElement("span");
  track.className = "ui-toggle-track";
  track.setAttribute("aria-hidden", "true");
  wrap.append(input, track);
  if (options.label) {
    const text = document.createElement("span");
    text.className = "ui-toggle-label";
    text.textContent = options.label;
    wrap.append(text);
  }
  return wrap;
}

// web/shared/ts/queuepanel.ts
var DEFAULT_TITLES = {
  running: "Running",
  upnext: "Up next",
  recent: "Recent"
};
var DEFAULT_EMPTY = {
  running: "nothing running",
  upnext: "queue is empty",
  recent: "no history yet"
};
var SECTIONS = ["running", "upnext", "recent"];
var ACTION_GLYPHS = {
  cancel: { glyph: "\u2716", label: "Cancel" },
  pause: { glyph: "\u23F8", label: "Pause" },
  resume: { glyph: "\u25B6", label: "Resume" },
  rerun: { glyph: "\u21BB", label: "Re-run" },
  remove: { glyph: "\u{1F5D1}", label: "Remove" }
};
function bucketQueue(items, section, recentLimit) {
  const out = { running: [], upnext: [], recent: [] };
  for (const item of items) {
    const sec = section(item);
    if (sec === null) continue;
    out[sec].push(item);
  }
  if (recentLimit !== void 0 && Number.isFinite(recentLimit) && recentLimit >= 0) {
    out.recent = out.recent.slice(0, recentLimit);
  }
  return out;
}
function progressView(p) {
  if (!p) return { show: false, pct: 0, indeterminate: false, label: "" };
  if (p.pct === null) return { show: true, pct: 0, indeterminate: true, label: p.label ?? "" };
  const clamped = Math.max(0, Math.min(100, p.pct));
  return { show: true, pct: clamped, indeterminate: false, label: p.label ?? "" };
}
var EMPTY_KEY = "__empty__";
var QueuePanel = class {
  constructor(host, adapter, opts = {}) {
    this.headerNoteEl = null;
    this.headerToggleBtn = null;
    this.host = host;
    this.adapter = adapter;
    this.opts = opts;
    this.root = document.createElement("div");
    this.root.className = "ui-queue";
    if (opts.header) {
      this.root.appendChild(this.buildHeader(opts.header));
    }
    this.lists = { running: document.createElement("div"), upnext: document.createElement("div"), recent: document.createElement("div") };
    for (const sec of SECTIONS) {
      this.root.appendChild(this.buildSection(sec));
    }
    this.host.appendChild(this.root);
  }
  buildHeader(header) {
    const wrap = document.createElement("div");
    wrap.className = "ui-queue-header";
    const btn = document.createElement("button");
    btn.type = "button";
    btn.className = "ui-queue-header-toggle";
    btn.addEventListener("click", () => void header.onTogglePause());
    wrap.appendChild(btn);
    this.headerToggleBtn = btn;
    const note = document.createElement("span");
    note.className = "ui-queue-header-note";
    wrap.appendChild(note);
    this.headerNoteEl = note;
    this.applyHeaderState(header.paused, header.note);
    return wrap;
  }
  applyHeaderState(paused, note) {
    if (this.headerToggleBtn) {
      this.headerToggleBtn.textContent = paused ? "\u25B6" : "\u23F8";
      this.headerToggleBtn.title = paused ? "Resume queue" : "Pause queue";
      this.headerToggleBtn.setAttribute("aria-label", this.headerToggleBtn.title);
      this.headerToggleBtn.disabled = !!note;
    }
    if (this.headerNoteEl) {
      this.headerNoteEl.textContent = note ?? "";
      this.headerNoteEl.hidden = !note;
    }
  }
  /** Updates the header pause state and optional note (e.g. "Paused by taskmaster hand brake"). */
  setPaused(paused, note) {
    this.applyHeaderState(paused, note);
  }
  buildSection(sec) {
    const title = this.opts.titles?.[sec] ?? DEFAULT_TITLES[sec];
    const collapsible = sec === "recent" && (this.opts.recentCollapsible ?? true);
    const wrap = document.createElement(collapsible ? "details" : "div");
    wrap.className = "ui-queue-section ui-queue-section-" + sec;
    if (collapsible) wrap.open = this.opts.recentOpen ?? false;
    const heading = document.createElement(collapsible ? "summary" : "div");
    heading.className = "ui-queue-title";
    heading.textContent = title;
    wrap.appendChild(heading);
    const list = this.lists[sec];
    list.className = "ui-queue-list ui-queue-list-" + sec;
    wrap.appendChild(list);
    return wrap;
  }
  /** Within a section, input order is preserved. */
  update(items) {
    const buckets = bucketQueue(items, (t) => this.adapter.section(t), this.opts.recentLimit);
    for (const sec of SECTIONS) {
      this.renderSection(sec, buckets[sec]);
    }
  }
  renderSection(sec, items) {
    const list = this.lists[sec];
    patchList(list, items, {
      key: (item) => this.adapter.key(item),
      create: (item) => this.createRow(item),
      update: (row, item) => this.updateRow(row, item)
    });
    this.toggleEmptyNote(list, items.length === 0, this.opts.emptyText?.[sec] ?? DEFAULT_EMPTY[sec]);
  }
  toggleEmptyNote(list, empty, text) {
    let note = list.querySelector(".ui-queue-empty-note");
    if (empty) {
      if (!note) {
        note = document.createElement("div");
        note.className = "ui-queue-empty-note";
        note.setAttribute("data-ui-key", EMPTY_KEY);
        list.appendChild(note);
      }
      note.textContent = text;
    } else {
      note?.remove();
    }
  }
  createRow(item) {
    const row = document.createElement("div");
    row.className = "ui-queue-row";
    const name = document.createElement("span");
    name.className = "ui-queue-name";
    row.appendChild(name);
    const badge = document.createElement("span");
    badge.className = "ui-queue-badge";
    row.appendChild(badge);
    const meta = document.createElement("span");
    meta.className = "ui-queue-meta";
    row.appendChild(meta);
    const progressWrap = document.createElement("div");
    progressWrap.className = "ui-queue-progress";
    const progressBar = document.createElement("div");
    progressBar.className = "ui-queue-progress-bar";
    const progressLabel = document.createElement("span");
    progressLabel.className = "ui-queue-progress-label";
    progressWrap.append(progressBar, progressLabel);
    row.appendChild(progressWrap);
    const actions = document.createElement("div");
    actions.className = "ui-queue-actions";
    row.appendChild(actions);
    this.updateRow(row, item, true);
    return row;
  }
  updateRow(row, item, created = false) {
    const nameEl = row.querySelector(".ui-queue-name");
    if (nameEl) {
      nameEl.textContent = this.adapter.title(item);
      if (this.adapter.onTitleClick) {
        nameEl.classList.add("ui-queue-name-clickable");
        nameEl.setAttribute("role", "button");
        nameEl.tabIndex = 0;
        const onClick = () => this.adapter.onTitleClick?.(item);
        nameEl.onclick = onClick;
        nameEl.onkeydown = (e) => {
          if (e.key === "Enter" || e.key === " ") {
            e.preventDefault();
            onClick();
          }
        };
      }
    }
    const badgeEl = row.querySelector(".ui-queue-badge");
    if (badgeEl) {
      if (this.adapter.renderBadge) {
        this.adapter.renderBadge(badgeEl, item);
      } else {
        const status = this.adapter.status(item);
        badgeEl.className = "ui-queue-badge ui-queue-badge-" + status;
        badgeEl.textContent = status;
        badgeEl.title = status;
        badgeEl.setAttribute("aria-label", status);
      }
    }
    const metaEl = row.querySelector(".ui-queue-meta");
    if (metaEl) metaEl.textContent = this.adapter.meta ? this.adapter.meta(item) : "";
    const progressWrap = row.querySelector(".ui-queue-progress");
    const progressBar = row.querySelector(".ui-queue-progress-bar");
    const progressLabelEl = row.querySelector(".ui-queue-progress-label");
    const pv = progressView(this.adapter.progress ? this.adapter.progress(item) : null);
    if (progressWrap) progressWrap.hidden = !pv.show;
    if (progressBar) {
      progressBar.classList.toggle("ui-queue-progress-indeterminate", pv.indeterminate);
      progressBar.style.width = pv.indeterminate ? "" : pv.pct + "%";
      progressBar.setAttribute("role", "progressbar");
      progressBar.setAttribute("aria-valuemin", "0");
      progressBar.setAttribute("aria-valuemax", "100");
      if (pv.indeterminate) {
        progressBar.removeAttribute("aria-valuenow");
      } else {
        progressBar.setAttribute("aria-valuenow", String(pv.pct));
      }
      const ariaLabel = pv.label || (pv.indeterminate ? "in progress" : pv.pct + "%");
      progressBar.setAttribute("aria-label", ariaLabel);
    }
    if (progressLabelEl) progressLabelEl.textContent = pv.label;
    const actionsEl = row.querySelector(".ui-queue-actions");
    if (actionsEl) {
      actionsEl.textContent = "";
      for (const kind of this.adapter.actions(item)) {
        const glyph = ACTION_GLYPHS[kind];
        const btn = document.createElement("button");
        btn.type = "button";
        btn.className = "ui-queue-action ui-queue-action-" + kind;
        btn.textContent = glyph.glyph;
        btn.title = glyph.label;
        btn.setAttribute("aria-label", glyph.label);
        btn.addEventListener("click", () => void this.adapter.onAction(kind, item, btn));
        actionsEl.appendChild(btn);
      }
    }
    if (this.adapter.decorate) this.adapter.decorate(row, item, created);
  }
  list(section) {
    return this.lists[section];
  }
  destroy() {
    this.root.remove();
  }
};
export {
  FileTree,
  HamburgerMenu,
  QueuePanel,
  THEMES,
  ThemeManager,
  alertDialog,
  confirmDialog,
  copyText,
  createCopyButton,
  createToggle,
  effectiveStatus,
  openModal,
  openOutputModal,
  openTreePicker,
  patchList,
  promptDialog,
  setTheme,
  showToast,
  statusSymbol
};
