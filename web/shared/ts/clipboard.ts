// clipboard.ts — the one copy-to-clipboard helper for every module.
//
// The Clipboard API exists only in a secure context (HTTPS or localhost), and
// these modules are also served over plain HTTP (the *.test hosts), where
// navigator.clipboard is undefined. So the hidden-textarea +
// execCommand("copy") fallback is required, not optional.
//
// createCopyButton is the matching control, so every module's copy button
// looks and behaves the same.

import { showToast } from "./toast.js";
import { checkIcon, copyIcon } from "./icons.js";

/** Copies `text` to the clipboard. Resolves true on success, false on failure; never rejects. */
export async function copyText(text: string): Promise<boolean> {
  try {
    if (navigator.clipboard && window.isSecureContext) {
      await navigator.clipboard.writeText(text);
      return true;
    }
  } catch {
    // fall through to the legacy path
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

export interface CopyButtonOptions {
  /** The text to copy, read at click time. */
  text: () => string;
  /** What is copied, for the tooltip and screen readers: "Copy <label>". */
  label: string;
  /** Extra classes, to match the module's own buttons. */
  className?: string;
}

/**
 * The one copy button: a copy icon with a "Copy <label>" tooltip, which
 * copies `text()` (copyText), toasts the result, and briefly shows a check
 * mark on success.
 */
export function createCopyButton(options: CopyButtonOptions): HTMLButtonElement {
  const btn = document.createElement("button");
  btn.type = "button";
  btn.className = options.className ? `ui-copy-btn ${options.className}` : "ui-copy-btn";
  btn.title = `Copy ${options.label}`;
  btn.setAttribute("aria-label", `Copy ${options.label}`);
  btn.append(copyIcon());
  let reset: ReturnType<typeof setTimeout> | undefined;
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
