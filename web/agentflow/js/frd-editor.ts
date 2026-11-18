// frd-editor.ts — the FRD sub-tab: the project's efforts (the FRD list), the
// describe-it box that drafts an FRD, a read-only review card with the human
// approval, the definition of done, the size meter with split and its parts,
// and the structured editor behind "edit" with fields matching model.FRD, plus
// the revision viewer (PJ1, DBS4, UI5). Saving always makes a new revision; the
// approved revision stays viewable after later edits. Ids are filled by the
// server and never typed. Every title, requirement and server message is
// placed with h(), so it is text.

import { autosave, type Autosave } from './autosave.js';
import { alertDialog, showToast } from '@shared';
import { api, ApiError, type DefinitionOfDone, type DocView, type DraftState, type EffortView, type FRD, type FRDAC, type FRDDecision, type FRDGroup, type FrdPart, type FRDRequirement, type SizeView } from './api.js';
import { chooseEffort, effortBar, isFinished, rememberEffort } from './current.js';
import { effortSettings, formatTokens } from './effort-settings.js';
import { chip, h, replace, type Child } from './dom.js';
import { icon } from './icons.js';
import { isTransient, startPoll, subscribe, type AgentflowEvent } from './events.js';
import { elapsedText } from './plan-view.js';
import { FormErrors, RowsEditor, badge, button, checkbox, fieldRow, section, selectInput, textArea, textInput } from './forms.js';

export type FrdDeps = Pick<typeof api, 'efforts' | 'createEffort' | 'patchEffort' | 'frd' | 'putFRD' | 'frdRevisions' | 'frdRevision' | 'approveFRD' |
  'draftFRD' | 'draftState' | 'definitionOfDone' | 'frdSize' | 'splitFRD' | 'frdParts' | 'startPart'> & {
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
  draftFRD: api.draftFRD,
  draftState: api.draftState,
  definitionOfDone: api.definitionOfDone,
  frdSize: api.frdSize,
  splitFRD: api.splitFRD,
  frdParts: api.frdParts,
  startPart: api.startPart,
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
    requirements: [{ id: '', priority: 'must', description: '' }],
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
      id: textInput('id', base.id ?? '', { readOnly: true }),
      title: textInput('title', base.title ?? ''),
      status: textInput('status', base.status ?? '', { readOnly: true }),
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
      const group = textInput('req_group', r.group ?? '', { placeholder: 'group' });
      const prio = selectInput('req_priority', PRIORITIES, r.priority ?? '');
      const desc = textArea('req_description', r.description, 2);
      return {
        el: h('div', { class: 'wizard-field' }, h('div', { class: 'row-actions' }, group, prio), desc),
        read: () => ({ ...r, group: group.value.trim() || undefined, priority: prio.value || undefined, description: desc.value.trim() }),
      };
    }, base.requirements ?? [], () => ({ id: '', description: '' }), 'Add requirement', onChange);
    this.acs = new RowsEditor<FRDAC>((a) => {
      const crit = textArea('ac_criterion', a.criterion, 2);
      // Ids and the links to requirements stay as loaded; the server fills them for new rows.
      return { el: h('div', { class: 'wizard-field' }, crit), read: () => ({ ...a, criterion: crit.value.trim() }) };
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
      e.add('acceptance_criteria', fieldRow('Acceptance criteria', this.acs.el)).el,
      e.add('decisions', fieldRow('Decisions', this.decisions.el)).el,
      e.add('out_of_scope', fieldRow('Out of scope', this.f.out_of_scope, 'One per line')).el,
      verbatim.length ? h('p', { class: 'field-hint', text: `Kept as they are: ${verbatim.join(', ')}` }) : null,
      e.general);
  }

  /** read assembles the FRD: the loaded document with the edited fields replaced. */
  read(): FRD {
    const v = (k: keyof FrdForm['f']): string => this.f[k].value.trim();
    const some = <T>(a: T[]): T[] | undefined => (a.length ? a : undefined);
    const requirements = this.reqs.read().filter((r) => r.id || r.description);
    // Ids are server-assigned and hidden, so a deleted requirement must not leave dead links in the criteria.
    const ids = new Set(requirements.map((r) => r.id));
    const acs = this.acs.read().filter((a) => a.id || a.criterion)
      .map((a) => (a.requirements ? { ...a, requirements: a.requirements.filter((id) => ids.has(id)) } : a));
    return {
      ...this.base,
      id: v('id'),
      title: v('title'),
      status: v('status') || undefined,
      objective: v('objective'),
      context: v('context') || undefined,
      constraints: some(lines(this.f.constraints.value)),
      groups: some(this.groups.read().filter((g) => g.id || g.title)),
      requirements,
      acceptance_criteria: acs,
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
  // Review mode shows the FRD as a card; editing swaps in the structured editor.
  private editing = false;
  private desc = '';
  /** The description last sent for drafting; it is cleared from the box when the draft finishes unchanged. */
  private sentDesc = '';
  /** The describe box lives across renders so a refresh never rebuilds it under the cursor. */
  private box: HTMLTextAreaElement | null = null;
  private draftBtn: HTMLElement | null = null;
  private root: HTMLElement | null = null;
  private top = h('div', {});
  private slot = h('div', {});
  private slotDyn = h('div', {});
  private slotFor = 0;
  private bottom = h('div', {});
  private draft: DraftState | null = null;
  private draftError = '';
  private dod: DefinitionOfDone | null = null;
  private size: SizeView | null = null;
  private parts: FrdPart[] = [];
  private partErrors = new Map<number, string>();
  private splitError = '';
  private disposed = false;
  private ticker: ReturnType<typeof setInterval> | null = null;
  private clocks: Array<{ el: HTMLElement; since: number }> = [];

  constructor(private host: HTMLElement, private slug: string, private deps: FrdDeps) {}

  mount(): () => void {
    this.off = subscribe((ev: AgentflowEvent) => {
      const mine = 'project' in ev && ev.project === this.slug;
      if (!this.dirty && (ev.type === 'state.changed' || mine && (ev.type === 'effort.update' || ev.type === 'project.update'))) void this.load();
    });
    this.stopPoll = startPoll(() => !this.dirty && (isTransient(this.current?.status) || this.draft?.state === 'running'), () => this.load());
    void this.load();
    return () => {
      this.disposed = true;
      if (this.ticker) clearInterval(this.ticker);
      this.ticker = null;
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
      // The side panels are best effort: a failed read shows nothing rather than blocking the FRD.
      const q = { quiet: true };
      const none = (): null => null;
      const [draft, dod, size, parts] = await Promise.all([
        this.deps.draftState(this.slug, ef.id, q).catch(none),
        this.deps.definitionOfDone(this.slug, q).catch(none),
        this.latest ? this.deps.frdSize(this.slug, ef.id, q).catch(none) : null,
        this.deps.frdParts(this.slug, ef.id, q).catch(none),
      ]);
      if (this.disposed) return;
      if (this.draft?.state === 'running' && draft?.state === 'idle' && this.draft.purpose !== 'split' && this.draft.purpose !== 'polish' && this.desc.trim() === this.sentDesc) this.desc = '';
      this.draft = draft;
      this.draftError = '';
      this.dod = dod;
      this.size = size;
      this.parts = parts ?? [];
      this.form = new FrdForm(this.latest?.json ?? blankFRD(this.slug, ef), () => { this.dirty = true; this.auto?.touch(); });
      this.armAuto();
    } else {
      this.form = null;
      this.auto?.stop();
    }
    if (!this.latest) this.editing = false;
    this.render();
  }

  async startDraft(polish: boolean): Promise<void> {
    const ef = this.current;
    const description = polish ? '' : this.desc.trim();
    if (!ef || (!polish && description === '')) return;
    this.draftError = '';
    try {
      await this.deps.draftFRD(this.slug, ef.id, description, polish, { quiet: true });
    } catch (err) {
      this.draftError = err instanceof Error ? err.message : String(err);
      this.render();
      return;
    }
    this.sentDesc = description;
    this.draft = { state: 'running', purpose: polish ? 'polish' : 'draft', started: Date.now() };
    this.render();
  }

  async split(): Promise<void> {
    const ef = this.current;
    if (!ef) return;
    this.splitError = '';
    try {
      await this.deps.splitFRD(this.slug, ef.id, { quiet: true });
    } catch (err) {
      this.splitError = err instanceof Error ? err.message : String(err);
      this.render();
      return;
    }
    this.draft = { state: 'running', purpose: 'split', started: Date.now() };
    this.render();
  }

  async startPart(n: number): Promise<void> {
    const ef = this.current;
    if (!ef || this.efforts.some((e) => !isFinished(e))) return;
    this.partErrors.delete(n);
    try {
      const started = await this.deps.startPart(this.slug, ef.id, n, { quiet: true });
      if (started?.id) rememberEffort(this.slug, started.id);
    } catch (err) {
      this.partErrors.set(n, err instanceof Error ? err.message : String(err));
      this.render();
      return;
    }
    this.deps.notify(`Started part ${n}`);
    await this.load();
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

  private auto: Autosave | null = null;
  private revHost = h('div');
  private hintEl: HTMLElement | null = null;

  /** armAuto starts watching the form that was just built; a pause after a change stores a new revision. */
  private armAuto(startDirty = false): void {
    this.auto?.stop();
    this.auto = autosave({ snapshot: () => JSON.stringify(this.form?.read() ?? null), save: () => this.save(), startDirty });
    if (this.form) this.auto.watch(this.form.el);
    if (startDirty) this.auto.touch();
  }

  /** save stores the form as a new revision and stays in the editor, so typing is not interrupted. */
  async save(): Promise<boolean> {
    const ef = this.current;
    if (!ef || !this.form) return false;
    this.form.errors.clear();
    try {
      const saved = await this.deps.putFRD(this.slug, ef.id, this.form.read(), { quiet: true });
      this.latest = { ...(this.latest ?? { created_by: '', created: 0, approved: false }), ...saved };
      this.revisions = await this.deps.frdRevisions(this.slug, ef.id);
    } catch (err) {
      this.form.errors.show(err);
      return false;
    }
    this.dirty = false;
    replace(this.revHost, this.revisionList());
    if (this.hintEl) this.hintEl.textContent = `Latest revision ${this.latest?.revision ?? ''}; not approved`;
    return true;
  }

  /** closeEditor saves a pending change, then shows the saved revision for review and approval. */
  private async closeEditor(): Promise<void> {
    await this.auto?.flush();
    this.editing = false;
    await this.loadDoc();
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
    this.form = new FrdForm(doc, () => { this.dirty = true; this.auto?.touch(); });
    this.armAuto(true);
    this.dirty = true;
    this.editing = true;
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

  /** strip is the progress strip of a running draft or split: label, elapsed time and an animated bar. */
  private strip(label: string, since: number): HTMLElement {
    const elapsed = h('span', { class: 'field-hint', text: since > 0 ? elapsedText(Date.now() - since) : '', attrs: { 'data-role': 'elapsed' } });
    this.clocks.push({ el: elapsed, since });
    return h('div', { class: 'progress-strip', attrs: { 'data-role': 'progress-strip', role: 'status' } },
      h('div', { class: 'row-actions' }, h('strong', { text: label }), elapsed),
      h('div', { class: 'progress-bar-wrap' }, h('div', { class: 'progress-bar progress-indeterminate' })));
  }

  /** syncTicker keeps one timer alive while any strip is on screen. */
  private syncTicker(): void {
    if (this.clocks.length === 0 || this.disposed) {
      if (this.ticker) clearInterval(this.ticker);
      this.ticker = null;
      return;
    }
    if (!this.ticker) {
      this.ticker = setInterval(() => {
        for (const c of this.clocks) if (c.since > 0) c.el.textContent = elapsedText(Date.now() - c.since);
      }, 1000);
    }
  }

  /** stripLabel names what the server is running, from the draft state's purpose. */
  private stripLabel(): string {
    const p = this.draft?.purpose;
    return p === 'split' ? 'Splitting the FRD' : p === 'polish' ? 'Tidying the FRD' : 'Drafting the FRD';
  }

  /** describeBody is the part of the describe section that changes: the strip, the error and the buttons. */
  private describeBody(): Child[] {
    const running = this.draft?.state === 'running';
    const mine = this.draft?.purpose !== 'split';
    const draftBtn = button('Draft the FRD', () => void this.startDraft(false), { primary: true, field: 'draft-frd', icon: 'sparkle' });
    this.draftBtn = draftBtn;
    if (this.desc.trim() === '') draftBtn.className += ' af-disabled';
    const error = this.draftError || (this.draft?.state === 'failed' && mine ? this.draft.error ?? 'The draft failed' : '');
    return [
      running && mine ? this.strip(this.stripLabel(), this.draft?.started ?? 0) : null,
      error ? h('div', { class: 'field-error', attrs: { role: 'alert', 'data-role': 'draft-error' }, text: error }) : null,
      running ? null : h('div', { class: 'settings-actions' },
        this.latest ? null : button('Write it by hand', () => { this.editing = true; this.render(); }, { field: 'edit-frd', icon: 'edit' }),
        draftBtn),
    ];
  }

  private bullets(title: string, items: string[]): HTMLElement | null {
    return items.length ? h('div', { class: 'plan-section' }, h('h4', { text: title }), h('ul', {}, ...items.map((t) => h('li', { text: t })))) : null;
  }

  /** reviewCard shows the latest FRD as a document: title, objective, requirements and criteria as plain lists. */
  private reviewCard(doc: FRD, approvedRev: number | undefined, canApprove: boolean): HTMLElement {
    const rev = this.latest?.revision ?? 0;
    const running = this.draft?.state === 'running';
    // Only a draft effort can be redrafted or edited; later it is read-only.
    const open = this.current?.status === 'draft' && !running;
    return h('div', { class: 'settings-section', attrs: { 'data-role': 'frd-review' } },
      h('div', { class: 'row-actions' },
        h('h3', { text: doc.title }),
        h('span', { class: 'field-hint', text: `rev ${rev}` }),
        approvedRev === rev ? badge('approved', 'ok') : badge(approvedRev ? `rev ${approvedRev} approved` : 'not approved', 'warn')),
      doc.objective ? h('p', { text: doc.objective }) : null,
      this.bullets('Requirements', (doc.requirements ?? []).map((r) => (r.priority ? `${r.priority}: ` : '') + r.description)),
      this.bullets('Acceptance criteria', (doc.acceptance_criteria ?? []).map((a) => a.criterion)),
      this.bullets('Out of scope', doc.out_of_scope ?? []),
      running ? null : h('div', { class: 'settings-actions' },
        open ? button('Tidy the text', () => void this.startDraft(true), { field: 'polish-frd', icon: 'sparkle' }) : null,
        open ? button('Edit', () => { this.editing = true; this.render(); }, { field: 'edit-frd', icon: 'edit' }) : null,
        canApprove ? button(`Approve revision ${rev}`, () => void this.approve(), { primary: true, field: 'approve-frd', icon: 'approve' }) : null));
  }

  private dodSection(): HTMLElement | null {
    const items = this.dod?.items ?? [];
    if (items.length === 0) return null;
    return h('div', { class: 'detail-section', attrs: { 'data-role': 'definition-of-done' } },
      h('h4', { text: 'Definition of done', attrs: { title: 'Applies to every task, so it need not be written as acceptance criteria' } }),
      h('ul', { class: 'field-hint' }, ...items.map((t) => h('li', { text: t }))));
  }

  /** sizeRow is the FRD's token count against the smallest planning context, with the split offer when it is over. */
  private sizeRow(): HTMLElement {
    const z = this.size;
    if (!z || !(z.budget_tokens > 0) || typeof z.over !== 'boolean') {
      return h('div', { class: 'console-header', attrs: { 'data-role': 'size-unknown' } },
        h('span', { class: 'field-hint', attrs: { title: 'Set context sizes in Backends', 'aria-label': 'Set context sizes in Backends' } }, icon('gauge')));
    }
    const pct = Math.min(100, Math.round((z.frd_tokens / z.budget_tokens) * 100));
    const unknown = z.unknown_models ?? [];
    const unknownTip = `Context size unknown for: ${unknown.map((m) => `${m.backend}/${m.model}`).join(', ')}`;
    return h('div', { class: 'console-header', attrs: { 'data-role': 'size' } },
      h('span', { class: 'field-hint', attrs: { title: 'Size of the FRD against what the smallest planning model can read', 'aria-label': 'Size of the FRD against what the smallest planning model can read' } }, icon('gauge')),
      unknown.length ? h('span', { class: 'field-hint', attrs: { 'data-role': 'unknown-models', title: unknownTip, 'aria-label': unknownTip } }, icon('warn')) : null,
      h('div', { class: 'progress-bar-wrap' }, h('div', { class: 'progress-bar' + (z.over ? ' progress-over' : ''), attrs: { style: `width: ${pct}%` } })),
      h('span', { class: z.over ? 'progress-pct field-error' : 'progress-pct', text: `${formatTokens(z.frd_tokens)} / ${formatTokens(z.budget_tokens)} tokens`, attrs: { title: z.advice, 'aria-label': z.advice } }),
      z.over && this.draft?.state !== 'running' ? button('Split into smaller parts', () => void this.split(), { field: 'split-frd', icon: 'split' }) : null);
  }

  private partsList(): HTMLElement | null {
    if (this.parts.length === 0) return null;
    const busy = this.efforts.find((e) => !isFinished(e));
    return h('div', { class: 'wizard-list', attrs: { 'data-role': 'frd-parts' } }, ...this.parts.map((p) => {
      const started = p.started_effort > 0;
      const pending = !started && p.n > 1;
      const start = pending ? button('Start this part', () => void this.startPart(p.n), { field: 'start-part', icon: 'start' }) : null;
      if (start && busy) {
        start.className += ' af-disabled';
        start.setAttribute('aria-disabled', 'true');
        start.setAttribute('title', `Finish or abandon #${busy.id} first`);
      }
      return h('div', { class: 'wizard-field', attrs: { 'data-part': p.n } },
        h('div', { class: 'inline-row' },
          h('strong', { text: `${p.n}. ${p.title}` }),
          badge(`${formatTokens(p.tokens)} tokens`, 'muted'),
          started ? badge('started', 'ok') : null,
          start),
        p.summary ? h('p', { class: 'field-hint', text: p.summary }) : null,
        this.partErrors.get(p.n) ? h('div', { class: 'field-error', attrs: { role: 'alert' }, text: this.partErrors.get(p.n) }) : null);
    }));
  }

  /** renderDescribe keeps the describe box element across renders (so typing is never lost) and refreshes everything around it. */
  private renderDescribe(ef: EffortView | undefined): void {
    if (!ef) {
      replace(this.slot);
      this.slotFor = 0;
      return;
    }
    if (!this.box) {
      this.box = textArea('description', this.desc, 4);
      this.box.setAttribute('placeholder', 'What do you want?');
      this.box.addEventListener('input', () => {
        this.desc = (this.box as HTMLTextAreaElement).value;
        const b = this.draftBtn;
        if (b) b.className = b.className.replace(' af-disabled', '') + (this.desc.trim() === '' ? ' af-disabled' : '');
      });
    }
    if (this.box.value !== this.desc) this.box.value = this.desc;
    replace(this.slotDyn, ...this.describeBody());
    if (this.slotFor !== ef.id || this.slot.firstChild === null) {
      replace(this.slot, section(`FRD for #${ef.id}`, this.box, this.slotDyn));
      this.slotFor = ef.id;
    }
  }

  render(): void {
    if (this.disposed) return;
    if (!this.loaded) {
      this.root = null;
      replace(this.host, h('p', { class: 'field-hint', text: 'Loading efforts…' }));
      return;
    }
    this.clocks = [];
    const ef = this.current;
    const approvedRev = this.revisions.find((r) => r.approved)?.revision;
    // The one obvious next step: approving a saved, unedited revision; otherwise saving.
    const canApprove = !!ef && !!this.latest && this.latest.revision !== approvedRev && !this.dirty && !isFinished(ef);
    const editor = ef && this.form ? section(`FRD for #${ef.id}`,
      h('div', { class: 'row-actions' },
        this.hintEl = h('span', { class: 'field-hint', text: this.latest ? `Latest revision ${this.latest.revision}${approvedRev ? `; revision ${approvedRev} approved` : '; not approved'}` : 'Not saved yet' })),
      this.form.el,
      h('div', { class: 'settings-actions' },
        this.auto?.status ?? null,
        button('Done', () => void this.closeEditor(), { primary: true, field: 'close-editor', icon: 'approve' }))) : null;
    const splitError = this.splitError || (this.draft?.state === 'failed' && this.draft.purpose === 'split' ? this.draft.error ?? 'The split failed' : '');
    const review = ef && !this.editing && this.latest?.json ? [
      this.reviewCard(this.latest.json, approvedRev, canApprove),
      this.dodSection(),
      this.draft?.state === 'running' && this.draft.purpose === 'split' ? this.strip(this.stripLabel(), this.draft.started ?? 0) : this.sizeRow(),
      splitError ? h('div', { class: 'field-error', attrs: { role: 'alert', 'data-role': 'split-error' }, text: splitError }) : null,
      this.partsList(),
    ] : [];
    // A project has one active effort at a time: show the form only when none is active, else a disabled "+" says why.
    const active = this.efforts.find((e) => !isFinished(e));
    const bar = this.efforts.length ? effortBar(this.slug, this.efforts, ef, (id) => { this.current = this.efforts.find((e) => e.id === id); void this.loadDoc(); }) : null;
    if (bar && active) {
      bar.appendChild(h('button', {
        class: 'ui-modal-btn af-disabled',
        text: '+',
        attrs: {
          type: 'button', 'data-action': 'new-effort', 'aria-disabled': 'true', 'aria-label': 'New effort',
          title: `New effort is unavailable: a project has one active effort at a time. Finish or abandon #${active.id} (${active.status.replace(/_/g, ' ')}) first.`,
        },
      }));
    }
    const showDescribe = !!ef && !this.editing && ef.status === 'draft';
    replace(this.top,
      bar,
      active ? null : this.efforts.length ? null : h('div', { class: 'plan-empty' }, h('h3', { text: 'No efforts yet' }), h('p', { text: 'An effort is one FRD, plan and queue. Create the first one below.' })),
      active ? null : this.newEffortForm());
    this.renderDescribe(showDescribe ? ef : undefined);
    replace(this.bottom,
      ...review,
      this.editing ? editor : null,
      ef && !isFinished(ef) ? effortSettings(ef, this.form ? JSON.stringify(this.form.read()) : '', this.deps, () => void this.load()) : null,
      ef ? (replace(this.revHost, this.revisionList()), this.revHost) : null,
      this.efforts.length ? section('Efforts', this.list()) : null);
    if (!this.root || this.host.firstChild !== this.root) {
      this.root = h('div', { class: 'settings-host' }, this.top, this.slot, this.bottom);
      replace(this.host, this.root);
    }
    this.syncTicker();
  }
}

/** mountFrd is the FRD sub-tab's mount. */
export function mountFrd(host: HTMLElement, slug: string): () => void {
  return new FrdEditor(host, slug, defaultFrdDeps()).mount();
}
