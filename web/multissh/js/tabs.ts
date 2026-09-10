import { mountHostRail } from "./hosts";
import { mountSSHApp } from "./ui";
import { mountUploadApp } from "./upload";
import type { HostStore } from "./hosts";

function el<K extends keyof HTMLElementTagNameMap>(
  tag: K,
  className?: string,
): HTMLElementTagNameMap[K] {
  const node = document.createElement(tag);
  if (className) node.className = className;
  return node;
}

/**
 * Mount the full app: persistent host rail on the left, tabbed pane on the right.
 *
 * `maxSessions` is the server-reported panel count (GET /api/config); it sizes
 * both the host rail and the terminal grid.
 */
export function mountTabs(root: HTMLElement, maxSessions: number): void {
  root.innerHTML = "";
  root.classList.add("tabbed-app");

  const rail = el("aside", "host-rail");
  const store: HostStore = mountHostRail(rail, maxSessions);

  const right = el("div", "right-pane");
  const tabBar = el("nav", "tab-bar");
  const content = el("div", "tab-content");

  const defs: {
    id: string;
    label: string;
    mount: (panel: HTMLElement, store: HostStore, maxSessions: number) => void;
  }[] = [
    { id: "ssh", label: "SSH Console", mount: mountSSHApp },
    { id: "upload", label: "Upload to Host", mount: mountUploadApp },
  ];

  const panels = new Map<string, HTMLElement>();
  const btns = new Map<string, HTMLButtonElement>();

  for (const def of defs) {
    const btn = el("button", "tab-btn");
    btn.type = "button";
    btn.textContent = def.label;
    tabBar.append(btn);
    btns.set(def.id, btn);

    const panel = el("div", "tab-panel");
    panel.dataset.panel = def.id;
    content.append(panel);
    panels.set(def.id, panel);

    def.mount(panel, store, maxSessions);
  }

  const activate = (id: string): void => {
    for (const [tid, btn] of btns) {
      btn.classList.toggle("is-active", tid === id);
    }
    for (const [tid, panel] of panels) {
      panel.classList.toggle("is-hidden", tid !== id);
    }
  };

  for (const [id, btn] of btns) {
    btn.addEventListener("click", () => activate(id));
  }

  activate("ssh");

  right.append(tabBar, content);
  root.append(rail, right);
}
