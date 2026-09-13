import { api, Capabilities } from './api.js';
import { renderGroups } from './groups.js';
import { renderTasks } from './tasks.js';
import { renderExecutions } from './executions.js';
import { renderMetrics } from './metrics.js';
import { renderOutput } from './output.js';

export let caps: Capabilities = { allow_sudo: false };

// authEnabled reflects whether the platform gate protects this module. It is
// derived from GET /api/auth/mode, which is always answered (even for an open
// module, where it returns an empty methods list). Drives the logout control.
let authEnabled = false;

const NAV_LINKS: Array<{ label: string; hash: string }> = [
  { label: 'Groups', hash: '#groups' },
  { label: 'Tasks', hash: '#tasks' },
  { label: 'Executions', hash: '#executions' },
  { label: 'Metrics', hash: '#metrics' },
];

// --- Icons (inline SVG, trusted static markup) ---
const ICON_MENU =
  '<svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><line x1="3" y1="6" x2="21" y2="6"/><line x1="3" y1="12" x2="21" y2="12"/><line x1="3" y1="18" x2="21" y2="18"/></svg>';
const ICON_LOGOUT =
  '<svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4"/><polyline points="16 17 21 12 16 7"/><line x1="21" y1="12" x2="9" y2="12"/></svg>';

// --- Auto-refresh: client-side pref, persisted in localStorage. ---
const AR_ENABLED_KEY = 'tm.autorefresh.enabled';
const AR_INTERVAL_KEY = 'tm.autorefresh.interval';
const AR_INTERVALS = [5, 10, 30, 60]; // seconds

function lsGet(key: string): string | null {
  try { return localStorage.getItem(key); } catch { return null; }
}
function lsSet(key: string, val: string): void {
  try { localStorage.setItem(key, val); } catch { /* ignore */ }
}

function arEnabled(): boolean { return lsGet(AR_ENABLED_KEY) === '1'; }
function arInterval(): number {
  const n = parseInt(lsGet(AR_INTERVAL_KEY) || '', 10);
  return AR_INTERVALS.includes(n) ? n : 5;
}

let arTimer: number | undefined;
function restartAutoRefresh(): void {
  if (arTimer !== undefined) { clearInterval(arTimer); arTimer = undefined; }
  if (!arEnabled()) return;
  arTimer = window.setInterval(() => {
    // Never auto-refresh the live output view (it streams over SSE), and skip
    // refreshing while a modal/form dialog is open so we don't wipe input.
    if (currentPage() === 'output') return;
    if (document.querySelector('.modal, dialog[open]')) return;
    renderPage();
  }, arInterval() * 1000);
}

function currentPage(): string {
  const hash = window.location.hash || '#groups';
  return hash.slice(1).split('/')[0];
}

// --- Server status panel (populated lazily when the menu opens). ---
async function refreshStatusPanel(): Promise<void> {
  const statusEl = document.getElementById('st-status');
  const sudoToggle = document.getElementById('st-sudo-toggle') as HTMLInputElement | null;
  if (sudoToggle && !sudoToggle.disabled) sudoToggle.checked = caps.allow_sudo;
  if (!statusEl) return;
  statusEl.textContent = 'checking…';
  statusEl.className = 'st-value st-muted';
  try {
    const h = await api.health();
    statusEl.textContent = h.status === 'ok' ? 'healthy' : h.status;
    statusEl.className = 'st-value st-ok';
  } catch {
    statusEl.textContent = 'unreachable';
    statusEl.className = 'st-value st-err';
  }
}

function closeMenu(): void {
  const panel = document.getElementById('nav-menu-panel');
  const btn = document.getElementById('nav-menu-btn');
  if (panel) panel.hidden = true;
  if (btn) btn.setAttribute('aria-expanded', 'false');
}

function buildNav(): void {
  const nav = document.getElementById('nav');
  if (!nav) return;
  nav.textContent = '';

  const brand = document.createElement('a');
  brand.className = 'nav-brand';
  brand.textContent = 'taskmaster';
  brand.href = '#groups';
  nav.appendChild(brand);

  // Inline page links (hidden below the responsive breakpoint via CSS).
  const inlineLinks = document.createElement('div');
  inlineLinks.className = 'nav-links';
  NAV_LINKS.forEach(({ label, hash }) => {
    const a = document.createElement('a');
    a.className = 'nav-link' + (window.location.hash === hash ? ' active' : '');
    a.textContent = label;
    a.href = hash;
    inlineLinks.appendChild(a);
  });
  nav.appendChild(inlineLinks);

  const spacer = document.createElement('div');
  spacer.className = 'nav-spacer';
  nav.appendChild(spacer);

  // Logout: only when the module is behind the platform gate, rendered as an
  // icon rather than a text button.
  if (authEnabled) {
    const btnLogout = document.createElement('button');
    btnLogout.className = 'nav-icon-btn';
    btnLogout.title = 'Log out';
    btnLogout.setAttribute('aria-label', 'Log out');
    btnLogout.innerHTML = ICON_LOGOUT;
    btnLogout.addEventListener('click', async () => {
      await fetch('/api/auth/logout', { method: 'POST' });
      window.location.reload();
    });
    nav.appendChild(btnLogout);
  }

  nav.appendChild(buildMenu());
}

function buildMenu(): HTMLElement {
  const wrap = document.createElement('div');
  wrap.className = 'nav-menu';

  const btn = document.createElement('button');
  btn.id = 'nav-menu-btn';
  btn.className = 'nav-icon-btn';
  btn.title = 'Menu';
  btn.setAttribute('aria-label', 'Menu');
  btn.setAttribute('aria-haspopup', 'true');
  btn.setAttribute('aria-expanded', 'false');
  btn.innerHTML = ICON_MENU;

  const panel = document.createElement('div');
  panel.id = 'nav-menu-panel';
  panel.className = 'nav-menu-panel';
  panel.hidden = true;

  // Section 1: page links, shown inside the menu only on narrow screens.
  const navSection = document.createElement('div');
  navSection.className = 'menu-section menu-nav';
  NAV_LINKS.forEach(({ label, hash }) => {
    const a = document.createElement('a');
    a.className = 'menu-item' + (window.location.hash === hash ? ' active' : '');
    a.textContent = label;
    a.href = hash;
    a.addEventListener('click', closeMenu);
    navSection.appendChild(a);
  });
  panel.appendChild(navSection);

  // Section 2: auto-refresh preferences.
  const arSection = document.createElement('div');
  arSection.className = 'menu-section';
  arSection.innerHTML = '<div class="menu-heading">Auto-refresh</div>';

  const toggleRow = document.createElement('label');
  toggleRow.className = 'menu-row menu-control';
  const toggle = document.createElement('input');
  toggle.type = 'checkbox';
  toggle.checked = arEnabled();
  const toggleText = document.createElement('span');
  toggleText.textContent = 'Enabled';
  toggleRow.append(toggle, toggleText);

  const intervalRow = document.createElement('label');
  intervalRow.className = 'menu-row menu-control';
  const intervalText = document.createElement('span');
  intervalText.textContent = 'Interval';
  const select = document.createElement('select');
  AR_INTERVALS.forEach((s) => {
    const opt = document.createElement('option');
    opt.value = String(s);
    opt.textContent = s + 's';
    if (s === arInterval()) opt.selected = true;
    select.appendChild(opt);
  });
  select.disabled = !toggle.checked;
  intervalRow.append(intervalText, select);

  toggle.addEventListener('change', () => {
    lsSet(AR_ENABLED_KEY, toggle.checked ? '1' : '0');
    select.disabled = !toggle.checked;
    restartAutoRefresh();
  });
  select.addEventListener('change', () => {
    lsSet(AR_INTERVAL_KEY, select.value);
    restartAutoRefresh();
  });

  arSection.append(toggleRow, intervalRow);
  panel.appendChild(arSection);

  // Section 3: server status + the runtime sudo toggle. Health is read-only;
  // the sudo toggle POSTs to /api/capabilities (persisted server-side).
  const stSection = document.createElement('div');
  stSection.className = 'menu-section';
  stSection.innerHTML =
    '<div class="menu-heading">Server</div>' +
    '<div class="menu-row"><span>Status</span><span id="st-status" class="st-value st-muted">…</span></div>';

  const sudoRow = document.createElement('label');
  sudoRow.className = 'menu-row menu-control';
  const sudoToggle = document.createElement('input');
  sudoToggle.id = 'st-sudo-toggle';
  sudoToggle.type = 'checkbox';
  sudoToggle.checked = caps.allow_sudo;
  const sudoText = document.createElement('span');
  sudoText.textContent = 'Allow sudo';
  sudoRow.append(sudoToggle, sudoText);

  sudoToggle.addEventListener('change', async () => {
    const desired = sudoToggle.checked;
    sudoToggle.disabled = true;
    try {
      caps = await api.setCapabilities(desired);
    } catch {
      caps.allow_sudo = !desired; // revert optimistic assumption on failure
    }
    sudoToggle.checked = caps.allow_sudo;
    sudoToggle.disabled = false;
  });

  stSection.appendChild(sudoRow);
  panel.appendChild(stSection);

  btn.addEventListener('click', (e) => {
    e.stopPropagation();
    const open = panel.hidden;
    panel.hidden = !open;
    btn.setAttribute('aria-expanded', String(open));
    if (open) void refreshStatusPanel();
  });

  wrap.append(btn, panel);
  return wrap;
}

// Dismiss the menu on outside click or Escape.
document.addEventListener('click', (e) => {
  const menu = document.getElementById('nav-menu');
  if (menu && !menu.contains(e.target as Node)) closeMenu();
});
document.addEventListener('keydown', (e) => {
  if (e.key === 'Escape') closeMenu();
});

function renderPage(): void {
  const container = document.getElementById('app');
  if (!container) return;

  const hash = window.location.hash || '#groups';
  const [page, param] = hash.slice(1).split('/');

  switch (page) {
    case 'groups':
      renderGroups(container);
      break;
    case 'tasks':
      renderTasks(container, param);
      break;
    case 'executions':
      renderExecutions(container, param);
      break;
    case 'metrics':
      renderMetrics(container, param);
      break;
    case 'output':
      if (param) renderOutput(container, param);
      break;
    default:
      renderGroups(container);
  }
}

function route(): void {
  buildNav();
  renderPage();
}

async function bootstrap(): Promise<void> {
  try {
    caps = await api.capabilities();
  } catch {
    caps = { allow_sudo: false };
  }
  try {
    const mode = await api.authMode();
    authEnabled = Array.isArray(mode.methods) && mode.methods.length > 0;
  } catch {
    authEnabled = false;
  }
  window.addEventListener('hashchange', route);
  route();
  restartAutoRefresh();
}

document.addEventListener('DOMContentLoaded', () => { void bootstrap(); });
