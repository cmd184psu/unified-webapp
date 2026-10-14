// history.ts — the History sub-tab (DBS6): every effort of the project with
// its FRD, plan and queue state, export links (JSON and rendered markdown for
// the effort, its FRD and its plan) and the FRD import. The YOLO toggle sits on
// each active effort (YO1). Titles and server messages are placed with h(), so
// they are text.

import { showToast } from '@shared';
import { api, exportURL, type EffortView } from './api.js';
import { rememberEffort } from './current.js';
import { chip, h, replace } from './dom.js';
import { isTransient, startPoll, subscribe, type AgentflowEvent } from './events.js';
import { FormErrors, button, fieldRow, section } from './forms.js';
import { defaultYoloDeps, yoloBadge, yoloControl, type YoloDeps } from './yolo.js';

export type HistoryDeps = Pick<typeof api, 'efforts' | 'importFRD'> & {
  /** Reads the chosen file's text. */
  readFile(file: unknown): Promise<string>;
  notify(message: string): void;
};

export const defaultHistoryDeps = (): HistoryDeps => ({
  efforts: api.efforts,
  importFRD: api.importFRD,
  readFile: (f) => (f as File).text(),
  notify: (m) => void showToast(m, 'success'),
});

const when = (ms: number): string => (ms ? new Date(ms).toLocaleString() : '');

function link(label: string, href: string, name: string): HTMLElement {
  return h('a', { class: 'ui-modal-btn', text: label, attrs: { href, download: '', 'data-export': name } });
}

export class History {
  efforts: EffortView[] = [];
  private loaded = false;
  private errors = new FormErrors();
  private fileInput = h('input', { attrs: { type: 'file', accept: '.json,application/json', name: 'frd_file', 'data-field': 'frd_file' } }) as HTMLInputElement;
  private off: (() => void) | null = null;

  private stopPoll: (() => void) | null = null;

  constructor(private host: HTMLElement, private slug: string, private deps: HistoryDeps, private yolo: YoloDeps) {}

  mount(): () => void {
    this.off = subscribe((ev: AgentflowEvent) => {
      const mine = 'project' in ev && ev.project === this.slug;
      if (ev.type === 'state.changed' || (mine && (ev.type === 'effort.update' || ev.type === 'project.update'))) void this.load();
    });
    this.stopPoll = startPoll(() => this.efforts.some((e) => isTransient(e.status)), () => this.load());
    void this.load();
    return () => {
      this.off?.();
      this.off = null;
      this.stopPoll?.();
      this.stopPoll = null;
    };
  }

  async load(): Promise<void> {
    try {
      this.efforts = await this.deps.efforts(this.slug);
    } catch {
      if (!this.loaded) return;
    }
    this.loaded = true;
    this.render();
  }

  /** importFile posts the chosen FRD JSON; the server validates it and names a bad field. */
  async importFile(): Promise<void> {
    this.errors.clear();
    const file = this.fileInput.files?.[0];
    if (!file) {
      this.errors.show(new Error('Choose a JSON file first'));
      return;
    }
    try {
      const text = await this.deps.readFile(file);
      const ef = await this.deps.importFRD(this.slug, text, { quiet: true });
      rememberEffort(this.slug, ef.id);
    } catch (err) {
      this.errors.show(err);
      return;
    }
    this.deps.notify('FRD imported as a new effort');
    await this.load();
  }

  private row(e: EffortView): HTMLElement {
    return h('li', { class: 'profile-card', attrs: { 'data-effort': e.id } },
      h('div', { class: 'profile-header' },
        h('span', { class: 'row-actions' }, h('strong', { text: `#${e.id} ${e.title}` }), chip(e.status), e.yolo ? yoloBadge() : null),
        h('span', { class: 'field-hint', text: when(e.created) })),
      h('div', { class: 'task-item-meta', text:
        `FRD ${e.frd.latest ? `revision ${e.frd.latest}${e.frd.approved ? ` (approved ${e.frd.approved})` : ''}` : 'none'} · ` +
        `plan ${e.plan.latest ? `revision ${e.plan.latest}${e.plan.approved ? ` (approved ${e.plan.approved})` : ''}` : 'none'} · ` +
        `queue ${e.queue ? `revision ${e.queue.revision} ${e.queue.status}` : 'none'}` }),
      h('div', { class: 'row-actions' },
        link('Effort JSON', exportURL(e.id, 'json'), 'effort-json'),
        link('Effort Markdown', exportURL(e.id, 'md'), 'effort-md'),
        e.frd.latest ? link('FRD JSON', exportURL(e.id, 'json', 'frd'), 'frd-json') : null,
        e.frd.latest ? link('FRD Markdown', exportURL(e.id, 'md', 'frd'), 'frd-md') : null,
        e.plan.latest ? link('Plan JSON', exportURL(e.id, 'json', 'plan'), 'plan-json') : null,
        e.plan.latest ? link('Plan Markdown', exportURL(e.id, 'md', 'plan'), 'plan-md') : null),
      yoloControl(this.slug, e, this.yolo, () => void this.load()));
  }

  render(): void {
    if (!this.loaded) {
      replace(this.host, h('p', { class: 'field-hint', text: 'Loading efforts…' }));
      return;
    }
    this.errors = new FormErrors();
    const fileRow = this.errors.add('body', fieldRow('FRD file', this.fileInput, 'A JSON FRD, for example one exported from here; it becomes a new effort at revision 1'));
    this.errors.add('frd', fileRow);
    replace(this.host,
      h('div', { class: 'settings-host' },
        section('Efforts',
          this.efforts.length
            ? h('ul', { class: 'profile-list', attrs: { 'data-role': 'history-list' } }, ...[...this.efforts].reverse().map((e) => this.row(e)))
            : h('p', { class: 'field-hint', text: 'No efforts yet.' })),
        section('Import an FRD', fileRow.el, this.errors.general,
          h('div', { class: 'settings-actions' }, button('Import FRD', () => void this.importFile(), { primary: true, field: 'import-frd', icon: 'import' })))));
  }
}

/** mountHistory is the History sub-tab's mount. */
export function mountHistory(host: HTMLElement, slug: string): () => void {
  return new History(host, slug, defaultHistoryDeps(), defaultYoloDeps()).mount();
}
