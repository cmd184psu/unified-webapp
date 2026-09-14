/**
 * Non-blocking action feedback -- the replacement for legacy's
 * `alert('CA ready')` (`reference/certmachine/static/app.js:2`), which fires
 * whether or not the request actually succeeded because the fetch result is
 * never inspected. Every mutating action in this app (generate/renew/
 * delete/import/init-CA) reports its outcome, success or the server's own
 * error text, through `showToast` -- never `alert()`, `confirm()`, or
 * `prompt()` (grep-guarded, see the slice 10 handoff).
 */

export type ToastTone = "success" | "error" | "notice";

export interface ToastHandle {
  dismiss(): void;
}

/** Auto-dismiss delay per tone. Errors are sticky (0 = no auto-dismiss) -- a
 * failure the operator didn't get to read before it vanished is exactly the
 * "alert-and-hope" failure mode this module exists to replace. */
const DEFAULT_DURATION_MS: Record<ToastTone, number> = {
  success: 6000,
  notice: 8000,
  error: 0,
};

let stack: HTMLElement | null = null;

function ensureStack(): HTMLElement {
  if (stack && document.body.contains(stack)) return stack;
  stack = document.createElement("div");
  stack.className = "cert-toast-stack";
  stack.setAttribute("role", "status");
  stack.setAttribute("aria-live", "polite");
  document.body.append(stack);
  return stack;
}

/**
 * Show a toast. `message` is always rendered via `.textContent` -- it is
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
  node.className = "cert-toast";
  node.dataset.tone = tone;

  const text = document.createElement("p");
  text.className = "cert-toast-text";
  text.textContent = message;

  const close = document.createElement("button");
  close.type = "button";
  close.className = "cert-toast-close";
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
