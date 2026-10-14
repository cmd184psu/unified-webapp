import { test, run, eq, ok, installFakeDom, FakeEl, asEl, byField, byAttr, allByField, button, flush, findAll } from './testkit';
import { DirPicker } from './dirpicker';
import { ProjectForm, splitArgv, joinArgv, shellOperators, isNotARepo, type ProjectDeps } from './projects';
import { ApiError, type FsDirs, type Backend, type Candidate, type ProjectInput, type ProjectView } from './api';

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
  const chosen: { path: string | null } = { path: null };
  const bodies: Record<string, unknown> = {};
  let cands: Candidate[] = [cand('global', 'worker', 0), cand('project', 'critic', 0, 'p1')];
  const deps: ProjectDeps = {
    createProject: async (p: ProjectInput) => { log.push('create:' + p.slug); bodies.create = p; return view(p.slug); },
    patchProject: async (slug, p) => { log.push('patch:' + slug); bodies.patch = p; return view(slug); },
    gitInit: async (path) => { log.push('git-init:' + path); return { repo_path: path }; },
    deleteProject: async (slug) => { log.push('delete:' + slug); return { deleted: slug, branches: [], commands: [] }; },
    backends: async () => [backend],
    candidates: async () => cands,
    putProjectCandidates: async (slug, role, list) => { log.push(`put:${slug}:${role}`); bodies.put = list; cands = [...cands.filter((c) => c.role !== role), ...list.map((c, i) => cand('project', role, i, slug))]; return cands; },
    clearProjectCandidates: async (slug, role) => { log.push(`clear:${slug}:${role}`); cands = cands.filter((c) => !(c.scope === 'project' && c.role === role)); },
    definitionOfDone: async () => ({ items: ['go test passes'], source: 'detected' }),
    confirm: async (m) => { log.push('confirm:' + m); return true; },
    pickDir: async (start) => { log.push('pick:' + start); return chosen.path; },
    notify: (m) => { log.push('notify:' + m); },
    ...over,
  };
  const done: string[] = [];
  const form = new ProjectForm(asEl(host), deps, mode, (s) => { done.push(s); });
  return { host, form, log, bodies, done, chosen };
}

/** pick drives the Repository button: the picker is stubbed to answer path. */
async function pick(t: ReturnType<typeof harness>, path: string | null): Promise<void> {
  t.chosen.path = path;
  byField(t.host, 'repo_path').click();
  await flush();
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
  await pick(t, '/srv/demo');
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

test('H2: the Repository field is a button showing the placeholder, then the chosen path; cancelling keeps it', async () => {
  const t = harness();
  await t.form.mount();
  eq(byField(t.host, 'repo_path').tagName, 'BUTTON');
  eq(byField(t.host, 'repo_path').textContent, 'Choose a folder...');
  await pick(t, '/srv/one');
  eq(byField(t.host, 'repo_path').textContent, '/srv/one');
  await pick(t, null);
  eq(byField(t.host, 'repo_path').textContent, '/srv/one');
  ok(t.log.includes('pick:/srv/one'), 'the picker starts at the current path');
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
  await pick(t, '/srv/new');
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
  await pick(t, '/srv/new');
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
  await pick(t, '/r/moved');
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

test('unregister asks first, deletes, and shows the branch commands', async () => {
  const shown: string[][] = [];
  const t = harness({
    deleteProject: async (slug) => { t.log.push('delete:' + slug); return { deleted: slug, branches: ['agentflow/effort-1'], commands: ["git -C '/r/p1' branch -D 'agentflow/effort-1'"] }; },
    showCommands: (_slug, commands) => { shown.push(commands); },
  }, { project: view('p1') });
  await t.form.mount();
  const b = button(t.host, 'Unregister');
  eq((b as unknown as { disabled: boolean }).disabled, false);
  b.click();
  await flush();
  ok(t.log.includes('confirm:Unregister p1? Its history is removed; the repository is not touched.'), 'asked: ' + t.log.join('|'));
  eq(t.log.filter((l) => l.startsWith('delete:')), ['delete:p1']);
  eq(shown, [["git -C '/r/p1' branch -D 'agentflow/effort-1'"]]);
  eq(t.done, ['']);
});

test('unregister is disabled with a reason while the run is not stopped or an effort is active', async () => {
  for (const [over, reason] of [
    [{ run_state: 'running' }, 'Stop the run first'],
    [{ effort: { id: 1 } as never }, 'Finish or abandon the active effort first'],
  ] as const) {
    const t = harness({}, { project: view('p1', over) });
    await t.form.mount();
    const b = byAttr(t.host, 'data-action', 'delete-project')[0];
    eq((b as unknown as { disabled: boolean }).disabled, true);
    eq(b.getAttribute('title'), reason);
    b.click();
    await flush();
    eq(t.log.filter((l) => l.startsWith('delete:') || l.startsWith('confirm:')), []);
  }
});

test('a created form offers no role editor yet', async () => {
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

// ─── H2: the server-side directory picker ───

const TREE: Record<string, FsDirs> = {
  '/srv': { path: '/srv', parent: '', dirs: [{ name: 'app', path: '/srv/app', git: true }, { name: 'docs', path: '/srv/docs', git: false }] },
  '/srv/app': { path: '/srv/app', parent: '/srv', dirs: [] },
  '/srv/docs': { path: '/srv/docs', parent: '/srv', dirs: [{ name: '<b>x</b>', path: '/srv/docs/x', git: false }] },
};

function picker(start = '', over: { fsDirs?: (p?: string) => Promise<FsDirs> } = {}) {
  const asked: string[] = [];
  const out = { chosen: null as string | null, cancelled: false };
  const fsDirs = over.fsDirs ?? (async (p = ''): Promise<FsDirs> => {
    asked.push(p);
    const r = TREE[p || '/srv'];
    if (!r) throw new ApiError(404, p + ' is outside the browse roots', 'path');
    return r;
  });
  const dp = new DirPicker(fsDirs, start, (p) => { out.chosen = p; }, () => { out.cancelled = true; });
  const el = dp.el as unknown as FakeEl;
  const rows = (): string[] => findAll(el, (e) => e.getAttribute('data-dir') !== null).map((e) => e.textContent);
  return { dp, el, asked, out, rows };
}

test('H2: the picker lists the first root, marks git repositories and shows names as text', async () => {
  const t = picker();
  await t.dp.mount();
  eq(t.asked, ['']);
  eq(t.rows(), ['appgit', 'docs']);
  eq(byAttr(t.el, 'data-role', 'dir-path')[0].textContent, '/srv');
  ok(byAttr(t.el, 'data-dir', '/srv/app')[0].getAttribute('title')?.includes('git repository'), 'git tooltip');
  ok(button(t.el, 'Parent folder').hasAttribute('disabled'), 'no parent at a root');
});

test('H2: clicking a folder descends, Up returns, Choose reports the current folder', async () => {
  const t = picker();
  await t.dp.mount();
  byAttr(t.el, 'data-dir', '/srv/docs')[0].click();
  await flush();
  eq(byAttr(t.el, 'data-role', 'dir-path')[0].textContent, '/srv/docs');
  eq(t.rows(), ['<b>x</b>']);
  ok(findAll(t.el, (e) => e.tagName === 'B').length === 0, 'name is not markup');
  ok(!button(t.el, 'Parent folder').hasAttribute('disabled'), 'up enabled');
  button(t.el, 'Parent folder').click();
  await flush();
  eq(byAttr(t.el, 'data-role', 'dir-path')[0].textContent, '/srv');
  byAttr(t.el, 'data-dir', '/srv/app')[0].click();
  await flush();
  ok(t.el.textContent.includes('No subfolders'), 'empty note');
  button(t.el, 'Choose this folder').click();
  eq(t.out.chosen, '/srv/app');
  button(t.el, 'Cancel').click();
  ok(t.out.cancelled, 'cancel reported');
});

test('H2: a start path the server refuses falls back to the first root; a later refusal shows its message and keeps the listing', async () => {
  const t = picker('/etc');
  await t.dp.mount();
  eq(t.asked, ['/etc', '']);
  eq(t.rows(), ['appgit', 'docs']);
  eq(errorsOf(t.el), []);
  const bad = picker('', { fsDirs: async (p = '') => { if (p === '/srv/app') throw new ApiError(404, 'no such directory'); return TREE['/srv']; } });
  await bad.dp.mount();
  byAttr(bad.el, 'data-dir', '/srv/app')[0].click();
  await flush();
  eq(errorsOf(bad.el), ['no such directory']);
  eq(bad.rows(), ['appgit', 'docs']);
  byAttr(bad.el, 'data-dir', '/srv/docs')[0].click();
  await flush();
  eq(errorsOf(bad.el), []);
});

test('U2: the definition of done is overridden as a list, saved, and reset to detected with null', async () => {
  const untouched = harness({}, { project: view('p1') });
  await untouched.form.mount();
  button(untouched.host, 'Save project').click();
  await flush();
  ok(!('definition_of_done' in (untouched.bodies.patch as object)), 'untouched detected list is not sent');

  const t = harness({}, { project: view('p1') });
  await t.form.mount();
  ok(byAttr(t.host, 'data-role', 'dod-editor')[0].textContent.includes('go test passes'), 'detected list shown');
  button(t.host, 'Override for this project').click();
  allByField(t.host, 'dod_item')[0].value = 'make check passes';
  button(t.host, 'Add item').click();
  allByField(t.host, 'dod_item')[1].value = 'docs updated';
  button(t.host, 'Save project').click();
  await flush();
  eq((t.bodies.patch as { definition_of_done?: string[] | null }).definition_of_done, ['make check passes', 'docs updated']);

  const o = harness({ definitionOfDone: async () => ({ items: ['only this'], source: 'project' }) }, { project: view('p1') });
  await o.form.mount();
  eq(allByField(o.host, 'dod_item')[0].value, 'only this');
  button(o.host, 'Reset to detected').click();
  button(o.host, 'Save project').click();
  await flush();
  eq((o.bodies.patch as { definition_of_done?: string[] | null }).definition_of_done, null);
});

void run('agentflow-projects');
