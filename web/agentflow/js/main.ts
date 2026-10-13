// main.ts — app shell for AgentFlow with tabbed navigation:
// FRD Wizard → Plan Gen/Review → Task Compiler → Queue Dashboard → Settings

import { api, type QueueTask, type ProgressCounts } from './api.js';
import { ThemeManager, HamburgerMenu, confirmDialog, alertDialog } from '@shared';
import { initFRDWizard } from './frd-wizard.js';
import { initPlanGen } from './plan-gen.js';
import { initTaskCompiler } from './task-compiler.js';
import { initQueueDashboard } from './queue-dashboard.js';
import { initSettings } from './settings.js';

const themes = new ThemeManager({ module: 'agentflow', default: 'dark' });
themes.apply();

type TabId = 'frd' | 'plan' | 'compile' | 'queue' | 'settings';

interface TabConfig {
  id: TabId;
  label: string;
  icon?: string;
  init: () => void | Promise<void>;
  requiresAuth?: boolean;
}

const TABS: TabConfig[] = [
  { id: 'frd', label: 'FRD Wizard', init: initFRDWizard },
  { id: 'plan', label: 'Plan Gen', init: initPlanGen },
  { id: 'compile', label: 'Compile', init: initTaskCompiler },
  { id: 'queue', label: 'Queue', init: initQueueDashboard },
  { id: 'settings', label: 'Settings', init: initSettings },
];

let activeTab: TabId = 'frd';
let selectedTaskId: string | null = null;
let tasks: QueueTask[] = [];

// ─── Status chip mapping ───────────────────────────────────────────────────

const STATUS_CHIP_CLASSES: Record<string, string> = {
  ready: 'status-ready',
  blocked: 'status-blocked',
  'in-progress': 'status-in-progress',
  review: 'status-review',
  repair: 'status-repair',
  passed: 'status-passed',
  failed: 'status-failed',
};

function chipClass(status: string): string {
  return STATUS_CHIP_CLASSES[status] || 'status-ready';
}

// ─── Hamburger menu ────────────────────────────────────────────────────────

function buildHamburger(): HamburgerMenu {
  const items = [
    { section: 'AgentFlow' },
  ];

  return new HamburgerMenu({
    title: 'AgentFlow',
    items,
    themePicker: true,
    themes,
    side: 'right',
  });
}

function initHamburger(): void {
  const trigger = document.getElementById('hamburger-trigger');
  if (!trigger) return;
  const hamburger = buildHamburger();
  trigger.appendChild(hamburger.trigger);
}

// ─── Logout button ─────────────────────────────────────────────────────────

function initLogout(): void {
  const btn = document.getElementById('logout-btn');
  if (!btn) return;
  btn.removeAttribute('hidden');
  btn.addEventListener('click', () => {
    window.location.href = '/logout';
  });
}

// ─── Tab navigation ────────────────────────────────────────────────────────

async function hasFRD(): Promise<boolean> {
  try {
    const r = await fetch('/frd');
    return r.ok;
  } catch {
    return false;
  }
}

function renderTabs(): void {
  const host = document.getElementById('tab-bar');
  if (!host) return;

  host.innerHTML = TABS.map(t => `
    <button type="button" class="tab-btn ${t.id === activeTab ? 'active' : ''}" data-tab="${t.id}" data-tab-id="${t.id}">
      ${t.label}
    </button>
  `).join('');

  // Update disabled state for plan/compile tabs
  hasFRD().then(has => {
    document.querySelectorAll('[data-tab-id="plan"], [data-tab-id="compile"]').forEach(el => {
      const btn = el as HTMLButtonElement;
      btn.disabled = !has;
      if (!has) btn.style.opacity = '0.4';
    });
  });

  host.querySelectorAll('.tab-btn').forEach(btn => {
    btn.addEventListener('click', async () => {
      const tabId = btn.getAttribute('data-tab') as TabId;
      if ((tabId === 'plan' || tabId === 'compile') && !(await hasFRD())) {
        await alertDialog('Please complete the FRD wizard first before accessing this tab.');
        return;
      }
      switchTab(tabId);
    });
  });
}

async function switchTab(tabId: TabId): Promise<void> {
  const panel0 = document.getElementById(`panel-${tabId}`);
  if (tabId === activeTab && panel0 && !panel0.hasAttribute('hidden')) return;

  // Find tab config
  const tabConfig = TABS.find(t => t.id === tabId);
  if (!tabConfig) return;

  // Update UI
  activeTab = tabId;
  renderTabs();

  // Hide all tab panels
  document.querySelectorAll('.tab-panel').forEach(p => p.setAttribute('hidden', ''));

  // Show selected panel
  const panel = document.getElementById(`panel-${tabId}`);
  if (panel) panel.removeAttribute('hidden');

  // Initialize tab if first visit
  try {
    await tabConfig.init();
  } catch (err) {
    console.error(`Tab ${tabId} init failed:`, err);
    await alertDialog(`Failed to load ${tabConfig.label}: ${(err as Error).message}`);
  }
}

// ─── Queue Dashboard (Queue tab) ───────────────────────────────────────────

async function loadConsole(): Promise<void> {
  try {
    const [queueData, progressData] = await Promise.all([
      api.queue(),
      api.progress(),
    ]);
    tasks = queueData.tasks;
    renderProgress(progressData);
    renderTaskList();
  } catch {
    /* ignore transient network errors — poll will retry */
  }
}

function renderProgress(counts: ProgressCounts): void {
  const fill = document.getElementById('progress-fill') as HTMLElement | null;
  const pctEl = document.getElementById('progress-pct') as HTMLElement | null;
  if (!fill || !pctEl) return;

  const pct = counts.total > 0 ? Math.round((counts.passed / counts.total) * 100) : 0;
  fill.style.width = pct + '%';
  pctEl.textContent = pct + '% — ' +
    'passed ' + counts.passed + '/' + counts.total +
    ' · ready ' + counts.ready + ' · blocked ' + counts.blocked +
    ' · review ' + counts.review + ' · repair ' + counts.repair +
    ' · failed ' + counts.failed;
}

function renderTaskList(): void {
  const host = document.getElementById('task-list');
  if (!host) return;
  host.innerHTML = '';

  if (tasks.length === 0) {
    host.textContent = 'No pending tasks.';
    return;
  }

  const ul = document.createElement('ul');
  ul.className = 'task-list';

  tasks.forEach(task => {
    const li = document.createElement('li');
    const selected = task.id === selectedTaskId;
    li.className = `task-item ${chipClass(task.status)}${selected ? ' selected' : ''}`;
    li.innerHTML =
      `<div class="task-item-title">${escHtml(task.title)}</div>` +
      `<div class="task-item-meta">` +
        `<span class="status-chip ${chipClass(task.status)}">${escHtml(task.status)}</span>` +
        `<span class="task-attempts">attempt${task.attempts !== 1 ? 's' : ''}: ${task.attempts}</span>` +
      `</div>`;
    li.addEventListener('click', () => {
      selectedTaskId = task.id;
      renderTaskList();
      renderDetail(task);
    });
    ul.appendChild(li);
  });

  host.replaceChildren(ul);
}

function renderDetail(task: QueueTask): void {
  const pane = document.getElementById('detail-pane') as HTMLElement | null;
  if (!pane) return;

  const titleEl = document.getElementById('detail-title') as HTMLElement | null;
  const statusEl = document.getElementById('detail-status') as HTMLElement | null;
  const statusRepeatedEl = document.getElementById('detail-status-repeated') as HTMLElement | null;
  const attemptsEl = document.getElementById('detail-attempts') as HTMLElement | null;
  const promptEl = document.getElementById('detail-prompt') as HTMLElement | null;
  const acceptanceEl = document.getElementById('detail-acceptance') as HTMLElement | null;
  const verifyEl = document.getElementById('detail-verify') as HTMLElement | null;
  const verdictChip = document.getElementById('detail-verdict') as HTMLElement | null;
  const summaryP = document.getElementById('detail-review-summary') as HTMLElement | null;

  if (titleEl) titleEl.textContent = task.title;
  if (statusEl) {
    statusEl.className = `status-chip ${chipClass(task.status)}`;
    statusEl.textContent = task.status;
  }
  if (statusRepeatedEl) {
    statusRepeatedEl.className = `status-chip ${chipClass(task.status)}`;
    statusRepeatedEl.textContent = task.status;
  }
  if (attemptsEl) attemptsEl.textContent = 'Attempt' + (task.attempts !== 1 ? 's' : '') + ': ' + task.attempts;

  // Prompt
  if (promptEl) {
    if (task.prompt) {
      promptEl.textContent = task.prompt;
      (promptEl.closest('.detail-section') as HTMLElement | null)?.style.removeProperty('display');
    } else {
      promptEl.textContent = '';
      (promptEl.closest('.detail-section') as HTMLElement | null)!.style.display = 'none';
    }
  }

  // Acceptance criteria
  if (acceptanceEl) {
    const acceptance = Array.isArray(task.acceptance)
      ? task.acceptance.join('\n')
      : (task.acceptance ?? '');
    if (acceptance) {
      acceptanceEl.textContent = acceptance;
      (acceptanceEl.closest('.detail-section') as HTMLElement | null)?.style.removeProperty('display');
    } else {
      (acceptanceEl.closest('.detail-section') as HTMLElement | null)!.style.display = 'none';
    }
  }

  // Verify command
  if (verifyEl) {
    if (task.verify) {
      verifyEl.textContent = task.verify;
      (verifyEl.closest('.detail-section') as HTMLElement | null)?.style.removeProperty('display');
    } else {
      (verifyEl.closest('.detail-section') as HTMLElement | null)!.style.display = 'none';
    }
  }

  // Last review
  if (verdictChip && summaryP) {
    const lastReview = task.last_review ?? null;
    if (lastReview) {
      verdictChip.className = `status-chip last-review-chip ${chipClass(lastReview.verdict)}`;
      verdictChip.textContent = lastReview.verdict;
      summaryP.textContent = lastReview.summary;
    } else {
      verdictChip.className = 'status-chip last-review-chip';
      verdictChip.style.background = 'var(--color-text-faint)';
      verdictChip.textContent = 'no review yet';
      summaryP.textContent = '';
    }
  }

  wireActions(task.id);

  pane.removeAttribute('hidden');
}

function wireActions(taskId: string): void {
  const wire = (id: string, url: string) => {
    const btn = document.getElementById(id) as HTMLButtonElement | null;
    if (btn) btn.onclick = () => { void postAction(url); };
  };
  wire('action-approve', `/tasks/${taskId}/approve`);
  wire('action-repair', `/tasks/${taskId}/repair`);
  wire('action-fail', `/tasks/${taskId}/fail`);
  wire('action-run-next', '/run-next');
}

// ─── Console actions ────────────────────────────────────────────────────────

async function postAction(url: string): Promise<void> {
  try {
    await fetch(url, { method: 'POST' });
  } catch {
    /* ignore transient network errors — poll will retry */
  }
  await loadConsole();
  if (selectedTaskId) {
    const task = tasks.find(t => t.id === selectedTaskId);
    if (task) renderDetail(task);
  }
}

// ─── Helpers ────────────────────────────────────────────────────────────────

function escHtml(s: string): string {
  const d = document.createElement('div');
  d.textContent = s;
  return d.innerHTML;
}

// ─── Close detail pane ────────────────────────────────────────────────────

function initDetailClose(): void {
  const btn = document.getElementById('detail-close');
  if (!btn) return;
  btn.addEventListener('click', () => {
    selectedTaskId = null;
    const pane = document.getElementById('detail-pane');
    if (pane) pane.setAttribute('hidden', '');
    renderTaskList();
  });
}

// ─── SSE Streaming for live task output ────────────────────────────────────

let currentEventSource: EventSource | null = null;

function connectTaskStream(taskId: string): void {
  if (currentEventSource) {
    currentEventSource.close();
  }

  const panel = document.getElementById('stream-panel');
  if (!panel) return;

  panel.innerHTML = '<div class="stream-status">Connecting...</div>';
  panel.removeAttribute('hidden');

  currentEventSource = new EventSource(`/tasks/${encodeURIComponent(taskId)}/stream`);

  currentEventSource.onmessage = (event) => {
    try {
      const data = JSON.parse(event.data);
      handleStreamMessage(data);
    } catch {
      // ignore parse errors
    }
  };

  currentEventSource.onerror = () => {
    const status = panel.querySelector('.stream-status');
    if (status) status.textContent = 'Disconnected — retrying...';
  };
}

function handleStreamMessage(data: any): void {
  const panel = document.getElementById('stream-panel');
  if (!panel) return;

  if (data.type === 'state') {
    // Initial state
    renderStreamPanel(data);
  } else if (data.type === 'output') {
    appendStreamOutput(data.lines);
  } else if (data.type === 'update') {
    // Task status update
    const task = data.task;
    if (task) {
      const idx = tasks.findIndex(t => t.id === task.id);
      if (idx >= 0) tasks[idx] = task;
      renderTaskList();
      if (selectedTaskId === task.id) renderDetail(task);
    }
  }
}

function renderStreamPanel(data: any): void {
  const panel = document.getElementById('stream-panel');
  if (!panel) return;

  panel.innerHTML = `
    <div class="stream-header">
      <span class="stream-status ${data.status}">${data.status}</span>
      <button type="button" class="icon-btn" id="close-stream" title="Close stream">✕</button>
    </div>
    <div class="stream-output" id="stream-output"></div>
    ${data.verify ? `<div class="stream-verify"><strong>Verify:</strong><pre>${data.verify}</pre></div>` : ''}
    ${data.review ? `<div class="stream-review"><strong>Review:</strong> ${data.review.verdict} — ${data.review.summary}</div>` : ''}
  `;

  panel.querySelector('#close-stream')?.addEventListener('click', () => {
    if (currentEventSource) currentEventSource.close();
    currentEventSource = null;
    panel.setAttribute('hidden', '');
  });

  if (data.output) {
    const output = panel.querySelector('#stream-output') as HTMLElement;
    if (output) output.textContent = data.output.join('\n');
  }
}

function appendStreamOutput(lines: string[]): void {
  const output = document.getElementById('stream-output') as HTMLElement;
  if (!output) return;
  output.textContent += '\n' + lines.join('\n');
  output.scrollTop = output.scrollHeight;
}

// ─── Init & poll ───────────────────────────────────────────────────────────

function bootstrap(): void {
  initHamburger();
  initLogout();
  initDetailClose();
  renderTabs();
  switchTab('frd'); // Start at FRD wizard

  // Poll every 3s for queue/progress updates
  setInterval(() => { void loadConsole(); }, 3000);
}

document.addEventListener('DOMContentLoaded', bootstrap);

// Re-export for queue-dashboard import
export { loadConsole, renderProgress, renderTaskList, renderDetail, wireActions, postAction };