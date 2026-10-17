// overview.ts — the Overview tab (PJ5, RC5): every project with its run state,
// active effort, progress, current stage(s) and backend, kept live from the
// event stream, with start / pause / stop / run-one buttons (RC2).

import { confirmDialog, copyText } from '@shared';
import { api, type Effort, type GitCommands, type ProjectView, type RunOp, type WorkItem } from './api.js';
import { chip, h, replace } from './dom.js';
import { button } from './forms.js';
import type { IconName } from './icons.js';
import { isTransient, startPoll, subscribe, type AgentflowEvent } from './events.js';
import { mergeCard } from './merge-card.js';
import { yoloBadge } from './yolo.js';
import { defaultDropAllDeps, defaultUnregisterDeps, dropAll, unregister, unregisterBlock } from './unregister.js';

export interface OverviewDeps {
  list(): Promise<ProjectView[]>;
  run(slug: string, op: RunOp): Promise<ProjectView>;
  confirm(message: string): Promise<boolean>;
  /** Called when a project name is clicked. */
  open(slug: string): void;
  /** Optional: with these the row of a project whose newest effort is complete shows the Ready to merge card. */
  efforts?: typeof api.efforts;
  gitCommands?: typeof api.gitCommands;
  copy?(text: string): void;
  /** Optional: unregisters a project after asking; true when it is gone. */
  unregister?(slug: string): Promise<boolean>;
  /** Optional: drops every project and effort after asking; true when it did. */
  dropAll?(): Promise<boolean>;
}

export const defaultDeps = (open: (slug: string) => void): OverviewDeps => ({
  list: () => api.projects(),
  run: (slug, op) => api.run(slug, op),
  confirm: (m) => confirmDialog(m, { confirmLabel: 'Stop' }),
  open,
  efforts: api.efforts,
  gitCommands: api.gitCommands,
  copy: (t) => void copyText(t),
  unregister: (slug) => unregister(defaultUnregisterDeps(), slug),
  dropAll: () => dropAll(defaultDropAllDeps()),
});

interface Button {
  op: RunOp;
  label: string;
  primary?: boolean;
  icon: IconName;
  /** keep the label beside the glyph where the glyph alone is ambiguous */
  word?: boolean;
}

/** buttonsFor lists the run controls that make sense in a run state. */
export function buttonsFor(runState: string): Button[] {
  switch (runState) {
    case 'running':
      return [{ op: 'pause', label: 'Pause', icon: 'pause' }, { op: 'stop', label: 'Stop', icon: 'stop' }];
    case 'pausing':
      return [{ op: 'start', label: 'Resume', primary: true, icon: 'start' }, { op: 'stop', label: 'Stop', icon: 'stop' }];
    case 'paused':
      return [
        { op: 'start', label: 'Resume', primary: true, icon: 'start' },
        { op: 'run-one', label: 'Run one', icon: 'start', word: true },
        { op: 'stop', label: 'Stop', icon: 'stop' },
      ];
    default:
      return [{ op: 'start', label: 'Start', primary: true, icon: 'start' }, { op: 'run-one', label: 'Run one', icon: 'start', word: true }];
  }
}

/** progressText is "NN% — passed X/Y · running N · ..." from a project's progress counts. */
export function progressText(progress: Record<string, number>): { pct: number; text: string } {
  const total = progress.total ?? 0;
  const done = (progress.passed ?? 0) + (progress.overridden ?? 0);
  const pct = total > 0 ? Math.round((done / total) * 100) : 0;
  const rest = Object.keys(progress)
    .filter((k) => k !== 'total' && k !== 'passed' && progress[k] > 0)
    .sort()
    .map((k) => `${k.replace(/_/g, ' ')} ${progress[k]}`);
  const head = total > 0 ? `${pct}% — passed ${progress.passed ?? 0}/${total}` : 'no queue yet';
  return { pct, text: [head, ...rest].join(' · ') };
}

function workLine(w: WorkItem): string {
  return `${w.role} · ${w.task || 'effort'}${w.attempt ? ` (attempt ${w.attempt})` : ''} · ${w.backend}/${w.model} · ${w.state}`;
}

export class Overview {
  rows: ProjectView[] = [];
  /** Per project, the newest effort when it is complete (the project row's active effort is then null). */
  private done = new Map<string, Effort>();
  private git = new Map<number, GitCommands | null>();
  private loaded = false;
  private refreshing = false;
  private again = false;
  private off: (() => void) | null = null;
  private stopPoll: (() => void) | null = null;

  constructor(private host: HTMLElement, private deps: OverviewDeps) {}

  /** mount fetches the rows and starts listening to the event stream; it returns the dispose. */
  mount(): () => void {
    this.off = subscribe((ev) => this.handle(ev));
    this.stopPoll = startPoll(() => this.rows.some((r) => isTransient(r.effort?.status)), () => this.refresh());
    void this.refresh();
    return () => {
      this.off?.();
      this.off = null;
      this.stopPoll?.();
      this.stopPoll = null;
    };
  }

  /** refresh refetches the rows; overlapping calls collapse into one follow-up fetch. */
  async refresh(): Promise<void> {
    if (this.refreshing) {
      this.again = true;
      return;
    }
    this.refreshing = true;
    try {
      do {
        this.again = false;
        try {
          this.rows = await this.deps.list();
          await this.loadCompleted();
          this.loaded = true;
        } catch {
          // apiFetch already told the user; keep what is shown.
        }
        this.render();
      } while (this.again);
    } finally {
      this.refreshing = false;
    }
  }

  /** loadCompleted finds, for each project without an active effort, its newest effort when that is complete, with its git commands. */
  private async loadCompleted(): Promise<void> {
    const { efforts, gitCommands } = this.deps;
    this.done.clear();
    if (!efforts) return;
    for (const p of this.rows.filter((r) => !r.effort)) {
      try {
        const newest = (await efforts(p.slug)).reduce<Effort | undefined>((m, e) => (!m || e.id > m.id ? e : m), undefined);
        if (!newest || newest.status !== 'complete') continue;
        this.done.set(p.slug, newest);
        if (gitCommands && !this.git.has(newest.id)) this.git.set(newest.id, await gitCommands(p.slug, newest.id, { quiet: true }).catch(() => null));
      } catch {
        // keep the row without a card
      }
    }
  }

  /** handle applies one stream event: run-state changes show at once, the rest refetch. */
  handle(ev: AgentflowEvent): void {
    switch (ev.type) {
      case 'snapshot': {
        let stale = ev.projects.length !== this.rows.length;
        for (const p of ev.projects) {
          const row = this.rows.find((r) => r.slug === p.project);
          if (!row || (row.effort?.id ?? 0) !== p.effort) stale = true;
          else row.run_state = p.state;
        }
        this.render();
        if (stale) void this.refresh();
        return;
      }
      case 'project.run_state': {
        const row = this.rows.find((r) => r.slug === ev.project);
        if (row) {
          row.run_state = ev.state;
          this.render();
        }
        void this.refresh();
        return;
      }
      case 'project.update':
      case 'project.delete':
      case 'effort.update':
      case 'task.update':
      case 'state.changed':
        void this.refresh();
        return;
      default:
        return;
    }
  }

  private async control(slug: string, op: RunOp): Promise<void> {
    if (op === 'stop' && !(await this.deps.confirm(`Stop ${slug}? Running stages are cancelled.`))) return;
    try {
      const view = await this.deps.run(slug, op);
      const i = this.rows.findIndex((r) => r.slug === slug);
      if (i >= 0) this.rows[i] = view;
      this.render();
    } catch {
      // apiFetch already showed the error.
    }
  }

  render(): void {
    if (!this.loaded) {
      replace(this.host, h('p', { class: 'field-hint', text: 'Loading projects…' }));
      return;
    }
    if (this.rows.length === 0) {
      replace(
        this.host,
        h('div', { class: 'plan-empty' },
          h('h3', { text: 'No projects yet' }),
          h('p', { text: 'Create a project to get started.' }),
          button('New project', () => this.deps.open('+new'), { primary: true, op: 'new-project', icon: 'add' })),
      );
      return;
    }
    const drop = this.deps.dropAll;
    replace(this.host,
      drop ? h('div', { class: 'settings-actions' },
        button('Drop all projects', () => void this.dropEverything(), { op: 'drop-all', icon: 'trash', word: true })) : null,
      h('ul', { class: 'task-list' }, ...this.rows.map((r) => this.row(r))));
  }

  private async dropEverything(): Promise<void> {
    if (!(await this.deps.dropAll?.())) return;
    await this.refresh();
  }

  private async unregisterRow(slug: string): Promise<void> {
    const row = this.rows.find((r) => r.slug === slug);
    if (!row || unregisterBlock(row) || !(await this.deps.unregister?.(slug))) return;
    await this.refresh();
  }

  private row(p: ProjectView): HTMLElement {
    const { pct, text } = progressText(p.progress ?? {});
    const ef = p.effort;
    const bar = h('div', { class: 'progress-bar-wrap' }, h('div', { class: 'progress-bar', attrs: { style: `width: ${pct}%` } }));
    const work = (p.current ?? []).length > 0
      ? (p.current ?? []).map((w) => h('div', { class: 'task-item-meta', text: workLine(w) }))
      : [h('div', { class: 'task-item-meta', text: ef ? 'no stage running' : 'no active effort' })];
    // "stopped" and Start mean nothing while the effort is still being drafted, planned or compiled.
    const runnable = p.run_state !== 'stopped' || (!!ef && ['queue_approved', 'running', 'paused'].includes(ef.status));
    const buttons = (runnable ? buttonsFor(p.run_state) : []).map((b) =>
      button(b.label, () => void this.control(p.slug, b.op), { primary: b.primary, op: b.op, icon: b.icon, word: b.word }));
    const why = unregisterBlock(p);
    const trash = this.deps.unregister
      ? button(why ? `Unregister: ${why}` : 'Unregister', () => void this.unregisterRow(p.slug), { field: 'unregister-project', icon: 'trash' })
      : null;
    if (trash && why) (trash as HTMLButtonElement).disabled = true;
    return h('li', { class: 'task-item', attrs: { 'data-project': p.slug } },
      h('div', { class: 'task-item-meta' },
        h('a', { class: 'task-item-title', text: p.slug, attrs: { href: '#', 'data-open': p.slug }, on: {
          click: (e) => { e.preventDefault(); this.deps.open(p.slug); },
        } }),
        runnable ? chip(p.run_state) : null,
        ef?.yolo ? yoloBadge() : null),
      h('div', { class: 'task-item-meta', text: ef ? `${ef.title} · ${ef.status.replace(/_/g, ' ')}` : 'no active effort' }),
      bar,
      h('div', { class: 'progress-pct', text: text }),
      ...work,
      h('div', { class: 'task-item-meta' }, ...buttons, trash),
      this.done.has(p.slug) ? mergeCard(this.done.get(p.slug) as Effort, this.git.get((this.done.get(p.slug) as Effort).id) ?? null, (t) => this.deps.copy?.(t)) : null);
  }
}
