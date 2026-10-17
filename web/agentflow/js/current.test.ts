import { test, run, eq } from './testkit';
import { subTabBlock } from './current';
import type { EffortView } from './api';

const ef = (over: Partial<EffortView> = {}): EffortView => ({
  id: 1, project: 'p', title: 't', status: 'draft', run_state: 'stopped', integration_branch: '', base_commit: '', yolo: false, deliberate: false, created: 0, updated: 0,
  frd: { latest: 0, approved: 0 }, plan: { latest: 0, approved: 0 }, queue: null, ...over,
});

test('with no effort only the FRD tab is open', () => {
  eq(subTabBlock('frd', undefined), '');
  for (const id of ['plan', 'queue', 'board', 'history']) eq(subTabBlock(id, undefined) !== '', true, id);
});

test('Plan opens once an FRD is saved; Queue once the plan is approved; Board only once the queue is approved', () => {
  const planning = ef({ status: 'planning', frd: { latest: 2, approved: 2 }, plan: { latest: 1, approved: 0 } });
  eq(subTabBlock('plan', ef()) !== '', true);
  eq(subTabBlock('plan', planning), '');
  eq(subTabBlock('queue', planning) !== '', true);
  eq(subTabBlock('board', planning) !== '', true);
  const compiling = ef({ status: 'compiling', frd: { latest: 2, approved: 2 }, plan: { latest: 1, approved: 1 } });
  eq(subTabBlock('queue', compiling), '');
  eq(subTabBlock('board', compiling) !== '', true);
  const draft = ef({ status: 'queue_draft', frd: { latest: 2, approved: 2 }, plan: { latest: 1, approved: 1 }, queue: { revision: 1, hash: 'h', status: 'draft' } });
  eq(subTabBlock('queue', draft), '');
  eq(subTabBlock('board', draft) !== '', true);
  const approved = ef({ status: 'running', frd: { latest: 2, approved: 2 }, plan: { latest: 1, approved: 1 }, queue: { revision: 1, hash: 'h', status: 'approved' } });
  for (const id of ['frd', 'plan', 'queue', 'board', 'history']) eq(subTabBlock(id, approved), '', id);
});

test('the reason names what to do', () => {
  eq(subTabBlock('board', ef({ status: 'planning', frd: { latest: 1, approved: 1 } })), 'Available once the queue is approved');
});

void run('agentflow-current');
