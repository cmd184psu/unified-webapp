import { test, run, eq, ok, installFakeDom, FakeEl, asEl, byAttr, button, flush, findAll } from './testkit';
import { PlanViewer, renderPlan, stageName, elapsedText, type PlanDeps } from './plan-view';
import { dispatch, pollSettings } from './events';
import type { DocView, EffortView, PlanDoc, PlanReview, PlanView } from './api';

installFakeDom();
pollSettings.ms = 20;

const effort = (over: Partial<EffortView> = {}): EffortView => ({
  id: 1, project: 'p1', title: 'Alpha', status: 'plan_ready', run_state: 'stopped', integration_branch: '', base_commit: '', yolo: false,
  deliberate: false, created: 0, updated: 0, frd: { latest: 1, approved: 1 }, plan: { latest: 2, approved: 0 }, queue: null, ...over,
});

const doc = (name: string): PlanDoc => ({
  principles: ['P1'], drivers: ['D1'], options: [{ name, description: 'desc', pros: ['fast'], cons: ['risky'] }],
  phases: [{ id: 'PH1', name: 'Build', tasks_description: 'do the build', depends_on: [] }],
  adr: { decision: 'Go ' + name, drivers: ['D1'], alternatives: ['A'], why: 'because', consequences: ['c'], followups: ['f'] },
});

const review = (role: string, round: number, verdict: string, summary: string, rev: number): PlanReview =>
  ({ revision: round, role, round, plan_revision: rev, stage: 1, verdict, summary, issues: [{ description: 'fix ' + role, severity: 'major', file: 'a.go', line: 3 }] });

interface Opts {
  effort?: EffortView;
  state?: Partial<PlanView>;
  reviews?: PlanReview[];
  confirm?: boolean;
  approvedRevision?: number;
}

function harness(o: Opts = {}) {
  const host = new FakeEl('DIV');
  const log: string[] = [];
  const calls = { plan: 0 };
  const reviews = o.reviews ?? [
    review('architect', 1, 'revise', 'architect round one', 1), review('critic', 1, 'revise', 'critic round one', 1),
    review('architect', 2, 'approve', 'architect round two', 2), review('critic', 2, 'approve', '<script>alert(1)</script>', 2),
  ];
  const rounds = [1, 2].map((r) => ({ round: r, reviews: reviews.filter((x) => x.round === r) })).filter((r) => r.reviews.length > 0);
  const state: PlanView = {
    effort: 1, status: 'plan_ready', rounds: 2, revision: 2, plan: doc('two'), not_critic_approved: false, reviews, warnings: null, ...o.state,
  };
  const deps: PlanDeps = {
    efforts: async () => [o.effort ?? effort()],
    plan: async () => { calls.plan++; return { ...state, status: o.state?.status ?? o.effort?.status ?? 'plan_ready' }; },
    planRounds: async () => ({ rounds, revision: state.revision, not_critic_approved: state.not_critic_approved, warnings: state.warnings, waiting: state.waiting }),
    planRevisions: async () => [1, 2].map((n) => ({ revision: n, created_by: 'planner', created: 0, approved: o.approvedRevision === n })),
    planRevision: async (_s, _id, n): Promise<DocView<PlanDoc>> => ({ revision: n, created_by: 'planner', created: 0, approved: o.approvedRevision === n, json: doc(n === 1 ? 'one' : 'two') }),
    generatePlan: async () => { log.push('generate'); return effort(); },
    approvePlan: async () => { log.push('approve'); return effort(); },
    confirm: async (m) => { log.push('confirm:' + m); return o.confirm ?? true; },
    notify: (m) => { log.push('notify:' + m); },
  };
  const v = new PlanViewer(asEl(host), 'p1', deps);
  return { host, v, log, calls };
}

const tab = (host: FakeEl, name: string): FakeEl => byAttr(host, 'data-plan-tab', name)[0];

test('a plan-less effort (null rounds, reviews and revisions) renders without throwing', async () => {
  const t = harness({ state: { revision: 0, rounds: 0, plan: undefined, reviews: null as unknown as PlanReview[], warnings: null }, effort: effort({ plan: { latest: 0, approved: 0 } }) });
  const origRounds = t.v as unknown as { deps: PlanDeps };
  origRounds.deps.planRounds = async () => ({ rounds: null, revision: 0, not_critic_approved: false, warnings: null } as never);
  origRounds.deps.planRevisions = async () => null as never;
  const stop = t.v.mount();
  await flush();
  ok(t.host.textContent.includes('No plan yet'), 'empty state shown');
  stop();
});

test('a view disposed while a fetch is in flight neither draws nor starts a timer', async () => {
  const t = harness({ effort: effort({ status: 'planning' }), state: { status: 'planning' } });
  const deps = (t.v as unknown as { deps: PlanDeps }).deps;
  let release: () => void = () => undefined;
  const gate = new Promise<void>((r) => { release = r; });
  const slow = deps.plan;
  deps.plan = async (...a) => { await gate; return slow(...a); };
  const stop = t.v.mount();
  await flush();
  stop();
  release();
  await flush();
  eq(byAttr(t.host, 'data-role', 'progress-strip').length, 0);
  eq((t.v as unknown as { ticker: unknown }).ticker, null);
});

test('RP2: rounds with planner, architect and critic tabs and verdict chips', async () => {
  const t = harness();
  const stop = t.v.mount();
  await flush();
  eq(byAttr(t.host, 'data-round', '1').length + byAttr(t.host, 'data-round', '2').length, 2);
  ok(tab(t.host, 'architect').textContent.includes('approve') && tab(t.host, 'critic').textContent.includes('approve'), 'round 2 verdicts on the tabs');
  ok(byAttr(t.host, 'data-role', 'plan-doc')[0].textContent.includes('Go two'), 'planner tab shows the round-2 plan');
  tab(t.host, 'architect').click();
  ok(byAttr(t.host, 'data-role', 'review-architect')[0].textContent.includes('architect round two'), 'architect review');
  byAttr(t.host, 'data-round', '1')[0].click();
  await flush();
  tab(t.host, 'critic').click();
  const crit = byAttr(t.host, 'data-role', 'review-critic')[0];
  ok(crit.textContent.includes('critic round one') && crit.textContent.includes('revise'), 'round-1 critic review');
  ok(crit.textContent.includes('fix critic') && crit.textContent.includes('a.go:3'), 'issues listed');
  tab(t.host, 'planner').click();
  ok(byAttr(t.host, 'data-role', 'plan-doc')[0].textContent.includes('Go one'), 'planner tab follows the round');
  stop();
});

test('AC-UI1: a critic summary containing <script> renders literally', async () => {
  const t = harness();
  const stop = t.v.mount();
  await flush();
  tab(t.host, 'critic').click();
  const crit = byAttr(t.host, 'data-role', 'review-critic')[0];
  ok(crit.textContent.includes('<script>alert(1)</script>'), 'literal');
  ok(findAll(t.host, (e) => e.tagName === 'SCRIPT').length === 0, 'no script element');
  stop();
});

test('RP2: a plan that ran out of rounds is badged not critic-approved and approving it asks first', async () => {
  const t = harness({ state: { not_critic_approved: true }, confirm: false });
  const stop = t.v.mount();
  await flush();
  ok(byAttr(t.host, 'data-role', 'plan-header')[0].textContent.includes('not critic-approved'), 'badge');
  button(t.host, 'Approve plan').click();
  await flush();
  ok(t.log.some((l) => l.startsWith('confirm:The critic did not approve')), 'asked');
  eq(t.log.filter((l) => l === 'approve'), []);
  stop();
  const t2 = harness({ state: { not_critic_approved: true }, confirm: true });
  const stop2 = t2.v.mount();
  await flush();
  button(t2.host, 'Approve plan').click();
  await flush();
  eq(t2.log.filter((l) => l === 'approve'), ['approve']);
  stop2();
});

test('RP3: a human approves; a critic-approved plan needs no extra confirmation and an approved one has no button', async () => {
  const t = harness();
  const stop = t.v.mount();
  await flush();
  button(t.host, 'Approve plan').click();
  await flush();
  eq(t.log.filter((l) => l.startsWith('confirm:')), []);
  eq(t.log.filter((l) => l === 'approve'), ['approve']);
  stop();
  const done = harness({ effort: effort({ status: 'plan_approved' }), approvedRevision: 2 });
  const stop2 = done.v.mount();
  await flush();
  eq(findAll(done.host, (e) => e.tagName === 'BUTTON' && e.textContent === 'Approve plan').length, 0);
  ok(byAttr(done.host, 'data-role', 'plan-header')[0].textContent.includes('revision 2 approved'), 'approved badge');
  stop2();
});

test('MB6: the distinct-model warnings and the waiting reason are shown', async () => {
  const t = harness({ state: { warnings: ['planner and critic share model m1: only one eligible model'], waiting: 'no eligible backend for critic' } });
  const stop = t.v.mount();
  await flush();
  ok(byAttr(t.host, 'data-role', 'plan-warnings')[0].textContent.includes('planner and critic share model m1'), 'warning');
  ok(byAttr(t.host, 'data-role', 'plan-waiting')[0].textContent.includes('no eligible backend for critic'), 'waiting');
  stop();
});

test('AC-DBS3 (UI): every plan revision stays viewable, the approved one is marked', async () => {
  const t = harness({ approvedRevision: 1 });
  const stop = t.v.mount();
  await flush();
  const r1 = byAttr(t.host, 'data-revision', '1')[0];
  ok(r1.textContent.includes('approved'), 'marked');
  button(r1, 'View').click();
  await flush();
  const shown = byAttr(t.host, 'data-role', 'revision-view')[0];
  ok(shown.textContent.includes('Revision 1 (approved)') && shown.textContent.includes('Go one'), 'old revision body');
  stop();
});

test('a draft effort with an FRD offers Generate plan; without rounds the empty state shows', async () => {
  const t = harness({
    effort: effort({ status: 'draft' }), reviews: [],
    state: { revision: 0, rounds: 0, plan: undefined },
  });
  (t.v as unknown as { deps: PlanDeps }).deps.planRevisions = async () => [];
  const stop = t.v.mount();
  await flush();
  ok(t.host.textContent.includes('No plan yet'), 'empty state');
  button(t.host, 'Generate plan').click();
  await flush();
  eq(t.log.filter((l) => l === 'generate'), ['generate']);
  stop();
});

test('renderPlan shows hostile plan text literally', () => {
  const p = doc('<img src=x onerror=alert(1)>');
  const el = renderPlan(p) as unknown as FakeEl;
  ok(el.textContent.includes('<img src=x onerror=alert(1)>'), 'literal');
  ok(findAll(el, (e) => e.tagName === 'IMG').length === 0, 'no element');
});

test('F1: a plan view refetches on an effort.update event for its project and ignores other projects', async () => {
  const t = harness();
  const stop = t.v.mount();
  await flush();
  const before = t.calls.plan;
  dispatch(JSON.stringify({ type: 'effort.update', project: 'other', effort: 9 }));
  await flush();
  eq(t.calls.plan, before);
  dispatch(JSON.stringify({ type: 'effort.update', project: 'p1', effort: 1 }));
  await flush();
  ok(t.calls.plan > before, 'refetched on the event');
  stop();
});

test('F1: while the effort is planning the view polls on its own and stops once it settles', async () => {
  const o: Opts = { effort: effort({ status: 'planning' }) };
  const t = harness(o);
  const stop = t.v.mount();
  await flush();
  ok(byAttr(t.host, 'data-role', 'progress-strip').length === 1, 'strip while planning');
  const first = t.calls.plan;
  await new Promise((r) => setTimeout(r, 120));
  ok(t.calls.plan > first + 1, 'polled without any event');
  o.effort = effort({ status: 'queue_draft' });
  await new Promise((r) => setTimeout(r, 80));
  eq(byAttr(t.host, 'data-role', 'progress-strip').length, 0);
  const settled = t.calls.plan;
  await new Promise((r) => setTimeout(r, 120));
  eq(t.calls.plan, settled);
  stop();
});

test('F2: a first draft with no rounds yet shows that plan, not "No plan yet", and a progress strip with the stage and elapsed time', async () => {
  const t = harness({
    effort: effort({ status: 'planning', updated: Date.now() - 65000 }), reviews: [],
    state: { revision: 0, rounds: 0, plan: undefined },
  });
  const stop = t.v.mount();
  await flush();
  ok(!t.host.textContent.includes('No plan yet'), 'no empty state');
  ok(byAttr(t.host, 'data-role', 'plan-doc')[0].textContent.includes('Go two'), 'the revision on file is shown');
  const strip = byAttr(t.host, 'data-role', 'progress-strip')[0].textContent;
  ok(strip.includes('Architect: reviewing plan revision 2'), 'stage: ' + strip);
  ok(/1m 0[5-9]s/.test(byAttr(t.host, 'data-role', 'elapsed')[0].textContent), 'elapsed: ' + strip);
  stop();
});

test('F2: a failed refetch keeps the rounds on screen instead of resetting to the empty state', async () => {
  const t = harness();
  const stop = t.v.mount();
  await flush();
  (t.v as unknown as { deps: PlanDeps }).deps.planRounds = async () => { throw new Error('boom'); };
  await t.v.load();
  ok(byAttr(t.host, 'data-round', '1').length === 1 && !t.host.textContent.includes('No plan yet'), 'rounds kept');
  stop();
});

test('F2: stageName and elapsedText', () => {
  const rv = (role: string, rev: number) => review(role, 1, 'revise', 's', rev);
  eq(stageName('compiling', [], 1), 'Compiler: turning the plan into tasks');
  eq(stageName('plan_ready', [], 1), '');
  eq(stageName('planning', [], 0), 'Planner: drafting the first plan');
  eq(stageName('planning', [], 1), 'Architect: reviewing plan revision 1');
  eq(stageName('planning', [{ round: 1, reviews: [rv('architect', 1)] }], 1), 'Critic: reviewing round 1');
  eq(stageName('planning', [{ round: 1, reviews: [rv('architect', 1), rv('critic', 1)] }], 1), 'Planner: revising the plan after round 1');
  eq(stageName('planning', [{ round: 1, reviews: [rv('architect', 1), rv('critic', 1)] }], 2), 'Architect: reviewing plan revision 2');
  eq([0, 59000, 65000, 3725000].map(elapsedText), ['0s', '59s', '1m 05s', '62m 05s']);
});

void run('agentflow-plan');
