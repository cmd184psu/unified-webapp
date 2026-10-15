import { test, run, eq, ok, installFakeDom, FakeEl, asEl, byAttr, flush } from './testkit';
import { Activity, describe, type ActivityDeps } from './activity';
import { pollSettings } from './events';
import type { DraftState, EffortView, TaskView } from './api';

installFakeDom();
pollSettings.ms = 20;

const effort = (over: Partial<EffortView> = {}): EffortView => ({
  id: 3, project: 'p1', title: 'T', status: 'draft', run_state: 'stopped', integration_branch: '', base_commit: '', yolo: false,
  deliberate: false, created: 0, updated: 1000, frd: { latest: 0, approved: 0 }, plan: { latest: 0, approved: 0 }, queue: null, ...over,
});
const task = (key: string, over: Partial<TaskView> = {}): TaskView =>
  ({ key, title: key, phase: '', prompt: '', acceptance: [], files_touched: [], depends_on: [], id: 1, queue: 1, waiting_reason: '', status: 'queued', halted: false, attempts: 0, current: null, ...over } as TaskView);

test('AV1: every status says what is going on and which tab shows it', () => {
  const running: DraftState = { state: 'running', purpose: 'draft', started: 5, error: '' };
  const cases: Array<[EffortView, DraftState | null, TaskView[], string, string, string]> = [
    [effort(), running, [], 'busy', 'Drafting the FRD…', 'frd'],
    [effort(), { ...running, purpose: 'split' }, [], 'busy', 'Splitting the FRD into parts…', 'frd'],
    [effort(), null, [], 'wait', '#3: describe what to build', 'frd'],
    [effort({ frd: { latest: 1, approved: 0 } }), null, [], 'wait', '#3: review the FRD and approve it', 'frd'],
    [effort({ status: 'planning' }), null, [], 'busy', '#3: planning…', 'plan'],
    [effort({ status: 'plan_ready' }), null, [], 'wait', '#3: review the plan and approve it', 'plan'],
    [effort({ status: 'compiling' }), null, [], 'busy', '#3: turning the plan into tasks…', 'queue'],
    [effort({ status: 'queue_draft' }), null, [], 'wait', '#3: review the queue and approve it', 'queue'],
    [effort({ status: 'queue_approved' }), null, [], 'wait', '#3: queue approved; start the run', 'board'],
    [effort({ status: 'complete' }), null, [], 'rest', '#3: complete; ready to merge', 'board'],
  ];
  for (const [ef, d, ts, kind, text, tab] of cases) {
    const l = describe(ef, d, ts);
    eq([l.kind, l.text, l.tab], [kind, text, tab]);
  }
});

test('AV2: a running effort names the task, stage and model, and a halted task asks for attention', () => {
  const ts = [task('T001', { status: 'passed' }), task('T002', { status: 'working', current: { task: 'T002', attempt: 1, stage: 4, role: 'worker', backend: 'b', model: 'big-pickle', state: 'running' } })];
  eq(describe(effort({ status: 'running' }), null, ts).text, '#3: T002 worker on big-pickle (1/2 done)');
  const halted = describe(effort({ status: 'running' }), null, [task('T001', { halted: true })]);
  eq([halted.kind, halted.text], ['wait', '#3: T001 needs attention']);
});

test('AV3: the strip shows the line, ticks a clock for timed work, hides with no effort, and the arrow opens the tab', async () => {
  let efforts: EffortView[] = [effort({ status: 'planning' })];
  const deps: ActivityDeps = { efforts: async () => efforts, draftState: async () => ({ state: 'idle', purpose: 'draft', error: '' }), tasks: async () => [] };
  const opened: string[] = [];
  const el = new FakeEl('DIV');
  const a = new Activity(asEl(el), (t) => opened.push(t), deps);
  const stop = a.mount('p1');
  await flush();
  eq(el.getAttribute('data-kind'), 'busy');
  eq(byAttr(el, 'data-role', 'activity-text')[0].textContent, '#3: planning…');
  ok(byAttr(el, 'data-role', 'elapsed').length === 1, 'clock');
  byAttr(el, 'data-action', 'open-activity')[0].click();
  eq(opened, ['plan']);
  // the poll picks up a change the events missed
  efforts = [effort({ status: 'plan_ready' })];
  await new Promise((r) => setTimeout(r, 80));
  await flush();
  eq(byAttr(el, 'data-role', 'activity-text')[0].textContent, '#3: review the plan and approve it');
  eq(el.getAttribute('data-kind'), 'wait');
  efforts = [];
  a.setProject('p2');
  await flush();
  ok(el.hasAttribute('hidden'), 'hidden with no effort');
  stop();
});

void run('agentflow-activity');
