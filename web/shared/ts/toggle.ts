// toggle.ts — a small reusable on/off switch for boolean settings.
//
// Standing UI preference: boolean settings use a toggle switch, never a
// checkbox, anywhere in this codebase (owner policy). This is the one
// implementation every module should reuse instead of hand-rolling its own.

export interface ToggleOptions {
  id?: string;
  checked: boolean;
  label?: string;
  onChange(checked: boolean): void | Promise<void>;
}

/** Builds a <label> wrapping a visually-hidden checkbox plus a styled track/
 * thumb — an accessible, keyboard-operable toggle switch. */
export function createToggle(options: ToggleOptions): HTMLLabelElement {
  const wrap = document.createElement('label');
  wrap.className = 'ui-toggle';

  const input = document.createElement('input');
  input.type = 'checkbox';
  input.className = 'ui-toggle-input';
  if (options.id) input.id = options.id;
  input.checked = options.checked;
  input.addEventListener('change', () => void options.onChange(input.checked));

  const track = document.createElement('span');
  track.className = 'ui-toggle-track';
  track.setAttribute('aria-hidden', 'true');

  wrap.append(input, track);

  if (options.label) {
    const text = document.createElement('span');
    text.className = 'ui-toggle-label';
    text.textContent = options.label;
    wrap.append(text);
  }

  return wrap;
}
