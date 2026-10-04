// board.ts — the lane board (home screen).
//
// Renders lanes as vertical columns, each a playlist that reads top→bottom
// in the order things happen: Running (now) / Up next (in the order they'll
// run) / Recent runs (below, de-emphasized and collapsible, most-recent
// first). Live updates arrive via the shared LiveController (ui/live.ts)
// subscribed to GET /api/board/events; each lane's Running/Up next/Recent
// runs list is rendered by the shared QueuePanel (@shared, plan
// docs/PLAN-utuber-taskmaster-lane.md §4.11, decision D10 — taskmaster is
// its first adopter, utuber's queue page is the second), which patches its
// rows surgically (no full repaint, no scroll/focus loss — FRD §8a).
// Drag-and-drop reorders within a lane (PUT .../order) and moves a task
// between lanes (POST .../move); that stays board-specific and is wired
// directly onto the panel's "upnext" list.
//
// Task creation opens the full Task Designer (designer.ts): name/command/
// lane, Repeat + Cooldown, an Advanced section (output_file/sudo), and live
// taskmasterctl/curl export panels. Clicking a task's name navigates to its
// drill-in (`#task/<name>`, handled by taskview.ts + taskdetail.ts) — an
// in-page split view, not a modal.
//
// mountBoard also renders the single-lane column used by the split view
// (see the `laneFilter` option below): the same lane markup, same ordering,
// same live wiring, just scoped to one lane and without the "+ new lane"
// toolbar.

import { api, Capabilities, Lane, LaneStatus, Task, TaskExecution } from './api.js';
import { LiveController, BoardEvent } from './ui/live.js';
import { openModal, confirmDialog, alertDialog, openOutputModal, QueuePanel, QueuePanelAdapter, QueueActionKind, QueueProgress } from '@shared';
import { openTaskDesigner } from './designer.js';
import { fmtElapsed, renderStatusBadge, effectiveStatus } from './status.js';

const RAN_PER_LANE = 5;

export interface MountBoardOptions {
  /** When set, render only this lane (no "+ new lane" toolbar) — used by the split task view. */
  laneFilter?: string;
}

interface BoardState {
  lanes: LaneStatus[];
  tasks: Task[];
  executions: TaskExecution[];
}

// A running/recent row wraps an execution; an up-next row wraps a task (plus
// its pending execution, if any — a func task is only "up next" while one
// exists, P3).
type BoardItem =
  | { t: 'exec'; exec: TaskExecution; sec: 'running' | 'recent' }
  | { t: 'task'; task: Task; pending: TaskExecution | null };

const state: BoardState = { lanes: [], tasks: [], executions: [] };

let boardEl: HTMLElement | null = null;
let unsubscribeEvent: (() => void) | null = null;
let countdownTimer: number | undefined;
let loadSeq = 0;
let caps: Capabilities = { allow_sudo: false };
let laneFilter: string | undefined;
let brakeEngaged = false;

const panels = new WeakMap<HTMLElement, QueuePanel<BoardItem>>();

function openTaskRoute(taskName: string): void {
  window.location.hash = '#task/' + encodeURIComponent(taskName);
}

/** Mounts the board into `container`. Returns a cleanup function to call on navigation away. */
export function mountBoard(
  container: HTMLElement,
  live: LiveController,
  capabilities: Capabilities,
  options: MountBoardOptions = {}
): () => void {
  container.textContent = '';
  caps = capabilities;
  laneFilter = options.laneFilter;

  if (!laneFilter) {
    const toolbar = document.createElement('div');
    toolbar.className = 'board-toolbar';
    const addLaneBtn = document.createElement('button');
    addLaneBtn.className = 'btn btn-secondary';
    addLaneBtn.textContent = '+ new lane';
    addLaneBtn.addEventListener('click', () => void openAddLaneModal());
    toolbar.appendChild(addLaneBtn);
    container.appendChild(toolbar);
  }

  boardEl = document.createElement('div');
  boardEl.className = 'board-lanes' + (laneFilter ? ' board-lanes-single' : '');
  container.appendChild(boardEl);

  void api.getBrake().then((b) => { brakeEngaged = b.engaged; render(); }).catch(() => {});

  void refreshAll();

  unsubscribeEvent = live.onEvent((ev) => void handleBoardEvent(ev));
  countdownTimer = window.setInterval(() => render(), 1000);

  return () => {
    if (unsubscribeEvent) unsubscribeEvent();
    unsubscribeEvent = null;
    if (countdownTimer !== undefined) {
      clearInterval(countdownTimer);
      countdownTimer = undefined;
    }
    boardEl = null;
  };
}

async function handleBoardEvent(ev: BoardEvent): Promise<void> {
  if (ev.type === 'brake' && typeof ev.engaged === 'boolean') {
    brakeEngaged = ev.engaged;
  }
  // task-progress is a high-frequency, small event: patch the one
  // execution's progress fields in place and re-render, without a full
  // refetch (that would storm the server on a small poll interval). Every
  // other event type still means "something on the board changed" and gets
  // the ordinary refetch-and-let-patchList-reconcile treatment.
  if (ev.type === 'task-progress' && ev.execution_id !== undefined) {
    const idx = state.executions.findIndex((e) => e.id === ev.execution_id);
    if (idx !== -1) {
      state.executions[idx] = {
        ...state.executions[idx],
        progress_pct: ev.progress_pct ?? null,
        progress_label: ev.progress_label ?? '',
      };
      render();
    }
    return;
  }
  await refreshAll();
}

async function refreshAll(): Promise<void> {
  const seq = ++loadSeq;
  try {
    const [lanes, tasks, executions] = await Promise.all([
      api.listLanes(),
      api.listTasks(),
      api.listExecutions(undefined, 300),
    ]);
    if (seq !== loadSeq) return; // a newer refresh superseded this one
    state.lanes = lanes;
    state.tasks = tasks;
    state.executions = executions;
    render();
  } catch {
    // api.ts already surfaced the error via alertDialog.
  }
}

function tasksByLane(laneName: string): Task[] {
  return state.tasks
    .filter((t) => t.lane_name === laneName)
    .sort((a, b) => a.position - b.position);
}

function taskByName(name: string | undefined): Task | undefined {
  if (!name) return undefined;
  return state.tasks.find((t) => t.name === name);
}

function executionsByTask(taskName: string): TaskExecution[] {
  return state.executions.filter((e) => e.task_name === taskName);
}

function render(): void {
  if (!boardEl) return;
  const visible = laneFilter ? state.lanes.filter((l) => l.name === laneFilter) : state.lanes;
  if (visible.length === 0) {
    if (!boardEl.querySelector('.empty-state')) {
      boardEl.textContent = '';
      const empty = document.createElement('div');
      empty.className = 'empty-state';
      empty.textContent = laneFilter ? 'Lane not found.' : 'No lanes yet. Create one to start running tasks.';
      boardEl.appendChild(empty);
    }
    return;
  }
  boardEl.querySelector('.empty-state')?.remove();

  const sorted = [...visible].sort((a, b) => a.name.localeCompare(b.name));
  patchLanes(boardEl, sorted);
}

// A tiny lane-level patcher (lanes are few and keyed by name; the shared
// patchList primitive is reserved for the item lists inside each lane,
// which is where the row volume — and the anti-jitter need — actually is).
function patchLanes(container: HTMLElement, lanes: LaneStatus[]): void {
  const existingByName = new Map<string, HTMLElement>();
  for (const child of Array.from(container.children)) {
    const el = child as HTMLElement;
    const name = el.getAttribute('data-lane');
    if (name !== null) existingByName.set(name, el);
  }
  const seen = new Set<string>();
  let cursor: ChildNode | null = container.firstChild;
  for (const lane of lanes) {
    seen.add(lane.name);
    let el = existingByName.get(lane.name);
    if (el) {
      updateLaneEl(el, lane);
    } else {
      el = createLaneEl(lane);
    }
    if (cursor !== el) {
      container.insertBefore(el, cursor);
    } else {
      cursor = cursor.nextSibling;
      continue;
    }
    cursor = el.nextSibling;
  }
  for (const [name, el] of existingByName) {
    if (!seen.has(name)) el.remove();
  }
}

// ─── Lane column ─────────────────────────────────────────────────────────

function createLaneEl(lane: LaneStatus): HTMLElement {
  const el = document.createElement('section');
  el.className = 'lane';

  const header = document.createElement('div');
  header.className = 'lane-header';

  const nameEl = document.createElement('span');
  nameEl.className = 'lane-name';
  header.appendChild(nameEl);

  // Per-lane pause/resume, independent of the global hand brake. Pausing a
  // lane lets its current run finish but starts nothing new; re-symboled
  // to read as ⏹ Stop / ▶ Play, matching the process pause/resume glyphs
  // used elsewhere in the app.
  const pauseWrap = document.createElement('div');
  pauseWrap.className = 'lane-pause';
  const pauseBtn = document.createElement('button');
  pauseBtn.type = 'button';
  pauseBtn.className = 'btn-icon lane-pause-btn';
  const pauseLabel = document.createElement('span');
  pauseLabel.className = 'lane-pause-label';
  pauseLabel.textContent = 'Paused';
  pauseWrap.append(pauseBtn, pauseLabel);
  header.appendChild(pauseWrap);

  const widthWrap = document.createElement('div');
  widthWrap.className = 'lane-width';
  const widthDown = document.createElement('button');
  widthDown.type = 'button';
  widthDown.className = 'lane-width-btn';
  widthDown.textContent = '−';
  widthDown.setAttribute('aria-label', 'Decrease lane width');
  const widthVal = document.createElement('span');
  widthVal.className = 'lane-width-val';
  const widthUp = document.createElement('button');
  widthUp.type = 'button';
  widthUp.className = 'lane-width-btn';
  widthUp.textContent = '+';
  widthUp.setAttribute('aria-label', 'Increase lane width');
  widthWrap.append(widthDown, widthVal, widthUp);
  header.appendChild(widthWrap);

  const deleteBtn = document.createElement('button');
  deleteBtn.type = 'button';
  deleteBtn.className = 'lane-delete-btn';
  deleteBtn.title = 'Delete lane';
  deleteBtn.setAttribute('aria-label', 'Delete lane');
  deleteBtn.textContent = '×';
  header.appendChild(deleteBtn);

  el.appendChild(header);

  const addBtn = document.createElement('button');
  addBtn.className = 'btn btn-primary btn-sm lane-add-task';
  addBtn.textContent = '+ add task';
  el.appendChild(addBtn);

  const body = document.createElement('div');
  body.className = 'lane-body';
  el.appendChild(body);

  // Playlist order, top → bottom: what's running now, what's up next (in
  // the order it will run), then recent runs — de-emphasized and
  // collapsible since they're history, not what's about to happen. The
  // shared QueuePanel owns that structure; board.ts supplies only the
  // adapter (tmQueueAdapter) and the module-specific extras (decorate).
  const panel = new QueuePanel<BoardItem>(body, tmQueueAdapter, {
    recentLimit: RAN_PER_LANE,
    titles: { recent: 'Recent runs' },
    emptyText: { running: 'nothing running', upnext: 'lane is empty', recent: 'no history yet' },
  });
  panels.set(el, panel);

  wireDropTarget(panel.list('upnext'), el);

  updateLaneEl(el, lane);
  return el;
}

/**
 * Stopping a lane lets its current run finish by default. When something is
 * running, ask whether to cancel it too; dismissing the dialog keeps the
 * default. The lane is paused first so nothing new starts, then the running
 * executions are canceled.
 */
async function toggleLanePause(btn: HTMLButtonElement, lane: LaneStatus): Promise<void> {
  if (lane.paused) {
    btn.disabled = true;
    await api.resumeLane(lane.name).catch(() => undefined);
    btn.disabled = false;
    void refreshAll();
    return;
  }

  const laneTaskNames = new Set(tasksByLane(lane.name).map((t) => t.name));
  const running = state.executions.filter(
    (e) => e.status === 'running' && !!e.task_name && laneTaskNames.has(e.task_name),
  );
  let cancelToo = false;
  if (running.length > 0) {
    const names = running.map((e) => e.task_name).join(', ');
    cancelToo = await confirmDialog(
      running.length === 1
        ? `"${names}" is still running in this lane. Let it finish, or cancel it now?`
        : `${running.length} tasks are still running in this lane (${names}). Let them finish, or cancel them now?`,
      {
        title: `Stop lane "${lane.name}"`,
        confirmLabel: running.length === 1 ? 'Cancel it too' : 'Cancel them too',
        cancelLabel: running.length === 1 ? 'Let it finish' : 'Let them finish',
      },
    );
  }

  btn.disabled = true;
  try {
    await api.pauseLane(lane.name);
    if (cancelToo) {
      await Promise.all(running.map((e) => api.cancelExecution(e.id).catch(() => undefined)));
    }
  } catch {
    // refreshAll() below shows the lane's actual state either way.
  } finally {
    btn.disabled = false;
    void refreshAll();
  }
}

function updateLaneEl(el: HTMLElement, lane: LaneStatus): void {
  el.setAttribute('data-lane', lane.name);
  el.classList.toggle('lane-paused', lane.paused);

  const nameEl = el.querySelector<HTMLElement>('.lane-name');
  if (nameEl) {
    nameEl.textContent = lane.name;
  }

  const pauseBtn = el.querySelector<HTMLButtonElement>('.lane-pause-btn');
  if (pauseBtn) {
    pauseBtn.textContent = lane.paused ? '▶' : '⏹';
    pauseBtn.title = lane.paused ? 'Resume lane' : 'Stop lane (start nothing new; asks about a running task)';
    pauseBtn.setAttribute('aria-label', pauseBtn.title);
    pauseBtn.onclick = () => void toggleLanePause(pauseBtn, lane);
  }
  const pauseLabel = el.querySelector<HTMLElement>('.lane-pause-label');
  if (pauseLabel) pauseLabel.style.display = lane.paused ? '' : 'none';

  const widthVal = el.querySelector<HTMLElement>('.lane-width-val');
  if (widthVal) widthVal.textContent = String(lane.width);

  const widthDown = el.querySelector<HTMLButtonElement>('.lane-width-btn:first-child');
  const widthUp = el.querySelector<HTMLButtonElement>('.lane-width-btn:last-of-type');
  if (widthDown) {
    widthDown.disabled = lane.width <= 1;
    widthDown.onclick = () => void changeWidth(lane, lane.width - 1);
  }
  if (widthUp) {
    widthUp.onclick = () => void changeWidth(lane, lane.width + 1);
  }

  const deleteBtn = el.querySelector<HTMLButtonElement>('.lane-delete-btn');
  if (deleteBtn) {
    deleteBtn.onclick = () => void deleteLane(lane.name);
  }

  const addBtn = el.querySelector<HTMLButtonElement>('.lane-add-task');
  if (addBtn) {
    addBtn.onclick = () => void openTaskDesigner(state.lanes, lane.name, caps).then(() => refreshAll());
  }

  const laneTasks = tasksByLane(lane.name);
  const laneTaskNames = new Set(laneTasks.map((t) => t.name));
  const laneExecs = state.executions.filter((e) => e.task_name && laneTaskNames.has(e.task_name));

  const runningExecs = laneExecs
    .filter((e) => e.status === 'running')
    .sort((a, b) => b.id - a.id);
  const ranExecs = laneExecs
    .filter((e) => e.status === 'success' || e.status === 'failed' || e.status === 'canceled')
    .sort((a, b) => b.id - a.id);

  const runningTaskNames = new Set(runningExecs.map((e) => e.task_name));
  const pendingByTask = new Map<string, TaskExecution>();
  for (const e of laneExecs) {
    if (e.status === 'pending' && e.task_name && !pendingByTask.has(e.task_name)) {
      pendingByTask.set(e.task_name, e);
    }
  }

  // A func task is only ever "up next" while a pending execution exists —
  // once it finishes it drops out of Up next entirely (P3's eligibility
  // narrowing means a finished one-shot func task never gets re-picked).
  // Shell tasks keep their existing always-listed behavior.
  const upNextTasks = laneTasks.filter((t) => {
    if (runningTaskNames.has(t.name)) return false;
    if (t.kind && !pendingByTask.has(t.name)) return false;
    return true;
  });

  const items: BoardItem[] = [
    ...runningExecs.map((exec): BoardItem => ({ t: 'exec', exec, sec: 'running' })),
    ...upNextTasks.map((task): BoardItem => ({ t: 'task', task, pending: pendingByTask.get(task.name) ?? null })),
    ...ranExecs.map((exec): BoardItem => ({ t: 'exec', exec, sec: 'recent' })),
  ];

  panels.get(el)?.update(items);
}

// ─── Shared queue panel adapter (BoardItem) ────────────────────────────────

function itemKey(item: BoardItem): string {
  return item.t === 'exec' ? 'exec:' + item.exec.id : 'task:' + item.task.name;
}

function itemSection(item: BoardItem): 'running' | 'upnext' | 'recent' {
  return item.t === 'exec' ? item.sec : 'upnext';
}

function itemTitle(item: BoardItem): string {
  if (item.t === 'task') return item.task.label || item.task.name;
  const task = taskByName(item.exec.task_name);
  return (task && (task.label || task.name)) || item.exec.task_name || '(unknown task)';
}

function itemStatus(item: BoardItem): string {
  if (item.t === 'exec') return effectiveStatus(item.exec.status, item.exec.suspended);
  if (item.pending) return 'queued';
  if (!item.task.enabled) return 'disabled';
  if (item.task.paused) return 'paused';
  return 'ready';
}

function itemMeta(item: BoardItem): string {
  if (item.t === 'exec') {
    if (item.exec.duration_ms !== undefined && item.exec.duration_ms !== null) {
      return (item.exec.duration_ms / 1000).toFixed(1) + 's';
    }
    if (item.exec.suspended) {
      // A paused process's wall-clock time keeps passing even though it's
      // doing nothing — a ticking counter here would make a genuinely
      // frozen process look like pause had no effect. Say so plainly
      // instead (the badge already shows ⏸ too).
      return 'paused';
    }
    if (item.exec.started_at) {
      // "running…" said nothing useful — show live elapsed time instead.
      // The board's 1s tick (render()) re-runs this via the panel's update
      // callback, so this counts up on its own.
      return fmtElapsed(item.exec.started_at);
    }
    return '';
  }
  const task = item.task;
  if (!task.enabled) return 'disabled';
  if (task.paused) return 'paused';
  if (item.pending) return 'queued';
  if (task.repeat) {
    // No "again in X" countdown while the hand brake is engaged — it would
    // imply the task might still fire on its own, which the brake
    // explicitly prevents.
    return brakeEngaged ? 'ready' : cooldownLabel(task);
  }
  return 'ready';
}

function itemProgress(item: BoardItem): QueueProgress | null {
  if (item.t !== 'exec') return null;
  const pct = item.exec.progress_pct;
  const label = item.exec.progress_label;
  if (pct === undefined && !label) return null;
  return { pct: pct === undefined ? null : pct, label: label ?? '' };
}

function itemActions(item: BoardItem): QueueActionKind[] {
  if (item.t === 'exec') {
    if (item.sec === 'running') {
      const task = taskByName(item.exec.task_name);
      if (task?.kind) return ['cancel']; // func tasks have no per-execution SIGSTOP (P5/G2)
      return [item.exec.suspended ? 'resume' : 'pause', 'cancel'];
    }
    const task = taskByName(item.exec.task_name);
    return task?.kind ? ['rerun', 'remove'] : ['rerun'];
  }
  // Up-next: only a func task with a pending execution offers a generic
  // action (cancel-while-queued, P4); a shell up-next task's "Up next"
  // button is a module extra added by decorate(), not a QueuePanel action.
  if (item.task.kind && item.pending) return ['cancel'];
  return [];
}

async function onBoardAction(action: QueueActionKind, item: BoardItem, btn: HTMLButtonElement): Promise<void> {
  btn.disabled = true;
  try {
    if (item.t === 'exec') {
      if (action === 'cancel') {
        await api.cancelExecution(item.exec.id);
      } else if (action === 'pause') {
        await api.pauseExecution(item.exec.id);
      } else if (action === 'resume') {
        await api.resumeExecution(item.exec.id);
      } else if (action === 'rerun') {
        await api.rerunExecution(item.exec.id);
      } else if (action === 'remove') {
        const name = item.exec.task_name;
        if (name) {
          const ok = await confirmDialog('Remove task "' + name + '"? This deletes its run history too.', {
            title: 'Remove task',
            confirmLabel: 'Remove',
          });
          if (ok) await api.deleteTask(name);
        }
      }
    } else if (action === 'cancel' && item.pending) {
      await api.cancelExecution(item.pending.id);
    }
  } catch {
    // api.ts already surfaced the error via alertDialog.
  } finally {
    btn.disabled = false;
    void refreshAll();
  }
}

function renderBoardBadge(badge: HTMLElement, item: BoardItem): void {
  if (item.t === 'exec') {
    renderStatusBadge(badge, item.exec.status, item.exec.suspended);
    return;
  }
  // An up-next task row shows its state as plain text (itemMeta), the way
  // the legacy task row did — no colored status dot for "not running yet".
  badge.className = 'ui-queue-badge';
  badge.textContent = '';
  badge.title = '';
  badge.removeAttribute('aria-label');
}

/** Module-specific extras the generic QueuePanel row doesn't know about: pid, live output, drag, Up next, and the legacy CSS classes style.css still targets. */
function decorateBoardRow(row: HTMLElement, item: BoardItem, created: boolean): void {
  if (item.t === 'exec') {
    const kindClass = item.sec === 'running' ? 'running' : 'ran';
    row.classList.add('exec-row', 'exec-row-' + kindClass);
    if (item.sec !== 'running') return;

    let pidSeam = row.querySelector<HTMLElement>('.exec-row-pid-seam');
    if (!pidSeam) {
      pidSeam = document.createElement('span');
      pidSeam.className = 'exec-row-pid-seam';
      const pidLabel = document.createElement('span');
      pidLabel.className = 'exec-row-pid';
      const outputBtn = document.createElement('button');
      outputBtn.type = 'button';
      outputBtn.className = 'btn-icon exec-row-output-btn';
      // Not ⏹ (that means Stop) or any other square — a square already
      // means something else in this app. ↗ reads as "open in a window",
      // matching what the button actually does.
      outputBtn.textContent = '↗';
      outputBtn.title = 'View live output';
      outputBtn.setAttribute('aria-label', 'View live output');
      pidSeam.append(pidLabel, outputBtn);
      row.appendChild(pidSeam);
    }
    const pidLabel = row.querySelector<HTMLElement>('.exec-row-pid');
    if (pidLabel) pidLabel.textContent = item.exec.pid !== undefined ? 'pid ' + item.exec.pid : '';
    const outputBtn = row.querySelector<HTMLButtonElement>('.exec-row-output-btn');
    if (outputBtn) outputBtn.onclick = () => openRunningOutput(item.exec);
    return;
  }

  row.classList.add('task-row');
  row.setAttribute('data-task', item.task.name);
  row.classList.toggle('task-row-disabled', !item.task.enabled || item.task.paused);

  if (item.task.kind) return; // func tasks: no drag, no "Up next" button

  if (created) {
    const capturedName = item.task.name;
    const capturedLane = item.task.lane_name;
    row.draggable = true;
    row.addEventListener('dragstart', (e) => {
      if (!e.dataTransfer) return;
      e.dataTransfer.effectAllowed = 'move';
      e.dataTransfer.setData('text/plain', JSON.stringify({ task: capturedName, lane: capturedLane }));
      row.classList.add('dragging');
    });
    row.addEventListener('dragend', () => row.classList.remove('dragging'));
  }

  let upNextBtn = row.querySelector<HTMLButtonElement>('.task-row-upnext');
  if (!upNextBtn) {
    upNextBtn = document.createElement('button');
    upNextBtn.type = 'button';
    upNextBtn.className = 'btn btn-secondary btn-sm task-row-upnext';
    upNextBtn.textContent = 'Up next';
    row.appendChild(upNextBtn);
  }
  upNextBtn.disabled = !item.task.enabled || item.task.paused;
  const taskName = item.task.name;
  upNextBtn.onclick = () => void api.upNext(taskName).then(() => refreshAll());
}

const tmQueueAdapter: QueuePanelAdapter<BoardItem> = {
  key: itemKey,
  section: itemSection,
  title: itemTitle,
  status: itemStatus,
  meta: itemMeta,
  progress: itemProgress,
  actions: itemActions,
  onAction: onBoardAction,
  renderBadge: renderBoardBadge,
  decorate: decorateBoardRow,
  onTitleClick: (item) => openTaskRoute(item.t === 'exec' ? item.exec.task_name ?? '' : item.task.name),
};

function openRunningOutput(exec: TaskExecution): void {
  const title = (exec.task_name ?? 'task') + ' — run #' + exec.id;
  openOutputModal(api.openExecutionOutput(exec.id), title);
}

function cooldownLabel(task: Task): string {
  const execs = executionsByTask(task.name)
    .filter((e) => e.finished_at)
    .sort((a, b) => b.id - a.id);
  const last = execs[0];
  if (!last || !last.finished_at) return 'ready';
  const readyAt = new Date(last.finished_at).getTime() + task.cooldown_seconds * 1000;
  const remainingMs = readyAt - Date.now();
  if (remainingMs <= 0) return 'ready';
  const totalSec = Math.ceil(remainingMs / 1000);
  const m = Math.floor(totalSec / 60);
  const s = totalSec % 60;
  return 'again in ' + m + ':' + String(s).padStart(2, '0');
}

// ─── Drag and drop ───────────────────────────────────────────────────────

function wireDropTarget(list: HTMLElement, laneEl: HTMLElement): void {
  list.addEventListener('dragover', (e) => {
    e.preventDefault();
    if (e.dataTransfer) e.dataTransfer.dropEffect = 'move';
    list.classList.add('drag-over');
    const dragging = list.querySelector<HTMLElement>('.dragging');
    const after = rowAfter(list, e.clientY);
    if (dragging) {
      if (after == null) {
        list.appendChild(dragging);
      } else if (after !== dragging) {
        list.insertBefore(dragging, after);
      }
    }
  });
  list.addEventListener('dragleave', (e) => {
    if (e.target === list) list.classList.remove('drag-over');
  });
  list.addEventListener('drop', (e) => {
    e.preventDefault();
    list.classList.remove('drag-over');
    const raw = e.dataTransfer?.getData('text/plain');
    if (!raw) return;
    let payload: { task: string; lane: string };
    try {
      payload = JSON.parse(raw);
    } catch {
      return;
    }
    const destLane = laneEl.getAttribute('data-lane');
    if (!destLane) return;
    void handleDrop(payload.task, payload.lane, destLane, list);
  });
}

function rowAfter(list: HTMLElement, y: number): HTMLElement | null {
  const rows = Array.from(list.querySelectorAll<HTMLElement>('.task-row:not(.dragging)'));
  let closest: { el: HTMLElement; offset: number } | null = null;
  for (const row of rows) {
    const box = row.getBoundingClientRect();
    const offset = y - box.top - box.height / 2;
    if (offset < 0 && (closest === null || offset > closest.offset)) {
      closest = { el: row, offset };
    }
  }
  return closest ? closest.el : null;
}

async function handleDrop(taskName: string, sourceLane: string, destLane: string, list: HTMLElement): Promise<void> {
  const currentOrder = Array.from(list.querySelectorAll<HTMLElement>('.task-row')).map(
    (r) => r.getAttribute('data-task') as string
  );
  try {
    if (sourceLane !== destLane) {
      await api.moveTask(taskName, destLane);
    }
    await api.setLaneOrder(destLane, currentOrder);
  } finally {
    await refreshAll();
  }
}

// ─── Width / delete lane ─────────────────────────────────────────────────

async function changeWidth(lane: Lane, width: number): Promise<void> {
  if (width < 1) return;
  try {
    await api.setLaneWidth(lane.name, width);
  } finally {
    await refreshAll();
  }
}

async function deleteLane(name: string): Promise<void> {
  const ok = await confirmDialog('Delete lane "' + name + '"? Tasks in it must be moved or removed first.', {
    title: 'Delete lane',
    confirmLabel: 'Delete',
  });
  if (!ok) return;
  try {
    await api.deleteLane(name);
  } finally {
    await refreshAll();
  }
}

// ─── Modals: new lane / add task ─────────────────────────────────────────

async function openAddLaneModal(): Promise<void> {
  const content = document.createElement('div');

  const nameGroup = document.createElement('div');
  nameGroup.className = 'form-group';
  const nameLabel = document.createElement('label');
  nameLabel.textContent = 'Lane name';
  const nameInput = document.createElement('input');
  nameInput.type = 'text';
  nameInput.placeholder = 'e.g. backups';
  nameGroup.append(nameLabel, nameInput);

  const widthGroup = document.createElement('div');
  widthGroup.className = 'form-group';
  const widthLabel = document.createElement('label');
  widthLabel.textContent = 'Width (concurrent slots)';
  const widthInput = document.createElement('input');
  widthInput.type = 'number';
  widthInput.min = '1';
  widthInput.value = '1';
  widthGroup.append(widthLabel, widthInput);

  const actions = document.createElement('div');
  actions.className = 'form-actions';
  const cancelBtn = document.createElement('button');
  cancelBtn.type = 'button';
  cancelBtn.className = 'btn btn-secondary';
  cancelBtn.textContent = 'Cancel';
  const createBtn = document.createElement('button');
  createBtn.type = 'button';
  createBtn.className = 'btn btn-primary';
  createBtn.textContent = 'Create lane';
  actions.append(cancelBtn, createBtn);

  content.append(nameGroup, widthGroup, actions);

  const handle = openModal(content, { title: 'New lane' });
  cancelBtn.addEventListener('click', () => handle.close());
  createBtn.addEventListener('click', () => {
    void (async () => {
      const name = nameInput.value.trim();
      const width = parseInt(widthInput.value, 10) || 1;
      if (!name) {
        await alertDialog('Lane name is required.');
        return;
      }
      try {
        await api.createLane({ name, width });
        handle.close();
        await refreshAll();
      } catch {
        // api.ts already surfaced the error.
      }
    })();
  });
  nameInput.focus();
}

// Task creation now goes through the full Task Designer (designer.ts,
// opened above via openTaskDesigner) — advanced fields, sudo gating, and
// the taskmasterctl/curl exports live there.
