// web/sampler/js/main.ts
import * as shared from "/shared/dist/shared.mjs";
var { THEMES, ThemeManager, openModal, confirmDialog, alertDialog, promptDialog, showToast } = shared;
var themes = new ThemeManager({
  module: "sampler",
  default: "dark",
  onChange: (name) => {
    const select = document.getElementById("theme-select");
    if (select instanceof HTMLSelectElement) select.value = name;
  }
});
var COLOR_TOKENS = [
  "--color-bg",
  "--color-surface-1",
  "--color-surface-2",
  "--color-surface-3",
  "--color-surface-dynamic",
  "--color-border",
  "--color-divider",
  "--color-text",
  "--color-text-muted",
  "--color-text-faint",
  "--color-primary",
  "--color-primary-hover",
  "--color-primary-active",
  "--color-primary-tint",
  "--color-primary-fg",
  "--color-danger",
  "--color-success",
  "--color-warning"
];
var STRUCTURAL_TOKENS = [
  "--radius-sm",
  "--radius-md",
  "--radius-lg",
  "--radius-full",
  "--shadow-sm",
  "--shadow-md",
  "--space-1",
  "--space-2",
  "--space-3",
  "--space-4",
  "--space-5",
  "--space-6",
  "--space-7",
  "--space-8",
  "--text-xs",
  "--text-sm",
  "--text-base",
  "--text-lg",
  "--text-xl",
  "--font-body",
  "--font-mono",
  "--font-body-fallback",
  "--font-mono-fallback",
  "--sidebar-width",
  "--topbar-height",
  "--transition",
  "--overlay-scrim"
];
function buildThemeSelect() {
  const select = document.getElementById("theme-select");
  if (!(select instanceof HTMLSelectElement)) return;
  for (const theme of themes.list) {
    const option = document.createElement("option");
    option.value = theme;
    option.textContent = theme;
    select.appendChild(option);
  }
  select.value = document.documentElement.dataset.theme ?? THEMES[0];
  select.addEventListener("change", () => themes.set(select.value));
}
function buildThemePicker() {
  const host = document.getElementById("theme-picker");
  if (!host) return;
  themes.renderPicker(host);
}
function buildSwatches() {
  const grid = document.getElementById("swatch-grid");
  if (!grid) return;
  for (const token of COLOR_TOKENS) {
    const swatch = document.createElement("div");
    swatch.className = "sampler-swatch";
    const chip = document.createElement("div");
    chip.className = "sampler-swatch-chip";
    chip.style.background = `var(${token})`;
    swatch.appendChild(chip);
    const label = document.createElement("code");
    label.className = "sampler-swatch-label";
    label.textContent = token;
    swatch.appendChild(label);
    grid.appendChild(swatch);
  }
}
function buildSpecimens() {
  const list = document.getElementById("specimen-list");
  if (!list) return;
  const computed = getComputedStyle(document.documentElement);
  for (const token of STRUCTURAL_TOKENS) {
    const row = document.createElement("div");
    row.className = "sampler-specimen";
    const label = document.createElement("code");
    label.className = "sampler-specimen-label";
    label.textContent = token;
    row.appendChild(label);
    const value = document.createElement("code");
    value.className = "sampler-specimen-value";
    value.textContent = computed.getPropertyValue(token).trim();
    row.appendChild(value);
    list.appendChild(row);
  }
}
function buildModalDemos() {
  const container = document.getElementById("modal-demos");
  if (!container) return;
  const demos = [
    {
      label: "openModal",
      source: 'const content = document.createElement("div");\nconst p = document.createElement("p");\np.textContent = "Hello from openModal.";\ncontent.appendChild(p);\nconst close = document.createElement("button");\nclose.type = "button";\nclose.className = "ui-modal-btn";\nclose.textContent = "Close";\ncontent.appendChild(close);\nconst handle = openModal(content, { title: "openModal" });\nclose.addEventListener("click", () => handle.close());',
      run: () => {
        const content = document.createElement("div");
        const p = document.createElement("p");
        p.textContent = "Hello from openModal.";
        content.appendChild(p);
        const close = document.createElement("button");
        close.type = "button";
        close.className = "ui-modal-btn";
        close.textContent = "Close";
        content.appendChild(close);
        const handle = openModal(content, { title: "openModal" });
        close.addEventListener("click", () => handle.close());
      }
    },
    {
      label: "confirmDialog",
      source: 'const ok = await confirmDialog("Proceed?");',
      run: () => confirmDialog("Proceed?")
    },
    {
      label: "alertDialog",
      source: 'await alertDialog("Something happened.");',
      run: () => alertDialog("Something happened.")
    },
    {
      label: "promptDialog",
      source: 'const name = await promptDialog("Your name:", { defaultValue: "" });',
      run: () => promptDialog("Your name:", { defaultValue: "" })
    }
  ];
  for (const demo of demos) {
    const row = document.createElement("div");
    row.className = "sampler-modal-demo";
    const button = document.createElement("button");
    button.type = "button";
    button.className = "ui-modal-btn ui-modal-btn-primary";
    button.textContent = demo.label;
    button.addEventListener("click", () => {
      void demo.run();
    });
    row.appendChild(button);
    const pre = document.createElement("pre");
    pre.className = "sampler-modal-source";
    pre.textContent = demo.source;
    row.appendChild(pre);
    container.appendChild(row);
  }
}
function buildToastDemos() {
  const container = document.getElementById("toast-demos");
  if (!container) return;
  const demos = [
    {
      label: "success",
      source: 'showToast("Saved.", "success");',
      run: () => showToast("Saved.", "success")
    },
    {
      label: "error (sticky)",
      source: 'showToast("Could not save: disk full.", "error");',
      run: () => showToast("Could not save: disk full.", "error")
    },
    {
      label: "notice",
      source: 'showToast("Nothing to do.", "notice");',
      run: () => showToast("Nothing to do.", "notice")
    },
    {
      label: "3 at once",
      source: 'showToast("First.", "success");\nshowToast("Second.", "notice");\nshowToast("Third.", "error");',
      run: () => {
        showToast("First.", "success");
        showToast("Second.", "notice");
        showToast("Third.", "error");
      }
    }
  ];
  for (const demo of demos) {
    const row = document.createElement("div");
    row.className = "sampler-modal-demo";
    const button = document.createElement("button");
    button.type = "button";
    button.className = "ui-modal-btn ui-modal-btn-primary";
    button.textContent = demo.label;
    button.addEventListener("click", () => {
      void demo.run();
    });
    row.appendChild(button);
    const pre = document.createElement("pre");
    pre.className = "sampler-modal-source";
    pre.textContent = demo.source;
    row.appendChild(pre);
    container.appendChild(row);
  }
}
themes.apply();
buildThemeSelect();
buildThemePicker();
buildSwatches();
buildSpecimens();
buildModalDemos();
buildToastDemos();
