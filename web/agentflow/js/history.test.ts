import { test, run, eq, ok, installFakeDom, FakeEl, asEl, byAttr, byField, button, flush, findAll } from './testkit';
import { History, type HistoryDeps } from './history';
import type { YoloDeps } from './yolo';
import { ApiError, type EffortView } from './api';

installFakeDom();

const effort = (id: number, title: string, over: Partial<EffortView> = {}): EffortView => ({
  id, project: 'p1', title, status: 'complete', run_state: 'stopped', integration_branch: '', base_commit: '', yolo: false, deliberate: false,
  created: 0, updated: 0, frd: { latest: 2, approved: 1 }, plan: { latest: 1, approved: 1 }, queue: { revision: 1, hash: 'h', status: 'approved' }, ...over,
});

function harness(over: Partial<HistoryDeps> = {}, efforts = [effort(1, 'Old'), effort(2, '<img src=x onerror=alert(1)>', { status: 'running', frd: { latest: 0, approved: 0 }, plan: { latest: 0, approved: 0 }, queue: null })]) {
  const host = new FakeEl('DIV');
  const log: string[] = [];
  const deps: HistoryDeps = {
    efforts: async () => efforts,
    importFRD: async (_s, text) => { log.push('import:' + text); return effort(3, 'Imported'); },
    readFile: async (f) => (f as { text: string }).text,
    notify: (m) => { log.push('notify:' + m); },
    ...over,
  };
  const yolo: YoloDeps = { setYOLO: async () => effort(2, 'x'), yoloInvariants: async () => [], confirm: async () => true, notify: () => {} };
  return { host, log, h: new History(asEl(host), 'p1', deps, yolo) };
}

test('DBS6 (UI): every effort is listed with its states and JSON/Markdown export links', async () => {
  const t = harness();
  const stop = t.h.mount();
  await flush();
  const list = byAttr(t.host, 'data-role', 'history-list')[0];
  ok(list.textContent.includes('FRD revision 2 (approved 1)') && list.textContent.includes('queue revision 1 approved'), 'states');
  const first = byAttr(list, 'data-effort', '1')[0];
  const hrefs = findAll(first, (e) => e.getAttribute('data-export') !== null).map((e) => [e.getAttribute('data-export'), e.getAttribute('href')]);
  eq(hrefs, [
    ['effort-json', '/api/efforts/1/export?format=json'], ['effort-md', '/api/efforts/1/export?format=md'],
    ['frd-json', '/api/efforts/1/frd/export?format=json'], ['frd-md', '/api/efforts/1/frd/export?format=md'],
    ['plan-json', '/api/efforts/1/plan/export?format=json'], ['plan-md', '/api/efforts/1/plan/export?format=md'],
  ]);
  eq(findAll(byAttr(list, 'data-effort', '2')[0], (e) => e.getAttribute('data-export') === 'frd-json').length, 0);
  eq(byAttr(list, 'data-effort', '1')[0].getAttribute('data-effort'), '1');
  ok(list.textContent.indexOf('#2') < list.textContent.indexOf('#1'), 'newest first');
  stop();
});

test('AC-UI1: a hostile effort title is literal text in the history', async () => {
  const t = harness();
  const stop = t.h.mount();
  await flush();
  ok(t.host.textContent.includes('<img src=x onerror=alert(1)>'), 'literal');
  eq(findAll(t.host, (e) => e.tagName === 'IMG').length, 0);
  stop();
});

test('DBS6 (UI): choosing a JSON file and importing posts its text; success reloads', async () => {
  const t = harness();
  const stop = t.h.mount();
  await flush();
  button(t.host, 'Import FRD').click();
  await flush();
  ok(t.host.textContent.includes('Choose a JSON file first'), 'asks for a file');
  byField(t.host, 'frd_file').files = [{ text: '{"id":"F"}' }];
  button(t.host, 'Import FRD').click();
  await flush();
  eq(t.log.filter((l) => l.startsWith('import:')), ['import:{"id":"F"}']);
  ok(t.log.includes('notify:FRD imported as a new effort'), 'toast');
  stop();
});

test('an import 400 is shown beside the file field', async () => {
  const t = harness({ importFRD: async () => { throw new ApiError(400, 'body: invalid JSON', 'body'); } });
  const stop = t.h.mount();
  await flush();
  byField(t.host, 'frd_file').files = [{ text: 'nope' }];
  button(t.host, 'Import FRD').click();
  await flush();
  const row = byField(t.host, 'frd_file').parentNode as FakeEl;
  eq(findAll(row, (e) => e.className === 'field-error')[0].textContent, 'body: invalid JSON');
  stop();
});

test('the YOLO toggle shows on an active effort only', async () => {
  const t = harness();
  const stop = t.h.mount();
  await flush();
  eq(findAll(byAttr(t.host, 'data-effort', '2')[0], (e) => e.tagName === 'BUTTON' && e.textContent === 'Enable YOLO').length, 1);
  eq(findAll(byAttr(t.host, 'data-effort', '1')[0], (e) => e.tagName === 'BUTTON' && e.textContent === 'Enable YOLO').length, 0);
  stop();
});

void run('agentflow-history');
