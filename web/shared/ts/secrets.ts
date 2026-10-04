// secrets.ts — a show/hide eye inside the right end of every masked `.ui-secret`
// input, like most sites do it. Call watchSecrets() once at startup; inputs added
// later (dialogs, innerHTML) are picked up too.
//
// The button is a sibling placed over the input with absolute positioning rather
// than a wrapper around it, so a framework that owns the input (React) is never
// surprised by the input moving in the DOM.

const SVG = (inner: string) =>
  `<svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${inner}</svg>`;
const EYE = SVG('<path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"/><circle cx="12" cy="12" r="3"/>');
const EYE_OFF = SVG(
  '<path d="M17.94 17.94A10.07 10.07 0 0 1 12 20c-7 0-11-8-11-8a18.45 18.45 0 0 1 5.06-5.94M9.9 4.24A9.12 9.12 0 0 1 12 4c7 0 11 8 11 8a18.5 18.5 0 0 1-2.16 3.19m-6.72-1.07a3 3 0 1 1-4.24-4.24"/><line x1="1" y1="1" x2="23" y2="23"/>',
);

const BTN = 28; // px, keep in step with .ui-reveal

function place(input: HTMLInputElement, btn: HTMLElement): void {
  const parent = input.offsetParent as HTMLElement | null;
  if (!parent) return; // hidden: placed again when it gets a size
  if (parent !== document.body && getComputedStyle(parent).position === "static") parent.style.position = "relative";
  btn.style.left = `${input.offsetLeft + input.offsetWidth - BTN - 2}px`;
  btn.style.top = `${input.offsetTop + (input.offsetHeight - BTN) / 2}px`;
}

function enhance(input: HTMLInputElement): void {
  if (input.dataset.revealAttached) return;
  input.dataset.revealAttached = "1";
  const btn = document.createElement("button");
  btn.type = "button";
  btn.className = "ui-reveal";
  btn.innerHTML = EYE;
  btn.title = "Show";
  btn.setAttribute("aria-label", "Show");
  btn.setAttribute("aria-pressed", "false");
  btn.addEventListener("click", () => {
    const shown = input.classList.toggle("ui-revealed");
    btn.innerHTML = shown ? EYE_OFF : EYE;
    btn.title = shown ? "Hide" : "Show";
    btn.setAttribute("aria-label", btn.title);
    btn.setAttribute("aria-pressed", String(shown));
    input.focus();
  });
  input.classList.add("ui-has-reveal");
  input.insertAdjacentElement("afterend", btn);
  const again = () => place(input, btn);
  again();
  if (typeof ResizeObserver !== "undefined") new ResizeObserver(again).observe(input);
  window.addEventListener("resize", again);
}

function scan(root: ParentNode): void {
  root.querySelectorAll<HTMLInputElement>("input.ui-secret").forEach(enhance);
}

export function watchSecrets(): void {
  const run = () => {
    scan(document);
    new MutationObserver(muts => {
      for (const m of muts) m.addedNodes.forEach(n => {
        if (n instanceof HTMLInputElement && n.classList.contains("ui-secret")) enhance(n);
        else if (n instanceof Element) scan(n);
      });
    }).observe(document.body, { childList: true, subtree: true });
  };
  if (document.body) run();
  else document.addEventListener("DOMContentLoaded", run, { once: true });
}
