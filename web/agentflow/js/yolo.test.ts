import { test, run, eq, ok, installFakeDom, FakeEl, asEl, byAttr, button, flush, findAll } from './testkit';
import { yoloControl, confirmText, type YoloDeps } from './yolo';
import { effortBar } from './current';
import { Overview, type OverviewDeps } from './overview';
import type { EffortView, ProjectView } from './api';

installFakeDom();

const effort = (over: Partial<EffortView> = {}): EffortView => ({
  id: 4, project: 'p1', title: 'Alpha', status: 'planning', run_state: 'stopped', integration_branch: '', base_commit: '', yolo: false,
  deliberate: false, created: 0, updated: 0, frd: { latest: 1, approved: 1 }, plan: { latest: 0, approved: 0 }, queue: null, ...over,
});

const INV = [{ id: 'GIT4', text: 'Nothing is ever pushed.' }, { id: 'MB5', text: 'Paid providers stay disabled.' }];

function deps(answer = true) {
  const log: string[] = [];
  const d: YoloDeps = {
    setYOLO: async (_s, id, on, confirm) => { log.push(`set:${id}:${on}:${confirm}`); return effort({ yolo: on }); },
    yoloInvariants: async () => INV,
    confirm: async (m) => { log.push('confirm:' + m); return answer; },
    notify: (m) => { log.push('notify:' + m); },
  };
  return { d, log };
}

test('YO1: enabling asks for confirmation that lists what stays enforced, and sends confirm=true', async () => {
  const { d, log } = deps(true);
  let changed = 0;
  const el = yoloControl('p1', effort(), d, () => changed++) as unknown as FakeEl;
  await flush();
  button(el, 'Enable YOLO').click();
  await flush();
  const msg = log.find((l) => l.startsWith('confirm:')) ?? '';
  ok(msg.includes('GIT4: Nothing is ever pushed.') && msg.includes('MB5'), 'dialog lists the invariants: ' + msg);
  eq(log.filter((l) => l.startsWith('set:')), ['set:4:true:true']);
  eq(changed, 1);
});

test('YO1: declining the dialog changes nothing', async () => {
  const { d, log } = deps(false);
  const el = yoloControl('p1', effort(), d, () => {}) as unknown as FakeEl;
  await flush();
  button(el, 'Enable YOLO').click();
  await flush();
  eq(log.filter((l) => l.startsWith('set:')), []);
});

test('YO1: disabling needs no confirmation', async () => {
  const { d, log } = deps();
  const el = yoloControl('p1', effort({ yolo: true }), d, () => {}) as unknown as FakeEl;
  await flush();
  button(el, 'Disable YOLO').click();
  await flush();
  eq(log.filter((l) => l.startsWith('confirm:')), []);
  eq(log.filter((l) => l.startsWith('set:')), ['set:4:false:false']);
});

test('YO3: the always-enforced list from the server is rendered beside the toggle, as text', async () => {
  const { d } = deps();
  d.yoloInvariants = async () => [{ id: 'X', text: '<img src=x onerror=alert(1)>' }];
  const el = yoloControl('p1', effort(), d, () => {}) as unknown as FakeEl;
  await flush();
  const list = byAttr(el, 'data-role', 'yolo-invariants')[0];
  ok(list.textContent.includes('Always enforced') && list.textContent.includes('<img src=x onerror=alert(1)>'), 'listed literally');
  eq(findAll(el, (e) => e.tagName === 'IMG').length, 0);
});

test('the badge shows on the effort header (with and without the toggle) and on the overview card when YOLO is on', async () => {
  const on = effort({ yolo: true });
  const bar = effortBar('p1', [on], on, () => {}) as unknown as FakeEl;
  eq(byAttr(bar, 'data-role', 'yolo-badge').length, 1);
  const { d } = deps();
  const bar2 = effortBar('p1', [on], on, () => {}, { deps: d, onChanged: () => {} }) as unknown as FakeEl;
  eq(byAttr(bar2, 'data-role', 'yolo-badge').length, 1);
  const off = effortBar('p1', [effort()], effort(), () => {}) as unknown as FakeEl;
  eq(byAttr(off, 'data-role', 'yolo-badge').length, 0);

  const proj = (yolo: boolean): ProjectView => ({
    slug: 'p1', repo_path: '/r', model_scope: 'local', feedback_cmds: [], limits: {}, created: 0, updated: 0, run_state: 'stopped',
    remembered_run_state: 'stopped', effort: effort({ yolo }), progress: {}, current: [],
  });
  for (const yolo of [true, false]) {
    const host = new FakeEl('DIV');
    const od: OverviewDeps = { list: async () => [proj(yolo)], run: async () => proj(yolo), confirm: async () => true, open: () => {} };
    const stop = new Overview(asEl(host), od).mount();
    await flush();
    eq(byAttr(host, 'data-role', 'yolo-badge').length, yolo ? 1 : 0);
    stop();
  }
});

test('a finished effort has no toggle; confirmText names the effort', () => {
  const { d } = deps();
  const el = yoloControl('p1', effort({ status: 'complete' }), d, () => {}) as unknown as FakeEl;
  eq(findAll(el, (e) => e.tagName === 'BUTTON').length, 0);
  ok(confirmText(effort(), INV).startsWith('Enable YOLO for effort #4?'), 'text');
});

void run('agentflow-yolo');
