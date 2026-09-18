import "./ssh.css";
import { ThemeManager, HamburgerMenu } from "@shared";
import type { MenuItem } from "@shared";
import { fetchConfig } from "./api";
import { mountTabs } from "./tabs";
import { reThemeAll } from "./terminal";

export const themes = new ThemeManager({
  module: "multissh",
  default: "dark",
  onChange: () => { reThemeAll(); },
});
themes.apply();

function buildHamburger(): void {
  const items: MenuItem[] = [];
  const hamburger = new HamburgerMenu({
    title: "MultiSSH",
    items,
    themePicker: true,
    themes,
  });
  const app = document.getElementById("ssh-app");
  if (app) {
    app.prepend(hamburger.trigger);
  }
}

async function bootstrap(): Promise<void> {
  const root = document.getElementById("ssh-app");
  if (!root) {
    throw new Error("missing #ssh-app root element");
  }
  buildHamburger();
  const cfg = await fetchConfig();
  mountTabs(root, cfg.maxSessions);
}

void bootstrap();
