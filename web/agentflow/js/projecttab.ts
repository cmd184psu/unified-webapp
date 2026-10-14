// projecttab.ts — the Project tab: a project picker with New and Edit, and the
// project's sub-tabs (FRD, Plan, Queue, Board, History). The sub-tab views are
// mounted from SUB_TABS; the form for creating and editing a project replaces
// the sub-tabs while it is open.

import { api, type ProjectView } from './api.js';
import { h, replace } from './dom.js';
import { mountBoard } from './board.js';
import { mountHistory } from './history.js';
import { mountFrd } from './frd-editor.js';
import { mountPlan } from './plan-view.js';
import { mountQueue } from './queue.js';
import { showProjectForm } from './projects.js';
import { button } from './forms.js';

/** A view mounts into host for one project and may return its dispose. */
export type ProjectMount = (host: HTMLElement, slug: string) => void | (() => void);

export interface SubTab {
  id: string;
  label: string;
  mount: ProjectMount;
}

export const SUB_TABS: SubTab[] = [
  { id: 'frd', label: 'FRD', mount: mountFrd },
  { id: 'plan', label: 'Plan', mount: mountPlan },
  { id: 'queue', label: 'Queue', mount: mountQueue },
  { id: 'board', label: 'Board', mount: mountBoard },
  { id: 'history', label: 'History', mount: mountHistory },
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

/** mountProjectTab renders the picker and sub-tabs into host and returns the dispose; wanted "+new" opens the create form. */
export function mountProjectTab(host: HTMLElement, wanted: string): () => void {
  let dispose: void | (() => void);
  let sub = SUB_TABS[0].id;
  let slug = wanted === '+new' ? remembered() : wanted || remembered();
  let projects: ProjectView[] = [];
  const picker = h('select', { class: 'settings-input', attrs: { 'aria-label': 'Project' } }) as HTMLSelectElement;
  const bar = h('div', { class: 'tab-bar', attrs: { role: 'tablist' } });
  const body = h('div', { class: 'project-body' });

  const closeSub = (): void => {
    if (typeof dispose === 'function') dispose();
    dispose = undefined;
  };

  const showSub = (): void => {
    closeSub();
    replace(bar, ...SUB_TABS.map((t) =>
      h('button', {
        class: 'tab-btn' + (t.id === sub ? ' active' : ''),
        text: t.label,
        attrs: { type: 'button', 'data-sub': t.id },
        on: { click: () => { sub = t.id; showSub(); } },
      })));
    if (!slug) {
      replace(body, h('p', { class: 'field-hint', text: 'Pick a project, or create one with New project.' }));
      return;
    }
    const tab = SUB_TABS.find((t) => t.id === sub) ?? SUB_TABS[0];
    replace(body);
    dispose = tab.mount(body, slug);
  };

  const reload = async (): Promise<void> => {
    try {
      projects = await api.projects();
    } catch {
      projects = [];
    }
    if (!projects.some((p) => p.slug === slug)) slug = projects[0]?.slug ?? '';
    replace(picker, ...projects.map((p) => h('option', { text: p.slug, attrs: { value: p.slug, selected: p.slug === slug } })));
    picker.value = slug;
    showSub();
  };

  const showForm = (project?: ProjectView): void => {
    closeSub();
    replace(bar);
    showProjectForm(body, { project }, (saved) => {
      if (saved) {
        slug = saved;
        remember(saved);
      }
      void reload();
    });
  };

  picker.addEventListener('change', () => {
    slug = picker.value;
    remember(slug);
    showSub();
  });
  replace(host,
    h('div', { class: 'console-header' },
      h('span', { class: 'pane-title', text: 'Project' }), picker,
      button('New project', () => showForm(), { field: 'new-project' }),
      button('Edit project', () => { const cur = projects.find((p) => p.slug === slug); if (cur) showForm(cur); }, { field: 'edit-project' })),
    bar, body);

  if (wanted === '+new') showForm();
  else void reload();

  return closeSub;
}
