// clipboard.ts — the one copy-to-clipboard helper for every module.
//
// The Clipboard API exists only in a secure context (HTTPS or localhost), and
// these modules are also served over plain HTTP (the *.test hosts), where
// navigator.clipboard is undefined. So the hidden-textarea +
// execCommand("copy") fallback is required, not optional.

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
