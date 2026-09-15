// modal.ts — a dependency-free in-page lightbox/modal primitive, plus
// confirm/alert/prompt replacements for the native dialogs.
//
// Global UI policy (see taskmaster-ui-FRD.md §8a): never use
// alert()/confirm()/prompt() — all errors, confirmations, and prompts use
// styled in-page modals instead. Built self-contained so it can be lifted
// wholesale into a future shared UI layer (§8b) — no imports from any
// taskmaster app code.
//
// Usage:
//   if (await confirmDialog("Delete this task?")) { ... }
//   await alertDialog("Something went wrong.");
//   const name = await promptDialog("Lane name:", { defaultValue: "New lane" });
//   const handle = openModal(myContentEl, { title: "Details" });
//   handle.close();

const STYLE_ATTR = "data-tm-ui-modal-styles";

/** Injects the modal's CSS once per document (idempotent). */
function ensureStyles(): void {
  if (document.head.querySelector(`style[${STYLE_ATTR}]`)) return;
  const style = document.createElement("style");
  style.setAttribute(STYLE_ATTR, "");
  style.textContent = `
.tm-modal-overlay {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.55);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 10000;
  font-family: var(--font-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif);
}
.tm-modal-panel {
  background: var(--bg-secondary, #252525);
  color: var(--text-normal, #dcddde);
  border: 1px solid var(--bg-modifier-border, #3a3a3a);
  border-radius: var(--radius, 6px);
  min-width: 20em;
  max-width: min(32em, calc(100vw - 2em));
  max-height: calc(100vh - 2em);
  overflow: auto;
  box-shadow: 0 8px 32px rgba(0, 0, 0, 0.4);
  padding: 1.25em;
}
.tm-modal-title {
  font-size: 1.05em;
  font-weight: 600;
  margin: 0 0 0.75em 0;
}
.tm-modal-message {
  margin: 0 0 1em 0;
  white-space: pre-wrap;
  line-height: 1.4;
}
.tm-modal-input {
  width: 100%;
  box-sizing: border-box;
  padding: 0.5em;
  margin-bottom: 1em;
  background: var(--bg-primary, #1e1e1e);
  color: var(--text-normal, #dcddde);
  border: 1px solid var(--bg-modifier-border, #3a3a3a);
  border-radius: var(--radius, 6px);
  font-family: inherit;
  font-size: 1em;
}
.tm-modal-input:focus-visible {
  outline: none;
  border-color: var(--interactive-accent, #7f6df2);
}
.tm-modal-actions {
  display: flex;
  justify-content: flex-end;
  gap: 0.5em;
}
.tm-modal-btn {
  padding: 0.45em 1em;
  border-radius: var(--radius, 6px);
  border: 1px solid var(--bg-modifier-border, #3a3a3a);
  background: var(--bg-tertiary, #2d2d2d);
  color: var(--text-normal, #dcddde);
  cursor: pointer;
  font-family: inherit;
  font-size: 0.9em;
}
.tm-modal-btn:hover {
  border-color: var(--interactive-accent, #7f6df2);
}
.tm-modal-btn:focus-visible {
  outline: none;
  border-color: var(--interactive-accent, #7f6df2);
  box-shadow: 0 0 0 2px var(--interactive-accent-hover, #9d8fff);
}
.tm-modal-btn-primary {
  background: var(--interactive-accent, #7f6df2);
  border-color: var(--interactive-accent, #7f6df2);
  color: #fff;
}
.tm-modal-btn-primary:hover {
  background: var(--interactive-accent-hover, #9d8fff);
}
`;
  document.head.appendChild(style);
}

export interface ModalOptions {
  title?: string;
  /** Closes on Escape key. Default true. */
  closeOnEscape?: boolean;
  /** Closes on overlay (backdrop) click. Default true. */
  closeOnOverlayClick?: boolean;
  /** Called after the modal is closed (any reason). */
  onClose?: () => void;
}

export interface ModalHandle {
  overlay: HTMLElement;
  panel: HTMLElement;
  close: () => void;
}

/**
 * Opens a generic modal around an arbitrary content element. Traps focus
 * within the panel while open and restores focus to the previously focused
 * element on close.
 */
export function openModal(contentEl: HTMLElement, opts: ModalOptions = {}): ModalHandle {
  ensureStyles();

  const closeOnEscape = opts.closeOnEscape !== false;
  const closeOnOverlayClick = opts.closeOnOverlayClick !== false;
  const previouslyFocused = document.activeElement as HTMLElement | null;

  const overlay = document.createElement("div");
  overlay.className = "tm-modal-overlay";

  const panel = document.createElement("div");
  panel.className = "tm-modal-panel";
  panel.setAttribute("role", "dialog");
  panel.setAttribute("aria-modal", "true");
  panel.tabIndex = -1;

  if (opts.title) {
    const titleEl = document.createElement("h2");
    titleEl.className = "tm-modal-title";
    titleEl.textContent = opts.title;
    panel.appendChild(titleEl);
  }

  panel.appendChild(contentEl);
  overlay.appendChild(panel);
  document.body.appendChild(overlay);

  let closed = false;

  function getFocusable(): HTMLElement[] {
    return Array.from(
      panel.querySelectorAll<HTMLElement>(
        'a[href], button:not([disabled]), textarea:not([disabled]), input:not([disabled]), select:not([disabled]), [tabindex]:not([tabindex="-1"])'
      )
    );
  }

  function onKeydown(e: KeyboardEvent): void {
    if (e.key === "Escape" && closeOnEscape) {
      e.preventDefault();
      close();
      return;
    }
    if (e.key === "Tab") {
      const focusable = getFocusable();
      if (focusable.length === 0) {
        e.preventDefault();
        panel.focus();
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

  function onOverlayClick(e: MouseEvent): void {
    if (closeOnOverlayClick && e.target === overlay) {
      close();
    }
  }

  function close(): void {
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

  // Initial focus: first focusable element in the content, else the panel.
  const focusable = getFocusable();
  (focusable[0] ?? panel).focus();

  return { overlay, panel, close };
}

export interface DialogOptions {
  title?: string;
  confirmLabel?: string;
  cancelLabel?: string;
}

/** In-page replacement for window.confirm(). Resolves true/false. */
export function confirmDialog(message: string, opts: DialogOptions = {}): Promise<boolean> {
  return new Promise((resolve) => {
    let settled = false;
    const content = document.createElement("div");

    const msg = document.createElement("p");
    msg.className = "tm-modal-message";
    msg.textContent = message;
    content.appendChild(msg);

    const actions = document.createElement("div");
    actions.className = "tm-modal-actions";

    const cancelBtn = document.createElement("button");
    cancelBtn.type = "button";
    cancelBtn.className = "tm-modal-btn";
    cancelBtn.textContent = opts.cancelLabel ?? "Cancel";

    const okBtn = document.createElement("button");
    okBtn.type = "button";
    okBtn.className = "tm-modal-btn tm-modal-btn-primary";
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
      },
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

/** In-page replacement for window.alert(). Resolves once dismissed. */
export function alertDialog(message: string, opts: DialogOptions = {}): Promise<void> {
  return new Promise((resolve) => {
    let settled = false;
    const content = document.createElement("div");

    const msg = document.createElement("p");
    msg.className = "tm-modal-message";
    msg.textContent = message;
    content.appendChild(msg);

    const actions = document.createElement("div");
    actions.className = "tm-modal-actions";

    const okBtn = document.createElement("button");
    okBtn.type = "button";
    okBtn.className = "tm-modal-btn tm-modal-btn-primary";
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
      },
    });

    okBtn.addEventListener("click", () => {
      settled = true;
      resolve();
      handle.close();
    });
  });
}

export interface PromptOptions extends DialogOptions {
  defaultValue?: string;
  placeholder?: string;
}

/**
 * In-page replacement for window.prompt(). Resolves the entered string, or
 * null if canceled/dismissed.
 */
export function promptDialog(message: string, opts: PromptOptions = {}): Promise<string | null> {
  return new Promise((resolve) => {
    let settled = false;
    const content = document.createElement("div");

    const msg = document.createElement("p");
    msg.className = "tm-modal-message";
    msg.textContent = message;
    content.appendChild(msg);

    const input = document.createElement("input");
    input.type = "text";
    input.className = "tm-modal-input";
    input.value = opts.defaultValue ?? "";
    if (opts.placeholder) input.placeholder = opts.placeholder;
    content.appendChild(input);

    const actions = document.createElement("div");
    actions.className = "tm-modal-actions";

    const cancelBtn = document.createElement("button");
    cancelBtn.type = "button";
    cancelBtn.className = "tm-modal-btn";
    cancelBtn.textContent = opts.cancelLabel ?? "Cancel";

    const okBtn = document.createElement("button");
    okBtn.type = "button";
    okBtn.className = "tm-modal-btn tm-modal-btn-primary";
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
      },
    });

    function submit(): void {
      settled = true;
      resolve(input.value);
      handle.close();
    }

    input.addEventListener("keydown", (e: KeyboardEvent) => {
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
