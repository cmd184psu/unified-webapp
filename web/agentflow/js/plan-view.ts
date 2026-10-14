// plan-view.ts — the Plan sub-tab: the ralplan loop's rounds with planner,
// architect and critic tabs and verdict chips, the "not critic-approved"
// badge, the distinct-model warning (MB6) and the human approval (RP2, RP3).
// Approved revisions stay viewable (DBS4). All plan and review text is placed
// with h(), so it is text.

import { confirmDialog, showToast } from '@shared';
import { api, type DocView, type EffortView, type PlanDoc, type PlanReview, type PlanRound, type PlanView as PlanState } from './api.js';
import { chooseEffort, effortBar } from './current.js';
import { chip, h, replace, type Child } from './dom.js';
import { subscribe, type AgentflowEvent } from './events.js';
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
  private loaded = false;
  private off: (() => void) | null = null;

  constructor(private host: HTMLElement, private slug: string, private deps: PlanDeps) {}

  mount(): () => void {
    this.off = subscribe((ev: AgentflowEvent) => {
      const mine = 'project' in ev && ev.project === this.slug;
      if (ev.type === 'state.changed' || (mine && (ev.type === 'effort.update' || ev.type === 'project.update'))) void this.load();
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
    await this.loadPlan();
  }

  private async loadPlan(): Promise<void> {
    const ef = this.current;
    this.state = null;
    this.rounds = [];
    this.revisions = [];
    this.viewing = null;
    if (ef) {
      try {
        const [state, rounds, revisions] = await Promise.all([
          this.deps.plan(this.slug, ef.id), this.deps.planRounds(this.slug, ef.id), this.deps.planRevisions(this.slug, ef.id),
        ]);
        this.state = state;
        this.rounds = (rounds.rounds ?? []).map((r) => ({ ...r, reviews: r.reviews ?? [] }));
        this.revisions = revisions ?? [];
        this.state = { ...state, warnings: rounds.warnings ?? state.warnings, waiting: rounds.waiting ?? state.waiting };
      } catch {
        // apiFetch already told the user.
      }
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
    return this.state?.plan;
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
        st.revision > 0 ? h('span', { class: 'plan-frd-id', text: `revision ${st.revision} · ${st.rounds} round${st.rounds === 1 ? '' : 's'}` }) : h('span', { class: 'plan-frd-id', text: 'no plan yet' }),
        st.not_critic_approved ? badge('not critic-approved', 'warn') : null,
        approved ? badge(`revision ${approved} approved`, 'ok') : null),
      h('div', { class: 'plan-actions' },
        canGenerate ? button(st.revision > 0 ? 'Regenerate plan' : 'Generate plan', () => void this.generate(), { field: 'generate-plan' }) : null,
        canApprove ? button('Approve plan', () => void this.approve(), { primary: true, field: 'approve-plan' }) : null));
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
        ef.status === 'planning' && !st.waiting ? h('p', { class: 'field-hint', text: 'Planning is running; this page updates as rounds finish.' }) : null,
        this.rounds.length > 0 || st.plan ? this.roundTabs() : h('div', { class: 'plan-empty' }, h('h3', { text: 'No plan yet' }), h('p', { text: 'Generate a plan once the FRD is saved.' })),
        this.revisionList()));
  }
}

/** mountPlan is the Plan sub-tab's mount. */
export function mountPlan(host: HTMLElement, slug: string): () => void {
  return new PlanViewer(host, slug, defaultPlanDeps()).mount();
}
