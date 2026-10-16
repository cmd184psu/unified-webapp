// stats.ts — the Stats tab: how each model has done, per role (MB8, H3b). It
// has a tab of its own so the evidence for rating a model (tier) is one click
// away, not low on the Backends page. Filter by role and by project; the table
// refetches when stages finish.

import { api, type ModelStat, type ProjectView } from './api.js';
import { h, replace } from './dom.js';
import { subscribe } from './events.js';
import { badge, button, fieldRow, section, selectInput } from './forms.js';

export interface StatsDeps {
  stats(project: string): Promise<ModelStat[]>;
  projects(): Promise<ProjectView[]>;
  clear(backend: string, model: string, role: string): Promise<void>;
}

export const defaultStatsDeps = (): StatsDeps => ({
  stats: (project) => api.modelStats(project),
  projects: () => api.projects(),
  clear: (backend, model, role) => api.clearStrikes(backend, model, role),
});

const pct = (r: number): string => `${Math.round(r * 100)}%`;

/** duration is a compact length of time: 45s, 7.2m, 2.4h. */
export function duration(ms: number): string {
  if (ms <= 0) return '-';
  const s = ms / 1000;
  if (s < 90) return `${Math.round(s)}s`;
  if (s < 5400) return `${(s / 60).toFixed(1)}m`;
  return `${(s / 3600).toFixed(1)}h`;
}

/** tokensShort is a compact token count: 950, 12.4k, 5.5M. */
export function tokensShort(n: number): string {
  if (n <= 0) return '-';
  if (n < 1000) return String(n);
  if (n < 1_000_000) return `${(n / 1000).toFixed(1)}k`;
  return `${(n / 1_000_000).toFixed(1)}M`;
}

const ROLES = ['', 'planner', 'architect', 'critic', 'compiler', 'worker', 'reviewer'];

export class StatsView {
  rows: ModelStat[] = [];
  projects: ProjectView[] = [];
  role = '';
  project = '';
  loaded = false;
  private disposed = false;
  private busy = false;

  constructor(private host: HTMLElement, private deps: StatsDeps) {}

  mount(): () => void {
    void this.load();
    const off = subscribe((ev) => {
      if (ev.type === 'state.changed' || ev.type === 'effort.update' || ev.type === 'backend.update' || ev.type === 'strikes.update') void this.load();
    });
    return () => { this.disposed = true; off(); };
  }

  async load(): Promise<void> {
    if (this.busy) return;
    this.busy = true;
    try {
      const [rows, projects] = await Promise.all([this.deps.stats(this.project), this.deps.projects()]);
      if (this.disposed) return;
      Object.assign(this, { rows, projects, loaded: true });
      this.render();
    } catch {
      // The tab shows what it last had; the global health banner reports a lost service.
    } finally {
      this.busy = false;
    }
  }

  /** strikeCell shows a model's strikes out of three, a benched badge at three, and a Clear button. */
  private strikeCell(s: ModelStat): HTMLElement {
    if (!s.strikes) return h('td', { text: '0' });
    return h('td', {},
      s.benched ? badge('benched', 'warn') : h('span', { text: `${s.strikes}/3 ` }),
      button('Clear', () => { void this.deps.clear(s.backend, s.model, s.role).then(() => this.load()); }, { field: 'clear-strikes' }));
  }

  private table(): HTMLElement {
    const shown = this.rows.filter((r) => this.role === '' || r.role === this.role);
    if (shown.length === 0) return h('p', { class: 'field-hint', text: 'No finished stages yet.' });
    const head = ['Model', 'Backend', 'Tier', 'Family', 'Role', 'Stages', 'Passed', 'Failed', 'Infra', 'Pass rate', 'Avg time', 'Total time', 'Tokens', 'Worker attempts passed', 'Strikes'];
    return h('table', { class: 'stats-table', attrs: { 'data-role': 'pass-rates' } },
      h('thead', {}, h('tr', {}, ...head.map((t) => h('th', { text: t })))),
      h('tbody', {}, ...shown.map((s) => h('tr', {},
        h('td', { text: s.model }), h('td', { text: s.backend }),
        h('td', {}, s.tier ? badge(`tier ${s.tier}`, s.tier === 3 ? 'warn' : 'muted') : h('span', { class: 'field-hint', text: 'not rated' })),
        h('td', { text: s.family }), h('td', { text: s.role }),
        h('td', { text: String(s.stages) }), h('td', { text: String(s.passed) }), h('td', { text: String(s.failed) }),
        h('td', { text: String(s.infra) }), h('td', { text: s.passed + s.failed > 0 ? pct(s.pass_rate) : '-' }),
        h('td', { text: duration(s.avg_ms) }), h('td', { text: duration(s.total_ms) }), h('td', { text: tokensShort(s.tokens) }),
        h('td', { text: s.role === 'worker' ? `${s.attempts_passed}/${s.attempts_total}` : '-' }),
        this.strikeCell(s)))));
  }

  render(): void {
    if (!this.loaded) {
      replace(this.host, h('p', { class: 'field-hint', text: 'Loading stats…' }));
      return;
    }
    const role = selectInput('role', ROLES.map((r) => [r, r === '' ? 'All roles' : r] as [string, string]), this.role);
    role.addEventListener('change', () => { this.role = role.value; this.render(); });
    const project = selectInput('project', [['', 'All projects'], ...this.projects.map((p) => [p.slug, p.slug] as [string, string])], this.project);
    project.addEventListener('change', () => { this.project = project.value; void this.load(); });
    replace(this.host,
      h('div', { class: 'settings-host' },
        section('Model results',
          h('div', { class: 'inline-row' }, fieldRow('Role', role).el, fieldRow('Project', project).el),
          h('p', { class: 'field-hint', text: 'Infra failures (a crash, a timeout) say nothing about the model and are left out of the pass rate. Rate a model on the Backends tab once these numbers say how it does. A strike is output the engine could not read, a git policy break, or the same failure twice in a row on one task; three strikes bench a model in that role (local, free or paid alike) until you clear them here.' }),
          this.table())));
  }
}

/** mountStats is the Stats tab's mount. */
export function mountStats(host: HTMLElement): () => void {
  return new StatsView(host, defaultStatsDeps()).mount();
}
