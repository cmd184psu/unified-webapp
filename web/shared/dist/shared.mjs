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
export {
  THEMES,
  alertDialog,
  confirmDialog,
  openModal,
  promptDialog,
  setTheme
};
