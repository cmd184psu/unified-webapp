import { api, Capabilities } from './api.js';
import { renderGroups } from './groups.js';
import { renderTasks } from './tasks.js';
import { renderExecutions } from './executions.js';
import { renderMetrics } from './metrics.js';
import { renderOutput } from './output.js';

export let caps: Capabilities = { allow_sudo: false };

function buildNav(): void {
  const nav = document.getElementById('nav');
  if (!nav) return;
  nav.textContent = '';

  const brand = document.createElement('a');
  brand.className = 'nav-brand';
  brand.textContent = 'taskmaster';
  brand.href = '#groups';
  nav.appendChild(brand);

  const links: Array<{ label: string; hash: string }> = [
    { label: 'Groups', hash: '#groups' },
    { label: 'Tasks', hash: '#tasks' },
    { label: 'Executions', hash: '#executions' },
    { label: 'Metrics', hash: '#metrics' },
  ];
  links.forEach(({ label, hash }) => {
    const a = document.createElement('a');
    a.className = 'nav-link' + (window.location.hash === hash ? ' active' : '');
    a.textContent = label;
    a.href = hash;
    nav.appendChild(a);
  });

  const spacer = document.createElement('div');
  spacer.className = 'nav-spacer';
  nav.appendChild(spacer);

  const btnLogout = document.createElement('button');
  btnLogout.className = 'nav-link';
  btnLogout.textContent = 'Logout';
  btnLogout.addEventListener('click', async () => {
    await fetch('/api/auth/logout', { method: 'POST' });
    window.location.reload();
  });
  nav.appendChild(btnLogout);
}

function route(): void {
  const container = document.getElementById('app');
  if (!container) return;

  buildNav();

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

async function bootstrap(): Promise<void> {
  try {
    caps = await api.capabilities();
  } catch {
    caps = { allow_sudo: false };
  }
  window.addEventListener('hashchange', route);
  route();
}

document.addEventListener('DOMContentLoaded', () => { void bootstrap(); });
