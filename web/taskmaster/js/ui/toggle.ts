// toggle.ts — a dependency-free toggle-switch UI primitive.
//
// Global UI policy (see taskmaster-ui-FRD.md §8a): every binary on/off
// control renders as a sliding toggle switch, never a checkbox. Built
// self-contained so it can be lifted wholesale into a future shared UI
// layer (§8b) — no imports from any taskmaster app code.
//
// Usage:
//   const el = createToggle({
//     checked: true,
//     label: "Auto-refresh",
//     onChange: (v) => console.log(v),
//   });
//   container.appendChild(el);

const STYLE_ATTR = "data-tm-ui-toggle-styles";

/** Injects the toggle's CSS once per document (idempotent). */
function ensureStyles(): void {
  if (document.head.querySelector(`style[${STYLE_ATTR}]`)) return;
  const style = document.createElement("style");
  style.setAttribute(STYLE_ATTR, "");
  style.textContent = `
.tm-toggle {
  display: inline-flex;
  align-items: center;
  gap: 0.5em;
  cursor: pointer;
  font-family: var(--font-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif);
  color: var(--text-normal, #dcddde);
  user-select: none;
}
.tm-toggle[data-disabled="true"] {
  cursor: not-allowed;
  opacity: 0.5;
}
.tm-toggle-track {
  position: relative;
  flex: 0 0 auto;
  width: 2.25em;
  height: 1.25em;
  border-radius: 999px;
  background: var(--bg-modifier-border, #3a3a3a);
  transition: background-color 0.15s ease;
  box-sizing: border-box;
  border: 1px solid transparent;
}
.tm-toggle-track:focus-visible {
  outline: none;
  border-color: var(--interactive-accent, #7f6df2);
  box-shadow: 0 0 0 2px var(--interactive-accent-hover, #9d8fff);
}
.tm-toggle[data-checked="true"] .tm-toggle-track {
  background: var(--interactive-accent, #7f6df2);
}
.tm-toggle-thumb {
  position: absolute;
  top: 0.1em;
  left: 0.1em;
  width: 1.05em;
  height: 1.05em;
  border-radius: 50%;
  background: #fff;
  transition: transform 0.15s ease;
}
.tm-toggle[data-checked="true"] .tm-toggle-thumb {
  transform: translateX(1em);
}
.tm-toggle-label {
  font-size: 0.9em;
  line-height: 1;
}
`;
  document.head.appendChild(style);
}

export interface ToggleOptions {
  checked: boolean;
  onChange: (value: boolean) => void;
  label?: string;
  disabled?: boolean;
}

export interface ToggleHandle {
  el: HTMLElement;
  setChecked: (checked: boolean) => void;
  setDisabled: (disabled: boolean) => void;
  getChecked: () => boolean;
}

/**
 * Creates a keyboard-accessible toggle switch. The returned element is a
 * single wrapper <span> containing the track/thumb and an optional label;
 * append it wherever a checkbox would have gone.
 */
export function createToggle(opts: ToggleOptions): HTMLElement {
  return createToggleHandle(opts).el;
}

/** Like createToggle, but also returns a handle for programmatic updates. */
export function createToggleHandle(opts: ToggleOptions): ToggleHandle {
  ensureStyles();

  let checked = !!opts.checked;
  let disabled = !!opts.disabled;

  const wrapper = document.createElement("span");
  wrapper.className = "tm-toggle";

  const track = document.createElement("span");
  track.className = "tm-toggle-track";
  track.setAttribute("role", "switch");
  track.tabIndex = disabled ? -1 : 0;

  const thumb = document.createElement("span");
  thumb.className = "tm-toggle-thumb";
  track.appendChild(thumb);
  wrapper.appendChild(track);

  let labelEl: HTMLSpanElement | null = null;
  if (opts.label) {
    labelEl = document.createElement("span");
    labelEl.className = "tm-toggle-label";
    labelEl.textContent = opts.label;
    wrapper.appendChild(labelEl);
  }

  function render(): void {
    wrapper.setAttribute("data-checked", String(checked));
    wrapper.setAttribute("data-disabled", String(disabled));
    track.setAttribute("aria-checked", String(checked));
    track.setAttribute("aria-disabled", String(disabled));
    track.tabIndex = disabled ? -1 : 0;
    if (opts.label) {
      track.setAttribute("aria-label", opts.label);
    }
  }

  function toggle(): void {
    if (disabled) return;
    checked = !checked;
    render();
    opts.onChange(checked);
  }

  track.addEventListener("click", toggle);
  if (labelEl) {
    labelEl.addEventListener("click", toggle);
  }
  track.addEventListener("keydown", (e: KeyboardEvent) => {
    if (e.key === " " || e.key === "Enter") {
      e.preventDefault();
      toggle();
    }
  });

  render();

  return {
    el: wrapper,
    setChecked: (v: boolean) => {
      checked = v;
      render();
    },
    setDisabled: (v: boolean) => {
      disabled = v;
      render();
    },
    getChecked: () => checked,
  };
}
