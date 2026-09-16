// shell.ts -- todo module build entry (C6a, phase2 Step 6).
//
// Owns the ThemeManager instance and the topbar toggle listener.
// The hamburger drawer guard lives here; C6b fills its body.

import { ThemeManager, type ThemeManagerOptions } from "@shared";

// Storage key "todo-theme" preserves existing user preferences from the
// migrated theme.js.  The default key would be "ui-theme:todo"; overriding
// avoids a one-time reset for returning visitors.
const themes = new ThemeManager({
  module: "todo",
  default: "dark",
  storageKey: () => "todo-theme",
  onChange: (name) => {
    const icon = document.getElementById("theme-icon");
    if (icon) {
      icon.className = name === "dark" ? "fas fa-moon" : "fas fa-sun";
    }
  },
});

themes.apply();

// -- Topbar toggle ------------------------------------------------------------

const toggle = document.getElementById("theme-toggle");
if (toggle) {
  toggle.addEventListener("click", () => {
    const cur = document.documentElement.getAttribute("data-theme");
    const next = cur === "dark" ? "light" : "dark";
    themes.set(next);
  });
}

// -- Drawer (C6b fills the body) ----------------------------------------------

function initDrawer(): void {
  // Placeholder -- C6b wires up the hamburger drawer here.
}

if (document.getElementById("menu-toggle")) {
  initDrawer();
}
