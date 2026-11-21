import { timing } from './autosave';
import { test, run, eq, ok, installFakeDom, FakeEl, asEl, byField, byAttr, button, flush, findAll } from './testkit';
import type { InstallDeps } from './installs';
import { BackendsView, isPaid, isPrivateHost, lastForRoles, setCheckPollMs, unconsented, type BackendsDeps } from './backends';
import { moveItem, dropIndex } from './candidates';
import { dispatch } from './events';
import { ApiError, type Backend, type ModelRest, type BackendInput, type BackendResult, type Candidate, type ProjectView } from './api';

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
  /** what the "Are you sure?" dialog answers */
  sure: boolean;
  rests: ModelRest[];
  holds: { backend: string; model: string }[];
}

function harness(init: Partial<State> = {}, over: Partial<BackendsDeps> = {}) {
  const st: State = { backends: [], candidates: [], projects: [], sure: true, rests: [], holds: [], ...init };
  const host = new FakeEl('DIV');
  const log: string[] = [];
  const bodies: Record<string, unknown> = {};
  const deps: BackendsDeps = {
    backends: async () => st.backends.map((b) => ({ ...b })),
    candidates: async () => st.candidates.map((c) => ({ ...c })),
    projects: async () => st.projects,
    settings: async () => ({ worker_timeout: '30m0s', max_attempts: 5 }),
    limits: async () => ({ limits: { max_attempts: '9' }, effective: {} }),
    putLimits: async (slug, limits) => { log.push('limits:' + slug); bodies.limits = limits; return { limits: {}, effective: {} } as never; },
    probeBackend: async () => ({ models: ['m-a', 'm-b'] }),
    createBackend: async (b: BackendInput) => { log.push('create:' + b.name); bodies.create = b; return result(be(b.name)); },
    patchBackend: async (name, b) => { log.push('patch:' + name); bodies.patch = b; return result(be(name)); },
    retireBackend: async (name, cancel) => { log.push(`retire:${name}:${cancel}`); return result(be(name, { retired: true }), ['retired the last backend for worker; project p1 is running']); },
    enableBackend: async (name) => { log.push('enable:' + name); return result(be(name)); },
    rests: async () => st.rests,
    holds: async () => st.holds,
    setHold: async (backend, model, hold) => {
      log.push(`hold:${backend}:${model}:${hold}`);
      st.holds = st.holds.filter((x) => !(x.backend === backend && x.model === model));
      if (hold) st.holds.push({ backend, model });
    },
    wakeModel: async (backend, model) => { log.push(`wake:${backend}:${model}`); st.rests = st.rests.filter((r) => !(r.backend === backend && r.model === model)); },
    deleteBackend: async (name) => { log.push('delete:' + name); st.backends = st.backends.filter((b) => b.name !== name); },
    confirm: async (message, label) => { log.push(`confirm:${label ?? ''}`); bodies.confirm = message; return st.sure; },
    checkModels: async (name) => { log.push('check:' + name); return { state: 'running', current: 'm1', results: [] }; },
    checkStatus: async () => ({ state: 'idle', results: [] }),
    checkReviewers: async (name) => { log.push('reviewers:' + name); return { state: 'idle', done: 0, total: 0, summary: [], rows: [], cases: [] }; },
    evalStatus: async () => ({ state: 'idle', done: 0, total: 0, summary: [], rows: [], cases: [] }),
    extendWindows: async (name) => { log.push('extend:' + name); return [{ model: 'm1', previous: 0, window: 57344, applied: true, note: 'set to 57344 tokens' }]; },
    putCandidates: async (role, list) => {
      log.push('cands:' + role);
      bodies.cands = list;
      st.candidates = [...st.candidates.filter((c) => c.role !== role), ...list.map((c, i) => cand(role, i, c.backend, c.model))];
      return st.candidates;
    },
    notify: (m) => { log.push('notify:' + m); },
    ...over,
  };
  const installDeps: InstallDeps = {
    installs: async () => [],
    start: async (b, m, tier) => { log.push(`install:${b}:${m}:${tier}`); return { id: 1, backend: b, model: m, tier, state: 'running', status: '', completed: 0, total: 0, started: 1 }; },
    cancel: async () => {},
  };
  const view = new BackendsView(asEl(host), deps, installDeps);
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

test('H10: a probe marks a model without tool support, the form warns when it is picked, and the card badges a stored one', async () => {
  const t = harness({ backends: [be('o1', { models: [{ model: 'gemma', paid: false, max_concurrent: 0, tools: 'no' }, { model: 'ok', paid: false, max_concurrent: 0, tools: 'yes' }] })] }, {
    probeBackend: async () => ({ models: ['m-a', 'm-b'], tools: { 'm-a': 'yes', 'm-b': 'no' } }),
  });
  const stop = t.view.mount();
  await flush();
  const card = byAttr(t.host, 'data-backend', 'o1')[0].textContent;
  ok(card.includes('no tools'), 'card badge: ' + card);
  eq((card.match(/no tools/g) ?? []).length, 1, 'only the model without tools is badged');
  button(t.host, 'Add backend').click();
  field(t.host, 'host').value = 'http://127.0.0.1:11434';
  button(t.host, 'Probe host for models').click();
  await flush();
  const pick = field(t.host, 'model');
  ok(pick.childNodes.some((o) => o.textContent.includes('m-b (no tool support)')), 'the picker says which model has no tools');
  eq((byAttr(t.host, 'data-role', 'no-tools')[0] as unknown as { hidden: boolean }).hidden, true, 'no warning while a good model is picked');
  pick.value = 'm-b';
  pick.fire('change');
  eq((byAttr(t.host, 'data-role', 'no-tools')[0] as unknown as { hidden: boolean }).hidden, false, 'warning once the bad model is picked');
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

test('U4 (H15): probing does not fill the window field; the host\'s maximum is sent as context_tokens, a typed window as window_tokens, and the card shows both', async () => {
  const t = harness({}, { probeBackend: async () => ({ models: ['m-a', 'm-b'], contexts: { 'm-a': 32768 } }) });
  const stop = t.view.mount();
  await flush();
  button(t.host, 'Add backend').click();
  field(t.host, 'name').value = 'ctx1';
  field(t.host, 'host').value = 'http://127.0.0.1:11434';
  button(t.host, 'Probe host for models').click();
  await flush();
  eq(field(t.host, 'window_tokens').value, '');
  field(t.host, 'window_tokens').value = '24000';
  button(t.host, 'Add backend').click();
  await flush();
  eq((t.bodies.create as BackendInput).models, [{ model: 'm-a', paid: false, max_concurrent: 0, context_tokens: 32768, window_tokens: 24000, tier: 0 }]);
  stop();
  const card = harness({ backends: [be('b1', { models: [{ model: 'm1', paid: false, max_concurrent: 0, context_tokens: 32768, window_tokens: 16384, window_source: 'observed' }] })] });
  const stop2 = card.view.mount();
  await flush();
  ok(card.host.textContent.includes('max 32.8k'), 'max badge');
  ok(card.host.textContent.includes('window 16.4k (learned)'), 'learned window badge');
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
  eq(t.bodies.create, { name: 'local1', host: 'http://127.0.0.1:11434', api_type: 'ollama', hosted: false, paid: false, slots: 3, memory_budget_mb: 0, window_cap_tokens: 0, models: [{ model: 'qwen', paid: false, max_concurrent: 1, context_tokens: 0, window_tokens: 0, tier: 0 }] });
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

const wait = (ms: number): Promise<void> => new Promise((r) => setTimeout(r, ms));
/** settle lets the pause after an edit pass (the tests shorten it) so the card has saved. */
const settle = async (el: FakeEl): Promise<void> => { el.fire('input'); await wait(60); await flush(); };
timing.delay = 10;

test('edit patches the backend without renaming it', async () => {
  const t = harness({ backends: [be('b1')] });
  const stop = t.view.mount();
  await flush();
  button(t.host, 'Edit').click();
  eq(field(t.host, 'name').getAttribute('readonly'), '');
  field(t.host, 'slots').value = '4';
  await settle(field(t.host, 'slots'));
  eq(t.log.filter((l) => l.startsWith('patch:')), ['patch:b1']);
  eq((t.bodies.patch as BackendInput).slots, 4);
  stop();
});

test('H20: the Pace choice on a backend card sets its duty cap with no restart', async () => {
  const t = harness({ backends: [be('b1'), be('b2', { retired: true })] });
  const stop = t.view.mount();
  await flush();
  eq(byAttr(t.host, 'data-field', 'duty_percent').length, 1, 'a disabled backend has no pace');
  const pace = field(t.host, 'duty_percent') as FakeEl;
  eq(pace.value, '100', 'full speed by default');
  pace.value = '50';
  pace.fire('change');
  await flush();
  eq(t.log.filter((l) => l.startsWith('patch:')), ['patch:b1']);
  eq((t.bodies.patch as Partial<BackendInput>).duty_percent, 50);
  stop();
});

test('SV3: retiring the last backend shows the warning up front and the server warnings after', async () => {
  const t = harness({ backends: [be('b1')], candidates: [cand('worker', 0, 'b1', 'm1')] });
  const stop = t.view.mount();
  await flush();
  button(t.host, 'Disable').click();
  const panel = byAttr(t.host, 'data-panel', 'retire')[0];
  ok(panel.textContent.includes('only active backend'), 'last-backend warning');
  ok(panel.textContent.includes('only backend listed for: worker'), 'role named');
  (field(panel, 'cancel_running') as FakeEl).checked = true;
  button(panel, 'Disable').click();
  await flush();
  eq(t.log.filter((l) => l.startsWith('retire:')), ['retire:b1:true']);
  const warn = byAttr(t.host, 'data-role', 'warnings')[0];
  ok(warn.textContent.includes('retired the last backend for worker; project p1 is running'), 'server warning shown');
  stop();
});

test('H8: a backend that stopped answering shows as suspended, not just unreachable', async () => {
  const t = harness({ backends: [be('b1', { suspended_since: 1700000000000 }), be('b2', { down_until: 1700000000000 }), be('b3')] });
  const stop = t.view.mount();
  await flush();
  const cards = byAttr(t.host, 'data-backend', 'b1')[0].textContent + '|' + byAttr(t.host, 'data-backend', 'b2')[0].textContent + '|' + byAttr(t.host, 'data-backend', 'b3')[0].textContent;
  const [one, two, three] = cards.split('|');
  ok(one.includes('suspended') && !one.includes('unreachable'), 'b1: ' + one);
  ok(two.includes('unreachable') && !two.includes('suspended'), 'b2: ' + two);
  ok(!three.includes('suspended') && !three.includes('unreachable'), 'b3: ' + three);
  stop();
});

test('H6: an Ollama card has an install row that sends the model and tier; other backends have none', async () => {
  const t = harness({ backends: [be('o1'), be('cloud', { api_type: 'openai_compatible' })] });
  const stop = t.view.mount();
  await flush();
  eq(byAttr(t.host, 'data-role', 'install-panel').length, 1, 'one panel, on the ollama backend');
  field(t.host, 'install-model').value = ' devstral-small-2 ';
  field(t.host, 'install-model').fire('input');
  field(t.host, 'install-tier').value = '1';
  field(t.host, 'install-tier').fire('change');
  button(t.host, 'Install').click();
  await flush();
  eq(t.log.filter((l) => l.startsWith('install:')), ['install:o1:devstral-small-2:1']);
  stop();
});

test('a retired backend can be enabled again', async () => {
  const t = harness({ backends: [be('b1', { retired: true })] });
  const stop = t.view.mount();
  await flush();
  ok(t.host.textContent.includes('disabled'), 'disabled badge');
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
  await wait(60);
  await flush();
  eq(t.log.filter((l) => l.startsWith('cands:')), []);
  ok(byAttr(t.host, 'data-role-editor', 'worker')[0].textContent.includes('Tick the paid opt-in for: b1/m2-paid'), 'blocked with a reason');
  const optIn = field(byAttr(t.host, 'data-role-editor', 'worker')[0], 'opt_in');
  optIn.checked = true;
  optIn.fire('change');
  await wait(60);
  await flush();
  eq(t.bodies.cands, [{ backend: 'b1', model: 'm1' }, { backend: 'b1', model: 'm2-paid' }]);
  stop();
});

test('F5: an empty list saves nothing by itself; Add candidate adds the picked pair and it saves after the pause', async () => {
  const t = harness({ backends: [be('b1')] });
  const stop = t.view.mount();
  await flush();
  const worker = (): FakeEl => byAttr(t.host, 'data-role-editor', 'worker')[0];
  await wait(60);
  eq(t.log.filter((l) => l.startsWith('cands:')), []);
  button(worker(), 'Add candidate').click();
  await wait(60);
  await flush();
  eq(t.bodies.cands, [{ backend: 'b1', model: 'm1' }]);
  stop();
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
  await wait(60);
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
  await wait(60);
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
  await settle(field(limits, 'max_attempts'));
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
  const lim = byAttr(t.host, 'data-role', 'project-limits')[0];
  field(lim, 'worker_timeout').value = '1m';
  await settle(field(lim, 'worker_timeout'));
  eq(errorOf(t.host, 'worker_timeout'), 'must be a duration such as 30m');
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

test('H5: the API key input is masked and shown for every backend except opencode\'s built-in provider', async () => {
  const t = harness();
  const stop = t.view.mount();
  await flush();
  button(t.host, 'Add backend').click();
  eq(field(t.host, 'api_key').getAttribute('type'), 'password');
  ok(!keyRow(t.host).hasAttribute('hidden'), 'a local server may ask for a key too');
  setHosted(t.host, true);
  ok(!keyRow(t.host).hasAttribute('hidden'), 'shown when hosted');
  field(t.host, 'api_type').value = 'opencode';
  field(t.host, 'api_type').fire('change');
  ok(keyRow(t.host).hasAttribute('hidden'), 'hidden for the built-in provider');
  stop();
});

test('H5: a typed key is sent as api_key; an untouched field sends none', async () => {
  const t = harness({ backends: [be('cloud', { hosted: true, has_key: true })] });
  const stop = t.view.mount();
  await flush();
  button(t.host, 'Edit').click();
  field(t.host, 'slots').value = '3';
  await settle(field(t.host, 'slots'));
  ok(!('api_key' in (t.bodies.patch as object)), 'absent keeps the stored key');
  field(t.host, 'api_key').value = SECRET;
  await settle(field(t.host, 'api_key'));
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
  field(t.host, 'slots').value = '3';
  await settle(field(t.host, 'slots'));
  ok(!('api_key' in (t.bodies.patch as object)), 'kept');
  button(t.host, 'Clear key').click();
  await wait(60);
  await flush();
  eq((t.bodies.patch as BackendInput).api_key, '');
  stop();
});

test('U5 (H15): the window field says unknown, and picking another probed model sends that model\'s own maximum', async () => {
  const t = harness({}, { probeBackend: async () => ({ models: ['m-a', 'm-b', 'm-c'], contexts: { 'm-a': 32768, 'm-b': 8192 } }) });
  const stop = t.view.mount();
  await flush();
  button(t.host, 'Add backend').click();
  eq(field(t.host, 'window_tokens').getAttribute('placeholder'), '?');
  field(t.host, 'name').value = 'ctx2';
  field(t.host, 'host').value = 'http://127.0.0.1:11434';
  button(t.host, 'Probe host for models').click();
  await flush();
  eq(field(t.host, 'window_tokens').value, '');
  field(t.host, 'model').value = 'm-b';
  field(t.host, 'model').fire('change');
  eq(field(t.host, 'window_tokens').value, '');
  button(t.host, 'Add backend').click();
  await flush();
  eq((t.bodies.create as BackendInput).models, [{ model: 'm-b', paid: false, max_concurrent: 0, context_tokens: 8192, window_tokens: 0, tier: 0 }]);
  stop();
});

test('H4a: Probe sends the typed key and the saved backend name so a host that wants a key can be probed', async () => {
  const seen: unknown[][] = [];
  const t = harness({ backends: [be('kimi', { api_type: 'openai_compatible', has_key: true })] },
    { probeBackend: async (...a: unknown[]) => { seen.push(a); return { models: ['kimi-k3'] }; } });
  const stop = t.view.mount();
  await flush();
  button(t.host, 'Edit').click();
  button(t.host, 'Probe host for models').click();
  await flush();
  eq(seen[0].slice(2, 4), ['', 'kimi']);
  field(t.host, 'api_key').value = 'typed-key';
  button(t.host, 'Probe host for models').click();
  await flush();
  eq(seen[1].slice(2, 4), ['typed-key', 'kimi']);
  stop();
});

test('AF-14b: Kilo is a hostless backend with its own CLI path and full model ids', async () => {
  let probed: string[] = [];
  const t = harness({}, { probeBackend: async (host, type, _key, _backend, path) => { probed = [host, type, path]; return { models: ['kilo/liquid/lfm-3-1.7b:free'] }; } });
  const stop = t.view.mount();
  await flush();
  button(t.host, 'Add backend').click();
  field(t.host, 'name').value = 'kilo';
  field(t.host, 'api_type').value = 'kilo';
  field(t.host, 'api_type').fire('change');
  ok((field(t.host, 'host').parentNode as FakeEl).hasAttribute('hidden'), 'host hidden');
  ok(keyRow(t.host).hasAttribute('hidden'), 'key hidden');
  ok(!(field(t.host, 'cli_path').parentNode as FakeEl).hasAttribute('hidden'), 'CLI path shown');
  field(t.host, 'cli_path').value = '/opt/homebrew/bin/kilo';
  button(t.host, 'Probe host for models').click();
  await flush();
  eq(probed, ['', 'kilo', '/opt/homebrew/bin/kilo']);
  button(t.host, 'Add backend').click();
  await flush();
  const body = t.bodies.create as BackendInput;
  eq([body.host, body.api_type, body.cli_path, body.models[0].model], ['', 'kilo', '/opt/homebrew/bin/kilo', 'kilo/liquid/lfm-3-1.7b:free']);
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

test('H3: a model row has a tier control; the choice is sent and the card shows the tier', async () => {
  const t = harness({});
  const stop = t.view.mount();
  await flush();
  button(t.host, 'Add backend').click();
  field(t.host, 'name').value = 'tier1';
  field(t.host, 'host').value = 'http://127.0.0.1:11434';
  field(t.host, 'model').value = 'qwen';
  eq(field(t.host, 'tier').value, '0');
  field(t.host, 'tier').value = '3';
  button(t.host, 'Add backend').click();
  await flush();
  eq((t.bodies.create as BackendInput).models, [{ model: 'qwen', paid: false, max_concurrent: 0, context_tokens: 0, window_tokens: 0, tier: 3 }]);
  stop();
  const card = harness({ backends: [be('b1', { models: [{ model: 'm1', paid: false, max_concurrent: 0, tier: 1 }, { model: 'm2', paid: false, max_concurrent: 0, tier: 3 }] })] });
  const stop2 = card.view.mount();
  await flush();
  ok(card.host.textContent.includes('tier 1') && card.host.textContent.includes('tier 3'), 'badges');
  stop2();
});


test('H15d: the memory budget and window cap are sent; a card with a budget can extend its windows', async () => {
  const t = harness({});
  const stop = t.view.mount();
  await flush();
  button(t.host, 'Add backend').click();
  field(t.host, 'name').value = 'big';
  field(t.host, 'host').value = 'http://127.0.0.1:11434';
  field(t.host, 'model').value = 'qwen';
  field(t.host, 'memory_budget_gb').value = '30';
  field(t.host, 'window_cap_tokens').value = '65536';
  button(t.host, 'Add backend').click();
  await flush();
  const sent = t.bodies.create as BackendInput;
  eq([sent.memory_budget_mb, sent.window_cap_tokens], [30720, 65536]);
  stop();
  const none = harness({ backends: [be('b0')] });
  const stop0 = none.view.mount();
  await flush();
  ok(!none.host.textContent.includes('Extend windows'), 'no button without a budget');
  stop0();
  const card = harness({ backends: [be('b1', { memory_budget_mb: 30720, window_cap_tokens: 65536 })] });
  const stop2 = card.view.mount();
  await flush();
  ok(card.host.textContent.includes('30 GB'), 'budget shown');
  button(card.host, 'Extend windows').click();
  await flush();
  ok(card.log.includes('extend:b1'), 'extend called');
  ok(card.log.some((l) => l.startsWith('notify:Extended 1 of 1')), 'result reported');
  stop2();
});

test('H27: Check models runs a one-word test of each model, follows it, and badges each model with its result', async () => {
  setCheckPollMs(1);
  const results = [
    { Backend: 'b1', Model: 'm1', OK: true, Class: '', Message: '', Millis: 1200, Checked: 1 },
    { Backend: 'b1', Model: 'm2', OK: false, Class: 'failing', Message: 'Unexpected server error', Millis: 2000, Checked: 1 },
  ];
  let polls = -1; // the first look, when the page loads, finds no check running
  const t = harness({ backends: [be('b1', { models: [{ model: 'm1', paid: false, max_concurrent: 0 }, { model: 'm2', paid: false, max_concurrent: 0 }] })] }, {
    checkStatus: async () => (polls++ < 0 ? { state: 'idle' as const, results: [] } : polls < 2 ? { state: 'running' as const, current: 'm2', results: [results[0]] } : { state: 'idle' as const, results }),
  });
  const stop = t.view.mount();
  await flush();
  button(t.host, 'Check models').click();
  for (let i = 0; i < 20; i++) await flush();
  ok(t.log.includes('check:b1'), 'check started');
  const text = t.host.textContent;
  ok(text.includes('answers (1.2s)') && text.includes('not answering'), `badges shown: ${text}`);
  ok(t.log.some((l) => l.startsWith('notify:1 model on b1 did not answer: m2')), 'result reported');
  stop();
});

test('H32: Check reviewers runs the seeded-defect cases and badges each model with its score', async () => {
  setCheckPollMs(1);
  const done = {
    state: 'idle' as const, done: 4, total: 4, cases: [],
    summary: [
      { model: 'm1', layer: 'acceptance', caught: 4, defects: 4, false_alarms: 0, fixed: 4, unread: 0, ran: 1 },
      { model: 'm2', layer: 'acceptance', caught: 2, defects: 4, false_alarms: 1, fixed: 4, unread: 0, ran: 1 },
    ],
    rows: [{ model: 'm2', layer: 'acceptance', case: 'page-through-api', variant: 'defect', run: 1, outcome: 'missed', detail: 'verdict pass', millis: 10 }],
  };
  let polls = -1;
  const t = harness({ backends: [be('b1', { models: [{ model: 'm1', paid: false, max_concurrent: 0 }, { model: 'm2', paid: false, max_concurrent: 0 }] })] }, {
    evalStatus: async () => (polls++ < 0 ? { state: 'idle' as const, done: 0, total: 0, summary: [], rows: [], cases: [] } : done),
    checkReviewers: async (name) => { t.log.push('reviewers:' + name); return { ...done, state: 'running' as const, done: 1 }; },
  });
  const stop = t.view.mount();
  await flush();
  button(t.host, 'Check reviewers').click();
  for (let i = 0; i < 20; i++) await flush();
  ok(t.log.includes('reviewers:b1'), 'run started');
  const text = t.host.textContent;
  ok(text.includes('acceptance 4/4') && text.includes('acceptance 2/4, 1 false alarm'), `scores shown: ${text}`);
  ok(t.log.some((l) => l.startsWith('notify:1 model score on b1 missed a defect')), 'result reported');
  stop();
});

const trash = (host: FakeEl, name: string): FakeEl => button(byAttr(host, 'data-backend', name)[0], 'Delete');

test('BD1: the trash icon is greyed out, with the reason as its tooltip, while the backend is enabled or on a candidate list', async () => {
  const t = harness({
    backends: [be('on'), be('listed', { retired: true }), be('free', { retired: true })],
    candidates: [cand('worker', 0, 'listed', 'm1'), { scope: 'project', project: 'p1', role: 'reviewer', position: 0, backend: 'listed', model: 'm1', paid: false }],
  });
  const stop = t.view.mount();
  await flush();
  const on = trash(t.host, 'on'), listed = trash(t.host, 'listed'), free = trash(t.host, 'free');
  ok(on.getAttribute('disabled') !== null && on.getAttribute('title')!.toLowerCase().includes('disable'), `enabled: ${on.getAttribute('title')}`);
  ok(listed.getAttribute('disabled') !== null, 'a listed backend is greyed out');
  const why = listed.getAttribute('title')!;
  ok(why.includes('global worker') && why.includes('project p1 reviewer'), `the tooltip names both lists: ${why}`);
  eq(free.getAttribute('disabled'), null, 'a disabled backend on no list can be deleted');
  stop();
});

test('BD2: delete asks "Are you sure?" first, then deletes and the card goes', async () => {
  const t = harness({ backends: [be('b1', { retired: true }), be('b2', { retired: true })] });
  const stop = t.view.mount();
  await flush();
  t.st.sure = false;
  trash(t.host, 'b1').click();
  await flush();
  eq(t.log.filter((l) => l.startsWith('delete:')), [], 'declined: nothing deleted');
  ok(String(t.bodies.confirm).includes('b1') && String(t.bodies.confirm).includes('history'), `the question names it and says history stays: ${t.bodies.confirm}`);
  t.st.sure = true;
  trash(t.host, 'b1').click();
  await flush();
  eq(t.log.filter((l) => l.startsWith('delete:')), ['delete:b1']);
  eq(byAttr(t.host, 'data-backend', 'b1').length, 0, 'the card is gone');
  eq(byAttr(t.host, 'data-backend', 'b2').length, 1, 'the other card stays');
  stop();
});

const wakeBtn = (host: FakeEl, name: string): FakeEl => button(byAttr(host, 'data-backend', name)[0], 'Wake');

test('RS1: a resting model shows when it comes back and why, and Wake ends the rest', async () => {
  const until = Date.now() + 20 * 60_000;
  const t = harness({
    backends: [be('hosted1'), be('plain')],
    rests: [{ backend: 'hosted1', model: 'm1', until, reason: 'rate limited', streak: 2, since: until - 1000 }],
  });
  const stop = t.view.mount();
  await flush();
  const card = byAttr(t.host, 'data-backend', 'hosted1')[0].textContent;
  ok(card.includes('resting until') && card.includes('rate limited') && card.includes('2 in a row'), `the card says it rests: ${card}`);
  ok(!byAttr(t.host, 'data-backend', 'plain')[0].textContent.includes('resting'), 'a backend with no rest shows nothing');
  wakeBtn(t.host, 'hosted1').click();
  await flush();
  eq(t.log.filter((l) => l.startsWith('wake:')), ['wake:hosted1:m1']);
  ok(!byAttr(t.host, 'data-backend', 'hosted1')[0].textContent.includes('resting until'), 'the badge is gone after waking');
  stop();
});

test('RS2: a rest that has ended shows nothing', async () => {
  const t = harness({ backends: [be('hosted1')], rests: [{ backend: 'hosted1', model: 'm1', until: Date.now() - 1000, reason: 'stalled', streak: 1, since: 0 }] });
  const stop = t.view.mount();
  await flush();
  ok(!byAttr(t.host, 'data-backend', 'hosted1')[0].textContent.includes('resting'), 'an expired rest is not shown');
  stop();
});

test('RS3: Hold keeps a hosted model\'s work waiting for it, Release undoes it, a local backend has no Hold', async () => {
  const t = harness({ backends: [be('hosted1', { hosted: true }), be('plain')] });
  const stop = t.view.mount();
  await flush();
  ok(!byAttr(t.host, 'data-backend', 'plain')[0].textContent.includes('Hold'), 'a local backend has no Hold');
  button(byAttr(t.host, 'data-backend', 'hosted1')[0], 'Hold').click();
  await flush();
  eq(t.log.filter((l) => l.startsWith('hold:')), ['hold:hosted1:m1:true']);
  ok(byAttr(t.host, 'data-backend', 'hosted1')[0].textContent.includes('held'), 'the card says the model is held');
  button(byAttr(t.host, 'data-backend', 'hosted1')[0], 'Release').click();
  await flush();
  eq(t.log.filter((l) => l.startsWith('hold:')), ['hold:hosted1:m1:true', 'hold:hosted1:m1:false']);
  stop();
});
