// dom.ts — the only way agentflow code puts model-, user- or backend-supplied
// text on the page (UI1). Elements are built with createElement and filled with
// text nodes, so the text can never become markup. esc() is for the rare string
// that must go through an HTML string (the escape gate checks for it).

const ESCAPES: Record<string, string> = {
  '&': '&amp;',
  '<': '&lt;',
  '>': '&gt;',
  '"': '&quot;',
  "'": '&#39;',
};

/** esc returns s with HTML metacharacters escaped; null and undefined give "". */
export function esc(s: unknown): string {
  if (s === null || s === undefined) return '';
  return String(s).replace(/[&<>"']/g, (c) => ESCAPES[c]);
}

export type Child = Node | string | number | null | undefined | false;

export interface Props {
  /** className */
  class?: string;
  /** textContent (set before the children are appended) */
  text?: string;
  /** attributes; names starting with "on" are refused, handlers go in `on` */
  attrs?: Record<string, string | number | boolean | null | undefined>;
  /** event listeners by event name */
  on?: Record<string, (ev: Event) => void>;
}

/** h builds an element; string and number children become text nodes. */
export function h(tag: string, props: Props = {}, ...children: Child[]): HTMLElement {
  const el = document.createElement(tag);
  if (props.class) el.className = props.class;
  if (props.text !== undefined) el.textContent = props.text;
  for (const [name, value] of Object.entries(props.attrs ?? {})) {
    if (/^on/i.test(name)) throw new Error(`h: attribute ${name} refused; use props.on`);
    if (value === null || value === undefined || value === false) continue;
    el.setAttribute(name, value === true ? '' : String(value));
  }
  for (const [name, fn] of Object.entries(props.on ?? {})) el.addEventListener(name, fn);
  append(el, children);
  return el;
}

/** append adds children to el; falsy placeholders are skipped. */
export function append(el: HTMLElement, children: Child[]): void {
  for (const c of children) {
    if (c === null || c === undefined || c === false) continue;
    el.appendChild(typeof c === 'string' || typeof c === 'number' ? document.createTextNode(String(c)) : c);
  }
}

/** replace empties host and fills it with children. */
export function replace(host: HTMLElement, ...children: Child[]): void {
  host.textContent = '';
  append(host, children);
}

// Server statuses whose chip colour comes from an older class name.
const CHIP_ALIAS: Record<string, string> = {
  running: 'in-progress',
  pausing: 'review',
  paused: 'review',
  // Plan review verdicts (RP2).
  approve: 'passed',
  revise: 'repair',
  reject: 'failed',
};

/** chip builds a status chip; needs_input gets status-needs-input, running reuses status-in-progress. */
export function chip(status: string): HTMLElement {
  const key = status.toLowerCase().replace(/[^a-z0-9]+/g, '-');
  return h('span', { class: `status-chip status-${CHIP_ALIAS[key] ?? key}`, text: status.replace(/_/g, ' ') });
}
