// projecttab.ts — the Project tab: a project picker with New and Edit, and the
// project's sub-tabs (FRD, Plan, Queue, Board, History). The sub-tab views are
// mounted from SUB_TABS; the form for creating and editing a project replaces
// the sub-tabs while it is open.

import { Activity } from './activity.js';
import { api, type EffortView, type ProjectView } from './api.js';
import { chooseEffort, subTabBlock } from './current.js';
import { subscribe } from './events.js';
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
  let disposed = false;
  let sub = SUB_TABS[0].id;
  let slug = wanted === '+new' ? remembered() : wanted || remembered();
  let projects: ProjectView[] = [];
  /** sub-tab id -> why it is closed; absent when open. Read from the effort being looked at. */
  let blocked = new Map<string, string>();
  const picker = h('select', { class: 'settings-input', attrs: { 'aria-label': 'Project' } }) as HTMLSelectElement;
  const bar = h('div', { class: 'tab-bar', attrs: { role: 'tablist' } });
  const body = h('div', { class: 'project-body' });
  const activityEl = h('div', { class: 'activity-strip', attrs: { role: 'status', hidden: true, 'data-role': 'activity' } });
  const activity = new Activity(activityEl, (id) => { if (!blocked.has(id)) { sub = id; showSub(); } });
  // The remembered project may be gone: the strip points at nothing until reload has checked it exists.
  const stopActivity = activity.mount('');

  const closeSub = (): void => {
    if (typeof dispose === 'function') dispose();
    dispose = undefined;
  };

  const drawBar = (): void => {
    replace(bar, ...SUB_TABS.map((t) => {
      const why = blocked.get(t.id) ?? '';
      return h('button', {
        class: 'tab-btn' + (t.id === sub ? ' active' : ''),
        text: t.label,
        attrs: { type: 'button', 'data-sub': t.id, disabled: why !== '', title: why || undefined, 'aria-disabled': why !== '' ? 'true' : undefined },
        on: { click: () => { if (blocked.has(t.id)) return; sub = t.id; showSub(); } },
      });
    }));
  };

  /** readBlocks reads which sub-tabs the project's current effort has something to show in. */
  const readBlocks = async (): Promise<void> => {
    let efforts: EffortView[] = [];
    if (slug) {
      try {
        efforts = await api.efforts(slug);
      } catch {
        efforts = [];
      }
    }
    const ef = chooseEffort(slug, efforts);
    blocked = new Map(SUB_TABS.map((t) => [t.id, subTabBlock(t.id, ef)] as [string, string]).filter(([, why]) => why !== ''));
  };

  const showSub = (): void => {
    closeSub();
    drawBar();
    if (!slug) {
      replace(body, h('p', { class: 'field-hint', text: 'Pick a project, or create one with New project.' }));
      return;
    }
    if (blocked.has(sub)) {
      sub = SUB_TABS[0].id;
      drawBar();
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
    if (disposed) return;
    if (!projects.some((p) => p.slug === slug)) {
      slug = projects[0]?.slug ?? '';
      remember(slug); // a project that no longer exists is forgotten, not asked for again
    }
    // A project with an active run says so, so the one that is working is never mistaken for an idle one.
    const active = (p: ProjectView): boolean => p.run_state === 'running' || p.run_state === 'pausing';
    replace(picker, ...projects.map((p) => h('option', { text: active(p) ? `${p.slug}  ● running` : p.slug, attrs: { value: p.slug, selected: p.slug === slug } })));
    picker.value = slug;
    activity.setProject(slug);
    await readBlocks();
    if (disposed) return;
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
    activity.setProject(slug);
    void readBlocks().then(() => { if (!disposed) showSub(); });
  });
  replace(host,
    h('div', { class: 'console-header' },
      h('span', { class: 'pane-title', text: 'Project' }), picker,
      button('New project', () => showForm(), { field: 'new-project', icon: 'add' }),
      button('Edit project', () => { const cur = projects.find((p) => p.slug === slug); if (cur) showForm(cur); }, { field: 'edit-project', icon: 'edit' })),
    activityEl, bar, body);

  // The efforts move on by themselves (planning, compiling, approving): reopen tabs as they get something to show.
  const off = subscribe((ev) => {
    if (!slug || !['effort.update', 'project.update', 'state.changed', 'task.update'].includes(ev.type)) return;
    if ('project' in ev && ev.project !== slug) return;
    void readBlocks().then(() => {
      if (disposed || !bar.firstChild) return; // the project form is open
      if (blocked.has(sub)) showSub();
      else drawBar();
    });
  });

  if (wanted === '+new') showForm();
  else void reload();

  return () => {
    disposed = true;
    off();
    stopActivity();
    closeSub();
  };
}
