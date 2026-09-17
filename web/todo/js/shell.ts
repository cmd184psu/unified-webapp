// shell.ts -- todo module build entry (C6a, phase2 Step 6).
//
// Owns the ThemeManager instance and the topbar toggle listener.
// The hamburger drawer guard lives here; C6b fills its body.

import { ThemeManager, HamburgerMenu, type ThemeManagerOptions, type MenuItem } from "@shared";

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
    const dp = document.getElementById("drawer-theme");
    if (dp) {
      for (const btn of dp.querySelectorAll(".ui-theme-btn")) {
        const sw = btn.querySelector("[data-theme]") as HTMLElement | null;
        btn.className = sw?.dataset.theme === name
          ? "ui-theme-btn is-active"
          : "ui-theme-btn";
      }
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
  const trigger = document.getElementById("menu-toggle");
  if (!trigger) return;

  new HamburgerMenu({
    title: "Menu",
    mountTrigger: trigger,
    items: [
      {
        id: "drawer-subject",
        render(host) {
          const label = document.createElement("label");
          label.className = "field-label";
          label.htmlFor = "subject-list-selector";
          label.textContent = "Subject";
          const sel = document.createElement("select");
          sel.id = "subject-list-selector";
          sel.className = "select-input";
          sel.setAttribute("onchange", "changeSubject()");
          host.append(label, sel);
        },
      },
      {
        id: "drawer-list",
        render(host) {
          const label = document.createElement("label");
          label.className = "field-label";
          label.htmlFor = "item-list-selector";
          label.textContent = "List";
          const sel = document.createElement("select");
          sel.id = "item-list-selector";
          sel.className = "select-input";
          sel.setAttribute("onchange", "changeItem()");
          host.append(label, sel);
        },
      },
      {
        id: "drawer-move",
        render(host) {
          const label = document.createElement("label");
          label.className = "field-label";
          label.htmlFor = "new-subject-list-selector";
          label.textContent = "Move to subject";
          const sel = document.createElement("select");
          sel.id = "new-subject-list-selector";
          sel.className = "select-input";
          const btn = document.createElement("button");
          btn.className = "btn btn-outline mt-1";
          btn.setAttribute("onclick", "moveListToNewSubject()");
          btn.textContent = "Move List";
          host.append(label, sel, btn);
        },
      },
      { separator: true as const },
      { section: "Columns" },
      {
        id: "drawer-columns",
        render(host) {
          host.classList.add("option-group");
          const cols = [
            { id: "col-votes", label: "Votes", field: "votes" },
            { id: "col-period", label: "Period", field: "period" },
            { id: "col-next_due", label: "Next Due Date", field: "next_due" },
            { id: "col-cooldown", label: "Cooldown", field: "cooldown" },
          ];
          for (const col of cols) {
            const wrapper = document.createElement("label");
            wrapper.className = "toggle-label";
            const input = document.createElement("input");
            input.type = "checkbox";
            input.id = col.id;
            input.setAttribute("onchange", `setColVisible('${col.field}', this.checked)`);
            const span = document.createElement("span");
            span.className = "toggle-text";
            span.textContent = col.label;
            wrapper.append(input, span);
            host.append(wrapper);
          }
        },
      },
      { separator: true as const },
      {
        id: "drawer-cooldown",
        render(host) {
          const label = document.createElement("label");
          label.className = "field-label";
          label.htmlFor = "cooldown-input";
          label.textContent = "Vote Cooldown";
          const row = document.createElement("div");
          row.className = "cooldown-row";
          const input = document.createElement("input");
          input.type = "number";
          input.id = "cooldown-input";
          input.className = "text-input";
          input.min = "1";
          input.max = "120";
          input.value = "10";
          input.setAttribute("onchange", "saveCooldown(this.value)");
          const unit = document.createElement("span");
          unit.className = "cooldown-unit";
          unit.textContent = "min";
          row.append(input, unit);
          host.append(label, row);
        },
      },
      { separator: true as const },
      { section: "Theme" },
      {
        id: "drawer-theme",
        render(host) {
          const picker = document.createElement("div");
          picker.className = "ui-theme-picker";
          const cur = document.documentElement.getAttribute("data-theme") || "dark";
          for (const name of ["dark", "light"] as const) {
            const button = document.createElement("button");
            button.type = "button";
            button.className = name === cur
              ? "ui-theme-btn is-active"
              : "ui-theme-btn";
            const swatch = document.createElement("span");
            swatch.className = "ui-theme-swatch";
            swatch.dataset.theme = name;
            button.append(swatch, name);
            button.addEventListener("click", () => themes.set(name));
            picker.append(button);
          }
          host.append(picker);
        },
      },
    ],
  });
}

if (document.getElementById("menu-toggle")) {
  initDrawer();
}
