// metrics.ts — the global Metrics tab (plan Phase F5, FRD §8).
//
// A fleet-wide, cross-task overview from api.getMetrics() (no task filter):
// a card grid with per-task success/failed/canceled counts, avg/min/max
// duration, and last run. Per-task metrics still live in the task drill-in
// (taskdetail.ts) — this is the "how's everything doing" view. Live via the
// shared LiveController (board events trigger a refetch); patched with
// patchList so re-renders never repaint the whole page.

import { api, MetricSummary } from './api.js';
import { LiveController, patchList } from './ui/live.js';

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

let gridEl: HTMLElement | null = null;
let unsubscribe: (() => void) | null = null;
let loadSeq = 0;

/** Mounts the metrics overview into `container`. Returns a cleanup function. */
export function mountMetrics(container: HTMLElement, live: LiveController): () => void {
  container.textContent = '';

  const heading = document.createElement('div');
  heading.className = 'metrics-heading';
  heading.textContent = 'Fleet-wide metrics (last 24h)';
  container.appendChild(heading);

  gridEl = document.createElement('div');
  gridEl.className = 'metrics-grid';
  container.appendChild(gridEl);

  void refresh();
  unsubscribe = live.onEvent(() => void refresh());

  return () => {
    if (unsubscribe) unsubscribe();
    unsubscribe = null;
    gridEl = null;
  };
}

async function refresh(): Promise<void> {
  const seq = ++loadSeq;
  let rows: MetricSummary[] = [];
  try {
    rows = await api.getMetrics();
  } catch {
    return;
  }
  if (seq !== loadSeq || !gridEl) return;
  render(rows);
}

function render(rows: MetricSummary[]): void {
  if (!gridEl) return;
  if (rows.length === 0) {
    if (!gridEl.querySelector('.empty-state')) {
      gridEl.textContent = '';
      const empty = document.createElement('div');
      empty.className = 'empty-state';
      empty.textContent = 'No executions recorded yet.';
      gridEl.appendChild(empty);
    }
    return;
  }
  gridEl.querySelector('.empty-state')?.remove();

  const sorted = [...rows].sort((a, b) => a.task_name.localeCompare(b.task_name));
  patchList(gridEl, sorted, {
    key: (m) => m.task_name,
    create: (m) => createCard(m),
    update: (el, m) => updateCard(el, m),
  });
}

function createCard(m: MetricSummary): HTMLElement {
  const card = document.createElement('div');
  card.className = 'metric-card';

  const title = document.createElement('div');
  title.className = 'metric-card-title';
  card.appendChild(title);

  const lane = document.createElement('div');
  lane.className = 'metric-card-lane';
  card.appendChild(lane);

  const stats = document.createElement('div');
  stats.className = 'metric-card-stats';
  const fields = ['success', 'failed', 'canceled', 'avg', 'min', 'max', 'last run'];
  for (const f of fields) {
    const cell = document.createElement('div');
    cell.className = 'metric-card-stat metric-stat-' + f.replace(' ', '-');
    const v = document.createElement('div');
    v.className = 'metric-card-stat-val';
    const l = document.createElement('div');
    l.className = 'metric-card-stat-label';
    l.textContent = f;
    cell.append(v, l);
    stats.appendChild(cell);
  }
  card.appendChild(stats);

  updateCard(card, m);
  return card;
}

function updateCard(card: HTMLElement, m: MetricSummary): void {
  const title = card.querySelector<HTMLElement>('.metric-card-title');
  if (title) title.textContent = m.task_name;
  const lane = card.querySelector<HTMLElement>('.metric-card-lane');
  if (lane) lane.textContent = 'lane: ' + m.group_name;

  const values: Record<string, string> = {
    success: String(m.success_count),
    failed: String(m.failed_count),
    canceled: String(m.canceled_count),
    avg: fmtMs(m.avg_duration_ms),
    min: fmtMs(m.min_duration_ms),
    max: fmtMs(m.max_duration_ms),
    'last-run': fmtDate(m.last_execution),
  };
  for (const [key, val] of Object.entries(values)) {
    const cell = card.querySelector<HTMLElement>('.metric-stat-' + key);
    const v = cell?.querySelector<HTMLElement>('.metric-card-stat-val');
    if (v) v.textContent = val;
  }
}
