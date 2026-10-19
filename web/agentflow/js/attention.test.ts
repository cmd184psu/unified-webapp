import { test, run, eq, ok, installFakeDom, FakeEl, asEl } from './testkit';
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

test('an item is keyed by what makes it new', () => {
  ok(itemKey(item()) !== itemKey(item({ since: 2 })), 'a later failure is a new item');
  eq(itemKey(item()), itemKey(item()));
});

void run('agentflow-attention');
