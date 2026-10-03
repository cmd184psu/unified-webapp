// secrets.ts — a show/hide eye next to every masked `.ui-secret` input.
// Call watchSecrets() once at startup; inputs added later (dialogs, innerHTML) are picked up too.

const EYE = "\u{1F441}";

function enhance(input: HTMLInputElement): void {
  if (input.dataset.revealAttached) return;
  input.dataset.revealAttached = "1";
  const btn = document.createElement("button");
  btn.type = "button";
  btn.className = "ui-copy-btn ui-reveal";
  btn.textContent = EYE;
  btn.title = "Show";
  btn.setAttribute("aria-label", "Show");
  btn.setAttribute("aria-pressed", "false");
  btn.addEventListener("click", () => {
    const shown = input.classList.toggle("ui-revealed");
    btn.title = shown ? "Hide" : "Show";
    btn.setAttribute("aria-label", btn.title);
    btn.setAttribute("aria-pressed", String(shown));
    btn.style.opacity = shown ? "1" : "0.6";
  });
  btn.style.opacity = "0.6";
  input.insertAdjacentElement("afterend", btn);
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
