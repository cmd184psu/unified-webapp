import { test, run, eq, ok, installFakeDom, FakeEl, asEl, byField, button, flush, findAll } from './testkit';
import { SettingsView, LIMIT_FIELDS, type SettingsDeps } from './settings';
import { ApiError, type OpenCodeStatus, type Settings } from './api';

installFakeDom();

const SETTINGS: Settings = {
  max_attempts: 5, stuck_threshold: 3, max_infra_retries: 5, plan_max_rounds: 5,
  worker_timeout: '30m0s', review_timeout: '15m0s', plan_timeout: '20m0s', verify_timeout: '20m0s',
  opencode_path: 'opencode', verify_slots: 2, backend_cooldown: '2m0s',
};

function harness(over: Partial<SettingsDeps> = {}) {
  const host = new FakeEl('DIV');
  const calls: string[] = [];
  const sent: Record<string, unknown>[] = [];
  const picked: { path: string | null } = { path: null };
  const deps: SettingsDeps = {
    settings: async () => ({ ...SETTINGS }),
    putSettings: async (s) => { sent.push(s); return { ...SETTINGS, ...s }; },
    opencodeVersion: async (refresh) => {
      calls.push('oc:' + !!refresh);
      return refresh ? { status: 'ok', version: '2.0.0' } : { status: 'ok', version: '1.0.0' };
    },
    pickDir: async (start) => { calls.push('pick:' + start); return picked.path; },
    notify: (m) => { calls.push('notify:' + m); },
    ...over,
  };
  const view = new SettingsView(asEl(host), deps);
  return { host, view, calls, sent, picked };
}

test('CF1: every limit, verify slots, cooldown and the opencode path are shown with their values', async () => {
  const t = harness();
  const stop = t.view.mount();
  await flush();
  for (const f of LIMIT_FIELDS) ok(byField(t.host, f.key).value === String(SETTINGS[f.key]), 'value of ' + f.key);
  eq(byField(t.host, 'verify_slots').value, '2');
  eq(byField(t.host, 'backend_cooldown').value, '2m0s');
  eq(byField(t.host, 'opencode_path').value, 'opencode');
  stop();
});

test('OC1: the opencode version is shown and the refresh button probes again', async () => {
  const t = harness();
  const stop = t.view.mount();
  await flush();
  ok(t.host.textContent.includes('version 1.0.0'), 'cached version');
  button(t.host, 'Refresh').click();
  await flush();
  ok(t.host.textContent.includes('version 2.0.0'), 'refreshed version: ' + t.host.textContent);
  eq(t.calls.filter((c) => c.startsWith('oc:')), ['oc:false', 'oc:true']);
  stop();
});

test('a failed probe shows its error text literally', async () => {
  const evil = '<img src=x onerror=alert(1)>';
  const t = harness({ opencodeVersion: async (): Promise<OpenCodeStatus> => ({ status: 'unavailable', error: evil }) });
  const stop = t.view.mount();
  await flush();
  ok(t.host.textContent.includes(evil), 'literal');
  ok(findAll(t.host, (e) => e.tagName === 'IMG').length === 0, 'no img element');
  stop();
});

test('save sends every field and confirms', async () => {
  const t = harness();
  const stop = t.view.mount();
  await flush();
  byField(t.host, 'max_attempts').value = ' 7 ';
  await t.view.saveNow();
  await flush();
  eq(t.sent.length, 1);
  eq(t.sent[0].max_attempts, '7');
  eq(Object.keys(t.sent[0]).length, LIMIT_FIELDS.length + 3);
  stop();
});

test('AC-SV1 (UI): a 400 naming a field shows its message next to that field only', async () => {
  const t = harness({
    putSettings: async () => { throw new ApiError(400, 'stuck_threshold must not exceed max_attempts (3)', 'stuck_threshold'); },
  });
  const stop = t.view.mount();
  await flush();
  byField(t.host, 'max_attempts').value = '9';
  await t.view.saveNow();
  await flush();
  const row = byField(t.host, 'stuck_threshold').parentNode as FakeEl;
  const msgs = findAll(row, (e) => e.className === 'field-error' && e.textContent !== '');
  eq(msgs.map((m) => m.textContent), ['stuck_threshold must not exceed max_attempts (3)']);
  const other = byField(t.host, 'max_attempts').parentNode as FakeEl;
  eq(findAll(other, (e) => e.className === 'field-error')[0].textContent, '');
  eq(findAll(t.host, (e) => e.className === 'field-error' && e.textContent !== '').length, 1);
  stop();
});

test('an error that names no field lands in the form-level line', async () => {
  const t = harness({ putSettings: async () => { throw new ApiError(500, 'disk full'); } });
  const stop = t.view.mount();
  await flush();
  byField(t.host, 'max_attempts').value = '9';
  await t.view.saveNow();
  await flush();
  eq(findAll(t.host, (e) => e.className === 'field-error' && e.textContent !== '').map((e) => e.textContent), ['disk full']);
  stop();
});

test('H1: no text asks for a restart; saving applies at once', async () => {
  const t = harness();
  const stop = t.view.mount();
  await flush();
  ok(!/restart/i.test(t.host.textContent), 'no restart text');
  stop();
});

test('H1: browse roots are listed, added through the picker (no typing), removed, and saved as a list', async () => {
  const t = harness({ settings: async () => ({ ...SETTINGS, browse_roots: ['/srv'] }) });
  const stop = t.view.mount();
  await flush();
  eq(findAll(t.host, (e) => e.getAttribute('data-root') !== null).map((e) => e.textContent), ['/srv']);
  eq(findAll(t.host, (e) => e.getAttribute('data-field') === 'browse_roots').length, 0);
  t.picked.path = '/home/me';
  button(t.host, 'Add folder').click();
  await flush();
  eq(t.calls.filter((c) => c.startsWith('pick:')), ['pick:/srv']);
  t.picked.path = '/srv';
  button(t.host, 'Add folder').click();
  await flush();
  eq(findAll(t.host, (e) => e.getAttribute('data-root') !== null).length, 2);
  await t.view.saveNow();
  await flush();
  eq(t.sent[0].browse_roots, ['/srv', '/home/me']);
  button(t.host, 'Remove').click();
  await flush();
  await t.view.saveNow();
  await flush();
  eq(t.sent[1].browse_roots, ['/home/me']);
  stop();
});

test('H1: a cancelled picker adds nothing', async () => {
  const t = harness();
  const stop = t.view.mount();
  await flush();
  button(t.host, 'Add folder').click();
  await flush();
  eq(findAll(t.host, (e) => e.getAttribute('data-root') !== null).length, 0);
  stop();
});

void run('agentflow-settings');
