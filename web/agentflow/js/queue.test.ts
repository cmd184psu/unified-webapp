import { test, run, eq, ok, installFakeDom, FakeEl, asEl, byAttr, button, flush, findAll } from './testkit';
import { QueueApproval, type QueueDeps } from './queue';
import { ApiError, type EffortView, type QueueView } from './api';

installFakeDom();

const effort = (over: Partial<EffortView> = {}): EffortView => ({
  id: 1, project: 'p1', title: 'Alpha', status: 'queue_draft', run_state: 'stopped', integration_branch: '', base_commit: '', yolo: false,
  deliberate: false, created: 0, updated: 0, frd: { latest: 1, approved: 1 }, plan: { latest: 1, approved: 1 }, queue: null, ...over,
});

const queue = (hash: string, over: Partial<QueueView> = {}): QueueView => ({
  effort: 1, effort_status: 'queue_draft', revision: 1, hash, status: 'draft',
  tasks: [
    { key: 'T001', phase: 'build', title: 'First', prompt: 'do <b>one</b>', acceptance: ['a1', 'a2'], files_touched: ['x.go', 'y.go'], depends_on: [], id: 1, queue: 1, waiting_reason: '' },
    { key: 'T002', phase: 'build', title: 'Second', prompt: 'do two', acceptance: ['b1'], files_touched: ['z.go'], depends_on: ['T001'], id: 2, queue: 1, waiting_reason: 'waiting for T001' },
  ], ...over,
});

function harness(o: { effort?: EffortView; queue?: QueueView | null; approve?: QueueDeps['approveQueue']; confirm?: boolean } = {}) {
  const host = new FakeEl('DIV');
  const log: string[] = [];
  let q: QueueView | null = o.queue === undefined ? queue('hash-1') : o.queue;
  const deps: QueueDeps = {
    efforts: async () => [o.effort ?? effort()],
    queue: async () => {
      if (!q) throw new ApiError(404, 'no queue');
      return q;
    },
    compile: async () => { log.push('compile'); return effort(); },
    approveQueue: o.approve ?? (async (_s, _id, hash) => { log.push('approve:' + hash); return q as QueueView; }),
    confirm: async (m) => { log.push('confirm:' + m); return o.confirm ?? true; },
    notify: (m) => { log.push('notify:' + m); },
  };
  const v = new QueueApproval(asEl(host), 'p1', deps);
  return { host, v, log, setQueue: (x: QueueView | null) => { q = x; } };
}

test('QA3: the approval view lists every task with prompt, files_touched, acceptance and dependencies', async () => {
  const t = harness();
  const stop = t.v.mount();
  await flush();
  for (const key of ['T001', 'T002']) ok(byAttr(t.host, 'data-task', key).length === 1, key + ' listed');
  const first = byAttr(t.host, 'data-task', 'T001')[0].textContent;
  ok(first.includes('do <b>one</b>') && first.includes('a1') && first.includes('a2') && first.includes('x.go') && first.includes('y.go'), 'first task: ' + first);
  const second = byAttr(t.host, 'data-task', 'T002')[0].textContent;
  ok(second.includes('T001') && second.includes('waiting for T001') && second.includes('z.go'), 'second task');
  ok(findAll(t.host, (e) => e.tagName === 'B').length === 0, 'prompt is text, not markup');
  ok(byAttr(t.host, 'data-role', 'queue-header')[0].textContent.includes('2 tasks'), 'count');
  stop();
});

test('QA3: approve whole queue sends the hash of the queue on screen, after a confirmation', async () => {
  const t = harness({ confirm: false });
  const stop = t.v.mount();
  await flush();
  button(t.host, 'Approve whole queue').click();
  await flush();
  eq(t.log.filter((l) => l.startsWith('approve:')), []);
  stop();
  const ok1 = harness();
  const stop2 = ok1.v.mount();
  await flush();
  button(ok1.host, 'Approve whole queue').click();
  await flush();
  eq(ok1.log.filter((l) => l.startsWith('approve:')), ['approve:hash-1']);
  ok(ok1.log.some((l) => l.startsWith('confirm:Approve all 2 tasks of revision 1')), 'confirm text');
  stop2();
});

test('QA2 (UI): a 409 means the queue changed; nothing is approved and the new queue is shown for review', async () => {
  const t = harness({
    approve: async () => { throw new ApiError(409, 'queue hash mismatch'); },
  });
  const stop = t.v.mount();
  await flush();
  t.setQueue(queue('hash-2', { revision: 2 }));
  button(t.host, 'Approve whole queue').click();
  await flush();
  ok(t.host.textContent.includes('The queue changed while you were reviewing it'), 'explained');
  ok(byAttr(t.host, 'data-role', 'queue-hash')[0].textContent === 'hash hash-2', 'reloaded the new queue: ' + byAttr(t.host, 'data-role', 'queue-hash')[0].textContent);
  ok(t.host.textContent.includes('revision 2'), 'new revision shown');
  stop();
});

test('an approved queue shows who approved it and offers no approve button', async () => {
  const t = harness({ queue: queue('h', { status: 'approved', approved_by: 'alice' }), effort: effort({ status: 'queue_approved' }) });
  const stop = t.v.mount();
  await flush();
  ok(byAttr(t.host, 'data-role', 'queue-header')[0].textContent.includes('approved by alice'), 'approver');
  eq(findAll(t.host, (e) => e.tagName === 'BUTTON' && e.textContent === 'Approve whole queue').length, 0);
  stop();
});

test('with an approved plan and no queue the view offers to compile', async () => {
  const t = harness({ queue: null, effort: effort({ status: 'plan_approved' }) });
  const stop = t.v.mount();
  await flush();
  ok(t.host.textContent.includes('No queue yet'), 'empty state');
  button(t.host, 'Compile plan').click();
  await flush();
  eq(t.log.filter((l) => l === 'compile'), ['compile']);
  stop();
});

void run('agentflow-queue');
