import { test, run, eq, ok, installFakeDom, FakeEl, asEl, byField, byAttr, button, flush, findAll } from './testkit';
import { BackendsView, isPaid, isPrivateHost, lastForRoles, unconsented, type BackendsDeps } from './backends';
import { moveItem, dropIndex } from './candidates';
import { dispatch } from './events';
import { ApiError, type Backend, type BackendInput, type BackendResult, type Candidate, type ModelStat, type ProjectView } from './api';

installFakeDom();

const be = (name: string, over: Partial<Backend> = {}): Backend => ({
  name, host: 'http://127.0.0.1:11434', api_type: 'ollama', hosted: false, paid: false, slots: 2, retired: false,
  models: [{ model: 'm1', paid: false, max_concurrent: 0 }, { model: 'm2-paid', paid: true, max_concurrent: 2 }], ...over,
});
const cand = (role: string, position: number, backend: string, model: string, paid = false): Candidate =>
  ({ scope: 'global', role, position, backend, model, paid });
const project = (slug: string): ProjectView => ({
  slug, repo_path: '/r', model_scope: 'local', feedback_cmds: [], limits: {}, created: 0, updated: 0,
  run_state: 'stopped', remembered_run_state: 'stopped', effort: null, progress: {}, current: [],
});
const result = (b: Backend, warnings: string[] = []): BackendResult => ({ backend: b, warnings });

interface State {
  backends: Backend[];
  candidates: Candidate[];
  projects: ProjectView[];
  stats: ModelStat[];
}

function harness(init: Partial<State> = {}, over: Partial<BackendsDeps> = {}) {
  const st: State = { backends: [], candidates: [], projects: [], stats: [], ...init };
  const host = new FakeEl('DIV');
  const log: string[] = [];
  const bodies: Record<string, unknown> = {};
  const deps: BackendsDeps = {
    backends: async () => st.backends.map((b) => ({ ...b })),
    candidates: async () => st.candidates.map((c) => ({ ...c })),
    projects: async () => st.projects,
    modelStats: async () => st.stats,
    settings: async () => ({ worker_timeout: '30m0s', max_attempts: 5 }),
    limits: async () => ({ limits: { max_attempts: '9' }, effective: {} }),
    putLimits: async (slug, limits) => { log.push('limits:' + slug); bodies.limits = limits; return { limits: {}, effective: {} } as never; },
    probeBackend: async () => ({ models: ['m-a', 'm-b'] }),
    createBackend: async (b: BackendInput) => { log.push('create:' + b.name); bodies.create = b; return result(be(b.name)); },
    patchBackend: async (name, b) => { log.push('patch:' + name); bodies.patch = b; return result(be(name)); },
    retireBackend: async (name, cancel) => { log.push(`retire:${name}:${cancel}`); return result(be(name, { retired: true }), ['retired the last backend for worker; project p1 is running']); },
    enableBackend: async (name) => { log.push('enable:' + name); return result(be(name)); },
    putCandidates: async (role, list) => {
      log.push('cands:' + role);
      bodies.cands = list;
      st.candidates = [...st.candidates.filter((c) => c.role !== role), ...list.map((c, i) => cand(role, i, c.backend, c.model))];
      return st.candidates;
    },
    notify: (m) => { log.push('notify:' + m); },
    ...over,
  };
  const view = new BackendsView(asEl(host), deps);
  return { host, view, log, bodies, st };
}

const field = (host: FakeEl, name: string): FakeEl => byField(host, name);
const errorOf = (host: FakeEl, name: string): string =>
  findAll(field(host, name).parentNode as FakeEl, (e) => e.className === 'field-error')[0].textContent;

test('CI1: with no backends the tab prompts to add one', async () => {
  const t = harness();
  const stop = t.view.mount();
  await flush();
  ok(t.host.textContent.includes('No backends configured'), 'empty state');
  eq(byAttr(t.host, 'data-role', 'no-backends').length, 1);
  button(t.host, 'Add backend').click();
  ok(byAttr(t.host, 'data-form', 'add').length === 1, 'add form opened');
  stop();
});

test('probing the host turns the model box into a picker of the names it reported', async () => {
  const t = harness({});
  const stop = t.view.mount();
  await flush();
  button(t.host, 'Add backend').click();
  field(t.host, 'host').value = 'http://127.0.0.1:11434';
  button(t.host, 'Probe host for models').click();
  await flush();
  const pick = field(t.host, 'model');
  eq(pick.tagName, 'SELECT');
  eq(pick.childNodes.length, 2);
  stop();
});

test('U4: probing prefills the context field, it is sent as context_tokens, and the card shows a compact badge', async () => {
  const t = harness({}, { probeBackend: async () => ({ models: ['m-a', 'm-b'], contexts: { 'm-a': 32768 } }) });
  const stop = t.view.mount();
  await flush();
  button(t.host, 'Add backend').click();
  field(t.host, 'name').value = 'ctx1';
  field(t.host, 'host').value = 'http://127.0.0.1:11434';
  button(t.host, 'Probe host for models').click();
  await flush();
  eq(field(t.host, 'context_tokens').value, '32768');
  button(t.host, 'Add backend').click();
  await flush();
  eq((t.bodies.create as BackendInput).models, [{ model: 'm-a', paid: false, max_concurrent: 0, context_tokens: 32768 }]);
  stop();
  const card = harness({ backends: [be('b1', { models: [{ model: 'm1', paid: false, max_concurrent: 0, context_tokens: 32768 }] })] });
  const stop2 = card.view.mount();
  await flush();
  ok(card.host.textContent.includes('32.8k'), 'badge');
  stop2();
});

test('BK1: adding a backend sends the form; a 400 shows next to the field it names', async () => {
  let fail = true;
  const t = harness({}, {
    createBackend: async (b) => {
      if (fail) throw new ApiError(400, 'must be an http or https URL with a host', 'host');
      t.log.push('create:' + b.name);
      t.bodies.create = b;
      return result(be(b.name));
    },
  });
  const stop = t.view.mount();
  await flush();
  button(t.host, 'Add backend').click();
  field(t.host, 'name').value = 'local1';
  field(t.host, 'host').value = 'nope';
  field(t.host, 'api_type').value = 'ollama';
  field(t.host, 'slots').value = '3';
  field(t.host, 'model').value = ' qwen ';
  field(t.host, 'max_concurrent').value = '1';
  button(t.host, 'Add backend').click();
  await flush();
  eq(errorOf(t.host, 'host'), 'must be an http or https URL with a host');
  eq(errorOf(t.host, 'name'), '');
  field(t.host, 'host').value = 'http://127.0.0.1:11434';
  fail = false;
  button(t.host, 'Add backend').click();
  await flush();
  eq(t.bodies.create, { name: 'local1', host: 'http://127.0.0.1:11434', api_type: 'ollama', hosted: false, paid: false, slots: 3, models: [{ model: 'qwen', paid: false, max_concurrent: 1, context_tokens: 0 }] });
  ok(t.log.includes('notify:Added local1'), 'toast');
  stop();
});

test('a models error from the server shows beside the models list', async () => {
  const t = harness({}, { createBackend: async () => { throw new ApiError(400, 'at least one model is required', 'models'); } });
  const stop = t.view.mount();
  await flush();
  button(t.host, 'Add backend').click();
  button(t.host, 'Add backend').click();
  await flush();
  ok(findAll(t.host, (e) => e.className === 'field-error' && e.textContent === 'at least one model is required').length === 1, 'models error shown');
  stop();
});

test('edit patches the backend without renaming it', async () => {
  const t = harness({ backends: [be('b1')] });
  const stop = t.view.mount();
  await flush();
  button(t.host, 'Edit').click();
  eq(field(t.host, 'name').getAttribute('readonly'), '');
  field(t.host, 'slots').value = '4';
  button(t.host, 'Save backend').click();
  await flush();
  eq(t.log.filter((l) => l.startsWith('patch:')), ['patch:b1']);
  eq((t.bodies.patch as BackendInput).slots, 4);
  stop();
});

test('SV3: retiring the last backend shows the warning up front and the server warnings after', async () => {
  const t = harness({ backends: [be('b1')], candidates: [cand('worker', 0, 'b1', 'm1')] });
  const stop = t.view.mount();
  await flush();
  button(t.host, 'Retire').click();
  const panel = byAttr(t.host, 'data-panel', 'retire')[0];
  ok(panel.textContent.includes('only active backend'), 'last-backend warning');
  ok(panel.textContent.includes('only backend listed for: worker'), 'role named');
  (field(panel, 'cancel_running') as FakeEl).checked = true;
  button(panel, 'Retire backend').click();
  await flush();
  eq(t.log.filter((l) => l.startsWith('retire:')), ['retire:b1:true']);
  const warn = byAttr(t.host, 'data-role', 'warnings')[0];
  ok(warn.textContent.includes('retired the last backend for worker; project p1 is running'), 'server warning shown');
  stop();
});

test('a retired backend can be enabled again', async () => {
  const t = harness({ backends: [be('b1', { retired: true })] });
  const stop = t.view.mount();
  await flush();
  ok(t.host.textContent.includes('retired'), 'retired badge');
  button(t.host, 'Enable').click();
  await flush();
  eq(t.log.filter((l) => l.startsWith('enable:')), ['enable:b1']);
  stop();
});

test('MB5: a paid candidate shows the paid badge and cannot be saved without opt-in', async () => {
  const t = harness({ backends: [be('b1')], candidates: [cand('worker', 0, 'b1', 'm1')] });
  const stop = t.view.mount();
  await flush();
  const worker = byAttr(t.host, 'data-role-editor', 'worker')[0];
  field(worker, 'model').value = 'm2-paid';
  button(worker, 'Add candidate').click();
  const w = byAttr(t.host, 'data-role-editor', 'worker')[0];
  ok(findAll(w, (e) => e.className.startsWith('badge badge-paid')).length === 1, 'paid badge on the new row');
  button(w, 'Save order').click();
  await flush();
  eq(t.log.filter((l) => l.startsWith('cands:')), []);
  ok(w.textContent.includes('Tick the paid opt-in for: b1/m2-paid'), 'blocked with a reason');
  const optIn = field(w, 'opt_in');
  optIn.checked = true;
  optIn.fire('change');
  button(byAttr(t.host, 'data-role-editor', 'worker')[0], 'Save order').click();
  await flush();
  eq(t.bodies.cands, [{ backend: 'b1', model: 'm1' }, { backend: 'b1', model: 'm2-paid' }]);
  stop();
});

test('F5: Save order on an empty list adds the picked pair and saves in one step; nothing picked keeps the message', async () => {
  const t = harness({ backends: [be('b1')] });
  const stop = t.view.mount();
  await flush();
  const worker = (): FakeEl => byAttr(t.host, 'data-role-editor', 'worker')[0];
  button(worker(), 'Save order').click();
  await flush();
  eq(t.bodies.cands, [{ backend: 'b1', model: 'm1' }]);
  stop();
  const none = harness({ backends: [be('b1', { retired: true })] });
  const stop2 = none.view.mount();
  await flush();
  const w = byAttr(none.host, 'data-role-editor', 'worker')[0];
  button(w, 'Save order').click();
  await flush();
  ok(w.textContent.includes('Nothing to save yet'), 'message kept when nothing is selected');
  eq(none.log.filter((l) => l.startsWith('cands:')), []);
  stop2();
});

test('F4: a backend flagged hosted on a private or loopback host gets a warning badge', async () => {
  const t = harness({ backends: [
    be('ollama-m5', { hosted: true, host: 'http://192.168.1.20:11434' }),
    be('cloud', { hosted: true, host: 'https://api.example.com' }),
    be('local', { hosted: false, host: 'http://127.0.0.1:11434' }),
  ] });
  const stop = t.view.mount();
  await flush();
  const warn = (name: string): number => byAttr(byAttr(t.host, 'data-backend', name)[0], 'data-role', 'hosted-warning').length;
  eq([warn('ollama-m5'), warn('cloud'), warn('local')], [1, 0, 0]);
  ok(byAttr(t.host, 'data-role', 'hosted-warning')[0].getAttribute('title')?.includes('mis-flagged'), 'tooltip explains');
  stop();
});

test('F4: isPrivateHost recognises loopback and private ranges only', () => {
  const yes = ['http://localhost:11434', 'http://127.0.0.1:1', '10.1.2.3:8080', 'http://192.168.0.5', 'http://172.16.0.1', 'http://172.31.9.9:80', 'http://[::1]:80'];
  const no = ['https://api.openai.com', 'http://172.15.0.1', 'http://172.32.0.1', 'http://8.8.8.8', 'http://192.169.0.1', 'http://example.com:80'];
  eq(yes.filter((h) => !isPrivateHost(h)), []);
  eq(no.filter((h) => isPrivateHost(h)), []);
});

test('candidates reorder and remove locally before saving', async () => {
  const t = harness({ backends: [be('b1'), be('b2')], candidates: [cand('critic', 0, 'b1', 'm1'), cand('critic', 1, 'b2', 'm1')] });
  const stop = t.view.mount();
  await flush();
  const ed = (): FakeEl => byAttr(t.host, 'data-role-editor', 'critic')[0];
  const row = (i: number): FakeEl => byAttr(ed(), 'data-candidate', 'critic:' + i)[0];
  const handle = (i: number): FakeEl => byAttr(row(i), 'data-action', 'reorder')[0];
  handle(1).fire('keydown', { key: 'ArrowUp' });
  ok(row(0).textContent.includes('b2 / m1'), 'moved up');
  button(row(1), 'Remove').click();
  button(ed(), 'Save order').click();
  await flush();
  eq(t.bodies.cands, [{ backend: 'b2', model: 'm1' }]);
  stop();
});

test('H4: the reorder handle is a focusable labelled control; arrow keys move a candidate and the ends are no-ops', async () => {
  const t = harness({ backends: [be('b1'), be('b2'), be('b3')], candidates: [cand('critic', 0, 'b1', 'm1'), cand('critic', 1, 'b2', 'm1'), cand('critic', 2, 'b3', 'm1')] });
  const stop = t.view.mount();
  await flush();
  const ed = (): FakeEl => byAttr(t.host, 'data-role-editor', 'critic')[0];
  const row = (i: number): FakeEl => byAttr(ed(), 'data-candidate', 'critic:' + i)[0];
  const handle = (i: number): FakeEl => byAttr(row(i), 'data-action', 'reorder')[0];
  eq(handle(0).getAttribute('tabindex'), '0');
  ok(handle(0).getAttribute('aria-label')?.includes('b1 / m1'), 'labelled with the candidate');
  eq(findAll(ed(), (e) => e.tagName === 'BUTTON' && (e.textContent === 'Up' || e.textContent === 'Down')).length, 0);
  handle(0).fire('keydown', { key: 'ArrowUp' });
  ok(row(0).textContent.includes('b1 / m1'), 'first row cannot move up');
  handle(0).fire('keydown', { key: 'ArrowDown' });
  handle(1).fire('keydown', { key: 'ArrowDown' });
  handle(2).fire('keydown', { key: 'a' });
  eq([0, 1, 2].map((i) => /b\d/.exec(row(i).textContent)?.[0]), ['b2', 'b3', 'b1']);
  button(ed(), 'Save order').click();
  await flush();
  eq(t.bodies.cands, [{ backend: 'b2', model: 'm1' }, { backend: 'b3', model: 'm1' }, { backend: 'b1', model: 'm1' }]);
  stop();
});

test('H4: moveItem and dropIndex place a dragged row by the pointer', () => {
  eq(moveItem(['a', 'b', 'c'], 0, 2), ['b', 'c', 'a']);
  eq(moveItem(['a', 'b', 'c'], 2, 0), ['c', 'a', 'b']);
  eq(moveItem(['a', 'b', 'c'], 1, 9), ['a', 'c', 'b']);
  eq(moveItem(['a', 'b'], 5, 0), ['a', 'b']);
  const mids = [10, 30, 50];
  eq(dropIndex(mids, 0, 40), 1);
  eq(dropIndex(mids, 0, 60), 2);
  eq(dropIndex(mids, 2, 0), 0);
  eq(dropIndex(mids, 1, 31), 1);
});

test('isPaid, unconsented and lastForRoles', () => {
  const bs = [be('b1'), be('b2', { paid: true })];
  ok(isPaid(bs, 'b1', 'm2-paid') && !isPaid(bs, 'b1', 'm1') && isPaid(bs, 'b2', 'm1'), 'paid by model or backend');
  eq(unconsented(bs, [{ backend: 'b2', model: 'm1', optIn: false }, { backend: 'b1', model: 'm1', optIn: false }]).length, 1);
  eq(lastForRoles([cand('worker', 0, 'b1', 'm1'), cand('critic', 0, 'b1', 'm1'), cand('critic', 1, 'b2', 'm1')], 'b1'), ['worker']);
});

test('CF2: project limits show blank as inherit and send blanks for the unset ones', async () => {
  const t = harness({ backends: [be('b1')], projects: [project('p1')] });
  const stop = t.view.mount();
  await flush();
  const limits = byAttr(t.host, 'data-role', 'project-limits')[0];
  eq(field(limits, 'max_attempts').value, '9');
  eq(field(limits, 'worker_timeout').getAttribute('placeholder'), 'inherit (30m0s)');
  field(limits, 'worker_timeout').value = '45m';
  field(limits, 'max_attempts').value = '';
  button(t.host, 'Save project limits').click();
  await flush();
  const sent = t.bodies.limits as Record<string, string>;
  eq([sent.worker_timeout, sent.max_attempts, sent.review_timeout], ['45m', '', '']);
  eq(t.log.filter((l) => l.startsWith('limits:')), ['limits:p1']);
  stop();
});

test('a limits 400 names the limit field', async () => {
  const t = harness({ backends: [be('b1')], projects: [project('p1')] }, {
    putLimits: async () => { throw new ApiError(400, 'must be a duration such as 30m', 'limits.worker_timeout'); },
  });
  const stop = t.view.mount();
  await flush();
  button(t.host, 'Save project limits').click();
  await flush();
  eq(errorOf(t.host, 'worker_timeout'), 'must be a duration such as 30m');
  stop();
});

test('MB8 + AC-UI1: the pass-rate table shows hostile model names as text', async () => {
  const evil = '<img src=x onerror=alert(1)>';
  const t = harness({
    backends: [be('b1')],
    stats: [{ model: evil, backend: 'b1', role: 'worker', stages: 4, passed: 2, failed: 1, infra: 1, pass_rate: 2 / 3, attempts_total: 3, attempts_passed: 2 }],
  });
  const stop = t.view.mount();
  await flush();
  const table = byAttr(t.host, 'data-role', 'pass-rates')[0];
  ok(table.textContent.includes(evil), 'literal name');
  ok(table.textContent.includes('67%') && table.textContent.includes('2/3'), 'rates: ' + table.textContent);
  ok(findAll(t.host, (e) => e.tagName === 'IMG').length === 0, 'no element injected');
  stop();
});

test('an unsaved candidate draft survives a backend.update event; an idle view refetches', async () => {
  const t = harness({ backends: [be('b1')], candidates: [cand('worker', 0, 'b1', 'm1')] });
  const stop = t.view.mount();
  await flush();
  button(byAttr(t.host, 'data-role-editor', 'worker')[0], 'Remove').click();
  t.st.backends = [be('b1'), be('b2')];
  dispatch(JSON.stringify({ type: 'backend.update', backend: 'b2' }));
  await flush();
  ok(!t.host.textContent.includes('b2'), 'no refetch while editing');
  button(byAttr(t.host, 'data-role-editor', 'worker')[0], 'Discard').click();
  dispatch(JSON.stringify({ type: 'backend.update', backend: 'b2' }));
  await flush();
  ok(t.host.textContent.includes('b2'), 'refetched once idle');
  stop();
});

// ─── H5: the write-only API key ───

const keyRow = (host: FakeEl): FakeEl => field(host, 'api_key').parentNode?.parentNode as FakeEl;
const setHosted = (host: FakeEl, on: boolean): void => {
  const toggle = field(host, 'hosted');
  toggle.checked = on;
  (toggle.firstChild as FakeEl).fire('change');
};
const SECRET = 'sk-secret-123';

test('H5: the API key input is masked and shown only while Hosted is on', async () => {
  const t = harness();
  const stop = t.view.mount();
  await flush();
  button(t.host, 'Add backend').click();
  eq(field(t.host, 'api_key').getAttribute('type'), 'password');
  ok(keyRow(t.host).hasAttribute('hidden'), 'hidden while not hosted');
  setHosted(t.host, true);
  ok(!keyRow(t.host).hasAttribute('hidden'), 'shown when hosted');
  setHosted(t.host, false);
  ok(keyRow(t.host).hasAttribute('hidden'), 'hidden again');
  stop();
});

test('H5: a typed key is sent as api_key; an untouched field sends none', async () => {
  const t = harness({ backends: [be('cloud', { hosted: true, has_key: true })] });
  const stop = t.view.mount();
  await flush();
  button(t.host, 'Edit').click();
  button(t.host, 'Save backend').click();
  await flush();
  ok(!('api_key' in (t.bodies.patch as object)), 'absent keeps the stored key');
  button(t.host, 'Edit').click();
  field(t.host, 'api_key').value = SECRET;
  button(t.host, 'Save backend').click();
  await flush();
  eq((t.bodies.patch as BackendInput).api_key, SECRET);
  ok(!t.log.some((l) => l.includes(SECRET)), 'the key is not in any toast');
  ok(!t.host.textContent.includes(SECRET), 'the key is not in the page text');
  stop();
});

test('H5: has_key shows a key set badge in the list and the form; Clear key sends an empty api_key, Keep key undoes it', async () => {
  const t = harness({ backends: [be('cloud', { hosted: true, has_key: true }), be('plain', { hosted: true })] });
  const stop = t.view.mount();
  await flush();
  eq(findAll(byAttr(t.host, 'data-backend', 'cloud')[0], (e) => e.textContent === 'key set').length, 1);
  eq(findAll(byAttr(t.host, 'data-backend', 'plain')[0], (e) => e.textContent === 'key set').length, 0);
  button(byAttr(t.host, 'data-backend', 'cloud')[0], 'Edit').click();
  eq(byAttr(t.host, 'data-role', 'key-state')[0].textContent, 'key setClear key');
  button(t.host, 'Clear key').click();
  ok(t.host.textContent.includes('key cleared on save'), 'pending clear shown');
  button(t.host, 'Keep key').click();
  button(t.host, 'Save backend').click();
  await flush();
  ok(!('api_key' in (t.bodies.patch as object)), 'kept');
  button(t.host, 'Edit').click();
  button(t.host, 'Clear key').click();
  button(t.host, 'Save backend').click();
  await flush();
  eq((t.bodies.patch as BackendInput).api_key, '');
  stop();
});

test('U5 (review): the context field says unknown, and picking another probed model swaps in that model\'s window unless one was typed', async () => {
  const t = harness({}, { probeBackend: async () => ({ models: ['m-a', 'm-b', 'm-c'], contexts: { 'm-a': 32768, 'm-b': 8192 } }) });
  const stop = t.view.mount();
  await flush();
  button(t.host, 'Add backend').click();
  eq(field(t.host, 'context_tokens').getAttribute('placeholder'), '?');
  field(t.host, 'name').value = 'ctx2';
  field(t.host, 'host').value = 'http://127.0.0.1:11434';
  button(t.host, 'Probe host for models').click();
  await flush();
  eq(field(t.host, 'context_tokens').value, '32768');
  field(t.host, 'model').value = 'm-b';
  field(t.host, 'model').fire('change');
  eq(field(t.host, 'context_tokens').value, '8192');
  field(t.host, 'model').value = 'm-c';
  field(t.host, 'model').fire('change');
  eq(field(t.host, 'context_tokens').value, '');
  field(t.host, 'context_tokens').value = '4000';
  field(t.host, 'model').value = 'm-a';
  field(t.host, 'model').fire('change');
  eq(field(t.host, 'context_tokens').value, '4000');
  stop();
});

void run('agentflow-backends');

test('OB1: the opencode built-in type hides host and key, probes without a host, and saves an empty host', async () => {
  let probed: string[] = [];
  const t = harness({}, { probeBackend: async (host, type) => { probed = [host, type]; return { models: ['big-pickle', 'cat-jam-free'] }; } });
  const stop = t.view.mount();
  await flush();
  button(t.host, 'Add backend').click();
  const hostRow = (): FakeEl => field(t.host, 'host').parentNode as FakeEl;
  ok(!hostRow().hasAttribute('hidden'), 'host shown for ollama');
  field(t.host, 'name').value = 'free';
  field(t.host, 'host').value = 'http://stale:1';
  field(t.host, 'api_type').value = 'opencode';
  field(t.host, 'api_type').fire('change');
  ok(hostRow().hasAttribute('hidden'), 'host hidden');
  setHosted(t.host, true);
  ok(keyRow(t.host).hasAttribute('hidden'), 'no key field for the built-in provider');
  button(t.host, 'Probe host for models').click();
  await flush();
  eq(probed, ['', 'opencode']);
  button(t.host, 'Add backend').click();
  await flush();
  const body = t.bodies.create as BackendInput;
  eq([body.host, body.api_type, body.api_key], ['', 'opencode', undefined]);
  stop();
});
