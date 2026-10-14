// plan-view.ts — the Plan sub-tab: the ralplan loop's rounds with planner,
// architect and critic tabs and verdict chips, the "not critic-approved"
// badge, the distinct-model warning (MB6) and the human approval (RP2, RP3).
// Approved revisions stay viewable (DBS4). All plan and review text is placed
// with h(), so it is text.

import { confirmDialog, showToast } from '@shared';
import { api, type DocView, type EffortView, type PlanDoc, type PlanReview, type PlanRound, type PlanView as PlanState } from './api.js';
import { chooseEffort, effortBar } from './current.js';
import { chip, h, replace, type Child } from './dom.js';
import { isTransient, startPoll, subscribe, type AgentflowEvent } from './events.js';
import { badge, button, section } from './forms.js';

export type PlanDeps = Pick<typeof api, 'efforts' | 'plan' | 'planRounds' | 'planRevisions' | 'planRevision' | 'generatePlan' | 'approvePlan'> & {
  confirm(message: string): Promise<boolean>;
  notify(message: string): void;
};

export const defaultPlanDeps = (): PlanDeps => ({
  efforts: api.efforts,
  plan: api.plan,
  planRounds: api.planRounds,
  planRevisions: api.planRevisions,
  planRevision: api.planRevision,
  generatePlan: api.generatePlan,
  approvePlan: api.approvePlan,
  confirm: (m) => confirmDialog(m, { confirmLabel: 'Approve' }),
  notify: (m) => void showToast(m, 'success'),
});

export const PLAN_TABS = ['planner', 'architect', 'critic'] as const;
export type PlanTab = (typeof PLAN_TABS)[number];

const list = (title: string, items: string[] | undefined): Child =>
  items && items.length > 0 ? h('div', { class: 'plan-section' }, h('h4', { text: title }), h('ul', {}, ...items.map((t) => h('li', { text: t })))) : null;

/** renderPlan shows a plan document read-only. */
export function renderPlan(p: PlanDoc): HTMLElement {
  return h('div', { class: 'plan-content', attrs: { 'data-role': 'plan-doc' } },
    list('Principles', p.principles),
    list('Decision drivers', p.drivers),
    p.options?.length ? h('div', { class: 'plan-section' }, h('h4', { text: 'Options' }),
      ...p.options.map((o) => h('div', {}, h('strong', { text: o.name }), h('p', { text: o.description }),
        list('Pros', o.pros), list('Cons', o.cons)))) : null,
    p.phases?.length ? h('div', { class: 'plan-section' }, h('h4', { text: 'Phases' }),
      ...p.phases.map((ph) => h('div', { class: 'phase-card' },
        h('div', { class: 'phase-header' }, h('strong', { text: `${ph.id} ${ph.name}` })),
        h('div', { class: 'phase-desc', text: ph.tasks_description }),
        ph.depends_on?.length ? h('div', { class: 'phase-deps', text: `depends on ${ph.depends_on.join(', ')}` }) : null))) : null,
    p.adr ? h('div', { class: 'plan-section' }, h('h4', { text: 'Decision record' }),
      h('p', { text: p.adr.decision }), h('p', { text: p.adr.why }),
      list('Alternatives', p.adr.alternatives), list('Consequences', p.adr.consequences), list('Follow-ups', p.adr.followups)) : null,
    list('Pre-mortem', p.premortem),
    list('Test plan', p.test_plan));
}

/** renderReview shows an architect or critic review: verdict chip, summary and issues. */
export function renderReview(r: PlanReview | undefined, role: string): HTMLElement {
  if (!r) return h('p', { class: 'field-hint', text: `No ${role} review in this round yet.` });
  return h('div', { class: 'review-section', attrs: { 'data-role': `review-${role}` } },
    h('div', { class: 'row-actions' }, chip(r.verdict), h('span', { class: 'field-hint', text: `on plan revision ${r.plan_revision}` })),
    r.summary ? h('p', { text: r.summary }) : null,
    r.issues?.length ? h('ul', { class: 'plan-review-notes' }, ...r.issues.map((i) =>
      h('li', {}, h('strong', { text: i.severity }), ` ${i.description}`, i.file ? ` (${i.file}${i.line ? ':' + i.line : ''})` : null))) : null);
}

/** elapsedText formats a duration as "42s" or "3m 05s". */
export function elapsedText(ms: number): string {
  const s = Math.max(0, Math.floor(ms / 1000));
  return s < 60 ? `${s}s` : `${Math.floor(s / 60)}m ${String(s % 60).padStart(2, '0')}s`;
}

/** stageName says what the engine is doing now, from the effort status and the plan rounds so far. */
export function stageName(status: string, rounds: PlanRound[], latestRevision: number): string {
  if (status === 'compiling') return 'Compiler: turning the plan into tasks';
  if (status !== 'planning') return '';
  const reviewed = Math.max(0, ...rounds.flatMap((r) => r.reviews.map((x) => x.plan_revision)));
  if (latestRevision === 0) return 'Planner: drafting the first plan';
  if (latestRevision > reviewed) return `Architect: reviewing plan revision ${latestRevision}`;
  const last = rounds[rounds.length - 1];
  if (last && !last.reviews.some((x) => x.role === 'critic')) return `Critic: reviewing round ${last.round}`;
  return `Planner: revising the plan after round ${last?.round ?? 1}`;
}

export class PlanViewer {
  efforts: EffortView[] = [];
  current: EffortView | undefined;
  state: PlanState | null = null;
  rounds: PlanRound[] = [];
  revisions: DocView[] = [];
  round = 0;
  tab: PlanTab = 'planner';
  private docs = new Map<number, PlanDoc>();
  private viewing: DocView<PlanDoc> | null = null;
  /** The newest plan document the snapshot knows: the presented plan, else the latest revision. */
  private latestDoc: PlanDoc | undefined;
  private loaded = false;
  private off: (() => void) | null = null;
  private stopPoll: (() => void) | null = null;
  private ticker: ReturnType<typeof setInterval> | null = null;
  private elapsedEl: HTMLElement | null = null;
  private since = 0;

  constructor(private host: HTMLElement, private slug: string, private deps: PlanDeps) {}

  mount(): () => void {
    this.off = subscribe((ev: AgentflowEvent) => {
      const mine = 'project' in ev && ev.project === this.slug;
      if (ev.type === 'state.changed' || (mine && (ev.type === 'effort.update' || ev.type === 'project.update'))) void this.load();
    });
    this.stopPoll = startPoll(() => isTransient(this.current?.status), () => this.load());
    void this.load();
    return () => {
      this.off?.();
      this.off = null;
      this.stopPoll?.();
      this.stopPoll = null;
      this.stopTicker();
    };
  }

  private stopTicker(): void {
    if (this.ticker) clearInterval(this.ticker);
    this.ticker = null;
    this.elapsedEl = null;
  }

  async load(): Promise<void> {
    try {
      this.efforts = await this.deps.efforts(this.slug);
    } catch {
      if (!this.loaded) return;
    }
    this.loaded = true;
    this.current = chooseEffort(this.slug, this.efforts);
    await this.loadPlan();
  }

  /**
   * loadPlan fetches the plan, rounds and revisions as one snapshot and swaps
   * it in whole, so the view never mixes a new status with old rounds. A failed
   * fetch keeps what is shown.
   */
  private async loadPlan(): Promise<void> {
    const ef = this.current;
    if (!ef) {
      this.state = null;
      this.rounds = [];
      this.revisions = [];
      this.viewing = null;
      this.latestDoc = undefined;
      this.render();
      return;
    }
    try {
      const [state, rounds, revisions] = await Promise.all([
        this.deps.plan(this.slug, ef.id), this.deps.planRounds(this.slug, ef.id), this.deps.planRevisions(this.slug, ef.id),
      ]);
      const revs = revisions ?? [];
      let latestDoc = state.plan;
      if (!latestDoc && revs.length > 0) {
        try {
          latestDoc = (await this.deps.planRevision(this.slug, ef.id, Math.max(...revs.map((r) => r.revision)))).json;
        } catch {
          // apiFetch already told the user.
        }
      }
      if (this.current?.id !== ef.id) return; // the reader picked another effort meanwhile
      this.state = { ...state, warnings: rounds.warnings ?? state.warnings, waiting: rounds.waiting ?? state.waiting };
      this.current = { ...ef, status: state.status };
      this.rounds = (rounds.rounds ?? []).map((r) => ({ ...r, reviews: r.reviews ?? [] }));
      this.revisions = revs;
      this.latestDoc = latestDoc;
      if (this.viewing && !revs.some((r) => r.revision === this.viewing?.revision)) this.viewing = null;
    } catch {
      // apiFetch already told the user.
    }
    const last = this.rounds[this.rounds.length - 1]?.round ?? 0;
    if (this.round === 0 || !this.rounds.some((r) => r.round === this.round)) this.round = last;
    await this.fetchRoundPlan();
    this.render();
  }

  /** The plan revision the planner produced for the selected round, from its reviews. */
  private roundPlanRevision(): number {
    const r = this.rounds.find((x) => x.round === this.round);
    return r?.reviews[0]?.plan_revision ?? 0;
  }

  private async fetchRoundPlan(): Promise<void> {
    const ef = this.current;
    const rev = this.roundPlanRevision();
    if (!ef || rev === 0 || this.docs.has(rev * 1000 + ef.id)) return;
    try {
      const d = await this.deps.planRevision(this.slug, ef.id, rev);
      if (d.json) this.docs.set(rev * 1000 + ef.id, d.json);
    } catch {
      // apiFetch already told the user.
    }
  }

  private plannerDoc(): PlanDoc | undefined {
    const ef = this.current;
    const rev = this.roundPlanRevision();
    if (ef && rev > 0) return this.docs.get(rev * 1000 + ef.id);
    return this.state?.plan ?? this.latestDoc;
  }

  async generate(): Promise<void> {
    const ef = this.current;
    if (!ef) return;
    try {
      await this.deps.generatePlan(this.slug, ef.id);
    } catch {
      return;
    }
    this.deps.notify('Planning started');
    await this.load();
  }

  async approve(): Promise<void> {
    const ef = this.current;
    const st = this.state;
    if (!ef || !st) return;
    if (st.not_critic_approved && !(await this.deps.confirm(
      'The critic did not approve this plan (the revision rounds ran out). Approve revision ' + st.revision + ' anyway?'))) return;
    try {
      await this.deps.approvePlan(this.slug, ef.id);
    } catch {
      return;
    }
    this.deps.notify(`Approved plan revision ${st.revision}`);
    await this.load();
  }

  private async viewRevision(n: number): Promise<void> {
    const ef = this.current;
    if (!ef) return;
    try {
      this.viewing = await this.deps.planRevision(this.slug, ef.id, n);
    } catch {
      return;
    }
    this.render();
  }

  private header(ef: EffortView, st: PlanState): HTMLElement {
    const approved = this.revisions.find((r) => r.approved)?.revision;
    const canGenerate = ef.frd.latest > 0 && (ef.status === 'draft' || ef.status === 'plan_ready');
    const canApprove = ef.status === 'plan_ready' && st.revision > 0 && approved !== st.revision;
    return h('div', { class: 'plan-header', attrs: { 'data-role': 'plan-header' } },
      h('div', { class: 'plan-meta' },
        chip(ef.status),
        st.revision > 0 ? h('span', { class: 'plan-frd-id', text: `revision ${st.revision} · ${st.rounds} round${st.rounds === 1 ? '' : 's'}` })
          : h('span', { class: 'plan-frd-id', text: this.latestDoc ? 'first draft, not reviewed yet' : 'no plan yet' }),
        st.not_critic_approved ? badge('not critic-approved', 'warn') : null,
        approved ? badge(`revision ${approved} approved`, 'ok') : null),
      h('div', { class: 'plan-actions' },
        canGenerate ? button(st.revision > 0 ? 'Regenerate plan' : 'Generate plan', () => void this.generate(), { field: 'generate-plan' }) : null,
        canApprove ? button('Approve plan', () => void this.approve(), { primary: true, field: 'approve-plan' }) : null));
  }

  /** strip is the live indicator shown while the engine plans or compiles: stage, and time since the effort last changed. */
  private strip(ef: EffortView): HTMLElement | null {
    if (ef.status !== 'planning' && ef.status !== 'compiling') {
      this.stopTicker();
      return null;
    }
    const stage = stageName(ef.status, this.rounds, Math.max(0, ...this.revisions.map((r) => r.revision)));
    this.since = ef.updated;
    const elapsed = h('span', { class: 'field-hint', text: this.since > 0 ? elapsedText(Date.now() - this.since) : '', attrs: { 'data-role': 'elapsed' } });
    this.elapsedEl = elapsed;
    if (!this.ticker) {
      this.ticker = setInterval(() => {
        if (this.elapsedEl && this.since > 0) this.elapsedEl.textContent = elapsedText(Date.now() - this.since);
      }, 1000);
    }
    return h('div', { class: 'progress-strip', attrs: { 'data-role': 'progress-strip', role: 'status' } },
      h('div', { class: 'row-actions' }, h('strong', { text: stage }), elapsed),
      h('div', { class: 'progress-bar-wrap' }, h('div', { class: 'progress-bar progress-indeterminate' })));
  }

  private roundTabs(): HTMLElement {
    const review = (role: string): PlanReview | undefined =>
      this.rounds.find((r) => r.round === this.round)?.reviews.filter((x) => x.role === role).pop();
    return h('div', {},
      h('div', { class: 'tab-bar', attrs: { role: 'tablist' } }, ...this.rounds.map((r) =>
        h('button', { class: 'tab-btn' + (r.round === this.round ? ' active' : ''), text: `Round ${r.round}`,
          attrs: { type: 'button', 'data-round': r.round },
          on: { click: () => { this.round = r.round; void this.fetchRoundPlan().then(() => this.render()); } } }))),
      h('div', { class: 'tab-bar', attrs: { role: 'tablist' } }, ...PLAN_TABS.map((t) => {
        const v = t === 'planner' ? undefined : review(t);
        return h('button', { class: 'tab-btn' + (t === this.tab ? ' active' : ''), attrs: { type: 'button', 'data-plan-tab': t },
          on: { click: () => { this.tab = t; this.render(); } } }, t, v ? ' ' : null, v ? chip(v.verdict) : null);
      })),
      this.tab === 'planner'
        ? (this.plannerDoc() ? renderPlan(this.plannerDoc() as PlanDoc) : h('p', { class: 'field-hint', text: 'No plan for this round yet.' }))
        : renderReview(review(this.tab), this.tab));
  }

  private revisionList(): HTMLElement {
    const v = this.viewing;
    return section('Plan revisions',
      this.revisions.length
        ? h('div', { class: 'wizard-list' }, ...this.revisions.map((r) =>
          h('div', { class: 'inline-row', attrs: { 'data-revision': r.revision } },
            h('span', { text: `Revision ${r.revision} · ${r.created_by}` }),
            r.approved ? badge('approved', 'ok') : null,
            button('View', () => void this.viewRevision(r.revision), { field: 'view-revision' }))))
        : h('p', { class: 'field-hint', text: 'No revisions yet.' }),
      v ? h('div', { attrs: { 'data-role': 'revision-view' } },
        h('h4', { text: `Revision ${v.revision}${v.approved ? ' (approved)' : ''}` }),
        v.json ? renderPlan(v.json) : null,
        button('Close', () => { this.viewing = null; this.render(); }, { field: 'close-revision' })) : null);
  }

  render(): void {
    if (!this.loaded) {
      replace(this.host, h('p', { class: 'field-hint', text: 'Loading efforts…' }));
      return;
    }
    const ef = this.current;
    const st = this.state;
    if (!ef || !st) {
      this.stopTicker();
      replace(this.host, h('div', { class: 'plan-empty' }, h('h3', { text: 'No effort to plan' }), h('p', { text: 'Create an effort and save its FRD on the FRD tab first.' })));
      return;
    }
    const warnings = st.warnings ?? [];
    replace(this.host,
      h('div', { class: 'settings-host' },
        effortBar(this.slug, this.efforts, ef, (id) => { this.current = this.efforts.find((e) => e.id === id); this.round = 0; void this.loadPlan(); }),
        this.header(ef, st),
        warnings.length ? h('div', { class: 'warn-box', attrs: { 'data-role': 'plan-warnings' } }, ...warnings.map((w) => h('p', { text: w }))) : null,
        st.waiting ? h('div', { class: 'warn-box', attrs: { 'data-role': 'plan-waiting' } }, h('p', { text: `Waiting: ${st.waiting}` })) : null,
        this.strip(ef),
        this.rounds.length > 0 || this.plannerDoc() ? this.roundTabs() : h('div', { class: 'plan-empty' }, h('h3', { text: 'No plan yet' }), h('p', { text: 'Generate a plan once the FRD is saved.' })),
        this.revisionList()));
  }
}

/** mountPlan is the Plan sub-tab's mount. */
export function mountPlan(host: HTMLElement, slug: string): () => void {
  return new PlanViewer(host, slug, defaultPlanDeps()).mount();
}
