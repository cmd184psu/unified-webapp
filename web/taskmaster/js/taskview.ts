// taskview.ts — the split task view (`#task/<name>`), replacing the old
// task-detail modal.
//
// Layout: a ~30% left pane showing ONLY that task's lane (the same lane
// column markup as the full board, via board.ts's laneFilter option, so
// drag/drop, per-lane pause, and the playlist ordering all keep working
// unchanged) and a ~70% right pane with the task's detail (command/meta,
// Output pane, run history, metrics — taskdetail.ts). Both panes share the
// same LiveController, so both stay live without polling. "Back to board"
// (in the detail pane) simply navigates back to `#board`.

import { api, Capabilities } from './api.js';
import { LiveController } from './ui/live.js';
import { mountBoard } from './board.js';
import { mountTaskDetail } from './taskdetail.js';

/** Mounts the split task view into `container`. Returns a cleanup function. */
export function mountTaskView(
  container: HTMLElement,
  live: LiveController,
  caps: Capabilities,
  taskName: string
): () => void {
  container.textContent = '';

  const wrap = document.createElement('div');
  wrap.className = 'split-view';
  const left = document.createElement('div');
  left.className = 'split-left';
  const right = document.createElement('div');
  right.className = 'split-right';
  wrap.append(left, right);
  container.appendChild(wrap);

  right.textContent = 'Loading task…';

  let unmountLeft: (() => void) | null = null;
  let unmountRight: (() => void) | null = null;
  let cancelled = false;

  const goBack = (): void => {
    window.location.hash = '#board';
  };

  void api
    .getTask(taskName)
    .then((task) => {
      if (cancelled) return;
      unmountLeft = mountBoard(left, live, caps, { laneFilter: task.lane_name });
      right.textContent = '';
      unmountRight = mountTaskDetail(right, live, task, goBack);
    })
    .catch(() => {
      if (cancelled) return;
      right.textContent = '';
      const err = document.createElement('div');
      err.className = 'empty-state';
      err.textContent = 'Task not found.';
      right.appendChild(err);
      const back = document.createElement('button');
      back.type = 'button';
      back.className = 'btn btn-secondary';
      back.textContent = '← Back to board';
      back.addEventListener('click', goBack);
      right.appendChild(back);
    });

  return () => {
    cancelled = true;
    if (unmountLeft) unmountLeft();
    if (unmountRight) unmountRight();
  };
}
