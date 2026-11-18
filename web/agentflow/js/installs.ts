// installs.ts — "Install a model" on an Ollama backend's card (H6). The server
// pulls the model through the host's own API and streams progress as
// install.update events; this panel shows the form, a bar per running install
// and the last results, and survives a reload because the rows are stored.

import { api, type Install } from './api.js';
import { h, replace } from './dom.js';
import { subscribe } from './events.js';
import { badge, button, textInput, selectInput } from './forms.js';

export interface InstallDeps {
  installs(backend: string): Promise<Install[]>;
  start(backend: string, model: string, tier: number): Promise<Install>;
  cancel(id: number): Promise<void>;
}

export const defaultInstallDeps = (): InstallDeps => ({
  installs: (b) => api.installs(b),
  start: (b, m, t) => api.startInstall(b, m, t),
  cancel: (id) => api.cancelInstall(id),
});

const TIERS: Array<[string, string]> = [['0', 'not rated'], ['1', 'tier 1 (strong)'], ['2', 'tier 2 (capable)'], ['3', 'tier 3 (light)']];

/** size is bytes as a short GB/MB figure. */
export function size(n: number): string {
  if (n >= 1e9) return `${(n / 1e9).toFixed(1)} GB`;
  if (n >= 1e6) return `${Math.round(n / 1e6)} MB`;
  return `${n} B`;
}

/** progressText is "42% of 15.2 GB" for a layer in flight, else the host's own status. */
export function progressText(i: Install): string {
  if (i.state === 'running' && i.total > 0) return `${Math.round((100 * i.completed) / i.total)}% of ${size(i.total)}`;
  return i.status;
}

export class InstallPanel {
  /** el is the panel's own root: it is re-rendered in place, so a card can move it. */
  readonly el: HTMLElement = h('div', { class: 'install-panel', attrs: { 'data-role': 'install-panel' } });
  items: Install[] = [];
  private error = '';
  private model = '';
  private tier = '0';
  private off: (() => void) | null = null;

  constructor(readonly backend: string, private deps: InstallDeps) {}

  mount(): void {
    this.off = subscribe((ev) => {
      if (ev.type !== 'install.update' || ev.backend !== this.backend) return;
      const known = this.items.find((i) => i.id === ev.id);
      if (known) Object.assign(known, { state: ev.state, status: ev.status, completed: ev.completed, total: ev.total });
      else void this.load();
      this.render();
      if (known && ev.state !== 'running') void this.load(); // pick up the finished row's error text
    });
    void this.load();
  }

  unmount(): void {
    this.off?.();
    this.off = null;
  }

  async load(): Promise<void> {
    try {
      this.items = await this.deps.installs(this.backend);
    } catch {
      // The card still works without the list; the global banner reports a lost service.
    }
    this.render();
  }

  private async start(): Promise<void> {
    const model = this.model.trim();
    this.error = '';
    try {
      await this.deps.start(this.backend, model, Number(this.tier));
      this.model = '';
    } catch (e) {
      this.error = e instanceof Error ? e.message : String(e);
    }
    await this.load();
  }

  private row(i: Install): HTMLElement {
    const kind = i.state === 'done' ? 'ok' : i.state === 'running' ? 'muted' : 'warn';
    return h('div', { class: 'install-row', attrs: { 'data-install': String(i.id) } },
      h('strong', { text: i.model }), badge(i.state, kind),
      i.state === 'running' && i.total > 0 ? h('progress', { attrs: { value: String(i.completed), max: String(i.total) } }) : null,
      h('span', { class: 'field-hint', text: i.error ? i.error : progressText(i) }),
      i.state === 'running' ? button('Cancel', () => { void this.deps.cancel(i.id).then(() => this.load()); }, { field: 'cancel-install' }) : null);
  }

  render(): void {
    const model = textInput('install-model', this.model, { placeholder: 'model to install, e.g. devstral-small-2' });
    model.addEventListener('input', () => { this.model = model.value; });
    const tier = selectInput('install-tier', TIERS, this.tier);
    tier.addEventListener('change', () => { this.tier = tier.value; });
    replace(this.el,
      h('div', { class: 'install-form inline-row' }, model, tier,
        button('Install', () => { this.model = model.value; this.tier = tier.value; void this.start(); }, { field: 'install', primary: true, word: true, icon: 'add' })),
      this.error ? h('div', { class: 'field-error', text: this.error, attrs: { 'data-role': 'install-error' } }) : null,
      ...this.items.slice(0, 5).map((i) => this.row(i)));
  }
}
