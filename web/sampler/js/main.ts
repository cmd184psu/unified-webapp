// main.ts — the sampler module's entry point. Renders a live specimen page
// for every token in the shared design system and exercises the shared
// modal/dialog primitives, all through @shared -- driver rule 10 collapses
// every "@shared/..." specifier to the one barrel bundle at
// /shared/dist/shared.mjs (docs/PLAN-ui-unification-phase1.md Step 6).

import * as shared from "@shared";

const { THEMES, setTheme, openModal, confirmDialog, alertDialog, promptDialog } = shared;

// Table T1 -- the 18 --color-* keys every theme in web/shared/css/themes.css
// declares. Not a second theme list (that would violate FRD :226-228): this
// is the fixed set of colour *tokens*, not the set of themes -- THEMES above
// is the one array iterated for that.
const COLOR_TOKENS = [
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
  "--color-warning",
] as const;

// Table T2 -- the 27 structural tokens in web/shared/css/tokens.css.
const STRUCTURAL_TOKENS = [
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
  "--overlay-scrim",
] as const;

function buildThemeSelect(): void {
  const select = document.getElementById("theme-select");
  if (!(select instanceof HTMLSelectElement)) return;

  for (const theme of THEMES) {
    const option = document.createElement("option");
    option.value = theme;
    option.textContent = theme;
    select.appendChild(option);
  }

  select.value = document.documentElement.dataset.theme ?? THEMES[0];
  select.addEventListener("change", () => setTheme(select.value));
}

function buildSwatches(): void {
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

function buildSpecimens(): void {
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

interface ModalDemo {
  label: string;
  source: string;
  run: () => void | Promise<unknown>;
}

function buildModalDemos(): void {
  const container = document.getElementById("modal-demos");
  if (!container) return;

  const demos: ModalDemo[] = [
    {
      label: "openModal",
      source:
        'const content = document.createElement("div");\n' +
        'const p = document.createElement("p");\n' +
        'p.textContent = "Hello from openModal.";\n' +
        'content.appendChild(p);\n' +
        'const close = document.createElement("button");\n' +
        'close.type = "button";\n' +
        'close.className = "ui-modal-btn";\n' +
        'close.textContent = "Close";\n' +
        'content.appendChild(close);\n' +
        'const handle = openModal(content, { title: "openModal" });\n' +
        'close.addEventListener("click", () => handle.close());',
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
      },
    },
    {
      label: "confirmDialog",
      source: 'const ok = await confirmDialog("Proceed?");',
      run: () => confirmDialog("Proceed?"),
    },
    {
      label: "alertDialog",
      source: 'await alertDialog("Something happened.");',
      run: () => alertDialog("Something happened."),
    },
    {
      label: "promptDialog",
      source: 'const name = await promptDialog("Your name:", { defaultValue: "" });',
      run: () => promptDialog("Your name:", { defaultValue: "" }),
    },
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

buildThemeSelect();
buildSwatches();
buildSpecimens();
buildModalDemos();
