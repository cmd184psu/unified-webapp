// overview.ts — the Overview tab (PJ5, RC5): every project with its run state,
// active effort, progress, current stage(s) and backend, kept live from the
// event stream, with start / pause / stop / run-one buttons (RC2).

import { confirmDialog } from '@shared';
import { api, type ProjectView, type RunOp, type WorkItem } from './api.js';
import { chip, h, replace } from './dom.js';
import { subscribe, type AgentflowEvent } from './events.js';

export interface OverviewDeps {
  list(): Promise<ProjectView[]>;
  run(slug: string, op: RunOp): Promise<ProjectView>;
  confirm(message: string): Promise<boolean>;
  /** Called when a project name is clicked. */
  open(slug: string): void;
}

export const defaultDeps = (open: (slug: string) => void): OverviewDeps => ({
  list: () => api.projects(),
  run: (slug, op) => api.run(slug, op),
  confirm: (m) => confirmDialog(m, { confirmLabel: 'Stop' }),
  open,
});

interface Button {
  op: RunOp;
  label: string;
  primary?: boolean;
}

/** buttonsFor lists the run controls that make sense in a run state. */
export function buttonsFor(runState: string): Button[] {
  switch (runState) {
    case 'running':
      return [{ op: 'pause', label: 'Pause' }, { op: 'stop', label: 'Stop' }];
    case 'pausing':
      return [{ op: 'start', label: 'Resume', primary: true }, { op: 'stop', label: 'Stop' }];
    case 'paused':
      return [
        { op: 'start', label: 'Resume', primary: true },
        { op: 'run-one', label: 'Run one' },
        { op: 'stop', label: 'Stop' },
      ];
    default:
      return [{ op: 'start', label: 'Start', primary: true }, { op: 'run-one', label: 'Run one' }];
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
  private loaded = false;
  private refreshing = false;
  private again = false;
  private off: (() => void) | null = null;

  constructor(private host: HTMLElement, private deps: OverviewDeps) {}

  /** mount fetches the rows and starts listening to the event stream; it returns the dispose. */
  mount(): () => void {
    this.off = subscribe((ev) => this.handle(ev));
    void this.refresh();
    return () => {
      this.off?.();
      this.off = null;
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
          h('button', { class: 'ui-modal-btn ui-modal-btn-primary', text: 'New project', attrs: { type: 'button', 'data-op': 'new-project' }, on: {
            click: () => this.deps.open('+new'),
          } })),
      );
      return;
    }
    replace(this.host, h('ul', { class: 'task-list' }, ...this.rows.map((r) => this.row(r))));
  }

  private row(p: ProjectView): HTMLElement {
    const { pct, text } = progressText(p.progress ?? {});
    const ef = p.effort;
    const bar = h('div', { class: 'progress-bar-wrap' }, h('div', { class: 'progress-bar', attrs: { style: `width: ${pct}%` } }));
    const work = (p.current ?? []).length > 0
      ? (p.current ?? []).map((w) => h('div', { class: 'task-item-meta', text: workLine(w) }))
      : [h('div', { class: 'task-item-meta', text: ef ? 'no stage running' : 'no active effort' })];
    const buttons = buttonsFor(p.run_state).map((b) =>
      h('button', {
        class: 'ui-modal-btn' + (b.primary ? ' ui-modal-btn-primary' : ''),
        text: b.label,
        attrs: { type: 'button', 'data-op': b.op },
        on: { click: () => void this.control(p.slug, b.op) },
      }));
    return h('li', { class: 'task-item', attrs: { 'data-project': p.slug } },
      h('div', { class: 'task-item-meta' },
        h('a', { class: 'task-item-title', text: p.slug, attrs: { href: '#', 'data-open': p.slug }, on: {
          click: (e) => { e.preventDefault(); this.deps.open(p.slug); },
        } }),
        chip(p.run_state)),
      h('div', { class: 'task-item-meta', text: ef ? `${ef.title} · ${ef.status.replace(/_/g, ' ')}` : 'no active effort' }),
      bar,
      h('div', { class: 'progress-pct', text: text }),
      ...work,
      h('div', { class: 'task-item-meta' }, ...buttons));
  }
}
