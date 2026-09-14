// taskdetail.ts — the per-task drill-in panel (plan Phase F4, split-view
// rework).
//
// Used to be a modal opened over the full board; it is now mounted directly
// into the right-hand pane of the split task view (see taskview.ts), with a
// "Back to board" control instead of a close button. Shows: run history
// (api.listExecutions(name)), a live output viewer for a selected/most
// recent execution (api.openExecutionOutput(id) EventSource, closed on
// unmount or when a different execution is selected), a Cancel control on
// any RUNNING execution, and that task's own metrics
// (api.getMetrics(undefined, name)).
//
// Refresh is event-driven, not timer-driven: there is no polling interval
// here. Instead this subscribes to the shared LiveController's board events
// (the same feed the board uses) and re-fetches history/metrics only when
// something changed, patching both in place via patchList/targeted DOM
// updates — never a full innerHTML rebuild — so the Output pane never
// resets mid-stream and selecting a run never jitters the rest of the view.

import { api, MetricSummary, Task, TaskExecution } from './api.js';
import { LiveController } from './ui/live.js';
import { patchList } from './ui/live.js';

function statusBadgeClass(status: string): string {
  switch (status) {
    case 'success':
      return 'badge-green';
    case 'failed':
      return 'badge-red';
    case 'canceled':
      return 'badge-yellow';
    case 'running':
      return 'badge-blue';
    default:
      return 'badge-muted';
  }
}

function fmtMs(ms: number | null | undefined): string {
  if (ms === null || ms === undefined) return '—';
  return (ms / 1000).toFixed(2) + 's';
}

function fmtDate(iso: string | null | undefined): string {
  if (!iso) return '—';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return d.toLocaleString();
}

/**
 * Mounts the task detail panel into `container` (the right pane of the
 * split task view). `onBack` is invoked when the user asks to return to the
 * full board. `onChange` is invoked after any action here that might affect
 * the board (cancel), so the caller's own board mount (which listens to the
 * same live feed) can pick it up — most callers can pass a no-op since the
 * board already refreshes itself on board events. Returns a cleanup
 * function to call on navigation away.
 */
export function mountTaskDetail(
  container: HTMLElement,
  live: LiveController,
  task: Task,
  onBack: () => void,
  onChange: () => void = () => {}
): () => void {
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

  // --- Live output viewer (single, clear pane — always shows the selected run) ---
  const outputHeader = document.createElement('div');
  outputHeader.className = 'task-detail-section-title';
  outputHeader.textContent = 'Output';
  const outputBox = document.createElement('pre');
  outputBox.className = 'task-detail-output';
  outputBox.textContent = '(select a run to view its output)';
  container.append(outputHeader, outputBox);

  // --- History ---
  const historyHeader = document.createElement('div');
  historyHeader.className = 'task-detail-section-title';
  historyHeader.textContent = 'History';
  const historyList = document.createElement('div');
  historyList.className = 'task-detail-history';
  container.append(historyHeader, historyList);

  let currentSource: EventSource | null = null;
  let selectedExecId: number | null = null;
  let destroyed = false;

  function closeStream(): void {
    if (currentSource) {
      currentSource.close();
      currentSource = null;
    }
  }

  function streamExecution(exec: TaskExecution): void {
    if (selectedExecId === exec.id && currentSource) return; // already viewing this run
    closeStream();
    selectedExecId = exec.id;
    outputBox.textContent = '';
    updateHistorySelection();
    const source = api.openExecutionOutput(exec.id);
    currentSource = source;
    source.addEventListener('output', (ev: MessageEvent) => {
      try {
        const parsed = JSON.parse(ev.data) as { stream?: string; line?: string };
        outputBox.textContent += (parsed.line ?? '') + '\n';
        outputBox.scrollTop = outputBox.scrollHeight;
      } catch {
        // ignore malformed line
      }
    });
    source.addEventListener('status', (ev: MessageEvent) => {
      if (outputBox.textContent === '') outputBox.textContent = '(execution ' + ev.data + ')';
      closeStream();
    });
    source.addEventListener('done', () => closeStream());
    source.onerror = () => {
      // Execution finished / stream closed server-side; EventSource will
      // stop retrying naturally once the server ends the stream. Leave any
      // already-received output visible — never reset it here.
    };
  }

  function updateHistorySelection(): void {
    for (const row of Array.from(historyList.children)) {
      const el = row as HTMLElement;
      const id = Number(el.getAttribute('data-tm-key'));
      el.classList.toggle('history-row-selected', id === selectedExecId);
    }
  }

  async function loadHistory(): Promise<void> {
    let execs: TaskExecution[] = [];
    try {
      execs = await api.listExecutions(task.name, 50);
    } catch {
      return;
    }
    if (destroyed) return;
    execs.sort((a, b) => b.id - a.id); // most-recent first

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
      key: (e) => e.id,
      create: (e) => createHistoryRow(e),
      update: (row, e) => updateHistoryRow(row, e),
    });

    // Default to the most recent run; never yank the user off a run they
    // (or a prior auto-select) already picked.
    if (selectedExecId === null) {
      streamExecution(execs[0]);
    }
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

    const pidLabel = document.createElement('span');
    pidLabel.className = 'history-row-pid';

    const suspendedBadge = document.createElement('span');
    suspendedBadge.className = 'badge badge-yellow history-row-suspended-badge';
    suspendedBadge.textContent = 'suspended';

    const pauseBtn = document.createElement('button');
    pauseBtn.type = 'button';
    pauseBtn.className = 'btn-icon history-row-pause-btn';

    const viewBtn = document.createElement('button');
    viewBtn.type = 'button';
    viewBtn.className = 'btn btn-secondary btn-sm';
    viewBtn.textContent = 'View output';

    const cancelBtn = document.createElement('button');
    cancelBtn.type = 'button';
    cancelBtn.className = 'btn btn-danger btn-sm';
    cancelBtn.title = 'Cancel execution';
    cancelBtn.setAttribute('aria-label', 'Cancel execution');
    cancelBtn.textContent = '✖';

    row.append(pidLabel, suspendedBadge, pauseBtn, badge, when, dur, viewBtn, cancelBtn);
    updateHistoryRow(row, exec);
    return row;
  }

  function updateHistoryRow(row: HTMLElement, exec: TaskExecution): void {
    row.classList.toggle('history-row-selected', exec.id === selectedExecId);

    const badge = row.querySelector<HTMLElement>('.history-row-status-badge');
    if (badge) {
      badge.className = 'badge history-row-status-badge ' + statusBadgeClass(exec.status);
      badge.textContent = exec.status;
    }
    const when = row.querySelector<HTMLElement>('.history-row-when');
    if (when) when.textContent = fmtDate(exec.started_at ?? exec.scheduled_at);
    const dur = row.querySelector<HTMLElement>('.history-row-dur');
    if (dur) dur.textContent = fmtMs(exec.duration_ms);

    const viewBtn = row.querySelector<HTMLButtonElement>('.btn-secondary');
    if (viewBtn) viewBtn.onclick = () => streamExecution(exec);

    const isRunning = exec.status === 'running';

    // Process PID + pause/resume (SIGSTOP/SIGCONT) — only meaningful for a
    // genuinely running execution. Use style.display, not the `hidden`
    // attribute — the `.btn`/`.badge` classes set `display`, which
    // overrides `[hidden]` and would leave elements showing (and 404-ing)
    // on finished history rows.
    const pidLabel = row.querySelector<HTMLElement>('.history-row-pid');
    if (pidLabel) {
      pidLabel.style.display = isRunning ? '' : 'none';
      pidLabel.textContent = exec.pid !== undefined ? 'pid ' + exec.pid : '';
    }
    const suspendedBadge = row.querySelector<HTMLElement>('.history-row-suspended-badge');
    if (suspendedBadge) {
      suspendedBadge.style.display = isRunning && exec.suspended ? '' : 'none';
    }
    const pauseBtn = row.querySelector<HTMLButtonElement>('.history-row-pause-btn');
    if (pauseBtn) {
      pauseBtn.style.display = isRunning ? '' : 'none';
      if (isRunning) {
        const suspended = !!exec.suspended;
        pauseBtn.textContent = suspended ? '▶' : '⏸';
        pauseBtn.title = suspended ? 'Resume process' : 'Pause process';
        pauseBtn.setAttribute('aria-label', pauseBtn.title);
        pauseBtn.onclick = () => {
          pauseBtn.disabled = true;
          const req = suspended ? api.resumeExecution(exec.id) : api.pauseExecution(exec.id);
          void req
            .then(() => {
              onChange();
              void loadHistory();
            })
            .finally(() => {
              pauseBtn.disabled = false;
            });
        };
      }
    }

    const cancelBtn = row.querySelector<HTMLButtonElement>('.btn-danger');
    if (cancelBtn) {
      // Cancel only makes sense for a genuinely running execution. Use
      // style.display, not the `hidden` attribute — the `.btn` class sets
      // `display`, which overrides `[hidden]` and left the button showing
      // (and 404-ing) on finished history rows.
      cancelBtn.style.display = isRunning ? '' : 'none';
      cancelBtn.onclick = () => {
        cancelBtn.disabled = true;
        void api
          .cancelExecution(exec.id)
          .then(() => {
            onChange();
            void loadHistory();
          })
          .finally(() => {
            cancelBtn.disabled = false;
          });
      };
    }
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

  void loadHistory();
  void loadMetrics();

  // Event-driven refresh: re-check history/metrics whenever the shared
  // board-events feed reports a change, instead of polling on a blunt
  // timer. patchList + the in-place metric-cell updates above ensure this
  // never causes a visible repaint or resets the Output pane.
  const unsubscribe = live.onEvent(() => {
    void loadHistory();
    void loadMetrics();
  });

  return () => {
    destroyed = true;
    closeStream();
    unsubscribe();
  };
}
