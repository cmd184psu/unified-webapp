// activity.ts — the strip above a project's tabs that says what the project
// is doing right now, whichever tab is open: working (animated, with the time
// and, while running, the task and model), waiting for the owner, or idle.
// It reads the efforts list, the draft state and the tasks; it never writes.

import { api, type DraftState, type EffortView, type TaskView } from './api.js';
import { chooseEffort } from './current.js';
import { h, replace } from './dom.js';
import { isTransient, startPoll, subscribe } from './events.js';
import { icon } from './icons.js';

export interface ActivityDeps {
  efforts: (slug: string) => Promise<EffortView[]>;
  draftState: (slug: string, id: number) => Promise<DraftState>;
  tasks: (slug: string, id: number) => Promise<TaskView[]>;
}

export interface Line {
  /** busy: the engine or a model is working; wait: it is the owner's turn; rest: nothing to do. */
  kind: 'busy' | 'wait' | 'rest';
  text: string;
  /** The sub-tab that shows it. */
  tab: string;
  /** Epoch ms the work began, for the running clock; 0 for none. */
  since: number;
}

/** describe turns an effort's state into the line shown; tasks is only read while running. */
export function describe(ef: EffortView, draft: DraftState | null, tasks: TaskView[]): Line {
  const n = `#${ef.id}`;
  switch (ef.status) {
    case 'draft':
      if (draft?.state === 'running') {
        const what = draft.purpose === 'split' ? 'Splitting the FRD into parts' : draft.purpose === 'polish' ? 'Tidying the FRD' : 'Drafting the FRD';
        return { kind: 'busy', text: `${what}…`, tab: 'frd', since: draft.started ?? 0 };
      }
      if (ef.frd?.latest) return { kind: 'wait', text: `${n}: review the FRD and approve it`, tab: 'frd', since: 0 };
      return { kind: 'wait', text: `${n}: describe what to build`, tab: 'frd', since: 0 };
    case 'planning':
      return { kind: 'busy', text: `${n}: planning…`, tab: 'plan', since: ef.updated };
    case 'plan_ready':
      return { kind: 'wait', text: `${n}: review the plan and approve it`, tab: 'plan', since: 0 };
    case 'plan_approved':
    case 'compiling':
      return { kind: 'busy', text: `${n}: turning the plan into tasks…`, tab: 'queue', since: ef.updated };
    case 'queue_draft':
      return { kind: 'wait', text: `${n}: review the queue and approve it`, tab: 'queue', since: 0 };
    case 'queue_approved':
      return { kind: 'wait', text: `${n}: queue approved; start the run`, tab: 'board', since: 0 };
    case 'running': {
      const halted = tasks.find((t) => t.halted);
      if (halted) return { kind: 'wait', text: `${n}: ${halted.key} needs attention`, tab: 'board', since: 0 };
      const cur = tasks.map((t) => t.current).find((c) => c);
      const done = tasks.filter((t) => t.status === 'passed').length;
      const progress = tasks.length ? ` (${done}/${tasks.length} done)` : '';
      return {
        kind: 'busy',
        text: cur ? `${n}: ${cur.task} ${cur.role} on ${cur.model || cur.backend}${progress}` : `${n}: running${progress}`,
        tab: 'board',
        since: 0,
      };
    }
    case 'paused':
      return { kind: 'rest', text: `${n}: paused`, tab: 'board', since: 0 };
    case 'complete':
      return { kind: 'rest', text: `${n}: complete; ready to merge`, tab: 'board', since: 0 };
    case 'abandoned':
      return { kind: 'rest', text: `${n}: abandoned`, tab: 'history', since: 0 };
    default:
      return { kind: 'rest', text: `${n}: ${ef.status.replace(/_/g, ' ')}`, tab: 'frd', since: 0 };
  }
}

function clock(ms: number): string {
  const s = Math.max(0, Math.floor(ms / 1000));
  return s < 60 ? `${s}s` : `${Math.floor(s / 60)}m ${String(s % 60).padStart(2, '0')}s`;
}

export class Activity {
  private slug = '';
  private line: Line | null = null;
  private ticker: ReturnType<typeof setInterval> | null = null;
  private clockEl: HTMLElement | null = null;
  private off: (() => void) | null = null;
  private stopPoll: (() => void) | null = null;
  private disposed = false;
  private seq = 0;

  constructor(
    readonly el: HTMLElement,
    private open: (tab: string) => void,
    private deps: ActivityDeps = { efforts: api.efforts, draftState: (s, i) => api.draftState(s, i, { quiet: true }), tasks: api.tasks },
  ) {}

  mount(slug: string): () => void {
    this.slug = slug;
    this.off = subscribe((ev) => {
      if (['state.changed', 'effort.update', 'project.update', 'draft.update'].includes(ev.type)) void this.refresh();
    });
    // Drafting and the engine's own stages change without an event reaching us, so poll while they run.
    this.stopPoll = startPoll(() => this.line?.kind === 'busy', () => this.refresh());
    void this.refresh();
    return () => {
      this.disposed = true;
      this.stopTicker();
      this.off?.();
      this.stopPoll?.();
    };
  }

  /** setProject points the strip at another project. */
  setProject(slug: string): void {
    this.slug = slug;
    this.line = null;
    this.render();
    void this.refresh();
  }

  async refresh(): Promise<void> {
    if (this.disposed) return;
    const slug = this.slug;
    const seq = ++this.seq;
    let line: Line | null = null;
    try {
      const ef = slug ? chooseEffort(slug, await this.deps.efforts(slug)) : undefined;
      if (ef) {
        const draft = ef.status === 'draft' ? await this.deps.draftState(slug, ef.id).catch(() => null) : null;
        const tasks = ef.status === 'running' ? await this.deps.tasks(slug, ef.id).catch(() => []) : [];
        line = describe(ef, draft, tasks);
      }
    } catch {
      return; // apiFetch already told the user; keep what is shown
    }
    if (this.disposed || seq !== this.seq || slug !== this.slug) return;
    this.line = line;
    this.render();
  }

  private stopTicker(): void {
    if (this.ticker) clearInterval(this.ticker);
    this.ticker = null;
    this.clockEl = null;
  }

  private render(): void {
    this.stopTicker();
    const l = this.line;
    if (!l) {
      replace(this.el);
      this.el.setAttribute('hidden', '');
      return;
    }
    this.el.removeAttribute('hidden');
    this.el.setAttribute('data-kind', l.kind);
    const clockEl = l.since > 0 ? h('span', { class: 'field-hint', attrs: { 'data-role': 'elapsed' }, text: clock(Date.now() - l.since) }) : null;
    this.clockEl = clockEl;
    if (clockEl && l.since > 0) {
      const since = l.since;
      this.ticker = setInterval(() => { if (this.clockEl) this.clockEl.textContent = clock(Date.now() - since); }, 1000);
    }
    replace(this.el,
      h('span', { class: 'activity-dot', attrs: { 'aria-hidden': 'true' } }),
      h('span', { class: 'activity-text', text: l.text, attrs: { 'data-role': 'activity-text' } }),
      clockEl,
      h('button', {
        class: 'ui-modal-btn af-has-icon af-icon-only',
        attrs: { type: 'button', title: 'Open it', 'aria-label': 'Open it', 'data-action': 'open-activity' },
        on: { click: () => this.open(l.tab) },
      }, icon('next')));
  }
}

/** transientNow is true while the shown effort is in a status the engine changes by itself. */
export { isTransient };
