// frd-editor.ts — the FRD sub-tab: the project's efforts (the FRD list), an
// editor for the effort's FRD with fields matching model.FRD, the revision
// viewer and the human approval (PJ1, DBS4, UI5). Saving always makes a new
// revision; the approved revision stays viewable after later edits. Every
// title, requirement and server message is placed with h(), so it is text.

import { alertDialog, showToast } from '@shared';
import { api, ApiError, type DocView, type EffortView, type FRD, type FRDAC, type FRDDecision, type FRDGroup, type FRDRequirement } from './api.js';
import { chooseEffort, effortBar, isFinished, rememberEffort } from './current.js';
import { effortSettings } from './effort-settings.js';
import { chip, h, replace } from './dom.js';
import { isTransient, startPoll, subscribe, type AgentflowEvent } from './events.js';
import { FormErrors, RowsEditor, badge, button, checkbox, fieldRow, section, selectInput, textArea, textInput } from './forms.js';

export type FrdDeps = Pick<typeof api, 'efforts' | 'createEffort' | 'patchEffort' | 'frd' | 'putFRD' | 'frdRevisions' | 'frdRevision' | 'approveFRD'> & {
  notify(message: string): void;
};

export const defaultFrdDeps = (): FrdDeps => ({
  efforts: api.efforts,
  createEffort: api.createEffort,
  patchEffort: api.patchEffort,
  frd: api.frd,
  putFRD: api.putFRD,
  frdRevisions: api.frdRevisions,
  frdRevision: api.frdRevision,
  approveFRD: api.approveFRD,
  notify: (m) => void showToast(m, 'success'),
});

export const PRIORITIES: Array<[string, string]> = [['', '(none)'], ['must', 'must'], ['should', 'should'], ['could', 'could'], ['wont', "won't"]];

/** lines splits a textarea into trimmed, non-empty lines. */
export const lines = (s: string): string[] => s.split('\n').map((l) => l.trim()).filter((l) => l !== '');

/** commaList splits "R1, R2" into ["R1", "R2"]. */
export const commaList = (s: string): string[] => s.split(',').map((l) => l.trim()).filter((l) => l !== '');

const when = (ms: number): string => (ms ? new Date(ms).toLocaleString() : '');

/** blankFRD is the starting point for an effort that has no FRD yet. */
export function blankFRD(slug: string, effort: EffortView): FRD {
  return {
    id: `FRD-${slug}-${effort.id}`.toUpperCase(),
    title: effort.title,
    objective: '',
    requirements: [{ id: 'R1', priority: 'must', description: '' }],
    acceptance_criteria: [],
  };
}

/** FrdForm is the editable form over one FRD; read() assembles the document, carrying fields it does not edit. */
export class FrdForm {
  readonly el: HTMLElement;
  readonly errors = new FormErrors();
  private f: Record<'id' | 'title' | 'status' | 'objective' | 'context' | 'constraints' | 'out_of_scope', HTMLInputElement | HTMLTextAreaElement>;
  private groups: RowsEditor<FRDGroup>;
  private reqs: RowsEditor<FRDRequirement>;
  private acs: RowsEditor<FRDAC>;
  private decisions: RowsEditor<FRDDecision>;

  constructor(private base: FRD, onChange: () => void) {
    this.f = {
      id: textInput('id', base.id ?? ''),
      title: textInput('title', base.title ?? ''),
      status: textInput('status', base.status ?? ''),
      objective: textArea('objective', base.objective ?? '', 3),
      context: textArea('context', base.context ?? '', 3),
      constraints: textArea('constraints', (base.constraints ?? []).join('\n'), 3),
      out_of_scope: textArea('out_of_scope', (base.out_of_scope ?? []).join('\n'), 3),
    };
    for (const c of Object.values(this.f)) c.addEventListener('input', onChange);
    this.groups = new RowsEditor<FRDGroup>((g) => {
      const id = textInput('group_id', g.id, { placeholder: 'id' });
      const title = textInput('group_title', g.title, { placeholder: 'title' });
      const summary = textInput('group_summary', g.summary ?? '', { placeholder: 'summary' });
      return { el: h('div', { class: 'row-actions' }, id, title, summary), read: () => ({ id: id.value.trim(), title: title.value.trim(), summary: summary.value.trim() || undefined }) };
    }, base.groups ?? [], () => ({ id: '', title: '' }), 'Add group', onChange);
    this.reqs = new RowsEditor<FRDRequirement>((r) => {
      const id = textInput('req_id', r.id, { placeholder: 'id' });
      const group = textInput('req_group', r.group ?? '', { placeholder: 'group' });
      const prio = selectInput('req_priority', PRIORITIES, r.priority ?? '');
      const desc = textArea('req_description', r.description, 2);
      return {
        el: h('div', { class: 'wizard-field' }, h('div', { class: 'row-actions' }, id, group, prio), desc),
        read: () => ({ ...r, id: id.value.trim(), group: group.value.trim() || undefined, priority: prio.value || undefined, description: desc.value.trim() }),
      };
    }, base.requirements ?? [], () => ({ id: '', description: '' }), 'Add requirement', onChange);
    this.acs = new RowsEditor<FRDAC>((a) => {
      const id = textInput('ac_id', a.id, { placeholder: 'id' });
      const reqs = textInput('ac_requirements', (a.requirements ?? []).join(', '), { placeholder: 'R1, R2' });
      const crit = textArea('ac_criterion', a.criterion, 2);
      return {
        el: h('div', { class: 'wizard-field' }, h('div', { class: 'row-actions' }, id, reqs), crit),
        read: () => ({ id: id.value.trim(), requirements: commaList(reqs.value), criterion: crit.value.trim() }),
      };
    }, base.acceptance_criteria ?? [], () => ({ id: '', criterion: '' }), 'Add acceptance criterion', onChange);
    this.decisions = new RowsEditor<FRDDecision>((d) => {
      const id = textInput('decision_id', d.id, { placeholder: 'id' });
      const dec = textInput('decision_text', d.decision, { placeholder: 'decision' });
      const why = textInput('decision_rationale', d.rationale ?? '', { placeholder: 'rationale' });
      return { el: h('div', { class: 'row-actions' }, id, dec, why), read: () => ({ id: id.value.trim(), decision: dec.value.trim(), rationale: why.value.trim() || undefined }) };
    }, base.decisions ?? [], () => ({ id: '', decision: '' }), 'Add decision', onChange);

    const e = this.errors;
    const verbatim = ['state_model', 'findings', 'planning_spikes'].filter((k) => (base as unknown as Record<string, unknown>)[k] !== undefined);
    this.el = h('div', { class: 'detail-body', attrs: { 'data-role': 'frd-form' } },
      h('div', { class: 'profile-fields' },
        e.add('id', fieldRow('ID', this.f.id)).el,
        e.add('title', fieldRow('Title', this.f.title)).el,
        e.add('status', fieldRow('Status', this.f.status)).el),
      e.add('objective', fieldRow('Objective', this.f.objective)).el,
      e.add('context', fieldRow('Context', this.f.context)).el,
      e.add('constraints', fieldRow('Constraints', this.f.constraints, 'One per line')).el,
      e.add('groups', fieldRow('Groups', this.groups.el)).el,
      e.add('requirements', fieldRow('Requirements', this.reqs.el)).el,
      e.add('acceptance_criteria', fieldRow('Acceptance criteria', this.acs.el, 'Requirements are a comma-separated list of requirement ids')).el,
      e.add('decisions', fieldRow('Decisions', this.decisions.el)).el,
      e.add('out_of_scope', fieldRow('Out of scope', this.f.out_of_scope, 'One per line')).el,
      verbatim.length ? h('p', { class: 'field-hint', text: `Kept as they are: ${verbatim.join(', ')}` }) : null,
      e.general);
  }

  /** read assembles the FRD: the loaded document with the edited fields replaced. */
  read(): FRD {
    const v = (k: keyof FrdForm['f']): string => this.f[k].value.trim();
    const some = <T>(a: T[]): T[] | undefined => (a.length ? a : undefined);
    return {
      ...this.base,
      id: v('id'),
      title: v('title'),
      status: v('status') || undefined,
      objective: v('objective'),
      context: v('context') || undefined,
      constraints: some(lines(this.f.constraints.value)),
      groups: some(this.groups.read().filter((g) => g.id || g.title)),
      requirements: this.reqs.read().filter((r) => r.id || r.description),
      acceptance_criteria: this.acs.read().filter((a) => a.id || a.criterion),
      decisions: some(this.decisions.read().filter((d) => d.id || d.decision)),
      out_of_scope: some(lines(this.f.out_of_scope.value)),
    };
  }
}

export class FrdEditor {
  efforts: EffortView[] = [];
  current: EffortView | undefined;
  private revisions: DocView[] = [];
  private latest: DocView<FRD> | null = null;
  private viewing: DocView<FRD> | null = null;
  private form: FrdForm | null = null;
  private dirty = false;
  private loaded = false;
  private off: (() => void) | null = null;
  private createErrors = new FormErrors();

  private stopPoll: (() => void) | null = null;

  constructor(private host: HTMLElement, private slug: string, private deps: FrdDeps) {}

  mount(): () => void {
    this.off = subscribe((ev: AgentflowEvent) => {
      const mine = 'project' in ev && ev.project === this.slug;
      if (!this.dirty && (ev.type === 'state.changed' || mine && (ev.type === 'effort.update' || ev.type === 'project.update'))) void this.load();
    });
    this.stopPoll = startPoll(() => !this.dirty && isTransient(this.current?.status), () => this.load());
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
    this.current = chooseEffort(this.slug, this.efforts);
    await this.loadDoc();
  }

  private async loadDoc(): Promise<void> {
    this.latest = null;
    this.viewing = null;
    this.revisions = [];
    this.dirty = false;
    const ef = this.current;
    if (ef) {
      try {
        this.latest = await this.deps.frd(this.slug, ef.id, { quiet: true });
        this.revisions = await this.deps.frdRevisions(this.slug, ef.id);
      } catch (err) {
        if (!(err instanceof ApiError && err.status === 404)) void alertDialog(err instanceof Error ? err.message : String(err));
      }
      this.form = new FrdForm(this.latest?.json ?? blankFRD(this.slug, ef), () => { this.dirty = true; });
    } else {
      this.form = null;
    }
    this.render();
  }

  private async create(title: string, deliberate: boolean, tokens: string, minutes: string): Promise<void> {
    this.createErrors.clear();
    try {
      const ef = await this.deps.createEffort(this.slug, title.trim(), deliberate, { quiet: true }, {
        budget_tokens: Math.max(0, Math.round(Number(tokens) || 0)),
        budget_seconds: Math.max(0, Math.round((Number(minutes) || 0) * 60)),
      });
      rememberEffort(this.slug, ef.id);
    } catch (err) {
      this.createErrors.show(err);
      return;
    }
    this.deps.notify('Effort created');
    await this.load();
  }

  async save(): Promise<void> {
    const ef = this.current;
    if (!ef || !this.form) return;
    this.form.errors.clear();
    try {
      await this.deps.putFRD(this.slug, ef.id, this.form.read(), { quiet: true });
    } catch (err) {
      this.form.errors.show(err);
      return;
    }
    this.deps.notify('Saved a new FRD revision');
    await this.load();
  }

  async approve(): Promise<void> {
    const ef = this.current;
    if (!ef || !this.latest) return;
    try {
      await this.deps.approveFRD(this.slug, ef.id, this.latest.revision);
    } catch {
      return;
    }
    this.deps.notify(`Approved revision ${this.latest.revision}`);
    await this.load();
  }

  async view(n: number): Promise<void> {
    const ef = this.current;
    if (!ef) return;
    try {
      this.viewing = await this.deps.frdRevision(this.slug, ef.id, n);
    } catch {
      return;
    }
    this.render();
  }

  private loadIntoEditor(doc: FRD): void {
    this.form = new FrdForm(doc, () => { this.dirty = true; });
    this.dirty = true;
    this.viewing = null;
    this.render();
  }

  private newEffortForm(): HTMLElement {
    const title = textInput('effort_title', '', { placeholder: 'What this effort delivers' });
    const deliberate = checkbox('deliberate', false);
    const tokens = textInput('budget_tokens', '', { type: 'number', placeholder: 'no limit' });
    const minutes = textInput('budget_minutes', '', { type: 'number', placeholder: 'no limit' });
    this.createErrors = new FormErrors();
    return section('New effort',
      this.createErrors.add('title', fieldRow('Title', title)).el,
      fieldRow('Deliberate planning', deliberate, 'Adds a pre-mortem and test plan to the planner prompt; after the FRD is written you are told when it mentions auth, migration or personal data').el,
      this.createErrors.add('budget_tokens', fieldRow('Token budget', tokens, 'Optional; blank is no limit')).el,
      this.createErrors.add('budget_seconds', fieldRow('Time budget (minutes)', minutes, 'Optional; blank is no limit')).el,
      this.createErrors.general,
      h('div', { class: 'settings-actions' }, button('Create effort', () => void this.create(title.value, deliberate.checked, tokens.value, minutes.value), { primary: true, field: 'create-effort', icon: 'add', word: true })));
  }

  private list(): HTMLElement {
    return h('ul', { class: 'task-list', attrs: { 'data-role': 'frd-list' } }, ...this.efforts.map((e) =>
      h('li', { class: 'task-item' + (e.id === this.current?.id ? ' selected' : ''), attrs: { 'data-effort': e.id }, on: {
        click: () => { rememberEffort(this.slug, e.id); this.current = e; void this.loadDoc(); },
      } },
        h('div', { class: 'task-item-title', text: e.title }),
        h('div', { class: 'task-item-meta' }, chip(e.status),
          h('span', { text: e.frd.latest ? `FRD revision ${e.frd.latest}${e.frd.approved ? `, approved ${e.frd.approved}` : ', not approved'}` : 'no FRD yet' })))));
  }

  private revisionList(): HTMLElement {
    const rows = this.revisions.map((r) =>
      h('div', { class: 'inline-row', attrs: { 'data-revision': r.revision } },
        h('span', { text: `Revision ${r.revision} · ${r.created_by} · ${when(r.created)}` }),
        r.approved ? badge('approved', 'ok') : null,
        button('View', () => void this.view(r.revision), { field: 'view-revision', icon: 'view' })));
    const viewing = this.viewing;
    return section('Revisions',
      rows.length ? h('div', { class: 'wizard-list' }, ...rows) : h('p', { class: 'field-hint', text: 'No revisions yet.' }),
      viewing ? h('div', { class: 'detail-section', attrs: { 'data-role': 'revision-view' } },
        h('h4', { text: `Revision ${viewing.revision}${viewing.approved ? ' (approved)' : ''}` }),
        h('pre', { class: 'detail-mono', text: JSON.stringify(viewing.json, null, 2) }),
        h('div', { class: 'row-actions' },
          button('Load into editor', () => viewing.json && this.loadIntoEditor(viewing.json), { field: 'load-revision', icon: 'import', word: true }),
          button('Close', () => { this.viewing = null; this.render(); }, { field: 'close-revision', icon: 'cancel' }))) : null);
  }

  render(): void {
    if (!this.loaded) {
      replace(this.host, h('p', { class: 'field-hint', text: 'Loading efforts…' }));
      return;
    }
    const ef = this.current;
    const approvedRev = this.revisions.find((r) => r.approved)?.revision;
    // The one obvious next step: approving a saved, unedited revision; otherwise saving.
    const canApprove = !!ef && !!this.latest && this.latest.revision !== approvedRev && !this.dirty && !isFinished(ef);
    const editor = ef && this.form ? section(`FRD for #${ef.id}`,
      h('div', { class: 'row-actions' },
        h('span', { class: 'field-hint', text: this.latest ? `Latest revision ${this.latest.revision}${approvedRev ? `; revision ${approvedRev} approved` : '; not approved'}` : 'Not saved yet' }),
        this.dirty ? badge('unsaved', 'warn') : null),
      this.form.el,
      h('div', { class: 'settings-actions' },
        button('Save as new revision', () => void this.save(), { primary: !canApprove, field: 'save-frd', icon: 'save' }),
        canApprove && this.latest ? button(`Approve revision ${this.latest.revision}`, () => void this.approve(), { primary: true, field: 'approve-frd', icon: 'approve' }) : null)) : null;
    // A project has one active effort at a time: show the form only when none is active, else a disabled "+" says why.
    const active = this.efforts.find((e) => !isFinished(e));
    const bar = this.efforts.length ? effortBar(this.slug, this.efforts, ef, (id) => { this.current = this.efforts.find((e) => e.id === id); void this.loadDoc(); }) : null;
    if (bar && active) {
      bar.appendChild(h('button', {
        class: 'ui-modal-btn is-disabled',
        text: '+',
        attrs: {
          type: 'button', 'data-action': 'new-effort', 'aria-disabled': 'true', 'aria-label': 'New effort',
          title: `New effort is unavailable: a project has one active effort at a time. Finish or abandon #${active.id} (${active.status.replace(/_/g, ' ')}) first.`,
        },
      }));
    }
    replace(this.host,
      h('div', { class: 'settings-host' },
        bar,
        active ? null : this.efforts.length ? null : h('div', { class: 'plan-empty' }, h('h3', { text: 'No efforts yet' }), h('p', { text: 'An effort is one FRD, plan and queue. Create the first one below.' })),
        active ? null : this.newEffortForm(),
        editor,
        ef && !isFinished(ef) ? effortSettings(ef, this.form ? JSON.stringify(this.form.read()) : '', this.deps, () => void this.load()) : null,
        ef ? this.revisionList() : null,
        this.efforts.length ? section('Efforts', this.list()) : null));
  }
}

/** mountFrd is the FRD sub-tab's mount. */
export function mountFrd(host: HTMLElement, slug: string): () => void {
  return new FrdEditor(host, slug, defaultFrdDeps()).mount();
}
