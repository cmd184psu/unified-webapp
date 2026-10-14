// forms.ts — the small form kit the settings, backend, project and document
// editors share: labelled field rows with an inline error slot, buttons, and
// the mapping of a 400 response's field name onto the right row (UI2, UI5).
// Everything is built with h(), so values and server messages are text.

import { ApiError } from './api.js';
import { h, type Child } from './dom.js';

export interface Field {
  /** The row: label, control, hint and error. */
  el: HTMLElement;
  control: HTMLElement;
  error(msg: string): void;
}

type Control = HTMLInputElement | HTMLSelectElement | HTMLTextAreaElement;

/** textInput builds a single-line input marked data-field=name. */
export function textInput(name: string, value: string, opts: { type?: string; placeholder?: string; readOnly?: boolean } = {}): HTMLInputElement {
  const el = h('input', {
    class: 'settings-input',
    attrs: { type: opts.type ?? 'text', name, 'data-field': name, placeholder: opts.placeholder, readonly: opts.readOnly },
  }) as HTMLInputElement;
  el.value = value;
  return el;
}

/** textArea builds a multi-line input marked data-field=name. */
export function textArea(name: string, value: string, rows = 4): HTMLTextAreaElement {
  const el = h('textarea', { class: 'settings-input', attrs: { name, 'data-field': name, rows } }) as HTMLTextAreaElement;
  el.value = value;
  return el;
}

/** checkbox builds a checkbox marked data-field=name. */
export function checkbox(name: string, checked: boolean): HTMLInputElement {
  const el = h('input', { attrs: { type: 'checkbox', name, 'data-field': name } }) as HTMLInputElement;
  el.checked = checked;
  return el;
}

/** selectInput builds a select from [value, label] pairs; an unknown value selects the first. */
export function selectInput(name: string, options: Array<[string, string]>, value: string): HTMLSelectElement {
  const el = h('select', { class: 'settings-input', attrs: { name, 'data-field': name } },
    ...options.map(([v, label]) => h('option', { text: label, attrs: { value: v } }))) as HTMLSelectElement;
  el.value = options.some(([v]) => v === value) ? value : (options[0]?.[0] ?? '');
  return el;
}

/** fieldRow wraps a control in a labelled row with a hint and an inline error. */
export function fieldRow(label: string, control: Control | HTMLElement, hint?: string): Field {
  const err = h('div', { class: 'field-error', attrs: { role: 'alert', hidden: true } });
  const row = h('div', { class: 'wizard-field' },
    h('label', { text: label }),
    control,
    hint ? h('div', { class: 'field-hint', text: hint }) : null,
    err);
  return {
    el: row,
    control,
    error(msg: string): void {
      err.textContent = msg;
      if (msg) err.removeAttribute('hidden');
      else err.setAttribute('hidden', '');
    },
  };
}

/** button builds a plain or primary button. */
export function button(label: string, onClick: () => void, opts: { primary?: boolean; field?: string } = {}): HTMLElement {
  return h('button', {
    class: 'ui-modal-btn' + (opts.primary ? ' ui-modal-btn-primary' : ''),
    text: label,
    attrs: { type: 'button', 'data-action': opts.field },
    on: { click: onClick },
  });
}

/** section is a titled block. */
export function section(title: string, ...children: Child[]): HTMLElement {
  return h('div', { class: 'settings-section' }, h('h4', { text: title }), ...children);
}

/** badge is a small tag such as "paid". */
export function badge(label: string, kind = 'paid'): HTMLElement {
  return h('span', { class: `badge badge-${kind}`, text: label });
}

/**
 * matchKey maps the field name of a 400 onto one of keys: the name itself, or
 * what is left after dropping an index ("feedback_cmds[2]"), a "limits."
 * prefix or a trailing ".sub" ("models[0].model" -> "models").
 */
export function matchKey(field: string | undefined, keys: Iterable<string>): string | undefined {
  if (!field) return undefined;
  const set = new Set(keys);
  const tries = [field, field.replace(/^limits\./, ''), field.replace(/\[.*$/, ''), field.replace(/^limits\./, '').replace(/[[.].*$/, '')];
  return tries.find((k) => set.has(k));
}

/** FormErrors routes a failed write to the row it names, or to a form-level line. */
export class FormErrors {
  readonly general: HTMLElement = h('div', { class: 'field-error', attrs: { role: 'alert', hidden: true } });
  private fields = new Map<string, Field>();

  add(key: string, f: Field): Field {
    this.fields.set(key, f);
    return f;
  }

  clear(): void {
    for (const f of this.fields.values()) f.error('');
    this.setGeneral('');
  }

  private setGeneral(msg: string): void {
    this.general.textContent = msg;
    if (msg) this.general.removeAttribute('hidden');
    else this.general.setAttribute('hidden', '');
  }

  /** show displays err next to its field, or in the form-level line when it names none. */
  show(err: unknown): void {
    const message = err instanceof Error ? err.message : String(err);
    const key = err instanceof ApiError ? matchKey(err.field, this.fields.keys()) : undefined;
    if (key) this.fields.get(key)?.error(message);
    else this.setGeneral(message);
  }
}
