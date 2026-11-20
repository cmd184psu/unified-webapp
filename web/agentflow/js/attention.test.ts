import { test, run, eq, ok, installFakeDom, FakeEl, asEl, findAll } from './testkit';
import { AttentionBanner, itemKey, titleWith } from './attention';
import type { AttentionItem } from './api';

installFakeDom();

const item = (over: Partial<AttentionItem> = {}): AttentionItem => ({ project: 'p', effort: 3, kind: 'draft_failed', message: 'the draft failed', since: 1, ...over });

test('the count goes in the title once', () => {
  eq(titleWith('AgentFlow', 2), '(2) AgentFlow');
  eq(titleWith('(2) AgentFlow', 1), '(1) AgentFlow');
  eq(titleWith('(2) AgentFlow', 0), 'AgentFlow');
});

test('the banner lists what stopped and hides when nothing waits', async () => {
  const host = new FakeEl('DIV');
  const list = [item(), item({ effort: 4, task: 'T8', kind: 'task_halted', message: 'T8 is exhausted' })];
  const b = new AttentionBanner(asEl(host), { attention: async () => list });
  await b.load();
  ok(!host.hasAttribute('hidden'), 'shown');
  ok(host.textContent.includes('2 things have stopped') && host.textContent.includes('T8 is exhausted'), host.textContent);
  list.length = 0;
  await b.load();
  ok(host.hasAttribute('hidden'), 'hidden when empty');
});

test('a task that needs files shows them and an Allow button that allows them', async () => {
  const host = new FakeEl('DIV');
  const list = [item({ task: 'T005', kind: 'task_needs_files', message: 'T005 needs files', needs_files: [{ path: 'src/game/state.go', reason: 'a TruceTurns field' }] })];
  const calls: string[] = [];
  const b = new AttentionBanner(asEl(host), {
    attention: async () => list,
    allowTaskFiles: async (slug: string, id: number, key: string) => { calls.push(`${slug}/${id}/${key}`); list.length = 0; return {} as never; },
  });
  await b.load();
  ok(host.textContent.includes('src/game/state.go') && host.textContent.includes('a TruceTurns field'), host.textContent);
  const btns = findAll(host, (e) => e.getAttribute('data-role') === 'allow-files');
  eq(btns.length, 1);
  btns[0].click();
  await new Promise((r) => setTimeout(r, 0));
  eq(calls.join(','), 'p/3/T005');
});

test('an item is keyed by what makes it new', () => {
  ok(itemKey(item()) !== itemKey(item({ since: 2 })), 'a later failure is a new item');
  eq(itemKey(item()), itemKey(item()));
});

void run('agentflow-attention');
