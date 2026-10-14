// projecttab.ts — the Project tab: a project picker and the project's sub-tabs
// (FRD, Plan, Queue, Board, History). The sub-tab views are placeholders until
// E3 (FRD, Plan), E4 (Queue, Board) and E5 (History) replace their `mount`.

import { api } from './api.js';
import { h, replace } from './dom.js';

/** A view mounts into host for one project and may return its dispose. */
export type ProjectMount = (host: HTMLElement, slug: string) => void | (() => void);

export interface SubTab {
  id: string;
  label: string;
  mount: ProjectMount;
}

function placeholder(story: string): ProjectMount {
  return (host) => replace(host, h('p', { class: 'field-hint', text: `coming in ${story}` }));
}

export const SUB_TABS: SubTab[] = [
  { id: 'frd', label: 'FRD', mount: placeholder('E3') },
  { id: 'plan', label: 'Plan', mount: placeholder('E3') },
  { id: 'queue', label: 'Queue', mount: placeholder('E4') },
  { id: 'board', label: 'Board', mount: placeholder('E4') },
  { id: 'history', label: 'History', mount: placeholder('E5') },
];

const KEY = 'agentflow:project';

function remembered(): string {
  try {
    return localStorage.getItem(KEY) ?? '';
  } catch {
    return '';
  }
}

function remember(slug: string): void {
  try {
    localStorage.setItem(KEY, slug);
  } catch {
    /* private mode: the pick just is not remembered */
  }
}

/** mountProjectTab renders the picker and sub-tabs into host and returns the dispose. */
export function mountProjectTab(host: HTMLElement, wanted: string): () => void {
  let dispose: void | (() => void);
  let sub = SUB_TABS[0].id;
  let slug = wanted || remembered();
  const picker = h('select', { class: 'settings-input', attrs: { 'aria-label': 'Project' } }) as HTMLSelectElement;
  const bar = h('div', { class: 'tab-bar', attrs: { role: 'tablist' } });
  const body = h('div', { class: 'project-body' });

  const showSub = (): void => {
    if (typeof dispose === 'function') dispose();
    dispose = undefined;
    replace(bar, ...SUB_TABS.map((t) =>
      h('button', {
        class: 'tab-btn' + (t.id === sub ? ' active' : ''),
        text: t.label,
        attrs: { type: 'button', 'data-sub': t.id },
        on: { click: () => { sub = t.id; showSub(); } },
      })));
    if (!slug) {
      replace(body, h('p', { class: 'field-hint', text: 'Pick a project.' }));
      return;
    }
    const tab = SUB_TABS.find((t) => t.id === sub) ?? SUB_TABS[0];
    replace(body);
    dispose = tab.mount(body, slug);
  };

  picker.addEventListener('change', () => {
    slug = picker.value;
    remember(slug);
    showSub();
  });
  replace(host, h('div', { class: 'console-header' }, h('span', { class: 'pane-title', text: 'Project' }), picker), bar, body);

  void api.projects().then((ps) => {
    if (!ps.some((p) => p.slug === slug)) slug = ps[0]?.slug ?? '';
    replace(picker, ...ps.map((p) => h('option', { text: p.slug, attrs: { value: p.slug, selected: p.slug === slug } })));
    picker.value = slug;
    showSub();
  }).catch(() => showSub());
  showSub();

  return () => {
    if (typeof dispose === 'function') dispose();
  };
}
