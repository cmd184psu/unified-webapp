// main.ts — the sampler module's entry point. Renders a live specimen page
// for every token in the shared design system and exercises the shared
// modal/dialog primitives, all through @shared -- driver rule 10 collapses
// every "@shared/..." specifier to the one barrel bundle at
// /shared/dist/shared.mjs (docs/PLAN-ui-unification-phase1.md Step 6).

import * as shared from "@shared";

const { THEMES, ThemeManager, openModal, confirmDialog, alertDialog, promptDialog, showToast } = shared;

// The sampler's ThemeManager (phase2 §5 Step 3.5). `default: "dark"` is the
// typed-config floor — present by construction and unreachable at runtime,
// because the `system` step resolves under every matchMedia outcome (§5
// Step 3.1). `setTheme` is no longer destructured here: themes.set() is the
// persisting path, and setTheme stays the barrel's primitive (ADR-008).
//
// Two sanctioned deltas land with this instance, both stated in Step 3.5.
// (1) The sampler becomes the first module with PERSISTED theme selection.
// (2) Its default render becomes OS-DEPENDENT: index.html:2 carries no
//     data-theme, so before C3 the page rendered `dark` unconditionally from
//     themes.css:15's `:root, [data-theme="dark"]` half; now, on a fresh
//     profile, resolution falls through empty storage and an absent
//     serverDefault() to the `system` step — `light` under
//     prefers-color-scheme: light or no-preference, `dark` only when the OS
//     asks for dark. This is the one place the `system` step reaches a pixel
//     in Phase 2. B10.5's premise is about the STYLESHEET, which does not
//     move; what moves is which attribute the page stamps on itself.
const themes = new ThemeManager({
  module: "sampler",
  default: "dark",
  onChange: (name) => {
    // Keep the header <select> and the swatch picker — two views of one state
    // — in agreement when the choice is made in the other one.
    const select = document.getElementById("theme-select");
    if (select instanceof HTMLSelectElement) select.value = name;
  },
});

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

  for (const theme of themes.list) {
    const option = document.createElement("option");
    option.value = theme;
    option.textContent = theme;
    select.appendChild(option);
  }

  select.value = document.documentElement.dataset.theme ?? THEMES[0];
  select.addEventListener("change", () => themes.set(select.value));
}

// The Theme picker section (phase2 §5 Step 3.5). The shared .ui-theme-picker
// swatch grid, built by the same ThemeManager method HamburgerMenu mounts at
// C4 — one definition of "how the picker is built", and the section B10.3
// requires for this component.
function buildThemePicker(): void {
  const host = document.getElementById("theme-picker");
  if (!host) return;
  themes.renderPicker(host);
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

// The Toasts section (phase2 §5 Step 2.6). Same row shape as
// buildModalDemos() above — a button that runs the demo and a <pre> carrying
// the source that produced it — one row per tone plus a stacking row, which is
// the only way the "one aria-live region for N toasts" property (B5.1/B5.5) is
// visible on the page rather than only in the unit suite. `error` is sticky by
// design, so its row is also the one that demonstrates the close button.
function buildToastDemos(): void {
  const container = document.getElementById("toast-demos");
  if (!container) return;

  const demos: ModalDemo[] = [
    {
      label: "success",
      source: 'showToast("Saved.", "success");',
      run: () => showToast("Saved.", "success"),
    },
    {
      label: "error (sticky)",
      source: 'showToast("Could not save: disk full.", "error");',
      run: () => showToast("Could not save: disk full.", "error"),
    },
    {
      label: "notice",
      source: 'showToast("Nothing to do.", "notice");',
      run: () => showToast("Nothing to do.", "notice"),
    },
    {
      label: "3 at once",
      source:
        'showToast("First.", "success");\n' +
        'showToast("Second.", "notice");\n' +
        'showToast("Third.", "error");',
      run: () => {
        showToast("First.", "success");
        showToast("Second.", "notice");
        showToast("Third.", "error");
      },
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

// apply() first: buildThemeSelect() reads the stamped attribute back to seed
// the <select>, so the resolution has to have run before it does.
themes.apply();
buildThemeSelect();
buildThemePicker();
buildSwatches();
buildSpecimens();
buildModalDemos();
buildToastDemos();
