// modal.ts — a dependency-free in-page lightbox/modal primitive, plus
// confirm/alert/prompt replacements for the native dialogs.
//
// Global UI policy: never use alert()/confirm()/prompt() — all errors,
// confirmations, and prompts use styled in-page modals instead. Built
// self-contained so it can be lifted into any shared UI layer — no imports
// from any app code.
//
// Usage:
//   if (await confirmDialog("Delete this task?")) { ... }
//   await alertDialog("Something went wrong.");
//   const name = await promptDialog("Lane name:", { defaultValue: "New lane" });
//   const handle = openModal(myContentEl, { title: "Details" });
//   handle.close();
//
// The focus trap's element predicate lives in ./focusable.ts so menu.ts's
// trap can reuse it instead of declaring a second copy.

import { getFocusable } from "./focusable.js";

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
  const closeOnEscape = opts.closeOnEscape !== false;
  const closeOnOverlayClick = opts.closeOnOverlayClick !== false;
  const previouslyFocused = document.activeElement as HTMLElement | null;

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

  function onKeydown(e: KeyboardEvent): void {
    if (e.key === "Escape" && closeOnEscape) {
      e.preventDefault();
      close();
      return;
    }
    if (e.key === "Tab") {
      const focusable = getFocusable(panel);
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
  const focusable = getFocusable(panel);
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
