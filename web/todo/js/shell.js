// web/todo/js/shell.ts
import { ThemeManager } from "/shared/dist/shared.mjs";
var themes = new ThemeManager({
  module: "todo",
  default: "dark",
  storageKey: () => "todo-theme",
  onChange: (name) => {
    const icon = document.getElementById("theme-icon");
    if (icon) {
      icon.className = name === "dark" ? "fas fa-moon" : "fas fa-sun";
    }
  }
});
themes.apply();
var toggle = document.getElementById("theme-toggle");
if (toggle) {
  toggle.addEventListener("click", () => {
    const cur = document.documentElement.getAttribute("data-theme");
    const next = cur === "dark" ? "light" : "dark";
    themes.set(next);
  });
}
function initDrawer() {
}
if (document.getElementById("menu-toggle")) {
  initDrawer();
}
