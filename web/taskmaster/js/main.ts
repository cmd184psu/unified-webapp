// main.ts — app shell: top nav (brand + Metrics stub + hamburger) and a
// hash router whose default route is the lane board (Slice A, FRD §8/§8a).
//
// The Live/pause toggle itself sits in the top nav, to the left of the hand
// brake — it's a frequently-reached control, not a settings knob. The
// hamburger holds the rest of the on/off controls as toggles (ui/toggle.ts,
// never a checkbox): the fallback poll interval (wired to the shared
// LiveController), allow-sudo (wired to /api/capabilities), a read-only
// server status line, and an auth-aware logout icon. There is deliberately
// no manual "Refresh" button anywhere — the board updates itself via the
// live controller.

import { api, BrakeState, Capabilities } from './api.js';
import { LiveController } from './ui/live.js';
import { createToggleHandle } from './ui/toggle.js';
import { confirmDialog } from './ui/modal.js';
import { mountBoard } from './board.js';
import { mountMetrics } from './metrics.js';
import { mountTaskView } from './taskview.js';
import { FRONTEND_BUILD_TIME } from './buildinfo.js';

let caps: Capabilities = { allow_sudo: false };
let authEnabled = false;
let brake: BrakeState = { engaged: false };
const live = new LiveController();

const LIVE_INTERVALS_SEC = [5, 10, 30, 60];

// --- Icons (inline SVG, trusted static markup) ---
const ICON_MENU =
  '<svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><line x1="3" y1="6" x2="21" y2="6"/><line x1="3" y1="12" x2="21" y2="12"/><line x1="3" y1="18" x2="21" y2="18"/></svg>';
const ICON_LOGOUT =
  '<svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4"/><polyline points="16 17 21 12 16 7"/><line x1="21" y1="12" x2="9" y2="12"/></svg>';
const ICON_BRAKE =
  '<svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="9"/><line x1="7" y1="7" x2="17" y2="17"/></svg>';

const NAV_LINKS: Array<{ label: string; hash: string }> = [{ label: 'Metrics', hash: '#metrics' }];

function currentPage(): string {
  const hash = window.location.hash || '#board';
  return hash.slice(1).split('/')[0] || 'board';
}

/** For the `#task/<name>` route: the task name, URL-decoded, or null. */
function currentTaskName(): string | null {
  const hash = window.location.hash || '';
  const parts = hash.slice(1).split('/');
  if (parts[0] === 'task' && parts[1]) {
    try {
      return decodeURIComponent(parts[1]);
    } catch {
      return parts[1];
    }
  }
  return null;
}

function closeMenu(): void {
  const panel = document.getElementById('nav-menu-panel');
  const btn = document.getElementById('nav-menu-btn');
  if (panel) panel.hidden = true;
  if (btn) btn.setAttribute('aria-expanded', 'false');
}

async function refreshStatusLine(): Promise<void> {
  const statusEl = document.getElementById('st-status');
  const backendBuildEl = document.getElementById('st-backend-build');
  if (!statusEl) return;
  statusEl.textContent = 'checking…';
  statusEl.className = 'st-value st-muted';
  try {
    const h = await api.health();
    statusEl.textContent = h.status === 'ok' ? 'healthy' : h.status;
    statusEl.className = 'st-value st-ok';
    if (backendBuildEl) backendBuildEl.textContent = h.build ?? 'dev';
  } catch {
    statusEl.textContent = 'unreachable';
    statusEl.className = 'st-value st-err';
    if (backendBuildEl) backendBuildEl.textContent = '—';
  }
}

function buildNav(): void {
  const nav = document.getElementById('nav');
  if (!nav) return;
  nav.textContent = '';

  const brand = document.createElement('a');
  brand.className = 'nav-brand';
  brand.textContent = 'taskmaster';
  brand.href = '#board';
  nav.appendChild(brand);

  const inlineLinks = document.createElement('div');
  inlineLinks.className = 'nav-links';
  const page = currentPage();
  NAV_LINKS.forEach(({ label, hash }) => {
    const a = document.createElement('a');
    a.className = 'nav-link' + (hash === '#' + page ? ' active' : '');
    a.textContent = label;
    a.href = hash;
    inlineLinks.appendChild(a);
  });
  nav.appendChild(inlineLinks);

  const spacer = document.createElement('div');
  spacer.className = 'nav-spacer';
  nav.appendChild(spacer);

  nav.appendChild(buildLiveControl());
  nav.appendChild(buildBrakeControl());

  if (authEnabled) {
    const btnLogout = document.createElement('button');
    btnLogout.className = 'nav-icon-btn';
    btnLogout.title = 'Log out';
    btnLogout.setAttribute('aria-label', 'Log out');
    btnLogout.innerHTML = ICON_LOGOUT;
    btnLogout.addEventListener('click', async () => {
      await api.logout();
      window.location.reload();
    });
    nav.appendChild(btnLogout);
  }

  nav.appendChild(buildMenu());
}

// ─── Live/pause (top nav, left of hand brake) ─────────────────────────────

function liveToggleTitle(enabled: boolean): string {
  return enabled ? 'Live updates on — click to pause' : 'Live updates paused — click to resume';
}

function buildLiveControl(): HTMLElement {
  const wrap = document.createElement('div');
  wrap.className = 'nav-live';
  const label = document.createElement('span');
  label.className = 'nav-live-label';
  label.textContent = 'Live';
  const toggle = createToggleHandle({
    checked: live.isEnabled(),
    onChange: (v) => {
      live.setEnabled(v);
      toggle.el.title = liveToggleTitle(v);
    },
  });
  toggle.el.title = liveToggleTitle(live.isEnabled());
  wrap.append(label, toggle.el);
  return wrap;
}

function buildMenu(): HTMLElement {
  const wrap = document.createElement('div');
  wrap.className = 'nav-menu';
  wrap.id = 'nav-menu';

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

  // Section 1: page links, shown in the menu only on narrow screens.
  const navSection = document.createElement('div');
  navSection.className = 'menu-section menu-nav';
  const page = currentPage();
  NAV_LINKS.forEach(({ label, hash }) => {
    const a = document.createElement('a');
    a.className = 'menu-item' + (hash === '#' + page ? ' active' : '');
    a.textContent = label;
    a.href = hash;
    a.addEventListener('click', closeMenu);
    navSection.appendChild(a);
  });
  panel.appendChild(navSection);

  // Section 2: fallback poll interval (the live/pause toggle itself now
  // lives in the top nav, left of the hand brake).
  const liveSection = document.createElement('div');
  liveSection.className = 'menu-section';
  liveSection.innerHTML = '<div class="menu-heading">Live updates</div>';

  const intervalRow = document.createElement('div');
  intervalRow.className = 'menu-row';
  const intervalLabel = document.createElement('span');
  intervalLabel.textContent = 'Fallback poll interval';
  const intervalSelect = document.createElement('select');
  LIVE_INTERVALS_SEC.forEach((s) => {
    const opt = document.createElement('option');
    opt.value = String(s * 1000);
    opt.textContent = s + 's';
    if (s * 1000 === live.getInterval()) opt.selected = true;
    intervalSelect.appendChild(opt);
  });
  intervalSelect.addEventListener('change', () => {
    live.setInterval(parseInt(intervalSelect.value, 10));
  });
  intervalRow.append(intervalLabel, intervalSelect);

  liveSection.append(intervalRow);
  panel.appendChild(liveSection);

  // Section 3: server + capabilities.
  const stSection = document.createElement('div');
  stSection.className = 'menu-section';
  stSection.innerHTML =
    '<div class="menu-heading">Server</div>' +
    '<div class="menu-row"><span>Status</span><span id="st-status" class="st-value st-muted">…</span></div>' +
    '<div class="menu-row"><span>Backend build</span><span id="st-backend-build" class="st-value st-muted">…</span></div>' +
    '<div class="menu-row"><span>Frontend build</span><span class="st-value">' + FRONTEND_BUILD_TIME + '</span></div>';

  const sudoRow = document.createElement('div');
  sudoRow.className = 'menu-row';
  const sudoLabel = document.createElement('span');
  sudoLabel.textContent = 'Allow sudo';
  const sudoToggle = createToggleHandle({
    checked: caps.allow_sudo,
    onChange: (desired) => {
      sudoToggle.setDisabled(true);
      void api
        .setCapabilities(desired)
        .then((updated) => {
          caps = updated;
        })
        .catch(() => {
          caps.allow_sudo = !desired; // revert optimistic assumption on failure
        })
        .finally(() => {
          sudoToggle.setChecked(caps.allow_sudo);
          sudoToggle.setDisabled(false);
        });
    },
  });
  sudoRow.append(sudoLabel, sudoToggle.el);
  stSection.appendChild(sudoRow);
  panel.appendChild(stSection);

  btn.addEventListener('click', (e) => {
    e.stopPropagation();
    const open = panel.hidden;
    panel.hidden = !open;
    btn.setAttribute('aria-expanded', String(open));
    if (open) void refreshStatusLine();
  });

  wrap.append(btn, panel);
  return wrap;
}

// ─── Hand brake (FRD §5): prominent, always-visible, never buried ─────────

function buildBrakeControl(): HTMLElement {
  const btn = document.createElement('button');
  btn.id = 'brake-btn';
  btn.type = 'button';
  btn.className = 'brake-btn';
  btn.innerHTML = ICON_BRAKE + '<span class="brake-btn-label"></span>';
  btn.addEventListener('click', () => void toggleBrake());
  applyBrakeUI(btn);
  return btn;
}

function applyBrakeUI(btn: HTMLElement): void {
  btn.classList.toggle('brake-engaged', brake.engaged);
  btn.title = brake.engaged ? 'Hand brake engaged — click to release' : 'Hand brake — click to stop everything';
  btn.setAttribute('aria-pressed', String(brake.engaged));
  const label = btn.querySelector('.brake-btn-label');
  if (label) label.textContent = brake.engaged ? 'RELEASE BRAKE' : 'HAND BRAKE';
}

function refreshBrakeUI(): void {
  const btn = document.getElementById('brake-btn');
  if (btn) applyBrakeUI(btn);
  renderBrakeBanner();
}

function renderBrakeBanner(): void {
  let banner = document.getElementById('brake-banner');
  if (!brake.engaged) {
    banner?.remove();
    return;
  }
  if (!banner) {
    banner = document.createElement('div');
    banner.id = 'brake-banner';
    banner.className = 'brake-banner';
    banner.textContent = 'ALL PAUSED — hand brake engaged. No tasks will launch until it is released.';
    const nav = document.getElementById('nav');
    nav?.insertAdjacentElement('afterend', banner);
  }
}

async function toggleBrake(): Promise<void> {
  if (brake.engaged) {
    try {
      brake = await api.releaseBrake();
    } catch {
      return;
    }
    refreshBrakeUI();
    return;
  }
  const ok = await confirmDialog(
    'Engage the hand brake? This pauses every lane and force-kills every running task (sudo children may survive). Everything stays paused until you release it.',
    { title: 'Engage hand brake', confirmLabel: 'Engage' }
  );
  if (!ok) return;
  try {
    brake = await api.engageBrake();
  } catch {
    return;
  }
  refreshBrakeUI();
}

document.addEventListener('click', (e) => {
  const menu = document.getElementById('nav-menu');
  if (menu && !menu.contains(e.target as Node)) closeMenu();
});
document.addEventListener('keydown', (e) => {
  if (e.key === 'Escape') closeMenu();
});

let unmountCurrentPage: (() => void) | null = null;

function renderPage(): void {
  const container = document.getElementById('app');
  if (!container) return;

  if (unmountCurrentPage) {
    unmountCurrentPage();
    unmountCurrentPage = null;
  }

  const page = currentPage();
  switch (page) {
    case 'metrics':
      unmountCurrentPage = mountMetrics(container, live);
      break;
    case 'task': {
      const taskName = currentTaskName();
      if (taskName) {
        unmountCurrentPage = mountTaskView(container, live, caps, taskName);
        break;
      }
      // Malformed #task hash with no name — fall through to the board.
      unmountCurrentPage = mountBoard(container, live, caps);
      break;
    }
    case 'board':
    default:
      unmountCurrentPage = mountBoard(container, live, caps);
  }
}

function route(): void {
  buildNav();
  renderBrakeBanner();
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
  try {
    brake = await api.getBrake();
  } catch {
    brake = { engaged: false };
  }
  live.onEvent((ev) => {
    if (ev.type === 'brake' && typeof ev.engaged === 'boolean') {
      brake = { engaged: ev.engaged };
      refreshBrakeUI();
    }
  });
  window.addEventListener('hashchange', route);
  route();
}

document.addEventListener('DOMContentLoaded', () => {
  void bootstrap();
});
