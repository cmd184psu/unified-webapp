import { test, run, eq, ok, installFakeDom, byAttr, button, flush, type FakeEl } from './testkit';
import { ConfigPanel, describe, type ConfigDeps } from './configfile';
import type { ImportReport } from './api';

installFakeDom();

const report = (over: Partial<ImportReport> = {}): ImportReport => ({
  applied: false, created_backends: [], updated_backends: [], candidate_roles: [], settings: [], warnings: [], ...over,
});

const fe = (p: ConfigPanel): FakeEl => p.el as unknown as FakeEl;

function harness(r: ImportReport | Error, applied = 0) {
  const calls: boolean[] = [];
  const deps: ConfigDeps = {
    importConfig: async (_c, apply) => {
      calls.push(apply);
      if (r instanceof Error) throw r;
      return { ...r, applied: apply };
    },
  };
  let appliedCount = applied;
  const panel = new ConfigPanel(deps, () => { appliedCount++; });
  return { panel, calls, applied: () => appliedCount };
}

test('describe lists what an import would do, or says nothing changes', () => {
  eq(describe(report()), ['nothing to change']);
  const d = describe(report({ created_backends: ['a'], updated_backends: ['b'], candidate_roles: ['worker'], settings: ['max_attempts'] }));
  eq(d.length, 4);
  ok(d[1].includes('replacing their model lists: b'), 'update names the replaced lists');
});

test('a chosen file is previewed with a dry run and applied only on the second click', async () => {
  const t = harness(report({ created_backends: ['gpu'] }));
  await t.panel.loadText('{"version":1}');
  eq(t.calls, [false]);
  eq(byAttr(fe(t.panel), 'data-role', 'config-preview').length, 1);
  button(fe(t.panel), 'Apply import').click();
  await flush();
  eq(t.calls, [false, true]);
  eq(t.applied(), 1);
  ok(fe(t.panel).textContent.includes('Imported: create backend(s): gpu'), 'the result is shown');
  eq(byAttr(fe(t.panel), 'data-role', 'config-preview').length, 0);
});

test('Cancel drops the preview without applying anything', async () => {
  const t = harness(report({ settings: ['x'] }));
  await t.panel.loadText('{}');
  button(fe(t.panel), 'Cancel').click();
  eq(t.calls, [false]);
  eq(byAttr(fe(t.panel), 'data-role', 'config-preview').length, 0);
  eq(t.applied(), 0);
});

test('a file that is not JSON, or that the server refuses, shows an error and no preview', async () => {
  const t = harness(report());
  await t.panel.loadText('not json');
  eq(t.calls, []);
  ok(fe(t.panel).textContent.includes('not JSON'), 'bad JSON is named');
  const u = harness(new Error('unknown backend kind'));
  await u.panel.loadText('{}');
  ok(fe(u.panel).textContent.includes('unknown backend kind'), 'the server error is shown');
  eq(byAttr(fe(u.panel), 'data-role', 'config-preview').length, 0);
});

void run('agentflow-configfile');
