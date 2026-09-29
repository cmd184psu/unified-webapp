// toggle.ts — builds the shared toggle switch (.ui-toggle) from TypeScript.
//
// Global UI policy (see taskmaster-ui-FRD.md §8a): every binary on/off
// control renders as a sliding toggle switch, never a checkbox. The look comes
// from the shared stylesheet's .ui-toggle, the one toggle every module uses;
// this file only builds its markup and wires the handle below.
//
// Usage:
//   const el = createToggle({
//     checked: true,
//     label: "Auto-refresh",
//     onChange: (v) => console.log(v),
//   });
//   container.appendChild(el);

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
 * Creates a toggle switch. The returned element is a <label class="ui-toggle">
 * wrapping a real checkbox, so it is keyboard-accessible (Space) and
 * label-clickable natively; append it wherever a checkbox would have gone,
 * but not inside another <label>.
 */
export function createToggle(opts: ToggleOptions): HTMLElement {
  return createToggleHandle(opts).el;
}

/** Like createToggle, but also returns a handle for programmatic updates. */
export function createToggleHandle(opts: ToggleOptions): ToggleHandle {
  const wrapper = document.createElement("label");
  wrapper.className = "ui-toggle";

  const input = document.createElement("input");
  input.type = "checkbox";
  input.setAttribute("role", "switch");
  input.checked = !!opts.checked;
  input.disabled = !!opts.disabled;
  if (opts.label) input.setAttribute("aria-label", opts.label);

  const track = document.createElement("span");
  track.className = "ui-toggle-track";
  wrapper.append(input, track);

  if (opts.label) {
    const labelEl = document.createElement("span");
    labelEl.className = "ui-toggle-label";
    labelEl.textContent = opts.label;
    wrapper.appendChild(labelEl);
  }

  input.addEventListener("change", () => opts.onChange(input.checked));

  return {
    el: wrapper,
    setChecked: (v: boolean) => {
      input.checked = v;
    },
    setDisabled: (v: boolean) => {
      input.disabled = v;
    },
    getChecked: () => input.checked,
  };
}
