// current.ts — which effort of a project the FRD, Plan, Queue, Board and
// History tabs are looking at. The pick is kept in memory per project so the
// tabs agree; by default it is the active effort, else the newest one.

import type { EffortView } from './api.js';
import { chip, h } from './dom.js';
import { selectInput } from './forms.js';
import { yoloBadge, yoloControl, type YoloDeps } from './yolo.js';

const picked = new Map<string, number>();

/** isFinished is true for an effort that is complete or abandoned (PJ4). */
export function isFinished(e: { status: string }): boolean {
  return e.status === 'complete' || e.status === 'abandoned';
}

/** chooseEffort returns the effort a tab should show: the remembered pick, else the active one, else the newest. */
export function chooseEffort(slug: string, efforts: EffortView[]): EffortView | undefined {
  const want = picked.get(slug);
  return efforts.find((e) => e.id === want) ?? efforts.find((e) => !isFinished(e)) ?? efforts[efforts.length - 1];
}

/** rememberEffort stores the pick for the next tab. */
export function rememberEffort(slug: string, id: number): void {
  picked.set(slug, id);
}

/** forgetEfforts drops every remembered pick, for when all projects are gone. */
export function forgetEfforts(): void {
  picked.clear();
}

/** effortBar is the picker row: a select of the project's efforts and the chosen one's status. */
export function effortBar(
  slug: string,
  efforts: EffortView[],
  current: EffortView | undefined,
  onPick: (id: number) => void,
  yolo?: { deps: YoloDeps; onChanged: () => void },
): HTMLElement {
  const sel = selectInput('effort', efforts.map((e) => [String(e.id), `#${e.id} ${e.title}`] as [string, string]), String(current?.id ?? ''));
  sel.addEventListener('change', () => {
    rememberEffort(slug, Number(sel.value));
    onPick(Number(sel.value));
  });
  return h('div', { class: 'console-header' },
    h('span', { class: 'pane-title', text: 'Effort' }), sel,
    current ? chip(current.status) : null,
    current?.yolo && !yolo ? yoloBadge() : null,
    current && yolo ? yoloControl(slug, current, yolo.deps, yolo.onChanged) : null);
}
