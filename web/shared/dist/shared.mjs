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
  function getFocusable() {
    return Array.from(
      panel.querySelectorAll(
        'a[href], button:not([disabled]), textarea:not([disabled]), input:not([disabled]), select:not([disabled]), [tabindex]:not([tabindex="-1"])'
      )
    );
  }
  function onKeydown(e) {
    if (e.key === "Escape" && closeOnEscape) {
      e.preventDefault();
      close();
      return;
    }
    if (e.key === "Tab") {
      const focusable2 = getFocusable();
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
  const focusable = getFocusable();
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
    /**
     * The roster pickers enumerate (FRD :256). Deliberately the same set as
     * THEMES and nothing more (B3.5): `system` is the resolver's implicit third
     * step, not an offerable name — it is absent here, absent from THEMES, and
     * rejected on read-back from storage (§10 ledger row 22).
     */
    this.list = THEMES;
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
    setTheme(this.resolve());
  }
  /**
   * Re-runs the resolution order against the closures' CURRENT values and
   * applies the result, without writing storage (ADR-008's v2 amendment,
   * B3.10). Same operation as apply(): the separate name is the entry point
   * obsidianoid's fetchVaults/switchVault call when the per-vault storage key
   * changes under a live instance, and delegating keeps resolution in one
   * place. Not writing is the load-bearing half — a writing reresolve() would
   * overwrite the destination vault's saved choice with the source vault's.
   */
  reresolve() {
    this.apply();
  }
  /** Writes storage, applies, and fires onChange. */
  set(name) {
    localStorage.setItem(this.storageKey(), name);
    setTheme(name);
    this.options.onChange?.(name);
  }
  /**
   * Renders the shared swatch picker into `host` — the widget HamburgerMenu
   * mounts at C4, so it has one definition and lives here beside the
   * resolution it drives. Every node is built through createElement and every
   * label is a text node — no markup string is assigned anywhere in this file
   * (B3.6), unlike the donor at obsidianoid's `app.ts:438`. No colour value
   * reaches this file either: each swatch carries `data-theme`, and themes.css
   * keys every palette on a BARE attribute selector, so a swatch sets its OWN
   * --color-primary and eight swatches render eight fills in one open picker
   * (ADR-015, B4.2).
   */
  renderPicker(host) {
    const picker = document.createElement("div");
    picker.className = "ui-theme-picker";
    const current = this.resolve();
    const buttons = [];
    for (const name of this.list) {
      const button = document.createElement("button");
      button.type = "button";
      button.className = name === current ? "ui-theme-btn is-active" : "ui-theme-btn";
      const swatch = document.createElement("span");
      swatch.className = "ui-theme-swatch";
      swatch.dataset.theme = name;
      button.append(swatch, name);
      button.addEventListener("click", () => {
        this.set(name);
        for (const other of buttons) {
          other.className = other === button ? "ui-theme-btn is-active" : "ui-theme-btn";
        }
      });
      buttons.push(button);
      picker.append(button);
    }
    host.append(picker);
  }
  /** The storage key in force right now — `ui-theme:<module>` unless overridden. */
  storageKey() {
    const override = this.options.storageKey;
    return override ? override() : `ui-theme:${this.options.module}`;
  }
  /**
   * Step 1. Every value read from storage is validated against THEMES before
   * use, so an unknown stored string falls through to the next step rather
   * than stamping a nonexistent theme (B3.2).
   */
  storedTheme() {
    const raw = localStorage.getItem(this.storageKey());
    if (raw === null) return void 0;
    return THEMES.includes(raw) ? raw : void 0;
  }
  /**
   * Step 3. `matches` → dark; everything else, `no-preference` included,
   * → light (§15 row 9). This step therefore ALWAYS resolves.
   */
  systemTheme() {
    return this.media.matches ? "dark" : "light";
  }
  /**
   * The resolution order, FRD :245-247: localStorage → serverDefault() →
   * system → default. Step 3 always answers, which makes the trailing
   * `?? this.options.default` the typed-config floor §5 Step 3.1 declares it
   * to be — present, in order, and unreachable at runtime.
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
export {
  THEMES,
  ThemeManager,
  alertDialog,
  confirmDialog,
  openModal,
  promptDialog,
  setTheme,
  showToast
};
