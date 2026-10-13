// AgentFlow module stub
import { ThemeManager, HamburgerMenu } from '/shared/dist/shared.mjs';

const themes = new ThemeManager({ module: 'agentflow', default: 'dark' });
themes.apply();

// ── Hamburger menu ───────────────────────────────────────────────────────

const hamburger = new HamburgerMenu({
  title: 'AgentFlow',
  items: [],
  themePicker: true,
  themes,
  side: 'right',
});

(function initHamburger() {
  const trigger = document.getElementById('hamburger-trigger');
  if (trigger) trigger.appendChild(hamburger.trigger);
})();

// ── Logout button ────────────────────────────────────────────────────────

(function initLogout() {
  const btn = document.getElementById('logout-btn');
  if (btn) {
    btn.removeAttribute('hidden');
    btn.addEventListener('click', () => {
      window.location.href = '/logout';
     });
    }
})();

// ── Data types ───────────────────────────────────────────────────────────

/** @typedef {{ tasks: AgentQueueTask[] }} AgentQueue */
/** @typedef {{ total: number, passed: number, ready: number, blocked: number, review: number, repair: number, failed: number }} ProgressCounts */
/** @typedef {{ id: string, title: string, status: string, attempts: number, prompt: string, acceptance: string[] | string, verify: string, last_review: { verdict: string, summary: string } | null }} AgentQueueTask */

let state = { tasks: [], selectedId: null };

// ── Console rendering ────────────────────────────────────────────────────

/** @type {Record<string, string>} */
const STATUS_CHIP_CLASSES = {
    'ready': 'status-ready',
    'blocked': 'status-blocked',
    'in-progress': 'status-in-progress',
    'review': 'status-review',
    'repair': 'status-repair',
    'passed': 'status-passed',
    'failed': 'status-failed',
};

function chipClass(status) {
  return STATUS_CHIP_CLASSES[status] || 'status-ready';
}

async function loadConsole() {
  try {
    const [queueRes, progressRes] = await Promise.all([
      fetch('/queue'),
      fetch('/progress'),
    ]);
    if (queueRes.ok) {
      const queueData = /** @type {AgentQueue} */ (await queueRes.json());
      state.tasks = queueData.tasks;
    }
    if (progressRes.ok) {
      const counts = /** @type {ProgressCounts} */ (await progressRes.json());
      renderProgress(counts);
    }
    renderTaskList();
  } catch {
    /* ignore transient network errors — poll will retry */
  }
}

function renderProgress(counts) {
  const fill = document.getElementById('progress-fill');
  const pctEl = document.getElementById('progress-pct');
  if (!fill || !pctEl) return;

  const pct = counts.total > 0 ? Math.round((counts.passed / counts.total) * 100) : 0;
  fill.style.width = pct + '%';
  pctEl.textContent = pct + '% — ' +
    'passed ' + counts.passed + '/' + counts.total +
    ' · ready ' + counts.ready + ' · blocked ' + counts.blocked +
    ' · review ' + counts.review + ' · repair ' + counts.repair +
    ' · failed ' + counts.failed;
}

function renderTaskList() {
  const host = document.getElementById('task-list');
  if (!host) return;
  host.innerHTML = '';

  if (state.tasks.length === 0) {
    host.textContent = 'No pending tasks.';
    return;
  }

  const ul = document.createElement('ul');
  ul.className = 'task-list';

  state.tasks.forEach(task => {
    const li = document.createElement('li');
    const selected = task.id === state.selectedId;
    li.className = `task-item ${chipClass(task.status)}${selected ? ' selected' : ''}`;
    li.innerHTML =
        `<div class="task-item-title">${escHtml(task.title)}</div>` +
        `<div class="task-item-meta">` +
          `<span class="status-chip ${chipClass(task.status)}">${escHtml(task.status)}</span>` +
          `<span class="task-attempts">attempt${task.attempts !== 1 ? 's' : ''}: ${task.attempts}</span>` +
        `</div>`;
    li.addEventListener('click', () => {
      state.selectedId = task.id;
      renderTaskList();
      renderDetail(task);
    });
    ul.appendChild(li);
  });

  host.replaceChildren(ul);
}

/** @typedef {{ verdict: string, summary: string }} TaskReview */

function renderDetail(task) {
  const pane = document.getElementById('detail-pane');
  if (!pane) return;

  document.getElementById('detail-title').textContent = task.title;
  document.getElementById('detail-status').className = `status-chip ${chipClass(task.status)}`;
  document.getElementById('detail-status').textContent = task.status;
  document.getElementById('detail-status-repeated').className = `status-chip ${chipClass(task.status)}`;
  document.getElementById('detail-status-repeated').textContent = task.status;
  document.getElementById('detail-attempts').textContent = 'Attempt' + (task.attempts !== 1 ? 's' : '') + ': ' + task.attempts;

  // Prompt
  const promptEl = document.getElementById('detail-prompt');
  if (promptEl) {
    if (task.prompt) {
      promptEl.textContent = task.prompt;
    } else {
      promptEl.textContent = task.prompt ?? '';
      promptEl.closest('.detail-section').style.display = 'none';
    }
  }

  // Acceptance criteria (backend sends an array; older queues may send a string)
  const acceptanceEl = document.getElementById('detail-acceptance');
  if (acceptanceEl) {
    const acceptance = Array.isArray(task.acceptance)
      ? task.acceptance.join('\n')
      : (task.acceptance ?? '');
    if (acceptance) {
      acceptanceEl.textContent = acceptance;
    } else {
      acceptanceEl.closest('.detail-section').style.display = 'none';
    }
  }

  // Verify command
  const verifyEl = document.getElementById('detail-verify');
  if (verifyEl) {
    if (task.verify) {
      verifyEl.textContent = task.verify;
    } else {
      verifyEl.closest('.detail-section').style.display = 'none';
    }
  }

  // Last review — verdict + summary
  const verdictChip = document.getElementById('detail-verdict');
  const summaryP = document.getElementById('detail-review-summary');
  const lastReview = task.last_review ?? null;
  if (verdictChip && summaryP) {
    if (lastReview) {
      verdictChip.className = `status-chip last-review-chip ${chipClass(lastReview.verdict)}`;
      verdictChip.textContent = lastReview.verdict;
      summaryP.textContent = lastReview.summary;
      // Show the section even if empty — we want to show "no review yet" not hide it
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

// ── Console actions ──────────────────────────────────────────────────────

async function postAction(url) {
  try {
    await fetch(url, { method: 'POST' });
  } catch {
    /* ignore transient network errors — poll will retry */
  }
  await loadConsole();
  if (state.selectedId) {
    const task = state.tasks.find(t => t.id === state.selectedId);
    if (task) renderDetail(task);
  }
}

function wireActions(taskId) {
  const wire = (id, url) => {
    const btn = document.getElementById(id);
    if (btn) btn.onclick = () => { void postAction(url); };
  };
  wire('action-approve', `/tasks/${taskId}/approve`);
  wire('action-repair', `/tasks/${taskId}/repair`);
  wire('action-fail', `/tasks/${taskId}/fail`);
  wire('action-run-next', '/run-next');
}

// ── Helpers ──────────────────────────────────────────────────────────────

function escHtml(s) {
  const d = document.createElement('div');
  d.textContent = String(s ?? '');
  return d.innerHTML;
}

// ── Close detail pane ───────────────────────────────────────────────────

(function initDetailClose() {
  const btn = document.getElementById('detail-close');
  if (btn) {
    btn.addEventListener('click', () => {
      state.selectedId = null;
      document.getElementById('detail-pane').setAttribute('hidden', '');
      renderTaskList();
    });
  }
})();

// ── Init & poll ──────────────────────────────────────────────────────────

loadConsole();

// Poll every 3 s for fresh queue / progress data.
setInterval(() => { void loadConsole(); }, 3000);
