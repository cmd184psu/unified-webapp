import { test, run, eq, ok, installFakeDom, FakeEl, asEl, flush } from './testkit';
import { mountProjectTab } from './projecttab';

installFakeDom();

// A project that was dropped can still be the remembered pick. The tab must
// not ask the server about it: it checks the list first and forgets it.
test('a remembered project that no longer exists is never requested, and is forgotten', async () => {
  const store = new Map<string, string>([['agentflow:project', 'webcalc-local3']]);
  const g = globalThis as unknown as Record<string, unknown>;
  const oldStorage = g.localStorage;
  const oldFetch = g.fetch;
  g.localStorage = { getItem: (k: string) => store.get(k) ?? null, setItem: (k: string, v: string) => { store.set(k, v); } };
  const urls: string[] = [];
  g.fetch = async (path: string) => {
    urls.push(path);
    if (path === '/api/projects') return { ok: true, status: 200, json: async () => [{ slug: 'webcalc-t1', run_state: 'stopped' }] };
    return { ok: true, status: 200, json: async () => [] };
  };
  const host = new FakeEl('DIV');
  const stop = mountProjectTab(asEl(host), '');
  await flush();
  await flush();
  stop();
  g.localStorage = oldStorage;
  g.fetch = oldFetch;
  eq(urls.filter((u) => u.includes('webcalc-local3')), [], 'no request names the gone project');
  ok(urls.some((u) => u.startsWith('/api/projects/webcalc-t1')), 'the surviving project is used: ' + urls.join(' '));
  eq(store.get('agentflow:project'), 'webcalc-t1');
});

void run('agentflow-projecttab');
