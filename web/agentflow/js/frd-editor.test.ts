import { test, run, eq, ok, installFakeDom, FakeEl, asEl, byField, byAttr, allByField, button, flush, findAll } from './testkit';
import { FrdEditor, commaList, lines, type FrdDeps } from './frd-editor';
import { Overview, type OverviewDeps } from './overview';
import { budgetBar, deliberateTriggers } from './effort-settings';
import { ApiError, type DefinitionOfDone, type DocView, type DraftState, type EffortView, type FRD, type FrdPart, type ProjectView, type SizeView } from './api';

installFakeDom();

const asFakeEl = (e: HTMLElement): FakeEl => e as unknown as FakeEl;

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
  const ctl = {
    draft: { state: 'idle' } as DraftState,
    draftFails: '',
    size: null as SizeView | null,
    parts: [] as FrdPart[],
    dod: { items: [], source: 'detected' } as DefinitionOfDone,
    startFails: '',
  };
  const view = (id: number, n: number, body: boolean): DocView<FRD> => {
    const d = w.docs.get(id)?.[n - 1];
    if (!d) throw new ApiError(404, 'not found');
    return { revision: n, created_by: d.by, created: 0, approved: w.approved.get(id) === n, json: body ? d.doc : undefined };
  };
  const deps: FrdDeps = {
    efforts: async () => w.efforts.map((e) => ({ ...e, frd: { latest: w.docs.get(e.id)?.length ?? 0, approved: w.approved.get(e.id) ?? 0 } })),
    createEffort: async (_slug, title, deliberate) => { log.push(`create:${title}:${deliberate}`); const e = effort(w.efforts.length + 1, title); w.efforts.push(e); return e; },
    patchEffort: async (_s, id, patch) => { log.push(`patch:${id}:${JSON.stringify(patch)}`); return effort(id, 'x', { deliberate: patch.deliberate === true }); },
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
    draftFRD: async (_s, _id, description, polish) => {
      log.push(`draft:${description}:${polish}`);
      if (ctl.draftFails) throw new ApiError(409, ctl.draftFails);
      ctl.draft = { state: 'running', started: Date.now() };
      return ctl.draft;
    },
    draftState: async () => ctl.draft,
    definitionOfDone: async () => ctl.dod,
    frdSize: async () => { if (!ctl.size) throw new ApiError(404, 'no size'); return ctl.size; },
    splitFRD: async () => { log.push('split'); return {}; },
    frdParts: async () => ctl.parts,
    startPart: async (_s, _id, n) => {
      log.push(`start-part:${n}`);
      if (ctl.startFails) throw new ApiError(409, ctl.startFails);
      return effort(9, 'part');
    },
    notify: (m) => { log.push('notify:' + m); },
    ...over,
  };
  const ed = new FrdEditor(asEl(host), 'p1', deps);
  return { host, ed, log, sent, ctl };
}

/** openEditor presses Edit (or Write it by hand when there is no FRD yet). */
const openEditor = (host: FakeEl): void => (findAll(host, (e) => e.getAttribute('aria-label') === 'Edit').length ? button(host, 'Edit') : button(host, 'Write it by hand')).click();

const world = (): World => ({ efforts: [effort(1, 'Alpha')], docs: new Map([[1, [{ doc: FRD0, by: 'me' }]]]), approved: new Map() });

test('an FRD without acceptance_criteria or requirements still renders the form', async () => {
  const w = world();
  const { acceptance_criteria: _a, requirements: _r, ...bare } = FRD0;
  w.docs.set(1, [{ doc: bare as unknown as FRD, by: 'me' }]);
  const t = harness(w);
  const stop = t.ed.mount();
  await flush();
  openEditor(t.host);
  ok(byAttr(t.host, 'data-role', 'frd-form').length === 1, 'form rendered');
  stop();
});

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
  openEditor(t.host);
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
  openEditor(t.host);
  byField(t.host, 'title').value = 'Second';
  byField(t.host, 'constraints').value = 'c1\nc2\n';
  allByField(t.host, 'req_description')[0].value = 'one, edited';
  button(t.host, 'Add requirement').click();
  allByField(t.host, 'req_description')[1].value = 'two';
  allByField(t.host, 'req_priority')[1].value = 'should';
  button(t.host, 'Save as new revision').click();
  await flush();
  eq(t.sent.length, 1);
  const f = t.sent[0];
  eq(f.title, 'Second');
  eq(f.constraints, ['c1', 'c2']);
  eq(f.requirements.map((r) => [r.id, r.priority, r.description]), [['R1', 'must', 'one, edited'], ['', 'should', 'two']]);
  eq(f.acceptance_criteria[0].requirements, ['R1']);
  eq(allByField(t.host, 'req_id').length + allByField(t.host, 'ac_id').length, 0);
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
  openEditor(t.host);
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
  openEditor(t.host);
  ok(t.host.textContent.includes('revision 1 approved'), 'approved shown');
  eq(findAll(t.host, (e) => e.getAttribute('aria-label') === 'Approve revision 1').length, 0);
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
  eq(findAll(t.host, (e) => e.getAttribute('aria-label') === 'Approve revision 1').length, 0);
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
  openEditor(t.host);
  eq(byField(t.host, 'id').value, 'FRD-P1-1');
  ok(byField(t.host, 'id').hasAttribute('readonly'), 'the FRD id is read-only');
  eq(byField(t.host, 'title').value, 'First effort');
  stop();
});

test('a failed effort create (one active effort per project) is shown in the form', async () => {
  const w = world();
  w.efforts = [effort(1, 'Alpha', { status: 'complete' })];
  const t = harness(w, { createEffort: async () => { throw new ApiError(409, 'project p1 already has an active effort'); } });
  const stop = t.ed.mount();
  await flush();
  byField(t.host, 'effort_title').value = 'x';
  button(t.host, 'Create effort').click();
  await flush();
  ok(findAll(t.host, (e) => e.className === 'field-error' && e.textContent.includes('already has an active effort')).length === 1, 'shown');
  stop();
});

test('H3: while an effort is active the new-effort form is gone and a disabled + explains why', async () => {
  const w = world();
  const t = harness(w);
  const stop = t.ed.mount();
  await flush();
  eq(findAll(t.host, (e) => e.getAttribute('data-field') === 'effort_title').length, 0);
  const plus = byAttr(t.host, 'data-action', 'new-effort');
  eq(plus.length, 1);
  eq(plus[0].getAttribute('aria-disabled'), 'true');
  ok(plus[0].getAttribute('title')?.includes('one active effort at a time'), 'reason');
  ok(plus[0].getAttribute('title')?.includes('#1'), 'names the active effort');
  plus[0].click();
  eq(findAll(t.host, (e) => e.getAttribute('data-field') === 'effort_title').length, 0);
  stop();
});

test('H3: the current effort comes before the efforts list and its next action is the primary button', async () => {
  const w = world();
  const t = harness(w);
  const stop = t.ed.mount();
  await flush();
  openEditor(t.host);
  const order = findAll(t.host, (e) => e.getAttribute('data-role') === 'frd-form' || e.getAttribute('data-role') === 'frd-list');
  eq(order.map((e) => e.getAttribute('data-role')), ['frd-form', 'frd-list']);
  ok(button(t.host, 'Approve revision 1').className.includes('primary'), 'a saved, unapproved revision: approve is primary');
  ok(!button(t.host, 'Save as new revision').className.includes('primary'), 'save is secondary then');
  w.approved.set(1, 1);
  const done = harness(w);
  const stop2 = done.ed.mount();
  await flush();
  openEditor(done.host);
  ok(button(done.host, 'Save as new revision').className.includes('primary'), 'nothing to approve: save is primary');
  stop2();
  stop();
});

test('H3: with only finished efforts the form is shown, and the + is absent', async () => {
  const w = world();
  w.efforts = [effort(1, 'Alpha', { status: 'abandoned' })];
  const t = harness(w);
  const stop = t.ed.mount();
  await flush();
  eq(findAll(t.host, (e) => e.getAttribute('data-field') === 'effort_title').length, 1);
  eq(byAttr(t.host, 'data-action', 'new-effort').length, 0);
  stop();
});

test('RP5: an FRD that mentions migration suggests deliberate planning; accepting it saves the toggle', async () => {
  const w = world();
  w.docs.set(1, [{ doc: { ...FRD0, objective: 'Run the database migration for orders' }, by: 'me' }]);
  const t = harness(w);
  const stop = t.ed.mount();
  await flush();
  const box = byAttr(t.host, 'data-role', 'deliberate-suggestion');
  eq(box.length, 1);
  ok(box[0].textContent.includes('migration'), 'names the matched topic');
  button(t.host, 'Turn on deliberate planning').click();
  await flush();
  eq(t.log.filter((l) => l.startsWith('patch:')), ['patch:1:{"deliberate":true,"budget_tokens":0,"budget_seconds":0}']);
  stop();
});

test('RP5: an FRD without such topics shows no suggestion; the keyword match is simple', async () => {
  const t = harness(world());
  const stop = t.ed.mount();
  await flush();
  eq(byAttr(t.host, 'data-role', 'deliberate-suggestion').length, 0);
  for (const [text, hit] of [
    ['Add OAuth authentication', true], ['Data MIGRATION to v2', true], ['store PII safely', true], ['personal data export', true],
    ['rotate a password', true], ['add a button', false], ['author attribution', false], ['rename the file', false],
  ] as Array<[string, boolean]>) eq([text, deliberateTriggers(text).length > 0], [text, hit]);
  stop();
});

test('AT12: the effort settings send the token budget and the time budget in seconds', async () => {
  const t = harness(world());
  const stop = t.ed.mount();
  await flush();
  allByField(t.host, 'budget_tokens')[0].value = '250000';
  allByField(t.host, 'budget_minutes')[0].value = '90';
  button(t.host, 'Save settings').click();
  await flush();
  eq(t.log.filter((l) => l.startsWith('patch:')), ['patch:1:{"deliberate":false,"budget_tokens":250000,"budget_seconds":5400}']);
  stop();
});

test('AT12: the budget bar shows usage and what 90% and 100% mean', () => {
  const warn = asFakeEl(budgetBar({ tokens: 9500, seconds: 0, token_limit: 10000, seconds_limit: 0, fraction: 0.95, state: 'no_new_tasks' }));
  ok(warn.textContent.includes('95%') && warn.textContent.includes('tokens 9.5k/10k') && warn.textContent.includes('no new task starts'), warn.textContent);
  eq(findAll(warn, (e) => e.className === 'progress-bar progress-warn').length, 1);
  const over = asFakeEl(budgetBar({ tokens: 10, seconds: 7300, token_limit: 0, seconds_limit: 7200, fraction: 1.01, state: 'exhausted' }));
  ok(over.textContent.includes('100%') && over.textContent.includes('time 2h/2h') && over.textContent.includes('run is stopped'), over.textContent);
  eq(findAll(over, (e) => e.className === 'progress-bar progress-over').length, 1);
});

const FRD1: FRD = { ...FRD0, out_of_scope: ['no ui'] };

test('U1: an FRD opens as a read-only review card with Approve and Edit, and no id inputs', async () => {
  const w = world();
  w.docs.set(1, [{ doc: FRD1, by: 'me' }]);
  const t = harness(w);
  const stop = t.ed.mount();
  await flush();
  const card = byAttr(t.host, 'data-role', 'frd-review')[0];
  ok(card.textContent.includes('First') && card.textContent.includes('Do it') && card.textContent.includes('must: one') && card.textContent.includes('works') && card.textContent.includes('no ui'), card.textContent);
  eq(byAttr(t.host, 'data-role', 'frd-form').length, 0);
  eq(findAll(t.host, (e) => e.tagName === 'TEXTAREA' && e.getAttribute('data-field') !== 'description' && e.getAttribute('data-field') !== null && e.getAttribute('data-field') !== 'effort_title').length, 0);
  button(t.host, 'Approve revision 1').click();
  await flush();
  eq(t.log.filter((l) => l.startsWith('approve:')), ['approve:1:1']);
  button(t.host, 'Edit').click();
  eq(byAttr(t.host, 'data-role', 'frd-form').length, 1);
  eq(allByField(t.host, 'req_id').length + allByField(t.host, 'ac_id').length + allByField(t.host, 'ac_requirements').length, 0);
  button(t.host, 'Close the editor').click();
  await flush();
  eq(byAttr(t.host, 'data-role', 'frd-form').length, 0);
  stop();
});

test('U1: describing sends the text to the draft endpoint, shows a progress strip, then the reviewed FRD', async () => {
  const w = world();
  w.docs.set(1, []);
  const t = harness(w);
  const stop = t.ed.mount();
  await flush();
  button(t.host, 'Draft the FRD').click();
  eq(t.log.filter((l) => l.startsWith('draft:')), []);
  byField(t.host, 'description').value = '  add a login page ';
  byField(t.host, 'description').fire('input');
  button(t.host, 'Draft the FRD').click();
  await flush();
  eq(t.log.filter((l) => l.startsWith('draft:')), ['draft:add a login page:false']);
  eq(byAttr(t.host, 'data-role', 'progress-strip').length, 1);
  w.docs.set(1, [{ doc: FRD1, by: 'planner' }]);
  t.ctl.draft = { state: 'idle' };
  await t.ed.load();
  eq(byAttr(t.host, 'data-role', 'progress-strip').length, 0);
  eq(byAttr(t.host, 'data-role', 'frd-review').length, 1);
  eq(byField(t.host, 'description').value, '');
  stop();
});

test('U1: a failed draft shows the error and keeps the typed description', async () => {
  const t = harness(world());
  t.ctl.draftFails = 'no planner backend';
  const stop = t.ed.mount();
  await flush();
  byField(t.host, 'description').value = 'keep me';
  byField(t.host, 'description').fire('input');
  button(t.host, 'Draft the FRD').click();
  await flush();
  eq(byAttr(t.host, 'data-role', 'draft-error')[0].textContent, 'no planner backend');
  eq(byField(t.host, 'description').value, 'keep me');
  t.ctl.draftFails = '';
  t.ctl.draft = { state: 'failed', error: 'model timed out' };
  await t.ed.load();
  eq(byAttr(t.host, 'data-role', 'draft-error')[0].textContent, 'model timed out');
  eq(byField(t.host, 'description').value, 'keep me');
  stop();
});

test('U1: the polish button asks the server to tidy the existing text', async () => {
  const t = harness(world());
  const stop = t.ed.mount();
  await flush();
  button(t.host, 'Tidy the text').click();
  await flush();
  eq(t.log.filter((l) => l.startsWith('draft:')), ['draft::true']);
  stop();
});

test('U2: the definition of done is listed read-only under the review card, and absent when empty', async () => {
  const t = harness(world());
  const stop = t.ed.mount();
  await flush();
  eq(byAttr(t.host, 'data-role', 'definition-of-done').length, 0);
  t.ctl.dod = { items: ['go test passes', 'no unrelated files changed'], source: 'detected', toolchains: ['go'] };
  await t.ed.load();
  const dod = byAttr(t.host, 'data-role', 'definition-of-done')[0];
  ok(dod.textContent.includes('go test passes') && dod.textContent.includes('no unrelated files changed'), dod.textContent);
  eq(findAll(dod, (e) => e.tagName === 'INPUT').length, 0);
  stop();
});

test('U3: an oversized FRD highlights the meter and offers a split that shows progress until the parts arrive', async () => {
  const t = harness(world());
  t.ctl.size = { frd_tokens: 12000, budget_tokens: 8000, over: true, advice: 'split it' };
  const stop = t.ed.mount();
  await flush();
  const meter = byAttr(t.host, 'data-role', 'size')[0];
  ok(meter.textContent.includes('12k / 8k tokens'), meter.textContent);
  eq(findAll(meter, (e) => e.className === 'progress-bar progress-over').length, 1);
  button(t.host, 'Split into smaller parts').click();
  await flush();
  eq(t.log.filter((l) => l === 'split').length, 1);
  eq(byAttr(t.host, 'data-role', 'progress-strip').length, 1);
  eq(findAll(t.host, (e) => e.getAttribute('aria-label') === 'Split into smaller parts').length, 0);
  t.ctl.parts = [{ n: 1, title: 'Core', tokens: 4000, status: 'started' }, { n: 2, title: 'Extras', summary: 'the rest', tokens: 3000 }];
  await t.ed.load();
  eq(byAttr(t.host, 'data-role', 'progress-strip').length, 0);
  eq(byAttr(t.host, 'data-part', '2').length, 1);
  stop();
});

test('U3: a fitting FRD shows no split; unknown contexts show only a muted hint with the Backends tooltip', async () => {
  const t = harness(world());
  t.ctl.size = { frd_tokens: 100, budget_tokens: 8000, over: false };
  const stop = t.ed.mount();
  await flush();
  eq(byAttr(t.host, 'data-role', 'size').length, 1);
  eq(findAll(t.host, (e) => e.getAttribute('aria-label') === 'Split into smaller parts').length, 0);
  t.ctl.size = { frd_tokens: 100, budget_tokens: 0 };
  await t.ed.load();
  eq(byAttr(t.host, 'data-role', 'size').length, 0);
  const hint = byAttr(t.host, 'data-role', 'size-unknown')[0];
  eq(findAll(hint, (e) => e.getAttribute('title') === 'Set context sizes in Backends').length, 1);
  eq(findAll(hint, (e) => e.tagName === 'BUTTON').length, 0);
  stop();
});

test('U3: pending parts start only when the project has no active effort; a 409 shows inline', async () => {
  const w = world();
  const t = harness(w);
  t.ctl.parts = [{ n: 1, title: 'Core', tokens: 4000 }, { n: 2, title: 'Extras', summary: 'the rest', tokens: 3000 }, { n: 3, title: 'Done', tokens: 10, status: 'started' }];
  const stop = t.ed.mount();
  await flush();
  eq(findAll(byAttr(t.host, 'data-part', '3')[0], (e) => e.tagName === 'BUTTON').length, 0);
  const busy = findAll(byAttr(t.host, 'data-part', '2')[0], (e) => e.tagName === 'BUTTON')[0];
  eq(busy.getAttribute('aria-disabled'), 'true');
  busy.click();
  await flush();
  eq(t.log.filter((l) => l.startsWith('start-part')), []);
  stop();

  w.efforts = [effort(1, 'Alpha', { status: 'complete' })];
  const t2 = harness(w);
  t2.ctl.parts = t.ctl.parts;
  t2.ctl.startFails = 'project p1 already has an active effort';
  const stop2 = t2.ed.mount();
  await flush();
  const go = findAll(byAttr(t2.host, 'data-part', '2')[0], (e) => e.tagName === 'BUTTON')[0];
  eq(go.getAttribute('aria-disabled'), null);
  go.click();
  await flush();
  eq(t2.log.filter((l) => l.startsWith('start-part')), ['start-part:2']);
  ok(byAttr(t2.host, 'data-part', '2')[0].textContent.includes('already has an active effort'), 'inline 409');
  t2.ctl.startFails = '';
  findAll(byAttr(t2.host, 'data-part', '2')[0], (e) => e.tagName === 'BUTTON')[0].click();
  await flush();
  ok(t2.log.includes('notify:Started part 2'), 'started');
  stop2();
});

void run('agentflow-frd');
