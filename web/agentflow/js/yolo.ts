// yolo.ts — the YOLO toggle (YO1, YO3): a persistent badge while it is on, an
// enable button that asks for confirmation (the dialog lists what stays
// enforced), a disable button that needs none, and the "always enforced" list
// from GET /api/yolo/invariants beside the toggle. Enabling and disabling are
// audited by the server.

import { confirmDialog, showToast } from '@shared';
import { api, type EffortView, type Invariant } from './api.js';
import { h, replace } from './dom.js';
import { button } from './forms.js';

export type YoloDeps = Pick<typeof api, 'setYOLO' | 'yoloInvariants'> & {
  confirm(message: string, confirmLabel?: string): Promise<boolean>;
  notify(message: string): void;
};

export const defaultYoloDeps = (): YoloDeps => ({
  setYOLO: api.setYOLO,
  yoloInvariants: api.yoloInvariants,
  confirm: (m, label = 'Confirm') => confirmDialog(m, { confirmLabel: label }),
  notify: (m) => void showToast(m, 'success'),
});

/** yoloBadge is the persistent marker shown on the effort header and the overview card. */
export function yoloBadge(): HTMLElement {
  return h('span', { class: 'badge badge-warn', text: 'YOLO', attrs: { 'data-role': 'yolo-badge', title: 'YOLO is on: the human gates approve themselves' } });
}

/** confirmText is the enable dialog's message, including what YOLO cannot relax. */
export function confirmText(effort: EffortView, invariants: Invariant[]): string {
  return `Enable YOLO for effort #${effort.id}? The FRD-to-plan, plan-to-queue and queue approvals will happen automatically, and a stuck task is retried once. Still enforced:\n` +
    invariants.map((i) => `- ${i.id}: ${i.text}`).join('\n');
}

/** yoloControl builds the toggle for one active effort; onChanged runs after a successful switch. */
export function yoloControl(slug: string, effort: EffortView, deps: YoloDeps, onChanged: () => void): HTMLElement {
  const listHost = h('div', { attrs: { 'data-role': 'yolo-invariants' } });
  let invariants: Invariant[] = [];
  const loading = deps.yoloInvariants().then((inv) => {
    invariants = inv;
    replace(listHost, h('details', {}, h('summary', { text: 'Always enforced, even under YOLO' }),
      h('ul', { class: 'plan-review-notes' }, ...inv.map((i) => h('li', {}, h('strong', { text: i.id }), ` ${i.text}`)))));
  }).catch(() => undefined);

  const toggle = async (): Promise<void> => {
    if (effort.yolo) {
      try {
        await deps.setYOLO(slug, effort.id, false, false);
      } catch {
        return;
      }
      deps.notify('YOLO disabled');
    } else {
      await loading;
      if (!(await deps.confirm(confirmText(effort, invariants), 'Enable YOLO'))) return;
      try {
        await deps.setYOLO(slug, effort.id, true, true);
      } catch {
        return;
      }
      deps.notify('YOLO enabled');
    }
    onChanged();
  };

  if (effort.status === 'complete' || effort.status === 'abandoned') return h('div', { attrs: { 'data-role': 'yolo' } }, effort.yolo ? yoloBadge() : null);
  return h('div', { class: 'detail-section', attrs: { 'data-role': 'yolo' } },
    h('div', { class: 'row-actions' },
      effort.yolo ? yoloBadge() : null,
      button(effort.yolo ? 'Disable YOLO' : 'Enable YOLO', () => void toggle(), { field: 'toggle-yolo' })),
    listHost);
}
