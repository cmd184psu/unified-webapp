import { test, run, eq, ok, installFakeDom, FakeEl, asEl, byField, byAttr, allByField, button, flush, findAll } from './testkit';
import { ProjectForm, splitArgv, joinArgv, shellOperators, isNotARepo, type ProjectDeps } from './projects';
import { ApiError, type Backend, type Candidate, type ProjectInput, type ProjectView } from './api';

installFakeDom();

const view = (slug: string, over: Partial<ProjectView> = {}): ProjectView => ({
  slug, repo_path: '/r/' + slug, model_scope: 'local', feedback_cmds: ['go vet ./...'], limits: {}, created: 0, updated: 0,
  run_state: 'stopped', remembered_run_state: 'stopped', effort: null, progress: {}, current: [], ...over,
});
const backend: Backend = { name: 'b1', host: 'http://h', api_type: 'ollama', hosted: false, paid: false, slots: 1, retired: false, models: [{ model: 'm1', paid: false, max_concurrent: 0 }] };
const cand = (scope: string, role: string, position: number, project?: string): Candidate =>
  ({ scope, project, role, position, backend: 'b1', model: 'm1', paid: false });

function harness(over: Partial<ProjectDeps> = {}, mode: { project?: ProjectView } = {}) {
  const host = new FakeEl('DIV');
  const log: string[] = [];
  const bodies: Record<string, unknown> = {};
  let cands: Candidate[] = [cand('global', 'worker', 0), cand('project', 'critic', 0, 'p1')];
  const deps: ProjectDeps = {
    createProject: async (p: ProjectInput) => { log.push('create:' + p.slug); bodies.create = p; return view(p.slug); },
    patchProject: async (slug, p) => { log.push('patch:' + slug); bodies.patch = p; return view(slug); },
    gitInit: async (path) => { log.push('git-init:' + path); return { repo_path: path }; },
    deleteProject: async (slug) => { log.push('delete:' + slug); },
    backends: async () => [backend],
    candidates: async () => cands,
    putProjectCandidates: async (slug, role, list) => { log.push(`put:${slug}:${role}`); bodies.put = list; cands = [...cands.filter((c) => c.role !== role), ...list.map((c, i) => cand('project', role, i, slug))]; return cands; },
    clearProjectCandidates: async (slug, role) => { log.push(`clear:${slug}:${role}`); cands = cands.filter((c) => !(c.scope === 'project' && c.role === role)); },
    confirm: async (m) => { log.push('confirm:' + m); return true; },
    notify: (m) => { log.push('notify:' + m); },
    ...over,
  };
  const done: string[] = [];
  const form = new ProjectForm(asEl(host), deps, mode, (s) => { done.push(s); });
  return { host, form, log, bodies, done };
}

const errorsOf = (host: FakeEl): string[] => findAll(host, (e) => e.className === 'field-error' && e.textContent !== '').map((e) => e.textContent);

test('argv helpers: split on whitespace, join with single spaces, spot shell operators', () => {
  eq(splitArgv('  go   test ./... '), ['go', 'test', './...']);
  eq(joinArgv(['go', 'test']), 'go test');
  eq(shellOperators(['make', '&&', 'make', 'test', '$(id)', 'a|b']), ['&&', '$(id)']);
  ok(isNotARepo(new ApiError(400, '/x is not a git repository', 'repo_path')), 'recognised');
  ok(!isNotARepo(new ApiError(400, 'does not exist', 'repo_path')), 'other repo_path errors are not the offer');
});

test('PJ1: create sends slug, repo_path, scope and the feedback commands as argv-joined rows', async () => {
  const t = harness();
  await t.form.mount();
  byField(t.host, 'slug').value = ' demo ';
  byField(t.host, 'repo_path').value = '/srv/demo';
  byField(t.host, 'model_scope').value = 'any';
  byField(t.host, 'cleanup_enabled').checked = true;
  button(t.host, 'Add command').click();
  allByField(t.host, 'feedback_cmd')[0].value = '  go   test ./... ';
  button(t.host, 'Add command').click();
  allByField(t.host, 'feedback_cmd')[1].value = '   ';
  button(t.host, 'Create project').click();
  await flush();
  eq(t.bodies.create, { slug: 'demo', repo_path: '/srv/demo', model_scope: 'any', feedback_cmds: ['go test ./...'], cleanup_enabled: true, plugins: [] });
  eq(t.done, ['demo']);
});

test('the argv preview shows the split and a shell operator is flagged, never executed as shell', async () => {
  const t = harness();
  await t.form.mount();
  button(t.host, 'Add command').click();
  const input = byField(t.host, 'feedback_cmd');
  input.value = 'make && make test';
  input.fire('input');
  const prev = byAttr(t.host, 'data-role', 'argv')[0];
  eq(prev.textContent, 'argv: ["make","&&","make","test"]');
  const warn = byAttr(t.host, 'data-role', 'shell-warning')[0];
  ok(warn.textContent.includes('&&') && warn.getAttribute('hidden') === null, 'warned: ' + warn.textContent);
});

test('PJ1: a repo path that is not a repository offers git init; declining keeps the error beside the field', async () => {
  let creates = 0;
  const t = harness({
    createProject: async () => { creates++; throw new ApiError(400, '/srv/new is not a git repository', 'repo_path'); },
    confirm: async (m) => { t.log.push('confirm:' + m); return false; },
  });
  await t.form.mount();
  byField(t.host, 'slug').value = 'new';
  byField(t.host, 'repo_path').value = '/srv/new';
  button(t.host, 'Create project').click();
  await flush();
  ok(t.log.some((l) => l.startsWith('confirm:') && l.includes('git init')), 'offered');
  eq(t.log.filter((l) => l.startsWith('git-init:')), []);
  eq(creates, 1);
  eq(errorsOf(t.host), ['/srv/new is not a git repository']);
  eq(t.done, []);
});

test('PJ1: accepting the offer runs git init through the confirmed endpoint and retries the create', async () => {
  let creates = 0;
  const t = harness({
    createProject: async (p) => {
      creates++;
      if (creates === 1) throw new ApiError(400, '/srv/new is not a git repository', 'repo_path');
      return view(p.slug);
    },
  });
  await t.form.mount();
  byField(t.host, 'slug').value = 'new';
  byField(t.host, 'repo_path').value = '/srv/new';
  button(t.host, 'Create project').click();
  await flush();
  eq(t.log.filter((l) => l.startsWith('git-init:')), ['git-init:/srv/new']);
  eq(creates, 2);
  eq(t.done, ['new']);
});

test('a bad feedback row error lands beside the feedback commands, a slug error beside the slug', async () => {
  let field = 'feedback_cmds[1]';
  const t = harness({ createProject: async () => { throw new ApiError(400, 'must not be blank', field); } });
  await t.form.mount();
  button(t.host, 'Create project').click();
  await flush();
  const labelRow = (text: string): FakeEl => findAll(t.host, (e) => e.tagName === 'LABEL' && e.textContent === text)[0].parentNode as FakeEl;
  eq(errorsOf(labelRow('Feedback commands')), ['must not be blank']);
  eq(errorsOf(labelRow('Slug')), []);
  field = 'slug';
  button(t.host, 'Create project').click();
  await flush();
  eq(errorsOf(labelRow('Slug')), ['must not be blank']);
});

test('edit: the slug is fixed, the save patches, and the role editor shows the global fallback', async () => {
  const t = harness({}, { project: view('p1') });
  await t.form.mount();
  eq(byField(t.host, 'slug').getAttribute('readonly'), '');
  eq(allByField(t.host, 'feedback_cmd')[0].value, 'go vet ./...');
  const worker = byAttr(t.host, 'data-role-editor', 'worker')[0];
  ok(worker.textContent.includes('Uses the global list: b1/m1'), 'fallback note: ' + worker.textContent);
  byField(t.host, 'repo_path').value = '/r/moved';
  button(t.host, 'Save project').click();
  await flush();
  eq(t.bodies.patch, { repo_path: '/r/moved', model_scope: 'local', feedback_cmds: ['go vet ./...'], cleanup_enabled: false, plugins: [] });
  eq(t.done, ['p1']);
});

test('role preferences: a project override is saved per role and can be cleared back to the global list', async () => {
  const t = harness({}, { project: view('p1') });
  await t.form.mount();
  const reviewer = (): FakeEl => byAttr(t.host, 'data-role-editor', 'reviewer')[0];
  button(reviewer(), 'Add candidate').click();
  button(reviewer(), 'Save order').click();
  await flush();
  eq(t.log.filter((l) => l.startsWith('put:')), ['put:p1:reviewer']);
  eq(t.bodies.put, [{ backend: 'b1', model: 'm1' }]);
  const critic = byAttr(t.host, 'data-role-editor', 'critic')[0];
  ok(critic.textContent.includes('1. b1 / m1'), 'existing override shown');
  button(critic, 'Use global list').click();
  await flush();
  eq(t.log.filter((l) => l.startsWith('clear:')), ['clear:p1:critic']);
});

test('delete asks first and a created form offers no role editor yet', async () => {
  const t = harness({}, { project: view('p1') });
  await t.form.mount();
  button(t.host, 'Delete project').click();
  await flush();
  ok(t.log.some((l) => l.startsWith('confirm:Delete project p1')), 'asked');
  eq(t.log.filter((l) => l.startsWith('delete:')), ['delete:p1']);
  const c = harness();
  await c.form.mount();
  ok(c.host.textContent.includes('Role preferences can be set once the project exists'), 'create form note');
  eq(byAttr(c.host, 'data-role-editor', 'worker').length, 0);
});

test('OA3: plugins are sent as pinned name and version rows; a rejected version shows beside the field', async () => {
  let reject = true;
  const t = harness({
    patchProject: async (slug, p) => {
      t.log.push('patch:' + slug);
      t.bodies.patch = p;
      if (reject) throw new ApiError(400, 'plugin notify version "latest" must be an exact version such as 1.2.3 (no latest, no ranges)', 'plugins[0].version');
      return view(slug);
    },
  }, { project: view('p1', { plugins: [{ name: 'notify', version: 'latest' }] }) });
  await t.form.mount();
  eq(allByField(t.host, 'plugin_name')[0].value, 'notify');
  button(t.host, 'Save project').click();
  await flush();
  ok(errorsOf(t.host).some((m) => m.includes('exact version')), 'the version error is shown');
  eq(t.done, []);
  allByField(t.host, 'plugin_version')[0].value = ' 1.2.3 ';
  button(t.host, 'Add plugin').click();
  allByField(t.host, 'plugin_name')[1].value = '@acme/other';
  allByField(t.host, 'plugin_version')[1].value = '0.4.0';
  button(t.host, 'Add plugin').click(); // a blank row is dropped
  reject = false;
  button(t.host, 'Save project').click();
  await flush();
  eq((t.bodies.patch as ProjectInput).plugins, [{ name: 'notify', version: '1.2.3' }, { name: '@acme/other', version: '0.4.0' }]);
  eq(t.done, ['p1']);
});

test('OA3: a project starts with no plugins', async () => {
  const t = harness();
  await t.form.mount();
  eq(allByField(t.host, 'plugin_name').length, 0);
});

void run('agentflow-projects');
