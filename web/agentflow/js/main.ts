// main.ts — app shell for AgentFlow: the top-level tabs (Overview, Project,
// Backends), hamburger and the event stream. Views mount into
// #panel-view and return a dispose; the Project tab's sub-tabs are in
// projecttab.ts.

import { ThemeManager, HamburgerMenu } from '@shared';
import { api } from './api.js';
import { mountSaveMark } from './autosave.js';
import { h, replace } from './dom.js';
import { startEvents, startPoll } from './events.js';
import { AttentionBanner } from './attention.js';
import { HealthBanner } from './health.js';
import { Overview, defaultDeps } from './overview.js';
import { mountBackends } from './backends.js';
import { mountProjectTab } from './projecttab.js';
import { mountStats } from './stats.js';
import { SettingsView, defaultSettingsDeps } from './settings.js';

const themes = new ThemeManager({ module: 'agentflow', default: 'dark' });
themes.apply();

type TabId = 'overview' | 'project' | 'backends' | 'stats';

interface Tab {
  id: TabId;
  label: string;
  mount: (host: HTMLElement, arg: string) => void | (() => void);
}

const TABS: Tab[] = [
  {
    id: 'overview',
    label: 'Overview',
    mount: (host) => new Overview(host, defaultDeps((slug) => showTab('project', slug))).mount(),
  },
  { id: 'project', label: 'Project', mount: mountProjectTab },
  { id: 'backends', label: 'Backends', mount: mountBackends },
  { id: 'stats', label: 'Stats', mount: mountStats },
];

let active: TabId = 'overview';
let dispose: void | (() => void);

function renderTabs(): void {
  const host = document.getElementById('tab-bar');
  if (!host) return;
  replace(host, ...TABS.map((t) =>
    h('button', {
      class: 'tab-btn' + (t.id === active ? ' active' : ''),
      text: t.label,
      attrs: { type: 'button', 'data-tab': t.id, role: 'tab' },
      on: { click: () => showTab(t.id) },
    })));
}

function showTab(id: TabId, arg = ''): void {
  const tab = TABS.find((t) => t.id === id);
  const host = document.getElementById('panel-view');
  if (!tab || !host) return;
  if (typeof dispose === 'function') dispose();
  active = id;
  renderTabs();
  replace(host);
  dispose = tab.mount(host, arg);
}

function initHamburger(): void {
  const trigger = document.getElementById('hamburger-trigger');
  if (!trigger) return;
  new HamburgerMenu({
    title: 'AgentFlow',
    items: [
      { section: 'Settings' },
      // Settings apply as soon as they are saved; the view lives as long as the drawer.
      { id: 'settings', render: (host: HTMLElement) => { new SettingsView(host, defaultSettingsDeps()).mount(); } },
    ],
    themePicker: true,
    themes,
    side: 'right',
    // Adopt the button index.html already ships; appending a second trigger into it nests a button in a button.
    mountTrigger: trigger,
  });
}

// initServiceHold puts the hold in the header (AF-10): while the service is
// held nothing starts anywhere, and a background tab has to show that.
function initServiceHold(): void {
  const right = document.querySelector('#nav .nav-right');
  if (!right) return;
  const chip = h('span', { class: 'nav-hold', attrs: { 'data-role': 'service-hold', hidden: '', role: 'status' } });
  right.prepend(chip);
  const load = async (): Promise<void> => {
    let hold;
    try {
      hold = await api.serviceHold({ quiet: true });
    } catch {
      return;
    }
    if (!hold?.active) {
      chip.setAttribute('hidden', '');
      return;
    }
    chip.textContent = `Service held: ${hold.reason ?? ''}`;
    chip.title = hold.restart ? 'A restart waits for a long stage to end' : 'Nothing new starts until the hold is lifted';
    chip.removeAttribute('hidden');
  };
  startPoll(() => true, load);
  void load();
}

function initSaveMark(): void {
  const right = document.querySelector('#nav .nav-right');
  if (!right) return;
  // First in the corner, before the menu: always there, always the same place.
  const holder = h('span', { class: 'autosave-holder' });
  right.prepend(holder);
  mountSaveMark(holder);
}

initHamburger();
initSaveMark();
initServiceHold();
startEvents();
const attn = document.getElementById('attention-banner');
if (attn) new AttentionBanner(attn, api, (slug) => showTab('project', slug)).mount();
const banner = document.getElementById('health-banner');
if (banner) new HealthBanner(banner, api).mount();
showTab('overview');
