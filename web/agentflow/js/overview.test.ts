import { test, run, eq, ok, installFakeDom, FakeEl, findAll, asEl, byAttr } from './testkit';
import { Overview, buttonsFor, progressText, type OverviewDeps } from './overview';
import { dispatch, subscribe, backoffMs, pollSettings } from './events';
import { HealthBanner, healthMessage } from './health';
import type { Effort, Health, ProjectView, RunOp } from './api';

installFakeDom();
pollSettings.ms = 20;

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
  const efforts: Effort[] = [];
  const deps: OverviewDeps = {
    efforts: async () => efforts as never,
    gitCommands: async () => ({ commands: ['git merge --no-ff agentflow/effort-3'], branch: 'agentflow/effort-3', files: 2 }),
    copy: (t: string) => { calls.push('copy:' + t); },
    list: async () => { listCalls++; return rows.map((r) => ({ ...r })); },
    run: async (slug: string, op: RunOp) => {
      calls.push(`${slug}:${op}`);
      return proj(slug, { run_state: op === 'stop' ? 'stopped' : 'running' });
    },
    confirm: async (m: string) => { calls.push('confirm:' + m); return true; },
    open: (s: string) => { calls.push('open:' + s); },
    unregister: async (slug: string) => { calls.push('unregister:' + slug); return true; },
  };
  const ov = new Overview(asEl(host), deps);
  return { host, ov, calls, efforts, setRows: (r: ProjectView[]) => { rows = r; }, listCalls: () => listCalls };
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

test('OV1: while an effort is only being drafted or planned, the row shows no "stopped" chip and no Start', async () => {
  const planning = proj('alpha', { effort: { ...proj('x').effort as NonNullable<ProjectView['effort']>, status: 'planning' }, current: [], progress: {} });
  const ready = proj('beta', { effort: { ...proj('x').effort as NonNullable<ProjectView['effort']>, status: 'queue_approved' }, current: [], progress: {} });
  const t = harness([planning, ready]);
  const stop = t.ov.mount();
  await flush();
  const chips = (slug: string): string[] => findAll(byAttr(t.host, 'data-project', slug)[0], (e) => e.className.startsWith('status-chip')).map((e) => e.textContent);
  eq(chips('alpha'), []);
  eq(chips('beta'), ['stopped']);
  eq(findAll(byAttr(t.host, 'data-project', 'alpha')[0], (e) => e.getAttribute('data-op') === 'start').length, 0);
  eq(findAll(byAttr(t.host, 'data-project', 'beta')[0], (e) => e.getAttribute('data-op') === 'start').length, 1);
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
  ok(btn('start') !== undefined, 'row re-rendered from the response (stopped)');
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

test('F1: the overview polls while a project effort is running and stops once nothing is transient', async () => {
  const t = harness([proj('alpha')]);
  const stop = t.ov.mount();
  await flush();
  const first = t.listCalls();
  await new Promise((r) => setTimeout(r, 120));
  ok(t.listCalls() > first + 1, 'polled without any event');
  t.setRows([proj('alpha', { effort: { ...proj('x').effort!, status: 'queue_draft' } })]);
  await new Promise((r) => setTimeout(r, 80));
  const settled = t.listCalls();
  await new Promise((r) => setTimeout(r, 120));
  eq(t.listCalls(), settled);
  stop();
});

test('F3: a project whose newest effort is complete shows the Ready to merge card with copyable commands', async () => {
  const t = harness([proj('alpha', { effort: null, current: [], progress: {} })]);
  t.efforts.push({ ...proj('x').effort!, id: 3, title: 'Done thing', status: 'complete', integration_branch: 'agentflow/effort-3' });
  const stop = t.ov.mount();
  await flush();
  const card = findAll(t.host, (e) => e.getAttribute('data-role') === 'git-commands')[0];
  ok(card.textContent.includes('Done thing') && card.textContent.includes('agentflow/effort-3') && card.textContent.includes('Nothing was merged into main'), card.textContent);
  ok(card.textContent.includes('2 files changed'), 'optional file count');
  findAll(card, (e) => e.getAttribute('data-action') === 'copy-command')[0].click();
  eq(t.calls.filter((c) => c.startsWith('copy:')), ['copy:git merge --no-ff agentflow/effort-3']);
  stop();
});

test('F4: the health banner is hidden when ok and shows the server message when degraded, then clears on recovery', async () => {
  let hl: Health = { status: 'degraded', opencode: { status: 'unavailable', error: 'opencode not found in PATH' } };
  const host = new FakeEl('DIV');
  const b = new HealthBanner(asEl(host), { health: async () => hl });
  const stop = b.mount();
  await flush();
  ok(host.textContent.includes('opencode not found in PATH') && !host.hasAttribute('hidden'), 'banner shown: ' + host.textContent);
  hl = { status: 'ok', opencode: { status: 'ok', version: '1.0' } };
  await new Promise((r) => setTimeout(r, 80));
  eq(host.textContent, '');
  ok(host.hasAttribute('hidden'), 'hidden when ok');
  eq(healthMessage(hl), '');
  stop();
});


test('a row offers Unregister only when its run is stopped with no active effort', async () => {
  const t = harness([proj('busy'), proj('idle', { effort: null, current: [] })]);
  const stop = t.ov.mount();
  await flush();
  const trash = findAll(t.host, (e) => e.getAttribute('data-action') === 'unregister-project') as unknown as Array<{ disabled: boolean; getAttribute(k: string): string | null; click(): void }>;
  eq(trash.length, 2);
  eq(trash[0].disabled, true);
  ok((trash[0].getAttribute('aria-label') ?? '').includes('Finish or abandon the active effort first'), 'reason on the disabled one');
  eq(trash[1].disabled, false);
  t.setRows([proj('busy')]);
  trash[1].click();
  await flush();
  await flush();
  eq(t.calls.filter((c) => c.startsWith('unregister:')), ['unregister:idle']);
  ok(!t.host.textContent.includes('idle'), 'row gone after a refresh');
  stop();
});

void run('agentflow-overview');
