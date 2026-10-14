// candidates.ts — the ordered model-candidate list of one role, shared by the
// global editor (Backends tab) and a project's override (project form). A paid
// candidate (paid backend or paid model) shows the paid badge and can only be
// saved after its opt-in box is ticked (MB5): paid models are used only when
// explicitly listed, and listing one here is that act.

import type { Backend, CandidateInput } from './api.js';
import { h, replace } from './dom.js';
import { FormErrors, badge, button, checkbox, selectInput } from './forms.js';

export const ROLES = ['worker', 'reviewer', 'planner', 'architect', 'critic', 'compiler'];

/** CandRow is one candidate being edited; optIn is the explicit consent a paid one needs. */
export interface CandRow {
  backend: string;
  model: string;
  optIn: boolean;
}

/** isPaid is true when the backend or the model is paid. */
export function isPaid(backends: Backend[], backend: string, model: string): boolean {
  const b = backends.find((x) => x.name === backend);
  return !!b && (b.paid || b.models.some((m) => m.model === model && m.paid));
}

/** unconsented lists the paid candidates whose opt-in box is not ticked. */
export function unconsented(backends: Backend[], rows: CandRow[]): CandRow[] {
  return rows.filter((r) => isPaid(backends, r.backend, r.model) && !r.optIn);
}

export interface RoleEditorOpts {
  role: string;
  backends: Backend[];
  /** The rows to show: the unsaved draft if there is one, else what the server has. */
  rows(): CandRow[];
  hasDraft(): boolean;
  /** Keeps (or with null drops) the unsaved rows across redraws of the surrounding view. */
  setDraft(rows: CandRow[] | null): void;
  /** Persists the list; a throw is shown beside the editor. */
  save(body: CandidateInput[]): Promise<void>;
  emptyNote: string;
  /** Extra buttons next to Discard and Save. */
  extra?: HTMLElement[];
}

/** buildRoleEditor returns a card that edits one role's candidate list. */
export function buildRoleEditor(o: RoleEditorOpts): HTMLElement {
  const { role } = o;
  const host = h('div', { class: 'profile-card', attrs: { 'data-role-editor': role } });
  const errors = new FormErrors();
  const edit = (fn: (r: CandRow[]) => CandRow[]): void => {
    o.setDraft(fn(o.rows().map((r) => ({ ...r }))));
    draw();
  };
  const active = o.backends.filter((b) => !b.retired);
  const pickBackend = selectInput('backend', active.map((b) => [b.name, b.name] as [string, string]), active[0]?.name ?? '');
  const pickModel = selectInput('model', [], '');
  const fillModels = (): void => {
    const b = active.find((x) => x.name === pickBackend.value);
    replace(pickModel, ...(b?.models ?? []).map((m) => h('option', { text: m.model + (m.paid ? ' (paid)' : ''), attrs: { value: m.model } })));
    pickModel.value = b?.models[0]?.model ?? '';
  };
  pickBackend.addEventListener('change', fillModels);
  fillModels();

  const save = async (): Promise<void> => {
    errors.clear();
    const missing = unconsented(o.backends, o.rows());
    if (missing.length > 0) {
      errors.show(new Error(`Tick the paid opt-in for: ${missing.map((m) => `${m.backend}/${m.model}`).join(', ')}`));
      return;
    }
    try {
      await o.save(o.rows().map((r) => ({ backend: r.backend, model: r.model })));
    } catch (err) {
      errors.show(err);
      return;
    }
    o.setDraft(null);
    draw();
  };

  const draw = (): void => {
    const list = o.rows();
    replace(host,
      h('div', { class: 'profile-header' }, h('strong', { text: role }), o.hasDraft() ? badge('unsaved', 'warn') : null),
      h('div', { class: 'wizard-list' }, ...(list.length === 0
        ? [h('p', { class: 'field-hint', text: o.emptyNote })]
        : list.map((r, i) => {
          const paid = isPaid(o.backends, r.backend, r.model);
          const optIn = checkbox('opt_in', r.optIn);
          optIn.addEventListener('change', () => edit((all) => { all[i].optIn = optIn.checked; return all; }));
          return h('div', { class: 'inline-row', attrs: { 'data-candidate': `${role}:${i}` } },
            h('span', { text: `${i + 1}. ${r.backend} / ${r.model}` }),
            paid ? badge('paid') : null,
            paid ? h('span', { class: 'inline-check' }, optIn, 'I opt in to paid use') : null,
            button('Up', () => edit((all) => { if (i > 0) [all[i - 1], all[i]] = [all[i], all[i - 1]]; return all; }), { field: 'up' }),
            button('Down', () => edit((all) => { if (i < all.length - 1) [all[i + 1], all[i]] = [all[i], all[i + 1]]; return all; }), { field: 'down' }),
            button('Remove', () => edit((all) => all.filter((_, j) => j !== i)), { field: 'remove' }));
        }))),
      h('div', { class: 'inline-row' }, pickBackend, pickModel,
        button('Add candidate', () => {
          if (pickBackend.value && pickModel.value) {
            edit((all) => [...all, { backend: pickBackend.value, model: pickModel.value, optIn: !isPaid(o.backends, pickBackend.value, pickModel.value) }]);
          }
        }, { field: 'add-candidate' })),
      errors.general,
      h('div', { class: 'settings-actions' },
        ...(o.extra ?? []),
        button('Discard', () => { o.setDraft(null); draw(); }, { field: 'discard' }),
        button('Save order', () => void save(), { primary: true, field: 'save-candidates' })));
  };
  draw();
  return host;
}
