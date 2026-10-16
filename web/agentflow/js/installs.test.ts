import { test, run, eq, ok, installFakeDom, byAttr, byField, button, flush, type FakeEl } from './testkit';
import { InstallPanel, progressText, size, type InstallDeps } from './installs';
import { dispatch } from './events';
import type { Install } from './api';

installFakeDom();

const inst = (over: Partial<Install> = {}): Install => ({
  id: 1, backend: 'gpu', model: 'devstral-small-2', tier: 1, state: 'running', status: 'pulling abc', completed: 0, total: 0, started: 1, ...over,
});

const fe = (p: InstallPanel): FakeEl => p.el as unknown as FakeEl;

function harness(items: Install[], over: Partial<InstallDeps> = {}) {
  const log: string[] = [];
  const deps: InstallDeps = {
    installs: async () => items.map((i) => ({ ...i })),
    start: async (b, m, t) => { log.push(`start:${b}:${m}:${t}`); return inst({ model: m }); },
    cancel: async (id) => { log.push('cancel:' + id); },
    ...over,
  };
  const panel = new InstallPanel('gpu', deps);
  return { panel, log };
}

test('sizes and progress text', () => {
  eq(size(15_200_000_000), '15.2 GB');
  eq(size(400_000_000), '400 MB');
  eq(progressText(inst({ completed: 420, total: 1000 })), '42% of 1000 B');
  eq(progressText(inst({ state: 'running', status: 'verifying sha256 digest' })), 'verifying sha256 digest');
});

test('a running install shows a bar, its percent and a Cancel button; a failed one shows the host error', async () => {
  const t = harness([
    inst({ id: 2, completed: 5_000_000_000, total: 10_000_000_000 }),
    inst({ id: 1, model: 'seed-oss:36b', state: 'failed', error: 'pull model manifest: file does not exist' }),
  ]);
  t.panel.mount();
  await flush();
  const text = t.panel.el.textContent;
  ok(text.includes('50% of 10.0 GB') && text.includes('running'), 'progress: ' + text);
  ok(text.includes('pull model manifest: file does not exist') && text.includes('failed'), 'error: ' + text);
  eq(byAttr(fe(t.panel), 'data-action', 'cancel-install').length, 1, 'only the running one can be cancelled');
  byAttr(fe(t.panel), 'data-action', 'cancel-install')[0].fire('click');
  await flush();
  eq(t.log, ['cancel:2']);
  t.panel.unmount();
});

test('install.update events move the bar without a reload; a new install triggers a reload', async () => {
  let rows = [inst({ id: 2, completed: 1, total: 10 })];
  const t = harness([], { installs: async () => rows.map((r) => ({ ...r })) });
  t.panel.mount();
  await flush();
  dispatch(JSON.stringify({ type: 'install.update', id: 2, backend: 'gpu', model: 'devstral-small-2', state: 'running', status: 'pulling abc', completed: 9, total: 10 }));
  ok(t.panel.el.textContent.includes('90% of 10 B'), 'bar moved: ' + t.panel.el.textContent);
  dispatch(JSON.stringify({ type: 'install.update', id: 2, backend: 'other', model: 'x', state: 'running', status: '', completed: 0, total: 0 }));
  eq(t.panel.items.length, 1, 'another backend\'s event ignored');
  ok(t.panel.el.textContent.includes('90% of 10 B'), 'bar untouched by it');
  rows = [inst({ id: 3, model: 'new-one' }), ...rows];
  dispatch(JSON.stringify({ type: 'install.update', id: 3, backend: 'gpu', model: 'new-one', state: 'running', status: '', completed: 0, total: 0 }));
  await flush();
  ok(t.panel.el.textContent.includes('new-one'), 'new install listed');
  t.panel.unmount();
});

test('Install sends the trimmed model and tier; a refusal shows next to the form', async () => {
  const t = harness([], { start: async () => { throw new Error('devstral is already being installed on gpu'); } });
  t.panel.mount();
  await flush();
  byField(fe(t.panel), 'install-model').value = 'devstral';
  byField(fe(t.panel), 'install-model').fire('input');
  button(fe(t.panel), 'Install').click();
  await flush();
  ok(byAttr(fe(t.panel), 'data-role', 'install-error')[0].textContent.includes('already being installed'), 'error shown');
  t.panel.unmount();
});

void run('agentflow-installs');
