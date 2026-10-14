// queue.ts — the Queue sub-tab (QA3): compile the approved plan, review every
// task of the draft queue (prompt, files_touched, acceptance criteria and
// dependencies) and approve the whole queue at once. The approval sends the
// hash of the queue that was reviewed; if the queue changed since, the server
// answers 409 and the view reloads so the new tasks can be reviewed. All task
// text is placed with h(), so it is text.

import { confirmDialog, showToast } from '@shared';
import { api, ApiError, type EffortView, type QueueView } from './api.js';
import { chooseEffort, effortBar } from './current.js';
import { chip, h, replace } from './dom.js';
import { subscribe, type AgentflowEvent } from './events.js';
import { FormErrors, badge, button, section } from './forms.js';

export type QueueDeps = Pick<typeof api, 'efforts' | 'queue' | 'compile' | 'approveQueue'> & {
  confirm(message: string, confirmLabel?: string): Promise<boolean>;
  notify(message: string): void;
};

export const defaultQueueDeps = (): QueueDeps => ({
  efforts: api.efforts,
  queue: api.queue,
  compile: api.compile,
  approveQueue: api.approveQueue,
  confirm: (m, label = 'Confirm') => confirmDialog(m, { confirmLabel: label }),
  notify: (m) => void showToast(m, 'success'),
});

const list = (title: string, items: string[] | null | undefined): HTMLElement =>
  h('div', { class: 'detail-section' }, h('h4', { text: title }),
    items?.length ? h('ul', { class: 'plan-review-notes' }, ...items.map((i) => h('li', { text: i }))) : h('p', { class: 'field-hint', text: 'none' }));

export class QueueApproval {
  efforts: EffortView[] = [];
  current: EffortView | undefined;
  queue: QueueView | null = null;
  private loaded = false;
  private errors = new FormErrors();
  private off: (() => void) | null = null;

  constructor(private host: HTMLElement, private slug: string, private deps: QueueDeps) {}

  mount(): () => void {
    this.off = subscribe((ev: AgentflowEvent) => {
      const mine = 'project' in ev && ev.project === this.slug;
      if (ev.type === 'state.changed' || (mine && (ev.type === 'effort.update' || ev.type === 'task.update'))) void this.refresh();
    });
    void this.load();
    return () => {
      this.off?.();
      this.off = null;
    };
  }

  async load(): Promise<void> {
    try {
      this.efforts = await this.deps.efforts(this.slug);
    } catch {
      if (!this.loaded) return;
    }
    this.loaded = true;
    this.current = chooseEffort(this.slug, this.efforts);
    await this.loadQueue();
  }

  private async refresh(): Promise<void> {
    try {
      this.efforts = await this.deps.efforts(this.slug);
      this.current = this.efforts.find((e) => e.id === this.current?.id) ?? chooseEffort(this.slug, this.efforts);
    } catch {
      return;
    }
    await this.loadQueue();
  }

  private async loadQueue(): Promise<void> {
    const ef = this.current;
    this.queue = null;
    if (ef) {
      try {
        this.queue = await this.deps.queue(this.slug, ef.id, { quiet: true });
      } catch (err) {
        if (!(err instanceof ApiError && (err.status === 404 || err.status === 409))) this.errors.show(err);
      }
    }
    this.render();
  }

  async compile(): Promise<void> {
    const ef = this.current;
    if (!ef) return;
    try {
      await this.deps.compile(this.slug, ef.id);
    } catch {
      return;
    }
    this.deps.notify('Compiling the plan into tasks');
    await this.refresh();
  }

  /** approve sends the hash of the queue on screen; a 409 means it changed, so it reloads for another review. */
  async approve(): Promise<void> {
    const ef = this.current;
    const q = this.queue;
    if (!ef || !q) return;
    if (!(await this.deps.confirm(`Approve all ${q.tasks.length} task${q.tasks.length === 1 ? '' : 's'} of revision ${q.revision}? Once approved the queue can run.`, 'Approve queue'))) return;
    this.errors.clear();
    try {
      await this.deps.approveQueue(this.slug, ef.id, q.hash, { quiet: true });
    } catch (err) {
      if (err instanceof ApiError && err.status === 409) {
        this.deps.notify('The queue changed since you opened it; review it again');
        await this.refresh();
        this.errors.show(new Error(`The queue changed while you were reviewing it, so nothing was approved. (${err.message})`));
        return;
      }
      this.errors.show(err);
      return;
    }
    this.deps.notify('Queue approved; start the run from the Board tab');
    await this.refresh();
  }

  private taskCard(t: QueueView['tasks'][number]): HTMLElement {
    return h('div', { class: 'profile-card', attrs: { 'data-task': t.key } },
      h('div', { class: 'profile-header' }, h('strong', { text: `${t.key} · ${t.title}` }), t.phase ? badge(t.phase, 'muted') : null),
      t.waiting_reason ? h('p', { class: 'field-hint', text: t.waiting_reason }) : null,
      h('div', { class: 'detail-section' }, h('h4', { text: 'Prompt' }), h('pre', { class: 'detail-mono', text: t.prompt })),
      list('Acceptance criteria', t.acceptance),
      list('Files touched', t.files_touched),
      list('Depends on', t.depends_on));
  }

  render(): void {
    if (!this.loaded) {
      replace(this.host, h('p', { class: 'field-hint', text: 'Loading efforts…' }));
      return;
    }
    const ef = this.current;
    if (!ef) {
      replace(this.host, h('div', { class: 'plan-empty' }, h('h3', { text: 'No effort yet' }), h('p', { text: 'Create an effort on the FRD tab first.' })));
      return;
    }
    const q = this.queue;
    const canCompile = ef.status === 'plan_approved' || (ef.status === 'queue_draft' && !!q);
    const draft = !!q && q.status === 'draft';
    replace(this.host,
      h('div', { class: 'settings-host' },
        effortBar(this.slug, this.efforts, ef, (id) => { this.current = this.efforts.find((e) => e.id === id); void this.loadQueue(); }),
        q ? h('div', { class: 'plan-header', attrs: { 'data-role': 'queue-header' } },
          h('div', { class: 'plan-meta' },
            chip(q.status),
            h('span', { class: 'plan-frd-id', text: `revision ${q.revision} · ${q.tasks.length} task${q.tasks.length === 1 ? '' : 's'}` }),
            h('code', { class: 'plan-frd-id', text: `hash ${q.hash.slice(0, 12)}`, attrs: { 'data-role': 'queue-hash' } }),
            q.approved_by ? badge(`approved by ${q.approved_by}`, 'ok') : null),
          h('div', { class: 'plan-actions' },
            canCompile ? button('Recompile', () => void this.compile(), { field: 'compile' }) : null,
            draft ? button('Approve whole queue', () => void this.approve(), { primary: true, field: 'approve-queue' }) : null))
          : h('div', { class: 'plan-empty' },
            h('h3', { text: 'No queue yet' }),
            h('p', { text: ef.status === 'plan_approved' ? 'The plan is approved. Compile it into tasks to review.' : 'Approve a plan first, then compile it into tasks.' }),
            ef.status === 'plan_approved' ? button('Compile plan', () => void this.compile(), { primary: true, field: 'compile' }) : null),
        this.errors.general,
        q ? section('Tasks', h('div', { class: 'profile-list' }, ...q.tasks.map((t) => this.taskCard(t)))) : null));
  }
}

/** mountQueue is the Queue sub-tab's mount. */
export function mountQueue(host: HTMLElement, slug: string): () => void {
  return new QueueApproval(host, slug, defaultQueueDeps()).mount();
}
