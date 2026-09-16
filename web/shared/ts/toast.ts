// toast.ts — non-blocking action feedback, lifted from
// web/certmachine/js/toast.ts (ADR-010) with its surface unchanged.
//
// It is the replacement for `alert('CA ready')`-style reporting: every
// mutating action reports its outcome, success or the server's own error
// text, through showToast — never alert(), confirm() or prompt() (BX.7 is
// the grep that keeps it that way).
//
// Three mechanical changes from the donor, and no others (§5 Step 2.1):
//   - the class names are cert-toast* → ui-toast*, because every selector in
//     web/shared/css/components.css must match /^\.ui-/ (check-shared-css.mjs
//     clause 7);
//   - the dismiss button's × glyph stays a text node via textContent, and
//     keeps its aria-label="Dismiss";
//   - no colour literal lives here — the tone reaches CSS as a `data-tone`
//     attribute and components.css selects on it, so the palette stays in
//     themes.css where clause 5 counts it.

export type ToastTone = "success" | "error" | "notice";

export interface ToastHandle {
  dismiss(): void;
}

/** Auto-dismiss delay per tone. Errors are sticky (0 = no auto-dismiss) — a
 * failure the operator didn't get to read before it vanished is exactly the
 * "alert-and-hope" failure mode this module exists to replace. */
const DEFAULT_DURATION_MS: Record<ToastTone, number> = {
  success: 6000,
  notice: 8000,
  error: 0,
};

let stack: HTMLElement | null = null;

// One live region for N toasts, created lazily and reused. Re-created only if
// something removed it from the document, so a page that clears document.body
// does not end up with a detached stack that never shows anything again.
function ensureStack(): HTMLElement {
  if (stack && document.body.contains(stack)) return stack;
  stack = document.createElement("div");
  stack.className = "ui-toast-stack";
  stack.setAttribute("role", "status");
  stack.setAttribute("aria-live", "polite");
  document.body.append(stack);
  return stack;
}

/**
 * Show a toast. `message` is always rendered via `.textContent` — it is
 * frequently the server's own `{"error": "..."}` text, which must never be
 * interpreted as markup.
 */
export function showToast(
  message: string,
  tone: ToastTone = "notice",
  durationMs: number = DEFAULT_DURATION_MS[tone],
): ToastHandle {
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
  close.textContent = "×";
  close.setAttribute("aria-label", "Dismiss");

  node.append(text, close);
  container.append(node);

  let timer: ReturnType<typeof setTimeout> | null = null;
  let dismissed = false;

  const dismiss = (): void => {
    if (dismissed) return;
    dismissed = true;
    if (timer !== null) clearTimeout(timer);
    node.remove();
  };

  close.addEventListener("click", dismiss);
  if (durationMs > 0) timer = setTimeout(dismiss, durationMs);

  return { dismiss };
}
