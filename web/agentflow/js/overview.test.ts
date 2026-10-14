import { test, run, eq, ok, installFakeDom, FakeEl, findAll, asEl } from './testkit';
import { Overview, buttonsFor, progressText, type OverviewDeps } from './overview';
import { dispatch, subscribe, backoffMs } from './events';
import type { ProjectView, RunOp } from './api';

installFakeDom();

const flush = (): Promise<void> => new Promise((r) => setTimeout(r, 0));

function proj(slug: string, over: Partial<ProjectView> = {}): ProjectView {
  return {
    slug, repo_path: '/r/' + slug, model_scope: 'local', feedback_cmds: [], limits: {}, created: 0, updated: 0,
    run_state: 'stopped', remembered_run_state: 'stopped',
    effort: { id: 1, project: slug, title: 'Effort ' + slug, status: 'running', run_state: 'stopped', integration_branch: '', base_commit: '', yolo: false, deliberate: false, created: 0, updated: 0 },
    progress: { total: 4, passed: 1, running: 1, ready: 2 },
    current: [{ task: 't1', attempt: 2, stage: 9, role: 'worker', backend: 'bk', model: 'm1', state: 'running' }],
    ...over,
  };
}

function harness(initial: ProjectView[]) {
  const host = new FakeEl('DIV');
  const calls: string[] = [];
  let rows = initial;
  let listCalls = 0;
  const deps: OverviewDeps = {
    list: async () => { listCalls++; return rows.map((r) => ({ ...r })); },
    run: async (slug: string, op: RunOp) => {
      calls.push(`${slug}:${op}`);
      return proj(slug, { run_state: op === 'stop' ? 'stopped' : 'running' });
    },
    confirm: async (m: string) => { calls.push('confirm:' + m); return true; },
    open: (s: string) => { calls.push('open:' + s); },
  };
  const ov = new Overview(asEl(host), deps);
  return { host, ov, calls, setRows: (r: ProjectView[]) => { rows = r; }, listCalls: () => listCalls };
}

test('overview shows every project with run state, effort, progress, stage and backend (PJ5)', async () => {
  const t = harness([proj('alpha'), proj('beta', { run_state: 'paused', current: [], progress: {} })]);
  const stop = t.ov.mount();
  await flush();
  const text = t.host.textContent;
  ok(text.includes('alpha') && text.includes('beta'), 'both projects');
  ok(text.includes('Effort alpha · running'), 'active effort');
  ok(text.includes('25% — passed 1/4'), 'progress: ' + text);
  ok(text.includes('worker · t1 (attempt 2) · bk/m1 · running'), 'current stage and backend');
  ok(text.includes('no queue yet'), 'project with no queue');
  eq(findAll(t.host, (e) => e.className.startsWith('status-chip')).map((e) => e.textContent), ['stopped', 'paused']);
  stop();
});

test('RC5: a project.run_state event re-renders the chip at once and refetches', async () => {
  const t = harness([proj('alpha')]);
  const stop = t.ov.mount();
  await flush();
  const before = t.listCalls();
  t.setRows([proj('alpha', { run_state: 'running', progress: { total: 4, passed: 2, ready: 2 }, current: [] })]);
  dispatch(JSON.stringify({ type: 'project.run_state', project: 'alpha', effort: 1, state: 'running', action: 'start', actor: 'u' }));
  ok(findAll(t.host, (e) => e.className.startsWith('status-chip'))[0].className.includes('in-progress'), 'chip changed synchronously');
  await flush();
  ok(t.listCalls() > before, 'refetched');
  ok(t.host.textContent.includes('50% — passed 2/4'), 'new progress shown');
  ok(t.host.textContent.includes('no stage running'), 'stage cleared');
  stop();
});

test('state.changed refetches and a new project appears; project.delete removes it', async () => {
  const t = harness([proj('alpha')]);
  const stop = t.ov.mount();
  await flush();
  t.setRows([proj('alpha'), proj('gamma')]);
  dispatch(JSON.stringify({ type: 'state.changed' }));
  await flush();
  ok(t.host.textContent.includes('gamma'), 'gamma appeared');
  t.setRows([proj('gamma')]);
  dispatch(JSON.stringify({ type: 'project.delete', project: 'alpha' }));
  await flush();
  ok(!t.host.textContent.includes('alpha'), 'alpha gone');
  stop();
});

test('a snapshot with an unknown project refetches; one with a known project updates state', async () => {
  const t = harness([proj('alpha')]);
  const stop = t.ov.mount();
  await flush();
  dispatch(JSON.stringify({ type: 'snapshot', projects: [{ project: 'alpha', state: 'paused', effort: 1, remembered: '' }] }));
  eq(findAll(t.host, (e) => e.className.startsWith('status-chip'))[0].textContent, 'paused');
  const before = t.listCalls();
  dispatch(JSON.stringify({ type: 'snapshot', projects: [{ project: 'alpha', state: 'paused', effort: 1, remembered: '' }, { project: 'zed', state: 'stopped', effort: 0, remembered: '' }] }));
  await flush();
  ok(t.listCalls() > before, 'unknown project triggers refetch');
  stop();
});

test('after dispose the overview ignores events', async () => {
  const t = harness([proj('alpha')]);
  const stop = t.ov.mount();
  await flush();
  stop();
  const before = t.listCalls();
  dispatch(JSON.stringify({ type: 'state.changed' }));
  await flush();
  eq(t.listCalls(), before);
});

test('buttons follow the run state; stop asks for confirmation; start does not', async () => {
  eq(buttonsFor('stopped').map((b) => b.op), ['start', 'run-one']);
  eq(buttonsFor('running').map((b) => b.op), ['pause', 'stop']);
  eq(buttonsFor('pausing').map((b) => b.op), ['start', 'stop']);
  eq(buttonsFor('paused').map((b) => b.op), ['start', 'run-one', 'stop']);
  const t = harness([proj('alpha', { run_state: 'running' })]);
  const stop = t.ov.mount();
  await flush();
  const btn = (op: string): FakeEl => findAll(t.host, (e) => e.getAttribute('data-op') === op)[0];
  btn('stop').click();
  await flush();
  ok(t.calls[0].startsWith('confirm:') && t.calls[1] === 'alpha:stop', 'stop confirmed then sent: ' + t.calls.join('|'));
  ok(t.host.textContent.includes('Start'), 'row re-rendered from the response (stopped)');
  btn('start').click();
  await flush();
  eq(t.calls.slice(2), ['alpha:start']);
  findAll(t.host, (e) => e.getAttribute('data-open') === 'alpha')[0].click();
  eq(t.calls[t.calls.length - 1], 'open:alpha');
  stop();
});

test('a hostile project slug and effort title render as literal text', async () => {
  const evil = '<img src=x onerror=alert(1)>';
  const t = harness([proj(evil, { effort: { ...proj('x').effort!, title: '<script>' } })]);
  const stop = t.ov.mount();
  await flush();
  ok(t.host.textContent.includes(evil) && t.host.textContent.includes('<script>'), 'literal');
  ok(findAll(t.host, (e) => e.tagName === 'IMG' || e.tagName === 'SCRIPT').length === 0, 'no injected elements');
  stop();
});

test('progressText and backoff', () => {
  eq(progressText({ total: 0 }).text, 'no queue yet');
  eq(progressText({ total: 3, passed: 1, overridden: 1, needs_input: 1 }), { pct: 67, text: '67% — passed 1/3 · needs input 1 · overridden 1' });
  eq([0, 1, 2, 5, 9].map(backoffMs), [1000, 2000, 4000, 30000, 30000]);
  const off = subscribe(() => {});
  off();
});

void run('agentflow-overview');
