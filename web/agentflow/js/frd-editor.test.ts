import { test, run, eq, ok, installFakeDom, FakeEl, asEl, byField, byAttr, allByField, button, flush, findAll } from './testkit';
import { FrdEditor, commaList, lines, type FrdDeps } from './frd-editor';
import { Overview, type OverviewDeps } from './overview';
import { ApiError, type DocView, type EffortView, type FRD, type ProjectView } from './api';

installFakeDom();

const EVIL = '<img src=x onerror=alert(1)>';

const effort = (id: number, title: string, over: Partial<EffortView> = {}): EffortView => ({
  id, project: 'p1', title, status: 'draft', run_state: 'stopped', integration_branch: '', base_commit: '', yolo: false, deliberate: false,
  created: 0, updated: 0, frd: { latest: 0, approved: 0 }, plan: { latest: 0, approved: 0 }, queue: null, ...over,
});

const FRD0: FRD = {
  id: 'FRD-1', title: 'First', objective: 'Do it', context: 'why', constraints: ['c1'],
  requirements: [{ id: 'R1', priority: 'must', description: 'one' }],
  acceptance_criteria: [{ id: 'AC1', requirements: ['R1'], criterion: 'works' }],
  state_model: { keep: ['me'] },
};

interface World {
  efforts: EffortView[];
  /** revisions per effort id */
  docs: Map<number, Array<{ doc: FRD; by: string }>>;
  approved: Map<number, number>;
}

function harness(w: World, over: Partial<FrdDeps> = {}) {
  const host = new FakeEl('DIV');
  const log: string[] = [];
  const sent: FRD[] = [];
  const view = (id: number, n: number, body: boolean): DocView<FRD> => {
    const d = w.docs.get(id)?.[n - 1];
    if (!d) throw new ApiError(404, 'not found');
    return { revision: n, created_by: d.by, created: 0, approved: w.approved.get(id) === n, json: body ? d.doc : undefined };
  };
  const deps: FrdDeps = {
    efforts: async () => w.efforts.map((e) => ({ ...e, frd: { latest: w.docs.get(e.id)?.length ?? 0, approved: w.approved.get(e.id) ?? 0 } })),
    createEffort: async (_slug, title, deliberate) => { log.push(`create:${title}:${deliberate}`); const e = effort(w.efforts.length + 1, title); w.efforts.push(e); return e; },
    frd: async (_s, id) => view(id, w.docs.get(id)?.length ?? 0, true),
    putFRD: async (_s, id, frd) => {
      sent.push(frd);
      if (frd.title === '') throw new ApiError(400, 'title is required', 'title');
      if (frd.requirements.length === 0) throw new ApiError(400, 'requirements[0] needs an id and a description', 'requirements[0]');
      const list = w.docs.get(id) ?? [];
      list.push({ doc: frd, by: 'me' });
      w.docs.set(id, list);
      return view(id, list.length, true);
    },
    frdRevisions: async (_s, id) => (w.docs.get(id) ?? []).map((_, i) => view(id, i + 1, false)),
    frdRevision: async (_s, id, n) => view(id, n, true),
    approveFRD: async (_s, id, rev) => { log.push(`approve:${id}:${rev}`); w.approved.set(id, rev ?? 0); return effort(id, 'x'); },
    notify: (m) => { log.push('notify:' + m); },
    ...over,
  };
  const ed = new FrdEditor(asEl(host), 'p1', deps);
  return { host, ed, log, sent };
}

const world = (): World => ({ efforts: [effort(1, 'Alpha')], docs: new Map([[1, [{ doc: FRD0, by: 'me' }]]]), approved: new Map() });

test('helpers split lines and comma lists', () => {
  eq(lines(' a \n\n b\n'), ['a', 'b']);
  eq(commaList('R1, R2 ,,R3'), ['R1', 'R2', 'R3']);
});

test('AC-UI1 (rendered): an FRD title and effort title with <img onerror> are literal text in the FRD list and the overview', async () => {
  const w = world();
  w.efforts = [effort(1, EVIL)];
  w.docs.set(1, [{ doc: { ...FRD0, title: EVIL }, by: 'me' }]);
  const t = harness(w);
  const stop = t.ed.mount();
  await flush();
  const list = byAttr(t.host, 'data-role', 'frd-list')[0];
  ok(list.textContent.includes(EVIL), 'literal in the list');
  eq(byField(t.host, 'title').value, EVIL);
  ok(findAll(t.host, (e) => e.tagName === 'IMG').length === 0, 'no img element in the FRD tab');
  stop();

  const ovHost = new FakeEl('DIV');
  const proj: ProjectView = {
    slug: 'p1', repo_path: '/r', model_scope: 'local', feedback_cmds: [], limits: {}, created: 0, updated: 0, run_state: 'stopped',
    remembered_run_state: 'stopped', effort: effort(1, EVIL), progress: {}, current: [],
  };
  const deps: OverviewDeps = { list: async () => [proj], run: async () => proj, confirm: async () => true, open: () => {} };
  const ov = new Overview(asEl(ovHost), deps);
  const off = ov.mount();
  await flush();
  ok(ovHost.textContent.includes(EVIL), 'literal in the overview');
  ok(findAll(ovHost, (e) => e.tagName === 'IMG').length === 0, 'no img element in the overview');
  off();
});

test('UI5: saving sends the edited fields as a new revision and keeps the verbatim ones', async () => {
  const w = world();
  const t = harness(w);
  const stop = t.ed.mount();
  await flush();
  byField(t.host, 'title').value = 'Second';
  byField(t.host, 'constraints').value = 'c1\nc2\n';
  allByField(t.host, 'req_description')[0].value = 'one, edited';
  button(t.host, 'Add requirement').click();
  allByField(t.host, 'req_id')[1].value = 'R2';
  allByField(t.host, 'req_description')[1].value = 'two';
  allByField(t.host, 'req_priority')[1].value = 'should';
  allByField(t.host, 'ac_requirements')[0].value = 'R1, R2';
  button(t.host, 'Save as new revision').click();
  await flush();
  eq(t.sent.length, 1);
  const f = t.sent[0];
  eq(f.title, 'Second');
  eq(f.constraints, ['c1', 'c2']);
  eq(f.requirements.map((r) => [r.id, r.priority, r.description]), [['R1', 'must', 'one, edited'], ['R2', 'should', 'two']]);
  eq(f.acceptance_criteria[0].requirements, ['R1', 'R2']);
  eq(f.state_model, { keep: ['me'] });
  eq(w.docs.get(1)?.length, 2);
  ok(t.host.textContent.includes('Revision 2'), 'new revision listed');
  stop();
});

test('a 400 shows next to the field it names', async () => {
  const w = world();
  const t = harness(w);
  const stop = t.ed.mount();
  await flush();
  byField(t.host, 'title').value = '';
  button(t.host, 'Save as new revision').click();
  await flush();
  const row = byField(t.host, 'title').parentNode as FakeEl;
  eq(findAll(row, (e) => e.className === 'field-error')[0].textContent, 'title is required');
  eq(w.docs.get(1)?.length, 1);
  stop();
});

test('AC-DBS3 (UI): the approved revision stays viewable after an edit', async () => {
  const w = world();
  w.approved.set(1, 1);
  const t = harness(w);
  const stop = t.ed.mount();
  await flush();
  ok(t.host.textContent.includes('revision 1 approved'), 'approved shown');
  eq(findAll(t.host, (e) => e.textContent === 'Approve revision 1').length, 0);
  byField(t.host, 'objective').value = 'Do it better';
  button(t.host, 'Save as new revision').click();
  await flush();
  const rev1 = byAttr(t.host, 'data-revision', '1')[0];
  ok(rev1.textContent.includes('approved'), 'revision 1 still marked approved');
  const rev2 = byAttr(t.host, 'data-revision', '2')[0];
  ok(!rev2.textContent.includes('approved'), 'revision 2 is not approved');
  button(rev1, 'View').click();
  await flush();
  const shown = byAttr(t.host, 'data-role', 'revision-view')[0];
  ok(shown.textContent.includes('Revision 1 (approved)') && shown.textContent.includes('"objective": "Do it"'), 'old text viewable: ' + shown.textContent);
  stop();
});

test('approve targets the latest saved revision and is offered only when it is not approved yet', async () => {
  const w = world();
  const t = harness(w);
  const stop = t.ed.mount();
  await flush();
  button(t.host, 'Approve revision 1').click();
  await flush();
  eq(t.log.filter((l) => l.startsWith('approve:')), ['approve:1:1']);
  eq(findAll(t.host, (e) => e.textContent === 'Approve revision 1').length, 0);
  stop();
});

test('an effort without an FRD starts from a template; the new-effort form creates efforts', async () => {
  const w: World = { efforts: [], docs: new Map(), approved: new Map() };
  const t = harness(w);
  const stop = t.ed.mount();
  await flush();
  ok(t.host.textContent.includes('No efforts yet'), 'empty state');
  byField(t.host, 'effort_title').value = 'First effort';
  byField(t.host, 'deliberate').checked = true;
  button(t.host, 'Create effort').click();
  await flush();
  eq(t.log.filter((l) => l.startsWith('create:')), ['create:First effort:true']);
  eq(byField(t.host, 'id').value, 'FRD-P1-1');
  eq(byField(t.host, 'title').value, 'First effort');
  stop();
});

test('a failed effort create (one active effort per project) is shown in the form', async () => {
  const w = world();
  const t = harness(w, { createEffort: async () => { throw new ApiError(409, 'project p1 already has an active effort'); } });
  const stop = t.ed.mount();
  await flush();
  byField(t.host, 'effort_title').value = 'x';
  button(t.host, 'Create effort').click();
  await flush();
  ok(findAll(t.host, (e) => e.className === 'field-error' && e.textContent.includes('already has an active effort')).length === 1, 'shown');
  stop();
});

void run('agentflow-frd');
