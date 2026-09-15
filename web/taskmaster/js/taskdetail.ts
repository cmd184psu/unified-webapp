// taskdetail.ts — the per-task drill-in panel (plan Phase F4, split-view
// rework).
//
// Used to be a modal opened over the full board; it is now mounted directly
// into the right-hand pane of the split task view (see taskview.ts), with a
// "Back to board" control instead of a close button. Shows: run history
// (api.listExecutions(name), terminal-status runs only) and that task's own
// metrics (api.getMetrics(undefined, name)).
//
// The currently-running instance is NOT listed here — it lives only in the
// lane board's card (board.ts), which is where its PID, pause/resume,
// cancel, and live-output controls live. Showing the same running process
// in two panels meant they could disagree (e.g. pausing on one side didn't
// update the other); one source of truth removes that by construction.
// History rows open their own past output in a shared modal
// (outputmodal.ts) instead of an inline pane, since most task output is
// more than a couple of lines.
//
// Refresh is event-driven, not timer-driven: there is no polling interval
// here. Instead this subscribes to the shared LiveController's board events
// (the same feed the board uses) and re-fetches history/metrics only when
// something changed, patching both in place via patchList/targeted DOM
// updates — never a full innerHTML rebuild.

import { api, MetricSummary, Task, TaskExecution } from './api.js';
import { LiveController } from './ui/live.js';
import { patchList } from './ui/live.js';
import { openOutputModal } from './outputmodal.js';
import { fmtDate, fmtMs, renderStatusBadge } from './status.js';

/**
 * Mounts the task detail panel into `container` (the right pane of the
 * split task view). `onBack` is invoked when the user asks to return to the
 * full board. Returns a cleanup function to call on navigation away.
 */
export function mountTaskDetail(container: HTMLElement, live: LiveController, task: Task, onBack: () => void): () => void {
  container.textContent = '';
  container.className = 'task-detail';

  const header = document.createElement('div');
  header.className = 'task-detail-header';
  const backBtn = document.createElement('button');
  backBtn.type = 'button';
  backBtn.className = 'btn btn-secondary btn-sm task-detail-back';
  backBtn.textContent = '← Back to board';
  backBtn.addEventListener('click', onBack);
  const title = document.createElement('h2');
  title.className = 'task-detail-title';
  title.textContent = task.name;
  header.append(backBtn, title);
  container.appendChild(header);

  const meta = document.createElement('div');
  meta.className = 'task-detail-meta';
  const cmdLine = document.createElement('code');
  cmdLine.className = 'task-detail-command';
  cmdLine.textContent = task.command;
  meta.appendChild(cmdLine);
  const laneLine = document.createElement('div');
  laneLine.className = 'task-detail-sub';
  laneLine.textContent =
    'lane: ' + task.lane_name + (task.repeat ? ' · repeats, cooldown ' + task.cooldown_seconds + 's' : ' · one-shot') + (task.sudo ? ' · sudo' : '');
  meta.appendChild(laneLine);
  container.appendChild(meta);

  // --- Metrics summary ---
  const metricsWrap = document.createElement('div');
  metricsWrap.className = 'task-detail-metrics';
  metricsWrap.textContent = 'Loading metrics…';
  container.appendChild(metricsWrap);

  // --- History (past runs only — the running instance lives on the board) ---
  const historyHeader = document.createElement('div');
  historyHeader.className = 'task-detail-section-title';
  historyHeader.textContent = 'History';
  const historyList = document.createElement('div');
  historyList.className = 'task-detail-history';
  container.append(historyHeader, historyList);

  let destroyed = false;

  function isTerminal(exec: TaskExecution): boolean {
    return exec.status === 'success' || exec.status === 'failed' || exec.status === 'canceled';
  }

  function byIdDescending(a: TaskExecution, b: TaskExecution): number {
    return b.id - a.id;
  }

  function keyById(exec: TaskExecution): number {
    return exec.id;
  }

  async function loadHistory(): Promise<void> {
    let execs: TaskExecution[] = [];
    try {
      const all = await api.listExecutions(task.name, 50);
      execs = all.filter(isTerminal);
    } catch {
      return;
    }
    if (destroyed) return;
    execs.sort(byIdDescending);

    if (execs.length === 0) {
      historyList.textContent = '';
      const empty = document.createElement('div');
      empty.className = 'lane-empty-note';
      empty.textContent = 'no runs yet';
      historyList.appendChild(empty);
      return;
    }

    historyList.querySelector('.lane-empty-note')?.remove();
    patchList(historyList, execs, {
      key: keyById,
      create: createHistoryRow,
      update: updateHistoryRow,
    });
  }

  function createHistoryRow(exec: TaskExecution): HTMLElement {
    const row = document.createElement('div');
    row.className = 'history-row';

    const badge = document.createElement('span');
    badge.className = 'badge history-row-status-badge';

    const when = document.createElement('span');
    when.className = 'history-row-when';

    const dur = document.createElement('span');
    dur.className = 'history-row-dur';

    const viewBtn = document.createElement('button');
    viewBtn.type = 'button';
    viewBtn.className = 'btn btn-secondary btn-sm';
    viewBtn.textContent = 'View output';

    row.append(badge, when, dur, viewBtn);
    updateHistoryRow(row, exec);
    return row;
  }

  function updateHistoryRow(row: HTMLElement, exec: TaskExecution): void {
    const badge = row.querySelector<HTMLElement>('.history-row-status-badge');
    if (badge) renderStatusBadge(badge, exec.status, exec.suspended);

    const when = row.querySelector<HTMLElement>('.history-row-when');
    if (when) when.textContent = fmtDate(exec.started_at ?? exec.scheduled_at);
    const dur = row.querySelector<HTMLElement>('.history-row-dur');
    if (dur) dur.textContent = fmtMs(exec.duration_ms);

    const viewBtn = row.querySelector<HTMLButtonElement>('.btn-secondary');
    if (viewBtn) viewBtn.onclick = makeViewOutputHandler(exec);
  }

  function makeViewOutputHandler(exec: TaskExecution): () => void {
    function handleClick(): void {
      openOutputModal(exec.id, task.name + ' — run #' + exec.id);
    }
    return handleClick;
  }

  async function loadMetrics(): Promise<void> {
    let rows: MetricSummary[] = [];
    try {
      rows = await api.getMetrics(undefined, task.name);
    } catch {
      metricsWrap.textContent = 'Metrics unavailable.';
      return;
    }
    if (destroyed) return;
    const m = rows.find((r) => r.task_name === task.name) ?? rows[0];
    if (!m) {
      metricsWrap.textContent = 'No runs recorded yet.';
      return;
    }
    if (metricsWrap.textContent !== '' && metricsWrap.children.length === 0) {
      metricsWrap.textContent = '';
    }
    const stats: Array<[string, string]> = [
      ['success', String(m.success_count)],
      ['failed', String(m.failed_count)],
      ['canceled', String(m.canceled_count)],
      ['avg', fmtMs(m.avg_duration_ms ?? null)],
      ['min', fmtMs(m.min_duration_ms ?? null)],
      ['max', fmtMs(m.max_duration_ms ?? null)],
      ['last run', fmtDate(m.last_execution ?? null)],
    ];
    // Patch metric cells in place rather than rebuilding the grid, so a
    // live-event-triggered refresh never flickers.
    let grid = metricsWrap.querySelector<HTMLElement>('.task-detail-metric-grid');
    if (!grid) {
      metricsWrap.textContent = '';
      grid = document.createElement('div');
      grid.className = 'task-detail-metric-grid';
      metricsWrap.appendChild(grid);
    }
    for (const [label, val] of stats) {
      const key = label.replace(/\s+/g, '-');
      let cell = grid.querySelector<HTMLElement>('[data-metric="' + key + '"]');
      if (!cell) {
        cell = document.createElement('div');
        cell.className = 'task-detail-metric';
        cell.setAttribute('data-metric', key);
        const v = document.createElement('div');
        v.className = 'task-detail-metric-val';
        const l = document.createElement('div');
        l.className = 'task-detail-metric-label';
        l.textContent = label;
        cell.append(v, l);
        grid.appendChild(cell);
      }
      const v = cell.querySelector<HTMLElement>('.task-detail-metric-val');
      if (v) v.textContent = val;
    }
  }

  function refreshHistoryAndMetrics(): void {
    void loadHistory();
    void loadMetrics();
  }

  refreshHistoryAndMetrics();

  // Event-driven refresh: re-check history/metrics whenever the shared
  // board-events feed reports a change, instead of polling on a blunt
  // timer. patchList + the in-place metric-cell updates above ensure this
  // never causes a visible repaint.
  const unsubscribe = live.onEvent(refreshHistoryAndMetrics);

  function cleanup(): void {
    destroyed = true;
    unsubscribe();
  }
  return cleanup;
}
