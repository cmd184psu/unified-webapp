// attention.ts — the banner for work that has stopped on its own (GET
// /api/attention): a failed draft, a halted plan or compile, an exhausted task,
// a task no model can take. Hidden while nothing waits. The tab title carries the
// count, so a background tab shows it, and an item that appears while the page is
// open raises a toast (and a desktop notification if the browser already allows
// them). Text goes through h().

import { showToast } from '@shared';
import { api, type AttentionItem } from './api.js';
import { h, replace } from './dom.js';
import { startPoll, subscribe } from './events.js';

/** toast is showToast that never throws: a toast is a courtesy. */
function toast(text: string, kind: 'success' | 'error'): void {
  try {
    void Promise.resolve(showToast(text, kind)).catch(() => {});
  } catch { /* no page to show it on */ }
}

export type AttentionDeps = Pick<typeof api, 'attention'> & Partial<Pick<typeof api, 'allowTaskFiles'>>;

/** itemKey identifies an item across loads, so only a new one raises a toast. */
export function itemKey(i: AttentionItem): string {
  return `${i.backend ?? i.project}/${i.effort}/${i.task ?? ''}/${i.kind}/${i.since}`;
}

/** titleWith puts the count in front of the page title, replacing an earlier count. */
export function titleWith(base: string, count: number): string {
  const bare = base.replace(/^\(\d+\) /, '');
  return count > 0 ? `(${count}) ${bare}` : bare;
}

export class AttentionBanner {
  private items: AttentionItem[] = [];
  private seen: Set<string> | null = null; // null until the first load: what was already there is not news
  private off: (() => void) | null = null;
  private stopPoll: (() => void) | null = null;

  constructor(private host: HTMLElement, private deps: AttentionDeps, private open: (slug: string) => void = () => {}) {}

  mount(): () => void {
    this.off = subscribe((ev) => { if (ev.type === 'state.changed' || ev.type === 'project.run_state') void this.load(); });
    this.stopPoll = startPoll(() => true, () => this.load());
    void this.load();
    return () => {
      this.off?.();
      this.stopPoll?.();
      this.off = this.stopPoll = null;
    };
  }

  async load(): Promise<void> {
    try {
      this.items = await this.deps.attention({ quiet: true });
    } catch {
      return;
    }
    this.announce();
    this.render();
  }

  private announce(): void {
    const keys = new Set(this.items.map(itemKey));
    if (this.seen) {
      for (const i of this.items) {
        if (this.seen.has(itemKey(i))) continue;
        const who = i.backend ? `backend ${i.backend}` : `${i.project}, effort ${i.effort}`;
        const text = `${who}: ${i.message}`;
        void showToast(text, 'error');
        try {
          if (typeof Notification !== 'undefined' && Notification.permission === 'granted') new Notification('AgentFlow needs you', { body: text });
        } catch { /* a notification is a courtesy */ }
      }
    }
    this.seen = keys;
  }

  render(): void {
    if (typeof document.title === 'string') document.title = titleWith(document.title, this.items.length);
    if (this.items.length === 0) {
      replace(this.host);
      this.host.setAttribute('hidden', '');
      return;
    }
    replace(this.host, h('div', { class: 'warn-box attention-banner', attrs: { role: 'alert', 'data-role': 'attention-banner' } },
      h('p', { text: this.items.length === 1 ? 'One thing has stopped and needs you:' : `${this.items.length} things have stopped and need you:` }),
      h('ul', {}, ...this.items.map((i) => h('li', {},
        i.backend ? h('strong', { text: `backend ${i.backend}` }) : h('a', { text: `${i.project}, effort ${i.effort}`, attrs: { href: '#', 'data-project': i.project }, on: { click: (e: Event) => { e.preventDefault(); this.open(i.project); } } }),
        h('span', { text: ` — ${i.message}` }),
        ...this.needsFiles(i))))));
    this.host.removeAttribute('hidden');
  }

  /** needsFiles is a task_needs_files item's file list and its Allow button (AF-13). */
  private needsFiles(i: AttentionItem): HTMLElement[] {
    const allow = this.deps.allowTaskFiles;
    if (i.kind !== 'task_needs_files' || !i.task || !i.needs_files?.length || !allow) return [];
    const task = i.task;
    return [
      h('ul', { class: 'needs-files' }, ...i.needs_files.map((f) => h('li', {}, h('code', { text: f.path }), h('span', { text: ` — ${f.reason}` })))),
      h('button', {
        text: i.needs_files.length === 1 ? 'Allow this file and retry' : `Allow these ${i.needs_files.length} files and retry`,
        attrs: { type: 'button', 'data-role': 'allow-files' },
        on: { click: async (e: Event) => {
          const btn = (e.currentTarget ?? e.target) as HTMLButtonElement | null;
          if (btn) btn.disabled = true;
          try {
            await allow(i.project, i.effort, task);
            toast(`${task}: files allowed; it continues in the same worktree`, 'success');
          } catch (err) {
            toast(`${task}: ${err instanceof Error ? err.message : String(err)}`, 'error');
            if (btn) btn.disabled = false;
          }
          void this.load();
        } },
      }),
    ];
  }
}
