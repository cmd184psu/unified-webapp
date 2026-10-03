import "./ssh.css";
import { ThemeManager, HamburgerMenu, watchSecrets } from "@shared";
import type { MenuItem } from "@shared";
import { fetchConfig } from "./api";
import { mountTabs } from "./tabs";
import { reThemeAll } from "./terminal";

watchSecrets();
export const themes = new ThemeManager({
  module: "multissh",
  default: "dark",
  onChange: () => { reThemeAll(); },
});
themes.apply();

function buildHamburger(root: HTMLElement): void {
  const items: MenuItem[] = [];

  const trigger = document.createElement("button");
  trigger.className = "ui-menu-trigger";
  trigger.type = "button";
  trigger.setAttribute("aria-label", "Menu");
  trigger.innerHTML =
    '<svg width="16" height="16" viewBox="0 0 16 16" fill="currentColor"><rect x="1" y="2.5" width="14" height="2" rx="0.5"/><rect x="1" y="7" width="14" height="2" rx="0.5"/><rect x="1" y="11.5" width="14" height="2" rx="0.5"/></svg>';

  const header = document.createElement("header");
  header.className = "app-header";
  const title = document.createElement("h1");
  title.textContent = "MultiSSH";
  header.append(trigger, title);
  root.before(header);

  new HamburgerMenu({
    title: "MultiSSH",
    items,
    themePicker: true,
    themes,
    mountTrigger: trigger,
    side: "right",
  });
}

async function bootstrap(): Promise<void> {
  const root = document.getElementById("ssh-app");
  if (!root) {
    throw new Error("missing #ssh-app root element");
  }
  buildHamburger(root);
  const cfg = await fetchConfig();
  mountTabs(root, cfg.maxSessions);
}

void bootstrap();
