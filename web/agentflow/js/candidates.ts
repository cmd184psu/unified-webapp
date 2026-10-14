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

/** moveItem returns list with the item at from moved to index to (clamped); the input is not changed. */
export function moveItem<T>(list: T[], from: number, to: number): T[] {
  const out = list.slice();
  if (from < 0 || from >= out.length) return out;
  const [it] = out.splice(from, 1);
  out.splice(Math.max(0, Math.min(out.length, to)), 0, it);
  return out;
}

/** dropIndex is where a dragged row lands: the number of other rows whose midpoint is above y. */
export function dropIndex(mids: number[], from: number, y: number): number {
  return mids.filter((m, i) => i !== from && m < y).length;
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

/**
 * attachDrag lets a handle drag its row among its siblings (mouse and touch, as
 * the grocery group list does): on release the row is moved to where the pointer
 * is. getMove returns the move function of the current draw.
 */
function attachDrag(handle: HTMLElement, row: HTMLElement, getMove: () => (to: number) => void): void {
  const release = (y: number): void => {
    if (!Number.isFinite(y)) return;
    row.classList.remove('dragging');
    const rows = Array.from(row.parentElement?.children ?? []);
    const mids = rows.map((r) => { const b = r.getBoundingClientRect(); return b.top + b.height / 2; });
    getMove()(dropIndex(mids, rows.indexOf(row), y));
  };
  handle.addEventListener('mousedown', (e) => {
    e.preventDefault();
    row.classList.add('dragging');
    const onUp = (ev: MouseEvent): void => {
      document.removeEventListener('mouseup', onUp);
      release(ev.clientY);
    };
    document.addEventListener('mouseup', onUp);
  });
  handle.addEventListener('touchstart', () => {
    row.classList.add('dragging');
    let y = NaN;
    const onMove = (ev: TouchEvent): void => { y = ev.touches[0].clientY; ev.preventDefault(); };
    const onEnd = (): void => {
      handle.removeEventListener('touchmove', onMove);
      handle.removeEventListener('touchend', onEnd);
      release(y);
    };
    handle.addEventListener('touchmove', onMove, { passive: false });
    handle.addEventListener('touchend', onEnd);
  }, { passive: true });
}

/** buildRoleEditor returns a card that edits one role's candidate list. */
export function buildRoleEditor(o: RoleEditorOpts): HTMLElement {
  const { role } = o;
  const host = h('div', { class: 'profile-card', attrs: { 'data-role-editor': role } });
  const errors = new FormErrors();
  let focusIdx = -1;
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
    if (o.rows().length === 0 && pickBackend.value && pickModel.value) {
      // Save on an empty list adds the pair picked in the dropdowns, as Add candidate would.
      o.setDraft([{ backend: pickBackend.value, model: pickModel.value, optIn: !isPaid(o.backends, pickBackend.value, pickModel.value) }]);
    }
    if (o.rows().length === 0) {
      errors.show(new Error('Nothing to save yet: pick a backend and model, press Add candidate, then Save order'));
      return;
    }
    const missing = unconsented(o.backends, o.rows());
    if (missing.length > 0) {
      draw();
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

  const handles: HTMLElement[] = [];

  const draw = (): void => {
    const list = o.rows();
    handles.length = 0;
    replace(host,
      h('div', { class: 'profile-header' }, h('strong', { text: role }), o.hasDraft() ? badge('unsaved', 'warn') : null),
      h('div', { class: 'wizard-list' }, ...(list.length === 0
        ? [h('p', { class: 'field-hint', text: o.emptyNote })]
        : list.map((r, i) => {
          const paid = isPaid(o.backends, r.backend, r.model);
          const optIn = checkbox('opt_in', r.optIn);
          optIn.addEventListener('change', () => edit((all) => { all[i].optIn = optIn.checked; return all; }));
          const row = h('div', { class: 'inline-row', attrs: { 'data-candidate': `${role}:${i}` } });
          const handle = h('span', {
            class: 'drag-handle',
            text: '\u283F',
            attrs: { role: 'button', tabindex: 0, 'data-action': 'reorder', 'aria-label': `Reorder ${r.backend} / ${r.model}: drag, or press the up and down arrow keys`, title: 'Drag to reorder (or use the up and down arrow keys)' },
          });
          handles.push(handle);
          const moveTo = (to: number): void => {
            if (to < 0 || to >= list.length || to === i) return;
            focusIdx = to;
            edit((all) => moveItem(all, i, to));
          };
          handle.addEventListener('keydown', (ev) => {
            const key = (ev as KeyboardEvent).key;
            if (key !== 'ArrowUp' && key !== 'ArrowDown') return;
            ev.preventDefault();
            moveTo(i + (key === 'ArrowUp' ? -1 : 1));
          });
          attachDrag(handle, row, () => moveTo);
          replace(row,
            handle,
            h('span', { text: `${i + 1}. ${r.backend} / ${r.model}` }),
            paid ? badge('paid') : null,
            paid ? h('span', { class: 'inline-check' }, optIn, 'I opt in to paid use') : null,
            button('Remove', () => edit((all) => all.filter((_, j) => j !== i)), { field: 'remove', icon: 'remove' }));
          return row;
        }))),
      h('div', { class: 'inline-row' }, pickBackend, pickModel,
        button('Add candidate', () => {
          if (pickBackend.value && pickModel.value) {
            edit((all) => [...all, { backend: pickBackend.value, model: pickModel.value, optIn: !isPaid(o.backends, pickBackend.value, pickModel.value) }]);
          }
        }, { field: 'add-candidate', icon: 'add' })),
      errors.general,
      h('div', { class: 'settings-actions' },
        ...(o.extra ?? []),
        button('Discard', () => { o.setDraft(null); draw(); }, { field: 'discard', icon: 'cancel' }),
        button('Save order', () => void save(), { primary: true, field: 'save-candidates', icon: 'save' })));
    // A keyboard move redraws the list; keep the focus on the handle that moved.
    if (focusIdx >= 0) handles[focusIdx]?.focus();
    focusIdx = -1;
  };
  draw();
  return host;
}
