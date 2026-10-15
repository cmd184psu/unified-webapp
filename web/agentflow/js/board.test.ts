import { timing } from './autosave';
import { test, run, eq, ok, installFakeDom, FakeEl, asEl, byField, byAttr, button, flush, findAll } from './testkit';
import { Board, counts, renderVerify, renderReviewOut, type BoardDeps } from './board';
import { StreamPanel, MAX_LINES, type SourceLike, type StreamDeps } from './stream';
import { statusKey } from './dom';
import type { YoloDeps } from './yolo';
import { dispatch, pollSettings } from './events';
import { mountProjectTab, SUB_TABS } from './projecttab';
import { api } from './api';
import { ApiError, type Attempt, type EffortView, type Stage, type TaskDetail, type TaskView } from './api';

// The suite runs under node (test-web.mjs pipes the bundle to it); the app tsconfig has no node types.
declare const require: (id: string) => { readFileSync(path: string, enc: string): string };
const { readFileSync } = require('fs');

installFakeDom();
timing.delay = 10;
pollSettings.ms = 20;

const effort = (over: Partial<EffortView> = {}): EffortView => ({
  id: 1, project: 'p1', title: 'Alpha', status: 'running', run_state: 'running', integration_branch: 'af/1', base_commit: '', yolo: false,
  deliberate: false, created: 0, updated: 0, frd: { latest: 1, approved: 1 }, plan: { latest: 1, approved: 1 }, queue: { revision: 1, hash: 'h', status: 'approved' }, ...over,
});

const task = (key: string, status: string, over: Partial<TaskView> = {}): TaskView => ({
  key, phase: 'ph', title: 'Title ' + key, prompt: 'prompt of ' + key, acceptance: ['it works'], files_touched: ['a.go'], depends_on: [], id: 1, queue: 1,
  waiting_reason: '', status, halted: false, attempts: 1, current: null, ...over,
});

const stage = (id: number, role: string, parsed: unknown, over: Partial<Stage> = {}): Stage => ({
  id, attempt: 1, effort: 0, role, backend: 'b1', model: 'm1', lane_job_id: 'j', queue_hash: '', state: 'done', outcome: 'pass',
  raw_output: 'raw ' + role, parsed, prompt_path: '', started: 0, finished: 0, ...over,
});

const attempt = (stages: Stage[]): Attempt => ({
  id: 1, task: 1, n: 1, kind: 'normal', worktree: '', outcome: 'failed', fingerprint: '', worker_model: 'm1', reviewer_model: 'm2', started: 0, finished: 0, stages,
});

class FakeSource implements SourceLike {
  handlers = new Map<string, Array<(ev: { data?: string }) => void>>();
  closed = false;
  constructor(public url: string) {}
  addEventListener(type: string, fn: (ev: { data?: string }) => void): void {
    this.handlers.set(type, [...(this.handlers.get(type) ?? []), fn]);
  }
  close(): void { this.closed = true; }
  emit(type: string, data?: string): void { for (const fn of this.handlers.get(type) ?? []) fn({ data }); }
}

interface World {
  effort: EffortView;
  tasks: TaskView[];
  detail: Record<string, TaskDetail>;
  /** the live project run state; defaults to the effort's */
  live?: string;
}

function harness(w: World, over: Partial<BoardDeps> = {}) {
  const host = new FakeEl('DIV');
  const panel = new FakeEl('ASIDE');
  panel.setAttribute('hidden', '');
  const log: string[] = [];
  const sources: FakeSource[] = [];
  const answers = { confirm: true };
  const deps: BoardDeps = {
    project: async () => ({ run_state: w.live ?? w.effort.run_state }) as never,
    efforts: async () => [w.effort],
    tasks: async () => w.tasks,
    task: async (_s, _id, key) => w.detail[key],
    run: async (_s, op) => { log.push('run:' + op); return {} as never; },
    retryTask: async (_s, _id, key, clean) => { log.push(`retry:${key}:${clean}`); return w.detail[key]; },
    editTask: async (_s, _id, key, def) => { log.push(`edit:${key}:${def.prompt}`); return {} as never; },
    overrideTask: async (_s, _id, key, reason) => { log.push(`override:${key}:${reason}`); return w.detail[key]; },
    abandonEffort: async () => { log.push('abandon'); return w.effort; },
    gitCommands: async () => ({ commands: ["cd '/r'", "git merge --no-ff 'af/1'", "git push origin 'main'"] }),
    confirm: async (m) => { log.push('confirm:' + m); return answers.confirm; },
    copy: (t) => { log.push('copy:' + t); },
    notify: (m) => { log.push('notify:' + m); },
    ...over,
  };
  const streamDeps: StreamDeps = { open: (url) => { const s = new FakeSource(url); sources.push(s); return s; } };
  const yolo: YoloDeps = {
    setYOLO: async () => effort(), yoloInvariants: async () => [], confirm: async () => true, notify: () => {},
  };
  const board = new Board(asEl(host), asEl(panel), 'p1', deps, streamDeps, yolo);
  return { host, panel, board, log, sources, answers };
}

const detailOf = (t: TaskView, attempts: Attempt[] = [], over: Partial<TaskDetail> = {}): TaskDetail => ({ ...t, attempt_list: attempts, progress: [], ...over });

const mixed = (): World => {
  const tasks = [task('T001', 'passed'), task('T002', 'running', { current: { task: 'T002', attempt: 1, stage: 7, role: 'worker', backend: 'b1', model: 'm1', state: 'running' } }),
    task('T003', 'blocked', { waiting_reason: 'waiting for T002' }), task('T004', 'stuck', { halted: true, attempts: 3 })];
  return { effort: effort(), tasks, detail: Object.fromEntries(tasks.map((t) => [t.key, detailOf(t)])) };
};

test('UI6: the board uses the console layout: progress header, task list with chips, detail pane', async () => {
  const t = harness(mixed());
  const stop = t.board.mount();
  await flush();
  const text = t.host.textContent;
  ok(text.includes('25% — passed 1/4'), 'progress: ' + text);
  eq(findAll(t.host, (e) => e.className === 'task-list' || e.className === 'console-body').map((e) => e.className), ['console-body', 'task-list']);
  const rows = findAll(t.host, (e) => e.className.startsWith('task-item '));
  eq(rows.map((r) => r.className), ['task-item status-passed', 'task-item status-in-progress', 'task-item status-blocked', 'task-item status-stuck']);
  ok(text.includes('waiting for T002'), 'waiting reason in the list');
  ok(text.includes('worker · b1/m1'), 'backend and model of the running stage');
  stop();
});

test('selecting a task shows its detail: prompt, acceptance, attempts with VerifyReport, reviewer issues, backend and model', async () => {
  const w = mixed();
  const t4 = w.tasks[3];
  w.detail.T004 = detailOf(t4, [attempt([
    stage(10, 'worker', { summary: 'did the thing' }),
    stage(11, 'verify', { pass: false, checks: [{ name: 'go vet', pass: false, detail: 'bad' }, { name: 'structural', pass: true }], new_failures: ['TestX'], warnings: ['w1'] }, { backend: '', model: '', outcome: 'repair' }),
    stage(12, 'reviewer', { verdict: 'repair', summary: 'needs work', issues: [{ description: 'nil deref', severity: 'major', file: 'a.go', line: 4 }], repair_prompt: 'fix it' }, { model: 'm2', outcome: 'repair' }),
  ])], { progress: ['attempt 1 failed verify'] });
  const t = harness(w);
  const stop = t.board.mount();
  await flush();
  byAttr(t.host, 'data-task', 'T004')[0].click();
  await flush();
  const pane = byAttr(t.host, 'data-role', 'detail-pane')[0];
  const text = pane.textContent;
  ok(text.includes('prompt of T004') && text.includes('it works') && text.includes('a.go'), 'definition');
  ok(text.includes('worker m1 · reviewer m2'), 'models');
  ok(byAttr(pane, 'data-role', 'verify-report')[0].textContent.includes('go vet failed — bad'), 'verify report');
  ok(text.includes('New failures') && text.includes('TestX'), 'new failures');
  const rev = byAttr(pane, 'data-role', 'reviewer-report')[0].textContent;
  ok(rev.includes('needs work') && rev.includes('nil deref') && rev.includes('a.go:4') && rev.includes('fix it'), 'reviewer: ' + rev);
  ok(text.includes('did the thing') && text.includes('attempt 1 failed verify'), 'worker summary and progress log');
  stop();
});

test('AC-UI1: a reviewer summary containing <script> renders literally', async () => {
  const w = mixed();
  w.detail.T004 = detailOf(w.tasks[3], [attempt([stage(12, 'reviewer', { verdict: 'repair', summary: '<script>alert(1)</script>', issues: [{ description: '<img src=x onerror=alert(1)>', severity: 'minor' }] })])]);
  const t = harness(w);
  const stop = t.board.mount();
  await flush();
  byAttr(t.host, 'data-task', 'T004')[0].click();
  await flush();
  ok(t.host.textContent.includes('<script>alert(1)</script>') && t.host.textContent.includes('<img src=x onerror=alert(1)>'), 'literal');
  ok(findAll(t.host, (e) => e.tagName === 'SCRIPT' || e.tagName === 'IMG').length === 0, 'no injected elements');
  const r = renderReviewOut({ summary: '<script>x</script>' }) as unknown as FakeEl;
  ok(r.textContent.includes('<script>x</script>'), 'helper literal');
  ok(renderVerify({ pass: false, checks: [{ name: '<b>', pass: false }] }).textContent === 'failed1 check<b> failed', 'verify helper');
  stop();
});

test('UI3: run controls follow the run state; stop asks first', async () => {
  const t = harness(mixed());
  const stop = t.board.mount();
  await flush();
  eq(findAll(t.host, (e) => e.getAttribute('data-op') !== null).map((e) => e.getAttribute('data-op')), ['pause', 'stop']);
  byAttr(t.host, 'data-op', 'stop')[0].click();
  await flush();
  ok(t.log[0].startsWith('confirm:Stop p1') && t.log[1] === 'run:stop', 'confirmed then sent: ' + t.log.join('|'));
  dispatch(JSON.stringify({ type: 'project.run_state', project: 'p1', effort: 1, state: 'paused', action: 'pause', actor: 'u' }));
  eq(findAll(t.host, (e) => e.getAttribute('data-op') !== null).map((e) => e.getAttribute('data-op')), ['start', 'run-one', 'stop']);
  byAttr(t.host, 'data-op', 'run-one')[0].click();
  await flush();
  eq(t.log.filter((l) => l === 'run:run-one').length, 1);
  stop();
});

test('AT9: a halted task offers retry, edit, override and abandon, each behind a confirmation', async () => {
  const t = harness(mixed());
  const stop = t.board.mount();
  await flush();
  byAttr(t.host, 'data-task', 'T004')[0].click();
  await flush();
  const pane = (): FakeEl => byAttr(t.host, 'data-role', 'detail-pane')[0];
  // Retry with the clean-worktree box ticked, declined then accepted.
  byField(pane(), 'clean_worktree').checked = true;
  t.answers.confirm = false;
  button(pane(), 'Retry').click();
  await flush();
  eq(t.log.filter((l) => l.startsWith('retry:')), []);
  t.answers.confirm = true;
  button(pane(), 'Retry').click();
  await flush();
  eq(t.log.filter((l) => l.startsWith('retry:')), ['retry:T004:true']);
  ok(t.log.some((l) => l.includes('worktree is recreated clean')), 'confirm text names the clean option');
  // Override needs a reason, then a confirmation.
  button(pane(), 'Override as passed').click();
  await flush();
  ok(pane().textContent.includes('A reason is required'), 'reason error beside the field');
  eq(t.log.filter((l) => l.startsWith('override:')), []);
  byField(pane(), 'override_reason').value = ' reviewed by hand ';
  button(pane(), 'Override as passed').click();
  await flush();
  eq(t.log.filter((l) => l.startsWith('override:')), ['override:T004:reviewed by hand']);
  // Abandon.
  t.answers.confirm = false;
  button(pane(), 'Abandon effort').click();
  await flush();
  eq(t.log.filter((l) => l === 'abandon'), []);
  t.answers.confirm = true;
  button(pane(), 'Abandon effort').click();
  await flush();
  eq(t.log.filter((l) => l === 'abandon'), ['abandon']);
  // Non-halted task: no actions.
  byAttr(t.host, 'data-task', 'T001')[0].click();
  await flush();
  eq(findAll(pane(), (e) => e.getAttribute('data-panid') === 'actions').length, 0);
  stop();
});

test('AT9: edit task saves the full definition by itself and says the queue goes back to draft', async () => {
  const t = harness(mixed());
  const stop = t.board.mount();
  await flush();
  byAttr(t.host, 'data-task', 'T004')[0].click();
  await flush();
  button(byAttr(t.host, 'data-role', 'detail-pane')[0], 'Edit task').click();
  byField(t.host, 'prompt').value = 'new prompt';
  byField(t.host, 'acceptance').value = 'a\n\nb';
  byField(t.host, 'depends_on').value = 'T001, T002';
  byField(t.host, 'depends_on').fire('input');
  await new Promise((r) => setTimeout(r, 60));
  await flush();
  ok(t.host.textContent.includes('returns the queue to draft'), 'said up front');
  ok(t.log.some((l) => l.startsWith('notify:Saved T004') && l.includes('approve the queue again')), 'toast');
  eq(t.log.filter((l) => l.startsWith('edit:')), ['edit:T004:new prompt']);
  stop();
});

test('a field error from an edit shows beside the field', async () => {
  const t = harness(mixed(), { editTask: async () => { throw new ApiError(400, 'prompt must not be empty', 'prompt'); } });
  const stop = t.board.mount();
  await flush();
  byAttr(t.host, 'data-task', 'T004')[0].click();
  await flush();
  button(byAttr(t.host, 'data-role', 'detail-pane')[0], 'Edit task').click();
  byField(t.host, 'prompt').value = '';
  byField(t.host, 'prompt').fire('input');
  await new Promise((r) => setTimeout(r, 60));
  await flush();
  const row = byField(t.host, 'prompt').parentNode as FakeEl;
  eq(findAll(row, (e) => e.className === 'field-error')[0].textContent, 'prompt must not be empty');
  stop();
});

test('GIT4: a complete effort shows a Ready to merge card with a copy button per command', async () => {
  const w = mixed();
  w.effort = effort({ status: 'complete', run_state: 'stopped' });
  const t = harness(w);
  const stop = t.board.mount();
  await flush();
  const box = byAttr(t.host, 'data-role', 'git-commands')[0];
  ok(box.textContent.includes('Ready to merge') && box.textContent.includes('Nothing was merged into main'), 'plain statement');
  ok(box.textContent.includes("git merge --no-ff 'af/1'") && box.textContent.includes('never pushes'), 'commands shown');
  const copies = byAttr(box, 'data-action', 'copy-command');
  eq(copies.length, 3);
  copies[1].click();
  eq(t.log.filter((l) => l.startsWith('copy:')), ["copy:git merge --no-ff 'af/1'"]);
  stop();
  const running = harness(mixed());
  const stop2 = running.board.mount();
  await flush();
  eq(byAttr(running.host, 'data-role', 'git-commands').length, 0);
  stop2();
});

test('task.update for this project refetches the list and the open task', async () => {
  const w = mixed();
  const t = harness(w);
  const stop = t.board.mount();
  await flush();
  byAttr(t.host, 'data-task', 'T002')[0].click();
  await flush();
  w.tasks = w.tasks.map((x) => (x.key === 'T002' ? { ...x, status: 'passed', current: null } : x));
  w.detail.T002 = detailOf(w.tasks[1], [], { waiting_reason: '' });
  dispatch(JSON.stringify({ type: 'task.update', project: 'p1', effort: 1, task: 'T002' }));
  await flush();
  ok(t.host.textContent.includes('50% — passed 2/4'), 'progress advanced');
  dispatch(JSON.stringify({ type: 'task.update', project: 'other', effort: 9, task: 'T002' }));
  stop();
});

test('B1: the run controls follow the live state, not the effort\'s persisted run_state', async () => {
  const w = mixed();
  w.effort = effort({ run_state: 'running' });
  w.live = 'stopped';
  const t = harness(w);
  const stop = t.board.mount();
  await flush();
  const ops = (): Array<string | null> => findAll(t.host, (e) => e.getAttribute('data-op') !== null).map((e) => e.getAttribute('data-op'));
  eq(ops(), ['start', 'run-one']);
  eq(byAttr(t.host, 'data-role', 'was-running').length, 1);
  // An event sets the live state and the next refresh (which reads the same live value) keeps it.
  w.live = 'running';
  dispatch(JSON.stringify({ type: 'project.run_state', project: 'p1', effort: 1, state: 'running', action: 'start', actor: 'u' }));
  await flush();
  eq(ops(), ['pause', 'stop']);
  w.live = 'paused';
  w.effort = effort({ run_state: 'stopped' });
  dispatch(JSON.stringify({ type: 'task.update', project: 'p1', effort: 1, task: 'T002' }));
  await flush();
  eq(ops(), ['start', 'run-one', 'stop']);
  stop();
});

test('B2: state.changed does not wipe a typed override reason, the clean box or the edit form', async () => {
  const w = mixed();
  const t = harness(w);
  const stop = t.board.mount();
  await flush();
  byAttr(t.host, 'data-task', 'T004')[0].click();
  await flush();
  byField(t.host, 'override_reason').value = 'typed by hand';
  byField(t.host, 'clean_worktree').checked = true;
  dispatch(JSON.stringify({ type: 'state.changed' }));
  await flush();
  eq(byField(t.host, 'override_reason').value, 'typed by hand');
  eq(byField(t.host, 'clean_worktree').checked, true);
  // The list still refreshes while the detail pane is kept.
  w.tasks = w.tasks.map((x) => (x.key === 'T002' ? { ...x, status: 'passed', current: null } : x));
  dispatch(JSON.stringify({ type: 'task.update', project: 'p1', effort: 1, task: 'T002' }));
  await flush();
  ok(t.host.textContent.includes('passed 2/4'), 'list refreshed');
  eq(byField(t.host, 'override_reason').value, 'typed by hand');
  // The edit form survives too.
  button(byAttr(t.host, 'data-role', 'detail-pane')[0], 'Edit task').click();
  byField(t.host, 'prompt').value = 'half-typed';
  dispatch(JSON.stringify({ type: 'state.changed' }));
  await flush();
  eq(byField(t.host, 'prompt').value, 'half-typed');
  stop();
});

test('a failing gitCommands is quiet and is not retried on every refresh', async () => {
  const w = mixed();
  w.effort = effort({ status: 'complete' });
  let calls = 0;
  let quiet: boolean | undefined;
  const t = harness(w, { gitCommands: async (_s, _id, o) => { calls++; quiet = o?.quiet; throw new ApiError(409, 'not complete'); } });
  const stop = t.board.mount();
  await flush();
  dispatch(JSON.stringify({ type: 'state.changed' }));
  await flush();
  eq(calls, 1);
  eq(quiet, true);
  stop();
});

test('a project tab disposed before the project list arrives mounts no sub-view', async () => {
  const mounted: string[] = [];
  const saved = SUB_TABS[0].mount;
  SUB_TABS[0].mount = (_h, slug) => { mounted.push(slug); };
  const origProjects = api.projects;
  let release: (v: never[]) => void = () => {};
  api.projects = () => new Promise((r) => { release = r as (v: never[]) => void; });
  const host = new FakeEl('DIV');
  const dispose = mountProjectTab(asEl(host), 'p1');
  dispose();
  release([{ slug: 'p1' }] as never);
  await flush();
  eq(mounted, []);
  api.projects = origProjects;
  SUB_TABS[0].mount = saved;
});

// ─── The stream panel ──────────────────────────────────────────────────────

test('UI3: Watch live opens /api/stages/{id}/stream and appends output lines as text; done closes it', async () => {
  const t = harness(mixed());
  const stop = t.board.mount();
  await flush();
  byAttr(t.host, 'data-task', 'T002')[0].click();
  await flush();
  button(byAttr(t.host, 'data-role', 'detail-pane')[0], 'Watch live').click();
  eq(t.sources.map((s) => s.url), ['/api/stages/7/stream']);
  eq(t.panel.getAttribute('hidden'), null);
  t.sources[0].emit('output', JSON.stringify({ stream: 'stdout', line: 'hello <b>world</b>', ts: 'x' }));
  t.sources[0].emit('output', JSON.stringify({ stream: 'stderr', line: '<script>alert(1)</script>', ts: 'x' }));
  const out = byAttr(t.panel, 'data-role', 'stream-output')[0];
  eq(out.textContent, 'hello <b>world</b>\n<script>alert(1)</script>\n');
  ok(findAll(t.panel, (e) => e.tagName === 'B' || e.tagName === 'SCRIPT').length === 0, 'no injected element');
  t.sources[0].emit('done');
  ok(t.sources[0].closed, 'source closed');
  eq(byAttr(t.panel, 'data-role', 'stream-status')[0].textContent, 'finished');
  button(t.panel, 'Close').click();
  eq(t.panel.getAttribute('hidden'), '');
  stop();
});

test('a finished stage shows its stored output without opening a stream; an error closes the source', () => {
  const panel = new FakeEl('ASIDE');
  const sources: FakeSource[] = [];
  const sp = new StreamPanel(asEl(panel), { open: (u) => { const s = new FakeSource(u); sources.push(s); return s; } });
  sp.open({ id: 3, role: 'worker', backend: 'b', model: 'm', state: 'done', outcome: 'pass', raw_output: 'stored <i>text</i>' });
  eq(sources.length, 0);
  ok(panel.textContent.includes('stored <i>text</i>'), 'stored output');
  sp.open({ id: 4, role: 'worker', backend: 'b', model: 'm', state: 'running', outcome: '', raw_output: '' });
  sources[0].emit('error');
  ok(sources[0].closed, 'closed on error');
  eq(byAttr(panel, 'data-role', 'stream-status')[0].textContent, 'stream ended');
});

test('the stream keeps at most MAX_LINES lines', () => {
  const panel = new FakeEl('ASIDE');
  let src!: FakeSource;
  const sp = new StreamPanel(asEl(panel), { open: (u) => (src = new FakeSource(u)) });
  sp.open({ id: 1, role: 'worker', backend: 'b', model: 'm', state: 'running', outcome: '', raw_output: '' });
  for (let i = 0; i < MAX_LINES + 50; i++) src.emit('output', JSON.stringify({ line: 'l' + i }));
  const out = byAttr(panel, 'data-role', 'stream-output')[0];
  eq(out.childNodes.length, MAX_LINES);
  ok(out.textContent.startsWith('l50\n'), 'oldest dropped');
});

// ─── UI6: the selectors board.ts uses exist in style.css ───────────────────

test('UI6: every console-*, status-*, progress-*, stream-* and task-* class board.ts and stream.ts use exists in style.css', () => {
  const css = readFileSync('web/agentflow/style.css', 'utf8');
  const src = readFileSync('web/agentflow/js/board.ts', 'utf8') + readFileSync('web/agentflow/js/stream.ts', 'utf8');
  const used = new Set<string>();
  for (const m of src.matchAll(/class: '([^']*)'/g)) for (const c of m[1].split(/\s+/)) used.add(c);
  for (const m of src.matchAll(/class: `([^`$]*)/g)) for (const c of m[1].split(/\s+/)) used.add(c);
  const relevant = [...used].filter((c) => /^(console-|status-|progress-|stream-|task-|detail-|tab-|pane-)/.test(c));
  ok(relevant.length >= 10, 'found the classes: ' + relevant.join(','));
  const missing = relevant.filter((c) => !new RegExp('\\.' + c.replace(/[-]/g, '\\-') + '(?![\\w-])').test(css));
  eq(missing, []);
  ok(/stream-panel/.test(css) && /progress-bar-wrap/.test(css), 'stream-panel and progress bar styles');
  for (const status of ['blocked', 'ready', 'running', 'repair', 'passed', 'overridden', 'stuck', 'exhausted', 'needs_input', 'needs_attention', 'failed']) {
    const key = statusKey(status);
    ok(new RegExp('\\.status-chip\\.status-' + key + '(?![\\w-])').test(css), 'chip class for ' + status);
    ok(status === 'failed' || new RegExp('\\.task-item\\.status-' + key + '(?![\\w-])').test(css), 'row class for ' + status);
  }
});

test('counts and the dynamic status classes', () => {
  eq(counts([task('a', 'passed'), task('b', 'passed'), task('c', 'ready')]), { total: 3, passed: 2, ready: 1 });
  eq(statusKey('needs_input'), 'needs-input');
  eq(statusKey('running'), 'in-progress');
});

test('AT12: the board shows the budget bar, and a budget stop is explained as not a failure', async () => {
  const w = mixed();
  w.effort = effort({ budget: { tokens: 9500, seconds: 0, token_limit: 10000, seconds_limit: 0, fraction: 0.95, state: 'no_new_tasks' } });
  w.live = 'stopped';
  const t = harness(w, { project: async () => ({ run_state: 'stopped', stop_reason: 'budget' }) as never });
  const stop = t.board.mount();
  await flush();
  eq(byAttr(t.host, 'data-role', 'budget').length, 1);
  ok(t.host.textContent.includes('tokens 9.5k/10k'), 'usage shown');
  eq(byAttr(t.host, 'data-role', 'budget-stop').length, 1);
  ok(t.host.textContent.includes('Nothing failed'), 'says nothing failed');
  stop();
  const plain = harness(mixed());
  const stop2 = plain.board.mount();
  await flush();
  eq(byAttr(plain.host, 'data-role', 'budget').length, 0);
  eq(byAttr(plain.host, 'data-role', 'budget-stop').length, 0);
  stop2();
});

test('F3: the Ready to merge card shows the branch, commits, file count, diffstat and one copy button per command', async () => {
  const w = mixed();
  w.effort = effort({ status: 'complete', run_state: 'stopped', integration_branch: 'agentflow/effort-1' });
  const t = harness(w, { gitCommands: async () => ({
    commands: ['git merge --no-ff agentflow/effort-1'],
    commits: [{ sha: 'abcdef1234567', subject: 'T001: add the thing' }], diffstat: ' a.go | 3 ++-', files: 1,
  }) });
  const stop = t.board.mount();
  await flush();
  const card = byAttr(t.host, 'data-role', 'git-commands')[0].textContent;
  ok(card.includes('agentflow/effort-1') && card.includes('Nothing was merged into main'), 'branch and plain statement');
  ok(card.includes('abcdef1 T001: add the thing') && card.includes('1 file changed') && card.includes('a.go | 3 ++-'), 'optional fields: ' + card);
  stop();
});

test('F3: a complete effort whose git commands cannot be fetched still shows the card with the branch', async () => {
  const w = mixed();
  w.effort = effort({ status: 'complete', run_state: 'stopped', integration_branch: '' });
  const t = harness(w, { gitCommands: async () => { throw new ApiError(500, 'boom'); } });
  const stop = t.board.mount();
  await flush();
  const card = byAttr(t.host, 'data-role', 'git-commands')[0].textContent;
  ok(card.includes('agentflow/effort-1') && card.includes('could not be loaded'), card);
  stop();
});

test('F1: the board polls while the effort is running and refetches on project.update', async () => {
  const w = mixed();
  let tasksCalls = 0;
  const t = harness(w, { tasks: async () => { tasksCalls++; return w.tasks; } });
  const stop = t.board.mount();
  await flush();
  const first = tasksCalls;
  await new Promise((r) => setTimeout(r, 120));
  ok(tasksCalls > first + 1, 'polled while running');
  w.effort = effort({ status: 'complete', run_state: 'stopped' });
  await new Promise((r) => setTimeout(r, 80));
  ok(byAttr(t.host, 'data-role', 'git-commands').length === 1, 'completion shown without a reload');
  const settled = tasksCalls;
  await new Promise((r) => setTimeout(r, 120));
  eq(tasksCalls, settled);
  dispatch(JSON.stringify({ type: 'project.update', project: 'p1' }));
  await flush();
  ok(tasksCalls > settled, 'event refetch');
  stop();
});

void run('agentflow-board');
