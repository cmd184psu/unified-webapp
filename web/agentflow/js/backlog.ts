// backlog.ts — the AF-15 backlog tab: prepared efforts that are parked (ready)
// and prepared efforts still waiting to be parked. Priority up/down changes
// the auto-start order; a parked effort can be unparked without starting it.

import { showToast } from '@shared';
import { api, type EffortView } from './api.js';
import { h, replace } from './dom.js';
import { badge, button } from './forms.js';

export interface BacklogDeps {
  backlog(): Promise<EffortView[]>;
  projects(): Promise<Array<{ slug: string }>>;
  efforts(slug: string): Promise<EffortView[]>;
  patchEffort(slug: string, id: number, patch: { priority?: number; start_condition?: string }): Promise<EffortView>;
  park(slug: string, id: number, priority?: number, condition?: string): Promise<EffortView>;
  unpark(slug: string, id: number): Promise<EffortView>;
  openProject(slug: string): void;
  notify(message: string): void;
}

export const defaultBacklogDeps = (openProject: (slug: string) => void): BacklogDeps => ({
  backlog: api.backlog,
  projects: api.projects,
  efforts: api.efforts,
  patchEffort: api.patchEffort,
  park: api.parkEffort,
  unpark: api.unparkEffort,
  openProject,
  notify: (m) => void showToast(m, 'success'),
});

export class Backlog {
  private ready: EffortView[] = [];
  private prepared: EffortView[] = [];
  private host = h('div', { class: 'backlog-host', attrs: { 'data-role': 'backlog' } });
  private error = h('p', { class: 'field-error', attrs: { hidden: '', 'data-role': 'backlog-error' } });
  private busy = false;

  constructor(private root: HTMLElement, private deps: BacklogDeps) {}

  mount(): () => void {
    replace(this.root, this.host);
    void this.load();
    return () => undefined;
  }

  async load(): Promise<void> {
    try {
      this.ready = await this.deps.backlog();
      const projects = await this.deps.projects();
      const prepared: EffortView[] = [];
      for (const p of projects) {
        const list = await this.deps.efforts(p.slug);
        prepared.push(...list.filter((e) => e.status === 'queue_approved'));
      }
      this.prepared = prepared;
      this.error.setAttribute('hidden', '');
      this.render();
    } catch {
      // apiFetch has already shown the failure.
    }
  }

  private priority(e: EffortView): number {
    return Number(e.priority ?? 0);
  }

  private async run(action: () => Promise<unknown>, said: string): Promise<void> {
    if (this.busy) return;
    this.busy = true;
    try {
      await action();
      this.deps.notify(said);
      await this.load();
    } catch {
      // apiFetch has already shown the failure.
    } finally {
      this.busy = false;
    }
  }

  private move(e: EffortView, delta: number): void {
    const next = this.priority(e) + delta;
    void this.run(() => this.deps.patchEffort(e.project, e.id, { priority: next }), `Priority ${next}`);
  }

  private condition(e: EffortView): HTMLElement {
    const input = h('input', {
      class: 'ui-input',
      attrs: { type: 'text', value: e.start_condition ?? '', placeholder: 'branch main contains feature/turn-loop', 'data-field': 'start_condition' },
    }) as HTMLInputElement;
    const save = button('Save condition', () => {
      void this.run(() => this.deps.patchEffort(e.project, e.id, { start_condition: input.value.trim() }), 'Start condition saved');
    }, { field: 'save-condition', icon: 'save' });
    return h('div', { class: 'inline-row' }, input, save);
  }

  private row(e: EffortView, prepared: boolean): HTMLElement {
    return h('div', { class: 'backlog-row', attrs: { 'data-effort': e.id, 'data-project': e.project } },
      h('div', { class: 'backlog-main' },
        h('div', { class: 'backlog-title' },
          h('strong', { text: e.title }),
          badge(e.status, e.status === 'ready' ? 'ok' : 'warn'),
          h('span', { class: 'field-hint', text: `${e.project} · #${e.id} · priority ${this.priority(e)}` })),
        h('div', { class: 'backlog-meta' },
          h('span', { text: `base ${e.base_commit ? e.base_commit.slice(0, 10) : 'not set'}` }),
          h('span', { text: e.budget_tokens || e.budget_seconds ? 'budget set' : 'no budget: auto-start skips it' })),
        prepared ? null : this.condition(e)),
      h('div', { class: 'backlog-actions' },
        button('Up', () => this.move(e, 1), { field: 'priority-up', icon: 'up', word: true }),
        button('Down', () => this.move(e, -1), { field: 'priority-down', icon: 'down', word: true }),
        prepared
          ? button('Park', () => void this.run(() => this.deps.park(e.project, e.id, this.priority(e), e.start_condition), 'Parked in the backlog'), { field: 'park', icon: 'pause', word: true })
          : button('Unpark', () => void this.run(() => this.deps.unpark(e.project, e.id), 'Unparked'), { field: 'unpark', icon: 'start', word: true }),
        button('Open', () => this.deps.openProject(e.project), { field: 'open-project', icon: 'view', word: true })));
  }

  private render(): void {
    replace(this.host,
      h('div', { class: 'section-head' },
        h('h2', { text: 'Backlog' }),
        button('Refresh', () => void this.load(), { field: 'backlog-refresh', icon: 'refresh', word: true }),
        h('p', { class: 'field-hint', text: 'Ready efforts start by themselves when fewer than the setting minimum are running, best priority first, with a budget set and their git condition met.' })),
      this.error,
      h('h3', { text: `Ready (${this.ready.length})` }),
      ...(this.ready.length ? this.ready.map((e) => this.row(e, false)) : [h('p', { class: 'field-hint', text: 'No parked efforts.' })]),
      h('h3', { text: `Prepared, not parked (${this.prepared.length})` }),
      ...(this.prepared.length ? this.prepared.map((e) => this.row(e, true)) : [h('p', { class: 'field-hint', text: 'No queue-approved efforts waiting to be parked.' })]));
  }
}
