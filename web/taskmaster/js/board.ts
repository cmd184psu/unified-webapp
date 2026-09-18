// board.ts — the lane board (home screen).
//
// Renders lanes as vertical columns, each a playlist that reads top→bottom
// in the order things happen: Running (now) / Up next (in the order they'll
// run) / Recent runs (below, de-emphasized and collapsible, most-recent
// first). Live updates arrive via the shared LiveController (ui/live.ts)
// subscribed to GET /api/board/events; every re-render goes through
// patchList so the DOM is patched surgically (no full repaint, no
// scroll/focus loss — FRD §8a). Drag-and-drop reorders within a lane (PUT
// .../order) and moves a task between lanes (POST .../move). Each lane
// header also carries its own pause/resume toggle (api.pauseLane /
// api.resumeLane) independent of the global hand brake.
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
import { LiveController, BoardEvent, patchList } from './ui/live.js';
import { openModal, confirmDialog, alertDialog } from '@shared';
import { openTaskDesigner } from './designer.js';
import { openOutputModal } from './outputmodal.js';
import { fmtElapsed, renderStatusBadge } from './status.js';

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

const state: BoardState = { lanes: [], tasks: [], executions: [] };

let boardEl: HTMLElement | null = null;
let unsubscribeEvent: (() => void) | null = null;
let countdownTimer: number | undefined;
let loadSeq = 0;
let caps: Capabilities = { allow_sudo: false };
let laneFilter: string | undefined;

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
  // Board events are compact change notifications, not full snapshots
  // (D7). Any event type means "something on the board changed" — refetch
  // the current snapshot and let patchList reconcile the DOM surgically.
  // (Coalescing per-lane refetches is a nice-to-have; a full snapshot
  // refetch keeps this slice simple while patchList still guarantees no
  // full-DOM repaint / no jitter.)
  void ev;
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
  patchList(boardEl, sorted, {
    key: (l) => l.name,
    create: (l) => createLaneEl(l),
    update: (el, l) => updateLaneEl(el, l),
  });
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

  // Playlist order, top → bottom: what's running now, what's up next (in
  // the order it will run), then recent runs — de-emphasized and
  // collapsible since they're history, not what's about to happen.
  const runningSection = buildSection('running', 'Running');
  const upNextSection = buildSection('upnext', 'Up next');
  const ranSection = buildRanSection();
  body.append(runningSection.wrap, upNextSection.wrap, ranSection.wrap);
  el.appendChild(body);

  wireDropTarget(upNextSection.list, el);

  updateLaneEl(el, lane);
  return el;
}

function buildSection(kind: string, title: string): { wrap: HTMLElement; list: HTMLElement } {
  const wrap = document.createElement('div');
  wrap.className = 'lane-section lane-section-' + kind;
  const h = document.createElement('div');
  h.className = 'lane-section-title';
  h.textContent = title;
  const list = document.createElement('div');
  list.className = 'lane-list lane-list-' + kind;
  wrap.append(h, list);
  return { wrap, list };
}

/** "Recent runs" — de-emphasized and collapsible (native <details>), most-recent first. */
function buildRanSection(): { wrap: HTMLElement; list: HTMLElement } {
  const wrap = document.createElement('details');
  wrap.className = 'lane-section lane-section-ran';
  const summary = document.createElement('summary');
  summary.className = 'lane-section-title';
  summary.textContent = 'Recent runs';
  const list = document.createElement('div');
  list.className = 'lane-list lane-list-ran';
  wrap.append(summary, list);
  return { wrap, list };
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
    pauseBtn.title = lane.paused ? 'Resume lane' : 'Stop lane (finish current run, start nothing new)';
    pauseBtn.setAttribute('aria-label', pauseBtn.title);
    pauseBtn.onclick = () => {
      pauseBtn.disabled = true;
      const req = lane.paused ? api.resumeLane(lane.name) : api.pauseLane(lane.name);
      void req.finally(() => {
        pauseBtn.disabled = false;
        void refreshAll();
      });
    };
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
    .sort((a, b) => b.id - a.id)
    .slice(0, RAN_PER_LANE);

  const runningTaskNames = new Set(runningExecs.map((e) => e.task_name));
  const pendingByTask = new Map<string, TaskExecution>();
  for (const e of laneExecs) {
    if (e.status === 'pending' && e.task_name && !pendingByTask.has(e.task_name)) {
      pendingByTask.set(e.task_name, e);
    }
  }

  const upNextTasks = laneTasks.filter((t) => !runningTaskNames.has(t.name));

  const runningList = el.querySelector<HTMLElement>('.lane-list-running');
  if (runningList) {
    patchList(runningList, runningExecs, {
      key: (e) => e.id,
      create: (e) => createExecRow(e, 'running'),
      update: (row, e) => updateExecRow(row, e, 'running'),
    });
    toggleEmptyNote(runningList, runningExecs.length === 0, 'nothing running');
  }

  const ranList = el.querySelector<HTMLElement>('.lane-list-ran');
  if (ranList) {
    patchList(ranList, ranExecs, {
      key: (e) => e.id,
      create: (e) => createExecRow(e, 'ran'),
      update: (row, e) => updateExecRow(row, e, 'ran'),
    });
    toggleEmptyNote(ranList, ranExecs.length === 0, 'no history yet');
  }

  const upNextList = el.querySelector<HTMLElement>('.lane-list-upnext');
  if (upNextList) {
    patchList(upNextList, upNextTasks, {
      key: (t) => t.name,
      create: (t) => createTaskRow(t, pendingByTask.get(t.name) ?? null),
      update: (row, t) => updateTaskRow(row, t, pendingByTask.get(t.name) ?? null),
    });
    toggleEmptyNote(upNextList, upNextTasks.length === 0, 'lane is empty');
  }
}

function toggleEmptyNote(list: HTMLElement, empty: boolean, text: string): void {
  let note = list.querySelector<HTMLElement>('.lane-empty-note');
  if (empty) {
    if (!note) {
      note = document.createElement('div');
      note.className = 'lane-empty-note';
      note.setAttribute('data-tm-key', '__empty__');
      list.appendChild(note);
    }
    note.textContent = text;
  } else {
    note?.remove();
  }
}

// ─── Rows ────────────────────────────────────────────────────────────────

function createExecRow(exec: TaskExecution, kind: 'running' | 'ran'): HTMLElement {
  const row = document.createElement('div');
  row.className = 'exec-row exec-row-' + kind;
  const name = document.createElement('span');
  name.className = 'exec-row-name exec-row-name-clickable';
  name.setAttribute('role', 'button');
  name.tabIndex = 0;
  const badge = document.createElement('span');
  badge.className = 'badge';
  const meta = document.createElement('span');
  meta.className = 'exec-row-meta';
  row.append(name, badge, meta);
  if (kind === 'running') {
    // The running instance lives ONLY in the lane card — the task-detail
    // split view no longer lists it (that would be the same process shown
    // twice, disagreeing whenever one side updated and the other didn't).
    // So every control for a running execution belongs here: PID, process
    // pause/resume (SIGSTOP/SIGCONT), cancel, and viewing its live output.
    const pidSeam = document.createElement('span');
    pidSeam.className = 'exec-row-pid-seam';

    const pidLabel = document.createElement('span');
    pidLabel.className = 'exec-row-pid';

    const outputBtn = document.createElement('button');
    outputBtn.type = 'button';
    outputBtn.className = 'btn-icon exec-row-output-btn';
    // Not ⏹ (that means Stop) or any other square — a square already means
    // something else in this app. ↗ reads as "open in a window", matching
    // what the button actually does.
    outputBtn.textContent = '↗';
    outputBtn.title = 'View live output';
    outputBtn.setAttribute('aria-label', 'View live output');

    const pauseBtn = document.createElement('button');
    pauseBtn.type = 'button';
    pauseBtn.className = 'btn-icon exec-row-pause-btn';

    const cancelBtn = document.createElement('button');
    cancelBtn.type = 'button';
    cancelBtn.className = 'btn-icon exec-row-cancel-btn';
    cancelBtn.textContent = '✖';
    cancelBtn.title = 'Cancel execution';
    cancelBtn.setAttribute('aria-label', 'Cancel execution');

    pidSeam.append(pidLabel, outputBtn, pauseBtn, cancelBtn);
    row.appendChild(pidSeam);
  }
  updateExecRow(row, exec, kind);
  return row;
}

function openRunningOutput(exec: TaskExecution): void {
  const title = (exec.task_name ?? 'task') + ' — run #' + exec.id;
  openOutputModal(exec.id, title);
}

function requestProcessToggle(btn: HTMLButtonElement, execId: number, suspended: boolean): void {
  btn.disabled = true;
  const request = suspended ? api.resumeExecution(execId) : api.pauseExecution(execId);
  finishProcessToggle(request, btn);
}

function finishProcessToggle(request: Promise<unknown>, btn: HTMLButtonElement): void {
  request.then(reenableProcessButton(btn), reenableProcessButton(btn));
}

function reenableProcessButton(btn: HTMLButtonElement): () => void {
  function reenable(): void {
    btn.disabled = false;
    void refreshAll();
  }
  return reenable;
}

function wireProcessToggle(btn: HTMLButtonElement, exec: TaskExecution): void {
  const suspended = !!exec.suspended;
  btn.textContent = suspended ? '▶' : '⏸';
  btn.title = suspended ? 'Resume process' : 'Pause process';
  btn.setAttribute('aria-label', btn.title);
  btn.onclick = makeProcessToggleHandler(btn, exec.id, suspended);
}

function makeProcessToggleHandler(btn: HTMLButtonElement, execId: number, suspended: boolean): () => void {
  function handleClick(): void {
    requestProcessToggle(btn, execId, suspended);
  }
  return handleClick;
}

function requestCancel(btn: HTMLButtonElement, execId: number): void {
  btn.disabled = true;
  api.cancelExecution(execId).then(reenableCancelButton(btn), reenableCancelButton(btn));
}

function reenableCancelButton(btn: HTMLButtonElement): () => void {
  function reenable(): void {
    btn.disabled = false;
    void refreshAll();
  }
  return reenable;
}

function wireCancelButton(btn: HTMLButtonElement, exec: TaskExecution): void {
  btn.onclick = makeCancelHandler(btn, exec.id);
}

function makeCancelHandler(btn: HTMLButtonElement, execId: number): () => void {
  function handleClick(): void {
    requestCancel(btn, execId);
  }
  return handleClick;
}

function wireOutputButton(btn: HTMLButtonElement, exec: TaskExecution): void {
  btn.onclick = makeOutputHandler(exec);
}

function makeOutputHandler(exec: TaskExecution): () => void {
  function handleClick(): void {
    openRunningOutput(exec);
  }
  return handleClick;
}

function updateExecRow(row: HTMLElement, exec: TaskExecution, kind: 'running' | 'ran'): void {
  const name = row.querySelector<HTMLElement>('.exec-row-name');
  if (name) {
    name.textContent = exec.task_name ?? '(unknown task)';
    const taskName = exec.task_name;
    const openDetail = (): void => {
      if (!taskName) return;
      openTaskRoute(taskName);
    };
    name.onclick = openDetail;
    name.onkeydown = (e) => {
      if (e.key === 'Enter' || e.key === ' ') {
        e.preventDefault();
        openDetail();
      }
    };
  }
  const badge = row.querySelector<HTMLElement>('.badge');
  if (badge) renderStatusBadge(badge, exec.status, exec.suspended);
  const meta = row.querySelector<HTMLElement>('.exec-row-meta');
  if (meta) {
    if (exec.duration_ms !== undefined && exec.duration_ms !== null) {
      meta.textContent = (exec.duration_ms / 1000).toFixed(1) + 's';
    } else if (exec.suspended) {
      // A paused process's wall-clock time keeps passing even though it's
      // doing nothing — a ticking counter here would make a genuinely
      // frozen process look like pause had no effect. Say so plainly
      // instead (the badge already shows ⏸ too).
      meta.textContent = 'paused';
    } else if (exec.started_at) {
      // "running…" said nothing useful — show live elapsed time instead.
      // The board's 1s tick (render()) re-runs this via patchList's update
      // callback, so this counts up on its own.
      meta.textContent = fmtElapsed(exec.started_at);
    } else {
      meta.textContent = '';
    }
  }

  if (kind === 'running') {
    const pidLabel = row.querySelector<HTMLElement>('.exec-row-pid');
    if (pidLabel) pidLabel.textContent = exec.pid !== undefined ? 'pid ' + exec.pid : '';

    const outputBtn = row.querySelector<HTMLButtonElement>('.exec-row-output-btn');
    if (outputBtn) wireOutputButton(outputBtn, exec);

    const pauseBtn = row.querySelector<HTMLButtonElement>('.exec-row-pause-btn');
    if (pauseBtn) wireProcessToggle(pauseBtn, exec);

    const cancelBtn = row.querySelector<HTMLButtonElement>('.exec-row-cancel-btn');
    if (cancelBtn) wireCancelButton(cancelBtn, exec);
  }
}

function createTaskRow(task: Task, pending: TaskExecution | null): HTMLElement {
  const row = document.createElement('div');
  row.className = 'task-row';
  row.draggable = true;

  const name = document.createElement('span');
  name.className = 'task-row-name task-row-name-clickable';
  name.setAttribute('role', 'button');
  name.tabIndex = 0;
  const openDetail = (): void => {
    openTaskRoute(task.name);
  };
  name.addEventListener('click', openDetail);
  name.addEventListener('keydown', (e) => {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault();
      openDetail();
    }
  });
  const status = document.createElement('span');
  status.className = 'task-row-status';
  const upNextBtn = document.createElement('button');
  upNextBtn.type = 'button';
  upNextBtn.className = 'btn btn-secondary btn-sm task-row-upnext';
  upNextBtn.textContent = 'Up next';

  row.append(name, status, upNextBtn);

  row.addEventListener('dragstart', (e) => {
    if (!e.dataTransfer) return;
    e.dataTransfer.effectAllowed = 'move';
    e.dataTransfer.setData('text/plain', JSON.stringify({ task: task.name, lane: task.lane_name }));
    row.classList.add('dragging');
  });
  row.addEventListener('dragend', () => row.classList.remove('dragging'));

  updateTaskRow(row, task, pending);
  return row;
}

function updateTaskRow(row: HTMLElement, task: Task, pending: TaskExecution | null): void {
  row.setAttribute('data-task', task.name);
  row.classList.toggle('task-row-disabled', !task.enabled || task.paused);

  const name = row.querySelector<HTMLElement>('.task-row-name');
  if (name) name.textContent = task.name;

  const status = row.querySelector<HTMLElement>('.task-row-status');
  if (status) {
    if (!task.enabled) {
      status.textContent = 'disabled';
    } else if (task.paused) {
      status.textContent = 'paused';
    } else if (pending) {
      status.textContent = 'queued';
    } else if (task.repeat) {
      status.textContent = cooldownLabel(task);
    } else {
      status.textContent = 'ready';
    }
  }

  const btn = row.querySelector<HTMLButtonElement>('.task-row-upnext');
  if (btn) {
    btn.disabled = !task.enabled || task.paused;
    btn.onclick = () => void api.upNext(task.name).then(() => refreshAll());
  }
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
